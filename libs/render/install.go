package render

import (
	"fmt"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/compose-spec/compose-go/v2/types"

	"github.com/racherom/modular-platform/libs/contract"
	"github.com/racherom/modular-platform/sdk/manifest"
)

// askedValue is one question setup.sh asks before anything else (spec 0013, Install time, steps
// 2 and 3): a text value it writes into .env, or a secret it writes into its file.
type askedValue struct {
	Func string // the shell function that asks it: ask or ask_secret
	Args string // its arguments, quoted
}

// stepRun is one run of a setup step (spec 0013, Install time, step 5); a for_each step has one
// per client.
type stepRun struct {
	Title   string   // "<step> of <instance>", plus " for <client>"
	Out     string   // secrets/<i>, mounted at /out
	Writes  []string // the files and folders (ending in /) whose existence skips the run
	Modes   []string // "<mode> <path>" per write, applied after the run
	Asks    []string // the arguments of step_ask per question, quoted
	Reads   []string // the files the run mounts, which must exist
	Command string   // the docker run arguments, quoted
}

// installStep is one declared setup step of one instance with its runs.
type installStep struct {
	Title   string
	Folders []string // secrets/<i>/<folder> a for_each step writes into, made before its runs
	Runs    []stepRun
}

// installTime collects what setup.sh does for instance i besides generating files: its asked
// config values and asked secrets, and its mounts of the secrets its setup steps write. The
// managed flavor has no setup.sh, so it refuses all of these until #9 (spec 0013).
func (r *renderer) installTime(svc *types.ServiceConfig, i string, m contract.Module) error {
	if r.managed && needsSetup(m) {
		return fmt.Errorf("%w: module %s needs setup.sh (asked values, asked or setup secrets, or setup steps), which a deployed graph cannot run yet", ErrNotRendered, m.Name)
	}
	compose := m.ComposeValues()
	for _, name := range sortedKeys(m.Config) {
		v := m.Config[name]
		if strings.HasPrefix(v.Default, "{install}/") {
			return fmt.Errorf("%w: {install} in config value %s of module %s belongs to the platform flavor", ErrNotRendered, name, m.Name)
		}
		switch {
		case v.Type == contract.ConfigValueTypeSecret && v.Asked():
			path := "secrets/" + i + "/" + name
			target := "/run/secrets/" + i + "_" + name
			r.mountSecret(svc, i+"_"+name, path, target)
			setEnv(svc, v.Env+"_FILE", target)
			r.declared[path] = true
			r.data.Asked = append(r.data.Asked, askedValue{Func: "ask_secret", Args: askSecretArgs(path, v.Optional, v.Description)})
		case slices.Contains(compose, name):
			setEnvRaw(svc, v.Env, composeVar(i, name, v))
			if v.Asked() {
				a, err := r.askText(i, name, m)
				if err != nil {
					return err
				}
				r.data.Asked = append(r.data.Asked, a)
			}
		case v.Default == "{uid}" || v.Default == "{gid}":
			setEnvRaw(svc, v.Env, map[string]string{"{uid}": "${MP_UID}", "{gid}": "${MP_GID}"}[v.Default])
		}
	}
	for _, s := range slices.SortedFunc(slices.Values(m.Secrets), func(a, b contract.Secret) int { return strings.Compare(a.Name, b.Name) }) {
		if s.From == contract.SecretFromAsk {
			path := "secrets/" + i + "/" + s.Name
			r.declared[path] = true
			r.data.AskedSecrets = append(r.data.AskedSecrets, askedValue{Func: "ask_secret", Args: askSecretArgs(path, false, s.Description)})
		}
	}
	return nil
}

// needsSetup reports whether setup.sh must ask or run something for an instance of m.
func needsSetup(m contract.Module) bool {
	for _, v := range m.Config {
		if v.Asked() {
			return true
		}
	}
	for _, s := range m.Secrets {
		if s.From == contract.SecretFromAsk || s.From == contract.SecretFromSetup {
			return true
		}
	}
	return len(m.Setup) > 0
}

// askText is the question for asked text value name of instance i: its variable, the checks
// of its type, its description and its default as a shell word.
func (r *renderer) askText(i, name string, m contract.Module) (askedValue, error) {
	v := m.Config[name]
	rules := manifest.Rules{Type: string(v.Type), Pattern: v.Pattern, Values: v.Values}
	if v.Min != nil {
		rules.Min = v.Min.Text()
	}
	if v.Max != nil {
		rules.Max = v.Max.Text()
	}
	if rules.Type == manifest.TypeDuration {
		rules.Min, rules.Max = nanoseconds(rules.Min), nanoseconds(rules.Max)
		r.data.NeedDuration = true
	}
	var re string
	if rules.Pattern != "" {
		var err error
		if re, err = erePattern(rules.Pattern); err != nil {
			return askedValue{}, fmt.Errorf("config value %s of module %s: %w", name, m.Name, err)
		}
	}
	words := []string{
		contract.ComposeVar(i, name), shellQuote(rules.Type), shellQuote(rules.Min), shellQuote(rules.Max), shellQuote(re),
		strconv.FormatBool(v.Optional), shellQuote(v.Description), r.defaultWord(i, m, v.Default),
	}
	for _, e := range rules.Values {
		words = append(words, shellQuote(e))
	}
	return askedValue{Func: "ask", Args: strings.Join(words, " ")}, nil
}

