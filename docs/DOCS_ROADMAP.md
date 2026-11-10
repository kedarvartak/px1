---
title: Self-evolving documentation: roadmap
section: operations
kind: narrative
status: planned
covers:
  - internal/docs/
  - docs_cmd.go
verified_at:
---

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

## Working with the pages (phase 1)

Every page under `docs/` starts with front matter:

```
---
title: Agentic review in CI
section: guides
kind: narrative
status: shipped
covers:
  - post_github.go
  - agentic-action/
verified_at:
---
```

- `section` is one of start, guides, concepts, reference, operations, policies.
- `kind` is narrative or reference.
- `status` is shipped (usable now), built (code and tests exist, not deployed)
  or planned.
- `covers` lists the code the page describes: files, directories ending in
  `/`, or globs. Drift detection (phase 4) uses it to find the pages a diff
  affects.
- `verified_at` is the commit at which the page was last checked against that
  code. It is empty on every page today, because phase 0 did not read the
  prose line by line. Fill it in only after doing that check.

`px1 docs gen` writes `docs/manifest.json` (what the UI is built from) and
`docs/README.md` (an index of every page). Run it after adding a page or
changing front matter.

`px1 docs gen` also writes the reference content, which is read from source
files as data and never by running them (phase 2):

| Output | Read from |
| --- | --- |
| `CLI_REFERENCE.md` | The subcommands `main.go` dispatches, the doc comment of the function each one calls, and its `flag` definitions. |
| `ACTION_INPUTS.md` | Every `action.yml`. |
| The fields table in `FINDING_SCHEMA.md` | `schemas/finding.schema.json`. |
| The tool table in `AGENT_TOOLS.md` | `Toolbox.Specs` in `internal/hosted/agent/tools.go`. |

The two tables live between `<!-- px1:generated NAME -->` and
`<!-- px1:end NAME -->` markers inside hand-written pages; only the text
between the markers is replaced. A new subcommand needs a doc comment on its
`run` function: that comment is its description in the reference.

`px1 docs check` fails on:

- missing or invalid front matter;
- a relative link that does not resolve;
- a `covers` entry that names nothing;
- a reference page, manifest or index that is out of date;
- code with no page. Each Go package, each Go file of the root package and each
  `extra_units` path in `docs/docs.config.json` must be covered by a page or
  listed in `known_gaps`. A listed gap that gains a page, or disappears, must be
  removed, so the list only shrinks when the documentation really grows.

The check runs in `go test ./...` (`docs_check_test.go`), so CI fails when a
pull request adds a package or command without a page.

## The site (phase 3)

`px1 docs site --out DIR` renders the pages as a static website:

- a sidebar of every page by section, with a dot for its status;
- search over titles, headings and text, in the browser, with no service;
- on each page: its status, whether it was verified or generated, an outline,
  and the code it covers, linked to the source when `--source-url` is given;
- `md/NAME.md`, the page's Markdown exactly as it is in the repository, linked
  from the page, and `manifest.json`.

Links between pages become links within the site. Raw HTML inside a page is
dropped, so a page cannot inject script. The site has no build step and no
network dependency: fonts and scripts are files in the output, and every page
works with JavaScript off (only search and the theme switch need it).

Not built yet: publishing the site on Pages with a preview per pull request,
and the "changed in this pull request" view, which needs phase 4.
