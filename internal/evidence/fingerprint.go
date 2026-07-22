package evidence

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/rrrrrredy/beforedone/internal/repository"
)

const maxFingerprintBytes int64 = 512 << 20

const (
	maxGitFileListBytes  = 64 << 20
	maxRelevantFiles     = 100000
	maxPathspecBatchSize = 16 << 10
)

func ValidatePatterns(patterns []string) error {
	for _, pattern := range patterns {
		if _, err := compileGlob(pattern); err != nil {
			return err
		}
	}
	return nil
}

func Fingerprint(repo *repository.Repository, patterns []string) (string, int, error) {
	return FingerprintContext(context.Background(), repo, patterns)
}

func FingerprintContext(ctx context.Context, repo *repository.Repository, patterns []string) (string, int, error) {
	matchers := make([]*regexp.Regexp, 0, len(patterns))
	for _, pattern := range patterns {
		m, err := compileGlob(pattern)
		if err != nil {
			return "", 0, err
		}
		matchers = append(matchers, m)
	}

	if err := ctx.Err(); err != nil {
		return "", 0, err
	}
	seen := map[string]struct{}{}
	var files []string
	var listedBytes int64
	if err := collectGitFiles(ctx, repo, []string{"ls-files", "-co", "--exclude-standard", "-z"}, matchers, seen, &files, &listedBytes); err != nil {
		return "", 0, fmt.Errorf("list repository files: %w", err)
	}
	// Ask Git to apply the relevant globs before it emits ignored paths. This
	// includes ignored generated sources for broad patterns such as **/*.go
	// without first materializing every ignored node_modules entry in memory.
	// collectGitFiles additionally enforces hard byte and file-count limits.
	for _, batch := range ignoredPathspecBatches(patterns) {
		args := []string{"ls-files", "-oi", "--exclude-standard", "-z", "--"}
		args = append(args, batch...)
		if err := collectGitFiles(ctx, repo, args, matchers, seen, &files, &listedBytes); err != nil {
			return "", 0, fmt.Errorf("list ignored relevant files: %w", err)
		}
	}
	indexModes := map[string]string{}
	var gitlinks []string
	if err := collectGitIndex(ctx, repo, matchers, indexModes, &gitlinks, &listedBytes); err != nil {
		return "", 0, fmt.Errorf("list Git index entries: %w", err)
	}
	sort.Strings(gitlinks)
	for _, gitlink := range gitlinks {
		for i, pattern := range patterns {
			if err := ctx.Err(); err != nil {
				return "", 0, err
			}
			if patternMayCoverGitlink(pattern, matchers[i], gitlink) {
				return "", 0, fmt.Errorf("relevant_files may include Git submodule %q; submodule contents are not fingerprinted", gitlink)
			}
		}
	}
	sort.Strings(files)
	h := sha256.New()
	for _, pattern := range patterns {
		writeField(h, "pattern", pattern)
	}
	var total int64
	for _, rel := range files {
		if err := ctx.Err(); err != nil {
			return "", 0, err
		}
		full := filepath.Join(repo.Root, filepath.FromSlash(rel))
		if !repository.IsWithin(repo.Root, full) {
			return "", 0, fmt.Errorf("relevant path escapes repository: %s", rel)
		}
		info, err := os.Lstat(full)
		writeField(h, "path", rel)
		if os.IsNotExist(err) {
			writeField(h, "kind", "missing")
			continue
		}
		if err != nil {
			return "", 0, err
		}
		if indexMode, ok := indexModes[rel]; ok {
			writeField(h, "git-index-executable", strconv.FormatBool(indexMode == "100755"))
		}
		if info.Mode()&os.ModeSymlink != 0 {
			target, err := os.Readlink(full)
			if err != nil {
				return "", 0, err
			}
			writeField(h, "symlink", target)
			resolved, err := repository.ResolveFileWithin(repo.Root, full)
			if err != nil {
				return "", 0, fmt.Errorf("unsafe relevant symlink %s: %w", rel, err)
			}
			resolvedInfo, err := os.Stat(resolved)
			if err != nil {
				return "", 0, err
			}
			writeField(h, "target-executable", strconv.FormatBool(resolvedInfo.Mode().Perm()&0o111 != 0))
			total += resolvedInfo.Size()
			if total > maxFingerprintBytes {
				return "", 0, errors.New("relevant file content exceeds 512 MiB")
			}
			digest, err := digestFileContext(ctx, resolved)
			if err != nil {
				return "", 0, err
			}
			writeField(h, "symlink-target-sha256", digest)
			continue
		}
		if !info.Mode().IsRegular() {
			writeField(h, "kind", info.Mode().String())
			continue
		}
		writeField(h, "executable", strconv.FormatBool(info.Mode().Perm()&0o111 != 0))
		total += info.Size()
		if total > maxFingerprintBytes {
			return "", 0, errors.New("relevant file content exceeds 512 MiB")
		}
		digest, err := digestFileContext(ctx, full)
		if err != nil {
			return "", 0, err
		}
		writeField(h, "sha256", digest)
	}
	return "sha256:" + hex.EncodeToString(h.Sum(nil)), len(files), nil
}

