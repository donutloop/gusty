// Comprehensions and generators: the fold a static range allows, and the runtime loop a dynamic one needs.
package lang

import (
	"fmt"
	"strconv"
	"strings"
)

// foldConstInt evaluates e to a compile-time integer constant using the
// current constBindings, or returns (0, false) when not compile-time-known.
// It mirrors the record's constant arithmetic so comprehension bodies can
// be unrolled at codegen time without emitting IR.
func (g *irGen) foldConstInt(e Expr) (int64, bool) {
	switch n := e.(type) {
	case *IntLit:
		return n.Value, true
	case *BoolLit:
		if n.Value {
			return 1, true
		}
		return 0, true
	case *NoneLit:
		return 0, true
	case *Name:
		if v, ok := g.constBindings[n.Value]; ok {
			return v, true
		}
		return 0, false
	case *BinOp:
		lv, lok := g.foldConstInt(n.L)
		rv, rok := g.foldConstInt(n.R)
		if !lok || !rok {
			return 0, false
		}
		switch n.Op {
		case "+":
			return lv + rv, true
		case "-":
			return lv - rv, true
		case "*":
			return lv * rv, true
		case "/", "//":
			if rv == 0 {
				return 0, false
			}
			return lv / rv, true
		case "%":
			if rv == 0 {
				return 0, false
			}
			return lv % rv, true
		case "==":
			if lv == rv {
				return 1, true
			}
			return 0, true
		case "!=":
			if lv != rv {
				return 1, true
			}
			return 0, true
		case "<":
			if lv < rv {
				return 1, true
			}
			return 0, true
		case "<=":
			if lv <= rv {
				return 1, true
			}
			return 0, true
		case ">":
			if lv > rv {
				return 1, true
			}
			return 0, true
		case ">=":
			if lv >= rv {
				return 1, true
			}
			return 0, true
		case "and", "or":
			// The answer is an operand, not a verdict (roadmap Gap R.147, ADR 0269): a folder that returns
			// 1 here is the silent wrong answer one statement further out cannot undo.
			return logicFoldConst(lv, n.Op, rv), true
		}
		return 0, false
	case *UnOp:
		if n.Op == "-" && negationOperandIsLiterallyNotANumber(n.X) {
			// The fold may not answer a negation the reference stops on: the 0 it would print is the very
			// silence this road replaces with a raise, so the shape is left un-folded and the caller walks
			// the lowering that names the operand's kind (roadmap Gap R.137, ADR 0266).
			return 0, false
		}
		xv, xok := g.foldConstInt(n.X)
		if !xok {
			return 0, false
		}
		switch n.Op {
		case "-":
			return -xv, true
		case "not":
			if xv == 0 {
				return 1, true
			}
			return 0, true
		}
		return 0, false
	default:
		return 0, false
	}
}

