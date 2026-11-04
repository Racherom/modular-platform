// Package render turns a plan into the files of a runnable Compose project: docker-compose.yml,
// graph.yaml, setup.sh, .env, README.md and a config/<instance>.env per module built on the sdk
// (spec 0013, "Render"; spec 0014, "Jobs in the render"; spec 0005, render additions). The zip
// download and the managed deploy both use it, so they cannot drift.
package render

import (
	"archive/zip"
	"bytes"
	"crypto/sha256"
	"embed"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"slices"
	"strings"
	"text/template"
	"time"

	"github.com/compose-spec/compose-go/v2/types"
	"sigs.k8s.io/yaml"

	"github.com/racherom/modular-platform/libs/contract"
)

// ErrPlanHasErrors is returned for a plan that is not ok; such a plan is never rendered.
var ErrPlanHasErrors = errors.New("render: the plan has errors")

// ErrNotRendered is returned for a catalog field this release declares but does not render yet.
var ErrNotRendered = errors.New("render: not rendered by this release")

//go:embed templates/*.tmpl
var templateFS embed.FS

var templates = template.Must(template.New("").Funcs(template.FuncMap{"shellQuote": shellQuote}).ParseFS(templateFS, "templates/*.tmpl"))

// shellQuote quotes s as one POSIX shell word.
func shellQuote(s string) string { return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'" }

// zipTime is the earliest time the zip format can hold, so the same graph gives the same bytes.
var zipTime = time.Date(1980, time.January, 1, 0, 0, 0, 0, time.UTC)

// The networks of every project (spec 0013, "Host name and networks").
const (
	networkInternal = "internal" // every instance, no route out
	networkEgress   = "egress"   // instances that declare network.egress, and jobs of an external provisioner
	runUser         = "${MP_UID}:${MP_GID}"
)

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
	Instance          string
	Path              string // secrets/<instance>/<name>
	Capability, Field string // placed only: the external provide's capability and field
	Description       string // placed only: the field's description
}

type templateData struct {
	Name     string
	Secrets  []secret // the secret files setup.sh creates
	Placed   []secret // the files you place
	EnvFiles []string // config/<instance>.env of each sdk instance
	Folders  []string // data/<instance>/<volume> folders setup.sh creates
}

// Render renders the resolved graph of an ok plan. catalog must hold every module and
// capability the graph uses.
func Render(plan contract.PlanResult, catalog contract.Catalog) (Output, error) {
	r, err := renderProject(plan, catalog, false, nil)
	if err != nil {
		return Output{}, err
	}
	return r.out, nil
}

// rendered is a render's output plus what the managed flavor reports beside it.
type rendered struct {
	out  Output
	data templateData
	jobs map[string]job // job service name to what it does
}

// job is a one shot service of the project (spec 0014).
type job struct {
	instance, module, kind string // the provisioner instance and module; provision or revoke
}

// renderer holds one render in progress.
type renderer struct {
	g        contract.Graph
	managed  bool
	modules  map[string]contract.Module
	caps     map[string]contract.Capability
	project  types.Project
	data     templateData
	jobs     map[string]job
	declared map[string]bool // secret file paths already listed for setup.sh or as placed

	bound  map[string]map[string]map[string]value // consumer, slot, field to its value
	admin  map[string]map[string]value            // provider.provide to its admin field values
	egress bool                                   // some service joins the egress network
}

