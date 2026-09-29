package lang

import (
	"fmt"
	"sort"
	"strings"
)

// A slot the program never wrote must not read as a value.
//
// The compiled backend gives every local an alloca in its frame, and an alloca has no initial state:
// read one on a path that did not assign it and the program gets whatever the previous frame, or the
// allocator, left behind. Measured (roadmap Gap R.36, ADR 0228): `def f(c): if c: x = 1; return x`
// called with False printed 0 and exited 0, `while 0: w = 1` printed 8555776, and a `try` that raised
// before its second assignment printed 518208. CPython raises UnboundLocalError there and the
// interpreter traps; only the compiled path invented an answer.
//
// The fix is not to refuse. `def f(c): if c: x = 1; return x` is a program CPython accepts, because
// whether the read is an error depends on the argument: it is a runtime event, and so it needs a
// runtime representation — one bit per slot the checker could not prove was written, cleared on entry,
// set by every write, tested at the read, raising the class a handler can match on. Names the checker
// *can* prove are never paid for: no bit, no test, no extra instruction anywhere.
//
// The set of names is the checker's own: UnwrittenReads re-runs the checking walk and reports what it
// recorded at its `possibly unbound` warning, so this file contains no copy of the dataflow rule.
// What it does decide is *eligibility*: a flag is used only where every write to the name goes through
// a store site that sets the flag. A name written by any form not hooked here is left exactly as it
// was — a known gap rather than a wrong answer — because a check whose flag some writer forgets turns
// a silent zero into a spurious trap, which would be worse.

// enterBoundFlags puts a written-flag on each eligible local of the body about to be emitted and
// returns the restore closure. Call it right after enterBody, before any statement is emitted: the
// allocas and their zero-stores must dominate every read in the body.
func (g *irGen) enterBoundFlags(fd *FuncDef, body []Stmt) func() {
	savedFlags, savedSlots, savedOrder := g.boundFlags, g.boundSlots, g.boundOrder
	g.boundFlags, g.boundSlots, g.boundOrder = nil, nil, nil
	restore := func() { g.boundFlags, g.boundSlots, g.boundOrder = savedFlags, savedSlots, savedOrder }

	cand := g.unwritten[g.originFD(fd)]
	if !g.inFunc {
		cand = g.unwritten[nil]
	}
	if len(cand) == 0 {
		return restore
	}
	ok, bad := boundEligible(body)
	params := map[string]bool{}
	if fd != nil {
		for _, p := range fd.Params {
			params[p.Name] = true
		}
	}
	flags := map[string]bool{}
	slots := map[string]string{}
	names := make([]string, 0, len(cand))
	for nm := range cand {
		// A parameter is written by the call itself, and a name the module owns is read from
		// elsewhere: neither is a frame slot this body may leave unwritten.
		if params[nm] || g.moduleSlots[nm] != "" || g.moduleConsts[nm] != nil {
			continue
		}
		if bad[nm] || !ok[nm] {
			continue
		}
		flags[nm] = true
		slots[nm] = "bnd_" + nm
		names = append(names, nm)
	}
	if len(names) == 0 {
		return restore
	}
	sort.Strings(names) // the entry block must not depend on map order
	g.boundFlags, g.boundSlots, g.boundOrder = flags, slots, names
	return restore
}

// originFD follows a clone back to the FuncDef the checker saw. emitDecoratedFunc lowers a copy of a
// body under a new name, and a copy is a different pointer: without the alias its body would silently
// get no flags, so the alias is recorded rather than guessed at.
func (g *irGen) originFD(fd *FuncDef) *FuncDef {
	if fd == nil {
		return nil
	}
	if origin, ok := g.fdAlias[fd]; ok && origin != nil {
		return origin
	}
	return fd
}

// emitBoundAllocas writes the flags recorded by enterBoundFlags. It has to be called just after a
// function's opening brace, not where the flags are decided: an instruction before the `define` line
// lands in the module's global area, and `llc` reads it as a malformed global (exit 2, the toolchain
// blamed for a source error -- ADR 0166's contract).
func (g *irGen) emitBoundAllocas(b *strings.Builder) {
	for _, nm := range g.boundOrder {
		b.WriteString(fmt.Sprintf("  %%bnd_%s = alloca i8\n", nm))
		b.WriteString(fmt.Sprintf("  store i8 0, i8* %%bnd_%s\n", nm))
		// A flagged name has a slot whether or not any store has reached it yet. Two shapes depend
		// on this: a read above the first assignment (`print(v)` then `v = 2`), and a name whose
		// only assignment sits in a loop the folder removed (`for i in []: z = 1`). Their reads used
		// to hit codegen's unbound-name guard and refuse a program CPython runs -- which, after ADR
		// 0228, is supposed to be a runtime trap instead (Gap R.36 + R.37).
		if !g.allocd[nm] {
			b.WriteString(fmt.Sprintf("  %%_%s = alloca i32\n", nm))
			b.WriteString(fmt.Sprintf("  store i32 0, i32* %%_%s\n", nm))
			g.allocd[nm] = true
		}
	}
}

// markBound records that a write to this name happened. It sits next to every store the eligibility
// rule accepts, so a read that follows any of them is legitimate.
func (g *irGen) markBound(b *strings.Builder, name string) {
	slot, ok := g.boundSlots[name]
	if !ok || !g.boundFlags[name] {
		return
	}
	b.WriteString(fmt.Sprintf("  store i8 1, i8* %%%s\n", slot))
}

