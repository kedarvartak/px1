# Safe repository cloning

The hosted indexer clones only allow-listed HTTPS hosts into disposable
directories. Clones are bare, depth-one, single-branch, tag-free, and use
`--filter=blob:limit=1m`. Local and external Git transports, credential helpers,
system Git configuration, and terminal prompts are disabled.

Repository contents remain untrusted data. The indexer reads Git objects for
parsing and retrieval; it does not check out a worktree, run hooks, install
dependencies, invoke build tools, or execute any file from the repository.
