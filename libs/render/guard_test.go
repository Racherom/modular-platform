package render_test

import (
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/racherom/modular-platform/libs/contract"
	"github.com/racherom/modular-platform/libs/render"
)

// reserved are the platform's ingress ports in these tests.
var reserved = []int{80, 443}

// deployEnv stands in for the deployer's deploy.env.
var deployEnv = []byte("MP_UID=1000\nMP_GID=1000\n")

// TestGuardAcceptsEveryRender renders every fixture graph with Managed and shows the guard and
// the project check accept it, so the allow list and the render cannot drift (spec 0006, AC-8).
// The bindings case adds NET_BIND_SERVICE, the one Linux capability a deployed graph may add
// (spec 0022), and every case shows Managed emits cap_drop and security_opt on each service.
func TestGuardAcceptsEveryRender(t *testing.T) {
	t.Parallel()
	for name, c := range map[string]struct {
		plan    func(*testing.T) (contract.PlanResult, contract.Catalog, contract.Templates)
		refused []string // the paths the guard still refuses
	}{
		"whoami-postgres": {noTemplates(samplePlan), nil},
		"sdk-pair":        {noTemplates(pairPlan), nil},
		"bindings":        {noTemplates(bindingsPlan), nil},
		"templates":       {templatesPlan, nil},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			p, catalog, tmpls := c.plan(t)
			m, err := render.Managed(p, catalog, tmpls, testDeployment)
			if err != nil {
				t.Fatal(err)
			}
			project, err := render.LoadProject(t.Context(), fileData(t, m.Output, "docker-compose.yml"), fileData(t, m.Output, ".env"), deployEnv)
			if err != nil {
				t.Fatal(err)
			}
			var refused []string
			for _, pr := range render.Guard(project, reserved) {
				refused = append(refused, pr.Path)
			}
			if !slices.Equal(refused, c.refused) {
				t.Errorf("the guard refuses %v, want %v", refused, c.refused)
			}
			if problems := render.CheckProject(project, m.Name, testDeployment); len(problems) > 0 {
				t.Errorf("the project check refuses the render: %+v", problems)
			}
		})
	}
}

// guarded builds a project in the rendered shape with extra service keys and top level keys.
func guarded(service, top string) string {
	return fmt.Sprintf(`name: mp-sample
services:
  app:
    image: traefik/whoami:v1.12.0
    labels:
      io.github.racherom.modular-platform.deployment: %s
      io.github.racherom.modular-platform.graph: sample
      io.github.racherom.modular-platform.instance: app
      io.github.racherom.modular-platform.module: whoami
    restart: unless-stopped
    cap_drop: [ALL]
    security_opt: ["no-new-privileges:true"]
%s
%s`, testDeployment, indent(service, "    "), top)
}

func indent(s, prefix string) string {
	if s == "" {
		return ""
	}
	return prefix + strings.ReplaceAll(s, "\n", "\n"+prefix)
}

// noTemplates turns a plan helper whose catalog has no templates into one that returns none.
func noTemplates(plan func(*testing.T) (contract.PlanResult, contract.Catalog)) func(*testing.T) (contract.PlanResult, contract.Catalog, contract.Templates) {
	return func(t *testing.T) (contract.PlanResult, contract.Catalog, contract.Templates) {
		t.Helper()
		p, c := plan(t)
		return p, c, builtinTemplates(t)
	}
}

