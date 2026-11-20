package render

import (
	"cmp"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/compose-spec/compose-go/v2/types"

	"github.com/racherom/modular-platform/libs/contract"
)

// The problems of a keeper render (spec 0016, "Contract changes").
const (
	// CodeKeeperMissing refuses a platform graph with no instance of a keeper module.
	CodeKeeperMissing = "keeper_missing"
	// CodeKeeperDuplicate refuses a platform graph with more than one.
	CodeKeeperDuplicate = "keeper_duplicate"
)

// KeeperImageVar is the Compose variable that replaces the keeper's catalog image in keeper.yml
// and in the seed setup.sh starts, for dev and CI; a release leaves it unset.
const KeeperImageVar = "MP_KEEPER_IMAGE"

// PlatformUID is the fixed user and group id the platform's files belong to and its modules run
// as: the distroless nonroot user (spec 0016). setup.sh runs as root, so id -u would give 0.
const PlatformUID = "65532"

// keeperStartPeriod is the healthcheck start_period keeper.yml gives the keeper: its first
// pipeline may prepare and start the whole platform before it serves (spec 0016).
const keeperStartPeriod = 5 * time.Minute

// WithKeeper renders the platform flavor for the keeper (spec 0016): docker-compose.yml without
// the keeper instance, keeper.yml with only the keeper, setup.json with the install actions the
// keeper applies, and a setup.sh that runs as root, asks the values that are not secret and
// starts the seed. It refuses a graph the keeper cannot set up (RefusedError). Platform alone
// ignores it.
//
// The option exists only while milestone 4's platform output is still the default; task 4 of
// spec 0016 makes this output the default and removes WithKeeper.
func WithKeeper() Option {
	return func(o *options) { o.keeper = true }
}

// keeperRefusals lists why a keeper render refuses an ok check result (spec 0016): setup_required
// at each instance that asks a secret (a from: ask secret, a setup step with asks, an asked
// config value of type secret), naming every such field, and keeper_missing or
// keeper_duplicate at modules unless exactly one instance runs a keeper module. Sorted by path
// then code.
func keeperRefusals(result contract.CheckResult, catalog contract.Catalog) []contract.Problem {
	modules := make(map[string]contract.Module, len(catalog.Modules))
	for _, m := range catalog.Modules {
		modules[m.Name] = m
	}
	var out []contract.Problem
	var keepers []string
	for _, i := range sortedKeys(result.Graph.Modules) {
		gm := result.Graph.Modules[i]
		m, ok := modules[gm.Module]
		if gm.External != nil || !ok {
			continue
		}
		if m.Keeper != nil {
			keepers = append(keepers, i)
		}
		var fields []string
		for _, s := range m.Secrets {
			if s.From == contract.SecretFromAsk {
				fields = append(fields, "secrets."+s.Name+" (from: ask)")
			}
		}
		for _, s := range m.Setup {
			if len(s.Asks) > 0 {
				fields = append(fields, "setup."+s.Name+".asks")
			}
		}
		for _, name := range sortedKeys(m.Config) {
			if v := m.Config[name]; v.Asked() && v.Type == contract.ConfigValueTypeSecret {
				fields = append(fields, "config."+name+" (an asked secret)")
			}
		}
		if len(fields) > 0 {
			out = append(out, contract.Problem{
				Severity: contract.Error, Code: CodeSetupRequired, Path: "modules." + i,
				Message: fmt.Sprintf("Instance %s asks a secret at install, which the platform never does: %s. The keeper generates every secret.", i, strings.Join(fields, ", ")),
			})
		}
	}
	switch len(keepers) {
	case 1:
	case 0:
		out = append(out, contract.Problem{
			Severity: contract.Error, Code: CodeKeeperMissing, Path: "modules",
			Message: "The platform graph has no instance of a keeper module, so nothing would start the platform.",
		})
	default:
		out = append(out, contract.Problem{
			Severity: contract.Error, Code: CodeKeeperDuplicate, Path: "modules",
			Message: fmt.Sprintf("The platform graph has %d keeper instances (%s); it needs exactly one.", len(keepers), strings.Join(keepers, ", ")),
		})
	}
	slices.SortStableFunc(out, func(a, b contract.Problem) int {
		return cmp.Or(strings.Compare(a.Path, b.Path), strings.Compare(a.Code, b.Code))
	})
	return out
}

// keeperInstance is the one instance whose module is a keeper; keeperRefusals has made sure
// there is exactly one.
func (r *renderer) keeperInstance() string {
	for _, i := range sortedKeys(r.g.Modules) {
		if gm := r.g.Modules[i]; gm.External == nil && r.modules[gm.Module].Keeper != nil {
			return i
		}
	}
	return ""
}

