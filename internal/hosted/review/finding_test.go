package review

import "testing"

func TestDecodeFindingsAcceptsStrictAuditableShape(t *testing.T) {
	data := []byte(`[{"path":"src/a.go","start_line":4,"end_line":5,"severity":"P1","type":"logic","title":"Wrong branch","body":"The condition reverses behavior.","evidence":"Line 4 returns the error path.","suggestion":"return ok","confidence":0.8,"rule_id":"errors"}]`)
	findings, err := DecodeFindings(data, 15)
	if err != nil || len(findings) != 1 {
		t.Fatalf("findings=%+v err=%v", findings, err)
	}
}
func TestDecodeFindingsRejectsUnknownFieldsAndUnsafePaths(t *testing.T) {
	for _, data := range []string{
		`[{"path":"../secret","start_line":1,"end_line":1,"severity":"P1","type":"logic","title":"x","body":"x","evidence":"x","confidence":.8}]`,
		`[{"path":"a.go","start_line":1,"end_line":1,"severity":"P1","type":"logic","title":"x","body":"x","evidence":"x","confidence":.8,"verdict":"approve"}]`,
	} {
		if _, err := DecodeFindings([]byte(data), 15); err == nil {
			t.Fatalf("expected rejection for %s", data)
		}
	}
}
