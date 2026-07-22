package replay

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/rrrrrredy/beforedone/internal/model"
	"github.com/rrrrrredy/beforedone/internal/redact"
	"github.com/rrrrrredy/beforedone/internal/repository"
)

type Analysis struct {
	SchemaVersion int              `json:"schema_version"`
	ReplayCaseID  string           `json:"replay_case_id"`
	IncidentID    string           `json:"incident_id"`
	Verdict       model.Verdict    `json:"verdict"`
	Divergence    model.Divergence `json:"first_observable_divergence"`
	ExternalRuns  int              `json:"external_commands_executed"`
	Statement     string           `json:"statement"`
}

type CheckPlan struct {
	CheckID          string        `json:"check_id"`
	Argv             []string      `json:"argv"`
	WorkingDirectory string        `json:"working_directory"`
	Timeout          time.Duration `json:"timeout"`
}

type CheckResult struct {
	CheckPlan
	Verdict  model.Verdict `json:"verdict"`
	ExitCode int           `json:"exit_code"`
	Output   string        `json:"output,omitempty"`
	Error    string        `json:"error,omitempty"`
}

type Verification struct {
	SchemaVersion int           `json:"schema_version"`
	ReplayCaseID  string        `json:"replay_case_id"`
	DryRun        bool          `json:"dry_run"`
	Source        string        `json:"command_source"`
	GitCommit     string        `json:"git_commit"`
	Plans         []CheckPlan   `json:"plans"`
	Results       []CheckResult `json:"results,omitempty"`
	Verdict       model.Verdict `json:"verdict"`
}

func LoadCase(repo *repository.Repository, path string) (*model.ReplayCase, string, error) {
	if path == "" {
		path = filepath.Join(repo.RuntimeDir, "latest-replay-case.json")
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, path, err
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, 4<<20+1))
	if err != nil {
		return nil, path, err
	}
	if len(data) > 4<<20 {
		return nil, path, errors.New("replay case exceeds 4 MiB")
	}
	var replayCase model.ReplayCase
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&replayCase); err != nil {
		return nil, path, fmt.Errorf("parse replay case: %w", err)
	}
	var trailing any
	if err := dec.Decode(&trailing); !errors.Is(err, io.EOF) {
		if err == nil {
			return nil, path, errors.New("replay case contains trailing JSON")
		}
		return nil, path, fmt.Errorf("parse replay case trailing content: %w", err)
	}
	if replayCase.SchemaVersion != model.SchemaVersion || replayCase.ID == "" || !replayCase.Verdict.Valid() {
		return nil, path, errors.New("invalid replay case contract")
	}
	switch replayCase.Divergence.Precision {
	case model.ExactEvent:
		if replayCase.Divergence.EventID == "" || replayCase.Divergence.StartEvent != "" || replayCase.Divergence.EndEvent != "" {
			return nil, path, errors.New("exact_event divergence requires only event_id")
		}
	case model.TimeWindow:
		if replayCase.Divergence.EventID != "" || replayCase.Divergence.StartEvent == "" || replayCase.Divergence.EndEvent == "" {
			return nil, path, errors.New("time_window divergence requires start_event and end_event")
		}
	case model.Unlocated:
		if replayCase.Divergence.EventID != "" || replayCase.Divergence.StartEvent != "" || replayCase.Divergence.EndEvent != "" {
			return nil, path, errors.New("unlocated divergence cannot identify events")
		}
	default:
		return nil, path, errors.New("invalid first observable divergence precision")
	}
	if strings.TrimSpace(replayCase.Divergence.Reason) == "" {
		return nil, path, errors.New("first observable divergence reason is required")
	}
	return &replayCase, path, nil
}

func Analyze(repo *repository.Repository, path string) (*Analysis, error) {
	replayCase, _, err := LoadCase(repo, path)
	if err != nil {
		return nil, err
	}
	return &Analysis{
		SchemaVersion: model.SchemaVersion,
		ReplayCaseID:  replayCase.ID,
		IncidentID:    replayCase.IncidentID,
		Verdict:       replayCase.Verdict,
		Divergence:    replayCase.Divergence,
		ExternalRuns:  0,
		Statement:     "Evidence-only analysis completed. No verifier or imported command was executed.",
	}, nil
}

