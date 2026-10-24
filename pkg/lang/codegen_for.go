// codegen_for.go is the loop road: `for` over every iterable the backend answers and `while`, including the induction shape a constant range folds to and the safe point the collector is invited to in a body that can allocate.
package lang

import (
	"fmt"
	"strings"
)

// stmtFor is the road a `for` takes, once its iterable has been normalised to a range the
// backend can lower.
func (g *irGen) stmtFor(b *strings.Builder, st Stmt, n *ForStmt) error {
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
		case g.mixedListRead(name.Value):
			// Iterating a mixed list binds the loop variable to an element *and* its
			// tag, so print(x) can dispatch per iteration (ADR 0185). The claim is asked through the
			// alias resolver: `ys = xs` shares the object and writes no record, so `for v in ys` printed
			// the void's payload where `for v in xs` printed `None` (roadmap L11.1, Gap R.415, ADR 0412).
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
		} else if h, t, okPair := g.slotPairWhereStaticLabelCannotWalk(b, ix); okPair && h != "" && t != "" {
			// The compiler CAN read this slot — and what it reads is a kind a loop cannot step over: a text,
			// a number, a verdict, the void. A static label of that shape is not permission to walk the
			// payload as a handle (that printed nothing for `for x in d[None]` over a text, and `0 1 2` over
			// the integer 5, both at the exit code of success) and not permission to fall through to the
			// counter road, which reads the word as a repeat count (Gap R.299's accident). The object's tag
			// chooses the arm, and raises the reference's `'int' object is not iterable` for a kind with no
			// elements (roadmap Gap R.285, ADR 0419; the same widening ADR 0323 gave a slot the compiler
			// could not describe).
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
	return nil
}

// stmtWhile is the road a `while` takes: the test, the body, the safe point.
func (g *irGen) stmtWhile(b *strings.Builder, st Stmt, n *WhileStmt) error {
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
	return nil
}
