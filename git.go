package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

// gitDisabled turns off all git awareness (the -no-git flag). Like uiQuiet, a
// process-wide switch set once in main before anything reads it.
var gitDisabled bool

type gitInfo struct {
	ok       bool
	toplevel string // repo root as git reports it (symlinks resolved)
}

var (
	gitMu    sync.Mutex
	gitCache = map[string]gitInfo{}
)

// gitAvailable reports whether the git binary is on PATH and root sits inside a
// working tree. Memoized per root: detection shells out once. Fails quiet -- no
// git, no repo, or -no-git all yield false, never an error.
func gitAvailable(root string) bool { return gitProbe(root).ok }

func gitProbe(root string) gitInfo {
	if gitDisabled {
		return gitInfo{}
	}
	gitMu.Lock()
	defer gitMu.Unlock()
	if info, ok := gitCache[root]; ok {
		return info
	}
	var info gitInfo
	if _, err := exec.LookPath("git"); err == nil {
		if out, err := exec.Command("git", "-C", root, "rev-parse", "--show-toplevel").Output(); err == nil {
			info = gitInfo{ok: true, toplevel: strings.TrimSpace(string(out))}
		}
	}
	gitCache[root] = info
	return info
}

// gitStatus maps repo-relative-to-served-root path -> single-letter status for
// every file git considers changed. Uses porcelain v2 -z, the stable
// null-delimited format. Fails quiet: nil on any error, no repo, or disabled.
func gitStatus(root string) map[string]string {
	info := gitProbe(root)
	if !info.ok {
		return nil
	}
	out, err := exec.Command("git", "-C", root, "status", "--porcelain=v2", "-z").Output()
	if err != nil {
		return nil
	}
	// Porcelain paths are relative to the repo root regardless of -C, so strip
	// the served root's offset within the repo to match the index's keys.
	prefix := ""
	if rel, err := filepath.Rel(info.toplevel, root); err == nil && rel != "." {
		prefix = filepath.ToSlash(rel) + "/"
	}
	key := func(p string) (string, bool) {
		if prefix == "" {
			return p, true
		}
		if !strings.HasPrefix(p, prefix) {
			return "", false // outside the served subtree
		}
		return p[len(prefix):], true
	}

	status := map[string]string{}
	fields := strings.Split(string(out), "\x00")
	for i := 0; i < len(fields); i++ {
		f := fields[i]
		if f == "" {
			continue
		}
		switch f[0] {
		case '?': // "? <path>"
			if k, ok := key(f[2:]); ok {
				status[k] = "U" // untracked
			}
		case '1': // "1 <XY> <sub> <mH> <mI> <mW> <hH> <hI> <path>"
			p := strings.SplitN(f, " ", 9)
			if len(p) == 9 {
				if k, ok := key(p[8]); ok {
					status[k] = mapXY(p[1])
				}
			}
		case '2': // "2 <XY> ... <Rscore> <path>", then original path in the next field
			p := strings.SplitN(f, " ", 10)
			if len(p) == 10 {
				if k, ok := key(p[9]); ok {
					status[k] = mapXY(p[1])
				}
			}
			i++ // the original path follows as its own NUL-terminated field
		case 'u': // "u <XY> <sub> <m1> <m2> <m3> <mW> <h1> <h2> <h3> <path>"
			p := strings.SplitN(f, " ", 11)
			if len(p) == 11 {
				if k, ok := key(p[10]); ok {
					status[k] = "!" // unmerged / conflict
				}
			}
		}
	}
	if len(status) == 0 {
		return nil
	}
	return status
}

// mapXY collapses a porcelain v2 two-letter XY code (X=index, Y=worktree) into
// a single status letter, preferring the staged side when both are set.
func mapXY(xy string) string {
	if len(xy) < 2 {
		return "M"
	}
	c := xy[0]
	if c == '.' {
		c = xy[1]
	}
	switch c {
	case 'A':
		return "A"
	case 'D':
		return "D"
	case 'R':
		return "R"
	case 'C':
		return "C"
	case 'U':
		return "!" // unmerged / conflict
	default: // M (modified), T (typechange) and anything else read as modified
		return "M"
	}
}

// gitDiff returns the unified diff of relpath against HEAD. relpath is relative
// to the served root; git resolves it against -C root. Fails quiet -> "".
func gitDiff(root, relpath string) string {
	if !gitAvailable(root) {
		return ""
	}
	out, err := exec.Command("git", "-C", root, "diff", "--no-color", "HEAD", "--", relpath).Output()
	if err != nil {
		return ""
	}
	return string(out)
}

