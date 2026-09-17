# px1

px1 is a fast, lightweight human control plane for coding agents. It gives you an immediate browser view of a worktree, then helps you inspect, direct, correct, and verify agent-generated changes without turning into another full editor.

## Human control over agent changes

Agents write code; humans decide whether it is ready. px1 keeps reading and navigation fast, while adding a durable review workflow: task baselines, review state, targeted corrections, agent feedback, and evidence that checks ran against the version under review.

### Where px1 fits in best:

- **Reviewing agent output**: Start a review session, inspect the exact delta since the task began, send precise feedback, and return directly to what needs another look.
- **Correcting without context switching**: Make a narrow patch or restore an incorrect agent change to the task baseline, then go back to review.
- **Verifying work on remote machines**: Inspect code and agent changes on a server or runner with a small native process and no remote desktop.
- **Remote & Cloud Server Inspection**: Spin up on any remote server, VM, or CI runner and browse the codebase instantly from your local browser—no SSH keys, no port forwarding hassle, and no heavy remote desktop/daemons.
- **Auditing Large Repositories**: Read through massive, 50,000+ file codebases on a laptop without background indexers hogging RAM or spinning up fans.
- **Sidecar to Terminal Editors**: Keep lightweight editors (like Vim, Neovim, or Helix) in the terminal for typing, while using px1 as a high-density, rich graphical inspection and diff console.

### A five-minute review loop

Start **Review** before asking your coding agent to work. When it finishes, px1 queues only the files that changed during that task. Open **Next change**, inspect the task-baseline diff, mark the file reviewed, and use **Comment**, **Patch**, **Revert hunk**, or **Ask agent** when something needs attention. Run a configured check before approving the final queue.

## Installation

### Quick Install (macOS, Linux, BSD)

Releases will be published from this repository. Until then, build from source.

### Build from Source

Requires Go 1.25+. No npm, node, CGO, or external dependencies:

```bash
git clone https://github.com/kedarvartak/px1.git
cd px0
make build
install -d ~/.local/bin && install px1 ~/.local/bin/
```

To cross-compile binaries for all supported platforms:

```bash
make dist
```

## Features

- **Blazing Fast Navigation**: Fuzzy file search (`Cmd/Ctrl+P`), symbol outline (`Cmd/Ctrl+Shift+O`), and workspace regex search (`Cmd/Ctrl+Shift+F`) in milliseconds.
- **Remote-First, Zero SSH Hassle**: Spin up on any remote server, cloud instance, or runner in < 1 ms. Inspect remote code in your local browser over a single port (Tailscale, WireGuard, reverse proxy, or tunnel) without SSH key setups, port forwarding churn, or remote extension daemons.
- **Rich Syntax Highlighting**: Native tokenization for ~280 languages via Chroma with windowed rendering.
- **Git Awareness & Visual Diffs**: Status badges (`M`, `A`, `D`, `U`, `R`), dirty folder ancestry propagation, changed-files filter, and side-by-side / unified diffs vs `HEAD` (`Cmd/Ctrl+D`).
- **Review Sessions**: Capture a recoverable task baseline outside the repository, detect files changed since review began, and restore the workspace to that baseline when needed.
- **Human Review Cockpit**: Review only task-session changes, open task-baseline diffs, mark files reviewed, leave line-anchored feedback, ask the local agent to address current comments, make a narrow Patch Mode correction, undo it, or restore a normal hunk to the task baseline.
- **Decision Pins**: Every non-obvious choice an agent made (library, data shape, security, limits) appears as a short pin on the lines it affects, ranked by impact. Expand a pin to see why and the options not taken, then **Accept**, **Ask**, or **Switch** to an alternative and let the agent rework the change.
- **Revision-Linked Verification**: Run explicitly configured tests, lint, or typechecks from the review queue. Results become stale if reviewed files change after the run.
- **Edit with Your Coding Agent**: Select code in the source or diff view, right-click (or `Alt+E`), and describe the change. px1 runs Claude Code, OpenCode, OpenAI Codex, Antigravity, Aider, Goose, Gemini CLI, or Cursor Agent on it, reloads what changed, and shows harness errors inline. Several edits can run at once, as long as their line ranges don't overlap.
- **Rendered Markdown Preview**: Full GFM preview with Chroma-highlighted code fences; switch between preview and source with `Alt+M` while preserving scroll.
- **Custom Themes**: 14 built-in themes (GitHub Dark, Tokyo Night, Catppuccin, Dracula, Gruvbox, Nord, Solarized, and more).
- **Optional Language Server Protocol (LSP)**: Zero-config auto-detection (`gopls`, `rust-analyzer`, `pyright`, `typescript-language-server`, `clangd`) for Go-to-Definition (`F12`), Hover, references, and call trails. Falls back automatically to regex outlines.
- **Settings & Configuration Modal**: Press `Cmd/Ctrl+,` or click the ⚙️ icon in the status bar to open the VS Code-style Settings editor. Configure editor typography, cursor styles, diff modes, themes, search behavior, file exclusions, and coding agents with live preview and raw JSON synchronization (`~/.px1/settings.json`).
- **Virtual DOM / Zero Overhead**: Opening a 400,000-line file costs the same as a 10-line file; only visible rows are mounted. Reclaims memory after 15 seconds of inactivity.
- **Completely Self-Contained**: Single static binary embedding all web assets. Zero runtime dependencies, no Electron, no Node, no cloud phone-homes.

