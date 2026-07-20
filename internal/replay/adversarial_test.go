package replay

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/rrrrrredy/beforedone/internal/model"
	"github.com/rrrrrredy/beforedone/internal/repository"
)

func TestReplayRedactionRemovesJSONSecrets(t *testing.T) {
	output := sanitizeOutput(`{"api_key":"replay-json-secret","note":"safe"}`, nil)
	if strings.Contains(output, "replay-json-secret") {
		t.Fatalf("JSON secret survived replay redaction: %q", output)
	}
}

func TestAnalyzeAndDryRunExecuteNoCommands(t *testing.T) {
	repo := newReplayTestRepo(t)
	importedMarker := filepath.Join(t.TempDir(), "imported-ran")
	configuredMarker := filepath.Join(t.TempDir(), "configured-ran")
	casePath := writeReplayCase(t, repo, validReplayCase([][]string{replayHelperArgv("touch", importedMarker)}), nil)
	cfg := replayTestConfig(replayHelperArgv("touch", configuredMarker))

	analysis, err := Analyze(repo, casePath)
	if err != nil {
		t.Fatal(err)
	}
	if analysis.ExternalRuns != 0 {
		t.Fatalf("analyze external runs = %d, want 0", analysis.ExternalRuns)
	}
	assertReplayFileAbsent(t, importedMarker)
	assertReplayFileAbsent(t, configuredMarker)

	verification, err := Verify(repo, cfg, casePath, "", false)
	if err != nil {
		t.Fatal(err)
	}
	if !verification.DryRun || len(verification.Results) != 0 {
		t.Fatalf("dry-run verification = %+v", verification)
	}
	assertReplayFileAbsent(t, importedMarker)
	assertReplayFileAbsent(t, configuredMarker)
}

func TestExecuteUsesCurrentConfigAndIgnoresImportedArgv(t *testing.T) {
	repo := newReplayTestRepo(t)
	importedMarker := filepath.Join(t.TempDir(), "imported-ran")
	configuredMarker := filepath.Join(t.TempDir(), "configured-ran")
	casePath := writeReplayCase(t, repo, validReplayCase([][]string{replayHelperArgv("touch", importedMarker)}), nil)
	cfg := replayTestConfig(replayHelperArgv("touch", configuredMarker))

	verification, err := Verify(repo, cfg, casePath, "", true)
	if err != nil {
		t.Fatal(err)
	}
	if verification.DryRun || verification.Verdict != model.Pass {
		t.Fatalf("executed verification = %+v", verification)
	}
	if _, err := os.Stat(configuredMarker); err != nil {
		t.Fatalf("current-config command did not run: %v", err)
	}
	assertReplayFileAbsent(t, importedMarker)
}

func TestLoadCaseRejectsUnknownFields(t *testing.T) {
	repo := newReplayTestRepo(t)
	replayCase := validReplayCase(nil)
	data, err := json.Marshal(replayCase)
	if err != nil {
		t.Fatal(err)
	}
	var object map[string]any
	if err := json.Unmarshal(data, &object); err != nil {
		t.Fatal(err)
	}
	object["execute_argv"] = []string{"attacker", "controlled"}
	path := writeReplayCase(t, repo, nil, object)
	if _, _, err := LoadCase(repo, path); err == nil {
		t.Fatal("replay import accepted a field forbidden by replay-case.schema.json")
	}
}

func TestLoadCaseRejectsStructurallyInvalidDivergence(t *testing.T) {
	repo := newReplayTestRepo(t)
	tests := []struct {
		name string
		div  model.Divergence
	}{
		{"exact without event", model.Divergence{Precision: model.ExactEvent, Reason: "missing id"}},
		{"window without boundaries", model.Divergence{Precision: model.TimeWindow, Reason: "missing bounds"}},
		{"unlocated with exact event", model.Divergence{Precision: model.Unlocated, EventID: "contradiction", Reason: "conflicting fields"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			replayCase := validReplayCase(nil)
			replayCase.Divergence = tt.div
			path := writeReplayCase(t, repo, replayCase, nil)
			if _, _, err := LoadCase(repo, path); err == nil {
				t.Fatalf("accepted invalid divergence: %+v", tt.div)
			}
		})
	}
}

func TestVerifyExecuteRedactsCapturedOutput(t *testing.T) {
	repo := newReplayTestRepo(t)
	casePath := writeReplayCase(t, repo, validReplayCase(nil), nil)
	cfg := replayTestConfig(replayHelperArgv("print-secret"))
	cfg.Capture.RedactPatterns = []string{`(?i)token\s*=\s*[^\s]+`}
	verification, err := Verify(repo, cfg, casePath, "", true)
	if err != nil {
		t.Fatal(err)
	}
	if len(verification.Results) != 1 {
		t.Fatalf("results = %d, want 1", len(verification.Results))
	}
	if strings.Contains(verification.Results[0].Output, "replay-secret-value") {
		t.Fatalf("secret leaked into replay result: %q", verification.Results[0].Output)
	}
}

func TestVerifyExecuteRedactsQuotedCredentialDelimiterTails(t *testing.T) {
	repo := newReplayTestRepo(t)
	casePath := writeReplayCase(t, repo, validReplayCase(nil), nil)
	cfg := replayTestConfig(replayHelperArgv("print-delimited-secret"))
	verification, err := Verify(repo, cfg, casePath, "", true)
	if err != nil {
		t.Fatal(err)
	}
	if len(verification.Results) != 1 {
		t.Fatalf("results = %d, want 1", len(verification.Results))
	}
	for _, secret := range []string{"REPLAY-COMMA-TAIL-LEAK", "REPLAY-SEMI-TAIL-LEAK", "REPLAY-DIGEST-LEAK", "REPLAY-PERCENT-LEAK", "REPLAY-UNICODE-LEAK"} {
		if strings.Contains(verification.Results[0].Output, secret) {
			t.Fatalf("secret %q leaked into replay result: %q", secret, verification.Results[0].Output)
		}
	}
}

