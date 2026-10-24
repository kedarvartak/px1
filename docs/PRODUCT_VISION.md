# Product vision

Understand the code your agent wrote before you merge it.

px1 began with a specific failure: delegating coding to a CLI agent, then
approving its PR based on another AI's review without reading the code. False
positives require enough context to challenge a finding. False negatives
require inspecting changes the automated reviewer never mentioned.

px1 helps developers regain enough understanding to take responsibility for
what they merge. The primary audience is developers who delegate implementation
to coding agents and want a practical human review habit.

## Differentiation and the moat we aim to build

AI plus human review, readable diffs, and summaries are established competitor
capabilities. We do not claim those features are exclusive to px1. Our focus is
a comprehension workflow combining explicit human review progress, inspectable
evidence, portable artifacts, optional AI, and compatibility with existing
coding agents and reviewers.

The instrument-panel analogy guides the design: show what changed, what evidence
is available, and what the human still needs to inspect. Never turn an empty
findings list into a claim that review is complete.

A durable advantage must be earned through measured improvements in human
understanding and decisions, repeated use, and insight into where agent-written
changes confuse reviewers. Feature counts and acknowledgement clicks alone do
not establish that advantage.

## The product today

When a pull request opens or changes, GitHub Actions generates a static report
for that exact head commit. A sticky PR comment links to either an authenticated
Actions artifact or a deliberately published GitHub Pages report. The report
combines:

- readable, structured diffs;
- explicit file-review states independent of automated findings;
- evidence-first inspection questions linked to code and tests;
- concise explanation chips beside changed blocks;
- repository-defined team-rule findings with evidence;
- exact-revision CI results; and
- transparent attention signals for changes with wider impact.

The reviewer uses that context and then approves or requests changes in GitHub.
px1 does not make the decision for them.

Every changed file starts unreviewed, including files without findings. Reviewers
explicitly mark files reviewed or needing follow-up, with next-unreviewed
navigation. Progress is browser-local and scoped to repository, base, and head;
it records a declaration, not verified comprehension. Finding acknowledgements
never complete the human review. The report remains a snapshot; approval
requires checking the current PR in GitHub.

## Hosted direction

The next product checkpoint adds a GitHub App that indexes the repository and
posts evidence-backed P0-P2 findings on pull requests. The static report remains
the place where a person can inspect every changed file, including code with no
finding, and see the context and verifier result behind each AI claim.

The hosted service is additive. The free GitHub Action remains available for
open-source and self-run use. Hosted indexing and review never execute pull
request code, every stored row is tenant-scoped, and approval remains in GitHub.

## Public launch priorities

Explicit review progress, behavioral inspection questions, source-by-source
evidence, and access-appropriate delivery are implemented. Next prioritize a
demo plus pilot measuring comprehension and review decisions. Deeper structural
rules follow once a concrete reviewer need justifies them. See the roadmap for
status.

## Team policy belongs in the repository

Teams store rules in `.px1/rules.json`. Rules are versioned and reviewed with
the code, so a finding can always show which policy produced it. The current
implementation uses patterns and file globs. The longer-term goal is narrowly
scoped semantic rules written in ordinary language, with evidence and explicit
uncertainty.

## AI findings remain auditable

The Action's model-generated explanations remain optional context. The hosted
reviewer may produce findings, but every finding must show line evidence,
retrieved context, confidence inputs, and its verifier disposition. It must not
invent author intent or label code safe.

Without a model, px1 still provides diffs, rules, CI evidence, and deterministic
attention signals.

## Product boundary

Hosted v1 is not:

- a local IDE or code browser;
- an agent editor or task runner;
- an agent that edits the repository;
- a test runner for untrusted pull-request code;
- an automatic approval system; or
- a replacement for GitHub comments or approvals.

The product is an auditable review workflow: findings return to GitHub, while
the commit-pinned report makes the evidence and the human's inspection progress
visible. The Action is the self-run mode; the GitHub App is the hosted mode.
