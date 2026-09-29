package integration

import (
	"strings"
	"testing"

	"github.com/donutloop/gusty/pkg/lang"
)

// TestComprehensionCallsMatchCPythonOnAllThreeLegs is the integration half of the runtime
// comprehension feature (roadmap L11.7, ADR 0192). The AOT path folded constant elements and
// stopped, so [f(x) for x in range(5)] refused while the interpreter ran it — a two-backends,
// one-language situation the corpus had never exercised, because every comprehension in it had a
// constant element. Expectations are CPython's, transcribed from running the same source.
func TestComprehensionCallsMatchCPythonOnAllThreeLegs(t *testing.T) {
	for _, tc := range []struct {
		name, src, want string
	}{
		{"call_element", "def sq(n):\n    return n * n\n\nprint([sq(x) for x in range(5)])\n", "[0, 1, 4, 9, 16]\n"},
		{"builtin_element", "print([abs(x) for x in [-1, 2, -3]])\n", "[1, 2, 3]\n"},
		{"arith_element", "print([x * 2 for x in [1, 2, 3]])\n", "[2, 4, 6]\n"},
		{"folded_filter", "print([x for x in range(6) if x % 2 == 0])\n", "[0, 2, 4]\n"},
		{"call_filter", "def is_even(n):\n    return n % 2 == 0\n\nprint([x for x in range(10) if is_even(x)])\n", "[0, 2, 4, 6, 8]\n"},
		{"bound_result", "def sq(n):\n    return n * n\n\nsqrs = [sq(x) for x in range(4)]\nprint(sqrs)\nprint(len(sqrs))\nprint(sqrs[2])\n", "[0, 1, 4, 9]\n4\n4\n"},
		{"iterated_result", "def sq(n):\n    return n * n\n\nsqrs = [sq(x) for x in range(3)]\nfor v in sqrs:\n    print(v)\n", "0\n1\n4\n"},
		{"materialised_iterable", "xs = [1, 2, 3]\nxs.append(9)\nprint([x * 2 for x in xs])\n", "[2, 4, 6, 18]\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := runInterp(t, tc.src); got != tc.want {
				t.Errorf("interpreter\n got %q\nwant %q", got, tc.want)
			}
			res, err := lang.JIT(tc.src, 0)
			if err != nil {
				t.Fatalf("compile: %v", err)
			}
			if res.Output != tc.want {
				t.Errorf("compiled\n got %q\nwant %q", res.Output, tc.want)
			}
			if py := pythonOutput(t, tc.src); py != tc.want {
				t.Errorf("CPython disagrees with the expectation we asserted\n got %q\nwant %q", py, tc.want)
			}
		})
	}
}

// TestComprehensionRefusalsAreHonest covers the three places the new path declines rather than
// getting one answer wrong: a runtime reduction (which would have summed an empty compile-time
// element set and printed 0), and a folded-away iterable (which has no slot to load). A string
// comparison in a filter used to be the third case: the comparison itself was broken (two operands
// in different representations) and the filtered loop emitted a `phi` whose predecessors did not
// match. Both are fixed (Gap R.42, ADR 0224) and asserted positively in TestStringElementFilterCompiles.
func TestComprehensionRefusalsAreHonest(t *testing.T) {
	for _, tc := range []struct {
		src, want string
	}{
		{"def sq(n):\n    return n * n\n\nprint(sum([sq(x) for x in range(4)]))\n", "runtime reduction"},
		{"xs = [1, 2, 3]\nprint([x * 2 for x in xs])\n", "compile-time constant"},
	} {
		res, err := lang.Compile(tc.src)
		msg := ""
		if err != nil {
			msg = err.Error()
		}
		if res != nil {
			for _, d := range res.Diagnostics {
				msg += d.Msg + "\n"
			}
		}
		if err == nil {
			t.Fatalf("%q must not compile", tc.src)
		}
		if !strings.Contains(msg, tc.want) {
			t.Fatalf("%q refusal should mention %q, got:\n%s", tc.src, tc.want, msg)
		}
	}
}
