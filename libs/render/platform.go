package render

import (
	"github.com/compose-spec/compose-go/v2/types"

	"github.com/racherom/modular-platform/libs/contract"
)

// PlatformStack is the stack label of every service of the platform's own project
// (spec 0006, "Cross child contracts").
const PlatformStack = "platform"

// Platform renders an ok check result of the platform graph into the folder the managed release zip
// ships (spec 0015): docker-compose.yml, config/, setup.sh, .env.example, README.md and
// graph.yaml. Unlike the managed flavor it renders publish, host_mounts and setup steps; unlike
// the static one it sets restart: unless-stopped and the platform labels with stack
// platform, renders {install} paths as ${MP_DIR}/<relative> (setup.sh writes MP_DIR) and asks
// its values in binding order. The project is named after the graph.
func Platform(result contract.CheckResult, catalog contract.Catalog, templates contract.Templates) (Output, error) {
	r, err := renderProject(result, catalog, templates, flavorPlatform, func(p *types.Project, jobs map[string]job) {
		label(p, result.Graph, jobs, PlatformStack)
	})
	if err != nil {
		return Output{}, err
	}
	return r.out, nil
}