// runtimeCompLoop is the comprehension over a container whose length is only known at runtime —
// a variable, or a builtin that builds one. Unlike the unrolled paths it is a real loop: an index
// counter, rt_get_elem per step, the loop variable bound to the element the way a `for` statement
// binds it (roadmap L11.7, ADR 0192).
func (g *irGen) runtimeCompLoop(b *strings.Builder, c *Comp) (string, error) {
	// The operands a loop has to lower: one element for a list or a set, a key and a value for a
	// dict. A shape the loop cannot spell is a refusal, never a container built without them.
	var elemExprs []Expr
	switch c.Kind {
	case CompDict:
		if len(c.Keys) != 1 || len(c.Vals) != 1 {
			return "", fmt.Errorf("codegen: a runtime comprehension builds one entry at a time (roadmap L11.1)")
		}
		elemExprs = []Expr{c.Keys[0], c.Vals[0]}
	default:
		if len(c.Elems) != 1 {
			return "", fmt.Errorf("codegen: a runtime comprehension builds one element at a time (roadmap L11.1)")
		}
		elemExprs = []Expr{c.Elems[0]}
	}
	for _, el := range elemExprs {
		if g.nestedContainerElem(el) && !g.taggableNestedElem(el) {
			return "", nestedContainerErr(el)
		}
	}
	// Iterating a container whose slots mix kinds binds its loop variable by tag — the pair ADR 0185
	// put in `%_x` and `%_x_tag` for `for`, and which the comprehension loop now binds the same way.
	// Reading the payload without the tag is how `out = [x for x in sa]` over {1, "a", None} printed
	// [1, 0, 0] on the compiled backend: three numbers, two of them zeros, where CPython prints
	// [1, 'a', None] (roadmap Gap R.76, ADR 0244).
	mixedIter := false
	if nm, ok := c.Iter.(*Name); ok {
		mixedIter = g.mixedLists[nm.Value] || g.mixedSets[nm.Value] || g.mixedDicts[nm.Value]
	}
	// Iterating a dict walks its **keys**, and a dict entry occupies two words, so the position is the
	// counter doubled and the length asked of the object is its entry count — the same pair of facts
	// `for v in d:` applies (ADR 0188). Reading slot 0 and slot 1 instead of slot 0 and slot 2 answers
	// `['a', 1]` for `{"a": 1, 2: "b"}`, which is a key and a value wearing the two keys' place.
	iterIsDict := false
	if nm, ok := c.Iter.(*Name); ok {
		iterIsDict = g.runtimeDicts[nm.Value] || g.mixedDicts[nm.Value]
	}
	// The iterable has to become a heap handle. A tracked container variable already has one;
	// a variable the compiler kept as a compile-time list does not — its value is a folded
	// global whose layout is a length plus an array, so it is materialised into the heap here.
	// That the same list can be either depends on what else the program does with it is exactly
	// the kind of compiler-state decision this project keeps having to chase, so both routes are
	// explicit rather than one of them being an accident.
	// A variable the compiler kept as a compile-time list has no slot to load — asking for one
	// emits `load i32, i32* %_xs` for a register that does not exist, which is the module llc
	// rejects — so the static route is tried first and only a real container variable goes
	// through the heap path.
	// A name the escape analysis kept as a compile-time list has no runtime object to walk and
	// no slot to load: emitting the load is the module llc rejects. It is a refusal today, and
	// the honest fix is the tagged value word making every container a runtime object.
	if nm, ok := c.Iter.(*Name); ok && !g.allocd[nm.Value] && !g.listVars[nm.Value] && !g.mixedLists[nm.Value] && !g.runtimeSets[nm.Value] && !g.runtimeDicts[nm.Value] {
		return "", fmt.Errorf("codegen: %s is a list the compiler kept as a compile-time constant, so a comprehension cannot walk it at runtime; mutating it (append) or iterating it with `for` materialises it, and the tagged value word will make every container a runtime object (roadmap L11.2, ADR 0192)", nm.Value)
	}
	// A call as the iterable is the one iterable this loop cannot take on trust: the handle is the callee's
	// `ret`, and if the scan never saw a container there, lowering the call through the one-word road hands
	// back a plain `0` that `rt_list_len` happily measures as empty. `def h(n): return range(n)` /
	// `[x for x in h(3)]` therefore printed `[]` at exit 0 where CPython prints `[0, 1, 2]` — a wrong answer,
	// the class this project ranks above every refusal. So the loop asks the SAME permission the container-handle
	// door asks (`callAnswersContainer`) and, failing that, the permission its own type already grants; what it
	// may not do is walk an object it never proved exists (roadmap Gap R.67, ADR 0333, Gap R.291).
	if _, isCallIter := c.Iter.(*Call); isCallIter {
		// A call as the iterable is the one iterable this loop cannot take on trust: the handle is the callee's
		// `ret`, and if nothing proved a container there, lowering the call through the one-word road hands back
		// a plain `0` that `rt_list_len` happily measures as empty. `def h(n): return range(n)` /
		// `[x for x in h(3)]` printed `[]` at exit 0 where CPython prints `[0, 1, 2]` — a wrong answer, the class
		// this project ranks above every refusal.
		//
		// The `for` road has been answering this shape for cycles, because it asks its iterable three questions
		// before lowering it. The comprehension asks them too, in the same order (roadmap Gap R.291, ADR 0357);
		// that the two roads keep their own copies of the sequence — rather than one normalisation, as the
		// subscript door finally got — is itself filed (Gap R.298).
		iter, okIter := g.normaliseCompIterable(b, c.Iter)
		if !okIter {
			return "", fmt.Errorf("codegen: %s does not name a container the compiled backend can prove, so the "+
				"comprehension walking it has nothing to measure; a call answers a container only where the scan can "+
				"see every arm of the callee answer one (roadmap Gap R.67, ADR 0333)", exprSnippet(c.Iter))
		}
		c.Iter = iter
	}
	var src string
	var err error
	lowered, lerr := g.value(b, c.Iter)
	if lerr == nil {
		if lit, ok := g.staticLists[lowered]; ok {
			src, err = g.heapListFrom(b, lit, "")
			if err != nil {
				return "", err
			}
		} else {
			src = lowered
		}
	} else {
		src, err = g.containerOperand(b, c.Iter)
		if err != nil {
			return "", err
		}
	}
	lv := loopVarName(c.ForVar)
	if !g.allocd[lv] {
		b.WriteString(fmt.Sprintf("  %%_%s = alloca i32\n", lv))
		g.gcReg(b, lv)
		g.allocd[lv] = true
	}
	// The target is a binding of THIS frame, and now says so. The slot above is what the reads load,
	// and the table beside it is what every road that asks a frame what it owns listens to — the
	// capture road among them, which until now heard nothing and refused the read of a name the loop
	// had just written (roadmap L11.7 · Gap R.348; `frame_targets.go` is the rule).
	g.noteFrameTarget(lv)
	defer g.forgetFrameTarget(lv)
	g.heapUsed = true // the comprehension is often the only heap allocation in the program
	// The loop variable of a string container holds an index into @str_tab, and saying so is
	// what makes `n == "a"` in the filter compare texts instead of comparing an index with a
	// string global — which is the IR llc rejects (the `for` statement records the same fact).
	iterIsStrElems := false
	if nm, ok := c.Iter.(*Name); ok {
		// A dict iteration yields keys, so it is the key kind that decides what the loop variable
		// holds — the same fact `for v in d:` uses to print a text key as text instead of as its
		// index in @str_tab (Gap I.2, ADR 0188).
		iterIsStrElems = g.listElemStr[nm.Value] || g.setElemStr[nm.Value] || g.dictKeyStr[nm.Value]
	}
	if ty := exprTyName(c.Iter); !iterIsStrElems && containerKindFromTy(ty) != "" && strings.Contains(ty, "str") {
		// The compiler's own tracking is not the only evidence: the analyzer inferred
		// list[str], and a folded (never-materialised) list never populates the kind maps.
		iterIsStrElems = true
	}
	if iterIsStrElems {
		g.internedVars[lv] = true
	}

	g.heapSeq++
	h := fmt.Sprintf("%%h%d", g.heapSeq)
	// The object the loop fills is the container the comprehension means: a list appends, a set
	// adds (deduping on the (payload, tag) pair) and a dict puts (replacing the entry whose key
	// pair matches) — all three through the tagged helpers, which find their own slot, so a filter
	// that skips an item cannot leave a hole behind (roadmap Gap J.2, ADR 0234).
	allocKind := HeapList
	if c.Kind == CompDict {
		allocKind = HeapKindDict
	} else if c.Kind == CompSet {
		allocKind = HeapKindSet
	}
	g.emitRtAllocKind(b, h, allocKind, c.Span())
	// The object lives in a register while the element road below is free to CALL, and a call's body
	// collects: the half-filled object is ADR 0181's forbidden shape unless it is rooted for the build
	// (roadmap **Gap R.500**, **ADR 0443**, **ADR 0347**).
	buildRoot := g.gcRootDuringBuild(b, h)
	// Every slot the loop writes goes through a tagged helper, so the object must let each slot say
	// what it holds. The compiler cannot always name the kind before the iteration reaches it — the
	// iterable may hold more than one kind, and an operand may be a call — and assuming one kind is
	// how `{x: x.upper() for x in xs}` printed the interned index as a number (roadmap Gap R.353,
	// ADR 0232, ADR 0244).
	b.WriteString(fmt.Sprintf("  call void @rt_mark_estr(i32 %s, i32 8)\n", h))
	nlen := g.newTmp()
	dictLenFn := "rt_list_len"
	if iterIsDict {
		dictLenFn = "rt_dict_len"
	}
	b.WriteString(fmt.Sprintf("  %s = call i32 @%s(i32 %s)\n", nlen, dictLenFn, src))
	// A named preheader block: the induction phi needs a predecessor to take its 0 from, and
	// the block the loop is written in has no name to reference.
	pre := g.newLabel("comp.pre")
	cond := g.newLabel("comp.cond")
	body := g.newLabel("comp.body")
	done := g.newLabel("comp.done")
	b.WriteString(fmt.Sprintf("  br label %%%s\n%s:\n  br label %%%s\n%s:\n", pre, pre, cond, cond))
	// The induction variable's phi forward-references the value the body defines; the parser
	// takes that (the runtime helpers do the same), and naming it off the temp counter keeps
	// two comprehensions in one function from colliding. A %s<N> name would read as a string
	// value to the print and call lowerings, which is a bug this backend has already had.
	g.tmp++
	nextReg := fmt.Sprintf("%%cc%d", g.tmp)
	idx := g.newTmp()
	// A filter puts the increment in the block the skips fall through to, so that block -- not
	// the body -- is the loop header's real predecessor. Naming the body anyway built a `phi`
	// whose entry list did not match its predecessors, which is why a runtime-list comprehension
	// with a string filter was refused rather than compiled: the refusal was covering an invalid
	// module, and the comparison bug underneath it (roadmap L11.8) has its own fix now
	// (Gap R.42, ADR 0224).
	backEdge := body
	var keep, skip, merge string
	if c.Cond != nil {
		keep = g.newLabel("comp.keep")
		skip = g.newLabel("comp.skip")
		backEdge = skip
	} else {
		// The same merge the filter already needs, for the body that has no filter. An element is
		// free to branch — `v / 2` emits its own zero guard (ADR 0253), and a slot whose kind the
		// object reports branches on the tag (ADR 0251) — so the block the element instructions end
		// in is not knowable here, and naming the body in the `phi` above built an entry list that
		// did not match the loop's predecessors: `llc` rejected `[v / 2 for v in xs]` after
		// `xs.append(6)` with *PHI node entries do not match predecessors*, exit 2 on a program the
		// oracle prints `[3.0]` for (roadmap Gap R.100, ADR 0138's lesson one door over).
		merge = g.newLabel("comp.merge")
		backEdge = merge
	}
	b.WriteString(fmt.Sprintf("  %s = phi i32 [ 0, %%%s ], [ %s, %%%s ]\n", idx, pre, nextReg, backEdge))
	cmp := g.newTmp()
	b.WriteString(fmt.Sprintf("  %s = icmp slt i32 %s, %s\n  br i1 %s, label %%%s, label %%%s\n%s:\n", cmp, idx, nlen, cmp, body, done, body))
	iv := g.newTmp()
	pos := idx
	if iterIsDict {
		scaled := g.newTmp()
		b.WriteString(fmt.Sprintf("  %s = mul i32 %s, 2\n", scaled, idx))
		pos = scaled
	}
	b.WriteString(fmt.Sprintf("  %s = call i32 @rt_get_elem(i32 %s, i32 %s)\n", iv, src, pos))
	b.WriteString(fmt.Sprintf("  store i32 %s, i32* %%_%s\n", iv, lv))
	if mixedIter {
		// The companion tag slot, in the loop body and not in the preheader, because it is the element
		// of *this* iteration that is being named. `for` over the same container binds it identically
		// (ADR 0185), and the two allocas are read as one pair by elemPayloadAndTag.
		if !g.allocd[lv+"_tag"] {
			b.WriteString(fmt.Sprintf("  %%_%s_tag = alloca i32\n", lv))
			g.allocd[lv+"_tag"] = true
		}
		lvt := g.newTmp()
		b.WriteString(fmt.Sprintf("  %s = call i32 @rt_tag_of(i32 %s, i32 %s)\n", lvt, src, pos))
		b.WriteString(fmt.Sprintf("  store i32 %s, i32* %%_%s_tag\n", lvt, lv))
		g.taggedVars[lv] = true
	}
	appendElem := func() error {
		if c.Kind == CompDict {
			// A dict entry is two (payload, tag) pairs, and a pair is only ever read or written as a
			// pair. When the key or value is the loop variable of a container whose slots mix kinds,
			// its kind lives in the object and nowhere else, so the pair comes from the two allocas —
			// the same door the list element goes through (ADR 0185, ADR 0244). Anything else keeps the
			// static answer it always had, including the message for a kind the compiler cannot name.
			dictPair := func(e Expr) (payload, tag string, interned, dynamic bool, err error) {
				if nm, isName := e.(*Name); isName {
					if p, t, ok := g.taggedLoopVarRead(b, nm.Value, nm.Span()); ok {
						return p, t, false, true, nil
					}
					// The loop variable of a text container holds an index into @str_tab, and the tag has
					// to say so: writing it as the integer 0 makes the entry an int the lookup cannot
					// find, so `{k: 1 for k in d}` printed {0: 1} and `out["a"]` died with KeyError
					// while the interpreter and CPython both answered {'a': 1} (roadmap Gap R.78, ADR 0245).
					if c.ForVar != nil && nm.Value == c.ForVar.Value && g.internedVars[nm.Value] {
						p, perr := g.value(b, e)
						if perr != nil {
							return "", "", false, false, perr
						}
						return p, strconv.FormatInt(int64(TagStr), 10), true, false, nil
					}
				}
				// The general entry is the same door the list element uses: payload and tag are chosen
				// together. Lowering only the payload truncates a float-valued expression to the integer the
				// value word happens to hold, which is how `{v: v / 2 for v in xs}` printed the number instead
				// of the box the printer was about to render (roadmap Gap R.105, ADR 0233, ADR 0244).
				p, t, intern, perr := g.elemPayloadAndTag(b, e)
				if perr != nil {
					return "", "", false, false, perr
				}
				return p, t, intern, false, nil
			}
			kv, kt, kIntern, kDyn, kerr := dictPair(c.Keys[0])
			if kerr != nil {
				return kerr
			}
			vv, vt, vIntern, vDyn, verr := dictPair(c.Vals[0])
			if verr != nil {
				return verr
			}
			b.WriteString(fmt.Sprintf("  call void @rt_dict_put_tagged(i32 %s, i32 %s, i32 %s, i32 %s, i32 %s)\n", h, kv, vv, kt, vt))
			if kIntern {
				b.WriteString(fmt.Sprintf("  call void @rt_mark_estr(i32 %s, i32 2)\n", h))
			}
			if vIntern {
				b.WriteString(fmt.Sprintf("  call void @rt_mark_estr(i32 %s, i32 4)\n", h))
			}
			if kDyn || vDyn {
				// A slot written from a tag the compiler only carries, never reads, obliges the object
				// to let each slot speak for itself when it is printed (ADR 0232).
				b.WriteString(fmt.Sprintf("  call void @rt_mark_estr(i32 %s, i32 8)\n", h))
			}
			return nil
		}
		av, kt, intern, aerr := g.elemPayloadAndTag(b, c.Elems[0])
		if aerr != nil {
			return aerr
		}
		addTagged := "rt_append_tagged"
		if c.Kind == CompSet {
			addTagged = "rt_set_add_tagged"
		}
		// Payload and tag are one call, through the door `xs.append(v)` uses: no untagged add is left to
		// fall back to, so a slot can never carry the tag of whoever held it last (ADR 0187, ADR 0244).
		b.WriteString(fmt.Sprintf("  call void @%s(i32 %s, i32 %s, i32 %s)\n", addTagged, h, av, kt))
		if intern {
			b.WriteString(fmt.Sprintf("  call void @rt_mark_estr(i32 %s, i32 1)\n", h))
		}
		// As above: a slot that needs its tag obliges the object to let each slot speak (ADR 0232). A
		// tagged loop variable always needs it — its kind is in the object and nowhere the compiler can
		// read it at this point in the program.
		if t, okTag := g.elemKindTag(c.Elems[0]); (okTag && slotTagSelfDescribing(t)) || g.compElemIsTaggedLoopVar(c) {
			b.WriteString(fmt.Sprintf("  call void @rt_mark_estr(i32 %s, i32 8)\n", h))
		}
		return nil
	}
	if c.Cond != nil {
		tv := g.truthOperand(b, c.Cond)
		b.WriteString(fmt.Sprintf("  br i1 %s, label %%%s, label %%%s\n%s:\n", tv, keep, skip, keep))
		if err := appendElem(); err != nil {
			return "", err
		}
		b.WriteString(fmt.Sprintf("  br label %%%s\n%s:\n", skip, skip))
	} else {
		if err := appendElem(); err != nil {
			return "", err
		}
		// Wherever the element's own branches left the block, the increment runs in a block with
		// exactly one predecessor — the one the `phi` above names (Gap R.100).
		b.WriteString(fmt.Sprintf("  br label %%%s\n%s:\n", merge, merge))
	}
	b.WriteString(fmt.Sprintf("  %s = add i32 %s, 1\n  br label %%%s\n%s:\n", nextReg, idx, cond, done))
	g.gcEndBuildRoot(b, buildRoot)
	if g.rtComps == nil {
		g.rtComps = map[*Comp]bool{}
	}
	g.rtComps[c] = true
	return h, nil
}

