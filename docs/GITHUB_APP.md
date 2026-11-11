---
title: GitHub App delivery
section: concepts
kind: narrative
status: built
covers:
  - internal/hosted/githubapp/
verified_at:
---

# GitHub App delivery

`internal/hosted/githubapp` holds the code that talks to GitHub as an
installed GitHub App: it mints installation tokens, sends rate-limit-aware API
requests, publishes check runs, reviews and a sticky summary comment, parses
`@px1` comment commands, and polls reactions. The package is built: the code
and tests exist, but it is not deployed. No non-test Go code outside the
package imports it, so nothing in the hosted service calls it yet.

See [Hosted architecture](HOSTED_ARCHITECTURE.md) for the surrounding service,
[GitHub App registration](GITHUB_APP_REGISTRATION.md) for the App itself, and
[SCM providers](SCM_PROVIDERS.md) for the provider-neutral interface.

## Files

| File | Main identifiers | Purpose |
| --- | --- | --- |
| `client.go` | `AppMinter`, `CachedTokenSource`, `Client` | Authentication and the HTTP client |
| `checks.go` | `CheckRunPublisher`, `ReviewConclusion` | Check run start and completion |
| `review.go` | `ReviewPublisher`, `MaxInlineReviewComments` | One pull request review with inline comments |
| `summary.go` | `StickySummaryPublisher`, `StaticReviewCommentMarker` | Create or update one summary comment |
| `commands.go` | `ParseCommand`, `CommandAuthorizer` | `@px1` commands and the permission check |
| `remember.go` | `RememberHandler`, `ExplicitMemory` | Store a memory from `@px1 remember` |
| `forget.go` | `ForgetHandler` | Delete a memory from `@px1 forget` |
| `reactions.go` | `ReactionPoller` | Collect thumbs up and thumbs down on review comments |

Every publisher takes a `BaseURL`, which defaults to `https://api.github.com`
when empty, and a client with one method,
`Do(ctx, installationID, request)`. That method is declared three times, as
`APIClient`, `CheckAPIClient` and `SummaryAPIClient`; `Client` satisfies all
three.

## Authentication

### `AppMinter`

`AppMinter.Mint` exchanges a GitHub App JWT for an installation token.

1. It requires a non-empty `AppID`, a non-nil RSA `PrivateKey`, and an
   installation ID greater than zero. The code comment says the key is
   supplied by the deployment secrets manager; the package does not load it.
2. `signAppJWT` builds an RS256 token with `iss` set to the App ID, `iat` set
   30 seconds in the past, and `exp` 9 minutes in the future.
3. It sends `POST /app/installations/{id}/access_tokens` with no body, so the
   request does not narrow the token to particular repositories or
   permissions.
4. A status outside 200 to 299 returns an error with the status code only.
5. The response is decoded through a 1 MiB limit into a `Token` with `Value`
   and `ExpiresAt`.

When `HTTP` is nil it uses `http.DefaultClient`, which has no timeout.

### `CachedTokenSource`

`CachedTokenSource` wraps a `Minter` and keeps tokens in a map in process
memory. Nothing writes them to disk or a database.

- A cached token is reused while the current time plus `Skew` is before its
  expiry. `Skew` defaults to 2 minutes when zero or negative.
- Concurrent calls for the same installation share one mint. Waiting callers
  receive the same token or error, or their own context error if it ends
  first.
- A minted token that is empty, or that already expires within the skew, is
  turned into an error and not cached.
- Failed mints are not cached; the next call tries again.

## The `Client`

`Client.Do` sends one request as an installation and handles retries.

It reads the whole request body into memory so it can be replayed, with no
size limit. On every attempt it fetches a token from `Tokens` and sets three
headers on a clone of the request, overwriting any existing values:

| Header | Value |
| --- | --- |
| `Authorization` | `Bearer ` followed by the installation token |
| `Accept` | `application/vnd.github+json` |
| `X-GitHub-Api-Version` | `2022-11-28` |

### Retry rules

A request is retryable only if its method is `GET`, `HEAD` or `OPTIONS`, or
it carries an `X-GitHub-Idempotency-Key` header. Other requests are sent
exactly once. None of the publishers in this package set that header, so
their `POST` and `PATCH` calls are never replayed.

