# Sandboxed SCIP indexing

SCIP supplements tree-sitter with exact cross-file references for configured
Go, TypeScript, Python, and Java indexers. Each indexer image must be pinned by
SHA-256 digest. Deployment configuration supplies the image and command so an
image update is an explicit reviewed change.

SCIP runs only against the trusted default-branch clone. Pull-request heads are
never passed to an executable indexer; their graph is parsed in memory. The
container runtime receives a read-only root filesystem, no network, and a
ten-minute CPU and wall-clock limit. Implementations must also mount the
repository read-only and expose only the designated index output path.

An indexer failure leaves tree-sitter data available and marks exact resolution
partial. It must not silently reuse an index generated for another commit.
