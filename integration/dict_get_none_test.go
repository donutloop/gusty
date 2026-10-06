package integration

// Gap R.174 / ADR 0291 at the CLI. `d.get(key)` with the key absent hands back None, and this front end
// answered with the number 0 on the interpreter while the compiled leg refused the program outright
// (`get: key not found and no default`). Both are the bare-word zero: a void leaving a builtin was written
// `return 0, nil`, the same representation Gap R.171 caught leaving a function body. The third defect lived
// in the refusal — a name bound to a dict reached the string-method road and was told it was a
// "non-constant string", which describes a program the author never wrote (Gap R.38).

import (
	"strings"
	"testing"
)

func TestDictGetAnswersNoneAtTheCLI(t *testing.T) {
	dir := t.TempDir()
	rows := []struct {
		name string
		src  string
		want string
	}{
		{"key present", "print({\"a\": 1}.get(\"a\"))\n", "1\n"},
		{"key absent", "print({\"a\": 1}.get(\"z\"))\n", "None\n"},
		{"absent with a default", "print({\"a\": 1}.get(\"z\", 42))\n", "42\n"},
		{"int key absent", "print({1: \"x\"}.get(9))\n", "None\n"},
		{"empty dict", "print({}.get(\"a\"))\n", "None\n"},
		{"compares to None", "v = {\"a\": 1}.get(\"z\")\nprint(v == None)\n", "True\n"},
		{"does not equal zero", "v = {\"a\": 1}.get(\"z\")\nprint(v == 0)\n", "False\n"},
		{"falsey as a test", "if {\"a\": 1}.get(\"z\"):\n    print(\"t\")\nelse:\n    print(\"f\")\n", "f\n"},
		{"as a branch", "if {\"a\": 1}.get(\"a\") == None:\n    print(\"absent\")\nelse:\n    print(\"there\")\n", "there\n"},
		{"default wins", "print({\"a\": 1}.get(\"b\", 9))\n", "9\n"},
	}
	for _, r := range rows {
		r := r
		t.Run(r.name, func(t *testing.T) {
			want, ok := cpythonPlainOut(t, dir, r.src)
			if !ok {
				t.Skip("no python3 available to cross-check")
			}
			if want != r.want {
				t.Fatalf("row is stale: python3 prints %q, row pins %q", want, r.want)
			}
			for _, engine := range cliEngines {
				p := writeSrc(t, dir, "dget", r.src)
				out, code := cliRunMerged(t, engine, "--file", p)
				if code == 2 {
					t.Fatalf("%s exited 2 (ADR 0166's compiler-bug code): %s", engine, out)
				}
				if code != 0 {
					t.Fatalf("%s exited %d on a program the reference prints %q: %s", engine, code, want, out)
				}
				if out != want {
					t.Errorf("%s printed %q, want %q — a void printed as its word is Gap R.174", engine, out, want)
				}
			}
		})
	}
}

// TestARefusalAboutADictCallsItADict is the Gap R.38 half through the shipped binary. The compiled leg may
// decline a container method over a NAME — a name's slots belong to the runtime — but it may not call the
// dict a string while doing so.
func TestARefusalAboutADictCallsItADict(t *testing.T) {
	dir := t.TempDir()
	for _, src := range []string{
		"d = {\"a\": 1}\nprint(d.keys())\n",
		"d = {\"a\": 1}\nprint(d.get(\"a\"))\n",
	} {
		src := src
		t.Run(strings.TrimSpace(src), func(t *testing.T) {
			p := writeSrc(t, dir, "dictrefuse", src)
			out, code := cliRunMerged(t, "--aot", "--file", p)
			if code == 2 {
				t.Fatalf("exited 2 (ADR 0166's compiler-bug code) rather than refusing: %s", out)
			}
			if code == 0 {
				return // answered: an answer is never worse than a refusal
			}
			for _, banned := range []string{"non-constant string", "not a text"} {
				if strings.Contains(out, banned) {
					t.Errorf("the refusal calls a dict a string (`%s`): %s", banned, out)
				}
			}
			if !strings.Contains(out, "container") {
				t.Errorf("the refusal does not name what the program holds: %s", out)
			}
		})
	}
}
