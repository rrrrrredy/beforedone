package config

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/rrrrrredy/beforedone/internal/evidence"
	"github.com/rrrrrredy/beforedone/internal/model"
	"github.com/rrrrrredy/beforedone/internal/repository"
	"gopkg.in/yaml.v3"
)

const FileName = ".beforedone.yaml"

func Default(root string) model.Config {
	argv := []string{"git", "status", "--short"}
	patterns := []string{"**/*"}
	if _, err := os.Stat(filepath.Join(root, "go.mod")); err == nil {
		argv = []string{"go", "test", "./..."}
		patterns = []string{"**/*.go", "go.mod", "go.sum"}
	}
	return model.Config{
		SchemaVersion: model.SchemaVersion,
		Checks: map[string]model.CheckConfig{
			"test": {Argv: argv, RelevantFiles: patterns, WorkingDirectory: ".", TimeoutSeconds: 600},
		},
		Capture: model.CaptureConfig{MaxOutputBytes: 1 << 20, RedactPatterns: []string{
			`(?i)(api[_-]?key|token|password|secret)\s*[:=]\s*[^\s]+`,
		}},
		Reports: model.ReportConfig{Retain: 20},
	}
}

func Load(repo *repository.Repository) (model.Config, error) {
	path := filepath.Join(repo.Root, FileName)
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return model.Config{}, fmt.Errorf("%s not found; run `beforedone init`", FileName)
		}
		return model.Config{}, err
	}
	if len(data) > 1<<20 {
		return model.Config{}, errors.New("configuration exceeds 1 MiB")
	}
	var cfg model.Config
	dec := yaml.NewDecoder(strings.NewReader(string(data)))
	dec.KnownFields(true)
	if err := dec.Decode(&cfg); err != nil {
		return model.Config{}, fmt.Errorf("parse %s: %w", FileName, err)
	}
	var trailing any
	if err := dec.Decode(&trailing); !errors.Is(err, io.EOF) {
		if err == nil {
			return model.Config{}, fmt.Errorf("parse %s: multiple YAML documents are not allowed", FileName)
		}
		return model.Config{}, fmt.Errorf("parse %s trailing content: %w", FileName, err)
	}
	if err := Validate(repo.Root, cfg); err != nil {
		return model.Config{}, err
	}
	return cfg, nil
}

func Validate(root string, cfg model.Config) error {
	if cfg.SchemaVersion != model.SchemaVersion {
		return fmt.Errorf("unsupported config schema_version %d", cfg.SchemaVersion)
	}
	if len(cfg.Checks) == 0 {
		return errors.New("configuration must define at least one check")
	}
	for id, check := range cfg.Checks {
		if !validID(id) {
			return fmt.Errorf("invalid check id %q", id)
		}
		if len(check.Argv) == 0 || strings.TrimSpace(check.Argv[0]) == "" {
			return fmt.Errorf("check %q argv must be a non-empty array", id)
		}
		for _, arg := range check.Argv {
			if strings.ContainsRune(arg, '\x00') {
				return fmt.Errorf("check %q argv contains NUL", id)
			}
		}
		if len(check.RelevantFiles) == 0 {
			return fmt.Errorf("check %q must define relevant_files", id)
		}
		if err := evidence.ValidatePatterns(check.RelevantFiles); err != nil {
			return fmt.Errorf("check %q has invalid relevant_files: %w", id, err)
		}
		wd := check.WorkingDirectory
		if wd == "" {
			wd = "."
		}
		if filepath.IsAbs(wd) || !repository.IsWithin(root, filepath.Join(root, wd)) {
			return fmt.Errorf("check %q working_directory must remain inside the repository", id)
		}
		if _, err := repository.ResolveDirectoryWithin(root, filepath.Join(root, wd)); err != nil {
			return fmt.Errorf("check %q has invalid working_directory: %w", id, err)
		}
		if check.TimeoutSeconds < 0 || check.TimeoutSeconds > 86400 {
			return fmt.Errorf("check %q timeout_seconds must be between 0 and 86400", id)
		}
	}
	if cfg.Capture.MaxOutputBytes < 0 || cfg.Capture.MaxOutputBytes > 64<<20 {
		return errors.New("capture.max_output_bytes must be between 0 and 64 MiB")
	}
	for _, pattern := range cfg.Capture.RedactPatterns {
		if _, err := regexp.Compile(pattern); err != nil {
			return fmt.Errorf("invalid redact pattern: %w", err)
		}
	}
	if cfg.Reports.Retain < 0 || cfg.Reports.Retain > 10000 {
		return errors.New("reports.retain must be between 0 and 10000")
	}
	return nil
}

func WriteDefault(repo *repository.Repository) (string, error) {
	path := filepath.Join(repo.Root, FileName)
	if _, err := os.Stat(path); err == nil {
		return "", fmt.Errorf("%s already exists", FileName)
	} else if !os.IsNotExist(err) {
		return "", err
	}
	data, err := yaml.Marshal(Default(repo.Root))
	if err != nil {
		return "", err
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return "", err
	}
	return path, nil
}

func validID(id string) bool {
	if id == "" || len(id) > 64 {
		return false
	}
	for _, r := range id {
		if !(r >= 'a' && r <= 'z') && !(r >= 'A' && r <= 'Z') && !(r >= '0' && r <= '9') && r != '-' && r != '_' && r != '.' {
			return false
		}
	}
	return true
}
