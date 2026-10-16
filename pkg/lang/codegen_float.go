// Doubles: the constant's bits, the float fold of an arithmetic expression, and the questions the roads ask
// about an operand's numeric kind (roadmap L11.1, ADR 0279's shared power rule included).
package lang

import (
	"fmt"
	"math"
	"math/big"
	"strconv"
	"strings"
)

// floatConst formats a float constant as an LLVM double literal.
func floatConst(v float64) string {
	// The two values a decimal spelling cannot write. LLVM 20's parser takes no `inf` or `nan`
	// keyword in a double literal (it wants the hex bit pattern), and Go's own rendering of them is
	// `inf`/`-inf`/`nan` — which this compiler used to glue `e+00` onto, producing `inf.0e+00` and an
	// assembler that stops at `expected value token`: a program of ours reaching llc and failing there,
	// which is exit 2 and the one outcome this compiler is never allowed to produce (roadmap Gap R.133,
	// ADR 0264). The IEEE bit pattern is what the parser does accept, and it says exactly the number:
	// no rounding, no sign lost, no dependence on a fold.
	switch {
	case math.IsNaN(v):
		return "0x7FF8000000000000"
	case math.IsInf(v, 1):
		return "0x7FF0000000000000"
	case math.IsInf(v, -1):
		return "0xFFF0000000000000"
	}
	f := pyFloatRepr(v)
	// LLVM double literals need a decimal point in the mantissa.
	if i := strings.IndexAny(f, "eE"); i >= 0 {
		if !strings.Contains(f[:i], ".") {
			f = f[:i] + ".0" + f[i:]
		}
	} else {
		if !strings.Contains(f, ".") {
			f += ".0"
		}
		f += "e+00"
	}
	return f
}

// materialiseFloat writes one compile-time double into a register, and the reason it exists is the
// one number the obvious idiom gets wrong. All thirteen sites used to say
// `fadd double 0.0, <const>` — a no-op addition to land a constant in an SSA value — and IEEE answers
// `-0.0 + +0.0` with `+0.0`. The sign of a negative zero the COMPILER wrote was therefore gone before
// the module existed: `print(-0.0)` printed `0.0` and `x = -0.0` / `print(x)` printed `0.0`, both at
// exit 0, while a product the target itself multiplied kept its sign because nothing folded it
// (roadmap Gap R.132, ADR 0349 — the runtime formatter `rt_fmt_double` renders the sign correctly and
// was never handed the value).
//
// A value with its sign bit set takes the road ADR 0264 opened for the two values a decimal spelling
// cannot write: the IEEE bit pattern, bitcast. It states the number and nothing else — no fold, no
// rounding, no addition that can decide what the sign meant. Every other value keeps the addition,
// because `+0.0 + x` is `x` exactly for every finite value, infinity and NaN, and a large body of
// pinned IR knows that shape; the promise this makes is about the sign, not about the idiom.
func materialiseFloat(b *strings.Builder, t string, v float64) {
	if math.Signbit(v) {
		// Decimal, not `0x…`: LLVM's lexer reads a bare hex token as a *floating-point* constant
		// whatever type token precedes it, and `bitcast i64 0x8000000000000000 to double` dies in
		// llc with "floating point constant invalid for type" — an assembler failure on a module
		// the emitter believed, which is exit 2 and the one outcome this compiler is never allowed
		// to produce (ADR 0166's rule, read by ADR 0264's hex-for-double exception).
		fmt.Fprintf(b, "  %s = bitcast i64 %d to double\n", t, math.Float64bits(v))
		return
	}
	fmt.Fprintf(b, "  %s = fadd double 0.0, %s\n", t, floatConst(v))
}

// powerAnswersDouble is the `**` answer kind, asked of the same table the arithmetic roads use. It is a
// method rather than a call into power_kind.go's free function because the answer depends on what the
// RECORDS know about a name: `x ** -1` where `x = 2` is the float 0.5, and a name this pass cannot name is
// conservatively a double, because a double printed where an int was due is caught by the verifier while an
// int truncated out of a correct double is the silent wrong number this row removes (Gap R.176, ADR 0293).
func (g *irGen) powerAnswersDouble(n *BinOp) bool {
	if g.floatUnlowerable != "" {
		// already refusing something; do not compound it with a guess
		return true
	}
	return powerAnswerIsFloat(n.L, n.R, g.powerReaders())
}

// intPowerOverflow asks whether a literal `a ** b` would leave the 64-bit word, so the compiled leg can
// REFUSE instead of wrapping. The reference has arbitrary-precision integers — `2 ** 100` is a 31-digit
// number — and the compiled backend printed `0` for it at exit 0, which is a wrong answer with a plausible face.
// A bounded integer is this language's defensible 2026 design (docs/roadmap-details.md's Phase 12 note); a
// silently WRAPPING one is not, and the wrap was invisible to parity because the two engines wrapped alike.
func (g *irGen) intPowerOverflow(l, r Expr) (string, bool) {
	lv, lok := intLiteralOf(l)
	rv, rok := intLiteralOf(r)
	if !lok || !rok || rv < 0 {
		return "", false
	}
	// The WORD, not int64: the compiled `int` is an i32 (Gap R.133, owner L12.12), so `2 ** 31` is already
	// past it, and answering -2147483648 is the wrap this refusal exists to stop. Measuring the wrong word
	// would refuse `2 ** 100` while letting `2 ** 32` print `0`.
	acc := new(big.Int).Exp(big.NewInt(lv), big.NewInt(rv), nil)
	if acc.IsInt64() && acc.Int64() >= math.MinInt32 && acc.Int64() <= math.MaxInt32 {
		return "", false
	}
	return acc.String(), true
}

// floatPowerNeedsZeroGuard asks whether the runtime zero-power trap has to be emitted ahead of
// `llvm.pow.f64`. It is emitted for every float `**`, because `pow(0, -1)` answers `inf` instead of
// trapping and an unchecked `inf` reaching printf is the exit-0 wrong number Gap R.176 removes. The
// compile-time claim alone was not enough: it can only see a literal base, and `z = 0` / `print(z ** -1)`
// is the same program to the reference.
func (g *irGen) floatPowerNeedsZeroGuard(n *BinOp) bool {
	// A pair that is provably safe skips the guard: a base literal known to be non-zero, or an exponent
	// literal known to be non-negative. Anything else pays two fcmps.
	if lb, ok := powerNumericLiteral(n.L, g.powerReaders()); ok && lb != 0 {
		return false
	}
	if eb, ok := powerNumericLiteral(n.R, g.powerReaders()); ok && eb >= 0 {
		return false
	}
	return true
}

