---
title: Agentic review in CI
section: guides
kind: narrative
status: shipped
covers:
  - post_github.go
  - feedback_collect.go
  - agentic-action/
  - examples/px1-agentic-review.yml
  - .github/workflows/px1-agentic-review.yml
verified_at:
---

# Agentic review in CI

`px1 post-github` and `agentic-action/` run the agentic reviewer on every pull request and comment on the result, like a hosted reviewer, without a hosted service. Each repository carries a workflow; the key stays in that repository's secrets.

## Set up a repository

1. Copy `examples/px1-agentic-review.yml` to `.github/workflows/` in the repository.
2. Add a repository secret `OPENAI_API_KEY` (Settings, Secrets and variables, Actions). Never put the key in the workflow file.
3. Keep `ref:` in the workflow pinned to a full commit SHA. The px1 checkout runs with your API key in its environment, so never point it at a branch. To update, read the px1 changes first, then change the SHA.
4. Open a pull request.

## What it posts

- One **sticky summary comment**, found by a hidden marker and edited on every push instead of adding a new one. With no findings it says so, and that this is not evidence the change is correct.
- **Inline comments** on the changed lines for findings that fall inside the diff, at most 20 per run. A finding already commented on an earlier push is not posted again.
- Findings outside the diff stay in the summary, with their evidence.

## The report

Each run uploads the report as one unzipped `px1-review.html` artifact (kept 14 days), and the summary comment links straight to it. GitHub serves artifacts from its own storage, and only people who can read the repository can open the link; whether the browser shows it or saves it is up to GitHub. The report contains your code. Raw model transcripts (`private/`) are never uploaded.

## px1 reviewing itself

`.github/workflows/px1-agentic-review.yml` runs the reviewer on this repository's
own pull requests. It does nothing until a repository secret `OPENAI_API_KEY` exists
(add it in Settings, Secrets and variables, Actions). The reviewer is built from the
base commit, not the pull request, so a pull request cannot change the code that holds
the key; the pull request is checked out only as data. Fork pull requests are skipped.

## Safety

- Runs on `pull_request`, never `pull_request_target`. Pull requests from forks are skipped: they get no secrets and a read-only token.
- The model reads the diff and any file it asks for at that commit through read-only git. Secret files and key-shaped values are blocked or redacted; this is detection, not access control.
- `max-cost` is a hard per-run cap checked before every model call, using the prices you supply.
- `continue-on-error: true` in the example keeps a failed or skipped review from blocking a merge.
- The title and description of the pull request are passed as environment variables, never interpolated into a shell script.

## Commands

    px1 post-github --findings findings.json --pr 7 [--repo owner/name] [--head SHA] [--footer TEXT] [--dry-run]

`GITHUB_TOKEN` is read from the environment only. The workflow needs `pull-requests: write`.

## Measuring real use

    GITHUB_TOKEN=$(gh auth token) px1 feedback-collect --repo owner/name --days 30 [--out feedback.json]

Counts what people did with px1's inline comments: 👍, 👎, replies, and how many
comments drew any response. Acceptance is 👍 out of all reactions, and is reported as
unknown until someone has reacted, because silence is not agreement. Only
comments carrying px1's hidden marker are counted, and replies from bots are ignored.
It is a rough signal: people react to few comments, and it complements the seeded
benchmark rather than replacing it.

## Known limits

- Very large pull requests are reviewed in part: by default at most 40 files and 120,000 diff bytes go to the model, highest-risk first (workflows, migrations, auth and security paths, then source, then tests, then docs). The summary comment opens with a "Partial review" line naming what was left out. Raise `--max-files` and `--max-diff-bytes` to review more, at more cost.
- If the provider's safety filter rejects a prompt, the call is retried once (it is non-deterministic: the same prompt has been accepted on the next try). Both attempts are metered and count against the cost cap. A second rejection fails that step and is reported in the notes.
