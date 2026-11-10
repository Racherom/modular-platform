package contract

import (
	"bytes"
	"errors"
	"fmt"
	"io/fs"
	"maps"
	"path"
	"strings"
	"text/template"
)

// ErrNoTemplate is returned for a template that the loaded templates do not hold.
var ErrNoTemplate = errors.New("contract: template not loaded")

// TemplateData is what a module template reads (spec 0013, "Templates"): graph facts known at
// check time, never a secret, an install answer or a run time value.
type TemplateData struct {
	Graph    string                      // graph name
	Instance string                      // instance name
	Host     string                      // <instance>.<graph>.internal
	Provides map[string]TemplateProvide  // every declared provide is a key
	Uses     map[string]TemplateSlot     // every declared slot is a key, bound or not
	Clients  map[string][]TemplateClient // every declared provide is a key; one item per binding to it
}

// TemplateProvide is one provide of the instance.
type TemplateProvide struct {
	Port int
}

// TemplateSlot is one slot of the instance and the provide it is bound to. An unbound slot has
// Bound false, the zero address and empty Values.
type TemplateSlot struct {
	Bound             bool
	Provider, Provide string
	Host              string
	Port              int
	URL               string
	Values            map[string]string // the non secret fields known at check time that the provider gives
}

// TemplateClient is one binding to a provide of the instance, from consumer Instance's Slot.
type TemplateClient struct {
	Instance, Slot, Host string
	Values               map[string]string // the binding's to_provider text fields
}

// Templates holds the parsed templates of a catalog's modules. The zero value holds none. A
// template may be executed by several goroutines at once.
type Templates struct {
	byModule map[string]map[string]*template.Template // module name, then template file
}

// LoadTemplates parses the templates of every module in c from fsys, laid out as LoadCatalog
// reads it. c must come from LoadCatalog over the same fsys with no problems, so every template
// exists and parses.
func LoadTemplates(fsys fs.FS, c Catalog) (Templates, error) {
	t := Templates{byModule: map[string]map[string]*template.Template{}}
	for _, m := range c.Modules {
		for _, tm := range m.Templates {
			parsed, err := readTemplate(fsys, m.Name, tm)
			if err != nil {
				return Templates{}, err
			}
			if t.byModule[m.Name] == nil {
				t.byModule[m.Name] = map[string]*template.Template{}
			}
			t.byModule[m.Name][tm.File] = parsed
		}
	}
	return t, nil
}

// Execute renders template file of module with data. A key the data lacks is an error.
func (t Templates) Execute(module, file string, data TemplateData) ([]byte, error) {
	parsed, ok := t.byModule[module][file]
	if !ok {
		return nil, fmt.Errorf("%w: %s of module %s", ErrNoTemplate, file, module)
	}
	var buf bytes.Buffer
	if err := parsed.Execute(&buf, data); err != nil {
		return nil, fmt.Errorf("template %s of module %s: %w", file, module, err)
	}
	return buf.Bytes(), nil
}

// TemplateOutput is the file a template renders to inside the project, config/<instance>/<file
// without .tmpl>, and the name of its Compose config, <instance>_<file without .tmpl>.
func TemplateOutput(instance string, t Template) (file, config string) {
	name := strings.TrimSuffix(t.File, ".tmpl")
	return "config/" + instance + "/" + name, instance + "_" + name
}

// readTemplate reads and parses template tm of module from fsys: text/template with no functions
// beyond the built ins, missing keys an error, and the entry's delims when it sets them.
func readTemplate(fsys fs.FS, module string, tm Template) (*template.Template, error) {
	body, err := fs.ReadFile(fsys, path.Join(modulesDir, module, tm.File))
	if err != nil {
		return nil, fmt.Errorf("template %s of module %s: %w", tm.File, module, err)
	}
	t := template.New(tm.File).Option("missingkey=error")
	if len(tm.Delims) == 2 {
		t = t.Delims(tm.Delims[0], tm.Delims[1])
	}
	t, err = t.Parse(string(body))
	if err != nil {
		return nil, fmt.Errorf("template %s of module %s: %w", tm.File, module, err)
	}
	return t, nil
}

// checkTemplates parses each template of m and executes it twice (spec 0013): once with every
// slot bound to placeholder values and a client on every provide, once with every optional slot
// unbound and no client. A template that is missing, fails to parse or names a key the data
// lacks in either run is invalid_template. That proves only that the keys exist; a golden
// output from a real graph proves the rest.
func checkTemplates(fsys fs.FS, file string, m Module, caps map[string]Capability) []CatalogProblem {
	c := checker{file: file}
	bound, unbound := placeholderData(m, caps, true), placeholderData(m, caps, false)
	for i, tm := range m.Templates {
		p := fmt.Sprintf("templates.%d", i)
		t, err := readTemplate(fsys, m.Name, tm)
		if errors.Is(err, fs.ErrNotExist) {
			c.add(p+".file", CodeInvalidTemplate, "file %q is not in the module folder", tm.File)
			continue
		}
		if err != nil {
			c.add(p, CodeInvalidTemplate, "%s", oneLine(err))
			continue
		}
		for _, run := range []struct {
			name string
			data TemplateData
		}{{"with every slot bound", bound}, {"with every optional slot unbound", unbound}} {
			if err := t.Execute(&bytes.Buffer{}, run.data); err != nil {
				c.add(p, CodeInvalidTemplate, "%s: %s", run.name, oneLine(err))
				break
			}
		}
	}
	return c.problems
}

// placeholderData is the template data of an instance of m named after the module in graph
// "graph". With bound set every slot is bound and every provide has one client; otherwise every
// optional slot is unbound and no provide has a client. Each field known at check time holds its name.
func placeholderData(m Module, caps map[string]Capability, bound bool) TemplateData {
	const graph = "graph"
	host := func(i string) string { return i + "." + graph + ".internal" }
	d := TemplateData{
		Graph:    graph,
		Instance: m.Name,
		Host:     host(m.Name),
		Provides: map[string]TemplateProvide{},
		Uses:     map[string]TemplateSlot{},
		Clients:  map[string][]TemplateClient{},
	}
	for name, q := range m.Provides {
		d.Provides[name] = TemplateProvide{Port: q.Port}
		d.Clients[name] = []TemplateClient{}
		if bound {
			k := caps[q.Capability]
			values := textFields(k, CapabilityField.ToProvider, true)
			if k.Yields != nil {
				maps.Copy(values, textFields(caps[k.Yields.Capability], CapabilityField.ToProvider, true))
			}
			d.Clients[name] = []TemplateClient{{Instance: "client", Slot: "slot", Host: host("client"), Values: values}}
		}
	}
	for name, s := range m.Slots() {
		if !bound && s.Optional {
			d.Uses[name] = TemplateSlot{Values: map[string]string{}}
			continue
		}
		k := caps[s.Capability]
		url := ""
		if k.Scheme != "" {
			url = k.Scheme + "://" + host("provider") + ":1"
		}
		d.Uses[name] = TemplateSlot{
			Bound: true, Provider: "provider", Provide: "provide", Host: host("provider"), Port: 1, URL: url,
			Values: textFields(k, CapabilityField.ToConsumer, false),
		}
	}
	return d
}

// textFields maps each non file field of k that reaches the side dir picks to its own name; with
// exact set a both field is left out, as to_provider means exactly that direction.
func textFields(k Capability, dir func(CapabilityField) bool, exact bool) map[string]string {
	out := map[string]string{}
	for f, field := range k.Fields {
		if field.IsFile() || !dir(field) || (exact && field.Direction == Both) {
			continue
		}
		out[f] = f
	}
	return out
}
