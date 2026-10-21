// codegen_builtin_numeric.go is the builtin road for the numeric names: `sum`, `min`,
// `max`, `abs`, `sqrt`, `floor`, `ceil`, `round`, `int`, `float` and `bool`.
package lang

import (
	"fmt"
	"math"
	"strconv"
	"strings"
)

// builtinBool is the road `bool` takes: the truthiness of the argument, as a value
func (g *irGen) builtinBool(b *strings.Builder, c *Call, fnName string) (string, error) {
	// bool() is a constructor answering False, and bool(x) asks the same question the `if` road
	// asks, answered by the select the `str()` road beside it already emits for a verdict (ADR
	// 0257: a verdict writes its own name, not the 1 the slot holds). The name reached the
	// `unsupported call` fall-through, which is ADR 0166's exit 1 vocabulary for a feature the
	// PROGRAM lacks — and `print(bool())` is a program the reference prints `False`
	// (roadmap Gap R.131, ADR 0287).
	// bool() answers a VERDICT, and a verdict in this backend is the WORD 1 or 0 that
	// rt_print_bool turns into True/False — not an interned "True"/"False" text. My first
	// version selected between the two interned strings, so `print(bool(1))` handed the index
	// of "True" to a printer that asks only whether the word is zero and answered `False`, and
	// `print(bool(0))` answered `True`: the pair of answers was INVERTED, which no amount of
	// reading the select would reveal (roadmap Gap R.131, ADR 0287).
	if len(c.Args) == 0 {
		return "0", nil
	}
	if len(c.Args) != 1 {
		return "", fmt.Errorf("bool expects one argument")
	}
	// A self-describing subscript asks the same question in the same table as an `if`, an f-string and `and`/`or`.
	// Its payload is meaningless on its own: a text's intern index and a container's heap handle are both nonzero,
	// while a float box for `0.0` is also nonzero and `None` is zero for reasons that have nothing to do with the
	// reference's truth (roadmap Gap R.284, ADR 0388).
	if nm, isName := c.Args[0].(*Name); isName {
		if t, asks := g.truthAsksTheLoopItem(nm); asks {
			payload, perr := g.value(b, nm)
			if perr != nil {
				return "", perr
			}
			return g.asBoolI32(b, g.pairTruth(b, payload, fmt.Sprintf("%d", t))), nil
		}
	}
	if ix, isIdx := c.Args[0].(*Index); isIdx && (g.entryReadNeedsTheTag(ix) || g.isPairElem(ix)) {
		if payload, tag, okPair, xerr := g.subscriptPairOf(b, ix); xerr != nil {
			return "", xerr
		} else if okPair {
			bit := g.pairTruth(b, payload, tag)
			return g.asBoolI32(b, bit), nil
		}
		if payload, tag, okPair, xerr := g.conditionPairOfRead(b, ix); xerr != nil {
			return "", xerr
		} else if okPair {
			bit := g.pairTruth(b, payload, tag)
			return g.asBoolI32(b, bit), nil
		}
	}
	// A CONTAINER's truthiness is whether it has any elements, which is a question about its
	// length and therefore about the object, not about the word the slot holds. Comparing that word
	// against zero is what my first version did, and it failed twice over: for a list literal the
	// word is a GLOBAL and llc-20 rejects the module (`icmp ne i32 @.lst1, 0` — "global variable
	// reference must have pointer type"), and for a container the answer would have been "non-empty"
	// for every handle ever allocated, `bool([])` included. Refusing is the honest answer until the
	// tagged value word lets the object be asked (roadmap Gap R.131, ADR 0287, L11.1).
	if g.nameIsContainerRecorded(c.Args[0]) {
		return "", fmt.Errorf("codegen: a container's truthiness is its length, and this backend cannot ask an object how many elements it has from a call to bool(); CPython answers it (roadmap Gap R.131, ADR 0166)")
	}
	// A TEXT asks a different question than a number: `bool("")` is False and `bool("x")` is
	// True, and a text's word is its interned INDEX, whose being non-zero says nothing about
	// whether it has any characters. So the text road measures it (rt_str_len), and only the
	// number road gets to use "the word is not zero" (roadmap Gap R.131, ADR 0287).
	bv, err := g.value(b, c.Args[0])
	if err != nil {
		return "", err
	}
	probe := bv
	if g.exprIsString(c.Args[0]) || isStringExpr(c.Args[0]) {
		ln := g.newTmp()
		b.WriteString(fmt.Sprintf("  %s = call i32 @rt_str_len(i32 %s)\n", ln, bv))
		probe = ln
	}
	// The verdict word itself: 1 when the probe is non-zero, 0 otherwise. This is the same
	// shape rt_print_bool asks, with the printer's own test hoisted into the caller.
	bit := g.newTmp()
	b.WriteString(fmt.Sprintf("  %s = icmp ne i32 %s, 0\n", bit, probe))
	val := g.newTmp()
	b.WriteString(fmt.Sprintf("  %s = zext i1 %s to i32\n", val, bit))
	return val, nil
}

