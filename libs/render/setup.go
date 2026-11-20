package render

import (
	"bytes"
	"cmp"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/racherom/modular-platform/libs/contract"
)

// SetupFormatVersion is the only formatVersion of setup.json this release writes and reads.
const SetupFormatVersion = 1

// ErrSetupInvalid is returned for a setup.json the keeper must refuse: another formatVersion, an
// unknown field, or text that is not one JSON object (the keeper reports it as state_invalid).
var ErrSetupInvalid = errors.New("setup.json is invalid")

// The formats of a file in Setup.Generate.
const (
	// GenerateHex is 32 random bytes as hex, the default of a generated secret.
	GenerateHex = string(contract.SecretFormatHex)
	// GeneratePassword is 24 characters drawn uniformly from A to Z, a to z and 0 to 9.
	GeneratePassword = string(contract.SecretFormatPassword)
	// GenerateEmpty is an empty file a provision job writes into at every up (spec 0014).
	GenerateEmpty = "empty"
)

// Setup is what the keeper applies to the state folder before it starts the platform (spec 0016,
// "Render changes"), written as setup.json: every install action of every instance, the keeper
// included, so the files keeper.yml mounts exist before the keeper starts. Paths are relative to
// the state folder.
type Setup struct {
	FormatVersion int            `json:"formatVersion"`
	Generate      []GenerateFile `json:"generate"` // module secrets, per binding files, then empty job outputs (spec 0013, step 4)
	Steps         []SetupRun     `json:"steps"`    // in the order of spec 0013, step 5
	Folders       []SetupFolder  `json:"folders"`  // sorted by path
	Show          []ShownSecret  `json:"show"`     // instance then name order
	Keeper        SetupKeeper    `json:"keeper"`
}

// GenerateFile is one file the keeper makes when it is missing.
type GenerateFile struct {
	Path   string `json:"path"`
	Format string `json:"format"` // GenerateHex, GeneratePassword or GenerateEmpty
	Show   bool   `json:"show"`   // setup.sh prints it once at a fresh install
	// Readers lists the writes of every step run that reads the file. A shown file whose
	// readers all exist is never made again, so deleting it never leaves a password no
	// account uses.
	Readers []string `json:"readers"`
}

// SetupRun is one run of a setup step: a for_each step has one per client, with {client}
// already replaced.
type SetupRun struct {
	Instance   string   `json:"instance"`
	Name       string   `json:"name"`             // the step's name
	Client     string   `json:"client,omitempty"` // the consumer of a for_each run
	Image      string   `json:"image"`
	Entrypoint []string `json:"entrypoint"` // empty: the image's own
	Command    []string `json:"command"`
	// Network is "internal" for a step that joins the platform's internal network (spec 0037);
	// left out, the run has no network.
	Network string            `json:"network,omitempty"`
	Env     map[string]string `json:"env"` // the step's env; the keeper adds MP_GRAPH and MP_INSTANCE
	// AskEnv maps each MP_ASK_<V> to the .env variable MP_<I>__<V> it reads. An asked value the
	// graph sets is in Env as its text instead.
	AskEnv map[string]string `json:"askEnv"`
	Reads  []SetupRead       `json:"reads"`
	Writes []SetupWrite      `json:"writes"`
}

// SetupRead is a file a run mounts read only.
type SetupRead struct {
	Path  string `json:"path"`
	Mount string `json:"mount"`
}

// SetupWrite is a file the run writes into /out, which then moves into secrets/<instance>/; a
// name ending in / is a folder.
type SetupWrite struct {
	Name string `json:"name"`
	Mode string `json:"mode"` // octal, such as 0600
}

// SetupFolder is a folder the keeper makes when it is missing.
type SetupFolder struct {
	Path string `json:"path"`
	Mode string `json:"mode"`
}

// ShownSecret is a show: once secret setup.sh prints at a fresh install.
type ShownSecret struct {
	Path        string `json:"path"`
	Description string `json:"description"`
}

// SetupKeeper names the keeper instance and the command its seed runs (spec 0016).
type SetupKeeper struct {
	Instance string   `json:"instance"`
	Module   string   `json:"module"`
	Command  []string `json:"command"`
}

