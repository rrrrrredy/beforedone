package cli

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"

	"github.com/rrrrrredy/beforedone/internal/adapter"
	"github.com/rrrrrredy/beforedone/internal/checker"
	"github.com/rrrrrredy/beforedone/internal/config"
	"github.com/rrrrrredy/beforedone/internal/evidence"
	"github.com/rrrrrredy/beforedone/internal/hooks"
	"github.com/rrrrrredy/beforedone/internal/incident"
	"github.com/rrrrrredy/beforedone/internal/model"
	"github.com/rrrrrredy/beforedone/internal/replay"
	"github.com/rrrrrredy/beforedone/internal/repository"
	"github.com/rrrrrredy/beforedone/internal/setup"
)

const (
	ExitOK           = 0
	ExitFail         = 1
	ExitInconclusive = 2
	ExitUsage        = 64
	ExitInternal     = 70
)

type App struct {
	In      io.Reader
	Out     io.Writer
	Err     io.Writer
	CWD     string
	Version string
}

func New() *App {
	cwd, _ := os.Getwd()
	return &App{In: os.Stdin, Out: os.Stdout, Err: os.Stderr, CWD: cwd, Version: "dev"}
}

func (a *App) Run(args []string) int {
	if len(args) == 0 {
		a.help()
		return ExitUsage
	}
	switch args[0] {
	case "help", "--help", "-h":
		a.help()
		return ExitOK
	case "version", "--version":
		fmt.Fprintf(a.Out, "beforedone %s\n", a.Version)
		return ExitOK
	case "init":
		return a.init(args[1:])
	case "doctor":
		return a.doctor(args[1:])
	case "setup":
		return a.setup(args[1:])
	case "check":
		return a.check(args[1:])
	case "receipt":
		return a.receipt(args[1:])
	case "incident":
		return a.incident(args[1:])
	case "replay":
		return a.replay(args[1:])
	case "adapter":
		return a.adapter(args[1:])
	case "licenses":
		return a.licenses(args[1:])
	case "hook":
		return a.hook(args[1:])
	default:
		return a.usageError("unknown command %q", args[0])
	}
}

func (a *App) init(args []string) int {
	jsonOutput, args := consumeBool(args, "--json")
	if len(args) != 0 {
		return a.usageError("usage: beforedone init [--json]")
	}
	repo, code := a.repo()
	if code != ExitOK {
		return code
	}
	path := filepath.Join(repo.Root, config.FileName)
	created := false
	if _, err := os.Stat(path); os.IsNotExist(err) {
		path, err = config.WriteDefault(repo)
		if err != nil {
			return a.usageErr(err)
		}
		created = true
	} else if err != nil {
		return a.usageErr(err)
	} else if _, err := config.Load(repo); err != nil {
		return a.usageErr(err)
	}
	if err := evidence.EnsureKey(repo); err != nil {
		return a.internal(err)
	}
	result := map[string]any{"schema_version": 1, "config_path": path, "config_created": created, "runtime_dir": repo.RuntimeDir}
	if jsonOutput {
		a.writeJSON(result)
	} else {
		verb := "using existing"
		if created {
			verb = "created"
		}
		fmt.Fprintf(a.Out, "Initialized BeforeDone\n  config (%s): %s\n  runtime: %s\n", verb, path, repo.RuntimeDir)
	}
	return ExitOK
}

type doctorCheck struct {
	Name    string        `json:"name"`
	Verdict model.Verdict `json:"verdict"`
	Detail  string        `json:"detail"`
}

