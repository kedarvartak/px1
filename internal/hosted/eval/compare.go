package eval

import (
	"fmt"
	"math"
	"strings"
)

// A reviewer change is only worth keeping if it beats the noise. The same case
// can be found in one run and missed in the next, so a before/after difference
// of one run means nothing. CompareRepeat tests each labelled bug, and the
// pooled result, with Fisher's exact test on found-versus-missed counts.

// Significance is the two-sided p-value below which a difference is called real.
// It is deliberately loose: with three runs a side the smallest possible p-value
// is 0.1, and a stricter bar would call everything noise.
const Significance = 0.10

type LabelChange struct {
	CaseID       string
	Path         string
	Line         int
	BeforeCaught int
	BeforeRuns   int
	AfterCaught  int
	AfterRuns    int
	P            float64
	Verdict      string // better, worse, or same
}

type Comparison struct {
	Labels                    []LabelChange
	Before, After             RepeatReport
	PooledP                   float64
	PooledVerdict             string
	Improved, Regressed, Same int
}

// CompareRepeat compares two scored reports over the same dataset. Labels with
// no scored run on either side are left out, since nothing can be said of them.
func CompareRepeat(before, after RepeatReport) (Comparison, error) {
	if len(before.Cases) != len(after.Cases) {
		return Comparison{}, fmt.Errorf("reports cover different datasets (%d and %d cases)", len(before.Cases), len(after.Cases))
	}
	c := Comparison{Before: before, After: after}
	var bc, bn, ac, an int
	for i, b := range before.Cases {
		a := after.Cases[i]
		if a.ID != b.ID || len(a.Labels) != len(b.Labels) {
			return Comparison{}, fmt.Errorf("case %d differs between reports (%q and %q)", i, b.ID, a.ID)
		}
		for j, bl := range b.Labels {
			al := a.Labels[j]
			if b.Runs == 0 || a.Runs == 0 {
				continue
			}
			ch := LabelChange{CaseID: b.ID, Path: bl.Label.Path, Line: bl.Label.StartLine,
				BeforeCaught: bl.Caught, BeforeRuns: b.Runs, AfterCaught: al.Caught, AfterRuns: a.Runs}
			ch.P = fisherExact(bl.Caught, b.Runs-bl.Caught, al.Caught, a.Runs-al.Caught)
			ch.Verdict = verdict(ch.P, rate(al.Caught, a.Runs)-rate(bl.Caught, b.Runs))
			switch ch.Verdict {
			case "better":
				c.Improved++
			case "worse":
				c.Regressed++
			default:
				c.Same++
			}
			bc, bn, ac, an = bc+bl.Caught, bn+b.Runs, ac+al.Caught, an+a.Runs
			c.Labels = append(c.Labels, ch)
		}
	}
	if bn > 0 && an > 0 {
		c.PooledP = fisherExact(bc, bn-bc, ac, an-ac)
		c.PooledVerdict = verdict(c.PooledP, rate(ac, an)-rate(bc, bn))
	}
	return c, nil
}

func rate(n, total int) float64 {
	if total == 0 {
		return 0
	}
	return float64(n) / float64(total)
}

func verdict(p, delta float64) string {
	switch {
	case p > Significance || delta == 0:
		return "same"
	case delta > 0:
		return "better"
	default:
		return "worse"
	}
}

// fisherExact is the two-sided p-value for the table [[a b] [c d]]: the total
// probability of tables, with the same margins, at most as likely as the one seen.
func fisherExact(a, b, c, d int) float64 {
	row1, row2, col1 := a+b, c+d, a+c
	n := row1 + row2
	if n == 0 {
		return 1
	}
	prob := func(x int) float64 {
		return math.Exp(logChoose(row1, x) + logChoose(row2, col1-x) - logChoose(n, col1))
	}
	observed := prob(a)
	lo, hi := max(0, col1-row2), min(row1, col1)
	p := 0.0
	for x := lo; x <= hi; x++ {
		if q := prob(x); q <= observed*(1+1e-9) {
			p += q
		}
	}
	return math.Min(1, p)
}

func logChoose(n, k int) float64 {
	if k < 0 || k > n {
		return math.Inf(-1)
	}
	a, _ := math.Lgamma(float64(n + 1))
	b, _ := math.Lgamma(float64(k + 1))
	c, _ := math.Lgamma(float64(n - k + 1))
	return a - b - c
}

// Markdown renders the comparison. It leads with what cannot be concluded,
// because most small before/after differences are noise.
func (c Comparison) Markdown() string {
	var b strings.Builder
	b.WriteString("# Review comparison\n\n")
	fmt.Fprintf(&b, "Before: %d scored runs, after: %d scored runs. A difference counts only at p ≤ %.2f (Fisher's exact test); with few runs most differences are noise.\n\n", c.Before.ScoredRuns, c.After.ScoredRuns, Significance)
	fmt.Fprintf(&b, "| | Before | After |\n| --- | --- | --- |\n")
	fmt.Fprintf(&b, "| Recall | %.0f%% | %.0f%% |\n", c.Before.Recall*100, c.After.Recall*100)
	fmt.Fprintf(&b, "| Precision | %.0f%% | %.0f%% |\n", c.Before.Precision*100, c.After.Precision*100)
	fmt.Fprintf(&b, "| Noise per clean run | %.2f | %.2f |\n", c.Before.NoisePerCleanRun, c.After.NoisePerCleanRun)
	fmt.Fprintf(&b, "| Unstable bugs | %d | %d |\n", c.Before.FlakyLabels, c.After.FlakyLabels)
	fmt.Fprintf(&b, "| Cost | $%.4f | $%.4f |\n\n", float64(c.Before.CostMicrousd)/1e6, float64(c.After.CostMicrousd)/1e6)
	if c.PooledVerdict != "" {
		fmt.Fprintf(&b, "**Overall: %s** (p=%.2f, pooled over all labelled bugs; runs of one bug are not fully independent, so treat this as a guide).\n\n", c.PooledVerdict, c.PooledP)
	}
	fmt.Fprintf(&b, "%d bugs clearly better, %d clearly worse, %d no clear difference.\n\n", c.Improved, c.Regressed, c.Same)
	b.WriteString("| Case | Before | After | p | Verdict |\n| --- | --- | --- | --- | --- |\n")
	for _, l := range c.Labels {
		fmt.Fprintf(&b, "| %s | %d/%d | %d/%d | %.2f | %s |\n", l.CaseID, l.BeforeCaught, l.BeforeRuns, l.AfterCaught, l.AfterRuns, l.P, l.Verdict)
	}
	if c.After.NoisePerCleanRun > c.Before.NoisePerCleanRun {
		b.WriteString("\n**Warning:** the after run flags more on clean changes: more findings may mean more false alarms, not a better reviewer.\n")
	}
	return b.String()
}
