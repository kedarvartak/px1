package agent

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"px1/internal/hosted/llm"
	"px1/internal/hosted/review"
)

// roleModel answers each pipeline role with its own handler. Roles are
// recognized by their system prompts, so the fake is safe under the parallel
// runner.
type roleModel struct {
	mu       sync.Mutex
	handlers map[string]func(call int, request llm.Request) (string, error)
	calls    map[string]int
}

func roleOf(system string) string {
	switch {
	case strings.Contains(system, "You are the planner"):
		return "planner"
	case strings.Contains(system, "review sub-agent"):
		return "sub-agent"
	case strings.Contains(system, "You are the px1 judge"):
		return "judge"
	case strings.Contains(system, "Verify each proposed"):
		return "verifier"
	}
	return "unknown"
}

func (r *roleModel) Complete(_ context.Context, request llm.Request) (llm.Response, error) {
	role := roleOf(request.System)
	r.mu.Lock()
	if r.calls == nil {
		r.calls = map[string]int{}
	}
	r.calls[role]++
	call := r.calls[role]
	handler := r.handlers[role]
	r.mu.Unlock()
	if handler == nil {
		return llm.Response{}, fmt.Errorf("no handler for %s", role)
	}
	text, err := handler(call, request)
	return llm.Response{Text: text, Usage: llm.Usage{InputTokens: 200, OutputTokens: 40}}, err
}

func (r *roleModel) count(role string) int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.calls[role]
}

func findingJSON(path string, start, end int, severity, title string) string {
	return fmt.Sprintf(`{"path":%q,"start_line":%d,"end_line":%d,"severity":%q,"type":"logic","title":%q,"body":"b","evidence":"amount / 100","confidence":0.8}`, path, start, end, severity, title)
}

func pipelineFixture() PipelineInput {
	return PipelineInput{
		Title: "Fix refunds",
		Files: []ReviewFile{
			{Path: "billing/refund.go", Diff: "10\t+func Refund() {\n12\t+\treturn amount / 100\n14\t+}", Changed: []review.DiffRange{{Path: "billing/refund.go", StartLine: 10, EndLine: 14}}},
			{Path: "README.md", Diff: "1\t+docs", Changed: []review.DiffRange{{Path: "README.md", StartLine: 1, EndLine: 1}}},
		},
		Blocks: []review.Block{{ID: "g1", Title: "Refunds", Paths: []string{"billing/refund.go"}}, {ID: "g2", Title: "Docs", Paths: []string{"README.md"}}},
	}
}

func newPipeline(t *testing.T, model *roleModel, budget Budget) (Pipeline, *Trace) {
	t.Helper()
	meter := NewMeter(budget, time.Now(), nil)
	trace := NewTrace(scope.HeadSHA, Plus, nil)
	limiter, _ := NewOrgLimiter(3)
	return Pipeline{
		Model:   Model{Client: model, Provider: "openai", Name: "m", MaxOutputTokens: 500, Meter: meter, Trace: trace, ContentionWait: time.Millisecond, Price: llm.Price{InputMicrousdPerMillion: 1_000_000, OutputMicrousdPerMillion: 4_000_000}},
		Box:     Toolbox{Files: staticFiles{content: "12\tamount / 100"}},
		Scope:   scope,
		Limiter: limiter,
	}, trace
}

func pipelineBudget() Budget {
	b := roomyBudget()
	b.MaxSubAgents = 4
	b.MaxToolCallsPerAgent = 3
	return b
}

const verifierSupportsAll = "Here you go:\n```json\n[{\"id\":\"f1\",\"supported\":true,\"confidence\":0.9,\"reason\":\"the quoted line shows it\"}]\n```"

