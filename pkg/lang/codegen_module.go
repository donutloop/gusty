// Module-level state: the environment a module's slots carry, the name each slot is stored under, and the
// refusal a program that would change that state at run time gets.
package lang

import (
	"fmt"
	"strings"
)

// gcStoreHandle stores a heap handle into a variable's slot and re-registers that
// slot as a root of the currently open frame (ADR 0181).
//
// The re-registration is not redundant. Rebinding a variable to a scalar tags its
// root entry dead (rt_root_clear), so the *next* container binding — say `s = {1, 2}`
// after `s = 5` — would otherwise leave a live container invisible to the collector,
// which recycles it while the program still uses it (observed as a set whose insert
// loop never terminated: the object under the variable had been reused). Because the
// runtime dedups within a frame, pushing on every handle store costs a scan of a
// handful of entries and cannot grow the stack.
// storeModuleScalar emits the store that binds a MODULE-level name to an `i32` value, and reports
// whether it emitted one. It exists because the read road (`g.value` on a `*Name`) prefers
// `g.moduleSlots[name]` — the `@gy_mod_name` global that a compiled body reads when the module also
// rebinds the name — so every road that BINDS that same name at module level has to write the same
// slot or the program reads a value that no statement produced.
//
// Before ADR 0351 only the plain-assignment road asked the question, and the two roads that did not
// each gave a different face to the same defect: an unrolled `for` wrote its loop variable to a
// main-frame `%_y` that the reads never consulted (CPython's `10`, `20`, `20` became `2`, `2`, `2` at
// exit 0), and `y += 3` wrote an `%_y` that had never been allocated at all, which `llc` rejected —
// exit 2, the code the exit-code contract reserves for a compiler bug (roadmap Gap R.49).
//
// A name with no module slot is the ordinary case and returns false: the caller keeps its own alloca,
// which is what a function body's local has always been and what a module temporary still is.
func (g *irGen) storeModuleScalar(b *strings.Builder, name, val string) bool {
	if g.inFunc {
		return false
	}
	sym, ok := g.moduleSlots[name]
	if !ok {
		return false
	}
	b.WriteString(fmt.Sprintf("  store i32 %s, i32* @%s\n", val, sym))
	return true
}

// noteUnlowered records the first expression a helper could not lower. It shares the field the
// method emitter uses (ADR 0223) because the rule is the same one: an emission path with no error
// channel of its own reports into the generator, and GenerateIR refuses rather than shipping IR it
// knows to be wrong.
// moduleEnvFor decides, once per program, what a compiled function body may know about the module
// it sits in. A name bound exactly once at module level to a literal, and never rebound by any
// module-level statement, is a value: inlining it is sound and needs no runtime slot. Everything
// else the module binds stays refused -- with a message that says so -- because main's slots are
// stack allocas a callee cannot lawfully read. Assignments inside a function body are not module
// writes (a binding inside a body is local, ADR 0220), so they neither disqualify a constant nor
// make one mutable.
func moduleEnvFor(prog *Program) (map[string]Expr, map[string]bool) {
	consts := map[string]Expr{}
	all := map[string]bool{}
	if prog == nil {
		return consts, all
	}
	literal := func(e Expr) bool {
		switch e.(type) {
		case *IntLit, *StrLit, *BoolLit, *NoneLit:
			return true
		}
		return false
	}
	disqualify := func(names map[string]bool) {
		for n := range names {
			delete(consts, n)
			all[n] = true
		}
	}
	for _, st := range prog.Stmts {
		switch n := st.(type) {
		case *FuncDef, *ClassDef:
			continue // a body's bindings are local to it
		case *AssignStmt:
			targets := map[string]bool{}
			moduleBindingNames(n, targets)
			if nm, ok := n.Target.(*Name); ok && len(targets) == 1 && literal(n.Value) && !all[nm.Value] {
				consts[nm.Value] = n.Value
				all[nm.Value] = true
				continue
			}
			disqualify(targets)
		default:
			targets := map[string]bool{}
			moduleBindingNames(st, targets)
			disqualify(targets)
		}
	}
	return consts, all
}

