# GitHub review artifact architecture

px1 helps developers understand the code their agents wrote before merging.
This architecture delivers inspectable code and evidence for that human review.

## Supported flow

px1 supports one delivery path:

1. A pull request is opened, synchronized, or reopened.
2. A `pull_request_target` workflow loads trusted workflow code from the base
   branch.
3. The workflow fetches the PR head as Git objects without executing it.
4. px1 compares the exact base and head SHAs and generates one static HTML file.
5. The workflow restores older reports, adds the new SHA report, and publishes
   the complete site through GitHub Pages.
6. A sticky PR comment links to `reviews/<head-sha>/`.

## Artifact identity

Every report contains full base and head commit SHAs. A new PR commit creates a
new path rather than changing the old report:

```text
https://<owner>.github.io/<repo>/reviews/<head-sha>/
```

The generated-only `px1-review-reports` branch retains historical reports. Its
tree is validated before restore and may contain only regular files matching:

```text
reviews/<40-character-lowercase-sha>/index.html
```

## Inputs

The generator reads only commit-pinned data:

- base and head Git commits;
- `.px1/rules.json` from the head commit;
- GitHub check runs collected for the head SHA; and
- optional explanation JSON whose revision equals the head SHA.

The report contains changed files, structured hunks, rule findings, attention
signals, verification results, and optional explanations.

## Security boundaries

- Workflow and Action code come from a trusted px1 release and the PR base.
- PR code is inspected as data and is never executed.
- Shell arguments are passed through environment variables and quoted arrays.
- Verification URLs must be HTTPS.
- Imported verification and explanation revisions must equal the report head.
- The static report has no backend and makes no network requests.
- Reviewer acknowledgement is browser-local progress, not approval.

GitHub remains the authority for comments, requested changes, and approval.

## Human review progress contract

Every changed file has one explicit state: `unreviewed`, `reviewed`, or
`follow-up`. States are browser-local entries under `px1:file-review:` followed
by the JSON-encoded repository/base/head tuple. Only recognized states for paths
in the current report are restored. Changing either revision starts a fresh
comparison; generated HTML and `review.json` are never modified by review.

Finding acknowledgements remain independent. Neither scrolling nor clearing
findings marks a file reviewed. Next-unreviewed navigation includes unflagged
files and clears search and flag filters to reveal its destination. A storage
failure leaves progress usable in memory and displays its temporary nature.
Review declarations are not attestations of comprehension or shared approvals.

## Deliberately unsupported

- Local workspace servers
- Snapshot import endpoints
- Signed self-hosted links
- px1 user accounts
- Hosted snapshot storage
- Agent-driven code edits
- Mutable comments or approvals inside a static report

These features create a second product architecture and are not planned.
