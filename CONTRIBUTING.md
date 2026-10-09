# Contributing to px1

px1 helps developers understand the code their agents wrote before merging.
Changes should help a reviewer inspect behavior, challenge incorrect findings,
examine unflagged changes, or understand the limits of the evidence. The static
review artifact delivers that workflow.

Before proposing work, read [the product vision](docs/PRODUCT_VISION.md). Local
editors, local review sessions, self-hosted services, and SaaS features are out
of scope.

## Report a bug

Use the bug template and include:

- the affected PR or a minimal reproduction repository;
- the report head SHA;
- the relevant GitHub Actions run;
- expected and actual report behavior; and
- screenshots for visual issues.

Do not include repository secrets or private source in a public issue.

## Propose an improvement

Explain which review question becomes easier to answer and what evidence the
report should display. Prefer focused, testable behavior over a broad new
platform surface.

## Validate a change

```bash
go test ./...
go test -race ./...
go vet ./...
```

For renderer changes, also generate a report from a fixture repository and
inspect the resulting self-contained HTML.
