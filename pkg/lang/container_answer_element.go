package lang

// container_answer_element.go — a container literal whose ELEMENT is a call that hands back a container
// (roadmap **Gap R.530**, **ADR 0457**).
//
// `def f():` / `    return [1, 2]` / `print([1, 2] in [f()])` refused with `list literal elements must be
// integers, not list[int]` where the reference prints `True`, and `print(1 in [f()])` the same way where it
// prints `False` — while `print(len([f()]))` in the very same program compiled and printed `1`. The
// sentence was a lie twice over: an element slot of a self-describing list is not an `i32` the compiler
// writes an integer into, and `list[int]` names an element whose kind the program had already proven. What
// was missing was not a runtime door — **ADR 0333**'s scan already knows `f` answers a list, `containerOperand`
// already carries the `*Call` arm that lowers such a call to the handle the callee's builders made, and
// `heapListFromTagged` already writes a kind beside every payload. What was missing was that the four doors
// that DECIDE how a literal is built never asked the scan anything: `literalNeedsHeap`, `literalNeedsTags`,
// `nestedContainerElem` and `elemKindTag` all stopped at the shapes the SOURCE spells, so `[f()]` was
// declared a constant int array, the constant emitter refused it, and the membership door **ADR 0456** added
// fell through to that refusal.
//
// This is **Gap R.487**/**Gap R.488** one construct over, and it carries the same lesson: the BUILD door has
// known about a construct for cycles (`containerOperand`'s `*Call` arm is **ADR 0333**), while the doors that
// decide never grew the arm — **ADR 0249**'s gate-and-door disagreement, a gate asking a different question
// from the door it gates. So the four doors ask ONE predicate, the one below, and an element cannot be
// claimed by the folded global while the object's builder is being told to tag it.
//
// The permission is deliberately narrower than "the scan names this function":
//
//   - the CALLEE'S OWN ARMS must be values the slot table can tag (`containerAnswerContentsTaggable`), the
//     same question `compContentsTaggable` asks of a comprehension element, because the slot holds the
//     callee's object and the tag claims what its slots hold;
//   - the ARGUMENTS must be values the container roads can carry (`containerAnswerCallIsCarriable`), because
//     an argument the pair road owns crosses the boundary as two words and this build has room for one; and
//   - a function the scan could only PROVE and not describe stays out — `containerAnswerOf` answers nothing
//     for it, so such a callee keeps the Gap R.261 refusal that says which half is missing rather than
//     having a slot label invented for contents nobody read.

// containerAnswerSlotIsContainer is the element door's answer for the slot that holds a CALL whose answer
// the program-wide scan proves is a container: the word the slot carries is the handle the callee's builders
// made, and the callee's one answer kind says which of the four that object is.
//
// It is the same shape of answer as `compSlotIsContainer` — tag, plus the permission to build — so that the
// gates (`literalNeedsHeap`, `literalNeedsTags`, `nestedContainerElem`) and the label (`elemKindTag`) ask one
// reading and cannot disagree about one element (**ADR 0232**'s one question, **ADR 0438**'s container-in-a-name
// rule applied to a container in a call's answer).
func (g *irGen) containerAnswerSlotIsContainer(e Expr) (int32, bool) {
	c, isCall := e.(*Call)
	if !isCall || g == nil {
		return 0, false
	}
	nm, isName := c.Fn.(*Name)
	if !isName {
		// An attribute call, a method, a subscripted callee: no name to ask the scan about. An attribute
		// call whose answer is a container is **Gap R.531**'s shape and keeps its refusal.
		return 0, false
	}
	// The scan's answer, and only an answer it can act on: `containerAnswerOf` already declines a function
	// whose answer word another convention owns (a float body's `double`, a text body's `@str_tab` index, a
	// generator's iterator, a decorated body), and says nothing at all for a name the scan never reached — an
	// applied `lambda`, which the hoisted-lambda machinery answers at the CALL SITE instead. Asking this
	// literal to store a handle another road is answering built a slot pointing back at the object being
	// built: `print((lambda: [(lambda: [3, 4])(), 1])())` printed `[[[[…` until the process died
	// (**ROAD-R.536**). Those callees keep their own refusals; this door does not borrow their answers.
	a, ok := g.containerAnswerOf(nm.Value)
	if !ok {
		return 0, false
	}
	// A definition the SOURCE wrote. `lookupFuncDef` answers for those and for the declarations this backend
	// mints, and `CloneOf` tells them apart: an applied `lambda`'s hoisted declaration carries ADR 0345's
	// anonymous display name there, and its answer is already being handed to the call site by the
	// hoisted-lambda machinery. Asking this literal to store that same handle made the object's slot point
	// back at the object — `print((lambda: [(lambda: [3, 4])(), 1])())` printed `[[[[…` until the process
	// died at signal 11 — so the hoisted `lambda` stays on the door's own refusal until the row that carries
	// a lambda's answer through a slot lands (**ROAD-R.536**).
	if fd := g.lookupFuncDef(nm.Value); fd == nil || fd.CloneOf != "" {
		return 0, false
	}
	// The arguments: the callee's `ret` is one i32, and an argument that crosses as a (payload, tag) pair is
	// two — the same gate `containerOperand`'s `*Call` arm asks before it lowers the call (**ADR 0333**'s
	// carriable question), asked here too so the DECISION and the BUILD cannot disagree.
	if !g.containerAnswerCallIsCarriable(c) {
		return 0, false
	}
	// The contents: a slot tagged `list` claims the object's slots can carry tags, and that claim belongs to
	// the arms the callee returns, not to this literal.
	if !g.containerAnswerContentsTaggable(a) {
		return 0, false
	}
	switch a.kind {
	case listAnswer:
		return int32(TagList), true
	case setAnswer:
		return int32(TagSet), true
	case dictAnswer:
		return int32(TagDict), true
	}
	// A tuple answer is declined, and declined ON PURPOSE. The object the callee built carries the tuple bit
	// in its own `@estr[h]` word (**ADR 0361**), and the slot that copies the handle has no road that reads
	// that bit through the OUTER list's printer: `xs = [t()]` printed `[1]` at exit 0 for the tuple the
	// reference prints as `[(1, 2)]`, the handle wearing the tag of the object it points at. A slot whose tag
	// the printer cannot honour is the wrong-answer class **ADR 0166** puts above any refusal, so the tuple
	// keeps **Gap R.67**'s refusal until the row that carries a tuple through a slot lands.
	return 0, false
}

