package indexer

import (
	"bytes"
	"context"
	"errors"
	"path/filepath"
	"strings"
)

type Language string

const (
	LanguageUnknown    Language = "unknown"
	LanguageGo         Language = "go"
	LanguageJavaScript Language = "javascript"
	LanguageTypeScript Language = "typescript"
	LanguageTSX        Language = "tsx"
	LanguagePython     Language = "python"
	LanguageRuby       Language = "ruby"
	LanguageRust       Language = "rust"
	LanguageJava       Language = "java"
	LanguageC          Language = "c"
	LanguageCSharp     Language = "csharp"
	LanguageCPP        Language = "cpp"
	LanguagePHP        Language = "php"
)

var extensionLanguages = map[string]Language{
	".go": LanguageGo, ".js": LanguageJavaScript, ".jsx": LanguageJavaScript,
	".mjs": LanguageJavaScript, ".cjs": LanguageJavaScript, ".ts": LanguageTypeScript,
	".mts": LanguageTypeScript, ".cts": LanguageTypeScript, ".tsx": LanguageTSX,
	".py": LanguagePython, ".rb": LanguageRuby, ".rs": LanguageRust,
	".java": LanguageJava, ".c": LanguageC, ".h": LanguageC,
	".cs": LanguageCSharp, ".cc": LanguageCPP, ".cpp": LanguageCPP,
	".cxx": LanguageCPP, ".hh": LanguageCPP, ".hpp": LanguageCPP, ".php": LanguagePHP,
}

func DetectLanguage(path string, content []byte) Language {
	if language := extensionLanguages[strings.ToLower(filepath.Ext(path))]; language != "" {
		return language
	}
	name := strings.ToLower(filepath.Base(path))
	if name == "gemfile" || name == "rakefile" {
		return LanguageRuby
	}
	firstLine, _, _ := bytes.Cut(content, []byte("\n"))
	if bytes.HasPrefix(firstLine, []byte("#!")) {
		line := strings.ToLower(string(firstLine))
		for marker, language := range map[string]Language{
			"python": LanguagePython, "ruby": LanguageRuby, "node": LanguageJavaScript, "php": LanguagePHP,
		} {
			if strings.Contains(line, marker) {
				return language
			}
		}
	}
	return LanguageUnknown
}

type SyntaxNode struct {
	Kind      string
	Name      string
	StartByte int
	EndByte   int
	StartLine int
	EndLine   int
}

type TreeSitterParser interface {
	Parse(context.Context, []byte) ([]SyntaxNode, error)
}

type ParseResult struct {
	Language Language
	Mode     string
	Reason   string
	Nodes    []SyntaxNode
}

type ParserRegistry struct {
	Parsers     map[Language]TreeSitterParser
	MaxBytes    int
	WindowLines int
	MaxWindows  int
}

func (r ParserRegistry) Parse(ctx context.Context, path string, content []byte) (ParseResult, error) {
	maxBytes := r.MaxBytes
	if maxBytes <= 0 {
		maxBytes = 1 << 20
	}
	if len(content) > maxBytes {
		return ParseResult{}, errors.New("source exceeds parser byte limit")
	}
	if bytes.IndexByte(content, 0) >= 0 {
		return ParseResult{}, errors.New("binary content is not parsed")
	}
	language := DetectLanguage(path, content)
	parser := r.Parsers[language]
	if parser != nil {
		nodes, err := parser.Parse(ctx, content)
		if err == nil {
			return ParseResult{Language: language, Mode: "tree-sitter", Nodes: nodes}, nil
		}
		if ctx.Err() != nil {
			return ParseResult{}, ctx.Err()
		}
		return ParseResult{Language: language, Mode: "line-window", Reason: "tree-sitter parse failed", Nodes: r.lineWindows(content)}, nil
	}
	reason := "language is not supported"
	if language != LanguageUnknown {
		reason = "tree-sitter parser is not registered"
	}
	return ParseResult{Language: language, Mode: "line-window", Reason: reason, Nodes: r.lineWindows(content)}, nil
}

func (r ParserRegistry) lineWindows(content []byte) []SyntaxNode {
	if len(content) == 0 {
		return nil
	}
	window := r.WindowLines
	if window <= 0 {
		window = 200
	}
	limit := r.MaxWindows
	if limit <= 0 {
		limit = 500
	}
	lines := bytes.SplitAfter(content, []byte("\n"))
	nodes := make([]SyntaxNode, 0, min((len(lines)+window-1)/window, limit))
	offset := 0
	for start := 0; start < len(lines) && len(nodes) < limit; start += window {
		end := min(start+window, len(lines))
		length := 0
		for _, line := range lines[start:end] {
			length += len(line)
		}
		nodes = append(nodes, SyntaxNode{Kind: "line_window", StartByte: offset, EndByte: offset + length, StartLine: start + 1, EndLine: end})
		offset += length
	}
	return nodes
}
