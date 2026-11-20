package contract

import (
	"fmt"
	"path"
	"regexp"
	"slices"
	"strings"

	"github.com/distribution/reference"
)

var (
	fieldNameRe = regexp.MustCompile(`^[a-z][a-z0-9_]{0,62}$`)
	schemeRe    = regexp.MustCompile(`^[a-z][a-z0-9+.-]*$`)
)

var fieldTypes = []CapabilityFieldType{
	CapabilityFieldTypeString, CapabilityFieldTypeInt, CapabilityFieldTypeBool, CapabilityFieldTypeUrl,
	CapabilityFieldTypeEnum, CapabilityFieldTypeSecret, CapabilityFieldTypeCertificate,
}

// IsFile reports whether the field travels as a file: a secret or a certificate (spec 0012).
func (f CapabilityField) IsFile() bool {
	return f.Type == CapabilityFieldTypeSecret || f.Type == CapabilityFieldTypeCertificate
}

// ToConsumer reports whether the field reaches the consumer: direction to_consumer or both.
func (f CapabilityField) ToConsumer() bool {
	return f.Direction == "" || f.Direction == ToConsumer || f.Direction == Both
}

// ToProvider reports whether the field reaches the provider: direction to_provider or both.
func (f CapabilityField) ToProvider() bool {
	return f.Direction == ToProvider || f.Direction == Both
}

// CheckCapability decodes one capability file strictly and checks every rule that concerns it
// alone (spec 0012). CheckYields checks its yields against the other capabilities.
func CheckCapability(file string, data []byte) (Capability, []CatalogProblem) {
	c := checker{file: file}
	var k Capability
	if !c.decode(data, &k, KindCapability, func() string { return k.ApiVersion }, func() string { return k.Kind }) {
		return k, c.problems
	}
	c.name("name", k.Name)
	if k.Name != "" && k.Name != strings.TrimSuffix(path.Base(file), ".yaml") {
		c.add("name", CodeNameMismatch, "name %q differs from the file name", k.Name)
	}
	c.description("description", k.Description)
	if k.Scheme != "" && !schemeRe.MatchString(k.Scheme) {
		c.add("scheme", CodeInvalidRule, "scheme %q is not a url scheme like postgres", k.Scheme)
	}
	if k.Fields == nil {
		c.add("fields", CodeMissingField, "fields is required")
	}
	for _, name := range sortedKeys(k.Fields) {
		c.capabilityField("fields."+name, name, k.Fields[name])
	}
	return k, c.problems
}

func (c *checker) capabilityField(p, name string, f CapabilityField) {
	if !fieldNameRe.MatchString(name) {
		c.add(p, CodeInvalidName, "field %q must be snake case, like admin_user", name)
	}
	c.description(p+".description", f.Description)
	if !slices.Contains(fieldTypes, f.Type) {
		c.add(p+".type", CodeInvalidType, "type %q is not one of string, int, bool, url, enum, secret, certificate", f.Type)
		return
	}
	if f.Pattern != "" {
		if f.Type != CapabilityFieldTypeString {
			c.add(p+".pattern", CodeInvalidRule, "pattern applies to a string field only")
		} else if _, err := regexp.Compile(`^(?:` + f.Pattern + `)$`); err != nil {
			c.add(p+".pattern", CodeInvalidRule, "pattern does not compile: %v", err)
		}
	}
	switch {
	case f.Type == CapabilityFieldTypeEnum && len(f.Values) == 0:
		c.add(p+".values", CodeInvalidRule, "an enum needs at least one value")
	case f.Type != CapabilityFieldTypeEnum && f.Values != nil:
		c.add(p+".values", CodeInvalidRule, "values applies to an enum field only")
	default:
		seen := map[string]bool{}
		for _, v := range f.Values {
			if v == "" || seen[v] {
				c.add(p+".values", CodeInvalidRule, "enum values must be unique and not empty")
				break
			}
			seen[v] = true
		}
	}
	switch f.Direction {
	case "", ToConsumer, ToProvider:
	case Both:
		if !f.IsFile() {
			c.add(p+".direction", CodeInvalidDirection, "direction both applies to a secret or certificate field only")
		}
	default:
		c.add(p+".direction", CodeInvalidDirection, "direction %q is not to_consumer, to_provider or both", f.Direction)
	}
}

