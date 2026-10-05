package integration

// integration/floor_pair_test.go — `//` and `%` answer over a number whose kind crossed a call, at the CLI,
// against the reference, on both engines (roadmap L11.6's numeric truth, Gap R.162, ADR 0278).
//
// The shape is the pair door's oldest gap behind ADR 0276's: the door served `+ - *` and the condition doors
// served the comparisons, but the flooring operators were in neither list, so a parameter that could hold a
// double never left the one-word road and the double was truncated into it before the floor ran. `print(f(5))`
// and `print(f(5.0))` printed `2` and `2` for CPython's `2` and `2.0`, `print(modop(7.5, 2))` printed `1` for
// `1.5`, `print(modop(-7.5, 2))` printed `1` for `0.5`, and a forwarded `print(outer(5.0))` printed `2` — all
// at **exit 0**. The interpreter's answers were right on every one of them, which is the whole finding.
//
// Three claims, three tables:
//
//   - the parity rows, three legs, one answer: floor and remainder over a parameter, over two parameters, one
//     frame and two frames deep, and the two sign rules that make flooring worth pinning at all;
//   - the promoted programs/probe_floor_a_pair.gy, ten lines, pinned as a program so the corpus keeps it
//     honest rather than this file;
//   - the traps and the refusals: which of the reference's four ZeroDivisionError sentences a line raises is
//     the operand's kind's fact (the tag, not the source text), and a floored answer *combined* with other
//     arithmetic is still refused rather than answered with a truncated digit — Gap R.166, pinned as a
//     refusal so it cannot silently become a number again.
//
// Exit 2 fails every row here: a module `llc` rejects for an ordinary program is the compiler's bug, not the
// program's (ADR 0166).

import (
	"bytes"
	"os/exec"
	"strings"
	"testing"
)

// cliRunMerged is cliRunCode with the report attached: an uncaught trap's traceback and a front-end refusal's
// sentence are written to stderr, and a row that asserts what the program *said* has to read the stream it
// was said on. Exit 2 is still the compiler's bug (ADR 0166) and each row checks for it.
func cliRunMerged(t *testing.T, args ...string) (string, int) {
	t.Helper()
	cmd := exec.Command(cliBin(t), args...)
	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &out
	code := 0
	if err := cmd.Run(); err != nil {
		ee, ok := err.(*exec.ExitError)
		if !ok {
			t.Fatalf("gustyc %v: %v", args, err)
		}
		code = ee.ExitCode()
	}
	return out.String(), code
}

