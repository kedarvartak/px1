import { formatCreditPrice, pricingPlans } from "../../../lib/pricing";
import styles from "../public.module.css";

export const metadata = { title: "px1 pricing" };

export default function Pricing() {
  return (
    <main className={styles.page}>
      <h1>Pricing</h1>
      <p>One standard review costs one credit. Credits are shared across an organization, and there are no seat fees.</p>
      <div className={styles.grid}>
        {pricingPlans.map((plan) => (
          <article className={styles.card} key={plan.slug}>
            <h2>{plan.name}</h2>
            <p className={styles.note}>{plan.audience}</p>
            <p className={styles.price}>{formatCreditPrice(plan)}</p>
            <ul>
              {plan.includes.map((item) => <li key={item}>{item}</li>)}
            </ul>
          </article>
        ))}
      </div>
      <p className={styles.note}>
        The self-run GitHub Action stays free and needs no px1 account. A review skipped for missing credits is reported on the pull request; it never fails silently.
      </p>
    </main>
  );
}
