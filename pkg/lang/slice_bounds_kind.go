package lang

import (
	"fmt"
	"strings"
)

// A slice bound that is not a position.
//
// A slice asks a container that counts for a run of its slots, and the reference words the two answers a
// bound can have in one sentence — `slice indices must be integers or None or have an __index__ method` —
// because a bound is either a position or the default for that side. The compiled road had neither question:
// `rt_slice` takes seven i32s (handle, three bounds, three presence flags) and every bound was lowered through
// the generic expression door, whatever it held.
//
//	xs = [1, 2]
//	print(xs["a":"b"])   # TypeError: slice indices must be integers or None or have an __index__ method
//	                     # this backend: [1]        (exit 0 — "a" is @str_tab index 0, "b" index 1)
//	print(xs[1.5:2])     # the reference raises;    this backend: [2]  (exit 0)
//	print(xs[[0]:2])     # the reference raises;    this backend: exit 2 — a @.lstN global inside `icmp slt`
//	s  = "abcd"
//	print(s["a":"b"])    # the reference raises;    this backend: b    (exit 0)
//
// And the one bound the reference excuses, `None`, was read as a position too — the word a `None` lowers to is
// 0, and the presence flag was set as though the program had written a number:
//
//	xs = [1, 2]
//	print(xs[None:None]) # [1, 2] in the reference; this backend: []   (exit 0)
//	print(xs[::None])    # [1, 2] in the reference; this backend: exit 2 (the runtime's own step loop)
//
// `badIndexGateAtSubscript` (roadmap Gap R.222, ADR 0329) already stands at the subscript door with this
// question for an `Index`. A `Slice` is the same door with three handles on it, and it asks the same question
// of each: an integer (or a bool, which is an int) is a position, `None` is the absence of one, and anything
// else the compiler can name the kind of is the reference's `TypeError`.
//
// Where the kind is only a run-time fact the gate stays silent, exactly as R.222's does: a name the records do
// not describe keeps the road it has always gone through rather than being accused of a kind nobody wrote.

// sliceBoundIsAbsent reports whether a bound is the *absence* of a bound spelled as a value. The reference
// writes an omitted bound and a `None` bound identically at the top of a slice — both mean "the default for
// this side" — and the compiler has to read them the same way, because `None` lowers to the word 0 and a
// presence flag set beside that word is a bound of 0: `xs[None:None]` answering `[]` instead of `[1, 2]` is
// the same defect as `xs["a":"b"]` answering `[1]`, seen from the other side of the same missing question.
func (g *irGen) sliceBoundIsAbsent(e Expr) bool {
	if e == nil {
		return true
	}
	switch n := e.(type) {
	case *NoneLit:
		return true
	case *Name:
		// The same notebook the print door asks: `i = None` binds the singleton, and a slice
		// written with it is the slice written without it.
		return g.noneVars[n.Value]
	}
	return false
}

// sliceBoundsMessage is the reference's sentence, quoted rather than paraphrased — one string for a list and
// for a text alike, which is how the reference words it (`typeError: slice indices must be integers or None or
// have an __index__ method` comes out of the slice object itself, before the container is ever asked).
func sliceBoundsMessage() string {
	return "slice indices must be integers or None or have an __index__ method"
}

