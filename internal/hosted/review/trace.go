package review

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
)

const (
	MaxTraceEvents      = 2_000
	MaxTraceDetailBytes = 512
	MaxTracePreview     = 512
)

// TraceEvent is one step of an agentic review: a model call, a tool call, a
// finished sub-agent task, or a note such as a discarded finding. It records what was asked and how much came back,
// never provider error text, and never linked-issue content.
type TraceEvent struct {
	Seq          int    `json:"seq"`
	Agent        string `json:"agent"`
	Kind         string `json:"kind"`
	Name         string `json:"name"`
	Detail       string `json:"detail,omitempty"`
	Preview      string `json:"preview,omitempty"`
	ResultBytes  int    `json:"result_bytes,omitempty"`
	Truncated    bool   `json:"truncated,omitempty"`
	Failed       bool   `json:"failed,omitempty"`
	InputTokens  int64  `json:"input_tokens,omitempty"`
	OutputTokens int64  `json:"output_tokens,omitempty"`
	ElapsedMS    int64  `json:"elapsed_ms"`
}

type TraceSpend struct {
	InputTokens  int64  `json:"input_tokens"`
	OutputTokens int64  `json:"output_tokens"`
	CostMicrousd int64  `json:"cost_microusd"`
	ModelCalls   int    `json:"model_calls"`
	ToolCalls    int    `json:"tool_calls"`
	Exhausted    string `json:"exhausted,omitempty"`
}

// TraceFile is the revision-pinned record rendered as "How px1 reviewed this".
type TraceFile struct {
	Revision string       `json:"revision"`
	Tier     string       `json:"tier"`
	Spend    TraceSpend   `json:"spend"`
	Events   []TraceEvent `json:"events"`
	Dropped  int          `json:"dropped,omitempty"`
}

var traceKinds = map[string]bool{"model": true, "tool": true, "task": true, "note": true}

// DecodeTraceFile accepts a trace only for the exact report head and within
// the event and text bounds a recorder can produce.
func DecodeTraceFile(data []byte, head string) (TraceFile, error) {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	var trace TraceFile
	if err := decoder.Decode(&trace); err != nil {
		return TraceFile{}, fmt.Errorf("decode review trace: %w", err)
	}
	if _, err := decoder.Token(); err != io.EOF {
		return TraceFile{}, errors.New("review trace contains trailing data")
	}
	if trace.Revision != head {
		return TraceFile{}, errors.New("review trace revision does not match report head")
	}
	if trace.Tier != "plus" && trace.Tier != "apex" {
		return TraceFile{}, errors.New("review trace tier must be plus or apex")
	}
	if len(trace.Events) > MaxTraceEvents || trace.Dropped < 0 {
		return TraceFile{}, fmt.Errorf("review trace exceeds %d events", MaxTraceEvents)
	}
	for i, event := range trace.Events {
		if event.Seq != i+1 || !traceKinds[event.Kind] || strings.TrimSpace(event.Agent) == "" || strings.TrimSpace(event.Name) == "" ||
			len(event.Agent) > 200 || len(event.Name) > 100 || len(event.Detail) > MaxTraceDetailBytes || len(event.Preview) > MaxTracePreview ||
			event.ResultBytes < 0 || event.InputTokens < 0 || event.OutputTokens < 0 || event.ElapsedMS < 0 {
			return TraceFile{}, fmt.Errorf("review trace event %d is invalid", i+1)
		}
	}
	if trace.Events == nil {
		trace.Events = []TraceEvent{}
	}
	return trace, nil
}
