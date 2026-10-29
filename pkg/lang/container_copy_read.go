// container_copy_read.go — the object that has no name is given one, so every road that reads a
// container variable reads it too (roadmap Gap R.235, ADR 0459).
//
// Three constructs build a container object and never name it:
//
//   - `rt_slice` builds the copy — `xs[0:2]`;
//   - a comprehension's loop builds its brace — `[x for x in xs]`;
//   - a slot holds the object another container wrote — `d[0]`, `m()["k"]`.
//
// Every one of them lowers to a heap handle. The roads that READ a container — the subscript door, the
// copy door — have always asked the question of the base's SPELLING instead: the fold's gate wants an
// "inline list/dict/set literal", and the copy's gate wants "a literal, a variable a binding recorded as a
// list, a call that hands back a list, or a slice of one". A handle has a spelling and none of those words
// describe it, so the program that named the copy (`ys = xs[0:2]` / `print(ys[0])`) answered and the program
// that did not (`print(xs[0:2][0])`) stopped the build — one object, two doors (roadmap Gap R.235).
//
// The door below fixes that at the altitude the decision belongs on: it asks the object's own builder for
// the handle (`containerHandleOf`, the road the print, the length and the membership doors already use) and
// for the tag claim, roots the handle under a fresh name and writes the SAME notebooks the binding road
// writes for `ys = <that>`. The caller then lowers the node with the base replaced by that name — so the
// bounds raise, the mutator, the pop pair, the set hash door and the container printer all answer the direct
// shape without being told anything exists. ADR 0458 recorded this shape of fix for a method run on a call's
// answer; ADR 0333 and ADR 0334 recorded it for a subscript standing on a call, and **ADR 0232** is the rule
// it obeys: one question, asked by one door, at the altitude that decides.
//
// What the door declines, it declines BY NAME: an object whose element kind the builder never claimed is not
// labelled with one the compiler invented (roadmap Gap R.539 owns the claim for a comprehension whose loop
// copies a container that mixes kinds; Gap R.540 owns the record for a name bound from such a slot). A
// refusal is cheap; a fabricated number is the answer this toolchain may not print.
package lang

import (
	"fmt"
	"strings"
)

// containerCopyOperand answers the base an expression-position road can read: it lowers `e` to the heap
// handle its own builder made, roots it, writes the container notebooks under a fresh name and hands back
// that name. It returns ok=false — leaving the caller's own refusal about the node the PROGRAM wrote — for
// every shape whose tag claim it cannot derive (roadmap Gap R.235, ADR 0459).
func (g *irGen) containerCopyOperand(b *strings.Builder, e Expr) (Expr, bool) {
	tags, ok := g.containerCopyClaims(e)
	if !ok {
		return nil, false
	}
	h, _, okH := g.containerHandleOf(b, e)
	if !okH {
		return nil, false
	}
	g.tmp++
	name := fmt.Sprintf("__cobj%d", g.tmp)
	g.allocd[name] = true
	g.heapUsed = true
	b.WriteString(fmt.Sprintf("  %s = alloca i32\n", "%_"+name))
	g.gcReg(b, name)
	b.WriteString(fmt.Sprintf("  store i32 %s, i32* %s\n", h, "%_"+name))
	// The claims of the object, written by the one door each kind has (Gap Q.1, ADR 0232): the copy goes to
	// the road the sliced binding already asks, the comprehension to the road its own binding asks, and the
	// slot to neither — its object was built by whoever wrote the slot, so only the tag claim is this
	// door's to make.
	switch n := e.(type) {
	case *Slice:
		if !g.recordContainerSlice(name, n.Obj) {
			return nil, false
		}
	case *Comp:
		g.recordCompBindingClaims(name, n)
	case *Index:
		g.settleContainerKind(name, containerKindList)
		if tags {
			g.mixedLists[name] = true
			delete(g.listElemStr, name)
			delete(g.listElemInt, name)
		}
	}
	delete(g.containerLits, name)
	return &Name{Value: name, Src: e.Span()}, true
}

