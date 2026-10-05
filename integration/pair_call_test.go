package integration

// integration/pair_call_test.go — the (payload, tag) pair crosses a call at the CLI, against the
// reference, on both engines (roadmap Gap R.139, ADR 0273).
//
// Each row is one source run three ways: CPython, `gustyc --file <path> --interp`, and
// `gustyc --file <path> --aot`. The legs are forced explicitly — a bare `--file` is the interpreter's
// default (docs/operations.md), and the row that does not say which engine it ran on is the row that
// later turns out to have measured the wrong one. Exit 2, the contract's "the compiler is broken" code
// (ADR 0166), fails any row here.
//
// Three tables, because the three claims fail differently:
//
//   - the shapes the door answers, where all three legs must print the same bytes;
//   - the trap, where the *callee's* arithmetic raises CPython's own sentence naming the slot's real
//     kind, catchable by `except TypeError:` — the raise leaving the callee rather than the caller;
//   - the shapes the gate closes, which must be refused in words at exit 1 while the interpreted leg
//     answers the reference. A refusal is the honest outcome; a payload printed as though it were the
//     whole value is the one outcome the contract does not allow.

import (
	"strings"
	"testing"
)

const pairCallTwice = "def twice(v):\n    return v * 2\n\nxs = []\nxs.append([7, 8])\n"

// TestASlotReadHandedToAFunctionAnswersAtTheCLI is the roadmap row through the CLI: the expression the
// print door answers (ADR 0265), handed to a parameter.
func TestASlotReadHandedToAFunctionAnswersAtTheCLI(t *testing.T) {
	for _, tc := range []struct{ name, src, want string }{
		{"the row's own shape", pairCallTwice + "print(twice(xs[0][0]))\n", "14\n"},
		{
			"one define, two call sites — a slot and a literal",
			pairCallTwice + "print(twice(xs[0][0]))\nprint(twice(3))\n", "14\n6\n",
		},
		{
			"a float slot keeps its float",
			"def twice(v):\n    return v * 2\n\nxs = []\nxs.append([1.5, 2])\nprint(twice(xs[0][0]))\n", "3.0\n",
		},
		{
			"the keyword form lands on the same position",
			pairCallTwice + "print(twice(v=xs[0][1]))\n", "16\n",
		},
		{
			"two operands are slot reads",
			"def add(a, b):\n    return a + b\n\nxs = []\nxs.append([7, 8])\nprint(add(xs[0][0], xs[0][1]))\n", "15\n",
		},
		{
			"one slot and one literal, either side",
			"def add(a, b):\n    return a + b\n\nxs = []\nxs.append([7, 8])\nprint(add(xs[0][1], 1))\nprint(add(1, xs[0][1]))\n", "9\n9\n",
		},
		{
			"a dict slot by key",
			"def twice(v):\n    return v * 2\n\nd = {}\nd[\"k\"] = 40\nprint(twice(d[\"k\"]))\n", "80\n",
		},
		{
			// Python's bool is a number; the tag the caller handed over is the bool's own (ADR 0233's
			// rule, answered at this door by the same `@rt_num_arith`).
			"a bool slot is a number",
			"def twice(v):\n    return v * 2\n\nxs = []\nxs.append([True, 3])\nprint(twice(xs[0][0]))\n", "2\n",
		},
		{
			"three levels deep",
			"def twice(v):\n    return v * 2\n\nxs = []\nxs.append([[7, 8]])\nprint(twice(xs[0][0][1]))\n", "16\n",
		},
		{
			"arithmetic in the argument itself",
			pairCallTwice + "print(twice(xs[0][0] + 1))\n", "16\n",
		},
		{
			"a pair-bound name as the argument (Gap R.146's calling half)",
			pairCallTwice + "n = xs[0][0] * 2\nprint(twice(n))\n", "28\n",
		},
		{
			"the answer bound to a name",
			pairCallTwice + "n = twice(xs[0][0])\nprint(n)\n", "14\n",
		},
		{
			"two answers bound from one callee, each carrying its own kind",
			"def twice(v):\n    return v * 2\n\nxs = []\nxs.append([7, 8])\nxs.append([1.5, 2])\na = twice(xs[0][0])\nb = twice(xs[1][0])\nprint(a, b)\n", "14 3.0\n",
		},
		{
			"a container the loop built",
			"def twice(v):\n    return v * 2\n\nxs = []\ni = 0\nwhile i < 2:\n    xs.append([i * 7, 8])\n    i = i + 1\nprint(twice(xs[1][0]))\n", "14\n",
		},
		{
			"the answer printed twice, the tag read after each call",
			pairCallTwice + "print(twice(xs[0][0]))\nprint(twice(xs[0][0]))\n", "14\n14\n",
		},
		{
			"the answer in a print's second argument",
			pairCallTwice + "print(\"n\", twice(xs[0][0]))\n", "n 14\n",
		},
		{
			"a body that prints its own parameter",
			"def show(v):\n    print(v)\n    return 0\n\nxs = []\nxs.append([7, 8])\nshow(xs[0][0])\n", "7\n",
		},
		{
			"both arms of a condition over the parameter",
			"def big(v):\n    if v > 10:\n        return v * 2\n    return v\n\nxs = []\nxs.append([7, 18])\nprint(big(xs[0][1]))\nprint(big(xs[0][0]))\n", "36\n7\n",
		},
		{
			// A body that can reach its end without a `return` answers None, and the tag word has to say
			// so — otherwise the next caller reads the previous answer's kind out of it.
			"a callee that falls off the end answers None",
			"def maybe(v):\n    if v > 100:\n        return v * 2\n\nxs = []\nxs.append([7, 8])\nprint(maybe(xs[0][0]))\n", "None\n",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			gy := writeSrc(t, dir, "pair_call.gy", tc.src)
			if py, ok := cpythonPlainOut(t, dir, tc.src); !ok || py != tc.want {
				t.Fatalf("the expectation is not the reference's: python said %q (ok %v), the row says %q\nsrc: %s", py, ok, tc.want, tc.src)
			}
			for _, engine := range []string{"--interp", "--aot"} {
				out, code := cliRunCode(t, engine, "--file", gy)
				if code == 2 {
					t.Fatalf("%s: the compiler's own module was rejected (ADR 0166):\n%s", engine, out)
				}
				if code != 0 || out != tc.want {
					t.Errorf("%s: exit %d, stdout %q, want the reference's %q\nsrc: %s", engine, code, out, tc.want, tc.src)
				}
			}
		})
	}
}

