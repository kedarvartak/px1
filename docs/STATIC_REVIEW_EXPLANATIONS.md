# Static review explanations

px1 can add short, expandable AI explanation chips to a static PR review
report. You can let the reusable Action generate them with OpenAI, or provide
the same provider-neutral JSON from another generator. In both cases,
`export-review` validates the commit-pinned file before rendering it.

## Generate explanations in the px1 Action

Set an exact model name and expose `OPENAI_API_KEY` from a GitHub Actions
secret. The key remains in the workflow environment: it is not an Action input
and is never written to the report.

```yaml
- name: Generate px1 review artifact
  uses: kedarvartak/px1@v1
  with:
    base: ${{ github.event.pull_request.base.sha }}
    head: ${{ github.event.pull_request.head.sha }}
    explanation-model: your-model-name
    explanation-exclude-globs: |
      config/private/**
      **/*.pem
  env:
    OPENAI_API_KEY: ${{ secrets.OPENAI_API_KEY }}
```

The generator sends only the bounded, non-excluded Git diff to the Responses
API, requests a strict JSON schema, sets `store: false`, and refuses incomplete,
malformed, stale, or out-of-scope output. Use `explanation-exclude-globs` for
paths your team does not want sent to the model. It never checks out or executes
pull-request code. If no model is set,
the Action works without an API key and produces the normal diff-and-rules
artifact.

The exclusion affects only AI input. Excluded files still appear in the local
static report, rules, and deterministic attention signals. Review the API
provider's current data controls before enabling explanations for private code.

## Bring your own explanation file

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

Generate this file after the PR head commit exists, then pass it explicitly to
the px1 Action or report command:

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

The JSON contract is provider-neutral. Provider credentials stay in GitHub
Actions secrets and are never written to the report. Explanations are review
aids and may be wrong; reviewers should confirm them against the diff and CI
evidence.