// splitKeeper splits the finished project into the platform project, every service but keeper
// k, and the keeper project with only k (spec 0016, "Render changes"). No platform service
// depends on the keeper, and the keeper depends on nothing: it starts in its own project once
// reconcile has brought the platform up. The keeper project has no name, joins the platform
// project's networks as external ones, runs ${MP_KEEPER_IMAGE:-<catalog image>} and gives the
// healthcheck a start_period of 5 minutes. Each project keeps only the secrets and configs its
// services mount.
func splitKeeper(p types.Project, k string) (platform, keeper types.Project) {
	platform = p
	platform.Services = types.Services{}
	for name, svc := range p.Services {
		if name == k {
			continue
		}
		if _, ok := svc.DependsOn[k]; ok {
			svc.DependsOn = withoutService(svc.DependsOn, k)
		}
		platform.Services[name] = svc
	}

	svc := p.Services[k]
	svc.DependsOn = nil
	svc.Image = "${" + KeeperImageVar + ":-" + svc.Image + "}"
	if svc.HealthCheck != nil {
		hc := *svc.HealthCheck
		start := types.Duration(keeperStartPeriod)
		hc.StartPeriod = &start
		svc.HealthCheck = &hc
	}
	keeper = types.Project{Services: types.Services{k: svc}, Networks: types.Networks{}}
	for _, n := range sortedKeys(svc.Networks) {
		keeper.Networks[n] = types.NetworkConfig{Name: p.Name + "_" + n, External: true}
	}
	platform.Secrets, platform.Configs = mounted(p, platform.Services)
	keeper.Secrets, keeper.Configs = mounted(p, keeper.Services)
	return platform, keeper
}

// withoutService returns deps without service k, nil when none is left.
func withoutService(deps types.DependsOnConfig, k string) types.DependsOnConfig {
	out := types.DependsOnConfig{}
	for name, d := range deps {
		if name != k {
			out[name] = d
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// mounted is the part of p's secrets and configs that services mount; nil when none.
func mounted(p types.Project, services types.Services) (types.Secrets, types.Configs) {
	var secrets types.Secrets
	var configs types.Configs
	for _, svc := range services {
		for _, s := range svc.Secrets {
			if secrets == nil {
				secrets = types.Secrets{}
			}
			secrets[s.Source] = p.Secrets[s.Source]
		}
		for _, c := range svc.Configs {
			if configs == nil {
				configs = types.Configs{}
			}
			configs[c.Source] = p.Configs[c.Source]
		}
	}
	return secrets, configs
}

// keeperScript is what the keeper's setup.sh needs beyond the asked values (spec 0016,
// "setup.sh (platform flavor)").
type keeperScript struct {
	Instance   string        // the keeper instance
	Module     string        // its module, the seed's module label
	Image      string        // its catalog image, the seed's image unless .env names MP_KEEPER_IMAGE
	Entrypoint string        // its entrypoint as a quoted word, "" for the image's own
	Command    string        // the keeper command as quoted words; setup.sh adds --state <dir>
	Shown      []ShownSecret // printed once at a fresh install
	OriginVar  string        // the .env variable of the origin to open, "" when unknown
	UID        string        // PlatformUID

	StackLabel, InstanceLabel, ModuleLabel string
	Stack                                  string // PlatformStack, the seed's stack label
	ImageVar                               string // KeeperImageVar
}

// keeperScript collects the keeperScript of keeper instance k and the Setup s.
func (r *renderer) keeperScript(k string, s Setup) *keeperScript {
	m := r.modules[r.g.Modules[k].Module]
	words := make([]string, len(m.Keeper.Command))
	for n, c := range m.Keeper.Command {
		words[n] = shellQuote(c)
	}
	ks := &keeperScript{
		Instance: k, Module: m.Name, Image: m.Image, Command: strings.Join(words, " "),
		Shown: s.Show, OriginVar: r.originVar(), UID: PlatformUID,
		StackLabel: LabelStack, InstanceLabel: LabelInstance, ModuleLabel: LabelModule,
		Stack: PlatformStack, ImageVar: KeeperImageVar,
	}
	if len(m.Entrypoint) > 0 {
		ks.Entrypoint = shellQuote(m.Entrypoint[0])
		rest := make([]string, 0, len(m.Entrypoint)-1+len(words))
		for _, e := range m.Entrypoint[1:] {
			rest = append(rest, shellQuote(e))
		}
		ks.Command = strings.Join(append(rest, words...), " ")
	}
	return ks
}

// originVar is the .env variable of the platform's origin: the config value the first instance,
// in name order, sources a provided origin field from. setup.sh prints it as the address to open.
func (r *renderer) originVar() string {
	for _, i := range sortedKeys(r.g.Modules) {
		gm := r.g.Modules[i]
		if gm.External != nil {
			continue
		}
		m := r.modules[gm.Module]
		for _, q := range sortedKeys(m.Provides) {
			if src, ok := m.Provides[q].Fields["origin"]; ok && src.Config != "" {
				return contract.ComposeVar(i, src.Config)
			}
		}
	}
	return ""
}
