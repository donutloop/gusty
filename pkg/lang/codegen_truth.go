// Truth: an operand's i1, the and/or connectives, and the i32/i1 boundary the roads meet at.
package lang

import (
	"fmt"
	"strings"
)

// truthyValue emits an i32 0/1 for a condition, using float != 0.0
// for float expressions (fcmp one + zext) instead of truncated ints.
func (g *irGen) truthyValue(b *strings.Builder, e Expr) (string, error) {
	v, err := g.truthOperandErr(b, e)
	if err != nil {
		// The condition used to be replaced by a false branch and the error dropped on the
		// floor. That is the worst failure this backend can make: `print(1 if s[1] == "b" else 0)`
		// could not lower its condition at all, and printed `0` as if the program had asked a
		// question and been answered. A part that cannot be lowered is a compile error, reported
		// by the caller that has the error channel (roadmap Gap R.45, ADR 0225; ADR 0166).
		return "", err
	}
	return v, nil
}

// --- i1 / i32 truthiness normalisation -------------------------------------
//
// Conditions in this language accept any value (`if count:`, `if flag and ready:`,
// `while total:`), and a value in IR is either an i32 (integers, booleans stored
// as 0/1) or an i1 (the result of a comparison or boolean-logic instruction).
// Mixing the two — `icmp ne i32 %cmp, 0` on an i1, or `br i1 %count` on an i32 —
// is a verifier failure, so every consumer of a condition goes through these
// helpers instead of assuming a representation.
// markI1 records that reg holds an i1 and returns reg, so emission sites can wrap
// the register inline.
func (g *irGen) markI1(reg string) string {
	if g.i1Vals == nil {
		g.i1Vals = map[string]bool{}
	}
	g.i1Vals[reg] = true
	return reg
}

// i1Reg reports whether reg was produced by a comparison / boolean-logic
// instruction and is therefore already an i1.
func (g *irGen) i1Reg(reg string) bool { return g.i1Vals[reg] }

// asI1 normalises any scalar value to an i1 predicate usable by br/select/and/or:
// comparison results pass straight through, everything else is tested against zero.
func (g *irGen) asI1(b *strings.Builder, v string) string {
	switch v {
	case "true":
		return "true"
	case "false":
		return "false"
	}
	if g.i1Reg(v) {
		return v
	}
	t := g.newTmp()
	b.WriteString(fmt.Sprintf("  %s = icmp ne i32 %s, 0\n", t, v))
	return g.markI1(t)
}

// asBoolI32 renders a boolean-producing expression the way the record stores
// booleans — an i32 0/1 — so `not x`, `a and b`, `x in xs` can be printed, stored
// in a variable and tested again instead of leaking a bare i1 into an i32 slot.
func (g *irGen) asBoolI32(b *strings.Builder, v string) string {
	p := g.asI1(b, v)
	switch p {
	case "true":
		return "1"
	case "false":
		return "0"
	}
	t := g.newTmp()
	b.WriteString(fmt.Sprintf("  %s = zext i1 %s to i32\n", t, p))
	return t
}

