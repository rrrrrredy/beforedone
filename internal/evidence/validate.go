package evidence

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/rrrrrredy/beforedone/internal/model"
	"github.com/rrrrrredy/beforedone/internal/repository"
)

type FingerprintCache struct {
	results map[string]fingerprintResult
}

type fingerprintResult struct {
	fingerprint string
	count       int
	err         error
}

func NewFingerprintCache() *FingerprintCache {
	return &FingerprintCache{results: map[string]fingerprintResult{}}
}

func (c *FingerprintCache) Fingerprint(ctx context.Context, repo *repository.Repository, patterns []string) (string, int, error) {
	if c == nil {
		return FingerprintContext(ctx, repo, patterns)
	}
	key := strings.Join(patterns, "\x00")
	if result, ok := c.results[key]; ok {
		return result.fingerprint, result.count, result.err
	}
	fingerprint, count, err := FingerprintContext(ctx, repo, patterns)
	c.results[key] = fingerprintResult{fingerprint: fingerprint, count: count, err: err}
	return fingerprint, count, err
}

func ValidateFresh(repo *repository.Repository, cfg model.Config, receipt *model.Receipt) (bool, string) {
	return ValidateFreshContext(context.Background(), repo, cfg, receipt, NewFingerprintCache())
}

func ValidateFreshContext(ctx context.Context, repo *repository.Repository, cfg model.Config, receipt *model.Receipt, cache *FingerprintCache) (bool, string) {
	fresh, reason := ValidateBindingContext(ctx, repo, cfg, receipt, cache)
	if !fresh {
		return false, reason
	}
	if receipt.Verdict != model.Pass {
		return false, fmt.Sprintf("latest check verdict is %s", receipt.Verdict)
	}
	return true, ""
}

func ValidateBinding(repo *repository.Repository, cfg model.Config, receipt *model.Receipt) (bool, string) {
	return ValidateBindingContext(context.Background(), repo, cfg, receipt, NewFingerprintCache())
}

func ValidateBindingContext(ctx context.Context, repo *repository.Repository, cfg model.Config, receipt *model.Receipt, cache *FingerprintCache) (bool, string) {
	if err := VerifySignature(repo, receipt); err != nil {
		return false, "invalid receipt: " + err.Error()
	}
	check, ok := cfg.Checks[receipt.CheckID]
	if !ok {
		return false, "check no longer exists in current configuration"
	}
	if !equalStrings(check.Argv, receipt.Argv) {
		return false, "check argv changed since evidence was collected"
	}
	wd := check.WorkingDirectory
	if wd == "" {
		wd = "."
	}
	if filepath.Clean(wd) != filepath.Clean(receipt.WorkingDirectory) {
		return false, "check working directory changed since evidence was collected"
	}
	fingerprint, _, err := cache.Fingerprint(ctx, repo, check.RelevantFiles)
	if err != nil {
		return false, "cannot recompute relevant-file fingerprint: " + err.Error()
	}
	if fingerprint != receipt.RelevantFingerprint {
		return false, "relevant files changed after the check"
	}
	if receipt.LogPath == "" {
		return false, "receipt log is missing"
	}
	logPath := filepath.Join(repo.RuntimeDir, filepath.FromSlash(receipt.LogPath))
	if !repository.IsWithin(repo.RuntimeDir, logPath) {
		return false, "receipt log path escapes runtime directory"
	}
	logPath, err = repository.ResolveFileWithin(repo.RuntimeDir, logPath)
	if err != nil {
		return false, "receipt log path is unsafe: " + err.Error()
	}
	actual, err := hashFileContext(ctx, logPath)
	if err != nil {
		return false, "receipt log cannot be verified: " + err.Error()
	}
	if actual != receipt.LogSHA256 {
		return false, "receipt log hash does not match"
	}
	return true, ""
}

func hashFile(path string) (string, error) {
	return hashFileContext(context.Background(), path)
}

func hashFileContext(ctx context.Context, path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	buffer := make([]byte, 256*1024)
	for {
		if err := ctx.Err(); err != nil {
			return "", err
		}
		n, readErr := f.Read(buffer)
		if n > 0 {
			if _, err := h.Write(buffer[:n]); err != nil {
				return "", err
			}
		}
		if readErr == io.EOF {
			break
		}
		if readErr != nil {
			return "", readErr
		}
	}
	return "sha256:" + hex.EncodeToString(h.Sum(nil)), nil
}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
