# Self-hosted review snapshot import

The live px1 server accepts a commit-pinned review snapshot from trusted CI:

```text
POST /api/review/import
Authorization: Bearer <PX1_IMPORT_TOKEN>
Content-Type: application/json
```

Configure a high-entropy token on the server and store the same value as a
masked GitHub Actions secret:

```bash
PX1_IMPORT_TOKEN='replace-with-a-random-secret' px1 -host 0.0.0.0 -port 7777 /workspace
```

CI can generate the nested `snapshot` object without parsing HTML:

```bash
px1 export-review \
  --base "$BASE_SHA" \
  --head "$HEAD_SHA" \
  --out ./site \
  --snapshot-out ./snapshot.json
```

Wrap `snapshot.json` with the GitHub owner, repository, and pull-request fields
shown below, then POST that envelope to the import endpoint.

The request body is limited to 16 MiB and uses this versioned shape:

```json
{
  "version": 1,
  "provider": "github",
  "owner": "acme",
  "repository": "widgets",
  "pullRequest": 42,
  "base": "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
  "head": "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
  "snapshot": {
    "version": 1,
    "repository": "widgets",
    "base": "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
    "head": "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
    "generatedAt": "2026-10-03T12:00:00Z",
    "files": [],
    "ruleHits": [],
    "explanations": [],
    "verification": {
      "available": false,
      "checks": []
    }
  }
}
```

The top-level repository and commit identity must exactly match the embedded
snapshot. Owners, repository names, PR numbers, changed-file paths, findings,
explanations, and verification evidence are validated before anything is
stored. Unknown JSON fields are rejected.

An initial import returns HTTP `201`; retrying identical content returns HTTP
`200`. Different content for the same repository, PR, and head SHA returns
HTTP `409`, preserving the immutable-link contract. Snapshots are stored with
user-only permissions under the px1 state directory, not in the checkout.

The response contains the review path:

```text
/github/acme/widgets/pull/42?sha=bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb
```

Use an HTTPS reverse proxy and keep the service behind a VPN or private
network. The import endpoint is bearer-authenticated, but read-link
authentication is a separate roadmap item; possessing the review URL is
currently enough to read the imported report.