// truthOperand is the condition-side helper: float falsiness goes through fcmp, a
// comparison emits its predicate directly, everything else goes through asI1.
func (g *irGen) truthOperand(b *strings.Builder, e Expr) string {
	v, err := g.truthOperandErr(b, e)
	if err != nil {
		// Mirrors valueText: a condition that cannot be lowered is reported by the
		// enclosing statement path, not here.
		return g.asI1(b, "0")
	}
	return v
}
func (g *irGen) truthOperandErr(b *strings.Builder, e Expr) (string, error) {
	// `if a and b:` and `while a or b:` ask only whether the answer is true, and the truth of the operand
	// the operator chooses is the composition of the two operands' truths. That question never needs the
	// operands to agree on a word, so it is answered here rather than by the value door — which is
	// entitled to refuse the shapes this one can still branch on (roadmap Gap R.147, ADR 0269).
	if n, ok := e.(*BinOp); ok && (n.Op == "and" || n.Op == "or") {
		if chosen, decided := constantLogicArm(n); decided {
			return g.truthyValue(b, chosen)
		}
		return g.logicCondition(b, n)
	}
	// `if x == "a":` where x carries a runtime tag — a loop variable over a container that
	// mixes kinds. An `if` condition is lowered here, not through the expression path, so the
	// tagged comparison gets its hook in both places (ADR 0232). `is`/`is not` join for the one
	// shape the door admits — the void, whose identity is a tag and not a source fact
	// (roadmap Gap R.412, ADR 0411).
	if n, ok := e.(*BinOp); ok && (n.Op == "==" || n.Op == "!=" || n.Op == "is" || n.Op == "is not") {
		if res, handled, err := g.mixedTaggedCompare(b, n); err != nil {
			return "", err
		} else if handled {
			p := g.newTmp()
			fmt.Fprintf(b, "  %s = icmp ne i32 %s, 0\n", p, res)
			return g.markI1(p), nil
		}
	}
	// `if f():` where the callee answers more than one kind. Which arm ran is a run-time fact, so the
	// truth is asked of the tag — the same helper `and` and `or` ask about the operand they test — and not
	// of the payload, which is an @str_tab index for a text and a heap handle for a container: testing
	// those is how `if f(1):` took the else arm for CPython's `a` (roadmap Gap R.219, ADR 0269's door,
	// ADR 0368's second user).
	if i1, answered, terr := g.answerTruthOf(b, e); terr != nil {
		return "", terr
	} else if answered {
		return i1, nil
	}
	// `if build():` asks whether the container the callee built has anything in it. The ordinary road has
	// no word for that object, so the condition is asked again of the name the container door materialised
	// for it — and the road below (`heapContainerLenFn`) already knows how to ask a container name whether
	// it is empty, which is what a condition on a container has always meant (roadmap Gap R.67, ADR 0333).
	if ansName, _, okAns := g.containerAnswerOperandRead(b, e, containerReadShape); okAns {
		return g.truthOperandErr(b, ansName)
	}
	// Strings and containers test their *content*, never their representation: a
	// string is an i8* and a container is a compile-time struct or a heap handle,
	// so `if xs:` on a handle would be true even for [] and testing the string
	// pointer is not even valid IR. Empty is false, non-empty is true.
	if sv, ok := g.stringVal(e); ok {
		return constI1(sv != ""), nil
	}
	if n, ok := g.listArgLen(e); ok {
		return constI1(n != 0), nil
	}
	if fn, ok := g.heapContainerLenFn(e); ok {
		v, err := g.value(b, e)
		if err != nil {
			return "", err
		}
		n := g.newTmp()
		fmt.Fprintf(b, "  %s = call i32 @%s(i32 %s)\n", n, fn, v)
		p := g.newTmp()
		fmt.Fprintf(b, "  %s = icmp ne i32 %s, 0\n", p, n)
		return g.markI1(p), nil
	}
	if g.isFloat(e) {
		t := g.newTmp()
		fmt.Fprintf(b, "  %s = fcmp one double %s, 0.0\n", t, g.floatValue(b, e))
		return g.markI1(t), nil
	}
	// `if v:` where the value arrived in a PARAMETER the caller filled. Its tag is a register the run
	// time wrote at the call site (ADR 0389) and nothing in the compiler has read it, so the arm below —
	// the lift ADR 0268 built for a name the ARITHMETIC door bound, whose tag can only ever name int,
	// float or bool — widens a text's `@str_tab` index and a container's heap handle as if they were the
	// number and calls `if []:` true. The truth of this pair is the same table `and`/`or` ask about the
	// operand they test, the entry door ask in an `if`, and the unrolled loop ask about its item
	// (roadmap Gap R.361, ADR 0393).
	if bit, asked := g.pairSuppliedNameTruth(b, e); asked {
		return bit, nil
	}
	// A name the arithmetic door bound is a number whose kind the objects decided at the moment the
	// sum ran. `if n:` does not care which of the two families it is — zero is false, anything else is
	// true — but the word the answer lives in does, so the pair is lifted to the one word that holds
	// both and tested there (roadmap L11.1, Gap R.143).
	if d := g.numericPairLift(b, e); d != "" {
		t := g.newTmp()
		fmt.Fprintf(b, "  %s = fcmp one double %s, 0.0\n", t, d)
		return g.markI1(t), nil
	}
	// An ordering whose pair-bound side is a name the arithmetic door bound is the same question with
	// one side still unlifted; it goes through the lift rather than the tag-dispatch door below
	// because this tag cannot say anything but int or float (roadmap Gap R.143).
	if n, ok := e.(*BinOp); ok && cmpI1Op(n.Op) != "" {
		// The same door the value road asks, asked here because an `if`/`while`/ternary condition is lowered
		// on this road and not through `valueBinOp`: an ordering between two kinds that do not order against
		// each other raises the reference's `TypeError` rather than testing the two words the lowering would
		// have handed the `icmp` — `None`'s word is 0 and a text's is an intern index (roadmap Gap R.85, ADR 0382).
		if g.crossKindOrderGate(b, n) {
			return constI1(false), nil
		}
		// An ordering whose pair-bound side is a name the arithmetic door bound is the same question with
		// one side still unlifted; it goes through the lift rather than the tag-dispatch door below
		// because this tag cannot say anything but int or float (roadmap Gap R.143).
		if i1, okPair, perr := g.pairOrder(b, n); perr != nil {
			return "", perr
		} else if okPair {
			return i1, nil
		}
	}
	// An ordering with a slot on one side is asked of the tag **before** anything reads the operands as
	// numbers: which arm this pair is — two numbers, two texts, or CPython's TypeError — is a run-time
	// question, and every path below this one has already committed to one of the answers. It sits above
	// the float test for the same reason: `isFloat` on a slot whose literal holds a float is true, and
	// falling into the fcmp below would test a box handle against zero (roadmap L11.1, Gap R.82).
	if n, ok := e.(*BinOp); ok && cmpI1Op(n.Op) != "" && g.taggedOrderApplies(n) {
		_, i1, ook, oerr := g.emitTaggedOrder(b, n)
		if oerr != nil {
			return "", oerr
		}
		if ook {
			return i1, nil
		}
	}
	// `if a < b and b < 9:` would otherwise pay for a zext and a re-test per
	// comparison; emit the predicate itself and use it directly.
	if n, ok := e.(*BinOp); ok && cmpI1Op(n.Op) != "" && !g.isFloat(n.L) && !g.isFloat(n.R) {
		// An ordering of two containers the compiler BUILT in place is the sequence door's refusal, and the
		// condition road asks it too: without this arm the pair fell through to the generic comparison below,
		// which rendered both sides and emitted `%t1 = icmp slt i32 @.lst1, @.lst2` — the invalid IR this row's
		// own bug was, reported by `llc` at the compiler-bug exit code, while the value door gave the same
		// shape honest words. One predicate, both roads (ADR 0232; roadmap Gap R.33, ADR 0365; the ordering
		// itself is Gap R.314).
		if msg, refuses := g.seqBuiltOrderingRefusal(n); refuses {
			return "", fmt.Errorf("%s", msg)
		}
		// A container comparison in a condition goes through the same value-equality helper the
		// expression path uses; `if [1] == 1:` emitted `icmp eq i32 @.lst1, 1` before, which is
		// both invalid IR and the wrong question (ADR 0189).
		if (n.Op == "==" || n.Op == "!=") && (g.isContainerExpr(n.L) || g.isContainerExpr(n.R)) {
			eq, err := g.containerEquality(b, n)
			if err != nil {
				return "", err
			}
			t := g.newTmp()
			fmt.Fprintf(b, "  %s = icmp ne i32 %s, 0\n", t, eq)
			return g.markI1(t), nil
		}
		// The same door as the value path: an ordering of two texts asks strcmp, because the
		// operands at this point are two @str_tab indices and their order is the order the texts
		// were interned (roadmap Gap R.84).
		if g.textOrderOperands(n) {
			_, i1, terr := g.emitTextOrder(b, n)
			if terr != nil {
				return "", terr
			}
			return i1, nil
		}
		// The same door as the value path, on the side a condition takes: an ordering of a slot whose
		// kind the object carries has to reach the arms that can ask the tag, because the integer path
		// here reads the payload of a float slot as a count (roadmap L11.1, Gap R.88).
		if g.taggedNumberUseApplies(n) && cmpI1Op(n.Op) != "" {
			res := g.floatBinOp(b, n)
			if g.floatUnlowerable != "" {
				return "", g.floatLoweringRefusal(n)
			}
			p := g.newTmp()
			fmt.Fprintf(b, "  %s = icmp ne i32 %s, 0\n", p, res)
			return g.markI1(p), nil
		}
		l, err := g.value(b, n.L)
		if err != nil {
			return "", err
		}
		r, err := g.value(b, n.R)
		if err != nil {
			return "", err
		}
		t := g.newTmp()
		fmt.Fprintf(b, "  %s = %s i32 %s, %s\n", t, cmpI1Op(n.Op), l, r)
		return g.markI1(t), nil
	}
	// A list literal read by a position, in the one position that asks only "is it true": the element's kind
	// lives in the tag the builder wrote beside its payload, and truth is the tag's fact — an empty text, an
	// empty container, None, 0.0 and False are all false, and none of that is readable off the payload alone
	// (`[0.0, 1][0]` is false in the reference and a payload tested against zero calls it true). The pair door
	// that answers print, str/repr and a binding answers the condition as well, through the same table the
	// printer and the `and`/`or` operands ask (roadmap Gap R.218, ADR 0331).
	if nm, isName := e.(*Name); isName {
		if t, asks := g.truthAsksTheLoopItem(nm); asks {
			payload, perr := g.value(b, e)
			if perr != nil {
				return "", perr
			}
			return g.pairTruth(b, payload, fmt.Sprintf("%d", t)), nil
		}
	}
	if ix, isIdx := e.(*Index); isIdx {
		if ll, isLit := ix.Obj.(*ListLit); isLit && !g.listLiteralFoldAnswers(ll, ix.Idx) {
			payload, tag, okPair := g.listLiteralPair(b, ll, ix.Idx, ix.Span())
			if !okPair {
				return "", listLiteralOneWordErr(ll, ix.Idx)
			}
			return g.pairTruth(b, payload, tag), nil
		}
		// A dict entry or runtime slot whose kind lives beside the payload asks the same question print, an
		// f-string field, `str()`/`repr()` and a binding ask. Testing its payload is not a conservative guess:
		// a text entry is an intern index, a float entry a heap handle, `None` is 0, and an empty container is a
		// handle whose length has not been asked. The same `@rt_pair_truth` table decides the condition that
		// `and`/`or` already use for its chosen operand (roadmap Gap R.284, ADR 0388).
		if g.entryReadNeedsTheTag(ix) || g.isPairElem(ix) {
			if payload, tag, okPair, xerr := g.subscriptPairOf(b, ix); xerr != nil {
				return "", xerr
			} else if okPair {
				return g.pairTruth(b, payload, tag), nil
			}
			// The fold answers this read, so the print door's dispatch declined — and a condition may not
			// test what the fold produced. Ask the literal for the pair the fold threw away.
			if payload, tag, okPair, xerr := g.conditionPairOfRead(b, ix); xerr != nil {
				return "", xerr
			} else if okPair {
				return g.pairTruth(b, payload, tag), nil
			}
		}
	}
	// `if f():` where the answer is a TEXT the compiler holds as an `@str_tab` index and no fold can read: a
	// call into a text-returning `def` or method, `str(...)`, `repr(...)`, a text method's answer, a slice of a
	// runtime text. Everything below this arm asks a word that is not this word, and the fallback tests the
	// operand against zero — which for an interned text asks WHICH TABLE ENTRY the intern table handed out, not
	// whether the text has any characters in it. `def f(): return "abc"` / `if f():` therefore took the else arm
	// at exit 0 where the reference runs the body, `while f(i):` never entered its body at all, `not f()` answered
	// True, and `"y" if f() else "n"` chose `n`. The loop item already asks `rt_str_len` for exactly this reason
	// (the arm `rt_pair_truth` takes for the text tag); the condition road had simply never been asked about a
	// text that arrives from a call, because every shape that predated this arm is a literal, a name the binding
	// road recorded, or a text door with a constant on the left (roadmap Gap R.407, ADR 0409).
	if g.truthAsksTheInternedText(e) {
		v, err := g.value(b, e)
		if err != nil {
			return "", err
		}
		n := g.newTmp()
		fmt.Fprintf(b, "  %s = call i32 @rt_str_len(i32 %s)\n", n, v)
		p := g.newTmp()
		fmt.Fprintf(b, "  %s = icmp ne i32 %s, 0\n", p, n)
		return g.markI1(p), nil
	}
	v, err := g.value(b, e)
	if err != nil {
		return "", err
	}
	return g.asI1(b, v), nil
}

