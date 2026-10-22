// pair_param_truth.go — the truth of a name whose kind the CALLER supplied asks the tag
// (roadmap L11.1, Gap R.361, ADR 0393).
//
// ADR 0389 paid the argument: `f(d["a"])` where `d` is a dict literal now crosses as a (payload, tag)
// pair, so the callee's `define` carries a second word and `bindPairParams` stores it beside the
// parameter's payload. What that row did not do is ask the callee's OWN condition the same question.
// `truthOperandErr` reaches, for a name the pair road bound, the arm ADR 0268 built for a name the
// ARITHMETIC door bound — lift the pair with `@rt_lift_num`, compare the double against `0.0`. That is the
// right answer for the only kinds the arithmetic door can write (int, float, bool) and the wrong one for
// every kind the CALLER can write: a text's payload is its `@str_tab` index, a container's is a heap
// handle, `None`'s is `0`, and `rt_lift_num` widens each of those as if it were the number — so
//
//	def f(v):
//	    if v:
//	        return 1
//	    return 0
//
//	d = {"e": [], "t": "", "l": [1], "n": None}
//	print(f(d["e"]), f(d["t"]), f(d["l"]), f(d["n"]))
//
// printed `1 1 1 0` at exit 0 where the reference prints `0 0 1 0`. `@rt_pair_truth` is the table that
// already answers this question for the operand `and`/`or` tests (ADR 0275), for a self-describing entry
// in an `if` (ADR 0388) and for the item of an unrolled loop (ADR 0386): a number in either family is
// true when it is not zero, an empty text or an empty container is false, `None` is false, any other heap
// object is true. The parameter's truth is the same fact about the same pair, so it asks the same table.
//
// Why the gate is the ORIGIN and not the shape. The pair road binds a name from several kinds of
// evidence, and the origins say what the tag word is allowed to hold:
//
//   - `taggedOriginArith`, `taggedOriginFloat`, `taggedOriginParamArith` — an arithmetic answer, whose tag
//     `@rt_num_arith` wrote and which can therefore only ever name int, float or bool. The lift arm answers
//     this one exactly, and it keeps answering it: those names stay on ADR 0268's road, and the module a
//     `if n:` over such a name compiles to does not grow a call it has no reason to make.
//   - `taggedOriginParam` — the tag is a register the RUN TIME wrote, filled at the call site from the
//     object the argument named (ADR 0389). Nothing in the compiler has read it. This is the origin whose
//     truth cannot be answered from one word, and this file is that ask.
//   - `taggedOriginSlot`, `taggedOriginLoop`, `taggedOriginFold` — a name bound from a slot, a loop element
//     or a fold. These travel as a pair too, and their truth is the same missing tagged value word the
//     print, `str`/`repr` and the arithmetic positions were paid for one door at a time; they are
//     roadmap Gap R.146's, and `TestTheTruthOfTheEntryBoundaryStaysWhereADR0388DrewIt` pins what each of
//     them answers (or refuses) today. Opening them here would fold one row's evidence into another's, and
//     a door that opens two rows at once is judged by neither.
//
// Why the gate is also the BODY. The ask reads the name's two slot words, and only one store in the
// program writes them: the one `bindPairParams` emits at the top of the body. A rebinding — `while v: …
// v = 0` — lowers through the plain scalar road, which stores the payload and retires the compile-time
// record (ADR 0172's latest-binding rule) but leaves the run-time tag word holding whatever the call site
// left there. Asking `@rt_pair_truth` then reads a tag that describes no value the name holds: `[]` in the
// slot with a stale list tag asks `rt_heap_len` of the payload, and `def f(v): while v: … v = []` runs
// forever instead of ending. So the road is taken only where the entry pair is the ONLY pair the name can
// hold for the frame's life — `bodyKeepsTheEntryPair`, which walks the body for every form that can write
// the name and declines the door (and so keeps the road, and the answer, the program has always had) where
// anything writes it. A straight-line rebinding does not need the walk: by the time the later read lowers,
// ADR 0172 has already cleared the record and the origin gate answers this case by itself. What the walk
// is for is the read a loop header emits BEFORE the store that would clear it — the one place where a
// compile-time record cannot see a run-time edge (measured: three probes hung where the baseline answered,
// wrong, and terminatingly). The rebind family is roadmap Gap R.365, filed with those probes.
//
// The ask is therefore one predicate — `truthAsksThePairSuppliedName` — and every truth position that
// wants it goes through it: the `if`/`while`/ternary condition road and `not` (all four lower through
// `truthOperandErr`), and `bool()`, which asks the same question on its own road beside `str()`.
package lang

