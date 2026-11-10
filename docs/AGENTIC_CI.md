# Agentic review in CI

`px1 post-github` and `agentic-action/` run the agentic reviewer on every pull request and comment on the result, like a hosted reviewer, without a hosted service. Each repository carries a workflow; the key stays in that repository's secrets.

## Set up a repository

1. Copy `examples/px1-agentic-review.yml` to `.github/workflows/` in the repository.
2. Add a repository secret `OPENAI_API_KEY` (Settings, Secrets and variables, Actions). Never put the key in the workflow file.
3. Open a pull request.

## What it posts

- One **sticky summary comment**, found by a hidden marker and edited on every push instead of adding a new one. With no findings it says so, and that this is not evidence the change is correct.
- **Inline comments** on the changed lines for findings that fall inside the diff, at most 20 per run. A finding already commented on an earlier push is not posted again.
- Findings outside the diff stay in the summary, with their evidence.

## The report

Each run uploads the report as one unzipped `px1-review.html` artifact (kept 14 days), and the summary comment links straight to it. GitHub serves artifacts from its own storage, and only people who can read the repository can open the link; whether the browser shows it or saves it is up to GitHub. The report contains your code. Raw model transcripts (`private/`) are never uploaded.

## Safety

- Runs on `pull_request`, never `pull_request_target`. Pull requests from forks are skipped: they get no secrets and a read-only token.
- The model reads the diff and any file it asks for at that commit through read-only git. Secret files and key-shaped values are blocked or redacted; this is detection, not access control.
- `max-cost` is a hard per-run cap checked before every model call, using the prices you supply.
- `continue-on-error: true` in the example keeps a failed or skipped review from blocking a merge.
- The title and description of the pull request are passed as environment variables, never interpolated into a shell script.

## Commands

    px1 post-github --findings findings.json --pr 7 [--repo owner/name] [--head SHA] [--footer TEXT] [--dry-run]

`GITHUB_TOKEN` is read from the environment only. The workflow needs `pull-requests: write`.

## Known limits

- Very large pull requests exceed the model budget and fail; there is no fallback yet.
- A policy rejection from the model provider is not retried yet.
