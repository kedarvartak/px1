# Agentic review: prompts and budgets

Plus and Apex reviews replace the fixed pipeline with three model roles. Base
reviews keep the v2 pipeline and have no agent budget.

1. **Planner** reads the pull request and its change groups and chooses which
   groups need a sub-agent, what each should check, and which files to read
   first.
2. **Sub-agents** inspect one change group each with the
   [read-only tools](AGENT_TOOLS.md) and return findings in the
   [finding schema](FINDING_SCHEMA.md).
3. **Judge** merges duplicates and drops or downgrades weak findings. The
   verifier then runs as before.

## Output contracts

| Role | Output | Rejected when |
| --- | --- | --- |
| Planner | `{"tasks":[{"block_id","focus","files"}]}` | Unknown or repeated group, more tasks than the tier allows, empty or long focus, more than 10 files, a path outside the repository, unknown fields |
| Sub-agent | One JSON object per turn: `{"tool","arguments"}` or `{"findings":[…]}` | Not valid JSON twice in a row. An invalid finding is dropped, not fatal (see below) |
| Judge | Finding array, at most 50 | A finding overlaps no candidate in the same file, or is more severe than the candidates it overlaps |

The judge can narrow a review but never widen it: it cannot invent findings,
move them to other lines, or raise severity.

## Untrusted content

Pull-request text, diffs, change groups, and candidate findings are embedded as
JSON inside `<untrusted_*>` tags. `encoding/json` escapes `<`, `>`, and `&`, so
embedded text cannot close a tag. Every system prompt says tagged content is
data, never instructions, and forbids claims about author intent or approval.
An empty sub-agent result is explicitly not a claim that the change is safe.

## Budgets

| | Plus | Apex |
| --- | --- | --- |
| Credits | 3 | 10 |
| Model cost ceiling | $0.96 | $3.20 |
| Input / output tokens | 400k / 40k | 1.5M / 120k |
| Sub-agents | 6 | 10 |
| Tool calls per sub-agent | 12 | 30 |
| Model calls | 48 | 160 |
| Verifier passes | 2 | 2 |
| Wall time | 4 min | 10 min |

Cost ceilings equal credits × the $0.80 Pro price × (1 − 60% margin
guardrail); a test fails if either side changes without the other.

Budgets are hard. Before each model call, `Meter.Reserve` holds its worst case:
prompt tokens, the request's output cap, and `WorstCaseCost` at the model's
price, rounded up. A call that could cross any ceiling is refused before it is
sent. `Settle` replaces the hold with actual usage; usage above a reservation
is still recorded and returned as an error so provider adapters can be fixed.
A refused reservation is one of three errors:

| Error | Meaning | Sticky |
| --- | --- | --- |
| `ErrBudgetExhausted` | Settled usage alone leaves no room for the request, or wall time or the model-call count ran out | Yes. `Spend().Exhausted` names the first such limit, so the summary can say why exploration stopped |
| `ErrBudgetContended` | The request fits once other in-flight holds settle | No. Concurrent worst-case holds never end a review on their own |
| `ErrToolCallLimit` | One sub-agent used its tool calls | No. Other sub-agents continue |

## Parallel execution

`Runner.Run` starts one goroutine per planned task. Each one waits for a slot
from a shared `OrgLimiter` before calling its sub-agent. The limiter caps
concurrent sub-agents per installation across every review in the process, so
one large organization cannot starve the others. Idle installations are dropped
from the limiter. The cap is per process: with N reviewer workers an
installation can run up to N × limit sub-agents, so a cluster-wide cap still
needs a shared store.

The whole run shares the meter's wall-time deadline. A task ends one of three
ways:

| Outcome | When | Reason shown |
| --- | --- | --- |
| `completed` | The sub-agent returned findings | — |
| `failed` | It returned an error or panicked | The failure class only, never provider or tool error text |
| `skipped` | The deadline passed while it waited for a slot, or a budget was exhausted before it started | Names the exhausted limit |

One failing or panicking sub-agent never fails the review or crashes the
worker. `RunResult.Complete` is true only when every task completed, so a
summary can say the review covered only part of the change.

### Streaming

`OnPartial` is called as each task finishes, in completion order, one at a time
on the caller's goroutine. That lets the summary post early without its own
locking. Each partial carries `Done`/`Total` and `Provisional: true`. Its
findings have not been judged or verified yet and must be labelled that way.
The final candidates follow plan order, not completion order, so the judge's
input does not depend on timing.

