// container_alias.go is the notebook's half of the binding that copies a handle and nothing else.
//
// `ys = xs` is the most ordinary statement in the language, and this backend lowers it as what it is:
// the handle `xs` holds is stored into the slot `ys` holds, so the two names read ONE object. The
// object carries its own truth — every slot was written with its tag (ADR 0189), and bit 8 says the
// slots may be read (ADR 0232) — but the notebook does not: `mixedLists`, `listElemStr` and
// `listElemInt` are keyed by NAME, and the binding that shares an object shares none of those three
// claims. So every road that asks the notebook before it asks the object asks it in silence about the
// alias, and answers from the payload alone (roadmap L11.1, Gap R.415, ADR 0412):
//
//	xs = [None, 1]                     xs = ["a", "b"]
//	ys = xs                            ys = xs
//	print(ys.pop(0))    # None / 0     print(ys.pop(0))   # a / 0
//
// Asked against the ORIGINAL name both programs answer, which is what localises the defect to the NAME
// the read is written against rather than to the list — and why the fix belongs to the notebook that
// describes the object, not to the road that renders it.
//
// The record travels with the handle in THREE places, and the row's finding is that all three are needed:
//
//   - the claims are COPIED at the binding, because the roads that decide whether an incoming element
//     needs a tag written with it read the maps directly, and a door handed an empty record for the alias
//     stores an untagged payload into a tagged container — which is how `xs = [1, 2]` / `ys = xs` /
//     `ys.append("a")` / `print(xs.pop(2))` reached the printer with a text slot nobody had labelled and
//     died on a segmentation fault (ADR 0166's artifact-at-fault class, reproduced on the parent build);
//   - the link is KEPT, because a claim frozen at the binding is a claim about the moment of the binding:
//     `xs = []` / `ys = xs` / `xs.append("a")` records a kind on `xs` that the alias never had, and the
//     roads that ask the record at READ time must ask the record of the object, not of the name;
//   - the PROMOTION is told to every name that holds the object, because a promotion is one event on one
//     object wherever the source writes it. `xs.append("a")` contradicts the kind recorded for the list,
//     and a notebook that told only `xs` would leave `ys` claiming integers about a list whose next slot
//     is a text — the stale-copy bug the copy above exists to avoid, arriving from the other direction.
//
// Any two of the three is a wrong answer at the exit code of SUCCESS or worse; the third is what the
// remaining shape is waiting for. That is why they live in one file, beside one another, with the tests
// for each named after the failure it closes (roadmap L11.1, Gap R.415, ADR 0412; ADR 0232's promotion,
// ADR 0166's wrong-answer class).
package lang

// recordContainerAlias notes that `to` names the object `from` names, for the binding that shares a
// handle (`ys = xs`). It is the alias door's only writer, and it is called from the one place that
// lowers an assignment, beside the door that settles the kind.
//
// Only a list is recorded today: the element-kind claims that make a read ask the object are the list's
// three, and the door this row pays (`popAnswerShape`) is a list door. A dict's or a set's alias is the
// SAME missing record one container over, and it answers wrong (a dict's value written through the alias
// renders the intern index; a set element added through it crashes) rather than declining — filed as
// roadmap **Gap R.419** with the bytes beside it, because recording the kinds whose claims nobody asks
// would be a claim with no reader and paying a door nobody has opened is not how this row works.
func (g *irGen) recordContainerAlias(to, from string) {
	if to == "" || from == "" || to == from {
		return
	}
	if !g.listVars[from] {
		return // the alias names an object no notebook calls a list: nothing to follow
	}
	if g.containerAlias == nil {
		g.containerAlias = map[string]string{}
	}
	g.containerAlias[to] = from
	// The three claims TRAVEL at the binding, and the link is kept so that a later promotion reaches both
	// names. Copying alone would freeze the kind the list had (ADR 0232's promotion is exactly the event
	// that makes a frozen claim a lie), and the link alone would leave the roads that read the maps
	// directly — the mutation doors, which decide whether an incoming element needs a tag written with it
	// — reading an empty record for the alias and storing an untagged payload in a tagged container.
	// That pair of half-measures is why the two live together here (roadmap Gap R.415, ADR 0412).
	g.mixedLists[to] = g.mixedLists[from]
	g.listElemStr[to] = g.listElemStr[from]
	g.listElemInt[to] = g.listElemInt[from]
}

// forgetContainerAlias drops a name's own link, and every link that LEADS through it: the binding that
// hands a name a different object ends the object's readers, so a read of a name that used to be an
// alias may not resolve into a record that now describes something else.
func (g *irGen) forgetContainerAlias(name string) {
	if len(g.containerAlias) == 0 {
		return
	}
	delete(g.containerAlias, name)
	for alias, src := range g.containerAlias {
		if src == name {
			delete(g.containerAlias, alias)
		}
	}
}