// powerReaders is the facts the power predicates are given for this module: the family an operand belongs to,
// and a way to read a NAME — `z = 0` / `print(z ** -1)` is the same program to the reference as
// `print(0 ** -1)`, and a predicate that can only see literals answers `inf` for one and raises for the other.
// The reader closes over *this* generator's module constants and travels to the predicates as a value, so a
// name it cannot resolve stays unknown rather than becoming a guess, and so no second compile can be answered
// from a first module's constants (ADR 0343: the hook this replaces was a shared function pointer, and the
// suite now has two compiles reaching for it at once).
func (g *irGen) powerReaders() powerFacts {
	consts := g.moduleConsts
	return powerFacts{
		kindOf: g.powerOperandKind,
		readInt: func(e Expr) (int64, bool) {
			nm, ok := e.(*Name)
			if !ok || consts == nil {
				return 0, false
			}
			v, ok := consts[nm.Value]
			if !ok {
				return 0, false
			}
			return intLiteralOfAny(v)
		},
	}
}

// powerOperandKind names an operand's numeric family for the power rule: a float variable, an int variable,
// or "" when this pass cannot say — and "" means the caller treats it as possibly-a-float.
func (g *irGen) powerOperandKind(e Expr) string {
	switch e.(type) {
	case *IntLit:
		return "int"
	case *FloatLit:
		return "float"
	case *BoolLit:
		return "bool"
	case *StrLit:
		return "str"
	}
	if nm, ok := e.(*Name); ok {
		if g.builtinShadowed(nm.Value) {
			return ""
		}
		if g.floatVars != nil && g.floatVars[nm.Value] {
			return "float"
		}
		// A name bound to an int LITERAL in the module environment is an int; anything else this pass
		// cannot name, and the caller treats the unknown as possibly-a-float.
		if e2, ok := g.moduleConsts[nm.Value]; ok {
			switch e2.(type) {
			case *IntLit:
				return "int"
			case *FloatLit:
				return "float"
			case *BoolLit:
				return "bool"
			}
		}
		return ""
	}
	if g.isFloat(e) {
		return "float"
	}
	return ""
}

// isFloat reports whether expression e produces a float (double) value.
func (g *irGen) isFloat(e Expr) bool {
	switch n := e.(type) {
	case *FloatLit:
		return true
	case *UnOp:
		if n.Op == "-" {
			// A negated slot read whose kind the object carries is answered by the float arms, so its
			// result *is* a float and print has to choose the float formatter for it (Gap R.88).
			return g.isFloat(n.X) || g.taggedNegationApplies(n)
		}
		return false
	case *BinOp:
		if g.percentFormatOwnsBinOp(n) {
			// A formatting answers a TEXT, and the number-shaped road reading `"%s" % 3.5` as a remainder with a
			// float operand asked the double lift for the format itself — which the L11.2 guard refused, so
			// `u = "%s" % 3.5` could not be bound at all, and `def f(v): return "%.2f" % v` lost the argument's
			// tag on the way and printed `3.00` for `3.50` (roadmap Gap R.31, L11.1, ADR 0360). The operator is
			// the same character and a different question; the plan is what tells them apart.
			return false
		}
		// `/` is *true* division (PEP 238): 7 / 2 is 3.5 even when both operands are
		// integers, so the result is a float whatever the operands are. Truncating it
		// made the language's most common operator behave like C's, and the parity
		// harness could not see it because both backends agreed on the wrong answer.
		if n.Op == "/" {
			return true
		}
		if n.Op == "and" || n.Op == "or" {
			// What the operator answers with is what the operand it chooses answers with — ADR 0262's rule
			// for a ternary's arms read for the two operators that hand back an operand (roadmap Gap R.147,
			// ADR 0269). Without this the print formatter and the arithmetic each picked their own answer for
			// `print(1.5 and 2.5)`, which is how a float came to be printed through `%d`.
			return g.logicAnswerIsFloat(n)
		}
		// The numeric door answers an arithmetic use of a slot whose kind the object carries through
		// the float arms — unboxing a float slot rather than reading its handle as a count — so the
		// result of such an expression *is* a float, and print has to pick the float formatter for it
		// (roadmap L11.1, Gap R.88). Comparisons are not in this: they answer 0/1 whatever lifts.
		switch n.Op {
		case "+", "-", "*", "%", "//", "**":
			if g.taggedNumberUseApplies(n) {
				return true
			}
		}
		switch n.Op {
		case "+", "-", "*", "%", "//":
			return g.isFloat(n.L) || g.isFloat(n.R)
		case "**":
			// `**` answers a float whenever the reference does, which is NOT "either side is written
			// with a dot": `2 ** -1` is the float 0.5 with two int literals, and `2.0 ** 10` is a float
			// the reference renders `1024.0`. `**` was absent from this list, so the correct double that
			// `llvm.pow.f64` computed came back through `fptosi` and printed with `%d` — `print(4 ** 0.5)`
			// said `1` where the reference says `2.0`, and both backends agreed on the truncation, so only
			// the oracle leg could see it (roadmap Gap R.176, ADR 0293).
			return g.powerAnswersDouble(n)
		}
		return false
	case *Index:
		// A slot whose literal is a float is a float *use*, even though the container holding it is
		// not a float (the container arm below): `xs = [1.5, "a"]; print(xs[0] * 2)` must multiply a
		// double and print `3.0`, and the only reason the compiler knows the slot holds 1.5 is the
		// same promise that lets the numeric read fold (roadmap L11.1, ADR 0243). Restricted to the
		// tagged shapes — a mixed list, or a slot reached through another slot — so every container
		// that already answered this question keeps the answer it has.
		if el, ok := g.staticNumericElem(n); ok {
			switch base := n.Obj.(type) {
			case *Name:
				if g.mixedLists[base.Value] {
					return g.isFloat(el)
				}
			case *Index:
				return g.isFloat(el)
			}
		}
		return false
	case *Name:
		if g.floatVars != nil {
			return g.floatVars[n.Value]
		}
		return false
	case *Attr:
		// `mod.NAME` for a data import: the value is a literal the program never wrote, and the only
		// reason the compiler knows `math.PI` is a double is that the module declares it as one.
		// Without this read the whole float family was answered by the integer road — `print(math.PI)`
		// printed `3`, `math.PI * 2` printed `6`, and a hand-written literal beside it was right
		// (roadmap L11.6, the typed stdlib constants; `probe_math_const`).
		return g.foldedModuleFloat(e)
	case *ListLit, *DictLit, *SetLit:
		// A container is not a float, whatever its elements are — `[1.5, "a"] == [1.5, "a"]` is a
		// structural comparison of two containers, not an fcmp of two doubles. This arm used to
		// answer "yes, it is a float" when any element was, which was unreachable while a float
		// element was refused outright; once the element could be stored the answer came out as
		// `sitofp i32 <handle> to double`, comparing boxes rather than contents (roadmap L11.1,
		// ADR 0233). A program that wants a number out of a container asks for the element.
		return false
	case *CondExpr:
		// What a ternary answers with is what its arms answer with, asked in one place
		// (`ternaryKind`) and read by the print formatter, the return-word gate and both lowerings —
		// never re-derived from the shape of the line, which is how the arithmetic and the renderer
		// came to disagree about the same expression (roadmap L11.6, Gap R.102, ADR 0262).
		k := g.ternaryKind(n)
		return k == ternaryArmIsDouble || k == ternaryBothAreDoubles
	case *Call:
		if n.Fn != nil {
			if id, ok := n.Fn.(*Name); ok {
				// A function the program defined and that returns a float is a fact about
				// the program, and it outranks everything below.
				if g.floatFuncs[id.Value] {
					return true
				}
				// A program that defines `float`, `abs`, `sum` ... owns that name: nothing
				// below may read the call through the built-in's shape (Gap R.6).
				if g.builtinShadowed(id.Value) {
					return false
				}
				if id.Value == "float" {
					return true
				}
				if id.Value == "abs" || id.Value == "min" || id.Value == "max" {
					// min/max return the element they choose, so the answer is a float when the
					// *winner* is; the others ask whether any operand is (ADR 0221).
					if id.Value == "min" || id.Value == "max" {
						if len(n.Args) == 1 {
							if lst, isLit := n.Args[0].(*ListLit); isLit {
								if isF, known := g.minMaxReturnsFloat(lst.Elems, id.Value == "min"); known {
									return isF
								}
							}
							// A single non-list argument is still an element, and an int argument is
							// printed as an int even when it arrived beside a float in a different call.
							return g.isFloat(n.Args[0])
						}
						return g.minMaxVarargsReturnsFloat(n.Args, id.Value == "min")
					}
					for _, a := range n.Args {
						if g.isFloatNumericOperand(a) {
							return true
						}
					}
				}
				if id.Value == "sqrt" {
					// sqrt answers a float whatever arrives; floor and ceil do not — they answer a whole
					// number, and saying "float" here is what made `print(floor(3.7))` print `3.0`
					// (roadmap Gap R.51, ADR 0264).
					return len(n.Args) == 1
				}
				if id.Value == "round" && len(n.Args) == 2 {
					// `round(x, ndigits)` answers with the number whose decimal point moved, which is a
					// float whatever came in — `round(3.5, 0)` is `4.0`, printed with the trailing zero,
					// while the one-argument form above stays the integer-answering question (Gap R.69).
					return g.isFloatNumericOperand(n.Args[0])
				}
				if id.Value == "sum" {
					for _, a := range n.Args {
						if g.isFloatNumericOperand(a) {
							return true
						}
					}
				}
			}
		}
		return false
	}
	return false
}

