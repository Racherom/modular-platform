package contract

import (
	"fmt"
	"maps"
	"path"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/distribution/reference"

	"github.com/racherom/modular-platform/sdk/manifest"
)

// ACMESlot is the optional slot every runtime: sdk entry has without declaring it: capability
// acme, delivered as MP_ACME_URL and MP_CA_FILE (spec 0013).
const ACMESlot = "acme"

// ACMESlotSpec is the implicit acme slot of a runtime: sdk entry.
var ACMESlotSpec = Slot{
	Capability: "acme",
	Optional:   true,
	Env:        map[string]string{"url": "MP_ACME_URL", "ca": "MP_CA_FILE"},
}

// Slots returns the entry's slots, with the implicit acme slot of a runtime: sdk entry.
func (m Module) Slots() map[string]Slot {
	out := make(map[string]Slot, len(m.Uses)+1)
	maps.Copy(out, m.Uses)
	if m.Runtime == Sdk {
		if _, declared := out[ACMESlot]; !declared {
			out[ACMESlot] = ACMESlotSpec
		}
	}
	return out
}

// formatFieldRe finds the {field} references of a uses format.
var formatFieldRe = regexp.MustCompile(`\{([a-z][a-z0-9_]*)\}`)

// CheckModule decodes one module entry strictly and checks every rule that concerns it, with the
// capabilities it names (spec 0013). The sdk fields and every env name are checked by
// sdk/manifest, so sdkgen refuses the same entries.
func CheckModule(file string, data []byte, caps map[string]Capability) (Module, []CatalogProblem) {
	c := checker{file: file}
	var m Module
	if !c.decode(data, &m, KindModule, func() string { return m.ApiVersion }, func() string { return m.Kind }) {
		return m, c.problems
	}
	entry, err := manifest.Parse(data)
	if err != nil {
		// the schema and sdk/manifest disagree about a field; manifest_test.go guards against it
		c.add(".", CodeInvalidEntry, "the entry could not be read: %s", oneLine(err))
		return m, c.problems
	}

	mc := moduleChecker{checker: &c, m: m, caps: caps, mounts: map[string]string{}}
	mc.top()
	mc.secrets()
	for _, p := range sortedKeys(m.Provides) {
		mc.provide("provides."+p, p, m.Provides[p])
	}
	for _, s := range sortedKeys(m.Uses) {
		mc.slot("uses."+s, s, m.Uses[s])
	}
	mc.volumes()
	mc.templates()
	mc.setup()
	if m.Hardening != nil {
		for i, t := range m.Hardening.Tmpfs {
			mc.mount(fmt.Sprintf("hardening.tmpfs.%d", i), t)
		}
	}
	if m.Healthcheck != nil {
		c.healthcheck(*m.Healthcheck)
	}

	for _, p := range manifest.Check(entry, ManifestCapabilities(caps)) {
		c.add(p.Path, p.Code, "%s", p.Message)
	}
	return m, c.problems
}

// ManifestCapabilities turns capabilities into the form sdk/manifest reads.
func ManifestCapabilities(caps map[string]Capability) map[string]manifest.Capability {
	out := make(map[string]manifest.Capability, len(caps))
	for name, k := range caps {
		mk := manifest.Capability{Name: name, Fields: make(map[string]manifest.Field, len(k.Fields))}
		for f, field := range k.Fields {
			mk.Fields[f] = manifest.Field{Type: string(field.Type), Direction: string(field.Direction), Optional: field.Optional}
		}
		out[name] = mk
	}
	return out
}

type moduleChecker struct {
	*checker
	m      Module
	caps   map[string]Capability
	mounts map[string]string // container path to the dotted path that mounts it first
}

func (c *moduleChecker) top() {
	m := c.m
	c.name("name", m.Name)
	if m.Name != "" && m.Name != path.Base(path.Dir(c.file)) {
		c.add("name", CodeNameMismatch, "name %q differs from the folder name", m.Name)
	}
	c.description("description", m.Description)
	c.image(m.Image)
}

