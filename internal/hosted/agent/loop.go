package agent

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"px1/internal/hosted/review"
)

const (
	defaultMaxFindings    = 20
	defaultMaxPromptBytes = 120 << 10
	maxInvalidReplies     = 2
)

// Loop runs one sub-agent: it asks the model for a JSON action, runs the tool
// it names, shows the model the result, and repeats until the model returns
// findings. The protocol is plain JSON over the single-prompt gateway, so it
// works with any provider; the whole transcript is replayed each turn.
type Loop struct {
	Model          Model
	Box            Toolbox
	Scope          Scope
	MaxFindings    int
	MaxPromptBytes int
}

type action struct {
	Tool      string          `json:"tool"`
	Arguments json.RawMessage `json:"arguments"`
	Findings  json.RawMessage `json:"findings"`
}

type step struct {
	N         int             `json:"n"`
	Tool      string          `json:"tool"`
	Arguments json.RawMessage `json:"arguments"`
	OK        bool            `json:"ok"`
	Result    string          `json:"result"`
	Truncated bool            `json:"truncated,omitempty"`
}

func (l Loop) Run(ctx context.Context, in SubAgentInput) ([]review.Finding, error) {
	if l.Model.Meter == nil {
		return nil, errors.New("sub-agent loop needs a metered model")
	}
	in.Tools = l.Box.Specs()
	base, err := SubAgentPrompt(in)
	if err != nil {
		return nil, err
	}
	agentID := in.Block.ID
	var steps []step
	var notes []string
	toolsUsed, invalid, ignoredTools := 0, 0, 0
	final := false
	for turn := 0; turn < in.ToolCalls+maxInvalidReplies+2; turn++ {
		user, _ := l.render(base.User, steps, notes, final)
		prompt := Prompt{System: base.System, User: user}
		reply, err := l.Model.Call(ctx, agentID, "sub-agent", prompt)
		if err != nil {
			return nil, err
		}
		act, err := parseAction(reply)
		if err != nil {
			if invalid++; invalid >= maxInvalidReplies {
				return nil, errors.New("sub-agent replied with invalid JSON twice in a row")
			}
			notes = append(notes, "Your last reply was not usable: "+err.Error()+". Reply with exactly one JSON object.")
			continue
		}
		invalid = 0
		if act.Findings != nil {
			return l.finish(agentID, act.Findings)
		}
		if final || toolsUsed >= in.ToolCalls {
			if ignoredTools++; ignoredTools > 1 {
				return nil, fmt.Errorf("%w: the model kept requesting tools after being told to finish", ErrToolCallLimit)
			}
			final = true
			continue
		}
		if err := l.Model.Meter.UseTool(agentID); err != nil {
			return nil, err
		}
		toolsUsed++
		steps = append(steps, l.runTool(ctx, agentID, toolsUsed, act))
		// Once old results had to be dropped to fit, the conversation is
		// full: ask for findings rather than lose more context.
		if _, trimmed := l.render(base.User, steps, notes, false); toolsUsed >= in.ToolCalls || trimmed > 0 {
			final = true
		}
	}
	return nil, fmt.Errorf("%w: no findings after the turn limit", ErrToolCallLimit)
}

func (l Loop) runTool(ctx context.Context, agentID string, n int, act action) step {
	call := Call{Name: act.Tool, Arguments: act.Arguments}
	var result Result
	var err error
	if l.Model.Trace != nil {
		result, err = l.Model.Trace.Dispatch(ctx, l.Box, l.Scope, agentID, call)
	} else {
		result, err = l.Box.Dispatch(ctx, l.Scope, call)
	}
	recorded := step{N: n, Tool: act.Tool, Arguments: act.Arguments, OK: err == nil, Result: result.Content, Truncated: result.Truncated}
	if err != nil {
		// Tool errors are written to be safe to show a model: no git stderr,
		// local paths, or provider text.
		recorded.Result = err.Error()
	}
	return recorded
}

func (l Loop) finish(agentID string, raw json.RawMessage) ([]review.Finding, error) {
	max := l.MaxFindings
	if max <= 0 {
		max = defaultMaxFindings
	}
	kept, dropped, err := review.DecodeFindingsLenient(raw, max)
	if err != nil {
		return nil, err
	}
	if l.Model.Trace != nil {
		for _, drop := range dropped {
			l.Model.Trace.Note(agentID, "dropped_finding", fmt.Sprintf("%q: %s", drop.Title, drop.Reason))
		}
	}
	return kept, nil
}

func (l Loop) promptLimit() int {
	if l.MaxPromptBytes > 0 {
		return l.MaxPromptBytes
	}
	return defaultMaxPromptBytes
}

// render appends the transcript to the task and reports how many of the
// oldest results had to be replaced by a marker to fit the limit. Tool results
// are repository content, so each step is JSON inside an untrusted tag.
func (l Loop) render(base string, steps []step, notes []string, final bool) (string, int) {
	limit := l.promptLimit()
	elided := 0
	for {
		var out strings.Builder
		out.WriteString(base)
		for i, s := range steps {
			if i < elided {
				s.Result, s.Truncated = "[result omitted to keep the conversation short]", false
			}
			block, err := untrusted("tool_step", s)
			if err != nil {
				continue
			}
			out.WriteString("\n" + block)
		}
		for _, note := range notes {
			out.WriteString("\nNOTE: " + note)
		}
		if final {
			out.WriteString("\nNOTE: You cannot call more tools. Reply now with exactly {\"findings\":[...]}.")
		}
		if out.Len() <= limit || elided >= len(steps) {
			return out.String(), elided
		}
		elided++
	}
}

// parseAction reads the first JSON object in a reply, tolerating a code fence
// or surrounding prose, and requires exactly one of "tool" or "findings".
func parseAction(reply string) (action, error) {
	start := strings.IndexByte(reply, '{')
	if start < 0 {
		return action{}, errors.New("no JSON object found")
	}
	var act action
	if err := json.NewDecoder(strings.NewReader(reply[start:])).Decode(&act); err != nil {
		return action{}, errors.New("the JSON object could not be parsed")
	}
	if bytes.Equal(bytes.TrimSpace(act.Findings), []byte("null")) {
		act.Findings = nil
	}
	hasTool := strings.TrimSpace(act.Tool) != ""
	switch {
	case hasTool && act.Findings != nil:
		return action{}, errors.New("the object had both a tool call and findings")
	case !hasTool && act.Findings == nil:
		return action{}, errors.New(`the object needs a "tool" or a "findings" key`)
	}
	if len(bytes.TrimSpace(act.Arguments)) == 0 {
		act.Arguments = json.RawMessage(`{}`)
	}
	act.Tool = strings.TrimSpace(act.Tool)
	return act, nil
}
