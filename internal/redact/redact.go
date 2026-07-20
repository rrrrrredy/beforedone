// Package redact defines the shared secret-redaction policy used before
// BeforeDone persists check output, normalized events, incidents, or replay
// output.
package redact

import (
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"
)

var (
	percentAssignment = regexp.MustCompile(`(?i)(?:%22|%27|\\+["']|["'])*(?:api(?:%5f|_|-)?key|access(?:%5f|_|-)?token|refresh(?:%5f|_|-)?token|token|password|passwd|secret|authorization|auth|private(?:%5f|_|-)?key|credential)(?:%22|%27|\\+["']|["'])*(?:%3a|%3d)(?:%22|%27|\\+["']|["'])*[^\s&]+`)
	openAIKey         = regexp.MustCompile(`\bsk-[A-Za-z0-9_-]{12,}\b`)
)

// Compile validates repository-configured regexes. Built-in structured
// redaction is intentionally not represented as regexes: quoted values can
// contain punctuation and nested escaping that a delimiter regex cannot parse
// without leaking a suffix.
func Compile(patterns []string) ([]*regexp.Regexp, error) {
	result := make([]*regexp.Regexp, 0, len(patterns))
	for _, pattern := range patterns {
		compiled, err := regexp.Compile(pattern)
		if err != nil {
			return nil, err
		}
		result = append(result, compiled)
	}
	return result, nil
}

// Apply runs the built-in structural policy, then the caller's configured
// regexes. It prefers over-redaction to retaining a credential suffix.
func Apply(value string, configured []*regexp.Regexp) string {
	value = normalizeControls(value)
	value = redactAssignments(value)
	value = percentAssignment.ReplaceAllString(value, "[REDACTED]")
	value = openAIKey.ReplaceAllString(value, "[REDACTED]")
	for _, redactor := range configured {
		value = redactor.ReplaceAllString(value, "[REDACTED]")
	}
	return value
}

// normalizeControls runs before any secret matcher. Some consumers remove NUL
// or Unicode format characters when rendering an artifact; if redaction ran
// first, an input such as "pass\x00word=secret" could become a visible
// credential assignment only after it had already bypassed the matcher.
func normalizeControls(value string) string {
	value = strings.Map(func(r rune) rune {
		if r == '\t' || r == '\r' || r == '\n' {
			return r
		}
		if unicode.IsControl(r) || unicode.In(r, unicode.Cf) {
			return -1
		}
		return r
	}, value)
	var output strings.Builder
	output.Grow(len(value))
	for i := 0; i < len(value); {
		if value[i] == '\\' {
			end := i
			for end < len(value) && value[end] == '\\' {
				end++
			}
			if decoded, width, ok := decodeUnicodeRune(value[end:]); ok && (unicode.IsControl(decoded) || unicode.In(decoded, unicode.Cf)) {
				i = end + width
				continue
			}
			if end < len(value) && strings.ContainsRune("tnrbf", rune(value[end])) {
				i = end + 1
				continue
			}
		}
		output.WriteByte(value[i])
		i++
	}
	return output.String()
}

// BestEffort is for artifact paths whose configuration has already passed
// validation. An invalid late-supplied pattern is ignored while built-ins
// still run.
func BestEffort(value string, patterns []string) string {
	configured, err := Compile(patterns)
	if err != nil {
		configured = nil
	}
	return Apply(value, configured)
}

// SensitiveLabel reports whether an attribute name itself identifies a value
// that must be replaced even when the value has no recognizable prefix.
func SensitiveLabel(value string) bool {
	return isSensitiveName(normalizeIdentifier(value))
}

func redactAssignments(value string) string {
	matches := findSensitiveKeyMatches(value)
	if len(matches) == 0 {
		return value
	}
	var output strings.Builder
	output.Grow(len(value))
	cursor := 0
	for _, match := range matches {
		if match[0] < cursor {
			continue
		}
		end, ok := assignmentEnd(value, match[1], strings.EqualFold(value[match[0]:match[1]], "authorization"))
		if !ok {
			continue
		}
		output.WriteString(value[cursor:match[0]])
		output.WriteString("[REDACTED]")
		cursor = end
	}
	if cursor == 0 {
		return value
	}
	output.WriteString(value[cursor:])
	return output.String()
}

func assignmentEnd(value string, keyEnd int, authorization bool) (int, bool) {
	i := keyEnd
	for i < len(value) && (isSpace(value[i]) || value[i] == '\\' || value[i] == '\'' || value[i] == '"') {
		i++
	}
	if i >= len(value) || (value[i] != ':' && value[i] != '=') {
		return 0, false
	}
	i++
	for i < len(value) && isSpace(value[i]) {
		i++
	}
	if i >= len(value) {
		return i, true
	}

	openingSlashes := 0
	for i+openingSlashes < len(value) && value[i+openingSlashes] == '\\' {
		openingSlashes++
	}
	quoteAt := i + openingSlashes
	if quoteAt < len(value) && (value[quoteAt] == '"' || value[quoteAt] == '\'') {
		quote := value[quoteAt]
		for j := quoteAt + 1; j < len(value); j++ {
			if value[j] != quote || precedingBackslashes(value, j) != openingSlashes {
				continue
			}
			return j + 1, true
		}
		// An unterminated quoted credential is redacted through the end. Keeping
		// a suffix would be less safe than losing malformed diagnostic text.
		return len(value), true
	}

	end := unquotedEnd(value, i)
	if authorization {
		// Authorization schemes such as Digest and AWS SigV4 contain multiple
		// space-separated credential parameters. Redacting only the scheme and
		// first parameter leaks response, nonce, or signature values. The header
		// value ends at the physical line boundary.
		end = i
		for end < len(value) && value[end] != '\r' && value[end] != '\n' {
			end++
		}
	}
	return end, true
}

