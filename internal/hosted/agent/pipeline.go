package agent

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"

	"px1/internal/hosted/llm"
	"px1/internal/hosted/review"
)

// ReviewFile is one changed file as the models see it. Diff is the unified
// diff with new-file line numbers on added and context lines; Changed lists
// the new-side line ranges that were added or modified.
type ReviewFile struct {
	Path    string
	Diff    string
	Changed []review.DiffRange
}

type PipelineInput struct {
	Title, Body string
	Files       []ReviewFile
	Blocks      []review.Block
}

type StageNote struct{ Stage, Message string }

type PipelineResult struct {
	// Findings survived the verifier. Only these may be shown as verified.
	Findings []review.Finding
	// Unanchored are verified findings whose lines are not all inside a changed
	// range. They are still in Findings; this says which ones.
	Unanchored []review.SummaryFinding
	// Unverified are candidates that could not be verified, for local
	// inspection only; they must not be presented as verified findings.
	Unverified []review.Finding
	Plan       []PlanTask
	Run        RunResult
	Candidates int
	Judged     int
	Notes      []StageNote
	Spend      Spend
}

// Pipeline runs plan → parallel sub-agents → judge → verifier for one review.
// Every stage that depends on imperfect model output has a fallback that
// narrows rather than widens the result.
type Pipeline struct {
	Model         Model
	Box           Toolbox
	Scope         Scope
	Limiter       *OrgLimiter
	MinConfidence float64
	OnPartial     func(Partial)
	// OnStage is called as each stage starts, for progress output.
	OnStage func(stage string)
}

func (p Pipeline) stage(name string) {
	if p.OnStage != nil {
		p.OnStage(name)
	}
}

func (p Pipeline) Run(ctx context.Context, in PipelineInput) (PipelineResult, error) {
	var result PipelineResult
	if p.Model.Meter == nil || p.Limiter == nil || len(in.Blocks) == 0 || len(in.Files) == 0 {
		return result, errors.New("pipeline needs a metered model, limiter, change groups, and files")
	}
	budget := p.Model.Meter.budget
	note := func(stage, format string, args ...any) {
		message := fmt.Sprintf(format, args...)
		result.Notes = append(result.Notes, StageNote{Stage: stage, Message: message})
		if p.Model.Trace != nil {
			p.Model.Trace.Note(stage, "stage_note", message)
		}
	}
	finish := func(err error) (PipelineResult, error) {
		result.Spend = p.Model.Meter.Spend()
		return result, err
	}
	files := map[string]ReviewFile{}
	var changedPaths []string
	var changed []review.DiffRange
	for _, file := range in.Files {
		files[file.Path] = file
		changedPaths = append(changedPaths, file.Path)
		changed = append(changed, file.Changed...)
	}
	sort.Strings(changedPaths)

	// 1. Plan.
	p.stage("planning")
	plan, err := p.plan(ctx, in, budget, changedPaths, files, note)
	if err != nil {
		return finish(err)
	}
	result.Plan = plan

	// 2. Sub-agents.
	p.stage("investigating")
	inputs := map[string]SubAgentInput{}
	blocks := map[string]review.Block{}
	for _, block := range in.Blocks {
		blocks[block.ID] = block
	}
	for _, task := range plan {
		diff := blockDiff(blocks[task.BlockID], files, note)
		inputs[task.BlockID] = SubAgentInput{Task: task, Block: blocks[task.BlockID], Diff: diff, ToolCalls: budget.MaxToolCallsPerAgent}
	}
	runner := Runner{Limiter: p.Limiter, Trace: p.Model.Trace, OnPartial: p.OnPartial, SubAgent: func(ctx context.Context, task PlanTask, _ *Meter) ([]review.Finding, error) {
		return Loop{Model: p.Model, Box: p.Box, Scope: p.Scope}.Run(ctx, inputs[task.BlockID])
	}}
	run, err := runner.Run(ctx, p.Scope.InstallationID, plan, p.Model.Meter)
	if err != nil {
		return finish(err)
	}
	result.Run = run
	for _, task := range plan {
		if outcome := run.Outcomes[task.BlockID]; outcome.Outcome != TaskCompleted {
			note("investigating", "change group %s did not finish: %s", task.BlockID, outcome.Reason)
		}
	}

	// 3. Keep only candidates about changed files, then collapse duplicates.
	var candidates []review.Finding
	for _, finding := range run.Candidates {
		if _, ok := files[finding.Path]; !ok {
			note("investigating", "dropped a finding about %s, which this change did not touch", finding.Path)
			continue
		}
		candidates = append(candidates, finding)
	}
	candidates = review.RankAndCapFindings(candidates, MaxCandidates)
	result.Candidates = len(candidates)
	if len(candidates) == 0 {
		note("investigating", "no candidate findings; this is not a statement that the change is correct")
		return finish(nil)
	}

	// 4. Judge: merge and narrow. Never widen.
	p.stage("judging")
	judged := p.judge(ctx, candidates, note)
	result.Judged = len(judged)

	// 5. Verify. Only verified findings are reported as findings.
	p.stage("verifying")
	verified, err := p.verify(ctx, judged, files, note)
	if err != nil {
		result.Unverified = judged
		note("verifying", "verification did not complete (%s); %d candidates are not reported as verified", describeError(err), len(judged))
		return finish(nil)
	}
	result.Findings = review.RankAndCapFindings(verified, MaxJudgedFindings)
	if len(changed) > 0 && len(result.Findings) > 0 {
		_, unanchored, err := review.MapFindingsToDiff(result.Findings, changed)
		if err == nil {
			result.Unanchored = unanchored
		}
	}
	return finish(nil)
}

