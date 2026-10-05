package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestAccessPolicyMatrix(t *testing.T) {
	isolateSettings(t)
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "main.go"), []byte("package main\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	outside := filepath.Join(filepath.Dir(root), "px1-secret.txt")
	t.Cleanup(func() { os.Remove(outside) })
	s := NewServer(NewIndex(root), nil)
	const remotePeer = "10.9.8.7:54321"
	const localPeer = "127.0.0.1:54321"

	postPatch := func(peer, host, origin, token string) (int, string) {
		req := httptest.NewRequest(http.MethodPost, "/api/review/patch", strings.NewReader(`{"path":"../px1-secret.txt"}`))
		req.RemoteAddr = peer
		req.Host = host
		if origin != "" {
			req.Header.Set("Origin", origin)
		}
		if token != "" {
			req.Header.Set("Authorization", "Bearer "+token)
		}
		rec := httptest.NewRecorder()
		s.handleReviewPatch(rec, req)
		var body map[string]string
		_ = json.Unmarshal(rec.Body.Bytes(), &body)
		return rec.Code, body["error"]
	}
	postChecks := func() (int, string) {
		req := httptest.NewRequest(http.MethodPost, "/api/review/checks", nil)
		req.RemoteAddr = remotePeer
		req.Host = "10.1.2.3:7777"
		req.Header.Set("Origin", "http://10.1.2.3:7777")
		req.Header.Set("Authorization", "Bearer tok")
		rec := httptest.NewRecorder()
		s.handleReviewChecks(rec, req)
		var body map[string]string
		_ = json.Unmarshal(rec.Body.Bytes(), &body)
		return rec.Code, body["error"]
	}

	if code, msg := postPatch(remotePeer, "10.1.2.3:7777", "http://10.1.2.3:7777", ""); code != http.StatusForbidden || !strings.Contains(msg, "-allow-patch") {
		t.Fatalf("default remote patch = %d %q", code, msg)
	}
	if code, msg := postPatch(remotePeer, "review.example:7777", "http://review.example:7777", ""); code != http.StatusForbidden || !strings.Contains(msg, "-allow-patch") {
		t.Fatalf("default hostname patch = %d %q", code, msg)
	}
	if code, msg := postPatch(remotePeer, "127.0.0.1:7777", "http://127.0.0.1:7777", ""); code != http.StatusForbidden || !strings.Contains(msg, "-allow-patch") {
		t.Fatalf("remote peer spoofing loopback Host = %d %q", code, msg)
	}
	settingsReq := httptest.NewRequest(http.MethodPost, "/api/settings", strings.NewReader(`{}`))
	settingsReq.RemoteAddr = remotePeer
	settingsReq.Host = "10.1.2.3:7777"
	settingsReq.Header.Set("Origin", "http://10.1.2.3:7777")
	settingsRec := httptest.NewRecorder()
	s.handleSettings(settingsRec, settingsReq)
	if settingsRec.Code != http.StatusForbidden {
		t.Fatalf("default remote settings write = %d %s", settingsRec.Code, settingsRec.Body.String())
	}
	if code, msg := postChecks(); code != http.StatusForbidden || !strings.Contains(msg, "-allow-checks") {
		t.Fatalf("default remote checks = %d %q", code, msg)
	}

	s.SetAccessPolicy(true, false, false, "tok")
	req := httptest.NewRequest(http.MethodPost, "/api/lsp/install", nil)
	req.RemoteAddr = remotePeer
	req.Host = "10.1.2.3:7777"
	req.Header.Set("Origin", "http://10.1.2.3:7777")
	req.Header.Set("Authorization", "Bearer tok")
	rec := httptest.NewRecorder()
	s.handleLSPInstall(rec, req)
	if rec.Code != http.StatusForbidden || !strings.Contains(rec.Body.String(), "-allow-agent") {
		t.Fatalf("allow-patch also allowed agent: %d %s", rec.Code, rec.Body.String())
	}
	if code, msg := postPatch(remotePeer, "10.1.2.3:7777", "http://10.1.2.3:7777", "tok"); code != http.StatusBadRequest || msg != "bad path" {
		t.Fatalf("authed traversal = %d %q", code, msg)
	}
	if _, err := os.Stat(outside); !os.IsNotExist(err) {
		t.Fatal("path traversal wrote outside the workspace")
	}
	if code, _ := postPatch(remotePeer, "10.1.2.3:7777", "http://10.1.2.3:7777", "nope"); code != http.StatusUnauthorized {
		t.Fatalf("wrong token = %d", code)
	}
	if code, msg := postPatch(remotePeer, "review.example:7777", "https://evil.example", "tok"); code != http.StatusForbidden || !strings.Contains(msg, "did not come from px1") {
		t.Fatalf("cross-origin hostname = %d %q", code, msg)
	}

	s.SetAccessPolicy(false, true, false, "tok")
	if code, msg := postPatch(remotePeer, "10.1.2.3:7777", "http://10.1.2.3:7777", "tok"); code != http.StatusForbidden || !strings.Contains(msg, "-allow-patch") {
		t.Fatalf("allow-agent also allowed patch: %d %q", code, msg)
	}
	warm := httptest.NewRequest(http.MethodGet, "/api/lsp/warm?path=main.go", nil)
	warm.RemoteAddr = remotePeer
	warm.Host = "10.1.2.3:7777"
	warm.Header.Set("Authorization", "Bearer tok")
	warmRec := httptest.NewRecorder()
	s.handleLSPWarm(warmRec, warm)
	if warmRec.Code != http.StatusOK {
		t.Fatalf("authed remote LSP warm = %d %s", warmRec.Code, warmRec.Body.String())
	}
	s.SetAccessPolicy(false, false, false, "")
	warm = httptest.NewRequest(http.MethodGet, "/api/lsp/warm?path=main.go", nil)
	warm.RemoteAddr = remotePeer
	warm.Host = "10.1.2.3:7777"
	warmRec = httptest.NewRecorder()
	s.handleLSPWarm(warmRec, warm)
	if warmRec.Code != http.StatusForbidden {
		t.Fatalf("default remote LSP warm = %d %s", warmRec.Code, warmRec.Body.String())
	}

	s.SetAccessPolicy(false, false, true, "tok")
	if code, msg := postChecks(); code != http.StatusNotImplemented || !strings.Contains(msg, "does not execute") {
		t.Fatalf("allow-checks executed a command: %d %q", code, msg)
	}
	if code, msg := postPatch(remotePeer, "10.1.2.3:7777", "http://10.1.2.3:7777", "tok"); code != http.StatusForbidden {
		t.Fatalf("allow-checks also allowed patch: %d %q", code, msg)
	}

	s.SetAccessPolicy(true, false, false, "")
	if code, msg := postPatch(remotePeer, "10.1.2.3:7777", "http://10.1.2.3:7777", ""); code != http.StatusForbidden || !strings.Contains(msg, "PX1_REMOTE_TOKEN") {
		t.Fatalf("flag without token = %d %q", code, msg)
	}

	s.SetAccessPolicy(false, false, false, "")
	if code, msg := postPatch(localPeer, "127.0.0.1:7777", "http://127.0.0.1:7777", ""); code != http.StatusBadRequest || msg != "bad path" {
		t.Fatalf("loopback patch = %d %q", code, msg)
	}
	if code, _ := postPatch(localPeer, "127.0.0.1:7777", "https://evil.example", ""); code != http.StatusForbidden {
		t.Fatalf("loopback cross-origin = %d", code)
	}

	areq := httptest.NewRequest(http.MethodGet, "/api/audit", nil)
	areq.RemoteAddr = localPeer
	areq.Host = "127.0.0.1:7777"
	ar := httptest.NewRecorder()
	s.handleAudit(ar, areq)
	if ar.Code != http.StatusOK || !strings.Contains(ar.Body.String(), `"action":"patch"`) {
		t.Fatalf("audit = %d %s", ar.Code, ar.Body.String())
	}
	areq = httptest.NewRequest(http.MethodGet, "/api/audit", nil)
	areq.RemoteAddr = remotePeer
	areq.Host = "10.1.2.3:7777"
	ar = httptest.NewRecorder()
	s.handleAudit(ar, areq)
	if ar.Code != http.StatusUnauthorized {
		t.Fatalf("remote audit = %d", ar.Code)
	}
}

func TestPermitProcessSkipsRoutineLocalAudit(t *testing.T) {
	s := NewServer(NewIndex(t.TempDir()), nil)
	req := httptest.NewRequest(http.MethodGet, "/api/lsp/warm?path=main.go", nil)
	req.RemoteAddr = "127.0.0.1:54321"
	req.Host = "127.0.0.1:7777"
	rec := httptest.NewRecorder()

	if !s.permitProcess(rec, req) {
		t.Fatalf("local process read denied: %d %s", rec.Code, rec.Body.String())
	}
	if len(s.audit) != 0 {
		t.Fatalf("routine local process read added %d audit entries", len(s.audit))
	}
}
