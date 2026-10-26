package graph

import (
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

const (
	MaxDiagramSymbols = 50
	MaxDiagramEdges   = 100
)

type DiagramKind string

const (
	DiagramSequence DiagramKind = "sequence"
	DiagramER       DiagramKind = "er"
	DiagramClass    DiagramKind = "class"
	DiagramFlow     DiagramKind = "flow"
)

type DiagramEdge struct {
	Source ToolSymbol
	Target ToolSymbol
	Kind   string
}

type Diagram struct {
	Kind          DiagramKind
	Mermaid       string
	SkippedReason string
}

var mermaidLine = regexp.MustCompile(`^[A-Za-z0-9_ ()\[\]{}|:;.,+*/<>-]+$`)

// GenerateDiagram emits Mermaid only for graph slices large enough to help a
// reviewer. Labels are quoted and identifiers are generated, never copied from
// repository source.
func GenerateDiagram(kind DiagramKind, edges []DiagramEdge) (Diagram, error) {
	if !map[DiagramKind]bool{DiagramSequence: true, DiagramER: true, DiagramClass: true, DiagramFlow: true}[kind] {
		return Diagram{}, errors.New("unsupported diagram kind")
	}
	if len(edges) > MaxDiagramEdges {
		return Diagram{}, errors.New("diagram edge limit exceeded")
	}
	symbols, paths := map[int64]ToolSymbol{}, map[string]bool{}
	for _, edge := range edges {
		if edge.Source.ID <= 0 || edge.Target.ID <= 0 || strings.TrimSpace(edge.Source.Name) == "" || strings.TrimSpace(edge.Target.Name) == "" {
			return Diagram{}, errors.New("diagram edges require named symbols")
		}
		symbols[edge.Source.ID], symbols[edge.Target.ID] = edge.Source, edge.Target
		paths[edge.Source.Path], paths[edge.Target.Path] = true, true
	}
	if len(symbols) < 3 || len(paths) < 2 {
		return Diagram{Kind: kind, SkippedReason: "diagram requires at least three symbols across two files"}, nil
	}
	if len(symbols) > MaxDiagramSymbols {
		return Diagram{}, errors.New("diagram symbol limit exceeded")
	}
	sort.Slice(edges, func(i, j int) bool {
		if edges[i].Source.ID != edges[j].Source.ID {
			return edges[i].Source.ID < edges[j].Source.ID
		}
		if edges[i].Target.ID != edges[j].Target.ID {
			return edges[i].Target.ID < edges[j].Target.ID
		}
		return edges[i].Kind < edges[j].Kind
	})
	var body strings.Builder
	switch kind {
	case DiagramSequence:
		body.WriteString("sequenceDiagram\n")
		ids := make([]int64, 0, len(symbols))
		for id := range symbols {
			ids = append(ids, id)
		}
		sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
		for _, id := range ids {
			fmt.Fprintf(&body, "  participant %s as %s\n", diagramID(id), safeDiagramLabel(symbols[id].Name))
		}
	case DiagramER:
		body.WriteString("erDiagram\n")
	case DiagramClass:
		body.WriteString("classDiagram\n")
	default:
		body.WriteString("flowchart LR\n")
	}
	for _, edge := range edges {
		source, target := diagramID(edge.Source.ID), diagramID(edge.Target.ID)
		label := safeDiagramLabel(edge.Kind)
		switch kind {
		case DiagramSequence:
			fmt.Fprintf(&body, "  %s->>%s: %s\n", source, target, label)
		case DiagramER:
			fmt.Fprintf(&body, "  %s ||--o{ %s : %s\n", source, target, label)
		case DiagramClass:
			fmt.Fprintf(&body, "  %s --> %s : %s\n", source, target, label)
		default:
			fmt.Fprintf(&body, "  %s[%s] -->|%s| %s[%s]\n", source, safeDiagramLabel(edge.Source.Name), label, target, safeDiagramLabel(edge.Target.Name))
		}
	}
	result := body.String()
	if err := ValidateMermaid(result); err != nil {
		return Diagram{Kind: kind, SkippedReason: "generated Mermaid failed validation"}, nil
	}
	return Diagram{Kind: kind, Mermaid: result}, nil
}

func ValidateMermaid(value string) error {
	if len(value) == 0 || len(value) > 32_000 || strings.Contains(value, "%%") || strings.Contains(value, "`") || strings.Contains(value, "click ") {
		return errors.New("unsafe Mermaid source")
	}
	for _, line := range strings.Split(strings.TrimSuffix(value, "\n"), "\n") {
		if !mermaidLine.MatchString(line) {
			return errors.New("Mermaid contains unsupported characters")
		}
	}
	return nil
}

func diagramID(id int64) string { return "s" + strconv.FormatInt(id, 10) }

func safeDiagramLabel(value string) string {
	value = strings.Join(strings.Fields(value), " ")
	value = regexp.MustCompile(`[^A-Za-z0-9_ .+*/()-]`).ReplaceAllString(value, "")
	if value == "" {
		return "related"
	}
	if len(value) > 80 {
		value = value[:80]
	}
	return value
}
