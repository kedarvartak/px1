# Git Awareness & Diffing

This document describes the design and implementation of px1's git integration engine ([`git.go`](../../git.go)) and its diff-rendering frontend ([`web/src/diff.js`](../../web/src/diff.js)).

## 1. Zero-Dependency Shell-Out Architecture

px1 avoids heavy third-party Go git libraries (such as `go-git`, which can consume large amounts of memory re-parsing packfiles, or `libgit2`, which requires CGO).

Instead, px1 adheres to a Pure Shell-Out Architecture:

- Shells out directly to the host `git` binary.
- Never stages, commits, or changes refs or the index. The only workspace writes px1 makes are the ones a dispatched coding harness makes itself ([Harness Editing & Agent Dispatch](agent-editing.md)).
- Zero disk footprint: holds all status and diff structures in volatile memory on the `Index` (`Node.Status`).
- Graceful degradation: if `git` is not installed, or if the opened directory is not a git repository, git features degrade silently without warnings or errors.
- Can be disabled explicitly using the `-no-git` CLI flag.

## 2. Concurrent Status Generation

On large repositories, running `git status` can take 50-100 milliseconds. Running this serially during startup would delay index readiness.

px1 runs `git status` concurrently alongside the filesystem walk:

```go
gsCh := make(chan map[string]string, 1)
go func() { gsCh <- gitStatus(ix.root) }()

// Walk directory tree concurrently...
walk(ix.root, "", root)

// Overlay git status onto index nodes
gs := <-gsCh
```

### Git Command Specification

px1 invokes:

```bash
git status --porcelain=v2 -z
```

- `--porcelain=v2`: Machine-readable format immune to user git config customizations.
- `-z`: NUL-delimited output preventing issues with filenames containing spaces, tabs, quotes, or Unicode characters.

## 3. In-Memory Status & Dirty Folder Propagation

Git status codes are mapped onto tree nodes:

- `M`: Modified
- `A`: Added / Staged
- `D`: Deleted
- `U`: Untracked
- `R`: Renamed

### Ancestor Folder Dirty Propagation (`Node.Dirty`)

When a file is modified, its status is recorded on its `Node.Status`. Furthermore, every ancestor folder in its path hierarchy is marked `Dirty: true`:

```go
for p := rel; p != ""; {
    if i := strings.LastIndexByte(p, '/'); i >= 0 {
        p = p[:i]
    } else {
        p = ""
    }
    for i := range children[p] {
        if children[p][i].Dir && isAncestor(children[p][i].Path, rel) {
            children[p][i].Dirty = true
        }
    }
}
```

This enables the file tree in the sidebar to visually highlight collapsed directories that contain modified descendants, allowing developers to immediately spot repository changes.

## 4. Diffing: One Git Call, Two Consumers, Three Views

Both the line gutter and the full diff view are read off the same shell-out, `gitDiff(root, relpath)`:

```bash
git diff --no-color HEAD -- <path>
```

The raw unified diff text is cached at that call site; everything downstream (line-range extraction in Go, and hunk parsing in the browser) is a pure parse of that one string, so a file is never diffed against `HEAD` more than once per request.

### Gutter Change Indicators (`/api/gutter?path=...`)

When viewing a file, the editor displays green, blue, and red markers in the line gutter indicating local edits. `gitHunks(root, relpath)` (`git.go`) runs `gitDiff` and walks its `@@ -l,s +l,s @@` hunk headers and `+`/`-` lines with a small state machine, bucketing every changed line into 1-based **new-file** line numbers:

- `added`: Pure insertions.
- `modified`: Lines replaced (a `-` run immediately followed by a `+` run).
- `deleted`: One marker per pure-deletion run, placed at the new-file line the deletion sat before.

`/api/gutter` returns these three arrays; `web/src/tabs.js` fetches them once per opened tab and `web/src/renderer.js` paints them as `box-shadow` bars (added/modified) or a small wedge (deleted) on the `.g` line-number cell — O(1) per visible row, no re-parsing on scroll.

### File Diff (`/api/diff?path=...`)

`handleDiff` (`server.go`) returns `{ path, diff, available }` — the same raw text `gitDiff` produced, with `available` set whenever it's non-empty (clean or untracked files get `""`). No hunk parsing happens on the server for this endpoint; the client owns that, because it needs two different reshapes of the same hunks (split and unified) and re-parsing client-side avoids two server round trips or two response shapes for one diff.

### Split & Unified Views (`web/src/diff.js`)

