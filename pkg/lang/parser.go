package lang

import (
	"fmt"
	"strings"
)

// ParseError is a parsing error with a source span.
type ParseError struct {
	Span Span
	Msg  string
}

func (e *ParseError) Error() string {
	return fmt.Sprintf("parse error at %d:%d: %s", e.Span.Line, e.Span.Col, e.Msg)
}

// ParseErrors is an aggregate of one or more parse errors produced by
// panic-mode error recovery: the parser skips past a bad statement and keeps
// parsing, so a single run can surface a forest of diagnostics rather than
// just the first one.
type ParseErrors struct {
	Errors []*ParseError
}

func (pe *ParseErrors) Error() string {
	if len(pe.Errors) == 0 {
		return "parse error"
	}
	if len(pe.Errors) == 1 {
		return pe.Errors[0].Error()
	}
	var b strings.Builder
	fmt.Fprintf(&b, "%d parse errors", len(pe.Errors))
	for _, e := range pe.Errors {
		b.WriteString("\n\t")
		b.WriteString(e.Error())
	}
	return b.String()
}

type parser struct {
	src  string
	cur  Cursor
	nest int // currently-open block depth (unconsumed DEDENTs); used by panic-mode recovery
	// inListLit counts the `[` displays whose element list this expression is being parsed inside.
	// A `{…}` display may finish itself into a comprehension when a `for` follows its `}` — that is
	// the call-argument genexp form `len({x*x} for x in xs)` — but not when the display is an
	// *element* of a list display: there the `for` belongs to the enclosing `[`, and stealing it made
	// `[{1, 2} for x in xs]` a list of one set comprehension instead of a list comprehension over
	// set literals (roadmap Gap R.74, ADR 0244).
	inListLit int
	// typeAliases records compile-time structural type aliases (`type X = T`)
	// so later annotations resolve them structurally (L5.7).
	typeAliases map[string]*Type
	// classNames records the declared classes of the module (pre-scanned from the
	// token stream) so an annotation may name a user class — `a: Animal` — which
	// the checker then treats nominally (L6.6).
	classNames map[string]bool
}

func newParser(src string, toks []Token) *parser {
	return &parser{src: src, cur: *NewCursor(toks), typeAliases: map[string]*Type{}, classNames: scanClassNames(toks)}
}

// scanClassNames pre-scans the token stream for `class NAME` declarations so an
// annotation can name a user class no matter where it is declared (L6.6). It
// reads tokens only — it never consumes the parser's cursor.
func scanClassNames(toks []Token) map[string]bool {
	names := map[string]bool{}
	for i := 0; i+1 < len(toks); i++ {
		if toks[i].IsKeyword("class") && toks[i+1].Kind == TokIdent {
			names[toks[i+1].Text] = true
		}
	}
	return names
}

// The parser walks the token stream through the shared Cursor abstraction
// (token.go) — the single source of truth for spans. These are thin wrappers
// so the parser reads the same stream as the formatter and LSP.
func (p *parser) peek() Token     { return p.cur.peek(0) }
func (p *parser) peekNext() Token { return p.cur.peek(1) }
func (p *parser) atEOF() bool     { return p.cur.atEOF() }
func (p *parser) atNewline() bool { return p.cur.atNewline() }
func (p *parser) atDedent() bool  { return p.cur.atDedent() }
func (p *parser) atIndent() bool  { return p.cur.atIndent() }
func (p *parser) next() Token     { return p.cur.next() }
func (p *parser) skipNewlines()   { p.cur.skipNewlines() }

// skipSemis consumes ';' statement separators. A `;` is the on-line spelling of
// the newline that would otherwise break the statements apart, so every loop
// that lists statements has to step over it - and an inline suite has to notice
// it, because `for i in xs: f(i); g(i)` puts both calls in the suite.
func (p *parser) skipSemis() {
	for p.peek().Kind == TokSemi {
		p.next()
	}
}

// skipSemiRun consumes one run of ';' separators and reports the empty statement
// CPython calls a syntax error: two separators with nothing between them
// (`x = 1;;y = 2`). It belongs to the parser, where every syntax rule lives, so
// that no engine can read the same source differently from another.
func (p *parser) skipSemiRun() error {
	if !p.atSemi() {
		return nil
	}
	first := p.peek()
	n := 0
	for p.atSemi() {
		p.next()
		n++
	}
	if n > 1 {
		return p.errorf(first, "empty statement: ';' separates two statements, and there is nothing between them")
	}
	return nil
}

// atSemi reports whether the parser sits on a ';' separator.
func (p *parser) atSemi() bool { return p.peek().Kind == TokSemi }

