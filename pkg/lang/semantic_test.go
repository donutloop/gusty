package lang

import "testing"

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
