package tlsacme_test

import (
	"crypto/tls"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"golang.org/x/crypto/acme"

	"github.com/racherom/modular-platform/sdk/internal/testpki"
	"github.com/racherom/modular-platform/sdk/tlsacme"
)

// stub is a small ACME server: one account, one order with one authorization validated over
// tls-alpn-01 by asking the client's Challenge directly, and a certificate signed by ca for the
// CSR's key. Its fields pick how it answers; it records what it saw.
type stub struct {
	t      *testing.T
	srv    *httptest.Server
	ca     *testpki.CA
	client *tlsacme.Client

	mu sync.Mutex
	// how it answers
	accountExists bool     // answer the account with 200 (already registered) instead of 201
	lostAccount   int      // orders still to answer with accountDoesNotExist
	identifier    string   // the name the order returns; the asked one when empty
	authzStatus   string   // the authorization's state before validation
	challengeType []string // the challenges offered
	// what it saw
	registrations int
	asked         string
	validated     bool
	answers       map[string]error // Challenge's result per server name during the validation
	csr           *x509.CertificateRequest
	finalized     bool
}

func newStub(t *testing.T) *stub {
	s := &stub{t: t, ca: testpki.New(t), authzStatus: acme.StatusPending, challengeType: []string{"http-01", "tls-alpn-01"}}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /dir", s.directory)
	mux.HandleFunc("HEAD /nonce", func(http.ResponseWriter, *http.Request) {})
	mux.HandleFunc("POST /account", s.account)
	mux.HandleFunc("POST /order", s.newOrder)
	mux.HandleFunc("POST /order/1", s.order)
	mux.HandleFunc("POST /authz/1", s.authz)
	mux.HandleFunc("POST /chal/1", s.challenge)
	mux.HandleFunc("POST /finalize/1", s.finalize)
	mux.HandleFunc("POST /cert/1", s.cert)
	var nonce int
	s.srv = httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		s.mu.Lock()
		nonce++
		w.Header().Set("Replay-Nonce", fmt.Sprintf("nonce-%d", nonce))
		s.mu.Unlock()
		mux.ServeHTTP(w, r)
	}))
	t.Cleanup(s.srv.Close)
	roots := x509.NewCertPool()
	roots.AddCert(s.srv.Certificate())
	c, err := tlsacme.NewClient(s.srv.URL+"/dir", roots)
	if err != nil {
		t.Fatal(err)
	}
	s.client = c
	return s
}

// seen returns what the stub recorded, read under its lock.
func (s *stub) seen() (registrations int, answers map[string]error, csr *x509.CertificateRequest) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.registrations, s.answers, s.csr
}

func (s *stub) url(path string) string { return s.srv.URL + path }

func (s *stub) json(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	if err := json.NewEncoder(w).Encode(v); err != nil {
		s.t.Error(err)
	}
}

// payload returns the JWS payload of a request.
func (s *stub) payload(r *http.Request) []byte {
	var jws struct{ Payload string }
	if err := json.NewDecoder(r.Body).Decode(&jws); err != nil {
		s.t.Errorf("%s: %v", r.URL.Path, err)
	}
	p, err := base64.RawURLEncoding.DecodeString(jws.Payload)
	if err != nil {
		s.t.Errorf("%s: %v", r.URL.Path, err)
	}
	return p
}

func (s *stub) directory(w http.ResponseWriter, _ *http.Request) {
	s.json(w, http.StatusOK, map[string]string{
		"newNonce": s.url("/nonce"), "newAccount": s.url("/account"), "newOrder": s.url("/order"),
	})
}

func (s *stub) account(w http.ResponseWriter, _ *http.Request) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.registrations++
	w.Header().Set("Location", s.url(fmt.Sprintf("/acct/%d", s.registrations)))
	code := http.StatusCreated
	if s.accountExists {
		code = http.StatusOK
	}
	s.json(w, code, map[string]string{"status": "valid"})
}

func (s *stub) orderBody() map[string]any {
	name := s.identifier
	if name == "" {
		name = s.asked
	}
	status := acme.StatusPending
	switch {
	case s.finalized:
		status = acme.StatusValid
	case s.validated || s.authzStatus == acme.StatusValid:
		status = acme.StatusReady
	}
	body := map[string]any{
		"status":         status,
		"identifiers":    []map[string]string{{"type": "dns", "value": name}},
		"authorizations": []string{s.url("/authz/1")},
		"finalize":       s.url("/finalize/1"),
	}
	if s.finalized {
		body["certificate"] = s.url("/cert/1")
	}
	return body
}

