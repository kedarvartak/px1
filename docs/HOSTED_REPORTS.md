---
title: Hosted report publishing and access
section: concepts
kind: narrative
status: built
covers:
  - internal/hosted/report/
verified_at:
---

# Hosted report publishing and access

The `internal/hosted/report` package does two jobs for the hosted service. It
publishes a static review bundle for one commit to object storage
(`publish.go`), and it provides the pieces needed to decide whether a signed-in
GitHub user may read a repository's report (`access.go`).

Status: built. The code and its tests exist, but the hosted service is not
deployed (see [Hosted architecture](HOSTED_ARCHITECTURE.md)). No other package
in the repository imports this one, and it contains no HTTP handler and no
object store implementation.

## Publishing a bundle

### Types

| Identifier | Purpose |
| --- | --- |
| `ExportRequest` | `Root` (repository checkout), `BaseSHA`, `HeadSHA` |
| `Exporter` | Interface with `Export(ctx, request, outputDir)`; writes the bundle into a directory |
| `CLIExporter` | `Exporter` that runs the px1 binary |
| `Object` | One upload: `Key`, `ContentType`, `SHA256`, `Body`, `Size`, `Immutable` |
| `ObjectStore` | Interface with `Put(ctx, object)` |
| `Publisher` | Holds an `Exporter`, an `ObjectStore` and `MaxBundleBytes` |

### Flow of `Publisher.Publish`

`Publish(ctx, owner, repo, request)` returns the list of object keys it
uploaded.

1. Validate the inputs (see below). On failure it returns
   `exporter, store, repository, root, and full SHAs are required`.
2. Create a temporary directory named `px1-report-*`. It is removed when the
   function returns.
3. Call `Exporter.Export` to write the bundle into that directory.
4. For each of the three required files, in order: check it, add its size to
   the running total, read it and compute its SHA-256.
5. Upload the three objects with `ObjectStore.Put`, in the same order.

All three files are read and checked before the first upload starts. If a
`Put` fails, the error is returned as `upload FILE: ...`; objects already
uploaded in that call are not removed.

### Bundle contents

| File | Content type |
| --- | --- |
| `index.html` | `text/html; charset=utf-8` |
| `review.json` | `application/json` |
| `checksums.sha256` | `text/plain; charset=utf-8` |

Each file is checked with `os.Lstat` and must be a regular file. A missing
file, a directory or a symbolic link fails with
`report output FILE is missing or not regular`. The test
`TestPublisherRequiresCompleteBundle` covers an export that writes only
`index.html`.

### Object keys and metadata

Each key has the form:

```
reports/OWNER/REPO/HEADSHA/FILE
```

`HeadSHA` is used as given; it is not lower-cased. Every object is created
with `Immutable: true`, `SHA256` set to the lower-case hex digest of the body,
and `Size` set to the body length. How a store enforces `Immutable` is up to
the `ObjectStore` implementation; none exists in the repository.

### Input validation and limits

| Check | Rule |
| --- | --- |
| `owner`, `repo` | `validObjectSlug`: non-empty, not `.` or `..`, only ASCII letters, digits, `-`, `_` and `.` |
| `BaseSHA`, `HeadSHA` | `fullSHA`: exactly 40 hexadecimal characters, either case |
| `Root` | Non-empty |
| `Exporter`, `Store` | Non-nil |
| Bundle size | Sum of the three file sizes must not exceed the limit |

The size limit is `Publisher.MaxBundleBytes` when it is greater than zero.
Otherwise it is `maxBundleBytes`, 50 MiB (`50 << 20` bytes). Exceeding it
returns `report bundle exceeds N bytes`. Because the slug rule excludes `/`
and the SHA rule excludes everything but hex digits, no caller-supplied value
can add a path segment to an object key.

### `CLIExporter`

`CLIExporter.Export` runs:

```
BINARY export-review --root ROOT --base BASESHA --head HEADSHA --out OUTPUTDIR
```