// nonIntBoundTypeName is `nonIntIndexTypeName` with the two rulings a *bound* needs that an *index* does not:
// `None` is a legal bound (it means "absent"), and an absent bound is not a claim about a kind at all.
// Everything else the index classifier names — a text, a float, a list, a dict, a set, a tuple, or a name the
// records last filled with one of them — is a bound the reference refuses.
//
// The three rulings **Gap R.237** adds are the three kinds that are not facts about the SOURCE: the kind a CALL
// answers, the kind a CALLER hands a parameter, and the kind a CLASS writes into a field. `nonIntIndexTypeName`
// reads what the source spelled and what the records last bound a NAME to, and none of those is either — so
// `xs[g():2]` over `def g(): return "a"` reached `rt_slice` with the text's `@str_tab` index as its high bound and
// answered `[1, 2]` at the exit code of a success, `def at(k): return xs[0:k]` / `print(at(1.5))` answered `1`, the
// copy's heap handle, and the float spellings reached `rt_slice`'s `i32` argument as a `double`, which is `llc`
// refusing the module (**exit 2**, **ADR 0166**). The tables these three ask are the ones the print, container and
// truth roads already decide with — `staticContainerKind`, `strFuncs`, `floatFuncs`, `floatArgKinds`, `strAttrs` —
// and not a fourth list of callables or a per-door copy of any of them (**ADR 0232**).
func (g *irGen) nonIntBoundTypeName(e Expr) (string, bool) {
	if g.sliceBoundIsAbsent(e) {
		return "", false
	}
	typeName, bad := g.nonIntIndexTypeName(e)
	if bad && typeName != "NoneType" {
		return typeName, true
	}
	// The first kind the index classifier cannot see: the kind the CALLERS hand a parameter (floatargs.go, the
	// float twin of the text and verdict censuses). A parameter is the binding that is never in the body, so
	// `def at(k): return xs[0:k]` called with `at(1.5)` had no record to ask and answered `1` — the copy's heap
	// handle as a number — where the reference raises (roadmap **Gap R.237**).
	if nm, isName := e.(*Name); isName && g.floatParamNames[nm.Value] {
		return kindFloat, true
	}
	// The second kind is the one a CLASS writes: `self.k = "x"` stores an `@str_tab` index in the slot, and
	// reading `b.k` back hands that index to `rt_slice` as a position. The census is the one the truth road
	// already asks it from (**Gap R.42**, **ADR 0224**'s slot-is-the-truth, `negation.go`'s arm), so a field
	// the class filled with a text is a text here and nowhere else (`xs[0:b.k]` answered `[]` at exit 0).
	if at, isAttr := e.(*Attr); isAttr {
		if cls := g.receiverClass(at.Obj); cls != "" && g.strAttrs[cls+"."+at.Name.Value] {
			return kindStr, true
		}
	}
	// The third is the kind a call's ANSWER carries. Where one of the tables names it the gate raises below —
	// running the call first, which is the order the reference keeps — and where none does, the bound is a run-
	// time pair and the door in `slice_bound_pair.go` asks its tag instead.
	if tn, isKind := g.sliceBoundCallNames(e); isKind {
		return tn, true
	}
	return "", false
}

// sliceBoundCallNames names the kind a call answers, for the bounds the static gate can settle without
// running anything. It answers "" for a call none of the tables describes — a carried body whose arms
// disagree is the run-time door's, and a builtin the tables do not name stays the road it has always gone
// through rather than being accused of a kind nobody wrote.
func (g *irGen) sliceBoundCallNames(e Expr) (string, bool) {
	c, isCall := e.(*Call)
	if !isCall {
		return "", false
	}
	// A container the answer table registers is named by the one predicate the slice base, the comparison,
	// the membership test and the loop already decide with (ADR 0466's door), so a bound that IS a container
	// is refused with the reference's sentence rather than reaching `rt_slice` as a heap handle.
	if kind, ok := g.staticContainerKind(e); ok {
		return kind, true
	}
	nm, isName := c.Fn.(*Name)
	if !isName {
		return "", false
	}
	// A body the text tables own answers a text: its word is an `@str_tab` index, and an index counted as a
	// position is this row's wrong answer (roadmap Gap R.233's disease, one construct and one indirection on).
	if g.strFuncs[nm.Value] {
		return kindStr, true
	}
	// A body ADR 0274's promotion emitted `define double` answers a double, and ADR 0475 is the record of
	// what one does to an `i32` parameter slot; to `rt_slice`'s argument it does the same.
	if g.floatFuncs[nm.Value] {
		return kindFloat, true
	}
	return "", false
}

// badSliceBoundGate asks the bound question at the slice door and raises through the same non-local road every
// other exception takes, so `except TypeError:` catches it and an uncaught one exits 3 with the reference's own
// two lines. It runs BEFORE the copy is built and before a bound that is a literal or a slot is lowered — which
// is also what keeps a list-valued bound from reaching `rt_slice`'s i32 arguments as a `@.lstN` global reference
// and taking the whole module down with it, the shape Gap R.222 measured for an index and this row measures for
// a slice. The one bound it does lower first is a CALL, whose body the reference runs on its way to building the
// slice (see the loop); everything else is refused unread. It ends the block it branched out of and reopens the
// continuation, so the door stays a valid module either way.
//
// The three bounds are asked in the order the reference asks them (low, high, step) so the first complaint is
// the one the reference would have made. The message names no type — the reference's own sentence does not —
// so the raise's value is in the class and in the silence on stdout, and the row asserts both.
//
// The error it returns is the compiler's own (a bound the road below could not lower), not the program's: the
// program's TypeError travels the non-local raise road like every other exception, and the caller's `true` says
// "a raise was written here, answer nothing and emit nothing after it".
func (g *irGen) badSliceBoundGate(b *strings.Builder, n *Slice) (bool, error) {
	if n == nil {
		return false, nil
	}
	for _, bound := range []Expr{n.Low, n.High, n.Step} {
		kind, bad := g.nonIntBoundTypeName(bound)
		if !bad {
			continue
		}
		if _, isCall := bound.(*Call); isCall && (kind == kindStr || kind == kindFloat) {
			// The reference builds the slice object from the values the bound EXPRESSIONS answer, and only then
			// refuses it — so a bound that is a call to a body the text or float tables own runs first, and
			// whatever the callee printed is on stdout before the complaint, the way the reference prints it.
			// The word it answers is thrown away (nothing else may read it: a text's word is an `@str_tab`
			// index and a float's a box handle), and the raise follows it, which is the one order that keeps
			// `print(xs[g():2])` printing `side` (**Gap R.237**'s half of the question). A CONTAINER answer is
			// not lowered here: this position keeps one word for a value, and lowering a container answer into
			// it is the refusal **Gap R.67**/**ADR 0333** already owns — the gate must not spend exit 1 on a
			// program the reference stops with a TypeError. Literal and slot bounds have no effects to run, so
			// they keep the gate's own order and are lowered nowhere.
			if _, err := g.value(b, bound); err != nil {
				return false, err
			}
		}
		g.heapUsed = true
		g.raiseTo(b, exnCode("TypeError"), "TypeError", sliceBoundsMessage(), n.Span())
		cont := g.newLabel("sbc.cont")
		b.WriteString(fmt.Sprintf("%s:\n", cont))
		return true, nil
	}
	return false, nil
}

