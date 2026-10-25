# Free Action compatibility contract

The released px1 GitHub Action remains an accountless, self-run product mode.
Installing the hosted GitHub App is optional.

The Action receives explicit base and head revisions, reads the caller's
checkout, and generates the review inside the caller's GitHub Actions runner.
It must not require a px1 account, px1-issued token, hosted px1 API, or payment.
Repositories control artifact delivery, retention, optional model credentials,
and any deliberate public publication.

The hosted product may add managed indexing, delivery, and collaboration, but
must not silently turn those services into Action prerequisites. CI protects
this boundary by rejecting known hosted-account and backend dependencies in
`action.yml`. A future intentional compatibility change requires an explicit
major-version decision and documentation before it is merged.
