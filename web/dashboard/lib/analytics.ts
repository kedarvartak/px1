export type AnalyticsPoint = {
  day: string;
  findings: number;
  addressedRate: number;
  medianReviewLatencyMs: number;
};

export type AnalyticsFilters = {
  repository?: string;
  team?: string;
  author?: string;
};

const filterPattern = /^[A-Za-z0-9_.@/-]{1,100}$/;

export function analyticsQuery(filters: AnalyticsFilters) {
  const query = new URLSearchParams();
  for (const [key, value] of Object.entries(filters)) {
    if (value && filterPattern.test(value)) query.set(key, value);
  }
  return query.toString();
}

export async function loadAnalytics(organization: string, filters: AnalyticsFilters): Promise<AnalyticsPoint[]> {
  const base = process.env.PX1_API_URL;
  if (!base || !/^https?:\/\//.test(base)) return [];
  const query = analyticsQuery(filters);
  const response = await fetch(`${base.replace(/\/$/, "")}/v1/organizations/${encodeURIComponent(organization)}/analytics${query ? `?${query}` : ""}`, {
    cache: "no-store",
    headers: { accept: "application/json" },
    signal: AbortSignal.timeout(5_000),
  });
  if (!response.ok) throw new Error(`analytics request failed with status ${response.status}`);
  const payload: unknown = await response.json();
  if (!Array.isArray(payload) || !payload.every(validPoint)) throw new Error("analytics response contains invalid points");
  return payload;
}

function validPoint(value: unknown): value is AnalyticsPoint {
  if (!value || typeof value !== "object") return false;
  const point = value as Record<string, unknown>;
  return typeof point.day === "string" && /^\d{4}-\d{2}-\d{2}$/.test(point.day)
    && typeof point.findings === "number" && point.findings >= 0
    && typeof point.addressedRate === "number" && point.addressedRate >= 0 && point.addressedRate <= 1
    && typeof point.medianReviewLatencyMs === "number" && point.medianReviewLatencyMs >= 0;
}
