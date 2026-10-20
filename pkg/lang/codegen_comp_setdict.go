// pkg/lang/codegen_comp_setdict.go — the set and dict comprehension roads grow the runtime builder
// the list road has had since ADR 0192 (roadmap Gap R.353, ADR 0384).
//
// A set or dict comprehension has had one path: fold the element (and, for a dict, the key and the
// value) under each item and emit the compile-time global the container means. That is the right fast
// path — `print({x: x*2 for x in [1,2]})` is a literal by another spelling, and ADR 0234's rule that a
// comprehension which folds IS the literal it folds to depends on it — but it is the only path, so every
// element the folder cannot see came back as a refusal:
//
//	print({(lambda z: z + x)(1) for x in [10, 20]})   // CPython {11, 21}   here: "element must be constant"
//	print({x: f(x)                for x in [1, 2]})   // CPython {1: 2, …}  here: "value must be constant"
//
// while the list-shaped twin of both has answered since Gap R.348, because the list road grew a runtime
// arm (`runtimeCompList`) that evaluates the element once per item and writes the (payload, tag) pair.
// The same asymmetry produced two silently-wrong answers, which is the class this repo files rather than
// ships: `{None for x in [1]}` printed `{0}` and `{1: None for x in [1]}` printed `{1: 0}` — the void
// folds to the integer 0, which is the right answer to `if None:` and the wrong answer to "what is in
// this slot" (ADR 0244's rule, one construct over).
//
// Two questions are asked here, and they are the two the list road asks:
//
//   - `compOperandsFold` — can the fold answer every operand for every item? It runs the fold the fold
//     will run (a probe that lies would build the container twice and call whichever failed an error),
//     and it asks `foldsToAnInteger`/`anyItemIsAVerdict` first, because a folded container is a global
//     with no tag table to write — a verdict, a float, a text, the void or a container operand belongs
//     to the builder beside every other value that needs its kind said (ADR 0259, ADR 0244, ADR 0232);
//   - `runtimeCompSetDict` — build the heap object one member or entry at a time, binding each clause's
//     loop variable to the item the way `for` binds it (payload slot and tag slot together, ADR 0185) and
//     writing through the doors `s.add(v)` and `d[k] = v` use (`rt_set_add_tagged`, `rt_dict_put_tagged`,
//     ADR 0310/ADR 0311). A member and a key also ask the question no element asks — can this value be
//     hashed — and only the tag can answer it, which is why an unhashable member raises the reference's
//     `TypeError: unhashable type: 'list'` from the write rather than landing a handle in a bucket.
//
// The fold keeps priority: a program the folder can see is the same global it always was, emitted by the
// same code, with the same print dispatch. Nothing here widens what folds.
package lang

import (
	"fmt"
	"strconv"
	"strings"
)

// setCompFolds and dictCompFolds ask whether the constant path can evaluate every operand the
// container writes for every item. They are `comprehensionFolds` wearing the set's or the dict's
// clothes, and they are the gate that decides fold-or-build — so they must never disagree with the
// fold they are guarding, which is why they run the fold rather than pattern-match it (roadmap Gap R.353).
func (g *irGen) setCompFolds(c *Comp, items []int64, itemExprs []Expr) bool {
	if len(c.Elems) != 1 {
		return false
	}
	return g.compOperandsFold(c, items, itemExprs, c.Elems[0])
}

func (g *irGen) dictCompFolds(c *Comp, items []int64, itemExprs []Expr) bool {
	if len(c.Keys) != 1 || len(c.Vals) != 1 {
		return false
	}
	return g.compOperandsFold(c, items, itemExprs, c.Keys[0], c.Vals[0])
}

