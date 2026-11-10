package eval

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"sort"
	"strings"
	"time"
)

// A repeat dataset is the small, local-first benchmark used to learn how a
// reviewer behaves on code you have access to, run several times per case.
// It differs from Dataset in three ways: the repository may be private (only
// commit identities are recorded, never source), a case may be "clean" (no
// bug; any finding is noise), and the same case is run more than once so
// run-to-run variance is measured instead of hidden.
type RepeatDataset struct {
	Version int          `json:"version"`
	Cases   []RepeatCase `json:"cases"`
}

type RepeatCase struct {
	ID         string  `json:"id"`
	Kind       string  `json:"kind"` // seeded, historical, or clean
	Repository string  `json:"repository,omitempty"`
	BaseSHA    string  `json:"base_sha"`
	HeadSHA    string  `json:"head_sha"`
	Labels     []Label `json:"labels,omitempty"`
	Notes      string  `json:"notes,omitempty"`
}

func DecodeRepeat(data []byte) (RepeatDataset, error) {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	var dataset RepeatDataset
	if err := decoder.Decode(&dataset); err != nil {
		return RepeatDataset{}, fmt.Errorf("decode eval cases: %w", err)
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		return RepeatDataset{}, errors.New("eval cases contain trailing data")
	}
	return dataset, dataset.Validate()
}

func (d RepeatDataset) Validate() error {
	if d.Version != 1 {
		return errors.New("eval cases version must be 1")
	}
	if len(d.Cases) == 0 {
		return errors.New("eval cases need at least one case")
	}
	seen := map[string]bool{}
	for i, c := range d.Cases {
		if c.ID == "" || seen[c.ID] {
			return fmt.Errorf("case %d has a missing or duplicate ID", i)
		}
		seen[c.ID] = true
		if !sha(c.BaseSHA) || !sha(c.HeadSHA) || c.BaseSHA == c.HeadSHA {
			return fmt.Errorf("case %s needs distinct full base and head SHAs", c.ID)
		}
		switch c.Kind {
		case "seeded", "historical":
			if len(c.Labels) == 0 {
				return fmt.Errorf("%s case %s needs at least one bug label", c.Kind, c.ID)
			}
		case "clean":
			if len(c.Labels) != 0 {
				return fmt.Errorf("clean case %s must have no labels", c.ID)
			}
		default:
			return fmt.Errorf("case %s has unsupported kind %q", c.ID, c.Kind)
		}
		for _, label := range c.Labels {
			if label.Path == "" || label.StartLine <= 0 || label.EndLine < label.StartLine {
				return fmt.Errorf("case %s has an invalid label", c.ID)
			}
		}
	}
	return nil
}

// RunResult is one review of one case. A run that failed is recorded with its
// error and left out of every score, so an outage never looks like a miss.
type RunResult struct {
	CaseID       string
	Run          int
	Findings     []Finding
	CostMicrousd int64
	Latency      time.Duration
	Error        string
}

// MatchOptions decides when a finding counts as the labelled bug. Models
// rarely land on the exact lines a human labelled, so a small slack is allowed;
// the bug class is ignored unless a dataset uses the reviewer's own classes.
type MatchOptions struct {
	LineSlack    int
	RequireClass bool
}

func (o MatchOptions) matches(f Finding, l Label) bool {
	slack := o.LineSlack
	if slack < 0 {
		slack = 0
	}
	if f.Path != l.Path || f.StartLine > l.EndLine+slack || l.StartLine > f.EndLine+slack {
		return false
	}
	return !o.RequireClass || f.BugClass == l.BugClass
}

type LabelScore struct {
	Label  Label
	Caught int // runs in which a finding matched
}

type CaseScore struct {
	ID, Kind string
	Runs     int // scored runs
	Failed   int
	Labels   []LabelScore
	Findings int // across scored runs
	Noise    int // findings matching no label, across scored runs
}

// Flaky reports labels found in some scored runs but not all of them.
func (c CaseScore) Flaky() int {
	n := 0
	for _, l := range c.Labels {
		if l.Caught > 0 && l.Caught < c.Runs {
			n++
		}
	}
	return n
}

type RepeatReport struct {
	Cases                        []CaseScore
	ScoredRuns, FailedRuns       int
	Recall, RecallMin, RecallMax float64 // over runs; Min and Max are across whole-set runs
	Precision                    float64
	NoisePerCleanRun             float64
	FlakyLabels, MissedLabels    int
	Labels                       int
	CostMicrousd                 int64
	MedianLatency                time.Duration
}

