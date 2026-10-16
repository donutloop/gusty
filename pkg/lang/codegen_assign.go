// codegen_assign.go is the binding road: what an `x = …`, `x += …` or an annotated binding writes, which slot it writes it into, and the record it leaves for the kind questions the other roads ask about that name.
package lang

import (
	"fmt"
	"strings"
)

// stmtAugAssign is the road an augmented binding takes — the same BinOp road a plain
// expression asks, then the same store.
func (g *irGen) stmtAugAssign(b *strings.Builder, st Stmt, n *AugAssignStmt) error {
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
}

// stmtAssign is the road a binding takes: the slot it writes, the value word it writes, and what
// the name is recorded as holding afterwards.
func (g *irGen) stmtAssign(b *strings.Builder, st Stmt, n *AssignStmt) error {
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
	return nil
}
