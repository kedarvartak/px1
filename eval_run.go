package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"px1/internal/hosted/eval"
)

type evalRunOptions struct {
	cases, root, out, model, endpoint, tier string
	priceIn, priceOut, priceCached          float64
	maxCost, maxTotal                       float64
	runs, lineSlack                         int
	maxMinutes, maxOutput, concurrency      int
	maxFiles, maxDiffBytes                  int
	excludes                                repeatedStringFlag
	yes, dryRun, resume, requireClass       bool
}

// runEvalRun reviews every case in a repeat dataset several times with the
// agentic reviewer and scores the results. It goes through runAgenticReview
// for each run, so consent, the privacy policy and the per-review cost cap are
// exactly those of a normal review, and adds a cap on the total spend.
func runEvalRun(args []string, stdout io.Writer) error {
	o, err := parseEvalRunFlags(args)
	if err != nil {
		return err
	}
	data, err := os.ReadFile(o.cases)
	if err != nil {
		return fmt.Errorf("read cases: %w", err)
	}
	dataset, err := eval.DecodeRepeat(data)
	if err != nil {
		return err
	}
	root, err := filepath.Abs(o.root)
	if err != nil {
		return err
	}
	for _, c := range dataset.Cases {
		for _, rev := range []string{c.BaseSHA, c.HeadSHA} {
			if _, err := resolveCommit(root, rev); err != nil {
				return fmt.Errorf("case %s: commit %s is not in %s (fetch the branch that holds it): %w", c.ID, rev[:8], root, err)
			}
		}
	}
	totalRuns := len(dataset.Cases) * o.runs
	fmt.Fprintf(stdout, "px1 eval of %d cases x %d runs = %d reviews with %s\n", len(dataset.Cases), o.runs, totalRuns, o.model)
	fmt.Fprintf(stdout, "Cost: each review is capped at $%.2f; the whole evaluation at $%.2f. Typical reviews have cost a few cents; the caps are ceilings, not estimates.\n", o.maxCost, o.maxTotal)
	fmt.Fprintf(stdout, "Each review sends its case's diff, and files the model chooses to read, to %s. Secret files and key-shaped values are blocked or redacted.\n", hostOf(o.endpoint))
	if o.dryRun {
		fmt.Fprintln(stdout, "\nDry run: nothing was sent. Cases:")
		for _, c := range dataset.Cases {
			fmt.Fprintf(stdout, "  %s (%s) %s..%s, %d label(s)\n", c.ID, c.Kind, c.BaseSHA[:8], c.HeadSHA[:8], len(c.Labels))
		}
		return nil
	}
	if !o.yes {
		return errors.New("this sends repository content to the model provider; re-run with --yes, or --dry-run to preview")
	}
	if strings.TrimSpace(os.Getenv("OPENAI_API_KEY")) == "" {
		return errors.New("eval-run requires OPENAI_API_KEY in the environment; keys are never accepted as flags")
	}
	if err := os.MkdirAll(o.out, 0o700); err != nil {
		return err
	}
	resultsPath := filepath.Join(o.out, "results.json")
	var results []eval.RunResult
	if o.resume {
		if existing, err := os.ReadFile(resultsPath); err == nil {
			if err := json.Unmarshal(existing, &results); err != nil {
				return fmt.Errorf("read %s to resume: %w", resultsPath, err)
			}
		}
	}
	done := map[string]bool{}
	var spent int64
	for _, r := range results {
		done[fmt.Sprintf("%s#%d", r.CaseID, r.Run)] = true
		spent += r.CostMicrousd
	}
	capMicro := usdToMicro(o.maxTotal)
	perRunMicro := usdToMicro(o.maxCost)
	stopped := ""
	// Run-major order: every case is reviewed once before any is reviewed twice,
	// so a stop leaves whole runs to compare, not one case repeated.
	for run := 1; run <= o.runs && stopped == ""; run++ {
		for _, c := range dataset.Cases {
			key := fmt.Sprintf("%s#%d", c.ID, run)
			if done[key] {
				continue
			}
			if spent+perRunMicro > capMicro {
				stopped = fmt.Sprintf("stopped before run %d of %s: another review could pass the $%.2f total cap (spent about $%.4f)", run, c.ID, o.maxTotal, float64(spent)/1e6)
				break
			}
			result := runOneEval(o, root, c, run, stdout)
			spent += result.CostMicrousd
			results = append(results, result)
			if err := writeResults(resultsPath, results); err != nil {
				return err
			}
		}
	}
	report, err := eval.ScoreRepeat(dataset, results, eval.MatchOptions{LineSlack: o.lineSlack, RequireClass: o.requireClass})
	if err != nil {
		return err
	}
	md := report.Markdown()
	if stopped != "" {
		md += "\n**Note:** " + stopped + ". Re-run with --resume and a higher --max-total-cost to finish.\n"
	}
	if err := os.WriteFile(filepath.Join(o.out, "report.md"), []byte(md), 0o600); err != nil {
		return err
	}
	fmt.Fprintf(stdout, "\n%s\nResults: %s\nReport:  %s\n", md, resultsPath, filepath.Join(o.out, "report.md"))
	return nil
}