// builtinFloat is the road `float` takes
func (g *irGen) builtinFloat(b *strings.Builder, c *Call, fnName string) (string, error) {
	// float(x) converts x to a float. The AOT backend represents floats
	// as truncated ints (value() truncates FloatLit to int64), so
	// float(int) folds to itself and float(str) parses the string to a
	// float then truncates, mirroring the interpreter's allocFloat.
	// float() is a CONSTRUCTOR answering 0.0, and it is exactly the literal `0.0`: returning a
	// folded FloatLit lets every road that already knows how to print a double take this one
	// unchanged. My first version emitted `g.floatValue(b, &FloatLit{Value: 0})` from the call
	// site, which handed the print road a double it then widened AGAIN — `%t2 = fadd double ...`
	// followed by `%t1 = sitofp i32 %t2 to double`, LLVM's verifier happy, the program printing
	// an empty line at exit 0 (roadmap Gap R.131, ADR 0287). A shape the compiler can express as
	// a literal should BE one, not a hand-rolled sequence with the same meaning.
	// float() is a CONSTRUCTOR answering 0.0. It is answered as a DOUBLE ROAD below like any
	// other float-returning expression; see isFloat's *Call arm, which already answers "yes" for
	// this name so print picks rt_fmt_double rather than %d (roadmap Gap R.131, ADR 0287).
	if len(c.Args) == 0 {
		// Folded like a literal: see floatEval's *Call arm for why the answer must come from
		// there rather than from a textual double handed back here.
		if fv, ok := g.floatEval(c); ok {
			return floatConst(fv), nil
		}
		return "", fmt.Errorf("float expects one argument")
	}
	if len(c.Args) != 1 {
		return "", fmt.Errorf("float expects one argument")
	}
	// A name the pair road bound has no one word to widen — the payload beside its tag is a float box,
	// an interned text, or the number, and only the tag says which. `rt_lift_num` answers the double and
	// raises CPython's own sentence for a kind that has no number in it (roadmap L11.1, Gap R.148).
	if nm, isName := c.Args[0].(*Name); isName && g.taggedVars[nm.Value] {
		p, t := g.numericPairRegs(b, nm.Value, nm.Span())
		return g.liftPair(b, p, t), nil
	}
	if il, ok := c.Args[0].(*IntLit); ok {
		return fmt.Sprintf("%d", il.Value), nil
	}
	if bl, ok := c.Args[0].(*BoolLit); ok {
		// A verdict IS a number to the reference: `float(True)` is 1.0 and the i32 road below answers the
		// word 1 for it, exactly as the `int` fold beside it does (roadmap Gap R.213, ADR 0326).
		if bl.Value {
			return "1", nil
		}
		return "0", nil
	}
	if sv, ok := stringConst(c.Args[0], g.builtinShadowed); ok {
		f, okNum := floatFromText(sv)
		if !okNum {
			g.constantTrap(b, "ValueError", floatLiteralSentence(sv), c.Span())
			return "0", nil
		}
		return fmt.Sprintf("%d", int64(f)), nil
	}
	if kind := g.unconvertibleArg(c.Args[0]); kind != "" {
		// `float(None)` and `float([1, 2])` answered `0.0` and an empty line: the general road below
		// widens whatever word the operand lowers to, and for a text, a None or a container that word is
		// an interned index, a singleton or a handle. The reference names the operand's type and stops the
		// program (roadmap Gap R.37, ADR 0326).
		g.constantTrap(b, "TypeError", convertArgumentSentence("float", kind), c.Span())
		return "0", nil
	}
	// General arg: emit a real double value. floatValue already converts
	// ints to doubles (sitofp) and passes through float values, so
	// float(x) == floatValue(x) for numeric args.
	v := g.floatValue(b, c.Args[0])
	if v == "" {
		return "", fmt.Errorf("float: unsupported argument")
	}
	return v, nil
}

