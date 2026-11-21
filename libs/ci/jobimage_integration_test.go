//go:build integration

package ci_test

import (
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// TestOIDCJobImageRuns covers spec 0030 AC-14: the catalog Dockerfile of the job image builds
// from its own folder, carries the zip's ref as its revision label, and runs sleep infinity,
// curl and jq under the four hardening options of a job service (read only, no capabilities,
// no new privileges, the platform's user).
func TestOIDCJobImageRuns(t *testing.T) {
	root, err := filepath.Abs("../..")
	if err != nil {
		t.Fatal(err)
	}
	const tag = "mp-oidc-job-test:ac14"
	dir := filepath.Join(root, "catalog/images/oidc-job")
	run(t, root, nil, "docker", "build", "-q", "--build-arg", "MP_SOURCE_REF=ac14-ref", "-t", tag, "-f", filepath.Join(dir, "Dockerfile"), dir)
	t.Cleanup(func() { _ = exec.Command("docker", "rmi", "-f", tag).Run() })

	label := run(t, root, nil, "docker", "image", "inspect", "--format", `{{ index .Config.Labels "org.opencontainers.image.revision" }}`, tag)
	if strings.TrimSpace(label) != "ac14-ref" {
		t.Errorf("revision label %q, want ac14-ref", label)
	}
	hardened := []string{"run", "--rm", "--read-only", "--cap-drop=ALL", "--security-opt=no-new-privileges:true", "--user", "10001:10001", "--tmpfs", "/tmp"}
	out := run(t, root, nil, "docker", append(append([]string{}, hardened...), tag, "sh", "-c", "curl --version | head -1; jq -n '{a: 1}' -c; echo ok > /tmp/x && cat /tmp/x")...)
	for _, want := range []string{"curl ", `{"a":1}`, "ok"} {
		if !strings.Contains(out, want) {
			t.Errorf("output lacks %q:\n%s", want, out)
		}
	}
	id := strings.TrimSpace(run(t, root, nil, "docker", append(append([]string{}, hardened[:1]...), append([]string{"-d", "--init"}, append(hardened[2:], tag)...)...)...))
	t.Cleanup(func() { _ = exec.Command("docker", "rm", "-f", id).Run() })
	if state := strings.TrimSpace(run(t, root, nil, "docker", "inspect", "--format", "{{.State.Status}}", id)); state != "running" {
		t.Errorf("the default command (sleep infinity) is %s, want running", state)
	}
}
