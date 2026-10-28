package lang

// A literal's element that is itself a read is stored as the pair the read answers.
//
// A container literal is built by asking each position for two words — a payload and the tag that says what
// the payload means — and the answer to "what are those two words?" has two sources. For an element the
// compiler can read off its spelling (`"a"`, `1.5`, `None`, `[1, 2]`) the tag is a constant written into the
// `rt_tag_elem`/`rt_dict_put_tagged`/`rt_set_add_tagged` call. For an element whose kind lives in a register
// the objects wrote — a name the pair road bound (`n = xs[0]`), and, since this row, a **subscript**
// (`xs[1]`, `{"a": "x"}["a"]`, `xs[0][1]`) — the tag has to be that register, because the slot's meaning is
// the object's fact and not the compiler's.
//
// The element that is itself a read never reached the register road. `pairElemPair` asked only for a `*Name`,
// so a subscript fell to the ordinary lowering, which hands the builder one word and lets the builder guess
// the tag:
//
//	print([{"a": "x"}["a"], 2])   # ['x', 2] in the reference; the program printed [1, 2]  — an @str_tab index
//	print([["a"][0], 2])          # ['a', 2] in the reference; the program refused         — the fold's error
//	print([[1.5, 2][0], 3])       # [1.5, 3] in the reference; the program refused
//	xs = [1, "a"]
//	print([xs[1], 2])             # ['a', 2] in the reference; the program refused        — Gap R.82's sentence
//
// Three refusals and one lie, all of them the same missing half: the print door, the binding door and the
// membership door all ask the subscript for a (payload, tag) pair and get one — `print(xs[1])` has printed
// `a` for as long as the pair door existed — while the container builder asked for one word and stored it.
// ADR 0306 opened this road for a name the pair road bound (`[n]`, `{"k": n}`, `{n}`); this file is the same
// road for the read the pair road reads (roadmap Gap R.224 — the residue ADR 0331 named, one boundary in
// from the element it just taught its own read).
//
// What is NOT widened is the important half of the row. The gate below asks the *same* questions the door
// asks, in the same order, and declines wherever the door would decline:
//
//   - the container side must be an object the run time owns (`runtimeSlotReadOf`) — a list or dict variable,
//     a list or dict literal, or another such read; a set is not a side, because a set has no slots to read;
//   - the read must be one the compile-time fold cannot answer alone (`listLiteralFoldAnswers`,
//     `dictLiteralFoldAnswers`) — `[[1, 2][0], 3]` is a number the compiler can see, needs no tag, and keeps
//     emitting a constant instead of allocating a list to ask it.
//
// And the positions that keep **one word** for a whole value — a `return`, a call argument, an operand of
// arithmetic — keep refusing (Gap R.146's standing): they have nowhere to put the register, and a container
// whose slots were written by a builder that guessed is the wrong answer the refusal exists to prevent.

// subscriptIsPairElem is `pairElemPair`'s question for the element that is itself a read: does this subscript
// denote a (payload, tag) pair the pair door answers — in which case a container builder that asks the
// element for one word has nowhere to put the kind, and the slot it writes means the wrong thing (roadmap
// Gap R.224).
//
// It is a gate and not a door: it emits nothing, and it asks the *same* questions the door asks, in the same
// order — is the container side one the run time owns, and is the read one the compile-time fold cannot
// answer on its own. Where the fold answers, the payload *is* the value and no tag is needed to say so; the
// fold keeps that shape, and a gate that admitted it would send the whole literal to the heap for nothing
// (ADR 0249's rule that a gate and a door which test different things ship a refusal nobody measured).
func (g *irGen) subscriptIsPairElem(x Expr) bool {
	if g == nil || x == nil {
		return false
	}
	ix, isIx := x.(*Index)
	if !isIx || !g.runtimeSlotReadOf(ix) {
		return false
	}
	switch base := ix.Obj.(type) {
	case *ListLit:
		return !g.listLiteralFoldAnswers(base, ix.Idx)
	case *DictLit:
		return !g.dictLiteralFoldAnswers(base, ix.Idx)
	}
	return true
}

