package lang

import (
	"fmt"
	"sort"
	"strings"
	"unicode/utf8"
)

// Edit describes a single source edit in a document. Start and End are
// 1-based spans (Line, rune-based Col) in the document's ORIGINAL
// coordinates; the region [Start, End) is replaced by NewText. A zero-length
// region inserts NewText; an empty NewText deletes the region.
type Edit struct {
	Start   Span
	End     Span
	NewText string
}

// ParseCache is a stable, span-keyed parse tree for a source document.
// Incremental updates re-lex the document and re-parse only the top-level
// statements affected by an edit; top-level statements whose source region
// is untouched keep their AST node identity (keyed by their source span)
// across updates. The LSP and REPL use it to avoid a full re-parse on every
// keystroke.
type ParseCache struct {
	src   string
	toks  []Token
	diags []Diagnostic
	prog  *Program
	// per-statement boundaries recorded during the most recent parse:
	// stmtStartTok[i] is the token index at which top-level statement i
	// starts, stmtEndTok[i] the token index just after its body, and the
	// byte slices are the corresponding byte offsets in c.src.
	stmtStartTok  []int
	stmtEndTok    []int
	stmtStartByte []int
	stmtEndByte   []int
	reused        int // top-level statements preserved by the most recent Update
	parseErrs     []*ParseError
}

// NewParseCache parses src fully and returns a span-keyed parse cache.
func NewParseCache(src string) (*ParseCache, error) {
	raw, err := Lex(src)
	if err != nil {
		return nil, err
	}
	ok, diags := filterLex(raw)
	c := &ParseCache{src: src, toks: ok, diags: diags}
	c.reparse()
	return c, nil
}

// Source returns the current document text.
func (c *ParseCache) Source() string { return c.src }

// Program returns the current parse tree.
func (c *ParseCache) Program() *Program { return c.prog }

// Reused returns how many top-level statements the most recent Update kept
// by span identity (0 for a full parse).
func (c *ParseCache) Reused() int { return c.reused }

// ParseErrors returns the parse errors collected during the most recent
// parse/update.
func (c *ParseCache) ParseErrors() []*ParseError { return c.parseErrs }

// reparse fully re-parses c.toks (which must lex c.src) and records each
// top-level statement's token/byte boundaries.
func (c *ParseCache) reparse() {
	stmts, starts, ends, startBytes, endBytes, errs := parseTopLevel(c.toks, c.src, 0)
	c.prog = &Program{Diags: c.diags, Stmts: stmts}
	c.stmtStartTok = starts
	c.stmtEndTok = ends
	c.stmtStartByte = startBytes
	c.stmtEndByte = endBytes
	c.parseErrs = errs
	c.reused = 0
}

// SetText replaces the whole document with newText and reparses it.
func (c *ParseCache) SetText(newText string) error {
	raw, err := Lex(newText)
	if err != nil {
		return err
	}
	ok, diags := filterLex(raw)
	c.src = newText
	c.toks = ok
	c.diags = diags
	c.reparse()
	return nil
}

