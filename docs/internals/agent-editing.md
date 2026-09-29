# Review Provider & Agent Dispatch

px1 can optionally invoke a locally configured coding harness for review
actions. The review UI remains a read-only surface: it does not contain an
inline edit composer, harness picker, cancellation controls, or agent-range
decorations.

## Provider model

The provider is wired by `main.go` and implemented by [`agent.go`](../../agent.go).
Use `-agent` to pin a command for a run, or configure the persisted provider
choice in the user settings file. `-no-agent` disables provider actions. A
missing or unavailable provider does not prevent browsing or reviewing a
workspace.

The provider is used only for explicit review actions:

- decision explanations and alternative suggestions from [`pins.go`](../../pins.go);
- rule-pattern suggestions from [`rules.go`](../../rules.go); and
- addressing saved review comments through [`review.go`](../../review.go).

Normal harness work remains external to px1. The browser notices those edits
through the review revision fingerprint and refreshes the review queue.

## Invocation and jobs

Each configured harness has a headless command template. px1 supplies a
review-specific prompt, runs it with the workspace as its working directory,
keeps stdin empty, and records bounded stdout/stderr tails. Jobs are exposed as
read-only snapshots through `GET /api/agent/job`; the review action that started
the job polls until it finishes.

Provider jobs retain server-side guards for overlapping anchored work and for a
busy workspace. Those checks now live in Go; the browser no longer maintains a
second edit-composer state machine.

When a provider writes files, px1 compares the workspace snapshot before and
after the job. Changed files invalidate the relevant highlight and language
server caches, rebuild the index, and become visible through the ordinary
review refresh path. This is the same safety boundary used for edits made by a
harness outside px1.

## HTTP surface

The provider endpoints are local-only mutation routes guarded by `localPost`:

| Endpoint | Method | Purpose |
| --- | --- | --- |
| `/api/agent/harnesses` | GET | Diagnostic list of known installed harnesses. |
| `/api/agent/select` | POST | Update the configured provider for explicit clients. |
| `/api/agent/edit` | POST | Legacy explicit dispatch API; not called by the review UI. |
| `/api/agent/job` | GET | Read a running or completed provider job. |
| `/api/agent/cancel` | POST | Legacy explicit cancellation API; not called by the review UI. |

The browser uses only the job snapshot endpoint for review actions. Keeping the
legacy routes temporarily allows scripts and local integrations to migrate
without making them part of the review artifact path.

## Security

Harness execution is arbitrary local code execution as the user running px1.
Mutation routes require POST, a matching origin, and a localhost/IP host. This
is a browser-origin guard, not authentication; remote instances still belong
on a trusted network.
