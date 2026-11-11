---
title: SCM providers
section: concepts
kind: narrative
status: built
covers:
  - internal/hosted/scm/
  - internal/hosted/gitlab/
verified_at:
---

# SCM providers

`internal/hosted/scm` defines a provider-neutral boundary between hosted
reviews and a source-control host. `internal/hosted/gitlab` implements that
boundary for GitLab. Both packages are built: the code and tests exist, but
they are not deployed. No non-test Go code outside these two packages imports
either of them, so nothing in the hosted service calls them yet.

For the service these packages are meant to sit inside, see
[Hosted architecture](HOSTED_ARCHITECTURE.md). The GitHub delivery code is
described in [GitHub App delivery](GITHUB_APP.md).

## The `scm` package

The package holds plain data types, one interface, two validators, and a
GitHub adapter. It makes no network calls.

### Data types

| Type | Fields | Notes |
| --- | --- | --- |
| `Repository` | `InstallationID`, `Owner`, `Name` | The tenant and repository a call acts on |
| `PullRequest` | `Number`, `BaseSHA`, `HeadSHA` | Also used for GitLab merge requests |
| `File` | `Path`, `Status`, `Additions`, `Deletions` | One changed file |
| `Diff` | `BaseSHA`, `HeadSHA`, `Patch` | `Patch` is raw bytes |
| `Comment` | `ID`, `Body`, `Path`, `Line`, `CreatedAt` | `ID` is a string |
| `Thread` | `ID`, `Resolved`, `Comments` | A review discussion |
| `CheckStatus` | `Name`, `State`, `Summary`, `DetailsURL` | A status to show on the head commit |
| `Webhook` | `DeliveryID`, `Event`, `Body` | A verified webhook delivery |

### The `Provider` interface

| Method | Purpose |
| --- | --- |
| `Diff` | Return the patch for a pull request |
| `Files` | List the changed files |
| `UpsertSummary` | Create or update one summary comment, identified by a marker string |
| `Threads` | List review threads and whether each is resolved |
| `SetStatus` | Publish a `CheckStatus` for the pull request |
| `VerifyWebhook` | Check a webhook body against a delivery ID and a signature or token, and return a `Webhook` |

The interface has no method for posting inline review comments.

### Validation

`ValidateRepository` requires `InstallationID` greater than zero and a
non-empty `Owner` and `Name`. It does not restrict which characters the owner
and name may contain.

`ValidatePullRequest` requires `Number` greater than zero, and a `BaseSHA` and
`HeadSHA` that are each exactly 40 hexadecimal characters (upper or lower
case) and differ from each other. Short SHAs and branch names are rejected.

### `GitHubProvider`

`GitHubProvider` wraps a `GitHubOperations` value. `GitHubOperations` has the
same six methods as `Provider`. The code comment says it is implemented by the
GitHub App delivery layer and is kept separate so the adapter can be
contract-tested without GitHub calls. No type in the repository implements
`GitHubOperations` outside the test file, where a fake records calls.

Each `GitHubProvider` method validates its input and then delegates:

| Method | Checks before delegating |
| --- | --- |
| `Diff`, `Files`, `Threads` | `Operations` is set, `ValidateRepository`, `ValidatePullRequest` |
| `UpsertSummary` | The same, plus a non-empty marker and body |
| `SetStatus` | The same, plus a non-empty `status.Name` and `status.State` |
| `VerifyWebhook` | `Operations` is set, the body reader is not nil, delivery and signature are non-empty |

When a check fails, the wrapped operation is not called. A test confirms that a
repository with no installation ID never reaches `Operations`.

`UpsertSummary` returns one fixed error message for every validation failure,
so the caller cannot tell a bad repository from an empty marker.
`GitHubProvider.VerifyWebhook` does no signature checking itself; it only
rejects empty inputs and passes the rest to `Operations`.

## The `gitlab` package

`gitlab.Provider` implements `scm.Provider` against the GitLab REST API.

| Field | Meaning | Default when empty |
| --- | --- | --- |
| `HTTP` | The `*http.Client` used for requests | A new client with a 10 second timeout |
| `Tokens` | A `TokenSource` that returns an access token for an installation ID | None; requests fail without it |
| `BaseURL` | API base URL, trailing slashes removed | `https://gitlab.com/api/v4` |
| `WebhookToken` | The shared secret webhooks must present | None; every webhook is rejected |

`TokenSource` has one method, `Token(ctx, installationID)`, which returns a
plain string. The package contains no implementation of it, so the code does
not show where GitLab tokens come from or how they are stored.

### How a request is sent

Every API call goes through the unexported `request` method:

1. Fail if `Tokens` is nil.
2. Ask `Tokens` for a token using `Repository.InstallationID`. An error or an
   empty token becomes `GitLab access token is unavailable`; the underlying
   error is not included.
3. Build the URL as base URL plus path. The project is addressed as
   `/projects/` followed by the URL-escaped `Owner/Name`, so the slash is sent
   as `%2F`.
4. Send the token in the `PRIVATE-TOKEN` header. A JSON body, when present, is
   sent with `Content-Type: application/json`.
5. A status outside 200 to 299 returns `GitLab API returned status N`. The
   response body is drained and never included in the error.
6. A successful body is decoded as JSON through a reader limited to
   `maxResponseBytes`.