// containerAnswerIsContainerSlotExpr is the gates' reading of the question above — does this element hold a
// container's handle — asked and answered once, so the folded global, the heap builder and the tag table all
// hear the same verdict about one element (**ADR 0249**'s gate-and-door pair).
func (g *irGen) containerAnswerIsContainerSlotExpr(e Expr) bool {
	_, ok := g.containerAnswerSlotIsContainer(e)
	return ok
}

// containerAnswerContentsTaggable asks whether every slot of the object the callee hands back can carry a
// tag, by asking the question of the expressions that `return` was built from — the arms the scan recorded,
// which are the arms that verdict was read from (**ADR 0232**'s rule that the slots the builder wrote are the
// truth about the object). It is deliberately NARROWER than "ask the same door again": an arm is admitted
// only as a spelling, because the element door is claiming what the object's slots hold and it can only claim
// what it can see.
//
// A `return inner()` is not a spelling. Measured on the first draft, which asked `taggableNestedElem` of
// every arm and so followed the forward: `print([f(), 7])` built a list whose slot 0 pointed at the list
// itself and printed `[` until the process died, and `print([1, 2] in [f()])` answered `False` where the
// reference prints `True`. The handle a forwarding body hands on belongs to the door the CALLEE stands at
// (**ADR 0333**'s forwarding road), not to this one, and an element that stores a handle it cannot describe is
// the wrong-answer class **ADR 0166** puts above any refusal. The forwarding shape stays with **Gap R.67**'s
// sentence and is filed as **Gap R.533**.
func (g *irGen) containerAnswerContentsTaggable(a containerAnswer) bool {
	if g == nil || len(a.arms) == 0 {
		return false
	}
	for _, arm := range a.arms {
		switch arm.(type) {
		case *ListLit, *SetLit, *DictLit:
			// The object's slots are the literal's elements: the same question the literal road asks of the
			// same contents, so a body and a binding of the same shape cannot disagree (**ADR 0249**).
			if !g.taggableNestedElem(arm) {
				return false
			}
		case *Call:
			// `return set()`, `return list()`, `return dict()` build the same object the empty literal spells
			// (**ADR 0333** reads them as the answer's kind); any other call is a forward, and declined.
			if _, isEmpty := g.emptyContainerCall(arm); !isEmpty {
				return false
			}
		default:
			// A name, a subscript, a comprehension, a format: the scan's slot table may follow these, and the
			// CALLER of this door may print the answer whole, but a slot label is a build-time claim and this
			// door makes it only about a shape it can read.
			return false
		}
	}
	return true
}
