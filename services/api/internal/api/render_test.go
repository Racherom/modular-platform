package api

import (
	"bytes"
	"context"
	"log/slog"
	"net/http/httptest"
	"testing"

	"github.com/racherom/modular-platform/libs/contract"
)

// zipPlanner answers render with fixed bytes, as the planner's static render does.
type zipPlanner struct {
	Planner
	zip []byte
}

func (p zipPlanner) Render(context.Context, contract.Graph) ([]byte, string, *contract.PlanResult, error) {
	return p.zip, `attachment; filename="whoami-postgres.zip"`, nil, nil
}

// TestRenderPassesTheZipThrough covers AC-20: Download zip in managed mode gives the bytes the
// planner rendered, unchanged, with its file name.
func TestRenderPassesTheZipThrough(t *testing.T) {
	t.Parallel()
	zip := []byte("PK\x03\x04 the planner's bytes \x00\xff")
	s := &Server{planner: zipPlanner{zip: zip}, log: slog.New(slog.DiscardHandler)}
	resp, err := s.Render(t.Context(), RenderRequestObject{Body: &contract.Graph{Name: "whoami-postgres"}})
	if err != nil {
		t.Fatal(err)
	}
	rec := httptest.NewRecorder()
	if err := resp.VisitRenderResponse(rec); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(rec.Body.Bytes(), zip) {
		t.Errorf("body %q, want the planner's bytes %q", rec.Body.Bytes(), zip)
	}
	if got := rec.Header().Get("Content-Disposition"); got != `attachment; filename="whoami-postgres.zip"` {
		t.Errorf("Content-Disposition %q", got)
	}
	if got := rec.Header().Get("Content-Type"); got != "application/zip" {
		t.Errorf("Content-Type %q", got)
	}
}
