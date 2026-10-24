// Package webhook authenticates GitHub webhook deliveries and hands accepted
// payloads to a durable queue. It intentionally performs no repository work in
// the request path.
package webhook

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
)

const defaultMaxBodyBytes int64 = 10 << 20

var defaultEvents = map[string]bool{
	"installation":                true,
	"installation_repositories":   true,
	"issue_comment":               true,
	"pull_request":                true,
	"pull_request_review_comment": true,
	"push":                        true,
}

// Delivery is the immutable envelope stored by the queue. Consumers must use
// ID as their idempotency key too; webhook acknowledgement alone is not proof
// that later processing completed.
type Delivery struct {
	ID      string
	Event   string
	Payload json.RawMessage
}

// Acceptor deduplicates and durably enqueues a delivery in one transaction.
// Accept returns false when the delivery ID already exists.
type Acceptor interface {
	Accept(context.Context, Delivery) (bool, error)
}

type Handler struct {
	Secret        []byte
	Sink          Acceptor
	AllowedEvents map[string]bool
	MaxBodyBytes  int64
}

func (h Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if len(h.Secret) == 0 || h.Sink == nil {
		http.Error(w, "webhook is not configured", http.StatusServiceUnavailable)
		return
	}

	limit := h.MaxBodyBytes
	if limit <= 0 {
		limit = defaultMaxBodyBytes
	}
	body, err := io.ReadAll(io.LimitReader(r.Body, limit+1))
	if err != nil {
		http.Error(w, "read webhook body", http.StatusBadRequest)
		return
	}
	if int64(len(body)) > limit {
		http.Error(w, "webhook body too large", http.StatusRequestEntityTooLarge)
		return
	}
	if !validSignature(h.Secret, body, r.Header.Get("X-Hub-Signature-256")) {
		http.Error(w, "invalid webhook signature", http.StatusUnauthorized)
		return
	}

	id := strings.TrimSpace(r.Header.Get("X-GitHub-Delivery"))
	event := strings.TrimSpace(r.Header.Get("X-GitHub-Event"))
	if id == "" || len(id) > 200 || event == "" {
		http.Error(w, "missing or invalid GitHub delivery metadata", http.StatusBadRequest)
		return
	}
	allowed := h.AllowedEvents
	if allowed == nil {
		allowed = defaultEvents
	}
	if !allowed[event] {
		http.Error(w, "unsupported GitHub event", http.StatusBadRequest)
		return
	}
	if !json.Valid(body) {
		http.Error(w, "webhook body must be JSON", http.StatusBadRequest)
		return
	}

	accepted, err := h.Sink.Accept(r.Context(), Delivery{ID: id, Event: event, Payload: append(json.RawMessage(nil), body...)})
	if err != nil {
		http.Error(w, "enqueue webhook delivery", http.StatusServiceUnavailable)
		return
	}
	if !accepted {
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, "duplicate\n")
		return
	}

	w.WriteHeader(http.StatusOK)
	_, _ = io.WriteString(w, "accepted\n")
}

func validSignature(secret, body []byte, header string) bool {
	const prefix = "sha256="
	if !strings.HasPrefix(header, prefix) {
		return false
	}
	want, err := hex.DecodeString(strings.TrimPrefix(header, prefix))
	if err != nil || len(want) != sha256.Size {
		return false
	}
	mac := hmac.New(sha256.New, secret)
	_, _ = mac.Write(body)
	return hmac.Equal(want, mac.Sum(nil))
}

func Signature(secret, body []byte) string {
	mac := hmac.New(sha256.New, secret)
	_, _ = mac.Write(body)
	return fmt.Sprintf("sha256=%x", mac.Sum(nil))
}