func (c *moduleChecker) secrets() {
	names := map[string]bool{}
	for i, s := range c.m.Secrets {
		p := fmt.Sprintf("secrets.%d", i)
		folder := strings.HasSuffix(s.Name, "/")
		name := strings.TrimSuffix(s.Name, "/")
		c.name(p+".name", name)
		if names[name] && name != "" {
			c.add(p+".name", CodeDuplicateName, "secret %q is declared twice", name)
		}
		names[name] = true
		c.description(p+".description", s.Description)
		switch s.From {
		case "", SecretFromGenerate, SecretFromAsk, SecretFromSetup:
		default:
			c.add(p+".from", CodeInvalidRule, "from %q is not generate, ask or setup", s.From)
		}
		if s.Env == "" && s.Mount == "" {
			c.add(p, CodeMissingField, "a secret needs env, mount or both")
		}
		if folder && (s.From != SecretFromSetup || s.Mount == "" || s.Env != "") {
			c.add(p, CodeInvalidRule, "a secret folder needs from: setup and a mount, and takes no env")
		}
		if s.Mount != "" {
			c.mount(p+".mount", s.Mount)
		}
	}
}

// secret reports whether the entry declares a secret file (not a folder) named n.
func (c *moduleChecker) secret(n string) bool {
	return slices.ContainsFunc(c.m.Secrets, func(s Secret) bool { return s.Name == n })
}

func (c *moduleChecker) provide(p, name string, pr Provide) {
	c.name(p, name)
	if _, ok := ParseVersion(pr.Version); !ok {
		c.add(p+".version", CodeInvalidVersion, "version %q is not a version like 18.6", pr.Version)
	}
	if c.m.Runtime != Sdk && (pr.Port < 1 || pr.Port > 65535) {
		c.add(p+".port", CodeInvalidPort, "port %d is not between 1 and 65535", pr.Port)
	}
	k, known := c.caps[pr.Capability]
	if !known {
		return // sdk/manifest reports unknown_capability
	}
	for _, f := range sortedKeys(k.Fields) {
		field := k.Fields[f]
		if _, ok := pr.Fields[f]; !ok && field.ToConsumer() && !field.Optional {
			c.add(p+".fields."+f, CodeMissingFieldSource, "field %s of %s needs a source", f, pr.Capability)
		}
	}
	for _, f := range sortedKeys(pr.Fields) {
		fp := p + ".fields." + f
		field, ok := k.Fields[f]
		switch {
		case !ok:
			c.add(fp, CodeUnknownField, "capability %s has no field %s", pr.Capability, f)
		case !field.ToConsumer():
			c.add(fp, CodeInvalidFieldSource, "%s is a to_provider field; the consumer gives its value", f)
		default:
			c.source(fp, k, f, field, pr.Fields[f])
		}
	}
	if pr.Auth == BindingToken {
		token, ok := k.Fields["token"]
		if !ok || token.Direction != Both || !pr.Fields["token"].Generate {
			c.add(p+".auth", CodeInvalidFieldSource, "auth: binding-token needs a field token of direction both with source {generate: true}")
		}
	}
}

