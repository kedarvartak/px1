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

## Next priorities

1. **Automated explanations in Actions.** Add an optional provider step that
   writes the existing commit-pinned explanation contract. Keep reports fully
   functional without a key.
2. **Semantic team rules.** ✅ Let teams express focused rules in plain language,
   while showing evidence and uncertainty rather than pretending every match is
   a proven defect.
3. **Better review blocks.** Group related hunks across files when they belong
   to one behavioral change.
4. **Report quality gates.** Add fixture-driven browser tests, accessibility
   checks, and large-diff performance limits.
5. **Action releases.** Publish immutable version tags and document safe upgrade
   practices for consuming repositories.

## Product constraints

- Never execute code from an untrusted PR head.
- Never require an account or px1-hosted backend.
- Keep reports tied to one full head SHA.
- Keep approval and requests for changes in GitHub.
- Treat AI output as explanation, not proof.
- Prefer evidence visible in the diff over opaque scores.
