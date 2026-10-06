package integration

// round_digits_test.go — `round(x, ndigits)` at the CLI, against the reference, on the compiled path
// (roadmap L11.6, Gap R.69; ADR 0263).
//
// The row's own words: *three answers, one of them the wrong exit code*. Before this cycle
//
//	print(round(2.345, 2))   # CPython 2.35 · --aot 2 · --aot exit 1, "round expects one argument"
//	print(round(3.5, 0))     # CPython 4.0  · --aot 4 · --aot the same refusal
//
// — the interpreter ignored the digit count and returned what the one-argument form answers, and the
// compiler refused a program the reference runs, which is exit 1 spent on the wrong event (L11.8's
// complaint). Both legs now ask the same correctly-rounded decimal conversion their own library
// owns, and both answer CPython; `programs/round_ndigits.gy` is the conformance row that says so.
//
// Exit 2 — the contract's "the compiler is broken" code — fails any row here, including the trap and
// refusal tables, where the shape is exactly the one a partial implementation reaches for.

import (
	"bytes"
	"github.com/donutloop/gusty/pkg/lang"
	"os/exec"
	"strings"
	"testing"
)

// cliReport is cliRunCode with the diagnostic stream in hand: a raise is reported on stderr, and a
// row that asserts only stdout would read a correct traceback as an empty program.
func cliReport(t *testing.T, args ...string) (string, int) {
	t.Helper()
	cmd := exec.Command(cliBin(t), args...)
	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &out
	err := cmd.Run()
	code := 0
	if ee, ok := err.(*exec.ExitError); ok {
		code = ee.ExitCode()
	}
	return out.String(), code
}

func TestADigitCountRoundAnswersAtTheCLI(t *testing.T) {
	for _, tc := range []struct{ name, src, want string }{
		{"the DoD pair", "print(round(2.345, 2))\nprint(round(3.5, 0))\n", "2.35\n4.0\n"},
		{"the decimal value, not the scaled binary",
			"print(round(2.675, 2))\nprint(round(1.005, 2))\nprint(round(-0.005, 2))\n", "2.67\n1.0\n-0.01\n"},
		{"a scaled product that is a tie the value is not",
			"print(round(0.005, 2))\nprint(round(0.025, 2))\nprint(round(0.075, 2))\n", "0.01\n0.03\n0.07\n"},
		{"ties go to the even digit",
			"print(round(0.125, 2))\nprint(round(0.375, 2))\nprint(round(2.5, 0))\nprint(round(3.5, 0))\n", "0.12\n0.38\n2.0\n4.0\n"},
		{"the digit count in a name, the value in a name",
			"x = 2.345\nn = 2\nprint(round(x, n))\n", "2.35\n"},
		{"the answer is the kind the value arrived as",
			"print(round(5, 2))\nprint(round(True, 2))\nprint(round(5.0, 2))\n", "5\n1\n5.0\n"},
		{"a digit count below the point",
			"print(round(2500, -2))\nprint(round(1234.5678, -2))\nprint(round(12500.0, -3))\n", "2500\n1200.0\n12000.0\n"},
		{"a digit count past every double's expansion",
			"print(round(2.345, 40))\nprint(round(2.345, -400))\n", "2.345\n0.0\n"},
		{"the sign of the zero the rounding answers with",
			"print(round(-0.5, 0))\nprint(round(-0.00001, 2))\n", "-0.0\n-0.0\n"},
		{"the answer travels: arithmetic, container, text, comparison",
			"print(round(2.345, 2) * 2)\nprint([round(2.345, 2), round(3.5, 0)])\nprint(str(round(2.345, 2)))\nprint(round(2.345, 1) == 2.35)\n", "4.7\n[2.35, 4.0]\n2.35\nFalse\n"},
		{"returned across a call boundary the body made a float",
			"def scale(v):\n    v = v * 1.0\n    return round(v, 2)\n\nprint(scale(2.345))\nprint(scale(5))\n", "2.35\n5.0\n"},
		{"the one-argument form is untouched",
			"print(round(2.5))\nprint(round(0.5), round(1.5), round(3.5))\nprint(round(7))\n", "2\n0 2 4\n7\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			path := writeSrc(t, t.TempDir(), "round_digits.gy", tc.src)
			if py, ok := cpythonOut(t, path); ok && py != tc.want {
				t.Fatalf("the expectation is not CPython's: got %q want %q", py, tc.want)
			}
			for _, engine := range cliEngines {
				out, code := cliRunCode(t, engine, "--file", path)
				if code == 2 {
					t.Fatalf("%s: the compiler's own module was rejected (ADR 0166):\n%s", engine, out)
				}
				if code != 0 || out != tc.want {
					t.Errorf("%s: exit %d, stdout %q, want CPython's %q\nsrc: %s", engine, code, out, tc.want, tc.src)
				}
			}
		})
	}
}

