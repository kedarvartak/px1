package billing

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"testing"
	"time"
)

type stripeStore struct{ invoice, subscription int }

func (s *stripeStore) InvoicePaid(context.Context, string, json.RawMessage) error {
	s.invoice++
	return nil
}

type stripeClient struct{ usage int64 }

func (c *stripeClient) EnsureCustomer(context.Context, int64, string) (string, error) {
	return "cus_test", nil
}
func (c *stripeClient) ReportMeteredUsage(_ context.Context, _, _, _ string, quantity int64, _ time.Time) error {
	c.usage += quantity
	return nil
}
func (c *stripeClient) CreatePortalSession(context.Context, string, string) (string, error) {
	return "https://billing.stripe.test/session", nil
}
func (s *stripeStore) SubscriptionUpdated(context.Context, string, json.RawMessage) error {
	s.subscription++
	return nil
}

func stripeHeader(secret []byte, timestamp int64, body []byte) string {
	mac := hmac.New(sha256.New, secret)
	fmt.Fprintf(mac, "%d.%s", timestamp, body)
	return fmt.Sprintf("t=%d,v1=%s", timestamp, hex.EncodeToString(mac.Sum(nil)))
}

func TestStripeWebhookVerifiesAndDispatchesSupportedEvents(t *testing.T) {
	now := time.Unix(1000, 0)
	secret := []byte("whsec_test")
	store := &stripeStore{}
	handler := StripeWebhook{Secret: secret, Store: store, Now: func() time.Time { return now }}
	for _, body := range [][]byte{
		[]byte(`{"id":"evt_1","type":"invoice.paid","data":{"object":{"customer":"cus_1"}}}`),
		[]byte(`{"id":"evt_2","type":"customer.subscription.updated","data":{"object":{"id":"sub_1"}}}`),
	} {
		if err := handler.Handle(context.Background(), body, stripeHeader(secret, now.Unix(), body)); err != nil {
			t.Fatal(err)
		}
	}
	if store.invoice != 1 || store.subscription != 1 {
		t.Fatalf("dispatch = %#v", store)
	}
}

func TestStripeWebhookRejectsTamperingAndReplay(t *testing.T) {
	now := time.Unix(1000, 0)
	body := []byte(`{"id":"evt","type":"invoice.paid","data":{"object":{}}}`)
	handler := StripeWebhook{Secret: []byte("secret"), Store: &stripeStore{}, Now: func() time.Time { return now }}
	if err := handler.Handle(context.Background(), append(body, ' '), stripeHeader([]byte("secret"), now.Unix(), body)); err == nil {
		t.Fatal("accepted tampered body")
	}
	if err := handler.Handle(context.Background(), body, stripeHeader([]byte("secret"), now.Add(-10*time.Minute).Unix(), body)); err == nil {
		t.Fatal("accepted stale event")
	}
}

func TestStripeCustomerMeterAndPortalContracts(t *testing.T) {
	client := &stripeClient{}
	customer, err := ProvisionCustomer(context.Background(), client, 7, "billing@example.com")
	if err != nil || customer != "cus_test" {
		t.Fatalf("customer=%q err=%v", customer, err)
	}
	if err := RecordMeteredReview(context.Background(), client, customer, "price_metered", "review-1", 12, time.Now()); err != nil {
		t.Fatal(err)
	}
	portal, err := OpenCustomerPortal(context.Background(), client, customer, "https://px1.example/settings/billing")
	if err != nil || client.usage != 12 || portal == "" {
		t.Fatalf("usage=%d portal=%q err=%v", client.usage, portal, err)
	}
}
