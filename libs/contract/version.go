package contract

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

var (
	versionRe = regexp.MustCompile(`^[0-9]+(\.[0-9]+){0,3}$`)
	formatRe  = regexp.MustCompile(`^1\.(0|[1-9][0-9]{0,8})$`)
)

// FormatMinor is the highest minor of format 1 this release reads (spec 0011).
const FormatMinor = 0

// Version is a dotted version like 18.6, compared part by part as numbers (spec 0012).
type Version []int

// ParseVersion reads a version of one to four dotted numbers.
func ParseVersion(s string) (Version, bool) {
	if !versionRe.MatchString(s) {
		return nil, false
	}
	parts := strings.Split(s, ".")
	v := make(Version, len(parts))
	for i, p := range parts {
		n, err := strconv.Atoi(p)
		if err != nil {
			return nil, false
		}
		v[i] = n
	}
	return v, true
}

// Compare returns -1, 0 or +1; a missing part counts as 0, so 18 equals 18.0.
func (v Version) Compare(w Version) int {
	for i := range max(len(v), len(w)) {
		a, b := part(v, i), part(w, i)
		switch {
		case a < b:
			return -1
		case a > b:
			return 1
		}
	}
	return 0
}

func part(v Version, i int) int {
	if i < len(v) {
		return v[i]
	}
	return 0
}

type clause struct {
	op string
	v  Version
}

// Range is a version range like `>=15, <19` (spec 0012). The empty range holds every version.
type Range struct {
	clauses []clause
}

// ParseRange reads one or more clauses joined by commas, each an operator >=, >, <=, < or =
// followed by a version. It refuses a range no version can satisfy.
func ParseRange(s string) (Range, error) {
	var r Range
	if strings.TrimSpace(s) == "" {
		return r, nil
	}
	for c := range strings.SplitSeq(s, ",") {
		c = strings.TrimSpace(c)
		op := ""
		for _, o := range []string{">=", "<=", ">", "<", "="} {
			if strings.HasPrefix(c, o) {
				op = o
				break
			}
		}
		if op == "" {
			return Range{}, fmt.Errorf("clause %q does not start with >=, >, <=, < or =", c)
		}
		text := strings.TrimSpace(strings.TrimPrefix(c, op))
		v, ok := ParseVersion(text)
		if !ok {
			return Range{}, fmt.Errorf("clause %q does not hold a version like 15 or 18.6", c)
		}
		r.clauses = append(r.clauses, clause{op: op, v: v})
	}
	if !r.satisfiable() {
		return Range{}, fmt.Errorf("no version can satisfy %q", s)
	}
	return r, nil
}

// Contains reports whether v is inside the range: every clause holds.
func (r Range) Contains(v Version) bool {
	for _, c := range r.clauses {
		cmp := v.Compare(c.v)
		ok := false
		switch c.op {
		case ">=":
			ok = cmp >= 0
		case ">":
			ok = cmp > 0
		case "<=":
			ok = cmp <= 0
		case "<":
			ok = cmp < 0
		case "=":
			ok = cmp == 0
		}
		if !ok {
			return false
		}
	}
	return true
}

// satisfiable reports whether some version is inside the range: an equal clause must satisfy
// every clause, else the tightest lower bound must sit below the tightest upper bound.
func (r Range) satisfiable() bool {
	var lo, hi *clause
	for i := range r.clauses {
		c := &r.clauses[i]
		switch c.op {
		case "=":
			return r.Contains(c.v)
		case ">=", ">":
			if lo == nil || c.v.Compare(lo.v) > 0 || (c.v.Compare(lo.v) == 0 && c.op == ">") {
				lo = c
			}
		case "<=", "<":
			if hi == nil || c.v.Compare(hi.v) < 0 || (c.v.Compare(hi.v) == 0 && c.op == "<") {
				hi = c
			}
		}
	}
	if lo == nil || hi == nil {
		return true
	}
	switch cmp := lo.v.Compare(hi.v); {
	case cmp < 0:
		return true
	case cmp == 0:
		return lo.op == ">=" && hi.op == "<="
	}
	return false
}

// checkFormat reads a formatVersion: ok when it is empty or 1.<minor> with a minor this release
// reads; tooNew names the version needed when the minor is higher.
func checkFormat(s string) (ok, tooNew bool) {
	if s == "" {
		return true, false
	}
	m := formatRe.FindStringSubmatch(s)
	if m == nil {
		return false, false
	}
	minor, _ := strconv.Atoi(m[1])
	if minor > FormatMinor {
		return false, true
	}
	return true, false
}
