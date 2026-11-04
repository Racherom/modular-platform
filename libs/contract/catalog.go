package contract

import (
	"errors"
	"fmt"
	"io/fs"
	"path"
	"slices"
	"strings"

	"sigs.k8s.io/yaml"

	"github.com/racherom/modular-platform/sdk/manifest"
)

// Catalog rule codes (spec 0002 and 0005, grown by 0012, 0013 and 0014; the sdk fields come
// from sdk/manifest).
const (
	CodeInvalidEntry        = "invalid_entry"
	CodeMissingField        = manifest.CodeMissingField
	CodeInvalidName         = manifest.CodeInvalidName
	CodeNameMismatch        = "name_mismatch"
	CodeDuplicateName       = manifest.CodeDuplicateName
	CodeInvalidImage        = "invalid_image"
	CodeImageUntagged       = "image_untagged"
	CodeImageLatest         = "image_latest"
	CodeInvalidPort         = "invalid_port"
	CodeInvalidEnvName      = manifest.CodeInvalidEnvName
	CodeDuplicateEnv        = manifest.CodeDuplicateEnv
	CodeInvalidHealthcheck  = "invalid_healthcheck"
	CodeInvalidDuration     = "invalid_duration"
	CodeInvalidRetries      = "invalid_retries"
	CodeInvalidRuntime      = manifest.CodeInvalidRuntime
	CodeInvalidType         = manifest.CodeInvalidType
	CodeInvalidDefault      = manifest.CodeInvalidDefault
	CodeRuleNotAllowed      = manifest.CodeRuleNotAllowed
	CodeInvalidRule         = manifest.CodeInvalidRule
	CodeClientNotAllowed    = manifest.CodeClientNotAllowed
	CodePortNotAllowed      = manifest.CodePortNotAllowed
	CodeTooManyProvides     = manifest.CodeTooManyProvides
	CodeUnknownCapability   = manifest.CodeUnknownCapability
	CodeUnknownField        = manifest.CodeUnknownField
	CodePerCallerNotAllowed = manifest.CodePerCallerNotAllowed
	CodeInvalidDirection    = "invalid_direction"
	CodeInvalidVersion      = "invalid_version"
	CodeInvalidRange        = "invalid_range"
	CodeMissingFieldSource  = "missing_field_source"
	CodeInvalidFieldSource  = "invalid_field_source"
	CodeReservedSlot        = "reserved_slot"
	CodeInvalidTemplate     = "invalid_template"
	CodeInvalidSetupStep    = "invalid_setup_step"
	CodeInvalidMount        = "invalid_mount"
	CodeInvalidYields       = "invalid_yields"
	CodeInvalidJob          = "invalid_job"
)

// Catalog folders (spec 0011, "Catalog layout and loading").
const (
	capabilitiesDir = "capabilities"
	modulesDir      = "modules"
	moduleFile      = "module.yaml"
)

// CatalogProblem is one broken catalog rule in one file.
type CatalogProblem struct {
	File    string // path inside the catalog, like modules/whoami/module.yaml
	Path    string // dotted path inside the file; "." for the whole file
	Code    string
	Message string
}

// String formats the problem as the planner logs it:
// catalog/<file>: <dotted.path>: <code>: <message>.
func (p CatalogProblem) String() string {
	return fmt.Sprintf("catalog/%s: %s: %s: %s", p.File, p.Path, p.Code, p.Message)
}

