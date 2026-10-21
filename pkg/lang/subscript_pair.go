package lang

import "strings"

// A slot read is one question, and every position that renders a value by its kind asks it in the same order.
//
// A subscript has two roads in this backend (ADR 0331's statement of them, above `listLiteralPair`). When the
// compiler can read the whole thing — a literal whose elements are all integer constants, at an integer
// position — the read is a compile-time lookup. When it cannot, the read is a *message to the object*, which
// answers a (payload, tag) pair, because the kind of a slot is a fact about the object and not about the line
// that wrote it (ADR 0187, ADR 0241).
//
// The pair door has always been one function per container (`mixedElemPair`, `mixedDictPair`,
// `literalDictPair`, `listLiteralPair`, `taggedContainerRead`). What was not one thing was the *list of
// them*: `print` asked all five, in an order it decided for itself, and the f-string field asked **none** —
// so one construct, `f"{d[1]}"`, printed the payload alone: the `@str_tab` index of a text entry, the
// `@float_box` handle of a float entry, the heap slot of a container entry, all at exit 0, while the very
// next line — `print(d[1])` — printed `3.5`, `x`, `[2]` (roadmap Gap R.283, measured beside ADR 0353).
// ADR 0331's own docstring claimed the field asked the door; the arm was never there. A door that is
// documented and not wired is worse than a missing door, because the test suite inherits the claim.
//
// So this file is that list, in one place, asked by both roads. The order is print's and is load-bearing: the
// five arms overlap (a dict literal is also a `*Name`-free subscript; a nested read is also a subscript of a
// subscript), and the two roads that disagree about which arm runs first are the two roads that grow apart.
// The binding road (`bindTaggedVar`'s callers) keeps its own order because it asks a different question —
// which name becomes a tagged pair — and is already answered for all five shapes.
//
// What the door does NOT serve is the one-word positions (Gap R.146's standing): a `return`, a call argument,
// an arithmetic operand. Those have nowhere to put a tag, and they refuse with the missing half named rather
// than handing out a handle and calling it a number.

// subscriptPairOf asks the object what the slot named by `ix` holds, and answers its payload **together with
// the tag that says what it is**. It is the whole print road's subscript dispatch, extracted so the f-string
// field asks the identical question and cannot answer it differently: the field, `print(x)` and `str(x)` of
// one and the same read now share one dispatch, one order, and one set of arms.
//
// ok=false means no arm runs — the ordinary one-word road answers, which is right for the shapes that road
// can read (an all-integer literal at a constant position, a single-kind container, an int slot). err is set
// only where an arm ran and could not finish, or where a literal element has a kind that cannot travel
// without its tag (`listLiteralOneWordErr`): a refusal is that road's own answer, and the field keeps it
// rather than printing a payload.
//
// The road sets `heapUsed`/`floatFmtUsed` as it goes, the way the print arms always did — the caller renders
// the pair through the module's one tag-reading printer, which lives in the heap runtime block.
func (g *irGen) subscriptPairOf(b *strings.Builder, ix *Index) (val, tag string, ok bool, err error) {
	if ix == nil {
		return "", "", false, nil
	}
	// `{"a": "x"}["a"]` — a dict the program spelled out and never bound. The entry is built, the key is
	// compared as a (payload, tag) pair, and the answer is read from the pair the object hands back: an int
	// entry prints a number, a text entry its word, a float entry its decimal, a container entry its own
	// contents. The fold still answers what it can read, so this arm runs exactly where the fold cannot
	// (roadmap Gap R.215, ADR 0327).
	if dl, isLit := ix.Obj.(*DictLit); isLit && !g.dictLiteralFoldAnswers(dl, ix.Idx) {
		v, t, derr := g.literalDictPair(b, dl, ix.Idx, ix.Span())
		if derr != nil {
			return "", "", false, derr
		}
		g.heapUsed = true
		g.floatFmtUsed = true
		return v, t, true, nil
	}
	// `[1.5, 2][0]` — the same door, one container over: an element whose kind lives in the tag, read by the
	// door a list *variable*'s element is read by, so the literal and the variable cannot grow apart
	// (roadmap Gap R.218, ADR 0331). A literal the fold reads is not this arm's, and an element whose kind
	// cannot travel without its tag is refused by name rather than printed as a table index.
	if ll, isLit := ix.Obj.(*ListLit); isLit && !g.listLiteralFoldAnswers(ll, ix.Idx) {
		v, t, okPair := g.listLiteralPair(b, ll, ix.Idx, ix.Span())
		if !okPair {
			return "", "", false, listLiteralOneWordErr(ll, ix.Idx)
		}
		g.heapUsed = true
		g.floatFmtUsed = true
		return v, t, true, nil
	}
	// `xs[i]` out of a container the notebook says mixes kinds: the element *and* its tag travel, so the
	// printer dispatches on what the slot holds instead of on what the compiler guessed (roadmap L11.1,
	// ADR 0187).
	if listName, mixed := g.mixedIndexRead(ix); mixed {
		v, t, merr := g.mixedElemPair(b, listName, ix.Idx, ix.Span())
		if merr != nil {
			return "", "", false, merr
		}
		g.heapUsed = true
		g.floatFmtUsed = true
		return v, t, true, nil
	}
	// `d[k]` out of a dict whose values mix kinds — the entry's own slot carries its tag, and that is the
	// only thing that can tell `"x"` from the number 2 (ADR 0232). Which keys the door is asked with is
	// `mixedDictIndexRead`'s own question (Gap R.221, ADR 0353).
	if dictName, mixed := g.mixedDictIndexRead(ix); mixed {
		v, t, merr := g.mixedDictPair(b, dictName, ix.Idx, ix.Span())
		if merr != nil {
			return "", "", false, merr
		}
		g.heapUsed = true
		g.floatFmtUsed = true
		return v, t, true, nil
	}
	// `xs[0][1]`, `d["a"][0]`, `t[0][0][0]`: the container being indexed is itself a read, so its kind is in
	// the object and not in the spelling, and only the object can say which arm runs (ADR 0251, ADR 0324).
	if v, t, okRead, rerr := g.taggedContainerRead(b, ix); rerr != nil {
		return "", "", false, rerr
	} else if okRead {
		g.heapUsed = true
		g.floatFmtUsed = true
		return v, t, true, nil
	}
	return "", "", false, nil
}

