package render

import (
	"cmp"
	"fmt"
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

// CodeSetupRequired refuses a deployed instance that needs setup.sh (spec 0013, Managed flavor).
const CodeSetupRequired = "setup_required"

// RefusedError lists why the managed flavor refuses an ok plan, one error per instance and
// code, sorted by path then code; nothing was rendered.
type RefusedError struct {
	Problems []contract.Problem
}

func (e *RefusedError) Error() string {
	return fmt.Sprintf("render: the managed flavor refuses the graph: %d problems", len(e.Problems))
}

// Refuse returns plan refused with problems: not ok, no digest, a warning that one of problems
// repeats as an error (same code and path) left out, and every problem sorted by path then code.
func Refuse(plan contract.PlanResult, problems []contract.Problem) contract.PlanResult {
	plan.Ok, plan.Digest = false, ""
	kept := slices.DeleteFunc(slices.Clone(plan.Problems), func(w contract.Problem) bool {
		return w.Severity == contract.Warning && slices.ContainsFunc(problems, func(e contract.Problem) bool {
			return e.Code == w.Code && e.Path == w.Path
		})
	})
	plan.Problems = slices.Concat(kept, problems)
	slices.SortStableFunc(plan.Problems, func(a, b contract.Problem) int {
		return cmp.Or(strings.Compare(a.Path, b.Path), strings.Compare(a.Code, b.Code))
	})
	return plan
}

// refusals lists what a deployed graph may not have (spec 0013, Host access and Managed flavor):
// host_access at each instance whose module publishes a port or mounts a host path, and
// setup_required at each one that needs setup.sh, which the deployer cannot run until #9.
func refusals(plan contract.PlanResult, catalog contract.Catalog) []contract.Problem {
	modules := make(map[string]contract.Module, len(catalog.Modules))
	for _, m := range catalog.Modules {
		modules[m.Name] = m
	}
	var out []contract.Problem
	for _, i := range sortedKeys(plan.Graph.Modules) {
		gm := plan.Graph.Modules[i]
		m, ok := modules[gm.Module]
		if gm.External != nil || !ok {
			continue
		}
		if len(m.Publish) > 0 || len(m.HostMounts) > 0 {
			out = append(out, contract.Problem{
				Severity: contract.Error, Code: CodeHostAccess, Path: "modules." + i,
				Message: fmt.Sprintf("Instance %s publishes host ports or mounts host paths, which a deployed graph never gets; download the zip to run it.", i),
			})
		}
		if needsSetup(m) {
			out = append(out, contract.Problem{
				Severity: contract.Error, Code: CodeSetupRequired, Path: "modules." + i,
				Message: fmt.Sprintf("Instance %s needs setup.sh (an asked value, an asked secret or a setup step), which a deployed graph cannot run yet; download the zip to run it.", i),
			})
		}
	}
	return out
}

// Managed renders an ok plan for the managed deployer (spec 0006, grown by 0013): the static
// output plus the platform labels on every service (and the job label on each one shot job),
// restart: unless-stopped on every long running service, and the project named mp-<graph>.
// Host names stay <instance>.<graph>.internal. deployment is the deployment id written into the
// deployment label. A graph with an instance the managed flavor refuses returns a *RefusedError.
func Managed(plan contract.PlanResult, catalog contract.Catalog, templates contract.Templates, deployment string) (ManagedProject, error) {
	if !plan.Ok {
		return ManagedProject{}, ErrPlanHasErrors
	}
	if problems := refusals(plan, catalog); len(problems) > 0 {
		return ManagedProject{}, &RefusedError{Problems: problems}
	}
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
