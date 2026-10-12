# GitHub review artifact architecture

px1 helps developers understand the code their agents wrote before merging.
This architecture delivers inspectable code and evidence for that human review.

## Supported flows

px1 generates the same static, commit-pinned bundle for two delivery paths:

1. A pull request is opened, synchronized, or reopened.
2. A `pull_request_target` workflow loads trusted workflow code from the base
   branch.
3. The workflow fetches the PR head as Git objects without executing it.
4. px1 compares the exact base and head SHAs and generates `index.html`,
   `review.json`, and `checksums.sha256`.
5. The workflow either uploads an authenticated, expiring Actions artifact or,
   after an explicit visibility decision, publishes through GitHub Pages.
6. A sticky PR comment links to the selected delivery location.

## Artifact identity

Every report contains full base and head commit SHAs. The private workflow gives
each head its own immutable Actions artifact. The Pages workflow creates a new
path rather than changing the old report:

```text
https://<owner>.github.io/<repo>/reviews/<head-sha>/
```

In Pages mode, the generated-only `px1-review-reports` branch retains historical
reports. Its tree is validated before restore and may contain only regular files
matching:

```text
reviews/<40-character-lowercase-sha>/{index.html,review.json,checksums.sha256}
```

## Inputs

The generator reads only commit-pinned data:

- base and head Git commits;
- `.px1/rules.json` from the head commit;
- GitHub check runs collected for the head SHA; and
- optional explanation JSON whose revision equals the head SHA.

The report contains changed files, structured hunks, rule findings, attention
signals, bounded inspection questions with observed evidence and related-file
references, verification results, and optional explanations.

## Security boundaries

- Workflow and Action code come from a trusted px1 release and the PR base.
- PR code is inspected as data and is never executed.
- Shell arguments are passed through environment variables and quoted arrays.
- Verification URLs must be HTTPS.
- Imported verification and explanation revisions must equal the report head.
- The static report has no backend and makes no network requests.
- Reviewer acknowledgement is browser-local progress, not approval.
- The default privacy check blocks high-confidence secret signatures and
  sensitive paths before delivery; it is detection, not redaction or access
  control.
- Enabling model explanations transfers bounded, non-excluded diff content to
  the configured provider and emits a workflow warning.
- `checksums.sha256` detects changed bundle files but does not prove authorship.

GitHub remains the authority for comments, requested changes, and approval.

## Delivery contract

Use `examples/px1-review-private.yml` for private repositories unless a reviewed
GitHub Pages access-control configuration is already in place. Its download URL
requires GitHub authentication and repository read access, and the example uses
a 30-day retention period. Download and unzip the artifact before opening
`index.html`.

Use `examples/px1-review.yml` only after deliberately confirming Pages
visibility. GitHub Pages can be public even when its source repository is
private. Neither the privacy check nor checksums make public publication safe.

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

## Evidence and limitations contract

`review.json` exposes human progress, deterministic analysis, optional AI
explanations, and CI verification as independent evidence sources. Each source
has a state, a plain-language description, explicit limits, and the full base
and/or head revision it represents. Missing AI and CI inputs are distinguishable
from attached inputs that contain zero entries.

Deterministic analysis is marked `limited` when attention scanning or
inspection-question generation skips content or reaches a bound. The relevant
component and limit are retained in the evidence entry. An empty finding list
means only that no rule or signal matched. Reports are generated snapshots; they
do not poll GitHub and cannot establish the current PR head or live checks.

## Inspection-question contract

Inspection questions are deterministic prompts derived from existing attention
evidence and changed source/test relationships. Each entry in `review.json` keeps the
observed evidence separate from the question and identifies its primary changed
file, optional line, and related paths. Links are emitted only for files present
in the diff.

Questions are bounded to 100 entries and related paths to 8 per question; the
report exposes either truncation explicitly. Questions are not findings,
acknowledgements, AI output, inferred author intent, or evidence that a behavior
is correct.

## Deliberately unsupported

- Local workspace servers
- Snapshot import endpoints
- Signed self-hosted links
- px1 user accounts
- Hosted snapshot storage
- Agent-driven code edits
- Mutable comments or approvals inside a static report

These features create a second product architecture and are not planned.
