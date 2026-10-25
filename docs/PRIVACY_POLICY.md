# Privacy policy

This document describes the intended hosted px1 v1 data practices. It is a
launch draft and must be completed with the operating legal entity, contact
address, subprocessors, and effective date before public signup is enabled.

## Data px1 processes

When an authorized GitHub App installation requests a review, px1 processes
repository metadata, source code needed for indexing, pull-request diffs,
configured rules, review findings, and delivery metadata. Authentication tokens
are encrypted at rest and are not included in model prompts.

Optional AI analysis sends the minimum relevant code context to the configured
model provider after secret and sensitive-data checks. The product must identify
the provider and its retention terms before enabling this feature in production.

## Access and isolation

Access is limited to repositories selected during GitHub App installation.
Stored repository data is associated with an installation tenant, and service
queries enforce that tenant boundary. px1 employees and operators access private
repository data only for documented support, security, or reliability needs.

## Retention and deletion

Repository clones and derived index data are removed after installation access
is revoked or after the documented retention period. Uninstalling the App starts
a 30-day deletion deadline for all hosted data associated with that installation,
including review artifacts and findings. Logs and backups age out under the
periods in [DATA_RETENTION.md](DATA_RETENTION.md). An installation owner may
request deletion; production contact details must be published before launch.

px1 does not sell repository code or use private repository code to train a
general-purpose model.
