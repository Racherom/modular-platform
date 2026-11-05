package render

import (
	"errors"
	"os/exec"
	"regexp"
	"testing"
)

// shellMatch runs the check setup.sh runs: the printf format to an ERE, then grep -E in the C
// locale against one answer.
func shellMatch(t *testing.T, format, answer string) bool {
	t.Helper()
	cmd := exec.Command("sh", "-c", `re=$(printf "$1"); printf '%s\n' "$2" | LC_ALL=C grep -Eq -e "$re"`, "sh", format, answer)
	err := cmd.Run()
	var exit *exec.ExitError
	switch {
	case err == nil:
		return true
	case errors.As(err, &exit) && exit.ExitCode() == 1:
		return false
	}
	t.Fatalf("grep with %q: %v", format, err)
	return false
}

// TestEREPatternMatchesRE2 checks that setup.sh accepts exactly the answers the module's RE2
// pattern accepts (spec 0013, Install time, step 2).
func TestEREPatternMatchesRE2(t *testing.T) {
	if _, err := exec.LookPath("grep"); err != nil {
		t.Skip("no grep")
	}
	cases := map[string][]string{
		`[^@\s]+@[^@\s]+`:           {"a@b.test", "admin@example.org", "a b@c", "@b", "a@", "a@b@c", "ä@ö.de", "a\t@b", ""},
		`[a-z][a-z0-9-]{0,62}`:      {"a", "web-1", "1web", "Web", "a-", ""},
		`(?i)hello|bye`:             {"HeLLo", "bye", "BYE", "hello!", "hellobye"},
		`a.c`:                       {"abc", "a.c", "aäc", "ac", "abbc"},
		`[]^-]+`:                    {"]", "^", "-", "]^-", "a", "^a"},
		`\^|\$|\.|\\|\[x\]|a\{2\}`:  {"^", "$", ".", `\`, "[x]", "a{2}", "aa", "x"},
		`[\^]`:                      {"^", "a"},
		`[-^]`:                      {"-", "^", "a"},
		`100%|50%`:                  {"100%", "50%", "100"},
		`(ab)*c?`:                   {"", "ab", "ababc", "c", "abca"},
		`x{2,3}y{2,}`:               {"xxyy", "xxxyyyy", "xyy", "xxxxyy", "xxy"},
		`\d+(\.\d+)?`:               {"1", "1.5", "1.", ".5", "١"},
		`[[:alpha:]_]\w*`:           {"_a1", "a", "1a", "a-b"},
		`[^a]`:                      {"b", "a", "ä", "bb", "€", "𝄞"},
		`.+`:                        {"x", "日本", ""},
		`grün|[äöü]?x`:              {"grün", "grun", "x"},
		`https://[a-z.]+(:\d+)?/.*`: {"https://a.test/", "https://a.test:8443/x?y", "http://a.test/", "https://a.test"},
	}
	for pattern, answers := range cases {
		format, err := erePattern(pattern)
		if err != nil {
			if pattern == `grün|[äöü]?x` {
				continue // a class of some non ASCII letters is refused; checked below
			}
			t.Fatalf("%s: %v", pattern, err)
		}
		re := regexp.MustCompile(`^(?:` + pattern + `)$`)
		for _, a := range answers {
			if got, want := shellMatch(t, format, a), re.MatchString(a); got != want {
				t.Errorf("pattern %s, answer %q: setup.sh accepts it %v, the planner %v (format %s)", pattern, a, got, want, format)
			}
		}
	}
}

func TestEREPatternRefusesWhatERECannotSay(t *testing.T) {
	for _, pattern := range []string{`\bword\b`, `[äöü]`, `a{0}`, "a\nb"} {
		if _, err := erePattern(pattern); !errors.Is(err, ErrNotRendered) {
			t.Errorf("%q: err %v, want ErrNotRendered", pattern, err)
		}
	}
}
