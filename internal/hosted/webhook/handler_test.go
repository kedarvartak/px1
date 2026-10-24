package webhook

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

type memorySink struct {
	mu         sync.Mutex
	seen       map[string]bool
	deliveries []Delivery
	err        error
}

func (s *memorySink) Accept(_ context.Context, delivery Delivery) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.err != nil {
		return false, s.err
	}
	if s.seen == nil {
		s.seen = map[string]bool{}
	}
	if s.seen[delivery.ID] {
		return false, nil
	}
	s.seen[delivery.ID] = true
	s.deliveries = append(s.deliveries, delivery)
	return true, nil
}

func request(secret []byte, id, event, body string) *http.Request {
	r := httptest.NewRequest(http.MethodPost, "/webhooks/github", strings.NewReader(body))
	r.Header.Set("X-Hub-Signature-256", Signature(secret, []byte(body)))
	r.Header.Set("X-GitHub-Delivery", id)
	r.Header.Set("X-GitHub-Event", event)
	return r
}

func TestHandlerAuthenticatesDeduplicatesAndEnqueues(t *testing.T) {
	secret := []byte("development-secret")
	sink := &memorySink{}
	handler := Handler{Secret: secret, Sink: sink}

	for i := 0; i < 2; i++ {
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, request(secret, "delivery-1", "pull_request", `{"action":"opened"}`))
		if response.Code != http.StatusOK {
			t.Fatalf("request %d status = %d, body = %s", i+1, response.Code, response.Body.String())
		}
	}
	if len(sink.deliveries) != 1 {
		t.Fatalf("queued %d deliveries, want 1", len(sink.deliveries))
	}
	if sink.deliveries[0].ID != "delivery-1" || sink.deliveries[0].Event != "pull_request" {
		t.Fatalf("unexpected delivery: %+v", sink.deliveries[0])
	}
}

func TestHandlerAcceptsCheckRunRetryActions(t *testing.T) {
	secret := []byte("development-secret")
	sink := &memorySink{}
	response := httptest.NewRecorder()
	Handler{Secret: secret, Sink: sink}.ServeHTTP(response, request(secret, "retry-1", "check_run", `{"action":"requested_action","requested_action":{"identifier":"retry_review"}}`))
	if response.Code != http.StatusOK || len(sink.deliveries) != 1 || sink.deliveries[0].Event != "check_run" {
		t.Fatalf("status = %d, deliveries = %#v", response.Code, sink.deliveries)
	}
}

func TestHandlerRejectsInvalidRequestsBeforeReservation(t *testing.T) {
	secret := []byte("development-secret")
	sink := &memorySink{}
	handler := Handler{Secret: secret, Sink: sink, MaxBodyBytes: 16}

	tests := []struct {
		name string
		req  *http.Request
		want int
	}{
		{"bad signature", request([]byte("wrong"), "delivery-1", "pull_request", `{}`), http.StatusUnauthorized},
		{"unsupported event", request(secret, "delivery-2", "fork", `{}`), http.StatusBadRequest},
		{"invalid json", request(secret, "delivery-3", "push", `{`), http.StatusBadRequest},
		{"too large", request(secret, "delivery-4", "push", `{"value":"too long"}`), http.StatusRequestEntityTooLarge},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, test.req)
			if response.Code != test.want {
				t.Fatalf("status = %d, want %d: %s", response.Code, test.want, response.Body.String())
			}
		})
	}
	if len(sink.deliveries) != 0 {
		t.Fatalf("queued invalid deliveries: %+v", sink.deliveries)
	}
}

func TestHandlerLeavesDeliveryRetryableWhenAtomicAcceptFails(t *testing.T) {
	secret := []byte("development-secret")
	sink := &memorySink{err: errors.New("database unavailable")}
	handler := Handler{Secret: secret, Sink: sink}

	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request(secret, "delivery-1", "push", `{}`))
	if response.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusServiceUnavailable)
	}
	sink.err = nil
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, request(secret, "delivery-1", "push", `{}`))
	if response.Code != http.StatusOK || len(sink.deliveries) != 1 {
		t.Fatalf("delivery was not retryable: status=%d queued=%d", response.Code, len(sink.deliveries))
	}
}
