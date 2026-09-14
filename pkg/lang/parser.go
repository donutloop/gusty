package lang

import "fmt"

// ParseError is a parsing error with a source span.
type ParseError struct {
	Span Span
	Msg  string
}

func (e *ParseError) Error() string {
	return fmt.Sprintf("parse error at %d:%d: %s", e.Span.Line, e.Span.Col, e.Msg)
}

type parser struct {
	src  string
	toks []Token
	pos  int
}

func newParser(src string, toks []Token) *parser { return &parser{src: src, toks: toks} }

func (p *parser) peek() Token  { return p.toks[p.pos] }
func (p *parser) atEOF() bool  { return p.peek().Kind == TokEOF }
func (p *parser) atNewline() bool { return p.peek().Kind == TokNewline }
func (p *parser) atDedent() bool  { return p.peek().Kind == TokDedent }
func (p *parser) atIndent() bool  { return p.peek().Kind == TokIndent }

func (p *parser) next() Token {
	t := p.toks[p.pos]
	if p.pos < len(p.toks)-1 {
		p.pos++
	}
	return t
}

func (p *parser) skipNewlines() {
	for p.atNewline() {
		p.next()
	}
}

// errorf reports a parse error at the given token span.
func (p *parser) errorf(t Token, msg string) error {
	return &ParseError{Span: t.Span, Msg: msg}
}

func (p *parser) expectOp(text string) error {
	t := p.peek()
	if !t.IsOp(text) {
		return p.errorf(t, fmt.Sprintf("expected %q", text))
	}
	p.next()
	return nil
}

func (p *parser) expectKeyword(text string) error {
	t := p.peek()
	if !t.IsKeyword(text) {
		return p.errorf(t, fmt.Sprintf("expected keyword %q", text))
	}
	p.next()
	return nil
}

func (p *parser) expectIdent() (string, error) {
	t := p.peek()
	if t.Kind != TokIdent {
		return "", p.errorf(t, "expected identifier")
	}
	p.next()
	return t.Text, nil
}

// parseProgram parses the whole module.
func parseProgram(src string) (*Program, error) {
	toks, err := Lex(src)
	if err != nil {
		return nil, err
	}
	p := newParser(src, toks)
	prog := &Program{}
	for !p.atEOF() {
		p.skipNewlines()
		if p.atEOF() {
			break
		}
		st, err := p.parseStmt()
		if err != nil {
			return nil, err
		}
		prog.Stmts = append(prog.Stmts, st)
	}
	return prog, nil
}

// parseStmt parses a single statement and consumes its trailing NEWLINE(s).
func (p *parser) parseStmt() (Stmt, error) {
	t := p.peek()
	if t.IsKeyword("def") {
		return p.parseFuncDef()
	}
	if t.IsKeyword("class") {
		return p.parseClassDef()
	}
	if t.IsKeyword("import") {
		return p.parseImport()
	}
	if t.IsKeyword("if") {
		return p.parseIf()
	}
	if t.IsKeyword("while") {
		return p.parseWhile()
	}
	if t.IsKeyword("for") {
		return p.parseFor()
	}
	if t.IsKeyword("match") {
		return p.parseMatch()
	}
	if t.IsKeyword("try") {
		return p.parseTry()
	}
	if t.IsKeyword("return") {
		return p.parseReturn()
	}
	if t.IsKeyword("yield") {
		return p.parseYield()
	}
	if t.IsKeyword("break") {
		p.next()
		return &BreakStmt{sp: t.Span}, nil
	}
	if t.IsKeyword("continue") {
		p.next()
		return &ContinueStmt{sp: t.Span}, nil
	}
	// expression / assignment
	return p.parseExprOrAssign()
}

