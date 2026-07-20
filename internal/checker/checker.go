package checker

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/rrrrrredy/beforedone/internal/evidence"
	"github.com/rrrrrredy/beforedone/internal/model"
	redaction "github.com/rrrrrredy/beforedone/internal/redact"
	"github.com/rrrrrredy/beforedone/internal/repository"
)

var Version = "dev"

type Result struct {
	Receipt *model.Receipt
	Path    string
}

func Run(repo *repository.Repository, cfg model.Config, checkID string) (*Result, error) {
	check, ok := cfg.Checks[checkID]
	if !ok {
		return nil, fmt.Errorf("unknown check %q", checkID)
	}
	if err := repo.EnsureRuntime(); err != nil {
		return nil, err
	}
	if err := evidence.EnsureKey(repo); err != nil {
		return nil, err
	}
	before, _, err := evidence.Fingerprint(repo, check.RelevantFiles)
	if err != nil {
		return nil, err
	}

	limit := cfg.Capture.MaxOutputBytes
	if limit == 0 {
		limit = 1 << 20
	}
	timeout := check.TimeoutSeconds
	if timeout == 0 {
		timeout = 600
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(timeout)*time.Second)
	defer cancel()

	wd := check.WorkingDirectory
	if wd == "" {
		wd = "."
	}
	resolvedWD, err := repository.ResolveDirectoryWithin(repo.Root, filepath.Join(repo.Root, wd))
	if err != nil {
		return nil, err
	}
	started := time.Now().UTC()
	stdout := &limitedBuffer{limit: limit}
	stderr := &limitedBuffer{limit: limit}
	cmd := exec.CommandContext(ctx, check.Argv[0], check.Argv[1:]...)
	cmd.Dir = resolvedWD
	cmd.Stdout = stdout
	cmd.Stderr = stderr
	runErr := cmd.Run()
	finished := time.Now().UTC()

	exitCode := 0
	verdict := model.Pass
	errorText := ""
	if runErr != nil {
		var exitErr *exec.ExitError
		switch {
		case errors.Is(ctx.Err(), context.DeadlineExceeded):
			exitCode = 124
			verdict = model.Inconclusive
			errorText = fmt.Sprintf("check timed out after %d seconds", timeout)
		case errors.As(runErr, &exitErr):
			exitCode = exitErr.ExitCode()
			verdict = model.Fail
		default:
			exitCode = -1
			verdict = model.Inconclusive
			errorText = "could not start check: " + runErr.Error()
		}
	}

	after, count, fingerprintErr := evidence.Fingerprint(repo, check.RelevantFiles)
	if fingerprintErr != nil {
		return nil, fingerprintErr
	}
	if before != after {
		verdict = model.Inconclusive
		errorText = "relevant files changed while the check was running"
	}
	commit, commitErr := repo.Git("rev-parse", "HEAD")
	if commitErr != nil {
		commit = "UNBORN"
	}

	redactors, err := compileRedactors(cfg.Capture.RedactPatterns)
	if err != nil {
		return nil, err
	}
	stdoutText := redact(stdout.String(), redactors)
	stderrText := redact(stderr.String(), redactors)
	if stdout.truncated {
		stdoutText += "\n[output truncated by BeforeDone]"
	}
	if stderr.truncated {
		stderrText += "\n[output truncated by BeforeDone]"
	}
	logData := []byte("[stdout]\n" + stdoutText + "\n[stderr]\n" + stderrText + "\n")
	logName := evidence.NewID("check") + ".log"
	logRel := filepath.ToSlash(filepath.Join("logs", logName))
	logPath := filepath.Join(repo.RuntimeDir, filepath.FromSlash(logRel))
	if err := os.WriteFile(logPath, logData, 0o600); err != nil {
		return nil, err
	}
	logSum := sha256.Sum256(logData)
	receipt := &model.Receipt{
		SchemaVersion:       model.SchemaVersion,
		ID:                  evidence.NewID("receipt"),
		Producer:            "beforedone.check",
		CheckID:             checkID,
		Argv:                append([]string(nil), check.Argv...),
		WorkingDirectory:    filepath.ToSlash(filepath.Clean(wd)),
		StartedAt:           started,
		FinishedAt:          finished,
		ExitCode:            exitCode,
		Verdict:             verdict,
		GitCommit:           commit,
		RelevantFingerprint: after,
		RelevantFileCount:   count,
		StdoutSummary:       summarize(stdoutText),
		StderrSummary:       summarize(stderrText),
		LogPath:             logRel,
		LogSHA256:           "sha256:" + hex.EncodeToString(logSum[:]),
		BeforeDoneVersion:   Version,
		Error:               errorText,
	}
	path, err := evidence.Save(repo, receipt)
	if err != nil {
		return nil, err
	}
	return &Result{Receipt: receipt, Path: path}, nil
}

type limitedBuffer struct {
	data      []byte
	limit     int64
	truncated bool
}

func (b *limitedBuffer) Write(p []byte) (int, error) {
	n := len(p)
	remaining := b.limit - int64(len(b.data))
	if remaining > 0 {
		keep := int64(len(p))
		if keep > remaining {
			keep = remaining
		}
		b.data = append(b.data, p[:keep]...)
	}
	if int64(n) > remaining {
		b.truncated = true
	}
	return n, nil
}

func (b *limitedBuffer) String() string { return string(b.data) }

func compileRedactors(patterns []string) ([]*regexp.Regexp, error) {
	return redaction.Compile(patterns)
}

func redact(value string, redactors []*regexp.Regexp) string {
	return redaction.Apply(value, redactors)
}

func summarize(value string) string {
	value = strings.TrimSpace(value)
	const limit = 4000
	if len(value) <= limit {
		return value
	}
	return value[:limit] + "…"
}
