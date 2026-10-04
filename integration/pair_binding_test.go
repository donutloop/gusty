package integration

// integration/pair_binding_test.go — the arithmetic a slot's answer takes, bound to a name, at the CLI,
// against the reference, on both engines (roadmap Gap R.138, ADR 0267).
//
// Each row is the same source run three ways: CPython, `gustyc --file <path> --interp`, and
// `gustyc --file <path> --aot`. The legs are forced explicitly — a bare `--file` is the interpreter's
// default, and `--aot` written after the path becomes the flag's value rather than the compiled leg.
// Exit 2 — the contract's "the compiler is broken" code (ADR 0166) — fails any row here.
//
// Three tables, because the three claims fail differently: the bindings that answer; the rebindings that
// retire the tag (Gap R.142, the wrong answer this cycle's own probe found); and the positions the pair
// does not reach yet, which must be refused in words rather than answered by the payload alone
// (Gaps R.143, R.144).

import (
	"os"
	"strings"
	"testing"
)

const built = "xs = []\nxs.append([7, 8])\n"

func TestABoundArithmeticAnswerAnswersLikeTheReferenceAtTheCLI(t *testing.T) {
	for _, tc := range []struct{ name, src, want string }{
		{"the row's own shape", built + "n = xs[0][0] * 2\nprint(n)\n", "14\n"},
		{"addition", built + "n = xs[0][0] + 1\nprint(n)\n", "8\n"},
		{"subtraction", built + "n = xs[0][1] - 3\nprint(n)\n", "5\n"},
		{"the negation", built + "n = -xs[0][0]\nprint(n)\n", "-7\n"},
		{"both operands are slots", built + "n = xs[0][0] + xs[0][1]\nprint(n)\n", "15\n"},
		{"a float slot keeps the float", "xs = []\nxs.append([7.5, 8])\nn = xs[0][0] * 2\nprint(n)\n", "15.0\n"},
		{"a slot times a double literal", built + "b = xs[0][0] * 2.5\nprint(b)\n", "17.5\n"},
		{"two bindings of two families", built + "a = xs[0][0] + 1\nb = xs[0][0] * 2.5\nprint(a, b)\n", "8 17.5\n"},
		{"a bool slot is a number", "xs = []\nxs.append([True, 2])\nn = xs[0][0] + 1\nprint(n)\n", "2\n"},
		{"a dict value by key", "d = {}\nd[\"k\"] = 40\nn = d[\"k\"] + 2\nprint(n)\n", "42\n"},
		{"three levels deep", "xs = []\nxs.append([[7, 8]])\nn = xs[0][0][1] - 1\nprint(n)\n", "7\n"},
		{"two slots multiplied", built + "n = xs[0][0] * xs[0][1]\nprint(n)\n", "56\n"},
		{"bound between two prints", built + "print(0)\nn = xs[0][0] * 2\nprint(n)\nprint(9)\n", "0\n14\n9\n"},
		{"bound inside an if arm", built + "if 1 > 0:\n    n = xs[0][0] * 2\n    print(n)\n", "14\n"},
		{"bound inside a while arm", built + "i = 0\nwhile i < 1:\n    n = xs[0][0] * 2\n    print(n)\n    i = 1\n", "14\n"},
		{"two names bound from one container", built + "a = xs[0][0] * 2\nb = xs[0][1] * 3\nprint(a, b)\n", "14 24\n"},
		{"the answer in a print's second argument", built + "n = xs[0][0] * 2\nprint(\"n\", n)\n", "n 14\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			gy := writeSrc(t, dir, "pair_binding.gy", tc.src)
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

// TestARebindingRetiresTheTagAtTheCLI is the wrong-answer half this cycle found. A tagged variable's
// print dispatch reads the tag slot beside the value; a binding that is not a pair has to retire it, or
// the name goes on printing through the tag the arithmetic left behind. Before the rule was applied the
// compiled leg answered `[1, 2]` as `2` (the heap handle) and `2.5` as `0` — two engines agreeing on the
// reference's answer and the third quietly printing digits, which is the class ADR 0166 counts as the
// compiler's bug (roadmap Gap R.142).
func TestARebindingRetiresTheTagAtTheCLI(t *testing.T) {
	for _, tc := range []struct{ name, src, want string }{
		{"a plain number retires it", built + "n = xs[0][0] * 2\nn = 3\nprint(n)\n", "3\n"},
		{"a float retires it", built + "n = xs[0][0] * 2\nn = 2.5\nprint(n)\n", "2.5\n"},
		{"a text retires it", built + "n = xs[0][0] * 2\nn = \"hi\"\nprint(n)\n", "hi\n"},
		{"a list retires it", built + "n = xs[0][0] * 2\nn = [1, 2]\nprint(n)\n", "[1, 2]\n"},
		{"a set retires it", built + "n = xs[0][0] * 2\nn = {5, 6}\nprint(n)\n", "{5, 6}\n"},
		{"a dict retires it", built + "n = xs[0][0] * 2\nn = {\"a\": 1}\nprint(n)\n", "{'a': 1}\n"},
		{"a comprehension retires it", built + "n = xs[0][0] * 2\nn = [v for v in [1, 2]]\nprint(n)\n", "[1, 2]\n"},
		{"and a pair can come back", built + "n = xs[0][0] * 2\nn = 3\nn = xs[0][1] * 2\nprint(n)\n", "16\n"},
		{
			// The same rule the loop binding has always needed: ADR 0185 bound the loop variable with
			// a tag, and nothing after the loop ever took it away.
			"a loop variable's tag retires too", "xs = [1.5, \"a\"]\nfor v in xs:\n    print(v)\n\nv = [1, 2]\nprint(v)\n", "1.5\na\n[1, 2]\n",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			gy := writeSrc(t, dir, "pair_rebind.gy", tc.src)
			if py, ok := cpythonPlainOut(t, dir, tc.src); !ok || py != tc.want {
				t.Fatalf("the expectation is not the reference's: python said %q (ok %v), the row says %q\nsrc: %s", py, ok, tc.want, tc.src)
			}
			for _, engine := range []string{"--interp", "--aot"} {
				out, code := cliRunCode(t, engine, "--file", gy)
				if code == 2 {
					t.Fatalf("%s: exit 2 (ADR 0166):\n%s", engine, out)
				}
				if code != 0 || out != tc.want {
					t.Errorf("%s: exit %d, stdout %q, want %q — the tag outlived its binding\nsrc: %s", engine, code, out, tc.want, tc.src)
				}
			}
		})
	}
}

// TestAContainerRebindingRetiresTheTextBindingToo pins the sibling of Gap R.142 that this cycle
// did not bring in: an interned-text binding that outlives the binding which replaced it. Measured
// on the pre-cycle binary, so it is a row rather than a regression — see docs/roadmap-details.md.
// TestAContainerRebindingRetiresTheTextBindingToo is the filed-not-fixed sibling of Gap R.142 that this
// cycle did not bring in: an interned-text binding that outlives the binding which replaced it, measured
// on the pre-cycle binary and so a row rather than a regression. The assignment stores a container and
// leaves the earlier binding's interned text where the print dispatch reads it, at exit 0. The row is
// written to fail the day Gap Q.1's status table retires it. See docs/roadmap-details.md.
func TestAContainerRebindingRetiresTheTextBindingToo(t *testing.T) {
	src := "n = \"text\"\nn = [1, 2]\nprint(n)\n"
	dir := t.TempDir()
	gy := writeSrc(t, dir, "rebinding_of_text_with_a_container.gy", src)
	if py, ok := cpythonPlainOut(t, dir, src); !ok || py != "[1, 2]\n" {
		t.Fatalf("the reference is expected to print [1, 2], said %q (ok %v)", py, ok)
	}
	if out, code := cliRunCode(t, "--interp", "--file", gy); code != 0 || out != "[1, 2]\n" {
		t.Errorf("--interp: exit %d, stdout %q, want the reference's [1, 2]", code, out)
	}
	out, code := cliRunCode(t, "--aot", "--file", gy)
	if code == 2 {
		t.Fatalf("--aot: exit 2 (ADR 0166):\n%s", out)
	}
	if code != 0 || out != "text\n" {
		t.Errorf("--aot: exit %d, stdout %q, expected the filed Gap R.145 answer — the print dispatch's "+
			"interned-text record of the earlier binding, at exit 0", code, out)
	}
}

// TestABoundAnswerTrapsLikeTheReferenceAtTheCLI: the arithmetic the binding performs raises the
// reference's own sentence, at the reference's exit class, and the program can reach it with `except`.
func TestABoundAnswerTrapsLikeTheReferenceAtTheCLI(t *testing.T) {
	for _, tc := range []struct{ name, src, want string }{
		{
			"the negation of a text slot",
			"xs = []\nxs.append(\"hi\")\nn = -xs[0]\nprint(n)\n",
			"TypeError: bad operand type for unary -: 'str'",
		},
		{
			"subtraction under a text slot",
			"xs = []\nxs.append(\"hi\")\nn = xs[0] - 1\nprint(n)\n",
			"TypeError: unsupported operand type(s) for -: 'str' and 'int'",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			gy := writeSrc(t, dir, "pair_trap.gy", tc.src)
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

// TestABoundAnswerIsCatchableAtTheCLI is the raise run rather than merely emitted: the arithmetic helper
// fills a buffer and returns a status, and the *emitted* statement does the store-and-branch, which is
// what makes `except TypeError:` and `except OverflowError:` reach it (ADR 0228).
func TestABoundAnswerIsCatchableAtTheCLI(t *testing.T) {
	for _, tc := range []struct{ name, src, want string }{
		{
			"typeError caught",
			"xs = []\nxs.append(\"hi\")\ntry:\n    n = -xs[0]\n    print(n)\nexcept TypeError:\n    print(\"caught\")\n",
			"caught\n",
		},
		{
			"overflowError caught",
			built + "try:\n    n = xs[0][0] * 1000000000\n    print(n)\nexcept OverflowError:\n    print(\"caught\")\n",
			"caught\n",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			gy := writeSrc(t, dir, "pair_catch.gy", tc.src)
			out, code := cliRunCode(t, "--aot", "--file", gy)
			if code == 2 {
				t.Fatalf("--aot: exit 2 (ADR 0166):\n%s", out)
			}
			if code != 0 || out != tc.want {
				t.Errorf("--aot: exit %d, stdout %q, want %q", code, out, tc.want)
			}
		})
	}
}

// TestTheWholeNumberBeyondTheCompiledIntWordIsFiledNotFixed pins the one arithmetic the compiled word
// cannot hold, at the new door. The reference answers 7000000000 and so does the interpreted leg, whose
// ints are int64; the compiled int is 32 bits and its guard raises before the `fptosi`, because out of
// the word the truncation is poison rather than a wrong number (ADR 0264's rule, applied at the print
// door by ADR 0265 and here by ADR 0267). The decision that would settle it is roadmap L12.12's.
func TestTheWholeNumberBeyondTheCompiledIntWordIsFiledNotFixed(t *testing.T) {
	const src = built + "n = xs[0][0] * 1000000000\nprint(n)\n"
	const sentence = "OverflowError: the whole number the arithmetic would answer is beyond the word this backend's int holds (roadmap L12.12)"
	dir := t.TempDir()
	gy := writeSrc(t, dir, "pair_int_word.gy", src)
	if py, ok := cpythonPlainOut(t, dir, src); !ok || py != "7000000000\n" {
		t.Fatalf("the reference is expected to answer 7000000000, said %q (ok %v)", py, ok)
	}
	if out, code := cliRunCode(t, "--interp", "--file", gy); code != 0 || out != "7000000000\n" {
		t.Errorf("--interp: exit %d, stdout %q, want the reference's 7000000000", code, out)
	}
	out, code := cliReport(t, "--aot", "--file", gy)
	if code == 2 {
		t.Fatalf("--aot: exit 2 (ADR 0166):\n%s", out)
	}
	if code != 3 || !strings.Contains(out, sentence) {
		t.Errorf("--aot: exit %d, want 3 with the guard's sentence, said:\n%s", code, out)
	}
}

// TestAPairBoundNameRefusesThePositionsThePairDoesNotReachAtTheCLI is the half the row does not pay yet.
// Every row here is a program the reference answers and the interpreted leg answers too; the compiled
// leg declines it with the missing half named (exit 1), which is the honest class — and exit 2, the
// compiler's own code, is forbidden (ADR 0166). These are roadmap Gaps R.143 and R.144, and each row
// fails the day the position opens.
func TestAPairBoundNameRefusesThePositionsThePairDoesNotReachAtTheCLI(t *testing.T) {
	for _, tc := range []struct{ name, src, want string }{
		{"used as a number", built + "n = xs[0][0] * 2\nprint(n + 1)\n", "holds the answer of arithmetic over a slot"},
		{"negated", built + "n = xs[0][0] * 2\nprint(-n)\n", "holds the answer of arithmetic over a slot"},
		{"handed to abs", built + "n = xs[0][0] * 2\nprint(abs(n))\n", "holds the answer of arithmetic over a slot"},
		{"asked for its truth", built + "n = xs[0][0] * 2\nif n:\n    print(\"yes\")\n", "holds the answer of arithmetic over a slot"},
		{"as a while head", built + "n = xs[0][0] * 2\nwhile n > 0:\n    print(n)\n    n = 0\n", "holds the answer of arithmetic over a slot"},
		{"interpolated", built + "n = xs[0][0] * 2\nprint(f\"{n}\")\n", "holds the answer of arithmetic over a slot"},
		{"str() of it", built + "n = xs[0][0] * 2\nprint(str(n))\n", "holds the answer of arithmetic over a slot"},
		{"augmented assignment onto it", built + "n = xs[0][0] * 2\nn += 1\nprint(n)\n", "holds the answer of arithmetic over a slot"},
		{"unpacked from a tuple", built + "a, b = xs[0][0] + 1, xs[0][1] + 2\nprint(a)\n", "cannot reach into"},
		{"a sum of two names, each bound from a slot", built + "a = xs[0][0] + 1\nb = xs[0][1] + 1\nprint(a + b)\n", "holds the answer of arithmetic over a slot"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			gy := writeSrc(t, dir, "pair_refusal.gy", tc.src)
			// The reference and the interpreted leg answer the program; only the compiled leg reads
			// the pair door, so that is the leg whose refusal is pinned.
			if _, ok := cpythonPlainOut(t, dir, tc.src); !ok {
				t.Fatalf("the reference was expected to answer this program\nsrc: %s", tc.src)
			}
			if out, code := cliRunCode(t, "--interp", "--file", gy); code != 0 {
				t.Errorf("--interp: exit %d, want 0 (the interpreted leg answers this)\n%s", code, out)
			}
			out, code := cliReport(t, "--aot", "--file", gy)
			if code == 2 {
				t.Fatalf("--aot: exit 2 where the front end should refuse (ADR 0166):\n%s", out)
			}
			if code != 1 {
				t.Errorf("--aot: exit %d, want 1 (a program this backend declines to build)\n%s", code, out)
			}
			if !strings.Contains(out, tc.want) {
				t.Errorf("--aot refused without naming the missing half: wanted %q in\n%s", tc.want, out)
			}
		})
	}
}

// TestTheCorpusProgramStillPrintsWhatTheLedgerSays runs the promoted conformance file through both
// engines and the reference: programs/probe_arith_result_bound_to_a_name.gy left the debt ledger to
// become parity surface, and this is where that claim is checked rather than asserted.
func TestTheCorpusProgramStillPrintsWhatTheLedgerSays(t *testing.T) {
	const want = "14\n8\n-7\n5\n15\n15.0\n42\n3\n[1, 2]\n{5, 6}\n"
	raw, err := os.ReadFile("programs/probe_arith_result_bound_to_a_name.gy")
	if err != nil {
		t.Fatalf("the registered corpus file is missing: %v", err)
	}
	src := string(raw)
	dir := t.TempDir()
	gy := writeSrc(t, dir, "probe_arith_result_bound_to_a_name.gy", src)
	if py, ok := cpythonPlainOut(t, dir, src); !ok || py != want {
		t.Fatalf("the ledger's expectation is not the reference's: %q (ok %v)", py, ok)
	}
	for _, engine := range []string{"--interp", "--aot"} {
		out, code := cliRunCode(t, engine, "--file", gy)
		if code == 2 {
			t.Fatalf("%s: exit 2 (ADR 0166):\n%s", engine, out)
		}
		if code != 0 || out != want {
			t.Errorf("%s: exit %d, stdout %q, want %q", engine, code, out, want)
		}
	}
}
