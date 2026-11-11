---
title: Hosted billing
section: concepts
kind: narrative
status: built
covers:
  - internal/hosted/billing/
verified_at:
---

# Hosted billing

`internal/hosted/billing` holds the credit and payment rules for the hosted
service: a credit gate, an append-only credit ledger, a weekly margin check, an
open-source programme, and Stripe webhook and client contracts. The code and
its tests exist, but it is not deployed. See
[Hosted architecture](HOSTED_ARCHITECTURE.md) for the wider service.

The package is logic plus interfaces. Every database, Stripe API and publishing
operation is an interface (`LedgerStore`, `BalanceReader`, `SkipPublisher`,
`MarginStore`, `MarginAlerter`, `OSSStore`, `StripeEventStore`, `StripeClient`,
`PortalClient`). The repository contains no non-test implementation of any of
them; the tests use in-memory stubs. The only non-test code outside the package
that imports it is `internal/hosted/publicsite/pricing.go`, which reads
`DefaultMinimumMargin`.

The package defines no prices, plans or credit amounts. Credits are plain
`int64` counts supplied by callers.

## Files

| File | Purpose |
| --- | --- |
| `gate.go` | Decide whether a review may run on the available credits, and publish an explanation when it may not. |
| `ledger.go` | Validate and build grant, reserve, settle and refund transactions. |
| `margin.go` | Compute weekly gross margin per installation and alert when it is low. |
| `oss.go` | Accept and decide open-source programme applications. |
| `stripe.go` | Verify Stripe webhooks and validate arguments to Stripe client calls. |

## Credit gate

`CreditGate.Authorize(ctx, installationID, required)` reads the balance through
`BalanceReader.Balance` and returns a `ReviewDecision`.

| Case | `Allowed` | `SkipCode` | Error returned |
| --- | --- | --- | --- |
| Nil reader, `installationID <= 0` or `required <= 0` | false | empty | yes |
| Balance read fails | false | `billing_unavailable` | yes, wrapped as `read credit balance` |
| `available < required` | false | `credits_exhausted` | no |
| `available >= required` | true | empty | no |

The gate fails closed: if the balance cannot be read, the review is not
allowed. Every decision that passes argument validation carries a `Summary`
written for the pull request reader. The skip summaries state that no AI
review ran.

`Authorize` only reads the balance. It does not write a reservation, although
the allowed summary says credits were "reserved". Reserving is a separate call
on `Ledger`; the package has no function that performs both.

`CreditEnforcer.Enforce(ctx, installationID, reviewID, required)` wraps the
gate. It requires a non-nil `SkipPublisher` and a non-empty `reviewID`. When the
decision is allowed it returns without publishing. Otherwise it calls
`SkipPublisher.PublishSkippedReview` with the decision, including when the gate
returned an error. If both the gate and the publisher fail, the two errors are
combined with `errors.Join`; otherwise the gate error is returned in preference
to the publish error.

## Credit ledger

A `Transaction` has an `InstallationID`, an optional `ReviewID`, an
`IdempotencyKey`, a `Kind` and a signed `Credits` value that is the change to
the available balance.

| `Ledger` method | Kind | `Credits` written | Required arguments |
| --- | --- | --- | --- |
| `Grant` | `grant` | `+credits` | store, positive installation, `credits > 0`, key |
| `Reserve` | `reserve` | `-credits` | the above plus a non-empty `reviewID` |
| `Settle` | `settle` | `reserved - actualCredits` | a valid reservation, `actualCredits >= 0`, key |
| `Refund` | `refund` | `+reserved` | a valid reservation, key |

A valid reservation (`validReservation`) has a positive installation, a
non-empty `ReviewID`, kind `reserve` and negative `Credits`.

`Settle` returns the unused part of a reservation. In the test, reserving 6 and
settling at 4 returns 2. `Settle` does not cap `actualCredits` at the reserved
amount, so a higher actual cost produces a negative settle entry. `Refund`
returns the whole reservation.

