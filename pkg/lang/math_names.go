package lang

// math_names.go — `floor`, `ceil` and `sqrt`: one rule per name, asked the same way by both
// backends (roadmap L11.6, Gap R.51; ADR 0264).
//
// The three names sit in `pkg/lang/predeclared.go`, so the checker has always called a program that
// uses them well-typed. Behind that one table were three different languages:
//
//	print(floor(3.7))   # math.floor 3 (an int) · --interp NameError, exit 3 · --aot 3.0
//	print(ceil(-0.5))   # math.ceil  0 (an int)  · --interp NameError        · --aot -0.0
//	print(sqrt(-1))     # ValueError: math domain error · --interp NameError · --aot nan
//	print(floor("a"))   # TypeError: must be real number, not str            · --aot 0.0
//
// the record had no such builtins at all — it trapped `NameError` for names the checker
// predeclares, which is a program the toolchain accepts and then refuses to run. The compiled
// backend lowered all three on the float road and answered a float where Python answers a whole
// number, silently turned a text argument into `0.0`, and answered `nan` for the domain error,
// which is a value where the reference has a raise. `sqrt(-1)` was also a *compile-time refusal*
// when the constant was visible, i.e. exit 1 for a program CPython runs.
//
// What the compiled backend share now is one rule per name, and the same four questions asked in the
// same order: arity, the argument's kind, the domain, and the word the answer travels in.
//
//	                         floor(x)          ceil(x)         sqrt(x)
//	language reference       math.floor        math.ceil       math.sqrt
//	answer                   int               int             float
//	non-real argument        TypeError         TypeError       TypeError
//	                           must be real number, not <kind>
//	domain                                       ValueError: math domain error   (sqrt < 0)
//	                            ValueError: cannot convert float NaN to integer    (floor/ceil)
//	                            OverflowError: cannot convert float infinity to integer
//
// The `int` answer is what the row existed to file: `math.floor` returns an `int`, and a backend
// that answers `3.0` prints a number the program does not have — the same defect family as ADR
// 0236's `round`, which also answered the wrong kind before it answered the right rule.
//
// Beyond the int word's reach both legs disagree, and the disagreement is a filed row rather
// than a silence: `floor(2147483648.0)` is 2147483648 in the evaluator (whose ints are int64) and
// an `OverflowError` on the compiled side naming L12.12, whose bounded-`int` decision this is
// (roadmap Gap R.133 — the compiled leg refuses to answer a wrong number, which `fptosi` of an
// out-of-range double would have been).

import (
	"fmt"
	"math"
	"strings"
)

// intWordMax / intWordMin bound the whole number the compiled backend's int word carries. They are not
// clamps chosen here: they are the range `fptosi … to i32` is defined on, and outside it LLVM's answer is
// poison rather than a wrong number, so the guard is what keeps a big double from becoming a silent
// negative. L12.12 owns replacing the word; until then it owns the message too.
//
// The two bounds are asymmetric on purpose, and the asymmetry is the whole content of the guard: the i32
// holds -2147483648 … 2147483647, so the *upper* test is against 2^31 (the first value that does not fit,
// raised by `fcmp oge`) and the *lower* one is against -2^31 itself (which does fit, so only a value
// strictly below it is raised by `fcmp olt`). Writing -2^31 - 1 here, as this file first did, lets
// `floor(-2147483649.0)` through the guard and into the poison `fptosi`, which answers 2147483647 — a
// silently wrong number, found by walking the boundary rather than the middle.
const (
	intWordMax float64 = 2147483648.0  //  2^31: the first value NOT representable (raised at, `>=`)
	intWordMin float64 = -2147483648.0 // -2^31: representable, so raised only BELOW it (`<`)
)