// numericFoldElems answers a min/max/sum argument's elements as numbers the compiler can hold,
// and says which of them are floats. It is the question min/max must ask twice over: Python returns
// the *element*, so the answer's type is the winner's own type — max([1, 2.5]) is the float 2.5 and
// min([2.5, 1]) is the integer 1, printed 2.5 and 1. Comparing them in the i32 domain truncated the
// float first, which answered max([1, 2.5]) as 2 (roadmap L11.1). ok is false when some element is
// computed at runtime, because then no static winner exists.
func (g *irGen) numericFoldElems(elems []Expr) (vals []float64, isFloatElem []bool, ok bool) {
	vals = make([]float64, 0, len(elems))
	isFloatElem = make([]bool, 0, len(elems))
	for _, el := range elems {
		// One candidate rule for the fold and for the verdict question the print path asks, so the
		// element the module selects and the element the renderer names cannot be chosen twice.
		// A verdict is foldable as the number it compares to (1/0), which is also what lets
		// max([True, 1.5]) see the double underneath it instead of truncating it to 1 (Gap R.117).
		if v, isF, foldable := minMaxCandidateValue(el, g.foldableCandidate); foldable {
			vals = append(vals, v)
			isFloatElem = append(isFloatElem, isF)
			continue
		}
		return nil, nil, false
	}
	return vals, isFloatElem, true
}

// foldableCandidate is the compiled backend's half of the candidate rule: everything the source
// does not write as a literal but this backend can still read a number from — a constant-folded
// name, an element a literal wrote into a slot. It is a hook on BoolEnv as well as a step of
// numericFoldElems for exactly one reason: the two questions must not own two rules.
func (g *irGen) foldableCandidate(e Expr) (float64, bool, bool) {
	if fv, isF := g.floatEval(e); isF {
		return fv, true, true
	}
	// An element the program wrote into a literal-backed container: reading the slot and reading
	// the expression that filled it are the same number (ADR 0243), so the winner of a comparison
	// over it is still settled by the source and the renderer may name its kind.
	if ix, ok := e.(*Index); ok {
		if el, ok2 := g.staticNumericElem(ix); ok2 {
			return minMaxCandidateValue(el, nil)
		}
	}
	return 0, false, false
}

// isFloatNumericOperand is the question the numeric folds ask, which isFloat no longer answers
// for a container: what type should the accumulator have. sum([1.5, 2.5]) and max([1.5, 2.5])
// fold a literal container into one number, and that number is a double when any element is.
// isFloat itself says a container is not a float — `[1.5] == [1.5]` is a structural comparison of
// two containers, and reading it as an fcmp of two truncated handles was how print([1.5]) came to
// answer [1] — so the folds ask the element question here, out loud, rather than having a list
// claim to be a number (roadmap L11.1, ADR 0233).
func (g *irGen) isFloatNumericOperand(e Expr) bool {
	if g.isFloat(e) {
		return true
	}
	switch n := e.(type) {
	case *ListLit:
		for _, el := range n.Elems {
			if g.isFloat(el) {
				return true
			}
		}
	case *SetLit:
		for _, el := range n.Elems {
			if g.isFloat(el) {
				return true
			}
		}
	}
	return false
}

