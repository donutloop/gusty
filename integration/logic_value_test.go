package integration

// integration/logic_value_test.go — `and`/`or` choose an operand at the CLI, against the reference, on both
// engines (roadmap Gap R.147, ADR 0269).
//
// Every row is the same source run three ways: CPython, `gustyc --file <path> --aot`, and `gustyc --file
// <path> --aot`. The legs are forced explicitly — a bare `--file` is the interpreter's default.
//
// Three classes of verdict are kept apart:
//
//   - a shape the reference answers prints the same bytes on the compiled path, at exit 0;
//   - a shape whose answer has no word is a *front-end refusal* on the compiled leg (exit 1, the contract's
//     capability class) while the reference and the reference answer — the honest direction to be
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
			for _, engine := range cliEngines {
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
// naming both operands and the missing word, while the reference and the reference answer.
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
			// The reference's answer is the row's claim; the compiled leg owes those bytes or an honest
			// refusal, and the refusal's wording is what the rest of this case checks.
			out0, code0 := cliReport(t, "--aot", "--file", gy)
			checkCompiledRow(t, out0, code0, tc.src, tc.interp)
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
	for _, engine := range cliEngines {
		out, code := cliReport(t, engine, "--file", gy)
		if code != 0 {
			t.Fatalf("%s: exit %d\n%s", engine, code, out)
		}
		if out != py {
			t.Errorf("%s disagrees with the reference:\n got %q\nwant %q", engine, out, py)
		}
	}
}

// TestTheOwedHalvesArePinnedAsTheyMeasure keeps the rows this family filed rather than fixed honest: the legs
// are what the ledger says they are, and neither is exit 2. The operand the test skipped is not one of them
// any more — that program is promoted parity surface (`TestTheShortCircuitProgramPrintsTheSameOnEveryLeg`),
// and the ledger row that pinned the compiled path evaluating it is gone with ADR 0275.
func TestTheOwedHalvesArePinnedAsTheyMeasure(t *testing.T) {
	carry := readProgram(t, "probe_and_or_shapes_the_word_carry.gy")
	dir := t.TempDir()
	gy2 := writeSrc(t, dir, "probe_and_or_shapes_the_word_carry.gy", carry)
	if out, code := cliReport(t, "--aot", "--file", gy2); code != 1 {
		t.Fatalf("the compiled leg was expected to refuse the one-word positions (exit 1), got %d:\n%s", code, out)
	}
}

// TestTheShortCircuitProgramPrintsTheSameOnEveryLeg runs the promoted program: the operand the test did not
// choose is not in the program, so `boom` is printed only for the operand the test reached, the division the
// test skipped never traps, and the operand it does reach runs exactly once (roadmap Gap R.149, ADR 0275).
func TestTheShortCircuitProgramPrintsTheSameOnEveryLeg(t *testing.T) {
	src := readProgram(t, "probe_and_or_the_test_skips.gy")
	dir := t.TempDir()
	gy := writeSrc(t, dir, "probe_and_or_the_test_skips.gy", src)
	py, ok := cpythonPlainOut(t, dir, src)
	if !ok {
		t.Skipf("no python3 to act as the oracle")
	}
	// The bytes the reference prints are the whole claim, and they name the absence: exactly two `boom`
	// lines, for the two operands the tests reached, and no `then`, no `loop`, no traceback.
	if want := "0\n1\n0\n1\nor-then\nloop ended\n0\n1\nthe chosen operand raised\nthe other chosen operand raised\nboom\n2\nboom\n9\n"; py != want {
		t.Fatalf("the reference said %q, want %q", py, want)
	}
	for _, engine := range cliEngines {
		out, code := cliReport(t, engine, "--file", gy)
		if code == 2 {
			t.Fatalf("%s: the compiler's own module was rejected (ADR 0166):\n%s", engine, out)
		}
		if code != 0 {
			t.Fatalf("%s: exit %d\n%s", engine, code, out)
		}
		if out != py {
			t.Errorf("%s disagrees with the reference:\n got %q\nwant %q", engine, out, py)
		}
		if strings.Count(out, "boom\n") != 2 {
			t.Errorf("%s ran the operand the test skipped: %q", engine, out)
		}
		if strings.Contains(out, "then\n") && !strings.Contains(out, "or-then") {
			t.Errorf("%s entered the branch the test refused: %q", engine, out)
		}
	}
}