// comprehensionFolds reports whether the constant comprehension path can evaluate the element
// expression for every item. It runs the same fold the constant path will run, so the two can
// never disagree about what is foldable — a probe that lies would build a list at compile time
// and a list at runtime and call whichever failed an error.
func (g *irGen) comprehensionFolds(c *Comp, items []int64, itemExprs []Expr) bool {
	if g.constBindings == nil {
		g.constBindings = map[string]int64{}
	}
	if len(c.Elems) != 1 {
		return false
	}
	// `None` folds to the integer 0 — which is the right answer to `if None:` and the wrong answer to
	// \"what is in this slot\". An element that is not an integer belongs to the runtime builder, which
	// tags every slot it writes (roadmap Gap R.75, ADR 0244).
	if !g.foldsToAnInteger(c.Elems[0]) {
		return false
	}
	// The same question one level indirect: an element that is only a copy of the loop variable is a
	// verdict when the item it copies is, and a folded comprehension is a compile-time global with no
	// tag table to say so with (roadmap Gap R.112, ADR 0259).
	if g.anyItemIsAVerdict(c.Elems[0], c.ForVar, itemExprs) {
		return false
	}
	for _, item := range items {
		g.constBindings[c.ForVar.Value] = item
		_, ok := g.foldConstInt(c.Elems[0])
		if ok && c.Cond != nil {
			// The filter has to fold too: a condition that calls a function is exactly as
			// un-foldable as an element that does, and the constant path would refuse with
			// "comprehension condition must be constant".
			_, ok = g.foldConstInt(c.Cond)
		}
		delete(g.constBindings, c.ForVar.Value)
		if !ok {
			return false
		}
	}
	return true
}

