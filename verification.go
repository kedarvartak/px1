package main

import (
	"context"
	"errors"
	"net/http"
	"os/exec"
	"sort"
	"sync"
	"time"
)

type verificationResult struct {
	ID         string     `json:"id"`
	Name       string     `json:"name"`
	Running    bool       `json:"running"`
	ExitCode   int        `json:"exitCode,omitempty"`
	Output     string     `json:"output,omitempty"`
	StartedAt  time.Time  `json:"startedAt"`
	FinishedAt *time.Time `json:"finishedAt,omitempty"`
	Stale      bool       `json:"stale,omitempty"`
	Revision   string     `json:"-"`
}

type verificationManager struct {
	root string
	mu   sync.Mutex
	jobs map[string]*verificationResult
}

// SetRoot points checks at another checkout and drops results from the old one.
func (m *verificationManager) SetRoot(root string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.root = root
	m.jobs = map[string]*verificationResult{}
}

func newVerificationManager(root string) *verificationManager {
	return &verificationManager{root: root, jobs: map[string]*verificationResult{}}
}
func (m *verificationManager) Commands() map[string]string {
	return readSettings().VerificationCommands
}
func (m *verificationManager) Start(name, revision string) (*verificationResult, error) {
	cmdline := m.Commands()[name]
	if cmdline == "" {
		return nil, errors.New("unknown verification command")
	}
	id := time.Now().UTC().Format("20060102T150405.000000000")
	j := &verificationResult{ID: id, Name: name, Running: true, StartedAt: time.Now().UTC(), Revision: revision}
	m.mu.Lock()
	m.jobs[id] = j
	m.mu.Unlock()
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
		defer cancel()
		cmd := exec.CommandContext(ctx, "sh", "-lc", cmdline)
		cmd.Dir = m.root
		out, err := cmd.CombinedOutput()
		now := time.Now().UTC()
		m.mu.Lock()
		defer m.mu.Unlock()
		j.Running = false
		j.Output = string(out)
		j.FinishedAt = &now
		if err != nil {
			if exit, ok := err.(*exec.ExitError); ok {
				j.ExitCode = exit.ExitCode()
			} else {
				j.ExitCode = -1
			}
		}
	}()
	return copyVerification(j), nil
}
func (m *verificationManager) List() []*verificationResult {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]*verificationResult, 0, len(m.jobs))
	for _, j := range m.jobs {
		out = append(out, copyVerification(j))
	}
	sort.Slice(out, func(i, j int) bool { return out[i].StartedAt.After(out[j].StartedAt) })
	return out
}
func copyVerification(j *verificationResult) *verificationResult { c := *j; return &c }

func (s *Server) handleReviewChecks(w http.ResponseWriter, r *http.Request) {
	if r.Method != "GET" {
		fail(w, 405, "GET only")
		return
	}
	rev, _ := s.review.Revision()
	jobs := s.verify.List()
	for _, j := range jobs {
		j.Stale = !j.Running && rev != "" && j.Revision != rev
		j.Revision = ""
	}
	writeJSON(w, map[string]any{"commands": s.verify.Commands(), "jobs": jobs})
}
func (s *Server) handleReviewCheckRun(w http.ResponseWriter, r *http.Request) {
	if !localPost(w, r) {
		return
	}
	rev, err := s.review.Revision()
	if err != nil {
		fail(w, 409, err.Error())
		return
	}
	job, err := s.verify.Start(r.URL.Query().Get("name"), rev)
	if err != nil {
		fail(w, 400, err.Error())
		return
	}
	writeJSON(w, map[string]any{"job": job})
}
