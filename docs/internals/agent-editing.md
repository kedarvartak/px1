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

The review UI exposes only the read-only job snapshot needed while an explicit
provider action is running:

| Endpoint | Method | Purpose |
| --- | --- | --- |
| `/api/agent/job` | GET | Read a running or completed provider job. |

Provider selection is configuration-only (`-agent` or the persisted settings
file), and provider jobs can only be started by the review actions that create
them. The former harness discovery, selection, direct-edit, and cancellation
routes are intentionally gone; they made an editor-like agent control plane
part of px1's review artifact API.

px1 does not scan installed providers at startup and `/api/meta` does not expose
provider names, models, or pinning state. A provider is resolved only when the
configured `-agent` value or persisted setting is loaded, keeping provider
configuration out of the workspace metadata and the normal navigation path.

## Security

Harness execution is arbitrary local code execution as the user running px1.
Review actions remain local-only and use the same matching-origin and
localhost/IP host guard as other command-executing endpoints. This is a
browser-origin guard, not authentication; remote instances still belong on a
trusted network.