`Grant`, `Settle` and `Refund` call `LedgerStore.Append`. `Reserve` calls
`LedgerStore.Reserve`, whose contract is to append the negative entry only when
the installation has at least the required credits, and to serialize that
check. Both store methods return the transaction and a boolean; the test stub
returns the earlier transaction and `false` when the idempotency key was
already used. The `Ledger` methods add no idempotency logic of their own beyond
requiring a key.

### `credit_transactions` table

Migration `0011_credit_ledger` defines the table the ledger is designed for.

| Rule | Definition |
| --- | --- |
| Idempotency | `UNIQUE (installation_id, idempotency_key)`; key length 1 to 200 |
| Review ID | Text, default empty, at most 200 characters |
| Kinds | `grant`, `reserve`, `settle`, `refund` |
| Sign | `reserve` must be negative; `grant` and `refund` must be positive; `settle` may be any value |
| One reservation per review | Unique index on `(installation_id, review_id)` where kind is `reserve` |
| One terminal entry per review | Unique index on `(installation_id, review_id)` where kind is `settle` or `refund` |
| Append-only | Trigger `credit_transactions_append_only` raises on any `UPDATE` or `DELETE` |
| Tenant isolation | Row level security is enabled and forced; policy `tenant_isolation` compares `installation_id` with the `px1.installation_id` setting |

Rows are deleted only through `ON DELETE CASCADE` from `installations`.

## Margin guardrail

`EvaluateMargin(e, minimum)` computes
`(RevenueMicros - LLMCostMicros) / RevenueMicros` for one `WeeklyEconomics`
row.

- It returns an error for a non-positive installation, a zero `WeekStart`, or
  negative revenue or cost.
- With zero revenue it returns `Available: false`, no alert, and a reason
  saying margin is unavailable.
- A `minimum` that is `<= 0` or `>= 1` is replaced by `DefaultMinimumMargin`,
  which is `0.60`.
- `Alert` is true when the margin is strictly below the minimum. The margin can
  be negative when cost exceeds revenue.

`MarginJob.Run(ctx, week)` requires a `MarginStore` and a `MarginAlerter`. It
normalizes `week` to Monday 00:00 UTC (`utcWeek`), loads rows with
`MarginStore.WeeklyEconomics`, and for each row:

1. Fails if the row's `WeekStart` is not the requested week.
2. Evaluates the margin with `MarginJob.Minimum`.
3. Calls `MarginStore.RecordMargin`, which reports whether the record was newly
   created.
4. Calls `MarginAlerter.AlertLowMargin` only when the record was newly created
   and the result is an alert.

It returns the number of alerts sent. Alert deduplication therefore depends on
the store: a re-run for a week already recorded sends nothing. The job stops at
the first error and returns the alerts sent so far.

Migration `0014_margin_guardrails` defines `weekly_billing_metrics`, keyed by
`(installation_id, week_start)`, with non-negative `revenue_micros` and
`llm_cost_micros`, a nullable `margin`, `low_margin`, `recorded_at` and
`alerted_at`. A check requires `margin` to be null exactly when revenue is
zero. Row level security is enabled and forced with the same `tenant_isolation`
policy.

`internal/hosted/publicsite/pricing.go` uses `DefaultMinimumMargin` in
`MaxStandardReviewCostMicrousd` to derive a cost ceiling from plan revenue.

## Open-source programme

`OSSProgramme` has a `Store` and an `ApprovalCredits` amount. The package sets
no default for `ApprovalCredits`.

`Apply` validates an `OSSApplication` and submits it with status `submitted`.
It requires a positive installation and repository ID, a non-empty
`ContactEmail`, a `Reason` that is not blank, and a repository URL that passes
`publicHTTPSRepository`. That function checks only the URL's form: scheme
`https`, a non-empty host, no user info and no fragment. It does not contact
the host, does not check that the repository is public, and does not require a
particular host. The email is checked only for being non-empty.

