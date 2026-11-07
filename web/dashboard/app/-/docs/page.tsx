import styles from "../public.module.css";

export const metadata = { title: "px1 docs" };

const source = "https://github.com/kedarvartak/px1/blob/master/docs";

const sections = [
  { title: "Install the hosted GitHub App", file: "HOSTED_INSTALL.md" },
  { title: "Run the free, self-hosted Action", file: "FREE_ACTION.md" },
  { title: "Deliver private reports", file: "PRIVATE_DELIVERY.md" },
  { title: "How confidence is scored", file: "CONFIDENCE_SCORE.md" },
  { title: "Metric definitions", file: "METRIC_DEFINITIONS.md" },
  { title: "Configuration schema", file: "config.schema.json" },
  { title: "Privacy policy", file: "PRIVACY_POLICY.md" },
  { title: "Data retention", file: "DATA_RETENTION.md" },
  { title: "Terms", file: "TERMS.md" },
];

export default function Docs() {
  return (
    <main className={styles.page}>
      <h1>Documentation</h1>
      <p>The documentation lives with the code, so each page matches the commit it describes.</p>
      <ul>
        {sections.map((section) => (
          <li key={section.file}>
            <a href={`${source}/${section.file}`}>{section.title}</a>
          </li>
        ))}
      </ul>
    </main>
  );
}
