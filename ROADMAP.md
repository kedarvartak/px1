# px1 roadmap

Understand the code your agent wrote before you merge it.

Our public-launch priority is meaningful human inspection after delegating
coding to an agent. Acknowledging AI findings is insufficient: reviewers need
to challenge incorrect findings and inspect code with no findings. The roadmap
prioritizes that outcome over expanding automated detection.

px1 currently ships one Action-based product shape with two delivery choices:

```text
push PR → GitHub Action → static review artifact → authenticated download or deliberate Pages publication → PR link
```

The Action remains the free, self-run mode. The next product checkpoint adds a
hosted GitHub App without removing the commit-pinned report or human review
progress that make px1 auditable.

## Hosted v1: context-aware PR bot

The hosted v1 is built in small, independently reviewable changes. Its first
backend-foundation tasks are:

1. [x] **Choose the hosted stack.** Go API and workers, PostgreSQL 16 with
   pgvector, River for transactional jobs, and S3-compatible object storage.
   See [the hosted architecture decision](docs/HOSTED_ARCHITECTURE.md).
2. [x] **Prepare GitHub App registration.** Separate development and production
   manifests, least-privilege permissions, webhook subscriptions, and a
   secrets-manager handoff for private keys.
3. [x] **Build webhook ingestion.** Verify HMAC-SHA256 signatures, deduplicate
   delivery IDs, enqueue accepted events, and acknowledge within one second.
4. [x] **Create the hosted schema.** Installations, repositories, indexed files
   and chunks, pull requests, reviews, findings, comments, feedback, and jobs.
5. [x] **Add the GitHub installation client.** Mint and cache installation
   tokens, respect primary and secondary rate limits, and retry safely.
6. [x] **Add the LLM gateway.** One bounded interface for OpenAI, Anthropic, and
   Bedrock with timeout, retry, prompt-cache, token, and cost telemetry.
7. [x] **Prepare staging and production deployment.** Ship isolated Fly
   manifests, role-aware health endpoints, structured logs, OpenTelemetry trace
   export, and Sentry error reporting with required hosted configuration.
8. [x] **Clone repositories as untrusted data.** Use bounded, filtered shallow
   clones in disposable directories and never execute repository code.
9. [x] **Detect and parse source languages.** Recognize the initial language
   set, use registered tree-sitter parsers, and fall back to bounded line data.
10. [x] **Chunk at symbol boundaries.** Produce retrieval chunks of roughly
    200–800 tokens without silently discarding oversized symbols.
11. [x] **Embed in bounded batches.** Persist provider model and version with
    every vector so indexes can be migrated safely.
12. [x] **Summarize substantial files.** Create bounded, low-cost summaries for
    source files over 50 lines while skipping generated and binary content.
13. [x] **Re-index incrementally.** Compare default-branch blob SHAs and mutate
    only added, changed, or deleted file chunks in one indexed-head transaction.
14. [x] **Enforce repository limits.** Cap v1 indexing at 50,000 files or 2 GiB
    and expose partial-index state wherever review context is summarized.
15. [x] **Make diff grouping worker-callable.** Move deterministic change-block
    grouping behind a reusable review package without changing Action output.
16. [x] **Assemble bounded review context.** Combine diff, functions, retrieval,
    relationships, tests, summaries, and rules within a 60k-token budget.
17. [x] **Define strict finding JSON.** Validate auditable P0-P2 findings before
    persistence or GitHub delivery.
18. [x] **Verify proposed findings.** Re-read each finding against its evidence
   and drop unsupported or sub-0.6-confidence output.
19. [x] **Deduplicate and rank findings.** Collapse overlapping, similarly titled
   findings, order by severity then confidence, and cap inline delivery.
20. [x] **Render an auditable review summary.** Keep deterministic signals
   verbatim beside bounded model prose.
21. [x] **Map findings to diff lines.** Deliver only comments whose ranges are
   valid on the current diff and retain the rest in the summary.
22. [x] **Post one review per run.** Send all inline findings through GitHub's
   Create Review API instead of emitting one notification per comment.
23. [x] **Maintain one sticky summary.** Find the bot-owned marker and edit the
   existing PR summary in place.
24. [x] **Expose review check status.** Publish in-progress, completed, and
   failed check runs with a retry action.
25. [x] **Authorize review commands.** Parse supported `@px1` issue-comment
   commands and require repository write access before any work is enqueued.
26. [ ] **Re-review incrementally.** Diff from the last reviewed SHA, resolve
   fixed threads, and carry dismissed finding fingerprints forward.
27. [ ] **Publish commit-pinned reports.** Run the exporter in the worker and
   upload an immutable report bundle under the full head SHA.
28. [ ] **Gate report access.** Require GitHub OAuth and confirm repository read
   access before returning private review artifacts.
29. [ ] **Show hosted findings in reports.** Keep AI findings visibly separate
   from deterministic evidence and browser-local human review progress.
