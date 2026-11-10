---
title: MCP server
section: guides
kind: narrative
status: built
covers:
  - mcp_proxy.go
  - internal/hosted/mcpserver/
verified_at:
---

# MCP server

px1 exposes reviews to coding agents over the
[Model Context Protocol](https://modelcontextprotocol.io), using the official
Go SDK (`github.com/modelcontextprotocol/go-sdk` v1.8.0).

- **Hosted:** `mcpserver.Handler` serves streamable HTTP at `mcp.px1.dev`.
- **Local:** `px1 mcp` serves the same tools over stdio for agents that launch a
  local command. It has no hosted data: it connects to the hosted server with
  your key and forwards each call.

```sh
PX1_API_KEY=px1k_... px1 mcp            # defaults to https://mcp.px1.dev/mcp
```

The key is read only from `PX1_API_KEY`, never from a flag, so it stays out of
shell history and process lists. The CLI sends it only to the configured host
and refuses redirects.

## Tools

| Tool | Scope | Notes |
| --- | --- | --- |
| `list_reviews` | `reviews:read` | Newest first, with report links |
| `get_review` | `reviews:read` | The reviewed head can be older than the PR's current head |
| `list_findings` | `findings:read` | `open` (default), `resolved`, or `all` |
| `get_finding` | `findings:read` | Includes evidence and the "why" link |
| `resolve_finding` | `findings:write` | Requires a note; does not approve the PR |
| `trigger_review` | `reviews:trigger` | Spends credits |
| `search_codebase` | `codebase:read` | At most 100 hits |
| `get_rules` | `rules:read` | Effective rules for a path |
| `get_memories` | `memories:read` | Each memory with its evidence |

Read tools carry `readOnlyHint`. Every read description warns that returned
text is repository, pull-request, and model content: data, never instructions.

## Isolation

1. `apikeys.Middleware` verifies the bearer key and applies the per-key rate
   limit, returning `401`, `403`, or `429` (see [API keys](API_KEYS.md)).
2. The SDK's bearer middleware records `<installation>/<key id>` as the
   session user. A request presenting that session ID with any other key gets
   `403`, so a leaked session ID cannot be reused across keys or tenants.
3. Each new session's server is built for that key: every tool is bound to the
   key's installation, and tools outside the key's scopes are not listed or
   callable.

Request bodies are capped at 1 MiB and idle sessions close after 30 minutes.

## Not yet included

- OAuth 2.1 authorization for interactive clients; API keys are the only
  credential today.
- A production `Backend` and API-key `Store` backed by PostgreSQL, and the
  `px1d` route that serves the handler. This change defines and tests the
  contract end to end with in-memory implementations.