// foldsToAnInteger is the comprehension fold's own question, asked before the shared folder answers
// the truthiness one: an element that is literally a None, a piece of text or a float is a *value* of
// that kind, not the integer its truthiness happens to fold to. Those elements go to the runtime
// builder, where the slot gets the tag that says what it holds (roadmap Gap R.75, ADR 0244).
//
// A verdict is the same trap (roadmap Gap R.112, ADR 0259): `[x for x in [True, 1, 1]]` used to fold
// to [1, 1, 1], and a folded comprehension is a compile-time global with no tag table to write, so
// the printing question had no answer to be given. It belongs to the runtime builder beside the rest.
// A container is the same trap wearing a different hat: `foldConstInt` can see the global that
// represents `{1}` or `[1]`, but the answer is a handle or a table index, not an `i32` the compile-time
// element array can carry without saying what it is. Such operands go to the builder, which writes
// payload and tag together (roadmap Gap R.75, ADR 0187, ADR 0244).
func (g *irGen) foldsToAnInteger(e Expr) bool {
	switch e.(type) {
	case *IntLit:
		return true
	case *BoolLit, *NoneLit, *StrLit, *FloatLit, *ListLit, *SetLit, *DictLit, *Tuple:
		return false
	}
	if g == nil {
		return true
	}
	if g.printsAsBool(e) || g.isNoneExpr(e) || g.isFloat(e) {
		return false
	}
	return true
}

// bindUnrolledCompItem binds the loop variable for one item of a compile-time-enumerated comprehension,
// recording exactly the per-item facts an unrolled `for` records: the payload, whether the loop variable
// currently means an interned text, whether it may be read as a digit, whether it carries a verdict, and
// the tag of the value this iteration copied. The state is restored afterwards so one iteration's text
// does not print the next item as `(null)` (roadmap Gap R.358, beside ADR 0233's loop-element tag and
// ADR 0259's verdict tag).
func (g *irGen) bindUnrolledCompItem(b *strings.Builder, lv string, item Expr) (func(), error) {
	prevInterned, hadInterned := g.internedVars[lv]
	prevTagged, hadTagged := g.taggedVars[lv]
	prevLoopTag, hadLoopTag := g.loopElemTag[lv]
	prevLoopElem, hadLoopElem := g.loopElem[lv]
	v, isStr, err := g.heapElemKind(b, item)
	if err != nil {
		return nil, err
	}
	b.WriteString(fmt.Sprintf("  store i32 %s, i32* %%_%s\n", v, lv))
	g.storeModuleScalar(b, lv, v)
	if isStr {
		if g.internedVars == nil {
			g.internedVars = map[string]bool{}
		}
		g.internedVars[lv] = true
		g.releaseDigitLoopVar(lv)
	} else {
		delete(g.internedVars, lv)
		if _, isInt := item.(*IntLit); isInt {
			g.claimDigitLoopVar(lv)
		} else {
			g.releaseDigitLoopVar(lv)
		}
	}
	// A loop variable is rebound per item: the name may once have held a verdict, but this iteration
	// binds it to the item in front of it, and ADR 0172's latest-binding rule decides what it prints.
	g.forgetVarBool(lv)
	if g.loopElemTag == nil {
		g.loopElemTag = map[string]int32{}
	}
	itemTag := g.elemTagFor(item, isStr)
	g.loopElemTag[lv] = itemTag
	// Which item this copy walks, beside what it is: the sentence a body raises names the member the
	// comprehension put in front of it (roadmap Gap R.440, Gap R.189).
	if g.loopElem == nil {
		g.loopElem = map[string]Expr{}
	}
	g.loopElem[lv] = item
	// The unrolled road records this item's tag for the print, filter, and container-door questions that
	// read the loop variable. It does not turn every item into the dynamic tagged-variable pair: those
	// positions keep their own doors, and claiming the pair here made ordinary integer comprehension
	// elements refuse (roadmap Gap R.358, beside ADR 0233's unrolled `for` tag).
	delete(g.taggedVars, lv)
	return func() {
		if hadInterned {
			g.internedVars[lv] = prevInterned
		} else {
			delete(g.internedVars, lv)
		}
		if hadTagged {
			g.taggedVars[lv] = prevTagged
		} else {
			delete(g.taggedVars, lv)
		}
		if hadLoopTag {
			g.loopElemTag[lv] = prevLoopTag
		} else {
			delete(g.loopElemTag, lv)
		}
		if hadLoopElem {
			g.loopElem[lv] = prevLoopElem
		} else {
			delete(g.loopElem, lv)
		}
	}, nil
}

// compElemCopiesTheLoopVariable asks whether the operand is literally the iteration's name. If it is,
// the operand is the kind of the item this iteration bound, not a kind the compiler inferred once from
// some earlier use of the name (roadmap Gap R.358, ADR 0259's rule generalised from verdicts to every
// item kind).
func compElemCopiesTheLoopVariable(e Expr, lv string) bool {
	nm, ok := e.(*Name)
	return ok && lv != "" && nm.Value == lv
}

// runtimeCompList builds a list comprehension whose element is not a constant: the element
// expression is evaluated once per item and appended to a heap list. The loop is unrolled, as
// everything else in this backend is — `for` over a literal gets one block per element too — so
// the iterable still has to be a shape the compiler can enumerate (a literal or range with
// constant bounds) even though the elements no longer have to be known.
//
// Binding the loop variable matters: it is a real slot the element expression reads, which is why
// [sq(x) for x in range(5)] can call sq at all (roadmap L11.7, ADR 0192).
func (g *irGen) runtimeCompList(b *strings.Builder, c *Comp, itemExprs []Expr) (string, error) {
	if len(c.Elems) != 1 {
		return "", fmt.Errorf("codegen: a runtime comprehension builds one element at a time (roadmap L11.1)")
	}
	if g.nestedContainerElem(c.Elems[0]) && !g.taggableNestedElem(c.Elems[0]) {
		// The door's own sentence, once. This road used to wrap it in a second `codegen: `, so the reader
		// saw the prefix twice — `gustyc: jit: codegen: codegen: a compiled container cannot hold …` — which
		// is the same defect as promising a way to run that does not exist: a sentence about the toolchain
		// that is not true (**ADR 0437**, **ADR 0249**).
		return "", nestedContainerErr(c.Elems[0])
	}
	lv := loopVarName(c.ForVar)
	if !g.allocd[lv] {
		b.WriteString(fmt.Sprintf("  %%_%s = alloca i32\n", lv))
		g.gcReg(b, lv)
		g.allocd[lv] = true
	}
	if !g.allocd[lv+"_tag"] {
		b.WriteString(fmt.Sprintf("  %%_%s_tag = alloca i32\n", lv))
		g.allocd[lv+"_tag"] = true
	}
	// The unrolled road binds the target per item and no further than the item: the name is the
	// comprehension's own, recorded for the element and the filter and released when they are lowered,
	// so the module never learns a spelling its source never wrote (roadmap L11.7 · Gap R.348).
	g.noteFrameTarget(lv)
	defer g.forgetFrameTarget(lv)
	g.heapUsed = true // the comprehension is often the only heap allocation in the program
	g.heapSeq++
	h := fmt.Sprintf("%%h%d", g.heapSeq)
	g.emitRtAllocKind(b, h, HeapList, c.Span())
	// Rooted for the build: the handle is a register and an element may call (roadmap **Gap R.500**,
	// **ADR 0443**; **ADR 0181**, **ADR 0347**).
	buildRoot := g.gcRootDuringBuild(b, h)
	// Whether the elements are interned text is asked of the element itself, once per write, by the
	// door below — a pre-scan here emitted an intern call the loop never used.
	// appendElem writes the payload and the tag together, through the same door `xs.append(v)` uses
	// (ADR 0187, ADR 0244). An append that wrote only the payload leaves the new slot tagged as
	// whatever held it before, and an element compiled with `g.value` alone reaches a container slot as
	// the compiler's *global* (`@.lst1`) — a value position `llc` rejects, which is how a comprehension
	// over container literals ended the module instead of building the list (roadmap Gap R.75).
	appendElem := func(item Expr) error {
		// When the element is only a copy of the loop variable, the printing question belongs to the item
		// it copies: `[x for x in [True, 1, 1]]` asks `x`, which says nothing by itself, and the list came
		// out as three integers (roadmap Gap R.112, ADR 0259).
		tagQuery := c.Elems[0]
		if compElemCopiesTheLoopVariable(c.Elems[0], lv) || g.compElemCopiesABool(c.Elems[0], lv, item) {
			tagQuery = item
		}
		av, kt, intern, err := g.elemPayloadAndTag(b, c.Elems[0])
		if err != nil {
			return err
		}
		forcedText := false
		if tagQuery != c.Elems[0] {
			if t, okTag := g.elemKindTag(tagQuery); okTag {
				kt = strconv.FormatInt(int64(t), 10)
				intern = false
				forcedText = t == int32(TagStr)
			}
		}
		b.WriteString(fmt.Sprintf("  call void @rt_append_tagged(i32 %s, i32 %s, i32 %s)\n", h, av, kt))
		if intern || forcedText {
			b.WriteString(fmt.Sprintf("  call void @rt_mark_estr(i32 %s, i32 1)\n", h))
		}
		// A slot whose payload only means something through its tag — a float box, None, another
		// container — obliges the whole object to stop claiming one element kind, or the printer renders
		// every slot through the number printer and `[[1, 2] for x in [1]]` prints the box handles
		// ([2, 3]) instead of the lists (ADR 0232, roadmap Gap R.75).
		if t, okTag := g.elemKindTag(tagQuery); okTag && slotTagSelfDescribing(t) {
			b.WriteString(fmt.Sprintf("  call void @rt_mark_estr(i32 %s, i32 8)\n", h))
		}
		return nil
	}
	for _, item := range itemExprs {
		restore, err := g.bindUnrolledCompItem(b, lv, item)
		if err != nil {
			return "", err
		}
		if c.Cond == nil {
			if err := appendElem(item); err != nil {
				restore()
				return "", err
			}
			restore()
			continue
		}
		// The filter runs per item, in source order, with the loop variable bound — so
		// [x for x in xs if f(x)] filters with the same f the interpreter calls.
		condL := g.newLabel("comp.cond")
		itemL := g.newLabel("comp.item")
		skipL := g.newLabel("comp.skip")
		b.WriteString(fmt.Sprintf("  br label %%%s\n%s:\n", condL, condL))
		tv := g.truthOperand(b, c.Cond)
		b.WriteString(fmt.Sprintf("  br i1 %s, label %%%s, label %%%s\n%s:\n", tv, itemL, skipL, itemL))
		if err := appendElem(item); err != nil {
			restore()
			return "", err
		}
		restore()
		b.WriteString(fmt.Sprintf("  br label %%%s\n%s:\n", skipL, skipL))
	}
	g.gcEndBuildRoot(b, buildRoot)
	if g.rtComps == nil {
		g.rtComps = map[*Comp]bool{}
	}
	g.rtComps[c] = true
	return h, nil
}

