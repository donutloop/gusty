package integration

// integration/forwarded_pair_test.go — a number handed to a function keeps its kind when the body hands it
// on to *another* function, at the CLI, against the reference, on the compiled path (roadmap L11.6's numeric
// truth, Gap R.161, ADR 0277).
//
// The shape is one line longer than ADR 0276's and a whole level deeper: `def outer(x): return twice(x)`
// contains nothing that says `x` can be a float. A parameter is written by the caller and never by the
// body, so no assignment in the program records a binding for it, and the only evidence that `x` can end on
// a double is the argument `outer`'s own call site was written with. Read without that evidence the compiled
// leg truncated at each boundary — `print(outer(2.5))` printed 4 at **exit 0**, `print(fib-free add(x, 1))`
// printed 3 for CPython's 3.5 — while the interpreter, which boxes the value and asks it, printed CPython's
// answer.
//
// Three claims, three tables:
//
//   - the parity rows, three legs, one answer: the family the call graph now answers, one to three frames
//     deep, a forwarded parameter beside a provably-integer one, and a keyword argument on the same page;
//   - the promoted programs/probe_forward_a_pair_through_a_function.gy, eight lines, pinned as a program so
//     the corpus keeps it honest rather than this file;
//   - the chain that must *not* take the pair: close the callee — it floors the value, renders it a text,
//     indexes with it — and the caller's pair has to go with it. Those rows pin the exit class rather than a
//     number, because what they protect is the absence of a truncated digit at exit 0.
//
// Exit 2 fails every row here: a module `llc` rejects for an ordinary program is the compiler's bug, not the
// program's (ADR 0166).

import (
	"strings"
	"testing"
)

// forwardedParity is the family Gap R.161 measured wrong, with CPython's answer beside each.
func forwardedParity() []struct{ name, src, want string } {
	return []struct{ name, src, want string }{
		{
			"the row itself: a double forwarded through a parameter",
			"def twice(v):\n    return v * 2\n\ndef outer(x):\n    return twice(x)\n\nprint(outer(2.5))\n", "5.0\n",
		},
		{
			// Gap R.164: the callee's answer is first a *name*, and the body returns that name. Nothing in
			// the chain changed shape — `return twice(x)` already answered and `print(y)` already answered —
			// and the middle position alone printed `4` at exit 0 for CPython's `5.0`. The sibling that must
			// not move is the rebinding below: a name written back with an ordinary int answers as one.
			"a pair answer bound to a name and returned",
			"def twice(v):\n    return v * 2\n\ndef outer(x):\n    y = twice(x)\n    return y\n\nprint(outer(2.5))\nprint(outer(3))\n", "5.0\n6\n",
		},
		{
			"a pair answer bound to a name and returned through arithmetic",
			"def twice(v):\n    return v * 2\n\ndef outer(x):\n    y = twice(x)\n    return y + 0\n\nprint(outer(2.5))\nprint(outer(3))\n", "5.0\n6\n",
		},
		{
			"a pair answer bound in a frame two deep",
			"def twice(v):\n    return v * 2\n\ndef a(x):\n    y = twice(x)\n    return y\n\ndef b(x):\n    return a(x)\n\nprint(b(2.5))\nprint(b(3))\n", "5.0\n6\n",
		},
		{
			"a floored answer bound to a name and returned",
			"def floorit(v):\n    return v // 2\n\ndef outer(x):\n    h = floorit(x)\n    return h\n\nprint(outer(5.0))\nprint(outer(5))\n", "2.0\n2\n",
		},
		{
			"a name bound to a pair answer and then to an ordinary int answers as one",
			"def twice(v):\n    return v * 2\n\ndef outer(x):\n    y = twice(x)\n    y = 3\n    return y\n\nprint(outer(2.5))\n", "3\n",
		},
		{
			"one forwarded define, two arguments of two kinds",
			"def twice(v):\n    return v * 2\n\ndef outer(x):\n    return twice(x)\n\nprint(outer(2.5))\nprint(outer(3))\n", "5.0\n6\n",
		},
		{
			"two frames hand the same pair on",
			"def twice(v):\n    return v * 2\n\ndef middle(x):\n    return twice(x)\n\ndef outer(x):\n    return middle(x)\n\nprint(outer(2.5))\n", "5.0\n",
		},
		{
			"a forwarded parameter beside a provably-integer one",
			"def twice(v):\n    return v * 2\n\ndef outer(a, b):\n    return twice(b)\n\nprint(outer(1, 2.5))\nprint(outer(1, 2))\n", "5.0\n4\n",
		},
		{
			"a forwarded parameter in arithmetic with a literal the body writes",
			"def add(a, b):\n    return a + b\n\ndef shift_it(z):\n    return add(z, 1)\n\nprint(shift_it(2.5))\nprint(shift_it(2))\n", "3.5\n3\n",
		},
		{
			"a slot of a literal list, forwarded",
			"def twice(v):\n    return v * 2\n\ndef outer(x):\n    return twice(x)\n\nys = [1, 2.5]\nprint(outer(ys[1]))\nprint(outer(ys[0]))\n", "5.0\n2\n",
		},
		{
			"a float-state variable, forwarded",
			"def twice(v):\n    return v * 2\n\ndef outer(x):\n    return twice(x)\n\nx = 8\nx = 2.5\nprint(outer(x))\n", "5.0\n",
		},
		{
			"the keyword form lands on the forwarded position",
			"def twice(v):\n    return v * 2\n\ndef outer(x):\n    return twice(v=x)\n\nprint(outer(2.5))\n", "5.0\n",
		},
		{
			"a slot read handed on by a callee written below its caller",
			"def f(v):\n    return other(v)\n\ndef other(w):\n    return w * 2\n\nxs = []\nxs.append([1.5, 8])\nprint(f(xs[0][0]))\nprint(f(3))\n", "3.0\n6\n",
		},
		{
			"a slot read handed on by a callee written above its caller",
			"def other(w):\n    return w * 2\n\ndef f(v):\n    return other(v)\n\nxs = []\nxs.append([1.5, 8])\nprint(f(xs[0][0]))\nprint(f(3))\n", "3.0\n6\n",
		},
		// The neighbours: an integer chain keeps the one word it always had, and a body that cannot read a
		// pair is not given one.
		{
			"an all-integer chain is untouched",
			"def twice(v):\n    return v * 2\n\ndef outer(x):\n    return twice(x)\n\nprint(outer(3))\n", "6\n",
		},
		{
			"the recursive integer function is still one word",
			"def fib(n):\n    if n < 2:\n        return n\n    return fib(n - 1) + fib(n - 2)\n\nprint(fib(10))\n", "55\n",
		},
	}
}

