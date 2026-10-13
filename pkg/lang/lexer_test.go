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
	// `print` is a built-in name, not a keyword (ADR 0203), so it lexes as an identifier.
	want := "Ident Op Int Op Int Op Newline\nIdent Op Int Op Newline\nEOF\n"
	if got != want {
		t.Fatalf("lex line continuation:\n got %q\nwant %q", got, want)
	}

	// multiple continuations chain within one logical line
	got = lexKinds("x = 1 + \\\n    2 + \\\n    3\nprint(x)\n")
	want = "Ident Op Int Op Int Op Int Newline\nIdent Op Ident Op Newline\nEOF\n"
	if got != want {
		t.Fatalf("lex chained continuation:\n got %q\nwant %q", got, want)
	}

	// blank and comment-only continuation lines are skipped
	got = lexKinds("x = 1 + \\\n\n    # note\n    2\nprint(x)\n")
	want = "Ident Op Int Op Int Newline\nIdent Op Ident Op Newline\nEOF\n"
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

// TestLexUnicodeIdentifiers verifies L4.3: identifiers may start with
// non-ASCII characters from the Unicode XID_Start class and continue with
// XID_Continue characters (letters, marks, digits, connector punctuation).
func TestLexUnicodeIdentifiers(t *testing.T) {
	cases := []struct{ src, want string }{
		{"café = 1", "café"}, // Latin-1 Supplement (U+00E9)
		{"你好 = 2", "你好"},     // CJK ideographs (Lo)
		{"αβγ = 3", "αβγ"},   // Greek letters
		{"变量 = 4", "变量"},     // CJK ideographs
		{"num١ = 5", "num١"}, // ASCII start + Arabic-Indic digit continue
		{"naïve", "naïve"},   // U+00EF i with diaeresis
	}
	for _, c := range cases {
		toks, err := Lex(c.src)
		if err != nil {
			t.Fatalf("Lex(%q): %v", c.src, err)
		}
		var id string
		for _, tk := range toks {
			if tk.Kind == TokIdent {
				id = tk.Text
				break
			}
		}
		if id != c.want {
			t.Errorf("Lex(%q): identifier = %q, want %q (tokens: %+v)", c.src, id, c.want, toks)
		}
	}
}

// TestLexNFCNormalization verifies L4.3: identifier text is NFC-normalized,
// so a decomposed spelling (e + U+0301 COMBINING ACUTE) and the precomposed
// U+00E9 are the same canonical identifier.
func TestLexNFCNormalization(t *testing.T) {
	toks, err := Lex("e\u0301clair = 1")
	if err != nil {
		t.Fatalf("Lex: %v", err)
	}
	var id string
	for _, tk := range toks {
		if tk.Kind == TokIdent {
			id = tk.Text
			break
		}
	}
	if id != "éclair" {
		t.Errorf("NFC normalization failed: got %q (%U), want precomposed éclair", id, []rune(id))
	}
}

// TestLexUnicodeContinuation verifies an ASCII start can continue through
// non-ASCII runes (XID_Continue), which the old byte scanner rejected.
func TestLexUnicodeContinuation(t *testing.T) {
	toks, err := Lex("café")
	if err != nil {
		t.Fatalf("Lex: %v", err)
	}
	var id string
	for _, tk := range toks {
		if tk.Kind == TokIdent {
			id = tk.Text
			break
		}
	}
	if id != "café" {
		t.Errorf("continuation: got %q, want café", id)
	}
}

// TestLexConfusableWarning verifies L4.3: an identifier containing a rune
// visually confusable with an ASCII character (U+039F GREEK CAPITAL OMICRON
// vs Latin 'O') emits a TokWarning so the parser surfaces a LevelWarning.
func TestLexConfusableWarning(t *testing.T) {
	// "cafΟ" uses GREEK CAPITAL OMICRON U+039F, confusable with Latin 'O'.
	toks, err := Lex("cafΟ = 1")
	if err != nil {
		t.Fatalf("Lex: %v", err)
	}
	found := false
	for _, tk := range toks {
		if tk.Kind == TokWarning {
			found = true
			if tk.ErrMsg == "" {
				t.Errorf("warning token must carry a message")
			}
			break
		}
	}
	if !found {
		t.Errorf("expected a TokWarning for confusable identifier, tokens: %+v", toks)
	}
}

