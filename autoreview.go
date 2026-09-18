package main

import "os"

// Reviewing a worktree an agent is already working in.
//
// The flow this serves: an agent is told to build something, it creates its own
// `git worktree` and starts editing immediately. By the time a human opens px1
// and switches to that checkout, the work is done — and a baseline captured at
// that moment would record the finished work as the starting state, leaving an
// empty review queue.
//
// So px1 starts the session itself, and takes the baseline from the commit
// checked out when the worktree was created rather than from the working tree.
// Everything the agent has done since, committed or not, is then in the queue
// no matter when the human arrives.
//
// Only linked worktrees are started this way. The main checkout is where people
// keep unrelated work in progress, and calling that "agent changes" would be a
// lie; there, Start Review stays a deliberate act.

// autoStartReview starts a session for the current workspace when it is a
// linked worktree with no session yet. Failures are quiet: a missing session is
// an inconvenience, not a reason to refuse to serve the workspace.
func (s *Server) autoStartReview() {
	if s.review == nil || !reviewAutoStartEnabled() {
		return
	}
	if active, _ := s.review.Active(); active != nil {
		return
	}
	root := s.ix.Root()
	wt := currentWorktree(root)
	if wt == nil || wt.Main {
		return
	}
	base := worktreeBase(root)
	if base == "" {
		return
	}
	session, err := s.review.StartFromRef(base)
	if err != nil {
		uiStatus("warn", "review", "could not start a review for this worktree: "+err.Error(), 0, os.Stdout)
		return
	}
	where := wt.Branch
	if where == "" {
		where = wt.Name
	}
	_ = session
	uiStatus("ok", "review", "started for "+where+", baseline "+shortRef(base), 0, os.Stdout)
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

// reviewAutoStartEnabled reads the user setting; the default is on.
func reviewAutoStartEnabled() bool {
	if v := readSettings().ReviewAutoStart; v != nil {
		return *v
	}
	return true
}
