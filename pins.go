package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
)

const (
	pinMaxBatch       = 50
	pinMaxDecision    = 200
	pinMaxWhy         = 400
	pinMaxAlternative = 120
	pinMaxAlternates  = 4
	pinPromptBytes    = 60 << 10
)

type reviewPin struct {
	ID           string    `json:"id"`
	Path         string    `json:"path"`
	LineStart    int       `json:"lineStart"`
	LineEnd      int       `json:"lineEnd"`
	Decision     string    `json:"decision"`
	Why          string    `json:"why,omitempty"`
	Alternatives []string  `json:"alternatives,omitempty"`
	Impact       int       `json:"impact"`
	RangeHash    string    `json:"rangeHash"`
	Status       string    `json:"status"`
	Choice       string    `json:"choice,omitempty"`
	Source       string    `json:"source"`
	CreatedAt    time.Time `json:"createdAt"`
	UpdatedAt    time.Time `json:"updatedAt"`
}

type reviewPinView struct {
	reviewPin
	Stale bool `json:"stale"`
}

type pinInput struct {
	Path         string   `json:"path"`
	LineStart    int      `json:"lineStart"`
	LineEnd      int      `json:"lineEnd"`
	Decision     string   `json:"decision"`
	Why          string   `json:"why"`
	Alternatives []string `json:"alternatives"`
	Impact       int      `json:"impact"`
}

func clip(s string, max int) string {
	s = strings.Join(strings.Fields(s), " ")
	if len(s) <= max {
		return s
	}
	return strings.TrimSpace(strings.ToValidUTF8(s[:max], ""))
}

func (m *reviewManager) rangeHashLocked(path string, l1, l2 int) (string, error) {
	b, err := os.ReadFile(filepath.Join(m.root, filepath.FromSlash(path)))
	if err != nil {
		return "", err
	}
	start, end, err := lineSpan(b, l1, l2)
	if err != nil {
		return "", err
	}
	return hashBytes(b[start:end]), nil
}

func (m *reviewManager) AddPins(in []pinInput, source string, replace bool) ([]reviewPinView, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.active == nil {
		return nil, errors.New("no active review session")
	}
	if len(in) > pinMaxBatch {
		return nil, fmt.Errorf("at most %d pins per request", pinMaxBatch)
	}
	now := time.Now().UTC()
	fresh := make([]reviewPin, 0, len(in))
	for i, p := range in {
		p.Decision = clip(p.Decision, pinMaxDecision)
		if p.Decision == "" {
			return nil, fmt.Errorf("pin %d has no decision", i+1)
		}
		hash, err := m.rangeHashLocked(p.Path, p.LineStart, p.LineEnd)
		if err != nil {
			return nil, fmt.Errorf("pin %d (%s:%s): %w", i+1, p.Path, lineRef(p.LineStart, p.LineEnd), err)
		}
		alts := make([]string, 0, pinMaxAlternates)
		for _, a := range p.Alternatives {
			if a = clip(a, pinMaxAlternative); a != "" && len(alts) < pinMaxAlternates {
				alts = append(alts, a)
			}
		}
		impact := p.Impact
		if impact < 1 || impact > 3 {
			impact = 2
		}
		fresh = append(fresh, reviewPin{
			ID:           now.Format("20060102T150405.000000000") + "-" + strconv.Itoa(i),
			Path:         p.Path,
			LineStart:    p.LineStart,
			LineEnd:      p.LineEnd,
			Decision:     p.Decision,
			Why:          clip(p.Why, pinMaxWhy),
			Alternatives: alts,
			Impact:       impact,
			RangeHash:    hash,
			Status:       "proposed",
			Source:       source,
			CreatedAt:    now,
			UpdatedAt:    now,
		})
	}
	if replace {
		kept := m.active.Pins[:0]
		for _, p := range m.active.Pins {
			if p.Status != "proposed" || p.Source != source {
				kept = append(kept, p)
			}
		}
		m.active.Pins = kept
	}
	m.active.Pins = append(m.active.Pins, fresh...)
	if err := m.saveLocked(); err != nil {
		return nil, err
	}
	return m.pinViewsLocked(), nil
}

func (m *reviewManager) pinViewsLocked() []reviewPinView {
	out := make([]reviewPinView, 0, len(m.active.Pins))
	for _, p := range m.active.Pins {
		hash, err := m.rangeHashLocked(p.Path, p.LineStart, p.LineEnd)
		out = append(out, reviewPinView{reviewPin: p, Stale: err != nil || hash != p.RangeHash})
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Impact != out[j].Impact {
			return out[i].Impact > out[j].Impact
		}
		if out[i].Path != out[j].Path {
			return out[i].Path < out[j].Path
		}
		return out[i].LineStart < out[j].LineStart
	})
	return out
}

func (m *reviewManager) Pins() ([]reviewPinView, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.active == nil {
		return nil, errors.New("no active review session")
	}
	return m.pinViewsLocked(), nil
}

