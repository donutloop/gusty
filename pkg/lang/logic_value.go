package lang

import (
	"fmt"
	"strings"
)

// logic_value.go — `a and b` / `a or b` choose an **operand**, not a verdict (roadmap Gap R.147, ADR 0269).
//
// CPython's `and` and `or` are the two operators that are not operators: they test the left operand and
// hand back whichever operand the test chose, unconverted. `print(2 and 3)` is `3`, `print(0 or 5)` is `5`,
// `print("" or "d")` is the text `d`, and `print([1] and [2])` is `[2]`. Both backends printed `1` for all
// four — exit 0, digits wrong, no diagnostic, on an operator every Python program uses — because the
// lowering composed two predicates and zero-extended the verdict, and every truthiness table since asked
// whether the *test* passed rather than what the expression *is*.
//
// A condition still wants only a verdict, and `truth(a and b)` is `truth(a) and truth(b)`: that question is
// answered by two predicates composing, in `logicCondition`, where the operands never have to agree on
// anything. This file answers the other question — *which value* — and that one needs both operands to be
// able to live in a word the module can write. It is ADR 0262's rule for a ternary's arms read from the
// other direction: an int and a float do not share a word, and which one the answer is stays a run-time
// fact. So the door has three roads:
//
//   - the test is a value the source wrote → the operand the test chooses *is* the answer, the other is not
//     in the program at all, and it is rendered and stored exactly as that operand would be (ADR 0261's
//     rule for a constant ternary test, asked of `constantLogicArm`);
//   - both operands answer in the same word → one `select` picks between them, in the i32 door
//     (`logicValue`) or in the double door (`logicDouble`, the door `floatValue` opens);
//   - the operands do not agree on a word → the answer has no word, and the shape is refused in words that
//     name the missing tag (L11.1) rather than answered by the verdict that stood here before. The print
//     door is the exception, because the module already has a renderer that takes a value *and* its kind:
//     `logicPrintPair` builds the (payload, tag) pair per operand, selects the pair, and hands it to
//     `rt_print_mixed_value` — which is why `print(x or "d")` prints a word and not a `1`.
//
// What the two operators *are* — and what both engines used to fail at — is that the test decides whether the
// other operand exists at all. `x and boom()` does not call `boom` when x is false, `y or (1 // 0)` does not
// divide when y is true, and `if xs and xs[0] > 0:` does not subscript an empty list. Both engines used to
// evaluate both operands and choose afterwards — which is what a `select` is, and all the roads below used
// one — so the skipped operand's print ran, its division raised, and its call ran twice on the compiled leg
// because its truth and its value each evaluated it. Every road now emits the three-block shape of
// `logicSkeleton`: the left operand once, a branch on its truth, and the right operand's instructions on the
// one path that can reach them (roadmap Gap R.149, ADR 0275; the row's ADR 0269 half is the *operand* being
// the answer, which is what the roads below still decide).
//
// A test the source wrote stays the trivial case of the same rule: the operand the test chooses is the answer,
// the other one is not in the program, and nothing is emitted for it at all (`constantLogicArm`).

const (
	logicKindInt   = "int"
	logicKindFloat = "float"
	logicKindBool  = "bool"
	logicKindText  = "text"
	logicKindNone  = "none"
	logicKindList  = "list"
	logicKindDict  = "dict"
	logicKindSet   = "set"
)

// logicTagWord is the tag rt_print_mixed_value reads for a kind this pass can state. The numbers are the
// module's own vocabulary (value.go's ValueTag): int 0, float 1, bool 2, None 3, text 4, list 5, dict 6,
// set 7 — so a pair built here and printed by the runtime cannot disagree with a pair a container slot
// carries (ADR 0187, ADR 0232).
func logicTagWord(kind string) string {
	switch kind {
	case logicKindFloat:
		return "1"
	case logicKindBool:
		return "2"
	case logicKindNone:
		return "3"
	case logicKindText:
		return "4"
	case logicKindList:
		return "5"
	case logicKindDict:
		return "6"
	case logicKindSet:
		return "7"
	}
	return "0"
}

