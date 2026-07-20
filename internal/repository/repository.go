package repository

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

type Repository struct {
	Root       string
	GitDir     string
	RuntimeDir string
}

func Discover(start string) (*Repository, error) {
	root, err := gitOutput(start, "rev-parse", "--show-toplevel")
	if err != nil {
		return nil, fmt.Errorf("not inside a Git repository: %w", err)
	}
	gitDir, err := gitOutput(start, "rev-parse", "--absolute-git-dir")
	if err != nil {
		return nil, fmt.Errorf("resolve Git directory: %w", err)
	}
	root = filepath.Clean(root)
	gitDir = filepath.Clean(gitDir)
	return &Repository{Root: root, GitDir: gitDir, RuntimeDir: filepath.Join(gitDir, "beforedone")}, nil
}

func (r *Repository) EnsureRuntime() error {
	if err := os.MkdirAll(r.RuntimeDir, 0o700); err != nil {
		return err
	}
	resolvedGitDir, err := filepath.EvalSymlinks(r.GitDir)
	if err != nil {
		return fmt.Errorf("resolve Git directory: %w", err)
	}
	resolvedRuntime, err := filepath.EvalSymlinks(r.RuntimeDir)
	if err != nil {
		return fmt.Errorf("resolve BeforeDone runtime directory: %w", err)
	}
	if !IsWithin(resolvedGitDir, resolvedRuntime) {
		return errors.New("BeforeDone runtime directory resolves outside the Git directory")
	}
	for _, dir := range []string{filepath.Join(r.RuntimeDir, "receipts"), filepath.Join(r.RuntimeDir, "logs"), filepath.Join(r.RuntimeDir, "incidents")} {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			return err
		}
		if _, err := ResolveDirectoryWithin(r.RuntimeDir, dir); err != nil {
			return err
		}
	}
	return nil
}

func (r *Repository) Git(args ...string) (string, error) { return gitOutput(r.Root, args...) }

func gitOutput(dir string, args ...string) (string, error) {
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			msg = err.Error()
		}
		return "", errors.New(msg)
	}
	return strings.TrimSpace(stdout.String()), nil
}

func IsWithin(root, target string) bool {
	r, err := filepath.Abs(root)
	if err != nil {
		return false
	}
	t, err := filepath.Abs(target)
	if err != nil {
		return false
	}
	rel, err := filepath.Rel(r, t)
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

// ResolveDirectoryWithin resolves symlinks for an existing directory and
// verifies that the resolved location remains beneath root. Lexical path
// checks alone are insufficient for configured working directories because a
// repository can contain a symlink that points outside the worktree.
func ResolveDirectoryWithin(root, target string) (string, error) {
	resolvedRoot, err := filepath.EvalSymlinks(root)
	if err != nil {
		return "", fmt.Errorf("resolve repository root: %w", err)
	}
	resolvedTarget, err := filepath.EvalSymlinks(target)
	if err != nil {
		return "", fmt.Errorf("resolve working directory: %w", err)
	}
	info, err := os.Stat(resolvedTarget)
	if err != nil {
		return "", fmt.Errorf("inspect working directory: %w", err)
	}
	if !info.IsDir() {
		return "", fmt.Errorf("working directory is not a directory: %s", target)
	}
	if !IsWithin(resolvedRoot, resolvedTarget) {
		return "", fmt.Errorf("working directory resolves outside the repository: %s", target)
	}
	return resolvedTarget, nil
}

// ResolveFileWithin is the file counterpart to ResolveDirectoryWithin. It
// rejects symlink targets outside root before a caller opens the file.
func ResolveFileWithin(root, target string) (string, error) {
	resolvedRoot, err := filepath.EvalSymlinks(root)
	if err != nil {
		return "", fmt.Errorf("resolve root: %w", err)
	}
	resolvedTarget, err := filepath.EvalSymlinks(target)
	if err != nil {
		return "", fmt.Errorf("resolve file: %w", err)
	}
	info, err := os.Stat(resolvedTarget)
	if err != nil {
		return "", fmt.Errorf("inspect file: %w", err)
	}
	if !info.Mode().IsRegular() {
		return "", fmt.Errorf("path is not a regular file: %s", target)
	}
	if !IsWithin(resolvedRoot, resolvedTarget) {
		return "", fmt.Errorf("file resolves outside its root: %s", target)
	}
	return resolvedTarget, nil
}