func TestGuard(t *testing.T) {
	t.Parallel()
	for name, tc := range map[string]struct {
		service, top, env string
		edit              func(string) string // changes the rendered shape itself
		want              string              // the path the refusal starts with; empty means accepted
	}{
		"the rendered shape":           {},
		"an own network":               {service: "networks:\n  default:\n    aliases: [app.sample.internal]", want: ""},
		"a free port":                  {service: "ports: [\"8080:80\"]", want: ""},
		"a random host port":           {service: "ports: [\"80\"]", want: ""},
		"privileged":                   {service: "privileged: true", want: "services.app.privileged"},
		"host network":                 {service: "network_mode: host", want: "services.app.network_mode"},
		"container network":            {service: "network_mode: \"container:other\"", want: "services.app.network_mode"},
		"host pid":                     {service: "pid: host", want: "services.app.pid"},
		"host ipc":                     {service: "ipc: host", want: "services.app.ipc"},
		"userns":                       {service: "userns_mode: host", want: "services.app.userns_mode"},
		"cgroup":                       {service: "cgroup: host", want: "services.app.cgroup"},
		"cgroup parent":                {service: "cgroup_parent: /", want: "services.app.cgroup_parent"},
		"seccomp unconfined":           {edit: replace(noNewPrivs, `security_opt: ["seccomp=unconfined"]`), want: "services.app.security_opt"},
		"apparmor unconfined":          {edit: replace(noNewPrivs, `security_opt: ["no-new-privileges:true", "apparmor=unconfined"]`), want: "services.app.security_opt"},
		"cap_drop of one":              {edit: replace(dropAll, "cap_drop: [NET_RAW]"), want: "services.app.cap_drop"},
		"no cap_drop":                  {edit: replace("    "+dropAll+"\n", ""), want: "services.app.cap_drop"},
		"no security_opt":              {edit: replace("    "+noNewPrivs+"\n", ""), want: "services.app.security_opt"},
		"a job without cap_drop":       {edit: then(waitAnchor("restart: \"no\"", "", "wait", "provision"), replace("    "+dropAll+"\n", "")), want: "services.app.cap_drop"},
		"a job without security_opt":   {edit: then(waitAnchor("restart: \"no\"", "", "wait", "revoke"), replace("    "+noNewPrivs+"\n", "")), want: "services.app.security_opt"},
		"mp_jobs without cap_drop":     {edit: then(waitAnchor("restart: unless-stopped", "init: true\nnetwork_mode: none"), replace("    "+dropAll+"\n", "")), want: "services.app.cap_drop"},
		"mp_jobs without security_opt": {edit: then(waitAnchor("restart: unless-stopped", "init: true\nnetwork_mode: none"), replace("    "+noNewPrivs+"\n", "")), want: "services.app.security_opt"},
		"sysctls":                      {service: "sysctls:\n  net.ipv4.ip_forward: 1", want: "services.app.sysctls"},
		"devices":                      {service: "devices: [\"/dev/sda:/dev/sda\"]", want: "services.app.devices"},
		"cap_add of NET_BIND_SERVICE":  {service: "cap_add: [NET_BIND_SERVICE]", want: ""},
		"cap_add of nothing":           {service: "cap_add: []", want: ""},
		"cap_add of SYS_ADMIN":         {service: "cap_add: [SYS_ADMIN]", want: "services.app.cap_add.0"},
		"cap_add lower case":           {service: "cap_add: [net_bind_service]", want: "services.app.cap_add.0"},
		"cap_add with CAP_":            {service: "cap_add: [CAP_NET_BIND_SERVICE]", want: "services.app.cap_add.0"},
		"cap_add of ALL":               {service: "cap_add: [ALL]", want: "services.app.cap_add.0"},
		"cap_add second item":          {service: "cap_add: [NET_BIND_SERVICE, SYS_ADMIN]", want: "services.app.cap_add.1"},
		"profiles":                     {service: "profiles: [x]", want: "services.app.profiles"},
		"bind mount":                   {service: "volumes: [\"/:/host\"]", want: "services.app.volumes"},
		"docker socket mount":          {service: "volumes: [\"/var/run/docker.sock:/var/run/docker.sock\"]", want: "services.app.volumes.0.source"},
		"socket in a value":            {service: "environment:\n  DOCKER: unix:///run/docker.sock", want: "services.app.environment.DOCKER"},
		"volumes_from":                 {service: "volumes_from: [other]", want: "services.app.volumes_from"},
		"env_file":                     {service: "env_file: /etc/passwd", want: "services.app.env_file"},
		"build":                        {service: "build: .", want: "services.app.build"},
		"extends":                      {service: "extends:\n  file: other.yml\n  service: base", want: "services.app.extends"},
		"container_name":               {service: "container_name: modular-platform-api-1", want: "services.app.container_name"},
		"another restart":              {edit: replace("restart: unless-stopped", "restart: always"), want: "services.app.restart"},
		"a compose label":              {edit: replace("    labels:\n", "    labels:\n      com.docker.compose.project: modular-platform\n"), want: "services.app.labels.com.docker.compose.project"},
		"reserved http port":           {service: "ports: [\"80:80\"]", want: "services.app.ports.0.published"},
		"range over https":             {service: "ports: [\"400-500:400-500\"]", want: "services.app.ports."},
		"port on one address":          {service: "ports: [\"127.0.0.1:8080:80\"]", want: "services.app.ports.0.host_ip"},
		"an unknown network":           {service: "networks: [elsewhere]", want: "services.app.networks.elsewhere"},
		"a secret elsewhere":           {service: "secrets: [s]", top: "secrets:\n  s:\n    file: /etc/shadow", want: "secrets.s.file"},
		"a secret by env":              {top: "secrets:\n  s:\n    file: ${SECRET}", env: "SECRET=/etc/shadow", want: "secrets.s.file"},
		"a secret from env":            {top: "secrets:\n  s:\n    environment: HOME", want: "secrets.s.environment"},
		"a config outside":             {top: "configs:\n  c:\n    file: ../other/config/app.env", want: "configs.c.file"},
		"a template output":            {top: "configs:\n  c:\n    file: ./config/app/Caddyfile", want: ""},
		"a template folder up":         {top: "configs:\n  c:\n    file: ./config/app/..", want: "configs.c.file"},
		"a template two deep":          {top: "configs:\n  c:\n    file: ./config/app/a/b", want: "configs.c.file"},
		"include":                      {top: "include: [other.yml]", want: "include"},
		"an extension":                 {top: "x-anything: 1", want: "x-anything"},
		"the platform network":         {top: "networks:\n  internal:\n    external: true\n    name: modular-platform_internal", want: "networks.internal.external"},
		"a named network":              {top: "networks:\n  internal:\n    name: modular-platform_internal", want: "networks.internal.name"},
		"an external volume":           {top: "volumes:\n  data:\n    external: true", want: "volumes.data.external"},
		"volume driver_opts":           {top: "volumes:\n  data:\n    driver_opts:\n      type: none\n      device: /\n      o: bind", want: "volumes.data.driver_opts"},
		"another engine":               {env: "DOCKER_HOST=tcp://elsewhere:2375", want: "env.DOCKER_HOST"},
		"the wait anchor":              {edit: waitAnchor("restart: unless-stopped", "init: true\nnetwork_mode: none"), want: ""},
		"init on a module":             {service: "init: true", want: "services.app.init"},
		"no network on a module":       {service: "network_mode: none", want: "services.app.network_mode"},
		"an unknown job kind":          {edit: waitAnchor("restart: unless-stopped", "init: true", "wait", "cleanup"), want: "services.app.labels.io.github.racherom.modular-platform.job"},
		"a wait anchor on host":        {edit: waitAnchor("restart: unless-stopped", "network_mode: host"), want: "services.app.network_mode"},
		"a wait anchor on a network":   {edit: waitAnchor("restart: unless-stopped", "network_mode: none\nnetworks: [default]"), want: "services.app.network_mode"},
		"a wait anchor not restarted":  {edit: waitAnchor("restart: \"no\"", "init: true"), want: "services.app.restart"},
		"init off":                     {edit: waitAnchor("restart: unless-stopped", "init: false"), want: "services.app.init"},
		"a revoke job restart no":      {edit: waitAnchor("restart: \"no\"", "", "wait", "revoke"), want: ""},
		"compose settings":             {env: "COMPOSE_PROFILES=all", want: "env.COMPOSE_PROFILES"},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			compose := guarded(tc.service, tc.top)
			if tc.edit != nil {
				compose = tc.edit(compose)
			}
			p, err := render.LoadProject(t.Context(), []byte(compose), []byte(tc.env), deployEnv)
			if err != nil {
				t.Fatal(err)
			}
			problems := render.Guard(p, reserved)
			if tc.want == "" {
				if len(problems) > 0 {
					t.Fatalf("refused: %+v", problems)
				}
				return
			}
			if !slices.ContainsFunc(problems, func(p contract.Problem) bool {
				return p.Code == render.CodeHostAccess && strings.HasPrefix(p.Path, tc.want)
			}) {
				t.Errorf("problems %+v lack host_access at %s", problems, tc.want)
			}
		})
	}
}