// nanoseconds writes a duration bound as whole nanoseconds, which setup.sh compares; "" stays.
func nanoseconds(d string) string {
	if d == "" {
		return ""
	}
	n, _ := time.ParseDuration(d) // the catalog check refused a bound that does not parse
	return strconv.FormatInt(int64(n), 10)
}

// askedRefRe finds the {<value>} references a default makes to another asked value.
var askedRefRe = regexp.MustCompile(`\{([a-z0-9-]+)\}`)

// defaultWord is the default of an asked value as one double quoted shell word: {uid} and {gid}
// as the setup user's ids, and {<value>} naming another asked value of the same instance as
// that value's answer in .env.
func (r *renderer) defaultWord(i string, m contract.Module, def string) string {
	switch def {
	case "{uid}":
		return `"$(id -u)"`
	case "{gid}":
		return `"$(id -g)"`
	}
	var b strings.Builder
	b.WriteString(`"`)
	last := 0
	for _, loc := range askedRefRe.FindAllStringSubmatchIndex(def, -1) {
		ref := def[loc[2]:loc[3]]
		if v, ok := m.Config[ref]; !ok || !v.Asked() || v.Type == contract.ConfigValueTypeSecret {
			continue
		}
		b.WriteString(doubleQuoted(def[last:loc[0]]))
		b.WriteString("$(env_get " + contract.ComposeVar(i, ref) + ")")
		last = loc[1]
		r.data.NeedEnvGet = true
	}
	b.WriteString(doubleQuoted(def[last:]) + `"`)
	return b.String()
}

