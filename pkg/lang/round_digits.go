package lang

// round_digits.go — `round(x, ndigits)` moves the decimal point, it does not truncate the number
// (roadmap L11.6, Gap R.69; ADR 0263).
//
// `round(x)` has had one rule since ADR 0236: ask for the named IEEE operation
// (`math.RoundToEven` / `@llvm.roundeven.f64`) rather than writing a rounding rule down. The
// digit-count form needs a different named operation, because *binary* rounding is the wrong
// question — and the wrongness is not that ties are subtle, it is that scaling manufactures ties
// the value does not have. `round(0.005, 2)` is 0.01 in CPython, because the exact value of the
// double nearest 0.005 is 0.005000000000000000104083408558…, which is above the tie; multiply it by
// 100 and you get exactly 0.5, a tie, which `roundeven` then answers 0. Same family:
// `round(0.025, 2)` (product 2.5) is 0.03 and comes back 0.02, `round(0.075, 2)` (7.5) is 0.07 and
// comes back 0.08, `round(2.675, 2)` (267.5) is 2.67 and comes back 2.68. That is a rounding rule
// asked of a number the program never wrote. What the reference rounds is the *exact decimal value
// of the double*, to `ndigits` places, ties to even, and it comes back to the nearest double.
// That is a correctly-rounded double→decimal conversion, a different operation from `roundeven` and
// one neither backend owns:
//
//	interpreter  strconv.FormatFloat(v, 'f', n, 64) → strconv.ParseFloat   (Go's dtoa)
//	compiled     snprintf("%.*f", n, v)             → strtod               (the C library's dtoa)
//
// Both are correctly rounded, so both agree with CPython rather than with each other by
// construction. The identity was checked by sweeping 375,224 (value, ndigits) pairs — every
// k/1000 and k/100 in the range, 22,500 uniform randoms, 500 raw bit patterns, ndigits from -2 to
// 10 — against `round(x, n)` compared bit for bit: zero differences, on either spelling of the
// conversion. (The row's own suggested rule — scale, `roundeven`, unscale — differs from CPython on
// 1,077 of those same pairs, and passes `round(2.345, 2)` only by accident.) The negative-digit
// branch (scale, the same conversion with no fractional digits, unscale) was swept again over
// 156,048 pairs with ndigits from -400 to 400: the 143 differences that remain are all between
// |x| = 4.117e18 and 1e300, all one ULP, and all the scale's own inexactness rather than the
// conversion's; they are recorded as their own roadmap row rather than hidden behind a passing test.
//
// The two clamps are arithmetic facts, not moods. Every binary double is exactly a decimal with at
// most 324 digits after the point (the smallest subnormal is 4.94e-324), so rounding at 324 places
// or beyond is the identity and the text stays inside the runtime's buffer. And 10^309 exceeds the
// largest finite double, so `ndigits` at or below -309 asks for a multiple of a number no double
// can reach: the nearest such multiple to any finite value is zero, with the sign kept — which is
// what CPython answers there too.

import (
	"fmt"
	"math"
	"strconv"
	"strings"
)

// roundArityMessage is the arity sentence, written once and read by both backends: the evaluator
// raises it as an EvalError and codegen refuses with it. Two spellings of one event is the shape
// ADR 0236 and ADR 0262 both had to come back for, and `round()` with no argument was, until this
// pair of lines, a Go panic in the evaluator (roadmap Gap R.131).
func roundArityMessage(given int) string {
	if given == 0 {
		return "round expects 1 or 2 arguments, none given: round(x) answers the nearest whole number and round(x, ndigits) moves the decimal point"
	}
	return fmt.Sprintf("round expects 1 or 2 arguments, %d given", given)
}

