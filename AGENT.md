# Agent guidance

px1 has one product flow: GitHub Actions generates a commit-pinned static
review artifact, GitHub Pages serves it, and a PR comment links to it.

Keep changes inside that boundary. Do not reintroduce a local server, workspace
browser, mutable review sessions, agent editing, self-hosted imports, accounts,
or hosted application state.

Every product change should preserve explicit base/head identity, avoid
executing PR code, and keep approval in GitHub. Update the README, product
vision, roadmap, contracts, and fixture tests when behavior changes.

Before opening a PR, run `go test ./...`, `go test -race ./...`, `go vet ./...`,
and generate one report from two commits.
