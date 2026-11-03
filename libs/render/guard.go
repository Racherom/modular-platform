package render

import (
	"context"
	"fmt"
	"maps"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"github.com/compose-spec/compose-go/v2/dotenv"
	"github.com/compose-spec/compose-go/v2/loader"
	"github.com/compose-spec/compose-go/v2/types"

	"github.com/racherom/modular-platform/libs/contract"
)

// Problem codes of the guard (spec 0006, child 0007).
const (
	// CodeHostAccess refuses a key or value the managed render never emits.
	CodeHostAccess = "host_access"
	// CodeInvalidProject refuses a project that is not the deployment's own.
	CodeInvalidProject = "invalid_project"
)

var (
	secretFile = regexp.MustCompile(`^\./secrets/[a-z0-9-]+/[A-Za-z0-9._-]+$`)
	configFile = regexp.MustCompile(`^\./config/[a-z0-9]([a-z0-9-]*[a-z0-9])?\.env$`)
	// platformLabels are the only labels a managed service carries.
	platformLabels = []string{LabelDeployment, LabelGraph, LabelInstance, LabelModule}
)

// Project is a Compose file as docker compose reads it: interpolated with its env files and in
// canonical form. include and extends stay keys, so the guard sees and refuses them.
type Project struct {
	model map[string]any
	env   []string // the variable names the env files set, sorted
}

// LoadProject reads compose with the variables of envFiles, the later file winning, as docker
// compose does with --env-file. It never touches disk: include, extends and label files are not
// followed, and paths stay as written.
func LoadProject(ctx context.Context, compose []byte, envFiles ...[]byte) (Project, error) {
	env := types.Mapping{}
	for _, f := range envFiles {
		vars, err := dotenv.UnmarshalBytesWithLookup(f, func(name string) (string, bool) {
			v, ok := env[name]
			return v, ok
		})
		if err != nil {
			return Project{}, fmt.Errorf("render: env file: %w", err)
		}
		maps.Copy(env, vars)
	}
	names := sortedKeys(env) // before the loader adds its own
	model, err := loader.LoadModelWithContext(ctx, types.ConfigDetails{
		ConfigFiles: []types.ConfigFile{{Filename: "docker-compose.yml", Content: compose}},
		Environment: env,
	}, func(o *loader.Options) {
		o.SkipInclude = true
		o.SkipExtends = true
		o.SkipResolveLabels = true
		// normalization would add the names Compose derives, which the file never wrote
		o.SkipNormalization = true
		o.ResolvePaths = false
		// a fallback only: the file's own name wins, and CheckProject compares it
		o.SetProjectName("guard", false)
	})
	if err != nil {
		return Project{}, fmt.Errorf("render: load docker-compose.yml: %w", err)
	}
	return Project{model: model, env: names}, nil
}

// Guard checks p against an allow list of exactly the shape Managed emits (spec 0006, AC-8):
// anything else is one host_access problem per instance and key. reservedPorts are the host
// ports of the platform's ingress, which no project may publish. Guard is the last line between
// an imported graph and the host, so a key the render does not emit is refused even when it
// looks harmless; widening the render means widening this list in the same change.
func Guard(p Project, reservedPorts []int) []contract.Problem {
	g := guard{reserved: reservedPorts}
	for _, k := range sortedKeys(p.model) {
		v := p.model[k]
		switch k {
		case "name":
			g.want(k, isString(v))
		case "services":
			g.services(p.model, v)
		case "networks":
			g.entries(k, v, func(path string, e map[string]any) {
				g.only(path, e, "internal")
			})
		case "volumes":
			g.entries(k, v, func(path string, e map[string]any) { g.only(path, e) })
		case "secrets":
			g.files(k, v, secretFile)
		case "configs":
			g.files(k, v, configFile)
		default:
			g.refuse(k)
		}
	}
	for _, name := range p.env {
		// docker compose reads its own settings, such as another engine, from the env files
		if strings.HasPrefix(name, "COMPOSE_") || strings.HasPrefix(name, "DOCKER_") {
			g.refuse("env." + name)
		}
	}
	g.socket("", p.model)
	return g.problems
}

// CheckProject refuses, as invalid_project, a project that is not the deployment's own: a
// name other than project, or a service whose graph label is not project without mp- or whose
// deployment label is not deployment.
func CheckProject(p Project, project, deployment string) []contract.Problem {
	var problems []contract.Problem
	refuse := func(path, format string, args ...any) {
		problems = append(problems, contract.Problem{
			Severity: contract.Error, Code: CodeInvalidProject, Path: path, Message: fmt.Sprintf(format, args...),
		})
	}
	if name, ok := p.model["name"]; ok && name != project {
		refuse("name", "The project is named %v, not %s.", name, project)
	}
	graph := strings.TrimPrefix(project, ProjectPrefix)
	services, _ := p.model["services"].(map[string]any)
	for _, i := range sortedKeys(services) {
		svc, _ := services[i].(map[string]any)
		labels, _ := svc["labels"].(map[string]any)
		if labels[LabelGraph] != graph {
			refuse("services."+i+".labels", "Service %s does not carry the graph label %s.", i, graph)
		}
		if labels[LabelDeployment] != deployment {
			refuse("services."+i+".labels", "Service %s does not carry this deployment's label.", i)
		}
	}
	return problems
}

// guard collects host_access problems.
type guard struct {
	reserved []int
	problems []contract.Problem
}

func (g *guard) refuse(path string) {
	g.problems = append(g.problems, contract.Problem{
		Severity: contract.Error, Code: CodeHostAccess, Path: path,
		Message: path + " is not something the platform renders, so it is refused: it could reach the host.",
	})
}

