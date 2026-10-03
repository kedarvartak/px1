# Static review explanations

px1 can add short, expandable AI explanation chips to a static PR review
report. The explanation generator is intentionally separate from px1: a local
script, CI job, or future provider integration writes JSON, and `export-review`
validates and renders it.

```json
{
  "version": 1,
  "revision": "0123456789abcdef0123456789abcdef01234567",
  "explanations": [
    {
      "id": "retry-behavior",
      "path": "api/client.ts",
      "lineStart": 12,
      "lineEnd": 18,
      "title": "Changes retry behavior",
      "summary": "Failed requests now retry twice before the error reaches the caller."
    }
  ]
}
```

Generate the file after the PR head commit exists, then pass it explicitly:

```bash
px1 export-review \
  --base "$BASE_SHA" \
  --head "$HEAD_SHA" \
  --explanations-file ./px1-explanations.json \
  --out ./site
```

The exporter rejects stale or malformed input. `revision` must be the exact
40-character review-head SHA; every path must be a changed repository-relative
file; IDs must be unique; and line ranges, titles, summaries, and item counts
are bounded. Explanation text is handled as untrusted data and inserted into
the report with DOM text nodes, not HTML.

The JSON contract is provider-neutral. px1 does not call an LLM or require an
API key during export. Explanations are review aids and may be wrong; reviewers
should confirm them against the diff and CI evidence.
