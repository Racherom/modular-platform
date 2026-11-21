package catalog_test

import (
	"io/fs"
	"regexp"
	"strings"
	"testing"

	"github.com/racherom/modular-platform/catalog"
	"github.com/racherom/modular-platform/libs/contract"
	"github.com/racherom/modular-platform/libs/render"
)

// TestBuiltInCatalog runs the same checks the renderer runs at start (AC-2).
func TestBuiltInCatalog(t *testing.T) {
	t.Parallel()
	c, problems := contract.LoadCatalog(catalog.FS)
	for _, p := range problems {
		t.Error(p)
	}
	if len(c.Modules) == 0 || len(c.Capabilities) == 0 {
		t.Fatal("the built in catalog is empty")
	}
}

// TestPlatformImagesHaveDockerfiles covers the spec 0030 invariant: every module whose image is
// one of the platform's own has the Dockerfile that builds it beside its module.yaml, so a zip
// that names the image before its release can carry it.
func TestPlatformImagesHaveDockerfiles(t *testing.T) {
	t.Parallel()
	c, _ := contract.LoadCatalog(catalog.FS)
	n := 0
	for _, m := range c.Modules {
		if !strings.HasPrefix(m.Image, "ghcr.io/racherom/modular-platform/") {
			continue
		}
		n++
		if _, err := fs.Stat(catalog.FS, "modules/"+m.Name+"/Dockerfile"); err != nil {
			t.Errorf("module %s (image %s) has no Dockerfile: %v", m.Name, m.Image, err)
		}
	}
	if n == 0 {
		t.Fatal("no module names a platform image")
	}
}

// pinned matches an image reference addressed by the digest of its multi platform index.
var pinned = regexp.MustCompile(`@sha256:[0-9a-f]{64}$`)

// unpinned lists the lines of a Dockerfile that fetch from outside the repository without a
// digest (spec 0030, AC-13): the `# syntax=` line, a FROM of a registry image, and a COPY --from
// or RUN --mount from=, each unless it names scratch, an earlier stage, or holds `${`.
func unpinned(dockerfile string) []string {
	stages := map[string]bool{"scratch": true}
	var bad []string
	check := func(line, ref string) {
		if stages[ref] || strings.Contains(ref, "${") || pinned.MatchString(ref) {
			return
		}
		bad = append(bad, line)
	}
	for line := range strings.Lines(dockerfile) {
		line = strings.TrimSpace(line)
		fields := strings.Fields(line)
		if len(fields) == 0 {
			continue
		}
		if ref, ok := strings.CutPrefix(line, "# syntax="); ok {
			if !pinned.MatchString(strings.TrimSpace(ref)) {
				bad = append(bad, line)
			}
			continue
		}
		switch strings.ToUpper(fields[0]) {
		case "FROM":
			args := fields[1:]
			for len(args) > 0 && strings.HasPrefix(args[0], "--") {
				args = args[1:]
			}
			if len(args) == 0 {
				bad = append(bad, line)
				continue
			}
			check(line, args[0])
			if len(args) == 3 && strings.EqualFold(args[1], "AS") {
				stages[args[2]] = true
			}
		case "COPY", "ADD", "RUN":
			for _, f := range fields[1:] {
				if from, ok := strings.CutPrefix(f, "--from="); ok {
					check(line, from)
				}
				if mount, ok := strings.CutPrefix(f, "--mount="); ok {
					for opt := range strings.SplitSeq(mount, ",") {
						if from, ok := strings.CutPrefix(opt, "from="); ok {
							check(line, from)
						}
					}
				}
			}
		}
	}
	return bad
}

