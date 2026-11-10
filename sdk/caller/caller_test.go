package caller_test

import (
	"bytes"
	"crypto"
	"crypto/ecdsa"
	"crypto/ed25519"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/go-jose/go-jose/v4"
	"github.com/go-jose/go-jose/v4/jwt"

	"github.com/racherom/modular-platform/sdk/auth"
	"github.com/racherom/modular-platform/sdk/caller"
)

const self = "renderer.stack.internal"

var now = time.Date(2026, 11, 1, 12, 0, 0, 0, time.UTC)

func newKey(t *testing.T) ed25519.PrivateKey {
	t.Helper()
	_, key, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	return key
}

// keys returns the Keys of the consumer api: one file of a caller keys directory holding the
// public keys of keys.
func keys(t *testing.T, keys ...ed25519.PrivateKey) caller.Keys {
	t.Helper()
	k, err := caller.KeysOf("api", publicPEM(t, keys...))
	if err != nil {
		t.Fatal(err)
	}
	return k
}

func publicPEM(t *testing.T, keys ...ed25519.PrivateKey) []byte {
	t.Helper()
	var b []byte
	for _, k := range keys {
		der, err := x509.MarshalPKIXPublicKey(k.Public())
		if err != nil {
			t.Fatal(err)
		}
		b = append(b, pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: der})...)
	}
	return b
}

func kid(t *testing.T, pub crypto.PublicKey) string {
	t.Helper()
	b, err := (&jose.JSONWebKey{Key: pub}).Thumbprint(crypto.SHA256)
	if err != nil {
		t.Fatal(err)
	}
	return base64.RawURLEncoding.EncodeToString(b)
}

