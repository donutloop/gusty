package lang

// Span is a source location. Line and Col are 1-based rune offsets.
type Span struct {
	Line int `json:"line"`
	Col  int `json:"col"`
}

func (s Span) IsZero() bool { return s.Line == 0 && s.Col == 0 }

// TokenKind enumerates lexical token kinds.
type TokenKind int

const (
	TokEOF TokenKind = iota
	TokNewline
	TokIndent
	TokDedent
	TokIdent
	TokInt
	TokFloat
	TokString
	TokRawString
	TokTripleString
	TokRawTripleString
	TokFString
	TokOp
	TokKeyword
	TokError
)

// Token is a single lexical token with its source span.
type Token struct {
	Kind TokenKind
	Text string
	Span Span

	// Literal payloads.
	Int   int64
	Float float64
	Str   string
	FStrRaw string // raw inner content of an f-string token
	ErrMsg string // message carried by a TokError token
}

func (t Token) Is(kind TokenKind, text string) bool {
	return t.Kind == kind && t.Text == text
}

func (t Token) IsOp(text string) bool {
	return t.Kind == TokOp && t.Text == text
}

func (t Token) IsKeyword(text string) bool {
	return t.Kind == TokKeyword && t.Text == text
}

func (t Token) IsIdent(text string) bool {
	return t.Kind == TokIdent && t.Text == text
}

// Cursor is a bounds-safe position over a token stream. It is the shared
// abstraction for walking tokens — the parser, formatter, and LSP all walk
// the same stream through this type so spans are a single source of truth.
// peek(n) provides lookahead without advancing; mark()/reset() enable
// backtracking for speculative parses.
type Cursor struct {
	toks []Token
	pos  int
}

// NewCursor returns a cursor positioned at the start of toks.
func NewCursor(toks []Token) *Cursor { return &Cursor{toks: toks} }

// peek returns the token n positions ahead of the current one (0 = current)
// without advancing. Past the end of the stream it returns TokEOF, so callers
// never need to bounds-check.
func (c *Cursor) peek(n int) Token {
	i := c.pos + n
	if i < 0 || i >= len(c.toks) {
		return Token{Kind: TokEOF}
	}
	return c.toks[i]
}

// next returns the current token and advances by one. At the final token it
// stays put and returns TokEOF, mirroring parser semantics.
func (c *Cursor) next() Token {
	t := c.peek(0)
	if c.pos < len(c.toks)-1 {
		c.pos++
	}
	return t
}

// mark returns the current position, for a later reset.
func (c *Cursor) mark() int { return c.pos }

// reset seeks back to a previously-marked position.
func (c *Cursor) reset(pos int) { c.pos = pos }

// position returns the current position.
func (c *Cursor) position() int { return c.pos }

func (c *Cursor) atEOF() bool     { return c.peek(0).Kind == TokEOF }
func (c *Cursor) atNewline() bool { return c.peek(0).Kind == TokNewline }
func (c *Cursor) atDedent() bool  { return c.peek(0).Kind == TokDedent }
func (c *Cursor) atIndent() bool  { return c.peek(0).Kind == TokIndent }

// skipNewlines consumes consecutive NEWLINE tokens.
func (c *Cursor) skipNewlines() {
	for c.atNewline() {
		c.next()
	}
}

// keywords in the language.
var keywords = map[string]bool{
	"def": true, "return": true, "if": true, "elif": true, "else": true,
	"while": true, "for": true, "in": true, "range": true, "print": true,
	"class": true, "import": true,
	"extern": true, "match": true, "case": true, "try": true,
	"except": true, "finally": true, "yield": true, "lambda": true,
	"None": true, "True": true, "False": true, "not": true, "and": true, "or": true,
	"is": true,
	"with": true, "from": true, "as": true,
	"break": true, "continue": true, "raise": true, "pass": true,
}
