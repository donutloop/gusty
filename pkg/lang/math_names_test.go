package lang

// math_names_test.go — `floor`, `ceil`, `sqrt`: the whole-number builtins answer whole numbers, on
// the compiled backend, in the word a whole number travels in (roadmap L11.6, Gap R.51; ADR 0264).
//
// The measured defect, both legs and three behaviours, with the checker in the middle calling
// all of them well-typed:
//
//	print(floor(3.7))   # math.floor 3 (an int) · --interp NameError, exit 3 · --aot 3.0
//	print(ceil(-0.5))   # math.ceil  0 (an int)  · --interp NameError        · --aot -0.0
//	print(sqrt(-1))     # ValueError: math domain error · --interp NameError · --aot nan
//	print(floor("a"))   # TypeError: must be real number, not str            · --aot 0.0
//
// the record had no such builtins at all — three names in `predeclared.go` that the evaluator
// trapped `NameError` for, which is a program the toolchain accepts and then refuses to run. The
// compiled backend lowered all three on the float road: a float where the reference answers a whole
// number, `0.0` for a text argument, `nan` for a domain the reference raises, and a compile-time
// refusal (exit 1, for a program CPython runs) whenever the negative under `sqrt` was a constant it
// could see.
//
// Every row here is CPython's answer — the reference's `math.floor` / `math.ceil` / `math.sqrt` — on
// both legs. The two shapes that cannot be, because the answer's word or the value's kind is a
// fact only one backend has, are in the filed-not-fixed table with each engine's number beside them.

import (
	"math"
	"strconv"
	"strings"
	"testing"
)

