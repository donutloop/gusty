package lang

// pkg/lang/codegen_comp_nested.go — a comprehension with a CLAUSE LIST is the nested loop its clauses
// spell (roadmap Gap R.352, ADR 0381's measurement of it).
//
// The grammar never had ONE `for` clause. `[y for x in rows for y in x]` is two clauses read left to
// right, each one seeing the targets the earlier ones bound, and the flattening idiom Python programmers
// write — rows of rows, a matrix, a pair of ranges — is a program the reference runs and this toolchain
// refused at the parser with `expected "]"`.
//
// Every road in `codegen_comp.go` walks ONE clause: it reads `c.ForVar`, `c.Iter`, `c.Cond`, binds that
// one name, and folds or emits once per item of that one iterable. Those roads are untouched here, and
// they are asked only when `compIsSingleClause` holds. A comprehension with more than one clause comes
// here, where the same two arms exist in their nested shape:
//
//   - the FOLD arm binds each clause's target to each item that clause walks, one combination at a
//     time, and folds the element (and a key and a value, for a dict) under all of those bindings. A
//     filter belongs to its OWN clause and is asked at that clause's level, which is when the reference
//     asks it — a filter on the outer clause skips the whole inner loop, not just one element;
//   - the RUNTIME arm, when the element is something the folder cannot see (a nested container, a call),
//     builds the heap object one append per combination, storing each clause's target into its own slot
//     so the element's reads see the binding the clause made. This is `runtimeCompList`'s unrolled shape
//     with the recursion the clause list needs — the same `appendElem`, the same tag questions, the same
//     per-target `noteFrameTarget` ADR 0381 requires of a comprehension binding.
//
// What is refused, in these roads' own words and never with a wrong answer: a clause whose iterable this
// pass cannot enumerate at compile time — a container variable, a call's answer. That needs a real
// nested run-time loop (two counters, two `rt_list_len` reads, the tag questions of both levels), which
// is the half of this row the next cycle owns; a comprehension that would need it says so rather than
// walking one clause and answering as if there had been one.

import (
	"fmt"
	"strconv"
	"strings"
)

// nestedComp is the entry the comprehension road takes when the node carries more than one clause.
func (g *irGen) nestedComp(b *strings.Builder, c *Comp) (string, error) {
	if vals, keys, ok := g.nestedCompFold(c); ok {
		return g.emitFoldedNestedComp(c, vals, keys)
	}
	if c.Kind != CompList {
		return "", fmt.Errorf("codegen: %s has no compiled lowering: a nested %s folds only constant elements, and the second clause's runtime builder exists only for a list comprehension — keep every clause's iterable and element constant, or build the rows with a loop (roadmap Gap R.352)", compSurfaceOf(c), nestedCompKindWord(c.Kind))
	}
	return g.nestedCompRuntimeList(b, c)
}

// compFoldItemIsAPlainNumber reports whether one item a clause walks may be bound as a folded loop
// variable's VALUE. Only a literal number may: a container literal lowered as an `i32` is that object's
// index in the module's container table, and a text literal is its index in the string table, so binding
// either as the loop variable's value and folding it into an element fabricates an integer out of a
// handle. The runtime road owns those items instead, and stores the item's payload and tag through the
// same constructors a literal's elements go through (roadmap Gap R.352).
func compFoldItemIsAPlainNumber(e Expr) bool {
	_, ok := e.(*IntLit)
	return ok
}