// moduleSlotNames chooses which module-level names need real storage: those that some function,
// method or closure body reads, that the module rebinds (a name bound once to a literal is a value and
// needs no slot at all), and whose module-level assignments are all scalar. A container is left out on
// purpose: reading a module list as a handle without the container operations behind it would trade an
// honest refusal for a half-working answer (Gap R.35's remaining half, ADR 0227).
func moduleSlotNames(prog *Program, consts map[string]Expr, all map[string]bool) map[string]string {
	slots := map[string]string{}
	if prog == nil {
		return slots
	}
	// What scalar shape does each module name hold?
	scalar := map[string]bool{}
	seenBefore := map[string]bool{}
	for _, st := range prog.Stmts {
		as, ok := st.(*AssignStmt)
		if !ok {
			continue
		}
		nm, ok := as.Target.(*Name)
		if !ok {
			continue
		}
		// Every assignment to the name must hold a scalar: an absent map entry reads false, so the
		// first assignment has to initialise it rather than AND against nothing.
		if !seenBefore[nm.Value] {
			scalar[nm.Value] = scalarValue(as.Value)
			seenBefore[nm.Value] = true
		} else {
			scalar[nm.Value] = scalar[nm.Value] && scalarValue(as.Value)
		}
	}
	// Which names do bodies read? — asked per SCOPE, not per program.
	//
	// A `def`'s parameter is a binding of that body, and a body's own assignment shadows the module for
	// the whole body exactly as it does in the reference. So `def g(y): return y` reads its PARAMETER and
	// says nothing whatsoever about a module-level `y`. Asked without the scope, this walk handed every
	// name that any body mentioned a module global (`@gy_mod_y`) — including the names the module itself
	// binds to a main-frame alloca — and the read road then consulted a slot no store ever visited:
	// `y = 2` / `for y in [10, 20]: print(y)` printed `2` twice at the exit code of success, and `y += 3`
	// stored to an `%_y` that had never been allocated and left `llc` rejecting the module at exit 2
	// (roadmap Gap R.49, ADR 0351). A name still gets its global when ANY body reads it without binding
	// it — `def g(y): pass` and `def h(): return y` in one program both need what `h` asks for.
	reads := map[string]bool{}
	var walk func(node interface{}, shadow map[string]bool)
	var mark func(e Expr, shadow map[string]bool)
	mark = func(e Expr, shadow map[string]bool) {
		switch n := e.(type) {
		case *Name:
			if all[n.Value] && !shadow[n.Value] {
				reads[n.Value] = true
			}
		case *BinOp:
			mark(n.L, shadow)
			mark(n.R, shadow)
		case *UnOp:
			mark(n.X, shadow)
		case *Index:
			mark(n.Obj, shadow)
			mark(n.Idx, shadow)
		case *Call:
			mark(n.Fn, shadow)
			for _, a := range n.Args {
				mark(a, shadow)
			}
		case *Attr:
			mark(n.Obj, shadow)
		case *Tuple:
			for _, el := range n.Elems {
				mark(el, shadow)
			}
		}
	}
	// outerScope copies the enclosing body's bindings into the set the inner body shadows with; a
	// nested `def` therefore shadows what encloses it as well as what it binds itself, which is the
	// scoping the reference applies (and the closures that depend on reading *through* it are refused,
	// roadmap Gap R.68).
	outerScope := func(shadow map[string]bool) map[string]bool {
		inner := make(map[string]bool, len(shadow)+2)
		for k := range shadow {
			inner[k] = true
		}
		return inner
	}
	walk = func(node interface{}, shadow map[string]bool) {
		switch n := node.(type) {
		case *FuncDef:
			inner := outerScope(shadow)
			for _, pa := range n.Params {
				inner[pa.Name] = true
			}
			for _, st := range n.Body {
				moduleBindingNames(st, inner)
			}
			// A default expression is evaluated where the `def` statement is, not in the body, so it
			// is asked of the enclosing scope: `STEP = 1` / `def f(a=STEP)` reads the module.
			for _, pa := range n.Params {
				if pa.Default != nil {
					mark(pa.Default, shadow)
				}
			}
			for _, st := range n.Body {
				walk(st, inner)
			}
		case *ClassDef:
			// A class body's own names are attributes of the class, not bindings the methods shadow —
			// the reference does not let a method read a class-level name either — so the methods are
			// walked in the scope their own bodies create, over the shadow they inherit.
			for _, st := range n.Body {
				walk(st, shadow)
			}
		case []Stmt:
			for _, st := range n {
				walk(st, shadow)
			}
		case *AssignStmt:
			mark(n.Value, shadow)
		case *AugAssignStmt:
			mark(n.Value, shadow)
			mark(n.Target, shadow)
		case *ExprStmt:
			mark(n.Expr, shadow)
		case *ReturnStmt:
			mark(n.Expr, shadow)
		case *IfStmt:
			mark(n.Cond, shadow)
			walk(n.Then, shadow)
			walk(n.Else, shadow)
		case *WhileStmt:
			mark(n.Cond, shadow)
			walk(n.Body, shadow)
			walk(n.Else, shadow)
		case *ForStmt:
			mark(n.Iter, shadow)
			// The loop variable is a binding of the scope the loop lives in, so inside the body it is
			// local: `for i in xs: print(i)` reads the element, not a module `i` of the same name.
			body := outerScope(shadow)
			moduleBindingNames(n, body)
			walk(n.Body, body)
			walk(n.Else, shadow)
		case *TryStmt:
			body := outerScope(shadow)
			moduleBindingNames(n, body)
			walk(n.Body, body)
			for _, arm := range n.Excepts {
				walk(arm.Body, body)
			}
			walk(n.Finally, shadow)
		case *WithStmt:
			mark(n.Expr, shadow)
			body := outerScope(shadow)
			moduleBindingNames(n, body)
			walk(n.Body, body)
		case *MatchStmt:
			mark(n.Subject, shadow)
			body := outerScope(shadow)
			moduleBindingNames(n, body)
			for _, c := range n.Cases {
				walk(c.Body, body)
			}
		}
	}
	for _, st := range prog.Stmts {
		switch st.(type) {
		case *FuncDef, *ClassDef:
			walk(st, nil)
		}
	}
	for nm := range reads {
		if _, isConst := consts[nm]; isConst {
			continue // a value needs no slot
		}
		if !scalar[nm] {
			continue
		}
		slots[nm] = irSymbol("gy_mod_" + nm)
	}
	return slots
}
func scalarValue(e Expr) bool {
	switch n := e.(type) {
	case *IntLit, *StrLit, *BoolLit, *NoneLit:
		return true
	case *BinOp:
		return scalarValue(n.L) && scalarValue(n.R)
	case *UnOp:
		return scalarValue(n.X)
	}
	return false
}

