package render

import (
	"fmt"
	"slices"
	"strings"

	"github.com/compose-spec/compose-go/v2/types"

	"github.com/racherom/modular-platform/libs/contract"
)

// The render additions for modules built on the sdk, plain mode only (spec 0005, "Render
// additions").
const (
	// moduleEnvPath is where an sdk instance's config/<instance>.env is mounted.
	moduleEnvPath = "/etc/modular-platform/module.env"
	// sdkUser runs sdk containers as the user who ran setup.sh, so the 0600 secret files that
	// Compose mounts with the host file's owner stay readable.
	sdkUser = "${MP_UID}:${MP_GID}"
	// sdkPort is where a runtime: sdk module listens in plain mode.
	sdkPort = 8080
)

// sdkInstance gives sdk instance i its bootstrap env, its host name as a network alias, its user,
// its env file mount and its generated secret values.
func sdkInstance(p *types.Project, svc *types.ServiceConfig, data *templateData, i string, e contract.CatalogEntry) {
	setEnv(svc, "MP_INSTANCE", i)
	setEnv(svc, "MP_DOMAIN", sdkDomain(p.Name))
	setEnv(svc, "MP_ENV_FILE", moduleEnvPath)
	// default is the only network the static render writes; the bare service name keeps resolving
	svc.Networks = map[string]*types.ServiceNetworkConfig{"default": {Aliases: []string{sdkHost(p.Name, i)}}}
	svc.User = sdkUser

	config := i + "_env"
	if p.Configs == nil {
		p.Configs = types.Configs{}
	}
	p.Configs[config] = types.ConfigObjConfig{File: "./" + envFilePath(i)}
	svc.Configs = append(svc.Configs, types.ServiceConfigObjConfig{Source: config, Target: moduleEnvPath})

	for _, name := range sortedKeys(e.Config) {
		v := e.Config[name]
		if v.Type != contract.ConfigValueTypeSecret || !v.Generate {
			continue
		}
		sec := secret{Instance: i, Path: "secrets/" + i + "/" + name, Compose: i + "_" + name}
		addSecret(p, svc, sec, "")
		setEnv(svc, v.Env+"_FILE", "/run/secrets/"+sec.Compose)
		data.Secrets = append(data.Secrets, sec)
	}
	data.SDK = true
	data.EnvFiles = append(data.EnvFiles, envFilePath(i))
}

func envFilePath(i string) string { return "config/" + i + ".env" }

// sdkDomain is the MP_DOMAIN of every sdk instance in graph: <graph>.internal, a name ICANN
// reserves for private use.
func sdkDomain(graph string) string { return graph + ".internal" }

// sdkHost is the one name sdk instance i goes by: its network alias, the host of binding URLs
// that point at it, and later its certificate name.
func sdkHost(graph, i string) string { return i + "." + sdkDomain(graph) }

// sdkBinding writes what a binding needs beyond 0002's host and port: the url, the per binding
// token on both sides, the credential files the consumer reads, and sslmode.
func sdkBinding(p *types.Project, data *templateData, b contract.Binding, consumer, provider contract.CatalogEntry) {
	c, cp, pv := b.Consumer, b.Capability, b.Provider
	use := consumer.Uses[cp]
	svc := p.Services[c]
	if use.Env.Url != "" {
		setEnv(&svc, use.Env.Url, bindingURL(p.Name, provider, pv, cp, use.Client))
	}
	if provide := provider.Provides[cp]; provide.Auth == contract.BindingToken && use.Env.TokenFile != "" {
		sec := secret{Instance: c, Path: "secrets/" + c + "/uses_" + cp, Compose: c + "_uses_" + cp}
		token := "/run/secrets/uses/" + cp + "/token"
		addSecret(p, &svc, sec, token)
		setEnv(&svc, use.Env.TokenFile, token)

		psvc := p.Services[pv]
		clients := "/run/secrets/clients/" + cp
		addSecret(p, &psvc, sec, clients+"/"+c)
		if provide.Env != nil && provide.Env.ClientsDir != "" {
			setEnv(&psvc, provide.Env.ClientsDir, clients)
		}
		p.Services[pv] = psvc
		data.Secrets = append(data.Secrets, sec)
	}
	for _, f := range []struct{ env, suffix, file string }{
		{use.Env.PasswordFile, "_password", "password"},
		{use.Env.SecretAccessKeyFile, "_secret_key", "secret_key"},
	} {
		if f.env == "" {
			continue
		}
		sec := secret{
			Instance: c, Capability: cp, Field: f.file,
			Path: "secrets/" + c + "/uses_" + cp + f.suffix, Compose: c + "_uses_" + cp + f.suffix,
		}
		target := "/run/secrets/uses/" + cp + "/" + f.file
		addSecret(p, &svc, sec, target)
		setEnv(&svc, f.env, target)
		data.Placed = append(data.Placed, sec)
	}
	if use.Env.Sslmode != "" {
		setEnv(&svc, use.Env.Sslmode, "disable") // no unchanged provider has TLS before #3
	}
	p.Services[c] = svc
}

