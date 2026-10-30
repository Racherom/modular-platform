package api

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestTimeoutIsJSON(t *testing.T) {
	slow := http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) { <-r.Context().Done() })
	h := jsonTimeout(http.TimeoutHandler(slow, 10*time.Millisecond, `{"problems":[]}`))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/v1/render", nil))
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status %d, want 503", rec.Code)
	}
	if got := rec.Header().Get("Content-Type"); got != "application/json" {
		t.Fatalf("Content-Type %q, want application/json", got)
	}
}

func TestTimeoutKeepsOwnContentType(t *testing.T) {
	fast := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/zip")
		w.WriteHeader(http.StatusOK)
	})
	h := jsonTimeout(http.TimeoutHandler(fast, time.Second, ""))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/v1/render", nil))
	if got := rec.Header().Get("Content-Type"); got != "application/zip" {
		t.Fatalf("Content-Type %q, want application/zip", got)
	}
}