func Verify(repo *repository.Repository, cfg model.Config, path, selected string, execute bool) (*Verification, error) {
	replayCase, _, err := LoadCase(repo, path)
	if err != nil {
		return nil, err
	}
	ids := make([]string, 0, len(cfg.Checks))
	for id := range cfg.Checks {
		if selected == "" || id == selected {
			ids = append(ids, id)
		}
	}
	if selected != "" {
		if _, ok := cfg.Checks[selected]; !ok {
			return nil, fmt.Errorf("unknown check %q", selected)
		}
	}
	sort.Strings(ids)
	verification := &Verification{
		SchemaVersion: model.SchemaVersion,
		ReplayCaseID:  replayCase.ID,
		DryRun:        !execute,
		Source:        "current .beforedone.yaml (imported case commands ignored)",
		Verdict:       model.Inconclusive,
	}
	for _, id := range ids {
		check := cfg.Checks[id]
		wd := check.WorkingDirectory
		if wd == "" {
			wd = "."
		}
		timeout := check.TimeoutSeconds
		if timeout == 0 {
			timeout = 600
		}
		verification.Plans = append(verification.Plans, CheckPlan{CheckID: id, Argv: append([]string(nil), check.Argv...), WorkingDirectory: wd, Timeout: time.Duration(timeout) * time.Second})
	}
	commit, err := repo.Git("rev-parse", "HEAD")
	if err != nil {
		return nil, err
	}
	verification.GitCommit = commit
	if !execute {
		return verification, nil
	}

	parent, err := os.MkdirTemp("", "beforedone-replay-")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(parent)
	disabledHooks := filepath.Join(parent, "disabled-hooks")
	if err := os.Mkdir(disabledHooks, 0o700); err != nil {
		return nil, fmt.Errorf("create disabled hooks directory: %w", err)
	}
	worktree := filepath.Join(parent, "worktree")
	if _, err := repo.Git("-c", "core.hooksPath="+disabledHooks, "worktree", "add", "--detach", worktree, "HEAD"); err != nil {
		return nil, fmt.Errorf("create detached replay worktree: %w", err)
	}
	defer repo.Git("worktree", "remove", "--force", worktree)

	overall := model.Pass
	captureLimit := cfg.Capture.MaxOutputBytes
	if captureLimit <= 0 {
		captureLimit = 1 << 20
	}
	for _, plan := range verification.Plans {
		result := runPlan(worktree, plan, cfg.Capture.RedactPatterns, captureLimit)
		verification.Results = append(verification.Results, result)
		if result.Verdict == model.Fail {
			overall = model.Fail
		} else if result.Verdict == model.Inconclusive && overall != model.Fail {
			overall = model.Inconclusive
		}
	}
	verification.Verdict = overall
	return verification, nil
}

func runPlan(worktree string, plan CheckPlan, redactPatterns []string, captureLimit int64) CheckResult {
	result := CheckResult{CheckPlan: plan, Verdict: model.Pass}
	workingDirectory, err := repository.ResolveDirectoryWithin(worktree, filepath.Join(worktree, plan.WorkingDirectory))
	if err != nil {
		result.Verdict = model.Inconclusive
		result.ExitCode = -1
		result.Error = err.Error()
		return result
	}
	ctx, cancel := context.WithTimeout(context.Background(), plan.Timeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, plan.Argv[0], plan.Argv[1:]...)
	cmd.Dir = workingDirectory
	output := &limitedOutput{limit: captureLimit}
	cmd.Stdout = output
	cmd.Stderr = output
	err = cmd.Run()
	rawOutput := output.String()
	if output.truncated {
		rawOutput += "\n[output truncated by capture.max_output_bytes]"
	}
	result.Output = sanitizeOutput(rawOutput, redactPatterns)
	if err == nil {
		return result
	}
	var exitErr *exec.ExitError
	switch {
	case errors.Is(ctx.Err(), context.DeadlineExceeded):
		result.Verdict = model.Inconclusive
		result.ExitCode = 124
		result.Error = "verification timed out"
	case errors.As(err, &exitErr):
		result.Verdict = model.Fail
		result.ExitCode = exitErr.ExitCode()
	case err != nil:
		result.Verdict = model.Inconclusive
		result.ExitCode = -1
		result.Error = err.Error()
	}
	return result
}

type limitedOutput struct {
	data      bytes.Buffer
	limit     int64
	truncated bool
}

func (b *limitedOutput) Write(p []byte) (int, error) {
	n := len(p)
	remaining := b.limit - int64(b.data.Len())
	if remaining > 0 {
		keep := int64(len(p))
		if keep > remaining {
			keep = remaining
		}
		_, _ = b.data.Write(p[:int(keep)])
	}
	if int64(n) > remaining {
		b.truncated = true
	}
	return n, nil
}

func (b *limitedOutput) String() string { return b.data.String() }

func sanitizeOutput(value string, configured []string) string {
	value = redact.BestEffort(value, configured)
	value = strings.TrimSpace(value)
	const limit = 16000
	if len(value) > limit {
		value = value[:limit] + "…"
	}
	return value
}
