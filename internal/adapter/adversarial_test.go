package adapter

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/rrrrrredy/beforedone/internal/hooks"
	"github.com/rrrrrredy/beforedone/internal/model"
	"github.com/rrrrrredy/beforedone/internal/repository"
)

func TestManifestRequiresAllCapabilityFlags(t *testing.T) {
	manifest := map[string]any{
		"schema_version": 1,
		"name":           "incomplete",
		"version":        "1.0.0",
		"capabilities": map[string]bool{
			"tool_events": true,
			"stop_retry":  true,
			"subagents":   false,
			// transcript is intentionally omitted. Every adapter must make this
			// capability explicit rather than relying on an implicit false.
		},
	}
	data, _ := json.Marshal(manifest)
	if err := validateManifest(data); err == nil {
		t.Fatal("manifest with an omitted capability flag was accepted")
	}
}

func TestManifestRejectsInvalidEventMappingAndUnknownFields(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(map[string]any)
	}{
		{
			name: "invalid event mapping",
			mutate: func(m map[string]any) {
				m["event_map"] = map[string]string{"Stop": "HiddenThoughtRecovered"}
			},
		},
		{
			name: "unknown top-level field",
			mutate: func(m map[string]any) {
				m["execute"] = true
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			manifest := validAdapterManifestObject()
			tt.mutate(manifest)
			data, _ := json.Marshal(manifest)
			if err := validateManifest(data); err == nil {
				t.Fatalf("adapter manifest accepted %s", tt.name)
			}
		})
	}
}

func TestNormalizedEventEnforcesPublishedSchema(t *testing.T) {
	base := map[string]any{
		"schema_version": 1,
		"id":             "event-1",
		"occurred_at":    time.Now().UTC().Format(time.RFC3339Nano),
		"type":           "ToolFinished",
		"source":         "fixture",
	}
	tests := []struct {
		name   string
		mutate func(map[string]any)
	}{
		{"missing source", func(m map[string]any) { delete(m, "source") }},
		{"blank source", func(m map[string]any) { m["source"] = "   " }},
		{"unknown field", func(m map[string]any) { m["chain_of_thought"] = "should not be imported" }},
		{"oversize summary", func(m map[string]any) { m["summary"] = strings.Repeat("x", 20001) }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			object := make(map[string]any, len(base)+1)
			for key, value := range base {
				object[key] = value
			}
			tt.mutate(object)
			if _, err := normalizeObject(object, model.Config{}); err == nil {
				t.Fatalf("normalized event accepted %s", tt.name)
			}
		})
	}
}

func TestDecoderRejectsTrailingJSONValues(t *testing.T) {
	data := []byte(`{"schema_version":1,"id":"first","occurred_at":"2026-07-17T00:00:00Z","type":"SessionStarted","source":"fixture"} {"unexpected":"second"}`)
	if _, err := decodeObjects(data); err == nil {
		t.Fatal("adapter decoder silently ignored a trailing JSON value")
	}
}

func TestCodexAdapterRedactsSecrets(t *testing.T) {
	object := map[string]any{
		"hook_event_name": "PostToolUse",
		"session_id":      `password%3Dcodex-session-secret`,
		"tool_name":       "Bash",
		"tool_response": map[string]any{
			"exit_code": 0,
			"output":    `{\"password\":\"adapter-secret-value,COMMA-TAIL-LEAK\"} {'token':'single-codex-secret;SEMI-TAIL-LEAK'} Authorization: Digest username="alice", response="DIGEST-ADAPTER-LEAK" %22token%22%3D%22PERCENT-ADAPTER-LEAK%22 {"pass\u0077ord":"UNICODE-ADAPTER-LEAK"}`,
		},
	}
	event, err := normalizeObject(object, model.Config{})
	if err != nil {
		t.Fatal(err)
	}
	data, err := json.Marshal(event)
	if err != nil {
		t.Fatal(err)
	}
	stored := string(data)
	for _, secret := range []string{"codex-session-secret", "adapter-secret-value", "single-codex-secret", "COMMA-TAIL-LEAK", "SEMI-TAIL-LEAK", "DIGEST-ADAPTER-LEAK", "PERCENT-ADAPTER-LEAK", "UNICODE-ADAPTER-LEAK"} {
		if strings.Contains(stored, secret) {
			t.Fatalf("Codex event retained secret %q: %s", secret, stored)
		}
	}
	if !strings.HasPrefix(event.SessionID, "session-redacted-") {
		t.Fatalf("Codex session id was not pseudonymized: %q", event.SessionID)
	}
}