// floatValue emits a double IR operand for a float-typed expression e.
func (g *irGen) floatValue(b *strings.Builder, e Expr) string {
	// Asking for a double is what licenses the numeric door: everything lowered underneath this call
	// is headed for the double domain, and a slot read reaching the tag dispatch from here has an arm
	// that can produce the double its caller wants (roadmap Gap R.88).
	g.doubleDomain++
	defer func() { g.doubleDomain-- }()
	// `mod.NAME` for a data import carries its own double: the value is the literal the module declares,
	// and the generic path below would lower the attribute through `value()` — the integer road — and
	// `sitofp` the truncated word, so `print(math.PI)` came out `3.0` beside the record's
	// `3.141592653589793` (roadmap L11.6, the typed stdlib constants).
	if lit, folded := g.foldedModuleAttr(e); folded {
		return g.floatValue(b, lit)
	}
	switch n := e.(type) {
	case *FloatLit:
		t := g.newTmp()
		materialiseFloat(b, t, n.Value)

		return t
	case *CondExpr:
		// The double domain's ternary: `select i1 %c, double %a, double %b` — the instruction roadmap
		// Gap R.102 was filed for, and the reason `def f(x): x = x + 1.5` / `return x if x > 2 else 0.0`
		// had to be refused. Every arm that is not already a double converts, because the caller asked
		// for a double and that conversion is what the reference's own promotion means (ADR 0262).
		return g.ternaryDouble(b, n)
	case *Index:
		// The numeric half of the slot read (roadmap L11.1, ADR 0243): a slot whose literal is a float
		// *is* that float for arithmetic. The word the slot holds is a float box handle, and lifting one
		// of those straight into an fadd is the wrong answer ADR 0233 exists to keep out — so the double
		// comes from the element the compiler can see instead.
		if el, ok := g.staticNumericElem(n); ok {
			return g.floatValue(b, el)
		}
		// The other half: the literal no longer describes the element, but the object remembers what it
		// wrote beside it. The pair comes out of the slot and the tag decides — unbox, convert, or raise
		// what CPython raises for this operator and that kind (roadmap L11.1, Gap R.88).
		if g.numCtx != nil {
			if fam, ok := g.taggedNumberOperands(g.numCtx); ok {
				if v, okUse, err := g.taggedFloatOperand(b, n, g.numCtx, fam); okUse && err == nil && v != "" {
					return v
				}
			}
		}
		return ""
	case *Name:
		if g.floatTemps != nil && g.floatTemps[n.Value] {
			return n.Value
		}
		if g.floatVars != nil && g.floatVars[n.Value] {
			t := g.newTmp()
			fmt.Fprintf(b, "  %s = load double, double* %%_%s\n", t, n.Value)
			return t
		}
		// A name the pair road bound — an arithmetic answer, a slot read, a loop element over a container
		// that mixes kinds — has no single word to widen: the payload beside its tag is a float BOX handle,
		// an interned text index, or the number itself, and only the tag says which. The float domain reaches
		// it through the same per-tag arms a slot read walks: the float arm unboxes, the int/bool arm
		// converts, and every other tag raises the sentence CPython writes for THIS operator and that kind
		// (roadmap L11.1, Gap R.148 — the row that kept `n / 4` and `n > 2.5` refusing while `2.5 - n`
		// answered). A widening that merely called `rt_lift_num` here would print the interned index of a
		// text as a number, which is the wrong-answer-at-exit-0 family this loop keeps filing.
		if g.taggedVars[n.Value] && g.taggedOrigin != nil && g.numCtx != nil {
			if d, okD, derr := g.pairDoubleInCtx(b, e); derr == nil && okD && d != "" {
				return d
			}
		}
		t := g.newTmp()
		fmt.Fprintf(b, "  %s = sitofp i32 %s to double\n", t, g.valueText(b, n))
		return t
	case *UnOp:
		if n.Op == "-" {
			// The double domain asks the same question the i32 road asks before it writes an instruction:
			// what kind is the operand? Lifting a text through `sitofp` here printed 1.5 for `-"hi" + 1.5`,
			// a number for a program the reference stops on, so the raise is emitted here too and the
			// unreachable double it leaves behind is never read (roadmap Gap R.137, ADR 0266).
			if kind, bad := g.negationOperandKind(n.X); bad {
				if kind == "" {
					ix, isIdx := n.X.(*Index)
					if !isIdx {
						return ""
					}
					if _, ok := g.emitBadNegationOfSlot(b, ix, n.Span()); !ok {
						return ""
					}
					return "0.000000e+00"
				}
				g.emitBadNegation(b, kind, n.Span())
				return "0.000000e+00"
			}
			// A negated slot read whose kind only the object carries goes to the tag dispatch, which is
			// the only place that can unbox a float slot rather than read its handle as a number.
			if g.taggedNegationApplies(n) {
				if v, ok, err := g.taggedFloatNegate(b, n); err != nil {
					g.floatUnlowerable = err.Error()
					return ""
				} else if ok && v != "" {
					t := g.newTmp()
					fmt.Fprintf(b, "  %s = fsub double 0.0, %s\n", t, v)
					return t
				}
			}
			fx := g.floatValue(b, n.X)
			if fx == "" {
				// `fsub double 0.0, ` with an empty operand is the module llc rejects, which ADR 0166
				// counts as the compiler's bug — so the shape is recorded and the caller refuses it.
				g.floatUnlowerable = fmt.Sprintf("the operand %s of the negation", g.exprSummary(n.X))
				return ""
			}
			t := g.newTmp()
			fmt.Fprintf(b, "  %s = fsub double 0.0, %s\n", t, fx)
			return t
		}
	case *BinOp:
		if n.Op == "and" || n.Op == "or" {
			// The double domain's `and`/`or`: `select i1 %c, double %a, double %b`, the instruction ADR 0262
			// wrote for a ternary's arms and the door a float answer reaches from an arithmetic context
			// (roadmap Gap R.147, ADR 0269).
			return g.logicDouble(b, n)
		}
		return g.floatBinOp(b, n)
	case *Call:
		if n.Fn != nil {
			if id, ok := n.Fn.(*Name); ok && g.builtinShadowed(id.Value) && !g.floatFuncs[id.Value] {
				// The name is the program's, so this is an ordinary call — and a call in this
				// backend returns i32 unless the program's own definition says otherwise. Lift
				// its result the way a plain int expression is lifted into float arithmetic
				// (what the switch's own default does), rather than folding it as the built-in
				// conversion, which is what produced an i32 operand in an fadd (Gap R.6).
				t := g.newTmp()
				fmt.Fprintf(b, "  %s = sitofp i32 %s to double\n", t, g.valueText(b, n))
				return t
			}
			if id, ok := n.Fn.(*Name); ok {
				if g.floatFuncs[id.Value] {
					t, err := g.call(b, n)
					if err != nil {
						return ""
					}
					return t
				}
			}
			if id, ok := n.Fn.(*Name); ok && id.Value == "sum" && len(n.Args) >= 1 {
				if lst, ok := n.Args[0].(*ListLit); ok {
					total := 0.0
					okAll := true
					for _, elem := range lst.Elems {
						fv, ok2 := g.floatEval(elem)
						if !ok2 {
							// An integer element of a sum that also holds a float is still a
							// number. Refusing to lift it sent the whole call through the i32
							// path, where 2.5 became 2: print(sum([1, 2.5])) answered 3.0 against
							// CPython's 3.5 (roadmap L11.1).
							if il, isInt := elem.(*IntLit); isInt {
								fv, ok2 = float64(il.Value), true
							}
						}
						if !ok2 {
							okAll = false
							break
						}
						total += fv
					}
					if okAll {
						t := g.newTmp()
						materialiseFloat(b, t, total)

						return t
					}
				}
			}
			if id, ok := n.Fn.(*Name); ok && (id.Value == "min" || id.Value == "max") {
				if lst, ok := n.Args[0].(*ListLit); ok {
					elems := lst.Elems
					fe := make([]float64, 0, len(elems))
					okAll := true
					for _, elem := range elems {
						fv, ok2 := g.floatEval(elem)
						if !ok2 {
							// The same lift as sum's: max([1, 2.5]) is 2.5, not 2.
							if il, isInt := elem.(*IntLit); isInt {
								fv, ok2 = float64(il.Value), true
							}
						}
						if !ok2 {
							okAll = false
							break
						}
						fe = append(fe, fv)
					}
					if okAll && len(fe) > 0 {
						t := g.newTmp()
						materialiseFloat(b, t, fe[numericWinner(fe, id.Value == "min")])

						return t
					}
					// The elements fold to numbers even when not every one of them is a float
					// literal; the winner decides, and an integer winner is the i32 path's to
					// answer (roadmap L11.1).
					if vals, floats, foldable := g.numericFoldElems(lst.Elems); foldable && len(vals) > 0 {
						best := numericWinner(vals, id.Value == "min")
						if !floats[best] {
							return ""
						}
						t := g.newTmp()
						materialiseFloat(b, t, vals[best])

						return t
					}
				}
				if len(n.Args) > 1 {
					// The varargs spelling is not a double-returning builtin: it hands back the winner,
					// and a winner that arrived as an int must leave as one. The integer fold answers that
					// case; this path keeps only an all-double comparison (roadmap Gap R.73, ADR 0256).
					if v, isFloatWinner, foldable := g.minMaxFoldedWinner(n.Args, id.Value == "min"); foldable {
						if !isFloatWinner {
							return ""
						}
						t := g.newTmp()
						materialiseFloat(b, t, v)

						return t
					}
					allDouble := true
					for _, a := range n.Args {
						if !g.isFloat(a) {
							allDouble = false
							break
						}
					}
					if !allDouble {
						return ""
					}
					best := g.floatValue(b, n.Args[0])
					for _, a := range n.Args[1:] {
						cand := g.floatValue(b, a)
						cmp := g.newTmp()
						if id.Value == "min" {
							fmt.Fprintf(b, "  %s = fcmp olt double %s, %s\n", cmp, cand, best)
						} else {
							fmt.Fprintf(b, "  %s = fcmp ogt double %s, %s\n", cmp, cand, best)
						}
						t := g.newTmp()
						fmt.Fprintf(b, "  %s = select i1 %s, double %s, double %s\n", t, cmp, cand, best)
						best = t
					}
					return best
				}
				fvals := make([]float64, 0, len(n.Args))
				allFold := true
				for _, a := range n.Args {
					fv, ok := g.floatEval(a)
					if !ok {
						allFold = false
						break
					}
					fvals = append(fvals, fv)
				}
				if allFold {
					if len(fvals) == 0 {
						return g.floatValue(b, n)
					}
					best := fvals[0]
					for _, fv := range fvals[1:] {
						if id.Value == "min" && fv < best {
							best = fv
						} else if id.Value == "max" && fv > best {
							best = fv
						}
					}
					t := g.newTmp()
					materialiseFloat(b, t, best)

					return t
				}
			}
			if id, ok := n.Fn.(*Name); ok && id.Value == "abs" && len(n.Args) == 1 {
				// The double door asks the same question the i32 door asks. Landing here with an operand that
				// has no number in it is what made `print(abs("hi") * 2.5)` answer `0.0` at exit 0: the operand
				// lowered to nothing, and the intrinsic took an empty operand (roadmap Gap R.140, ADR 0271).
				if kind, bad := g.absOperandKind(n.Args[0]); bad {
					raised := false
					if ix, isIdx := n.Args[0].(*Index); isIdx {
						_, raised = g.emitBadAbsOfSlot(b, ix, n.Span())
					}
					if !raised {
						// One kind, or a slot the literal does not describe: the same raise, from the name the
						// door already has.
						g.emitBadAbs(b, kind, n.Span())
					}
					return "0.0"
				}
				t := g.newTmp()
				fmt.Fprintf(b, "  %s = call double @llvm.fabs.f64(double %s)\n", t, g.floatValue(b, n.Args[0]))
				return t
			}
			if id, ok := n.Fn.(*Name); ok && id.Value == "sqrt" {
				// The domain is guarded here too: a negative asks for CPython's ValueError rather than
				// the `nan` the intrinsic hands back (roadmap Gap R.51, ADR 0264).
				sv, raised, err := g.sqrtDouble(b, n.Args[0], n.Span())
				if err != nil || raised {
					return "0.000000e+00"
				}
				return sv
			}
			if id, ok := n.Fn.(*Name); ok && id.Value == "round" && len(n.Args) == 2 {
				// The double domain's digit-count round: one runtime call, not an i32 truncation of one.
				d, raised, err := g.roundDigitsDouble(b, n.Args[0], n.Args[1], n.Span())
				if err != nil {
					return ""
				}
				if raised {
					// Dead code past the raise; a well-formed double keeps the module the verifier accepts.
					return "0.000000e+00"
				}
				if d == "" {
					return ""
				}
				return d
			}
			if id, ok := n.Fn.(*Name); ok && (id.Value == "floor" || id.Value == "ceil") {
				// The whole-number answer, asked on the road that wants a double: `floor(3.7) + 1.5`
				// is an int beside a float, so the answer is computed in the int word and lifted — the
				// same (payload, widen) step ADR 0243 does for an element. It is not computed as a
				// double any more: that is what printed `3.0` for `floor(3.7)` (Gap R.51, ADR 0264).
				who := id.Value
				if len(n.Args) != 1 {
					return ""
				}
				iv, err := g.floorCeilValue(b, who, n.Args[0], n.Span())
				if err != nil || iv == "" {
					return ""
				}
				rt := g.newTmp()
				fmt.Fprintf(b, "  %s = sitofp i32 %s to double\n", rt, iv)
				return rt
			}
			// float() with no argument is the constructor that answers 0.0. It needs an ARM here, not
			// just the fold: this road is asked before floatEval is, and falling through made the print
			// road widen an i32 that was never one — `%t1 = sitofp i32 0.0e+00 to double`, which llc-20
			// rejects (roadmap Gap R.131, ADR 0287). The emitted shape is the one a literal 0.0 takes,
			// so nothing downstream can tell the two apart.
			if id, ok := n.Fn.(*Name); ok && id.Value == "float" && len(n.Args) == 0 && !g.builtinShadowed(id.Value) {
				t := g.newTmp()
				materialiseFloat(b, t, 0)

				return t
			}
			if id, ok := n.Fn.(*Name); ok && id.Value == "float" && len(n.Args) == 1 {
				arg := n.Args[0]
				if g.isFloat(arg) {
					return g.floatValue(b, arg)
				}
				switch a := arg.(type) {
				case *IntLit:
					t := g.newTmp()
					fmt.Fprintf(b, "  %s = sitofp i32 %d to double\n", t, a.Value)
					return t
				case *StrLit:
					if fv, okF := floatFromText(a.Value); okF {
						t := g.newTmp()
						materialiseFloat(b, t, fv)

						return t
					}
					// The fold has no answer, which is not the same as the operand having no number: the
					// fall-through below widened the text's @str_tab index with `sitofp` and printed `0.0`
					// for a program the reference stops with a ValueError (roadmap Gap R.37, ADR 0326 — the
					// silent sibling of the refusal the i32 road used to print).
					g.constantTrap(b, "ValueError", floatLiteralSentence(a.Value), n.Span())
					t := g.newTmp()
					materialiseFloat(b, t, 0)
					return t
				}
				if kind := g.unconvertibleArg(arg); kind != "" {
					// The double road's own version of the converter's complaint: widening the word a None,
					// a container or a text lowers to is how `float(None)` printed `0.0` and `float([1, 2])`
					// printed an empty line for a program the reference stops (roadmap Gap R.37, ADR 0326).
					g.constantTrap(b, "TypeError", convertArgumentSentence("float", kind), n.Span())
					t := g.newTmp()
					materialiseFloat(b, t, 0)
					return t
				}
				t := g.newTmp()
				fmt.Fprintf(b, "  %s = sitofp i32 %s to double\n", t, g.valueText(b, arg))
				return t
			}
		}
	}
	// A text is not a number waiting to be widened. The fall-through below converts the i32 the
	// expression lowers to, and for a text that i32 is its index in `@str_tab` — so a plain text arriving
	// at an arithmetic operator emitted `sitofp i32 %t2 to double` and `frem`'d it against the other
	// operand, answering `0.0` at exit 0 where the reference raises TypeError (roadmap L11.2, Gap R.165,
	// ADR 0282). Refusing here is the same judgement the sibling shapes already make — `"%d items" % 3`
	// and `"%s!" % "hi"` exit 1 naming the operator and both kinds — reached one road earlier: those
	// arrive with a left operand the checker types as text.
	//
	// This is NOT the formatting door's territory: `"%.2f" % 3.5` never reaches here, because the door
	// answers that shape as a text and the print road prints it (Gap R.31, ADR 0360). What is left at this
	// guard is a text with no `%` behind it at all — `"a" * 2.5`, `"a" + 1.5` — whose reference answer is
	// a TypeError, not a number.
	// The record-then-refuse convention is ADR 0166's: a substitute digit is the compiler's bug.
	// A comparison is not a numeric use: `1.0 == "a"` answers `False` in the reference and `0` here,
	// and the equality road reaches this lift for both of its operands. Only the arithmetic operators
	// — the ones that answer a number or raise — refuse a text operand (roadmap L11.2, Gap R.165).
	if (isStringExpr(e) || g.exprIsString(e)) && !comparisonOpForTextGuard(g.numCtx) {
		g.floatUnlowerable = floatLowerCompleteMarker + fmt.Sprintf("%s is a text, which has no double to widen from: it is not a format either, so no door gives it a digit, and the only text-to-number roads this backend builds are int(), float() and round() (roadmap L11.2, Gap R.165)", g.exprSummary(e))
		g.noteUnlowered(e, fmt.Errorf("codegen: %s cannot be lifted to a double: it is a text, and the only text-to-number roads this backend builds are int(), float() and round(); the formatting door answers a `%%`-format, and this operand is not one it was asked to format (roadmap L11.2, Gap R.165, ADR 0166)", g.exprSummary(e)))
		return ""
	}
	// A CONTAINER has no double to widen from either, and the shape here was worse than the text case
	// above: `print([1] / 2)` emitted `sitofp i32 @.lst1 to double`, because `value()` renders a list
	// literal as the address of a compile-time global. llc rejected the compiler's own module and the
	// exit-code contract called a program the reference stops with a TypeError a compiler bug
	// (roadmap Gap R.175, ADR 0292). Refused AT THE LIFT, the same choke point the text guard above
	// occupies — not beside whichever caller happened to be measured.
	// The guard belongs to an ARITHMETIC context: `g.numCtx` is set around the operands of a numeric
	// operator and empty when a value is simply being lifted for a call argument. `def half(xs):
	// return xs[0] / 2` with `half([1.5])` lowers the LIST as an argument through this lift — a
	// container is a legal argument, its slots are what the body reads — and refusing it there turned a
	// working 0.75 into a refusal, which the ladder forbids. Only an operand an operator is about to do
	// arithmetic WITH has no legal container reading.
	if g.arithOperandIsContainer(e) && g.numCtx != nil && !comparisonOpForTextGuard(g.numCtx) {
		k := g.containerKindName(e)
		if k == "" {
			k = "container"
		}
		// Where the reference RAISES for this operator and pair, the lift raises too — a compile-time
		// refusal for `[1] / 2` would put our words where the reference has a sentence of its own
		// (`unsupported operand type(s) for /: 'list' and 'int'`), and a program that catches
		// TypeError would not run (ADR 0215). The refusal below is only for a pair the reference
		// answers and this backend cannot build.
		if g.containerRaisesForPair(g.numCtx.Op, g.numCtx.L, g.numCtx.R) {
			multiplier := multiplierKindNameOf(g, otherOperandOfNumeric(g.numCtx, e))
			class, msg := containerNumberOp(g.numCtx.Op, multiplier, g.containerKindName(g.numCtx.L), g.containerKindName(g.numCtx.R))
			// raiseTo closes the block (it ends in a `br` to the handler), so the label the caller's
			// next instruction lands on has to be opened here — the same shape the min/max kind trap
			// uses when it raises from inside an expression. Without it the fadd the caller goes on to
			// emit dangles after a terminator, which is what llc then rejects.
			g.raiseTo(b, exnCode(class), class, msg, g.numCtx.Span())
			ok := g.newLabel("containerarith")
			fmt.Fprintf(b, "%s:\n", ok)
			// The caller is in the DOUBLE domain and will splice this string after a `double`, so it
			// must be a double constant: `fdiv double 0, %t1` is llc saying "integer constant must
			// have integer type", and a raise means the value is never read anyway.
			return "0.0"
		}
		g.floatUnlowerable = floatLowerCompleteMarker + fmt.Sprintf("%s is a %s, which has no double to widen from — the reference stops with a TypeError of its own, and this backend builds no number from a %s", g.exprSummary(e), k, k)
		g.noteUnlowered(e, fmt.Errorf("codegen: %s cannot be lifted to a double: it is a %s, and the only container-to-number roads this backend builds are len() and an explicit int()/float() cast (roadmap L11.1, Gap R.175, ADR 0166)", g.exprSummary(e), k))
		return ""
	}
	t := g.newTmp()
	fmt.Fprintf(b, "  %s = sitofp i32 %s to double\n", t, g.valueText(b, e))
	return t
}

