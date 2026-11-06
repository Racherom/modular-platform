package api

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/racherom/modular-platform/libs/contract"
	"github.com/racherom/modular-platform/sdk"
	"github.com/racherom/modular-platform/sdk/httpkit"
	"github.com/racherom/modular-platform/services/api/internal/deploy"
	"github.com/racherom/modular-platform/services/api/internal/signin"
)

const (
	maxBody        = 1 << 20 // 1 MB
	requestTimeout = 30 * time.Second
)

// Options wires the HTTP layer.
type Options struct {
	Signin       *signin.Service
	Deploy       *deploy.Service
	Hub          *deploy.Hub
	Planner      Planner
	PublicOrigin string
	BasePath     string
	Idle         time.Duration
	Log          *slog.Logger
}

// NewHandler serves api's contract under <base>/api/v1: the auth routes and the event stream as
// plain handlers, everything else through the strict server. Every route but login and
// callback needs a session; every mutating route needs the public origin.
func NewHandler(o Options) http.Handler {
	s := &Server{signin: o.Signin, deploy: o.Deploy, planner: o.Planner, log: o.Log}
	strict := NewStrictHandlerWithOptions(s, nil, StrictHTTPServerOptions{
		RequestErrorHandlerFunc: func(w http.ResponseWriter, _ *http.Request, _ error) {
			httpkit.WriteProblem(w, http.StatusBadRequest, "invalid_request", "The request could not be read.")
		},
		ResponseErrorHandlerFunc: func(w http.ResponseWriter, r *http.Request, err error) {
			o.Log.ErrorContext(r.Context(), "request failed", slog.String("path", r.URL.Path), slog.Any("error", err))
			httpkit.WriteProblem(w, http.StatusInternalServerError, "internal", "api failed; try again.")
		},
	})
	guard := guard{signin: o.Signin, origin: o.PublicOrigin, idle: o.Idle}
	mux := http.NewServeMux()
	prefix := o.BasePath + "/api/v1"
	mux.HandleFunc("GET "+prefix+"/auth/login", o.Signin.Login)
	mux.HandleFunc("GET "+prefix+"/auth/callback", o.Signin.Callback)
	mux.Handle("POST "+prefix+"/auth/logout", guard.wrap(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		next, err := o.Signin.Logout(w, r, sessionFrom(r.Context()).Session)
		if err != nil {
			o.Log.ErrorContext(r.Context(), "sign out failed", slog.Any("error", err))
			httpkit.WriteProblem(w, http.StatusInternalServerError, "internal", "Sign out failed; try again.")
			return
		}
		httpkit.WriteJSON(w, http.StatusOK, Logout{Redirect: next})
	}), true))
	HandlerWithOptions(strict, StdHTTPServerOptions{
		BaseURL:    o.BasePath,
		BaseRouter: mux,
		// the last one runs first: the origin, then the session, then the body
		Middlewares: []MiddlewareFunc{strictBodies, func(next http.Handler) http.Handler { return guard.wrap(next, true) }},
	})
	timed := http.TimeoutHandler(mux, requestTimeout, `{"problems":[{"severity":"error","code":"timeout","path":"","message":"api took too long; try again."}]}`)

	// the event stream never ends, so it stays outside the timeout, which buffers its answer
	stream := guard.wrap(&events{signin: o.Signin, deploy: o.Deploy, hub: o.Hub, log: o.Log, perSession: map[string]int{}}, false)
	outer := http.NewServeMux()
	outer.Handle("GET "+prefix+"/events", stream)
	outer.Handle("/", timed)
	return outer
}

// requestSession is the session of a request, with when it ends without activity.
type requestSession struct {
	signin.Session
	IdleExpiresAt time.Time
}

type sessionKey struct{}

func sessionFrom(ctx context.Context) requestSession {
	s, _ := ctx.Value(sessionKey{}).(requestSession)
	return s
}

// guard checks the Origin of mutating requests (403 bad_origin) and the session (401
// unauthenticated). The session's admin becomes the MP-Caller of every call api makes for the
// request. touch is false for the event stream, which is no activity.
type guard struct {
	signin *signin.Service
	origin string
	idle   time.Duration
}

