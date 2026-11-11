---
title: Feedback learning
section: concepts
kind: narrative
status: built
covers:
  - internal/hosted/learning/
verified_at:
---

# Feedback learning

`internal/hosted/learning` turns reviewer feedback into bounded signals:
classified replies, stored memories, noise suppression, rule proposals and
weekly metrics. The code and its tests exist, but it is not deployed. See
[Hosted architecture](HOSTED_ARCHITECTURE.md) for the wider service.

The package comment states the principle: model classifications are evidence,
not authority over a pull request.

The package is logic plus interfaces. Storage, clustering, the model call and
GitHub publishing are all interfaces (`ReplyStore`, `ReplyModel`,
`ThreadReplier`, `MemoryStore`, `MemoryCatalogStore`, `RuleInferenceStore`,
`CommentClusterer`, `RuleProposalPublisher`, `ProposalQueueStore`,
`ProposalPullRequestPublisher`). The repository contains no non-test
implementation of any of them, and no non-test code outside the package
imports it.

Every entry point that takes a tenant rejects an `installationID` or
`repositoryID` that is zero or negative.

## Files

| File | Purpose |
| --- | --- |
| `replies.go` | Classify a reviewer's reply to a finding and answer questions. |
| `memory.go` | Retrieve memories for a review prompt, with a hard cap. |
| `catalog.go` | Page through memories for display, requiring evidence. |
| `suppression.go` | Decide whether a finding is hidden because similar ones were ignored. |
| `inference.go` | Propose repository rules from recurring human review comments. |
| `proposal_queue.go` | Edit, reject or accept a queued rule proposal. |
| `metrics.go` | Aggregate one week of review outcomes for a repository. |

## Reply classification

`ReplyProcessor.Process(ctx, input)` classifies one reply with one model call.

Validation, before the model is called:

- `Model` and `Store` are set; `Provider` and `ModelName` are not blank.
- `InstallationID`, `RepositoryID`, `CommentID` and `ActorID` are positive and
  `FindingID` is non-empty.
- `Reply` is not blank. `Reply` and `Finding` are each at most `maxReplyBytes`,
  which is `8 << 10` (8192 bytes).

The request sets `MaxOutputTokens: 300` and `Temperature: 0`. The finding and
the reply are sent as a JSON object in the prompt, and the system prompt tells
the model to treat the supplied text as data, not instructions, and to return
only JSON.

`decodeReplyClassification` then enforces the output shape:

| Rule | Detail |
| --- | --- |
| Strict JSON | Unknown fields are rejected; any data after the first JSON value is rejected |
| `kind` | One of `agree`, `disagree`, `context`, `question` |
| `confidence` | Between 0 and 1 inclusive |
| `answer` | Required for `question`, forbidden for every other kind, at most 4000 bytes after trimming |

A valid classification is saved with `ReplyStore.SaveReplyClassification`. If
the kind is `question`, the answer is posted with
`ThreadReplier.ReplyToComment`. A `question` with a nil `Replier` returns an
error, but only after the classification has been saved. A model, decode,
store or reply failure returns an error and an empty classification.

`Confidence` is validated and stored but no code in the package acts on it. No
migration defines a table for reply classifications.

## Memory retrieval

A `Memory` has an `ID`, `Scope`, `Kind`, `Text`, optional `PathGlob` and
`Language`, a `Weight`, `EvidenceLinks` and `LastConfirmedAt`.

`RetrieveMemories(ctx, store, query)` fetches memories for a prompt.

- It requires a store, a positive tenant, a `Path`, a `Language` and a
  non-empty `Embedding`.
- The path is cleaned and converted to forward slashes. Absolute paths, `..`
  and paths starting with `../` are rejected.
- `Limit` is forced to `MaxPromptMemories` (20) when it is zero, negative or
  larger than 20. A caller cannot raise the cap.
- Results from `MemoryStore.SearchMemories` are truncated to the limit again,
  so a store that returns too many cannot exceed it.
- Every returned memory must have an ID, non-blank text and a weight between 0
  and 1, or the whole call fails.