// floatBinOp emits float arithmetic/comparison for a float-typed BinOp.
func (g *irGen) floatBinOp(b *strings.Builder, n *BinOp) string {
	// The numeric door (taggedFloatOperand) needs to know which operator and which source span the
	// raise it emits belongs to; floatValue is reached from here, so the context is set around the
	// call and restored after it, the way a nested expression's own operator belongs to itself.
	prev := g.numCtx
	g.numCtx = n
	defer func() { g.numCtx = prev }()
	// `**` refuses two shapes before any instruction is built, because `llvm.pow.f64` answers both with a
	// number the reference never produces: `0 ** -1` comes back `inf` (which the caller then truncated to
	// `2147483647`) and `(-8) ** (1/3)` comes back `nan` where the reference has a COMPLEX number. The
	// first is the reference's own ZeroDivisionError, catchable and worded as it words it; the second is
	// refused in words, because this language has no complex value to answer with (Gap R.176, ADR 0293).
	if n.Op == "**" {
		// The zero-base raise is emitted as a RUNTIME guard in the `**` case below, which is what lets a
		// base the compiler cannot see at compile time — a parameter, a slot, a name bound in a `try:`
		// arm — raise the same way a literal does. Doing it here instead left the frame's `ret` naming a
		// temporary that was allocated but never written: `use of undefined value ’%t3’`, llc rejecting
		// OUR module, which ADR 0166 counts as exit 2.
		if powerComplexIsAsked(n.L, n.R, g.powerReaders()) {
			g.floatUnlowerable = floatLowerCompleteMarker + fmt.Sprintf("`%s` is a negative base raised to a fractional power, which the reference answers with a complex number", exprSurface(n))
			g.noteUnlowered(n, fmt.Errorf("`%s` is a negative base raised to a fractional power: the reference answers a complex number ((1.0000000000000002+1.7320508075688772j) for `(-8) ** (1/3)`), and this language has no complex value — the compiled leg declines rather than print NaN (roadmap Gap R.176, ADR 0166)", exprSurface(n)))
			return ""
		}
	}
	l := g.floatValue(b, n.L)
	r := g.floatValue(b, n.R)
	if l == "" || r == "" {
		// An operand the lift cannot reach must never reach the instruction. `fdiv double , %t1` is
		// llc rejecting the compiler's own module, which ADR 0166 counts as our bug rather than the
		// program's, so the shape is recorded and the caller refuses it as a front-end diagnostic
		// (roadmap Gap R.88).
		//
		// It is *recorded and returned empty*, not filled with `0.0`: an earlier draft of this path left
		// the substitute in the instruction whenever the caller did not check the record, and `print
		// (xs[0] / 2)` of a container the program built printed `0.0` with exit 0 — a wrong answer is a
		// worse exit than a refusal, and ADR 0166 puts the blame on us either way (Gap R.96's measurement).
		missing, which := n.L, "left"
		if l != "" {
			missing, which = n.R, "right"
		}
		// A record the lift itself wrote may already name the true cause (a text has no double to
		// widen from, which is not the slot story the generic sentence below tells); only when nothing
		// was recorded does the BinOp compose the operand-and-operator form (roadmap L11.2, Gap R.165,
		// ADR 0282).
		if !strings.HasPrefix(g.floatUnlowerable, floatLowerCompleteMarker) {
			g.floatUnlowerable = fmt.Sprintf("the %s operand %s of %q", which, g.exprSummary(missing), n.Op)
		}
		// Sticky as well as returned: the callers that check the record refuse with the good message, and
		// the ones that do not are caught here rather than emitting `fadd double , %t1` for `llc` to
		// reject — which ADR 0166 counts as the compiler's own bug, exit 2 (roadmap Gap R.96).
		g.noteUnlowered(n, fmt.Errorf("%s of %q has no number the compiled backend can lift: a slot whose "+
			"kind only the object knows is answered for printing, equality, length and ordering, and using it "+
			"as a number needs the tagged value word still owed here (roadmap L11.1, Gap R.82, ADR 0166)",
			g.exprSummary(missing), n.Op))
		return ""
	}
	t := g.newTmp()
	switch n.Op {
	case "+":
		fmt.Fprintf(b, "  %s = fadd double %s, %s\n", t, l, r)
	case "-":
		fmt.Fprintf(b, "  %s = fsub double %s, %s\n", t, l, r)
	case "*":
		fmt.Fprintf(b, "  %s = fmul double %s, %s\n", t, l, r)
	case "//":
		g.guardNonZeroFloat(b, r, n.Span(), g.floorDivMessage(n.L, n.R))
		fmt.Fprintf(b, "  %s = fdiv double %s, %s\n", t, l, r)
		q := g.newTmp()
		fmt.Fprintf(b, "  %s = call double @llvm.floor.f64(double %s)\n", q, t)
		return q
	case "/":
		g.guardNonZeroFloat(b, r, n.Span(), g.trueDivMessage(n.L, n.R))
		fmt.Fprintf(b, "  %s = fdiv double %s, %s\n", t, l, r)
	case "%":
		g.guardNonZeroFloat(b, r, n.Span(), "float modulo")
		// `frem` is libm's fmod — the truncated remainder — so `-7.0 % 2.0` would answer
		// -1. Same correction as the integer path: add the divisor back when the
		// remainder's sign differs from the divisor's (floorModFloat says it in Go).
		rm := g.newTmp()
		fmt.Fprintf(b, "  %s = frem double %s, %s\n", rm, l, r)
		// An exact remainder carries the divisor's sign (7.5 % -0.5 is -0.0); fmod gives it
		// the dividend's, which prints as a different number.
		zsig := g.newTmp()
		fmt.Fprintf(b, "  %s = call double @llvm.copysign.f64(double 0.0, double %s)\n", zsig, r)
		iszero := g.newTmp()
		fmt.Fprintf(b, "  %s = fcmp oeq double %s, 0.0\n", iszero, rm)
		base := g.newTmp()
		fmt.Fprintf(b, "  %s = select i1 %s, double %s, double %s\n", base, iszero, zsig, rm)
		rm = base
		nz := g.newTmp()
		fmt.Fprintf(b, "  %s = fcmp one double %s, 0.0\n", nz, rm)
		rneg := g.newTmp()
		fmt.Fprintf(b, "  %s = fcmp olt double %s, 0.0\n", rneg, rm)
		bneg := g.newTmp()
		fmt.Fprintf(b, "  %s = fcmp olt double %s, 0.0\n", bneg, r)
		diff := g.newTmp()
		fmt.Fprintf(b, "  %s = xor i1 %s, %s\n", diff, rneg, bneg)
		adj := g.newTmp()
		fmt.Fprintf(b, "  %s = and i1 %s, %s\n", adj, nz, diff)
		sum := g.newTmp()
		fmt.Fprintf(b, "  %s = fadd double %s, %s\n", sum, rm, r)
		fmt.Fprintf(b, "  %s = select i1 %s, double %s, double %s\n", t, adj, sum, rm)
	case "**":
		// `pow(0, -1)` is not an error to LLVM: `llvm.pow.f64` answers `inf`, which the caller then
		// truncated to `2147483647` and printed at exit 0. The reference raises ZeroDivisionError, so the
		// test is ours to emit — on the LIFTED values, which is what lets a base the compiler could not
		// see at compile time (`z = 0` / `print(z ** -1)`, a parameter, a slot) raise the same way a
		// literal does (Gap R.176, ADR 0293).
		if g.floatPowerNeedsZeroGuard(n) {
			zeroBase := g.newTmp()
			fmt.Fprintf(b, "  %s = fcmp oeq double %s, 0.000000e+00\n", zeroBase, l)
			g.markI1(zeroBase)
			negExp := g.newTmp()
			fmt.Fprintf(b, "  %s = fcmp olt double %s, 0.000000e+00\n", negExp, r)
			g.markI1(negExp)
			both := g.newTmp()
			fmt.Fprintf(b, "  %s = and i1 %s, %s\n", both, zeroBase, negExp)
			g.markI1(both)
			// branchRaise closes the guard's own two blocks and returns the block the continuation runs
			// in; the write continues into the SAME builder afterwards, exactly as the `/` guards beside
			// this do. Swapping in a fresh builder here swallowed the `pow` call and left the caller's
			// `%t3` undefined — llc rejecting OUR module, which ADR 0166 counts as exit 2.
			g.branchRaise(b, both, "ZeroDivisionError", "0.0 cannot be raised to a negative power", n.Span(), "pow0")
		}
		fmt.Fprintf(b, "  %s = call double @llvm.pow.f64(double %s, double %s)\n", t, l, r)
	case "==":
		bt := g.newTmp()
		fmt.Fprintf(b, "  %s = fcmp oeq double %s, %s\n", bt, l, r)
		fmt.Fprintf(b, "  %s = zext i1 %s to i32\n", t, bt)
		return t
	case "!=":
		bt := g.newTmp()
		fmt.Fprintf(b, "  %s = fcmp one double %s, %s\n", bt, l, r)
		fmt.Fprintf(b, "  %s = zext i1 %s to i32\n", t, bt)
		return t
	case "<":
		bt := g.newTmp()
		fmt.Fprintf(b, "  %s = fcmp olt double %s, %s\n", bt, l, r)
		fmt.Fprintf(b, "  %s = zext i1 %s to i32\n", t, bt)
		return t
	case "<=":
		bt := g.newTmp()
		fmt.Fprintf(b, "  %s = fcmp ole double %s, %s\n", bt, l, r)
		fmt.Fprintf(b, "  %s = zext i1 %s to i32\n", t, bt)
		return t
	case ">":
		bt := g.newTmp()
		fmt.Fprintf(b, "  %s = fcmp ogt double %s, %s\n", bt, l, r)
		fmt.Fprintf(b, "  %s = zext i1 %s to i32\n", t, bt)
		return t
	case ">=":
		bt := g.newTmp()
		fmt.Fprintf(b, "  %s = fcmp oge double %s, %s\n", bt, l, r)
		fmt.Fprintf(b, "  %s = zext i1 %s to i32\n", t, bt)
		return t
	default:
		fmt.Fprintf(b, "  %s = fadd double %s, %s\n", t, l, r)
	}
	return t
}

