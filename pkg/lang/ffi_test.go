package lang

import (
	"os"
	"strings"
	"testing"
)

// parser: extern fn declaration parses into an ExternDecl statement.
func TestParseExternDecl(t *testing.T) {
	prog, err := Parse("extern fn abs(x: int) -> int\nextern fn strlen(s: str) -> int\n")
	if err != nil {
		t.Fatalf("parse extern: %v", err)
	}
	var externs []*ExternDecl
	for _, st := range prog.Stmts {
		if ed, ok := st.(*ExternDecl); ok {
			externs = append(externs, ed)
		}
	}
	if len(externs) != 2 {
		t.Fatalf("expected 2 externs, got %d", len(externs))
	}
	if externs[0].Name != "abs" || len(externs[0].Params) != 1 {
		t.Fatalf("extern abs wrong: %+v", externs[0])
	}
	if externs[1].Name != "strlen" || len(externs[1].Params) != 1 {
		t.Fatalf("extern strlen wrong: %+v", externs[1])
	}
	if externs[1].Params[0].Annot == nil || externs[1].Params[0].Annot.Kind != KindString {
		t.Fatalf("strlen param should be str: %+v", externs[1].Params[0].Annot)
	}
}

// codegen: extern declarations emit C prototypes and calls are lowered.
func TestGenerateIRExtern(t *testing.T) {
	prog, err := Parse("extern fn abs(x: int) -> int\nabs(-5)")
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	ir, err := GenerateIR(prog)
	if err != nil {
		t.Fatalf("GenerateIR: %v", err)
	}
	if !strings.Contains(ir, "declare i32 @abs(i32)") {
		t.Fatalf("missing extern declare:\n%s", ir)
	}
	if !strings.Contains(ir, "call i32 @abs(") {
		t.Fatalf("missing extern call:\n%s", ir)
	}
}

// codegen: strlen with a string literal passes an i8* to the C function.
func TestGenerateIRExternStr(t *testing.T) {
	prog, err := Parse("extern fn strlen(s: str) -> int\nstrlen(\"hello\")")
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	ir, err := GenerateIR(prog)
	if err != nil {
		t.Fatalf("GenerateIR: %v", err)
	}
	if !strings.Contains(ir, "declare i32 @strlen(i8*)") {
		t.Fatalf("missing strlen declare:\n%s", ir)
	}
	if !strings.Contains(ir, "call i32 @strlen(i8*") {
		t.Fatalf("missing strlen call:\n%s", ir)
	}
}

// interpreter: extern calls dispatch to the Go registry.
func TestEvalExtern(t *testing.T) {
	// abs(-5) == 5
	v, _, err := EvalExpr("extern fn abs(x: int) -> int\nabs(-5)")
	if err != nil {
		t.Fatalf("abs: %v", err)
	}
	if v != 5 {
		t.Fatalf("abs(-5) = %d, want 5", v)
	}
	// getpid() returns the process id
	v, _, err = EvalExpr("extern fn getpid() -> int\ngetpid()")
	if err != nil {
		t.Fatalf("getpid: %v", err)
	}
	if v != int64(os.Getpid()) {
		t.Fatalf("getpid() = %d, want %d", v, os.Getpid())
	}
	// strlen("hello") == 5
	v, _, err = EvalExpr("extern fn strlen(s: str) -> int\nstrlen(\"hello\")")
	if err != nil {
		t.Fatalf("strlen: %v", err)
	}
	if v != 5 {
		t.Fatalf("strlen(hello) = %d, want 5", v)
	}
}

// semantic: extern arity is type-checked.
func TestExternArityCheck(t *testing.T) {
	prog, err := Parse("extern fn abs(x: int) -> int\nabs(1, 2)")
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	diags := Analyze(prog)
	found := false
	for _, d := range diags {
		if strings.Contains(d.Msg, "abs") && strings.Contains(d.Msg, "expects") {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected extern arity diagnostic, got: %v", diags)
	}
}
