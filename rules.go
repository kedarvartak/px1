package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	ruleVersion       = 1
	rulePatternMax    = 300
	ruleGlobMax       = 200
	ruleMessageMax    = 400
	ruleMaxRules      = 500
	ruleMaxHits       = 500
	ruleMaxHitsPerKey = 20
	ruleTeamFile      = ".px1/rules.json"
)

type reviewRule struct {
	ID        string     `json:"id"`
	Pattern   string     `json:"pattern"`
	Glob      string     `json:"glob,omitempty"`
	Message   string     `json:"message"`
	Enabled   bool       `json:"enabled"`
	Source    string     `json:"source"`
	Origin    string     `json:"origin,omitempty"`
	CreatedAt time.Time  `json:"createdAt"`
	Hits      int        `json:"hits"`
	LastHitAt *time.Time `json:"lastHitAt,omitempty"`
}

type ruleHit struct {
	Key     string `json:"key"`
	RuleID  string `json:"ruleId"`
	Message string `json:"message"`
	Source  string `json:"source"`
	Origin  string `json:"origin,omitempty"`
	Path    string `json:"path"`
	Line    int    `json:"line"`
	Text    string `json:"text"`
}

type ruleStore struct {
	Version int          `json:"version"`
	Root    string       `json:"root"`
	Rules   []reviewRule `json:"rules"`
}

type ruleInput struct {
	Pattern string `json:"pattern"`
	Glob    string `json:"glob"`
	Message string `json:"message"`
	Origin  string `json:"origin"`
}

type ruleMemory struct {
	root, path string
	mu         sync.Mutex
	store      *ruleStore
	seen       map[string]bool
}

func newRuleMemory(root string) *ruleMemory {
	m := &ruleMemory{root: root, seen: map[string]bool{}}
	if state := reviewStateRoot(); state != "" {
		key := (&reviewManager{root: root}).workspaceKey()
		m.path = filepath.Join(filepath.Dir(state), "rules", key+".json")
	}
	return m
}

func (m *ruleMemory) loadLocked() *ruleStore {
	if m.store != nil {
		return m.store
	}
	m.store = &ruleStore{Version: ruleVersion, Root: m.root}
	if m.path == "" {
		return m.store
	}
	if b, err := os.ReadFile(m.path); err == nil {
		var s ruleStore
		if json.Unmarshal(b, &s) == nil && s.Version == ruleVersion && s.Root == m.root {
			m.store = &s
		}
	}
	return m.store
}

func (m *ruleMemory) saveLocked() error {
	if m.path == "" {
		return errors.New("cannot determine px1 state directory")
	}
	b, err := json.Marshal(m.store)
	if err != nil {
		return err
	}
	return writeAtomic(m.path, b, 0o600)
}

func globRegexp(glob string) (*regexp.Regexp, error) {
	glob = strings.TrimSpace(glob)
	if glob == "" {
		return nil, nil
	}
	if !strings.Contains(glob, "/") {
		glob = "**/" + glob
	}
	var b strings.Builder
	b.WriteString("^")
	for i := 0; i < len(glob); i++ {
		switch c := glob[i]; {
		case strings.HasPrefix(glob[i:], "**/"):
			b.WriteString("(?:.*/)?")
			i += 2
		case strings.HasPrefix(glob[i:], "**"):
			b.WriteString(".*")
			i++
		case c == '*':
			b.WriteString("[^/]*")
		case c == '?':
			b.WriteString("[^/]")
		default:
			b.WriteString(regexp.QuoteMeta(string(c)))
		}
	}
	b.WriteString("$")
	return regexp.Compile(b.String())
}

