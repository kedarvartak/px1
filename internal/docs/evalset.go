package docs

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"path"
	"regexp"
	"strings"
)

// An evaluation set for documentation updates. Each case is a small invented
// repository: one page, the files it covers before and after a change, and
// what a correct update does. A "stale" case makes a statement on the page
// false; a "clean" case changes code without touching anything the page
// says, so the only correct update is none.

// EvalSetVersion is the version of the case file format.
const EvalSetVersion = 1

// Case kinds.
const (
	EvalStale = "stale"
	EvalClean = "clean"
)

// EvalFile is one covered file before and after the change. An empty Before
// means the change adds the file; an empty After means it removes it.
type EvalFile struct {
	Before string `json:"before"`
	After  string `json:"after"`
}

// EvalExpect says what a correct update of a stale case looks like.
type EvalExpect struct {
	// Section is the heading of the section that must be edited.
	Section string `json:"section"`
	// MustContain are strings the section must hold after the update.
	MustContain []string `json:"must_contain"`
	// MustNotContain are strings the section must no longer hold.
	MustNotContain []string `json:"must_not_contain"`
}

// EvalCase is one case of the set.
type EvalCase struct {
	ID   string `json:"id"`
	Kind string `json:"kind"`
	// What says, for a person, what the change is and why the page is or is
	// not affected.
	What string `json:"what"`
	// Page is the Markdown of the page, without front matter; it is stored
	// at docs/PAGE.md and covers every file of the case.
	Page   string              `json:"page"`
	Files  map[string]EvalFile `json:"files"`
	Expect *EvalExpect         `json:"expect,omitempty"`
}

// EvalSet is a case file.
type EvalSet struct {
	Version int        `json:"version"`
	Cases   []EvalCase `json:"cases"`
}

// EvalPagePath is where a case's page is stored in its repository.
const EvalPagePath = Dir + "/PAGE.md"

