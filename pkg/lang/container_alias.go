// container_alias.go is the notebook's half of the binding that copies a handle and nothing else.
//
// `ys = xs` is the most ordinary statement in the language, and this backend lowers it as what it is:
// the handle `xs` holds is stored into the slot `ys` holds, so the two names read ONE object. The
// object carries its own truth — every slot was written with its tag (ADR 0189), and bit 8 says the
// slots may be read (ADR 0232) — but the notebook does not: the claims that say what a container's slots
// hold are keyed by NAME — `mixedLists`, `listElemStr`, `listElemInt` for a list, `runtimeDicts`,
// `mixedDicts`, `dictKeyStr`/`dictKeyInt`/`dictValStr`/`dictValInt` for a dict, `runtimeSets`,
// `mixedSets`, `setElemStr`/`setElemInt` for a set — and the binding that shares an object shares none of
// them. So every road that asks the notebook before it asks the object asks it in silence about the
// alias, and answers from the payload alone (roadmap L11.1, Gap R.415 / Gap R.419, ADRs 0412 and 0413):
//
//	xs = [None, 1]                     xs = ["a", "b"]
//	ys = xs                            ys = xs
//	print(ys.pop(0))    # None / 0     print(ys.pop(0))   # a / 0
//
//	d = {"a": 1}   e = d   e["b"] = "x"
//	print(d)          # {'a': 1, 'b': 'x'} / {'a': 1, 'b': 1}   — the text's intern index, under a label
//
// Asked against the ORIGINAL name every one of those programs answers, which is what localises the defect
// to the NAME the read is written against rather than to the container — and why the fix belongs to the
// notebook that describes the object, not to the road that renders it.
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
// for each named after the failure it closes (roadmap L11.1, Gap R.415 / Gap R.419, ADRs 0412 and 0413;
// ADR 0232's promotion, ADR 0166's wrong-answer class).
//
// What this file does NOT do is open a door. An alias is a NAME, so a container METHOD over one —
// `e.items()`, `t.remove(1)` — is refused for the standing it has over the source name (Gap R.174), and
// the residue the rows measured beside themselves is filed rather than smoothed over: Gap R.420 (a
// rebinding releases what another name holds), Gap R.421 (a set's print order) and Gap R.422 (`discard`
// comparing the payload and not the pair).
package lang

import "strings"

// containerKindHeld answers which container the notebook says a name holds — the question the alias
// door asks before it copies anything. A name is a dict to the notebook when the binding that settled
// its kind said dict, or when a mixed record (the slots describe themselves) names it: either half is
// enough to make some door ask the record about it, and a door that asks is a door the alias must not
// answer from an empty record (roadmap Gap R.419, **ADR 0412**).
func (g *irGen) containerKindHeld(name string) string {
	switch {
	case name == "":
		return containerKindScalar
	case g.listVars[name] || g.mixedLists[name]:
		return containerKindList
	case g.runtimeDicts[name] || g.mixedDicts[name]:
		return containerKindDict
	case g.runtimeSets[name] || g.mixedSets[name]:
		return containerKindSet
	}
	return containerKindScalar
}

// recordContainerAlias notes that `to` names the object `from` names, for the binding that shares a
// handle (`ys = xs`). It is the alias door's only writer, and it is called from the one place that
// lowers an assignment, beside the door that settles the kind.
//
// All three containers are recorded (**Gap R.419**, **ADR 0412**). The list's half is what **ADR 0412**
// was measured on; the dict's and the set's are the same missing record one container over — `e = d` /
// `e["b"] = "x"` / `print(d)` rendered the text's intern index, and `t.add("a")` stored an untagged
// member — and the reason they are paid here rather than in a later commit is that the door is ONE door:
// the claims that travel are exactly the maps the container's own doors read.
func (g *irGen) recordContainerAlias(to, from string) {
	if to == "" || from == "" || to == from {
		return
	}
	c := g.containerClaimsOf(from)
	if c.kind == containerKindScalar {
		return
	}
	g.noteContainerAlias(to, from)
	g.writeContainerClaims(to, c)
}

// containerClaims is one name's whole container record: which kind the notebook says it holds, whether
// the slots describe themselves, and the single-kind claims the roads read before they ask the object.
// It is the SHAPE the alias door copies, named so that the two roads which copy it — `ys = xs` and an
// unpacking element that reads a handle out of a name's slot (**Gap R.527**) — take it from one reader
// rather than from two spellings of fifteen map fields (**ADR 0232**).
type containerClaims struct {
	kind string

	mixedList, elemStr, elemInt  bool
	dictMixed                    bool
	dictKeyStr, dictKeyInt       bool
	dictValStr, dictValInt       bool
	setMixed, setElemStr, setInt bool
}

// containerClaimsOf reads a name's record, following the shared-handle link when the name has no record
// of its own — the same question `listElementClaims` asks, asked once for every kind so that the copy an
// alias performs and the copy an unpacking performs cannot resolve the link differently (roadmap
// L11.1, Gap R.415, Gap R.527).
func (g *irGen) containerClaimsOf(name string) containerClaims {
	if k := g.containerKindHeld(name); k != containerKindScalar {
		return g.ownClaims(name, k)
	}
	if owner := g.containerAliasTarget(name); owner != name {
		if k := g.containerKindHeld(owner); k != containerKindScalar {
			return g.ownClaims(owner, k)
		}
	}
	return containerClaims{kind: containerKindScalar}
}

