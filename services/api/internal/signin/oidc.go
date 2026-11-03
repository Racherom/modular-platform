package signin

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"sync/atomic"
	"time"

	"github.com/coreos/go-oidc/v3/oidc"
	"golang.org/x/oauth2"
)

// errNoProvider is returned by a provider whose discovery has not succeeded.
var errNoProvider = errors.New("signin: the identity provider has not answered discovery yet")

// OIDCConfig is the client's registration at the provider.
type OIDCConfig struct {
	Issuer       string
	ClientID     string
	ClientSecret string
	RedirectURL  string // <origin><base>/api/v1/auth/callback
	CA           []byte // PEM roots to trust the provider with; nil uses the system roots
}

// OIDC is the Provider over OpenID Connect discovery, the code flow with PKCE and userinfo.
type OIDC struct {
	cfg    OIDCConfig
	client *http.Client
	log    *slog.Logger
	state  atomic.Pointer[discovered]
}

type discovered struct {
	provider   *oidc.Provider
	oauth      oauth2.Config
	verifier   *oidc.IDTokenVerifier
	endSession string
}

var _ Provider = (*OIDC)(nil)

// NewOIDC returns the provider and starts discovery, retried with backoff until it succeeds or
// ctx ends; Ready reports false until then.
func NewOIDC(ctx context.Context, cfg OIDCConfig, log *slog.Logger) (*OIDC, error) {
	tr := http.DefaultTransport.(*http.Transport).Clone()
	if len(cfg.CA) > 0 {
		roots := x509.NewCertPool()
		if !roots.AppendCertsFromPEM(cfg.CA) {
			return nil, errors.New("signin: the OIDC CA holds no PEM certificate")
		}
		tr.TLSClientConfig = &tls.Config{RootCAs: roots, MinVersion: tls.VersionTLS12}
	}
	o := &OIDC{cfg: cfg, client: &http.Client{Transport: tr, Timeout: 10 * time.Second}, log: log}
	go o.discover(ctx)
	return o, nil
}

func (o *OIDC) discover(ctx context.Context) {
	wait := time.Second
	for {
		err := o.discoverOnce(ctx)
		if err == nil {
			o.log.InfoContext(ctx, "identity provider discovered", slog.String("issuer", o.cfg.Issuer))
			return
		}
		o.log.WarnContext(ctx, "identity provider discovery failed", slog.String("issuer", o.cfg.Issuer),
			slog.Any("error", err), slog.Duration("retry_in", wait))
		select {
		case <-ctx.Done():
			return
		case <-time.After(wait):
		}
		wait = min(wait*2, 30*time.Second)
	}
}

func (o *OIDC) discoverOnce(ctx context.Context) error {
	p, err := oidc.NewProvider(oidc.ClientContext(ctx, o.client), o.cfg.Issuer)
	if err != nil {
		return err
	}
	var extra struct {
		EndSession string `json:"end_session_endpoint"`
	}
	if err := p.Claims(&extra); err != nil {
		return err
	}
	o.state.Store(&discovered{
		provider: p,
		oauth: oauth2.Config{
			ClientID: o.cfg.ClientID, ClientSecret: o.cfg.ClientSecret, RedirectURL: o.cfg.RedirectURL,
			Endpoint: p.Endpoint(), Scopes: []string{oidc.ScopeOpenID, "profile", "email", "groups"},
		},
		verifier:   p.Verifier(&oidc.Config{ClientID: o.cfg.ClientID}),
		endSession: extra.EndSession,
	})
	return nil
}

// Ready reports whether discovery has succeeded.
func (o *OIDC) Ready() bool { return o.state.Load() != nil }

// AuthCodeURL is the provider's authorization URL with PKCE (S256) and the nonce.
func (o *OIDC) AuthCodeURL(state, nonce, verifier string, relogin bool) string {
	d := o.state.Load()
	if d == nil {
		return ""
	}
	opts := []oauth2.AuthCodeOption{oidc.Nonce(nonce), oauth2.S256ChallengeOption(verifier)}
	if relogin {
		opts = append(opts, oauth2.SetAuthURLParam("prompt", "login"))
	}
	return d.oauth.AuthCodeURL(state, opts...)
}

// Exchange trades the code for tokens, verifies the ID token and its nonce, and reads email and
// email_verified from userinfo (Authelia puts them there), else from the ID token.
func (o *OIDC) Exchange(ctx context.Context, code, verifier, nonce string) (Claims, string, error) {
	d := o.state.Load()
	if d == nil {
		return Claims{}, "", errNoProvider
	}
	ctx = oidc.ClientContext(ctx, o.client)
	tok, err := d.oauth.Exchange(ctx, code, oauth2.VerifierOption(verifier))
	if err != nil {
		return Claims{}, "", fmt.Errorf("token exchange: %w", err)
	}
	raw, ok := tok.Extra("id_token").(string)
	if !ok {
		return Claims{}, "", errors.New("the token answer holds no id_token")
	}
	idt, err := d.verifier.Verify(ctx, raw)
	if err != nil {
		return Claims{}, "", fmt.Errorf("id token: %w", err)
	}
	if idt.Nonce != nonce {
		return Claims{}, "", errors.New("id token: the nonce does not match")
	}
	var fromToken struct {
		Email         string `json:"email"`
		EmailVerified bool   `json:"email_verified"`
	}
	if err := idt.Claims(&fromToken); err != nil {
		return Claims{}, "", fmt.Errorf("id token claims: %w", err)
	}
	c := Claims{Identity: Identity{Issuer: idt.Issuer, Subject: idt.Subject}, Email: fromToken.Email, EmailVerified: fromToken.EmailVerified}
	info, err := d.provider.UserInfo(ctx, oauth2.StaticTokenSource(tok))
	switch {
	case err != nil:
		o.log.WarnContext(ctx, "userinfo failed; using the id token's claims", slog.Any("error", err))
	case info.Subject != idt.Subject:
		return Claims{}, "", errors.New("userinfo names another subject than the id token")
	case info.Email != "":
		c.Email, c.EmailVerified = info.Email, info.EmailVerified
	}
	return c, raw, nil
}

// EndSessionURL is the provider's end_session_endpoint, or empty.
func (o *OIDC) EndSessionURL() string {
	if d := o.state.Load(); d != nil {
		return d.endSession
	}
	return ""
}
