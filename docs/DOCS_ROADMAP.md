---
title: Self-evolving documentation: roadmap
section: operations
kind: narrative
status: planned
covers:
  - internal/docs/
  - docs_cmd.go
  - docs_update.go
  - docs_eval.go
  - eval/docs-cases.json
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
| 5. Self-evolving agent | `px1 docs apply` applies edits that stay in one section and quote real code. `px1 docs update` has a model draft them, under the existing budget meter, and checks each against the code it cites. Committing to the branch from CI comes last. | Yes |
| 6. Quality gates | A docs evaluation set: invented code changes with known documentation consequences, and clean changes that must produce no edits. Scored by `px1 docs eval`; the review tools `eval-run` and `eval-compare` score findings by line and do not fit. | Yes |
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

## Drift detection (phase 4)

`px1 docs impact --base BASE` lists the pages whose `covers` include a file
that differs between BASE and HEAD. Each page is in one of three states:

- **needs review**: the code it covers changed and the page did not;
- **updated**: the page changed in the same diff;
- **generated**: a generated reference page, which the check keeps current.

It also lists changed Go files that no page covers. Test files never count.
The output is Markdown for a pull-request comment (it starts with a marker so
the comment can be updated in place), or JSON with `--json`.

`px1 docs site --impact FILE` takes that JSON and marks the affected pages:
a banner on each page naming the changed files, a marker in the sidebar, and
an "Affected by this pull request" list on the home page.

The report is a map, not a judgment. "Updated" means the page file changed,
not that the change is the right one, and "needs review" does not mean the
page is wrong. Deciding that is phase 5.

## Applying edits (phase 5, first part)

`px1 docs apply --base BASE --edits FILE` applies edits to pages. An edit is
data, whoever wrote it:

```
[{
  "page": "docs/INDEX_LIMITS.md",
  "heading": "Limits",
  "body": "The indexer reads at most 25 files per run.",
  "reason": "The file limit rose from 10 to 25.",
  "citations": [{"file": "indexer.go", "quote": "const maxFiles = 25"}]
}]
```

An edit replaces the text of one section, from its heading to the next
heading of any level. It is applied only when all of this holds:

- the page is a narrative page that the diff from BASE affects;
- the heading names exactly one section, and that section holds no generated
  content;
- the new text is not empty, not the old text reworded only in white space,
  at most 8 KB, has no heading of its own, closes its code fences, and its
  links resolve;
- it gives a reason and one to eight citations;
- every citation names a file the page covers and quotes at least 12
  characters that are really in that file at the head commit, compared with
  white space collapsed. The line numbers are found from the quote; numbers
  supplied with the edit are ignored;
- at least one citation is from a file the diff changed.

Quotes are checked against the files on disk, so the working tree must be
checked out at the head commit; the command refuses to run otherwise. Every
page to be written is checked before any is written.

One edit is accepted per section. A refused edit changes nothing and is
reported with its reason, and the command exits with an error. `--dry-run`
reports without writing; `--json` prints the results, including the line
numbers found for each citation.

These rules bound what an edit can touch and prove that its evidence exists.
They do not prove that the new sentence follows from the evidence. That is
the job of the reviewer of the diff, and of the model check in the next part.

## Drafting edits with a model (phase 5, second part)

`px1 docs update --base BASE --model M --price-input N --price-output N --yes`
drafts edits to the narrative pages that `docs impact` marks "needs review",
and writes the ones that survive to the working tree. Review them with
`git diff -- docs/` and commit them like any other change.

For each page, in order:

1. **Draft.** The model is shown the page's sections and the diff of the
   changed files the page covers. It replies with a list of edits, or an empty
   list. It is told that most changes need no edit and to repeat unchanged
   sentences exactly.
2. **Gate.** Every drafted edit goes through the rules of `docs apply` above.
   A reply cannot aim an edit at another page, and at most four edits are
   taken per page.
3. **Check.** Unless `--verify=false`, a second call shows the model the old
   text, the new text, the cited lines as they are at the head commit and the
   diff, and asks whether every new or changed statement follows from the
   code. Anything but a clear yes refuses the edit. So does a failed call.
4. **Write.** Surviving edits are applied.

What is sent and what it can cost:

- Only the page and the diffs of the files it covers are sent. Files the
  privacy policy excludes (the same one as `agentic-review`, plus
  `--exclude-glob`) are never sent, and secret-like values are redacted.
  This holds for the check too: an edit that cites an excluded file is
  refused without a call, and the cited lines are redacted.
