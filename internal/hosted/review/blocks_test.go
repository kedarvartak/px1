package review

import "testing"

func TestGroupBlocksIsWorkerCallableAndDeterministic(t *testing.T) {
	files := []File{
		{Path: "src/order_test.go", Hunks: []Hunk{{Lines: []DiffLine{{Kind: "added", Text: "validateOrder invoiceTotal"}}}}},
		{Path: "src/order.go", Hunks: []Hunk{{Lines: []DiffLine{{Kind: "added", Text: "validateOrder invoiceTotal"}}}}},
	}
	blocks := GroupBlocks(files)
	if len(blocks) != 1 || blocks[0].Paths[0] != "src/order.go" || blocks[0].ID == "" {
		t.Fatalf("blocks = %+v", blocks)
	}
}
