# Review context assembly

Hosted reviews assemble context in a fixed, auditable order: diff hunks, full
changed functions, the top eight similar chunks, direct imports/importers,
matching tests, file summaries, `rules.md`, and matched rules. The default
budget is 60,000 tokens. Items that do not fit are omitted and the bundle is
marked truncated; each item retains its kind, path, symbol, and retrieval score.
