package evidence

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/rrrrrredy/beforedone/internal/model"
	"github.com/rrrrrredy/beforedone/internal/repository"
)

func TestFingerprintCacheReusesIdenticalRelevantFileSet(t *testing.T) {
	repo := newEvidenceTestRepo(t)
	cache := NewFingerprintCache()
	patterns := []string{"**/*.go"}
	want, wantCount, err := cache.Fingerprint(context.Background(), repo, patterns)
	if err != nil {
		t.Fatal(err)
	}
	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	got, gotCount, err := cache.Fingerprint(cancelled, repo, patterns)
	if err != nil {
		t.Fatalf("cached fingerprint unexpectedly recomputed after cancellation: %v", err)
	}
	if got != want || gotCount != wantCount {
		t.Fatalf("cached fingerprint = (%q, %d), want (%q, %d)", got, gotCount, want, wantCount)
	}
}

func TestIgnoredRelevantFileStillInvalidatesFingerprint(t *testing.T) {
	repo := newEvidenceTestRepo(t)
	if err := os.WriteFile(filepath.Join(repo.Root, ".gitignore"), []byte("generated.txt\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	generated := filepath.Join(repo.Root, "generated.txt")
	if err := os.WriteFile(generated, []byte("first\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	first, count, err := Fingerprint(repo, []string{"generated.txt"})
	if err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("ignored relevant file count = %d, want 1", count)
	}
	if err := os.WriteFile(generated, []byte("second\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	second, _, err := Fingerprint(repo, []string{"generated.txt"})
	if err != nil {
		t.Fatal(err)
	}
	if first == second {
		t.Fatal("ignored relevant file changed without invalidating fingerprint")
	}
}

func TestBroadGlobIncludesIgnoredGeneratedGoWithoutIncludingIrrelevantIgnoredFiles(t *testing.T) {
	repo := newEvidenceTestRepo(t)
	writeEvidenceFile(t, filepath.Join(repo.Root, ".gitignore"), "generated/\nnode_modules/\n")
	writeEvidenceFile(t, filepath.Join(repo.Root, "main.go"), "package main\n")
	writeEvidenceFile(t, filepath.Join(repo.Root, "generated", "generated.go"), "package generated\n")
	writeEvidenceFile(t, filepath.Join(repo.Root, "node_modules", "dependency.js"), "first\n")

	first, count, err := Fingerprint(repo, []string{"**/*.go"})
	if err != nil {
		t.Fatal(err)
	}
	if count != 2 {
		t.Fatalf("broad Go fingerprint file count = %d, want tracked/untracked main.go plus ignored generated.go", count)
	}
	writeEvidenceFile(t, filepath.Join(repo.Root, "node_modules", "dependency.js"), "second\n")
	unchanged, _, err := Fingerprint(repo, []string{"**/*.go"})
	if err != nil {
		t.Fatal(err)
	}
	if unchanged != first {
		t.Fatal("irrelevant ignored JavaScript changed a **/*.go fingerprint")
	}
	writeEvidenceFile(t, filepath.Join(repo.Root, "generated", "generated.go"), "package generated\n// changed\n")
	changed, _, err := Fingerprint(repo, []string{"**/*.go"})
	if err != nil {
		t.Fatal(err)
	}
	if changed == first {
		t.Fatal("ignored generated.go changed without invalidating **/*.go fingerprint")
	}
}

func TestIgnoredPathspecBatchesPreserveBroadGlobs(t *testing.T) {
	batches := ignoredPathspecBatches([]string{"**/*.go", "generated/*.json"})
	if len(batches) != 1 {
		t.Fatalf("ignoredPathspecBatches produced %d batches, want 1", len(batches))
	}
	got := strings.Join(batches[0], "|")
	want := ":(top,glob)**/*.go|:(top,glob)generated/*.json"
	if got != want {
		t.Fatalf("ignored pathspecs = %q, want %q", got, want)
	}
}

func TestExecutableModeInvalidatesFingerprint(t *testing.T) {
	repo := newEvidenceTestRepo(t)
	path := filepath.Join(repo.Root, "script.sh")
	writeEvidenceFile(t, path, "#!/bin/sh\nexit 0\n")
	if err := os.Chmod(path, 0o644); err != nil {
		t.Fatal(err)
	}
	runEvidenceGit(t, repo.Root, "add", "script.sh")
	first, _, err := Fingerprint(repo, []string{"script.sh"})
	if err != nil {
		t.Fatal(err)
	}
	// update-index makes the Git executable mode observable even on Windows,
	// where os.Chmod does not expose POSIX executable bits.
	runEvidenceGit(t, repo.Root, "update-index", "--chmod=+x", "script.sh")
	second, _, err := Fingerprint(repo, []string{"script.sh"})
	if err != nil {
		t.Fatal(err)
	}
	if first == second {
		t.Fatal("executable mode changed without invalidating fingerprint")
	}
}

func TestFingerprintFailsClosedForPossiblyRelevantGitlink(t *testing.T) {
	repo := newEvidenceTestRepo(t)
	firstCommit, secondCommit := addEvidenceGitlink(t, repo, "vendor/lib")

	for _, patterns := range [][]string{
		{"vendor/lib"},
		{"vendor/lib/main.go"},
		{"**/*.go"},
		{"ven*/lib/**/*.go"},
	} {
		if _, _, err := Fingerprint(repo, patterns); err == nil || !strings.Contains(err.Error(), "submodule contents are not fingerprinted") {
			t.Fatalf("Fingerprint(%q) error = %v, want fail-closed submodule error", patterns, err)
		}
	}

	runEvidenceGit(t, repo.Root, "update-index", "--cacheinfo", "160000,"+secondCommit+",vendor/lib")
	if _, _, err := Fingerprint(repo, []string{"vendor/lib/**"}); err == nil {
		t.Fatal("changed gitlink pointer was allowed to produce reusable evidence")
	}
	runEvidenceGit(t, repo.Root, "update-index", "--cacheinfo", "160000,"+firstCommit+",vendor/lib")
}

func TestFingerprintAllowsUnrelatedGitlink(t *testing.T) {
	repo := newEvidenceTestRepo(t)
	writeEvidenceFile(t, filepath.Join(repo.Root, "docs", "guide.md"), "first\n")
	runEvidenceGit(t, repo.Root, "add", ".")
	runEvidenceGit(t, repo.Root, "commit", "-m", "docs")
	firstCommit, secondCommit := addEvidenceGitlink(t, repo, "vendor/lib")

	first, count, err := Fingerprint(repo, []string{"docs/**"})
	if err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("unrelated-submodule fingerprint count = %d, want 1", count)
	}
	runEvidenceGit(t, repo.Root, "update-index", "--cacheinfo", "160000,"+secondCommit+",vendor/lib")
	second, _, err := Fingerprint(repo, []string{"docs/**"})
	if err != nil {
		t.Fatal(err)
	}
	if first != second {
		t.Fatal("unrelated gitlink pointer invalidated docs-only fingerprint")
	}
	runEvidenceGit(t, repo.Root, "update-index", "--cacheinfo", "160000,"+firstCommit+",vendor/lib")
}

func TestFingerprintFailsClosedForUnmergedGitlink(t *testing.T) {
	repo := newEvidenceTestRepo(t)
	firstCommit, secondCommit := addEvidenceGitlink(t, repo, "vendor/lib")
	runEvidenceGit(t, repo.Root, "update-index", "--force-remove", "vendor/lib")
	indexInfo := "160000 " + firstCommit + " 1\tvendor/lib\n" +
		"160000 " + firstCommit + " 2\tvendor/lib\n" +
		"160000 " + secondCommit + " 3\tvendor/lib\n"
	runEvidenceGitInput(t, repo.Root, indexInfo, "update-index", "--index-info")

	if _, _, err := Fingerprint(repo, []string{"vendor/lib/**"}); err == nil || !strings.Contains(err.Error(), "submodule contents are not fingerprinted") {
		t.Fatalf("unmerged gitlink error = %v, want fail-closed submodule error", err)
	}
}

func TestReceiptBindingTracksOnlyRelevantContents(t *testing.T) {
	repo := newEvidenceTestRepo(t)
	writeEvidenceFile(t, filepath.Join(repo.Root, "src", "main.go"), "package main\n")
	writeEvidenceFile(t, filepath.Join(repo.Root, "README.md"), "first\n")
	runEvidenceGit(t, repo.Root, "add", ".")
	runEvidenceGit(t, repo.Root, "commit", "-m", "fixture")

	cfg := evidenceTestConfig([]string{"**/*.go"})
	receipt := validEvidenceReceipt(t, repo, cfg, model.Pass, 0)
	if fresh, reason := ValidateFresh(repo, cfg, receipt); !fresh {
		t.Fatalf("new PASS receipt is stale: %s", reason)
	}

	writeEvidenceFile(t, filepath.Join(repo.Root, "README.md"), "unrelated edit\n")
	if fresh, reason := ValidateFresh(repo, cfg, receipt); !fresh {
		t.Fatalf("unrelated file invalidated PASS: %s", reason)
	}

	writeEvidenceFile(t, filepath.Join(repo.Root, "src", "main.go"), "package main\n// relevant edit\n")
	if fresh, _ := ValidateFresh(repo, cfg, receipt); fresh {
		t.Fatal("PASS remained fresh after relevant file content changed")
	}
}

func TestForgedPassWithNonzeroExitIsRejected(t *testing.T) {
	repo := newEvidenceTestRepo(t)
	writeEvidenceFile(t, filepath.Join(repo.Root, "main.go"), "package main\n")
	runEvidenceGit(t, repo.Root, "add", ".")
	runEvidenceGit(t, repo.Root, "commit", "-m", "fixture")
	cfg := evidenceTestConfig([]string{"**/*.go"})

	// This models an agent that can read .git/beforedone/receipt.key and compute
	// the same HMAC as BeforeDone. A signature alone must not make the
	// semantically impossible combination exit_code=1/verdict=PASS acceptable.
	receipt := validEvidenceReceipt(t, repo, cfg, model.Pass, 0)
	receipt.ExitCode = 1
	data, err := canonicalReceipt(receipt)
	if err != nil {
		t.Fatal(err)
	}
	key, err := readKey(repo)
	if err != nil {
		t.Fatal(err)
	}
	mac := hmac.New(sha256.New, key)
	_, _ = mac.Write(data)
	receipt.Signature = "hmac-sha256:" + hex.EncodeToString(mac.Sum(nil))
	if fresh, reason := ValidateFresh(repo, cfg, receipt); fresh {
		t.Fatalf("forged PASS with exit_code=1 was accepted (reason=%q)", reason)
	}
}

func TestLoadLatestRejectsCheckIDPathTraversal(t *testing.T) {
	repo := newEvidenceTestRepo(t)
	if err := repo.EnsureRuntime(); err != nil {
		t.Fatal(err)
	}
	for _, checkID := range []string{"../outside", `..\outside`, "nested/outside", `C:\outside`} {
		if receipt, err := LoadLatest(repo, checkID); err == nil || !strings.Contains(err.Error(), "invalid check id") {
			t.Fatalf("LoadLatest(%q) = receipt=%v err=%v, want explicit invalid check id rejection", checkID, receipt, err)
		}
	}
}

func TestLoadLatestRejectsTrailingJSON(t *testing.T) {
	repo := newEvidenceTestRepo(t)
	writeEvidenceFile(t, filepath.Join(repo.Root, "main.go"), "package main\n")
	runEvidenceGit(t, repo.Root, "add", ".")
	runEvidenceGit(t, repo.Root, "commit", "-m", "fixture")
	cfg := evidenceTestConfig([]string{"**/*.go"})
	receipt := validEvidenceReceipt(t, repo, cfg, model.Pass, 0)
	if _, err := Save(repo, receipt); err != nil {
		t.Fatal(err)
	}
	latest := filepath.Join(repo.RuntimeDir, "receipts", "latest-unit.json")
	data, err := os.ReadFile(latest)
	if err != nil {
		t.Fatal(err)
	}
	data = append(data, []byte("{\"verdict\":\"PASS\"}\n")...)
	if err := os.WriteFile(latest, data, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadLatest(repo, "unit"); err == nil || !strings.Contains(err.Error(), "trailing") {
		t.Fatalf("trailing JSON receipt error = %v, want explicit rejection", err)
	}
}

func TestLoadLatestRejectsOversizedReceiptEvenWhenPrefixIsValidJSON(t *testing.T) {
	repo := newEvidenceTestRepo(t)
	if err := repo.EnsureRuntime(); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(repo.RuntimeDir, "receipts", "latest-unit.json")
	data := append([]byte("{}\n"), make([]byte, (2<<20)+1)...)
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadLatest(repo, "unit"); err == nil || !strings.Contains(err.Error(), "exceeds 2 MiB") {
		t.Fatalf("oversized receipt error = %v, want bounded-read rejection", err)
	}
}

func TestReceiptLogSymlinkCannotEscapeRuntime(t *testing.T) {
	repo := newEvidenceTestRepo(t)
	writeEvidenceFile(t, filepath.Join(repo.Root, "main.go"), "package main\n")
	runEvidenceGit(t, repo.Root, "add", ".")
	runEvidenceGit(t, repo.Root, "commit", "-m", "fixture")
	cfg := evidenceTestConfig([]string{"**/*.go"})
	receipt := validEvidenceReceipt(t, repo, cfg, model.Pass, 0)

	outside := filepath.Join(t.TempDir(), "outside.log")
	writeEvidenceFile(t, outside, "attacker controlled\n")
	link := filepath.Join(repo.RuntimeDir, "logs", "escaped.log")
	if err := os.Symlink(outside, link); err != nil {
		t.Skipf("symlinks unavailable on this platform: %v", err)
	}
	receipt.LogPath = filepath.ToSlash(filepath.Join("logs", "escaped.log"))
	receipt.LogSHA256, _ = hashFile(outside)
	if err := Sign(repo, receipt); err != nil {
		t.Fatal(err)
	}
	if fresh, reason := ValidateFresh(repo, cfg, receipt); fresh {
		t.Fatalf("receipt log symlink escaped runtime and was accepted (reason=%q)", reason)
	}
}

func validEvidenceReceipt(t *testing.T, repo *repository.Repository, cfg model.Config, verdict model.Verdict, exitCode int) *model.Receipt {
	t.Helper()
	if err := EnsureKey(repo); err != nil {
		t.Fatal(err)
	}
	check := cfg.Checks["unit"]
	fingerprint, count, err := Fingerprint(repo, check.RelevantFiles)
	if err != nil {
		t.Fatal(err)
	}
	logRel := filepath.ToSlash(filepath.Join("logs", "unit.log"))
	logPath := filepath.Join(repo.RuntimeDir, filepath.FromSlash(logRel))
	writeEvidenceFile(t, logPath, "[stdout]\nok\n[stderr]\n\n")
	logHash, err := hashFile(logPath)
	if err != nil {
		t.Fatal(err)
	}
	commit, err := repo.Git("rev-parse", "HEAD")
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	receipt := &model.Receipt{
		SchemaVersion:       model.SchemaVersion,
		ID:                  "receipt-test",
		Producer:            "beforedone.check",
		CheckID:             "unit",
		Argv:                append([]string(nil), check.Argv...),
		WorkingDirectory:    ".",
		StartedAt:           now.Add(-time.Second),
		FinishedAt:          now,
		ExitCode:            exitCode,
		Verdict:             verdict,
		GitCommit:           commit,
		RelevantFingerprint: fingerprint,
		RelevantFileCount:   count,
		LogPath:             logRel,
		LogSHA256:           logHash,
		BeforeDoneVersion:   "test",
	}
	if err := Sign(repo, receipt); err != nil {
		t.Fatal(err)
	}
	return receipt
}

func evidenceTestConfig(patterns []string) model.Config {
	return model.Config{
		SchemaVersion: 1,
		Checks: map[string]model.CheckConfig{
			"unit": {Argv: []string{"go", "test", "./..."}, RelevantFiles: patterns, WorkingDirectory: "."},
		},
	}
}

func newEvidenceTestRepo(t *testing.T) *repository.Repository {
	t.Helper()
	root := t.TempDir()
	runEvidenceGit(t, root, "init", "-q")
	runEvidenceGit(t, root, "config", "user.email", "test@example.invalid")
	runEvidenceGit(t, root, "config", "user.name", "BeforeDone Test")
	repo, err := repository.Discover(root)
	if err != nil {
		t.Fatal(err)
	}
	return repo
}

func addEvidenceGitlink(t *testing.T, repo *repository.Repository, path string) (string, string) {
	t.Helper()
	writeEvidenceFile(t, filepath.Join(repo.Root, "gitlink-fixture.txt"), "first\n")
	runEvidenceGit(t, repo.Root, "add", "gitlink-fixture.txt")
	runEvidenceGit(t, repo.Root, "commit", "-m", "gitlink fixture one")
	first, err := repo.Git("rev-parse", "HEAD")
	if err != nil {
		t.Fatal(err)
	}
	writeEvidenceFile(t, filepath.Join(repo.Root, "gitlink-fixture.txt"), "second\n")
	runEvidenceGit(t, repo.Root, "add", "gitlink-fixture.txt")
	runEvidenceGit(t, repo.Root, "commit", "-m", "gitlink fixture two")
	second, err := repo.Git("rev-parse", "HEAD")
	if err != nil {
		t.Fatal(err)
	}
	runEvidenceGit(t, repo.Root, "update-index", "--add", "--cacheinfo", "160000,"+first+","+filepath.ToSlash(path))
	return first, second
}

func runEvidenceGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, output)
	}
}

func runEvidenceGitInput(t *testing.T, dir, input string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Stdin = strings.NewReader(input)
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, output)
	}
}

func writeEvidenceFile(t *testing.T, path, contents string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
		t.Fatal(err)
	}
}
