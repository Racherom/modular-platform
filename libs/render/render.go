// Package render turns a check result into the files of a runnable Compose project: docker-compose.yml,
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

// ErrGraphHasErrors is returned for a check result that is not ok; such a check result is never rendered.
var ErrGraphHasErrors = errors.New("render: the check result has errors")

// ErrNotRendered is returned for a catalog field this release declares but does not render yet.
var ErrNotRendered = errors.New("render: not rendered by this release")

//go:embed templates/*.tmpl
var templateFS embed.FS

var projectTemplates = template.Must(template.New("").Funcs(template.FuncMap{"shellQuote": shellQuote}).ParseFS(templateFS, "templates/*.tmpl"))

// shellQuote quotes s as one POSIX shell word.
func shellQuote(s string) string { return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'" }

// zipTime is the earliest time the zip format can hold, so the same graph gives the same bytes.
var zipTime = time.Date(1980, time.January, 1, 0, 0, 0, 0, time.UTC)

// The networks of every project (spec 0013, "Host name and networks").
const (
	networkInternal = "internal" // every instance, no route out
	networkEgress   = "egress"   // instances that declare network.egress, consumers and jobs of an external instance
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
	PerBinding        bool   // generated for one binding, after the module secrets
	Output            bool   // a file a provision job writes: setup.sh makes it empty (spec 0014)
	Capability, Field string // placed only: the external provide's capability and field
	Description       string // placed only: the field's description
}

type templateData struct {
	Name         string
	Asked        []askedValue    // asked config values, instances then values in name order
	AskedSecrets []askedValue    // secrets with from: ask, in the same order
	Secrets      []secret        // the secret files setup.sh generates
	Steps        []installStep   // setup steps in binding order
	Placed       []secret        // the files you place
	EnvFiles     []string        // config/<instance>.env of each sdk instance
	Folders      []string        // data/<instance>/<volume> folders setup.sh creates
	Blocks       []envBlock      // the .env blocks, instances then values in name order
	Checks       []envCheck      // the .env checks, in the same order
	Known        []knownInstance // the variable prefix of each instance in the stack

	NeedValid    bool // setup.sh checks a value by its rules
	NeedAsk      bool // setup.sh asks at least one value
	GraphSet     bool // the graph sets an asked value, so setup.sh has graph_set and env_or
	NeedEnvGet   bool // setup.sh reads an answer back from .env
	NeedDuration bool // setup.sh checks a duration
	WaitAnchor   bool // the stack has the wait anchor mp_jobs (spec 0014, "Waiting")
	Platform     bool // the platform flavor: setup.sh writes MP_DIR, .env.example holds the blocks
}

// HasOutputs reports whether setup.sh makes a file a provision job writes.
func (d templateData) HasOutputs() bool {
	return slices.ContainsFunc(d.Secrets, func(s secret) bool { return s.Output })
}

// Render renders the resolved graph of an ok check result. catalog must hold every module and
// capability the graph uses, and templates the templates of those modules.
func Render(result contract.CheckResult, catalog contract.Catalog, templates contract.Templates) (Output, error) {
	r, err := renderProject(result, catalog, templates, flavorStatic, nil)
	if err != nil {
		return Output{}, err
	}
	return r.out, nil
}

// flavor is one of the three renders over one core (spec 0011, "Render flavors").
type flavor int

const (
	flavorStatic   flavor = iota // the zip from POST /v1/render
	flavorManaged                // a deployed graph for deployer-docker
	flavorPlatform               // the managed platform's own folder (spec 0015)
)

// rendered is a render's output plus what the managed flavor reports beside it.
type rendered struct {
	out  Output
	data templateData
	jobs map[string]job // job service name to what it does
}

// job is a one shot service of the project (spec 0014), or the wait anchor mp_jobs.
type job struct {
	instance, module, kind string // the provisioner instance and module (none on mp_jobs); provision, revoke or wait
}

// renderer holds one render in progress.
type renderer struct {
	g         contract.Graph
	flavor    flavor
	templates contract.Templates
	modules   map[string]contract.Module
	caps      map[string]contract.Capability
	project   types.Project
	data      templateData
	jobs      map[string]job
	declared  map[string]bool // secret file paths already listed for setup.sh or as placed

	bound      map[string]map[string]map[string]value // consumer, slot, field to its value
	admin      map[string]map[string]value            // provider.provide to its admin field values
	keep       map[string][]string                    // provider.provide to the logins its bindings name
	provisions map[string][]string                    // provider.provide to its provision jobs
	egress     bool                                   // some service joins the egress network
}

// renderProject renders result in flavor f: the managed and platform flavors set restart:
// unless-stopped on every long running service, and finish is called on the project before it
// is written.
func renderProject(result contract.CheckResult, catalog contract.Catalog, templates contract.Templates, f flavor, finish func(*types.Project, map[string]job)) (rendered, error) {
	if !result.Ok {
		return rendered{}, ErrGraphHasErrors
	}
	r := renderer{
		g:          result.Graph,
		flavor:     f,
		templates:  templates,
		modules:    make(map[string]contract.Module, len(catalog.Modules)),
		caps:       make(map[string]contract.Capability, len(catalog.Capabilities)),
		data:       templateData{Name: result.Graph.Name, Platform: f == flavorPlatform},
		jobs:       map[string]job{},
		declared:   map[string]bool{},
		bound:      map[string]map[string]map[string]value{},
		admin:      map[string]map[string]value{},
		keep:       map[string][]string{},
		provisions: map[string][]string{},
		project: types.Project{
			Name:     result.Graph.Name,
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
	for _, b := range deliveryOrder(instances, result.Bindings) {
		if err := r.binding(b); err != nil {
			return rendered{}, err
		}
	}
	if err := r.revokeJobs(); err != nil {
		return rendered{}, err
	}
	if err := r.steps(result.Bindings); err != nil {
		return rendered{}, err
	}
	if f == flavorPlatform {
		r.askInBindingOrder(instances, result.Bindings)
	}
	for _, i := range instances {
		if gm := r.g.Modules[i]; gm.External == nil {
			if err := r.environment(i, r.modules[gm.Module]); err != nil {
				return rendered{}, err
			}
		}
	}
	configs, err := r.templateFiles(result.Bindings)
	if err != nil {
		return rendered{}, err
	}
	r.waitAnchor()
	if r.egress {
		r.project.Networks[networkEgress] = types.NetworkConfig{}
	}
	// module secrets first, then per binding files, each in path order (spec 0013, step 4)
	slices.SortFunc(r.data.Secrets, func(a, b secret) int {
		if a.PerBinding != b.PerBinding {
			return map[bool]int{false: -1, true: 1}[a.PerBinding]
		}
		return strings.Compare(a.Path, b.Path)
	})
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
		{envName(f), "env.tmpl", 0o644},
		{"README.md", readmeTemplate(f), 0o644},
	} {
		var buf bytes.Buffer
		if err := projectTemplates.ExecuteTemplate(&buf, t.template, r.data); err != nil {
			return rendered{}, fmt.Errorf("render %s: %w", t.path, err)
		}
		files = append(files, File{Path: t.path, Mode: t.mode, Data: buf.Bytes()})
	}
	for _, i := range instances {
		if gm := r.g.Modules[i]; gm.External == nil && r.modules[gm.Module].Runtime == contract.Sdk {
			files = append(files, File{Path: envFilePath(i), Mode: 0o644, Data: envFile(i, r.modules[gm.Module], gm.Config)})
		}
	}
	files = append(files, configs...)
	slices.SortFunc(files, func(a, b File) int { return strings.Compare(a.Path, b.Path) })
	return rendered{out: Output{Name: r.project.Name, Files: files}, data: r.data, jobs: r.jobs}, nil
}

// askInBindingOrder sorts the asked values by their instance's place in the binding order, so a
// provider's answers come before its consumers' (the platform flavor, spec 0015); each
// instance's values keep their name order.
func (r *renderer) askInBindingOrder(instances []string, bindings []contract.Binding) {
	place := map[string]int{}
	for n, i := range contract.BindingOrder(instances, bindings) {
		place[i] = n
	}
	byPlace := func(a, b askedValue) int { return place[a.instance] - place[b.instance] }
	slices.SortStableFunc(r.data.Asked, byPlace)
	slices.SortStableFunc(r.data.AskedSecrets, byPlace)
}

// envName is the file the .env blocks go to: .env, or .env.example in the platform flavor, whose
// setup.sh writes .env itself, so an unzipped release never overwrites one you filled.
func envName(f flavor) string {
	if f == flavorPlatform {
		return ".env.example"
	}
	return ".env"
}

// readmeTemplate is the README of flavor f: the platform's own guide, or the stack's.
func readmeTemplate(f flavor) string {
	if f == flavorPlatform {
		return "platform-README.md.tmpl"
	}
	return "README.md.tmpl"
}

// deliveryOrder sorts bindings by their consumer's place in the binding order (providers before
// consumers, spec 0013), keeping the check result's slot order per consumer. A provider's own bindings
// are then delivered before any binding to it, so a field source that reads one of them (its
// TLS certificate source, a {uses} field) finds it resolved.
func deliveryOrder(instances []string, bindings []contract.Binding) []contract.Binding {
	place := map[string]int{}
	for n, i := range contract.BindingOrder(instances, bindings) {
		place[i] = n
	}
	out := slices.Clone(bindings)
	slices.SortStableFunc(out, func(a, b contract.Binding) int { return place[a.Consumer] - place[b.Consumer] })
	return out
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
	r.networks(&svc, i, m)
	r.harden(&svc, m.Hardening)
	if err := r.installTime(&svc, i, m); err != nil {
		return svc, err
	}

	for _, s := range m.Secrets {
		if s.From == contract.SecretFromSetup {
			r.setupSecrets(&svc, i, s)
			continue
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
		if s.From != contract.SecretFromAsk { // setup.sh asks it instead
			r.generated(i, path, false)
		}
	}
	for _, n := range sortedKeys(m.Volumes) {
		folder := "data/" + i + "/" + n
		svc.Volumes = append(svc.Volumes, types.ServiceVolumeConfig{Type: types.VolumeTypeBind, Source: "./" + folder, Target: m.Volumes[n]})
		r.data.Folders = append(r.data.Folders, folder)
	}
	if err := r.hostAccess(&svc, i, m); err != nil {
		return svc, err
	}
	if m.Runtime == contract.Sdk {
		r.sdkInstance(&svc, i, m)
	}
	if r.flavor != flavorStatic {
		svc.Restart = types.RestartPolicyUnlessStopped
	}
	return svc, nil
}

// networks joins instance i to internal, and to egress when its entry declares network.egress
// or publishes a port, with its host name and the entry's config aliases on each (spec 0013).
func (r *renderer) networks(svc *types.ServiceConfig, i string, m contract.Module) {
	aliases := []string{r.host(i)}
	joins := []string{networkInternal}
	if m.Network != nil {
		for _, a := range m.Network.Aliases {
			aliases = append(aliases, r.composeVar(i, a.Config, m.Config[a.Config]))
		}
	}
	// a published port needs a network with a route to the host: internal has none
	if m.Network != nil && m.Network.Egress || len(m.Publish) > 0 {
		joins = append(joins, networkEgress)
		r.egress = true
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
			case contract.EnvInstall:
				if r.flavor != flavorPlatform {
					return fmt.Errorf("%w: {install} in the environment of module %s belongs to the platform flavor", ErrNotRendered, m.Name)
				}
				b.WriteString("${MP_DIR}")
			case contract.EnvConfig:
				b.WriteString(r.composeVar(i, part.Name, m.Config[part.Name]))
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

// generated lists path once as a file setup.sh (or the deployer) creates; perBinding marks a
// file of one binding, which setup.sh creates after the module secrets.
func (r *renderer) generated(instance, path string, perBinding bool) {
	if r.declared[path] {
		return
	}
	r.declared[path] = true
	r.data.Secrets = append(r.data.Secrets, secret{Instance: instance, Path: path, PerBinding: perBinding})
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
