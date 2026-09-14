package lang

// Evaluator is a small AST interpreter used by --eval and the REPL.
// It evaluates integer-typed expressions deterministically without needing
// an LLVM JIT engine (the go-llvm fork is bindings-only, no ExecutionEngine).
type Evaluator struct {
	Vars  map[string]int64
	funcs map[string]*FuncDef
}

func NewEvaluator() *Evaluator { return &Evaluator{Vars: map[string]int64{}, funcs: map[string]*FuncDef{}} }

// EvalProgram evaluates prog's top-level statements and returns the value of
// the final expression statement (or last assignment). It returns an error on
// unsupported constructs.
func (e *Evaluator) EvalProgram(prog *Program) (int64, error) {
	var last int64
	for _, st := range prog.Stmts {
		switch s := st.(type) {
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
		start, stop, err := e.rangeBounds(s.Iter)
		if err != nil {
			return 0, err
		}
		completed := true
		if n := s.Var; n != nil {
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
	case *Call:
		return e.evalCall(n)
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

func (e *Evaluator) evalCall(n *Call) (int64, error) {
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
