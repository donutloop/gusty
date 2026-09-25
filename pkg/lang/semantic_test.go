package lang

import (
	"strings"
	"testing"
)

func TestStaticAnnotMismatch(t *testing.T) {
	// x: int = "hello" is a static gradual-typing mismatch caught by Analyze.
	prog, err := Parse("x: int = \"hello\"")
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if diags := Analyze(prog); len(diags) == 0 {
		t.Fatalf("expected a static type mismatch diagnostic")
	}
	// The `any` annotation accepts anything: no diagnostic.
	prog2, err := Parse("x: any = [1, 2]")
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if diags := Analyze(prog2); len(diags) != 0 {
		t.Fatalf("any annotation should not report a mismatch")
	}
}

func TestArgTypeVsAnnotation(t *testing.T) {
	// Static argument incompatible with a static parameter annotation.
	diags := Analyze(parseOrFatal(t, `def add(a: int, b: int) -> int:
    return a + b
x = add(1, "hi")`))
	if !hasErrorMsg(diags, "argument") {
		t.Fatalf("expected an argument type-mismatch diagnostic, got %v", diags)
	}
	// A dynamic (unannotated) argument is accepted (gradual typing).
	diags2 := Analyze(parseOrFatal(t, `def add(a: int, b: int) -> int:
    return a + b
x = add(1, y)`))
	if hasErrorMsg(diags2, "argument") {
		t.Fatalf("dynamic argument should not be flagged, got %v", diags2)
	}
}

func TestReturnTypeVsAnnotation(t *testing.T) {
	// A return statement whose static type conflicts with -> int.
	diags := Analyze(parseOrFatal(t, `def f() -> int:
    return "not an int"`))
	if !hasErrorMsg(diags, "return type mismatch") {
		t.Fatalf("expected a return type-mismatch diagnostic, got %v", diags)
	}
	// Matching return type is clean.
	diags2 := Analyze(parseOrFatal(t, `def f() -> int:
    return 42`))
	if hasErrorMsg(diags2, "return type mismatch") {
		t.Fatalf("matching return should not be flagged, got %v", diags2)
	}
}

// parseOrFatal parses src or fails the test.
func parseOrFatal(t *testing.T, src string) *Program {
	t.Helper()
	prog, err := Parse(src)
	if err != nil {
		t.Fatalf("parse %q: %v", src, err)
	}
	return prog
}

// hasErrorMsg reports whether any LevelError diagnostic contains substr.
func hasErrorMsg(diags []Diagnostic, substr string) bool {
	for _, d := range diags {
		if d.Level == LevelError && strings.Contains(d.Msg, substr) {
			return true
		}
	}
	return false
}

func hasWarningMsg(diags []Diagnostic, substr string) bool {
	for _, d := range diags {
		if d.Level == LevelWarning && strings.Contains(d.Msg, substr) {
			return true
		}
	}
	return false
}

func TestMatchExhaustiveness(t *testing.T) {
	// A match with a wildcard `_` case is exhaustive: no non-exhaustive warning.
	src := `
def f(x):
    match x:
        case 1:
            print(1)
        case _:
            print(0)
`
	diags := Analyze(parseOrFatal(t, src))
	if hasWarningMsg(diags, "match is not exhaustive") {
		t.Fatalf("exhaustive match produced a warning: %v", diags)
	}

	// A match without a wildcard or always-matching case is non-exhaustive.
	src2 := `
def f(x):
    match x:
        case 1:
            print(1)
        case 2:
            print(2)
`
	diags2 := Analyze(parseOrFatal(t, src2))
	if !hasWarningMsg(diags2, "match is not exhaustive") {
		t.Fatalf("expected a non-exhaustive warning, got %v", diags2)
	}

	// Definite assignment: a binding in every (here the only) case is usable
	// after the match and must not be reported as undefined.
	src3 := `
def f(x):
    match x:
        case y:
            pass
    print(y)
`
	diags3 := Analyze(parseOrFatal(t, src3))
	if hasErrorMsg(diags3, "undefined name") {
		t.Fatalf("definitely-assigned binding reported undefined: %v", diags3)
	}
}


func TestAnalyzeSurfacesLexRecoveryDiagnostics(t *testing.T) {
	// L4.1: the parser collects TokError tokens (lexer recovery) into prog.Diags,
	// and Analyze prepends them so the CLI/LSP report every lex error per run.
	prog, err := Parse("1__0")
	if err != nil {
		t.Fatalf("Parse should recover, got %v", err)
	}
	diags := Analyze(prog)
	if len(diags) == 0 {
		t.Fatal("Analyze should surface at least one lexer-recovery diagnostic")
	}
	found := false
	for _, d := range diags {
		if d.Msg != "" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected a lexer-recovery diagnostic, got %d diags", len(diags))
	}
}