// TestLexNoConfusableASCII verifies plain ASCII identifiers produce no warning.
func TestLexNoConfusableASCII(t *testing.T) {
	toks, err := Lex("order = 1")
	if err != nil {
		t.Fatalf("Lex: %v", err)
	}
	for _, tk := range toks {
		if tk.Kind == TokWarning {
			t.Errorf("unexpected warning for ASCII identifier: %+v", tk)
		}
	}
}

// tokenKindName is the print name a token stream assertion reads. The kinds have no
// String() — the stream is only ever printed by a failing test, and a failing test that
// prints `6` for an integer is not a diagnostic (roadmap Gap R.242, ADR 0338).
func tokenKindName(k TokenKind) string {
	switch k {
	case TokEOF:
		return "EOF"
	case TokNewline:
		return "NL"
	case TokIndent:
		return "INDENT"
	case TokDedent:
		return "DEDENT"
	case TokIdent:
		return "IDENT"
	case TokInt:
		return "INT"
	case TokFloat:
		return "FLOAT"
	case TokString:
		return "STR"
	case TokRawString:
		return "RAWSTR"
	case TokTripleString:
		return "TRIPLE"
	case TokRawTripleString:
		return "RAWTRIPLE"
	case TokFString:
		return "FSTR"
	case TokOp:
		return "OP"
	case TokKeyword:
		return "KW"
	case TokError:
		return "ERR"
	case TokWarning:
		return "WARN"
	case TokSemi:
		return "SEMI"
	}
	return "?"
}

// tokenStream renders a token stream as `KIND:text` pairs, dropping the statement
// terminators and the EOF every line gets: the question the `-N` rows ask is about the
// tokens that carry the program's meaning, and `Int:-1` beside `OP:- Int:1` is the whole
// difference between `y = x-1` answering `4` and answering `5`.
func tokenStream(t *testing.T, src string) string {
	t.Helper()
	toks, err := Lex(src)
	if err != nil {
		t.Fatalf("Lex(%q): %v", src, err)
	}
	parts := make([]string, 0, len(toks))
	for _, tk := range toks {
		if tk.Kind == TokNewline || tk.Kind == TokEOF || tk.Kind == TokWarning {
			continue
		}
		parts = append(parts, tokenKindName(tk.Kind)+":"+tk.Text)
	}
	return strings.Join(parts, " ")
}