// TestACalledSlotTrapsLikeTheReferenceAtTheCLI is the raise leaving the *callee*: the arithmetic runs
// inside the function, so the helper's status, the emitted store-and-branch and the sentence all belong
// to that frame, and the unwinding road has to name the answer it never gave (a tag word left holding
// the last real answer would print a number-shaped lie for the exception path).
func TestACalledSlotTrapsLikeTheReferenceAtTheCLI(t *testing.T) {
	for _, tc := range []struct{ name, src, want string }{
		{
			"a None slot under the callee's multiplication",
			"def twice(v):\n    return v * 2\n\nxs = []\nxs.append([None, 1])\nprint(twice(xs[0][0]))\n",
			"TypeError: unsupported operand type(s) for *: 'NoneType' and 'int'",
		},
		{
			"a None slot under the callee's addition",
			"def add(v):\n    return v + 1\n\nxs = []\nxs.append([None, 1])\nprint(add(xs[0][0]))\n",
			"TypeError: unsupported operand type(s) for +: 'NoneType' and 'int'",
		},
		{
			// The negation runs in the callee too, and ADR 0266's separate sentence for an operand with
			// no sign at all is the one that has to leave the callee's frame.
			"a None slot under the callee's negation",
			"def neg(v):\n    return -v\n\nxs = []\nxs.append([None, 1])\nprint(neg(xs[0][0]))\n",
			"TypeError: bad operand type for unary -: 'NoneType'",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			gy := writeSrc(t, dir, "pair_call_trap.gy", tc.src)
			if py, ok := cpythonPlainOut(t, dir, tc.src); ok || !strings.Contains(py, tc.want) {
				t.Fatalf("the reference was expected to stop with %q, said %q (ok %v)", tc.want, py, ok)
			}
			for _, engine := range []string{"--interp", "--aot"} {
				out, code := cliReport(t, engine, "--file", gy)
				if code == 2 {
					t.Fatalf("%s: exit 2 for a program the reference raises on (ADR 0166):\n%s", engine, out)
				}
				if code != 3 {
					t.Errorf("%s: exit %d, want 3 (the runtime-error class)\n%s", engine, code, out)
				}
				if !strings.Contains(out, tc.want) {
					t.Errorf("%s raised with %q, want the reference's %q", engine, out, tc.want)
				}
			}
		})
	}
}

