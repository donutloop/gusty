package lang

import (
	"strings"
	"testing"
)

// Artifact-level coverage for roadmap Gap R.45 (ADR 0225): a subscript of a string is a
// one-character string, counted in code points — on both backends. Both used to answer the *byte*:
// the record returned an int and the codegen fold emitted `%d` of that int, so the wrong type
// spread to everything the value touched — `s[0] + s[2]` did arithmetic, `s[1] == "b"` said false,
// and `len(s[1])`, `s[1].upper()`, `ord(s[1])` trapped.

func compiledPrints(t *testing.T, src string) string {
	t.Helper()
	return captureStdout(t, src)
}

func TestStringSubscriptIsTextOnTheCompiledBackend(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct{ src, want string }{
		{"s = \"abc\"\nprint(s[1])\n", "b\n"},
		{"s = \"abc\"\nprint(s[-1])\n", "c\n"},
		{"s = \"abc\"\nprint(1 if s[1] == \"b\" else 0)\n", "1\n"},
		{"s = \"abc\"\nprint(1 if s[1] == 98 else 0)\n", "0\n"},
		{"s = \"abc\"\nprint(s[0] + s[2])\n", "ac\n"},
		{"s = \"abc\"\nprint(len(s[1]))\n", "1\n"},
		{"s = \"abc\"\nprint(s[1].upper())\n", "B\n"},
		{"s = \"abc\"\nprint(ord(s[1]))\n", "98\n"},
		{"s = \"abc\"\nxs = [s[1]]\nprint(xs[0])\n", "b\n"},
		{"d = {\"k\": \"abc\"}\nprint(d[\"k\"][1])\n", "b\n"},
		// code points, not bytes: the answer the old byte index gave was 195 for café[1].
		{"s = \"café\"\nprint(s[1])\nprint(s[-1])\nprint(len(s))\n", "a\né\n4\n"},
		{"s = \"café\"\nprint(ord(s[3]))\n", "233\n"},
	} {
		if got := compiledPrints(t, tc.src); got != tc.want {
			t.Fatalf("%q printed %q, want %q", tc.src, got, tc.want)
		}
	}
}

func TestStringSubscriptIsTextInCompiledCode(t *testing.T) {
	t.Parallel()
	for _, src := range []string{
		"s = \"abc\"\nprint(s[1])\n",
		"s = \"abc\"\nprint(s[-1])\n",
		"s = \"abc\"\nprint(1 if s[1] == \"b\" else 0)\n",
		"s = \"abc\"\nxs = [s[1]]\nprint(xs[0])\n",
	} {
		res, err := Compile(src)
		if err != nil {
			t.Fatalf("%q refused: %v", src, err)
		}
		if _, verr := VerifyModuleIR(res.IR, 0); verr != nil {
			t.Fatalf("%q does not verify: %v", src, verr)
		}
		if !strings.Contains(res.IR, "rt_str_intern2") {
			t.Fatalf("%q did not intern the character; printing it would show an index:\n%s", src, res.IR)
		}
	}
}

// TestStringSubscriptTrapIsTyped: an out-of-range character subscript is the program's mistake, and
// the record says so with the class CPython uses, not an untyped message.
func TestStringSubscriptTrapIsTyped(t *testing.T) {
	t.Parallel()
	_, _, err := evalGolden(t, "s = \"abc\"\nprint(s[9])\n")
	ee, ok := err.(*TrapError)
	if !ok || ee.ExnType != "IndexError" {
		t.Fatalf("expected a typed IndexError, got %#v", err)
	}
}

