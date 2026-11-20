package api

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"

	"github.com/racherom/modular-platform/libs/contract"
	"github.com/racherom/modular-platform/sdk/httpkit"
	"github.com/racherom/modular-platform/services/api/internal/setup"
)

const maxSetupBody = 64 << 10

// setupRoutes are the setup routes (spec 0036): plain handlers, because they need no admin
// session and set a cookie. Each POST needs the public origin, like every mutating route.
type setupRoutes struct {
	svc    *setup.Service
	origin string
	log    *slog.Logger
}

func (h setupRoutes) mount(mux *http.ServeMux, prefix string) {
	mux.HandleFunc("GET "+prefix+"/setup", h.get)
	mux.Handle("POST "+prefix+"/setup/code", h.sameOrigin(http.HandlerFunc(h.code)))
	mux.Handle("POST "+prefix+"/setup/enroll", h.sameOrigin(http.HandlerFunc(h.enroll)))
	mux.Handle("POST "+prefix+"/setup/identity-link", h.sameOrigin(http.HandlerFunc(h.identityLink)))
	mux.HandleFunc("GET "+prefix+"/setup/identity-enter", h.identityEnter)
	mux.HandleFunc("GET "+prefix+"/setup/gate", h.gate)
}

func (h setupRoutes) sameOrigin(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Origin") != h.origin {
			httpkit.WriteProblem(w, http.StatusForbidden, "bad_origin", "The request does not come from the platform's own origin.")
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (h setupRoutes) get(w http.ResponseWriter, r *http.Request) {
	st, err := h.svc.State(r)
	if err != nil {
		h.fail(w, r, err)
		return
	}
	out := SetupState{State: SetupStateStateDone}
	if st.Pending {
		out.State = SetupStateStatePending
		out.Session = st.Session
		out.ProviderReady = st.ProviderReady
		if st.NoCode {
			out.Reason = NoCode
		}
		if st.Enroll != nil {
			asks := make([]SetupAsk, len(st.Enroll))
			for i, a := range st.Enroll {
				asks[i] = SetupAsk{Name: a.Name, Description: a.Description, Secret: a.Secret, Pattern: a.Pattern, MinLength: a.MinLength}
			}
			out.Enroll = &SetupEnrollAsks{Asks: asks}
		}
	}
	w.Header().Set("Cache-Control", "no-store")
	httpkit.WriteJSON(w, http.StatusOK, out)
}

func (h setupRoutes) code(w http.ResponseWriter, r *http.Request) {
	var body SetupCode
	if !decodeSetupBody(w, r, &body) {
		return
	}
	cookie, err := h.svc.Redeem(r, body.Code)
	switch {
	case errors.Is(err, setup.ErrCodeRefused):
		httpkit.WriteProblem(w, http.StatusUnauthorized, "code_refused", "This code is not accepted.")
	case errors.Is(err, setup.ErrTooManyTries):
		w.Header().Set("Retry-After", "6")
		httpkit.WriteProblem(w, http.StatusTooManyRequests, "too_many_tries", "Too many tries; wait a minute.")
	case errors.Is(err, setup.ErrDone):
		httpkit.WriteProblem(w, http.StatusConflict, "setup_done", "The setup is already done.")
	case err != nil:
		h.fail(w, r, err)
	default:
		http.SetCookie(w, cookie)
		w.Header().Set("Cache-Control", "no-store")
		w.WriteHeader(http.StatusNoContent)
	}
}

func (h setupRoutes) enroll(w http.ResponseWriter, r *http.Request) {
	var body SetupEnroll
	if !decodeSetupBody(w, r, &body) {
		return
	}
	err := h.svc.Enroll(r, body.Answers)
	if invalid, ok := errors.AsType[*setup.InvalidAnswerError](err); ok {
		httpkit.WriteJSON(w, http.StatusUnprocessableEntity, contract.ProblemList{Problems: []contract.Problem{{
			Severity: contract.Error, Code: "invalid_answer", Path: "answers." + invalid.Field, Message: "The answer breaks the rule: " + invalid.Rule + ".",
		}}})
		return
	}
	switch {
	case errors.Is(err, setup.ErrNoSession):
		httpkit.WriteProblem(w, http.StatusUnauthorized, "no_session", "Enter the setup code first.")
	case errors.Is(err, setup.ErrDone):
		httpkit.WriteProblem(w, http.StatusConflict, "setup_done", "The setup is already done.")
	case errors.Is(err, setup.ErrNoEnroll):
		httpkit.WriteProblem(w, http.StatusNotFound, "no_enroll", "The identity module has nothing to enroll.")
	case errors.Is(err, setup.ErrEnrollFailed):
		httpkit.WriteProblem(w, http.StatusBadGateway, "enroll_failed", "Registration failed; check your answers and try again.")
	case errors.Is(err, setup.ErrBusy):
		w.Header().Set("Retry-After", "5")
		httpkit.WriteProblem(w, http.StatusServiceUnavailable, "busy", "The platform is busy; try again.")
	case err != nil:
		h.fail(w, r, err)
	default:
		w.WriteHeader(http.StatusNoContent)
	}
}

func (h setupRoutes) identityLink(w http.ResponseWriter, r *http.Request) {
	link, err := h.svc.IdentityLink(r)
	w.Header().Set("Cache-Control", "no-store")
	switch {
	case errors.Is(err, setup.ErrNoSession):
		httpkit.WriteProblem(w, http.StatusForbidden, "setup_pending", "Enter the setup code first.")
	case errors.Is(err, setup.ErrDone):
		httpkit.WriteProblem(w, http.StatusConflict, "setup_done", "The setup is already done.")
	case errors.Is(err, setup.ErrNoFirstAdmin):
		httpkit.WriteProblem(w, http.StatusNotFound, "no_first_admin", "The identity module has no first admin page.")
	case errors.Is(err, setup.ErrProviderUnavailable):
		w.Header().Set("Retry-After", "2")
		httpkit.WriteProblem(w, http.StatusServiceUnavailable, "provider_unavailable", "The identity provider is not ready yet.")
	case err != nil:
		h.fail(w, r, err)
	default:
		httpkit.WriteJSON(w, http.StatusOK, IdentityLink{Url: link})
	}
}

// identityEnter is routed on the auth host. The token is a bearer token in a URL, so the answer
// is never cached and never sends a referrer.
func (h setupRoutes) identityEnter(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Referrer-Policy", "no-referrer")
	w.Header().Set("Cache-Control", "no-store")
	cookie, to, err := h.svc.Enter(r, r.URL.Query().Get("t"))
	switch {
	case errors.Is(err, setup.ErrHandoffRefused):
		httpkit.WriteProblem(w, http.StatusForbidden, "handoff_refused", "This link is not valid; make a new one on the setup page.")
	case err != nil:
		h.fail(w, r, err)
	default:
		http.SetCookie(w, cookie)
		http.Redirect(w, r, to, http.StatusFound) //nolint:gosec // to is the identity module's first admin origin and path from config, never request input
	}
}

// gate answers the ingress's forward auth question: 204 or 403, nothing else, so a failure of
// any kind refuses.
func (h setupRoutes) gate(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	if h.svc.GateOpen(r) {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	w.WriteHeader(http.StatusForbidden)
}

// fail logs an unexpected error without any request content and answers 500.
func (h setupRoutes) fail(w http.ResponseWriter, r *http.Request, err error) {
	h.log.ErrorContext(r.Context(), "setup request failed", slog.String("path", r.URL.Path), slog.Any("error", err))
	httpkit.WriteProblem(w, http.StatusInternalServerError, "internal", "api failed; try again.")
}

// decodeSetupBody reads one JSON object of up to 64 KiB with no unknown key; it answers the
// problem itself and returns false when the body is no such object.
func decodeSetupBody(w http.ResponseWriter, r *http.Request, v any) bool {
	data, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxSetupBody))
	if err != nil {
		httpkit.WriteProblem(w, http.StatusBadRequest, "invalid_request", "The request body could not be read.")
		return false
	}
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil || dec.More() {
		httpkit.WriteProblem(w, http.StatusBadRequest, "invalid_request", "The body must be one JSON object with the documented keys.")
		return false
	}
	return true
}
