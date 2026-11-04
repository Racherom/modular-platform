package render

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/compose-spec/compose-go/v2/types"

	"github.com/racherom/modular-platform/libs/contract"
)

// sdkPort is where a runtime: sdk module listens in plain mode.
const sdkPort = 8080

// jobProvision is the job label of a provision job (spec 0013, "Labels"); the revoke job waits
// on a spec change for how `up` waits on a job nothing depends on (build task 10).
const jobProvision = "provision"

// value is what one field of a binding resolves to: text (a literal, or a Compose variable
// reference when raw), or the project relative path of a file.
type value struct {
	text string
	raw  bool
	file string
}

// end is the provider side of a binding as the render sees it: one provide of a module instance
// or of an external instance. b names the binding; for a provide's admin fields it names only
// the provider and provide.
type end struct {
	b        contract.Binding
	offered  string                    // the provide's capability
	external *contract.ExternalProvide // set when the provider is an external instance
	module   contract.Module           // the provider's module otherwise
	provide  contract.Provide
}

// end returns provide q of instance p.
func (r *renderer) end(p, q string) end {
	e := end{b: contract.Binding{Provider: p, Provide: q}}
	if pg := r.g.Modules[p]; pg.External != nil {
		ep := pg.External.Provides[q]
		e.external, e.offered = &ep, ep.Capability
	} else {
		e.module = r.modules[pg.Module]
		e.provide = e.module.Provides[q]
		e.offered = e.provide.Capability
	}
	return e
}

// binding delivers one binding (spec 0013, "Binding delivery"): every field the consumer maps,
// the provider side files of both and to_provider fields, depends_on, and for a binding to a
// provisioner its provision job (spec 0014).
func (r *renderer) binding(b contract.Binding) error {
	e := r.end(b.Provider, b.Provide)
	e.b = b
	k := r.caps[b.Capability]

	var (
		values map[string]value
		err    error
	)
	if b.Provisioned {
		values, err = r.provisioned(e)
	} else {
		values, err = r.values(e, k)
	}
	if err != nil {
		return err
	}
	if r.bound[b.Consumer] == nil {
		r.bound[b.Consumer] = map[string]map[string]value{}
	}
	r.bound[b.Consumer][b.Slot] = values

	consumer := r.project.Services[b.Consumer]
	slot := r.modules[r.g.Modules[b.Consumer].Module].Slots()[b.Slot]
	for _, f := range sortedKeys(k.Fields) {
		v, ok := values[f]
		env, mount := slot.Env[f], slot.Files[f]
		if !ok || !k.Fields[f].ToConsumer() || (env == "" && mount == "") {
			continue // a field the consumer does not map is not delivered
		}
		switch {
		case k.Fields[f].IsFile():
			if mount == "" {
				mount = "/run/secrets/uses/" + b.Slot + "/" + f
			}
			r.mountSecret(&consumer, b.Consumer+"_uses_"+b.Slot+"_"+f, v.file, mount)
			if env != "" {
				setEnv(&consumer, env, mount)
			}
		case v.raw:
			setEnvRaw(&consumer, env, v.text)
		default:
			setEnv(&consumer, env, v.text)
		}
	}

	if e.external == nil {
		if !b.Provisioned {
			r.clientFiles(e, k, slot, values)
		}
		dependOn(&consumer, b.Provider, condition(e.module))
	}
	if b.Provisioned {
		dependOn(&consumer, provisionJobName(b), types.ServiceConditionCompletedSuccessfully)
	}
	r.project.Services[b.Consumer] = consumer
	return nil
}

// clientFiles mounts the file fields that reach the provider of binding e.b, one file per
// consumer in the provide's clients directory /run/secrets/clients/<provide>/<field>/ (spec
// 0013): a both field from the provider's own source, a to_provider field from the consumer's
// secret. A to_provider text field reaches the provider only as template data. An external
// provider is outside the stack, so nothing is mounted on it.
func (r *renderer) clientFiles(e end, k contract.Capability, slot contract.Slot, values map[string]value) {
	b := e.b
	provider := r.project.Services[b.Provider]
	for _, f := range sortedKeys(k.Fields) {
		field := k.Fields[f]
		if !field.ToProvider() || !field.IsFile() {
			continue
		}
		var file string
		if field.Direction == contract.Both {
			file = values[f].file
		} else if src, ok := slot.Fields[f]; ok {
			file = "secrets/" + b.Consumer + "/" + src.Secret
		}
		if file == "" {
			continue // an optional field the binding leaves out
		}
		dir := "/run/secrets/clients/" + b.Provide + "/" + f
		r.mountSecret(&provider, b.Consumer+"_uses_"+b.Slot+"_"+f, file, dir+"/"+b.Consumer)
		if env := e.provide.Env[f]; env != "" {
			setEnv(&provider, env, dir)
		}
	}
	r.project.Services[b.Provider] = provider
}