func (g guard) wrap(next http.Handler, touch bool) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && r.Method != http.MethodHead && r.Header.Get("Origin") != g.origin {
			httpkit.WriteProblem(w, http.StatusForbidden, "bad_origin", "The request does not come from the platform's own origin.")
			return
		}
		check := g.signin.Peek
		if touch {
			check = g.signin.Authenticate
		}
		sess, err := check(r)
		if errors.Is(err, signin.ErrUnauthenticated) {
			httpkit.WriteProblem(w, http.StatusUnauthorized, "unauthenticated", "Sign in first.")
			return
		}
		if err != nil {
			httpkit.WriteProblem(w, http.StatusInternalServerError, "internal", "api failed; try again.")
			return
		}
		if touch {
			// a request is activity: the idle limit starts again from now
			sess.LastSeenAt = time.Now()
		}
		ctx := context.WithValue(r.Context(), sessionKey{}, requestSession{Session: sess, IdleExpiresAt: sess.IdleExpiresAt(g.idle)})
		ctx = sdk.WithCaller(ctx, sdk.Caller{Issuer: sess.Identity.Issuer, User: sess.Identity.Subject})
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// strictBodies decodes graph bodies with the strict decoder of spec 0002 before the generated
// code sees them, as the planner does: over 1 MB is 413; unknown keys, duplicate keys, another
// apiVersion or kind are 400. The plan and render routes take a bare graph; review and apply
// take an object with a graph key.
func strictBodies(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			next.ServeHTTP(w, r)
			return
		}
		data, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxBody))
		if err != nil {
			if _, tooBig := errors.AsType[*http.MaxBytesError](err); tooBig {
				httpkit.WriteProblem(w, http.StatusRequestEntityTooLarge, "request_too_large", "The body is over 1 MB.")
				return
			}
			httpkit.WriteProblem(w, http.StatusBadRequest, "invalid_request", "The request body could not be read.")
			return
		}
		clean, problem := cleanBody(r.URL.Path, data)
		if problem != nil {
			httpkit.WriteJSON(w, http.StatusBadRequest, contract.ProblemList{Problems: []contract.Problem{*problem}})
			return
		}
		r.Body = io.NopCloser(bytes.NewReader(clean))
		r.ContentLength = int64(len(clean))
		next.ServeHTTP(w, r)
	})
}

func cleanBody(path string, data []byte) ([]byte, *contract.Problem) {
	if strings.HasSuffix(path, "/plan") || strings.HasSuffix(path, "/render") {
		g, problem := contract.DecodeGraph(data)
		if problem != nil {
			return nil, problem
		}
		clean, _ := json.Marshal(g) //nolint:errchkjson // a decoded graph always encodes
		return clean, nil
	}
	if !strings.HasSuffix(path, "/review") && !strings.HasSuffix(path, "/apply") {
		return data, nil // a body without a graph, like remove's: the generated decoder reads it
	}
	var envelope map[string]json.RawMessage
	dec := json.NewDecoder(bytes.NewReader(data))
	if err := dec.Decode(&envelope); err != nil || dec.More() {
		return nil, &contract.Problem{Severity: contract.Error, Code: "invalid_request", Message: "The body must be one JSON object."}
	}
	allowed := map[string]bool{"graph": true}
	if strings.HasSuffix(path, "/apply") {
		allowed["planDigest"], allowed["placed"] = true, true
	}
	for k := range envelope {
		if !allowed[k] {
			return nil, &contract.Problem{Severity: contract.Error, Code: "invalid_request", Path: k, Message: "The body has an unknown key " + k + "."}
		}
	}
	raw, ok := envelope["graph"]
	if !ok {
		return data, nil // the generated decoder names what is missing
	}
	g, problem := contract.DecodeGraph(raw)
	if problem != nil {
		problem.Path = strings.TrimSuffix("graph."+problem.Path, ".")
		return nil, problem
	}
	envelope["graph"], _ = json.Marshal(g) //nolint:errchkjson // a decoded graph always encodes
	clean, _ := json.Marshal(envelope)     //nolint:errchkjson // raw messages always encode
	return clean, nil
}
