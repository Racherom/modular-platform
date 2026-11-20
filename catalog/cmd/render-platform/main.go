// Command render-platform renders the managed platform's own graph with the built in catalog
// into the folder the release zip ships (spec 0015): `task managed:render` runs it as
//
//	render-platform -graph deploy/managed/graph.yaml -out build/managed
//
// It checks the graph as the renderer does and writes libs/render's platform flavor into -out,
// replacing what was there, so nothing rendered is kept under version control to drift. A check result
// with an error writes nothing and exits 1.
//
// -image-tag retags the platform's own images (those under ghcr.io/racherom/modular-platform/)
// for a local or CI run, such as the ci tag the smoke-managed job builds: setup.sh runs a setup
// step in its module's image, which a Compose overlay cannot change. A release render leaves it
// empty and takes every tag from the catalog entries.
//
// -source-ref is the git ref of this repository the platform images build from while they carry
// the unreleased tag 0.0.0 (spec 0030): the folder then holds their Dockerfiles, and setup.sh
// builds them first. Empty means main, with a warning; `task managed:render` passes the commit.
// The retag comes first, so a dev or ci render builds nothing.
package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/racherom/modular-platform/catalog"
	"github.com/racherom/modular-platform/libs/check"
	"github.com/racherom/modular-platform/libs/contract"
	"github.com/racherom/modular-platform/libs/render"
)

func main() {
	graph := flag.String("graph", "deploy/managed/graph.yaml", "the platform graph")
	out := flag.String("out", "build/managed", "the folder to write; replaced as a whole")
	tag := flag.String("image-tag", "", "a tag for the platform's own images instead of the catalog's, for a local or CI run")
	sourceRef := flag.String("source-ref", "", "the git ref unreleased platform images build from (a full commit, a branch, or local for a checkout with changes); empty means main")
	flag.Parse()
	if err := run(*graph, *out, *tag, *sourceRef, os.Stderr); err != nil {
		fmt.Fprintln(os.Stderr, "render-platform:", err)
		os.Exit(1)
	}
}

func run(graphPath, out, tag, sourceRef string, log io.Writer) error {
	source, err := render.NewSourceRef(sourceRef)
	if err != nil {
		return err
	}
	switch {
	case source.Local():
		fmt.Fprintln(log, "warning: source ref local: this render comes from a local checkout, and a zip that builds a platform image builds only with MP_SOURCE_PATH")
	case source.Moves():
		fmt.Fprintf(log, "warning: source ref %s is a branch, not a commit: a zip that builds a platform image builds whatever it holds then\n", source)
	}
	entries, problems := contract.LoadCatalog(catalog.FS)
	if len(problems) > 0 {
		return fmt.Errorf("the built in catalog has %d problems, the first: %v", len(problems), problems[0])
	}
	if tag != "" {
		for i, m := range entries.Modules {
			if name, ok := strings.CutPrefix(m.Image, render.PlatformImages); ok {
				repo, _, _ := strings.Cut(name, ":")
				entries.Modules[i].Image = render.PlatformImages + repo + ":" + tag
			}
		}
	}
	templates, err := contract.LoadTemplates(catalog.FS, entries)
	if err != nil {
		return fmt.Errorf("templates: %w", err)
	}
	data, err := os.ReadFile(graphPath) //nolint:gosec // the graph file named on the command line
	if err != nil {
		return fmt.Errorf("read the graph: %w", err)
	}
	g, p := contract.DecodeGraph(data)
	if p != nil {
		return fmt.Errorf("%s: %s", graphPath, p.Message)
	}
	result := check.Graph(g, entries)
	for _, p := range result.Problems {
		fmt.Fprintf(log, "%s %s at %s: %s\n", p.Severity, p.Code, p.Path, p.Message)
	}
	if !result.Ok {
		return errors.New("the platform graph does not pass its checks")
	}
	dockerfiles, err := contract.LoadDockerfiles(catalog.FS, entries)
	if err != nil {
		return err
	}
	folder, err := render.Platform(result, entries, templates, render.WithSource(source, dockerfiles))
	if err != nil {
		return err
	}
	if err := os.RemoveAll(out); err != nil {
		return fmt.Errorf("clear %s: %w", out, err)
	}
	for _, f := range folder.Files {
		path := filepath.Join(out, filepath.FromSlash(f.Path))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil { //nolint:gosec // a release folder, read by everyone who installs it
			return fmt.Errorf("write %s: %w", path, err)
		}
		if err := os.WriteFile(path, f.Data, f.Mode); err != nil {
			return fmt.Errorf("write %s: %w", path, err)
		}
	}
	fmt.Fprintf(log, "rendered %d files into %s\n", len(folder.Files), out)
	return nil
}
