package render

import (
	"slices"
	"strings"

	"github.com/compose-spec/compose-go/v2/types"

	"github.com/racherom/modular-platform/libs/contract"
)

// LabelPrefix starts every platform label (spec 0006, "Cross child contracts").
const LabelPrefix = "io.github.racherom.modular-platform."

// The platform labels on every service of a managed project.
const (
	LabelGraph      = LabelPrefix + "graph"      // the graph name
	LabelInstance   = LabelPrefix + "instance"   // the instance name
	LabelModule     = LabelPrefix + "module"     // the catalog module name
	LabelDeployment = LabelPrefix + "deployment" // the deployment id; "platform" on the platform's own services
)

// ProjectPrefix starts every managed project name, so a deployment can never be or clash with
// the platform's own project, modular-platform.
const ProjectPrefix = "mp-"

// SecretKind tells who makes a secret file of a managed project.
type SecretKind string

// The secret kinds.
const (
	// SecretGenerated is created by the deployer as 32 random bytes in hex.
	SecretGenerated SecretKind = "generated"
	// SecretPlaced is a credential you supply, such as a database password.
	SecretPlaced SecretKind = "placed"
)

// Secret is one secret file a managed project mounts. Capability and Field are set for a placed
// secret only.
type Secret struct {
	Path       string // secrets/<instance>/<name>
	Kind       SecretKind
	Instance   string
	Capability string
	Field      string // password or secret_key
}

// Image is the image one instance runs.
type Image struct {
	Instance, Module, Image string
}

// ManagedProject is a project rendered for the managed deployer: the static output with the
// project named mp-<graph>, the images it runs and every secret file it needs.
type ManagedProject struct {
	Output
	Images  []Image  // sorted by instance
	Secrets []Secret // sorted by path
}

// Managed renders an ok plan for the managed deployer (spec 0006): the static output plus
// the platform labels and restart: unless-stopped on every service, and the project named
// mp-<graph>. Host names stay <instance>.<graph>.internal. deployment is the deployment id
// written into the deployment label.
func Managed(plan contract.PlanResult, catalog []contract.CatalogEntry, deployment string) (ManagedProject, error) {
	modules := map[string]string{}
	for i, m := range plan.Graph.Modules {
		modules[i] = m.Module
	}
	graph := plan.Graph.Name
	r, err := render(plan, catalog, func(p *types.Project) {
		p.Name = ProjectPrefix + graph
		for i, svc := range p.Services {
			svc.Labels = types.Labels{
				LabelGraph:      graph,
				LabelInstance:   i,
				LabelModule:     modules[i],
				LabelDeployment: deployment,
			}
			svc.Restart = types.RestartPolicyUnlessStopped
			p.Services[i] = svc
		}
	})
	if err != nil {
		return ManagedProject{}, err
	}
	m := ManagedProject{Output: r.out}
	images := map[string]string{}
	for _, e := range catalog {
		images[e.Name] = e.Image
	}
	for _, i := range sortedKeys(modules) {
		m.Images = append(m.Images, Image{Instance: i, Module: modules[i], Image: images[modules[i]]})
	}
	for _, s := range r.data.Secrets {
		m.Secrets = append(m.Secrets, Secret{Path: s.Path, Kind: SecretGenerated, Instance: s.Instance})
	}
	for _, s := range r.data.Placed {
		m.Secrets = append(m.Secrets, Secret{Path: s.Path, Kind: SecretPlaced, Instance: s.Instance, Capability: s.Capability, Field: s.Field})
	}
	// a token secret appears once per binding; keep the first
	slices.SortStableFunc(m.Secrets, func(a, b Secret) int { return strings.Compare(a.Path, b.Path) })
	m.Secrets = slices.CompactFunc(m.Secrets, func(a, b Secret) bool { return a.Path == b.Path })
	return m, nil
}
