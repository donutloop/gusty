package lang

import (
	"fmt"
	"strings"
)

// GenerateIR produces LLVM IR text for prog (deterministic, no native LLVM).
func GenerateIR(prog *Program) (string, error) {
	g := &irGen{fmtIdx: 0, strIdx: 0, tmp: 0}
	var b strings.Builder
	// printf declaration
	g.decls = "declare i32 @printf(i8*, ...)\n"
	if prog != nil {
		for _, st := range prog.Stmts {
			if err := g.stmt(&b, st); err != nil {
				return "", err
			}
		}
	}
	// emit globals then main function
	var out strings.Builder
	out.WriteString(g.globals.String())
	out.WriteString(g.decls)
	out.WriteString("define i32 @main() {\n")
	out.WriteString("entry:\n")
	out.WriteString(b.String())
	out.WriteString("  ret i32 0\n}\n")
	return out.String(), nil
}

type irGen struct {
	globals strings.Builder
	decls   string
	fmtIdx  int
	strIdx  int
	tmp     int
	label   int
}

func (g *irGen) newTmp() string { g.tmp++; return fmt.Sprintf("%%t%d", g.tmp) }
func (g *irGen) newLabel(s string) string { g.label++; return fmt.Sprintf("%s%d", s, g.label) }

// fmtStr emits a global string constant for a printf format and returns its name.
func (g *irGen) fmtStr(format string) string {
	g.fmtIdx++
	name := fmt.Sprintf("@.fmt%d", g.fmtIdx)
	// escape for IR: % -> %% and \n stays literal in IR text
	f := strings.ReplaceAll(format, "%", "%%")
	f = strings.ReplaceAll(f, "\\", "\\\\")
	g.globals.WriteString(fmt.Sprintf("%s = private unnamed_addr constant [%d x i8] c\"%s\\00\"\n", name, len(f)+1, f))
	return name
}

// strConst emits a global for a string literal operand.
func (g *irGen) strConst(s string) string {
	g.strIdx++
	name := fmt.Sprintf("@.str%d", g.strIdx)
	esc := strings.ReplaceAll(s, "\\", "\\\\")
	g.globals.WriteString(fmt.Sprintf("%s = private unnamed_addr constant [%d x i8] c\"%s\\00\"\n", name, len(esc)+1, esc))
	return name
}

// value emits an IR expression returning an i32 value; returns the operand string.
func (g *irGen) value(b *strings.Builder, e Expr) (string, error) {
	switch n := e.(type) {
	case *IntLit:
		return fmt.Sprintf("i32 %d", n.Value), nil
	case *FloatLit:
		return fmt.Sprintf("i32 %d", int64(n.Value)), nil
	case *BoolLit:
		if n.Value {
			return "i32 1", nil
		}
		return "i32 0", nil
	case *NoneLit:
		return "i32 0", nil
	case *Name:
		return "%" + n.Value, nil
	case *BinOp:
		l, err := g.value(b, n.L)
		if err != nil {
			return "", err
		}
		r, err := g.value(b, n.R)
		if err != nil {
			return "", err
		}
		t := g.newTmp()
		var op string
		switch n.Op {
		case "+":
			op = "add"
		case "-":
			op = "sub"
		case "*":
			op = "mul"
		case "/":
			op = "sdiv"
		case "==":
			op = "icmp eq"
		case "!=":
			op = "icmp ne"
		case "<":
			op = "icmp slt"
		case "<=":
			op = "icmp sle"
		case ">":
			op = "icmp sgt"
		case ">=":
			op = "icmp sge"
		default:
			return "", fmt.Errorf("codegen: unsupported operator %q", n.Op)
		}
		if strings.HasPrefix(op, "icmp") {
			b.WriteString(fmt.Sprintf("  %s = %s i32 %s, i32 %s\n", t, op, l, r))
		} else {
			b.WriteString(fmt.Sprintf("  %s = %s i32 %s, i32 %s\n", t, op, l, r))
		}
		return t, nil
	case *UnOp:
		x, err := g.value(b, n.X)
		if err != nil {
			return "", err
		}
		t := g.newTmp()
		switch n.Op {
		case "-":
			b.WriteString(fmt.Sprintf("  %s = sub i32 0, %s\n", t, x))
		case "not":
			b.WriteString(fmt.Sprintf("  %s = icmp eq i32 %s, 0\n", t, x))
		default:
			return "", fmt.Errorf("codegen: unsupported unary %q", n.Op)
		}
		return t, nil
	case *StrLit:
		name := g.strConst(n.Value)
		return name, nil
	case *Call:
		return g.call(b, n)
	default:
		return "", fmt.Errorf("codegen: unsupported expression %T", e)
	}
}

// call emits a call; supports print/printf and range(n).
func (g *irGen) call(b *strings.Builder, c *Call) (string, error) {
	fnName := ""
	if n, ok := c.Fn.(*Name); ok {
		fnName = n.Value
	}
	switch fnName {
	case "print", "printf":
		if len(c.Args) < 1 {
			return "", fmt.Errorf("codegen: print needs an argument")
		}
		v, err := g.value(b, c.Args[0])
		if err != nil {
			return "", err
		}
		fmtName := g.fmtStr("%d\n")
		t := g.newTmp()
		b.WriteString(fmt.Sprintf("  %s = call i32 @printf(i8* getelementptr inbounds ([%d x i8], [%d x i8]* %s, i32 0, i32 0), i32 %s)\n", t, 4, 4, fmtName, v))
		return t, nil
	case "range":
		if len(c.Args) != 1 {
			return "", fmt.Errorf("codegen: range needs one argument")
		}
		return g.value(b, c.Args[0])
	default:
		return "", fmt.Errorf("codegen: unsupported call %q", fnName)
	}
}

