package render_test

import (
	"errors"
	"os"
	"regexp"
	"strings"
	"testing"

	"sigs.k8s.io/yaml"

	"github.com/racherom/modular-platform/libs/render"
)

// TestPostgresCertificateJob checks the render side of AC-17 (spec 0014, "Certificate
// provisioner"): the platform's postgres takes its certificate and key from the job of its tls
// slot, which writes them in place into files setup.sh makes empty, postgres starts only after
// the job, and the consumers of postgres verify its certificate against the internal root.
func TestPostgresCertificateJob(t *testing.T) {
	t.Parallel()
	plan, catalog, templates := platformPlan(t)
	out, err := render.Platform(plan, catalog, templates)
	if err != nil {
		t.Fatal(err)
	}
	files := map[string]string{}
	for _, f := range out.Files {
		files[f.Path] = string(f.Data)
	}
	var project struct {
		Services map[string]struct {
			Environment map[string]string         `json:"environment"`
			DependsOn   map[string]map[string]any `json:"depends_on"`
			Volumes     []struct {
				Source, Target, Type string
			} `json:"volumes"`
			Secrets []struct{ Source, Target string } `json:"secrets"`
		} `json:"services"`
		Secrets map[string]struct{ File string } `json:"secrets"`
	}
	if err := yaml.Unmarshal([]byte(files["docker-compose.yml"]), &project); err != nil {
		t.Fatal(err)
	}

	job, ok := project.Services["cert-issuer_provision_postgres_tls"]
	if !ok {
		t.Fatal("no certificate job cert-issuer_provision_postgres_tls")
	}
	if got := job.Environment["MP_CONSUMER_HOST"]; got != "postgres.modular-platform.internal" {
		t.Errorf("the job asks for a certificate for %q, want postgres.modular-platform.internal", got)
	}
	for _, key := range []string{"MP_ADMIN_URL", "MP_ADMIN_PROVISIONER", "MP_ADMIN_CA_FILE", "MP_ADMIN_PASSWORD_FILE"} {
		if job.Environment[key] == "" {
			t.Errorf("the job lacks %s", key)
		}
	}
	for field, target := range map[string]string{"cert": "/out/cert", "key": "/out/key"} {
		want := "./secrets/postgres/uses_tls_" + field
		found := false
		for _, v := range job.Volumes {
			if v.Target == target {
				found = true
				if v.Type != "bind" || v.Source != want {
					t.Errorf("job volume at %s = %+v, want a bind of %s", target, v, want)
				}
			}
		}
		if !found {
			t.Errorf("the job has no volume at %s", target)
		}
	}
	for _, s := range job.Secrets {
		if strings.HasPrefix(s.Target, "/out/") {
			t.Errorf("the job mounts %s as a read only secret; it writes there", s.Target)
		}
	}

	pg := project.Services["postgres"]
	if cond := pg.DependsOn["cert-issuer_provision_postgres_tls"]["condition"]; cond != "service_completed_successfully" {
		t.Errorf("postgres waits for the certificate job with %v", cond)
	}
	targets := map[string]string{}
	for _, s := range pg.Secrets {
		targets[s.Target] = s.Source
	}
	for target, source := range map[string]string{"/run/tls/server.crt": "postgres_uses_tls_cert", "/run/tls/server.key": "postgres_uses_tls_key"} {
		if targets[target] != source {
			t.Errorf("postgres mounts %q at %s, want %s", targets[target], target, source)
		}
	}

	// the consumers verify the server and trust the internal root
	api := project.Services["api"].Environment
	if api["MP_DATABASE_SSLMODE"] != "verify-full" || api["MP_DATABASE_CA_FILE"] == "" {
		t.Errorf("api connects with sslmode %q and CA %q, want verify-full and a CA file", api["MP_DATABASE_SSLMODE"], api["MP_DATABASE_CA_FILE"])
	}
	if got := project.Secrets["api_uses_main-db_ca"].File; got != "./secrets/cert-issuer/root-ca" {
		t.Errorf("api trusts %q, want the internal root ./secrets/cert-issuer/root-ca", got)
	}

	// setup.sh makes the two job outputs empty; a random value would look like a certificate
	setup := files["setup.sh"]
	for _, f := range []string{"cert", "key"} {
		if !strings.Contains(setup, "\nnew_output secrets/postgres/uses_tls_"+f+"\n") {
			t.Errorf("setup.sh does not make secrets/postgres/uses_tls_%s with new_output", f)
		}
		if strings.Contains(setup, "new_secret secrets/postgres/uses_tls_"+f) {
			t.Errorf("setup.sh fills secrets/postgres/uses_tls_%s with a random value", f)
		}
	}
	if !regexp.MustCompile(`(?m)^new_output\(\) \{$`).MatchString(setup) {
		t.Error("setup.sh does not define new_output")
	}

	pgConf := files["config/postgres/postgresql.conf"]
	for _, want := range []string{"ssl = on", "ssl_cert_file = '/run/tls/server.crt'", "ssl_key_file = '/run/tls/server.key'"} {
		if !strings.Contains(pgConf, want) {
			t.Errorf("postgresql.conf lacks %q", want)
		}
	}
	if hba := files["config/postgres/pg_hba.conf"]; strings.Contains(hba, "\nhost ") || !strings.Contains(hba, "\nhostssl all all all scram-sha-256") {
		t.Errorf("pg_hba.conf lets a plain connection in:\n%s", hba)
	}
}

