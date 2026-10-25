package review

import (
	"context"
	"strings"
	"testing"

	"px1/internal/hosted/graph"
)

type blastReader struct{}

func (blastReader) Neighbors(_ context.Context, _, _ int64, ids []int64, direction graph.Direction, kinds []string) ([]graph.ToolEdge, error) {
	if direction == graph.Reverse && kinds[0] == "calls" {
		return []graph.ToolEdge{{Source: graph.ToolSymbol{ID: 2, Path: "caller.go", Name: "Call"}, Target: graph.ToolSymbol{ID: ids[0], Path: "changed.go", Name: "Changed"}}}, nil
	}
	if direction == graph.Reverse && kinds[0] == "tests" {
		return []graph.ToolEdge{{Source: graph.ToolSymbol{ID: 3, Path: "changed_test.go", Name: "TestChanged"}, Target: graph.ToolSymbol{ID: ids[0], Path: "changed.go", Name: "Changed"}}}, nil
	}
	return nil, nil
}

func TestBuildBlastRadiusAndRenderSummary(t *testing.T) {
	items, err := BuildBlastRadius(context.Background(), graph.Tools{Reader: blastReader{}}, 1, 2, []int64{1}, []string{"changed.go"})
	if err != nil || len(items) != 2 {
		t.Fatalf("items = %#v, %v", items, err)
	}
	body, err := RenderSummary(SummaryInput{HeadSHA: strings.Repeat("a", 40), ChangedFiles: 1, ReportURL: "https://px1.example/r/a", BlastRadius: items})
	if err != nil || !strings.Contains(body, "inspection pointers") || !strings.Contains(body, "caller.go") {
		t.Fatalf("summary = %q, %v", body, err)
	}
}