// sign signs any claims as a token with alg and kid, for the cases Mint never makes.
func sign(t *testing.T, alg jose.SignatureAlgorithm, key any, kid string, claims map[string]any) string {
	t.Helper()
	sig, err := jose.NewSigner(jose.SigningKey{Algorithm: alg, Key: jose.JSONWebKey{Key: key, KeyID: kid}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	token, err := jwt.Signed(sig).Claims(claims).Serialize()
	if err != nil {
		t.Fatal(err)
	}
	return token
}

// good is the claims of a token Verify accepts at now for self.
func good() map[string]any {
	return map[string]any{
		"iss": "api", "sub": "ada", "groups": []string{"admins"}, "aud": self,
		"iat": now.Unix(), "exp": now.Add(5 * time.Minute).Unix(), "jti": "x",
	}
}

func with(c map[string]any, k string, v any) map[string]any {
	c[k] = v
	if v == nil {
		delete(c, k)
	}
	return c
}

// TestMintVerify covers a minted token (AC-11): Verify returns its caller; its header carries
// EdDSA and the kid; its claims carry iss, sub, groups, one aud, iat, exp 5 minutes later and a
// 16 byte jti.
func TestMintVerify(t *testing.T) {
	t.Parallel()
	key := newKey(t)
	token, err := caller.Mint(key, caller.Caller{Issuer: "api", User: "ada", Groups: []string{"admins", "ops"}}, self, now)
	if err != nil {
		t.Fatal(err)
	}
	got, err := caller.Verify(keys(t, newKey(t), key), "api", token, self, now)
	if err != nil {
		t.Fatal(err)
	}
	if got.Issuer != "api" || got.User != "ada" || !slices.Equal(got.Groups, []string{"admins", "ops"}) {
		t.Errorf("Verify = %+v", got)
	}

	parts := strings.Split(token, ".")
	var header struct{ Alg, Kid, Typ string }
	var claims struct {
		Iss, Sub, Aud, Jti string
		Groups             []string
		Iat, Exp           int64
	}
	decode(t, parts[0], &header)
	decode(t, parts[1], &claims)
	if header.Alg != "EdDSA" || header.Kid != kid(t, key.Public()) {
		t.Errorf("header = %+v", header)
	}
	jti, _ := base64.RawURLEncoding.DecodeString(claims.Jti)
	if claims.Iss != "api" || claims.Sub != "ada" || claims.Aud != self || claims.Iat != now.Unix() ||
		claims.Exp != now.Add(5*time.Minute).Unix() || len(jti) != 16 {
		t.Errorf("claims = %+v", claims)
	}

	other, _ := caller.Mint(key, caller.Caller{Issuer: "api", User: "ada"}, self, now)
	var again struct{ Jti string }
	decode(t, strings.Split(other, ".")[1], &again)
	if again.Jti == claims.Jti {
		t.Error("two tokens share a jti")
	}
	if c, err := caller.Verify(keys(t, key), "api", other, self, now); err != nil || c.Groups == nil || len(c.Groups) != 0 {
		t.Errorf("no groups: %+v, %v; want an empty list", c, err)
	}
}

func decode(t *testing.T, part string, v any) {
	t.Helper()
	b, err := base64.RawURLEncoding.DecodeString(part)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(b, v); err != nil {
		t.Fatal(err)
	}
}

// TestKID covers the kid (AC-11): the RFC 7638 thumbprint, checked against RFC 8037 appendix A.3.
func TestKID(t *testing.T) {
	t.Parallel()
	seed, _ := base64.RawURLEncoding.DecodeString("nWGxne_9WmC6hEr0kuwsxERJxWl7MmkZcDusAxyuf2A")
	token, err := caller.Mint(ed25519.NewKeyFromSeed(seed), caller.Caller{Issuer: "api", User: "ada"}, self, now)
	if err != nil {
		t.Fatal(err)
	}
	var header struct{ Kid string }
	decode(t, strings.Split(token, ".")[0], &header)
	if header.Kid != "kPrK_qmxVWaYVA9wwBF6Iuo3vVzz7TxHCTwXBygrS4k" {
		t.Errorf("kid = %s", header.Kid)
	}
}

// TestMintRefusesOtherKeys covers Mint's one error (AC-11): a key that is not Ed25519.
func TestMintRefusesOtherKeys(t *testing.T) {
	t.Parallel()
	ec, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if _, err := caller.Mint(ec, caller.Caller{User: "ada"}, self, now); err == nil {
		t.Error("an ECDSA key minted a token")
	}
}

// TestVerifyRefusals covers each refusal of AC-11 that Verify checks, and the 30 second leeway
// on both sides of it.
func TestVerifyRefusals(t *testing.T) {
	t.Parallel()
	key := newKey(t)
	ks := keys(t, key)
	kidOK := kid(t, key.Public())
	ec, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	stranger := newKey(t)
	edsa := func(c map[string]any) string { return sign(t, jose.EdDSA, key, kidOK, c) }
	tampered := func() string {
		parts := strings.Split(edsa(good()), ".")
		b, _ := json.Marshal(with(good(), "sub", "root"))
		return parts[0] + "." + base64.RawURLEncoding.EncodeToString(b) + "." + parts[2]
	}
	none := func() string {
		h, _ := json.Marshal(map[string]string{"alg": "none", "kid": kidOK})
		b, _ := json.Marshal(good())
		return base64.RawURLEncoding.EncodeToString(h) + "." + base64.RawURLEncoding.EncodeToString(b) + "."
	}
	for name, tc := range map[string]struct {
		token string
		at    time.Time
		ok    bool
	}{
		"a good token":                {token: edsa(good()), at: now, ok: true},
		"another aud":                 {token: edsa(with(good(), "aud", "api.stack.internal")), at: now},
		"the bare instance as aud":    {token: edsa(with(good(), "aud", "renderer")), at: now},
		"two auds":                    {token: edsa(with(good(), "aud", []string{self, "api.stack.internal"})), at: now},
		"expired within leeway":       {token: edsa(good()), at: now.Add(5*time.Minute + 29*time.Second), ok: true},
		"expired":                     {token: edsa(good()), at: now.Add(5*time.Minute + 31*time.Second)},
		"iat ahead within leeway":     {token: edsa(good()), at: now.Add(-29 * time.Second), ok: true},
		"iat in the future":           {token: edsa(good()), at: now.Add(-31 * time.Second)},
		"no exp":                      {token: edsa(with(good(), "exp", nil)), at: now},
		"no iat":                      {token: edsa(with(good(), "iat", nil)), at: now},
		"lives longer than 5m":        {token: edsa(with(good(), "exp", now.Add(5*time.Minute+time.Second).Unix())), at: now},
		"an empty sub":                {token: edsa(with(good(), "sub", "")), at: now},
		"no sub":                      {token: edsa(with(good(), "sub", nil)), at: now},
		"an unknown kid":              {token: sign(t, jose.EdDSA, stranger, kid(t, stranger.Public()), good()), at: now},
		"a known kid, another key":    {token: sign(t, jose.EdDSA, stranger, kidOK, good()), at: now},
		"ES256":                       {token: sign(t, jose.ES256, ec, kidOK, good()), at: now},
		"alg none":                    {token: none(), at: now},
		"a tampered payload":          {token: tampered(), at: now},
		"not a token":                 {token: "not.a.token", at: now},
		"a JWS in JSON serialization": {token: `{"payload":"e30","signatures":[]}`, at: now},
		"iss is another module":       {token: edsa(with(good(), "iss", "web")), at: now},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			c, err := caller.Verify(ks, "api", tc.token, self, tc.at)
			switch {
			case tc.ok && err != nil:
				t.Errorf("refused: %v", err)
			case tc.ok && (c.Issuer != "api" || c.User != "ada" || !slices.Equal(c.Groups, []string{"admins"})):
				t.Errorf("caller = %+v", c)
			case !tc.ok && !errors.Is(err, caller.ErrInvalidToken):
				t.Errorf("err = %v, want ErrInvalidToken", err)
			}
		})
	}
}