// Update applies the given edits to the document and re-parses only the
// top-level statements affected by them. Unaffected statements keep their
// AST node identity across the update.
//
// A keystroke near the top of a file used to re-parse everything below it:
// reuse was prefix-only, so the statements after the edit lost their node
// identity and the LSP re-did the work for the whole document. When the edit
// is a single in-line substitution, the text after it is byte-identical (only
// shifted), so the statements that live entirely in that tail are reused too
// and only the statements overlapping the edit are re-parsed.
func (c *ParseCache) Update(edits []Edit) error {
	newSrc, editStartByte, editEndByte, err := applyEdits(c.src, edits)
	if err != nil {
		return err
	}
	raw, err := Lex(newSrc)
	if err != nil {
		return err
	}
	ok, diags := filterLex(raw)

	affected, decremented := affectedIndex(c.stmtStartByte, c.stmtEndByte, editStartByte)
	firstTok := c.firstAffectedTok(ok, affected, decremented)

	// Tail reuse: the index of the first statement that starts after the edit,
	// plus the token index where its text begins in the NEW stream. Only sound
	// for a single edit that adds no lines, because a reused node keeps its
	// spans and inserting a line would silently stale them.
	tailIdx, tailTok, canReuseTail := c.tailReuse(edits, editEndByte, newSrc, ok, affected)

	var (
		middle        []Stmt
		mStarts       []int
		mEnds         []int
		mStartBytes   []int
		mEndBytes     []int
		middleErrs    []*ParseError
		tail          []Stmt
		tailStartTok  []int
		tailEndTok    []int
		tailStartByte []int
		tailEndByte   []int
	)
	if canReuseTail {
		middle, mStarts, mEnds, mStartBytes, mEndBytes, middleErrs = parseTopLevelRange(ok, newSrc, firstTok, tailTok)
		tail = append([]Stmt(nil), c.prog.Stmts[tailIdx:]...)
		delta := len(newSrc) - len(c.src)
		tailStartTok = append([]int(nil), c.stmtStartTok[tailIdx:]...)
		tailEndTok = append([]int(nil), c.stmtEndTok[tailIdx:]...)
		for i := range tailStartTok {
			tailStartTok[i] += tailTok - c.stmtStartTok[tailIdx]
			tailEndTok[i] += tailTok - c.stmtStartTok[tailIdx]
		}
		tailStartByte = make([]int, len(tail))
		tailEndByte = make([]int, len(tail))
		for i := range tail {
			tailStartByte[i] = c.stmtStartByte[tailIdx+i] + delta
			tailEndByte[i] = c.stmtEndByte[tailIdx+i] + delta
		}
	} else {
		middle, mStarts, mEnds, mStartBytes, mEndBytes, middleErrs = parseTopLevel(ok, newSrc, firstTok)
		tailIdx = len(c.prog.Stmts) // no tail reused
	}

	stmts := make([]Stmt, 0, affected+len(middle)+len(tail))
	stmts = append(stmts, c.prog.Stmts[:affected]...)
	stmts = append(stmts, middle...)
	stmts = append(stmts, tail...)

	// rebuilt boundaries: preserved prefix + re-parsed middle + preserved tail
	presStart := append([]int(nil), c.stmtStartTok[:affected]...)
	presEnd := append([]int(nil), c.stmtEndTok[:affected]...)
	presSByte := append([]int(nil), c.stmtStartByte[:affected]...)
	presEByte := append([]int(nil), c.stmtEndByte[:affected]...)

	c.src = newSrc
	c.toks = ok
	c.diags = diags
	c.prog = &Program{Diags: diags, Stmts: stmts}
	c.stmtStartTok = append(append(presStart, mStarts...), tailStartTok...)
	c.stmtEndTok = append(append(presEnd, mEnds...), tailEndTok...)
	c.stmtStartByte = append(append(presSByte, mStartBytes...), tailStartByte...)
	c.stmtEndByte = append(append(presEByte, mEndBytes...), tailEndByte...)
	c.parseErrs = middleErrs
	c.reused = affected + len(tail)
	return nil
}

// tailReuse decides whether the statements after an edit can be reused as they
// are. It returns the index of the first such statement and the token index in
// the NEW stream where its text begins.
//
// Soundness needs three things, and the ordinary keystroke gives them: exactly
// one edit, no newline added or removed (so every reused node's line/column
// spans stay true), and the bytes after the edit unchanged apart from a
// constant shift (verified against the text itself rather than assumed).
func (c *ParseCache) tailReuse(edits []Edit, editEndByte int, newSrc string, newToks []Token, prefix int) (int, int, bool) {
	if len(edits) != 1 || c.prog == nil {
		return 0, 0, false
	}
	e := edits[0]
	if e.Start.Line != e.End.Line || strings.ContainsRune(e.NewText, '\n') {
		return 0, 0, false
	}
	tailIdx := len(c.prog.Stmts)
	for i, s := range c.stmtStartByte {
		if s >= editEndByte {
			tailIdx = i
			break
		}
	}
	// The tail must start after the last statement the prefix reuse already keeps.
	if tailIdx >= len(c.prog.Stmts) || tailIdx < prefix {
		return 0, 0, false
	}
	delta := len(newSrc) - len(c.src)
	oldFrom := c.stmtStartByte[tailIdx]
	if oldFrom < editEndByte || editEndByte+delta > len(newSrc) {
		return 0, 0, false
	}
	// The tail must be identical text, not merely the same length.
	if c.src[oldFrom:] != newSrc[oldFrom+delta:] {
		return 0, 0, false
	}
	tok, ok := tokenIndexAtByte(newToks, oldFrom+delta)
	if !ok {
		return 0, 0, false
	}
	return tailIdx, tok, true
}