// containerAliasTarget answers "which name's record describes the object this name holds?", following
// the shared-handle bindings to the name that was actually built. It stops at the first name with a
// record to ask, at the end of the chain, or at a cycle — `a = b` above `b = a` is a program, not a
// proof, and a reader that looped on it would be the compiler's fault rather than the program's.
func (g *irGen) containerAliasTarget(name string) string {
	if len(g.containerAlias) == 0 {
		return name
	}
	seen := map[string]bool{name: true}
	for {
		src, aliased := g.containerAlias[name]
		if !aliased || seen[src] {
			return name
		}
		seen[src] = true
		name = src
	}
}

// listElementClaims answers the two questions a read of a list's ELEMENT asks of the record: do the
// slots describe themselves, and if not, which single kind does the record claim they hold. The name's
// OWN record is asked first — the binding copied the object's claims onto every name that holds it, and a
// promotion writes there — and only a name with nothing recorded at all follows the shared-handle link,
// which is the case the binding could not have known: the kind the source list was given AFTER the alias
// was written (roadmap L11.1, Gap R.415, ADR 0412).
//
// Asking the link at all is what made `print(ys.pop(0))` answer `0` beside `print(xs.pop(0))`'s `None`
// and `for v in ys` print the void's payload where `for v in xs` printed `None`: those roads ask the
// record before they ask the object, and the record they asked had never been written.
//
// `ok=false` is the third answer, and it is a decline: no record describes these slots, and a door that
// guessed a kind is how a payload gets rendered under a label the object never gave it (ADR 0166).
func (g *irGen) listElementClaims(name string) (mixed, elemStr, elemInt, ok bool) {
	if mixed, elemStr, elemInt = g.mixedLists[name], g.listElemStr[name], g.listElemInt[name]; mixed || elemStr || elemInt {
		return mixed, elemStr, elemInt, true
	}
	owner := g.containerAliasTarget(name)
	mixed, elemStr, elemInt = g.mixedLists[owner], g.listElemStr[owner], g.listElemInt[owner]
	return mixed, elemStr, elemInt, mixed || elemStr || elemInt
}

// mixedListRead is the same lookup for the roads that ask only the first question — `for v in ys` and
// `print(ys[i])` both decide from "do the slots speak?", and both were answering for the alias from a
// record nobody wrote. It is one lookup shared by those roads and this file's door, not a second gate
// over one fact (ADR 0249's rule that a gate and a door are asked in the same place).
func (g *irGen) mixedListRead(name string) bool {
	mixed, _, _, _ := g.listElementClaims(name)
	return mixed
}

// markListMixed is the promotion (ADR 0232) told to EVERY name that holds the object: a name whose slots
// speak stops claiming one element kind, and an alias holding the same list is telling the same truth
// whether the mutation was written against it or against the name that built the object.
//
// This is the half that keeps the copy at the binding honest. A claim copied and then left alone is a
// claim about the moment of the binding, and `xs.append("a")` is an event that happened after it; a
// promotion told only to the mutated name leaves the OTHER name claiming integers about a list whose next
// slot is a text. Neither half-measure survives a program, which is why the walk over the sharers is here
// and not in the callers (roadmap Gap R.415, ADR 0412).
func (g *irGen) markListMixed(name string) {
	for _, nm := range g.containerSharers(name) {
		g.mixedLists[nm] = true
		g.listElemStr[nm] = false
		g.listElemInt[nm] = false
	}
}

// containerSharers names every name the program holds this object under, following the shared-handle
// bindings in both directions: the name that was built, the name it was bound from, and every alias
// bound from either. The walk is cycle-guarded — `a = b` above `b = a` is a program, and an infinite
// walk is the compiler's fault rather than the program's (ADR 0166).
func (g *irGen) containerSharers(name string) []string {
	if name == "" {
		return nil
	}
	if len(g.containerAlias) == 0 {
		return []string{name}
	}
	out := []string{name}
	seen := map[string]bool{name: true}
	queue := []string{name}
	for len(queue) > 0 {
		cur := queue[0]
		queue = queue[1:]
		if src, aliased := g.containerAlias[cur]; aliased && !seen[src] {
			seen[src] = true
			out = append(out, src)
			queue = append(queue, src)
		}
		for alias, src := range g.containerAlias {
			if src != cur || seen[alias] {
				continue
			}
			seen[alias] = true
			out = append(out, alias)
			queue = append(queue, alias)
		}
	}
	return out
}