The store performs tenant, scope and similarity filtering; this function does
not rank or filter by scope itself.

### `memories` table

Migration `0007_memories` defines the table.

| Column or rule | Definition |
| --- | --- |
| `scope` | `organization`, `repository`, `path` or `language` |
| `kind` | `preference`, `suppression`, `convention` or `fact` |
| `memory_text` | 1 to 8000 characters |
| `weight` | `real`, 0 to 1, default 1 |
| `evidence` | `jsonb`, default empty array |
| `embedding` | `vector(1536)`, with an HNSW cosine index; `embedding_model` is required |
| Scope checks | `organization` rows have no `repository_id` and all others have one; `path_glob` is set only for `path` scope; `language` is set only for `language` scope |
| Tenant isolation | Row level security is enabled and forced; policy `tenant_isolation` compares `installation_id` with the `px1.installation_id` setting |

## Memory catalogue

`VisibleMemoryPage(ctx, store, installationID, repositoryID, afterID)` returns
one `MemoryPage` for display. It asks `MemoryCatalogStore.ListMemories` for
`MemoryPageSize + 1` rows, where `MemoryPageSize` is 100. If more than 100 come
back it keeps the first 100 and sets `NextAfterID` to the last kept ID;
otherwise `NextAfterID` is empty.

Every memory on the page must have an ID, non-blank text and at least one
evidence link, and every link must be an `https` URL whose host is
`github.com` (case-insensitive). One failing memory fails the whole page.

## Noise suppression

`DecideSuppression(finding, events, config, now)` is a pure function that
returns a `SuppressionDecision` with `Suppress`, a `Reason` and the
`EvidenceIDs` that counted.

Protected findings are never suppressed: severity `P0`, category `security`
and category `logic` return `Suppress: false` whatever the feedback history
is. Severity must be `P0`, `P1` or `P2`; a missing path, category or time is an
error.

| Setting | Default | Limit |
| --- | --- | --- |
| `Threshold` | `DefaultSuppressionThreshold` (3) | Values below 3 are an error |
| `Window` | `DefaultSuppressionWindow` (30 days) | Must be positive and at most 365 days |
| `PathGlob` | `**` | Must be a valid `path.Match` pattern |

A `NoiseEvent` counts when all of these hold:

- its `ID` is positive and not already counted;
- its `Category` equals the finding's category;
- its `OccurredAt` is inside the window and not after `now`;
- its `Kind` is `ignored` or `thumbs_down`;
- when `PathGlob` is not `**`, its `Path` matches the glob.

The finding is suppressed when the count reaches the threshold. If `PathGlob`
is not `**` and the finding's own path does not match it, the decision is
"outside the configured path scope" and nothing is counted.

Globs use Go's `path.Match`. Only the exact value `**` is treated as "match
everything"; inside any other pattern `*` does not cross a `/`, so `src/*`
matches `src/a.go` but not `src/pkg/a.go`.

The `feedback_events.kind` check in `0001_hosted_core` allows `thumbs_up`,
`thumbs_down`, `reply`, `resolved` and `line_changed`. It has no `ignored`
value; the code does not show where `ignored` events come from.

## Rule inference

`RuleInferenceJob.Run(ctx, installationID, repositoryID)` proposes rules from
human review comments.

1. Load comments newer than `RuleInferenceWindow` (7 days) with
   `RuleInferenceStore.HumanReviewComments`.
2. Pass them to `CommentClusterer.Cluster`, which returns `CommentCluster`
   values with a `Theme`, a `RuleText` and `CommentIDs`.
3. For each cluster, keep only comment IDs that were loaded in step 1, are not
   repeated, have a positive pull request number and a URL starting with
   `https://github.com/`.
4. Skip the cluster unless it has at least 3 such comments from at least 2
   different pull requests, a theme of 1 to 200 bytes and rule text of 1 to
   8000 bytes.
5. Compute `proposalFingerprint`: the SHA-256 hex digest of the theme and rule
   text, lowercased with whitespace collapsed. Skip if
   `RuleProposalExists` reports that fingerprint.
