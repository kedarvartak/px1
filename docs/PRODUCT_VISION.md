# px1: a clearer way to review a PR

Agents can produce code faster than people can review it. px1 helps the reviewer
understand a PR and check it against the team's expectations. The main product
is an HTML review report linked from the GitHub PR.

## The experience we are building

An agent opens a PR. A GitHub Action reads the changes and repository rules,
generates a report, and adds a link to the PR. The reviewer sees:

- **Diffs:** what was added, removed, or changed.
- **Highlighted blocks:** related changes grouped into readable pieces.
- **AI explanation chips:** small labels beside blocks that open concise
  explanations of what changed and why it matters. They should identify
  uncertainty rather than invent the author's intent.
- **Rule findings:** the team rule, the offending code, and a short explanation.
- **CI results:** available tests and checks for the reviewed commit.

Approval and requests for changes remain in GitHub. Generating a report does
not mean the code is correct or approved.

## Example

A team says, “Use ternaries for simple conditional assignments.” A PR adds an
if/else that assigns one of two values. The intended report highlights that
block and explains: “This assigns a value based on a condition. Your team asks
for a ternary in this case.” A separate AI chip explains what the value affects.

The rule belongs to the team. px1 should distinguish that convention from a
bug, and avoid flagging unrelated uses of `if` under this example rule.

## Rules live with the code

The file is `.px1/rules.json` at the repository root. Keeping it in Git makes
rule changes reviewable alongside code changes. The goal is to support rules
expressed in the team's own words.

The current report implementation requires regex patterns and optional file
globs alongside readable messages. It checks added lines. Arbitrary
plain-language rules, multi-line reasoning, and semantic checks need more work.

## Delivery and status

The first path uses GitHub Actions to generate a static report, GitHub Pages
to serve it, and a PR comment to link to it. No px1 SaaS is required. AI
explanations need a configured provider; diffs and pattern matching do not.

Report export, contextual findings, CI imports, and the publishing workflow
are implemented on the [integration branch](https://github.com/kedarvartak/px1/tree/codex/remove-telemetry-metrics).
Deployment testing and integration into the default branch remain. AI
explanation chips and richer block highlighting are planned.

Reports identify the commit being reviewed. The current publishing workflow
replaces the deployed site with the latest report. Preserving earlier links,
including reports for other PRs, remains unfinished. CI results are a snapshot
at generation time.

## Priorities

Make the report easy to open, the changes easy to understand, and the findings
easy to verify against the code. Measure progress by whether reviewers can
understand the PR and spot rule violations with less back-and-forth.

Local navigation and the CLI support development and testing. The main product
promise is a useful review artifact attached to the PR.
