package lang

import (
	"fmt"
	"strings"
)


// GenerateIR produces LLVM IR text for prog (deterministic, no native LLVM).
func GenerateIR(prog *Program) (string, error) {
	g := &irGen{sym: map[string]string{}, allocd: map[string]bool{}, funcs: map[string]bool{}, fds: map[string]*FuncDef{}, params: map[string]string{}, fmtIdx: 0, strIdx: 0, tmp: 0, ldN: 0}
	// pre-scan top-level for user function names
	for _, st := range prog.Stmts {
		if fd, ok := st.(*FuncDef); ok {
			g.funcs[fd.Name] = true
			g.fds[fd.Name] = fd
		}
	}
	g.decls = "declare i32 @printf(i8*, ...)\n"
	var b strings.Builder
	// pre-scan top-level for user function names
	// user function definitions become separate defines before main
	for _, st := range prog.Stmts {
		if fd, ok := st.(*FuncDef); ok {
			if err := g.funcDef(&b, fd); err != nil {
				return "", err
			}
		}
	}
	b.WriteString("define i32 @main() {\nentry:\n")
	for _, ap := range g.applyCalls {
		fmt.Fprintf(&b, "  call void %s()\n", ap)
	}
	for _, st := range prog.Stmts {
		if _, ok := st.(*FuncDef); ok {
			continue
		}
		if err := g.stmt(&b, st); err != nil {
			return "", err
		}
	}
	b.WriteString("  ret i32 0\n}\n")
	// assemble output
	var out strings.Builder
	g.emitEnvGlobals()
	out.WriteString(g.globals.String())
	out.WriteString(g.decls)
	out.WriteString(b.String())
	// PIC Level = 2 module flag: forces llc to emit position-independent code
	// so string constants in .rodata are referenced PIC-safely. Without it llc
	// defaults to the static relocation model, which emits 32-bit absolute
	// relocations (e.g. R_X86_64_32) that the default PIE link (cc) rejects.
	out.WriteString("!llvm.module.flags = !{!0}\n")
	out.WriteString("!0 = !{i32 2, !\"PIC Level\", i32 2}\n")
	return out.String(), nil
}

type irGen struct {
	globals strings.Builder
	decls   string
	sym     map[string]string  // variable -> load temp
	allocd  map[string]bool    // alloca emitted?
	funcs   map[string]bool    // user-defined function names
	fds     map[string]*FuncDef // function definitions by name (for call arg binding)
	params  map[string]string  // current function params: name -> register
	fmtIdx  int
	strIdx  int
	tmp     int
	label   int
	ldN     int
	loopStack []loopInfo

	closures    map[string]*closureInfo
	envMode     bool
	envCaptures map[string]int
	envParam    string
	decorated   map[string]bool
	applyCalls  []string
	listNames map[*ListLit]string
	lstIdx int
}

type loopInfo struct {
	breakLabel    string
	continueLabel string
}

func (g *irGen) newTmp() string { g.tmp++; return fmt.Sprintf("%%t%d", g.tmp) }
func (g *irGen) newLabel(s string) string { g.label++; return fmt.Sprintf("%s%d", s, g.label) }

// fmtStr emits a global string constant for a printf format; returns name and size.
func (g *irGen) fmtStr(format string) (string, int) {
	g.fmtIdx++
	name := fmt.Sprintf("@.fmt%d", g.fmtIdx)
	// escape backslashes for the IR c"..." literal; % is literal in IR and
	// must stay single so printf sees a real format directive (e.g. %d -> 42).
	f := strings.ReplaceAll(format, "\\", "\\\\")
	f = strings.ReplaceAll(f, "\n", "\\0A")
	g.globals.WriteString(fmt.Sprintf("%s = private unnamed_addr constant [%d x i8] c\"%s\\00\"\n", name, len(format)+1, f))
	return name, len(f) + 1
}

