<div align="center">
  <img src="assets/px1-eclipse-orbital-banner.png" alt="PX1 eclipse orbital banner" width="100%">
</div>

<div align="center">
  <h1>Understand the PR before you approve it</h1>
</div>

Agents write the PR. px1 helps reviewers understand it.

px1 produces a commit-pinned HTML review report that can be attached to a
GitHub pull request. Reviewers get readable diffs, short explanations beside
changed code, trusted CI evidence, and highlights where changes break rules
stored in the repository.

The core stays self-hosted and useful without an account, SaaS backend, or LLM
API key. Optional AI explanations help with understanding; reviewers still
make the decision.

## How the review works

1. **An agent opens or updates a PR.** Team rules live in `.px1/rules.json`.
2. **px1 generates a report.** The included GitHub Action publishes it through
   GitHub Pages and posts one sticky link on the PR.
3. **The reviewer opens the link.** Structured, line-numbered diff blocks show
   exactly what changed.
4. **Important context stands out.** Rule findings include nearby code, CI
   results are tied to the exact commit, and optional explanation chips clarify
   unfamiliar changes.
5. **The reviewer decides.** Findings can be acknowledged while reviewing;
   approval and change requests remain in GitHub.

Every report belongs to an exact head commit, and older report links remain
available after a PR receives newer commits.

## Team rules

Teams keep review policy in `.px1/rules.json`. Current rules use file globs and
regular-expression patterns with readable messages. For example, a team can
flag direct `fetch` calls and explain that its shared API client must be used.

This makes expectations versioned, visible, and repeatable. Broader semantic
understanding of arbitrary natural-language rules is planned; a pattern match
alone does not prove a semantic violation.

## What is included

- Commit-pinned static review export
- GitHub Pages publishing and a sticky PR link
- Structured highlighted diffs with old/new line numbers
- Contextual team-rule findings on added lines
- Exact-revision GitHub check summaries
- Explainable, model-free attention flags for high-review-risk changes
- Optional, provider-neutral AI explanation chips
- Browser-local finding acknowledgement scoped to the exact revision
- Historical SHA report retention
- Local task-baseline review, comments, patches, and hunk reverts
- File, symbol, and workspace search with Git-aware navigation
- Optional LSP navigation and review-provider handoff
- One local binary, no account, and no required code upload

## Install

The quickest install is through npm (Node 16+):

```bash
npx px1-cli
npm install -g px1-cli
```

The command is `px1`; the package downloads the platform binary and has no
runtime dependencies beyond Node. For a source build, use Go 1.25+ and Node for
the bundled web assets:

```bash
git clone https://github.com/kedarvartak/px1.git
cd px1
make build
install -d ~/.local/bin && install px1 ~/.local/bin/
```

## Use

```bash
px1                    # current workspace
px1 ~/src/project      # another workspace
px1 main.go:42         # open a file at a line
px1 -no-open -port 8080 /workspace
```

### Export a static review report

Export a self-contained report from exact Git commits:

```bash
px1 export-review --base "$BASE_SHA" --head "$HEAD_SHA" --out ./site
```

The exporter reads committed team rules and verification evidence, evaluates
added lines, runs the same attention rules as the local review on the two
commits, and writes `./site/index.html`. The page inlines its styles, script and
IBM Plex fonts (SIL OFL, see `static_report/fonts/OFL.txt`), so it makes no
network requests. It does not execute PR code, call
an LLM, require GitHub credentials, or start a server.

Add `--snapshot-out ./snapshot.json` when CI also needs the versioned JSON
payload for a self-hosted import.

CI may pass `--verification-file` with an exact-revision GitHub Actions report.
A local script or CI job may pass
`--explanations-file ./px1-explanations.json` to add expandable explanation
chips. See the [explanation JSON contract](docs/STATIC_REVIEW_EXPLANATIONS.md).

The included workflow publishes reports at
`reviews/<head-sha>/index.html`, retains older revisions on the generated-only
`px1-review-reports` branch, and updates one PR comment with the current link.

### Import into a self-hosted px1 server

A self-hosted server can accept the same versioned report snapshot from CI.
Set `PX1_IMPORT_TOKEN` on the server, then send an authenticated JSON payload
to `POST /api/review/import`. Successful imports are available at the
commit-pinned `/github/<owner>/<repo>/pull/<number>?sha=<head-sha>` route.

Imports are size-limited, strictly validated, stored outside the repository,
and immutable for a repository, PR, and head SHA. Set
`PX1_REVIEW_LINK_SECRET` as a separate server-side secret to make the returned
self-hosted review link HMAC-signed and reject unsigned links. See the
[self-hosted import contract](docs/SELF_HOSTED_REVIEW_IMPORT.md) for the JSON
shape and CI example. GitHub Pages links remain public static artifacts.

## Local review loop

Start a review from the Review panel before assigning an agent. px1 records the
task baseline, notices later edits, and includes files created or deleted after
the task starts. Reviewers can inspect changes, leave comments, ask a configured
provider to follow up, revert a hunk, and verify imported CI results. A bounded
local pass also calls out dependency, authentication, schema, configuration,
test deletion, generated-code, permission, large-diff, and public-API changes.
Every flag states the matching evidence, opens the diff, and can be dismissed
for the current session; it never blocks approval or claims a change is unsafe.

For remote use, keep px1 on a private network such as Tailscale or WireGuard.
A request is local only when its connected peer and HTTP host are both
loopback. Everything else is read-only: it cannot change review state or
settings, patch files, run an agent, or start a language server. Patch, agent,
and check access are separate flags, and a remote flag does nothing
until `-remote-token` or `PX1_REMOTE_TOKEN` is set. The browser asks for that
token only when a remote capability is on. Loopback (`localhost` or `127.0.0.1`)
keeps local patch and agent access without the token.

```bash
# Private network, read-only. A LAN IP or hostname cannot mutate state or start processes.
px1 -host 0.0.0.0 -port 7777 ~/work/repo

# SSH tunnel. The browser talks to loopback, so this session has local write access.
ssh -L 7777:127.0.0.1:7777 user@host
px1 -host 127.0.0.1 -port 7777 ~/work/repo

# Remote patch only. Agent execution and checks stay off.
PX1_REMOTE_TOKEN='replace-with-a-random-secret' px1 -host 0.0.0.0 -allow-patch -port 7777 ~/work/repo
```

Anyone who can open the URL can still read the workspace and imported reports.
The token authorizes only the capabilities you enabled, for that browser
session. Do not expose the server publicly. px1 does not execute verification
commands; `-allow-checks` only permits an attempt, which is refused because CI
results are imported instead.

## Performance

px1 is built for large repositories. The included benchmark indexes the Linux
kernel corpus (95,710 files / 1.8 GB) in 370 ms with 55 MB RSS. See
[BENCHMARKS.md](BENCHMARKS.md) for methodology.

## Development

```bash
make test
go run . -dev . .
make dist
```

See [CONTRIBUTING.md](CONTRIBUTING.md), the
[engineering documentation](docs/internals/README.md), the
[roadmap](ROADMAP.md), and [release instructions](PUBLISHING.md).

## License

[MIT](LICENSE) © 2026 Arpit Bhayani. px1 is a fork of
[px0](https://github.com/px0-ai/px0); upstream notices are retained.