// TestTheWholeNumberBuiltinsAnswerWholeNumbers is the parity table. The `want` column is CPython's
// output — the integration twin in integration/math_names_test.go runs the same source with
// `from math import floor, ceil, sqrt` prefixed and fails this file's expectation if it differs.
func TestTheWholeNumberBuiltinsAnswerWholeNumbers(t *testing.T) {
	for _, tc := range []struct{ name, src, want string }{
		// ---- the pair the roadmap row names as its definition of done.
		{"the row's own answer", "print(floor(3.7))\n", "3\n"},
		{"the second half of it", "print(ceil(-2.2))\n", "-2\n"},
		// ---- whole numbers, in both directions, and not only halfway ones.
		{"floor down", "print(floor(2.7))\n", "2\n"},
		{"floor down, negative", "print(floor(-2.7))\n", "-3\n"},
		{"ceil up", "print(ceil(2.2))\n", "3\n"},
		{"ceil up, negative", "print(ceil(-2.2))\n", "-2\n"},
		{"a tie is already whole", "print(floor(2.0), ceil(2.0))\n", "2 2\n"},
		{"floor of a negative half", "print(floor(-0.5), ceil(-0.5))\n", "-1 0\n"},
		{"minus zero is whole and has no sign to keep", "print(floor(-0.0), ceil(-0.0))\n", "0 0\n"},
		// ---- the answer is the kind the question asks for, not the kind the argument arrived in.
		{"an integer argument is already whole", "print(floor(7), ceil(7))\n", "7 7\n"},
		{"a verdict is the number it is made of", "print(floor(True), ceil(True), ceil(False))\n", "1 1 0\n"},
		{"a float argument still answers a whole number", "print(floor(5.0))\n", "5\n"},
		// ---- the edge of the compiled int word, walked from both sides. The middle of the range is
		// where a wrong rule still looks right; the boundary is where an off-by-one in a guard answers
		// poison, and this pair is what caught one here on the day the feature shipped (Gap R.133).
		{"the largest whole number the compiled int holds, asked of floor", "print(floor(2147483647.0))\n", "2147483647\n"},
		{"the smallest, asked of ceil", "print(ceil(-2147483648.0))\n", "-2147483648\n"},
		{"the smallest, asked of floor", "print(floor(-2147483648.0))\n", "-2147483648\n"},
		// ---- sqrt keeps the float answer it always had.
		{"sqrt of a square", "print(sqrt(9))\n", "3.0\n"},
		{"sqrt of a float literal", "print(sqrt(2))\n", "1.4142135623730951\n"},
		{"sqrt of zero", "print(sqrt(0))\n", "0.0\n"},
		{"sqrt of a quarter", "print(sqrt(2.25))\n", "1.5\n"},
		{"sqrt of a verdict", "print(sqrt(True))\n", "1.0\n"},
		// ---- names, not just literals: the runtime road, not the fold.
		{"the value in a name", "x = 3.7\nprint(floor(x), ceil(x))\n", "3 4\n"},
		{"a whole number in a name", "y = 9\nprint(sqrt(y))\n", "3.0\n"},
		{"a computed argument", "n = 1 + 1\nprint(floor(2.7 + n), sqrt(4 * n))\n", "4 2.8284271247461903\n"},
		// ---- the answer travels: arithmetic, containers, text, comparisons, conditions.
		{"a whole number beside a float is widened, not truncated", "print(floor(2.7) + 1.5)\n", "3.5\n"},
		{"a whole number in arithmetic with itself", "print(floor(2.7) * 3, ceil(2.2) - 1)\n", "6 2\n"},
		{"whole numbers in a container", "print([floor(2.7), ceil(2.2)])\n", "[2, 3]\n"},
		{"a whole number through str()", "print(str(floor(2.7)))\n", "2\n"},
		{"a whole number in an f-string", "print(f\"{floor(2.7)}|{ceil(2.2)}\")\n", "2|3\n"},
		{"a square root compared to a whole number", "print(sqrt(4) == 2)\n", "True\n"},
		{"a whole number compared", "print(floor(2.7) == 2, ceil(2.2) == 2)\n", "True False\n"},
		{"a whole number in a condition", "if floor(2.7) > 1:\n    print(\"big\")\nelse:\n    print(\"not\")\n", "big\n"},
		{"a whole number in a loop", "for i in range(3):\n    print(floor(2.7) + i)\n", "2\n3\n4\n"},
		{"a whole number as a builtin's candidate", "print(max([floor(3.7), 2]))\n", "3\n"},
		{"a square root beside a whole number", "print(sqrt(16) * 2)\n", "8.0\n"},
		// ---- across a call boundary, where the answer's word has to be chosen with it.
		{"returned by a function", "def w(v):\n    return floor(v)\n\nprint(w(2.7))\n", "2\n"},
		{"returned beside its own arithmetic", "def w(v):\n    return floor(v) + 1\n\nprint(w(2.7) * 2)\n", "6\n"},
		{"a square root returned across a call boundary", "def r(v):\n    return sqrt(v)\n\nprint(r(9))\n", "3.0\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			res, err := Compile(tc.src)
			if err != nil {
				t.Fatalf("%s (%q): refused: %v", tc.name, tc.src, err)
			}
			assertNoForbiddenIR(t, tc.src, res.IR)
			if out := runIR(t, res.IR); out != tc.want {
				t.Errorf("%s: AOT ran %q, want the reference's %q\nsrc: %s", tc.name, out, tc.want, tc.src)
			}
			if out := captureStdout(t, tc.src); out != tc.want {
				t.Errorf("%s: interpreter printed %q, want the reference's %q\nsrc: %s", tc.name, out, tc.want, tc.src)
			}
		})
	}
}

