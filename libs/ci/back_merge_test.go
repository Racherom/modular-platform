package ci_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

const (
	releaseSHA = "0123456789abcdef0123456789abcdef01234567"
	otherSHA   = "fedcba9876543210fedcba9876543210fedcba98"
)

// TestBackMergeScript covers spec 0032 AC-5 for scripts/release-back-merge.sh with the gh stub:
// the pull request is opened once; it is not opened when dev holds the commit or a pull request
// is open; an existing branch at the commit gets only its pull request; a branch at another
// commit fails; a lookup that fails for another reason fails; a bad version or SHA is refused.
func TestBackMergeScript(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		files    map[string]string // state files for the stub
		version  string
		sha      string
		wantFail bool
		wantMsg  string
		wantCall []string // call log lines that must appear, in order
		noCall   []string // substrings no call may hold
	}{
		{
			name: "fresh release creates the branch and the pull request", version: "1.2.3", sha: releaseSHA,
			wantCall: []string{"api -X POST repos/o/r/git/refs -f ref=refs/heads/back-merge/1.2.3 -f sha=" + releaseSHA, "pr create --repo o/r --base dev --head back-merge/1.2.3 --title chore(release): back merge 1.2.3"},
		},
		{
			name: "dev already ahead of the commit", files: map[string]string{"compare-status": "behind"}, version: "1.2.3", sha: releaseSHA,
			noCall: []string{"pr create", "-X POST"},
		},
		{
			name: "dev holds exactly the commit", files: map[string]string{"compare-status": "identical"}, version: "1.2.3", sha: releaseSHA,
			noCall: []string{"pr create", "-X POST"},
		},
		{
			name: "diverged dev still needs the back merge", files: map[string]string{"compare-status": "diverged"}, version: "1.2.3", sha: releaseSHA,
			wantCall: []string{"pr create"},
		},
		{
			name: "pull request already open", files: map[string]string{"prs": "back-merge/1.2.3\n", "ref-back-merge__1.2.3": releaseSHA + "\n"}, version: "1.2.3", sha: releaseSHA,
			noCall: []string{"pr create", "-X POST"},
		},
		{
			name: "branch at the commit without a pull request", files: map[string]string{"ref-back-merge__1.2.3": releaseSHA + "\n"}, version: "1.2.3", sha: releaseSHA,
			wantCall: []string{"pr create"}, noCall: []string{"-X POST"},
		},
		{
			name: "branch at another commit", files: map[string]string{"ref-back-merge__1.2.3": otherSHA + "\n"}, version: "1.2.3", sha: releaseSHA,
			wantFail: true, wantMsg: "exists at " + otherSHA, noCall: []string{"pr create", "-X POST"},
		},
		{
			name: "pull request of another version does not count", files: map[string]string{"prs": "back-merge/1.2.2\n"}, version: "1.2.3", sha: releaseSHA,
			wantCall: []string{"pr create"},
		},
		{name: "version with a shell word", version: "1.2.3; touch x", sha: releaseSHA, wantFail: true, wantMsg: "is not a version"},
		{name: "short sha", version: "1.2.3", sha: "abc123", wantFail: true, wantMsg: "full commit SHA"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			state := t.TempDir()
			for name, content := range tt.files {
				if err := os.WriteFile(filepath.Join(state, name), []byte(content), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			stub, err := filepath.Abs("testdata/gh-stub/gh")
			if err != nil {
				t.Fatal(err)
			}
			cmd := exec.Command("sh", "../../scripts/release-back-merge.sh", tt.version, tt.sha)
			cmd.Env = append(os.Environ(), "GITHUB_REPOSITORY=o/r", "GH="+stub, "GH_STUB_STATE="+state)
			out, err := cmd.CombinedOutput()
			if failed := err != nil; failed != tt.wantFail {
				t.Fatalf("failed = %v, want %v\n%s", failed, tt.wantFail, out)
			}
			if !strings.Contains(string(out), tt.wantMsg) {
				t.Errorf("output lacks %q\n%s", tt.wantMsg, out)
			}
			log, _ := os.ReadFile(filepath.Join(state, "calls.log"))
			calls := string(log)
			from := 0
			for _, want := range tt.wantCall {
				i := strings.Index(calls[from:], want)
				if i < 0 {
					t.Fatalf("call log lacks %q after offset %d\n%s", want, from, calls)
				}
				from += i + len(want)
			}
			for _, no := range tt.noCall {
				if strings.Contains(calls, no) {
					t.Errorf("call log holds %q\n%s", no, calls)
				}
			}
			if _, err := os.Stat("x"); err == nil {
				t.Error("a version was run as a command")
			}
		})
	}
}