// MarshalSetup writes s as canonical JSON: keys sorted, two space indent, no HTML escaping and
// a final newline, so the same Setup always gives the same bytes.
func MarshalSetup(s Setup) ([]byte, error) {
	raw, err := json.Marshal(s)
	if err != nil {
		return nil, fmt.Errorf("setup.json: %w", err)
	}
	var tree any // a map marshals with its keys sorted
	if err := json.Unmarshal(raw, &tree); err != nil {
		return nil, fmt.Errorf("setup.json: %w", err)
	}
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	if err := enc.Encode(tree); err != nil {
		return nil, fmt.Errorf("setup.json: %w", err)
	}
	return buf.Bytes(), nil
}

// DecodeSetup reads setup.json, refusing another formatVersion, an unknown field and trailing
// text with ErrSetupInvalid.
func DecodeSetup(data []byte) (Setup, error) {
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	var s Setup
	if err := dec.Decode(&s); err != nil {
		return Setup{}, fmt.Errorf("%w: %w", ErrSetupInvalid, err)
	}
	if dec.More() {
		return Setup{}, fmt.Errorf("%w: text after the object", ErrSetupInvalid)
	}
	if s.FormatVersion != SetupFormatVersion {
		return Setup{}, fmt.Errorf("%w: formatVersion %d, want %d", ErrSetupInvalid, s.FormatVersion, SetupFormatVersion)
	}
	return s, nil
}

// setup collects the Setup of a keeper render from what the render listed for setup.sh.
func (r *renderer) setup(keeper string) Setup {
	s := Setup{
		FormatVersion: SetupFormatVersion,
		Generate:      []GenerateFile{},
		Steps:         slices.Clone(r.runs),
		Folders:       []SetupFolder{},
		Show:          []ShownSecret{},
		Keeper: SetupKeeper{
			Instance: keeper,
			Module:   r.g.Modules[keeper].Module,
			Command:  slices.Clone(r.modules[r.g.Modules[keeper].Module].Keeper.Command),
		},
	}
	if s.Steps == nil {
		s.Steps = []SetupRun{}
	}
	readers := map[string][]string{}
	for _, run := range s.Steps {
		for _, rd := range run.Reads {
			for _, w := range run.Writes {
				readers[rd.Path] = append(readers[rd.Path], "secrets/"+run.Instance+"/"+strings.TrimSuffix(w.Name, "/"))
			}
		}
	}
	files := slices.Clone(r.data.Secrets)
	tier := func(f secret) int {
		switch {
		case f.Output:
			return 2
		case f.PerBinding:
			return 1
		}
		return 0
	}
	slices.SortStableFunc(files, func(a, b secret) int { return cmp.Or(tier(a)-tier(b), strings.Compare(a.Path, b.Path)) })
	for _, f := range files {
		g := GenerateFile{Path: f.Path, Format: GenerateHex, Show: f.Show, Readers: []string{}}
		switch {
		case f.Output:
			g.Format = GenerateEmpty
		case f.Format != "":
			g.Format = string(f.Format)
		}
		for _, w := range readers[f.Path] {
			if !slices.Contains(g.Readers, w) {
				g.Readers = append(g.Readers, w)
			}
		}
		slices.Sort(g.Readers)
		s.Generate = append(s.Generate, g)
	}
	// instance then name order: a path order would put secrets/a-b/ before secrets/a/
	slices.SortFunc(files, func(a, b secret) int {
		return cmp.Or(strings.Compare(a.Instance, b.Instance), strings.Compare(a.Path, b.Path))
	})
	for _, f := range files {
		if f.Show {
			s.Show = append(s.Show, ShownSecret{Path: f.Path, Description: f.Description})
		}
	}
	folders := slices.Clone(r.data.Folders)
	for _, st := range r.data.Steps {
		folders = append(folders, st.Folders...)
	}
	slices.Sort(folders)
	for _, f := range slices.Compact(folders) {
		s.Folders = append(s.Folders, SetupFolder{Path: f, Mode: "0700"})
	}
	return s
}