// parseBlock parses an indented statement block after a colon.
func (p *parser) parseBlock(open Span) ([]Stmt, error) {
	// support single-line block: `if x: stmt` (no NEWLINE/INDENT)
	if !p.atNewline() {
		st, err := p.parseStmt()
		if err != nil {
			return nil, err
		}
		return []Stmt{st}, nil
	}
	// consume NEWLINE(s)
	p.skipNewlines()
	if !p.atIndent() {
		return nil, p.errorf(p.peek(), "expected indented block after ':'")
	}
	// consume INDENT
	p.next()
	var stmts []Stmt
	for !p.atDedent() && !p.atEOF() {
		p.skipNewlines()
		if p.atDedent() || p.atEOF() {
			break
		}
		st, err := p.parseStmt()
		if err != nil {
			return nil, err
		}
		stmts = append(stmts, st)
	}
	// consume DEDENT
	if p.atDedent() {
		p.next()
	}
	// consume trailing NEWLINE after block close if any
	for p.atNewline() {
		p.next()
	}
	return stmts, nil
}

func (p *parser) parseFuncDef() (Stmt, error) {
	def := p.next() // 'def'
	name, err := p.expectIdent()
	if err != nil {
		return nil, err
	}
	fd := &FuncDef{Name: name, sp: def.Span}
	if err := p.expectOp("("); err != nil {
		return nil, err
	}
	if !p.peek().IsOp(")") {
		for {
			param, err := p.parseParam()
			if err != nil {
				return nil, err
			}
			fd.Params = append(fd.Params, param)
			if p.peek().IsOp(",") {
				p.next()
				continue
			}
			break
		}
	}
	if err := p.expectOp(")"); err != nil {
		return nil, err
	}
	// optional -> return type
	if p.peek().IsOp("->") {
		p.next()
		ty, err := p.parseTypeAnnot()
		if err != nil {
			return nil, err
		}
		fd.ReturnAnno = ty
	}
	if err := p.expectOp(":"); err != nil {
		return nil, err
	}
	body, err := p.parseBlock(def.Span)
	if err != nil {
		return nil, err
	}
	fd.Body = body
	return fd, nil
}

func (p *parser) parseParam() (*Param, error) {
	t := p.peek()
	name, err := p.expectIdent()
	if err != nil {
		return nil, err
	}
	param := &Param{Name: name, sp: t.Span}
	// optional : type
	if p.peek().IsOp(":") {
		p.next()
		ty, err := p.parseTypeAnnot()
		if err != nil {
			return nil, err
		}
		param.Annot = ty
	}
	// optional = default
	if p.peek().IsOp("=") {
		p.next()
		def, err := p.parseExpr()
		if err != nil {
			return nil, err
		}
		param.Default = def
	}
	return param, nil
}

func (p *parser) parseClassDef() (Stmt, error) {
	cls := p.next() // 'class'
	name, err := p.expectIdent()
	if err != nil {
		return nil, err
	}
	cd := &ClassDef{Name: name, sp: cls.Span}
	// optional base list in parens
	if p.peek().IsOp("(") {
		p.next()
		for !p.peek().IsOp(")") {
			t := p.peek()
			if t.Kind != TokIdent {
				return nil, p.errorf(t, "expected base class name")
			}
			p.next()
			cd.Bases = append(cd.Bases, &Name{Value: t.Text, sp: t.Span})
			if p.peek().IsOp(",") {
				p.next()
				continue
			}
			break
		}
		if err := p.expectOp(")"); err != nil {
			return nil, err
		}
	}
	if err := p.expectOp(":"); err != nil {
		return nil, err
	}
	body, err := p.parseBlock(cls.Span)
	if err != nil {
		return nil, err
	}
	cd.Body = body
	return cd, nil
}

func (p *parser) parseImport() (Stmt, error) {
	im := p.next() // 'import'
	name, err := p.expectIdent()
	if err != nil {
		return nil, err
	}
	p.skipNewlines()
	return &ImportStmt{Module: name, sp: im.Span}, nil
}

func (p *parser) parseReturn() (Stmt, error) {
	rt := p.next() // 'return'
	rs := &ReturnStmt{sp: rt.Span}
	if !p.atNewline() && !p.atEOF() && !p.atDedent() {
		ex, err := p.parseExpr()
		if err != nil {
			return nil, err
		}
		rs.Expr = ex
	}
	p.skipNewlines()
	return rs, nil
}

func (p *parser) parseYield() (Stmt, error) {
	yt := p.next() // 'yield'
	ys := &YieldStmt{sp: yt.Span}
	if !p.atNewline() && !p.atEOF() && !p.atDedent() {
		ex, err := p.parseExpr()
		if err != nil {
			return nil, err
		}
		ys.Expr = ex
	}
	p.skipNewlines()
	return ys, nil
}

