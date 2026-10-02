<div align="center">
  <img src="assets/px1-banner.png" alt="Collaborative robots around px1" width="100%">
</div>

# Understand the PR before you approve it

Agents write the PR. px1 helps reviewers understand it.

px1 is building a review report attached to a GitHub pull request. Open one
link to see the changes, short explanations beside the code, and places where
the change breaks your team's rules.

## How the review works

1. **An agent opens or updates a PR.** Your team's rules live in the repository.
2. **px1 generates a report.** A GitHub Action attaches a link to the PR.
3. **The reviewer opens the link.** Diffs show what changed. Highlighted code
   blocks carry small AI explanation chips describing what the change does
   and why it matters.
4. **Rule violations stand out.** Each finding points to the relevant code and
   explains which team rule it breaks.
5. **The reviewer decides.** Use the report to ask for changes or approve the
   PR in GitHub. AI explanations help with understanding; they can be wrong.

A report belongs to a specific commit, so reviewers know which version they
are reading. Here, “artifact” simply means the generated HTML review report.

## Your repository, your rules

Keep team rules in `.px1/rules.json` at the repository root. For example:

- “Use a ternary instead of an if/else for simple value assignments.”
- “Use our API client instead of calling fetch directly.”
- “Never log access tokens.”

The goal is to explain a team's expectations in plain language and highlight
code that does not meet them. Today's report implementation uses regex patterns
and file globs with readable messages. Understanding arbitrary natural-language
rules is still planned; a regex match alone does not prove a semantic violation.

## What is ready, and what is next?

The report work on the [integration branch](https://github.com/kedarvartak/px1/tree/codex/remove-telemetry-metrics)
includes HTML export, diffs, rule findings with surrounding code, CI result
import, and a GitHub Pages workflow that posts a PR link. Deployment still needs
end-to-end testing. The default branch and published CLI do not yet include
all of that work.

AI explanation chips, richer block highlighting, broader rule understanding,
and keeping earlier reports available are next. See the [product vision](docs/PRODUCT_VISION.md)
for the intended experience and current limits.

The first delivery path uses GitHub Actions and GitHub Pages. It does not
require running a px1 SaaS. AI-generated explanations will need a configured
provider; basic diffs and pattern-based rule findings do not. Reports contain
source code, so choose publication access appropriate for your repository.

## Try the existing CLI

The existing CLI opens a repository in your browser. This is the current local
review tool; the PR report experience above is the product being built.

```bash
npx px1-cli
# Or install the command:
npm install -g px1-cli
px1 /path/to/repo
```

To build from source, use Go 1.25+ and Node for the web assets:

```bash
git clone https://github.com/kedarvartak/px1.git
cd px1
make build
```

## Development

```bash
make test
go run . -dev . .
```

See [contribution guidance](CONTRIBUTING.md), [engineering documentation](docs/internals/README.md),
and [release instructions](PUBLISHING.md).

## License

[MIT](LICENSE) © 2026 Arpit Bhayani. px1 is a fork of [px0](https://github.com/px0-ai/px0); upstream notices are retained.