There are no retries and no rate-limit handling.

### Limits

| Limit | Value | Where |
| --- | --- | --- |
| `maxResponseBytes` | 20 MiB (`20 << 20`) | API response bodies and webhook bodies |
| Default HTTP timeout | 10 seconds | Only when `HTTP` is nil |
| Diff page size | 100 | `per_page=100` on the diffs endpoint |
| Diff pages | 10 | A full tenth page is an error |

### `Diff` and `Files`

Both call the unexported `changes` method, which validates the repository and
pull request and then reads
`/merge_requests/{number}/diffs?per_page=100&page=N`.

Paging stops at the first page with fewer than 100 entries. If ten pages all
return 100 entries, `changes` fails with `GitLab merge request diff exceeded
the 1000-file retrieval limit`. A merge request with exactly 1000 changed
files therefore fails too.

If any entry has `collapsed` or `too_large` set, `changes` fails with an error
saying the review cannot continue with incomplete evidence. It does not return
a partial diff.

`Diff` joins the entries into one patch. Each entry becomes a
`diff --git a/{old_path} b/{new_path}` line followed by GitLab's diff text.
The returned `BaseSHA` and `HeadSHA` are copied from the input. The SHAs are
validated but are not sent to GitLab, so the patch is whatever the diffs
endpoint returns for the merge request at the time of the call.

`Files` returns one `scm.File` per entry with `Path` set to the new path.
`Additions` and `Deletions` are left at zero. `Status` is chosen in this
order, later rules overriding earlier ones:

| GitLab flag | `Status` |
| --- | --- |
| none | `modified` |
| `new_file` | `added` |
| `deleted_file` | `deleted` |
| `renamed_file` | `renamed` |

Because `Diff` and `Files` each call `changes`, calling both fetches the diff
twice.

### `UpsertSummary`

The body must contain the marker, and the marker must be non-empty. The method
then lists the merge request notes with one `GET .../notes` request and looks
for the first note whose body contains the marker.

- If one is found, it sends `PUT .../notes/{id}` with the new body.
- Otherwise it sends `POST .../notes`.

The returned `scm.Comment` carries the note ID, body, and creation time.

Two behaviours are worth knowing. The note list is a single request with no
paging parameters, so only the notes GitLab returns in that one response are
searched. And the match is on the marker text alone: the author of the note is
not checked, unlike the GitHub sticky summary, which only trusts a comment
owned by the expected bot.

### `Threads`

`Threads` reads `GET .../discussions` in a single request, again without
paging. Each discussion becomes an `scm.Thread`. A thread is `Resolved` only
when every note in it has `resolved` set; a discussion with no notes is
reported as resolved. Comments carry the note ID, body, and creation time.
`Path` and `Line` are not filled in.

### `SetStatus`

`SetStatus` posts to `/statuses/{HeadSHA}` with `state`, `name`,
`description` (from `Summary`), and `target_url` (from `DetailsURL`). The
state is translated by `mapStatus`:

| Input state | GitLab state |
| --- | --- |
| `pending`, `running`, `success`, `failed`, `canceled`, `skipped` | unchanged |
| `in_progress` | `running` |
| `completed`, `neutral` | `success` |
| `failure` | `failed` |
| anything else | error `unsupported GitLab status state` |

Note that `neutral` and `completed` both become `success`.

### `VerifyWebhook`

GitLab webhooks are authenticated with a shared token, not an HMAC signature.
`VerifyWebhook(body, delivery, token)` rejects the request unless all of these
hold:

- `body` is not nil and `delivery` is non-empty.
- `Provider.WebhookToken` is non-empty. An unconfigured provider accepts
  nothing.
- The supplied token has the same length as `WebhookToken` and matches it
  under `subtle.ConstantTimeCompare`.

Only then is the body read, through a reader capped at `maxResponseBytes`
plus one byte. A body over 20 MiB, a read error, or invalid JSON is rejected.
The JSON must have a non-empty `object_kind`, which becomes `Webhook.Event`.
`DeliveryID` is the caller's `delivery` value unchanged.

The method takes the token and delivery ID as arguments. The code does not
show which HTTP headers a caller should read them from, and it does not
deduplicate deliveries.

### `MapDiffPosition`

`MapDiffPosition(oldPath, newPath, oldLine, newLine)` returns a `DiffPosition`
with a `LineCode` of the form `{sha1 of newPath in hex}_{oldLine}_{newLine}`.
It requires both paths, non-negative lines, and at least one positive line.
No `Provider` method uses it.

## Error handling

Errors are plain `errors.New` or `fmt.Errorf` values with fixed messages. There
are no exported error variables or types, so callers cannot branch on the
cause except by message. Validation errors are returned before any network
call. API errors carry only the HTTP status code.

## Not implemented

- Nothing wires either package into the hosted service.
- No concrete `GitHubOperations` exists, so `GitHubProvider` cannot run
  against GitHub as written.
- No GitLab `TokenSource` implementation exists.
- No inline review comments on GitLab. `MapDiffPosition` builds a line code
  but nothing posts with it.
- No paging for GitLab notes or discussions.
- No retries, backoff, or rate-limit handling in the GitLab client.
- `File.Additions`, `File.Deletions`, `Comment.Path`, and `Comment.Line` are
  never set by the GitLab provider.
