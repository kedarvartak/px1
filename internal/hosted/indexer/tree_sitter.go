package indexer

import (
	"context"
	"errors"

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
)

func NewParserRegistry() ParserRegistry {
	return ParserRegistry{Parsers: map[Language]TreeSitterParser{
		LanguageGo:         nativeParser{treesitter.NewLanguage(tsgo.Language())},
		LanguageJavaScript: nativeParser{treesitter.NewLanguage(tsjavascript.Language())},
		LanguageTypeScript: nativeParser{treesitter.NewLanguage(tstypescript.LanguageTypescript())},
		LanguageTSX:        nativeParser{treesitter.NewLanguage(tstypescript.LanguageTSX())},
		LanguagePython:     nativeParser{treesitter.NewLanguage(tspython.Language())},
		LanguageRuby:       nativeParser{treesitter.NewLanguage(tsruby.Language())},
		LanguageRust:       nativeParser{treesitter.NewLanguage(tsrust.Language())},
		LanguageJava:       nativeParser{treesitter.NewLanguage(tsjava.Language())},
		LanguageC:          nativeParser{treesitter.NewLanguage(tsc.Language())},
		LanguageCPP:        nativeParser{treesitter.NewLanguage(tscpp.Language())},
		LanguageCSharp:     nativeParser{treesitter.NewLanguage(tscsharp.Language())},
		LanguagePHP:        nativeParser{treesitter.NewLanguage(tsphp.LanguagePHP())},
	}}
}

type nativeParser struct{ language *treesitter.Language }

func (p nativeParser) Parse(ctx context.Context, content []byte) ([]SyntaxNode, error) {
	parser := treesitter.NewParser()
	defer parser.Close()
	if err := parser.SetLanguage(p.language); err != nil {
		return nil, err
	}
	tree := parser.ParseCtx(ctx, content, nil)
	if tree == nil {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		return nil, errors.New("tree-sitter returned no syntax tree")
	}
	defer tree.Close()
	root := tree.RootNode()
	nodes := make([]SyntaxNode, 0, root.NamedChildCount())
	collectDeclarations(root, content, &nodes)
	if len(nodes) == 0 {
		for index := uint(0); index < root.NamedChildCount(); index++ {
			appendSyntaxNode(root.NamedChild(index), "", &nodes)
		}
	}
	return nodes, nil
}

func collectDeclarations(node *treesitter.Node, content []byte, nodes *[]SyntaxNode) {
	for index := uint(0); index < node.NamedChildCount(); index++ {
		child := node.NamedChild(index)
		if nameNode := child.ChildByFieldName("name"); nameNode != nil {
			start, end := int(nameNode.StartByte()), int(nameNode.EndByte())
			name := ""
			if start >= 0 && end >= start && end <= len(content) && end-start <= 200 {
				name = string(content[start:end])
			}
			appendSyntaxNode(child, name, nodes)
			continue
		}
		collectDeclarations(child, content, nodes)
	}
}

func appendSyntaxNode(node *treesitter.Node, name string, nodes *[]SyntaxNode) {
	if node == nil || node.EndByte() <= node.StartByte() {
		return
	}
	*nodes = append(*nodes, SyntaxNode{
		Kind: node.Kind(), Name: name, StartByte: int(node.StartByte()), EndByte: int(node.EndByte()),
		StartLine: int(node.StartPosition().Row) + 1, EndLine: int(node.EndPosition().Row) + 1,
	})
}
