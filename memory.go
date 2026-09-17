package main

import (
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

const (
	memoryVersion     = 1
	memorySnippetMax  = 4 << 10
	memoryAnchorMin   = 12
	memoryMaxRecords  = 2000
	memoryNoteMax     = 400
	memoryWhySwitched = "Reviewer switched from: "
)

type decisionRecord struct {
	ID           string     `json:"id"`
	Path         string     `json:"path"`
	LineStart    int        `json:"lineStart"`
	LineEnd      int        `json:"lineEnd"`
	Decision     string     `json:"decision"`
	Why          string     `json:"why,omitempty"`
	Alternatives []string   `json:"alternatives,omitempty"`
	Status       string     `json:"status"`
	Snippet      string     `json:"snippet"`
	SessionID    string     `json:"sessionId,omitempty"`
	PinID        string     `json:"pinId,omitempty"`
	RecordedAt   time.Time  `json:"recordedAt"`
	SupersededAt *time.Time `json:"supersededAt,omitempty"`
	Note         string     `json:"note,omitempty"`
}

type decisionView struct {
	decisionRecord
	Located     bool `json:"located"`
	CurrentFrom int  `json:"currentFrom,omitempty"`
	CurrentTo   int  `json:"currentTo,omitempty"`
}

type decisionChallenge struct {
	decisionRecord
	BaselineFrom int `json:"baselineFrom"`
	BaselineTo   int `json:"baselineTo"`
}

type decisionStore struct {
	Version   int              `json:"version"`
	Root      string           `json:"root"`
	Decisions []decisionRecord `json:"decisions"`
}

type decisionMemory struct {
	root, path string
	mu         sync.Mutex
	store      *decisionStore
}

func newDecisionMemory(root string) *decisionMemory {
	m := &decisionMemory{root: root}
	if state := reviewStateRoot(); state != "" {
		key := (&reviewManager{root: root}).workspaceKey()
		m.path = filepath.Join(filepath.Dir(state), "decisions", key+".json")
	}
	return m
}

func (m *decisionMemory) loadLocked() *decisionStore {
	if m.store != nil {
		return m.store
	}
	m.store = &decisionStore{Version: memoryVersion, Root: m.root}
	if m.path == "" {
		return m.store
	}
	b, err := os.ReadFile(m.path)
	if err != nil {
		return m.store
	}
	var s decisionStore
	if json.Unmarshal(b, &s) == nil && s.Version == memoryVersion && s.Root == m.root {
		m.store = &s
	}
	return m.store
}

func (m *decisionMemory) saveLocked() error {
	if m.path == "" {
		return errors.New("cannot determine px1 state directory")
	}
	b, err := json.Marshal(m.store)
	if err != nil {
		return err
	}
	return writeAtomic(m.path, b, 0o600)
}

func (m *decisionMemory) Record(r decisionRecord) (decisionRecord, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	s := m.loadLocked()
	if len(r.Snippet) > memorySnippetMax {
		r.Snippet = strings.ToValidUTF8(r.Snippet[:memorySnippetMax], "")
	}
	if r.RecordedAt.IsZero() {
		r.RecordedAt = time.Now().UTC()
	}
	if r.ID == "" {
		r.ID = r.RecordedAt.Format("20060102T150405.000000000")
	}
	kept := s.Decisions[:0]
	for _, d := range s.Decisions {
		if r.PinID == "" || d.PinID != r.PinID {
			kept = append(kept, d)
		}
	}
	s.Decisions = append(kept, r)
	if over := len(s.Decisions) - memoryMaxRecords; over > 0 {
		s.Decisions = append([]decisionRecord(nil), s.Decisions[over:]...)
	}
	return r, m.saveLocked()
}

func (m *decisionMemory) ForgetPin(pinID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	s := m.loadLocked()
	kept := s.Decisions[:0]
	removed := false
	for _, d := range s.Decisions {
		if d.PinID == pinID {
			removed = true
			continue
		}
		kept = append(kept, d)
	}
	s.Decisions = kept
	if !removed {
		return nil
	}
	return m.saveLocked()
}

func (m *decisionMemory) Supersede(id, note string) (decisionRecord, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	s := m.loadLocked()
	for i := range s.Decisions {
		d := &s.Decisions[i]
		if d.ID != id {
			continue
		}
		now := time.Now().UTC()
		d.Status, d.SupersededAt, d.Note = "superseded", &now, clip(note, memoryNoteMax)
		if err := m.saveLocked(); err != nil {
			return decisionRecord{}, err
		}
		return *d, nil
	}
	return decisionRecord{}, errors.New("decision not found")
}

func (m *decisionMemory) active(path string) []decisionRecord {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []decisionRecord
	for _, d := range m.loadLocked().Decisions {
		if d.Status != "superseded" && (path == "" || d.Path == path) {
			out = append(out, d)
		}
	}
	return out
}

func (m *decisionMemory) ForFile(path string) []decisionView {
	content, err := os.ReadFile(filepath.Join(m.root, filepath.FromSlash(path)))
	out := []decisionView{}
	for _, d := range m.active(path) {
		v := decisionView{decisionRecord: d}
		if err == nil {
			v.CurrentFrom, v.CurrentTo, v.Located = locateSnippet(content, d.Snippet, d.LineStart)
		}
		out = append(out, v)
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].RecordedAt.After(out[j].RecordedAt) })
	return out
}

type anchorLine struct {
	n    int
	text string
}

func anchorLines(b []byte) []anchorLine {
	var out []anchorLine
	for i, line := range strings.Split(strings.ReplaceAll(string(b), "\r\n", "\n"), "\n") {
		if t := strings.Join(strings.Fields(line), " "); t != "" {
			out = append(out, anchorLine{n: i + 1, text: t})
		}
	}
	return out
}