// ownClaims lifts the fifteen fields one name owns for one kind. The kind gates which fields are read,
// so a record for another kind cannot leak into the copy.
func (g *irGen) ownClaims(name, kind string) containerClaims {
	c := containerClaims{kind: kind}
	switch kind {
	case containerKindList:
		c.mixedList, c.elemStr, c.elemInt = g.mixedLists[name], g.listElemStr[name], g.listElemInt[name]
	case containerKindDict:
		c.dictMixed = g.mixedDicts[name]
		c.dictKeyStr, c.dictKeyInt = g.dictKeyStr[name], g.dictKeyInt[name]
		c.dictValStr, c.dictValInt = g.dictValStr[name], g.dictValInt[name]
	case containerKindSet:
		c.setMixed, c.setElemStr, c.setInt = g.mixedSets[name], g.setElemStr[name], g.setElemInt[name]
	}
	return c
}

// writeContainerClaims writes the record an alias door took from another name onto this one. The kind is
// settled by the caller's door (**Gap Q.1**), which owns the twelve claims this writes as its own set;
// writing the fields directly here is the copy **ADR 0412** measured as necessary — the mutation doors
// read the maps themselves, and a door handed an empty record for the alias stores an untagged payload
// into a tagged container.
func (g *irGen) writeContainerClaims(to string, c containerClaims) {
	switch c.kind {
	case containerKindList:
		g.mixedLists[to], g.listElemStr[to], g.listElemInt[to] = c.mixedList, c.elemStr, c.elemInt
	case containerKindDict:
		g.mixedDicts[to] = c.dictMixed
		g.dictKeyStr[to], g.dictKeyInt[to] = c.dictKeyStr, c.dictKeyInt
		g.dictValStr[to], g.dictValInt[to] = c.dictValStr, c.dictValInt
	case containerKindSet:
		g.mixedSets[to], g.setElemStr[to], g.setElemInt[to] = c.setMixed, c.setElemStr, c.setInt
	}
}

// settleUnpackedClaims is the unpacking's half of the same door (roadmap **Gap R.527**). An element that
// read a handle out of another name's slot binds the OBJECT, and the claims that make an object printable,
// measurable, iterable and comparable are keyed by NAME — so they have to arrive here too, or the receiving
// name is a number wearing another object's bits and the name it displaced keeps claiming a container it no
// longer holds (`a = [1]` / `b = 2` / `a, b = b, a` printing `[]` `0` where the reference prints `2` `[1]`).
//
// `c` is a SNAPSHOT taken before any of this statement's bindings landed: a swap reads a name this same
// statement is about to settle, and a claim read after the first settle describes the wrong object.
//
// `shared` says whether the new name may keep resolving into the source name's record — the plain road's
// `ys = xs` may, and an unpacking may NOT when the same statement re-binds the source, because the link
// would be a circle this statement created and the object it describes has moved.
func (g *irGen) settleUnpackedClaims(b *strings.Builder, to, from string, c containerClaims, shared bool) {
	if to == "" {
		return
	}
	if c.kind == containerKindScalar {
		// Nothing was carried: the binding ends the container-hood the name had (**ADR 0172**'s
		// latest-binding rule, **Gap Q.1**'s door), so no later read of it is measured as a container read.
		if g.containerKindHeld(to) != containerKindScalar {
			g.settleContainerKind(to, containerKindScalar)
		}
		return
	}
	g.settleContainerKind(to, c.kind)
	g.writeContainerClaims(to, c)
	if shared && from != "" && from != to {
		g.noteContainerAlias(to, from)
	}
	// The payload the store wrote is a heap handle, and a handle that is not a root is one collection away
	// from the segfault ADR 0181's rooting exists to prevent.
	g.gcReg(b, to)
}

// noteContainerAlias keeps the link that says which name's record describes the shared object. The
// claims TRAVEL at the binding, and the link is kept so that a later promotion reaches every name for
// the same object. Copying alone would freeze the kind the container had (**ADR 0232**'s promotion is
// exactly the event that makes a frozen claim a lie), and the link alone would leave the roads that read
// the maps directly — the mutation doors, which decide whether an incoming element needs a tag written
// with it — reading an empty record for the alias and storing an untagged payload in a tagged container.
// That pair of half-measures is why the two live together here (roadmap Gap R.415, **ADR 0412**).
func (g *irGen) noteContainerAlias(to, from string) {
	if g.containerAlias == nil {
		g.containerAlias = map[string]string{}
	}
	g.containerAlias[to] = from
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

// markSetMixed and markDictMixed are the same event told to the same readers, one container over
// (roadmap **Gap R.419**, **ADR 0412**). `s.add("a")` written against one name and `d[k] = v` written
// against another are each ONE event on ONE object, and a notebook that cleared the single-kind claim on
// the mutated name alone left the OTHER name rendering every slot through a table the object had outgrown
// — which is how `d = {"a": 1}` / `e = d` / `e["b"] = "x"` / `print(d)` printed `{'a': 1, 'b': 1}`: the
// text's intern index, under the label `d`'s stale record still claimed.
func (g *irGen) markSetMixed(name string) {
	for _, nm := range g.containerSharers(name) {
		g.mixedSets[nm] = true
		g.setElemStr[nm] = false
		g.setElemInt[nm] = false
	}
}

func (g *irGen) markDictMixed(name string) {
	for _, nm := range g.containerSharers(name) {
		g.mixedDicts[nm] = true
		g.dictKeyStr[nm] = false
		g.dictKeyInt[nm] = false
		g.dictValStr[nm] = false
		g.dictValInt[nm] = false
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