func (g *irGen) stmt(b *strings.Builder, st Stmt) error {
	switch n := st.(type) {
	case *ExprStmt:
		if _, err := g.value(b, n.Expr); err != nil {
			return err
		}
	case *AssignStmt:
		if nm, ok := n.Target.(*Name); ok {
			v, err := g.value(b, n.Value)
			if err != nil {
				return err
			}
			b.WriteString(fmt.Sprintf("  %%_%s = alloca i32\n", nm.Value))
			b.WriteString(fmt.Sprintf("  store i32 %s, i32* %%_%s\n", v, nm.Value))
			// name the alloca pointer as %<name> via load
			b.WriteString(fmt.Sprintf("  %%_%s.ld = load i32, i32* %%_%s\n", nm.Value, nm.Value))
		} else {
			return fmt.Errorf("codegen: unsupported assignment target %T", n.Target)
		}
	case *IfStmt:
		cond, err := g.value(b, n.Cond)
		if err != nil {
			return err
		}
		thenL := g.newLabel("if.then")
		elseL := g.newLabel("if.else")
		endL := g.newLabel("if.end")
		b.WriteString(fmt.Sprintf("  br i1 %s, label %%%s, label %%%s\n", cond, thenL, elseL))
		b.WriteString(fmt.Sprintf("%s:\n", thenL))
		for _, s := range n.Then {
			if err := g.stmt(b, s); err != nil {
				return err
			}
		}
		b.WriteString(fmt.Sprintf("  br label %%%s\n", endL))
		b.WriteString(fmt.Sprintf("%s:\n", elseL))
		for _, s := range n.Else {
			if err := g.stmt(b, s); err != nil {
				return err
			}
		}
		b.WriteString(fmt.Sprintf("  br label %%%s\n", endL))
		b.WriteString(fmt.Sprintf("%s:\n", endL))
	case *WhileStmt:
		condL := g.newLabel("while.cond")
		bodyL := g.newLabel("while.body")
		endL := g.newLabel("while.end")
		b.WriteString(fmt.Sprintf("  br label %%%s\n", condL))
		b.WriteString(fmt.Sprintf("%s:\n", condL))
		cond, err := g.value(b, n.Cond)
		if err != nil {
			return err
		}
		b.WriteString(fmt.Sprintf("  br i1 %s, label %%%s, label %%%s\n", cond, bodyL, endL))
		b.WriteString(fmt.Sprintf("%s:\n", bodyL))
		for _, s := range n.Body {
			if err := g.stmt(b, s); err != nil {
				return err
			}
		}
		b.WriteString(fmt.Sprintf("  br label %%%s\n", condL))
		b.WriteString(fmt.Sprintf("%s:\n", endL))
	case *ForStmt:
		initL := g.newLabel("for.init")
		condL := g.newLabel("for.cond")
		bodyL := g.newLabel("for.body")
		incL := g.newLabel("for.inc")
		endL := g.newLabel("for.end")
		b.WriteString(fmt.Sprintf("  br label %%%s\n", initL))
		b.WriteString(fmt.Sprintf("%s:\n", initL))
		iter, err := g.value(b, n.Iter)
		if err != nil {
			return err
		}
		b.WriteString(fmt.Sprintf("  %%_%s = alloca i32\n", n.Var.Value))
		b.WriteString(fmt.Sprintf("  store i32 0, i32* %%_%s\n", n.Var.Value))
		b.WriteString(fmt.Sprintf("  br label %%%s\n", condL))
		b.WriteString(fmt.Sprintf("%s:\n", condL))
		b.WriteString(fmt.Sprintf("  %%_%s.ld = load i32, i32* %%_%s\n", n.Var.Value, n.Var.Value))
		t := g.newTmp()
		b.WriteString(fmt.Sprintf("  %s = icmp slt i32 %%_%s.ld, i32 %s\n", t, n.Var.Value, iter))
		b.WriteString(fmt.Sprintf("  br i1 %s, label %%%s, label %%%s\n", t, bodyL, endL))
		b.WriteString(fmt.Sprintf("%s:\n", bodyL))
		for _, s := range n.Body {
			if err := g.stmt(b, s); err != nil {
				return err
			}
		}
		b.WriteString(fmt.Sprintf("  br label %%%s\n", incL))
		b.WriteString(fmt.Sprintf("%s:\n", incL))
		b.WriteString(fmt.Sprintf("  %%_%s.ld2 = load i32, i32* %%_%s\n", n.Var.Value, n.Var.Value))
		b.WriteString(fmt.Sprintf("  %%_%s.inc = add i32 %%_%s.ld2, 1\n", n.Var.Value, n.Var.Value))
		b.WriteString(fmt.Sprintf("  store i32 %%_%s.inc, i32* %%_%s\n", n.Var.Value, n.Var.Value))
		b.WriteString(fmt.Sprintf("  br label %%%s\n", condL))
		b.WriteString(fmt.Sprintf("%s:\n", endL))
	case *ReturnStmt:
		v, err := g.value(b, n.Expr)
		if err != nil {
			return err
		}
		b.WriteString(fmt.Sprintf("  ret i32 %s\n", v))
	default:
		return fmt.Errorf("codegen: unsupported statement %T", st)
	}
	return nil
}