// TestKeyFiles covers the key file formats: public keys as PKIX PEM, one or more; the signing key
// as PKCS 8 PEM; anything else is an error that never carries the key.
func TestKeyFiles(t *testing.T) {
	t.Parallel()
	a, b := newKey(t), newKey(t)
	if k, err := caller.KeysOf("api", publicPEM(t, a, b)); err != nil || k.Len() != 1 {
		t.Errorf("two keys: %d, %v", k.Len(), err)
	}
	ec, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	ecPub, _ := x509.MarshalPKIXPublicKey(ec.Public())
	ecPriv, _ := x509.MarshalPKCS8PrivateKey(ec)
	edPriv, _ := x509.MarshalPKCS8PrivateKey(a)
	for name, data := range map[string][]byte{
		"empty":            nil,
		"not PEM":          []byte("hello"),
		"an ECDSA key":     pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: ecPub}),
		"a private key":    pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: edPriv}),
		"a broken block":   pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: []byte("x")}),
		"one good, one EC": append(publicPEM(t, a), pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: ecPub})...),
	} {
		if _, err := caller.KeysOf("api", data); err == nil {
			t.Errorf("KeysOf(%s) passed", name)
		}
	}

	if k, err := caller.ParseSigningKey(pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: edPriv})); err != nil || !k.Equal(a) {
		t.Errorf("ParseSigningKey = %v", err)
	}
	for name, data := range map[string][]byte{
		"empty":          nil,
		"a public key":   publicPEM(t, a),
		"an ECDSA key":   pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: ecPriv}),
		"a broken block": pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: []byte("x")}),
	} {
		_, err := caller.ParseSigningKey(data)
		if err == nil {
			t.Errorf("ParseSigningKey(%s) passed", name)
		} else if strings.Contains(err.Error(), base64.StdEncoding.EncodeToString(edPriv)[:16]) {
			t.Errorf("ParseSigningKey(%s): the error shows the key", name)
		}
	}
}

// TestSignerHidesKey covers the signer: it mints as its module whatever Issuer it is given, and
// fmt and slog show the issuer only.
func TestSignerHidesKey(t *testing.T) {
	t.Parallel()
	key := newKey(t)
	s := caller.NewSigner(key, "api")
	token, err := s.Mint(caller.Caller{Issuer: "someone-else", User: "ada"}, self, now)
	if err != nil {
		t.Fatal(err)
	}
	if c, err := caller.Verify(keys(t, key), "api", token, self, now); err != nil || c.Issuer != "api" {
		t.Errorf("Verify = %+v, %v; want iss api", c, err)
	}
	var logs bytes.Buffer
	slog.New(slog.NewJSONHandler(&logs, nil)).Info("x", slog.Any("signer", s))
	for _, out := range []string{fmtAll(s), logs.String()} {
		if strings.Contains(out, string(key)) || strings.Contains(out, base64.StdEncoding.EncodeToString(key)) || !strings.Contains(out, "api") {
			t.Errorf("output = %q", out)
		}
	}
}

