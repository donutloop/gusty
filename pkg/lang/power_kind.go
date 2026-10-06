package lang

// `x ** y` answers an int or a float, and which one is a rule about the OPERANDS (Gap R.176, ADR 0293).
//
// The reference's rule is not "if either side is written with a dot". It is:
//
//   - int ** int with a NON-NEGATIVE exponent  -> int      (`2 ** 3` is `8`)
//   - int ** int with a NEGATIVE exponent       -> float    (`2 ** -1` is `0.5`, not `0`)
//   - anything else numeric                     -> float    (`4 ** 0.5` is `2.0`)
//   - base 0 with a negative exponent           -> ZeroDivisionError
//   - `0 ** 0`                                  -> `1`
//   - a negative base with a fractional exponent -> a complex number, which this language has no value
//     for, so it is refused rather than approximated by a NaN
//
// Before this file the compiled leg answered EVERY `**` as an int: `**` was absent from the operator list
// that tells `print` what kind an expression answers with, so the correct double from `llvm.pow.f64` was
// pushed back through `fptosi` and printed with `%d` — `print(2.0 ** 10)` said `1024` where the reference
// says `1024.0`, and `print(4 ** 0.5)` said `1` where it says `2.0`. The interpreter had the other half
// wrong: `if r < 0 { return 0 }`, under a comment claiming it mirrored Python.
//
// The rule lives here, asked once, because four roads need the same answer: the print formatter, the
// arithmetic lowering, the constant fold, and the double road's lift. Asking it twice is how the two
// backends came to disagree about the same source (ADR 0279/0280's one-question rule).

import (
	"math"
	"strings"
)

// powerAnswerIsFloat asks whether `l ** r` answers a float, for the pieces of the front end that can see
// literal operands. It is deliberately conservative: an operand whose kind cannot be named is treated as
// "this might be a float", because a `**` that prints through `%d` when its answer is a double is the
// wrong number this row exists to remove, while a double printed where an int was due is caught by the
// module verifier.
func powerAnswerIsFloat(l, r Expr, kindOf func(Expr) string) bool {
	lk, rk := powerKindOf(l, kindOf), powerKindOf(r, kindOf)
	if lk == "float" || rk == "float" {
		return true
	}
	if lk == "str" || lk == "None" || rk == "str" || rk == "None" {
		// Not a number at all: the numeric doors beside this rule raise the reference's TypeError, and
		// answering "float" here would only route a trap through the double road.
		return false
	}
	if rk == "" {
		// The EXPONENT is the side whose kind decides int-vs-float, so an exponent this pass cannot name
		// is treated as possibly-negative and the answer is conservatively a double.
		return true
	}
	if lk == "" {
		// The BASE being unknown does not change the rule: an int exponent that is provably non-negative
		// means the reference answers an int. `(2 and 3) ** 2` is `9`, not `9.0` — the first draft of this
		// rule answered "float" for any unnamed side and turned that answer into `9.0`, which is a working
		// answer becoming a wrong number (the ladder's forbidden direction, Gap R.176).
		return powerExponentIsNegativeKnown(r, kindOf)
	}
	if lk == "int" && rk == "int" {
		return powerExponentIsNegative(r, kindOf)
	}
	// bool behaves as int everywhere else in this front end.
	if powerIsBoolKind(lk) || powerIsBoolKind(rk) {
		return rk == "bool" && powerExponentIsNegative(r, kindOf)
	}
	return true
}

// powerRaisesZeroDivisionForExpr is the raise test for the codegen roads, which see AST nodes rather than
// evaluated operands.
func powerRaisesZeroDivisionForExpr(l, r Expr, kindOf func(Expr) string) bool {
	if !powerExponentIsNegativeKnown(r, kindOf) {
		return false
	}
	// The base may be a literal, a negated literal, or a NAME the records resolve to zero — `z = 0` /
	// `print(z ** -1)` is the same program to the reference and the same ZeroDivisionError, but the
	// compiled leg answered `inf` until the read could see a binding as well as a literal.
	base, ok := powerNumericLiteral(l, kindOf)
	if !ok {
		return false
	}
	return base == 0
}

