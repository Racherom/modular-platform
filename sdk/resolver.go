package sdk

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/racherom/modular-platform/sdk/internal/load"
	"github.com/racherom/modular-platform/sdk/manifest"
)

// errResolverClosed reports a resolved client asked for after shutdown.
var errResolverClosed = errors.New("sdk: the module is stopping")

// resolver asks a BindingResolver which provider binding a caller reaches, keeps each answer for
// a while, and opens one client per provider binding, shared by every caller it is picked for.
type resolver struct {
	r       BindingResolver
	budget  time.Duration
	specs   map[string]BindingSpec
	boot    load.Bootstrap
	answers *lru[resolvedAnswer]

	mu      sync.Mutex
	clients map[string]Opened // by bindingKey
	closed  bool
}

// resolvedAnswer is the client key the resolver picked, or the fact that it picked none.
type resolvedAnswer struct {
	key   string
	found bool
}

func newResolver(ctx context.Context, specs []BindingSpec, boot load.Bootstrap, o options) *resolver {
	r := &resolver{
		r:       o.resolver,
		budget:  o.callerBudget,
		specs:   map[string]BindingSpec{},
		boot:    boot,
		answers: newLRU[resolvedAnswer](callerEntries, o.callerTTL),
		clients: map[string]Opened{},
	}
	for _, s := range specs {
		r.specs[s.Capability] = s
	}
	context.AfterFunc(ctx, r.close)
	return r
}

// client returns the client of the binding the resolver picks for c, and whether it picked one.
// Only bindings with a client are asked about.
func (r *resolver) client(ctx context.Context, c Caller, capability string) (any, bool, error) {
	spec := r.specs[capability]
	if spec.Client == "" || spec.Open == nil {
		return nil, false, nil
	}
	answerKey := callerKey(c) + "\x00" + capability
	if a, ok := r.answers.get(answerKey); ok {
		if !a.found {
			return nil, false, nil
		}
		r.mu.Lock()
		o, held := r.clients[a.key]
		r.mu.Unlock()
		if held {
			return o.Client, true, nil
		}
	}
	b, found, err := ask(ctx, r.budget, func(ctx context.Context) (Binding, bool, error) {
		return r.r.BindingFor(ctx, c, capability)
	})
	if err != nil {
		// the resolver's own error is never shown: its text may carry a credential
		return nil, false, fmt.Errorf("%w: binding %s", ErrCallerSource, capability)
	}
	if !found {
		r.answers.put(answerKey, resolvedAnswer{})
		return nil, false, nil
	}
	key, o, err := r.open(spec, b)
	if err != nil {
		return nil, false, err
	}
	r.answers.put(answerKey, resolvedAnswer{key: key, found: true})
	return o.Client, true, nil
}

// open returns the client of b, opening it the first time b is picked. b takes the capability,
// client kind and optional flag of spec, MP_CA_FILE's roots when it names none, and the module's
// MP-Caller signer on an http binding.
func (r *resolver) open(spec BindingSpec, b Binding) (string, Opened, error) {
	b.Capability, b.Client, b.Optional, b.Bound = spec.Capability, spec.Client, spec.Optional, true
	switch {
	case b.URL == nil || b.URL.Host == "":
		return "", Opened{}, fmt.Errorf("sdk: the resolved binding %s has no provider address", spec.Capability)
	case r.boot.Downgrade(b.URL):
		return "", Opened{}, fmt.Errorf("sdk: the resolved binding %s: %s: %s is a module on the sdk, which TLS mode never calls in plain HTTP",
			spec.Capability, load.CodeDowngrade, strings.ToLower(b.URL.Hostname()))
	}
	if b.Roots == nil {
		b.Roots = r.boot.Roots
	}
	if spec.Client == manifest.ClientHTTP {
		b.Caller = r.boot.CallerSigner
	}
	key := bindingKey(b)
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.closed {
		return "", Opened{}, errResolverClosed
	}
	if o, ok := r.clients[key]; ok {
		return key, o, nil
	}
	o, err := spec.Open(b)
	if err != nil {
		return "", Opened{}, fmt.Errorf("sdk: the resolved binding %s: %s: %w", spec.Capability, load.CodeInvalidBinding, err)
	}
	r.clients[key] = o
	return key, o, nil
}

// close closes every resolved client once the module stops.
func (r *resolver) close() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.closed = true
	closeAll(r.clients)
	clear(r.clients)
}

// bindingKey names one provider binding: its address and credentials, the secrets hashed so the
// key never holds them.
func bindingKey(b Binding) string {
	h := sha256.New()
	for _, s := range []Secret{b.Token, b.Password, b.SecretKey} {
		h.Write([]byte(s.Reveal()))
		h.Write([]byte{0})
	}
	return strings.Join([]string{
		b.Capability, b.URL.String(), b.Database, b.User, b.SSLMode, b.Bucket, b.Region, b.AccessKeyID,
		hex.EncodeToString(h.Sum(nil)),
	}, "\x00")
}
