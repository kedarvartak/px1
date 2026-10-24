# Hosted deployment

Staging and production are separate Fly applications and must use separate
PostgreSQL, object-store, GitHub App, OTLP, and Sentry credentials. Set
`DATABASE_URL`, `PX1_OBJECT_STORE_URL`, `OTEL_EXPORTER_OTLP_ENDPOINT`, and
`SENTRY_DSN` as Fly secrets before deploying. Provider and GitHub private keys
remain secrets and must never be placed in these manifests.

Deploy staging first:

```sh
flyctl deploy --config deploy/fly/staging.toml
```

Promote the same tested image digest to production with the production config.
Each process exposes `/healthz` and `/readyz`; structured startup logs, OTLP
traces, and Sentry errors identify the environment, role, and release.