`BINARY` is the `Binary` field, or the running executable (`os.Executable`)
when the field is empty. The type's comment states the intent: it invokes the
trusted px1 executable, not repository code. Standard output and standard
error are discarded. A failure is wrapped as `export static report: ...`.
The `export-review` command is described in the
[CLI reference](CLI_REFERENCE.md).

## Report access

`access.go` contains a GitHub OAuth web flow (`OAuthFlow`) and a repository
read check (`AccessAuthorizer`).

### `OAuthFlow`

Fields: `ClientID`, `ClientSecret`, `CallbackURL`, `StateKey`, `HTTP`,
`AuthorizeURL`, `TokenURL` and `Now` (a clock override used by tests).

`Begin(redirect, sessionBinding)` returns the URL to send the browser to. It
fails unless:

- `ClientID` is non-empty;
- `CallbackURL` parses with scheme `http` or `https` and a host;
- `StateKey` is at least 32 bytes;
- `redirect` passes `safeRedirect`: starts with `/`, does not start with `//`,
  and contains no carriage return or line feed;
- `sessionBinding` is at least 16 bytes.

The state payload is JSON with four fields: `redirect`, `expires` (10 minutes
from now, Unix seconds), `nonce` (16 random bytes from `crypto/rand`,
base64url) and `binding` (base64url SHA-256 of `sessionBinding`). The state
string is the base64url payload, a dot, and the base64url HMAC-SHA256 of the
payload under `StateKey`.

The authorize URL is `AuthorizeURL`, or
`https://github.com/login/oauth/authorize` when empty, with query parameters
`client_id`, `redirect_uri`, `scope` set to `read:user repo`, and `state`.

`ValidateState(state, sessionBinding)` returns the stored redirect path. It
returns `invalid OAuth state` when:

- the state is not exactly two dot-separated parts;
- either part is not valid base64url;
- the HMAC does not match (compared with `hmac.Equal`);
- the payload is not valid JSON, the nonce is empty, or the redirect fails
  `safeRedirect`;
- the binding does not equal the hash of the supplied `sessionBinding`.

It returns `expired OAuth state` when the current time is later than
`expires`. `ValidateState` does not re-check the length of `StateKey`; only
`Begin` does. The package keeps no record of used nonces.

The test `TestOAuthStateIsSignedScopedAndExpires` shows a tampered state, a
state presented with a different session binding, and a state checked 11
minutes later all being rejected.

`Exchange(ctx, code)` trades the code for a GitHub access token. It requires
`ClientID`, `ClientSecret` and a non-blank code. It sends a JSON `POST` with
`client_id`, `client_secret`, `code` and `redirect_uri` to `TokenURL`, or
`https://github.com/login/oauth/access_token` when empty. The response must
have status 200 and a non-empty `access_token`; the body is read through a
1 MiB limit. Any other outcome returns
`GitHub OAuth exchange failed: status N`. The token is returned to the caller;
this package does not store it.

### `AccessAuthorizer`

`CanRead(ctx, oauthToken, owner, repo)` asks GitHub whether the user's token
can see the repository. It requires a non-empty token and an owner and repo
that pass `slug` (the same character rule as `validObjectSlug`). It sends
`GET BASE/repos/OWNER/REPO` with `Authorization: Bearer TOKEN` and
`Accept: application/vnd.github+json`. `BASE` is `BaseURL`, or
`https://api.github.com` when empty.

| GitHub status | Result |
| --- | --- |
| 200 | `true`, no error |
| 403 or 404 | `false`, no error |
| Anything else | `false` and `check GitHub repository access: status N` |

Nothing is cached: each call makes one GitHub request.

### HTTP clients

`Exchange` and `CanRead` use the `HTTP` field when set and
`http.DefaultClient` otherwise. The default client has no timeout, so a
deadline must come from the context or from a supplied client.

## Not implemented

- No HTTP routes: nothing here serves a report, sets a session cookie or
  handles the OAuth callback.
- No `ObjectStore` implementation, and no code that reads a published bundle
  back.
- No storage or refresh of the GitHub access token.
- No link between `Publish` and `CanRead`; a caller must combine them.
- No deletion or expiry of published objects.