// source checks a provider side field source: exactly one kind, the keys that kind takes, and a
// value the field's type allows.
func (c *moduleChecker) source(p string, k Capability, name string, field CapabilityField, s FieldSource) {
	kinds := sourceKinds(s)
	if len(kinds) != 1 {
		c.add(p, CodeInvalidFieldSource, "a source names exactly one of platform, value, config, secret, generate, uses and volume")
		return
	}
	text := !field.IsFile()
	extra := func(allowed ...string) {
		for _, k := range setKeys(s) {
			if !slices.Contains(allowed, k) {
				c.add(p, CodeInvalidFieldSource, "%s does not apply to a %s source", k, kinds[0])
			}
		}
	}
	switch kinds[0] {
	case "platform":
		extra("platform", "path", "on", "off")
		switch s.Platform {
		case FieldSourcePlatformHost, FieldSourcePlatformPort, FieldSourcePlatformConsumer:
			c.want(p, text, "platform: %s gives text, but %s is a %s", s.Platform, name, field.Type)
		case FieldSourcePlatformUrl:
			c.want(p, text, "platform: url gives text, but %s is a %s", name, field.Type)
			c.want(p, k.Scheme != "" || c.m.Runtime == Sdk, "platform: url needs the capability's scheme")
		case FieldSourcePlatformCa:
			c.want(p, field.Type == CapabilityFieldTypeCertificate && field.Optional, "platform: ca fills an optional certificate field")
		case FieldSourcePlatformTls:
			c.want(p, text && s.On != "" && s.Off != "", "platform: tls needs on and off text values")
			c.value(p, name, field, s.On)
			c.value(p, name, field, s.Off)
		default:
			c.add(p, CodeInvalidFieldSource, "platform %q is not host, port, url, ca, tls or consumer", s.Platform)
		}
		if s.Path != "" {
			c.want(p, s.Platform == FieldSourcePlatformUrl && strings.HasPrefix(s.Path, "/"), "path goes with platform: url and starts with /")
		}
		if s.On != "" || s.Off != "" {
			c.want(p, s.Platform == FieldSourcePlatformTls, "on and off go with platform: tls")
		}
	case "value":
		extra("value")
		c.want(p, text, "value gives text, but %s is a %s", name, field.Type)
		c.value(p, name, field, s.Value)
	case "config":
		extra("config", "format")
		c.want(p, text, "config gives text, but %s is a %s", name, field.Type)
		_, ok := c.m.Config[s.Config]
		c.want(p, ok, "config value %q is not declared", s.Config)
		c.want(p, s.Format == "" || strings.Contains(s.Format, "{}"), "a config format holds {}")
	case "secret":
		extra("secret")
		c.want(p, !text, "secret gives a file, but %s is a %s", name, field.Type)
		c.want(p, c.secret(s.Secret), "secret %q is not declared", s.Secret)
	case "generate":
		extra("generate")
		c.want(p, field.Type == CapabilityFieldTypeSecret, "generate makes a secret, but %s is a %s", name, field.Type)
	case "uses":
		extra("uses", "field", "format")
		slot, ok := c.m.Slots()[s.Uses]
		if !c.want(p, ok, "slot %q is not declared", s.Uses) {
			return
		}
		from := c.caps[slot.Capability]
		switch {
		case (s.Field == "") == (s.Format == ""):
			c.add(p, CodeInvalidFieldSource, "a uses source takes exactly one of field and format")
		case s.Field != "":
			f, ok := from.Fields[s.Field]
			c.want(p, ok && f.IsFile() == field.IsFile(), "field %s of slot %s must exist and match %s as file or text", s.Field, s.Uses, name)
		default:
			c.want(p, text, "a format gives text, but %s is a %s", name, field.Type)
			for _, ref := range formatFieldRe.FindAllStringSubmatch(s.Format, -1) {
				f, ok := from.Fields[ref[1]]
				c.want(p, ok && !f.IsFile(), "format names %s, which is not a text field of slot %s", ref[1], s.Uses)
			}
		}
	case "volume":
		extra("volume", "path")
		_, ok := c.m.Volumes[s.Volume]
		c.want(p, ok, "volume %q is not declared", s.Volume)
		c.want(p, !text, "volume gives a file, but %s is a %s", name, field.Type)
		c.want(p, s.Path != "" && !path.IsAbs(s.Path) && !strings.Contains(s.Path, ".."), "path is a relative file inside the volume")
	}
}

// want reports invalid_field_source unless ok, and returns ok.
func (c *moduleChecker) want(p string, ok bool, format string, args ...any) bool {
	if !ok {
		c.add(p, CodeInvalidFieldSource, format, args...)
	}
	return ok
}

// value checks a literal against the field's type and rules.
func (c *moduleChecker) value(p, name string, field CapabilityField, v string) {
	if msg := FieldValueProblem(field, v); msg != "" {
		c.add(p, CodeInvalidFieldSource, "%s: %s", name, msg)
	}
}

// FieldValueProblem describes why v is not a value of field, or is "".
func FieldValueProblem(field CapabilityField, v string) string {
	switch field.Type {
	case CapabilityFieldTypeInt:
		if _, err := strconv.Atoi(v); err != nil {
			return fmt.Sprintf("%q is not a whole number", v)
		}
	case CapabilityFieldTypeBool:
		if v != "true" && v != "false" {
			return fmt.Sprintf("%q is not true or false", v)
		}
	case CapabilityFieldTypeEnum:
		if !slices.Contains(field.Values, v) {
			return fmt.Sprintf("%q is not one of %s", v, strings.Join(field.Values, ", "))
		}
	case CapabilityFieldTypeString:
		if field.Pattern != "" && !regexp.MustCompile(`^(?:`+field.Pattern+`)$`).MatchString(v) {
			return fmt.Sprintf("%q does not match the pattern %s", v, field.Pattern)
		}
	case CapabilityFieldTypeUrl, CapabilityFieldTypeSecret, CapabilityFieldTypeCertificate:
		// a url is checked where it is used; a file field never has a literal value
	}
	return ""
}

