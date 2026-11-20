package clients_test

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/racherom/modular-platform/sdk/httpclient"
	"github.com/racherom/modular-platform/services/api/internal/clients"
	"github.com/racherom/modular-platform/services/api/internal/deploy"
)

// TestImagesLockInvalid covers the 500 lock_invalid of GET …/images (spec 0031): the error is
// ErrLockInvalid naming the file, and a path that is not one of the two lock files is dropped.
func TestImagesLockInvalid(t *testing.T) {
	t.Parallel()
	for name, tc := range map[string]struct{ path, want string }{
		"current":  {path: "images.lock", want: deploy.LockFile},
		"previous": {path: "images.previous.lock", want: deploy.PreviousLockFile},
		"unknown":  {path: "../../etc/passwd", want: ""},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusInternalServerError)
				_, _ = w.Write([]byte(`{"problems":[{"severity":"error","code":"lock_invalid","path":"` + tc.path + `","message":"bad"}]}`))
			}))
			t.Cleanup(srv.Close)
			base, _ := url.Parse(srv.URL)
			target, err := clients.NewTarget(httpclient.New(base, "key"))
			if err != nil {
				t.Fatal(err)
			}
			_, err = target.Images(t.Context(), "mp-sample")
			if !errors.Is(err, deploy.ErrLockInvalid) {
				t.Fatalf("err = %v, want ErrLockInvalid", err)
			}
			l, ok := errors.AsType[*deploy.LockInvalidError](err)
			if !ok || l.File != tc.want {
				t.Errorf("err = %#v, want file %q", err, tc.want)
			}
		})
	}
}

// TestResolveRefused covers AC-6: a resolve call the deploy target refuses as a whole (400) is a
// *ResolveRefusedError carrying its problems, never a result with per reference problems.
func TestResolveRefused(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"problems":[{"severity":"error","code":"invalid_reference","path":"references[0]","message":"bad"}]}`))
	}))
	t.Cleanup(srv.Close)
	base, _ := url.Parse(srv.URL)
	target, err := clients.NewTarget(httpclient.New(base, "key"))
	if err != nil {
		t.Fatal(err)
	}
	_, err = target.Resolve(t.Context(), []string{"traefik/whoami:v1"})
	r, ok := errors.AsType[*deploy.ResolveRefusedError](err)
	if !ok || len(r.Problems) != 1 || r.Problems[0].Path != "references[0]" {
		t.Fatalf("err = %#v, want a ResolveRefusedError with the problem", err)
	}
}
