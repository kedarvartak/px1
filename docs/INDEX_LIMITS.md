# Repository indexing limits

Hosted v1 indexes at most 50,000 files or 2 GiB of source per repository.
Selection is path-sorted and deterministic. Repositories that exceed either
limit enter `partial` state and persist indexed file/byte counts plus a reason.
Review summaries must display that state and warn that findings may miss
skipped code; partial context must never be presented as a complete index.
