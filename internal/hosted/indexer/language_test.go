package indexer

import (
	"context"
	"errors"
	"testing"
)

type parserStub struct{ err error }

func (p parserStub) Parse(_ context.Context, content []byte) ([]SyntaxNode, error) {
	if p.err != nil {
		return nil, p.err
	}
	return []SyntaxNode{{Kind: "function", Name: "main", StartByte: 0, EndByte: len(content), StartLine: 1, EndLine: 1}}, nil
}

func TestDetectLanguageUsesExtensionAndShebang(t *testing.T) {
	for _, test := range []struct {
		path string
		body string
		want Language
	}{
		{"main.go", "package main", LanguageGo},
		{"component.tsx", "export const A = () => null", LanguageTSX},
		{"tool", "#!/usr/bin/env python3\n", LanguagePython},
		{"Gemfile", "source 'https://rubygems.org'", LanguageRuby},
	} {
		if got := DetectLanguage(test.path, []byte(test.body)); got != test.want {
			t.Errorf("DetectLanguage(%q) = %q, want %q", test.path, got, test.want)
		}
	}
}

func TestRegistryUsesParserAndExplicitFallback(t *testing.T) {
	registry := ParserRegistry{Parsers: map[Language]TreeSitterParser{LanguageGo: parserStub{}}}
	parsed, err := registry.Parse(context.Background(), "main.go", []byte("package main"))
	if err != nil || parsed.Mode != "tree-sitter" || len(parsed.Nodes) != 1 {
		t.Fatalf("parsed = %+v, err = %v", parsed, err)
	}

	registry.Parsers[LanguageGo] = parserStub{err: errors.New("bad tree")}
	fallback, err := registry.Parse(context.Background(), "main.go", []byte("package main"))
	if err != nil || fallback.Mode != "line-window" || fallback.Reason == "" {
		t.Fatalf("fallback = %+v, err = %v", fallback, err)
	}
}

func TestRegistryBoundsInputAndFallbackWindows(t *testing.T) {
	registry := ParserRegistry{MaxBytes: 4, WindowLines: 2, MaxWindows: 2}
	if _, err := registry.Parse(context.Background(), "a.py", []byte("12345")); err == nil {
		t.Fatal("expected byte limit error")
	}
	registry.MaxBytes = 100
	parsed, err := registry.Parse(context.Background(), "notes.txt", []byte("1\n2\n3\n4\n5\n"))
	if err != nil || len(parsed.Nodes) != 2 {
		t.Fatalf("parsed = %+v, err = %v", parsed, err)
	}
}
