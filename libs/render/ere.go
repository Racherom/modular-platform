package render

import (
	"errors"
	"fmt"
	"regexp/syntax"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"
)

// maxRepeat is RE_DUP_MAX, the largest count POSIX promises in a {min,max} bound.
const maxRepeat = 255

// multibyte is one UTF-8 encoded character of two to four bytes, as bytes in the C locale.
const multibyte = "[\xc2-\xf4][\x80-\xbf]{1,3}"

// erePattern turns a config value's RE2 pattern into a printf format whose output is a POSIX
// extended regular expression that `LC_ALL=C grep -E` matches against a whole answer exactly
// when the planner's regexp matches it (spec 0013, Install time, step 2). The pattern is read in
// the C locale byte by byte, so a class or dot that reaches past ASCII matches any UTF-8
// character as its two to four bytes. A pattern using what ERE cannot say (word boundaries, a
// class of only some non ASCII characters, a literal newline) is refused, so setup.sh never
// checks less than the module states.
func erePattern(pattern string) (string, error) {
	re, err := syntax.Parse(pattern, syntax.Perl)
	if err != nil {
		return "", fmt.Errorf("pattern %q: %w", pattern, err)
	}
	var b strings.Builder
	b.WriteString("^(")
	if err := writeERE(&b, re.Simplify()); err != nil {
		return "", fmt.Errorf("%w: pattern %q in setup.sh: %w", ErrNotRendered, pattern, err)
	}
	b.WriteString(")$")
	return printfFormat(b.String()), nil
}

func writeERE(b *strings.Builder, re *syntax.Regexp) error {
	switch re.Op {
	case syntax.OpNoMatch:
		b.WriteString("[^\x01-\xff]")
	case syntax.OpEmptyMatch:
		return errors.New("an empty match has no POSIX form")
	case syntax.OpLiteral:
		for _, r := range re.Rune {
			if err := writeLiteral(b, r, re.Flags&syntax.FoldCase != 0); err != nil {
				return err
			}
		}
	case syntax.OpCharClass:
		return writeClass(b, re.Rune)
	case syntax.OpAnyCharNotNL:
		return writeClass(b, []rune{0, '\n' - 1, '\n' + 1, unicode.MaxRune})
	case syntax.OpAnyChar:
		return writeClass(b, []rune{0, unicode.MaxRune})
	case syntax.OpBeginLine, syntax.OpBeginText:
		b.WriteString("^")
	case syntax.OpEndLine, syntax.OpEndText:
		b.WriteString("$")
	case syntax.OpCapture:
		return writeGroup(b, re.Sub[0])
	case syntax.OpStar, syntax.OpPlus, syntax.OpQuest:
		if err := writeGroup(b, re.Sub[0]); err != nil {
			return err
		}
		b.WriteString(map[syntax.Op]string{syntax.OpStar: "*", syntax.OpPlus: "+", syntax.OpQuest: "?"}[re.Op])
	case syntax.OpRepeat:
		if re.Min > maxRepeat || re.Max > maxRepeat {
			return fmt.Errorf("a count above %d", maxRepeat)
		}
		if err := writeGroup(b, re.Sub[0]); err != nil {
			return err
		}
		upper := ""
		if re.Max >= 0 {
			upper = strconv.Itoa(re.Max)
		}
		fmt.Fprintf(b, "{%d,%s}", re.Min, upper)
	case syntax.OpConcat:
		for _, sub := range re.Sub {
			if err := writeERE(b, sub); err != nil {
				return err
			}
		}
	case syntax.OpAlternate:
		b.WriteString("(")
		for n, sub := range re.Sub {
			if n > 0 {
				b.WriteString("|")
			}
			if err := writeERE(b, sub); err != nil {
				return err
			}
		}
		b.WriteString(")")
	case syntax.OpWordBoundary, syntax.OpNoWordBoundary:
		return errors.New("a word boundary has no POSIX form")
	default:
		return fmt.Errorf("%s has no POSIX form", re.Op)
	}
	return nil
}

func writeGroup(b *strings.Builder, re *syntax.Regexp) error {
	b.WriteString("(")
	if err := writeERE(b, re); err != nil {
		return err
	}
	b.WriteString(")")
	return nil
}