func (m *reviewManager) Pin(id string) (reviewPinView, error) {
	pins, err := m.Pins()
	if err != nil {
		return reviewPinView{}, err
	}
	for _, p := range pins {
		if p.ID == id {
			return p, nil
		}
	}
	return reviewPinView{}, errors.New("pin not found")
}

func (m *reviewManager) SetPinStatus(id, status, choice string) (reviewPin, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.active == nil {
		return reviewPin{}, errors.New("no active review session")
	}
	if status != "proposed" && status != "accepted" && status != "switched" {
		return reviewPin{}, errors.New("pin status must be proposed, accepted or switched")
	}
	for i := range m.active.Pins {
		p := &m.active.Pins[i]
		if p.ID != id {
			continue
		}
		p.Status, p.Choice, p.UpdatedAt = status, "", time.Now().UTC()
		if status == "switched" {
			p.Choice = clip(choice, pinMaxAlternative)
		}
		if err := m.saveLocked(); err != nil {
			return reviewPin{}, err
		}
		return *p, nil
	}
	return reviewPin{}, errors.New("pin not found")
}

func (m *reviewManager) ExplainPrompt() (string, error) {
	q, err := m.Queue()
	if err != nil {
		return "", err
	}
	var body strings.Builder
	truncated := false
	for _, item := range q.Items {
		if item.CurrentHash == "<deleted>" {
			continue
		}
		var section string
		if item.BaselineHash == "<deleted>" {
			b, err := os.ReadFile(filepath.Join(m.root, filepath.FromSlash(item.Path)))
			if err != nil {
				continue
			}
			var numbered strings.Builder
			for i, line := range strings.Split(strings.TrimRight(string(b), "\n"), "\n") {
				fmt.Fprintf(&numbered, "%d: %s\n", i+1, line)
			}
			section = fmt.Sprintf("### New file %s\n%s\n", item.Path, numbered.String())
		} else {
			diff, err := m.BaselineDiff(item.Path)
			if err != nil || diff == "" {
				continue
			}
			section = fmt.Sprintf("### Diff %s\n%s\n", item.Path, diff)
		}
		if body.Len()+len(section) > pinPromptBytes {
			truncated = true
			continue
		}
		body.WriteString(section)
	}
	if body.Len() == 0 {
		return "", errors.New("no reviewable changes to explain")
	}
	var b strings.Builder
	b.WriteString("You are annotating a code change for a human reviewer. Do not edit any files.\n\n")
	b.WriteString("List every non-obvious decision this change makes: design, library, data shape, security, error handling, limits. Skip trivial edits.\n\n")
	b.WriteString("Reply with only a JSON array, no prose, where each element is:\n")
	b.WriteString(`{"path":"<file>","lineStart":<n>,"lineEnd":<n>,"decision":"<what was chosen, under 12 words>","why":"<reason, under 15 words>","alternatives":["<option not taken, under 6 words>"],"impact":<1 low, 2 medium, 3 high>}`)
	b.WriteString("\n\nLine numbers refer to the current file (the + side of each diff). Order by impact, highest first. At most 12 elements.\n\n")
	b.WriteString(body.String())
	if truncated {
		b.WriteString("\n(Some files were omitted for size.)\n")
	}
	return b.String(), nil
}

func parsePinsOutput(out string) ([]pinInput, error) {
	var pins []pinInput
	start := strings.Index(out, "[")
	end := strings.LastIndex(out, "]")
	for start != -1 && end > start {
		if err := json.Unmarshal([]byte(out[start:end+1]), &pins); err == nil {
			return pins, nil
		}
		next := strings.Index(out[start+1:], "[")
		if next == -1 {
			break
		}
		start += next + 1
	}
	return nil, errors.New("harness did not return a JSON array of decisions")
}

