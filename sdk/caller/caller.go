// Package caller mints and checks the MP-Caller token, which says which user a call between
// modules is made for (spec 0005, "MP-Caller token"). Only api holds the signing key and mints;
// every other module checks: an EdDSA compact JWS whose kid is the RFC 7638 thumbprint of the
// public key, for one audience (the receiving module's host name), living at most 5 minutes. A
// token counts only on a call that passed binding token auth and whose iss is that calling module.
package caller

import (
	"context"
	"crypto"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/x509"
	"encoding/base64"
	"encoding/pem"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"sync"
	"time"

	"github.com/go-jose/go-jose/v4"
	"github.com/go-jose/go-jose/v4/jwt"

	"github.com/racherom/modular-platform/sdk/auth"
	"github.com/racherom/modular-platform/sdk/httpkit"
)

// Header carries the token on a call between modules.
const Header = "MP-Caller"

const (
	// lifetime is how long a minted token lives, and the longest a checked one may.
	lifetime = 5 * time.Minute
	// leeway is the clock skew allowed on exp and iat.
	leeway = 30 * time.Second
)

// ErrInvalidToken reports a token that is refused; the wrapped message says why, never the token.
var ErrInvalidToken = errors.New("invalid_token")

// Caller is the user a call is made for, and the module that minted the token (its MP_INSTANCE).
type Caller struct {
	Issuer string
	User   string
	Groups []string
}

type callerKey struct{}

// With returns ctx carrying c.
func With(ctx context.Context, c Caller) context.Context {
	return context.WithValue(ctx, callerKey{}, c)
}

// From returns the caller in ctx.
func From(ctx context.Context) (Caller, bool) {
	c, ok := ctx.Value(callerKey{}).(Caller)
	return c, ok
}

// claims are the token's claims beyond the registered ones.
type claims struct {
	Groups []string `json:"groups"`
}

// Mint returns a token for c (c.Issuer becomes iss) and the audience aud, issued at now, signed
// with key, which must be an Ed25519 private key.
func Mint(key crypto.PrivateKey, c Caller, aud string, now time.Time) (string, error) {
	k, ok := key.(ed25519.PrivateKey)
	if !ok {
		return "", fmt.Errorf("caller: the signing key is %T, not Ed25519", key)
	}
	kid, err := thumbprint(k.Public().(ed25519.PublicKey))
	if err != nil {
		return "", err
	}
	sig, err := jose.NewSigner(jose.SigningKey{Algorithm: jose.EdDSA, Key: jose.JSONWebKey{Key: k, KeyID: kid}},
		(&jose.SignerOptions{}).WithType("JWT"))
	if err != nil {
		return "", fmt.Errorf("caller: signer: %w", err)
	}
	jti := make([]byte, 16)
	if _, err := rand.Read(jti); err != nil {
		return "", fmt.Errorf("caller: jti: %w", err)
	}
	groups := c.Groups
	if groups == nil {
		groups = []string{}
	}
	token, err := jwt.Signed(sig).Claims(jwt.Claims{
		Issuer:   c.Issuer,
		Subject:  c.User,
		Audience: jwt.Audience{aud},
		IssuedAt: jwt.NewNumericDate(now),
		Expiry:   jwt.NewNumericDate(now.Add(lifetime)),
		ID:       base64.RawURLEncoding.EncodeToString(jti),
	}).Claims(claims{Groups: groups}).Serialize()
	if err != nil {
		return "", fmt.Errorf("caller: sign: %w", err)
	}
	return token, nil
}

