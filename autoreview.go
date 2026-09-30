package main

import "os"

// Optional local automatic baselines mean px1 can capture the workspace before
// a harness writes its first file, without requiring a harness-specific
// command or plugin. The default path is an explicit Review-panel start.
//
// A linked worktree may already contain agent work before px1 opens it. Its
// baseline therefore comes from the commit checked out when the worktree was
// created. Every other workspace, including the main checkout, snapshots the
// current working tree. That preserves pre-existing local work while queuing
// only later writes.

// autoStartReview starts a session for the current workspace when no session
// exists. Failures are quiet: a missing session is an inconvenience, not a
// reason to refuse to serve the workspace.
func (s *Server) autoStartReview() {
	if s.review == nil || !reviewAutoStartEnabled() {
		return
	}
	if active, _ := s.review.Active(); active != nil {
		return
	}
	root := s.ix.Root()
	wt := currentWorktree(root)
	base := ""
	if wt != nil && !wt.Main {
		base = worktreeBase(root)
	}
	var err error
	if base != "" {
		_, err = s.review.StartFromRef(base)
	} else {
		_, err = s.review.Start()
	}
	if err != nil {
		uiStatus("warn", "review", "could not start a review: "+err.Error(), 0, os.Stdout)
		return
	}
	if base != "" {
		where := wt.Branch
		if where == "" {
			where = wt.Name
		}
		uiStatus("ok", "review", "started for "+where+", baseline "+shortRef(base), 0, os.Stdout)
		return
	}
	uiStatus("ok", "review", "started, baseline captured", 0, os.Stdout)
}

// currentWorktree is the entry for the checkout being served, or nil outside a
// repository with worktrees.
func currentWorktree(root string) *worktree {
	for _, wt := range worktrees(root) {
		if wt.Current {
			cp := wt
			return &cp
		}
	}
	return nil
}

func shortRef(ref string) string {
	if len(ref) > 8 {
		return ref[:8]
	}
	return ref
}

// reviewAutoStartEnabled reads the user setting; the default is off so local
// automatic-baseline behavior remains an explicit opt-in.
func reviewAutoStartEnabled() bool {
	if v := readSettings().ReviewAutoStart; v != nil {
		return *v
	}
	return false
}
