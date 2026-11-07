// Package apikeys issues and verifies per-organization API keys for the
// public API and the MCP server.
//
// A key looks like px1k_<installation>_<key id>_<secret>. The installation and
// key ID are not secret: they let the server set the tenant before any lookup,
// so key rows stay behind the same row-level security as every other hosted
// table. Only a SHA-256 digest of the 256-bit random secret is stored. A fast
// hash is appropriate here because the secret is random, not a password.
package apikeys

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"
)

const (
	Prefix         = "px1k_"
	MaxKeyLifetime = 365 * 24 * time.Hour
	MaxRotation    = 7 * 24 * time.Hour
	MaxNameBytes   = 100
)

type Scope string

const (
	ReviewsRead    Scope = "reviews:read"
	ReviewsTrigger Scope = "reviews:trigger"
	FindingsRead   Scope = "findings:read"
	FindingsWrite  Scope = "findings:write"
	CodebaseRead   Scope = "codebase:read"
	RulesRead      Scope = "rules:read"
	MemoriesRead   Scope = "memories:read"
)

var knownScopes = map[Scope]bool{ReviewsRead: true, ReviewsTrigger: true, FindingsRead: true, FindingsWrite: true, CodebaseRead: true, RulesRead: true, MemoriesRead: true}

type Key struct {
	ID             string
	InstallationID int64
	Name           string
	Scopes         []Scope
	SecretHash     [32]byte
	CreatedAt      time.Time
	ExpiresAt      time.Time
	RevokedAt      time.Time
	ReplacedBy     string
}

// Store reads and writes key rows for one tenant. Implementations set
// px1.installation_id from the installationID argument before querying.
type Store interface {
	Insert(ctx context.Context, key Key) error
	Get(ctx context.Context, installationID int64, id string) (Key, bool, error)
	// Supersede atomically marks old as replaced by next and moves its expiry
	// earlier, then inserts next. It fails if old is revoked or already replaced.
	Supersede(ctx context.Context, installationID int64, oldID string, oldExpiresAt time.Time, next Key) error
	Revoke(ctx context.Context, installationID int64, id string, at time.Time) error
}

type Principal struct {
	InstallationID int64
	KeyID          string
	Scopes         []Scope
}

func (p Principal) Has(scope Scope) bool { return slices.Contains(p.Scopes, scope) }

var (
	// ErrUnauthorized covers malformed, unknown, wrong, expired, and revoked
	// keys alike, so callers cannot probe which keys exist.
	ErrUnauthorized = errors.New("invalid API key")
	ErrForbidden    = errors.New("API key lacks the required scope")
)

var tokenPattern = regexp.MustCompile(`^px1k_([1-9][0-9]{0,18})_([0-9a-f]{16})_([A-Za-z0-9_-]{43})$`)

type Manager struct {
	Store Store
	Now   func() time.Time
}

// Issue creates a key and returns its plaintext token exactly once.
func (m Manager) Issue(ctx context.Context, installationID int64, name string, scopes []Scope, lifetime time.Duration) (string, Key, error) {
	name = strings.TrimSpace(name)
	if m.Store == nil || installationID <= 0 || name == "" || len(name) > MaxNameBytes {
		return "", Key{}, errors.New("store, tenant, and a 1-100 byte name are required")
	}
	if lifetime <= 0 || lifetime > MaxKeyLifetime {
		return "", Key{}, fmt.Errorf("key lifetime must be positive and at most %s", MaxKeyLifetime)
	}
	normalized, err := normalizeScopes(scopes)
	if err != nil {
		return "", Key{}, err
	}
	token, key, err := m.newKey(installationID, name, normalized, lifetime)
	if err != nil {
		return "", Key{}, err
	}
	if err := m.Store.Insert(ctx, key); err != nil {
		return "", Key{}, fmt.Errorf("store API key: %w", err)
	}
	return token, key, nil
}

