---
title: Issue tracker and Slack integrations
section: concepts
kind: narrative
status: built
covers:
  - internal/hosted/integrations/issues/
  - internal/hosted/integrations/slack/
verified_at:
---

# Issue tracker and Slack integrations

Two packages under `internal/hosted/integrations` connect the hosted service
to outside tools. `issues` reads one Linear or Jira issue with a tenant's
OAuth token. `slack` verifies Slack requests, completes the Slack OAuth
install, builds a review message and parses the `/px1 status` command.

Status: built. The code and its tests exist, but the hosted service is not
deployed (see [Hosted architecture](HOSTED_ARCHITECTURE.md)). Both packages
are libraries: neither contains an HTTP handler, a token store or an OAuth
code exchange. Those are interfaces the caller must supply, and no
implementation of them exists elsewhere in the repository.

## Issue trackers (`issues`)

### Types

| Identifier | Purpose |
| --- | --- |
| `Provider` | `Linear` (`"linear"`) or `Jira` (`"jira"`) |
| `Issue` | `Provider`, `Key`, `Title`, `Description`, `AcceptanceCriteria`, `URL` |
| `Tracker` | Interface with `FetchIssue(ctx, installationID, key)`; both clients satisfy it |
| `TokenSource` | Interface with `OAuthToken(ctx, installationID, provider)`; returns the bearer token for one tenant |
| `OAuthGrant` | `Provider`, `AccountID`, `BaseURL`, `AccessToken`, `RefreshToken`, `ExpiresAt` |
| `OAuthExchanger` | Interface with `ExchangeOAuthCode(ctx, provider, code, redirectURI)` |
| `ConnectionStore` | Interface with `SaveOAuthConnection(ctx, installationID, grant)` |

The only caller in the repository is the agentic review toolbox: the
`read_issue` tool holds an `issues.Tracker` in `Toolbox.Issues` and fetches
only keys listed in the review scope. See [Agentic review tools](AGENT_TOOLS.md).

### Connecting a tracker

`CompleteOAuth` finishes an OAuth connection. The caller passes the
installation ID and provider; the function's comment says these must come from
the already-verified dashboard session, not from callback query parameters.

It rejects the call before any network request unless all of these hold:

- the exchanger and store are non-nil;
- `installationID` is greater than zero;
- `provider` is `Linear` or `Jira`;
- `code` is non-empty;
- `redirectURI` starts with `https://`.

It then calls `ExchangeOAuthCode`. The grant is rejected if its `Provider`
differs from the requested one, or if `AccountID` or `AccessToken` is empty.
A valid grant is passed to `SaveOAuthConnection`. Token refresh is not
implemented in this package; `RefreshToken` and `ExpiresAt` are only carried.

### Finding issue keys

`DetectIssueKeys(branch, pullBody)` upper-cases both strings and collects
matches of `issueKeyPattern`:

```
\b[A-Z][A-Z0-9]{1,9}-[1-9][0-9]*\b
```

That is a project prefix of 2 to 10 characters starting with a letter, a
hyphen, and a number with no leading zero. The branch is scanned first, then
the body. Duplicates are dropped, collection stops at 10 keys, and the result
is sorted. In the test, branch `feature/eng-123` and body
`Implements OPS-9 and ENG-123` give `ENG-123`, `OPS-9`.

### Extracting acceptance criteria

`ExtractAcceptanceCriteria(description)` reads the description line by line:

- A line that equals `acceptance criteria` or `acceptance criterion`, after
  trimming `#`, `*`, `:` and spaces and lower-casing, starts the section.
- Inside the section, a line starting with `#` ends the scan.
- Checkbox lines (`- [ ] `, `- [x] `, `- [X] `) are collected wherever they
  appear before the scan ends, including before the section heading.
- Plain bullets (`- ` or `* `) are collected only inside the section.
- The scan stops at 20 criteria.

### Fetching an issue

Both clients have the fields `HTTP`, `Tokens` and `BaseURL`.

| Client | Request | Checks |
| --- | --- | --- |
| `LinearClient` | `POST` of a GraphQL query `issue(id: $id)` asking for `identifier`, `title`, `description`, `url`. Endpoint is `BaseURL`, or `https://api.linear.app/graphql` when empty | Key must match `issueKeyPattern`. A non-empty `errors` array or an empty `identifier` returns `Linear issue was not found` |
| `JiraClient` | `GET` of `BaseURL` + `/rest/api/3/issue/KEY?fields=summary,description`, with the key path-escaped | Key must match `issueKeyPattern` and `BaseURL` must start with `https://` |

The key check uses `MatchString`, which is not anchored: it passes any string
that contains a valid key.

Jira descriptions arrive as a nested document. `flattenText` walks it: strings
are kept, arrays are walked, and for an object it follows `text` if present,
otherwise `content`. Non-blank strings are joined with newlines. The Jira
issue URL is built as `BaseURL` + `/browse/KEY`. The Jira client does not check
that the returned key is non-empty.