30. [ ] **Create the v1 evaluation set.** Define auditable historical and seeded
   benchmark cases across public repositories.

The v1 review pipeline, indexing, report hosting, evaluation, and launch gates
follow these foundations. AI findings must remain auditable: evidence,
retrieved context, confidence inputs, and verifier disposition are visible.

## Current foundation

- [x] Commit-pinned static report generation
- [x] Structured diffs with old and new line numbers
- [x] Repository rules from `.px1/rules.json`
- [x] Exact-revision GitHub check summaries
- [x] Deterministic attention signals
- [x] Provider-neutral explanation chips
- [x] Historical SHA report retention
- [x] GitHub Pages deployment and sticky PR comment
- [x] Reusable report and history Actions
- [x] Removal of the local server and workspace application
- [x] Optional AI explanations generated in Actions with strict JSON output
- [x] AI input exclusion globs for sensitive repository paths
- [x] Rich team rules with severity, rationale, literal matching, and block matching
- [x] Large-report file and changed-code search
- [x] Machine-readable `review.json` alongside the reviewer HTML
- [x] Browser rendering, accessibility, and large-diff performance gates
- [x] `validate-rules` CLI for repository policy checks
- [x] Immutable release workflow for the `v1` Action tag
- [x] Access-controlled Actions-artifact delivery for private repositories
- [x] Privacy checks, model-transfer warnings, and report checksums

## Public v1 launch priorities

The existing `v1` Action tag identifies a release; this checklist governs the
broader public product launch. It does not imply every item is already shipped.

1. [x] **Explicit human review progress (implemented in this change).** Every
   changed file has unreviewed, reviewed, and needs-follow-up states, including
   files with no findings. Next-unreviewed navigation works across filters.
   Headline progress is independent of finding acknowledgements. Marks are
   deliberate, browser-local declarations scoped to repository/base/head;
   scrolling never marks code reviewed, and marks never imply approval.
2. [x] **Behavioral understanding beside the code.** Deterministic inspection
   cards now separate observed evidence from a concrete reviewer question for
   selected high-impact changes and source/test relationships. They link to the
   relevant diff or changed companion tests, expose output limits, and make no
   claim about author intent, correctness, or coverage.
3. [x] **Transparent evidence and limitations.** The report now separates human
   review progress, deterministic analysis, AI explanations, and CI results in
   an evidence panel. Each source names its represented revision and state;
   skipped or bounded analysis and missing inputs are visible. Empty findings
   explicitly mean no matches, not safety, and the snapshot disclaims knowledge
   of the current live PR head and check status.
4. [x] **Safe private-code delivery and publication.** Private repositories now
   have a dedicated workflow that uploads an authenticated, expiring Actions
   artifact and requires repository read access. Public Pages is a separate,
   explicitly warned template. Generated bundles include per-file SHA-256
   checksums, and the Action blocks high-confidence secret signatures and
   sensitive paths by default while warning on model transfer. The docs state
   that scanning is not redaction or access control and checksums do not prove
   authorship.
5. [ ] **Demonstrate and measure the original problem.** Demo one misleading
   automated finding and one meaningful unflagged problem, showing the evidence
   a person uses to decide. Run a pilot with roughly 5–10 agent-heavy developers
   on comparable changes. Compare comprehension, correct decisions, and time
   against their existing workflow. Click counts alone are not a success metric.

## Supporting work shipped alongside the review workflow

- **Repository code-writing policy.** `.px1/rules.json` now contains scoped,
  deterministic conventions for clearer JavaScript, TypeScript, and Go instead
  of treating broad change-risk signals as team style rules.
- **PR summary improvements — [PR #136](https://github.com/kedarvartak/px1/pull/136).**
  Summarize findings and verification with the artifact link while keeping the
  comment an invitation to inspect code, not an approval-like conclusion.

## After launch

- **Rule analysis depth.** Safe language-aware structural checks beyond text and
  block matching, with bounded performance and explicit limitations. Advance
  this only for a concrete reviewer or pilot need; it is not a launch gate.
- **Review workflow refinement.** Improve explanations, context navigation, and
  feedback handoff using pilot evidence about comprehension and decisions.

## Positioning and success

AI plus human review is an established category, not an exclusive moat. px1's
focus is a portable, optional-AI workflow that helps people understand changes
from their existing agents. A durable advantage must come from demonstrated
review outcomes and repeated use. See [the product vision](docs/PRODUCT_VISION.md).

The old local review sessions, agent editing, verification commands, activity
feeds, and external-agent issues were retired with the local application and
are no longer active roadmap work.

## Product constraints

- Never execute code from an untrusted PR head.
- Keep the GitHub Action usable without a px1 account or hosted backend.
- Keep reports tied to one full head SHA.
- Keep approval and requests for changes in GitHub.
- Treat AI output as a finding to verify, never as proof.
- Prefer evidence visible in the diff over opaque scores.
- Put a tenant identifier on every hosted data row and test isolation.
- Never execute pull-request code during review or indexing.