- A diff is cut at 12 KB per file and 40 KB per page; `--max-pages` (default
  6) bounds the pages. What was left out is listed.
- `--dry-run` lists what would be sent and sends nothing. Without `--yes`
  nothing is sent. The key is read from `OPENAI_API_KEY` only.
- `--max-cost` (default $0.50) is a hard cap, enforced by reserving the worst
  case of each call before it is sent.
- `--out FILE` writes every edit, applied or refused, with its citations and
  the metered cost.

Measured on this repository with `gpt-6-luna`: a change that added a
subcommand without making the page false drafted no edit, for one call and
$0.0013. A change to two limits the page states drafted one edit that changed
those two numbers and nothing else, for two calls and $0.0010.

Limits:

- A page is edited only where a section's text is wrong or incomplete. The
  command does not add sections or pages, and does not see changed files that
  no page covers; `docs impact` lists those.
- It reads the diff, not the whole file or the rest of the repository. There
  is no tool loop yet.
- The check is a model's opinion. It lowers the rate of unsupported
  statements; it does not remove the need for a person to read the diff.
- It does not commit or push. Committing to the pull-request branch from CI is
  a later step.

## Measuring the updater (phase 6)

`px1 docs eval --model M --price-input N --price-output N --yes` runs
`docs update` on the cases in `eval/docs-cases.json` and scores it. Each case
is a small invented repository: one page, the files it covers before and
after a change, and what a correct update does.

| Kind | The change | A correct update |
| --- | --- | --- |
| stale | makes a statement on the page false | edits the named section so that it holds the new fact and no longer holds the old one |
| clean | touches nothing the page says | applies no edit |

The set has 8 stale cases (a limit, a default, a removed format, a changed
failure mode, a renamed flag, a changed unit, a changed status code, and a
fact in one of two changed files) and 6 clean cases (an internal rename,
comments only, a bug fix that makes the code match the page, a speed-up, an
addition the page does not describe, a reworded log message).

The run prints each case and three numbers: stale pages corrected, unaffected
pages left alone, and edits applied to sections that were not stale.
`--min-fixed` and `--min-quiet` make the command fail below a bar, `--only ID`
runs one case, and `--out FILE` writes the outcomes. Without `--yes` the
command only validates the case file. `--max-cost` caps the whole set.

A case file is rejected if a case could not tell a good update from a bad
one: a stale case must name one section, every `must_not_contain` string must
be in that section to begin with, and every `must_contain` string must not.

What the set found on its first run: the check in `docs update` refused a
correct edit. The edit changed "5 minutes" to "15 minutes" in a sentence, and
the check objected that the code did not establish the rest of the sentence,
which was already on the page. The check now judges only what differs
between the old and the new text.

Measured with `gpt-6-luna` after that fix, two runs of the whole set:

| Run | Stale pages corrected | Unaffected pages left alone | Edits elsewhere | Cost |
| --- | --- | --- | --- | --- |
| 1 | 8 of 8 | 6 of 6 | 0 | $0.0025 |
| 2 | 7 of 8 | 6 of 6 | 0 | $0.0027 |

The miss in run 2 was a refusal that was right: the draft fixed the status
code but dropped a true sentence, and the check refused it for that. The page
stayed stale, which `docs impact` still reports as "needs review".

Limits of the set:

- 14 cases is enough to catch a broken prompt, not to rank two good ones.
- The cases are invented and small: one page, one or two short files. Real
  pages are longer and real diffs noisier.
- Scoring is by strings in the section. It shows the old fact is gone and the
  new one is present; it does not judge the wording.
- Model replies vary between runs, and a failed network call counts as a
  failed case.

## In CI

The review-report workflow builds the documentation with the trusted base
checkout, after the report history is validated:

- the site for the base commit is published at `docs/` on Pages;
- a preview for the pull request, with the affected pages marked, is
  published at `docs-preview/<head sha>/`;
- the impact report is posted as one pull-request comment that is updated on
  every push, with a link to the preview.

The pull request's files are checked out as data. `px1 docs` reads Markdown
and parses source; it builds and runs nothing from them, and it refuses any
symbolic link that leaves the repository, so a pull request cannot get a file
from the runner published. The step cannot fail the workflow.

Limits: Pages holds one deployment, so a preview is replaced when another
pull request is published. Previews run for pull requests from forks too,
since nothing from them is executed and no secret is involved.
