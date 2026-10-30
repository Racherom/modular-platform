package envfile_test

import (
	"slices"
	"strings"
	"testing"

	"github.com/racherom/modular-platform/sdk/envfile"
)

func TestParse(t *testing.T) {
	t.Parallel()
	for name, tc := range map[string]struct {
		data  string
		lines []envfile.Line
		errs  []int // lines that break the syntax
	}{
		"empty":                {data: ""},
		"blank and comments":   {data: "\n   \n# a comment\n  # indented\n"},
		"key value":            {data: "A=1\nB_2=two words\n", lines: []envfile.Line{{"A", "1", 1}, {"B_2", "two words", 2}}},
		"no trailing newline":  {data: "A=1", lines: []envfile.Line{{"A", "1", 1}}},
		"crlf":                 {data: "A=1\r\nB=2\r\n", lines: []envfile.Line{{"A", "1", 1}, {"B", "2", 2}}},
		"empty value":          {data: "A=\n", lines: []envfile.Line{{"A", "", 1}}},
		"double quotes":        {data: `A="x y"` + "\n", lines: []envfile.Line{{"A", "x y", 1}}},
		"single quotes":        {data: `A='x\ny'` + "\n", lines: []envfile.Line{{"A", `x\ny`, 1}}},
		"unmatched quotes":     {data: `A="x'` + "\n", lines: []envfile.Line{{"A", `"x'`, 1}}},
		"one quote":            {data: `A="` + "\n", lines: []envfile.Line{{"A", `"`, 1}}},
		"equals in value":      {data: "A=b=c\n", lines: []envfile.Line{{"A", "b=c", 1}}},
		"no inline comment":    {data: "A=1 # two\n", lines: []envfile.Line{{"A", "1 # two", 1}}},
		"no expansion":         {data: "A=${B}\n", lines: []envfile.Line{{"A", "${B}", 1}}},
		"no equals":            {data: "A\nB=1\n", lines: []envfile.Line{{"B", "1", 2}}, errs: []int{1}},
		"lower case key":       {data: "a=1\n", errs: []int{1}},
		"export":               {data: "export A=1\n", errs: []int{1}},
		"space before equals":  {data: "A =1\n", errs: []int{1}},
		"indented key":         {data: " A=1\n", errs: []int{1}},
		"digit first":          {data: "1A=1\n", errs: []int{1}},
		"key twice":            {data: "A=1\nA=2\n", lines: []envfile.Line{{"A", "1", 1}}, errs: []int{2}},
		"not utf 8":            {data: "A=\xff\nB=1\n", lines: []envfile.Line{{"B", "1", 2}}, errs: []int{1}},
		"several broken lines": {data: "x\nA=1\ny\n", lines: []envfile.Line{{"A", "1", 2}}, errs: []int{1, 3}},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			lines, errs := envfile.Parse([]byte(tc.data))
			if !slices.Equal(lines, tc.lines) {
				t.Errorf("lines = %+v, want %+v", lines, tc.lines)
			}
			var at []int
			for _, e := range errs {
				at = append(at, e.Line)
			}
			if !slices.Equal(at, tc.errs) {
				t.Errorf("broken lines = %v (%v), want %v", at, errs, tc.errs)
			}
		})
	}
}

// TestParseNeverRepeatsAValue keeps a value, which may be a secret, out of every message.
func TestParseNeverRepeatsAValue(t *testing.T) {
	t.Parallel()
	_, errs := envfile.Parse([]byte("A=hunter2\nA=hunter2\nhunter2\n"))
	if len(errs) != 2 {
		t.Fatalf("errs = %v, want 2", errs)
	}
	for _, e := range errs {
		if strings.Contains(e.Error(), "hunter2") {
			t.Errorf("message repeats the value: %q", e.Error())
		}
	}
}
