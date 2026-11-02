package sdk

import (
	"time"

	"github.com/racherom/modular-platform/sdk/tlsacme"
)

// WithIssuer lets a test run TLS mode with a fake issuer in place of the ACME client.
func WithIssuer(i tlsacme.Issuer) Option {
	return func(o *options) { o.issuer = i }
}

// WithAddrs moves the plain and TLS listen addresses off 8080 and 8443.
func WithAddrs(plain, tls string) Option {
	return func(o *options) { o.plainAddr, o.tlsAddr = plain, tls }
}

// WithSourceTimes shortens the source limits: the start source deadline, the wait for the first
// pushed snapshot, and the caller source budget and cache lifetime.
func WithSourceTimes(deadline, wait, budget, ttl time.Duration) Option {
	return func(o *options) {
		o.sourceDeadline, o.pushWait, o.callerBudget, o.callerTTL = deadline, wait, budget, ttl
	}
}

// Healthcheck runs the healthcheck subcommand with opts.
func Healthcheck(opts ...Option) int { return healthcheck(newOptions(opts)) }