// compItems derives the items a comprehension walks at compile time: an inline list literal of
// constant integers, or range(start, stop[, step]) with constant bounds. Anything else — a
// container variable, a builtin that builds a list — has no compile-time length, and the caller
// compItems derives the items a comprehension walks at compile time: an inline list literal of
// constant integers, or range(start, stop[, step]) with constant bounds. Anything else — a
// container variable, a builtin that builds a list — has no compile-time length, and the caller
// answers it with the runtime loop (roadmap L11.7, ADR 0192).
func (g *irGen) compItems(c *Comp) ([]int64, []Expr, error) {
	return g.compItemsOf(c.Iter)
}

// compItemsOf is `compItems` asked of ONE iterable rather than of a whole comprehension — the question
// a clause list asks clause by clause, and the reason it is a separate function is that a later clause
// walks a name an earlier clause bound, which the caller hands back here already resolved to the
// literal that name was bound to (roadmap Gap R.352).
func (g *irGen) compItemsOf(iter Expr) ([]int64, []Expr, error) {
	var items []int64
	var itemExprs []Expr
	if ll, ok := iter.(*ListLit); ok {
		for _, el := range ll.Elems {
			v, ok := g.foldConstInt(el)
			if !ok {
				return nil, nil, fmt.Errorf("codegen: comprehension iterable must be constant integers")
			}
			items = append(items, v)
			itemExprs = append(itemExprs, el)
		}
		return items, itemExprs, nil
	}
	if r, ok := iter.(*Call); ok {
		// The caller only asks for items from a range(); anything else has no compile-time length to
		// enumerate, and reaching here with a call that is not one is a routing bug, not an input to
		// guess at. The zero-argument shape is the one that used to fall through to the `Args[0]`
		// below and panic (roadmap Gap R.291, ADR 0356).
		if !compIterIsStaticRange(r) {
			return nil, nil, fmt.Errorf("codegen: comprehension iterable has no compile-time length")
		}
		// range(stop), range(start, stop) or range(start, stop, step)
		start := int64(0)
		stop, ok := g.foldConstInt(r.Args[0])
		if !ok {
			return nil, nil, fmt.Errorf("codegen: range bound must be a constant")
		}
		step := int64(1)
		if len(r.Args) > 1 {
			start = stop
			stop, ok = g.foldConstInt(r.Args[1])
			if !ok {
				return nil, nil, fmt.Errorf("codegen: range stop must be a constant")
			}
		}
		if len(r.Args) > 2 {
			step, ok = g.foldConstInt(r.Args[2])
			if !ok {
				return nil, nil, fmt.Errorf("codegen: range step must be a constant")
			}
		}
		if step > 0 {
			for v := start; v < stop; v += step {
				items = append(items, v)
				itemExprs = append(itemExprs, &IntLit{Value: v})
			}
		} else {
			for v := start; v > stop; v += step {
				items = append(items, v)
				itemExprs = append(itemExprs, &IntLit{Value: v})
			}
		}
		return items, itemExprs, nil
	}
	return nil, nil, fmt.Errorf("codegen: a comprehension over %s is refused: this backend builds a comprehension over an inline list literal, range(), or a container variable it can see the kind of, and the reference iterates a text one character at a time — answering the same needs the value word that carries its own kind (roadmap L11.1; Gap I.2 covers text iteration through the same door)", exprSurface(iter))
}

// foldConstUnder folds one operand of a comprehension with the loop variable bound to one item,
// clearing the binding again on the way out. Every fold a comprehension does goes through here, so
// a refused fold cannot leave the loop variable's value behind for the next statement to read, and
// the refusal names the operand that gave up (`element`, `condition`, `key`, `value`) instead of
// blaming the element for a filter that will not fold.
func (g *irGen) foldConstUnder(noun, forVar string, item int64, e Expr) (int64, error) {
	if g.constBindings == nil {
		g.constBindings = map[string]int64{}
	}
	g.constBindings[forVar] = item
	v, ok := g.foldConstInt(e)
	delete(g.constBindings, forVar)
	if !ok {
		return 0, fmt.Errorf("codegen: comprehension %s must be constant", noun)
	}
	return v, nil
}

// foldSetComp unrolls a set comprehension into the literal it means: the element and, when there
// is one, the filter are folded with the loop variable bound to each item, and a member already
// seen is dropped. The literal is what the binding rule stores into the heap, so a folded set
// comprehension and the equivalent `{1, 2}` literal build the very same object (roadmap Gap J.2,
// ADR 0234).
func (g *irGen) foldSetComp(c *Comp, items []int64, itemExprs []Expr) (*SetLit, []int64, error) {
	// A set comprehension folds into a compile-time global, which has no tag table; an element that
	// has to say True has nowhere to say it (roadmap Gap R.112, ADR 0259, roadmap Gap R.116).
	if g.boolSlotExpr(c.Elems[0]) || g.anyItemIsAVerdict(c.Elems[0], c.ForVar, itemExprs) {
		return nil, nil, fmt.Errorf("codegen: a comprehension of verdicts needs the tagged set builder, which a set comprehension does not have yet (roadmap Gap R.116)")
	}
	seen := map[int64]bool{}
	var vals []int64
	var elems []Expr
	for _, item := range items {
		v, err := g.foldConstUnder("element", c.ForVar.Value, item, c.Elems[0])
		if err != nil {
			return nil, nil, err
		}
		if c.Cond != nil {
			cv, cerr := g.foldConstUnder("condition", c.ForVar.Value, item, c.Cond)
			if cerr != nil {
				return nil, nil, cerr
			}
			if cv == 0 {
				continue
			}
		}
		if seen[v] {
			continue
		}
		seen[v] = true
		vals = append(vals, v)
		elems = append(elems, &IntLit{Value: v})
	}
	return &SetLit{Elems: elems, Src: c.Src}, vals, nil
}