// roundToDigits is the whole rule, in the one place both the evaluator and the compiler's constant
// fold reach. It is the interpreter's half of the pair above; the compiled runtime's half is
// `rt_round_digits` in floatRuntimeIR, which performs the same two named operations on the target.
func roundToDigits(v float64, n int) float64 {
	// An infinity or a NaN has no decimal point to move. CPython with an explicit digit count
	// hands it back (`round(inf, 2)` is inf), which is what the text round-trip below does too.
	if math.IsNaN(v) || math.IsInf(v, 0) {
		return v
	}
	if n <= -309 {
		return math.Copysign(0, v)
	}
	if n >= 324 {
		// Identity, not a rounding: see the comment above.
		return v
	}
	if n >= 0 {
		s, err := strconv.ParseFloat(strconv.FormatFloat(v, 'f', n, 64), 64)
		if err != nil {
			// Every finite value formats, so this is the infinity/NaN text path; the value it
			// came in with is the answer it should keep.
			return v
		}
		return s
	}
	// Below zero digits the same conversion is asked one scale earlier: divide to bring the
	// round point to the units place, render that with no fractional digits at all, and scale
	// back. The two candidate answers are multiples of 10^m, and `%.0f` + `strtod` is again the
	// correctly-rounded decimal step. `math.RoundToEven(v/scale)*scale` was tried first and
	// differs from CPython on 74,838 of the 156,048 swept negative-digit cases — the scale
	// overflows to +Inf at the large magnitudes and the answer comes back NaN among them —
	// where this shape differs on 143. The reason is the one above: the nearest-even rule is
	// being asked of a binary value that is not the one the question is about.
	scale := pow10(-n)
	q, err := strconv.ParseFloat(strconv.FormatFloat(v/scale, 'f', 0, 64), 64)
	if err != nil {
		return v
	}
	return q * scale
}

// pow10 is 10^k for the k the negative-digit branch can ask for (1..308), built by multiplying by
// ten k times. That is deliberately the *slow* way: it is the same sequence of roundings the
// compiled runtime's loop walks, instruction for instruction, so the two backends cannot differ in
// the last bits of the scale the way they would if one called libm `pow` and the other squared its
// way there. Every power up to 10^22 is exact; beyond that the two loops round identically.
func pow10(k int) float64 {
	s := 1.0
	for i := 0; i < k; i++ {

		s *= 10.0
	}
	return s
}

// roundDigitsValue is the compiled backend's answer to `round(x, ndigits)` on the road that wants
// an i32 — the same register `sqrt` hands back when the i32 path asks it for a root. An integer's
// digit-count round is the integer itself (CPython: `round(5, 2)` is `5`, and `round(True, 2)` is
// `1`), and the digit count is still evaluated, because `round(5, 1.5)` is a TypeError rather than
// a free pass.
func (g *irGen) roundDigitsValue(b *strings.Builder, c *Call) (string, error) {
	if g.isFloatNumericOperand(c.Args[0]) {
		v, raised, err := g.roundDigitsDouble(b, c.Args[0], c.Args[1], c.Span())
		if err != nil {
			return "", err
		}
		if raised {
			// The block is closed over by the raise; the i32 road's zero keeps whatever is emitted
			// next parseable rather than putting a double in an i32 slot.
			return "0", nil
		}
		return v, nil
	}
	// Left to right, as the reference evaluates a call's arguments.
	xv, xerr := g.value(b, c.Args[0])
	if xerr != nil {
		return "", xerr
	}
	if _, err := g.roundNdigitsValue(b, c.Args[1], c.Span()); err != nil {
		return "", err
	}
	if rv, err := g.constIntVal(c.Args[0]); err == nil {
		return fmt.Sprintf("%d", rv), nil
	}
	return xv, nil
}