// powerNumericLiteral reads a numeric literal, through a name the caller's reader can resolve. It reports
// false rather than guessing, so the roads that use it never raise on an operand they cannot see.
func powerNumericLiteral(e Expr, kindOf func(Expr) string) (float64, bool) {
	if u, ok := e.(*UnOp); ok && (u.Op == "-" || u.Op == "+") {
		v, ok := powerNumericLiteral(u.X, kindOf)
		if !ok {
			return 0, false
		}
		if u.Op == "-" {
			return -v, true
		}
		return v, true
	}
	switch v := e.(type) {
	case *IntLit:
		return float64(v.Value), true
	case *FloatLit:
		return v.Value, true
	case *Name:
		if kindOf == nil {
			return 0, false
		}
		// A name the reader calls "int" or "float" is only answerable when an int value is on record;
		// anything else stays unknown, and an unknown base never raises.
		switch kindOf(e) {
		case "int", "float":
			if iv, ok := readIntLiteral(e); ok {
				return float64(iv), true
			}
		}
		return 0, false
	}
	return 0, false
}

// powerKindOf names an operand's numeric family, asking the caller-supplied reader for anything the node
// itself cannot say.
func powerKindOf(e Expr, kindOf func(Expr) string) string {
	switch v := e.(type) {
	case *IntLit:
		return "int"
	case *FloatLit:
		return "float"
	case *BoolLit:
		return "bool"
	case *StrLit:
		return "str"
	case *NoneLit:
		return "None"
	case *UnOp:
		if v.Op == "-" || v.Op == "+" {
			return powerKindOf(v.X, kindOf)
		}
		return ""
	case *BinOp:
		// `/` is TRUE division: its answer is a float whatever the operands' written kinds are, so
		// `(1/3)` is a float exponent even though both literals are ints. A NEGATIVE fractional exponent
		// on a negative base is the complex case, and reading `1/3` as "not an int" is what lets the
		// refusal fire instead of `llvm.pow.f64` answering NaN (Gap R.176, ADR 0293).
		if v.Op == "/" {
			return "float"
		}
		// A nested power's kind is the same question the whole asks, answered recursively: the right
		// operand of `2 ** 3 ** 2` IS a `**`, and calling it "unknown" made the outer one answer a
		// double, so `print(2 ** 3 ** 2)` printed `512.0` where the reference prints `512` — a working
		// answer turned into a wrong number by the rule meant to fix one.
		if v.Op == "**" {
			if powerAnswerIsFloat(v.L, v.R, kindOf) {
				return "float"
			}
			return "int"
		}
		// The other arithmetic operators answer a number whose family is its operands' families.
		lk, rk := powerKindOf(v.L, kindOf), powerKindOf(v.R, kindOf)
		if lk == "" || rk == "" {
			return ""
		}
		if lk == "float" || rk == "float" {
			return "float"
		}
		if lk == "int" && rk == "int" {
			return "int"
		}
		return ""
	}
	if kindOf != nil {
		return kindOf(e)
	}
	return ""
}

func powerIsBoolKind(k string) bool { return k == "bool" }

// powerExponentIsNegative decides `int ** -n` -> float. A literal answers directly; a negated literal is
// read through the UnOp; anything else is unknown and the CALLER must not call this for it.
func powerExponentIsNegative(r Expr, kindOf func(Expr) string) bool {
	neg := false
	if u, ok := r.(*UnOp); ok && u.Op == "-" {
		neg = true
		r = u.X
	}
	if lit, ok := r.(*IntLit); ok {
		if lit.Value < 0 {
			return true
		}
		return neg
	}
	if v, ok := readIntLiteral(r); ok {
		if v < 0 {
			return true
		}
		return neg
	}
	return neg
}

// powerIsLiteralZero reports a literal 0 base (or a name the records say holds 0), which is what turns a
// negative exponent into ZeroDivisionError.
func powerIsLiteralZero(e Expr, kindOf func(Expr) string) (bool, bool) {
	if u, ok := e.(*UnOp); ok && u.Op == "-" {
		e = u.X
	}
	if lit, ok := e.(*IntLit); ok {
		return lit.Value == 0, true
	}
	if lit, ok := e.(*FloatLit); ok {
		return lit.Value == 0, true
	}
	if v, ok := readIntLiteral(e); ok {
		return v == 0, true
	}
	return false, false
}