import (
	"reflect"
	"strings"
)

// truthAsksThePairSuppliedName is the gate: does this name hold a (payload, tag) pair whose tag the
// CALLER wrote, and whose pair nothing in the body can overwrite? A name the arithmetic door bound answers
// its own truth (see the file header); a name no pair road bound has no second word to ask, and keeps the
// road — and the answer — it has always had.
func (g *irGen) truthAsksThePairSuppliedName(name string) bool {
	if g == nil || !g.taggedVars[name] || g.taggedOrigin == nil {
		return false
	}
	if g.taggedOrigin[name] != taggedOriginParam {
		return false
	}
	return g.bodyKeepsTheEntryPair(name)
}

// pairSuppliedNameTruth emits the truth of such a name as the i1 the condition roads want, and reports
// whether it emitted anything at all. The two words are loaded through the one door every pair-bound read
// uses — which is also the one place ADR 0228's written-flag is tested for all of them, so a parameter
// slot no path wrote cannot answer either word (Gap R.68).
func (g *irGen) pairSuppliedNameTruth(b *strings.Builder, e Expr) (bit string, asked bool) {
	nm, isName := e.(*Name)
	if !isName || !g.truthAsksThePairSuppliedName(nm.Value) {
		return "", false
	}
	payload, tag, ok := g.taggedLoopVarRead(b, nm.Value, nm.Span())
	if !ok {
		return "", false
	}
	return g.pairTruth(b, payload, tag), true
}

// bodyKeepsTheEntryPair asks the current function's body the static half of the gate: does anything in
// here write this name? With no function on the stack (a REPL fragment, a module-level read that the pair
// road should not be able to reach) there is no body to ask and no pair to trust, so the door declines.
func (g *irGen) bodyKeepsTheEntryPair(name string) bool {
	fd := g.curFD
	if fd == nil {
		if found, ok := g.fds[g.curFunc]; ok {
			fd = found
		}
	}
	if fd == nil {
		return false
	}
	return !stmtsRebindName(fd.Body, name)
}

