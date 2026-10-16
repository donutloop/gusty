package lang

import "strings"

// pkg/lang/assign_pair.go — the ONE question an assignment asks of its right-hand side: does this value
// travel as a (payload, tag) pair, and if so, what are its two words?
//
// Until Gap R.144 the question existed twice: once inline in the plain-assignment road (the four subscript
// doors) and once as four separate `bind*Pair` helpers called in a fixed order right after them. An
// unpacking — `a, b = xs[0][0] + 1, xs[0][1] + 2` — asked neither. It lowered every element through the
// ordinary one-word road, which refuses the slot it cannot see into, or worse, answers it: with
// `d = {"a": "x"}` the element `d["a"]` came back as the text's `@str_tab` index and the name was bound to
// `1` at exit 0 while the reference binds `x` (measured landing this row; the same shape had been paid for
// `print`, for the plain binding by ADR 0267 and for a list element by ADR 0332 — the unpacking was the
// binding road that had not been taken).
//
// So the doors are now one ordered list, and BOTH binding roads ask it. The plain assignment asks the two
// halves at the two positions it has always asked them from (the subscript half before its container roads,
// the value half after them), and the unpacking asks the whole list per element, because an element of an
// unpacking is a binding position exactly like the plain one. The list is ordered and every door is allowed
// to decline: a door that answers must not speak over the road that would have named what it could not
// carry (ADR 0273), and a shape no door answers keeps the ordinary road's own refusal, word for word.
//
// One door belongs to the unpacking alone: a name the pair road already bound (`a, b = n, 1`). The plain
// road has no answer for `y = n` either and never had one — that position is Gap R.321, filed beside this
// row rather than quietly widened into it, and it is pinned as a refusal below so the two roads cannot drift
// into agreeing by accident.
//
// roadmap L11.1, Gap R.144; ADR 0367 (this file), ADR 0267 (the plain binding's pair), ADR 0187 (one door
// binds every pair), ADR 0232 (one question, asked once).

// assignPair is one right-hand side answered as a pair: the payload, the tag that says what the payload
// means, and the three records the binding door writes beside them.
type assignPair struct {
	payload string
	tag     string
	// origin is what the name's tag will be recorded as coming from, so a later one-word position can say
	// where the tag it cannot carry came from (Gap R.38's rule: a refusal may only describe this program).
	origin string
	// restateOrigin is the difference between "record this origin" and "this origin is the answer": the four
	// value doors always restate it, the subscript doors only fill it in when nothing wrote it, which is how
	// the plain road has behaved since ADR 0241 kept recordSlotOrigin out of the way of an earlier claim.
	restateOrigin bool
	// roots says whether the payload may name a heap object — a float box, a container handle — and so
	// whether the name's slot has to be registered as a root for as long as the name holds it (ADR 0181).
	roots bool
}

// assignPairWords asks the whole ordered list. It is what an unpacking element asks; the plain assignment
// asks the same doors through indexPairWords and valuePairWords, at its own two positions.
func (g *irGen) assignPairWords(b *strings.Builder, e Expr) (assignPair, bool, error) {
	if p, ok, err := g.indexPairWords(b, e); err != nil || ok {
		return p, ok, err
	}
	if p, ok, err := g.valuePairWords(b, e); err != nil || ok {
		return p, ok, err
	}
	p, ok := g.namePairWords(b, e)
	return p, ok, nil
}

// indexPairWords is the subscript half of the question: the read whose answer's kind lives beside the slot.
// The order is the plain assignment's, and it is load-bearing — the compile-time fold over a dict literal
// (dictLiteralFoldAnswers) has to answer before the object is built, and the run-time door is the last word.
func (g *irGen) indexPairWords(b *strings.Builder, e Expr) (assignPair, bool, error) {
	ix, isIndex := e.(*Index)
	if !isIndex {
		return assignPair{}, false, nil
	}
	// `y = xs[0][1]`, `y = d["a"][1]`, `y = t[0][0][0]` — a slot reached through a slot: only the objects
	// know the inner kind, and the payload means nothing without the tag that came with it (ADR 0241).
	if v, t, okRead, rerr := g.taggedContainerRead(b, ix); okRead && rerr == nil {
		return assignPair{payload: v, tag: t, origin: taggedOriginSlot}, true, nil
	}
	if dictName, mixed := g.mixedDictIndexRead(ix); mixed {
		// `v = d[k]` out of a dict whose values mix kinds.
		v, t, err := g.mixedDictPair(b, dictName, ix.Idx, ix.Span())
		if err != nil {
			return assignPair{}, false, err
		}
		return assignPair{payload: v, tag: t, origin: taggedOriginSlot}, true, nil
	}
	if dl, isLit := ix.Obj.(*DictLit); isLit && !g.dictLiteralFoldAnswers(dl, ix.Idx) {
		// `v = {"a": "x"}["a"]` — the entry's kind is a fact about the object, and a payload without its tag
		// is a table index or a box handle wearing a number (Gap R.215, ADR 0327).
		v, t, err := g.literalDictPair(b, dl, ix.Idx, ix.Span())
		if err != nil {
			return assignPair{}, false, err
		}
		return assignPair{payload: v, tag: t, origin: taggedOriginSlot}, true, nil
	}
	if listName, mixed := g.mixedIndexRead(ix); mixed {
		// `v = xs[i]` out of a tagged list (ADR 0185, ADR 0187).
		v, t, err := g.mixedElemPair(b, listName, ix.Idx, ix.Span())
		if err != nil {
			return assignPair{}, false, err
		}
		return assignPair{payload: v, tag: t, origin: taggedOriginSlot}, true, nil
	}
	return assignPair{}, false, nil
}

