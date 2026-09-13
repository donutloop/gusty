package lang

// Evaluator is a small AST interpreter used by --eval and the REPL.
// It evaluates integer-typed expressions deterministically without needing
// an LLVM JIT engine (the go-llvm fork is bindings-only, no ExecutionEngine).
type Evaluator struct {
	Vars map[string]int64
}

func NewEvaluator() *Evaluator { return &Evaluator{Vars: map[string]int64{}} }

// EvalProgram evaluates prog's top-level statements and returns the value of
// the final expression statement (or last assignment). It returns an error on
// unsupported constructs.
func (e *Evaluator) EvalProgram(prog *Program) (int64, error) {
	var last int64
	for _, st := range prog.Stmts {
		switch s := st.(type) {
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

func (e *Evaluator) evalCall(n *Call) (int64, error) {
	if name, ok := n.Fn.(*Name); ok {
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
			return 0, nil
		}
	}
	return 0, &EvalError{Msg: "unsupported call for eval"}
}

// EvalError is a runtime eval error.
type EvalError struct{ Msg string }

func (e *EvalError) Error() string { return "eval error: " + e.Msg }

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
