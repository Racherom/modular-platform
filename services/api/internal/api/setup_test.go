package api

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io/fs"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/racherom/modular-platform/sdk"
	"github.com/racherom/modular-platform/services/api/internal/config"
)

// settingsEnv is a start env of api with every binding set, plus over; an empty value in over
// removes the key.
func settingsEnv(t *testing.T, over map[string]string) map[string]string {
	t.Helper()
	secret := func(content string) string {
		path := filepath.Join(t.TempDir(), "secret")
		if err := os.WriteFile(path, []byte(content+"\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		return path
	}
	token := strings.Repeat("t", 32)
	env := map[string]string{
		"MP_INSTANCE":                 "api",
		"MP_DATABASE_URL":             "postgres://postgres.test:5432",
		"MP_DATABASE_NAME":            "platform_api",
		"MP_DATABASE_USER":            "api",
		"MP_DATABASE_PASSWORD_FILE":   secret("pw"),
		"MP_RENDERER_URL":             "http://renderer.test:8080",
		"MP_RENDERER_TOKEN_FILE":      secret(token),
		"MP_DEPLOY_TARGET_URL":        "http://deployer.test:8080",
		"MP_DEPLOY_TARGET_TOKEN_FILE": secret(token),
		"MP_PUBLIC_ORIGIN":            "https://platform.test",
		"MP_OIDC_ISSUER":              "https://platform.test/auth",
		"MP_OIDC_CLIENT_ID":           "api",
		"MP_OIDC_CLIENT_SECRET_FILE":  secret("client-secret"),
		"MP_ADMIN_EMAIL":              "admin@platform.test",
	}
	for k, v := range over {
		if v == "" {
			delete(env, k)
			continue
		}
		env[k] = v
	}
	return env
}

// startSettings starts api's module with env and runs readSettings as its setup.
func startSettings(t *testing.T, env map[string]string) (settings, []string, error) {
	t.Helper()
	var (
		logs bytes.Buffer
		s    settings
	)
	_, err := sdk.Build(t.Context(), config.Module, func(ctx context.Context, m *sdk.Module[config.Config, config.Uses]) (http.Handler, error) {
		var err error
		s, err = readSettings(ctx, m)
		return http.NotFoundHandler(), err
	}, sdk.WithEnv(env), sdk.WithLogger(slog.New(slog.NewJSONHandler(&logs, nil))))
	var lines []string
	for line := range strings.SplitSeq(logs.String(), "\n") {
		var entry struct{ Level, Msg string }
		if json.Unmarshal([]byte(line), &entry) == nil && entry.Level == "ERROR" {
			lines = append(lines, entry.Msg)
		}
	}
	return s, lines, err
}

// TestReadSettings covers api's part of spec 0025 AC-24: origin and base path come from the
// public binding, the OIDC settings from the oidc binding, and every env name is the one api read
// before; at the site root MP_BASE_PATH is empty and api starts with no base path.
func TestReadSettings(t *testing.T) {
	t.Parallel()
	s, lines, err := startSettings(t, settingsEnv(t, map[string]string{"MP_BASE_PATH": ""}))
	if err != nil {
		t.Fatalf("start refused: %v %q", err, lines)
	}
	switch {
	case s.PublicOrigin != "https://platform.test", s.BasePath != "", s.AdminEmail != "admin@platform.test":
		t.Errorf("settings = %q %q %q", s.PublicOrigin, s.BasePath, s.AdminEmail)
	case s.oidc.Issuer != "https://platform.test/auth", s.oidc.ClientID != "api", s.oidc.ClientSecret != "client-secret":
		t.Errorf("oidc = %q %q, secret right: %v", s.oidc.Issuer, s.oidc.ClientID, s.oidc.ClientSecret == "client-secret")
	case s.oidc.RedirectURL != "https://platform.test/api/v1/auth/callback", s.oidc.CA != nil:
		t.Errorf("redirect %q, CA %v", s.oidc.RedirectURL, s.oidc.CA != nil)
	}

	s, lines, err = startSettings(t, settingsEnv(t, map[string]string{"MP_BASE_PATH": "/platform"}))
	if err != nil || s.BasePath != "/platform" || s.oidc.RedirectURL != "https://platform.test/platform/api/v1/auth/callback" {
		t.Errorf("under /platform: base %q, redirect %q, %v %q", s.BasePath, s.oidc.RedirectURL, err, lines)
	}
}

// TestReadSettingsRefused covers the refused starts of AC-24: an unset binding field is the sdk's
// missing line, and the rules api keeps for its bindings stop the start with one line each.
func TestReadSettingsRefused(t *testing.T) {
	t.Parallel()
	for name, tc := range map[string]struct {
		over map[string]string
		want string
	}{
		"no issuer":         {map[string]string{"MP_OIDC_ISSUER": ""}, "config: uses.oidc.issuer (MP_OIDC_ISSUER): missing: "},
		"no client id":      {map[string]string{"MP_OIDC_CLIENT_ID": ""}, "config: uses.oidc.client_id (MP_OIDC_CLIENT_ID): missing: "},
		"no origin":         {map[string]string{"MP_PUBLIC_ORIGIN": ""}, "config: uses.public.origin (MP_PUBLIC_ORIGIN): missing: "},
		"an origin path":    {map[string]string{"MP_PUBLIC_ORIGIN": "https://platform.test/x"}, "config: uses.public.origin (MP_PUBLIC_ORIGIN): invalid: "},
		"a bad base path":   {map[string]string{"MP_BASE_PATH": "/platform/"}, "config: uses.public.base_path (MP_BASE_PATH): no_match: "},
		"no admin email":    {map[string]string{"MP_ADMIN_EMAIL": ""}, "config: uses.oidc.admin_email (MP_ADMIN_EMAIL): missing: "},
		"a CA that is text": {map[string]string{"MP_OIDC_CA_FILE": "/no/such/ca.pem"}, "config: uses.oidc.ca (MP_OIDC_CA_FILE): invalid: "},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			_, lines, err := startSettings(t, settingsEnv(t, tc.over))
			if !errors.Is(err, sdk.ErrStart) {
				t.Fatalf("err = %v, want ErrStart", err)
			}
			if len(lines) != 1 || !strings.HasPrefix(lines[0], tc.want) {
				t.Errorf("error lines = %q, want one starting %q", lines, tc.want)
			}
		})
	}
}

// oldClientID matches the client id api's entry defaulted to before spec 0025, as a whole word
// (mp-api-root, a meta name of the editor, is another word).
var oldClientID = regexp.MustCompile(`(^|[^a-z0-9-])` + "mp" + `-api($|[^a-z0-9-])`)

// TestNoOldClientID covers task 4 of spec 0025: the rendered client id is api, and the old
// default left with the hand written deploy/managed folder (spec 0011, build task 19).
func TestNoOldClientID(t *testing.T) {
	t.Parallel()
	root := filepath.Join("..", "..", "..", "..")
	skip := map[string]bool{".git": true, "node_modules": true, "docs": true, "build": true, "dist": true, ".agents": true, ".claude": true}
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		switch {
		case err != nil:
			return err
		case d.IsDir() && skip[d.Name()]:
			return filepath.SkipDir
		case d.IsDir() || !d.Type().IsRegular():
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if !bytes.ContainsRune(data, 0) && oldClientID.Match(data) {
			t.Errorf("%s names the old OIDC client id", path)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}
