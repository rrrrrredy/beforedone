package setup

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPiWritesPinnedIdempotentBoundedExtension(t *testing.T) {
	repo := newSetupRepo(t)
	first, err := Pi(repo)
	if err != nil {
		t.Fatal(err)
	}
	if !first.Changed {
		t.Fatal("first Pi setup did not report a change")
	}
	second, err := Pi(repo)
	if err != nil {
		t.Fatal(err)
	}
	if second.Changed {
		t.Fatal("second Pi setup rewrote identical generated content")
	}

	data, err := os.ReadFile(first.ExtensionPath)
	if err != nil {
		t.Fatal(err)
	}
	text := string(data)
	if !strings.HasPrefix(text, piGeneratedMarker) {
		t.Fatalf("generated marker missing: %s", text[:min(len(text), 200)])
	}
	line := findLine(text, "const BEFORE_DONE_EXECUTABLE = ")
	encoded := strings.TrimSuffix(strings.TrimPrefix(line, "const BEFORE_DONE_EXECUTABLE = "), ";")
	var executable string
	if err := json.Unmarshal([]byte(encoded), &executable); err != nil {
		t.Fatalf("decode pinned executable %q: %v", encoded, err)
	}
	if !filepath.IsAbs(executable) {
		t.Fatalf("Pi extension executable is not absolute: %q", executable)
	}

	for _, event := range []string{
		"session_start",
		"input",
		"turn_start",
		"tool_execution_start",
		"tool_execution_end",
		"agent_settled",
		"session_shutdown",
	} {
		if count := strings.Count(text, `pi.on("`+event+`"`); count != 1 {
			t.Fatalf("%s handler count = %d, want 1", event, count)
		}
	}
	for _, required := range []string{
		`["gate", "--json"]`,
		`["adapter", "ingest", path, "--json"]`,
		`ctx.sessionManager.getBranch()`,
		`pi.appendEntry(STATE_ENTRY, stopState)`,
		`corrective_continuation_used`,
		`ownCorrectionPending || stopState.corrective_continuation_used`,
		`could not persist the corrective-continuation guard`,
		`pi.sendUserMessage`,
	} {
		if !strings.Contains(text, required) {
			t.Fatalf("generated Pi extension omitted %q", required)
		}
	}
	for _, forbidden := range []string{"sourceEvent.text", "sourceEvent.args", "sourceEvent.result"} {
		if strings.Contains(text, forbidden) {
			t.Fatalf("generated Pi extension persists sensitive source payload through %q", forbidden)
		}
	}
}

func TestRemovePiIsIdempotentAndRefusesForeignExtension(t *testing.T) {
	repo := newSetupRepo(t)
	installed, err := Pi(repo)
	if err != nil {
		t.Fatal(err)
	}
	removed, err := RemovePi(repo)
	if err != nil {
		t.Fatal(err)
	}
	if !removed.Removed || !removed.Changed {
		t.Fatalf("remove result = %+v", removed)
	}
	again, err := RemovePi(repo)
	if err != nil {
		t.Fatal(err)
	}
	if again.Removed || again.Changed {
		t.Fatalf("second remove was not idempotent: %+v", again)
	}

	foreign := []byte("export default function foreign() {}\n" + piGeneratedMarker + "\n")
	if err := os.WriteFile(installed.ExtensionPath, foreign, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Pi(repo); err == nil || !strings.Contains(err.Error(), "refusing to overwrite") {
		t.Fatalf("Pi foreign overwrite error = %v", err)
	}
	if _, err := RemovePi(repo); err == nil || !strings.Contains(err.Error(), "refusing to remove") {
		t.Fatalf("Pi foreign remove error = %v", err)
	}
	after, err := os.ReadFile(installed.ExtensionPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(after) != string(foreign) {
		t.Fatalf("foreign extension was modified: %q", after)
	}
}

func findLine(text, prefix string) string {
	for _, line := range strings.Split(text, "\n") {
		if strings.HasPrefix(line, prefix) {
			return line
		}
	}
	return ""
}
