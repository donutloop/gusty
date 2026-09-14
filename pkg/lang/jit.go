package lang

// Evaluator is a small AST interpreter used by --eval and the REPL.
// It evaluates integer-typed expressions deterministically without needing
// an LLVM JIT engine (the go-llvm fork is bindings-only, no ExecutionEngine).
type Evaluator struct {
	Vars     map[string]int64
	funcs    map[string]*FuncDef
	heap     map[int64]*obj
	nextID   int64
	classIDs map[string]int64
	yieldList int64 // list handle accumulating yields (0 = not in generator)
}

// obj is a heap value: a class, an instance, or a bound/unbound method.
type obj struct {
	kind  string           // "class" | "instance" | "method" | "list"
	class string           // class name (instance/method)
	attrs map[string]int64 // instance attrs or class method-handle ids
	fn    *FuncDef         // method body (kind=method)
	mname string           // method name (kind=method)
	recv  int64            // bound receiver id (0 = unbound)
	elems []int64          // list elements (kind=list)
}

func (e *Evaluator) allocObj(kind string) int64 {
	e.nextID++
	id := e.nextID
	e.heap[id] = &obj{kind: kind, attrs: map[string]int64{}}
	return id
}

func NewEvaluator() *Evaluator { return &Evaluator{Vars: map[string]int64{}, funcs: map[string]*FuncDef{}, heap: map[int64]*obj{}, classIDs: map[string]int64{}} }

