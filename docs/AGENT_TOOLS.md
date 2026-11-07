# Agentic review tools

The agentic engine (Plus and Apex tiers) lets sub-agents explore a repository
through a fixed set of tools in `internal/hosted/agent`. Every tool is
read-only. `run_tests` will join this set only with the sandboxed test agent;
it is deliberately absent until that sandbox exists.

| Tool | Reads | Bound |
| --- | --- | --- |
| `read_file` | One line range at the reviewed head | 400 lines |
| `search_code` | Embedding and exact-text index | 20 hits |
| `graph_callers`, `graph_callees` | Symbol graph from a path and symbol name | Two hops, 50 symbols |
| `git_blame` | Last commit per line in a range | 400 lines |
| `git_log` | Recent commits to one file | 20 commits |
| `read_issue` | A Linear or Jira issue linked from the pull request | Linked keys only |

Tool names use underscores rather than `graph.callers` because model
providers restrict tool names to letters, digits, `_`, and `-`.

## Guarantees

- **Pinned scope.** Every call carries the installation, repository, and full
  head SHA. A call without them is rejected before any store is read.
- **Strict arguments.** Arguments are decoded with unknown fields and trailing
  data rejected, and limited to 4 KiB. Out-of-range values are clamped or
  rejected, never passed through.
- **No path escapes.** Paths are normalized and must stay repository-relative.
- **Linked issues only.** `read_issue` fetches only keys detected on the pull
  request. Text injected into the diff cannot make a sub-agent read, and
  then quote, an unrelated ticket from the same tenant.
- **Bounded output.** Each result is capped at 16 KiB, cut on a UTF-8
  boundary and marked `[output truncated by px1]`.
- **Fail closed.** A model is offered only tools whose backing store is
  configured, and a call to an unconfigured or unknown tool returns an error
  that can be reported back to the model.

Tool output is repository content. Prompts must present it as untrusted data,
never as instructions.
