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

// TestAContainerRebindingRetiresTheTextBindingToo is the row Gap R.145 filed and ADR 0270 paid. It used to
// be a filed-not-fixed table pinning the compiled leg's `text` — the print dispatch reading the
// interned-text record the earlier binding left behind, at exit 0, beside the interpreter and CPython
// printing `[1, 2]`. It now asserts the reference's answer on both legs, and fails the day a binding path
// starts leaving a status behind again (roadmap Gap R.145, ADR 0270).
func TestAContainerRebindingRetiresTheTextBindingToo(t *testing.T) {
	for _, tc := range []struct{ name, src, want string }{
		{"a container over a text", "n = \"text\"\nn = [1, 2]\nprint(n)\n", "[1, 2]\n"},
		{"a dict over a text", "n = \"text\"\nn = {\"a\": 1}\nprint(n)\n", "{'a': 1}\n"},
		{"a number over a text", "n = \"text\"\nn = 5\nprint(n)\n", "5\n"},
		{"None over a text", "n = \"text\"\nn = None\nprint(n)\n", "None\n"},
		{"a text over a number", "n = 5\nn = \"hi\"\nprint(n)\n", "hi\n"},
		{"a text keeps what it needs", "n = \"a\"\nn = \"abc\"\nprint(len(n))\n", "3\n"},
		{"the arithmetic asks the new value", "n = \"text\"\nn = 5\nprint(n * 2)\n", "10\n"},
		{"the method asks the new value", "n = 5\nn = \"abc\"\nprint(n.upper())\n", "ABC\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			gy := writeSrc(t, dir, "rebinding.gy", tc.src)
			if py, ok := cpythonPlainOut(t, dir, tc.src); !ok || py != tc.want {
				t.Fatalf("the expectation is not the reference's: python said %q (ok %v), the row says %q\nsrc: %s", py, ok, tc.want, tc.src)
			}
			for _, engine := range []string{"--interp", "--aot"} {
				out, code := cliRunCode(t, engine, "--file", gy)
				if code == 2 {
					t.Fatalf("%s: exit 2 (ADR 0166):\n%s", engine, out)
				}
				if code != 0 || out != tc.want {
					t.Errorf("%s: exit %d, stdout %q, want the reference's %q — a status outlived its binding\nsrc: %s", engine, code, out, tc.want, tc.src)
				}
			}
		})
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

// TestAPairBoundNameAnswersWhereverANumberIsAskedAtTheCLI is roadmap Gap R.143 paid at the CLI: the
// positions that ask for one static number — an operand, a condition's head, a format field, the target
// of an augmented assignment — now ask the pair, on both engines, against the reference.
func TestAPairBoundNameAnswersWhereverANumberIsAskedAtTheCLI(t *testing.T) {
	for _, tc := range []struct{ name, src, want string }{
		{"an operand of a sum", built + "n = xs[0][0] * 2\nprint(n + 1)\n", "15\n"},
		{"the sum of two pair-bound names", built + "a = xs[0][0] + 1\nb = xs[0][1] + 1\nprint(a + b)\n", "17\n"},
		{"negated", built + "n = xs[0][0] * 2\nprint(-n)\n", "-14\n"},
		{"asked for its truth", built + "n = xs[0][0] * 2\nif n:\n    print(\"yes\")\n", "yes\n"},
		{"a zero answers false", built + "n = xs[0][0] * 0\nif n:\n    print(\"yes\")\nelse:\n    print(\"no\")\n", "no\n"},
		{"a while head", built + "n = xs[0][0] * 2\nwhile n > 0:\n    print(n)\n    n = 0\n", "14\n"},
		{"ordered against a number", built + "n = xs[0][0] * 2\nprint(n > 13)\nprint(13 > n)\n", "True\nFalse\n"},
		{"ordered against an int variable", built + "n = xs[0][0] * 2\nk = 3\nprint(n > k)\n", "True\n"},
		{"a compound condition", built + "n = xs[0][0] * 2\nif n > 1 and n < 20:\n    print(\"mid\")\n", "mid\n"},
		{"a condition's head", built + "n = xs[0][0] * 2\nprint(1 if n > 1 else 0)\n", "1\n"},
		{"interpolated", built + "n = xs[0][0] * 2\nprint(f\"{n}\")\n", "14\n"},
		{"interpolated with text around it", built + "n = xs[0][0] * 2\nprint(f\"v={n}!\")\n", "v=14!\n"},
		{"str() of it", built + "n = xs[0][0] * 2\nprint(str(n))\n", "14\n"},
		{"repr() of it", built + "n = xs[0][0] * 2\nprint(repr(n))\n", "14\n"},
		{"str() joins another text", built + "n = xs[0][0] * 2\nprint(str(n) + \"!\")\n", "14!\n"},
		{"the float family keeps its digits", built + "n = xs[0][0] * 2.5\nprint(str(n))\nprint(n > 17)\n", "17.5\nTrue\n"},
		{"augmented assignment onto it", built + "n = xs[0][0] * 2\nn += 1\nprint(n)\n", "15\n"},
		{"augmented product onto it", built + "n = xs[0][0] * 2\nn *= 2\nprint(n)\n", "28\n"},
		{"augmented with a double", built + "n = xs[0][0] * 2\nn += 0.5\nprint(n)\n", "14.5\n"},
		{"read again after the rebinding", built + "n = xs[0][0] * 2\nn += 1\nprint(n > 14)\n", "True\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			gy := writeSrc(t, dir, "pair_number.gy", tc.src)
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

// TestThePairRoadStillRefusesThePositionsThatTakeAValueAtTheCLI files what this cycle did not open, with
// each engine's answer written into the row. Two shapes are owed and are named here rather than answered
// wrongly: a position that takes a whole *value* — a builtin's argument, a container's element, an `and`'s
// operand — has nowhere to put the tag (that is the same missing word Gap R.139 names on the calling
// side); and a pair-bound name that enters the float domain beside a variable, or against a text, or
// through `/`, is refused by the road it takes rather than answered by the pair. A tuple unpacking is the
// third family and is Gap R.144's own row. Exit 2 is forbidden in every row (ADR 0166): the float road
// stores a double into the i32 slot a tagged name owns, which is the module `llc` rejects, so these
// shapes must stay refusals until the pair reaches them.
func TestThePairRoadStillRefusesThePositionsThatTakeAValueAtTheCLI(t *testing.T) {
	for _, tc := range []struct{ name, src, want string }{
		{"handed to abs", built + "n = xs[0][0] * 2\nprint(abs(n))\n", "holds the answer of arithmetic over a slot"},
		{"handed to min", built + "n = xs[0][0] * 2\nprint(min(n, 3))\n", "holds the answer of arithmetic over a slot"},
		{"an element of a list", built + "n = xs[0][0] * 2\nprint([n])\n", "holds the answer of arithmetic over a slot"},
		{"divided by a literal", built + "n = xs[0][0] * 2\nprint(n / 4)\n", "cannot be compiled"},
		{"ordered against a float variable", built + "n = xs[0][0] * 2\nd = 2.5\nprint(n > d)\n", "cannot be compiled"},
		{"unpacked from a tuple", built + "a, b = xs[0][0] + 1, xs[0][1] + 2\nprint(a)\n", "cannot reach into"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			gy := writeSrc(t, dir, "pair_value_position.gy", tc.src)
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
