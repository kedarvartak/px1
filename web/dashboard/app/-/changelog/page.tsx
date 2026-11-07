import { readChangelog } from "../../../lib/changelog";
import styles from "../public.module.css";

export const metadata = { title: "px1 changelog" };

export default async function Changelog() {
  const days = await readChangelog();
  return (
    <main className={styles.page}>
      <h1>Changelog</h1>
      <p className={styles.note}>Each entry links the merged pull request that shipped it. Dates are UTC.</p>
      {days.map((day) => (
        <section key={day.date}>
          <h2><time dateTime={day.date}>{day.date}</time></h2>
          <ul>
            {day.entries.map((entry) => (
              <li key={entry.number}>
                {entry.summary} (<a href={entry.url}>#{entry.number}</a>)
              </li>
            ))}
          </ul>
        </section>
      ))}
    </main>
  );
}
