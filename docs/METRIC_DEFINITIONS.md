# Metric definitions

The customer-visible definitions live in
`web/dashboard/lib/metric-definitions.ts` and are rendered at
`/:organization/analytics/definitions`. Each definition states its numerator,
denominator, and exclusions. A metric with no eligible denominator is absent;
it is never reported as zero. This keeps claims such as “bugs caught” auditable
and prevents missing telemetry from looking like poor performance.
