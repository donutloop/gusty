package lang

// round_digits_test.go — `round(x, ndigits)` moves the decimal point, and answers with the kind the
// value arrived as (roadmap L11.6, Gap R.69; ADR 0263).
//
// The measured defect, on the books since ADR 0236 found round's ties: the digit count had three
// answers, one per engine, and two of them were wrong in different directions.
//
//	print(round(2.345, 2))   # CPython 2.35 · --interp 2 · --aot refused, exit 1
//	print(round(3.5, 0))     # CPython 4.0  · --interp 4 · --aot refused, exit 1
//
// The interpreter walked ndigits on the floor and handed back the integer the *one-argument* form
// answers with; the compiler had `if len(c.Args) != 1 { return "round expects one argument" }`, which
// is exit 1 — "your program has a compile error" — for a program the reference runs in one line.
//
// What the two backends now share is one rule, `roundToDigits`, and one named operation under it: a
// correctly-rounded double→decimal conversion, asked of Go's strconv in the evaluator and of the C
// library's snprintf/strtod in the compiled runtime. Binary rounding is the wrong question here — not
// because ties are subtle but because scaling manufactures ties the value never has: `0.005` is
// 0.00500000000000000010408… (above the tie, so CPython answers 0.01) and times 100 is exactly 0.5,
// which a nearest-even rule answers 0. That is why the sweep, not the DoD example, is the evidence:
// the row's own scale/roundeven/unscale algorithm prints CPython's `2.35` for `round(2.345, 2)` and
// differs from the reference on 1,077 of the 375,224 swept pairs. 531,272 pairs were compared bit for
// bit against CPython, and both engines agree with the reference everywhere but the 143
// far-magnitude negative-digit cases the row's own scale step causes
// (docs/roadmap-details.md, Gap R.69).
//
// Every row here is CPython's answer on both engines. The shapes that cannot be, because the answer's
// kind is a fact only the run time knows, are in the filed-not-fixed table with each engine's number
// written down beside it — never silently truncated (that is Gap R.129).

import (
	"math"
	"strings"
	"testing"
)