// LoadCatalog reads capabilities/*.yaml and modules/*/module.yaml from fsys and checks every
// catalog rule; capabilities load first, so the module rules can check field names. It returns
// the modules and capabilities without problems, each sorted by name, and every broken rule; a
// caller must not serve a catalog that has problems.
func LoadCatalog(fsys fs.FS) (Catalog, []CatalogProblem) {
	var problems []CatalogProblem
	capFiles, err := fs.Glob(fsys, capabilitiesDir+"/*.yaml")
	if err != nil {
		return Catalog{}, []CatalogProblem{{File: ".", Path: ".", Code: CodeInvalidEntry, Message: err.Error()}}
	}
	slices.Sort(capFiles)

	type loaded[T any] struct {
		file     string
		value    T
		problems []CatalogProblem
	}
	var caps []loaded[Capability]
	byName := map[string]Capability{}
	seen := map[string]string{}
	for _, file := range capFiles {
		data, err := fs.ReadFile(fsys, file)
		if err != nil {
			problems = append(problems, CatalogProblem{File: file, Path: ".", Code: CodeInvalidEntry, Message: err.Error()})
			continue
		}
		c, ps := CheckCapability(file, data)
		if prev, ok := seen[c.Name]; ok && c.Name != "" {
			ps = append(ps, CatalogProblem{
				File: file, Path: "name", Code: CodeDuplicateName,
				Message: fmt.Sprintf("the name %q is already used by %s", c.Name, prev),
			})
		}
		if c.Name != "" {
			seen[c.Name] = file
			byName[c.Name] = c
		}
		caps = append(caps, loaded[Capability]{file: file, value: c, problems: ps})
	}
	for i := range caps {
		caps[i].problems = append(caps[i].problems, CheckYields(caps[i].file, caps[i].value, byName)...)
	}

	var modules []loaded[Module]
	dirs, err := fs.ReadDir(fsys, modulesDir)
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		problems = append(problems, CatalogProblem{File: modulesDir, Path: ".", Code: CodeInvalidEntry, Message: err.Error()})
	}
	seen = map[string]string{}
	for _, d := range dirs {
		if !d.IsDir() {
			continue
		}
		file := path.Join(modulesDir, d.Name(), moduleFile)
		data, err := fs.ReadFile(fsys, file)
		if err != nil {
			problems = append(problems, CatalogProblem{File: file, Path: ".", Code: CodeInvalidEntry, Message: "the module folder has no module.yaml"})
			continue
		}
		m, ps := CheckModule(file, data, byName)
		if prev, ok := seen[m.Name]; ok && m.Name != "" {
			ps = append(ps, CatalogProblem{
				File: file, Path: "name", Code: CodeDuplicateName,
				Message: fmt.Sprintf("the name %q is already used by %s", m.Name, prev),
			})
		}
		seen[m.Name] = file
		modules = append(modules, loaded[Module]{file: file, value: m, problems: ps})
	}

	out := Catalog{Modules: []Module{}, Capabilities: []Capability{}}
	for _, c := range caps {
		problems = append(problems, c.problems...)
		if len(c.problems) == 0 {
			out.Capabilities = append(out.Capabilities, c.value)
		}
	}
	for _, m := range modules {
		problems = append(problems, m.problems...)
		if len(m.problems) == 0 {
			out.Modules = append(out.Modules, m.value)
		}
	}
	// capabilities/ sorts before modules/; each file keeps its problems in the order found
	slices.SortStableFunc(problems, func(a, b CatalogProblem) int { return strings.Compare(a.File, b.File) })
	slices.SortFunc(out.Capabilities, func(a, b Capability) int { return strings.Compare(a.Name, b.Name) })
	slices.SortFunc(out.Modules, func(a, b Module) int { return strings.Compare(a.Name, b.Name) })
	return out, problems
}

// checker collects the problems of one catalog file.
type checker struct {
	file     string
	problems []CatalogProblem
}

func (c *checker) add(path, code, format string, args ...any) {
	c.problems = append(c.problems, CatalogProblem{File: c.file, Path: path, Code: code, Message: fmt.Sprintf(format, args...)})
}

func (c *checker) name(path, name string) {
	switch {
	case name == "":
		c.add(path, CodeMissingField, "a name is required")
	case !ValidName(name):
		c.add(path, CodeInvalidName, "%q must be a lowercase DNS label and not a YAML reserved word", name)
	}
}

func (c *checker) description(path, d string) {
	switch {
	case strings.TrimSpace(d) == "":
		c.add(path, CodeMissingField, "description is required")
	case strings.ContainsAny(d, "\r\n"):
		c.add(path, CodeInvalidRule, "description must be one line")
	}
}

// decode checks the format version before strict decoding, then decodes data strictly into v
// and checks apiVersion and kind. It reports whether the rest of the file can be checked.
func (c *checker) decode(data []byte, v any, kind string, apiVersion, gotKind func() string) bool {
	var h header
	if err := yaml.Unmarshal(data, &h); err == nil && h.FormatVersion != nil {
		s, _ := h.FormatVersion.(string)
		switch ok, tooNew := checkFormat(s); {
		case tooNew:
			c.add("formatVersion", CodeFormatTooNew, "needs a planner that reads format %s; this one reads 1.%d", s, FormatMinor)
			return false
		case !ok || s == "":
			c.add("formatVersion", CodeInvalidEntry, "formatVersion %v is not a version like 1.0", h.FormatVersion)
			return false
		}
	}
	if err := yaml.UnmarshalStrict(data, v); err != nil {
		c.add(".", CodeInvalidEntry, "the entry could not be read: %s", oneLine(err))
		return false
	}
	ok := true
	if apiVersion() != APIVersion {
		c.add("apiVersion", CodeUnsupportedVersion, "apiVersion %q is not %s", apiVersion(), APIVersion)
		ok = false
	}
	if gotKind() != kind {
		c.add("kind", CodeWrongKind, "kind %q is not %s", gotKind(), kind)
		ok = false
	}
	return ok
}

func sortedKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	return keys
}
