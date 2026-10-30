// Package envfile parses the strict env file syntax of spec 0005: UTF 8, LF or CRLF; each line is
// blank, a comment starting with # after optional spaces, or KEY=value with KEY matching
// ^[A-Z][A-Z0-9_]*$. The value runs to the end of the line; one pair of matching quotes around it
// is removed and nothing inside is unescaped. No export, no ${VAR}, no multi line values, no
// inline comments.
package envfile

import (
	"bytes"
	"fmt"
	"regexp"
	"strings"
	"unicode/utf8"
)

var keyRe = regexp.MustCompile(`^[A-Z][A-Z0-9_]*$`)

// Line is one KEY=value line.
type Line struct {
	Key   string
	Value string
	Line  int // 1 based
}

// Error is one line that breaks the syntax, or a key set twice.
type Error struct {
	Line    int
	Message string
}

func (e Error) Error() string { return fmt.Sprintf("line %d: %s", e.Line, e.Message) }

// Parse reads every line of data. It returns the KEY=value lines in file order and every broken
// line; the messages never repeat a value, which may be a secret.
func Parse(data []byte) ([]Line, []Error) {
	var (
		lines []Line
		errs  []Error
		seen  = map[string]int{}
	)
	raw := bytes.Split(data, []byte("\n"))
	if len(raw) > 0 && len(raw[len(raw)-1]) == 0 {
		raw = raw[:len(raw)-1] // the trailing newline ends the last line, it starts none
	}
	for i, b := range raw {
		n := i + 1
		b = bytes.TrimSuffix(b, []byte("\r"))
		if !utf8.Valid(b) {
			errs = append(errs, Error{n, "the line is not UTF 8"})
			continue
		}
		s := string(b)
		trimmed := strings.TrimLeft(s, " \t")
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		key, value, ok := strings.Cut(s, "=")
		switch {
		case !ok:
			errs = append(errs, Error{n, "the line is not KEY=value"})
			continue
		case !keyRe.MatchString(key):
			errs = append(errs, Error{n, fmt.Sprintf("the key %q must match ^[A-Z][A-Z0-9_]*$", key)})
			continue
		}
		if first, dup := seen[key]; dup {
			errs = append(errs, Error{n, fmt.Sprintf("%s is already set on line %d", key, first)})
			continue
		}
		seen[key] = n
		lines = append(lines, Line{Key: key, Value: unquote(value), Line: n})
	}
	return lines, errs
}

func unquote(v string) string {
	if len(v) >= 2 && (v[0] == '"' || v[0] == '\'') && v[len(v)-1] == v[0] {
		return v[1 : len(v)-1]
	}
	return v
}
