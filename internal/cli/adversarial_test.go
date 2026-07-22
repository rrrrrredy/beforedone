package cli

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/rrrrrredy/beforedone/internal/config"
	"github.com/rrrrrredy/beforedone/internal/model"
	"gopkg.in/yaml.v3"
)

func TestStableVerdictExitCodes(t *testing.T) {
	tests := []struct {
		verdict model.Verdict
		want    int
	}{
		{model.Pass, ExitOK},
		{model.Fail, ExitFail},
		{model.Inconclusive, ExitInconclusive},
	}
	for _, tt := range tests {
		if got := verdictExit(tt.verdict); got != tt.want {
			t.Fatalf("verdictExit(%s) = %d, want %d", tt.verdict, got, tt.want)
		}
	}
	if ExitUsage != 64 || ExitInternal != 70 {
		t.Fatalf("stable non-verdict exits changed: usage=%d internal=%d", ExitUsage, ExitInternal)
	}
}

func TestPublicCheckExitCodesAndJSONEnvelope(t *testing.T) {
	tests := []struct {
		name string
		argv []string
		want int
	}{
		{"pass", []string{"git", "status", "--short"}, ExitOK},
		{"fail despite PASS text elsewhere", []string{"git", "rev-parse", "--verify", "refs/heads/beforedone-definitely-missing"}, ExitFail},
		{"missing executable", []string{"beforedone-missing-command-42dcf6"}, ExitInconclusive},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			root := newCLITestRepo(t, tt.argv)
			code, stdout, stderr := runCLI(t, root, "check", "unit", "--json")
			if code != tt.want {
				t.Fatalf("exit = %d, want %d\nstdout=%s\nstderr=%s", code, tt.want, stdout, stderr)
			}
			assertSchemaOne(t, stdout)
		})
	}
}

func TestMissingReceiptIsInconclusiveNotInternalError(t *testing.T) {
	root := newCLITestRepo(t, []string{"git", "status", "--short"})
	code, stdout, stderr := runCLI(t, root, "receipt", "unit", "--json")
	if code != ExitInconclusive {
		t.Fatalf("exit = %d, want %d\nstdout=%s\nstderr=%s", code, ExitInconclusive, stdout, stderr)
	}
	assertSchemaOne(t, stdout)
	if !strings.Contains(stdout, "INCONCLUSIVE") {
		t.Fatalf("missing explicit INCONCLUSIVE verdict: %s", stdout)
	}
}

func TestUnknownCheckIsUsageError(t *testing.T) {
	root := newCLITestRepo(t, []string{"git", "status", "--short"})
	code, _, stderr := runCLI(t, root, "check", "not-configured", "--json")
	if code != ExitUsage {
		t.Fatalf("exit = %d, want usage %d (stderr=%s)", code, ExitUsage, stderr)
	}
}

func TestSetupCodexRemoveWorksAfterConfigIsDeleted(t *testing.T) {
	root := newCLITestRepo(t, []string{"git", "status", "--short"})
	code, stdout, stderr := runCLI(t, root, "setup", "codex", "--json")
	if code != ExitOK {
		t.Fatalf("setup exit = %d\nstdout=%s\nstderr=%s", code, stdout, stderr)
	}
	if err := os.Remove(filepath.Join(root, config.FileName)); err != nil {
		t.Fatal(err)
	}
	code, stdout, stderr = runCLI(t, root, "setup", "codex", "--remove", "--json")
	if code != ExitOK {
		t.Fatalf("remove exit = %d\nstdout=%s\nstderr=%s", code, stdout, stderr)
	}
	assertSchemaOne(t, stdout)
	if !strings.Contains(stdout, "removed_events") {
		t.Fatalf("remove result does not report cleanup: %s", stdout)
	}
}

func TestHookRejectsIncompatiblePluginMajorWithValidJSON(t *testing.T) {
	t.Setenv("BEFOREDONE_PLUGIN_VERSION", "2.0.0")
	root := newCLITestRepo(t, []string{"git", "status", "--short"})
	var stdout, stderr bytes.Buffer
	app := &App{
		In:      strings.NewReader(`{"hook_event_name":"Stop","stop_hook_active":false}`),
		Out:     &stdout,
		Err:     &stderr,
		CWD:     root,
		Version: "1.0.0",
	}
	if code := app.Run([]string{"hook", "codex"}); code != ExitOK {
		t.Fatalf("hook exit = %d, stderr=%s", code, stderr.String())
	}
	var output map[string]any
	if err := json.Unmarshal(stdout.Bytes(), &output); err != nil {
		t.Fatalf("hook output is not JSON: %q: %v", stdout.String(), err)
	}
	if !strings.Contains(output["systemMessage"].(string), "not compatible") {
		t.Fatalf("missing compatibility guidance: %s", stdout.String())
	}
}

func TestLicensesIncludesRuntimeAndLinkedDependencies(t *testing.T) {
	code, stdout, stderr := runCLI(t, t.TempDir(), "licenses", "--json")
	if code != ExitOK {
		t.Fatalf("licenses exit = %d\nstdout=%s\nstderr=%s", code, stdout, stderr)
	}
	var output struct {
		SchemaVersion int `json:"schema_version"`
		Licenses      []struct {
			Component string `json:"component"`
		} `json:"licenses"`
	}
	if err := json.Unmarshal([]byte(stdout), &output); err != nil {
		t.Fatalf("invalid licenses JSON %q: %v", stdout, err)
	}
	if output.SchemaVersion != 1 {
		t.Fatalf("schema_version = %d, want 1", output.SchemaVersion)
	}
	want := map[string]bool{
		"BeforeDone":                      false,
		"Go standard library and runtime": false,
		"golang.org/x/sys":                false,
		"gopkg.in/yaml.v3":                false,
	}
	for _, item := range output.Licenses {
		if _, ok := want[item.Component]; ok {
			want[item.Component] = true
		}
	}
	for component, found := range want {
		if !found {
			t.Errorf("licenses output omitted %q", component)
		}
	}
}

func runCLI(t *testing.T, cwd string, args ...string) (int, string, string) {
	t.Helper()
	var stdout, stderr bytes.Buffer
	app := &App{In: bytes.NewReader(nil), Out: &stdout, Err: &stderr, CWD: cwd, Version: "test"}
	code := app.Run(args)
	return code, stdout.String(), stderr.String()
}

func assertSchemaOne(t *testing.T, raw string) {
	t.Helper()
	var object map[string]any
	if err := json.Unmarshal([]byte(raw), &object); err != nil {
		t.Fatalf("invalid JSON output %q: %v", raw, err)
	}
	if got := object["schema_version"]; got != float64(1) {
		t.Fatalf("schema_version = %v, want 1", got)
	}
}

func newCLITestRepo(t *testing.T, argv []string) string {
	t.Helper()
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "README.md"), []byte("fixture\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	runCLIGit(t, root, "init", "-q")
	runCLIGit(t, root, "config", "user.email", "test@example.invalid")
	runCLIGit(t, root, "config", "user.name", "BeforeDone Test")
	runCLIGit(t, root, "add", ".")
	runCLIGit(t, root, "commit", "-m", "fixture")
	cfg := model.Config{
		SchemaVersion: 1,
		Checks: map[string]model.CheckConfig{
			"unit": {Argv: argv, RelevantFiles: []string{"README.md"}, WorkingDirectory: ".", TimeoutSeconds: 120},
		},
	}
	data, err := yaml.Marshal(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, ".beforedone.yaml"), data, 0o600); err != nil {
		t.Fatal(err)
	}
	return root
}

func runCLIGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, output)
	}
}
