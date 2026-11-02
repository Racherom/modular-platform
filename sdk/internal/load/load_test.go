package load_test

import (
	"context"
	"fmt"
	"io/fs"
	"strings"
	"testing"
	"time"

	"github.com/racherom/modular-platform/sdk/internal/load"
)

// lookup reads env from m.
func lookup(m map[string]string) func(string) (string, bool) {
	return func(key string) (string, bool) {
		v, ok := m[key]
		return v, ok
	}
}

// readFile reads files from m; a path not in m does not exist.
func readFile(m map[string]string) func(string) ([]byte, error) {
	return func(path string) ([]byte, error) {
		data, ok := m[path]
		if !ok {
			return nil, fs.ErrNotExist
		}
		return []byte(data), nil
	}
}

var greeting = load.ValueSpec{Name: "greeting", Env: "TM_GREETING", Type: "string", Default: "hello"}

// TestLoadPrecedence covers the start levels from the top down: the start source, the process
// env, the file a _FILE env names, an env file line and the default. Each case removes the level
// above it, and the value and its config loaded label follow the highest level left (covers:
// AC-2, AC-5, AC-10).
func TestLoadPrecedence(t *testing.T) {
	t.Parallel()
	files := map[string]string{
		"/run/greeting": "fromfile\n",
		"/run/empty":    " \n",
		"/etc/tm.env":   "# the module's values\nTM_GREETING=fromenvfile\n",
	}
	tests := []struct {
		name       string
		source     map[string]string // nil: no start source
		env        map[string]string
		wantValue  string
		wantSource string
	}{
		{
			name:       "the start source beats every other level",
			source:     map[string]string{"greeting": "fromsource"},
			env:        map[string]string{"TM_GREETING": "fromenv", "MP_ENV_FILE": "/etc/tm.env"},
			wantValue:  "fromsource",
			wantSource: "source",
		},
		{
			name:       "an empty source answer falls through to the env",
			source:     map[string]string{"greeting": ""},
			env:        map[string]string{"TM_GREETING": "fromenv", "MP_ENV_FILE": "/etc/tm.env"},
			wantValue:  "fromenv",
			wantSource: "env",
		},
		{
			name:       "the env beats an env file",
			env:        map[string]string{"TM_GREETING": "fromenv", "MP_ENV_FILE": "/etc/tm.env"},
			wantValue:  "fromenv",
			wantSource: "env",
		},
		{
			name:       "the _FILE form beats an env file, its trailing newline trimmed",
			env:        map[string]string{"TM_GREETING_FILE": "/run/greeting", "MP_ENV_FILE": "/etc/tm.env"},
			wantValue:  "fromfile",
			wantSource: "file:/run/greeting",
		},
		{
			name:       "an empty _FILE file falls through to the env file",
			env:        map[string]string{"TM_GREETING_FILE": "/run/empty", "MP_ENV_FILE": "/etc/tm.env"},
			wantValue:  "fromenvfile",
			wantSource: "env-file:/etc/tm.env:2",
		},
		{
			name:       "an env file line names its file and line",
			env:        map[string]string{"MP_ENV_FILE": "/etc/tm.env"},
			wantValue:  "fromenvfile",
			wantSource: "env-file:/etc/tm.env:2",
		},
		{
			name:       "an empty env value counts as unset",
			env:        map[string]string{"TM_GREETING": ""},
			wantValue:  "hello",
			wantSource: "default",
		},
		{
			name:       "the default comes last",
			env:        map[string]string{},
			wantValue:  "hello",
			wantSource: "default",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			env := map[string]string{"MP_INSTANCE": "tm"}
			for k, v := range tt.env {
				env[k] = v
			}
			in := load.Input{Values: []load.ValueSpec{greeting}, LookupEnv: lookup(env), ReadFile: readFile(files)}
			if tt.source != nil {
				in.Source = func(_ context.Context, value string) (string, bool, error) {
					text, ok := tt.source[value]
					return text, ok, nil
				}
				in.SourceDeadline = time.Second
			}

			res := load.Load(t.Context(), in)

			if len(res.Problems) != 0 {
				t.Fatalf("problems = %v", res.Problems)
			}
			if got := res.Values["greeting"]; got != tt.wantValue {
				t.Errorf("value = %v, want %q", got, tt.wantValue)
			}
			if got := res.Sources["greeting"]; got != tt.wantSource {
				t.Errorf("source = %q, want %q", got, tt.wantSource)
			}
		})
	}
}

