package lang

import (
	"strings"
	"testing"
)

// Gap N — string literals are text, with Python escapes (ADR 0178).
//
// The lexer used to "decode" an escape by dropping the backslash and keeping
// the next byte, and built ordinary-string values with string(byte), which in Go
// converts the byte to a rune — re-encoding every non-ASCII byte as two bytes.

func lexStringValue(t *testing.T, src string) (string, []Diagnostic) {
	t.Helper()
	raw, err := Lex(src)
	if err != nil {
		t.Fatalf("Lex(%q): %v", src, err)
	}
	var diags []Diagnostic
	var val string
	found := false
	for _, tk := range raw {
		if tk.Kind == TokError {
			diags = append(diags, Diagnostic{Level: LevelError, Span: tk.Span, Msg: tk.ErrMsg})
		}
		if tk.Kind == TokString && !found {
			val = tk.Str
			found = true
		}
	}
	if !found {
		t.Fatalf("no string token lexed from %q", src)
	}
	return val, diags
}

func TestLexerDecodesPythonEscapes(t *testing.T) {
	for _, tc := range []struct{ src, want string }{
		{`x = "a\nb"`, "a\nb"},
		{`x = "a\tb"`, "a\tb"},
		{`x = "a\rb"`, "a\rb"},
		{`x = "a\\b"`, `a\b`},
		{`x = "a\"b"`, `a"b`},
		{`x = "a\'b"`, "a'b"},
		{`x = "a\ab"`, "a\ab"},
		{`x = "a\bb"`, "a\bb"},
		{`x = "a\fb"`, "a\fb"},
		{`x = "a\vb"`, "a\vb"},
		{`x = "a\0b"`, "a\x00b"},
		{`x = "\x41\x42"`, "AB"},
		{`x = "\u00e9"`, "é"},
		{`x = "\U0001F600"`, "😀"},
		{`x = "a\qb"`, `a\qb`},             // unknown escape: kept, as in Python
		{`x = "\x4"`, `\x4`},               // too few hex digits: kept
		{`x = "\uD800"`, `\uD800`},         // a surrogate is not a code point
		{`x = "\U00110000"`, `\U00110000`}, // past Unicode
	} {
		got, diags := lexStringValue(t, tc.src)
		if got != tc.want {
			t.Errorf("%s: value = %q, want %q", tc.src, got, tc.want)
		}
		if len(diags) != 0 {
			t.Errorf("%s: unexpected diagnostics %+v", tc.src, diags)
		}
	}
}

// The value of a UTF-8 literal must be the source's own bytes. The old scanner
// produced "hÃ©llo" for "héllo" because string(byte) re-encodes each byte.
func TestLexerPreservesUTF8Bytes(t *testing.T) {
	for _, word := range []string{"héllo", "café", "日本語", "ñ", "€", "naïve", "🐍"} {
		src := `x = "` + word + `"`
		got, diags := lexStringValue(t, src)
		if got != word {
			t.Errorf("%s: value = %q (%v), want %q", src, got, []byte(got), word)
		}
		if len(diags) != 0 {
			t.Errorf("%s: unexpected diagnostics %+v", src, diags)
		}
	}
}

// Every string form must go through one decoder: the ordinary path used to be a
// duplicate of scanString with different bugs.
func TestAllStringFormsShareEscapes(t *testing.T) {
	cases := []struct{ src, want string }{
		{`x = "a\nb"`, "a\nb"},
		{"x = \"\"\"a\\nb\"\"\"", "a\nb"},
		{`x = r"a\nb"`, `a\nb`}, // raw keeps the backslash
	}
	for _, tc := range cases {
		raw, err := Lex(tc.src)
		if err != nil {
			t.Fatalf("Lex(%q): %v", tc.src, err)
		}
		val := ""
		for _, tk := range raw {
			if tk.Kind == TokString || tk.Kind == TokTripleString || tk.Kind == TokRawString {
				val = tk.Str
			}
		}
		if val != tc.want {
			t.Errorf("%s: value = %q, want %q", tc.src, val, tc.want)
		}
	}
}

// unescapeStr (f-strings) and the lexer must never disagree about an escape:
// they share appendEscape.
func TestUnescapeStrMatchesLexer(t *testing.T) {
	for _, body := range []string{`a\nb`, `a\tb`, `\x41`, `\u00e9`, `\U0001F600`, `a\qb`, `\\`, `\xZZ`, `100%`} {
		lexed, _ := lexStringValue(t, `x = "`+body+`"`)
		if got := unescapeStr(body); got != lexed {
			t.Errorf("escape %q: unescapeStr = %q, lexer = %q", body, got, lexed)
		}
	}
}

// An unterminated string is a diagnostic, not a hang or a silent empty value.
func TestUnterminatedStringDiagnostics(t *testing.T) {
	for _, src := range []string{`x = "abc`, "x = 'abc"} {
		raw, err := Lex(src)
		if err != nil {
			t.Fatalf("Lex(%q): %v", src, err)
		}
		found := false
		for _, tk := range raw {
			if tk.Kind == TokError && strings.Contains(tk.ErrMsg, "unterminated") {
				found = true
			}
		}
		if !found {
			t.Errorf("%q must produce an unterminated-string diagnostic", src)
		}
	}
}
