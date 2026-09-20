package lang

// deadListAssignments returns the set of top-level variable names whose every
// top-level assignment is a list-literal and which are never read at top level.
// For such variables the heap list allocation is dead (the list is never
// observed), so the codegen skips the rt_alloc entirely.
//
// The analysis walks only top-level statements (descending into compound
// bodies like if/for/while/try/match) and deliberately does NOT descend into
// function/lambda bodies: in this codegen function bodies use their own
// shadowed locals and cannot read top-level globals.
func deadListAssignments(prog []Stmt) map[string]bool {
	reads := map[string]bool{}
	nonListAssign := map[string]bool{}

	var walkExpr func(e Expr)
	var walkStmt func(s Stmt)

	// walkExpr marks any Name used in expression position as a potential read.
	walkExpr = func(e Expr) {
		if e == nil {
			return
		}
		switch n := e.(type) {
		case *Name:
			reads[n.Value] = true
		case *ListLit:
			for _, el := range n.Elems {
				walkExpr(el)
			}
		case *DictLit:
			for _, k := range n.Keys {
				walkExpr(k)
			}
			for _, v := range n.Vals {
				walkExpr(v)
			}
		case *SetLit:
			for _, el := range n.Elems {
				walkExpr(el)
			}
		case *BinOp:
			walkExpr(n.L)
			walkExpr(n.R)
		case *UnOp:
			walkExpr(n.X)
		case *CondExpr:
			walkExpr(n.Cond)
			walkExpr(n.If)
			walkExpr(n.Else)
		case *Call:
			walkExpr(n.Fn)
			for _, a := range n.Args {
				walkExpr(a)
			}
		case *Attr:
			walkExpr(n.Obj)
		case *Index:
			walkExpr(n.Obj)
			walkExpr(n.Idx)
		case *Comp:
			for _, el := range n.Elems {
				walkExpr(el)
			}
			for _, k := range n.Keys {
				walkExpr(k)
			}
			for _, v := range n.Vals {
				walkExpr(v)
			}
		case *Generator:
			for _, el := range n.Elems {
				walkExpr(el)
			}
			walkExpr(n.Iter)
		case *Lambda:
			// function body: not descended (locals shadow globals)
		}
	}

	// walkStmt processes a top-level statement: it descends into compound
	// bodies but not into function definitions.
	walkStmt = func(s Stmt) {
		switch n := s.(type) {
		case *AssignStmt:
			if name, ok := n.Target.(*Name); ok {
				if _, isList := n.Value.(*ListLit); !isList {
					nonListAssign[name.Value] = true
				}
			}
			// a plain Name target is a write, not a read; only walk Attr/Index
			// targets (their object sub-expressions are reads).
			if _, ok := n.Target.(*Name); !ok {
				walkExpr(n.Target)
			}
			walkExpr(n.Value)
		case *ExprStmt:
			walkExpr(n.Expr)
		case *ReturnStmt:
			walkExpr(n.Expr)
		case *YieldStmt:
			walkExpr(n.Expr)
		case *IfStmt:
			walkExpr(n.Cond)
			for _, st := range n.Then {
				walkStmt(st)
			}
			for _, e := range n.Elifs {
				walkExpr(e.Cond)
				for _, st := range e.Then {
					walkStmt(st)
				}
			}
			for _, st := range n.Else {
				walkStmt(st)
			}
		case *WhileStmt:
			walkExpr(n.Cond)
			for _, st := range n.Body {
				walkStmt(st)
			}
			for _, st := range n.Else {
				walkStmt(st)
			}
		case *ForStmt:
			if n.Var != nil {
				// loop var is an assignment target (value is an element,
				// not a list literal) so the variable must keep its slot.
				nonListAssign[n.Var.Value] = true
			}
			walkExpr(n.Iter)
			for _, st := range n.Body {
				walkStmt(st)
			}
			for _, st := range n.Else {
				walkStmt(st)
			}
		case *TryStmt:
			for _, st := range n.Body {
				walkStmt(st)
			}
			for _, ec := range n.Excepts {
				if ec.Exn != nil {
					nonListAssign[ec.Exn.Value] = true
				}
				for _, st := range ec.Body {
					walkStmt(st)
				}
			}
			for _, st := range n.Finally {
				walkStmt(st)
			}
		case *MatchStmt:
			walkExpr(n.Subject)
			for _, c := range n.Cases {
				walkExpr(c.Pattern)
				for _, st := range c.Body {
					walkStmt(st)
				}
			}
		case *FuncDef:
			// function body: not descended
		case *ImportStmt, *BreakStmt, *PassStmt, *ContinueStmt:
			// no reads
		}
	}

	for _, st := range prog {
		walkStmt(st)
	}

	// A variable is a dead-list candidate iff every top-level assignment to it
	// is a list-literal and it is never read at top level.
	dead := map[string]bool{}
	for _, st := range prog {
		if as, ok := st.(*AssignStmt); ok {
			if name, ok2 := as.Target.(*Name); ok2 {
				if _, isList := as.Value.(*ListLit); isList && !reads[name.Value] && !nonListAssign[name.Value] {
					dead[name.Value] = true
				}
			}
		}
	}
	return dead
}
