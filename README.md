

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

- **Task baselines, not just Git diffs.** Start a review before work begins; px1 queues the exact changes made for that task and can restore them to the baseline.
- **Decision memory.** Plain-language decision pins explain important changes in context. Accepted decisions persist per workspace and later changes that reverse them are flagged for review.
- **Review memory.** Turn a review comment into a reusable rule. px1 checks later agent changes against it, so hard-won feedback does not disappear in chat history.
- **Evidence tied to the revision.** Run your configured tests, lint, or typechecks from the review queue. Results go stale when the reviewed files change.
- **Fast enough to stay open.** A single static Go binary gives instant navigation, virtualized large-file viewing, Git diffs, and remote inspection without an IDE, cloud account, or background indexer.

Agents implement. px1 preserves human judgment.

## Review loop

Agents usually make their own `git worktree` and start editing straight away. px1 handles that: the workspace name in the sidebar lists every checkout, newest first, and opening one starts its review automatically with the commit checked out when that worktree was created as the baseline. Only changes made in that worktree, committed or not, are in the queue no matter how late you arrive. The main checkout is left alone, since that is where your own work in progress lives; turn the behaviour off with `review.autoStart` in settings.

1. Start **Review** before assigning the task, or let px1 start it when you open the agent's worktree.
2. Inspect each task-baseline diff with **Next change**.
3. Leave feedback, ask the agent to revise, make a narrow patch, or revert a hunk.
4. Run a configured check; approve only the revision it verified.

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
- Coding-agent handoff for Claude Code, Codex, Gemini CLI, Cursor Agent, OpenCode, Aider, Goose, and custom commands
- Configurable verification commands, themes, and user-local settings
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

## License

[MIT](LICENSE) © 2026 Arpit Bhayani. px1 is a fork of [px0](https://github.com/px0-ai/px0); upstream notices are retained.