// mathNameArityMessage is the arity sentence for the three names, written once and read by both
// backends: the evaluator raises it and codegen refuses with it. Zero arguments was a Go panic in
// the evaluator's `float`-style paths before, and is exit 2's family (roadmap Gap R.131).
func mathNameArityMessage(who string, given int) string {
	tell := map[string]string{
		"floor": "floor(x) answers the largest whole number at or below x",
		"ceil":  "ceil(x) answers the smallest whole number at or above x",
		"sqrt":  "sqrt(x) answers the square root of x, and raises where x is negative",
	}
	if given == 0 {
		return fmt.Sprintf("%s expects 1 argument, none given: %s", who, tell[who])
	}
	return fmt.Sprintf("%s expects 1 argument, %d given", who, given)
}

// realNumberMessage is CPython's own sentence, in CPython's own words: `math.floor("a")` says
// "must be real number, not str" — no quotes round the kind, which is worth writing down because
// the neighbouring TypeError family this language raises (`'str' object cannot be interpreted as an
// integer`, ADR 0263) does quote it. A program's `except` matches on the text a user reads.
func realNumberMessage(kind string) string {
	return "must be real number, not " + kind
}

// bareTypeName takes a kind name out of the quotes the neighbouring TypeError family wears. The
// two sentences differ in the same way CPython's do — `'str' object cannot be interpreted as an
// integer` (ADR 0263) quotes the kind, `must be real number, not str` does not — and a program's
// `except` matches on the text a user reads, so the two are kept as the reference writes them.
func bareTypeName(quoted string) string {
	if len(quoted) >= 2 && strings.HasPrefix(quoted, "'") && strings.HasSuffix(quoted, "'") {
		return quoted[1 : len(quoted)-1]
	}
	return quoted
}

// realKindNames the argument for that sentence when the compiler can see what it is holding.
func (g *irGen) realKindNames(x Expr) string {
	switch {
	case g.exprIsString(x):
		return "str"
	case g.isFloat(x) || g.isFloatNumericOperand(x):
		return ""
	default:
		if _, ok := x.(*NoneLit); ok {
			// Checked before the integer read: `None` is a constant the compiler can read as 0, and
			// `ceil(None)` answering 0 is exactly the silence this check exists to break.
			return "NoneType"
		}
		if n, ok := x.(*Name); ok && n.Value == "None" {
			return "NoneType"
		}
		switch x.(type) {
		case *ListLit:
			return "list"
		case *DictLit:
			return "dict"
		case *SetLit:
			return "set"
		}
		if _, err := g.constIntVal(x); err == nil {
			return ""
		}
		if _, ok := x.(*NoneLit); ok {
			return "NoneType"
		}
		// A name, a call, a container: the kind is not statically known here, and the compiled
		// backend has no tagged value word to ask (L11.1's wall). Answering "" lets the numeric
		// road be taken, which is what every neighbouring builtin does with an unseen kind.
		return ""
	}
}

// raiseRealTypeError raises the TypeError a non-real argument earns, through the door every raise
// in the language uses, so `except TypeError:` takes the branch on the compiled leg as it does on
// the other two engines (ADR 0166: this is a program the reference runs and stops on, not one the
// compiler may decline to build).
func (g *irGen) raiseRealTypeError(b *strings.Builder, kind string, sp Span) {
	always := g.newTmp()
	fmt.Fprintf(b, "  %s = icmp slt i32 0, 1\n", always)
	g.markI1(always)
	g.branchRaise(b, always, "TypeError", realNumberMessage(kind), sp, "realty")
}

// raiseMathRaise raises a class the domain asks for (`ValueError`, `OverflowError`) under a fixed
// condition. Returns nothing: the block is closed with its own branch either way, and what the
// caller needs from the dead tail is an operand of the right type, not a value.
func (g *irGen) raiseMathRaise(b *strings.Builder, cond, class, msg string, sp Span, tag string) {
	g.markI1(cond)
	g.branchRaise(b, cond, class, msg, sp, tag)
}

