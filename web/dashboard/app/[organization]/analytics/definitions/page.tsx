import Link from "next/link";
import { metricDefinitions } from "../../../../lib/metric-definitions";
import styles from "./definitions.module.css";

type MetricDefinitionsProps = { params: Promise<{ organization: string }> };

export default async function MetricDefinitions({ params }: MetricDefinitionsProps) {
  const { organization } = await params;
  return (
    <main>
      <p>{organization}</p>
      <h1>Metric definitions</h1>
      <p>Every metric names its numerator, denominator, and exclusions. Empty denominators produce no value, not zero.</p>
      <Link href={`/${organization}`}>Back to overview</Link>
      <div className={styles.definitions}>
        {metricDefinitions.map((metric) => (
          <article id={metric.slug} key={metric.slug}>
            <h2>{metric.name}</h2>
            <p>{metric.definition}</p>
            <dl>
              <dt>Numerator</dt><dd>{metric.numerator}</dd>
              <dt>Denominator</dt><dd>{metric.denominator}</dd>
              <dt>Exclusions</dt><dd>{metric.exclusions}</dd>
            </dl>
          </article>
        ))}
      </div>
    </main>
  );
}
