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
