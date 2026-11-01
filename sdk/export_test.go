package sdk

import "github.com/racherom/modular-platform/sdk/tlsacme"

// WithIssuer lets a test run TLS mode with a fake issuer in place of the ACME client.
func WithIssuer(i tlsacme.Issuer) Option {
	return func(o *options) { o.issuer = i }
}

// WithAddrs moves the plain and TLS listen addresses off 8080 and 8443.
func WithAddrs(plain, tls string) Option {
	return func(o *options) { o.plainAddr, o.tlsAddr = plain, tls }
}

// Healthcheck runs the healthcheck subcommand with opts.
func Healthcheck(opts ...Option) int { return healthcheck(newOptions(opts)) }
