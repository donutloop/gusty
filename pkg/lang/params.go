package lang

// Rebound parameters — a parameter is a local that starts out bound to an argument.
//
// Python-shaped programs rebind parameters constantly: an accumulator that
// decrements the number it was given, a clamp that overwrites its argument, a loop
// that reuses the parameter as its variable:
//
//	def acc(n):
//	    total = 0
//	    while n > 0:
//	        total = total + n
//	        n = n - 1
//	    return total
//
// The compiled backend registered each parameter as its incoming argument register
// and resolved every reference from that register. The assignment below such a read
// allocated a *second* slot that nothing ever read, so `acc(4)` never terminated —
// its condition compared the argument, forever — and the quieter form
//
//	def bump(n):
//	    n = n + 1
//	    return n
//
// answered 0: the store happened, the read ignored it. Functions, methods and nested
// defs were all affected; only parameters rebound to a float, a string or a container
// worked, because those reads go through the kind maps rather than the register.
//
// The fix is what an SSA-form compiler does with a variable assigned after its
// definition: copy the incoming register into a real stack slot at the top of the
// body and make that slot authoritative for the whole body. Every path then reads
// initialized storage — including a read *textually before* the first assignment,
// which is why the copy belongs at the entry and not lazily at the assignment.
//
// Two rules keep the change honest:
//
//   - A parameter is copied in only when every value bound to it in the body is a
//     plain scalar. A parameter rebound to a float, a string or a container already
//     reads through its kind map (floatVars / strVals / listVars), which loads the
//     slot; making the entry decide that slot's LLVM type would duplicate a decision
//     the per-kind paths make at the store, and those shapes are not broken today.
//   - Extra copies are harmless, missing ones are not: a copy-in for a parameter that
//     turns out never to be assigned just costs one alloca, so the scan is deliberately
//     generous about what counts as a binding (a `for` over a parameter name, a `with`
//     target, a `case y:` capture, a comprehension's loop variable).
//
// The single float question is asked of the caller's own predicate — in codegen,
// (*irGen).isFloat — so the decision here can never disagree with the store type the
// assignment path will choose.