// moduleBindingNames records the names a module-level statement *binds* -- the left-hand sides, the
// loop variable, the `with ... as` name, and whatever a nested if/while/try body binds. It is a
// binding collector, not a use collector: an expression that merely reads a name says nothing about
// whether that name can change.
func moduleBindingNames(st Stmt, out map[string]bool) {
	if st == nil {
		return
	}
	var bind func(e Expr)
	bind = func(e Expr) {
		switch t := e.(type) {
		case *Name:
			out[t.Value] = true
		case *Tuple:
			for _, el := range t.Elems {
				bind(el)
			}
		case *ListLit:
			for _, el := range t.Elems {
				bind(el)
			}
		}
	}
	switch n := st.(type) {
	case *AssignStmt:
		bind(n.Target)
	case *AugAssignStmt:
		bind(n.Target)
	case *ForStmt:
		bind(n.Var)
		for _, s := range n.Body {
			moduleBindingNames(s, out)
		}
		for _, s := range n.Else {
			moduleBindingNames(s, out)
		}
	case *WhileStmt:
		for _, s := range n.Body {
			moduleBindingNames(s, out)
		}
		for _, s := range n.Else {
			moduleBindingNames(s, out)
		}
	case *IfStmt:
		for _, s := range n.Then {
			moduleBindingNames(s, out)
		}
		for _, s := range n.Else {
			moduleBindingNames(s, out)
		}
	case *TryStmt:
		for _, s := range n.Body {
			moduleBindingNames(s, out)
		}
		for _, arm := range n.Excepts {
			for _, s := range arm.Body {
				moduleBindingNames(s, out)
			}
		}
		for _, s := range n.Finally {
			moduleBindingNames(s, out)
		}
	case *WithStmt:
		if n.As != nil {
			out[n.As.Value] = true
		}
		for _, s := range n.Body {
			moduleBindingNames(s, out)
		}
	case *MatchStmt:
		for _, c := range n.Cases {
			for _, s := range c.Body {
				moduleBindingNames(s, out)
			}
		}
	}
}

// moduleStateErr is the honest answer when a body refers to module-level *state* -- a container, or
// anything the module rebinds -- which a compiled body cannot reach. The two sites that used to answer
// "non-constant string" (for a plain list's .append) and "non-string variable" (for len of a list) are
// the ADR-0166 face of that gap: a claim that misdescribes the value sends the reader to a line they
// never wrote, which the record treats as worse than no claim at all (roadmap Gap R.38).
func (g *irGen) moduleStateErr(nm string) error {
	if !g.inFunc || nm == "" {
		return nil
	}
	if g.allocd[nm] || (g.funcLocals != nil && g.funcLocals[nm]) {
		return nil // the body binds it: this is a local, and its own rules apply
	}
	if g.moduleNames[nm] {
		return fmt.Errorf("codegen: %q is bound at module level, and a compiled function body cannot reach module-level containers or state that changes (CPython answers this program; compiled module globals are roadmap Gap R.35)", nm)
	}
	return nil
}
