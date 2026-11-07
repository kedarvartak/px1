import { readFile } from "node:fs/promises";
import path from "node:path";

export type ChangelogDay = { date: string; entries: { summary: string; number: number; url: string }[] };

const dateLine = /^## (\d{4}-\d{2}-\d{2})$/;
const entryLine = /^- (.+) \(\[#(\d+)\]\((https:\/\/github\.com\/kedarvartak\/px1\/pull\/\d+)\)\)$/;

// The repository CHANGELOG.md is validated by internal/hosted/publicsite.ParseChangelog;
// this reader only renders lines that already passed that gate.
export async function readChangelog(): Promise<ChangelogDay[]> {
  const markdown = await readFile(path.join(process.cwd(), "..", "..", "CHANGELOG.md"), "utf8");
  const days: ChangelogDay[] = [];
  for (const line of markdown.split("\n")) {
    const date = dateLine.exec(line);
    if (date) {
      days.push({ date: date[1], entries: [] });
      continue;
    }
    const entry = entryLine.exec(line);
    if (entry && days.length > 0) {
      days[days.length - 1].entries.push({ summary: entry[1], number: Number(entry[2]), url: entry[3] });
    }
  }
  return days;
}
