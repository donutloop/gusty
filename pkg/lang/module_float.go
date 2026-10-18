package lang

import (
	"fmt"
	"strings"
)

// The module's float state, and the one road a compiled body takes to read it.
//
// A module-level name is a binding the program can read from anywhere, and ADR 0227 gave the compiled
// backend one honest mechanism for that: the name lives in a module GLOBAL that `@main` writes and any
// body loads, because a frame's allocas die with the frame and module state does not. That mechanism
// was built for the `i32` word alone. A float was left out on purpose — `moduleSlots` holds an `i32`,
// and a double does not fit it — so a body that named a module-level float was answered with a
// refusal, at exit 1, for a program CPython runs:
//
//	RATE = 2.5
//	def scale(v):
//	    return v * RATE
//	print(scale(2))      # CPython 5.0; this toolchain: "RATE" is bound at module level … Gap R.35
//
// The refusal was the right shape for a construct the backend cannot lower and the wrong shape for
// this one: the double already has a word, a slot type (`alloca double`), a printer (`rt_fmt_double`)
// and a return convention (`define double`) — every one of which ADR 0373 and ADR 0375 spent a cycle
// putting beside the body that computes it. What was missing was the module's half of the pair: a
// `global double` that `@main` writes when it binds the name, and a body that loads it when it reads
// the name.
//
// This file is that pair, and the gate that decides when a name may use it. The gate is the whole
// safety of the feature, because a module global has ONE word:
//
//   - a name gets one only when EVERY binding the module ever performs for it answers the double.
//     `X = 1.5` above `X = 3` is two words for one name — the tagged value word's problem (roadmap
//     L11.1), not this door's, and it stays refused;
//   - a name the module binds through a road this door does not write — a `for` loop variable, a
//     `with … as` name, a `match` capture, an unpacking target, an import — is refused for the whole
//     program. Each of those is a store that would have to write the global too: ADR 0351 learned
//     that on the `i32` side, where only the plain assignment asked the question and the loop wrote an
//     alloca nobody read, and the answer was a wrong value at exit 0;
//   - the value must be spelled out of literals the compiler can read (`2.5`, `2.0 * 3`, `7 / 2`,
//     `-1.5`). `RATE = float(x)` or `RATE = xs[0]` answers a double only when the program runs, and a
//     door that promises a word it cannot see is how a `0` reaches the screen.
//
// What the name may NOT do once it has a global is be a local anywhere it is read. A body that
// assigns the name binds it for its whole body (ADR 0220), and the read road asks that first:
// `def f(): RATE = 1.0; return RATE` reads its own `alloca double` and never touches the module's.
// That ordering is also what keeps this door from undoing ADR 0375 — the per-body float table answers
// 「what did THIS body write」, this table answers 「what does the module hold」, and the two are never
// the same map.
//
// A module-level CONTAINER read stays refused (this row's other half, still open): a container's word
// is a heap handle, which has to be rooted against the collector, and its element operations live in
// the per-scope notebooks ADR 0341 settles at a binding — a global handle buys none of that.

