"use client";

export default function AnalyticsError({ reset }: { error: Error; reset: () => void }) {
  return (
    <main>
      <h1>Analytics unavailable</h1>
      <p>The analytics API did not return a usable response.</p>
      <button type="button" onClick={reset}>Try again</button>
    </main>
  );
}
