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
| Sub-agent | Finding array | Any finding fails `review.Finding.Validate` |
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
When a budget runs out, `Spend().Exhausted` names the first limit reached so
the summary can say why exploration stopped.
