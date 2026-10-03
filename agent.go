package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Review-provider actions run a configured local command. px1 composes a
// review-specific prompt, records the bounded job output, and reloads whatever
// moved once the provider exits. Normal harness work remains outside px1.

const (
	agentTimeout  = 10 * time.Minute
	agentLogBytes = 32 << 10
)

// agentPreset is a provider command px1 knows and the argv that runs it headless. Each
// of these starts an interactive session by default and would sit forever
// waiting for approval, so every preset carries the flag that turns that off
// and the one that lets it apply edits without asking.
type agentPreset struct {
	Name         string
	Args         []string
	ModelFlag    string
	DefaultModel string
}

var agentPresets = []agentPreset{
	{
		Name:         "claude",
		Args:         []string{"claude", "--permission-mode", "acceptEdits", "-p", "{prompt}"},
		ModelFlag:    "--model",
		DefaultModel: "haiku",
	},
	{
		Name:         "gemini",
		Args:         []string{"gemini", "--approval-mode", "auto_edit", "-p", "{prompt}"},
		ModelFlag:    "-m",
		DefaultModel: "gemini-2.5-flash-lite",
	},
	{
		Name:         "cursor-agent",
		Args:         []string{"cursor-agent", "--force", "-p", "{prompt}"},
		ModelFlag:    "--model",
		DefaultModel: "gemini-3.6-flash-minimal",
	},
	{
		Name:         "agy",
		Args:         []string{"agy", "--dangerously-skip-permissions", "--mode", "accept-edits", "-p", "{prompt}"},
		ModelFlag:    "--model",
		DefaultModel: "gemini-3.6-flash-low",
	},
	{
		Name:         "opencode",
		Args:         []string{"opencode", "run", "{prompt}"},
		ModelFlag:    "-m",
		DefaultModel: "opencode/big-pickle",
	},
	{
		Name:         "codex",
		Args:         []string{"codex", "exec", "--approve-for-me", "{prompt}"},
		ModelFlag:    "-m",
		DefaultModel: "gpt-5.6-sol",
	},
	{
		Name:         "aider",
		Args:         []string{"aider", "--yes-always", "--no-auto-commits", "--message", "{prompt}"},
		ModelFlag:    "--model",
		DefaultModel: "claude-3-7-sonnet",
	},
	{
		Name:         "goose",
		Args:         []string{"goose", "run", "--no-session", "-t", "{prompt}"},
		ModelFlag:    "--model",
		DefaultModel: "gpt-4o",
	},
}

// agentJob is one dispatch, snapshot-able while it runs.
type agentJob struct {
	ID      int64    `json:"id"`
	Harness string   `json:"harness"`
	Path    string   `json:"path"`
	Lines   string   `json:"lines"`
	Running bool     `json:"running"`
	Error   string   `json:"error,omitempty"`
	Log     string   `json:"log"`
	Stdout  string   `json:"stdout,omitempty"`
	Stderr  string   `json:"stderr,omitempty"`
	Changed []string `json:"changed"`
	Ms      int64    `json:"ms"`
	// Tracked is false outside a git repository, where px1 cannot tell which
	// files a provider touched. An empty Changed then means "unknown", not
	// "nothing", and the client reloads regardless.
	Tracked bool `json:"tracked"`

	// l1/l2 anchor this job for the overlap check in Start; unexported since
	// Lines already carries the display form.
	l1, l2 int
	cancel context.CancelFunc
	onDone func(stdout string, err error)
	kind   string
	out    *tailBuffer
	stderr *tailBuffer
	start  time.Time
}

var (
	// The refusals the review UI reacts to rather than merely reporting.
	errAgentBusy = errors.New("a provider job is already running")
	errAgentNone = errors.New("no coding harness is selected")
)

// lineStreamer forwards complete lines to w with a prefix in real time,
// while also storing the raw bytes into a tailBuffer.
type lineStreamer struct {
	mu     sync.Mutex
	buf    *tailBuffer
	prefix string
	line   []byte
	w      io.Writer
}

func newLineStreamer(buf *tailBuffer, prefix string, w io.Writer) *lineStreamer {
	return &lineStreamer{buf: buf, prefix: prefix, w: w}
}

