package render

import (
	"fmt"
	"slices"
	"strings"

	"github.com/compose-spec/compose-go/v2/types"

	"github.com/racherom/modular-platform/libs/contract"
)

// moduleEnvPath is where an sdk instance's config/<instance>.env is mounted (spec 0005, "Render
// additions").
const moduleEnvPath = "/etc/modular-platform/module.env"

// sdkInstance gives sdk instance i its bootstrap env, its env file mount and its generated
// secret values. Its host name, user and hardening come from service like every module's.
func (r *renderer) sdkInstance(svc *types.ServiceConfig, i string, m contract.Module) {
	setEnv(svc, "MP_INSTANCE", i)
	setEnv(svc, "MP_DOMAIN", domain(r.g.Name))
	setEnv(svc, "MP_ENV_FILE", moduleEnvPath)

	config := i + "_env"
	if r.project.Configs == nil {
		r.project.Configs = types.Configs{}
	}
	r.project.Configs[config] = types.ConfigObjConfig{File: "./" + envFilePath(i)}
	svc.Configs = append(svc.Configs, types.ServiceConfigObjConfig{Source: config, Target: moduleEnvPath})

	for _, name := range sortedKeys(m.Config) {
		v := m.Config[name]
		if v.Type != contract.ConfigValueTypeSecret || !v.Generate {
			continue
		}
		path := "secrets/" + i + "/" + name
		target := "/run/secrets/" + i + "_" + name
		r.mountSecret(svc, i+"_"+name, path, target)
		setEnv(svc, v.Env+"_FILE", target)
		r.generated(i, path, false)
	}
	r.data.EnvFiles = append(r.data.EnvFiles, envFilePath(i))
}

func envFilePath(i string) string { return "config/" + i + ".env" }

// envFile writes config/<i>.env: one block per value in name order, secrets never. Bindings
// deliver every value they carry, so the file holds no binding lines.
func envFile(i string, m contract.Module) []byte {
	blocks := []string{fmt.Sprintf("# Settings for %s (module %s). Edit and restart the module to apply.\n"+
		"# Lines starting with # are defaults or optional values; remove the # to set one.\n", i, m.Name)}
	compose := m.ComposeValues()
	for _, name := range sortedKeys(m.Config) {
		v := m.Config[name]
		if v.Type == contract.ConfigValueTypeSecret {
			continue
		}
		var line string
		switch {
		case slices.Contains(compose, name):
			line = "# set in .env as " + contract.ComposeVar(i, name)
		case v.Default == "{uid}" || v.Default == "{gid}":
			line = fmt.Sprintf("# set to the id of the user setup.sh ran as (%s)", strings.Trim(v.Default, "{}"))
		case v.Default != "":
			line = fmt.Sprintf("# %s=%s", v.Env, v.Default)
		case v.Optional:
			line = fmt.Sprintf("# optional\n# %s=", v.Env)
		default:
			line = fmt.Sprintf("# required\n%s=", v.Env)
		}
		blocks = append(blocks, fmt.Sprintf("# %s\n%s\n", v.Description, line))
	}
	return []byte(strings.Join(blocks, "\n"))
}
