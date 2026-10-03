package integration

import (
	"strings"
	"testing"
)

// integration/render_pair_test.go — roadmap L11.2, ADR 0258 (closes Gap L.2).
//
// str() and repr() are one pair: one renderer per backend, asked two questions. The pkg/lang table
// drives the forms through both engines; this file drives them through the CLI, which is what an
// agent consumes — the human legs (--interp / --aot), the machine legs (--json, --oracle), and the
// exit codes each of them answers with.
//
// The expectations are CPython's answers, taken from python3 (the --oracle leg checks the same
// thing from the other side). Neither backend is allowed to be its own oracle: the defect this
// row closed was print printing [1, 2] while str() answered 0, and both of those were self-agreed.

// pairForms is the table every leg below walks. want is CPython's answer.
var pairForms = []struct {
	name, src, want string
}{
	{"an int list in a variable", "xs = [1, 2]\nprint(str(xs))\n", "[1, 2]\n"},
	{"an int list under repr", "xs = [1, 2]\nprint(repr(xs))\n", "[1, 2]\n"},
	{"a text list quotes its elements", "xs = [\"a\"]\nprint(str(xs))\n", "['a']\n"},
	{"a list that mixes kinds", "xs = [\"a\", 1]\nprint(str(xs))\n", "['a', 1]\n"},
	{"a nested list", "xs = [[1, 2], [3]]\nprint(str(xs))\n", "[[1, 2], [3]]\n"},
	{"a dict", "d = {\"a\": 1}\nprint(str(d))\n", "{'a': 1}\n"},
	{"a dict under repr", "d = {1: \"a\"}\nprint(repr(d))\n", "{1: 'a'}\n"},
	{"a set", "print(str({1}))\n", "{1}\n"},
	{"the empty set", "s = set()\nprint(str(s))\n", "set()\n"},
	{"the empty set written inline", "print(str(set()))\n", "set()\n"},
	{"the empty list written inline", "print(str(list()))\n", "[]\n"},
	{"the empty list", "print(str([]))\n", "[]\n"},
	{"a float keeps its .0", "x = 2.0\nprint(str(x))\n", "2.0\n"},
	{"None", "print(str(None))\n", "None\n"},
	{"a verdict", "print(str(True))\n", "True\n"},
	{"a verdict under repr", "print(repr(False))\n", "False\n"},
	{"a text writes itself under str", "print(str(\"hi\"))\n", "hi\n"},
	{"a text writes its quoting under repr", "print(repr(\"hi\"))\n", "'hi'\n"},
	{"a number the compiler cannot read", "n = 42\nprint(repr(n))\n", "42\n"},
	{"a rendering used as a text", "xs = [1, 2]\ns = str(xs)\nprint(s.upper())\n", "[1, 2]\n"},
	{"a double rendered by the pair", "xs = []\nxs.append(6)\nprint(str(xs[0] / 2))\n", "3.0\n"},
}

// TestCLIPairWritesWhatCPythonWrites drives each form through the human legs and, where a Python is
// installed, through CPython itself.
func TestCLIPairWritesWhatCPythonWrites(t *testing.T) {
	for _, tc := range pairForms {
		t.Run(tc.name, func(t *testing.T) {
			path := writeSrc(t, t.TempDir(), "pair.gy", tc.src)
			if out := cliRun(t, "--interp", "--file", path); out != tc.want {
				t.Errorf("interpreter leg wrote %q, want CPython's %q\nsource: %s", out, tc.want, tc.src)
			}
			out, code := cliRunCode(t, "--aot", "--file", path)
			if code == 2 {
				t.Fatalf("the compiled leg rejected the compiler's own module (ADR 0166): %s", out)
			}
			if code != 0 {
				t.Fatalf("compiled leg exited %d: %s\nsource: %s", code, out, tc.src)
			}
			if out != tc.want {
				t.Errorf("compiled leg wrote %q, want CPython's %q\nsource: %s", out, tc.want, tc.src)
			}
			if py, ok := cpythonOut(t, path); ok && py != tc.want {
				t.Errorf("the pinned answer %q is not CPython's %q — the table drifted from the oracle\nsource: %s", tc.want, py, tc.src)
			}
		})
	}
}

// TestDoubleIntoThePairIsRendered pins the program integration/slot_division_test.go used to keep
// as a refusal. A double could not travel into str() while str() was a number-formatter that took
// an i32; the pair pointed the same float renderer print uses at a capture buffer, so the double
// arrives as the text a str() argument is anyway. CPython writes 3.0, and so do both legs.
func TestDoubleIntoThePairIsRendered(t *testing.T) {
	const src = "xs = []\nxs.append(6)\nprint(str(xs[0] / 2))\n"
	path := writeSrc(t, t.TempDir(), "pair_double.gy", src)
	if out := cliRun(t, "--interp", "--file", path); out != "3.0\n" {
		t.Errorf("interpreter wrote %q, want 3.0", out)
	}
	out, code := cliRunCode(t, "--aot", "--file", path)
	if code != 0 || out != "3.0\n" {
		t.Errorf("compiled leg wrote %q exit %d, want 3.0 exit 0", out, code)
	}
	if py, ok := cpythonOut(t, path); ok && py != "3.0\n" {
		t.Errorf("CPython wrote %q — the pinned answer is wrong", py)
	}
}

