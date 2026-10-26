import Link from "next/link";
import { notFound } from "next/navigation";
import styles from "./dashboard.module.css";

type DashboardPageProps = {
  params: Promise<{ organization: string; route: string[] }>;
};

const sections: Record<string, { title: string; description: string }> = {
  repositories: { title: "Repositories", description: "Index status, recent reviews, and repository settings." },
  "pull-requests": { title: "Pull requests", description: "Review scores, findings, and human review progress." },
  rules: { title: "Rules", description: "Repository rules and evidence-backed proposals." },
  memories: { title: "Memories", description: "Review preferences with their source evidence." },
  analytics: { title: "Analytics", description: "Review outcomes and quality signals over time." },
  billing: { title: "Billing", description: "Shared credits, usage, and spending limits." },
  members: { title: "Members", description: "Organization roles and team access." },
  settings: { title: "Settings", description: "Organization-wide review defaults." },
};

function validSegment(value: string) {
  return /^[A-Za-z0-9_.-]+$/.test(value);
}

export default async function DashboardPage({ params }: DashboardPageProps) {
  const { organization, route } = await params;
  if (!validSegment(organization) || route.some((segment) => !validSegment(segment))) notFound();

  const [section, identifier, action, number] = route;
  let content = sections[section];
  if (section === "repositories" && identifier && action === "settings" && route.length === 3) {
    content = { title: `${identifier} settings`, description: "Configure review behaviour for this repository." };
  } else if (section === "repositories" && identifier && action === "pull-requests" && number && /^[1-9]\d*$/.test(number) && route.length === 4) {
    content = { title: `${identifier} pull request #${number}`, description: "Findings and the commit-pinned review report." };
  } else if (!content || route.length !== 1) {
    notFound();
  }

  return (
    <main>
      <nav aria-label="Dashboard" className={styles.navigation}>
        <Link href={`/${organization}`}>Overview</Link>
        {Object.entries(sections).map(([slug, item]) => (
          <Link href={`/${organization}/${slug}`} key={slug}>{item.title}</Link>
        ))}
      </nav>
      <p>{organization}</p>
      <h1>{content.title}</h1>
      <p>{content.description}</p>
      {section === "repositories" && action === "pull-requests" && number ? (
        <iframe className={styles.report} title={`Review report for ${identifier} pull request ${number}`} src={`/api/reports/${encodeURIComponent(organization)}/${encodeURIComponent(identifier)}/${encodeURIComponent(number)}`} />
      ) : null}
    </main>
  );
}