func (s *stub) newOrder(w http.ResponseWriter, r *http.Request) {
	var req struct{ Identifiers []struct{ Value string } }
	if err := json.Unmarshal(s.payload(r), &req); err != nil || len(req.Identifiers) != 1 {
		s.t.Errorf("order request: %v %v", req, err)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.lostAccount > 0 {
		s.lostAccount--
		w.Header().Set("Content-Type", "application/problem+json")
		w.WriteHeader(http.StatusBadRequest)
		_, _ = io.WriteString(w, `{"type":"urn:ietf:params:acme:error:accountDoesNotExist","detail":"no such account"}`)
		return
	}
	s.asked, s.validated, s.finalized, s.csr = req.Identifiers[0].Value, false, false, nil
	w.Header().Set("Location", s.url("/order/1"))
	s.json(w, http.StatusCreated, s.orderBody())
}

func (s *stub) order(w http.ResponseWriter, _ *http.Request) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.json(w, http.StatusOK, s.orderBody())
}

func (s *stub) authz(w http.ResponseWriter, _ *http.Request) {
	s.mu.Lock()
	defer s.mu.Unlock()
	status := s.authzStatus
	if s.validated {
		status = acme.StatusValid
	}
	var chals []map[string]string
	for _, typ := range s.challengeType {
		chals = append(chals, map[string]string{"type": typ, "url": s.url("/chal/1"), "token": "token-" + typ, "status": acme.StatusPending})
	}
	s.json(w, http.StatusOK, map[string]any{
		"identifier": map[string]string{"type": "dns", "value": s.asked},
		"status":     status,
		"challenges": chals,
	})
}