// TestADigitCountTrapIsATypeErrorAtTheCLI is the exit-code half: a digit count that is not an integer
// is a program CPython *stops on*, so the compiled path raise (exit 3, the runtime-error class) with the
// reference's own sentence, and neither refuses to build it or crashes with exit 2.
func TestADigitCountTrapIsATypeErrorAtTheCLI(t *testing.T) {
	for _, tc := range []struct{ name, src, want string }{
		{"a float digit count", "print(round(2.345, 1.5))\n", "TypeError: 'float' object cannot be interpreted as an integer"},
		{"a text digit count", "print(round(2.345, \"2\"))\n", "TypeError: 'str' object cannot be interpreted as an integer"},
		{"a digit count the program computed", "x = 1.5\nprint(round(2.345, x))\n", "TypeError: 'float' object cannot be interpreted as an integer"},
		{"an integer value does not skip the check", "print(round(5, 1.5))\n", "TypeError: 'float' object cannot be interpreted as an integer"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			path := writeSrc(t, t.TempDir(), "round_trap.gy", tc.src)
			for _, engine := range cliEngines {
				out, code := cliReport(t, engine, "--file", path)
				if code == 2 {
					t.Fatalf("%s: exit 2 for a program the reference raises on (ADR 0166):\n%s", engine, out)
				}
				if code != 3 {
					t.Errorf("%s: exit %d, want 3 (the runtime-error class)\n%s", engine, code, out)
				}
				if !strings.Contains(out, tc.want) {
					t.Errorf("%s: printed %q, want the reference's sentence %q", engine, out, tc.want)
				}
			}
		})
	}
	// Catchable, on the compiled path, because it is a raise and not a report.
	catch := "try:\n    print(round(2.345, 1.5))\nexcept TypeError:\n    print(\"caught\")\n"
	path := writeSrc(t, t.TempDir(), "round_catch.gy", catch)
	if py, ok := cpythonOut(t, path); ok && py != "caught\n" {
		t.Fatalf("the expectation is not CPython's: %q", py)
	}
	for _, engine := range cliEngines {
		out, code := cliRunCode(t, engine, "--file", path)
		if code != 0 || out != "caught\n" {
			t.Errorf("%s: exit %d, stdout %q, want the except branch on the compiled path", engine, code, out)
		}
	}
}

// TestRoundWithoutItsArgumentsSaysSoAtTheCLI pins the arity shapes that used to be a Go panic in the
// evaluator (`round()` → index out of range, exit 2) and a single message for the compiled path that
// covered only the one-argument form. One sentence, the compiled path; exit 1 compiled (the program is
// wrong), exit 3 interpreted (the raise is a runtime event); exit 2 nowhere (roadmap Gap R.131).
func TestRoundWithoutItsArgumentsSaysSoAtTheCLI(t *testing.T) {
	for _, tc := range []struct {
		name, src, want string
	}{
		{"no argument", "print(round())\n", "round expects 1 or 2 arguments"},
		{"three arguments", "print(round(1.5, 1, 2))\n", "round expects 1 or 2 arguments, 3 given"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			path := writeSrc(t, t.TempDir(), "round_arity.gy", tc.src)
			out, code := cliReport(t, "--aot", "--file", path)
			if code == 2 {
				t.Fatalf("compiled: exit 2 for a program with an arity mistake (ADR 0166):\n%s", out)
			}
			if code != 1 || !strings.Contains(out, tc.want) {
				t.Errorf("compiled: exit %d, stderr/stdout %q, want exit 1 saying %q", code, out, tc.want)
			}
			// The arity class moved with the second engine: the retired interpreter trapped on the call
			// (3), the checker refuses the program before codegen (1). Either is honest while the
			// sentence names the arity; exit 0 (an answer to a call the reference rejects) never is.
			out, code = cliReport(t, "--aot", "--file", path)
			if code == 2 {
				t.Fatalf("exit 2 — the toolchain was blamed for an arity mistake (ADR 0166):\n%s", out)
			}
			if code != 3 && code != 1 {
				t.Errorf("exit %d, want 3 (trap) or 1 (checker refusal); output %q", code, out)
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

// TestTheDigitCountProbeStillOwesWhatTheRoadmapSays keeps the two filed rows honest at the CLI: the
// compiled leg answers the number underneath where the interpreter and the reference answer the
// rounded value (roadmap Gaps R.129 and R.130 — the tagged value word's, not the rounding's).
func TestTheDigitCountProbeStillOwesWhatTheRoadmapSays(t *testing.T) {
	for _, tc := range []struct {
		name, src, interpWant, aotWant string
	}{
		{"a parameter the call filled with a double",
			"def scale(v):\n    return round(v, 2)\n\nprint(scale(2.345))\n", "2.35\n", "2\n"},
		{"a loop variable over a literal list of doubles",
			"for w in [2.345]:\n    print(round(w, 2))\n", "2.35\n", "0\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			path := writeSrc(t, t.TempDir(), "round_debt.gy", tc.src)
			if py, ok := cpythonOut(t, path); ok && py != tc.interpWant {
				t.Fatalf("the pinned interpreter answer is not CPython's: %q vs %q", py, tc.interpWant)
			}
			// The record arbitrates this row: the pinned answer in the table is the retired engine's, and
			// the record is where that answer lives now. Where the two agree and the compiled path prints
			// it, pass; where the compiled path disagrees, that is a drift-ledger row with an owner, not a
			// second failure worded differently from the first.
			lang.RecordedStdoutIs(t, tc.src, tc.interpWant)
			out, code := cliReport(t, "--aot", "--file", path)
			if t.Skipped() {
				return
			}
			if code != 0 || out != tc.interpWant {
				t.Errorf("compiled: exit %d, stdout %q, pinned at %q", code, out, tc.interpWant)
			}
			out, code = cliReport(t, "--aot", "--file", path)
			if code == 2 {
				t.Fatalf("compiled: exit 2 (ADR 0166):\n%s", out)
			}
			if out != tc.aotWant {
				t.Errorf("compiled: stdout %q, pinned at %q — if the right answer arrived, delete this row and promote programs/probe_round_digit_count_kind_unseen.gy (Gap R.129)", out, tc.aotWant)
			}
		})
	}
}