// compSurfaceOf words a comprehension for a diagnostic: its element (or its entry, for a dict) and every
// clause in the order the author wrote them, spelled with the brackets the program used. A diagnostic that
// writes `[...]` for a set comprehension tells the reader about a program they did not write, so the kind
// decides the brackets and the thing inside them (roadmap Gap R.352).
func compSurfaceOf(c *Comp) string {
	clauses := c.compClauses()
	parts := make([]string, 0, len(clauses))
	for _, cl := range clauses {
		name := "<no name>"
		if cl.Target != nil {
			name = cl.Target.Value
		}
		clause := "for " + name + " in " + exprSurface(cl.Iter)
		if cl.Cond != nil {
			clause += " if " + exprSurface(cl.Cond)
		}
		parts = append(parts, clause)
	}
	head, open, close := "x", "[", "]"
	switch c.Kind {
	case CompSet:
		head, open, close = "x", "{", "}"
	case CompDict:
		head, open, close = "k: v", "{", "}"
	}
	if len(c.Elems) == 1 && c.Elems[0] != nil {
		head = exprSurface(c.Elems[0])
	} else if len(c.Keys) == 1 && len(c.Vals) == 1 && c.Keys[0] != nil && c.Vals[0] != nil {
		head = exprSurface(c.Keys[0]) + ": " + exprSurface(c.Vals[0])
	}
	return "`" + open + head + " " + strings.Join(parts, " ") + close + "`"
}

func nestedCompKindWord(k CompKind) string {
	switch k {
	case CompDict:
		return "dict comprehension"
	case CompSet:
		return "set comprehension"
	default:
		return "comprehension"
	}
}

// compClauseItemsOf answers the items one clause walks, reading the bindings the earlier clauses made:
// a later clause may walk a name an earlier clause bound, and the item that name was bound to is a
// literal this pass can enumerate (`[y for x in [[1, 2]] for y in x]` walks the inner `[1, 2]` because
// the first clause bound `x` to it). Anything else — a container variable, a call's answer — has no
// compile-time length here, and the caller refuses rather than walk one clause and call it done.
func (g *irGen) compClauseItemsOf(cl *CompClause, bound map[string]Expr) ([]int64, []Expr, error) {
	iter := cl.Iter
	if nm, ok := iter.(*Name); ok {
		prev, seen := bound[nm.Value]
		if !seen || prev == nil {
			return nil, nil, g.compClauseNotEnumerable(cl, nm.Value)
		}
		iter = prev
	}
	vals, exprs, err := g.compItemsOf(iter)
	if err != nil {
		return nil, nil, err
	}
	return vals, exprs, nil
}

// compClauseRuntimeItems is the same question asked of a clause for the arm that BUILDS rather than the
// arm that folds: the item EXPRESSIONS the clause walks, with no demand that each one fold to a number.
// `[y for x in [[1, 2]] for y in x]` walks an inner list literal, and the only fact the builder needs is
// the expression to store into the target's slot for that item (roadmap Gap R.352).
func (g *irGen) compClauseRuntimeItems(cl *CompClause, bound map[string]Expr) ([]Expr, error) {
	iter := cl.Iter
	if nm, ok := iter.(*Name); ok {
		prev, seen := bound[nm.Value]
		if !seen || prev == nil {
			return nil, g.compClauseNotEnumerable(cl, nm.Value)
		}
		iter = prev
	}
	switch it := iter.(type) {
	case *ListLit:
		return g.checkedCompItems(cl, it.Elems)
	case *Call:
		if !compIterIsStaticRange(it) {
			return nil, fmt.Errorf("codegen: clause `for %s in %s` is not a list literal, a constant range(), or a name an earlier clause bound", cl.Target.Value, exprSurface(cl.Iter))
		}
		_, exprs, err := g.compItemsOf(iter)
		if err != nil {
			return nil, err
		}
		return exprs, nil
	}
	return nil, fmt.Errorf("codegen: clause `for %s in %s` walks an iterable this backend cannot enumerate where the compiler stands; a clause list is built today over list literals, constant range()s, and names an earlier clause bound to one — a container the run time builds needs the nested runtime loop (roadmap Gap R.356)", cl.Target.Value, exprSurface(cl.Iter))
}

