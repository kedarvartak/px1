package agent

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"
)

const (
	maxBlobBytes     = 2 << 20
	maxGitOutput     = 512 << 10
	maxHitsPerFile   = 3
	maxSnippetBytes  = 300
	maxListedEntries = 200
	maxSummaryBytes  = 200
	defaultGitTime   = 20 * time.Second
)

// GitRepo backs the agent tools with read-only git queries against a local
// clone, pinned to Scope.HeadSHA. It never checks out files and never runs
// anything the repository configures: git can execute repo-chosen commands
// (textconv filters, fsmonitor hooks) during blame and status-like reads, so
// those are disabled on every call. The clone is treated as untrusted data.
type GitRepo struct {
	Root    string
	GitPath string
	Timeout time.Duration
}

var errNotFound = errors.New("not found at this revision")

// gitError reports a failed git command by name and exit code only: stderr
// can contain local paths, so it is never surfaced to tools or models.
type gitError struct {
	command string
	code    int
}

func (e *gitError) Error() string { return "git " + e.command + " failed" }

func (g GitRepo) git(ctx context.Context, limit int, args ...string) (out []byte, truncated bool, err error) {
	if g.Root == "" {
		return nil, false, errors.New("git repository root is required")
	}
	timeout := g.Timeout
	if timeout <= 0 {
		timeout = defaultGitTime
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	binary := g.GitPath
	if binary == "" {
		binary = "git"
	}
	full := append([]string{
		"-C", g.Root, "--no-pager",
		"-c", "core.fsmonitor=false",
		"-c", "core.hooksPath=" + os.DevNull,
		"-c", "core.quotePath=false",
	}, args...)
	cmd := exec.CommandContext(ctx, binary, full...)
	cmd.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0", "GIT_OPTIONAL_LOCKS=0", "GIT_PAGER=cat", "LC_ALL=C")
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, false, err
	}
	var stderr bytes.Buffer
	cmd.Stderr = &limitedBuffer{buf: &stderr, max: 2 << 10}
	if err := cmd.Start(); err != nil {
		return nil, false, fmt.Errorf("start git: %w", err)
	}
	data, readErr := io.ReadAll(io.LimitReader(stdout, int64(limit)+1))
	if len(data) > limit {
		truncated = true
		data = data[:limit]
		cancel()
	}
	waitErr := cmd.Wait()
	if readErr != nil {
		return nil, false, readErr
	}
	if truncated {
		return data, true, nil
	}
	if waitErr != nil {
		if ctx.Err() != nil {
			return nil, false, fmt.Errorf("git %s timed out", args[0])
		}
		var exit *exec.ExitError
		code := -1
		if errors.As(waitErr, &exit) {
			code = exit.ExitCode()
		}
		return nil, false, &gitError{command: args[0], code: code}
	}
	return data, false, nil
}

type limitedBuffer struct {
	buf *bytes.Buffer
	max int
}

func (b *limitedBuffer) Write(p []byte) (int, error) {
	if room := b.max - b.buf.Len(); room > 0 {
		b.buf.Write(p[:min(room, len(p))])
	}
	return len(p), nil
}

// blobInfo returns the size of a regular file at the revision.
func (g GitRepo) blobInfo(ctx context.Context, sha, path string) (int, error) {
	out, _, err := g.git(ctx, 4<<10, "ls-tree", "-l", sha, "--", path)
	if err != nil {
		return 0, err
	}
	fields := strings.Fields(strings.SplitN(string(out), "\t", 2)[0])
	if len(fields) < 4 {
		return 0, errNotFound
	}
	switch {
	case fields[1] == "tree":
		return 0, errors.New("path is a directory; use list_files")
	case fields[0] == "120000":
		return 0, errors.New("path is a symbolic link")
	case fields[1] != "blob":
		return 0, errors.New("path is not a regular file")
	}
	size, err := strconv.Atoi(fields[3])
	if err != nil {
		return 0, errNotFound
	}
	return size, nil
}

func (g GitRepo) ReadLines(ctx context.Context, scope Scope, path string, start, end int) (string, error) {
	clean, err := repositoryPath(path)
	if err != nil {
		return "", err
	}
	size, err := g.blobInfo(ctx, scope.HeadSHA, clean)
	if err != nil {
		return "", err
	}
	if size > maxBlobBytes {
		return "", fmt.Errorf("file is %d bytes; files over %d bytes cannot be read, use search_code", size, maxBlobBytes)
	}
	data, _, err := g.git(ctx, maxBlobBytes, "show", scope.HeadSHA+":"+clean)
	if err != nil {
		return "", err
	}
	if bytes.IndexByte(data[:min(len(data), 8000)], 0) >= 0 {
		return "", errors.New("file is binary")
	}
	lines := strings.Split(strings.TrimSuffix(string(data), "\n"), "\n")
	if len(data) == 0 {
		lines = nil
	}
	if start > len(lines) {
		return "", fmt.Errorf("file has only %d lines", len(lines))
	}
	end = min(end, len(lines))
	var out strings.Builder
	fmt.Fprintf(&out, "%s lines %d-%d of %d\n", clean, start, end, len(lines))
	for i := start; i <= end; i++ {
		fmt.Fprintf(&out, "%d\t%s\n", i, strings.TrimSuffix(lines[i-1], "\r"))
	}
	return out.String(), nil
}