func collectGitFiles(ctx context.Context, repo *repository.Repository, args []string, matchers []*regexp.Regexp, seen map[string]struct{}, files *[]string, listedBytes *int64) error {
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = repo.Root
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}
	cmd.Stderr = io.Discard
	if err := cmd.Start(); err != nil {
		return err
	}
	reader := bufio.NewReaderSize(stdout, 64<<10)
	for {
		raw, readErr := reader.ReadBytes(0)
		*listedBytes += int64(len(raw))
		if *listedBytes > maxGitFileListBytes {
			_ = cmd.Process.Kill()
			_ = cmd.Wait()
			return errors.New("repository file list exceeds 64 MiB")
		}
		if len(raw) > 0 {
			raw = raw[:len(raw)-1]
			if len(raw) > 0 {
				rel := filepath.ToSlash(string(raw))
				if matchesAny(matchers, rel) {
					if _, ok := seen[rel]; !ok {
						if len(*files) >= maxRelevantFiles {
							_ = cmd.Process.Kill()
							_ = cmd.Wait()
							return errors.New("more than 100000 relevant files")
						}
						seen[rel] = struct{}{}
						*files = append(*files, rel)
					}
				}
			}
		}
		if errors.Is(readErr, io.EOF) {
			break
		}
		if readErr != nil {
			_ = cmd.Process.Kill()
			_ = cmd.Wait()
			return readErr
		}
		if err := ctx.Err(); err != nil {
			_ = cmd.Process.Kill()
			_ = cmd.Wait()
			return err
		}
	}
	return cmd.Wait()
}

func collectGitIndex(ctx context.Context, repo *repository.Repository, matchers []*regexp.Regexp, modes map[string]string, gitlinks *[]string, listedBytes *int64) error {
	cmd := exec.CommandContext(ctx, "git", "ls-files", "--stage", "-z")
	cmd.Dir = repo.Root
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}
	cmd.Stderr = io.Discard
	if err := cmd.Start(); err != nil {
		return err
	}
	reader := bufio.NewReaderSize(stdout, 64<<10)
	for {
		raw, readErr := reader.ReadBytes(0)
		*listedBytes += int64(len(raw))
		if *listedBytes > maxGitFileListBytes {
			_ = cmd.Process.Kill()
			_ = cmd.Wait()
			return errors.New("repository file list exceeds 64 MiB")
		}
		if len(raw) > 1 {
			raw = raw[:len(raw)-1]
			tab := strings.IndexByte(string(raw), '\t')
			if tab > 0 {
				metadata := strings.Fields(string(raw[:tab]))
				rel := filepath.ToSlash(string(raw[tab+1:]))
				if len(metadata) == 3 {
					if metadata[0] == "160000" {
						*gitlinks = append(*gitlinks, rel)
					}
					if metadata[2] == "0" && matchesAny(matchers, rel) {
						modes[rel] = metadata[0]
					}
				}
			}
		}
		if errors.Is(readErr, io.EOF) {
			break
		}
		if readErr != nil {
			_ = cmd.Process.Kill()
			_ = cmd.Wait()
			return readErr
		}
		if err := ctx.Err(); err != nil {
			_ = cmd.Process.Kill()
			_ = cmd.Wait()
			return err
		}
	}
	return cmd.Wait()
}