func validateRule(in ruleInput) (ruleInput, error) {
	in.Pattern = strings.TrimSpace(in.Pattern)
	in.Glob = strings.TrimSpace(in.Glob)
	in.Message = clip(in.Message, ruleMessageMax)
	in.Origin = clip(in.Origin, ruleGlobMax)
	if in.Pattern == "" || len(in.Pattern) > rulePatternMax {
		return in, fmt.Errorf("pattern must be between 1 and %d bytes", rulePatternMax)
	}
	re, err := regexp.Compile(in.Pattern)
	if err != nil {
		return in, fmt.Errorf("pattern: %w", err)
	}
	if re.MatchString("") {
		return in, errors.New("pattern matches every line")
	}
	if len(in.Glob) > ruleGlobMax {
		return in, fmt.Errorf("glob must be at most %d bytes", ruleGlobMax)
	}
	if _, err := globRegexp(in.Glob); err != nil {
		return in, fmt.Errorf("glob: %w", err)
	}
	if in.Message == "" {
		return in, errors.New("rule needs a message")
	}
	return in, nil
}

func (m *ruleMemory) Add(in ruleInput) (reviewRule, error) {
	in, err := validateRule(in)
	if err != nil {
		return reviewRule{}, err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	s := m.loadLocked()
	if len(s.Rules) >= ruleMaxRules {
		return reviewRule{}, fmt.Errorf("at most %d rules per workspace", ruleMaxRules)
	}
	now := time.Now().UTC()
	r := reviewRule{ID: now.Format("20060102T150405.000000000"), Pattern: in.Pattern, Glob: in.Glob, Message: in.Message, Origin: in.Origin, Enabled: true, Source: "review", CreatedAt: now}
	s.Rules = append(s.Rules, r)
	return r, m.saveLocked()
}

func (m *ruleMemory) Update(id string, in ruleInput, enabled *bool, remove bool) (reviewRule, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	s := m.loadLocked()
	for i := range s.Rules {
		r := &s.Rules[i]
		if r.ID != id {
			continue
		}
		if remove {
			removed := *r
			s.Rules = append(s.Rules[:i], s.Rules[i+1:]...)
			return removed, m.saveLocked()
		}
		if in.Pattern != "" || in.Message != "" || in.Glob != "" {
			merged := ruleInput{Pattern: r.Pattern, Glob: r.Glob, Message: r.Message, Origin: r.Origin}
			if in.Pattern != "" {
				merged.Pattern = in.Pattern
			}
			if in.Message != "" {
				merged.Message = in.Message
			}
			if in.Glob != "" {
				merged.Glob = in.Glob
			}
			valid, err := validateRule(merged)
			if err != nil {
				return reviewRule{}, err
			}
			r.Pattern, r.Glob, r.Message = valid.Pattern, valid.Glob, valid.Message
		}
		if enabled != nil {
			r.Enabled = *enabled
		}
		return *r, m.saveLocked()
	}
	return reviewRule{}, errors.New("rule not found")
}

func (m *ruleMemory) teamRules() ([]reviewRule, error) {
	b, err := os.ReadFile(filepath.Join(m.root, filepath.FromSlash(ruleTeamFile)))
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var file struct {
		Rules []ruleInput `json:"rules"`
	}
	if err := json.Unmarshal(b, &file); err != nil {
		return nil, fmt.Errorf("%s: %w", ruleTeamFile, err)
	}
	out := make([]reviewRule, 0, len(file.Rules))
	for i, in := range file.Rules {
		valid, err := validateRule(in)
		if err != nil {
			return out, fmt.Errorf("%s rule %d: %w", ruleTeamFile, i+1, err)
		}
		out = append(out, reviewRule{ID: "team-" + strconv.Itoa(i+1), Pattern: valid.Pattern, Glob: valid.Glob, Message: valid.Message, Origin: valid.Origin, Enabled: true, Source: "team"})
	}
	return out, nil
}

func (m *ruleMemory) All() ([]reviewRule, error) {
	m.mu.Lock()
	own := append([]reviewRule(nil), m.loadLocked().Rules...)
	m.mu.Unlock()
	team, err := m.teamRules()
	return append(team, own...), err
}

func (m *ruleMemory) noteHits(hits []ruleHit) {
	m.mu.Lock()
	defer m.mu.Unlock()
	s := m.loadLocked()
	now := time.Now().UTC()
	changed := false
	for _, h := range hits {
		if m.seen[h.Key] {
			continue
		}
		m.seen[h.Key] = true
		for i := range s.Rules {
			if s.Rules[i].ID == h.RuleID {
				s.Rules[i].Hits++
				s.Rules[i].LastHitAt = &now
				changed = true
			}
		}
	}
	if changed {
		_ = m.saveLocked()
	}
}

func addedLines(diff string) map[int]string {
	out := map[int]string{}
	line := 0
	for _, raw := range strings.Split(diff, "\n") {
		if m := hunkHeader.FindStringSubmatch(raw); m != nil {
			line, _ = strconv.Atoi(m[1])
			continue
		}
		if line == 0 || raw == "" || strings.HasPrefix(raw, `\`) {
			continue
		}
		switch raw[0] {
		case '+':
			out[line] = raw[1:]
			line++
		case ' ':
			line++
		}
	}
	return out
}

var hunkHeader = regexp.MustCompile(`^@@ -\d+(?:,\d+)? \+(\d+)(?:,\d+)? @@`)

func ruleHitKey(ruleID, p, text string) string {
	return ruleID + ":" + p + ":" + hashBytes([]byte(strings.TrimSpace(text)))[:16]
}

func (m *reviewManager) DismissRuleHit(key string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.active == nil {
		return errors.New("no active review session")
	}
	for _, k := range m.active.DismissedRuleHits {
		if k == key {
			return nil
		}
	}
	m.active.DismissedRuleHits = append(m.active.DismissedRuleHits, key)
	return m.saveLocked()
}

func (m *reviewManager) RuleHits(rules []reviewRule) ([]ruleHit, error) {
	q, err := m.Queue()
	if err != nil {
		return nil, err
	}
	m.mu.Lock()
	dismissed := map[string]bool{}
	if m.active != nil {
		for _, k := range m.active.DismissedRuleHits {
			dismissed[k] = true
		}
	}
	m.mu.Unlock()

	type compiled struct {
		rule reviewRule
		re   *regexp.Regexp
		glob *regexp.Regexp
	}
	var active []compiled
	for _, r := range rules {
		if !r.Enabled {
			continue
		}
		re, err := regexp.Compile(r.Pattern)
		if err != nil {
			continue
		}
		glob, err := globRegexp(r.Glob)
		if err != nil {
			continue
		}
		active = append(active, compiled{r, re, glob})
	}
	hits := []ruleHit{}
	if len(active) == 0 {
		return hits, nil
	}
	for _, item := range q.Items {
		if item.CurrentHash == "<deleted>" || strings.HasPrefix(item.Path, ".px1/") {
			continue
		}
		var added map[int]string
		for _, c := range active {
			if c.glob != nil && !c.glob.MatchString(item.Path) && !c.glob.MatchString(path.Base(item.Path)) {
				continue
			}
			if added == nil {
				added = m.addedLinesFor(item)
			}
			perKey := map[string]int{}
			lines := make([]int, 0, len(added))
			for n := range added {
				lines = append(lines, n)
			}
			sort.Ints(lines)
			for _, n := range lines {
				text := added[n]
				if !c.re.MatchString(text) {
					continue
				}
				key := ruleHitKey(c.rule.ID, item.Path, text)
				if dismissed[key] || perKey[key] >= ruleMaxHitsPerKey {
					continue
				}
				perKey[key]++
				hits = append(hits, ruleHit{Key: key, RuleID: c.rule.ID, Message: c.rule.Message, Source: c.rule.Source, Origin: c.rule.Origin, Path: item.Path, Line: n, Text: clip(text, 300)})
				if len(hits) >= ruleMaxHits {
					return hits, nil
				}
			}
		}
	}
	return hits, nil
}

func (m *reviewManager) addedLinesFor(item reviewItem) map[int]string {
	if item.BaselineHash != "<deleted>" {
		diff, err := m.BaselineDiff(item.Path)
		if err != nil {
			return map[int]string{}
		}
		return addedLines(diff)
	}
	b, err := os.ReadFile(filepath.Join(m.root, filepath.FromSlash(item.Path)))
	out := map[int]string{}
	if err != nil {
		return out
	}
	for i, line := range strings.Split(strings.ReplaceAll(string(b), "\r\n", "\n"), "\n") {
		out[i+1] = line
	}
	return out
}

func rulePrompt(comment, code, rel string) string {
	var b strings.Builder
	b.WriteString("A reviewer left this comment on code in a repository. Turn it into a reusable lint rule that would flag the same problem in future changes. Do not edit any files.\n\n")
	fmt.Fprintf(&b, "File: %s\n\nCode:\n```\n%s\n```\n\nComment: %s\n\n", rel, code, comment)
	b.WriteString("Reply with only a JSON object, no prose:\n")
	b.WriteString(`{"pattern":"<Go RE2 regular expression matching a single offending line>","glob":"<file glob such as *.ts or src/**/*.go, empty for all files>","message":"<the rule, imperative, under 15 words>"}`)
	b.WriteString("\n\nThe pattern must be specific enough not to match correct code.")
	return b.String()
}

func parseRuleOutput(out string) (ruleInput, error) {
	start := strings.Index(out, "{")
	for start != -1 {
		end := strings.LastIndex(out, "}")
		for end > start {
			var in ruleInput
			if json.Unmarshal([]byte(out[start:end+1]), &in) == nil && in.Pattern != "" {
				return validateRule(in)
			}
			end = strings.LastIndex(out[:end], "}")
		}
		next := strings.Index(out[start+1:], "{")
		if next == -1 {
			break
		}
		start += next + 1
	}
	return ruleInput{}, errors.New("harness did not return a rule")
}

func (s *Server) handleRules(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		rules, err := s.rules.All()
		msg := ""
		if err != nil {
			msg = err.Error()
		}
		writeJSON(w, map[string]any{"rules": rules, "teamFile": ruleTeamFile, "error": msg})
	case http.MethodPost:
		if !localPost(w, r) {
			return
		}
		var req ruleInput
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			fail(w, http.StatusBadRequest, "invalid rule")
			return
		}
		rule, err := s.rules.Add(req)
		if err != nil {
			fail(w, http.StatusBadRequest, err.Error())
			return
		}
		writeJSON(w, map[string]any{"rule": rule})
	default:
		fail(w, http.StatusMethodNotAllowed, "GET or POST only")
	}
}

func (s *Server) handleRuleUpdate(w http.ResponseWriter, r *http.Request) {
	if !localPost(w, r) {
		return
	}
	var req struct {
		ID      string `json:"id"`
		Pattern string `json:"pattern"`
		Glob    string `json:"glob"`
		Message string `json:"message"`
		Enabled *bool  `json:"enabled"`
		Delete  bool   `json:"delete"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		fail(w, http.StatusBadRequest, "invalid rule update")
		return
	}
	rule, err := s.rules.Update(req.ID, ruleInput{Pattern: req.Pattern, Glob: req.Glob, Message: req.Message}, req.Enabled, req.Delete)
	if err != nil {
		fail(w, http.StatusConflict, err.Error())
		return
	}
	writeJSON(w, map[string]any{"rule": rule})
}

func (s *Server) handleRuleSuggest(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodGet {
		id, _ := strconv.ParseInt(r.URL.Query().Get("job"), 10, 64)
		s.ruleMu.Lock()
		res, ok := s.ruleSuggest[id]
		s.ruleMu.Unlock()
		if !ok {
			fail(w, http.StatusNotFound, "no suggestion for that job")
			return
		}
		writeJSON(w, res)
		return
	}
	if !localPost(w, r) || !s.agentOrFail(w) {
		return
	}
	var req struct {
		Path      string `json:"path"`
		LineStart int    `json:"lineStart"`
		LineEnd   int    `json:"lineEnd"`
		Comment   string `json:"comment"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || strings.TrimSpace(req.Comment) == "" {
		fail(w, http.StatusBadRequest, "a comment is required")
		return
	}
	abs, rel, ok := s.resolvePath(req.Path)
	if !ok {
		fail(w, http.StatusBadRequest, "bad path")
		return
	}
	code, err := readLineRange(abs, req.LineStart, req.LineEnd)
	if err != nil {
		fail(w, http.StatusBadRequest, err.Error())
		return
	}
	ready := make(chan int64, 1)
	job, err := s.agent.RunPrompt("rule", rulePrompt(clip(req.Comment, 2000), code, rel), func(stdout string, runErr error) {
		jobID := <-ready
		res := map[string]any{"done": true}
		if runErr != nil {
			res["error"] = runErr.Error()
		} else if in, err := parseRuleOutput(stdout); err != nil {
			res["error"] = err.Error()
		} else {
			res["pattern"], res["glob"], res["message"] = in.Pattern, in.Glob, in.Message
		}
		s.ruleMu.Lock()
		s.ruleSuggest[jobID] = res
		s.ruleMu.Unlock()
	})
	if err != nil {
		code := http.StatusBadRequest
		if errors.Is(err, errAgentBusy) || errors.Is(err, errAgentNone) {
			code = http.StatusConflict
		}
		fail(w, code, err.Error())
		return
	}
	s.ruleMu.Lock()
	s.ruleSuggest[job.ID] = map[string]any{"done": false}
	s.ruleMu.Unlock()
	ready <- job.ID
	writeJSON(w, job)
}

func (s *Server) handleReviewRuleHits(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		fail(w, http.StatusMethodNotAllowed, "GET only")
		return
	}
	rules, rulesErr := s.rules.All()
	hits, err := s.review.RuleHits(rules)
	if err != nil {
		fail(w, http.StatusConflict, err.Error())
		return
	}
	s.rules.noteHits(hits)
	msg := ""
	if rulesErr != nil {
		msg = rulesErr.Error()
	}
	writeJSON(w, map[string]any{"hits": hits, "error": msg})
}

func (s *Server) handleReviewRuleHitDismiss(w http.ResponseWriter, r *http.Request) {
	if !localPost(w, r) {
		return
	}
	var req struct {
		Key string `json:"key"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Key == "" {
		fail(w, http.StatusBadRequest, "invalid rule hit")
		return
	}
	if err := s.review.DismissRuleHit(req.Key); err != nil {
		fail(w, http.StatusConflict, err.Error())
		return
	}
	writeJSON(w, map[string]any{"dismissed": req.Key})
}

func (s *Server) handleReviewRuleHitsSend(w http.ResponseWriter, r *http.Request) {
	if !localPost(w, r) || !s.agentOrFail(w) {
		return
	}
	rules, _ := s.rules.All()
	hits, err := s.review.RuleHits(rules)
	if err != nil {
		fail(w, http.StatusConflict, err.Error())
		return
	}
	existing, err := s.review.Comments()
	if err != nil {
		fail(w, http.StatusConflict, err.Error())
		return
	}
	have := map[string]bool{}
	for _, c := range existing {
		if c.Status == "open" && !c.Stale {
			have[c.Path+":"+strconv.Itoa(c.LineStart)+":"+c.Text] = true
		}
	}
	for _, h := range hits {
		text := "Rule: " + h.Message
		if have[h.Path+":"+strconv.Itoa(h.Line)+":"+text] {
			continue
		}
		if _, err := s.review.AddComment(h.Path, h.Line, h.Line, text); err != nil {
			fail(w, http.StatusConflict, err.Error())
			return
		}
	}
	s.handleReviewCommentsAgent(w, r)
}