func writeResults(path string, results []eval.RunResult) error {
	data, err := json.MarshalIndent(results, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(data, '\n'), 0o600)
}

// runOneEval reviews one case once. Anything that stops the review from
// completing is recorded as an error, so it is left out of scoring instead of
// being counted as a bug the reviewer missed.
func runOneEval(o evalRunOptions, root string, c eval.RepeatCase, run int, stdout io.Writer) eval.RunResult {
	result := eval.RunResult{CaseID: c.ID, Run: run}
	out := filepath.Join(o.out, "runs", c.ID, strconv.Itoa(run))
	args := []string{"--root", root, "--base", c.BaseSHA, "--head", c.HeadSHA, "--out", out, "--model", o.model, "--endpoint", o.endpoint,
		"--price-input", fmt.Sprint(o.priceIn), "--price-output", fmt.Sprint(o.priceOut), "--price-cached-input", fmt.Sprint(o.priceCached),
		"--tier", o.tier, "--max-cost", fmt.Sprint(o.maxCost), "--max-output-tokens", strconv.Itoa(o.maxOutput), "--concurrency", strconv.Itoa(o.concurrency),
		"--max-files", strconv.Itoa(o.maxFiles), "--max-diff-bytes", strconv.Itoa(o.maxDiffBytes), "--transcripts=false", "--yes"}
	if o.maxMinutes > 0 {
		args = append(args, "--max-minutes", strconv.Itoa(o.maxMinutes))
	}
	for _, g := range o.excludes {
		args = append(args, "--exclude-glob", g)
	}
	fmt.Fprintf(stdout, "[%s run %d] reviewing...", c.ID, run)
	started := time.Now()
	var sink bytes.Buffer
	err := runAgenticReview(args, &sink)
	result.Latency = time.Since(started)
	if spend, ok := readSpend(out); ok {
		result.CostMicrousd = spend
	}
	if err != nil {
		result.Error = err.Error()
		fmt.Fprintf(stdout, " failed: %s\n", describeEvalError(err))
		return result
	}
	if scope, ok := readScope(out); !ok || !scope.Complete {
		result.Error = "review did not complete (a step failed); not scored"
		fmt.Fprintf(stdout, " incomplete, not scored\n")
		return result
	}
	findings, err := readEvalFindings(out)
	if err != nil {
		result.Error = err.Error()
		fmt.Fprintf(stdout, " failed: %v\n", err)
		return result
	}
	result.Findings = findings
	fmt.Fprintf(stdout, " %d finding(s), $%.4f, %s\n", len(findings), float64(result.CostMicrousd)/1e6, result.Latency.Round(time.Second))
	return result
}

func describeEvalError(err error) string {
	msg := err.Error()
	if len(msg) > 160 {
		msg = msg[:160] + "..."
	}
	return msg
}

func readEvalFindings(out string) ([]eval.Finding, error) {
	data, err := os.ReadFile(filepath.Join(out, "findings.json"))
	if err != nil {
		return nil, fmt.Errorf("read findings: %w", err)
	}
	var envelope hostedFindingsEnvelope
	if err := json.Unmarshal(data, &envelope); err != nil {
		return nil, fmt.Errorf("decode findings: %w", err)
	}
	findings := make([]eval.Finding, 0, len(envelope.Findings))
	for _, f := range envelope.Findings {
		findings = append(findings, eval.Finding{Path: f.Path, BugClass: f.Type, Severity: f.Severity, StartLine: f.StartLine, EndLine: f.EndLine})
	}
	return findings, nil
}

