package lang

import (
	"strconv"
	"strings"
)

// Format renders prog as canonical gusty source. It is deterministic and
// idempotent: Format(Parse(Format(src))) == Format(src).
func Format(prog *Program) string {
	var sb strings.Builder
	for i, st := range prog.Stmts {
		if i > 0 {
			sb.WriteByte('\n')
		}
		writeStmt(&sb, st, 0)
	}
	return sb.String()
}

// FormatSrc parses src and returns its canonical formatting.
func FormatSrc(src string) (string, error) {
	prog, err := Parse(src)
	if err != nil {
		return "", err
	}
	return Format(prog), nil
}

// IsFormatted reports whether src is already canonical.
func IsFormatted(src string) (bool, error) {
	f, err := FormatSrc(src)
	if err != nil {
		return false, err
	}
	return f == src, nil
}

func indent(sb *strings.Builder, n int) {
	for i := 0; i < n; i++ {
		sb.WriteString("  ")
	}
}

func writeStmt(sb *strings.Builder, st Stmt, depth int) {
	switch s := st.(type) {
	case *ExprStmt:
		indent(sb, depth)
		writeExpr(sb, s.Expr, 0)
	case *ReturnStmt:
		indent(sb, depth)
		if s.Expr != nil {
			sb.WriteString("return ")
			writeExpr(sb, s.Expr, 0)
		} else {
			sb.WriteString("return")
		}
	case *RaiseStmt:
		indent(sb, depth)
		sb.WriteString("raise")
		if s.Expr != nil {
			sb.WriteByte(' ')
			writeExpr(sb, s.Expr, 0)
		}
	case *AssignStmt:
		indent(sb, depth)
		writeExpr(sb, s.Target, 0)
		sb.WriteString(" = ")
		writeExpr(sb, s.Value, 0)
	case *AugAssignStmt:
		indent(sb, depth)
		writeExpr(sb, s.Target, 0)
		sb.WriteByte(' ')
		sb.WriteString(s.Op)
		sb.WriteByte(' ')
		writeExpr(sb, s.Value, 0)
	case *ImportStmt:
		indent(sb, depth)
		sb.WriteString("import ")
		sb.WriteString(s.Module)
	case *IfStmt:
		writeIf(sb, s, depth)
	case *WhileStmt:
		indent(sb, depth)
		sb.WriteString("while ")
		writeExpr(sb, s.Cond, 0)
		sb.WriteString(":\n")
		writeBlock(sb, s.Body, depth+1)
		writeLoopElse(sb, s.Else, depth)
	case *ForStmt:
		indent(sb, depth)
		sb.WriteString("for ")
		writeExpr(sb, s.Var, 0)
		sb.WriteString(" in ")
		writeExpr(sb, s.Iter, 0)
		sb.WriteString(":\n")
		writeBlock(sb, s.Body, depth+1)
		writeLoopElse(sb, s.Else, depth)
	case *FuncDef:
		writeFuncDef(sb, s, depth)
	case *ClassDef:
		writeClassDef(sb, s, depth)
	case *MatchStmt:
		writeMatch(sb, s, depth)
	case *TryStmt:
		writeTry(sb, s, depth)
	case *WithStmt:
		indent(sb, depth)
		sb.WriteString("with ")
		writeExpr(sb, s.Expr, 0)
		if s.As != nil {
			sb.WriteString(" as ")
			writeExpr(sb, s.As, 0)
		}
		sb.WriteString(":\n")
		writeBlock(sb, s.Body, depth+1)
	case *YieldStmt:
		indent(sb, depth)
		if s.Expr != nil {
			sb.WriteString("yield ")
			writeExpr(sb, s.Expr, 0)
		} else {
			sb.WriteString("yield")
		}
	case *YieldFromStmt:
		indent(sb, depth)
		sb.WriteString("yield from ")
		writeExpr(sb, s.Expr, 0)
	case *BreakStmt:
		indent(sb, depth)
		sb.WriteString("break")
	case *ContinueStmt:
		indent(sb, depth)
		sb.WriteString("continue")
	case *PassStmt:
		indent(sb, depth)
		sb.WriteString("pass")
	default:
		indent(sb, depth)
		sb.WriteString("pass")
	}
}