// compClauseNotEnumerable words the refusal for the half of this row that is not built: a clause whose
// iterable is a container the RUN TIME makes (`rows = [[1,2],[3,4]]` / `for x in rows`) has no items to
// unroll where the compiler stands, and walking it needs a real nested loop — two counters, two length
// reads, the tag questions of both levels. Naming that, rather than blaming the name for not being a
// clause target, is what makes the diagnostic actionable (roadmap Gap R.352).
func (g *irGen) compClauseNotEnumerable(cl *CompClause, walked string) error {
	return fmt.Errorf("codegen: clause `for %s in %s` walks %s, a container the run time builds: a clause list is unrolled today over list literals, constant range()s, and names an earlier clause bound to one of those, and the nested run-time loop this shape needs is owed (roadmap Gap R.356)", cl.Target.Value, exprSurface(cl.Iter), walked)
}

// checkedCompItems is the permission one clause's items have to earn before a nested comprehension walks
// them: the same `foldConstInt` question the single-clause road asks, so a clause over texts or floats
// refuses HERE the way a one-clause comprehension over texts or floats refuses today, instead of being
// built from an intern-table index the element then adds as a number. A container element is allowed
// past that door — `[y for x in [[1, 2]] for y in x]` walks a row the previous clause bound, and the row
// is a literal the builder turns into a heap object (roadmap Gap R.352).
func (g *irGen) checkedCompItems(cl *CompClause, elems []Expr) ([]Expr, error) {
	for _, el := range elems {
		if compItemIsContainer(el) {
			continue
		}
		if _, ok := g.foldConstInt(el); !ok {
			return nil, fmt.Errorf("codegen: clause `for %s in %s` walks an element the comprehension road cannot carry; a comprehension over texts or floats is refused with the same words one clause in (roadmap Gap R.352, L11.1)", cl.Target.Value, exprSurface(cl.Iter))
		}
	}
	return elems, nil
}

// compItemIsContainer reports whether an expression is a container literal, which the builder can turn
// into the heap object the loop variable holds.
func compItemIsContainer(e Expr) bool {
	switch e.(type) {
	case *ListLit, *SetLit, *DictLit:
		return true
	}
	return false
}

// compItemValue lowers one item to the word the clause's target stores. A container literal does not go
// through `g.value`: that road answers with the address of a compile-time global, and storing a pointer
// into the i32 slot is the module `llc` rejects — exit 2, the compiler's own bug for a program the
// reference runs. It is built as a heap object instead, the way an assignment to a container name builds
// one (roadmap Gap R.75's rule, applied one construct further in).
func (g *irGen) compItemValue(b *strings.Builder, item Expr) (string, error) {
	switch it := item.(type) {
	case *ListLit:
		return g.heapListFromTagged(b, it)
	case *SetLit:
		return g.heapSetFrom(b, it, "")
	case *DictLit:
		return g.heapDictFrom(b, it, "")
	}
	return g.value(b, item)
}

