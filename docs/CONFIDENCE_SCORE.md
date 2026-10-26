# Review confidence score

px1's confidence score is a deterministic summary of the evidence available to
the review. It is not a probability that the change is correct and must not be
used as approval by itself.

Every review starts at 5. The following visible deductions are applied:

| Input | Deduction |
| --- | --- |
| Each unresolved P0 | 2, capped at 4 |
| Each unresolved P1 | 1, capped at 3 |
| More than 1,000 changed lines | 1 |
| More than 3,000 changed lines | 2 instead |
| Complexity increase above 15 | 1 |
| Fewer than half of changed symbols have linked tests | 1 |
| One or more high-risk paths changed | 1 |
| Repository-pattern fit below 50% | 1 |

The result is clamped to 0–5. The API returns each deduction with the input and
plain-language reason, so the report can show exactly how the score was formed.
