package render_test

import (
	"errors"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/compose-spec/compose-go/v2/loader"
	"github.com/compose-spec/compose-go/v2/types"

	"github.com/racherom/modular-platform/libs/contract"
	"github.com/racherom/modular-platform/libs/render"
)

// envCheckOutput renders the env-check case: one instance, ingress, of a module not built on
// the SDK, whose values have a default, a $ and a # in a default, an enum, an int range and an
// optional value without a default (spec 0025, AC-25 and AC-26).
func envCheckOutput(t *testing.T) render.Output {
	t.Helper()
	catalog := loadCatalog(t, "testdata/env-check/catalog")
	out, err := render.Render(planFile(t, "testdata/env-check/graph.yaml", catalog), catalog, contract.Templates{})
	if err != nil {
		t.Fatal(err)
	}
	return out
}

// TestRenderEnvCheckGolden pins every rendered file of the env-check case (AC-25, AC-26).
func TestRenderEnvCheckGolden(t *testing.T) {
	checkGolden(t, filepath.Join("testdata", "env-check", "golden"), envCheckOutput(t))
}

// TestNonSDKValuesReachTheService reads the rendered Compose file back as docker compose does:
// with no .env line each value is its default, $ and # included, and an optional value without
// one is set and empty; a .env line replaces the default (AC-25).
func TestNonSDKValuesReachTheService(t *testing.T) {
	compose := fileData(t, envCheckOutput(t), "docker-compose.yml")
	defaults := map[string]string{
		"GATE_BANNER":   "pay $5 # now",
		"GATE_NOTE":     "",
		"GATE_PORT":     "8080",
		"GATE_TLS_MODE": "internal",
	}
	for name, tc := range map[string]struct {
		env  map[string]string
		want map[string]string
	}{
		"defaults":        {map[string]string{"MP_UID": "1000", "MP_GID": "1000"}, defaults},
		"set in .env":     {map[string]string{"MP_UID": "1000", "MP_GID": "1000", "MP_INGRESS__TLS_MODE": "acme"}, with(defaults, "GATE_TLS_MODE", "acme")},
		"empty in .env":   {map[string]string{"MP_UID": "1000", "MP_GID": "1000", "MP_INGRESS__PORT": ""}, defaults},
		"optional in env": {map[string]string{"MP_UID": "1000", "MP_GID": "1000", "MP_INGRESS__NOTE": "hi"}, with(defaults, "GATE_NOTE", "hi")},
	} {
		t.Run(name, func(t *testing.T) {
			p, err := loader.LoadWithContext(t.Context(), types.ConfigDetails{
				ConfigFiles: []types.ConfigFile{{Filename: "docker-compose.yml", Content: compose}},
				Environment: tc.env,
			}, func(o *loader.Options) { o.SkipConsistencyCheck = true; o.ResolvePaths = false })
			if err != nil {
				t.Fatal(err)
			}
			env := p.Services["ingress"].Environment
			for k, want := range tc.want {
				if got := env[k]; got == nil || *got != want {
					t.Errorf("%s = %v, want %q", k, got, want)
				}
			}
		})
	}
}

func with(m map[string]string, k, v string) map[string]string {
	out := maps.Clone(m)
	out[k] = v
	return out
}

