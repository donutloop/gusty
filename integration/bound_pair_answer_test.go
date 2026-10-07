package integration

// The CLI half of "a number the body computed out of its own parameter keeps the kind the argument
// arrived with" (roadmap L11.6, Gap R.169, ADR 0285).
//
// Before this cycle the compiled leg printed a truncated integer at exit 0 for each row below — `1` for
// `1.1`, `0` for `0.2`, `-1` for `-0.9` — because the scan could not see that a returned local's kind came
// from a parameter: a parameter is written by the caller, so no assignment in the body records it, the
// body's answer direction closed, and the one-word return road took the body.
//
// Exit 2 is forbidden (ADR 0166). Exit 0 with the wrong digit is the failure each row exists to catch.

import (
	"strings"
	"testing"
)

func TestABoundPairAnswerAtTheCLI(t *testing.T) {
	for _, tc := range []struct{ name, src, want string }{
		{"add one, bind, return", "def f(x):\n    y = x + 1\n    return y\n\nprint(f(0.1))\n", "1.1\n"},
		{"double it, bind, return", "def f(x):\n    y = x * 2\n    return y\n\nprint(f(0.1))\n", "0.2\n"},
		{"subtract one, bind, return", "def f(x):\n    y = x - 1\n    return y\n\nprint(f(0.1))\n", "-0.9\n"},
		{"the int side of the same body", "def f(x):\n    y = x + 1\n    return y\n\nprint(f(2))\n", "3\n"},
		{"one body, both arguments", "def f(x):\n    y = x * 2\n    return y\n\nprint(f(2.5))\nprint(f(3))\n", "5.0\n6\n"},
		{"arms inside the binding", "def f(v):\n    y = (v - 1) * 2\n    return y\n\nprint(f(2.5))\nprint(f(3))\n", "3.0\n4\n"},
		{"a float default, bound", "def f(x=2.5):\n    y = x * 2\n    return y\n\nprint(f())\n", "5.0\n"},
		{"a call answer bound (ADR 0280's row)", "def d(v):\n    return v * 2\n\ndef f(x):\n    y = d(x)\n    return y\n\nprint(f(2.5))\n", "5.0\n"},
		{"an int-only callee is untouched", "def f(x):\n    y = x + 1\n    return y\n\nprint(f(10))\n", "11\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			gy := writeSrc(t, dir, "bound.gy", tc.src)
			py, ok := cpythonPlainOut(t, dir, tc.src)
			if !ok {
				t.Fatalf("the reference was expected to answer this program\nsrc: %s", tc.src)
			}
			if py != tc.want {
				t.Fatalf("the pinned expectation is not the reference's: cpython %q, table %q", py, tc.want)
			}
			for _, engine := range cliEngines {
				out, code := cliRunMerged(t, engine, "--file", gy)
				if code == 2 {
					t.Fatalf("%s: exit 2, the contract's compiler-bug code (ADR 0166):\n%s", engine, out)
				}
				if code != 0 {
					t.Fatalf("%s exited %d on a program the reference answers:\n%s", engine, code, out)
				}
				if out != tc.want {
					t.Fatalf("%s printed %q, want %q — a truncated digit is Gap R.169 back\n(cpython agrees: %q)", engine, out, tc.want, py)
				}
			}
		})
	}
}

// TestABoundPairAnswerRefusalIsHonestAtTheCLI pins what the row still owes: these shapes answered a wrong
// number at exit 0 and now exit 1 with a sentence about the program the reader is actually holding.
func TestABoundPairAnswerRefusalIsHonestAtTheCLI(t *testing.T) {
	for _, tc := range []struct {
		name, src, want string
		not             []string
	}{
		{
			name: "a name bound straight from a parameter",
			src:  "def f(x):\n    y = x\n    return y\n\nprint(f(0.1))\n", want: "parameter the call site handed a double",
			not: []string{"slot the program built at run time", "another arm", "plain integer"},
		},
		{
			name: "arithmetic over a parameter under a condition",
			src:  "def f(x):\n    if x > 0:\n        y = x + 1\n        return y\n\n    return 0\n\nprint(f(0.5))\n", want: "arithmetic over a parameter the caller supplied",
			not: []string{"slot the program built at run time"},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			gy := writeSrc(t, dir, "bound_refuse.gy", tc.src)
			out, code := cliRunMerged(t, "--aot", "--file", gy)
			if code == 2 {
				t.Fatalf("--aot: exit 2 on an ordinary program (ADR 0166):\n%s", out)
			}
			if code == 0 {
				t.Fatalf("--aot answered %q; this shape is owed a tagged return word, so an answer at exit 0 is the wrong number Gap R.169 was measured on\nsrc: %s", out, tc.src)
			}
			if code != 1 {
				t.Fatalf("--aot: exit %d, want the front-end refusal's 1\n%s", code, out)
			}
			if !strings.Contains(out, tc.want) {
				t.Errorf("the refusal must name %q, got:\n%s", tc.want, out)
			}
			for _, banned := range tc.not {
				if strings.Contains(out, banned) {
					t.Errorf("the refusal must not claim %q — a statement about a program the reader cannot find (Gap R.38):\n%s", banned, out)
				}
			}
		})
	}
}