func writeBlock(sb *strings.Builder, body []Stmt, depth int) {
	if len(body) == 0 {
		indent(sb, depth)
		sb.WriteString("pass")
		return
	}
	for i, st := range body {
		if i > 0 {
			sb.WriteByte('\n')
		}
		writeStmt(sb, st, depth)
	}
}

func writeLoopElse(sb *strings.Builder, else_ []Stmt, depth int) {
	if len(else_) == 0 {
		return
	}
	sb.WriteByte('\n')
	indent(sb, depth)
	sb.WriteString("else:\n")
	writeBlock(sb, else_, depth+1)
}

func writeIf(sb *strings.Builder, s *IfStmt, depth int) {
	indent(sb, depth)
	sb.WriteString("if ")
	writeExpr(sb, s.Cond, 0)
	sb.WriteString(":\n")
	writeBlock(sb, s.Then, depth+1)
	for _, el := range s.Elifs {
		sb.WriteByte('\n')
		indent(sb, depth)
		sb.WriteString("elif ")
		writeExpr(sb, el.Cond, 0)
		sb.WriteString(":\n")
		writeBlock(sb, el.Then, depth+1)
	}
	if len(s.Else) > 0 {
		sb.WriteByte('\n')
		indent(sb, depth)
		sb.WriteString("else:\n")
		writeBlock(sb, s.Else, depth+1)
	}
}

func writeFuncDef(sb *strings.Builder, s *FuncDef, depth int) {
	for _, dec := range s.Decorators {
		indent(sb, depth)
		sb.WriteByte('@')
		writeExpr(sb, dec, 0)
		sb.WriteByte('\n')
	}
	indent(sb, depth)
	sb.WriteString("def ")
	sb.WriteString(s.Name)
	sb.WriteByte('(')
	writeParams(sb, s.Params)
	sb.WriteByte(')')
	if s.ReturnAnno != nil {
		sb.WriteString(" -> ")
		sb.WriteString(s.ReturnAnno.Name())
	}
	sb.WriteString(":\n")
	writeBlock(sb, s.Body, depth+1)
}

func writeClassDef(sb *strings.Builder, s *ClassDef, depth int) {
	indent(sb, depth)
	sb.WriteString("class ")
	sb.WriteString(s.Name)
	sb.WriteByte('(')
	for i, b := range s.Bases {
		if i > 0 {
			sb.WriteString(", ")
		}
		writeExpr(sb, b, 0)
	}
	sb.WriteByte(')')
	sb.WriteString(":\n")
	writeBlock(sb, s.Body, depth+1)
}

func writeMatch(sb *strings.Builder, s *MatchStmt, depth int) {
	indent(sb, depth)
	sb.WriteString("match ")
	writeExpr(sb, s.Subject, 0)
	sb.WriteString(":\n")
	for _, c := range s.Cases {
		sb.WriteByte('\n')
		indent(sb, depth+1)
		sb.WriteString("case ")
		writeExpr(sb, c.Pattern, 0)
		if c.Guard != nil {
			sb.WriteString(" if ")
			writeExpr(sb, c.Guard, 0)
		}
		sb.WriteString(":\n")
		writeBlock(sb, c.Body, depth+2)
	}
}

func writeTry(sb *strings.Builder, s *TryStmt, depth int) {
	indent(sb, depth)
	sb.WriteString("try:\n")
	writeBlock(sb, s.Body, depth+1)
	for _, e := range s.Excepts {
		sb.WriteByte('\n')
		indent(sb, depth)
		sb.WriteString("except")
		if e.Exn != nil {
			sb.WriteByte(' ')
			writeExpr(sb, e.Exn, 0)
		}
		sb.WriteString(":\n")
		writeBlock(sb, e.Body, depth+1)
	}
	if len(s.Finally) > 0 {
		sb.WriteByte('\n')
		indent(sb, depth)
		sb.WriteString("finally:\n")
		writeBlock(sb, s.Finally, depth+1)
	}
}

