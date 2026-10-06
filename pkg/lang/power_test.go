package lang

// `x ** y` answers the kind the reference answers with (Gap R.176, ADR 0293).
//
// The rule is not "either side is written with a dot":
//
//	int ** int, exponent >= 0   -> int     `2 ** 3` is `8`
//	int ** int, exponent <  0   -> float   `2 ** -1` is `0.5`, NOT `0`
//	anything float              -> float   `4 ** 0.5` is `2.0`, `2.0 ** 10` is `1024.0`
//	0 ** -n                     -> ZeroDivisionError, one sentence for both spellings
//	negative base, fractional exponent -> a complex number, which this language has no value for
//	past the machine word       -> refused in words, never a wrapped number
//
// Before this file the compiled leg answered EVERY `**` through `fptosi` + `%d`, because `**` was missing
// from the operator list that tells `print` what kind an expression answers with: `print(4 ** 0.5)` said
// `1` where the reference says `2.0`. The interpreter had the other half: `if r < 0 { return 0, nil }`,
// under the comment "negative exponents yield 0 for an integer result, mirroring Python's int ** int" —
// an operation Python does not have. Both backends agreed on the wrong answers, so parity was blind and
// only the oracle leg could see them.
//
// The word each backend holds matters too: the compiled `int` is an i32 (Gap R.133, owner L12.12) while
// the interpreter's is 64-bit, so `2 ** 31` refuses compiled and answers interpreted. Neither wraps.

import (
	"os"
	"os/exec"
	"strings"
	"testing"
)

// powerRef answers from a live CPython, so a stale row fails loudly rather than pinning a remembered value.
func powerRef(t *testing.T, src string) string {
	t.Helper()
	if _, err := exec.LookPath("python3"); err != nil {
		t.Skip("no python3 to cross-check")
	}
	cmd := exec.Command("python3", "-c", src)
	out, err := cmd.CombinedOutput()
	if err != nil {
		lines := strings.Split(strings.TrimRight(string(out), "\n"), "\n")
		return strings.TrimSpace(lines[len(lines)-1])
	}
	return strings.TrimRight(string(out), "\n")
}

// TestPowerAnswerKindMatchesTheReference is the table. Every row is run through BOTH engines and against a
// live reference, and the reference is the only authority on the expected string.
func TestPowerAnswerKindMatchesTheReference(t *testing.T) {
	rows := []struct{ name, src string }{
		{"int base int exponent", "print(2 ** 3)\n"},
		{"zero exponent", "print(2 ** 0)\n"},
		{"one exponent", "print(7 ** 1)\n"},
		{"negative exponent answers a float", "print(2 ** -1)\n"},
		{"negative exponent, second power", "print(2 ** -2)\n"},
		{"ten to the minus one", "print(10 ** -1)\n"},
		{"fractional exponent", "print(4 ** 0.5)\n"},
		// NOT in this table: `print(2 ** 1.5)`. The compiled leg and the reference agree on
		// 2.8284271247461903 and the interpreter answers 2.82842712474619 — ONE ULP apart, because Go's
		// math.Pow and the host libm the compiled leg reaches through llvm.pow.f64 disagree. That is
		// Gap R.178 (owner L11.6, one float power path), pinned by TestPowerLastDigitGapIsFiled rather
		// than folded in here, where a green row would hide it.
		{"float base negative exponent", "print(2.0 ** -1)\n"},
		{"float base whole exponent keeps .0", "print(2.0 ** 10)\n"},
		{"right associative", "print(2 ** 3 ** 2)\n"},
		{"bool base", "print(True ** 0)\n"},
		{"nested zero is one", "print(0 ** 0)\n"},
		{"a product that folds, printed as an int", "print((2 and 3) ** 2)\n"},
		// A variable base and exponent: the kinds come from the records, not the literals.
		{"variable base whole exponent", "a = 2\nb = 10\nprint(a ** b)\n"},
		{"variable base negative literal exponent", "a = 2\nprint(a ** -1)\n"},
		{"float variable base", "x = 2.0\nprint(x ** 10)\n"},
	}
	for _, r := range rows {
		r := r
		t.Run(r.name, func(t *testing.T) {
			want := powerRef(t, strings.TrimRight(r.src, "\n"))
			if got := captureStdout(t, r.src); got != want+"\n" {
				t.Errorf("interpreter printed %q, reference prints %q", got, want+"\n")
			}
			if got := compiledOut(t, r.src); got != want+"\n" {
				t.Errorf("--aot printed %q, reference prints %q — the answer KIND is part of the answer", got, want+"\n")
			}
		})
	}
}