func readSpend(out string) (int64, bool) {
	data, err := os.ReadFile(filepath.Join(out, "trace.json"))
	if err != nil {
		return 0, false
	}
	var trace struct {
		Spend struct {
			CostMicrousd int64 `json:"cost_microusd"`
		} `json:"spend"`
	}
	if json.Unmarshal(data, &trace) != nil {
		return 0, false
	}
	return trace.Spend.CostMicrousd, true
}

func readScope(out string) (reviewScope, bool) {
	data, err := os.ReadFile(filepath.Join(out, "scope.json"))
	if err != nil {
		return reviewScope{}, false
	}
	var scope reviewScope
	return scope, json.Unmarshal(data, &scope) == nil
}

func parseEvalRunFlags(args []string) (evalRunOptions, error) {
	var o evalRunOptions
	fs := flag.NewFlagSet("eval-run", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	fs.StringVar(&o.cases, "cases", "", "repeat dataset JSON (required)")
	fs.StringVar(&o.root, "root", ".", "local clone holding every case's commits")
	fs.StringVar(&o.out, "out", "px1-eval", "output directory")
	fs.StringVar(&o.model, "model", "", "OpenAI model (required)")
	fs.StringVar(&o.endpoint, "endpoint", defaultResponsesEndpoint, "Responses API endpoint")
	fs.Float64Var(&o.priceIn, "price-input", 0, "input price, USD per million tokens (required)")
	fs.Float64Var(&o.priceOut, "price-output", 0, "output price, USD per million tokens (required)")
	fs.Float64Var(&o.priceCached, "price-cached-input", 0, "cached input price (default: same as input)")
	fs.StringVar(&o.tier, "tier", "plus", "agentic tier: plus or apex")
	fs.Float64Var(&o.maxCost, "max-cost", 0.50, "hard cap per review, USD")
	fs.Float64Var(&o.maxTotal, "max-total-cost", 5.00, "hard cap for the whole evaluation, USD")
	fs.IntVar(&o.runs, "runs", 3, "reviews per case; at least 3 to see run-to-run variance")
	fs.IntVar(&o.lineSlack, "line-slack", 3, "lines a finding may be from a label and still match")
	fs.BoolVar(&o.requireClass, "require-class", false, "also require the finding's type to equal the label's bug_class")
	fs.IntVar(&o.maxMinutes, "max-minutes", 0, "wall-time limit per review in minutes (default: the tier's)")
	fs.IntVar(&o.maxOutput, "max-output-tokens", 4000, "output-token limit per model call")
	fs.IntVar(&o.concurrency, "concurrency", 3, "sub-agents running at once")
	fs.IntVar(&o.maxFiles, "max-files", defaultMaxReviewFiles, "most changed files sent per review")
	fs.IntVar(&o.maxDiffBytes, "max-diff-bytes", defaultMaxReviewDiffBytes, "most diff bytes sent per review")
	fs.Var(&o.excludes, "exclude-glob", "repository-relative glob never shown to the model (repeatable)")
	fs.BoolVar(&o.yes, "yes", false, "send repository content to the model provider")
	fs.BoolVar(&o.dryRun, "dry-run", false, "list the cases; send nothing")
	fs.BoolVar(&o.resume, "resume", false, "skip reviews already in <out>/results.json")
	fs.Usage = func() {
		fmt.Fprintln(fs.Output(), "usage: OPENAI_API_KEY=... px1 eval-run --cases <file> --root <clone> --model <m> --price-input <usd/M> --price-output <usd/M> [--dry-run | --yes] [options]")
		fs.PrintDefaults()
	}
	if err := fs.Parse(args); err != nil {
		return o, err
	}
	switch {
	case o.cases == "" || strings.TrimSpace(o.model) == "":
		return o, errors.New("eval-run requires --cases and --model")
	case o.priceIn <= 0 || o.priceOut <= 0 || o.priceCached < 0:
		return o, errors.New("--price-input and --price-output must be positive")
	case o.runs < 1 || o.runs > 20:
		return o, errors.New("--runs must be between 1 and 20")
	case o.maxCost <= 0 || o.maxTotal < o.maxCost:
		return o, errors.New("--max-cost must be positive and no more than --max-total-cost")
	case o.lineSlack < 0:
		return o, errors.New("--line-slack cannot be negative")
	}
	if o.priceCached == 0 {
		o.priceCached = o.priceIn
	}
	return o, nil
}
