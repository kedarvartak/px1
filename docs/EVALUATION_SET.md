# v1 review evaluation set

The hosted reviewer uses a versioned JSON manifest rather than copying source
code into this repository. Every case pins a public GitHub repository, pull
request, base commit, reviewed commit, and one or more labelled bug ranges.

Historical cases must also pin the later bug-fix commit that establishes the
label. Seeded cases identify the deterministic mutation recipe so the injected
bug can be reproduced and removed. `ValidateV1Coverage` rejects a launch set
with fewer than 50 historical cases, 20 seeded cases, or 10 repositories.

Labels record a bug class and exact line range. Review output is scored against
those labels; clean/noise cases will be added as a separate gate rather than
silently treating unlabelled historical changes as correct.

Do not add private source, access tokens, model output, or mutable branch names
to a dataset manifest. Commit identities and human grading decisions should be
reviewed through normal pull requests.

## Agentic tier launch gate

`eval.GateTierLaunch` runs Base and a candidate agentic tier on the same full
v2 seeded-bug benchmark (at least 100 seeded cases, 40 of them cross-file).
The candidate may launch only if it passes all three checks:

| Check | Threshold |
| --- | --- |
| Recall lift | At least 20% relative to Base (Plus recall ≥ 1.2 × Base recall) |
| Cost | At most 3× Base on the same cases |
| Precision | No more than 3 points below Base |

The precision check is not in the original launch criterion. Without it, a tier
could pass on recall by adding findings until some match. The gate fails
closed: it never passes on a partial benchmark, different case sets, zero Base
recall, or zero Base cost. Each failed check adds a human-readable reason to
the result.

## Local repeat evaluation

The launch dataset above needs public repositories and many cases. To learn
how the reviewer behaves on code you can reach today, including private
repositories, use a repeat dataset (`eval.RepeatDataset`, version 1). It records
only commit identities and labelled line ranges, never source or model output.

    {"version": 1, "cases": [
      {"id": "refund-off-by-one", "kind": "seeded", "repository": "owner/repo",
       "base_sha": "<40 hex>", "head_sha": "<40 hex>",
       "labels": [{"path": "src/refund.ts", "bug_class": "logic", "start_line": 40, "end_line": 42}]},
      {"id": "rename-only", "kind": "clean", "base_sha": "<40 hex>", "head_sha": "<40 hex>"}
    ]}

- `seeded` and `historical` cases need at least one label. A `clean` case has
  none, so any finding on it is counted as noise.
- Every case is meant to be run several times. Models are not deterministic: on
  one real pull request the same seeded bug was missed once and found twice.
  `ScoreRepeat` reports the share of bugs found per run, the best and worst whole-set
  run, labels found in some runs but not all ("unstable"), and labels never found.
- A finding matches a label when the path is equal and the line ranges overlap
  within a small slack (default 3 lines). The bug class is compared only when
  `RequireClass` is set, because the reviewer's own classes differ from human labels.
- A run that failed (outage, budget) is recorded and left out of every score, so
  an outage never looks like a miss.
- A score from a few runs is a sketch: the report states how many runs it covers.


### Running a repeat evaluation

    OPENAI_API_KEY=... px1 eval-run --cases eval/cases.json --root ~/code/repo \
      --model gpt-6-luna --price-input 0.10 --price-output 0.50 \
      --runs 3 --max-total-cost 5 --out ~/px1-eval --dry-run   # then --yes

- Every review goes through the same path as `px1 agentic-review`: consent
  (`--yes`), the privacy policy and secret blocking, and a per-review cost cap
  (`--max-cost`, default $0.50). `--max-total-cost` (default $5) caps the whole
  evaluation; it stops before a review that could cross it and says how to resume.
- Reviews run in rounds: every case once before any case twice, so a stop leaves
  whole runs to compare.
- `results.json` is written after every review. `--resume` skips reviews already
  in it. `report.md` holds the scores.
- Each case's commits must exist in `--root`. Check them out or fetch the branch first.
- A review that failed or did not finish (a sub-agent or the verifier failed, the
  cap stopped it) is recorded with its reason and left out of scoring. It is never
  counted as a missed bug.
