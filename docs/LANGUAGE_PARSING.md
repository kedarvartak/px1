# Language parsing boundary

The hosted indexer detects Go, JavaScript, TypeScript, TSX, Python, Ruby, Rust,
Java, Kotlin, C#, C++, and PHP from paths, with a small shebang fallback.
Tree-sitter grammar adapters register behind `TreeSitterParser`; parser errors,
unregistered grammars, and unknown text formats fall back to explicit bounded
line windows. Inputs over one MiB and binary content are rejected before a
parser sees them.

Parsing extracts data only. It does not load repository plugins, compilers,
language servers, package managers, or other repository-controlled programs.
