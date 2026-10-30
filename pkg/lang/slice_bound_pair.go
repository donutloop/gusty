package lang

import (
	"fmt"
	"strings"
)

// A slice bound whose kind only the run time can name.
//
// **ADR 0336**'s gate asks a bound what it IS, and it can only answer where the SOURCE wrote the kind:
// `xs["a":2]`, `xs[1.5:2]`, `xs[i:2]` over `i = "a"` — every one of those is CPython's
// `TypeError: slice indices must be integers or None or have an __index__ method`, and the gate raises it.
// Where the kind is not in the source the gate stays silent, which is its own correct answer for a name the
// records describe as an integer and the whole defect for a bound whose kind the compiler never held:
//
//	xs = [1, 2]
//	ys = ["a"]
//	print(xs[ys[0]:2])   # the reference raises;  this backend answered [1, 2]  (exit 0)
//
//	def g():
//	    return "a"
//	print(xs[g():2])     # the reference raises;  this backend answered [1, 2]  (exit 0)
//
// A slot read's kind lives in the object's tag, a call's answer in the word the callee stored beside its
// `ret`, and a float that reaches a parameter as a `double` in the promotion's own record — and each of
// those three words was lowered through the generic expression door and handed to `rt_slice` as a position.
// A text's `@str_tab` index counted as slot 0; a float box handle counted as slot 1. The copy the program
// printed was the copy those invented positions selected, at the exit code of a success (**ADR 0166**'s
// worst class), and the float spelling reached `rt_slice`'s `i32` argument as a `double` and had `llc`
// reject the module (**exit 2**, the code the contract reserves for this compiler's own bug).
//
// So the door asks the second half of the same question, where the kind actually is:
//
//   - `sliceBoundPair` asks the pair roads — the ones print, the comparison, the binding and the membership
//     needle already ask (**ADR 0247**, **ADR 0267**, **ADR 0474**, **ADR 0368**) — for the bound's
//     (payload, tag) words. It is asked in that file's order, and it declines anything whose kind the
//     compiler already names, so a bound that has always answered keeps answering on the road it has always
//     taken (**ADR 0273**: a door that opens may not speak over the road that named what it could not carry).
//   - `rt_bound_is_position` (codegen_ir_slice.go) reads that tag and answers the reference's three verdicts
//     — a position, an absence (`None`), or the `TypeError`. One helper holds the legal-tag set for all three
//     bounds of both slice roads, because a legality table copied per road is how the roads stop agreeing
//     (**ADR 0232**).
//
// The verdict is not only used to raise: a tag that says `None` is the absence of a bound, which is what the
// reference means by `xs[None:2]`, so the presence flag becomes a run-time word (`rt_slice` takes it as an
// i32 argument already) and `ys = [None]` / `xs[0:ys[0]]` answers the whole list instead of the empty one.
// A step whose tag says `None` becomes the stride the program left out — 1 — which is also what keeps the
// **ADR 0337** zero-step question from accusing a bound that never was a step.

// sliceBoundPair asks the pair roads for a slice bound's (payload, tag) words. It answers only for a bound
// whose kind is a run-time fact; for a literal, a name the records settled, or anything else the ordinary
// road already reads, it declines and the caller lowers the bound exactly as it always has.
//
// The order is the render door's (render.go), and for the same reason: a name the arithmetic door bound is
// asked before the container probe, whose number-shaped read would refuse before this arm could speak.
func (g *irGen) sliceBoundPair(b *strings.Builder, e Expr) (payload, tag string, ok bool, err error) {
	if e == nil || g.sliceBoundIsAbsent(e) {
		return "", "", false, nil
	}
	if nm, isName := e.(*Name); isName {
		// `n = xs[0][0] * 2` binds a name whose family the objects chose (**ADR 0267**), and a pair-bound
		// PARAMETER arrives the same way (**ADR 0273**) — the two words live in the name's own allocas.
		if g.numericPairVar(nm.Value) {
			p, t := g.numericPairRegs(b, nm.Value, nm.Span())
			return p, t, true, nil
		}
		if p, t, okTag := g.taggedLoopVarRead(b, nm.Value, nm.Span()); okTag {
			return p, t, true, nil
		}
	}
	if p, t, okCall, cerr := g.answerCallPair(b, e); cerr != nil {
		return "", "", false, cerr
	} else if okCall {
		return p, t, true, nil
	}
	if p, t, okSlot := g.runtimeSlotPairDeep(b, e); okSlot {
		return p, t, true, nil
	}
	return "", "", false, nil
}

