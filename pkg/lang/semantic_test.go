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