// strConst emits a global for a string literal operand.
func (g *irGen) strConst(s string) string {
	g.strIdx++
	name := fmt.Sprintf("@.str%d", g.strIdx)
	esc := strings.ReplaceAll(s, "\\", "\\\\")
	esc = strings.ReplaceAll(esc, "\n", "\\0A")
	g.globals.WriteString(fmt.Sprintf("%s = private unnamed_addr constant [%d x i8] c\"%s\\00\"\n", name, len(esc)+1, esc))
	return name
}
// emitList emits a dedicated global struct for an inline list literal
// (dedup by AST node) and returns its global name. Elements must be ints.
func (g *irGen) emitList(ln *ListLit) (string, error) {
	if name, ok := g.listNames[ln]; ok {
		return name, nil
	}
	if g.listNames == nil {
		g.listNames = map[*ListLit]string{}
	}
	n := len(ln.Elems)
	g.lstIdx++
	name := fmt.Sprintf("@.lst%d", g.lstIdx)
	g.globals.WriteString(fmt.Sprintf("%s = private global {i32, [%d x i32]} { i32 %d, [%d x i32] [", name, n, n, n))
	for i, el := range ln.Elems {
		il, ok := el.(*IntLit)
		if !ok {
			return "", fmt.Errorf("list literal elements must be integers")
		}
		if i > 0 {
			g.globals.WriteString(", ")
		}
		g.globals.WriteString(fmt.Sprintf("i32 %d", il.Value))
	}
	g.globals.WriteString("] }\n")
	g.listNames[ln] = name
	return name, nil
}


// value emits an IR expression returning an i32 value; returns the operand string.
func (g *irGen) value(b *strings.Builder, e Expr) (string, error) {
	switch n := e.(type) {
	case *IntLit:
		return fmt.Sprintf("%d", n.Value), nil
	case *FloatLit:
		return fmt.Sprintf("%d", int64(n.Value)), nil
	case *BoolLit:
		if n.Value {
			return "1", nil
		}
		return "0", nil
	case *NoneLit:
		return "0", nil
	case *Name:
		if reg, ok := g.params[n.Value]; ok {
			return reg, nil
		}
		// Always load fresh from the alloca so the value dominates its use.
		g.ldN++
		if off, ok := g.envCaptures[n.Value]; ok && g.envMode {
			return g.emitEnvLoad(b, g.envParam, off), nil
		}
		ld := fmt.Sprintf("%%_%s.ld%d", n.Value, g.ldN)
		b.WriteString(fmt.Sprintf("  %s = load i32, i32* %%_%s\n", ld, n.Value))
		return ld, nil
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
			b.WriteString(fmt.Sprintf("  %s = %s i32 %s, %s\n", t, op, l, r))
		} else {
			b.WriteString(fmt.Sprintf("  %s = %s i32 %s, %s\n", t, op, l, r))
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
	case *ListLit:
		// inline list literal: emit a dedicated global struct and return its name.
		name, err := g.emitList(n)
		if err != nil {
			return "", err
		}
		return name, nil
	case *Index:
		// list indexing: require an inline list literal with a constant index
		// (this llc build accepts only constant GEP indices).
		ln, ok := n.Obj.(*ListLit)
		if !ok {
			return "", fmt.Errorf("index requires an inline list literal")
		}
		il, ok := n.Idx.(*IntLit)
		if !ok {
			return "", fmt.Errorf("list index must be a constant")
		}
		name, err := g.emitList(ln)
		if err != nil {
			return "", err
		}
		v := g.newTmp()
		b.WriteString(fmt.Sprintf("%s = load i32, i32* getelementptr({i32, [%d x i32]}, {i32, [%d x i32]}* %s, i32 0, i32 1, i32 %d)\n", v, len(ln.Elems), len(ln.Elems), name, il.Value))
		return v, nil
	case *Call:
		return g.call(b, n)
	case *KeywordArg:
		return g.value(b, n.Value)
	default:
		return "", fmt.Errorf("codegen: unsupported expression %T", e)
	}
}

// rangeBounds computes the loop start and stop operands for a for statement.
// A `range(a, b)` iterable yields start=a and stop=b; anything else starts at 0
// with stop being the single evaluated bound.
func (g *irGen) rangeBounds(b *strings.Builder, iter Expr) (string, string, error) {
	if c, ok := iter.(*Call); ok {
		if n, ok2 := c.Fn.(*Name); ok2 && n.Value == "range" && len(c.Args) == 2 {
			start, err := g.value(b, c.Args[0])
			if err != nil {
				return "", "", err
			}
			stop, err := g.value(b, c.Args[1])
			if err != nil {
				return "", "", err
			}
			return start, stop, nil
		}
	}
	stop, err := g.value(b, iter)
	if err != nil {
		return "", "", err
	}
	return "0", stop, nil
}

