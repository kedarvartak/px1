# Agent guidance

px1 helps developers understand the code their agents wrote before merging.
Prioritize human inspection and decisions, including code without findings.
Keep file-review progress independent of automated finding acknowledgements;
neither is proof of correctness or a GitHub approval.

px1 has two compatible product shapes. The free GitHub Action generates a
commit-pinned static review artifact. The hosted product adds a GitHub App,
repository indexing, evidence-backed AI findings, and authenticated report
hosting while preserving that report and the human file-review workflow.

Hosted changes must keep tenant boundaries explicit and must not execute pull
request code. Local agent editing and automatic approval remain outside the v1
hosted scope. The free Action stays maintained for open-source and self-run use.

Every product change should preserve explicit base/head identity, avoid
executing PR code, and keep approval in GitHub. AI findings must expose their
evidence and verifier result rather than acting as proof. Update the README,
product vision, roadmap, contracts, and fixture tests when behavior changes.

Before opening a PR, run `go test ./...`, `go test -race ./...`, `go vet ./...`,
and generate one report from two commits.
