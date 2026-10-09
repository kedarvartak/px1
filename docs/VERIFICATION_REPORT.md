# GitHub Actions verification input

Verification is evidence for a human reviewing agent-written code. Passing CI
does not mark files reviewed, establish behavioral test coverage, or approve a
PR. Results describe the recorded revision and collection time, not live state.

The px1 workflow collects check runs for the exact pull-request head and writes
a small JSON input for the report generator:

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

`source` must be `github-actions`. `revision` must be the full report-head SHA.
Links must use HTTPS, check names must be unique, and statuses are `queued`,
`running`, `passed`, `failed`, or `cancelled`. Optional timestamps use RFC 3339.

The generator rejects stale identity and untrusted URLs. Invalid verification
data is shown as unavailable and is never treated as passing evidence. px1
displays results; it does not execute project tests or PR code.
