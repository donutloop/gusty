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
func (c *ParseCache) Update(edits []Edit) error {
	newSrc, editStartByte, _, err := applyEdits(c.src, edits)
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
	suffix, sStarts, sEnds, sStartBytes, sEndBytes, suffixErrs := parseTopLevel(ok, newSrc, firstTok)

	stmts := make([]Stmt, 0, len(c.prog.Stmts)-affected+len(suffix))
	stmts = append(stmts, c.prog.Stmts[:affected]...)
	stmts = append(stmts, suffix...)

	// rebuilt boundaries: preserved prefix + re-parsed suffix
	presStart := append([]int(nil), c.stmtStartTok[:affected]...)
	presEnd := append([]int(nil), c.stmtEndTok[:affected]...)
	presSByte := append([]int(nil), c.stmtStartByte[:affected]...)
	presEByte := append([]int(nil), c.stmtEndByte[:affected]...)

	c.src = newSrc
	c.toks = ok
	c.diags = diags
	c.prog = &Program{Diags: diags, Stmts: stmts}
	c.stmtStartTok = append(presStart, sStarts...)
	c.stmtEndTok = append(presEnd, sEnds...)
	c.stmtStartByte = append(presSByte, sStartBytes...)
	c.stmtEndByte = append(presEByte, sEndBytes...)
	c.parseErrs = suffixErrs
	c.reused = affected
	return nil
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
		p.skipNewlines()
		if p.atEOF() {
			break
		}
		starts = append(starts, p.cur.pos)
		startBytes = append(startBytes, p.cur.peek(0).Start)
		st, err := p.parseStmt()
		if err != nil {
			if pe, ok := err.(*ParseError); ok {
				errs = append(errs, pe)
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
			diags = append(diags, Diagnostic{Level: LevelError, Span: tk.Span, Msg: tk.ErrMsg})
		case TokWarning:
			diags = append(diags, Diagnostic{Level: LevelWarning, Span: tk.Span, Msg: tk.ErrMsg})
		default:
			ok = append(ok, tk)
		}
	}
	return ok, diags
}

// skipNewlinesAt returns the index of the first non-NEWLINE token at or
// after pos.
func skipNewlinesAt(toks []Token, pos int) int {
	for pos < len(toks) && toks[pos].Kind == TokNewline {
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