// TestTheDomainAndTheKindAreRaisesNotValues is the trap half. Each of these is a program the
// reference runs and stops on, so both legs raise the reference's own sentence — catchably — and
// none of them is a refusal to build the program (ADR 0166), a silent `0.0`, or a `nan` printed and
// the program carried on.
func TestTheDomainAndTheKindAreRaisesNotValues(t *testing.T) {
	for _, tc := range []struct{ name, src, want string }{
		{"a negative constant under sqrt", "print(sqrt(-1))\n", "ValueError: math domain error"},
		{"a negative in a name under sqrt", "x = -1.0\nprint(sqrt(x))\n", "ValueError: math domain error"},
		{"a negative computed at run time", "n = 0 - 4\nprint(sqrt(n * 1.0))\n", "ValueError: math domain error"},
		{"a text under floor", "print(floor(\"a\"))\n", "TypeError: must be real number, not str"},
		{"a text under ceil", "print(ceil(\"2\"))\n", "TypeError: must be real number, not str"},
		{"a text under sqrt", "print(sqrt(\"a\"))\n", "TypeError: must be real number, not str"},
		{"a verdict of None", "print(ceil(None))\n", "TypeError: must be real number, not NoneType"},
		{"a list under floor", "print(floor([1]))\n", "TypeError: must be real number, not list"},
		{"a dict under sqrt", "print(sqrt({\"a\": 1}))\n", "TypeError: must be real number, not dict"},
		{"a set under ceil", "print(ceil({1}))\n", "TypeError: must be real number, not set"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := trapRun(t, tc.src)
			if err == nil {
				t.Fatalf("the interpreter answered where the reference raises: %q", tc.src)
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
			// `nan` and `0.0` are the two answers this row replaces, and both are silent.
			for _, bad := range []string{"nan", "0.0"} {
				if strings.Contains(out, bad) {
					t.Errorf("%s: the compiled leg answered %q where the reference raises:\n%s", tc.name, bad, out)
				}
			}
		})
	}
	// A raise is a raise: the program can name it and take the branch, on both legs.
	for _, tc := range []struct{ name, src, class, want string }{
		{"the domain caught", "try:\n    print(sqrt(-4))\nexcept ValueError:\n    print(\"caught\")\n", "ValueError", "caught\n"},
		{"the kind caught", "try:\n    print(floor(\"a\"))\nexcept TypeError:\n    print(\"caught\")\n", "TypeError", "caught\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if out := captureStdout(t, tc.src); out != tc.want {
				t.Errorf("interpreter: the %s did not reach its except, printed %q", tc.class, out)
			}
			res, err := Compile(tc.src)
			if err != nil {
				t.Fatalf("the catchable form was refused: %v", err)
			}
			if out := runIR(t, res.IR); out != tc.want {
				t.Errorf("compiled: the %s did not reach its except, printed %q", tc.class, out)
			}
		})
	}
	// Zero and two arguments are typos in the program rather than programs the reference runs:
	// one shared sentence, raised interpreted and refused compiled, exit 2 nowhere (Gap R.131's
	// shape, paid for these three names by ADR 0264).
	for _, tc := range []struct{ name, src, want string }{
		{"no argument", "print(floor())\n", "floor expects 1 argument, none given"},
		{"two arguments", "print(sqrt(1, 2))\n", "sqrt expects 1 argument, 2 given"},
		{"no argument to ceil", "print(ceil())\n", "ceil expects 1 argument, none given"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := Compile(tc.src); err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Errorf("compiled: err = %v, want it to say %q", err, tc.want)
			}
			err := trapRun(t, tc.src)
			if err == nil || !strings.Contains(trapText(err), tc.want) {
				t.Errorf("interpreter: trap = %v, want the arity message %q", err, tc.want)
			}
		})
	}
}