func TestVerifyExecuteBoundsCapturedOutputBeforeSanitizing(t *testing.T) {
	repo := newReplayTestRepo(t)
	casePath := writeReplayCase(t, repo, validReplayCase(nil), nil)
	cfg := replayTestConfig(replayHelperArgv("print-large"))
	cfg.Capture.MaxOutputBytes = 1024
	verification, err := Verify(repo, cfg, casePath, "", true)
	if err != nil {
		t.Fatal(err)
	}
	if len(verification.Results) != 1 {
		t.Fatalf("results = %d, want 1", len(verification.Results))
	}
	output := verification.Results[0].Output
	if len(output) > 1200 {
		t.Fatalf("bounded replay output is %d bytes", len(output))
	}
	if !strings.Contains(output, "output truncated by capture.max_output_bytes") {
		t.Fatalf("bounded replay output omitted truncation marker: %q", output)
	}
}

func TestLimitedOutputNeverRetainsMoreThanLimit(t *testing.T) {
	buffer := &limitedOutput{limit: 8}
	payload := strings.Repeat("x", 2<<20)
	n, err := buffer.Write([]byte(payload))
	if err != nil || n != len(payload) {
		t.Fatalf("Write = (%d, %v), want (%d, nil)", n, err, len(payload))
	}
	if got := len(buffer.String()); got != 8 || !buffer.truncated {
		t.Fatalf("limited output = (%d bytes, truncated=%v), want (8, true)", got, buffer.truncated)
	}
}

func TestReplayAdversarialHelperProcess(t *testing.T) {
	separator := -1
	for i, arg := range os.Args {
		if arg == "--" {
			separator = i
			break
		}
	}
	if separator < 0 || separator+1 >= len(os.Args) {
		return
	}
	switch os.Args[separator+1] {
	case "touch":
		if separator+2 >= len(os.Args) {
			os.Exit(97)
		}
		if err := os.WriteFile(os.Args[separator+2], []byte("executed"), 0o600); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(98)
		}
	case "print-secret":
		fmt.Fprintln(os.Stdout, "token=replay-secret-value")
	case "print-delimited-secret":
		fmt.Fprintln(os.Stdout, `{"password":"prefix,REPLAY-COMMA-TAIL-LEAK"}`)
		fmt.Fprintln(os.Stderr, `{'token':'prefix;REPLAY-SEMI-TAIL-LEAK'}`)
		fmt.Fprintln(os.Stdout, `Authorization: Digest username="alice", response="REPLAY-DIGEST-LEAK"`)
		fmt.Fprintln(os.Stdout, `%22token%22%3D%22REPLAY-PERCENT-LEAK%22`)
		fmt.Fprintln(os.Stdout, `{"pass\u0077ord":"REPLAY-UNICODE-LEAK"}`)
	case "print-large":
		fmt.Fprint(os.Stdout, strings.Repeat("x", 2<<20))
	default:
		os.Exit(99)
	}
	os.Exit(0)
}

func validReplayCase(observed [][]string) *model.ReplayCase {
	return &model.ReplayCase{
		SchemaVersion: 1,
		ID:            "replay-test",
		CreatedAt:     time.Now().UTC(),
		IncidentID:    "incident-test",
		Verdict:       model.Inconclusive,
		Divergence: model.Divergence{
			Precision: model.Unlocated,
			Reason:    "insufficient observable evidence",
		},
		ObservedCommands: observed,
	}
}

func replayTestConfig(argv []string) model.Config {
	return model.Config{
		SchemaVersion: 1,
		Checks: map[string]model.CheckConfig{
			"unit": {Argv: argv, RelevantFiles: []string{"**/*"}, WorkingDirectory: ".", TimeoutSeconds: 120},
		},
	}
}

func replayHelperArgv(mode string, extra ...string) []string {
	argv := []string{os.Args[0], "-test.run=^TestReplayAdversarialHelperProcess$", "--", mode}
	return append(argv, extra...)
}

func writeReplayCase(t *testing.T, repo *repository.Repository, replayCase *model.ReplayCase, object map[string]any) string {
	t.Helper()
	var data []byte
	var err error
	if object != nil {
		data, err = json.Marshal(object)
	} else {
		data, err = json.Marshal(replayCase)
	}
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "replay-case.json")
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func assertReplayFileAbsent(t *testing.T, path string) {
	t.Helper()
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("unexpected command side effect at %s (err=%v)", path, err)
	}
}

func newReplayTestRepo(t *testing.T) *repository.Repository {
	t.Helper()
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "README.md"), []byte("fixture\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	runReplayGit(t, root, "init", "-q")
	runReplayGit(t, root, "config", "user.email", "test@example.invalid")
	runReplayGit(t, root, "config", "user.name", "BeforeDone Test")
	runReplayGit(t, root, "add", ".")
	runReplayGit(t, root, "commit", "-m", "fixture")
	repo, err := repository.Discover(root)
	if err != nil {
		t.Fatal(err)
	}
	return repo
}

func runReplayGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, output)
	}
}
