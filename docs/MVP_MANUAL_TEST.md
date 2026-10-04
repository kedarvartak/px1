# px1 P1 MVP manual test checklist

Run this checklist on Go 1.25+ before tagging the P1 MVP. Use a disposable Git worktree with a configured local coding harness and a GitHub Actions verification fixture.

## Review flow

1. Start px1, open **Review**, and start a review session. Confirm it reports no changes.
1. Modify two files outside px1 (or through the agent). Confirm only those files appear in the queue and **Next change** opens the task-baseline diff.
1. Mark one file reviewed, modify it again, refresh the queue, and confirm its state becomes **stale** rather than remaining reviewed.
1. Use the review search field and status filters to narrow the queue; confirm **Next change** follows the visible pending result.
1. Select changed code and add a comment. Confirm it persists after closing and reopening the review pane.
1. Add two current comments, choose **Ask agent**, and confirm the agent receives both file/line anchors; inspect the refreshed queue afterward.

## Human interventions

1. Select a changed range, add a review comment, and confirm the comment appears on the review diff.
1. From a task-baseline diff, use **Revert hunk** on a normal replacement hunk. Confirm the hunk matches the task-start snapshot and that a subsequent external edit causes a stale-write rejection.

## Verification and safety

1. Start a review and place a `.px1/verification.json` report in the workspace with `source: "github-actions"`, the active commit SHA, one passing check, and one failing check. Confirm both results appear in Review and link clicks open HTTPS CI URLs.
1. Change the report revision to another SHA and confirm the results are rejected as belonging to a different revision. Remove the report and confirm Review remains usable with an empty CI state.
1. Open px1 through a hostname or LAN IP and confirm patch, agent, and check attempts are refused. Repeat with only `-allow-patch` and `PX1_REMOTE_TOKEN`, and confirm agent execution is still refused while an authenticated patch reaches path checks. Loopback keeps local patch and agent access.
1. Run `go test ./...`, `node ./scripts/build-web.js`, and `git diff --check` with Go 1.25+. Record exact versions and results in the release notes.