// valuePairWords is the expression half: the four doors the plain assignment has asked, in its order, ever
// since ADR 0267 gave the binding the answer the arithmetic arrived in.
func (g *irGen) valuePairWords(b *strings.Builder, e Expr) (assignPair, bool, error) {
	// `v = twice(xs[0][0])` — the answer arrives as a pair, the tag travelling in the word the callee
	// stores beside its own return (ADR 0273). The payload may be a float box: the slot roots it.
	if p, t, ok, err := g.pairCallPair(b, e); err != nil {
		return assignPair{}, false, err
	} else if ok {
		return assignPair{payload: p, tag: t, origin: taggedOriginArith, restateOrigin: true, roots: true}, true, nil
	}
	// `n = xs[0][0] * 2` — the arithmetic the print position already answers (ADR 0265), asked of the
	// objects because the ordinary numeric road wants one i32 with no tag beside it. Guarded by the same
	// question the plain road asks: only where the ordinary road would refuse.
	if g.arithWouldRefuse(e) {
		if p, okPair, err := g.arithPairWords(b, e); err != nil {
			return assignPair{}, false, err
		} else if okPair {
			return p, true, nil
		}
	}
	// `m = min(n, 3)` — a fold hands back one of the values it was given, so the answer's kind is the
	// winner's own and only the run time knows which arm won (ADR 0316).
	if p, t, ok, err := g.taggedFoldPair(b, e); err != nil {
		return assignPair{}, false, err
	} else if ok {
		return assignPair{payload: p, tag: t, origin: taggedOriginFold, restateOrigin: true}, true, nil
	}
	// `n = xs[0]` over a container the program BUILT: the kind is a fact about the object (ADR 0251,
	// ADR 0185's rule carried to the binding road by Gap R.146's door).
	if p, t, ok := g.runtimeSlotPairDeep(b, e); ok {
		return assignPair{payload: p, tag: t, origin: taggedOriginSlot, restateOrigin: true, roots: true}, true, nil
	}
	return assignPair{}, false, nil
}

// namePairWords is the unpacking's own door: `a, b = n, 1` where `n = xs[0]` bound a pair. The name is
// already a (payload, tag) pair living in two allocas, and an unpacking binds a name, so the two words
// travel together into the new name — which is what `print(n)` and `print(a)` have to agree about.
//
// The plain binding road does not ask this door: `y = n` still refuses, with the sentence that names where
// n's tag came from. That position is Gap R.321.
func (g *irGen) namePairWords(b *strings.Builder, e Expr) (assignPair, bool) {
	nm, isName := e.(*Name)
	if !isName || !g.allocd[nm.Value+"_tag"] {
		return assignPair{}, false
	}
	if !(g.numericPairVar(nm.Value) || (g.taggedVars != nil && g.taggedVars[nm.Value])) {
		return assignPair{}, false
	}
	payload, tag := g.numericPairRegs(b, nm.Value, nm.Span())
	// The origin travels with the pair: the new name holds the value that door produced, and a refusal it
	// later meets may only say where that value came from (Gap R.38).
	return assignPair{payload: payload, tag: tag, origin: g.taggedOrigin[nm.Value], restateOrigin: g.taggedOrigin[nm.Value] != "", roots: true}, true
}

// arithPairWords is `n = xs[0][0] * 2`: the arithmetic the print position already answers (ADR 0265),
// asked of the objects because the ordinary numeric road wants one i32 with no tag beside it.
func (g *irGen) arithPairWords(b *strings.Builder, e Expr) (assignPair, bool, error) {
	v, t, okPair, err := g.taggedArithPair(b, e)
	if err != nil || !okPair {
		return assignPair{}, false, err
	}
	origin := taggedOriginArith
	// The sentence a later refusal writes has to name what the arithmetic was *over*: a slot the program
	// built (`xs = [1]; xs.append(2.5)`), or a parameter the caller supplied — telling the reader to look for
	// a container they never wrote is Gap R.38's own defect (roadmap L11.1, Gap R.38, ADR 0285).
	if g.arithReadsAParameter(e, g.params) {
		origin = taggedOriginParamArith
	}
	return assignPair{payload: v, tag: t, origin: origin, restateOrigin: true}, true, nil
}

// bindAssignPair writes one name with the two words an assignPair carries. It is bindTaggedVar (ADR 0187's
// one door for every pair a program binds) plus the two records that door does not own: where the tag came
// from, and whether the payload has to be rooted.
func (g *irGen) bindAssignPair(b *strings.Builder, name string, p assignPair) {
	g.bindTaggedVar(b, name, p.payload, p.tag)
	if p.origin != "" {
		if g.taggedOrigin == nil {
			g.taggedOrigin = map[string]string{}
		}
		if p.restateOrigin {
			g.taggedOrigin[name] = p.origin
		} else {
			g.recordSlotOrigin(name)
		}
	}
	if p.roots {
		// The payload may name a heap object, and a handle that is not a root is one collection away from
		// the segfault ADR 0181's rooting exists to prevent.
		g.gcReg(b, name)
	}
}

// bindArithmeticPair is the augmented-assignment road's (`n += 1` over a slot) half of the door above: it
// asks the one arithmetic door and binds the answer through the one binding door.
func (g *irGen) bindArithmeticPair(b *strings.Builder, name string, e Expr) (bool, error) {
	if !g.arithWouldRefuse(e) {
		return false, nil
	}
	p, ok, err := g.arithPairWords(b, e)
	if err != nil || !ok {
		return false, err
	}
	g.bindAssignPair(b, name, p)
	return true, nil
}