func happyModel() *roleModel {
	return &roleModel{handlers: map[string]func(int, llm.Request) (string, error){
		"planner": func(int, llm.Request) (string, error) {
			return "```json\n{\"tasks\":[{\"block_id\":\"g1\",\"focus\":\"rounding\",\"files\":[\"billing/refund.go\"],\"extra\":1},{\"block_id\":\"nope\",\"focus\":\"x\"}]}\n```", nil
		},
		"sub-agent": func(call int, _ llm.Request) (string, error) {
			if call == 1 {
				return `{"tool":"read_file","arguments":{"path":"billing/refund.go","start_line":1,"end_line":20}}`, nil
			}
			return `{"findings":[` + findingJSON("billing/refund.go", 12, 12, "P1", "Refund rounds down") + `,` + findingJSON("billing/refund.go", 12, 12, "P1", "Refund rounds down cents") + `,` + findingJSON("other/untouched.go", 3, 3, "P1", "Elsewhere") + `]}`, nil
		},
		"judge": func(int, llm.Request) (string, error) {
			return "[" + findingJSON("billing/refund.go", 12, 12, "P1", "Refund rounds down") + "]", nil
		},
		"verifier": func(int, llm.Request) (string, error) { return verifierSupportsAll, nil },
	}}
}

func TestPipelineProducesVerifiedFindingsEndToEnd(t *testing.T) {
	model := happyModel()
	pipeline, trace := newPipeline(t, model, pipelineBudget())
	var stages []string
	pipeline.OnStage = func(s string) { stages = append(stages, s) }
	result, err := pipeline.Run(context.Background(), pipelineFixture())
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Findings) != 1 || result.Findings[0].Title != "Refund rounds down" || len(result.Unanchored) != 0 {
		t.Fatalf("findings=%#v unanchored=%v notes=%v", result.Findings, result.Unanchored, result.Notes)
	}
	if len(result.Plan) != 1 || result.Plan[0].BlockID != "g1" || result.Candidates != 1 || result.Judged != 1 {
		t.Fatalf("plan=%v candidates=%d judged=%d", result.Plan, result.Candidates, result.Judged)
	}
	if strings.Join(stages, ",") != "planning,investigating,judging,verifying" {
		t.Fatalf("stages = %v", stages)
	}
	if model.count("planner") != 1 || model.count("sub-agent") != 2 || model.count("judge") != 1 || model.count("verifier") != 1 {
		t.Fatalf("calls: %v", model.calls)
	}
	joined := ""
	for _, n := range result.Notes {
		joined += n.Message + ";"
	}
	if !strings.Contains(joined, "which this change did not touch") {
		t.Fatalf("a finding about an untouched file was not reported as dropped: %s", joined)
	}
	roles := map[string]int{}
	for _, event := range trace.File(result.Spend).Events {
		if event.Kind == "model" {
			roles[event.Name]++
		}
	}
	if roles["planner"] != 1 || roles["sub-agent"] != 2 || roles["judge"] != 1 || roles["verifier"] != 1 || result.Spend.ModelCalls != 5 {
		t.Fatalf("model events by role = %v, spend = %#v", roles, result.Spend)
	}
}

func TestVerifiedFindingsOutsideTheChangedLinesAreFlagged(t *testing.T) {
	model := happyModel()
	model.handlers["judge"] = func(int, llm.Request) (string, error) {
		return "[" + findingJSON("billing/refund.go", 12, 12, "P1", "Refund rounds down") + "]", nil
	}
	input := pipelineFixture()
	input.Files[0].Changed = []review.DiffRange{{Path: "billing/refund.go", StartLine: 30, EndLine: 31}}
	pipeline, _ := newPipeline(t, model, pipelineBudget())
	result, err := pipeline.Run(context.Background(), input)
	if err != nil || len(result.Findings) != 1 || len(result.Unanchored) != 1 {
		t.Fatalf("findings=%d unanchored=%d err=%v", len(result.Findings), len(result.Unanchored), err)
	}
}

