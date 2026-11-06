package api

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"regexp"
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

// basePathRe is the shape of a base path: one or more /segments, no trailing slash.
var basePathRe = regexp.MustCompile(`^(/[A-Za-z0-9._~-]+)+$`)

// readSettings reads the public and oidc bindings and every value, and checks the rules the
// capability fields and the declared bounds cannot: an origin without a path, a base path's
// shape, an admin email the identity provider must deliver, and session-max at least
// session-idle (AC-3). A broken rule is logged as a problem line and stops the start.
func readSettings(ctx context.Context, m *sdk.Module[config.Config, config.Uses]) (settings, error) {
	var s settings
	c := m.Config
	public, err := m.Uses.Public(ctx)
	if err != nil {
		return s, err
	}
	oidc, err := m.Uses.Oidc(ctx)
	if err != nil {
		return s, err
	}
	origin := public.Origin
	if origin.Path != "" && origin.Path != "/" || origin.RawQuery != "" {
		m.Log.ErrorContext(ctx, "config: uses.public.origin (MP_PUBLIC_ORIGIN): invalid: an origin is scheme, host and port, without a path")
		return s, fmt.Errorf("public origin %s: %w", origin.Redacted(), sdk.ErrStart)
	}
	s.PublicOrigin = strings.TrimSuffix(origin.String(), "/")
	if public.BasePath != nil {
		if !basePathRe.MatchString(*public.BasePath) {
			m.Log.ErrorContext(ctx, "config: uses.public.base_path (MP_BASE_PATH): no_match: a base path is /segments without a trailing slash, like /platform")
			return s, fmt.Errorf("base path: %w", sdk.ErrStart)
		}
		s.BasePath = *public.BasePath
	}
	if oidc.AdminEmail == nil {
		// the first verified sign in with this email binds the admin, so api cannot run without it
		m.Log.ErrorContext(ctx, "config: uses.oidc.admin_email (MP_ADMIN_EMAIL): missing: api needs the admin email; set MP_ADMIN_EMAIL")
		return s, fmt.Errorf("admin email: %w", sdk.ErrStart)
	}
	s.AdminEmail = *oidc.AdminEmail
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
	s.oidc = signin.OIDCConfig{
		Issuer: oidc.Issuer.String(), ClientID: oidc.ClientId, ClientSecret: oidc.ClientSecret.Reveal(),
		RedirectURL: s.PublicOrigin + s.BasePath + "/api/v1/auth/callback",
		CA:          oidc.Ca, // nil when the binding delivers none
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
