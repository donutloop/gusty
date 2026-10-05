package integration

// integration/logic_value_test.go — `and`/`or` choose an operand at the CLI, against the reference, on both
// engines (roadmap Gap R.147, ADR 0269).
//
// Every row is the same source run three ways: CPython, `gustyc --file <path> --interp`, and `gustyc --file
// <path> --aot`. The legs are forced explicitly — a bare `--file` is the interpreter's default.
//
// Three classes of verdict are kept apart:
//
//   - a shape the reference answers prints the same bytes on every engine, at exit 0;
//   - a shape whose answer has no word is a *front-end refusal* on the compiled leg (exit 1, the contract's
//     capability class) while the reference and the interpreted leg answer — the honest direction to be
//     wrong, and the one that keeps `except`-level behaviour reachable rather than invented;
//   - exit 2 — the contract's "the compiler is broken" code — fails any row here, including the refusal
//     table, because a module `llc` rejects for an ordinary program is exactly what the old verdict lowering
//     was papering over (ADR 0166).
//
// The whole family also runs as one program, integration/programs/and_or_answer_like_python.gy, registered
// in the conformance corpus; the owed halves are the two probes beside it.

import (
	"strings"
	"testing"
)

// logicParity is one program the reference answers, with the bytes it prints.
func logicParity() []struct{ name, src, want string } {
	return []struct{ name, src, want string }{
		{"the row the defect was filed with", "print(2 and 3)\n", "3\n"},
		{"a falsy left operand keeps itself", "print(0 and 3)\n", "0\n"},
		{"the fallback an or takes", "print(0 or 5)\n", "5\n"},
		{"the text a run-time test chooses", "x = \"\"\nprint(x or \"d\")\n", "d\n"},
		{"the text a constant test chooses", "print(\"\" or \"d\")\n", "d\n"},
		{"the container a constant test chooses", "print([1] and [2])\n", "[2]\n"},
		{"the container an empty one falls back to", "print([] or [1, 2])\n", "[1, 2]\n"},
		{"a container variable beside a text", "xs = [1, 2]\nprint(xs or \"empty\")\n", "[1, 2]\n"},
		{"an empty dict takes the text", "d = {}\nprint(d or \"empty\")\n", "empty\n"},
		{"the double an or chooses", "print(0.0 or 2.5)\n", "2.5\n"},
		{"a float the int operand chose", "print(2 and 0.0)\n", "0.0\n"},
		{"the verdict an or takes", "print(False or True)\n", "True\n"},
		{"the verdict the left operand keeps", "print(True or 1)\n", "True\n"},
		{"the number the left operand keeps", "print(1 or True)\n", "1\n"},
		{"a verdict-bound name wins its own test", "x = True\nprint(x or 2)\n", "True\n"},
		{"None as the answer", "print(0 or None)\n", "None\n"},
		{"None as the test", "print(None or 3)\n", "3\n"},
		{"three operands two tests", "print(\"a\" and \"b\" and \"c\")\n", "c\n"},
		{"a chain of fallbacks", "print(0 or \"\" or \"x\")\n", "x\n"},
		{"inside arithmetic", "print((2 and 3) + 1)\nprint((0 or 2) * 3)\n", "4\n6\n"},
		{"as a container element", "x = 0\nprint([x or 1])\n", "[1]\n"},
		{"in a while head", "x = 0\nwhile (x or 3) < 5:\n    print(x)\n    x = x + 1\n", "0\n1\n2\n3\n4\n"},
		{"in an if head", "x = 0\nif x and 3:\n    print(\"yes\")\nelse:\n    print(\"no\")\n", "no\n"},
		{"the operand a comparison chose", "print((1 < 2) and (3 < 4))\n", "True\n"},
		{"a slot the literal describes", "ys = [True, 1]\nprint(ys[0] and ys[1])\n", "1\n"},
		{"a pair-bound name as the operand", "xs = []\nxs.append([7, 8])\nn = xs[0][0] * 2\nprint(n and 3)\n", "3\n"},
	}
}

// TestTheReferenceAndBothEnginesPrintTheSameOperand is the parity table: one source, three engines, the same
// bytes, no exit but 0.
func TestTheReferenceAndBothEnginesPrintTheSameOperand(t *testing.T) {
	for _, tc := range logicParity() {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			gy := writeSrc(t, dir, "logic_parity.gy", tc.src)
			py, ok := cpythonPlainOut(t, dir, tc.src)
			if !ok || py != tc.want {
				t.Fatalf("the reference said %q (ok %v), want %q\nsrc: %s", py, ok, tc.want, tc.src)
			}
			for _, engine := range []string{"--interp", "--aot"} {
				out, code := cliReport(t, engine, "--file", gy)
				if code == 2 {
					t.Fatalf("%s: the compiler's own module was rejected (ADR 0166):\n%s", engine, out)
				}
				if code != 0 {
					t.Fatalf("%s: exit %d, want 0\n%s", engine, code, out)
				}
				if out != tc.want {
					t.Errorf("%s: stdout %q, want %q\nsrc: %s", engine, out, tc.want, tc.src)
				}
			}
		})
	}
}