// TestPlatformDockerfilesPinned covers spec 0030 AC-13: every build input the four platform
// Dockerfiles fetch from a registry carries a digest, so two builds of one commit use the same
// frontend and base images.
func TestPlatformDockerfilesPinned(t *testing.T) {
	t.Parallel()
	files, err := fs.Glob(catalog.FS, "*/*/Dockerfile")
	if err != nil {
		t.Fatal(err)
	}
	if len(files) < 5 {
		t.Fatalf("found %d catalog Dockerfiles, want the four module images and the job image", len(files))
	}
	for _, f := range files {
		data, err := fs.ReadFile(catalog.FS, f)
		if err != nil {
			t.Fatal(err)
		}
		for _, line := range unpinned(string(data)) {
			t.Errorf("%s: %q has no @sha256: digest", f, line)
		}
		for line := range strings.Lines(string(data)) {
			if !strings.Contains(line, "apk add") {
				continue
			}
			for _, pkg := range strings.Fields(line)[2:] {
				if strings.HasPrefix(pkg, "-") || pkg == "apk" || pkg == "add" || strings.HasPrefix(pkg, "RUN") {
					continue
				}
				if !strings.Contains(pkg, "=") {
					t.Errorf("%s: apk package %q is not pinned to a version", f, pkg)
				}
			}
		}
	}
}

// TestJobImagesHaveDockerfiles covers spec 0030 AC-14g: every platform image that a capability's
// job names has its Dockerfile at images/<name>/Dockerfile, and no folder under images/ shares a
// name with a module (the zip path build/<name> must be unique).
func TestJobImagesHaveDockerfiles(t *testing.T) {
	t.Parallel()
	c, _ := contract.LoadCatalog(catalog.FS)
	n := 0
	for _, k := range c.Capabilities {
		if k.Yields == nil || !render.Unreleased(k.Yields.Job.Image) {
			continue
		}
		n++
		name, _, _ := strings.Cut(strings.TrimPrefix(k.Yields.Job.Image, render.PlatformImages), ":")
		if _, err := fs.Stat(catalog.FS, "images/"+name+"/Dockerfile"); err != nil {
			t.Errorf("capability %s (job image %s) has no images/%s/Dockerfile: %v", k.Name, k.Yields.Job.Image, name, err)
		}
	}
	if n == 0 {
		t.Fatal("no capability names an unreleased job image")
	}
	dirs, err := fs.ReadDir(catalog.FS, "images")
	if err != nil {
		t.Fatal(err)
	}
	for _, d := range dirs {
		for _, m := range c.Modules {
			if m.Name == d.Name() {
				t.Errorf("images/%s shares its name with module %s", d.Name(), m.Name)
			}
		}
	}
}

// TestUnpinned checks the pin rule itself on lines it must refuse and lines it must let pass.
func TestUnpinned(t *testing.T) {
	t.Parallel()
	const d = "@sha256:" + "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
	for name, tc := range map[string]struct {
		in  string
		bad int
	}{
		"pinned":           {in: "# syntax=docker/dockerfile:1" + d + "\nFROM golang:1" + d + " AS build\nFROM gcr.io/x:nonroot" + d + "\n"},
		"stages":           {in: "FROM scratch AS a\nFROM a AS b\nFROM src-${X} AS c\nCOPY --from=b / /\nRUN --mount=type=cache,from=c,target=/x true\n"},
		"syntax tag":       {in: "# syntax=docker/dockerfile:1\n", bad: 1},
		"from tag":         {in: "FROM golang:1.27.3-alpine AS build\n", bad: 1},
		"from platform":    {in: "FROM --platform=$BUILDPLATFORM node:26 AS build\n", bad: 1},
		"short digest":     {in: "FROM node@sha256:abc\n", bad: 1},
		"copy from image":  {in: "FROM scratch\nCOPY --from=alpine:3 /x /x\n", bad: 1},
		"mount from image": {in: "FROM scratch\nRUN --mount=type=bind,from=alpine:3,target=/a true\n", bad: 1},
		"later stage":      {in: "FROM b\nFROM scratch AS b\n", bad: 1},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			if got := unpinned(tc.in); len(got) != tc.bad {
				t.Errorf("unpinned = %q, want %d lines", got, tc.bad)
			}
		})
	}
}
