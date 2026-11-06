package render

import (
	"fmt"
	"path"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"github.com/compose-spec/compose-go/v2/types"

	"github.com/racherom/modular-platform/libs/contract"
)

// sdkPort is where a runtime: sdk module listens in plain mode.
const sdkPort = 8080

// sdkTLSPort is where a runtime: sdk module listens when its acme slot is bound (spec 0005).
const sdkTLSPort = 8443

// The job labels of a one shot service, and of the wait anchor (spec 0013, "Labels").
const (
	jobProvision = "provision"
	jobRevoke    = "revoke"
	jobWait      = "wait"
)

// waitAnchorName is the service that waits for every job nothing else depends on (spec 0014,
// "Waiting"). Instance names hold no _, and it fits no job name pattern, so it meets no other
// service name.
const waitAnchorName = "mp_jobs"

// value is what one field of a binding resolves to: text (a literal, or a Compose variable
// reference when raw), the project relative path of a file, or a file name in a folder of an
// instance's data volume, which may appear only after that instance starts (a {volume} source).
type value struct {
	text string
	raw  bool
	file string

	folder string // data/<owner>/<volume>/<dir>, mounted read only as a whole
	name   string // the file in folder
	owner  string // the instance whose volume holds folder
}

// isText reports whether v is a text value known when the plan is made: a file is a secret, a
// volume file appears at run time, and a Compose variable is known only at run time.
func (v value) isText() bool { return v.file == "" && v.folder == "" && !v.raw }

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
		case k.Fields[f].IsFile() && v.folder != "":
			if mount != "" {
				return fmt.Errorf("%w: a fixed path for field %s of slot %s of %s, whose value is a file in a volume", ErrNotRendered, f, b.Slot, b.Consumer)
			}
			// the folder, not the file: the file may appear after the owner starts (spec 0013)
			dir := "/run/volumes/" + b.Slot + "/" + f + "/"
			consumer.Volumes = append(consumer.Volumes, types.ServiceVolumeConfig{
				Type: types.VolumeTypeBind, Source: "./" + v.folder, Target: dir, ReadOnly: true,
			})
			if env != "" {
				setEnv(&consumer, env, dir+v.name)
			}
			if v.owner != b.Consumer && v.owner != b.Provider {
				dependOn(&consumer, v.owner, condition(r.modules[r.g.Modules[v.owner].Module]))
			}
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
	} else {
		r.joinEgress(&consumer) // the bound host is outside the stack, and internal has no route out
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
			if r.flavor == flavorManaged {
				return nil, fmt.Errorf("%w: job outputs of %s on a deployed graph (spec 0011, follow up #31)", ErrNotRendered, e.offered)
			}
			values[f] = value{file: r.output(e.b, f)}
		}
	}
	key := e.b.Provider + "." + e.b.Provide
	if y.Login != "" {
		r.keep[key] = append(r.keep[key], named[y.Login])
	}
	r.provisions[key] = append(r.provisions[key], provisionJobName(e.b))
	r.provisionJob(e, admin, adminValues, named)
	return values, nil
}

// adminValues resolves the fields of provisioner provide q of instance p once; the provision
// jobs and the revoke job of that provide share them.
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
	port, scheme := r.address(p, e.provide, k)
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
		return value{text: fmt.Sprintf("%s://%s:%d%s", scheme, r.host(p), port, src.Path)}, true, nil
	case src.Platform == contract.FieldSourcePlatformConsumer:
		return value{text: e.b.Consumer}, true, nil
	case src.Platform == contract.FieldSourcePlatformTls, src.Platform == contract.FieldSourcePlatformCa:
		slot, tls, err := r.tlsSlot(p)
		switch {
		case err != nil:
			return value{}, false, err
		case !tls && src.Platform == contract.FieldSourcePlatformCa:
			return value{}, false, nil // no TLS, so no root CA to trust
		case !tls:
			return value{text: src.Off}, true, nil
		case src.Platform == contract.FieldSourcePlatformTls:
			return value{text: src.On}, true, nil
		}
		ca, ok := r.bound[p][slot]["ca"]
		return ca, ok, nil
	case src.Uses != "" && src.Field != "":
		v, ok := r.bound[p][src.Uses][src.Field] // delivered first: providers come before consumers
		return v, ok, nil
	case src.Uses != "":
		return r.usesFormat(r.bound[p][src.Uses], src.Format), true, nil
	case src.Volume != "":
		dir, name := path.Split(src.Path)
		folder := path.Join("data", p, src.Volume, dir)
		if !slices.Contains(r.data.Folders, folder) {
			r.data.Folders = append(r.data.Folders, folder) // a bind source must exist before up
		}
		return value{folder: folder, name: name, owner: p}, true, nil
	case src.Value != "":
		return value{text: src.Value}, true, nil
	case src.Config != "":
		v := r.composeVar(p, src.Config, m.Config[src.Config])
		if src.Format != "" {
			v = strings.ReplaceAll(escape(src.Format), "{}", v)
		}
		return value{text: v, raw: true}, true, nil
	case src.Secret != "":
		return value{file: "secrets/" + p + "/" + src.Secret}, true, nil
	case src.Generate:
		return value{file: r.perBinding(e.b, f)}, true, nil
	}
	return value{}, false, fmt.Errorf("render: field %s of %s.%s has a source this release does not know", f, p, e.b.Provide)
}