// TestPowerZeroDivisionIsTheReferencesTrap pins the raise. The sentence is measured, not composed: the
// reference says `0.0 cannot be raised to a negative power` for `0 ** -1` AND for `0.0 ** -1`, so a rule
// that named the base's type in the message would get one of these two rows wrong.
func TestPowerZeroDivisionIsTheReferencesTrap(t *testing.T) {
	for _, r := range []struct{ name, src string }{
		{"int zero base", "print(0 ** -1)\n"},
		{"float zero base", "print(0.0 ** -1)\n"},
		{"int zero base, second power", "print(0 ** -2)\n"},
		{"zero variable base", "z = 0\nprint(z ** -1)\n"},
	} {
		r := r
		t.Run(r.name, func(t *testing.T) {
			want := powerRef(t, strings.TrimRight(r.src, "\n"))
			if !strings.Contains(want, "ZeroDivisionError") {
				t.Fatalf("the row is stale: the reference prints %q", want)
			}
			ex := trapRun(t, r.src)
			if ex == nil {
				t.Fatalf("the interpreter answered where the reference raises %q", want)
			}
			if got := ex.ExnType + ": " + ex.ExnMsg; got != want {
				t.Errorf("interpreter raised %q, want the reference's %q", got, want)
			}
			// Catchable on the compiled leg too — a raise that only the interpreter can raise is half a raise.
			// compiledOutOrRefusal reports (output, refused) — a catchable raise must RUN, so the
			// second value has to be false here, not true. The program is written through a file
			// because the raise's own lines are indented under `try:`.
			out, refused := compiledOutOrRefusal(t, wrapInZeroDivisionTry(r.src))
			if refused {
				t.Fatalf("the compiled leg refused a catchable raise outright: %s", out)
			}
			if !strings.Contains(out, "caught") {
				t.Errorf("the compiled leg did not take the except arm: %q", out)
			}
		})
	}
}

// TestPowerPastTheWordRefusesRatherThanWraps is why the row was filed alongside Gap R.133 rather than
// quietly fixed: both backends printed `0` for `2 ** 100` at exit 0. The reference has arbitrary-precision
// integers; a bounded int is this language's defensible design, a SILENTLY WRAPPING one is not, and the
// wrap is invisible to parity because both backends wrap alike.
func TestPowerPastTheWordRefusesRatherThanWraps(t *testing.T) {
	// Each pair is (source, the exact wrong number the backend used to print).
	for _, r := range []struct{ name, src, wrapped string }{
		{"compiled two to the 31st", "print(2 ** 31)\n", "-2147483648\n"},
		{"compiled two to the 32nd", "print(2 ** 32)\n", "0\n"},
		{"compiled square of 65536", "print(65536 ** 2)\n", "0\n"},
		{"compiled two to the 100th", "print(2 ** 100)\n", "0\n"},
	} {
		r := r
		t.Run(r.name, func(t *testing.T) {
			res, err := Compile(r.src)
			if err == nil {
				if out := runIR(t, res.IR); out == r.wrapped {
					t.Errorf("the compiled leg answered %s — a wrapped number the reference never produces", strings.TrimSpace(out))
					return
				}
				t.Skip("the compiled leg now answers this; widen the row with the reference's value")
			}
			msg := err.Error()
			if !strings.Contains(msg, "overflow") || !strings.Contains(msg, "bounded integer") {
				t.Errorf("the refusal does not say what is missing (Gap R.38): %v", err)
			}
			// The message quotes the TRUE value, which is the only way an author can act on it.
			if !strings.Contains(msg, "1267650600228229401496703205376") && r.name == "compiled two to the 100th" {
				t.Errorf("the refusal does not name the real number: %v", err)
			}
		})
	}
	// The interpreter holds a 64-bit word, so it refuses far later and must still refuse.
	for _, src := range []string{"print(2 ** 100)\n", "print(2 ** 63)\n"} {
		src := src
		t.Run("interpreter "+strings.TrimSpace(src), func(t *testing.T) {
			ex := trapRun(t, src)
			if ex == nil {
				t.Errorf("the interpreter answered a power past its word; the reference has bignums and this backend does not")
			}
		})
	}
}

