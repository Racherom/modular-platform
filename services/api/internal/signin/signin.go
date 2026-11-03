// Package signin signs the one admin in through OIDC Authorization Code with PKCE and keeps
// their server side sessions (spec 0006). The first verified sign in with the admin email binds
// that issuer plus subject as the admin; after that only they get a session. Tokens from the
// provider never reach the browser: it holds only the session cookie and the login cookies.
package signin

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"time"
)

// Reasons a sign in is refused, shown on the signed out screen.
const (
	ReasonNotAdmin        = "not_admin"
	ReasonEmailUnverified = "email_unverified"
	ReasonStateMismatch   = "state_mismatch"
	ReasonProviderError   = "provider_error"
)

// ErrUnauthenticated reports a request without a valid session.
var ErrUnauthenticated = errors.New("signin: no valid session")

// Identity is who signed in at which provider; the admin binding holds one.
type Identity struct {
	Issuer, Subject string
}

// Session is a server side session. Only the hash of its token is stored.
type Session struct {
	TokenHash  []byte
	Identity   Identity
	Email      string
	IDToken    string // kept only when the provider lists an end_session_endpoint
	CreatedAt  time.Time
	LastSeenAt time.Time
	ExpiresAt  time.Time
}

// IdleExpiresAt is when the session ends without another request.
func (s Session) IdleExpiresAt(idle time.Duration) time.Time { return s.LastSeenAt.Add(idle) }

// Store keeps sessions and the admin binding.
type Store interface {
	CreateSession(ctx context.Context, s Session) error
	// Session returns the session with the token hash; false when there is none.
	Session(ctx context.Context, tokenHash []byte) (Session, bool, error)
	// TouchSession records activity, at most once a minute per session.
	TouchSession(ctx context.Context, tokenHash []byte) error
	DeleteSession(ctx context.Context, tokenHash []byte) error
	// DeleteStaleSessions deletes every expired or idle session and returns how many.
	DeleteStaleSessions(ctx context.Context, idle time.Duration) (int64, error)
	// AdminBinding returns the bound admin; false when none is bound yet.
	AdminBinding(ctx context.Context) (Identity, bool, error)
	// BindAdmin binds id with email when no admin is bound, atomically, and returns whoever is
	// bound afterwards: id, or the admin another sign in bound first.
	BindAdmin(ctx context.Context, id Identity, email string) (Identity, error)
}

// newToken is a 32 byte session token as base64url, and the SHA-256 the store keeps.
func newToken() (string, []byte) {
	b := make([]byte, 32)
	_, _ = rand.Read(b) // crypto/rand never fails on supported platforms
	sum := sha256.Sum256(b)
	return base64.RawURLEncoding.EncodeToString(b), sum[:]
}

// tokenHash decodes a session cookie value and hashes it; false for a malformed value.
func tokenHash(cookie string) ([]byte, bool) {
	b, err := base64.RawURLEncoding.DecodeString(cookie)
	if err != nil || len(b) != 32 {
		return nil, false
	}
	sum := sha256.Sum256(b)
	return sum[:], true
}
