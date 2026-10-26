type MetricChartProps = {
  title: string;
  values: number[];
  labels: string[];
  format?: (value: number) => string;
};

export function MetricChart({ title, values, labels, format = String }: MetricChartProps) {
  const maximum = Math.max(1, ...values);
  const points = values.map((value, index) => {
    const x = values.length <= 1 ? 50 : (index / (values.length - 1)) * 100;
    const y = 100 - (value / maximum) * 90;
    return `${x},${y}`;
  }).join(" ");

  return (
    <section className={styles.chart}>
      <h2>{title}</h2>
      {values.length ? (
        <>
          <svg role="img" aria-label={`${title} over time`} viewBox="0 0 100 100" preserveAspectRatio="none">
            <polyline points={points} fill="none" vectorEffect="non-scaling-stroke" />
          </svg>
          <table>
            <thead><tr><th>Date</th><th>{title}</th></tr></thead>
            <tbody>{values.map((value, index) => <tr key={labels[index]}><td>{labels[index]}</td><td>{format(value)}</td></tr>)}</tbody>
          </table>
        </>
      ) : <p>No data matches these filters.</p>}
    </section>
  );
}
import styles from "./metric-chart.module.css";