// logicOperandKind names the word an operand's answer lives in, and says false for the operands whose kind
// this pass cannot state — a slot the object describes, a container the run time built, a call whose return
// kind the module never settled. Those are the operands whose answer would need the tag to travel with it,
// which is the word L11.1 still owes.
func (g *irGen) logicOperandKind(e Expr) (string, bool) {
	if e == nil {
		return "", false
	}
	if g.isNoneExpr(e) {
		return logicKindNone, true
	}
	if _, ok := g.stringVal(e); ok {
		return logicKindText, true
	}
	if isStringExpr(e) || g.printsAsInternedStr(e) || g.exprIsString(e) {
		return logicKindText, true
	}
	if nm, ok := e.(*Name); ok {
		if _, isText := g.strVals[nm.Value]; isText {
			return logicKindText, true
		}
	}
	if k, ok := g.staticContainerKind(e); ok {
		return k, true
	}
	if g.isFloat(e) {
		return logicKindFloat, true
	}
	// A slot the object describes is not this pass's to name: its payload could be a text, a float or a
	// nested container, and only the tag says which. The print door asks for the pair; the value doors
	// decline. An element read the literal *does* describe is the exception, and it is the ordinary
	// numeric road's exception too — same promise, same answer.
	if ix, ok := e.(*Index); ok {
		if _, mixed := g.mixedIndexRead(ix); mixed {
			return "", false
		}
		if _, mixed := g.mixedDictIndexRead(ix); mixed {
			return "", false
		}
		if _, _, okRead, err := g.taggedContainerRead(nil, ix); err == nil && okRead {
			return "", false
		}
	}
	if nm, ok := e.(*Name); ok && g.taggedVars[nm.Value] {
		return "", false
	}
	if g.printsAsBool(e) {
		return logicKindBool, true
	}
	if call, ok := e.(*Call); ok {
		// sorted(...), list(...), a comprehension: a handle the printers render, not a number. Nothing
		// here can say what the container holds, which is exactly why the word is not this pass's.
		if g.callBuildsContainer(call) {
			return "", false
		}
	}
	// A nested `and`/`or` is an operand whose kind is the kind of whichever answer it chooses: one operand
	// when the source wrote the test, and when it did not, an answer both sides agree on. The print door
	// needs this to lower `"a" and "b" and "c"` as the pair of pairs that it is.
	if n, isLogic := e.(*BinOp); isLogic && (n.Op == "and" || n.Op == "or") {
		if chosen, decided := constantLogicArm(n); decided {
			return g.logicOperandKind(chosen)
		}
		lk, lok := g.logicOperandKind(n.L)
		rk, rok := g.logicOperandKind(n.R)
		if !lok || !rok {
			return "", false
		}
		if lk == rk {
			return lk, true
		}
		if logicIsNumberWord(lk) && logicIsNumberWord(rk) {
			return logicKindInt, true
		}
		return "", false
	}

	switch e.(type) {
	case *IntLit, *BoolLit:
		return logicKindInt, true
	case *ListLit, *SetLit, *DictLit:
		return "", false
	}
	if g.isContainerExpr(e) {
		return "", false
	}
	// After that list the default has to be *earned*: an operand this pass cannot name is an operand it
	// cannot put in a word, and the shape whose answer is that operand is refused rather than read as an
	// int. `print(x or math.PI)` answering `3` is what an unearned default buys — the attribute is a
	// double the module never describes, and every instruction written after such a guess is a guess too
	// (ADR 0166, and ADR 0266's rule for the unary minus read for these two operators).
	switch n := e.(type) {
	case *Name:
		if g.mixedDicts[n.Value] || g.mixedSets[n.Value] || g.mixedLists[n.Value] || g.runtimeDicts[n.Value] || g.runtimeSets[n.Value] || g.listVars[n.Value] {
			return "", false
		}
		if g.floatVars[n.Value] {
			return logicKindFloat, true
		}
		return logicKindInt, true
	case *Index:
		return logicKindInt, true
	case *UnOp:
		if n.Op != "not" {
			return "", false
		}
		return logicKindBool, true
	case *Call:
		if nm, isName := n.Fn.(*Name); isName {
			if _, isClass := g.classInfos[nm.Value]; isClass {
				// C() is an instance, not a number: the run time knows and the module does not.
				return "", false
			}
		}
		return logicKindInt, true
	case *BinOp:
		// An arithmetic expression is a number when both of its operands are, which is the same question
		// this door asks of its own two — asked once, in one place, for both (`/` is excluded: it is a
		// double whatever comes in, and `isFloat` already says so above).
		switch n.Op {
		case "+", "-", "*", "//", "%", "**":
			lk, lok := g.logicOperandKind(n.L)
			rk, rok := g.logicOperandKind(n.R)
			if lok && rok && logicIsNumberWord(lk) && logicIsNumberWord(rk) {
				return logicKindInt, true
			}
			return "", false
		}
		return "", false
	}
	return "", false
}

