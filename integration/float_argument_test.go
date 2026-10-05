package integration

// integration/float_argument_test.go — a number handed to a function keeps the kind its argument had, at
// the CLI, against the reference, on both engines (roadmap L11.6's numeric truth, Gap P.1's argument half,
// ADR 0276).
//
// One source, three legs: CPython, `gustyc --file <path> --interp`, `gustyc --file <path> --aot`. Three
// verdict classes are kept apart, as everywhere in this corpus:
//
//   - a shape the reference answers must print the same bytes on every leg, at exit 0;
//   - a shape whose answer has no word is a *front-end refusal* on the compiled leg (exit 1) while the
//     reference and the interpreted leg answer — the honest direction to be wrong;
//   - exit 2 fails any row here, refusal rows included: a module `llc` rejects for an ordinary program is
//     the compiler's bug, not the program's (ADR 0166).
//
// What these rows exist to prevent is the middle case none of them has: the compiled leg printing a
// truncated number at exit 0. `twice(2.5)` printed 4 that way, and `bump(1.5)` printed 2 — which is why
// the parity table asserts bytes rather than an exit code, and why the promoted
// integration/programs/probe_float_numeric.gy is registered for the corpus to keep honest.

import (
	"strings"
	"testing"
)

const floatArgTwice = "def twice(v):\n    return v * 2\n\n"

// floatArgParity is the family the compiled backend answered with a truncated number, exit 0 included.
func floatArgParity() []struct{ name, src, want string } {
	return []struct{ name, src, want string }{
		{"the row Gap P.1 filed", floatArgTwice + "print(twice(2.5))\n", "5.0\n"},
		{
			"one define, two arguments of two kinds",
			floatArgTwice + "print(twice(2.5))\nprint(twice(2))\nprint(twice(True))\n", "5.0\n4\n2\n",
		},
		{
			"a float-state variable handed to a call",
			"def twice(v):\n    return v * 2\n\nx = 8\nx = 2.5\nprint(twice(x))\n", "5.0\n",
		},
		{
			"a true-division quotient handed to a call",
			"def f(a, b):\n    return b\n\nxs = []\nxs.append(6)\nprint(f(1, xs[0] / 2))\n", "3.0\n",
		},
		{
			"a default is an argument the caller did not write",
			"def greet(name, times=1.5):\n    return times\n\nprint(greet(\"a\"))\nprint(greet(\"a\", 2))\n", "1.5\n2\n",
		},
		{
			"a parameter beside a provably-integer one",
			"def area(w, h):\n    return w * h\n\nprint(area(2.5, 2))\nprint(area(3, 4))\n", "5.0\n12\n",
		},
		{
			"a slot of a literal list that holds a double",
			floatArgTwice + "ys = [1, 2.5]\nprint(twice(ys[1]))\nprint(twice(ys[0]))\n", "5.0\n2\n",
		},
		{
			"a pair parameter beside an ordinary parameter",
			"def shift(a, b=100):\n    return a + b\n\nxs = []\nxs.append([7, 8])\nprint(shift(xs[0][1]))\nprint(shift(xs[0][1], 2))\n",
			"108\n10\n",
		},
		{
			"a body that adds the double to the parameter",
			"def bump(v):\n    v += 0.5\n    return v\n\nprint(bump(1))\n", "1.5\n",
		},
		{
			"a pair answer handed through another function",
			"def g(y):\n    return y * 2\n\ndef f(x):\n    x = x + 1.5\n    return g(x)\n\nprint(f(1.0))\n", "5.0\n",
		},
		{
			"the accumulator that changes state twice in a loop",
			"t = 0\nfor i in [4, 9]:\n    t += i / 2\nprint(t)\n", "6.5\n",
		},
		// The neighbours: integers keep answering integers, and the pair road must not reach a body that
		// already had a road of its own.
		{"integers keep their answers", floatArgTwice + "print(twice(3))\nprint(twice(0))\n", "6\n0\n"},
		{
			"the recursive integer function is untouched",
			"def fib(n):\n    if n < 2:\n        return n\n    return fib(n - 1) + fib(n - 2)\n\nprint(fib(10))\n", "55\n",
		},
		{
			"a comparison returned as a value keeps the road it had",
			"def cmpf(v):\n    return v > 1.5\n\nprint(cmpf(2.0))\nprint(cmpf(1.0))\n", "True\nFalse\n",
		},
		{
			"a comparison read as a condition takes the pair",
			"def big(v):\n    if v > 10:\n        return v * 2\n    return v\n\nprint(big(18.5))\nprint(big(7))\n", "37.0\n7\n",
		},
	}
}

