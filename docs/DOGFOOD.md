# Hosted v1 dogfood program

The v1 launch gate is a two-week dogfood run across the px1 repository and at
least three friendly repositories. This document defines the evidence to
collect; the gate must not be marked complete until the elapsed run exists.

For every reviewed pull request, record the repository, URL, review timestamp,
time from request to completed draft, number of px1 comments, number addressed
by the author, helpful and unhelpful votes, whether a human completed the code
review, and model cost. Do not treat a merge, an acknowledgement, or the absence
of findings as proof that the review was correct.

Use comparable development pull requests rather than synthetic empty changes.
Repository owners should inspect px1 comments and the full diff, then classify
missed material issues separately. Remove credentials and personal data from
the evidence export.

The run is eligible for launch review only when it spans at least 14 calendar
days, contains at least four distinct repositories, and every recorded pull
request received a completed human review. Report median latency, comment
address rate, helpful-vote rate, total model cost, false-positive examples, and
known misses. Launch remains a human decision; meeting the collection floor is
necessary but not sufficient.