// builtinRound is the road `round` takes
func (g *irGen) builtinRound(b *strings.Builder, c *Call, fnName string) (string, error) {
	// round(x) folds a constant integer literal to itself (mirroring the
	// interpreter's int case; the AOT backend has no float representation).
	// A float goes to the nearest value with ties to EVEN: `llvm.roundeven.f64` is IEEE
	// roundTiesToEven and `math.RoundToEven` is the same operation on the host, so the constant
	// fold, the runtime call and the interpreter cannot drift the way `llvm.round.f64` and
	// `math.Round` did — both tied away from zero, both wrong where CPython ties to even.
	//
	// The two-argument form is a different operation and lives in round_digits.go: a digit
	// count moves the decimal point rather than asking for a whole number, so `round(x, n)`
	// answers with a float (`round(3.5, 0)` is `4.0`) and has to say so (Gap R.69, ADR 0263).
	if len(c.Args) == 0 || len(c.Args) > 2 {
		return "", fmt.Errorf("%s", roundArityMessage(len(c.Args)))
	}
	if len(c.Args) == 2 {
		return g.roundDigitsValue(b, c)
	}
	if g.isFloat(c.Args[0]) {
		if fv, ok := g.floatEval(c.Args[0]); ok {
			return fmt.Sprintf("%d", int64(math.RoundToEven(fv))), nil
		}
		fx := g.floatValue(b, c.Args[0])
		rt := g.newTmp()
		fmt.Fprintf(b, "  %s = call double @llvm.roundeven.f64(double %s)\n", rt, fx)
		t := g.newTmp()
		fmt.Fprintf(b, "  %s = fptosi double %s to i32\n", t, rt)
		return t, nil
	}

	rv, rerr := g.constIntVal(c.Args[0])
	if rerr != nil {
		if _, ok := c.Args[0].(*StrLit); ok {
			return "", fmt.Errorf("round: cannot round a string")
		}
		v, verr := g.value(b, c.Args[0])
		if verr != nil {
			return "", verr
		}
		return v, nil
	}
	return fmt.Sprintf("%d", rv), nil
}