// truthAsksTheInternedText is this road's turn at the one text question (ADR 0232's rule, the one the print
// door, the operation door and the sequence door each had to learn): is the operand a text whose value is an
// `@str_tab` index the compiler cannot read as a constant? The shapes that CAN be read are left to the arms
// above — a literal, a name the binding road recorded as a constant text, and the text doors with a constant
// operand all fold to a compile-time truth, which is both cheaper and the answer the fold is obliged to give.
// A container or a float is declined here because those objects own roads above this one: a container asks its
// own length, and a float asks its payload against zero.
func (g *irGen) truthAsksTheInternedText(e Expr) bool {
	switch e.(type) {
	case *StrLit:
		// A literal folds above (`constI1(sv != "")`), and it must: the intern table's index for "" is whatever
		// position it happens to occupy, and a compile-time answer is the fold's obligation, not a runtime call.
		return false
	}
	if nm, ok := e.(*Name); ok {
		// A NAME is the binding road's to answer: a name recorded as holding a CONSTANT text folds above, a name
		// the pair road bound carries its own tag (that arm is above this one), and a loop item asks `rt_str_len`
		// through the loop-item arm. What is left is the name the call road filled — `s = f()` where `f` hands back
		// a text — whose word is an index no fold can read, and testing THAT against zero is this row's bug.
		if _, folded := g.stringVal(e); folded {
			return false
		}
		// `g.caughtVars` is in this test beside `g.internedVars` because ADR 0406's arm binds the name to the
		// INTERNED message (`except ZeroDivisionError as err:` / `if err:`), and the binding road that fills
		// `internedVars` never sees an `except` clause's binding — so without this the name fell to the payload test
		// below and asked which table entry the message got (roadmap Gap R.395's neighbourhood, Gap R.407).
		return (g.internedVars[nm.Value] || g.caughtVars[nm.Value]) && !g.taggedVars[nm.Value]
	}
	if _, ok := g.stringVal(e); ok {
		return false
	}
	if g.isContainerExpr(e) || g.isFloat(e) {
		return false
	}
	return g.exprIsString(e) || g.printsAsInternedStr(e)
}