// callBuildsContainer is the small refusal list for the builtins and constructors whose answer is a heap
// container rather than a value the numeric road can read.
func (g *irGen) callBuildsContainer(c *Call) bool {
	nm, ok := c.Fn.(*Name)
	if !ok || g.builtinShadowed(nm.Value) {
		return false
	}
	switch nm.Value {
	case "list", "dict", "set", "tuple", "sorted", "reversed", "values", "keys", "items", "enumerate", "zip":
		return true
	}
	if g.listOperands[nm.Value] || g.genFuncs[nm.Value] {
		return true
	}
	return false
}

// logicIsNumberWord reports the kinds that live in the module's one integer word: a verdict is the 0/1 the
// comparison produced, an int is a number, and ADR 0259 put them in one family — `True + 1` is 2 because
// the payload answers, and a `select` between the two needs no tag beside it to be legal IR.
func logicIsNumberWord(kind string) bool {
	return kind == logicKindInt || kind == logicKindBool
}

// logicAnswerIsFloat is the and/or counterpart of the ternary's kind question (ADR 0262): what the operator
// answers with is what the operand it chooses answers with. When the test is a value the source wrote, that
// operand decides alone; otherwise both have to agree, because a print formatter or an fadd cannot carry
// both families in one word.
func (g *irGen) logicAnswerIsFloat(n *BinOp) bool {
	if chosen, ok := constantLogicArm(n); ok {
		return g.isFloat(chosen)
	}
	return g.isFloat(n.L) && g.isFloat(n.R)
}