func (p Pipeline) plan(ctx context.Context, in PipelineInput, budget Budget, changedPaths []string, files map[string]ReviewFile, note func(string, string, ...any)) ([]PlanTask, error) {
	fallback := func(why string) []PlanTask {
		note("planning", "%s; reviewing the largest change groups with a generic focus", why)
		return FallbackPlan(in.Blocks, func(b review.Block) int {
			total := 0
			for _, path := range b.Paths {
				total += len(files[path].Diff)
			}
			return total
		}, budget)
	}
	prompt, err := PlannerPrompt(PlanInput{Title: in.Title, Body: in.Body, Blocks: in.Blocks, Tools: p.Box.Specs(), Budget: budget, ChangedPaths: changedPaths})
	if err != nil {
		return nil, err
	}
	planner := p.Model
	planner.MaxOutputTokens = min(p.Model.MaxOutputTokens, 2_000)
	reply, err := planner.Call(ctx, "planner", "planner", prompt)
	if errors.Is(err, ErrBudgetExhausted) || ctx.Err() != nil {
		return nil, err
	}
	if err != nil {
		return fallback("the planner call failed"), nil
	}
	tasks, problems := DecodePlanLenient(reply, in.Blocks, budget)
	for _, problem := range problems {
		note("planning", "%s", problem)
	}
	if len(tasks) == 0 {
		return fallback("the planner returned no usable tasks"), nil
	}
	return tasks, nil
}

func (p Pipeline) judge(ctx context.Context, candidates []review.Finding, note func(string, string, ...any)) []review.Finding {
	prompt, err := JudgePrompt(candidates)
	if err != nil {
		note("judging", "judge skipped: %v", err)
		return candidates
	}
	judge := p.Model
	judge.MaxOutputTokens = max(p.Model.MaxOutputTokens, min(len(candidates)*400, 12_000))
	reply, err := judge.Call(ctx, "judge", "judge", prompt)
	if err != nil {
		note("judging", "the judge call failed; keeping all %d candidates", len(candidates))
		return candidates
	}
	kept, dropped, err := DecodeJudgmentLenient(reply, candidates)
	if err != nil {
		note("judging", "the judge reply was unusable (%s); keeping all %d candidates", describeError(err), len(candidates))
		return candidates
	}
	for _, drop := range dropped {
		note("judging", "dropped judged finding %q: %s", drop.Title, drop.Reason)
	}
	// An explicit empty list is the judge discarding every candidate, which is
	// its job. A reply whose every entry was invalid is a broken judge, and
	// must not erase real candidates.
	if len(kept) == 0 && len(dropped) > 0 {
		note("judging", "every judged entry was invalid; keeping all %d candidates", len(candidates))
		return candidates
	}
	note("judging", "%d candidates merged into %d findings", len(candidates), len(kept))
	return kept
}