// call emits a call; supports print/printf and range(n).
func (g *irGen) call(b *strings.Builder, c *Call) (string, error) {
	fnName := ""
	if n, ok := c.Fn.(*Name); ok {
		fnName = n.Value
	}
	if g.funcs[fnName] {
		fd := g.fds[fnName]
		if fd == nil {
			return "", fmt.Errorf("codegen: unknown function %q", fnName)
		}
		n := len(fd.Params)
		vals := make([]string, n)
		provided := make([]bool, n)
		pos := 0
		seenKw := false
		for _, a := range c.Args {
			if kw, ok := a.(*KeywordArg); ok {
				seenKw = true
				idx := -1
				for i, p := range fd.Params {
					if p.Name == kw.Name {
						idx = i
						break
					}
				}
				if idx < 0 {
					return "", fmt.Errorf("codegen: unknown keyword argument %q for %s", kw.Name, fnName)
				}
				if provided[idx] {
					return "", fmt.Errorf("codegen: multiple values for argument %q of %s", kw.Name, fnName)
				}
				av, err := g.value(b, kw.Value)
				if err != nil {
					return "", err
				}
				vals[idx] = "i32 " + av
				provided[idx] = true
				continue
			}
			if seenKw {
				return "", fmt.Errorf("codegen: positional argument after keyword argument for %s", fnName)
			}
			if pos >= n {
				return "", fmt.Errorf("codegen: too many arguments for %s", fnName)
			}
			if provided[pos] {
				return "", fmt.Errorf("codegen: multiple values for argument %q of %s", fd.Params[pos].Name, fnName)
			}
			av, err := g.value(b, a)
			if err != nil {
				return "", err
			}
			vals[pos] = "i32 " + av
			provided[pos] = true
			pos++
		}
		// fill defaults for params not supplied
		for i := range fd.Params {
			if provided[i] {
				continue
			}
			if fd.Params[i].Default == nil {
				return "", fmt.Errorf("codegen: missing argument %q for %s", fd.Params[i].Name, fnName)
			}
			dv, err := g.value(b, fd.Params[i].Default)
			if err != nil {
				return "", err
			}
			vals[i] = "i32 " + dv
		}
		t := g.newTmp()
		if _, ok := g.closures[fnName]; ok {
			env := g.newTmp()
			b.WriteString(fmt.Sprintf("  %s = load i32, i32* @%s_slot\n", env, fnName))
			callArgs := append([]string{"i32 " + env}, vals...)
			b.WriteString(fmt.Sprintf("  %s = call i32 @%s_env(%s)\n", t, fnName, strings.Join(callArgs, ", ")))
		} else if g.decorated[fnName] {
			b.WriteString(fmt.Sprintf("  %s = call i32 @%s_impl(%s)\n", t, fnName, strings.Join(vals, ", ")))
		} else {
			b.WriteString(fmt.Sprintf("  %s = call i32 @%s(%s)\n", t, fnName, strings.Join(vals, ", ")))
		}
		return t, nil
	}
	switch fnName {
	case "print", "printf":
		for _, a := range c.Args {
			if _, ok := a.(*KeywordArg); ok {
				return "", fmt.Errorf("codegen: %s does not accept keyword arguments", fnName)
			}
		}
		if len(c.Args) < 1 {
			return "", fmt.Errorf("codegen: print needs an argument")
		}
		v, err := g.value(b, c.Args[0])
		if err != nil {
			return "", err
		}
		fmtName, size := g.fmtStr("%d\n")
		t := g.newTmp()
		b.WriteString(fmt.Sprintf("  %s = call i32 @printf(i8* getelementptr inbounds ([%d x i8], [%d x i8]* %s, i32 0, i32 0), i32 %s)\n", t, size, size, fmtName, v))
		return t, nil
	case "len":
		// len(list) -> load the count field of an inline list literal's struct.
		if len(c.Args) != 1 {
			return "", fmt.Errorf("len expects one argument")
		}
		ln, ok := c.Args[0].(*ListLit)
		if !ok {
			return "", fmt.Errorf("len requires an inline list literal")
		}
		name, err := g.emitList(ln)
		if err != nil {
			return "", err
		}
		v := g.newTmp()
		b.WriteString(fmt.Sprintf("%s = load i32, i32* getelementptr({i32, [%d x i32]}, {i32, [%d x i32]}* %s, i32 0, i32 0)\n", v, len(ln.Elems), len(ln.Elems), name))
		return v, nil
	case "range":
		for _, a := range c.Args {
			if _, ok := a.(*KeywordArg); ok {
				return "", fmt.Errorf("codegen: range does not accept keyword arguments")
			}
		}
		if len(c.Args) != 1 {
			return "", fmt.Errorf("codegen: range needs one argument")
		}
		return g.value(b, c.Args[0])
	default:
		return "", fmt.Errorf("codegen: unsupported call %q", fnName)
	}
}

