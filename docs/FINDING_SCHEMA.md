---
title: Finding contract
section: reference
kind: reference
status: shipped
covers:
  - schemas/finding.schema.json
  - internal/hosted/review/finding.go
verified_at:
---

# Finding contract

Model findings use strict JSON with no unknown fields. Every finding names a
repository-relative path and valid line range, P0–P2 severity, supported type,
title, explanation, concrete evidence, optional local suggestion, confidence,
and optional rule ID. Runtime validation mirrors `schemas/finding.schema.json`
and caps both the number and size of findings before persistence or posting.

## Fields

Read from `schemas/finding.schema.json` by `px1 docs gen`.

<!-- px1:generated finding-fields -->

| Field | Type | Required | Constraints |
| --- | --- | --- | --- |
| `path` | string | yes | not empty |
| `start_line` | integer | yes | at least 1 |
| `end_line` | integer | yes | at least 1 |
| `severity` | enum | yes | one of `P0`, `P1`, `P2` |
| `type` | enum | yes | one of `logic`, `syntax`, `style`, `security` |
| `title` | string | yes | not empty; at most 200 characters |
| `body` | string | yes | not empty; at most 16000 characters |
| `evidence` | string | yes | not empty; at most 8000 characters |
| `confidence` | number | yes | at least 0; at most 1 |
| `rule_id` | string | no |  |
| `suggestion` | string | no | at most 16000 characters |

<!-- px1:end finding-fields -->