// want refuses path unless ok.
func (g *guard) want(path string, ok bool) {
	if !ok {
		g.refuse(path)
	}
}

// only refuses every key of m outside allowed.
func (g *guard) only(path string, m map[string]any, allowed ...string) {
	for _, k := range sortedKeys(m) {
		if !slices.Contains(allowed, k) {
			g.refuse(path + "." + k)
		}
	}
}

// entries calls check on each entry of the top level map v, an empty entry as an empty map.
func (g *guard) entries(path string, v any, check func(string, map[string]any)) {
	m, ok := v.(map[string]any)
	if !ok {
		g.refuse(path)
		return
	}
	for _, name := range sortedKeys(m) {
		switch e := m[name].(type) {
		case nil:
			check(path+"."+name, map[string]any{})
		case map[string]any:
			check(path+"."+name, e)
		default:
			g.refuse(path + "." + name)
		}
	}
}

// files checks top level secrets or configs: each only a file inside the project matching pattern.
func (g *guard) files(path string, v any, pattern *regexp.Regexp) {
	g.entries(path, v, func(p string, e map[string]any) {
		g.only(p, e, "file")
		file, _ := e["file"].(string)
		g.want(p+".file", pattern.MatchString(file))
	})
}

func (g *guard) services(model map[string]any, v any) {
	services, ok := v.(map[string]any)
	if !ok {
		g.refuse("services")
		return
	}
	defined := func(key string) map[string]any {
		m, _ := model[key].(map[string]any)
		return m
	}
	for _, i := range sortedKeys(services) {
		svc, ok := services[i].(map[string]any)
		if !ok {
			g.refuse("services." + i)
			continue
		}
		for _, k := range sortedKeys(svc) {
			path, v := "services."+i+"."+k, svc[k]
			switch k {
			case "image", "user":
				g.want(path, isString(v) && v != "")
			case "restart":
				g.want(path, v == string(types.RestartPolicyUnlessStopped))
			case "environment":
				_, ok := v.(map[string]any)
				g.want(path, ok)
			case "healthcheck":
				g.entry(path, v, func(m map[string]any) {
					g.only(path, m, "test", "interval", "timeout", "start_period", "start_interval", "retries", "disable")
				})
			case "depends_on":
				g.entries(path, v, func(p string, dep map[string]any) {
					g.want(p, services[strings.TrimPrefix(p, path+".")] != nil)
					g.only(p, dep, "condition", "required")
				})
			case "networks":
				g.entries(path, v, func(p string, n map[string]any) {
					name := strings.TrimPrefix(p, path+".")
					g.want(p, name == "default" || hasKey(defined("networks"), name))
					g.only(p, n, "aliases")
				})
			case "secrets", "configs":
				g.list(path, v, func(p string, ref map[string]any) {
					g.only(p, ref, "source", "target")
					source, _ := ref["source"].(string)
					g.want(p+".source", hasKey(defined(k), source))
				})
			case "labels":
				g.entry(path, v, func(m map[string]any) {
					for _, l := range sortedKeys(m) {
						g.want(path+"."+l, slices.Contains(platformLabels, l) && isString(m[l]))
					}
				})
			case "ports":
				g.list(path, v, func(p string, port map[string]any) {
					g.only(p, port, "target", "published", "protocol", "mode")
					g.want(p+".published", !g.publishesReserved(port["published"]))
				})
			default:
				g.refuse(path)
			}
		}
	}
}

// entry calls check on v when it is a map.
func (g *guard) entry(path string, v any, check func(map[string]any)) {
	m, ok := v.(map[string]any)
	if !ok {
		g.refuse(path)
		return
	}
	check(m)
}

// list calls check on each map item of the list v.
func (g *guard) list(path string, v any, check func(string, map[string]any)) {
	items, ok := v.([]any)
	if !ok {
		g.refuse(path)
		return
	}
	for n, item := range items {
		p := path + "." + strconv.Itoa(n)
		m, ok := item.(map[string]any)
		if !ok {
			g.refuse(p)
			continue
		}
		check(p, m)
	}
}

// publishesReserved reports whether a published port, a number or a range like 8000-8100,
// is or covers a reserved port. A value that cannot be read counts as reserved.
func (g *guard) publishesReserved(v any) bool {
	if v == nil {
		return false // Docker picks a free port
	}
	s := fmt.Sprint(v)
	from, to, isRange := strings.Cut(s, "-")
	if !isRange {
		to = from
	}
	lo, err1 := strconv.Atoi(from)
	hi, err2 := strconv.Atoi(to)
	if err1 != nil || err2 != nil || lo > hi {
		return true
	}
	return slices.ContainsFunc(g.reserved, func(r int) bool { return r >= lo && r <= hi })
}

// socket refuses every string value that names the Docker socket, wherever it sits.
func (g *guard) socket(path string, v any) {
	join := func(k string) string {
		if path == "" {
			return k
		}
		return path + "." + k
	}
	switch t := v.(type) {
	case map[string]any:
		for _, k := range sortedKeys(t) {
			g.socket(join(k), t[k])
		}
	case []any:
		for n, item := range t {
			g.socket(join(strconv.Itoa(n)), item)
		}
	case string:
		if strings.Contains(t, "docker.sock") {
			g.refuse(path)
		}
	}
}

func isString(v any) bool {
	_, ok := v.(string)
	return ok
}

func hasKey(m map[string]any, k string) bool {
	_, ok := m[k]
	return ok
}