// stmtsRebindName walks a statement list for every binding form that can write NAME. The walk is
// deliberately coarser than the emitter's own scope rules — a nested `def`'s parameter of the same name,
// a `for` that reuses it, a `match` that captures it all count as writes — because the two directions are
// not symmetric: declining the door costs the program the right answer it would otherwise have printed
// nowhere, while letting the door stand on a name something can write costs it a loop that never ends.
// A statement form the walk has no case for is a WRITE: an analysis that has to be sure fails by declining.
func stmtsRebindName(body []Stmt, name string) bool {
	var walk func([]Stmt) bool
	bind := func(target Expr) bool {
		if target == nil {
			return false
		}
		out := map[string]bool{}
		collectBoundTargets(target, out)
		return out[name]
	}
	walk = func(sts []Stmt) bool {
		for _, st := range sts {
			switch t := st.(type) {
			case nil:
			case *ExprStmt:
				if exprRebindsName(t.Expr, name) {
					return true
				}
			case *AssignStmt:
				if bind(t.Target) || exprRebindsName(t.Value, name) {
					return true
				}
			case *AugAssignStmt:
				if bind(t.Target) || exprRebindsName(t.Value, name) {
					return true
				}
			case *IfStmt:
				if exprRebindsName(t.Cond, name) || walk(t.Then) {
					return true
				}
				for _, e := range t.Elifs {
					if e != nil && walk([]Stmt{e}) {
						return true
					}
				}
				if walk(t.Else) {
					return true
				}
			case *WhileStmt:
				if exprRebindsName(t.Cond, name) || walk(t.Body) || walk(t.Else) {
					return true
				}
			case *ForStmt:
				if bind(t.Var) || exprRebindsName(t.Iter, name) || walk(t.Body) || walk(t.Else) {
					return true
				}
			case *TryStmt:
				if walk(t.Body) {
					return true
				}
				for _, arm := range t.Excepts {
					if arm != nil && walk(arm.Body) {
						return true
					}
				}
				if walk(t.Finally) {
					return true
				}
			case *WithStmt:
				if (t.As != nil && t.As.Value == name) || exprRebindsName(t.Expr, name) || walk(t.Body) {
					return true
				}
			case *MatchStmt:
				for _, c := range t.Cases {
					if c == nil {
						continue
					}
					for _, p := range append([]Expr{c.Pattern}, c.Or...) {
						if matchCaptureNames(p)[name] {
							return true
						}
						if exprRebindsName(p, name) {
							return true
						}
					}
					if exprRebindsName(c.Guard, name) || walk(c.Body) {
						return true
					}
				}
			case *FuncDef:
				// A nested function is a different frame, but its body is walked: a name the inner frame
				// writes is a name this frame's read may not be able to tell apart, and the direction of
				// the doubt is the direction that does not hang.
				for _, p := range t.Params {
					if p != nil && p.Name == name {
						return true
					}
				}
				if walk(t.Body) {
					return true
				}
			case *ClassDef:
				if walk(t.Body) {
					return true
				}
			case *ImportStmt:
				if importBoundNamesForFlag(t)[name] {
					return true
				}
			case *ReturnStmt:
				if exprRebindsName(t.Expr, name) {
					return true
				}
			case *RaiseStmt:
				if exprRebindsName(t.Expr, name) {
					return true
				}
			case *YieldStmt:
				if exprRebindsName(t.Expr, name) {
					return true
				}
			case *YieldFromStmt:
				if exprRebindsName(t.Expr, name) {
					return true
				}
			case *BreakStmt, *PassStmt, *ContinueStmt, *TypeAliasStmt, *ExternDecl:
			default:
				return true
			}
		}
		return false
	}
	return walk(body)
}

// exprRebindsName is the expression half of the walk: two Expr nodes bind names — a comprehension's
// clause target (whose slot lives in the SAME frame as the parameter whose truth is being asked, which is
// why `v = 3` and `[v for v in xs]` are the same hazard) and a `lambda`'s parameter. Everything else an
// expression holds is a read, so the descent is generic — the node types it passes through are not the
// point of the walk, and an expression form added to the AST next quarter is still descended.
func exprRebindsName(e Expr, name string) bool {
	if e == nil {
		return false
	}
	var scan func(reflect.Value) bool
	scan = func(v reflect.Value) bool {
		for v.IsValid() && (v.Kind() == reflect.Interface || v.Kind() == reflect.Pointer) {
			if v.IsNil() {
				return false
			}
			v = v.Elem()
		}
		if !v.IsValid() {
			return false
		}
		switch v.Kind() {
		case reflect.Slice, reflect.Array:
			for i := 0; i < v.Len(); i++ {
				if scan(v.Index(i)) {
					return true
				}
			}
		case reflect.Map:
			for _, k := range v.MapKeys() {
				if scan(v.MapIndex(k)) {
					return true
				}
			}
		case reflect.Struct:
			switch n := v.Interface().(type) {
			case Comp:
				for _, c := range n.compClauses() {
					if c != nil && c.Target != nil && c.Target.Value == name {
						return true
					}
				}
			case Lambda:
				for _, p := range n.Params {
					if p != nil && p.Name == name {
						return true
					}
				}
			}
			for i := 0; i < v.NumField(); i++ {
				if !v.Type().Field(i).IsExported() {
					continue
				}
				if scan(v.Field(i)) {
					return true
				}
			}
		}
		return false
	}
	return scan(reflect.ValueOf(e))
}