// TestADigitCountRoundMovesTheDecimalPoint is the parity table: every shape the rule can name, run
// through the real compiler and the interpreter, against what CPython prints.
func TestADigitCountRoundMovesTheDecimalPoint(t *testing.T) {
	for _, tc := range []struct{ name, src, want string }{
		// ---- the pair the roadmap row names as its DoD.
		{"the DoD pair, first half", "print(round(2.345, 2))\n", "2.35\n"},
		{"the DoD pair, second half", "print(round(3.5, 0))\n", "4.0\n"},
		// ---- the decimal value of the double is what rounds, not the scaled binary.
		{"a tie the binary domain would send the wrong way", "print(round(1.005, 2))\n", "1.0\n"},
		{"a value whose exact expansion is above the tie", "print(round(2.675, 2))\n", "2.67\n"},
		{"the same shape on the other side of the tie", "print(round(1.235, 2))\n", "1.24\n"},
		// The three the row's suggested algorithm gets wrong while still printing CPython's answer
		// for round(2.345, 2): each scaled product is exactly 0.5, 2.5, 7.5 — a tie the value is not.
		{"a scaled tie the value does not have, above", "print(round(0.005, 2), round(0.025, 2))\n", "0.01 0.03\n"},
		{"a scaled tie the value does not have, below", "print(round(0.075, 2))\n", "0.07\n"},
		{"ties go to the even digit at two places", "print(round(0.125, 2), round(0.375, 2))\n", "0.12 0.38\n"},
		{"a negative value keeps its magnitude's rule", "print(round(-1.25, 1))\n", "-1.2\n"},
		{"one digit", "print(round(2.345, 1))\n", "2.3\n"},
		// ---- zero and beyond: the digit count is a scale, not a truncation.
		{"zero digits is still a float answer", "print(round(0.5, 0), round(1.5, 0), round(2.5, 0), round(3.5, 0))\n", "0.0 2.0 2.0 4.0\n"},
		{"more digits than the value has is the identity", "print(round(2.345, 40))\n", "2.345\n"},
		{"below the point the unit reaches left", "print(round(123456.0, -3))\n", "123000.0\n"},
		{"a tie below the point goes to the even multiple", "print(round(12500.0, -3), round(13500.0, -3))\n", "12000.0 14000.0\n"},
		{"below every double's expansion is the identity", "print(round(2.345, -400))\n", "0.0\n"},
		// The fold hands back the constant itself rather than an instruction that recomputes it,
		// which is the difference between CPython's -0.0 and 0.0 (roadmap Gap R.132 is the literal
		// spelling of the same question, still open, and still refuses to be folded into this row).
		{"the sign of the zero the fold answers with", "print(round(-0.5, 0))\n", "-0.0\n"},
		{"the sign travels with it", "y = round(-0.00001, 2)\nprint(y, y * 2)\n", "-0.0 -0.0\n"},
		// ---- the answer is the kind the value arrived as.
		{"an integer keeps being an integer", "print(round(5, 2))\n", "5\n"},
		{"a verdict is the integer it is made of", "print(round(True, 2))\n", "1\n"},
		{"a float that is integral still reads as a float", "print(round(5.0, 2))\n", "5.0\n"},
		{"a verdict as the digit count is the integer it is", "print(round(2.345, True))\n", "2.3\n"},
		{"the one-argument form still answers an int", "print(round(2.5), round(3.5), round(-0.5))\n", "2 4 0\n"},
		// ---- names, not just literals: the runtime call, not the fold.
		{"the value in a name", "x = 2.345\nprint(round(x, 2))\n", "2.35\n"},
		{"the digit count in a name", "n = 2\nprint(round(2.345, n))\n", "2.35\n"},
		{"both in names", "x = 2.345\nn = 2\nprint(round(x, n))\n", "2.35\n"},
		{"a digit count computed at run time", "n = 1 + 1\nprint(round(2.345, n))\n", "2.35\n"},
		{"a negative digit count in a name", "n = 0 - 2\nprint(round(1234.5678, n))\n", "1200.0\n"},
		// ---- the answer travels: arithmetic, containers, text, comparisons.
		{"a rounded value in arithmetic", "print(round(2.345, 2) * 2)\n", "4.7\n"},
		{"a rounded value in a container", "print([round(2.345, 2), round(3.5, 0)])\n", "[2.35, 4.0]\n"},
		{"a rounded value through str()", "print(str(round(2.345, 2)))\n", "2.35\n"},
		{"a rounded value compared", "print(round(2.345, 2) == 2.35, round(2.345, 1) == 2.35)\n", "True False\n"},
		{"a rounded value in an f-string", "print(f\"{round(2.345, 2)}\")\n", "2.35\n"},
		{"a rounded value bound and printed", "y = round(2.345, 2)\nprint(y)\n", "2.35\n"},
		{"a rounded value in a condition", "if round(2.345, 1) > 2.3:\n    print(\"big\")\nelse:\n    print(\"not\")\n", "not\n"},
		{"a rounded value in a loop body", "for i in range(2):\n    print(round(2.345 + i, 1))\n", "2.3\n3.3\n"},
		// ---- through a call boundary, where the answer's word has to be chosen with it.
		{"returned by a function whose body made the value a float",
			"def scale(v):\n    v = v * 1.0\n    return round(v, 2)\n\nprint(scale(2.345))\nprint(scale(5))\n", "2.35\n5.0\n"},
		{"returned beside the answer's own arithmetic",
			"def scale(v):\n    v = v * 1.0\n    return round(v, 2) + 0.0\n\nprint(scale(2.345))\n", "2.35\n"},
		{"the one-argument form across a call boundary still answers an int",
			"def whole(v):\n    v = v * 1.0\n    return round(v)\n\nprint(whole(2.5))\n", "2\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			res, err := Compile(tc.src)
			if err != nil {
				t.Fatalf("%s (%q): refused: %v", tc.name, tc.src, err)
			}
			assertNoForbiddenIR(t, tc.src, res.IR)
			if out := runIR(t, res.IR); out != tc.want {
				t.Errorf("%s: AOT ran %q, want CPython's %q\nsrc: %s", tc.name, out, tc.want, tc.src)
			}
			if out := captureStdout(t, tc.src); out != tc.want {
				t.Errorf("%s: interpreter printed %q, want CPython's %q\nsrc: %s", tc.name, out, tc.want, tc.src)
			}
		})
	}
}

