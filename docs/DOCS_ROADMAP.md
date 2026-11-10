# Self-evolving documentation: roadmap

Goal: px1's documentation is detailed, has a UI, and changes in the same pull
request as the code it describes.

## Design

- **Markdown in `docs/` is the source of truth.** The UI is rendered from it.
  Nothing is written only in the UI.
- **Two kinds of content.** Reference pages (CLI flags, config schema, agent
  tools, finding schema, action inputs) are generated from code and cannot
  drift. Narrative pages (guides, concepts, architecture) are drafted by a model
  and must be grounded.
- **Every generated claim cites code.** A model-written edit carries the file
  and lines it rests on and passes the finding verifier. Unverified edits are
  not applied.
- **Docs change in the same pull request as the code,** as an ordinary diff a
  reviewer approves.
- **The docs agent never runs pull-request code.** It is built from the base
  commit and reads the pull request as data, like the dogfood reviewer.

## Decisions taken

| Question | Decision |
| --- | --- |
| Where do edits land? | A bot commit on the pull-request branch, for same-repository branches only, so docs and code merge together. |
| Where is the UI hosted? | GitHub Pages, next to the review reports. px1 is public. Private repositories need a different answer later. |
| May the agent create pages? | Not at first. It updates existing pages and flags gaps; a person decides on new pages. |

## Phases

| Phase | What is built | Needs a model |
| --- | --- | --- |
| 0. Inventory | Audit of the current pages and the target structure. See `DOCS_INVENTORY.md`. | No |
| 1. Foundation | Front matter on every page (title, kind, status, `covers` code paths, `verified_at` commit), `docs/manifest.json`, and `px1 docs check` for broken links, orphan pages and code with no page. | No |
| 2. Generated reference | `px1 docs gen` writes reference pages from the real flag sets, schemas, tool specs and action inputs. CI fails when they are out of date. | No |
| 3. UI | A static site built from the Markdown: sidebar, search, per-page status and "verified at commit", source links, and a "changed in this pull request" view. Published on Pages with a preview per pull-request head. | No |
| 4. Drift detection | `px1 docs impact --base --head` lists the pages a diff affects, using `covers`. Posted as a pull-request comment; stale pages are marked in the UI. | No |
| 5. Self-evolving agent | `px1 docs update` drafts edits to affected pages with the existing planner, tools, budget and verifier, adds a changelog entry, and commits to the branch. | Yes |
| 6. Quality gates | A docs evaluation set: seeded code changes with known documentation consequences, and clean changes that must produce no edits. Scored with `eval-run` and `eval-compare`. | Yes |
| 7. Rollout | px1's own pull requests first, then a second repository, then as a product feature. | Yes |

Phases 1 to 4 are useful without any generation and ship first.

## Risks

- **Invented documentation.** Mitigated by citations, the verifier, the
  evaluation set, and a person approving the diff.
- **A bot with write access to branches.** Same-repository branches only, a
  narrowly scoped token, and the base-built tool.
- **Churn.** Edits are limited to pages the diff affects, and clean cases in the
  evaluation set measure needless rewording.
- **Cost.** About one extra review per pull request, under the same per-run cap.