// bindingURL is where a consumer reaches capability on provider: an sdk module by its host name
// on 8080, else the instance name on the entry's own port, with the scheme of the consumer's
// client kind.
func bindingURL(graph string, e contract.CatalogEntry, provider, capability string, client contract.UseClient) string {
	if e.Runtime == contract.Sdk {
		return fmt.Sprintf("http://%s:%d", sdkHost(graph, provider), sdkPort)
	}
	scheme := "http"
	if client == contract.Postgresql {
		scheme = "postgres"
	}
	return fmt.Sprintf("%s://%s:%d", scheme, provider, e.Provides[capability].Port)
}

// addSecret declares sec once in the project and mounts it on svc, at target or, when target
// is empty, at Compose's default /run/secrets/<name>.
func addSecret(p *types.Project, svc *types.ServiceConfig, sec secret, target string) {
	if p.Secrets == nil {
		p.Secrets = types.Secrets{}
	}
	p.Secrets[sec.Compose] = types.SecretConfig{File: "./" + sec.Path}
	svc.Secrets = append(svc.Secrets, types.ServiceSecretConfig{Source: sec.Compose, Target: target})
}

// credentialKeys are the binding keys a consumer's env file asks you to fill, in the order of
// the data model: the render has no source for them until provisioning (#3).
var credentialKeys = []struct {
	name string
	env  func(contract.UseEnv) string
}{
	{"database", func(e contract.UseEnv) string { return e.Database }},
	{"user", func(e contract.UseEnv) string { return e.User }},
	{"bucket", func(e contract.UseEnv) string { return e.Bucket }},
	{"region", func(e contract.UseEnv) string { return e.Region }},
	{"access_key_id", func(e contract.UseEnv) string { return e.AccessKeyId }},
}

// envFile writes config/<i>.env: one block per value in name order (secrets never), then one
// per binding in capability order with the credential keys you fill.
func envFile(i string, e contract.CatalogEntry, bindings []contract.Binding) []byte {
	blocks := []string{fmt.Sprintf("# Settings for %s (module %s). Edit and restart the module to apply.\n"+
		"# Lines starting with # are defaults or optional values; remove the # to set one.\n", i, e.Name)}
	for _, name := range sortedKeys(e.Config) {
		v := e.Config[name]
		if v.Type == contract.ConfigValueTypeSecret {
			continue
		}
		var line string
		switch {
		case v.Default != "":
			line = fmt.Sprintf("# %s=%s", v.Env, v.Default)
		case v.Optional:
			line = fmt.Sprintf("# optional\n# %s=", v.Env)
		default:
			line = fmt.Sprintf("# required\n%s=", v.Env)
		}
		blocks = append(blocks, fmt.Sprintf("# %s\n%s\n", v.Description, line))
	}
	for _, b := range bindings {
		var block strings.Builder
		for _, k := range credentialKeys {
			if env := k.env(e.Uses[b.Capability].Env); env != "" {
				fmt.Fprintf(&block, "%s=\n", env)
			}
		}
		if block.Len() > 0 {
			blocks = append(blocks, fmt.Sprintf("# Binding uses.%s to %s: required, from your %s setup\n%s", b.Capability, b.Provider, b.Provider, block.String()))
		}
	}
	return []byte(strings.Join(blocks, "\n"))
}

// consumerBindings returns the bindings whose consumer is i, in capability order.
func consumerBindings(bindings []contract.Binding, i string) []contract.Binding {
	var out []contract.Binding
	for _, b := range bindings {
		if b.Consumer == i {
			out = append(out, b)
		}
	}
	slices.SortFunc(out, func(a, b contract.Binding) int { return strings.Compare(a.Capability, b.Capability) })
	return out
}

func sortedKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	return keys
}