// TestTheDigitCountRoundNamesTheConversionAndFoldsOnce is the IR half of the row. The rule is a
// decimal conversion the *library* owns (ADR 0236's lesson about round's ties, applied to the digit
// count): the compiled module asks for it once per rounding, and where the compiler can hold both
// operands it asks nothing at all.
func TestTheDigitCountRoundNamesTheConversionAndFoldsOnce(t *testing.T) {
	res, err := Compile("x = 2.345\nn = 2\nprint(round(x, n))\n")
	if err != nil {
		t.Fatalf("the runtime digit-count round was refused: %v", err)
	}
	if got := strings.Count(res.IR, "call double @rt_round_digits("); got != 1 {
		t.Errorf("the module asks for the conversion %d times, want exactly once:\n%s", got, res.IR)
	}
	if !strings.Contains(res.IR, "define internal double @rt_round_digits(") {
		t.Errorf("the runtime half of the rule is missing from the module:\n%s", res.IR)
	}
	// The answer is a double, so nothing truncates it on the way to the printer.
	if strings.Contains(res.IR, "fptosi double %t") && !strings.Contains(res.IR, "rt_round_digits") {
		t.Errorf("a digit-count round was truncated to an i32:\n%s", res.IR)
	}
	// A conversion the compiler can do itself is not asked of the target twice.
	folded, err := Compile("print(round(2.345, 2))\n")
	if err != nil {
		t.Fatalf("the constant digit-count round was refused: %v", err)
	}
	if strings.Contains(folded.IR, "call double @rt_round_digits(") {
		t.Errorf("a rounding the compiler could hold in its hand was still emitted as a call:\n%s", folded.IR)
	}
	// And a program that never rounds does not pay for the runtime block at all.
	quiet, err := Compile("print(1 + 1)\n")
	if err != nil {
		t.Fatalf("a program without round was refused: %v", err)
	}
	if strings.Contains(quiet.IR, "rt_round_digits") {
		t.Errorf("a program that never rounds carries the rounding runtime:\n%s", quiet.IR)
	}
}

// TestTheDigitCountIsCheckedAsAnInteger is the trap half: a digit count that is not an integer is
// CPython's TypeError, raised on both engines, catchable on both, and not a refusal to build the
// program (which would be exit 1 for a program the reference merely stops on).
func TestTheDigitCountIsCheckedAsAnInteger(t *testing.T) {
	for _, tc := range []struct{ name, src, want string }{
		{"a float digit count", "print(round(2.345, 1.5))\n", "TypeError: 'float' object cannot be interpreted as an integer"},
		{"a text digit count", "print(round(2.345, \"2\"))\n", "TypeError: 'str' object cannot be interpreted as an integer"},
		{"a float digit count in a name", "x = 1.5\nprint(round(2.345, x))\n", "TypeError: 'float' object cannot be interpreted as an integer"},
		{"an integer value with a float digit count", "print(round(5, 1.5))\n", "TypeError: 'float' object cannot be interpreted as an integer"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := trapRun(t, tc.src)
			if err == nil {
				t.Fatalf("the interpreter answered %q where CPython raises", tc.src)
			}
			if got := trapText(err); !strings.Contains(got, tc.want) {
				t.Errorf("interpreter raised %q, want %q", got, tc.want)
			}
			res, cerr := Compile(tc.src)
			if cerr != nil {
				t.Fatalf("%s: refused instead of raising: %v", tc.name, cerr)
			}
			assertNoForbiddenIR(t, tc.src, res.IR)
			out := runIRMayTrap(t, res.IR)
			if !strings.Contains(out, tc.want) {
				t.Errorf("%s: compiled trap printed %q, want %q\nsrc: %s", tc.name, out, tc.want, tc.src)
			}
			if strings.Contains(out, "2.3\n") {
				t.Errorf("%s: the compiled leg answered the question nobody asked (a truncated digit count):\n%s", tc.name, out)
			}
		})
	}
	// The raise is a raise: the program can name it and take the branch.
	catch := "try:\n    print(round(2.345, 1.5))\nexcept TypeError:\n    print(\"caught\")\n"
	if out := captureStdout(t, catch); out != "caught\n" {
		t.Errorf("interpreter: the TypeError did not reach except TypeError, printed %q", out)
	}
	res, err := Compile(catch)
	if err != nil {
		t.Fatalf("the catchable form was refused: %v", err)
	}
	if out := runIR(t, res.IR); out != "caught\n" {
		t.Errorf("compiled: the TypeError did not reach except TypeError, printed %q", out)
	}
}

