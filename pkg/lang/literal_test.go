package lang

import "testing"

func litOf(t *testing.T, src string) *Type {
	t.Helper()
	prog, err := Parse(src)
	if err != nil {
		t.Fatalf("parse %q: %v", src, err)
	}
	fd, ok := prog.Stmts[0].(*FuncDef)
	if !ok || len(fd.Params) == 0 {
		t.Fatalf("expected a func with a param: %s", src)
	}
	return fd.Params[0].Annot
}

func TestLiteralParseSingle(t *testing.T) {
	ty := litOf(t, "def f(x: Literal[1]) -> int:\n    return x\n")
	if ty.Kind != KindLiteral || ty.LitVal != 1 {
		t.Fatalf("got %v, want Literal[1]", ty.Name())
	}
	if ty.Name() != "Literal[1]" {
		t.Errorf("Name() = %s, want Literal[1]", ty.Name())
	}
}

func TestLiteralParseUnion(t *testing.T) {
	ty := litOf(t, "def f(x: Literal[1, 2]) -> int:\n    return x\n")
	if ty.Kind != KindUnion || len(ty.Members) != 2 {
		t.Fatalf("got %v, want Literal[1, 2] union", ty.Name())
	}
	if ty.Members[0].LitVal != 1 || ty.Members[1].LitVal != 2 {
		t.Errorf("members = %v, %v; want 1, 2", ty.Members[0].LitVal, ty.Members[1].LitVal)
	}
}

func hasWarning(diags []Diagnostic) bool {
	for _, d := range diags {
		if d.Level == LevelWarning {
			return true
		}
	}
	return false
}

func hasError(diags []Diagnostic) bool {
	for _, d := range diags {
		if d.Level == LevelError {
			return true
		}
	}
	return false
}

func TestLiteralMatchExhaustiveCovered(t *testing.T) {
	src := "def f(x: Literal[1]):\n    match x:\n        case 1:\n            print(x)\n"
	prog, err := Parse(src)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	diags := Analyze(prog)
	if hasWarning(diags) {
		t.Fatalf("expected exhaustive match (value 1 covered), got warnings: %v", diags)
	}
}

func TestLiteralMatchExhaustiveUncovered(t *testing.T) {
	src := "def f(x: Literal[1]):\n    match x:\n        case 2:\n            print(x)\n"
	prog, err := Parse(src)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	diags := Analyze(prog)
	if !hasWarning(diags) {
		t.Fatalf("expected an exhaustiveness warning (value 1 not covered), got %v", diags)
	}
}


func TestLiteralUnionMatchExhaustiveCovered(t *testing.T) {
	src := "def f(x: Literal[1, 2]):\n    match x:\n        case 1:\n            print(x)\n        case 2:\n            print(x)\n"
	prog, err := Parse(src)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	diags := Analyze(prog)
	if hasWarning(diags) {
		t.Fatalf("expected exhaustive match over Literal[1,2], got warnings: %v", diags)
	}
}

func TestLiteralUnionMatchExhaustiveUncovered(t *testing.T) {
	src := "def f(x: Literal[1, 2]):\n    match x:\n        case 1:\n            print(x)\n"
	prog, err := Parse(src)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	diags := Analyze(prog)
	if !hasWarning(diags) {
		t.Fatalf("expected exhaustiveness warning (value 2 not covered), got %v", diags)
	}
}

func TestLiteralReturnMismatch(t *testing.T) {
	// Returning a Literal[1] where Literal[2] is expected must be a type error.
	src := "def f(x: Literal[1]) -> Literal[2]:\n    return x\n"
	prog, err := Parse(src)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	diags := Analyze(prog)
	if !hasError(diags) {
		t.Fatalf("expected a type error returning Literal[1] as Literal[2], got %v", diags)
	}
}

func TestLiteralReturnMatch(t *testing.T) {
	src := "def f(x: Literal[1]) -> Literal[1]:\n    return x\n"
	prog, err := Parse(src)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	diags := Analyze(prog)
	if hasError(diags) {
		t.Fatalf("expected Literal[1] return to satisfy Literal[1], got %v", diags)
	}
}