// floatEval returns the float64 value of a float-literal expression, if foldable.
func (g *irGen) floatEval(e Expr) (float64, bool) {
	switch n := e.(type) {
	case *FloatLit:
		return n.Value, true
	case *Index:
		// Same fold as floatValue: the element the literal wrote, not the word in the slot
		// (roadmap L11.1, ADR 0243).
		if el, ok := g.staticNumericElem(n); ok {
			return g.floatEval(el)
		}
		return 0, false
	case *BinOp:
		l, lok := g.floatEval(n.L)
		r, rok := g.floatEval(n.R)
		if !lok || !rok {
			return 0, false
		}
		switch n.Op {
		case "+":
			return l + r, true
		case "-":
			return l - r, true
		case "*":
			return l * r, true
		case "/":
			if r == 0 {
				return 0, false
			}
			return l / r, true
		case "**":
			return math.Pow(l, r), true
		}
		return 0, false
	case *Call:
		if n.Fn != nil {
			if id, ok := n.Fn.(*Name); ok && g.builtinShadowed(id.Value) && !g.floatFuncs[id.Value] {
				return 0, false
			}
			// float() with no argument IS the literal 0.0 — a constructor, not a conversion of a
			// missing operand — and the answer belongs HERE, with the other folds, so that every road
			// which already folds a double takes this call unchanged: the print road, the comparison
			// road, the arithmetic road (roadmap Gap R.131, ADR 0287). Answering it only at the call
			// site handed those roads an i32 they then widened — `%t1 = sitofp i32 0.0 to double`,
			// which llc-20 rejects with "floating point constant invalid for type".
			if id, ok := n.Fn.(*Name); ok && id.Value == "float" && len(n.Args) == 0 && !g.builtinShadowed(id.Value) {
				return 0, true
			}
			if id, ok := n.Fn.(*Name); ok && id.Value == "float" && len(n.Args) == 1 {
				switch a := n.Args[0].(type) {
				case *IntLit:
					return float64(a.Value), true
				case *StrLit:
					if f, err := strconv.ParseFloat(a.Value, 64); err == nil {
						return f, true
					}
				}
			}
		}
	}
	return 0, false
}