// shortCircuitParity is the family asked one shape at a time, so a regression names the shape rather than
// the program.
func shortCircuitParity() []struct{ name, src, want string } {
	const boom = "def boom():\n    print(\"boom\")\n    return 9\n\n"
	return []struct{ name, src, want string }{
		{"a falsy test skips a call", boom + "x = 0\nprint(x and boom())\n", "0\n"},
		{"a truthy test skips the fallback", boom + "y = 1\nprint(y or boom())\n", "1\n"},
		{"a skipped division does not trap", "x = 0\nprint(x and (1 // 0))\n", "0\n"},
		{"a skipped division on the other side", "y = 1\nprint(y or (1 // 0))\n", "1\n"},
		{"a binding skips too", boom + "x = 0\nv = x and boom()\nprint(v)\n", "0\n"},
		{"a binding keeps what the test chose", boom + "y = 1\nw = y or boom()\nprint(w)\n", "1\n"},
		{"an if head does not run the skipped operand", boom + "x = 0\nif x and boom():\n    print(\"then\")\nprint(\"done\")\n", "done\n"},
		{"an or head does not run the skipped operand", boom + "y = 1\nif y or boom():\n    print(\"taken\")\n", "taken\n"},
		{"a while head never enters", boom + "x = 0\nwhile x and boom():\n    print(\"loop\")\nprint(\"ended\")\n", "ended\n"},
		{"the operand the test reaches runs once", boom + "print(boom() and 2)\n", "boom\n2\n"},
		{"the tested operand runs once", boom + "print(boom() or 2)\n", "boom\n9\n"},
		{"twice in one line, twice in the module", boom + "x = 0\nprint(x and boom(), x and boom())\n", "0 0\n"},
		{"a chain runs only what it reaches", boom + "x = 0\ny = 0\nprint(x or y or boom())\n", "boom\n9\n"},
		{"a chain stops at the first answer", boom + "y = 2\nprint(y or boom() or 3)\n", "2\n"},
		{"a condition chain skips the second test", boom + "x = 0\ny = 1\nif x and boom() and y:\n    print(\"y\")\nelse:\n    print(\"n\")\n", "n\n"},
		{"a skipped print inside the skipped operand", boom + "x = 0\nprint(x and (boom() + 1))\n", "0\n"},
		{"a truthy text skips the fallback", boom + "s = \"x\"\nprint(s or boom())\n", "x\n"},
		{"a truthy container skips the fallback", boom + "xs = [1]\nprint(xs or boom())\n", "[1]\n"},
		{"None skips the fallback", boom + "print(None and boom())\n", "None\n"},
		{"a call's own arguments still run", boom + "def pick(a, b):\n    return a and b\n\nprint(pick(0, boom()))\n", "boom\n0\n"},
	}
}

// TestTheReferenceShortCircuitsAndSoDoBothEngines is the three-engine row: one source, the reference's bytes,
// the reference and the compiled leg (roadmap Gap R.149, ADR 0275). A leg that evaluated the skipped
// operand prints an extra `boom` line, and a leg that evaluated the tested operand twice prints it twice —
// both are caught by comparing bytes with the reference, which is why the expected text is the reference's
// and not the compiler's.
func TestTheReferenceShortCircuitsAndSoDoBothEngines(t *testing.T) {
	for _, tc := range shortCircuitParity() {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			gy := writeSrc(t, dir, "logic_shortcircuit.gy", tc.src)
			py, ok := cpythonPlainOut(t, dir, tc.src)
			if !ok || py != tc.want {
				t.Fatalf("the reference said %q (ok %v), want %q\nsrc: %s", py, ok, tc.want, tc.src)
			}
			for _, engine := range cliEngines {
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