func TestUnusablePlannerFallsBackToTheLargestGroups(t *testing.T) {
	model := happyModel()
	model.handlers["planner"] = func(int, llm.Request) (string, error) { return "I would start with the tests.", nil }
	pipeline, _ := newPipeline(t, model, pipelineBudget())
	result, err := pipeline.Run(context.Background(), pipelineFixture())
	if err != nil {
		t.Fatal(err)
	}
	fallbackNoted := false
	for _, n := range result.Notes {
		fallbackNoted = fallbackNoted || strings.Contains(n.Message, "generic focus")
	}
	if len(result.Plan) != 2 || result.Plan[0].BlockID != "g1" || !fallbackNoted {
		t.Fatalf("plan=%v notes=%v", result.Plan, result.Notes)
	}
	failing := happyModel()
	failing.handlers["planner"] = func(int, llm.Request) (string, error) { return "", errors.New("503") }
	pipeline, _ = newPipeline(t, failing, pipelineBudget())
	if result, err = pipeline.Run(context.Background(), pipelineFixture()); err != nil || len(result.Plan) != 2 {
		t.Fatalf("a failing planner must not fail the review: %v %v", result.Plan, err)
	}
}

func TestJudgeCanNarrowButAnInvalidJudgeCannotEraseCandidates(t *testing.T) {
	invented := happyModel()
	invented.handlers["judge"] = func(int, llm.Request) (string, error) {
		return "[" + findingJSON("billing/refund.go", 12, 12, "P0", "Escalated") + "," + findingJSON("billing/elsewhere.go", 1, 1, "P2", "Invented") + "]", nil
	}
	pipeline, _ := newPipeline(t, invented, pipelineBudget())
	result, err := pipeline.Run(context.Background(), pipelineFixture())
	if err != nil || result.Judged != 1 || len(result.Findings) != 1 || result.Findings[0].Severity != "P1" {
		t.Fatalf("judged=%d findings=%#v err=%v", result.Judged, result.Findings, err)
	}
	joined := ""
	for _, n := range result.Notes {
		joined += n.Message + ";"
	}
	if !strings.Contains(joined, "every judged entry was invalid") {
		t.Fatalf("fallback not explained: %s", joined)
	}

	dropsAll := happyModel()
	dropsAll.handlers["judge"] = func(int, llm.Request) (string, error) { return "[]", nil }
	pipeline, _ = newPipeline(t, dropsAll, pipelineBudget())
	result, err = pipeline.Run(context.Background(), pipelineFixture())
	if err != nil || len(result.Findings) != 0 || dropsAll.count("verifier") != 0 {
		t.Fatalf("an explicit empty judgment should be honoured: %#v %v", result.Findings, err)
	}
}

func TestOnlyVerifiedFindingsAreReported(t *testing.T) {
	two := happyModel()
	two.handlers["sub-agent"] = func(int, llm.Request) (string, error) {
		return `{"findings":[` + findingJSON("billing/refund.go", 12, 12, "P1", "Refund rounds down") + `,` + findingJSON("billing/refund.go", 14, 14, "P2", "Unclear brace") + `]}`, nil
	}
	two.handlers["judge"] = func(int, llm.Request) (string, error) {
		return "[" + findingJSON("billing/refund.go", 12, 12, "P1", "Refund rounds down") + "," + findingJSON("billing/refund.go", 14, 14, "P2", "Unclear brace") + "]", nil
	}
	two.handlers["verifier"] = func(int, llm.Request) (string, error) {
		return `[{"id":"f1","supported":true,"confidence":0.9,"reason":"clear"},{"id":"f2","supported":false,"confidence":0.9,"reason":"the line shows no problem"}]`, nil
	}
	pipeline, _ := newPipeline(t, two, pipelineBudget())
	result, err := pipeline.Run(context.Background(), pipelineFixture())
	if err != nil || len(result.Findings) != 1 || result.Findings[0].Title != "Refund rounds down" {
		t.Fatalf("findings=%#v err=%v", result.Findings, err)
	}
	joined := ""
	for _, n := range result.Notes {
		joined += n.Message + ";"
	}
	if !strings.Contains(joined, `verifier dropped "Unclear brace": the line shows no problem`) {
		t.Fatalf("verifier drop not recorded: %s", joined)
	}

	broken := happyModel()
	broken.handlers["verifier"] = func(int, llm.Request) (string, error) { return "I could not verify these.", nil }
	pipeline, _ = newPipeline(t, broken, pipelineBudget())
	result, err = pipeline.Run(context.Background(), pipelineFixture())
	if err != nil || len(result.Findings) != 0 || len(result.Unverified) != 1 || broken.count("verifier") != 2 {
		t.Fatalf("findings=%d unverified=%d verifier calls=%d err=%v", len(result.Findings), len(result.Unverified), broken.count("verifier"), err)
	}
}