// TestTheMinusBeforeADigitIsAskedWhatItFollows is the token stream both ways (roadmap
// Gap R.242, ADR 0338). The rule used to look only at the character AFTER the minus, so
// `y = x-1` was lexed as `y = x` followed by an expression statement `-1` — the program
// printed the value the subtraction never produced (`5`, where the reference prints `4`)
// at exit 0, and `print(1-1)` did not parse at all. Each row below is a token stream the
// reference's grammar forces; a lexer that drops the context question fails the `1-1`
// rows, and a lexer that always splits fails the `-1` rows.
func TestTheMinusBeforeADigitIsAskedWhatItFollows(t *testing.T) {
	for _, tc := range []struct {
		src  string
		want string
	}{
		// ---- the operator: after something that can END an expression ----
		{"y = x-1", "IDENT:y OP:= IDENT:x OP:- INT:1"},
		{"print(1-1)", "IDENT:print OP:( INT:1 OP:- INT:1 OP:)"},
		{"print(1-1-1)", "IDENT:print OP:( INT:1 OP:- INT:1 OP:- INT:1 OP:)"},
		{"print(x-1)", "IDENT:print OP:( IDENT:x OP:- INT:1 OP:)"},
		{"print(len(xs)-1)", "IDENT:print OP:( IDENT:len OP:( IDENT:xs OP:) OP:- INT:1 OP:)"},
		{"print(xs[2-1])", "IDENT:print OP:( IDENT:xs OP:[ INT:2 OP:- INT:1 OP:] OP:)"},
		{"print((1)-1)", "IDENT:print OP:( OP:( INT:1 OP:) OP:- INT:1 OP:)"},
		{"print(w[\"a\"]-1)", "IDENT:print OP:( IDENT:w OP:[ STR:\"a\" OP:] OP:- INT:1 OP:)"},
		{"print(True-1)", "IDENT:print OP:( KW:True OP:- INT:1 OP:)"},
		{"print(False-1)", "IDENT:print OP:( KW:False OP:- INT:1 OP:)"},
		{"print({1}-1)", "IDENT:print OP:( OP:{ INT:1 OP:} OP:- INT:1 OP:)"},
		{"print(\"ab\"-1)", "IDENT:print OP:( STR:\"ab\" OP:- INT:1 OP:)"},
		{"x = 5\nprint(x-1)", "IDENT:x OP:= INT:5 IDENT:print OP:( IDENT:x OP:- INT:1 OP:)"},
		// the spaced spelling is the same token stream it always was
		{"print(x - 1)", "IDENT:print OP:( IDENT:x OP:- INT:1 OP:)"},
		// `-=` is the longest match and is not a sign, whatever follows it
		{"x-=1", "IDENT:x OP:-= INT:1"},
		// ---- the sign: everywhere else ----
		{"print(-1)", "IDENT:print OP:( INT:-1 OP:)"},
		{"print(-1.5)", "IDENT:print OP:( FLOAT:-1.5 OP:)"},
		{"print(xs[-1])", "IDENT:print OP:( IDENT:xs OP:[ INT:-1 OP:] OP:)"},
		{"print(f(-1))", "IDENT:print OP:( IDENT:f OP:( INT:-1 OP:) OP:)"},
		{"print(1, -1)", "IDENT:print OP:( INT:1 OP:, INT:-1 OP:)"},
		{"print(5*-1)", "IDENT:print OP:( INT:5 OP:* INT:-1 OP:)"},
		{"print(2 ** -1)", "IDENT:print OP:( INT:2 OP:** INT:-1 OP:)"},
		{"x = -1", "IDENT:x OP:= INT:-1"},
		{"print(d[-1:1])", "IDENT:print OP:( IDENT:d OP:[ INT:-1 OP:: INT:1 OP:] OP:)"},
		{"print({-1: \"n\"})", "IDENT:print OP:( OP:{ INT:-1 OP:: STR:\"n\" OP:} OP:)"},
		// a minus after the operator is the sign of the operand the parser already takes
		{"print(5--1)", "IDENT:print OP:( INT:5 OP:- INT:-1 OP:)"},
		{"print(5- -1)", "IDENT:print OP:( INT:5 OP:- INT:-1 OP:)"},
		{"x-=-1", "IDENT:x OP:-= INT:-1"},
		// a `-` inside a text is not a token at all, and a `-` inside a comment is not either
		{"x = int(\"-7\")", "IDENT:x OP:= IDENT:int OP:( STR:\"-7\" OP:)"},
		{"x = 5 # a-1", "IDENT:x OP:= INT:5"},
	} {
		t.Run(tc.src, func(t *testing.T) {
			if got := tokenStream(t, tc.src); got != tc.want {
				t.Errorf("Lex(%q) streamed\n  %s\nwant\n  %s", tc.src, got, tc.want)
			}
		})
	}
}

// TestTheContextQuestionIsAskedOfThePreviousToken is the predicate on its own, so a
// change to the rule cannot hide behind a token-stream row that happens to agree (ADR 0338).
func TestTheContextQuestionIsAskedOfThePreviousToken(t *testing.T) {
	tok := func(k TokenKind, text string) Token { return Token{Kind: k, Text: text} }
	for _, tc := range []struct {
		name string
		toks []Token
		want bool
	}{
		{"start of a line", nil, true},
		{"after a newline", []Token{tok(TokNewline, "\n")}, true},
		{"after an indent", []Token{tok(TokNewline, "\n"), tok(TokIndent, "")}, true},
		{"after an operator", []Token{tok(TokIdent, "x"), tok(TokOp, "*")}, true},
		{"after an open bracket", []Token{tok(TokOp, "[")}, true},
		{"after a comma", []Token{tok(TokIdent, "f"), tok(TokOp, "("), tok(TokOp, ",")}, true},
		{"after an equals", []Token{tok(TokIdent, "a"), tok(TokOp, "=")}, true},
		{"after the return keyword", []Token{tok(TokKeyword, "return")}, true},
		{"after the in keyword", []Token{tok(TokKeyword, "for"), tok(TokIdent, "i"), tok(TokKeyword, "in")}, true},
		{"after a homoglyph note", []Token{tok(TokIdent, "x"), tok(TokWarning, "")}, false},
		{"after a number", []Token{tok(TokInt, "5")}, false},
		{"after a float", []Token{tok(TokFloat, "1.5")}, false},
		{"after a name", []Token{tok(TokIdent, "x")}, false},
		{"after a text", []Token{tok(TokString, "\"a\"")}, false},
		{"after a closing paren", []Token{tok(TokInt, "1"), tok(TokOp, ")")}, false},
		{"after a closing bracket", []Token{tok(TokIdent, "xs"), tok(TokOp, "["), tok(TokInt, "0"), tok(TokOp, "]")}, false},
		{"after a closing brace", []Token{tok(TokOp, "{"), tok(TokInt, "1"), tok(TokOp, "}")}, false},
		{"after the True keyword", []Token{tok(TokKeyword, "True")}, false},
		{"after the None keyword", []Token{tok(TokKeyword, "None")}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := minusBeginsLiteral(tc.toks); got != tc.want {
				t.Errorf("minusBeginsLiteral(%+v) = %v, want %v", tc.toks, got, tc.want)
			}
		})
	}
}

