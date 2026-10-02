package integration

import (
	"strings"
	"testing"

	"github.com/donutloop/gusty/pkg/lang"
)

// container_equality_test.go — `==` on containers is value equality, on both backends, with
// CPython as the referee.
//
// The value model makes a container an i32 handle, and the comparison compared those i32s: two
// equal lists were unequal, and — worse in the other direction — a stored string is an index into
// the interned table, so a container holding the number 0 and a container holding the first
// interned string were *equal*. Both were answers, on both backends, for as long as the only
// referee was the two backends agreeing with each other (roadmap L11.1, ADR 0189).

func TestContainerEqualityMatchesCPython(t *testing.T) {
	for _, tc := range []struct {
		src  string
		want string
	}{
		// Lists: positional, element by element.
		{"xs = [1, 2]\nys = [1, 2]\nif xs == ys:\n    print(\"eq\")\nelse:\n    print(\"ne\")\n", "eq\n"},
		{"xs = [1, 2]\nys = [2, 1]\nif xs == ys:\n    print(\"eq\")\nelse:\n    print(\"ne\")\n", "ne\n"},
		{"def v(a, b):\n    return a == b\n\nprint(v([1, \"a\", None], [1, \"a\", None]))\n", "True\n"},
		// The interned-index collision: only the tag separates a number from a word.
		{"def v(a, b):\n    return a == b\n\nprint(v([0], [\"zero\"]))\n", "False\n"},
		{"def v(a, b):\n    return a == b\n\nprint(v([0, \"zero\"], [0, \"zero\"]))\n", "True\n"},
		// Sets and dicts are unordered; a positional walk would get these wrong.
		{"def v(a, b):\n    return a == b\n\nprint(v({1, 2}, {2, 1}))\n", "True\n"},
		{"def v(a, b):\n    return a == b\n\nprint(v({\"a\": 1, \"b\": 2}, {\"b\": 2, \"a\": 1}))\n", "True\n"},
		{"def v(a, b):\n    return a == b\n\nprint(v({\"a\": 1}, {\"a\": 2}))\n", "False\n"},
		// Mutation rewrites the answer, so mutation has to rewrite the tags too.
		{
			"xs = [1, 2]\nys = [1, 2]\nxs.append(3)\nif xs == ys:\n    print(\"eq\")\nelse:\n    print(\"ne\")\n",
			"ne\n",
		},
		{
			"d = {\"a\": 1}\ne = {\"a\": 1}\nd[\"b\"] = 2\nif d == e:\n    print(\"eq\")\nelse:\n    print(\"ne\")\n",
			"ne\n",
		},
		{
			"s = {1, 2}\nt = {1, 2}\ns.add(3)\nif s == t:\n    print(\"eq\")\nelse:\n    print(\"ne\")\n",
			"ne\n",
		},
		// A container against a scalar is unequal, not a trap and not a crash.
		{"xs = [1]\nif xs == 1:\n    print(\"eq\")\nelse:\n    print(\"ne\")\n", "ne\n"},
		// `is` keeps asking the identity question.
		{"xs = [1, 2]\nys = [1, 2]\nif xs is xs:\n    print(\"same\")\nif xs is ys:\n    print(\"aliased\")\nelse:\n    print(\"distinct\")\n", "same\ndistinct\n"},
	} {
		if interped := runInterp(t, tc.src); interped != tc.want {
			t.Errorf("interpreter %q = %q, want %q", tc.src, interped, tc.want)
		}
		res, err := lang.JIT(tc.src, 0)
		if err != nil {
			t.Fatalf("compile %q: %v", tc.src, err)
		}
		if res.Output != tc.want {
			t.Errorf("compiled %q = %q, want %q", tc.src, res.Output, tc.want)
		}
		if py := pythonOutput(t, tc.src); py != tc.want {
			t.Errorf("CPython %q = %q — expectations come from the oracle, not from us", tc.src, py)
		}
	}
}

// Comparing a container with something of unknown kind has no honest answer until values carry
// tags (Gap R.112): the i32s are both numbers, and only one of them is a handle. It refuses, and the
// refusal says what is missing.
func TestContainerEqualityWithUnknownKindRefuses(t *testing.T) {
	for _, src := range []string{
		"def make():\n    return [1]\n\nxs = [1]\nprint(xs == make())\n",
		"def echo(v):\n    return v\n\nxs = [1]\nprint(xs == echo(1))\n",
	} {
		_, err := lang.Compile(src)
		if err == nil {
			t.Fatalf("%q compiled; comparing a container with an unknown kind must refuse", src)
		}
		if !strings.Contains(err.Error(), "tagged value") {
			t.Errorf("%q refused with %q, want it to name the missing tagged value", src, err.Error())
		}
	}
}