func writeParams(sb *strings.Builder, ps []*Param) {
	for i, p := range ps {
		if i > 0 {
			sb.WriteString(", ")
		}
		sb.WriteString(p.Name)
		if p.Annot != nil {
			sb.WriteString(": ")
			sb.WriteString(p.Annot.Name())
		}
		if p.Default != nil {
			sb.WriteString(" = ")
			writeExpr(sb, p.Default, 0)
		}
	}
}

func writeExprList(sb *strings.Builder, elems []Expr) {
	for i, e := range elems {
		if i > 0 {
			sb.WriteString(", ")
		}
		writeExpr(sb, e, 0)
	}
}

func writeExpr(sb *strings.Builder, e Expr, prec int) {
	switch x := e.(type) {
	case *Name:
		sb.WriteString(x.Value)
	case *IntLit:
		sb.WriteString(strconv.FormatInt(x.Value, 10))
	case *FloatLit:
		s := strconv.FormatFloat(x.Value, 'f', -1, 64)
		if !strings.ContainsAny(s, ".") {
			s += ".0"
		}
		sb.WriteString(s)
	case *BoolLit:
		if x.Value {
			sb.WriteString("True")
		} else {
			sb.WriteString("False")
		}
	case *NoneLit:
		sb.WriteString("None")
	case *StrLit:
		sb.WriteString(quoteString(x.Value))
	case *KeywordArg:
		sb.WriteString(x.Name)
		sb.WriteString(" = ")
		writeExpr(sb, x.Value, 0)
	case *Tuple:
		sb.WriteByte('(')
		writeExprList(sb, x.Elems)
		sb.WriteByte(')')
	case *ListLit:
		sb.WriteByte('[')
		writeExprList(sb, x.Elems)
		sb.WriteByte(']')
	case *SetLit:
		sb.WriteByte('{')
		writeExprList(sb, x.Elems)
		sb.WriteByte('}')
	case *DictLit:
		sb.WriteByte('{')
		for i := 0; i < len(x.Keys); i++ {
			if i > 0 {
				sb.WriteString(", ")
			}
			writeExpr(sb, x.Keys[i], 0)
			sb.WriteString(": ")
			writeExpr(sb, x.Vals[i], 0)
		}
		sb.WriteByte('}')
	case *BinOp:
		opPrec := binPrec(x.Op)
		if prec > opPrec {
			sb.WriteByte('(')
		}
		writeExpr(sb, x.L, opPrec)
		sb.WriteByte(' ')
		sb.WriteString(x.Op)
		sb.WriteByte(' ')
		writeExpr(sb, x.R, opPrec+1)
		if prec > opPrec {
			sb.WriteByte(')')
		}
	case *UnOp:
		if prec > 4 {
			sb.WriteByte('(')
		}
		sb.WriteString(x.Op)
		writeExpr(sb, x.X, 4)
		if prec > 4 {
			sb.WriteByte(')')
		}
	case *CondExpr:
		if prec > 2 {
			sb.WriteByte('(')
		}
		writeExpr(sb, x.If, 2)
		sb.WriteString(" if ")
		writeExpr(sb, x.Cond, 0)
		sb.WriteString(" else ")
		writeExpr(sb, x.Else, 2)
		if prec > 2 {
			sb.WriteByte(')')
		}
	case *Call:
		writeExpr(sb, x.Fn, 5)
		sb.WriteByte('(')
		writeArgs(sb, x.Args)
		sb.WriteByte(')')
	case *Index:
		writeExpr(sb, x.Obj, 5)
		sb.WriteByte('[')
		writeExpr(sb, x.Idx, 0)
		sb.WriteByte(']')
	case *Attr:
		writeExpr(sb, x.Obj, 5)
		sb.WriteByte('.')
		writeExpr(sb, x.Name, 0)
	case *Slice:
		writeExpr(sb, x.Obj, 5)
		sb.WriteByte('[')
		if x.Low != nil {
			writeExpr(sb, x.Low, 0)
		}
		sb.WriteByte(':')
		if x.High != nil {
			writeExpr(sb, x.High, 0)
		}
		if x.Step != nil {
			sb.WriteByte(':')
			writeExpr(sb, x.Step, 0)
		}
		sb.WriteByte(']')
	case *Lambda:
		sb.WriteString("lambda ")
		writeParams(sb, x.Params)
		sb.WriteString(": ")
		writeExpr(sb, x.Body, 0)
	case *Comp:
		writeComp(sb, x)
	case *Generator:
		sb.WriteByte('(')
		writeExprList(sb, x.Elems)
		sb.WriteString(" for ")
		writeExpr(sb, x.ForVar, 0)
		sb.WriteString(" in ")
		writeExpr(sb, x.Iter, 0)
		if x.Cond != nil {
			sb.WriteString(" if ")
			writeExpr(sb, x.Cond, 0)
		}
		sb.WriteByte(')')
	case *FString:
		sb.WriteString("f\"")
		for _, part := range x.Parts {
			if part.Lit != "" {
				sb.WriteString(escapeFString(part.Lit))
			} else {
				sb.WriteByte('{')
				writeExpr(sb, part.Expr, 0)
				sb.WriteByte('}')
			}
		}
		sb.WriteByte('"')
	default:
		sb.WriteString("None")
	}
}

