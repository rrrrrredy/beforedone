package redact

import (
	"strings"
	"testing"
)

func TestDefaultPatternsCoverCommonStructuredAndEncodedAssignments(t *testing.T) {
	cases := []struct {
		name  string
		value string
		leak  string
	}{
		{"plain", `password=plain-secret`, "plain-secret"},
		{"json", `{"password":"json-secret"}`, "json-secret"},
		{"single quoted", `{'token':'single-secret'}`, "single-secret"},
		{"escaped json", `{\"api_key\":\"escaped-secret\"}`, "escaped-secret"},
		{"multiply escaped json", `{\\\"password\\\":\\\"multiply-escaped-secret\\\"}`, "multiply-escaped-secret"},
		{"escaped quote in value", `{\"password\":\"prefix-secret\\\"suffix-secret\"}`, "suffix-secret"},
		{"quoted value with spaces", `token='two word secret'`, "word secret"},
		{"comma in quoted value", `{"password":"prefix,comma-tail-secret"}`, "comma-tail-secret"},
		{"semicolon in quoted value", `{'token':'prefix;semicolon-tail-secret'}`, "semicolon-tail-secret"},
		{"brace in quoted value", `{"secret":"prefix}brace-tail-secret"}`, "brace-tail-secret"},
		{"bracket in quoted value", `{"api_key":"prefix]bracket-tail-secret"}`, "bracket-tail-secret"},
		{"percent encoded", `password%3Dpercent-secret%26next%3Dsafe`, "percent-secret"},
		{"percent encoded quoted comma", `password%3D%22prefix,PERCENT-COMMA-TAIL%22`, "PERCENT-COMMA-TAIL"},
		{"fully percent encoded quoted key", `%22token%22%3D%22PERCENT-KEY-SECRET%22%26x%3Dy`, "PERCENT-KEY-SECRET"},
		{"unicode escaped key", `{"pass\u0077ord":"UNICODE-KEY-SECRET"}`, "UNICODE-KEY-SECRET"},
		{"NUL split key", "pass\x00word=NUL-SPLIT-SECRET", "NUL-SPLIT-SECRET"},
		{"escaped NUL split key", `{"pass\u0000word":"ESCAPED-NUL-SECRET"}`, "ESCAPED-NUL-SECRET"},
		{"multiply escaped NUL split key", `{\"pass\\u0000word\":\"MULTIPLY-ESCAPED-NUL-SECRET\"}`, "MULTIPLY-ESCAPED-NUL-SECRET"},
		{"tab split key", "pass\tword=TAB-SPLIT-SECRET", "TAB-SPLIT-SECRET"},
		{"escaped tab split key", `{"pass\tword":"ESCAPED-TAB-SECRET"}`, "ESCAPED-TAB-SECRET"},
		{"multiply escaped tab split key", `{\"pass\\tword\":\"MULTIPLY-ESCAPED-TAB-SECRET\"}`, "MULTIPLY-ESCAPED-TAB-SECRET"},
		{"format split key", "pass\u200bword=FORMAT-SPLIT-SECRET", "FORMAT-SPLIT-SECRET"},
		{"escaped format split key", `{"pass\u200bword":"ESCAPED-FORMAT-SECRET"}`, "ESCAPED-FORMAT-SECRET"},
		{"unquoted comma", `token=prefix,UNQUOTED-COMMA-TAIL`, "UNQUOTED-COMMA-TAIL"},
		{"unquoted semicolon", `secret=prefix;UNQUOTED-SEMI-TAIL`, "UNQUOTED-SEMI-TAIL"},
		{"authorization", `Authorization: Bearer bearer-secret`, "bearer-secret"},
		{"quoted authorization", `{"authorization":"Bearer quoted-bearer-secret"}`, "quoted-bearer-secret"},
		{"digest authorization", `Authorization: Digest username="alice", response="DIGEST-SECRET-42", nonce="NONCE-SECRET-42"`, "DIGEST-SECRET-42"},
		{"digest authorization after structured secret", "{\"password\":\"prefix,earlier-secret\"}\r\nAuthorization: Digest username=\"alice\", response=\"DIGEST-AFTER-SECRET-42\"\r\n", "DIGEST-AFTER-SECRET-42"},
		{"aws authorization", `Authorization: AWS4-HMAC-SHA256 Credential=AKIAEXAMPLE/aws4_request, SignedHeaders=host;x-amz-date, Signature=AWS-SIGNATURE-SECRET-42`, "AWS-SIGNATURE-SECRET-42"},
		{"openai key", `sk-abcdefghijklmnopqrstuvwx`, "sk-abcdefghijklmnopqrstuvwx"},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			got := Apply(tt.value, nil)
			if strings.Contains(got, tt.leak) {
				t.Fatalf("secret survived redaction: %q", got)
			}
		})
	}
}

func TestSensitiveLabelRecognizesCredentialFields(t *testing.T) {
	for _, value := range []string{"password", "pass\x00word", "api_key", "Authorization", "auth_token"} {
		if !SensitiveLabel(value) {
			t.Fatalf("SensitiveLabel(%q) = false", value)
		}
	}
	if SensitiveLabel("duration") {
		t.Fatal("ordinary attribute was classified as sensitive")
	}
}

func TestRedactionPreservesWindowsDiagnosticPaths(t *testing.T) {
	value := `open D:\Codex\_tmp\go-build123\b001\beforedone-demo.test.exe and C:\new\folder: Access is denied`
	if got := Apply(value, nil); got != value {
		t.Fatalf("Windows path changed during redaction:\n got: %q\nwant: %q", got, value)
	}
}