// compOperandsFold folds each operand under each item, plus the clause's filter once per item. An
// operand that is not a compile-time integer, or that is only a copy of a loop variable the item bound
// to a verdict, is not a fold at all: it is a value whose kind lives in the tag the global cannot
// carry (ADR 0259, ADR 0244).
func (g *irGen) compOperandsFold(c *Comp, items []int64, itemExprs []Expr, operands ...Expr) bool {
	if g.constBindings == nil {
		g.constBindings = map[string]int64{}
	}
	if c.ForVar == nil {
		return false
	}
	for _, e := range operands {
		if e == nil {
			continue
		}
		if !g.foldsToAnInteger(e) {
			return false
		}
		if g.anyItemIsAVerdict(e, c.ForVar, itemExprs) {
			return false
		}
	}
	for _, item := range items {
		g.constBindings[c.ForVar.Value] = item
		ok := true
		for _, e := range operands {
			if e == nil {
				continue
			}
			if _, fok := g.foldConstInt(e); !fok {
				ok = false
				break
			}
		}
		if ok && c.Cond != nil {
			// The filter is as un-foldable as an element that is not, and a fold that skipped the check
			// would refuse with "condition must be constant" beside an element it could have built.
			if _, cok := g.foldConstInt(c.Cond); !cok {
				ok = false
			}
		}
		delete(g.constBindings, c.ForVar.Value)
		if !ok {
			return false
		}
	}
	return true
}

// compOperandTagKind classifies one comprehension operand for the object that is about to store it.
// The fold asks the same question of the AST and then writes a global with no tags; the builder writes
// tags, but still has to know before it starts whether the object has one kind or must let each slot
// speak for itself. A loop variable is only ever as good as the item it copied, and an operand whose
// kind the AST does not name is deliberately `dynamic`: the tag register says what the compiler cannot,
// so the object is asked to read it (roadmap Gap R.353, ADR 0232, ADR 0244, ADR 0259).
func (g *irGen) compOperandTagKind(e Expr, c *Comp, lv string, item Expr) (tag int32, dynamic bool, ok bool) {
	if e == nil {
		return 0, true, false
	}
	if nm, isName := e.(*Name); isName && c != nil && c.ForVar != nil && nm.Value == c.ForVar.Value {
		if g.compElemCopiesABool(e, lv, item) {
			if t, hasTag := g.elemKindTag(item); hasTag {
				return t, false, true
			}
			return 0, true, false
		}
		if g.internedVars != nil && g.internedVars[nm.Value] {
			return int32(TagStr), false, true
		}
		if t, hasTag := g.elemKindTag(e); hasTag {
			return t, false, true
		}
		if t, hasTag := g.elemKindTag(item); hasTag {
			return t, false, true
		}
		return 0, true, false
	}
	if t, hasTag := g.elemKindTag(e); hasTag {
		return t, false, true
	}
	return 0, true, false
}

// runtimeCompNeedsSelfDescribingSlots asks, before lowering the members or entries, whether a set or
// dict built by `runtimeCompSetDict` has to set bit 8. A slot whose payload is a handle, a float box,
// None or a verdict cannot speak for itself; neither can an object that stores text beside anything
// else, because there is no one element kind for the printer to assume. If any write arrives through a
// tag register the compiler carried but never named, the object must ask that tag too (ADR 0232,
// ADR 0244, ADR 0259; roadmap Gap R.353).
func (g *irGen) runtimeCompNeedsSelfDescribingSlots(c *Comp, operands []Expr, lv string, itemExprs []Expr) bool {
	if c == nil || len(operands) == 0 {
		return false
	}
	// A filter is evaluated per item, so an operand that only survives on one item can still be the
	// operand that makes this object mixed. Conservatively asking every item is therefore sound.
	sawStr, sawOther := false, false
	for _, item := range itemExprs {
		for _, operand := range operands {
			if g.nestedContainerElem(operand) {
				return true
			}
			t, dynamic, ok := g.compOperandTagKind(operand, c, lv, item)
			if !ok || dynamic || slotTagSelfDescribing(t) {
				return true
			}
			if t == int32(TagStr) {
				sawStr = true
			} else {
				sawOther = true
			}
		}
	}
	return sawStr && sawOther
}

