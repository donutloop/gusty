// The statement roads: every Stmt's lowering, including the loop bodies, the scope bookkeeping a
// statement does, and the loop-safe-point the collector is invited to.
package lang

import (
	"fmt"
	"strings"
)

type loopInfo struct {
	breakLabel    string
	continueLabel string
}

// blockEndsInTerminator reports whether the block being built already ended in a terminator,
// in which case another instruction would be dead weight: an arm whose last statement is
// `return` or `raise` has already left, and it did so with the exception's own state.
func blockEndsInTerminator(b *strings.Builder) bool {
	lines := strings.Split(strings.TrimRight(b.String(), "\n"), "\n")
	for i := len(lines) - 1; i >= 0; i-- {
		ln := strings.TrimSpace(lines[i])
		if ln == "" {
			continue
		}
		if strings.HasSuffix(ln, ":") {
			return false // a label: a fresh block, nothing emitted in it yet
		}
		return strings.HasPrefix(ln, "ret ") || strings.HasPrefix(ln, "br ") || ln == "unreachable"
	}
	return false
}

// loopVarName returns the name of a Name loop variable, or "" for a Tuple.
func loopVarName(v Expr) string {
	if n, ok := v.(*Name); ok {
		return n.Value
	}
	return ""
}
func (g *irGen) stmt(b *strings.Builder, st Stmt) error {
	// Every statement is a position a debugger, a profiler or (Gap K.8) a traceback can be
	// asked about, so record where its IR starts before anything writes a line (L8.5, ADR 0231).
	g.dbgMark(b, st.Span())
	switch n := st.(type) {
	case *ImportStmt:
	case *ExternDecl:
		// extern declarations are handled at the module level

		// `import mod` resolves module globals at compile time (see resolveImports);
		// the statement itself emits no IR.
		return nil
	case *TryStmt:
		return g.tryStmt(b, n)
	case *RaiseStmt:
		return g.raiseStmt(b, n)
	case *ExprStmt:
		// A bare string expression as a statement has no effect, and evaluating it would now
		// intern it -- which keeps the literal's globals alive and unprunable for a program that
		// only mentions the text (the `--opt-level=1` dead-global assertion). Skipping it is the
		// same no-op it always was.
		if _, isLit := n.Expr.(*StrLit); !isLit {
			if _, err := g.value(b, n.Expr); err != nil {
				return err
			}
		}
	case *AssignStmt:
		// tuple unpacking: a, b = v1, v2
		// Subscript assignment: `d[k] = v` inserts/updates a dict entry, `xs[i] = v`
		// replaces a list element within bounds (Python raises IndexError otherwise).
		// Sets and strings reject it, as they do in the interpreter.
		if ix, ok := n.Target.(*Index); ok {
			return g.assignIndex(b, ix, n.Value)
		}
		if tup, ok := n.Target.(*Tuple); ok {
			var valElems []Expr
			switch vt := n.Value.(type) {
			case *Tuple:
				valElems = vt.Elems
			case *ListLit:
				valElems = vt.Elems
			default:
				return fmt.Errorf("codegen: unsupported tuple assignment value %T", n.Value)
			}
			if len(tup.Elems) != len(valElems) {
				// The reference raises this one at run time; this backend answers it at build time, where
				// both sides of the binding are visible, and says the reference's own sentence so a reader
				// who greps for the wording finds this road (roadmap L11.3, ADR 0361, ADR 0166 — the build
				// refuses rather than binding the wrong number of names).
				if len(valElems) > len(tup.Elems) {
					// The reference's too-many sentence has no `got` count — it names only what was expected.
					// Quoting a count the reference never prints would make this refusal disagree with the
					// sentence it claims to quote (roadmap L11.3, ADR 0361, ADR 0214).
					return fmt.Errorf("codegen: unpacking %d values into %d names: the reference raises ValueError: too many values to unpack (expected %d) (roadmap L11.3, ADR 0361)", len(valElems), len(tup.Elems), len(tup.Elems))
				}
				return fmt.Errorf("codegen: unpacking %d values into %d names: the reference raises ValueError: not enough values to unpack (expected %d, got %d) (roadmap L11.3, ADR 0361)", len(valElems), len(tup.Elems), len(tup.Elems), len(valElems))
			}
			// Tuple assignment must be simultaneous: `a, b = b, a` swaps,
			// it must not alias the updated targets. Emit all target allocas
			// first, then snapshot all RHS values, then store each target.
			for _, tgt := range tup.Elems {
				if nm, ok2 := tgt.(*Name); ok2 {
					// Unpacking binds each name to an element the front end cannot read as
					// a verdict, so whatever the name held before is gone (ADR 0172's
					// latest-binding rule, carried to bools by ADR 0257 — the interpreter
					// already forgets here, and the two engines must forget together).
					g.forgetVarBool(nm.Value)
					if !g.allocd[nm.Value] {
						g.allocd[nm.Value] = true
						b.WriteString(fmt.Sprintf("  %%_%s = alloca i32\n", nm.Value))
						g.gcReg(b, nm.Value)
					}
				}
			}
			vals := make([]string, len(tup.Elems))
			for i := range tup.Elems {
				val, err := g.value(b, valElems[i])
				if err != nil {
					return err
				}
				vals[i] = val
			}
			for i, tgt := range tup.Elems {
				if nm, ok2 := tgt.(*Name); ok2 {
					b.WriteString(fmt.Sprintf("  store i32 %s, i32* %%_%s\n", vals[i], nm.Value))
					g.markBound(b, nm.Value) // unpacking binds every element (ADR 0228)
				}
			}
			return nil
		}
		if nm, ok := n.Target.(*Name); ok {
			// Whatever this statement binds, the records the *previous* binding left behind stop applying.
			// ADR 0172 states the rule and ADR 0267 built the door for the tag; the interned-text record has
			// been missed, which is why `x = "text"` then `x = [1, 2]` printed `text` compiled at exit 0
			// (roadmap Gap R.145, ADR 0270). Asked before anything below records the new kind.
			g.retireVarStatuses(nm.Value, n.Value)
			// union-annotated scalar variable: tag its slot for runtime dispatch
			if g.unionVars == nil {
				g.unionVars = map[string]bool{}
			}
			if n.Annot != nil && n.Annot.Kind == KindUnion {
				g.unionVars[nm.Value] = true
			}
			// escape analysis: a list literal assigned to a variable that is
			// never read (dead) skips its heap allocation entirely.
			if _, isList := n.Value.(*ListLit); isList && !g.inFunc && g.deadLists[nm.Value] {
				return nil
			}
			// A binding whose *inferred type* is a container is a container, even
			// when the right-hand side is a call (`d = make(4)`). The kind maps are
			// what make iteration, indexing, len and truthiness ask the runtime for
			// a length; unregistered, `for k in d:` fell back to the range path and
			// compared the loop index against the handle — zero iterations.
			// A set/dict comprehension whose every operand folds means the literal it folds to, and a
			// container literal already has exactly one lowering here: allocate the heap object, write
			// every slot with its tag, register the variable's kind (ADR 0163's binding rule). Asking
			// the fold before the value is lowered is what makes `sa = {1, 2}` and
			// `sa = {x for x in [1, 2]}` build one and the same object, instead of the second spelling
			// storing the compile-time global's address — `store i32 @.set1, i32* %_sa`, the module llc
			// rejected and the exit-code contract called a compiler bug for an ordinary program
			// (roadmap Gap J.2, ADR 0234).
			rhs := n.Value
			if lit, folded := g.foldedContainerCompLiteral(n.Value); folded {
				rhs = lit
			}
			_, literal := rhs.(*ListLit)
			_, literalDict := rhs.(*DictLit)
			_, literalSet := rhs.(*SetLit)
			boundKind := containerKindFromTy(exprTyName(n.Value))
			// A call that hands back a container says so from its own body, which is better evidence than an
			// inferred type name: the answer is an object of that kind whatever the inference made of the call
			// (roadmap Gap R.67, ADR 0333 — `xs = build()` with `def build(): return {"a": 1}` printed the
			// handle because nothing had recorded `xs` as a dict).
			if c, isCall := n.Value.(*Call); isCall {
				if nm, isName := c.Fn.(*Name); isName {
					if a, okA := g.containerAnswerOf(nm.Value); okA && boundKind == "" {
						boundKind = a.kind.String()
					}
				}
			}
			// A comprehension says what it binds better than an inferred type can: the kind is written
			// in the syntax — `[..]`, `{..}` and `{k: v ..}` build a list, a set and a dict — and the
			// runtime loop has just put a handle of that kind in the slot. Without the record,
			// `print(sa)` printf'd the handle and answered `1` where both other engines answer `{2, 3}`
			// (roadmap Gap J.2, ADR 0234).
			if c, isComp := rhs.(*Comp); isComp && boundKind == "" {
				switch c.Kind {
				case CompList:
					boundKind = "list"
				case CompSet:
					boundKind = "set"
				case CompDict:
					boundKind = "dict"
				}
			}
			// A slice of a container binds a container. `rt_slice` returns a fresh heap list whose slots are
			// the base's payloads with their tags, so `ys = xs[0:2]` receives an object of the same kind as
			// `xs` — and the name has to be recorded as one, or the binding keeps a handle nothing can name:
			// `print(ys)` answered `1`, `len(ys)` refused `len of a non-string variable`, and
			// `for v in ys:` printed a single `0` (roadmap Gap R.230, ADR 0335).
			var boundSliceBase *Slice
			boundIsSlice := false
			if sl, isSlice := n.Value.(*Slice); isSlice && boundKind == "" {
				if kind, okK := g.staticContainerKind(sl.Obj); okK && kind == "list" {
					boundKind = kind
					boundSliceBase = sl
					boundIsSlice = true
				}
			}
			// Container literals have their own lowering paths below, which register
			// the kind themselves; only non-literal bindings (a call, an index, ...)
			// need the inferred type to say so here.
			rebindsContainer := boundKind != "" && !literal && !literalDict && !literalSet
			if rebindsContainer {
				// The kind a call's answer, an index or a copy brings is settled by the one door that
				// writes all twelve container facts, not by the three bits this switch used to set —
				// which left the two kinds the binding is NOT still claimed by every read of the name
				// (roadmap Gap Q.1).
				g.settleContainerKind(nm.Value, boundKind)
			}
			// A container handed back by a call records its own facts, and they are richer than the three
			// kind bits above: whether its slots describe themselves, and that no static literal describes
			// them — the two questions every read of a container the program built at run time asks (ADR
			// 0232's rule, applied to the answer of a call by roadmap Gap R.67, ADR 0333).
			if c, isCall := n.Value.(*Call); isCall {
				if cn, isName := c.Fn.(*Name); isName {
					if a, okA := g.containerAnswerOf(cn.Value); okA {
						g.recordContainerAnswer(nm.Value, a)
					}
				}
			}
			// The sliced binding's facts are the same three questions — which kind, do the slots describe
			// themselves, is there a static literal behind it — asked of the base the copy was taken from
			// (roadmap Gap R.230).
			if boundIsSlice {
				g.recordContainerSlice(nm.Value, boundSliceBase.Obj)
			}
			// Track the None singleton the same way containers are tracked: the variable's
			// *latest* assignment decides how print/truthiness/equality lower, and any other
			// assignment must clear the status (x = None; x = 0 must print 0) (ADR 0172).
			if g.isNoneExpr(n.Value) {
				g.noneVars[nm.Value] = true
			} else if !rebindsContainer {
				delete(g.noneVars, nm.Value)
			}
			// A bool is tracked the same way, by the same rule: the variable's latest
			// assignment decides how print and str render it, and any other assignment clears
			// the status — `flag = 1 == 1` then `flag = 5` must print 5 (ADR 0172's rule, applied
			// to bools by ADR 0257).
			if g.printsAsBool(n.Value) && !rebindsContainer {
				if g.boolVars == nil {
					g.boolVars = map[string]bool{}
				}
				g.boolVars[nm.Value] = true
			} else if !rebindsContainer {
				delete(g.boolVars, nm.Value)
			}
			// A module-level container variable needs its slot + GC root before
			// anything mutates it; the empty-literal init (`xs = []`) is where that
			// happens, because the literal path below only stores a fresh handle.
			if ll, isList := n.Value.(*ListLit); isList && !g.inFunc && len(ll.Elems) == 0 {
				// Module-level `xs = []`: give the variable its slot and GC root up
				// front, the way a non-empty literal does, so a later `xs.append(i)`
				// (which only stores through the slot) has somewhere to write and
				// rt_gc can see the live handle.
				_ = ll
				g.emitModuleContainerList(b, nm.Value, "%_"+nm.Value)
			}
			// `f = lambda ...` binds the generated lambda FuncDef to the variable.
			if lam, ok := n.Value.(*Lambda); ok {
				name, err := g.emitLambda(b, lam)
				if err != nil {
					return err
				}
				if g.lambdas == nil {
					g.lambdas = map[string]string{}
				}
				g.lambdas[nm.Value] = name
				g.forgetTaggedBinding(nm.Value) // a function is not a pair (Gap R.142)
				return nil
			}
			// `ys = [f(x) for x in ...]` / `sa = {x for x in xs}` / `da = {k: v for k in xs}` bind a container
			// the program builds at runtime. The variable needs its slot and its GC root *before* the
			// handle is stored: an unrooted handle is one collection away from a segfault, and this branch
			// is what keeps a comprehension result alive past the next statement (ADR 0192, ADR 0181; the
			// set and dict kinds join it with ADR 0234).
			// A comprehension whose operands all folded is not bound here: `rhs` carries the literal the
			// fold produced, and the container-literal paths below bind it, so `{x for x in [1, 2]}` and
			// `{1, 2}` are one code path (ADR 0234). This branch is the runtime one — the loop has just
			// built a handle, and it needs the slot, the root and the kind recorded for it.
			if comp, isComp := n.Value.(*Comp); isComp && rhs == n.Value {
				if g.listVars[nm.Value] || g.runtimeDicts[nm.Value] || g.runtimeSets[nm.Value] {
					g.emitFreeOld(b, nm.Value)
				}
				// One door, twelve claims: the comprehension binds whatever kind its brackets wrote, and
				// the claims of the other two kinds go with it (roadmap Gap Q.1).
				compKind := containerKindList
				switch comp.Kind {
				case CompDict:
					compKind = containerKindDict
				case CompSet:
					compKind = containerKindSet
				}
				g.settleContainerKind(nm.Value, compKind)
				if len(comp.Elems) == 1 {
					if g.compElemIsTaggedLoopVar(comp) {
						// The element's kind is only in the object: the loop variable came out of a
						// container whose slots mix kinds, so nothing static says what landed in this
						// list. It has to let its slots speak (ADR 0232) and be able to print a float it
						// cannot see coming (roadmap Gap R.76, ADR 0244).
						g.mixedLists[nm.Value] = true
						g.listElemStr[nm.Value] = false
						g.floatFmtUsed = true
					} else if t, okTag := g.elemKindTag(comp.Elems[0]); okTag && slotTagSelfDescribing(t) {
						// The builder wrote a tag with every slot and told the object to let its slots
						// speak; the read has to ask them too, or `print(xs[0])` shows the float box's
						// handle where CPython shows 1.5, and a container element prints its handle
						// instead of the list inside (roadmap Gap R.46, Gap R.75, ADR 0244).
						g.mixedLists[nm.Value] = true
						g.floatFmtUsed = g.floatFmtUsed || t == int32(TagFloat)
					} else if g.compElemPrintsAsText(comp) {
						g.listElemStr[nm.Value] = true
					} else {
						g.listElemStr[nm.Value] = g.exprIsString(comp.Elems[0])
					}
				}
				if !g.allocd[nm.Value] {
					b.WriteString(fmt.Sprintf("  %%_%s = alloca i32\n", nm.Value))
					g.gcReg(b, nm.Value)
					g.allocd[nm.Value] = true
				}
				// A container the callee built is handed over as the handle its builders made; the ordinary
				// road refuses to use that word as a number, so the binding — which can hold it — asks for the
				// permission first (roadmap Gap R.67, ADR 0333).
				perm := g.permitContainerAnswer(n.Value)
				h, herr := g.value(b, n.Value)
				perm.Done()
				if herr != nil {
					return herr
				}
				// The constant path hands back a folded global (@.lstN / @.setN / @.dictN), whose
				// layout is a length plus an array — storing that into an i32 slot is the module llc
				// refuses, so materialise it into the heap (ADR 0163's rule; Gap J.2, ADR 0234).
				if lit, folded := g.staticLists[h]; folded {
					hh, merr := g.heapListFrom(b, lit, "")
					if merr != nil {
						return merr
					}
					h = hh
				} else if lit, folded := g.staticSets[h]; folded {
					hh, merr := g.heapSetFrom(b, lit, "")
					if merr != nil {
						return merr
					}
					h = hh
				} else if lit, folded := g.staticDicts[h]; folded {
					hh, merr := g.heapDictFrom(b, lit, "")
					if merr != nil {
						return merr
					}
					h = hh
				}
				g.gcStoreHandle(b, h, nm.Value)
				delete(g.noneVars, nm.Value)
				g.forgetTaggedBinding(nm.Value) // the container is the binding now (Gap R.142)
				return nil
			}
			// `v = xs[i]` out of a tagged list binds a (value, tag) pair, not a bare i32:
			// the slot means nothing without its companion, and print(v) dispatches on the
			// tag exactly as a loop variable over a mixed list does (ADR 0185, ADR 0187).
			if ix, isIndex := n.Value.(*Index); isIndex {
				if v, t, okRead, rerr := g.taggedContainerRead(b, ix); okRead && rerr == nil {
					// `y = xs[0][1]`, `y = d["a"][1]`, `y = t[0][0][0]` — binding a slot of a container that
					// is itself reached through a slot. Same rule as the two bindings below: the payload
					// means nothing without its tag, so both travel and print(y) dispatches on the tag
					// instead of guessing a kind (roadmap L11.1, ADR 0241).
					g.bindTaggedVar(b, nm.Value, v, t)
					g.recordSlotOrigin(nm.Value)
					return nil
				}
				if dictName, mixed := g.mixedDictIndexRead(ix); mixed {
					// `v = d[k]` out of a dict whose values mix kinds binds the pair too; the
					// block below this one is the list version of the same binding.
					val, tag, err := g.mixedDictPair(b, dictName, ix.Idx, ix.Span())
					if err != nil {
						return err
					}
					g.bindTaggedVar(b, nm.Value, val, tag)
					g.recordSlotOrigin(nm.Value)
					return nil
				}
				if dl, isLit := ix.Obj.(*DictLit); isLit && !g.dictLiteralFoldAnswers(dl, ix.Idx) {
					// `v = {"a": "x"}["a"]` binds the pair for the same reason `v = d[k]` does: the entry's kind is
					// a fact about the object, and a payload without its tag is a table index or a box handle
					// wearing a number. The door is the one the print position asks, so the two cannot disagree
					// about the answer (roadmap Gap R.215, ADR 0327 — ADR 0267's rule one construct nearer the source).
					val, tag, derr := g.literalDictPair(b, dl, ix.Idx, ix.Span())
					if derr != nil {
						return derr
					}
					g.bindTaggedVar(b, nm.Value, val, tag)
					g.recordSlotOrigin(nm.Value)
					return nil
				}
				if listName, mixed := g.mixedIndexRead(ix); mixed {
					val, tag, err := g.mixedElemPair(b, listName, ix.Idx, ix.Span())
					if err != nil {
						return err
					}
					// Rebinding over a container or a tagged value: the door frees the old heap slot and
					// marks the root dead, the way every other immediate binding does.
					g.bindTaggedVar(b, nm.Value, val, tag) // the tagged slot is written too (ADR 0228)
					g.recordSlotOrigin(nm.Value)
					return nil
				}
			}
			// `n = xs[0][0] * 2` — the arithmetic the print position answers, refused one statement
			// earlier. The print dispatch asks the pair door for exactly this expression (ADR 0265) and
			// gets `14`; binding the same answer to a name asked the ordinary numeric road instead, which
			// wants one i32 with no tag beside it and refuses the slot it cannot see into. The road is the
			// same door, and the name becomes a tagged binding — the shape `print(n)` already reads —
			// because a payload without its tag is a number wearing another object's bits (roadmap
			// Gap R.138, ADR 0265's rule one statement earlier).
			if handled, aerr := g.bindPairCallResult(b, nm.Value, n.Value); aerr != nil {
				return aerr
			} else if handled {
				return nil
			}
			if handled, aerr := g.bindArithmeticPair(b, nm.Value, n.Value); aerr != nil {
				return aerr
			} else if handled {
				return nil
			}
			// `m = min(n, 3)` — the answer of a fold the run time decided, bound to a name. The print
			// position already asks this door; a binding that only kept the payload would leave a float
			// winner printing as its box handle and an int winner printing as whatever a text slot's
			// interned index happens to be (roadmap L11.1, Gap R.146; ADR 0267's rule, ADR 0316).
			if handled, ferr := g.bindFoldPair(b, nm.Value, n.Value); ferr != nil {
				return ferr
			} else if handled {
				return nil
			}
			// `n = xs[0]` over a container the program built: the kind lives in the object, so the binding
			// carries the tag beside the payload or the name means nothing on its own (ADR 0185, applied
			// by the one binding road that had not asked — roadmap L11.1, Gap R.146).
			if handled, perr := g.bindSlotReadPair(b, nm.Value, n.Value); perr != nil {
				return perr
			} else if handled {
				return nil
			}
			if lit, ok := n.Value.(*ListLit); ok {
				g.heapUsed = true
				g.heapSeq++
				hs := g.heapSeq
				// heap slot reuse: rebinding a list var frees its old heap slot so rt_alloc can recycle it.
				if g.listVars[nm.Value] || g.runtimeDicts[nm.Value] || g.runtimeSets[nm.Value] {
					g.emitFreeOld(b, nm.Value)
				}
				// The one door: a list literal takes the name over, and every claim the dict and set
				// roads made about it goes with the object they described — including this kind's own
				// element notebook, which describes slots this statement writes again (roadmap Gap Q.1).
				g.settleContainerKindFromLiteral(nm.Value, containerKindList)
				// The name learns the one thing the container notebook does not carry: this binding is the
				// immutable spelling, which prints with parentheses and is never written again (L11.3, ADR 0361).
				g.recordTupleBinding(nm.Value, n.Value)
				if !g.allocd[nm.Value] {
					b.WriteString(fmt.Sprintf("  %%_%s = alloca i32\n", nm.Value))
					g.gcReg(b, nm.Value)
					g.allocd[nm.Value] = true
				}
				g.emitRtAllocKind(b, fmt.Sprintf("%%h%d", hs), HeapKindList, n.Span())
				// A literal that mixes numbers with interned strings or None is no longer a
				// refusal: each slot carries a canonical ValueTag (heap_tags), and the
				// container prints through rt_print_list_mixed. Everything else that would
				// read an element out of it keeps refusing (ADR 0184).
				mixed := g.taggableMixedList(lit)
				// How the slots are stored is written on the object as well as remembered in
				// this scope: bit 1 for interned text, bit 8 for a list whose slots must be read
				// one at a time. print has always chosen its printer from the record kept beside
				// the compiler; str() and repr() ask the object, so the object has to answer.
				estrBits := 0
				// A pair-bound element (`y = [n]` with `n = xs[0]`) is an element whose two words only
				// exist at run time: the static layout has no place for the tag and `value()` rightly
				// refuses the name. Both words come from the name's own allocas, and the object is told
				// to let its slots speak (roadmap L11.1, Gap R.146; ADR 0187, ADR 0232).
				if g.literalHasPairElement(lit) {
					mixed = true
				}
				for i, el := range lit.Elems {
					if pv, pt, isPair := g.pairElemPair(b, el); isPair {
						b.WriteString(fmt.Sprintf("  call void @rt_tag_elem(i32 %%h%d, i32 %d, i32 %s)\n", hs, i, pt))
						b.WriteString(fmt.Sprintf("  call void @rt_set_elem(i32 %%h%d, i32 %d, i32 %s)\n", hs, i, pv))
						continue
					}
					// heapElemKind, not value(): an assigned container literal is still a
					// runtime container, so a string element becomes an index into @str_tab
					// instead of the global pointer that LLVM rejects in an i32 parameter
					// (ADR 0166, roadmap Gap I.2).
					ev, interned, err := g.heapElemKind(b, el)
					if err != nil {
						return err
					}
					// `heapElemKind` reads the emitted shape, and an element that is a folded string
					// subscript (`xs = [s[1]]`) has no shape yet -- its interning is a call, not a
					// literal. The static question is the same question, and the tag below is what the
					// printer reads, so answering it only one way prints the index (ADR 0225).
					if !interned {
						interned = g.exprIsString(el)
					}
					if interned {
						estrBits |= 1
					}
					// Every slot is tagged, not just the ones in a mixed list: rt_container_eq
					// compares (payload, tag) pairs, and a slot whose tag was never written holds
					// whatever the previous tenant of that heap slot left (ADR 0187, ADR 0189).
					tag := g.elemTagFor(el, interned)
					if tag == int32(TagNone) {
						// None has no i32 payload of its own; the tag is what renders it.
						ev = "0"
					}
					b.WriteString(fmt.Sprintf("  call void @rt_tag_elem(i32 %%h%d, i32 %d, i32 %d)\n", hs, i, tag))
					if !mixed {
						if err := g.recordElemKind(nm.Value, "list", interned, el); err != nil {
							return err
						}
					}
					b.WriteString(fmt.Sprintf("  call void @rt_set_elem(i32 %%h%d, i32 %d, i32 %s)\n", hs, i, ev))
				}
				if mixed {
					g.mixedLists[nm.Value] = true
					delete(g.listElemStr, nm.Value)
					estrBits |= 8
					// The object says so too. The set and dict assignment paths have marked
					// bit 8 on a mixed object all along; the list one recorded the fact only in
					// this scope, so print — which picks its printer from the record — was right
					// while anything that asks the object was wrong. str() and repr() ask the
					// object, because the pair is one renderer pointed at a different sink and a
					// handle reaches it without the builder's scope beside it: ["a", 1] came back
					// as [0, 1], the two interned indexes (roadmap L11.2, ADR 0258).
				}
				// The immutable spelling says so on the object, whatever the elements were: the bracket
				// printer, the container dispatcher and every mutation door read this one word
				// (roadmap L11.3, ADR 0361).
				if estrBits = tupleEstrBits(lit, estrBits); estrBits != 0 {
					b.WriteString(fmt.Sprintf("  call void @rt_mark_estr(i32 %%h%d, i32 %d)\n", hs, estrBits))
				}
				g.gcStoreHandle(b, fmt.Sprintf("%%h%d", hs), nm.Value)
				g.forgetTaggedBinding(nm.Value) // the container is the binding now (Gap R.142)
				return nil
			}
			// list var rebound to a non-list value: free its heap slot (GC-correctness).
			// A binding that IS a container (e.g. `d = make(4)`) keeps its kind.
			if !rebindsContainer && (g.listVars[nm.Value] || g.runtimeDicts[nm.Value] || g.runtimeSets[nm.Value]) {
				g.emitFreeOld(b, nm.Value)
				// The slot now holds a raw value: tag its root entry dead so the
				// collector never scans the int as a candidate heap index (ADR 0181).
				if g.allocd[nm.Value] {
					g.gcClearRoot(b, nm.Value)
				}
				// The one door for the binding that is not a container at all: the slot now holds a
				// raw value, and no read of the name may still be measured as a container read
				// (roadmap Gap Q.1).
				g.settleContainerKind(nm.Value, containerKindScalar)
			}
			// runtime set: allocate a heap set object and add each element.
			if sl, ok := rhs.(*SetLit); ok {
				if !g.allocd[nm.Value] {
					b.WriteString(fmt.Sprintf("  %%_%s = alloca i32\n", nm.Value))
					g.gcReg(b, nm.Value)
					g.allocd[nm.Value] = true
				}
				g.heapUsed = true
				if g.listVars[nm.Value] || g.runtimeDicts[nm.Value] || g.runtimeSets[nm.Value] {
					g.emitFreeOld(b, nm.Value)
				}
				g.settleContainerKindFromLiteral(nm.Value, containerKindSet)
				// A set whose members describe themselves has no element kind to record: {1, "a"}
				// is neither the number set nor the string set (roadmap L11.1 (1b), ADR 0232). A member
				// the pair road bound is the same record for the same reason: its tag is a register, and a
				// member added without it dedups against the payload alone (Gap R.146).
				mixedSet := g.taggableMixedSet(sl) || g.literalTakesPairSlots(sl)
				if mixedSet {
					g.mixedSets[nm.Value] = true
					g.setElemStr[nm.Value] = false
					g.setElemInt[nm.Value] = false
				}
				g.heapSeq++
				hs := g.heapSeq
				g.emitRtAllocKind(b, fmt.Sprintf("%%h%d", hs), HeapKindSet, n.Span())
				si := 0
				for _, el := range sl.Elems {
					if mixedSet {
						// Adding and tagging are one call: a member added without its tag dedups
						// against the payload alone and prints through whatever the slot last held.
						// A member the pair road bound hands over the tag the objects wrote instead of
						// the constant a literal answers, and a member whose kind only that tag can tell
						// has to be asked whether it can be a member at all (roadmap L11.1, Gap R.146,
						// Gap R.81; ADR 0187).
						ev, t, isPair, err := g.taggedSlotWords(b, el)
						if err != nil {
							return err
						}
						if isPair {
							g.guardHashableTag(b, t, sl.Src)
						}
						b.WriteString(fmt.Sprintf("  call void @rt_set_add_tagged(i32 %%h%d, i32 %s, i32 %s)\n", hs, ev, t))
						si++
						continue
					}
					ev, interned, err := g.heapElemKind(b, el)
					if err != nil {
						return err
					}
					if err := g.recordElemKind(nm.Value, "set", interned, el); err != nil {
						return err
					}
					b.WriteString(fmt.Sprintf("  call void @rt_set_add(i32 %%h%d, i32 %s)\n", hs, ev))
					b.WriteString(fmt.Sprintf("  call void @rt_tag_elem(i32 %%h%d, i32 %d, i32 %d)\n", hs, si, g.elemTagFor(el, interned)))
					si++
				}
				if mixedSet {
					b.WriteString(fmt.Sprintf("  call void @rt_mark_estr(i32 %%h%d, i32 8)\n", hs))
				}
				g.gcStoreHandle(b, fmt.Sprintf("%%h%d", hs), nm.Value)
				g.forgetTaggedBinding(nm.Value) // the container is the binding now (Gap R.142)
				return nil
			}
			var v string
			var err error
			if _, isStr := g.stringVal(n.Value); isStr {
				// String RHS: fold at compile time into strVals (strings are read back
				// via strVals, never via the runtime slot). Emit a dummy i32 store so
				// the generated IR is valid (the pointer would be invalid as i32).
				v = "0"
			} else if g.isFloat(n.Value) {
				// A float right-hand side belongs to the double domain, and the store below asks
				// `floatValue` for it. Lowering it in the integer domain as well used to be dead but
				// valid IR — an `sdiv` of two i32 registers nobody read — and it stayed invisible for
				// every shape the int path could fold. Once a slot read of a container the program
				// built could answer with a double, the dead emission became
				// `add i32 %_total.ld1, %t27` with %t27 a double: the module `llc` rejects, which
				// ADR 0166 counts as the compiler's bug rather than the program's (roadmap Gap R.88,
				// measured beside Gap R.96). The value is asked for once, in the domain that stores it.
				v = "0"
			} else {
				// A binding is a position that CAN keep the handle a container answer travels in — the slot
				// it stores into is what the container roads read back — so it asks the door's permission
				// before the ordinary road is asked for the word (roadmap Gap R.67, ADR 0333).
				perm := g.permitContainerAnswer(n.Value)
				v, err = g.value(b, n.Value)
				perm.Done()
				if err != nil {
					return err
				}
			}
			// track concrete string constant values for `len(s)` and string ops
			if sv, ok := g.stringVal(n.Value); ok {
				if g.strVals == nil {
					g.strVals = map[string]string{}
				}
				g.strVals[nm.Value] = sv
			}
			// A call can return an @str_tab index without being a literal the folds recognize;
			// min/max of texts is one. Recording the binding here is what lets a later print ask the
			// same question that printing the call directly would have asked (roadmap Gap R.73).
			if _, tracked := g.strVals[nm.Value]; !tracked && g.printsAsInternedStr(n.Value) {
				g.internedVars[nm.Value] = true
			}
			// track dict literals assigned to variables for `d[key]`
			if dl, ok := rhs.(*DictLit); ok {
				// runtime dict: allocate a heap dict object and fill pairs.
				if !g.allocd[nm.Value] {
					b.WriteString(fmt.Sprintf("  %%_%s = alloca i32\n", nm.Value))
					g.gcReg(b, nm.Value)
					g.allocd[nm.Value] = true
				}
				g.heapUsed = true
				if g.listVars[nm.Value] || g.runtimeDicts[nm.Value] || g.runtimeSets[nm.Value] {
					g.emitFreeOld(b, nm.Value)
				}
				g.settleContainerKindFromLiteral(nm.Value, containerKindDict)
				// The set above and heapDictFrom share this rule: a dict whose keys or values mix
				// kinds has no kind to record, so every slot carries its own tag (ADR 0232). A key or a
				// value the pair road bound is the same case with one more word: the entry goes through
				// the tagged put with the tag register the objects wrote (roadmap L11.1, Gap R.146).
				mixedDict := g.taggableMixedDict(dl) || g.literalTakesPairSlots(dl)
				if mixedDict {
					g.mixedDicts[nm.Value] = true
					g.dictKeyStr[nm.Value] = false
					g.dictKeyInt[nm.Value] = false
					g.dictValStr[nm.Value] = false
					g.dictValInt[nm.Value] = false
				}
				g.heapSeq++
				hs := g.heapSeq
				g.emitRtAllocKind(b, fmt.Sprintf("%%h%d", hs), HeapKindDict, n.Span())
				for i := range dl.Keys {
					if mixedDict {
						// Put and tag in one call: an entry stored without its key tag would match
						// another key with the same payload, and an entry without its value tag
						// would print the kind the slot held last time. A position the pair road
						// bound brings its own tag register (roadmap L11.1, Gap R.146; ADR 0187), and a
						// key whose kind only that register can tell has to be asked whether it can be a
						// key at all before the store (Gap R.81).
						kk, kt, kPair, err := g.taggedSlotWords(b, dl.Keys[i])
						if err != nil {
							return err
						}
						if kPair {
							g.guardHashableTag(b, kt, dl.Src)
						}
						vv, vt, _, err := g.taggedSlotWords(b, dl.Vals[i])
						if err != nil {
							return err
						}
						b.WriteString(fmt.Sprintf("  call void @rt_dict_put_tagged(i32 %%h%d, i32 %s, i32 %s, i32 %s, i32 %s)\n", hs, kk, vv, kt, vt))
						continue
					}
					// A key or value the pair road bound (`d = {"k": n}`) brings its own tag register:
					// the element has no one word to store, and asking its `value()` is the refusal
					// Gap R.146 kept for this shape (roadmap L11.1; ADR 0187's payload-never-without-tag).
					kk, kIsStr, err := g.heapElemKind(b, dl.Keys[i])
					if err != nil {
						return err
					}
					vv, vIsStr, err := g.heapElemKind(b, dl.Vals[i])
					if err != nil {
						return err
					}
					if err := g.recordElemKind(nm.Value, "dict key", kIsStr, dl.Keys[i]); err != nil {
						return err
					}
					if err := g.recordElemKind(nm.Value, "dict value", vIsStr, dl.Vals[i]); err != nil {
						return err
					}
					bits := 0
					if kIsStr {
						bits |= 2
					}
					if vIsStr {
						bits |= 4
					}
					b.WriteString(fmt.Sprintf("  call void @rt_dict_put(i32 %%h%d, i32 %s, i32 %s)\n", hs, kk, vv))
					b.WriteString(fmt.Sprintf("  call void @rt_tag_elem(i32 %%h%d, i32 %d, i32 %d)\n", hs, i*2, g.elemTagFor(dl.Keys[i], kIsStr)))
					b.WriteString(fmt.Sprintf("  call void @rt_tag_elem(i32 %%h%d, i32 %d, i32 %d)\n", hs, i*2+1, g.elemTagFor(dl.Vals[i], vIsStr)))
					if bits != 0 {
						b.WriteString(fmt.Sprintf("  call void @rt_mark_estr(i32 %%h%d, i32 %d)\n", hs, bits))
					}
				}
				if mixedDict {
					b.WriteString(fmt.Sprintf("  call void @rt_mark_estr(i32 %%h%d, i32 8)\n", hs))
				}
				g.gcStoreHandle(b, fmt.Sprintf("%%h%d", hs), nm.Value)
				g.forgetTaggedBinding(nm.Value) // the container is the binding now (Gap R.142)
				return nil
			}
			if g.unionVars[nm.Value] {
				g.emitUnionStore(b, nm.Value, n.Value)
				g.forgetTaggedBinding(nm.Value) // the union's own tag word is the record now (Gap R.142)
				return nil
			}
			if g.storeModuleScalar(b, nm.Value, v) {
				// The module's state lives in a module global, so a body called later reads what
				// the module assigned by then -- which is what "look the name up when the call
				// runs" means once a frame's allocas are gone (ADR 0220, implemented compiled-side
				// by ADR 0227). A main-frame alloca could not do that.
				g.forgetTaggedBinding(nm.Value) // the module global holds the value now (Gap R.142)
				return nil
			}
			isFloat := g.isFloat(n.Value)
			// Whether the name had a slot before this statement is what separates the float that arrives
			// at an `i32` from the float that opens the variable: the second may choose a double slot, the
			// first cannot (roadmap L11.6, Gap R.155).
			hadSlot := g.allocd[nm.Value]
			if !g.allocd[nm.Value] {
				slotTy := "i32"
				if isFloat {
					slotTy = "double"
				}
				b.WriteString(fmt.Sprintf("  %%_%s = alloca %s\n", nm.Value, slotTy))
				g.allocd[nm.Value] = true
			}
			if isFloat {
				fv := g.floatValue(b, n.Value)
				// The variable was bound to a number and is being rebound to a double: the slot is the
				// `i32` that first binding chose, and a `store double` into it is an eight-byte write into
				// a four-byte allocation — which LLVM's opaque-pointer verifier cannot see, and which the
				// neighbour pays for (roadmap L11.6, Gap R.155, ADR 0274). The pair is the answer.
				if hadSlot && g.bindFloatRebinding(b, nm.Value, fv) {
					return nil
				}
				if g.doubleSlot == nil {
					g.doubleSlot = map[string]bool{}
				}
				g.doubleSlot[nm.Value] = true
				b.WriteString(fmt.Sprintf("  store double %s, double* %%_%s\n", fv, nm.Value))
				// A float is a word of its own, not a payload waiting for a tag: the pair the
				// last binding wrote has no reader left, and leaving it would have print ask
				// the stale tag instead of the double in the slot (Gap R.142).
				g.forgetTaggedBinding(nm.Value)
				if g.floatVars == nil {
					g.floatVars = map[string]bool{}
				}
				g.floatVars[nm.Value] = true
			} else {
				// Root the variable's slot so a heap handle (instance/list/dict/set
				// handle) stored here survives a later GC collection (Gap A / ADR
				// 0151). Only i32 slots are rooted; double slots hold floats.
				if !g.floatVars[nm.Value] && !isScalarConst(n.Value) {
					g.gcReg(b, nm.Value)
				}
				// Track the class of a variable assigned from a class instantiation.
				if call, ok := n.Value.(*Call); ok {
					if fn, ok2 := call.Fn.(*Name); ok2 {
						if _, isClass := g.classInfos[fn.Value]; isClass {
							if g.varClasses == nil {
								g.varClasses = map[string]string{}
							}
							g.varClasses[nm.Value] = fn.Value
						}
					}
				}
				// a generator call returns a runtime heap list handle: track the
				// target so print/for/indexing treat it as a list, not a scalar.
				// sorted(...)/reversed(...) return a handle the same way — without this,
				// `ys = sorted(xs)` assigned a handle to a plain int variable and
				// print(ys) printed the slot number (roadmap L11.7, ADR 0191).
				if call, ok := n.Value.(*Call); ok {
					if fn, ok2 := call.Fn.(*Name); ok2 {
						if g.genFuncs[fn.Value] {
							g.settleContainerKind(nm.Value, containerKindList)
						} else if fn.Value == "sorted" || fn.Value == "reversed" || fn.Value == "list" {
							g.settleContainerKind(nm.Value, containerKindList)
							// Inherit what the elements are, so the printer and the sort
							// comparator agree with the source container. The source is a NAME the
							// program filled earlier, or the LITERAL the ordering builtin was handed —
							// and beside a key= the literal is the only thing standing there, because
							// the door built the copy itself. Asking only the first case printed
							// `sorted(words, key=len)` as [1, 0]: the intern table's numbers, where the
							// reference prints the texts (roadmap L11.7, Gap R.264).
							if len(call.Args) > 0 {
								switch src := call.Args[0].(type) {
								case *Name:
									if g.listElemStr[src.Value] {
										g.listElemStr[nm.Value] = true
									}
									if g.mixedLists[src.Value] {
										g.mixedLists[nm.Value] = true
									}
								case *ListLit:
									if g.allElemsPrintAsText(src.Elems) {
										g.listElemStr[nm.Value] = true
									}
								}
							}
						}
					}
				}
				// A constant-folded comprehension lowers to a compile-time list
				// global (@.lstN). That is not an i32: storing it directly emits IR
				// LLVM's verifier rejects ("global variable reference must have
				// pointer type"), and print/len/append would read an address instead
				// of the container. Copy the folded elements into a runtime heap list
				// and track the target as a list, exactly as the literal path above.
				if ln, folded := g.staticLists[v]; folded {
					if g.listVars[nm.Value] || g.runtimeDicts[nm.Value] || g.runtimeSets[nm.Value] {
						g.emitFreeOld(b, nm.Value)
					}
					g.settleContainerKindFromLiteral(nm.Value, containerKindList)
					if !g.allocd[nm.Value] {
						b.WriteString(fmt.Sprintf("  %%_%s = alloca i32\n", nm.Value))
						g.gcReg(b, nm.Value)
						g.allocd[nm.Value] = true
					}
					h, herr := g.heapListFrom(b, ln, "")
					if herr != nil {
						return herr
					}
					g.gcStoreHandle(b, h, nm.Value)
					g.forgetTaggedBinding(nm.Value) // the container is the binding now (Gap R.142)
					return nil
				}
				b.WriteString(fmt.Sprintf("  store i32 %s, i32* %%_%s\n", v, nm.Value))
				// Every write to a possibly-unwritten slot sets its flag; a read that follows any of
				// them is then legitimate (ADR 0228).
				g.markBound(b, nm.Value)
				if g.floatVars != nil {
					delete(g.floatVars, nm.Value)
				}
				g.forgetTaggedBinding(nm.Value) // ADR 0172's latest-binding rule, tag and all (Gap R.142)
			}
		} else if attr, ok := n.Target.(*Attr); ok {
			// Instance attribute write: `self.x = v` / `inst.x = v`.
			if className := g.receiverClass(attr.Obj); className != "" {
				objHandle, err := g.value(b, attr.Obj)
				if err != nil {
					return err
				}
				v, err := g.value(b, n.Value)
				if err != nil {
					return err
				}
				g.heapUsed = true
				slot := g.attrSlot(attr.Name.Value)
				b.WriteString(fmt.Sprintf("  call void @rt_inst_put(i32 %s, i32 %d, i32 %s)\n", objHandle, slot, v))
				return nil
			}
			return fmt.Errorf("codegen: unsupported assignment target %T", n.Target)
		} else {
			return fmt.Errorf("codegen: unsupported assignment target %T", n.Target)
		}
	case *AugAssignStmt:
		// augmented assignment: read target, apply op with rhs, store back.
		binop := &BinOp{Op: n.Op, L: n.Target, R: n.Value}
		if nm, ok := n.Target.(*Name); ok {
			// `flag += 1` rebinds the name to arithmetic, and arithmetic is not a verdict:
			// the status has to go with the value it replaces, here as in the interpreter
			// (ADR 0172's rule, ADR 0257).
			g.forgetVarBool(nm.Value)
			// `n += 1` where n holds the answer of arithmetic over a slot: the left-hand side is a
			// (payload, tag) pair, so the sum is asked of the objects and the name is rebound with the
			// pair the answer arrived in — the same road the plain assignment takes, taken here
			// because `n += 1` is the assignment a program writes once it has the answer (roadmap
			// L11.1, Gap R.143).
			if handled, aerr := g.bindArithmeticPair(b, nm.Value, binop); aerr != nil {
				return aerr
			} else if handled {
				return nil
			}
			var v string
			var err error
			// `/=` is true division: the reference answers a float whatever arrives, so the double domain
			// is chosen by the operator and not by the two operands' kinds — `x = 8` / `x /= 2` is CPython's
			// `4.0`, and asking the integer road made it `4` with the exit code of success (roadmap L11.6,
			// Gap P.1; the same rule `/` takes in ADR 0253). Every other operator keeps the kinds it has
			// always read, because `//`, `%` and `*` answer an `int` for two `int`s — and a target that was
			// already a float was served by the double road before this row, which stays untouched.
			divToInt := n.Op == "/" && !g.isFloat(n.Target) && !g.isFloat(n.Value)
			floatResult := divToInt || g.isFloat(n.Target) || g.isFloat(n.Value)
			if floatResult {
				// The same rule as the plain assignment below: a float result belongs to the double
				// domain, so it is asked for there and not lowered twice — once into a dead `add i32`
				// and once into the `fadd` that stores it (roadmap Gap R.88, measured beside Gap R.96).
				g.doubleDomain++
				v = g.floatBinOp(b, binop)
				g.doubleDomain--
				if g.floatUnlowerable != "" {
					return g.floatLoweringRefusal(binop)
				}
				if v == "" {
					return g.floatOperandRefusal(n.Value)
				}
			} else {
				v, err = g.value(b, binop)
				if err != nil {
					return err
				}
			}
			if floatResult {
				// The answer is a double and the slot may be the `i32` the variable's first binding chose.
				// The pair is the answer there (floatbind.go, roadmap Gap R.155), which is also what retires
				// the refusal this road used to give: `t = 0` / `t += 1.5` asked for the tagged value word by
				// name, and the tagged value word is what it now gets (roadmap Gap R.88, Gap R.98's `+=` sink).
				// A name that was already a float took this road before the row landed and still does: the
				// rebinding below is asked only of a variable whose slot was made for an `i32`.
				if divToInt || g.allocd[nm.Value] {
					if g.bindFloatRebinding(b, nm.Value, v) {
						return nil
					}
				}
				if g.doubleSlot == nil {
					g.doubleSlot = map[string]bool{}
				}
				g.doubleSlot[nm.Value] = true
				if g.allocd[nm.Value] && !g.floatVars[nm.Value] && !g.unionVars[nm.Value] && !g.taggedVars[nm.Value] {
					// The variable's slot is an i32 and `+=` produced a double: `t = 0` then
					// `t += 1.5` is the assignment that changes a variable's kind, which needs the
					// (payload, tag) pair the tagged value word carries — storing a double into the
					// i32 slot is the module llc rejects (roadmap L11.1, Gap R.88).
					return fmt.Errorf("codegen: %q on %q writes a double into the i32 slot the variable already has: the first assignment decided its kind, and re-deciding it needs the (payload, tag) pair the tagged value word still owed here would carry — assign a float to give the variable one (roadmap L11.1, Gap R.88, ADR 0166)", n.Op, nm.Value)
				}
				if !g.allocd[nm.Value] {
					// The slot is the variable's, allocated once — `t = 0.0` then `t += 1.5` asked for
					// a second `%_t` and llc rejected the module for the duplicate (measured beside
					// roadmap Gap R.96; the plain-assignment branch above already asked `allocd`).
					b.WriteString(fmt.Sprintf("  %%_%s = alloca double\n", nm.Value))
					g.allocd[nm.Value] = true
				}
				b.WriteString(fmt.Sprintf("  store double %s, double* %%_%s\n", v, nm.Value))
				if g.floatVars == nil {
					g.floatVars = map[string]bool{}
				}
				g.floatVars[nm.Value] = true // `t += 1.5` as the first write had no table to note it in
			} else if !g.storeModuleScalar(b, nm.Value, v) {
				// A name the module rebinds has one slot, the global every read consults; writing a
				// main-frame `%_y` here is the store that made `llc` reject the module for a slot that
				// was never allocated (roadmap Gap R.49, ADR 0351).
				b.WriteString(fmt.Sprintf("  store i32 %s, i32* %%_%s\n", v, nm.Value))
			}
			return nil
		}
		if attr, ok := n.Target.(*Attr); ok {
			objHandle, err := g.value(b, attr.Obj)
			if err != nil {
				return err
			}
			v, err := g.value(b, binop)
			if err != nil {
				return err
			}
			// attrs are int-only in codegen; truncate a float result to i32.
			if g.isFloat(n.Value) {
				b.WriteString(fmt.Sprintf("  %%_aug = fptosi double %s to i32\n", v))
				v = "%_aug"
			}
			b.WriteString(fmt.Sprintf("  call void @rt_inst_put(i32 %s, i32 %d, i32 %s)\n", objHandle, g.attrSlot(attr.Name.Value), v))
			return nil
		}
		return fmt.Errorf("codegen: unsupported augmented-assignment target %T", n.Target)

	case *IfStmt:
		cond, cerr := g.truthyValue(b, n.Cond)
		if cerr != nil {
			return cerr
		}
		thenL := g.newLabel("if.then")
		endL := g.newLabel("if.end")
		var elseL string
		// build elif chain: each elif gets a cond label and a then label.
		type el struct {
			condL, thenL string
			e            *IfStmt
		}
		els := []el{}
		for _, e := range n.Elifs {
			els = append(els, el{g.newLabel("if.elif"), g.newLabel("if.elif.then"), e})
		}
		elseL = g.newLabel("if.else")
		// dispatch from top: cond -> then, else -> first elif cond (or else).
		firstTarget := elseL
		if len(els) > 0 {
			firstTarget = els[0].condL
		}
		b.WriteString(fmt.Sprintf("  br i1 %s, label %%%s, label %%%s\n", cond, thenL, firstTarget))
		b.WriteString(fmt.Sprintf("%s:\n", thenL))
		for _, s := range n.Then {
			if err := g.stmt(b, s); err != nil {
				return err
			}
		}
		b.WriteString(fmt.Sprintf("  br label %%%s\n", endL))
		// elif branches
		for i, e := range els {
			b.WriteString(fmt.Sprintf("%s:\n", e.condL))
			ec, err := g.truthOperandErr(b, e.e.Cond)
			if err != nil {
				return err
			}
			nextTarget := elseL
			if i+1 < len(els) {
				nextTarget = els[i+1].condL
			}
			b.WriteString(fmt.Sprintf("  br i1 %s, label %%%s, label %%%s\n", ec, e.thenL, nextTarget))
			b.WriteString(fmt.Sprintf("%s:\n", e.thenL))
			for _, s := range e.e.Then {
				if err := g.stmt(b, s); err != nil {
					return err
				}
			}
			b.WriteString(fmt.Sprintf("  br label %%%s\n", endL))
		}
		b.WriteString(fmt.Sprintf("%s:\n", elseL))
		for _, s := range n.Else {
			if err := g.stmt(b, s); err != nil {
				return err
			}
		}
		b.WriteString(fmt.Sprintf("  br label %%%s\n", endL))
		b.WriteString(fmt.Sprintf("%s:\n", endL))
	case *MatchStmt:
		sub, err := g.matchSubjectOperand(b, n.Subject)
		if err != nil {
			return err
		}
		endL := g.newLabel("match.end")
		for i, c := range n.Cases {
			bodyL := g.newLabel("match.case")
			fallL := endL
			if i < len(n.Cases)-1 {
				fallL = g.newLabel("match.next")
			}
			// The captures of the arm that runs are written in the block the branch enters, and only by
			// that arm: deciding which arm matches is not binding anything (Gap R.68, ADR 0358).
			savedBinds := g.matchBinds
			g.matchBinds = nil
			patterns := append([]Expr{c.Pattern}, c.Or...)
			if len(patterns) == 1 {
				pc, perr := g.matchPattern(b, sub, patterns[0])
				if perr != nil {
					return perr
				}
				b.WriteString(fmt.Sprintf("  br i1 %s, label %%%s, label %%%s\n", pc, bodyL, fallL))
				b.WriteString(fmt.Sprintf("%s:\n", bodyL))
				// The arm is the one that matched: this is where its captures bind.
				g.emitArmCaptures(b)
			} else {
				// An arm written `case p0 | p1 | …` is decided one alternative at a time, and each
				// alternative enters the arm's body carrying ITS captures. Folding the alternatives into one
				// `or i1` and writing every alternative's captures made a name bound by the alternative that
				// did not match answer the value the other one computed — `case [a] | {"k": a}:` over [3]
				// printed the interred `k`, because the mapping pattern's `rt_dict_get` ran last and won.
				// One arm block cannot hold that, since the stores differ per alternative.
				for k, p := range patterns {
					altFalse := g.newLabel("match.alt")
					last := k == len(patterns)-1
					if last {
						altFalse = fallL
					}
					pc, perr := g.matchPattern(b, sub, p)
					if perr != nil {
						return perr
					}
					enterL := g.newLabel("match.alt.enter")
					b.WriteString(fmt.Sprintf("  br i1 %s, label %%%s, label %%%s\n", pc, enterL, altFalse))
					b.WriteString(fmt.Sprintf("%s:\n", enterL))
					g.emitArmCaptures(b) // this alternative's captures, and no other's
					b.WriteString(fmt.Sprintf("  br label %%%s\n", bodyL))
					if !last {
						b.WriteString(fmt.Sprintf("%s:\n", altFalse))
					}
				}
				b.WriteString(fmt.Sprintf("%s:\n", bodyL))
			}
			g.matchBinds = savedBinds
			if c.Guard != nil {
				gok, gerr := g.truthyValue(b, c.Guard)
				if gerr != nil {
					return gerr
				}
				bodyAfter := g.newLabel("match.case.body")
				b.WriteString(fmt.Sprintf("  br i1 %s, label %%%s, label %%%s\n", gok, bodyAfter, fallL))
				b.WriteString(fmt.Sprintf("%s:\n", bodyAfter))
			}
			for _, s := range c.Body {
				if err := g.stmt(b, s); err != nil {
					return err
				}
			}
			b.WriteString(fmt.Sprintf("  br label %%%s\n", endL))
			if i < len(n.Cases)-1 {
				b.WriteString(fmt.Sprintf("%s:\n", fallL))
			}
		}
		b.WriteString(fmt.Sprintf("%s:\n", endL))
	case *WhileStmt:
		condL := g.newLabel("while.cond")
		bodyL := g.newLabel("while.body")
		elseL := g.newLabel("while.else")
		endL := g.newLabel("while.end")
		b.WriteString(fmt.Sprintf("  br label %%%s\n", condL))
		b.WriteString(fmt.Sprintf("%s:\n", condL))
		cond, cerr := g.truthyValue(b, n.Cond)
		if cerr != nil {
			return cerr
		}
		// normal completion (cond false) enters else if present; break skips else
		normalL := endL
		if len(n.Else) > 0 {
			normalL = elseL
		}
		b.WriteString(fmt.Sprintf("  br i1 %s, label %%%s, label %%%s\n", cond, bodyL, normalL))
		b.WriteString(fmt.Sprintf("%s:\n", bodyL))
		// A `while` allocates in its body, so its body is where the collector gets a turn.
		g.loopSafePoint(b, n.Body)
		g.loopStack = append(g.loopStack, loopInfo{breakLabel: endL, continueLabel: condL})
		for _, s := range n.Body {
			if err := g.stmt(b, s); err != nil {
				return err
			}
		}
		g.loopStack = g.loopStack[:len(g.loopStack)-1]
		b.WriteString(fmt.Sprintf("  br label %%%s\n", condL))
		if len(n.Else) > 0 {
			b.WriteString(fmt.Sprintf("%s:\n", elseL))
			for _, s := range n.Else {
				if err := g.stmt(b, s); err != nil {
					return err
				}
			}
			b.WriteString(fmt.Sprintf("  br label %%%s\n", endL))
		}
		b.WriteString(fmt.Sprintf("%s:\n", endL))
	case *ForStmt:
		// The iterable is asked what it is BEFORE any road is chosen for it (roadmap Gap R.246, ADR 0339).
		// Three questions, in this order, because each is cheaper and narrower than the next, and the last
		// one is the door every other `for` has always gone through:
		//
		//   1. can the compiler WRITE the container down? `sorted`/`reversed` over a literal list, a literal
		//      text reversed, either nested in the other — the fold hands the literal road the elements in the
		//      order the reference yields them, through the same function the `print` answer uses (ADR 0258).
		//   2. can the compiler NAME the container the builtin denotes? `sorted(xs)`, `reversed(xs)`,
		//      `sorted(b())` — the copy @rt_sort built or @rt_list_copy + @rt_reverse flipped is rooted in a
		//      slot and the loop walks it exactly as it walks a list variable: one road for a list, whichever
		//      builtin produced it (ADR 0333's pattern, applied to the two builtins that hand back a list).
		//   3. otherwise the container door beside this, for the container a user function built.
		//
		// Before these two questions the loop had no way to ask: a `sorted`/`reversed` fell to the counter
		// road, which read the iterable's word as a repeat count — `%t1 = icmp slt i32 %_ctr1.ld1, @.lst1`,
		// which `llc` rejects (exit 2, the code ADR 0166 reserves for a compiler bug), or succeeded and
		// printed the counter, or printed nothing at all.
		// `key=None` is the reference's identity key rather than a callable argument, and an option that
		// means "no key" has to be gone BEFORE the questions below: the fold asks the shape of the
		// builtin's argument list, and a no-op option sitting in it would decline a program the builtin
		// itself answers (roadmap Gap R.249, ADR 0340).
		n.Iter = normaliseIdentityKey(n.Iter)
		if folded, okFold := g.foldedForIterable(n.Iter); okFold {
			n.Iter = folded
		} else if named, okNamed := g.materializeSortedReversedForLoop(b, n.Iter); okNamed {
			n.Iter = named
		} else if ansName, _, okAns := g.containerAnswerOperandRead(b, n.Iter, containerReadWalk); okAns {
			// `for v in build():` — the iterable is the container the callee built. The loop road below reads a
			// container by name, which is what the container door hands over: the call is emitted once, the
			// answer is rooted for the collector, and the loop runs over the answer as over any other container
			// the program named (roadmap Gap R.67, ADR 0333).
			n.Iter = ansName
		} else if fnName, refused := g.containerAnswerReadIsRefused(n.Iter, containerReadWalk); refused {
			// ADR 0339's rule, applied to an answer rather than to a variable: a loop asks its iterable what
			// it is, and a container whose members this table cannot describe is refused — never read as a
			// count, never walked slot by slot for whatever word the allocator left there (roadmap Gap R.261).
			return containerAnswerUndescribedRefusal(fnName)
		}
		// `for x in [1, 2, 3]`: iterate an inline list literal's constant
		// elements by unrolling one body block per element. `break` skips the
		// `else`, `continue` advances to the next element; after the last
		// element normal completion enters `else` if present (like Python).
		// `for x in "str"`: unroll each rune as a single-char string literal.
		if sl, ok := n.Iter.(*StrLit); ok {
			var elems []Expr
			for _, r := range sl.Value {
				elems = append(elems, &StrLit{Value: string(r)})
			}
			n.Iter = &ListLit{Elems: elems}
		}
		if ll, ok := n.Iter.(*ListLit); ok {
			endL := g.newLabel("for.end")
			elseL := g.newLabel("for.else")
			normalL := endL
			if len(n.Else) > 0 {
				normalL = elseL
			}
			if !g.allocd[loopVarName(n.Var)] {
				b.WriteString(fmt.Sprintf("  %%_%s = alloca i32\n", loopVarName(n.Var)))
				g.gcReg(b, loopVarName(n.Var))
				g.allocd[loopVarName(n.Var)] = true
			}
			var contL string
			loopSlot := loopVarName(n.Var)
			prevInterned, wasInterned := g.internedVars[loopSlot]
			for _, el := range ll.Elems {
				// Each element is lowered the way a heap container slot is, not the way a
				// scalar is: a string element becomes its @str_tab index. Handing `g.value`
				// a StrLit returned the raw `@.strN` global, so an unrolled loop over text
				// emitted `store i32 @.str1, i32* %_c` — a global in an i32 slot, which llc
				// rejects and the exit-code contract calls a compiler bug (roadmap Gap R.15,
				// ADR 0208; the same rule Gap I.2 installed for container writes).
				v, isStr, err := g.heapElemKind(b, el)
				if err != nil {
					return err
				}
				// The body copy emitted for *this* element then reads the loop variable as
				// text or as a number, which is what lets a mixed literal print correctly.
				if isStr {
					g.internedVars[loopSlot] = true
					g.releaseDigitLoopVar(loopSlot)
				} else {
					delete(g.internedVars, loopSlot)
					if _, isInt := el.(*IntLit); isInt {
						// This element IS a number the program wrote, for this pass through the loop
						// only (roadmap L11.1, ADR 0359).
						g.claimDigitLoopVar(loopSlot)
					} else {
						g.releaseDigitLoopVar(loopSlot)
					}
				}
				// The element decides what the loop variable is, exactly as it decides whether
				// it is text: a name that once held a verdict is bound by the iterable to a
				// number and prints as that number — ADR 0172's latest-binding rule, carried to
				// bools by ADR 0257.
				g.forgetVarBool(loopSlot)
				// And the tag says what the element is, which is the question neither of those two
				// answers covers for a float (whose payload is a box handle) or None (whose payload
				// is nothing): printing the loop variable showed 0 where Python shows 1.5 and None
				// (roadmap L11.1, ADR 0233).
				if g.loopElemTag == nil {
					g.loopElemTag = map[string]int32{}
				}
				g.loopElemTag[loopSlot] = g.elemTagFor(el, isStr)
				bodyL := g.newLabel("for.list.body")
				contL = g.newLabel("for.list.cont")
				b.WriteString(fmt.Sprintf("  store i32 %s, i32* %%_%s\n", v, loopVarName(n.Var)))
				// The loop variable is a binding like any other: a module name that some body reads
				// lives in its global, and the alloca above (rooted, because an element can be a handle)
				// is not the slot the reads consult (roadmap Gap R.49, ADR 0351).
				g.storeModuleScalar(b, loopVarName(n.Var), v)
				b.WriteString(fmt.Sprintf("  br label %%%s\n", bodyL))
				b.WriteString(fmt.Sprintf("%s:\n", bodyL))
				// An unrolled loop mints no container for the elements it walks, but its body allocates every
				// iteration exactly as a `while`'s does, and nothing else collects inside it.
				g.loopSafePoint(b, n.Body)
				g.loopStack = append(g.loopStack, loopInfo{breakLabel: endL, continueLabel: contL})
				for _, s := range n.Body {
					if err := g.stmt(b, s); err != nil {
						return err
					}
				}
				g.loopStack = g.loopStack[:len(g.loopStack)-1]
				b.WriteString(fmt.Sprintf("  br label %%%s\n", contL))
				b.WriteString(fmt.Sprintf("%s:\n", contL))
			}
			// last cont block (continue on the final element) reaches normal
			// completion, entering `else` when present.
			b.WriteString(fmt.Sprintf("  br label %%%s\n", normalL))
			if len(n.Else) > 0 {
				b.WriteString(fmt.Sprintf("%s:\n", elseL))
				for _, s := range n.Else {
					if err := g.stmt(b, s); err != nil {
						return err
					}
				}
				b.WriteString(fmt.Sprintf("  br label %%%s\n", endL))
			}
			b.WriteString(fmt.Sprintf("%s:\n", endL))
			// The loop variable stops existing as a loop variable here: restore whatever
			// the name meant before this loop, rather than leaving it interned for the
			// rest of the function.
			if wasInterned {
				g.internedVars[loopSlot] = prevInterned
			} else {
				delete(g.internedVars, loopSlot)
			}
			delete(g.loopElemTag, loopSlot) // the loop variable is a plain name again (ADR 0233)
			return nil
		}
		// runtime heap list iterable: a generator-call result or a tracked
		// list variable. Iterate positions 0..len-1 and bind the loop
		// variable to each element via rt_get_elem.
		loopVar := loopVarName(n.Var)
		// Runtime heap container iterables. A list (or generator result) yields its
		// elements and a set its members; a dict yields its KEYS in insertion order,
		// which the runtime stores as [key, value] pairs — so entry i's key lives at
		// 2*i. Iterating anything else falls through to the range path.
		iterKind := "" // "list" | "set" | "dict" | "tagged"
		mixedIter := false
		var hVal string
		var iterTag string
		if name, ok := n.Iter.(*Name); ok {
			switch {
			case g.mixedLists[name.Value]:
				// Iterating a mixed list binds the loop variable to an element *and* its
				// tag, so print(x) can dispatch per iteration (ADR 0185).
				iterKind = "list"
				mixedIter = true
			case g.listVars[name.Value]:
				iterKind = "list"
			case g.mixedSets[name.Value]:
				// A mixed set binds payload and tag like a mixed list: the member's i32 is
				// ambiguous on its own, and the tag is what prints it (ADR 0232).
				iterKind = "set"
				mixedIter = true
			case g.mixedDicts[name.Value]:
				// A dict yields keys, so it is the key slots (stride 2) whose tags travel.
				iterKind = "dict"
				mixedIter = true
			case g.runtimeSets[name.Value]:
				iterKind = "set"
			case g.runtimeDicts[name.Value]:
				iterKind = "dict"
			}
		} else if call, ok := n.Iter.(*Call); ok && g.forIterCallIsGenerator(call) {
			if fn, ok2 := call.Fn.(*Name); ok2 {
				if g.genFuncs[fn.Value] {
					iterKind = "list"
				}
			}
		} else if sl, ok := n.Iter.(*SetLit); ok {
			// `for v in {1, 2}` is a container the program wrote where a variable would stand: it is
			// built as an object and walked by the runtime, exactly like the variable form. Falling
			// through to the range path did two wrong things — it compared the counter against @.set1,
			// a global in an i32 slot, which llc refuses and the exit-code contract charges the
			// compiler with (ADR 0166), and it bound the loop variable to the counter, which would
			// have printed 0, 1 where CPython prints 1, 2 (roadmap L11.1).
			iterKind = "set"
			mixedIter = g.taggableMixedSet(sl)
		} else if dl, ok := n.Iter.(*DictLit); ok {
			// A dict literal yields its keys, in insertion order, like the dict it is (ADR 0188).
			iterKind = "dict"
			mixedIter = g.taggableMixedDict(dl)
		} else if h, kind, ok := g.containerHandleOf(b, n.Iter); ok {
			// `for v in xs[0]` — the iterable is a container the program can name, reached through a
			// slot rather than standing where a literal would. The slot's payload is the handle of the
			// inner object, and only its tag says so: containerHandleOf asks that question and refuses
			// when the slot holds a number pretending to be one. The loop then walks that object the way
			// it walks any list, and the tag travels with each element so print(v) dispatches per
			// iteration — text, float, None, a nested container (roadmap L11.1, ADR 0241).
			iterKind = kind
			mixedIter = true
			hVal = h
		} else if ix, isRead := n.Iter.(*Index); isRead {
			// The iterable is a slot the program *built*, so nothing says which kind of object sits there:
			// `xs = []` / `xs.append([7, 8])` / `for v in xs[0]`, and the same shape through a dict key.
			// Before this arm the loop chose its helper by the kind the compiler had written down for the
			// container and walked the wrong object entirely — `d = {}` / `d["a"] = [1, 2]` printed a single
			// `0` for CPython's `1` and `2`, and a text slot printed nothing at all, each at the exit code of
			// success. Now the tag the writer left beside the payload chooses, per iteration, through
			// @rt_iter_len / @rt_iter_elem / @rt_iter_tag, and a slot holding a number, a verdict or None
			// raises the sentence the reference raises rather than being stepped over as if it were a
			// container (roadmap L11.1, Gap R.95, ADR 0251's rule for this second caller).
			if h, t, okPair := g.builtSlotPair(b, ix); okPair && h != "" && t != "" {
				iterKind = "tagged"
				mixedIter = true
				hVal = h
				iterTag = t
			}
		}
		if iterKind != "" {
			// A loop over a container of interned strings binds the loop variable to an
			// index into @str_tab; recording that keeps print(x) rendering the text instead
			// of the index (roadmap Gap I.2). Dict iteration yields keys, so it is the key
			// kind that matters.
			if mixedIter {
				// The tag decides per iteration; a static claim here would print every
				// member through one kind, which is the bug the tag exists to prevent.
				delete(g.internedVars, loopVar)
				delete(g.noneVars, loopVar)
			}
			if name, ok := n.Iter.(*Name); ok {
				// The claim is made from THIS iterable and taken back when this iterable's elements
				// are not text. Setting it only one way left the name interned for every later loop in
				// the function: `for v in words:` (text) followed by `for v in xs:` (numbers) printed
				// `b`, `a`, `a`, `(null)` for the reference's `b`, `a`, `1`, `2`, because the second
				// loop never asked its own iterable what it held — the first loop's answer was still
				// on the name. Asking per loop is what this row's question means (roadmap Gap R.246,
				// ADR 0339; the same rule the unrolled road beside it already follows).
				switch iterKind {
				case "list", "set":
					if mixedIter {
						// left alone above: the tag decides
						g.releaseDigitLoopVar(loopVar)
					} else if g.listElemStr[name.Value] || g.setElemStr[name.Value] {
						g.internedVars[loopVar] = true
						g.releaseDigitLoopVar(loopVar)
					} else if g.listElemInt[name.Value] || g.setElemInt[name.Value] {
						// The container records its elements as integers, so the name this loop binds
						// holds one — the claim the digits road needs and no spelling can give
						// (roadmap L11.1, ADR 0359; Gap R.246's per-loop question).
						g.claimDigitLoopVar(loopVar)
						delete(g.internedVars, loopVar)
					} else {
						delete(g.internedVars, loopVar)
						g.releaseDigitLoopVar(loopVar)
					}
				case "dict":
					if mixedIter {
						g.releaseDigitLoopVar(loopVar)
					} else if g.dictKeyStr[name.Value] {
						g.internedVars[loopVar] = true
						g.releaseDigitLoopVar(loopVar)
					} else if g.dictKeyInt[name.Value] {
						g.claimDigitLoopVar(loopVar)
						delete(g.internedVars, loopVar)
					} else {
						delete(g.internedVars, loopVar)
						g.releaseDigitLoopVar(loopVar)
					}
				}
			} else if !mixedIter {
				// The same claim for a literal iterable, asked of its elements: a loop over
				// {"a", "b"} binds interned text, and printing the loop variable without that
				// records the interned index instead of the text (Gap I.2).
				var keys []Expr
				switch it := n.Iter.(type) {
				case *SetLit:
					keys = it.Elems
				case *DictLit:
					keys = it.Keys
				}
				if keys != nil && len(keys) > 0 {
					allText := true
					for _, k := range keys {
						// isStringExpr because a literal key is text the program wrote, and
						// printsAsInternedStr because a key expression can hand back an index too.
						if !isStringExpr(k) && !g.printsAsInternedStr(k) {
							allText = false
							break
						}
					}
					if allText {
						g.internedVars[loopVar] = true
					}
				}
			}
			lenFn := "rt_list_len"
			elemStride := 1
			switch iterKind {
			case "set":
				lenFn = "rt_set_len"
			case "dict":
				lenFn = "rt_dict_len"
				elemStride = 2 // keys only
			case "tagged":
				// Neither question is answered by a table: the count, the word and the kind all come from the
				// object the slot's tag names, and the stride the dict's keys live at is the runtime helper's
				// business because the compiler cannot say which stride it is (Gap R.95).
				lenFn = ""
				elemStride = 1
			}
			if hVal == "" {
				// The iterable is asked for its *handle*. g.value answers a literal with its folded
				// global, whose layout is a length plus an array, so `for v in {1, 2}` compared the
				// counter against @.set1 — a global in an i32 slot, which llc refuses and the
				// exit-code contract then charges the compiler with for an ordinary program (ADR 0166).
				// A container variable already holds a handle, which is why only the literal leg
				// broke. Anything the container rule does not cover — a generator call's result is
				// the live one — already carries a handle from its own lowering (ADR 0192).
				var hv string
				var err error
				if g.isContainerExpr(n.Iter) {
					hv, err = g.containerOperand(b, n.Iter)
				} else {
					hv, err = g.value(b, n.Iter)
				}
				if err != nil {
					return err
				}
				hVal = hv
			}
			lenT := g.newTmp()
			if lenFn == "" {
				lenT = g.iterLenUnderTag(b, hVal, iterTag, n.Span())
			} else {
				b.WriteString(fmt.Sprintf("  %s = call i32 @%s(i32 %s)\n", lenT, lenFn, hVal))
			}
			idxVar := g.newTmp()
			b.WriteString(fmt.Sprintf("  %s = alloca i32\n", idxVar))
			b.WriteString(fmt.Sprintf("  store i32 0, i32* %s\n", idxVar))
			// The loop variable's slot must be allocated here, in the block that
			// branches into the loop: an alloca emitted inside the body block does
			// not dominate the blocks after the loop, so a second loop reusing the
			// same variable name (`for k in d:` … `for k in m:`) failed the verifier
			// with "Instruction does not dominate all uses".
			if !g.allocd[loopVar] {
				b.WriteString(fmt.Sprintf("  %%%s = alloca i32\n", "_"+loopVar))
			}
			g.allocd[loopVar] = true
			// The handle the loop walks never changes across the back edge, and the body now collects: park
			// it in a rooted slot here, in the block that branches into the loop, where the `alloca` is minted
			// once. A slot the collector has never been told about is invisible to the mark phase, and the
			// sweep would recycle the very container the loop is still indexing (ADR 0181; roadmap Gap Q.2).
			g.gcRootIterable(b, hVal)
			condL := g.newLabel("for.list.cond")
			bodyL := g.newLabel("for.list.body")
			incL := g.newLabel("for.list.inc")
			elseL := g.newLabel("for.list.else")
			endL := g.newLabel("for.list.end")
			b.WriteString(fmt.Sprintf("  br label %%%s\n", condL))
			b.WriteString(fmt.Sprintf("%s:\n", condL))
			ild := g.newTmp()
			b.WriteString(fmt.Sprintf("  %s = load i32, i32* %s\n", ild, idxVar))
			cmp := g.newTmp()
			b.WriteString(fmt.Sprintf("  %s = icmp slt i32 %s, %s\n", cmp, ild, lenT))
			b.WriteString(fmt.Sprintf("  br i1 %s, label %%%s, label %%%s\n", cmp, bodyL, elseL))
			b.WriteString(fmt.Sprintf("%s:\n", bodyL))
			pos := ild
			if elemStride != 1 {
				// dict entries occupy two words: walk the key slots.
				scaled := g.newTmp()
				b.WriteString(fmt.Sprintf("  %s = mul i32 %s, %d\n", scaled, ild, elemStride))
				pos = scaled
			}
			elemT := g.newTmp()
			if iterKind == "tagged" {
				b.WriteString(fmt.Sprintf("  %s = call i32 @rt_iter_elem(i32 %s, i32 %s, i32 %s)\n", elemT, hVal, iterTag, ild))
			} else {
				b.WriteString(fmt.Sprintf("  %s = call i32 @rt_get_elem(i32 %s, i32 %s)\n", elemT, hVal, pos))
			}
			b.WriteString(fmt.Sprintf("  store i32 %s, i32* %%_%s\n", elemT, loopVar))
			if mixedIter {
				// The companion tag slot is what makes the loop variable printable: its
				// i32 alone is ambiguous (a number, or an index into the string table),
				// and only the tag says which (ADR 0185).
				if !g.allocd[loopVar+"_tag"] {
					b.WriteString(fmt.Sprintf("  %%_%s_tag = alloca i32\n", loopVar))
					g.allocd[loopVar+"_tag"] = true
				}
				tagT := g.newTmp()
				if iterKind == "tagged" {
					b.WriteString(fmt.Sprintf("  %s = call i32 @rt_iter_tag(i32 %s, i32 %s, i32 %s)\n", tagT, hVal, iterTag, ild))
				} else {
					b.WriteString(fmt.Sprintf("  %s = call i32 @rt_tag_of(i32 %s, i32 %s)\n", tagT, hVal, pos))
				}
				b.WriteString(fmt.Sprintf("  store i32 %s, i32* %%_%s_tag\n", tagT, loopVar))
				g.taggedVars[loopVar] = true
				if g.taggedOrigin == nil {
					g.taggedOrigin = map[string]string{}
				}
				// Recorded so a refusal this variable may meet later can say "a loop stepped over it"
				// truthfully, rather than a fallback sentence guessing at a loop the program never wrote
				// (roadmap Gap R.38, L11.1).
				g.taggedOrigin[loopVar] = taggedOriginLoop
			}
			// The container is walked by handle from a register and the element is bound to a slot neither of
			// them rooted, so both are parked before the body's collection can reach them: an object whose only
			// remaining reference is a register — or an untagged slot — is invisible to the mark phase, and the
			// sweep would recycle the list the loop is still indexing and the element it just bound (ADR 0181's
			// rule, applied one construct later; roadmap Gap Q.2, ADR 0347).
			// The element the road just bound lives in a slot nobody rooted, and container elements are not
			// marked transitively (L11.1), so the slot is registered before the body's collection can sweep
			// what the loop has in hand. Pushing on every iteration is the established shape — the runtime
			// dedups by slot address — while the loop-invariant container handle is parked in the preheader
			// below, where its `alloca` is minted once and not once per iteration (ADR 0181; roadmap Gap Q.2,
			// ADR 0347).
			g.gcReg(b, loopVar)
			g.loopSafePoint(b, n.Body)
			g.loopStack = append(g.loopStack, loopInfo{breakLabel: endL, continueLabel: incL})
			for _, s := range n.Body {
				if err := g.stmt(b, s); err != nil {
					return err
				}
			}
			g.loopStack = g.loopStack[:len(g.loopStack)-1]
			b.WriteString(fmt.Sprintf("  br label %%%s\n", incL))
			b.WriteString(fmt.Sprintf("%s:\n", incL))
			ild2 := g.newTmp()
			b.WriteString(fmt.Sprintf("  %s = load i32, i32* %s\n", ild2, idxVar))
			i2 := g.newTmp()
			b.WriteString(fmt.Sprintf("  %s = add i32 %s, 1\n", i2, ild2))
			b.WriteString(fmt.Sprintf("  store i32 %s, i32* %s\n", i2, idxVar))
			b.WriteString(fmt.Sprintf("  br label %%%s\n", condL))
			b.WriteString(fmt.Sprintf("%s:\n", elseL))
			if n.Else != nil {
				for _, s := range n.Else {
					if err := g.stmt(b, s); err != nil {
						return err
					}
				}
			}
			b.WriteString(fmt.Sprintf("  br label %%%s\n", endL))
			b.WriteString(fmt.Sprintf("%s:\n", endL))
			return nil
		}
		initL := g.newLabel("for.init")
		condL := g.newLabel("for.cond")
		bodyL := g.newLabel("for.body")
		incL := g.newLabel("for.inc")
		elseL := g.newLabel("for.else")
		endL := g.newLabel("for.end")
		b.WriteString(fmt.Sprintf("  br label %%%s\n", initL))
		b.WriteString(fmt.Sprintf("%s:\n", initL))
		// Iterating text that only exists at run time is not implementable in this backend,
		// and the fall-through below would not refuse it: `def txt(): return "hi"` hands back
		// its @str_tab index, which the count path reads as a repeat count — so
		// `for c in txt(): print(c)` compiled cleanly and printed nothing, where the
		// interpreter and CPython print `h i`. A silent wrong answer is worse than a refusal,
		// so the shape is named instead (roadmap Gap R.16, ADR 0209). A string *literal*
		// iterates fine — it became a list of one-rune literals above (ADR 0208).
		if g.iterableIsRuntimeString(n.Iter) {
			// The refusal used to stand here because the fall-through read a string's table
			// index as a repeat count and printed nothing — a silent wrong answer. The table
			// now supplies the count and each character, so the loop is the counter loop with
			// a code-point index for an element (ADR 0229, ADR 0196).
			return g.emitForOverRuntimeString(b, n)
		}
		// The same silent-wrong-answer fall-through, one kind over: the iterable is a CONTAINER that neither
		// the unrolled literal road nor the runtime container road took (`sorted(xs)` of a named list, a
		// `sorted` of a call's answer, a `sorted` of a slice copy). The counter road below compares its
		// counter with the iterable's word and binds the loop variable to the COUNTER, so the program printed
		// `0` once (`for v in sorted([3,1,2][0:2])`, the reference `1`, `3`) or printed nothing at all
		// (`for v in sorted(["b","a"])`, the reference `a`, `b`) at the exit code of success. Refusing is the
		// honest answer for what the backend cannot walk, and the sentence names the half that is missing
		// (roadmap Gap R.246, ADR 0339; ADR 0166, ADR 0249).
		if g.forIterableIsUnwalkableContainer(n.Iter) {
			return fmt.Errorf("%s", g.forIterableRefusal(n.Iter))
		}
		start, stop, step, err := g.rangeBounds(b, n.Iter)
		if err != nil {
			return err
		}
		if !g.allocd[loopVarName(n.Var)] {
			b.WriteString(fmt.Sprintf("  %%_%s = alloca i32\n", loopVarName(n.Var)))
			// A counter loop's variable holds a NUMBER, so its slot is tagged not-a-handle rather than
			// pushed as one: a root entry whose value is the integer 409 keeps alive whatever object lives at
			// slot 409, and the 409th list of a churn loop is exactly what the collector was meant to take
			// (roadmap Gap Q.2, ADR 0347 — the same tag-blindness the collector's element pass just fixed).
			g.gcClearRoot(b, loopVarName(n.Var))
			g.allocd[loopVarName(n.Var)] = true
		}
		// A counter is a number, so the loop variable holds a number: the text claim a PREVIOUS loop
		// left on the same name is withdrawn here, as the container road beside this withdraws it when
		// its own iterable is not a list of text. Without it, `for v in words:` (text) followed by
		// `for v in range(2):` printed the STRING TABLE — `b`, `a` — for the reference's `0`, `1`, at
		// the exit code of success: the second loop never asked its own iterable what it held
		// (roadmap Gap R.246, ADR 0339; the claim is Gap I.2's, the rule for taking it back is this row's).
		delete(g.internedVars, loopVar)
		// The induction counter is the loop's own, never the loop variable's slot.
		// Sharing them made the variable answer the *bound* after the loop —
		// `for i in range(3): print(i)` and then `print(i)` gave 3 where Python and
		// the interpreter give 2 — and worse, an assignment to the loop variable in
		// the body moved the iteration, so `for i in range(3): i = i * 100` ran twice
		// and answered 101. The variable is bound from the counter at the top of each
		// body, which is when Python binds it (Gap R.3b, ADR 0196).
		g.forCtrSeq++
		ctr := fmt.Sprintf("_ctr%d", g.forCtrSeq)
		b.WriteString(fmt.Sprintf("  %%%s = alloca i32\n", ctr))
		b.WriteString(fmt.Sprintf("  store i32 %s, i32* %%%s\n", start, ctr))
		b.WriteString(fmt.Sprintf("  br label %%%s\n", condL))
		b.WriteString(fmt.Sprintf("%s:\n", condL))
		g.ldN++
		cld := fmt.Sprintf("%%%s.ld%d", ctr, g.ldN)
		b.WriteString(fmt.Sprintf("  %s = load i32, i32* %%%s\n", cld, ctr))
		t := g.newTmp()
		cmpOp := "slt"
		if strings.HasPrefix(step, "-") {
			cmpOp = "sgt"
		}
		b.WriteString(fmt.Sprintf("  %s = icmp %s i32 %s, %s\n", t, cmpOp, cld, stop))
		// normal completion (i >= stop) enters else if present; break skips else
		normalL := endL
		if len(n.Else) > 0 {
			normalL = elseL
		}
		b.WriteString(fmt.Sprintf("  br i1 %s, label %%%s, label %%%s\n", t, bodyL, normalL))
		b.WriteString(fmt.Sprintf("%s:\n", bodyL))
		// Python binds the loop variable to each element as the loop produces it, so
		// the store to the user's name happens here and nowhere else: what the body
		// writes to it is overwritten by the next element, and survives the loop only
		// as the last value actually bound.
		g.ldN++
		cbd := fmt.Sprintf("%%%s.ld%d", ctr, g.ldN)
		b.WriteString(fmt.Sprintf("  %s = load i32, i32* %%%s\n", cbd, ctr))
		b.WriteString(fmt.Sprintf("  store i32 %s, i32* %%_%s\n", cbd, loopVarName(n.Var)))
		// …and a module name that some body reads is bound in its global too, because that is the slot
		// the read road consults (roadmap Gap R.49, ADR 0351).
		g.storeModuleScalar(b, loopVarName(n.Var), cbd)
		// The name now holds the counter, a number: whatever the slot carried before — including a root
		// entry this loop registered on an earlier shape — stops being a handle the collector may follow
		// (ADR 0181's root/tag pairing; roadmap Gap Q.2, ADR 0347).
		g.gcClearRoot(b, loopVarName(n.Var))
		// The counter the iterator produced is what the name holds for this pass, verdict
		// status included: it is a number however the name began (ADR 0172, ADR 0257).
		g.forgetVarBool(loopVarName(n.Var))
		// The loop bound this name from the counter: a number the compiler can see, for as long as
		// this body is being lowered. Taken back below, so the claim never outlives the loop that
		// earned it (roadmap Gap R.252's rule, ADR 0339's per-loop question; ADR 0359 reads it where
		// an interpolated field needs the field's digits).
		g.claimDigitLoopVar(loopVarName(n.Var))
		// A counter loop's back edge carries numbers only — the counter slot and the loop variable, both
		// rooted — so its body is a safe point with nothing further to park.
		g.loopSafePoint(b, n.Body)
		g.loopStack = append(g.loopStack, loopInfo{breakLabel: endL, continueLabel: incL})
		for _, s := range n.Body {
			if err := g.stmt(b, s); err != nil {
				return err
			}
		}
		g.releaseDigitLoopVar(loopVarName(n.Var)) // the loop's claim ends with the loop (Gap R.252)
		g.loopStack = g.loopStack[:len(g.loopStack)-1]
		b.WriteString(fmt.Sprintf("  br label %%%s\n", incL))
		b.WriteString(fmt.Sprintf("%s:\n", incL))
		g.ldN++
		ild := fmt.Sprintf("%%%s.ld%d", ctr, g.ldN)
		b.WriteString(fmt.Sprintf("  %s = load i32, i32* %%%s\n", ild, ctr))
		itmp := g.newTmp()
		b.WriteString(fmt.Sprintf("  %s = add i32 %s, %s\n", itmp, ild, step))
		b.WriteString(fmt.Sprintf("  store i32 %s, i32* %%%s\n", itmp, ctr))
		b.WriteString(fmt.Sprintf("  br label %%%s\n", condL))
		if len(n.Else) > 0 {
			b.WriteString(fmt.Sprintf("%s:\n", elseL))
			for _, s := range n.Else {
				if err := g.stmt(b, s); err != nil {
					return err
				}
			}
			b.WriteString(fmt.Sprintf("  br label %%%s\n", endL))
		}
		b.WriteString(fmt.Sprintf("%s:\n", endL))
	case *ClassDef:
		g.registerClass(n)
		return nil
	case *FuncDef:
		ci := g.closures[n.Name]
		if ci == nil {
			if err := g.funcDef(b, n); err != nil {
				return err
			}
			return nil
		}
		env := g.emitNewEnv(b)
		for i, c := range ci.captured {
			val := ""
			if pn, ok := g.params[c]; ok && !g.paramSlot[c] {
				val = pn
			} else {
				val = g.newTmp()
				fmt.Fprintf(b, "  %s = load i32, i32* %%_%s\n", val, c)
			}
			g.emitEnvStore(b, env, i, val)
		}
		fmt.Fprintf(b, "  store i32 %s, i32* @%s_slot\n", env, n.Name)
	case *ReturnStmt:
		if g.handledArms > 0 {
			// Returning out of an `except` arm ends the handler's work: the exception this
			// arm accepted must not survive the return, or the caller's next call-site check
			// hands it back as though nothing had handled it (Gap R.21, compiled half).
			g.clearExn(b)
		}
		if n.Expr == nil {
			// bare `return` yields None (ADR 0172). A float function reaching this is
			// a type error the checker reports; 0.0 keeps the module valid.
			if g.floatFuncs[g.curFunc] {
				if err := g.runDeferred(b); err != nil {
					return err
				}
				g.gcCloseFrame(b)
				b.WriteString("  ret double 0.000000\n")
				return nil
			}
			g.heapUsed = true
			t := g.newTmp()
			g.emitHeapHelperCall(b, t, "rt_none", "", Span{})
			if err := g.runDeferred(b); err != nil {
				return err
			}
			g.gcCloseFrame(b)
			b.WriteString(fmt.Sprintf("  ret i32 %s\n", t))
			return nil
		}
		// One word crosses a `return`, and a dict literal's entry does not always travel in one word: the
		// answer to `{"a": "x"}["a"]` is a text whose payload is an `@str_tab` index, and a caller with no tag
		// to read prints the table position — the reference's `x` turned into a number at exit 0. The pair road
		// at the call boundary (ADR 0273) is the row that owns carrying it (Gap R.146); until it does, this
		// door refuses by name rather than answers wrongly, and every position that does carry the pair —
		// print, str()/repr(), an f-string field, a binding — is answered (roadmap Gap R.215, ADR 0327).
		if dl, key, isLit := literalDictReadOf(n.Expr); isLit && !g.dictLiteralFoldAnswers(dl, key) && !g.dictLiteralEntryIsPlainInt(dl, key) {
			return literalDictOneWordErr(key)
		}
		if g.floatFuncs[g.curFunc] {
			// A pair-returning callee under a double return: the callee answered (payload, tag) beside its
			// own return, and the one place a tagged value becomes a double is @rt_lift_num — the same door a
			// float slot's arithmetic lifts through. `def g(y): return y * 2` under
			// `def f(x): x = x + 1.5; return g(x)` is this shape: the callee's parameter has to arrive as a
			// pair for the multiply to keep the half, and the answer the caller reads is that pair lifted to
			// the double this function's convention already promised (roadmap L11.1, Gap R.139's return half).
			if p, t, okPair, pairErr := g.pairCallPair(b, n.Expr); pairErr != nil {
				return pairErr
			} else if okPair {
				lift := g.newTmp()
				b.WriteString(fmt.Sprintf("  %s = call double @rt_lift_num(i32 %s, i32 %s)\n", lift, p, t))
				if err := g.runDeferred(b); err != nil {
					return err
				}
				g.gcCloseFrame(b)
				b.WriteString(fmt.Sprintf("  ret double %s\n", lift))
				return nil
			}
			fv := g.floatValue(b, n.Expr)
			if err := g.runDeferred(b); err != nil {
				return err
			}
			g.gcCloseFrame(b)
			b.WriteString(fmt.Sprintf("  ret double %s\n", fv))
			return nil
		}
		if g.strFuncs[g.curFunc] {
			// A function known to yield a string returns its @str_tab index: callers
			// read the result as an index (that is what makes print(f("x")) show text,
			// ADR 0174), so returning the raw @.strN global here put a global in an
			// i32 slot and llc rejected the module — a plain `def g(): return "hi"`
			// looked like a compiler bug (ADR 0166).
			if txt, ok := g.stringVal(n.Expr); ok {
				it := g.newTmp()
				b.WriteString(fmt.Sprintf("  %s = call i32 @rt_str_intern2(i8* %s, i8* %s)\n", it, g.strConst(txt), g.strConst(pyReprString(txt))))
				if err := g.runDeferred(b); err != nil {
					return err
				}
				g.gcCloseFrame(b)
				b.WriteString(fmt.Sprintf("  ret i32 %s\n", it))
				return nil
			}
		}
		// A container handed back by a `return` is an object, and an object has a handle rather than a
		// global. `return [1, 2]` used to lower the literal to its compile-time array and write
		// `ret i32 @.lst1`, which is not IR: `llc` rejected the module and `gustyc` exited **2** — the code
		// the exit-code contract reserves for a compiler bug — on `def build(): return [1, 2]` with
		// `print(build())`, a program the reference runs and prints `[1, 2]` (roadmap Gap R.67, ADR 0333).
		// The builders reached here are the ones a binding already uses, so the word that crosses the `ret`
		// is the same handle `ys = build()` has been handing back all along, and the roads that read a
		// container variable read the answer as soon as the print door asks it (below).
		if _, isContainerAnswer := g.containerAnswerOf(g.curFunc); isContainerAnswer {
			mustBuild := false
			switch n.Expr.(type) {
			case *ListLit, *SetLit, *DictLit, *Comp:
				mustBuild = true // the scan proved the answer; a builder that declines says so, it does not emit a global
			}
			if h, _, okHandle := g.containerHandleOf(b, n.Expr); okHandle {
				g.heapUsed = true
				if err := g.runDeferred(b); err != nil {
					return err
				}
				g.gcCloseFrame(b)
				b.WriteString(fmt.Sprintf("  ret i32 %s\n", h))
				return nil
			}
			if mustBuild {
				_, cErr := g.containerOperand(b, n.Expr)
				if cErr != nil {
					return cErr
				}
				return fmt.Errorf("codegen: %q answers a container this backend cannot build at the return (roadmap Gap R.67, ADR 0333)", g.curFunc)
			}
		}
		if g.pairRetDone[g.curFunc] {
			// The body's answer already is a (payload, tag) pair — the arithmetic door decided its kind from
			// the objects the caller read — so the tag travels beside the return and the payload keeps the
			// word this function always returned (roadmap L11.1, Gap R.139, ADR 0273).
			p, t, rerr := g.pairReturnWords(b, n.Expr)
			if rerr != nil {
				return rerr
			}
			g.storePairTag(b, t)
			// The value is computed before the deferred bodies run, as Python's `return` in a `try` does
			// (Gap R.23, ADR 0222) — and the tag is stored before them for the same reason: a `finally` that
			// calls this function again would otherwise leave its own answer's kind in the word.
			if err := g.runDeferred(b); err != nil {
				return err
			}
			g.gcCloseFrame(b)
			b.WriteString(fmt.Sprintf("  ret i32 %s\n", p))
			return nil
		}
		v, err := g.value(b, n.Expr)
		if err != nil {
			return err
		}
		// deferred bodies run -- which is why `return n` in a `try` hands back the `n` from
		// before the `finally` reassigned it, as it does in Python (Gap R.23, ADR 0222).
		if err := g.runDeferred(b); err != nil {
			return err
		}
		g.gcCloseFrame(b)
		b.WriteString(fmt.Sprintf("  ret i32 %s\n", v))
	case *BreakStmt:
		if len(g.loopStack) == 0 {
			return fmt.Errorf("codegen: break outside loop")
		}
		info := g.loopStack[len(g.loopStack)-1]
		if g.handledArms > 0 {
			g.clearExn(b) // leaving an arm we already accepted the exception in
		}
		if err := g.runDeferred(b); err != nil {
			return err
		}
		b.WriteString(fmt.Sprintf("  br label %%%s\n", info.breakLabel))
	case *ContinueStmt:
		if len(g.loopStack) == 0 {
			return fmt.Errorf("codegen: continue outside loop")
		}
		info := g.loopStack[len(g.loopStack)-1]
		if g.handledArms > 0 {
			g.clearExn(b)
		}
		if err := g.runDeferred(b); err != nil {
			return err
		}
		b.WriteString(fmt.Sprintf("  br label %%%s\n", info.continueLabel))
	case *PassStmt, *TypeAliasStmt:
		// no-op statement: type aliases are compile-time only (L5.7); emit nothing
	case *YieldStmt:
		// `yield expr` inside a generator function appends to the function's
		// runtime heap list (the interpreter evaluates generators eagerly).
		if g.genHandle == "" {
			return fmt.Errorf("codegen: yield outside a generator function")
		}
		v, err := g.value(b, n.Expr)
		if err != nil {
			return err
		}
		fmt.Fprintf(b, "  call void @rt_append(i32 %s, i32 %s)\n", g.genHandle, v)
	case *YieldFromStmt:
		if g.genHandle == "" {
			return fmt.Errorf("codegen: yield from outside a generator")
		}
		// A statically-known list literal has no heap backing: append each
		// element directly instead of treating the global as a heap handle.
		if ll, ok := n.Expr.(*ListLit); ok {
			for _, el := range ll.Elems {
				ev, err := g.value(b, el)
				if err != nil {
					return err
				}
				fmt.Fprintf(b, "  call void @rt_append(i32 %s, i32 %s)\n", g.genHandle, ev)
			}
			return nil
		}
		hv, err := g.value(b, n.Expr)
		if err != nil {
			return err
		}
		// Keep the sub-list handle alive across the GC emitted before the
		// append loop; otherwise the generator's freshly-returned list can be
		// collected and the loop reads a stale handle. Store the handle into a
		// rooted alloca slot so GC keeps the object reachable.
		hvslot := g.newTmp()
		hvslotName := strings.TrimPrefix(hvslot, "%")
		fmt.Fprintf(b, "  %%_%s = alloca i32\n", hvslotName)
		fmt.Fprintf(b, "  store i32 %s, i32* %%_%s\n", hv, hvslotName)
		g.gcReg(b, hvslotName)
		ln := g.newTmp()
		fmt.Fprintf(b, "%s = call i32 @rt_list_len(i32 %s)\n", ln, hv)
		ild := g.newLabel("yf.cond")
		ilp := g.newLabel("yf.body")
		inc := g.newLabel("yf.inc")
		end := g.newLabel("yf.end")
		iv := g.newTmp()
		fmt.Fprintf(b, "%s = alloca i32\n", iv)
		fmt.Fprintf(b, "  store i32 0, i32* %s\n", iv)
		fmt.Fprintf(b, "  br label %%%s\n", ild)
		fmt.Fprintf(b, "%s:\n", ild)
		ivc := g.newTmp()
		fmt.Fprintf(b, "%s = load i32, i32* %s\n", ivc, iv)
		cmp := g.newTmp()
		fmt.Fprintf(b, "%s = icmp slt i32 %s, %s\n", cmp, ivc, ln)
		fmt.Fprintf(b, "  br i1 %s, label %%%s, label %%%s\n", cmp, ilp, end)
		fmt.Fprintf(b, "%s:\n", ilp)
		// The generator's answer is already parked in a rooted slot above (it keeps the list alive across
		// the append loop), so the body can collect: each element is appended straight into that container,
		// which roots it transitively from the sweep's own walk of the payload. The loop itself is
		// the compiler's, not the program's: it fetches a word and appends it, minting no object, so it
		// asks for no collection (loopSafePoint's `nil` body).
		g.loopSafePoint(b, nil)
		el := g.newTmp()
		fmt.Fprintf(b, "%s = call i32 @rt_get_elem(i32 %s, i32 %s)\n", el, hv, ivc)
		fmt.Fprintf(b, "  call void @rt_append(i32 %s, i32 %s)\n", g.genHandle, el)
		fmt.Fprintf(b, "  br label %%%s\n", inc)
		fmt.Fprintf(b, "%s:\n", inc)
		nv := g.newTmp()
		fmt.Fprintf(b, "%s = add i32 %s, 1\n", nv, ivc)
		fmt.Fprintf(b, "  store i32 %s, i32* %s\n", nv, iv)
		fmt.Fprintf(b, "  br label %%%s\n", ild)
		fmt.Fprintf(b, "%s:\n", end)
	case *WithStmt:
		mh, err := g.value(b, n.Expr)
		if err != nil {
			return err
		}
		_ = mh // manager handle kept for __exit__ dispatch
		enterCall := &Call{Fn: &Attr{Obj: n.Expr, Name: &Name{Value: "__enter__"}}}
		eh, err := g.value(b, enterCall)
		if err != nil {
			return err
		}
		if n.As != nil {
			fmt.Fprintf(b, "%%_%s = alloca i32\n", n.As.Value)
			fmt.Fprintf(b, "  store i32 %s, i32* %%_%s\n", eh, n.As.Value)
			// Record the slot like every other binding does: the unbound-name guard
			// (nameIsBound) consults allocd, and `with M() as m: … m.n` was rejected as
			// "undefined name m" because this path allocated without registering.
			g.allocd[n.As.Value] = true
			// Record the `as` binding's class so instance field access (m.n) works.
			// `__enter__` returns self, so the binding is an instance of the manager's class.
			cls := ""
			if call, ok := n.Expr.(*Call); ok {
				if fn, ok2 := call.Fn.(*Name); ok2 {
					if _, isClass := g.classInfos[fn.Value]; isClass {
						cls = fn.Value
					}
				}
			} else if name, ok := n.Expr.(*Name); ok {
				cls = g.varClasses[name.Value]
			}
			if cls != "" {
				if g.varClasses == nil {
					g.varClasses = map[string]string{}
				}
				g.varClasses[n.As.Value] = cls
			}
		}
		for _, s := range n.Body {
			if err := g.stmt(b, s); err != nil {
				return err
			}
		}
		exitCall := &Call{Fn: &Attr{Obj: n.Expr, Name: &Name{Value: "__exit__"}},
			Args: []Expr{&IntLit{Value: 0}, &IntLit{Value: 0}, &IntLit{Value: 0}}}
		_, err = g.value(b, exitCall)
		if err != nil {
			return err
		}
	default:
		return fmt.Errorf("codegen: unsupported statement %T", st)
	}
	return nil
}
