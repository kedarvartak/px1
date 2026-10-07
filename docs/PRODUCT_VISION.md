# Product vision

Agents can write pull requests faster than people can confidently review them.
px1 gives the reviewer a compact artifact that explains what changed and where
the team should pay attention.

## The product

When a pull request opens or changes, GitHub Actions generates a static report
for that exact head commit. GitHub Pages hosts it and a sticky PR comment links
to it. The report combines:

- readable, structured diffs;
- concise explanation chips beside changed blocks;
- repository-defined team-rule findings with evidence;
- exact-revision CI results; and
- transparent attention signals for changes with wider impact.

The reviewer uses that context and then approves or requests changes in GitHub.
px1 does not make the decision for them.

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