// EvalProgram evaluates prog's top-level statements and returns the value of
// the final expression statement (or last assignment). It returns an error on
// unsupported constructs.
func (e *Evaluator) EvalProgram(prog *Program) (int64, error) {
	var last int64
	for _, st := range prog.Stmts {
		switch s := st.(type) {
		case *ClassDef:
			classID := e.allocObj("class")
			e.classIDs[s.Name] = classID
			cls := e.heap[classID]
			for _, m := range s.Body {
				fd, ok := m.(*FuncDef)
				if !ok {
					continue
				}
				methodID := e.allocObj("method")
				mo := e.heap[methodID]
				mo.class = s.Name
				mo.mname = fd.Name
				mo.fn = fd
				cls.attrs[fd.Name] = methodID
			}
		case *FuncDef:
			e.funcs[s.Name] = s
			continue
		case *IfStmt:
		cond, err := e.eval(s.Cond)
		if err != nil {
			return 0, err
		}
		if cond != 0 {
			rv, err := e.evalBody(s.Then)
			if err != nil {
				return 0, err
			}
			last = rv
		} else if s.Else != nil {
			rv, err := e.evalBody(s.Else)
			if err != nil {
				return 0, err
			}
			last = rv
		}
		continue
	case *MatchStmt:
		sub, err := e.eval(s.Subject)
		if err != nil {
			return 0, err
		}
		for _, c := range s.Cases {
			matches := true
			if pn, ok := c.Pattern.(*Name); !ok || pn.Value != "_" {
				pv, err := e.eval(c.Pattern)
				if err != nil {
					return 0, err
				}
				matches = pv == sub
			}
			if matches {
				rv, err := e.evalBody(c.Body)
				if err != nil {
					return 0, err
				}
				last = rv
				break
			}
		}
		continue
	case *TryStmt:
		_, bodyErr := e.evalBody(s.Body)
		if bodyErr != nil {
			caught := false
			for _, ec := range s.Excepts {
				if ec.Exn == nil || ec.Exn.Value == "Exception" {
					_, err2 := e.evalBody(ec.Body)
					if err2 != nil {
						return 0, err2
					}
					caught = true
					break
				}
			}
			if !caught {
				return 0, bodyErr
			}
		}
		if len(s.Finally) > 0 {
			_, err := e.evalBody(s.Finally)
			if err != nil {
				return 0, err
			}
		}
	case *WhileStmt:
		completed := true
		for {
			cond, err := e.eval(s.Cond)
			if err != nil {
				return 0, err
			}
			if cond == 0 {
				break
			}
			rv, err := e.evalBody(s.Body)
			if err != nil {
				if ls, ok := err.(*loopSignal); ok {
					if ls.kind == "break" {
						completed = false
						break
					}
					continue
				}
				return 0, err
			}
			last = rv
		}
		if completed {
			rv, err := e.evalBody(s.Else)
			if err != nil {
				return 0, err
			}
			last = rv
		}
		continue
	case *ForStmt:
		isRange := false
		if c, ok := s.Iter.(*Call); ok {
			if n, ok2 := c.Fn.(*Name); ok2 && n.Value == "range" {
				isRange = true
			}
		}
		completed := true
		if n := s.Var; n != nil {
			if !isRange {
				itV, err := e.eval(s.Iter)
				if err != nil {
					return 0, err
				}
				if o, ok := e.heap[itV]; ok && o.kind == "list" {
					for _, el := range o.elems {
						e.Vars[n.Value] = el
						rv, err := e.evalBody(s.Body)
						if err != nil {
							if ls, ok := err.(*loopSignal); ok {
								if ls.kind == "break" {
									completed = false
									break
								}
								continue
							}
							return 0, err
						}
						last = rv
					}
				} else {
					start, stop, err := e.rangeBounds(s.Iter)
					if err != nil {
						return 0, err
					}
					for i := start; i < stop; i++ {
						e.Vars[n.Value] = i
						rv, err := e.evalBody(s.Body)
						if err != nil {
							if ls, ok := err.(*loopSignal); ok {
								if ls.kind == "break" {
									completed = false
									break
								}
								continue
							}
							return 0, err
						}
						last = rv
					}
				}
			} else {
				start, stop, err := e.rangeBounds(s.Iter)
				if err != nil {
					return 0, err
				}
				for i := start; i < stop; i++ {
					e.Vars[n.Value] = i
					rv, err := e.evalBody(s.Body)
					if err != nil {
						if ls, ok := err.(*loopSignal); ok {
							if ls.kind == "break" {
								completed = false
								break
							}
							continue
						}
						return 0, err
					}
					last = rv
				}
			}
		}
		if completed {
			rv, err := e.evalBody(s.Else)
			if err != nil {
				return 0, err
			}
			last = rv
		}
	case *AssignStmt:
			v, err := e.eval(s.Value)
			if err != nil {
				return 0, err
			}
			if n, ok := s.Target.(*Name); ok {
				e.Vars[n.Value] = v
				last = v
			}
			if a, ok := s.Target.(*Attr); ok {
				objV, err := e.eval(a.Obj)
				if err != nil {
					return 0, err
				}
				o, ok := e.heap[objV]
				if ok && o.kind == "instance" {
					o.attrs[a.Name.Value] = v
					last = v
				}
			}
		case *ExprStmt:
			v, err := e.eval(s.Expr)
			if err != nil {
				return 0, err
			}
			last = v
		case *ReturnStmt:
			if s.Expr != nil {
				v, err := e.eval(s.Expr)
				if err != nil {
					return 0, err
				}
				return v, nil
			}
		case *YieldStmt:
			v, err := e.eval(s.Expr)
			if err != nil {
				return 0, err
			}
			if e.yieldList != 0 {
				if o, ok := e.heap[e.yieldList]; ok {
					o.elems = append(o.elems, v)
				}
				last = v
				continue
			}
			last = v
			continue
		case *RaiseStmt:
			return 0, &EvalError{Msg: "raised"}
		case *BreakStmt:
			return 0, &loopSignal{kind: "break"}
		case *ContinueStmt:
			return 0, &loopSignal{kind: "continue"}
		default:
			return 0, &EvalError{Msg: "unsupported statement for eval"}
		}
	}
	return last, nil
}

