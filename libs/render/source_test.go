package render_test

import (
	"errors"
	"testing"

	"github.com/racherom/modular-platform/libs/render"
)

// TestNewSourceRef covers spec 0030 AC-10: a full commit or a branch name is kept, an empty ref
// means main, and a short commit (or a name of only 4 to 39 hex digits), a ref with a quote,
// space or #, and a name git refuses (.., //, a leading or trailing /, a trailing ., a component
// starting with . or ending with .lock) are refused.
func TestNewSourceRef(t *testing.T) {
	t.Parallel()
	const commit = "b5f34e9a1c2d3e4f5a6b7c8d9e0f1a2b3c4d5e6f"
	for name, tc := range map[string]struct {
		in, want string
		commit   bool
		err      bool
	}{
		"full commit":   {in: commit, want: commit, commit: true},
		"branch":        {in: "main", want: "main"},
		"branch path":   {in: "release-please--branches--main", want: "release-please--branches--main"},
		"tag":           {in: "v1.2.3", want: "v1.2.3"},
		"empty":         {in: "", want: "main"},
		"short commit":  {in: "b5f34e9", err: true},
		"space":         {in: "my branch", err: true},
		"quote":         {in: "it's", err: true},
		"hash":          {in: "main#x", err: true},
		"option":        {in: "-x", err: true},
		"upper commit":  {in: "B5F34E9A1C2D3E4F5A6B7C8D9E0F1A2B3C4D5E6F", want: "B5F34E9A1C2D3E4F5A6B7C8D9E0F1A2B3C4D5E6F"},
		"long hex name": {in: commit + "0", want: commit + "0"},
		"hex name":      {in: "cafe", err: true},
		"dot dot":       {in: "a..b", err: true},
		"double slash":  {in: "a//b", err: true},
		"lead slash":    {in: "/main", err: true},
		"trail slash":   {in: "main/", err: true},
		"trail dot":     {in: "main.", err: true},
		"dot":           {in: ".", err: true},
		"dot component": {in: "feat/.x", err: true},
		"lock":          {in: "feat/x.lock", err: true},
		"lock inside":   {in: "x.lock/y", err: true},
		"dots in name":  {in: "v1.2.3-rc.1", want: "v1.2.3-rc.1"},
		"nested branch": {in: "feat/release-gate", want: "feat/release-gate"},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			ref, err := render.NewSourceRef(tc.in)
			if tc.err {
				if !errors.Is(err, render.ErrSourceRef) {
					t.Fatalf("NewSourceRef(%q) error %v, want ErrSourceRef", tc.in, err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if ref.String() != tc.want || ref.Commit() != tc.commit {
				t.Errorf("NewSourceRef(%q) = %q commit %v, want %q commit %v", tc.in, ref, ref.Commit(), tc.want, tc.commit)
			}
		})
	}
	if got := (render.SourceRef{}).String(); got != render.DefaultSourceRef {
		t.Errorf("zero SourceRef = %q, want %q", got, render.DefaultSourceRef)
	}
}
