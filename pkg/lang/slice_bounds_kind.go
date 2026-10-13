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
func (g *irGen) nonIntBoundTypeName(e Expr) (string, bool) {
	if g.sliceBoundIsAbsent(e) {
		return "", false
	}
	typeName, bad := g.nonIntIndexTypeName(e)
	if !bad || typeName == "NoneType" {
		return "", false
	}
	return typeName, true
}

// badSliceBoundGate asks the bound question at the slice door and raises through the same non-local road every
// other exception takes, so `except TypeError:` catches it and an uncaught one exits 3 with the reference's own
// two lines. It runs BEFORE any bound is lowered — which is also what keeps a list-valued bound from reaching
// `rt_slice`'s i32 arguments as a `@.lstN` global reference and taking the whole module down with it, the shape
// Gap R.222 measured for an index and this row measures for a slice. It ends the block it branched out of and
// reopens the continuation, so the door stays a valid module either way.
//
// The three bounds are asked in the order the reference asks them (low, high, step) so the first complaint is
// the one the reference would have made. The message names no type — the reference's own sentence does not —
// so the raise's value is in the class and in the silence on stdout, and the row asserts both.
func (g *irGen) badSliceBoundGate(b *strings.Builder, n *Slice) bool {
	if n == nil {
		return false
	}
	for _, bound := range []Expr{n.Low, n.High, n.Step} {
		if _, bad := g.nonIntBoundTypeName(bound); !bad {
			continue
		}
		g.heapUsed = true
		g.raiseTo(b, exnCode("TypeError"), "TypeError", sliceBoundsMessage(), n.Span())
		cont := g.newLabel("sbc.cont")
		b.WriteString(fmt.Sprintf("%s:\n", cont))
		return true
	}
	return false
}