// nestedCompFold folds a clause-list comprehension to the values it means. It walks the clauses left to
// right, binding each target to each item that clause walks, asking that clause's own filter at that
// clause's level, and folding the element (and a key, and a value) under the whole bundle of bindings.
// ok=false means some element or filter the folder cannot see — the caller then takes the runtime arm.
func (g *irGen) nestedCompFold(c *Comp) (results []int64, keys []int64, ok bool) {
	clauses := c.compClauses()
	if g.constBindings == nil {
		g.constBindings = map[string]int64{}
	}
	if len(c.Elems) > 1 || len(c.Keys) > 1 || len(c.Vals) > 1 {
		return nil, nil, false
	}
	var elem Expr
	switch c.Kind {
	case CompDict:
		if len(c.Keys) != 1 || len(c.Vals) != 1 {
			return nil, nil, false
		}
	default:
		if len(c.Elems) != 1 {
			return nil, nil, false
		}
		elem = c.Elems[0]
	}
	seenKey := map[int64]bool{}
	keyPos := map[int64]int{}
	var boundNames []string
	bind := func(name string, v int64) {
		g.constBindings[name] = v
		boundNames = append(boundNames, name)
	}
	unbindAll := func() {
		for _, n := range boundNames {
			delete(g.constBindings, n)
		}
		boundNames = nil
	}
	// foldOne folds one expression under the bindings currently standing.
	foldOne := func(e Expr) (int64, bool) {
		if e == nil {
			return 0, true
		}
		return g.foldConstInt(e)
	}
	var failed bool
	var walk func(k int, bound map[string]Expr)
	walk = func(k int, bound map[string]Expr) {
		if failed {
			return
		}
		if k == len(clauses) {
			if c.Kind == CompDict {
				kv, kok := foldOne(c.Keys[0])
				vv, vok := foldOne(c.Vals[0])
				if !kok || !vok {
					failed = true
					return
				}
				if seenKey[kv] {
					// A LATER entry replaces the earlier one's VALUE and keeps the key's PLACE, which is what
					// a dict is: `{x:y for x in [1,2] for y in [10,20] if y>x}` ends at `{1: 20, 2: 20}`, not at
					// the first value each key was written with.
					results[keyPos[kv]] = vv
					return
				}
				seenKey[kv] = true
				keyPos[kv] = len(results)
				keys = append(keys, kv)
				results = append(results, vv)
				return
			}
			v, vok := foldOne(elem)
			if !vok {
				failed = true
				return
			}
			if c.Kind == CompSet {
				if seenKey[v] {
					return
				}
				seenKey[v] = true
			}
			results = append(results, v)
			return
		}
		cl := clauses[k]
		if cl.Target == nil {
			failed = true
			return
		}
		vals, exprs, err := g.compClauseItemsOf(cl, bound)
		if err != nil {
			failed = true
			return
		}
		// A folded nest binds each loop variable to an `i64` answer, which is only the loop variable's
		// value while the item the clause walks IS a number: a nested container literal reaches
		// `lowerAsExpr` as its interned LIST index, and binding that as the variable's value is how
		// `[[x,y] for x in [1] for y in [[3]]]` answered `[[1, 1]]` for CPython's `[[1, [3]]]`. Items that
		// are not plain numbers send the whole comprehension to the runtime road (roadmap Gap R.352).
		for i := range vals {
			if !compFoldItemIsAPlainNumber(exprs[i]) {
				failed = true
				return
			}
		}
		for i := range vals {
			bind(cl.Target.Value, vals[i])
			nb := map[string]Expr{}
			for n, e := range bound {
				nb[n] = e
			}
			nb[cl.Target.Value] = exprs[i]
			// The filter belongs to THIS clause: it decides whether the whole rest of the nest runs.
			if cl.Cond != nil {
				cv, cok := foldOne(cl.Cond)
				if !cok {
					failed = true
					return
				}
				if cv == 0 {
					continue
				}
			}
			walk(k+1, nb)
			if failed {
				return
			}
		}
	}
	walk(0, map[string]Expr{})
	if failed {
		unbindAll()
		return nil, nil, false
	}
	unbindAll()
	return results, keys, true
}