The active tab gets a `Source | Diff` switch next to the tab bar (`#diff-switch`, shown only when `d.diffAvailable`) whenever the open file is modified in a git repo. `#diff-source` and `#diff-btn` each show their own view, and hovering the Diff half opens the Split/Unified menu; `Cmd/Ctrl+D` toggles the same thing, resuming whichever layout was used last (`localStorage['px1.diffLayout']`, default `split`). Diff view and the Markdown preview are mutually exclusive — entering one hides the other — and each tab remembers its own state on `d.diffMode` (`'split' | 'unified' | null`).

Unlike the main code view, the diff is **not** rendered through the virtualized `#rows` viewport. A single file's diff is small (bounded by the size of that one file), so `diff.js` renders it as plain DOM into a dedicated `#diffview` overlay — the same overlay-over-`#viewport` pattern the Markdown preview uses (see [Markdown Preview](markdown.md)), just with its own content:

1. **Parse.** `parseDiff(text)` splits the raw diff on `@@ ... @@` hunk headers and walks each hunk's `+`/`-`/context lines once, tagging every row `add` / `del` / `ctx` and carrying its old-file and/or new-file line number. This runs once per file per session; the parsed hunks are cached on `d.diffHunks` so switching Split ↔ Unified re-renders from memory with no re-fetch.
1. **Unified layout.** One row per parsed line: old-line column, new-line column (whichever side doesn't apply is blank), a `+`/`-` marker, and the code — a direct read of `d.diffHunks`, GitHub-"unified"-style.
1. **Split layout.** `pairRows(hunk.rows)` walks each hunk and pairs a deletion run with the addition run immediately following it, index by index, padding the shorter side with a blank cell (`.diff-blank`) — the same replacement-block pairing GitHub's split view uses. Context lines pass straight across both columns unpaired. Each pair renders as one flex row with a left/right half, so the two columns stay vertically aligned for free — no synced-scroll JavaScript, because both halves of a pair are literally the same DOM row.

Every rendered row that exists in the working tree carries its line in `data-l`, on both halves of a split context row; a deleted row carries `data-at`, the working-tree line it sat before. `selbar.js` reads these so a selection anywhere in the diff can drive the selection bar, the right-click menu and Edit with Agent (see [Harness Editing & Agent Dispatch](agent-editing.md)).

Both layouts share the same hunk-header, line-number, marker, and code-cell builders; only the row-shape (one column vs. two) differs, so a fix to how a line renders never needs to be made twice.

## 6. Worktrees

`git worktree list --porcelain` gives px1 every checkout of the repository (`worktrees` in [`git.go`](../../git.go)): path, branch, HEAD, which one is the main checkout, which one is being served, and whether the directory still exists. An agent told to work on a branch frequently runs in one of these, so the files it changed are not in the directory px1 was launched in; without the switcher the review queue simply looks empty.

- **Listing.** `GET /api/worktrees` is read-only and returns an empty list outside a git repository, which hides the switcher in the UI. Entries are ordered newest first, by the modification time of the `.git` *file* `git worktree add` writes into a linked checkout. The main checkout's `.git` is a directory that every git operation touches, so it carries no time and sorts last.
- **Switching.** `POST /api/worktree/switch` accepts only a path git itself lists, so it never becomes a way to open an arbitrary directory. It re-points each root-bound manager through its own `SetRoot` — index, language servers (stopped first: a server is started inside one root and cannot follow), checks, review sessions, decision memory and rules — then rebuilds the index. It refuses while an agent edit is in flight, since that harness is writing into the old checkout.
- **Per-checkout state.** Review sessions, decisions and rules are keyed by workspace root, so each worktree keeps its own baseline, comments, pins and rule hits. Switching therefore never carries one branch's review into another.
- **Starting the review.** An agent given its own worktree is usually finished before anyone opens px1, and a baseline snapshotted at that moment would record the finished work as the starting state. So `autoStartReview` ([`autoreview.go`](../../autoreview.go)) starts the session itself, at launch and after a switch, with `branchBase` — `merge-base HEAD <default branch>` — as the baseline. `reviewManager.StartFromRef` materialises that commit through `git archive` instead of walking the working tree, so committed and uncommitted work both read as changes. Only linked worktrees are started this way: the main checkout holds unrelated work in progress, and calling that "agent changes" would be false. The setting `review.autoStart` turns it off.
- **The gitfile.** In a linked worktree `.git` is a file pointing at the main repository, not a directory, so the index and the review snapshot skip `.git` by name rather than by type.
- **Client.** [`web/src/worktree.js`](../../web/src/worktree.js) fills the header menu and reloads the page after a successful switch: open tabs, the tree and the review queue all belong to the previous checkout.
