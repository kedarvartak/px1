# Hosted v1 architecture decision

Status: accepted for the hosted v1 foundation.

px1's hosted product is a GitHub App that indexes repositories and reviews pull
requests while retaining the existing commit-pinned report and file-by-file
human review workflow. The free GitHub Action remains a separate self-run mode.

## Stack

| Layer | Decision | Boundary |
| --- | --- | --- |
| API and workers | Go | Reuse diff, rules, evidence, and report code. |
| Database | PostgreSQL 16 with pgvector | Tenant-scoped metadata, chunks, findings, and feedback. |
| Queue | River on PostgreSQL | Transactional enqueue without another broker. |
| Object storage | S3-compatible API | Commit-pinned reports and bounded shallow clones. |
| Repository parsing | tree-sitter, with line-window fallback | Parse data only; never execute pull-request code. |
| Observability | OpenTelemetry traces plus structured logs | Correlate webhook, index, review, and LLM calls. |

## Service boundary

The first deployment has one Go codebase with independently scalable process
roles:

- `web`: health, OAuth callbacks, authenticated report access, and API traffic;
- `webhook`: signature verification, delivery deduplication, and enqueue only;
- `indexer`: default-branch cloning, parsing, chunking, and embedding;
- `reviewer`: pull-request context assembly, deterministic analysis, model
  findings, verification, GitHub comments, and report generation;
- `learner`: feedback ingestion and later memory/rule-proposal jobs.

All work flows through PostgreSQL transactions and River jobs. Workers use
S3-compatible storage through an interface so development, cloud, and future
self-hosted deployments do not depend on one vendor.

## Security invariants

1. Every hosted row carries an installation or organization tenant key.
2. Webhooks are authenticated before parsing or enqueueing.
3. Installation tokens and model credentials never enter logs or reports.
4. Pull-request heads are fetched as data and are never executed.
5. Reports remain tied to an exact base and full head SHA.
6. Findings expose line evidence and verifier status; approval stays in GitHub.

## Initial operating limits

- 50,000 files or 2 GB of source per repository;
- webhook acknowledgement target under one second;
- review context capped before model transfer;
- shallow, filtered clones in isolated temporary directories;
- separate development and production GitHub Apps and secrets.

These are product limits, not claims that the hosted service is already
deployed. Each subsequent roadmap PR adds one tested boundary.