func (s *Server) handleReviewPins(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		pins, err := s.review.Pins()
		if err != nil {
			fail(w, http.StatusConflict, err.Error())
			return
		}
		writeJSON(w, map[string]any{"pins": pins, "explain": s.explainState()})
	case http.MethodPost:
		if !localPost(w, r) {
			return
		}
		var req struct {
			Pins    []pinInput `json:"pins"`
			Replace bool       `json:"replace"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			fail(w, http.StatusBadRequest, "invalid pins")
			return
		}
		if !s.cleanPinPaths(w, req.Pins) {
			return
		}
		pins, err := s.review.AddPins(req.Pins, "agent", req.Replace)
		if err != nil {
			fail(w, http.StatusConflict, err.Error())
			return
		}
		writeJSON(w, map[string]any{"pins": pins})
	default:
		fail(w, http.StatusMethodNotAllowed, "GET or POST only")
	}
}

func (s *Server) cleanPinPaths(w http.ResponseWriter, pins []pinInput) bool {
	for i := range pins {
		_, clean, ok := s.safePath(pins[i].Path)
		if !ok || clean == "" {
			fail(w, http.StatusBadRequest, "bad pin path: "+pins[i].Path)
			return false
		}
		pins[i].Path = clean
	}
	return true
}

func (s *Server) explainState() map[string]any {
	s.pinMu.Lock()
	defer s.pinMu.Unlock()
	return map[string]any{"job": s.pinJob, "error": s.pinErr}
}

func (s *Server) handleReviewPinsExplain(w http.ResponseWriter, r *http.Request) {
	if !localPost(w, r) || !s.agentOrFail(w) {
		return
	}
	prompt, err := s.review.ExplainPrompt()
	if err != nil {
		fail(w, http.StatusConflict, err.Error())
		return
	}
	job, err := s.agent.Explain(prompt, func(stdout string, runErr error) {
		msg := ""
		if runErr != nil {
			msg = runErr.Error()
		} else if pins, err := parsePinsOutput(stdout); err != nil {
			msg = err.Error()
		} else if _, err := s.review.AddPins(nil, "explain", true); err != nil {
			msg = err.Error()
		} else {
			valid := make([]pinInput, 0, len(pins))
			for _, p := range pins {
				if _, clean, ok := s.safePath(p.Path); ok && clean != "" {
					p.Path = clean
					if _, err := s.review.AddPins([]pinInput{p}, "explain", false); err == nil {
						valid = append(valid, p)
					}
				}
			}
			if len(valid) == 0 && len(pins) > 0 {
				msg = "harness decisions did not match the changed lines"
			}
		}
		s.pinMu.Lock()
		s.pinErr = msg
		s.pinMu.Unlock()
	})
	if err != nil {
		code := http.StatusBadRequest
		if errors.Is(err, errAgentBusy) || errors.Is(err, errAgentNone) {
			code = http.StatusConflict
		}
		fail(w, code, err.Error())
		return
	}
	s.pinMu.Lock()
	s.pinJob, s.pinErr = job.ID, ""
	s.pinMu.Unlock()
	writeJSON(w, job)
}

func (s *Server) handleReviewPinStatus(w http.ResponseWriter, r *http.Request) {
	if !localPost(w, r) {
		return
	}
	var req struct {
		ID     string `json:"id"`
		Status string `json:"status"`
		Choice string `json:"choice"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		fail(w, http.StatusBadRequest, "invalid pin status")
		return
	}
	if req.Status != "switched" {
		pin, err := s.review.SetPinStatus(req.ID, req.Status, "")
		if err != nil {
			fail(w, http.StatusConflict, err.Error())
			return
		}
		remembered := false
		if pin.Status == "accepted" {
			abs, _, ok := s.resolvePath(pin.Path)
			if snippet, err := readLineRange(abs, pin.LineStart, pin.LineEnd); ok && err == nil {
				remembered = s.rememberPin(pin, snippet) == nil
			}
		} else {
			_ = s.memory.ForgetPin(pin.ID)
		}
		writeJSON(w, map[string]any{"pin": pin, "remembered": remembered})
		return
	}
	if !s.agentOrFail(w) {
		return
	}
	choice := clip(req.Choice, pinMaxAlternative)
	if choice == "" {
		fail(w, http.StatusBadRequest, "choose an alternative to switch to")
		return
	}
	current, err := s.review.Pin(req.ID)
	if err != nil {
		fail(w, http.StatusConflict, err.Error())
		return
	}
	if current.Stale {
		fail(w, http.StatusConflict, "these lines changed since the decision was pinned")
		return
	}
	abs, rel, ok := s.resolvePath(current.Path)
	if !ok {
		fail(w, http.StatusBadRequest, "bad pin path")
		return
	}
	pin, err := s.review.SetPinStatus(req.ID, "switched", choice)
	if err != nil {
		fail(w, http.StatusConflict, err.Error())
		return
	}
	job, err := s.agent.StartWithDone(abs, rel, current.LineStart, current.LineEnd, switchInstruction(current.reviewPin, choice), false, func(_ string, runErr error) {
		if runErr != nil {
			return
		}
		latest, err := s.review.Pin(req.ID)
		if err != nil || latest.Status != "switched" {
			return
		}
		if snippet, err := readLineRange(abs, latest.LineStart, latest.LineEnd); err == nil {
			_ = s.rememberPin(latest.reviewPin, snippet)
		}
	})
	if err != nil {
		_, _ = s.review.SetPinStatus(req.ID, "proposed", "")
		code := http.StatusBadRequest
		if errors.Is(err, errAgentBusy) || errors.Is(err, errAgentDirty) {
			code = http.StatusConflict
		}
		fail(w, code, err.Error())
		return
	}
	writeJSON(w, map[string]any{"pin": pin, "job": job})
}

func switchInstruction(p reviewPin, choice string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "The reviewer rejected this decision: %s.\n", p.Decision)
	if p.Why != "" {
		fmt.Fprintf(&b, "Original reasoning: %s.\n", p.Why)
	}
	fmt.Fprintf(&b, "Rework the implementation to use this instead: %s.\n", choice)
	b.WriteString("Update every place in the workspace that depends on the old decision, and keep unrelated work intact.")
	return b.String()
}