func (a *App) doctor(args []string) int {
	jsonOutput, args := consumeBool(args, "--json")
	if len(args) != 0 {
		return a.usageError("usage: beforedone doctor [--json]")
	}
	repo, err := repository.Discover(a.CWD)
	if err != nil {
		return a.outputDoctor(jsonOutput, []doctorCheck{{"git", model.Inconclusive, err.Error()}})
	}
	checks := []doctorCheck{{"git", model.Pass, repo.Root}}
	cfg, cfgErr := config.Load(repo)
	if cfgErr != nil {
		checks = append(checks, doctorCheck{"configuration", model.Inconclusive, cfgErr.Error()})
	} else {
		checks = append(checks, doctorCheck{"configuration", model.Pass, filepath.Join(repo.Root, config.FileName)})
		ids := make([]string, 0, len(cfg.Checks))
		for id := range cfg.Checks {
			ids = append(ids, id)
		}
		sort.Strings(ids)
		for _, id := range ids {
			executable := cfg.Checks[id].Argv[0]
			if path, err := exec.LookPath(executable); err == nil {
				checks = append(checks, doctorCheck{"executable:" + id, model.Pass, path})
			} else {
				checks = append(checks, doctorCheck{"executable:" + id, model.Inconclusive, err.Error()})
			}
		}
	}
	if info, err := os.Stat(repo.RuntimeDir); err == nil && info.IsDir() {
		checks = append(checks, doctorCheck{"runtime", model.Pass, repo.RuntimeDir})
	} else {
		checks = append(checks, doctorCheck{"runtime", model.Inconclusive, "run `beforedone init`"})
	}
	hooksPath := filepath.Join(repo.Root, ".codex", "hooks.json")
	if data, err := os.ReadFile(hooksPath); err == nil && strings.Contains(string(data), "beforedone") {
		checks = append(checks, doctorCheck{"codex-hooks", model.Pass, hooksPath})
	} else {
		checks = append(checks, doctorCheck{"codex-hooks", model.Inconclusive, "optional: run `beforedone setup codex`"})
	}
	return a.outputDoctor(jsonOutput, checks)
}

func (a *App) outputDoctor(jsonOutput bool, checks []doctorCheck) int {
	overall := model.Pass
	for _, check := range checks {
		if check.Verdict != model.Pass && !strings.HasPrefix(check.Name, "codex-hooks") {
			overall = model.Inconclusive
		}
	}
	result := map[string]any{"schema_version": 1, "verdict": overall, "checks": checks}
	if jsonOutput {
		a.writeJSON(result)
	} else {
		for _, check := range checks {
			fmt.Fprintf(a.Out, "%-13s %-24s %s\n", check.Verdict, check.Name, check.Detail)
		}
		fmt.Fprintf(a.Out, "Doctor verdict: %s\n", overall)
	}
	return verdictExit(overall)
}

func (a *App) setup(args []string) int {
	if len(args) == 0 || args[0] != "codex" {
		return a.usageError("usage: beforedone setup codex [--remove] [--json]")
	}
	jsonOutput, rest := consumeBool(args[1:], "--json")
	remove, rest := consumeBool(rest, "--remove")
	if len(rest) != 0 {
		return a.usageError("usage: beforedone setup codex [--remove] [--json]")
	}
	repo, code := a.repo()
	if code != ExitOK {
		return code
	}
	if remove {
		result, err := setup.RemoveCodex(repo)
		if err != nil {
			return a.internal(err)
		}
		if jsonOutput {
			a.writeJSON(result)
		} else if len(result.RemovedEvents) == 0 {
			fmt.Fprintf(a.Out, "No project-local BeforeDone hooks found in %s\n", result.HooksPath)
		} else {
			fmt.Fprintf(a.Out, "Removed project-local BeforeDone hooks from %s\n", result.HooksPath)
		}
		return ExitOK
	}
	if _, err := config.Load(repo); err != nil {
		return a.usageErr(err)
	}
	result, err := setup.Codex(repo)
	if err != nil {
		return a.internal(err)
	}
	if jsonOutput {
		a.writeJSON(result)
	} else {
		fmt.Fprintf(a.Out, "Codex hooks configured: %s\n", result.HooksPath)
		for _, warning := range result.Warnings {
			fmt.Fprintf(a.Out, "Warning: %s\n", warning)
		}
	}
	return ExitOK
}

func (a *App) check(args []string) int {
	jsonOutput, args := consumeBool(args, "--json")
	if len(args) != 1 {
		return a.usageError("usage: beforedone check <check-id> [--json]")
	}
	repo, cfg, code := a.repoConfig()
	if code != ExitOK {
		return code
	}
	checker.Version = a.Version
	result, err := checker.Run(repo, cfg, args[0])
	if err != nil {
		if strings.Contains(err.Error(), "unknown check") {
			return a.usageErr(err)
		}
		return a.internal(err)
	}
	if jsonOutput {
		a.writeJSON(map[string]any{"schema_version": 1, "receipt_path": result.Path, "receipt": result.Receipt})
	} else {
		fmt.Fprintf(a.Out, "%s %s (exit %d)\nReceipt: %s\n", result.Receipt.Verdict, result.Receipt.CheckID, result.Receipt.ExitCode, result.Path)
		if result.Receipt.Error != "" {
			fmt.Fprintln(a.Out, result.Receipt.Error)
		}
	}
	return verdictExit(result.Receipt.Verdict)
}