// floorAnswer / ceilAnswer are the whole-number answers, in the int64 the evaluator carries and as
// the errors CPython raises for the two values no whole number holds.
func floorAnswer(f float64) (int64, error) {
	return wholeAnswer(f, math.Floor)
}

func ceilAnswer(f float64) (int64, error) {
	return wholeAnswer(f, math.Ceil)
}

func wholeAnswer(f float64, toward func(float64) float64) (int64, error) {
	if math.IsNaN(f) {
		return 0, trapError("ValueError", "cannot convert float NaN to integer")
	}
	if math.IsInf(f, 0) {
		return 0, trapError("OverflowError", "cannot convert float infinity to integer")
	}
	w := toward(f)
	if w >= 9223372036854775808.0 || w < -9223372036854775808.0 {
		// Beyond even the evaluator's int64 word. L12.12 owns the choice of what an int is;
		// until it is made, this is said rather than wrapped (Gap R.64's silent class).
		return 0, trapError("OverflowError", "the whole number is beyond the int word this backend holds (roadmap L12.12)")
	}
	return int64(w), nil
}

// sqrtAnswer is the domain question. Negative is a raise rather than a NaN, because that is what
// the reference does with it and a value where the reference has an exception is the difference
// between a program's `except` working and a program printing `nan` and carrying on.
func sqrtAnswer(f float64) (float64, error) {
	if math.IsNaN(f) {
		return math.NaN(), nil // math.sqrt(nan) is nan, and the reference hands it back
	}
	if f < 0 {
		return 0, trapError("ValueError", "math domain error")
	}
	return math.Sqrt(f), nil
}

// floorCeilValue lowers `floor(x)` / `ceil(x)` to the i32 word the answer travels in. This is the
// row's own point: the answer is a whole number, so it is emitted as one rather than as a double
// the printer would have to truncate.
func (g *irGen) floorCeilValue(b *strings.Builder, who string, x Expr, sp Span) (string, error) {
	if bad := g.realKindNames(x); bad != "" {
		// Left to right, as a call's arguments are evaluated: the operand is lowered, then the
		// kind it really has decides the raise.
		g.valueText(b, x)
		g.raiseRealTypeError(b, bad, sp)
		return "0", nil
	}
	if fv, ok := g.floatEval(x); ok {
		w := math.Floor(fv)
		if who == "ceil" {
			w = math.Ceil(fv)
		}
		if math.IsNaN(w) {
			g.raiseNaNToInteger(b, sp)
			return "0", nil
		}
		if math.IsInf(w, 0) {
			g.raiseInfinityToInteger(b, sp)
			return "0", nil
		}
		if w >= intWordMax || w < intWordMin {
			g.raiseIntWordOverflow(b, who, sp)
			return "0", nil
		}
		// The constant is the answer; it needs no instruction to become a value.
		return fmt.Sprintf("%d", int64(w)), nil
	}
	if !g.isFloat(x) && !g.isFloatNumericOperand(x) {
		// An integer argument: floor and ceil are the identity on one, and the answer was already
		// in the word the answer travels in.
		v, err := g.value(b, x)
		if err != nil {
			return "", err
		}
		return v, nil
	}
	fx := g.floatValue(b, x)
	if fx == "" {
		return "", fmt.Errorf("%s: the value at line %d is not a number this backend can lower into a double", who, sp.Line)
	}
	w := g.newTmp()
	if who == "ceil" {
		fmt.Fprintf(b, "  %s = call double @llvm.ceil.f64(double %s)\n", w, fx)
	} else {
		fmt.Fprintf(b, "  %s = call double @llvm.floor.f64(double %s)\n", w, fx)
	}
	// Three ways the whole number can fail to exist, found before the truncation rather than
	// after: NaN (neither bound catches it, so it needs its own test), an infinity, and a value
	// outside the i32 the answer travels in — `fptosi` of which is poison, not a wrong number.
	isnan := g.newTmp()
	fmt.Fprintf(b, "  %s = fcmp uno double %s, %s\n", isnan, w, w)
	hi := g.newTmp()
	fmt.Fprintf(b, "  %s = fcmp oge double %s, %s\n", hi, w, floatConst(intWordMax))
	lo := g.newTmp()
	fmt.Fprintf(b, "  %s = fcmp olt double %s, %s\n", lo, w, floatConst(intWordMin))
	badNaN := g.newTmp()
	fmt.Fprintf(b, "  %s = or i1 %s, %s\n", badNaN, isnan, hi)
	bad := g.newTmp()
	fmt.Fprintf(b, "  %s = or i1 %s, %s\n", bad, badNaN, lo)
	g.raiseMathRaise(b, bad, "OverflowError",
		fmt.Sprintf("%s: the whole number is beyond the word this backend's int holds (roadmap L12.12)", who), sp, who)
	i := g.newTmp()
	fmt.Fprintf(b, "  %s = fptosi double %s to i32\n", i, w)
	return i, nil
}

