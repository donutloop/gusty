package lang

// =============================================================================
// A variable's container kind is ONE fact, written in fourteen places
// (roadmap Gap Q.1, L11.1)
//
// `d = {1: 10}` then `d = [3, 4]` is two programs to the object and one program to
// the reader. The assignment frees the old object, allocates a new one and stores the
// new handle — and then the read asks a notebook the compiler kept *by name*, and the
// notebook still says `dict`. `print(d)` walks the dict printer over a list's bytes and
// writes `{3: 4, 0: 0}`; `print(d[0])` hashes a key the reference asks by position and
// writes `0`; both at the exit code that means the program ran. That is the priority-1
// class: a fabricated number where the reference has an answer, at exit 0.
//
// The facts live in fourteen name-keyed tables — three kind claims (`listVars`,
// `runtimeDicts`, `runtimeSets`), three "my slots describe themselves" bits (`mixedLists`,
// `mixedDicts`, `mixedSets`) and eight slot-kind notebooks (`listElemStr`/`listElemInt`,
// `setElemStr`/`setElemInt`, `dictKeyStr`/`dictKeyInt`, `dictValStr`/`dictValInt`) — and
// every binding road that hands a name a container has been clearing its own three
// neighbours ever since: the set-literal road clears the list and dict claims, the dict
// road clears the list and set claims, the comprehension road clears all three plus
// `mixedLists`, the non-container road clears the three claims, and the list-literal road
// — the most-used container binding in the language — clears **nothing at all**, which is
// why exactly one direction of this row's six transitions answered.
//
// Six roads deciding the same fact six ways is the bug. This file is the seventh road
// and the only one that decides: `settleContainerKind` is called by every binding that
// gives a name a container (and by the one that takes it away), and it settles all fourteen
// claims for that name in one place — the eleven of them that belong to the kinds this binding is
// not (including all three "my slots describe themselves" bits), the claim of the kind it is, and the
// fold record only when the kind changed — from the binding that is happening *now* — the same
// latest-binding rule ADR 0172 wrote for `None`, that ADR 0257 extended to `bool`, that
// ADR 0267 used for a pair's tag, and that ADR 0270 used for every status a container
// rebinding retires (Gap R.142, Gap R.145). This row is what those rows left standing:
// "the container kinds stay with their own doors".
//
// Two rules keep the door from becoming a broom:
//
//   - It clears what **disagrees** with the binding in hand, not what happens to be
//     there. A claim of the same kind survives, because the binding road about to run is
//     going to re-record it — and a name bound once to one literal keeps the fold record
//     ADR 0241 licensed slot reads from, which a blanket delete would silently take away.
//   - It decides nothing else. The tag is `forgetTaggedBinding`'s, the interned-text claim
//     is ADR 0270's, `None` and `bool` are ADR 0172's. A door that retires the facts it
//     was not asked about is how one row quietly re-decides another.
//
// The gate is pure: it writes notebook entries and emits no instruction, evaluates no
// expression and advances no counter (ADR 0273's rule for a question asked before a road
// is chosen).
// =============================================================================

// The two fold-record tables a name can hold (`containerLits`, and the `dictVals` table the
// dict-read road still consults) are cleared only when they describe an object this binding
// replaced; `dictVals` is written nowhere today, so the door does not pretend to clear it.

// containerKindList, containerKindDict and containerKindSet are the three spellings a
// container kind travels under. They are the same strings `staticContainerKind` answers,
// so the notebook this door writes and the question that reads it speak one vocabulary.
const (
	containerKindList   = "list"
	containerKindDict   = "dict"
	containerKindSet    = "set"
	containerKindScalar = ""
)

