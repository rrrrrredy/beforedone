package hooks

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/rrrrrredy/beforedone/internal/config"
	"github.com/rrrrrredy/beforedone/internal/evidence"
	"github.com/rrrrrredy/beforedone/internal/model"
	"github.com/rrrrrredy/beforedone/internal/redact"
	"github.com/rrrrrredy/beforedone/internal/repository"
)

const (
	// MaxEventBytes is the largest encoded normalized event that may be stored
	// as one JSONL record. Writers and readers intentionally share this limit so
	// an accepted event can never poison the ledger for later incident reads.
	MaxEventBytes       = 1 << 20
	MaxEventAttributes  = 256
	maxEventLedgerBytes = 64 << 20
	eventLockWait       = 8 * time.Second
)

const maxHookInput = 8 << 20
const stopValidationBudget = 45 * time.Second

type Output struct {
	Decision      string `json:"decision,omitempty"`
	Reason        string `json:"reason,omitempty"`
	SystemMessage string `json:"systemMessage,omitempty"`
}

func Codex(in io.Reader, out io.Writer, processCWD string) error {
	return codexWithValidationBudget(in, out, processCWD, stopValidationBudget)
}

func codexWithValidationBudget(in io.Reader, out io.Writer, processCWD string, validationBudget time.Duration) error {
	raw, err := io.ReadAll(io.LimitReader(in, maxHookInput+1))
	if err != nil {
		return err
	}
	if len(raw) > maxHookInput {
		return errors.New("hook input exceeds 8 MiB")
	}
	var input map[string]any
	dec := json.NewDecoder(strings.NewReader(string(raw)))
	dec.UseNumber()
	if err := dec.Decode(&input); err != nil {
		return fmt.Errorf("parse Codex hook JSON: %w", err)
	}
	var trailing any
	if err := dec.Decode(&trailing); !errors.Is(err, io.EOF) {
		if err == nil {
			return errors.New("parse Codex hook JSON: trailing JSON value")
		}
		return fmt.Errorf("parse Codex hook JSON trailing content: %w", err)
	}
	eventName := stringValue(input, "hook_event_name")
	stopActive := boolValue(input, "stop_hook_active")

	repo, discoverErr := repository.Discover(processCWD)
	var persistErr error
	if discoverErr == nil {
		if err := repo.EnsureRuntime(); err != nil {
			persistErr = fmt.Errorf("initialize event runtime: %w", err)
		} else {
			cfg, _ := config.Load(repo)
			event, normalizeErr := NormalizeCodex(input, cfg)
			if normalizeErr != nil {
				persistErr = fmt.Errorf("normalize %s event: %w", eventName, normalizeErr)
			} else if err := AppendEvent(repo, event); err != nil {
				persistErr = fmt.Errorf("persist %s event: %w", eventName, err)
			}
		}
	}

	if eventName == "SubagentStop" {
		if persistErr != nil {
			return writeJSON(out, Output{SystemMessage: "BeforeDone could not record this subagent event: " + persistErr.Error()})
		}
		return writeJSON(out, Output{})
	}
	if eventName != "Stop" {
		return persistErr
	}
	if stopActive {
		if persistErr != nil {
			return writeJSON(out, Output{SystemMessage: "BeforeDone's loop guard allows stopping, but the event timeline is incomplete: " + persistErr.Error()})
		}
		return writeJSON(out, Output{})
	}
	if discoverErr != nil {
		return block(out, "BeforeDone cannot find the Git repository: "+discoverErr.Error())
	}
	if persistErr != nil {
		return block(out, "BeforeDone could not safely record the stopping event; the incident timeline would be incomplete: "+persistErr.Error())
	}
	cfg, err := config.Load(repo)
	if err != nil {
		return block(out, "BeforeDone is not configured: "+err.Error())
	}

	var blockers, warnings []string
	ctx, cancel := context.WithTimeout(context.Background(), validationBudget)
	defer cancel()
	fingerprintCache := evidence.NewFingerprintCache()
	ids := make([]string, 0, len(cfg.Checks))
	for id, check := range cfg.Checks {
		if check.IsRequired() {
			ids = append(ids, id)
		}
	}
	sort.Strings(ids)
	for _, id := range ids {
		if err := ctx.Err(); err != nil {
			blockers = append(blockers, fmt.Sprintf("evidence validation exceeded the %s safety budget; narrow relevant_files or run `beforedone doctor`", validationBudget))
			break
		}
		receipt, err := evidence.LoadLatest(repo, id)
		if err != nil {
			blockers = append(blockers, fmt.Sprintf("%s: no evidence receipt; run `beforedone check %s`", id, id))
			continue
		}
		if err := evidence.VerifySignature(repo, receipt); err != nil {
			blockers = append(blockers, fmt.Sprintf("%s: invalid evidence receipt (%v)", id, err))
			continue
		}
		switch receipt.Verdict {
		case model.Inconclusive:
			warnings = append(warnings, fmt.Sprintf("%s: verification was INCONCLUSIVE (%s)", id, receipt.Error))
			continue
		case model.Fail:
			blockers = append(blockers, fmt.Sprintf("%s: latest verification FAILED; run `beforedone check %s` after fixing it", id, id))
			continue
		}
		fresh, reason := evidence.ValidateFreshContext(ctx, repo, cfg, receipt, fingerprintCache)
		if !fresh {
			blockers = append(blockers, fmt.Sprintf("%s: evidence is stale (%s); run `beforedone check %s`", id, reason, id))
		}
	}
	if len(blockers) > 0 {
		return block(out, "BeforeDone requires fresh evidence before completion:\n- "+strings.Join(blockers, "\n- "))
	}
	if len(warnings) > 0 {
		return writeJSON(out, Output{SystemMessage: "BeforeDone could not reach a conclusive verdict:\n- " + strings.Join(warnings, "\n- ")})
	}
	return writeJSON(out, Output{})
}

