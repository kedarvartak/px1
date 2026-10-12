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
generates the static report for exact base and head commits; a PR comment links
to either an authenticated Actions download or a deliberately published GitHub
Pages report.

px1 does not host a SaaS, start a local application, edit code, or replace
GitHub review decisions. It creates context; approval and change requests stay
in GitHub.

## What reviewers get

- Explicit file states: unreviewed, reviewed, or needs follow-up
- Next-unreviewed navigation that includes code with no automated findings
- Structured, line-numbered diffs grouped into reviewable blocks
- Evidence-first inspection questions linked to changed code and companion tests
- Fast file-and-code search for large pull requests
- Repository-specific findings from `.px1/rules.json`
- Exact-commit GitHub check results
- Deterministic attention signals for dependencies, authentication, schemas,
  configuration, generated code, deleted tests, permissions, large diffs, and
  public APIs
- Optional AI explanation chips generated in CI
- A report artifact pinned to the PR head SHA
- Machine-readable `review.json` for agents and repository automation
- Access-controlled downloads for private code, or historical Pages links when
  public publication is deliberately enabled

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

The **Evidence & limits** panel keeps four signals distinct: browser-local human
progress, deterministic analysis, optional AI explanations, and imported CI
verification. It shows the exact comparison or revision each source represents,
whether an input is absent, and any scan or output limits. Zero findings means
that no configured analysis matched; it is never a safety claim. The generated
page is a static snapshot and cannot know the live PR head or live check status.

Inspection questions keep observations separate from judgment. px1 can point
out that an exported interface, authentication path, schema, configuration,
dependency, executable mode, or test relationship changed, then ask what the
reviewer should verify. Changed companion tests link back into the diff. These
prompts are deterministic and bounded. They do not infer author intent, prove
test coverage, or count as findings.

## How it works

```text
PR opened or updated
        ↓
GitHub Action reads the base and head commits
        ↓
px1 generates index.html, review.json, and checksums.sha256
        ↓
GitHub uploads an authenticated artifact or deliberately publishes Pages
        ↓
A sticky PR comment links the reviewer to it
```

The workflow checks out trusted base-branch code and fetches the PR head only
as Git objects. px1 inspects the diff; it does not execute code from the PR.

## Set up px1

For private repositories, start with access-controlled artifact delivery:

1. Copy [`examples/px1-review-private.yml`](examples/px1-review-private.yml) to
   `.github/workflows/px1-review.yml` in the repository you want reviewed.
2. Pin the px1 Action reference in that file to a released major version.
3. Open or update a non-draft pull request.

The PR comment links to an expiring GitHub Actions artifact. Download access
requires a signed-in user with repository read access. Unzip it and open
`index.html`.

For deliberate GitHub Pages publication, enable Pages with **GitHub Actions**
as the source and use [`examples/px1-review.yml`](examples/px1-review.yml).
Confirm the Pages site's effective visibility first: a private repository does
not by itself guarantee a private site.

The Pages workflow publishes:

```text
https://<owner>.github.io/<repository>/reviews/<head-sha>/
```

and creates or updates one marked px1 comment on the PR.
The comment summarizes finding counts, the highest-priority team rules, exact
verification state, and the commit-pinned artifact link.

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

px1's own policy file focuses on concrete code-writing habits, including
conditional expressions for simple JavaScript value choices, strict equality,
block-scoped declarations, explicit error handling, awaited iteration, and
modern Go APIs. These are review conventions, so each rule is scoped by language
and phrased with an exception where the shorter pattern would be misleading.

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
Enabling explanations transfers bounded, non-excluded diff content to the
configured model endpoint and emits a workflow warning. Exclusion globs are not
redaction.

See [the explanation contract](docs/STATIC_REVIEW_EXPLANATIONS.md) and
[verification contract](docs/VERIFICATION_REPORT.md).

## Security model

- Every report identifies full base and head commit SHAs.
- The supplied workflow never checks out or executes the PR head.
- Verification and explanation inputs must match the report head SHA.
- Historical storage accepts only regular
  `reviews/<40-character-sha>/index.html`, `review.json`, and
  `checksums.sha256` files.
- The reviewer UI is one static HTML file and makes no network requests.
- The default privacy check blocks high-confidence secret signatures and
  sensitive paths before delivery. It is not redaction or access control.
- Each bundle includes SHA-256 checksums. Hashes detect changed bytes; they do
  not prove authorship or approval.
- GitHub Pages reports can be public even when the source repository is private.
  Use authenticated artifact delivery unless Pages access control is deliberate.

See [private-code delivery and privacy limits](docs/PRIVATE_DELIVERY.md).

Finding acknowledgements remain in browser storage, scoped to the repository
and full head SHA. px1 never uploads them or presents them as shared state.

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
