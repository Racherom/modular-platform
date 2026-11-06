package sdk_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"maps"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/racherom/modular-platform/sdk"
	"github.com/racherom/modular-platform/sdk/internal/testmodule"
	"github.com/racherom/modular-platform/sdk/internal/testpki"
	"github.com/racherom/modular-platform/sdk/sdktest"
)

const realmKey = "realm-key-content"

// realmEnv sets every required field of the realm slot, plus over.
func realmEnv(t *testing.T, ca []byte, over map[string]string) map[string]string {
	t.Helper()
	env := map[string]string{
		"TM_REALM_NAME":     "north",
		"TM_REALM_PORT":     "8443",
		"TM_REALM_SECURE":   "true",
		"TM_REALM_ISSUER":   "https://issuer.test/realm",
		"TM_REALM_MODE":     "closed",
		"TM_REALM_KEY_FILE": file(t, realmKey+"\n"),
		"TM_REALM_CA_FILE":  file(t, string(ca)),
	}
	maps.Copy(env, over)
	return env
}

// TestFieldBinding covers AC-22: the getter of a slot with no client returns one typed value per
// mapped field, an optional field not delivered is nil, an optional slot with nothing set is
// unbound, and the config loaded line names each field with source binding or unset.
func TestFieldBinding(t *testing.T) {
	t.Parallel()
	ca := testpki.New(t).PEM()
	uses, logs, err := consumerUses(t, realmEnv(t, ca, map[string]string{
		"TM_REALM_HINT": "by the sea",
		"TM_REALM_TIER": "paid",
		"TM_REALM_HOME": "https://north.test",
	}))
	if err != nil {
		t.Fatalf("start refused: %v\n%s", err, logs)
	}
	r, bound, err := uses.Realm(t.Context())
	if err != nil || !bound {
		t.Fatalf("Realm() = bound %v, %v", bound, err)
	}
	switch {
	case r.Name != "north", r.Port != 8443, !r.Secure, r.Mode != testmodule.RealmModeClosed:
		t.Errorf("text fields = %q %d %v %q", r.Name, r.Port, r.Secure, r.Mode)
	case r.Issuer == nil || r.Issuer.String() != "https://issuer.test/realm":
		t.Errorf("issuer = %v", r.Issuer)
	case r.Key.Reveal() != realmKey:
		t.Error("the key is not the file's content, trimmed")
	case !bytes.Equal(r.Ca, ca):
		t.Error("the CA is not the file's PEM")
	case r.Hint == nil || *r.Hint != "by the sea", r.Tier == nil || *r.Tier != testmodule.RealmTierPaid:
		t.Errorf("optional fields set = %v %v", r.Hint, r.Tier)
	case r.Home == nil || r.Home.Host != "north.test":
		t.Errorf("home = %v", r.Home)
	case r.Retries != nil, r.Strict != nil, r.Pin != nil, r.Roots != nil:
		t.Errorf("optional fields not delivered = %v %v %v %v, want nil", r.Retries, r.Strict, r.Pin, r.Roots)
	}
	if strings.Contains(logs.String(), realmKey) || strings.Contains(logs.String(), "BEGIN CERTIFICATE") {
		t.Error("a secret or certificate's content reached the log")
	}
	var loaded struct {
		Values map[string]string `json:"values"`
	}
	for line := range strings.SplitSeq(logs.String(), "\n") {
		if strings.Contains(line, `"msg":"config loaded"`) {
			loaded.Values = jsonGroup(t, line, "values")
		}
	}
	for field, want := range map[string]string{"uses.realm.name": "binding", "uses.realm.key": "binding", "uses.realm.hint": "binding", "uses.realm.pin": "unset"} {
		if got := loaded.Values[field]; got != want {
			t.Errorf("config loaded %s = %q, want %q", field, got, want)
		}
	}

	unbound, logs, err := consumerUses(t, map[string]string{"TM_REALM_NAME": ""})
	if err != nil {
		t.Fatalf("start refused: %v\n%s", err, logs)
	}
	if _, bound, err := unbound.Realm(t.Context()); bound || err != nil {
		t.Errorf("unbound Realm() = bound %v, %v; want false, nil", bound, err)
	}
	if strings.Contains(logs.String(), "uses.realm.") {
		t.Error("the config loaded line names fields of an unbound slot")
	}
}

