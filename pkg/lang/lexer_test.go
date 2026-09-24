package lang

import (
	"strings"
	"testing"
)

// TestLexNumericLiterals verifies modern numeric-literal syntax (hex,
// binary, octal, and digit separators) lexes to correct token values and
// preserves the original spelling in token.Text.
func TestLexNumericLiterals(t *testing.T) {
	cases := []struct {
		src  string
		want int64
	}{
		{"0xFF", 255},
		{"0Xff", 255},
		{"0b101", 5},
		{"0B101", 5},
		{"0o17", 15},
		{"0O17", 15},
		{"1_000", 1000},
		{"0x_FF", 255},
		{"0b_1010", 10},
		{"0o_777", 511},
		{"2_5", 25},
		{"-0xFF", -255},
		{"-0b101", -5},
		{"-1_000", -1000},
	}
	for _, c := range cases {
		toks, err := Lex(c.src)
		if err != nil {
			t.Fatalf("Lex(%q): %v", c.src, err)
		}
		if len(toks) == 0 {
			t.Fatalf("Lex(%q): no tokens", c.src)
		}
		// find the numeric token (skip Newline/Dedent kinds if present)
		var found *Token
		for i := range toks {
			if toks[i].Kind == TokInt || toks[i].Kind == TokFloat {
				found = &toks[i]
				break
			}
		}
		if found == nil {
			t.Fatalf("Lex(%q): no numeric token", c.src)
		}
		if found.Kind != TokInt {
			t.Fatalf("Lex(%q): got %v want TokInt", c.src, found.Kind)
		}
		if found.Int != c.want {
			t.Errorf("Lex(%q).Int = %d, want %d", c.src, found.Int, c.want)
		}
		if found.Text != c.src {
			t.Errorf("Lex(%q).Text = %q, want %q", c.src, found.Text, c.src)
		}
	}
}

// TestLexFloatSeparators verifies underscores are accepted in float literals.
func TestLexFloatSeparators(t *testing.T) {
	toks, err := Lex("1_0.5")
	if err != nil {
		t.Fatalf("Lex: %v", err)
	}
	var found *Token
	for i := range toks {
		if toks[i].Kind == TokFloat {
			found = &toks[i]
			break
		}
	}
	if found == nil {
		t.Fatalf("no float token")
	}
	if got := found.Float; got != 10.5 {
		t.Errorf("Float = %v, want 10.5", got)
	}
}

// TestLexUnderscoreMisuse verifies misplaced '_' separators are rejected.
func TestLexUnderscoreMisuse(t *testing.T) {
	// L4.1 error-recovering lexer: numeric misuse no longer aborts; it emits a
	// TokError token and resumes, so the parser can surface multiple diagnostics.
	for _, src := range []string{"1__0", "1_", "0x_", "0b1_", "1_a"} {
		toks, err := Lex(src)
		if err != nil {
			t.Fatalf("Lex(%q): unexpected hard error %v", src, err)
		}
		if !hasTokError(toks) {
			t.Errorf("Lex(%q): expected a TokError token, got %d tokens", src, len(toks))
		}
	}
}

func hasTokError(toks []Token) bool {
	for _, tk := range toks {
		if tk.Kind == TokError {
			return true
		}
	}
	return false
}

func TestLexRecoveryMultipleErrors(t *testing.T) {
	// L4.1: the lexer recovers and emits one TokError token per bad site, so the
	// parser can surface multiple diagnostics in a single pass.
	src := "1__0; \"unterminated"
	toks, err := Lex(src)
	if err != nil {
		t.Fatalf("Lex: should recover, got %v", err)
	}
	var n int
	for _, tk := range toks {
		if tk.Kind == TokError {
			n++
		}
	}
	if n < 2 {
		t.Errorf("Lex(%q): expected >=2 TokError tokens, got %d", src, n)
	}
}

// kindName maps a TokenKind to its short name for test assertions.
func kindName(k TokenKind) string {
	names := [...]string{"EOF", "Newline", "Indent", "Dedent", "Ident", "Int",
		"Float", "String", "RawString", "TripleString", "RawTripleString",
		"FString", "Op", "Keyword"}
	if int(k) < len(names) {
		return names[k]
	}
	return "?"
}

// lexKinds returns a compact string of token kinds for a source, for assertions.
func lexKinds(src string) string {
	toks, err := Lex(src)
	if err != nil {
		return "ERR:" + err.Error()
	}
	var b strings.Builder
	for _, t := range toks {
		b.WriteString(kindName(t.Kind))
		if t.Kind == TokNewline || t.Kind == TokEOF {
			b.WriteByte('\n')
		} else {
			b.WriteByte(' ')
		}
	}
	return b.String()
}

