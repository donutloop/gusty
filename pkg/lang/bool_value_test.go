package lang

import (
	"strings"
	"testing"
)

// A bool is a value: `print(True)` writes True, `str(True)` writes True, an f-string
// writes True. The answer comes from one shared question — is this expression a bool? —
// asked of the AST with the bindings that are in scope, which is why the interpreter and
// the compiled backend cannot disagree about it (roadmap L11.1 step 2, ADR 0257).
//
// The two shapes that still print the number a bool is stored as — a bool handed to a
// function, a bool inside a container — are pinned as they answer today and filed as
// roadmap Gaps R.111 and R.112.

func boolInterp(t *testing.T, src string) string {
	t.Helper()
	return captureStdout(t, src)
}

func TestBoolWritesItsNameInBothBackends(t *testing.T) {
	tests := []struct{ src, want string }{
		{"print(True)\n", "True\n"},
		{"print(False)\n", "False\n"},
		{"print(1 == 1)\n", "True\n"},
		{"print(1 != 1)\n", "False\n"},
		{"print(3 < 4)\n", "True\n"},
		{"print(3 >= 4)\n", "False\n"},
		{"print(0 == None)\n", "False\n"},
		{"print(None == None)\n", "True\n"},
		{`print("yes" == "yes")` + "\n", "True\n"},
		{`print("a" in ["a", "b"])` + "\n", "True\n"},
		{`print("z" not in ["a", "b"])` + "\n", "True\n"},
		{"print(True and False)\n", "False\n"},
		{"print(True or False)\n", "True\n"},
		{"print(not True)\n", "False\n"},
		{"print(True if 1 == 1 else False)\n", "True\n"},
		// a name holds the verdict it was last bound to, and nothing else
		{"ok = 2 < 3\nprint(ok)\n", "True\n"},
		{"ok = 2 < 3\nok = 4\nprint(ok)\n", "4\n"},
		{"flag = False\nprint(flag)\n", "False\n"},
		// str() of a bool writes the verdict, and the answer is a real string
		{"print(str(True))\n", "True\n"},
		{"print(str(True).lower())\n", "true\n"},
		// the verdict still behaves as the number it is stored as
		{"print(True + 1)\n", "2\n"},
		{"print(True * 3)\n", "3\n"},
		{"print(-True)\n", "-1\n"},
		{"print(sum([True, True, False]))\n", "2\n"},
		// an f-string interpolates the verdict, not the number
		{`print(f"{True}")` + "\n", "True\n"},
		{`ok = 1 == 1` + "\n" + `print(f"flag: {ok}")` + "\n", "flag: True\n"},
	}
	for _, tc := range tests {
		if got := boolInterp(t, tc.src); got != tc.want {
			t.Errorf("interpreter %q = %q, want %q", tc.src, got, tc.want)
		}
		if code, out := negBuildRun(t, "bool_value", tc.src); code == 0 && out != tc.want {
			t.Errorf("compiled %q = %q, want %q", tc.src, out, tc.want)
		}
	}
}

// Truthiness is unaffected: a bool is still the 0/1 the branches and loops test. The loop
// variable case is the one that can be got wrong by a name-keyed rule — a name re-bound by
// a `for` holds elements, not the verdict it was created from.
func TestBoolTruthinessIsUnchanged(t *testing.T) {
	tests := []struct{ src, want string }{
		{"if 1 == 1:\n    print(\"yes\")\n", "yes\n"},
		{"if not 0:\n    print(\"zero is falsey\")\n", "zero is falsey\n"},
		{"for x in [1, 2]:\n    if x == 2:\n        print(\"two\")\n", "two\n"},
		{"ok = True\nfor ok in [1, 2]:\n    print(ok)\n", "1\n2\n"},
		{"flag = False\nfor i in [1]:\n    flag = i == 1\n    print(flag)\n", "True\n"},
	}
	for _, tc := range tests {
		if got := boolInterp(t, tc.src); got != tc.want {
			t.Errorf("interpreter %q = %q, want %q", tc.src, got, tc.want)
		}
		if code, out := negBuildRun(t, "bool_truth", tc.src); code == 0 && out != tc.want {
			t.Errorf("compiled %q = %q, want %q", tc.src, out, tc.want)
		}
	}
}

// The answers IsBoolExpr gives, and the reason each one is that way. `1 and 2` and
// `1 or 0` are the Python rule that a boolean operator yields an operand, not a verdict;
// a ternary is a verdict only when both of its arms are; `len(x)` never answers a question.
func TestBoolQuestionIsAskedOfTheAST(t *testing.T) {
	yes := []string{
		"True", "False", "1 == 1", "1 != 2", "3 < 4", "0 == None", `"a" in ["a"]`,
		"a is None", "not x", "True and False", "True or False",
		"True if x else False", "all([True])", "any([])", "ok",
	}
	no := []string{
		"1", `"text"`, "1 and 2", "1 or 0", "x", "1.5", "None",
		"True if x else 1", "1 if x else False", "len([1])",
	}
	env := BoolEnv{Vars: map[string]bool{"ok": true}}
	for _, src := range yes {
		if !IsBoolExpr(parseExprForTest(t, "v = "+src), env) {
			t.Errorf("IsBoolExpr(%q) = false, want true", src)
		}
	}
	for _, src := range no {
		if IsBoolExpr(parseExprForTest(t, "v = "+src), env) {
			t.Errorf("IsBoolExpr(%q) = true, want false", src)
		}
	}
}

// A comparison that dispatches to a dunder is not a verdict the front end may name: the
// program's own __lt__ ran and returned an int, and CPython prints that int.
func TestDunderComparisonIsNotAVerdict(t *testing.T) {
	src := "class A:\n    def __init__(self, x):\n        self.x = x\n\n    def __lt__(self, other):\n        if self.x < other:\n            return 1\n        return 0\n\na = A(3)\nprint(a < 4)\n"
	if got, want := boolInterp(t, src), "1\n"; got != want {
		t.Errorf("interpreter dunder comparison = %q, want %q", got, want)
	}
	if code, out := negBuildRun(t, "bool_dunder", src); code == 0 && out != "1\n" {
		t.Errorf("compiled dunder comparison = %q, want %q", out, "1\n")
	}
}

// The two shapes that still print the number a bool is stored as, pinned as they answer
// today: a parameter is a fresh binding the caller's expression never travels with
// (Gap R.111), and a container element has no bool in the tag vocabulary the printer reads
// (Gap R.112).
func TestBoolThroughACallAndAContainerAreStillNumbers(t *testing.T) {
	call := "def show(f):\n    print(f)\n\nshow(1 == 1)\nshow(True)\n"
	if got, want := boolInterp(t, call), "1\n1\n"; got != want {
		t.Errorf("bool through a call = %q, want %q (Gap R.111)", got, want)
	}
	if code, out := negBuildRun(t, "bool_call", call); code == 0 && out != "1\n1\n" {
		t.Errorf("compiled bool through a call = %q, want %q (Gap R.111)", out, "1\n1\n")
	}
	container := "print([True, 1])\n"
	if got, want := boolInterp(t, container), "[1, 1]\n"; got != want {
		t.Errorf("bool in a list = %q, want %q (Gap R.112)", got, want)
	}
	if code, out := negBuildRun(t, "bool_container", container); code == 0 && !strings.Contains(out, "1") {
		t.Errorf("compiled bool in a list = %q, still a number-shaped answer (Gap R.112)", out)
	}
}
