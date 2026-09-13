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
	TokOp
	TokKeyword
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

// keywords in the language.
var keywords = map[string]bool{
	"def": true, "return": true, "if": true, "elif": true, "else": true,
	"while": true, "for": true, "in": true, "range": true, "print": true,
	"class": true, "import": true, "match": true, "case": true, "try": true,
	"except": true, "finally": true, "yield": true, "lambda": true,
	"None": true, "True": true, "False": true, "not": true, "and": true, "or": true,
}
