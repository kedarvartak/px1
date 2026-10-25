# Hosted data retention

This is the proposed hosted v1 retention schedule. Deployment configuration
must enforce these limits before launch.

| Data | Default retention | Deletion trigger |
| --- | --- | --- |
| Temporary repository clones | Job duration, no more than 24 hours | Job completion or failure cleanup |
| Indexed code chunks and symbol graph | While installation has repository access, then 30 days | Repository access revoked or installation deleted |
| Review reports and findings | 90 days | Expiry or installation-owner request |
| Delivery and audit metadata | 180 days | Expiry, subject to security/legal preservation |
| Application logs | 30 days | Rolling expiry |
| Encrypted installation credentials | While installation is active | Installation deletion or token revocation |

Uninstalling the GitHub App or revoking an installation starts a tenant-wide
deletion window: all repository content, indexes, review artifacts, findings,
credentials, and ordinary logs for that installation are deleted within 30
days, even when their normal active-installation retention period is longer.

Backups must age out deleted records within 35 additional days. Deletion jobs
must be tenant-scoped, idempotent, observable, and tested. Security incidents or
legal obligations may require a documented temporary preservation hold.

The free GitHub Action runs in the user's GitHub Actions environment. Its
artifacts follow the retention settings of that repository and GitHub account.