// TestPowerComplexIsRefusedNotNan: a negative base to a fractional power is a COMPLEX number in the
// reference, `llvm.pow.f64` answers NaN, and both backends printed `nan` at exit 0. No float rendering of
// NaN is the right answer for a value the language does not have.
func TestPowerComplexIsRefusedNotNan(t *testing.T) {
	for _, r := range []struct{ name, src string }{
		{"minus eight to a third", "print((-8) ** (1/3))\n"},
		{"minus two to a quarter", "print((-2) ** (2/4))\n"},
	} {
		r := r
		t.Run(r.name, func(t *testing.T) {
			// The interpreter REFUSES with an EvalError, so `captureStdout` cannot be used here — it
			// fails the test on a program that traps, which is the shape being pinned.
			// The test looks for a printed NaN, not for the WORD "nan", which the refusal's own
			// message contains: only an ANSWER of nan is the bug, and the refusal naming it is the fix.
			if out, noTrap := compiledOutOrRefusal(t, r.src); noTrap == false && strings.Contains(out, "nan") {
				t.Errorf("the compiled leg printed %q where the reference produces a complex number", out)
			}
			res, err := Compile(r.src)
			if err == nil {
				if out := runIR(t, res.IR); strings.Contains(out, "nan") {
					t.Errorf("the compiled leg printed %q", out)
				}
				return
			}
			if !strings.Contains(err.Error(), "complex") {
				t.Errorf("the refusal does not name what is missing: %v", err)
			}
		})
	}
	// The same base with a WHOLE float exponent stays an answer: `(-8) ** 2.0` is 64.0.
	if got := compiledOut(t, "print((-8) ** 2.0)\n"); got != "64.0\n" {
		t.Errorf("(-8) ** 2.0 printed %q, want 64.0 — a whole float exponent does not leave the reals", got)
	}
}

// TestPowerKeepsTheAnswersThatAlreadyWorked is the ladder's other direction. This rule had to be written
// twice: the first draft answered "float" for any operand whose kind it could not name, and turned three
// working answers into wrong numbers — `(2 and 3) ** 2` became `9.0`, and `2 ** 3 ** 2` became `512.0`.
func TestPowerKeepsTheAnswersThatAlreadyWorked(t *testing.T) {
	for _, r := range []struct{ src, want string }{
		{"print(2 ** 3)\n", "8\n"},
		{"print(2 ** 3 ** 2)\n", "512\n"},
		{"print((2 and 3) ** 2)\n", "9\n"},
		{"a = 2\nb = 10\nprint(a ** b)\n", "1024\n"},
		{"print(2 ** 10 % 7)\n", "2\n"},
		{"xs = [2]\nprint(xs[0] ** 3)\n", "8\n"},
		{"def cube(v):\n    return v ** 3\n\n\nprint(cube(3))\n", "27\n"},
	} {
		r := r
		t.Run(strings.TrimSpace(r.src), func(t *testing.T) {
			if got := captureStdout(t, r.src); got != r.want {
				t.Errorf("interpreter printed %q, want %q", got, r.want)
			}
			if got := compiledOut(t, r.src); got != r.want {
				t.Errorf("--aot printed %q, want %q — an answer may not become a wrong number on the way to fixing one", got, r.want)
			}
		})
	}
}

