// Command sdkgen writes a module's typed config getters and its sdk.Declaration from its catalog
// entry (spec 0005). Run it with go generate:
//
//	//go:generate go tool sdkgen -entry ../../../../catalog/planner.yaml -package config -out config.gen.go
//
// It refuses an entry that breaks a catalog rule, with the lines the catalog check prints, and
// exits 1.
package main

import (
	"flag"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"strings"
)

func main() {
	entry := flag.String("entry", "", "path of the catalog entry, like ../../catalog/planner.yaml")
	pkg := flag.String("package", "config", "Go package name of the generated file")
	out := flag.String("out", "config.gen.go", "file to write")
	flag.Parse()
	if *entry == "" {
		fmt.Fprintln(os.Stderr, "sdkgen: -entry is required")
		os.Exit(2)
	}
	os.Exit(run(*entry, *pkg, *out))
}

func run(entry, pkg, out string) int {
	data, err := os.ReadFile(entry) //nolint:gosec // the catalog entry named on the command line
	if err != nil {
		fmt.Fprintln(os.Stderr, "sdkgen:", err)
		return 1
	}
	src, problems := Generate(SourceName(entry), pkg, data)
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
// leading ../ that go generate's working directory adds, so it reads catalog/planner.yaml.
func SourceName(entry string) string {
	name := path.Clean(filepath.ToSlash(entry))
	for strings.HasPrefix(name, "../") {
		name = strings.TrimPrefix(name, "../")
	}
	return name
}
