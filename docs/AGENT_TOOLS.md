# Agentic review tools

The agentic engine (Plus and Apex tiers) lets sub-agents explore a repository
through a fixed set of tools in `internal/hosted/agent`. Every tool is
read-only. `run_tests` will join this set only with the sandboxed test agent;
it is deliberately absent until that sandbox exists.

| Tool | Reads | Bound |
| --- | --- | --- |
| `read_file` | One line range at the reviewed head | 400 lines |
| `list_files` | One directory at the reviewed head | 200 entries |
| `search_code` | Embedding and exact-text index; local git backing is exact, case-sensitive text | 20 hits, 3 per file |
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

## Local git backing

`agent.GitRepo` backs the tools with read-only `git` queries against a local
clone, pinned to the head SHA, so a review can run without the hosted index.
It supports `read_file`, `list_files`, `search_code` (`git grep`, literal text
only), `git_blame`, and `git_log`. It does not provide the symbol graph,
embeddings, or linked issues, so `graph_*` and `read_issue` stay hidden.

Git can run commands that a repository configures: `textconv` filters and
`core.fsmonitor` both execute during `git blame`. Cloned repositories are
untrusted data, so every call passes `-c core.fsmonitor=false`,
`-c core.hooksPath=/dev/null`, and `--no-textconv`. A test configures a hostile
repository and fails if any tool call runs its script.

Other behavior to know:

- Files are read from the commit, never the working tree. Symbolic links,
  directories, binary files, and files over 2 MiB are refused with a reason.
- A search with no matches returns an empty list. A failing search (for
  example an unknown revision) is an error, not an empty list.
- Blame and log report author names but not email addresses.
- Errors name the failing git command only. Git's stderr can contain local
  paths and is never shown to a model.
- Each call has a 20-second timeout and a 512 KiB output cap.