// reboundParams returns the parameters of fd whose own body rebinds them to plain
// scalar values, so the caller gives them an entry slot instead of letting them stay
// as incoming argument registers. isFloat is the caller's float predicate; nil means
// "treat every binding as scalar".
//
// Nested `def`s, `lambda`s and class bodies are not walked: their parameters and
// bodies are a separate scope with their own registers, and each gets this treatment
// from its own prologue.
func reboundParams(fd *FuncDef, isFloat func(Expr) bool) map[string]bool {
	if fd == nil || len(fd.Params) == 0 {
		return nil
	}
	bindings := map[string][]Expr{}
	scanRebinds(fd.Body, bindings)
	out := map[string]bool{}
	for _, p := range fd.Params {
		bs, ok := bindings[p.Name]
		if !ok || len(bs) == 0 {
			continue
		}
		scalar := true
		for _, v := range bs {
			if v == nil {
				// A binding with no observable value (a loop variable, a `case`
				// capture): codegen decides the kind where it writes it, and an
				// entry copy of the argument is correct whatever that turns out to
				// be as long as it is not a float slot.
				continue
			}
			if isFloat != nil && isFloat(v) {
				scalar = false
				break
			}
		}
		if scalar {
			out[p.Name] = true
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// scanRebinds records every name a statement list binds, without descending into a
// nested function or lambda, whose parameters shadow the outer ones.
func scanRebinds(list []Stmt, into map[string][]Expr) {
	var walk func([]Stmt)
	rec := func(e Expr, val Expr) {
		nm, ok := e.(*Name)
		if !ok {
			return
		}
		into[nm.Value] = append(into[nm.Value], val)
	}
	var bindPattern func(Expr)
	bindPattern = func(p Expr) {
		// `case y:` binds y; a destructuring pattern binds through its parts.
		switch n := p.(type) {
		case *Name:
			if n.Value != "_" {
				rec(n, nil)
			}
		case *Tuple:
			for _, el := range n.Elems {
				bindPattern(el)
			}
		case *ListLit:
			for _, el := range n.Elems {
				bindPattern(el)
			}
		case *Call: // `case Point(x, y):` — the arguments are patterns
			for _, a := range n.Args {
				bindPattern(a)
			}
		}
	}
	walk = func(stmts []Stmt) {
		for _, st := range stmts {
			switch s := st.(type) {
			case *AssignStmt:
				rec(s.Target, s.Value)
				// `[n for n in ...]` binds n inside the assignment's *value*, and
				// codegen reuses that name as a local of this function.
				scanCompBinds(s.Value, into)
				switch t := s.Target.(type) {
				case *ListLit:
					for _, el := range t.Elems {
						rec(el, nil)
					}
				case *Tuple:
					for _, el := range t.Elems {
						rec(el, nil)
					}
				}
			case *AugAssignStmt:
				rec(s.Target, s.Value)
			case *ForStmt:
				rec(s.Var, nil)
				walk(s.Body)
				walk(s.Else)
			case *WithStmt:
				if s.As != nil {
					rec(s.As, nil)
				}
				walk(s.Body)
			case *IfStmt:
				walk(s.Then)
				for _, e := range s.Elifs {
					walk(e.Then)
				}
				walk(s.Else)
			case *WhileStmt:
				walk(s.Body)
				walk(s.Else)
			case *TryStmt:
				walk(s.Body)
				for _, e := range s.Excepts {
					if e.Exn != nil {
						// `except E as e` rebinds the name; recording the class name
						// too costs an unused alloca at worst, and missing a real
						// rebind is the expensive direction.
						into[e.Exn.Value] = append(into[e.Exn.Value], nil)
					}
					walk(e.Body)
				}
				walk(s.Finally)
			case *MatchStmt:
				for _, c := range s.Cases {
					bindPattern(c.Pattern)
					for _, o := range c.Or {
						bindPattern(o)
					}
					walk(c.Body)
				}
			case *ExprStmt:
				scanCompBinds(s.Expr, into)
			case *ReturnStmt:
				if s.Expr != nil {
					scanCompBinds(s.Expr, into)
				}
			}
			// *FuncDef, *Lambda and *ClassDef are deliberately not descended into:
			// that scope owns those names.
		}
	}
	walk(list)
}

// scanCompBinds records the loop variable of a comprehension inside an expression.
// Codegen reuses that name as a local of the enclosing function, so a parameter of
// the same name is overwritten just the same.
func scanCompBinds(e Expr, into map[string][]Expr) {
	var walk func(Expr)
	walk = func(x Expr) {
		switch n := x.(type) {
		case *Comp:
			if n.ForVar != nil {
				into[n.ForVar.Value] = append(into[n.ForVar.Value], nil)
			}
			for _, el := range n.Elems {
				walk(el)
			}
			for _, k := range n.Keys {
				walk(k)
			}
			for _, v := range n.Vals {
				walk(v)
			}
			walk(n.Iter)
			walk(n.Cond)
		case *BinOp:
			walk(n.L)
			walk(n.R)
		case *UnOp:
			walk(n.X)
		case *Call:
			walk(n.Fn)
			for _, a := range n.Args {
				walk(a)
			}
		case *Index:
			walk(n.Obj)
			walk(n.Idx)
		case *CondExpr:
			walk(n.Cond)
			walk(n.If)
			walk(n.Else)
		case *Attr:
			walk(n.Obj)
		case *Tuple:
			for _, el := range n.Elems {
				walk(el)
			}
		case *ListLit:
			for _, el := range n.Elems {
				walk(el)
			}
		}
	}
	walk(e)
}

// floatFromSyntax is the stateless half of the float question codegen asks: a float
// literal, a true division, a sum or product with one, or a node the checker inferred
// as float. (*irGen).isFloat adds the compiler's own knowledge — which locals are
// floats — and is what the compiled path passes to reboundParams; this is the version
// the analysis can be tested against, and the fallback when there is no compiler.
func floatFromSyntax(e Expr) bool {
	switch n := e.(type) {
	case *FloatLit:
		return true
	case *UnOp:
		return n.Op == "-" && floatFromSyntax(n.X)
	case *BinOp:
		if n.Op == "/" {
			return true
		}
		switch n.Op {
		case "+", "-", "*", "%", "//":
			return floatFromSyntax(n.L) || floatFromSyntax(n.R)
		}
		return false
	case *Name:
		return n.Ty == "float"
	case *Call:
		return n.Ty == "float"
	case *Index:
		return n.Ty == "float"
	case *CondExpr:
		return floatFromSyntax(n.If) || floatFromSyntax(n.Else)
	}
	return false
}