func (g GitRepo) Search(ctx context.Context, scope Scope, query string, limit int) ([]SearchHit, error) {
	if strings.ContainsAny(query, "\x00\n\r") {
		return nil, errors.New("query must be a single line of text")
	}
	out, _, err := g.git(ctx, maxGitOutput, "grep", "-n", "-z", "-F", "-I", "--no-color", "-e", query, scope.HeadSHA, "--")
	if err != nil {
		// git grep exits 1 when nothing matches; every other failure is real.
		var failed *gitError
		if errors.As(err, &failed) && failed.code == 1 {
			return []SearchHit{}, nil
		}
		return nil, err
	}
	prefix := scope.HeadSHA + ":"
	perFile := map[string]int{}
	hits := []SearchHit{}
	for _, record := range strings.Split(string(out), "\n") {
		parts := strings.SplitN(record, "\x00", 3)
		if len(parts) != 3 || !strings.HasPrefix(parts[0], prefix) {
			continue
		}
		path := strings.TrimPrefix(parts[0], prefix)
		line, err := strconv.Atoi(parts[1])
		if err != nil || perFile[path] >= maxHitsPerFile {
			continue
		}
		perFile[path]++
		hits = append(hits, SearchHit{Path: path, StartLine: line, EndLine: line, Snippet: clipText(parts[2], maxSnippetBytes), Source: "git-grep"})
		if len(hits) >= limit {
			break
		}
	}
	return hits, nil
}

func (g GitRepo) Blame(ctx context.Context, scope Scope, path string, start, end int) ([]BlameLine, error) {
	clean, err := repositoryPath(path)
	if err != nil {
		return nil, err
	}
	if _, err := g.blobInfo(ctx, scope.HeadSHA, clean); err != nil {
		return nil, err
	}
	out, _, err := g.git(ctx, maxGitOutput, "blame", "--no-textconv", "--porcelain", "-L", fmt.Sprintf("%d,%d", start, end), scope.HeadSHA, "--", clean)
	if err != nil {
		return nil, errors.New("line range is not valid for this file at this revision")
	}
	type commitInfo struct{ author, summary string }
	commits := map[string]*commitInfo{}
	var lines []BlameLine
	var current string
	var finalLine int
	for _, raw := range strings.Split(string(out), "\n") {
		switch {
		case strings.HasPrefix(raw, "\t"):
			info := commits[current]
			if info != nil {
				lines = append(lines, BlameLine{Line: finalLine, Commit: current, Author: info.author, Summary: info.summary})
			}
		case isBlameHeader(raw):
			fields := strings.Fields(raw)
			current = fields[0]
			finalLine, _ = strconv.Atoi(fields[2])
			if commits[current] == nil {
				commits[current] = &commitInfo{}
			}
		case strings.HasPrefix(raw, "author "):
			commits[current].author = clipText(strings.TrimPrefix(raw, "author "), maxSummaryBytes)
		case strings.HasPrefix(raw, "summary "):
			commits[current].summary = clipText(strings.TrimPrefix(raw, "summary "), maxSummaryBytes)
		}
	}
	return lines, nil
}

func isBlameHeader(line string) bool {
	fields := strings.Fields(line)
	if len(fields) < 3 || len(fields[0]) != 40 {
		return false
	}
	for _, r := range fields[0] {
		if !((r >= '0' && r <= '9') || (r >= 'a' && r <= 'f')) {
			return false
		}
	}
	_, a := strconv.Atoi(fields[1])
	_, b := strconv.Atoi(fields[2])
	return a == nil && b == nil
}

func (g GitRepo) Log(ctx context.Context, scope Scope, path string, limit int) ([]Commit, error) {
	clean, err := repositoryPath(path)
	if err != nil {
		return nil, err
	}
	out, _, err := g.git(ctx, maxGitOutput, "log", "-n", strconv.Itoa(limit), "--format=%H%x1f%an%x1f%aI%x1f%s%x1e", scope.HeadSHA, "--", clean)
	if err != nil {
		return nil, err
	}
	commits := []Commit{}
	for _, record := range strings.Split(string(out), "\x1e") {
		fields := strings.Split(strings.TrimSpace(record), "\x1f")
		if len(fields) != 4 {
			continue
		}
		commits = append(commits, Commit{SHA: fields[0], Author: clipText(fields[1], maxSummaryBytes), Date: fields[2], Summary: clipText(fields[3], maxSummaryBytes)})
	}
	return commits, nil
}

// ListFiles lists one directory (not recursive) at the revision. Directories
// end with "/". An empty directory means the repository root.
func (g GitRepo) ListFiles(ctx context.Context, scope Scope, directory string) ([]string, error) {
	spec := scope.HeadSHA
	if directory != "" && directory != "." {
		clean, err := repositoryPath(directory)
		if err != nil {
			return nil, err
		}
		spec += ":" + clean
	}
	out, _, err := g.git(ctx, maxGitOutput, "ls-tree", "-z", spec)
	if err != nil {
		return nil, errors.New("directory not found at this revision")
	}
	entries := []string{}
	for _, record := range strings.Split(string(out), "\x00") {
		meta, name, ok := strings.Cut(record, "\t")
		if !ok {
			continue
		}
		if strings.Contains(meta, " tree ") {
			name += "/"
		}
		entries = append(entries, name)
		if len(entries) >= maxListedEntries {
			entries = append(entries, "[listing truncated]")
			break
		}
	}
	return entries, nil
}

func clipText(value string, limit int) string {
	value = strings.ToValidUTF8(value, "�")
	if len(value) <= limit {
		return value
	}
	cut := limit
	for cut > 0 && !utf8.RuneStart(value[cut]) {
		cut--
	}
	return value[:cut] + "…"
}