// TestTheWholeNumberAnswerIsEmittedAsOne is the IR half of the row. `floor(3.7)` printing `3` is not
// a rendering decision: the answer has to be computed and carried as an i32, or the printer is being
// handed a double and is right to print `3.0`.
func TestTheWholeNumberAnswerIsEmittedAsOne(t *testing.T) {
	res, err := Compile("x = 3.7\nprint(floor(x), ceil(x))\n")
	if err != nil {
		t.Fatalf("the runtime whole-number builtins were refused: %v", err)
	}
	for _, want := range []string{
		"call double @llvm.floor.f64(",
		"call double @llvm.ceil.f64(",
		"fptosi double ",
	} {
		if !strings.Contains(res.IR, want) {
			t.Errorf("the module does not ask for the whole number in a whole-number word (%s missing):\n%s", want, res.IR)
		}
	}
	// The constant fold asks nothing of the target, and does not dress the answer up in an
	// instruction that would cost it its sign (ADR 0263's fold lesson).
	folded, err := Compile("print(floor(2.7))\n")
	if err != nil {
		t.Fatalf("the folded whole number was refused: %v", err)
	}
	for _, bad := range []string{"@llvm.floor.f64", "fptosi double"} {
		if strings.Contains(folded.IR, bad) {
			t.Errorf("a whole number the compiler could hold in its hand was still asked of the target (%s):\n%s", bad, folded.IR)
		}
	}
	// The domain is guarded before the intrinsic is asked, and the guard's raise is the reference's.
	domain, err := Compile("x = -1.0\nprint(sqrt(x))\n")
	if err != nil {
		t.Fatalf("the guarded square root was refused: %v", err)
	}
	if !strings.Contains(domain.IR, "fcmp olt double") || !strings.Contains(domain.IR, "math domain error") {
		t.Errorf("the domain is not guarded before the root is asked:\n%s", domain.IR)
	}
	if strings.Contains(domain.IR, "call double @llvm.sqrt.f64(") && strings.Count(domain.IR, "fcmp olt double") < 1 {
		t.Errorf("the intrinsic is asked without the guard in front of it:\n%s", domain.IR)
	}
	// A program that never asks for these names does not carry the guards.
	quiet, err := Compile("print(1 + 1)\n")
	if err != nil {
		t.Fatalf("a program without the builtins was refused: %v", err)
	}
	for _, bad := range []string{"math domain error", "must be real number", "llvm.floor", "llvm.ceil", "llvm.sqrt"} {
		if strings.Contains(quiet.IR, bad) {
			t.Errorf("a program that never asks for the name carries its machinery (%q):\n%s", bad, quiet.IR)
		}
	}
}

// TestTheWholeNumberRuleIsTheOnesTheReferenceUses is the rule on its own, with neither engine in the
// way: the two answers that do not exist (`NaN`, an infinity), and the boundary of the word the
// compiled backend carries the answer in.
func TestTheWholeNumberRuleIsTheOnesTheReferenceUses(t *testing.T) {
	for _, tc := range []struct {
		name string
		f    float64
		fl   int64
		ce   int64
	}{
		{"down", 2.7, 2, 3},
		{"down, negative", -2.7, -3, -2},
		{"a tie", 0.5, 0, 1},
		{"a tie, negative", -0.5, -1, 0},
		{"already whole", 7, 7, 7},
		{"minus zero", math.Copysign(0, -1), 0, 0},
		{"the largest the compiled word holds", 2147483647, 2147483647, 2147483647},
	} {
		fl, err := floorAnswer(tc.f)
		if err != nil || fl != tc.fl {
			t.Errorf("floorAnswer(%v) = %v, %v; the reference answers %d", tc.f, fl, err, tc.fl)
		}
		ce, err := ceilAnswer(tc.f)
		if err != nil || ce != tc.ce {
			t.Errorf("ceilAnswer(%v) = %v, %v; the reference answers %d", tc.f, ce, err, tc.ce)
		}
	}
	// The two values with no whole number: the reference raises, in two different classes, and so do
	// we — on both legs, which is the point of the guard the constant fold shares with the runtime.
	for _, tc := range []struct {
		name, class string
		f           float64
	}{
		{"NaN", "ValueError", math.NaN()},
		{"positive infinity", "OverflowError", math.Inf(1)},
		{"negative infinity", "OverflowError", math.Inf(-1)},
	} {
		if _, err := floorAnswer(tc.f); !strings.Contains(exnText(err), tc.class) {
			t.Errorf("floorAnswer(%s) = %v, want the reference's %s", tc.name, err, tc.class)
		}
		if _, err := ceilAnswer(tc.f); !strings.Contains(exnText(err), tc.class) {
			t.Errorf("ceilAnswer(%s) = %v, want the reference's %s", tc.name, err, tc.class)
		}
	}
	// The domain, in both directions, and the root itself.
	if _, err := sqrtAnswer(-1); !strings.Contains(exnText(err), "ValueError: math domain error") {
		t.Errorf("sqrtAnswer(-1) = %v, want ValueError: math domain error", err)
	}
	if r, err := sqrtAnswer(math.Copysign(0, -1)); err != nil || r != 0 || !math.Signbit(r) {
		t.Errorf("sqrtAnswer(-0.0) = %v (%v), the reference hands back -0.0", r, err)
	}
	if r, err := sqrtAnswer(2); err != nil || r != math.Sqrt2 {
		t.Errorf("sqrtAnswer(2) = %v (%v), want the square root", r, err)
	}
	if r, err := sqrtAnswer(math.NaN()); err != nil || !math.IsNaN(r) {
		t.Errorf("sqrtAnswer(NaN) = %v (%v), the reference hands a NaN back", r, err)
	}
}