// moduleFloatSlotNames chooses the module-level float names that get a `global double`: every name
// some function, method or closure body reads WITHOUT binding it, that is not a folded module
// constant, and whose every module binding is a float-valued plain assignment. It is the float sibling
// of `moduleSlotNames` and shares that function's scope question (`moduleBodyReads`) — one rule about
// which names a body reads that it does not bind, asked in one place.
func moduleFloatSlotNames(prog *Program, consts map[string]Expr, all map[string]bool) map[string]string {
	slots := map[string]string{}
	if prog == nil {
		return slots
	}
	reads := moduleBodyReads(prog, all)
	if len(reads) == 0 {
		return slots
	}
	// What does every module binding of the name answer? A name starts eligible and any binding that
	// is not an answer-in-the-double — or that is not a plain assignment this door writes — takes that
	// away for the whole program.
	answer := map[string]bool{}
	seen := map[string]bool{}
	drop := func(nms ...string) {
		for _, nm := range nms {
			if nm == "" {
				continue
			}
			seen[nm] = true
			answer[nm] = false
		}
	}
	keep := func(nm string, isFloatAnswer bool) {
		if !seen[nm] {
			answer[nm] = isFloatAnswer
			seen[nm] = true
			return
		}
		answer[nm] = answer[nm] && isFloatAnswer
	}
	var walk func(sts []Stmt)
	walk = func(sts []Stmt) {
		for _, st := range sts {
			switch n := st.(type) {
			case *FuncDef, *ClassDef:
				continue // a body's bindings are local to it (ADR 0220)
			case *AssignStmt:
				nm, ok := n.Target.(*Name)
				if !ok {
					markTargetNames(n.Target, drop) // an unpacking binds names this door does not write
					continue
				}
				keep(nm.Value, moduleFloatAnswer(n.Value, nm.Value) && !(selfRead(n.Value, nm.Value) && !seen[nm.Value]))
			case *AugAssignStmt:
				nm, ok := n.Target.(*Name)
				if !ok {
					continue
				}
				// `RATE += 1.5` answers what its operand answers, and the road that binds it writes the
				// global beside the alloca — so it keeps the name eligible only on a double operand.
				keep(nm.Value, moduleFloatAnswer(n.Value, nm.Value) && !(selfRead(n.Value, nm.Value) && !seen[nm.Value]))
			case *ForStmt:
				drop(loopVarNames(n.Var)...)
				walk(n.Body)
				walk(n.Else)
			case *WithStmt:
				if n.As != nil {
					drop(n.As.Value)
				}
				walk(n.Body)
			case *MatchStmt:
				for _, c := range n.Cases {
					for nm := range matchCaptureNames(c.Pattern) {
						drop(nm)
					}
					for _, or := range c.Or {
						for nm := range matchCaptureNames(or) {
							drop(nm)
						}
					}
					walk(c.Body)
				}
			case *IfStmt:
				walk(n.Then)
				for _, e := range n.Elifs {
					walk(e.Then)
					walk(e.Else)
				}
				walk(n.Else)
			case *WhileStmt:
				walk(n.Body)
				walk(n.Else)
			case *TryStmt:
				walk(n.Body)
				for _, a := range n.Excepts {
					walk(a.Body)
				}
				walk(n.Finally)
			case *ImportStmt:
				drop(importBoundNames(n)...)
			case *TypeAliasStmt:
				drop(n.Name)
			}
		}
	}
	walk(prog.Stmts)
	for nm := range reads {
		if _, isConst := consts[nm]; isConst {
			continue // a value needs no slot
		}
		if !seen[nm] || !answer[nm] {
			continue // never bound here, or bound by a road this door does not write
		}
		slots[nm] = irSymbol("gy_mod_" + nm)
	}
	return slots
}

// markTargetNames hands every name a target expression binds to the "not this door's road" bucket —
// the same unpacking shapes (`a, b = …`, `[a, b] = …`) the i32 side treats as bindings.
func markTargetNames(target Expr, drop func(...string)) {
	if target == nil {
		return
	}
	switch t := target.(type) {
	case *Tuple:
		var nms []string
		for _, el := range t.Elems {
			nms = append(nms, targetName(el)...)
		}
		drop(nms...)
	case *ListLit:
		var nms []string
		for _, el := range t.Elems {
			nms = append(nms, targetName(el)...)
		}
		drop(nms...)
	}
}

func targetName(e Expr) []string {
	if nm, ok := e.(*Name); ok {
		return []string{nm.Value}
	}
	return nil
}

// importBoundNames is the name an `import` binds — the module's own name, which is a table of
// constants and never a double this door may promise.
func importBoundNames(n *ImportStmt) []string {
	if n == nil || n.Module == "" {
		return nil
	}
	name := n.Module
	if i := strings.LastIndexByte(name, '.'); i >= 0 {
		name = name[i+1:]
	}
	return []string{name}
}

// moduleFloatAnswer answers the gate's one question about an expression: is this an answer in the
// double, spelled out of literals the compiler can see? `RATE = 2.5`, `RATE = 2.0 * 3`, `RATE = 7 / 2`
// and `RATE = -1.5` are; `RATE = float(x)`, `RATE = xs[0]` and `RATE = input()` are not — those names
// stay refused, because this door promises the word it writes and a call it cannot read carries none.
func moduleFloatAnswer(e Expr, self string) bool {
	switch n := e.(type) {
	case *FloatLit:
		return true
	case *UnOp:
		return n.Op == "-" && moduleFloatAnswer(n.X, self)
	case *BinOp:
		if n.Op == "/" {
			return literalNumberTree(n.L, self) && literalNumberTree(n.R, self) // true division: a double, PEP 238
		}
		switch n.Op {
		case "+", "-", "*", "%", "//", "**":
			// The arithmetic answers a double when one side does (ADR 0221, ADR 0293) — and only over
			// literals, which is what lets the door name the word without asking an object about itself.
			return (moduleFloatAnswer(n.L, self) || moduleFloatAnswer(n.R, self) ||
				// `RATE = RATE * 2` reads the module's OWN previous binding, which is the other
				// reason this door can promise the word: the value it holds is one this same gate
				// wrote, and `@main` writes them in the order the source does.
				nameIsSelf(n.L, self)) &&
				literalNumberTree(n.L, self) && literalNumberTree(n.R, self)
		}
		return false
	}
	return false
}