// CheckYields checks the yields of capability k against the capabilities by name (spec 0014).
func CheckYields(file string, k Capability, byName map[string]Capability) []CatalogProblem {
	if k.Yields == nil {
		return nil
	}
	c := checker{file: file}
	y := *k.Yields
	target, ok := byName[y.Capability]
	switch {
	case !ok:
		c.add("yields.capability", CodeInvalidYields, "capability %q is not in the catalog", y.Capability)
		return c.problems
	case target.Yields != nil:
		c.add("yields.capability", CodeInvalidYields, "%s is itself a provisioner and cannot be yielded", y.Capability)
		return c.problems
	}
	for i, n := range y.Named {
		f, ok := target.Fields[n]
		switch {
		case !ok:
			c.add("yields.named", CodeInvalidYields, "named field %s is not a field of %s", n, y.Capability)
		case f.Type != CapabilityFieldTypeString || f.Pattern == "":
			c.add("yields.named", CodeInvalidYields, "named field %s must be a string with a pattern", n)
		case slices.Index(y.Named, n) != i:
			c.add("yields.named", CodeInvalidYields, "named field %s is listed twice", n)
		}
	}
	if y.Login != "" && !slices.Contains(y.Named, y.Login) {
		c.add("yields.login", CodeInvalidYields, "login %s must be one of the named fields", y.Login)
	}
	for _, f := range sortedKeys(target.Fields) {
		if _, ok := y.Fields[f]; !ok && target.Fields[f].ToConsumer() {
			c.add("yields.fields."+f, CodeMissingFieldSource, "field %s of %s needs a source", f, y.Capability)
		}
	}
	for _, f := range sortedKeys(y.Fields) {
		p := "yields.fields." + f
		field, ok := target.Fields[f]
		switch {
		case !ok:
			c.add(p, CodeUnknownField, "capability %s has no field %s", y.Capability, f)
			continue
		case !field.ToConsumer():
			c.add(p, CodeInvalidFieldSource, "%s is a to_provider field; the consumer gives its value", f)
			continue
		}
		src := y.Fields[f]
		named := slices.Contains(y.Named, f)
		switch src.Kind {
		case YieldNamed:
			if !named {
				c.add(p, CodeInvalidFieldSource, "%s is not in yields.named", f)
			}
		case YieldGenerate:
			if field.Type != CapabilityFieldTypeSecret {
				c.add(p, CodeInvalidFieldSource, "generate makes a secret, but %s is a %s", f, field.Type)
			}
		case YieldOutput:
			if !field.IsFile() {
				c.add(p, CodeInvalidFieldSource, "output is a file, but %s is a %s", f, field.Type)
			}
		case YieldAdmin:
			admin, ok := k.Fields[src.Admin]
			switch {
			case !ok:
				c.add(p, CodeInvalidFieldSource, "%s has no field %s", k.Name, src.Admin)
			case admin.IsFile() != field.IsFile():
				c.add(p, CodeInvalidFieldSource, "%s and %s.%s must both be files or both be text", f, k.Name, src.Admin)
			case !field.Optional && admin.Optional:
				c.add(p, CodeInvalidFieldSource, "required field %s cannot come from optional field %s", f, src.Admin)
			}
		}
		if named && src.Kind != YieldNamed {
			c.add(p, CodeInvalidFieldSource, "%s is in yields.named, so its source is named", f)
		}
	}
	c.job(y)
	return c.problems
}

func (c *checker) job(y Yields) {
	if code, msg := imageProblem(y.Job.Image); code != "" {
		if code != CodeImageDigest {
			code = CodeInvalidJob
		}
		c.add("yields.job.image", code, "%s", msg)
	}
	if strings.TrimSpace(y.Job.Provision) == "" {
		c.add("yields.job.provision", CodeInvalidJob, "a provisioner needs a provision script")
	}
	switch {
	case y.Login != "" && strings.TrimSpace(y.Job.Revoke) == "":
		c.add("yields.job.revoke", CodeInvalidJob, "a provisioner with a login needs a revoke script")
	case y.Login == "" && y.Job.Revoke != "":
		c.add("yields.job.revoke", CodeInvalidJob, "a revoke script needs yields.login")
	}
}

// imageProblem gives the code and message of why image is not a catalog image, `repo:tag` with
// an exact tag and no digest (spec 0031: the lock is the one place an image is pinned), or "".
func imageProblem(image string) (code, msg string) {
	if image == "" {
		return CodeMissingField, "image is required"
	}
	ref, err := reference.ParseNormalizedNamed(image)
	if err != nil {
		return CodeInvalidImage, fmt.Sprintf("%q is not an image reference: %v", image, err)
	}
	if _, ok := ref.(reference.Digested); ok {
		return CodeImageDigest, fmt.Sprintf("%q must not pin a digest; the stack's image lock freezes the version", image)
	}
	tagged, ok := ref.(reference.Tagged)
	switch {
	case !ok:
		return CodeImageUntagged, fmt.Sprintf("%q must carry an exact tag", image)
	case tagged.Tag() == "latest":
		return CodeImageLatest, fmt.Sprintf("%q must not use the latest tag", image)
	}
	return "", ""
}
