---
title: px1 agentic-review
section: guides
kind: narrative
status: shipped
covers:
  - agentic_review.go
  - agentic_review_prep.go
  - internal/hosted/agent/run.go
  - internal/hosted/agent/gitstore.go
verified_at:
---

# `px1 agentic-review`

Reviews two commits of a **local clone** with the agentic pipeline
([prompts, budgets, and stages](AGENTIC_REVIEW.md)) and writes a commit-pinned
report you can open in a browser. It needs no database, GitHub App, or hosted
service. It does send repository content to a model provider, so it previews
what would be sent and refuses to proceed without `--yes`.

```sh
# 1. See what would be sent and what it could cost. Needs no API key; sends nothing.
px1 agentic-review --root ~/code/app --base main~2 --head main \
  --model <model> --price-input <usd per M> --price-output <usd per M> --dry-run

# 2. Run it.
OPENAI_API_KEY=... px1 agentic-review ... --yes
```

The key comes only from `OPENAI_API_KEY`, never a flag. Prices are required and
are in USD per million tokens, because the cost cap is only as real as the
prices behind it.

## What gets sent, and what never does

- **The diff** of every reviewable file, with new-file line numbers.
- **Anything a tool reads.** The model can read other files at the head commit,
  list directories, search text, and read blame and log. It does not see your
  working tree, only the committed content at the head.
- **Never sent, and blocked from every tool:** sensitive paths (`.env`, keys,
  certificates, credential files), generated or dependency files (lock files,
  `dist/`, `node_modules/`, `vendor/`, minified files), and anything matching
  `--exclude-glob`. Excluded files are not even listed to the model.
- **Redacted:** known secret shapes (private keys, OpenAI, GitHub, AWS, Slack,
  and npm tokens) found in text returned by tools. The count is reported.
- **Blocked:** if the diff text bound for the model contains a high-confidence
  secret, the run stops before any request and names the file, never the value.
  Remove the secret or exclude the file.

Secret detection is detection, not access control: it knows common shapes and
nothing else. The report is a local file for you. It shows the whole change,
including excluded files, so it warns when it contains likely sensitive content.
Do not publish it without checking.

## Limits

The tier budget ([Plus](AGENTIC_REVIEW.md#budgets) by default) applies, and
`--max-cost` can only lower its cost ceiling. The cap is checked before every
call from your prices and the call's worst case, so a run stops before it
crosses the cap rather than after. Real spend can exceed the checked figure
slightly if a model uses more input tokens than estimated; the summary says when
that happened. Other limits: `--max-minutes`, `--max-output-tokens`, and
`--concurrency`.

Because each sub-agent replays its whole conversation every turn, input tokens
grow with every tool call. A large diff can exhaust the input budget before the
cost cap. Start with a small range.

Reasoning models spend output tokens on reasoning before they answer. If calls
return empty text, raise `--max-output-tokens`.

## Output

```text
<out>/
  report/index.html     the review report, with "How px1 reviewed this"
  findings.json         verified findings, pinned to the head commit
  trace.json            every model call, tool call, task outcome, and dropped finding
  private/              owner-only; contains repository code
    feedback.md         a sheet for judging each finding
    notes.json          why stages fell back or dropped things
    unverified.json     candidates that could not be verified (not reported)
    transcripts.jsonl   raw prompts and replies (turn off with --transcripts=false)
```

A finding appears only if the verifier supported it. An empty result means no
proposed finding survived verification; it is not evidence the change is
correct.

Provider error messages (for example an unknown model) are shown on your
terminal and kept in `private/transcripts.jsonl`. They are never written to the
report or the trace.

## Judging the result

Fill in `private/feedback.md` for each finding (real, false alarm, or unclear),
whether you already knew, whether you would have caught it in a normal review,
and what it missed. That is the data the roadmap's pilot needs; time to read the
report and whether the trace helped you decide what to trust are asked too.

## Large changes

`--max-files` (default 40) and `--max-diff-bytes` (default 120000) bound what is sent as the diff. A larger change is reviewed in part, highest-risk files first, and the dry run lists what was left out. Left-out files are not blocked: the model may still read them with its tools. `scope.json` in the output directory records reviewed and skipped files; `post-github --scope` turns it into a "Partial review" note on the pull request.