func sourceKinds(s FieldSource) []string {
	var kinds []string
	for _, k := range []struct {
		name string
		set  bool
	}{
		{"platform", s.Platform != ""},
		{"value", s.Value != ""},
		{"config", s.Config != ""},
		{"secret", s.Secret != ""},
		{"generate", s.Generate},
		{"uses", s.Uses != ""},
		{"volume", s.Volume != ""},
	} {
		if k.set {
			kinds = append(kinds, k.name)
		}
	}
	return kinds
}

// setKeys lists the keys a source sets, in the schema's order.
func setKeys(s FieldSource) []string {
	var keys []string
	for _, k := range []struct {
		name string
		set  bool
	}{
		{"platform", s.Platform != ""},
		{"path", s.Path != ""},
		{"on", s.On != ""},
		{"off", s.Off != ""},
		{"provide", s.Provide != ""},
		{"value", s.Value != ""},
		{"config", s.Config != ""},
		{"format", s.Format != ""},
		{"secret", s.Secret != ""},
		{"generate", s.Generate},
		{"uses", s.Uses != ""},
		{"field", s.Field != ""},
		{"volume", s.Volume != ""},
	} {
		if k.set {
			keys = append(keys, k.name)
		}
	}
	return keys
}

func (c *moduleChecker) slot(p, name string, s Slot) {
	c.name(p, name)
	if name == ACMESlot && c.m.Runtime == Sdk {
		c.add(p, CodeReservedSlot, "every runtime: sdk entry has the slot acme; it cannot declare its own")
	}
	if _, err := ParseRange(s.Versions); err != nil {
		c.add(p+".versions", CodeInvalidRange, "%s", err)
	}
	k, known := c.caps[s.Capability]
	for _, f := range sortedKeys(s.Files) {
		fp := p + ".files." + f
		if known {
			field, ok := k.Fields[f]
			switch {
			case !ok:
				c.add(fp, CodeUnknownField, "capability %s has no field %s", s.Capability, f)
			case !field.IsFile() || !field.ToConsumer():
				c.add(fp, CodeInvalidFieldSource, "files mounts a secret or certificate the consumer receives; %s is not one", f)
			}
		}
		c.mount(fp, s.Files[f])
	}
	for _, f := range sortedKeys(s.Fields) {
		fp := p + ".fields." + f
		if !known {
			continue
		}
		field, ok := k.Fields[f]
		switch {
		case !ok:
			c.add(fp, CodeUnknownField, "capability %s has no field %s", s.Capability, f)
		case field.Direction != ToProvider:
			c.add(fp, CodeInvalidFieldSource, "a consumer gives only to_provider fields; %s is not one", f)
		default:
			c.consumerSource(fp, f, field, s.Fields[f])
		}
	}
}

// consumerSource checks the source of a to_provider field: one of the consumer's own secrets,
// a literal, or the host, port or url of one of its own provides (spec 0013).
func (c *moduleChecker) consumerSource(p, name string, field CapabilityField, s FieldSource) {
	keys := setKeys(s)
	switch {
	case s.Secret != "" && len(keys) == 1:
		c.want(p, field.IsFile(), "secret gives a file, but %s is a %s", name, field.Type)
		c.want(p, c.secret(s.Secret), "secret %q is not declared", s.Secret)
	case s.Value != "" && len(keys) == 1:
		c.want(p, !field.IsFile(), "value gives text, but %s is a %s", name, field.Type)
		c.value(p, name, field, s.Value)
	case s.Platform != "" && s.Provide != "" && len(keys) == 2:
		c.want(p, !field.IsFile(), "platform gives text, but %s is a %s", name, field.Type)
		c.want(p, slices.Contains([]FieldSourcePlatform{FieldSourcePlatformUrl, FieldSourcePlatformHost, FieldSourcePlatformPort}, s.Platform),
			"a consumer's platform source is url, host or port")
		_, ok := c.m.Provides[s.Provide]
		c.want(p, ok, "provide %q is not declared", s.Provide)
	default:
		c.add(p, CodeInvalidFieldSource, "a consumer source is {secret}, {value} or {platform, provide}")
	}
}

