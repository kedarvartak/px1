package main

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"sync"
	"testing"
	"time"
)

func TestIsOptedOut(t *testing.T) {
	if !isOptedOut(true) {
		t.Fatal("expected isOptedOut(true) to be true")
	}

	os.Setenv("DO_NOT_TRACK", "1")
	if !isOptedOut(false) {
		t.Fatal("expected isOptedOut to respect DO_NOT_TRACK=1")
	}
	os.Unsetenv("DO_NOT_TRACK")

	for _, val := range []string{"0", "false", "off", "no"} {
		os.Setenv("PX1_TELEMETRY", val)
		if !isOptedOut(false) {
			t.Fatalf("expected isOptedOut to respect PX1_TELEMETRY=%s", val)
		}
	}
	os.Unsetenv("PX1_TELEMETRY")

	if isOptedOut(false) {
		t.Fatal("expected isOptedOut to be false when no opt-out is set")
	}
}

func TestFilesBucket(t *testing.T) {
	tests := []struct {
		files    int
		expected string
	}{
		{0, "<100"},
		{99, "<100"},
		{100, "100-500"},
		{499, "100-500"},
		{500, "500-2.5k"},
		{2499, "500-2.5k"},
		{2500, "2.5k-10k"},
		{9999, "2.5k-10k"},
		{10000, "10k-50k"},
		{49999, "10k-50k"},
		{50000, "50k-100k"},
		{99999, "50k-100k"},
		{100000, ">100k"},
		{500000, ">100k"},
	}

	for _, tt := range tests {
		actual := filesBucket(tt.files)
		if actual != tt.expected {
			t.Errorf("filesBucket(%d) = %q, want %q", tt.files, actual, tt.expected)
		}
	}
}

func TestTelemetrySessionLifecycle(t *testing.T) {
	var mu sync.Mutex
	var received []map[string]any

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/capture/" {
			http.NotFound(w, r)
			return
		}
		body, err := io.ReadAll(r.Body)
		if err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
		var payload map[string]any
		if err := json.Unmarshal(body, &payload); err != nil {
			http.Error(w, err.Error(), 400)
			return
		}
		mu.Lock()
		received = append(received, payload)
		mu.Unlock()
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"status":"ok"}`))
	}))
	defer server.Close()

	os.Setenv("PX1_POSTHOG_KEY", "phc_test_key_xyz")
	os.Setenv("PX1_POSTHOG_HOST", server.URL)
	defer os.Unsetenv("PX1_POSTHOG_KEY")
	defer os.Unsetenv("PX1_POSTHOG_HOST")

	tel := NewTelemetryService(false)
	if !tel.enabled {
		t.Fatal("expected telemetry to be enabled with key set")
	}

	// 1. Session start
	tel.Track("session_started", map[string]any{
		"files_bucket": "100-1k",
		"has_git":      true,
		"has_lsp":      false,
	})

	// Brief pause to ensure duration >= 0
	time.Sleep(10 * time.Millisecond)

	// 2. Session stop
	tel.Close("normal")

	mu.Lock()
	defer mu.Unlock()

	if len(received) != 3 {
		t.Fatalf("expected 3 events (session_started, session_ended, session_stopped), got %d", len(received))
	}

	// Check session_started
	evtStart := received[0]
	if evtStart["event"] != "session_started" {
		t.Errorf("expected event session_started, got %v", evtStart["event"])
	}
	propsStart := evtStart["properties"].(map[string]any)
	if propsStart["files_bucket"] != "100-1k" {
		t.Errorf("unexpected files_bucket: %v", propsStart["files_bucket"])
	}
	if propsStart["distinct_id"] == "" {
		t.Error("expected non-empty distinct_id")
	}

	// Check session_ended
	evtEnded := received[1]
	if evtEnded["event"] != "session_ended" {
		t.Errorf("expected session_ended, got %v", evtEnded["event"])
	}
	propsEnded := evtEnded["properties"].(map[string]any)
	if propsEnded["exit_reason"] != "normal" {
		t.Errorf("expected exit_reason=normal, got %v", propsEnded["exit_reason"])
	}
	if _, ok := propsEnded["duration_seconds"]; !ok {
		t.Errorf("expected duration_seconds in session_ended properties")
	}

	// Check session_stopped
	evtStop := received[2]
	if evtStop["event"] != "session_stopped" {
		t.Errorf("expected session_stopped, got %v", evtStop["event"])
	}
	propsStop := evtStop["properties"].(map[string]any)
	if propsStop["exit_reason"] != "normal" {
		t.Errorf("expected exit_reason=normal, got %v", propsStop["exit_reason"])
	}
	if _, ok := propsStop["duration_seconds"]; !ok {
		t.Errorf("expected duration_seconds in session_stopped properties")
	}

	// Verify PostHog built-in $session_id groups the events together
	startSessID, _ := propsStart["$session_id"].(string)
	endedSessID, _ := propsEnded["$session_id"].(string)
	stopSessID, _ := propsStop["$session_id"].(string)
	if startSessID == "" {
		t.Error("expected non-empty $session_id")
	}
	if startSessID != endedSessID || startSessID != stopSessID {
		t.Errorf("expected matching $session_id across session events: start=%q ended=%q stop=%q",
			startSessID, endedSessID, stopSessID)
	}
}

func TestTelemetryDisabledWithoutKey(t *testing.T) {
	os.Unsetenv("PX1_POSTHOG_KEY")
	tel := NewTelemetryService(false)
	if tel.enabled {
		t.Fatal("expected telemetry disabled without key")
	}
	tel.Track("session_started", nil)
	tel.Close("normal")
}