// usesFieldRe finds the {<field>} references of a {uses, format} source (spec 0013).
var usesFieldRe = regexp.MustCompile(`\{([a-z][a-z0-9_]*)\}`)

// usesFormat fills format's {<field>} references from the fields of a binding (spec 0013, a
// {uses, format} source); a field the binding does not give is empty. The result is a Compose
// reference when any field it names is one, with the rest escaped.
func (r *renderer) usesFormat(fields map[string]value, format string) value {
	refs := usesFieldRe.FindAllStringSubmatchIndex(format, -1)
	raw := false
	for _, loc := range refs {
		raw = raw || fields[format[loc[2]:loc[3]]].raw
	}
	quote := func(s string) string { return s }
	if raw {
		quote = escape // a literal beside a Compose reference must not read as one
	}
	var b strings.Builder
	last := 0
	for _, loc := range refs {
		b.WriteString(quote(format[last:loc[0]]))
		if v := fields[format[loc[2]:loc[3]]]; v.raw {
			b.WriteString(v.text)
		} else {
			b.WriteString(quote(v.text))
		}
		last = loc[1]
	}
	b.WriteString(quote(format[last:]))
	return value{text: b.String(), raw: raw}
}

// tlsSlot names the slot instance p takes its serving certificate from, if it is bound: the
// implicit acme slot of a runtime: sdk instance, which then serves TLS on 8443, or a bound
// tls-certificate slot, whose certificate and key an unchanged server reads from files (spec
// 0013, 0014).
func (r *renderer) tlsSlot(p string) (string, bool, error) {
	gm := r.g.Modules[p]
	if gm.External != nil || gm.Uses == nil {
		return "", false, nil
	}
	m := r.modules[gm.Module]
	slots := m.Slots()
	for _, s := range sortedKeys(*gm.Uses) {
		if (*gm.Uses)[s] == nil {
			continue
		}
		switch slots[s].Capability {
		case "tls-certificate":
			return s, true, nil
		case "acme":
			if m.Runtime == contract.Sdk && s == contract.ACMESlot {
				return s, true, nil
			}
		}
	}
	return "", false, nil
}

// address is the port and URL scheme provide q of instance p is reached at: its declared port and
// the capability's scheme, or for a runtime: sdk instance 8080 and http, or 8443 and https when
// its acme slot is bound (spec 0013, Field sources).
func (r *renderer) address(p string, q contract.Provide, k contract.Capability) (int, string) {
	if r.modules[r.g.Modules[p].Module].Runtime != contract.Sdk {
		return q.Port, k.Scheme
	}
	if _, tls, _ := r.tlsSlot(p); tls {
		return sdkTLSPort, "https"
	}
	return sdkPort, "http"
}

// output is the file a provision job writes for field f of binding b, secrets/<consumer>/
// uses_<slot>_<field>: setup.sh makes it empty (mode 0600) so Compose finds it at the first up, and
// the job fills it in place, so the consumer's mount sees every reissue (spec 0014).
func (r *renderer) output(b contract.Binding, f string) string {
	path := "secrets/" + b.Consumer + "/uses_" + b.Slot + "_" + f
	if !r.declared[path] {
		r.declared[path] = true
		r.data.Secrets = append(r.data.Secrets, secret{Instance: b.Consumer, Path: path, PerBinding: true, Output: true})
	}
	return path
}

// perBinding is the file of a per binding generated field, created by setup.sh or the deployer:
// secrets/<consumer>/uses_<slot>_<field>.
func (r *renderer) perBinding(b contract.Binding, f string) string {
	path := "secrets/" + b.Consumer + "/uses_" + b.Slot + "_" + f
	r.generated(b.Consumer, path, true)
	return path
}

// composeVar is the Compose variable of config value v of instance i with its default (spec
// 0025, AC-25): the graph's text, else the catalog default, else empty, with $ written $$. An
// asked value the graph does not set stays bare, so a missing answer fails up; {uid} and {gid}
// defaults take the setup user's variables.
func (r *renderer) composeVar(i, v string, cv contract.ConfigValue) string {
	name := contract.ComposeVar(i, v)
	text, set := r.graphValue(i, v)
	switch {
	case set:
		return "${" + name + ":-" + escape(text) + "}"
	case cv.Asked():
		return "${" + name + "}"
	case tokenDefault(cv.Default):
		return "${" + name + ":-" + tokenText(cv.Default) + "}"
	}
	return "${" + name + ":-" + escape(cv.Default) + "}"
}