func (g *irGen) funcDef(b *strings.Builder, fd *FuncDef) error {
	g.closures = map[string]*closureInfo{}
	g.envMode = false
	g.envCaptures = nil
	g.envParam = "%env"
	g.decorated = map[string]bool{}
	op := map[string]bool{}
	for _, p := range fd.Params {
		op[p.Name] = true
	}
	ol := map[string]bool{}
	collectLocals(fd.Body, ol)
	for _, nd := range nestedDefs(fd.Body) {
		ci := closureInfoFor(nd, op, ol)
		g.closures[ci.name] = ci
		fmt.Fprintf(&g.globals, "@%s_slot = internal global i32 0\n", ci.name)
		g.emitClosureDef(b, ci, nd)
	}
	if len(fd.Decorators) > 0 {
		g.emitDecoratedFunc(b, fd)
		return nil
	}
	g.params = map[string]string{}
	fmt.Fprintf(b, "define i32 @%s(", fd.Name)
	for i := range fd.Params {
		if i > 0 {
			fmt.Fprintf(b, ", ")
		}
		fmt.Fprintf(b, "i32 %%p%d", i)
	}
	fmt.Fprintf(b, ") {\n")
	for i, p := range fd.Params {
		g.params[p.Name] = fmt.Sprintf("%%p%d", i)
	}
	for _, st := range fd.Body {
		if err := g.stmt(b, st); err != nil {
			return err
		}
	}
	fmt.Fprintf(b, "  ret i32 0\n}\n")
	g.params = map[string]string{}
	return nil
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
			if !g.allocd[nm.Value] {
				b.WriteString(fmt.Sprintf("  %%_%s = alloca i32\n", nm.Value))
				g.allocd[nm.Value] = true
			}
			b.WriteString(fmt.Sprintf("  store i32 %s, i32* %%_%s\n", v, nm.Value))
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
	case *MatchStmt:
		sub, err := g.value(b, n.Subject)
		if err != nil {
			return err
		}
		endL := g.newLabel("match.end")
		for i, c := range n.Cases {
			pat, err := g.value(b, c.Pattern)
			if err != nil {
				return err
			}
			bodyL := g.newLabel("match.case")
			cmp := g.newTmp()
			b.WriteString(fmt.Sprintf("  %s = icmp eq i32 %s, %s\n", cmp, sub, pat))
			var fallL string
			if i < len(n.Cases)-1 {
				fallL = g.newLabel("match.next")
			} else {
				fallL = endL
			}
			b.WriteString(fmt.Sprintf("  br i1 %s, label %%%s, label %%%s\n", cmp, bodyL, fallL))
			b.WriteString(fmt.Sprintf("%s:\n", bodyL))
			for _, s := range c.Body {
				if err := g.stmt(b, s); err != nil {
					return err
				}
			}
			b.WriteString(fmt.Sprintf("  br label %%%s\n", endL))
			if fallL != endL {
				b.WriteString(fmt.Sprintf("%s:\n", fallL))
			}
		}
		b.WriteString(fmt.Sprintf("%s:\n", endL))
	case *WhileStmt:
		condL := g.newLabel("while.cond")
		bodyL := g.newLabel("while.body")
		elseL := g.newLabel("while.else")
		endL := g.newLabel("while.end")
		b.WriteString(fmt.Sprintf("  br label %%%s\n", condL))
		b.WriteString(fmt.Sprintf("%s:\n", condL))
		cond, err := g.value(b, n.Cond)
		if err != nil {
			return err
		}
		// normal completion (cond false) enters else if present; break skips else
		normalL := endL
		if len(n.Else) > 0 {
			normalL = elseL
		}
		b.WriteString(fmt.Sprintf("  br i1 %s, label %%%s, label %%%s\n", cond, bodyL, normalL))
		b.WriteString(fmt.Sprintf("%s:\n", bodyL))
		g.loopStack = append(g.loopStack, loopInfo{breakLabel: endL, continueLabel: condL})
		for _, s := range n.Body {
			if err := g.stmt(b, s); err != nil {
				return err
			}
		}
		g.loopStack = g.loopStack[:len(g.loopStack)-1]
		b.WriteString(fmt.Sprintf("  br label %%%s\n", condL))
		if len(n.Else) > 0 {
			b.WriteString(fmt.Sprintf("%s:\n", elseL))
			for _, s := range n.Else {
				if err := g.stmt(b, s); err != nil {
					return err
				}
			}
			b.WriteString(fmt.Sprintf("  br label %%%s\n", endL))
		}
		b.WriteString(fmt.Sprintf("%s:\n", endL))
	case *ForStmt:
		initL := g.newLabel("for.init")
		condL := g.newLabel("for.cond")
		bodyL := g.newLabel("for.body")
		incL := g.newLabel("for.inc")
		elseL := g.newLabel("for.else")
		endL := g.newLabel("for.end")
		b.WriteString(fmt.Sprintf("  br label %%%s\n", initL))
		b.WriteString(fmt.Sprintf("%s:\n", initL))
		start, stop, err := g.rangeBounds(b, n.Iter)
		if err != nil {
			return err
		}
		b.WriteString(fmt.Sprintf("  %%_%s = alloca i32\n", n.Var.Value))
		b.WriteString(fmt.Sprintf("  store i32 %s, i32* %%_%s\n", start, n.Var.Value))
		b.WriteString(fmt.Sprintf("  br label %%%s\n", condL))
		b.WriteString(fmt.Sprintf("%s:\n", condL))
		g.ldN++
		cld := fmt.Sprintf("%%_%s.ld%d", n.Var.Value, g.ldN)
		b.WriteString(fmt.Sprintf("  %s = load i32, i32* %%_%s\n", cld, n.Var.Value))
		t := g.newTmp()
		b.WriteString(fmt.Sprintf("  %s = icmp slt i32 %s, %s\n", t, cld, stop))
		// normal completion (i >= stop) enters else if present; break skips else
		normalL := endL
		if len(n.Else) > 0 {
			normalL = elseL
		}
		b.WriteString(fmt.Sprintf("  br i1 %s, label %%%s, label %%%s\n", t, bodyL, normalL))
		b.WriteString(fmt.Sprintf("%s:\n", bodyL))
		g.loopStack = append(g.loopStack, loopInfo{breakLabel: endL, continueLabel: incL})
		for _, s := range n.Body {
			if err := g.stmt(b, s); err != nil {
				return err
			}
		}
		g.loopStack = g.loopStack[:len(g.loopStack)-1]
		b.WriteString(fmt.Sprintf("  br label %%%s\n", incL))
		b.WriteString(fmt.Sprintf("%s:\n", incL))
		g.ldN++
		ild := fmt.Sprintf("%%_%s.ld%d", n.Var.Value, g.ldN)
		b.WriteString(fmt.Sprintf("  %s = load i32, i32* %%_%s\n", ild, n.Var.Value))
		itmp := g.newTmp()
		b.WriteString(fmt.Sprintf("  %s = add i32 %s, 1\n", itmp, ild))
		b.WriteString(fmt.Sprintf("  store i32 %s, i32* %%_%s\n", itmp, n.Var.Value))
		b.WriteString(fmt.Sprintf("  br label %%%s\n", condL))
		if len(n.Else) > 0 {
			b.WriteString(fmt.Sprintf("%s:\n", elseL))
			for _, s := range n.Else {
				if err := g.stmt(b, s); err != nil {
					return err
				}
			}
			b.WriteString(fmt.Sprintf("  br label %%%s\n", endL))
		}
		b.WriteString(fmt.Sprintf("%s:\n", endL))
	case *FuncDef:
		ci := g.closures[n.Name]
		if ci == nil {
			if err := g.funcDef(b, n); err != nil {
				return err
			}
			return nil
		}
		env := g.emitNewEnv(b)
		for i, c := range ci.captured {
			val := ""
			if pn, ok := g.params[c]; ok {
				val = pn
			} else {
				val = g.newTmp()
				fmt.Fprintf(b, "  %s = load i32, i32* %%_%s\n", val, c)
			}
			g.emitEnvStore(b, env, i, val)
		}
		fmt.Fprintf(b, "  store i32 %s, i32* @%s_slot\n", env, n.Name)
	case *ReturnStmt:
		v, err := g.value(b, n.Expr)
		if err != nil {
			return err
		}
		b.WriteString(fmt.Sprintf("  ret i32 %s\n", v))
	case *BreakStmt:
		if len(g.loopStack) == 0 {
			return fmt.Errorf("codegen: break outside loop")
		}
		info := g.loopStack[len(g.loopStack)-1]
		b.WriteString(fmt.Sprintf("  br label %%%s\n", info.breakLabel))
	case *ContinueStmt:
		if len(g.loopStack) == 0 {
			return fmt.Errorf("codegen: continue outside loop")
		}
		info := g.loopStack[len(g.loopStack)-1]
		b.WriteString(fmt.Sprintf("  br label %%%s\n", info.continueLabel))
	default:
		return fmt.Errorf("codegen: unsupported statement %T", st)
	}
	return nil
}