// gitHunks parses the unified diff of relpath against HEAD into 1-based
// NEW-FILE line numbers for a change gutter: added lines, modified (replaced)
// lines, and one marker per pure-deletion run (the new-file line immediately
// preceding the removed run; 0 means "before the first line"). Fails quiet:
// empty when git is off/unavailable or the file has no diff (clean/untracked).
func gitHunks(root, relpath string) (added, modified, deleted []int) {
	diff := gitDiff(root, relpath)
	if diff == "" {
		return nil, nil, nil
	}
	newLine := 0
	inHunk := false
	// Current block: a maximal run of consecutive '+'/'-' lines.
	dels := 0
	var adds []int
	blockStart := 0 // newLine when the block began (for deletion markers)
	flush := func() {
		switch {
		case dels > 0 && len(adds) > 0:
			modified = append(modified, adds...) // replacement
		case len(adds) > 0:
			added = append(added, adds...) // pure insertion
		case dels > 0:
			deleted = append(deleted, blockStart-1) // pure deletion
		}
		dels, adds = 0, nil
	}
	for _, line := range strings.Split(diff, "\n") {
		switch {
		case strings.HasPrefix(line, "@@"):
			flush()
			inHunk = true
			newLine = parseNewStart(line)
		case !inHunk, strings.HasPrefix(line, "\\"): // pre-hunk header / "\ No newline"
			// skip: neither +/- nor a new-file line
		case strings.HasPrefix(line, "+"):
			if dels == 0 && len(adds) == 0 {
				blockStart = newLine
			}
			adds = append(adds, newLine)
			newLine++
		case strings.HasPrefix(line, "-"):
			if dels == 0 && len(adds) == 0 {
				blockStart = newLine
			}
			dels++
		default: // context line (" ...", or the trailing empty split element)
			flush()
			newLine++
		}
	}
	flush()
	return added, modified, deleted
}

// parseNewStart pulls newStart out of a hunk header "@@ -a,b +c,d @@".
func parseNewStart(hdr string) int {
	i := strings.IndexByte(hdr, '+')
	if i < 0 {
		return 1
	}
	rest := hdr[i+1:]
	if end := strings.IndexAny(rest, ", "); end >= 0 {
		rest = rest[:end]
	}
	if n, err := strconv.Atoi(rest); err == nil {
		return n
	}
	return 1
}

// worktree is one checkout of the repository: the main one plus every
// `git worktree add` directory. An agent told to work on a branch often runs in
// one of these, so px1 has to be able to look at them, not only the directory
// it was started in.
type worktree struct {
	Path    string `json:"path"`
	Name    string `json:"name"`
	Branch  string `json:"branch,omitempty"`
	Head    string `json:"head,omitempty"`
	Main    bool   `json:"main"`
	Current bool   `json:"current"`
	Missing bool   `json:"missing,omitempty"`
	// AddedAt is when `git worktree add` created the checkout, taken from its
	// .git entry. The newest is what an agent was most likely just told to work
	// in, so the list leads with it.
	AddedAt time.Time `json:"addedAt,omitempty"`
}

// worktrees lists the checkouts of root's repository, main one first. The list
// is empty outside a git repository, which is what hides the switcher.
func worktrees(root string) []worktree {
	if !gitAvailable(root) {
		return nil
	}
	out, err := exec.Command("git", "-C", root, "worktree", "list", "--porcelain").Output()
	if err != nil {
		return nil
	}
	current := resolvePathOrSelf(root)
	var list []worktree
	var cur *worktree
	for _, line := range strings.Split(string(out), "\n") {
		switch {
		case strings.HasPrefix(line, "worktree "):
			p := strings.TrimPrefix(line, "worktree ")
			list = append(list, worktree{Path: p, Name: filepath.Base(p), Main: len(list) == 0})
			cur = &list[len(list)-1]
			cur.Current = resolvePathOrSelf(p) == current
			if fi, err := os.Stat(p); err != nil || !fi.IsDir() {
				cur.Missing = true
			}
			// In a linked worktree .git is a file written when `git worktree
			// add` created it. The main checkout's .git is the repository
			// directory, whose time moves with every operation, so it is left
			// unset and sorts last.
			if fi, err := os.Lstat(filepath.Join(p, ".git")); err == nil && fi.Mode().IsRegular() {
				cur.AddedAt = fi.ModTime()
			}
		case cur == nil:
		case strings.HasPrefix(line, "HEAD "):
			cur.Head = strings.TrimPrefix(line, "HEAD ")
		case strings.HasPrefix(line, "branch "):
			cur.Branch = strings.TrimPrefix(strings.TrimPrefix(line, "branch "), "refs/heads/")
		case line == "detached":
			cur.Branch = "detached"
		}
	}
	// Newest first, which is where the work being reviewed usually is; the main
	// checkout has no added time and settles at the bottom.
	sort.SliceStable(list, func(i, j int) bool {
		if list[i].AddedAt.IsZero() != list[j].AddedAt.IsZero() {
			return list[j].AddedAt.IsZero()
		}
		return list[i].AddedAt.After(list[j].AddedAt)
	})
	return list
}

// worktreeBase is the commit checked out when this linked worktree was created.
// Git keeps a separate HEAD reflog for each worktree, whose oldest entry is that
// checkout. Unlike a merge-base with main, this excludes changes that were
// already present on the branch (for example when the worktree starts from
// staging) and includes only work done after the agent got this checkout.
func worktreeBase(root string) string {
	if !gitAvailable(root) {
		return ""
	}
	out, err := exec.Command("git", "-C", root, "reflog", "show", "--format=%H", "HEAD").Output()
	if err != nil {
		return ""
	}
	lines := strings.Fields(string(out))
	if len(lines) == 0 {
		return ""
	}
	// reflog lists newest first, so the last entry is the initial checkout.
	return lines[len(lines)-1]
}

// resolvePathOrSelf follows symlinks so /tmp and /private/tmp style pairs
// compare equal; an unresolvable path is compared as given.
func resolvePathOrSelf(p string) string {
	if r, err := filepath.EvalSymlinks(p); err == nil {
		return r
	}
	return filepath.Clean(p)
}
