// Package graph owns the language-aware tree-sitter queries used to build the
// hosted symbol graph. Query compilation is tested for every indexed language.
package graph

import (
	"embed"
	"fmt"
	"sort"

	treesitter "github.com/tree-sitter/go-tree-sitter"
	tscsharp "github.com/tree-sitter/tree-sitter-c-sharp/bindings/go"
	tsc "github.com/tree-sitter/tree-sitter-c/bindings/go"
	tscpp "github.com/tree-sitter/tree-sitter-cpp/bindings/go"
	tsgo "github.com/tree-sitter/tree-sitter-go/bindings/go"
	tsjava "github.com/tree-sitter/tree-sitter-java/bindings/go"
	tsjavascript "github.com/tree-sitter/tree-sitter-javascript/bindings/go"
	tsphp "github.com/tree-sitter/tree-sitter-php/bindings/go"
	tspython "github.com/tree-sitter/tree-sitter-python/bindings/go"
	tsruby "github.com/tree-sitter/tree-sitter-ruby/bindings/go"
	tsrust "github.com/tree-sitter/tree-sitter-rust/bindings/go"
	tstypescript "github.com/tree-sitter/tree-sitter-typescript/bindings/go"

	"px1/internal/hosted/indexer"
)

//go:embed queries/*.scm
var queryFiles embed.FS

var grammars = map[indexer.Language]*treesitter.Language{
	indexer.LanguageGo:         treesitter.NewLanguage(tsgo.Language()),
	indexer.LanguageJavaScript: treesitter.NewLanguage(tsjavascript.Language()),
	indexer.LanguageTypeScript: treesitter.NewLanguage(tstypescript.LanguageTypescript()),
	indexer.LanguageTSX:        treesitter.NewLanguage(tstypescript.LanguageTSX()),
	indexer.LanguagePython:     treesitter.NewLanguage(tspython.Language()),
	indexer.LanguageRuby:       treesitter.NewLanguage(tsruby.Language()),
	indexer.LanguageRust:       treesitter.NewLanguage(tsrust.Language()),
	indexer.LanguageJava:       treesitter.NewLanguage(tsjava.Language()),
	indexer.LanguageC:          treesitter.NewLanguage(tsc.Language()),
	indexer.LanguageCPP:        treesitter.NewLanguage(tscpp.Language()),
	indexer.LanguageCSharp:     treesitter.NewLanguage(tscsharp.Language()),
	indexer.LanguagePHP:        treesitter.NewLanguage(tsphp.LanguagePHP()),
}

func SupportedLanguages() []indexer.Language {
	languages := make([]indexer.Language, 0, len(grammars))
	for language := range grammars {
		languages = append(languages, language)
	}
	sort.Slice(languages, func(i, j int) bool { return languages[i] < languages[j] })
	return languages
}

func Source(language indexer.Language) (string, error) {
	if grammars[language] == nil {
		return "", fmt.Errorf("unsupported graph language %q", language)
	}
	data, err := queryFiles.ReadFile("queries/" + string(language) + ".scm")
	if err != nil {
		return "", fmt.Errorf("read %s graph query: %w", language, err)
	}
	return string(data), nil
}

func Compile(language indexer.Language) (*treesitter.Query, error) {
	source, err := Source(language)
	if err != nil {
		return nil, err
	}
	query, queryErr := treesitter.NewQuery(grammars[language], source)
	if queryErr != nil {
		return nil, fmt.Errorf("compile %s graph query: %w", language, queryErr)
	}
	return query, nil
}