// TestASubtractionIsOneExpressionNotTwo is the parse the token stream decides: `y = x-1`
// used to be TWO statements — an assignment and an expression statement holding `-1` —
// which is why the program printed `5` and why no diagnostic could have told the author.
func TestASubtractionIsOneExpressionNotTwo(t *testing.T) {
	for _, tc := range []struct {
		src   string
		stmts int
		check func(t *testing.T, e Stmt)
	}{
		{"y = x-1", 1, func(t *testing.T, e Stmt) {
			a, ok := e.(*AssignStmt)
			if !ok {
				t.Fatalf("the line is a %T, not an assignment", e)
			}
			b, ok := a.Value.(*BinOp)
			if !ok {
				t.Fatalf("y = x-1 holds a %T, not a subtraction", a.Value)
			}
			if b.Op != "-" {
				t.Errorf("the operator is %q, want -", b.Op)
			}
		}},
		{"print(1-1)", 1, func(t *testing.T, e Stmt) {
			s, ok := e.(*ExprStmt)
			if !ok {
				t.Fatalf("the line is a %T, not an expression statement", e)
			}
			c, ok := s.Expr.(*Call)
			if !ok || len(c.Args) != 1 {
				t.Fatalf("print(1-1) is not a one-argument call: %T", s.Expr)
			}
			if _, ok := c.Args[0].(*BinOp); !ok {
				t.Fatalf("print(1-1) argues with a %T, not a subtraction", c.Args[0])
			}
		}},
		// the sign side must stay a LITERAL, not a unary minus the parser rebuilt
		{"x = -1", 1, func(t *testing.T, e Stmt) {
			a, ok := e.(*AssignStmt)
			if !ok {
				t.Fatalf("the line is a %T, not an assignment", e)
			}
			n, ok := a.Value.(*IntLit)
			if !ok {
				t.Fatalf("x = -1 holds a %T; the sign stopped merging", a.Value)
			}
			if n.Value != -1 {
				t.Errorf("x = -1 is %d", n.Value)
			}
		}},
		{"xs = [1, 2, 3]\nprint(xs[-1])", 2, func(t *testing.T, e Stmt) {
			s, ok := e.(*ExprStmt)
			if !ok {
				t.Fatalf("the line is a %T, not an expression statement", e)
			}
			c, ok := s.Expr.(*Call)
			if !ok || len(c.Args) != 1 {
				t.Fatalf("print(xs[-1]) is not a one-argument call: %T", s.Expr)
			}
			ix, ok := c.Args[0].(*Index)
			if !ok {
				t.Fatalf("xs[-1] is a %T, not a subscript", c.Args[0])
			}
			if n, ok := ix.Idx.(*IntLit); !ok || n.Value != -1 {
				t.Fatalf("xs[-1] indexes with a %T, not the literal -1", ix.Idx)
			}
		}},
	} {
		t.Run(tc.src, func(t *testing.T) {
			p, err := Parse(tc.src)
			if err != nil {
				t.Fatalf("Parse(%q): %v", tc.src, err)
			}
			if len(p.Stmts) != tc.stmts {
				t.Fatalf("Parse(%q) produced %d statements, want %d", tc.src, len(p.Stmts), tc.stmts)
			}
			tc.check(t, p.Stmts[len(p.Stmts)-1])
		})
	}
}
