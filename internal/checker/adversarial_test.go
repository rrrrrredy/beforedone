package checker

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/rrrrrredy/beforedone/internal/evidence"
	"github.com/rrrrrredy/beforedone/internal/model"
	"github.com/rrrrrredy/beforedone/internal/repository"
)

func TestDefaultRedactorsRemoveJSONSecrets(t *testing.T) {
	redactors, err := compileRedactors(nil)
	if err != nil {
		t.Fatal(err)
	}
	output := redact(`{"password":"checker-json-secret","note":"safe"}`, redactors)
	if strings.Contains(output, "checker-json-secret") {
		t.Fatalf("JSON secret survived check-output redaction: %q", output)
	}
}

func TestCheckLogRedactsQuotedCredentialDelimiterTails(t *testing.T) {
	repo := newCheckerTestRepo(t)
	cfg := checkerTestConfig(helperArgv("print-delimited-secret"))
	result, err := Run(repo, cfg, "unit")
	if err != nil {
		t.Fatal(err)
	}
	logData, err := os.ReadFile(filepath.Join(repo.RuntimeDir, filepath.FromSlash(result.Receipt.LogPath)))
	if err != nil {
		t.Fatal(err)
	}
	stored := string(logData)
	for _, secret := range []string{"CHECK-COMMA-TAIL-LEAK", "CHECK-SEMI-TAIL-LEAK", "CHECK-DIGEST-LEAK", "CHECK-PERCENT-LEAK", "CHECK-UNICODE-LEAK"} {
		if strings.Contains(stored, secret) {
			t.Fatalf("check log retained %q: %s", secret, stored)
		}
	}
}

func TestOutputContainingPASSCannotOverrideNonzeroExit(t *testing.T) {
	repo := newCheckerTestRepo(t)
	argv := helperArgv("print-pass-and-fail")
	cfg := checkerTestConfig(argv)
	result, err := Run(repo, cfg, "unit")
	if err != nil {
		t.Fatal(err)
	}
	if result.Receipt.Verdict != model.Fail {
		t.Fatalf("verdict = %s, want FAIL", result.Receipt.Verdict)
	}
	if result.Receipt.ExitCode == 0 {
		t.Fatal("nonzero helper exit was recorded as zero")
	}
	if !strings.Contains(result.Receipt.StdoutSummary, "PASS") {
		t.Fatalf("fixture did not exercise PASS log text: %q", result.Receipt.StdoutSummary)
	}
	if fresh, _ := evidence.ValidateFresh(repo, cfg, result.Receipt); fresh {
		t.Fatal("FAIL receipt was accepted as a fresh PASS")
	}
}

func TestMissingExecutableProducesInconclusiveReceipt(t *testing.T) {
	repo := newCheckerTestRepo(t)
	cfg := checkerTestConfig([]string{"beforedone-definitely-missing-executable-8e74c5"})
	result, err := Run(repo, cfg, "unit")
	if err != nil {
		t.Fatal(err)
	}
	if result.Receipt.Verdict != model.Inconclusive {
		t.Fatalf("verdict = %s, want INCONCLUSIVE", result.Receipt.Verdict)
	}
	if result.Receipt.ExitCode != -1 {
		t.Fatalf("exit_code = %d, want -1", result.Receipt.ExitCode)
	}
	if !strings.Contains(result.Receipt.Error, "could not start") {
		t.Fatalf("missing actionable error: %q", result.Receipt.Error)
	}
}

func TestRelevantMutationDuringCheckCannotProducePASS(t *testing.T) {
	repo := newCheckerTestRepo(t)
	target := filepath.Join(repo.Root, "main.go")
	cfg := checkerTestConfig(helperArgv("mutate-and-pass", target))
	result, err := Run(repo, cfg, "unit")
	if err != nil {
		t.Fatal(err)
	}
	if result.Receipt.ExitCode != 0 {
		t.Fatalf("helper exit_code = %d, want 0", result.Receipt.ExitCode)
	}
	if result.Receipt.Verdict != model.Inconclusive {
		t.Fatalf("verdict = %s, want INCONCLUSIVE after in-flight mutation", result.Receipt.Verdict)
	}
	if !strings.Contains(result.Receipt.Error, "changed while") {
		t.Fatalf("missing mutation reason: %q", result.Receipt.Error)
	}
}

func TestAdversarialHelperProcess(t *testing.T) {
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
	case "print-pass-and-fail":
		fmt.Fprintln(os.Stdout, "PASS — this text is not evidence")
		os.Exit(1)
	case "print-delimited-secret":
		fmt.Fprintln(os.Stdout, `{"password":"prefix,CHECK-COMMA-TAIL-LEAK"}`)
		fmt.Fprintln(os.Stderr, `{'token':'prefix;CHECK-SEMI-TAIL-LEAK'}`)
		fmt.Fprintln(os.Stdout, `Authorization: Digest username="alice", response="CHECK-DIGEST-LEAK"`)
		fmt.Fprintln(os.Stdout, `%22token%22%3D%22CHECK-PERCENT-LEAK%22`)
		fmt.Fprintln(os.Stdout, `{"pass\u0077ord":"CHECK-UNICODE-LEAK"}`)
		os.Exit(0)
	case "mutate-and-pass":
		if separator+2 >= len(os.Args) {
			os.Exit(97)
		}
		if err := os.WriteFile(os.Args[separator+2], []byte("package main\n// changed by verifier\n"), 0o600); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(98)
		}
		os.Exit(0)
	default:
		os.Exit(99)
	}
}

func helperArgv(mode string, extra ...string) []string {
	argv := []string{os.Args[0], "-test.run=^TestAdversarialHelperProcess$", "--", mode}
	return append(argv, extra...)
}

func checkerTestConfig(argv []string) model.Config {
	return model.Config{
		SchemaVersion: 1,
		Checks: map[string]model.CheckConfig{
			"unit": {Argv: argv, RelevantFiles: []string{"**/*.go"}, WorkingDirectory: ".", TimeoutSeconds: 120},
		},
		Capture: model.CaptureConfig{MaxOutputBytes: 1 << 20},
	}
}

func newCheckerTestRepo(t *testing.T) *repository.Repository {
	t.Helper()
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "main.go"), []byte("package main\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	runCheckerGit(t, root, "init", "-q")
	runCheckerGit(t, root, "config", "user.email", "test@example.invalid")
	runCheckerGit(t, root, "config", "user.name", "BeforeDone Test")
	runCheckerGit(t, root, "add", ".")
	runCheckerGit(t, root, "commit", "-m", "fixture")
	repo, err := repository.Discover(root)
	if err != nil {
		t.Fatal(err)
	}
	return repo
}

func runCheckerGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, output)
	}
}