// provisioned resolves the fields a provisioner yields for binding e.b (spec 0014: admin
// fields, named values, per binding files) and adds the binding's provision job.
func (r *renderer) provisioned(e end) (map[string]value, error) {
	admin := r.caps[e.offered]
	adminValues, err := r.adminValues(e.b.Provider, e.b.Provide)
	if err != nil {
		return nil, err
	}
	y := admin.Yields
	named := (*r.g.Modules[e.b.Consumer].Uses)[e.b.Slot].Named
	values := map[string]value{}
	for _, f := range sortedKeys(y.Fields) {
		switch src := y.Fields[f]; src.Kind {
		case contract.YieldAdmin:
			if v, ok := adminValues[src.Admin]; ok {
				values[f] = v
			}
		case contract.YieldNamed:
			values[f] = value{text: named[f]}
		case contract.YieldGenerate:
			values[f] = value{file: r.perBinding(e.b, f)}
		case contract.YieldOutput:
			return nil, fmt.Errorf("%w: job outputs of %s", ErrNotRendered, e.offered)
		}
	}
	r.provisionJob(e, admin, adminValues, named)
	return values, nil
}

// adminValues resolves the fields of provisioner provide q of instance p once; every provision
// job of that provide shares them.
func (r *renderer) adminValues(p, q string) (map[string]value, error) {
	key := p + "." + q
	if v, ok := r.admin[key]; ok {
		return v, nil
	}
	e := r.end(p, q)
	v, err := r.values(e, r.caps[e.offered])
	if err != nil {
		return nil, err
	}
	r.admin[key] = v
	return v, nil
}

// values resolves every field of capability k that reaches the consumer from the provider's
// sources; an optional field without a source has no value.
func (r *renderer) values(e end, k contract.Capability) (map[string]value, error) {
	out := map[string]value{}
	for _, f := range sortedKeys(k.Fields) {
		field := k.Fields[f]
		if !field.ToConsumer() {
			continue
		}
		if e.external != nil {
			if field.IsFile() {
				if field.Optional {
					continue
				}
				path := "secrets/" + e.b.Provider + "/" + e.b.Provide + "_" + f
				r.placed(e.b.Provider, path, e.external.Capability, f, field.Description)
				out[f] = value{file: path}
			} else if v, ok := e.external.Fields[f]; ok {
				out[f] = value{text: v}
			}
			continue
		}
		src, ok := e.provide.Fields[f]
		if !ok {
			continue
		}
		v, set, err := r.source(e, k, f, src)
		if err != nil {
			return nil, err
		}
		if set {
			out[f] = v
		}
	}
	return out, nil
}

// source resolves one provider side field source (spec 0013, "Field sources").
func (r *renderer) source(e end, k contract.Capability, f string, src contract.FieldSource) (value, bool, error) {
	p, m := e.b.Provider, e.module
	sdk := m.Runtime == contract.Sdk
	port := e.provide.Port
	if sdk {
		port = sdkPort
	}
	perBinding := src.Generate || src.Platform == contract.FieldSourcePlatformConsumer
	if perBinding && e.b.Consumer == "" {
		return value{}, false, fmt.Errorf("render: field %s of %s.%s is per binding, but a provisioner's own fields are shared by all its bindings", f, p, e.b.Provide)
	}
	switch {
	case src.Platform == contract.FieldSourcePlatformHost:
		return value{text: r.host(p)}, true, nil
	case src.Platform == contract.FieldSourcePlatformPort:
		return value{text: strconv.Itoa(port)}, true, nil
	case src.Platform == contract.FieldSourcePlatformUrl:
		scheme := k.Scheme
		if sdk {
			scheme = "http"
		}
		return value{text: fmt.Sprintf("%s://%s:%d%s", scheme, r.host(p), port, src.Path)}, true, nil
	case src.Platform == contract.FieldSourcePlatformConsumer:
		return value{text: e.b.Consumer}, true, nil
	case src.Platform == contract.FieldSourcePlatformTls, src.Platform == contract.FieldSourcePlatformCa:
		if r.servesTLS(p) {
			return value{}, false, fmt.Errorf("%w: TLS of provider %s", ErrNotRendered, p)
		}
		if src.Platform == contract.FieldSourcePlatformCa {
			return value{}, false, nil // no TLS, so no root CA to trust
		}
		return value{text: src.Off}, true, nil
	case src.Value != "":
		return value{text: src.Value}, true, nil
	case src.Config != "":
		v := composeVar(p, src.Config, m.Config[src.Config].Default)
		if src.Format != "" {
			v = strings.ReplaceAll(escape(src.Format), "{}", v)
		}
		return value{text: v, raw: true}, true, nil
	case src.Secret != "":
		return value{file: "secrets/" + p + "/" + src.Secret}, true, nil
	case src.Generate:
		return value{file: r.perBinding(e.b, f)}, true, nil
	}
	return value{}, false, fmt.Errorf("%w: field %s of %s.%s takes its value from another binding or a volume", ErrNotRendered, f, p, e.b.Provide)
}