func (p *parser) parseIf() (Stmt, error) {
	kw := p.next() // 'if'
	cond, err := p.parseExpr()
	if err != nil {
		return nil, err
	}
	if err := p.expectOp(":"); err != nil {
		return nil, err
	}
	body, err := p.parseBlock(kw.Span)
	if err != nil {
		return nil, err
	}
	isf := &IfStmt{Cond: cond, Then: body, sp: kw.Span}
	// elif / else at the same level
	for {
		p.skipNewlines()
		t := p.peek()
		if t.IsKeyword("elif") {
			p.next()
			cond2, err := p.parseExpr()
			if err != nil {
				return nil, err
			}
			if err := p.expectOp(":"); err != nil {
				return nil, err
			}
			body2, err := p.parseBlock(t.Span)
			if err != nil {
				return nil, err
			}
			isf.Elifs = append(isf.Elifs, &IfStmt{Cond: cond2, Then: body2, sp: t.Span})
			continue
		}
		if t.IsKeyword("else") {
			p.next()
			if err := p.expectOp(":"); err != nil {
				return nil, err
			}
			body3, err := p.parseBlock(t.Span)
			if err != nil {
				return nil, err
			}
			isf.Else = body3
		}
		break
	}
	return isf, nil
}

func (p *parser) parseWhile() (Stmt, error) {
	kw := p.next() // 'while'
	cond, err := p.parseExpr()
	if err != nil {
		return nil, err
	}
	if err := p.expectOp(":"); err != nil {
		return nil, err
	}
	body, err := p.parseBlock(kw.Span)
	if err != nil {
		return nil, err
	}
	elseBody, err := p.parseLoopElse(kw.Span)
	if err != nil {
		return nil, err
	}
	return &WhileStmt{Cond: cond, Body: body, Else: elseBody, sp: kw.Span}, nil
}

func (p *parser) parseFor() (Stmt, error) {
	kw := p.next() // 'for'
	t := p.peek()
	if t.Kind != TokIdent {
		return nil, p.errorf(t, "expected loop variable")
	}
	p.next()
	varName := &Name{Value: t.Text, sp: t.Span}
	if err := p.expectKeyword("in"); err != nil {
		return nil, err
	}
	iter, err := p.parseExpr()
	if err != nil {
		return nil, err
	}
	if err := p.expectOp(":"); err != nil {
		return nil, err
	}
	body, err := p.parseBlock(kw.Span)
	if err != nil {
		return nil, err
	}
	elseBody, err := p.parseLoopElse(kw.Span)
	if err != nil {
		return nil, err
	}
	return &ForStmt{Var: varName, Iter: iter, Body: body, Else: elseBody, sp: kw.Span}, nil
}

// parseLoopElse parses an optional `else:` block following a while/for loop,
// returning nil when no else clause is present.
func (p *parser) parseLoopElse(kw Span) ([]Stmt, error) {
	p.skipNewlines()
	t := p.peek()
	if !t.IsKeyword("else") {
		return nil, nil
	}
	p.next()
	if err := p.expectOp(":"); err != nil {
		return nil, err
	}
	return p.parseBlock(t.Span)
}

func (p *parser) parseMatch() (Stmt, error) {
	kw := p.next() // 'match'
	subj, err := p.parseExpr()
	if err != nil {
		return nil, err
	}
	if err := p.expectOp(":"); err != nil {
		return nil, err
	}
	ms := &MatchStmt{Subject: subj, sp: kw.Span}
	// cases are indented blocks each starting with 'case'
	if !p.atIndent() {
		p.skipNewlines()
	}
	if p.atIndent() {
		p.next() // INDENT
	}
	for !p.atDedent() && !p.atEOF() {
		p.skipNewlines()
		t := p.peek()
		if !t.IsKeyword("case") {
			break
		}
		p.next()
		pat, err := p.parseExpr()
		if err != nil {
			return nil, err
		}
		if err := p.expectOp(":"); err != nil {
			return nil, err
		}
		body, err := p.parseBlock(t.Span)
		if err != nil {
			return nil, err
		}
		ms.Cases = append(ms.Cases, &MatchCase{Pattern: pat, Body: body, sp: t.Span})
	}
	if p.atDedent() {
		p.next()
	}
	return ms, nil
}

