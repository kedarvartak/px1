# GitHub App registration

px1 uses separate GitHub Apps for development and production. The checked-in
manifests are the reviewed permission and webhook contract:

- [`deploy/github-app/dev.json`](../deploy/github-app/dev.json)
- [`deploy/github-app/prod.json`](../deploy/github-app/prod.json)

Create each App through GitHub's App-manifest flow. Do not commit the generated
App ID, client secret, webhook secret, or private key.

## Secrets contract

Store the generated values in the deployment's secrets manager under distinct
development and production namespaces:

| Name | Purpose |
| --- | --- |
| `PX1_GITHUB_APP_ID` | Numeric App identity used to mint App JWTs. |
| `PX1_GITHUB_CLIENT_ID` | OAuth client identity for authenticated reports. |
| `PX1_GITHUB_CLIENT_SECRET` | OAuth callback credential. |
| `PX1_GITHUB_WEBHOOK_SECRET` | HMAC-SHA256 webhook verification key. |
| `PX1_GITHUB_PRIVATE_KEY` | PEM private key used to mint installation tokens. |

Grant runtime access only to the process that needs each value. Rotate the
webhook secret and private key independently, allowing an overlap window for
in-flight deliveries. Secret values must never be logged or copied into GitHub
Actions artifacts.

## Event policy

The App subscribes only to installation lifecycle, repository selection,
pull-request, review-comment, issue-comment, and default-branch push events.
The webhook handler still rejects unsupported actions after signature
verification. Push events for non-default branches must not trigger indexing.

## Registration checklist

1. Review the manifest diff and run `go test ./...`.
2. Create the development App and store all returned credentials in the
   development secrets namespace.
3. Confirm the development webhook receives a signed `ping` delivery.
4. Repeat for production only after staging validates signature verification,
   deduplication, installation-token minting, and repository isolation.
5. Record App owners and rotation dates in the secrets manager, not this repo.
