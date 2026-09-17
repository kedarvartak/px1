package main

// Review sessions are deliberately kept outside a workspace.  They capture the
// state a human started reviewing, so later agent changes can be compared with
// (and restored to) that state without treating HEAD as the only baseline.

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

const reviewManifestVersion = 1

type reviewFile struct {
	Path string      `json:"path"`
	Mode fs.FileMode `json:"mode"`
	Size int64       `json:"size"`
	Hash string      `json:"hash"`
}

type reviewSession struct {
	Version   int                       `json:"version"`
	ID        string                    `json:"id"`
	Root      string                    `json:"root"`
	StartedAt time.Time                 `json:"startedAt"`
	Head      string                    `json:"head,omitempty"`
	Files     []reviewFile              `json:"files"`
	Dirs      []string                  `json:"dirs"`
	Reviews   map[string]reviewDecision `json:"reviews,omitempty"`
	ClosedAt  *time.Time                `json:"closedAt,omitempty"`
}

// reviewDecision is tied to the exact content a human saw. A later write to
// that path never inherits approval: it becomes stale and returns to the queue.
type reviewDecision struct {
	State      string    `json:"state"` // reviewed or blocked
	Hash       string    `json:"hash"`
	ReviewedAt time.Time `json:"reviewedAt"`
}

type reviewItem struct {
	Path         string `json:"path"`
	State        string `json:"state"` // unreviewed, reviewed, stale, blocked
	BaselineHash string `json:"baselineHash,omitempty"`
	CurrentHash  string `json:"currentHash,omitempty"`
}

type reviewQueue struct {
	Items     []reviewItem `json:"items"`
	Reviewed  int          `json:"reviewed"`
	Stale     int          `json:"stale"`
	Remaining int          `json:"remaining"`
	Total     int          `json:"total"`
}

type reviewManager struct {
	root, state string
	mu          sync.Mutex
	active      *reviewSession
}

func reviewStateRoot() string {
	if xdg := os.Getenv("XDG_CONFIG_HOME"); xdg != "" {
		return filepath.Join(xdg, "px1", "review-sessions")
	}
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return ""
	}
	return filepath.Join(home, ".px1", "review-sessions")
}

func newReviewManager(root string) *reviewManager {
	m := &reviewManager{root: root, state: reviewStateRoot()}
	if m.state == "" {
		return m
	}
	if b, err := os.ReadFile(m.activePath()); err == nil {
		var pointer struct {
			ID string `json:"id"`
		}
		if json.Unmarshal(b, &pointer) == nil && pointer.ID != "" {
			if s, err := m.load(pointer.ID); err == nil && s.Root == root && s.ClosedAt == nil {
				m.active = s
			}
		}
	}
	return m
}

func (m *reviewManager) workspaceKey() string {
	s := sha256.Sum256([]byte(m.root))
	return hex.EncodeToString(s[:])
}
func (m *reviewManager) activePath() string {
	return filepath.Join(m.state, "active", m.workspaceKey()+".json")
}
func (m *reviewManager) sessionDir(id string) string { return filepath.Join(m.state, "sessions", id) }
func (m *reviewManager) manifestPath(id string) string {
	return filepath.Join(m.sessionDir(id), "session.json")
}

func (m *reviewManager) load(id string) (*reviewSession, error) {
	b, err := os.ReadFile(m.manifestPath(id))
	if err != nil {
		return nil, err
	}
	var s reviewSession
	if err := json.Unmarshal(b, &s); err != nil {
		return nil, err
	}
	if s.Version != reviewManifestVersion || s.ID == "" {
		return nil, errors.New("unsupported review session")
	}
	return &s, nil
}

func writeAtomic(path string, b []byte, mode fs.FileMode) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".px1-")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)
	if _, err = tmp.Write(b); err == nil {
		err = tmp.Chmod(mode)
	}
	if closeErr := tmp.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		return err
	}
	return os.Rename(tmpName, path)
}

func reviewHead(root string) string {
	if !gitAvailable(root) {
		return ""
	}
	b, err := execOutput("git", "-C", root, "rev-parse", "HEAD")
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(b))
}

// execOutput exists so review code has one small, testable seam around Git.
var execOutput = func(name string, args ...string) ([]byte, error) { return exec.Command(name, args...).Output() }