// checkBound emits the test in front of a read: an unwritten slot raises UnboundLocalError inside a
// function and NameError at module level, which is CPython's own split — the first says this frame
// owns the name and has no value for it yet, the second says nothing owns it at all — and both are
// typed raises, so `except UnboundLocalError:` catches them on either backend (ADR 0212's rule).
func (g *irGen) checkBound(b *strings.Builder, name string, sp Span) {
	slot, ok := g.boundSlots[name]
	if !ok || !g.boundFlags[name] {
		return
	}
	cur := g.newTmp()
	b.WriteString(fmt.Sprintf("  %s = load i8, i8* %%%s\n", cur, slot))
	bad := g.newTmp()
	b.WriteString(fmt.Sprintf("  %s = icmp eq i8 %s, 0\n", bad, cur))
	g.markI1(bad)
	class, msg := "UnboundLocalError", "cannot access local variable '"+name+"' where it is not associated with a value"
	if !g.inFunc {
		class, msg = "NameError", "name '"+name+"' is not defined"
	}
	g.branchRaise(b, bad, class, msg, sp, "unb")
}

// boundEligible decides, from the source alone, which names may carry a flag: those the body writes
// through a form this file hooks, and never through a form it does not. Both directions are wrong
// answers if got wrong — a write that skips markBound makes a legitimate read trap — so the rule
// refuses rather than guesses.
func boundEligible(body []Stmt) (ok, bad map[string]bool) {
	ok, bad = map[string]bool{}, map[string]bool{}
	disqualify := func(names map[string]bool) {
		for n := range names {
			ok[n] = false
			bad[n] = true
		}
	}
	note := func(e Expr) {
		names := map[string]bool{}
		collectBoundTargets(e, names)
		for n := range names {
			if bad[n] {
				continue
			}
			ok[n] = true
		}
	}
	var walk func(st Stmt)
	walk = func(st Stmt) {
		switch n := st.(type) {
		case nil:
			return
		case *AssignStmt:
			if !scalarStoreValue(n.Value) {
				names := map[string]bool{}
				collectBoundTargets(n.Target, names)
				disqualify(names)
				return
			}
			note(n.Target)
		case *AugAssignStmt:
			// An augmented assignment reads as well as writes, at the store site we hook.
			names := map[string]bool{}
			collectBoundTargets(n.Target, names)
			for n := range names {
				if bad[n] {
					continue
				}
				ok[n] = true
			}
		case *ForStmt:
			// A loop header binds its variable in a store this file does not hook (and the unrolled
			// and counted variants bind it in different places again), so a name bound there is left
			// alone rather than flagged by a writer nobody set.
			names := map[string]bool{}
			collectBoundTargets(n.Var, names)
			disqualify(names)
			for _, s := range n.Body {
				walk(s)
			}
			for _, s := range n.Else {
				walk(s)
			}
		case *WithStmt:
			if n.As != nil {
				disqualify(map[string]bool{n.As.Value: true})
			}
			for _, s := range n.Body {
				walk(s)
			}
		case *FuncDef:
			// A nested def binds its own name, and its body is its own scope: neither belongs here.
			disqualify(map[string]bool{n.Name: true})
		case *ClassDef:
			disqualify(map[string]bool{n.Name: true})
		case *ImportStmt:
			disqualify(importBoundNamesForFlag(n))
		case *IfStmt:
			for _, s := range n.Then {
				walk(s)
			}
			for _, s := range n.Else {
				walk(s)
			}
		case *WhileStmt:
			for _, s := range n.Body {
				walk(s)
			}
			for _, s := range n.Else {
				walk(s)
			}
		case *TryStmt:
			for _, s := range n.Body {
				walk(s)
			}
			for _, arm := range n.Excepts {
				for _, s := range arm.Body {
					walk(s)
				}
			}
			for _, s := range n.Finally {
				walk(s)
			}
		case *MatchStmt:
			for _, c := range n.Cases {
				// A capture binds through bindPat, which sets the flag.
				note(c.Pattern)
				for _, orp := range c.Or {
					note(orp)
				}
				for _, s := range c.Body {
					walk(s)
				}
			}
		}
	}
	for _, st := range body {
		walk(st)
	}
	return ok, bad
}

// collectBoundTargets records the names a binding form writes: a plain name, a tuple or list of names,
// or a match pattern that captures by name.
func collectBoundTargets(e Expr, out map[string]bool) {
	var rec func(Expr)
	rec = func(e Expr) {
		switch n := e.(type) {
		case nil:
			return
		case *Name:
			out[n.Value] = true
		case *Tuple:
			for _, el := range n.Elems {
				rec(el)
			}
		case *ListLit:
			for _, el := range n.Elems {
				rec(el)
			}
		}
	}
	rec(e)
}

// scalarStoreValue is the shape test behind the eligibility rule: a value the scalar store site
// handles. A container or float assignment stores elsewhere (a handle store, or a `double` slot),
// which this file does not hook, so such a name is left as it was rather than flagged.
func scalarStoreValue(e Expr) bool {
	switch n := e.(type) {
	case *IntLit, *StrLit, *BoolLit, *NoneLit, *Name, *Index:
		return true
	case *BinOp:
		return scalarStoreValue(n.L) && scalarStoreValue(n.R)
	case *UnOp:
		return scalarStoreValue(n.X)
	case *Tuple:
		for _, el := range n.Elems {
			if !scalarStoreValue(el) {
				return false
			}
		}
		return true
	case *ListLit:
		// A list literal assigned to a name is a container: its store is a handle store and its
		// reads bypass the scalar load, so the name is left alone rather than half-flagged.
		return len(n.Elems) == 0 && false
	case *DictLit, *SetLit, *Comp, *FloatLit:
		return false
	}
	return false
}

func importBoundNamesForFlag(n *ImportStmt) map[string]bool {
	out := map[string]bool{}
	if n == nil || n.Module == "" {
		return out
	}
	// `import a.b.c` binds the first dotted segment, the way the module registry does.
	out[strings.SplitN(n.Module, ".", 2)[0]] = true
	return out
}
