# CI verification report

px1 does not run project commands from the review service. A CI job can make
its results available to a local px1 checkout by writing
`.px1/verification.json` before the checkout is opened for review.

The report is intentionally small and commit-pinned:

```json
{
  "source": "github-actions",
  "revision": "0123456789abcdef0123456789abcdef01234567",
  "url": "https://github.com/acme/example/actions/runs/123",
  "checks": [
    {
      "name": "go test",
      "status": "passed",
      "summary": "go test ./...",
      "url": "https://github.com/acme/example/actions/runs/123/job/456"
    }
  ]
}
```

`source` must be `github-actions`, `revision` must be the full 40-character
commit SHA of the active review, and every link must use HTTPS. Check statuses
are `queued`, `running`, `passed`, `failed`, or `cancelled`. `startedAt` and
`finishedAt` may be included as RFC 3339 timestamps.

The report is read-only in px1. A missing report leaves the review queue
usable with an empty CI section; malformed reports or reports for another
commit are shown as unavailable rather than being treated as evidence. The
next GitHub integration slice will generate this file from the Action API and
attach the commit-pinned px1 link to the PR.