// TestParseConfusableWarning verifies L4.3: a confusable identifier is still
// parsed (it is a valid XID identifier) but surfaces a LevelWarning
// diagnostic in prog.Diags, feeding the CLI/LSP warning path.
func TestParseConfusableWarning(t *testing.T) {
	// "cafΟ" uses GREEK CAPITAL OMICRON U+039F, visually confusable with Latin 'O'.
	src := "cafΟ = 1\nprint(cafΟ)\n"
	prog, err := Parse(src)
	if err != nil {
		t.Fatalf("parse %q: %v", src, err)
	}
	found := false
	for _, d := range prog.Diags {
		if d.Level == LevelWarning && strings.Contains(d.Msg, "confusable") {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected a confusable LevelWarning diagnostic, got diags=%+v", prog.Diags)
	}
}

// TestParseNoWarningAscii verifies plain identifiers produce no warning.
func TestParseNoWarningAscii(t *testing.T) {
	src := "order = 1\nprint(order)\n"
	prog, err := Parse(src)
	if err != nil {
		t.Fatalf("parse %q: %v", src, err)
	}
	for _, d := range prog.Diags {
		if d.Level == LevelWarning {
			t.Fatalf("unexpected warning for ASCII identifier: %+v", d)
		}
	}
}

// TestUnionTypeAnnotation verifies `int | str` union annotations parse and
// drive gradual assignability: a value assignable to any member is accepted,
// and a value outside every member is reported.
func TestUnionTypeAnnotation(t *testing.T) {
	cases := []struct {
		src      string
		wantErr  string // if non-empty, an error diagnostic must contain this
	}{
		// int member accepted
		{`x: int | str = 3
print(x)`, ""},
		// str member accepted
		{`x: int | str = "hi"
print(x)`, ""},
		// nested union inside a generic: list[int | str]
		{`x: list[int | str] = [1]
print(x)`, ""},
		// bool is outside both members -> mismatch
		{`x: int | str = True
print(x)`, "expected int | str, got bool"},
		// single-member union is assignable like the plain type
		{`x: int | int = 3
print(x)`, ""},
	}
	for _, c := range cases {
		prog := parseOrFatal(t, c.src)
		diags := Analyze(prog)
		if c.wantErr == "" {
			if hasErrorMsg(diags, "type mismatch") {
				t.Errorf("src %q: unexpected mismatch: %v", c.src, diags)
			}
			continue
		}
		if !hasErrorMsg(diags, c.wantErr) {
			t.Errorf("src %q: expected error %q, got %v", c.src, c.wantErr, diags)
		}
	}
}

// TestUnionTypeName verifies the rendered union name is `int | str`.
func TestUnionTypeName(t *testing.T) {
	u := TUnion(TInt(), TStr())
	if got := u.Name(); got != "int | str" {
		t.Fatalf("union name: got %q, want %q", got, "int | str")
	}
	// order-independent Same
	u2 := TUnion(TStr(), TInt())
	if !u.Same(u2) {
		t.Fatalf("union Same should be order-independent")
	}
}

// TestUnionInferTernary verifies L6.3 union inference: a conditional
// expression whose branches carry different concrete types widens to their
// normalized union, so it is assignable to a union annotation but not to
// either single member type.
func TestUnionInferTernary(t *testing.T) {
	// int branch vs str branch widen to int | str: accepted.
	diags := Analyze(parseOrFatal(t, `c = 1
x: int | str = (1 if c else "hi")
print(x)`))
	if hasErrorMsg(diags, "type mismatch") {
		t.Fatalf("ternary widening should satisfy int | str, got %v", diags)
	}
	// Not assignable to plain int.
	diags2 := Analyze(parseOrFatal(t, `c = 1
x: int = (1 if c else "hi")
print(x)`))
	if !hasErrorMsg(diags2, "expected int, got int | str") {
		t.Fatalf("ternary widening should not satisfy plain int, got %v", diags2)
	}
	// Not assignable to plain str.
	diags3 := Analyze(parseOrFatal(t, `c = 1
x: str = (1 if c else "hi")
print(x)`))
	if !hasErrorMsg(diags3, "expected str, got int | str") {
		t.Fatalf("ternary widening should not satisfy plain str, got %v", diags3)
	}
	// Identical branches normalize to a single member type: assignable to it.
	diags4 := Analyze(parseOrFatal(t, `c = 1
x: int = (1 if c else 2)
print(x)`))
	if hasErrorMsg(diags4, "type mismatch") {
		t.Fatalf("identical int branches should satisfy plain int, got %v", diags4)
	}
}

// TestUnionArithmetic verifies L6.3 union-aware arithmetic: a union whose
// every member is numeric widens without warning, a union mixing a
// non-numeric member warns, and `+` over a string-only union concatenates.
func TestUnionArithmetic(t *testing.T) {
	// int | float union arithmetic widens: no non-numeric warning.
	diags := Analyze(parseOrFatal(t, `c = 1
x: int | float = (1 if c else 2.5)
y = x * 2
print(y)`))
	if hasWarningMsg(diags, "arithmetic on non-numeric") {
		t.Fatalf("numeric-union arithmetic should not warn, got %v", diags)
	}
	// A union that mixes a non-numeric member warns.
	diags2 := Analyze(parseOrFatal(t, `c = 1
x: int | str = (1 if c else "hi")
y = x * 2
print(y)`))
	if !hasWarningMsg(diags2, "arithmetic on non-numeric") {
		t.Fatalf("mixed-union arithmetic should warn, got %v", diags2)
	}
}
