package lang

import (
	"strings"
	"testing"
)

// Artifact-level coverage for roadmap Gap R.45 (ADR 0225): a subscript of a string is a
// one-character string, counted in code points — on both backends. Both used to answer the *byte*:
// the interpreter returned an int and the codegen fold emitted `%d` of that int, so the wrong type
// spread to everything the value touched — `s[0] + s[2]` did arithmetic, `s[1] == "b"` said false,
// and `len(s[1])`, `s[1].upper()`, `ord(s[1])` trapped.

func interpPrints(t *testing.T, src string) string {
	t.Helper()
	return captureStdout(t, src)
}

func TestStringSubscriptIsTextInTheInterpreter(t *testing.T) {
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
		if got := interpPrints(t, tc.src); got != tc.want {
			t.Fatalf("%q printed %q, want %q", tc.src, got, tc.want)
		}
	}
}

func TestStringSubscriptIsTextInCompiledCode(t *testing.T) {
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
// the interpreter says so with the class CPython uses, not an untyped message.
func TestStringSubscriptTrapIsTyped(t *testing.T) {
	_, _, err := EvalExpr("s = \"abc\"\nprint(s[9])\n")
	ee, ok := err.(*EvalError)
	if !ok || ee.ExnType != "IndexError" {
		t.Fatalf("expected a typed IndexError, got %#v", err)
	}
}

// TestUnlowerableConditionIsAnErrorNotAFalseBranch is the regression that mattered most here.
// truthyValue used to answer `asI1(b, "0")` when its condition could not be lowered, on the theory
// that "the enclosing statement path still has the error" — but the ternary path had no error
// channel, so `print(1 if s[1] == "b" else 0)` compiled into `icmp ne i32 0, 0` and printed 0. The
// program looked answered. A part of a program that cannot be lowered is a compile error (ADR 0166).
func TestUnlowerableConditionIsAnErrorNotAFalseBranch(t *testing.T) {
	for _, src := range []string{
		"s = \"abc\"\nprint(1 if s[9] == \"b\" else 0)\n",
		"s = \"abc\"\nif s[9] == \"b\":\n    print(1)\nelse:\n    print(0)\n",
		// The shape with a run-time position (`s[x]`) is not here any more: ADR 0229 gave the
		// subscript an answer, so the condition lowers and the program prints CPython's 0. It
		// moved to TestCompiledStringSubscriptAnswersAtRuntime in integration/, where a wrong
		// answer — not just a refusal — is what gets caught. An unlowerable condition is still
		// an error, never `icmp ne i32 0, 0`.

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
