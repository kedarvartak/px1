# Public pages

The dashboard serves four pages without a session, all under `/-/`:

| Page | Source of truth | Gate |
| --- | --- | --- |
| `/-/pricing` | `internal/hosted/publicsite.Plans` | `ValidatePlans`; a test keeps `web/dashboard/lib/pricing.json` identical |
| `/-/docs` | Files in `docs/` at the deployed commit | Links only; no copied prose |
| `/-/changelog` | Repository `CHANGELOG.md` | `ParseChangelog`: dated sections newest first, one merged-PR link per entry |
| `/-/status` | `/status.json` on the `web` role | Readiness probes only |

## Why `/-/`

Dashboard routes start with the organization login (`/:organization/...`).
GitHub and GitLab both reject logins that begin with `-`, so `/-/` can never
shadow a tenant's dashboard. A bare `/pricing` would hide the dashboard of a
real organization named `pricing`.

## Pricing

One standard review costs one credit. Starter includes 50 credits a month for
one developer; Pro is $0.80 per credit, shared across the organization, with no
seat fees; approved open-source repositories are free; Enterprise is an annual
contract. Review tiers are not priced until they ship.

`MaxStandardReviewCostMicrousd` derives the model-cost ceiling that keeps a Pro
review at or above the 60% margin guardrail: $0.32. The v1 cost target of
$0.40 per median review is above that ceiling, so it would trigger margin
alerts at list price.

## Status

The status response contains component names, `operational` or `unavailable`,
and the check time. It never includes error text, latency, hosts, or tenant
data. A component is operational only when its readiness endpoint answers 2xx
within two seconds without redirects. If the dashboard cannot reach
`PX1_STATUS_URL`, the page says status could not be checked and makes no
claim about health.

## Changelog

Add one line per merged pull request under its UTC merge date:

```markdown
## 2026-10-29

- Add auditable requirement coverage ([#221](https://github.com/kedarvartak/px1/pull/221))
```

Free-form lines inside a dated section fail the test, so the published page
cannot drift from the file.