func (p *parser) parseTry() (Stmt, error) {
	kw := p.next() // 'try'
	if err := p.expectOp(":"); err != nil {
		return nil, err
	}
	body, err := p.parseBlock(kw.Span)
	if err != nil {
		return nil, err
	}
	ts := &TryStmt{Body: body, sp: kw.Span}
	// except / finally clauses
	for {
		p.skipNewlines()
		t := p.peek()
		if t.IsKeyword("except") {
			p.next()
			ec := &ExceptClause{sp: t.Span}
			if p.peek().Kind == TokIdent {
				nt := p.next()
				ec.Exn = &Name{Value: nt.Text, sp: nt.Span}
			}
			if err := p.expectOp(":"); err != nil {
				return nil, err
			}
			eb, err := p.parseBlock(t.Span)
			if err != nil {
				return nil, err
			}
			ec.Body = eb
			ts.Excepts = append(ts.Excepts, ec)
			continue
		}
		if t.IsKeyword("finally") {
			p.next()
			if err := p.expectOp(":"); err != nil {
				return nil, err
			}
			fb, err := p.parseBlock(t.Span)
			if err != nil {
				return nil, err
			}
			ts.Finally = fb
		}
		break
	}
	return ts, nil
}

// parseExprOrAssign parses an expression statement or an assignment.
func (p *parser) parseExprOrAssign() (Stmt, error) {
	// detect simple name assignment: IDENT [= | : type =]
	if t := p.peek(); t.Kind == TokIdent {
		// lookahead: next significant token
		i := p.pos + 1
		for i < len(p.toks) && p.toks[i].Kind == TokNewline {
			i++
		}
		if i < len(p.toks) && (p.toks[i].IsOp("=") || p.toks[i].IsOp(":")) {
			p.next() // ident
			name := &Name{Value: t.Text, sp: t.Span}
			as := &AssignStmt{Target: name, sp: t.Span}
			// optional : type
			if p.peek().IsOp(":") {
				p.next()
				ty, err := p.parseTypeAnnot()
				if err != nil {
					return nil, err
				}
				as.Annot = ty
			}
			if err := p.expectOp("="); err != nil {
				return nil, err
			}
			val, err := p.parseExpr()
			if err != nil {
				return nil, err
			}
			as.Value = val
			p.skipNewlines()
			return as, nil
		}
	}
	ex, err := p.parseExpr()
	if err != nil {
		return nil, err
	}
	p.skipNewlines()
	return &ExprStmt{Expr: ex, sp: ex.Span()}, nil
}

// parseTypeAnnot parses a type annotation token (int/float/bool/str/any).
func (p *parser) parseTypeAnnot() (*Type, error) {
	t := p.peek()
	p.next()
	switch t.Text {
	case "int":
		return TInt(), nil
	case "float":
		return TFlt(), nil
	case "bool":
		return TBool(), nil
	case "str":
		return TStr(), nil
	case "any":
		return TDyn(), nil
	default:
		return nil, p.errorf(t, "unknown type annotation "+t.Text)
	}
}

// --- expression parsing (precedence climbing) ---

func (p *parser) parseExpr() (Expr, error) {
	return p.parseOr()
}

func (p *parser) parseOr() (Expr, error) {
	l, err := p.parseAnd()
	if err != nil {
		return nil, err
	}
	for p.peek().IsKeyword("or") {
		op := p.next()
		r, err := p.parseAnd()
		if err != nil {
			return nil, err
		}
		l = &BinOp{Op: "or", L: l, R: r, sp: op.Span}
	}
	return l, nil
}

func (p *parser) parseAnd() (Expr, error) {
	l, err := p.parseNot()
	if err != nil {
		return nil, err
	}
	for p.peek().IsKeyword("and") {
		op := p.next()
		r, err := p.parseNot()
		if err != nil {
			return nil, err
		}
		l = &BinOp{Op: "and", L: l, R: r, sp: op.Span}
	}
	return l, nil
}