// builtinInt is the road `int` takes: the conversions, the radix forms and the raise each one gets
func (g *irGen) builtinInt(b *strings.Builder, c *Call, fnName string) (string, error) {
	// int() with no argument is a CONSTRUCTOR and answers 0 — a constant the compiler can emit
	// without asking anything of an operand that does not exist. It used to answer
	// "int expects one argument" at exit 1 for a program the reference prints `0`, which ADR 0166
	// reserves exit 1 for the program's own missing features, not the compiler's untouched shapes
	// (roadmap Gap R.131, ADR 0287). Too many arguments still refuse; there is no honest 0-arg
	// answer to a 2-argument call.
	if len(c.Args) == 0 {
		return "0", nil
	}
	if len(c.Args) != 1 {
		return "", fmt.Errorf("int expects one argument")
	}
	if g.isFloat(c.Args[0]) {
		if fv, ok := g.floatEval(c.Args[0]); ok {
			return strconv.FormatInt(int64(fv), 10), nil
		}
		t := g.newTmp()
		fmt.Fprintf(b, "  %s = fptosi double %s to i32\n", t, g.floatValue(b, c.Args[0]))
		return t, nil
	}

	if il, ok := c.Args[0].(*IntLit); ok {
		return fmt.Sprintf("%d", il.Value), nil
	}
	if bl, ok := c.Args[0].(*BoolLit); ok {
		// A verdict IS the number 1 or 0 to the reference — `int(True)` is 1 — and this fold already
		// answered for an `int` literal, so refusing it was a hole in one door rather than a missing
		// feature (roadmap Gap R.213, closed by ADR 0326 beside the traps this row owns).
		if bl.Value {
			return "1", nil
		}
		return "0", nil
	}
	if sl, ok := c.Args[0].(*StrLit); ok {
		n, okNum, tooWide := intFromText(sl.Value)
		if tooWide {
			// Not this row's decision: the reference's `int` is unbounded and this backend's word is
			// an i64, so folding `int("99999999999999999999")` would answer a wrong number at the exit
			// code of success. A refusal is the honest direction to be wrong in (roadmap L12.12).
			return "", fmt.Errorf("codegen: int(%s) is an integer wider than this backend's word: the reference has an unbounded int and this one has a 64-bit slot, so the fold would answer a plausible wrong number rather than the reference's — the bounded-vs-arbitrary decision roadmap L12.12 is waiting for (ADR 0326)", exprSurface(c.Args[0]))
		}
		if !okNum {
			// The reference does not refuse this program: it raises ValueError and lets the program's
			// `except ValueError:` answer. The fold used to stop the whole compilation at exit 1, which
			// is a different language event and unreachable from the handler (roadmap Gap R.37, ADR 0326).
			g.constantTrap(b, "ValueError", intLiteralSentence(sl.Value), c.Span())
			return "0", nil
		}
		return fmt.Sprintf("%d", n), nil
	}
	if kind := g.unconvertibleArg(c.Args[0]); kind != "" {
		// The reference does not fold these: it raises TypeError and names the operand's type. The
		// fold used to answer "codegen folds only literal int/string args", which is a fact about this
		// compiler rather than about the program (roadmap Gap R.37, ADR 0326).
		g.constantTrap(b, "TypeError", convertArgumentSentence("int", kind), c.Span())
		return "0", nil
	}
	return "", fmt.Errorf("int: codegen folds only literal int/string args")
}

// builtinFloorCeil is the road `floor` and `ceil` take
func (g *irGen) builtinFloorCeil(b *strings.Builder, c *Call, fnName string) (string, error) {
	// The whole-number question, answered in the word a whole number travels in. Both backends
	// used to disagree with the reference and with each other here: the evaluator trapped
	// `NameError` on a name the checker predeclares, and the compiled leg returned the double
	// (`print(floor(3.7))` → `3.0` where `math.floor` answers the int `3`).
	who := "floor"
	if n, ok := c.Fn.(*Name); ok && n.Value == "ceil" {
		who = "ceil"
	}
	if len(c.Args) != 1 {
		return "", fmt.Errorf("%s", mathNameArityMessage(who, len(c.Args)))
	}
	return g.floorCeilValue(b, who, c.Args[0], c.Span())
}

// builtinSqrt is the road `sqrt` takes
func (g *irGen) builtinSqrt(b *strings.Builder, c *Call, fnName string) (string, error) {
	// sqrt promotes its argument to a real number and answers the square root as a float —
	// and RAISES where the argument is negative, which is what the reference does with it.
	// Until ADR 0264 a visible negative was a compile-time refusal (exit 1 for a program
	// CPython runs) and a run-time negative answered `nan`, a value where the reference has a
	// raise (roadmap Gap R.51).
	if len(c.Args) != 1 {
		return "", fmt.Errorf("%s", mathNameArityMessage("sqrt", len(c.Args)))
	}
	sv, raised, serr := g.sqrtDouble(b, c.Args[0], c.Span())
	if serr != nil {
		return "", serr
	}
	if raised {
		// The block is closed over by the raise; a well-formed double keeps whatever is emitted
		// next parseable.
		return "0.000000e+00", nil
	}
	return sv, nil
}