// emitFoldedNestedComp mints the compile-time global a folded clause-list comprehension answers from —
// the same layout the single-clause fold writes, so a comprehension with two clauses and the literal it
// means are the same object to the printer, to `len` and to an index.
func (g *irGen) emitFoldedNestedComp(c *Comp, results, keys []int64) (string, error) {
	if g.compNames == nil {
		g.compNames = map[*Comp]string{}
	}
	if g.compLen == nil {
		g.compLen = map[*Comp]int{}
	}
	if g.compEls == nil {
		g.compEls = map[*Comp][]int64{}
	}
	if g.compKeys == nil {
		g.compKeys = map[*Comp][]int64{}
	}
	exprsOf := func(vs []int64) []Expr {
		out := make([]Expr, 0, len(vs))
		for _, v := range vs {
			out = append(out, &IntLit{Value: v})
		}
		return out
	}
	partsOf := func(vs []int64) string {
		parts := make([]string, 0, len(vs))
		for _, v := range vs {
			parts = append(parts, fmt.Sprintf("i32 %d", v))
		}
		return strings.Join(parts, ", ")
	}
	var name string
	switch c.Kind {
	case CompSet:
		g.setIdx++
		name = fmt.Sprintf("@.set%d", g.setIdx)
		if g.staticSets == nil {
			g.staticSets = map[string]*SetLit{}
		}
		g.staticSets[name] = &SetLit{Elems: exprsOf(results), Src: c.Src}
		n := len(results)
		g.globals.WriteString(fmt.Sprintf("%s = private global {i32, [%d x i32]} { i32 %d, [%d x i32] [%s] }\n", name, n, n, n, partsOf(results)))
	case CompDict:
		g.dictIdx++
		name = fmt.Sprintf("@.dict%d", g.dictIdx)
		if g.staticDicts == nil {
			g.staticDicts = map[string]*DictLit{}
		}
		g.staticDicts[name] = &DictLit{Keys: exprsOf(keys), Vals: exprsOf(results), Src: c.Src}
		n := len(keys)
		g.globals.WriteString(fmt.Sprintf("%s = private global {i32, [%d x i32], [%d x i32]} { i32 %d, [%d x i32] [%s], [%d x i32] [%s] }\n", name, n, n, n, n, partsOf(keys), n, partsOf(results)))
		g.compKeys[c] = keys
	default:
		g.lstIdx++
		name = fmt.Sprintf("@.lst%d", g.lstIdx)
		if g.staticLists == nil {
			g.staticLists = map[string]*ListLit{}
		}
		g.staticLists[name] = &ListLit{Elems: exprsOf(results), Src: c.Src}
		n := len(results)
		g.globals.WriteString(fmt.Sprintf("%s = private global {i32, [%d x i32]} { i32 %d, [%d x i32] [%s] }\n", name, n, n, n, partsOf(results)))
	}
	g.compNames[c] = name
	g.compLen[c] = len(results)
	g.compEls[c] = results
	return name, nil
}

