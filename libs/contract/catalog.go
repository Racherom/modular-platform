package contract

import (
	"fmt"
	"io/fs"
	"path"
	"slices"
	"strings"
	"time"

	"github.com/distribution/reference"
	"sigs.k8s.io/yaml"

	"github.com/racherom/modular-platform/sdk/manifest"
)

// Catalog rule codes (AC-11 of spec 0001; the sdk fields of spec 0005 come from sdk/manifest).
const (
	CodeInvalidEntry       = "invalid_entry"
	CodeMissingField       = manifest.CodeMissingField
	CodeInvalidName        = manifest.CodeInvalidName
	CodeNameMismatch       = "name_mismatch"
	CodeDuplicateName      = manifest.CodeDuplicateName
	CodeInvalidImage       = "invalid_image"
	CodeImageUntagged      = "image_untagged"
	CodeImageLatest        = "image_latest"
	CodeInvalidPort        = "invalid_port"
	CodeInvalidEnvName     = manifest.CodeInvalidEnvName
	CodeDuplicateEnv       = manifest.CodeDuplicateEnv
	CodeInvalidHealthcheck = "invalid_healthcheck"
	CodeInvalidDuration    = "invalid_duration"
	CodeInvalidRetries     = "invalid_retries"
	CodeInvalidRuntime     = manifest.CodeInvalidRuntime
	CodeInvalidType        = manifest.CodeInvalidType
	CodeInvalidDefault     = manifest.CodeInvalidDefault
	CodeRuleNotAllowed     = manifest.CodeRuleNotAllowed
	CodeInvalidRule        = manifest.CodeInvalidRule
	CodeClientNotAllowed   = manifest.CodeClientNotAllowed
	CodeUnknownBindingKey  = manifest.CodeUnknownBindingKey
	CodePortNotAllowed     = manifest.CodePortNotAllowed
	CodeTooManyProvides    = manifest.CodeTooManyProvides
)

// CatalogProblem is one broken catalog rule in one file.
type CatalogProblem struct {
	File    string // file name inside the catalog, like whoami.yaml
	Path    string // dotted path inside the entry; "." for the whole file
	Code    string
	Message string
}

// String formats the problem as the planner logs it:
// catalog/<file>: <dotted.path>: <code>: <message>.
func (p CatalogProblem) String() string {
	return fmt.Sprintf("catalog/%s: %s: %s: %s", p.File, p.Path, p.Code, p.Message)
}

// LoadCatalog reads every *.yaml file at the root of fsys as one catalog entry and checks the
// catalog rules. It returns the entries sorted by name, and every broken rule; a caller must not
// serve a catalog that has problems.
func LoadCatalog(fsys fs.FS) ([]CatalogEntry, []CatalogProblem) {
	files, err := fs.Glob(fsys, "*.yaml")
	if err != nil {
		return nil, []CatalogProblem{{File: ".", Path: ".", Code: CodeInvalidEntry, Message: err.Error()}}
	}
	slices.Sort(files)

	var (
		entries  []CatalogEntry
		problems []CatalogProblem
		seen     = map[string]string{}
	)
	for _, file := range files {
		data, err := fs.ReadFile(fsys, file)
		if err != nil {
			problems = append(problems, CatalogProblem{File: file, Path: ".", Code: CodeInvalidEntry, Message: err.Error()})
			continue
		}
		entry, entryProblems := CheckEntry(file, data)
		if prev, ok := seen[entry.Name]; ok && entry.Name != "" {
			entryProblems = append(entryProblems, CatalogProblem{
				File: file, Path: "name", Code: CodeDuplicateName,
				Message: fmt.Sprintf("the name %q is already used by %s", entry.Name, prev),
			})
		}
		seen[entry.Name] = file
		problems = append(problems, entryProblems...)
		if len(entryProblems) == 0 {
			entries = append(entries, entry)
		}
	}
	slices.SortFunc(entries, func(a, b CatalogEntry) int { return strings.Compare(a.Name, b.Name) })
	return entries, problems
}

// CheckEntry decodes one catalog file strictly and checks every rule that concerns it alone. The
// sdk fields and every env name are checked by sdk/manifest, so sdkgen refuses the same entries.
func CheckEntry(file string, data []byte) (CatalogEntry, []CatalogProblem) {
	c := entryChecker{file: file}
	var e CatalogEntry
	if err := yaml.UnmarshalStrict(data, &e); err != nil {
		c.add(".", CodeInvalidEntry, "the entry could not be read: %s", oneLine(err))
		return e, c.problems
	}
	m, err := manifest.Parse(data)
	if err != nil {
		// the schema and sdk/manifest disagree about a field; manifest_test.go guards against it
		c.add(".", CodeInvalidEntry, "the entry could not be read: %s", oneLine(err))
		return e, c.problems
	}

	if e.ApiVersion != APIVersion {
		c.add("apiVersion", CodeUnsupportedVersion, "apiVersion %q is not %s", e.ApiVersion, APIVersion)
	}
	if e.Kind != KindModule {
		c.add("kind", CodeWrongKind, "kind %q is not %s", e.Kind, KindModule)
	}
	c.name("name", e.Name)
	if e.Name != "" && e.Name != strings.TrimSuffix(path.Base(file), ".yaml") {
		c.add("name", CodeNameMismatch, "name %q differs from the file name", e.Name)
	}
	if strings.TrimSpace(e.Description) == "" {
		c.add("description", CodeMissingField, "description is required")
	}
	c.image(e.Image)

	for _, capability := range sortedKeys(e.Provides) {
		c.name("provides."+capability, capability)
		if e.Runtime == Sdk {
			continue // a runtime: sdk provide has no port (manifest checks it)
		}
		if port := e.Provides[capability].Port; port < 1 || port > 65535 {
			c.add("provides."+capability+".port", CodeInvalidPort, "port %d is not between 1 and 65535", port)
		}
	}
	for _, capability := range sortedKeys(e.Uses) {
		c.name("uses."+capability, capability)
	}

	secretNames := map[string]bool{}
	for i, s := range e.Secrets {
		p := fmt.Sprintf("secrets.%d", i)
		c.name(p+".name", s.Name)
		if secretNames[s.Name] && s.Name != "" {
			c.add(p+".name", CodeDuplicateName, "secret %q is declared twice", s.Name)
		}
		secretNames[s.Name] = true
	}

	if e.Healthcheck != nil {
		c.healthcheck(*e.Healthcheck)
	}
	for _, p := range manifest.Check(m) {
		c.add(p.Path, p.Code, "%s", p.Message)
	}
	return e, c.problems
}

type entryChecker struct {
	file     string
	problems []CatalogProblem
}

func (c *entryChecker) add(path, code, format string, args ...any) {
	c.problems = append(c.problems, CatalogProblem{File: c.file, Path: path, Code: code, Message: fmt.Sprintf(format, args...)})
}

func (c *entryChecker) name(path, name string) {
	switch {
	case name == "":
		c.add(path, CodeMissingField, "a name is required")
	case !ValidName(name):
		c.add(path, CodeInvalidName, "%q must be a lowercase DNS label and not a YAML reserved word", name)
	}
}

func (c *entryChecker) image(image string) {
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

func (c *entryChecker) healthcheck(h Healthcheck) {
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

func sortedKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	return keys
}
