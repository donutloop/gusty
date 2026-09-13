package integration

import (
	"strings"
	"testing"

	"github.com/donutloop/gusty/pkg/lang"
)

// lit-style integration tests: real source through the full pipeline
// (lex -> parse -> typecheck -> codegen), checking IR shape + eval output.

func TestEvalArithmetic(t *testing.T) {
	v, diags, err := lang.EvalExpr("3 + 4 * 2")
	if err != nil {
		t.Fatalf("eval err: %v", err)
	}
	if len(diags) > 0 {
		t.Fatalf("diags: %v", diags)
	}
	if v != 11 {
		t.Fatalf("expected 11, got %d", v)
	}
}

func TestEvalAssign(t *testing.T) {
	v, diags, err := lang.EvalExpr("x = 5\nx + 1")
	if err != nil {
		t.Fatalf("eval err: %v", err)
	}
	if len(diags) > 0 {
		t.Fatalf("diags: %v", diags)
	}
	if v != 6 {
		t.Fatalf("expected 6, got %d", v)
	}
}

func TestEvalComparisons(t *testing.T) {
	v, _, err := lang.EvalExpr("1 < 2 and 3 == 3")
	if err != nil {
		t.Fatalf("eval err: %v", err)
	}
	if v != 1 {
		t.Fatalf("expected 1, got %d", v)
	}
}

func TestCompileIRContainsMainAndPrintf(t *testing.T) {
	src := `print(40 + 2)`
	res, err := lang.Compile(src)
	if err != nil {
		t.Fatalf("compile err: %v", err)
	}
	ir := res.IR
	if !strings.Contains(ir, "define") {
		t.Fatalf("IR missing function definitions:\n%s", ir)
	}
	if !strings.Contains(ir, "printf") {
		t.Fatalf("IR missing printf:\n%s", ir)
	}
	if !strings.Contains(res.ASTJSON, "print") && !strings.Contains(res.ASTJSON, "Args") {
		t.Fatalf("AST JSON missing call expr:\n%s", res.ASTJSON)
	}
}

func TestCompileWhileLoop(t *testing.T) {
	src := `i = 0
while i < 3:
    print(i)
    i = i + 1`
	res, err := lang.Compile(src)
	if err != nil {
		t.Fatalf("compile err: %v", err)
	}
	if !strings.Contains(res.IR, "while") && !strings.Contains(res.IR, "icmp") {
		t.Fatalf("IR missing loop control:\n%s", res.IR)
	}
}

func TestCompileForRange(t *testing.T) {
	src := `for i in range(3):
    print(i)`
	res, err := lang.Compile(src)
	if err != nil {
		t.Fatalf("compile err: %v", err)
	}
	if !strings.Contains(res.IR, "for") && !strings.Contains(res.IR, "icmp") {
		t.Fatalf("IR missing for-loop:\n%s", res.IR)
	}
}

func TestLexIndentation(t *testing.T) {
	src := "if x:\n    print(1)\nprint(2)"
	toks, err := lang.Lex(src)
	if err != nil {
		t.Fatalf("lex err: %v", err)
	}
	foundIndent := false
	foundDedent := false
	for _, tok := range toks {
		if tok.Kind == lang.TokIndent {
			foundIndent = true
		}
		if tok.Kind == lang.TokDedent {
			foundDedent = true
		}
	}
	if !foundIndent {
		t.Fatalf("expected INDENT token, got %v", toks)
	}
	if !foundDedent {
		t.Fatalf("expected DEDENT token, got %v", toks)
	}
}

func TestUndefinedNameDiagnostic(t *testing.T) {
	_, diags, err := lang.EvalExpr("nope + 1")
	if err != nil {
		t.Fatalf("eval err: %v", err)
	}
	hasErr := false
	for _, d := range diags {
		if d.Level == lang.LevelError {
			hasErr = true
		}
	}
	if !hasErr {
		t.Fatalf("expected an error diagnostic, got %v", diags)
	}
}
