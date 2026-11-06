package render_test

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/compose-spec/compose-go/v2/loader"
	"github.com/compose-spec/compose-go/v2/types"

	"github.com/racherom/modular-platform/libs/contract"
	"github.com/racherom/modular-platform/libs/render"
	"github.com/racherom/modular-platform/sdk/envfile"
)

// graphConfigOutput renders the graph-config case: a module not built on the SDK whose graph sets
// some asked values (with ', $ and #) and leaves others out, plus plain values, and a module on
// the SDK whose graph sets a"b and "q (spec 0025, AC-25, AC-27, AC-28).
func graphConfigOutput(t *testing.T) render.Output {
	t.Helper()
	catalog := loadCatalog(t, "testdata/graph-config/catalog")
	out, err := render.Render(planFile(t, "testdata/graph-config/graph.yaml", catalog), catalog, contract.Templates{})
	if err != nil {
		t.Fatal(err)
	}
	return out
}

// TestRenderGraphConfigGolden pins every rendered file of the graph-config case (AC-25, AC-27,
// AC-28).
func TestRenderGraphConfigGolden(t *testing.T) {
	checkGolden(t, filepath.Join("testdata", "graph-config", "golden"), graphConfigOutput(t))
}

// TestGraphValuesReachTheService reads the Compose file back as docker compose does: each graph
// value is its variable's default, as typed, an empty graph value beats nothing, an asked value
// the graph leaves out stays bare, and a .env line still wins (AC-25, AC-27).
func TestGraphValuesReachTheService(t *testing.T) {
	compose := fileData(t, graphConfigOutput(t), "docker-compose.yml")
	ids := map[string]string{"MP_UID": "1000", "MP_GID": "1000"}
	for name, tc := range map[string]struct {
		env  map[string]string
		want map[string]string
	}{
		"graph values": {with(with(ids, "MP_TELLER__MODE", "relaxed"), "MP_TELLER__ORIGIN", "https://example.org"), map[string]string{
			"TELLER_BANNER": "$y $$z", "TELLER_DOMAIN": "example.org", "TELLER_LEVEL": "debug", "TELLER_MOTD": "",
			"TELLER_MOTTO": "$x", "TELLER_NOTE": "a'b", "TELLER_TAGLINE": "a #b", "TELLER_MODE": "relaxed",
		}},
		".env wins": {with(with(with(ids, "MP_TELLER__MODE", "strict"), "MP_TELLER__ORIGIN", "https://x.org"), "MP_TELLER__DOMAIN", "x.org"), map[string]string{
			"TELLER_DOMAIN": "x.org", "TELLER_NOTE": "a'b",
		}},
	} {
		t.Run(name, func(t *testing.T) {
			p, err := loader.LoadWithContext(t.Context(), types.ConfigDetails{
				ConfigFiles: []types.ConfigFile{{Filename: "docker-compose.yml", Content: compose}},
				Environment: tc.env,
			}, func(o *loader.Options) { o.SkipConsistencyCheck = true; o.ResolvePaths = false })
			if err != nil {
				t.Fatal(err)
			}
			env := p.Services["teller"].Environment
			for k, want := range tc.want {
				if got := env[k]; got == nil || *got != want {
					t.Errorf("%s = %v, want %q", k, got, want)
				}
			}
		})
	}
	if !strings.Contains(string(compose), "TELLER_MODE: ${MP_TELLER__MODE}\n") {
		t.Error("an asked value the graph leaves out is not bare")
	}
}

// TestSDKGraphValuesRoundTrip reads config/tally.env with the SDK's own parser: each graph value
// comes back exactly as typed, a"b and "q included (AC-27).
func TestSDKGraphValuesRoundTrip(t *testing.T) {
	t.Parallel()
	lines, errs := envfile.Parse(fileData(t, graphConfigOutput(t), "config/tally.env"))
	if len(errs) > 0 {
		t.Fatalf("env file: %v", errs)
	}
	got := map[string]string{}
	for _, l := range lines {
		got[l.Key] = l.Value
	}
	if got["TALLY_GREETING"] != `a"b` || got["TALLY_NOTE"] != `"q` {
		t.Errorf("env file values = %q", got)
	}
	if _, ok := got["TALLY_LEVEL"]; ok {
		t.Error("a value the graph leaves out is set, want its default commented")
	}
}