// TestFieldBindingProblems covers AC-23: a bound slot with a required field unset, a value its
// type refuses, a pattern or values miss, an unreadable secret file and a certificate file with
// no certificate each stop the start with one line, in the sorted problem order.
func TestFieldBindingProblems(t *testing.T) {
	t.Parallel()
	env := realmEnv(t, testpki.New(t).PEM(), map[string]string{
		"TM_REALM_ISSUER":   "",
		"TM_REALM_PORT":     "abc",
		"TM_REALM_MODE":     "ajar",
		"TM_REALM_NAME":     "North",
		"TM_REALM_KEY_FILE": "/no/such/key",
		"TM_REALM_CA_FILE":  file(t, "not a certificate\n"),
	})
	base := baseEnv(t)
	maps.Copy(base, env)
	_, logs, err := start(t, base)
	if !errors.Is(err, sdk.ErrStart) {
		t.Fatalf("err = %v, want ErrStart", err)
	}
	want := []string{
		"config: uses.realm.ca (TM_REALM_CA_FILE): invalid: the file " + env["TM_REALM_CA_FILE"] + " holds no PEM certificate",
		"config: uses.realm.issuer (TM_REALM_ISSUER): missing: a value is required; set TM_REALM_ISSUER",
		"config: uses.realm.key (TM_REALM_KEY_FILE): invalid: the file /no/such/key cannot be read",
		`config: uses.realm.mode (TM_REALM_MODE): not_allowed: "ajar" is not one of open, closed`,
		"config: uses.realm.name (TM_REALM_NAME): no_match: does not match the pattern [a-z]+",
		`config: uses.realm.port (TM_REALM_PORT): invalid: "abc" is not a whole number`,
	}
	if got := errorMsgs(t, logs); !slices.Equal(got, want) {
		t.Errorf("problems =\n%s\nwant\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
	if strings.Contains(logs.String(), "config loaded") {
		t.Error("config loaded after a refused start")
	}

	// an empty secret file is missing, not invalid
	base = baseEnv(t)
	maps.Copy(base, realmEnv(t, testpki.New(t).PEM(), map[string]string{"TM_REALM_KEY_FILE": file(t, " \n")}))
	_, logs, _ = start(t, base)
	if got := errorMsgs(t, logs); len(got) != 1 || !strings.HasPrefix(got[0], "config: uses.realm.key (TM_REALM_KEY_FILE): missing: the file ") {
		t.Errorf("empty key file: %q", got)
	}
}

// TestFieldBindingResolver covers the per caller part of AC-23: a resolver answer replaces the
// slot's fields for that call and is kept; a bad answer is an error, never a partial value; a
// slow resolver is ErrCallerSource.
func TestFieldBindingResolver(t *testing.T) {
	t.Parallel()
	ca := testpki.New(t).PEM()
	issuer := must[*url.URL](t)(url.Parse("https://other.test"))
	good := sdk.Binding{Fields: map[string]any{
		"name": "south", "port": 9443, "secure": false, "issuer": issuer, "mode": "open",
		"key": sdk.NewSecret("south-key"), "ca": ca, "retries": 3,
	}}
	bad := func(field string, v any) sdk.Binding {
		f := maps.Clone(good.Fields)
		if v == nil {
			delete(f, field)
		} else {
			f[field] = v
		}
		return sdk.Binding{Fields: f}
	}
	resolver := &sdktest.Resolver{Bindings: map[string]map[string]sdk.Binding{
		"alice":   {"realm": good},
		"no-port": {"realm": bad("port", nil)},
		"text":    {"realm": bad("port", "9443")},
		"pattern": {"realm": bad("name", "South")},
		"enum":    {"realm": bad("mode", "ajar")},
		"cert":    {"realm": bad("ca", []byte("plain"))},
		"extra":   {"realm": bad("member", "me")},
	}}
	b, err := buildWith(t, withBase(t, realmEnv(t, ca, nil)), sdk.WithBindingResolver(resolver))
	if err != nil {
		t.Fatalf("start refused: %v\n%s", err, b.logs)
	}
	ctx := t.Context()
	r, _, err := b.uses.Realm(asCaller(ctx, "alice"))
	if err != nil || r.Name != "south" || r.Port != 9443 || r.Issuer != issuer || r.Retries == nil || *r.Retries != 3 || r.Key.Reveal() != "south-key" {
		t.Errorf("alice's realm = %+v, %v", r, err)
	}
	if r, _, _ := b.uses.Realm(asCaller(ctx, "bob")); r.Name != "north" {
		t.Errorf("bob's realm = %q, want the module's own north", r.Name)
	}
	_, _, _ = b.uses.Realm(asCaller(ctx, "alice"))
	if n := resolver.Calls(); n != 2 {
		t.Errorf("resolver calls = %d, want 2 (alice's answer kept)", n)
	}
	for _, user := range []string{"no-port", "text", "pattern", "enum", "cert", "extra"} {
		r, bound, err := b.uses.Realm(asCaller(ctx, user))
		if err == nil || errors.Is(err, sdk.ErrCallerSource) || bound || r.Name != "" {
			t.Errorf("%s: Realm() = %q bound %v, %v; want an error and no value", user, r.Name, bound, err)
		}
		if err != nil && strings.Contains(err.Error(), "south-key") {
			t.Errorf("%s: the error shows the key", user)
		}
	}

	slow, err := buildWith(t, withBase(t, realmEnv(t, ca, nil)), sdk.WithBindingResolver(&sdktest.Resolver{Delay: 2 * time.Second}))
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := slow.uses.Realm(asCaller(ctx, "alice")); !errors.Is(err, sdk.ErrCallerSource) {
		t.Errorf("a slow resolver: err = %v, want ErrCallerSource", err)
	}
}

// TestRequiredFieldBinding covers a required slot with no client: it starts and is ready with its
// fields set (there is no client to poll), and each unset required field is missing.
func TestRequiredFieldBinding(t *testing.T) {
	t.Parallel()
	d := testmodule.Module
	d.Bindings = slices.Clone(d.Bindings)
	for i := range d.Bindings {
		if d.Bindings[i].Capability == "realm" {
			d.Bindings[i].Optional = false
		}
	}
	var uses testmodule.Uses
	setup := func(_ context.Context, m *tmModule) (http.Handler, error) {
		uses = m.Uses
		return http.NotFoundHandler(), nil
	}
	h, err := sdk.Build(t.Context(), d, setup, sdk.WithEnv(withBase(t, realmEnv(t, testpki.New(t).PEM(), nil))), sdk.WithLogger(slog.New(slog.DiscardHandler)))
	if err != nil {
		t.Fatal(err)
	}
	if code := readyz(t, h); code != http.StatusOK {
		t.Errorf("/readyz = %d, want 200", code)
	}
	if r, _, err := uses.Realm(t.Context()); err != nil || r.Name != "north" {
		t.Errorf("Realm() = %q, %v", r.Name, err)
	}
	var logs bytes.Buffer
	_, err = sdk.Build(t.Context(), d, setup, sdk.WithEnv(baseEnv(t)), sdk.WithLogger(slog.New(slog.NewJSONHandler(&logs, nil))))
	if !errors.Is(err, sdk.ErrStart) {
		t.Fatalf("err = %v, want ErrStart", err)
	}
	if got := errorMsgs(t, &logs); len(got) != 7 || !strings.HasPrefix(got[0], "config: uses.realm.ca (TM_REALM_CA_FILE): missing: ") {
		t.Errorf("problems = %q, want one missing line per required field", got)
	}
}

// withBase adds env to baseEnv.
func withBase(t *testing.T, env map[string]string) map[string]string {
	t.Helper()
	base := baseEnv(t)
	maps.Copy(base, env)
	return base
}

// jsonGroup returns the string attributes of a group in one JSON log line.
func jsonGroup(t *testing.T, line, group string) map[string]string {
	t.Helper()
	var entry map[string]any
	if err := json.Unmarshal([]byte(line), &entry); err != nil {
		t.Fatal(err)
	}
	out := map[string]string{}
	if g, ok := entry[group].(map[string]any); ok {
		for k, v := range g {
			out[k] = fmt.Sprint(v)
		}
	}
	return out
}
