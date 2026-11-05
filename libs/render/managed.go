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
	LabelInstance   = LabelPrefix + "instance"   // the instance name; a job's provisioner instance
	LabelModule     = LabelPrefix + "module"     // the catalog module name; a job's provisioner module
	LabelDeployment = LabelPrefix + "deployment" // the deployment id; "platform" on the platform's own services
	LabelJob        = LabelPrefix + "job"        // provision or revoke on a one shot job, wait on mp_jobs (spec 0013)
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
	Field      string // the capability field the placed file holds
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

// Managed renders an ok plan for the managed deployer (spec 0006, grown by 0013): the static
// output plus the platform labels on every service (and the job label on each one shot job),
// restart: unless-stopped on every long running service, and the project named mp-<graph>.
// Host names stay <instance>.<graph>.internal. deployment is the deployment id written into the
// deployment label.
func Managed(plan contract.PlanResult, catalog contract.Catalog, templates contract.Templates, deployment string) (ManagedProject, error) {
	r, err := renderProject(plan, catalog, templates, true, func(p *types.Project, jobs map[string]job) {
		graph := p.Name
		p.Name = ProjectPrefix + graph
		for name, svc := range p.Services {
			labels := types.Labels{LabelGraph: graph, LabelDeployment: deployment}
			if j, ok := jobs[name]; ok {
				labels[LabelJob] = j.kind
				if j.instance != "" { // the wait anchor belongs to no instance
					labels[LabelInstance] = j.instance
				}
				if j.module != "" { // an external provisioner, or the wait anchor, has no module
					labels[LabelModule] = j.module
				}
			} else {
				labels[LabelInstance], labels[LabelModule] = name, plan.Graph.Modules[name].Module
			}
			svc.Labels = labels
			p.Services[name] = svc
		}
	})
	if err != nil {
		return ManagedProject{}, err
	}
	m := ManagedProject{Output: r.out}
	images := map[string]string{}
	for _, e := range catalog.Modules {
		images[e.Name] = e.Image
	}
	for _, i := range sortedKeys(plan.Graph.Modules) {
		if gm := plan.Graph.Modules[i]; gm.External == nil {
			m.Images = append(m.Images, Image{Instance: i, Module: gm.Module, Image: images[gm.Module]})
		}
	}
	for _, s := range r.data.Secrets {
		m.Secrets = append(m.Secrets, Secret{Path: s.Path, Kind: SecretGenerated, Instance: s.Instance})
	}
	for _, s := range r.data.Placed {
		m.Secrets = append(m.Secrets, Secret{Path: s.Path, Kind: SecretPlaced, Instance: s.Instance, Capability: s.Capability, Field: s.Field})
	}
	slices.SortFunc(m.Secrets, func(a, b Secret) int { return strings.Compare(a.Path, b.Path) })
	return m, nil
}
