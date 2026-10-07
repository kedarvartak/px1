<div align="center">
  <img src="assets/px1-eclipse-orbital-banner.png" alt="px1" width="100%">
  <h1>Understand the PR before you approve it</h1>
</div>

px1 turns every GitHub pull request into a commit-pinned review artifact.

Push a PR. GitHub Actions generates a static report, publishes it through
GitHub Pages, and adds one link to the PR. The reviewer opens that link to see
readable diffs, concise explanations, CI context, risk signals, and violations
of rules stored in the repository.

px1 does not host a SaaS, start a local application, edit code, or replace
GitHub review decisions. It creates context; approval and change requests stay
in GitHub.

## What reviewers get

- Structured, line-numbered diffs grouped into reviewable blocks
- Repository-specific findings from `.px1/rules.json`
- Exact-commit GitHub check results
- Deterministic attention signals for dependencies, authentication, schemas,
  configuration, generated code, deleted tests, permissions, large diffs, and
  public APIs
- Optional AI explanation chips generated in CI
- A report URL pinned to the PR head SHA
- Historical report links that remain valid after the PR changes

## How it works

```text
PR opened or updated
        ↓
GitHub Action reads the base and head commits
        ↓
px1 generates reviews/<head-sha>/index.html
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
more file globs. A finding shows the team-owned title, reason, severity,
matching line, and nearby diff context. The compact format remains supported.
A finding is a team convention, not a claim that the code is unsafe.

## Optional explanations

The generator accepts commit-pinned explanation JSON through
`explanations-file`. This keeps model credentials and generation inside CI;
the published page receives only validated explanation text. Reports remain
useful when no model or API key is configured.

See [the explanation contract](docs/STATIC_REVIEW_EXPLANATIONS.md) and
[verification contract](docs/VERIFICATION_REPORT.md).

## Security model

- Every report identifies full base and head commit SHAs.
- The supplied workflow never checks out or executes the PR head.
- Verification and explanation inputs must match the report head SHA.
- Historical storage accepts only regular
  `reviews/<40-character-sha>/index.html` files.
- The report is one static HTML file and makes no network requests.
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
