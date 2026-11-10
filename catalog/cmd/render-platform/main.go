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

// platformImages starts the images of the platform's own services.
const platformImages = "ghcr.io/racherom/modular-platform/"

func main() {
	graph := flag.String("graph", "deploy/managed/graph.yaml", "the platform graph")
	out := flag.String("out", "build/managed", "the folder to write; replaced as a whole")
	tag := flag.String("image-tag", "", "a tag for the platform's own images instead of the catalog's, for a local or CI run")
	flag.Parse()
	if err := run(*graph, *out, *tag, os.Stderr); err != nil {
		fmt.Fprintln(os.Stderr, "render-platform:", err)
		os.Exit(1)
	}
}

func run(graphPath, out, tag string, log io.Writer) error {
	entries, problems := contract.LoadCatalog(catalog.FS)
	if len(problems) > 0 {
		return fmt.Errorf("the built in catalog has %d problems, the first: %v", len(problems), problems[0])
	}
	if tag != "" {
		for i, m := range entries.Modules {
			if name, ok := strings.CutPrefix(m.Image, platformImages); ok {
				repo, _, _ := strings.Cut(name, ":")
				entries.Modules[i].Image = platformImages + repo + ":" + tag
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
	folder, err := render.Platform(result, entries, templates)
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