// challenge validates the way an issuer does: it asks for the answer with a hello offering only
// acme-tls/1 for the name (upper case and a trailing dot, which the client must accept), and for
// another name, which must get none.
func (s *stub) challenge(w http.ResponseWriter, _ *http.Request) {
	s.mu.Lock()
	name := s.asked
	s.mu.Unlock()
	answers := map[string]error{}
	for _, sni := range []string{strings.ToUpper(name) + ".", "other.sdk-test.internal"} {
		c, err := s.client.Challenge(&tls.ClientHelloInfo{ServerName: sni, SupportedProtos: []string{acme.ALPNProto}})
		if err == nil {
			leaf, perr := x509.ParseCertificate(c.Certificate[0])
			if perr != nil || !slices.Equal(leaf.DNSNames, []string{name}) {
				err = fmt.Errorf("the answer names %v, %w", leaf.DNSNames, perr)
			}
		}
		answers[sni] = err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.answers, s.validated = answers, true
	s.json(w, http.StatusOK, map[string]string{"type": "tls-alpn-01", "url": s.url("/chal/1"), "token": "token-tls-alpn-01", "status": acme.StatusValid})
}

func (s *stub) finalize(w http.ResponseWriter, r *http.Request) {
	var req struct{ CSR string }
	if err := json.Unmarshal(s.payload(r), &req); err != nil {
		s.t.Error(err)
	}
	der, _ := base64.RawURLEncoding.DecodeString(req.CSR)
	csr, err := x509.ParseCertificateRequest(der)
	if err != nil {
		s.t.Errorf("CSR: %v", err)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.csr, s.finalized = csr, true
	s.json(w, http.StatusOK, s.orderBody())
}

func (s *stub) cert(w http.ResponseWriter, _ *http.Request) {
	s.mu.Lock()
	csr := s.csr
	s.mu.Unlock()
	now := time.Now()
	leaf := s.ca.Sign(s.t, csr.PublicKey, now, now.Add(24*time.Hour), csr.DNSNames...)
	w.Header().Set("Content-Type", "application/pem-certificate-chain")
	_ = pem.Encode(w, &pem.Block{Type: "CERTIFICATE", Bytes: leaf.Raw})
	_, _ = w.Write(s.ca.PEM())
}

// TestClientIssue covers the ACME engine's order (AC-8): it registers, orders the one name,
// answers the tls-alpn-01 validation for that name only (case and a trailing dot ignored),
// empties the answer slot after, sends a CSR with an empty subject, the name alone and an ECDSA
// P-256 key, and returns the chain with its key and parsed leaf. An already registered account
// counts as registered.
func TestClientIssue(t *testing.T) {
	t.Parallel()
	for _, exists := range []bool{false, true} {
		t.Run(fmt.Sprintf("account exists %v", exists), func(t *testing.T) {
			t.Parallel()
			s := newStub(t)
			s.accountExists = exists
			c, err := s.client.Issue(t.Context(), host)
			if err != nil {
				t.Fatal(err)
			}
			registrations, answers, csr := s.seen()
			if c.Leaf == nil || !slices.Equal(c.Leaf.DNSNames, []string{host}) || len(c.Certificate) != 2 {
				t.Fatalf("certificate = leaf %v, chain of %d", c.Leaf, len(c.Certificate))
			}
			if err := c.Leaf.CheckSignatureFrom(mustParse(t, c.Certificate[1])); err != nil {
				t.Error(err)
			}
			if _, err := tls.X509KeyPair(pemChain(c), pemKey(t, c)); err != nil {
				t.Errorf("the key does not match the leaf: %v", err)
			}
			if err := answers[strings.ToUpper(host)+"."]; err != nil {
				t.Errorf("the validation for the host name got no answer: %v", err)
			}
			if err := answers["other.sdk-test.internal"]; err == nil {
				t.Error("a validation for another name got an answer")
			}
			if _, err := s.client.Challenge(&tls.ClientHelloInfo{ServerName: host, SupportedProtos: []string{acme.ALPNProto}}); err == nil {
				t.Error("an answer is still pending after Issue")
			}
			if csr.Subject.String() != "" || !slices.Equal(csr.DNSNames, []string{host}) || csr.SignatureAlgorithm != x509.ECDSAWithSHA256 || csr.PublicKeyAlgorithm != x509.ECDSA {
				t.Errorf("CSR = subject %q, names %v, %s, %s", csr.Subject, csr.DNSNames, csr.SignatureAlgorithm, csr.PublicKeyAlgorithm)
			}
			if registrations != 1 {
				t.Errorf("%d registrations, want 1", registrations)
			}
		})
	}
}

// TestClientRegistersAgain covers a lost account (AC-8): when the issuer answers
// accountDoesNotExist, the next Issue registers again; otherwise no Issue registers twice.
func TestClientRegistersAgain(t *testing.T) {
	t.Parallel()
	s := newStub(t)
	if _, err := s.client.Issue(t.Context(), host); err != nil {
		t.Fatal(err)
	}
	s.mu.Lock()
	s.lostAccount = 1
	s.mu.Unlock()
	_, err := s.client.Issue(t.Context(), host)
	var ae *acme.Error
	if !errors.As(err, &ae) || ae.ProblemType != "urn:ietf:params:acme:error:accountDoesNotExist" {
		t.Fatalf("Issue = %v, want accountDoesNotExist", err)
	}
	for range 2 {
		if _, err := s.client.Issue(t.Context(), host); err != nil {
			t.Fatal(err)
		}
	}
	if n, _, _ := s.seen(); n != 2 {
		t.Errorf("%d registrations, want 2", n)
	}
}

// TestClientRefusals covers orders the engine gives up on (AC-8): an order naming something else,
// an authorization in a state it cannot settle, and no tls-alpn-01 challenge offered. An
// authorization that is already valid needs no validation.
func TestClientRefusals(t *testing.T) {
	t.Parallel()
	for name, tc := range map[string]struct {
		set  func(s *stub)
		want string // empty: Issue succeeds
	}{
		"another identifier":    {func(s *stub) { s.identifier = "planner.sdk-test.internal" }, "the order names"},
		"invalid authorization": {func(s *stub) { s.authzStatus = acme.StatusInvalid }, "is invalid"},
		"expired authorization": {func(s *stub) { s.authzStatus = "expired" }, "is expired"},
		"no tls-alpn-01":        {func(s *stub) { s.challengeType = []string{"http-01", "dns-01"} }, "no tls-alpn-01 challenge"},
		"valid authorization":   {func(s *stub) { s.authzStatus = acme.StatusValid }, ""},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			s := newStub(t)
			tc.set(s)
			_, err := s.client.Issue(t.Context(), host)
			_, answers, _ := s.seen()
			switch {
			case tc.want == "" && err != nil:
				t.Fatal(err)
			case tc.want == "" && answers != nil:
				t.Error("a valid authorization was validated again")
			case tc.want != "" && (err == nil || !strings.Contains(err.Error(), tc.want)):
				t.Errorf("Issue = %v, want an error with %q", err, tc.want)
			}
		})
	}
}

func mustParse(t *testing.T, der []byte) *x509.Certificate {
	t.Helper()
	c, err := x509.ParseCertificate(der)
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func pemChain(c *tls.Certificate) []byte {
	var b []byte
	for _, der := range c.Certificate {
		b = append(b, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})...)
	}
	return b
}

func pemKey(t *testing.T, c *tls.Certificate) []byte {
	t.Helper()
	der, err := x509.MarshalPKCS8PrivateKey(c.PrivateKey)
	if err != nil {
		t.Fatal(err)
	}
	return pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: der})
}