// TestPowerOperandKindIsAskedOnce is the one-question rule the cycle paid for twice. The print formatter,
// the double road and the constant fold must all get their answer from powerAnswerIsFloat: ADR 0279/0280's
// rule, and the reason `print(4 ** 0.5)` could print `1` while the arithmetic underneath was correct.
func TestPowerOperandKindIsAskedOnce(t *testing.T) {
	src := "print(4 ** 0.5)\n"
	res, err := Compile(src)
	if err != nil {
		t.Fatalf("compiled leg refused a program the reference prints: %v", err)
	}
	if !strings.Contains(res.IR, "@llvm.pow.f64") {
		t.Fatalf("the module does not call the power intrinsic:\n%s", res.IR)
	}
	// The answer reaches printf as a DOUBLE: no truncation of the power's result back to i32.
	for _, line := range strings.Split(res.IR, "\n") {
		if strings.Contains(line, "@llvm.pow.f64") {
			continue
		}
		if strings.Contains(line, "fptosi") && strings.Contains(line, "%t") {
			// fptosi is legitimate for OTHER shapes (floor/ceil); it must not consume the power.
			if strings.Contains(line, "pow") {
				t.Errorf("the power's answer is truncated to an int: %s", strings.TrimSpace(line))
			}
		}
	}
	// The rule is a function, not three copies of a switch.
	ruleSrc, rerr := os.ReadFile("power_kind.go")
	if rerr != nil {
		t.Fatalf("read power_kind.go: %v", rerr)
	}
	for _, fn := range []string{"powerAnswerIsFloat", "powerKindOf", "powerRaisesZeroDivisionForExpr", "powerComplexIsAsked"} {
		if !strings.Contains(string(ruleSrc), "func "+fn) {
			t.Errorf("the power rule lost %s — the roads must share one question (ADR 0279)", fn)
		}
	}
	// And the roads must actually ASK it, rather than restating a switch beside themselves.
	cg, err := os.ReadFile("codegen.go")
	if err != nil {
		t.Fatalf("read codegen.go: %v", err)
	}
	if !strings.Contains(string(cg), "g.powerAnswersDouble(n)") {
		t.Errorf("the print road no longer asks the shared power rule — this is how `print(4 ** 0.5)` came to print 1")
	}
}

// wrapInZeroDivisionTry puts a program's lines under `try:` and catches the raise, so a test can see the
// handler RUN rather than only checking that some error value exists.
func wrapInZeroDivisionTry(src string) string {
	var b strings.Builder
	b.WriteString("try:\n")
	for _, line := range strings.Split(strings.TrimRight(src, "\n"), "\n") {
		b.WriteString("    " + line + "\n")
	}
	b.WriteString("except ZeroDivisionError:\n    print(\"caught\")\n")
	return b.String()
}

// TestPowerLastDigitGapIsFiled pins the one-ULP disagreement as a KNOWN gap rather than letting it pass
// silently. `print(2 ** 1.5)`: the reference and the compiled leg say 2.8284271247461903; the interpreter
// says 2.82842712474619. Go's math.Pow ends ...3bcc where glibc — which is what llvm.pow.f64 calls, and
// what CPython calls — ends ...3bcd. The point of this test is that it FAILS the day someone fixes one
// side without the other, and that a fix which quietly makes the interpreter agree is recorded here.
func TestPowerLastDigitGapIsFiled(t *testing.T) {
	const src = "print(2 ** 1.5)\n"
	want := powerRef(t, "print(2 ** 1.5)")
	if got := compiledOut(t, src); got != want+"\n" {
		t.Errorf("--aot printed %q, reference prints %q — the compiled leg is the one that agrees today, keep it that way", got, want+"\n")
	}
	got := captureStdout(t, src)
	if got == want+"\n" {
		t.Log("Gap R.178 is CLOSED on the interpreter — promote this row into TestPowerAnswerKindMatchesTheReference and delete the filing")
		return
	}
	// It must differ only in the last digit: same significant-figure count, same prefix, one ULP.
	if !strings.HasPrefix(strings.TrimSpace(got), strings.TrimSpace(want)[:len(strings.TrimSpace(want))-2]) {
		t.Errorf("the interpreter printed %q, which differs from the reference's %q by MORE than one ULP — this is no longer Gap R.178, it is a new defect", got, want)
	}
}
