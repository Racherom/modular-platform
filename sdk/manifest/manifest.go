// Package manifest reads the module entry fields the sdk uses (spec 0005 and spec 0013: runtime,
// config, the slots and their env delivery, the provides with auth and their clients env) plus
// the fields of the capabilities they name, and checks their rules. The planner's catalog check
// (libs/contract) and sdkgen both call it, so a module and the catalog never disagree about an
// entry. It imports only the standard library and sigs.k8s.io/yaml.
package manifest

import (
	"encoding/json"
	"fmt"
	"regexp"
	"slices"
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
	"MP_DOMAIN",
	"MP_ENV_FILE",
	"MP_ACME_URL",
	"MP_CA_FILE",
	"MP_CALLER_KEY_FILE",
	"MP_CALLER_SIGNING_KEY_FILE",
}

// clientFields lists the capability fields a slot with that client kind must map in env, so the
// sdk can build the client (spec 0013). An http client also needs token when its capability has
// one.
var clientFields = map[string][]string{
	ClientHTTP:       {"url"},
	ClientPostgreSQL: {"url", "database", "user", "password"},
	ClientS3:         {"url", "bucket", "access_key_id", "secret_access_key"},
}

// Field types of a capability that travel as files.
const (
	FieldSecret      = "secret"
	FieldCertificate = "certificate"
)

// Field directions of a capability (spec 0012).
const (
	DirectionToConsumer = "to_consumer"
	DirectionToProvider = "to_provider"
	DirectionBoth       = "both"
)

// Capability holds the parts of a capability file the sdk reads: its fields. libs/contract checks
// the file's own rules.
type Capability struct {
	Name   string           `json:"name"`
	Fields map[string]Field `json:"fields"`
}

// Field is one field of a capability: its type, direction and rules, which sdkgen writes into a
// slot's field getter (spec 0025).
type Field struct {
	Type        string   `json:"type"`
	Direction   string   `json:"direction,omitempty"`
	Optional    bool     `json:"optional,omitempty"`
	Description string   `json:"description,omitempty"`
	Pattern     string   `json:"pattern,omitempty"`
	Values      []string `json:"values,omitempty"`
}

// Field types of a capability that travel as text (spec 0012).
const (
	FieldString = "string"
	FieldInt    = "int"
	FieldBool   = "bool"
	FieldURL    = "url"
	FieldEnum   = "enum"
)

// IsFile reports whether the field travels as a file: a secret or a certificate.
func (f Field) IsFile() bool {
	return f.Type == FieldSecret || f.Type == FieldCertificate
}

// ToConsumer reports whether the field reaches the consumer: direction to_consumer or both.
func (f Field) ToConsumer() bool {
	return f.Direction == "" || f.Direction == DirectionToConsumer || f.Direction == DirectionBoth
}

// ToProvider reports whether the field reaches the provider: direction to_provider or both.
func (f Field) ToProvider() bool {
	return f.Direction == DirectionToProvider || f.Direction == DirectionBoth
}

// ParseCapability reads the fields of a capability file. It is lenient: the strict decode and the
// file's rules are libs/contract's.
func ParseCapability(data []byte) (Capability, error) {
	var c Capability
	if err := yaml.Unmarshal(data, &c); err != nil {
		return Capability{}, err
	}
	return c, nil
}

// RuntimeKey is the key the sdk's binding loader reads a capability field under: the field name,
// with _file added for a field that travels as a file (password becomes password_file).
func RuntimeKey(name string, f Field) string {
	if f.IsFile() {
		return name + "_file"
	}
	return name
}

// Entry holds the fields of a v1 module entry the sdk reads. The other fields are kept raw, so a
// strict decode accepts a whole entry and still refuses an unknown key.
type Entry struct {
	APIVersion    json.RawMessage    `json:"apiVersion,omitempty"`
	Kind          json.RawMessage    `json:"kind,omitempty"`
	FormatVersion json.RawMessage    `json:"formatVersion,omitempty"`
	Name          string             `json:"name"`
	Description   string             `json:"description"`
	Image         json.RawMessage    `json:"image,omitempty"`
	Runtime       string             `json:"runtime,omitempty"`
	Command       json.RawMessage    `json:"command,omitempty"`
	Entrypoint    json.RawMessage    `json:"entrypoint,omitempty"`
	Environment   map[string]string  `json:"environment,omitempty"`
	Healthcheck   json.RawMessage    `json:"healthcheck,omitempty"`
	Provides      map[string]Provide `json:"provides,omitempty"`
	Uses          map[string]Use     `json:"uses,omitempty"`
	Secrets       []Secret           `json:"secrets,omitempty"`
	Config        map[string]Value   `json:"config,omitempty"`
	Setup         json.RawMessage    `json:"setup,omitempty"`
	Templates     json.RawMessage    `json:"templates,omitempty"`
	Volumes       json.RawMessage    `json:"volumes,omitempty"`
	Publish       json.RawMessage    `json:"publish,omitempty"`
	HostMounts    json.RawMessage    `json:"host_mounts,omitempty"`
	Network       json.RawMessage    `json:"network,omitempty"`
	Hardening     json.RawMessage    `json:"hardening,omitempty"`
}

// Provide is one named provide of the entry.
type Provide struct {
	Capability string            `json:"capability"`
	Version    json.RawMessage   `json:"version,omitempty"`
	Port       *int              `json:"port,omitempty"`
	Auth       string            `json:"auth,omitempty"`
	Fields     json.RawMessage   `json:"fields,omitempty"`
	Env        map[string]string `json:"env,omitempty"`
}

// Use is one slot of the entry: a named need of a capability.
type Use struct {
	Capability string            `json:"capability"`
	Versions   json.RawMessage   `json:"versions,omitempty"`
	Optional   bool              `json:"optional,omitempty"`
	PerCaller  bool              `json:"per_caller,omitempty"`
	Client     string            `json:"client,omitempty"`
	Env        map[string]string `json:"env,omitempty"`
	Files      map[string]string `json:"files,omitempty"`
	Fields     json.RawMessage   `json:"fields,omitempty"`
}

// Secret is one secret file of the entry (spec 0013).
type Secret struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	From        string `json:"from,omitempty"`
	Env         string `json:"env,omitempty"`
	Mount       string `json:"mount,omitempty"`
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
	Setup       string   `json:"setup,omitempty"`
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

// Parse decodes a v1 module entry strictly: an unknown or duplicate key is an error.
func Parse(data []byte) (Entry, error) {
	var e Entry
	if err := yaml.UnmarshalStrict(data, &e); err != nil {
		return Entry{}, err
	}
	return e, nil
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

// ValidDomain reports whether s is one or more DNS labels joined by dots, like the
// <graph>.internal the render writes into MP_DOMAIN.
func ValidDomain(s string) bool {
	for label := range strings.SplitSeq(s, ".") {
		if !nameRe.MatchString(label) {
			return false
		}
	}
	return true
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
	return slices.Contains(BootstrapNames, s)
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
