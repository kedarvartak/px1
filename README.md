

<div align="center"><pre>
██████╗ ██╗  ██╗ ██╗
██╔══██╗╚██╗██╔╝███║
██████╔╝ ╚███╔╝ ╚██║
██╔═══╝  ██╔██╗  ██║
██║     ██╔╝ ██╗ ██║
╚═╝     ╚═╝  ╚═╝ ╚═╝
</pre></div>

px1 is a fast, local control plane for reviewing coding-agent work. It opens any worktree in a browser and keeps the human in charge of what changes, why it changed, and whether it is ready.

## The moat

Most tools help an agent write code. px1 makes its output reviewable over time.

- **Task baselines, not just Git diffs.** Start a review before work begins; px1 queues the exact changes made for that task and can restore them to the baseline. Review diffs include files added or deleted after the task starts, even when Git has no HEAD diff for them.
- **Plain-language explanations.** Decision pins explain important changes in context and can be acknowledged within the active review.
- **Team rules.** Turn review feedback into explicit `.px1/rules.json` policy. px1 checks later agent changes against versioned rules, so hard-won feedback does not disappear in chat history.
- **Evidence tied to the revision.** Display trusted CI test, lint, and typecheck results for the reviewed commit. Results are rejected when they belong to a different revision.
- **Fast enough to stay open.** A single static Go binary gives instant navigation, virtualized large-file viewing, Git diffs, and remote inspection without an IDE, cloud account, or background indexer.

Agents implement. px1 preserves human judgment.

## Review loop

Start a review from the Review panel before assigning the harness, then use Claude, Codex, Gemini, or any other tool normally. px1 notices external edits in the browser, so no wrapper, plugin command, or manual re-index is required. Existing local work is part of the baseline; only later changes enter the queue. For an agent worktree opened after work has started, enable `review.autoStart` to opt into the local worktree baseline behavior. Review diffs are based on the task snapshot rather than HEAD, including newly created files.

1. Start px1, open **Review**, and capture the baseline before assigning the task.
2. Search or filter the review queue by filename and state, then inspect each task-baseline diff with **Next change**.
3. Leave feedback, ask the configured provider to revise, or revert a hunk.
4. Inspect the imported CI verification result; approve only the revision it verified.

## Install

The quickest install is through npm (Node 16+):

```bash
npx px1-cli
npm install -g px1-cli
```

The command is `px1`; the package downloads the platform binary and has no runtime dependencies beyond Node. For a source build, you need Go 1.25+ and Node for the bundled web assets:

```bash
git clone https://github.com/kedarvartak/px1.git
cd px1
make build
install -d ~/.local/bin && install px1 ~/.local/bin/
```

## Demos

[CLI to review](output/demos/cli-to-review.mp4) shows the npm-installed command opening a workspace and moving from the terminal into review. A [WebM version](output/demos/cli-to-review.webm) is included for browsers that prefer it.

## Use

```bash
px1                    # current workspace
px1 ~/src/project      # another workspace
px1 main.go:42         # open a file at a line
px1 -no-open -port 8080 /workspace
```

For a remote machine, bind to a private network and access it over Tailscale, WireGuard, or a tunnel:

```bash
px1 -host 0.0.0.0 -port 7777 ~/work/repo
```

Anyone able to reach px1 by IP can dispatch configured agent edits as you. Keep remote instances on a private network; agent editing is intentionally refused through hostnames such as reverse proxies and tunnel domains.

## What is included

- File, symbol, and workspace search; Git-aware tree and diffs; Markdown preview
- Optional local LSP navigation and hover, with a regex-outline fallback
- Optional review-provider handoff for Claude Code, Codex, Gemini CLI, Cursor Agent, OpenCode, Aider, Goose, and custom commands
- Revision-pinned CI verification results, themes, and other user-local preferences
- Local-first operation: one binary, no account, no code upload

## Performance

px1 is built for large repositories and remote machines: it indexes the Linux kernel corpus (95,710 files / 1.8 GB) in 370 ms with 55 MB RSS in the included benchmark. Run `./benchmark.sh` to reproduce results; see [BENCHMARKS.md](BENCHMARKS.md) for methodology.

## Development

```bash
make test
go run . -dev . .
make dist
```

See [CONTRIBUTING.md](CONTRIBUTING.md) for contribution guidance and [docs/internals](docs/internals/README.md) for architecture details.
See the [roadmap](ROADMAP.md) for the planned GitHub-connected review flows.

## License

[MIT](LICENSE) © 2026 Arpit Bhayani. px1 is a fork of [px0](https://github.com/px0-ai/px0); upstream notices are retained.