// trapText is a raised program as the CLI writes it: a typed raise carries its class and its
// message in two fields, an ordinary TrapError in one, and a test that compared only `Msg` would
// read a typed TypeError as empty.
func trapText(e *TrapError) string {
	if e.ExnType != "" {
		return e.ExnType + ": " + e.ExnMsg
	}
	return e.Msg
}

// TestRoundWithoutADigitCountKeepsItsOwnAnswer pins the neighbour that was already right, and the
// two arity shapes that used to be one message for both: `round()` was a Go panic in the evaluator
// (index out of range, exit 2 — the contract's compiler-bug code) and `round(x, 1, 2)` was nothing at
// all. Both engines now say what is missing, in words (roadmap Gap R.131 covers the other builtins).
func TestRoundWithoutADigitCountKeepsItsOwnAnswer(t *testing.T) {
	for _, tc := range []struct{ name, src, want string }{
		{"one argument, a float", "print(round(2.5))\n", "2\n"},
		{"one argument, ties to even", "print(round(0.5), round(1.5), round(2.5), round(3.5))\n", "0 2 2 4\n"},
		{"one argument, an int", "print(round(7))\n", "7\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			res, err := Compile(tc.src)
			if err != nil {
				t.Fatalf("%s: refused: %v", tc.name, err)
			}
			if out := runIR(t, res.IR); out != tc.want {
				t.Errorf("%s: AOT ran %q, want %q", tc.name, out, tc.want)
			}
			if out := captureStdout(t, tc.src); out != tc.want {
				t.Errorf("%s: interpreter printed %q, want %q", tc.name, out, tc.want)
			}
		})
	}
	for _, tc := range []struct {
		name, src, want string
	}{
		{"no argument at all", "print(round())\n", "round expects 1 or 2 arguments, none given"},
		{"three arguments", "print(round(1.5, 1, 2))\n", "round expects 1 or 2 arguments, 3 given"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := Compile(tc.src); err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Errorf("compiled: err = %v, want it to say %q", err, tc.want)
			}
			err := trapRun(t, tc.src)
			if err == nil || !strings.Contains(trapText(err), "round expects 1 or 2 arguments") {
				t.Errorf("interpreter: trap = %v, want the arity message", err)
			}
		})
	}
}

