package integration

// integration/abs_kind_test.go — `abs` answers with its operand's kind at the CLI, on the compiled path, against
// the reference (roadmap Gap R.140, ADR 0271).
//
// Every row is the same source run three ways: CPython, `gustyc --file <path> --aot`, and `gustyc --file
// <path> --aot`. The legs are forced explicitly — a bare `--file` is the interpreter's default.
//
// Two verdict classes are kept apart, because confusing them is how the row stayed open for four milestones:
//
//   - a shape the reference answers prints the same bytes on the compiled path, at exit 0;
//   - a shape the reference *traps* — `TypeError: bad operand type for abs(): '<kind>'` — traps on every
//     engine: the same sentence, exit 3 (ADR 0166's trap class), catchable by `except TypeError:`.
//
// Before this cycle the second class printed the operand (`abs("hi")` → `hi`), answered the number `0`
// (`abs(None)`), or — for the three container kinds — asked the host to subtract a heap pointer from zero
// and the module was rejected by `llc`: **exit 2**, the contract's unforgivable answer (ADR 0166).

import (
	"strings"
	"testing"
)

// absNumbers are the shapes the reference answers, so the compiled path must answer them identically.
func absNumbers() []struct{ name, src, want string } {
	return []struct{ name, src, want string }{
		{"the row's own answer", "print(abs(-10))\n", "10\n"},
		{"a positive integer keeps itself", "print(abs(10))\n", "10\n"},
		{"the double a float gets", "print(abs(-3.5))\n", "3.5\n"},
		{"a positive double keeps itself", "print(abs(3.5))\n", "3.5\n"},
		{"a verdict answers its number", "print(abs(True))\nprint(abs(False))\n", "1\n0\n"},
		{"an expression in the operand", "print(abs(3 - 10))\n", "7\n"},
		{"a name bound to a number", "x = -7\nprint(abs(x))\n", "7\n"},
		{"a rebinding retires the earlier kind", "x = \"text\"\nx = -7\nprint(abs(x))\n", "7\n"},
		{"a number the name holds by a rebinding alone", "x = \"text\"\nx = -10\nprint(abs(-x))\n", "10\n"},
		{"a slot the literal describes", "xs = [-2, 3]\nprint(abs(xs[0]))\n", "2\n"},
		{"in arithmetic", "print(abs(-4) + abs(-0.5) * 2)\n", "5.0\n"},
	}
}

// absTraps are the shapes the reference refuses to answer with a number: every one is a TypeError naming the
// operand's kind, on the compiled path.
func absTraps() []struct{ name, src, kind string } {
	return []struct{ name, src, kind string }{
		{"text", "print(abs(\"hi\"))\n", "str"},
		{"an interpolated text", "n = 1\nprint(abs(f\"{n}\"))\n", "str"},
		{"nothing", "print(abs(None))\n", "NoneType"},
		{"a list", "print(abs([1, 2]))\n", "list"},
		{"an empty list", "print(abs([]))\n", "list"},
		{"a dict", "print(abs({\"a\": 1}))\n", "dict"},
		{"a set", "print(abs({1, 2}))\n", "set"},
		{"a name bound to a text", "s = \"hi\"\nprint(abs(s))\n", "str"},
		{"a name bound to a list", "xs = [1, 2]\nprint(abs(xs))\n", "list"},
		{"an instance", "class Thing:\n    pass\n\nprint(abs(Thing()))\n", "Thing"},
		{"an instance bound to a name", "class Thing:\n    pass\n\nt = Thing()\nprint(abs(t))\n", "Thing"},
	}
}