// writeLiteral writes one character: an ERE special escaped, a letter under (?i) as a class of
// its cases, a non ASCII character as its UTF-8 bytes.
func writeLiteral(b *strings.Builder, r rune, fold bool) error {
	if fold && unicode.SimpleFold(r) != r {
		var ranges []rune
		for f := r; ; {
			ranges = append(ranges, f, f)
			if f = unicode.SimpleFold(f); f == r {
				break
			}
		}
		return writeClass(b, ranges)
	}
	switch {
	case r == 0 || r == '\n':
		return errors.New("a literal NUL or newline")
	case r >= utf8.RuneSelf:
		b.WriteString(string(r))
	case strings.ContainsRune(`.[]()*+?{}|^$\`, r):
		b.WriteString(`\` + string(r))
	default:
		b.WriteRune(r) // ASCII, so one byte
	}
	return nil
}

// writeClass writes a class given as rune ranges: its ASCII part as a bracket expression, and
// any UTF-8 character when the class holds every non ASCII character. NUL and newline never
// appear in an answer, so they are left out.
func writeClass(b *strings.Builder, ranges []rune) error {
	var ascii [utf8.RuneSelf]bool
	var high rune // how many runes above ASCII the class holds
	for n := 0; n+1 < len(ranges); n += 2 {
		lo, hi := ranges[n], ranges[n+1]
		for r := lo; r <= hi && r < utf8.RuneSelf; r++ {
			ascii[r] = true
		}
		if hi >= utf8.RuneSelf {
			high += hi - max(lo, utf8.RuneSelf) + 1
		}
	}
	allHigh := high == unicode.MaxRune-utf8.RuneSelf+1
	if high > 0 && !allHigh {
		return errors.New("a class of only some non ASCII characters")
	}
	ascii[0], ascii['\n'] = false, false
	bracket := bracketExpr(ascii)
	switch {
	case bracket == "" && !allHigh:
		b.WriteString("[^\x01-\xff]")
	case bracket == "":
		b.WriteString(multibyte)
	case allHigh:
		b.WriteString("(" + bracket + "|" + multibyte + ")")
	default:
		b.WriteString(bracket)
	}
	return nil
}

// bracketExpr writes the set as a bracket expression, or "" when it is empty. ], ^ and - have
// a meaning by place inside brackets, so ] goes first, - last and ^ never first.
func bracketExpr(set [utf8.RuneSelf]bool) string {
	special := map[byte]bool{']': set[']'], '^': set['^'], '-': set['-']}
	var runs strings.Builder
	for c := 0; c < utf8.RuneSelf; c++ {
		if !set[c] || c == ']' || c == '^' || c == '-' {
			continue
		}
		end := c
		for end+1 < utf8.RuneSelf && set[end+1] && end+1 != ']' && end+1 != '^' && end+1 != '-' {
			end++
		}
		runs.WriteByte(byte(c))
		if end > c+1 {
			runs.WriteByte('-')
		}
		if end > c {
			runs.WriteByte(byte(end))
		}
		c = end
	}
	inner := runs.String()
	if special[']'] {
		inner = "]" + inner
	}
	if special['^'] {
		if inner == "" && !special['-'] {
			return `\^`
		}
		inner += "^"
	}
	if special['-'] {
		if strings.HasPrefix(inner, "^") {
			inner = "-" + inner
		} else {
			inner += "-"
		}
	}
	if inner == "" {
		return ""
	}
	return "[" + inner + "]"
}

// printfFormat writes s as a printf format that prints s: % and \ doubled, and every byte that
// is not printable ASCII as an octal escape, so the script holds plain text only.
func printfFormat(s string) string {
	var b strings.Builder
	for n := range len(s) {
		switch c := s[n]; {
		case c == '%':
			b.WriteString("%%")
		case c == '\\':
			b.WriteString(`\\`)
		case c < 0x20 || c > 0x7e:
			fmt.Fprintf(&b, `\%03o`, c)
		default:
			b.WriteByte(c)
		}
	}
	return b.String()
}