// TestTheDigitCountRuleIsTheOnesWeSwept is the rule on its own, without either engine in the way:
// the values the 531,272-case sweep found interesting, at the digit counts that decide an answer, and
// the two properties the clamps are supposed to give. A regression here changes both engines at once,
// which is the point of keeping the rule in one function.
func TestTheDigitCountRuleIsTheOnesWeSwept(t *testing.T) {
	for _, tc := range []struct {
		v    float64
		n    int
		want float64
	}{
		// The pair the roadmap row names.
		{2.345, 2, 2.35}, {3.5, 0, 4.0},
		// Where scaling in binary and rounding the decimal value part company. Each of these is a
		// value whose scaled product lands exactly on a tie the value itself is not: the decimal
		// says above or below, the product says even, and `roundeven` answers the wrong question.
		{0.005, 2, 0.01}, {0.025, 2, 0.03}, {0.075, 2, 0.07}, {2.675, 2, 2.67},
		{1.005, 2, 1.0}, {1.235, 2, 1.24}, {-0.005, 2, -0.01},
		{0.045, 2, 0.04}, {0.145, 2, 0.14}, {8.835, 2, 8.84},
		// Ties to even, exactly representable, so the rule itself is asked.
		{0.125, 2, 0.12}, {0.375, 2, 0.38}, {2.5, 0, 2.0}, {3.5, 0, 4.0}, {1.25, 1, 1.2}, {1.35, 1, 1.4},
		// Below the point.
		{2500, -2, 2500}, {1234.5678, -2, 1200.0}, {12500.0, -3, 12000.0}, {13500.0, -3, 14000.0},
		{1e300, -1000, 0.0},
		// The clamps, which are arithmetic facts rather than moods.
		{2.345, 324, 2.345}, {2.345, 10000, 2.345}, {5e-324, 323, 0.0}, {5e-324, 324, 5e-324},
		{1.7976931348623157e308, -309, 0.0},
	} {
		got := roundToDigits(tc.v, tc.n)
		if math.Float64bits(got) != math.Float64bits(tc.want) {
			t.Errorf("roundToDigits(%v, %d) = %v, CPython answers %v", tc.v, tc.n, got, tc.want)
		}
		// Rounding twice at the same digit count changes nothing, and a digit count past every
		// double's decimal expansion hands the value back.
		if again := roundToDigits(got, tc.n); math.Float64bits(again) != math.Float64bits(got) {
			t.Errorf("roundToDigits is not idempotent at %d digits on %v: %v then %v", tc.n, tc.v, got, again)
		}
	}
	for _, v := range []float64{math.Inf(1), math.Inf(-1)} {
		if got := roundToDigits(v, 2); got != v {
			t.Errorf("round(%v, 2) = %v, CPython hands an infinity back", v, got)
		}
	}
	if got := roundToDigits(math.NaN(), 2); !math.IsNaN(got) {
		t.Errorf("round(nan, 2) = %v, CPython hands a NaN back", got)
	}
	if !math.Signbit(roundToDigits(-0.5, 0)) {
		t.Error("round(-0.5, 0) lost the sign of its zero; CPython answers -0.0")
	}
}

// TestADigitCountOfAValueTheModuleCannotSeeIsFiledNotFixed keeps the boundary of the feature in the
// test suite rather than in a comment. `round(x, ndigits)` follows the kind of x, and the compiled
// backend can only follow a kind the module can see; of a parameter the call filled with a double,
// and of a loop variable bound from a literal list of doubles, it takes the i32 road of the call and
// prints what that road holds. Both are exit 0 with a number-shaped answer, which is why they are
// pinned with each engine's number instead of being asserted (roadmap Gaps R.129 and R.130).
func TestADigitCountOfAValueTheModuleCannotSeeIsFiledNotFixed(t *testing.T) {
	for _, tc := range []struct {
		name, src, interpWant, aotWant string
	}{
		{
			"round of a parameter the call filled with a double",
			"def scale(v):\n    return round(v, 2)\n\nprint(scale(2.345))\n",
			"2.35\n", "2\n",
		},
		{
			"round of a loop variable over a literal list of doubles",
			"for w in [2.345]:\n    print(round(w, 2))\n",
			"2.35\n", "0\n",
		},
		{
			"the wall underneath it: the same loop variable multiplied",
			"for v in [1.5]:\n    print(v * 2)\n",
			"3.0\n", "0\n",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if out := captureStdout(t, tc.src); out != tc.interpWant {
				t.Errorf("interpreter: got %q, pinned at %q — the row's pin needs rewriting with the change that moves it", tc.interpWant, out)
			}
			res, err := Compile(tc.src)
			if err != nil {
				t.Fatalf("refused (%v) — a refusal here is a new shape, not the filed one", err)
			}
			assertNoForbiddenIR(t, tc.src, res.IR)
			if out := runIR(t, res.IR); out != tc.aotWant {
				t.Errorf("compiled: got %q, pinned at %q — if the answer arrived, delete this row and promote the probe (Gap R.129)", out, tc.aotWant)
			}
		})
	}
}