// roundDigitsDouble emits the double the digit-count round answers with: the folded constant when
// the compiler can hold both operands, and one call to `rt_round_digits` when it cannot. It is
// reached from the float lowering (`print(round(x, 2) * 2)`, a container element, a return) and
// from the value path, exactly like `sqrt`.
func (g *irGen) roundDigitsDouble(b *strings.Builder, x, nd Expr, sp Span) (string, bool, error) {
	// The digit count is checked before anything is folded, because the fold is where a text or a
	// double would otherwise be quietly read as the integer nearest it: `round(2.345, 1.5)` answered
	// 2.3, which is the answer to a question nobody asked (CPython stops that program on a TypeError).
	if bad := g.roundNdigitsKind(nd); bad != "" {
		// Left to right, as the reference evaluates a call's arguments: the value is lowered, then
		// the digit count raises what arrived un-interpretable.
		if _, ok := g.floatEval(x); !ok {
			g.floatValue(b, x)
		}
		g.raiseNdigitsTypeMismatch(b, bad, sp)
		// `raised` says the answer never runs: the caller sits in a block the raise has just closed
		// with its own branch, and what it needs from here is an operand of the right *type* for
		// whatever it goes on to write, not a value.
		return "", true, nil
	}
	if fv, ok := g.floatEval(x); ok {
		if n, nerr := g.constIntVal(nd); nerr == nil {
			return floatConst(roundToDigits(fv, int(n))), false, nil
		}
	}
	// Left to right, as the reference evaluates a call's arguments: what the value rounds is said
	// before the digit count gets its turn, so a raise from either one arrives in that order.
	fx := g.floatValue(b, x)
	if fx == "" {
		return "", false, fmt.Errorf("round: the value rounded at line %d is not a number this backend can lower into a double", sp.Line)
	}
	nv, err := g.roundNdigitsValue(b, nd, sp)
	if err != nil {
		return "", false, err
	}
	rt := g.newTmp()
	fmt.Fprintf(b, "  %s = call double @rt_round_digits(double %s, i32 %s)\n", rt, fx, nv)
	return rt, false, nil
}

// roundNdigitsKind names the TypeError a digit count earns, or says nothing when the digit count is
// an integer. A verdict answers nothing here because a verdict *is* an integer since ADR 0257 —
// `round(2.345, True)` is 2.3 in the reference, and the int path is the right road for it.
func (g *irGen) roundNdigitsKind(nd Expr) string {
	if g.isFloat(nd) {
		return "'float' object cannot be interpreted as an integer"
	}
	if g.exprIsString(nd) {
		return "'str' object cannot be interpreted as an integer"
	}
	return ""
}

// roundNdigitsValue lowers the digit count to the i32 the runtime asks for, raising what CPython
// raises when what arrived is not an integer. The raise is a real one — the same catchable door an
// out-of-range index uses — so `try: round(2.345, 1.5) except TypeError:` behaves here the way it
// behaves in the interpreter and in the reference, rather than being a refusal to build the
// program at all (ADR 0166: exit 1 is for a program the reference itself rejects).
func (g *irGen) roundNdigitsValue(b *strings.Builder, nd Expr, sp Span) (string, error) {
	if bad := g.roundNdigitsKind(nd); bad != "" {
		if g.isFloat(nd) {
			g.floatValue(b, nd)
		}
		g.raiseNdigitsTypeMismatch(b, bad, sp)
		return "0", nil
	}
	return g.valueText(b, nd), nil
}

// raiseNdigitsTypeMismatch raises the TypeError through the branch the exception runtime already
// knows about. The condition is a value the module can see is true rather than the literal, so
// every raise in the language leaves the block in the same shape.
func (g *irGen) raiseNdigitsTypeMismatch(b *strings.Builder, msg string, sp Span) {
	always := g.newTmp()
	fmt.Fprintf(b, "  %s = icmp slt i32 0, 1\n", always)
	g.markI1(always)
	g.branchRaise(b, always, "TypeError", msg, sp, "roundnd")
}

// roundNdigits is the interpreter's half of the digit-count check the compiled backend asks of
// `roundNdigitsValue`: the count must be an integer, and what arrives that is not one is the
// TypeError CPython raises, worded with the kind that turned up — `round(2.345, 1.5)` is
// "type 'float' is not what round() asked for" territory, and the reference says
// `'float' object cannot be interpreted as an integer`. Truncating a 1.5 to 1 instead would be the
// Gap R.69 mistake wearing a different hat: answering a program the reference stops on.
//
// A verdict is an integer here for the same reason it is one everywhere else since ADR 0257:
// `round(2.345, True)` is 2.3 in the reference, because True *is* 1.
func (e *Evaluator) roundNdigits(v int64) (int, error) {
	if o, ok := e.heap[v]; ok && o.kind != "bool" {
		return 0, exnError("TypeError", e.valueTypeName(v)+" object cannot be interpreted as an integer")
	}
	if o, ok := e.heap[v]; ok {
		return int(o.bval), nil
	}
	return int(v), nil
}
