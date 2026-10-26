import { MetricChart } from "../../../components/metric-chart";
import { loadAnalytics } from "../../../lib/analytics";
import styles from "./analytics.module.css";

type AnalyticsPageProps = {
  params: Promise<{ organization: string }>;
  searchParams: Promise<{ repository?: string; team?: string; author?: string }>;
};

export default async function AnalyticsPage({ params, searchParams }: AnalyticsPageProps) {
  const [{ organization }, filters] = await Promise.all([params, searchParams]);
  const points = await loadAnalytics(organization, filters);
  const labels = points.map((point) => point.day);
  return (
    <main>
      <p>{organization}</p>
      <h1>Review analytics</h1>
      <form className={styles.filters}>
        <label>Repository <input name="repository" defaultValue={filters.repository} /></label>
        <label>Team <input name="team" defaultValue={filters.team} /></label>
        <label>Author <input name="author" defaultValue={filters.author} /></label>
        <button type="submit">Apply filters</button>
      </form>
      <div className={styles.grid}>
        <MetricChart title="Findings" labels={labels} values={points.map((point) => point.findings)} />
        <MetricChart title="Addressed rate" labels={labels} values={points.map((point) => point.addressedRate)} format={(value) => `${Math.round(value * 100)}%`} />
        <MetricChart title="Median review time" labels={labels} values={points.map((point) => point.medianReviewLatencyMs)} format={(value) => `${Math.round(value / 1000)}s`} />
      </div>
    </main>
  );
}
