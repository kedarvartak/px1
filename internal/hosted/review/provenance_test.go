package review

import (
	"strings"
	"testing"
)

func TestFindingProvenanceRendersAuditableInputs(t *testing.T) {
	p := FindingProvenance{
		FindingID: "f-1", Verifier: "kept", Reason: "the evidence supports the finding",
		Contexts:  []ProvenanceContext{{Path: "api.go", StartLine: 10, EndLine: 12, Excerpt: "return nil"}},
		GraphPath: []ProvenanceGraphHop{{From: "handler", Kind: "calls", To: "store"}},
		Memories:  []ProvenanceMemory{{ID: "m-1", Text: "Keep writes in services", Evidence: "https://github.com/acme/repo/pull/1#discussion_r1"}},
	}
	body, err := RenderWhyComment(p)
	if err != nil || !strings.Contains(body, "Context used") || !strings.Contains(body, "Graph path") || !strings.Contains(body, "Memories used") {
		t.Fatalf("body = %q, %v", body, err)
	}
	link, err := WhyCommentURL("https://app.px1.dev/r/acme/repo/sha", p.FindingID)
	if err != nil || link != "https://app.px1.dev/r/acme/repo/sha#why-f-1" {
		t.Fatalf("link = %q, %v", link, err)
	}
}

func TestFindingProvenanceRejectsUnverifiableMemory(t *testing.T) {
	p := FindingProvenance{FindingID: "f", Verifier: "kept", Reason: "yes", Memories: []ProvenanceMemory{{ID: "m", Text: "rule", Evidence: "javascript:alert(1)"}}}
	if p.Validate() == nil {
		t.Fatal("accepted unsafe memory evidence")
	}
}