// TestAForwardedNumberKeepsItsKindOnEveryLeg is the parity table: one source, three legs, one answer.
func TestAForwardedNumberKeepsItsKindOnEveryLeg(t *testing.T) {
	for _, tc := range forwardedParity() {
		t.Run(tc.name, func(t *testing.T) {
			path := writeSrc(t, t.TempDir(), "forwarded_pair.gy", tc.src)
			if py, ok := cpythonOut(t, path); ok && py != tc.want {
				t.Fatalf("the expectation is not CPython's: got %q want %q", py, tc.want)
			}
			for _, engine := range cliEngines {
				out, code := cliRunCode(t, engine, "--file", path)
				if code == 2 {
					t.Fatalf("%s: exit 2 (ADR 0166):\n%s", engine, cliRun(t, engine, "--file", path))
				}
				if code != 0 || out != tc.want {
					t.Errorf("%s: exit %d, stdout %q, want %q\nsrc: %s", engine, code, out, tc.want, tc.src)
				}
			}
		})
	}
}

// TestThePromotedForwardingProbePrintsCPythonEightLines is the closing event of the cycle, pinned as a
// program rather than as a list of fragments: eight lines, three legs, the same bytes. Until this round the
// compiled leg printed `4` for the third, fifth and sixth lines and `3` for the seventh, all of them at
// exit 0.
func TestThePromotedForwardingProbePrintsCPythonEightLines(t *testing.T) {
	const want = "5.0\n6\n5.0\n6\n5.0\n5.0\n3.5\n3\n"
	src := readProgram(t, "probe_forward_a_pair_through_a_function.gy")
	path := writeSrc(t, t.TempDir(), "probe_forward_a_pair_through_a_function.gy", src)
	if py, ok := cpythonOut(t, path); ok && py != want {
		t.Fatalf("the expectation is not CPython's: got %q want %q", py, want)
	}
	for _, engine := range cliEngines {
		out, code := cliRunCode(t, engine, "--file", path)
		if code == 2 {
			t.Fatalf("%s: exit 2 (ADR 0166):\n%s", engine, cliRun(t, engine, "--file", path))
		}
		if code != 0 || out != want {
			t.Errorf("%s: exit %d, printed\n%s\nwant\n%s", engine, code, out, want)
		}
	}
}

