// Command pair-consumer answers GET /call with what pair-provider's /hello answered, called
// through the sdk with the binding token. `pair-consumer get <url>` prints the status and body of
// one plain GET, so the test can call from inside the network of a container without a shell.
package main

import (
	"context"
	"io"
	"net/http"
	"os"
	"strconv"
	"strings"

	"github.com/racherom/modular-platform/libs/render/internal/sdkpair/consumer"
	"github.com/racherom/modular-platform/sdk"
)

func main() {
	if len(os.Args) == 3 && os.Args[1] == "get" {
		os.Exit(get(os.Args[2]))
	}
	sdk.Run(consumer.Module, setup)
}

func setup(ctx context.Context, m *sdk.Module[consumer.Config, consumer.Uses]) (http.Handler, error) {
	shout, err := m.Config.Shout(ctx)
	if err != nil {
		return nil, err
	}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /call", func(w http.ResponseWriter, r *http.Request) {
		greet, err := m.Uses.Greet(r.Context())
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		req, err := greet.NewRequest(r.Context(), http.MethodGet, "/hello", nil)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		resp, err := greet.Do(req)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadGateway)
			return
		}
		defer resp.Body.Close()
		body, _ := io.ReadAll(resp.Body)
		if shout {
			body = []byte(strings.ToUpper(string(body)))
		}
		w.WriteHeader(resp.StatusCode)
		_, _ = w.Write(body)
	})
	return mux, nil
}

// get prints "<status> <body>" of GET url on stdout; it returns 1 when the call fails.
func get(url string) int {
	resp, err := http.Get(url) //nolint:noctx // a one shot probe that exits right after
	if err != nil {
		_, _ = io.WriteString(os.Stderr, err.Error()+"\n")
		return 1
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	_, _ = io.WriteString(os.Stdout, strconv.Itoa(resp.StatusCode)+" "+string(body)+"\n")
	return 0
}
