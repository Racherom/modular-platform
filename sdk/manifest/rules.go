package manifest

import (
	"fmt"
	"net/url"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Catalog rule codes for the fields this package reads (spec 0005, "Catalog rules for the new
// fields"); libs/contract reports them like its own.
const (
	CodeMissingField      = "missing_field"
	CodeInvalidName       = "invalid_name"
	CodeDuplicateName     = "duplicate_name"
	CodeInvalidEnvName    = "invalid_env_name"
	CodeDuplicateEnv      = "duplicate_env"
	CodeInvalidRuntime    = "invalid_runtime"
	CodeInvalidType       = "invalid_type"
	CodeInvalidDefault    = "invalid_default"
	CodeRuleNotAllowed    = "rule_not_allowed"
	CodeInvalidRule       = "invalid_rule"
	CodeClientNotAllowed  = "client_not_allowed"
	CodeUnknownBindingKey = "unknown_binding_key"
	CodePortNotAllowed    = "port_not_allowed"
	CodeTooManyProvides   = "too_many_provides"
	// CodeUnknownCapability names a capability no capability file declares (spec 0013).
	CodeUnknownCapability = "unknown_capability"
	// CodeUnknownField names a field its capability does not have (spec 0013).
	CodeUnknownField = "unknown_field"
	// CodePerCallerNotAllowed is per_caller on an entry that is not runtime: sdk (spec 0012).
	CodePerCallerNotAllowed = "per_caller_not_allowed"
)

// Value rule codes, shared by a default in the catalog and a value at start.
const (
	CodeInvalid    = "invalid"
	CodeOutOfRange = "out_of_range"
	CodeNoMatch    = "no_match"
	CodeNotAllowed = "not_allowed"
)

// Problem is one broken rule at a dotted path inside the entry.
type Problem struct {
	Path    string
	Code    string
	Message string
}

// RuleError is a value that breaks its type or a rule.
type RuleError struct {
	Code    string // CodeInvalid, CodeOutOfRange, CodeNoMatch or CodeNotAllowed
	Message string
}

func (e *RuleError) Error() string { return e.Code + ": " + e.Message }

// Rules are the checks of one value: its type and the rules that type takes.
type Rules struct {
	Type     string
	Min, Max string
	Pattern  string
	Values   []string
}

// Parse turns text into the value's Go form (string, int, bool, time.Duration, url.URL, or the
// text itself for enum and secret) and checks the rules. A failure is a *RuleError.
func (r Rules) Parse(text string) (any, error) {
	switch r.Type {
	case TypeString, TypeSecret:
		if r.Pattern != "" && !fullMatch(r.Pattern).MatchString(text) {
			return nil, &RuleError{CodeNoMatch, "does not match the pattern " + r.Pattern}
		}
		return text, nil
	case TypeInt:
		n, err := strconv.ParseInt(text, 10, 64)
		if err != nil {
			return nil, &RuleError{CodeInvalid, fmt.Sprintf("%q is not a whole number", text)}
		}
		if r.Min != "" {
			if lo, _ := strconv.ParseInt(r.Min, 10, 64); n < lo {
				return nil, &RuleError{CodeOutOfRange, fmt.Sprintf("%d is below the minimum %s", n, r.Min)}
			}
		}
		if r.Max != "" {
			if hi, _ := strconv.ParseInt(r.Max, 10, 64); n > hi {
				return nil, &RuleError{CodeOutOfRange, fmt.Sprintf("%d is above the maximum %s", n, r.Max)}
			}
		}
		return int(n), nil
	case TypeBool:
		switch text {
		case "true":
			return true, nil
		case "false":
			return false, nil
		}
		return nil, &RuleError{CodeInvalid, fmt.Sprintf("%q is not true or false", text)}
	case TypeDuration:
		d, err := time.ParseDuration(text)
		if err != nil {
			return nil, &RuleError{CodeInvalid, fmt.Sprintf("%q is not a duration like 10s", text)}
		}
		if r.Min != "" {
			if lo, _ := time.ParseDuration(r.Min); d < lo {
				return nil, &RuleError{CodeOutOfRange, fmt.Sprintf("%s is below the minimum %s", text, r.Min)}
			}
		}
		if r.Max != "" {
			if hi, _ := time.ParseDuration(r.Max); d > hi {
				return nil, &RuleError{CodeOutOfRange, fmt.Sprintf("%s is above the maximum %s", text, r.Max)}
			}
		}
		return d, nil
	case TypeURL:
		u, err := url.Parse(text)
		if err != nil || u.Scheme == "" || u.Host == "" {
			// the text may hold credentials, so it is not repeated
			return nil, &RuleError{CodeInvalid, "is not an absolute URL with a scheme and a host"}
		}
		return *u, nil
	case TypeEnum:
		if !slices.Contains(r.Values, text) {
			return nil, &RuleError{CodeNotAllowed, fmt.Sprintf("%q is not one of %s", text, strings.Join(r.Values, ", "))}
		}
		return text, nil
	}
	return nil, &RuleError{CodeInvalid, fmt.Sprintf("unknown type %q", r.Type)}
}

var patterns sync.Map // pattern text to its compiled whole value form

// fullMatch compiles pattern to match a whole value. The catalog check has already refused a
// pattern that does not compile.
func fullMatch(pattern string) *regexp.Regexp {
	if re, ok := patterns.Load(pattern); ok {
		return re.(*regexp.Regexp)
	}
	re, err := regexp.Compile(`^(?:` + pattern + `)$`)
	if err != nil {
		re = regexp.MustCompile(`$.^`) // matches nothing
	}
	patterns.Store(pattern, re)
	return re
}

// CheckValue checks text as a value of v by the rules the SDK loads it with (spec 0025), so a
// value the planner accepts from a graph is one the module accepts at start. A failure is a
// *RuleError.
func CheckValue(v Value, text string) error {
	_, err := RulesOf(v).Parse(text)
	return err
}

// QuotedText reports whether s is two characters or longer and starts and ends with the same
// ASCII quote, " or '. sdk/envfile strips such a pair from a line, so such a text cannot pass
// through an env file unchanged (spec 0025).
func QuotedText(s string) bool {
	return len(s) >= 2 && (s[0] == '"' || s[0] == '\'') && s[len(s)-1] == s[0]
}

// RulesOf returns the rules of a declared value.
func RulesOf(v Value) Rules {
	r := Rules{Type: v.Type, Values: v.Values}
	if v.Min != nil {
		r.Min = v.Min.Text
	}
	if v.Max != nil {
		r.Max = v.Max.Text
	}
	if v.Pattern != nil {
		r.Pattern = *v.Pattern
	}
	return r
}

var valueTypes = []string{TypeString, TypeInt, TypeBool, TypeDuration, TypeURL, TypeEnum, TypeSecret}

// Check checks the rules of every field this package reads against the capabilities the entry
// names, and returns each broken one. The fields it does not read (name, image, healthcheck,
// versions, field sources, mounts, secret names, provide ports of a non sdk entry) are the
// catalog check's.
func Check(e Entry, caps map[string]Capability) []Problem {
	c := checker{env: map[string]string{}, caps: caps}
	sdk := e.Runtime == RuntimeSDK
	if e.Runtime != "" && !sdk {
		c.add("runtime", CodeInvalidRuntime, "runtime %q is not %s", e.Runtime, RuntimeSDK)
	}

	for _, name := range sortedKeys(e.Provides) {
		c.provide("provides."+name, e.Provides[name], sdk)
	}
	if sdk && len(e.Provides) > 1 {
		c.add("provides", CodeTooManyProvides, "a runtime: sdk entry has at most one provide")
	}

	for _, slot := range sortedKeys(e.Uses) {
		c.use("uses."+slot, e.Uses[slot], sdk)
	}

	secretNames := map[string]bool{}
	for i, s := range e.Secrets {
		secretNames[strings.TrimSuffix(s.Name, "/")] = true
		if s.Env != "" {
			c.envName(fmt.Sprintf("secrets.%d.env", i), s.Env)
		}
	}
	for _, name := range sortedKeys(e.Environment) {
		c.envName("environment."+name, name)
	}

	for _, name := range sortedKeys(e.Config) {
		path := "config." + name
		switch {
		case !ValidName(name):
			c.add(path, CodeInvalidName, "%q must be a lowercase DNS label and not a YAML reserved word", name)
		case secretNames[name]:
			c.add(path, CodeDuplicateName, "the name %q is already a secret of this entry", name)
		}
		c.value(path, e.Config[name])
	}
	return c.problems
}

// capability returns the capability a provide or slot names, or reports unknown_capability.
func (c *checker) capability(path, name string) (Capability, bool) {
	if name == "" {
		c.add(path, CodeMissingField, "capability is required")
		return Capability{}, false
	}
	capability, ok := c.caps[name]
	if !ok {
		c.add(path, CodeUnknownCapability, "capability %q is not in the catalog", name)
	}
	return capability, ok
}

func (c *checker) provide(path string, p Provide, sdk bool) {
	if sdk && p.Port != nil {
		c.add(path+".port", CodePortNotAllowed, "a runtime: sdk entry listens on 8080 or 8443 and must not set a port")
	}
	capability, known := c.capability(path+".capability", p.Capability)
	for _, f := range sortedKeys(p.Env) {
		fp := path + ".env." + f
		if known {
			field, ok := capability.Fields[f]
			switch {
			case !ok:
				c.add(fp, CodeUnknownField, "capability %s has no field %s", p.Capability, f)
				continue
			case !field.ToProvider():
				c.add(fp, CodeInvalidRule, "%s reaches the consumer, not the provider; only to_provider and both fields take a provider env", f)
				continue
			}
		}
		if sdk {
			c.sdkEnv(fp, p.Env[f])
		} else {
			c.envName(fp, p.Env[f])
		}
	}
	switch p.Auth {
	case "":
	case AuthBindingToken:
		if p.Env["token"] == "" {
			c.add(path+".env.token", CodeMissingField, "auth: %s needs env.token, the clients directory of the tokens", AuthBindingToken)
		}
	default:
		c.add(path+".auth", CodeInvalidRule, "auth %q is not %s", p.Auth, AuthBindingToken)
	}
}

type checker struct {
	problems []Problem
	env      map[string]string     // env var name to the path that declared it first
	caps     map[string]Capability // the capabilities the entry may name
}

func (c *checker) add(path, code, format string, args ...any) {
	c.problems = append(c.problems, Problem{Path: path, Code: code, Message: fmt.Sprintf(format, args...)})
}

// envName checks a binding or secret env name (spec 0002) and claims it.
func (c *checker) envName(path, name string) {
	switch {
	case name == "":
		c.add(path, CodeMissingField, "an env var name is required")
	case !ValidEnvName(name):
		c.add(path, CodeInvalidEnvName, "%q is not a valid env var name", name)
	default:
		c.claim(path, name)
	}
}

// sdkEnv checks an env name the sdk reads itself: upper case, no _FILE suffix, not a bootstrap
// name.
func (c *checker) sdkEnv(path, name string) bool {
	switch {
	case name == "":
		c.add(path, CodeMissingField, "an env var name is required")
	case !ValidKey(name):
		c.add(path, CodeInvalidEnvName, "%q must match ^[A-Z][A-Z0-9_]*$", name)
	case strings.HasSuffix(name, "_FILE"):
		c.add(path, CodeInvalidEnvName, "%q must not end in _FILE; the sdk adds that form itself", name)
	case IsBootstrap(name):
		c.add(path, CodeInvalidEnvName, "%q is a bootstrap name the sdk reserves", name)
	default:
		return c.claim(path, name)
	}
	return false
}

func (c *checker) claim(path, name string) bool {
	if first, ok := c.env[name]; ok {
		c.add(path, CodeDuplicateEnv, "env var %s is already used by %s", name, first)
		return false
	}
	c.env[name] = path
	return true
}

func (c *checker) use(path string, u Use, sdk bool) {
	switch u.Client {
	case "", ClientHTTP, ClientPostgreSQL, ClientS3:
	default:
		c.add(path+".client", CodeInvalidRule, "client %q is not one of %s, %s, %s", u.Client, ClientHTTP, ClientPostgreSQL, ClientS3)
		return
	}
	if u.Client != "" && !sdk {
		c.add(path+".client", CodeClientNotAllowed, "client needs runtime: %s", RuntimeSDK)
	}
	if u.PerCaller && !sdk {
		c.add(path+".per_caller", CodePerCallerNotAllowed, "per_caller needs runtime: %s", RuntimeSDK)
	}
	capability, known := c.capability(path+".capability", u.Capability)
	for _, f := range sortedKeys(u.Env) {
		fp := path + ".env." + f
		if known {
			field, ok := capability.Fields[f]
			switch {
			case !ok:
				c.add(fp, CodeUnknownField, "capability %s has no field %s", u.Capability, f)
				continue
			case !field.ToConsumer():
				c.add(fp, CodeInvalidRule, "%s is a to_provider field; the consumer fills it and does not receive it", f)
				continue
			}
		}
		c.envName(fp, u.Env[f])
	}
	if !known || u.Client == "" || u.Client != ClientHTTP && u.Client != ClientPostgreSQL && u.Client != ClientS3 {
		return
	}
	required := slices.Clone(clientFields[u.Client])
	if _, ok := capability.Fields["token"]; ok && u.Client == ClientHTTP {
		required = append(required, "token")
	}
	for _, f := range required {
		switch _, ok := capability.Fields[f]; {
		case !ok:
			c.add(path+".client", CodeInvalidRule, "a %s client needs field %s, which capability %s lacks", u.Client, f, u.Capability)
		case u.Env[f] == "":
			c.add(path+".env."+f, CodeMissingField, "a %s client must map %s", u.Client, f)
		}
	}
}

func (c *checker) value(path string, v Value) {
	if c.sdkEnv(path+".env", v.Env) {
		c.claim(path+".env", v.Env+"_FILE")
	}
	switch {
	case strings.TrimSpace(v.Description) == "":
		c.add(path+".description", CodeMissingField, "description is required")
	case strings.ContainsAny(v.Description, "\r\n"):
		c.add(path+".description", CodeInvalidRule, "description must be one line")
	case strings.HasPrefix(strings.TrimSpace(v.Description), "#"):
		c.add(path+".description", CodeInvalidRule, "description must not start with #")
	}
	switch {
	case v.Type == "":
		c.add(path+".type", CodeMissingField, "type is required")
		return
	case !slices.Contains(valueTypes, v.Type):
		c.add(path+".type", CodeInvalidType, "type %q is not one of %s", v.Type, strings.Join(valueTypes, ", "))
		return
	}

	ruleOK := true
	notAllowed := func(field string, set bool, types ...string) {
		if set && !slices.Contains(types, v.Type) {
			c.add(path+"."+field, CodeRuleNotAllowed, "%s does not apply to a %s value", field, v.Type)
			ruleOK = false
		}
	}
	notAllowed("min", v.Min != nil, TypeInt, TypeDuration)
	notAllowed("max", v.Max != nil, TypeInt, TypeDuration)
	notAllowed("pattern", v.Pattern != nil, TypeString)
	notAllowed("values", v.Values != nil, TypeEnum)
	notAllowed("generate", v.Generate, TypeSecret)
	if v.Default != nil && v.Type == TypeSecret {
		c.add(path+".default", CodeRuleNotAllowed, "a secret takes no default")
		ruleOK = false
	}
	if !ruleOK {
		return
	}

	bound := func(field string, b *Bound) bool {
		if b == nil {
			return true
		}
		var err error
		if v.Type == TypeInt {
			_, err = strconv.ParseInt(b.Text, 10, 64)
		} else {
			_, err = time.ParseDuration(b.Text)
		}
		if err != nil {
			c.add(path+"."+field, CodeInvalidRule, "%s %q is not a %s", field, b.Text, v.Type)
			return false
		}
		return true
	}
	minOK, maxOK := bound("min", v.Min), bound("max", v.Max)
	ruleOK = minOK && maxOK
	if ruleOK && v.Min != nil && v.Max != nil {
		if _, err := (Rules{Type: v.Type, Min: v.Min.Text}).Parse(v.Max.Text); err != nil {
			c.add(path+".max", CodeInvalidRule, "max %s is below min %s", v.Max.Text, v.Min.Text)
			ruleOK = false
		}
	}
	if v.Pattern != nil {
		if _, err := regexp.Compile(`^(?:` + *v.Pattern + `)$`); err != nil {
			c.add(path+".pattern", CodeInvalidRule, "pattern does not compile: %v", err)
			ruleOK = false
		}
	}
	if v.Type == TypeEnum {
		ruleOK = c.enumValues(path, v.Values) && ruleOK
	}

	if v.Default != nil && ruleOK && !InstallToken(*v.Default) {
		if *v.Default == "" {
			c.add(path+".default", CodeInvalidDefault, "an empty default is no default; leave it out")
		} else if _, err := RulesOf(v).Parse(*v.Default); err != nil {
			c.add(path+".default", CodeInvalidDefault, "default %s", err.(*RuleError).Message) //nolint:errorlint // Parse returns only an unwrapped *RuleError
		}
	}
}

// InstallToken reports whether a default is one the render fills at install time (spec 0013):
// exactly {uid} or {gid} (the setup user), or a path starting with {install}/ (the platform's
// install folder). Its own rules apply to the value it becomes, not to the token.
func InstallToken(s string) bool {
	return s == "{uid}" || s == "{gid}" || strings.HasPrefix(s, "{install}/")
}

func (c *checker) enumValues(path string, values []string) bool {
	if len(values) == 0 {
		c.add(path+".values", CodeMissingField, "an enum needs at least one value")
		return false
	}
	seen := map[string]bool{}
	for i, s := range values {
		switch {
		case s == "":
			c.add(fmt.Sprintf("%s.values.%d", path, i), CodeInvalidRule, "an enum value must not be empty")
			return false
		case seen[s]:
			c.add(fmt.Sprintf("%s.values.%d", path, i), CodeInvalidRule, "%q is listed twice", s)
			return false
		case QuotedText(s):
			// a graph can never set such a text (spec 0025, "Quoted enum values")
			c.add(fmt.Sprintf("%s.values.%d", path, i), CodeInvalidRule, "an enum value must not start and end with the same quote")
			return false
		}
		seen[s] = true
	}
	return true
}

func sortedKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	return keys
}
