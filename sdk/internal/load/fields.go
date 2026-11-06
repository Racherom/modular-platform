package load

import (
	"crypto/x509"
	"errors"
	"fmt"
	"maps"
	"net/url"
	"slices"
	"strings"

	"github.com/racherom/modular-platform/sdk/manifest"
)

// FieldSpec declares one field of a slot with no client (spec 0025): the capability field, the
// env its slot maps, and the type and rules sdkgen copies from the capability file.
type FieldSpec struct {
	Name     string // the capability field name, like client_id
	Env      string // the env its slot maps; for a secret or certificate it names the file
	Type     string // a capability field type: string, int, bool, url, enum, secret or certificate
	Optional bool
	Pattern  string   // string only
	Values   []string // enum only
}

// The Go form of each field type, as Binding.Fields holds it and the generated getter returns
// it: string, int, bool, *url.URL, string for an enum, Secret for a secret (the file's content)
// and []byte for a certificate (the file's PEM).

// fields loads the fields of a slot with no client. An optional slot with every mapped env unset
// or empty is unbound; with any set, its required fields must all be present. Each field reports
// only its first broken rule; a slot with a problem is left out of the result.
func (l *loader) fields(b BindingSpec) {
	path := "uses." + b.Capability
	texts := make(map[string]string, len(b.Fields))
	set := false
	for _, f := range b.Fields {
		texts[f.Name] = l.key(f.Env)
		set = set || texts[f.Name] != ""
	}
	if !set && b.Optional {
		l.res.Bindings = append(l.res.Bindings, Binding{Capability: b.Capability, Optional: true})
		return
	}
	values := make(map[string]any, len(b.Fields))
	failed := false
	for _, f := range b.Fields {
		text := texts[f.Name]
		if text == "" {
			if !f.Optional {
				failed = true
				what := "a value"
				if f.isFile() {
					what = "a file path"
				}
				l.problem(path+"."+f.Name, f.Env, CodeMissing, "%s is required; set %s", what, f.Env)
			}
			continue
		}
		v, err := f.load(text, l.in.ReadFile)
		if err != nil {
			failed = true
			re := ruleError(err)
			l.problem(path+"."+f.Name, f.Env, re.Code, "%s", re.Message)
			continue
		}
		values[f.Name] = v
	}
	if !failed {
		l.res.Bindings = append(l.res.Bindings, Binding{Capability: b.Capability, Optional: b.Optional, Bound: true, Fields: values})
	}
}

func (f FieldSpec) isFile() bool {
	return f.Type == manifest.FieldSecret || f.Type == manifest.FieldCertificate
}

// load turns the text of f's env into its Go form: a text field parsed and checked, a secret or
// certificate read from the file the text names. A failure is a *manifest.RuleError whose message
// never carries a secret or certificate's content.
func (f FieldSpec) load(text string, readFile func(string) ([]byte, error)) (any, error) {
	switch f.Type {
	case manifest.FieldSecret:
		data, err := readFile(text)
		if err != nil {
			return nil, &manifest.RuleError{Code: CodeInvalid, Message: fmt.Sprintf("the file %s cannot be read", text)}
		}
		s := strings.TrimRight(string(data), " \t\r\n")
		if s == "" {
			return nil, &manifest.RuleError{Code: CodeMissing, Message: fmt.Sprintf("the file %s is empty", text)}
		}
		return NewSecret(s), nil
	case manifest.FieldCertificate:
		data, err := readFile(text)
		if err != nil {
			return nil, &manifest.RuleError{Code: CodeInvalid, Message: fmt.Sprintf("the file %s cannot be read", text)}
		}
		if !holdsCertificate(data) {
			return nil, &manifest.RuleError{Code: CodeInvalid, Message: fmt.Sprintf("the file %s holds no PEM certificate", text)}
		}
		return data, nil
	}
	return f.parse(text)
}