// TestLexLineContinuation verifies L4.6: a trailing backslash before the
// newline joins the next physical line into one logical line (no NEWLINE token
// in the middle), ignoring the continuation line's leading indentation.
func TestLexLineContinuation(t *testing.T) {
	// a long call argument list split with a continuation must lex as one line
	src := "print(1 + \\\n    2)\nprint(3)\n"
	got := lexKinds(src)
	want := "Keyword Op Int Op Int Op Newline\nKeyword Op Int Op Newline\nEOF\n"
	if got != want {
		t.Fatalf("lex line continuation:\n got %q\nwant %q", got, want)
	}

	// multiple continuations chain within one logical line
	got = lexKinds("x = 1 + \\\n    2 + \\\n    3\nprint(x)\n")
	want = "Ident Op Int Op Int Op Int Newline\nKeyword Op Ident Op Newline\nEOF\n"
	if got != want {
		t.Fatalf("lex chained continuation:\n got %q\nwant %q", got, want)
	}

	// blank and comment-only continuation lines are skipped
	got = lexKinds("x = 1 + \\\n\n    # note\n    2\nprint(x)\n")
	want = "Ident Op Int Op Int Newline\nKeyword Op Ident Op Newline\nEOF\n"
	if got != want {
		t.Fatalf("lex continuation with blank/comment lines:\n got %q\nwant %q", got, want)
	}

	// a lone backslash not before a newline is rejected
	toks2, err2 := Lex("x = \\ 1\n")
		if err2 != nil {
			t.Fatalf("Lex: misplaced backslash should recover, got %v", err2)
		}
		if !hasTokError(toks2) {
			t.Fatalf("Lex: misplaced backslash should emit a TokError token")
		}
}

// TestParseLineContinuation verifies the parser consumes a continued logical
// line as a single statement.
func TestParseLineContinuation(t *testing.T) {
	prog, err := Parse("total = 1 + \\\n    2 + \\\n    3\nprint(total)\n")
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if len(prog.Stmts) != 2 {
		t.Fatalf("want 2 statements, got %d", len(prog.Stmts))
	}
	as, ok := prog.Stmts[0].(*AssignStmt)
	if !ok {
		t.Fatalf("stmt[0] is %T, want *AssignStmt", prog.Stmts[0])
	}
	be, ok := as.Value.(*BinOp)
	if !ok || be.Op != "+" {
		t.Fatalf("continued expression not a full binary expr: %#v", as.Value)
	}
	// the inner + must also be a binary expr (1 + 2 + 3 fully folded left)
	inner, ok := be.L.(*BinOp)
	if !ok || inner.Op != "+" {
		t.Fatalf("expected left-assoc nested binary expr, got %#v", be.L)
	}
}

// TestRichTokenSpans verifies L4.2: every token carries precise start/end byte
// offsets, rune offsets, and a multi-line flag.
func TestRichTokenSpans(t *testing.T) {
	src := "foo bar\n\"hi\"\n"
	toks, err := Lex(src)
	if err != nil {
		t.Fatalf("Lex(%q): %v", src, err)
	}
	var foo, hi *Token
	for i := range toks {
		if toks[i].Text == "foo" {
			foo = &toks[i]
		}
		if toks[i].Text == "\"hi\"" {
			hi = &toks[i]
		}
	}
	if foo == nil {
		t.Fatalf("no 'foo' token")
	}
	if hi == nil {
		t.Fatalf("no string token")
	}
	if foo.Start != 0 || foo.End != 3 {
		t.Errorf("foo byte span = %d..%d, want 0..3", foo.Start, foo.End)
	}
	if foo.StartRune != 0 || foo.EndRune != 3 {
		t.Errorf("foo rune span = %d..%d, want 0..3", foo.StartRune, foo.EndRune)
	}
	if foo.Multiline {
		t.Errorf("foo must not be multiline")
	}
	if hi.Start != 8 || hi.End != 12 {
		t.Errorf("string byte span = %d..%d, want 8..12", hi.Start, hi.End)
	}
	if hi.Multiline {
		t.Errorf("single-line string must not be multiline")
	}

	// A triple-quoted string spanning multiple lines must be flagged multiline.
	src2 := "x \"\"\"\nline2\nline3\"\"\""
	toks2, err := Lex(src2)
	if err != nil {
		t.Fatalf("Lex(%q): %v", src2, err)
	}
	var triple *Token
	for i := range toks2 {
		if toks2[i].Kind == TokTripleString {
			triple = &toks2[i]
			break
		}
	}
	if triple == nil {
		t.Fatalf("no triple-string token")
	}
	if !triple.Multiline {
		t.Errorf("triple string must be multiline")
	}
	if triple.Start >= triple.End {
		t.Errorf("triple span invalid: %d..%d", triple.Start, triple.End)
	}
	if triple.StartRune >= triple.EndRune {
		t.Errorf("triple rune span invalid: %d..%d", triple.StartRune, triple.EndRune)
	}
}