`MaxRetries` defaults to 3 when zero or negative, giving at most four attempts.

For a retryable request, `retryDelay` decides whether a response should be
retried and how long to wait. Only status 429, 403, and 500 or above are
considered:

| Condition, checked in order | Wait |
| --- | --- |
| `Retry-After` is a number of seconds | That many seconds |
| `Retry-After` is an HTTP date | Until that time |
| `X-RateLimit-Remaining` is `0` and `X-RateLimit-Reset` parses | Until the reset time |
| Status is 429, or 500 or above | `backoff(attempt)` |
| Otherwise (a 403 with none of the above) | Not retried |

`backoff` is 1 second doubled per attempt, capped at 32 seconds. Waits taken
from `Retry-After` or `X-RateLimit-Reset` have no upper bound in the client;
only the context can cut them short. Negative waits become zero.

A transport error on a retryable request is retried with `backoff`. When
retries run out, or the request is not retryable, `Do` returns the last
response with a nil error. Callers must check the status code themselves, and
every publisher here does.

## Check runs

`CheckRunPublisher` creates and completes a check run named `px1 review`.

`Start` posts to `/repos/{owner}/{repo}/check-runs` with status
`in_progress`, the run ID as `external_id`, the details URL, and the output
title `px1 review in progress`. It returns the check run ID, and fails if
GitHub returns an ID that is not positive.

`Complete` patches the check run to `completed` with a conclusion, a
`completed_at` time in UTC, a title and summary, and one action button:
label `Retry review`, identifier `RetryReviewAction` (`retry_review`).

| Input | Rule |
| --- | --- |
| Owner and repository | Letters, digits, `-`, `_`, `.` only; not `.` or `..` |
| `HeadSHA` | Exactly 40 hexadecimal characters |
| `RunID` | Non-empty after trimming |
| `DetailsURL` | Parses with scheme `http` or `https` and a host |
| `Conclusion` | Only `neutral` or `failure` |
| `Title` (on completion) | Non-empty after trimming |
| Summary text | Cut at 60,000 bytes, then `[truncated by px1]` is appended |

The summary cut is by byte offset, so it can split a multi-byte character.

`ReviewConclusion(hasP0, failOnP0)` returns `failure` only when both are
true, and `neutral` otherwise. The package never reports `success`.

Nothing in this package handles the `retry_review` action when a user clicks
it.

## Pull request reviews

`ReviewPublisher.Create` posts all inline findings in one request to
`/repos/{owner}/{repo}/pulls/{number}/reviews`.

- The review `event` is always `COMMENT`. The package never approves or
  requests changes.
- `commit_id` is the validated 40-character head SHA.
- The body is prefixed with `<!-- px1-review-run:{RunID} -->`.
- More than `MaxInlineReviewComments` (15) findings is an error; nothing is
  sent and nothing is dropped silently.
- Each finding must pass `review.Finding.Validate`.

`RunID` must be 1 to 100 characters from letters, digits, `-`, `_` and `.`.
This stops a run ID from closing the HTML comment early; a test rejects
`run --> forged` before any request is made.

Each comment is placed on the `RIGHT` side at the finding's `EndLine`. When
`StartLine` differs, `start_line` and `start_side` are sent as well. The
comment body shows severity, title, body, evidence and confidence to two
decimals. A suggestion is wrapped in a `suggestion` code fence, and the fence
is lengthened until it does not appear inside the suggestion text.

The code comment states that `Create` does not retry and that the caller's
durable run state is the idempotency boundary.

## Sticky summary

`StickySummaryPublisher.Upsert` keeps one summary comment per pull request.

The body must start with `StaticReviewCommentMarker`
(`<!-- px1-static-review -->`). `find` then pages through the issue comments,
100 per page, looking for a comment where all three hold:

- `user.type` is `Bot`,
- `user.login` equals the `botLogin` argument, ignoring case,
- the body contains the marker.

If one is found it is updated with `PATCH`, and `Upsert` returns `updated`.
Otherwise a new comment is created with `POST`, and it returns `created`. A
comment by a normal user that contains the marker is ignored, which a test
covers.

