// Calls: a user function, a method, an extern, an import, a class instantiation and the builtins — the road
// that decides what a Call answers.
package lang

import (
	"fmt"
	"math"
	"strconv"
	"strings"
	"unicode"
)

// call emits a call; supports print/printf and range(n).
func (g *irGen) call(b *strings.Builder, c *Call) (string, error) {
	fnName := ""
	if n, ok := c.Fn.(*Name); ok {
		fnName = n.Value
	} else if lam, ok := c.Fn.(*Lambda); ok {
		// inline lambda callee: `(lambda x: expr)(args)`.
		name, err := g.emitLambda(b, lam)
		if err != nil {
			return "", err
		}
		fnName = name
	}
	// `f(3)` where f was bound to a lambda: resolve to its FuncDef name.
	if fnName != "" {
		if b, ok := g.funcBind[fnName]; ok {
			fnName = b
		}
	}
	if fnName != "" {
		if lamName, ok := g.lambdas[fnName]; ok {
			fnName = lamName
		}
	}
	// constant-fold attr methods on constant receivers.
	if attr, ok := c.Fn.(*Attr); ok {
		// The one thing the shared container layout may not be allowed to do quietly: write. The
		// reference raises for every one of these on the immutable spelling — `TypeError` for the slot
		// writes, `AttributeError` for the methods a tuple does not have — and a road that appended to a
		// tuple happily would answer `(1, 2, 3)`, a value no program of this language can print in the
		// reference. The door refuses, naming the call, the class the reference raises, and the row that
		// owns the raise this shape still lacks (roadmap L11.3, ADR 0361).
		if containerMutators[attr.Name.Value] && g.receiverIsTuple(attr.Obj) {
			return "", refusesTupleMutation("`"+attr.Name.Value+"()`", "AttributeError",
				"'tuple' object has no attribute '"+attr.Name.Value+"'")
		}
		// mod.fn(args): dispatch to an AOT module function.
		if modName, isName := attr.Obj.(*Name); isName {
			if modFuncs := g.imports.Funcs[modName.Value]; modFuncs != nil {
				if fd, ok := modFuncs[attr.Name.Value]; ok && fd != nil {
					mangle := modName.Value + "$" + attr.Name.Value
					ret := g.newTmp()
					b.WriteString(fmt.Sprintf("  %s = call i32 @%s(", ret, irSymbol(mangle)))
					for i, arg := range c.Args {
						if i > 0 {
							b.WriteString(", ")
						}
						av, err := g.value(b, arg)
						if err != nil {
							return "", err
						}
						b.WriteString("i32 " + av)
					}
					b.WriteString(")\n")
					return ret, nil
				}
			}
		}
		mname := attr.Name.Value

		// super().m(args): dispatch m on the base class of the enclosing class.
		if call, isSuper := attr.Obj.(*Call); isSuper && len(call.Args) == 0 {
			if n, isN := call.Fn.(*Name); isN && n.Value == "super" {
				base := ""
				if ci := g.classInfos[g.selfClass]; ci != nil && len(ci.bases) > 0 {
					base = ci.bases[0]
				}
				if fn, ok := g.resolveMethod(base, mname); ok {
					// Every argument is emitted BEFORE the call line is started: a value that
					// needs an instruction of its own (interning a string literal does) would
					// otherwise be written into the middle of the operand list (Gap R.42).
					argRegs := make([]string, len(c.Args))
					for i, arg := range c.Args {
						av, err := g.value(b, arg)
						if err != nil {
							return "", err
						}
						argRegs[i] = ", i32 " + av
					}
					ret := g.newTmp()
					b.WriteString(fmt.Sprintf("  %s = call i32 @%s(i32 %%self%s)\n", ret, fn, strings.Join(argRegs, "")))
					// A method is program code, and program code raises: without the
					// call-site check the exception stayed in flight, the method returned
					// its unwind value, and the caller carried on printing (Gap R.41, ADR 0223).
					g.checkExn(b)
					return ret, nil
				}
			}
		}

		// Class method dispatch: `recv.m(args)` where recv's class is known.
		if className := g.receiverClass(attr.Obj); className != "" {
			if fn, ok := g.resolveMethod(className, mname); ok {
				recvHandle, err := g.value(b, attr.Obj)
				if err != nil {
					return "", err
				}
				argRegs := make([]string, len(c.Args))
				for i, arg := range c.Args {
					av, err := g.value(b, arg)
					if err != nil {
						return "", err
					}
					argRegs[i] = ", i32 " + av
				}
				ret := g.newTmp()
				b.WriteString(fmt.Sprintf("  %s = call i32 @%s(i32 %s%s)\n", ret, fn, recvHandle, strings.Join(argRegs, "")))
				g.checkExn(b) // a method call propagates like any other call (Gap R.41, ADR 0223)
				return ret, nil
			}
		}

		// Dynamic dispatch: `recv.m(args)` where recv is a class instance whose
		// class is unknown at compile time. Dispatch on the runtime class-id
		// stored in instance slot 0 (set at instantiation).
		if mname := attr.Name.Value; g.hasMethod(mname) {
			h, _ := g.value(b, attr.Obj)
			// Runtime dispatch operates on the canonical %obj-tagged value
			// representation: the receiver is wrapped as {tag=instance,
			// payload=heap-handle}, its kind tag is verified, and the payload
			// (the instance heap handle) is extracted before the class-id is
			// read from instance slot 0. The compiled runtime and the retired engine both
			// derived these tags from the same canonical table (value.go).
			recv := g.newTmp() // %obj
			b.WriteString(fmt.Sprintf("  %s = call %%obj @rt_mkobj(i32 %d, i32 %s)\n", recv, int(TagInstance), h))
			isInst := g.newTmp() // i1
			b.WriteString(fmt.Sprintf("  %s = call i1 @rt_obj_is(%%obj %s, i32 %d)\n", isInst, recv, int(TagInstance)))
			h2 := g.newTmp() // i32 payload = the instance heap handle
			b.WriteString(fmt.Sprintf("  %s = call i32 @rt_obj_payload(%%obj %s)\n", h2, recv))
			cid := g.newTmp()
			b.WriteString(fmt.Sprintf("  %s = call i32 @rt_inst_get(i32 %s, i32 0)\n", cid, h2))
			argvals := make([]string, len(c.Args))
			for i, a := range c.Args {
				v, _ := g.value(b, a)
				argvals[i] = v
			}
			type dynCase struct {
				id int
				// cont is the block the exception check branches into; the join below has to be
				// entered from there, not from the call's own arm, once the call can raise (ADR 0223).
				fn, lab, ret, cont string
			}
			var cases []dynCase
			for _, cls := range g.classOrder {
				if fn, ok := g.resolveMethod(cls, mname); ok {
					cases = append(cases, dynCase{id: g.classIDs[cls], fn: fn, lab: g.newLabel("dyn.c"), ret: g.newTmp()})
				}
			}
			done := g.newLabel("dyn.done")
			miss := g.newLabel("dyn.miss")
			b.WriteString(fmt.Sprintf("  switch i32 %s, label %%%s [\n", cid, miss))
			for _, dc := range cases {
				b.WriteString(fmt.Sprintf("    i32 %d, label %%%s\n", dc.id, dc.lab))
			}
			b.WriteString("  ]\n")
			for i := range cases {
				dc := &cases[i]
				b.WriteString(fmt.Sprintf("%s:\n", dc.lab))
				b.WriteString(fmt.Sprintf("  %s = call i32 @%s(i32 %s", dc.ret, dc.fn, h))
				for _, av := range argvals {
					b.WriteString(fmt.Sprintf(", i32 %s", av))
				}
				b.WriteString(")\n")
				// The check has to happen inside the arm, and the arm's `phi` incoming label
				// becomes the check's continuation -- a call site that raises cannot also be a
				// value producer for the join (Gap R.41, ADR 0223).
				dc.cont = g.checkExnLabel(b)
				b.WriteString(fmt.Sprintf("  br label %%%s\n", done))
			}
			b.WriteString(fmt.Sprintf("%s:\n", miss))
			// Runtime miss: the receiver is not an instance of a class that
			// defines this method. Return 0 (the interpreter raises; AOT returns
			// a sentinel since no runtime error infrastructure exists).
			b.WriteString(fmt.Sprintf("  br label %%%s\n", done))
			b.WriteString(fmt.Sprintf("%s:\n", done))
			phi := g.newTmp()
			b.WriteString(fmt.Sprintf("  %s = phi i32 [ 0, %%%s ]", phi, miss))
			for _, dc := range cases {
				b.WriteString(fmt.Sprintf(", [ %s, %%%s ]", dc.ret, dc.cont))
			}
			b.WriteString("\n")
			return phi, nil
		}
		if nm, ok := attr.Obj.(*Name); ok && g.runtimeSets[nm.Value] {
			// Set methods on a heap set: without `add` the `set()` constructor produced
			// a value nothing could grow (roadmap Gap K.3).
			switch attr.Name.Value {
			case "add", "discard":
				if len(c.Args) != 1 {
					return "", fmt.Errorf("codegen: %s() takes exactly 1 argument", attr.Name.Value)
				}
				if g.mixedSets[nm.Value] {
					// Growing a set whose members describe themselves has to keep that promise:
					// the member travels with its tag, and adding without one would make the new
					// member print as whatever kind its slot held last (ADR 0232).
					mv, mt, ok := g.taggedOperand(b, c.Args[0])
					if !ok {
						return "", fmt.Errorf("codegen: %s.%s(%s) adds a member whose kind the compiler cannot prove to a set whose members are of more than one kind; a float or container member needs the tagged value word (roadmap L11.1, ADR 0232)", nm.Value, attr.Name.Value, g.exprSummary(c.Args[0]))
					}
					g.heapSeq++
					hs := g.heapSeq
					b.WriteString(fmt.Sprintf("  %%h%d = load i32, i32* %%%s\n", hs, "_"+nm.Value))
					fn := "rt_set_add_tagged"
					if attr.Name.Value == "discard" {
						fn = "rt_set_discard_tagged"
					}
					b.WriteString(fmt.Sprintf("  call void @%s(i32 %%h%d, i32 %s, i32 %s)\n", fn, hs, mv, mt))
					return "", nil
				}
				// A member the pair road bound hands the set its payload and its tag as two registers,
				// and the tagged add is the door that takes them — adding and tagging are one operation,
				// because a member added without its tag dedups against the payload alone and prints
				// through whatever the slot last held (ADR 0187, ADR 0232). A member is also the position
				// that has to be hashable, and only the tag can say whether it is (Gap R.81, ADR 0310's
				// literal road; roadmap L11.1, Gap R.146, ADR 0311).
				if pv, pt, isPair := g.pairElemPair(b, c.Args[0]); isPair {
					g.heapSeq++
					hs := g.heapSeq
					b.WriteString(fmt.Sprintf("  %%h%d = load i32, i32* %%%s\n", hs, "_"+nm.Value))
					g.promotePairMixed(b, fmt.Sprintf("%%h%d", hs), nm.Value, "set")
					g.guardHashableTag(b, pt, c.Src)
					addFn := "rt_set_add_tagged"
					if attr.Name.Value == "discard" {
						addFn = "rt_set_discard_tagged"
					}
					b.WriteString(fmt.Sprintf("  call void @%s(i32 %%h%d, i32 %s, i32 %s)\n", addFn, hs, pv, pt))
					return "", nil
				}
				av, interned, err := g.heapElemKind(b, c.Args[0])
				if err != nil {
					return "", err
				}
				if g.printsAsInternedStr(c.Args[0]) {
					interned = true
				}
				g.heapSeq++
				hs := g.heapSeq
				b.WriteString(fmt.Sprintf("  %%h%d = load i32, i32* %%%s\n", hs, "_"+nm.Value))
				if err := g.recordElemKind(nm.Value, "set", interned, c.Args[0]); err != nil {
					// A set grown with a member of the other kind is promoted, not refused, for
					// the same reason an appended element is (ADR 0232).
					if !g.promoteMixed(b, fmt.Sprintf("%%h%d", hs), nm.Value, "set", c.Args[0]) {
						return "", err
					}
				}
				fn := "rt_set_add"
				if attr.Name.Value == "discard" {
					fn = "rt_set_discard"
				}
				// A member that is a float or None has no payload the set's compiled kind can render,
				// so the set stops claiming one — the same promotion an appended element gets
				// (roadmap L11.1, ADR 0233).
				if !g.mixedSets[nm.Value] {
					if mt, mok := g.elemKindTag(c.Args[0]); mok && slotTagSelfDescribing(mt) {
						if g.promoteMixed(b, fmt.Sprintf("%%h%d", hs), nm.Value, "set", c.Args[0]) {
							g.floatFmtUsed = g.floatFmtUsed || mt == int32(TagFloat)
						}
					}
				}
				if kt, ok := g.elemKindTag(c.Args[0]); ok && attr.Name.Value == "add" {
					b.WriteString(fmt.Sprintf("  call void @rt_set_add_tagged(i32 %%h%d, i32 %s, i32 %d)\n", hs, av, kt))
				} else {
					b.WriteString(fmt.Sprintf("  call void @%s(i32 %%h%d, i32 %s)\n", fn, hs, av))
				}
				if interned {
					// The object, not the variable, records that its members are strings: a helper
					// that fills a container it was handed would otherwise leave the caller's print
					// rendering raw indices (roadmap Gap J.5).
					b.WriteString(fmt.Sprintf("  call void @rt_mark_estr(i32 %%h%d, i32 1)\n", hs))
				}
				return "", nil
			case "clear":
				if len(c.Args) != 0 {
					return "", fmt.Errorf("codegen: clear() takes no arguments")
				}
				g.heapSeq++
				hs := g.heapSeq
				b.WriteString(fmt.Sprintf("  %%h%d = load i32, i32* %%%s\n", hs, "_"+nm.Value))
				b.WriteString(fmt.Sprintf("  call void @rt_set_clear(i32 %%h%d)\n", hs))
				return "", nil
			}
		}
		if nm, ok := attr.Obj.(*Name); ok && g.listVars[nm.Value] && attr.Name.Value == "pop" {
			// xs.pop() removes and returns the last element; xs.pop(i) removes index i
			// (negative counts from the end). The bounds test is emitted around the
			// removal so an out-of-range index raises IndexError through the same
			// exception path a `raise` uses (roadmap Gap K.3).
			if len(c.Args) > 1 {
				return "", fmt.Errorf("codegen: pop() takes at most 1 argument (xs.pop(), xs.pop(i))")
			}
			g.heapSeq++
			hs := g.heapSeq
			h := fmt.Sprintf("%%h%d", hs)
			b.WriteString(fmt.Sprintf("  %s = load i32, i32* %%%s\n", h, "_"+nm.Value))
			n := g.newTmp()
			b.WriteString(fmt.Sprintf("  %s = call i32 @rt_list_len(i32 %s)\n", n, h))
			idx := g.newTmp()
			badL, okL := g.newLabel("pop.bad"), g.newLabel("pop.ok")
			var bad string
			if len(c.Args) == 1 {
				iv, err := g.value(b, c.Args[0])
				if err != nil {
					return "", err
				}
				neg := g.newTmp()
				b.WriteString(fmt.Sprintf("  %s = icmp slt i32 %s, 0\n", neg, iv))
				g.markI1(neg)
				adj := g.newTmp()
				b.WriteString(fmt.Sprintf("  %s = add i32 %s, %s\n", adj, iv, n))
				b.WriteString(fmt.Sprintf("  %s = select i1 %s, i32 %s, i32 %s\n", idx, neg, adj, iv))
				hi := g.newTmp()
				b.WriteString(fmt.Sprintf("  %s = icmp sge i32 %s, %s\n", hi, idx, n))
				g.markI1(hi)
				lo := g.newTmp()
				b.WriteString(fmt.Sprintf("  %s = icmp slt i32 %s, 0\n", lo, idx))
				g.markI1(lo)
				bad = g.newTmp()
				b.WriteString(fmt.Sprintf("  %s = or i1 %s, %s\n", bad, lo, hi))
				g.markI1(bad)
			} else {
				// No index: the last element, and an empty list is the error case.
				b.WriteString(fmt.Sprintf("  %s = sub i32 %s, 1\n", idx, n))
				bad = g.newTmp()
				b.WriteString(fmt.Sprintf("  %s = icmp eq i32 %s, 0\n", bad, n))
				g.markI1(bad)
			}
			b.WriteString(fmt.Sprintf("  br i1 %s, label %%%s, label %%%s\n", bad, badL, okL))
			b.WriteString(fmt.Sprintf("%s:\n", badL))
			if len(c.Args) == 1 {
				g.raiseTo(b, exnCode("IndexError"), "IndexError", "pop index out of range", c.Span())
			} else {
				g.raiseTo(b, exnCode("IndexError"), "IndexError", "pop from empty list", c.Span())
			}
			b.WriteString(fmt.Sprintf("%s:\n", okL))
			ret := g.newTmp()
			b.WriteString(fmt.Sprintf("  %s = call i32 @rt_pop(i32 %s, i32 %s)\n", ret, h, idx))
			return ret, nil
		}
		// xs.sort() and xs.reverse() mutate in place and return None. Neither existed in
		// either backend, and the compiled path diagnosed xs.sort() as a *string* method:
		// the call fell through to string-method dispatch and told the user their list was
		// a string (roadmap L11.7, ADR 0191).
		if nm, ok := attr.Obj.(*Name); ok && g.listVars[nm.Value] &&
			(attr.Name.Value == "sort" || attr.Name.Value == "reverse") {
			if len(c.Args) != 0 {
				return "", fmt.Errorf("%s() takes no arguments in this build — key= and reverse= need first-class functions (roadmap L11.7)", attr.Name.Value)
			}
			if attr.Name.Value == "sort" && g.mixedLists[nm.Value] {
				// Python raises TypeError for a str/int mix rather than inventing an order,
				// and ordering interned strings by payload would sort them by arrival.
				return "", fmt.Errorf("cannot sort a list whose elements are of more than one kind; Python raises TypeError here too, and the compiled backend reports it (roadmap L11.1, ADR 0191)")
			}
			g.heapSeq++
			hslot := g.heapSeq
			b.WriteString(fmt.Sprintf("  %%h%d = load i32, i32* %%_%s\n", hslot, nm.Value))
			if attr.Name.Value == "sort" {
				mode := 0
				if g.listElemStr[nm.Value] {
					mode = 1
				}
				b.WriteString(fmt.Sprintf("  call void @rt_sort(i32 %%h%d, i32 %d)\n", hslot, mode))
			} else {
				b.WriteString(fmt.Sprintf("  call void @rt_reverse(i32 %%h%d)\n", hslot))
			}
			return "", nil
		}
		if nm, ok := attr.Obj.(*Name); ok && g.listVars[nm.Value] && attr.Name.Value == "append" {
			if len(c.Args) != 1 {
				return "", fmt.Errorf("append expects one argument")
			}
			// `xs.append(build())` puts the container the callee built into a slot. The answer is materialised
			// to a name first, so the slot is filled the way `ys = build()` / `xs.append(ys)` fills it and every
			// question below — pair, tag, kind — is asked of a container variable rather than of a call this
			// road can only lower to one word (roadmap Gap R.67, ADR 0333).
			arg := c.Args[0]
			if ansName, _, okAns := g.containerAnswerOperand(b, arg); okAns {
				arg = ansName
			}
			// An element the pair road bound carries its kind in a register, and the tagged append is
			// the door that takes it: payload and tag are one operation (ADR 0187), and the list stops
			// claiming a kind this pass cannot state, because the kind is now a register the objects
			// wrote (roadmap L11.1, Gap R.146; ADR 0306's literal element, ADR 0232's promotion, ADR 0311).
			if pv, pt, isPair := g.pairElemPair(b, arg); isPair {
				g.heapSeq++
				hs := g.heapSeq
				b.WriteString(fmt.Sprintf("  %%h%d = load i32, i32* %%_%s\n", hs, nm.Value))
				g.promotePairMixed(b, fmt.Sprintf("%%h%d", hs), nm.Value, "list")
				b.WriteString(fmt.Sprintf("  call void @rt_append_tagged(i32 %%h%d, i32 %s, i32 %s)\n", hs, pv, pt))
				return "", nil
			}
			if g.mixedLists[nm.Value] {
				// Appending to a tagged list writes the payload and the tag together:
				// rt_append alone would leave the new slot reading back with whatever tag the
				// freed slot last carried, which is how an appended string would print as its
				// interned index (roadmap L11.1, ADR 0187).
				if av, tag, terr := g.mixedElemTag(b, arg); terr == nil {
					g.heapSeq++
					hs := g.heapSeq
					b.WriteString(fmt.Sprintf("  %%h%d = load i32, i32* %%_%s\n", hs, nm.Value))
					b.WriteString(fmt.Sprintf("  call void @rt_append_tagged(i32 %%h%d, i32 %s, i32 %s)\n", hs, av, tag))
					return "", nil
				} else {
					return "", terr
				}
			}
			av, interned, err := g.heapElemKind(b, arg)
			if err != nil {
				return "", err
			}
			// The value is asked twice what it is: heapElemKind answers for the word it interned,
			// printsAsInternedStr for the value the source says it is, and `xs.append(d["a"])` is
			// the shape where only the second one knows (ADR 0232).
			if g.printsAsInternedStr(arg) {
				interned = true
			}
			g.heapSeq++
			hs := g.heapSeq
			b.WriteString(fmt.Sprintf("  %%h%d = load i32, i32* %%_%s\n", hs, nm.Value))
			if err := g.recordElemKind(nm.Value, "list", interned, arg); err != nil {
				// Growing a list with a value of the other kind used to be the refusal, because
				// the container records one kind and would print the new element through the old
				// one. The slots carry tags, so the honest move is to stop claiming a kind
				// (ADR 0232); the refusal stays for a value no tag can describe.
				if !g.promoteMixed(b, fmt.Sprintf("%%h%d", hs), nm.Value, "list", c.Args[0]) {
					return "", err
				}
			}
			// A float or None element is the case the container's one compiled kind cannot render:
			// its payload is a box handle or nothing at all, so printing the list through the number
			// printer shows 1 where Python shows 1.5. The slots already carry tags, so the honest
			// move is the one ADR 0232 made for strings — stop claiming a kind (roadmap L11.1, ADR 0233).
			if !g.mixedLists[nm.Value] {
				if t, ok := g.elemKindTag(arg); ok && slotTagSelfDescribing(t) {
					if g.promoteMixed(b, fmt.Sprintf("%%h%d", hs), nm.Value, "list", arg) {
						g.floatFmtUsed = g.floatFmtUsed || t == int32(TagFloat)
					}
				}
			}
			// Payload and tag are one operation, so an append cannot leave the new slot carrying
			// the tag of whoever held it last (ADR 0187, ADR 0189): there is no untagged append
			// left to fall back to.
			b.WriteString(fmt.Sprintf("  call void @rt_append_tagged(i32 %%h%d, i32 %s, i32 %s)\n", hs, av, g.elemTagOperand(b, arg, interned)))
			if interned {
				b.WriteString(fmt.Sprintf("  call void @rt_mark_estr(i32 %%h%d, i32 1)\n", hs))
			}
			return "", nil
		}

		// list method: `[1, 2, 3].append(4)` -> [1, 2, 3, 4].
		if ll, ok := attr.Obj.(*ListLit); ok {
			if attr.Name.Value == "append" {
				if len(c.Args) != 1 {
					return "", fmt.Errorf("append expects one argument")
				}
				elems := append(append([]Expr{}, ll.Elems...), c.Args[0])
				return g.value(b, &ListLit{Elems: elems})
			}
			// Three words ("unsupported list method index") tell a reader nothing they can act on. The
			// sentence now says which door this is, what the reference does for this method, and the row
			// that owns the rest (ADR 0166's rule: a refusal names the half it lacks).
			return "", fmt.Errorf("codegen: %s is refused: this backend builds the list methods it can lower against the container's own word (append, extend, insert, remove, pop, clear, sort, reverse, index-of-a-literal, count, and membership), and %s needs a run-time search the compiled doors do not have — the reference answers it (with %s when the value is absent), which needs the value word that carries its own kind (roadmap L11.1, Gap I.2)", exprSurface(&Attr{Obj: attr.Obj, Name: attr.Name, Src: attr.Src}), attr.Name.Value, referenceMethodTrap(attr.Name.Value))
		}
		// dict methods: `{1: 2, 3: 4}.keys()` -> [1, 3], `.values()` -> [2, 4].
		if dl, ok := attr.Obj.(*DictLit); ok {
			switch attr.Name.Value {
			case "keys":
				return g.value(b, &ListLit{Elems: dl.Keys})
			case "values":
				return g.value(b, &ListLit{Elems: dl.Vals})
			case "get":
				// dict.get(key, default) returns the value for key or the default.
				if len(c.Args) < 1 || len(c.Args) > 2 {
					return "", fmt.Errorf("get expects 1 or 2 arguments")
				}
				// int key lookup
				if kv, kerr := g.constIntVal(c.Args[0]); kerr == nil {
					for i, k := range dl.Keys {
						if il, ok := k.(*IntLit); ok && il.Value == kv {
							return g.value(b, dl.Vals[i])
						}
					}
				} else if sv, ok := stringConst(c.Args[0], g.builtinShadowed); ok {
					// string key lookup
					for i, k := range dl.Keys {
						if sl, ok := k.(*StrLit); ok && sl.Value == sv {
							return g.value(b, dl.Vals[i])
						}
					}
				}
				// not found: return the default value, if given
				if len(c.Args) == 2 {
					return g.value(b, c.Args[1])
				}
				// A missing key with no default hands back NONE — the reference's answer, and the
				// interpreter's since this cycle. Refusing here (`get: key not found and no default`)
				// turned a program CPython prints `None` for into an error at exit 1, while the fold
				// already had every fact it needed: the key is absent, so the answer is void. The void
				// lowers to the word 0 and `isNoneExpr` renders it `None` through rt_print_none, which is
				// the same arrangement ADR 0172 set for a void return (roadmap Gap R.174, ADR 0291).
				return g.value(b, &NoneLit{})
			default:
				return "", fmt.Errorf("codegen: %s is refused: this backend builds the dict methods it can lower against the container's own word (get, keys, values, items, clear, and a literal read), and %s needs a run-time search the compiled doors do not have — the reference answers it (with %s when the key is absent), which needs the value word that carries its own kind (roadmap L11.1, Gap I.2)", exprSurface(&Attr{Obj: attr.Obj, Name: attr.Name, Src: attr.Src}), attr.Name.Value, referenceMethodTrap(attr.Name.Value))
			}
		}
		// string methods: `"AbC".upper()`, `.lower()`, `.strip()`.
		v, ok := g.stringVal(attr.Obj)
		if !ok {
			// The receiver's name, when the receiver is one: `xs.append(2)` over a module list
			// is not a string method with a non-constant string, and saying so is a lie about
			// the program (Gap R.38).
			if nm, ok2 := attr.Obj.(*Name); ok2 {
				if err := g.moduleStateErr(nm.Value); err != nil {
					return "", err
				}
			}
			// upper() and lower() of a string the compiler cannot read ask the table
			// (ADR 0229). The compiled fold covers ASCII and copies other bytes unchanged;
			// the interpreter has the full case tables — the difference is recorded as a
			// limit of this cycle, not hidden (roadmap Gap R.47).
			if sreg, isStr, err2 := g.strReg(b, attr.Obj); err2 != nil {
				return "", err2
			} else if isStr {
				// A text the compiler cannot read — a parameter, an element, a call's answer — is folded by
				// the runtime door, and since ADR 0362 the door reads the same generated table the fold
				// above does, so the two answers cannot part company. It answers the three operations whose
				// rules the old ASCII loop had no room for as well: title, capitalize and swapcase.
				switch attr.Name.Value {
				case "upper", "lower", "title", "capitalize", "swapcase":
					op := caseOpOfMethod(attr.Name.Value)
					if op < 0 {
						return "", fmt.Errorf("codegen: %s() has no case operation (roadmap L11.5, ADR 0362)", attr.Name.Value)
					}
					g.caseOps |= 1 << uint(op)
					return g.rtStrCall(b, "rt_str_case", "i32 "+sreg, "i32 "+strconv.Itoa(op)), nil
				case "strip", "lstrip", "rstrip":
					// A text the compiler cannot read is trimmed by the door, which walks the artifact's
					// whitespace set — the same one caseFoldTrim read above (ADR 0232).
					return g.emitTrimRoad(b, c, attr.Name.Value, sreg)
				}
			}
			// A NAME bound to a container is not a string with a value the compiler cannot read, and
			// saying "string method keys on non-constant string" about `d = {"a": 1}` / `d.keys()`
			// describes a program the author did not write (Gap R.38's rule). The records that decide
			// this are the same ones the loop and subscript roads read, so the refusal cannot disagree
			// with the codegen beside it (roadmap Gap R.174, ADR 0291).
			if nm, ok2 := attr.Obj.(*Name); ok2 && g.nameHoldsContainer(nm.Value) {
				return "", fmt.Errorf("%s() on %q, which the program bound to a container rather than a text: the compiled backend folds container methods only over a literal written at the call, and %q is a name whose slots the runtime owns — CPython answers this program, and the compiled leg waits for the tagged value word (roadmap L11.1, Gap R.174, ADR 0166)", attr.Name.Value, nm.Value, nm.Value)
			}
			return "", fmt.Errorf("string-method %s on a receiver that is not a text the compiler can read: %s (roadmap Gap I.2, ADR 0166)", attr.Name.Value, g.exprSummary(attr.Obj))
		}
		switch attr.Name.Value {
		// The third fold road, and the only one of the three that can say the table would not answer:
		// it is inside a road that returns an error, so it says so rather than keeping the old answer
		// (roadmap L11.5, ADR 0362).
		case "upper":
			t2, cerr := caseMapString(caseUpper, v)
			if cerr != nil {
				return "", cerr
			}
			v = t2
		case "capitalize":
			t2, cerr := caseMapString(caseCapitalize, v)
			if cerr != nil {
				return "", cerr
			}
			v = t2
		case "title":
			t2, cerr := caseMapString(caseTitle, v)
			if cerr != nil {
				return "", cerr
			}
			v = t2
		case "swapcase":
			t2, cerr := caseMapString(caseSwapcase, v)
			if cerr != nil {
				return "", cerr
			}
			v = t2
		case "lower":
			t2, cerr := caseMapString(caseLower, v)
			if cerr != nil {
				return "", cerr
			}
			v = t2
		case "strip", "lstrip", "rstrip":
			// The fold answers when it can read BOTH texts. A cutset it cannot read does not make the question
			// unanswerable — the receiver is right here, and the door beside it reads the same artifact (ADR 0232).
			if len(c.Args) > 1 {
				return "", fmt.Errorf("%s() takes one cutset at most; the reference raises TypeError for %d arguments (roadmap L11.5, ADR 0362)", attr.Name.Value, len(c.Args))
			}
			if len(c.Args) == 1 {
				if _, okCut := g.stringVal(c.Args[0]); !okCut {
					return g.emitTrimRoad(b, c, attr.Name.Value, g.internStr(b, v))
				}
			}
			t2, ok3 := caseFoldTrim(caseTrimMode(attr.Name.Value), v, c.Args, g.stringVal)
			if !ok3 {
				return "", fmt.Errorf("the trim table would not answer %s() (roadmap L11.5, ADR 0362)", attr.Name.Value)
			}
			v = t2
		case "replace":
			if len(c.Args) != 2 {
				return "", fmt.Errorf("replace() takes exactly 2 arguments")
			}
			oldv, ok := g.stringVal(c.Args[0])
			if !ok {
				return "", fmt.Errorf("replace() old must be a constant string")
			}
			newv, ok := g.stringVal(c.Args[1])
			if !ok {
				return "", fmt.Errorf("replace() new must be a constant string")
			}
			v = strings.ReplaceAll(v, oldv, newv)
		case "join":
			if len(c.Args) != 1 {
				return "", fmt.Errorf("join() takes exactly 1 argument")
			}
			ll, ok := c.Args[0].(*ListLit)
			if !ok {
				return "", fmt.Errorf("join() argument must be a constant list")
			}
			parts := []string{}
			for _, el := range ll.Elems {
				sv, ok := g.stringVal(el)
				if !ok {
					return "", fmt.Errorf("join() list elements must be constant strings")
				}
				parts = append(parts, sv)
			}
			v = strings.Join(parts, v)
		case "find":
			// s.find(sub) -> index of first occurrence of sub, or -1 if absent.
			if len(c.Args) != 1 {
				return "", fmt.Errorf("find() takes exactly 1 argument")
			}
			subv, ok := g.stringVal(c.Args[0])
			if !ok {
				return "", fmt.Errorf("find() argument must be a constant string")
			}
			return fmt.Sprintf("%d", strings.Index(v, subv)), nil
		case "rfind":
			// s.rfind(sub) -> index of last occurrence of sub, or -1 if absent.
			if len(c.Args) != 1 {
				return "", fmt.Errorf("rfind() takes exactly 1 argument")
			}
			subv, ok := g.stringVal(c.Args[0])
			if !ok {
				return "", fmt.Errorf("rfind() argument must be a constant string")
			}
			return fmt.Sprintf("%d", strings.LastIndex(v, subv)), nil
		case "count":
			// s.count(sub) -> number of non-overlapping occurrences of sub.
			if len(c.Args) != 1 {
				return "", fmt.Errorf("count() takes exactly 1 argument")
			}
			subv, ok := g.stringVal(c.Args[0])
			if !ok {
				return "", fmt.Errorf("count() argument must be a constant string")
			}
			return fmt.Sprintf("%d", strings.Count(v, subv)), nil
		case "isdigit":
			// s.isdigit() -> 1 if all runes are digits, else 0.
			res := 0
			if v != "" {
				all := true
				for _, r := range v {
					if !unicode.IsDigit(r) {
						all = false
						break
					}
				}
				if all {
					res = 1
				}
			}
			return fmt.Sprintf("%d", res), nil
		case "isalpha":
			// s.isalpha() -> 1 if all runes are alphabetic, else 0.
			res := 0
			if v != "" {
				all := true
				for _, r := range v {
					if !unicode.IsLetter(r) {
						all = false
						break
					}
				}
				if all {
					res = 1
				}
			}
			return fmt.Sprintf("%d", res), nil
		case "isalnum":
			res := 0
			if v != "" {
				all := true
				for _, r := range v {
					if !unicode.IsLetter(r) && !unicode.IsDigit(r) {
						all = false
						break
					}
				}
				if all {
					res = 1
				}
			}
			return fmt.Sprintf("%d", res), nil
		case "isspace":
			res := 0
			if v != "" {
				all := true
				for _, r := range v {
					if !unicode.IsSpace(r) {
						all = false
						break
					}
				}
				if all {
					res = 1
				}
			}
			return fmt.Sprintf("%d", res), nil
		case "islower":
			res := 0
			hasCased := false
			allLower := true
			for _, r := range v {
				if unicode.IsLower(r) {
					hasCased = true
				} else if unicode.IsUpper(r) {
					hasCased = true
					allLower = false
				}
			}
			if hasCased && allLower {
				res = 1
			}
			return fmt.Sprintf("%d", res), nil
		case "isupper":
			res := 0
			hasCased := false
			allUpper := true
			for _, r := range v {
				if unicode.IsUpper(r) {
					hasCased = true
				} else if unicode.IsLower(r) {
					hasCased = true
					allUpper = false
				}
			}
			if hasCased && allUpper {
				res = 1
			}
			return fmt.Sprintf("%d", res), nil
		case "startswith", "endswith":
			// s.startswith(sub) / s.endswith(sub) -> 1 or 0.
			if len(c.Args) != 1 {
				return "", fmt.Errorf("%s() takes exactly 1 argument", attr.Name.Value)
			}
			subv, ok := g.stringVal(c.Args[0])
			if !ok {
				return "", fmt.Errorf("%s() argument must be a constant string", attr.Name.Value)
			}
			res := 0
			if attr.Name.Value == "startswith" {
				if strings.HasPrefix(v, subv) {
					res = 1
				}
			} else if strings.HasSuffix(v, subv) {
				res = 1
			}
			return fmt.Sprintf("%d", res), nil
		case "ljust", "rjust":
			// ljust pads the receiver on the right with spaces to width w;
			// rjust pads on the left (no-op when len(v) >= w).
			if len(c.Args) != 1 {
				return "", fmt.Errorf("%s expects one argument", attr.Name.Value)
			}
			wv, werr := g.constIntVal(c.Args[0])
			if werr != nil {
				return "", fmt.Errorf("%s: codegen folds only a constant width arg", attr.Name.Value)
			}
			pad := int(wv) - len(v)
			if pad <= 0 {
				// A string produced by a call is an @str_tab index, not the address of a global (Gap R.42, ADR 0224).
				return g.internStr(b, v), nil
			}
			spaces := strings.Repeat(" ", pad)
			if attr.Name.Value == "ljust" {
				// A string produced by a call is an @str_tab index, not the address of a global (Gap R.42, ADR 0224).
				return g.internStr(b, v+spaces), nil
			}
			// A string produced by a call is an @str_tab index, not the address of a global (Gap R.42, ADR 0224).
			return g.internStr(b, spaces+v), nil
		case "zfill":
			// zfill pads the receiver on the left with '0' to width w
			// (no-op when len(v) >= w), mirroring the interpreter.
			if len(c.Args) != 1 {
				return "", fmt.Errorf("zfill expects one argument")
			}
			wv, werr := g.constIntVal(c.Args[0])
			if werr != nil {
				return "", fmt.Errorf("zfill: codegen folds only a constant width arg")
			}
			if int(wv) <= len(v) {
				// A string produced by a call is an @str_tab index, not the address of a global (Gap R.42, ADR 0224).
				return g.internStr(b, v), nil
			}
			// One rule with the interpreter — zfillTo pads after a leading sign, which neither
			// engine did when each padded on its own (roadmap Gap R.183, ADR 0297).
			return g.internStr(b, zfillTo(v, int(wv))), nil
		case "removeprefix", "removesuffix":
			// removeprefix strips the given prefix from the receiver;
			// removesuffix strips the suffix, mirroring strings.TrimPrefix/TrimSuffix.
			if len(c.Args) != 1 {
				return "", fmt.Errorf("%s expects one argument", attr.Name.Value)
			}
			sub, ok := g.stringVal(c.Args[0])
			if !ok {
				return "", fmt.Errorf("%s: codegen folds only a constant string arg", attr.Name.Value)
			}
			var res string
			if attr.Name.Value == "removeprefix" {
				res = strings.TrimPrefix(v, sub)
			} else {
				res = strings.TrimSuffix(v, sub)
			}
			// A string produced by a call is an @str_tab index, not the address of a global (Gap R.42, ADR 0224).
			return g.internStr(b, res), nil
		case "index":
			// "s".index(sub) returns the byte index of sub (strings.Index).
			// The interpreter raises on not-found; the AOT codegen has no error
			// channel, so it folds to -1 on not-found (like find).
			if len(c.Args) != 1 {
				return "", fmt.Errorf("index expects one argument")
			}
			sub, ok := g.stringVal(c.Args[0])
			if !ok {
				return "", fmt.Errorf("index: codegen folds only a constant string arg")
			}
			return fmt.Sprintf("%d", int64(strings.Index(v, sub))), nil
		case "expandtabs":
			// expandtabs replaces each tab with the spaces up to the next tab
			// stop at width w, tracking the running column, mirroring the
			// interpreter's tab-stop algorithm. Source string literals have no
			// escape sequences, so literal receivers contain no tabs (no-op).
			if len(c.Args) != 1 {
				return "", fmt.Errorf("expandtabs expects one argument")
			}
			wv, werr := g.constIntVal(c.Args[0])
			if werr != nil {
				return "", fmt.Errorf("expandtabs: codegen folds only a constant width arg")
			}
			w := int(wv)
			if w <= 0 {
				return "", fmt.Errorf("expandtabs width must be positive")
			}
			var sb strings.Builder
			col := 0
			for _, r := range v {
				if r == '\t' {
					n := w - (col % w)
					sb.WriteString(strings.Repeat(" ", n))
					col += n
				} else {
					sb.WriteRune(r)
					col++
				}
			}
			v = sb.String()
			// A string produced by a call is an @str_tab index, not the address of a global (Gap R.42, ADR 0224).
			return g.internStr(b, v), nil
		default:
			return "", fmt.Errorf("unsupported string method %s", attr.Name.Value)
		}
		// A string produced by a call is an @str_tab index, not the address of a global (Gap R.42, ADR 0224).
		return g.internStr(b, v), nil
	}

	// Class instantiation: `ClassName(args)`.
	if _, isClass := g.classInfos[fnName]; isClass {
		g.heapUsed = true
		h := g.newTmp()
		g.emitRtAllocKind(b, h, HeapKindInstance, c.Span())
		// A heap slot is reused, and an instance inherits nothing from whoever held it last — not
		// even which attributes exist. The clear zeroes the presence row; the slot count is final
		// because every name the program can write was interned before emission (ADR 0235).
		b.WriteString(fmt.Sprintf("  call void @rt_inst_clear(i32 %s, i32 %d)\n", h, g.nextSlot))
		b.WriteString(fmt.Sprintf("  call void @rt_inst_put(i32 %s, i32 0, i32 %d)\n", h, g.classIDs[fnName]))
		if fn, ok := g.resolveMethod(fnName, "__init__"); ok {
			// Compute each argument value first (each emits its own load
			// statement on a fresh line), then emit the call using the
			// computed temporaries so the IR stays well-formed.
			argTmp := make([]string, len(c.Args))
			for i, arg := range c.Args {
				av, err := g.value(b, arg)
				if err != nil {
					return "", err
				}
				argTmp[i] = av
			}
			b.WriteString(fmt.Sprintf("  call i32 @%s(i32 %s", fn, h))
			for _, av := range argTmp {
				b.WriteString(", i32 " + av)
			}
			b.WriteString(")\n")
			// A constructor that raises has failed to construct: the exception propagates
			// before the instance is ever used (Gap R.41, ADR 0223).
			g.checkExn(b)
		}
		return h, nil
	}

	if ed, ok := g.externs[fnName]; ok {
		if len(ed.Params) != len(c.Args) {
			panic(fmt.Sprintf("extern function %q: expected %d args, got %d", fnName, len(ed.Params), len(c.Args)))
		}
		argTypes := []string{}
		argRegs := []string{}
		for i, p := range ed.Params {
			if p.Annot != nil && p.Annot.Kind == KindString {
				lit, ok2 := c.Args[i].(*StrLit)
				if !ok2 {
					panic("extern string args must be string literals: " + fnName)
				}
				gn := g.strConst(lit.Value)
				n := len(lit.Value)
				argTypes = append(argTypes, "i8*")
				argRegs = append(argRegs, fmt.Sprintf("getelementptr([%d x i8], [%d x i8]* %s, i32 0, i32 0)", n, n, gn))
			} else {
				v, err := g.value(b, c.Args[i])
				if err != nil {
					return "", err
				}
				argTypes = append(argTypes, "i32")
				argRegs = append(argRegs, v)
			}
		}
		ret := "i32"
		if ed.ReturnAnno != nil && ed.ReturnAnno.Kind == KindString {
			ret = "i8*"
		}
		callArgs := []string{}
		for i := range argTypes {
			callArgs = append(callArgs, argTypes[i]+" "+argRegs[i])
		}
		t := g.newTmp()
		// NOT irSymbol: an `extern fn` is an FFI surface, and its link name is the C name
		// the program asked to bind — `declare i32 @strlen(i8*)` and the call to it must
		// both stay exactly `strlen` (roadmap Gap R.4). Only names the program *defines*
		// are prefixed.
		b.WriteString(fmt.Sprintf("  %s = call %s @%s(%s)\n", t, ret, fnName, strings.Join(callArgs, ", ")))
		return t, nil
	}
	if g.curModName != "" && g.imports != nil {
		if sibling := g.imports.Funcs[g.curModName]; sibling != nil {
			if fd, ok := sibling[fnName]; ok && fd != nil {
				mangle := g.curModName + "$" + fnName
				ret := g.newTmp()
				b.WriteString(fmt.Sprintf("  %s = call i32 @%s(", ret, irSymbol(mangle)))
				for i, arg := range c.Args {
					if i > 0 {
						b.WriteString(", ")
					}
					av, err := g.value(b, arg)
					if err != nil {
						return "", err
					}
					b.WriteString("i32 " + av)
				}
				b.WriteString(")\n")
				return ret, nil
			}
		}
	}
	if g.funcs[fnName] {
		fd := g.fds[fnName]
		if fd == nil {
			return "", fmt.Errorf("codegen: unknown function %q", fnName)
		}
		n := len(fd.Params)
		isFloat := g.floatFuncs[fnName]
		vals := make([]string, n)
		// tagWords is the second word of the arguments the pair crosses with, pre-filled from the same
		// scan the `define` read: a parameter the scan marked always gets its tag word, whether the pair
		// road answered the payload or the ordinary road did, because a `call` and its `define` that
		// disagree about the arity is the module verifier's problem (roadmap L11.1, ADR 0273).
		tagWords := make([]string, n)
		for i := range fd.Params {
			tagWords[i] = g.pairArityTag(fnName, i)
		}
		// pairArg is the pair road in front of the ordinary one; it is filled in below `argVal`, whose
		// refusal it keeps for an argument the pair cannot name (ADR 0273).
		var pairArg func(a Expr, idx int) (string, error)
		// A container literal argument is materialised into the runtime heap and
		// passed by handle; the callee declares the matching parameter as a
		// container (see declareHeapParams / heapargs.go).
		// strArgCheck reports the AOT limitation before emitting IR that LLVM
		// would reject: a string is an `i8*` global in this backend, so passing
		// one where the callee expects an `i32` produces
		// `call i32 @f(i32 @.str1)` — "global variable reference must have
		// pointer type". Naming the parameter keeps the message actionable.
		strArgCheck := func(a Expr, idx int) error {
			if _, isStr := g.stringVal(a); !isStr {
				if _, isFs := a.(*FString); !isFs {
					return nil
				}
			}
			what := "an argument"
			if idx >= 0 && idx < len(fd.Params) {
				what = fmt.Sprintf("parameter %q of %s", fd.Params[idx].Name, fnName)
			}
			return fmt.Errorf("codegen: strings are not supported as function arguments in the AOT backend yet (%s); CPython answers them", what)
		}
		argVal := func(a Expr, idx int) (string, error) {
			if h, ok, err := g.heapArg(b, a); ok || err != nil {
				if err != nil {
					return "", err
				}
				return h, nil
			}
			// A string argument to a string parameter becomes its @str_tab index: the
			// parameter is an i32 slot, and `call i32 @f(i32 @.strN)` is what LLVM rejects
			// (Gap J.5). A forwarded string (already an index) passes straight through.
			if idx >= 0 && g.strParamOf[fnName][idx] {
				if nm, ok := a.(*Name); ok && g.internedVars[nm.Value] {
					return g.value(b, a)
				}
				// An interpolation IS a text: the value road interns its parts and answers the index the
				// parameter's i32 slot holds, so `g(f"x{n}")` reaches the callee as any other text does
				// (roadmap Gap J.5's string parameter, L11.1's interpolation; ADR 0359).
				if _, isFs := a.(*FString); isFs {
					return g.value(b, a)
				}
				if txt, ok := g.stringVal(a); ok {
					g.heapUsed = true
					t := g.newTmp()
					b.WriteString(fmt.Sprintf("  %s = call i32 @rt_str_intern2(i8* %s, i8* %s)\n", t, g.strConst(txt), g.strConst(pyReprString(txt))))
					return t, nil
				}
			}
			if err := strArgCheck(a, idx); err != nil {
				return "", err
			}
			v, err := g.value(b, a)
			if err != nil {
				return "", err
			}
			// A compile-time list global (@.lstN, e.g. a folded comprehension)
			// is not an i32 handle: copy it into the runtime heap so the callee
			// receives a handle it can iterate, measure and index.
			if ln, ok := g.staticLists[v]; ok {
				return g.heapListFrom(b, ln, "")
			}
			return v, nil
		}
		// pairArg is tried before the ordinary road for a marked parameter: the argument's kind may live in
		// an object the caller can read and the callee cannot, and the two words are what carry it over.
		// It is written after `argVal` because the fallback is that road, refusal and all (ADR 0273).
		pairArg = func(a Expr, idx int) (string, error) {
			if p, t, handled, err := g.pairArgWords(b, fnName, idx, a); err != nil {
				return "", err
			} else if handled {
				tagWords[idx] = t
				return p, nil
			}
			// A position the scan considered for the pair and then closed is not a free pass. It was closed
			// because some *other* call site hands that parameter something this pass cannot name —
			// `y = twice(x)` / `z = twice(y)` is exactly that — and a caller holding a double now has no road
			// left that reads its kind: the ordinary road keeps one word per argument and truncates the double
			// to its int half, which is how `print(outer(2.5))` answered `8` for CPython's `10.0` at exit 0
			// (roadmap Gap R.164, ADR 0280). Refusing here is the same judgement pairArgWords already makes for a
			// float handed to a *marked* position: a kind with nowhere to travel.
			if g.pairClosed[fnName] != nil && g.pairClosed[fnName][idx] && g.isFloat(a) {
				return "", fmt.Errorf("codegen: %s is a float handed to parameter %d of %s, a position this pass considered for the (payload, tag) pair and closed because another call site hands it something it cannot name: the ordinary road keeps one word per argument and would truncate the double to its int half, which is a wrong number rather than a refusal (roadmap L11.6, Gap R.164, ADR 0280)", exprSurface(a), idx, fnName)
			}
			// The same judgement, one construct nearer the source: an entry of a dict literal whose kind the
			// object chose is a pair, and the ordinary argument road has one word per argument. The answer's
			// tag would be dropped at the door, which is how `g({"a": "x"}["a"])` prints the `@str_tab`
			// position instead of the word — the Gap R.164 shape with a text where ADR 0280 had a float.
			if dl, key, isLit := literalDictReadOf(a); isLit && !g.dictLiteralFoldAnswers(dl, key) && !g.dictLiteralEntryIsPlainInt(dl, key) {
				return "", literalDictOneWordErr(key)
			}
			return argVal(a, idx)
		}
		// callArgWords is the one place the arity is spelled: every parameter's word, plus the tag word
		// where the scan says the pair crosses.
		callArgWords := func() []string {
			out := make([]string, 0, len(vals)+len(tagWords))
			for i := range vals {
				out = append(out, vals[i])
				if tagWords[i] != "" {
					out = append(out, "i32 "+tagWords[i])
				}
			}
			return out
		}
		// A helper that fills a container it was handed tells the caller what its elements are:
		// `def fill(out, v): out.append(v)` called as `fill(names, "one")` is what makes
		// `print(names[1])` render text instead of the raw string-table index (Gap J.5).
		if fills := g.strFillOf[fnName]; len(fills) > 0 {
			fillPos := 0
			for _, a := range c.Args {
				if kw, ok := a.(*KeywordArg); ok {
					for i, p := range fd.Params {
						if p.Name == kw.Name {
							g.applyStrFill(fnName, i, kw.Value)
						}
					}
					continue
				}
				g.applyStrFill(fnName, fillPos, a)
				fillPos++
			}
		}
		// A call whose answer is a pair is emitted by the pair door alone (pairCallPair): the ordinary
		// numeric road would take the payload as though it were the whole value and print an int box
		// handle as a number, which is the wrong answer this line of work exists to eliminate. The
		// permission is this call's, and it is off while the arguments are lowered, so a nested call of
		// the same shape inside an argument keeps its own honest refusal (roadmap L11.1, ADR 0273).
		if g.pairRetDone[fnName] && !g.pairCallAsked[fnName] {
			return "", fmt.Errorf("codegen: %s hands back the (payload, tag) pair its answer arrived in: print(...), a binding, and the positions that read one number ask the tag, and this position keeps one word for the value, so the tag has nowhere to go (roadmap L11.1, Gap R.146, ADR 0273)", fnName)
		}
		// A call whose answer is a container is lowered by the container door alone: the ordinary road
		// would take the heap handle for the number it is not, which is how `print(f())` printed `0` and
		// `[f(), 9]` printed `[1, 9]` (roadmap Gap R.67, ADR 0333). The permission is this call's alone and
		// is off while the arguments are lowered, so a nested call of the same shape keeps its own refusal
		// — ADR 0273's rule, read again by the door beside the pair road's.
		if _, isContainerAnswer := g.containerAnswerOf(fnName); isContainerAnswer && !g.containerAnswerIsAsked(fnName) && g.containerAnswerCallIsCarriable(c) {
			return "", containerAnswerRefusal(fnName)
		}
		// The same refusal for a container the scan proved and the slot table declined. Without this the
		// fall-through returns the word the allocator handed back, and `print(f())` printed `0` for a
		// function that returns `[5]` — the module built, the program ran, exit 0, and the number on the
		// screen was a heap slot (roadmap Gap R.261, ADR 0333's scan read a second time).
		if g.containerAnswerIsUndescribed(fnName) && !g.containerAnswerIsAsked(fnName) {
			return "", containerAnswerUndescribedRefusal(fnName)
		}
		asked := g.pairCallAsked
		g.pairCallAsked = map[string]bool{}
		provided := make([]bool, n)
		pos := 0
		seenKw := false
		for _, a := range c.Args {
			if kw, ok := a.(*KeywordArg); ok {
				seenKw = true
				idx := -1
				for i, p := range fd.Params {
					if p.Name == kw.Name {
						idx = i
						break
					}
				}
				if idx < 0 {
					return "", fmt.Errorf("codegen: unknown keyword argument %q for %s", kw.Name, fnName)
				}
				if provided[idx] {
					return "", fmt.Errorf("codegen: multiple values for argument %q of %s", kw.Name, fnName)
				}
				if isFloat {
					fv := g.floatValue(b, kw.Value)
					vals[idx] = "double " + fv
				} else {
					av, err := pairArg(kw.Value, idx)
					if err != nil {
						return "", err
					}
					vals[idx] = "i32 " + av
				}
				provided[idx] = true
				continue
			}
			if seenKw {
				return "", fmt.Errorf("codegen: positional argument after keyword argument for %s", fnName)
			}
			if pos >= n {
				return "", fmt.Errorf("codegen: too many arguments for %s", callableSurfaceName(fnName))
			}
			if provided[pos] {
				return "", fmt.Errorf("codegen: multiple values for argument %q of %s", fd.Params[pos].Name, fnName)
			}
			if isFloat {
				fv := g.floatValue(b, a)
				vals[pos] = "double " + fv
			} else {
				av, err := pairArg(a, pos)
				if err != nil {
					return "", err
				}
				vals[pos] = "i32 " + av
			}
			provided[pos] = true
			pos++
		}
		// fill defaults for params not supplied
		for i := range fd.Params {
			if provided[i] {
				continue
			}
			if fd.Params[i].Default == nil {
				return "", fmt.Errorf("codegen: missing argument %q for %s", fd.Params[i].Name, fnName)
			}
			if isFloat {
				fv := g.floatValue(b, fd.Params[i].Default)
				vals[i] = "double " + fv
			} else {
				dv, err := pairArg(fd.Params[i].Default, i)
				if err != nil {
					return "", err
				}
				vals[i] = "i32 " + dv
			}
		}
		g.pairCallAsked = asked
		t := g.newTmp()
		if _, ok := g.closures[fnName]; ok {
			env := g.newTmp()
			b.WriteString(fmt.Sprintf("  %s = load i32, i32* @%s_slot\n", env, fnName))
			callArgs := append([]string{"i32 " + env}, callArgWords()...)
			b.WriteString(fmt.Sprintf("  %s = call i32 @%s_env(%s)\n", t, fnName, strings.Join(callArgs, ", ")))
			g.checkExn(b)
		} else if g.decorated[fnName] {
			// The decorated body is emitted through funcDef, which defines program-owned
			// names under irSymbolPrefix, so the reference must carry it too (Gap R.4).
			b.WriteString(fmt.Sprintf("  %s = call i32 @%s_impl(%s)\n", t, irSymbol(fnName), strings.Join(callArgWords(), ", ")))
			g.checkExn(b)
		} else {
			if isFloat {
				b.WriteString(fmt.Sprintf("  %s = call double @%s(%s)\n", t, irSymbol(fnName), strings.Join(callArgWords(), ", ")))
				if g.floatTemps == nil {
					g.floatTemps = map[string]bool{}
				}
				g.floatTemps[t] = true
			} else {
				// FFI: call to an extern (C) function — marshal values to native types.
				if ed, ok := g.externs[fnName]; ok {
					if len(ed.Params) != len(c.Args) {
						panic(fmt.Sprintf("extern function %q: expected %d args, got %d", fnName, len(ed.Params), len(c.Args)))
					}
					argTypes := []string{}
					argRegs := []string{}
					for i, p := range ed.Params {
						if p.Annot != nil && p.Annot.Kind == KindString {
							lit, ok2 := c.Args[i].(*StrLit)
							if !ok2 {
								panic("extern string args must be string literals: " + fnName)
							}
							gn := g.strConst(lit.Value) // ensure global @.strN exists
							n := len(lit.Value)
							argTypes = append(argTypes, "i8*")
							argRegs = append(argRegs, fmt.Sprintf("i8* getelementptr([%d x i8], [%d x i8]* %s, i32 0, i32 0)", n, n, gn))
						} else {
							v, err := g.value(b, c.Args[i])
							if err != nil {
								return "", err
							}
							argTypes = append(argTypes, "i32")
							argRegs = append(argRegs, "i32 "+v)
						}
					}
					ret := "i32"
					if ed.ReturnAnno != nil && ed.ReturnAnno.Kind == KindString {
						ret = "i8*"
					}
					callArgs := []string{}
					for i := range argTypes {
						callArgs = append(callArgs, argTypes[i]+" "+argRegs[i])
					}
					b.WriteString(fmt.Sprintf("  %s = call %s @%s(%s)\n", t, ret, irSymbol(fnName), strings.Join(callArgs, ", ")))
					return t, nil
				}
				b.WriteString(fmt.Sprintf("  %s = call i32 @%s(%s)\n", t, irSymbol(fnName), strings.Join(callArgWords(), ", ")))
			}
			g.checkExn(b)
			// a generator function returns a runtime heap list handle
			if g.genFuncs[fnName] {
				g.listOperands[t] = true
			}
		}
		return t, nil
	}
	switch fnName {
	case "print", "printf":
		// print(*args, sep=" ", end="\n") — Python's separator/terminator
		// semantics, identical on both backends (ADR 0165). An argument is
		// written WITHOUT its own terminator; `sep` is emitted between arguments
		// and `end` after the last one. So print("a =", n) is one line, print()
		// is a blank line, and print(x, end="") keeps the line open.
		sep, end := " ", "\n"
		var last string
		// emitText writes a literal string (the separator / terminator). `%` is
		// doubled so printf treats it as text, not as a directive.
		emitText := func(text string) {
			name, size := g.fmtStr(strings.ReplaceAll(text, "%", "%%"))
			t := g.newTmp()
			b.WriteString(fmt.Sprintf("  %s = call i32 (i8*, ...) @printf(i8* getelementptr inbounds ([%d x i8], [%d x i8]* %s, i32 0, i32 0))\n", t, size, size, name))
			last = t
		}
		args := make([]Expr, 0, len(c.Args))
		for _, a := range c.Args {
			kw, isKw := a.(*KeywordArg)
			if !isKw {
				args = append(args, a)
				continue
			}
			sl, isStr := kw.Value.(*StrLit)
			if !isStr {
				return "", fmt.Errorf("codegen: print's %s must be a compile-time string constant (CPython accepts any expression)", kw.Name)
			}
			switch kw.Name {
			case "sep":
				sep = sl.Value
			case "end":
				end = sl.Value
			default:
				return "", fmt.Errorf("codegen: print got an unexpected keyword argument %q", kw.Name)
			}
		}
		if len(args) < 1 {
			// print() writes just the terminator: a blank line, like Python.
			emitText(end)
			return last, nil
		}
		// Gap L.5: the line is written after the arguments, not during them. Each argument's
		// lowering is recorded so the statement's writes can be threaded past the whole argument
		// list — a call that prints is an *evaluation*, and the caller's own bytes must not be on
		// the stream before it runs (roadmap Gap L.5, ADR 0355; `pkg/lang/print_atomic.go`).
		argMarks := make([]int, 0, len(args)+1)
		for i, a := range args {
			// The separator is written *after* the argument it follows, so it belongs to that
			// argument's chunk and not to the one coming next: the mark is taken once the
			// separator is on the stream, which is what keeps a chunk's writes behind its own
			// argument's evaluation and the line's writes in order.
			if i > 0 {
				emitText(sep)
			}
			argMarks = append(argMarks, b.Len())
			// `print(a or b)` prints an **operand**, and the operand it prints is the one the test chooses —
			// a fact about a value, not about a verdict, so the printer is asked of the operand rather than
			// of the operator (ADR 0261's rule, roadmap Gap R.147, ADR 0269). Two roads: when the source
			// wrote the test, the chosen operand is the whole expression and every arm below gets to render
			// it as it would render that operand alone; otherwise the answer is one operand or the other and
			// only the run time knows which, so the pair is selected and given to the one printer in the
			// module that reads a tag. A shape neither road can state is refused, never answered with the 1
			// that used to come out here (ADR 0166).
			if ln, isLogic := a.(*BinOp); isLogic && (ln.Op == "and" || ln.Op == "or") {
				if chosen, decided := constantLogicArm(ln); decided {
					a = chosen
				} else if g.logicAnswerIsFloat(ln) {
					// Both operands answer with a double, so the answer does too, and the chain's float arm
					// below renders it from the double door (`select i1 …, double …`) exactly as it renders
					// any other float expression. No box, no tag, no second formatter.
				} else {
					payload, tag, handled, lerr := g.logicPrintPair(b, ln)
					if lerr != nil {
						return "", lerr
					}
					if !handled {
						return "", logicWordErr(ln)
					}
					fmt.Fprintf(b, "  call void @rt_print_mixed_value(i32 %s, i32 %s, i32 0)\n", payload, tag)
					continue
				}
			}
			if nm, ok := a.(*Name); ok && g.unionVars[nm.Value] {
				g.emitUnionPrint(b, nm.Value, "")
				continue
			}
			// A subscript whose base is a call that hands back a container is printed by the roads below,
			// which read a container *variable* — and the answer is one: the callee materialised the object at
			// its `ret` and the answer reaches here under a rooted name carrying the notebooks those roads
			// consult (the dict's key/value kind, the list's element kind, the set's member kind, the mixed
			// reads and the tagged read beside them). Rewriting the base to that name **before any of them is
			// asked** is what makes `print(build()[i])` and `ys = build(); print(ys[i])` one program rather than
			// an answer and a wrong number: the kind roads have to run first, because a dict built from a
			// literal carries no per-slot tags and the tagged read below would hand back its @str_tab index and
			// call it a number (roadmap Gap R.214, ADR 0334; the return half is ADR 0333).
			if redo, okAns := g.containerAnswerSubscript(b, a); okAns {
				a = redo
			}
			// print a constant string: literals and folded string-method results. This is a
			// context that wants the BYTES, so it takes the global pointer from strConst
			// directly -- value() now hands back an @str_tab index, which is the language's
			// string value and not something printf's %s may read (Gap R.42, ADR 0224).
			if txt, ok := g.stringVal(a); ok {
				fmtName, size := g.fmtStr("%s")
				v := g.strConst(txt)
				t := g.newTmp()
				b.WriteString(fmt.Sprintf("  %s = call i32 (i8*, ...) @printf(i8* getelementptr inbounds ([%d x i8], [%d x i8]* %s, i32 0, i32 0), i8* %s)\n", t, size, size, fmtName, v))
				last = t
				continue
			}
			// an f-string: print each constant part as a string and each
			// interpolated expression as its value (no runtime string type).
			// All parts are combined into a single printf call so the output
			// matches the interpreter's one-string Repr.
			if fs, ok := a.(*FString); ok {
				// Two fields the one printf below cannot state: the void, which has no digits, and a
				// container, whose text is its slots rather than its handle. Both used to answer a number
				// (`f"v={None}"` wrote `v=0`, `f"{xs}"` wrote `0` for `[1, 2]`) because the format string's
				// only words are an i32 and an i8*. Such an interpolation is built by the VALUE road — the
				// same door a binding, a concatenation and `str()` use — and handed to the module's string
				// printer, so the print of a program and the same program with the text named are one answer
				// (roadmap L11.1, ADR 0359).
				if g.fstringNeedsRenderer(fs) {
					if err := g.emitFStringPrint(b, fs); err != nil {
						return "", err
					}
					continue
				}
				var fmtLit string
				var operands []string
				for _, part := range fs.Parts {
					if part.Lit != "" {
						// escape % as %% so printf shows a literal %
						fmtLit += strings.ReplaceAll(part.Lit, "%", "%%")
						continue
					}
					// A name the pair road bound — from a slot read, a loop variable, an arithmetic
					// answer or a call — contributes the digits the module's ONE tag-reading printer
					// writes for its (payload, tag) pair, which is the same helper `str()` calls: the
					// field, `str(n)` and `print(n)` cannot disagree about `7` versus `7.0`, and a text
					// slot contributes its text rather than its interned index. A field that asked for
					// `!r` asks the same door with the quote flag on, which is what `repr()` does — so
					// `f"{n!r}"` writes `'a'` where it used to write nothing at all (roadmap L11.1,
					// Gap R.146's rendering positions; ADR 0303's one door, ADR 0187's pair). A field
					// that asked for a FORMAT SPEC still goes to the spec engine below: this door renders
					// a value, it does not pad one.
					if part.Spec == "" {
						if fp, ft, isPair, perr := g.filedPairOf(b, part.Expr); perr != nil {
							return "", perr
						} else if isPair {
							quote := "0"
							if part.Conv == ConvRepr {
								quote = "1"
							}
							fmtLit += "%s"
							sv := g.newTmp()
							b.WriteString(fmt.Sprintf("  %s = call i32 @rt_str_of_value(i32 %s, i32 %s, i32 %s)\n", sv, fp, ft, quote))
							sp := g.newTmp()
							b.WriteString(fmt.Sprintf("  %s = call i8* @rt_str_ptr(i32 %s)\n", sp, sv))
							operands = append(operands, "i8* "+sp)
							continue
						}
					}
					// A field that ASKED for a format is answered by the shared spec engine, at
					// compile time where the value is a constant and by refusing where it is not.
					// Falling through to the plain %d/%s below is the defect this row exists to end:
					// `f"{3.5:.2f}"` printed `3.5` here and on the interpreter at exit 0, and the two
					// engines agreeing is exactly why parity never saw it (Gap R.186, ADR 0299).
					// A TEXT field asked for `!r` is not folded into the format string: quoting is the
					// printer's job, and the door below asks it with the quote flag on. Embedding the quoted
					// literal in `@.fmtN` is what made `print(f"{'a'!r}")` spend the contract's forbidden exit 2
					// — `@.fmt1` came out `[0 x i8]` against a `[4 x i8]` use, which `llc` rejects (roadmap
					// Gap R.192; ADR 0166's rule, ADR 0303's one door).
					printerQuotes := part.Spec == "" && part.Conv == ConvRepr && g.fieldWantsPrinterQuotes(part.Expr)
					if !printerQuotes && (part.Spec != "" || (part.Conv != ConvNone && fieldIsConstantLiteral(part.Expr))) {
						lit, ok := g.formatFieldConst(part)
						if !ok {
							return "", fmt.Errorf("codegen: the format spec %q needs a value this backend "+
								"can read at compile time (AOT backend)", part.Spec)
						}
						fmtLit += strings.ReplaceAll(lit, "%", "%%")
						continue
					}
					if part.Expr != nil {
						// A name the pair road bound — from a slot read, a loop variable, an arithmetic
						// answer or a call — contributes the digits the module's ONE tag-reading printer
						// writes for its (payload, tag) pair, which is the same helper `str()` calls: the
						// field, `str(n)` and `print(n)` cannot disagree about `14` versus `14.0`, and a
						// text slot contributes its text rather than its interned index (roadmap L11.1,
						// Gap R.146's rendering positions; ADR 0303's one door, ADR 0187's pair).
						if exprIsContainerShape(part.Expr) {
							// A container interpolated into an f-string is a heap object, and this road
							// builds ONE printf format string: the only words it can hand printf are an
							// i32 or an i8*, neither of which renders `[1, 2]`. Emitting the handle as
							// `%d` is what produced `printf(..., i32 @.lst1)` -- a global address where a
							// heap handle belongs, which llc rejects as an invalid module and ADR 0166
							// forbids outright. Rendering it needs rt_print_list_mixed's sink, which
							// cannot compose into a single printf; so the field is REFUSED here, and the
							// interpreter -- which has no such limit -- answers [1, 2] like the
							// reference (Gap R.186 / Gap R.60's remainder, ADR 0299, owed to L11.1).
							return "", fmt.Errorf("codegen: an f-string field that is a container has no word to " +
								"travel in beside the text around it (AOT backend)")
						} else if g.isFloat(part.Expr) {
							// %s of rt_fmt_double's text, not %.17g: Python's str(0.1) is
							// "0.1" and str(2.0) is "2.0".
							fmtLit += "%s"
							fv := g.floatValue(b, part.Expr)
							if fv == "" {
								return "", g.floatOperandRefusal(part.Expr)
							}
							g.floatFmtUsed = true
							fs := g.newTmp()
							b.WriteString(fmt.Sprintf("  %s = call i8* @rt_fmt_double(double %s)\n", fs, fv))
							operands = append(operands, "i8* "+fs)
						} else if _, knownStr := g.stringVal(part.Expr); knownStr || g.printsAsInternedStr(part.Expr) {
							// An interpolated string is an @str_tab index now, so printf must be
							// handed the bytes through rt_str_ptr -- `%d` printed the index and
							// `f"hi {n}"` came out as `hi 0` (Gap R.42, ADR 0224).
							fmtLit += "%s"
							vv, err := g.value(b, part.Expr)
							if err != nil {
								return "", err
							}
							// `!r` of a text asks the module's one printer with the quote flag on — the same
							// door `repr()` uses — rather than the raw bytes `!s` wants. The conversion used to
							// be answered with an empty string here, at exit 0, for any field that was not a
							// literal (roadmap Gap R.186, Gap R.192; ADR 0303's one door).
							if part.Conv == ConvRepr {
								g.heapUsed = true
								qv := g.newTmp()
								b.WriteString(fmt.Sprintf("  %s = call i32 @rt_str_of_value(i32 %s, i32 %d, i32 1)\n", qv, vv, int(TagStr)))
								vv = qv
							}
							sp := g.newTmp()
							b.WriteString(fmt.Sprintf("  %s = call i8* @rt_str_ptr(i32 %s)\n", sp, vv))
							operands = append(operands, "i8* "+sp)
						} else if g.printsAsBool(part.Expr) {
							g.heapUsed = true // rt_bool_text lives in the heap runtime module
							// An interpolated bool contributes its word: `f"{flag}"` is "True", the
							// text print writes and str() returns, because all three ask one predicate
							// (roadmap L11.1 step 2, ADR 0257).
							fmtLit += "%s"
							pv, perr := g.value(b, part.Expr)
							if perr != nil {
								return "", perr
							}
							bp := g.newTmp()
							b.WriteString(fmt.Sprintf("  %s = call i8* @rt_bool_text(i32 %s)\n", bp, pv))
							operands = append(operands, "i8* "+bp)
						} else {
							fmtLit += "%d"
							vv, err := g.value(b, part.Expr)
							if err != nil {
								return "", err
							}
							operands = append(operands, "i32 "+vv)
						}
					}
				}
				fmtName, size := g.fmtStr(fmtLit)
				t := g.newTmp()
				callArgs := fmt.Sprintf("i8* getelementptr inbounds ([%d x i8], [%d x i8]* %s, i32 0, i32 0)", size, size, fmtName)
				if len(operands) > 0 {
					callArgs += ", " + strings.Join(operands, ", ")
				}
				b.WriteString(fmt.Sprintf("  %s = call i32 (i8*, ...) @printf(%s)\n", t, callArgs))
				last = t
				continue
			}
			if nm, ok := a.(*Name); ok {
				if g.runtimeDicts[nm.Value] {
					g.heapSeq++
					hs := g.heapSeq
					b.WriteString(fmt.Sprintf("  %%h%d = load i32, i32* %%_%s\n", hs, nm.Value))
					if g.dictKeyStr[nm.Value] || g.dictValStr[nm.Value] {
						// keys and/or values are @str_tab indices: the flagged printer renders
						// {'a': 1} / {1: 'a'} exactly as the interpreter's Repr does (Gap I.2).
						ks, vs := "0", "0"
						if g.dictKeyStr[nm.Value] {
							ks = "1"
						}
						if g.dictValStr[nm.Value] {
							vs = "1"
						}
						b.WriteString(fmt.Sprintf("  call void @rt_dict_print_s(i32 %%h%d, i32 0, i32 %s, i32 %s)\n", hs, ks, vs))
					} else {
						b.WriteString(fmt.Sprintf("  call void @rt_dict_print(i32 %%h%d, i32 0)\n", hs))
					}
					continue
				}
				if g.runtimeSets[nm.Value] {
					g.heapSeq++
					hs := g.heapSeq
					b.WriteString(fmt.Sprintf("  %%h%d = load i32, i32* %%_%s\n", hs, nm.Value))
					printer := "rt_set_print"
					if g.setElemStr[nm.Value] {
						printer = "rt_set_print_str"
					}
					b.WriteString(fmt.Sprintf("  call void @%s(i32 %%h%d, i32 0)\n", printer, hs))
					continue
				}
			}
			// None prints as "None" in both backends. The decision is static, like every
			// other print format here: values are untagged i32s, so codegen — not the
			// runtime — knows which expressions are the None singleton (ADR 0172).
			if g.isNoneExpr(a) {
				g.heapUsed = true // the None printer lives in the heap runtime module
				// Evaluate the argument first: `print(emit())` must run emit() (and any
				// output it produces) before writing None, exactly as the interpreter
				// interleaves them. Skipping the call is the classic way to lose output.
				if _, isLit := a.(*NoneLit); !isLit {
					if _, err := g.value(b, a); err != nil {
						return "", err
					}
				}
				b.WriteString("  call void @rt_print_none(i32 0)\n")
				continue
			}
			if g.printsAsInternedStr(a) {
				g.heapUsed = true
				v, err := g.value(b, a)
				if err != nil {
					return "", err
				}
				fmt.Fprintf(b, "  call void @rt_print_str(i32 %s, i32 0)\n", v)
				continue
			}
			if sl, ok := a.(*Slice); ok {
				if nm2, ok2 := sl.Obj.(*Name); ok2 {
					if _, isList := g.listVars[nm2.Value]; isList {
						h, err := g.value(b, sl)
						if err != nil {
							return "", err
						}
						// The sliced object is a copy of the base's slots WITH their tags (`rt_slice`
						// pairs them, ADR 0187's rule as ADR 0294 applied it), so the printer has to be the
						// one that reads a tag. Asking only the base's element notebook printed a text slot as
						// its @str_tab index and a None as its zero — `print(xs[0:2])` answered `[1, 0]` for
						// `[1, "a"]` while `print(xs)` printed `[1, 'a']`, because the name arm below asks
						// `mixedLists` first and this arm did not (roadmap Gap R.230, ADR 0335).
						printer := "rt_print_list"
						if g.mixedLists[nm2.Value] {
							printer = "rt_print_list_mixed"
						} else if g.listElemStr[nm2.Value] {
							printer = "rt_print_list_str"
						}
						if g.isTupleExpr(sl.Obj) {
							// The copy is a fresh object and it carries the attribute word of the object it
							// was cut from, so the parentheses come from the value and not from the road that
							// copied it: `t[0:1]` prints (1,) because the object says tuple (ADR 0361).
							fmt.Fprintf(b, "  call void @rt_print_list_or_tuple(i32 %s)\n", h)
							g.tupleUsed = true
							continue
						}
						fmt.Fprintf(b, "  call void @%s(i32 %s, i32 0)\n", printer, h)
						continue
					}
				}
			}
			if nm, ok := a.(*Name); ok && g.taggedVars[nm.Value] {
				// print() of a name whose pair the OBJECT decides reads both allocas — and both are
				// unread unless some path wrote them. The written-flag test sat only on the ordinary
				// slot load, so a capture no arm bound printed its zero instead of trapping (Gap R.68).
				g.checkBound(b, nm.Value, nm.Span())
				vt := g.newTmp()
				b.WriteString(fmt.Sprintf("  %s = load i32, i32* %%_%s\n", vt, nm.Value))
				tg := g.newTmp()
				b.WriteString(fmt.Sprintf("  %s = load i32, i32* %%_%s_tag\n", tg, nm.Value))
				b.WriteString(fmt.Sprintf("  call void @rt_print_mixed_value(i32 %s, i32 %s, i32 0)\n", vt, tg))
				continue
			}
			// print(xs[0][0] + 1), print(-xs[0][0]): arithmetic on a slot whose kind lives in the object
			// rather than in the spelling. The ordinary numeric road asks for one i32 with no tag attached
			// and refuses (correctly — a payload without its tag is a number wearing another object's
			// bits); this is the other answer to the same question, asked of the object and printed from
			// the (payload, tag) pair the answer arrives with (roadmap L11.1, ADR 0265). It is taken only
			// where the ordinary road would have refused, so no program that was answered before is now
			// routed through here.
			// print(twice(xs[0][0])) — the answer of a function whose body did the arithmetic over a slot the
			// literal never described. The callee's tag word says what the objects said, and the one printer
			// that takes a value *and* its kind renders it, which is the same door ADR 0265 opened for the
			// expression itself and ADR 0269 for an operand a test chose (roadmap L11.1, ADR 0273).
			if g.instanceExpressionClass(a) != "" {
				// `print(C())` and `print(c)` after `c = C()`: the value is a heap handle and always was,
				// but only the object's slot-0 class id says what to call it. Without this arm the print road
				// treats it as an int and prints the slot (roadmap Gap R.209).
				h, herr := g.value(b, a)
				if herr != nil {
					return "", herr
				}
				g.heapUsed = true
				b.WriteString(fmt.Sprintf("  call void @rt_print_mixed_value(i32 %s, i32 %d, i32 0)\n", h, int(TagInstance)))
				continue
			}
			if printed, perr := g.pairCallPrint(b, a); perr != nil {
				return "", perr
			} else if printed {
				continue
			}
			// print(min(n, 3)), print(sum([n, 1])): the answer of a fold whose winner the run time chose.
			// The fold returns one of the values it was handed, so the answer's kind is the winner's own
			// kind and the ordinary road — which compares lifted doubles and returns one word — cannot
			// carry it (roadmap L11.1, Gap R.146; ADR 0316).
			if printed, perr := g.pairFoldPrint(b, a); perr != nil {
				return "", perr
			} else if printed {
				continue
			}
			if g.arithWouldRefuse(a) {
				if val, tg, okPair, perr := g.taggedArithPair(b, a); perr != nil {
					return "", perr
				} else if okPair {
					g.heapUsed = true
					g.floatFmtUsed = true
					b.WriteString(fmt.Sprintf("  call void @rt_print_mixed_value(i32 %s, i32 %s, i32 0)\n", val, tg))
					continue
				}
			}
			if ix, ok := a.(*Index); ok {
				// The subscript's five pair arms — a dict literal, a list literal, an element of a mixed
				// list, an entry of a mixed dict, a slot of a container that is itself a read — are one
				// door, in one order, asked by this road and by the f-string field and by nothing else, so
				// `print(x)` and `f"{x}"` of one read cannot answer differently. The arms, their order and
				// the runtime flags they set live in `subscript_pair.go` (roadmap Gap R.283, where the
				// field asked NONE of them and printed a payload; ADR 0187's pair, ADR 0232's one question).
				if val, tag, okPair, perr := g.subscriptPairOf(b, ix); perr != nil {
					return "", perr
				} else if okPair {
					b.WriteString(fmt.Sprintf("  call void @rt_print_mixed_value(i32 %s, i32 %s, i32 0)\n", val, tag))
					continue
				}
			}
			// print([f(x) for x in xs]) — a comprehension is a container, so the runtime
			// printer renders it. The constant path used to printf the folded global
			// (@.lst1), which llc rejects outright: "global variable reference must have
			// pointer type" (roadmap L11.7, ADR 0192 — ADR 0188's rule one construct later).
			// The printer is the one for the container the comprehension means: a set comp is
			// rendered `{1, 2}` and a dict comp `{1: 2}`, not `[1, 2]`, which is what made
			// `print({x for x in [3, 1, 2]})` a question about the comprehension's kind and not
			// only about its elements (roadmap Gap J.2, ADR 0234).
			// print(t) of the immutable container: the brackets belong to the object, so the print road
			// hands the handle to the container dispatcher and lets the object answer — the same
			// arrangement that made `print(xs)` of a built container honest (ADR 0251), asked of a
			// spelling the list roads were never written for (roadmap L11.3, ADR 0361).
			if g.isTupleExpr(a) && !g.printsAsInternedStr(a) {
				// containerOperand, not g.value: a tuple of constants folds to a compile-time global
				// (@.lstN), and handing the address of that global to the printer is the exit-2 shape ADR 0188
				// removed for lists. The operand has to be the heap object the materialiser built, because its
				// own bits are what answer the bracket question (roadmap L11.3, ADR 0361).
				h, herr := g.containerOperand(b, a)
				if herr != nil {
					return "", herr
				}
				b.WriteString(fmt.Sprintf("  call void @rt_print_container_value(i32 %s, i32 0)\n", h))
				continue
			}
			// A slice of a container lowers to `rt_slice`, whose answer is a heap HANDLE; printf's %d
			// printed that handle as `1` where the reference prints `[2, 3]`. Ask before the numeric
			// road does, the same way the comprehension and sorted() arms beside this do (Gap R.179).
			if sl, ok := a.(*Slice); ok && g.isContainerExpr(sl.Obj) && !g.printsAsInternedStr(a) {
				// Lower the slice itself — `containerOperand` answers "not a container" for a *Slice,
				// and the answer here is the handle `rt_slice` returns.
				h, herr := g.value(b, a)
				if herr != nil {
					return "", herr
				}
				if g.isTupleExpr(sl.Obj) {
					b.WriteString(fmt.Sprintf("  call void @rt_print_container_value(i32 %s, i32 0)\n", h))
					g.tupleUsed = true
					continue
				}
				b.WriteString(fmt.Sprintf("  call void @rt_print_list_mixed(i32 %s, i32 0)\n", h))
				continue
			}
			// A DICT VIEW (`keys()`, `values()`) lowers through the call road to a heap LIST of the
			// dict's keys or values — and print never learned, so printf's %d printed the handle: `0`
			// where the reference prints `dict_keys(['a'])` (roadmap Gap R.182, ADR 0291's arrangement).
			// The reference's wrapper word is a separate row; the wrong number goes first.
			if cv, word, isView := g.dictViewElems(a); isView {
				// containerOperand, not g.value: a view over an INT-valued dict folds to a literal that
				// g.value renders as the ADDRESS of a compile-time global, and feeding that to
				// rt_print_list_mixed is the exit-2 shape ADR 0188 removed for literals.
				h, verr := g.containerOperand(b, &ListLit{Elems: cv})
				if verr != nil {
					return "", verr
				}
				// The reference names the WRAPPER in the rendering — `dict_keys(['a'])`, not `['a']` —
				// so the compiled leg prints the word, the list, and the closing paren (Gap R.182).
				b.WriteString(fmt.Sprintf("  call void @rt_out_txt(i8* getelementptr inbounds ([%d x i8], [%d x i8]* @.%s, i32 0, i32 0))\n", len(word)+1, len(word)+1, "fmtdict"+word))
				b.WriteString(fmt.Sprintf("  call void @rt_print_list_mixed(i32 %s, i32 0)\n", h))
				b.WriteString("  call void @rt_out_txt(i8* getelementptr inbounds ([2 x i8], [2 x i8]* @.fmtdictclose, i32 0, i32 0))\n")
				continue
			}
			// print(build()) — a call whose answer is a container. The answer is a heap handle and its kind
			// is in the scan's table, so the operand is lowered to the name that holds it and printed by the
			// printer that name would be printed by: `ys = build()` / `print(ys)` has printed `[1, 2]` all
			// along, and `print(build())` printing `0` was the same object asked a second question and given
			// the first answer it had (roadmap Gap R.67, ADR 0333).
			if ansName, ansKind, okAns := g.containerAnswerOperandRead(b, a, containerReadWhole); okAns {
				h, herr := g.value(b, ansName)
				if herr != nil {
					return "", herr
				}
				g.heapUsed = true
				// Which door prints the answer is asked of the answer, and the tuple is the answer that has to be
				// asked of the OBJECT: a callee that handed back a tuple wrote the parentheses and the caller never
				// saw them, so no road-level fact can name the brackets — the attribute word beside the slots can.
				// A described list, set or dict keeps the printer its kind names (roadmap Gap R.67's arrangement,
				// read again for the immutable container at L11.3, ADR 0361).
				ansNameStr := ""
				if nm, isName := ansName.(*Name); isName {
					ansNameStr = nm.Value
				}
				if ansKind == tupleAnswer || g.tupleVars[ansNameStr] {
					b.WriteString(fmt.Sprintf("  call void @rt_print_container_value(i32 %s, i32 0)\n", h))
					g.tupleUsed = true
				} else {
					b.WriteString(fmt.Sprintf("  call void @%s(i32 %s, i32 0)\n", g.answerPrinter(ansNameStr, ansKind), h))
				}
				continue
			}
			if comp, ok := a.(*Comp); ok {
				h, cerr := g.containerOperand(b, a)
				if cerr != nil {
					return "", cerr
				}
				printer := "rt_print_list_mixed"
				switch comp.Kind {
				case CompSet:
					printer = "rt_set_print"
				case CompDict:
					printer = "rt_dict_print"
				}
				b.WriteString(fmt.Sprintf("  call void @%s(i32 %s, i32 0)\n", printer, h))
				continue
			}
			// print(sorted(xs)) — that lowering hands back a runtime list handle, and only
			// the runtime printer can render it: the static path would printf the handle,
			// which is the invalid-IR shape ADR 0188 removed for literals (ADR 0191).
			if call, ok := a.(*Call); ok {
				// reversed("abc") is a string reversal and takes its own path; only a
				// container argument means this lowering produced a heap handle.
				if cname, ok := call.Fn.(*Name); ok && (cname.Value == "sorted" || cname.Value == "reversed") && g.isContainerExpr(call.Args[0]) {
					h, cerr := g.sortedRuntime(b, call)
					if cerr != nil {
						return "", cerr
					}
					// The two orderings answer a LIST whatever their argument was — `sorted((3, 1))` is `[1, 3]`, with
					// brackets, in the reference — so the printer is the one the compiler knows, not the one the
					// object would be asked: the copy `sortedRuntime` builds carries no attribute word of its own
					// (roadmap Gap R.67's arrangement, and ADR 0361's rule that a road asks what it can see).
					b.WriteString(fmt.Sprintf("  call void @rt_print_list_mixed(i32 %s, i32 0)\n", h))
					continue
				}
			}
			if nm, ok := a.(*Name); ok && g.listVars[nm.Value] {
				g.heapSeq++
				hs := g.heapSeq
				b.WriteString(fmt.Sprintf("  %%h%d = load i32, i32* %%_%s\n", hs, nm.Value))
				printer := "rt_print_list"
				if g.listElemStr[nm.Value] {
					printer = "rt_print_list_str"
				}
				if g.mixedLists[nm.Value] {
					printer = "rt_print_list_mixed"
				}
				b.WriteString(fmt.Sprintf("  call void @%s(i32 %%h%d, i32 0)\n", printer, hs))
				continue
			}
			// A loop variable unrolled from an inline list literal carries the tag its element was
			// built with. Printing the i32 alone showed a float box's handle and None's zero where
			// Python shows 1.5 and None; the tag travels with the element, and rt_print_mixed_value
			// is the one place that knows how to read it. Only the tags nothing else can render
			// take this branch — an integer and an interned string already have their answers from the
			// paths above, and routing them here too dragged the string runtime into a module for a
			// loop over plain numbers. A container element is a handle, and printing it without its
			// tag is how `for row in [[1, 2], [3, 4]]` printed 0 and 1 (roadmap L11.1, ADR 0233). A
			// bool joins them for the same reason a float does: the unrolled loop forgot the verdict
			// when it bound the name, so only the element's tag can still say True (Gap R.112).
			if nm, ok := a.(*Name); ok && g.loopElemTag != nil && !g.listVars[nm.Value] && !g.taggedVars[nm.Value] {
				if tg, has := g.loopElemTag[nm.Value]; has && (tg == int32(TagFloat) || tg == int32(TagNone) ||
					tg == int32(TagBool) || tg == int32(TagList) || tg == int32(TagDict) || tg == int32(TagSet)) {
					vv := g.newTmp()
					b.WriteString(fmt.Sprintf("  %s = load i32, i32* %%_%s\n", vv, nm.Value))
					b.WriteString(fmt.Sprintf("  call void @rt_print_mixed_value(i32 %s, i32 %d, i32 0)\n", vv, tg))
					if tg == int32(TagFloat) {
						g.floatFmtUsed = true
					}
					continue
				}
			}
			// A bool prints as True/False. The value is the 0/1 the comparison produced — an
			// untagged word, because giving bools their own tagged word is the L11.1 destination and
			// not this rung — so the question 「is this expression a bool?」 is answered from the AST,
			// where it was always answerable. It takes this branch only after every container, tag
			// and text arm above has declined, so what it intercepts is exactly what used to reach
			// printf("%d") and answer 1 (roadmap L11.1 step 2, ADR 0257).
			if g.printsAsBool(a) {
				// The bool printer lives in the heap runtime module, as the None printer does, and
				// that module drags in the root table it reads kinds from — so a program that prints
				// a verdict and allocates nothing else has to say the heap runtime is in use, or llc
				// reports `use of undefined value '@gc.kinds'` on a two-line program (ADR 0209's
				// rule about gates, roadmap L11.1 step 2, ADR 0257).
				g.heapUsed = true
				v, err := g.value(b, a)
				if err != nil {
					return "", err
				}
				b.WriteString(fmt.Sprintf("  call void @rt_print_bool(i32 %s, i32 0)\n", v))
				continue
			}
			var t string
			if g.isFloat(a) {
				// Python renders a float as its shortest round-tripping text with a
				// ".0" when integral (rt_fmt_double); printf's %.17g invented digits
				// and %g truncated them, and neither marked 2.0 as a float.
				fmtName, size := g.fmtStr("%s")
				fv := g.floatValue(b, a)
				if fv == "" {
					// An empty operand here would reach printf as `rt_fmt_double(double )` and
					// llc would reject the module: refuse it at the front end instead (ADR 0166).
					return "", g.floatOperandRefusal(a)
				}
				g.floatFmtUsed = true
				fs := g.newTmp()
				b.WriteString(fmt.Sprintf("  %s = call i8* @rt_fmt_double(double %s)\n", fs, fv))
				t = g.newTmp()
				b.WriteString(fmt.Sprintf("  %s = call i32 (i8*, ...) @printf(i8* getelementptr inbounds ([%d x i8], [%d x i8]* %s, i32 0, i32 0), i8* %s)\n", t, size, size, fmtName, fs))
			} else {
				// A container literal in print position is a *rendering* question, not a storage
				// question: build the runtime object and let the runtime printer render it. The old
				// gate was literalNeedsHeap — "does it contain a string?" — so an all-int literal
				// fell through to the static path and printed the elements-array global as a number:
				// `print([1, 2])` emitted `printf("%d\n", i32 @.lst1)`, which llc rejects outright
				// ("global variable reference must have pointer type"), and `print(set())` answered
				// `0`, the handle (roadmap Gap J.6, item (3)).
				if lit, ok := emptyContainerLiteral(a); ok {
					// `set()` / `list()` / `dict()` are containers the same way a literal is, and the
					// empty set has no literal spelling at all — so the constructor has to reach the
					// printer too, not just the literal (Gap K.3).
					a = lit
				}
				// print(xs[i]) with both parts literal folds to the element itself, and this branch
				// has to see that element: a folded container element that reaches the number path
				// emits printf("%d", i32 @.lst1), which llc refuses outright — the compiler rejecting
				// its own module for an ordinary program (roadmap L11.1, ADR 0166).
				if ix, isIx := a.(*Index); isIx {
					if base, isLit := ix.Obj.(*ListLit); isLit {
						if k, isConst := g.foldConstInt(ix.Idx); isConst && k >= 0 && k < int64(len(base.Elems)) {
							a = base.Elems[k]
						}
					}
				}
				if isContainerLiteral(a) {
					if _, isLL := a.(*ListLit); isLL && (literalMixedKinds(a) || g.literalNeedsTags(a)) && g.taggableMixedList(a.(*ListLit)) {
						// Heterogeneous list of taggable elements: build it with per-element
						// tags and print through the tag-aware printer, which is what the
						// interpreter's Repr does element by element (ADR 0184).
						g.heapUsed = true
						h, err := g.heapListFromTagged(b, a.(*ListLit))
						if err != nil {
							return "", err
						}
						b.WriteString(fmt.Sprintf("  call void @rt_print_list_mixed(i32 %s, i32 0)\n", h))
						continue
					}
					// A literal that mixes kinds is printed through the same dispatching
					// printers its variable-bound twin uses, whenever every slot can say what
					// it holds (ADR 0232); only a slot that cannot is the refusal.
					if (literalMixedKinds(a) || g.literalNeedsTags(a)) && !g.literalMixedIsTaggable(a) {
						noun := "list"
						switch a.(type) {
						case *SetLit:
							noun = "set"
						case *DictLit:
							noun = "dict"
						}
						return "", mixedKindErr(noun)
					}
					g.heapUsed = true
					switch lit := a.(type) {
					case *ListLit:
						h, err := g.heapListFrom(b, lit, "")
						if err != nil {
							return "", err
						}
						b.WriteString(fmt.Sprintf("  call void @rt_print_list(i32 %s, i32 0)\n", h))
					case *SetLit:
						h, err := g.heapSetFrom(b, lit, "")
						if err != nil {
							return "", err
						}
						b.WriteString(fmt.Sprintf("  call void @rt_set_print(i32 %s, i32 0)\n", h))
					case *DictLit:
						h, err := g.heapDictFrom(b, lit, "")
						if err != nil {
							return "", err
						}
						b.WriteString(fmt.Sprintf("  call void @rt_dict_print(i32 %s, i32 0)\n", h))
					}
					continue
				}
				v, err := g.value(b, a)
				if err != nil {
					return "", err
				}
				// a generator result is a runtime heap list handle: print it
				// as a list rather than an int.
				if g.listOperands[v] {
					b.WriteString(fmt.Sprintf("  call void @rt_print_list(i32 %s, i32 0)\n", v))
					continue
				}
				fmtName, size := g.fmtStr("%d")
				t := g.newTmp()
				b.WriteString(fmt.Sprintf("  %s = call i32 (i8*, ...) @printf(i8* getelementptr inbounds ([%d x i8], [%d x i8]* %s, i32 0, i32 0), i32 %s)\n", t, size, size, fmtName, v))
			}
			if i == len(args)-1 {
				last = t
			}
		}
		argMarks = append(argMarks, b.Len())
		if rewritten, moved, _ := deferPrintLineWrites(b.String(), argMarks); moved {
			b.Reset()
			b.WriteString(rewritten)
		}
		emitText(end)
		return last, nil
	case "set", "list", "dict":
		// Container constructors. `{}` is already the empty dict and `{1, 2}` a set,
		// but the empty *set* has no literal at all, so `set()` is the only way to
		// write one; `list()`/`dict()` are the matching spellings (roadmap Gap K.3).
		// Lowering to the corresponding empty literal keeps one allocation path.
		if len(c.Args) > 1 {
			return "", fmt.Errorf("codegen: %s() takes at most 1 argument", calleeName(c))
		}
		if len(c.Args) == 1 {
			// Copying another container is a loop over its elements; the interpreter
			// supports it, so say which backend does rather than miscompile (ADR 0166).
			return "", fmt.Errorf("codegen: %s(<container>) copies are not supported in the AOT backend yet; CPython answers them — build the container with %s() and add elements", calleeName(c), calleeName(c))
		}
		// Allocate a fresh heap container. Lowering to an empty literal instead would
		// hand back a compile-time global (@.set1), and `s = set()` would then store that
		// global into the variable slot — the folded-global bug this repo has been bitten
		// by before (ADR 0163). rt_alloc zeroes the length in both the fresh and the
		// recycled path, so the handle it returns is an empty container.
		g.heapSeq++
		hs := g.heapSeq
		kind := 3 // set
		switch c.Fn.(*Name).Value {
		case "list":
			kind = 1
		case "dict":
			kind = 2
		}
		g.heapUsed = true
		g.emitRtAllocKind(b, fmt.Sprintf("%%h%d", hs), kind, c.Span())
		return fmt.Sprintf("%%h%d", hs), nil
	case "len":
		// len(string-constant) -> compile-time character count; otherwise
		// len(list/dict/set) loads the count field (i32 0) of the inline
		// literal's global struct. Layouts share the count as the first field.
		if len(c.Args) != 1 {
			return "", fmt.Errorf("len expects one argument")
		}
		if n, ok := stringConstLen(c.Args[0], g.builtinShadowed); ok {
			return fmt.Sprintf("%d", n), nil
		}
		// len of a string the compiler cannot read asks the table, and counts code points —
		// the same unit every other position question in this language is asked in
		// (ADR 0225, ADR 0229). Bytes would say 5 for "café" and be wrong about its own string.
		if _, foldable := g.stringVal(c.Args[0]); !foldable {
			if sreg, isStr, err := g.strReg(b, c.Args[0]); err != nil {
				return "", err
			} else if isStr {
				// Code points, not bytes: the same unit every other position question in this
				// language is asked in (ADR 0225). Bytes would call "café" 5 characters long.
				return g.rtStrCall(b, "rt_str_nchars", "i32 "+sreg), nil
			}
		}
		// imported string module global (data imports): len(mod.str)
		if attr, ok := c.Args[0].(*Attr); ok {
			if nm, ok2 := attr.Obj.(*Name); ok2 {
				if globals, ok3 := g.imports.Globals[nm.Value]; ok3 {
					if lit, ok4 := globals[attr.Name.Value]; ok4 {
						if str, ok5 := lit.(*StrLit); ok5 {
							return fmt.Sprintf("%d", len([]rune(str.Value))), nil // code points, ADR 0225
						}
						if lst, ok5 := lit.(*ListLit); ok5 {
							return fmt.Sprintf("%d", len(lst.Elems)), nil
						}
						if dct, ok5 := lit.(*DictLit); ok5 {
							return fmt.Sprintf("%d", len(dct.Keys)), nil
						}
					}
				}
			}
		}
		switch lit := c.Args[0].(type) {
		case *Name:
			if g.strVals != nil {
				if sv, ok := g.strVals[lit.Value]; ok {
					return fmt.Sprintf("%d", len([]rune(sv))), nil
				}
			}
			// An interned string (parameter, container element, loop variable) has no
			// compile-time text: measure it through the runtime table (Gap J.5).
			if g.internedVars[lit.Value] {
				g.heapUsed = true
				v, err := g.value(b, lit)
				if err != nil {
					return "", err
				}
				t := g.newTmp()
				// The length of a text is its CODE points, on the road where the text has no compile-time
				// spelling too — `rt_str_len` counts the bytes the table stores, `rt_str_nchars` counts the
				// characters the program sees (ADR 0225, ADR 0362).
				fmt.Fprintf(b, "  %s = call i32 @rt_str_nchars(i32 %s)\n", t, v)
				return t, nil
			}
			if g.listVars[lit.Value] {
				g.heapSeq++
				hs := g.heapSeq
				b.WriteString(fmt.Sprintf("  %%h%d = load i32, i32* %%_%s\n", hs, lit.Value))
				b.WriteString(fmt.Sprintf("  %%l%d = call i32 @rt_list_len(i32 %%h%d)\n", hs, hs))
				return fmt.Sprintf("%%l%d", hs), nil
			}
			if g.runtimeDicts[lit.Value] {
				g.heapSeq++
				hs := g.heapSeq
				b.WriteString(fmt.Sprintf("  %%h%d = load i32, i32* %%_%s\n", hs, lit.Value))
				b.WriteString(fmt.Sprintf("  %%l%d = call i32 @rt_dict_len(i32 %%h%d)\n", hs, hs))
				return fmt.Sprintf("%%l%d", hs), nil
			}
			if g.runtimeSets[lit.Value] {
				g.heapSeq++
				hs := g.heapSeq
				b.WriteString(fmt.Sprintf("  %%h%d = load i32, i32* %%_%s\n", hs, lit.Value))
				b.WriteString(fmt.Sprintf("  %%l%d = call i32 @rt_set_len(i32 %%h%d)\n", hs, hs))
				return fmt.Sprintf("%%l%d", hs), nil
			}
			if err := g.moduleStateErr(lit.Value); err != nil {
				return "", err
			}
			return "", fmt.Errorf("len of a non-string variable")

		case *ListLit:
			// len of a list literal is the element count; no element lowering needed.
			return fmt.Sprintf("%d", len(lit.Elems)), nil
		case *DictLit:
			// A dict literal with strings is a heap object (its keys/values are interned),
			// so measure it through the runtime like any other dict (Gap J.6).
			if literalNeedsHeap(lit) || g.taggableMixedDict(lit) || g.literalTakesPairSlots(lit) {
				h, err := g.heapDictFrom(b, lit, "")
				if err != nil {
					return "", err
				}
				v := g.newTmp()
				b.WriteString(fmt.Sprintf("  %s = call i32 @rt_dict_len(i32 %s)\n", v, h))
				return v, nil
			}
			name, err := g.emitDict(lit)
			if err != nil {
				return "", err
			}
			n := len(lit.Keys)
			v := g.newTmp()
			b.WriteString(fmt.Sprintf("  %s = load i32, i32* getelementptr({i32, [%d x i32], [%d x i32]}, {i32, [%d x i32], [%d x i32]}* %s, i32 0, i32 0)\n", v, n, n, n, n, name))
			return v, nil
		case *SetLit:
			if literalNeedsHeap(lit) || g.taggableMixedSet(lit) || g.literalTakesPairSlots(lit) {
				h, err := g.heapSetFrom(b, lit, "")
				if err != nil {
					return "", err
				}
				v := g.newTmp()
				b.WriteString(fmt.Sprintf("  %s = call i32 @rt_set_len(i32 %s)\n", v, h))
				return v, nil
			}
			name, err := g.emitSet(lit)
			if err != nil {
				return "", err
			}
			n := len(lit.Elems)
			v := g.newTmp()
			b.WriteString(fmt.Sprintf("  %s = load i32, i32* getelementptr({i32, [%d x i32]}, {i32, [%d x i32]}* %s, i32 0, i32 0)\n", v, n, n, name))
			return v, nil
		case *Comp:
			// lowered comprehension result: same global shape as a list literal,
			// so len loads the stored count field.
			name, err := g.comp(b, lit)
			if err != nil {
				return "", err
			}
			n := g.compLen[lit]
			v := g.newTmp()
			b.WriteString(fmt.Sprintf("  %s = load i32, i32* getelementptr({i32, [%d x i32]}, {i32, [%d x i32]}* %s, i32 0, i32 0)\n", v, n, n, name))
			return v, nil
		case *Call:
			if elems, ok := g.dictMethodElems(c.Args[0]); ok {
				return fmt.Sprintf("%d", len(elems)), nil
			}
			if elems, ok := listCallElems(c.Args[0]); ok {
				return fmt.Sprintf("%d", len(elems)), nil
			}
			if n, ok := g.listLen(c.Args[0]); ok {
				return fmt.Sprintf("%d", n), nil
			}
			if mc, ok := c.Args[0].(*Call); ok {
				if at, isAttr := mc.Fn.(*Attr); isAttr && g.exprIsString(at.Obj) && g.textMethodAnswersText(at.Name.Value, len(mc.Args)) {
					// `len(w.lower())`, `len(w.strip(" "))`: the answer of this pair is text, so its length is the
					// number of code points in the answer the module already knows how to build — asked of the
					// answer, through the same predicate the print road uses to decide whether to print it as text
					// (ADR 0229, ADR 0362). Refusing here would leave `len` silent about a text the road beside it can make.
					av, err := g.value(b, mc)
					if err != nil {
						return "", err
					}
					g.heapUsed = true
					t := g.newTmp()
					b.WriteString(fmt.Sprintf("  %s = call i32 @rt_str_nchars(i32 %s)\n", t, av))
					return t, nil
				}
			}
			if h, _, okH := g.containerHandleOf(b, c.Args[0]); okH {
				return g.heapLenOf(b, h), nil
			}
			if ix, isIx := c.Args[0].(*Index); isIx {
				// The slot belongs to a container the program built rather than spelled: its own tag
				// array says what it holds, so the length is asked of the object (ADR 0187, L11.1).
				if v, okLen := g.lenOfSlotArgument(b, ix); okLen {
					return v, nil
				}
			}
			return "", g.slotReadRefusal(c.Args[0], "len")
		default:
			// len(xs[0]), len(d["a"]), len(s[0]): the argument is a read rather than a spelling, so
			// its length is a word inside the object the slot names, and the question goes to the
			// runtime the same way a container variable's does. The tag is what licenses asking it:
			// a slot the compiler cannot see is a container is refused, because the length of whatever
			// object happens to share a number's index is not an answer (roadmap L11.1, ADR 0241).
			if h, _, okH := g.containerHandleOf(b, c.Args[0]); okH {
				return g.heapLenOf(b, h), nil
			}
			if ix, isIx := c.Args[0].(*Index); isIx {
				if v, okLen := g.lenOfSlotArgument(b, ix); okLen {
					// The tag travels with the payload to the check, which is what lets a slot the
					// compiler never saw be measured as the text or container it turned out to hold —
					// and refuse to be measured as a number (roadmap L11.1, ADR 0241).
					return v, nil
				}
			}
			return "", g.slotReadRefusal(c.Args[0], "len")
		}
	case "any", "all":
		// any(iter) is 1 if any element is nonzero; all(iter) is 1 if all are.
		anyMode := fnName == "any"
		var elems []Expr
		switch a := c.Args[0].(type) {
		case *ListLit:
			elems = a.Elems
		case *SetLit:
			elems = a.Elems
		case *Call:
			if le, ok := listCallElems(a); ok {
				elems = le
			} else {
				return "", fmt.Errorf("any/all need a list literal")
			}
		default:
			return "", fmt.Errorf("any/all need a list literal")
		}
		for _, elem := range elems {
			if isContainerLiteral(elem) {
				// The fold would test the element's compile-time global for truth, which is a
				// global in an i32 slot: llc refuses it, and the exit-code contract calls that a
				// compiler bug for an ordinary program (ADR 0166). The interpreter, whose elements
				// are boxed values, answers `any([[1], [2]])` (roadmap L11.1).
				return "", fmt.Errorf("%s asks each element whether it is truthy, and %s is a container the compiled fold has no word for; CPython answers this program (roadmap L11.1)", fnName, exprSnippet(elem))
			}
		}
		if len(elems) == 0 {
			if anyMode {
				return "0", nil
			}
			return "1", nil
		}
		b0, err := g.value(b, elems[0])
		if err != nil {
			return "", err
		}
		acc := g.newTmp()
		b.WriteString(fmt.Sprintf("  %s = icmp ne i32 %s, 0\n", acc, b0))
		for i := 1; i < len(elems); i++ {
			el, err := g.value(b, elems[i])
			if err != nil {
				return "", err
			}
			bi := g.newTmp()
			b.WriteString(fmt.Sprintf("  %s = icmp ne i32 %s, 0\n", bi, el))
			op := "or"
			if !anyMode {
				op = "and"
			}
			a2 := g.newTmp()
			b.WriteString(fmt.Sprintf("  %s = %s i1 %s, %s\n", a2, op, acc, bi))
			acc = a2
		}
		// any/all yield an i1 accumulator; widen to i32 so callers (e.g. print)
		// can consume it as an integer 0/1.
		res := g.newTmp()
		b.WriteString(fmt.Sprintf("  %s = zext i1 %s to i32\n", res, acc))
		return res, nil

	case "sum":
		// sum(list) -> sum the elements of an inline list literal (unrolled).
		if len(c.Args) != 1 {
			return "", fmt.Errorf("sum expects one argument")
		}
		// sum over a lowered comprehension: the elements are folded constants,
		// so fold to a single constant at codegen time.
		if comp, ok := c.Args[0].(*Comp); ok {
			if _, err := g.comp(b, comp); err != nil {
				return "", err
			}
			if g.rtComps[comp] {
				// The elements are computed at runtime, so there is no compile-time element
				// set to add up. Folding the empty one would answer 0 for a list that has
				// elements — a wrong answer wearing the costume of a refusal (ADR 0192).
				return "", fmt.Errorf("sum of a comprehension whose elements are computed at runtime needs a runtime reduction (roadmap L11.7, ADR 0192)")
			}
			total := int64(0)
			for _, e := range g.compEls[comp] {
				total += e
			}
			return fmt.Sprintf("%d", total), nil
		}
		var elems []Expr
		if de, ok := g.dictMethodElems(c.Args[0]); ok {
			elems = de
		}
		if le, ok := listCallElems(c.Args[0]); ok {
			elems = le
		}
		if ln, ok := c.Args[0].(*ListLit); ok {
			elems = ln.Elems
		} else if sl, ok := c.Args[0].(*SetLit); ok {
			elems = sl.Elems
		} else if dl, ok := c.Args[0].(*DictLit); ok {
			if len(dl.Keys) > 0 {
				// The interpreter rejects non-empty dict literals for sum.
				return "", fmt.Errorf("sum expects a list or set")
			}
			elems = dl.Keys
		}
		if elems == nil {
			// Empty inline list/set/dict literals are valid (sum([]) -> 0),
			// so promote a nil slice to an empty one for those receivers.
			switch c.Args[0].(type) {
			case *ListLit, *SetLit, *DictLit:
				elems = []Expr{}
			default:
				return "", fmt.Errorf("sum requires an inline list/set/dict literal")
			}
		}
		if len(elems) == 0 {
			// sum([]) folds to 0, matching the interpreter.
			return "0", nil
		}
		for _, elem := range elems {
			if isContainerLiteral(elem) {
				// The fold would add the element's globals; Python answers this program with
				// `unsupported operand type(s) for +: 'int' and 'list'`, and this backend has no
				// operator dispatch to raise either, so the honest answer is the refusal
				// (roadmap L11.1, ADR 0166).
				return "", fmt.Errorf("sum adds numbers, and %s is a container: there is no numeric answer to give (Python raises TypeError for this program)", exprSnippet(elem))
			}
		}
		anyFloat := false
		fvals := make([]float64, 0, len(elems))
		for _, elem := range elems {
			// An element is asked what it adds. Text adds nothing here: Python raises
			// `unsupported operand type(s) for +: 'int' and 'str'`, and this fold answering 0
			// for sum(["a"]) is a number nobody asked for (roadmap L11.1, ADR 0166).
			if isStringExpr(elem) || g.printsAsInternedStr(elem) {
				return "", fmt.Errorf("sum adds numbers, and %s is text: Python raises TypeError for this program, and the compiled fold has no number to answer with (roadmap L11.1)", exprSnippet(elem))
			}
			if g.isFloat(elem) {
				anyFloat = true
			}
			if fv, ok := g.floatEval(elem); ok {
				fvals = append(fvals, fv)
				continue
			}
			// An integer element of a sum that also holds a float contributes its value. The
			// fold used to collect only the float elements and then, finding the list "not all
			// float", added everything in the i32 domain — so sum([1, 2.5]) printed 3.0, the
			// 2.5 truncated on the way through the integer path (roadmap L11.1).
			if il, ok := elem.(*IntLit); ok {
				fvals = append(fvals, float64(il.Value))
			}
		}
		if anyFloat {
			if len(fvals) == len(elems) {
				total := 0.0
				for _, fv := range fvals {
					total += fv
				}
				t := g.newTmp()
				materialiseFloat(b, t, total)

				return t, nil
			}
			// Some element is computed at runtime and the answer is a float: there is no i32
			// answer to fall back to, and truncating each element on the way through one is
			// the wrong answer this fold used to give (roadmap L11.7, ADR 0192).
			return "", fmt.Errorf("sum of a list whose elements are computed at runtime, with a float among them, needs a runtime reduction (roadmap L11.7)")
		}

		acc, err := g.value(b, elems[0])
		if err != nil {
			return "", err
		}
		for i := 1; i < len(elems); i++ {
			el, err := g.value(b, elems[i])
			if err != nil {
				return "", err
			}
			t := g.newTmp()
			b.WriteString(fmt.Sprintf("  %s = add i32 %s, %s\n", t, acc, el))
			acc = t
		}
		return acc, nil
	case "min", "max":
		// `min(a, b, ...)` is a value chosen from values, not an error about not having written a
		// list. The fold returns the winner, which means its kind is the winner's kind too.
		//
		// A `key=` is answered here, not by the fold: the door asks the option what it is, calls it on
		// each candidate through the ordinary call road, and compares the answers with the comparator the
		// unkeyed choice already uses (Gap R.249, ADR 0340).
		if picked := g.keyedMinMaxCall(b, c); picked.done {
			return picked.handle, picked.err
		}
		if len(c.Args) != 1 {
			return g.emitScalarMinMax(b, c.Args, fnName == "min", c.Span())
		}
		// min/max(list) -> fold the elements of an inline list literal (unrolled).
		// min/max accept a single scalar value (treated as a one-element
		// collection): min(5) -> 5, max(7) -> 7, matching the interpreter.
		if il, ok := c.Args[0].(*IntLit); ok {
			return fmt.Sprintf("%d", il.Value), nil
		}
		// min/max over a lowered comprehension: fold the constant elements.
		if comp, ok := c.Args[0].(*Comp); ok {
			if _, err := g.comp(b, comp); err != nil {
				return "", err
			}
			if g.rtComps[comp] {
				return "", fmt.Errorf("%s of a comprehension whose elements are computed at runtime needs a runtime reduction (roadmap L11.7, ADR 0192)", fnName)
			}
			els := g.compEls[comp]
			if len(els) == 0 {
				return "", fmt.Errorf("%s of an empty comprehension", fnName)
			}
			best := els[0]
			for _, e := range els[1:] {
				if fnName == "min" && e < best {
					best = e
				}
				if fnName == "max" && e > best {
					best = e
				}
			}
			return fmt.Sprintf("%d", best), nil
		}
		var elems []Expr
		if de, ok := g.dictMethodElems(c.Args[0]); ok {
			elems = de
		}
		if le, ok := listCallElems(c.Args[0]); ok {
			elems = le
		}
		if ln, ok := c.Args[0].(*ListLit); ok {
			elems = ln.Elems
		} else if sl, ok := c.Args[0].(*SetLit); ok {
			elems = sl.Elems
		} else if dl, ok := c.Args[0].(*DictLit); ok {
			if len(dl.Keys) > 0 {
				// The interpreter rejects non-empty dict literals for min/max.
				return "", fmt.Errorf("%s expects a list or set", fnName)
			}
			elems = dl.Keys
		}
		if elems == nil {
			return "", fmt.Errorf("%s requires an inline list/set/dict literal", fnName)
		}
		if len(elems) == 0 {
			return "", fmt.Errorf("%s of an empty list", fnName)
		}
		for _, elem := range elems {
			if isContainerLiteral(elem) {
				// The fold compares the elements with icmp on i32, and an element that is a
				// container is a compile-time global there — a global in an i32 slot, which llc
				// refuses, and the exit-code contract counts as a compiler bug for an ordinary
				// program (ADR 0166). Python compares lists element by element and answers this;
				// the interpreter, whose elements are boxed, agrees with it (roadmap L11.1).
				return "", fmt.Errorf("%s compares its elements, and %s is a container: the compiled fold has no word for comparing two containers, so it would compare globals (CPython answers this program; roadmap L11.1)", fnName, exprSnippet(elem))
			}
		}
		if trap, raised, err := g.minMaxKindTrap(b, elems, fnName == "min", c.Span()); raised || err != nil {
			return trap, err
		}
		if g.minMaxVarargsAreText(elems) {
			return g.emitTextMinMax(b, elems, fnName == "min")
		}
		// When every element is a number the compiler can hold, the winner is decided here and
		// the answer is that element's own text. Comparing the i32 payloads instead truncated a
		// float on the way past g.value, so max([1, 2.5]) answered 2 (roadmap L11.1).
		if vals, floats, foldable := g.numericFoldElems(elems); foldable && len(vals) > 0 {
			best := numericWinner(vals, fnName == "min")
			if floats[best] {
				// The winner is a float, so this is the wrong domain to be asked in: the caller
				// that prints or binds the value asks g.isFloat first, and that question answers
				// yes for exactly this shape. Reaching here means the two rules disagreed, and a
				// truncated integer is what the disagreement used to produce.
				return "", fmt.Errorf("%s of these elements is the float %s, which the integer fold cannot return; the float path answers this program (roadmap L11.1)", fnName, floatConst(vals[best]))
			}
			return fmt.Sprintf("%d", int64(vals[best])), nil
		}
		best, err := g.value(b, elems[0])
		if err != nil {
			return "", err
		}
		for i := 1; i < len(elems); i++ {
			el, err := g.value(b, elems[i])
			if err != nil {
				return "", err
			}
			cmp := g.newTmp()
			op := "icmp sgt"
			if fnName == "min" {
				op = "icmp slt"
			}
			b.WriteString(fmt.Sprintf("  %s = %s i32 %s, %s\n", cmp, op, el, best))
			t := g.newTmp()
			b.WriteString(fmt.Sprintf("  %s = select i1 %s, i32 %s, i32 %s\n", t, cmp, el, best))
			best = t
		}
		return best, nil
	case "abs":
		// abs(x) -> x < 0 ? -x : x (constant-folded when x is a literal).
		if len(c.Args) != 1 {
			return "", fmt.Errorf("abs expects one argument")
		}
		if g.isFloat(c.Args[0]) {
			if fv, ok := g.floatEval(c.Args[0]); ok {
				if fv < 0 {
					fv = -fv
				}
				t := g.newTmp()
				materialiseFloat(b, t, fv)

				return t, nil
			}
		}

		if il, ok := c.Args[0].(*IntLit); ok {
			if il.Value < 0 {
				return fmt.Sprintf("%d", -il.Value), nil
			}
			return fmt.Sprintf("%d", il.Value), nil
		}
		// `abs` is a numeric use, so it asks the unary minus's question of an operand whose kind the source
		// spells (ADR 0266's door, one table, one wording): a text, `None`, a container or an instance has no
		// number in it, and negating the word the value happens to be stored as is the answer this door used
		// to give — `abs("hi")` printed `0`, `abs([1])` wrote `sub i32 0, @.lst1` and spent the contract's
		// "the compiler is broken" code on a program the reference merely stops on
		// (roadmap Gap R.140, ADR 0271).
		if kind, bad := g.absOperandKind(c.Args[0]); bad {
			raised := false
			if ix, isIdx := c.Args[0].(*Index); isIdx {
				_, raised = g.emitBadAbsOfSlot(b, ix, c.Span())
			}
			if !raised {
				// One kind, or a slot the literal does not fully describe: the same raise, from the name the
				// door already has. Falling through to the numeric road here is the answer this door used to
				// give, and it is the one ADR 0166 forbids.
				g.emitBadAbs(b, kind, c.Span())
			}
			return "0", nil
		}
		v, err := g.value(b, c.Args[0])
		if err != nil {
			return "", err
		}
		neg := g.newTmp()
		b.WriteString(fmt.Sprintf("  %s = sub i32 0, %s\n", neg, v))
		cmp := g.newTmp()
		b.WriteString(fmt.Sprintf("  %s = icmp slt i32 %s, 0\n", cmp, v))
		t := g.newTmp()
		b.WriteString(fmt.Sprintf("  %s = select i1 %s, i32 %s, i32 %s\n", t, cmp, neg, v))
		return t, nil
	case "sqrt":
		// sqrt promotes its argument to a real number and answers the square root as a float —
		// and RAISES where the argument is negative, which is what the reference does with it.
		// Until ADR 0264 a visible negative was a compile-time refusal (exit 1 for a program
		// CPython runs) and a run-time negative answered `nan`, a value where the reference has a
		// raise (roadmap Gap R.51).
		if len(c.Args) != 1 {
			return "", fmt.Errorf("%s", mathNameArityMessage("sqrt", len(c.Args)))
		}
		sv, raised, serr := g.sqrtDouble(b, c.Args[0], c.Span())
		if serr != nil {
			return "", serr
		}
		if raised {
			// The block is closed over by the raise; a well-formed double keeps whatever is emitted
			// next parseable.
			return "0.000000e+00", nil
		}
		return sv, nil
	case "floor", "ceil":
		// The whole-number question, answered in the word a whole number travels in. Both backends
		// used to disagree with the reference and with each other here: the evaluator trapped
		// `NameError` on a name the checker predeclares, and the compiled leg returned the double
		// (`print(floor(3.7))` → `3.0` where `math.floor` answers the int `3`).
		who := "floor"
		if n, ok := c.Fn.(*Name); ok && n.Value == "ceil" {
			who = "ceil"
		}
		if len(c.Args) != 1 {
			return "", fmt.Errorf("%s", mathNameArityMessage(who, len(c.Args)))
		}
		return g.floorCeilValue(b, who, c.Args[0], c.Span())
	case "range":
		for _, a := range c.Args {
			if _, ok := a.(*KeywordArg); ok {
				return "", fmt.Errorf("codegen: range does not accept keyword arguments")
			}
		}
		if len(c.Args) != 1 {
			return "", fmt.Errorf("codegen: range needs one argument")
		}
		return g.value(b, c.Args[0])
	case "str", "repr":
		// str() and repr() are one pair, and the pair is one table (roadmap L11.2, ADR 0258).
		// The question each half asks is "how does this value write?", and the answer comes from
		// the same renderer print uses — pointed at a capture buffer instead of stdout — so the
		// two can no longer disagree about a form, the way they did while str() had a
		// number-formatter and print had a printer. Everything the pair can name by its own rules
		// answers here, in the order the forms are distinguished: a container asks its object,
		// a verdict writes its name, the void writes None, a float takes the round-tripping text,
		// a text writes itself under str and its quoted repr under repr, and a number writes its
		// digits. What none of those rules covers is refused by naming the half that is missing,
		// because a missing rendering must not become the number underneath the value: `str([1, 2])`
		// used to answer `0` and `str(None)` answered `0`, both with exit 0.
		if len(c.Args) == 0 && calleeName(c) == "str" {
			// str() is a constructor answering the empty text, and the compiler can name it: the
			// same @str_tab index the literal "" interned to. repr() has NO default and keeps
			// refusing, because the reference raises for `repr()` (Gap R.131's asymmetry).
			return g.internStr(b, ""), nil
		}
		if len(c.Args) != 1 {
			return "", fmt.Errorf("%s expects one argument", calleeName(c))
		}
		form := FormStr
		if calleeName(c) == "repr" {
			form = FormRepr
		}
		if out, handled, rerr := g.renderPair(b, c.Args[0], form, c.Span()); handled {
			return out, rerr
		}
		if calleeName(c) == "repr" {
			return "", g.renderPairRefusal(c.Args[0], form)
		}
		// str(n) folds to the decimal string of an int literal.
		// str(True) is the word "True" and not the digit "1". Both spellings are known right
		// here, so the answer is the @str_tab index the 0/1 selects — an ordinary string value
		// from here on, which is what makes `s = str(flag)` then `print(s.upper())` behave like
		// the text it is (Gap R.42's rule, roadmap L11.1 step 2, ADR 0257).
		if g.printsAsBool(c.Args[0]) {
			sv, serr := g.value(b, c.Args[0])
			if serr != nil {
				return "", serr
			}
			tt := g.internStr(b, "True")
			ff := g.internStr(b, "False")
			bt := g.newTmp()
			b.WriteString(fmt.Sprintf("  %s = icmp ne i32 %s, 0\n", bt, sv))
			sel := g.newTmp()
			b.WriteString(fmt.Sprintf("  %s = select i1 %s, i32 %s, i32 %s\n", sel, bt, tt, ff))
			return sel, nil
		}
		if g.isFloat(c.Args[0]) {
			if fv, ok := g.floatEval(c.Args[0]); ok {
				// A string produced by a call is an @str_tab index, not the address of a global (Gap R.42, ADR 0224).
				return g.internStr(b, pyFloatRepr(fv)), nil
			}
		}
		// str(None) is "None" and str("x") is "x" — not the int 0 and not an error.
		// The two compile-time folders agree on these texts (stringConst, and
		// irGen.stringVal for print/len/concat); this is the third site that has to,
		// because print(str(None)) lowers here rather than through an assignment
		// (ADR 0183). A string constant prints with %s, so handing back the folded
		// global is correct in that position; storing one is not, which is why the
		// assignment path interns (see the AssignStmt string branch).
		if _, ok := c.Args[0].(*NoneLit); ok {
			// A string produced by a call is an @str_tab index, not the address of a global (Gap R.42, ADR 0224).
			return g.internStr(b, "None"), nil
		}
		if sl, ok := c.Args[0].(*StrLit); ok {
			// A string produced by a call is an @str_tab index, not the address of a global (Gap R.42, ADR 0224).
			return g.internStr(b, sl.Value), nil
		}
		v, err := g.value(b, c.Args[0])
		if err != nil {
			return "", err
		}
		n, err := strconv.ParseInt(v, 10, 64)
		if err != nil {
			// str() of a number the compiler cannot read is digits written at run time and
			// interned, so the answer is an ordinary string value (ADR 0229). It used to be
			// reachable only for a literal argument, which made str(42) work and str(get())
			// refuse — one rule, two behaviours.
			// A float must keep refusing: the compiled backend has no float in a word-sized
			// slot yet (L11.6, ADR 0226), and truncating one to render it would be the
			// silent-truncation bug that gap exists to keep dead.
			// The run-time digits road may only be handed something the compiler can SEE is a number. It used
			// to take anything that was not a text and not a float, so `def f(v): print(str(v))` called with
			// `None` interned the digits of the None handle and printed `0` at exit 0 -- `str(None)` written
			// literally answers `None` through the renderer, and only the parameter form fell through to the
			// number underneath the value (roadmap L11.2, Gap R.171; ADR 0258's rule that a missing rendering
			// must not become a number).
			if !g.builtinShadowed("str") && len(c.Args) == 1 && !g.exprIsString(c.Args[0]) &&
				!g.isNoneExpr(c.Args[0]) && !g.printsAsInternedStr(c.Args[0]) && !g.nameIsContainerRecorded(c.Args[0]) &&
				g.strArgIsNumberish(c.Args[0]) &&
				!strings.Contains(exprTyName(c.Args[0]), "float") && !g.isFloat(c.Args[0]) {
				v, verr := g.value(b, c.Args[0])
				if verr != nil {
					return "", verr
				}
				r := g.rtStrCall(b, "rt_str_of_int", "i32 "+v)
				g.checkStrSentinels(b, r, "RuntimeError", strFullMessage, c.Span(), "stoint")
				return r, nil
			}
			// `str(v)` where v is not a number the renderer can name. The old sentence was three words —
			// `str on non-integer` — which told a reader nothing about what was missing, and an agent
			// could not act on it (ADR 0166's rule is that a refusal names the half it lacks).
			return "", fmt.Errorf("codegen: str(%s) is refused: this backend renders a text through the one str/repr table, which reaches an integer, a text, None, bool and the container literals, and has no word for this argument's kind — the reference answers str() of any value, and doing the same needs the value word that carries its own kind (roadmap L11.1/L11.2, Gap R.171)", exprSurface(c.Args[0]))
		}
		// A string produced by a call is an @str_tab index, not the address of a global (Gap R.42, ADR 0224).
		return g.internStr(b, fmt.Sprintf("%d", n)), nil
	case "int":
		// int() with no argument is a CONSTRUCTOR and answers 0 — a constant the compiler can emit
		// without asking anything of an operand that does not exist. It used to answer
		// "int expects one argument" at exit 1 for a program the reference prints `0`, which ADR 0166
		// reserves exit 1 for the program's own missing features, not the compiler's untouched shapes
		// (roadmap Gap R.131, ADR 0287). Too many arguments still refuse; there is no honest 0-arg
		// answer to a 2-argument call.
		if len(c.Args) == 0 {
			return "0", nil
		}
		if len(c.Args) != 1 {
			return "", fmt.Errorf("int expects one argument")
		}
		if g.isFloat(c.Args[0]) {
			if fv, ok := g.floatEval(c.Args[0]); ok {
				return strconv.FormatInt(int64(fv), 10), nil
			}
			t := g.newTmp()
			fmt.Fprintf(b, "  %s = fptosi double %s to i32\n", t, g.floatValue(b, c.Args[0]))
			return t, nil
		}

		if il, ok := c.Args[0].(*IntLit); ok {
			return fmt.Sprintf("%d", il.Value), nil
		}
		if bl, ok := c.Args[0].(*BoolLit); ok {
			// A verdict IS the number 1 or 0 to the reference — `int(True)` is 1 — and this fold already
			// answered for an `int` literal, so refusing it was a hole in one door rather than a missing
			// feature (roadmap Gap R.213, closed by ADR 0326 beside the traps this row owns).
			if bl.Value {
				return "1", nil
			}
			return "0", nil
		}
		if sl, ok := c.Args[0].(*StrLit); ok {
			n, okNum, tooWide := intFromText(sl.Value)
			if tooWide {
				// Not this row's decision: the reference's `int` is unbounded and this backend's word is
				// an i64, so folding `int("99999999999999999999")` would answer a wrong number at the exit
				// code of success. A refusal is the honest direction to be wrong in (roadmap L12.12).
				return "", fmt.Errorf("codegen: int(%s) is an integer wider than this backend's word: the reference has an unbounded int and this one has a 64-bit slot, so the fold would answer a plausible wrong number rather than the reference's — the bounded-vs-arbitrary decision roadmap L12.12 is waiting for (ADR 0326)", exprSurface(c.Args[0]))
			}
			if !okNum {
				// The reference does not refuse this program: it raises ValueError and lets the program's
				// `except ValueError:` answer. The fold used to stop the whole compilation at exit 1, which
				// is a different language event and unreachable from the handler (roadmap Gap R.37, ADR 0326).
				g.constantTrap(b, "ValueError", intLiteralSentence(sl.Value), c.Span())
				return "0", nil
			}
			return fmt.Sprintf("%d", n), nil
		}
		if kind := g.unconvertibleArg(c.Args[0]); kind != "" {
			// The reference does not fold these: it raises TypeError and names the operand's type. The
			// fold used to answer "codegen folds only literal int/string args", which is a fact about this
			// compiler rather than about the program (roadmap Gap R.37, ADR 0326).
			g.constantTrap(b, "TypeError", convertArgumentSentence("int", kind), c.Span())
			return "0", nil
		}
		return "", fmt.Errorf("int: codegen folds only literal int/string args")
	case "reversed":
		if len(c.Args) != 1 {
			return "", fmt.Errorf("reversed expects one argument")
		}
		if lit, ok := c.Args[0].(*ListLit); ok {
			rev := &ListLit{Elems: reversedExprs(lit.Elems)}
			return g.emitList(rev)
		}
		if lit, ok := c.Args[0].(*StrLit); ok {
			// A string produced by a call is an @str_tab index, not the address of a global (Gap R.42, ADR 0224).
			return g.internStr(b, reverseStr(lit.Value)), nil
		}
		// imported string module global (data imports): reversed(mod.str)
		if attr, ok := c.Args[0].(*Attr); ok {
			if nm, ok2 := attr.Obj.(*Name); ok2 {
				if globals, ok3 := g.imports.Globals[nm.Value]; ok3 {
					if lit2, ok4 := globals[attr.Name.Value]; ok4 {
						if str, ok5 := lit2.(*StrLit); ok5 {
							// A string produced by a call is an @str_tab index, not the address of a global (Gap R.42, ADR 0224).
							return g.internStr(b, reverseStr(str.Value)), nil
						}
						if lst, ok5 := lit2.(*ListLit); ok5 {
							return g.emitList(&ListLit{Elems: reversedExprs(lst.Elems)})
						}
					}
				}
			}
		}
		return "", fmt.Errorf("reversed: codegen folds only literal list/string args")
	case "sorted":
		// sorted(iter[, reverse=True]) folds to a sorted inline list literal.
		// The AOT codegen represents lists as constant integer-element globals,
		// so sorted folds only over an inline list literal of integer literals,
		// mirroring the interpreter's ascending-by-value default and the
		// descending reverse=True keyword / truthy positional second arg.
		//
		// The options are read by NAME first (Gap R.249, ADR 0340): until this row the second argument
		// was the reverse flag wherever it stood, which is how `sorted([3,1,2], key=abs, reverse=True)`
		// printed ascending at exit 0 and `sorted([3,1,2], key=3)` printed descending at exit 0 — a
		// non-callable tested for truthiness — while `sorted([3,1,2], key=abs)` refused with a sentence
		// about `reverse=`. An option the builtin does not take raises what the reference raises.
		if sortedKey := g.keyedSortedCall(b, c, false); sortedKey.done {
			return sortedKey.handle, sortedKey.err
		}
		if len(c.Args) < 1 || len(c.Args) > 2 {
			return "", fmt.Errorf("sorted expects 1 or 2 arguments")
		}
		// The ordinary cases first: sorted(xs) on a variable, and sorted([...]) with
		// anything but plain integers inside. The fold below is a fast path for constant
		// int literals; the runtime path copies and sorts, so sorted(xs) leaves xs in its
		// original order — the difference between the builtin and the method that a
		// program can see (roadmap L11.7, ADR 0191).
		if g.isContainerExpr(c.Args[0]) {
			foldable := false
			if lit, ok := c.Args[0].(*ListLit); ok {
				foldable = true
				for _, el := range lit.Elems {
					if _, isInt := el.(*IntLit); !isInt {
						foldable = false
					}
				}
			}
			if !foldable {
				return g.sortedRuntime(b, c)
			}
		}
		ln, ok := c.Args[0].(*ListLit)
		if !ok {
			// imported module list global (data imports): sorted(mod.list)
			if attr, ok2 := c.Args[0].(*Attr); ok2 {
				if nm, ok3 := attr.Obj.(*Name); ok3 {
					if globals, ok4 := g.imports.Globals[nm.Value]; ok4 {
						if lit, ok5 := globals[attr.Name.Value]; ok5 {
							if lst, ok6 := lit.(*ListLit); ok6 {
								ln, ok = lst, true
							}
						}
					}
				}
			}
		}
		if !ok {
			// Three words is not a refusal a reader can act on. The sentence says what the door takes,
			// what this program handed it, and which roadmap row owns the rest (ADR 0166).
			return "", fmt.Errorf("codegen: sorted(%s) is refused: this backend sorts an inline list literal it can evaluate at compile time, and this argument is not one — the reference sorts any iterable by building the container at run time, which needs the value word that carries its own kind (roadmap L11.1)", exprSurface(c.Args[0]))
		}
		// One ordering, one implementation: the loop road (`for v in sorted([3, 1, 2])`) and this answer call
		// the same `sortedLiteralElems`, so the order a program sees when it prints the sorted list and the
		// order it sees when it steps over it cannot drift apart (ADR 0258's one-table rule, applied to a loop; roadmap
		// Gap R.246, ADR 0339).
		elems, folded, ferr := g.sortedLiteralElems(ln.Elems, c.Args[1:])
		if ferr != nil {
			return "", ferr
		}
		if !folded {
			return "", fmt.Errorf("sorted: list elements must be integer literals")
		}
		return g.emitList(&ListLit{Elems: elems})
	case "chr":
		// chr(n) folds a constant codepoint to a single-character string
		// global, mirroring the interpreter's string(rune(n)).
		if len(c.Args) != 1 {
			return "", fmt.Errorf("chr expects one argument")
		}
		cn, cerr := g.constIntVal(c.Args[0])
		if cerr != nil {
			return "", fmt.Errorf("chr: codegen folds only a constant integer arg")
		}
		if cn < 0 || cn > 0x10FFFF {
			// The fold turned an out-of-range code point into a rune Go renders as a replacement glyph,
			// so `print(chr(-1))` printed a byte the program never named at the exit code of success.
			// The reference stops the program with a ValueError the author can catch (Gap R.37, ADR 0326).
			g.constantTrap(b, "ValueError", chrSentence(), c.Span())
			return "0", nil
		}
		// A string produced by a call is an @str_tab index, not the address of a global (Gap R.42, ADR 0224).
		return g.internStr(b, string(rune(cn))), nil
	case "ord":
		// ord(s) folds a constant string to the codepoint of its first byte,
		// mirroring the interpreter (int64(o.sval[0])).
		if len(c.Args) != 1 {
			return "", fmt.Errorf("ord expects one argument")
		}
		sv, ok := stringConst(c.Args[0], g.builtinShadowed)
		if !ok {
			// imported string module global (data imports): ord(mod.str)
			if attr, ok2 := c.Args[0].(*Attr); ok2 {
				if nm, ok3 := attr.Obj.(*Name); ok3 {
					if globals, ok4 := g.imports.Globals[nm.Value]; ok4 {
						if lit, ok5 := globals[attr.Name.Value]; ok5 {
							if str, ok6 := lit.(*StrLit); ok6 {
								if str.Value == "" {
									g.constantTrap(b, "TypeError", ordSentence(0), c.Span())
									return "0", nil
								}
								modRunes := []rune(str.Value)
								if len(modRunes) != 1 {
									g.constantTrap(b, "TypeError", ordSentence(len(modRunes)), c.Span())
									return "0", nil
								}
								return fmt.Sprintf("%d", int(modRunes[0])), nil
							}
						}
					}
				}
			}
		}
		if !ok {
			// ord of a string the compiler cannot read asks the table (ADR 0229). -1 means the
			// string is not exactly one code point, which is CPython's TypeError and, being a
			// raise, is catchable (ADR 0212).
			if sreg, isStr, err2 := g.strReg(b, c.Args[0]); err2 != nil {
				return "", err2
			} else if isStr {
				cp := g.rtStrCall(b, "rt_str_codepoint", "i32 "+sreg)
				g.checkStrSentinels(b, cp, "TypeError", "ord() expected a character", c.Span(), "ordcp")
				return cp, nil
			}
			return "", fmt.Errorf("ord: codegen folds only a constant string arg")
		}
		if len(sv) == 0 {
			g.constantTrap(b, "TypeError", ordSentence(0), c.Span())
			return "0", nil
		}
		// the first code point, not the first byte (ADR 0225)
		runes := []rune(sv)
		if len(runes) != 1 {
			// A constant text of two characters is the same TypeError as an empty one, quoting the length
			// the author has to fix; reading the first code point and answering it was the fold inventing an
			// answer the reference declines (roadmap Gap R.37, ADR 0326).
			g.constantTrap(b, "TypeError", ordSentence(len(runes)), c.Span())
			return "0", nil
		}
		return fmt.Sprintf("%d", int64(runes[0])), nil
	case "round":
		// round(x) folds a constant integer literal to itself (mirroring the
		// interpreter's int case; the AOT backend has no float representation).
		// A float goes to the nearest value with ties to EVEN: `llvm.roundeven.f64` is IEEE
		// roundTiesToEven and `math.RoundToEven` is the same operation on the host, so the constant
		// fold, the runtime call and the interpreter cannot drift the way `llvm.round.f64` and
		// `math.Round` did — both tied away from zero, both wrong where CPython ties to even.
		//
		// The two-argument form is a different operation and lives in round_digits.go: a digit
		// count moves the decimal point rather than asking for a whole number, so `round(x, n)`
		// answers with a float (`round(3.5, 0)` is `4.0`) and has to say so (Gap R.69, ADR 0263).
		if len(c.Args) == 0 || len(c.Args) > 2 {
			return "", fmt.Errorf("%s", roundArityMessage(len(c.Args)))
		}
		if len(c.Args) == 2 {
			return g.roundDigitsValue(b, c)
		}
		if g.isFloat(c.Args[0]) {
			if fv, ok := g.floatEval(c.Args[0]); ok {
				return fmt.Sprintf("%d", int64(math.RoundToEven(fv))), nil
			}
			fx := g.floatValue(b, c.Args[0])
			rt := g.newTmp()
			fmt.Fprintf(b, "  %s = call double @llvm.roundeven.f64(double %s)\n", rt, fx)
			t := g.newTmp()
			fmt.Fprintf(b, "  %s = fptosi double %s to i32\n", t, rt)
			return t, nil
		}

		rv, rerr := g.constIntVal(c.Args[0])
		if rerr != nil {
			if _, ok := c.Args[0].(*StrLit); ok {
				return "", fmt.Errorf("round: cannot round a string")
			}
			v, verr := g.value(b, c.Args[0])
			if verr != nil {
				return "", verr
			}
			return v, nil
		}
		return fmt.Sprintf("%d", rv), nil
	case "float":
		// float(x) converts x to a float. The AOT backend represents floats
		// as truncated ints (value() truncates FloatLit to int64), so
		// float(int) folds to itself and float(str) parses the string to a
		// float then truncates, mirroring the interpreter's allocFloat.
		// float() is a CONSTRUCTOR answering 0.0, and it is exactly the literal `0.0`: returning a
		// folded FloatLit lets every road that already knows how to print a double take this one
		// unchanged. My first version emitted `g.floatValue(b, &FloatLit{Value: 0})` from the call
		// site, which handed the print road a double it then widened AGAIN — `%t2 = fadd double ...`
		// followed by `%t1 = sitofp i32 %t2 to double`, LLVM's verifier happy, the program printing
		// an empty line at exit 0 (roadmap Gap R.131, ADR 0287). A shape the compiler can express as
		// a literal should BE one, not a hand-rolled sequence with the same meaning.
		// float() is a CONSTRUCTOR answering 0.0. It is answered as a DOUBLE ROAD below like any
		// other float-returning expression; see isFloat's *Call arm, which already answers "yes" for
		// this name so print picks rt_fmt_double rather than %d (roadmap Gap R.131, ADR 0287).
		if len(c.Args) == 0 {
			// Folded like a literal: see floatEval's *Call arm for why the answer must come from
			// there rather than from a textual double handed back here.
			if fv, ok := g.floatEval(c); ok {
				return floatConst(fv), nil
			}
			return "", fmt.Errorf("float expects one argument")
		}
		if len(c.Args) != 1 {
			return "", fmt.Errorf("float expects one argument")
		}
		// A name the pair road bound has no one word to widen — the payload beside its tag is a float box,
		// an interned text, or the number, and only the tag says which. `rt_lift_num` answers the double and
		// raises CPython's own sentence for a kind that has no number in it (roadmap L11.1, Gap R.148).
		if nm, isName := c.Args[0].(*Name); isName && g.taggedVars[nm.Value] {
			p, t := g.numericPairRegs(b, nm.Value, nm.Span())
			return g.liftPair(b, p, t), nil
		}
		if il, ok := c.Args[0].(*IntLit); ok {
			return fmt.Sprintf("%d", il.Value), nil
		}
		if bl, ok := c.Args[0].(*BoolLit); ok {
			// A verdict IS a number to the reference: `float(True)` is 1.0 and the i32 road below answers the
			// word 1 for it, exactly as the `int` fold beside it does (roadmap Gap R.213, ADR 0326).
			if bl.Value {
				return "1", nil
			}
			return "0", nil
		}
		if sv, ok := stringConst(c.Args[0], g.builtinShadowed); ok {
			f, okNum := floatFromText(sv)
			if !okNum {
				g.constantTrap(b, "ValueError", floatLiteralSentence(sv), c.Span())
				return "0", nil
			}
			return fmt.Sprintf("%d", int64(f)), nil
		}
		if kind := g.unconvertibleArg(c.Args[0]); kind != "" {
			// `float(None)` and `float([1, 2])` answered `0.0` and an empty line: the general road below
			// widens whatever word the operand lowers to, and for a text, a None or a container that word is
			// an interned index, a singleton or a handle. The reference names the operand's type and stops the
			// program (roadmap Gap R.37, ADR 0326).
			g.constantTrap(b, "TypeError", convertArgumentSentence("float", kind), c.Span())
			return "0", nil
		}
		// General arg: emit a real double value. floatValue already converts
		// ints to doubles (sitofp) and passes through float values, so
		// float(x) == floatValue(x) for numeric args.
		v := g.floatValue(b, c.Args[0])
		if v == "" {
			return "", fmt.Errorf("float: unsupported argument")
		}
		return v, nil
	case "bool":
		// bool() is a constructor answering False, and bool(x) asks the same question the `if` road
		// asks, answered by the select the `str()` road beside it already emits for a verdict (ADR
		// 0257: a verdict writes its own name, not the 1 the slot holds). The name reached the
		// `unsupported call` fall-through, which is ADR 0166's exit 1 vocabulary for a feature the
		// PROGRAM lacks — and `print(bool())` is a program the reference prints `False`
		// (roadmap Gap R.131, ADR 0287).
		// bool() answers a VERDICT, and a verdict in this backend is the WORD 1 or 0 that
		// rt_print_bool turns into True/False — not an interned "True"/"False" text. My first
		// version selected between the two interned strings, so `print(bool(1))` handed the index
		// of "True" to a printer that asks only whether the word is zero and answered `False`, and
		// `print(bool(0))` answered `True`: the pair of answers was INVERTED, which no amount of
		// reading the select would reveal (roadmap Gap R.131, ADR 0287).
		if len(c.Args) == 0 {
			return "0", nil
		}
		if len(c.Args) != 1 {
			return "", fmt.Errorf("bool expects one argument")
		}
		// A CONTAINER's truthiness is whether it has any elements, which is a question about its
		// length and therefore about the object, not about the word the slot holds. Comparing that word
		// against zero is what my first version did, and it failed twice over: for a list literal the
		// word is a GLOBAL and llc-20 rejects the module (`icmp ne i32 @.lst1, 0` — "global variable
		// reference must have pointer type"), and for a container the answer would have been "non-empty"
		// for every handle ever allocated, `bool([])` included. Refusing is the honest answer until the
		// tagged value word lets the object be asked (roadmap Gap R.131, ADR 0287, L11.1).
		if g.nameIsContainerRecorded(c.Args[0]) {
			return "", fmt.Errorf("codegen: a container's truthiness is its length, and this backend cannot ask an object how many elements it has from a call to bool(); CPython answers it (roadmap Gap R.131, ADR 0166)")
		}
		// A TEXT asks a different question than a number: `bool("")` is False and `bool("x")` is
		// True, and a text's word is its interned INDEX, whose being non-zero says nothing about
		// whether it has any characters. So the text road measures it (rt_str_len), and only the
		// number road gets to use "the word is not zero" (roadmap Gap R.131, ADR 0287).
		bv, err := g.value(b, c.Args[0])
		if err != nil {
			return "", err
		}
		probe := bv
		if g.exprIsString(c.Args[0]) || isStringExpr(c.Args[0]) {
			ln := g.newTmp()
			b.WriteString(fmt.Sprintf("  %s = call i32 @rt_str_len(i32 %s)\n", ln, bv))
			probe = ln
		}
		// The verdict word itself: 1 when the probe is non-zero, 0 otherwise. This is the same
		// shape rt_print_bool asks, with the printer's own test hoisted into the caller.
		bit := g.newTmp()
		b.WriteString(fmt.Sprintf("  %s = icmp ne i32 %s, 0\n", bit, probe))
		val := g.newTmp()
		b.WriteString(fmt.Sprintf("  %s = zext i1 %s to i32\n", val, bit))
		return val, nil
	default:
		// The name is called and the backend cannot find a callable behind it. The reference raises
		// TypeError here — `'int' object is not callable` — because it asks the value what it is at
		// the moment of the call; this backend decides at the compile door, and says so with the half
		// it is missing rather than the word "unsupported" alone (ADR 0166's vocabulary, and the
		// tagged value word of roadmap L11.1 is what would let the question be asked at run time).
		return "", fmt.Errorf("codegen: %q is called but is not a function this backend can build a call to: the reference asks the value what it is at the moment of the call and raises TypeError (`'int' object is not callable`), and asking that question needs the value word that carries its own kind (roadmap L11.1, ADR 0166)", fnName)
	}
}
