// Package billing contains tenant-scoped hosted billing contracts.
package billing

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"
)

const MaxStripeEventBytes = 1 << 20

type StripeEvent struct {
	ID   string `json:"id"`
	Type string `json:"type"`
	Data struct {
		Object json.RawMessage `json:"object"`
	} `json:"data"`
}

type StripeEventStore interface {
	InvoicePaid(context.Context, string, json.RawMessage) error
	SubscriptionUpdated(context.Context, string, json.RawMessage) error
}

type StripeWebhook struct {
	Secret    []byte
	Tolerance time.Duration
	Now       func() time.Time
	Store     StripeEventStore
}

func (h StripeWebhook) Handle(ctx context.Context, body []byte, signature string) error {
	if len(h.Secret) == 0 || h.Store == nil {
		return errors.New("Stripe webhook secret and store are required")
	}
	if len(body) == 0 || len(body) > MaxStripeEventBytes {
		return errors.New("Stripe webhook body is empty or too large")
	}
	timestamp, signatures, err := parseStripeSignature(signature)
	if err != nil {
		return err
	}
	now := time.Now()
	if h.Now != nil {
		now = h.Now()
	}
	tolerance := h.Tolerance
	if tolerance <= 0 {
		tolerance = 5 * time.Minute
	}
	when := time.Unix(timestamp, 0)
	if when.Before(now.Add(-tolerance)) || when.After(now.Add(tolerance)) {
		return errors.New("Stripe webhook timestamp is outside tolerance")
	}
	mac := hmac.New(sha256.New, h.Secret)
	fmt.Fprintf(mac, "%d.%s", timestamp, body)
	expected := mac.Sum(nil)
	valid := false
	for _, encoded := range signatures {
		candidate, decodeErr := hex.DecodeString(encoded)
		if decodeErr == nil && hmac.Equal(expected, candidate) {
			valid = true
		}
	}
	if !valid {
		return errors.New("Stripe webhook signature is invalid")
	}
	var event StripeEvent
	if err := json.Unmarshal(body, &event); err != nil || event.ID == "" || len(event.Data.Object) == 0 {
		return errors.New("Stripe webhook event is invalid")
	}
	switch event.Type {
	case "invoice.paid":
		return h.Store.InvoicePaid(ctx, event.ID, event.Data.Object)
	case "customer.subscription.updated":
		return h.Store.SubscriptionUpdated(ctx, event.ID, event.Data.Object)
	default:
		return nil
	}
}

func parseStripeSignature(header string) (int64, []string, error) {
	var timestamp int64
	var signatures []string
	for _, part := range strings.Split(header, ",") {
		key, value, ok := strings.Cut(strings.TrimSpace(part), "=")
		if !ok {
			continue
		}
		if key == "t" {
			parsed, err := strconv.ParseInt(value, 10, 64)
			if err != nil {
				return 0, nil, errors.New("invalid Stripe signature timestamp")
			}
			timestamp = parsed
		}
		if key == "v1" {
			signatures = append(signatures, value)
		}
	}
	if timestamp == 0 || len(signatures) == 0 {
		return 0, nil, errors.New("Stripe signature requires t and v1")
	}
	return timestamp, signatures, nil
}

type PortalClient interface {
	CreatePortalSession(context.Context, string, string) (string, error)
}

type StripeClient interface {
	PortalClient
	EnsureCustomer(context.Context, int64, string) (string, error)
	ReportMeteredUsage(context.Context, string, string, string, int64, time.Time) error
}

func ProvisionCustomer(ctx context.Context, client StripeClient, installationID int64, billingEmail string) (string, error) {
	if client == nil || installationID <= 0 || !strings.Contains(billingEmail, "@") {
		return "", errors.New("Stripe client, tenant, and billing email are required")
	}
	return client.EnsureCustomer(ctx, installationID, billingEmail)
}

func RecordMeteredReview(ctx context.Context, client StripeClient, customerID, priceID, eventID string, credits int64, occurredAt time.Time) error {
	if client == nil || !strings.HasPrefix(customerID, "cus_") || !strings.HasPrefix(priceID, "price_") || eventID == "" || credits <= 0 || occurredAt.IsZero() {
		return errors.New("Stripe client, customer, metered price, event, positive credits, and occurrence time are required")
	}
	return client.ReportMeteredUsage(ctx, customerID, priceID, eventID, credits, occurredAt.UTC())
}

func OpenCustomerPortal(ctx context.Context, client PortalClient, customerID, returnURL string) (string, error) {
	if client == nil || !strings.HasPrefix(customerID, "cus_") || !strings.HasPrefix(returnURL, "https://") {
		return "", errors.New("Stripe customer and HTTPS return URL are required")
	}
	return client.CreatePortalSession(ctx, customerID, returnURL)
}
