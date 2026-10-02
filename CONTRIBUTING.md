# Contributing to px1

px1 helps reviewers understand agent-written PRs through an attached report.
The goal is clear diffs, short AI explanations beside highlighted code blocks,
and findings for team rules stored in the repository. See the
[product vision](docs/PRODUCT_VISION.md) for status and priorities.

## AI-Driven Development and Contributions

px1 is built and maintained entirely using AI coding agents.

Implementation is written by AI agents guided by the PR report workflow and
our engineering constraints. We generally do not accept unsolicited pull
requests containing manual or disparate code patches.

Instead, the most valuable contribution you can provide is a clear, detailed bug report or a well-reasoned feature idea.

## How to Contribute
### Report a Bug

If you encounter unexpected behavior, memory leaks, navigation bugs, or performance issues:

- Check existing [GitHub Issues](https://github.com/kedarvartak/px1/issues) first to avoid duplicates.
- Open a new issue using our [Bug Report Template](.github/ISSUE_TEMPLATE/bug_report.md).
- Provide comprehensive context: operating system, browser, workspace size, steps to reproduce, expected vs. observed behavior, logs, and screenshots if applicable.

### Suggest a Feature or Improvement

Have an idea that makes a PR easier to understand or a team-rule finding easier to verify?

- Open a new issue using our [Feature Request Template](.github/ISSUE_TEMPLATE/feature_request.md).
- Explain how it helps a reviewer understand a change, check a rule, or reach a review decision.

## Review and Implementation Process

1. Review: Maintainers evaluate how the change improves the PR report and whether it stays simple and fast.
1. Assessment: We determine whether the proposal is apt, feasible, and aligned with the project roadmap.
1. Execution: If approved, we formulate prompt specifications and task plans, and our AI agents implement, test, benchmark, and release the changes.

Thank you for helping make px1 better.
