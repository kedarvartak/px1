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
PX1_IMPORT_TOKEN='replace-with-a-random-secret' \
PX1_REVIEW_LINK_SECRET='replace-with-a-different-random-secret' \
px1 -host 0.0.0.0 -port 7777 /workspace
```

`PX1_IMPORT_TOKEN` authenticates CI uploads. `PX1_REVIEW_LINK_SECRET` signs
review URLs. Keep both values server-side and rotate the link secret to revoke
previously issued read links. If the link secret is not configured, px1 keeps
the legacy private-network behavior: imported reports are readable by anyone
who can reach the server and knows the commit-pinned URL.

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
/github/acme/widgets/pull/42?sha=bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb&sig=<hmac>
```

When `PX1_REVIEW_LINK_SECRET` is configured, the `sig` value is an HMAC-SHA256
signature over the provider, owner, repository, pull-request number, and head
SHA. Missing or invalid signatures are rejected before a stored report is
read. GitHub Pages links remain public static artifacts and do not use this
self-hosted signature.

Use an HTTPS reverse proxy and keep the service behind a VPN or private
network. The import endpoint and self-hosted read links are independently
authenticated; possessing a valid signed URL is sufficient to read that
immutable report.
