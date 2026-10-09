# px1 roadmap

Understand the code your agent wrote before you merge it.

Our public-launch priority is meaningful human inspection after delegating
coding to an agent. Acknowledging AI findings is insufficient: reviewers need
to challenge incorrect findings and inspect code with no findings. The roadmap
prioritizes that outcome over expanding automated detection.

px1 has one product flow:

```text
push PR → GitHub Action → static review artifact → GitHub Pages → PR link
```

Local code browsing, local review sessions, agent editing, self-hosted imports,
accounts, and a managed SaaS are outside the product.

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
- [x] Cross-file change groups and large-report search
- [x] Machine-readable `review.json` alongside the reviewer HTML
- [x] Browser rendering, accessibility, and large-diff performance gates
- [x] `validate-rules` CLI for repository policy checks
- [x] Immutable release workflow for the `v1` Action tag

## Public v1 launch priorities

The existing `v1` Action tag identifies a release; this checklist governs the
broader public product launch. It does not imply every item is already shipped.

1. [x] **Explicit human review progress (implemented in this change).** Every
   changed file has unreviewed, reviewed, and needs-follow-up states, including
   files with no findings. Next-unreviewed navigation works across filters.
   Headline progress is independent of finding acknowledgements. Marks are
   deliberate, browser-local declarations scoped to repository/base/head;
   scrolling never marks code reviewed, and marks never imply approval.
2. [ ] **Behavioral understanding beside the code.** Explain important behavior
   changes with concrete inspection questions and links to the relevant diff,
   tests, or related files. Distinguish observed evidence from inferred intent.
   Start with a small useful set, without requiring general-purpose chat.
3. [ ] **Transparent evidence and limitations.** Separate human review progress,
   automated analysis, AI explanations, and CI results. Surface skipped and
   truncated analysis, missing verification, and the revision represented by
   each source. An empty findings list must not imply safety. A static snapshot
   must not claim to know the current live PR head.
4. [ ] **Safe private-code delivery and publication.** Provide an access-appropriate
   delivery path, such as downloadable Actions artifacts, before positioning
   px1 for private repositories. Public Pages publication must be deliberate.
   Add redaction checks, clear model-transfer warnings, and checksums; redaction
   is not access control and checksums alone do not prove authorship.
5. [ ] **Demonstrate and measure the original problem.** Demo one misleading
   automated finding and one meaningful unflagged problem, showing the evidence
   a person uses to decide. Run a pilot with roughly 5–10 agent-heavy developers
   on comparable changes. Compare comprehension, correct decisions, and time
   against their existing workflow. Click counts alone are not a success metric.

## Supporting work already proposed

- **Reviewer feedback export — [PR #134](https://github.com/kedarvartak/px1/pull/134).**
  Export local notes and acknowledgements as Markdown or JSON. Follow up by
  including file-review progress and unresolved follow-ups; keep the distinction
  between local declarations and shared GitHub review explicit.
- **Explainable affected-file hints — [PR #135](https://github.com/kedarvartak/px1/pull/135).**
  Keep suggestions separate from changed files, verify the evidence, and make
  related code inspectable. Suggestions alone do not establish impact.
- **PR summary improvements — [PR #136](https://github.com/kedarvartak/px1/pull/136).**
  Summarize findings and verification with the artifact link. Keep the comment
  an invitation to inspect code, avoiding approval-like conclusions.

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
- Never require an account or px1-hosted backend.
- Keep reports tied to one full head SHA.
- Keep approval and requests for changes in GitHub.
- Treat AI output as explanation, not proof.
- Prefer evidence visible in the diff over opaque scores.
