package contract_test

import (
	"errors"
	"testing"
	"testing/fstest"

	"github.com/racherom/modular-platform/libs/contract"
)

// goodTemplate reads every kind of template data, guarding the optional slot db.
const goodTemplate = `{{.Graph}} {{.Instance}} {{.Host}} {{.Provides.web.Port}}
{{if .Uses.db.Bound}}{{.Uses.db.Provider}}.{{.Uses.db.Provide}} {{.Uses.db.URL}} {{.Uses.db.Values.name}}{{else}}no db{{end}}
{{range .Clients.web}}{{.Instance}}.{{.Slot}} {{.Host}} {{.Values.owner}}
{{end}}`

// templateCatalog is the good capabilities and module, with the module's template body and
// template list replaced.
func templateCatalog(t *testing.T, body, templates string) fstest.MapFS {
	t.Helper()
	module := edit(t, goodModule, [][2]string{{"templates:\n  - file: svc.conf.tmpl\n    path: /etc/svc.conf\n", templates}})
	fsys := fstest.MapFS{
		"capabilities/store.yaml":       {Data: []byte(goodCapability)},
		"capabilities/store-admin.yaml": {Data: []byte(goodProvisioner)},
		"modules/svc/module.yaml":       {Data: []byte(module)},
	}
	if body != "" {
		fsys["modules/svc/svc.conf.tmpl"] = &fstest.MapFile{Data: []byte(body)}
	}
	return fsys
}

const svcTemplate = "templates:\n  - file: svc.conf.tmpl\n    path: /etc/svc.conf\n"

// TestTemplateRules refuses each template that cannot render at catalog load (AC-9, AC-2).
func TestTemplateRules(t *testing.T) {
	t.Parallel()
	delims := svcTemplate + "    delims: [\"[[\", \"]]\"]\n"
	tests := map[string]struct {
		body, templates, path string
	}{
		"missing file":         {"", svcTemplate, "templates.0.file"},
		"parse error":          {"{{ .Graph", svcTemplate, "templates.0"},
		"function":             {`{{ env "HOME" }}`, svcTemplate, "templates.0"},
		"undeclared slot":      {"{{ .Uses.nope.Host }}", svcTemplate, "templates.0"},
		"unknown key":          {"{{ .Bogus }}", svcTemplate, "templates.0"},
		"undeclared provide":   {"{{ .Provides.api.Port }}", svcTemplate, "templates.0"},
		"secret field":         {"{{ .Uses.db.Values.password }}", svcTemplate, "templates.0"},
		"unguarded optional":   {"{{ .Uses.db.Values.name }}", svcTemplate, "templates.0"},
		"client consumer side": {"{{ range .Clients.web }}{{ .Values.host }}{{ end }}", svcTemplate, "templates.0"},
		"delims own key":       {"[[ .Bogus ]]", delims, "templates.0"},
	}
	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			c, problems := contract.LoadCatalog(templateCatalog(t, tc.body, tc.templates))
			wantProblem(t, problems, tc.path, contract.CodeInvalidTemplate)
			if len(c.Modules) != 0 {
				t.Errorf("modules = %v, want the broken entry left out", c.Modules)
			}
		})
	}
}

// TestTemplatesAccepted loads good templates, one with its own delims and a body using {{ }}.
func TestTemplatesAccepted(t *testing.T) {
	t.Parallel()
	for name, tc := range map[string]struct{ body, templates string }{
		"default delims": {goodTemplate, svcTemplate},
		"own delims":     {`[[ .Instance ]] keeps {{ env "X" }}`, svcTemplate + "    delims: [\"[[\", \"]]\"]\n"},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			if _, problems := contract.LoadCatalog(templateCatalog(t, tc.body, tc.templates)); len(problems) > 0 {
				t.Errorf("problems = %v, want none", problems)
			}
		})
	}
}

// TestLoadTemplatesExecute renders a loaded template with real data, and refuses a template that
// was never loaded.
func TestLoadTemplatesExecute(t *testing.T) {
	t.Parallel()
	fsys := templateCatalog(t, goodTemplate, svcTemplate)
	c, problems := contract.LoadCatalog(fsys)
	if len(problems) > 0 {
		t.Fatal(problems)
	}
	tmpls, err := contract.LoadTemplates(fsys, c)
	if err != nil {
		t.Fatal(err)
	}
	data := contract.TemplateData{
		Graph: "g", Instance: "svc", Host: "svc.g.internal",
		Provides: map[string]contract.TemplateProvide{"web": {Port: 8080}},
		Uses:     map[string]contract.TemplateSlot{"db": {Values: map[string]string{}}},
		Clients: map[string][]contract.TemplateClient{"web": {
			{Instance: "a", Slot: "db", Host: "a.g.internal", Values: map[string]string{"owner": "team-a"}},
		}},
	}
	out, err := tmpls.Execute("svc", "svc.conf.tmpl", data)
	if err != nil {
		t.Fatal(err)
	}
	want := "g svc svc.g.internal 8080\nno db\na.db a.g.internal team-a\n"
	if string(out) != want {
		t.Errorf("output = %q, want %q", out, want)
	}
	if _, err := (contract.Templates{}).Execute("svc", "svc.conf.tmpl", data); !errors.Is(err, contract.ErrNoTemplate) {
		t.Errorf("err = %v, want ErrNoTemplate", err)
	}
	if file, config := contract.TemplateOutput("svc", c.Modules[0].Templates[0]); file != "config/svc/svc.conf" || config != "svc_svc.conf" {
		t.Errorf("TemplateOutput = %s, %s", file, config)
	}
}