// ScoreRepeat scores every run against the dataset. Recall is the share of
// labelled bugs found, averaged over scored runs, and RecallMin/RecallMax show
// the spread between whole-set runs. Precision is the share of findings on
// labelled cases that matched a label; findings on clean cases are noise.
func ScoreRepeat(dataset RepeatDataset, results []RunResult, options MatchOptions) (RepeatReport, error) {
	if err := dataset.Validate(); err != nil {
		return RepeatReport{}, err
	}
	byID := map[string]RepeatCase{}
	for _, c := range dataset.Cases {
		byID[c.ID] = c
	}
	scores := map[string]*CaseScore{}
	for _, c := range dataset.Cases {
		cs := &CaseScore{ID: c.ID, Kind: c.Kind}
		for _, l := range c.Labels {
			cs.Labels = append(cs.Labels, LabelScore{Label: l})
		}
		scores[c.ID] = cs
	}
	type tally struct{ caught, labels int }
	perRun := map[int]*tally{}
	var report RepeatReport
	var matched, findings, cleanRuns, cleanNoise int
	var latencies []time.Duration
	for _, r := range results {
		c, ok := byID[r.CaseID]
		if !ok {
			return RepeatReport{}, fmt.Errorf("result for unknown case %q", r.CaseID)
		}
		cs := scores[r.CaseID]
		report.CostMicrousd += r.CostMicrousd
		if r.Error != "" {
			cs.Failed++
			report.FailedRuns++
			continue
		}
		cs.Runs++
		report.ScoredRuns++
		latencies = append(latencies, r.Latency)
		t := perRun[r.Run]
		if t == nil {
			t = &tally{}
			perRun[r.Run] = t
		}
		for i, l := range c.Labels {
			t.labels++
			for _, f := range r.Findings {
				if options.matches(f, l) {
					cs.Labels[i].Caught++
					t.caught++
					break
				}
			}
		}
		for _, f := range r.Findings {
			hit := false
			for _, l := range c.Labels {
				if options.matches(f, l) {
					hit = true
					break
				}
			}
			cs.Findings++
			if !hit {
				cs.Noise++
			}
			if c.Kind == "clean" {
				cleanNoise++
			} else {
				findings++
				if hit {
					matched++
				}
			}
		}
		if c.Kind == "clean" {
			cleanRuns++
		}
	}
	var caught, labelRuns int
	first := true
	for _, t := range perRun {
		caught += t.caught
		labelRuns += t.labels
		if t.labels == 0 {
			continue
		}
		rate := float64(t.caught) / float64(t.labels)
		if first || rate < report.RecallMin {
			report.RecallMin = rate
		}
		if first || rate > report.RecallMax {
			report.RecallMax = rate
		}
		first = false
	}
	if labelRuns > 0 {
		report.Recall = float64(caught) / float64(labelRuns)
	}
	if findings > 0 {
		report.Precision = float64(matched) / float64(findings)
	}
	if cleanRuns > 0 {
		report.NoisePerCleanRun = float64(cleanNoise) / float64(cleanRuns)
	}
	for _, c := range dataset.Cases {
		cs := scores[c.ID]
		for _, l := range cs.Labels {
			report.Labels++
			switch {
			case cs.Runs > 0 && l.Caught == 0:
				report.MissedLabels++
			case l.Caught > 0 && l.Caught < cs.Runs:
				report.FlakyLabels++
			}
		}
		report.Cases = append(report.Cases, *cs)
	}
	sort.Slice(latencies, func(i, j int) bool { return latencies[i] < latencies[j] })
	if n := len(latencies); n > 0 {
		report.MedianLatency = latencies[n/2]
		if n%2 == 0 {
			report.MedianLatency = (latencies[n/2-1] + latencies[n/2]) / 2
		}
	}
	return report, nil
}

// Markdown renders the report for a person to read. It states the sample size
// because a score from a handful of runs is a sketch, not a measurement.
func (r RepeatReport) Markdown() string {
	var b strings.Builder
	fmt.Fprintf(&b, "# Review evaluation\n\n%d scored runs over %d cases (%d failed runs not scored). Cost $%.4f.\n\n", r.ScoredRuns, len(r.Cases), r.FailedRuns, float64(r.CostMicrousd)/1e6)
	if r.ScoredRuns == 0 {
		b.WriteString("Nothing was scored.\n")
		return b.String()
	}
	fmt.Fprintf(&b, "- **Recall** %.0f%% of labelled bugs found per run (best run %.0f%%, worst run %.0f%%)\n", r.Recall*100, r.RecallMax*100, r.RecallMin*100)
	fmt.Fprintf(&b, "- **Precision** %.0f%% of findings on bug cases matched a label\n", r.Precision*100)
	fmt.Fprintf(&b, "- **Noise on clean changes** %.2f findings per clean run\n", r.NoisePerCleanRun)
	fmt.Fprintf(&b, "- **Unstable bugs** %d of %d labels were found in some runs but not all; **never found** %d\n", r.FlakyLabels, r.Labels, r.MissedLabels)
	fmt.Fprintf(&b, "- Median review time %s\n\n", r.MedianLatency.Round(time.Second))
	b.WriteString("| Case | Kind | Found in runs | Findings | Noise |\n| --- | --- | --- | --- | --- |\n")
	for _, c := range r.Cases {
		found := "-"
		if len(c.Labels) > 0 {
			parts := make([]string, len(c.Labels))
			for i, l := range c.Labels {
				parts[i] = fmt.Sprintf("%d/%d", l.Caught, c.Runs)
			}
			found = strings.Join(parts, ", ")
		}
		fmt.Fprintf(&b, "| %s | %s | %s | %d | %d |\n", c.ID, c.Kind, found, c.Findings, c.Noise)
	}
	return b.String()
}