// TestSetupGraphAnswers runs the rendered setup.sh with a fake docker that records each step's
// env: it asks only the values the graph leaves out, says which the graph sets or .env
// overrides, writes no graph value into .env, and hands each one to the step as typed, also
// inside another value's default (AC-28).
func TestSetupGraphAnswers(t *testing.T) {
	if _, err := exec.LookPath("sh"); err != nil {
		t.Skip("no sh")
	}
	for name, tc := range map[string]struct {
		env        string
		lines      []string // in the output
		envLines   []string // in .env afterwards
		notInEnv   []string
		stepRecord string
	}{
		"graph answers": {
			lines:      []string{"set by the graph  MP_TELLER__DOMAIN=example.org", "set by the graph  MP_TELLER__NOTE=a'b", "set by the graph  MP_TELLER__MOTTO=$x", "set by the graph  MP_TELLER__TAGLINE=a #b"},
			envLines:   []string{"MP_TELLER__ORIGIN='https://example.org'", "MP_TELLER__MODE='strict'"},
			notInEnv:   []string{"MP_TELLER__DOMAIN", "MP_TELLER__NOTE", "MP_TELLER__MOTTO", "MP_TELLER__TAGLINE"},
			stepRecord: "MP_ASK_DOMAIN=example.org|MP_ASK_MODE=strict|MP_ASK_MOTTO=$x|MP_ASK_NOTE=a'b|MP_ASK_ORIGIN=https://example.org|MP_ASK_TAGLINE=a #b",
		},
		".env overrides": {
			env:        "MP_TELLER__DOMAIN='other.org'\nMP_TELLER__NOTE=a'b\n",
			lines:      []string{".env overrides the graph  MP_TELLER__DOMAIN", "set by the graph  MP_TELLER__NOTE=a'b"},
			envLines:   []string{"MP_TELLER__ORIGIN='https://other.org'"},
			stepRecord: "MP_ASK_DOMAIN=other.org|MP_ASK_MODE=strict|MP_ASK_MOTTO=$x|MP_ASK_NOTE=a'b|MP_ASK_ORIGIN=https://other.org|MP_ASK_TAGLINE=a #b",
		},
	} {
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			writeOutput(t, dir, graphConfigOutput(t))
			if err := os.WriteFile(filepath.Join(dir, ".env"), []byte("MP_UID=1000\nMP_GID=1000\n"+tc.env), 0o644); err != nil {
				t.Fatal(err)
			}
			bin := fakeDocker(t)
			cmd := exec.CommandContext(t.Context(), "sh", "setup.sh")
			cmd.Dir = dir
			cmd.Env = append(os.Environ(), "PATH="+bin+":"+os.Getenv("PATH"), "STEP_RECORD="+filepath.Join(dir, "step.txt"))
			cmd.Stdin = strings.NewReader("\n\n") // mode and origin take their defaults
			outBytes, err := cmd.CombinedOutput()
			out := string(outBytes)
			if err != nil {
				var exit *exec.ExitError
				_ = errors.As(err, &exit)
				t.Fatalf("setup.sh: %v\n%s", err, out)
			}
			for _, l := range tc.lines {
				if !strings.Contains(out, l+"\n") {
					t.Errorf("output lacks %q:\n%s", l, out)
				}
			}
			env, _ := os.ReadFile(filepath.Join(dir, ".env"))
			for _, l := range tc.envLines {
				if !strings.Contains(string(env), l+"\n") {
					t.Errorf(".env lacks %q:\n%s", l, env)
				}
			}
			for _, v := range tc.notInEnv {
				if strings.Contains(string(env), v+"=") {
					t.Errorf(".env holds %s:\n%s", v, env)
				}
			}
			record, _ := os.ReadFile(filepath.Join(dir, "step.txt"))
			if got := strings.TrimSpace(string(record)); got != tc.stepRecord {
				t.Errorf("step env = %q, want %q", got, tc.stepRecord)
			}
		})
	}
}

// fakeDocker writes a docker command that records the MP_ASK_ env a step run gets, sorted and
// joined by |, into $STEP_RECORD, and writes the step's output file so setup.sh goes on.
func fakeDocker(t *testing.T) string {
	t.Helper()
	bin := t.TempDir()
	script := `#!/bin/sh
out=
asks=
while [ $# -gt 0 ]; do
	case $1 in
	-e) case $2 in MP_ASK_*) asks="$asks$2
" ;; esac; shift 2 ;;
	-v) case $2 in *:/out) out=${2%:/out} ;; esac; shift 2 ;;
	*) shift ;;
	esac
done
printf '%s' "$asks" | sort | paste -sd '|' - > "$STEP_RECORD"
printf 'x' > "$out/hello"
`
	if err := os.WriteFile(filepath.Join(bin, "docker"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	return bin
}

// TestManagedGraphAnswersSetup: a deployed graph whose only setup need is an asked value the
// graph sets is not setup_required; left out, it still is (AC-28, AC-11).
func TestManagedGraphAnswersSetup(t *testing.T) {
	t.Parallel()
	catalog := loadCatalog(t, "testdata/graph-config/catalog")
	for text, refused := range map[string]bool{
		"apiVersion: modular-platform/v1\nkind: Graph\nmodules:\n  simple:\n    config:\n      domain: \"example.org\"\n    module: simple\nname: managed\n": false,
		"apiVersion: modular-platform/v1\nkind: Graph\nmodules:\n  simple:\n    module: simple\nname: managed\n":                                             true,
	} {
		_, err := render.Managed(planText(t, text, catalog), catalog, contract.Templates{}, testDeployment)
		var r *render.RefusedError
		if got := errors.As(err, &r); got != refused {
			t.Errorf("graph\n%s refused = %v (%v), want %v", text, got, err, refused)
		}
		if refused && (r == nil || len(r.Problems) != 1 || r.Problems[0].Code != render.CodeSetupRequired) {
			t.Errorf("problems = %+v, want one setup_required", r)
		}
	}
}