// servesTLS reports whether instance p binds a certificate source, an acme or tls-certificate
// slot; its TLS fields are rendered from a later release.
func (r *renderer) servesTLS(p string) bool {
	gm := r.g.Modules[p]
	if gm.Uses == nil {
		return false
	}
	slots := r.modules[gm.Module].Slots()
	for s, b := range *gm.Uses {
		if b == nil {
			continue
		}
		if c := slots[s].Capability; c == "acme" || c == "tls-certificate" {
			return true
		}
	}
	return false
}

// perBinding is the file of a per binding generated field, created by setup.sh or the deployer:
// secrets/<consumer>/uses_<slot>_<field>.
func (r *renderer) perBinding(b contract.Binding, f string) string {
	path := "secrets/" + b.Consumer + "/uses_" + b.Slot + "_" + f
	r.generated(b.Consumer, path)
	return path
}

// composeVar is the Compose variable of config value v of instance i: MP_<I>__<V>, upper case,
// - as _ (spec 0013), with the value's default.
func composeVar(i, v, def string) string {
	name := "MP_" + envPart(i) + "__" + envPart(v)
	if def == "" {
		return "${" + name + "}"
	}
	return "${" + name + ":-" + escape(def) + "}"
}

func envPart(s string) string { return strings.ToUpper(strings.ReplaceAll(s, "-", "_")) }

// provisionJobName is the provision job of binding b: <provider>_provision_<consumer>_<slot>.
// Names hold no _, so it is unique and never an instance name.
func provisionJobName(b contract.Binding) string {
	return b.Provider + "_provision_" + b.Consumer + "_" + b.Slot
}

// provisionJob adds the one shot service that makes what binding e.b names (spec 0014): the
// admin fields, the binding's consumer, slot and named values, and its generated files.
func (r *renderer) provisionJob(e end, admin contract.Capability, adminValues map[string]value, named map[string]string) {
	b, y := e.b, admin.Yields
	name := provisionJobName(b)
	svc := r.jobService(name, e, admin, adminValues, y.Job.Provision)
	setEnv(&svc, "MP_CONSUMER", b.Consumer)
	setEnv(&svc, "MP_SLOT", b.Slot)
	setEnv(&svc, "MP_CONSUMER_HOST", r.host(b.Consumer))
	for _, f := range y.Named {
		setEnv(&svc, "MP_"+strings.ToUpper(f), named[f])
	}
	for _, f := range sortedKeys(y.Fields) {
		if y.Fields[f].Kind != contract.YieldGenerate {
			continue
		}
		target := "/run/secrets/bind/" + f
		r.mountSecret(&svc, b.Consumer+"_uses_"+b.Slot+"_"+f, r.perBinding(b, f), target)
		setEnv(&svc, "MP_"+strings.ToUpper(f)+"_FILE", target)
	}
	r.project.Services[name] = svc
	r.jobs[name] = job{instance: b.Provider, module: e.module.Name, kind: jobProvision}
}

// jobService is what every job of provisioner provide e shares (spec 0014): the capability's job
// image running script, hardening defaults, the internal network (plus egress to reach an
// external provider), the stack's marker, the admin fields, and depends_on the provider.
func (r *renderer) jobService(name string, e end, admin contract.Capability, adminValues map[string]value, script string) types.ServiceConfig {
	b := e.b
	svc := types.ServiceConfig{Name: name}
	svc.Image = admin.Yields.Job.Image
	svc.Entrypoint = []string{"/bin/sh", "-eu", "-c"}
	svc.Command = []string{escape(script)}
	svc.Restart = types.RestartPolicyNo
	svc.Networks = map[string]*types.ServiceNetworkConfig{networkInternal: {}}
	if e.external != nil {
		svc.Networks[networkEgress] = &types.ServiceNetworkConfig{}
		r.egress = true
	}
	r.harden(&svc, nil)
	setEnv(&svc, "MP_MARKER", marker(r.g.Name))
	for _, f := range sortedKeys(adminValues) {
		v := adminValues[f]
		env := "MP_ADMIN_" + strings.ToUpper(f)
		switch {
		case admin.Fields[f].IsFile():
			target := "/run/secrets/admin/" + f
			r.mountSecret(&svc, b.Provider+"_admin_"+b.Provide+"_"+f, v.file, target)
			setEnv(&svc, env+"_FILE", target)
		case v.raw:
			setEnvRaw(&svc, env, v.text)
		default:
			setEnv(&svc, env, v.text)
		}
	}
	if e.external == nil {
		dependOn(&svc, b.Provider, condition(e.module))
	}
	return svc
}

// condition is what a dependant waits for: healthy when the module has a healthcheck, else
// started.
func condition(m contract.Module) string {
	if m.Healthcheck != nil {
		return types.ServiceConditionHealthy
	}
	return types.ServiceConditionStarted
}

func dependOn(svc *types.ServiceConfig, service, cond string) {
	if svc.DependsOn == nil {
		svc.DependsOn = types.DependsOnConfig{}
	}
	svc.DependsOn[service] = types.ServiceDependency{Condition: cond, Required: true}
}