// TestUnlowerableConditionIsAnErrorNotAFalseBranch is the regression that mattered most here.
// truthyValue used to answer `asI1(b, "0")` when its condition could not be lowered, on the theory
// that "the enclosing statement path still has the error" — but the ternary path had no error
// channel, so `print(1 if s[1] == "b" else 0)` compiled into `icmp ne i32 0, 0` and printed 0. The
// program looked answered. A part of a program that cannot be lowered is a compile error (ADR 0166).
//
// The two rows that used to stand here were `s[9]` over a three-character text. They are not
// unlowerable any more: ADR 0326 turned the constant out-of-range read into the IndexError the reference
// raises (roadmap Gap R.37), so they moved to TestOutOfRangeConstantConditionRaisesBelowTheTernary
// below, where the claim is that the condition *traps* rather than answering. What stays here is a
// condition this backend still cannot build at all.
func TestUnlowerableConditionIsAnErrorNotAFalseBranch(t *testing.T) {
	t.Parallel()
	for _, src := range []string{
		// `len` of a number has no lowering here, and a ternary whose condition cannot be built must
		// say so — the shape this test exists for, since ADR 0229 took the run-time subscript off the
		// list and ADR 0326 took the out-of-range constant read to the runtime.
		"s = \"abc\"\nprint(1 if len(5) == 1 else 0)\n",
	} {
		res, err := Compile(src)
		if err == nil {
			out := ""
			for _, d := range res.Diagnostics {
				out += d.Msg + "\n"
			}
			t.Fatalf("%q compiled into a condition nobody lowered; it must refuse. diagnostics:\n%s", src, out)
		}
		if strings.Contains(err.Error(), "unexpected") || strings.Contains(err.Error(), "panic") {
			t.Fatalf("%q failed with a crash-flavoured message: %v", src, err)
		}
	}
}

// TestOutOfRangeConstantConditionRaisesBelowTheTernary is the same program family read from the other
// side: a condition with a position that does not exist does not quietly take the `else` arm. The
// reference dies, the record died, and the compiled leg now dies too — with the sentence in the
// condition's own source position, catchable by the program's `except IndexError:`
// (roadmap Gap R.37, ADR 0326).
func TestOutOfRangeConstantConditionRaisesBelowTheTernary(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct{ src, class, want string }{
		{"s = \"abc\"\nprint(1 if s[9] == \"b\" else 0)\n", "IndexError", "string index out of range"},
		{"print(1 if [1, 2][5] < 2 else 0)\n", "IndexError", "list index out of range"},
	} {
		if _, err := Compile(tc.src); err != nil {
			t.Fatalf("%q was refused at the compile door; a position that does not exist is a runtime trap: %v", tc.src, err)
		}
		class, msg := jitTrap(t, tc.src)
		if class != tc.class {
			t.Fatalf("%q: class = %q, want %q", tc.src, class, tc.class)
		}
		if !strings.Contains(msg, tc.want) {
			t.Fatalf("%q: message = %q, want it to read %q", tc.src, msg, tc.want)
		}
	}
	// And the raise is a language event: the program's own handler catches it.
	out, err := RunSource("s = \"abc\"\ntry:\n    print(1 if s[9] == \"b\" else 0)\nexcept IndexError:\n    print(\"caught\")\n")
	if err != nil {
		t.Fatalf("the ternary's trap did not reach the handler: %v", err)
	}
	if out != "caught\n" {
		t.Fatalf("stdout = %q, want the handler to have run", out)
	}
}

// jitTrap runs a program through the module and reports the class and sentence the emitted trap raised.
// The runtime owns those words (rt_die writes CPython's `Class: message`), and ParseTrapReport is the
// host's half of reading them back, so an assertion here is about the program's exception and not about
// the compiler's opinion of it (roadmap ADR 0214, ADR 0326).
func jitTrap(t *testing.T, src string) (class, message string) {
	t.Helper()
	res, err := JIT(src, 0)
	if err != nil {
		t.Fatalf("%q: jit: %v", src, err)
	}
	if res.Code == 0 {
		t.Fatalf("%q answered %q where the reference raises", src, res.Output)
	}
	if res.Code == 2 {
		t.Fatalf("%q failed as a toolchain rejection (exit 2):\n%s", src, res.Stderr)
	}
	_, class, message = ParseTrapReport(res.Stderr)
	if class == "" {
		t.Fatalf("%q exited %d without an exception report:\n%s", src, res.Code, res.Stderr)
	}
	return class, message
}