// selfRead: does this expression mention the name at all — anywhere, at any depth? The gate asks it
// before allowing a name to feed on its own previous binding: `RATE = 2.5` then `RATE = RATE * 2` is a
// module computing on what it already holds, which `@main` replays in source order, while the same
// spelling as the FIRST binding is the program reading a name it has not bound yet, which is CPython's
// NameError and not a number this door may write into a global.
func selfRead(e Expr, name string) bool {
	switch n := e.(type) {
	case *Name:
		return n.Value == name
	case *UnOp:
		return selfRead(n.X, name)
	case *BinOp:
		return selfRead(n.L, name) || selfRead(n.R, name)
	}
	return false
}

// nameIsSelf: is this expression the name being bound, standing alone?
func nameIsSelf(e Expr, self string) bool {
	n, ok := e.(*Name)
	return ok && self != "" && n.Value == self
}

// literalNumberTree: an expression built only from numbers, so no part of it can hand back a
// container, a text, or a value whose kind the compiler cannot see.
func literalNumberTree(e Expr, self string) bool {
	switch n := e.(type) {
	case *IntLit, *FloatLit, *BoolLit:
		return true
	case *Name:
		return self != "" && n.Value == self
	case *UnOp:
		return n.Op == "-" && literalNumberTree(n.X, self)
	case *BinOp:
		return literalNumberTree(n.L, self) && literalNumberTree(n.R, self)
	}
	return false
}

// moduleFloatOwns answers the read-side question: is this name's value, from here, the module's
// double? A body that binds the name itself — anywhere in its body, the ADR 0220 rule — reads its own
// slot, and so does a body whose parameter carries the name. Both questions are asked here rather
// than at each caller so the print door, the arithmetic road, the comparison road and the return gate
// cannot hold four opinions about one name (ADR 0273's rule).
func (g *irGen) moduleFloatOwns(name string) bool {
	sym, ok := g.moduleFloatSlots[name]
	if !ok || sym == "" {
		return false
	}
	if g.params != nil {
		if _, isParam := g.params[name]; isParam {
			return false
		}
	}
	if g.funcLocals != nil && g.funcLocals[name] {
		return false
	}
	if g.allocd != nil && g.allocd[name] {
		return false
	}
	return true
}

// loadModuleFloat emits the read's one instruction: the module's double, at the moment the call runs.
// Only the FLOAT road calls it; the i32 road refuses the name through `refuseModuleFloat`, because one
// i32 slot, select or `ret` cannot hold a double (ADR 0166).
func (g *irGen) loadModuleFloat(b *strings.Builder, name string) string {
	sym := g.moduleFloatSlots[name]
	g.ldN++
	t := fmt.Sprintf("%%mfl%d", g.ldN)
	b.WriteString(fmt.Sprintf("  %s = load double, double* @%s, align 8\n", t, sym))
	return t
}

// refuseModuleFloat is the i32 road's sentence about the module's double: the position has one word for
// a value, the double has another, and emitting the load anyway is the type error `llc` rejects with
// exit 2 — the code the exit-code contract reserves for a compiler bug, which is why the road says the
// construct instead (ADR 0166). It names WHICH body has no double word: a method is emitted with an i32
// return word whatever its body computes, and so is a nested `def`, while a top-level `def` that returns
// the double is promoted to `define double` and compiles — so the same expression one indentation
// earlier is fine, and the reader learns that from the sentence (roadmap Gap R.3c, ADR 0196).
func (g *irGen) refuseModuleFloat(name string) error {
	sym := g.moduleFloatSlots[name]
	where := "this position"
	switch {
	case g.emittingMethod:
		where = "a method, which is emitted with an i32 return word whatever its body computes"
	case strings.Contains(g.curFnSrc, "."):
		where = fmt.Sprintf("a nested def (%s), which is emitted with one i32 word", g.curFnSrc)
	}
	return fmt.Errorf("codegen: %q is the module's float and %s: the double read from the module global (%s) has no word to travel in, and an i32 slot, select or `ret` would truncate it — which ADR 0166 counts as this compiler's bug, not the program's. Read it where a double has a word: behind `print`, in an arithmetic or comparison expression of a top-level `def`, or into a local of the body that reads it (roadmap Gap R.35, L11.1, Gap R.3c, ADR 0196, ADR 0166)", name, where, sym)
}