// containerCopySubscript is the subscript door's half of the rewrite: an `obj[i]` whose `obj` is a copy no
// name was written for. The base is replaced by the name the door just gave it and the caller lowers the
// node, so the bounds check, the raise, the pop pair and the tagged read are the roads a container variable
// already walks (roadmap Gap R.235, ADR 0459).
//
// Only the copy is taken here. A comprehension or a slot read standing under a subscript already has roads
// that answer it (`print([x for x in [1, 2, 3]][0])` is `1`, `d[0][0]` is `1`), and this row's rule is that
// an answer must not be traded for a new door beside it.
func (g *irGen) containerCopySubscript(b *strings.Builder, n *Index) (Expr, bool) {
	sl, isSlice := n.Obj.(*Slice)
	if !isSlice {
		return nil, false
	}
	tags, ok := g.containerCopyClaims(sl)
	if !ok {
		return nil, false
	}
	// The rewrite is only safe where a road exists that reads the slot BACK. The pair door takes the copy
	// whatever it holds (`subscriptPairOf`'s copy arm — the tags travel with the slots), but the ordinary
	// subscript road loads ONE word and calls it the value, which is true of an integer slot and false of a
	// text's `@str_tab` index, a float's box handle and a container's heap handle: `xs = ["x","y"]` /
	// `print(xs[0:2][0] + xs[0:2][1])` answered `1` — the two interned indices summed — at the exit code of
	// success, where the door had declined before it existed. A door that opens a one-word position without a
	// road to serve it manufactures ADR 0166's worst class, so it declines and lets the position keep
	// Gap R.82's refusal (roadmap Gap R.235, ADR 0459 — ADR 0458's rule that a position reading a SLOT is
	// taken only on the proof that the door which renders it serves the rewritten call).
	if !tags && !g.containerSlotsAnswerOneWord(sl.Obj) {
		return nil, false
	}
	name, okName := g.containerCopyOperand(b, n.Obj)
	if !okName {
		return nil, false
	}
	redo := *n
	redo.Obj = name
	return &redo, true
}

// containerSlotsAnswerOneWord asks whether every slot of the object the copy was taken from holds a value
// whose ONE word IS the value: an integer, or a verdict wearing the integer 0/1. The answers come from the
// same records the fold roads ask — the literal the program spelled, the answer the scan proved, the claim
// the builder left on the name — and an object the compiler cannot read answers no, which is the safe side:
// the plain road is then not taken and the position keeps its own refusal (roadmap Gap R.82, L11.1).
func (g *irGen) containerSlotsAnswerOneWord(base Expr) bool {
	switch n := base.(type) {
	case *ListLit:
		for _, el := range n.Elems {
			t, okTag := g.elemKindTag(el)
			if !okTag || (t != int32(TagInt) && t != int32(TagBool)) {
				return false
			}
		}
		return true
	case *Name:
		if lit, known := g.containerLits[n.Value]; known {
			return g.containerSlotsAnswerOneWord(lit)
		}
		if lit, known := g.staticLists[n.Value]; known {
			return g.containerSlotsAnswerOneWord(lit)
		}
		return g.listVars[n.Value] && g.listElemInt[n.Value]
	case *Call:
		fn, isName := n.Fn.(*Name)
		if !isName {
			return false
		}
		ans, okAns := g.containerAnswerOf(fn.Value)
		if !okAns || !ans.takesRead(containerReadSlots) || len(ans.arms) == 0 {
			return false
		}
		for _, arm := range ans.arms {
			if !g.containerSlotsAnswerOneWord(arm) {
				return false
			}
		}
		return true
	case *Slice:
		return g.containerSlotsAnswerOneWord(n.Obj)
	}
	return false
}

