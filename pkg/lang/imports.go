package lang

import (
	"fmt"
	"os"
)

// ImportInfo carries the compile-time-folded module globals for the AOT
// compiler. Each imported module's top-level global variables are folded to
// constant AST literals (data imports); module function dispatch is deferred.
type ImportInfo struct {
	Globals map[string]map[string]Expr // module name -> global name -> folded constant
}

// resolveImports loads each top-level `import mod` from `mod.gy` (relative to
// the working directory, mirroring the interpreter), parses + analyzes it, and
// constant-folds its top-level global assignments. The main program's
// `mod.var` references are resolved to the folded constants by the codegen.
//
// A module may itself `import other` (nested imports); the nested module's
// globals are folded too and resolved via `other.var` references.
func resolveImports(prog *Program) (*ImportInfo, error) {
	info := &ImportInfo{Globals: map[string]map[string]Expr{}}
	reg := map[string]map[string]Expr{}
	for _, st := range prog.Stmts {
		im, ok := st.(*ImportStmt)
		if !ok {
			continue
		}
		if err := resolveModule(im.Module, reg); err != nil {
			return nil, err
		}
	}
	info.Globals = reg
	return info, nil
}

// resolveModule loads mod.gy, parses + analyzes it, and constant-folds its
// top-level globals into the shared registry reg. Nested imports recurse.
func resolveModule(mod string, reg map[string]map[string]Expr) error {
	if _, dup := reg[mod]; dup {
		return fmt.Errorf("import: duplicate module %q", mod)
	}
	src, err := os.ReadFile(mod + ".gy")
	if err != nil {
		return fmt.Errorf("import %q: %v", mod, err)
	}
	mp, perr := Parse(string(src))
	if perr != nil {
		return fmt.Errorf("import %q: %v", mod, perr)
	}
	if diags := Analyze(mp); hasErr(diags) {
		return fmt.Errorf("import %q: %s", mod, diags[0].Msg)
	}
	// fold the module's own globals, allowing references to previously
	// folded module globals (this module's own, and nested-imported ones).
	own := map[string]Expr{}
	for _, mst := range mp.Stmts {
		switch s := mst.(type) {
		case *ImportStmt:
			if err := resolveModule(s.Module, reg); err != nil {
				return err
			}
		case *AssignStmt:
			nm, ok := s.Target.(*Name)
			if !ok {
				return fmt.Errorf("import %q: only plain top-level globals are supported", mod)
			}
			v, err := foldConst(s.Value, own, reg)
			if err != nil {
				return fmt.Errorf("import %q: global %q is not a compile-time constant: %v", mod, nm.Value, err)
			}
			own[nm.Value] = v
		case *FuncDef:
			return fmt.Errorf("import %q: module functions are not yet supported in AOT imports (data imports only)", mod)
		default:
			return fmt.Errorf("import %q: unsupported top-level statement in AOT import", mod)
		}
	}
	reg[mod] = own
	return nil
}

// foldConst folds an expression to a constant AST literal. It supports
// integer/float literals, booleans, strings, and arithmetic on already-folded
// module globals. `other.var` references resolve against the shared registry
// (nested imports). Anything else (calls, lists, etc.) is rejected.
func foldConst(e Expr, globals map[string]Expr, reg map[string]map[string]Expr) (Expr, error) {
	switch n := e.(type) {
	case *IntLit, *FloatLit, *BoolLit, *StrLit, *NoneLit:
		return e, nil
	case *Name:
		if v, ok := globals[n.Value]; ok {
			return v, nil
		}
		return nil, fmt.Errorf("undefined module global %q", n.Value)
	case *Attr:
		if nm, ok := n.Obj.(*Name); ok {
			if m, ok := reg[nm.Value]; ok {
				if v, ok := m[n.Name.Value]; ok {
					return v, nil
				}
			}
			return nil, fmt.Errorf("unknown imported module global %s.%s", nm.Value, n.Name.Value)
		}
		return nil, fmt.Errorf("unsupported attribute expression")
	case *BinOp:
		l, err := foldConst(n.L, globals, reg)
		if err != nil {
			return nil, err
		}
		r, err := foldConst(n.R, globals, reg)
		if err != nil {
			return nil, err
		}
		return foldBin(n.Op, l, r)
	case *UnOp:
		v, err := foldConst(n.X, globals, reg)
		if err != nil {
			return nil, err
		}
		return foldUn(n.Op, v)
	default:
		return nil, fmt.Errorf("unsupported expression")
	}
}

func foldBin(op string, l, r Expr) (Expr, error) {
	// string concatenation: "a" + "b" -> "ab"
	ls, lsok := l.(*StrLit)
	rs, rsok := r.(*StrLit)
	if op == "+" && lsok && rsok {
		return &StrLit{Value: ls.Value + rs.Value}, nil
	}
	li, lok := l.(*IntLit)
	ri, rok := r.(*IntLit)
	if lok && rok {
		a, b := int(li.Value), int(ri.Value)
		var res int64
		switch op {
		case "+":
			res = int64(a + b)
		case "-":
			res = int64(a - b)
		case "*":
			res = int64(a * b)
		case "/", "//":
			if b == 0 {
				return nil, fmt.Errorf("division by zero")
			}
			res = int64(a / b)
		case "%":
			if b == 0 {
				return nil, fmt.Errorf("division by zero")
			}
			res = int64(a % b)
		default:
			return nil, fmt.Errorf("unsupported op %q", op)
		}
		return &IntLit{Value: res}, nil
	}
	lf, lok := l.(*FloatLit)
	rf, rok := r.(*FloatLit)
	if lok && rok {
		a, b := lf.Value, rf.Value
		var res float64
		switch op {
		case "+":
			res = a + b
		case "-":
			res = a - b
		case "*":
			res = a * b
		case "/":
			if b == 0 {
				return nil, fmt.Errorf("division by zero")
			}
			res = a / b
		default:
			return nil, fmt.Errorf("unsupported op %q", op)
		}
		return &FloatLit{Value: res}, nil
	}
	return nil, fmt.Errorf("non-constant operands")
}

func foldUn(op string, v Expr) (Expr, error) {
	if i, ok := v.(*IntLit); ok {
		switch op {
		case "-":
			return &IntLit{Value: -i.Value}, nil
		case "not":
			return &BoolLit{Value: !(i.Value != 0)}, nil
		}
	}
	if b, ok := v.(*BoolLit); ok && op == "not" {
		return &BoolLit{Value: !b.Value}, nil
	}
	return nil, fmt.Errorf("unsupported unary op %q", op)
}

func hasErr(diags []Diagnostic) bool {
	for _, d := range diags {
		if d.Level == LevelError {
			return true
		}
	}
	return false
}