// TestJSONReportsThePair is the machine leg: an agent rendering a value asks for it as JSON and gets
// the text and the type it renders as, from either backend, without scraping stdout.
func TestJSONReportsThePair(t *testing.T) {
	for _, tc := range []struct {
		expr, result, typ string
	}{
		{"str([1, 2])", "[1, 2]", "str"},
		{"repr([1, 2])", "[1, 2]", "str"},
		{"repr(\"hi\")", "'hi'", "str"},
		{"str(\"hi\")", "hi", "str"},
		{"str(set())", "set()", "str"},
		{"str(None)", "None", "str"},
		{"str(True)", "True", "str"},
		{"str(1.5)", "1.5", "str"},
	} {
		out, code := cliRunCode(t, "--json", "--eval", tc.expr)
		if code != 0 {
			t.Fatalf("--json --eval %s exited %d: %s", tc.expr, code, out)
		}
		if !strings.Contains(out, `"result": "`+tc.result+`"`) {
			t.Errorf("--json --eval %s did not report %q: %s", tc.expr, tc.result, out)
		}
		if !strings.Contains(out, `"type": "`+tc.typ+`"`) {
			t.Errorf("--json --eval %s did not report type %q: %s", tc.expr, tc.typ, out)
		}
		if !strings.Contains(out, `"backend": "interpreter"`) {
			t.Errorf("--json --eval %s did not name the backend: %s", tc.expr, out)
		}
	}
}

// TestPairAgreesWithTheOracle asks CPython whether the pair is right, through the leg whose whole
// job is that question. A program the pair renders correctly is a match (exit 0); a form the pair
// cannot name yet is still reported, not swallowed.
func TestPairAgreesWithTheOracle(t *testing.T) {
	const agreeSrc = "xs = [1, 2]\nprint(str(xs))\nprint(repr(xs))\nprint(str([]))\nprint(repr(\"hi\"))\nprint(str(set()))\nprint(str(None))\nprint(repr(True))\nprint(str(1.5))\nprint(str([True, 1]))\nprint(repr([True, False]))\nprint(str({\"k\": True}))\n"
	out, code := cliRunCode(t, "--oracle", agreeSrc)
	if code == 2 {
		t.Fatalf("the oracle leg rejected the compiler's own module (ADR 0166): %s", out)
	}
	if code != 0 {
		t.Fatalf("the oracle called a matching pair program a divergence (exit %d):\n%s", code, out)
	}

	// A bool the program hands to a function is Gap R.111's, not this row's: the pair renders the
	// argument, and the parameter holds the 1 the caller's verdict was made from because no tag
	// travels across the call. The oracle must still say so — the shape this row used to pin, a bool
	// inside a container, is the one ADR 0259 closed, so it now belongs to the agreeing leg above.
	const boolThroughACall = "def show(f):\n    print(str(f))\n\nshow(True)\n"
	out, code = cliRunCode(t, "--oracle", boolThroughACall)
	if code != 6 {
		t.Errorf("the oracle scored a bool through a call as %d, want 6 (a divergence, Gap R.111):\n%s", code, out)
	}
}

// TestPairRefusalIsWrittenForTheAgent covers the machine path of the boundary: when a form cannot
// be named, the CLI refuses with exit 1 and says which half is missing, on stderr, in the same
// words for both halves. Exit 2 — the compiler rejecting its own module — is never acceptable here.
func TestPairRefusalIsWrittenForTheAgent(t *testing.T) {
	for _, src := range []string{
		"print(str((1, 2)))\n",
		"print(repr((1, 2)))\n",
	} {
		path := writeSrc(t, t.TempDir(), "pair_refuse.gy", src)
		combined := cliRun(t, "--aot", "--file", path)
		_, code := cliRunCode(t, "--aot", "--file", path)
		if code == 2 {
			t.Fatalf("the compiled leg rejected the compiler's own module (ADR 0166):\n%s", combined)
		}
		if code != 1 {
			t.Fatalf("exit %d, want 1 (a front-end refusal):\n%s", code, combined)
		}
		if !strings.Contains(combined, "is not implemented") && !strings.Contains(combined, "unsupported expression") {
			t.Errorf("refusal does not name the missing half:\n%s", combined)
		}
		for _, bad := range []string{"LLVM ERROR", "verifier", "Instruction does not dominate"} {
			if strings.Contains(combined, bad) {
				t.Errorf("refusal arrived as an IR problem (%s):\n%s", bad, combined)
			}
		}
	}
}
