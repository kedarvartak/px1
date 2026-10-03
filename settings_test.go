package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func TestSettingsDefaults(t *testing.T) {
	isolateSettings(t)
	m := readMergedSettingsMap()
	if len(m) != 0 {
		t.Errorf("fresh settings should be empty, got %v", m)
	}
	if reviewAutoStartEnabled() {
		t.Errorf("expected local automatic reviews to be opt-in")
	}
}

func TestSettingsPreserveNonAgentValues(t *testing.T) {
	cfg := isolateSettings(t)

	// Step 1: Update editor settings
	err := updateSettingsMap(map[string]any{
		"editor.fontSize": 16.0,
		"custom.property": "hello",
	})
	if err != nil {
		t.Fatalf("updateSettingsMap failed: %v", err)
	}

	// Step 2: Agent selects a harness (which calls writeSettings)
	err = writeSettings(settings{
		Agent: "claude",
		Models: map[string]string{
			"claude": "sonnet",
		},
	})
	if err != nil {
		t.Fatalf("writeSettings failed: %v", err)
	}

	// Step 3: Verify editor settings were NOT wiped
	m := readMergedSettingsMap()
	if m["editor.fontSize"] != 16.0 {
		t.Errorf("editor.fontSize was overwritten: got %v, want 16", m["editor.fontSize"])
	}
	if m["custom.property"] != "hello" {
		t.Errorf("custom.property was lost: got %v, want hello", m["custom.property"])
	}
	if m["agent"] != "claude" || m["agent.harness"] != "claude" {
		t.Errorf("agent not set properly: %v", m["agent"])
	}

	// Step 4: Verify file on disk
	data, err := os.ReadFile(filepath.Join(cfg, "px1", "settings.json"))
	if err != nil {
		t.Fatalf("failed to read settings file: %v", err)
	}
	var diskMap map[string]any
	if err := json.Unmarshal(data, &diskMap); err != nil {
		t.Fatalf("corrupt settings JSON: %v", err)
	}
	if diskMap["editor.fontSize"] != 16.0 {
		t.Errorf("diskMap missing editor.fontSize: %v", diskMap)
	}
}

func TestSettingsAPIEndpoints(t *testing.T) {
	isolateSettings(t)
	root := t.TempDir()
	srv := agentServer(t, root, "echo")

	// 1. GET /api/settings
	req := httptest.NewRequest(http.MethodGet, "/api/settings", nil)
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /api/settings failed: code %d, body: %s", rec.Code, rec.Body.String())
	}
	var getResp map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &getResp); err != nil {
		t.Fatalf("invalid GET json response: %v", err)
	}
	if _, ok := getResp["raw"]; !ok {
		t.Errorf("missing raw settings in GET /api/settings response")
	}

	// 2. POST /api/settings with key/value updates
	payload := map[string]any{
		"editor.tabSize":              2,
		"diffEditor.renderSideBySide": false,
	}
	b, _ := json.Marshal(payload)
	postReq := httptest.NewRequest(http.MethodPost, "/api/settings", bytes.NewReader(b))
	postReq.Host = "127.0.0.1:7777"
	postReq.Header.Set("Origin", "http://127.0.0.1:7777")
	postReq.Header.Set("Content-Type", "application/json")
	postRec := httptest.NewRecorder()
	srv.ServeHTTP(postRec, postReq)
	if postRec.Code != http.StatusOK {
		t.Fatalf("POST /api/settings failed: code %d, body: %s", postRec.Code, postRec.Body.String())
	}

	// 3. Verify changes persisted
	m := readMergedSettingsMap()
	if m["editor.tabSize"] != float64(2) && m["editor.tabSize"] != 2 {
		t.Errorf("expected editor.tabSize 2, got %v", m["editor.tabSize"])
	}
	if m["diffEditor.renderSideBySide"] != false {
		t.Errorf("expected diffEditor.renderSideBySide false, got %v", m["diffEditor.renderSideBySide"])
	}
	// 4. POST /api/settings with raw JSON
	rawPayload := map[string]any{
		"raw": "{\n  \"editor.fontSize\": 15\n}\n",
	}
	rb, _ := json.Marshal(rawPayload)
	rawReq := httptest.NewRequest(http.MethodPost, "/api/settings", bytes.NewReader(rb))
	rawReq.Host = "127.0.0.1:7777"
	rawReq.Header.Set("Origin", "http://127.0.0.1:7777")
	rawReq.Header.Set("Content-Type", "application/json")
	rawRec := httptest.NewRecorder()
	srv.ServeHTTP(rawRec, rawReq)
	if rawRec.Code != http.StatusOK {
		t.Fatalf("POST /api/settings raw failed: code %d, body: %s", rawRec.Code, rawRec.Body.String())
	}

	m2 := readMergedSettingsMap()
	if m2["editor.fontSize"] != float64(15) && m2["editor.fontSize"] != 15 {
		t.Errorf("expected editor.fontSize 15 from raw, got %v", m2["editor.fontSize"])
	}
}
