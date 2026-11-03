package api

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"

	"github.com/racherom/modular-platform/sdk"
	"github.com/racherom/modular-platform/services/api/internal/clients"
	"github.com/racherom/modular-platform/services/api/internal/config"
	"github.com/racherom/modular-platform/services/api/internal/deploy"
	"github.com/racherom/modular-platform/services/api/internal/signin"
	"github.com/racherom/modular-platform/services/api/internal/store"
)

// settings are api's checked config values.
type settings struct {
	signin.Config
	oidc signin.OIDCConfig
}

// readSettings reads every value and checks the rule the declared bounds cannot: session-max
// at least session-idle (AC-3). A broken rule is logged as a problem line and stops the start.
func readSettings(ctx context.Context, m *sdk.Module[config.Config, config.Uses]) (settings, error) {
	var s settings
	c := m.Config
	issuer, err := c.OidcIssuer(ctx)
	if err != nil {
		return s, err
	}
	origin, err := c.PublicOrigin(ctx)
	if err != nil {
		return s, err
	}
	if origin.Path != "" && origin.Path != "/" || origin.RawQuery != "" {
		m.Log.ErrorContext(ctx, "MP_PUBLIC_ORIGIN: invalid: an origin is scheme, host and port, without a path")
		return s, fmt.Errorf("public origin %s: %w", origin.Redacted(), sdk.ErrStart)
	}
	s.PublicOrigin = strings.TrimSuffix(origin.String(), "/")
	if s.BasePath, _, err = c.BasePath(ctx); err != nil {
		return s, err
	}
	if s.AdminEmail, err = c.AdminEmail(ctx); err != nil {
		return s, err
	}
	if s.Idle, err = c.SessionIdle(ctx); err != nil {
		return s, err
	}
	if s.Max, err = c.SessionMax(ctx); err != nil {
		return s, err
	}
	if err := signin.CheckLifetimes(s.Idle, s.Max); err != nil {
		m.Log.ErrorContext(ctx, "MP_SESSION_MAX: invalid: "+err.Error())
		return s, fmt.Errorf("%w: %w", err, sdk.ErrStart)
	}
	id, err := c.OidcClientId(ctx)
	if err != nil {
		return s, err
	}
	secret, err := c.OidcClientSecret(ctx)
	if err != nil {
		return s, err
	}
	ca, _, err := c.OidcCa(ctx)
	if err != nil {
		return s, err
	}
	s.oidc = signin.OIDCConfig{
		Issuer: issuer.String(), ClientID: id, ClientSecret: secret.Reveal(),
		RedirectURL: s.PublicOrigin + s.BasePath + "/api/v1/auth/callback",
	}
	if ca != "" {
		s.oidc.CA = []byte(ca)
	}
	return s, nil
}

// Setup migrates the database, starts OIDC discovery, resumes polling every running operation
// and serves api's contract; sdk.Run puts health and the access log in front. /readyz waits
// for the provider's discovery beside the bindings.
func Setup(ctx context.Context, m *sdk.Module[config.Config, config.Uses]) (http.Handler, error) {
	s, err := readSettings(ctx, m)
	if err != nil {
		return nil, err
	}
	pool, err := m.Uses.Postgresql(ctx)
	if err != nil {
		return nil, err
	}
	if err := store.Migrate(ctx, pool); err != nil {
		return nil, err
	}
	st := store.New(pool)
	plannerBinding, err := m.Uses.Planner(ctx)
	if err != nil {
		return nil, err
	}
	planner, err := clients.NewPlanner(plannerBinding)
	if err != nil {
		return nil, err
	}
	targetBinding, err := m.Uses.DeployTarget(ctx)
	if err != nil {
		return nil, err
	}
	target, err := clients.NewTarget(targetBinding)
	if err != nil {
		return nil, err
	}
	idp, err := signin.NewOIDC(ctx, s.oidc, m.Log)
	if err != nil {
		return nil, err
	}
	m.AddReady(idp.Ready)

	sessions := signin.New(s.Config, st, idp, m.Log)
	go sessions.Sweep(ctx)
	hub := deploy.NewHub()
	deployments := deploy.NewService(ctx, st, planner, target, hub, m.Log)
	if err := deployments.Resume(ctx); err != nil {
		return nil, errors.Join(errors.New("resuming running operations"), err)
	}
	m.Log.InfoContext(ctx, "api starting", slog.String("public_origin", s.PublicOrigin), slog.String("base_path", s.BasePath))
	return NewHandler(Options{
		Signin: sessions, Deploy: deployments, Hub: hub, Planner: planner,
		PublicOrigin: s.PublicOrigin, BasePath: s.BasePath, Idle: s.Idle, Log: m.Log,
	}), nil
}
