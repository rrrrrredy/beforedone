package setup

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/rrrrrredy/beforedone/internal/repository"
)

func TestCodexWritesAbsoluteIdempotentHooksAndStopBudget(t *testing.T) {
	repo := newSetupRepo(t)
	legacy := `{"hooks":{"Stop":[{"hooks":[{"type":"command","command":"beforedone hook codex","commandWindows":"beforedone hook codex","timeout":5}]}]}}`
	if err := os.MkdirAll(filepath.Join(repo.Root, ".codex"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repo.Root, ".codex", "hooks.json"), []byte(legacy), 0o600); err != nil {
		t.Fatal(err)
	}

	first, err := Codex(repo)
	if err != nil {
		t.Fatal(err)
	}
	if len(first.AddedEvents) != 7 {
		t.Fatalf("added events = %v, want all 7 lifecycle events", first.AddedEvents)
	}
	second, err := Codex(repo)
	if err != nil {
		t.Fatal(err)
	}
	if len(second.AddedEvents) != 0 {
		t.Fatalf("second setup added duplicate events: %v", second.AddedEvents)
	}

	data, err := os.ReadFile(first.HooksPath)
	if err != nil {
		t.Fatal(err)
	}
	var root map[string]any
	if err := json.Unmarshal(data, &root); err != nil {
		t.Fatal(err)
	}
	hooks := root["hooks"].(map[string]any)
	for event, rawGroups := range hooks {
		groups := rawGroups.([]any)
		if len(groups) != 1 {
			t.Fatalf("%s groups = %d, want exactly one BeforeDone group", event, len(groups))
		}
		handler := groups[0].(map[string]any)["hooks"].([]any)[0].(map[string]any)
		command := handler["command"].(string)
		commandWindows := handler["commandWindows"].(string)
		if !filepath.IsAbs(strings.TrimSuffix(strings.Trim(command, "'"), " hook codex")) {
			t.Fatalf("%s Unix command is not executable-pinned: %q", event, command)
		}
		windowsExecutable := strings.TrimSuffix(strings.Trim(commandWindows, `"`), " hook codex")
		if !filepath.IsAbs(windowsExecutable) {
			t.Fatalf("%s Windows command is not executable-pinned: %q", event, commandWindows)
		}
		timeout := int(handler["timeout"].(float64))
		if event == "Stop" && timeout != 90 {
			t.Fatalf("Stop timeout = %d, want 90", timeout)
		}
		if event != "Stop" && timeout != 30 {
			t.Fatalf("%s timeout = %d, want 30", event, timeout)
		}
	}
	if strings.Contains(string(data), `"command":"beforedone hook codex"`) {
		t.Fatal("legacy PATH-resolved hook survived migration")
	}
}

func TestRemoveCodexIsIdempotentAndPreservesThirdPartyHooks(t *testing.T) {
	repo := newSetupRepo(t)
	if _, err := Codex(repo); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(repo.Root, ".codex", "hooks.json")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var root map[string]any
	if err := json.Unmarshal(data, &root); err != nil {
		t.Fatal(err)
	}
	hooks := root["hooks"].(map[string]any)
	stop := hooks["Stop"].([]any)
	stop = append(stop, map[string]any{"hooks": []any{map[string]any{"type": "command", "command": "third-party-check"}}})
	hooks["Stop"] = stop
	updated, _ := json.Marshal(root)
	if err := os.WriteFile(path, updated, 0o600); err != nil {
		t.Fatal(err)
	}

	removed, err := RemoveCodex(repo)
	if err != nil {
		t.Fatal(err)
	}
	if len(removed.RemovedEvents) != 7 {
		t.Fatalf("removed events = %v, want 7", removed.RemovedEvents)
	}
	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(strings.ToLower(string(after)), "beforedone") {
		t.Fatalf("BeforeDone handler remains after removal: %s", after)
	}
	if !strings.Contains(string(after), "third-party-check") {
		t.Fatalf("third-party hook was removed: %s", after)
	}
	again, err := RemoveCodex(repo)
	if err != nil {
		t.Fatal(err)
	}
	if len(again.RemovedEvents) != 0 {
		t.Fatalf("second removal was not idempotent: %v", again.RemovedEvents)
	}
}

func newSetupRepo(t *testing.T) *repository.Repository {
	t.Helper()
	root := t.TempDir()
	for _, args := range [][]string{{"init", "-q"}, {"config", "user.email", "test@example.invalid"}, {"config", "user.name", "BeforeDone Test"}} {
		cmd := exec.Command("git", args...)
		cmd.Dir = root
		if output, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, output)
		}
	}
	repo, err := repository.Discover(root)
	if err != nil {
		t.Fatal(err)
	}
	return repo
}
