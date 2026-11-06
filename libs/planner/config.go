package planner

import (
	"errors"
	"strings"
	"unicode"

	"github.com/racherom/modular-platform/libs/contract"
	"github.com/racherom/modular-platform/sdk/manifest"
)

// Problem codes of the config values a graph sets on an instance (spec 0025).
const (
	// CodeUnknownConfig is a value name the instance's entry does not declare.
	CodeUnknownConfig = "unknown_config"
	// CodeInvalidConfig is text the value's rules or the graph text rules refuse, or empty text
	// on a required value.
	CodeInvalidConfig = "invalid_config"
	// CodeConfigNotSettable is a secret value, a value filled at install time, or one a
	// host_mounts source names: a graph never sets those.
	CodeConfigNotSettable = "config_not_settable"
	// CodeConfigNotAllowed is a config value on an external instance.
	CodeConfigNotAllowed = "config_not_allowed"
	// CodeMissingConfig is a required value with no default that nobody asks and the graph
	// leaves out: an error on a non sdk instance, a warning on an sdk one.
	CodeMissingConfig = "missing_config"
)

// config checks the config values the graph sets on each instance and reports the required
// values nothing fills (spec 0025, AC-27). Each path gets one problem, the first that applies in
// the order config_not_allowed, unknown_config, config_not_settable, invalid_config.
func (p *planning) config(instances []string) {
	for _, i := range instances {
		if !p.valid[i] {
			continue
		}
		gm := p.graph.Modules[i]
		var set contract.InstanceConfig
		if gm.Config != nil {
			set = *gm.Config
		}
		if gm.External != nil {
			for _, v := range sortedKeys(set) {
				p.fail(CodeConfigNotAllowed, configPath(i, v), "Instance %s is external, so it takes no config values; set %s on the service itself.", i, v)
			}
			continue
		}
		m, ok := p.modules[gm.Module]
		if !ok {
			continue // unknown_module says so
		}
		for _, v := range sortedKeys(set) {
			p.configValue(i, v, set[v], m)
		}
		for _, v := range sortedKeys(m.Config) {
			cv := m.Config[v]
			if _, given := set[v]; given || cv.Optional || cv.Default != "" || cv.Asked() || cv.Type == contract.ConfigValueTypeSecret {
				continue
			}
			pr := contract.Problem{Severity: contract.Error, Code: CodeMissingConfig, Path: configPath(i, v)}
			if m.Runtime == contract.Sdk {
				pr.Severity = contract.Warning
				pr.Message = "Instance " + i + " needs config value " + v + ", which has no default; set it here or in config/" + i + ".env, or the module stops at start."
			} else {
				pr.Message = "Instance " + i + " needs config value " + v + ", which has no default and is not asked; set it here."
			}
			p.add(pr)
		}
	}
}

// configValue checks the text the graph sets as value v of instance i of module m.
func (p *planning) configValue(i, v, text string, m contract.Module) {
	path := configPath(i, v)
	cv, declared := m.Config[v]
	if !declared {
		p.fail(CodeUnknownConfig, path, "Instance %s sets config value %s, but module %s declares no such value.", i, v, m.Name)
		return
	}
	if why := notSettable(m, v, cv); why != "" {
		p.fail(CodeConfigNotSettable, path, "Config value %s of instance %s %s, so a graph cannot set it.", v, i, why)
		return
	}
	if text == "" {
		if !cv.Optional {
			p.fail(CodeInvalidConfig, path, "Config value %s of instance %s is required, so it cannot be empty.", v, i)
		}
		return // empty on purpose beats the default
	}
	if err := cv.Check(text); err != nil {
		msg := err.Error()
		if re, ok := errors.AsType[*manifest.RuleError](err); ok {
			msg = re.Message
		}
		p.fail(CodeInvalidConfig, path, "Config value %s of instance %s: %s.", v, i, msg)
		return
	}
	if rule := GraphTextRule(text); rule != "" {
		p.fail(CodeInvalidConfig, path, "Config value %s of instance %s: %s.", v, i, rule)
	}
}

// notSettable says why a graph may not set value v of m, or "" when it may: a secret, a default
// the render fills at install time, or a host path a host_mounts source names.
func notSettable(m contract.Module, v string, cv contract.ConfigValue) string {
	switch {
	case cv.Type == contract.ConfigValueTypeSecret:
		return "is a secret, which a graph never holds"
	case manifest.InstallToken(cv.Default):
		return "is filled at install time"
	}
	for _, h := range m.HostMounts {
		if h.Host.Config == v || h.Path.Config == v {
			return "names a host path, which a graph never picks"
		}
	}
	return ""
}

// GraphTextRule names the graph text rule that text breaks, or "" (spec 0025, "Graph text
// rules"), checked in order: a control character (C0, DEL, C1, U+2028, U+2029); { or }, which
// Compose misreads inside ${V:-...}; two characters or more with the same ASCII quote at both
// ends, which sdk/envfile strips. The text is tested exactly, nothing trimmed, and the message
// never repeats it.
func GraphTextRule(text string) string {
	switch {
	case strings.ContainsFunc(text, func(r rune) bool { return unicode.IsControl(r) || r == ' ' || r == ' ' }):
		return "the text holds a control character"
	case strings.ContainsAny(text, "{}"):
		return "the text holds { or }, which Compose misreads"
	case manifest.QuotedText(text):
		return "the text starts and ends with the same quote, which the env file reader removes"
	}
	return ""
}

func configPath(i, v string) string { return "modules." + i + ".config." + v }
