package main

import (
	"crypto/subtle"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// Remote writes stay off unless that one capability was enabled and the
// request presents the server token. Loopback keeps today's local review
// actions. The three capabilities never imply each other.

const (
	capPatch  = "patch"
	capAgent  = "agent"
	capChecks = "checks"

	accessLoopback = "loopback"
	accessIP       = "ip"
	accessName     = "name"

	auditMax = 200
)

type accessPolicy struct {
	allowPatch  bool
	allowAgent  bool
	allowChecks bool
	token       string
}

type auditEntry struct {
	At      string `json:"at"`
	Action  string `json:"action"`
	Path    string `json:"path"`
	Access  string `json:"access"`
	Allowed bool   `json:"allowed"`
	Error   string `json:"error,omitempty"`
}

func (s *Server) SetAccessPolicy(patch, agent, checks bool, token string) {
	s.policy = accessPolicy{allowPatch: patch, allowAgent: agent, allowChecks: checks, token: token}
}

func (p accessPolicy) enabled(cap string) bool {
	switch cap {
	case capPatch:
		return p.allowPatch
	case capAgent:
		return p.allowAgent
	case capChecks:
		return p.allowChecks
	default:
		return false
	}
}

func requestHost(r *http.Request) string {
	return networkHost(r.Host)
}

func requestPeer(r *http.Request) string {
	return networkHost(r.RemoteAddr)
}

func networkHost(address string) string {
	host := address
	if h, _, err := net.SplitHostPort(host); err == nil {
		host = h
	}
	return strings.Trim(host, "[]")
}

func accessClass(host string) string {
	if strings.EqualFold(host, "localhost") {
		return accessLoopback
	}
	ip := net.ParseIP(host)
	if ip == nil {
		return accessName
	}
	if ip.IsLoopback() {
		return accessLoopback
	}
	return accessIP
}

// requestAccessClass grants loopback privileges only when both the HTTP
// authority and the connected peer are loopback. Host is client-controlled;
// trusting it alone lets a remote client impersonate localhost. Forwarded
// headers are deliberately ignored until px1 has an explicit trusted-proxy
// configuration.
func requestAccessClass(r *http.Request) string {
	hostClass := accessClass(requestHost(r))
	if hostClass != accessLoopback {
		return hostClass
	}
	if accessClass(requestPeer(r)) == accessLoopback {
		return accessLoopback
	}
	return accessIP
}

func originMatches(r *http.Request) bool {
	o, err := url.Parse(r.Header.Get("Origin"))
	return err == nil && o.Host != "" && strings.EqualFold(o.Host, r.Host)
}

func bearerOK(r *http.Request, token string) bool {
	const prefix = "Bearer "
	h := r.Header.Get("Authorization")
	if len(h) < len(prefix) || !strings.EqualFold(h[:len(prefix)], prefix) {
		return false
	}
	got := h[len(prefix):]
	if len(got) != len(token) || token == "" {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(got), []byte(token)) == 1
}

// permit is the gate for filesystem edits, process execution, and check
// execution. It fails closed and writes the response when it refuses.
func (s *Server) permit(w http.ResponseWriter, r *http.Request, cap string) bool {
	if r.Method != http.MethodPost {
		s.recordAudit(r, cap, false, "POST only")
		fail(w, http.StatusMethodNotAllowed, "POST only")
		return false
	}
	if !originMatches(r) {
		s.recordAudit(r, cap, false, "origin mismatch")
		fail(w, http.StatusForbidden, "request did not come from px1")
		return false
	}
	return s.permitCapability(w, r, cap)
}

// permitProcess protects read-shaped LSP routes that may lazily start a
// language-server process. Local readers keep the existing behavior; remote
// readers need the independently enabled agent capability and its token.
func (s *Server) permitProcess(w http.ResponseWriter, r *http.Request) bool {
	if r.Method != http.MethodGet {
		s.recordAudit(r, capAgent, false, "GET only")
		fail(w, http.StatusMethodNotAllowed, "GET only")
		return false
	}
	// Local editor reads are routine and can be very frequent. They do not
	// cross the remote capability boundary, so keep them out of the bounded
	// security audit log while continuing to audit every remote attempt.
	if requestAccessClass(r) == accessLoopback {
		return true
	}
	return s.permitCapability(w, r, capAgent)
}

func (s *Server) permitCapability(w http.ResponseWriter, r *http.Request, cap string) bool {
	if requestAccessClass(r) == accessLoopback {
		s.recordAudit(r, cap, true, "")
		return true
	}
	flag := "-allow-" + cap
	if !s.policy.enabled(cap) {
		msg := "remote " + cap + " is disabled; pass " + flag + " to enable it without enabling other remote actions"
		s.recordAudit(r, cap, false, msg)
		fail(w, http.StatusForbidden, msg)
		return false
	}
	if s.policy.token == "" {
		msg := "remote " + cap + " needs -remote-token or PX1_REMOTE_TOKEN; refusing without authentication"
		s.recordAudit(r, cap, false, msg)
		fail(w, http.StatusForbidden, msg)
		return false
	}
	if !bearerOK(r, s.policy.token) {
		msg := "remote " + cap + " needs Authorization: Bearer <token>"
		s.recordAudit(r, cap, false, msg)
		fail(w, http.StatusUnauthorized, msg)
		return false
	}
	s.recordAudit(r, cap, true, "")
	return true
}

func (s *Server) recordAudit(r *http.Request, action string, allowed bool, reason string) {
	s.auditMu.Lock()
	defer s.auditMu.Unlock()
	s.audit = append(s.audit, auditEntry{
		At:      time.Now().UTC().Format(time.RFC3339),
		Action:  action,
		Path:    r.URL.Path,
		Access:  requestAccessClass(r),
		Allowed: allowed,
		Error:   reason,
	})
	if extra := len(s.audit) - auditMax; extra > 0 {
		s.audit = append([]auditEntry(nil), s.audit[extra:]...)
	}
}

func (s *Server) policyView(r *http.Request) map[string]any {
	class := requestAccessClass(r)
	remote := class != accessLoopback
	flags := s.policy.allowPatch || s.policy.allowAgent || s.policy.allowChecks
	authed := s.policy.token != ""
	return map[string]any{
		"access":        class,
		"remote":        remote,
		"patch":         !remote || (s.policy.allowPatch && authed),
		"agent":         !remote || (s.policy.allowAgent && authed),
		"checks":        !remote || (s.policy.allowChecks && authed),
		"tokenRequired": remote && authed && flags,
		"needsToken":    remote && flags && !authed,
	}
}

func (s *Server) handleAudit(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		fail(w, http.StatusMethodNotAllowed, "GET only")
		return
	}
	if requestAccessClass(r) != accessLoopback {
		if s.policy.token == "" || !bearerOK(r, s.policy.token) {
			fail(w, http.StatusUnauthorized, "remote audit log needs Authorization: Bearer <token>")
			return
		}
	}
	s.auditMu.Lock()
	entries := append([]auditEntry(nil), s.audit...)
	s.auditMu.Unlock()
	if entries == nil {
		entries = []auditEntry{}
	}
	writeJSON(w, map[string]any{"entries": entries})
}
