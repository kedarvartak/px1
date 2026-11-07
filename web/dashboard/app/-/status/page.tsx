import styles from "../public.module.css";

export const metadata = { title: "px1 status" };
export const dynamic = "force-dynamic";

type Status = { overall: string; checkedAt: string; components: { name: string; state: string }[] };

async function loadStatus(): Promise<Status | null> {
  const url = process.env.PX1_STATUS_URL;
  if (!url) return null;
  try {
    const response = await fetch(url, { cache: "no-store", signal: AbortSignal.timeout(5_000) });
    return response.ok ? ((await response.json()) as Status) : null;
  } catch {
    return null;
  }
}

export default async function StatusPage() {
  const status = await loadStatus();
  return (
    <main className={styles.page}>
      <h1>Status</h1>
      {status === null ? (
        <p>Status could not be checked. This page makes no claim that services are up or down.</p>
      ) : (
        <>
          <p className={styles.price}>{status.overall === "operational" ? "All checked services are operational" : "Some services are unavailable"}</p>
          <ul>
            {status.components.map((component) => (
              <li key={component.name}>{component.name}: {component.state}</li>
            ))}
          </ul>
          <p className={styles.note}>Checked at <time dateTime={status.checkedAt}>{status.checkedAt}</time> using each service&apos;s readiness endpoint.</p>
        </>
      )}
    </main>
  );
}