func writeComp(sb *strings.Builder, x *Comp) {
	switch x.Kind {
	case CompList:
		sb.WriteByte('[')
		writeExprList(sb, x.Elems)
		writeCompTail(sb, x)
		sb.WriteByte(']')
	case CompSet:
		sb.WriteByte('{')
		writeExprList(sb, x.Elems)
		writeCompTail(sb, x)
		sb.WriteByte('}')
	case CompDict:
		sb.WriteByte('{')
		for i := 0; i < len(x.Keys); i++ {
			if i > 0 {
				sb.WriteString(", ")
			}
			writeExpr(sb, x.Keys[i], 0)
			sb.WriteString(": ")
			writeExpr(sb, x.Vals[i], 0)
		}
		writeCompTail(sb, x)
		sb.WriteByte('}')
	}
}

func writeCompTail(sb *strings.Builder, x *Comp) {
	sb.WriteString(" for ")
	writeExpr(sb, x.ForVar, 0)
	sb.WriteString(" in ")
	writeExpr(sb, x.Iter, 0)
	if x.Cond != nil {
		sb.WriteString(" if ")
		writeExpr(sb, x.Cond, 0)
	}
}

func writeArgs(sb *strings.Builder, args []Expr) {
	for i, a := range args {
		if i > 0 {
			sb.WriteString(", ")
		}
		writeExpr(sb, a, 0)
	}
}

func quoteString(s string) string {
	var sb strings.Builder
	sb.WriteByte('"')
	for _, r := range s {
		switch r {
		case '"':
			sb.WriteString(`\"`)
		case '\\':
			sb.WriteString(`\\`)
		case '\n':
			sb.WriteString(`\n`)
		case '\t':
			sb.WriteString(`\t`)
		case '\r':
			sb.WriteString(`\r`)
		default:
			sb.WriteRune(r)
		}
	}
	sb.WriteByte('"')
	return sb.String()
}

func escapeFString(s string) string {
	var sb strings.Builder
	for _, r := range s {
		switch r {
		case '"':
			sb.WriteString(`\"`)
		case '\\':
			sb.WriteString(`\\`)
		case '{':
			sb.WriteString(`{{`)
		case '}':
			sb.WriteString(`}}`)
		default:
			sb.WriteRune(r)
		}
	}
	return sb.String()
}

func binPrec(op string) int {
	switch op {
	case "or":
		return 1
	case "and":
		return 2
	case "==", "!=", "<", "<=", ">", ">=", "in", "is":
		return 3
	case "+", "-":
		return 4
	case "*", "/", "//", "%":
		return 5
	case "**":
		return 6
	}
	return 4
}