func (p Pipeline) verify(ctx context.Context, findings []review.Finding, files map[string]ReviewFile, note func(string, string, ...any)) ([]review.Finding, error) {
	if len(findings) == 0 {
		return nil, nil
	}
	candidates := make([]review.VerificationCandidate, len(findings))
	byID := map[string]review.Finding{}
	for i, finding := range findings {
		id := fmt.Sprintf("f%d", i+1)
		candidates[i] = review.VerificationCandidate{ID: id, Path: finding.Path, StartLine: finding.StartLine, EndLine: finding.EndLine, Title: finding.Title, Evidence: finding.Evidence, Context: diffContext(files[finding.Path].Diff, finding.StartLine, finding.EndLine)}
		byID[id] = finding
	}
	model := p.Model
	model.MaxOutputTokens = max(p.Model.MaxOutputTokens, min(len(findings)*200+500, 6_000))
	verifier := review.FindingVerifier{Client: cleaningClient{model: model}, Provider: p.Model.Provider, Model: p.Model.Name, PromptVersion: "px1-agentic-2", MinConfidence: p.MinConfidence, MaxOutputTokens: model.MaxOutputTokens}
	var result review.VerificationResult
	var err error
	for attempt := 0; attempt < 2; attempt++ {
		if result, err = verifier.Verify(ctx, candidates); err == nil || errors.Is(err, ErrBudgetExhausted) || ctx.Err() != nil {
			break
		}
		note("verifying", "verification attempt %d failed: %s", attempt+1, describeError(err))
	}
	if err != nil {
		return nil, err
	}
	kept := make([]review.Finding, 0, len(result.Kept))
	for _, candidate := range result.Kept {
		kept = append(kept, byID[candidate.ID])
	}
	ids := make([]string, 0, len(result.Dropped))
	for id := range result.Dropped {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for _, id := range ids {
		note("verifying", "verifier dropped %q: %s", byID[id].Title, result.Dropped[id])
	}
	return kept, nil
}

// cleaningClient lets the metered model act as the verifier's client. The
// verifier insists on a bare JSON array and rejects unknown fields; models
// often wrap the array in a code fence or a sentence. The client extracts the
// array, and the verifier's strict rules then apply to what it contains.
type cleaningClient struct{ model Model }

func (c cleaningClient) Complete(ctx context.Context, request llm.Request) (llm.Response, error) {
	text, err := c.model.Call(ctx, "verifier", "verifier", Prompt{System: request.System, User: request.Prompt})
	if err != nil {
		return llm.Response{}, err
	}
	if raw, jsonErr := firstJSON(text); jsonErr == nil {
		text = string(raw)
	}
	return llm.Response{Text: text}, nil
}

// blockDiff joins the numbered diffs of a change group's files, cutting at the
// prompt limit rather than failing the whole group.
func blockDiff(block review.Block, files map[string]ReviewFile, note func(string, string, ...any)) string {
	var out strings.Builder
	for _, path := range block.Paths {
		file, ok := files[path]
		if !ok {
			continue
		}
		section := "=== " + path + " ===\n" + file.Diff + "\n"
		if out.Len()+len(section) > maxDiffBytes {
			room := maxDiffBytes - out.Len() - 80
			if room > 200 {
				out.WriteString(clip(section, room))
			}
			out.WriteString("\n[diff truncated by px1: use read_file for the rest]\n")
			note("investigating", "the diff for change group %s was cut at %d bytes", block.ID, maxDiffBytes)
			break
		}
		out.WriteString(section)
	}
	return out.String()
}

const (
	// The window is at most 13 numbered lines (the finding plus six either
	// side). Each is shortened so all of them fit, and the limit holds them
	// with room for some removed lines, so nothing in the window is lost to
	// the final cut.
	maxKeptLineBytes    = 200
	diffContextLimit    = 13*(maxKeptLineBytes+1) + 400
	maxRemovedRunLines  = 8
	maxRemovedLineBytes = 160
)

// diffContext returns the numbered diff lines near a finding, bounded so a
// verifier request stays small. Removed lines ("-") directly before a kept line
// are included: a finding that a change introduced something cannot be checked
// without seeing what the change replaced. The numbered lines are the evidence,
// so they are never crowded out: removed lines are shortened, capped to the
// last few of each run, and dropped first when the limit is reached.
func diffContext(diff string, start, end int) string {
	type entry struct {
		text    string
		removed bool
	}
	var entries []entry
	var pending []string
	for _, line := range strings.Split(diff, "\n") {
		number, _, ok := strings.Cut(line, "\t")
		if !ok {
			pending = nil
			continue
		}
		if number == "-" {
			pending = append(pending, clip(line, maxRemovedLineBytes))
			continue
		}
		var n int
		if _, err := fmt.Sscanf(number, "%d", &n); err != nil {
			pending = nil
			continue
		}
		if n >= start-6 && n <= end+6 {
			if len(pending) > maxRemovedRunLines {
				pending = pending[len(pending)-maxRemovedRunLines:]
			}
			for _, r := range pending {
				entries = append(entries, entry{r, true})
			}
			entries = append(entries, entry{clip(line, maxKeptLineBytes), false})
		}
		pending = nil
	}
	budget := diffContextLimit
	for _, e := range entries {
		if !e.removed {
			budget -= len(e.text) + 1
		}
	}
	var picked []string
	for _, e := range entries {
		if e.removed {
			if budget < len(e.text)+1 {
				continue
			}
			budget -= len(e.text) + 1
		}
		picked = append(picked, e.text)
	}
	return clip(strings.Join(picked, "\n"), diffContextLimit)
}

// describeError classifies a failure for reports and traces. Provider error
// text is for the operator's terminal only, so a provider failure is reduced to
// its status code.
func describeError(err error) string {
	var provider llm.ProviderError
	switch {
	case errors.As(err, &provider):
		return fmt.Sprintf("the provider returned status %d", provider.StatusCode)
	case errors.Is(err, ErrBudgetExhausted):
		return "the review budget ran out"
	case errors.Is(err, context.DeadlineExceeded):
		return "the review time limit was reached"
	}
	return "the reply was not in the expected format"
}