func snapshotTree(root, copyTo string) ([]reviewFile, []string, error) {
	var files []reviewFile
	var dirs []string
	err := filepath.WalkDir(root, func(abs string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(root, abs)
		if err != nil {
			return err
		}
		if rel == "." {
			return nil
		}
		if d.IsDir() && d.Name() == ".git" {
			return filepath.SkipDir
		}
		if d.Type()&fs.ModeSymlink != 0 {
			return errors.New("review sessions do not support symbolic links: " + filepath.ToSlash(rel))
		}
		if d.IsDir() {
			dirs = append(dirs, filepath.ToSlash(rel))
			return nil
		}
		if !d.Type().IsRegular() {
			return errors.New("review sessions do not support special files: " + filepath.ToSlash(rel))
		}
		b, err := os.ReadFile(abs)
		if err != nil {
			return err
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		h := sha256.Sum256(b)
		p := filepath.ToSlash(rel)
		files = append(files, reviewFile{Path: p, Mode: info.Mode().Perm(), Size: int64(len(b)), Hash: hex.EncodeToString(h[:])})
		if copyTo != "" {
			if err := writeAtomic(filepath.Join(copyTo, filepath.FromSlash(p)), b, info.Mode().Perm()); err != nil {
				return err
			}
		}
		return nil
	})
	return files, dirs, err
}

func (m *reviewManager) Start() (*reviewSession, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.active != nil {
		cp := *m.active
		return &cp, nil
	}
	if m.state == "" {
		return nil, errors.New("cannot determine px1 state directory")
	}
	id := time.Now().UTC().Format("20060102T150405.000000000")
	dir := m.sessionDir(id)
	staging := dir + ".creating"
	if err := os.RemoveAll(staging); err != nil {
		return nil, err
	}
	files, dirs, err := snapshotTree(m.root, filepath.Join(staging, "files"))
	if err != nil {
		os.RemoveAll(staging)
		return nil, err
	}
	s := &reviewSession{Version: reviewManifestVersion, ID: id, Root: m.root, StartedAt: time.Now().UTC(), Head: reviewHead(m.root), Files: files, Dirs: dirs, Reviews: map[string]reviewDecision{}}
	b, err := json.Marshal(s)
	if err != nil {
		os.RemoveAll(staging)
		return nil, err
	}
	if err = writeAtomic(filepath.Join(staging, "session.json"), b, 0o600); err != nil {
		os.RemoveAll(staging)
		return nil, err
	}
	if err = os.MkdirAll(filepath.Dir(dir), 0o700); err == nil {
		err = os.Rename(staging, dir)
	}
	if err != nil {
		os.RemoveAll(staging)
		return nil, err
	}
	pointer, _ := json.Marshal(map[string]string{"id": id})
	if err = writeAtomic(m.activePath(), pointer, 0o600); err != nil {
		return nil, err
	}
	m.active = s
	return s, nil
}

func (m *reviewManager) Active() (*reviewSession, []string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.active == nil {
		return nil, nil
	}
	cp := *m.active
	return &cp, m.changedLocked(m.active)
}

func (m *reviewManager) changedLocked(s *reviewSession) []string {
	files, _, err := snapshotTree(m.root, "")
	if err != nil {
		return []string{"<workspace scan failed: " + err.Error() + ">"}
	}
	before, after := map[string]string{}, map[string]string{}
	for _, f := range s.Files {
		before[f.Path] = f.Hash
	}
	for _, f := range files {
		after[f.Path] = f.Hash
	}
	changed := []string{}
	for p, h := range before {
		if after[p] != h {
			changed = append(changed, p)
		}
	}
	for p := range after {
		if _, ok := before[p]; !ok {
			changed = append(changed, p)
		}
	}
	sort.Strings(changed)
	return changed
}

func (m *reviewManager) saveLocked() error {
	b, err := json.Marshal(m.active)
	if err != nil {
		return err
	}
	return writeAtomic(m.manifestPath(m.active.ID), b, 0o600)
}

func reviewHash(f reviewFile, present bool) string {
	if !present {
		return "<deleted>"
	}
	return f.Hash
}

func (m *reviewManager) queueLocked() (reviewQueue, error) {
	if m.active == nil {
		return reviewQueue{}, errors.New("no active review session")
	}
	current, _, err := snapshotTree(m.root, "")
	if err != nil {
		return reviewQueue{}, err
	}
	baseline := map[string]reviewFile{}
	now := map[string]reviewFile{}
	for _, f := range m.active.Files {
		baseline[f.Path] = f
	}
	for _, f := range current {
		now[f.Path] = f
	}
	paths := map[string]bool{}
	for path, old := range baseline {
		new, exists := now[path]
		if !exists || old.Hash != new.Hash {
			paths[path] = true
		}
	}
	for path := range now {
		if _, existed := baseline[path]; !existed {
			paths[path] = true
		}
	}
	q := reviewQueue{}
	for path := range paths {
		old, hadOld := baseline[path]
		new, hasNew := now[path]
		curHash := reviewHash(new, hasNew)
		item := reviewItem{Path: path, BaselineHash: reviewHash(old, hadOld), CurrentHash: curHash, State: "unreviewed"}
		if d, ok := m.active.Reviews[path]; ok {
			switch {
			case d.Hash != curHash:
				item.State = "stale"
			case d.State == "blocked":
				item.State = "blocked"
			default:
				item.State = "reviewed"
			}
		}
		switch item.State {
		case "reviewed":
			q.Reviewed++
		case "stale":
			q.Stale++
			q.Remaining++
		default:
			q.Remaining++
		}
		q.Items = append(q.Items, item)
	}
	sort.Slice(q.Items, func(i, j int) bool { return q.Items[i].Path < q.Items[j].Path })
	q.Total = len(q.Items)
	return q, nil
}

func (m *reviewManager) Queue() (reviewQueue, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.queueLocked()
}

func (m *reviewManager) Mark(path, state string) (reviewQueue, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.active == nil {
		return reviewQueue{}, errors.New("no active review session")
	}
	if state == "" {
		state = "reviewed"
	}
	if state != "reviewed" && state != "blocked" {
		return reviewQueue{}, errors.New("review state must be reviewed or blocked")
	}
	q, err := m.queueLocked()
	if err != nil {
		return reviewQueue{}, err
	}
	var found *reviewItem
	for i := range q.Items {
		if q.Items[i].Path == path {
			found = &q.Items[i]
			break
		}
	}
	if found == nil {
		return reviewQueue{}, errors.New("path has no changes in this review session")
	}
	if m.active.Reviews == nil {
		m.active.Reviews = map[string]reviewDecision{}
	}
	m.active.Reviews[path] = reviewDecision{State: state, Hash: found.CurrentHash, ReviewedAt: time.Now().UTC()}
	if err := m.saveLocked(); err != nil {
		return reviewQueue{}, err
	}
	return m.queueLocked()
}

func (m *reviewManager) Restore() (*reviewSession, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.active == nil {
		return nil, errors.New("no active review session")
	}
	s := m.active
	files := map[string]reviewFile{}
	for _, f := range s.Files {
		files[f.Path] = f
	}
	current, dirs, err := snapshotTree(m.root, "")
	if err != nil {
		return nil, err
	}
	for _, f := range current {
		if _, ok := files[f.Path]; !ok {
			if err := os.Remove(filepath.Join(m.root, filepath.FromSlash(f.Path))); err != nil {
				return nil, err
			}
		}
	}
	baselineDirs := map[string]bool{}
	for _, d := range s.Dirs {
		baselineDirs[d] = true
	}
	sort.Slice(dirs, func(i, j int) bool { return len(dirs[i]) > len(dirs[j]) })
	for _, d := range dirs {
		if !baselineDirs[d] {
			_ = os.Remove(filepath.Join(m.root, filepath.FromSlash(d)))
		}
	}
	for _, d := range s.Dirs {
		if err := os.MkdirAll(filepath.Join(m.root, filepath.FromSlash(d)), 0o755); err != nil {
			return nil, err
		}
	}
	for _, f := range s.Files {
		b, err := os.ReadFile(filepath.Join(m.sessionDir(s.ID), "files", filepath.FromSlash(f.Path)))
		if err != nil {
			return nil, err
		}
		if err := writeAtomic(filepath.Join(m.root, filepath.FromSlash(f.Path)), b, f.Mode); err != nil {
			return nil, err
		}
	}
	cp := *s
	return &cp, nil
}

func (m *reviewManager) Close() (*reviewSession, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.active == nil {
		return nil, errors.New("no active review session")
	}
	now := time.Now().UTC()
	m.active.ClosedAt = &now
	if err := m.saveLocked(); err != nil {
		return nil, err
	}
	_ = os.Remove(m.activePath())
	cp := *m.active
	m.active = nil
	return &cp, nil
}

// handleReviewSession returns the active session and the precise paths whose
// bytes differ from its task-start baseline. It is intentionally read-only so
// remote inspection remains safe.
func (s *Server) handleReviewSession(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		fail(w, 405, "GET only")
		return
	}
	active, changed := s.review.Active()
	if active == nil {
		writeJSON(w, map[string]any{"active": nil, "changed": []string{}, "queue": reviewQueue{}})
		return
	}
	queue, err := s.review.Queue()
	if err != nil {
		fail(w, 500, err.Error())
		return
	}
	writeJSON(w, map[string]any{"active": active, "changed": changed, "queue": queue})
}

