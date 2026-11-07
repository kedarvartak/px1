import plans from "./pricing.json";

// pricing.json is generated from internal/hosted/publicsite.Plans; a Go test fails
// when the two drift, so edit the Go table and copy its JSON here.
export type Plan = (typeof plans)[number];

export const pricingPlans: Plan[] = plans;

export function formatCreditPrice(plan: Plan): string {
  if (plan.contactSales) return "Contact us";
  if (plan.microusdPerCredit === 0) return "Free";
  return `$${(plan.microusdPerCredit / 1_000_000).toFixed(2)} per credit`;
}
