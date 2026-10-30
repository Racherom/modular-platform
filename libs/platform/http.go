package platform

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/racherom/modular-platform/libs/contract"
)

// NewLogger returns the JSON logger every service writes to stdout.
func NewLogger() *slog.Logger {
	return slog.New(slog.NewJSONHandler(os.Stdout, nil))
}

// WriteProblem answers with one request level problem, the error shape of every platform API.
func WriteProblem(w http.ResponseWriter, status int, code, message string) {
	WriteJSON(w, status, contract.ProblemList{Problems: []contract.Problem{
		{Severity: contract.Error, Code: code, Path: "", Message: message},
	}})
}

// WriteJSON answers with v as JSON.
func WriteJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v) // the client went away; nothing left to tell it
}

// Health serves /healthz (the process is up) and /readyz (ready reports whether it can serve).
func Health(mux *http.ServeMux, ready func() bool) {
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	mux.HandleFunc("GET /readyz", func(w http.ResponseWriter, _ *http.Request) {
		if ready() {
			w.WriteHeader(http.StatusOK)
			return
		}
		w.WriteHeader(http.StatusServiceUnavailable)
	})
}

// HealthPaths are exempt from auth and rate limits.
var HealthPaths = []string{"/healthz", "/readyz"}

// Healthcheck is the healthcheck subcommand of every binary (distroless images have no curl):
// it calls url and returns the exit code, 0 for a 2xx answer.
func Healthcheck(url string) int {
	client := http.Client{Timeout: 3 * time.Second}
	resp, err := client.Get(url)
	if err != nil {
		fmt.Fprintln(os.Stderr, "healthcheck:", err)
		return 1
	}
	_ = resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		fmt.Fprintln(os.Stderr, "healthcheck:", resp.Status)
		return 1
	}
	return 0
}

// BearerAuth admits a request only with Authorization: Bearer <token>, compared in constant
// time, and answers 401 otherwise. Paths in exempt pass untouched.
func BearerAuth(token string, exempt []string, next http.Handler) http.Handler {
	skip := map[string]bool{}
	for _, p := range exempt {
		skip[p] = true
	}
	want := []byte(token)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if skip[r.URL.Path] {
			next.ServeHTTP(w, r)
			return
		}
		got, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
		if !ok || subtle.ConstantTimeCompare([]byte(got), want) != 1 {
			w.Header().Set("WWW-Authenticate", "Bearer")
			WriteProblem(w, http.StatusUnauthorized, "unauthorized", "This planner needs a valid bearer token.")
			return
		}
		next.ServeHTTP(w, r)
	})
}

// AccessLog logs method, path, status and duration of each request, except the health probes.
// It never logs bodies, queries or client addresses.
func AccessLog(log *slog.Logger, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/healthz" || r.URL.Path == "/readyz" {
			next.ServeHTTP(w, r)
			return
		}
		start := time.Now()
		rec := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(rec, r)
		log.LogAttrs(r.Context(), slog.LevelInfo, "request",
			slog.String("method", r.Method),
			slog.String("path", r.URL.Path),
			slog.Int("status", rec.status),
			slog.Duration("duration", time.Since(start)),
		)
	})
}

type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (r *statusRecorder) WriteHeader(status int) {
	r.status = status
	r.ResponseWriter.WriteHeader(status)
}

func (r *statusRecorder) Unwrap() http.ResponseWriter {
	return r.ResponseWriter
}

// Serve runs handler on addr until ctx ends, then shuts down gracefully.
func Serve(ctx context.Context, log *slog.Logger, addr string, handler http.Handler) error {
	srv := &http.Server{
		Addr:              addr,
		Handler:           handler,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      20 * time.Second,
		IdleTimeout:       60 * time.Second,
		ErrorLog:          slog.NewLogLogger(log.Handler(), slog.LevelWarn),
	}
	errc := make(chan error, 1)
	go func() {
		log.Info("listening", slog.String("addr", addr))
		errc <- srv.ListenAndServe()
	}()
	select {
	case err := <-errc:
		return fmt.Errorf("serve: %w", err)
	case <-ctx.Done():
	}
	shutdownCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		return fmt.Errorf("shutdown: %w", err)
	}
	if err := <-errc; !errors.Is(err, http.ErrServerClosed) {
		return fmt.Errorf("serve: %w", err)
	}
	return nil
}

// EnvInt reads a whole number setting of at least min, or def when it is unset.
func EnvInt(name string, def, min int) (int, error) {
	v, ok := os.LookupEnv(name)
	if !ok || v == "" {
		return def, nil
	}
	n, err := strconv.Atoi(v)
	if err != nil || n < min {
		return 0, fmt.Errorf("%s must be a whole number of at least %d, got %q", name, min, v)
	}
	return n, nil
}

// ReadSecretFile reads a secret from the file a *_FILE setting names. A missing setting, a
// missing file or an empty secret is an error, so a service never starts without its secret.
func ReadSecretFile(setting string) (string, error) {
	path := os.Getenv(setting)
	if path == "" {
		return "", fmt.Errorf("%s is not set", setting)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("%s: %w", setting, err)
	}
	secret := strings.TrimSpace(string(data))
	if secret == "" {
		return "", fmt.Errorf("%s: %s is empty", setting, path)
	}
	return secret, nil
}

// BasePath checks a base path setting: empty for the site root, else like /platform (starts with
// a slash, no trailing slash).
func BasePath(v string) (string, error) {
	if v == "" {
		return "", nil
	}
	if !strings.HasPrefix(v, "/") || strings.HasSuffix(v, "/") || strings.Contains(v, "//") {
		return "", fmt.Errorf("base path %q must be empty or start with / and have no trailing slash", v)
	}
	return v, nil
}
