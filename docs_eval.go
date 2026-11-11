package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"px1/internal/docs"
)

// docsEvalReport is what docs eval --out receives.
type docsEvalReport struct {
	Model    string             `json:"model"`
	Outcomes []docs.EvalOutcome `json:"outcomes"`
}

// runDocsEval measures docs update on a set of invented cases. Each case is
// built as a small repository in a temporary directory, updated, and scored:
// a stale page must be corrected, and a page the change does not affect must
// be left alone. Only the invented content of the case file is sent to the
// model provider.
func runDocsEval(args []string, stdout io.Writer) error {
	fs := flag.NewFlagSet("docs eval", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	casesFile := fs.String("cases", "eval/docs-cases.json", "eval: case file")
	only := fs.String("only", "", "eval: run only the case with this id")
	minFixed := fs.Float64("min-fixed", 0, "eval: fail unless at least this fraction of stale cases is corrected")
	minQuiet := fs.Float64("min-quiet", 0, "eval: fail unless at least this fraction of clean cases gets no edit")
	out := fs.String("out", "", "eval: file to write the outcomes to as JSON")
	model := fs.String("model", "", "update: OpenAI model (required)")
	endpoint := fs.String("endpoint", defaultResponsesEndpoint, "update: Responses API endpoint")
	priceIn := fs.Float64("price-input", 0, "update: model input price in USD per million tokens (required)")
	priceOut := fs.Float64("price-output", 0, "update: model output price in USD per million tokens (required)")
	maxCost := fs.Float64("max-cost", 0.50, "update: hard cost cap in USD; eval: the cap for the whole set")
	verify := fs.Bool("verify", true, "update: have the model check each edit against the code it cites before applying it")
	yes := fs.Bool("yes", false, "update: send the pages and diffs to the model provider")
	if err := fs.Parse(args); err != nil {
		return err
	}
	data, err := os.ReadFile(*casesFile)
	if err != nil {
		return err
	}
	set, err := docs.ParseEvalSet(data)
	if err != nil {
		return fmt.Errorf("%s: %w", *casesFile, err)
	}
	cases := set.Cases
	if *only != "" {
		cases = nil
		for _, c := range set.Cases {
			if c.ID == *only {
				cases = append(cases, c)
			}
		}
		if len(cases) == 0 {
			return fmt.Errorf("%s has no case %q", *casesFile, *only)
		}
	}
	if !*yes {
		fmt.Fprintf(stdout, "%d case(s) in %s are valid.\n", len(cases), *casesFile)
		return errors.New("running them sends their invented pages and code to " + hostOf(*endpoint) + "; re-run with --yes")
	}

	var outcomes []docs.EvalOutcome
	remaining := *maxCost
	for _, c := range cases {
		if remaining <= 0 {
			outcomes = append(outcomes, docs.EvalOutcome{ID: c.ID, Kind: c.Kind, Detail: "not run: the cost cap was reached"})
			continue
		}
		outcome, err := runDocsEvalCase(c, []string{
			"--model", *model, "--endpoint", *endpoint, "--yes", fmt.Sprintf("--verify=%t", *verify),
			"--price-input", fmt.Sprint(*priceIn), "--price-output", fmt.Sprint(*priceOut), "--max-cost", fmt.Sprintf("%.6f", remaining),
		})
		if err != nil {
			return fmt.Errorf("case %s: %w", c.ID, err)
		}
		remaining -= float64(outcome.CostMicrousd) / 1_000_000
		mark := "pass"
		if !outcome.Pass {
			mark = "FAIL: " + outcome.Detail
		}
		fmt.Fprintf(stdout, "  %-5s %-28s %s\n", c.Kind, c.ID, mark)
		outcomes = append(outcomes, outcome)
	}

	s := docs.SummarizeEval(outcomes)
	fmt.Fprintf(stdout, "\nStale pages corrected: %d of %d. Unaffected pages left alone: %d of %d. Edits to sections that were not stale: %d. Cost: $%.4f.\n",
		s.StaleFixed, s.Stale, s.CleanQuiet, s.Clean, s.Elsewhere, float64(s.CostMicrousd)/1_000_000)
	if *out != "" {
		encoded, err := json.MarshalIndent(docsEvalReport{Model: *model, Outcomes: outcomes}, "", "  ")
		if err != nil {
			return err
		}
		if err := os.WriteFile(*out, append(encoded, '\n'), 0o644); err != nil {
			return err
		}
	}
	if fixed, quiet := docs.Rate(s.StaleFixed, s.Stale), docs.Rate(s.CleanQuiet, s.Clean); fixed < *minFixed || quiet < *minQuiet {
		return fmt.Errorf("below the bar: %.0f%% of stale pages corrected (need %.0f%%), %.0f%% of unaffected pages left alone (need %.0f%%)", fixed*100, *minFixed*100, quiet*100, *minQuiet*100)
	}
	return nil
}

// runDocsEvalCase builds the case's repository with two commits, runs docs
// update between them, and scores the page that results.
func runDocsEvalCase(c docs.EvalCase, modelArgs []string) (docs.EvalOutcome, error) {
	root, err := os.MkdirTemp("", "px1-docs-eval-")
	if err != nil {
		return docs.EvalOutcome{}, err
	}
	defer os.RemoveAll(root)
	git := func(args ...string) error {
		cmd := exec.Command("git", append([]string{"-C", root, "-c", "user.name=px1 eval", "-c", "user.email=eval@px1.invalid", "-c", "commit.gpgsign=false", "-c", "core.hooksPath=/dev/null"}, args...)...)
		if output, err := cmd.CombinedOutput(); err != nil {
			return fmt.Errorf("git %s: %s", args[0], strings.TrimSpace(string(output)))
		}
		return nil
	}
	write := func(after bool) error {
		for name, file := range c.Files {
			content := file.Before
			if after {
				content = file.After
			}
			full := filepath.Join(root, filepath.FromSlash(name))
			if content == "" {
				if err := os.Remove(full); err != nil && !os.IsNotExist(err) {
					return err
				}
				continue
			}
			if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
				return err
			}
			if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
				return err
			}
		}
		return nil
	}
	if err := git("init", "-q"); err != nil {
		return docs.EvalOutcome{}, err
	}
	page := filepath.Join(root, filepath.FromSlash(docs.EvalPagePath))
	if err := os.MkdirAll(filepath.Dir(page), 0o755); err != nil {
		return docs.EvalOutcome{}, err
	}
	if err := os.WriteFile(page, []byte(c.PageText()), 0o644); err != nil {
		return docs.EvalOutcome{}, err
	}
	for _, after := range []bool{false, true} {
		if err := write(after); err != nil {
			return docs.EvalOutcome{}, err
		}
		if err := git("add", "-A"); err != nil {
			return docs.EvalOutcome{}, err
		}
		if err := git("commit", "-q", "--allow-empty", "-m", "eval"); err != nil {
			return docs.EvalOutcome{}, err
		}
	}
	report := filepath.Join(root, ".git", "px1-update.json")
	args := append([]string{"--root", root, "--base", "HEAD~1", "--out", report}, modelArgs...)
	if err := runDocsUpdate(args, io.Discard); err != nil {
		return docs.EvalOutcome{}, err
	}
	var result docsUpdateReport
	data, err := os.ReadFile(report)
	if err != nil {
		return docs.EvalOutcome{}, err
	}
	if err := json.Unmarshal(data, &result); err != nil {
		return docs.EvalOutcome{}, err
	}
	after, err := os.ReadFile(page)
	if err != nil {
		return docs.EvalOutcome{}, err
	}
	outcome := docs.ScoreEvalCase(c, string(after), result.Results)
	outcome.CostMicrousd = result.CostMicrousd
	if !outcome.Pass {
		// Say why: the reasons edits were refused, and the run's notes.
		why := append([]string{}, result.Notes...)
		for _, r := range result.Results {
			if !r.Applied {
				why = append(why, "refused: "+r.Rejected)
			}
		}
		if len(why) > 0 {
			outcome.Detail += " (" + strings.Join(why, "; ") + ")"
		}
	}
	return outcome, nil
}