func (s *lineStreamer) Write(p []byte) (int, error) {
	if s.buf != nil {
		s.buf.Write(p)
	}
	if s.w == nil || uiQuiet {
		return len(p), nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, b := range p {
		if b == '\n' {
			if len(s.line) > 0 {
				fmt.Fprintf(s.w, "  %s %s\n", s.prefix, string(s.line))
				s.line = s.line[:0]
			}
		} else if b != '\r' {
			s.line = append(s.line, b)
		}
	}
	return len(p), nil
}

func (s *lineStreamer) Flush() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.line) > 0 && s.w != nil && !uiQuiet {
		fmt.Fprintf(s.w, "  %s %s\n", s.prefix, string(s.line))
		s.line = s.line[:0]
	}
}

// agentManager owns the configured provider and every review job in flight.
// Several jobs can run at once as long as they touch disjoint line ranges:
// overlapping ranges are refused rather than queued.
type agentManager struct {
	root string
	lsp  *lspManager

	mu       sync.Mutex
	selected string            // preset name, or the template itself when pinned
	args     []string          // resolved argv, nil when nothing is selected
	pinned   bool              // -agent was given, so the UI cannot change it
	models   map[string]string // provider name -> selected model
	jobs     map[int64]*agentJob
	seq      int64
}

// newAgentManager wires provider configuration and restores the remembered choice. A flag
// value pins a provider (or an arbitrary command template) for this run and is
// the only case that can fail: a bad -agent should stop startup, whereas a
// stale settings file should just leave nothing selected.
func newAgentManager(root, flagSpec string, lsp *lspManager) (*agentManager, error) {
	m := &agentManager{
		root:   root,
		lsp:    lsp,
		models: map[string]string{},
	}
	s := readSettings()
	if s.Models != nil {
		for k, v := range s.Models {
			m.models[k] = v
		}
	}

	if spec := strings.TrimSpace(flagSpec); spec != "" {
		name, args, chosenModel, err := resolveAgentSpec(spec, m.models[spec])
		if err != nil {
			return nil, err
		}
		m.selected, m.args, m.pinned = name, args, true
		if chosenModel != "" {
			m.models[name] = chosenModel
		}
		return m, nil
	}

	if s.Agent != "" {
		if name, args, chosenModel, err := resolveAgentSpec(s.Agent, m.models[s.Agent]); err == nil {
			m.selected, m.args = name, args
			if chosenModel != "" {
				m.models[name] = chosenModel
			}
		}
	}
	return m, nil
}

// resolveAgentSpec turns a preset name or a command template into argv, and
// verifies the binary exists now rather than at first use.
func resolveAgentSpec(spec, model string) (string, []string, string, error) {
	spec = strings.TrimSpace(spec)
	if spec == "" {
		return "", nil, "", errors.New("empty provider")
	}

	var name string
	var args []string
	var chosenModel string
	for _, p := range agentPresets {
		if strings.EqualFold(spec, p.Name) {
			name = p.Name
			chosenModel = model
			if chosenModel == "" {
				chosenModel = p.DefaultModel
			}
			promptIdx := -1
			for i, arg := range p.Args {
				if arg == "{prompt}" {
					promptIdx = i
					break
				}
			}
			args = make([]string, 0, len(p.Args)+2)
			insertIdx := promptIdx
			if promptIdx > 0 && strings.HasPrefix(p.Args[promptIdx-1], "-") {
				insertIdx = promptIdx - 1
			}
			for i, arg := range p.Args {
				if i == insertIdx && p.ModelFlag != "" && chosenModel != "" {
					args = append(args, p.ModelFlag, chosenModel)
				}
				args = append(args, arg)
			}
			break
		}
	}
	if args == nil {
		args = strings.Fields(spec)
		if len(args) == 0 {
			return "", nil, "", errors.New("empty provider command")
		}
		if !strings.Contains(spec, "{prompt}") {
			return "", nil, "", fmt.Errorf("a command template must contain {prompt} (known providers: %s)",
				strings.Join(agentPresetNames(), ", "))
		}
		name = filepath.Base(args[0])
		chosenModel = model
		if chosenModel != "" {
			for i, arg := range args {
				args[i] = strings.ReplaceAll(arg, "{model}", chosenModel)
			}
		}
	}

	bin, ok := lookPathIn(args[0], lspBinDirs())
	if !ok {
		return "", nil, "", fmt.Errorf("%s is not installed", args[0])
	}
	resolved := append([]string(nil), args...)
	resolved[0] = bin
	return name, resolved, chosenModel, nil
}

func agentPresetNames() []string {
	names := make([]string, len(agentPresets))
	for i, p := range agentPresets {
		names[i] = p.Name
	}
	return names
}