func (e *Evaluator) eval(x Expr) (int64, error) {
	switch n := x.(type) {
	case *IntLit:
		return n.Value, nil
	case *BoolLit:
		if n.Value {
			return 1, nil
		}
		return 0, nil
	case *NoneLit:
		return 0, nil
	case *Name:
		if v, ok := e.Vars[n.Value]; ok {
			return v, nil
		}
		if id, ok := e.classIDs[n.Value]; ok {
			return id, nil
		}
		return 0, &EvalError{Msg: "undefined name " + n.Value}
	case *BinOp:
		return e.evalBin(n)
	case *UnOp:
		v, err := e.eval(n.X)
		if err != nil {
			return 0, err
		}
		switch n.Op {
		case "-":
			return -v, nil
		case "not":
			if v == 0 {
				return 1, nil
			}
			return 0, nil
		}
		return 0, &EvalError{Msg: "unsupported unary " + n.Op}
	case *Attr:
		objV, err := e.eval(n.Obj)
		if err != nil {
			return 0, err
		}
		o, ok := e.heap[objV]
		if !ok {
			return 0, &EvalError{Msg: "attribute access on non-object"}
		}
		if o.kind == "instance" {
			if v, ok := o.attrs[n.Name.Value]; ok {
				return v, nil
			}
			classID, ok := e.classIDs[o.class]
			if !ok {
				return 0, &EvalError{Msg: "unknown class " + o.class}
			}
			if mID, ok := e.heap[classID].attrs[n.Name.Value]; ok {
				e.heap[mID].recv = objV
				return mID, nil
			}
			return 0, &EvalError{Msg: "no attribute " + n.Name.Value}
		}
		if o.kind == "class" {
			if mID, ok := o.attrs[n.Name.Value]; ok {
				return mID, nil
			}
			return 0, &EvalError{Msg: "no method " + n.Name.Value}
		}
		return 0, &EvalError{Msg: "attribute access on method"}
	case *Call:
		return e.evalCall(n)
	case *ListLit:
		h := e.allocObj("list")
		o := e.heap[h]
		for _, el := range n.Elems {
			ev, err := e.eval(el)
			if err != nil {
				return 0, err
			}
			o.elems = append(o.elems, ev)
		}
		return h, nil
	default:
		return 0, &EvalError{Msg: "unsupported expression for eval"}
	}
}

func (e *Evaluator) evalBin(n *BinOp) (int64, error) {
	l, err := e.eval(n.L)
	if err != nil {
		return 0, err
	}
	r, err := e.eval(n.R)
	if err != nil {
		return 0, err
	}
	switch n.Op {
	case "+":
		return l + r, nil
	case "-":
		return l - r, nil
	case "*":
		return l * r, nil
	case "/", "//":
		if r == 0 {
			return 0, &EvalError{Msg: "division by zero"}
		}
		return l / r, nil
	case "%":
		if r == 0 {
			return 0, &EvalError{Msg: "division by zero"}
		}
		return l % r, nil
	case "==":
		if l == r {
			return 1, nil
		}
		return 0, nil
	case "!=":
		if l != r {
			return 1, nil
		}
		return 0, nil
	case "<":
		if l < r {
			return 1, nil
		}
		return 0, nil
	case "<=":
		if l <= r {
			return 1, nil
		}
		return 0, nil
	case ">":
		if l > r {
			return 1, nil
		}
		return 0, nil
	case ">=":
		if l >= r {
			return 1, nil
		}
		return 0, nil
	case "and":
		if l != 0 && r != 0 {
			return 1, nil
		}
		return 0, nil
	case "or":
		if l != 0 || r != 0 {
			return 1, nil
		}
		return 0, nil
	}
	return 0, &EvalError{Msg: "unsupported operator " + n.Op}
}

func (e *Evaluator) evalBody(stmts []Stmt) (int64, error) {
	return e.EvalProgram(&Program{Stmts: stmts})
}

func (e *Evaluator) rangeBounds(iter Expr) (int64, int64, error) {
	if c, ok := iter.(*Call); ok {
		if n, ok2 := c.Fn.(*Name); ok2 && n.Value == "range" && len(c.Args) == 2 {
			start, err := e.eval(c.Args[0])
			if err != nil {
				return 0, 0, err
			}
			stop, err := e.eval(c.Args[1])
			if err != nil {
				return 0, 0, err
			}
			return start, stop, nil
		}
	}
	stop, err := e.eval(iter)
	if err != nil {
		return 0, 0, err
	}
	return 0, stop, nil
}

// callMethod invokes a method body with self bound as a local.
func (e *Evaluator) callMethod(mo *obj, self int64, args []int64) (int64, error) {
	scope := map[string]int64{}
	scope["self"] = self
	// params[0] is the receiver `self`; bind the remaining params from args
	params := mo.fn.Params
	if len(params) > 0 {
		params = params[1:]
	}
	for i, p := range params {
		if i < len(args) {
			scope[p.Name] = args[i]
		}
	}
	old := e.Vars
	e.Vars = scope
	defer func() { e.Vars = old }()
	return e.evalBody(mo.fn.Body)
}

