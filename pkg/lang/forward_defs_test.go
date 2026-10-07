package lang

import (
	"strings"
	"testing"
)

// A `def` is a binding of the scope that contains it, not only of the text below it.
// Python's rule is that the name must exist when the call *runs*, and a body is not run
// when it is defined — so two functions that call each other, or a helper declared
// below the code that uses it, are ordinary programs. The checker walked the file in
// order and called that `undefined name "is_odd"`, and the compiled path refused to build
// it at all while the record ran it happily (Gap R.6, ADR 0197).

func hasMsg(diags []Diagnostic, want string) bool {
	for _, d := range diags {
		if strings.Contains(d.Msg, want) {
			return true
		}
	}
	return false
}

func TestMutualRecursionIsNotAnUndefinedName(t *testing.T) {
	src := `def is_even(n):
    if n == 0:
        return True
    return is_odd(n - 1)

def is_odd(n):
    if n == 0:
        return False
    return is_even(n - 1)

print(is_even(4))
`
	diags := checkDiags(t, src)
	if hasMsg(diags, "undefined name") {
		t.Errorf("mutually recursive functions were refused: %v", diags)
	}
	if anyErr(diags) {
		t.Errorf("the canonical Python shape must check clean, got %v", diags)
	}
}

func TestForwardReferenceShapes(t *testing.T) {
	for _, tc := range []struct {
		name    string
		src     string
		want    string
		wantErr bool
	}{
		{
			name: "a helper declared below its caller",
			src:  "def twice(x):\n    return scale(x) * 2\n\ndef scale(x):\n    return x * 3\n\nprint(twice(2))\n",
		},
		{
			name: "a def inside an if arm is still a scope binding",
			src:  "def use():\n    return helper()\n\nif 1 == 1:\n    def helper():\n        return 5\n\nprint(use())\n",
		},
		{
			name: "a def inside a loop arm",
			src:  "def use():\n    return late()\n\nwhile 0 == 1:\n    def late():\n        return 1\n\nprint(use())\n",
		},
		{
			// Two sibling nested defs that call each other: reachable, and the
			// canonical shape for a mutually recursive pair inside a closure.
			name: "sibling nested defs call each other",
			src:  "def outer(n):\n    def a(m):\n        return b(m)\n\n    def b(m):\n        return m * 2\n\n    return a(n)\n\nprint(outer(3))\n",
		},
		{
			name: "a class method visible to another method",
			src:  "class Box:\n    def value(self):\n        return self.doubled() * 1\n\n    def doubled(self):\n        return 21\n\nprint(Box().value())\n",
		},
		{
			// A decorator is evaluated where it is written, so its name really does
			// have to exist there — that one stays an error, and this row is here to
			// keep the hoisting from swallowing it.
			name:    "a decorator declared after the function it decorates stays an error",
			src:     "@identity\ndef target():\n    return 3\n\ndef identity(f):\n    return f\n\nprint(target())\n",
			want:    "undefined name",
			wantErr: true,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			diags := checkDiags(t, tc.src)
			got := hasMsg(diags, "undefined name")
			if tc.wantErr && !got {
				t.Errorf("this shape really is broken and must stay reported: %v", diags)
			}
			if !tc.wantErr && got {
				t.Errorf("a legal declaration-order shape was refused: %v", diags)
			}
		})
	}
}

// TestForwardCallStillChecksArguments: hoisting must not turn the checker off for the
// functions it hoists. The declared signature goes in with the name, so a call to a
// function the walk has not reached still checks its argument against the annotation.
func TestForwardCallStillChecksArguments(t *testing.T) {
	src := `def use():
    return take("text")

def take(n: int) -> int:
    return n
`
	diags := checkDiags(t, src)
	if !hasMsg(diags, "int") || !hasMsg(diags, "str") {
		t.Errorf("an argument type error across a forward reference was not reported: %v", diags)
	}
}

// TestHoistingIsNotAFreeForAll: the names that genuinely do not exist must still be
// refused, and a *variable* is still not visible above its assignment — only defs and
// classes hoist, because only they are declarations.
func TestHoistingIsNotAFreeForAll(t *testing.T) {
	for _, tc := range []struct {
		name string
		src  string
		want string
	}{
		{"a name that does not exist", "print(nope)\n", `undefined name "nope"`},
		{"a variable used above its assignment", "print(total)\ntotal = 3\n", `undefined name "total"`},
		{"a function that exists only inside another function", "def outer():\n    return 1\nprint(hidden())\n\n# hidden is never declared\n", `undefined name "hidden"`},
		{"a typo in a mutual partner's name", "def a(n):\n    return badd(n)\n\ndef b(n):\n    return n\n\nprint(a(1))\n", `undefined name "badd"`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			diags := checkDiags(t, tc.src)
			if !hasMsg(diags, tc.want) {
				t.Errorf("want %q, got %v", tc.want, diags)
			}
		})
	}
}