// TestAForwardedChainThatCannotCarryThePairStaysOnItsRoad is the prune at the interface: closing the callee
// — it floors the value, renders it a text, indexes with it — has to close the caller's pair with it. The
// rows pin the exit class and the absence of a digit, not a number: what they protect is that no leg prints a
// truncated answer at exit 0 for a chain the doors cannot carry, and that the reference still answers,
// which is what makes any compiled refusal a capability gap rather than a question about the program.
func TestAForwardedChainThatCannotCarryThePairStaysOnItsRoad(t *testing.T) {
	for _, tc := range []struct{ name, src, refusal string }{
		{
			"the callee floors it",
			"def floorit(v):\n    return v // 2\n\ndef outer(x):\n    return floorit(x)\n\nprint(outer(5.0))\n",
			"",
		},
		{
			// A function whose answer is a string index owns its parameter words (ADR 0174's road, ADR 0276's
			// `pairReturnRoadOwns`), so the chain keeps that road and the refusal that road has always said.
			"the callee renders it a text",
			"def fmt(v):\n    return str(v)\n\ndef outer(x):\n    return fmt(x)\n\nprint(outer(2.5))\n",
			"is refused: this backend renders a text through the one str/repr table",
		},
		{
			// An index of the parameter is a position the pair doors do not reach, so neither frame is given
			// a tag word — and the program, whose value is an int throughout, answers as it always did.
			"the callee indexes with it",
			"def pick(v):\n    out = [9, 4]\n    return out[v] + 1\n\ndef outer(x):\n    return pick(x)\n\nprint(outer(1))\n",
			"",
		},
		{
			// The callee's body reads the parameter where no door reaches it (`%`), and the caller above it
			// would have carried the pair on the callee's promise alone.
			"the callee takes the remainder",
			"def modit(v):\n    return v % 2\n\ndef outer(x):\n    return modit(x)\n\nprint(outer(5))\n",
			"",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			path := writeSrc(t, t.TempDir(), "forwarded_closed.gy", tc.src)
			// This is the compiled leg — there is only one — and CPython is the reference it is compared
			// to. The duplicate "reference" in the old wording was the retired engine, whose leg left
			// with ADR 0302; the row's real claim is that the compiled program and CPython agree, or that
			// the compiler refuses this shape out loud.
			interp, icode := cliRunCode(t, "--aot", "--file", path)
			if icode == 2 {
				t.Fatalf("exit 2 — LLVM rejected the module gusty emitted (ADR 0166):\n%s", cliRun(t, "--aot", "--file", path))
			}
			py, ok := cpythonOut(t, path)
			if !ok {
				t.Fatalf("the reference failed to answer this program: %s", path)
			}
			checkCompiledRow(t, interp, icode, tc.src, py)
			out, code := cliRunCode(t, "--aot", "--file", path)
			if code == 2 {
				t.Fatalf("the compiled leg rejected the compiler's own module (ADR 0166):\n%s", cliRun(t, "--aot", "--file", path))
			}
			if code == 0 && out != interp {
				// The pre-existing half of this family: an answer that disagrees with the reference at
				// exit 0 is Gap R.162's and Gap R.164's business, and pinning it here would make the
				// ledger a record of whatever the compiler last did. It is pinned there instead, by name.
				t.Logf("compiled leg answers %q where the reference answers %q — filed, not pinned", out, interp)
			}
			if code != 0 && code != 1 {
				t.Errorf("exit %d, want 0 (an answer) or 1 (a front-end refusal)", code)
			}
			if code == 1 {
				want := tc.refusal
				if want == "" {
					want = "pair"
				}
				if msg := cliRun(t, "--aot", "--file", path); !strings.Contains(msg, want) {
					t.Errorf("the compiled refusal does not name the half it is missing (%q):\n%s", want, msg)
				}
			}
		})
	}
}