// builtinAbs is the road `abs` takes
func (g *irGen) builtinAbs(b *strings.Builder, c *Call, fnName string) (string, error) {
	// abs(x) -> x < 0 ? -x : x (constant-folded when x is a literal).
	if len(c.Args) != 1 {
		return "", fmt.Errorf("abs expects one argument")
	}
	if g.isFloat(c.Args[0]) {
		if fv, ok := g.floatEval(c.Args[0]); ok {
			if fv < 0 {
				fv = -fv
			}
			t := g.newTmp()
			materialiseFloat(b, t, fv)

			return t, nil
		}
	}

	if il, ok := c.Args[0].(*IntLit); ok {
		if il.Value < 0 {
			return fmt.Sprintf("%d", -il.Value), nil
		}
		return fmt.Sprintf("%d", il.Value), nil
	}
	// `abs` is a numeric use, so it asks the unary minus's question of an operand whose kind the source
	// spells (ADR 0266's door, one table, one wording): a text, `None`, a container or an instance has no
	// number in it, and negating the word the value happens to be stored as is the answer this door used
	// to give — `abs("hi")` printed `0`, `abs([1])` wrote `sub i32 0, @.lst1` and spent the contract's
	// "the compiler is broken" code on a program the reference merely stops on
	// (roadmap Gap R.140, ADR 0271).
	if kind, bad := g.absOperandKind(c.Args[0]); bad {
		raised := false
		if ix, isIdx := c.Args[0].(*Index); isIdx {
			_, raised = g.emitBadAbsOfSlot(b, ix, c.Span())
		}
		if !raised {
			// One kind, or a slot the literal does not fully describe: the same raise, from the name the
			// door already has. Falling through to the numeric road here is the answer this door used to
			// give, and it is the one ADR 0166 forbids.
			g.emitBadAbs(b, kind, c.Span())
		}
		return "0", nil
	}
	v, err := g.value(b, c.Args[0])
	if err != nil {
		return "", err
	}
	neg := g.newTmp()
	b.WriteString(fmt.Sprintf("  %s = sub i32 0, %s\n", neg, v))
	cmp := g.newTmp()
	b.WriteString(fmt.Sprintf("  %s = icmp slt i32 %s, 0\n", cmp, v))
	t := g.newTmp()
	b.WriteString(fmt.Sprintf("  %s = select i1 %s, i32 %s, i32 %s\n", t, cmp, neg, v))
	return t, nil
}