func (c *moduleChecker) volumes() {
	for _, n := range sortedKeys(c.m.Volumes) {
		c.name("volumes."+n, n)
		c.mount("volumes."+n, c.m.Volumes[n])
	}
}

func (c *moduleChecker) templates() {
	for i, t := range c.m.Templates {
		p := fmt.Sprintf("templates.%d", i)
		if !strings.HasSuffix(t.File, ".tmpl") || strings.Contains(t.File, "/") {
			c.add(p+".file", CodeInvalidTemplate, "file %q must be a .tmpl file in the module folder", t.File)
		}
		if len(t.Delims) != 0 && (len(t.Delims) != 2 || t.Delims[0] == "" || t.Delims[1] == "") {
			c.add(p+".delims", CodeInvalidTemplate, "delims are two non empty strings")
		}
		c.mount(p+".path", t.Path)
	}
}

func (c *moduleChecker) setup() {
	names := map[string]bool{}
	for i, s := range c.m.Setup {
		p := fmt.Sprintf("setup.%d", i)
		c.name(p+".name", s.Name)
		if names[s.Name] {
			c.add(p+".name", CodeDuplicateName, "setup step %q is declared twice", s.Name)
		}
		names[s.Name] = true
		if len(s.Command) == 0 {
			c.add(p+".command", CodeInvalidSetupStep, "a setup step needs a command")
		}
		if s.ForEach != "" {
			if _, ok := c.m.Provides[s.ForEach]; !ok {
				c.add(p+".for_each", CodeInvalidSetupStep, "provide %q is not declared", s.ForEach)
			}
		}
		for j, w := range s.Writes {
			if !ValidName(strings.TrimSuffix(w.Name, "/")) {
				c.add(fmt.Sprintf("%s.writes.%d.name", p, j), CodeInvalidSetupStep, "%q is not a secret or folder name", w.Name)
			}
		}
	}
}

// mount checks an absolute container path and claims it, so no two mounts share one path.
func (c *moduleChecker) mount(p, target string) {
	switch {
	case !path.IsAbs(target) || path.Clean(target) != target:
		c.add(p, CodeInvalidMount, "%q must be a clean absolute path", target)
	case c.mounts[target] != "":
		c.add(p, CodeInvalidMount, "%s is already mounted by %s", target, c.mounts[target])
	default:
		c.mounts[target] = p
	}
}

func (c *checker) image(image string) {
	if image == "" {
		c.add("image", CodeMissingField, "image is required")
		return
	}
	ref, err := reference.ParseNormalizedNamed(image)
	if err != nil {
		c.add("image", CodeInvalidImage, "%q is not an image reference: %v", image, err)
		return
	}
	tagged, ok := ref.(reference.Tagged)
	switch {
	case !ok:
		c.add("image", CodeImageUntagged, "%q must carry an exact tag", image)
	case tagged.Tag() == "latest":
		c.add("image", CodeImageLatest, "%q must not use the latest tag", image)
	}
}

func (c *checker) healthcheck(h Healthcheck) {
	if len(h.Test) < 2 || (h.Test[0] != "CMD" && h.Test[0] != "CMD-SHELL") {
		c.add("healthcheck.test", CodeInvalidHealthcheck, "test must be a list starting with CMD or CMD-SHELL and a command")
	}
	durations := []struct{ field, value string }{
		{"interval", h.Interval}, {"start_period", h.StartPeriod}, {"timeout", h.Timeout},
	}
	for _, d := range durations {
		if d.value == "" {
			continue
		}
		if _, err := time.ParseDuration(d.value); err != nil {
			c.add("healthcheck."+d.field, CodeInvalidDuration, "%q is not a duration like 5s", d.value)
		}
	}
	if h.Retries != nil && *h.Retries < 1 {
		c.add("healthcheck.retries", CodeInvalidRetries, "retries must be 1 or more")
	}
}
