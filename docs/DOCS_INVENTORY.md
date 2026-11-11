---
title: Documentation inventory (phase 0)
section: operations
kind: narrative
status: shipped
covers: []
verified_at:
---

# Documentation inventory (phase 0)

An audit of `docs/` at the start of the self-evolving documentation work. It
records what exists, what is wrong, and where each page goes.

## How this was checked

A script compared every page with the repository: files and directories named
in backticks, `px1` subcommands, command-line flags, relative links, and which
pages link to which. A sample of pages was then read for whether they describe
things as available that are not. The prose of every page was **not** read line
by line for factual accuracy; phase 1's `covers` mapping is what makes that
check possible page by page.

## Findings

1. **References are mostly sound.** No broken relative links. Every flag named
   in a page exists, apart from one git flag. The file names that do not resolve
   are output files (`results.json`, `scope.json`), not source files.
2. **Most pages cannot be found.** 17 of 36 pages have no inbound link from the
   README, another page, or the dashboard docs page. The dashboard docs page
   links to 9 files (8 pages and the config schema), so 28 pages are unreachable from the UI.
3. **There is no way in.** No index, no getting-started page, no CLI reference,
   and no overview of the path that works today (the CLI and the CI action).
4. **Status is not stated.** Pages about the hosted service (indexing, webhooks,
   dashboard) are written in the present tense, but that code runs only behind
   interfaces and in-memory fakes; nothing is deployed. Only `HOSTED_INSTALL.md`
   says "planned". A reader cannot tell what they can use.
5. **Commands with no documentation.** `generate-explanations`,
   `summary-comment` and `validate-config` appear in no page or README;
   `validate-rules` appears only in the README. That is 4 of 12 subcommands.
6. **Packages with no documentation.** `githubapp`, `integrations`, `learning`,
   `onboarding` and `scm`: 5 of 22 packages under `internal/hosted`.
   Phase 1's `covers` mapping is stricter than a name search and adds
   `billing`, `gitlab` and `report`, which pages mention but none describes.
   The current list is `known_gaps` in `docs.config.json`. It is now empty:
   each of these packages, and `config_cascade.go`, has a page.
7. **Stubs.** 12 pages are under 100 words. Six of them, with `SCIP_INDEXING.md`,
   describe parts of indexing and read better as one page.
8. **Overlap.** Agentic review is spread over four pages with repeated
   material, and `GITHUB_REVIEW_INTEGRATION_PLAN.md` is a plan from before the
   work it describes was built.

## Target structure

| Section | Purpose |
| --- | --- |
| Start | What px1 is, what works today, and a first run. |
| Guides | How to do one thing, step by step. |
| Concepts | How px1 works and why. |
| Reference | Exact facts generated from code. |
| Operations | Running, releasing and measuring px1. |
| Policies | Privacy, retention and terms. |

Every page states a **status**: `shipped` (usable now), `built` (code and tests
exist, not deployed), or `planned`.

## Where each page goes

| Page | Section | Status | Action |
| --- | --- | --- | --- |
| (new) index | Start | shipped | Write |
| (new) getting started | Start | shipped | Write |
| PRODUCT_VISION | Start | shipped | Keep |
| FREE_ACTION | Guides | shipped | Keep |
| AGENTIC_REVIEW_CLI | Guides | shipped | Keep |
| AGENTIC_CI | Guides | shipped | Keep |
| PRIVATE_DELIVERY | Guides | shipped | Keep |
| STATIC_REVIEW_EXPLANATIONS | Guides | shipped | Keep; document `generate-explanations` |
| EVALUATION_SET | Guides | shipped | Split: running an evaluation (guide) and the launch dataset (concept) |
| MCP_SERVER | Guides | built | Keep; state status |
| HOSTED_INSTALL | Guides | planned | Keep; status already stated |
| GITHUB_APP_REGISTRATION | Operations | planned | Keep; state status |
| AGENTIC_REVIEW | Concepts | shipped | Keep; remove what the guides repeat |
| FINDING_VERIFIER | Concepts | shipped | Expand |
| CONFIDENCE_SCORE | Concepts | shipped | Keep |
| REVIEW_CONTEXT | Concepts | built | Expand |
| VERIFICATION_REPORT | Concepts | shipped | Keep |
| SAFE_REPOSITORY_CLONING | Concepts | built | Expand; cover the read-only git rules the CLI uses |
| HOSTED_ARCHITECTURE | Concepts | built | Keep; state status |
| GITHUB_REVIEW_INTEGRATION_PLAN | Concepts | shipped | Rename to describe the static report architecture |
| EMBEDDING_INDEX, FILE_SUMMARIES, INCREMENTAL_REINDEX, INDEX_CHUNKING, INDEX_LIMITS, LANGUAGE_PARSING, SCIP_INDEXING | Concepts | built | Merge into one Indexing page |
| (new) CLI reference | Reference | shipped | Generate |
| (new) Action inputs | Reference | shipped | Generate |
| AGENT_TOOLS | Reference | shipped | Generate the tool list; keep the rules as prose |
| FINDING_SCHEMA | Reference | shipped | Generate |
| config.schema.json | Reference | shipped | Keep; render |
| METRIC_DEFINITIONS | Reference | built | Keep |
| API_KEYS | Reference | built | Keep; state status |
| PUBLIC_PAGES | Reference | built | Keep; state status |
| RELEASING | Operations | shipped | Keep |
| DOGFOOD | Operations | shipped | Update: px1 now reviews its own pull requests |
| DASHBOARD_FOUNDATION | Operations | built | Keep; state status |
| PRIVACY_POLICY, DATA_RETENTION, TERMS | Policies | shipped | Keep |

## Gaps to fill with new pages

- The four undocumented commands, through the generated CLI reference.
- The five undocumented packages, at least a paragraph each in the page that
  covers their area.
- A security model page that gathers what is now spread across the README and
  the agentic pages: what leaves the machine, secret handling, and never
  running repository code.