// foldDictComp is the same unrolling for a dict comprehension, keeping the folded keys beside the
// literal so `d[k]` on a folded comprehension can still be answered at codegen time.
func (g *irGen) foldDictComp(c *Comp, items []int64, itemExprs []Expr) (*DictLit, []int64, []int64, error) {
	// The same rule as the set: a folded dict is a compile-time global, and a slot that has to say
	// True or False has no tag to say it with (roadmap Gap R.112, ADR 0259, roadmap Gap R.116).
	if g.boolSlotExpr(c.Keys[0]) || g.boolSlotExpr(c.Vals[0]) ||
		g.anyItemIsAVerdict(c.Keys[0], c.ForVar, itemExprs) || g.anyItemIsAVerdict(c.Vals[0], c.ForVar, itemExprs) {
		return nil, nil, nil, fmt.Errorf("codegen: a comprehension of verdicts needs the tagged dict builder, which a dict comprehension does not have yet (roadmap Gap R.116)")
	}
	var keys, vals []int64
	var kexprs, vexprs []Expr
	for _, item := range items {
		k, kerr := g.foldConstUnder("key", c.ForVar.Value, item, c.Keys[0])
		if kerr != nil {
			return nil, nil, nil, kerr
		}
		v, verr := g.foldConstUnder("value", c.ForVar.Value, item, c.Vals[0])
		if verr != nil {
			return nil, nil, nil, verr
		}
		if c.Cond != nil {
			cv, cerr := g.foldConstUnder("condition", c.ForVar.Value, item, c.Cond)
			if cerr != nil {
				return nil, nil, nil, cerr
			}
			if cv == 0 {
				continue
			}
		}
		keys = append(keys, k)
		vals = append(vals, v)
		kexprs = append(kexprs, &IntLit{Value: k})
		vexprs = append(vexprs, &IntLit{Value: v})
	}
	return &DictLit{Keys: kexprs, Vals: vexprs, Src: c.Src}, keys, vals, nil
}

// foldedContainerCompLiteral is the question the binding rule asks: is this right-hand side a
// set/dict comprehension that means a literal outright? When it is, the binding goes through the
// literal's own lowering, so a comprehension and the literal it folds to cannot grow apart — and
// when it is not (an element the folder cannot see, a container variable to walk), the answer is
// simply `false` and the runtime loop answers the program instead (roadmap Gap J.2, ADR 0234).
func (g *irGen) foldedContainerCompLiteral(e Expr) (Expr, bool) {
	c, ok := e.(*Comp)
	if !ok || (c.Kind != CompSet && c.Kind != CompDict) {
		return nil, false
	}
	// The fold below reads ONE clause. A clause-list comprehension is folded by the nested road, which
	// binds each clause's own target; folding one clause and calling it the program is a wrong answer, so
	// this door declines the pair (roadmap Gap R.352).
	if !compIsSingleClause(c) {
		return nil, false
	}
	if c.ForVar == nil || len(c.Elems) > 1 || len(c.Keys) > 1 || len(c.Vals) > 1 {
		return nil, false
	}
	items, itemExprs, err := g.compItems(c)
	if err != nil {
		return nil, false
	}
	switch c.Kind {
	case CompSet:
		if len(c.Elems) != 1 {
			return nil, false
		}
		// The fold and the fold-QUESTION are one question (roadmap Gap R.353, ADR 0384): asking the
		// folder alone let `{1: None for x in [1]}` fold the void to the integer 0 and bind a dict
		// whose value prints `0`, while `comp()` — which asks both — built the member the tag names.
		if !g.setCompFolds(c, items, itemExprs) {
			return nil, false
		}
		if lit, _, ferr := g.foldSetComp(c, items, itemExprs); ferr == nil {
			return lit, true
		}
	case CompDict:
		if len(c.Keys) != 1 || len(c.Vals) != 1 {
			return nil, false
		}
		if !g.dictCompFolds(c, items, itemExprs) {
			return nil, false
		}
		if lit, _, _, ferr := g.foldDictComp(c, items, itemExprs); ferr == nil {
			return lit, true
		}
	}
	return nil, false
}

// compLiteralAsksForTheTaggedLoop asks whether an iterable written as a list literal contains anything
// the integer folder cannot name. Such a literal is not a compile-time array of ints; its elements are
// values whose kind has to travel with them, exactly as in a `for` over the same literal. The answer is
// deliberately about the ITERABLE, not the comprehension's element — a list of texts whose element is an
// arithmetic expression still needs the loop variable's tag before the arithmetic is lowered
// (roadmap Gap R.358, ADR 0386).
func compLiteralAsksForTheTaggedLoop(ll *ListLit) bool {
	if ll == nil {
		return false
	}
	for _, el := range ll.Elems {
		if _, isInt := constantTextIntLiteral(el); !isInt {
			return true
		}
	}
	return false
}

// constantTextIntLiteral recognizes the integer-shaped literals the comprehension fold used to require.
// A unary minus over a number is still integer-shaped only when the folder can evaluate it; asking the
// literal node itself lets a negative number keep its fold road while every other kind moves to the loop
// that carries its tag.
func constantTextIntLiteral(e Expr) (int64, bool) {
	switch x := e.(type) {
	case *IntLit:
		return x.Value, true
	default:
		return 0, false
	}
}

// compIterIsStaticRange reports whether a comprehension's iterable is a range() call the compiler can
// enumerate: the callee has to be the plain name `range` and it has to carry at least one argument, the
// stop. A zero-argument call is not range() at all, and a call to anything else is a value the program
// computes rather than a bound the compiler reads (roadmap Gap R.291, ADR 0356).
func compIterIsStaticRange(e Expr) bool {
	r, ok := e.(*Call)
	if !ok || len(r.Args) == 0 {
		return false
	}
	fn, ok := r.Fn.(*Name)
	return ok && fn.Value == "range"
}

// normaliseCompIterable asks a comprehension's iterable the three questions the `for` road asks of its own,
// in the same order: fold it if the compiler already knows every element, materialise a `sorted`/`reversed`
// over a walkable list into the heap, or read a proven container answer into a name the loop can hold. It is
// the same sequence, spelled once here, that codegen's `for` arm applies before it picks a route; the two
// copies are why `[x for x in h(3)]` could answer on one road and print `[]` on the other (roadmap Gap R.291,
// Gap R.298, ADR 0357).
func (g *irGen) normaliseCompIterable(b *strings.Builder, e Expr) (Expr, bool) {
	if folded, ok := g.foldedForIterable(e); ok {
		return folded, true
	}
	if named, ok := g.materializeSortedReversedForLoop(b, e); ok {
		return named, true
	}
	if ansName, _, okAns := g.containerAnswerOperandRead(b, e, containerReadWalk); okAns {
		return ansName, true
	}
	return nil, false
}

