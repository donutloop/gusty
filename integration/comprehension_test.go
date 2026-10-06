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
			lang.RecordedStdoutIs(t, tc.src, tc.want)
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

// TestContainerComprehensionsMatchCPythonOnAllThreeLegs is the Gap J.2 half of the same contract
// (ADR 0234): a set or dict comprehension bound to a variable. Both spellings of one container —
// the literal `{1, 2}` and the comprehension `{x for x in [1, 2]}` — now go through ADR 0163's
// binding rule, and the `if` filter belongs to the comprehension instead of being swallowed by a
// ternary that demanded an `else`. Set members are listed ascending so CPython's hash-ordered
// rendering and the insertion order the compiled path keep say the same line.
func TestContainerComprehensionsMatchCPythonOnAllThreeLegs(t *testing.T) {
	for _, tc := range []struct {
		name, src, want string
	}{
		{"set_bound", "sa = {x for x in [1, 2, 3, 2, 1]}\nprint(sa)\nprint(len(sa))\n", "{1, 2, 3}\n3\n"},
		{"dict_bound", "da = {k: k * 2 for k in [1, 2]}\nprint(da)\nprint(da[2])\n", "{1: 2, 2: 4}\n4\n"},
		{"set_filter", "print({x for x in [1, 2, 3, 4] if x > 2})\n", "{3, 4}\n"},
		{"dict_filter", "print({k: k * 3 for k in [1, 2, 3] if k > 2})\n", "{3: 9}\n"},
		{"range_iterable", "print({x for x in range(4)})\n", "{0, 1, 2, 3}\n"},
		{"empty_set_comprehension", "print({x for x in []})\n", "set()\n"},
		{"materialised_iterable", "xs = [1, 2, 3]\nxs.append(4)\nprint({x for x in xs if x > 2})\n", "{3, 4}\n"},
		{"iterated_keys", "da = {k: k * 2 for k in [1, 2]}\nfor k in da:\n    print(k)\n", "1\n2\n"},
		{"membership", "if 2 in {x for x in [1, 2]}:\n    print(\"two\")\n", "two\n"},
		{"printed_directly", "print({k: k * 2 for k in [3]})\n", "{3: 6}\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			lang.RecordedStdoutIs(t, tc.src, tc.want)
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

// TestComprehensionRefusalsAreHonest covers the places the comprehension paths decline rather than
// getting one answer wrong: a runtime reduction (which would have summed an empty compile-time
// element set and printed 0), and a folded-away iterable (which has no slot to load) — the latter
// asserted for both the list and the set spelling, which refuse in the same words. A string
// comparison in a filter used to be the third case: the comparison itself was broken (two operands
// in different representations) and the filtered loop emitted a `phi` whose predecessors did not
// match. Both are fixed (Gap R.42, ADR 0224) and asserted positively in TestStringElementFilterCompiles.

func TestComprehensionRefusalsAreHonest(t *testing.T) {
	for _, tc := range []struct {
		src, want string
	}{
		{"def sq(n):\n    return n * n\n\nprint(sum([sq(x) for x in range(4)]))\n", "runtime reduction"},
		{"xs = [1, 2, 3]\nprint([x * 2 for x in xs])\n", "compile-time constant"},
		// The set twin declines a folded-away iterable in the same words the list twin uses,
		// which is what makes the refusal a statement about the program (Gap J.2, ADR 0234).
		{"xs = [1, 2, 3]\nprint({x for x in xs})\n", "compile-time constant"},
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