// floorParity is the family Gap R.162 measured wrong, with CPython's answer beside each.
func floorParity() []struct{ name, src, want string } {
	return []struct{ name, src, want string }{
		{
			"the row itself: a floor over a parameter that carries a double",
			"def floorit(v):\n    return v // 2\n\nprint(floorit(5.0))\n", "2.0\n",
		},
		{
			"the same define over an int answers an int",
			"def floorit(v):\n    return v // 2\n\nprint(floorit(5.0))\nprint(floorit(5))\n", "2.0\n2\n",
		},
		{
			"a remainder over a parameter that carries a double",
			"def modit(v):\n    return v % 2\n\nprint(modit(5.0))\nprint(modit(5))\n", "1.0\n1\n",
		},
		{
			// The two rules that make `//` and `%` a language rather than an `fdiv`: the floor rounds
			// toward negative infinity, and the remainder carries the divisor's sign. Both are decisions
			// the reference made and ADR 0216 wrote down; the pair road had simply never been shown them.
			"the floor goes down and the remainder takes the divisor's sign",
			"def floorit(v):\n    return v // 2\n\ndef modit(v):\n    return v % 2\n\nprint(floorit(-7.5))\nprint(modit(-7.5))\nprint(floorit(-7))\nprint(modit(-7))\n", "-4.0\n0.5\n-4\n1\n",
		},
		{
			"two parameters, either of which can carry the double",
			"def floordiv(a, b):\n    return a // b\n\ndef modop(a, b):\n    return a % b\n\nprint(floordiv(7.5, 2))\nprint(floordiv(7, 2))\nprint(modop(7.5, 2))\nprint(modop(-7.5, 2))\nprint(modop(-7, 2))\n", "3.0\n3\n1.5\n0.5\n1\n",
		},
		{
			"a slot read under the remainder",
			"def f(v):\n    return v % 3\n\nxs = []\nxs.append([7, 8])\nprint(f(xs[0][0]))\n", "1\n",
		},
		{
			"a slot read under the floor, int slot and float slot",
			"def f(v):\n    return v // 3\n\nxs = []\nxs.append([7, 8])\nxs.append([7.5, 8])\nprint(f(xs[0][0]))\nprint(f(xs[1][0]))\n", "2\n2.0\n",
		},
		{
			// Two doors at once: the pair was opened by ADR 0277's scan (the caller forwards, and the
			// callee's floor is asked of a value no literal ever named) over operators ADR 0278 served.
			"a forwarded pair under the floor",
			"def floorit(v):\n    return v // 2\n\ndef outer(x):\n    return floorit(x)\n\nprint(outer(5.0))\nprint(outer(5))\n", "2.0\n2\n",
		},
		{
			"a forwarded pair under the remainder, one frame further",
			"def other(w):\n    return w % 3\n\ndef middle(x):\n    return other(x)\n\ndef outer(x):\n    return middle(x)\n\nprint(outer(7.5))\nprint(outer(7))\n", "1.5\n1\n",
		},
		{
			"a floored answer printed beside the parameter itself",
			"def show(v):\n    print(v)\n    print(v // 2)\n    return 0\n\nshow(5.0)\n", "5.0\n2.0\n",
		},
		{
			"a floored answer in the arm of a condition over the parameter",
			"def big(v):\n    if v > 10:\n        return v // 2\n    return v\n\nprint(big(18.5))\nprint(big(7.5))\n", "9.0\n7.5\n",
		},
		{
			// ADR 0216's identity, whole: what ADR 0278 left refused (and, before that, answered with a
			// truncated digit at exit 0), paid the same day it was filed.
			"the flooring identity over a parameter",
			"def f(v):\n    return (v // 2) * 2 + (v % 2)\n\nprint(f(7))\nprint(f(7.5))\nprint(f(-3.5))\n", "7\n7.5\n-3.5\n",
		},
		{
			"the identity asked as a question, so a wrong half cannot hide",
			"def f(v):\n    return (v // 2) * 2 + (v % 2) == v\n\nprint(f(7.5))\n", "True\n",
		},
		{
			"a product of a floored parameter, both kinds",
			"def f(v):\n    return (v // 2) * 2\n\nprint(f(7.5))\nprint(f(7))\n", "6.0\n6\n",
		},
		{
			"a product beside a literal",
			"def f(v):\n    return v * 2 + 1\n\nprint(f(7.5))\nprint(f(7))\n", "16.0\n15\n",
		},
		{
			"a difference under a product",
			"def f(v):\n    return (v - 1) * 2\n\nprint(f(2.5))\nprint(f(2))\n", "3.0\n2\n",
		},
		{
			"a sum of a sum, left-nested — the arm question's own shape",
			"def f(v):\n    return v + 1 + 1\n\nprint(f(2.5))\n", "4.5\n",
		},
		{
			"both flooring operators in one answer",
			"def f(v):\n    return (v % 3) + (v // 3)\n\nprint(f(7.5))\nprint(f(7))\n", "3.5\n3\n",
		},
		{
			"a nested answer in the arm of a condition",
			"def f(v):\n    if v > 10:\n        return (v // 2) * 2 + 1\n    return v\n\nprint(f(17.5))\nprint(f(2.5))\n", "17.0\n2.5\n",
		},
		{
			// A *call*'s answer as one arm of a pair expression: roadmap Gap R.164's other half, ADR 0280.
			// ADR 0273 read a pair answer at `return other(v)` and nowhere wider, so `f(7.5)` printed nothing
			// at exit 1 on the compiled leg while the interpreter printed 3.5 — both legs are checked.
			"a call answer as an arm",
			"def other(w):\n    return w % 3\n\ndef f(v):\n    return (v // 2) + other(v)\n\nprint(f(7.5))\nprint(f(7))\n", "4.5\n4\n",
		},
		{
			// Two pair-returning calls in one expression, both arm orders. The first draft printed `30.0`
			// for CPython's `18.0` one way and the truth the other: a double answer leaves the arithmetic
			// door as a heap box that nothing rooted, and the next allocation recycled the slot (ADR 0181).
			"two call answers as the two arms, either order",
			"def half(w):\n    return w // 2\n\ndef twice(u):\n    return u * 2\n\nprint(half(7.5) + twice(7.5))\nprint(twice(7.5) + half(7.5))\n", "18.0\n18.0\n",
		},
		{
			// An identity assembled from two floored call answers — the shape that printed `3.0` for
			// CPython's `7.5` for the same unrooted-box reason, one door deeper.
			"an identity assembled from two call answers",
			"def floorit(v):\n    return v // 2\n\ndef modit(v):\n    return v % 2\n\ndef idn(v):\n    return floorit(v) * 2 + modit(v)\n\nprint(idn(7.5))\nprint(idn(7))\nprint(idn(-3.5))\n", "7.5\n7\n-3.5\n",
		},
		{
			// The slot door and the parameter door in one expression, which ADR 0273 kept apart: the arm
			// question now asks both of the same leaves. This program refused before the arm question
			// walked its leaves, and prints the reference's answer now.
			"a floored slot read under a product",
			"xs = []\nxs.append([7.5, 8])\nprint(((xs[0][0] - 1) * 2))\n", "13.0\n",
		},
		{
			// A float-state name is a pair too, and the same arm question covers it.
			"a nested answer over a float-state name",
			"x = 2\nx = x / 2\nprint((x + 1) * 2)\n", "4.0\n",
		},
	}
}