func TestNormalizedAdapterRedactsSecretsBeforePersistence(t *testing.T) {
	repo := newAdapterTestRepo(t)
	cfg := model.Config{Capture: model.CaptureConfig{RedactPatterns: []string{`CUSTOM-[A-Z]+`}}}
	payload := `{
  "schema_version": 1,
  "id": "token=id-audit-secret",
  "occurred_at": "2026-07-17T00:00:00Z",
  "type": "ToolFinished",
  "source": "fixture",
  "session_id": "token=session-secret-value",
  "cwd": "C:/CUSTOM-CWD",
  "summary": "{\\\"password\\\":\\\"escaped-audit-secret,COMMA-AUDIT-TAIL\\\",'token':'single-audit-secret;SEMI-AUDIT-TAIL','api_key'%3A'percent-audit-secret'} authorization: Digest username='alice', response='DIGEST-AUDIT-TAIL'\n%22token%22%3D%22PERCENT-AUDIT-TAIL%22 {\\\"pass\\u0077ord\\\":\\\"UNICODE-AUDIT-TAIL\\\"}",
  "attributes": {
    "auth": "token=attribute-secret-value",
    "password": "attribute-audit-secret",
    "CUSTOM-KEY": "safe"
  }
}`
	result, err := Ingest(repo, cfg, strings.NewReader(payload))
	if err != nil {
		t.Fatal(err)
	}
	if len(result.EventIDs) != 1 || result.EventIDs[0] == "token=id-audit-secret" || !strings.HasPrefix(result.EventIDs[0], "event-redacted-") {
		t.Fatalf("ingest returned unsafe event id: %+v", result.EventIDs)
	}
	events, err := hooks.ReadEvents(repo)
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 1 {
		t.Fatalf("events = %d, want 1", len(events))
	}
	data, err := json.Marshal(events[0])
	if err != nil {
		t.Fatal(err)
	}
	stored := string(data)
	for _, secret := range []string{"id-audit-secret", "session-secret-value", "CUSTOM-CWD", "escaped-audit-secret", "COMMA-AUDIT-TAIL", "single-audit-secret", "SEMI-AUDIT-TAIL", "percent-audit-secret", "DIGEST-AUDIT-TAIL", "PERCENT-AUDIT-TAIL", "UNICODE-AUDIT-TAIL", "attribute-secret-value", "attribute-audit-secret", "CUSTOM-KEY"} {
		if strings.Contains(stored, secret) {
			t.Fatalf("normalized adapter persisted secret %q: %s", secret, stored)
		}
	}
	if !strings.Contains(stored, "[REDACTED]") {
		t.Fatalf("persisted event contains no redaction marker: %s", stored)
	}
}

func TestNormalizedAdapterRejectsDuplicateIDsWithoutPartialAppend(t *testing.T) {
	repo := newAdapterTestRepo(t)
	first := model.Event{
		SchemaVersion: model.SchemaVersion,
		ID:            "existing-event",
		OccurredAt:    time.Date(2026, 7, 17, 0, 0, 0, 0, time.UTC),
		Type:          model.SessionStarted,
		Source:        "fixture",
	}
	firstPayload, err := json.Marshal(first)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Ingest(repo, model.Config{}, strings.NewReader(string(firstPayload))); err != nil {
		t.Fatal(err)
	}
	newEvent := first
	newEvent.ID = "new-event-that-must-not-be-partially-appended"
	newEvent.OccurredAt = newEvent.OccurredAt.Add(time.Second)
	duplicate := first
	duplicate.OccurredAt = duplicate.OccurredAt.Add(2 * time.Second)
	batch, err := json.Marshal([]model.Event{newEvent, duplicate})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Ingest(repo, model.Config{}, strings.NewReader(string(batch))); err == nil || !strings.Contains(err.Error(), "duplicate normalized event id") {
		t.Fatalf("duplicate ingest error = %v", err)
	}
	events, err := hooks.ReadEvents(repo)
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 1 || events[0].ID != first.ID {
		t.Fatalf("duplicate batch partially changed ledger: %+v", events)
	}
}

func TestNormalizedAdapterRejectsEventWithTooManyAttributes(t *testing.T) {
	repo := newAdapterTestRepo(t)
	attributes := make(map[string]string, hooks.MaxEventAttributes+1)
	for i := 0; i <= hooks.MaxEventAttributes; i++ {
		attributes[fmt.Sprintf("field-%03d", i)] = "safe"
	}
	event := model.Event{
		SchemaVersion: model.SchemaVersion,
		ID:            "too-many-attributes",
		OccurredAt:    time.Date(2026, 7, 17, 0, 0, 0, 0, time.UTC),
		Type:          model.ToolFinished,
		Source:        "fixture",
		Attributes:    attributes,
	}
	payload, err := json.Marshal(event)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Ingest(repo, model.Config{}, strings.NewReader(string(payload))); err == nil || !strings.Contains(err.Error(), "exceeds 256 attributes") {
		t.Fatalf("ingest error = %v, want bounded attribute rejection", err)
	}
	events, err := hooks.ReadEvents(repo)
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 0 {
		t.Fatalf("rejected event was persisted: %+v", events)
	}
}

func TestAdapterTestRejectsOversizeFixtureBeforeReadingContents(t *testing.T) {
	path := filepath.Join(t.TempDir(), "oversize.json")
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := f.Truncate(maxInput + 1); err != nil {
		f.Close()
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	result, err := Test(path, model.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Failures) != 1 || !strings.Contains(result.Failures[0], "exceeds 16 MiB") {
		t.Fatalf("oversize fixture result = %+v", result)
	}
}

func newAdapterTestRepo(t *testing.T) *repository.Repository {
	t.Helper()
	root := t.TempDir()
	cmd := exec.Command("git", "init", "-q")
	cmd.Dir = root
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git init: %v\n%s", err, output)
	}
	repo, err := repository.Discover(root)
	if err != nil {
		t.Fatal(err)
	}
	return repo
}

func validAdapterManifestObject() map[string]any {
	return map[string]any{
		"schema_version": 1,
		"name":           "test-adapter",
		"version":        "1.0.0",
		"capabilities": map[string]bool{
			"tool_events": true,
			"stop_retry":  true,
			"subagents":   false,
			"transcript":  false,
		},
	}
}