// doubleQuoted escapes s for the inside of a double quoted shell word.
func doubleQuoted(s string) string {
	return strings.NewReplacer(`\`, `\\`, `"`, `\"`, "$", `\$`, "`", "\\`").Replace(s)
}

func askSecretArgs(path string, optional bool, description string) string {
	return path + " " + strconv.FormatBool(optional) + " " + shellQuote(description)
}

// setupSecrets mounts the secrets instance i's setup steps write (from: setup): a file as a
// Compose secret, a folder (a name ending in /) as a read only bind of secrets/<i>/<folder>.
func (r *renderer) setupSecrets(svc *types.ServiceConfig, i string, s contract.Secret) {
	if folder, ok := strings.CutSuffix(s.Name, "/"); ok {
		svc.Volumes = append(svc.Volumes, types.ServiceVolumeConfig{
			Type: types.VolumeTypeBind, Source: "./secrets/" + i + "/" + folder, Target: s.Mount, ReadOnly: true,
		})
		return
	}
	path := "secrets/" + i + "/" + s.Name
	target := s.Mount
	if target == "" {
		target = "/run/secrets/" + i + "_" + s.Name
	}
	r.mountSecret(svc, i+"_"+s.Name, path, target)
	if s.Env != "" {
		setEnv(svc, s.Env, target)
	}
	r.declared[path] = true
}

// steps lists every setup step run, instances in binding order (providers first, ties by name)
// and steps in declared order (spec 0013, Install time, step 5). It runs after the bindings,
// whose files the steps read.
func (r *renderer) steps(bindings []contract.Binding) error {
	clients := map[string][]contract.Binding{} // provider.provide to its bindings, by consumer
	for _, b := range bindings {
		clients[b.Provider+"."+b.Provide] = append(clients[b.Provider+"."+b.Provide], b)
	}
	for _, i := range contract.BindingOrder(sortedKeys(r.g.Modules), bindings) {
		gm := r.g.Modules[i]
		if gm.External != nil {
			continue
		}
		m := r.modules[gm.Module]
		for _, s := range m.Setup {
			st := installStep{Title: s.Name + " of " + i}
			if s.ForEach == "" {
				run, err := r.stepRun(i, m, s, nil)
				if err != nil {
					return err
				}
				st.Runs = append(st.Runs, run)
				r.data.Steps = append(r.data.Steps, st)
				continue
			}
			for _, w := range s.Writes {
				folder, _ := strings.CutSuffix(w.Name, "/"+contract.ClientToken)
				st.Folders = append(st.Folders, "secrets/"+i+"/"+folder)
			}
			for _, b := range clients[i+"."+s.ForEach] {
				run, err := r.stepRun(i, m, s, &b)
				if err != nil {
					return err
				}
				st.Runs = append(st.Runs, run)
			}
			r.data.Steps = append(r.data.Steps, st)
		}
	}
	return nil
}

// stepRun builds one run of step s of instance i; client is the binding a for_each run is for.
// It runs as `docker run --rm -i --network none --user <you>` in the module's own image, with
// secrets/<i> at /out and each read mounted read only under /in (spec 0013).
func (r *renderer) stepRun(i string, m contract.Module, s contract.SetupStep, client *contract.Binding) (stepRun, error) {
	expand := func(text string) string { return text }
	run := stepRun{Title: s.Name + " of " + i, Out: "secrets/" + i}
	if client != nil {
		expand = func(text string) string { return strings.ReplaceAll(text, contract.ClientToken, client.Consumer) }
		run.Title += " for " + client.Consumer
	}
	args := []string{
		"--rm -i --network none", `--user "$(id -u):$(id -g)"`,
		`-v "$PWD/secrets/` + i + `:/out"`,
	}
	for _, rd := range s.Reads {
		file, target, err := r.readFile(i, m, rd, client)
		if err != nil {
			return run, fmt.Errorf("render: setup step %s of %s: %w", s.Name, i, err)
		}
		if file == "" {
			continue // an optional field the binding leaves out, or an unbound slot
		}
		run.Reads = append(run.Reads, file)
		args = append(args, `-v "$PWD/`+file+":"+target+`:ro"`)
	}
	args = append(args, "-e "+shellQuote("MP_GRAPH="+r.g.Name), "-e "+shellQuote("MP_INSTANCE="+i))
	for _, name := range sortedKeys(m.Config) {
		if v := m.Config[name]; v.Asked() && v.Type != contract.ConfigValueTypeSecret {
			args = append(args, `-e "MP_ASK_`+envPart(name)+"=$(env_get "+contract.ComposeVar(i, name)+`)"`)
			r.data.NeedEnvGet = true
		}
	}
	for _, k := range sortedKeys(s.Env) {
		args = append(args, "-e "+shellQuote(k+"="+expand(s.Env[k])))
	}
	entrypoint := make([]string, len(s.Entrypoint))
	for n, e := range s.Entrypoint {
		entrypoint[n] = expand(e)
	}
	if len(entrypoint) > 0 {
		args = append(args, "--entrypoint "+shellQuote(entrypoint[0]))
	}
	tail := []string{shellQuote(m.Image)}
	for _, e := range append(entrypoint[min(1, len(entrypoint)):], s.Command...) {
		tail = append(tail, shellQuote(expand(e)))
	}
	args = append(args, strings.Join(tail, " "))
	run.Command = strings.Join(args, " \\\n\t\t")

	for _, w := range s.Writes {
		path := "secrets/" + i + "/" + expand(w.Name)
		mode := string(w.Mode)
		switch {
		case strings.HasSuffix(path, "/"):
			mode = "0700"
		case mode == "":
			mode = string(contract.N0600)
		}
		run.Writes = append(run.Writes, path)
		run.Modes = append(run.Modes, mode+" "+strings.TrimSuffix(path, "/"))
	}
	for _, a := range s.Asks {
		run.Asks = append(run.Asks, shellQuote(a.Description)+" "+strconv.FormatBool(a.Secret))
	}
	return run, nil
}

// readFile resolves one read of a setup step to the project relative file it mounts and where:
// its own secret at /in/<name>, a field of its binding on a slot at /in/<slot>/<field>, and in a
// for_each run the client's file of a field of its provide at /in/<provide>/<field>. The
// planner's setup_order check has made sure the file exists when the step runs.
func (r *renderer) readFile(i string, m contract.Module, rd contract.SetupRead, client *contract.Binding) (file, target string, err error) {
	switch {
	case rd.Secret != "":
		name := strings.TrimSuffix(rd.Secret, "/")
		return "secrets/" + i + "/" + name, "/in/" + name, nil
	case rd.Slot != "":
		v, bound := r.bound[i][rd.Slot][rd.Field]
		if !bound {
			return "", "", nil
		}
		if v.file == "" {
			return "", "", fmt.Errorf("field %s of slot %s is text, and a step reads files", rd.Field, rd.Slot)
		}
		return v.file, "/in/" + rd.Slot + "/" + rd.Field, nil
	}
	target = "/in/" + rd.Provide + "/" + rd.Field
	field := r.caps[m.Provides[rd.Provide].Capability].Fields[rd.Field]
	if field.Direction == contract.Both {
		return r.bound[client.Consumer][client.Slot][rd.Field].file, target, nil
	}
	consumer := r.modules[r.g.Modules[client.Consumer].Module]
	src, ok := consumer.Slots()[client.Slot].Fields[rd.Field]
	if !ok || src.Secret == "" {
		return "", "", nil
	}
	return "secrets/" + client.Consumer + "/" + src.Secret, target, nil
}
