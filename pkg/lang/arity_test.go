package lang

import (
	"strings"
	"testing"
)

// Arity is a contract, and the checker enforced only one half of it: a call with too many
// arguments was refused, a call with too few was not. The unfilled parameter was simply left
// unbound, so the program got blamed later — an `undefined name` inside the function it had just
// called, or a backend that answered differently (roadmap Gap R.10, ADR 0201).

func arityDiags(t *testing.T, src string) []Diagnostic {
	t.Helper()
	prog, err := Parse(src)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	return Analyze(prog)
}

func TestTooFewArgumentsIsRefused(t *testing.T) {
	diags := arityDiags(t, "def build(a, b):\n    return a\n\nprint(build(1))\n")
	if !hasMsg(diags, `expects 2 arguments, got 1`) {
		t.Errorf("a dropped argument was not reported: %v", diags)
	}
	if hasMsg(diags, "undefined name") {
		t.Errorf("the arity mistake was also blamed on the callee's own source: %v", diags)
	}
}

// TestOneErrorPerArityMistake: the derived diagnostics were the reason this gap stayed
// invisible — one real problem arrived as three, two of them pointing at the callee.
func TestOneErrorPerArityMistake(t *testing.T) {
	diags := arityDiags(t, "def build(a, b, c):\n    return a\n\nprint(build(1))\n")
	n := 0
	for _, d := range diags {
		if d.Level == LevelError {
			n++
		}
	}
	if n != 1 {
		t.Errorf("want exactly one error for one arity mistake, got %d: %v", n, diags)
	}
}

func TestKeywordCallNamesTheMissingParameter(t *testing.T) {
	diags := arityDiags(t, "def build(a, b):\n    return a\n\nprint(build(a=1))\n")
	if !hasMsg(diags, `missing argument "b"`) {
		t.Errorf("a keyword call that skipped a parameter should name it: %v", diags)
	}
}

func TestTooManyArgumentsNamesTheFunction(t *testing.T) {
	diags := arityDiags(t, "def build(a):\n    return a\n\nprint(build(1, 2))\n")
	if !hasMsg(diags, `function "build" accepts 1 argument`) {
		t.Errorf("the too-many message should say which call and what it accepts: %v", diags)
	}
}

// TestLegitimateShortCallsAreRefusedByNothing is the over-refusal guard: a default exists precisely
// so a call may pass fewer arguments than the definition lists.
func TestLegitimateShortCallsAreRefusedByNothing(t *testing.T) {
	for _, tc := range []struct{ name, src string }{
		{"trailing default", "def add(a, b=10):\n    return a + b\n\nprint(add(1))\n"},
		{"all defaults", "def add(a=1, b=2):\n    return a + b\n\nprint(add())\n"},
		{"defaults partly supplied", "def add3(a, b, c=10):\n    return a + b + c\n\nprint(add3(1, 2))\n"},
		{"keyword arguments", "def add(a, b):\n    return a + b\n\nprint(add(b=2, a=1))\n"},
		{"keyword plus default", "def add(a, b=10):\n    return a + b\n\nprint(add(a=1))\n"},
		{"zero-parameter function", "def constant():\n    return 42\n\nprint(constant())\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			for _, d := range arityDiags(t, tc.src) {
				if d.Level == LevelError {
					t.Errorf("%s: a legitimate call was refused: %v", tc.name, d)
				}
			}
		})
	}
}

// TestMethodsAreNotCountedWithSelf: `self` is not an argument the caller passes, so a method call
// must be measured against the parameters after it.
func TestMethodsAreNotCountedWithSelf(t *testing.T) {
	for _, tc := range []struct{ name, src string }{
		{"one-arg method", "class C:\n    def m(self, x):\n        return x\n\nprint(C().m(3))\n"},
		{"no-arg method", "class C:\n    def m(self):\n        return 1\n\nprint(C().m())\n"},
		{"method with a default", "class C:\n    def m(self, x, y=2):\n        return x + y\n\nprint(C().m(3))\n"},
		{"sibling call through self", "class C:\n    def two(self, x):\n        return x + x\n\n    def go(self, x):\n        return self.two(x)\n\nprint(C().go(2))\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			for _, d := range arityDiags(t, tc.src) {
				if d.Level == LevelError && strings.Contains(d.Msg, "argument") {
					t.Errorf("%s: a method call was refused over self: %v", tc.name, d)
				}
			}
		})
	}
}

// TestArgumentTypeCheckingSurvives: the early return that keeps a malformed call from being
// walked into the callee must not skip the argument checks that do apply.
func TestArgumentTypeCheckingSurvives(t *testing.T) {
	diags := arityDiags(t, "def take(n: int) -> int:\n    return n\n\nprint(take(\"text\"))\n")
	if !hasMsg(diags, `argument "n": expected int, got str`) {
		t.Errorf("a correct-arity call with a wrong argument type went unreported: %v", diags)
	}
}

// TestUnknownKeywordStillReported pins the sibling rules that already existed.
func TestUnknownKeywordStillReported(t *testing.T) {
	diags := arityDiags(t, "def add(a, b):\n    return a + b\n\nprint(add(a=1, zz=2))\n")
	if !hasMsg(diags, `unknown keyword argument "zz"`) {
		t.Errorf("an unknown keyword name went unreported: %v", diags)
	}
}

// TestBuiltinCallsKeepTheirOwnArityRules: `print` takes anything, `range` takes one to three.
// A built-in's arity is its own contract and must not be measured against a user definition.
func TestBuiltinCallsKeepTheirOwnArityRules(t *testing.T) {
	for _, src := range []string{
		"print(1)\n",
		"print(1, 2, 3)\n",
		"print()\n",
		"print(1, sep=\",\", end=\"!\")\n",
		"for i in range(3):\n    print(i)\n",
		"for i in range(1, 9, 2):\n    print(i)\n",
		"print(len([1, 2, 3]))\n",
	} {
		for _, d := range arityDiags(t, src) {
			if d.Level == LevelError {
				t.Errorf("a built-in call was refused: %v (source %q)", d, src)
			}
		}
	}
}
