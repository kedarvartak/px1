package review

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestDecodeFindingsLenientKeepsGoodFindingsAndExplainsDrops(t *testing.T) {
	good := `{"path":"./api/users.go","start_line":4,"end_line":4,"severity":"p1","type":"Logic","title":"Ignored error","body":"b","evidence":"e","confidence":0.8,"model_notes":"extra field"}`
	input := `[` + good + `,
		{"path":"/etc/passwd","start_line":1,"end_line":1,"severity":"P1","type":"logic","title":"Escape","body":"b","evidence":"e","confidence":0.9},
		{"path":"a.go","start_line":9,"end_line":2,"severity":"P1","type":"logic","title":"Bad range","body":"b","evidence":"e","confidence":0.9},
		{"path":"a.go","start_line":1,"end_line":1,"severity":"P3","type":"logic","title":"Bad severity","body":"b","evidence":"e","confidence":0.9},
		{"path":"a.go","start_line":1,"end_line":1,"severity":"P2","type":"logic","title":"No evidence","body":"b","evidence":"","confidence":0.9},
		{"path":"a.go","start_line":1,"end_line":1,"severity":"P2","type":"logic","title":"Over confident","body":"b","evidence":"e","confidence":87},
		"not an object"]`
	kept, dropped, err := DecodeFindingsLenient(json.RawMessage(input), 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(kept) != 1 || kept[0].Path != "api/users.go" || kept[0].Severity != "P1" || kept[0].Type != "logic" {
		t.Fatalf("kept = %#v", kept)
	}
	if len(dropped) != 6 {
		t.Fatalf("dropped = %#v", dropped)
	}
	reasons := ""
	for _, d := range dropped {
		reasons += d.Reason + ";"
	}
	for _, want := range []string{"repository-relative", "line range", "severity", "evidence", "confidence", "not a finding object"} {
		if !strings.Contains(reasons, want) {
			t.Fatalf("no drop reason mentions %q: %s", want, reasons)
		}
	}
	if dropped[0].Title != "Escape" || dropped[0].Index != 1 {
		t.Fatalf("drop should identify the finding: %#v", dropped[0])
	}
}

func TestDecodeFindingsLenientCapsAndRequiresAnArray(t *testing.T) {
	one := `{"path":"a.go","start_line":1,"end_line":1,"severity":"P2","type":"style","title":"t","body":"b","evidence":"e","confidence":0.7}`
	kept, dropped, err := DecodeFindingsLenient(json.RawMessage(`[`+one+`,`+one+`,`+one+`]`), 2)
	if err != nil || len(kept) != 2 || len(dropped) != 1 || !strings.Contains(dropped[0].Reason, "more than 2") {
		t.Fatalf("kept=%d dropped=%#v err=%v", len(kept), dropped, err)
	}
	for _, bad := range []string{`{}`, `"x"`, `null`, ``} {
		if _, _, err := DecodeFindingsLenient(json.RawMessage(bad), 5); err == nil && bad != `null` {
			t.Fatalf("%q accepted", bad)
		}
	}
	if kept, _, err := DecodeFindingsLenient(json.RawMessage(`[]`), 5); err != nil || kept == nil || len(kept) != 0 {
		t.Fatal("an empty array must decode to an empty, non-nil list")
	}
}