// TestACalledSlotTrapIsCatchableAtTheCLI pins that the raise reaches a handler rather than only printing:
// the callee emits the store-and-branch through the language's one raise door (ADR 0228), so
// `except TypeError:` in the *caller* — a frame away from the arithmetic — reaches it.
func TestACalledSlotTrapIsCatchableAtTheCLI(t *testing.T) {
	for _, tc := range []struct{ name, src string }{
		{
			"caught in the caller",
			"def twice(v):\n    return v * 2\n\nxs = []\nxs.append([None, 1])\ntry:\n    print(twice(xs[0][0]))\nexcept TypeError:\n    print(\"caught\")\n",
		},
		{
			"caught around a binding of the answer",
			"def twice(v):\n    return v * 2\n\nxs = []\nxs.append([None, 1])\nn = 0\ntry:\n    n = twice(xs[0][0])\nexcept TypeError:\n    print(\"caught\")\nprint(n)\n",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			gy := writeSrc(t, dir, "pair_call_catch.gy", tc.src)
			if py, ok := cpythonPlainOut(t, dir, tc.src); !ok || !strings.Contains(py, "caught") {
				t.Fatalf("the reference was expected to print \"caught\", said %q", py)
			}
			for _, engine := range []string{"--interp", "--aot"} {
				out, code := cliRunCode(t, engine, "--file", gy)
				if code == 2 {
					t.Fatalf("%s: exit 2 (ADR 0166):\n%s", engine, out)
				}
				if code != 0 || !strings.Contains(out, "caught") {
					t.Errorf("%s: exit %d, stdout %q, want the handler to run\nsrc: %s", engine, code, out, tc.src)
				}
			}
		})
	}
}

// TestThePairCallRefusesWhatItCannotNameAtTheCLI is the gate through the CLI. Each row is a program the
// reference answers and the interpreted leg prints, where the compiled leg declines with the half that is
// missing named — the shape the scan's gate closes rather than an answer built from a payload alone.
func TestThePairCallRefusesWhatItCannotNameAtTheCLI(t *testing.T) {
	for _, tc := range []struct{ name, src, interpWant, refusal string }{
		{
			// An argument that is itself a pair-returning call: the inner answer's tag belongs to the
			// inner call, and the outer parameter would take one word for a value that has two.
			"a call as the argument",
			pairCallTwice + "print(twice(twice(xs[0][0])))\n", "28\n", "cannot reach into",
		},
		{
			// An answer read as one number is the payload alone — printed as an int, a float box's
			// handle would come out as digits (roadmap Gap R.146's sentence, one door further out).
			"the answer used as one number",
			pairCallTwice + "print(twice(xs[0][0]) + 1)\n", "15\n", "hands back the (payload, tag) pair",
		},
		{
			"the answer as a container element",
			pairCallTwice + "print([twice(xs[0][0])])\n", "[14]\n", "hands back the (payload, tag) pair",
		},
		{
			"the answer handed to another function",
			"def twice(v):\n    return v * 2\n\ndef show(w):\n    return w\n\nxs = []\nxs.append([7, 8])\nprint(show(twice(xs[0][0])))\n",
			"14\n", "hands back the (payload, tag) pair",
		},
		{
			// Gap R.154: a pair-carrying parameter beside an ordinary one. The body's `a + b` asks the
			// shared door for a kind for `b`, which is a parameter and not a value the caller tagged.
			"a pair parameter beside an ordinary parameter",
			"def shift(a, b=100):\n    return a + b\n\nxs = []\nxs.append([7, 8])\nprint(shift(xs[0][1]))\n",
			"108\n", "cannot reach into",
		},
		{
			// Gap R.82's shape seen from this door: one call site hands a text, whose repetition this
			// backend builds from neither road, so the parameter stays closed and the slot call site
			// keeps the ordinary road's refusal.
			"a text at one call site closes the parameter",
			pairCallTwice + "print(twice(xs[0][0]))\nprint(twice(\"hi\"))\n", "14\nhihi\n", "is not supported in the AOT backend",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			gy := writeSrc(t, dir, "pair_call_refusal.gy", tc.src)
			if py, ok := cpythonPlainOut(t, dir, tc.src); !ok || py != tc.interpWant {
				t.Fatalf("the reference was expected to print %q, said %q (ok %v)\nsrc: %s", tc.interpWant, py, ok, tc.src)
			}
			out, code := cliRunCode(t, "--interp", "--file", gy)
			if code == 2 {
				t.Fatalf("--interp: exit 2 (ADR 0166):\n%s", out)
			}
			if code != 0 || out != tc.interpWant {
				t.Errorf("--interp: exit %d, stdout %q, want the reference's %q\nsrc: %s", code, out, tc.interpWant, tc.src)
			}
			out, code = cliReport(t, "--aot", "--file", gy)
			if code == 2 {
				t.Fatalf("--aot: exit 2 where the front end should refuse (ADR 0166):\n%s", out)
			}
			if code != 1 || !strings.Contains(out, tc.refusal) {
				t.Errorf("--aot: exit %d, want the refusal naming %q, said:\n%s", code, tc.refusal, out)
			}
		})
	}
}