// sliceStepZeroMessage is the reference's own words for the one *value* a bound may not take. The kind question
// above cannot answer it: `0` is a perfectly good integer, and the road that trusts it walks forever.
//
//	xs = [1, 2]
//	print(xs[::0])      # ValueError: slice step cannot be zero
//	                    # this backend: nothing at all — the run is killed, and exit 2 reports OUR bug (ADR 0166)
//	print(xs[0:2:0])    # the same ValueError
//	                    # this backend: []   at exit 0 — the loop happened to terminate on its bounds
//	print("abcd"[::0])  # the same ValueError
//	                    # this backend: `cannot fold string slice` at exit 1 — a refusal where the program raises
//
// Three shapes, three wrong answers, one missing question — and it is a different question from the gate's: that
// one asks what a bound *is*, this one asks what it *holds* (roadmap Gap R.236, ADR 0336's neighbour).
const sliceStepZeroMessage = "slice step cannot be zero"

// sliceStepIsKnown reports what the compiler can read of the step without running anything — a literal, a
// constant expression (`xs[::1 - 1]` with the spaces the parser needs, a name the constant notebook holds). A
// step it can read is settled at compile time: a known zero raises and emits no copy, a known non-zero needs no
// test at all, and only a step it cannot read (`xs[::k]`) carries the run-time guard. That split is what keeps
// the ordinary `xs[::2]` module free of a raise site it could never reach.
func (g *irGen) sliceStepIsKnown(n *Slice) (int64, bool) {
	if n == nil || g.sliceBoundIsAbsent(n.Step) {
		return 0, false
	}
	return g.foldConstInt(n.Step)
}

// sliceStepIsKnownZero is the question the text road asks: it has no run-time stepped helper to guard, so a
// step it can read as zero is the whole of the shape it can answer honestly.
func (g *irGen) sliceStepIsKnownZero(n *Slice) bool {
	v, ok := g.sliceStepIsKnown(n)
	return ok && v == 0
}

// raiseSliceStepIsZero raises the reference's ValueError on the non-local road — `except ValueError:` takes it,
// and an uncaught one reports the class and the message and exits 3 — then reopens the block, so the door stays
// a valid module either way.
func (g *irGen) raiseSliceStepIsZero(b *strings.Builder, n *Slice) {
	g.heapUsed = true
	g.raiseTo(b, exnCode("ValueError"), "ValueError", sliceStepZeroMessage, n.Span())
	cont := g.newLabel("sbz.cont")
	b.WriteString(fmt.Sprintf("%s:\n", cont))
}

// zeroStepGuardAtSlice asks the same question of a step the compiler could not read — `xs[::k]`, where `k` is a
// variable, a parameter, or the answer of a call. The check has to live where the value is, because the loop that
// never ends is the helper's own, and a run that has to be killed comes out as exit 2: the contract's compiler-bug
// class, paid by the toolchain for a program whose only fault is writing `::k`. It is emitted immediately before
// the copy is built, after every bound has been evaluated, which is the order the reference raises in: a slice
// object is constructed from the three bounds and *then* refuses a step of zero.
func (g *irGen) zeroStepGuardAtSlice(b *strings.Builder, stepReg string, sp Span) {
	bad := g.newTmp()
	b.WriteString(fmt.Sprintf("  %s = icmp eq i32 %s, 0\n", bad, stepReg))
	g.markI1(bad)
	badL, okL := g.newLabel("sbz.bad"), g.newLabel("sbz.ok")
	b.WriteString(fmt.Sprintf("  br i1 %s, label %%%s, label %%%s\n", bad, badL, okL))
	b.WriteString(badL + ":\n")
	g.raiseTo(b, exnCode("ValueError"), "ValueError", sliceStepZeroMessage, sp)
	b.WriteString(okL + ":\n")
	g.heapUsed = true
}
