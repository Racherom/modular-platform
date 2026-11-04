package api

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"
)

type fakeAdminStore struct {
	resets int
	err    error
}

func (f *fakeAdminStore) ResetAdmin(context.Context) error {
	f.resets++
	return f.err
}

func TestAdminReset(t *testing.T) {
	t.Parallel()
	for name, tc := range map[string]struct {
		args    []string
		err     error
		resets  int
		wantErr error
		out     string
	}{
		"reset prints one line": {args: []string{"reset"}, resets: 1, out: adminResetDone + "\n"},
		"no subcommand":         {args: nil, wantErr: errAdminUsage},
		"unknown subcommand":    {args: []string{"delete"}, wantErr: errAdminUsage},
		"extra argument":        {args: []string{"reset", "now"}, wantErr: errAdminUsage},
		"store fails":           {args: []string{"reset"}, err: errors.New("db down"), resets: 1, wantErr: errors.New("db down")},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			st := &fakeAdminStore{err: tc.err}
			var out bytes.Buffer
			err := adminReset(t.Context(), st, tc.args, &out)
			switch {
			case tc.wantErr == nil && err != nil:
				t.Fatalf("err = %v", err)
			case tc.wantErr != nil && (err == nil || !strings.Contains(err.Error(), tc.wantErr.Error())):
				t.Fatalf("err = %v, want %v", err, tc.wantErr)
			}
			if st.resets != tc.resets || out.String() != tc.out {
				t.Errorf("resets %d out %q, want %d %q", st.resets, out.String(), tc.resets, tc.out)
			}
			if strings.Count(out.String(), "\n") > 1 {
				t.Errorf("more than one line: %q", out.String())
			}
		})
	}
}
