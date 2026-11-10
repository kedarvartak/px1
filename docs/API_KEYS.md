---
title: API keys
section: reference
kind: reference
status: built
covers:
  - internal/hosted/apikeys/
verified_at:
---

# API keys

Organization API keys authenticate the public API and the MCP server.

```text
px1k_<installation id>_<key id>_<secret>
```

- **The prefix** `px1k_` makes leaked keys easy to match in secret scanners.
- **The installation and key IDs are not secret.** The server sets
  `px1.installation_id` from the token before reading anything, so the
  `api_keys` table stays under the same forced row-level security as every
  other tenant table. Leaking an installation ID grants nothing.
- **The secret** is 256 random bits. Only its SHA-256 digest is stored and
  compared in constant time. A slow password hash would add nothing for a
  random secret of that length.

## Scopes

| Scope | Allows |
| --- | --- |
| `reviews:read` | Listing and reading reviews |
| `reviews:trigger` | Requesting a review |
| `findings:read` | Reading findings and their context |
| `findings:write` | Resolving findings with a note |
| `codebase:read` | Searching the indexed codebase |
| `rules:read` | Reading effective rules for a path |
| `memories:read` | Reading review memories for a path |

Every key has at least one scope. Unknown scopes are rejected.

## Lifecycle

- **Issue:** the plaintext token is returned once. Lifetime is at most one year.
- **Rotate:** the new key keeps the name, scopes, and lifetime. The old key keeps
  working for an overlap of up to seven days, then expires. A key can be rotated
  only once.
- **Revoke:** takes effect on the next request.

Malformed, unknown, wrong, expired, revoked, and other-tenant keys all return
the same `401 invalid API key`. A valid key without the required scope gets
`403`.

## Rate limits

`RateLimiter` is a token bucket per key, with a configurable rate per minute and
burst. A refused request gets `429` with `Retry-After`. The limiter is
per-process, so with N API processes a key can make up to N × the rate. A
cluster-wide limit needs a shared store.