// TestManagedRefusesJobOutputs checks that a deployed graph whose binding yields a job output (a
// certificate) is not rendered yet: the deployer does not create empty output files until #31
// brings the certificate provisioner to deployed stacks (spec 0011, Follow-up).
func TestManagedRefusesJobOutputs(t *testing.T) {
	t.Parallel()
	catalog := loadCatalog(t, "../../catalog")
	plan := planText(t, `apiVersion: modular-platform/v1
kind: Graph
name: outputs
modules:
  ca:
    external:
      provides:
        certs:
          capability: tls-certificate-provisioner
          version: "1.0"
          fields:
            provisioner: jobs
            url: https://ca.example.test:9000
  db:
    module: postgres
    uses:
      tls: ca.certs
`, catalog)
	if _, err := render.Managed(plan, catalog, builtinTemplates(t), testDeployment); !errors.Is(err, render.ErrNotRendered) {
		t.Errorf("managed render: %v, want ErrNotRendered", err)
	}
	// the static flavor renders it: setup.sh makes the files and you place the CA's secrets
	if _, err := render.Render(plan, catalog, builtinTemplates(t)); err != nil {
		t.Errorf("static render: %v", err)
	}
}

// TestPostgresWithoutCertificate varies the input of the TLS source (spec 0013, `platform: tls`
// and `platform: ca`): with the tls slot of postgres unbound the platform renders no certificate
// job, every consumer connects with sslmode disable and no CA file, and pg_hba.conf lets the
// password sign in over a plain connection (AC-17).
func TestPostgresWithoutCertificate(t *testing.T) {
	t.Parallel()
	catalog, templates := loadCatalogTemplates(t, "../../catalog")
	data, err := os.ReadFile("../../deploy/managed/graph.yaml")
	if err != nil {
		t.Fatal(err)
	}
	text := strings.Replace(string(data), "tls: cert-issuer.certs", "tls: null", 1)
	if text == string(data) {
		t.Fatal("deploy/managed/graph.yaml no longer binds postgres.tls to cert-issuer.certs")
	}
	out, err := render.Platform(planText(t, text, catalog), catalog, templates)
	if err != nil {
		t.Fatal(err)
	}
	files := map[string]string{}
	for _, f := range out.Files {
		files[f.Path] = string(f.Data)
	}
	compose := files["docker-compose.yml"]
	for _, absent := range []string{"cert-issuer_provision_postgres_tls", "uses_tls_cert", "verify-full", "MP_DATABASE_CA_FILE"} {
		if strings.Contains(compose, absent) {
			t.Errorf("docker-compose.yml mentions %q with postgres.tls unbound", absent)
		}
	}
	if !strings.Contains(compose, "MP_DATABASE_SSLMODE: disable") {
		t.Error("api does not connect with sslmode disable")
	}
	if strings.Contains(files["setup.sh"], "new_output") {
		t.Error("setup.sh makes job output files with no certificate job")
	}
	if strings.Contains(files["config/postgres/postgresql.conf"], "ssl = on") || !strings.Contains(files["config/postgres/pg_hba.conf"], "\nhost all all all scram-sha-256") {
		t.Error("postgres is configured for TLS with its tls slot unbound")
	}
}
