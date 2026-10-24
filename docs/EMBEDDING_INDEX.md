# Embedding batches and versioning

The indexer sends at most 64 chunks per embedding request and caps one indexing
operation at 10,000 chunks by default. It rejects missing, non-finite, and
wrong-dimension vectors before persistence. Every stored vector includes its
tenant, repository, chunk, provider model, explicit model version, and observed
dimension so a model migration can build a parallel index instead of silently
mixing incompatible vectors.
