# px1 roadmap

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

## Next priorities

1. **Reviewer feedback export.** Let reviewers export local acknowledgements and
   notes as Markdown or JSON that can be pasted into the GitHub PR. Keep the
   report static; do not introduce a backend or pretend browser-local state is
   shared.
2. **Explainable affected-file hints.** Add deterministic import, test, config,
   and dependency evidence for nearby files without mixing suggestions into the
   mandatory changed-file queue.
3. **PR summary improvements.** Add concise finding counts, rule highlights,
   verification state, and the commit-pinned artifact link to the sticky comment.
4. **Privacy and integrity hardening.** Add optional redaction checks, artifact
   checksums, and clearer warnings before publishing reports or sending diffs to
   an external model.
5. **Rule analysis depth.** Extend block rules with safe language-aware
   structural checks while keeping deterministic text fallbacks and bounded
   performance.

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
