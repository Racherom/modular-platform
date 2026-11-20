package sdk_test

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/racherom/modular-platform/sdk"
)

// setupConfig is the Config of a module with lifetime: setup files, as sdkgen writes it.
type setupConfig struct{ v *sdk.Values }

func (c setupConfig) SetupCode(ctx context.Context) (sdk.Secret, bool, error) {
	return sdk.SetupFile(ctx, c.v, "code")
}

func (c setupConfig) Undeclared(ctx context.Context) (sdk.Secret, bool, error) {
	return sdk.SetupFile(ctx, c.v, "other")
}

var setupModule = sdk.Declaration[setupConfig, struct{}]{
	Name:       "holder",
	SetupFiles: []string{"code", "session-key"},
	New:        func(v *sdk.Values, _ *sdk.Bindings) (setupConfig, struct{}) { return setupConfig{v}, struct{}{} },
}

func startSetupModule(t *testing.T, env map[string]string) (setupConfig, *bytes.Buffer, error) {
	t.Helper()
	var (
		logs bytes.Buffer
		cfg  setupConfig
	)
	_, err := sdk.Build(t.Context(), setupModule, func(_ context.Context, m *sdk.Module[setupConfig, struct{}]) (http.Handler, error) {
		cfg = m.Config
		return http.NotFoundHandler(), nil
	}, sdk.WithEnv(env), sdk.WithLogger(slog.New(slog.NewJSONHandler(&logs, nil))))
	return cfg, &logs, err
}

// TestSetupFile covers spec 0036's setup getters: the file is read at every call, a missing or
// empty file is absent, and nothing but the declared names is readable.
func TestSetupFile(t *testing.T) {
	dir := t.TempDir()
	cfg, _, err := startSetupModule(t, map[string]string{"MP_INSTANCE": "holder", "MP_SETUP_DIR": dir})
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	ctx := t.Context()
	read := func() (string, bool) {
		t.Helper()
		s, ok, err := cfg.SetupCode(ctx)
		if err != nil {
			t.Fatalf("SetupCode: %v", err)
		}
		return s.Reveal(), ok
	}

	if _, ok := read(); ok {
		t.Error("a missing file is present")
	}
	write := func(text string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(dir, "code"), []byte(text), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	write("first-code\n")
	if got, ok := read(); !ok || got != "first-code" {
		t.Errorf("code = %q, %v; want first-code, true", got, ok)
	}
	write("second-code")
	if got, ok := read(); !ok || got != "second-code" {
		t.Errorf("after a new code: %q, %v; want second-code, true", got, ok)
	}
	write("\n")
	if _, ok := read(); ok {
		t.Error("an empty file is present")
	}
	write("third")
	if err := os.Remove(filepath.Join(dir, "code")); err != nil {
		t.Fatal(err)
	}
	if _, ok := read(); ok {
		t.Error("a deleted file is present")
	}
	if _, _, err := cfg.Undeclared(ctx); err == nil {
		t.Error("a name the entry does not declare was read")
	}
}

// TestSetupFileUnreadable names the file, never its content.
func TestSetupFileUnreadable(t *testing.T) {
	dir := t.TempDir()
	if err := os.Mkdir(filepath.Join(dir, "code"), 0o700); err != nil { // a folder where the file belongs
		t.Fatal(err)
	}
	cfg, _, err := startSetupModule(t, map[string]string{"MP_INSTANCE": "holder", "MP_SETUP_DIR": dir})
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	if _, ok, err := cfg.SetupCode(t.Context()); err == nil || ok {
		t.Errorf("SetupCode = %v, %v; want an error", ok, err)
	}
}

// TestSetupDirRules refuses a start without MP_SETUP_DIR, or with a relative one, for a module
// that has setup files.
func TestSetupDirRules(t *testing.T) {
	tests := map[string]struct {
		env  map[string]string
		want string
	}{
		"unset":    {map[string]string{"MP_INSTANCE": "holder"}, ": missing:"},
		"relative": {map[string]string{"MP_INSTANCE": "holder", "MP_SETUP_DIR": "run/setup"}, ": invalid:"},
	}
	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			_, logs, err := startSetupModule(t, tc.env)
			if !errors.Is(err, sdk.ErrStart) {
				t.Fatalf("err = %v, want ErrStart", err)
			}
			if !strings.Contains(logs.String(), "MP_SETUP_DIR") || !strings.Contains(logs.String(), tc.want) {
				t.Errorf("logs = %s, want a line for MP_SETUP_DIR with %q", logs.String(), tc.want)
			}
		})
	}
}