`MaxPages` defaults to 10. If the last allowed page is full, `find` returns
`PR comment search reached its page limit` and nothing is posted. Each page is
decoded through a 2 MiB limit.

## Comment commands

`ParseCommand` reads only the first non-empty line of a comment. The line must
start with `@px1` (any case) followed by a command word (any case).

| Command | Arguments | Result |
| --- | --- | --- |
| `@px1 review` | none | `Name: review` |
| `@px1 ignore` | none | `Name: ignore` |
| `@px1 why` | none | `Name: why` |
| `@px1 explain PATH` | exactly one path | `Path` cleaned to forward slashes |
| `@px1 remember TEXT` | one or more words | `Text`, words joined by single spaces, at most 2000 bytes |
| `@px1 forget ID` | exactly one ID | `MemoryID` lowercased |

Extra arguments make `review`, `ignore`, `why`, `explain` and `forget`
invalid. The `explain` path must be relative and must not be `.`, `..`, or
start with `../` after cleaning. The `forget` ID must be 36 characters of
hexadecimal with hyphens at positions 8, 13, 18 and 23.

`ParseCommand` only parses. The package has handlers for `remember` and
`forget`; it has none for `review`, `ignore`, `why` or `explain`.

### Permission check

`CommandAuthorizer.Authorize` parses the comment, validates the installation,
owner, repository and actor login, then calls
`GET /repos/{owner}/{repo}/collaborators/{actor}/permission`.

The command is returned only when the status is exactly 200 and the
`permission` field is `write`, `maintain` or `admin`. `read`, `triage`, any
other value, any other status, and any transport or decode error all return an
error. The code comment says the caller must not enqueue work before this
check.

The actor login is checked with the same character rule as owner and
repository names, so a login containing other characters, such as `[`, is
rejected before any request.

### `remember` and `forget`

Neither handler checks permissions. Their comments say the caller must run
`CommandAuthorizer` first.

`RememberHandler.Handle` requires a `remember` command with text, and an
`ExplicitMemory` with positive `InstallationID`, `RepositoryID` and `ActorID`
and non-empty `FindingID`, `ThreadID` and `EvidenceURL`. The evidence URL must
have scheme `https` and host `github.com`. The handler overwrites `Text` with
the command text and `CreatedAt` with the current UTC time, then calls
`WriteExplicitMemory`.

`ForgetHandler.Handle` requires a `forget` command with a valid memory ID and
positive installation, repository and actor IDs. It calls
`DeleteExplicitMemory` with all four, so the store receives the tenant and
repository scope, and returns the store's boolean and error unchanged.

Both stores are interfaces. No implementation is in this package.

## Reaction polling

`ReactionPoller.Run` makes one pass over the comments returned by
`ReactionStore.DueReactionComments`.

| Constant | Value |
| --- | --- |
| `ReactionPollInterval` | 15 minutes |
| `ReactionPollWindow` | 14 days |
| `maxReactionPages` | 10 pages of 100 |

For each comment:

1. If the review is 14 days old or more, it calls `SaveReactionPoll` with no
   feedback, a zero next-poll time and `true` as the final argument, and makes
   no GitHub request.
2. Otherwise it reads `/pulls/comments/{id}/reactions`. Only `+1` and `-1`
   are kept, as `thumbs_up` and `thumbs_down`. Entries without a reaction ID,
   user ID or creation time are skipped.
3. It saves the feedback with the next poll time set 15 minutes ahead, or to
   the end of the window if that comes first.

`Run` does not loop or schedule itself; something else must call it. Any
error, including an invalid comment record, stops the pass, and the remaining
comments are not processed. A full tenth page of reactions is an error.

## Not implemented

- Nothing wires the package into the hosted service.
- Webhook signature verification is not in this package.
- No implementation of `scm.GitHubOperations`; the package does not import
  `internal/hosted/scm`.
- No handlers for `review`, `ignore`, `why`, `explain`, or the
  `retry_review` check action.
- No store implementations for memories or reaction polls.
- No approving or change-requesting reviews, and no `success` conclusion.