func TestNothingFoundSkipsJudgeAndVerifierWithoutClaimingSafety(t *testing.T) {
	model := happyModel()
	model.handlers["sub-agent"] = func(int, llm.Request) (string, error) { return `{"findings":[]}`, nil }
	pipeline, _ := newPipeline(t, model, pipelineBudget())
	result, err := pipeline.Run(context.Background(), pipelineFixture())
	if err != nil || len(result.Findings) != 0 || model.count("judge") != 0 || model.count("verifier") != 0 {
		t.Fatalf("findings=%d judge=%d verifier=%d err=%v", len(result.Findings), model.count("judge"), model.count("verifier"), err)
	}
	last := result.Notes[len(result.Notes)-1].Message
	if !strings.Contains(last, "not a statement that the change is correct") {
		t.Fatalf("note = %q", last)
	}
}

func TestOneFailingSubAgentDoesNotSinkTheReview(t *testing.T) {
	model := happyModel()
	model.handlers["planner"] = func(int, llm.Request) (string, error) { return "", errors.New("down") }
	base := model.handlers["sub-agent"]
	model.handlers["sub-agent"] = func(call int, request llm.Request) (string, error) {
		if strings.Contains(request.Prompt, "README.md") {
			return "", errors.New("provider error")
		}
		return base(call, request)
	}
	pipeline, _ := newPipeline(t, model, pipelineBudget())
	result, err := pipeline.Run(context.Background(), pipelineFixture())
	if err != nil || len(result.Findings) != 1 || result.Run.Complete {
		t.Fatalf("findings=%d complete=%v err=%v", len(result.Findings), result.Run.Complete, err)
	}
	joined := ""
	for _, n := range result.Notes {
		joined += n.Message + ";"
	}
	if !strings.Contains(joined, "change group g2 did not finish: sub-agent failed") || strings.Contains(joined, "provider error") {
		t.Fatalf("notes = %s", joined)
	}
}

func TestExhaustedBudgetStopsThePlanner(t *testing.T) {
	budget := pipelineBudget()
	budget.MaxModelCalls = 0
	pipeline, _ := newPipeline(t, happyModel(), budget)
	if _, err := pipeline.Run(context.Background(), pipelineFixture()); !errors.Is(err, ErrBudgetExhausted) {
		t.Fatalf("err = %v", err)
	}
}

func TestPipelineRejectsAnIncompleteSetup(t *testing.T) {
	pipeline, _ := newPipeline(t, happyModel(), pipelineBudget())
	empty := pipelineFixture()
	empty.Files = nil
	if _, err := pipeline.Run(context.Background(), empty); err == nil {
		t.Fatal("a review with no files ran")
	}
	pipeline.Limiter = nil
	if _, err := pipeline.Run(context.Background(), pipelineFixture()); err == nil {
		t.Fatal("a review with no limiter ran")
	}
}

