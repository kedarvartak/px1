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
