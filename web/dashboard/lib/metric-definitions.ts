export type MetricDefinition = {
  slug: string;
  name: string;
  definition: string;
  numerator: string;
  denominator: string;
  exclusions: string;
};

export const metricDefinitions: MetricDefinition[] = [
  {
    slug: "bugs-caught",
    name: "Bugs caught before merge",
    definition: "P0 or P1 findings addressed before the pull request merged, normalized per 100 reviewed pull requests.",
    numerator: "P0 and P1 findings whose range changed before merge or received a thumbs-up.",
    denominator: "Pull requests with a completed px1 review, divided by 100.",
    exclusions: "Draft-only runs, cancelled reviews, and P2 findings.",
  },
  {
    slug: "addressed-rate",
    name: "Addressed rate",
    definition: "The share of delivered inline findings that the author addressed or a reviewer endorsed.",
    numerator: "Inline findings whose range changed before merge or received a thumbs-up.",
    denominator: "All inline findings delivered in completed reviews.",
    exclusions: "Summary-only observations and suppressed findings.",
  },
  {
    slug: "downvote-rate",
    name: "Downvote rate",
    definition: "The share of delivered inline findings that received a thumbs-down.",
    numerator: "Distinct delivered inline findings with at least one thumbs-down.",
    denominator: "All inline findings delivered in completed reviews.",
    exclusions: "Repeated reactions by the same actor and deleted bot comments.",
  },
  {
    slug: "time-to-review",
    name: "Time to first review",
    definition: "Elapsed time from pull-request creation to the first completed px1 summary for its head revision.",
    numerator: "Median elapsed milliseconds across eligible pull requests.",
    denominator: "Pull requests with both a creation timestamp and completed first review.",
    exclusions: "Draft waiting time when automatic draft review is disabled and cancelled reviews.",
  },
  {
    slug: "human-review-completion",
    name: "Human review completion",
    definition: "The share of reviewed pull requests where every changed file was marked reviewed before merge.",
    numerator: "Merged pull requests with all changed files marked reviewed before the merge timestamp.",
    denominator: "Merged pull requests with browser progress telemetry explicitly shared by the reviewer.",
    exclusions: "Browser-local progress that was never shared and pull requests still open.",
  },
  {
    slug: "comments-per-pr",
    name: "Comments per pull request",
    definition: "Average number of px1 inline findings delivered per completed review.",
    numerator: "Delivered inline findings.",
    denominator: "Pull requests with a completed px1 review, including zero-comment reviews.",
    exclusions: "Summary rows, command replies, and comments removed before delivery.",
  },
];