// nestedCompRuntimeList builds the heap list a clause-list comprehension means when its element is
// something the folder cannot see. Each clause's target gets a slot of its own, written once per item
// that clause walks; the append happens once per combination the filters let through.
func (g *irGen) nestedCompRuntimeList(b *strings.Builder, c *Comp) (string, error) {
	clauses := c.compClauses()
	if len(c.Elems) != 1 {
		return "", fmt.Errorf("codegen: a runtime comprehension builds one element at a time (roadmap L11.1)")
	}
	if g.nestedContainerElem(c.Elems[0]) && !g.taggableNestedElem(c.Elems[0]) {
		return "", nestedContainerErr(c.Elems[0])
	}
	// One slot per clause target: the element reads its names out of these, so the binding a clause
	// made IS the fact the element sees, and ADR 0381's rule — a comprehension's target is a binding of
	// the frame that hosts it — is applied to every clause rather than to the first one only.
	slots := make([]string, 0, len(clauses))
	for _, cl := range clauses {
		if cl.Target == nil {
			return "", fmt.Errorf("codegen: a comprehension clause binds no name")
		}
		lv := loopVarName(cl.Target)
		if !g.allocd[lv] {
			b.WriteString(fmt.Sprintf("  %%_%s = alloca i32\n", lv))
			g.gcReg(b, lv)
			g.allocd[lv] = true
		}
		g.noteFrameTarget(lv)
		defer g.forgetFrameTarget(lv)
		slots = append(slots, lv)
	}
	g.heapUsed = true
	g.heapSeq++
	h := fmt.Sprintf("%%h%d", g.heapSeq)
	g.emitRtAllocKind(b, h, HeapList, c.Span())
	// Rooted for the build: the handle is a register and a nested element may call (roadmap **Gap R.500**,
	// **ADR 0443**; **ADR 0181**, **ADR 0347**).
	buildRoot := g.gcRootDuringBuild(b, h)

	var walkFailed error
	var walk func(k int, bound map[string]Expr)
	walk = func(k int, bound map[string]Expr) {
		if walkFailed != nil {
			return
		}
		if k == len(clauses) {
			// An element that is ITSELF a container literal is built as a static table, and a static table
			// stores one `i32` per slot: a loop variable that a clause bound to a container, a text, a float
			// or a verdict contributes its payload with its tag left behind, which is how
			// `[[x,y] for x in [1] for y in [[3]]]` answered `[[1, 1]]` for CPython's `[[1, [3]]]`. The road
			// says so instead of shipping that (roadmap Gap R.355).
			if name, item, bad := compElemContainerMentionsAnUntaggableSlot(c.Elems[0], bound, clauses); bad {
				walkFailed = fmt.Errorf("codegen: %s element stores `%s`, which its clause bound to %s: the container literal the element builds keeps one word per slot, and the kind that word's meaning depends on is not in it (roadmap Gap R.355)", compSurfaceOf(c), name, exprSurface(item))
				return
			}
			// Which kind the element is is asked of the element itself; when the element only copies one
			// of the clause targets, the question belongs to the item that target was bound to — the same
			// reading `runtimeCompList` takes one clause in (roadmap Gap R.112, ADR 0259).
			tagQuery := c.Elems[0]
			if nm, isName := c.Elems[0].(*Name); isName {
				for _, lv := range slots {
					if lv == nm.Value {
						if item, seen := bound[nm.Value]; seen && g.compElemCopiesABool(c.Elems[0], lv, item) {
							tagQuery = item
						}
						break
					}
				}
			}
			av, kt, intern, err := g.elemPayloadAndTag(b, c.Elems[0])
			if err != nil {
				walkFailed = err
				return
			}
			if tagQuery != c.Elems[0] {
				if t, okTag := g.elemKindTag(tagQuery); okTag {
					kt, intern = strconv.FormatInt(int64(t), 10), false
				}
			}
			b.WriteString(fmt.Sprintf("  call void @rt_append_tagged(i32 %s, i32 %s, i32 %s)\n", h, av, kt))
			if intern {
				b.WriteString(fmt.Sprintf("  call void @rt_mark_estr(i32 %s, i32 1)\n", h))
			}
			if t, okTag := g.elemKindTag(tagQuery); okTag && slotTagSelfDescribing(t) {
				b.WriteString(fmt.Sprintf("  call void @rt_mark_estr(i32 %s, i32 8)\n", h))
			}
			return
		}
		cl := clauses[k]
		exprs, err := g.compClauseRuntimeItems(cl, bound)
		if err != nil {
			walkFailed = err
			return
		}
		for i := range exprs {
			iv, err := g.compItemValue(b, exprs[i])
			if err != nil {
				walkFailed = err
				return
			}
			b.WriteString(fmt.Sprintf("  store i32 %s, i32* %%_%s\n", iv, slots[k]))
			nb := map[string]Expr{}
			for n, e := range bound {
				nb[n] = e
			}
			nb[cl.Target.Value] = exprs[i]
			if cl.Cond == nil {
				walk(k+1, nb)
				continue
			}
			condL := g.newLabel("comp.cond")
			takeL := g.newLabel("comp.take")
			skipL := g.newLabel("comp.skip")
			b.WriteString(fmt.Sprintf("  br label %%%s\n%s:\n", condL, condL))
			tv := g.truthOperand(b, cl.Cond)
			b.WriteString(fmt.Sprintf("  br i1 %s, label %%%s, label %%%s\n%s:\n", tv, takeL, skipL, takeL))
			walk(k+1, nb)
			if walkFailed != nil {
				return
			}
			b.WriteString(fmt.Sprintf("  br label %%%s\n%s:\n", skipL, skipL))
		}
	}
	walk(0, map[string]Expr{})
	if walkFailed != nil {
		return "", walkFailed
	}
	g.gcEndBuildRoot(b, buildRoot)
	if g.rtComps == nil {
		g.rtComps = map[*Comp]bool{}
	}
	g.rtComps[c] = true
	return h, nil
}