// --- the short-circuit skeleton (roadmap Gap R.149, ADR 0275) ----------------
//
// `a and b` tests a and only then looks at b; `a or b` tests a and only then looks at b. Choosing with a
// `select` cannot honour that — an instruction has no way to not run — so the answer is three blocks:
//
//	<where the expression starts>  …the left operand, once…   %c = <its truth>
//	                               br i1 %c, label %logic.rhs, label %logic.lhs
//	logic.rhs                  …the right operand, emitted only here…
//	                               br label %logic.rhsfwd
//	logic.lhs                      br label %logic.merge
//	logic.rhsfwd                   br label %logic.merge
//	logic.merge              %ans = phi <ty> [ %l, %logic.lhs ], [ %r, %logic.rhsfwd ]
//
// The two one-instruction forwarding blocks are not decoration. A phi's incoming block has to be an actual
// predecessor of the merge, and this generator tracks no current-block state, so it cannot name the block it
// is writing into; naming a block it emitted itself is enough, because the incoming *value* only has to
// dominate that block — which the left operand's register does, being two instructions earlier on that very
// path. simplifycfg folds both the same day the module is built.
//
// Everything is built into a scratch builder and committed to `b` only when both arms answered: a door that
// changed its mind halfway through would leave the caller's block holding instructions after a terminator,
// and `llc` would report the compiler's mistake as the program's (ADR 0166; the same reason
// `mixedTaggedCompare` buffers, and the same class of bug that made the compiled leg evaluate a call twice).
//
// `right` is asked inside the rhs block; returning ok=false declines the whole shape (nothing is committed),
// returning an error fails the module. `left`/`types` are the arm already emitted — one register per word,
// which for the print door is the pair: payload and tag merged together, the only way a chosen operand that
// *is* a pair reaches the module's one tag-reading printer.
func (g *irGen) logicSkeleton(b *strings.Builder, op, cond string, left []string, types []string, right func(*strings.Builder) ([]string, bool, error)) ([]string, bool, error) {
	if cond == "" || len(left) != len(types) || (op != "and" && op != "or") {
		return nil, false, nil
	}
	lhsArm := g.newLabel("logic.lhs")
	rhsBlock := g.newLabel("logic.rhs")
	rhsArm := g.newLabel("logic.rhsfwd")
	merge := g.newLabel("logic.merge")
	var body strings.Builder
	// The test is what decides whether the right operand is in the program at all. `and` continues when the
	// left operand is true, `or` when it is false; the other path jumps straight to the merge.
	if op == "and" {
		fmt.Fprintf(&body, "  br i1 %s, label %%%s, label %%%s\n", cond, rhsBlock, lhsArm)
	} else {
		fmt.Fprintf(&body, "  br i1 %s, label %%%s, label %%%s\n", cond, lhsArm, rhsBlock)
	}
	fmt.Fprintf(&body, "%s:\n", rhsBlock)
	rregs, ok, err := right(&body)
	if err != nil {
		return nil, false, err
	}
	if !ok || len(rregs) != len(types) {
		return nil, false, nil
	}
	for _, r := range rregs {
		if r == "" {
			return nil, false, nil
		}
	}
	fmt.Fprintf(&body, "  br label %%%s\n", rhsArm)
	fmt.Fprintf(&body, "%s:\n  br label %%%s\n", lhsArm, merge)
	fmt.Fprintf(&body, "%s:\n  br label %%%s\n", rhsArm, merge)
	fmt.Fprintf(&body, "%s:\n", merge)
	out := make([]string, len(types))
	for i, ty := range types {
		t := g.newTmp()
		fmt.Fprintf(&body, "  %s = phi %s [ %s, %%%s ], [ %s, %%%s ]\n", t, ty, left[i], lhsArm, rregs[i], rhsArm)
		out[i] = t
	}
	b.WriteString(body.String())
	return out, true, nil
}

// pairTruth is the truth of a (payload, tag) pair, asked of the runtime: which arm answers — a number in
// either family, an empty text, an empty container, None, an object — is the object's fact, and the branch
// that decides whether the second operand exists has to ask the same table the printer and the comparison do.
func (g *irGen) pairTruth(b *strings.Builder, payload, tag string) string {
	g.heapUsed = true
	g.arithUsed = true
	r := g.newTmp()
	fmt.Fprintf(b, "  %s = call i32 @rt_pair_truth(i32 %s, i32 %s)\n", r, payload, tag)
	return g.asI1(b, r)
}

// numericTruthOf is the truth of an operand the value door has already evaluated, read from the register the
// evaluation left behind. It is the reason the left operand is now evaluated once: the old road asked
// truthyValue for the test and value for the answer, which ran `boom() and 2`'s call twice.
func (g *irGen) numericTruthOf(b *strings.Builder, kind, reg string) string {
	if kind == logicKindNone {
		// None is false and has no word to test; the value is still emitted, because an operand the program
		// wrote runs for its effects even when the test cannot choose it (print(f() or None) owes f's output).
		return "false"
	}
	return g.asI1(b, reg)
}