// tokenIndexAtByte finds the index of the first token whose byte offset in the
// source is exactly target (a statement boundary must land on a token start).
func tokenIndexAtByte(toks []Token, target int) (int, bool) {
	for i, tk := range toks {
		if tk.Kind == TokEOF {
			break
		}
		if tk.Start == target {
			return i, true
		}
		if tk.Start > target {
			return 0, false
		}
	}
	return 0, false
}

// parseTopLevelRange is parseTopLevel restricted to the token range [startTok,
// endTok), so an edit in the middle of a document parses only the statements
// that overlap it instead of everything below.
func parseTopLevelRange(toks []Token, src string, startTok, endTok int) (stmts []Stmt, starts, ends, startBytes, endBytes []int, errs []*ParseError) {
	if endTok > len(toks) {
		endTok = len(toks)
	}
	// Truncate to the range: the parser stops at the slice end, and the tokens
	// after it belong to statements being reused rather than re-parsed.
	trimmed := append([]Token(nil), toks[startTok:endTok]...)
	trimmed = append(trimmed, Token{Kind: TokEOF, Start: len(src), End: len(src)})
	stmts, starts, ends, startBytes, endBytes, errs = parseTopLevel(trimmed, src, 0)
	// Token indices come back relative to the slice; translate them to the real
	// stream so the boundaries stay comparable with the reused prefix and tail.
	for i := range starts {
		starts[i] += startTok
	}
	for i := range ends {
		ends[i] += startTok
	}
	return stmts, starts, ends, startBytes, endBytes, errs
}

// affectedIndex returns the index of the first top-level statement affected
// by an edit beginning at byte editStartByte (in the OLD document), and
// whether the edit falls inside the previous statement's body (so that
// statement must be re-parsed too). Statements [0, affected) are untouched.
func affectedIndex(starts, ends []int, editStartByte int) (affected int, decremented bool) {
	affected = len(starts)
	for i, s := range starts {
		if s >= editStartByte {
			affected = i
			break
		}
	}
	if affected > 0 && editStartByte < ends[affected-1] {
		affected--
		decremented = true
	}
	return affected, decremented
}

// firstAffectedTok returns the token index (in the NEW token stream) at
// which the first affected statement starts.
func (c *ParseCache) firstAffectedTok(toks []Token, affected int, decremented bool) int {
	if affected == len(c.stmtEndTok) { // edit after all statements (or empty doc)
		if len(c.stmtEndTok) == 0 {
			return 0
		}
		return skipNewlinesAt(toks, c.stmtEndTok[len(c.stmtEndTok)-1])
	}
	if decremented {
		// the affected statement's leading tokens are unchanged
		return c.stmtStartTok[affected]
	}
	if affected == 0 {
		return 0
	}
	// statements [0, affected) are unchanged: their token indexes are valid
	return skipNewlinesAt(toks, c.stmtEndTok[affected-1])
}

// parseTopLevel runs parseProgram's top-level statement loop starting at
// token index startTok, recording each successfully parsed statement's
// token/byte boundaries. It mirrors parseProgram's error recovery.
func parseTopLevel(toks []Token, src string, startTok int) (stmts []Stmt, starts, ends, startBytes, endBytes []int, errs []*ParseError) {
	p := newParser(src, toks)
	p.cur.pos = startTok
	for !p.atEOF() {
		if err := p.skipSeparators(); err != nil {
			if pe, ok := err.(*ParseError); ok {
				errs = append(errs, pe)
			} else {
				errs = append(errs, &ParseError{Span: p.peek().Span, Msg: err.Error()})
			}
			p.recoverStmt()
			continue
		}
		if p.atEOF() {
			break
		}
		starts = append(starts, p.cur.pos)
		startBytes = append(startBytes, p.cur.peek(0).Start)
		st, err := p.parseStmt()
		if err != nil {
			if pe, ok := err.(*ParseError); ok {
				errs = append(errs, pe)
			} else {
				// Any other parser failure must still be reported. Before this, a
				// plain error fell through here, the statement was recovered past and
				// dropped, and the program "parsed" with that code missing — e.g.
				// `f() = 1` or `1 = 2` produced zero statements and zero diagnostics.
				errs = append(errs, &ParseError{Span: p.peek().Span, Msg: err.Error()})
			}
			p.recoverStmt()
			continue
		}
		stmts = append(stmts, st)
		ends = append(ends, p.cur.pos)
		// end byte = byte offset where the statement's body ends (start of
		// its trailing NEWLINEs, or end of document at EOF).
		endBytes = append(endBytes, curByte(&p.cur, src))
	}
	return
}