func (p *parser) parseNot() (Expr, error) {
	if p.peek().IsKeyword("not") {
		op := p.next()
		x, err := p.parseNot()
		if err != nil {
			return nil, err
		}
		return &UnOp{Op: "not", X: x, sp: op.Span}, nil
	}
	return p.parseComparison()
}

func (p *parser) parseComparison() (Expr, error) {
	l, err := p.parseAdditive()
	if err != nil {
		return nil, err
	}
	for {
		t := p.peek()
		if !t.IsOp("==") && !t.IsOp("!=") && !t.IsOp("<") && !t.IsOp("<=") && !t.IsOp(">") && !t.IsOp(">=") {
			break
		}
		p.next()
		r, err := p.parseAdditive()
		if err != nil {
			return nil, err
		}
		l = &BinOp{Op: t.Text, L: l, R: r, sp: t.Span}
	}
	return l, nil
}

func (p *parser) parseAdditive() (Expr, error) {
	l, err := p.parseMultiplicative()
	if err != nil {
		return nil, err
	}
	for {
		t := p.peek()
		if !t.IsOp("+") && !t.IsOp("-") {
			break
		}
		p.next()
		r, err := p.parseMultiplicative()
		if err != nil {
			return nil, err
		}
		l = &BinOp{Op: t.Text, L: l, R: r, sp: t.Span}
	}
	return l, nil
}

func (p *parser) parseMultiplicative() (Expr, error) {
	l, err := p.parseUnary()
	if err != nil {
		return nil, err
	}
	for {
		t := p.peek()
		if !t.IsOp("*") && !t.IsOp("/") && !t.IsOp("//") && !t.IsOp("%") {
			break
		}
		p.next()
		r, err := p.parseUnary()
		if err != nil {
			return nil, err
		}
		l = &BinOp{Op: t.Text, L: l, R: r, sp: t.Span}
	}
	return l, nil
}

func (p *parser) parseUnary() (Expr, error) {
	if p.peek().IsOp("-") {
		op := p.next()
		x, err := p.parseUnary()
		if err != nil {
			return nil, err
		}
		return &UnOp{Op: "-", X: x, sp: op.Span}, nil
	}
	return p.parsePostfix()
}

func (p *parser) parsePostfix() (Expr, error) {
	x, err := p.parseAtom()
	if err != nil {
		return nil, err
	}
	for {
		t := p.peek()
		if t.IsOp("(") {
			p.next()
			var args []Expr
			if !p.peek().IsOp(")") {
				for {
					a, err := p.parseExpr()
					if err != nil {
						return nil, err
					}
					args = append(args, a)
					if p.peek().IsOp(",") {
						p.next()
						continue
					}
					break
				}
			}
			if err := p.expectOp(")"); err != nil {
				return nil, err
			}
			x = &Call{Fn: x, Args: args, sp: t.Span}
			continue
		}
		if t.IsOp("[") {
			p.next()
			idx, err := p.parseExpr()
			if err != nil {
				return nil, err
			}
			if err := p.expectOp("]"); err != nil {
				return nil, err
			}
			x = &Index{Obj: x, Idx: idx, sp: t.Span}
			continue
		}
		if t.IsOp(".") {
			p.next()
			nt := p.peek()
			if nt.Kind != TokIdent {
				return nil, p.errorf(nt, "expected attribute name")
			}
			p.next()
			x = &Attr{Obj: x, Name: &Name{Value: nt.Text, sp: nt.Span}, sp: t.Span}
			continue
		}
		break
	}
	return x, nil
}

