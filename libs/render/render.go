// Package render turns a plan into the files of a runnable Compose project: docker-compose.yml,
// graph.yaml, setup.sh, .env and README.md (spec 0002, render rules). The zip download and the
// managed deploy both use it, so they cannot drift.
package render

import (
	"archive/zip"
	"bytes"
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"slices"
	"strconv"
	"strings"
	"text/template"
	"time"

	"github.com/compose-spec/compose-go/v2/types"
	"sigs.k8s.io/yaml"

	"github.com/racherom/modular-platform/libs/contract"
)

// ErrPlanHasErrors is returned for a plan that is not ok; such a plan is never rendered.
var ErrPlanHasErrors = errors.New("render: the plan has errors")

//go:embed templates/*.tmpl
var templateFS embed.FS

var templates = template.Must(template.ParseFS(templateFS, "templates/*.tmpl"))

// zipTime is the earliest time the zip format can hold, so the same graph gives the same bytes.
var zipTime = time.Date(1980, time.January, 1, 0, 0, 0, 0, time.UTC)

// File is one rendered file.
type File struct {
	Path string
	Mode fs.FileMode
	Data []byte
}

// Output is a rendered Compose project, its files sorted by path.
type Output struct {
	Name  string
	Files []File
}

type secret struct {
	Instance, Name, Env string
	Path                string // secrets/<instance>/<name>
	Compose             string // <instance>-<name>
}

type templateData struct {
	Name    string
	Secrets []secret
}

// Render renders the resolved graph of an ok plan. catalog must hold every module the graph uses.
func Render(plan contract.PlanResult, catalog []contract.CatalogEntry) (Output, error) {
	if !plan.Ok {
		return Output{}, ErrPlanHasErrors
	}
	entries := make(map[string]contract.CatalogEntry, len(catalog))
	for _, e := range catalog {
		entries[e.Name] = e
	}
	g := plan.Graph
	data := templateData{Name: g.Name}

	instances := make([]string, 0, len(g.Modules))
	for name := range g.Modules {
		instances = append(instances, name)
	}
	slices.Sort(instances)

	project := types.Project{
		Name:     g.Name,
		Services: types.Services{},
	}
	for _, i := range instances {
		entry, ok := entries[g.Modules[i].Module]
		if !ok {
			return Output{}, fmt.Errorf("render: module %q of instance %s is not in the catalog", g.Modules[i].Module, i)
		}
		svc := types.ServiceConfig{Name: i}
		svc.Image = entry.Image
		svc.HealthCheck = healthcheck(entry.Healthcheck)
		for _, s := range entry.Secrets {
			sec := secret{Instance: i, Name: s.Name, Env: s.Env, Path: "secrets/" + i + "/" + s.Name, Compose: i + "-" + s.Name}
			if _, taken := project.Secrets[sec.Compose]; taken {
				return Output{}, fmt.Errorf("render: two secrets would share the Compose name %s", sec.Compose)
			}
			if project.Secrets == nil {
				project.Secrets = types.Secrets{}
			}
			project.Secrets[sec.Compose] = types.SecretConfig{File: "./" + sec.Path}
			svc.Secrets = append(svc.Secrets, types.ServiceSecretConfig{Source: sec.Compose})
			setEnv(&svc, s.Env, "/run/secrets/"+sec.Compose)
			data.Secrets = append(data.Secrets, sec)
		}
		project.Services[i] = svc
	}

	for _, b := range plan.Bindings {
		consumer := project.Services[b.Consumer]
		providerEntry := entries[g.Modules[b.Provider].Module]
		condition := types.ServiceConditionStarted
		if providerEntry.Healthcheck != nil {
			condition = types.ServiceConditionHealthy
		}
		if consumer.DependsOn == nil {
			consumer.DependsOn = types.DependsOnConfig{}
		}
		consumer.DependsOn[b.Provider] = types.ServiceDependency{Condition: condition, Required: true}

		env := entries[g.Modules[b.Consumer].Module].Uses[b.Capability].Env
		if env.Host != "" {
			setEnv(&consumer, env.Host, b.Provider)
		}
		if env.Port != "" {
			setEnv(&consumer, env.Port, strconv.Itoa(providerEntry.Provides[b.Capability].Port))
		}
		project.Services[b.Consumer] = consumer
	}

	compose, err := composeYAML(project)
	if err != nil {
		return Output{}, err
	}
	graphText, err := contract.MarshalGraph(g)
	if err != nil {
		return Output{}, fmt.Errorf("render: %w", err)
	}
	files := []File{
		{Path: "docker-compose.yml", Mode: 0o644, Data: compose},
		{Path: "graph.yaml", Mode: 0o644, Data: graphText},
	}
	for _, t := range []struct {
		path, template string
		mode           fs.FileMode
	}{
		{"setup.sh", "setup.sh.tmpl", 0o755},
		{".env", "env.tmpl", 0o644},
		{"README.md", "README.md.tmpl", 0o644},
	} {
		var buf bytes.Buffer
		if err := templates.ExecuteTemplate(&buf, t.template, data); err != nil {
			return Output{}, fmt.Errorf("render %s: %w", t.path, err)
		}
		files = append(files, File{Path: t.path, Mode: t.mode, Data: buf.Bytes()})
	}
	slices.SortFunc(files, func(a, b File) int { return strings.Compare(a.Path, b.Path) })
	return Output{Name: g.Name, Files: files}, nil
}

