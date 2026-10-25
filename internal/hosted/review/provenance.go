package review

import (
	"errors"
	"fmt"
	"net/url"
	"path/filepath"
	"strings"
)

const (
	MaxProvenanceContexts  = 8
	MaxProvenanceGraphHops = 8
	MaxProvenanceMemories  = 20
)

type ProvenanceContext struct {
	Path      string `json:"path"`
	StartLine int    `json:"start_line"`
	EndLine   int    `json:"end_line"`
	Excerpt   string `json:"excerpt"`
}

type ProvenanceGraphHop struct {
	From string `json:"from"`
	Kind string `json:"kind"`
	To   string `json:"to"`
}

type ProvenanceMemory struct {
	ID       string `json:"id"`
	Text     string `json:"text"`
	Evidence string `json:"evidence"`
}

type FindingProvenance struct {
	FindingID string               `json:"finding_id"`
	Contexts  []ProvenanceContext  `json:"contexts"`
	GraphPath []ProvenanceGraphHop `json:"graph_path"`
	Memories  []ProvenanceMemory   `json:"memories"`
	Verifier  string               `json:"verifier"`
	Reason    string               `json:"reason"`
}

// Validate ensures report provenance remains bounded and contains only
// repository-relative paths and auditable memory evidence.
func (p FindingProvenance) Validate() error {
	if strings.TrimSpace(p.FindingID) == "" || len(p.FindingID) > 200 || !map[string]bool{"kept": true, "dropped": true}[p.Verifier] || strings.TrimSpace(p.Reason) == "" || len(p.Reason) > 4000 {
		return errors.New("provenance requires finding, verifier disposition, and reason")
	}
	if len(p.Contexts) > MaxProvenanceContexts || len(p.GraphPath) > MaxProvenanceGraphHops || len(p.Memories) > MaxProvenanceMemories {
		return errors.New("provenance exceeds report limits")
	}
	for _, context := range p.Contexts {
		clean := filepath.ToSlash(filepath.Clean(context.Path))
		if context.Path == "" || filepath.IsAbs(context.Path) || clean == ".." || strings.HasPrefix(clean, "../") || context.StartLine <= 0 || context.EndLine < context.StartLine || strings.TrimSpace(context.Excerpt) == "" || len(context.Excerpt) > 8000 {
			return errors.New("invalid provenance context")
		}
	}
	for _, hop := range p.GraphPath {
		if strings.TrimSpace(hop.From) == "" || strings.TrimSpace(hop.Kind) == "" || strings.TrimSpace(hop.To) == "" || len(hop.From) > 500 || len(hop.Kind) > 80 || len(hop.To) > 500 {
			return errors.New("invalid provenance graph hop")
		}
	}
	for _, memory := range p.Memories {
		evidence, err := url.Parse(memory.Evidence)
		if memory.ID == "" || len(memory.ID) > 200 || strings.TrimSpace(memory.Text) == "" || len(memory.Text) > 4000 || err != nil || evidence.Scheme != "https" || !strings.EqualFold(evidence.Host, "github.com") {
			return errors.New("provenance memory requires GitHub evidence")
		}
	}
	return nil
}

func WhyCommentURL(reportURL, findingID string) (string, error) {
	parsed, err := url.Parse(reportURL)
	if err != nil || parsed.Scheme != "https" || parsed.Host == "" || strings.TrimSpace(findingID) == "" {
		return "", errors.New("why-comment link requires HTTPS report and finding id")
	}
	parsed.Fragment = "why-" + url.PathEscape(findingID)
	return parsed.String(), nil
}

func RenderWhyComment(p FindingProvenance) (string, error) {
	if err := p.Validate(); err != nil {
		return "", err
	}
	var body strings.Builder
	fmt.Fprintf(&body, "## Why this comment?\n\n**Verifier:** %s — %s\n", p.Verifier, p.Reason)
	if len(p.Contexts) > 0 {
		body.WriteString("\n### Context used\n")
		for _, context := range p.Contexts {
			fmt.Fprintf(&body, "- `%s:%d-%d`\n", context.Path, context.StartLine, context.EndLine)
		}
	}
	if len(p.GraphPath) > 0 {
		body.WriteString("\n### Graph path\n")
		for _, hop := range p.GraphPath {
			fmt.Fprintf(&body, "- %s —%s→ %s\n", hop.From, hop.Kind, hop.To)
		}
	}
	if len(p.Memories) > 0 {
		body.WriteString("\n### Memories used\n")
		for _, memory := range p.Memories {
			fmt.Fprintf(&body, "- %s ([evidence](%s))\n", memory.Text, memory.Evidence)
		}
	}
	return body.String(), nil
}
