package agent

import (
	"context"
	"encoding/json"
	"strings"
	"sync"
	"testing"
	"time"

	"px1/internal/hosted/review"
)

func TestTraceRecordsToolCallsWithoutIssueContent(t *testing.T) {
	trace := NewTrace(scope.HeadSHA, Plus, nil)
	box := Toolbox{Files: &fakeFiles{}, Issues: &fakeIssues{}}
	if _, err := trace.Dispatch(context.Background(), box, scope, "g1", call(ReadFile, `{"path":"a.go","start_line":1,"end_line":2}`)); err != nil {
		t.Fatal(err)
	}
	if _, err := trace.Dispatch(context.Background(), box, scope, "g1", call(ReadIssue, `{"key":"ENG-12"}`)); err != nil {
		t.Fatal(err)
	}
	if _, err := trace.Dispatch(context.Background(), box, scope, "g1", call(ReadFile, `{"path":"../x","start_line":1,"end_line":2}`)); err == nil {
		t.Fatal("escape accepted")
	}
	trace.Model("g1", "sub-agent", 120, 30, false, time.Second)
	file := trace.File(Spend{ModelCalls: 1, ToolCalls: 3})
	if len(file.Events) != 4 || file.Events[0].Seq != 1 || file.Events[3].Seq != 4 {
		t.Fatalf("events: %#v", file.Events)
	}
	read, issue, rejected := file.Events[0], file.Events[1], file.Events[2]
	if !read.Truncated || read.ResultBytes == 0 || len(read.Preview) > review.MaxTracePreview || read.Preview == "" {
		t.Fatalf("read event: %#v", read)
	}
	if issue.Preview != "" || issue.ResultBytes == 0 {
		t.Fatalf("issue content leaked into the trace: %#v", issue)
	}
	if !rejected.Failed || rejected.Preview != "" {
		t.Fatalf("rejected call: %#v", rejected)
	}
	data, _ := json.Marshal(file)
	if _, err := review.DecodeTraceFile(data, scope.HeadSHA); err != nil {
		t.Fatalf("recorder output must pass the report decoder: %v", err)
	}
}

func TestTraceIsBoundedAndConcurrencySafe(t *testing.T) {
	trace := NewTrace(scope.HeadSHA, Apex, nil)
	var wg sync.WaitGroup
	for i := 0; i < review.MaxTraceEvents+50; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); trace.Model("judge", "judge", 1, 1, false, 0) }()
	}
	wg.Wait()
	file := trace.File(Spend{})
	if len(file.Events) != review.MaxTraceEvents || file.Dropped != 50 {
		t.Fatalf("events=%d dropped=%d", len(file.Events), file.Dropped)
	}
	for i, event := range file.Events {
		if event.Seq != i+1 {
			t.Fatalf("event %d has seq %d", i, event.Seq)
		}
	}
}

func TestRunnerRecordsTaskOutcomes(t *testing.T) {
	limiter, _ := NewOrgLimiter(2)
	trace := NewTrace(scope.HeadSHA, Plus, nil)
	runner := Runner{Limiter: limiter, Trace: trace, SubAgent: func(_ context.Context, task PlanTask, _ *Meter) ([]review.Finding, error) {
		if task.BlockID == "bad" {
			return nil, ErrToolCallLimit
		}
		return nil, nil
	}}
	if _, err := runner.Run(context.Background(), 7, tasksFor("ok", "bad"), NewMeter(runBudget(2, time.Minute), time.Now(), nil)); err != nil {
		t.Fatal(err)
	}
	outcomes := map[string]review.TraceEvent{}
	for _, event := range trace.File(Spend{}).Events {
		outcomes[event.Agent] = event
	}
	if outcomes["ok"].Name != "completed" || outcomes["bad"].Name != "failed" || !strings.Contains(outcomes["bad"].Detail, "tool-call limit") {
		t.Fatalf("task events: %#v", outcomes)
	}
}
