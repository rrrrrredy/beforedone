package schemas_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"testing"

	jsonschema "github.com/santhosh-tekuri/jsonschema/v6"
)

const schemaBase = "https://rrrrrredy.github.io/beforedone/schemas/"

func TestPublicSchemasCompileAndEnforceConditionalContracts(t *testing.T) {
	compiler := jsonschema.NewCompiler()
	names := []string{
		"event.schema.json",
		"receipt.schema.json",
		"incident.schema.json",
		"replay-case.schema.json",
		"adapter-manifest.schema.json",
	}
	for _, name := range names {
		data, err := os.ReadFile(name)
		if err != nil {
			t.Fatal(err)
		}
		document, err := jsonschema.UnmarshalJSON(bytes.NewReader(data))
		if err != nil {
			t.Fatalf("decode %s: %v", name, err)
		}
		if err := compiler.AddResource(schemaBase+name, document); err != nil {
			t.Fatalf("register %s: %v", name, err)
		}
	}
	compiled := make(map[string]*jsonschema.Schema, len(names))
	for _, name := range names {
		schema, err := compiler.Compile(schemaBase + name)
		if err != nil {
			t.Fatalf("compile %s: %v", name, err)
		}
		compiled[name] = schema
	}

	receipt := validReceipt()
	assertValid(t, compiled["receipt.schema.json"], receipt)
	invalidPass := clone(receipt)
	invalidPass["exit_code"] = 1
	assertInvalid(t, compiled["receipt.schema.json"], invalidPass)
	invalidFail := clone(receipt)
	invalidFail["verdict"] = "FAIL"
	assertInvalid(t, compiled["receipt.schema.json"], invalidFail)

	incident := validIncident()
	incident["first_observable_divergence"] = map[string]any{
		"precision": "exact_event", "event_id": "event-1", "reason": "verified receipt match",
	}
	assertValid(t, compiled["incident.schema.json"], incident)
	missingExactID := clone(incident)
	missingExactID["first_observable_divergence"] = map[string]any{"precision": "exact_event", "reason": "missing id"}
	assertInvalid(t, compiled["incident.schema.json"], missingExactID)
	window := clone(incident)
	window["first_observable_divergence"] = map[string]any{
		"precision": "time_window", "start_event": "event-1", "end_event": "event-2", "reason": "bounded observation",
	}
	assertValid(t, compiled["incident.schema.json"], window)
	windowWithExact := clone(window)
	windowWithExact["first_observable_divergence"] = map[string]any{
		"precision": "time_window", "start_event": "event-1", "end_event": "event-2", "event_id": "event-1", "reason": "ambiguous",
	}
	assertInvalid(t, compiled["incident.schema.json"], windowWithExact)
	unlocated := clone(incident)
	unlocated["first_observable_divergence"] = map[string]any{"precision": "unlocated", "reason": "insufficient evidence"}
	assertValid(t, compiled["incident.schema.json"], unlocated)
	unlocatedWithEvent := clone(unlocated)
	unlocatedWithEvent["first_observable_divergence"] = map[string]any{
		"precision": "unlocated", "event_id": "event-1", "reason": "contradictory",
	}
	assertInvalid(t, compiled["incident.schema.json"], unlocatedWithEvent)

	event := validEvent(256)
	assertValid(t, compiled["event.schema.json"], event)
	assertInvalid(t, compiled["event.schema.json"], validEvent(257))
}

func validReceipt() map[string]any {
	return map[string]any{
		"schema_version": 1, "id": "receipt-1", "producer": "beforedone.check", "check_id": "unit",
		"argv": []any{"go", "test", "./..."}, "working_directory": ".",
		"started_at": "2026-07-17T00:00:00Z", "finished_at": "2026-07-17T00:00:01Z",
		"exit_code": 0, "verdict": "PASS", "git_commit": "abc",
		"relevant_fingerprint": "sha256:" + strings.Repeat("0", 64), "relevant_file_count": 1,
		"log_path": "logs/unit.log", "log_sha256": "sha256:" + strings.Repeat("1", 64),
		"beforedone_version": "1.0.0", "signature": "hmac-sha256:" + strings.Repeat("2", 64),
	}
}

func validIncident() map[string]any {
	return map[string]any{
		"schema_version": 1, "id": "incident-1", "created_at": "2026-07-17T00:00:00Z",
		"repository": "fixture", "verdict": "FAIL", "timeline": []any{},
		"claim_evidence_matrix": []any{}, "next_steps": []any{"run the check"},
	}
}

func validEvent(attributeCount int) map[string]any {
	attributes := make(map[string]any, attributeCount)
	for i := 0; i < attributeCount; i++ {
		attributes[fmt.Sprintf("field-%03d", i)] = "safe"
	}
	return map[string]any{
		"schema_version": 1, "id": "event-1", "occurred_at": "2026-07-17T00:00:00Z",
		"type": "ToolFinished", "source": "fixture", "attributes": attributes,
	}
}

func clone(value map[string]any) map[string]any {
	copy := make(map[string]any, len(value))
	for key, item := range value {
		copy[key] = item
	}
	return copy
}

func assertValid(t *testing.T, schema *jsonschema.Schema, value any) {
	t.Helper()
	if err := schema.Validate(jsonValue(t, value)); err != nil {
		t.Fatalf("expected valid instance: %v", err)
	}
}

func assertInvalid(t *testing.T, schema *jsonschema.Schema, value any) {
	t.Helper()
	if err := schema.Validate(jsonValue(t, value)); err == nil {
		t.Fatal("expected schema validation failure")
	}
}

func jsonValue(t *testing.T, value any) any {
	t.Helper()
	data, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := jsonschema.UnmarshalJSON(bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	return decoded
}
