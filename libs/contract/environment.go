package contract

import (
	"fmt"
	"regexp"
	"strings"
)

// The kinds of an EnvPart (spec 0013, `environment`).
const (
	EnvLiteral = "literal" // text copied as is
	EnvUID     = "uid"     // {uid}: the setup user's id
	EnvGID     = "gid"     // {gid}: the setup user's group id
	EnvInstall = "install" // {install}: the install folder, platform flavor only
	EnvConfig  = "config"  // {config.<value>}: one of the entry's config values
	EnvUses    = "uses"    // {<slot>.<field>}: a text field of one of the entry's own bindings
)

// envPlaceholderRe finds the placeholders of an environment value: braces around text with no
// space and no other brace. Other braces, like JSON with spaces, stay literal text.
var envPlaceholderRe = regexp.MustCompile(`\{([^{}\s]+)\}`)

// EnvPart is one piece of an environment value: literal text or one placeholder.
type EnvPart struct {
	Kind  string // EnvLiteral, EnvUID, EnvGID, EnvInstall, EnvConfig or EnvUses
	Text  string // the literal text, or the placeholder as written with its braces
	Name  string // the config value, or the slot
	Field string // the capability field, for EnvUses
}

// ParseEnvironment splits an environment value into literal text and placeholders. It does not
// check that a slot, field or config value exists; CheckModule does that with the catalog.
func ParseEnvironment(value string) []EnvPart {
	var parts []EnvPart
	last := 0
	for _, loc := range envPlaceholderRe.FindAllStringSubmatchIndex(value, -1) {
		if loc[0] > last {
			parts = append(parts, EnvPart{Kind: EnvLiteral, Text: value[last:loc[0]]})
		}
		parts = append(parts, placeholder(value[loc[0]:loc[1]], value[loc[2]:loc[3]]))
		last = loc[1]
	}
	if last < len(value) {
		parts = append(parts, EnvPart{Kind: EnvLiteral, Text: value[last:]})
	}
	return parts
}

func placeholder(text, inner string) EnvPart {
	switch inner {
	case EnvUID, EnvGID, EnvInstall:
		return EnvPart{Kind: inner, Text: text}
	}
	name, field, _ := strings.Cut(inner, ".")
	if name == EnvConfig {
		return EnvPart{Kind: EnvConfig, Text: text, Name: field}
	}
	return EnvPart{Kind: EnvUses, Text: text, Name: name, Field: field}
}

// environment checks the placeholders of every environment value (spec 0013): {uid}, {gid},
// {install}, a declared config value, or a text field of a declared slot that reaches the
// consumer.
func (c *moduleChecker) environment() {
	slots := c.m.Slots()
	for _, name := range sortedKeys(c.m.Environment) {
		p := "environment." + name
		for _, part := range ParseEnvironment(c.m.Environment[name]) {
			switch part.Kind {
			case EnvConfig:
				v, ok := c.m.Config[part.Name]
				if c.want(p, ok, "%s names config value %q, which is not declared", part.Text, part.Name) {
					c.want(p, v.Type != ConfigValueTypeSecret, "%s names a secret, which never travels in an env value", part.Text)
				}
			case EnvUses:
				slot, ok := slots[part.Name]
				if !c.want(p, ok && part.Field != "", "%s is not {uid}, {gid}, {install}, {config.<value>} or {<slot>.<field>} of a declared slot", part.Text) {
					continue
				}
				f, ok := c.caps[slot.Capability].Fields[part.Field]
				c.want(p, ok && !f.IsFile() && f.ToConsumer(), "%s names %s, which is not a text field slot %s receives", part.Text, part.Field, part.Name)
			}
		}
	}
}

// network checks that every alias names a declared config value that is not a secret.
func (c *moduleChecker) network() {
	if c.m.Network == nil {
		return
	}
	for i, a := range c.m.Network.Aliases {
		v, ok := c.m.Config[a.Config]
		if !ok || v.Type == ConfigValueTypeSecret {
			c.add(fmt.Sprintf("network.aliases.%d", i), CodeInvalidRule, "alias names config value %q, which is not a declared text value", a.Config)
		}
	}
}