// renderProject renders plan in the static flavor, or the managed one: restart: unless-stopped
// on every long running service, and finish called on the project before it is written.
func renderProject(plan contract.PlanResult, catalog contract.Catalog, managed bool, finish func(*types.Project, map[string]job)) (rendered, error) {
	if !plan.Ok {
		return rendered{}, ErrPlanHasErrors
	}
	r := renderer{
		g:        plan.Graph,
		managed:  managed,
		modules:  make(map[string]contract.Module, len(catalog.Modules)),
		caps:     make(map[string]contract.Capability, len(catalog.Capabilities)),
		data:     templateData{Name: plan.Graph.Name},
		jobs:     map[string]job{},
		declared: map[string]bool{},
		bound:    map[string]map[string]map[string]value{},
		admin:    map[string]map[string]value{},
		project: types.Project{
			Name:     plan.Graph.Name,
			Services: types.Services{},
			Networks: types.Networks{networkInternal: types.NetworkConfig{Internal: true}},
		},
	}
	for _, m := range catalog.Modules {
		r.modules[m.Name] = m
	}
	for _, c := range catalog.Capabilities {
		r.caps[c.Name] = c
	}

	instances := sortedKeys(r.g.Modules)
	for _, i := range instances {
		gm := r.g.Modules[i]
		if gm.External != nil {
			continue // a service outside the stack has no Compose service
		}
		m, ok := r.modules[gm.Module]
		if !ok {
			return rendered{}, fmt.Errorf("render: module %q of instance %s is not in the catalog", gm.Module, i)
		}
		svc, err := r.service(i, m)
		if err != nil {
			return rendered{}, err
		}
		r.project.Services[i] = svc
	}
	for _, b := range plan.Bindings {
		if err := r.binding(b); err != nil {
			return rendered{}, err
		}
	}
	for _, i := range instances {
		if gm := r.g.Modules[i]; gm.External == nil {
			if err := r.environment(i, r.modules[gm.Module]); err != nil {
				return rendered{}, err
			}
		}
	}
	if r.egress {
		r.project.Networks[networkEgress] = types.NetworkConfig{}
	}
	slices.SortFunc(r.data.Secrets, func(a, b secret) int { return strings.Compare(a.Path, b.Path) })
	slices.SortFunc(r.data.Placed, func(a, b secret) int { return strings.Compare(a.Path, b.Path) })
	slices.Sort(r.data.Folders)

	if finish != nil {
		finish(&r.project, r.jobs)
	}
	compose, err := composeYAML(r.project)
	if err != nil {
		return rendered{}, err
	}
	graphText, err := contract.MarshalGraph(r.g)
	if err != nil {
		return rendered{}, fmt.Errorf("render: %w", err)
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
		if err := templates.ExecuteTemplate(&buf, t.template, r.data); err != nil {
			return rendered{}, fmt.Errorf("render %s: %w", t.path, err)
		}
		files = append(files, File{Path: t.path, Mode: t.mode, Data: buf.Bytes()})
	}
	for _, i := range instances {
		if gm := r.g.Modules[i]; gm.External == nil && r.modules[gm.Module].Runtime == contract.Sdk {
			files = append(files, File{Path: envFilePath(i), Mode: 0o644, Data: envFile(i, r.modules[gm.Module])})
		}
	}
	slices.SortFunc(files, func(a, b File) int { return strings.Compare(a.Path, b.Path) })
	return rendered{out: Output{Name: r.project.Name, Files: files}, data: r.data, jobs: r.jobs}, nil
}

