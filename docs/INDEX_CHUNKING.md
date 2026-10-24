# Retrieval chunking

The indexer treats parser symbols as preferred boundaries and groups adjacent
units toward a 200-token target without exceeding 800 tokens. Oversized symbols
are split at UTF-8-safe boundaries instead of being dropped. Every byte of a
valid source file appears exactly once and each embedding input names its path
and symbols. The production embedding tokenizer can be injected through the
token-counter interface; the built-in counter is a conservative fallback.