// parse parses and checks the text of a text field.
func (f FieldSpec) parse(text string) (any, error) {
	r := manifest.Rules{Type: f.Type, Pattern: f.Pattern, Values: f.Values}
	switch f.Type {
	case manifest.FieldString, manifest.FieldInt, manifest.FieldBool, manifest.FieldEnum:
		return r.Parse(text)
	case manifest.FieldURL:
		v, err := r.Parse(text)
		if err != nil {
			return nil, err
		}
		u := v.(url.URL)
		return &u, nil
	}
	return nil, &manifest.RuleError{Code: CodeInvalid, Message: fmt.Sprintf("unknown field type %q; run sdkgen again", f.Type)}
}

// holdsCertificate applies the MP_CA_FILE rule: the data parses as PEM with at least one
// certificate.
func holdsCertificate(data []byte) bool {
	return x509.NewCertPool().AppendCertsFromPEM(data)
}

func ruleError(err error) *manifest.RuleError {
	if re, ok := errors.AsType[*manifest.RuleError](err); ok {
		return re
	}
	return &manifest.RuleError{Code: CodeInvalid, Message: err.Error()}
}

// CheckFields checks a BindingResolver's answer for a slot with no client by the start rules: every
// required field present, no field the slot does not map, each value of its field's Go form and
// passing its rules. It returns a copy of the fields, or an error that names the field and the
// broken rule, never a value.
func CheckFields(specs []FieldSpec, fields map[string]any) (map[string]any, error) {
	known := make(map[string]FieldSpec, len(specs))
	for _, f := range specs {
		known[f.Name] = f
	}
	for _, name := range slices.Sorted(maps.Keys(fields)) {
		if _, ok := known[name]; !ok {
			return nil, fmt.Errorf("%s: %s: the slot maps no field %s", name, CodeUnknownKey, name)
		}
	}
	out := make(map[string]any, len(fields))
	for _, f := range specs {
		v, ok := fields[f.Name]
		if !ok || isEmpty(v) {
			if !f.Optional {
				return nil, fmt.Errorf("%s: %s: a value is required", f.Name, CodeMissing)
			}
			continue
		}
		if err := f.check(v); err != nil {
			re := ruleError(err)
			return nil, fmt.Errorf("%s: %s: %s", f.Name, re.Code, re.Message)
		}
		out[f.Name] = v
	}
	return out, nil
}

// isEmpty reports a value that counts as unset: an empty string, Secret or certificate, or a nil
// URL.
func isEmpty(v any) bool {
	switch v := v.(type) {
	case nil:
		return true
	case string:
		return v == ""
	case Secret:
		return v.Reveal() == ""
	case []byte:
		return len(v) == 0
	case *url.URL:
		return v == nil
	}
	return false
}

// check checks a value already in its Go form against f's type and rules.
func (f FieldSpec) check(v any) error {
	wrong := func() error {
		return &manifest.RuleError{Code: CodeInvalid, Message: fmt.Sprintf("is a %T, not a %s field", v, f.Type)}
	}
	switch f.Type {
	case manifest.FieldString, manifest.FieldEnum:
		s, ok := v.(string)
		if !ok {
			return wrong()
		}
		_, err := f.parse(s)
		return err
	case manifest.FieldInt:
		if _, ok := v.(int); !ok {
			return wrong()
		}
	case manifest.FieldBool:
		if _, ok := v.(bool); !ok {
			return wrong()
		}
	case manifest.FieldURL:
		u, ok := v.(*url.URL)
		if !ok {
			return wrong()
		}
		if u.Scheme == "" || u.Host == "" {
			return &manifest.RuleError{Code: CodeInvalid, Message: "is not an absolute URL with a scheme and a host"}
		}
	case manifest.FieldSecret:
		if _, ok := v.(Secret); !ok {
			return wrong()
		}
	case manifest.FieldCertificate:
		data, ok := v.([]byte)
		if !ok {
			return wrong()
		}
		if !holdsCertificate(data) {
			return &manifest.RuleError{Code: CodeInvalid, Message: "holds no PEM certificate"}
		}
	default:
		return &manifest.RuleError{Code: CodeInvalid, Message: fmt.Sprintf("unknown field type %q", f.Type)}
	}
	return nil
}