// TestTheReferenceAndBothEnginesHandTheSameNumber is the parity table: one source, three legs, one answer.
func TestTheReferenceAndBothEnginesHandTheSameNumber(t *testing.T) {
	for _, tc := range floatArgParity() {
		t.Run(tc.name, func(t *testing.T) {
			path := writeSrc(t, t.TempDir(), "float_arg.gy", tc.src)
			if py, ok := cpythonOut(t, path); ok && py != tc.want {
				t.Fatalf("the expectation is not CPython's: got %q want %q", py, tc.want)
			}
			for _, engine := range []string{"--interp", "--aot"} {
				out, code := cliRunCode(t, engine, "--file", path)
				if code == 2 {
					t.Fatalf("%s: exit 2 (ADR 0166):\n%s", engine, out)
				}
				if code != 0 || out != tc.want {
					t.Errorf("%s: exit %d, stdout %q, want %q\nsrc: %s", engine, code, out, tc.want, tc.src)
				}
			}
		})
	}
}

// TestThePromotedFloatProbeStillPrintsCPythonSixLines is the paid debt, pinned as a program rather than as
// twelve fragments: the flooring identity, the rebound int variable, and the two calls whose arguments
// carry a double. It ran as a probe with the last two lines pinned wrong until this round.
func TestThePromotedFloatProbeStillPrintsCPythonSixLines(t *testing.T) {
	const want = "-4\n0.5\n4.0\n0.2\n2.5\n"
	src := readProgram(t, "probe_float_numeric.gy")
	path := writeSrc(t, t.TempDir(), "probe_float_numeric.gy", src)
	if py, ok := cpythonOut(t, path); ok && py != want {
		t.Fatalf("the expectation is not CPython's: got %q want %q", py, want)
	}
	for _, engine := range []string{"--interp", "--aot"} {
		out, code := cliRunCode(t, engine, "--file", path)
		if code == 2 {
			t.Fatalf("%s: exit 2 (ADR 0166):\n%s", engine, cliRun(t, engine, "--file", path))
		}
		if code != 0 || out != want {
			t.Errorf("%s: exit %d, printed\n%s\nwant\n%s", engine, code, out, want)
		}
	}
}

// TestTheCompiledLegRefusesWhatThePairCannotCarry keeps the honest half at the interface a script reads:
// exit 1, the missing half named, and no truncated digit anywhere in the output.
func TestTheCompiledLegRefusesWhatThePairCannotCarry(t *testing.T) {
	for _, tc := range []struct{ name, src, want string }{
		{
			// A container element is the element road's question, and this door declines it rather than
			// handing a payload wearing another kind's bits.
			"a container argument",
			"def pack(v):\n    return [v, 1]\n\nprint(pack(2.5))\n",
			"must be integers",
		},
		{
			// A function whose answer is a string index owns its parameter words too: taking the pair road
			// here printed the interned index as a number.
			"a function that returns str()",
			"def fmt(v):\n    return str(v)\n\nprint(fmt(2.5))\n",
			"str on non-integer",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			path := writeSrc(t, t.TempDir(), "float_arg_refuse.gy", tc.src)
			out, code := cliRunCode(t, "--aot", "--file", path)
			if code == 2 {
				t.Fatalf("the compiled leg rejected the compiler's own module (ADR 0166):\n%s", cliRun(t, "--aot", "--file", path))
			}
			if code != 1 {
				t.Fatalf("exit %d, want 1 (a front-end refusal): %s", code, out)
			}
			combined := cliRun(t, "--aot", "--file", path)
			if !strings.Contains(combined, tc.want) {
				t.Errorf("refusal does not name the missing half (%q):\n%s", tc.want, combined)
			}
			// The interpreted leg answers all of these, which is what makes the compiled refusal a
			// capability gap rather than a question about the program.
			if interp, icode := cliRunCode(t, "--interp", "--file", path); icode == 2 || interp == "" {
				t.Errorf("the interpreted leg printed nothing either (exit %d): %s", icode, interp)
			}
		})
	}
}
