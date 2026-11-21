package catalog_test

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/racherom/modular-platform/catalog"
	"github.com/racherom/modular-platform/libs/contract"
)

// fakeCurl stands in for curl in the revoke script: a list call answers the body and status of
// the page it asks for (page<N>.json and page<N>.code in $FAKE, the status 200 when no code file
// is there), and a DELETE records the client id in $FAKE/deleted and answers 204.
const fakeCurl = `#!/bin/sh
out=; m=GET; url=
while [ $# -gt 0 ]; do
  case $1 in
    -o) out=$2; shift ;;
    -X) m=$2; shift ;;
    -w|-H|--connect-timeout|--max-time) shift ;;
    -*) ;;
    *) url=$1 ;;
  esac
  shift
done
if [ "$m" = DELETE ]; then
  echo "${url##*/}" >> "$FAKE/deleted"
  : > "$out"
  printf 204
  exit 0
fi
page=${url##*%5Bpage%5D=}
cp "$FAKE/page$page.json" "$out"
if [ -f "$FAKE/page$page.code" ]; then cat "$FAKE/page$page.code"; else printf 200; fi
`

// TestOIDCRevokeScript runs the revoke script of oidc-provisioner against a fake provider
// (spec 0037 AC-12): it deletes the marked clients no binding keeps, and it deletes nothing and
// prints no response body when any page of the list fails, whether by status, by a body that is
// not the client list, or by a page count that is not a number.
func TestOIDCRevokeScript(t *testing.T) {
	t.Parallel()
	for _, tool := range []string{"sh", "jq"} {
		if _, err := exec.LookPath(tool); err != nil {
			t.Skipf("%s is not installed", tool)
		}
	}
	script := revokeScript(t)

	const (
		page1    = `{"data":[{"id":"old-a","description":"mp-marker:stack1"},{"id":"api","description":"mp-marker:stack1"},{"id":"hand","description":"made by hand"}],"pagination":{"totalPages":2}}`
		page2    = `{"data":[{"id":"old-b","description":"mp-marker:stack1"},{"id":"other","description":"mp-marker:stack2"}],"pagination":{"totalPages":2}}`
		badBody  = `<html>secret-looking proxy page</html>`
		leakBody = `{"data":"secret-looking value","pagination":{"totalPages":2}}`
	)

	tests := []struct {
		name        string
		pages       []string // body of page 1, 2, ...
		codes       map[int]string
		wantFail    bool
		wantDeleted []string
	}{
		{
			name:        "two good pages",
			pages:       []string{page1, page2},
			wantDeleted: []string{"old-a", "old-b"},
		},
		{
			name:        "one page with no page count",
			pages:       []string{`{"data":[{"id":"old-a","description":"mp-marker:stack1"}]}`},
			wantDeleted: []string{"old-a"},
		},
		{name: "second page fails by status", pages: []string{page1, page2}, codes: map[int]string{2: "502"}, wantFail: true},
		{name: "second page is not json", pages: []string{page1, badBody}, wantFail: true},
		{name: "second page has no client list", pages: []string{page1, `{}`}, wantFail: true},
		{name: "second page has a list that is not an array", pages: []string{page1, leakBody}, wantFail: true},
		{
			name:     "page count is not a number",
			pages:    []string{`{"data":[{"id":"old-a","description":"mp-marker:stack1"}],"pagination":{"totalPages":"two"}}`},
			wantFail: true,
		},
		{
			name:     "page count is empty",
			pages:    []string{`{"data":[{"id":"old-a","description":"mp-marker:stack1"}],"pagination":{"totalPages":""}}`},
			wantFail: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			dir := t.TempDir()
			bin := filepath.Join(dir, "bin")
			fake := filepath.Join(dir, "fake")
			for _, d := range []string{bin, fake} {
				if err := os.Mkdir(d, 0o700); err != nil {
					t.Fatal(err)
				}
			}
			write(t, filepath.Join(bin, "curl"), fakeCurl, 0o700)
			write(t, filepath.Join(dir, "key"), "test-key\n", 0o600)
			for i, body := range tt.pages {
				write(t, filepath.Join(fake, "page"+strconv.Itoa(i+1)+".json"), body, 0o600)
			}
			for n, code := range tt.codes {
				write(t, filepath.Join(fake, "page"+strconv.Itoa(n)+".code"), code, 0o600)
			}

			// The script works in the job's own /tmp; each case gets its own folder instead.
			work := filepath.Join(dir, "work")
			if err := os.Mkdir(work, 0o700); err != nil {
				t.Fatal(err)
			}
			cmd := exec.CommandContext(t.Context(), "sh", "-c", strings.ReplaceAll(script, "/tmp/", work+"/"))
			cmd.Env = []string{
				"PATH=" + bin + string(os.PathListSeparator) + os.Getenv("PATH"),
				"FAKE=" + fake,
				"MP_ADMIN_API_KEY_FILE=" + filepath.Join(dir, "key"),
				"MP_ADMIN_URL=http://pocket-id:1411",
				"MP_MARKER=stack1",
				"MP_KEEP=api,web",
			}
			var out bytes.Buffer
			cmd.Stdout, cmd.Stderr = &out, &out
			err := cmd.Run()

			if tt.wantFail && err == nil {
				t.Errorf("the script succeeded, want a failure; output:\n%s", out.String())
			}
			if !tt.wantFail && err != nil {
				t.Errorf("the script failed: %v; output:\n%s", err, out.String())
			}
			if strings.Contains(out.String(), "secret-looking") {
				t.Errorf("the script printed a response body:\n%s", out.String())
			}
			deleted, _ := os.ReadFile(filepath.Join(fake, "deleted"))
			if got, want := strings.Fields(string(deleted)), tt.wantDeleted; strings.Join(got, ",") != strings.Join(want, ",") {
				t.Errorf("deleted %v, want %v", got, want)
			}
			if left, _ := os.ReadDir(work); len(left) != 0 {
				t.Errorf("the script left %d temp files behind", len(left))
			}
		})
	}
}

func revokeScript(t *testing.T) string {
	t.Helper()
	c, problems := contract.LoadCatalog(catalog.FS)
	if len(problems) != 0 {
		t.Fatalf("the built in catalog has problems: %v", problems)
	}
	for _, k := range c.Capabilities {
		if k.Name == "oidc-provisioner" && k.Yields != nil && k.Yields.Job.Revoke != "" {
			return k.Yields.Job.Revoke
		}
	}
	t.Fatal("oidc-provisioner has no revoke script")
	return ""
}

func write(t *testing.T, path, content string, mode os.FileMode) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), mode); err != nil {
		t.Fatal(err)
	}
}
