package lang

import (
	"strings"
	"testing"
)

// Tests for Gap R.24 (roadmap), ADR 0217: a name bound inside a compound statement belongs to
// the enclosing function/module scope, because Python has one flat scope per def and per module.
// The checker analysed try bodies, handlers, `finally` clauses, `while` bodies and `match` arms
// in child scopes it then discarded, and never analysed `finally` at all, so `--check` reported
// `undefined name` and `--aot` refused programs the interpreter ran and CPython agreed with.

func scopeHasDiag(diags []Diagnostic, level Level, substr string) bool {
	for _, d := range diags {
		if d.Level == level && strings.Contains(d.Msg, substr) {
			return true
		}
	}
	return false
}

func TestBindingsInsideCompoundStatementsAreVisible(t *testing.T) {
	// Each of these is ordinary Python that prints something; the checker used to call the
	// printed name undefined, which failed `--check` and blocked `--aot` entirely.
	cases := []struct{ name, src string }{
		{"try body, every path assigns", "def f() -> int:\n    try:\n        a = 7\n    except:\n        a = 0\n    return a\nprint(f())\n"},
		{"try body only", "try:\n    t = 2\nexcept:\n    pass\nprint(t)\n"},
		{"handler body", "try:\n    pass\nexcept:\n    u = 3\nprint(u)\n"},
		{"finally body", "try:\n    pass\nexcept:\n    pass\nfinally:\n    v = 4\nprint(v)\n"},
		{"while body", "n = 0\nwhile n < 3:\n    k = 1\n    n = n + 1\nprint(k)\n"},
		{"match arms", "z = 1\nmatch z:\n    case 1:\n        m1 = 1\n    case _:\n        m1 = 2\nprint(m1)\n"},
		{"try inside while", "i = 0\nwhile i < 2:\n    try:\n        s = i\n    except:\n        s = 0\n    i = i + 1\nprint(s)\n"},
		{"finally assigns, read after", "def f() -> int:\n    try:\n        d = 1\n    finally:\n        e = 4\n    return e\nprint(f())\n"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if diags := checkDiags(t, tc.src); scopeHasDiag(diags, LevelError, "undefined name") {
				t.Fatalf("the checker refused a program that runs:\n%s\ndiags=%v", tc.src, diags)
			}
		})
	}
}

func TestFinallyBodyIsAnalysedAtAll(t *testing.T) {
	// Before this cycle the finally clause was never walked, so nothing inside it was checked:
	// a call to a function that does not exist slid through. A skipped body cannot report
	// anything, so the sharpest proof that it is analysed is that its own error appears.
	src := "try:\n    pass\nfinally:\n    print(nope_such_function())\n"
	diags := checkDiags(t, src)
	if !scopeHasDiag(diags, LevelError, "undefined name") || !scopeHasDiag(diags, LevelError, "nope_such_function") {
		t.Fatalf("`finally` bodies are not being checked, diags=%v", diags)
	}
}

func TestTrulyUndefinedNameIsStillRefused(t *testing.T) {
	// Widening visibility must not cost the checker its bite: a name no path binds is still
	// an error, in each of the shapes that got their child scopes removed.
	for _, tc := range []struct{ name, src string }{
		{"never assigned", "try:\n    pass\nexcept:\n    pass\nprint(definitely_not_bound)\n"},
		{"assigned only in a try inside a func", "def f() -> int:\n    try:\n        q = 1\n    except:\n        pass\n    return never_bound_here\n"},
		{"misspelled after a loop", "n = 0\nwhile n < 2:\n    k = n\n    n = n + 1\nprint(kk)\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if diags := checkDiags(t, tc.src); !scopeHasDiag(diags, LevelError, "undefined name") {
				t.Fatalf("a name nothing binds should still be an error, diags=%v", diags)
			}
		})
	}
}

func TestPartialBindingIsPossiblyUnboundNotUndefined(t *testing.T) {
	// Inside a function, a name assigned on only some paths is visible but not certain; the
	// honest diagnostic is `possibly unbound`, not `undefined name`.
	src := "def f(x):\n    match x:\n        case 1:\n            pass\n        case y:\n            pass\n    return y\n"
	diags := checkDiags(t, src)
	if scopeHasDiag(diags, LevelError, "undefined name") {
		t.Fatalf("a name some path binds is not undefined, diags=%v", diags)
	}
	if !scopeHasDiag(diags, LevelWarning, "possibly unbound") {
		t.Fatalf("y is bound by one arm only, the use should be flagged, diags=%v", diags)
	}
}

func TestNameBoundInEveryArmIsCertain(t *testing.T) {
	// The counterpart: with both arms binding it, the same use must produce no warning at all.
	src := "def f(x):\n    match x:\n        case 1:\n            y = 1\n        case _:\n            y = 2\n    return y\n"
	if diags := checkDiags(t, src); scopeHasDiag(diags, LevelWarning, "possibly unbound") {
		t.Fatalf("y is assigned on every path, diags=%v", diags)
	}
}
