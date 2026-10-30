package lang

import (
	"strings"
	"testing"
)

// floatargs_test.go — the census that reads a PARAMETER's float kind from the CALL SITES, which is the only
// place that evidence exists (**roadmap Gap R.237**, **ADR 0477**).
//
// `def at(k): return xs[0:k]` holds no assignment to `k`, so the classifier that reads a name back from what its
// own body last bound names nothing, and the bound reaches the slice helper as one i32 word carrying a float box's
// handle — `print(at(1.5))` answered `1` at the exit code of a success where the reference raises
// `TypeError: slice indices must be integers or None or have an __index__ method`. The claim `floatArgKinds` writes
// is what lets the door ask the question; these rows are the honesty rules that decide when the claim may exist:
//
//   - every call site, or nothing;
//   - a body that rebinds its own parameter takes the claim back;
//   - one callable per name, or the name is not a callable the census may describe;
//   - and a name is scoped to the body that owns it — a parameter named `k` in one function is not a float in
//     another, which is the same rule the text census (`strArgKinds`) and the verdict census enforce.

func mustParseCensus(t *testing.T, src string) *Program {
	t.Helper()
	prog, err := Parse(src)
	if err != nil {
		t.Fatalf("parse %q: %v", src, err)
	}
	return prog
}

func TestTheFloatCensusMarksWhatEveryCallSiteHandsAParameter(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name string
		src  string
		fn   string
		slot int
	}{
		{
			"a float literal at the call site",
			"def at(k):\n    xs = [1, 2, 3]\n    return xs[0:k]\n\nprint(at(1.5))\n",
			"at", 0,
		},
		{
			"two call sites, both floats",
			"def at(k):\n    return k\n\nprint(at(1.5))\nprint(at(2.5))\n",
			"at", 0,
		},
		{
			"a float handed through a forwarding body",
			"def at(k):\n    return k\n\ndef outer(k):\n    return at(k)\n\nprint(outer(1.5))\n",
			"at", 0,
		},
		{
			"the keyword spelling",
			"def at(a, k):\n    return k\n\nprint(at(1, k = 1.5))\n",
			"at", 1,
		},
		{
			"a declared float parameter, never called",
			"def at(k: float):\n    return k\n",
			"at", 0,
		},
		{
			"a float default",
			"def at(k = 1.5):\n    return k\n",
			"at", 0,
		},
		{
			"a method parameter, past self",
			"class Case:\n    def cut(self, k):\n        return k\n\nprint(Case().cut(1.5))\n",
			"cut", 1,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			kinds := floatArgKinds(mustParseCensus(t, tc.src))
			if !kinds[tc.fn][tc.slot] {
				t.Fatalf("%q: position %d of %q is filled with a float by every call site and the census is %v",
					tc.src, tc.slot, tc.fn, kinds)
			}
		})
	}
}

// TestTheFloatCensusDeclinesWhatAnyCallSiteContradicts is the honesty half: the claim is a claim about the whole
// program, and one argument that is not a float — or one body that rebinds its own parameter — is evidence the
// callers are not the story. A parameter the census declines keeps the road it always had, which is what keeps
// `print(at(2))` answering rather than refusing.
func TestTheFloatCensusDeclinesWhatAnyCallSiteContradicts(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name string
		src  string
		fn   string
		slot int
	}{
		{
			"one integer call site beside one float one",
			"def at(k):\n    return k\n\nprint(at(1.5))\nprint(at(2))\n",
			"at", 0,
		},
		{
			"a text call site",
			"def at(k):\n    return k\n\nprint(at(1.5))\nprint(at(\"1\"))\n",
			"at", 0,
		},
		{
			"the body rebinds its own parameter",
			"def at(k):\n    k = 2\n    return k\n\nprint(at(1.5))\n",
			"at", 0,
		},
		{
			"two callables, one name",
			"def at(k):\n    return k\n\ndef at(k):\n    return k\n\nprint(at(1.5))\n",
			"at", 0,
		},
		{
			"a method sharing a module function's name",
			"def cut(k):\n    return k\n\nclass Case:\n    def cut(self, k):\n        return k\n\n" +
				"print(cut(1.5))\nprint(Case().cut(2))\n",
			"cut", 0,
		},
		{
			"an argument the compiler cannot kind at all",
			"def g():\n    return 1.5\n\ndef at(k):\n    return k\n\nprint(at(g()))\n",
			"at", 0,
		},
		{
			"self is never a position",
			"class Case:\n    def cut(self, k):\n        return k\n\nprint(Case().cut(1.5))\n",
			"cut", 0,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			kinds := floatArgKinds(mustParseCensus(t, tc.src))
			if kinds[tc.fn][tc.slot] {
				t.Fatalf("%q: position %d of %q is contradicted and the census still claims it (census %v) — "+
					"the claim costs a program that compiles today (ADR 0273)", tc.src, tc.slot, tc.fn, kinds)
			}
		})
	}
}

