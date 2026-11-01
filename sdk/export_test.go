package sdk

import "github.com/racherom/modular-platform/sdk/tlsacme"

// WithCertificates lets a test run TLS mode with a fake certificate getter in place of autocert.
func WithCertificates(g tlsacme.Getter) Option {
	return func(o *options) { o.certificates = g }
}

// WithAddrs moves the plain and TLS listen addresses off 8080 and 8443.
func WithAddrs(plain, tls string) Option {
	return func(o *options) { o.plainAddr, o.tlsAddr = plain, tls }
}

// Healthcheck runs the healthcheck subcommand with opts.
func Healthcheck(opts ...Option) int { return healthcheck(newOptions(opts)) }
