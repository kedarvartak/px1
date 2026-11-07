package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"

	"px1/internal/hosted/agent"
	"px1/internal/hosted/llm"
	hostedreview "px1/internal/hosted/review"
)

type agenticOptions struct {
	base, head, root, out, model, endpoint, title, body, tier string
	priceIn, priceOut, priceCached                            float64
	maxCost                                                   float64
	maxMinutes, maxOutput, concurrency                        int
	excludes                                                  repeatedStringFlag
	yes, dryRun, transcripts                                  bool
}

// runAgenticReview reviews two commits of a local clone with the agentic
// pipeline and writes a commit-pinned report. It sends repository content to
// the model provider, so it previews what would be sent and requires --yes.
func runAgenticReview(args []string, stdout io.Writer) error {
	opts, err := parseAgenticFlags(args)
	if err != nil {
		return err
	}
	key := strings.TrimSpace(os.Getenv("OPENAI_API_KEY"))
	if key == "" && !opts.dryRun {
		return errors.New("agentic-review requires OPENAI_API_KEY in the environment; keys are never accepted as flags")
	}
	root, err := filepath.Abs(opts.root)
	if err != nil {
		return fmt.Errorf("resolve root: %w", err)
	}
	policy, err := newPrivacyPolicy(opts.excludes)
	if err != nil {
		return err
	}
	prep, err := prepareAgenticReview(root, opts.base, opts.head, policy)
	if err != nil {
		return err
	}
	budget, err := agenticBudget(opts)
	if err != nil {
		return err
	}
	printAgenticPlan(stdout, opts, prep, budget)
	if opts.dryRun {
		fmt.Fprintln(stdout, "\nDry run: nothing was sent to the model provider.")
		return nil
	}
	if !opts.yes {
		return errors.New("this would send the content above to " + hostOf(opts.endpoint) + "; re-run with --yes to proceed, or --dry-run to preview only")
	}
	out, err := filepath.Abs(opts.out)
	if err != nil {
		return fmt.Errorf("resolve output: %w", err)
	}
	private := filepath.Join(out, "private")
	if err := os.MkdirAll(private, 0o700); err != nil {
		return fmt.Errorf("create output directory: %w", err)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	started := time.Now()
	meter := agent.NewMeter(budget, started, nil)
	trace := agent.NewTrace(prep.HeadSHA, agent.Tier(opts.tier), nil)
	redactor := &secretRedactor{}
	guard := agent.Guarded{Repo: agent.GitRepo{Root: root}, Allow: policy.Allow, Redact: redactor.Redact}
	recorder, closeRecorder, err := newTranscriptRecorder(filepath.Join(private, "transcripts.jsonl"), opts.transcripts)
	if err != nil {
		return err
	}
	defer closeRecorder()
	gateway := llm.Gateway{Providers: map[string]llm.Provider{"openai": llm.OpenAIProvider{APIKey: key, Endpoint: opts.endpoint, HTTP: &http.Client{}}}, Timeout: 180 * time.Second}
	limiter, err := agent.NewOrgLimiter(opts.concurrency)
	if err != nil {
		return err
	}
	pipeline := agent.Pipeline{
		Model: agent.Model{Client: gateway, Provider: "openai", Name: opts.model, MaxOutputTokens: opts.maxOutput, Meter: meter, Trace: trace, Record: recorder,
			Price: llm.Price{InputMicrousdPerMillion: usdToMicro(opts.priceIn), OutputMicrousdPerMillion: usdToMicro(opts.priceOut), CacheReadMicrousdPerMillion: usdToMicro(opts.priceCached)}},
		Box:     agent.Toolbox{Files: guard, Lister: guard, Search: guard, History: guard},
		Scope:   agent.Scope{InstallationID: 1, RepositoryID: 1, HeadSHA: prep.HeadSHA},
		Limiter: limiter,
		OnStage: func(stage string) { fmt.Fprintf(stdout, "\n%s...\n", stage) },
		OnPartial: func(p agent.Partial) {
			mark := "done"
			if p.Outcome != agent.TaskCompleted {
				mark = string(p.Outcome) + ": " + p.Reason
			}
			fmt.Fprintf(stdout, "  change group %s %s (%d/%d, %d provisional findings)\n", p.BlockID, mark, p.Done, p.Total, len(p.Findings))
		},
	}
	title, body := opts.title, opts.body
	if title == "" {
		title, body = defaultReviewText(root, prep.BaseSHA, prep.HeadSHA, body)
	}
	result, runErr := pipeline.Run(ctx, agent.PipelineInput{Title: title, Body: body, Files: prep.Files, Blocks: prep.Blocks})
	elapsed := time.Since(started).Round(time.Second)

	if err := writeAgenticOutputs(out, private, opts, prep, trace, result, redactor.count.Load(), elapsed); err != nil {
		return err
	}
	if runErr != nil {
		printAgenticSummary(stdout, budget, result, redactor.count.Load(), elapsed, out, nil)
		return fmt.Errorf("review stopped early: %s. Partial notes and the trace are in %s", describeRunError(runErr), private)
	}
	report, err := generateStaticReviewWithOptions(root, prep.BaseSHA, prep.HeadSHA, staticReviewOptions{FindingsFile: filepath.Join(out, "findings.json"), TraceFile: filepath.Join(out, "trace.json")})
	if err != nil {
		return fmt.Errorf("build the report: %w", err)
	}
	if err := writeStaticReviewReport(filepath.Join(out, "report"), report); err != nil {
		return err
	}
	encoded, _ := json.Marshal(report)
	printAgenticSummary(stdout, budget, result, redactor.count.Load(), elapsed, out, findPrivacyRisks(report, encoded))
	return nil
}

func parseAgenticFlags(args []string) (agenticOptions, error) {
	var o agenticOptions
	fs := flag.NewFlagSet("agentic-review", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	fs.StringVar(&o.base, "base", "", "base commit or ref (required)")
	fs.StringVar(&o.head, "head", "HEAD", "head commit or ref")
	fs.StringVar(&o.root, "root", ".", "repository root (a local clone)")
	fs.StringVar(&o.out, "out", "px1-agentic", "output directory")
	fs.StringVar(&o.model, "model", "", "OpenAI model (required)")
	fs.StringVar(&o.endpoint, "endpoint", defaultResponsesEndpoint, "Responses API endpoint")
	fs.Float64Var(&o.priceIn, "price-input", 0, "model input price in USD per million tokens (required)")
	fs.Float64Var(&o.priceOut, "price-output", 0, "model output price in USD per million tokens (required)")
	fs.Float64Var(&o.priceCached, "price-cached-input", 0, "cached input price in USD per million tokens (default: same as input)")
	fs.StringVar(&o.tier, "tier", "plus", "agentic tier: plus or apex")
	fs.Float64Var(&o.maxCost, "max-cost", 1.00, "hard cost cap in USD; the review stops when it could be crossed")
	fs.IntVar(&o.maxMinutes, "max-minutes", 0, "wall-time limit in minutes (default: the tier's)")
	fs.IntVar(&o.maxOutput, "max-output-tokens", 4000, "output-token limit per model call; reasoning models need room for their reasoning")
	fs.IntVar(&o.concurrency, "concurrency", 3, "sub-agents running at once")
	fs.Var(&o.excludes, "exclude-glob", "repository-relative glob never shown to the model (repeatable)")
	fs.StringVar(&o.title, "title", "", "pull request title (default: the head commit subject)")
	fs.StringVar(&o.body, "body", "", "pull request description")
	fs.BoolVar(&o.yes, "yes", false, "send repository content to the model provider")
	fs.BoolVar(&o.dryRun, "dry-run", false, "show what would be sent and what it could cost; send nothing")
	fs.BoolVar(&o.transcripts, "transcripts", true, "save raw prompts and replies under <out>/private for debugging")
	fs.Usage = func() {
		fmt.Fprintln(fs.Output(), "usage: OPENAI_API_KEY=... px1 agentic-review --base <commit> --model <model> --price-input <usd/M> --price-output <usd/M> [--dry-run | --yes] [options]")
		fs.PrintDefaults()
	}
	if err := fs.Parse(args); err != nil {
		return o, err
	}
	switch {
	case strings.TrimSpace(o.base) == "" || strings.TrimSpace(o.model) == "":
		return o, errors.New("agentic-review requires --base and --model")
	case o.priceIn <= 0 || o.priceOut <= 0 || o.priceCached < 0:
		return o, errors.New("agentic-review requires --price-input and --price-output (USD per million tokens) so the cost cap is real")
	case o.tier != "plus" && o.tier != "apex":
		return o, errors.New("--tier must be plus or apex")
	case o.maxCost <= 0 || o.maxMinutes < 0 || o.maxOutput < 500 || o.maxOutput > 32000 || o.concurrency < 1 || o.concurrency > 6:
		return o, errors.New("--max-cost must be positive, --max-output-tokens 500-32000, and --concurrency 1-6")
	}
	if o.priceCached == 0 {
		o.priceCached = o.priceIn
	}
	parsed, err := url.Parse(o.endpoint)
	loopback := parsed != nil && (parsed.Hostname() == "localhost" || parsed.Hostname() == "127.0.0.1" || parsed.Hostname() == "::1")
	if err != nil || parsed.Host == "" || parsed.User != nil || (parsed.Scheme != "https" && !(parsed.Scheme == "http" && loopback)) {
		return o, errors.New("--endpoint must be an https URL (http only on loopback) without credentials")
	}
	return o, nil
}

func agenticBudget(o agenticOptions) (agent.Budget, error) {
	budget, err := agent.BudgetFor(agent.Tier(o.tier))
	if err != nil {
		return budget, err
	}
	budget.MaxCostMicrousd = min(budget.MaxCostMicrousd, int64(o.maxCost*1_000_000))
	if o.maxMinutes > 0 {
		budget.WallTime = time.Duration(o.maxMinutes) * time.Minute
	}
	return budget, nil
}

func usdToMicro(usd float64) int64 { return int64(usd*1_000_000 + 0.5) }

func hostOf(endpoint string) string {
	if parsed, err := url.Parse(endpoint); err == nil && parsed.Host != "" {
		return parsed.Host
	}
	return endpoint
}

func printAgenticPlan(w io.Writer, o agenticOptions, prep agenticPrep, budget agent.Budget) {
	fmt.Fprintf(w, "px1 agentic review of %s...%s\n", shortCommit(prep.BaseSHA), shortCommit(prep.HeadSHA))
	fmt.Fprintf(w, "  model %s via %s, tier %s\n", o.model, hostOf(o.endpoint), o.tier)
	fmt.Fprintf(w, "\nWill be shown to the model (%d files, about %d diff tokens; tools may also read other files at this commit):\n", len(prep.Files), agent.EstimateTokens(strings.Repeat("x", prep.DiffBytes)))
	for _, f := range prep.Files {
		fmt.Fprintf(w, "  %s (%d bytes)\n", f.Path, len(f.Diff))
	}
	if len(prep.TruncatedFiles) > 0 {
		fmt.Fprintf(w, "  diffs cut at %d bytes: %s\n", maxFileDiffBytes, strings.Join(prep.TruncatedFiles, ", "))
	}
	if len(prep.Excluded) > 0 {
		fmt.Fprintln(w, "\nNever shown to the model (also blocked from every tool):")
		for _, e := range prep.Excluded {
			fmt.Fprintf(w, "  %s - %s\n", e.Path, e.Reason)
		}
	}
	fmt.Fprintln(w, "\nSecret scan: no high-confidence secret signatures in the diff text. Secret-like values found by tools are redacted. This is detection, not access control.")
	fmt.Fprintf(w, "\nLimits: cost cap $%.2f, up to %d sub-agents x %d tool calls, %d model calls, %s, %d output tokens per call\n",
		float64(budget.MaxCostMicrousd)/1e6, budget.MaxSubAgents, budget.MaxToolCallsPerAgent, budget.MaxModelCalls, budget.WallTime, o.maxOutput)
	fmt.Fprintf(w, "Cost cap is checked before every call using the prices you gave ($%.2f in / $%.2f out per million tokens).\n", o.priceIn, o.priceOut)
}

func printAgenticSummary(w io.Writer, budget agent.Budget, result agent.PipelineResult, redactions int64, elapsed time.Duration, out string, risks []privacyRisk) {
	fmt.Fprintf(w, "\n=== Result ===\n")
	if len(result.Findings) == 0 {
		fmt.Fprintln(w, "No verified findings. That is not evidence the change is correct: it means nothing the models proposed survived verification.")
	}
	unanchored := map[string]bool{}
	for _, u := range result.Unanchored {
		unanchored[fmt.Sprintf("%s:%d:%s", u.Finding.Path, u.Finding.StartLine, u.Finding.Title)] = true
	}
	for _, f := range result.Findings {
		suffix := ""
		if unanchored[fmt.Sprintf("%s:%d:%s", f.Path, f.StartLine, f.Title)] {
			suffix = "  (lines not in the changed range)"
		}
		fmt.Fprintf(w, "  %s %s:%d  %s  [confidence %.2f]%s\n", f.Severity, f.Path, f.StartLine, f.Title, f.Confidence, suffix)
	}
	if len(result.Unverified) > 0 {
		fmt.Fprintf(w, "  %d candidates could not be verified and were not reported; see private/unverified.json\n", len(result.Unverified))
	}
	fmt.Fprintf(w, "\nCandidates proposed %d, after judging %d, verified %d.\n", result.Candidates, result.Judged, len(result.Findings))
	s := result.Spend
	fmt.Fprintf(w, "Spent about $%.4f (cap $%.2f): %d model calls, %d tool calls, %d input / %d output tokens, %s.\n", float64(s.CostMicrousd)/1e6, float64(budget.MaxCostMicrousd)/1e6, s.ModelCalls, s.ToolCalls, s.InputTokens, s.OutputTokens, elapsed)
	if s.Overruns > 0 {
		fmt.Fprintf(w, "  %d calls used more input tokens than estimated, so real spend may be slightly above the checked figure.\n", s.Overruns)
	}
	if s.Exhausted != "" {
		fmt.Fprintf(w, "  Exploration was limited: the %s budget ran out.\n", s.Exhausted)
	}
	if !result.Run.Complete && result.Run.Outcomes != nil {
		fmt.Fprintln(w, "  Some change groups did not finish; see the notes below.")
	}
	if redactions > 0 {
		fmt.Fprintf(w, "  %d secret-like values were redacted from tool output before reaching the model.\n", redactions)
	}
	if len(result.Notes) > 0 {
		fmt.Fprintln(w, "\nNotes:")
		for _, n := range result.Notes {
			fmt.Fprintf(w, "  [%s] %s\n", n.Stage, n.Message)
		}
	}
	if len(risks) > 0 {
		parts := make([]string, 0, len(risks))
		for _, risk := range risks {
			if risk.Path != "" {
				parts = append(parts, risk.Path+" ("+risk.Reason+")")
			} else {
				parts = append(parts, risk.Reason)
			}
		}
		fmt.Fprintf(w, "\nWARNING: the report shows the whole change, including likely sensitive content: %s.\nThe models never saw excluded files, but this report is for you only: do not publish or share it. (px1 privacy-check would block its delivery.)\n", strings.Join(parts, ", "))
	}
	fmt.Fprintf(w, "\nReport:   file://%s\n", filepath.Join(out, "report", "index.html"))
	fmt.Fprintf(w, "Feedback: %s\n", filepath.Join(out, "private", "feedback.md"))
	fmt.Fprintln(w, "The report and private/ hold your repository's code; do not publish them without checking.")
}

func describeRunError(err error) string {
	var provider llm.ProviderError
	switch {
	case errors.As(err, &provider):
		return err.Error()
	case errors.Is(err, agent.ErrBudgetExhausted):
		return "a budget limit was reached (" + err.Error() + ")"
	case errors.Is(err, context.Canceled):
		return "cancelled"
	}
	return err.Error()
}

func defaultReviewText(root, base, head, body string) (string, string) {
	subjects, err := commitSubjects(root, base, head)
	if err != nil || len(subjects) == 0 {
		return "Review of " + shortCommit(head), body
	}
	if body == "" {
		body = "Commits in this change:\n- " + strings.Join(subjects, "\n- ")
	}
	return subjects[0], body
}

func writeAgenticOutputs(out, private string, o agenticOptions, prep agenticPrep, trace *agent.Trace, result agent.PipelineResult, redactions int64, elapsed time.Duration) error {
	findings := result.Findings
	if findings == nil {
		findings = []hostedreview.Finding{}
	}
	writeJSON := func(path string, value any, perm os.FileMode) error {
		data, err := json.MarshalIndent(value, "", "  ")
		if err != nil {
			return err
		}
		return os.WriteFile(path, append(data, '\n'), perm)
	}
	if err := writeJSON(filepath.Join(out, "findings.json"), hostedFindingsEnvelope{Revision: prep.HeadSHA, Findings: findings}, 0o644); err != nil {
		return err
	}
	if err := writeJSON(filepath.Join(out, "trace.json"), trace.File(result.Spend), 0o644); err != nil {
		return err
	}
	if err := writeJSON(filepath.Join(private, "notes.json"), result.Notes, 0o600); err != nil {
		return err
	}
	if len(result.Unverified) > 0 {
		if err := writeJSON(filepath.Join(private, "unverified.json"), result.Unverified, 0o600); err != nil {
			return err
		}
	}
	return os.WriteFile(filepath.Join(private, "feedback.md"), []byte(feedbackSheet(o, prep, result, redactions, elapsed)), 0o600)
}

func feedbackSheet(o agenticOptions, prep agenticPrep, result agent.PipelineResult, redactions int64, elapsed time.Duration) string {
	var b strings.Builder
	fmt.Fprintf(&b, "# px1 agentic review feedback\n\n")
	fmt.Fprintf(&b, "Comparison `%s...%s`, model `%s`, tier %s, %s, about $%.4f.\n\n", shortCommit(prep.BaseSHA), shortCommit(prep.HeadSHA), o.model, o.tier, elapsed, float64(result.Spend.CostMicrousd)/1e6)
	b.WriteString("Judge each finding yourself before ticking anything. A finding you cannot judge is \"unclear\", not \"real\".\n\n## Findings\n\n")
	if len(result.Findings) == 0 {
		b.WriteString("_No verified findings._\n\n")
	}
	for i, f := range result.Findings {
		fmt.Fprintf(&b, "### %d. %s `%s:%d` - %s\n\n", i+1, f.Severity, f.Path, f.StartLine, f.Title)
		b.WriteString("- Verdict: [ ] real problem  [ ] false alarm  [ ] unclear\n")
		b.WriteString("- [ ] I already knew about this\n- [ ] I would have caught this in a normal review\n- Notes:\n\n")
	}
	b.WriteString("## What it missed\n\nReal problems in this change that no finding mentions (leave blank if none):\n\n- \n\n## Overall\n\n")
	b.WriteString("- Minutes to read the report:\n- Was the trace (\"How px1 reviewed this\") useful for deciding what to trust? \n- Would you run this on the next change? Why or why not:\n")
	fmt.Fprintf(&b, "\n---\nRun facts: %d candidates, %d after judging, %d verified; %d model calls; %d tool calls; secrets redacted from tool output: %d.\n", result.Candidates, result.Judged, len(result.Findings), result.Spend.ModelCalls, result.Spend.ToolCalls, redactions)
	return b.String()
}

// newTranscriptRecorder saves raw prompts and replies for debugging. They
// contain repository code, so they stay under private/ with owner-only access
// and are never part of the report.
func newTranscriptRecorder(path string, enabled bool) (func(agent, role string, prompt agent.Prompt, reply string, err error), func(), error) {
	var (
		mu     sync.Mutex
		file   *os.File
		failed = map[string]bool{}
	)
	if enabled {
		f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
		if err != nil {
			return nil, nil, fmt.Errorf("open transcripts: %w", err)
		}
		file = f
	}
	record := func(agentID, role string, prompt agent.Prompt, reply string, err error) {
		mu.Lock()
		defer mu.Unlock()
		if err != nil {
			if message := err.Error(); !failed[message] && len(failed) < 3 {
				failed[message] = true
				fmt.Fprintf(os.Stderr, "px1: %s model call failed: %s\n", role, message)
			}
		}
		if file == nil {
			return
		}
		entry := map[string]any{"time": time.Now().UTC().Format(time.RFC3339), "agent": agentID, "role": role, "system": prompt.System, "user": prompt.User, "reply": reply}
		if err != nil {
			entry["error"] = err.Error()
		}
		data, _ := json.Marshal(entry)
		_, _ = file.Write(append(data, '\n'))
	}
	closeFn := func() {
		mu.Lock()
		defer mu.Unlock()
		if file != nil {
			_ = file.Close()
		}
	}
	return record, closeFn, nil
}

func shortCommit(sha string) string {
	if len(sha) > 8 {
		return sha[:8]
	}
	return sha
}

// commitSubjects lists the subjects of the commits between base and head,
// newest first, so a review has the intent the author wrote down.
func commitSubjects(root, base, head string) ([]string, error) {
	out, err := exec.Command("git", "-C", root, "-c", "core.fsmonitor=false", "log", "-n", "20", "--format=%s", base+".."+head).Output()
	if err != nil {
		return nil, err
	}
	var subjects []string
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		if line = strings.TrimSpace(line); line != "" {
			subjects = append(subjects, line)
		}
	}
	return subjects, nil
}