// isPairElem is the whole element question, asked of every position of a container literal before any IR is
// emitted: is this value one whose kind travels beside it in a register the objects wrote, rather than in a
// tag the compiler can read off its spelling? Six shapes qualify — a name the pair road bound (ADR 0306),
// a subscript the pair door reads, a call the argument-carried answer plan owns, the answer of `list.pop()`,
// a hand-off that gives the caller's own argument back, and any call whose callee already answers with a
// pair. Every gate that decides whether a literal is built by the tagged builder has to ask this question
// and not a narrower one, because a builder that is never opened leaves the element to the road that stores
// one word and GUESSES the kind (roadmap L11.1, Gap R.146, Gap R.224, Gap R.502).
func (g *irGen) isPairElem(x Expr) bool {
	return g.nameIsPairElem(x) || g.subscriptIsPairElem(x) || g.carriedCallIsPairElem(x) ||
		g.popAnswerIsPairElem(x) || g.forwardedCallIsPairElem(x) || g.pairReturningCallIsPairElem(x)
}

// pairReturningCallIsPairElem is the sixth shape: the answer of a call the pair road already serves — the
// callee stored its answer's kind beside its own `ret` (**ADR 0273**), so the payload means a different
// thing per arm. The gate knew only the answer PLAN's carried functions (`carriedCallIsPairElem`), so the
// element of a list literal never reached the door that reads the second word, and the ordinary numeric
// road refused the shape the print door answers:
//
//	def g(v):
//	    return v
//	print([g(2.5), 1])   # [2.5, 1] in the reference; the program refused with Gap R.146's sentence
//
// A container ELEMENT is one of the positions ADR 0273 lists as opting in to a pair-returning answer —
// print, `str`/`repr`, a truth test, an f-string field and a container element — so widening the gate here
// gives the element the door that already exists rather than a new road (roadmap **Gap R.502**).
func (g *irGen) pairReturningCallIsPairElem(x Expr) bool {
	c, isCall := x.(*Call)
	if !isCall || g == nil {
		return false
	}
	if g.carriedCallIsPairElem(c) || g.popAnswerIsPairElem(c) || g.forwardedCallIsPairElem(c) {
		return false
	}
	nm, isName := c.Fn.(*Name)
	if !isName || !g.pairRetDone[nm.Value] {
		return false
	}
	// The claim is the pair road's own, narrowed to the answers the road can actually name: a hand-off of the
	// caller's argument, or a value the body spells. A body that COMPUTES its answer (`return v + 0`) keeps
	// Gap R.146's refusal, because an element built from a tag this pass cannot read prints a float box's
	// address as a number — the wrong answer the refusal exists to prevent (**ADR 0249**, **Gap R.502**).
	return g.handOffOrSpelledAnswer(c)
}

// The FIFTH shape — a hand-off that gives the caller's own argument back — is documented on
// `forwardedCallIsPairElem` in `forwarded_answer.go`, beside the door that reads the caller's expression.

// popAnswerIsPairElem is the fourth shape of pair element: the element a `list.pop()` removes. It is a
// subscript by another name — the position lives in the container, the kind lives in the container — and the
// door that answers it (`popAnswerPair`) reads the tag out of `@heap_tags` beside the payload, so a builder
// handed one word stores an `@str_tab` index and calls it a string:
//
//	xs = [1, "a"]
//	print([xs.pop(0), xs.pop(0)])   # [1, 'a'] in the reference; the program printed [1, 0]
//
// — the same answer, in the same program, that `print(xs.pop(0))` gives on its own, which is the shape of
// the bug. The gate asks the door's own shape question and declines where the door declines, so a `.pop()`
// the door will not serve never moves a literal onto the heap for nothing (roadmap L11.1, ADR 0410).
func (g *irGen) popAnswerIsPairElem(x Expr) bool {
	c, isCall := x.(*Call)
	if !isCall || g == nil {
		return false
	}
	_, _, serves := g.popAnswerShape(c)
	return serves
}

// carriedCallIsPairElem is the third shape of pair element: the answer of a call that carries its own
// kind. Its payload means a different thing per arm, so the position travels as the two words the callee
// wrote — the same arrangement ADR 0306 built for a name the pair road bound and ADR 0332 for a read the
// subscript door answers (roadmap L11.1, Gap R.219, ADR 0368).
func (g *irGen) carriedCallIsPairElem(x Expr) bool {
	c, isCall := x.(*Call)
	if !isCall {
		return false
	}
	if nm, isName := c.Fn.(*Name); isName {
		return g.answer.carries(nm.Value)
	}
	// The same answer at a class's door: a method whose arms answer more than one kind — or which answers a
	// container, the shape a `def` hands to `containerRet` and a method has no table for — stores the kind of
	// the arm that ran beside its `ret`, so the position travels as two words here too (roadmap **Gap R.453**,
	// **ADR 0430**).
	_, isCarried := g.carriedMethodCall(c)
	return isCarried
}
