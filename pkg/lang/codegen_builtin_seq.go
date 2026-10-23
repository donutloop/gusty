// codegen_builtin_seq.go is the builtin road for the sequence-shaped names: the
// constructors, `len`, `any`/`all`, `reversed`, `sorted` and `range`.
package lang

import (
	"fmt"
	"strings"
)

// builtinSorted is the road `sorted` takes, which is the order the rt_sort door answers
func (g *irGen) builtinSorted(b *strings.Builder, c *Call, fnName string) (string, error) {
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
}

// builtinReversed is the road `reversed` takes
func (g *irGen) builtinReversed(b *strings.Builder, c *Call, fnName string) (string, error) {
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
}

// builtinRange is the road `range` takes where a range still has a value of its own
func (g *irGen) builtinRange(b *strings.Builder, c *Call, fnName string) (string, error) {
	for _, a := range c.Args {
		if _, ok := a.(*KeywordArg); ok {
			return "", fmt.Errorf("codegen: range does not accept keyword arguments")
		}
	}
	if len(c.Args) != 1 {
		return "", fmt.Errorf("codegen: range needs one argument")
	}
	return g.value(b, c.Args[0])
}

// builtinAnyAll is the road `any` and `all` take — the truthiness of every element, asked
// through the one truthy road
func (g *irGen) builtinAnyAll(b *strings.Builder, c *Call, fnName string) (string, error) {
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
}

// builtinLen is the road `len` takes: the length door of the kind the argument actually holds
func (g *irGen) builtinLen(b *strings.Builder, c *Call, fnName string) (string, error) {
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
		if g.caughtVars[lit.Value] {
			// The name an `except ... as` clause bound prints as the message the raise carried, and it
			// is NOT a text to measure: the reference asks the exception OBJECT, which has no length,
			// and answering `16` for `len(err)` at exit 0 is the wrong answer ADR 0166 refuses. The
			// generic sentence below would have told the reader the name holds no string, which is a
			// claim about a program they did not write (roadmap Gap R.113, Gap R.395, Gap R.38's rule).
			return "", fmt.Errorf("len(%s) is refused: %q is the name an `except ... as` clause bound, and what it holds is not a text to measure — the reference asks the exception OBJECT, which has no length (`TypeError: object of type 'ZeroDivisionError' has no len()`), and asking that question needs the value word that carries its own kind (roadmap Gap R.113, Gap R.395, L11.1)", g.exprSummary(c.Args[0]), lit.Value)
		}
		return "", fmt.Errorf("len of a non-string variable")

	case *ListLit:
		// len of a list literal is the element count; no element lowering needed.
		return fmt.Sprintf("%d", len(lit.Elems)), nil
	case *DictLit:
		// A dict literal with strings is a heap object (its keys/values are interned),
		// so measure it through the runtime like any other dict (Gap J.6).
		if g.literalNeedsHeap(lit) || g.taggableMixedDict(lit) || g.literalTakesPairSlots(lit) {
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
		if g.literalNeedsHeap(lit) || g.taggableMixedSet(lit) || g.literalTakesPairSlots(lit) {
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
}

// builtinContainers is the road the container constructors take, including a copy of a
// container already on the heap
func (g *irGen) builtinContainers(b *strings.Builder, c *Call, fnName string) (string, error) {
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
}