// logicCondition is the condition door: `if a and b:` and `while a or b:` ask only whether the answer is
// true, and the truth of a chosen operand is the composition of the operands' truths — so the condition
// never needs the word the answer would travel in, and no pair, tag or agreement is asked of the operands.
// It is also the door that used to evaluate the operand a condition never reaches: `if x and boom():` called
// `boom` with x bound to 0, and `if y or boom():` called it with y true, on both engines (Gap R.149).
func (g *irGen) logicCondition(b *strings.Builder, n *BinOp) (string, error) {
	var scratch strings.Builder
	l, err := g.truthyValue(&scratch, n.L)
	if err != nil {
		return "", err
	}
	// The answer the test settles without asking the right operand: `and` with a false left is false, `or`
	// with a true one is true.
	settled := "false"
	if n.Op == "or" {
		settled = "true"
	}
	out, ok, serr := g.logicSkeleton(&scratch, n.Op, l, []string{settled}, []string{"i1"}, func(bb *strings.Builder) ([]string, bool, error) {
		r, rerr := g.truthOperandErr(bb, n.R)
		if rerr != nil {
			return nil, false, rerr
		}
		if r == "" {
			return nil, false, nil
		}
		return []string{r}, true, nil
	})
	if serr != nil {
		return "", serr
	}
	if !ok {
		return "", fmt.Errorf("`%s %s %s` cannot lower the operand its test reaches", exprSurface(n.L), n.Op, exprSurface(n.R))
	}
	b.WriteString(scratch.String())
	return g.markI1(out[0]), nil
}

// logicValue lowers `a and b` / `a or b` where the caller can only store an i32.
func (g *irGen) logicValue(b *strings.Builder, n *BinOp) (string, error) {
	// A test the source wrote is not a run-time question: the operand the test chooses is the answer and
	// the other one is not in the program. Nothing is lost by not emitting it — the un-chosen operand is a
	// literal (that is what makes the test decidable) and a literal has no effects to keep.
	if chosen, ok := constantLogicArm(n); ok {
		return g.value(b, chosen)
	}
	lk, lok := g.logicOperandKind(n.L)
	rk, rok := g.logicOperandKind(n.R)
	if !lok || !rok {
		return "", logicWordErr(n)
	}
	// One word for both operands is still what the merge needs — a phi has one type — and a verdict and an int
	// already share it: ADR 0259's numeric family answers `True + 1` with 2 because the payload speaks, and the
	// same payload is what the merge carries. What is refused below is an answer that could belong to a
	// different family — a double, a text, a container, None, or a kind this pass cannot name at all.
	if lk != rk && !(logicIsNumberWord(lk) && logicIsNumberWord(rk)) {
		return "", logicWordErr(n)
	}
	if lk == logicKindFloat || rk == logicKindFloat {
		// The answer is a double and this caller stores an i32: converting it back would truncate, which
		// is the number-shaped wrong answer ADR 0166 counts as ours. A caller that can take a double asks
		// `isFloat` first and comes to logicDouble.
		return "", logicWordErr(n)
	}
	if lk == logicKindList || lk == logicKindDict || lk == logicKindSet ||
		rk == logicKindList || rk == logicKindDict || rk == logicKindSet {
		// Two containers, one answer, and the word is a handle: only the tag says which object it is, and
		// a handle reaching an i32 slot is the shape runtime_ir_test.go fails the module on.
		return "", logicWordErr(n)
	}
	if lk == logicKindText || rk == logicKindText {
		// A text on either side, and the answer is an index into @str_tab — and an index is not a number the
		// numeric road may carry. The print door, the container element tag and the f-string field each
		// render a text by asking the *expression* what it is, and a selected register answers none of them:
		// `[x or "b"]` printed the index (ADR 0229's one-predicate rule, read the other way). The word that
		// carries the textness is the tag, and printing the number underneath is what ADR 0166 counts as ours.
		return "", logicWordErr(n)
	}
	// The left operand is evaluated once, and the test is read from the word that evaluation left behind:
	// asking truthyValue *and* value used to run a call on the left of the operator twice (Gap R.149).
	var scratch strings.Builder
	l, err := g.value(&scratch, n.L)
	if err != nil {
		return "", err
	}
	if l == "" {
		return "", logicWordErr(n)
	}
	cond := g.numericTruthOf(&scratch, lk, l)
	out, ok, serr := g.logicSkeleton(&scratch, n.Op, cond, []string{l}, []string{"i32"}, func(bb *strings.Builder) ([]string, bool, error) {
		r, rerr := g.value(bb, n.R)
		if rerr != nil {
			return nil, false, rerr
		}
		if r == "" {
			return nil, false, nil
		}
		return []string{r}, true, nil
	})
	if serr != nil {
		return "", serr
	}
	if !ok {
		return "", logicWordErr(n)
	}
	b.WriteString(scratch.String())
	return out[0], nil
}

