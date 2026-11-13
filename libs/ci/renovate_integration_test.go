//go:build integration

package ci_test

import (
	"bufio"
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"
	"time"
)

// The Renovate log messages and branch this test reads, as Renovate 44.198.3 writes them in its
// JSON debug log of a lookup run. A bump of RENOVATE_VERSION checks them again.
const (
	// msgPackageFiles lists every lookup result per package file, disabled updates included.
	msgPackageFiles = "packageFiles with updates"
	// msgBranch names one proposed branch in its branch field, once per branch.
	msgBranch = "Branch is not pending, removing pending upgrades"
	// msgBranches counts the proposed branches, after the disabled updates are filtered out.
	msgBranches = "Returning 1 branch(es)"
	// msgNodeUnsupported is logged at error level because Renovate supports an older Node than
	// the repository uses; it makes Renovate exit 1, so the test reads the log, not the exit code.
	msgNodeUnsupported = "Unsupported node environment detected"
	digestBranch       = "renovate/build-image-digests"
)

var (
	digestRef  = regexp.MustCompile(`@sha256:[0-9a-f]{64}`)
	realDigest = regexp.MustCompile(`^sha256:[0-9a-f]{64}$`)
	fromImage  = regexp.MustCompile(`^FROM (\S+)@sha256:`)
)

// renovateUpdate is one proposed update of a dependency in msgPackageFiles.
type renovateUpdate struct {
	UpdateType string `json:"updateType"`
	NewDigest  string `json:"newDigest"`
	BranchName string `json:"branchName"`
}

// renovateEntry is one line of Renovate's JSON log, with the fields this test reads.
type renovateEntry struct {
	Level  int    `json:"level"`
	Msg    string `json:"msg"`
	Branch string `json:"branch"`
	Host   string `json:"registryHost"`
	Err    any    `json:"err"`
	Config map[string][]struct {
		PackageFile string `json:"packageFile"`
		Deps        []struct {
			DepName       string           `json:"depName"`
			CurrentDigest string           `json:"currentDigest"`
			Updates       []renovateUpdate `json:"updates"`
		} `json:"deps"`
	} `json:"config"`
}

