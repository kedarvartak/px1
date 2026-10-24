package indexer

import (
	"errors"
	"fmt"
	"sort"
	"strings"
	"unicode"
	"unicode/utf8"
)

type ChunkSymbol struct {
	Name      string
	StartByte int
	EndByte   int
}

type TokenCounter interface {
	Count(string) int
}

type Chunk struct {
	Path       string
	Symbol     string
	StartLine  int
	EndLine    int
	Content    string
	TokenCount int
}

func (c Chunk) EmbeddingText() string {
	header := "path: " + c.Path
	if c.Symbol != "" {
		header += "\nsymbol: " + c.Symbol
	}
	return header + "\n\n" + c.Content
}

type Chunker struct {
	Counter   TokenCounter
	MinTokens int
	MaxTokens int
}

func (c Chunker) Chunk(path string, content []byte, symbols []ChunkSymbol) ([]Chunk, error) {
	if !utf8.Valid(content) {
		return nil, errors.New("chunk input must be UTF-8 text")
	}
	minimum, maximum := c.MinTokens, c.MaxTokens
	if minimum <= 0 {
		minimum = 200
	}
	if maximum <= 0 {
		maximum = 800
	}
	if minimum > maximum {
		return nil, errors.New("minimum chunk size exceeds maximum")
	}
	counter := c.Counter
	if counter == nil {
		counter = approximateCounter{}
	}
	units, err := symbolUnits(content, symbols)
	if err != nil {
		return nil, err
	}
	var pieces []piece
	for _, unit := range units {
		pieces = append(pieces, splitPiece(content, unit, maximum, counter)...)
	}
	var chunks []Chunk
	for i := 0; i < len(pieces); {
		start := i
		tokens := 0
		for i < len(pieces) && tokens+pieces[i].tokens <= maximum {
			tokens += pieces[i].tokens
			i++
			if tokens >= minimum {
				break
			}
		}
		if i == start {
			return nil, errors.New("unable to produce a bounded chunk")
		}
		firstPiece, lastPiece := pieces[start], pieces[i-1]
		chunkContent := string(content[firstPiece.start:lastPiece.end])
		chunks = append(chunks, Chunk{
			Path: path, Symbol: joinedSymbols(pieces[start:i]),
			StartLine: lineAt(content, firstPiece.start), EndLine: lineAt(content, max(firstPiece.start, lastPiece.end-1)),
			Content: chunkContent, TokenCount: counter.Count(chunkContent),
		})
	}
	return chunks, nil
}

type byteUnit struct {
	start  int
	end    int
	symbol string
}

type piece struct {
	byteUnit
	tokens int
}

func symbolUnits(content []byte, symbols []ChunkSymbol) ([]byteUnit, error) {
	sorted := append([]ChunkSymbol(nil), symbols...)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].StartByte < sorted[j].StartByte })
	units := make([]byteUnit, 0, len(sorted)*2+1)
	cursor := 0
	for _, symbol := range sorted {
		if symbol.StartByte < cursor || symbol.StartByte < 0 || symbol.EndByte <= symbol.StartByte || symbol.EndByte > len(content) {
			return nil, fmt.Errorf("invalid or overlapping symbol %q", symbol.Name)
		}
		if symbol.StartByte > cursor {
			units = append(units, byteUnit{start: cursor, end: symbol.StartByte})
		}
		units = append(units, byteUnit{start: symbol.StartByte, end: symbol.EndByte, symbol: symbol.Name})
		cursor = symbol.EndByte
	}
	if cursor < len(content) {
		units = append(units, byteUnit{start: cursor, end: len(content)})
	}
	if len(units) == 0 && len(content) > 0 {
		units = append(units, byteUnit{end: len(content)})
	}
	return units, nil
}

func splitPiece(content []byte, unit byteUnit, maximum int, counter TokenCounter) []piece {
	if tokens := counter.Count(string(content[unit.start:unit.end])); tokens <= maximum {
		return []piece{{byteUnit: unit, tokens: tokens}}
	}
	var pieces []piece
	start := unit.start
	for start < unit.end {
		end := boundedEnd(content, start, unit.end, maximum, counter)
		pieces = append(pieces, piece{byteUnit: byteUnit{start: start, end: end, symbol: unit.symbol}, tokens: counter.Count(string(content[start:end]))})
		start = end
	}
	return pieces
}

func boundedEnd(content []byte, start, limit, maximum int, counter TokenCounter) int {
	low, high := start+1, limit
	best := start
	for low <= high {
		middle := low + (high-low)/2
		for middle < limit && middle > start && !utf8.RuneStart(content[middle]) {
			middle--
		}
		if middle == start {
			middle = start + 1
			for middle < limit && !utf8.RuneStart(content[middle]) {
				middle++
			}
		}
		if counter.Count(string(content[start:middle])) <= maximum {
			best = middle
			low = middle + 1
		} else {
			high = middle - 1
		}
	}
	if best <= start {
		_, size := utf8.DecodeRune(content[start:limit])
		return start + size
	}
	if newline := strings.LastIndexByte(string(content[start:best]), '\n'); newline >= 0 {
		candidate := start + newline + 1
		if counter.Count(string(content[start:candidate])) >= maximum/2 {
			return candidate
		}
	}
	return best
}

func joinedSymbols(pieces []piece) string {
	var names []string
	for _, item := range pieces {
		if item.symbol != "" && (len(names) == 0 || names[len(names)-1] != item.symbol) {
			names = append(names, item.symbol)
		}
	}
	return strings.Join(names, ", ")
}

func lineAt(content []byte, offset int) int {
	return 1 + strings.Count(string(content[:offset]), "\n")
}

type approximateCounter struct{}

func (approximateCounter) Count(text string) int {
	return len(strings.FieldsFunc(text, func(r rune) bool { return unicode.IsSpace(r) || unicode.IsPunct(r) }))
}
