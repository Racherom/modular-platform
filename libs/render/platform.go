package render

import (
	"github.com/compose-spec/compose-go/v2/types"

	"github.com/racherom/modular-platform/libs/contract"
)

// PlatformDeployment is the deployment label of every service of the platform's own project
// (spec 0006, "Cross child contracts").
const PlatformDeployment = "platform"

// Platform renders an ok plan of the platform graph into the folder the managed release zip
// ships (spec 0015): docker-compose.yml, config/, setup.sh, .env.example, README.md and
// graph.yaml. Unlike the managed flavor it renders publish, host_mounts and setup steps; unlike
// the static one it sets restart: unless-stopped and the platform labels with deployment
// platform, renders {install} paths as ${MP_DIR}/<relative> (setup.sh writes MP_DIR) and asks
// its values in binding order. The project is named after the graph.
func Platform(plan contract.PlanResult, catalog contract.Catalog, templates contract.Templates) (Output, error) {
	r, err := renderProject(plan, catalog, templates, flavorPlatform, func(p *types.Project, jobs map[string]job) {
		label(p, plan.Graph, jobs, PlatformDeployment)
	})
	if err != nil {
		return Output{}, err
	}
	return r.out, nil
}