Both clients run `ExtractAcceptanceCriteria` on the description and return the
result in `Issue.AcceptanceCriteria`.

### Shared request rules

The unexported `request` function sends every call:

- It fails with `OAuth token source and tenant are required` if `Tokens` is
  nil or the installation ID is not positive.
- It asks `TokenSource.OAuthToken` for the token. An error or an empty token
  becomes `OAuth token is unavailable`; the underlying error is not returned.
- It sets `Authorization: Bearer TOKEN` and `Accept: application/json`, plus
  `Content-Type: application/json` when there is a body.
- If `HTTP` is nil it uses a client with a 10 second timeout.
- A status outside 200 to 299 returns `PROVIDER returned status N`. The body
  is drained, not returned.
- The response is read through a limit of `maxIssueResponse`, 2 MiB
  (`2 << 20` bytes).

## Slack (`slack`)

### Verifying a Slack request

`VerifyRequest(secret, timestamp, signature, body, now)` checks the signature
Slack puts on each request. It returns `Slack request signature is invalid`
when the secret is empty, the body is larger than `MaxSlackBodyBytes`
(1 MiB, `1 << 20` bytes), or the signature does not start with `v0=`.

The timestamp must parse as a Unix second count and be within 5 minutes of
`now` in either direction; otherwise the error is
`Slack request timestamp is outside tolerance`.

The expected value is HMAC-SHA256 over `v0:TIMESTAMP:BODY` with the signing
secret. The supplied signature is hex-decoded and compared with `hmac.Equal`,
a constant-time comparison. The tests cover a changed body and a request
replayed 10 minutes later; both are rejected.

### OAuth state

`OAuthState` carries `InstallationID`, `Nonce` and `ExpiresAt` (Unix seconds).

`SignOAuthState` requires a positive installation ID, a non-empty nonce, a
positive expiry and a non-empty secret. The output is the base64url JSON
payload, a dot, and the base64url HMAC-SHA256 of that encoded payload. The
caller chooses the expiry; the package sets no maximum lifetime.

`VerifyOAuthState` splits on the first dot, checks the HMAC with `hmac.Equal`
before decoding the payload, then rejects the state if the JSON is invalid,
the installation ID is not positive, the nonce is empty, or `now` is later
than `ExpiresAt`.

### Completing the install

`CompleteOAuth(ctx, client, store, code, signedState, stateSecret, now)`:

1. Requires a non-nil `OAuthClient`, a non-nil `InstallationStore` and a
   non-empty code.
2. Verifies the signed state. The tenant comes from the state, not from the
   request.
3. Calls `OAuthClient.ExchangeOAuthV2(ctx, code)`.
4. Rejects a grant with an empty `TeamID` or `BotToken`.
5. Rejects a grant whose `Scopes` lack `chat:write` or `commands`.
6. Calls `SaveSlackInstallation` with the state's installation ID, the grant
   and the state's nonce.

The package does not track used nonces. It passes the nonce to the store; what
the store does with it is not shown in this code.

### Review message

`ReviewMessage(repository, pull, p0, summary, reviewURL)` returns a `Message`
with fallback `Text` and three `Blocks`: a `header` block with the heading as
`plain_text`, a `section` with the summary as `mrkdwn`, and a `section` with a
link to `reviewURL` labelled `Open commit-pinned review`.

| Input | Rule |
| --- | --- |
| `repository` | Non-empty, at most 200 bytes |
| `pull` | Greater than zero |
| `p0` | Zero or more |
| `summary` | Non-empty, at most 2800 bytes |
| `reviewURL` | Starts with `https://` |

The heading is `px1 review: REPO !N`. When `p0` is greater than zero it
becomes an alert emoji followed by `N P0 finding(s): REPO !N`. The function
does not escape the summary or the URL.

### Status command

`ParseStatusCommand(body)` parses a form-encoded slash command body into a
`StatusCommand` (`ResponseURL`, `Repository`, `PullNumber`).

- The body must be non-empty and at most `MaxSlackBodyBytes`.
- `command` must be exactly `/px1`.
- `text` must be exactly three fields: `status`, a repository containing `/`,
  and a positive pull request number with an optional leading `#`.
- `response_url` must start with `https://hooks.slack.com/`.

Usage errors return `usage: /px1 status owner/repo PR`. This function does not
verify the signature; the caller must run `VerifyRequest` first.

## Not implemented

- No HTTP routes or handlers for OAuth callbacks, Slack commands or events.
- No implementation of `TokenSource`, `OAuthExchanger`, `ConnectionStore`,
  `OAuthClient` or `InstallationStore`.
- No code that sends a `Message` to Slack or replies to a `ResponseURL`.
- No token refresh, and no write access to Linear or Jira.
- `slack` is not imported by any other package in the repository.
