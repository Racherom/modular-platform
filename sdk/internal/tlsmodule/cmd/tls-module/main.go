// Command tls-module answers GET /hello with hi, for the sdk's TLS tests against pebble.
package main

import (
	"context"
	"io"
	"net/http"

	"github.com/racherom/modular-platform/sdk"
	"github.com/racherom/modular-platform/sdk/internal/tlsmodule"
)

func main() { sdk.Run(tlsmodule.Module, setup) }

func setup(context.Context, *sdk.Module[tlsmodule.Config, tlsmodule.Uses]) (http.Handler, error) {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /hello", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, "hi")
	})
	return mux, nil
}