// settleContainerKindFromLiteral is the door for the binding that REEBUILDS the object from a
// literal: `xs = ["a"]` then `xs = [1, 2]`, `d = {"a": 1}` then `d = {2: 3}`.
//
// The difference from settleContainerKind is one question — is the element-kind notebook a fact about
// THIS object? — and for a literal binding the answer is no: every slot is written again from the
// elements in hand, so the notebook the name carried from the previous object describes bytes that were
// freed by this statement. Leaving it is not neutral: `recordElemKind` reads the opposite notebook before
// it records, and a stale `listElemStr` turns `xs = ["a"]` / `xs = [1, 2]` into the refusal "a compiled
// list holds either strings or numbers, not both" (Gap J.6's sentence) on a program whose two literals
// hold one kind each. The reference prints `[1, 2]`; the row's own ranking puts the answer first.
func (g *irGen) settleContainerKindFromLiteral(name string, kind string) {
	g.settleContainerKind(name, kind)
	switch kind {
	case containerKindList:
		g.listElemStr[name] = false
		g.listElemInt[name] = false
	case containerKindDict:
		g.dictKeyStr[name] = false
		g.dictKeyInt[name] = false
		g.dictValStr[name] = false
		g.dictValInt[name] = false
	case containerKindSet:
		g.setElemStr[name] = false
		g.setElemInt[name] = false
	}
}

// containerKindOfLiteral names the kind a fold record describes, so the door can ask
// whether a literal still standing in `containerLits` is the object this binding wrote.
func containerKindOfLiteral(e Expr) string {
	switch e.(type) {
	case *ListLit:
		return containerKindList
	case *DictLit:
		return containerKindDict
	case *SetLit:
		return containerKindSet
	}
	return containerKindScalar
}

// settleContainerKind writes every container fact the name holds, from the binding that
// is happening now. `kind` is containerKindList, containerKindDict, containerKindSet, or
// containerKindScalar for a binding that hands the name something that is not a container
// at all — the object the name held is freed by that assignment, so no read of the name
// may still be measured as a container read.
//
// A claim of the kind being bound is left in place for the caller to re-record: the
// list-literal road, for example, settles on `list` and then records `mixedLists` itself
// once it has asked whether its elements describe themselves. The "my slots describe
// themselves" bits are NOT left standing, not even for the kind being bound: every road that
// builds an object writes its own mixed bit only in the branch where the slots carry tags, and
// none of them writes `false`. A name that was a mixed list and is rebound to a homogeneous
// literal would otherwise keep walking the per-slot-tag readers over tags nobody wrote — the
// right answer for the wrong reason, which is the state ADR 0233 forbids.
// allElemsPrintAsText asks whether every element of the container an ordering builtin was handed is a
// TEXT — the same question the literal path asks of its own brackets, asked here of the candidates the
// door copied into a heap. It is a claim, not a guess: one element that is not a text and the answer is
// no, which leaves the container on the mixed road that can render every kind (ADR 0348's rule; the
// print that showed the intern table's numbers instead of the texts is roadmap Gap R.264, L11.7).
func (g *irGen) allElemsPrintAsText(elems []Expr) bool {
	if len(elems) == 0 {
		return false
	}
	for _, el := range elems {
		if kw, isKw := el.(*KeywordArg); isKw {
			el = kw.Value
		}
		if !g.exprIsString(el) {
			return false
		}
	}
	return true
}

func (g *irGen) settleContainerKind(name string, kind string) {
	g.mixedLists[name] = false
	g.mixedDicts[name] = false
	g.mixedSets[name] = false
	if kind != containerKindList {
		g.listVars[name] = false
		g.listElemStr[name] = false
		g.listElemInt[name] = false
	}
	if kind != containerKindDict {
		g.runtimeDicts[name] = false
		g.dictKeyStr[name] = false
		g.dictKeyInt[name] = false
		g.dictValStr[name] = false
		g.dictValInt[name] = false
	}
	if kind != containerKindSet {
		g.runtimeSets[name] = false
		g.setElemStr[name] = false
		g.setElemInt[name] = false
	}
	switch kind {
	case containerKindList:
		g.listVars[name] = true
	case containerKindDict:
		g.runtimeDicts[name] = true
	case containerKindSet:
		g.runtimeSets[name] = true
	}
	// A fold record naming another kind's literal is a claim about an object this
	// assignment freed. One naming THIS kind may still be the binding the scan licensed a
	// slot read from (ADR 0241), so it is only taken away when the binding is not a
	// container or is a different one.
	if lit, ok := g.containerLits[name]; ok && containerKindOfLiteral(lit) != kind {
		delete(g.containerLits, name)
	}
}