// logicDouble is the double door: the answer is a double, so the merge is a `phi double` and each operand is
// lowered through floatValue first — converting an int operand to the double word, which conversion *is* the
// reference's own promotion inside `(1 and 2.5) * 2` (ADR 0262's rule for a ternary's arms, read for the two
// operators that hand back an operand).
func (g *irGen) logicDouble(b *strings.Builder, n *BinOp) string {
	if chosen, ok := constantLogicArm(n); ok {
		return g.floatValue(b, chosen)
	}
	lk, lok := g.logicOperandKind(n.L)
	rk, rok := g.logicOperandKind(n.R)
	if !lok || !rok {
		return ""
	}
	if lk != logicKindFloat && rk != logicKindFloat {
		return ""
	}
	for _, k := range []string{lk, rk} {
		switch k {
		case logicKindFloat, logicKindInt, logicKindBool:
		default:
			// A text, a container or None on one side: the answer could be either family, and the family is
			// the run time's fact, not this pass's.
			return ""
		}
	}
	cond := g.newTmp()
	var scratch strings.Builder
	l := g.floatValue(&scratch, n.L)
	if l == "" {
		return ""
	}
	// The same truth the float condition door uses — the double the operand answered with, tested against
	// zero — but read from the value the road already computed rather than from a second evaluation.
	fmt.Fprintf(&scratch, "  %s = fcmp one double %s, 0.0\n", cond, l)
	g.markI1(cond)
	out, ok, serr := g.logicSkeleton(&scratch, n.Op, cond, []string{l}, []string{"double"}, func(bb *strings.Builder) ([]string, bool, error) {
		r := g.floatValue(bb, n.R)
		if r == "" {
			return nil, false, nil
		}
		return []string{r}, true, nil
	})
	if serr != nil || !ok {
		return ""
	}
	b.WriteString(scratch.String())
	return out[0]
}

// logicBudget counts the heap objects an answer builds. The module has no way to keep a handle alive in a
// register across another allocation (ADR 0181's rooting rule), so the whole expression — however deep the
// `and`/`or` nest — may build at most one: a shape that would box two floats or build two containers is
// declined rather than printed from a handle the collector may have taken.
type logicBudget struct {
	builds int
}

// logicForm is one operand as the printer needs it: the payload word, the tag word, and whether the payload
// is a float (which is what makes the module carry the double formatter).
type logicForm struct {
	payload string
	tag     string
	floaty  bool
}