var evalIDRE = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{1,60}$`)

// PageText returns the case's page with front matter that covers its files.
func (c EvalCase) PageText() string {
	var b strings.Builder
	b.WriteString("---\ntitle: Evaluation page\nsection: guides\nkind: narrative\nstatus: shipped\ncovers:\n")
	for _, name := range sortedKeys(c.Files) {
		b.WriteString("  - " + name + "\n")
	}
	b.WriteString("verified_at:\n---\n\n" + strings.TrimSpace(c.Page) + "\n")
	return b.String()
}

func sortedKeys(files map[string]EvalFile) []string {
	names := make([]string, 0, len(files))
	for name := range files {
		names = append(names, name)
	}
	// Insertion sort: the sets are tiny and this avoids another import.
	for i := 1; i < len(names); i++ {
		for j := i; j > 0 && names[j] < names[j-1]; j-- {
			names[j], names[j-1] = names[j-1], names[j]
		}
	}
	return names
}

// ParseEvalSet decodes and validates a case file. A case is rejected when it
// could not tell a good update from a bad one: for example a stale case whose
// forbidden text is not on the page to begin with.
func ParseEvalSet(data []byte) (EvalSet, error) {
	var set EvalSet
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&set); err != nil {
		return set, err
	}
	if set.Version != EvalSetVersion {
		return set, fmt.Errorf("version is %d; this px1 reads version %d", set.Version, EvalSetVersion)
	}
	if len(set.Cases) == 0 {
		return set, errors.New("the set has no cases")
	}
	seen := map[string]bool{}
	for _, c := range set.Cases {
		if err := validateEvalCase(c); err != nil {
			return set, fmt.Errorf("case %q: %w", c.ID, err)
		}
		if seen[c.ID] {
			return set, fmt.Errorf("case %q appears twice", c.ID)
		}
		seen[c.ID] = true
	}
	return set, nil
}

func validateEvalCase(c EvalCase) error {
	switch {
	case !evalIDRE.MatchString(c.ID):
		return errors.New("id must be lower-case letters, digits and hyphens")
	case c.Kind != EvalStale && c.Kind != EvalClean:
		return errors.New("kind must be stale or clean")
	case strings.TrimSpace(c.What) == "":
		return errors.New("what is empty")
	case len(c.Files) == 0:
		return errors.New("the case changes no file")
	}
	for name, file := range c.Files {
		// Names are slash paths. A backslash or a colon is refused so that
		// no name means something else on Windows (a separator, a drive).
		clean := path.Clean(name)
		if name == "" || clean != name || path.IsAbs(name) || strings.HasPrefix(name, "..") || strings.HasPrefix(name, Dir+"/") || strings.HasPrefix(name, ".") || strings.ContainsAny(name, "\\:") {
			return fmt.Errorf("%q is not a plain path outside docs/", name)
		}
		if file.Before == file.After {
			return fmt.Errorf("%s is the same before and after", name)
		}
	}
	page, problems := parsePage(EvalPagePath, c.PageText())
	if len(problems) > 0 {
		return errors.New(problems[0].Message)
	}
	sections := page.Sections()
	if len(sections) == 0 {
		return errors.New("the page has no heading")
	}
	if c.Kind == EvalClean {
		if c.Expect != nil {
			return errors.New("a clean case expects no edit, so it has no expect")
		}
		return nil
	}
	if c.Expect == nil {
		return errors.New("a stale case needs expect")
	}
	text, found := "", 0
	for _, section := range sections {
		if section.Heading == c.Expect.Section {
			text = section.Text
			found++
		}
	}
	if found != 1 {
		return fmt.Errorf("expect.section %q names %d sections of the page", c.Expect.Section, found)
	}
	if len(c.Expect.MustContain)+len(c.Expect.MustNotContain) == 0 {
		return errors.New("expect has nothing to check")
	}
	for _, want := range c.Expect.MustContain {
		if want == "" || strings.Contains(text, want) {
			return fmt.Errorf("must_contain %q is empty or already in the section", want)
		}
	}
	for _, banned := range c.Expect.MustNotContain {
		if banned == "" || !strings.Contains(text, banned) {
			return fmt.Errorf("must_not_contain %q is empty or not in the section to begin with", banned)
		}
	}
	return nil
}

// EvalOutcome is how one case went.
type EvalOutcome struct {
	ID   string `json:"id"`
	Kind string `json:"kind"`
	Pass bool   `json:"pass"`
	// Detail says why a case failed.
	Detail string `json:"detail,omitempty"`
	// Applied and Refused count the edits drafted for the case. Elsewhere is
	// how many applied edits were to a section other than the expected one.
	Applied      int   `json:"applied"`
	Refused      int   `json:"refused"`
	Elsewhere    int   `json:"elsewhere"`
	CostMicrousd int64 `json:"cost_microusd"`
}

// ScoreEvalCase judges one case from the page as it is after the update and
// the edit results. A stale case passes when the expected section was edited
// and holds what it must and none of what it must not. A clean case passes
// only when nothing was applied.
func ScoreEvalCase(c EvalCase, pageAfter string, results []EditResult) EvalOutcome {
	outcome := EvalOutcome{ID: c.ID, Kind: c.Kind}
	edited := false
	for _, result := range results {
		switch {
		case !result.Applied:
			outcome.Refused++
		case c.Expect != nil && result.Edit.Heading == c.Expect.Section:
			outcome.Applied++
			edited = true
		default:
			outcome.Applied++
			outcome.Elsewhere++
		}
	}
	if c.Kind == EvalClean {
		outcome.Pass = outcome.Applied == 0
		if !outcome.Pass {
			outcome.Detail = fmt.Sprintf("%d edit(s) applied to a page the change does not affect", outcome.Applied)
		}
		return outcome
	}
	if !edited {
		outcome.Detail = "the stale section was not edited"
		return outcome
	}
	page, _ := parsePage(EvalPagePath, pageAfter)
	text := SectionText(page, c.Expect.Section)
	for _, want := range c.Expect.MustContain {
		if !strings.Contains(text, want) {
			outcome.Detail = fmt.Sprintf("the section does not say %q", want)
			return outcome
		}
	}
	for _, banned := range c.Expect.MustNotContain {
		if strings.Contains(text, banned) {
			outcome.Detail = fmt.Sprintf("the section still says %q", banned)
			return outcome
		}
	}
	outcome.Pass = true
	return outcome
}

// EvalSummary is the score of a run over a set.
type EvalSummary struct {
	Stale, StaleFixed int
	Clean, CleanQuiet int
	// Elsewhere counts applied edits to sections that were not stale.
	Elsewhere    int
	CostMicrousd int64
}

// SummarizeEval totals the outcomes.
func SummarizeEval(outcomes []EvalOutcome) EvalSummary {
	var s EvalSummary
	for _, o := range outcomes {
		s.Elsewhere += o.Elsewhere
		s.CostMicrousd += o.CostMicrousd
		switch o.Kind {
		case EvalStale:
			s.Stale++
			if o.Pass {
				s.StaleFixed++
			}
		case EvalClean:
			s.Clean++
			if o.Pass {
				s.CleanQuiet++
			}
		}
	}
	return s
}

// Rate is n of d as a fraction; 1 when there is nothing to count.
func Rate(n, d int) float64 {
	if d == 0 {
		return 1
	}
	return float64(n) / float64(d)
}