// containerCopyPairBase is the same rewrite standing at the PAIR doors, which are asked by the print road,
// `v = xs[i]` and every other position that reads a slot with its tag — all of them BEFORE they choose what
// to ask the subscript road for. A door that rewrote only inside `valueIndex` left those roads looking at a
// `*Slice`, declining it, and handing the read to the untagged fold, which refused naming Gap R.82 while the
// bound spelling answered: the two halves of one answer asked by two predicates that had never met (ADR 0300
// 's rule, ADR 0458's lesson, roadmap Gap R.235).
func (g *irGen) containerCopyPairBase(b *strings.Builder, e Expr) (Expr, bool) {
	ix, isIndex := e.(*Index)
	if !isIndex {
		return nil, false
	}
	return g.containerCopySubscript(b, ix)
}

// containerCopySliceBase is the copy door's half: a slice whose BASE is an object the run time built and no
// statement named — `[x for x in xs][0:2]`, `d[0][0:1]`, `m()["k"][0:1]`. The base is bound the way the
// binding road binds it and the caller lowers the slice over the name, which is the shape
// `ys = [x for x in xs]` / `print(ys[0:2])` has always answered.
//
// The copy of a copy stays on the road it has always walked, and a base that is a name, a literal or a
// call's answer is described before this door is ever asked.
func (g *irGen) containerCopySliceBase(b *strings.Builder, n *Slice) (Expr, bool) {
	switch n.Obj.(type) {
	case *Comp, *Index:
	default:
		return nil, false
	}
	name, ok := g.containerCopyOperand(b, n.Obj)
	if !ok {
		return nil, false
	}
	redo := *n
	redo.Obj = name
	return &redo, true
}

// containerCopyClaims is the door's gate, asked BEFORE anything is lowered so a declined shape keeps the
// position's own refusal (ADR 0458's lesson: a door that lowers first leaves a diagnostic about a name the
// program never wrote). It answers whether the object's slots describe themselves, and no for any object
// whose element kind is a claim no builder wrote.
func (g *irGen) containerCopyClaims(e Expr) (tags bool, ok bool) {
	switch n := e.(type) {
	case *Slice:
		// The copy is only as good as the description of what it copies — the question the copy road asks
		// itself, asked here one construct earlier, plus the tag claim the same road writes.
		if !g.sliceBaseIsDescribed(n.Obj) {
			return false, false
		}
		if kind, okKind := g.staticContainerKind(n.Obj); !okKind || kind != "list" {
			return false, false
		}
		return g.containerSlotsCarryTags(n.Obj), true
	case *Comp:
		return g.compCopyClaims(n)
	case *Index:
		return g.slotObjectClaims(n)
	}
	return false, false
}

// compCopyClaims answers the two questions the comprehension's own builder answered when it wrote the
// object: what kind is the object, and did it let its slots speak.
//
// A comp whose every element folded is a literal wearing brackets and asks the literal's question; a comp
// whose loop wrote per-slot tags has said so on the object, and the copy inherits the word (`rt_slice`
// copies payloads, tags and the attribute word together — ADR 0361); anything else is the case the claim
// was never made for, and the door declines it by name (roadmap Gap R.539).
func (g *irGen) compCopyClaims(c *Comp) (tags bool, ok bool) {
	if c == nil || !compIsContainer(c) || c.Kind != CompList || !compIsSingleClause(c) {
		return false, false
	}
	if lit, folded := g.foldedContainerCompLiteral(c); folded {
		return g.listObjectClaims(lit)
	}
	if g.compObjectLetsSlotsSpeak(c) {
		return true, true
	}
	if mixed, known := g.compWritesOneKind(c); !known || mixed {
		// The element's kind lives in the iterable's slots and nowhere the compiler can read it at this
		// point in the program. Labelling the copy anyway is the wrong-number class (Gap R.539).
		return false, false
	}
	return false, true
}

