# Language parsing boundary

The hosted indexer registers native tree-sitter grammars for Go, JavaScript,
TypeScript, TSX, Python, Ruby, Rust, Java, C, C++, C#, and PHP, with a small
shebang fallback. Parser errors, unregistered grammars, and unknown text formats
fall back to explicit bounded line windows. Inputs over one MiB and binary
content are rejected before a parser sees them.

Parsing extracts data only. It does not load repository plugins, compilers,
language servers, package managers, or other repository-controlled programs.