// TestLoadLaterEnvFileWins covers MP_ENV_FILE with two files: the later one overrides the
// earlier one, and the label names the file that won (covers: AC-2).
func TestLoadLaterEnvFileWins(t *testing.T) {
	t.Parallel()
	files := map[string]string{
		"/etc/base.env":  "TM_GREETING=base\n",
		"/etc/local.env": "\nTM_GREETING=local\n",
	}
	env := map[string]string{"MP_INSTANCE": "tm", "MP_ENV_FILE": "/etc/base.env:/etc/local.env"}

	res := load.Load(t.Context(), load.Input{Values: []load.ValueSpec{greeting}, LookupEnv: lookup(env), ReadFile: readFile(files)})

	if res.Values["greeting"] != "local" || res.Sources["greeting"] != "env-file:/etc/local.env:2" {
		t.Errorf("greeting = %v from %q, want local from env-file:/etc/local.env:2", res.Values["greeting"], res.Sources["greeting"])
	}
}

// TestSnapshotLabels covers the labels of a pushed snapshot: a pushed value shows push, a left
// out value with a default shows default, a left out optional value shows unset and has no value
// (covers: AC-5, AC-10).
func TestSnapshotLabels(t *testing.T) {
	t.Parallel()
	specs := []load.ValueSpec{
		greeting,
		{Name: "count", Env: "TM_COUNT", Type: "int", Default: "5"},
		{Name: "note", Env: "TM_NOTE", Type: "string", Optional: true},
	}

	values, sources, problems := load.Snapshot(specs, map[string]string{"greeting": "hi"})

	if len(problems) != 0 {
		t.Fatalf("problems = %v", problems)
	}
	wantSources := map[string]string{"greeting": "push", "count": "default", "note": "unset"}
	for name, want := range wantSources {
		if sources[name] != want {
			t.Errorf("source of %s = %q, want %q", name, sources[name], want)
		}
	}
	if values["greeting"] != "hi" || values["count"] != 5 {
		t.Errorf("values = %v, want greeting hi and count 5", values)
	}
	if _, ok := values["note"]; ok {
		t.Errorf("note = %v, want no value", values["note"])
	}
}

// TestSnapshotRefusals covers a snapshot refused whole: a required value left out is missing, a
// bad value breaks its rule, and a name the entry does not declare is unknown_key (covers: AC-10).
func TestSnapshotRefusals(t *testing.T) {
	t.Parallel()
	specs := []load.ValueSpec{
		{Name: "region", Env: "TM_REGION", Type: "string"},
		{Name: "count", Env: "TM_COUNT", Type: "int", Default: "5"},
	}
	tests := []struct {
		name     string
		snapshot map[string]string
		want     []string // the problem lines, sorted
	}{
		{
			name:     "a required value left out",
			snapshot: map[string]string{"count": "3"},
			want:     []string{"config: region (TM_REGION): missing: a value is required; the snapshot leaves it out"},
		},
		{
			name:     "a value that breaks its rule",
			snapshot: map[string]string{"region": "eu", "count": "many"},
			want:     []string{`config: count (TM_COUNT): invalid: "many" is not a whole number`},
		},
		{
			name:     "an undeclared name",
			snapshot: map[string]string{"region": "eu", "colour": "red"},
			want:     []string{"config: colour (colour): unknown_key: the snapshot names colour, which the catalog entry does not declare"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			_, _, problems := load.Snapshot(specs, tt.snapshot)

			if got := lines(problems); strings.Join(got, "\n") != strings.Join(tt.want, "\n") {
				t.Errorf("problems =\n%s\nwant\n%s", strings.Join(got, "\n"), strings.Join(tt.want, "\n"))
			}
		})
	}
}