6. Call `RuleProposalPublisher.OpenRulesPullRequest` for the path
   `.px1/rules.md` with the title `px1: propose <theme>`.
7. Call `MarkRuleProposal` with the fingerprint and the comment IDs.

The pull request body starts with the marker
`<!-- px1-rule-proposal:<fingerprint> -->`, then the rule text, the evidence
comment URLs, and a note that the rule is a proposal to review and edit.

`Run` returns the number of proposals opened. Load and cluster errors return
zero. An error in steps 5 to 7 stops the run and returns the count so far. If
the pull request opens but `MarkRuleProposal` fails, the fingerprint is not
recorded.

## Proposal queue

`ProposalQueue` manages a `QueuedRuleProposal`, whose `State` is `pending`,
`publishing`, `accepted` or `rejected`.

Every operation first loads the proposal (`loadProposal`) and rejects it unless
the returned ID, installation and repository equal the ones requested, the rule
text is not blank, and there is at least one evidence link. Each link must be
an `https` URL on `github.com` with a non-empty path. Checking the returned
tenant against the request means a store that returns another tenant's row
causes an error.

| Method | Allowed from | Effect |
| --- | --- | --- |
| `Edit` | `pending` | Trims the text, requires 1 to 8000 bytes, calls `UpdatePendingProposal` |
| `Reject` | `pending` | Calls `RejectPendingProposal` |
| `Accept` | `pending` or `publishing` | Opens a pull request and returns its URL |

`Accept` flow:

1. From `pending`, call `ClaimPendingProposal`. If the claim fails or is lost,
   return an error ("proposal is no longer pending"). From `publishing`, skip
   the claim, which lets an interrupted accept be retried.
2. Call `ProposalPullRequestPublisher.OpenProposal`, which the interface
   requires to be idempotent for the proposal ID.
3. If publishing fails, or the returned URL is not
   `https://github.com/<owner>/<repo>/pull/<positive number>` exactly
   (`githubPullRequestURL`), call `ReleaseProposal` and return an error. The
   result of `ReleaseProposal` is ignored.
4. Call `CompleteProposal` with the URL. If that fails the error is returned
   and the claim is not released.

No migration defines a table for queued proposals. The code does not show how
proposals enter the queue, or how the queue relates to `RuleInferenceJob`,
which opens pull requests directly.

## Weekly metrics

`AggregateWeeklyMetrics(installationID, repositoryID, week, outcomes)` builds
one `WeeklyRepositoryMetrics` snapshot from `ReviewOutcome` values. The week
runs from Monday 00:00 UTC (`startOfUTCWeek`) for 7 days; the start is
inclusive and the end exclusive. Outcomes outside the week are skipped.

| Field | Calculation |
| --- | --- |
| `PullRequests` | Outcomes in the week |
| `Comments` | Sum of `Comments` |
| `CommentsPerPR` | `Comments / PullRequests`; 0 with no pull requests |
| `AddressedRate` | Sum of `Addressed` divided by `Comments`; 0 with no comments |
| `DownvoteRate` | Sum of `Downvoted` divided by `Comments`; 0 with no comments |
| `Suppressed` | Sum of `Suppressed` |

The call fails if any outcome has an empty `PullRequestID`, a negative count,
or `Addressed` or `Downvoted` greater than `Comments`. A `PullRequestID` that
repeats one already counted in the week also fails.

The function computes only; it does not store. Migration
`0008_weekly_repository_metrics` defines `weekly_repository_metrics`, keyed by
`(installation_id, repository_id, week_start)`, with a check that `week_start`
is a Monday, both rates between 0 and 1, and row level security enabled and
forced.

## Not implemented

- No store, clusterer, model adapter or GitHub publisher exists outside the
  tests.
- No worker, scheduler or HTTP handler calls this package.
- No code writes memories, or turns a classified reply or a suppression
  decision into a memory.
- `DecideSuppression` returns a decision; nothing here applies it to a review.
- No migration exists for reply classifications, rule proposal fingerprints or
  the proposal queue.
