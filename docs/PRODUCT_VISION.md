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

## The product

When a pull request opens or changes, GitHub Actions generates a static report
for that exact head commit. GitHub Pages hosts it and a sticky PR comment links
to it. The report combines:

- readable, structured diffs;
- explicit file-review states independent of automated findings;
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

## Public launch priorities

After explicit review progress, prioritize behavioral explanations linked to
code and tests, visible analysis limitations, safe private-code delivery, and a
demo plus pilot measuring comprehension and review decisions. These are planned
work, not claims about current coverage. Deeper structural rules follow once a
concrete reviewer need justifies them. See the roadmap for status.

## Team policy belongs in the repository

Teams store rules in `.px1/rules.json`. Rules are versioned and reviewed with
the code, so a finding can always show which policy produced it. The current
implementation uses patterns and file globs. The longer-term goal is narrowly
scoped semantic rules written in ordinary language, with evidence and explicit
uncertainty.

## AI is optional context

Model-generated explanations should clarify mechanics, dependencies, and
review impact. They must not invent author intent or label code safe. Provider
keys stay in GitHub Actions secrets; only validated, commit-pinned output enters
the static report.

Without a model, px1 still provides diffs, rules, CI evidence, and deterministic
attention signals.

## Product boundary

px1 is not:

- a local IDE or code browser;
- an agent editor or task runner;
- a mutable review-session service;
- a self-hosted web application;
- a replacement for GitHub comments or approvals; or
- a px1-operated SaaS.

The product is the artifact attached to the PR.
