package lang

// An f-string field is a fragment of a line, and the lexer must not be told it is a line.
//
// `buildFString` re-lexes the inside of one `{…}` as an expression. The lexer measures a line's leading blanks
// before it reads a token, so the two spaces in `f"{ x }"` became an INDENT, the sub-parser met a token where
// an expression should start, and the whole program died with:
//
//	parse error at 1:2: unexpected token
//
// for a spelling CPython prints and `f"{x}"` prints. Every padded field was a syntax error, and with them
// went every field that opens with a brace — a dict or set literal — because the unpadded spelling of those
// is not something anyone writes by choice (`(roadmap Gap R.294, ADR 0356)`).
//
// The rows below are the front-end half: what parses, what still does not, and the exact tokens the fragment
// lexer is allowed to invent.

import (
	"reflect"
	"strings"
	"testing"
)

// fieldExpr digs the first interpolation out of a source's only statement.
func fieldExpr(t *testing.T, src string) *FStringPart {
	t.Helper()
	prog, err := Parse(src)
	if err != nil {
		t.Fatalf("the front end refused a program the reference parses: %v\n%s", err, src)
	}
	es, ok := prog.Stmts[len(prog.Stmts)-1].(*ExprStmt)
	if !ok {
		for _, st := range prog.Stmts {
			if e, isExpr := st.(*ExprStmt); isExpr {
				es = e
				ok = true
				break
			}
		}
	}
	if !ok {
		t.Fatalf("%s: no expression statement to read the field from", src)
	}
	call, ok := es.Expr.(*Call)
	if !ok || len(call.Args) == 0 {
		t.Fatalf("%s: expected a one-argument call", src)
	}
	fs, ok := call.Args[0].(*FString)
	if !ok || len(fs.Parts) == 0 {
		t.Fatalf("%s: expected an f-string argument", src)
	}
	for i := range fs.Parts {
		if fs.Parts[i].Expr != nil {
			return &fs.Parts[i]
		}
	}
	t.Fatalf("%s: the f-string has no interpolated field", src)
	return nil
}

func TestAPaddedFieldParsesToTheSameTreeAsAnUnpaddedOne(t *testing.T) {
	t.Parallel()
	plain := fieldExpr(t, "x = 5\nprint(f\"{x}\")\n")
	padded := fieldExpr(t, "x = 5\nprint(f\"{ x }\")\n")
	if !sameShape(plain.Expr, padded.Expr) {
		t.Fatalf("the padded field parsed to a different tree: %#v vs %#v", plain.Expr, padded.Expr)
	}
	if nm, ok := padded.Expr.(*Name); !ok || nm.Value != "x" {
		t.Fatalf("the padded field is no longer a bare name x: %T", padded.Expr)
	}
	// The padding is dropped from the TOKENS, not from the map: the field's own column is where the author
	// wrote the name, and a diagnostic that blamed column 1 of a field that starts at column 2 would be a
	// diagnostic about a token nobody wrote.
	if nm := padded.Expr.(*Name); nm.Src.Col != 2 {
		t.Fatalf("the padded field's name claims column %d; the fragment kept no columns", nm.Src.Col)
	}
}

// sameShape compares two fragments by type and spelling, ignoring where in the fragment they sit: the whole
// point of keeping columns is that they may differ by the padding.
func sameShape(a, b Expr) bool {
	if reflect.TypeOf(a) != reflect.TypeOf(b) {
		return false
	}
	va := reflect.ValueOf(a).Elem().FieldByName("Value")
	vb := reflect.ValueOf(b).Elem().FieldByName("Value")
	if va.IsValid() != vb.IsValid() {
		return false
	}
	if !va.IsValid() {
		return true
	}
	return va.Interface() == vb.Interface()
}