// comp lowers a list comprehension over a constant iterable (inline list
// literal or range(n)) into a dedicated global struct, unrolled at compile
// time. Returns the global's name.
func (g *irGen) comp(b *strings.Builder, c *Comp) (string, error) {
	if c.ForVar == nil {
		return "", fmt.Errorf("codegen: comprehension must bind a loop variable")
	}
	if c.Kind != CompList && c.Kind != CompSet && c.Kind != CompDict {
		return "", fmt.Errorf("codegen: unsupported comprehension kind %d", c.Kind)
	}
	// A clause list — `[y for x in rows for y in x]` — is the nested loop its clauses spell. The roads
	// below all walk ONE clause, so a comprehension with more than one comes here first, and comes back
	// answered or comes back refused in the row's own words: never a wrong answer (roadmap Gap R.352).
	if !compIsSingleClause(c) {
		return g.nestedComp(b, c)
	}
	// A literal whose elements are not all integers is still a container the program wrote, and the
	// `for` road has long walked it by asking each slot what it holds. Until ADR 0386 the comprehension
	// asked the integer folder instead, so `[x for x in ["a", "b"]]` refused while the equivalent `for`
	// loop answered — and, worse, `[x for x in [None, True, False]]` took the fold's integer reading of
	// `None` and printed `[0, True, False]` at the exit code of success. The runtime loop is the road that
	// carries the element's tag; the fold keeps priority only for the all-integer literal it can name
	// outright (roadmap Gap R.358, ADR 0386).
	if ll, ok := c.Iter.(*ListLit); ok && compLiteralAsksForTheTaggedLoop(ll) {
		items := append([]Expr(nil), ll.Elems...)
		switch c.Kind {
		case CompList:
			return g.runtimeCompList(b, c, items)
		case CompSet, CompDict:
			return g.runtimeCompSetDict(b, c, items)
		}
	}
	// A container variable as the iterable has no compile-time length, so this comprehension runs
	// as a real loop over the heap list. The check comes before the item derivation because that
	// derivation only understands literals and range(), and a variable is neither (roadmap L11.7,
	// ADR 0192).
	// A literal or range() iterable is enumerable at compile time, so the fold below keeps
	// priority — min/max/sum over a comprehension read its folded elements, and taking the
	// runtime path first would make those refuse. Only an iterable the compiler cannot enumerate
	// (a container variable) needs the real loop (roadmap L11.7, ADR 0192).
	iterStatic := false
	switch c.Iter.(type) {
	case *ListLit:
		iterStatic = true
	case *Call:
		// Only `range(...)` is enumerable at compile time. Every other call is a value the program
		// has to compute, so the comprehension walking it belongs to the runtime loop: classifying
		// *every* call as static is how `[x for x in g()]` died with `index out of range [0] with
		// length 0` inside compItems — which opens `Args[0]` to read a bound that a zero-argument
		// call never had — and how `[y for y in f([1, 2, 3])]` refused "range bound must be a
		// constant" while the selfsame comprehension over a variable of the same list answered
		// (roadmap Gap R.291, ADR 0356).
		iterStatic = compIterIsStaticRange(c.Iter)
	}
	iterKind := containerKindFromTy(exprTyName(c.Iter))
	if !iterStatic && (g.isContainerExpr(c.Iter) || iterKind == "list" || isCallExpr(c.Iter) ||
		(c.Kind != CompList && (iterKind == "set" || iterKind == "dict"))) {
		return g.runtimeCompLoop(b, c)
	}
	// Determine the iteration items: an inline integer list literal or range(n).
	items, itemExprs, ierr := g.compItems(c)
	if ierr != nil {
		return "", ierr
	}
	// The path below needs every element to fold to an integer. When an element is a call, a
	// string, or anything else the folder cannot see — the ordinary `[f(x) for x in range(5)]`
	// — the comprehension is built at runtime instead, with the same per-item unrolling this
	// backend already does for `for` loops (roadmap L11.7, ADR 0192).
	if c.Kind == CompList && !g.comprehensionFolds(c, items, itemExprs) {
		return g.runtimeCompList(b, c, itemExprs)
	}
	// The set and the dict took the fold and nothing else until ADR 0384: an element, key, value or
	// filter the folder cannot see was a refusal (`comprehension element must be constant`) beside the
	// list-shaped twin of the same program answering, and the two shapes that did NOT refuse —
	// `{None for x in [1]}` printing `{0}` and `{1: None for x in [1]}` printing `{1: 0}` — were the
	// fold answering the void with the integer its truthiness is. The builder beside is the arm the
	// list road has had since ADR 0192, wearing the set's or the dict's clothes: the fold keeps
	// priority — ADR 0234's rule that a comprehension which folds IS the literal it folds to is what
	// makes `len`, `in` and `d[k]` answer a folded container — and what it cannot see is built one
	// member or entry at a time through the tagged doors `s.add(v)` and `d[k] = v` use
	// (roadmap Gap R.353, ADR 0244, ADR 0259, ADR 0310).
	if c.Kind == CompSet && !g.setCompFolds(c, items, itemExprs) {
		return g.runtimeCompSetDict(b, c, itemExprs)
	}
	if c.Kind == CompDict && !g.dictCompFolds(c, items, itemExprs) {
		return g.runtimeCompSetDict(b, c, itemExprs)
	}
	// Unroll the comprehension, binding the loop variable to each item.
	if g.constBindings == nil {
		g.constBindings = map[string]int64{}
	}
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
	var name string
	var results []int64
	switch c.Kind {
	case CompList:
		for _, item := range items {
			g.constBindings[c.ForVar.Value] = item
			v, ok := g.foldConstInt(c.Elems[0])
			if !ok {
				delete(g.constBindings, c.ForVar.Value)
				return "", fmt.Errorf("codegen: comprehension element must be constant")
			}
			if c.Cond != nil {
				cv, ok := g.foldConstInt(c.Cond)
				if !ok {
					delete(g.constBindings, c.ForVar.Value)
					return "", fmt.Errorf("codegen: comprehension condition must be constant")
				}
				if cv == 0 {
					delete(g.constBindings, c.ForVar.Value)
					continue
				}
			}
			results = append(results, v)
			delete(g.constBindings, c.ForVar.Value)
		}
		g.lstIdx++
		name = fmt.Sprintf("@.lst%d", g.lstIdx)
		if g.staticLists == nil {
			g.staticLists = map[string]*ListLit{}
		}
		lit := &ListLit{Elems: make([]Expr, 0, len(results))}
		for _, v := range results {
			lit.Elems = append(lit.Elems, &IntLit{Value: v})
		}
		g.staticLists[name] = lit
		var parts []string
		for _, v := range results {
			parts = append(parts, fmt.Sprintf("i32 %d", v))
		}
		n := len(results)
		g.globals.WriteString(fmt.Sprintf("%s = private global {i32, [%d x i32]} { i32 %d, [%d x i32] [%s] }\n", name, n, n, n, strings.Join(parts, ", ")))
	case CompSet:
		sl, vals, serr := g.foldSetComp(c, items, itemExprs)
		if serr != nil {
			return "", serr
		}
		results = vals
		g.setIdx++
		name = fmt.Sprintf("@.set%d", g.setIdx)
		if g.staticSets == nil {
			g.staticSets = map[string]*SetLit{}
		}
		g.staticSets[name] = sl
		var parts []string
		for _, v := range results {
			parts = append(parts, fmt.Sprintf("i32 %d", v))
		}
		n := len(results)
		g.globals.WriteString(fmt.Sprintf("%s = private global {i32, [%d x i32]} { i32 %d, [%d x i32] [%s] }\n", name, n, n, n, strings.Join(parts, ", ")))
	case CompDict:
		dl, keys, vals, derr := g.foldDictComp(c, items, itemExprs)
		if derr != nil {
			return "", derr
		}
		results = vals
		g.dictIdx++
		name = fmt.Sprintf("@.dict%d", g.dictIdx)
		if g.staticDicts == nil {
			g.staticDicts = map[string]*DictLit{}
		}
		g.staticDicts[name] = dl
		var kparts, vparts []string
		for _, k := range keys {
			kparts = append(kparts, fmt.Sprintf("i32 %d", k))
		}
		for _, v := range results {
			vparts = append(vparts, fmt.Sprintf("i32 %d", v))
		}
		n := len(keys)
		g.globals.WriteString(fmt.Sprintf("%s = private global {i32, [%d x i32], [%d x i32]} { i32 %d, [%d x i32] [%s], [%d x i32] [%s] }\n", name, n, n, n, n, strings.Join(kparts, ", "), n, strings.Join(vparts, ", ")))
		// record the folded keys so `d[key]` on a dict comprehension can be
		// resolved at codegen time.
		g.compKeys[c] = keys
	default:
		return "", fmt.Errorf("codegen: unsupported comprehension kind %d", c.Kind)
	}
	g.compNames[c] = name
	g.compLen[c] = len(results)
	g.compEls[c] = results
	return name, nil
}