// builtinMinMax is the road `min` and `max` take, which is the candidate rule in
// boolvalue.go with the print road's kind claims beside it (Gap R.124)
func (g *irGen) builtinMinMax(b *strings.Builder, c *Call, fnName string) (string, error) {
	// `min(a, b, ...)` is a value chosen from values, not an error about not having written a
	// list. The fold returns the winner, which means its kind is the winner's kind too.
	//
	// A `key=` is answered here, not by the fold: the door asks the option what it is, calls it on
	// each candidate through the ordinary call road, and compares the answers with the comparator the
	// unkeyed choice already uses (Gap R.249, ADR 0340).
	if picked := g.keyedMinMaxCall(b, c); picked.done {
		return picked.handle, picked.err
	}
	if len(c.Args) != 1 {
		return g.emitScalarMinMax(b, c.Args, fnName == "min", c.Span())
	}
	// min/max(list) -> fold the elements of an inline list literal (unrolled).
	// min/max accept a single scalar value (treated as a one-element
	// collection): min(5) -> 5, max(7) -> 7, matching the interpreter.
	if il, ok := c.Args[0].(*IntLit); ok {
		return fmt.Sprintf("%d", il.Value), nil
	}
	// min/max over a lowered comprehension: fold the constant elements.
	if comp, ok := c.Args[0].(*Comp); ok {
		if _, err := g.comp(b, comp); err != nil {
			return "", err
		}
		if g.rtComps[comp] {
			return "", fmt.Errorf("%s of a comprehension whose elements are computed at runtime needs a runtime reduction (roadmap L11.7, ADR 0192)", fnName)
		}
		els := g.compEls[comp]
		if len(els) == 0 {
			return "", fmt.Errorf("%s of an empty comprehension", fnName)
		}
		best := els[0]
		for _, e := range els[1:] {
			if fnName == "min" && e < best {
				best = e
			}
			if fnName == "max" && e > best {
				best = e
			}
		}
		return fmt.Sprintf("%d", best), nil
	}
	var elems []Expr
	if de, ok := g.dictMethodElems(c.Args[0]); ok {
		elems = de
	}
	if le, ok := listCallElems(c.Args[0]); ok {
		elems = le
	}
	if ln, ok := c.Args[0].(*ListLit); ok {
		elems = ln.Elems
	} else if sl, ok := c.Args[0].(*SetLit); ok {
		elems = sl.Elems
	} else if dl, ok := c.Args[0].(*DictLit); ok {
		if len(dl.Keys) > 0 {
			// The interpreter rejects non-empty dict literals for min/max.
			return "", fmt.Errorf("%s expects a list or set", fnName)
		}
		elems = dl.Keys
	}
	if elems == nil {
		return "", fmt.Errorf("%s requires an inline list/set/dict literal", fnName)
	}
	if len(elems) == 0 {
		return "", fmt.Errorf("%s of an empty list", fnName)
	}
	for _, elem := range elems {
		if isContainerLiteral(elem) {
			// The fold compares the elements with icmp on i32, and an element that is a
			// container is a compile-time global there — a global in an i32 slot, which llc
			// refuses, and the exit-code contract counts as a compiler bug for an ordinary
			// program (ADR 0166). Python compares lists element by element and answers this;
			// the interpreter, whose elements are boxed, agrees with it (roadmap L11.1).
			return "", fmt.Errorf("%s compares its elements, and %s is a container: the compiled fold has no word for comparing two containers, so it would compare globals (CPython answers this program; roadmap L11.1)", fnName, exprSnippet(elem))
		}
	}
	if trap, raised, err := g.minMaxKindTrap(b, elems, fnName == "min", c.Span()); raised || err != nil {
		return trap, err
	}
	if g.minMaxVarargsAreText(elems) {
		return g.emitTextMinMax(b, elems, fnName == "min")
	}
	// When every element is a number the compiler can hold, the winner is decided here and
	// the answer is that element's own text. Comparing the i32 payloads instead truncated a
	// float on the way past g.value, so max([1, 2.5]) answered 2 (roadmap L11.1).
	if vals, floats, foldable := g.numericFoldElems(elems); foldable && len(vals) > 0 {
		best := numericWinner(vals, fnName == "min")
		if floats[best] {
			// The winner is a float, so this is the wrong domain to be asked in: the caller
			// that prints or binds the value asks g.isFloat first, and that question answers
			// yes for exactly this shape. Reaching here means the two rules disagreed, and a
			// truncated integer is what the disagreement used to produce.
			return "", fmt.Errorf("%s of these elements is the float %s, which the integer fold cannot return; the float path answers this program (roadmap L11.1)", fnName, floatConst(vals[best]))
		}
		return fmt.Sprintf("%d", int64(vals[best])), nil
	}
	best, err := g.value(b, elems[0])
	if err != nil {
		return "", err
	}
	for i := 1; i < len(elems); i++ {
		el, err := g.value(b, elems[i])
		if err != nil {
			return "", err
		}
		cmp := g.newTmp()
		op := "icmp sgt"
		if fnName == "min" {
			op = "icmp slt"
		}
		b.WriteString(fmt.Sprintf("  %s = %s i32 %s, %s\n", cmp, op, el, best))
		t := g.newTmp()
		b.WriteString(fmt.Sprintf("  %s = select i1 %s, i32 %s, i32 %s\n", t, cmp, el, best))
		best = t
	}
	return best, nil
}