// TestTheReferenceAndBothEnginesAnswerAbsWithTheNumber is the parity table for the shapes with an answer.
func TestTheReferenceAndBothEnginesAnswerAbsWithTheNumber(t *testing.T) {
	for _, tc := range absNumbers() {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			gy := writeSrc(t, dir, "abs_parity.gy", tc.src)
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

// TestTheReferenceAndBothEnginesTrapAbsTheSameWay is the trap table: the reference's sentence, on both
// engines, at the trap exit — never a printed operand, never the number zero, never the compiler's own exit.
func TestTheReferenceAndBothEnginesTrapAbsTheSameWay(t *testing.T) {
	for _, tc := range absTraps() {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			gy := writeSrc(t, dir, "abs_trap.gy", tc.src)
			sentence := "TypeError: bad operand type for abs(): '" + tc.kind + "'"
			py, ok := cpythonPlainOut(t, dir, tc.src)
			if ok {
				t.Fatalf("the reference answered %q, expected the trap\nsrc: %s", py, tc.src)
			}
			if !strings.Contains(py, sentence) {
				t.Fatalf("the reference did not say %q: %q\nsrc: %s", sentence, py, tc.src)
			}
			for _, engine := range cliEngines {
				out, code := cliReport(t, engine, "--file", gy)
				if code == 2 {
					t.Fatalf("%s: the compiler's own module was rejected (ADR 0166):\n%s", engine, out)
				}
				if code != 3 {
					t.Fatalf("%s: exit %d, want the trap exit 3 (ADR 0166)\n%s", engine, code, out)
				}
				if !strings.Contains(out, sentence) {
					t.Errorf("%s: did not say %q, said:\n%s", engine, sentence, out)
				}
			}
		})
	}
}

// TestAnAbsTrapIsCaughtByAnExceptTypeError is the other half of why a trap raises rather than refuses: the
// contract's exit 1 (ADR 0166) is a compile-time verdict and escapes the handler, so a source the reference
// can catch must be catchable on the compiled path.
func TestAnAbsTrapIsCaughtByAnExceptTypeError(t *testing.T) {
	for _, tc := range []struct{ name, src, want string }{
		{"text", "try:\n    print(abs(\"hi\"))\nexcept TypeError:\n    print(\"caught\")\n", "caught\n"},
		{"nothing", "try:\n    print(abs(None))\nexcept TypeError:\n    print(\"caught\")\n", "caught\n"},
		{"a list", "try:\n    print(abs([1, 2]))\nexcept TypeError:\n    print(\"caught\")\n", "caught\n"},
		{"a dict", "try:\n    print(abs({\"a\": 1}))\nexcept TypeError:\n    print(\"caught\")\n", "caught\n"},
		{"a loop of them", "for s in [\"a\", 1, \"b\"]:\n    try:\n        print(abs(s))\n    except TypeError:\n        print(\"nope\")\n", "nope\n1\nnope\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			gy := writeSrc(t, dir, "abs_catch.gy", tc.src)
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

// TestTheAbsTrapDoesNotAskTheHostForArithmeticOnAPointer is the half that was exit 2: the three container
// kinds used to emit the negation's `sub i32 0, <heap pointer>` and `llc` rejected the module, so the
// program never reached the trap it was owed. The module must verify (the CLI's own `--verify-llvm-file`
// path, ADR 0166's exit-2 prohibition) and the run must leave through the trap.
func TestTheAbsTrapDoesNotAskTheHostForArithmeticOnAPointer(t *testing.T) {
	for _, tc := range []struct{ name, src, kind string }{
		{"a list", "print(abs([1, 2]))\n", "list"},
		{"a dict", "print(abs({\"a\": 1}))\n", "dict"},
		{"a set", "print(abs({1, 2}))\n", "set"},
		{"an instance", "class Thing:\n    pass\n\nprint(abs(Thing()))\n", "Thing"},
		{"an instance of a named class", "class Thing:\n    pass\n\nt = Thing()\nprint(abs(t))\n", "Thing"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			gy := writeSrc(t, dir, "abs_verify.gy", tc.src)
			out, code := cliReport(t, "--verify-llvm-file", gy)
			if code == 2 {
				t.Fatalf("the compiler's own module was rejected (ADR 0166):\n%s", out)
			}
			if code != 0 || !strings.Contains(out, "module verified") {
				t.Fatalf("LLVM's module verifier did not pass the program (exit %d):\n%s", code, out)
			}
			out, code = cliReport(t, "--aot", "--file", gy)
			if code == 2 {
				t.Fatalf("the compiler's own module was rejected (ADR 0166):\n%s", out)
			}
			if code != 3 {
				t.Fatalf("exit %d, want the trap exit 3\n%s", code, out)
			}
			if want := "TypeError: bad operand type for abs(): '" + tc.kind + "'"; !strings.Contains(out, want) {
				t.Errorf("did not name the operand's kind (%q):\n%s", want, out)
			}
		})
	}
}
