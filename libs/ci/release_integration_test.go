//go:build integration

package ci_test

import (
	"strings"
	"testing"
)

// The lines this test looks for in act's plain output. The stub prints the first; release.yml's
// token step prints the second; act v0.2.92 prints the third when the release-please job ends
// well. A newer act may word its own line differently, so a bump of ACT_VERSION checks it.
const (
	stubRan        = "release-please stub ran"
	stubRanToken   = "release-please stub ran (token set: yes)"
	tokenMissing   = "RELEASE_PLEASE_TOKEN is not set"
	tokenStep      = "Release token is set"
	releaseJobDone = "[Release/release-please] 🏁  Job succeeded"
)

// TestReleaseGateAct covers spec 0030 AC-11 without GitHub: it runs the real release.yml with
// act in local Docker, release-please replaced by the stub in testdata. With RELEASES_ENABLED
// unset every job is skipped and the run passes; with it true and no RELEASE_PLEASE_TOKEN the
// token step fails before the stub; with both the stub runs with a token and reports no release,
// so the images and managed zip jobs stay skipped.
func TestReleaseGateAct(t *testing.T) {
	root := repoRoot(t)
	act := newActRunner(t, root)
	mappings := []string{act.stubFor(t, "release.yml", "googleapis/release-please-action", "release-please-stub")}
	event := act.event(t, `{"ref":"refs/heads/main"}`)

	tests := []struct {
		name     string
		args     []string
		wantFail bool
		want     []string
		notWant  []string
	}{
		{
			name:    "variable unset skips every job",
			notWant: []string{tokenStep, stubRan},
		},
		{
			name:     "variable true without the secret fails before release-please",
			args:     []string{"--var", "RELEASES_ENABLED=true"},
			wantFail: true,
			want:     []string{tokenMissing},
			notWant:  []string{stubRan},
		},
		{
			name: "variable true with the secret runs release-please only",
			args: []string{"--var", "RELEASES_ENABLED=true", "-s", "RELEASE_PLEASE_TOKEN=dummy"},
			want: []string{stubRanToken, releaseJobDone},
			// release_created is empty, so neither later job starts
			notWant: []string{"[Release/Release image", "[Release/Managed zip on the release"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			output, err := act.run(t, "push", ".github/workflows/release.yml", event, mappings, tt.args...)
			switch {
			case tt.wantFail && err == nil:
				t.Fatalf("act passed, want a failure\n%s", output)
			case !tt.wantFail && err != nil:
				t.Fatalf("act: %v\n%s", err, output)
			}
			for _, s := range tt.want {
				if !strings.Contains(output, s) {
					t.Errorf("output lacks %q\n%s", s, output)
				}
			}
			for _, s := range tt.notWant {
				if strings.Contains(output, s) {
					t.Errorf("output holds %q\n%s", s, output)
				}
			}
		})
	}
}
