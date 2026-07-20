package config

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/rrrrrredy/beforedone/internal/model"
)

func TestValidateRejectsRelevantGlobTraversal(t *testing.T) {
	root := t.TempDir()
	cfg := validConfigForSecurityTest()
	check := cfg.Checks["unit"]
	check.RelevantFiles = []string{"../outside/**"}
	cfg.Checks["unit"] = check
	if err := Validate(root, cfg); err == nil {
		t.Fatal("configuration accepted relevant_files path traversal")
	}
}

func TestValidateRejectsWorkingDirectorySymlinkEscape(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	link := filepath.Join(root, "linked-workdir")
	if err := os.Symlink(outside, link); err != nil {
		t.Skipf("symlinks unavailable on this platform: %v", err)
	}
	cfg := validConfigForSecurityTest()
	check := cfg.Checks["unit"]
	check.WorkingDirectory = "linked-workdir"
	cfg.Checks["unit"] = check
	if err := Validate(root, cfg); err == nil {
		t.Fatal("configuration accepted a working_directory symlink escaping the repository")
	}
}

func validConfigForSecurityTest() model.Config {
	return model.Config{
		SchemaVersion: 1,
		Checks: map[string]model.CheckConfig{
			"unit": {
				Argv:             []string{"go", "test", "./..."},
				RelevantFiles:    []string{"**/*.go"},
				WorkingDirectory: ".",
				TimeoutSeconds:   10,
			},
		},
	}
}
