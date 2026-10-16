package lang

import "fmt"

// The compiler's half of the immutable container (roadmap L11.3, ADR 0361).
//
// Two facts about a tuple matter to codegen, and each lives where it can be asked honestly:
//
//   - the OBJECT carries "I am a tuple" in its `@estr[h]` bit (tuple_runtime.go), because a value
//     printed, compared or mutated later may be reached through a name, a slot or a call answer
//     that never saw the parentheses (ADR 0251's rule, applied to a new container);
//   - the EXPRESSION carries it in the node's `Tuple` flag, which is where the roads that build a
//     container, and the print road that has to choose a printer for a name, ask it.
//
// What codegen deliberately does NOT do is guess a tuple from an element type or from a name's
// spelling: a name's tuple-ness is recorded at the statement that binds the tuple, and retired by
// the statement that binds anything else, which is ADR 0172's latest-binding rule with one more
// attribute on the binding.

// noteTuple records that this module builds at least one tuple, which is what pulls the immutable
// container's runtime block into the module (ADR 0173: a referenced internal function that was
// never emitted is the module `llc` rejects).
func (g *irGen) noteTuple(ln *ListLit) {
	if ln.IsTuple() {
		g.tupleUsed = true
	}
}

// recordTupleBinding settles what the name knows about its latest binding: a name bound to a tuple
// prints with parentheses, and a name bound to anything else stops saying so. The rule is the same
// one that retires a value's tag word (Gap R.142), asked of one more attribute of the binding.
func (g *irGen) recordTupleBinding(name string, e Expr) {
	if g.tupleVars == nil {
		g.tupleVars = map[string]bool{}
	}
	if _, isTuple := AsTupleList(e); isTuple {
		g.tupleVars[name] = true
		g.tupleUsed = true
		return
	}
	delete(g.tupleVars, name)
}

// recordTupleClaim is the binding road's half of the fact, for the case where the claim does not come
// from a source expression: a name holding the answer a tuple-returning callee handed back was never
// written with parentheses by anybody who read them, so the fact has nowhere to live but the notebook
// (roadmap L11.3, ADR 0361).
func (g *irGen) recordTupleClaim(name string) {
	if g.tupleVars == nil {
		g.tupleVars = map[string]bool{}
	}
	g.tupleVars[name] = true
	g.tupleUsed = true
}

// isTupleExpr is the one question the roads ask: does this expression answer a tuple? A literal
// says so from its node; a name says so from the statement that bound it; a folded comprehension
// that came from a tuple literal says so from the literal the fold kept.
func (g *irGen) isTupleExpr(e Expr) bool {
	switch t := e.(type) {
	case *ListLit:
		return t.IsTuple()
	case *Tuple:
		// The pattern spelling of the same value: a `*Tuple` node is a tuple whatever road it
		// arrived on (roadmap L11.3, ADR 0361).
		return true
	case *Name:
		if g.tupleVars[t.Value] {
			return true
		}
		if lit, has := g.containerLits[t.Value]; has {
			// The name still describes the literal this module folded: the fold kept the spelling,
			// so the spelling answers the question (roadmap L11.3, ADR 0361).
			_, isTuple := AsTupleList(lit)
			return isTuple
		}
		return false
	}
	return false
}

// receiverIsTuple asks the mutation doors one question about the value a call or an item assignment
// would write into: is this the immutable spelling? A literal answers from its node, a name from the
// statement that bound it. The answer is deliberately asked of the COMPILER here rather than of the
// object at run time, because the refusal must stop the build: a runtime no-op would keep the program
// running on a value the reference never produces (roadmap L11.3, ADR 0361).
func (g *irGen) receiverIsTuple(e Expr) bool { return g.isTupleExpr(e) }

// refusesTupleMutation is the honest sentence for writing into a value the language promises never
// changes. It is a front-end refusal rather than a silent write because the compiled backend has one
// container layout, and a road that did not ask the tuple question would otherwise append to a tuple
// happily — an answer the reference refuses with the class and sentence quoted here (roadmap L11.3,
// ADR 0361; the raise that would make this catchable is the filed half of the row).
func refusesTupleMutation(what, cls, sentence string) error {
	return fmt.Errorf("codegen: %s would write into a tuple, which is the immutable container: the reference raises %s: %s, and this backend builds the tuple with the layout a list mutates, so the door refuses rather than answering a mutated tuple — no slot is written and no element is added (roadmap L11.3, ADR 0361, immutable container)", what, cls, sentence)
}

// elemIsContainer asks whether one element of a container literal is itself a container — the
// question the sort comparator has to ask before it claims an order.
func elemIsContainer(e Expr) bool {
	switch e.(type) {
	case *ListLit, *DictLit, *SetLit, *Tuple:
		return true
	}
	return false
}

// sortElementsAreContainers answers whether a container literal's elements have an order at all.
//
// @rt_sort compares by payload under one mode per kind: numbers by the number, interned texts by
// strcmp of the text (ADR 0191). A container has no payload-order — its payload is a heap SLOT —
// so sorting [(2,1),(1,2)] by slot compared the order the tuples ARRIVED in and printed the input
// back at exit 0. That is the silent near-wrong answer ADR 0166 forbids, and it is why the door
// asks this question before it claims an order. The reference compares containers element by
// element, shorter-is-less on a tie; the door that would answer it here is the filed row
// (roadmap Gap R.309), not a payload compare dressed up as one.
func sortElementsAreContainers(n *ListLit) bool {
	if n == nil {
		return false
	}
	for _, el := range n.Elems {
		if elemIsContainer(el) {
			return true
		}
	}
	return false
}

// sortElementsAreContainersLit accepts either spelling of a container literal: the ListLit a tuple
// is spelled with, or the folded literal a name still carries.
func sortElementsAreContainersLit(n Expr) bool {
	switch v := n.(type) {
	case *ListLit:
		return sortElementsAreContainers(v)
	case *Tuple:
		for _, el := range v.Elems {
			if elemIsContainer(el) {
				return true
			}
		}
	}
	return false
}

func errSortElementsHaveNoOrder() error {
	return fmt.Errorf("codegen: sorting needs an order for the elements and these elements are containers, whose payload is a heap slot rather than a value: the answer would be the order they arrived in, printed as if it were an order (roadmap L11.3, Gap R.309, ADR 0166). The reference compares containers element by element; the door that answers that is the filed gap — no sort is emitted")
}