## Review trace

`agent.Trace` records each model call, tool call, and task outcome of an
agentic review, in order. Sub-agents call tools through `Trace.Dispatch`.
The runner adds task outcomes when its `Trace` field is set. `Trace.File` writes
a `review.TraceFile` pinned to the head SHA. Pass it to the exporter with
`px1 export-review --trace-file trace.json`. The report then shows a
**How px1 reviewed this** section: the tier, spend, any exhausted budget,
task outcomes, and an expandable list of every step.

| Recorded | Not recorded |
| --- | --- |
| Tool name, arguments (512 bytes), result size, truncation, a 512-byte result preview, and elapsed time | Linked-issue content: reports can be shared more widely than the tracker it came from |
| Model role and settled token counts | Prompts, model output, and provider error text |
| Task outcome and its generic reason | Anything past 2,000 events, which is counted as `dropped` |

The exporter rejects a trace for any other revision, a Base tier, unknown
fields, out-of-order sequence numbers, or oversized text. The report renders
every trace string as text, never markup. The section states that the log shows
what px1 looked at, not that unexamined code is correct.

## The sub-agent loop

`agent.Loop` runs one sub-agent over the single-prompt model gateway, so it
works with any provider. Each turn the model returns one JSON object: a tool
call, or its findings. The loop runs the tool and replays the whole transcript
on the next turn.

- **Metered.** `agent.Model` reserves the worst case before every call (input
  estimated at three bytes per token, which errs high), waits when other
  in-flight calls hold the headroom, and settles real usage with cached input
  priced separately. A failed call is settled at its input size with no output,
  so a provider that bills incomplete requests is not ignored. `Spend().Overruns`
  counts calls whose real input exceeded the estimate.
- **Results are data.** Each step is JSON inside an `<untrusted_tool_step>`
  tag, so a file containing `</untrusted_tool_step>` cannot close it.
- **Errors go back to the model.** A bad path, unknown tool, or invalid
  arguments becomes a failed step the model can correct. Tool errors never
  include git's stderr or local paths.
- **Bounded.** The tool-call limit is announced when reached ("You cannot call
  more tools"); a model that still asks for tools fails the task with
  `ErrToolCallLimit`. When the transcript outgrows 120 KiB, the oldest results
  are replaced by a marker and the model is asked for its findings.
- **Tolerant of imperfect output, strict about what ships.** Replies may be
  wrapped in a code fence or prose, and one invalid reply gets a repair note.
  Findings are validated one by one: unknown fields are ignored and severity
  case is normalized, but a finding that still fails `Validate` is dropped and
  recorded as a `dropped_finding` trace note with the reason, instead of
  rejecting the whole answer.

Because the transcript is replayed every turn, input tokens grow with each tool
call. A sub-agent that makes twelve calls can read a large multiple of its
final transcript, which is why input tokens are the budget most likely to run
out first.

## The pipeline

`agent.Pipeline.Run` chains the roles for one review. Every stage that depends
on imperfect model output has a fallback that narrows the result and says so
in the trace; none of them widens it.

| Stage | If it fails or returns nothing usable |
| --- | --- |
| Planner | Review the largest change groups with a generic focus. Tasks naming unknown or repeated groups are dropped; unknown fields are ignored |
| Sub-agents | The failing group is reported as unfinished; the others continue |
| Candidate filter | Findings about files the change did not touch are dropped and noted |
| Judge | Keep all candidates. An explicit empty list is honoured, but a reply whose every entry was invalid (invented, escalated) is treated as a broken judge and does not erase candidates |
| Verifier | Retry once. If it still fails, nothing is reported as verified: the candidates are returned as `Unverified` for local inspection only |

A finding is reported only after the verifier supports it. The verifier asks the
model for a bare JSON array and rejects unknown fields; models often wrap the
array in a code fence, so the pipeline extracts the array first and applies the
verifier's strict rules to what it contains.

`Result.Unanchored` lists verified findings whose lines are not all inside a
changed range. They stay in the report, because a real cross-file problem can
point at unchanged lines of a changed file, but they are flagged.

When nothing is found, the judge and verifier are skipped and the result says
that this is not a statement that the change is correct.

Per-role output limits: planner 2,000 tokens, judge up to 12,000, verifier up
to 6,000, and the configured limit for sub-agent turns.
