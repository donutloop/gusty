package lang

// What the call sites hand a parameter — when every one of them hands it a float.
//
// **ADR 0336**'s bound gate asks what a slice bound IS, and it can only answer where the SOURCE wrote the
// kind. A parameter is the binding that is never in the body: `def at(k): return xs[0:k]` holds no
// assignment to `k`, so the classifier — which reads a name back from the last thing its own body bound —
// names nothing, and the call handed over one i32 word with no kind beside it. Measured at the exit code of
// a success, against a reference that raises:
//
//	def at(k):
//	    xs = [1, 2, 3]
//	    return xs[0:k]
//	print(at(1.5))    # TypeError: slice indices must be integers or None or have an __index__ method
//	                # this backend: 1  — the copy's heap handle, printed as a number
//
// The evidence for a parameter's kind is not in its body, it is at the call sites — which is exactly where
// `strArgKinds` reads a text and `verdictFacts` reads a verdict (**Gap R.111**, **ADR 0366**, **Gap R.315**:
// "a parameter every call site hands a verdict to behaves inside the body exactly like a name the body
// itself assigned a verdict to"). This file adds the float to that pair, with the same two rules that keep
// those doors honest:
//
//   - **Every call site, or nothing.** One argument that is not a float deletes the claim, so a function
//     called `at(1)` as well as `at(1.5)` keeps the road it has always had rather than being accused of a
//     kind one of its callers never wrote (**ADR 0273**: a gate may not cost a program that compiles today).
//   - **The body can take the claim back.** A body that rebinds its own parameter to a non-float has proven
//     the callers are not the whole story, and the claim goes.
//
// What the claim is read by is narrow on purpose: the slice-bound door (`nonIntBoundTypeName`), where the
// reference's answer is a raise. The subscript door beside it asks the same question of the same word and
// does not yet ask it — that shape is filed as its own row rather than quietly widened into here.
func floatArgKinds(prog *Program) map[string]map[int]bool {
	if prog == nil {
		return nil
	}
	callables := map[string]*FuncDef{}
	for _, st := range prog.Stmts {
		if fd, ok := st.(*FuncDef); ok {
			if _, dup := callables[fd.Name]; dup {
				// Two functions with one name: the last wins at call time and the first may still be
				// reachable through a closure, so neither is safe to mark (the same ruling
				// verdictFacts applies, for the same reason).
				callables[fd.Name] = nil
				continue
			}
			callables[fd.Name] = fd
		}
	}
	for _, st := range prog.Stmts {
		cd, ok := st.(*ClassDef)
		if !ok {
			continue
		}
		for _, st2 := range cd.Body {
			m, ok := st2.(*FuncDef)
			if !ok || m == nil {
				continue
			}
			if _, seen := callables[m.Name]; seen {
				callables[m.Name] = nil // ambiguous: two callables, one name
				continue
			}
			callables[m.Name] = m
		}
	}
	for name, fd := range callables {
		if fd == nil {
			delete(callables, name)
		}
	}
	if len(callables) == 0 {
		return nil
	}

	// Which names the program has so far only ever filled with a float, per scope. A name ever assigned a
	// non-float drops out, so a parameter called `k` in one function cannot drag an unrelated `k` along —
	// the rule scopeHasString enforces for text and the verdict census enforces for bools.
	floatVars := map[string]bool{}
	notFloat := map[string]bool{}
	// Evidence against a position: the same parameter reached with a non-float somewhere else.
	against := map[string]map[int]bool{}
	out := map[string]map[int]bool{}
	mark := func(fn string, idx int) {
		if idx < 0 {
			return
		}
		if out[fn] == nil {
			out[fn] = map[int]bool{}
		}
		out[fn][idx] = true
	}
	negate := func(fn string, idx int) {
		if idx < 0 {
			return
		}
		if against[fn] == nil {
			against[fn] = map[int]bool{}
		}
		against[fn][idx] = true
	}

	// asksFloat is the whole question in one place: is this argument a float, as far as the compiler can
	// tell from the AST and what it has learned about names so far? The spelling rule is the pair road's
	// (**ADR 0274**'s promotion and **ADR 0357**'s two-double arms are both decided by it), so this door and
	// that one cannot disagree about what a float expression is (roadmap **ADR 0232**).
	asksFloat := func(scope string, a Expr) bool {
		if a == nil {
			return false
		}
		if kw, ok := a.(*KeywordArg); ok {
			a = kw.Value
		}
		if pairExprIsFloatBySpelling(a) {
			return true
		}
		if nm, ok := a.(*Name); ok {
			return floatVars[scopeKey(scope, nm.Value)] || floatVars[scopeKey("", nm.Value)]
		}
		return false
	}
	// refusesFloat is the answer's mirror: an argument that provably is NOT a float, which unmarks the
	// position no matter how many floats arrive at it elsewhere.
	refusesFloat := func(a Expr) bool {
		if a == nil {
			return false
		}
		if kw, isKw := a.(*KeywordArg); isKw {
			if kw.Value == nil {
				return false
			}
			a = kw.Value
		}
		if pairExprIsFloatBySpelling(a) {
			return false
		}
		if nm, isName := a.(*Name); isName {
			return notFloat[scopeKey("", nm.Value)]
		}
		switch a.(type) {
		case *IntLit, *BoolLit, *StrLit, *NoneLit, *ListLit, *DictLit, *SetLit, *Tuple, *Comp, *Lambda, *FString:
			return true
		}
		return false
	}

	w := heapASTWalker{}
	w.assign = func(scope, target string, value Expr, annot *Type) {
		key := scopeKey(scope, target)
		if annot.IsFloat() || asksFloat(scope, value) {
			floatVars[key] = true
			delete(notFloat, key)
			return
		}
		if value == nil {
			return // a bare `k:` declares without binding; it is evidence about nothing
		}
		notFloat[key] = true
		delete(floatVars, key)
	}
	// oneCall reads one call site. `viaMethod` is true for `obj.m(x)`, where the receiver fills the
	// callable's first parameter (the same reading verdictFacts does for the verdict, for the same reason).
	oneCall := func(scope, fn string, args []Expr, viaMethod bool) {
		fd, ok := callables[fn]
		if !ok || fd == nil {
			return
		}
		pos := 0
		if viaMethod && len(fd.Params) > 0 && (fd.Params[0] == nil || fd.Params[0].Name == "self" || fd.Params[0].Name == "cls") {
			pos = 1
		}
		for _, a := range args {
			idx := pos
			if kw, isKw := a.(*KeywordArg); isKw {
				idx = -1
				for i, p := range fd.Params {
					if p != nil && p.Name == kw.Name {
						idx = i
						break
					}
				}
			}
			if asksFloat(scope, a) {
				mark(fn, idx)
			} else if refusesFloat(a) {
				negate(fn, idx)
			}
			if _, isKw := a.(*KeywordArg); !isKw {
				pos++
			}
		}
	}
	w.call = func(scope, fn string, args []Expr) { oneCall(scope, fn, args, false) }
	w.attrCall = func(scope, fn string, args []Expr) { oneCall(scope, fn, args, true) }

	seed := func() {
		for _, fn := range sortedCallableNames(callables) {
			fd := callables[fn]
			for i, p := range fd.Params {
				if p == nil {
					continue
				}
				if p.Annot.IsFloat() || asksFloat(fn, p.Default) {
					mark(fn, i)
				} else if refusesFloat(p.Default) {
					negate(fn, i)
				}
			}
		}
		w.scope = ""
		w.stmts(prog.Stmts)
	}
	seed()
	// Fixed point: a parameter known to receive a float makes the body's own name a float, so a forwarding
	// call discovered on a later pass (`def outer(k): at(k)`) marks `at` too.
	for pass := 0; pass < 8; pass++ {
		before := snapshotFloatKinds(out)
		for _, fn := range sortedCallableNames(callables) {
			fd := callables[fn]
			for i := range out[fn] {
				if i >= len(fd.Params) || fd.Params[i] == nil {
					continue
				}
				if notFloat[scopeKey(fn, fd.Params[i].Name)] {
					continue
				}
				floatVars[scopeKey(fn, fd.Params[i].Name)] = true
			}
		}
		seed()
		if snapshotFloatKinds(out) == before {
			break
		}
	}
	// Contradiction wins: a position any call site filled with a non-float is not a float position, whatever
	// the rest of the program did.
	for fn, neg := range against {
		for i := range neg {
			delete(out[fn], i)
		}
		if len(out[fn]) == 0 {
			delete(out, fn)
		}
	}
	// And a parameter the body itself rebinds to something that is not a float is not a float parameter:
	// the callers were not the whole story, and a claim the body contradicts is not evidence.
	for fn, m := range out {
		fd := callables[fn]
		for i := range m {
			if i >= len(fd.Params) || fd.Params[i] == nil || notFloat[scopeKey(fn, fd.Params[i].Name)] {
				delete(m, i)
			}
		}
		if len(m) == 0 {
			delete(out, fn)
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// snapshotFloatKinds is the fixed point's equality. It is the SAME question the verdict census asks of its
// own table, so it is the same function: two snapshots of a claim map that differ in nothing but which
// claim they describe would be two rules drifting apart (roadmap **ADR 0232**).
func snapshotFloatKinds(m map[string]map[int]bool) string { return snapshotBoolKinds(m) }
