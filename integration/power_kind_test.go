package integration

// End-to-end coverage for Gap R.176 (ADR 0293): `x ** y` answers the KIND the reference answers with, on
// both engines.
//
// The shapes below were measured on a binary built from the pre-cycle HEAD, where each printed a number
// CPython never prints, at exit 0, with the two engines agreeing with each other:
//
//	print(2 ** -1)     reference 0.5        both engines 0        (the int road returned 0 for a negative exponent)
//	print(4 ** 0.5)    reference 2.0        compiled 1            (fptosi truncated llvm.pow.f64's answer)
//	print(2.0 ** 10)   reference 1024.0     compiled 1024         (printed through %d)
//	print(0 ** -1)     reference raises     compiled inf/2147483647
//	print(2 ** 100)    reference a 31-digit int   both engines 0  (the multiply wrapped in silence)
//
// A wrong number both engines agree on is invisible to the parity matrix; the oracle leg is the only
// instrument that sees it, which is why every row here is checked against a live python3 rather than
// against the other engine.

import (
	"strings"
	"testing"
)

// TestPowerExitCodesStayOnTheContract: no shape in the power family may spend exit 2, the contract's code
// for a bug of ours (ADR 0166). `2 ** 31` and `2 ** 100` refuse at 1; `0 ** -1` traps at 3, the code for a
// trap the reference traps on.
func TestPowerExitCodesStayOnTheContract(t *testing.T) {
	for _, src := range []string{
		"print(2 ** -1)", "print(2 ** -2)", "print(4 ** 0.5)", "print(2 ** 1.5)",
		"print(2.0 ** -1)", "print(2.0 ** 10)", "print(0 ** -1)", "print(0.0 ** -1)",
		"z = 0\nprint(z ** -1)", "print(2 ** 100)", "print(2 ** 31)", "print(65536 ** 2)",
		"print((-8) ** (1/3))", "print(2 ** 3 ** 2)", "print((2 and 3) ** 2)",
		"def f(z):\n    return z ** -1\n\n\nprint(f(2))",
	} {
		src := src
		t.Run(strings.ReplaceAll(strings.TrimSpace(src), "\n", "; "), func(t *testing.T) {
			if _, code := cliRunMerged(t, "--aot", "--eval", src); code == 2 {
				t.Errorf("exit 2 — the contract's code for OUR bug (ADR 0166)")
			}
		})
	}
}

// TestPowerMatchesTheReferenceOnBothEngines runs the promoted probe and a table of single expressions,
// each against a live python3, on BOTH legs.
func TestPowerMatchesTheReferenceOnBothEngines(t *testing.T) {
	dir := t.TempDir()
	// The probe's own output is the reference's output, line for line.
	src := readProgram(t, "probe_a_power_answers_the_right_kind.gy")
	want, wantOK := cpythonPlainOut(t, dir, src)
	if !wantOK {
		t.Skip("no usable oracle")
	}
	if got, _ := cliRunMerged(t, "--interp", "--file", writeSrc(t, dir, "pow_probe.gy", src)); got != want {
		t.Errorf("interpreter differs from CPython:\n got: %q\nwant: %q", got, want)
	}
	if got, _ := cliRunMerged(t, "--aot", "--file", writeSrc(t, dir, "pow_probe.gy", src)); got != want {
		t.Errorf("--aot differs from CPython:\n got: %q\nwant: %q", got, want)
	}

	// Single expressions, so a row that regresses names itself.
	for _, expr := range []string{
		"2 ** 3", "2 ** 0", "7 ** 1", "2 ** -1", "2 ** -2", "10 ** -1", "4 ** 0.5",
		"2.0 ** -1", "2.0 ** 10", "2 ** 3 ** 2", "0 ** 0", "True ** 0",
		"(2 and 3) ** 2", "(-8) ** 2.0",
	} {
		expr := expr
		t.Run(expr, func(t *testing.T) {
			line := "print(" + expr + ")"
			want, ok := cpythonPlainOut(t, t.TempDir(), line)
			if !ok {
				t.Skip("no usable oracle")
			}
			if got, _ := cliRunMerged(t, "--interp", "--eval", line); got != want {
				t.Errorf("interpreter printed %q, reference prints %q", got, want)
			}
			if got, _ := cliRunMerged(t, "--aot", "--eval", line); got != want {
				t.Errorf("--aot printed %q, reference prints %q — the answer KIND is part of the answer", got, want)
			}
		})
	}
}

// TestPowerRaiseAndRefusalAreCatchableOrWords checks the two non-answer outcomes are the right kind of
// non-answer: the reference's raise is catchable on BOTH legs (so a program that handles it runs), and the
// shapes the language cannot answer are refused in WORDS rather than in an invalid module (ADR 0166).
func TestPowerRaiseAndRefusalAreCatchableOrWords(t *testing.T) {
	dir := t.TempDir()
	for _, r := range []struct{ name, src, marker string }{
		{"zero base literal", "print(0 ** -1)", "ZeroDivisionError: 0.0 cannot be raised to a negative power"},
		{"zero base bound name", "z = 0\nprint(z ** -1)", "ZeroDivisionError: 0.0 cannot be raised to a negative power"},
	} {
		r := r
		t.Run(r.name, func(t *testing.T) {
			wrapped := "try:\n    " + strings.ReplaceAll(r.src, "\n", "\n    ") + "\nexcept ZeroDivisionError:\n    print(\"caught\")\n"
			for _, leg := range []string{"--interp", "--aot"} {
				leg := leg
				got, _ := cliRunMerged(t, leg, "--file", writeSrc(t, dir, "pow_try.gy", wrapped))
				if !strings.Contains(got, "caught") {
					t.Errorf("%s did not take the except arm: %q", leg, got)
				}
			}
			// Uncaught, it is the reference's trap on the reference's line.
			wantLine, ok := cpythonPlainOut(t, t.TempDir(), r.src)
			if !ok {
				t.Skip("no usable oracle")
			}
			if !strings.Contains(wantLine, "ZeroDivisionError") {
				t.Fatalf("the row is stale: the reference prints %q", wantLine)
			}
			for _, leg := range []string{"--interp", "--aot"} {
				got, _ := cliRunMerged(t, leg, "--eval", r.src)
				if !strings.Contains(got, "ZeroDivisionError") {
					t.Errorf("%s printed %q, want the reference's %q", leg, got, r.marker)
				}
			}
		})
	}
	for _, r := range []struct{ name, src string }{
		{"past the compiled word", "print(2 ** 31)"},
		{"far past the compiled word", "print(2 ** 100)"},
		{"complex shape", "print((-8) ** (1/3))"},
	} {
		r := r
		t.Run("refusal/"+r.name, func(t *testing.T) {
			out, _ := cliRunMerged(t, "--aot", "--eval", r.src)
			for _, banned := range []string{"global variable reference", "use of undefined value", "unexpected type", "integer constant must have integer type"} {
				if strings.Contains(out, banned) {
					t.Errorf("the refusal leaked an invalid module (`%s`): %s", banned, out)
				}
			}
			if strings.TrimSpace(out) == "" {
				t.Errorf("the compiled leg printed nothing for %s", r.src)
			}
		})
	}
}