// logicPrintPair builds the (payload, tag) pair the answer travels in for the one printer in the module that
// can read a tag. handled=false is the door's honest "I cannot state both kinds", and the caller refuses
// rather than print the verdict the operator does not return.
//
// The whole expression is one call to logicFormOf, because that is the door that knows how to answer a pair
// for any operand — including an `and`/`or` nested inside it — and the pair it answers is built the same way
// the answer itself is: the left operand's pair, the test read off it with @rt_pair_truth, and the right
// operand's pair built only on the path that reaches it (Gap R.149: `print(boom() or "d")` used to evaluate
// the left operand twice and the right one even when the test had already chosen it).
func (g *irGen) logicPrintPair(b *strings.Builder, n *BinOp) (string, string, bool, error) {
	bud := &logicBudget{}
	var scratch strings.Builder
	f, err := g.logicFormOf(&scratch, n, bud)
	if err != nil || f.payload == "" {
		return "", "", false, err
	}
	if bud.builds > 1 {
		// Both arms build: the second allocation can collect the first handle and nothing here roots it
		// (ADR 0181). A test the source wrote never reaches this — the caller rewrites it to the chosen
		// operand, which is one object and not two.
		return "", "", false, nil
	}
	if f.floaty {
		g.floatFmtUsed = true
	}
	g.heapUsed = true
	b.WriteString(scratch.String())
	return f.payload, f.tag, true, nil
}

// logicFormOf answers how one operand of `and`/`or` is rendered, as a (payload, tag) pair. The
// pair-providers come first — an `and`/`or` of its own, a name the arithmetic door bound, a tagged loop
// variable, a slot the object describes — because their tag is already a register the run time wrote, and a
// phi over two tag registers is exactly as legal as one over two constants. An empty payload means "I cannot
// state this operand's kind", which is the caller's signal to refuse rather than guess.
func (g *irGen) logicFormOf(b *strings.Builder, e Expr, bud *logicBudget) (logicForm, error) {
	// A nested `and`/`or` is an operand like any other, and its answer is a pair too: the left operand's pair,
	// the test read off that pair, and the right operand's pair built inside the block only the taken path
	// reaches — which is what lets `print("a" and "b" and "c")` render the text the two tests choose without
	// either operand running twice.
	if n, ok := e.(*BinOp); ok && (n.Op == "and" || n.Op == "or") {
		if chosen, decided := constantLogicArm(n); decided {
			return g.logicFormOf(b, chosen, bud)
		}
		var scratch strings.Builder
		lf, err := g.logicFormOf(&scratch, n.L, bud)
		if err != nil || lf.payload == "" {
			return logicForm{}, err
		}
		cond := g.pairTruth(&scratch, lf.payload, lf.tag)
		var rightFloaty bool
		out, ok, serr := g.logicSkeleton(&scratch, n.Op, cond, []string{lf.payload, lf.tag}, []string{"i32", "i32"}, func(bb *strings.Builder) ([]string, bool, error) {
			rf, rerr := g.logicFormOf(bb, n.R, bud)
			if rerr != nil {
				return nil, false, rerr
			}
			if rf.payload == "" || bud.builds > 1 {
				return nil, false, nil
			}
			rightFloaty = rf.floaty
			return []string{rf.payload, rf.tag}, true, nil
		})
		if serr != nil {
			return logicForm{}, serr
		}
		if !ok {
			return logicForm{}, nil
		}
		if lf.floaty || rightFloaty {
			g.floatFmtUsed = true
		}
		b.WriteString(scratch.String())
		return logicForm{payload: out[0], tag: out[1], floaty: lf.floaty || rightFloaty}, nil
	}
	if nm, ok := e.(*Name); ok {
		if g.numericPairVar(nm.Value) {
			p, t := g.numericPairRegs(b, nm.Value)
			return logicForm{payload: p, tag: t}, nil
		}
		if g.taggedVars[nm.Value] {
			p := g.newTmp()
			t := g.newTmp()
			fmt.Fprintf(b, "  %s = load i32, i32* %%%s\n", p, "_"+nm.Value)
			fmt.Fprintf(b, "  %s = load i32, i32* %%%s_tag\n", t, "_"+nm.Value)
			return logicForm{payload: p, tag: t}, nil
		}
	}
	if ix, ok := e.(*Index); ok {
		if listName, mixed := g.mixedIndexRead(ix); mixed {
			v, t, err := g.mixedElemPair(b, listName, ix.Idx, ix.Span())
			if err != nil {
				return logicForm{}, err
			}
			return logicForm{payload: v, tag: t}, nil
		}
		if dictName, mixed := g.mixedDictIndexRead(ix); mixed {
			v, t, err := g.mixedDictPair(b, dictName, ix.Idx, ix.Span())
			if err != nil {
				return logicForm{}, err
			}
			return logicForm{payload: v, tag: t}, nil
		}
		if v, t, okRead, err := g.taggedContainerRead(b, ix); err != nil {
			return logicForm{}, err
		} else if okRead {
			g.floatFmtUsed = true
			return logicForm{payload: v, tag: t}, nil
		}
	}
	k, ok := g.logicOperandKind(e)
	if !ok {
		return logicForm{}, nil
	}
	switch k {
	case logicKindNone:
		// None is the singleton, but the operand still runs: `print(f() or None)` owes f()'s output.
		if _, isLit := e.(*NoneLit); !isLit {
			if _, err := g.value(b, e); err != nil {
				return logicForm{}, err
			}
		}
		return logicForm{payload: "0", tag: logicTagWord(k)}, nil
	case logicKindFloat:
		d := g.floatValue(b, e)
		if d == "" {
			return logicForm{}, nil
		}
		g.heapUsed = true
		bud.builds++
		h := g.newTmp()
		fmt.Fprintf(b, "  %s = call i32 @rt_float_new(double %s)\n", h, d)
		return logicForm{payload: h, tag: logicTagWord(k), floaty: true}, nil
	case logicKindList, logicKindDict, logicKindSet:
		h, err := g.containerOperand(b, e)
		if err != nil {
			return logicForm{}, err
		}
		g.heapUsed = true
		// A container the program already built is a load of its slot or of its slot's slot — no
		// allocation, so it is safe beside an arm that does allocate. Only a container the answer has to
		// build is a handle the collector could take before the printer reads it (ADR 0181).
		switch e.(type) {
		case *Name, *Index:
		default:
			bud.builds++
		}
		return logicForm{payload: h, tag: logicTagWord(k)}, nil
	}
	v, err := g.value(b, e)
	if err != nil {
		return logicForm{}, err
	}
	if v == "" {
		return logicForm{}, nil
	}
	return logicForm{payload: v, tag: logicTagWord(k)}, nil
}