func (a *App) receipt(args []string) int {
	jsonOutput, args := consumeBool(args, "--json")
	if len(args) > 1 {
		return a.usageError("usage: beforedone receipt [check-id] [--json]")
	}
	checkID := ""
	if len(args) == 1 {
		checkID = args[0]
	}
	repo, cfg, code := a.repoConfig()
	if code != ExitOK {
		return code
	}
	receipt, err := evidence.LoadLatest(repo, checkID)
	if err != nil {
		if strings.Contains(err.Error(), "invalid check id") {
			return a.usageErr(err)
		}
		if errors.Is(err, os.ErrNotExist) {
			return a.resultError(jsonOutput, "no evidence receipt found", model.Inconclusive)
		}
		return a.internal(err)
	}
	effective := receipt.Verdict
	fresh, reason := evidence.ValidateBinding(repo, cfg, receipt)
	if !fresh {
		effective = model.Inconclusive
	}
	receipt.Fresh = &fresh
	receipt.StaleReason = reason
	result := map[string]any{"schema_version": 1, "effective_verdict": effective, "fresh": fresh, "stale_reason": reason, "receipt": receipt}
	if jsonOutput {
		a.writeJSON(result)
	} else {
		fmt.Fprintf(a.Out, "%s %s · fresh=%t\n", effective, receipt.CheckID, fresh)
		if reason != "" {
			fmt.Fprintln(a.Out, reason)
		}
		fmt.Fprintf(a.Out, "Fingerprint: %s\n", receipt.RelevantFingerprint)
	}
	return verdictExit(effective)
}

func (a *App) incident(args []string) int {
	jsonOutput, args := consumeBool(args, "--json")
	correction, args, err := consumeString(args, "--correction")
	if err != nil {
		return a.usageError("usage: beforedone incident [--correction <text>] [--transcript <path>] [--json]")
	}
	transcript, args, err := consumeString(args, "--transcript")
	if err != nil || len(args) != 0 {
		return a.usageError("usage: beforedone incident [--correction <text>] [--transcript <path>] [--json]")
	}
	repo, cfg, code := a.repoConfig()
	if code != ExitOK {
		return code
	}
	artifacts, err := incident.CreateWithTranscript(repo, cfg, correction, transcript)
	if err != nil {
		return a.internal(err)
	}
	if jsonOutput {
		a.writeJSON(artifacts)
	} else {
		fmt.Fprintf(a.Out, "Incident %s · %s\nHTML: %s\nJSON: %s\nReplay: %s\n", artifacts.Incident.ID, artifacts.Incident.Verdict, artifacts.HTMLPath, artifacts.JSONPath, artifacts.ReplayPath)
	}
	return verdictExit(artifacts.Incident.Verdict)
}

func (a *App) replay(args []string) int {
	if len(args) == 0 {
		return a.usageError("usage: beforedone replay <analyze|verify> ...")
	}
	switch args[0] {
	case "analyze":
		return a.replayAnalyze(args[1:])
	case "verify":
		return a.replayVerify(args[1:])
	default:
		return a.usageError("usage: beforedone replay <analyze|verify> ...")
	}
}

func (a *App) replayAnalyze(args []string) int {
	jsonOutput, args := consumeBool(args, "--json")
	if len(args) > 1 {
		return a.usageError("usage: beforedone replay analyze [replay-case.json] [--json]")
	}
	path := ""
	if len(args) == 1 {
		path = args[0]
	}
	repo, code := a.repo()
	if code != ExitOK {
		return code
	}
	analysis, err := replay.Analyze(repo, path)
	if err != nil {
		return a.usageErr(err)
	}
	if jsonOutput {
		a.writeJSON(analysis)
	} else {
		fmt.Fprintf(a.Out, "%s · %s\nFOD: %s — %s\n%s\n", analysis.ReplayCaseID, analysis.Verdict, analysis.Divergence.Precision, analysis.Divergence.Reason, analysis.Statement)
	}
	return ExitOK
}