// waitAnchor turns app into a service labelled job: <kind> (wait unless given as from, to),
// with restart and extra service keys.
func waitAnchor(restart, extra string, kind ...string) func(string) string {
	k := "wait"
	if len(kind) == 2 {
		k = kind[1]
	}
	return func(s string) string {
		s = strings.Replace(s, "      io.github.racherom.modular-platform.instance: app\n      io.github.racherom.modular-platform.module: whoami\n",
			"      io.github.racherom.modular-platform.job: "+k+"\n", 1)
		return strings.Replace(s, "    restart: unless-stopped\n", "    "+restart+"\n"+indent(extra, "    ")+"\n", 1)
	}
}

// The two hardening keys every deployed service must carry (spec 0022), as guarded writes them.
const (
	dropAll    = "cap_drop: [ALL]"
	noNewPrivs = `security_opt: ["no-new-privileges:true"]`
)

// then applies edits in order.
func then(edits ...func(string) string) func(string) string {
	return func(s string) string {
		for _, e := range edits {
			s = e(s)
		}
		return s
	}
}

func replace(old, replacement string) func(string) string {
	return func(s string) string { return strings.Replace(s, old, replacement, 1) }
}

func TestCheckProject(t *testing.T) {
	t.Parallel()
	for name, tc := range map[string]struct {
		compose, project, deployment string
		want                         string
	}{
		"its own":            {guarded("", ""), "mp-sample", testDeployment, ""},
		"another name":       {guarded("", ""), "mp-other", testDeployment, "name"},
		"another deployment": {guarded("", ""), "mp-sample", "01960000-0000-7000-8000-000000000002", "services.app.labels"},
		"another graph":      {strings.Replace(guarded("", ""), "name: mp-sample\n", "", 1), "mp-other", testDeployment, "services.app.labels"},
		"no labels":          {"services:\n  app:\n    image: x\n", "mp-sample", testDeployment, "services.app.labels"},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			p, err := render.LoadProject(t.Context(), []byte(tc.compose))
			if err != nil {
				t.Fatal(err)
			}
			problems := render.CheckProject(p, tc.project, tc.deployment)
			if tc.want == "" {
				if len(problems) > 0 {
					t.Fatalf("refused: %+v", problems)
				}
				return
			}
			if !slices.ContainsFunc(problems, func(p contract.Problem) bool {
				return p.Code == render.CodeInvalidProject && p.Path == tc.want
			}) {
				t.Errorf("problems %+v lack invalid_project at %s", problems, tc.want)
			}
		})
	}
}

func TestLoadProjectRefusesBrokenFiles(t *testing.T) {
	t.Parallel()
	for name, compose := range map[string]string{
		"not yaml":        "services: [",
		"not a mapping":   "- a\n",
		"breaks a schema": "services:\n  app:\n    image: [1]\n",
		// the guard refuses these at services.<i>.cap_add too, should the schema ever allow them
		"cap_add a string": "services:\n  app:\n    image: a:1\n    cap_add: NET_BIND_SERVICE\n",
		"cap_add null":     "services:\n  app:\n    image: a:1\n    cap_add:\n",
	} {
		if _, err := render.LoadProject(t.Context(), []byte(compose)); err == nil {
			t.Errorf("%s: loaded", name)
		}
	}
}

func fileData(t *testing.T, out render.Output, path string) []byte {
	t.Helper()
	for _, f := range out.Files {
		if f.Path == path {
			return f.Data
		}
	}
	t.Fatalf("no %s in %s", path, out.Name)
	return nil
}
