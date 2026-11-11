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
	"syscall"
	"time"

	"px1/internal/docs"
	"px1/internal/hosted/agent"
	"px1/internal/hosted/llm"
)

const (
	maxDocsDiffBytesPerFile = 12 << 10
	maxDocsDiffBytesPerPage = 40 << 10
)

type docsUpdateOptions struct {
	root, base, head, model, endpoint, out, sourceURL string
	priceIn, priceOut, maxCost                        float64
	maxPages, maxOutput                               int
	yes, dryRun, verify                               bool
	excludes                                          repeatedStringFlag
}

// docsUpdateJob is one page to draft edits for and what the model is shown.
type docsUpdateJob struct {
	page    docs.Page
	changed []docs.ChangedFile
	bytes   int
}

// docsUpdateReport is what --out receives.
type docsUpdateReport struct {
	Base    string            `json:"base"`
	Head    string            `json:"head"`
	Model   string            `json:"model"`
	Results []docs.EditResult `json:"results"`
	Notes   []string          `json:"notes"`
	// CostMicrousd is the metered cost of the run in millionths of a dollar.
	CostMicrousd int64 `json:"cost_microusd"`
	ModelCalls   int   `json:"model_calls"`
}

func parseDocsUpdateFlags(args []string) (docsUpdateOptions, error) {
	var o docsUpdateOptions
	fs := flag.NewFlagSet("docs update", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	fs.StringVar(&o.root, "root", ".", "repository root")
	fs.StringVar(&o.base, "base", "", "impact, apply: base commit or ref (required)")
	fs.StringVar(&o.head, "head", "HEAD", "impact: head commit or ref")
	fs.StringVar(&o.out, "out", "", "update: file to write the results to as JSON")
	fs.StringVar(&o.sourceURL, "source-url", "", "site, apply: web address of the repository tree at one commit")
	fs.StringVar(&o.model, "model", "", "update: OpenAI model (required)")
	fs.StringVar(&o.endpoint, "endpoint", defaultResponsesEndpoint, "update: Responses API endpoint")
	fs.Float64Var(&o.priceIn, "price-input", 0, "update: model input price in USD per million tokens (required)")
	fs.Float64Var(&o.priceOut, "price-output", 0, "update: model output price in USD per million tokens (required)")
	fs.Float64Var(&o.maxCost, "max-cost", 0.50, "update: hard cost cap in USD; drafting stops when it could be crossed")
	fs.IntVar(&o.maxPages, "max-pages", 6, "update: most pages drafted for in one run")
	fs.IntVar(&o.maxOutput, "max-output-tokens", 4000, "update: output-token limit per model call")
	fs.BoolVar(&o.verify, "verify", true, "update: have the model check each edit against the code it cites before applying it")
	fs.BoolVar(&o.yes, "yes", false, "update: send the pages and diffs to the model provider")
	fs.BoolVar(&o.dryRun, "dry-run", false, "apply: check the edits and report, but write nothing")
	fs.Var(&o.excludes, "exclude-glob", "update: repository-relative glob never shown to the model (repeatable)")
	if err := fs.Parse(args); err != nil {
		return o, err
	}
	switch {
	case strings.TrimSpace(o.base) == "":
		return o, errors.New("docs update requires --base")
	case o.maxPages < 1 || o.maxPages > 20 || o.maxOutput < 500 || o.maxOutput > 32000 || o.maxCost <= 0:
		return o, errors.New("--max-pages must be 1-20, --max-output-tokens 500-32000, and --max-cost positive")
	}
	if o.dryRun {
		return o, nil
	}
	if strings.TrimSpace(o.model) == "" || o.priceIn <= 0 || o.priceOut <= 0 {
		return o, errors.New("docs update requires --model, --price-input and --price-output (USD per million tokens) so the cost cap is real")
	}
	parsed, err := url.Parse(o.endpoint)
	loopback := parsed != nil && (parsed.Hostname() == "localhost" || parsed.Hostname() == "127.0.0.1" || parsed.Hostname() == "::1")
	if err != nil || parsed.Host == "" || parsed.User != nil || (parsed.Scheme != "https" && !(parsed.Scheme == "http" && loopback)) {
		return o, errors.New("--endpoint must be an https URL (http only on loopback) without credentials")
	}
	return o, nil
}

// docsFileDiff returns the diff of one file between two commits. Git only
// reads: no external diff program or text conversion filter runs.
func docsFileDiff(root, base, head, file string) (string, error) {
	out, err := exec.Command("git", "-C", root, "-c", "core.fsmonitor=false", "diff", "--no-ext-diff", "--no-textconv", "--no-renames", "--no-color", "-U3", base, head, "--", file).Output()
	if err != nil {
		return "", fmt.Errorf("git diff of %s failed: %w", file, err)
	}
	return string(out), nil
}

// docsUpdateJobs chooses the pages to draft for and gathers their diffs.
// Files the privacy policy excludes are never shown; secret-like values in
// the rest are redacted.
func docsUpdateJobs(root string, impact docs.Impact, o docsUpdateOptions, redact func(string) string) ([]docsUpdateJob, []string, error) {
	pages, problems, err := docs.LoadPages(root)
	if err != nil {
		return nil, nil, err
	}
	if len(problems) > 0 {
		return nil, nil, fmt.Errorf("%s: fix the pages first (px1 docs check)", problems[0])
	}
	byPath := map[string]docs.Page{}
	for _, page := range pages {
		byPath[page.Path] = page
	}
	policy, err := newPrivacyPolicy(o.excludes)
	if err != nil {
		return nil, nil, err
	}
	var jobs []docsUpdateJob
	var notes []string
	for _, affected := range impact.Pages {
		page := byPath[affected.Path]
		if affected.State != docs.NeedsReview || page.Kind != "narrative" {
			continue
		}
		if len(jobs) == o.maxPages {
			notes = append(notes, fmt.Sprintf("%s: not drafted, over --max-pages %d", page.Path, o.maxPages))
			continue
		}
		job := docsUpdateJob{page: page}
		for _, file := range affected.Changed {
			if allowed, reason := policy.Allow(file); !allowed {
				notes = append(notes, fmt.Sprintf("%s: %s not shown to the model (%s)", page.Path, file, reason))
				continue
			}
			diff, err := docsFileDiff(root, impact.Base, impact.Head, file)
			if err != nil {
				return nil, nil, err
			}
			diff = redact(diff)
			if len(diff) > maxDocsDiffBytesPerFile {
				diff = diff[:maxDocsDiffBytesPerFile]
				notes = append(notes, fmt.Sprintf("%s: the diff of %s was cut to %d bytes", page.Path, file, maxDocsDiffBytesPerFile))
			}
			if job.bytes+len(diff) > maxDocsDiffBytesPerPage {
				notes = append(notes, fmt.Sprintf("%s: %s not shown to the model (the page's diffs are over %d bytes)", page.Path, file, maxDocsDiffBytesPerPage))
				continue
			}
			job.bytes += len(diff)
			job.changed = append(job.changed, docs.ChangedFile{Path: file, Diff: diff})
		}
		if len(job.changed) == 0 {
			notes = append(notes, page.Path+": not drafted, none of its changed code can be shown to the model")
			continue
		}
		jobs = append(jobs, job)
	}
	return jobs, notes, nil
}

// runDocsUpdate drafts edits to the pages a diff affects. A model is shown
// each page and the diff of the code it covers and proposes edits; every
// edit must pass the same rules as px1 docs apply and, unless --verify=false,
// a second model call that checks it against the code it cites. Surviving
// edits are written to the working tree for a person to review as a diff.
// It sends repository content to the model provider, so it requires --yes.
func runDocsUpdate(args []string, stdout io.Writer) error {
	o, err := parseDocsUpdateFlags(args)
	if err != nil {
		return err
	}
	key := strings.TrimSpace(os.Getenv("OPENAI_API_KEY"))
	if key == "" && !o.dryRun {
		return errors.New("docs update requires OPENAI_API_KEY in the environment; keys are never accepted as flags")
	}
	root, err := filepath.Abs(o.root)
	if err != nil {
		return err
	}
	impact, err := docsImpact(root, o.base, o.head)
	if err != nil {
		return err
	}
	if current, err := resolveCommit(root, "HEAD"); err != nil || current != impact.Head {
		return errors.New("docs update edits the working tree, which must be checked out at --head")
	}
	redactor := &secretRedactor{}
	policy, err := newPrivacyPolicy(o.excludes)
	if err != nil {
		return err
	}
	jobs, notes, err := docsUpdateJobs(root, impact, o, redactor.Redact)
	if err != nil {
		return err
	}

	type request struct{ system, user string }
	requests := make([]request, len(jobs))
	var inputTokens int64
	fmt.Fprintf(stdout, "Pages whose covered code changed and that were not updated: %d\n", len(jobs))
	for i, job := range jobs {
		system, user, err := docs.DraftPrompt(job.page, job.changed)
		if err != nil {
			return fmt.Errorf("%s: %w", job.page.Path, err)
		}
		requests[i] = request{system, user}
		inputTokens += agent.EstimateTokens(system) + agent.EstimateTokens(user)
		files := make([]string, len(job.changed))
		for n, file := range job.changed {
			files[n] = file.Path
		}
		fmt.Fprintf(stdout, "  %s: the page and the diff of %s (%d bytes)\n", job.page.Path, strings.Join(files, ", "), len(user))
	}
	for _, note := range notes {
		fmt.Fprintln(stdout, "  note:", note)
	}
	if len(jobs) == 0 {
		fmt.Fprintln(stdout, "Nothing to draft.")
		return writeDocsUpdateReport(o.out, docsUpdateReport{Base: impact.Base, Head: impact.Head, Model: o.model, Results: []docs.EditResult{}, Notes: notes})
	}
	fmt.Fprintf(stdout, "About %d input tokens for drafting; the run stops before it could cost more than $%.2f.\n", inputTokens, o.maxCost)
	if o.dryRun {
		fmt.Fprintln(stdout, "\nDry run: nothing was sent to the model provider.")
		return nil
	}
	if !o.yes {
		return errors.New("this would send the content above to " + hostOf(o.endpoint) + "; re-run with --yes to proceed, or --dry-run to preview only")
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	calls := len(jobs) * (1 + docs.MaxDraftEdits)
	meter := agent.NewMeter(agent.Budget{
		MaxInputTokens: 4_000_000, MaxOutputTokens: int64(calls * o.maxOutput), MaxCostMicrousd: usdToMicro(o.maxCost),
		MaxModelCalls: calls, WallTime: 10 * time.Minute,
	}, time.Now(), nil)
	model := agent.Model{
		Client:   llm.Gateway{Providers: map[string]llm.Provider{"openai": llm.OpenAIProvider{APIKey: key, Endpoint: o.endpoint, HTTP: &http.Client{}}}, Timeout: 180 * time.Second},
		Provider: "openai", Name: o.model, MaxOutputTokens: o.maxOutput, Meter: meter,
		Price: llm.Price{InputMicrousdPerMillion: usdToMicro(o.priceIn), OutputMicrousdPerMillion: usdToMicro(o.priceOut), CacheReadMicrousdPerMillion: usdToMicro(o.priceIn)},
	}

	var drafted []docs.Edit
	changedFor := map[string][]docs.ChangedFile{}
	pageFor := map[string]docs.Page{}
	for i, job := range jobs {
		changedFor[job.page.Path], pageFor[job.page.Path] = job.changed, job.page
		reply, err := model.Call(ctx, "docs", "draft", agent.Prompt{System: requests[i].system, User: requests[i].user})
		if err != nil {
			notes = append(notes, fmt.Sprintf("%s: not drafted (%s)", job.page.Path, describeRunError(err)))
			if errors.Is(err, agent.ErrBudgetExhausted) || ctx.Err() != nil {
				break
			}
			continue
		}
		edits, err := docs.DecodeDraft(reply, job.page.Path)
		if err != nil {
			notes = append(notes, fmt.Sprintf("%s: the draft was unusable (%s)", job.page.Path, err))
			continue
		}
		fmt.Fprintf(stdout, "  %s: %d edit(s) drafted\n", job.page.Path, len(edits))
		drafted = append(drafted, edits...)
	}

	results, _, err := docs.PlanEdits(root, impact, drafted)
	if err != nil {
		return err
	}
	var keep []docs.Edit
	for i := range results {
		if !results[i].Applied {
			continue
		}
		if o.verify {
			if problem := docsEditUnsupported(ctx, root, model, results[i].Edit, pageFor[results[i].Edit.Page], changedFor[results[i].Edit.Page], policy.Allow, redactor.Redact); problem != "" {
				results[i].Applied, results[i].Rejected = false, problem
				continue
			}
		}
		keep = append(keep, results[i].Edit)
	}
	if _, err := docs.ApplyEdits(root, impact, keep); err != nil {
		return err
	}

	spend := meter.Spend()
	fmt.Fprintln(stdout)
	fmt.Fprint(stdout, docs.EditsMarkdown(results, o.sourceURL))
	for _, note := range notes {
		fmt.Fprintln(stdout, "note:", note)
	}
	fmt.Fprintf(stdout, "\n%d model call(s), $%.4f. %d secret-like value(s) redacted. Review the result with: git diff -- docs/\n", spend.ModelCalls, float64(spend.CostMicrousd)/1_000_000, redactor.count.Load())
	return writeDocsUpdateReport(o.out, docsUpdateReport{Base: impact.Base, Head: impact.Head, Model: o.model, Results: results, Notes: notes, CostMicrousd: spend.CostMicrousd, ModelCalls: spend.ModelCalls})
}

// docsEditUnsupported asks the model whether an edit follows from the code
// it cites. It returns why the edit must not be applied, or "" when it may.
// An edit that cannot be checked is not applied.
//
// The cited code is read from disk, and a citation may name any file the page
// covers, not only one whose diff was already sent. So the privacy policy and
// the redactor are applied here as they are to the diffs.
func docsEditUnsupported(ctx context.Context, root string, model agent.Model, edit docs.Edit, page docs.Page, changed []docs.ChangedFile, allow func(string) (bool, string), redact func(string) string) string {
	for _, citation := range edit.Citations {
		if allowed, reason := allow(citation.File); !allowed {
			return fmt.Sprintf("could not be checked: %s may not be shown to the model (%s)", citation.File, reason)
		}
	}
	evidence, err := docs.EvidenceFor(root, edit)
	if err != nil {
		return "could not be checked: " + err.Error()
	}
	for i := range evidence {
		evidence[i].Text = redact(evidence[i].Text)
	}
	system, user, err := docs.SupportPrompt(edit, docs.SectionText(page, edit.Heading), evidence, changed)
	if err != nil {
		return "could not be checked: " + err.Error()
	}
	reply, err := model.Call(ctx, "docs", "verify", agent.Prompt{System: system, User: user})
	if err != nil {
		return "could not be checked: " + describeRunError(err)
	}
	if supported, problem := docs.DecodeSupport(reply); !supported {
		return "not supported by the code it cites: " + problem
	}
	return ""
}

func writeDocsUpdateReport(path string, report docsUpdateReport) error {
	if path == "" {
		return nil
	}
	if report.Notes == nil {
		report.Notes = []string{}
	}
	encoded, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(encoded, '\n'), 0o644)
}
