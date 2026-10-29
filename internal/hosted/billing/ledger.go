// Package billing contains tenant-scoped credit and payment contracts.
package billing

import (
	"context"
	"errors"
	"fmt"
	"time"
)

type TransactionKind string

const (
	CreditGrant   TransactionKind = "grant"
	CreditReserve TransactionKind = "reserve"
	CreditSettle  TransactionKind = "settle"
	CreditRefund  TransactionKind = "refund"
)

type Transaction struct {
	ID             string
	InstallationID int64
	ReviewID       string
	IdempotencyKey string
	Kind           TransactionKind
	Credits        int64 // Signed change to the available balance.
	CreatedAt      time.Time
}

type LedgerStore interface {
	Append(context.Context, Transaction) (Transaction, bool, error)
	// Reserve atomically appends the negative transaction only when the tenant
	// has at least required credits. Implementations must serialize this check.
	Reserve(context.Context, Transaction, int64) (Transaction, bool, error)
	Balance(context.Context, int64) (int64, error)
}

type Ledger struct{ Store LedgerStore }

func (l Ledger) Grant(ctx context.Context, installationID, credits int64, key string) (Transaction, bool, error) {
	if l.Store == nil || installationID <= 0 || credits <= 0 || key == "" {
		return Transaction{}, false, errors.New("ledger store, tenant, positive credits, and idempotency key are required")
	}
	return l.Store.Append(ctx, Transaction{InstallationID: installationID, IdempotencyKey: key, Kind: CreditGrant, Credits: credits})
}

func (l Ledger) Reserve(ctx context.Context, installationID int64, reviewID string, credits int64, key string) (Transaction, bool, error) {
	if l.Store == nil || installationID <= 0 || reviewID == "" || credits <= 0 || key == "" {
		return Transaction{}, false, errors.New("ledger store, tenant, review, positive credits, and idempotency key are required")
	}
	entry := Transaction{InstallationID: installationID, ReviewID: reviewID, IdempotencyKey: key, Kind: CreditReserve, Credits: -credits}
	return l.Store.Reserve(ctx, entry, credits)
}

func (l Ledger) Settle(ctx context.Context, reservation Transaction, actualCredits int64, key string) (Transaction, bool, error) {
	if l.Store == nil {
		return Transaction{}, false, errors.New("ledger store is required")
	}
	if err := validReservation(reservation); err != nil || actualCredits < 0 || key == "" {
		return Transaction{}, false, errors.New("valid reservation, non-negative actual credits, and idempotency key are required")
	}
	reserved := -reservation.Credits
	return l.Store.Append(ctx, Transaction{InstallationID: reservation.InstallationID, ReviewID: reservation.ReviewID, IdempotencyKey: key, Kind: CreditSettle, Credits: reserved - actualCredits})
}

func (l Ledger) Refund(ctx context.Context, reservation Transaction, key string) (Transaction, bool, error) {
	if l.Store == nil {
		return Transaction{}, false, errors.New("ledger store is required")
	}
	if err := validReservation(reservation); err != nil || key == "" {
		return Transaction{}, false, errors.New("valid reservation and idempotency key are required")
	}
	return l.Store.Append(ctx, Transaction{InstallationID: reservation.InstallationID, ReviewID: reservation.ReviewID, IdempotencyKey: key, Kind: CreditRefund, Credits: -reservation.Credits})
}

func validReservation(entry Transaction) error {
	if entry.InstallationID <= 0 || entry.ReviewID == "" || entry.Kind != CreditReserve || entry.Credits >= 0 {
		return fmt.Errorf("invalid reservation")
	}
	return nil
}
