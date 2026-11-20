package setupsession_test

import (
	"errors"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/racherom/modular-platform/libs/setupsession"
)

var (
	key   = []byte("0123456789abcdef0123456789abcdef")
	other = []byte("fedcba9876543210fedcba9876543210")
	t0    = time.Date(2026, 11, 20, 12, 0, 0, 0, time.UTC)
)

func TestMintVerify(t *testing.T) {
	value, expiry := setupsession.Mint(key, t0)
	if want := t0.Add(setupsession.Lifetime); !expiry.Equal(want) {
		t.Fatalf("expiry = %s, want %s", expiry, want)
	}
	got, err := setupsession.Verify(key, value, t0.Add(time.Hour))
	if err != nil {
		t.Fatalf("Verify: %v", err)
	}
	if !got.Equal(expiry) {
		t.Errorf("expiry = %s, want %s", got, expiry)
	}
}

func TestVerifyRefusals(t *testing.T) {
	value, expiry := setupsession.Mint(key, t0)
	parts := strings.Split(value, ".")
	tests := []struct {
		name    string
		key     []byte
		value   string
		now     time.Time
		expired bool
	}{
		{"another key", other, value, t0, false},
		{"empty key", nil, value, t0, false},
		{"past expiry", key, value, expiry.Add(time.Second), true},
		{"at expiry", key, value, expiry, true},
		{"empty", key, "", t0, false},
		{"garbage", key, "not a session", t0, false},
		{"two parts", key, parts[0] + "." + parts[1], t0, false},
		{"four parts", key, value + ".x", t0, false},
		{"other version", key, "v2." + parts[1] + "." + parts[2], t0, false},
		{"changed expiry", key, parts[0] + ".9999999999." + parts[2], t0, false},
		{"leading zero expiry", key, parts[0] + ".0" + parts[1] + "." + parts[2], t0, false},
		{"non number expiry", key, parts[0] + ".soon." + parts[2], t0, false},
		{"bad mac text", key, parts[0] + "." + parts[1] + ".!!!", t0, false},
		{"short mac", key, parts[0] + "." + parts[1] + "." + parts[2][:10], t0, false},
		{"empty mac", key, parts[0] + "." + parts[1] + ".", t0, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := setupsession.Verify(tt.key, tt.value, tt.now)
			if !errors.Is(err, setupsession.ErrInvalid) {
				t.Fatalf("err = %v, want ErrInvalid", err)
			}
			if got := errors.Is(err, setupsession.ErrExpired); got != tt.expired {
				t.Errorf("ErrExpired = %t, want %t", got, tt.expired)
			}
		})
	}
}

func TestMintUntilKeepsTheEnd(t *testing.T) {
	_, expiry := setupsession.Mint(key, t0)
	value := setupsession.MintUntil(key, expiry)
	got, err := setupsession.Verify(key, value, t0)
	if err != nil {
		t.Fatalf("Verify: %v", err)
	}
	if !got.Equal(expiry) {
		t.Errorf("expiry = %s, want %s", got, expiry)
	}
}

func TestCookieName(t *testing.T) {
	tests := []struct{ base, name, path string }{
		{"", "__Host-mp_setup", "/"},
		{"/platform", "__Secure-mp_setup", "/platform/"},
	}
	for _, tt := range tests {
		if got := setupsession.CookieName(tt.base); got != tt.name {
			t.Errorf("CookieName(%q) = %q, want %q", tt.base, got, tt.name)
		}
		if got := setupsession.CookiePath(tt.base); got != tt.path {
			t.Errorf("CookiePath(%q) = %q, want %q", tt.base, got, tt.path)
		}
	}
}

func TestHandoff(t *testing.T) {
	now := time.Unix(1_800_000_000, 0)
	sess := now.Add(time.Hour)
	tok := setupsession.MintHandoff(key, "auth.example.org", sess, now, "n1")

	h, err := setupsession.VerifyHandoff(key, "auth.example.org", tok, now.Add(59*time.Second))
	if err != nil || h.Nonce != "n1" || !h.SessionExpiry.Equal(sess) || !h.Expiry.Equal(now.Add(setupsession.HandoffLifetime)) {
		t.Fatalf("valid token: %+v, %v", h, err)
	}

	tests := []struct {
		name, host, token string
		key               []byte
		at                time.Time
		expired           bool
	}{
		{"other host", "evil.example.org", tok, key, now, false},
		{"other key", "auth.example.org", tok, other, now, false},
		{"no key", "auth.example.org", tok, nil, now, false},
		{"expired", "auth.example.org", tok, key, now.Add(setupsession.HandoffLifetime), true},
		{"session over", "auth.example.org", setupsession.MintHandoff(key, "auth.example.org", now.Add(10*time.Second), now, "n2"), key, now.Add(10 * time.Second), true},
		{"garbage", "auth.example.org", "h1.x", key, now, false},
		{"empty nonce", "auth.example.org", setupsession.MintHandoff(key, "auth.example.org", sess, now, ""), key, now, false},
		{"session value", "auth.example.org", setupsession.MintUntil(key, sess), key, now, false},
		{"tampered expiry", "auth.example.org", strings.Replace(tok, strconv.FormatInt(now.Add(setupsession.HandoffLifetime).Unix(), 10), strconv.FormatInt(now.Add(time.Hour).Unix(), 10), 1), key, now, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := setupsession.VerifyHandoff(tt.key, tt.host, tt.token, tt.at)
			if !errors.Is(err, setupsession.ErrInvalid) || errors.Is(err, setupsession.ErrExpired) != tt.expired {
				t.Fatalf("err = %v, want invalid (expired %v)", err, tt.expired)
			}
		})
	}
}