// sqrtDouble lowers `sqrt(x)` on the road that wants a double, guarding the domain first: the
// reference raises where x is negative, so this does too, and `nan` never reaches a printer.
func (g *irGen) sqrtDouble(b *strings.Builder, x Expr, sp Span) (string, bool, error) {
	if bad := g.realKindNames(x); bad != "" {
		if _, ok := g.floatEval(x); !ok {
			g.valueText(b, x)
		}
		g.raiseRealTypeError(b, bad, sp)
		return "", true, nil
	}
	if fv, ok := g.floatEval(x); ok {
		if fv < 0 {
			g.raiseMathDomain(b, sp)
			return "", true, nil
		}
		return floatConst(math.Sqrt(fv)), false, nil
	}
	fx := g.floatValue(b, x)
	if fx == "" {
		return "", false, fmt.Errorf("sqrt: the value at line %d is not a number this backend can lower into a double", sp.Line)
	}
	neg := g.newTmp()
	fmt.Fprintf(b, "  %s = fcmp olt double %s, 0.000000e+00\n", neg, fx)
	g.raiseMathRaise(b, neg, "ValueError", "math domain error", sp, "sqrt")
	rt := g.newTmp()
	fmt.Fprintf(b, "  %s = call double @llvm.sqrt.f64(double %s)\n", rt, fx)
	return rt, false, nil
}

func (g *irGen) raiseMathDomain(b *strings.Builder, sp Span) {
	always := g.newTmp()
	fmt.Fprintf(b, "  %s = fcmp one double 0.0, 1.0\n", always)
	g.markI1(always)
	g.branchRaise(b, always, "ValueError", "math domain error", sp, "sqrtc")
}

func (g *irGen) raiseNaNToInteger(b *strings.Builder, sp Span) {
	always := g.newTmp()
	fmt.Fprintf(b, "  %s = fcmp one double 0.0, 1.0\n", always)
	g.markI1(always)
	g.branchRaise(b, always, "ValueError", "cannot convert float NaN to integer", sp, "flnan")
}

func (g *irGen) raiseInfinityToInteger(b *strings.Builder, sp Span) {
	always := g.newTmp()
	fmt.Fprintf(b, "  %s = fcmp one double 0.0, 1.0\n", always)
	g.markI1(always)
	g.branchRaise(b, always, "OverflowError", "cannot convert float infinity to integer", sp, "flinf")
}

func (g *irGen) raiseIntWordOverflow(b *strings.Builder, who string, sp Span) {
	always := g.newTmp()
	fmt.Fprintf(b, "  %s = fcmp one double 0.0, 1.0\n", always)
	g.markI1(always)
	g.branchRaise(b, always, "OverflowError",
		fmt.Sprintf("%s: the whole number is beyond the word this backend's int holds (roadmap L12.12)", who), sp, "flovf")
}