func block(out io.Writer, reason string) error {
	return writeJSON(out, Output{
		Decision: "block",
		Reason:   reason,
	})
}

func writeJSON(out io.Writer, value any) error {
	enc := json.NewEncoder(out)
	enc.SetEscapeHTML(true)
	return enc.Encode(value)
}

func NormalizeCodex(input map[string]any, cfg model.Config) (model.Event, error) {
	hookName := stringValue(input, "hook_event_name")
	var eventType model.EventType
	switch hookName {
	case "SessionStart":
		eventType = model.SessionStarted
	case "SubagentStart":
		eventType = model.SessionStarted
	case "UserPromptSubmit":
		eventType = model.PromptSubmitted
	case "PreToolUse":
		eventType = model.ToolStarted
	case "PostToolUse", "PostToolUseFailure":
		eventType = model.ToolFinished
	case "Stop", "SubagentStop":
		eventType = model.AgentStopping
	case "SessionEnd", "SessionStop":
		eventType = model.SessionEnded
	default:
		return model.Event{}, fmt.Errorf("unsupported Codex hook event %q", hookName)
	}
	redactors := genericRedactors()
	for _, pattern := range cfg.Capture.RedactPatterns {
		if r, err := regexp.Compile(pattern); err == nil {
			redactors = append(redactors, r)
		}
	}
	event := model.Event{
		SchemaVersion: model.SchemaVersion,
		ID:            evidence.NewID("event"),
		OccurredAt:    time.Now().UTC(),
		Type:          eventType,
		Source:        "codex",
		SessionID:     stringValue(input, "session_id"),
		TurnID:        stringValue(input, "turn_id"),
		CWD:           stringValue(input, "cwd"),
		ToolName:      firstString(input, "tool_name", "tool"),
		Attributes:    map[string]string{},
	}
	for _, key := range []string{"model", "permission_mode", "source", "agent_type", "agent_id"} {
		if value := stringValue(input, key); value != "" {
			event.Attributes[key] = value
		}
	}
	if len(event.Attributes) == 0 {
		event.Attributes = nil
	}
	if exitCode, ok := nestedInt(input, "tool_response", "exit_code"); ok {
		event.ExitCode = &exitCode
	} else if exitCode, ok := nestedInt(input, "tool_result", "exit_code"); ok {
		event.ExitCode = &exitCode
	} else if exitCode, ok := intValue(input, "exit_code"); ok {
		event.ExitCode = &exitCode
	}
	if eventType == model.PromptSubmitted {
		event.Summary = clean(firstString(input, "prompt", "user_prompt"), redactors, 4000)
	} else if eventType == model.ToolStarted {
		event.Summary = summarizeValue(input["tool_input"], redactors)
	} else if eventType == model.ToolFinished {
		value := input["tool_response"]
		if value == nil {
			value = input["tool_result"]
		}
		event.Summary = summarizeValue(value, redactors)
	}
	return SanitizeEvent(event, cfg), nil
}

// SanitizeEvent applies the same bounded, best-effort secret redaction used by
// the Codex adapter to an already-normalized event. Third-party adapters call
// this before persistence so the normalized import path cannot bypass capture
// policy merely by omitting hook_event_name.
func SanitizeEvent(event model.Event, cfg model.Config) model.Event {
	redactors := genericRedactors()
	for _, pattern := range cfg.Capture.RedactPatterns {
		if r, err := regexp.Compile(pattern); err == nil {
			redactors = append(redactors, r)
		}
	}
	event.ID = cleanIdentifier(event.ID, redactors, 256, "event")
	event.Source = clean(event.Source, redactors, 64)
	event.SessionID = cleanIdentifier(event.SessionID, redactors, 256, "session")
	event.TurnID = cleanIdentifier(event.TurnID, redactors, 256, "turn")
	event.CWD = clean(event.CWD, redactors, 1024)
	event.ToolName = clean(event.ToolName, redactors, 256)
	event.Summary = clean(event.Summary, redactors, 20000)
	if len(event.Attributes) > 0 {
		attributes := make(map[string]string, len(event.Attributes))
		for key, value := range event.Attributes {
			originalKey := strings.TrimSpace(key)
			cleanedKey := clean(originalKey, redactors, 128)
			if cleanedKey == "" {
				continue
			}
			if cleanedKey != originalKey {
				cleanedKey = redactedIdentifier("attribute", originalKey)
			}
			cleanedValue := clean(value, redactors, 1024)
			if redact.SensitiveLabel(originalKey) {
				cleanedValue = "[REDACTED]"
			}
			attributes[cleanedKey] = cleanedValue
		}
		if len(attributes) == 0 {
			event.Attributes = nil
		} else {
			event.Attributes = attributes
		}
	}
	return event
}