// TestRenovateDigests covers spec 0030 AC-13 without the GitHub App: Renovate at the pinned
// version, in its local mode, runs a lookup on a clone whose web `# syntax=` digest and renderer
// base image digest are stale, plus a Dockerfile outside catalog/modules. It proposes exactly one
// branch, build image digests, with a real new digest for both stale lines, and touches no file
// outside catalog/modules/*/Dockerfile; the tag bumps it finds are filtered out.
func TestRenovateDigests(t *testing.T) {
	root := repoRoot(t)
	needTools(t, "git", "node", "pnpm")
	version := taskfileVar(t, root, "RENOVATE_VERSION")

	clone := filepath.Join(t.TempDir(), "repo")
	run(t, root, nil, "git", "clone", "--quiet", root, clone)
	zero := "sha256:" + strings.Repeat("0", 64)

	// a stale digest on the web # syntax= line and on the renderer's first pinned FROM line
	staleLine(t, filepath.Join(clone, "catalog/modules/web/Dockerfile"), zero, func(line string) bool {
		return strings.HasPrefix(line, "# syntax=")
	})
	renderer := staleLine(t, filepath.Join(clone, "catalog/modules/renderer/Dockerfile"), zero, func(line string) bool {
		return strings.HasPrefix(line, "FROM ")
	})
	image := fromImage.FindStringSubmatch(renderer)[1]
	// the same stale image outside catalog/modules, which the rule must leave alone
	if err := os.MkdirAll(filepath.Join(clone, "scratch"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(clone, "scratch/Dockerfile"), []byte("FROM "+image+"@"+zero+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	// Renovate's local mode lists files through git, so an untracked file would be invisible to it
	run(t, clone, nil, "git", "add", "--all")
	run(t, clone, nil, "git", "-c", "user.name=mp-test", "-c", "user.email=mp-test@example.invalid",
		"commit", "--quiet", "-m", "test: stale digests")

	ctx, cancel := context.WithTimeout(t.Context(), 15*time.Minute)
	defer cancel()
	cmd := exec.CommandContext(ctx, "pnpm", "dlx", "--package", "renovate@"+version, "renovate")
	cmd.Dir = clone
	cmd.Env = append(os.Environ(),
		"RENOVATE_PLATFORM=local",
		"RENOVATE_DRY_RUN=lookup",
		"RENOVATE_FETCH_CHANGE_LOGS=off",
		"RENOVATE_BASE_DIR="+t.TempDir(),
		"LOG_FORMAT=json",
		"LOG_LEVEL=debug",
	)
	out, err := cmd.CombinedOutput()
	entries := parseRenovateLog(out)
	if len(entries) == 0 {
		t.Fatalf("renovate wrote no JSON log: %v\n%s", err, out)
	}
	// the exit code is 1 on Node 26 (msgNodeUnsupported); any other error fails the test
	for _, e := range entries {
		if e.Level >= 50 && !strings.HasPrefix(e.Msg, msgNodeUnsupported) {
			t.Errorf("renovate logged an error: %s %v", e.Msg, e.Err)
		}
	}

	// Renovate's depName is the image without its tag
	name := image
	if i := strings.LastIndex(image, ":"); i > strings.LastIndex(image, "/") {
		name = image[:i]
	}
	checkBranches(t, entries)
	checkPackageFiles(t, entries, name, zero)
	if t.Failed() {
		if hosts := failedRegistries(entries); len(hosts) > 0 {
			t.Logf("lookups failed at %s: no network or a rate limit there?", strings.Join(hosts, ", "))
		}
	}
}

// staleLine replaces the digest on the first line of the Dockerfile at path that matches pick and
// carries a digest with zero, and returns that line as it was.
func staleLine(t *testing.T, path, zero string, pick func(string) bool) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(string(data), "\n")
	for i, line := range lines {
		if pick(line) && digestRef.MatchString(line) {
			lines[i] = digestRef.ReplaceAllString(line, "@"+zero)
			if err := os.WriteFile(path, []byte(strings.Join(lines, "\n")), 0o600); err != nil {
				t.Fatal(err)
			}
			return line
		}
	}
	t.Fatalf("%s has no matching line with a digest", path)
	return ""
}

// parseRenovateLog returns every JSON line of Renovate's output; other lines (Node warnings) are
// skipped.
func parseRenovateLog(out []byte) []renovateEntry {
	var entries []renovateEntry
	scanner := bufio.NewScanner(strings.NewReader(string(out)))
	scanner.Buffer(make([]byte, 0, 1<<20), 16<<20)
	for scanner.Scan() {
		var e renovateEntry
		if json.Unmarshal(scanner.Bytes(), &e) == nil && e.Msg != "" {
			entries = append(entries, e)
		}
	}
	return entries
}

// checkBranches checks that Renovate proposes exactly one branch, the digest group.
func checkBranches(t *testing.T, entries []renovateEntry) {
	t.Helper()
	var branches []string
	counted := false
	for _, e := range entries {
		switch {
		case e.Msg == msgBranch:
			branches = append(branches, e.Branch)
		case strings.HasPrefix(e.Msg, "Returning ") && strings.HasSuffix(e.Msg, " branch(es)"):
			if e.Msg != msgBranches {
				t.Errorf("renovate says %q, want %q", e.Msg, msgBranches)
			}
			counted = true
		}
	}
	if !counted {
		t.Errorf("renovate logged no branch count (%q)", msgBranches)
	}
	if !slices.Equal(branches, []string{digestBranch}) {
		t.Errorf("proposed branches %q, want only %q", branches, digestBranch)
	}
}

// checkPackageFiles checks the lookup results: only catalog Dockerfiles have updates, the
// Dockerfile outside catalog/modules is not read, both stale lines get a real digest on the digest
// branch, and that branch holds digest updates only.
func checkPackageFiles(t *testing.T, entries []renovateEntry, image, zero string) {
	t.Helper()
	i := slices.IndexFunc(entries, func(e renovateEntry) bool { return e.Msg == msgPackageFiles })
	if i < 0 {
		t.Fatalf("renovate logged no %q", msgPackageFiles)
	}
	fixed := map[string]bool{"docker/dockerfile": false, image: false}
	for manager, files := range entries[i].Config {
		for _, f := range files {
			if ok, _ := filepath.Match("catalog/modules/*/Dockerfile", f.PackageFile); !ok {
				t.Errorf("%s update in %s, outside catalog/modules/*/Dockerfile", manager, f.PackageFile)
			}
			for _, d := range f.Deps {
				for _, u := range d.Updates {
					if u.BranchName == digestBranch && u.UpdateType != "digest" {
						t.Errorf("%s: %s update of %s on %s", f.PackageFile, u.UpdateType, d.DepName, digestBranch)
					}
					if d.CurrentDigest != zero || u.UpdateType != "digest" {
						continue
					}
					if !realDigest.MatchString(u.NewDigest) || u.NewDigest == zero {
						t.Errorf("%s: %s gets digest %q, want a real one", f.PackageFile, d.DepName, u.NewDigest)
					}
					if u.BranchName != digestBranch {
						t.Errorf("%s: %s digest update on %q, want %q", f.PackageFile, d.DepName, u.BranchName, digestBranch)
					}
					if _, ok := fixed[d.DepName]; ok {
						fixed[d.DepName] = true
					}
				}
			}
		}
	}
	for dep, ok := range fixed {
		if !ok {
			t.Errorf("no digest update for the stale %s line", dep)
		}
	}
}

// failedRegistries names the registries where a lookup failed, apart from the expected miss of
// the zero digests, so a network or rate limit failure says where it happened.
func failedRegistries(entries []renovateEntry) []string {
	var hosts []string
	for _, e := range entries {
		if e.Err == nil || e.Host == "" || e.Msg == "Docker Manifest is unknown" {
			continue
		}
		if !slices.Contains(hosts, e.Host) {
			hosts = append(hosts, e.Host)
		}
	}
	return hosts
}