func TestLenientDecodersToleratePlannerAndJudgeNoise(t *testing.T) {
	blocks := []review.Block{{ID: "g1", Paths: []string{"a.go"}}, {ID: "g2", Paths: []string{"b.go"}}}
	budget := Budget{MaxSubAgents: 1}
	tasks, problems := DecodePlanLenient("Sure!\n```json\n{\"tasks\":[{\"block_id\":\"g1\",\"focus\":\"\",\"files\":[\"../x\",\"./a.go\"]},{\"block_id\":\"g2\",\"focus\":\"y\"}],\"why\":\"because\"}\n```", blocks, budget)
	if len(tasks) != 1 || tasks[0].Files[0] != "a.go" || len(tasks[0].Files) != 1 || !strings.Contains(tasks[0].Focus, "correctness") || len(problems) != 1 {
		t.Fatalf("tasks=%#v problems=%v", tasks, problems)
	}
	if tasks, _ := DecodePlanLenient("no json here", blocks, budget); len(tasks) != 0 {
		t.Fatal("prose produced tasks")
	}
	candidates := []review.Finding{{Path: "a.go", StartLine: 1, EndLine: 5, Severity: "P1"}}
	kept, dropped, err := DecodeJudgmentLenient("{\"findings\":["+findingJSON("a.go", 2, 2, "P1", "ok")+","+findingJSON("a.go", 2, 2, "P0", "up")+"]}", candidates)
	if err != nil || len(kept) != 1 || len(dropped) != 1 || !strings.Contains(dropped[0].Reason, "raises severity") {
		t.Fatalf("kept=%v dropped=%v err=%v", kept, dropped, err)
	}
	if _, _, err := DecodeJudgmentLenient(`{"nothing":true}`, candidates); err == nil {
		t.Fatal("an object without findings accepted")
	}
}

func TestProviderErrorTextNeverReachesNotesOrTheTrace(t *testing.T) {
	leaky := llm.ProviderError{ProviderName: "openai", StatusCode: 400, Detail: "Incorrect API key provided: sk-proj-****abcd"}
	if got := describeError(leaky); got != "the provider returned status 400" || strings.Contains(got, "sk-proj") {
		t.Fatalf("describeError = %q", got)
	}
	if !strings.Contains(leaky.Error(), "sk-proj-****abcd") {
		t.Fatal("the operator's terminal error should keep the provider message")
	}
	model := happyModel()
	model.handlers["verifier"] = func(int, llm.Request) (string, error) { return "", leaky }
	pipeline, trace := newPipeline(t, model, pipelineBudget())
	result, err := pipeline.Run(context.Background(), pipelineFixture())
	if err != nil {
		t.Fatal(err)
	}
	everything := ""
	for _, n := range result.Notes {
		everything += n.Message + ";"
	}
	for _, event := range trace.File(result.Spend).Events {
		everything += event.Name + event.Detail + event.Agent + ";"
	}
	if strings.Contains(everything, "sk-proj") || strings.Contains(everything, "Incorrect API key") {
		t.Fatalf("provider error text leaked into notes or the trace: %s", everything)
	}
	if !strings.Contains(everything, "the provider returned status 400") || len(result.Findings) != 0 {
		t.Fatalf("a failed verifier should be explained without detail and report nothing: %s", everything)
	}
}

func TestDiffContextIncludesRemovedLinesNextToTheFinding(t *testing.T) {
	diff := "@@ -40,3 +40,3 @@\n40\t ctx\n-\told := a > b\n41\t+new := a >= b\n42\t ctx2\n-\tunrelated removal\n90\t far away\n"
	got := diffContext(diff, 41, 41)
	for _, want := range []string{"-\told := a > b", "41\t+new := a >= b", "40\t ctx", "42\t ctx2"} {
		if !strings.Contains(got, want) {
			t.Fatalf("context lacks %q:\n%s", want, got)
		}
	}
	if strings.Contains(got, "far away") {
		t.Fatalf("lines outside the window must be left out:\n%s", got)
	}
	if strings.Contains(got, "unrelated removal") {
		t.Fatalf("a removal that precedes a line outside the window is not context:\n%s", got)
	}
}

func TestDiffContextWithoutRemovalsIsUnchanged(t *testing.T) {
	diff := "10\t ctx\n11\t+added\n12\t ctx\n"
	if got := diffContext(diff, 11, 11); got != diff[:len(diff)-1] {
		t.Fatalf("got %q", got)
	}
}