func (e *Evaluator) evalCall(n *Call) (int64, error) {
	// method call: obj.method(args) — Fn is an Attr resolving to a method
	if _, ok := n.Fn.(*Attr); ok {
		// resolve the attribute/method reference via eval
		mID, err := e.eval(n.Fn)
		if err != nil {
			return 0, err
		}
		mo, ok := e.heap[mID]
		if ok && mo.kind == "method" {
			argVals := []int64{}
			for _, a := range n.Args {
				av, err := e.eval(a)
				if err != nil {
					return 0, err
				}
				argVals = append(argVals, av)
			}
			self := mo.recv
			if mo.recv == 0 && len(argVals) > 0 {
				self = argVals[0]
				argVals = argVals[1:]
			}
			return e.callMethod(mo, self, argVals)
		}
		return 0, &EvalError{Msg: "not a callable attribute"}
	}
	if name, ok := n.Fn.(*Name); ok {
		// class instantiation: Point(0,0)
		if classID, ok := e.classIDs[name.Value]; ok {
			instID := e.allocObj("instance")
			inst := e.heap[instID]
			inst.class = name.Value
			argVals := []int64{}
			for _, a := range n.Args {
				av, err := e.eval(a)
				if err != nil {
					return 0, err
				}
				argVals = append(argVals, av)
			}
			cls := e.heap[classID]
			if initID, ok := cls.attrs["__init__"]; ok {
				mo := e.heap[initID]
				mo.recv = instID
				_, err := e.callMethod(mo, instID, argVals)
				if err != nil {
					return 0, err
				}
			}
			return instID, nil
		}
	}
	if name, ok := n.Fn.(*Name); ok {
		if fd, ok2 := e.funcs[name.Value]; ok2 {
			if len(fd.Params) != len(n.Args) {
				return 0, &EvalError{Msg: "argument count mismatch for " + name.Value}
			}
			scope := map[string]int64{}
			for i, p := range fd.Params {
				av, err := e.eval(n.Args[i])
				if err != nil {
					return 0, err
				}
				scope[p.Name] = av
			}
			saved := e.Vars
			e.Vars = scope
			if containsYield(fd.Body) {
				genH := e.allocObj("list")
				prev := e.yieldList
				e.yieldList = genH
				_, err := e.evalBody(fd.Body)
				e.yieldList = prev
				e.Vars = saved
				if err != nil {
					return 0, err
				}
				return genH, nil
			}
			rv, err := e.evalBody(fd.Body)
			e.Vars = saved
			return rv, err
		}
		switch name.Value {
		case "print":
			for _, a := range n.Args {
				v, err := e.eval(a)
				if err != nil {
					return 0, err
				}
				_ = v
			}
			return 0, nil
		case "range":
			if len(n.Args) != 1 {
				return 0, &EvalError{Msg: "range expects 1 argument"}
			}
			return e.eval(n.Args[0])
		}
	}
	return 0, &EvalError{Msg: "unsupported call for eval"}
}

// EvalError is a runtime eval error.
type EvalError struct{ Msg string }

func (e *EvalError) Error() string { return "eval error: " + e.Msg }

// loopSignal carries break/continue control out of a loop body.
type loopSignal struct{ kind string }

func (l *loopSignal) Error() string { return "loop signal: " + l.kind }

// EvalExpr compiles src and evaluates it, returning the integer result and diagnostics.
func EvalExpr(src string) (int64, []Diagnostic, error) {
	prog, err := parseProgram(src)
	if err != nil {
		return 0, nil, err
	}
	diags := Analyze(prog)
	if anyErr(diags) {
		return 0, diags, nil
	}
	ev := NewEvaluator()
	v, err := ev.EvalProgram(prog)
	return v, diags, err
}

func containsYield(stmts []Stmt) bool {
	for _, st := range stmts {
		switch s := st.(type) {
		case *YieldStmt:
			return true
		case *IfStmt:
			if containsYield(s.Then) {
				return true
			}
			for _, e := range s.Elifs {
				if containsYield(e.Then) {
					return true
				}
			}
			if containsYield(s.Else) {
				return true
			}
		case *WhileStmt:
			if containsYield(s.Body) || containsYield(s.Else) {
				return true
			}
		case *ForStmt:
			if containsYield(s.Body) || containsYield(s.Else) {
				return true
			}
		case *FuncDef:
			if containsYield(s.Body) {
				return true
			}
		}
	}
	return false
}