func TestTheFlooringOperatorsAnswerOnAllThreeLegs(t *testing.T) {
	for _, tc := range floorParity() {
		t.Run(tc.name, func(t *testing.T) {
			path := writeSrc(t, t.TempDir(), "floor_pair.gy", tc.src)
			if py, ok := cpythonOut(t, path); ok && py != tc.want {
				t.Fatalf("the expectation is not CPython's: got %q want %q", py, tc.want)
			}
			for _, engine := range []string{"--interp", "--aot"} {
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

// TestThePromotedFlooringProbePrintsCPythonTenLines is the closing event of the cycle, pinned as a program
// rather than as a list of fragments: ten lines, three legs, the same bytes. The compiled leg printed `2`,
// `1`, `1`, `1`, `-4`, `1`, `3`, `1`, `1`, `2` — truncated digits, every one of them, at exit 0.
func TestThePromotedFlooringProbePrintsCPythonTenLines(t *testing.T) {
	const want = "2.0\n1.0\n2\n1\n-4.0\n0.5\n3.0\n1.5\n1\n2.0\n"
	src := readProgram(t, "probe_floor_a_pair.gy")
	path := writeSrc(t, t.TempDir(), "probe_floor_a_pair.gy", src)
	if py, ok := cpythonOut(t, path); ok && py != want {
		t.Fatalf("the expectation is not CPython's: got %q want %q", py, want)
	}
	for _, engine := range []string{"--interp", "--aot"} {
		out, code := cliRunCode(t, engine, "--file", path)
		if code != 0 || out != want {
			t.Errorf("%s: exit %d, stdout %q, want %q\nstderr: %s", engine, code, out, want, cliRun(t, engine, "--file", path))
		}
	}
}

// TestTheFlooringTrapKeepsTheReferenceSentencesFromTheCommandLine is the half the truncated road could not
// reach at all: which of the reference's four ZeroDivisionError sentences a line gets is the operand's kind's
// fact, and over a parameter the kind is the tag — so `f(5)` and `f(5.0)` name themselves differently from
// one `def`. Exit 0 fails the row: a trap that prints a number is not a trap (ADR 0216's finding, ADR 0278's
// second half).
func TestTheFlooringTrapKeepsTheReferenceSentencesFromTheCommandLine(t *testing.T) {
	for _, tc := range []struct{ name, src, want string }{
		{
			"the floor by zero over a parameter that carries a double",
			"def f(v):\n    return v // 0\n\nprint(f(5.0))\n", "ZeroDivisionError: float floor division by zero",
		},
		{
			"the same line over an int names the integer sentence",
			"def f(v):\n    return v // 0\n\nprint(f(5))\n", "ZeroDivisionError: integer division or modulo by zero",
		},
		{
			// CPython does not let the remainder share the floor's wording, and neither may this: the
			// first draft of the runtime table did, and the row exists because the sweep caught it.
			"the remainder by zero over an int",
			"def f(v):\n    return v % 0\n\nprint(f(5))\n", "ZeroDivisionError: integer modulo by zero",
		},
		{
			"the remainder by zero over a double",
			"def f(v):\n    return v % 0\n\nprint(f(5.0))\n", "ZeroDivisionError: float modulo",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			path := writeSrc(t, t.TempDir(), "floor_trap.gy", tc.src)
			for _, engine := range []string{"--interp", "--aot"} {
				out, code := cliRunMerged(t, engine, "--file", path)
				if code == 2 {
					t.Fatalf("%s: exit 2 (ADR 0166):\n%s", engine, out)
				}
				if code == 0 {
					t.Errorf("%s exited 0 on a divide by zero — a trap that produces output is not a trap\nsrc: %s", engine, tc.src)
				}
				if !strings.Contains(out, tc.want) {
					t.Errorf("%s did not say %q (got %q)\nsrc: %s", engine, tc.want, out, tc.src)
				}
			}
		})
	}
}

// TestAPairAnswerHeldByAOneWordPositionRefusesRatherThanTruncates pins the ladder's honest half after
// Gap R.166 was paid: the door combines an *expression*, and ADR 0273's refusals remain where a *position*
// keeps one word — a container's element (Gap R.146), or a call's pair answer used as an arm (Gap R.164).
// Before ADR 0278 the first of these answered a truncated digit at exit 0, so the assertion is the exit class
// and the sentence, never a number: a refusal is where a wrong number is allowed to stand until it is fixed.
func TestAPairAnswerHeldByAOneWordPositionRefusesRatherThanTruncates(t *testing.T) {
	for _, tc := range []struct{ name, src, want string }{
		// The pair answer stored in a container rather than returned: ADR 0273's own sentence, Gap R.146's
		// family — the position keeps one word, so the tag has nowhere to go. Before ADR 0278 this program
		// printed 2 and 2 at exit 0, because the floor never saw the double; refusal is the standing the
		// ladder allows until R.146's road learns to box a pair answer.
		{
			"a floored pair answer appended to a list",
			"def floorit(v):\n    return v // 2\n\nout = []\nout.append(floorit(5.0))\nout.append(floorit(5))\nprint(out[0])\nprint(out[1])\n",
			"hands back the (payload, tag) pair",
		},
		{
			"a nested pair answer as a list-literal element",
			"def f(v):\n    return [v * 2 + 1]\n\nprint(f(7.5)[0])\n",
			"list literal elements must be integers",
		},
		{
			// A *call*'s pair answer as an arm of an expression used to be ADR 0273's refusal; ADR 0280 made
			// it an answer, so the row lives in the table above, and this neighbour is a refusal for the
			// other reason — the argument below carries one word and no tag (Gap R.146).
			"a pair answer handed to another function as its argument",
			"def floorit(v):\n    return v // 2\n\ndef twice(w):\n    return w * 2\n\nprint(twice(floorit(5.0)))\n",
			"hands back the (payload, tag) pair",
		},
		{
			// A pair answer handed on as an *argument*: the callee's position was marked for the pair and
			// then closed under it, so the caller is left with a double and one word. ADR 0276's supply gate
			// closes the position; ADR 0280 makes that closure a refusal instead of the truncation that
			// printed `8` for CPython's `10.0` (roadmap Gap R.164).
			"a pair answer bound and handed on as an argument",
			"def twice(v):\n    return v * 2\n\ndef outer(x):\n    y = twice(x)\n    z = twice(y)\n    return z\n\nprint(outer(2.5))\n",
			"considered for the (payload, tag) pair and closed",
		},
		{
			"a pair answer read back by an augmented assignment",
			"def twice(v):\n    return v * 2\n\ndef outer(x):\n    y = twice(x)\n    y += 1\n    return y\n\nprint(outer(2.5))\n",
			"holds the answer of arithmetic over a slot",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			path := writeSrc(t, t.TempDir(), "floor_nested.gy", tc.src)
			out, code := cliRunMerged(t, "--aot", "--file", path)
			if code == 0 {
				t.Fatalf("the compiled leg answered a pair in a one-word position with %q; Gap R.146 / Gap R.164 still own it and must refuse\nsrc: %s", out, tc.src)
			}
			if code != 1 {
				t.Fatalf("compiled exit %d, want the front-end refusal's 1\noutput: %s\nsrc: %s", code, out, tc.src)
			}
			if !strings.Contains(out, tc.want) {
				t.Errorf("the refusal must name the road that declined (ADR 0273's contract), want %q, got %q\nsrc: %s", tc.want, out, tc.src)
			}
		})
	}
}