func TestTheFieldShapesThatWereSyntaxErrorsAreNotAnyMore(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name string
		src  string
	}{
		{"a leading space", "x = 5\nprint(f\"{ x}\")\n"},
		{"a trailing space", "x = 5\nprint(f\"{x }\")\n"},
		{"both", "x = 5\nprint(f\"{ x }\")\n"},
		{"a dict literal field", "print(f\"{ {'a': 1.5}['a'] }\")\n"},
		{"a set literal field", "print(f\"{ {1, 2} }\")\n"},
		{"a list literal field", "print(f\"{ [1, 2] }\")\n"},
		{"a tab-indented pad", "x = 5\nprint(f\"{  x  }\")\n"},
		{"a pad before a conversion", "s = \"a\"\nprint(f\"{ s !r}\")\n"},
		{"a pad inside a compound field", "d = {1: 3.5}\nprint(f\"{ d[1] + 1 }\")\n"},
	} {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if _, err := Parse(tc.src); err != nil {
				t.Fatalf("the front end refused a spelling the reference parses: %v\n%s", err, tc.src)
			}
			if out, refused := compiledOutOrRefusal(t, tc.src); refused && strings.Contains(out, "parse error") {
				t.Fatalf("the padded field still does not parse: %s\n%s", out, tc.src)
			}
		})
	}
}

// A conversion and a spec are read off the END of the field, and padding must not move them: `{ s !r}` is the
// repr of s, `{ x :>6}` is a width spec, and the padding is nothing at all.
func TestPaddingNeverMovesAConversionOrASpec(t *testing.T) {
	t.Parallel()
	s := fieldExpr(t, "s = \"a\"\nprint(f\"{ s !r}\")\n")
	if s.Conv != ConvRepr {
		t.Fatalf("the padded field lost its !r conversion: conv = %v", s.Conv)
	}
	spec := fieldExpr(t, "print(f\"{ 5.5 :.2f}\")\n")
	if spec.Spec != ".2f" {
		t.Fatalf("the padded field lost its format spec: spec = %q, want %q", spec.Spec, ".2f")
	}
	if out := compiledOut(t, "print(f\"{ 5.5 :.2f}\")\n"); out != "5.50\n" {
		t.Fatalf("the padded spec field printed %q, want \"5.50\\n\"", out)
	}
	plain := fieldExpr(t, "print(f\"{5.5:.2f}\")\n")
	if plain.Spec != spec.Spec || plain.Conv != spec.Conv {
		t.Fatalf("padding changed the field's presentation: %q/%v vs %q/%v", spec.Spec, spec.Conv, plain.Spec, plain.Conv)
	}
}

// The fragment lexer is allowed to drop the tokens its own padding invents, and only those. A token stream
// with nothing left in it is a bug, not a clean-up.
func TestLexFieldSrcDropsOnlyThePaddingTheFragmentInvents(t *testing.T) {
	t.Parallel()
	for _, src := range []string{"", "   ", "\t\t", "{1:2}", " {1:2} ", " x ", "  "} {
		toks, err := lexFieldSrc(src)
		if err != nil {
			t.Fatalf("lexFieldSrc(%q): %v", src, err)
		}
		for _, tk := range toks {
			if tk.Kind == TokIndent || tk.Kind == TokDedent {
				t.Fatalf("lexFieldSrc(%q) kept an indentation token the fragment invented: %+v", src, tk)
			}
			if tk.Kind == TokEOF && !(len(toks) > 0 && toks[len(toks)-1].Kind == TokEOF) {
				t.Fatalf("lexFieldSrc(%q) lost its EOF terminator", src)
			}
		}
		if len(toks) == 0 {
			t.Fatalf("lexFieldSrc(%q) returned no tokens at all, not even EOF", src)
		}
	}
	if toks, err := lexFieldSrc(" {1:2} "); err != nil || len(toks) != 6 {
		t.Fatalf("lexFieldSrc(\" {1:2} \") = %d tokens (%v), want the five of `{1:2}` plus EOF", len(toks), err)
	}
}