func (a *App) replayVerify(args []string) int {
	jsonOutput, args := consumeBool(args, "--json")
	execute, args := consumeBool(args, "--execute")
	selected, args, err := consumeString(args, "--check")
	if err != nil || len(args) > 1 {
		return a.usageError("usage: beforedone replay verify [replay-case.json] [--check <id>] [--execute] [--json]")
	}
	path := ""
	if len(args) == 1 {
		path = args[0]
	}
	repo, cfg, code := a.repoConfig()
	if code != ExitOK {
		return code
	}
	verification, err := replay.Verify(repo, cfg, path, selected, execute)
	if err != nil {
		if strings.Contains(err.Error(), "unknown check") || strings.Contains(err.Error(), "replay case") {
			return a.usageErr(err)
		}
		return a.internal(err)
	}
	if jsonOutput {
		a.writeJSON(verification)
	} else if !execute {
		fmt.Fprintf(a.Out, "DRY RUN for %s at %s\nCommands come only from current .beforedone.yaml; imported commands are ignored.\n", verification.ReplayCaseID, verification.GitCommit)
		for _, plan := range verification.Plans {
			fmt.Fprintf(a.Out, "  %s: %q (cwd %s)\n", plan.CheckID, plan.Argv, plan.WorkingDirectory)
		}
	} else {
		fmt.Fprintf(a.Out, "Replay verification: %s\n", verification.Verdict)
		for _, result := range verification.Results {
			fmt.Fprintf(a.Out, "  %s: %s (exit %d)\n", result.CheckID, result.Verdict, result.ExitCode)
		}
	}
	if !execute {
		return ExitOK
	}
	return verdictExit(verification.Verdict)
}

func (a *App) adapter(args []string) int {
	if len(args) == 0 {
		return a.usageError("usage: beforedone adapter <ingest|test> ...")
	}
	switch args[0] {
	case "ingest":
		return a.adapterIngest(args[1:])
	case "test":
		return a.adapterTest(args[1:])
	default:
		return a.usageError("usage: beforedone adapter <ingest|test> ...")
	}
}

func (a *App) adapterIngest(args []string) int {
	jsonOutput, args := consumeBool(args, "--json")
	if len(args) > 1 {
		return a.usageError("usage: beforedone adapter ingest [file|-] [--json]")
	}
	repo, cfg, code := a.repoConfig()
	if code != ExitOK {
		return code
	}
	reader := a.In
	var file *os.File
	if len(args) == 1 && args[0] != "-" {
		var err error
		file, err = os.Open(args[0])
		if err != nil {
			return a.usageErr(err)
		}
		defer file.Close()
		reader = file
	}
	result, err := adapter.Ingest(repo, cfg, reader)
	if err != nil {
		return a.usageErr(err)
	}
	if jsonOutput {
		a.writeJSON(result)
	} else {
		fmt.Fprintf(a.Out, "Ingested %d normalized event(s).\n", result.Ingested)
	}
	return ExitOK
}

func (a *App) adapterTest(args []string) int {
	jsonOutput, args := consumeBool(args, "--json")
	path := filepath.Join(a.CWD, "fixtures", "adapters")
	if len(args) == 1 {
		path = args[0]
	} else if len(args) > 1 {
		return a.usageError("usage: beforedone adapter test [path] [--json]")
	}
	cfg := config.Default(a.CWD)
	if repo, err := repository.Discover(a.CWD); err == nil {
		if loaded, err := config.Load(repo); err == nil {
			cfg = loaded
		}
	}
	result, err := adapter.Test(path, cfg)
	if err != nil {
		return a.usageErr(err)
	}
	if jsonOutput {
		a.writeJSON(result)
	} else {
		fmt.Fprintf(a.Out, "Adapter fixtures: %d/%d passed across %d files\n", result.Passed, result.Cases, result.Files)
		for _, failure := range result.Failures {
			fmt.Fprintf(a.Out, "FAIL: %s\n", failure)
		}
	}
	if len(result.Failures) > 0 {
		return ExitFail
	}
	return ExitOK
}

func (a *App) licenses(args []string) int {
	jsonOutput, args := consumeBool(args, "--json")
	if len(args) != 0 {
		return a.usageError("usage: beforedone licenses [--json]")
	}
	items := []map[string]string{
		{"component": "BeforeDone", "license": "Apache-2.0", "source": "https://github.com/rrrrrredy/beforedone"},
		{"component": "golang.org/x/sys", "license": "BSD-3-Clause", "source": "https://go.googlesource.com/sys"},
		{"component": "gopkg.in/yaml.v3", "license": "MIT and Apache-2.0", "source": "https://github.com/go-yaml/yaml"},
	}
	if jsonOutput {
		a.writeJSON(map[string]any{"schema_version": 1, "licenses": items})
	} else {
		for _, item := range items {
			fmt.Fprintf(a.Out, "%s — %s — %s\n", item["component"], item["license"], item["source"])
		}
	}
	return ExitOK
}

