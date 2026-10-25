# Install the hosted GitHub App

The hosted px1 service is planned for v1. It is not required to use the free,
accountless GitHub Action.

## Installation flow

1. Install the px1 GitHub App on an organization or personal account.
2. Grant access only to the repositories that px1 should review.
3. Open or update a pull request. px1 indexes the selected repository and
   publishes its review as a draft so a human can inspect it before submission.
4. Use `/px1 review` to request a fresh review or `/px1 explain` to ask for an
   explanation on the current pull request revision.

The App requests repository metadata and contents, pull-request read/write,
and checks read access. It does not merge code or submit an approval. Reviewers
remain responsible for reading the changed code and making the GitHub decision.

Repository indexing is asynchronous. Large repositories may show an indexing
status before their first review is ready. Reinstalling or changing repository
access queues a fresh index for affected repositories.

For self-hosted generation without an account, use the [free Action](../action.yml)
and follow the setup in the main README.