// ValidateEvent enforces the durable normalized-event contract. In particular,
// the encoded-size check is shared by all writers, including future adapters.
func ValidateEvent(event model.Event) error {
	if event.SchemaVersion != model.SchemaVersion || strings.TrimSpace(event.ID) == "" || len(event.ID) > 256 || event.OccurredAt.IsZero() || !event.Type.Valid() {
		return errors.New("invalid normalized event contract")
	}
	if strings.TrimSpace(event.Source) == "" || len(event.Source) > 64 || len(event.SessionID) > 256 || len(event.TurnID) > 256 || len(event.CWD) > 1024 || len(event.ToolName) > 256 || len(event.Summary) > 20000 {
		return errors.New("normalized event exceeds schema field limits")
	}
	if len(event.Attributes) > MaxEventAttributes {
		return fmt.Errorf("normalized event exceeds %d attributes", MaxEventAttributes)
	}
	for key, value := range event.Attributes {
		if strings.TrimSpace(key) == "" || len(key) > 128 {
			return errors.New("normalized event attribute name exceeds 128 bytes")
		}
		if len(value) > 1024 {
			return errors.New("normalized event attribute exceeds 1024 bytes")
		}
	}
	data, err := json.Marshal(event)
	if err != nil {
		return err
	}
	if len(data) > MaxEventBytes {
		return fmt.Errorf("normalized event exceeds %d encoded bytes", MaxEventBytes)
	}
	return nil
}

func AppendEvent(repo *repository.Repository, event model.Event) error {
	return AppendEvents(repo, []model.Event{event})
}

func AppendEvents(repo *repository.Repository, events []model.Event) error {
	return appendEventBatch(repo, events)
}

func ReadEvents(repo *repository.Repository) ([]model.Event, error) {
	return readEventStore(repo)
}

func genericRedactors() []*regexp.Regexp {
	return nil
}

func cleanIdentifier(value string, redactors []*regexp.Regexp, limit int, prefix string) string {
	original := strings.TrimSpace(value)
	if original == "" {
		return ""
	}
	cleaned := clean(original, redactors, limit)
	if cleaned != original {
		return redactedIdentifier(prefix, original)
	}
	return cleaned
}

func redactedIdentifier(prefix, value string) string {
	sum := sha256.Sum256([]byte(value))
	return prefix + "-redacted-" + hex.EncodeToString(sum[:16])
}

func clean(value string, redactors []*regexp.Regexp, limit int) string {
	value = redact.Apply(value, redactors)
	value = strings.TrimSpace(value)
	if len(value) > limit {
		const suffix = "…"
		cutoff := limit - len(suffix)
		if cutoff < 0 {
			cutoff = 0
		}
		for cutoff > 0 && !utf8.RuneStart(value[cutoff]) {
			cutoff--
		}
		value = value[:cutoff] + suffix
	}
	return value
}

func summarizeValue(value any, redactors []*regexp.Regexp) string {
	if value == nil {
		return ""
	}
	var raw string
	if s, ok := value.(string); ok {
		raw = s
	} else if data, err := json.Marshal(value); err == nil {
		raw = string(data)
	}
	// Preserve a digest when the source had to be truncated.
	sum := sha256.Sum256([]byte(raw))
	result := clean(raw, redactors, 4000)
	if len(raw) > 4000 {
		result += " [sha256:" + hex.EncodeToString(sum[:]) + "]"
	}
	return result
}

func stringValue(input map[string]any, key string) string {
	if value, ok := input[key].(string); ok {
		return value
	}
	return ""
}

func firstString(input map[string]any, keys ...string) string {
	for _, key := range keys {
		if value := stringValue(input, key); value != "" {
			return value
		}
	}
	return ""
}

func boolValue(input map[string]any, key string) bool {
	switch value := input[key].(type) {
	case bool:
		return value
	case string:
		parsed, _ := strconv.ParseBool(value)
		return parsed
	default:
		return false
	}
}

func intValue(input map[string]any, key string) (int, bool) {
	switch value := input[key].(type) {
	case json.Number:
		n, err := strconv.Atoi(value.String())
		return n, err == nil
	case float64:
		return int(value), true
	case int:
		return value, true
	default:
		return 0, false
	}
}

func nestedInt(input map[string]any, parent, key string) (int, bool) {
	nested, ok := input[parent].(map[string]any)
	if !ok {
		return 0, false
	}
	return intValue(nested, key)
}