// The data bindings of the test module, as sdkgen writes them.
var (
	dbBinding = load.BindingSpec{Capability: "postgresql", Client: "postgresql", Env: map[string]string{
		"url": "TM_DB_URL", "database": "TM_DB_NAME", "user": "TM_DB_USER",
		"password_file": "TM_DB_PASSWORD_FILE", "sslmode": "TM_DB_SSLMODE", "ca_file": "TM_DB_CA_FILE",
	}}
	storeBinding = load.BindingSpec{Capability: "storage", Client: "s3", Env: map[string]string{
		"url": "TM_STORE_URL", "bucket": "TM_STORE_BUCKET", "region": "TM_STORE_REGION",
		"access_key_id": "TM_STORE_ACCESS_KEY_ID", "secret_access_key_file": "TM_STORE_SECRET_KEY_FILE", "ca_file": "TM_STORE_CA_FILE",
	}}
)

const (
	dbPassword = "db-password-value"
	s3Secret   = "s3-secret-value"
)

// dataEnv is a full env for both data bindings; over replaces or adds keys.
func dataEnv(over map[string]string) map[string]string {
	env := map[string]string{
		"MP_INSTANCE":              "tm",
		"TM_DB_URL":                "postgres://db.test:5432",
		"TM_DB_NAME":               "app",
		"TM_DB_USER":               "app",
		"TM_DB_PASSWORD_FILE":      "/run/secrets/db",
		"TM_STORE_URL":             "http://store.test:9000",
		"TM_STORE_BUCKET":          "files",
		"TM_STORE_ACCESS_KEY_ID":   "key-id",
		"TM_STORE_SECRET_KEY_FILE": "/run/secrets/s3",
	}
	for k, v := range over {
		env[k] = v
	}
	return env
}

func dataFiles(over map[string]string) map[string]string {
	files := map[string]string{"/run/secrets/db": dbPassword + "\n", "/run/secrets/s3": s3Secret + " \r\n"}
	for k, v := range over {
		files[k] = v
	}
	return files
}

func loadData(t *testing.T, env, files map[string]string) load.Result {
	t.Helper()
	return load.Load(t.Context(), load.Input{
		Bindings:  []load.BindingSpec{dbBinding, storeBinding},
		LookupEnv: lookup(env),
		ReadFile:  readFile(files),
	})
}

// TestDataBindingKeys covers the plain keys of the data bindings: database, user, bucket, region
// and access key id follow the env and an env file like any key, the env winning; region stays
// empty when unset, so the s3 client signs for its default (covers: AC-6).
func TestDataBindingKeys(t *testing.T) {
	t.Parallel()
	files := dataFiles(map[string]string{"/etc/tm.env": "TM_DB_USER=fromfile\nTM_STORE_REGION=eu-west-1\n"})
	tests := []struct {
		name       string
		env        map[string]string
		wantUser   string
		wantRegion string
	}{
		{"the env sets them", nil, "app", ""},
		{"an env file sets them", map[string]string{"TM_DB_USER": "", "MP_ENV_FILE": "/etc/tm.env"}, "fromfile", "eu-west-1"},
		{"the env beats the env file", map[string]string{"TM_STORE_REGION": "eu-central-1", "MP_ENV_FILE": "/etc/tm.env"}, "app", "eu-central-1"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			res := loadData(t, dataEnv(tt.env), files)

			if len(res.Problems) != 0 || len(res.Bindings) != 2 {
				t.Fatalf("bindings = %d, problems = %v", len(res.Bindings), res.Problems)
			}
			db, store := res.Bindings[0], res.Bindings[1]
			if db.User != tt.wantUser || db.Database != "app" || db.SSLMode != load.SSLModeDisable {
				t.Errorf("postgresql = user %q database %q sslmode %q, want user %q database app sslmode disable", db.User, db.Database, db.SSLMode, tt.wantUser)
			}
			if store.Region != tt.wantRegion || store.Bucket != "files" || store.AccessKeyID != "key-id" {
				t.Errorf("s3 = region %q bucket %q key id %q, want region %q bucket files key id key-id", store.Region, store.Bucket, store.AccessKeyID, tt.wantRegion)
			}
		})
	}
}