// service builds the Compose service of module instance i: image, command, healthcheck, host
// name, hardening, its own secrets and data folders.
func (r *renderer) service(i string, m contract.Module) (types.ServiceConfig, error) {
	svc := types.ServiceConfig{Name: i}
	svc.Image = m.Image
	svc.HealthCheck = healthcheck(m.Healthcheck)
	if len(m.Entrypoint) > 0 {
		svc.Entrypoint = escapeAll(m.Entrypoint)
	}
	if len(m.Command) > 0 {
		svc.Command = escapeAll(m.Command)
	}
	if len(m.Templates) > 0 || len(m.Setup) > 0 || len(m.Publish) > 0 || len(m.HostMounts) > 0 {
		return svc, fmt.Errorf("%w: templates, setup steps and host access of module %s", ErrNotRendered, m.Name)
	}
	r.networks(&svc, i, m)
	r.harden(&svc, m.Hardening)

	for _, s := range m.Secrets {
		if s.From != "" && s.From != contract.SecretFromGenerate {
			return svc, fmt.Errorf("%w: secret %s of module %s comes from %s", ErrNotRendered, s.Name, m.Name, s.From)
		}
		path := "secrets/" + i + "/" + s.Name
		target := s.Mount
		if target == "" {
			target = "/run/secrets/" + i + "_" + s.Name // spec 0002's rule
		}
		r.mountSecret(&svc, i+"_"+s.Name, path, target)
		if s.Env != "" {
			setEnv(&svc, s.Env, target)
		}
		r.generated(i, path)
	}
	for _, n := range sortedKeys(m.Volumes) {
		folder := "data/" + i + "/" + n
		svc.Volumes = append(svc.Volumes, types.ServiceVolumeConfig{Type: types.VolumeTypeBind, Source: "./" + folder, Target: m.Volumes[n]})
		r.data.Folders = append(r.data.Folders, folder)
	}
	if m.Runtime == contract.Sdk {
		r.sdkInstance(&svc, i, m)
	}
	if r.managed {
		svc.Restart = types.RestartPolicyUnlessStopped
	}
	return svc, nil
}

// networks joins instance i to internal, and to egress when its entry declares network.egress,
// with its host name and the entry's config aliases on each (spec 0013).
func (r *renderer) networks(svc *types.ServiceConfig, i string, m contract.Module) {
	aliases := []string{r.host(i)}
	joins := []string{networkInternal}
	if m.Network != nil {
		for _, a := range m.Network.Aliases {
			aliases = append(aliases, composeVar(i, a.Config, m.Config[a.Config].Default))
		}
		if m.Network.Egress {
			joins = append(joins, networkEgress)
			r.egress = true
		}
	}
	svc.Networks = map[string]*types.ServiceNetworkConfig{}
	for _, n := range joins {
		svc.Networks[n] = &types.ServiceNetworkConfig{Aliases: slices.Clone(aliases)}
	}
}

// environment writes the entry's fixed env of instance i (spec 0013): literal text escaped, {uid}
// and {gid} as the setup user's Compose variables, {config.<v>} as the value's Compose variable,
// and {<slot>.<field>} as that field of the instance's own binding, empty when the slot is unbound
// or the provider gives no value. It runs after the bindings, whose values it reads.
func (r *renderer) environment(i string, m contract.Module) error {
	if len(m.Environment) == 0 {
		return nil
	}
	svc := r.project.Services[i]
	for _, name := range sortedKeys(m.Environment) {
		var b strings.Builder
		for _, part := range contract.ParseEnvironment(m.Environment[name]) {
			switch part.Kind {
			case contract.EnvLiteral:
				b.WriteString(escape(part.Text))
			case contract.EnvUID:
				b.WriteString("${MP_UID}")
			case contract.EnvGID:
				b.WriteString("${MP_GID}")
			case contract.EnvConfig:
				b.WriteString(composeVar(i, part.Name, m.Config[part.Name].Default))
			case contract.EnvUses:
				v := r.bound[i][part.Name][part.Field]
				if v.raw {
					b.WriteString(v.text)
				} else {
					b.WriteString(escape(v.text))
				}
			default:
				return fmt.Errorf("%w: %s in the environment of module %s", ErrNotRendered, part.Text, m.Name)
			}
		}
		setEnvRaw(&svc, name, b.String())
	}
	r.project.Services[i] = svc
	return nil
}

// harden writes the hardening defaults and the entry's opt outs (spec 0013).
func (r *renderer) harden(svc *types.ServiceConfig, h *contract.Hardening) {
	svc.ReadOnly = true
	svc.CapDrop = []string{"ALL"}
	svc.SecurityOpt = []string{"no-new-privileges:true"}
	svc.User = runUser
	if h == nil {
		return
	}
	switch h.User {
	case contract.Root:
		svc.User = "0:0"
	case contract.Image:
		svc.User = ""
	}
	if h.WritableRoot {
		svc.ReadOnly = false
	}
	svc.CapAdd = slices.Clone(h.CapAdd)
	svc.Tmpfs = slices.Clone(h.Tmpfs)
}