func (p *parser) parseAtom() (Expr, error) {
	t := p.peek()
	switch {
	case t.Kind == TokInt:
		p.next()
		return &IntLit{Value: t.Int, sp: t.Span}, nil
	case t.Kind == TokFloat:
		p.next()
		return &FloatLit{Value: t.Float, sp: t.Span}, nil
	case t.Kind == TokString:
		p.next()
		return &StrLit{Value: t.Str, sp: t.Span}, nil
	case t.Kind == TokKeyword && t.Text == "True":
		p.next()
		return &BoolLit{Value: true, sp: t.Span}, nil
	case t.Kind == TokKeyword && t.Text == "False":
		p.next()
		return &BoolLit{Value: false, sp: t.Span}, nil
	case t.Kind == TokKeyword && t.Text == "None":
		p.next()
		return &NoneLit{sp: t.Span}, nil
	case t.Kind == TokKeyword && (t.Text == "print" || t.Text == "range"):
		p.next()
		return &Name{Value: t.Text, sp: t.Span}, nil
	case t.Kind == TokKeyword && t.Text == "lambda":
		return p.parseLambda()
	case t.Kind == TokIdent:
		p.next()
		return &Name{Value: t.Text, sp: t.Span}, nil
	case t.IsOp("("):
		p.next()
		ex, err := p.parseExpr()
		if err != nil {
			return nil, err
		}
		if err := p.expectOp(")"); err != nil {
			return nil, err
		}
		return ex, nil
	case t.IsOp("["):
		return p.parseListOrComp()
	case t.IsOp("{"):
		return p.parseDictOrSet()
	}
	return nil, p.errorf(t, "unexpected token")
}

func (p *parser) parseLambda() (Expr, error) {
	t := p.next() // 'lambda'
	lm := &Lambda{sp: t.Span}
	if p.peek().Kind == TokIdent {
		for {
			param, err := p.parseParam()
			if err != nil {
				return nil, err
			}
			lm.Params = append(lm.Params, param)
			if p.peek().IsOp(",") {
				p.next()
				continue
			}
			break
		}
	}
	if err := p.expectOp(":"); err != nil {
		return nil, err
	}
	body, err := p.parseExpr()
	if err != nil {
		return nil, err
	}
	lm.Body = body
	return lm, nil
}

func (p *parser) parseListOrComp() (Expr, error) {
	t := p.next() // '['
	var elems []Expr
	for !p.peek().IsOp("]") {
		ex, err := p.parseExpr()
		if err != nil {
			return nil, err
		}
		elems = append(elems, ex)
		if p.peek().IsOp(",") {
			p.next()
			continue
		}
		break
	}
	// comprehension: `[x for x in iter if cond]`
	if p.peek().IsKeyword("for") {
		p.next()
		v := p.peek()
		if v.Kind != TokIdent {
			return nil, p.errorf(v, "expected comprehension variable")
		}
		p.next()
		if err := p.expectKeyword("in"); err != nil {
			return nil, err
		}
		iter, err := p.parseExpr()
		if err != nil {
			return nil, err
		}
		c := &Comp{Kind: CompList, Elems: elems, ForVar: &Name{Value: v.Text, sp: v.Span}, Iter: iter, sp: t.Span}
		if p.peek().IsKeyword("if") {
			p.next()
			cond, err := p.parseExpr()
			if err != nil {
				return nil, err
			}
			c.Cond = cond
		}
		if err := p.expectOp("]"); err != nil {
			return nil, err
		}
		return c, nil
	}
	if err := p.expectOp("]"); err != nil {
		return nil, err
	}
	return &ListLit{Elems: elems, sp: t.Span}, nil
}

func (p *parser) parseDictOrSet() (Expr, error) {
	t := p.next() // '{'
	var keys, vals []Expr
	var elems []Expr
	isDict := false
	if !p.peek().IsOp("}") {
		first, err := p.parseExpr()
		if err != nil {
			return nil, err
		}
		if p.peek().IsOp(":") {
			isDict = true
			p.next()
			val, err := p.parseExpr()
			if err != nil {
				return nil, err
			}
			keys = append(keys, first)
			vals = append(vals, val)
			for p.peek().IsOp(",") {
				p.next()
				k, err := p.parseExpr()
				if err != nil {
					return nil, err
				}
				if err := p.expectOp(":"); err != nil {
					return nil, err
				}
				v, err := p.parseExpr()
				if err != nil {
					return nil, err
				}
				keys = append(keys, k)
				vals = append(vals, v)
			}
		} else {
			elems = append(elems, first)
			for p.peek().IsOp(",") {
				p.next()
				e, err := p.parseExpr()
				if err != nil {
					return nil, err
				}
				elems = append(elems, e)
			}
		}
	}
	if err := p.expectOp("}"); err != nil {
		return nil, err
	}
	if isDict {
		return &DictLit{Keys: keys, Vals: vals, sp: t.Span}, nil
	}
	return &SetLit{Elems: elems, sp: t.Span}, nil
}