// TestDataBindingCredentials covers the password and secret key: each is its file's content,
// trailing whitespace trimmed, and hides itself from fmt (covers: AC-4, AC-6).
func TestDataBindingCredentials(t *testing.T) {
	t.Parallel()

	res := loadData(t, dataEnv(nil), dataFiles(nil))

	if len(res.Problems) != 0 || len(res.Bindings) != 2 {
		t.Fatalf("bindings = %d, problems = %v", len(res.Bindings), res.Problems)
	}
	db, store := res.Bindings[0], res.Bindings[1]
	if got := db.Password.Reveal(); got != dbPassword {
		t.Errorf("password = %q, want %q", got, dbPassword)
	}
	if got := store.SecretKey.Reveal(); got != s3Secret {
		t.Errorf("secret key = %q, want %q", got, s3Secret)
	}
	for _, verb := range []string{"%v", "%+v", "%#v", "%s"} {
		out := fmt.Sprintf(verb, res.Bindings)
		if strings.Contains(out, dbPassword) || strings.Contains(out, s3Secret) {
			t.Errorf("%s shows a credential: %s", verb, out)
		}
	}
}

// TestDataBindingCredentialProblems covers a credential file that is empty, unreadable or not
// named: one line for that key, no credential text, and the binding is left out (covers: AC-9).
func TestDataBindingCredentialProblems(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string
		env   map[string]string
		files map[string]string
		want  string
	}{
		{
			name:  "an empty password file",
			files: map[string]string{"/run/secrets/db": "\n"},
			want:  "config: uses.postgresql.password_file (TM_DB_PASSWORD_FILE): missing: the password file /run/secrets/db is empty",
		},
		{
			name:  "an empty secret key file",
			files: map[string]string{"/run/secrets/s3": "  "},
			want:  "config: uses.storage.secret_access_key_file (TM_STORE_SECRET_KEY_FILE): missing: the secret key file /run/secrets/s3 is empty",
		},
		{
			name: "an unreadable password file",
			env:  map[string]string{"TM_DB_PASSWORD_FILE": "/run/secrets/gone"},
			want: "config: uses.postgresql.password_file (TM_DB_PASSWORD_FILE): invalid: the password file /run/secrets/gone cannot be read",
		},
		{
			name: "no secret key file named",
			env:  map[string]string{"TM_STORE_SECRET_KEY_FILE": ""},
			want: "config: uses.storage.secret_access_key_file (TM_STORE_SECRET_KEY_FILE): missing: a value is required; set TM_STORE_SECRET_KEY_FILE to the secret key file",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			res := loadData(t, dataEnv(tt.env), dataFiles(tt.files))

			if got := lines(res.Problems); len(got) != 1 || got[0] != tt.want {
				t.Fatalf("problems = %q, want %q", got, tt.want)
			}
			if len(res.Bindings) != 1 {
				t.Errorf("bindings = %d, want only the good one", len(res.Bindings))
			}
			if out := strings.Join(lines(res.Problems), "\n"); strings.Contains(out, dbPassword) || strings.Contains(out, s3Secret) {
				t.Errorf("a problem shows a credential: %s", out)
			}
		})
	}
}

// TestSecretZeroValue covers a Secret never set: it reveals nothing and still prints redacted.
func TestSecretZeroValue(t *testing.T) {
	t.Parallel()
	var s load.Secret

	if got := s.Reveal(); got != "" {
		t.Errorf("Reveal() = %q, want empty", got)
	}
	if got := fmt.Sprintf("%v %+v %#v", s, s, s); got != "[redacted] [redacted] [redacted]" {
		t.Errorf("fmt = %q", got)
	}
}

func lines(ps []load.Problem) []string {
	out := make([]string, 0, len(ps))
	for _, p := range ps {
		out = append(out, p.String())
	}
	return out
}