// exnText is a raised value as the CLI would print it: the class and the message live in two
// fields of an TrapError, and Error() prints only the message, so a test that compared Error()
// would read a typed ValueError as an ordinary failure.
func exnText(err error) string {
	if e, ok := err.(*TrapError); ok {
		return trapText(e)
	}
	if err == nil {
		return ""
	}
	return err.Error()
}

// TestTheWholeNumberBeyondTheWordIsFiledNotFixed keeps the boundary of the feature in the test suite
// rather than in a comment. The two shapes below are pinned with each engine's answer, not asserted:
// the compiled int word's range is L12.12's decision, and a loop variable over a literal list of
// doubles has no kind for the builtin to follow at all (Gap R.130). Neither is refused, because both
// print something today and a refusal would retire them for a message.
func TestTheWholeNumberBeyondTheWordIsFiledNotFixed(t *testing.T) {
	for _, tc := range []struct {
		name, src, compiledWant, aotWant string
	}{
		{
			"the whole number past the compiled int word raises rather than wraps (Gap R.133)",
			"x = 3000000000.0\nprint(floor(x))\n",
			"3000000000\n", "OverflowError: floor: the whole number is beyond the word this backend's int holds",
		},
		{
			"one step below the compiled int word, from the boundary the guard was written for (Gap R.133)",
			"print(floor(-2147483649.0))\n",
			"-2147483649\n", "OverflowError: floor: the whole number is beyond the word this backend's int holds",
		},
		{
			"one step above it, reached by ceil where floor would have fitted (Gap R.133)",
			"print(ceil(2147483647.1))\n",
			"2147483648\n", "OverflowError: ceil: the whole number is beyond the word this backend's int holds",
		},
		{
			"a loop variable over a literal list of doubles has no kind to follow (Gap R.130)",
			"for v in [3.7]:\n    print(floor(v))\n",
			"3\n", "0\n",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if out := captureStdout(t, tc.src); !strings.Contains(out, tc.compiledWant) {
				t.Errorf("the record leg: got %q, pinned at %q — the row's pin needs rewriting with the change that moves it", out, tc.compiledWant)
			}
			res, err := Compile(tc.src)
			if err != nil {
				t.Fatalf("refused (%v) — a refusal here is a new shape, not the filed one", err)
			}
			assertNoForbiddenIR(t, tc.src, res.IR)
			if out := runIRMayTrap(t, res.IR); !strings.Contains(out, tc.aotWant) {
				t.Errorf("compiled: got %q, pinned at %q — if the answer arrived, delete this row and promote the probe", out, tc.aotWant)
			}
		})
	}
}