// truthAsksTheLoopItem answers the condition road's other self-describing read: the loop variable of an
// unrolled comprehension or `for`, whose kind ADR 0386 records once per item in `loopElemTag`. The name
// holds THIS item, and this item's payload is a float box, a container handle or a verdict — words whose
// meeting zero is not the reference's truth. ADR 0386 taught the print, the binding and the container doors
// to read that record; the condition is the road it left out, which is why
// `print([bool(x) for x in [0.0, [0]]])` wrote `[True, True]` at exit 0 where the reference writes `[False, True]`.
//
// Two kinds are deliberately absent. A container *variable* named by the loop (`for xs in lists`) keeps its
// own road, which asks the object its own length. And a name the pair road bound (`taggedVars`) travels with
// its own two registers, which the pair doors below read. A TEXT's item is answered here as well: its
// payload is the interned index, and `rt_str_len` — the arm `rt_pair_truth` takes for tag 4 — reads that
// index, which is the only honest way to ask an interned word whether it has any characters. Testing the
// index against zero is what made `print([x for x in ["", [0]] if x])` print `[”, [0]]` once another text
// was interned ahead of it.
func (g *irGen) truthAsksTheLoopItem(nm *Name) (int32, bool) {
	if g == nil || g.loopElemTag == nil || g.listVars[nm.Value] || g.taggedVars[nm.Value] {
		return 0, false
	}
	t, has := g.loopElemTag[nm.Value]
	if !has {
		return 0, false
	}
	switch t {
	case int32(TagFloat), int32(TagBool), int32(TagNone), int32(TagStr), int32(TagList), int32(TagDict), int32(TagSet):
		return t, true
	}
	return 0, false
}