// SetRoot points provider dispatch at another checkout. It refuses while a job
// is in flight because the provider may still be writing into the old one.
func (m *agentManager) SetRoot(root string) error {
	if m == nil {
		return nil
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.anyRunningLocked() {
		return errAgentBusy
	}
	m.root = root
	return nil
}

func (m *agentManager) Name() string {
	if m == nil {
		return ""
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.selected
}

func (m *agentManager) Model() string {
	if m == nil {
		return ""
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.selected == "" {
		return ""
	}
	return m.models[m.selected]
}

// Select remembers a provider for this workspace and every later run. Passing
// an empty name turns provider actions off.
func (m *agentManager) Select(name string, modelOpt ...string) error {
	m.mu.Lock()
	if m.pinned {
		m.mu.Unlock()
		return errors.New("px1 was started with -agent, so the provider is fixed for this run")
	}
	if m.anyRunningLocked() {
		m.mu.Unlock()
		return errAgentBusy
	}
	m.mu.Unlock()

	name = strings.TrimSpace(name)
	if name == "" {
		m.mu.Lock()
		m.selected, m.args = "", nil
		m.mu.Unlock()
		return writeSettings(settings{})
	}

	reqModel := ""
	if len(modelOpt) > 0 {
		reqModel = strings.TrimSpace(modelOpt[0])
	}
	m.mu.Lock()
	if reqModel == "" {
		reqModel = m.models[name]
	}
	m.mu.Unlock()

	display, args, chosenModel, err := resolveAgentSpec(name, reqModel)
	if err != nil {
		uiStatus("err", fmt.Sprintf("agent: failed to configure provider %q", name), err.Error(), 0, os.Stdout)
		return err
	}
	m.mu.Lock()
	m.selected, m.args = display, args
	if m.models == nil {
		m.models = map[string]string{}
	}
	if chosenModel != "" {
		m.models[display] = chosenModel
	}
	savedModels := make(map[string]string, len(m.models))
	for k, v := range m.models {
		savedModels[k] = v
	}
	m.mu.Unlock()

	modelNote := ""
	if chosenModel != "" {
		modelNote = fmt.Sprintf(" (%s)", chosenModel)
	}
	uiStatus("ok", "agent", fmt.Sprintf("%s%s", display, modelNote), 0, os.Stdout)
	// Persist the spec as given, not the display name: a command template
	// shortens to its binary for display and would not survive the round trip.
	return writeSettings(settings{Agent: name, Models: savedModels})
}

// Job returns a snapshot of job id, or of the most recently started job when
// id is 0, or nil when there isn't one.
func (m *agentManager) Job(id int64) *agentJob {
	if m == nil {
		return nil
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	j := m.jobs[id]
	if id == 0 {
		for _, cand := range m.jobs {
			if j == nil || cand.ID > j.ID {
				j = cand
			}
		}
	}
	if j == nil {
		return nil
	}
	cp := *j
	cp.Log = j.out.String()
	cp.Stdout = cp.Log
	if j.stderr != nil {
		cp.Stderr = j.stderr.String()
	}
	if cp.Running {
		cp.Ms = time.Since(j.start).Milliseconds()
	}
	return &cp
}

// anyRunningLocked reports whether any job is still in flight. Callers hold m.mu.
func (m *agentManager) anyRunningLocked() bool {
	for _, j := range m.jobs {
		if j.Running {
			return true
		}
	}
	return false
}

// overlapLocked reports whether a running job already touches rel within
// [l1,l2]. Different paths, or disjoint ranges on the same path, are free to
// run at the same time. Callers hold m.mu.
func (m *agentManager) overlapLocked(rel string, l1, l2 int) bool {
	for _, j := range m.jobs {
		if !j.Running || j.Path != rel {
			continue
		}
		if l1 <= j.l2 && j.l1 <= l2 {
			return true
		}
	}
	return false
}

// Start dispatches a review instruction anchored to abs:l1-l2. It returns as
// soon as the provider is running.
func (m *agentManager) Start(abs, rel string, l1, l2 int, instruction string) (*agentJob, error) {
	return m.StartWithDone(abs, rel, l1, l2, instruction, nil)
}

func (m *agentManager) StartWithDone(abs, rel string, l1, l2 int, instruction string, onDone func(stdout string, err error)) (*agentJob, error) {
	instruction = strings.TrimSpace(instruction)
	if instruction == "" {
		return nil, errors.New("instruction is empty")
	}

	m.mu.Lock()
	if m.args == nil {
		m.mu.Unlock()
		uiStatus("err", "agent", "provider dispatch refused: no review provider selected", 0, os.Stdout)
		return nil, errAgentNone
	}
	if m.overlapLocked(rel, l1, l2) {
		m.mu.Unlock()
		uiStatus("warn", "agent", "provider dispatch refused: overlapping review job already running", 0, os.Stdout)
		return nil, errAgentBusy
	}
	args := m.args
	name := m.selected
	m.mu.Unlock()

	snippet, err := readLineRange(abs, l1, l2)
	if err != nil {
		uiStatus("err", "agent", fmt.Sprintf("failed reading snippet for %s:%s: %s", rel, lineRef(l1, l2), err.Error()), 0, os.Stdout)
		return nil, err
	}

	m.mu.Lock()
	// Re-check under lock: another dispatch may have raced between the check
	// above and here, while this one was reading the file and git status.
	if m.overlapLocked(rel, l1, l2) {
		m.mu.Unlock()
		uiStatus("warn", "agent", "provider dispatch refused: overlapping review job already running", 0, os.Stdout)
		return nil, errAgentBusy
	}
	m.seq++
	ctx, cancel := context.WithTimeout(context.Background(), agentTimeout)
	job := &agentJob{
		ID:      m.seq,
		Harness: name,
		Path:    rel,
		Lines:   lineRef(l1, l2),
		Running: true,
		Changed: []string{},
		Tracked: gitAvailable(m.root),
		l1:      l1,
		l2:      l2,
		out:     &tailBuffer{max: agentLogBytes},
		stderr:  &tailBuffer{max: agentLogBytes},
		start:   time.Now(),
		cancel:  cancel,
		onDone:  onDone,
	}
	if m.jobs == nil {
		m.jobs = map[int64]*agentJob{}
	}
	m.jobs[job.ID] = job
	m.mu.Unlock()

	modelStr := ""
	if m.models != nil && m.models[name] != "" {
		modelStr = fmt.Sprintf(" (%s)", m.models[name])
	}
	uiStatus("step", "agent", fmt.Sprintf("#%d %s%s · %s:%s  %q", job.ID, name, modelStr, rel, lineRef(l1, l2), instruction), 0, os.Stdout)
	go m.run(ctx, cancel, job, args, agentPrompt(rel, l1, l2, snippet, instruction))
	return m.Job(job.ID), nil
}

func (m *agentManager) RunPrompt(kind, prompt string, onDone func(stdout string, err error)) (*agentJob, error) {
	if strings.TrimSpace(prompt) == "" {
		return nil, errors.New("nothing to explain")
	}
	m.mu.Lock()
	if m.args == nil {
		m.mu.Unlock()
		return nil, errAgentNone
	}
	for _, j := range m.jobs {
		if j.Running && j.kind == kind {
			m.mu.Unlock()
			return nil, errAgentBusy
		}
	}
	m.seq++
	ctx, cancel := context.WithTimeout(context.Background(), agentTimeout)
	job := &agentJob{
		ID:      m.seq,
		Harness: m.selected,
		Running: true,
		Changed: []string{},
		Tracked: gitAvailable(m.root),
		out:     &tailBuffer{max: agentLogBytes},
		stderr:  &tailBuffer{max: agentLogBytes},
		start:   time.Now(),
		cancel:  cancel,
		onDone:  onDone,
		kind:    kind,
	}
	if m.jobs == nil {
		m.jobs = map[int64]*agentJob{}
	}
	m.jobs[job.ID] = job
	args := m.args
	m.mu.Unlock()
	uiStatus("step", "agent", fmt.Sprintf("#%d %s · %s", job.ID, job.Harness, kind), 0, os.Stdout)
	go m.run(ctx, cancel, job, args, prompt)
	return m.Job(job.ID), nil
}

func (m *agentManager) run(ctx context.Context, cancel context.CancelFunc, job *agentJob, template []string, prompt string) {
	defer cancel()

	if uiVerbose {
		uiVerbosePrompt(job.ID, job.Harness, prompt, os.Stdout)
	}

	before := worktreeSnapshot(m.root)

	args := make([]string, len(template))
	for i, tok := range template {
		args[i] = strings.ReplaceAll(tok, "{prompt}", prompt)
	}

	stdoutStreamer := newLineStreamer(job.out, uiFaint("│", os.Stdout), os.Stdout)
	stderrStreamer := newLineStreamer(job.stderr, uiDim("│", os.Stdout), os.Stdout)

	cmd := exec.CommandContext(ctx, args[0], args[1:]...)
	cmd.Dir = m.root
	cmd.Stdout = stdoutStreamer
	cmd.Stderr = stderrStreamer
	// stdin stays empty: a harness that still wants to ask something fails
	// fast instead of hanging until the timeout with nothing on screen.

	err := cmd.Run()
	stdoutStreamer.Flush()
	stderrStreamer.Flush()
	if ctx.Err() != nil {
		err = fmt.Errorf("gave up after %s", agentTimeout)
	}

	changed := changedSince(m.root, before)
	m.settle(changed)
	if job.onDone != nil {
		job.onDone(job.out.String(), err)
	}

	m.mu.Lock()
	job.Running = false
	job.Changed = changed
	job.Ms = time.Since(job.start).Milliseconds()
	if err != nil {
		job.Error = err.Error()
	}
	stdoutOutput := job.out.String()
	stderrOutput := job.stderr.String()
	m.mu.Unlock()

	durStr := fmtDuration(time.Duration(job.Ms) * time.Millisecond)
	if err != nil {
		uiStatus("err", "agent", fmt.Sprintf("#%d %s failed in %s: %s", job.ID, job.Harness, durStr, err.Error()), 0, os.Stdout)
		if trimmedErr := strings.TrimSpace(stderrOutput); trimmedErr != "" {
			uiKV("harness stderr", trimmedErr, 0, os.Stdout)
		}
		if trimmedOut := strings.TrimSpace(stdoutOutput); trimmedOut != "" {
			uiKV("harness stdout", trimmedOut, 0, os.Stdout)
		}
	} else {
		var summary string
		switch len(changed) {
		case 0:
			summary = "no files changed"
		case 1:
			summary = fmt.Sprintf("1 file changed: %s", changed[0])
		default:
			summary = fmt.Sprintf("%d files changed: %s", len(changed), strings.Join(changed, ", "))
		}
		uiStatus("ok", "agent", fmt.Sprintf("#%d %s · %s  (%s)", job.ID, job.Harness, durStr, summary), 0, os.Stdout)
		if len(changed) == 0 && job.Tracked {
			if trimmedErr := strings.TrimSpace(stderrOutput); trimmedErr != "" {
				uiKV("harness stderr", trimmedErr, 0, os.Stdout)
			}
		}
	}
}

// settle drops every trace of the old bytes. Open memoises on path+mtime+size
// so a rewritten file already misses the cache, but a harness that truncates
// and writes in place can be read mid-write, and that torn copy would then sit
// under a key nothing invalidates. Evicting is cheaper than reasoning about it.
// Language servers hold their own copy of the file and never saw the write, so
// they are closed here too and reopen on the next request.
func (m *agentManager) settle(changed []string) {
	for _, rel := range changed {
		abs := filepath.Join(m.root, filepath.FromSlash(rel))
		Evict(abs)
		if m.lsp != nil {
			m.lsp.CloseDoc(abs, rel)
		}
	}
}

// Close stops provider jobs during process shutdown. Whatever each job has
// already written stays.
func (m *agentManager) Close() {
	if m == nil {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, j := range m.jobs {
		if !j.Running || j.cancel == nil {
			continue
		}
		uiStatus("warn", "agent", fmt.Sprintf("stopping provider job with %s (job %d)", j.Harness, j.ID), 0, os.Stdout)
		j.cancel()
	}
}

// changedSince reports the paths whose state differs from the snapshot taken
// before the run. Asking git is the only honest answer to "what did it touch":
// a harness routinely edits files nobody pointed it at.
func changedSince(root string, before map[string]string) []string {
	return changedSinceMaps(before, worktreeSnapshot(root))
}

// worktreeSnapshot is git status with each listed file's size and mtime folded
// into its entry. Status alone misses the common case of editing a file that is
// already modified: it reads "M" before and after, so the edit would go unseen.
// Files git lists as clean are left out, and those still surface through status.
func worktreeSnapshot(root string) map[string]string {
	st := gitStatus(root)
	for rel, code := range st {
		if fi, err := os.Stat(filepath.Join(root, filepath.FromSlash(rel))); err == nil {
			st[rel] = code + " " + strconv.FormatInt(fi.Size(), 10) + " " + strconv.FormatInt(fi.ModTime().UnixNano(), 10)
		}
	}
	return st
}

// changedSinceMaps compares two status snapshots in both directions. Outside a
// git repository both are nil and nothing is ever reported as changed, which is
// why a job carries Tracked for the client to fall back on.
func changedSinceMaps(before, after map[string]string) []string {
	out := []string{}
	for path, st := range after {
		if before[path] != st {
			out = append(out, path)
		}
	}
	// A file restored to its committed state leaves the status list entirely.
	for path := range before {
		if _, still := after[path]; !still {
			out = append(out, path)
		}
	}
	return out
}

func lineRef(l1, l2 int) string {
	if l1 == l2 {
		return strconv.Itoa(l1)
	}
	return strconv.Itoa(l1) + "-" + strconv.Itoa(l2)
}

// readLineRange returns lines l1..l2 of a file, 1-based and inclusive.
func readLineRange(abs string, l1, l2 int) (string, error) {
	data, err := os.ReadFile(abs)
	if err != nil {
		return "", err
	}
	lines := strings.Split(strings.ReplaceAll(string(data), "\r\n", "\n"), "\n")
	if l1 < 1 {
		l1 = 1
	}
	if l2 < l1 {
		l2 = l1
	}
	if l1 > len(lines) {
		return "", fmt.Errorf("line %d is past the end of %s", l1, filepath.Base(abs))
	}
	if l2 > len(lines) {
		l2 = len(lines)
	}
	return strings.Join(lines[l1-1:l2], "\n"), nil
}

// agentPrompt composes what the provider is told. It deliberately matches the
// shape of the Copy for Agent snippet in web/src/selbar.js, which these tools
// already read well.
func agentPrompt(rel string, l1, l2 int, snippet, instruction string) string {
	ext := strings.TrimPrefix(filepath.Ext(rel), ".")
	var b strings.Builder
	lineStr := fmt.Sprintf("lines %d-%d", l1, l2)
	if l1 == l2 {
		lineStr = fmt.Sprintf("line %d", l1)
	}
	fmt.Fprintf(&b, "@%s %s\n```%s\n%s\n```\n\n", rel, lineStr, ext, snippet)
	fmt.Fprintf(&b, "### Instruction\n%s\n\n", instruction)
	b.WriteString("Edit the file in place to carry out that instruction. ")
	b.WriteString("Change only what it asks for, and do not explain the change afterwards.")
	return b.String()
}

// reviewFeedbackInstruction is deliberately plain text: providers
// already understand file:line references, while Start still supplies the
// first comment's exact current snippet as the immutable anchor.
func reviewFeedbackInstruction(comments []reviewCommentView) string {
	var b strings.Builder
	b.WriteString("Address these human review comments. Inspect each referenced location, make only the needed changes, and keep unrelated work intact.\n\n")
	for i, c := range comments {
		fmt.Fprintf(&b, "%d. %s:%s — %s\n", i+1, c.Path, lineRef(c.LineStart, c.LineEnd), c.Text)
	}
	b.WriteString("\nDo not explain the changes; edit the workspace in place.")
	return b.String()
}

// ---------------------------------------------------------------- HTTP

func (s *Server) agentOrFail(w http.ResponseWriter) bool {
	if s.agent == nil {
		fail(w, http.StatusNotFound, "review-provider actions are not available in this session")
		return false
	}
	return true
}

// handleAgentJob is polled while an explicit review-provider action runs.
// id=0 (or missing) means the most recently started job.
func (s *Server) handleAgentJob(w http.ResponseWriter, r *http.Request) {
	if !s.agentOrFail(w) {
		return
	}
	id, _ := strconv.ParseInt(r.URL.Query().Get("id"), 10, 64)
	j := s.agent.Job(id)
	if j == nil {
		writeJSON(w, map[string]any{"idle": true})
		return
	}
	writeJSON(w, j)
}

func shellQuote(s string) string {
	if s == "" {
		return "''"
	}
	safe := true
	for _, r := range s {
		if !((r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') ||
			r == '-' || r == '_' || r == '.' || r == '/' || r == '=' || r == ':' || r == ',') {
			safe = false
			break
		}
	}
	if safe {
		return s
	}
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

func shellCommand(args []string) string {
	quoted := make([]string, len(args))
	for i, a := range args {
		quoted[i] = shellQuote(a)
	}
	return strings.Join(quoted, " ")
}
