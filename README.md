<div align="center">
  <img src="assets/px1-eclipse-orbital-banner.png" alt="px1" width="100%">
  <h1>Understand the code your agent wrote before you merge it</h1>
</div>

px1 helps developers who delegate coding to agents inspect and understand the
code they are about to merge.

The problem that started px1 was approving PRs based on AI reviews without
reading the code. When an agent writes the implementation and another AI
reviews it, the developer can miss both an incorrect finding and a real bug
that nobody flagged.

Push a PR. px1 gives you readable diffs, explanations beside the code, and
explicit progress through every changed file. Automated findings and CI results
provide evidence while you build your own understanding. GitHub Actions
generates the static report for exact base and head commits; GitHub Pages
publishes it and a PR comment links to it.

px1 does not host a SaaS, start a local application, edit code, or replace
GitHub review decisions. It creates context; approval and change requests stay
in GitHub.

## What reviewers get

- Explicit file states: unreviewed, reviewed, or needs follow-up
- Next-unreviewed navigation that includes code with no automated findings
- Structured, line-numbered diffs grouped into reviewable blocks
- Cross-file change groups that connect related routes, services, tests, and schemas
- Fast file-and-code search for large pull requests
- Repository-specific findings from `.px1/rules.json`
- Exact-commit GitHub check results
- Deterministic attention signals for dependencies, authentication, schemas,
  configuration, generated code, deleted tests, permissions, large diffs, and
  public APIs
- Optional AI explanation chips generated in CI
- A report URL pinned to the PR head SHA
- Machine-readable `review.json` for agents and repository automation
- Historical report links that remain valid after the PR changes

## Review the code, including changes nobody flagged

Open the report and follow **Next unreviewed file**. Inspect the diff and mark
each file **Reviewed** or **Needs follow-up**; reopen it as **Unreviewed** at any
time. The headline tracks these explicit marks separately from finding
acknowledgements. Acknowledging a finding never marks its file reviewed.

Progress stays in your browser for the repository and exact base/head comparison.
A new comparison starts fresh. Scrolling does not mark anything reviewed, and
your marks do not prove correctness or submit a GitHub approval. If browser
storage is unavailable, the report says progress lasts only on the page.

px1 works alongside existing coding agents and AI reviewers. Its focus is the
human's understanding and decisions; AI explanations are optional. Confirm the
current PR revision in GitHub before submitting your review.

## How it works

```text
PR opened or updated
        ↓
GitHub Action reads the base and head commits
        ↓
px1 generates reviews/<head-sha>/index.html and review.json
        ↓
GitHub Pages publishes the report
        ↓
A sticky PR comment links the reviewer to it
```

The workflow checks out trusted base-branch code and fetches the PR head only
as Git objects. px1 inspects the diff; it does not execute code from the PR.

## Set up px1

1. Enable GitHub Pages with **GitHub Actions** as the source.
2. Copy [`examples/px1-review.yml`](examples/px1-review.yml) to
   `.github/workflows/px1-review.yml` in the repository you want reviewed.
3. Pin both px1 Action references in that file to a released major version.
4. Open or update a non-draft pull request.

The workflow publishes:

```text
https://<owner>.github.io/<repository>/reviews/<head-sha>/
```

and creates or updates one marked px1 comment on the PR.

## Team rules

Rules live at `.px1/rules.json` and change through normal code review:

```json
{
  "rules": [
    {
      "pattern": "\\bfetch\\(",
      "glob": "src/**/*.ts",
      "message": "Use the shared API client instead of fetch"
    }
  ]
}
```

For rules that need clearer reviewer context, use the versioned format:

```json
{
  "version": 1,
  "rules": [
    {
      "id": "no-console",
      "title": "Use the shared logger",
      "description": "Shared logging applies the team's redaction policy.",
      "severity": "error",
      "globs": ["src/**/*.ts", "src/**/*.tsx"],
      "match": { "kind": "text", "pattern": "console.log(" },
      "message": "Replace direct console output with the shared logger."
    }
  ]
}
```

Rules support literal `text` and Go-compatible `regex` matches across one or
more file globs. `block-text` and `block-regex` inspect contiguous added lines,
so a rule can describe a small multi-line construct such as a simple `if/else`
value choice without matching unrelated lines elsewhere in the file. A finding
shows the team-owned title, reason, severity,
matching line, and nearby diff context. The compact format remains supported.
A finding is a team convention, not a claim that the code is unsafe.

Validate the policy before opening a PR or in repository CI:

```bash
go run . validate-rules --file .px1/rules.json
go run . validate-rules --file .px1/rules.json --json
```

Invalid patterns, globs, severities, duplicate IDs, and unsupported schema
versions fail with a non-zero exit status and a rule-specific message.

## Optional explanations

Set `explanation-model` on the px1 Action and provide `OPENAI_API_KEY` through
a GitHub Actions secret to generate explanation chips automatically. You can
also bring provider-neutral, commit-pinned JSON through `explanations-file`.
Model credentials and generation stay inside CI; the published page receives
only validated explanation text. Reports remain useful when no model or API
key is configured. Teams can exclude sensitive paths from model input with
`explanation-exclude-globs` while keeping those changes visible in the report.

See [the explanation contract](docs/STATIC_REVIEW_EXPLANATIONS.md) and
[verification contract](docs/VERIFICATION_REPORT.md).

## Security model

- Every report identifies full base and head commit SHAs.
- The supplied workflow never checks out or executes the PR head.
- Verification and explanation inputs must match the report head SHA.
- Historical storage accepts only regular
  `reviews/<40-character-sha>/index.html` and `review.json` files.
- The reviewer UI is one static HTML file and makes no network requests.
- GitHub Pages reports are public unless the repository's Pages configuration
  provides access control. Do not publish sensitive private code publicly.

## Development

```bash
go test ./...
go test -race ./...
go run . export-review --base <base-sha> --head <head-sha> --out ./site
```

The product boundary and next work are documented in
[PRODUCT_VISION.md](docs/PRODUCT_VISION.md) and [ROADMAP.md](ROADMAP.md).

## License

[MIT](LICENSE) © 2026 Arpit Bhayani. px1 began as a fork of
[px0](https://github.com/px0-ai/px0); upstream notices are retained.
