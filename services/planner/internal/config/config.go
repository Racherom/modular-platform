// Package config reads the planner's settings from the environment.
package config

import (
	"fmt"
	"os"

	"github.com/racherom/modular-platform/libs/platform"
)

// Mode is where the planner runs.
type Mode string

const (
	// ModePublic serves the online tool: no auth, a per client rate limit.
	ModePublic Mode = "public"
	// ModeInternal serves inside the managed platform: every call needs the bearer token.
	ModeInternal Mode = "internal"
)

// Config holds every planner setting.
type Config struct {
	Mode      Mode
	Rate      int    // MP_PLANNER_RATE: requests per minute per client IP (public mode)
	ProxyHops int    // MP_PROXY_HOPS: proxies in front of the planner that append X-Forwarded-For
	Token     string // read from MP_PLANNER_TOKEN_FILE (internal mode)
}

// FromEnv reads the settings. There is no default mode, so the planner never starts open by
// accident, and internal mode refuses to start without its token file.
func FromEnv() (Config, error) {
	var cfg Config
	switch mode := Mode(os.Getenv("MP_PLANNER_MODE")); mode {
	case ModePublic:
		cfg.Mode = mode
	case ModeInternal:
		token, err := platform.ReadSecretFile("MP_PLANNER_TOKEN_FILE")
		if err != nil {
			return Config{}, fmt.Errorf("internal mode needs a token: %w", err)
		}
		cfg.Mode, cfg.Token = mode, token
	default:
		return Config{}, fmt.Errorf("MP_PLANNER_MODE must be public or internal, got %q", mode)
	}

	var err error
	if cfg.Rate, err = platform.EnvInt("MP_PLANNER_RATE", 60, 1); err != nil {
		return Config{}, err
	}
	if cfg.ProxyHops, err = platform.EnvInt("MP_PROXY_HOPS", 1, 0); err != nil {
		return Config{}, err
	}
	return cfg, nil
}