func (s *Server) handleReviewMark(w http.ResponseWriter, r *http.Request) {
	if !localPost(w, r) {
		return
	}
	path := r.URL.Query().Get("path")
	_, clean, ok := s.safePath(path)
	if !ok || clean == "" {
		fail(w, 400, "bad path")
		return
	}
	queue, err := s.review.Mark(clean, r.URL.Query().Get("state"))
	if err != nil {
		fail(w, 409, err.Error())
		return
	}
	writeJSON(w, map[string]any{"queue": queue})
}

func (s *Server) handleReviewStart(w http.ResponseWriter, r *http.Request) {
	if !localPost(w, r) {
		return
	}
	active, err := s.review.Start()
	if err != nil {
		fail(w, 500, err.Error())
		return
	}
	writeJSON(w, map[string]any{"active": active, "changed": []string{}})
}

func (s *Server) handleReviewRestore(w http.ResponseWriter, r *http.Request) {
	if !localPost(w, r) {
		return
	}
	active, err := s.review.Restore()
	if err != nil {
		fail(w, 409, err.Error())
		return
	}
	s.ix.Build()
	writeJSON(w, map[string]any{"active": active, "changed": []string{}})
}

func (s *Server) handleReviewClose(w http.ResponseWriter, r *http.Request) {
	if !localPost(w, r) {
		return
	}
	active, err := s.review.Close()
	if err != nil {
		fail(w, 409, err.Error())
		return
	}
	writeJSON(w, map[string]any{"closed": active})
}