// Rotate issues a replacement with the same name, scopes, and lifetime, and
// lets the old key keep working for at most overlap so clients can switch.
func (m Manager) Rotate(ctx context.Context, installationID int64, id string, overlap time.Duration) (string, Key, error) {
	if m.Store == nil || overlap < 0 || overlap > MaxRotation {
		return "", Key{}, fmt.Errorf("store and an overlap of at most %s are required", MaxRotation)
	}
	old, found, err := m.Store.Get(ctx, installationID, id)
	if err != nil {
		return "", Key{}, err
	}
	now := m.now()
	if !found || !old.RevokedAt.IsZero() || old.ReplacedBy != "" || !now.Before(old.ExpiresAt) {
		return "", Key{}, errors.New("only an active key can be rotated")
	}
	token, next, err := m.newKey(installationID, old.Name, old.Scopes, old.ExpiresAt.Sub(old.CreatedAt))
	if err != nil {
		return "", Key{}, err
	}
	oldExpiry := now.Add(overlap)
	if old.ExpiresAt.Before(oldExpiry) {
		oldExpiry = old.ExpiresAt
	}
	if err := m.Store.Supersede(ctx, installationID, old.ID, oldExpiry, next); err != nil {
		return "", Key{}, fmt.Errorf("rotate API key: %w", err)
	}
	return token, next, nil
}

func (m Manager) Revoke(ctx context.Context, installationID int64, id string) error {
	if m.Store == nil || installationID <= 0 {
		return errors.New("store and tenant are required")
	}
	return m.Store.Revoke(ctx, installationID, id, m.now())
}

// Authenticate verifies a presented token and, when required is non-empty,
// that the key holds that scope.
func (m Manager) Authenticate(ctx context.Context, token string, required Scope) (Principal, error) {
	match := tokenPattern.FindStringSubmatch(token)
	if m.Store == nil || match == nil {
		return Principal{}, ErrUnauthorized
	}
	installationID, err := strconv.ParseInt(match[1], 10, 64)
	if err != nil {
		return Principal{}, ErrUnauthorized
	}
	key, found, err := m.Store.Get(ctx, installationID, match[2])
	if err != nil {
		return Principal{}, fmt.Errorf("read API key: %w", err)
	}
	digest := sha256.Sum256([]byte(match[3]))
	if !found || key.InstallationID != installationID || subtle.ConstantTimeCompare(digest[:], key.SecretHash[:]) != 1 {
		return Principal{}, ErrUnauthorized
	}
	if !key.RevokedAt.IsZero() || !m.now().Before(key.ExpiresAt) {
		return Principal{}, ErrUnauthorized
	}
	principal := Principal{InstallationID: key.InstallationID, KeyID: key.ID, Scopes: append([]Scope{}, key.Scopes...)}
	if required != "" && !principal.Has(required) {
		return principal, ErrForbidden
	}
	return principal, nil
}

func (m Manager) newKey(installationID int64, name string, scopes []Scope, lifetime time.Duration) (string, Key, error) {
	idBytes, secretBytes := make([]byte, 8), make([]byte, 32)
	if _, err := rand.Read(idBytes); err != nil {
		return "", Key{}, err
	}
	if _, err := rand.Read(secretBytes); err != nil {
		return "", Key{}, err
	}
	id, secret := hex.EncodeToString(idBytes), base64.RawURLEncoding.EncodeToString(secretBytes)
	now := m.now()
	key := Key{ID: id, InstallationID: installationID, Name: name, Scopes: scopes, SecretHash: sha256.Sum256([]byte(secret)), CreatedAt: now, ExpiresAt: now.Add(lifetime)}
	return fmt.Sprintf("%s%d_%s_%s", Prefix, installationID, id, secret), key, nil
}

func (m Manager) now() time.Time {
	if m.Now != nil {
		return m.Now().UTC()
	}
	return time.Now().UTC()
}

func normalizeScopes(scopes []Scope) ([]Scope, error) {
	if len(scopes) == 0 {
		return nil, errors.New("at least one scope is required")
	}
	normalized := []Scope{}
	for _, scope := range scopes {
		if !knownScopes[scope] {
			return nil, fmt.Errorf("unknown scope %q", scope)
		}
		if !slices.Contains(normalized, scope) {
			normalized = append(normalized, scope)
		}
	}
	slices.Sort(normalized)
	return normalized, nil
}
