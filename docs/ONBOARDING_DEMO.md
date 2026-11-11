---
title: Onboarding demo reviews
section: concepts
kind: narrative
status: built
covers:
  - internal/hosted/onboarding/
verified_at:
---

# Onboarding demo reviews

The `internal/hosted/onboarding` package queues one demo review per repository
for a new installation. For each repository it looks up the latest merged pull
request and enqueues a review of that pull request's head commit, labelled as
a demo.

Status: built. The code and its tests exist, but the hosted service is not
deployed (see [Hosted architecture](HOSTED_ARCHITECTURE.md)). The package is
one file, `demo.go`. No other package in the repository imports it, and the
two interfaces it depends on have no implementation outside its tests.

## Types

| Identifier | Purpose |
| --- | --- |
| `Repository` | `InstallationID`, `RepositoryID`, `Owner`, `Name` |
| `MergedPull` | `Number`, `HeadSHA`, `URL`, `MergedAt` |
| `DemoReview` | The queued job: `InstallationID`, `RepositoryID`, `PullNumber`, `HeadSHA`, `SourceURL`, `IdempotencyKey`, `Provenance` |
| `MergedPullReader` | Interface with `LatestMergedPull(ctx, repository)`; returns the pull request, a found flag and an error |
| `DemoReviewQueue` | Interface with `EnqueueDemoReview(ctx, job)`; returns whether a new job was created |
| `DemoScheduler` | Holds `Pulls` (a `MergedPullReader`) and `Queue` (a `DemoReviewQueue`) |

## Flow of `DemoScheduler.Schedule`

`Schedule(ctx, installationID, repositories)` returns the number of jobs newly
created.

1. Validate the call. `Pulls` and `Queue` must be non-nil, `installationID`
   must be greater than zero, and the list must hold between 1 and
   `MaxDemoRepositories` (20) repositories. Otherwise it returns
   `pull reader, queue, tenant, and one to twenty repositories are required`.
2. For each repository, in order:
   - Check that it belongs to the installation and is well formed (see
     below).
   - Call `LatestMergedPull`. If nothing is found, skip the repository.
   - Validate the returned pull request.
   - Build a `DemoReview` and call `EnqueueDemoReview`.
   - Count the job only if the queue reports it was created.

## Validation

| Subject | Rule | Error |
| --- | --- | --- |
| Repository | `InstallationID` equals the `installationID` argument, `RepositoryID` is positive, `Owner` and `Name` are non-empty, and the `RepositoryID` has not appeared earlier in the list | `demo repositories must be unique and belong to the installation` |
| Pull request | `Number` is positive, `HeadSHA` passes `fullSHA`, `MergedAt` is not the zero time, and `URL` starts with `https://` | `latest merged pull request is invalid` |

`fullSHA` requires exactly 40 hexadecimal characters in either case.

The repository check is the tenant boundary: a repository whose
`InstallationID` differs from the caller's is rejected before any pull request
lookup for it. `TestDemoSchedulerRejectsCrossTenantRepository` covers this.

## The queued job

| Field | Value |
| --- | --- |
| `InstallationID` | The `installationID` argument |
| `RepositoryID` | From the repository |
| `PullNumber` | `MergedPull.Number` |
| `HeadSHA` | `MergedPull.HeadSHA`, lower-cased |
| `SourceURL` | `MergedPull.URL` |
| `IdempotencyKey` | `demo:INSTALLATION:REPOSITORY:HEADSHA`, with the lower-cased SHA |
| `Provenance` | A fixed sentence, below |

The provenance text is always:

```
Demo review of the latest merged pull request selected during installation. It does not represent the current default branch or a live pull request.
```

## Idempotency

The scheduler does not remember what it has queued. Deduplication is the
queue's job: `EnqueueDemoReview` returns `false` when it did not create a job,
and the scheduler then leaves the count unchanged. In the test, the queue
stub keys on `IdempotencyKey`; a second `Schedule` call with the same
repository returns a count of 0.

Because the key includes the head SHA, a repository whose latest merged pull
request has changed produces a different key.

## Error handling

`Schedule` stops at the first error and returns it together with the count so
far. Errors from `LatestMergedPull` and `EnqueueDemoReview` are returned
unchanged. Jobs enqueued before the error stay enqueued; there is no rollback.

A repository with no merged pull request is not an error. It is skipped and
adds nothing to the count.

## Not implemented

- No implementation of `MergedPullReader` or `DemoReviewQueue`; the code does
  not show how the latest merged pull request is chosen or how the job is run.
- No handler or webhook wiring that calls `Schedule` at installation time.
  [Install the hosted GitHub App](HOSTED_INSTALL.md) describes the planned
  install flow; it does not mention demo reviews.
- No use of `Owner`, `Name` or `MergedAt` beyond validation; they are not
  copied into the job.
- No retry, rate limiting or concurrency; repositories are processed one at a
  time.