// TestTheCompiledLegRefusesWhatItCannotState is the other honest answer: where the chosen operand's kind is
// a run-time fact and the position keeps one word, the compiled leg spends exit 1 — the capability class —
// naming both operands and the missing word, while the reference and the interpreted leg answer.
func TestTheCompiledLegRefusesWhatItCannotState(t *testing.T) {
	for _, tc := range []struct{ name, src, want, interp string }{
		{
			"a text bound to a name",
			"x = 0\nz = x or \"d\"\nprint(z)\n",
			"chooses between two values", "d\n",
		},
		{
			"a float among integers",
			"x = 0\nprint((x or 2.5) * 2)\n",
			"`x or 2.5` chooses between two values", "5.0\n",
		},
		{
			"a container bound to a name",
			"x = 0\nys = x or [[1, 2]]\nprint(ys)\n",
			"chooses between two values", "[[1, 2]]\n",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			gy := writeSrc(t, dir, "logic_refusal.gy", tc.src)
			if out, code := cliReport(t, "--interp", "--file", gy); code != 0 || out != tc.interp {
				t.Fatalf("interpreted leg: exit %d stdout %q, want %q", code, out, tc.interp)
			}
			out, code := cliReport(t, "--aot", "--file", gy)
			if code == 2 {
				t.Fatalf("the compiled leg spent the compiler-is-broken class on an ordinary program (ADR 0166):\n%s", out)
			}
			if code != 1 {
				t.Fatalf("compiled leg exit %d, want 1 (the capability class)\n%s", code, out)
			}
			for _, want := range []string{tc.want, "roadmap L11.1", "Gap R.147"} {
				if !strings.Contains(out, want) {
					t.Errorf("refusal does not name %q:\n%s", want, out)
				}
			}
		})
	}
}

// TestTheParityProgramPrintsTheSameOnEveryLeg runs the corpus program itself, so a regression in one engine
// fails even when the tables above are read as unit-only coverage.
func TestTheParityProgramPrintsTheSameOnEveryLeg(t *testing.T) {
	src := readProgram(t, "and_or_answer_like_python.gy")
	dir := t.TempDir()
	gy := writeSrc(t, dir, "and_or_answer_like_python.gy", src)
	py, ok := cpythonPlainOut(t, dir, src)
	if !ok {
		t.Skipf("no python3 to act as the oracle")
	}
	for _, engine := range []string{"--interp", "--aot"} {
		out, code := cliReport(t, engine, "--file", gy)
		if code != 0 {
			t.Fatalf("%s: exit %d\n%s", engine, code, out)
		}
		if out != py {
			t.Errorf("%s disagrees with the reference:\n got %q\nwant %q", engine, out, py)
		}
	}
}

// TestTheOwedHalvesArePinnedAsTheyMeasure keeps the two rows this cycle filed rather than fixed honest: the
// legs are what the ledger says they are, and neither is exit 2.
func TestTheOwedHalvesArePinnedAsTheyMeasure(t *testing.T) {
	skips := readProgram(t, "probe_and_or_the_test_skips.gy")
	dir := t.TempDir()
	gy := writeSrc(t, dir, "probe_and_or_the_test_skips.gy", skips)
	want := "boom\n0\nboom\n1\nthe skipped operand raised\nthe skipped operand raised again\n"
	for _, engine := range []string{"--interp", "--aot"} {
		out, code := cliReport(t, engine, "--file", gy)
		if code != 0 {
			t.Fatalf("%s: exit %d\n%s", engine, code, out)
		}
		if out != want {
			t.Errorf("%s: the engines disagree with each other about the operand the test skipped:\n got %q\nwant %q", engine, out, want)
		}
	}
	py, ok := cpythonPlainOut(t, dir, skips)
	if ok && py == want {
		t.Errorf("the reference now behaves like both engines — Gap R.149 is paid and this row has to move to the parity table")
	}

	carry := readProgram(t, "probe_and_or_shapes_the_word_carry.gy")
	gy2 := writeSrc(t, dir, "probe_and_or_shapes_the_word_carry.gy", carry)
	if out, code := cliReport(t, "--aot", "--file", gy2); code != 1 {
		t.Fatalf("the compiled leg was expected to refuse the one-word positions (exit 1), got %d:\n%s", code, out)
	}
}