// logicWordErr is what the door says about the shape it will not answer: the two operands do not live in
// the same word, and which one the answer is only becomes known when the test runs. It names both operands
// and the missing word, because a refusal that says `unsupported expression` teaches nobody what to change.
func logicWordErr(n *BinOp) error {
	return fmt.Errorf("`%s %s %s` chooses between two values whose kinds this pass cannot state in one word: the answer is one operand or the other, and which one is a run-time fact. Printing the verdict the operator is not would be the wrong answer this backend refuses to ship (ADR 0166), and choosing the value needs the value word that carries its own tag — roadmap L11.1's tagged value word, Gap R.147. Write both operands in the same shape, test with `if`/`else` and bind the value there, or print it (the print door renders a chosen operand by its kind)", exprSurface(n.L), n.Op, exprSurface(n.R))
}

// logicFoldConst answers the fold paths: what operand an `and`/`or` of two values the source wrote hands
// back. Before this the folders answered the verdict, so `print(len("" or "abc"))` and every index, repeat
// count and constant argument built from `and`/`or` was computed from a 1 the operator never returns.
func logicFoldConst(lv int64, op string, rv int64) int64 {
	lTrue := lv != 0
	if op == "and" {
		if lTrue {
			return rv
		}
		return lv
	}
	if lTrue {
		return lv
	}
	return rv
}