`Decide(ctx, installationID, applicationID, actor, approve, reason)` requires
all of those values, with a non-blank reason. A rejection records status
`rejected` with no credits. An approval requires `ApprovalCredits > 0` and
records status `approved` with `CreditGrant` set to that amount. It calls
`OSSStore.Decide` with `submitted` as the expected current status; the store
contract is to record the decision and any credit grant atomically. The test
stub refuses a second decision on the same application.

Migration `0013_oss_programme` defines `oss_applications`. The repository URL
must start with `https://`, the contact email is 3 to 320 characters, the
reason is 1 to 4000 characters, and `credit_grant` is non-negative. A check
ties status to the decision fields: `submitted` rows have no `decided_at` or
`decided_by`, and decided rows have both. A composite foreign key to
`repositories(installation_id, id)` ties each application to a repository in
the same installation. Row level security is enabled and forced.

## Stripe webhook

`StripeWebhook.Handle(ctx, body, signature)` verifies a webhook and dispatches
it. The checks run in this order, and each failure returns an error before any
store call.

1. `Secret` is non-empty and `Store` is non-nil.
2. The body is non-empty and at most `MaxStripeEventBytes` (`1 << 20`, 1 MiB).
3. `parseStripeSignature` reads the comma-separated header. It needs a `t`
   value that parses as a non-zero integer and at least one `v1` value. Parts
   without `=` and other keys are ignored.
4. The timestamp is within `Tolerance` of the current time in either direction.
   A `Tolerance` of zero or less means 5 minutes. `Now` replaces `time.Now`
   when set.
5. The expected signature is HMAC-SHA256 over `<timestamp>.<body>` with
   `Secret`. Each `v1` value is hex-decoded and compared with `hmac.Equal`,
   which is constant-time. One match is enough; values that are not valid hex
   are skipped.
6. The body decodes as JSON with a non-empty `id` and a non-empty
   `data.object`.

| Event type | Store call |
| --- | --- |
| `invoice.paid` | `StripeEventStore.InvoicePaid(ctx, eventID, object)` |
| `customer.subscription.updated` | `StripeEventStore.SubscriptionUpdated(ctx, eventID, object)` |
| Any other type | None; `Handle` returns nil |

The handler passes the raw `data.object` JSON through without interpreting it.
It does not map the event to an installation, grant credits or record the event
itself. Replay protection in this package is the timestamp window only;
deduplication by event ID is left to the store.

Migration `0012_stripe_billing` defines the tables for that store.
`stripe_events` has `event_id` as its primary key (1 to 200 characters), an
`installation_id`, and an `event_type` limited to the two types above.
`stripe_accounts` has one row per installation with a unique `customer_id`
matching `^cus_[A-Za-z0-9]+$`, plus `subscription_id`, `subscription_status`
(default `inactive`) and `price_id`. Both tables have row level security
enabled and forced.

## Stripe client helpers

Three functions validate arguments and then call a `StripeClient` or
`PortalClient`. Each returns an error without calling the client when
validation fails.

| Function | Validation | Client call |
| --- | --- | --- |
| `ProvisionCustomer` | Positive installation; billing email contains `@` | `EnsureCustomer` |
| `RecordMeteredReview` | Customer ID starts with `cus_`; price ID starts with `price_`; non-empty event ID; `credits > 0`; non-zero time | `ReportMeteredUsage`, with the time converted to UTC |
| `OpenCustomerPortal` | Customer ID starts with `cus_`; return URL starts with `https://` | `CreatePortalSession` |

## Not implemented

- No store, Stripe API client, skip publisher or margin alerter exists outside
  the tests.
- No HTTP handler, worker or scheduler calls this package.
- Nothing connects the gate, the ledger and metered usage reporting into one
  review flow.
- `invoice.paid` and `customer.subscription.updated` payloads are not parsed,
  and no code turns a paid invoice into a credit grant.
- No price IDs, plan definitions or credit grant amounts are defined here.
