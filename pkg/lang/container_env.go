package lang

// containerLiteralsOf answers, for the names of a program, the container literal a name is bound to
// exactly once and never mutated afterwards. It is the permission for reading a slot as a container:
// the builder wrote a tag for that slot out of that literal, so while nothing has changed the object,
// the tag the compiler remembers is the tag the object holds. A second binding, an item assignment,
// a mutating method or handing the container to a function that could mutate it takes the name out of
// this map, and the read is refused rather than trusted — which is the ADR 0233 rule applied to a
// promise instead of to a fact (roadmap L11.1, ADR 0241).
//
// The answer is deliberately conservative in one direction only: it says "not provable" far more than
// the object is genuinely mutated, because the cost of being wrong is a payload read back as a handle
// — a number wearing another object's bits — and that is the class of wrong answer this language has
// decided it does not ship.
func containerLiteralsOf(prog *Program) map[string]Expr {
	if prog == nil {
		return nil
	}
	env := &containerEnv{
		lits:  map[string]Expr{},
		binds: map[string]int{},
		mut:   map[string]bool{},
	}
	env.stmts(prog.Stmts)
	out := map[string]Expr{}
	for name, n := range env.binds {
		if n == 1 && !env.mut[name] {
			if lit, ok := env.lits[name]; ok {
				out[name] = lit
			}
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

type containerEnv struct {
	lits  map[string]Expr
	binds map[string]int
	mut   map[string]bool
}

// containerMutators are the container methods that change what a slot holds. A name any of them is
// called on loses its literal: after `xs.append([9])` the literal the binding named is no longer a
// picture of the object, and the tag the compiler would quote for slot 5 would be the tag of a slot
// that did not exist when the literal was written.
var containerMutators = map[string]bool{
	"append": true, "extend": true, "insert": true, "pop": true, "clear": true,
	"sort": true, "reverse": true, "add": true, "discard": true, "remove": true,
	"update": true, "setdefault": true, "difference_update": true, "intersection_update": true,
	"symmetric_difference_update": true,
}

func (e *containerEnv) mutateName(name string) {
	e.mut[name] = true
}

func (e *containerEnv) stmts(stmts []Stmt) {
	for _, s := range stmts {
		e.stmt(s)
	}
}

func (e *containerEnv) stmt(s Stmt) {
	switch n := s.(type) {
	case *AssignStmt:
		e.expr(n.Value)
		switch t := n.Target.(type) {
		case *Name:
			e.binds[t.Value]++
			if isContainerLiteralExpr(n.Value) {
				e.lits[t.Value] = n.Value
			} else {
				// Rebound to something the compiler cannot see, so whatever literal the name used
				// to describe is no longer the object it names.
				delete(e.lits, t.Value)
			}
		case *Index:
			// `xs[0] = [9]` writes a slot: the literal no longer answers a question about it.
			if nm, ok := baseName(t); ok {
				e.mutateName(nm)
			}
		case *Attr:
			if nm, ok := t.Obj.(*Name); ok {
				e.mutateName(nm.Value)
			}
		}
		e.expr(n.Target)
	case *AugAssignStmt:
		e.expr(n.Value)
		if nm, ok := n.Target.(*Name); ok {
			e.binds[nm.Value]++
			delete(e.lits, nm.Value)
		} else if ix, ok := n.Target.(*Index); ok {
			if nm, ok := baseName(ix); ok {
				e.mutateName(nm)
			}
		}
	case *ExprStmt:
		e.expr(n.Expr)
	case *IfStmt:
		e.expr(n.Cond)
		e.stmts(n.Then)
		e.stmts(n.Else)
	case *WhileStmt:
		e.expr(n.Cond)
		e.stmts(n.Body)
		e.stmts(n.Else)
	case *ForStmt:
		e.expr(n.Iter)
		e.stmts(n.Body)
		e.stmts(n.Else)
	case *TryStmt:
		e.stmts(n.Body)
		for _, cl := range n.Excepts {
			e.stmts(cl.Body)
		}
		e.stmts(n.Finally)
	case *MatchStmt:
		e.expr(n.Subject)
		for _, c := range n.Cases {
			e.stmts(c.Body)
		}
	case *WithStmt:
		e.expr(n.Expr)
		e.stmts(n.Body)
	case *FuncDef:
		e.stmts(n.Body)
	case *ClassDef:
		e.stmts(n.Body)
	case *ReturnStmt:
		e.expr(n.Expr)
	case *RaiseStmt:
		e.expr(n.Expr)
	case *YieldStmt:
		e.expr(n.Expr)
	case *YieldFromStmt:
		e.expr(n.Expr)
	}
}

// expr looks for the two things a scan of an expression can learn about a container: a mutating
// method called on it, and the container handed to something outside this analysis that could mutate
// it. Everything else about an expression is irrelevant to whether its slots still hold what the
// literal said.
func (e *containerEnv) expr(x Expr) {
	switch n := x.(type) {
	case nil:
	case *Call:
		if at, ok := n.Fn.(*Attr); ok {
			if nm, ok := at.Obj.(*Name); ok {
				if at.Name != nil && containerMutators[at.Name.Value] {
					e.mutateName(nm.Value)
				}
			}
			e.expr(at.Obj)
		} else if id, ok := n.Fn.(*Name); ok && !isReadOnlyBuiltin(id.Value) {
			// A container passed to a function this pass cannot see is a container that function
			// could append to, sort or clear: the literal stops being a promise about it.
			for _, a := range n.Args {
				if nm, ok := a.(*Name); ok {
					e.mutateName(nm.Value)
				}
			}
		}
		for _, a := range n.Args {
			e.expr(a)
		}
	case *BinOp:
		e.expr(n.L)
		e.expr(n.R)
	case *UnOp:
		e.expr(n.X)
	case *CondExpr:
		e.expr(n.Cond)
		e.expr(n.If)
		e.expr(n.Else)
	case *ListLit:
		for _, el := range n.Elems {
			e.expr(el)
		}
	case *SetLit:
		for _, el := range n.Elems {
			e.expr(el)
		}
	case *DictLit:
		for _, k := range n.Keys {
			e.expr(k)
		}
		for _, v := range n.Vals {
			e.expr(v)
		}
	case *Tuple:
		for _, el := range n.Elems {
			e.expr(el)
		}
	case *Index:
		e.expr(n.Obj)
		e.expr(n.Idx)
	case *Attr:
		e.expr(n.Obj)
	case *Comp:
		e.expr(n.Iter)
		e.expr(n.Cond)
		for _, el := range n.Elems {
			e.expr(el)
		}
		for _, k := range n.Keys {
			e.expr(k)
		}
		for _, v := range n.Vals {
			e.expr(v)
		}
	}
}

// baseName is the variable a subscript chain hangs off: the answer for `xs[0][1]` is xs.
func baseName(ix *Index) (string, bool) {
	switch o := ix.Obj.(type) {
	case *Name:
		return o.Value, true
	case *Index:
		return baseName(o)
	}
	return "", false
}

// isContainerLiteralExpr is the question the binding line asks: is this value a container spelled
// out, so a slot of it can be read by looking rather than by asking the object.
func isContainerLiteralExpr(e Expr) bool {
	switch e.(type) {
	case *ListLit, *DictLit, *SetLit:
		return true
	}
	_, ok := emptyContainerLiteral(e)
	return ok
}

// isReadOnlyBuiltin names the builtins that cannot mutate an argument, which is the line the alias
// scan draws: an unknown callee could sort a list it was handed, `print` cannot.
func isReadOnlyBuiltin(name string) bool {
	switch name {
	case "print", "len", "str", "repr", "int", "float", "bool", "type", "abs", "min", "max",
		"sum", "any", "all", "sorted", "reversed", "enumerate", "zip", "range", "list", "dict",
		"set", "tuple", "ord", "chr", "round", "floor", "ceil", "sqrt", "pow", "divmod", "map",
		"filter", "input", "open", "close", "write", "read", "format", "hash", "id", "isinstance":
		return true
	}
	return false
}
