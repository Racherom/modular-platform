// Command sdkgen writes a module's typed config getters and its sdk.Declaration from its catalog
// entry (spec 0005). Run it with go generate:
//
//	//go:generate go tool sdkgen -entry ../../../../catalog/modules/planner/module.yaml -package config -out config.gen.go
//
// It reads the capability files from -capabilities, by default the catalog's capabilities folder
// beside modules/<name>/module.yaml. It refuses an entry that breaks a catalog rule, with the
// lines the catalog check prints, and exits 1.
package main

import (
	"flag"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"strings"

	"github.com/racherom/modular-platform/sdk/manifest"
)

func main() {
	entry := flag.String("entry", "", "path of the module entry, like ../../catalog/modules/planner/module.yaml")
	capsDir := flag.String("capabilities", "", "folder of the capability files; default ../../capabilities from the entry")
	pkg := flag.String("package", "config", "Go package name of the generated file")
	out := flag.String("out", "config.gen.go", "file to write")
	flag.Parse()
	if *entry == "" {
		fmt.Fprintln(os.Stderr, "sdkgen: -entry is required")
		os.Exit(2)
	}
	if *capsDir == "" {
		*capsDir = filepath.Join(filepath.Dir(*entry), "..", "..", "capabilities")
	}
	os.Exit(run(*entry, *capsDir, *pkg, *out))
}

// readCapabilities reads every *.yaml capability file in dir by name.
func readCapabilities(dir string) (map[string]manifest.Capability, error) {
	files, err := filepath.Glob(filepath.Join(dir, "*.yaml"))
	if err != nil {
		return nil, err
	}
	caps := map[string]manifest.Capability{}
	for _, f := range files {
		data, err := os.ReadFile(f) //nolint:gosec // the catalog folder named on the command line
		if err != nil {
			return nil, err
		}
		c, err := manifest.ParseCapability(data)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", f, err)
		}
		caps[c.Name] = c
	}
	return caps, nil
}

func run(entry, capsDir, pkg, out string) int {
	caps, err := readCapabilities(capsDir)
	if err != nil {
		fmt.Fprintln(os.Stderr, "sdkgen:", err)
		return 1
	}
	data, err := os.ReadFile(entry) //nolint:gosec // the catalog entry named on the command line
	if err != nil {
		fmt.Fprintln(os.Stderr, "sdkgen:", err)
		return 1
	}
	src, problems := Generate(SourceName(entry), pkg, data, caps)
	if len(problems) > 0 {
		for _, p := range problems {
			fmt.Fprintln(os.Stderr, p)
		}
		return 1
	}
	if err := os.WriteFile(out, src, 0o644); err != nil { //nolint:gosec // generated source is committed and world readable
		fmt.Fprintln(os.Stderr, "sdkgen:", err)
		return 1
	}
	return 0
}

// SourceName is the entry path as the generated header names it: slash separated, without the
// leading ../ that go generate's working directory adds, so it reads
// catalog/modules/planner/module.yaml.
func SourceName(entry string) string {
	name := path.Clean(filepath.ToSlash(entry))
	for strings.HasPrefix(name, "../") {
		name = strings.TrimPrefix(name, "../")
	}
	return name
}
