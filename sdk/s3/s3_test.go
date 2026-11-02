package s3_test

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"

	"github.com/racherom/modular-platform/sdk"
	"github.com/racherom/modular-platform/sdk/s3"
)

const secretKey = "s3-secret-value"

// stub stands in for the s3 provider: it answers HEAD on a bucket with status and records each
// request's path and Authorization header.
type stub struct {
	status int

	mu    sync.Mutex
	paths []string
	auth  []string
}

func (s *stub) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	s.paths = append(s.paths, r.Method+" "+r.URL.Path)
	s.auth = append(s.auth, r.Header.Get("Authorization"))
	s.mu.Unlock()
	w.WriteHeader(s.status)
}

func (s *stub) last() (path, auth string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.paths) == 0 {
		return "", ""
	}
	return s.paths[len(s.paths)-1], s.auth[len(s.auth)-1]
}

// open opens the client of a binding at srv with region.
func open(t *testing.T, srv *httptest.Server, region string) *s3.Client {
	t.Helper()
	u, err := url.Parse(srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	opened, err := s3.Open(sdk.Binding{
		Capability:  "storage",
		Client:      "s3",
		Bound:       true,
		URL:         u,
		Bucket:      "files",
		Region:      region,
		AccessKeyID: "key-id",
		SecretKey:   sdk.NewSecret(secretKey),
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(opened.Close)
	return opened.Client.(*s3.Client)
}

// TestSigningRegion covers the region a request is signed for: the binding's region, or
// us-east-1 when it is unset, with the binding's access key id, and the bucket addressed by path
// (covers: AC-6).
func TestSigningRegion(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		region string
		want   string
	}{
		{"an unset region signs for us-east-1", "", "/us-east-1/s3/aws4_request"},
		{"a set region signs for itself", "eu-west-1", "/eu-west-1/s3/aws4_request"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			s := &stub{status: http.StatusOK}
			srv := httptest.NewServer(s)
			t.Cleanup(srv.Close)
			c := open(t, srv, tt.region)

			if err := c.Ready(t.Context()); err != nil {
				t.Fatalf("Ready() = %v", err)
			}

			path, auth := s.last()
			if path != "HEAD /files/" && path != "HEAD /files" {
				t.Errorf("request = %q, want HEAD on /files", path)
			}
			if !strings.Contains(auth, "Credential=key-id/") || !strings.Contains(auth, tt.want) {
				t.Errorf("Authorization = %q, want key-id signing for %s", auth, tt.want)
			}
			if strings.Contains(auth, secretKey) {
				t.Error("the secret key went out in the Authorization header")
			}
		})
	}
}

// TestReady covers readiness: ready once the bucket exists, not ready while it is missing or the
// provider refuses the call (covers: AC-9).
func TestReady(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		status  int
		wantErr string // empty: ready
	}{
		{"the bucket exists", http.StatusOK, ""},
		{"the bucket is missing", http.StatusNotFound, "s3: bucket files does not exist"},
		{"the provider refuses the credentials", http.StatusForbidden, "s3: bucket files: "},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			srv := httptest.NewServer(&stub{status: tt.status})
			t.Cleanup(srv.Close)
			c := open(t, srv, "")

			err := c.Ready(t.Context())

			switch {
			case tt.wantErr == "" && err != nil:
				t.Errorf("Ready() = %v, want ready", err)
			case tt.wantErr != "" && (err == nil || !strings.HasPrefix(err.Error(), tt.wantErr)):
				t.Errorf("Ready() = %v, want an error starting %q", err, tt.wantErr)
			case err != nil && strings.Contains(err.Error(), secretKey):
				t.Errorf("Ready() shows the secret key: %v", err)
			}
		})
	}
}