// WriteZip writes the files as a zip, entries in path order with a fixed time, so the same
// output always gives the same bytes.
func (o Output) WriteZip(w io.Writer) error {
	zw := zip.NewWriter(w)
	for _, f := range o.Files {
		h := &zip.FileHeader{Name: f.Path, Method: zip.Deflate, Modified: zipTime}
		h.SetMode(f.Mode)
		fw, err := zw.CreateHeader(h)
		if err != nil {
			return fmt.Errorf("zip %s: %w", f.Path, err)
		}
		if _, err := fw.Write(f.Data); err != nil {
			return fmt.Errorf("zip %s: %w", f.Path, err)
		}
	}
	if err := zw.Close(); err != nil {
		return fmt.Errorf("zip: %w", err)
	}
	return nil
}

func setEnv(svc *types.ServiceConfig, name, value string) {
	if svc.Environment == nil {
		svc.Environment = types.MappingWithEquals{}
	}
	svc.Environment[name] = &value
}

// healthcheck copies a catalog healthcheck; the catalog rules already checked its durations.
func healthcheck(h *contract.Healthcheck) *types.HealthCheckConfig {
	if h == nil {
		return nil
	}
	out := &types.HealthCheckConfig{Test: types.HealthCheckTest(h.Test)}
	duration := func(s string) *types.Duration {
		if s == "" {
			return nil
		}
		d, _ := time.ParseDuration(s)
		v := types.Duration(d)
		return &v
	}
	out.Interval = duration(h.Interval)
	out.Timeout = duration(h.Timeout)
	out.StartPeriod = duration(h.StartPeriod)
	if h.Retries != nil {
		r := uint64(*h.Retries)
		out.Retries = &r
	}
	return out
}

// composeYAML writes the project from compose-go types with keys in sorted order. compose-go
// writes unset command and entrypoint as null, which Compose could read as a reset, so every
// null is dropped first.
func composeYAML(p types.Project) ([]byte, error) {
	raw, err := json.Marshal(p)
	if err != nil {
		return nil, fmt.Errorf("render compose: %w", err)
	}
	var tree any
	if err := json.Unmarshal(raw, &tree); err != nil {
		return nil, fmt.Errorf("render compose: %w", err)
	}
	out, err := yaml.Marshal(dropNulls(tree))
	if err != nil {
		return nil, fmt.Errorf("render compose: %w", err)
	}
	return out, nil
}

func dropNulls(v any) any {
	switch t := v.(type) {
	case map[string]any:
		for k, child := range t {
			if child == nil {
				delete(t, k)
				continue
			}
			t[k] = dropNulls(child)
		}
	case []any:
		for i, child := range t {
			t[i] = dropNulls(child)
		}
	}
	return v
}