func patternMayCoverGitlink(pattern string, matcher *regexp.Regexp, gitlink string) bool {
	if matcher.MatchString(gitlink) {
		return true
	}
	// A conservative prefix test keeps this fail-closed without modeling a
	// second glob engine. It may reject some disjoint patterns, but it cannot
	// omit a descendant whose path matches the configured glob.
	pattern = filepath.ToSlash(strings.TrimSpace(pattern))
	descendant := strings.TrimSuffix(gitlink, "/") + "/"
	wildcard := strings.IndexAny(pattern, "*?")
	if wildcard < 0 {
		return strings.HasPrefix(pattern, descendant)
	}
	literalPrefix := pattern[:wildcard]
	return strings.HasPrefix(descendant, literalPrefix) || strings.HasPrefix(literalPrefix, descendant)
}

func ignoredPathspecBatches(patterns []string) [][]string {
	var batches [][]string
	var batch []string
	batchSize := 0
	for _, pattern := range patterns {
		pattern = filepath.ToSlash(strings.TrimSpace(pattern))
		pathspec := ":(top,glob)" + escapeGitGlobLiterals(pattern)
		if len(batch) > 0 && batchSize+len(pathspec)+1 > maxPathspecBatchSize {
			batches = append(batches, batch)
			batch = nil
			batchSize = 0
		}
		batch = append(batch, pathspec)
		batchSize += len(pathspec) + 1
	}
	if len(batch) > 0 {
		batches = append(batches, batch)
	}
	return batches
}

func escapeGitGlobLiterals(pattern string) string {
	// BeforeDone globs only assign special meaning to * and ?. Protect the
	// additional metacharacters understood by Git's glob pathspec dialect so
	// Git emits a safe superset that our compiled matcher filters exactly.
	pattern = strings.ReplaceAll(pattern, `\`, `\\`)
	return strings.ReplaceAll(pattern, "[", "[[]")
}

func digestFile(path string) (string, error) {
	return digestFileContext(context.Background(), path)
}

func digestFileContext(ctx context.Context, path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	fileHash := sha256.New()
	reader := bufio.NewReader(f)
	buffer := make([]byte, 256*1024)
	var copyErr error
	for {
		if err := ctx.Err(); err != nil {
			copyErr = err
			break
		}
		n, readErr := reader.Read(buffer)
		if n > 0 {
			if _, err := fileHash.Write(buffer[:n]); err != nil {
				copyErr = err
				break
			}
		}
		if errors.Is(readErr, io.EOF) {
			break
		}
		if readErr != nil {
			copyErr = readErr
			break
		}
	}
	closeErr := f.Close()
	if copyErr != nil {
		return "", copyErr
	}
	if closeErr != nil {
		return "", closeErr
	}
	return hex.EncodeToString(fileHash.Sum(nil)), nil
}

func writeField(w io.Writer, name, value string) {
	fmt.Fprintf(w, "%s:%d:%s\n", name, len(value), value)
}

func matchesAny(matchers []*regexp.Regexp, value string) bool {
	for _, matcher := range matchers {
		if matcher.MatchString(value) {
			return true
		}
	}
	return false
}

func compileGlob(pattern string) (*regexp.Regexp, error) {
	pattern = filepath.ToSlash(strings.TrimSpace(pattern))
	if pattern == "" || strings.HasPrefix(pattern, "/") || strings.Contains(pattern, "\x00") {
		return nil, fmt.Errorf("invalid relevant_files pattern %q", pattern)
	}
	for _, part := range strings.Split(pattern, "/") {
		if part == ".." {
			return nil, fmt.Errorf("relevant_files pattern may not contain ..: %q", pattern)
		}
	}
	var b strings.Builder
	b.WriteString("^")
	for i := 0; i < len(pattern); {
		if pattern[i] == '*' {
			if i+1 < len(pattern) && pattern[i+1] == '*' {
				i += 2
				if i < len(pattern) && pattern[i] == '/' {
					i++
					b.WriteString("(?:.*/)?")
				} else {
					b.WriteString(".*")
				}
				continue
			}
			b.WriteString("[^/]*")
			i++
			continue
		}
		if pattern[i] == '?' {
			b.WriteString("[^/]")
			i++
			continue
		}
		b.WriteString(regexp.QuoteMeta(pattern[i : i+1]))
		i++
	}
	b.WriteString("$")
	return regexp.Compile(b.String())
}