// TestTheFloatClaimIsScopedToTheBodyThatOwnsIt is the row's own scoping rule, asserted where it can only be seen:
// two bodies, one parameter NAME, two arguments. A record keyed by the name and accumulated across the module would
// make `by_number(2)` raise for a bound its program filled with an integer, because some OTHER body's caller handed
// a `k` a float.
func TestTheFloatClaimIsScopedToTheBodyThatOwnsIt(t *testing.T) {
	t.Parallel()
	src := "def by_word(k):\n    xs = [1, 2, 3]\n    return xs[0:k]\n\n" +
		"def by_number(k):\n    xs = [1, 2, 3]\n    return xs[0:k]\n\n" +
		"print(by_number(2))\nprint(by_word(1.5))\n"
	if !floatArgKinds(mustParseCensus(t, src))["by_word"][0] {
		t.Fatal("the float-carrying body must carry the claim, or this row is testing nothing")
	}
	if got, refused := compiledOutOrRefusal(t, "def by_number(k):\n    xs = [1, 2, 3]\n    return xs[0:k]\n\nprint(by_number(2))"); refused {
		t.Fatalf("an integer-only call site refused (%s): the float claim leaked across bodies that share a parameter name", got)
	} else if got != "[1, 2]\n" {
		t.Fatalf("the integer body printed %q, want %q", got, "[1, 2]\n")
	}
	class, msg := jitTrap(t, "def by_word(k):\n    xs = [1, 2, 3]\n    return xs[0:k]\n\nprint(by_word(1.5))")
	if class != "TypeError" || !strings.Contains(msg, "slice indices") {
		t.Fatalf("the float body raised %s: %s; the reference raises TypeError: slice indices must be integers or None or have an __index__ method", class, msg)
	}
}

// TestTheFloatCensusIsAskedByTheBoundDoorAlone keeps the claim's reach where the row says it is. The census is a
// whole-program fact; the one position that reads it is a slice bound, where the reference's answer is a raise. A
// reader who widened the subscript door to it without measuring would be spending a new class of refusal on programs
// that answer today — that shape is its own roadmap row, not this file.
func TestTheFloatCensusIsAskedByTheBoundDoorAlone(t *testing.T) {
	t.Parallel()
	src := "def at(k):\n    xs = [1, 2, 3]\n    return xs[0:k]\n\nprint(at(1.5))\n"
	if !floatArgKinds(mustParseCensus(t, src))["at"][0] {
		t.Fatal("the census must mark the parameter this program calls with a float")
	}
	// The same parameter read where the census is NOT asked: an ordinary subscript, which keeps the answer
	// (and the trap class) it had before this row existed.
	if got, refused := compiledOutOrRefusal(t, "def at(k):\n    xs = [1, 2, 3]\n    return xs[k]\n\nprint(at(1))"); refused {
		t.Fatalf("a subscript position refused (%s) although only the slice-bound door asks the census", got)
	} else if got != "2\n" {
		t.Fatalf("the subscript answered %q, want %q", got, "2\n")
	}
}