// kindOfIntValue is the hook a codegen pass fills in to let these predicates read a NAME the program
// bound to a literal int. Left nil in the parser/tests, where only literals are visible — which is exactly
// why every read goes through readIntLiteral: calling a nil hook is a segfault in the middle of a
// diagnostic, not a declined guess.
var kindOfIntValue func(Expr) (int64, bool)

// readIntLiteral asks the hook when one is installed and declines otherwise.
func readIntLiteral(e Expr) (int64, bool) {
	if kindOfIntValue == nil {
		return 0, false
	}
	return kindOfIntValue(e)
}

// powerRaisesZeroDivision is `0 ** -n`, which the reference refuses with ZeroDivisionError — a sentence of
// its own, not the divide-by-zero one: `0.0 cannot be raised to a negative power`.
func powerRaisesZeroDivision(l, r Expr, kindOf func(Expr) string) (bool, string) {
	if !powerExponentIsNegativeKnown(r, kindOf) {
		return false, ""
	}
	isZero, known := powerIsLiteralZero(l, kindOf)
	if !known || !isZero {
		return false, ""
	}
	// Measured: one sentence for an int base and a float base both.
	return true, "0.0 cannot be raised to a negative power"
}

// powerExponentIsNegativeKnown is powerExponentIsNegative for a pair whose exponent kind is settled: an
// int or float NEGATIVE exponent raises off a zero base, and an unknown kind is not claimed.
func powerExponentIsNegativeKnown(r Expr, kindOf func(Expr) string) bool {
	k := powerKindOf(r, kindOf)
	if k != "int" && k != "float" && k != "bool" {
		return false
	}
	if u, ok := r.(*UnOp); ok && u.Op == "-" {
		return true
	}
	if lit, ok := r.(*IntLit); ok {
		return lit.Value < 0
	}
	if lit, ok := r.(*FloatLit); ok {
		return lit.Value < 0
	}
	if v, ok := readIntLiteral(r); ok {
		return v < 0
	}
	return false
}

// powerComplexIsAsked reports a negative base raised to a fractional exponent, which in the reference
// produces a COMPLEX number. This language has no complex value, so the roads refuse it in words rather
// than printing `nan` — which is what both backends did before, at exit 0.
func powerComplexIsAsked(l, r Expr, kindOf func(Expr) string) bool {
	if powerKindOf(r, kindOf) != "float" {
		return false
	}
	if !powerExponentIsFractional(r, kindOf) {
		return false
	}
	return powerBaseIsProvablyNegative(l, kindOf)
}

// powerExponentIsFractional says the exponent has a part the integer road cannot carry: 0.5 is fractional,
// 2.0 is not — `(-8) ** 2.0` is `64.0` in the reference and answers here too.
func powerExponentIsFractional(r Expr, kindOf func(Expr) string) bool {
	if u, ok := r.(*UnOp); ok && (u.Op == "-" || u.Op == "+") {
		r = u.X
	}
	switch v := r.(type) {
	case *FloatLit:
		return v.Value != math.Trunc(v.Value)
	case *BinOp:
		// `1/3` is a literal division whose answer is a fraction; `6/3` is a literal division whose
		// answer is a whole 2.0, and the difference decides whether the reference leaves the reals.
		// Both are decided from the literals, so no guess is involved.
		if v.Op != "/" {
			return false
		}
		lv, lok := intLiteralOfAny(v.L)
		rv, rok := intLiteralOfAny(v.R)
		if !lok || !rok || rv == 0 {
			return false
		}
		return lv%rv != 0
	}
	return false
}

// intLiteralOfAny reads an int literal, including a negated one, without needing a generator.
func intLiteralOfAny(e Expr) (int64, bool) {
	switch v := e.(type) {
	case *IntLit:
		return v.Value, true
	case *UnOp:
		if v.Op == "-" {
			if x, ok := intLiteralOfAny(v.X); ok {
				return -x, true
			}
		}
		if v.Op == "+" {
			return intLiteralOfAny(v.X)
		}
	}
	return 0, false
}

