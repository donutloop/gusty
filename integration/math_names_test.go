package integration

// integration/math_names_test.go — `floor`, `ceil`, `sqrt` at the CLI, against the reference, on both
// engines (roadmap L11.6, Gap R.51; ADR 0264).
//
// The reference does not have these names: it keeps them in the `math` module. So every case here runs
// the *same source twice* — gusty's, and CPython's with `from math import floor, ceil, sqrt` on the
// first line. That import line is the only difference between the two files, which is what makes the
// comparison a real one rather than a table of numbers I typed from memory; the conformance ledger
// carries the corpus file as `not_applicable` for exactly the spelling reason, and this file is where
// the reference gets to vote.
//
// What was wrong before, measured on both legs:
//
//	print(floor(3.7))   # math.floor 3 (an int) · --aot NameError, exit 3 · --aot 3.0
//	print(ceil(-0.5))   # math.ceil  0 (an int)  · --aot NameError        · --aot -0.0
//	print(sqrt(-1))     # ValueError: math domain error · --aot NameError · --aot nan
//	print(floor("a"))   # TypeError: must be real number, not str            · --aot 0.0
//
// Exit 2 — the contract's "the compiler is broken" code — fails any row here, including the trap and
// refusal tables, where a half-finished implementation is exactly what reaches for it.

import (
	"bytes"
	"strings"
	"testing"
)

// cpythonMathOut is the reference's answer for a source that uses the three builtin names, run with
// those names imported from where the reference keeps them. ok is false when the reference itself
// raised, and the text is then its diagnosis.
func cpythonMathOut(t *testing.T, dir, src string) (string, bool) {
	t.Helper()
	path := writeSrc(t, dir, "math_twin.py", "from math import floor, ceil, sqrt\n"+src)
	cmd := oracleCommand(t, path)
	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &out
	err := cmd.Run()
	return out.String(), err == nil
}

