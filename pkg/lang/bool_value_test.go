package lang

import (
	"testing"
)

// A bool is a value: `print(True)` writes True, `str(True)` writes True, an f-string
// writes True. The answer comes from one shared question — is this expression a bool? —
// asked of the AST with the bindings that are in scope, which is why the record and
// the compiled backend cannot disagree about it (roadmap L11.1 step 2, ADR 0257).
//
// The two shapes that still print the number a bool is stored as — a bool handed to a
// function, a bool inside a container — are pinned as they answer today and filed as
// roadmap Gaps R.111 and R.112.

func boolRun(t *testing.T, src string) string {
	t.Helper()
	return captureStdout(t, src)
}

func TestBoolWritesItsNameOnTheCompiledBackend(t *testing.T) {
	t.Parallel()
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
		if got := boolRun(t, tc.src); got != tc.want {
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
	t.Parallel()
	tests := []struct{ src, want string }{
		{"if 1 == 1:\n    print(\"yes\")\n", "yes\n"},
		{"if not 0:\n    print(\"zero is falsey\")\n", "zero is falsey\n"},
		{"for x in [1, 2]:\n    if x == 2:\n        print(\"two\")\n", "two\n"},
		{"ok = True\nfor ok in [1, 2]:\n    print(ok)\n", "1\n2\n"},
		{"flag = False\nfor i in [1]:\n    flag = i == 1\n    print(flag)\n", "True\n"},
	}
	for _, tc := range tests {
		if got := boolRun(t, tc.src); got != tc.want {
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
	t.Parallel()
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
	t.Parallel()
	src := "class A:\n    def __init__(self, x):\n        self.x = x\n\n    def __lt__(self, other):\n        if self.x < other:\n            return 1\n        return 0\n\na = A(3)\nprint(a < 4)\n"
	if got, want := boolRun(t, src), "1\n"; got != want {
		t.Errorf("interpreter dunder comparison = %q, want %q", got, want)
	}
	if code, out := negBuildRun(t, "bool_dunder", src); code == 0 && out != "1\n" {
		t.Errorf("compiled dunder comparison = %q, want %q", out, "1\n")
	}
}

// A verdict handed to a function writes its name on the compiled backend, and a position the call sites
// contradict keeps the number: the front end reads a parameter's callers before its body is emitted, and
// marks a position only when no call site disagrees about it (Gap R.111, ADR 0348; the declined half is
// Gap R.270 and the shapes `integration/bool_call_boundary_test.go` pins as declines).
//
// Both halves read the COMPILED answer directly (`negBuildRun`), not through `captureStdout`: the first
// program is in `testdata/interpreter-golden.json` with the retired engine's `1\n1\n`, and a record hit
// makes that helper return the record and skip — a test that silently skips over the exact case it exists
// to pin is how a closed gap stays red in the ledger and green in the suite. The record's side of the
// disagreement is registered in `testdata/interpreter-golden-drift.json`, and CPython is the definition
// (AGENTS.md: where the record and the reference disagree, the record is the debt).
//
// The container shape this test used to pin is closed: a slot now carries a bool tag of its own, and
// programs/probe_bool_in_a_container.gy pins CPython's answer (Gap R.112, ADR 0259).
func TestAVerdictAcrossACallWritesItsNameAndAContradictedOneDoesNot(t *testing.T) {
	t.Parallel()
	closed := "def show(f):\n    print(f)\n\nshow(1 == 1)\nshow(True)\n"
	if code, out := negBuildRun(t, "bool_call_closed", closed); code != 0 || out != "True\nTrue\n" {
		t.Errorf("compiled bool through a call = %q (exit %d), want %q (Gap R.111)", out, code, "True\nTrue\n")
	}
	contradicted := "def show(f):\n    print(f)\n\nshow(True)\nshow(1)\n"
	if code, out := negBuildRun(t, "bool_call_contradicted", contradicted); code != 0 || out != "1\n1\n" {
		t.Errorf("a contradicted parameter position = %q (exit %d), want %q (Gap R.270)", out, code, "1\n1\n")
	}
}