// powerBaseIsProvablyNegative reads the base's sign where it can: a literal, a negated literal, or a name
// the caller's reader resolves. An unresolved name is NOT claimed — refusing an answer the compiler cannot
// prove wrong is the ladder's forbidden direction.
func powerBaseIsProvablyNegative(l Expr, kindOf func(Expr) string) bool {
	switch v := l.(type) {
	case *IntLit:
		return v.Value < 0
	case *FloatLit:
		return v.Value < 0
	case *UnOp:
		if v.Op == "-" {
			return true
		}
	}
	if v, ok := readIntLiteral(l); ok {
		return v < 0
	}
	return false
}

// powerStrIsAnInt spells the one thing the exponent roads ask about a text: nothing about a text raises a
// TypeError from `**` more often than a name the author thought was a number.
func powerStrIsAnInt(e Expr) bool {
	_, ok := e.(*StrLit)
	return ok
}

// powerNumberSurface renders an operand for a diagnostic without leaking a Go repr.
func powerNumberSurface(e Expr) string {
	switch v := e.(type) {
	case *IntLit:
		return v.Text
	case *FloatLit:
		return strings.TrimRight(strings.TrimRight(v.Text, "0"), ".")
	case *StrLit:
		return "a text"
	}
	return "an operand"
}

// powerComplexResult reports the reference's third answer to `**`: a negative base to a fractional power
// is a COMPLEX number (`(-8) ** (1/3)` is `(1.0000000000000002+1.7320508075688772j`). This language has no
// complex value, so the roads refuse rather than printing `nan`, which is what both backends did at exit 0
// before this row (Gap R.176).
func powerComplexResult(base, exp float64) (raised bool, asked bool) {
	if base >= 0 || exp == math.Trunc(exp) {
		return false, false
	}
	if math.Floor(exp) != exp {
		return true, true
	}
	return false, false
}

// powerComplexRefusal is the words for that refusal: naming what the reference answers with and what this
// language lacks (Gap R.38 — a diagnostic must describe the program the reader is holding).
func powerComplexRefusal() *EvalError {
	return &EvalError{
		ExnType: "ValueError",
		Msg: "a negative base raised to a fractional power is a complex number in the reference, " +
			"and this language has no complex value; the compiled leg declines the program rather " +
			"than printing nan (roadmap Gap R.176, ADR 0293)",
	}
}

// powerNegativeExponentFromZeroFor is the raise test for two evaluated operands, where the caller has
// already unboxed them: a zero base and a negative exponent, either spelling. The base may be a boxed 0.0
// or the bare int 0, and the exponent may arrive as a negative int or as a boxed negative float.
func powerNegativeExponentFromZeroFor(l, r int64, e *Evaluator) bool {
	// `l` and `r` are the unboxed WORDS the numeric road carries, and a float is a heap HANDLE, not its
	// value — `0.0 ** -1` arrives as l=281474976710658 (the box), so testing `l != 0` declined the raise
	// and pow answered `inf`. The value has to be asked of the heap before the sign is judged.
	lv, isFloat := float64(0), false
	if e != nil {
		if f, ok := e.floatOf(l); ok {
			lv, isFloat = f, true
		} else if b, ok := e.heap[l]; ok && b.kind == "float" {
			lv, isFloat = b.fval, true
		}
	}
	if isFloat {
		if lv != 0 {
			return false
		}
		if r < 0 {
			return true
		}
		if e != nil {
			if rf, ok := e.floatOf(r); ok {
				return rf < 0
			}
		}
		return false
	}
	if l != 0 {
		return false
	}
	if r < 0 {
		return true
	}
	if e != nil {
		if rf, ok := e.floatOf(r); ok {
			return rf < 0
		}
	}
	return false
}

// The last-digit gap that powToReference tried to close is FILED, not closed: see Gap R.178. Go's
// math.Pow and the host libm the compiled leg reaches through llvm.pow.f64 disagree by one ULP on part of
// the fractional-exponent grid — `math.Pow(2, 1.5)` ends ...3bcc where glibc ends ...3bcd — so
// `print(2 ** 1.5)` differs in its LAST DIGIT between the two engines. A refinement built from exact
// integer powers plus math.Sqrt was tried here and made the grid worse (21 divergences either way, on
// different rows), so it was removed rather than shipped: a private libm is L11.6's to settle, and a
// "fix" that moves which rows are wrong is not a fix. Both engines are one ULP from each other, which the
// reference's own repr makes visible but no parity check can see.