// conditionPairOfRead is the truth road's wider version of the same dispatch, and the difference is the
// fold. `subscriptPairOf` leaves a read the compile-time fold can spell to the ordinary road, which is
// right for a value position: the printer is told the kind and renders the payload. A condition asks a
// different question — it tests a word against zero — and the word the fold produces for an entry is an
// interned text's INDEX, a float box's HANDLE, a container's HANDLE. Each of those is non-zero for reasons
// that have nothing to do with whether the value is true, which is how `d = {"k": ""}` / `if d["k"]:` took
// the `then` arm at exit 0 (roadmap Gap R.284, ADR 0388).
//
// The one entry that keeps the fold is the entry whose payload already IS the value: a provable integer.
// `d = {1: 2}` / `if d[1]:` has always compared an i32 against zero and still does, asserted as an IR claim
// in dict_entry_truth_test.go so a door that opens wider than it must is caught here rather than in review.
func (g *irGen) conditionPairOfRead(b *strings.Builder, ix *Index) (val, tag string, ok bool, err error) {
	if ix == nil {
		return "", "", false, nil
	}
	lit := g.literalBehind(ix.Obj)
	if tg, known := g.staticSlotTag(lit, ix.Idx); known && tg == int32(TagInt) {
		return "", "", false, nil
	}
	switch c := lit.(type) {
	case *DictLit:
		v, t, derr := g.literalDictPair(b, c, ix.Idx, ix.Span())
		if derr != nil {
			return "", "", false, derr
		}
		g.heapUsed = true
		g.floatFmtUsed = true
		return v, t, true, nil
	case *ListLit:
		v, t, okPair := g.listLiteralPair(b, c, ix.Idx, ix.Span())
		if !okPair {
			return "", "", false, listLiteralOneWordErr(c, ix.Idx)
		}
		g.heapUsed = true
		g.floatFmtUsed = true
		return v, t, true, nil
	}
	return "", "", false, nil
}

// literalBehind walks a read's container back to the literal it names: `d = {...}` leaves `d` in
// containerLits and the pair doors need the literal the fold read, not the name. Two levels is enough for
// what the pair doors serve; a container that has left the analysis answers nothing here and keeps its own
// door (`subscriptPairOf`'s runtime-slot arms).
func (g *irGen) literalBehind(e Expr) Expr {
	for depth := 0; depth < 2; depth++ {
		nm, isName := e.(*Name)
		if !isName {
			return e
		}
		if lit, ok := g.containerLits[nm.Value]; ok {
			e = lit
			continue
		}
		if lit, ok := g.staticLists[nm.Value]; ok {
			e = lit
			continue
		}
		return e
	}
	return e
}
