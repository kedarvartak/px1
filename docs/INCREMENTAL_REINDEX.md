# Incremental repository indexing

Default-branch push jobs compare the current path-to-blob-SHA inventory with the
last indexed inventory. Only new or changed blobs are re-parsed, re-chunked and
re-embedded; deleted paths are removed, and identical blobs are untouched. The
store applies file/chunk mutations and the new indexed head in one transaction,
so a retry cannot expose a head SHA with only part of its mutations.
