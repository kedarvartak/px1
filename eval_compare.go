package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"

	"px1/internal/hosted/eval"
)

// runEvalCompare scores two eval-run result files against the same cases and
// says whether the difference is larger than run-to-run noise. It sends nothing
// anywhere: it reads files only.
func runEvalCompare(args []string, stdout io.Writer) error {
	fs := flag.NewFlagSet("eval-compare", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	cases := fs.String("cases", "", "the repeat dataset both runs used (required)")
	before := fs.String("before", "", "results.json from the earlier eval-run (required)")
	after := fs.String("after", "", "results.json from the later eval-run (required)")
	slack := fs.Int("line-slack", 3, "lines a finding may be from a label and still match")
	requireClass := fs.Bool("require-class", false, "also require the finding's type to equal the label's bug_class")
	out := fs.String("out", "", "optional file to write the comparison to")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *cases == "" || *before == "" || *after == "" || *slack < 0 {
		return errors.New("eval-compare requires --cases, --before and --after")
	}
	data, err := os.ReadFile(*cases)
	if err != nil {
		return fmt.Errorf("read cases: %w", err)
	}
	dataset, err := eval.DecodeRepeat(data)
	if err != nil {
		return err
	}
	options := eval.MatchOptions{LineSlack: *slack, RequireClass: *requireClass}
	score := func(path string) (eval.RepeatReport, error) {
		raw, err := os.ReadFile(path)
		if err != nil {
			return eval.RepeatReport{}, fmt.Errorf("read results: %w", err)
		}
		var results []eval.RunResult
		if err := json.Unmarshal(raw, &results); err != nil {
			return eval.RepeatReport{}, fmt.Errorf("decode %s: %w", path, err)
		}
		return eval.ScoreRepeat(dataset, results, options)
	}
	b, err := score(*before)
	if err != nil {
		return err
	}
	a, err := score(*after)
	if err != nil {
		return err
	}
	comparison, err := eval.CompareRepeat(b, a)
	if err != nil {
		return err
	}
	md := comparison.Markdown()
	if *out != "" {
		if err := os.WriteFile(*out, []byte(md), 0o600); err != nil {
			return err
		}
	}
	_, err = io.WriteString(stdout, md)
	return err
}