// TestANonFiniteConstantIsWrittenInTheSpellingTheParserTakes is the second engine's half of Gap R.134,
// found by running the `sqrt` sweep rather than by reading the row: folding `sqrt(float("inf"))` handed
// the emitter Go's spelling of the answer with this compiler's `e+00` suffix glued on, and `llc` stopped
// at `expected value token` — exit 2, the contract's "the compiler is broken" code, on a program the
// reference prints. The same spelling was already reachable without any of my changes, from the simplest
// program that binds one of the two values:
//
//	x = float("inf")      # CPython prints inf; the module used to carry `fadd double 0.0, inf.0e+00`
//
// Both routes are pinned here because one fix covers both, and because a route discovered by accident
// deserves the same net as the one the row was about.
func TestANonFiniteConstantIsWrittenInTheSpellingTheParserTakes(t *testing.T) {
	for _, tc := range []struct {
		name, src, want, wantIR string
	}{
		{
			"an infinity in a name (the original exit-2 shape)",
			"x = float(\"inf\")\nprint(x)\n", "inf\n", "0x7FF0000000000000",
		},
		{
			// The sign is taken by the target here (`fsub double 0.0, inf`), so the module carries the
			// positive pattern and the answer still prints `-inf` — which is the same fact Gap R.132 is
			// about, seen from the side that works: what the target computes keeps its sign.
			"an infinity with a sign taken from it",
			"print(-float(\"inf\"))\n", "-inf\n", "0x7FF0000000000000",
		},
		{
			"a NaN in a name",
			"y = float(\"nan\")\nprint(y)\n", "nan\n", "0x7FF8000000000000",
		},
		{
			"the square root of an infinity, which is the fold that found the defect",
			"print(sqrt(float(\"inf\")))\n", "inf\n", "0x7FF0000000000000",
		},
		{
			"the square root of a NaN, which stays a NaN and prints like one",
			"print(sqrt(float(\"nan\")))\n", "nan\n", "0x7FF8000000000000",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			res, err := Compile(tc.src)
			if err != nil {
				t.Fatalf("the reference prints this program and we refused it: %v", err)
			}
			assertNoForbiddenIR(t, tc.src, res.IR)
			if !strings.Contains(res.IR, tc.wantIR) {
				t.Errorf("the value was not written as its IEEE bit pattern (%s):\n%s", tc.wantIR, res.IR)
			}
			if out := runIRMayTrap(t, res.IR); out != tc.want {
				t.Errorf("compiled: printed %q, want the reference's %q", out, tc.want)
			}
		})
	}
	// The spelling is a property of the emitter, not of these five programs, so it is asserted once more
	// on the function that owns it: whatever Go prints for these values, what comes back is a token the
	// LLVM parser has.
	for _, tc := range []struct {
		v    float64
		name string
		want string
	}{
		{math.Inf(1), "positive infinity", "0x7FF0000000000000"},
		{math.Inf(-1), "negative infinity", "0xFFF0000000000000"},
		{math.NaN(), "a NaN", "0x7FF8000000000000"},
	} {
		if got := floatConst(tc.v); got != tc.want {
			t.Errorf("floatConst(%s) = %q, want the bit pattern %s", tc.name, got, tc.want)
		}
	}
	// And a finite value still goes through the decimal road, sign included: the bit patterns are for the
	// two values that have no decimal spelling, not an excuse to rewrite every constant. (Note the Go
	// literal `-0.0` is +0 — a negative zero has to be parsed or copied, which is how Gap R.132's probe
	// gets its value and why the sign is tested through `strconv` here rather than a literal.)
	for _, tc := range []struct {
		name string
		v    float64
		want string
	}{
		{"zero", 0.0, "0.0e+00"},
		{"a whole number", 3.5, "3.5e+00"},
		{"a big number, which stays in exponent form", 1e18, "1.0e+18"},
		{
			"a negative zero keeps its sign in the emitter (what loses it is the fold idiom, Gap R.132)",
			negZeroParsed(t), "-0.0e+00",
		},
	} {
		if got := floatConst(tc.v); got != tc.want {
			t.Errorf("floatConst(%s) = %q, want %q", tc.name, got, tc.want)
		}
	}
}

// negZeroParsed is the value Go's own `-0.0` literal is not: a zero with the sign bit set. It is how a
// source `-0.0` reaches the compiler, which parses rather than writes the constant.
func negZeroParsed(t *testing.T) float64 {
	t.Helper()
	v, err := strconv.ParseFloat("-0.0", 64)
	if err != nil || !math.Signbit(v) {
		t.Fatalf("cannot build a negative zero to test with: %v, %v", v, err)
	}
	return v
}
