package main

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"strings"
	"testing"
	"testing/fstest"
)

// TestBadCatalogStopsStart covers AC-11: exit code 1 and one log line per problem.
func TestBadCatalogStopsStart(t *testing.T) {
	t.Setenv("MP_PLANNER_MODE", "public")
	bad := fstest.MapFS{
		"whoami.yaml": {Data: []byte(`apiVersion: modular-platform/v0alpha1
kind: Module
name: whoami
description: Demo.
image: traefik/whoami:latest
uses:
  postgresql:
    env:
      host: DEMO-HOST
`)},
	}
	var logs bytes.Buffer
	code := run(context.Background(), slog.New(slog.NewJSONHandler(&logs, nil)), bad)
	if code != 1 {
		t.Fatalf("exit code = %d, want 1", code)
	}
	var lines []string
	for _, line := range strings.Split(strings.TrimSpace(logs.String()), "\n") {
		var entry struct{ Msg string }
		if err := json.Unmarshal([]byte(line), &entry); err != nil {
			t.Fatal(err)
		}
		lines = append(lines, entry.Msg)
	}
	want := []string{
		"catalog/whoami.yaml: image: image_latest: ",
		"catalog/whoami.yaml: uses.postgresql.env.host: invalid_env_name: ",
	}
	if len(lines) != len(want) {
		t.Fatalf("log lines = %q, want %d", lines, len(want))
	}
	for i := range want {
		if !strings.HasPrefix(lines[i], want[i]) {
			t.Errorf("line %d = %q, want prefix %q", i, lines[i], want[i])
		}
	}
}