## Language Server (LSP) Setup (Optional)

`px1` works fully out of the box without language servers using built-in fuzzy search and regex outlines.

When installed, language servers provide semantic Go-to-Definition (`F12`), hover types/docs, and call trails. px1 auto-detects servers on your `PATH` or standard install directories:

| Language | Server | Quick Install |
| --- | --- | --- |
| Go | `gopls` | `go install golang.org/x/tools/gopls@latest` |
| Rust | `rust-analyzer` | `rustup component add rust-analyzer` |
| TypeScript / JavaScript | `typescript-language-server` | `npm install -g typescript-language-server typescript` |
| Python | `pyright` / `pylsp` / `ruff` | `npm install -g pyright` or `pipx install python-lsp-server` |
| C / C++ | `clangd` | `sudo apt install clangd` or `brew install llvm` |
| Zig | `zls` | `brew install zls` or [zigtools/zls](https://github.com/zigtools/zls) |
| Lua | `lua-language-server` | `brew install lua-language-server` |
| Ruby | `solargraph` | `gem install solargraph` |
| Java | `jdtls` | `brew install jdtls` |
| C# | `omnisharp` | Install OmniSharp on `PATH` |
| LaTeX | `texlab` | `brew install texlab` |

Servers spawn lazily on first request and shut down cleanly upon exit. Disable with `px1 -no-lsp`. You can also click **LSP: set up** in the status bar to view or trigger automatic installation for your OS.

## Editing with a Coding Agent (Optional)

px1 does not have a text editor. It hands changes to a coding agent already installed on your machine, then reloads what the agent changed.

| Harness | Default Model | Command px1 runs |
| --- | --- | --- |
| Claude Code | `haiku` | `claude --permission-mode acceptEdits --model haiku -p {prompt}` |
| Gemini CLI | `gemini-2.5-flash-lite` | `gemini --approval-mode auto_edit -m gemini-2.5-flash-lite -p {prompt}` |
| Cursor Agent | `gemini-3.6-flash-minimal` | `cursor-agent --force --model gemini-3.6-flash-minimal -p {prompt}` |
| Antigravity | `gemini-3.6-flash-low` | `agy --dangerously-skip-permissions --mode accept-edits --model gemini-3.6-flash-low -p {prompt}` |
| OpenCode | `opencode/big-pickle` | `opencode run -m opencode/big-pickle {prompt}` |
| OpenAI Codex | `gpt-5.6-sol` | `codex exec --approve-for-me -m gpt-5.6-sol {prompt}` |
| Aider | `claude-3-7-sonnet` | `aider --yes-always --no-auto-commits --model claude-3-7-sonnet --message {prompt}` |
| Goose | `gpt-4o` | `goose run --no-session --model gpt-4o -t {prompt}` |

By default, px1 selects the least capable (fastest and most economical) model for each harness, and allows you to choose any available model from the harness menu.

### How an Edit Works

1. Select code in the source view or the git diff view (split or unified, either side).
1. Pick **Edit with Agent** from the right-click menu, the footer selection bar, or press `Alt+E`.
1. The first time, choose a harness (and optional model). The choice is remembered in `~/.px1/settings.json` (or `$XDG_CONFIG_HOME/px1/settings.json`), never inside your repository.
1. Type what should change and press `Enter`. px1 sends the harness the instruction, the file and line range, and the selected lines.
1. As the agent runs, its progress and actions stream in real time to the terminal stdout where px1 was launched.
1. When the harness exits, px1 reloads the files it changed. Each tab stays in the view it was in: source stays source, diff stays diff.

The footer always shows the harness and model in use (**Agent: agy (gemini-3.6-flash-low)**). Click it to switch harnesses or choose a different model.

### When Something Goes Wrong

If the harness fails, the error appears inline under your instruction together with the harness's stdout and stderr, which usually say why (for example an invalid API key). Nothing is lost: the composer stays open with your instruction.

### Guards

- Several edits can run at once, each in its own box, as long as their line ranges don't overlap. A range that overlaps an edit already in flight is refused: two harnesses rewriting the same lines would produce a result nobody could review.
- Closing the tab while an edit is still running asks for confirmation first, so a harness is never abandoned mid-write with no way to see how it went.
- Edits are accepted only from px1's own page, opened by IP address or `localhost`. Through a hostname (reverse proxy, tunnel domain) they are refused. Anyone who can reach px1 by IP can run the harness as you, so keep `-host 0.0.0.0` to private networks.
- Nothing runs until you pick a harness. `-agent` pins one for the session; `-no-agent` turns editing off.

## Settings & Configuration (`settings.json`)

px1 provides a built-in Settings editor modeled after VS Code. Settings are stored per-user in `~/.px1/settings.json` (or `$XDG_CONFIG_HOME/px1/settings.json`), keeping your workspace repository clean.

### Opening Settings
- Press **`Cmd+,`** (macOS) or **`Ctrl+,`** (Linux/Windows).
- Click the **⚙️ Settings** button in the bottom status bar.
- Open the Command Palette (`Cmd/Ctrl+Shift+P`) and choose **Preferences: Open Settings (UI)** or **Preferences: Open Settings (JSON)**.

### Features
- **UI & Raw JSON Modes**: Switch between the graphical form editor and raw JSON mode with syntax validation and live synchronization.
- **Interactive Attribute Tags & Pills**: Every setting is tagged with its category, type, current active value, default value, and interactive pill buttons for allowed values (e.g. `[line]`, `[block]`, `[underline]` for cursor styles; `[true]`, `[false]` for toggles; numeric ranges and presets). Clicking any pill applies that value immediately.
- **Live Preview Without Reload**: Font sizes, line heights, cursor animations, themes, word wrapping, diff layouts, and git gutter indicators apply in real time without refreshing the page.
- **One-Click Reset**: Any modified setting displays a `Modified` badge and a `Reset` button to restore its factory default.

### Key Configurable Settings

| Setting Key | Default | Allowed Values / Options | Description |
| --- | --- | --- | --- |
| `editor.fontSize` | `13.5` | `9.0` – `32.0` (px) | Viewer font size |
| `editor.fontFamily` | JetBrains Mono stack | CSS font stack | Viewer font family stack |
| `editor.lineHeight` | `21.0` | `14.0` – `48.0` (px) | Viewer line height |
| `editor.tabSize` | `4` | `2`, `4`, `8` | Number of spaces per tab |
| `editor.wordWrap` | `"on"` | `"on"`, `"off"` | Soft wrap lines at editor boundary |
| `editor.lineNumbers` | `"on"` | `"on"`, `"off"` | Line numbers in gutter |
| `editor.cursorStyle` | `"line"` | `"line"`, `"block"`, `"underline"` | Cursor style |
| `editor.cursorBlinking` | `"smooth"` | `"blink"`, `"smooth"`, `"solid"` | Cursor animation style |
| `editor.renderLineHighlight` | `"line"` | `"line"`, `"none"` | Current line highlight |
| `editor.occurrencesHighlight` | `true` | `true`, `false` | Highlight occurrences of selected word |
| `editor.scrollBeyondLastLine` | `true` | `true`, `false` | Allow scrolling past file end |
| `editor.bracketPairColorization` | `true` | `true`, `false` | Rainbow bracket pairs and bracket matching |
| `workbench.colorTheme` | `"github-dark"` | 14 built-in themes | Workbench color theme |
| `diffEditor.renderSideBySide` | `true` | `true`, `false` | Split vs. unified diff view |
| `diffEditor.ignoreTrimWhitespace` | `true` | `true`, `false` | Ignore leading/trailing whitespace diffs |
| `git.gutterIndicators` | `true` | `true`, `false` | Gutter change indicators |
| `explorer.compactFolders` | `true` | `true`, `false` | Collapse single-child directory chains |
| `explorer.autoReveal` | `true` | `true`, `false` | Auto-scroll to active file in tree |
| `files.exclude` | `**/.git, **/node_modules...` | Glob patterns | Exclude patterns from trees and searches |
| `search.smartCase` | `true` | `true`, `false` | Case-insensitive when lowercase; sensitive when uppercase |
| `search.maxResults` | `1000` | `50` – `10000` | Maximum search results |
| `lsp.enabled` | `true` | `true`, `false` | Master switch for language servers |
| `lsp.hover.enabled` | `true` | `true`, `false` | Hover documentation cards |
| `agent.harness` | `""` | `claude`, `gemini`, `agy`, etc. | Preferred coding agent harness |
| `agent.timeoutSeconds` | `120` | `10` – `600` (s) | Max execution time for agent edits |
| `agent.autoAcceptEdits` | `false` | `true`, `false` | Auto-confirm agent diffs |
| `telemetry.enabled` | `true` | `true`, `false` | Anonymous usage metrics |

### Review verification commands

Verification commands are opt-in and live only in your user settings. Add named commands through **Settings → JSON**; px1 never accepts a shell command from the browser request itself.

```json
{
  "verification.commands": {
    "Tests": "go test ./...",
    "Lint": "golangci-lint run",
    "Typecheck": "npm run typecheck"
  }
}
```

Start a review session, open **Review** in the sidebar, and run a configured check. px1 records the review revision that was tested and labels a completed result **Stale** if the session changes afterward.

### Decision pins

Click **Explain changes** in the review queue and the selected harness reads the task-baseline diff and pins each decision it made to the exact lines, without editing files. A harness dispatched from your own terminal can also push pins while it works:

```sh
curl -s -X POST http://127.0.0.1:7777/api/review/pins \
  -H 'Origin: http://127.0.0.1:7777' \
  -d '{"pins":[{"path":"auth/login.go","lineStart":18,"lineEnd":19,"decision":"JWT in httpOnly cookie","why":"stateless; API is already REST","alternatives":["server sessions","localStorage"],"impact":3}]}'
```

`impact` is 1 (low) to 3 (high). A pin whose lines change afterwards is marked **lines changed** and can no longer be switched.


## Why a Dedicated Code Viewer?

Traditional IDEs carry tens of thousands of authoring features, Electron runtimes, background indexers, and gigabytes of memory overhead. In modern AI-assisted workflows, developers spend significantly more time reviewing code than typing it.

| Parameter | Traditional IDE (e.g., VS Code) | px1 (Code Viewer) |
| --- | --- | --- |
| Primary Purpose | Manual code authoring & plugin host | Instant code reading & navigation |
| Base Memory (RSS) | ~1,440 MB (1.4+ GB) | ~20 MB (~70x lighter) |
| Active Startup CPU Spike | 35% - 50% | < 1% |
| Cold Startup Time | Several seconds | Sub-millisecond |
| Process Tree | 15+ Node.js/Electron processes | 1 single static Go binary |
| Workspace Indexing | Multi-second background churn | 0 - 45 ms for entire repositories |
| Setup and Config | Config files, plugins, node, npm | Zero config, zero runtime |

## Key Numbers and Benchmarks

All metrics are measured on real-world repositories and reproducible using [`./benchmark.sh`](benchmark.sh).

### Real Corpus Performance (px1 standalone)

| Repository   | Source Size | Files Indexed | Index Time | Fuzzy Search | Full-Tree Regex Scan | Resident RAM (RSS) |
| ------------ | ----------- | ------------- | ---------- | ------------ | -------------------- | ------------------ |
| flask        | 3 MB        | 235           | 1 ms       | 0.8 ms       | 2.3 ms               | 20 MB              |
| redis        | 26 MB       | 1,855         | 13 ms      | 1.0 ms       | 18.2 ms              | 17 MB              |
| react        | 63 MB       | 7,178         | 52 ms      | 2.7 ms       | 32.2 ms              | 21 MB              |
| django       | 74 MB       | 7,014         | 39 ms      | 1.3 ms       | 26.8 ms              | 20 MB              |
| kubernetes   | 370 MB      | 25,926        | 150 ms     | 13.5 ms      | 84.6 ms              | 30 MB              |
| TypeScript   | 414 MB      | 66,533        | 566 ms     | 6.2 ms       | 150.3 ms             | 69 MB              |
| linux kernel | 1,809 MB    | 95,710        | 370 ms     | 6.0 ms       | 451.8 ms             | 55 MB              |

### Head-to-Head: px1 vs. VS Code

Run `./benchmark.sh --vscode .` to measure both on your active machine:

```text
### px1 vs. VS Code Comparison

| Metric / Parameter | px1                    | VS Code (Server/Remote) | Notes                   |
| ------------------ | ---------------------- | ----------------------- | ----------------------- |
| Memory (RSS)       | 20 MB                  | 1,166 - 1,440 MB        | ~70x lighter            |
| Instant CPU %      | 0.0%                   | 4.0% - 39.0%            | Minimal CPU churn       |
| Index Time         | < 1 ms                 | ~4 - 10 s               | px1 is instantaneous    |
| Process Count      | 1 single Go binary     | 15+ processes           | Multi-process Node tree |
```

## Usage

Run `px1` with an optional file or directory:

```bash
px1                     # view current workspace
px1 ~/src/kernel        # view another repository
px1 web/src/main.js     # view a file in its project workspace
px1 main.go:42          # open directly to a line number
```

### Remote & Cloud Workspaces

Spin up on any remote server, VM, or container and view code directly in your local browser without SSH shell management, X11 forwarding, or remote extension daemons:

```bash
# Bind all interfaces on a remote machine / cloud instance
px1 -host 0.0.0.0 -port 7777 ~/work/repo

# Headless / server mode without opening local browser
px1 -no-open -port 8080 /workspace

# In Docker / CI runner
docker run -p 7777:7777 -v $(pwd):/src px1:latest
```

Access securely over Tailscale, WireGuard, reverse proxy, or Cloudflare Tunnel with zero remote setup overhead and sandboxing (path traversal protection & DNS rebinding checks). Anyone who can reach px1 can dispatch agent edits when it is opened by IP address (for example over Tailscale), so bind to a private network. Opened through a hostname, such as a reverse proxy or tunnel domain, editing is refused.

### Updating px1

To check for updates and automatically upgrade `px1` to the latest release:

```bash
px1 --update
```

`px1` also checks asynchronously in the background once every 24 hours without delaying startup (<1 ms) and notifies you on stderr when an update is available.

### CLI Flags

| Flag         | Default     | Description                                                     |
| ------------ | ----------- | --------------------------------------------------------------- |
| `-port N`    | `7777`      | Port to listen on (`0` picks an ephemeral free port)            |
| `-host H`    | `127.0.0.1` | Local address to bind                                           |
| `-no-open`   | `false`     | Do not launch the web browser automatically                     |
| `-no-lsp`    | `false`     | Disable language server discovery and use regex-based outline   |
| `-no-git`    | `false`     | Disable git awareness (tree status badges and the diff view)    |
| `-agent H`   | none        | Pin the coding harness for edits: `claude`, `gemini`, `cursor-agent`, `agy`, `opencode`, `codex`, `aider`, `goose`, or a command template containing `{prompt}` |
| `-no-agent`  | `false`     | Do not offer editing through a coding harness                   |
| `-no-telemetry` | `false`  | Disable anonymous usage telemetry                               |
| `-no-color`  | `false`     | Strip ANSI escape sequences from terminal output                |
| `-quiet`     | `false`     | Suppress CLI narration (errors still print to stderr)           |
| `-update`    | `false`     | Check for updates and install the latest version                |
| `-version`   | `false`     | Print version and architecture and exit                         |

## Keyboard Shortcuts

`Cmd` on macOS, `Ctrl` on Windows and Linux; `Alt` is `Option` on a Mac. The in-app sheet (`?`), footer hints and tooltips show each key the way your keyboard labels it (`Cmd+Shift+F` on a Mac, `Ctrl+Shift+F` elsewhere).

| Key                                                    | Action                                                                                     |
| ------------------------------------------------------ | ------------------------------------------------------------------------------------------ |
| `Cmd/Ctrl+,`                                           | Open Settings (UI)                                                                         |
| `Cmd/Ctrl+K`                                           | Universal palette / quick open                                                             |
| `Cmd/Ctrl+P`                                           | Go to file                                                                                 |
| `Cmd/Ctrl+Shift+P`                                     | Command palette                                                                            |
| `Cmd/Ctrl+Shift+O`                                     | Go to symbol in file                                                                       |
| `Cmd/Ctrl+Shift+F`                                     | Full workspace search                                                                      |
| `Cmd/Ctrl+F`                                           | Find in active file (seeded with the current editor selection)                             |
| `Cmd/Ctrl+G`                                           | Jump to line                                                                               |
| `Cmd/Ctrl+D`                                           | Toggle git diff of the active file (split or unified, whichever you used last)             |
| `F12`, `Cmd/Ctrl+Click`                                | Go to definition                                                                           |
| `Shift+F12`                                            | Find all references                                                                        |
| `Left` / `Right`, `Home` / `End` (`Cmd+Left` / `Cmd+Right` on macOS) | Move the caret along the line; click places it                                             |
| `Ctrl+Home` / `Ctrl+End` (`Cmd+Up` / `Cmd+Down` on macOS)            | Top / bottom of file                                                                       |
| `Alt+Z` / `Alt+L`                                      | Toggle word wrap / line numbers                                                            |
| `Alt+C` / `Alt+A` / `Alt+U`                            | With code selected: copy reference / copy with context / find usages                          |
| `Alt+E`                                                | With code selected: edit with your coding agent                                            |
| `Right click`                                          | On a selection: the same actions in a menu at the pointer                                  |
| `Alt+Shift+H`                                          | Call trail: callers and callees of the function under the cursor, expandable level by level|
| `Hover`                                                | Type signature & doc hover                                                                 |
| `Cmd/Ctrl + Hover`                                     | Inspect identifier link                                                                    |
| `Alt+Left` / `Alt+Right`                               | Navigate back / forward in history                                                         |
| `Cmd/Ctrl+B`                                           | Toggle file tree sidebar                                                                   |
| `Alt+W`                                                | Close active tab (`Cmd/Ctrl+W` too, where the browser lets a page have it)                 |
| `Ctrl+Tab`                                             | Switch to next tab                                                                         |
| `Alt+1` ... `Alt+9`                                    | Select tab by position                                                                     |
| `?`                                                    | Show all keyboard shortcuts                                                                |

## Philosophy and Design Principles

- **Human control over agent changes**: px1 is a review and intervention surface. Agents implement; people inspect, leave feedback, apply narrow corrections, restore a known baseline, and verify the resulting work.
- **Read fast, intervene precisely**: px1 remains a compact navigator rather than a general-purpose editor. Any write must be intentional, bounded, previewable, and reversible.
- **Remote-First & SSH-Free**: Works seamlessly whether inspecting a local directory or a cloud instance over Tailscale/VPN—no remote daemons, no X11 forwarding, and no SSH session maintenance.
- **Private & Sandboxed**: Zero accounts, zero cloud dependencies. Code and queries stay on the running machine. Protected by path traversal guards and DNS rebinding prevention.
- **Reclaims Memory**: Automatically recovers memory after 15 seconds of inactivity so idle sessions don't hoard host RAM.

### Telemetry & Privacy

px1 collects lightweight, anonymous backend session metrics (via PostHog) strictly to calculate DAU/MAU and session duration (start time and stop time).

**What is NEVER collected:**
- No feature interactions, user actions, or command activity
- No frontend events, browser fingerprinting, or client trackers
- No code snippets, file contents, or diffs
- No file names, directory paths, or repository names
- No symbol names, function signatures, or search queries
- No personal data or user accounts

**How to opt out:**
You can disable telemetry completely at any time through any of the following:
- CLI flag: `px1 -no-telemetry`
- Environment variable: `export DO_NOT_TRACK=1` or `export PX1_TELEMETRY=0`


## Reproducing Benchmarks

All benchmark figures can be measured directly on your own system:

```bash
# 1. Fetch benchmark corpus (~3 GB shallow clones of Linux, K8s, TypeScript, etc.)
./benchmark.sh --clone

# 2. Run the full benchmark suite
./benchmark.sh

# 3. Compare px1 directly against VS Code process tree on your workspace
./benchmark.sh --vscode .

# 4. Profile memory lifecycle across index, search, and idle recovery
./benchmark.sh --memory bench-repos/linux

# 5. Measure LSP latency (definition, hover, references)
./benchmark.sh --lsp .
```

See [Performance Benchmarks](BENCHMARKS.md) for full methodology and detailed charts.

## Contributing

Contributions that keep px1 fast, minimal, and dependable are welcome. Please read [CONTRIBUTING.md](CONTRIBUTING.md) before submitting issues or pull requests.

### Development Workflow

1. Clone the repository:
  ```bash
  git clone https://github.com/kedarvartak/px1.git
  cd px0
  ```
1. Run tests:
  ```bash
  make test
  # or go test ./...
  ```
1. Live frontend development (serves `web/` assets from disk without rebuilding the binary):
  ```bash
  go run . -dev . .
  ```
1. Verify CLI formatting and builds:
  ```bash
  go vet ./...
  make dist
  ```

### Architecture & Internals

For comprehensive technical deep-dives into the architecture, indexing, virtualized rendering, syntax highlighting, and LSP subsystems, see the [Internals Documentation](docs/internals/README.md).

- `main.go` / `ui.go`: CLI entrypoint, flag parsing, signal management, Ape terminal experience.
- `update.go`: Self-updater and asynchronous daily version check.
- `server.go`: HTTP routes, JSON API, gzip compression, and embedded asset serving.
- `index.go`: Concurrently walks workspace, honors `.gitignore` (ignored files stay visible but dimmed in the explorer, and are never indexed or searched), builds in-memory path and trie structures in milliseconds.
- `search.go` / `fuzzy.go`: High-performance substring and fuzzy file/symbol matching algorithms.
- `lsp.go` / `lspnav.go` / `calls.go`: Lightweight JSON-RPC client communicating with local language servers over stdio, plus definitions, references and call trails.
- `lspservers.go` / `lspsetup.go`: Language server registry, discovery, and install on request.
- `agent.go` / `settings.go`: Coding harness discovery and dispatch, change detection, user configuration store (`~/.px1/settings.json`), and settings schema validation.
- `web/`: Native zero-dependency ES module frontend (custom virtual scroll, syntax highlight rendering, tab manager).
- `web/themes/`: One CSS file per colour theme, joined by the server into `/static/themes.css`. Token reference in [Styling & Themes](docs/internals/styling-and-themes.md).

## License

[MIT License](LICENSE) (c) 2026 Arpit Bhayani

px1 is a fork of [px0](https://github.com/px0-ai/px0). Upstream copyright and
permission notices are retained in accordance with the MIT License.