// storeModuleFloat emits the module's binding: the double, into the slot every read consults. It
// returns false when the name is not this door's, and the caller keeps its main-frame alloca — which
// is what a module float that NO body reads still is, byte for byte.
//
// It also refuses once the name has an alloca, which is the ADR 0351 rule read from the other side:
// one slot per name, or two slots are two opinions about one program.
func (g *irGen) storeModuleFloat(b *strings.Builder, name, val string) bool {
	if g.inFunc {
		return false
	}
	sym, ok := g.moduleFloatSlots[name]
	if !ok || sym == "" || g.allocd[name] {
		return false
	}
	b.WriteString(fmt.Sprintf("  store double %s, double* @%s, align 8\n", val, sym))
	// The module's own frame reads the name as the double it now holds — the same fact
	// `bindFloatValue` records for a frame-local float, recorded here without the alloca.
	if g.floatVars == nil {
		g.floatVars = map[string]bool{}
	}
	g.floatVars[name] = true
	if g.doubleSlot == nil {
		g.doubleSlot = map[string]bool{}
	}
	g.doubleSlot[name] = true
	g.forgetTaggedBinding(name) // the module global holds the value now (Gap R.142)
	return true
}

// returnedModuleFloat asks a method body the question ADR 0196 asks of a rebound parameter: does a
// `return` hand back the module's double? A method is emitted `define i32` whatever its body computes,
// so a return whose value comes from `@gy_mod_RATE` has no word to travel in — loading the double
// into an i32-returning define is the module `llc` rejects, which ADR 0166 counts as the compiler's
// bug. The refusal is the same family as that row's (L11.6 · Gap R.3c), asked here because the name
// arrives already double-typed rather than promoted by the body.
func returnedModuleFloat(fd *FuncDef, owns func(string) bool) string {
	if fd == nil {
		return ""
	}
	var scan func([]Stmt) string
	scan = func(sts []Stmt) string {
		for _, st := range sts {
			switch n := st.(type) {
			case *ReturnStmt:
				if nm := exprModuleFloatName(n.Expr, owns); nm != "" {
					return nm
				}
			case *IfStmt:
				if r := scan(n.Then); r != "" {
					return r
				}
				for _, e := range n.Elifs {
					if r := scan(e.Then); r != "" {
						return r
					}
					if r := scan(e.Else); r != "" {
						return r
					}
				}
				if r := scan(n.Else); r != "" {
					return r
				}
			case *WhileStmt:
				if r := scan(n.Body); r != "" {
					return r
				}
			case *ForStmt:
				if r := scan(n.Body); r != "" {
					return r
				}
			case *TryStmt:
				if r := scan(n.Body); r != "" {
					return r
				}
				for _, a := range n.Excepts {
					if r := scan(a.Body); r != "" {
						return r
					}
				}
			case *MatchStmt:
				for _, c := range n.Cases {
					if r := scan(c.Body); r != "" {
						return r
					}
				}
			}
		}
		return ""
	}
	return scan(fd.Body)
}

// exprModuleFloatName: is any leaf of this expression the module's double?
func exprModuleFloatName(e Expr, owns func(string) bool) string {
	switch n := e.(type) {
	case nil:
		return ""
	case *Name:
		if owns(n.Value) {
			return n.Value
		}
		return ""
	case *BinOp:
		if nm := exprModuleFloatName(n.L, owns); nm != "" {
			return nm
		}
		return exprModuleFloatName(n.R, owns)
	case *UnOp:
		return exprModuleFloatName(n.X, owns)
	case *CondExpr:
		if nm := exprModuleFloatName(n.If, owns); nm != "" {
			return nm
		}
		return exprModuleFloatName(n.Else, owns)
	case *Call:
		for _, a := range n.Args {
			if nm := exprModuleFloatName(a, owns); nm != "" {
				return nm
			}
		}
	}
	return ""
}
