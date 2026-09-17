# px1 P1 MVP manual test checklist

Run this checklist on Go 1.25+ before tagging the P1 MVP. Use a disposable Git worktree with a configured local coding harness and at least one verification command.

## Review flow

1. Start px1, open **Review**, and start a review session. Confirm it reports no changes.
1. Modify two files outside px1 (or through the agent). Confirm only those files appear in the queue and **Next change** opens the task-baseline diff.
1. Mark one file reviewed, modify it again, refresh the queue, and confirm its state becomes **stale** rather than remaining reviewed.
1. Select changed code and add a comment. Confirm it persists after closing and reopening the review pane.
1. Add two current comments, choose **Ask agent**, and confirm the agent receives both file/line anchors; inspect the refreshed queue afterward.

## Human interventions

1. Select a changed range, use **Patch**, apply a small correction, and confirm the source reloads read-only.
1. Use **Undo last patch** and confirm the exact pre-patch bytes return.
1. From a task-baseline diff, use **Revert hunk** on a normal replacement hunk. Confirm the hunk matches the task-start snapshot and that a subsequent external edit causes a stale-write rejection.

## Verification and safety

1. Configure a passing and a failing `verification.commands` entry in Settings JSON. Confirm each result is shown in Review; hover for recent output.
1. Change a reviewed file after a check passes. Confirm the result becomes **Stale**.
1. Open px1 through a hostname/reverse proxy and confirm mutation routes (patch, agent, checks) are refused; localhost/IP use remains subject to the local-post guard.
1. Run `go test ./...`, `node ./scripts/build-web.js`, and `git diff --check` with Go 1.25+. Record exact versions and results in the release notes.