func locateSnippet(content []byte, snippet string, near int) (int, int, bool) {
	want := anchorLines([]byte(snippet))
	size := 0
	for _, l := range want {
		size += len(l.text)
	}
	if len(want) == 0 || size < memoryAnchorMin {
		return 0, 0, false
	}
	have := anchorLines(content)
	best, bestDist := -1, 0
	for i := 0; i+len(want) <= len(have); i++ {
		match := true
		for k := range want {
			if have[i+k].text != want[k].text {
				match = false
				break
			}
		}
		if !match {
			continue
		}
		dist := have[i].n - near
		if dist < 0 {
			dist = -dist
		}
		if best == -1 || dist < bestDist {
			best, bestDist = i, dist
		}
	}
	if best == -1 {
		return 0, 0, false
	}
	return have[best].n, have[best+len(want)-1].n, true
}

func anchorable(snippet string) bool {
	size := 0
	for _, l := range anchorLines([]byte(snippet)) {
		size += len(l.text)
	}
	return size >= memoryAnchorMin
}

func (m *reviewManager) ActiveID() string {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.active == nil {
		return ""
	}
	return m.active.ID
}

func (m *reviewManager) DismissDecision(id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.active == nil {
		return errors.New("no active review session")
	}
	for _, d := range m.active.DismissedDecisions {
		if d == id {
			return nil
		}
	}
	m.active.DismissedDecisions = append(m.active.DismissedDecisions, id)
	return m.saveLocked()
}

func (m *reviewManager) Challenges(mem *decisionMemory) ([]decisionChallenge, error) {
	q, err := m.Queue()
	if err != nil {
		return nil, err
	}
	m.mu.Lock()
	if m.active == nil {
		m.mu.Unlock()
		return nil, errors.New("no active review session")
	}
	sessionID := m.active.ID
	baseDir := filepath.Join(m.sessionDir(sessionID), "files")
	dismissed := map[string]bool{}
	for _, id := range m.active.DismissedDecisions {
		dismissed[id] = true
	}
	m.mu.Unlock()

	out := []decisionChallenge{}
	for _, item := range q.Items {
		if item.BaselineHash == "<deleted>" {
			continue
		}
		records := mem.active(item.Path)
		if len(records) == 0 {
			continue
		}
		baseline, err := os.ReadFile(filepath.Join(baseDir, filepath.FromSlash(item.Path)))
		if err != nil {
			continue
		}
		current, _ := os.ReadFile(filepath.Join(m.root, filepath.FromSlash(item.Path)))
		for _, d := range records {
			if d.SessionID == sessionID || dismissed[d.ID] {
				continue
			}
			from, to, inBaseline := locateSnippet(baseline, d.Snippet, d.LineStart)
			if !inBaseline {
				continue
			}
			if _, _, still := locateSnippet(current, d.Snippet, from); still {
				continue
			}
			out = append(out, decisionChallenge{decisionRecord: d, BaselineFrom: from, BaselineTo: to})
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Path != out[j].Path {
			return out[i].Path < out[j].Path
		}
		return out[i].BaselineFrom < out[j].BaselineFrom
	})
	return out, nil
}

func (s *Server) rememberPin(p reviewPin, snippet string) error {
	if !anchorable(snippet) {
		return errors.New("pinned lines are too short to remember")
	}
	r := decisionRecord{
		Path:         p.Path,
		LineStart:    p.LineStart,
		LineEnd:      p.LineEnd,
		Decision:     p.Decision,
		Why:          p.Why,
		Alternatives: p.Alternatives,
		Status:       "accepted",
		Snippet:      snippet,
		SessionID:    s.review.ActiveID(),
		PinID:        p.ID,
	}
	if p.Status == "switched" {
		r.Status = "switched"
		r.Decision = p.Choice
		r.Why = memoryWhySwitched + p.Decision
		alts := []string{p.Decision}
		for _, a := range p.Alternatives {
			if a != p.Choice {
				alts = append(alts, a)
			}
		}
		r.Alternatives = alts
	}
	_, err := s.memory.Record(r)
	return err
}

func (s *Server) handleDecisions(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		fail(w, http.StatusMethodNotAllowed, "GET only")
		return
	}
	_, path, ok := s.safePath(r.URL.Query().Get("path"))
	if !ok || path == "" {
		fail(w, http.StatusBadRequest, "bad path")
		return
	}
	writeJSON(w, map[string]any{"path": path, "decisions": s.memory.ForFile(path)})
}

func (s *Server) handleReviewChallenges(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		fail(w, http.StatusMethodNotAllowed, "GET only")
		return
	}
	challenges, err := s.review.Challenges(s.memory)
	if err != nil {
		fail(w, http.StatusConflict, err.Error())
		return
	}
	writeJSON(w, map[string]any{"challenges": challenges})
}

func (s *Server) handleDecisionResolve(w http.ResponseWriter, r *http.Request) {
	if !localPost(w, r) {
		return
	}
	var req struct {
		ID     string `json:"id"`
		Action string `json:"action"`
		Note   string `json:"note"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		fail(w, http.StatusBadRequest, "invalid decision action")
		return
	}
	switch req.Action {
	case "supersede":
		d, err := s.memory.Supersede(req.ID, req.Note)
		if err != nil {
			fail(w, http.StatusConflict, err.Error())
			return
		}
		writeJSON(w, map[string]any{"decision": d})
	case "dismiss":
		if err := s.review.DismissDecision(req.ID); err != nil {
			fail(w, http.StatusConflict, err.Error())
			return
		}
		writeJSON(w, map[string]any{"dismissed": req.ID})
	default:
		fail(w, http.StatusBadRequest, "action must be supersede or dismiss")
	}
}