// genExpr lowers a generator expression `(elem for var in iter [if cond])`
// to a runtime heap list handle, matching the record's eager semantics.
// The iterable is unrolled at codegen time when it is a constant range() call
// or list literal (mirroring comp()); each element is appended to the list.
func (g *irGen) genExpr(b *strings.Builder, gen *Generator) (string, error) {
	// A generator expression with a clause list is not refused at the parser any more, and this road
	// still unrolls only ONE clause — so a multi-clause generator asks the comprehension road's nested
	// door, which owns the loop nest, rather than being unrolled on one clause only and answering a
	// two-clause generator with a one-clause answer (roadmap Gap R.352). Both roads end at a heap list
	// handle, which is what the caller expects; the generator's own clause list is the argument.
	if len(gen.compClauses()) > 1 {
		if len(gen.Elems) != 1 {
			return "", fmt.Errorf("codegen: a generator expression with %d elements has no compiled lowering", len(gen.Elems))
		}
		return g.nestedComp(b, &Comp{
			Kind:    CompList,
			Elems:   gen.Elems,
			Clauses: gen.Clauses,
			ForVar:  gen.ForVar,
			Iter:    gen.Iter,
			Cond:    gen.Cond,
			Src:     gen.Src,
		})
	}
	var items []int64
	if r, ok := gen.Iter.(*Call); ok {
		// the parser represents a range(...) iterable as a Call (mirroring
		// comp()); unroll its (constant) bounds into a slice of items.
		start := int64(0)
		stop, ok := g.foldConstInt(r.Args[0])
		if !ok {
			return "", fmt.Errorf("codegen: generator range() stop must be constant")
		}
		step := int64(1)
		if len(r.Args) > 1 {
			start = stop
			stop, ok = g.foldConstInt(r.Args[1])
			if !ok {
				return "", fmt.Errorf("codegen: generator range() stop must be constant")
			}
		}
		if len(r.Args) > 2 {
			step, ok = g.foldConstInt(r.Args[2])
			if !ok {
				return "", fmt.Errorf("codegen: generator range() step must be constant")
			}
		}
		if step > 0 {
			for v := start; v < stop; v += step {
				items = append(items, v)
			}
		} else {
			for v := start; v > stop; v += step {
				items = append(items, v)
			}
		}
	} else if lst, ok := gen.Iter.(*ListLit); ok {
		for _, el := range lst.Elems {
			v, ok := g.foldConstInt(el)
			if !ok {
				return "", fmt.Errorf("codegen: generator list elements must be constant")
			}
			items = append(items, v)
		}
	}
	g.genExprIdx++
	h := fmt.Sprintf("%%gx%d", g.genExprIdx)
	g.heapUsed = true
	g.emitRtAllocKind(b, h, HeapKindList, gen.Span())
	if g.constBindings == nil {
		g.constBindings = map[string]int64{}
	}
	for _, item := range items {
		g.constBindings[gen.ForVar.Value] = item
		if gen.Cond != nil {
			if cv, ok := g.foldConstInt(gen.Cond); ok && cv == 0 {
				continue
			}
		}
		ev, ok := g.foldConstInt(gen.Elems[0])
		if !ok {
			evOp, err := g.value(b, gen.Elems[0])
			if err != nil {
				return "", err
			}
			b.WriteString(fmt.Sprintf("  call void @rt_append(i32 %s, i32 %s)\n", h, evOp))
			continue
		}
		b.WriteString(fmt.Sprintf("  call void @rt_append(i32 %s, i32 %d)\n", h, ev))
	}
	g.listOperands[h] = true
	return h, nil
}

// rangeBounds computes the loop start and stop operands for a for statement.
// A `range(a, b)` iterable yields start=a and stop=b; anything else starts at 0
// with stop being the single evaluated bound.
func (g *irGen) rangeBounds(b *strings.Builder, iter Expr) (string, string, string, error) {
	if c, ok := iter.(*Call); ok && c.Fn != nil {
		if n, ok2 := c.Fn.(*Name); ok2 && n.Value == "range" && (len(c.Args) == 2 || len(c.Args) == 3) {
			lo, err := g.value(b, c.Args[0])
			if err != nil {
				return "", "", "", err
			}
			hi, err := g.value(b, c.Args[1])
			if err != nil {
				return "", "", "", err
			}
			step := "1"
			if len(c.Args) == 3 {
				step, err = g.value(b, c.Args[2])
				if err != nil {
					return "", "", "", err
				}
				if step == "0" {
					return "", "", "", fmt.Errorf("range step cannot be zero")
				}
			}
			return lo, hi, step, nil
		}
	}
	hi, err := g.value(b, iter)
	if err != nil {
		return "", "", "", err
	}
	return "0", hi, "1", nil
}

// compTakesTheRuntimeRoad is the pair doors' side-effect-free version of the choice `comp` makes: does
// this comprehension's answer live in a heap object a builder filled, or in the global the fold filled?
// **ADR 0442** asked the question once the builder had run (for `len`), and **ADR 0443** asked it of the
// loop, condition and `bool()` with a handle in hand. A subscript's pair road has to choose before it may
// emit anything, because a door that declines owes the module no instruction; asking `containerHandleOf`
// first would materialise a folded global's object just to discover the fold owned it (roadmap
// **Gap R.501**, **ADR 0249**'s gate/door rule).
//
// It deliberately mirrors the first lines of `comp`, not a guess: a literal that asks for the tagged loop,
// a real iterable, or an element/key/value/filter the folder cannot see belongs to the builder; everything
// the fold can name belongs to the global. If the two ever disagree, the actual `rtComps` record wins as
// soon as a builder has filled it, and the pair road's second question keeps it from answering for a fold
// the road's own record does not claim.
func (g *irGen) compTakesTheRuntimeRoad(c *Comp) bool {
	if c == nil || c.ForVar == nil {
		return false
	}
	switch c.Kind {
	case CompList, CompSet, CompDict:
	default:
		return false
	}
	if g.rtComps[c] {
		return true
	}
	if !compIsSingleClause(c) {
		_, _, folded := g.nestedCompFold(c)
		return !folded
	}
	if ll, ok := c.Iter.(*ListLit); ok && compLiteralAsksForTheTaggedLoop(ll) {
		return true
	}
	iterStatic := false
	switch c.Iter.(type) {
	case *ListLit:
		iterStatic = true
	case *Call:
		iterStatic = compIterIsStaticRange(c.Iter)
	}
	iterKind := containerKindFromTy(exprTyName(c.Iter))
	if !iterStatic && (g.isContainerExpr(c.Iter) || iterKind == "list" || isCallExpr(c.Iter) ||
		(c.Kind != CompList && (iterKind == "set" || iterKind == "dict"))) {
		return true
	}
	items, itemExprs, err := g.compItems(c)
	if err != nil {
		return false
	}
	switch c.Kind {
	case CompList:
		return !g.comprehensionFolds(c, items, itemExprs)
	case CompSet:
		return !g.setCompFolds(c, items, itemExprs)
	case CompDict:
		return !g.dictCompFolds(c, items, itemExprs)
	}
	return false
}