// host is the one name instance i goes by on every network: <i>.<graph>.internal.
func (r *renderer) host(i string) string { return i + "." + domain(r.g.Name) }

// domain is the MP_DOMAIN of every instance of graph: <graph>.internal, a name ICANN reserves
// for private use.
func domain(graph string) string { return graph + ".internal" }

// marker is the role a provisioner marks what this stack made with (spec 0014):
// mp_provisioned_ plus 8 hex digits of SHA-256 of the graph name.
func marker(graph string) string {
	sum := sha256.Sum256([]byte(graph))
	return "mp_provisioned_" + hex.EncodeToString(sum[:])[:8]
}

// mountSecret declares the Compose secret name for the file at path once and mounts it on svc
// at target.
func (r *renderer) mountSecret(svc *types.ServiceConfig, name, path, target string) {
	if r.project.Secrets == nil {
		r.project.Secrets = types.Secrets{}
	}
	r.project.Secrets[name] = types.SecretConfig{File: "./" + path}
	svc.Secrets = append(svc.Secrets, types.ServiceSecretConfig{Source: name, Target: target})
}

// generated lists path once as a file setup.sh (or the deployer) creates.
func (r *renderer) generated(instance, path string) {
	if r.declared[path] {
		return
	}
	r.declared[path] = true
	r.data.Secrets = append(r.data.Secrets, secret{Instance: instance, Path: path})
}

// placed lists path once as a file you place.
func (r *renderer) placed(instance, path, capability, field, description string) {
	if r.declared[path] {
		return
	}
	r.declared[path] = true
	r.data.Placed = append(r.data.Placed, secret{Instance: instance, Path: path, Capability: capability, Field: field, Description: description})
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

// setEnv sets a literal env value; a $ is escaped, so Compose never reads it as a variable.
func setEnv(svc *types.ServiceConfig, name, value string) {
	setEnvRaw(svc, name, escape(value))
}

// setEnvRaw sets an env value as written, for the Compose variables the render itself names.
func setEnvRaw(svc *types.ServiceConfig, name, value string) {
	if svc.Environment == nil {
		svc.Environment = types.MappingWithEquals{}
	}
	svc.Environment[name] = &value
}

// escape doubles every $, Compose's escape for a literal dollar sign.
func escape(s string) string { return strings.ReplaceAll(s, "$", "$$") }

func escapeAll(list []string) []string {
	out := make([]string, len(list))
	for i, s := range list {
		out[i] = escape(s)
	}
	return out
}

// healthcheck copies a catalog healthcheck; the catalog rules already checked its durations.
func healthcheck(h *contract.Healthcheck) *types.HealthCheckConfig {
	if h == nil {
		return nil
	}
	out := &types.HealthCheckConfig{Test: types.HealthCheckTest(escapeAll(h.Test))}
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
		r := uint64(*h.Retries) //nolint:gosec // CheckModule refuses retries below 1
		out.Retries = &r
	}
	return out
}

// composeYAML writes the project from compose-go types with keys in sorted order. compose-go
// writes unset command and entrypoint as null, which Compose could read as a reset, so every
// null is dropped first; so is the empty ipam it writes on every network.
func composeYAML(p types.Project) ([]byte, error) {
	raw, err := json.Marshal(p)
	if err != nil {
		return nil, fmt.Errorf("render compose: %w", err)
	}
	var tree any
	if err := json.Unmarshal(raw, &tree); err != nil {
		return nil, fmt.Errorf("render compose: %w", err)
	}
	if networks, ok := tree.(map[string]any)["networks"].(map[string]any); ok {
		for _, n := range networks {
			if n, ok := n.(map[string]any); ok {
				if ipam, ok := n["ipam"].(map[string]any); ok && len(ipam) == 0 {
					delete(n, "ipam")
				}
			}
		}
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

func sortedKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	return keys
}
