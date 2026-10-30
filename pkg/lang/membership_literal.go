package lang

import (
	"fmt"
	"strings"
)

// membershipOfLiteralContainer is `1 in [x]`, `"a" in [y]`, `5 in [f()]`, `5 in [x + 1]`, `1 in {x}`: the
// haystack is a container the SOURCE spells, and every slot of it is a value this door fills itself.
//
// Until this door existed the test lowered its haystack through the number road — `g.value(b, n.R)` — and
// for a literal that road answers a folded `@.lstN` global whose element slots are `i32` words. A name, a
// call, an arithmetic expression has no word to write into one of those slots, so the literal road refused
// the program: `x = 5` / `print(1 in [x])` died with `list literal elements must be integers, not int` — a
// sentence about an element kind the literal was not asking about — where the reference prints `False`, and
// `y = "a"` / `print("a" in [y])` died the same way with `not str` where the reference prints `True`
// (**roadmap Gap R.523**, **ADR 0456**). The SAME literal compiles one spelling away: `print([x])` prints
// `[5]`, `print(len([x]))` prints `1`, `print([[x]])` prints `[[5]]`, because those ask the container door,
// which builds a heap object whose slots carry a kind beside every payload. This door asks that same door —
// the haystack is built once by `containerOperand`, and the question goes to `rt_contains_tagged`, the
// object's own slots answering it. It is `membershipOfBuiltSequence`'s shape one spelling earlier: that door
// takes the haystack an operator builds (`2 in [1] + [3]`), this one takes the haystack the program writes.
//
// Two rules keep the door from costing more than the bug:
//
//   - It is asked BEFORE the operands are lowered, for the reason every door above that line gives — the
//     ordinary lowering of the haystack is the thing that refuses. Lowering it first is what made a working
//     program look like a compiler bug.
//   - It declines whenever the folded road can serve the haystack (`1 in [1, 2, 3]` stays the unroll that
//     builds no object at all, and the empty literal stays a compile-time verdict), and whenever no
//     (payload, kind) pair can be named for the needle — which leaves a needle the substring roads already
//     serve exactly where it was, and leaves a haystack no builder covers to the refusal that names the row
//     owning it. A door that answers beside the roads that already answer is the second opinion ADR 0166's
//     ladder punishes.
//
// A needle is read as the pair, never as a payload alone: a text needle's payload is an `@str_tab` index and
// a double's is the handle of its `@float_box`, and comparing payloads without the kind beside them is how
// one value stopped being a member of the very list it came from.
func (g *irGen) membershipOfLiteralContainer(b *strings.Builder, n *BinOp) (val string, ok bool, err error) {
	if n.Op != "in" && n.Op != "not in" {
		return "", false, nil
	}
	if !spellsLiteralContainer(n.R) {
		return "", false, nil
	}
	// The folded road owns a haystack it can serve: elements the compiler can write into a global at emit
	// time AND a needle whose kind the fold can name — a whole number, compared `icmp eq i32`. Both halves
	// matter. Declining on the first is what keeps `x in [1, 2, 3]` the zero-allocation unroll it always was.
	// Declining on the second is what lets the object answer the needles the fold CANNOT name: it compares
	// payloads alone, which is how the void became a member of a list of whole numbers (`None in [0]` reads
	// the void's word as a 0) and why a double never was (`1.0 in [1]` was refused outright, the numeric
	// cross-kind arm being `rt_payload_eq`'s, not the fold's). Asked of the object, both answer what the
	// reference answers (**roadmap Gap R.523**, **ADR 0456**; the payload-only fold is **ADR 0233**'s record).
	if _, served := intMembershipValues(n.R); served && g.foldNamesNeedle(n.L) {
		return "", false, nil
	}
	// A text haystack is the substring question, and the road beside this already answers it.
	if _, isStr := g.stringVal(n.R); isStr {
		return "", false, nil
	}
	hay, herr := g.containerOperand(b, n.R)
	if herr != nil {
		// The literal road declines for its own reasons — a shape no builder covers — and its refusal
		// names the row that owns that shape. Falling through hands the user that sentence rather than
		// a vaguer one minted here.
		return "", false, nil
	}
	// The needle is an ordering builtin the doors cannot build. Its value is the fold's GLOBAL, and
	// `rt_contains_tagged` takes a word, so the module would be refused by `llc` — the answer ADR 0166
	// ranks below a refusal, and the reason this door asks the question the equality door asks
	// (**Gap R.253**, **ADR 0472**).
	if g.sortedReversedDoorIsAskedOf(n.L) && !g.sortedReversedIsAContainer(n.L) {
		return "", true, g.sortedReversedDoorRefusal(n.L)
	}
	needle, needleTag, okPair := g.taggedOperand(b, n.L)
	if !okPair {
		// The haystack has already been built, and getting here may have run the effects its elements
		// performed: a declining door must not undo an effect an operand already had, and must not answer
		// with a payload whose kind it cannot name (membershipOfBuiltSequence's rule for the same case).
		return "", false, nil
	}
	// The needle is the ITERATOR `reversed` returned. The reference has no `__eq__` on an iterator, so it
	// matches one by identity and it is therefore a member of no container — the content question the door
	// is about to ask of the copy this call built would answer True for
	// `print(reversed([1, 2]) in [[2, 1]])` where the reference prints False (**Gap R.251**, **ADR 0472**).
	// Both operands are built by the lines above, so deciding here deletes no effect.
	if g.reversedIsAnIterator(n.L) {
		return g.iteratorIsNoMember(n.Op), true, nil
	}
	g.heapUsed = true
	found := g.newTmp()
	fmt.Fprintf(b, "  %s = call i32 @rt_contains_tagged(i32 %s, i32 %s, i32 %s)\n", found, hay, needle, needleTag)
	cmp := g.newTmp()
	if n.Op == "in" {
		fmt.Fprintf(b, "  %s = icmp ne i32 %s, 0\n", cmp, found)
	} else {
		fmt.Fprintf(b, "  %s = icmp eq i32 %s, 0\n", cmp, found)
	}
	g.markI1(cmp)
	out := g.newTmp()
	fmt.Fprintf(b, "  %s = zext i1 %s to i32\n", out, cmp)
	return out, true, nil
}

// spellsLiteralContainer asks whether the haystack is a container WRITTEN as a literal — the shape whose
// slots this door fills itself, as opposed to a name, a call's answer or a sequence an operator built, each
// of which already has a door above this one. It is a separate question from `isContainerExpr` because that
// one answers "does this denote a container" for every road, and this one answers "may this road build it".
func spellsLiteralContainer(e Expr) bool {
	switch e.(type) {
	case *ListLit, *SetLit, *DictLit:
		return true
	}
	return false
}

// foldNamesNeedle is the second half of the question the folded unroll answers: can `icmp eq i32` name this
// needle at all? A whole number and a verdict can — the fold lowers them to the word they occupy. A double,
// a text, the void and a container cannot: their word is a `@float_box` handle, an `@str_tab` index, the
// void's nothing and an object's handle, and comparing those words against a slot's payload without its kind
// beside them is the payload-only answer the door replaces. It is the same kind table the slot builder reads
// (`elemKindTag`), so the fold and the door cannot disagree about which needles are which (**ADR 0232**'s
// one question).
func (g *irGen) foldNamesNeedle(e Expr) bool {
	if g.isFloat(e) || isStringExpr(e) {
		return false
	}
	t, ok := g.elemKindTag(e)
	if !ok {
		return false
	}
	return t == int32(TagInt) || t == int32(TagBool)
}