// guardedSliceBound is the door one bound walks: the pair roads first, and where they decline the caller
// lowers the bound on the ordinary road, unchanged. `handled` is false for every bound whose kind the
// compiler already names — an integer literal, a name the records settled, a bound the program left out —
// which is what keeps `xs[1:k]` over `k = 1` answering the copy it always answered, with no test emitted.
func (g *irGen) guardedSliceBound(b *strings.Builder, bound Expr, sp Span) (payload, verdict string, handled bool, err error) {
	p, t, ok, perr := g.sliceBoundPair(b, bound)
	if perr != nil {
		return "", "", false, perr
	}
	if !ok {
		return "", "", false, nil
	}
	_, v := g.sliceBoundVerdict(b, p, t, sp)
	return p, v, true, nil
}

// sliceBoundVerdict emits the tag question and the raise it carries, and hands back the verdict register:
// 1 for a word that counts as a position, 0 for the absence of one, and — had the run time been able to
// reach the code — -1. Reaching -1 is the program's exception: `except TypeError:` takes it, an uncaught
// one reports the class and the reference's own sentence and exits 3, and the block the guard branched out
// of is reopened, so the module is valid whichever way the branch went (the shape `badSliceBoundGate` and
// `zeroStepGuardAtSlice` use).
func (g *irGen) sliceBoundVerdict(b *strings.Builder, payload, tag string, sp Span) (string, string) {
	verdict := g.newTmp()
	b.WriteString(fmt.Sprintf("  %s = call i32 @rt_bound_is_position(i32 %s)\n", verdict, tag))
	bad := g.newTmp()
	b.WriteString(fmt.Sprintf("  %s = icmp slt i32 %s, 0\n", bad, verdict))
	g.markI1(bad)
	badL, okL := g.newLabel("sbb.bad"), g.newLabel("sbb.ok")
	b.WriteString(fmt.Sprintf("  br i1 %s, label %%%s, label %%%s\n", bad, badL, okL))
	b.WriteString(badL + ":\n")
	g.heapUsed = true
	g.raiseTo(b, exnCode("TypeError"), "TypeError", sliceBoundsMessage(), sp)
	b.WriteString(okL + ":\n")
	return payload, verdict
}

// sliceBoundIsAbsentVerdict asks the verdict back as a condition: the tag said `None`, which for a bound is
// not a value at all — it is the side the program left out.
func (g *irGen) sliceBoundIsAbsentVerdict(b *strings.Builder, verdict string) string {
	isNone := g.newTmp()
	b.WriteString(fmt.Sprintf("  %s = icmp eq i32 %s, 0\n", isNone, verdict))
	return g.markI1(isNone)
}

// sliceBoundPresence turns a verdict into the presence flag `rt_slice` reads: a bound whose tag says `None`
// was not written, and the side takes its default.
func (g *irGen) sliceBoundPresence(b *strings.Builder, verdict string) string {
	has := g.newTmp()
	b.WriteString(fmt.Sprintf("  %s = icmp ne i32 %s, 0\n", has, verdict))
	g.markI1(has)
	flag := g.newTmp()
	b.WriteString(fmt.Sprintf("  %s = zext i1 %s to i32\n", flag, has))
	return flag
}

// sliceBoundWord answers a bound whose side has no presence flag to carry — the step, for which the default
// is a stride of 1, and the text road, whose helper spells absence as a sentinel — by selecting the default
// where the tag says `None` and the payload everywhere else.
func (g *irGen) sliceBoundWord(b *strings.Builder, payload, verdict, absentWord string) string {
	isNone := g.sliceBoundIsAbsentVerdict(b, verdict)
	sel := g.newTmp()
	b.WriteString(fmt.Sprintf("  %s = select i1 %s, i32 %s, i32 %s\n", sel, isNone, absentWord, payload))
	return sel
}