func TestTheWholeNumberBuiltinsAnswerLikeTheReferenceAtTheCLI(t *testing.T) {
	for _, tc := range []struct{ name, src, want string }{
		{"the row's own pair", "print(floor(3.7))\nprint(ceil(-2.2))\n", "3\n-2\n"},
		{"both directions, both signs",
			"print(floor(2.7))\nprint(floor(-2.7))\nprint(ceil(2.2))\nprint(ceil(-2.2))\n", "2\n-3\n3\n-2\n"},
		{"the answer is a whole number whatever arrived",
			"print(floor(7))\nprint(ceil(7))\nprint(floor(True))\nprint(ceil(True))\nprint(floor(5.0))\n", "7\n7\n1\n1\n5\n"},
		{"sqrt keeps the float answer",
			"print(sqrt(9))\nprint(sqrt(2))\nprint(sqrt(0))\nprint(sqrt(2.25))\n", "3.0\n1.4142135623730951\n0.0\n1.5\n"},
		{"the value in a name, the runtime road",
			"x = 3.7\nprint(floor(x), ceil(x))\ny = 9\nprint(sqrt(y))\n", "3 4\n3.0\n"},
		{"a whole number beside a float is widened, not truncated",
			"print(floor(2.7) + 1.5)\nprint(floor(2.7) * 3)\nprint(sqrt(16) * 2)\n", "3.5\n6\n8.0\n"},
		{"the answers travel: container, text, comparison, condition",
			"print([floor(2.7), ceil(2.2)])\nprint(str(floor(2.7)))\nprint(f\"{floor(2.7)}|{ceil(2.2)}\")\nprint(sqrt(4) == 2)\nif ceil(2.2) > 2:\n    print(\"many\")\n", "[2, 3]\n2\n2|3\nTrue\nmany\n"},
		{"across a call boundary",
			"def w(v):\n    return floor(v)\n\nprint(w(2.7) + 1)\n", "3\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			gy := writeSrc(t, dir, "math_names.gy", tc.src)
			if py, ok := cpythonMathOut(t, dir, tc.src); !ok || py != tc.want {
				t.Fatalf("the expectation is not the reference's: python said %q (ok %v), the row says %q\nsrc: %s", py, ok, tc.want, tc.src)
			}
			for _, engine := range cliEngines {
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

// TestTheWholeNumberBuiltinsTrapLikeTheReferenceAtTheCLI is the exit-code half. Every program here is
// one the reference runs and stops on, so the compiled path raise — exit 3, the runtime-error class, with
// the reference's own sentence — and neither refuses to build the program (exit 1, which belongs to
// programs the reference rejects) or crashes (exit 2).
func TestTheWholeNumberBuiltinsTrapLikeTheReferenceAtTheCLI(t *testing.T) {
	for _, tc := range []struct{ name, src, want string }{
		{"a negative constant under sqrt", "print(sqrt(-1))\n", "ValueError: math domain error"},
		{"a negative the program computed", "n = 0 - 4\nprint(sqrt(n * 1.0))\n", "ValueError: math domain error"},
		{"a text under floor", "print(floor(\"a\"))\n", "TypeError: must be real number, not str"},
		{"a text under sqrt", "print(sqrt(\"a\"))\n", "TypeError: must be real number, not str"},
		{"None under ceil", "print(ceil(None))\n", "TypeError: must be real number, not NoneType"},
		{"a list under floor", "print(floor([1]))\n", "TypeError: must be real number, not list"},
		// The two values a real number is not. The reference has a sentence for each, and this compiler
		// used to have a `nan` printed where CPython stops, and silence where it names the word.
		{"NaN under floor", "print(floor(float(\"nan\")))\n", "ValueError: cannot convert float NaN to integer"},
		{"an infinity under ceil", "print(ceil(float(\"inf\")))\n", "OverflowError: cannot convert float infinity to integer"},
		{"a negative infinity under sqrt", "n = float(\"-inf\")\nprint(sqrt(n))\n", "ValueError: math domain error"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			gy := writeSrc(t, dir, "math_trap.gy", tc.src)
			// The reference stops on the same program, with the same sentence.
			if py, ok := cpythonMathOut(t, dir, tc.src); ok || !strings.Contains(py, tc.want) {
				t.Fatalf("the reference was expected to stop with %q, said %q (ok %v)", tc.want, py, ok)
			}
			for _, engine := range cliEngines {
				out, code := cliReport(t, engine, "--file", gy)
				if code == 2 {
					t.Fatalf("%s: exit 2 for a program the reference raises on (ADR 0166):\n%s", engine, out)
				}
				if code != 3 {
					t.Errorf("%s: exit %d, want 3 (the runtime-error class)\n%s", engine, code, out)
				}
				if !strings.Contains(out, tc.want) {
					t.Errorf("%s: printed %q, want the reference's sentence %q", engine, out, tc.want)
				}
				// The two answers this row replaces were both silent: a value where the reference has
				// a raise (`nan`, `0.0`) and a program that carried on printing after it.
				for _, bad := range []string{"nan", "0.0"} {
					if strings.Contains(out, bad) {
						t.Errorf("%s: the engine answered %q where the reference raises:\n%s", engine, bad, out)
					}
				}
			}
		})
	}
	// A raise the program can name, on the compiled path.
	for _, tc := range []struct{ name, src, want string }{
		{"the domain caught", "try:\n    print(sqrt(-4))\nexcept ValueError:\n    print(\"caught\")\n", "caught\n"},
		{"the kind caught", "try:\n    print(floor(\"a\"))\nexcept TypeError:\n    print(\"caught\")\n", "caught\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			gy := writeSrc(t, dir, "math_catch.gy", tc.src)
			if py, ok := cpythonMathOut(t, dir, tc.src); !ok || py != tc.want {
				t.Fatalf("the reference's catch branch printed %q (ok %v), want %q", py, ok, tc.want)
			}
			for _, engine := range cliEngines {
				if out, code := cliRunCode(t, engine, "--file", gy); code != 0 || out != tc.want {
					t.Errorf("%s: exit %d, stdout %q, want the except branch on the compiled path", engine, code, out)
				}
			}
		})
	}
}

// TestTheWholeNumberBuiltinsSayTheirArityAtTheCLI pins the shapes that are typos in the program rather
// than programs the reference runs: one sentence, written once, exit 1 compiled and exit 3 interpreted,
// exit 2 nowhere. `floor()` was a Go panic in the evaluator, which is Gap R.131's class.
func TestTheWholeNumberBuiltinsSayTheirArityAtTheCLI(t *testing.T) {
	for _, tc := range []struct{ name, src, want string }{
		{"no argument to floor", "print(floor())\n", "floor expects 1 argument, none given"},
		{"two arguments to sqrt", "print(sqrt(1, 2))\n", "sqrt expects 1 argument, 2 given"},
		{"no argument to ceil", "print(ceil())\n", "ceil expects 1 argument, none given"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			gy := writeSrc(t, dir, "math_arity.gy", tc.src)
			if py, ok := cpythonMathOut(t, dir, tc.src); ok || !strings.Contains(py, "takes exactly one argument") {
				t.Fatalf("the reference stops on an arity mistake with its own sentence; said %q (ok %v)", py, ok)
			}
			out, code := cliReport(t, "--aot", "--file", gy)
			if code == 2 {
				t.Fatalf("compiled: exit 2 for a program with an arity mistake (ADR 0166):\n%s", out)
			}
			if code != 1 || !strings.Contains(out, tc.want) {
				t.Errorf("compiled: exit %d, output %q, want exit 1 saying %q", code, out, tc.want)
			}
			// Which class the arity mistake lands in is the checker's call, and it changed when the
			// second engine left: the retired interpreter discovered the missing argument when it reached
			// the call (a runtime trap, exit 3), and the compiled path never gets there because the
			// checker refuses the program first (exit 1). Both name the callee, the operator and the
			// count; that sentence is the contract this row checks. Exit 2 is never acceptable, and exit 0
			// — an answer to a call the reference rejects — is checked by the case beside this one.
			out, code = cliReport(t, "--aot", "--file", gy)
			if code == 2 {
				t.Fatalf("exit 2 — the toolchain was blamed for an arity mistake (ADR 0166):\n%s", out)
			}
			if code != 3 && code != 1 {
				t.Errorf("exit %d, want 3 (a runtime trap) or 1 (the checker refused it at the door); output %q", code, out)
			}
			if !strings.Contains(out, tc.want) {
				t.Errorf("the arity sentence is not named (%q): %q", tc.want, out)
			}
			if code == 1 {
				noteCompiledGap(t, tc.src, out)
			}
		})
	}
}

// TestWholeNumberBuiltinsCorpusRowMatchesTheTwin runs the conformance programs through the CLI against
// their own twin, so the files the ledger carries are the files the reference is consulted about. The
// twin is the same bytes with `from math import floor, ceil, sqrt` on the first line; the ledger row is
// `not_applicable` only because of that one line, and this is where the oracle actually votes.
func TestWholeNumberBuiltinsCorpusRowMatchesTheTwin(t *testing.T) {
	for _, name := range []string{"whole_number_builtins.gy", "non_finite_float_constant.gy"} {
		t.Run(name, func(t *testing.T) {
			src := readProgram(t, name)
			py, ok := cpythonMathOut(t, t.TempDir(), src)
			if !ok {
				t.Fatalf("the twin stopped: %s", py)
			}
			for _, engine := range cliEngines {
				gy := writeSrc(t, t.TempDir(), name, src)
				out, code := cliRunCode(t, engine, "--file", gy)
				if code == 2 {
					t.Fatalf("%s: the compiler's own module was rejected (ADR 0166):\n%s", engine, out)
				}
				if code != 0 || out != py {
					t.Errorf("%s: exit %d, stdout %q, want the reference's %q", engine, code, out, py)
				}
			}
		})
	}
}

// TestTheWholeNumberBuiltinsBeyondTheCompiledIntWordAreFiledNotSilent is the one place both legs
// are allowed to disagree, so it is pinned from both sides rather than averaged (roadmap Gap R.133).
// `fptosi` of a double outside the 32-bit word the compiled `int` occupies is not a wrong number, it is
// poison; the guard asks first and raises a catchable OverflowError naming L12.12. The evaluator's ints
// are int64, so it answers the reference's number — and a wrong answer in silence is Gap R.64's shape,
// which this row exists to avoid adding to.
func TestTheWholeNumberBuiltinsBeyondTheCompiledIntWordAreFiledNotSilent(t *testing.T) {
	const src = "print(floor(3000000000.0))\n"
	if py, ok := cpythonMathOut(t, t.TempDir(), src); !ok || py != "3000000000\n" {
		t.Fatalf("the reference answers the number; said %q (ok %v)", py, ok)
	}
	dir := t.TempDir()
	gy := writeSrc(t, dir, "wide.gy", src)
	// The wide answer is the debt: the reference prints 3000000000 and the compiled int word cannot
	// hold it. Answering is a pass; refusing with the missing half named is a filed pass; printing a
	// wrapped-around number at exit 0 is not a pass at all.
	if out, code := cliRunCode(t, "--aot", "--file", gy); code == 0 && out != "3000000000\n" {
		t.Errorf("exit 0 with %q where the reference answers 3000000000 — the wrapped number is the bug this row exists to catch", out)
	} else if code != 0 && !refusesHonestly(out) && code != 3 {
		t.Errorf("exit %d without naming the missing half: %s", code, out)
	}
	out, code := cliReport(t, "--aot", "--file", gy)
	if code == 2 {
		t.Fatalf("compiled: exit 2 (ADR 0166):\n%s", out)
	}
	if code != 3 || !strings.Contains(out, "OverflowError") || !strings.Contains(out, "L12.12") {
		t.Errorf("compiled: exit %d, output %q, want a catchable OverflowError naming L12.12", code, out)
	}
	// And the raise is one the program can name, which is what makes it a raise rather than a refusal.
	catch := "try:\n    print(floor(3000000000.0))\nexcept OverflowError:\n    print(\"caught\")\n"
	gy = writeSrc(t, dir, "wide_catch.gy", catch)
	if out, code := cliRunCode(t, "--aot", "--file", gy); code != 0 || out != "caught\n" {
		t.Errorf("compiled: exit %d, output %q, want the except branch (the raise must be catchable)", code, out)
	}
}
