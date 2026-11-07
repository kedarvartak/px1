// Package apikeystest provides an in-memory apikeys.Store for tests.
package apikeystest

import (
	"context"
	"errors"
	"sync"
	"time"

	"px1/internal/hosted/apikeys"
)

type Store struct {
	mu   sync.Mutex
	keys map[int64]map[string]apikeys.Key
}

func (s *Store) tenant(id int64) map[string]apikeys.Key {
	if s.keys == nil {
		s.keys = map[int64]map[string]apikeys.Key{}
	}
	if s.keys[id] == nil {
		s.keys[id] = map[string]apikeys.Key{}
	}
	return s.keys[id]
}

func (s *Store) Insert(_ context.Context, key apikeys.Key) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.tenant(key.InstallationID)[key.ID] = key
	return nil
}

func (s *Store) Get(_ context.Context, installationID int64, id string) (apikeys.Key, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	key, ok := s.tenant(installationID)[id]
	return key, ok, nil
}

func (s *Store) Supersede(_ context.Context, installationID int64, oldID string, oldExpiresAt time.Time, next apikeys.Key) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	old, ok := s.tenant(installationID)[oldID]
	if !ok || !old.RevokedAt.IsZero() || old.ReplacedBy != "" {
		return errors.New("key is not active")
	}
	old.ReplacedBy, old.ExpiresAt = next.ID, oldExpiresAt
	s.tenant(installationID)[oldID] = old
	s.tenant(installationID)[next.ID] = next
	return nil
}

func (s *Store) Revoke(_ context.Context, installationID int64, id string, at time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	key, ok := s.tenant(installationID)[id]
	if !ok {
		return errors.New("key not found")
	}
	key.RevokedAt = at
	s.tenant(installationID)[id] = key
	return nil
}
