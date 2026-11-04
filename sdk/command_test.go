package sdk_test

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"strings"
	"testing"

	"github.com/racherom/modular-platform/sdk"
	"github.com/racherom/modular-platform/sdk/internal/testmodule"
)

// TestCommand covers a command added with WithCommand: it gets the loaded config and its
// arguments, writes to out, logs nothing on success, and exits 1 on a refused start or an error.
func TestCommand(t *testing.T) {
	t.Parallel()
	echo := func(ctx context.Context, m *tmModule, args []string, out io.Writer) error {
		if len(args) == 1 && args[0] == "fail" {
			return errors.New("asked to fail")
		}
		key, err := m.Config.ApiKey(ctx)
		if err != nil {
			return err
		}
		_, err = fmt.Fprintf(out, "%s %s %d\n", m.Instance, strings.Join(args, " "), len(key.Reveal()))
		return err
	}
	for name, tc := range map[string]struct {
		env     map[string]string
		command string
		args    []string
		code    int
		out     string
		log     string // a log line must hold it; empty: no log line at all
	}{
		"runs with the loaded config": {env: baseEnv(t), command: "echo", args: []string{"a", "b"}, out: "tm a b 13\n"},
		"its error exits 1":           {env: baseEnv(t), command: "echo", args: []string{"fail"}, code: 1, log: "command failed"},
		"a refused start exits 1":     {env: map[string]string{"MP_INSTANCE": "tm"}, command: "echo", code: 1, log: "missing"},
		"an unknown command exits 1":  {env: baseEnv(t), command: "nope", code: 1, log: "no such command"},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			var out, logs bytes.Buffer
			code := sdk.RunCommand(t.Context(), testmodule.Module, tc.command, tc.args, &out,
				sdk.WithEnv(tc.env), sdk.WithLogger(slog.New(slog.NewJSONHandler(&logs, nil))),
				sdk.WithCommand("echo", sdk.Command[testmodule.Config, testmodule.Uses](echo)))
			if code != tc.code || out.String() != tc.out {
				t.Errorf("code %d out %q, want %d %q", code, out.String(), tc.code, tc.out)
			}
			if tc.log == "" && logs.Len() > 0 {
				t.Errorf("a command that worked logged:\n%s", logs.String())
			}
			if tc.log != "" && !strings.Contains(logs.String(), tc.log) {
				t.Errorf("logs miss %q:\n%s", tc.log, logs.String())
			}
		})
	}
}