func fmtAll(v any) string {
	var b strings.Builder
	for _, f := range []string{"%v", "%s", "%+v", "%#v"} {
		fmt.Fprintf(&b, f, v)
	}
	return b.String()
}

// middleware is the provider's chain: binding token auth for the consumers in tokens, then the
// caller check with ks; the handler answers with the caller it saw, or "none". anon skips auth.
func middleware(t *testing.T, ks caller.Keys, tokens map[string]string, anon bool) (http.Handler, *bytes.Buffer) {
	t.Helper()
	var logs bytes.Buffer
	log := slog.New(slog.NewJSONHandler(&logs, nil))
	h := caller.Middleware(ks, self, log, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c, ok := caller.From(r.Context())
		if !ok {
			_, _ = io.WriteString(w, "none")
			return
		}
		_ = json.NewEncoder(w).Encode(c)
	}))
	if anon {
		return h, &logs
	}
	dir := t.TempDir()
	for name, token := range tokens {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(token), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	clients, errs := auth.LoadClients(dir)
	if len(errs) > 0 {
		t.Fatal(errs)
	}
	return auth.Middleware(clients, h), &logs
}

const (
	apiToken  = "api-token-0123456789abcdef0123456789abcdef"
	toolToken = "tool-token-0123456789abcdef0123456789abcdef"
)

