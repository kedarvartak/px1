package indexer

import (
	"strings"
	"testing"
)

type wordCounter struct{}

func (wordCounter) Count(text string) int { return len(strings.Fields(text)) }

func TestChunkerUsesSymbolsAndPreservesAllContent(t *testing.T) {
	content := []byte("package demo\n\n" + strings.Repeat("alpha ", 6) + "\n" + strings.Repeat("beta ", 6))
	alphaStart := strings.Index(string(content), "alpha")
	betaStart := strings.Index(string(content), "beta")
	chunks, err := (Chunker{Counter: wordCounter{}, MinTokens: 4, MaxTokens: 8}).Chunk("demo.go", content, []ChunkSymbol{
		{Name: "Alpha", StartByte: alphaStart, EndByte: betaStart},
		{Name: "Beta", StartByte: betaStart, EndByte: len(content)},
	})
	if err != nil {
		t.Fatal(err)
	}
	var rebuilt strings.Builder
	for _, chunk := range chunks {
		if chunk.TokenCount > 8 {
			t.Fatalf("chunk exceeds token cap: %+v", chunk)
		}
		rebuilt.WriteString(chunk.Content)
		if !strings.Contains(chunk.EmbeddingText(), "path: demo.go") {
			t.Fatalf("embedding text lacks path: %q", chunk.EmbeddingText())
		}
	}
	if rebuilt.String() != string(content) {
		t.Fatalf("content was lost or reordered\nwant %q\n got %q", content, rebuilt.String())
	}
}

func TestChunkerSplitsOversizedSymbolWithoutDroppingIt(t *testing.T) {
	content := []byte(strings.Repeat("token ", 25))
	chunks, err := (Chunker{Counter: wordCounter{}, MinTokens: 5, MaxTokens: 10}).Chunk("big.go", content, []ChunkSymbol{{Name: "Big", EndByte: len(content)}})
	if err != nil {
		t.Fatal(err)
	}
	if len(chunks) < 3 {
		t.Fatalf("chunks = %d, want at least 3", len(chunks))
	}
	for _, chunk := range chunks {
		if chunk.TokenCount > 10 || chunk.Symbol != "Big" {
			t.Fatalf("unexpected chunk: %+v", chunk)
		}
	}
}

func TestChunkerRejectsOverlappingSymbols(t *testing.T) {
	_, err := (Chunker{}).Chunk("a.go", []byte("abcdef"), []ChunkSymbol{{Name: "a", EndByte: 4}, {Name: "b", StartByte: 3, EndByte: 6}})
	if err == nil {
		t.Fatal("expected overlap error")
	}
}