func findSensitiveKeyMatches(value string) [][2]int {
	var matches [][2]int
	for i := 0; i < len(value); {
		start := i
		var normalized strings.Builder
		for i < len(value) {
			if isIdentifierByte(value[i]) {
				normalized.WriteByte(toLowerASCII(value[i]))
				i++
				continue
			}
			if decoded, width, ok := decodeUnicodeEscape(value[i:]); ok {
				normalized.WriteByte(toLowerASCII(decoded))
				i += width
				continue
			}
			if value[i] != ' ' {
				r, width := rune(value[i]), 1
				if value[i] >= 0x80 {
					r, width = utf8.DecodeRuneInString(value[i:])
				}
				if unicode.IsControl(r) || unicode.In(r, unicode.Cf) {
					// A leading line/control separator is not part of the key.
					// Keeping it in start would make the authorization special
					// case compare "\r\nAuthorization" instead of the key.
					// Controls inside a partially collected identifier remain
					// ignored so "pass\tword" cannot bypass redaction.
					if normalized.Len() == 0 {
						start = i + width
					}
					i += width
					continue
				}
			}
			break
		}
		if i > start {
			if isSensitiveName(normalized.String()) {
				matches = append(matches, [2]int{start, i})
			}
			continue
		}
		i++
	}
	return matches
}

func normalizeIdentifier(value string) string {
	var normalized strings.Builder
	for i := 0; i < len(value); {
		if isIdentifierByte(value[i]) {
			normalized.WriteByte(toLowerASCII(value[i]))
			i++
			continue
		}
		if decoded, width, ok := decodeUnicodeEscape(value[i:]); ok {
			normalized.WriteByte(toLowerASCII(decoded))
			i += width
			continue
		}
		i++
	}
	return normalized.String()
}

func isSensitiveName(value string) bool {
	compact := strings.NewReplacer("_", "", "-", "").Replace(strings.ToLower(value))
	for _, marker := range []string{
		"apikey", "accesstoken", "refreshtoken", "token", "password", "passwd",
		"secret", "authorization", "privatekey", "credential",
	} {
		if strings.Contains(compact, marker) {
			return true
		}
	}
	return compact == "auth" || strings.HasSuffix(compact, "auth")
}

func decodeUnicodeEscape(value string) (byte, int, bool) {
	if len(value) < 6 || value[0] != '\\' || (value[1] != 'u' && value[1] != 'U') {
		return 0, 0, false
	}
	decoded := 0
	for i := 2; i < 6; i++ {
		digit, ok := hexDigit(value[i])
		if !ok {
			return 0, 0, false
		}
		decoded = (decoded << 4) | digit
	}
	if decoded > 0x7f || !isIdentifierByte(byte(decoded)) {
		return 0, 0, false
	}
	return byte(decoded), 6, true
}

func decodeUnicodeRune(value string) (rune, int, bool) {
	if len(value) < 6 || (value[0] != 'u' && value[0] != 'U') {
		return 0, 0, false
	}
	decoded := 0
	for i := 1; i < 5; i++ {
		digit, ok := hexDigit(value[i])
		if !ok {
			return 0, 0, false
		}
		decoded = (decoded << 4) | digit
	}
	return rune(decoded), 5, true
}

func hexDigit(value byte) (int, bool) {
	switch {
	case value >= '0' && value <= '9':
		return int(value - '0'), true
	case value >= 'a' && value <= 'f':
		return int(value-'a') + 10, true
	case value >= 'A' && value <= 'F':
		return int(value-'A') + 10, true
	default:
		return 0, false
	}
}

func isIdentifierByte(value byte) bool {
	return value >= 'a' && value <= 'z' || value >= 'A' && value <= 'Z' || value >= '0' && value <= '9' || value == '_' || value == '-'
}

func toLowerASCII(value byte) byte {
	if value >= 'A' && value <= 'Z' {
		return value + ('a' - 'A')
	}
	return value
}

func unquotedEnd(value string, start int) int {
	i := start
	for i < len(value) {
		switch value[i] {
		// A credential can legally contain commas, semicolons, braces, or
		// brackets. Stopping at those characters leaks a suffix. Whitespace and
		// the query-string '&' separator are the only safe generic boundaries
		// for an unquoted assignment; over-redaction is intentional.
		case ' ', '\t', '\r', '\n', '&':
			return i
		default:
			i++
		}
	}
	return i
}

func precedingBackslashes(value string, index int) int {
	count := 0
	for index > 0 && value[index-1] == '\\' {
		count++
		index--
	}
	return count
}

func isSpace(value byte) bool {
	return value == ' ' || value == '\t' || value == '\r' || value == '\n'
}