// thumbprint is the RFC 7638 SHA-256 thumbprint of pub, base64url without padding: the kid.
func thumbprint(pub ed25519.PublicKey) (string, error) {
	b, err := (&jose.JSONWebKey{Key: pub}).Thumbprint(crypto.SHA256)
	if err != nil {
		return "", fmt.Errorf("caller: thumbprint: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

// Keys are the public keys allowed to sign tokens, by kid.
type Keys struct {
	byKID map[string]ed25519.PublicKey
}

// Len is the number of keys.
func (k Keys) Len() int { return len(k.byKID) }

// ParseKeys reads the PEM blocks of an MP_CALLER_KEY_FILE: one or more Ed25519 public keys
// (PUBLIC KEY, PKIX). Any other block, or none, is an error.
func ParseKeys(data []byte) (Keys, error) {
	keys := Keys{byKID: map[string]ed25519.PublicKey{}}
	for {
		var block *pem.Block
		block, data = pem.Decode(data)
		if block == nil {
			break
		}
		if block.Type != "PUBLIC KEY" {
			return Keys{}, fmt.Errorf("a %s block is not an Ed25519 public key", block.Type)
		}
		pub, err := x509.ParsePKIXPublicKey(block.Bytes)
		if err != nil {
			return Keys{}, fmt.Errorf("a public key cannot be parsed: %w", err)
		}
		k, ok := pub.(ed25519.PublicKey)
		if !ok {
			return Keys{}, fmt.Errorf("a %T public key is not Ed25519", pub)
		}
		kid, err := thumbprint(k)
		if err != nil {
			return Keys{}, err
		}
		keys.byKID[kid] = k
	}
	if len(keys.byKID) == 0 {
		return Keys{}, errors.New("it holds no PEM public key")
	}
	return keys, nil
}

// ParseSigningKey reads an MP_CALLER_SIGNING_KEY_FILE: one Ed25519 private key (PRIVATE KEY,
// PKCS 8).
func ParseSigningKey(data []byte) (ed25519.PrivateKey, error) {
	block, _ := pem.Decode(data)
	if block == nil || block.Type != "PRIVATE KEY" {
		return nil, errors.New("it holds no PEM PRIVATE KEY block")
	}
	key, err := x509.ParsePKCS8PrivateKey(block.Bytes)
	if err != nil {
		return nil, errors.New("its private key cannot be parsed as PKCS 8")
	}
	k, ok := key.(ed25519.PrivateKey)
	if !ok {
		return nil, fmt.Errorf("its %T private key is not Ed25519", key)
	}
	return k, nil
}

// Verify checks token for the module whose host name is self at now and returns its caller: only
// EdDSA, a kid among keys, a valid signature, self as the one audience, exp and iat present with
// 30 seconds leeway each (iat not in the future), a lifetime of at most 5 minutes, and a user.
// Every refusal wraps ErrInvalidToken.
func Verify(keys Keys, token, self string, now time.Time) (Caller, error) {
	tok, err := jwt.ParseSigned(token, []jose.SignatureAlgorithm{jose.EdDSA})
	if err != nil {
		return Caller{}, refused("not an EdDSA compact JWS: %v", err)
	}
	kid := tok.Headers[0].KeyID
	pub, ok := keys.byKID[kid]
	if !ok {
		return Caller{}, refused("unknown kid %q", kid)
	}
	var (
		std jwt.Claims
		own claims
	)
	if err := tok.Claims(pub, &std, &own); err != nil {
		return Caller{}, refused("bad signature: %v", err)
	}
	switch {
	case len(std.Audience) != 1 || std.Audience[0] != self:
		return Caller{}, refused("aud %v is not %s", []string(std.Audience), self)
	case std.Expiry == nil || std.IssuedAt == nil:
		return Caller{}, refused("exp and iat are required")
	case std.Subject == "":
		return Caller{}, refused("sub is empty")
	}
	if err := std.ValidateWithLeeway(jwt.Expected{Time: now}, leeway); err != nil {
		return Caller{}, refused("%v", err)
	}
	if life := std.Expiry.Time().Sub(std.IssuedAt.Time()); life > lifetime {
		return Caller{}, refused("it lives %s, longer than %s", life, lifetime)
	}
	return Caller{Issuer: std.Issuer, User: std.Subject, Groups: own.Groups}, nil
}

func refused(format string, args ...any) error {
	return fmt.Errorf("%w: %s", ErrInvalidToken, fmt.Sprintf(format, args...))
}

// Middleware checks the MP-Caller header for the module whose host name is self and puts the
// caller into the request context. A request without the header passes with no caller. A header
// is refused with 401 (invalid_token) when it appears twice, when the request passed no binding
// token auth, when Verify refuses it, or when its iss is not the module the binding token proved.
// With no keys (MP_CALLER_KEY_FILE unset) the header is ignored, and logged once.
func Middleware(keys Keys, self string, log *slog.Logger, next http.Handler) http.Handler {
	var ignored sync.Once
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		values := r.Header.Values(Header)
		if len(values) == 0 {
			next.ServeHTTP(w, r)
			return
		}
		ctx := r.Context()
		if keys.Len() == 0 {
			ignored.Do(func() {
				log.WarnContext(ctx, "caller token ignored: MP_CALLER_KEY_FILE is unset")
			})
			next.ServeHTTP(w, r)
			return
		}
		module, authed := auth.CallerModule(ctx)
		var (
			c   Caller
			err error
		)
		switch {
		case len(values) > 1:
			err = refused("the %s header appears %d times", Header, len(values))
		case !authed:
			err = refused("the call passed no binding token auth")
		default:
			c, err = Verify(keys, values[0], self, time.Now())
			if err == nil && c.Issuer != module {
				err = refused("iss %q is not the calling module %q", c.Issuer, module)
			}
		}
		if err != nil {
			log.WarnContext(ctx, "caller token refused", slog.String("reason", err.Error()))
			httpkit.WriteProblem(w, http.StatusUnauthorized, "invalid_token", "This module needs a valid MP-Caller token.")
			return
		}
		next.ServeHTTP(w, r.WithContext(With(ctx, c)))
	})
}

// Signer mints tokens as one module (its MP_INSTANCE is iss). It hides its key from fmt and slog.
type Signer struct {
	key    ed25519.PrivateKey
	issuer string
}

// NewSigner returns a signer for the module instance issuer.
func NewSigner(key ed25519.PrivateKey, issuer string) *Signer {
	return &Signer{key: key, issuer: issuer}
}

// Mint returns a token for the user and groups of c, for the audience aud, issued at now.
func (s *Signer) Mint(c Caller, aud string, now time.Time) (string, error) {
	c.Issuer = s.issuer
	return Mint(s.key, c, aud, now)
}

// Format shows the issuer only, never the key.
func (s *Signer) Format(f fmt.State, _ rune) { _, _ = fmt.Fprintf(f, "caller.Signer{%s}", s.issuer) }

// LogValue shows the issuer only, never the key.
func (s *Signer) LogValue() slog.Value { return slog.StringValue(s.issuer) }
