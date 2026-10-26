package billing

import (
	"context"
	"errors"
	"testing"
)

type memoryLedger struct {
	balance int64
	entries map[string]Transaction
}

func (m *memoryLedger) Append(_ context.Context, entry Transaction) (Transaction, bool, error) {
	if old, ok := m.entries[entry.IdempotencyKey]; ok {
		return old, false, nil
	}
	m.entries[entry.IdempotencyKey] = entry
	m.balance += entry.Credits
	return entry, true, nil
}
func (m *memoryLedger) Reserve(ctx context.Context, entry Transaction, required int64) (Transaction, bool, error) {
	if old, ok := m.entries[entry.IdempotencyKey]; ok {
		return old, false, nil
	}
	if m.balance < required {
		return Transaction{}, false, errors.New("insufficient credits")
	}
	return m.Append(ctx, entry)
}
func (m *memoryLedger) Balance(context.Context, int64) (int64, error) { return m.balance, nil }

func TestLedgerReserveSettleAndIdempotency(t *testing.T) {
	store := &memoryLedger{entries: map[string]Transaction{}}
	ledger := Ledger{Store: store}
	if _, _, err := ledger.Grant(context.Background(), 7, 10, "grant"); err != nil {
		t.Fatal(err)
	}
	reservation, created, err := ledger.Reserve(context.Background(), 7, "review-1", 6, "reserve")
	if err != nil || !created || store.balance != 4 {
		t.Fatalf("reserve: created=%v balance=%d err=%v", created, store.balance, err)
	}
	if _, created, err = ledger.Reserve(context.Background(), 7, "review-1", 6, "reserve"); err != nil || created || store.balance != 4 {
		t.Fatalf("duplicate reserve changed balance")
	}
	if _, _, err = ledger.Settle(context.Background(), reservation, 4, "settle"); err != nil || store.balance != 6 {
		t.Fatalf("settle balance=%d err=%v", store.balance, err)
	}
}

func TestLedgerRefundAndInsufficientBalance(t *testing.T) {
	store := &memoryLedger{balance: 3, entries: map[string]Transaction{}}
	ledger := Ledger{Store: store}
	if _, _, err := ledger.Reserve(context.Background(), 1, "review", 4, "reserve"); err == nil {
		t.Fatal("reserved unavailable credits")
	}
	reservation, _, _ := ledger.Reserve(context.Background(), 1, "review", 2, "reserve-ok")
	if _, _, err := ledger.Refund(context.Background(), reservation, "refund"); err != nil || store.balance != 3 {
		t.Fatalf("refund balance=%d err=%v", store.balance, err)
	}
}