// graphValue is the text the graph sets as config value v of instance i, if it sets one (spec
// 0025). The planner has checked it, so it holds no control character, { or }.
func (r *renderer) graphValue(i, v string) (string, bool) {
	c := r.g.Modules[i].Config
	if c == nil {
		return "", false
	}
	text, ok := (*c)[v]
	return text, ok
}

func envPart(s string) string { return strings.ToUpper(strings.ReplaceAll(s, "-", "_")) }

// provisionJobName is the provision job of binding b: <provider>_provision_<consumer>_<slot>.
// Names hold no _, so it is unique and never an instance name.
func provisionJobName(b contract.Binding) string {
	return b.Provider + "_provision_" + b.Consumer + "_" + b.Slot
}

// revokeJobName is the revoke job of provide q of instance p: <p>_revoke_<q>.
func revokeJobName(p, q string) string { return p + "_revoke_" + q }

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
		switch y.Fields[f].Kind {
		case contract.YieldGenerate:
			target := "/run/secrets/bind/" + f
			r.mountSecret(&svc, b.Consumer+"_uses_"+b.Slot+"_"+f, r.perBinding(b, f), target)
			setEnv(&svc, "MP_"+strings.ToUpper(f)+"_FILE", target)
		case contract.YieldOutput:
			// a bind mount, not a secret: the job writes it
			svc.Volumes = append(svc.Volumes, types.ServiceVolumeConfig{
				Type: types.VolumeTypeBind, Source: "./" + r.output(b, f), Target: "/out/" + f,
			})
		}
	}
	r.project.Services[name] = svc
	r.jobs[name] = job{instance: b.Provider, module: e.module.Name, kind: jobProvision}
}

// revokeJobs adds one revoke job per provisioner provide whose capability names a login,
// whether or not anything binds to it (spec 0014): it disables every login this stack made on
// that provide that no current binding keeps. It runs after every provision job of the provide.
func (r *renderer) revokeJobs() error {
	for _, p := range sortedKeys(r.g.Modules) {
		for _, q := range r.provides(p) {
			e := r.end(p, q)
			admin := r.caps[e.offered]
			if admin.Yields == nil || admin.Yields.Login == "" {
				continue
			}
			adminValues, err := r.adminValues(p, q)
			if err != nil {
				return err
			}
			key := p + "." + q
			keep := slices.Clone(r.keep[key])
			slices.Sort(keep)
			name := revokeJobName(p, q)
			svc := r.jobService(name, e, admin, adminValues, admin.Yields.Job.Revoke)
			setEnv(&svc, "MP_KEEP", strings.Join(keep, ","))
			for _, j := range r.provisions[key] {
				dependOn(&svc, j, types.ServiceConditionCompletedSuccessfully)
			}
			r.project.Services[name] = svc
			r.jobs[name] = job{instance: p, module: e.module.Name, kind: jobRevoke}
		}
	}
	return nil
}

// provides lists the provide names of instance p in name order.
func (r *renderer) provides(p string) []string {
	if gm := r.g.Modules[p]; gm.External != nil {
		return sortedKeys(gm.External.Provides)
	}
	return sortedKeys(r.modules[r.g.Modules[p].Module].Provides)
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
		r.joinEgress(&svc)
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

// waitAnchor adds the service mp_jobs when some job has no dependent (spec 0014, "Waiting"):
// docker compose up --wait waits for a job correctly only through a depends_on with
// service_completed_successfully, so mp_jobs gives each such job one. It idles in the first such
// job's image with no network, so it holds nothing and reaches nothing.
func (r *renderer) waitAnchor() {
	waited := map[string]bool{}
	for _, svc := range r.project.Services {
		for dep := range svc.DependsOn {
			waited[dep] = true
		}
	}
	var alone []string
	for _, name := range sortedKeys(r.jobs) {
		if !waited[name] {
			alone = append(alone, name)
		}
	}
	if len(alone) == 0 {
		return
	}
	svc := types.ServiceConfig{Name: waitAnchorName}
	svc.Image = r.project.Services[alone[0]].Image
	svc.Entrypoint = []string{"sleep", "infinity"}
	svc.Init = new(true) // so it stops at once on down
	svc.NetworkMode = "none"
	r.harden(&svc, nil)
	if r.flavor != flavorStatic {
		svc.Restart = types.RestartPolicyUnlessStopped
	}
	for _, j := range alone {
		dependOn(&svc, j, types.ServiceConditionCompletedSuccessfully)
	}
	r.project.Services[waitAnchorName] = svc
	r.jobs[waitAnchorName] = job{kind: jobWait}
	r.data.WaitAnchor = true
}

// joinEgress joins svc to the egress network with the aliases it has on internal, if it has not
// joined it yet.
func (r *renderer) joinEgress(svc *types.ServiceConfig) {
	r.egress = true
	if _, ok := svc.Networks[networkEgress]; ok {
		return
	}
	var aliases []string
	if n := svc.Networks[networkInternal]; n != nil {
		aliases = slices.Clone(n.Aliases)
	}
	svc.Networks[networkEgress] = &types.ServiceNetworkConfig{Aliases: aliases}
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