// builtinSum is the road `sum` takes
func (g *irGen) builtinSum(b *strings.Builder, c *Call, fnName string) (string, error) {
	// sum(list) -> sum the elements of an inline list literal (unrolled).
	if len(c.Args) != 1 {
		return "", fmt.Errorf("sum expects one argument")
	}
	// sum over a lowered comprehension: the elements are folded constants,
	// so fold to a single constant at codegen time.
	if comp, ok := c.Args[0].(*Comp); ok {
		if _, err := g.comp(b, comp); err != nil {
			return "", err
		}
		if g.rtComps[comp] {
			// The elements are computed at runtime, so there is no compile-time element
			// set to add up. Folding the empty one would answer 0 for a list that has
			// elements — a wrong answer wearing the costume of a refusal (ADR 0192).
			return "", fmt.Errorf("sum of a comprehension whose elements are computed at runtime needs a runtime reduction (roadmap L11.7, ADR 0192)")
		}
		total := int64(0)
		for _, e := range g.compEls[comp] {
			total += e
		}
		return fmt.Sprintf("%d", total), nil
	}
	var elems []Expr
	if de, ok := g.dictMethodElems(c.Args[0]); ok {
		elems = de
	}
	if le, ok := listCallElems(c.Args[0]); ok {
		elems = le
	}
	if ln, ok := c.Args[0].(*ListLit); ok {
		elems = ln.Elems
	} else if sl, ok := c.Args[0].(*SetLit); ok {
		elems = sl.Elems
	} else if dl, ok := c.Args[0].(*DictLit); ok {
		if len(dl.Keys) > 0 {
			// The interpreter rejects non-empty dict literals for sum.
			return "", fmt.Errorf("sum expects a list or set")
		}
		elems = dl.Keys
	}
	if elems == nil {
		// Empty inline list/set/dict literals are valid (sum([]) -> 0),
		// so promote a nil slice to an empty one for those receivers.
		switch c.Args[0].(type) {
		case *ListLit, *SetLit, *DictLit:
			elems = []Expr{}
		default:
			return "", fmt.Errorf("sum requires an inline list/set/dict literal")
		}
	}
	if len(elems) == 0 {
		// sum([]) folds to 0, matching the interpreter.
		return "0", nil
	}
	for _, elem := range elems {
		if isContainerLiteral(elem) {
			// The fold would add the element's globals; Python answers this program with
			// `unsupported operand type(s) for +: 'int' and 'list'`, and this backend has no
			// operator dispatch to raise either, so the honest answer is the refusal
			// (roadmap L11.1, ADR 0166).
			return "", fmt.Errorf("sum adds numbers, and %s is a container: there is no numeric answer to give (Python raises TypeError for this program)", exprSnippet(elem))
		}
	}
	anyFloat := false
	fvals := make([]float64, 0, len(elems))
	for _, elem := range elems {
		// An element is asked what it adds. Text adds nothing here: Python raises
		// `unsupported operand type(s) for +: 'int' and 'str'`, and this fold answering 0
		// for sum(["a"]) is a number nobody asked for (roadmap L11.1, ADR 0166).
		if isStringExpr(elem) || g.printsAsInternedStr(elem) {
			return "", fmt.Errorf("sum adds numbers, and %s is text: Python raises TypeError for this program, and the compiled fold has no number to answer with (roadmap L11.1)", exprSnippet(elem))
		}
		if g.isFloat(elem) {
			anyFloat = true
		}
		if fv, ok := g.floatEval(elem); ok {
			fvals = append(fvals, fv)
			continue
		}
		// An integer element of a sum that also holds a float contributes its value. The
		// fold used to collect only the float elements and then, finding the list "not all
		// float", added everything in the i32 domain — so sum([1, 2.5]) printed 3.0, the
		// 2.5 truncated on the way through the integer path (roadmap L11.1).
		if il, ok := elem.(*IntLit); ok {
			fvals = append(fvals, float64(il.Value))
		}
	}
	if anyFloat {
		if len(fvals) == len(elems) {
			total := 0.0
			for _, fv := range fvals {
				total += fv
			}
			t := g.newTmp()
			materialiseFloat(b, t, total)

			return t, nil
		}
		// Some element is computed at runtime and the answer is a float: there is no i32
		// answer to fall back to, and truncating each element on the way through one is
		// the wrong answer this fold used to give (roadmap L11.7, ADR 0192).
		return "", fmt.Errorf("sum of a list whose elements are computed at runtime, with a float among them, needs a runtime reduction (roadmap L11.7)")
	}

	acc, err := g.value(b, elems[0])
	if err != nil {
		return "", err
	}
	for i := 1; i < len(elems); i++ {
		el, err := g.value(b, elems[i])
		if err != nil {
			return "", err
		}
		t := g.newTmp()
		b.WriteString(fmt.Sprintf("  %s = add i32 %s, %s\n", t, acc, el))
		acc = t
	}
	return acc, nil
}