// curByte returns the byte offset of the token at the cursor, clamping to
// len(src) at EOF (the lexer's EOF sentinel carries Start=0).
func curByte(cur *Cursor, src string) int {
	tk := cur.peek(0)
	if tk.Kind == TokEOF {
		return len(src)
	}
	return tk.Start
}

// filterLex drops error/warning tokens into diagnostics and returns the
// remaining token stream (mirroring parseProgram).
func filterLex(raw []Token) (ok []Token, diags []Diagnostic) {
	for _, tk := range raw {
		switch tk.Kind {
		case TokError:
			// The code is what lets an agent branch on "the source did not parse"
			// instead of matching message prose.
			diags = append(diags, Diagnostic{Level: LevelError, Span: tk.Span, Msg: tk.ErrMsg, Code: CodeParseError})
		case TokWarning:
			diags = append(diags, Diagnostic{Level: LevelWarning, Span: tk.Span, Msg: tk.ErrMsg})
		case TokSemi:
			// `x = 1; print(x)` - the ';' is the on-line spelling of the statement
			// break, and an inline suite (`for i in xs: f(i); g(i)`) needs to see it:
			// the statements after a ';' belong to the suite, not to the block
			// enclosing it. So it stays in the stream as a separator token and the
			// statement loops consume it. It is not a diagnostic: the same program
			// must not answer on the interpreter and be refused by the JIT/AOT
			// (roadmap Gap R.72).
			ok = append(ok, tk)
		default:
			ok = append(ok, tk)
		}
	}
	return ok, diags
}

// skipNewlinesAt returns the index of the first non-NEWLINE token at or
// after pos.
func skipNewlinesAt(toks []Token, pos int) int {
	// Both statement separators count: a blank line and a `;` are the same kind
	// of nothing-between-statements.
	for pos < len(toks) && (toks[pos].Kind == TokNewline || toks[pos].Kind == TokSemi) {
		pos++
	}
	return pos
}

// applyEdits applies edits (given in the ORIGINAL document's coordinates)
// to src and returns the new document plus the byte range of the edit
// region in the original document.
func applyEdits(src string, edits []Edit) (newSrc string, editStartByte, editEndByte int, err error) {
	type byteEdit struct {
		start, end int
		newText    string
	}
	bes := make([]byteEdit, len(edits))
	editStartByte = 1 << 30
	editEndByte = 0
	for i, e := range edits {
		start, err := spanToByte(src, e.Start)
		if err != nil {
			return "", 0, 0, err
		}
		end, err := spanToByte(src, e.End)
		if err != nil {
			return "", 0, 0, err
		}
		if start > end {
			return "", 0, 0, fmt.Errorf("edit range out of order")
		}
		bes[i] = byteEdit{start, end, e.NewText}
		if start < editStartByte {
			editStartByte = start
		}
		if end > editEndByte {
			editEndByte = end
		}
	}
	sort.Slice(bes, func(i, j int) bool { return bes[i].start < bes[j].start })
	var b strings.Builder
	pos := 0
	for _, be := range bes {
		if be.start < pos {
			return "", 0, 0, fmt.Errorf("overlapping edits")
		}
		b.WriteString(src[pos:be.start])
		b.WriteString(be.newText)
		pos = be.end
	}
	b.WriteString(src[pos:])
	return b.String(), editStartByte, editEndByte, nil
}

// spanToByte converts a 1-based (Line, rune-based Col) span to a byte offset
// in src, clamping a column past the end of its line to the line end.
func spanToByte(src string, s Span) (int, error) {
	if s.Line <= 0 || s.Col <= 0 {
		return 0, fmt.Errorf("invalid span")
	}
	pos := 0
	for line := 1; line < s.Line; line++ {
		i := strings.IndexByte(src[pos:], '\n')
		if i < 0 {
			return 0, fmt.Errorf("line %d beyond document", s.Line)
		}
		pos += i + 1
	}
	col := 1
	for col < s.Col && pos < len(src) && src[pos] != '\n' {
		_, size := utf8.DecodeRuneInString(src[pos:])
		if size == 0 {
			break
		}
		pos += size
		col++
	}
	return pos, nil
}
