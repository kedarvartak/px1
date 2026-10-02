# GitHub Review Integration Plan

px1 should support two deployment flows that share the same review model and
URL contract:

1. **Self-hosted px1:** a team runs the review service in its own network.
2. **Hosted px1:** a user signs up, connects GitHub, and uses managed review
   links and storage.

The core review experience must remain useful without an LLM API key. AI
explanations are an optional provider layered on top of the review snapshot.

## Shared review link

Every external review link is pinned to an immutable commit SHA:

```text
https://<px1-host>/github/<owner>/<repo>/pull/<number>?sha=<head-sha>
```

The URL identifies the GitHub PR, while the SHA prevents the review from
silently changing when the PR receives another commit. A new PR commit creates
a new snapshot and a new link target.

The review snapshot contains:

- changed files and baseline/current contents;
- review comments and their line anchors;
- AI explanations, when available;
- verification results;
- review disposition: pending, approved, or changes requested.

## Flow A: self-hosted environment

The customer owns the px1 host, repository data, and credentials.

1. Deploy px1 next to the repository or on an internal HTTPS host.
2. Configure a repository or organization variable such as
   `PX1_REVIEW_BASE_URL`.
3. A GitHub Action runs on PR open/update, checks out the exact head SHA, and
   sends the PR metadata plus a review snapshot to the self-hosted px1 import
   endpoint.
4. The Action creates or updates one sticky PR comment containing the pinned
   px1 link.
5. Reviewers open the link through the company network or VPN.
6. Optional write-back uses the Action's `GITHUB_TOKEN` or a customer-managed
   GitHub App to post review summaries/statuses back to GitHub.

Self-hosted installations can use local coding harnesses, a team-owned LLM
endpoint, or no LLM at all. No px1 cloud account is required.

## Flow B: hosted px1 platform

The platform owns hosting, snapshot storage, and the user experience; GitHub
access is granted explicitly by the customer.

1. The user signs up and creates or joins an organization.
2. The user installs the px1 GitHub App and chooses repositories.
3. GitHub sends pull-request webhooks for opened, synchronized, reopened, and
   closed events.
4. The platform fetches the PR's exact head SHA, creates a snapshot, and stores
   it under the organization and repository.
5. The platform posts the pinned px1 link to the PR.
6. Reviewers use px1 for diffs, comments, explanations, verification, and the
   approval gate.
7. px1 can optionally post a summary, review decision, or check status back to
   GitHub.

Hosted AI should support both a platform-managed provider key and a future
bring-your-own-key option. Keys must remain server-side and must never be
embedded in review links or browser code.

## Code to build before deployment

These pieces can be implemented and tested locally without external accounts:

1. Add a versioned review snapshot export/import format. The first local
   export slice is now available as `px1 export-review`: it takes explicit
   `--base` and `--head` commits, reads team rules and verification from the
   head commit, and writes a self-contained `index.html` containing changed
   files, unified diffs, rule findings, and verification status. It is suitable
   for a local preview or as the artifact consumed by a future GitHub Action;
   it intentionally does not publish, authenticate, or call an LLM. An
   optional `--explanations-file` input accepts provider-neutral, commit-pinned
   explanation JSON and renders expandable chips; generation remains a
   separate local or CI concern. Changed files are parsed into structured
   hunks with old/new line numbers and addition/removal highlighting, while
   binary or otherwise unparsed patches retain a raw-diff fallback.
2. Add commit-pinned GitHub review URL parsing and routing.
3. Add persisted review disposition with approval guards for stale files and
   open comments.
4. Add an import endpoint that accepts PR metadata and a snapshot, with strict
   SHA validation.
5. Add a GitHub Action template that publishes or updates the sticky link when
   `PX1_REVIEW_BASE_URL` is configured. The first Pages-based workflow is now
   implemented in `.github/workflows/px1-review-report.yml`: it runs trusted
   base-branch code, fetches the PR head as an object, publishes
   `reviews/<head-sha>/index.html`, and creates or updates one marked PR
   comment. It does not execute the PR checkout. Before export it also reads
   GitHub check runs for the exact head SHA and supplies them through the
   existing `.px1/verification.json` contract, so the report shows CI status
   without requiring that file to be committed by the PR.
6. Add interfaces for GitHub and LLM providers, with local fakes for tests.
7. Add documentation and fixture-based tests for both flows.

## Credentials and security

- Self-hosted mode uses customer-controlled GitHub credentials or the Action's
  short-lived `GITHUB_TOKEN`.
- Hosted mode uses a GitHub App with least-privilege repository permissions.
- Snapshot imports must validate repository, PR number, and full commit SHA.
- Review links should be authenticated or signed before exposing private code.
- API keys are server-side configuration only; the browser receives provider
  results, never secrets.
- Comments and approvals must remain tied to the snapshot SHA and become stale
  after the PR changes.

## GitHub Pages workflow

The repository can publish the local exporter without a px1 server. Enable
GitHub Pages with **GitHub Actions** as its source, then keep
`.github/workflows/px1-review-report.yml` on the target base branch. On each
opened, synchronized, or reopened non-draft pull request, it creates a link in
the form:

```text
https://<owner>.github.io/<repo>/reviews/<head-sha>/
```

The workflow uses `pull_request_target` so it can update the PR comment and
deploy Pages for fork pull requests. Its checkout is always the trusted base
SHA; the PR head is fetched only to let Git inspect its diff and committed
`.px1/rules.json`. Do not change it to execute code from the PR head. The
initial Pages deployment contains the latest generated report; retaining every
historical SHA across deployments is a follow-up improvement.

Static findings include a short hunk-local context block with line numbers and
added, unchanged, or removed markers. The full unified diff remains available
below the findings for reviewers who need broader context.

Reviewers can acknowledge individual team-rule findings in the generated page.
Acknowledgements are intentionally browser-local and keyed by repository plus
the exact head SHA, so they require no account and cannot leak onto a later PR
revision. They are personal review progress, not a shared approval signal.

## Suggested delivery order

1. Local snapshot contract, deep links, review disposition, and fake providers.
2. Self-hosted Action plus import endpoint and deployment documentation.
3. GitHub App, accounts, organization/repository access, and webhook handling.
4. Hosted storage, managed LLM provider, usage limits, and optional BYOK.
5. GitHub write-back for summaries, checks, comments, and approval status.

The first milestone proves the review workflow without requiring hosting,
GitHub credentials, or an LLM API key.
