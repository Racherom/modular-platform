// Package setupsession mints and verifies the setup session cookie value (spec 0033, 0034).
//
// A value looks like v1.<expiry>.<mac>: expiry is Unix seconds, mac is the base64url (no padding)
// HMAC SHA 256 of "mp-setup|v1|<expiry>" under the session key. A value is valid while now is
// before its expiry and the MAC matches the current key. The package is a small shared kernel
// helper, like libs/contract: key bytes, a clock and a value go in, a result comes out, and it
// never reads a file or the environment.
package setupsession

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"
)

// Lifetime is how long a session lasts from the redemption of the code.
const Lifetime = 24 * time.Hour

const (
	version    = "v1"
	macContext = "mp-setup|" + version + "|"
	hostName   = "__Host-mp_setup"
	secureName = "__Secure-mp_setup"
)

// ErrInvalid is wrapped by every refusal: the value is no session.
var ErrInvalid = errors.New("not a setup session")

// ErrExpired is wrapped, with ErrInvalid, by the refusal of a well formed value past its expiry.
var ErrExpired = errors.New("the setup session has expired")

// Mint returns the value of a session that starts at now and lasts Lifetime, and its expiry.
func Mint(key []byte, now time.Time) (value string, expiry time.Time) {
	expiry = now.Add(Lifetime).Truncate(time.Second)
	return MintUntil(key, expiry), expiry
}

// MintUntil returns the value of a session that ends at expiry (whole seconds). The probe of
// phase 1 uses it to hand the session on to the platform's host with the same end.
func MintUntil(key []byte, expiry time.Time) string {
	unix := strconv.FormatInt(expiry.Unix(), 10)
	return version + "." + unix + "." + mac(key, unix)
}

// Verify checks a cookie value against key at now and returns its expiry. Every refusal wraps
// ErrInvalid; a value that is well formed and signed but past its expiry also wraps ErrExpired.
func Verify(key []byte, value string, now time.Time) (time.Time, error) {
	if len(key) == 0 {
		return time.Time{}, fmt.Errorf("%w: no session key", ErrInvalid)
	}
	parts := strings.Split(value, ".")
	if len(parts) != 3 || parts[0] != version {
		return time.Time{}, fmt.Errorf("%w: malformed value", ErrInvalid)
	}
	unix, err := strconv.ParseInt(parts[1], 10, 64)
	if err != nil || strconv.FormatInt(unix, 10) != parts[1] {
		return time.Time{}, fmt.Errorf("%w: malformed expiry", ErrInvalid)
	}
	got, err := base64.RawURLEncoding.DecodeString(parts[2])
	if err != nil {
		return time.Time{}, fmt.Errorf("%w: malformed mac", ErrInvalid)
	}
	want, _ := base64.RawURLEncoding.DecodeString(mac(key, parts[1]))
	if !hmac.Equal(got, want) {
		return time.Time{}, fmt.Errorf("%w: wrong mac", ErrInvalid)
	}
	expiry := time.Unix(unix, 0)
	if !now.Before(expiry) {
		return time.Time{}, fmt.Errorf("%w: %w", ErrInvalid, ErrExpired)
	}
	return expiry, nil
}

func mac(key []byte, expiry string) string {
	h := hmac.New(sha256.New, key)
	h.Write([]byte(macContext + expiry))
	return base64.RawURLEncoding.EncodeToString(h.Sum(nil))
}

// CookieName is the name of the setup cookie for a base path (spec 0001's rule): __Host-mp_setup
// at the site root, __Secure-mp_setup under a base path.
func CookieName(basePath string) string {
	if basePath == "" {
		return hostName
	}
	return secureName
}

// CookiePath is the Path of the setup cookie: / at the site root, else <basePath>/.
func CookiePath(basePath string) string {
	if basePath == "" {
		return "/"
	}
	return basePath + "/"
}

// HandoffLifetime is how long a handoff token lasts (spec 0037).
const HandoffLifetime = 60 * time.Second

const (
	handoffVersion = "h1"
	handoffContext = "mp-handoff|" + handoffVersion + "|"
	gateCookieName = "__Host-mp_gate"
)

// Handoff is what a verified handoff token carries.
type Handoff struct {
	Expiry        time.Time // the token's own end
	SessionExpiry time.Time // the end of the setup session it hands on
	Nonce         string    // identifies the token, so the caller can accept it once
}

// MintHandoff returns a token that moves the setup session with the given end to host (the auth
// host, no scheme or path). The token is h1.<expiry>.<session expiry>.<nonce>.<mac>; the mac covers
// the host, so a token for one host is refused on another. The nonce must be random and unique.
func MintHandoff(key []byte, host string, sessionExpiry, now time.Time, nonce string) string {
	exp := strconv.FormatInt(now.Add(HandoffLifetime).Unix(), 10)
	sess := strconv.FormatInt(sessionExpiry.Unix(), 10)
	return strings.Join([]string{handoffVersion, exp, sess, nonce, handoffMAC(key, host, exp, sess, nonce)}, ".")
}

// VerifyHandoff checks a token against key, host and now. Every refusal wraps ErrInvalid; a signed
// token past its expiry also wraps ErrExpired. It does not know about reuse: the caller keeps the
// nonces it has seen until Expiry.
func VerifyHandoff(key []byte, host, token string, now time.Time) (Handoff, error) {
	if len(key) == 0 {
		return Handoff{}, fmt.Errorf("%w: no session key", ErrInvalid)
	}
	parts := strings.Split(token, ".")
	if len(parts) != 5 || parts[0] != handoffVersion || parts[3] == "" {
		return Handoff{}, fmt.Errorf("%w: malformed handoff", ErrInvalid)
	}
	got, err := base64.RawURLEncoding.DecodeString(parts[4])
	if err != nil {
		return Handoff{}, fmt.Errorf("%w: malformed mac", ErrInvalid)
	}
	want, _ := base64.RawURLEncoding.DecodeString(handoffMAC(key, host, parts[1], parts[2], parts[3]))
	if !hmac.Equal(got, want) {
		return Handoff{}, fmt.Errorf("%w: wrong mac", ErrInvalid)
	}
	exp, err1 := strconv.ParseInt(parts[1], 10, 64)
	sess, err2 := strconv.ParseInt(parts[2], 10, 64)
	if err1 != nil || err2 != nil {
		return Handoff{}, fmt.Errorf("%w: malformed time", ErrInvalid)
	}
	h := Handoff{Expiry: time.Unix(exp, 0), SessionExpiry: time.Unix(sess, 0), Nonce: parts[3]}
	if !now.Before(h.Expiry) || !now.Before(h.SessionExpiry) {
		return Handoff{}, fmt.Errorf("%w: %w", ErrInvalid, ErrExpired)
	}
	return h, nil
}

func handoffMAC(key []byte, host, exp, sess, nonce string) string {
	h := hmac.New(sha256.New, key)
	// the fields hold no "|", and the host is length prefixed, so no two inputs share a message
	h.Write([]byte(handoffContext + strconv.Itoa(len(host)) + ":" + host + "|" + exp + "|" + sess + "|" + nonce))
	return base64.RawURLEncoding.EncodeToString(h.Sum(nil))
}

// GateCookieName is the name of the auth host cookie the gate checks: host only, at the root.
// Its value is a setup session value (MintUntil), so it ends with the session.
func GateCookieName() string { return gateCookieName }
