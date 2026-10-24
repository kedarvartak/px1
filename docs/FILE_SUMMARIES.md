# File summaries

The hosted index creates concise summaries only for source files over 50 lines.
Generated files, binary/non-UTF-8 content, short files, and paths excluded from
model transfer are skipped before any provider call. Source input is capped at
64 KiB, output at 300 tokens, and truncation is recorded. Stored summaries are
tied to the full source digest, model, and prompt version so stale summaries can
be detected and rebuilt.
