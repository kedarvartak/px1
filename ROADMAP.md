# px1 Roadmap

px1 stays local-first, fast, and useful without a cloud account. Cloud
features should extend the review workflow rather than make the core editor
dependent on them.

## Next: GitHub-connected reviews

Make a px1 review link attachable to a GitHub pull request, pinned to the PR's
exact head commit. Reviewers should be able to open the link and see the
task-baseline diff, comments, explanations, verification results, and final
review disposition in px1.

This work has two supported flows:

- **Self-hosted:** teams run px1 inside their network and use a GitHub Action
  plus `PX1_REVIEW_BASE_URL` to publish or update a PR link.
- **Hosted platform:** users sign up, install the px1 GitHub App, and let the
  platform manage webhooks, snapshots, authentication, storage, and optional
  AI providers.

The detailed design is in
[GitHub Review Integration Plan](docs/GITHUB_REVIEW_INTEGRATION_PLAN.md).

### Delivery stages

1. Local snapshot export/import contract, commit-pinned deep links, review
   approval state, and fake GitHub/LLM providers.
2. Self-hosted Action, import endpoint, secure link configuration, and
   deployment documentation.
3. Hosted GitHub App, accounts, organizations, repository permissions, and
   webhook processing.
4. Hosted snapshot storage, managed or bring-your-own LLM keys, quotas, and
   optional write-back of comments/statuses to GitHub.

## Completed foundations

- Task-baseline review diffs, including files added or deleted after review
  starts.
- Cross-worktree review inbox with search and status filters.
- Review comments, patches, hunk reverts, decision memory, rules, and stale
  result detection.
- Named verification commands with a Settings editor and one-click Run all.
