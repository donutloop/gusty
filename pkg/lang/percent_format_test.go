package lang

// The printf-style `%` on a text: the reference formats it, this backend builds neither the
// formatting nor a number from a format string, so the honest answer is exit 1 — and the shape that
// used to answer is the reason. `print("%.2f" % 3.5)` is CPython's `3.50` and a `TypeError` in the
// interpreter; the compiled leg printed `0.0` at **exit 0**, because the float road lifted the format
// string's `@str_tab` index through `sitofp` and `frem`'d it against 3.5 (roadmap L11.2, Gap R.165,
// ADR 0282). Every row here is measured against `python3`; the numeric controls are what keep the
// guard from being a ban on the operator.

import (
	"strings"
	"testing"
)

// percentFormattingRefusals is the family that must refuse: a text on the left of `%`, whatever
// conversion the literal spells. Before ADR 0282 only the ones *without* a float conversion refused.
func TestTextLeftPercentRefusesRatherThanAnsweringANumber(t *testing.T) {
	for _, tc := range []struct{ name, src string }{
		// The row itself, and its sibling with a different precision: these two printed `0.0`.
		{"the row: %.2f over a double", "print(\"%.2f\" % 3.5)\n"},
		{"%.1f over a double", "print(\"%.1f\" % 3.14159)\n"},
		{"%f over an int", "print(\"%f\" % 3)\n"},
		{"%e over a double", "print(\"%e\" % 3.5)\n"},
		{"%% alone is still a format string", "print(\"50%%\" % 1)\n"},
		// The shapes that already refused; their wording must not have moved, which the second half
		// of this test checks by requiring a refusal at all, and the sentence stays the operator's.
		{"%d with a text left", "print(\"%d items\" % 3)\n"},
		{"%s with a text left", "print(\"%s!\" % \"hi\")\n"},
		{"%x with a text left", "print(\"%x\" % 255)\n"},
		{"width and precision", "print(\"%5.2f\" % 3.5)\n"},
		{"several conversions", "print(\"%s=%d\" % \"a\", 2)\n"},
		// The same lift reached from inside a function body, where the answer would otherwise leave
		// the frame as a double and print at the call site.
		{"in a function's return", "def f(v):\n    return \"%.2f\" % v\n\nprint(f(3.5))\n"},
		{"bound to a name first", "s = \"%.2f\" % 3.5\nprint(s)\n"},
		// (`-"hi" + 1.5` and `"hi" + 1.5` are not in this table: they are **raises**, ADR 0266's and
		// CPython's own, and the module compiles them — pinning them here would ask a compile-time
		// refusal for a run-time trap.)
	} {
		t.Run(tc.name, func(t *testing.T) {
			res, err := Compile(tc.src)
			if err == nil {
				_, out := runIRAllowingTrap(t, res.IR)
				t.Fatalf("a text-left %% compiled; the reference formats this and the interpreter refuses it, so answering is a wrong number (Gap R.165) — printed %q\nsrc: %s", out, tc.src)
			}
			if !strings.Contains(err.Error(), "codegen:") {
				t.Errorf("the refusal must be a front-end diagnostic, got %v", err)
			}
		})
	}
}

// TestRemainderStillAnswersIsTheOtherHalfOfGapR165 pins what the guard must not do: `%` is the
// remainder for numbers, on both engines, with both divide-by-zero sentences intact (ADR 0278's
// four). A guard that banned the operator would pass the table above and fail this one.
func TestRemainderStillAnswersIsTheOtherHalfOfGapR165(t *testing.T) {
	for _, tc := range []struct{ name, src, want string }{
		{"int remainder", "print(7 % 3)\n", "1\n"},
		{"negative int remainder", "print(-7 % 3)\n", "2\n"},
		{"float remainder", "print(7.5 % 2)\n", "1.5\n"},
		{"negative float remainder", "print(-7.5 % 2)\n", "0.5\n"},
		{"both double", "print(7.5 % 2.5)\n", "0.0\n"},
		{"remainder of a float by a float to zero result", "print(7.5 % 2.5) \n", "0.0\n"},
		{"remainder through a parameter", "def f(v):\n    return v % 2\n\nprint(f(7.5))\nprint(f(7))\n", "1.5\n1\n"},
		// The equality road lifts both of its operands through the same helper; a comparison is not a
		// numeric use, so unlike kinds must keep answering rather than being refused (ADR 0282's
		// narrowing, measured before the guard was scoped).
		{"float compared to a text", "print(1 if 1.0 == \"a\" else 0)\n", "0\n"},
		{"text compared to a float", "print(1 if \"a\" == 1.0 else 0)\n", "0\n"},
		{"float compared to a container", "print(1 if 1.0 == [1] else 0)\n", "0\n"},
		{"a float still prints", "print(1.5 + 2)\n", "3.5\n"},
		{"true division still answers", "print(7 / 2)\n", "3.5\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if out := captureStdout(t, tc.src); out != tc.want {
				t.Errorf("interpreter: stdout %q, want %q\nsrc: %s", out, tc.want, tc.src)
			}
			res, err := Compile(tc.src)
			if err != nil {
				t.Fatalf("compiled leg refused a program the oracle answers (%v): %s", err, tc.src)
			}
			if got := runIR(t, res.IR); got != tc.want {
				t.Errorf("compiled: stdout %q, want %q\nsrc: %s", got, tc.want, tc.src)
			}
		})
	}
}

// TestThePercentRefusalNamesItsOwnCause is the wording row. The refusal below the lift was first
// emitted with the *slot* sentence ("a slot whose kind only the object knows … needs the run-time
// tag"), which is true of a container read and false of a literal: it sent a reader looking for a
// container that was never in the program. The record now carries its own cause (ADR 0282).
func TestThePercentRefusalNamesItsOwnCause(t *testing.T) {
	_, err := Compile("print(\"%.2f\" % 3.5)\n")
	if err == nil {
		t.Fatal("expected the format string to be refused")
	}
	msg := err.Error()
	for _, want := range []string{"text", "printf", "Gap R.165", "3.50"} {
		if !strings.Contains(msg, want) {
			t.Errorf("the refusal must name %q, got: %s", want, msg)
		}
	}
	// …and must not claim the shape it is not: no run-time tag is being asked for here.
	for _, not := range []string{"run-time tag", "only the object knows"} {
		if strings.Contains(msg, not) {
			t.Errorf("the refusal must not say %q for a literal format string, got: %s", not, msg)
		}
	}
}
