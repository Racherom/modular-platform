package api

import (
	"bytes"
	"context"
	"log/slog"
	"net/http/httptest"
	"testing"

	"github.com/racherom/modular-platform/libs/contract"
)

// zipRenderer answers render with fixed bytes, as the renderer's static render does.
type zipRenderer struct {
	Renderer
	zip []byte
}

func (p zipRenderer) Render(context.Context, contract.Graph) ([]byte, string, *contract.CheckResult, error) {
	return p.zip, `attachment; filename="whoami-postgres.zip"`, nil, nil
}

// TestRenderPassesTheZipThrough covers AC-20: Download zip in managed mode gives the bytes the
// renderer rendered, unchanged, with its file name.
func TestRenderPassesTheZipThrough(t *testing.T) {
	t.Parallel()
	zip := []byte("PK\x03\x04 the renderer's bytes \x00\xff")
	s := &Server{renderer: zipRenderer{zip: zip}, log: slog.New(slog.DiscardHandler)}
	resp, err := s.Render(t.Context(), RenderRequestObject{Body: &contract.Graph{Name: "whoami-postgres"}})
	if err != nil {
		t.Fatal(err)
	}
	rec := httptest.NewRecorder()
	if err := resp.VisitRenderResponse(rec); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(rec.Body.Bytes(), zip) {
		t.Errorf("body %q, want the renderer's bytes %q", rec.Body.Bytes(), zip)
	}
	if got := rec.Header().Get("Content-Disposition"); got != `attachment; filename="whoami-postgres.zip"` {
		t.Errorf("Content-Disposition %q", got)
	}
	if got := rec.Header().Get("Content-Type"); got != "application/zip" {
		t.Errorf("Content-Type %q", got)
	}
}

// refusingRenderer answers render with the check result of a graph that holds a recommendation.
type refusingRenderer struct{ Renderer }

func (refusingRenderer) Render(context.Context, contract.Graph) ([]byte, string, *contract.CheckResult, error) {
	return nil, "", &contract.CheckResult{Problems: []contract.Problem{{
		Severity: contract.Error, Code: "binding_recommended", Path: "modules.ingress.uses.trust", Candidates: []string{"cert-issuer"},
	}}}, nil
}

// TestRenderRefusesARecommendation covers spec 0027 AC-8 at api: a graph with an open
// recommendation gets 422 with the renderer's check result, not a zip.
func TestRenderRefusesARecommendation(t *testing.T) {
	t.Parallel()
	s := &Server{renderer: refusingRenderer{}, log: slog.New(slog.DiscardHandler)}
	resp, err := s.Render(t.Context(), RenderRequestObject{Body: &contract.Graph{Name: "gap"}})
	if err != nil {
		t.Fatal(err)
	}
	rec := httptest.NewRecorder()
	if err := resp.VisitRenderResponse(rec); err != nil {
		t.Fatal(err)
	}
	if rec.Code != 422 || !bytes.Contains(rec.Body.Bytes(), []byte(`"code":"binding_recommended"`)) {
		t.Errorf("status %d body %s", rec.Code, rec.Body)
	}
}
