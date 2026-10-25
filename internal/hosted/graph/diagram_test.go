package graph

import (
	"strings"
	"testing"
)

func TestGenerateDiagramRequiresUsefulGraphSlice(t *testing.T) {
	edges := []DiagramEdge{
		{Source: ToolSymbol{ID: 1, Name: "handler", Path: "api.go"}, Target: ToolSymbol{ID: 2, Name: "service", Path: "service.go"}, Kind: "calls"},
		{Source: ToolSymbol{ID: 2, Name: "service", Path: "service.go"}, Target: ToolSymbol{ID: 3, Name: "store", Path: "store.go"}, Kind: "calls"},
	}
	diagram, err := GenerateDiagram(DiagramSequence, edges)
	if err != nil || !strings.HasPrefix(diagram.Mermaid, "sequenceDiagram") || diagram.SkippedReason != "" {
		t.Fatalf("diagram = %#v, %v", diagram, err)
	}
}

func TestGenerateDiagramDropsSmallOrUnsafeOutput(t *testing.T) {
	diagram, err := GenerateDiagram(DiagramFlow, []DiagramEdge{{Source: ToolSymbol{ID: 1, Name: "a", Path: "a.go"}, Target: ToolSymbol{ID: 2, Name: "b", Path: "b.go"}, Kind: "calls"}})
	if err != nil || diagram.Mermaid != "" || diagram.SkippedReason == "" {
		t.Fatalf("small diagram = %#v, %v", diagram, err)
	}
	if ValidateMermaid("flowchart LR\n  a-->b\n%%{init: {}}%%") == nil {
		t.Fatal("unsafe Mermaid directive was accepted")
	}
}
