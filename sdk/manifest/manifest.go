// Package manifest reads the catalog entry fields the sdk uses (spec 0005: runtime, config,
// uses.<cap>.client and its env keys, provides.<cap>.auth) and checks their rules. The planner's
// catalog check (libs/contract) and sdkgen both call it, so a module and the catalog never
// disagree about an entry. It imports only the standard library and sigs.k8s.io/yaml.
package manifest

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"

	"sigs.k8s.io/yaml"
)

// RuntimeSDK marks an entry whose image is built on the sdk.
const RuntimeSDK = "sdk"

// Value types a config value may declare.
const (
	TypeString   = "string"
	TypeInt      = "int"
	TypeBool     = "bool"
	TypeDuration = "duration"
	TypeURL      = "url"
	TypeEnum     = "enum"
	TypeSecret   = "secret"
)

// Client kinds a binding may ask the sdk to build.
const (
	ClientHTTP       = "http"
	ClientPostgreSQL = "postgresql"
	ClientS3         = "s3"
)

// AuthBindingToken marks a provide that checks one token per consumer.
const AuthBindingToken = "binding-token"

// BootstrapNames are read from the process env only, never declared, and filled by the render or
// a hand written Compose file.
var BootstrapNames = []string{
	"MP_INSTANCE",
	"MP_ENV_FILE",
	"MP_ACME_URL",
	"MP_CA_FILE",
	"MP_CALLER_KEY_FILE",
	"MP_CALLER_SIGNING_KEY_FILE",
}

// BindingKeys lists the env keys a binding may map, per client kind, in the order the render
// writes them. The key "" holds the keys every kind takes, including a binding without a client.
var BindingKeys = map[string][]string{
	"":               {"host", "port", "url", "token_file", "ca_file"},
	ClientPostgreSQL: {"database", "user", "password_file", "sslmode"},
	ClientS3:         {"bucket", "region", "access_key_id", "secret_access_key_file"},
}

// requiredBindingKeys must be mapped by a binding with that client kind.
var requiredBindingKeys = map[string][]string{
	ClientHTTP:       {"url"},
	ClientPostgreSQL: {"url", "database", "user", "password_file"},
	ClientS3:         {"url", "bucket", "access_key_id", "secret_access_key_file"},
}

// Entry holds the fields of a catalog entry the sdk reads. The other fields are kept raw, so a
// strict decode accepts a whole entry and still refuses an unknown key.
type Entry struct {
	APIVersion  json.RawMessage    `json:"apiVersion,omitempty"`
	Kind        json.RawMessage    `json:"kind,omitempty"`
	Name        string             `json:"name"`
	Description string             `json:"description"`
	Image       json.RawMessage    `json:"image,omitempty"`
	Runtime     string             `json:"runtime,omitempty"`
	Healthcheck json.RawMessage    `json:"healthcheck,omitempty"`
	Provides    map[string]Provide `json:"provides,omitempty"`
	Uses        map[string]Use     `json:"uses,omitempty"`
	Secrets     []Secret           `json:"secrets,omitempty"`
	Config      map[string]Value   `json:"config,omitempty"`
}

// Provide is one capability the entry provides.
type Provide struct {
	Port *int        `json:"port,omitempty"`
	Auth string      `json:"auth,omitempty"`
	Env  *ProvideEnv `json:"env,omitempty"`
}

// ProvideEnv names the env vars a provider reads for a capability it provides.
type ProvideEnv struct {
	ClientsDir string `json:"clients_dir,omitempty"`
}

// Use is one capability the entry uses.
type Use struct {
	Optional bool              `json:"optional,omitempty"`
	Client   string            `json:"client,omitempty"`
	Env      map[string]string `json:"env,omitempty"`
}

// Secret is one generated secret of the entry (spec 0002).
type Secret struct {
	Name string `json:"name"`
	Env  string `json:"env"`
}

// Value is one declared config value.
type Value struct {
	Env         string   `json:"env"`
	Type        string   `json:"type"`
	Description string   `json:"description"`
	Default     *string  `json:"default,omitempty"`
	Optional    bool     `json:"optional,omitempty"`
	Min         *Bound   `json:"min,omitempty"`
	Max         *Bound   `json:"max,omitempty"`
	Pattern     *string  `json:"pattern,omitempty"`
	Values      []string `json:"values,omitempty"`
	Generate    bool     `json:"generate,omitempty"`
}

// Bound is a min or max rule as text: a whole number for an int, a duration like 10s for a
// duration. YAML may give it as a number or a string.
type Bound struct {
	Text string
}

// UnmarshalJSON takes a JSON number or string.
func (b *Bound) UnmarshalJSON(data []byte) error {
	var n json.Number
	if err := json.Unmarshal(data, &n); err == nil {
		b.Text = n.String()
		return nil
	}
	var s string
	if err := json.Unmarshal(data, &s); err != nil {
		return fmt.Errorf("a bound must be a number or a string: %w", err)
	}
	b.Text = s
	return nil
}

// MarshalJSON writes the bound back as a string.
func (b Bound) MarshalJSON() ([]byte, error) {
	return json.Marshal(b.Text)
}

// Parse decodes a catalog entry strictly: an unknown or duplicate key is an error, and so is a
// binding env key that no client kind takes (a key of another kind is a Check problem).
func Parse(data []byte) (Entry, error) {
	var e Entry
	if err := yaml.UnmarshalStrict(data, &e); err != nil {
		return Entry{}, err
	}
	for _, capability := range sortedKeys(e.Uses) {
		for _, key := range sortedKeys(e.Uses[capability].Env) {
			if !knownBindingKey(key) {
				return Entry{}, fmt.Errorf("uses.%s.env: unknown field %q", capability, key)
			}
		}
	}
	return e, nil
}

func knownBindingKey(key string) bool {
	for _, keys := range BindingKeys {
		for _, k := range keys {
			if k == key {
				return true
			}
		}
	}
	return false
}

var (
	nameRe       = regexp.MustCompile(`^[a-z]([a-z0-9-]{0,61}[a-z0-9])?$`)
	envNameRe    = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)
	sdkEnvNameRe = regexp.MustCompile(`^[A-Z][A-Z0-9_]*$`)

	// YAML 1.1 readers turn these into booleans or null, so no name may be one of them.
	reservedNames = map[string]bool{
		"y": true, "n": true, "yes": true, "no": true, "on": true, "off": true,
		"true": true, "false": true, "null": true,
	}
)

// ValidName reports whether s is a DNS label and not a YAML reserved word. It applies to graph
// names, instance names, catalog entry names, capability names, secret names and value names.
func ValidName(s string) bool {
	return nameRe.MatchString(s) && !reservedNames[s]
}

// ValidEnvName reports whether s may name an environment variable in a catalog entry.
func ValidEnvName(s string) bool {
	return envNameRe.MatchString(s)
}

// ValidKey reports whether s may be a key of an env file and the env of a config value.
func ValidKey(s string) bool {
	return sdkEnvNameRe.MatchString(s)
}

// IsBootstrap reports whether s is one of the BootstrapNames.
func IsBootstrap(s string) bool {
	for _, b := range BootstrapNames {
		if s == b {
			return true
		}
	}
	return false
}

// GoName turns a name like planner-rate into PlannerRate: every run of letters and digits starts
// upper case.
func GoName(s string) string {
	var b strings.Builder
	upper := true
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
			if upper {
				b.WriteString(strings.ToUpper(string(r)))
			} else {
				b.WriteRune(r)
			}
			upper = false
		default:
			upper = true
		}
	}
	return b.String()
}