// runtimeCompSetDict builds a set or dict comprehension whose operands are not constants: each operand
// is evaluated once per item and written to the heap object with the tag that says what it is. The loop
// is unrolled, as everything else in this backend is, so the iterable still has to be a shape the
// compiler can enumerate even though the members no longer have to be known (roadmap Gap R.353, beside
// ADR 0192's list road and ADR 0310's tagged entry and member).
func (g *irGen) runtimeCompSetDict(b *strings.Builder, c *Comp, itemExprs []Expr) (string, error) {
	var operands []Expr
	kind := HeapSet
	switch c.Kind {
	case CompSet:
		if len(c.Elems) != 1 {
			return "", fmt.Errorf("codegen: a runtime set comprehension builds one member at a time (roadmap L11.1)")
		}
		operands = []Expr{c.Elems[0]}
	case CompDict:
		if len(c.Keys) != 1 || len(c.Vals) != 1 {
			return "", fmt.Errorf("codegen: a runtime dict comprehension builds one entry at a time (roadmap L11.1)")
		}
		kind = HeapDict
		operands = []Expr{c.Keys[0], c.Vals[0]}
	default:
		return "", fmt.Errorf("codegen: runtimeCompSetDict asked for comprehension kind %d (roadmap Gap R.353)", c.Kind)
	}
	// A member or an entry that is itself a container has to be storable, printable and hashable by
	// content, which needs every element one level in to be taggable too — asked, and answered in words,
	// by the same door the list road and the runtime loop ask (ADR 0232).
	for _, el := range operands {
		if g.nestedContainerElem(el) && !g.taggableNestedElem(el) {
			return "", nestedContainerErr(el)
		}
	}
	lv := loopVarName(c.ForVar)
	if !g.allocd[lv] {
		b.WriteString(fmt.Sprintf("  %%_%s = alloca i32\n", lv))
		g.gcReg(b, lv)
		g.allocd[lv] = true
	}
	// The target is a binding of this frame and says so, for the element, the filter and every road that
	// asks a frame what it owns — the same record `for` and the list road keep (ADR 0381).
	g.noteFrameTarget(lv)
	defer g.forgetFrameTarget(lv)
	g.heapUsed = true
	// Iterating a container of texts binds the loop variable to an index into @str_tab, and saying so is
	// what makes a member print as text rather than as the index (the `for` road records the same fact).
	if ll, ok := c.Iter.(*ListLit); ok && len(ll.Elems) > 0 {
		if _, isStr := ll.Elems[0].(*StrLit); isStr {
			if g.internedVars == nil {
				g.internedVars = map[string]bool{}
			}
			g.internedVars[lv] = true
		}
	}
	g.heapSeq++
	h := fmt.Sprintf("%%h%d", g.heapSeq)
	g.emitRtAllocKind(b, h, kind, c.Span())
	if g.runtimeCompNeedsSelfDescribingSlots(c, operands, lv, itemExprs) {
		b.WriteString(fmt.Sprintf("  call void @rt_mark_estr(i32 %s, i32 8)\n", h))
	}

	// addMember / putEntry write the operand(s) with their tags, through the doors the mutation
	// statements use. Payload and tag are one call each: an add that wrote only the payload leaves the
	// new slot tagged as whatever held it before, and an operand lowered through `g.value` alone reaches
	// a container slot as the compiler's global — the value position `llc` rejects (ADR 0187, ADR 0244).
	addMember := func(item Expr) error {
		// A member that is only a copy of the loop variable is the item's kind, not the slot's: the tag
		// question belongs to the item the loop variable copied (ADR 0259's rule for the list element).
		tagQuery := c.Elems[0]
		if g.compElemCopiesABool(c.Elems[0], lv, item) {
			tagQuery = item
		}
		av, kt, intern, err := g.elemPayloadAndTag(b, c.Elems[0])
		if err != nil {
			return err
		}
		if tagQuery != c.Elems[0] {
			if t, okTag := g.elemKindTag(tagQuery); okTag {
				kt, intern = strconv.FormatInt(int64(t), 10), false
			}
		}
		// A member is also the position that has to be hashable, and only the tag can say whether it
		// is: a list, a dict or a set member raises the reference's own sentence from the write rather
		// than landing a heap handle in a bucket and printing a plausible set (Gap R.81, ADR 0310,
		// ADR 0311 — the same door `s.add(v)` stands in front of).
		g.guardHashableTag(b, kt, c.Span())
		b.WriteString(fmt.Sprintf("  call void @rt_set_add_tagged(i32 %s, i32 %s, i32 %s)\n", h, av, kt))
		if intern {
			b.WriteString(fmt.Sprintf("  call void @rt_mark_estr(i32 %s, i32 1)\n", h))
		}
		return nil
	}
	// entryPair is the dict's half of the same door: the key and the value are each a (payload, tag)
	// pair, and a pair is only ever read or written as a pair (ADR 0185, ADR 0245). A key that is the
	// loop variable of a container of texts has to arrive tagged as text, or `{k: 1 for k in d}` keys
	// itself by an index and `out["a"]` dies with KeyError.
	entryPair := func(e Expr, item Expr) (payload, tag string, interned, dynamic bool, err error) {
		if nm, isName := e.(*Name); isName {
			if p, t, ok := g.taggedLoopVarRead(b, nm.Value, nm.Span()); ok {
				return p, t, false, true, nil
			}
			if c.ForVar != nil && nm.Value == c.ForVar.Value && g.internedVars[nm.Value] {
				p, perr := g.value(b, e)
				if perr != nil {
					return "", "", false, false, perr
				}
				return p, strconv.FormatInt(int64(TagStr), 10), true, false, nil
			}
			// The operand only copies the loop variable: its kind is the item's, asked once per write so
			// a set built from a mixed literal prints each slot by its own kind (ADR 0259).
			if c.ForVar != nil && nm.Value == c.ForVar.Value && g.compElemCopiesABool(e, lv, item) {
				t, okTag := g.elemKindTag(item)
				if !okTag {
					return "", "", false, false, fmt.Errorf("codegen: a dict entry whose operand copies an item of a kind the compiler cannot name needs a tagged value word (roadmap L11.1)")
				}
				p, perr := g.value(b, e)
				if perr != nil {
					return "", "", false, false, perr
				}
				return p, strconv.FormatInt(int64(t), 10), false, false, nil
			}
		}
		// The general case is the door every other container-writing position uses: it materialises a
		// container operand into the heap (a `@.lstN` global address in an `i32` argument is the module
		// `llc` rejects, exit 2, which is how `{x: [1,2] for x in [1]}` used to end), and it refuses an
		// operand whose kind it cannot name rather than storing the compiler's guess (ADR 0187, ADR 0244).
		p, t, intern, perr := g.elemPayloadAndTag(b, e)
		if perr != nil {
			return "", "", false, false, perr
		}
		return p, t, intern, false, nil
	}
	putEntry := func(item Expr) error {
		kv, kt, kIntern, _, kerr := entryPair(c.Keys[0], item)
		if kerr != nil {
			return kerr
		}
		vv, vt, vIntern, _, verr := entryPair(c.Vals[0], item)
		if verr != nil {
			return verr
		}
		// Only the key asks the hashable question — a dict VALUE may be a container (roadmap Gap R.353,
		// beside ADR 0310's entry door).
		g.guardHashableTag(b, kt, c.Span())
		b.WriteString(fmt.Sprintf("  call void @rt_dict_put_tagged(i32 %s, i32 %s, i32 %s, i32 %s, i32 %s)\n", h, kv, vv, kt, vt))
		if kIntern {
			b.WriteString(fmt.Sprintf("  call void @rt_mark_estr(i32 %s, i32 2)\n", h))
		}
		if vIntern {
			b.WriteString(fmt.Sprintf("  call void @rt_mark_estr(i32 %s, i32 4)\n", h))
		}
		return nil
	}
	write := func(item Expr) error {
		if c.Kind == CompSet {
			return addMember(item)
		}
		return putEntry(item)
	}

	for _, item := range itemExprs {
		iv, err := g.value(b, item)
		if err != nil {
			return "", err
		}
		b.WriteString(fmt.Sprintf("  store i32 %s, i32* %%_%s\n", iv, lv))
		if c.Cond == nil {
			if err := write(item); err != nil {
				return "", err
			}
			continue
		}
		// The filter runs per item, in source order, with the loop variable bound — so
		// `{f(x) for x in xs if g(x)}` filters with the same g the reference calls.
		condL := g.newLabel("comp.cond")
		itemL := g.newLabel("comp.item")
		skipL := g.newLabel("comp.skip")
		b.WriteString(fmt.Sprintf("  br label %%%s\n%s:\n", condL, condL))
		tv := g.truthOperand(b, c.Cond)
		b.WriteString(fmt.Sprintf("  br i1 %s, label %%%s, label %%%s\n%s:\n", tv, itemL, skipL, itemL))
		if err := write(item); err != nil {
			return "", err
		}
		b.WriteString(fmt.Sprintf("  br label %%%s\n%s:\n", skipL, skipL))
	}
	if g.rtComps == nil {
		g.rtComps = map[*Comp]bool{}
	}
	g.rtComps[c] = true
	return h, nil
}