func (a *App) hook(args []string) int {
	if len(args) != 1 || args[0] != "codex" {
		return a.usageError("hook is an internal command")
	}
	if pluginVersion := os.Getenv("BEFOREDONE_PLUGIN_VERSION"); !compatiblePluginVersion(a.Version, pluginVersion) {
		a.writeJSON(map[string]any{
			"systemMessage": fmt.Sprintf("BeforeDone Plugin %s is not compatible with CLI %s. Update both from https://github.com/rrrrrredy/beforedone before relying on the Stop gate.", pluginVersion, a.Version),
		})
		return ExitOK
	}
	if err := hooks.Codex(a.In, a.Out, a.CWD); err != nil {
		return a.internal(err)
	}
	return ExitOK
}

func compatiblePluginVersion(cliVersion, pluginVersion string) bool {
	pluginVersion = strings.TrimPrefix(strings.TrimSpace(pluginVersion), "v")
	cliVersion = strings.TrimPrefix(strings.TrimSpace(cliVersion), "v")
	if pluginVersion == "" || cliVersion == "" || cliVersion == "dev" || cliVersion == "(devel)" {
		return true
	}
	pluginMajor, _, pluginOK := strings.Cut(pluginVersion, ".")
	cliMajor, _, cliOK := strings.Cut(cliVersion, ".")
	return pluginOK && cliOK && pluginMajor == cliMajor
}

func (a *App) repo() (*repository.Repository, int) {
	repo, err := repository.Discover(a.CWD)
	if err != nil {
		return nil, a.usageErr(err)
	}
	return repo, ExitOK
}

func (a *App) repoConfig() (*repository.Repository, model.Config, int) {
	repo, code := a.repo()
	if code != ExitOK {
		return nil, model.Config{}, code
	}
	cfg, err := config.Load(repo)
	if err != nil {
		return nil, model.Config{}, a.usageErr(err)
	}
	return repo, cfg, ExitOK
}

func (a *App) writeJSON(value any) {
	enc := json.NewEncoder(a.Out)
	enc.SetEscapeHTML(true)
	enc.SetIndent("", "  ")
	_ = enc.Encode(value)
}

func (a *App) resultError(jsonOutput bool, message string, verdict model.Verdict) int {
	if jsonOutput {
		a.writeJSON(map[string]any{"schema_version": 1, "verdict": verdict, "error": message})
	} else {
		fmt.Fprintln(a.Err, message)
	}
	return verdictExit(verdict)
}

func (a *App) usageErr(err error) int {
	fmt.Fprintf(a.Err, "beforedone: %v\n", err)
	return ExitUsage
}

func (a *App) usageError(format string, args ...any) int {
	return a.usageErr(fmt.Errorf(format, args...))
}

func (a *App) internal(err error) int {
	fmt.Fprintf(a.Err, "beforedone: internal error: %v\n", err)
	return ExitInternal
}

func (a *App) help() {
	fmt.Fprint(a.Out, `BeforeDone — Prove Before Done

Usage:
  beforedone init
  beforedone doctor
  beforedone setup codex [--remove]
  beforedone check <check-id>
  beforedone receipt [check-id]
  beforedone incident [--correction <text>] [--transcript <path>]
  beforedone replay analyze [replay-case.json]
  beforedone replay verify [replay-case.json] [--check <id>] [--execute]
  beforedone adapter ingest [file|-]
  beforedone adapter test [path]
  beforedone licenses

Add --json to public commands for schema_version 1 machine output.
`)
}

func verdictExit(verdict model.Verdict) int {
	switch verdict {
	case model.Pass:
		return ExitOK
	case model.Fail:
		return ExitFail
	default:
		return ExitInconclusive
	}
}

func consumeBool(args []string, name string) (bool, []string) {
	var rest []string
	found := false
	for _, arg := range args {
		if arg == name {
			found = true
			continue
		}
		rest = append(rest, arg)
	}
	return found, rest
}

func consumeString(args []string, name string) (string, []string, error) {
	var rest []string
	value := ""
	for i := 0; i < len(args); i++ {
		arg := args[i]
		if strings.HasPrefix(arg, name+"=") {
			if value != "" {
				return "", nil, fmt.Errorf("%s supplied more than once", name)
			}
			value = strings.TrimPrefix(arg, name+"=")
			continue
		}
		if arg == name {
			if value != "" || i+1 >= len(args) {
				return "", nil, fmt.Errorf("%s requires one value", name)
			}
			value = args[i+1]
			i++
			continue
		}
		rest = append(rest, arg)
	}
	return value, rest, nil
}