// TestSetupChecksEnvLines runs the rendered setup.sh over hand edited .env files: a bad line stops
// it with exit 1 before anything changes, a good one passes and changes nothing, comment lines
// are skipped, the last line of a name wins, the quotes setup.sh writes are stripped, an empty
// value passes only when optional, and a name of a known instance that no value declares warns
// and goes on (AC-26).
func TestSetupChecksEnvLines(t *testing.T) {
	if _, err := exec.LookPath("sh"); err != nil {
		t.Skip("no sh")
	}
	const ids = "MP_UID=1000\nMP_GID=1000\n"
	for name, tc := range map[string]struct {
		env      string // after the ids
		fail     string // the line setup.sh stops with, or empty when it passes
		warnings []string
	}{
		"bad enum":           {env: "MP_INGRESS__TLS_MODE=acmee\n", fail: "setup.sh: MP_INGRESS__TLS_MODE in .env: the answer is not one of: internal acme"},
		"good enum":          {env: "MP_INGRESS__TLS_MODE=acme\n"},
		"quoted by setup.sh": {env: "MP_INGRESS__TLS_MODE='acme'\n"},
		"commented bad line": {env: "# MP_INGRESS__TLS_MODE=acmee\n"},
		"bad then good":      {env: "MP_INGRESS__TLS_MODE=acmee\nMP_INGRESS__TLS_MODE=acme\n"},
		"good then bad":      {env: "MP_INGRESS__TLS_MODE=acme\nMP_INGRESS__TLS_MODE=acmee\n", fail: "setup.sh: MP_INGRESS__TLS_MODE in .env: the answer is not one of: internal acme"},
		"first bad line":     {env: "MP_INGRESS__PORT=0\nMP_INGRESS__TLS_MODE=acmee\n", fail: "setup.sh: MP_INGRESS__PORT in .env: the answer is below the minimum 1"},
		"not a number":       {env: "MP_INGRESS__PORT=80a\n", fail: "setup.sh: MP_INGRESS__PORT in .env: the answer is not a whole number"},
		"empty optional":     {env: "MP_INGRESS__NOTE=\n"},
		"empty required":     {env: "MP_INGRESS__PORT=\n", fail: "setup.sh: MP_INGRESS__PORT in .env: the answer is empty"},
		"unknown value":      {env: "MP_INGRESS__TLS_MDE=acme\n", warnings: []string{"setup.sh: warning: MP_INGRESS__TLS_MDE in .env names no value of ingress"}},
		"unknown instance":   {env: "MP_OTHER__TLS_MODE=acmee\n"},
	} {
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			writeOutput(t, dir, envCheckOutput(t))
			if err := os.WriteFile(filepath.Join(dir, ".env"), []byte(ids+tc.env), 0o644); err != nil {
				t.Fatal(err)
			}
			before := dirFiles(t, dir)
			out, err := runSetupSh(t, dir)
			for _, w := range tc.warnings {
				if !strings.Contains(out, w) {
					t.Errorf("want the warning %q:\n%s", w, out)
				}
			}
			if tc.fail != "" {
				var exit *exec.ExitError
				if !errors.As(err, &exit) || exit.ExitCode() != 1 || !strings.Contains(out, tc.fail) {
					t.Fatalf("want exit 1 with %q; err %v\n%s", tc.fail, err, out)
				}
			} else if err != nil {
				t.Fatalf("setup.sh: %v\n%s", err, out)
			}
			if !maps.Equal(before, dirFiles(t, dir)) {
				t.Errorf("setup.sh changed files:\n%s", out)
			}
			if tc.fail == "" { // a second run changes nothing either
				if out, err := runSetupSh(t, dir); err != nil || !maps.Equal(before, dirFiles(t, dir)) {
					t.Errorf("second run: %v\n%s", err, out)
				}
			}
		})
	}
}

// TestSetupWithoutEnvFile: with no .env yet, the check passes and setup.sh writes the ids (AC-26).
func TestSetupWithoutEnvFile(t *testing.T) {
	if _, err := exec.LookPath("sh"); err != nil {
		t.Skip("no sh")
	}
	dir := t.TempDir()
	out := envCheckOutput(t)
	out.Files = slicesWithout(out.Files, ".env")
	writeOutput(t, dir, out)
	if got, err := runSetupSh(t, dir); err != nil {
		t.Fatalf("setup.sh: %v\n%s", err, got)
	}
}

func slicesWithout(files []render.File, path string) []render.File {
	var out []render.File
	for _, f := range files {
		if f.Path != path {
			out = append(out, f)
		}
	}
	return out
}

// writeOutput writes the rendered files into dir.
func writeOutput(t *testing.T, dir string, out render.Output) {
	t.Helper()
	for _, f := range out.Files {
		path := filepath.Join(dir, f.Path)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, f.Data, f.Mode); err != nil {
			t.Fatal(err)
		}
	}
}

// runSetupSh runs setup.sh in dir with no terminal and returns its output.
func runSetupSh(t *testing.T, dir string) (string, error) {
	t.Helper()
	cmd := exec.CommandContext(t.Context(), "sh", "setup.sh")
	cmd.Dir = dir
	cmd.Stdin = strings.NewReader("")
	out, err := cmd.CombinedOutput()
	return string(out), err
}

// dirFiles is every file under dir with its mode and content.
func dirFiles(t *testing.T, dir string) map[string]string {
	t.Helper()
	files := map[string]string{}
	err := filepath.WalkDir(dir, func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		data, err := os.ReadFile(path)
		files[path] = info.Mode().String() + " " + string(data)
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	return files
}