// compObjectLetsSlotsSpeak is the comprehension builder's OWN bit-8 question, imported rather than restated
// (roadmap Gap R.235, ADR 0232): the road that writes the tag array and the road that decides who reads it
// cannot then disagree about one object.
func (g *irGen) compObjectLetsSlotsSpeak(c *Comp) bool {
	if c == nil || len(c.Elems) != 1 {
		return false
	}
	t, okTag := g.elemKindTag(c.Elems[0])
	return (okTag && slotTagSelfDescribing(t)) || g.compElemIsTaggedLoopVar(c) || g.forwardedCallIsPairElem(c.Elems[0])
}

// compWritesOneKind answers whether the slots the comprehension's loop writes can hold more than one kind,
// and whether the compiler is in a position to know. `known=false` is the decline: an iterable the door
// cannot read is not a licence to guess.
func (g *irGen) compWritesOneKind(c *Comp) (mixed, known bool) {
	if len(c.Elems) != 1 {
		return false, true
	}
	nm, isName := c.Elems[0].(*Name)
	if !isName || c.ForVar == nil || nm.Value != c.ForVar.Value {
		// The element is an expression the door can ask a tag of; if that tag was good enough to build with
		// it is good enough to copy with.
		_, okTag := g.elemKindTag(c.Elems[0])
		return false, okTag
	}
	switch it := c.Iter.(type) {
	case *ListLit:
		return literalMixedKinds(it), true
	case *Name:
		if g.listVars[it.Value] || g.mixedLists[it.Value] {
			return g.mixedLists[it.Value], true
		}
		return false, false
	case *Call:
		// `range(...)` answers integers and nothing else; any other call is the iterable the door cannot read.
		if fn, isName := it.Fn.(*Name); isName && fn.Value == "range" {
			return false, true
		}
		return false, false
	}
	return false, false
}

// slotObjectClaims answers the copy door's question for a base that is a READ: does the slot hold a LIST,
// and did whoever wrote that object say whether its slots describe themselves? The two ways of knowing are
// the two the rest of the file already asks of a slot — the compile-time walk over the container's own
// elements, and the answer a scan-proved call hands back — and the answer has to hold for every arm
// (ADR 0172's rule asked of a return).
//
// The claim is taken from the EXPRESSION that built the object, which is the only thing that ever knew: an
// object written by any other road (a name, another call, a comprehension) is a tag this door did not write
// and does not claim (roadmap Gap R.261's rule, one object further in).
func (g *irGen) slotObjectClaims(ix *Index) (tags bool, ok bool) {
	if ix == nil {
		return false, false
	}
	if _, seen := g.staticSlotTag(ix.Obj, ix.Idx); seen {
		e, okE := g.staticElemExpr(ix.Obj, ix.Idx)
		if !okE {
			return false, false
		}
		return g.listObjectClaims(e)
	}
	c, isCall := ix.Obj.(*Call)
	if !isCall {
		return false, false
	}
	fn, isName := c.Fn.(*Name)
	if !isName {
		return false, false
	}
	ans, okAns := g.containerAnswerOf(fn.Value)
	if !okAns || !ans.takesRead(containerReadSlots) || len(ans.arms) == 0 {
		return false, false
	}
	sawTags := false
	for _, arm := range ans.arms {
		e, okE := g.staticElemExpr(arm, ix.Idx)
		if !okE {
			return false, false
		}
		t, okT := g.listObjectClaims(e)
		if !okT {
			return false, false
		}
		sawTags = sawTags || t
	}
	return sawTags, true
}

// listObjectClaims is the builder's own question asked of the expression that wrote an object:
// `heapListFrom` or `heapListFromTagged`, chosen by exactly these three predicates (ADR 0232's rule, the
// same one `containerAnswerCarriesTags` imports). Anything the door cannot pin to a spelled list answers no
// claim at all, which the caller reads as a decline.
func (g *irGen) listObjectClaims(e Expr) (tags bool, ok bool) {
	l, isList := e.(*ListLit)
	if !isList || l.IsTuple() {
		return false, false // the copy of a tuple keeps the row that owns the bracket bit (ADR 0361)
	}
	return g.literalNeedsHeap(l) || g.literalNeedsTags(l) || g.literalHasPairElement(l), true
}