// constI1 renders a compile-time truth value as an LLVM i1 constant.
func constI1(v bool) string {
	if v {
		return "true"
	}
	return "false"
}

// cmpI1Op maps a comparison operator to its LLVM predicate instruction, or "" when
// the operator is not a comparison.
func cmpI1Op(op string) string {
	switch op {
	case "==", "is":
		return "icmp eq"
	case "!=", "is not":
		return "icmp ne"
	case "<":
		return "icmp slt"
	case "<=":
		return "icmp sle"
	case ">":
		return "icmp sgt"
	case ">=":
		return "icmp sge"
	}
	return ""
}
func (g *irGen) andCond(b *strings.Builder, a, c string) string {
	if a == "true" || a == "1" {
		return c
	}
	if c == "true" || c == "1" {
		return a
	}
	// A pattern that cannot match is a constant, and `and i1 0, x` is not IR — the fold belongs in
	// the combinator, not in every caller (ADR 0235).
	if a == "false" || a == "0" || c == "false" || c == "0" {
		return "false"
	}
	t := g.newTmp()
	b.WriteString(fmt.Sprintf("  %s = and i1 %s, %s\n", t, a, c))
	return t
}
func (g *irGen) orCond(b *strings.Builder, a, c string) string {
	if a == "false" || a == "0" {
		return c
	}
	if c == "false" || c == "0" {
		return a
	}
	if a == "true" || a == "1" || c == "true" || c == "1" {
		return "true"
	}
	t := g.newTmp()
	b.WriteString(fmt.Sprintf("  %s = or i1 %s, %s\n", t, a, c))
	return t
}
