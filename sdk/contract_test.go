package sdk_test

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

var (
	// contractStatement is a statement line of CONTRACT.md: a bullet that opens with its bold id.
	contractStatement = regexp.MustCompile(`^- \*\*([A-Z]+-\d+)\*\*: `)
	// contractTestLink is a link to a test: [`TestName`](path/to/file_test.go).
	contractTestLink = regexp.MustCompile("\\[`(Test\\w+)`\\]\\(([^)]+_test\\.go)\\)")
)

// TestContract keeps CONTRACT.md honest (spec 0005, AC-14): every statement and every table row
// names at least one test, every named test exists in the file its link points at, and no
// statement id is used twice.
func TestContract(t *testing.T) {
	t.Parallel()
	data, err := os.ReadFile("CONTRACT.md")
	if err != nil {
		t.Fatal(err)
	}
	ids := map[string]int{}
	sources := map[string]string{}
	for i, line := range strings.Split(string(data), "\n") {
		at := i + 1
		links := contractTestLink.FindAllStringSubmatch(line, -1)
		if m := contractStatement.FindStringSubmatch(line); m != nil {
			if first, ok := ids[m[1]]; ok {
				t.Errorf("CONTRACT.md:%d: %s is already used on line %d", at, m[1], first)
			}
			ids[m[1]] = at
			if len(links) == 0 {
				t.Errorf("CONTRACT.md:%d: %s names no test", at, m[1])
			}
		}
		if strings.HasPrefix(line, "| `") && len(links) == 0 {
			t.Errorf("CONTRACT.md:%d: the table row names no test", at)
		}
		for _, l := range links {
			name, path := l[1], l[2]
			src, ok := sources[path]
			if !ok {
				b, err := os.ReadFile(filepath.FromSlash(path))
				if err != nil {
					t.Errorf("CONTRACT.md:%d: %v", at, err)
					continue
				}
				src = string(b)
				sources[path] = src
			}
			if !strings.Contains(src, "\nfunc "+name+"(t *testing.T) {") {
				t.Errorf("CONTRACT.md:%d: %s is not a test in %s", at, name, path)
			}
		}
	}
	if len(ids) == 0 {
		t.Fatal("CONTRACT.md holds no statement")
	}
}
