package httpclient

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
)

const token = "binding-token-0123456789abcdefghijkl"

func authServer(t *testing.T) (*httptest.Server, *[]string) {
	t.Helper()
	var seen []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen = append(seen, r.URL.String()+" "+r.Header.Get("Authorization"))
		w.WriteHeader(http.StatusNoContent)
	}))
	t.Cleanup(srv.Close)
	return srv, &seen
}

// TestBearer covers the token going to the binding's own address, below its base path, and
// nowhere else.
func TestBearer(t *testing.T) {
	t.Parallel()
	own, ownSeen := authServer(t)
	other, otherSeen := authServer(t)
	base, _ := url.Parse(own.URL + "/api")
	c := New(base, token)

	req, err := c.NewRequest(t.Context(), http.MethodGet, "/plan?x=1", nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := c.Do(req); err != nil {
		t.Fatal(err)
	}
	if want := "/api/plan?x=1 Bearer " + token; fmt.Sprint(*ownSeen) != "["+want+"]" {
		t.Errorf("provider saw %q, want %q", *ownSeen, want)
	}

	away, _ := http.NewRequestWithContext(t.Context(), http.MethodGet, other.URL+"/x", nil)
	if _, err := c.Do(away); err != nil {
		t.Fatal(err)
	}
	if fmt.Sprint(*otherSeen) != "[/x ]" {
		t.Errorf("another host saw %q, want no Authorization", *otherSeen)
	}
}

func TestNoToken(t *testing.T) {
	t.Parallel()
	srv, seen := authServer(t)
	base, _ := url.Parse(srv.URL)
	if err := New(base, "").Ready(t.Context()); err != nil {
		t.Fatal(err)
	}
	if fmt.Sprint(*seen) != "[/readyz ]" {
		t.Errorf("provider saw %q", *seen)
	}
}

func TestReady(t *testing.T) {
	t.Parallel()
	for status, ready := range map[int]bool{200: true, 401: true, 404: true, 499: true, 500: false, 503: false} {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(status) }))
		base, _ := url.Parse(srv.URL)
		err := New(base, token).Ready(t.Context())
		srv.Close()
		if (err == nil) != ready {
			t.Errorf("status %d: err = %v, want ready %v", status, err, ready)
		}
	}
}

func TestTokenHidden(t *testing.T) {
	t.Parallel()
	base, _ := url.Parse("http://provider:8080")
	c := New(base, token)
	for _, verb := range []string{"%v", "%+v", "%s", "%#v"} {
		if got := fmt.Sprintf(verb, c); got != "httpclient.Client{http://provider:8080}" {
			t.Errorf("%s = %q", verb, got)
		}
	}
	if got := c.LogValue().String(); got != "http://provider:8080" {
		t.Errorf("LogValue = %q", got)
	}
}