// skipSeparators steps over everything that can sit *between* statements: blank
// lines and ';' separators, in either order. `x = 1;` ends with a separator that
// is followed by the line's NEWLINE, so a loop that only skipped one kind would
// hand parseStmt a NEWLINE to parse as a statement.
func (p *parser) skipSeparators() error {
	saw := false
	for {
		p.skipNewlines()
		if !p.atSemi() {
			return nil
		}
		if saw {
			return p.errorf(p.peek(), "empty statement: ';' separates two statements, and there is nothing between them")
		}
		if err := p.skipSemiRun(); err != nil {
			return err
		}
		saw = true
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
	ok, diags := filterLex(toks)
	stmts, _, _, _, _, parseErrs := parseTopLevel(ok, src, 0)
	prog := &Program{Diags: diags, Stmts: stmts}
	if len(parseErrs) > 0 {
		return prog, &ParseErrors{Errors: parseErrs}
	}
	return prog, nil
}

// recoverStmt performs panic-mode error recovery: it skips tokens until the
// parser can resume at a top-level statement boundary. It tracks INDENT/DEDENT
// nesting (seeded from p.nest, which counts blocks whose INDENT was already
// consumed) so that a parse error deep inside a partially-parsed block skips
// out past the block's closing DEDENTs before resuming.
func (p *parser) recoverStmt() {
	for !p.atEOF() {
		t := p.peek()
		switch t.Kind {
		case TokIndent:
			p.nest++
			p.next()
		case TokDedent:
			if p.nest > 0 {
				p.nest--
			}
			p.next()
			if p.nest == 0 {
				// the partially-parsed block just closed: the next token
				// starts a top-level statement, so resume here.
				return
			}
		case TokNewline:
			if p.nest == 0 {
				p.skipNewlines()
				return
			}
			p.next()
		default:
			p.next()
		}
	}
}

// parseStmt parses a single statement and consumes its trailing NEWLINE(s).
func (p *parser) parseStmt() (Stmt, error) {
	t := p.peek()
	// async prefix: parse async def / async for / async with as first-class
	// syntax. Under the minimal synchronous-coroutine model (no suspension
	// primitives yet), async constructs behave identically to their
	// synchronous counterparts; the Async flag is informational until the
	// Phase-7 cooperative runtime lands.
	if t.IsKeyword("async") {
		p.next() // 'async'
		nt := p.peek()
		switch {
		case nt.IsKeyword("def"):
			fd, err := p.parseFuncDef()
			if err != nil {
				return nil, err
			}
			fd.(*FuncDef).Async = true
			return fd, nil
		case nt.IsKeyword("for"):
			fs, err := p.parseFor()
			if err != nil {
				return nil, err
			}
			fs.(*ForStmt).Async = true
			return fs, nil
		case nt.IsKeyword("with"):
			ws, err := p.parseWith()
			if err != nil {
				return nil, err
			}
			ws.(*WithStmt).Async = true
			return ws, nil
		default:
			return nil, p.errorf(nt, "expected 'def', 'for', or 'with' after 'async'")
		}
	}
	if t.IsOp("@") {
		return p.parseDecoratedDef()
	}
	if t.IsKeyword("def") {
		return p.parseFuncDef()
	}
	if t.IsKeyword("class") {
		return p.parseClassDef()
	}

	if t.IsKeyword("import") {
		return p.parseImport()
	}
	if t.IsKeyword("type") {
		return p.parseTypeAlias()
	}
	if t.IsKeyword("extern") {
		return p.parseExternDecl()
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
	if t.IsKeyword("with") {
		return p.parseWith()
	}
	if t.IsKeyword("raise") {
		// Keep the `raise` keyword's position: a traceback frame is only useful if the
		// statement carries the line it was written on. Without it both backends printed
		// `File "prog", line 0`, which answers "where" with nothing.
		sp := t.Span
		p.next()
		var ex Expr
		if !p.atNewline() && !p.atEOF() {
			ex, _ = p.parseExpr()
		}
		p.skipNewlines()
		return &RaiseStmt{Expr: ex, Src: sp}, nil
	}
	if t.IsKeyword("return") {
		return p.parseReturn()
	}
	if t.IsKeyword("yield") {
		return p.parseYield()
	}
	if t.IsKeyword("break") {
		p.next()
		return &BreakStmt{Src: t.Span}, nil
	}
	if t.IsKeyword("continue") {
		p.next()
		return &ContinueStmt{Src: t.Span}, nil
	}
	if t.IsKeyword("pass") {
		p.next()
		return &PassStmt{Src: t.Span}, nil
	}
	// expression / assignment
	return p.parseExprOrAssign()
}

// parseBlock parses an indented statement block after a colon.
func (p *parser) parseBlock(open Span) ([]Stmt, error) {
	// support single-line block: `if x: stmt` (no NEWLINE/INDENT)
	if !p.atNewline() {
		// A suite may be a list of simple statements separated by ';':
		// `for i in xs: f(i); g(i)` runs both per iteration, so both belong to
		// the suite. Stop at the NEWLINE that ends the physical line - the line
		// after it is the enclosing block's, not the suite's.
		var oneLine []Stmt
		for {
			st, err := p.parseStmt()
			if err != nil {
				return nil, err
			}
			oneLine = append(oneLine, st)
			if !p.atSemi() {
				break
			}
			if err := p.skipSemiRun(); err != nil {
				return nil, err
			}
			if p.atNewline() || p.atEOF() {
				break // `x = 1;` - a trailing separator is not an empty statement
			}
		}
		return oneLine, nil
	}
	// consume NEWLINE(s)
	p.skipNewlines()
	if !p.atIndent() {
		return nil, p.errorf(p.peek(), "expected indented block after ':'")
	}
	// consume INDENT
	p.next()
	p.nest++
	var stmts []Stmt
	for !p.atDedent() && !p.atEOF() {
		if err := p.skipSeparators(); err != nil {
			return nil, err
		}
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
		if p.nest > 0 {
			p.nest--
		}
	}
	// consume trailing NEWLINE after block close if any
	for p.atNewline() {
		p.next()
	}
	return stmts, nil
}

// extractDoc pulls a leading bare string-literal statement out of a def/class
// body and returns it as the docstring (Python-style). The literal is removed
// from the body so it is not re-evaluated as a no-op expression statement.
func (p *parser) extractDoc(body []Stmt) (string, []Stmt) {
	if len(body) == 0 {
		return "", body
	}
	es, ok := body[0].(*ExprStmt)
	if !ok {
		return "", body
	}
	lit, ok := es.Expr.(*StrLit)
	if !ok {
		return "", body
	}
	return lit.Value, body[1:]
}

// parseExternDecl parses `extern fn name(params) -> ret` — a declaration of a
// C function that gusty can call (FFI). It produces an ExternDecl statement.
func (p *parser) parseExternDecl() (Stmt, error) {
	t := p.peek()
	if t.Kind != TokKeyword || t.Text != "extern" {
		return nil, p.errorf(t, "expected 'extern'")
	}
	p.next() // 'extern'
	if p.peek().Text != "fn" {
		return nil, p.errorf(p.peek(), "expected 'fn' after 'extern'")
	}
	p.next() // 'fn'
	nameTok := p.peek()
	if nameTok.Kind != TokIdent {
		return nil, p.errorf(nameTok, "expected extern function name")
	}
	name := nameTok.Text
	p.next()
	decl := &ExternDecl{Name: name, Src: nameTok.Span}
	if !p.peek().IsOp("(") {
		return nil, p.errorf(p.peek(), "expected '(' after extern function name")
	}
	p.next()
	for {
		if p.atEOF() {
			return nil, p.errorf(p.peek(), "unexpected EOF in extern params")
		}
		if p.peek().IsOp(")") {
			p.next()
			break
		}
		param, err := p.parseParam()
		if err != nil {
			return nil, err
		}
		decl.Params = append(decl.Params, param)
		if p.peek().IsOp(",") {
			p.next()
			continue
		}
		if !p.peek().IsOp(")") {
			return nil, p.errorf(p.peek(), "expected ',' or ')' in extern params")
		}
	}
	if p.peek().IsOp("->") {
		p.next()
		ty, err := p.parseTypeAnnot()
		if err != nil {
			return nil, err
		}
		decl.ReturnAnno = ty
	}
	if p.atNewline() {
		p.next()
	}
	return decl, nil
}

func (p *parser) parseFuncDef() (Stmt, error) {
	def := p.next() // 'def'
	name, err := p.expectIdent()
	if err != nil {
		return nil, err
	}
	fd := &FuncDef{Name: name, Src: def.Span}
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
	fd.Doc, fd.Body = p.extractDoc(body)
	return fd, nil
}

// parseDecoratedDef parses one or more @decorator lines followed by a def.
func (p *parser) parseDecoratedDef() (Stmt, error) {
	var decs []Expr
	for {
		// current token is '@'
		p.next() // '@'
		ex, err := p.parseExpr()
		if err != nil {
			return nil, err
		}
		decs = append(decs, ex)
		// decorator line ends with a NEWLINE
		if !p.atNewline() {
			return nil, p.errorf(p.peek(), "expected newline after decorator")
		}
		p.next() // consume NEWLINE
		if p.peek().IsOp("@") {
			continue
		}
		break
	}
	if !p.peek().IsKeyword("def") {
		return nil, p.errorf(p.peek(), "expected def after decorators")
	}
	fd, err := p.parseFuncDef()
	if err != nil {
		return nil, err
	}
	f, ok := fd.(*FuncDef)
	if !ok {
		return nil, p.errorf(p.peek(), "expected function definition")
	}
	f.Decorators = decs
	return f, nil
}

func (p *parser) parseParam() (*Param, error) {
	t := p.peek()
	name, err := p.expectIdent()
	if err != nil {
		return nil, err
	}
	param := &Param{Name: name, Src: t.Span}
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
	cd := &ClassDef{Name: name, Src: cls.Span}
	// optional base list in parens
	if p.peek().IsOp("(") {
		p.next()
		for !p.peek().IsOp(")") {
			t := p.peek()
			if t.Kind != TokIdent {
				return nil, p.errorf(t, "expected base class name")
			}
			p.next()
			cd.Bases = append(cd.Bases, &Name{Value: t.Text, Src: t.Span})
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
	cd.Doc, cd.Body = p.extractDoc(body)
	return cd, nil
}

func (p *parser) parseImport() (Stmt, error) {
	im := p.next() // 'import'
	name, err := p.expectIdent()
	if err != nil {
		return nil, err
	}
	p.skipNewlines()
	return &ImportStmt{Module: name, Src: im.Span}, nil
}

// parseTypeAlias parses `type NAME = <type-annotation>` (L5.7). The alias is
// compile-time and structural: it binds NAME to a structural copy of the
// annotation type so later annotations can reference it. It has no runtime
// effect (the record/codegen treat it as a no-op statement).
func (p *parser) parseTypeAlias() (Stmt, error) {
	st := p.next() // 'type'
	nameTok := p.peek()
	if nameTok.Kind != TokIdent {
		return nil, p.errorf(nameTok, "expected type-alias name")
	}
	name := nameTok.Text
	p.next() // alias name
	op := p.peek()
	if !op.IsOp("=") {
		return nil, p.errorf(op, "expected '=' after type alias name")
	}
	p.next() // '='
	an, err := p.parseTypeAnnot()
	if err != nil {
		return nil, err
	}
	// Register the alias structurally: later annotations resolve it by
	// substituting a copy of the underlying annotation type.
	p.typeAliases[name] = an
	return &TypeAliasStmt{Name: name, Annot: an, Src: st.Span}, nil
}

func (p *parser) parseReturn() (Stmt, error) {
	rt := p.next() // 'return'
	rs := &ReturnStmt{Src: rt.Span}
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
	if p.peek().IsKeyword("from") {
		p.next()
		ex, err := p.parseExpr()
		if err != nil {
			return nil, err
		}
		return &YieldFromStmt{Expr: ex, Src: yt.Span}, nil
	}
	ys := &YieldStmt{Src: yt.Span}
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
	isf := &IfStmt{Cond: cond, Then: body, Src: kw.Span}
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
			isf.Elifs = append(isf.Elifs, &IfStmt{Cond: cond2, Then: body2, Src: t.Span})
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
	return &WhileStmt{Cond: cond, Body: body, Else: elseBody, Src: kw.Span}, nil
}

func (p *parser) parseFor() (Stmt, error) {
	kw := p.next() // 'for'
	t := p.peek()
	if t.Kind != TokIdent {
		return nil, p.errorf(t, "expected loop variable")
	}
	p.next()
	varName := &Name{Value: t.Text, Src: t.Span}
	varExpr := Expr(varName)
	// tuple loop variable: for a, b in ...
	if p.peek().IsOp(",") {
		elems := []Expr{varName}
		for p.peek().IsOp(",") {
			p.next()
			t2 := p.next()
			elems = append(elems, &Name{Value: t2.Text, Src: t2.Span})
		}
		varExpr = &Tuple{Elems: elems, Src: t.Span}
	}
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
	return &ForStmt{Var: varExpr, Iter: iter, Body: body, Else: elseBody, Src: kw.Span}, nil
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
	ms := &MatchStmt{Subject: subj, Src: kw.Span}
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
		pat, ors, guard, err := p.parseMatchPattern()
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
		ms.Cases = append(ms.Cases, &MatchCase{Pattern: pat, Or: ors, Guard: guard, Body: body, Src: t.Span})
	}
	if p.atDedent() {
		p.next()
	}
	return ms, nil
}

func (p *parser) parseMatchPattern() (Expr, []Expr, Expr, error) {
	pat, err := p.parsePatternAtom()
	if err != nil {
		return nil, nil, nil, err
	}
	var ors []Expr
	for p.peek().IsOp("|") {
		p.next()
		alt, err := p.parsePatternAtom()
		if err != nil {
			return nil, nil, nil, err
		}
		ors = append(ors, alt)
	}
	var guard Expr
	if p.peek().IsKeyword("if") {
		p.next()
		guard, err = p.parseExpr()
		if err != nil {
			return nil, nil, nil, err
		}
	}
	return pat, ors, guard, nil
}

func (p *parser) parsePatternAtom() (Expr, error) {
	t := p.peek()
	switch {
	case t.Kind == TokIdent:
		p.next()
		// class pattern: `case Point(x, y):` binds instance attributes x, y
		if p.peek().IsOp("(") {
			p.next() // consume '('
			fn := &Name{Value: t.Text, Src: t.Span}
			var args []Expr
			if !p.peek().IsOp(")") {
				for {
					attr := p.next()
					if attr.Kind != TokIdent {
						return nil, p.errorf(attr, "class pattern attribute must be a name")
					}
					args = append(args, &Name{Value: attr.Text, Src: attr.Span})
					if p.peek().IsOp(",") {
						p.next()
						if p.peek().IsOp(")") {
							break // trailing comma: `case Point(x, y,)`
						}
						continue
					}
					break
				}
			}
			if !p.peek().IsOp(")") {
				return nil, p.errorf(p.peek(), "expected ')' in class pattern")
			}
			p.next() // consume ')'
			return &Call{Fn: fn, Args: args, Src: t.Span}, nil
		}
		return &Name{Value: t.Text, Src: t.Span}, nil
	case t.Kind == TokInt:
		p.next()
		return &IntLit{Value: t.Int, Text: t.Text, Src: t.Span}, nil
	case t.Kind == TokString:
		p.next()
		return &StrLit{Value: t.Text, Src: t.Span}, nil
	case t.Kind == TokRawString:
		p.next()
		return &StrLit{Value: t.Str, Raw: true, Src: t.Span}, nil
	case t.Kind == TokTripleString:
		p.next()
		return &StrLit{Value: t.Str, Triple: true, Src: t.Span}, nil
	case t.Kind == TokRawTripleString:
		p.next()
		return &StrLit{Value: t.Str, Raw: true, Triple: true, Src: t.Span}, nil
	case t.IsOp("["):
		return p.parseListOrComp()
	case t.IsOp("{"):
		return p.parseDictOrSet()
	default:
		return p.parseExpr()
	}
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
	ts := &TryStmt{Body: body, Src: kw.Span}
	// except / finally clauses
	for {
		p.skipNewlines()
		t := p.peek()
		if t.IsKeyword("except") {
			p.next()
			ec := &ExceptClause{Src: t.Span}
			if p.peek().Kind == TokIdent {
				nt := p.next()
				ec.Exn = &Name{Value: nt.Text, Src: nt.Span}
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

// parseWith parses `with expr [as name]: body`.
func (p *parser) parseWith() (Stmt, error) {
	kw := p.next() // 'with'
	ex, err := p.parseExpr()
	if err != nil {
		return nil, err
	}
	ws := &WithStmt{Expr: ex, Src: kw.Span}
	if p.peek().IsKeyword("as") {
		p.next()
		nm, err := p.parseExpr()
		if err != nil {
			return nil, err
		}
		an, ok := nm.(*Name)
		if !ok {
			return nil, p.errorf(p.peek(), "expected identifier after 'as'")
		}
		ws.As = an
	}
	if err := p.expectOp(":"); err != nil {
		return nil, err
	}
	body, err := p.parseBlock(kw.Span)
	if err != nil {
		return nil, err
	}
	ws.Body = body
	return ws, nil
}

// parseExprOrAssign parses an expression statement or an assignment.

// isAugOp reports whether text is an augmented-assignment operator.
func isAugOp(text string) bool {
	switch text {
	case "+=", "-=", "*=", "/=", "//=", "%=":
		return true
	}
	return false
}

// augOpBase returns the arithmetic operator underlying an aug-op token.
func augOpBase(text string) string {
	switch text {
	case "+=":
		return "+"
	case "-=":
		return "-"
	case "*=":
		return "*"
	case "/=":
		return "/"
	case "//=":
		// `/=` and `//=` were collapsed to the same operator because `/` truncated;
		// now that `/` is true division (PEP 238) they are different operators and
		// `x //= 2` must keep floor semantics.
		return "//"
	case "%=":
		return "%"
	}
	return text
}

func (p *parser) parseExprOrAssign() (Stmt, error) {
	// detect simple name assignment: IDENT [= | : type =]
	if t := p.peek(); t.Kind == TokIdent {
		// lookahead: next significant token
		n := 1
		for p.cur.peek(n).Kind == TokNewline {
			n++
		}
		if tk := p.cur.peek(n); tk.IsOp("=") || tk.IsOp(":") {
			p.next() // ident
			name := &Name{Value: t.Text, Src: t.Span}
			as := &AssignStmt{Target: name, Src: t.Span}
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
	// tuple targets: a, b = ... or (a, b) = ...
	targets := []Expr{ex}
	for p.peek().IsOp(",") {
		p.next()
		ex2, err := p.parseExpr()
		if err != nil {
			return nil, err
		}
		targets = append(targets, ex2)
	}
	// tuple assignment: a, b = v1, v2
	if len(targets) > 1 && p.peek().IsOp("=") {
		p.next()
		rhs, err := p.parseExpr()
		if err != nil {
			return nil, err
		}
		rhsList := []Expr{rhs}
		for p.peek().IsOp(",") {
			p.next()
			ex2, err := p.parseExpr()
			if err != nil {
				return nil, err
			}
			rhsList = append(rhsList, ex2)
		}
		target := &Tuple{Elems: targets, Src: ex.Span()}
		value := Expr(rhs)
		if len(rhsList) > 1 {
			value = &Tuple{Elems: rhsList, Src: rhs.Span()}
		}
		p.skipNewlines()
		// The statement carries the position of its leftmost target: an unnamed
		// assignment has no line for a diagnostic, a traceback or a debugger to point
		// at (roadmap Gap K.6, and L8.5's line table reads the same spans).
		return &AssignStmt{Target: target, Value: value, Src: ex.Span()}, nil
	}
	// augmented assignment: target op= expr  (x += 1, self.x *= 2, ...)
	if op := p.peek(); op.Kind == TokOp && isAugOp(op.Text) {
		if _, ok := ex.(*Name); !ok {
			if _, ok := ex.(*Attr); !ok {
				return nil, fmt.Errorf("parser: augmented assignment requires a name or attribute target (got %T)", ex)
			}
		}
		p.next()
		val, err := p.parseExpr()
		if err != nil {
			return nil, err
		}
		aug := &AugAssignStmt{Target: ex, Op: augOpBase(op.Text), Value: val}
		aug.Src = ex.Span()
		return aug, nil
	}
	// assignment to an attribute or a subscript: `self.x = expr`, `d[k] = v`,
	// `xs[i] = v`. Anything else is a plain error: this path used to consume the
	// `= value` and fall through to an ExprStmt, so `d[1] = 2` parsed successfully
	// and silently did nothing.
	if p.peek().IsOp("=") {
		p.next()
		val, err := p.parseExpr()
		if err != nil {
			return nil, err
		}
		switch t := ex.(type) {
		case *Attr:
			return &AssignStmt{Target: t, Value: val, Src: t.Span()}, nil
		case *Index:
			return &AssignStmt{Target: t, Value: val, Src: t.Span()}, nil
		case *Tuple:
			return &AssignStmt{Target: t, Value: val, Src: t.Span()}, nil
		default:
			// A *ParseError (not a plain error) so the front end reports it: a
			// non-ParseError used to be swallowed by the statement-recovery loop,
			// which is how `f() = 1` came to parse as "no statement at all".
			return nil, &ParseError{Span: ex.Span(), Msg: "cannot assign to this target; assignment needs a name, attribute, tuple or subscript target"}
		}
	}
	p.skipNewlines()
	return &ExprStmt{Expr: ex, Src: ex.Span()}, nil
}

// parseTypeAnnot parses a type annotation token (int/float/bool/str/any).
func isTypeName(s string) bool {
	switch s {
	case "int", "float", "bool", "str", "any":
		return true
	}
	return false
}

func (p *parser) parseTypeAnnot() (*Type, error) {
	// Union-type syntax: `int | str` parses as a union of member types.
	first, err := p.parseTypeTerm()
	if err != nil {
		return nil, err
	}
	if !p.peek().IsOp("|") {
		return first, nil
	}
	members := []*Type{first}
	for p.peek().IsOp("|") {
		p.next()
		alt, err := p.parseTypeTerm()
		if err != nil {
			return nil, err
		}
		members = append(members, alt)
	}
	return TUnion(members...), nil
}

func (p *parser) parseTypeTerm() (*Type, error) {
	t := p.peek()
	p.next()
	name := t.Text

	// Generic/protocol annotation with args: name [ args ].
	if p.peek().IsOp("[") {
		p.next()
		// Callable[[A, B], R] — the first arg is itself a bracketed param list.
		// Literal[1, 2, ...] — a literal type: parse integer constant tokens.
		if name == "Literal" || name == "literal" {
			var vals []int64
			for {
				if p.peek().Kind != TokInt {
					return nil, p.errorf(p.peek(), "Literal[...] requires integer constants")
				}
				vals = append(vals, p.peek().Int)
				p.next()
				if p.peek().IsOp(",") {
					p.next()
					continue
				}
				break
			}
			if !p.peek().IsOp("]") {
				return nil, p.errorf(p.peek(), "expected ']' in Literal[...] annotation")
			}
			p.next()
			if len(vals) == 1 {
				return TLit(vals[0]), nil
			}
			members := []*Type{}
			for _, v := range vals {
				members = append(members, TLit(v))
			}
			return TUnion(members...), nil
		}
		// Callable[[A, B], R] — the first arg is itself a bracketed param list.
		if name == "Callable" || name == "callable" {
			pl, err := p.parseTypeList()
			if err != nil {
				return nil, err
			}
			if !p.peek().IsOp(",") {
				return nil, p.errorf(p.peek(), "expected ',' after Callable params")
			}
			p.next()
			retTy, err := p.parseTypeAnnot()
			if err != nil {
				return nil, err
			}
			if !p.peek().IsOp("]") {
				return nil, p.errorf(p.peek(), "expected ']' in Callable annotation")
			}
			p.next()
			return TCallable(pl, retTy), nil
		}
		// Generic element args: list[int], dict[str, int], Sequence[int], ...
		var args []*Type
		for {
			argTy, err := p.parseTypeAnnot()
			if err != nil {
				return nil, err
			}
			args = append(args, argTy)
			if p.peek().IsOp(",") {
				p.next()
				continue
			}
			break
		}
		if !p.peek().IsOp("]") {
			return nil, p.errorf(p.peek(), "expected ']' in type annotation")
		}
		p.next()
		return p.buildType(name, args, t)
	}

	// Bare name (no generic args).
	return p.buildType(name, nil, t)
}

// parseTypeList parses `[ type (, type)* ]` and returns the contained types.
// Used for Callable's parameter list, e.g. Callable[[int, str], bool].
func (p *parser) parseTypeList() ([]*Type, error) {
	if !p.peek().IsOp("[") {
		return nil, p.errorf(p.peek(), "expected '[' in type annotation")
	}
	p.next()
	var tys []*Type
	for {
		ty, err := p.parseTypeAnnot()
		if err != nil {
			return nil, err
		}
		tys = append(tys, ty)
		if p.peek().IsOp(",") {
			p.next()
			continue
		}
		break
	}
	if !p.peek().IsOp("]") {
		return nil, p.errorf(p.peek(), "expected ']' in type annotation")
	}
	p.next()
	return tys, nil
}

// buildType maps a parsed type name (with optional generic args) to a *Type.
func (p *parser) buildType(name string, args []*Type, t Token) (*Type, error) {
	switch name {
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
	case "None":
		return TNone(), nil
	case "void":
		return TVoid(), nil
	case "list":
		if len(args) != 1 {
			return nil, fmt.Errorf("list requires 1 type argument")
		}
		return TList(args[0]), nil
	case "dict":
		if len(args) != 2 {
			return nil, fmt.Errorf("dict requires 2 type arguments")
		}
		return TDict(args[0], args[1]), nil
	case "set":
		if len(args) != 1 {
			return nil, fmt.Errorf("set requires 1 type argument")
		}
		return TSet(args[0]), nil
	case "tuple":
		return TTuple(args...), nil
	case "Sequence", "sequence":
		if len(args) != 1 {
			return nil, fmt.Errorf("Sequence requires 1 type argument")
		}
		return TSequence(args[0]), nil
	case "Iterator", "Iterator[T]", "Iterable", "iterable":
		if len(args) != 1 {
			return nil, fmt.Errorf("Iterator requires 1 type argument")
		}
		return TIter(args[0]), nil
	default:
		// Structural type alias (L5.7): `type Alias = T` expands to T.
		// Aliases are resolved structurally (not nominally), so a reference
		// to an alias name substitutes the underlying annotation type.
		if aliased, ok := p.typeAliases[name]; ok {
			if len(args) != 0 {
				return nil, fmt.Errorf("type alias %q does not take type arguments", name)
			}
			return cloneType(aliased), nil
		}
		// A user-declared class: nominal class type (L6.6). An instance may flow
		// to such a position only if it is that class or a subclass of it.
		if p.classNames[name] {
			if len(args) != 0 {
				return nil, fmt.Errorf("class %q does not take type arguments", name)
			}
			return TClass(name), nil
		}
		return nil, fmt.Errorf("unknown type annotation %q", name)
	}
}

// cloneType returns a deep structural copy of a Type so that alias expansion
// never aliases the registered alias's underlying annotation (which could
// mutate if the alias were re-registered).
func cloneType(t *Type) *Type {
	if t == nil {
		return nil
	}
	c := *t
	if c.Elem != nil {
		c.Elem = cloneType(c.Elem)
	}
	if c.Key != nil {
		c.Key = cloneType(c.Key)
	}
	if c.Val != nil {
		c.Val = cloneType(c.Val)
	}
	if len(c.Params) > 0 {
		c.Params = make([]*Type, len(c.Params))
		for i, p := range t.Params {
			c.Params[i] = cloneType(p)
		}
	}
	if len(c.Members) > 0 {
		c.Members = make([]*Type, len(c.Members))
		for i, m := range t.Members {
			c.Members[i] = cloneType(m)
		}
	}
	if len(c.Elems) > 0 {
		c.Elems = make([]*Type, len(c.Elems))
		for i, e := range t.Elems {
			c.Elems[i] = cloneType(e)
		}
	}
	return &c
}

// --- expression parsing (precedence climbing) ---

// prec is a precedence level for the Pratt / precedence-climbing expression
// parser. Higher binds tighter; the ordering mirrors Python's precedence.
type prec int

const (
	precWalrus  prec = iota + 1
	precTernary      // a if b else c
	precOr           // or
	precAnd          // and
	precNot          // prefix not
	precCompare      // == != < <= > >= in not in is is not
	precAdd          // + -
	precMul          // * / // %
	precUnary        // prefix -
	precPower        // ** (right-associative)
	precPostfix      // call ( ) index [ ] attr .
)

// parseExpr parses a full expression using the precedence-climbing algorithm.
func (p *parser) parseExpr() (Expr, error) {
	return p.parseExprPrec(0)
}

// parseExprPrec parses an expression, consuming infix operators whose
// precedence is >= minPrec. This is the core Pratt loop.
func (p *parser) parseExprPrec(minPrec prec) (Expr, error) {
	lhs, err := p.parsePrefix()
	if err != nil {
		return nil, err
	}
	for {
		t := p.peek()

		// Ternary `a if b else c`: the then-part is lhs, the condition is
		// parsed at or-level, and the else-branch is a full (right-assoc) expr.
		if t.IsKeyword("if") {
			if precTernary < minPrec {
				break
			}
			p.next()
			cond, err := p.parseExprPrec(precTernary + 1)
			if err != nil {
				return nil, err
			}
			if err := p.expectKeyword("else"); err != nil {
				return nil, err
			}
			r, err := p.parseExprPrec(precTernary)
			if err != nil {
				return nil, err
			}
			lhs = &CondExpr{If: lhs, Cond: cond, Else: r, Src: t.Span}
			continue
		}

		// Postfix operators bind tighter than every infix operator.
		if t.IsOp("(") || t.IsOp("[") || t.IsOp(".") {
			if precPostfix < minPrec {
				break
			}
			lhs, err = p.parsePostfixOp(lhs)
			if err != nil {
				return nil, err
			}
			continue
		}

		// Binary operators.
		op, opPrec, rightAssoc, ok := p.binaryOp(t)
		if !ok || opPrec < minPrec {
			break
		}
		// Comparison CHAINS: `a < b < c` is Python's own construct, not two comparisons nested, and it
		// is recognised here rather than left to the loop's left-associativity because the loop's shape
		// is the bug — `(a < b) < c` asks a question about a boolean that the reference never asks
		// (roadmap L12.1 / Gap R.53, ADR 0288). Only the FIRST comparison of a run is seen here: by the
		// time the second operator is next, `lhs` is the chain node this branch builds.
		p.next() // consume the operator (and, below, any second token of a two-word one)
		// Two-token comparison operators. This runs BEFORE the chain is built, because `not in` and
		// `is not` are two tokens and the chain helper is handed the finished word: skipping it left
		// `1 not in [1]` and `1 is not 2` unparseable, which is a worse bug than the one being fixed
		// (roadmap L12.1 / Gap R.53, ADR 0288).
		switch op {
		case "not in":
			// binaryOp returned "not in" only when the next token is `in`.
			p.next()
		case "is":
			if p.peek().IsKeyword("not") {
				p.next()
				op = "is not"
			}
		}
		// Comparison CHAINS: `a < b < c` is Python's own construct, not two comparisons nested, and it
		// is recognised here rather than left to the loop's left-associativity because the loop's shape
		// is the bug — `(a < b) < c` asks a question about a boolean that the reference never asks
		// (roadmap L12.1 / Gap R.53, ADR 0288). Only the FIRST comparison of a run reaches here: after
		// this branch, `lhs` is the chain node and the next operator belongs to it, not to the loop.
		if opPrec == precCompare && isCompareOp(op) {
			chain, err := p.parseChainCompare(op, lhs, t.Span)
			if err != nil {
				return nil, err
			}
			lhs = chain
			continue
		}
		rhsPrec := opPrec
		if !rightAssoc {
			rhsPrec = opPrec + 1
		}
		r, err := p.parseExprPrec(rhsPrec)
		if err != nil {
			return nil, err
		}
		if op == ":=" {
			nm, ok := lhs.(*Name)
			if !ok {
				return nil, fmt.Errorf("walrus operator `:=` requires a name on the left")
			}
			lhs = &AssignExpr{Name: nm, Value: r, Src: t.Span}
		} else {
			lhs = &BinOp{Op: op, L: lhs, R: r, Src: t.Span}
		}
	}
	return lhs, nil
}

// isCompareOp reports whether an operator can appear in a Python comparison chain. The set is exactly
// the operators the comparison precedence level holds; `:=` and the boolean words are not in it.
func isCompareOp(op string) bool {
	switch op {
	case "==", "!=", "<", "<=", ">", ">=", "in", "not in", "is", "is not":
		return true
	}
	return false
}

// parseChainCompare consumes the rest of a run of comparison operators and returns the chain node.
// `first` is the operator already consumed, `firstLeft` its left operand, and `start` the chain's
// beginning span. Each operand is parsed at precCompare+1 so that a nested non-comparison operator
// (`a < b + c`) still binds inside an operand, and so a `not`/`and`/`or` beyond the run stops it:
// `a < b and b < c` is TWO chains joined by `and`, which is what the reference means too.
func (p *parser) parseChainCompare(first string, firstLeft Expr, start Span) (Expr, error) {
	ops := []string{first}
	// The operator that started the chain is already consumed by the caller, so its right operand is
	// the next thing to read — and the chain continues from there.
	r, err := p.parseExprPrec(precCompare + 1)
	if err != nil {
		return nil, err
	}
	operands := []Expr{firstLeft, r}
	for {
		t := p.peek()
		nextOp, opPrec, _, ok := p.binaryOp(t)
		if !ok || opPrec != precCompare || !isCompareOp(nextOp) {
			break
		}
		p.next()
		nr, err := p.parseExprPrec(precCompare + 1)
		if err != nil {
			return nil, err
		}
		ops = append(ops, nextOp)
		operands = append(operands, nr)
	}
	if len(ops) == 1 {
		// Not a chain after all: one operator is exactly a BinOp, and building a ChainCompare for it
		// would give every ordinary comparison in the language a second codegen path to get wrong.
		return &BinOp{Op: ops[0], L: operands[0], R: operands[1], Src: start}, nil
	}
	return &ChainCompare{Ops: ops, Operands: operands, Src: start}, nil
}

// parsePrefix parses a prefix (unary `not` / `-`) expression or an atom.
func (p *parser) parsePrefix() (Expr, error) {
	t := p.peek()
	// await expr: first-class syntax. Under the minimal synchronous-coroutine
	// model a coroutine completes immediately, so `await e` reduces to `e`;
	// the real awaitable lowering arrives with the Phase-7 cooperative
	// runtime. This keeps await valid in both the record and AOT paths.
	if t.IsKeyword("await") {
		p.next() // 'await'
		x, err := p.parseExprPrec(precUnary)
		if err != nil {
			return nil, err
		}
		return &AwaitExpr{Expr: x, Src: x.Span()}, nil
	}
	if t.IsKeyword("not") {
		p.next()
		x, err := p.parseExprPrec(precNot)
		if err != nil {
			return nil, err
		}
		return &UnOp{Op: "not", X: x, Src: t.Span}, nil
	}
	if t.IsOp("-") {
		p.next()
		x, err := p.parseExprPrec(precUnary)
		if err != nil {
			return nil, err
		}
		return &UnOp{Op: "-", X: x, Src: t.Span}, nil
	}
	return p.parseAtom()
}

// binaryOp reports the infix operator at token t: its text, precedence,
// right-associativity, and whether t is a valid binary operator.
func (p *parser) binaryOp(t Token) (op string, opPrec prec, rightAssoc, ok bool) {
	if t.Kind == TokKeyword {
		switch t.Text {
		case "or":
			return "or", precOr, false, true
		case "and":
			return "and", precAnd, false, true
		case "in":
			return "in", precCompare, false, true
		case "is":
			return "is", precCompare, false, true
		case "not":
			if p.peekNext().IsKeyword("in") {
				return "not in", precCompare, false, true
			}
		}
	} else if t.Kind == TokOp {
		switch t.Text {
		case ":=":
			return ":=", precWalrus, false, true
		case "==":
			return "==", precCompare, false, true
		case "!=":
			return "!=", precCompare, false, true
		case "<":
			return "<", precCompare, false, true
		case "<=":
			return "<=", precCompare, false, true
		case ">":
			return ">", precCompare, false, true
		case ">=":
			return ">=", precCompare, false, true
		case "+":
			return "+", precAdd, false, true
		case "-":
			return "-", precAdd, false, true
		case "*":
			return "*", precMul, false, true
		case "/":
			return "/", precMul, false, true
		case "//":
			return "//", precMul, false, true
		case "%":
			return "%", precMul, false, true
		case "**":
			return "**", precPower, true, true
		}
	}
	return "", 0, false, false
}

// parseArg parses a single call argument, which may be a `name = value`
// keyword argument or a plain positional expression.
func (p *parser) parseArg() (Expr, error) {
	x, err := p.parseExpr()
	if err != nil {
		return nil, err
	}
	if t := p.peek(); t.IsOp("=") {
		p.next()
		v, err := p.parseExpr()
		if err != nil {
			return nil, err
		}
		if n, ok := x.(*Name); ok {
			return &KeywordArg{Name: n.Value, Value: v, Src: t.Span}, nil
		}
		return nil, p.errorf(t, "keyword argument must be a name")
	}
	return x, nil
}

// parsePostfixOp applies a single postfix operation (call, index, or
// attribute access) to lhs and returns the result.
func (p *parser) parsePostfixOp(lhs Expr) (Expr, error) {
	t := p.peek()
	if t.IsOp("(") {
		op := p.next()
		var args []Expr
		if !p.peek().IsOp(")") {
			for {
				a, err := p.parseArg()
				if err != nil {
					return nil, err
				}
				args = append(args, a)
				if p.peek().IsOp(",") {
					p.next()
					if p.peek().IsOp(")") {
						break // trailing comma: `f(a, b,)`
					}
					continue
				}
				break
			}
		}
		if err := p.expectOp(")"); err != nil {
			return nil, err
		}
		return &Call{Fn: lhs, Args: args, Src: op.Span}, nil
	}
	if t.IsOp("[") {
		op := p.next()
		var low, high, step Expr
		var isSlice bool
		if !p.peek().IsOp(":") && !p.peek().IsOp("]") {
			var err error
			low, err = p.parseExpr()
			if err != nil {
				return nil, err
			}
		}
		if p.peek().IsOp(":") {
			isSlice = true
			p.next()
			if !p.peek().IsOp(":") && !p.peek().IsOp("]") {
				var err error
				high, err = p.parseExpr()
				if err != nil {
					return nil, err
				}
			}
			if p.peek().IsOp(":") {
				p.next()
				if !p.peek().IsOp("]") {
					var err error
					step, err = p.parseExpr()
					if err != nil {
						return nil, err
					}
				}
			}
		}
		if err := p.expectOp("]"); err != nil {
			return nil, err
		}
		if isSlice {
			return &Slice{Obj: lhs, Low: low, High: high, Step: step, Src: op.Span}, nil
		}
		return &Index{Obj: lhs, Idx: low, Src: op.Span}, nil
	}
	if t.IsOp(".") {
		op := p.next()
		nt := p.peek()
		if nt.Kind != TokIdent {
			return nil, p.errorf(nt, "expected attribute name")
		}
		p.next()
		return &Attr{Obj: lhs, Name: &Name{Value: nt.Text, Src: nt.Span}, Src: op.Span}, nil
	}
	return nil, p.errorf(t, "expected postfix operator")
}

func (p *parser) parseAtom() (Expr, error) {
	t := p.peek()
	switch {
	case t.Kind == TokInt:
		p.next()
		return &IntLit{Value: t.Int, Text: t.Text, Src: t.Span}, nil
	case t.Kind == TokFloat:
		p.next()
		return &FloatLit{Value: t.Float, Text: t.Text, Src: t.Span}, nil
	case t.Kind == TokString:
		p.next()
		return &StrLit{Value: t.Str, Src: t.Span}, nil
	case t.Kind == TokRawString:
		p.next()
		return &StrLit{Value: t.Str, Raw: true, Src: t.Span}, nil
	case t.Kind == TokTripleString:
		p.next()
		return &StrLit{Value: t.Str, Triple: true, Src: t.Span}, nil
	case t.Kind == TokRawTripleString:
		p.next()
		return &StrLit{Value: t.Str, Raw: true, Triple: true, Src: t.Span}, nil
	case t.Kind == TokFString:
		p.next()
		return p.buildFString(t.FStrRaw, t.Span)
	case t.Kind == TokKeyword && t.Text == "True":
		p.next()
		return &BoolLit{Value: true, Src: t.Span}, nil
	case t.Kind == TokKeyword && t.Text == "False":
		p.next()
		return &BoolLit{Value: false, Src: t.Span}, nil
	case t.Kind == TokKeyword && t.Text == "None":
		p.next()
		return &NoneLit{Src: t.Span}, nil
	case t.Kind == TokKeyword && (t.Text == "print" || t.Text == "range"):
		p.next()
		return &Name{Value: t.Text, Src: t.Span}, nil
	case t.Kind == TokKeyword && t.Text == "lambda":
		return p.parseLambda()
	case t.Kind == TokIdent:
		p.next()
		return &Name{Value: t.Text, Src: t.Span}, nil
	case t.IsOp("("):
		p.next()
		ex, err := p.parseExpr()
		if err != nil {
			return nil, err
		}
		elems := []Expr{ex}
		trailing := false
		for p.peek().IsOp(",") {
			p.next()
			if p.peek().IsOp(")") {
				trailing = true // trailing comma: `(a, b,)` / 1-tuple `(a,)`
				break
			}
			ex2, err := p.parseExpr()
			if err != nil {
				return nil, err
			}
			elems = append(elems, ex2)
		} // generator expression: `(elem for var in iter [if cond])`
		if p.peek().IsKeyword("for") {
			g, err := p.parseGeneratorTail(ex)
			if err != nil {
				return nil, err
			}
			if err := p.expectOp(")"); err != nil {
				return nil, err
			}
			return g, nil
		}
		if err := p.expectOp(")"); err != nil {
			return nil, err
		}
		if len(elems) > 1 || trailing {
			return &Tuple{Elems: elems, Src: ex.Span()}, nil
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
	lm := &Lambda{Src: t.Span}
	if p.peek().Kind == TokIdent {
		for {
			// Lambda params are plain names: parseParam() would greedily treat the
			// ':' body separator as a type annotation (e.g. lambda x: x + 1).
			tok := p.next()
			if tok.Kind != TokIdent {
				return nil, p.errorf(tok, "lambda param must be a name")
			}
			param := &Param{Name: tok.Text}
			// Optional `: type` annotation, but only when the token after ':' is a
			// known type keyword; otherwise ':' is the lambda body separator.
			if p.peek().IsOp(":") && isTypeName(p.cur.peek(1).Text) {
				p.next() // consume ':'
				ty, err := p.parseTypeAnnot()
				if err != nil {
					return nil, err
				}
				param.Annot = ty
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

// parseGeneratorTail parses `for var in iter [if cond]` for a generator
// expression `(elem for var in iter [if cond])`, returning a Generator.
func (p *parser) parseGeneratorTail(elem Expr) (*Generator, error) {
	if err := p.expectKeyword("for"); err != nil {
		return nil, err
	}
	vt := p.next()
	if vt.Kind != TokIdent {
		return nil, fmt.Errorf("expected generator variable name")
	}
	if err := p.expectKeyword("in"); err != nil {
		return nil, err
	}
	iter, err := p.parseExprPrec(precOr)
	if err != nil {
		return nil, err
	}
	var cond Expr
	if p.peek().IsKeyword("if") {
		p.next()
		cond, err = p.parseExprPrec(precOr)
		if err != nil {
			return nil, err
		}
	}
	return &Generator{Elems: []Expr{elem}, ForVar: &Name{Value: vt.Text}, Iter: iter, Cond: cond}, nil
}

func (p *parser) parseListOrComp() (Expr, error) {
	t := p.next() // '['
	// Every expression parsed below is an element of this display: a `{…}` among them must leave a
	// following `for` to this display, which is what turns it into a list comprehension (Gap R.74).
	p.inListLit++
	defer func() { p.inListLit-- }()
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
		// The iterable is an or-level expression (not a ternary): a ternary
		// here would greedily consume the comprehension\x27s own `if` filter.
		iter, err := p.parseExprPrec(precOr)
		if err != nil {
			return nil, err
		}
		c := &Comp{Kind: CompList, Elems: elems, ForVar: &Name{Value: v.Text, Src: v.Span}, Iter: iter, Src: t.Span}
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
	return &ListLit{Elems: elems, Src: t.Span}, nil
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
				if p.peek().IsOp("}") {
					break // trailing comma: `{1: 2,}`
				}
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
				if p.peek().IsOp("}") {
					break // trailing comma: `{1, 2,}`
				}
				e, err := p.parseExpr()
				if err != nil {
					return nil, err
				}
				elems = append(elems, e)
			}
		}
	}
	// Support `{... for x in iter}`: a comprehension 'for' inside the braces.
	if p.peek().IsKeyword("for") {
		p.next() // 'for'
		v := p.next()
		p.next() // 'in'
		// The iterable stops at `or`, exactly as the list comprehension above does: a
		// full expression here is a ternary, and the ternary eats the comprehension's own
		// `if` filter — `{x for x in xs if x > 1}` parsed as `{x for x in (xs if x > 1 …)}`
		// and demanded an `else` (roadmap Gap J.2, ADR 0234).
		iter, err := p.parseExprPrec(precOr)
		if err != nil {
			return nil, err
		}
		vn := &Name{Value: v.Text, Src: v.Span}
		var comp *Comp
		if isDict {
			comp = &Comp{Kind: CompDict, Keys: keys, Vals: vals, ForVar: vn, Iter: iter}
		} else {
			comp = &Comp{Kind: CompSet, Elems: elems, ForVar: vn, Iter: iter}
		}
		if p.peek().IsKeyword("if") {
			p.next()
			cond, err := p.parseExpr()
			if err != nil {
				return nil, err
			}
			comp.Cond = cond
		}
		if err := p.expectOp("}"); err != nil {
			return nil, err
		}
		return comp, nil
	}
	if err := p.expectOp("}"); err != nil {
		return nil, err
	}
	// A `for` after the closing brace finishes a display that is the *whole* expression — the
	// call-argument genexp `len({x*x} for x in xs)`. Inside a list display it is the enclosing
	// comprehension's, so the display stops here and `parseListOrComp` builds the comprehension over
	// this literal (roadmap Gap R.74, ADR 0244: `[{1, 2} for x in xs]` is a list of sets, not a set).
	if p.inListLit == 0 && p.peek().IsKeyword("for") {
		p.next() // 'for'
		v := p.peek()
		if v.Kind != TokIdent {
			return nil, p.errorf(v, "expected comprehension variable")
		}
		p.next()
		vn := &Name{Value: v.Text, Src: v.Span}
		if err := p.expectKeyword("in"); err != nil {
			return nil, err
		}
		// As above: the iterable is an or-level expression so the `if` filter stays the
		// comprehension's, not a ternary's (Gap J.2, ADR 0234).
		iter, err := p.parseExprPrec(precOr)
		if err != nil {
			return nil, err
		}
		var cond Expr
		if p.peek().IsKeyword("if") {
			p.next()
			cond, err = p.parseExpr()
			if err != nil {
				return nil, err
			}
		}
		if isDict {
			return &Comp{Kind: CompDict, Keys: keys, Vals: vals, ForVar: vn, Iter: iter, Cond: cond, Src: t.Span}, nil
		}
		return &Comp{Kind: CompSet, Elems: elems, ForVar: vn, Iter: iter, Cond: cond, Src: t.Span}, nil
	}
	if isDict {
		return &DictLit{Keys: keys, Vals: vals, Src: t.Span}, nil
	}
	// `{}` is an EMPTY DICT, like everywhere else in Python: braces mean mapping,
	// and the empty set is spelled set(). Classifying it as a set made `d = {}` then
	// `d[k] = v` fail with "not in set" on the record while AOT treated the same
	// literal as a dict — the same token, two kinds.
	if len(elems) == 0 && len(keys) == 0 {
		return &DictLit{Src: t.Span}, nil
	}
	return &SetLit{Elems: elems, Src: t.Span}, nil
}

// buildFString parses the raw inner content of an f-string token into an
// FString AST node. The raw content keeps escape sequences intact; literal
// segments are unescaped (backslash dropped, matching the lexer) and each
// `{expr}` segment is re-lexed and re-parsed as a full expression. A format
// spec (`:` suffix) is stripped before parsing the expression.
func (p *parser) buildFString(raw string, sp Span) (*FString, error) {
	fs := &FString{Src: sp}
	lit := ""
	flush := func() {
		if lit != "" {
			fs.Parts = append(fs.Parts, FStringPart{Lit: unescapeStr(lit)})
			lit = ""
		}
	}
	i := 0
	n := len(raw)
	for i < n {
		c := raw[i]
		switch c {
		case '{':
			if i+1 < n && raw[i+1] == '{' {
				lit += "{"
				i += 2
				continue
			}
			flush()
			depth := 1
			j := i + 1
			for j < n {
				if raw[j] == '{' {
					depth++
				} else if raw[j] == '}' {
					depth--
					if depth == 0 {
						break
					}
				}
				j++
			}
			if j >= n {
				return nil, &ParseError{Span: sp, Msg: "unterminated f-string expression"}
			}
			// One bracket- and quote-aware scan yields the expression, the `!r`/`!s` conversion and
			// the format spec. The spec used to be cut away by stripFormatSpec and never stored, so
			// every road that rendered an interpolation answered the plain value (Gap R.186, ADR 0299).
			exprSrc, conv, spec, serr := splitSpecConv(raw[i+1 : j])
			if serr != nil {
				return nil, &ParseError{Span: sp, Msg: serr.Error()}
			}
			toks, err := Lex(exprSrc)
			if err != nil {
				return nil, err
			}
			sub := newParser(exprSrc, toks)
			ex, err := sub.parseExpr()
			if err != nil {
				return nil, err
			}
			fs.Parts = append(fs.Parts, FStringPart{Expr: ex, Spec: spec, Conv: conv})
			i = j + 1
		case '}':
			if i+1 < n && raw[i+1] == '}' {
				lit += "}"
				i += 2
				continue
			}
			lit += "}"
			i++
		default:
			// raw[i:i+1], not string(c): c is a byte, and Go converts a byte to a
			// *rune*, so every non-ASCII byte in an f-string literal was
			// re-encoded as a two-byte sequence ("✓" became "âœ“").
			lit += raw[i : i+1]
			i++
		}
	}
	flush()
	if len(fs.Parts) == 0 {
		return nil, &ParseError{Span: sp, Msg: "empty f-string"}
	}
	return fs, nil
}

// stripFormatSpec returns the expression source before any top-level `:` so
// a Python-style format spec like `{x:>5}` parses as just `x`.
func stripFormatSpec(src string) string {
	depth := 0
	for i := 0; i < len(src); i++ {
		switch src[i] {
		case '{', '[':
			depth++
		case '}', ']':
			if depth > 0 {
				depth--
			}
		case ':':
			if depth == 0 {
				return src[:i]
			}
		}
	}
	return src
}

// unescapeStr decodes escape sequences in string-ish source text (f-string
// literals reach it with their backslashes intact). It shares the lexer's
// decoder so the two can never disagree: an earlier version dropped the
// backslash and kept the character, which turned "a\nb" into "anb".
func unescapeStr(s string) string {
	var b strings.Builder
	for i := 0; i < len(s); {
		if s[i] == '\\' && i+1 < len(s) {
			i = appendEscape(&b, s, i)
			continue
		}
		b.WriteByte(s[i])
		i++
	}
	return b.String()
}