func call(h http.Handler, bearer string, callers ...string) (int, string) {
	r := httptest.NewRequest(http.MethodGet, "/x", nil)
	if bearer != "" {
		r.Header.Set("Authorization", "Bearer "+bearer)
	}
	for _, c := range callers {
		r.Header.Add(caller.Header, c)
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	return w.Code, w.Body.String()
}

// TestMiddleware covers the MP-Caller check on a request (AC-11): a valid token from the module
// that minted it puts the caller into ctx; without the header there is no caller and no error;
// a token is refused with 401 invalid_token when the header appears twice, when the call passed
// no binding token auth, when its iss is not the calling module, or when Verify refuses it. The
// reason is logged, never the token.
func TestMiddleware(t *testing.T) {
	t.Parallel()
	key := newKey(t)
	ks := keys(t, key)
	tokens := map[string]string{"api": apiToken, "tool": toolToken}
	h, logs := middleware(t, ks, tokens, false)
	mint := func(iss, aud string) string {
		tok, err := caller.Mint(key, caller.Caller{Issuer: iss, User: "ada", Groups: []string{"admins"}}, aud, time.Now())
		if err != nil {
			t.Fatal(err)
		}
		return tok
	}
	good := mint("api", self)

	if code, body := call(h, apiToken, good); code != http.StatusOK || !strings.Contains(body, `"User":"ada"`) || !strings.Contains(body, `"Issuer":"api"`) {
		t.Errorf("a valid token = %d %s", code, body)
	}
	if code, body := call(h, apiToken); code != http.StatusOK || body != "none" {
		t.Errorf("no header = %d %s", code, body)
	}
	for name, tc := range map[string]struct {
		h       http.Handler
		bearer  string
		callers []string
	}{
		"the header twice":         {h, apiToken, []string{good, good}},
		"another module passes it": {h, toolToken, []string{good}},
		"another aud":              {h, apiToken, []string{mint("api", "tool.stack.internal")}},
		"garbage":                  {h, apiToken, []string{"garbage"}},
	} {
		code, body := call(tc.h, tc.bearer, tc.callers...)
		if code != http.StatusUnauthorized || !strings.Contains(body, `"invalid_token"`) {
			t.Errorf("%s = %d %s, want 401 invalid_token", name, code, body)
		}
	}
	if strings.Contains(logs.String(), good) || strings.Count(logs.String(), "caller token refused") != 4 {
		t.Errorf("want 4 refusals logged without the token\n%s", logs)
	}

	anon, _ := middleware(t, ks, nil, true)
	if code, body := call(anon, "", good); code != http.StatusUnauthorized || !strings.Contains(body, `"invalid_token"`) {
		t.Errorf("no binding token auth = %d %s, want 401", code, body)
	}
	if code, body := call(anon, ""); code != http.StatusOK || body != "none" {
		t.Errorf("no binding token auth, no header = %d %s", code, body)
	}
}

// TestMiddlewareWithoutKeys covers MP_CALLER_KEY_FILE unset: the header is ignored (no caller, no
// error) and logged once.
func TestMiddlewareWithoutKeys(t *testing.T) {
	t.Parallel()
	h, logs := middleware(t, caller.Keys{}, map[string]string{"api": apiToken}, false)
	tok, _ := caller.Mint(newKey(t), caller.Caller{Issuer: "api", User: "ada"}, self, time.Now())
	for range 3 {
		if code, body := call(h, apiToken, tok); code != http.StatusOK || body != "none" {
			t.Errorf("= %d %s, want 200 with no caller", code, body)
		}
	}
	if n := strings.Count(logs.String(), "caller token ignored"); n != 1 {
		t.Errorf("%d ignored lines, want 1\n%s", n, logs)
	}
}

// TestLoadKeys covers the caller keys directory (spec 0015): one file per consumer, the entry
// rules of the token clients directory, and a token checked only against its own consumer's keys
// (AC-18).
func TestLoadKeys(t *testing.T) {
	t.Parallel()
	apiKey, webKey := newKey(t), newKey(t)
	dir := t.TempDir()
	write := func(name string, data []byte) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(dir, name), data, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	write("api", publicPEM(t, apiKey))
	write("web", publicPEM(t, webKey))
	write(".hidden", []byte("skipped"))
	if err := os.Mkdir(filepath.Join(dir, "folder"), 0o700); err != nil {
		t.Fatal(err)
	}
	write("Not_A_Name", publicPEM(t, apiKey))
	write("broken", []byte("not PEM"))

	ks, errs := caller.LoadKeys(dir)
	if ks.Len() != 2 || len(errs) != 3 {
		t.Fatalf("LoadKeys = %d consumers, errors %v; want 2 consumers and 3 errors", ks.Len(), errs)
	}
	for _, err := range errs {
		if strings.Contains(err.Error(), "not PEM") {
			t.Errorf("an error shows a file's content: %v", err)
		}
	}
	token, err := caller.Mint(webKey, caller.Caller{Issuer: "api", User: "ada"}, self, now)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := caller.Verify(ks, "api", token, self, now); !errors.Is(err, caller.ErrInvalidToken) {
		t.Errorf("api's token signed with web's key: %v, want ErrInvalidToken", err)
	}
	if _, err := caller.Verify(ks, "web", token, self, now); !errors.Is(err, caller.ErrInvalidToken) {
		t.Errorf("a token whose iss is api, sent by web: %v, want ErrInvalidToken", err)
	}
	own, _ := caller.Mint(apiKey, caller.Caller{Issuer: "api", User: "ada"}, self, now)
	if c, err := caller.Verify(ks, "api", own, self, now); err != nil || c.User != "ada" {
		t.Errorf("api's own token = %+v, %v", c, err)
	}
	if _, errs := caller.LoadKeys(filepath.Join(dir, "missing")); len(errs) != 1 {
		t.Errorf("a missing directory: %v, want one error", errs)
	}
}

// TestGenerateKeyPair covers the pair api's setup step writes: each half parses as the other side
// reads it, and a token minted with one verifies with the other.
func TestGenerateKeyPair(t *testing.T) {
	t.Parallel()
	signing, public, err := caller.GenerateKeyPair()
	if err != nil {
		t.Fatal(err)
	}
	key, err := caller.ParseSigningKey(signing)
	if err != nil {
		t.Fatal(err)
	}
	ks, err := caller.KeysOf("api", public)
	if err != nil {
		t.Fatal(err)
	}
	token, _ := caller.Mint(key, caller.Caller{Issuer: "api", User: "ada"}, self, now)
	if _, err := caller.Verify(ks, "api", token, self, now); err != nil {
		t.Errorf("Verify: %v", err)
	}
}
