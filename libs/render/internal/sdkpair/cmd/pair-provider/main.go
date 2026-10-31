// Command pair-provider answers GET /hello with its greeting and the consumer instance whose
// binding token the call carried.
package main

import (
	"context"
	"fmt"
	"io"
	"net/http"

	"github.com/racherom/modular-platform/libs/render/internal/sdkpair/provider"
	"github.com/racherom/modular-platform/sdk"
)

func main() { sdk.Run(provider.Module, setup) }

func setup(ctx context.Context, m *sdk.Module[provider.Config, provider.Uses]) (http.Handler, error) {
	greeting, err := m.Config.Greeting(ctx)
	if err != nil {
		return nil, err
	}
	key, err := m.Config.SigningKey(ctx)
	if err != nil {
		return nil, err
	}
	// setup.sh writes 32 random bytes in hex
	if n := len(key.Reveal()); n != 64 {
		return nil, fmt.Errorf("the generated signing key has %d characters, want 64", n)
	}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /hello", func(w http.ResponseWriter, r *http.Request) {
		caller, _ := sdk.CallerModule(r.Context())
		_, _ = io.WriteString(w, greeting+" "+caller)
	})
	return mux, nil
}