// compElemContainerMentionsAnUntaggableSlot answers the question the nested runtime road cannot afford
// to skip: an element that is (or contains) a container literal is emitted as a static table, and a slot
// of that table is one `i32`. A clause target that the element stores into such a slot is only safe while
// the item the clause bound it to is a number — a container, text, float or verdict reaches the slot as
// its payload with its tag left behind, and the reader of the finished table has no way to ask which. So
// this names the offending clause target and the item it was bound to, and the caller refuses (roadmap
// Gap R.355). An element that is NOT a container literal goes through the pair roads, which ask the tag;
// those shapes answer.
func compElemContainerMentionsAnUntaggableSlot(elem Expr, bound map[string]Expr, clauses []*CompClause) (string, Expr, bool) {
	if elem == nil || !compExprMentionsContainerLiteral(elem) {
		return "", nil, false
	}
	names := map[string]bool{}
	walkExpr2(elem, func(n string) { names[n] = true }, nil)
	for _, cl := range clauses {
		if cl == nil || cl.Target == nil || !names[cl.Target.Value] {
			continue
		}
		item, seen := bound[cl.Target.Value]
		if !seen || compFoldItemIsAPlainNumber(item) {
			continue
		}
		return cl.Target.Value, item, true
	}
	return "", nil, false
}

// compExprMentionsContainerLiteral asks whether an expression builds a container anywhere under itself.
// The node list is closed and the DEFAULT IS TRUE: an element shape this file has not met is treated as
// one that does, because the cost of being wrong in that direction is a refusal, and the cost of the
// other direction is a plausible list at the exit code of success (roadmap Gap R.355).
func compExprMentionsContainerLiteral(e Expr) bool {
	switch v := e.(type) {
	case nil:
		return false
	case *Name, *IntLit, *FloatLit, *BoolLit, *NoneLit, *StrLit:
		return false
	case *ListLit:
		for _, it := range v.Elems {
			if compExprMentionsContainerLiteral(it) {
				return true
			}
		}
		return true
	case *SetLit:
		for _, it := range v.Elems {
			if compExprMentionsContainerLiteral(it) {
				return true
			}
		}
		return true
	case *Tuple:
		for _, it := range v.Elems {
			if compExprMentionsContainerLiteral(it) {
				return true
			}
		}
		return true
	case *DictLit:
		for _, it := range append(append([]Expr{}, v.Keys...), v.Vals...) {
			if compExprMentionsContainerLiteral(it) {
				return true
			}
		}
		return true
	case *BinOp:
		return compExprMentionsContainerLiteral(v.L) || compExprMentionsContainerLiteral(v.R)
	case *UnOp:
		return compExprMentionsContainerLiteral(v.X)
	case *Attr:
		return compExprMentionsContainerLiteral(v.Obj)
	case *Index:
		return compExprMentionsContainerLiteral(v.Obj) || compExprMentionsContainerLiteral(v.Idx)
	case *Slice:
		return compExprMentionsContainerLiteral(v.Obj) || compExprMentionsContainerLiteral(v.Low) ||
			compExprMentionsContainerLiteral(v.High) || compExprMentionsContainerLiteral(v.Step)
	case *CondExpr:
		return compExprMentionsContainerLiteral(v.If) || compExprMentionsContainerLiteral(v.Cond) || compExprMentionsContainerLiteral(v.Else)
	case *AssignExpr:
		return compExprMentionsContainerLiteral(v.Value)
	case *AwaitExpr:
		return compExprMentionsContainerLiteral(v.Expr)
	case *KeywordArg:
		return compExprMentionsContainerLiteral(v.Value)
	case *ChainCompare:
		for _, it := range v.Operands {
			if compExprMentionsContainerLiteral(it) {
				return true
			}
		}
		return false
	case *Call:
		for _, it := range v.Args {
			if compExprMentionsContainerLiteral(it) {
				return true
			}
		}
		return compExprMentionsContainerLiteral(v.Fn)
	case *Lambda:
		return compExprMentionsContainerLiteral(v.Body)
	default:
		return true
	}
}
