package lang

import (
	"strings"
	"testing"
)

// TestOptimizeIRLevel0 returns IR unchanged at opt-level 0.
func TestOptimizeIRLevel0(t *testing.T) {
	ir := "@.str0 = private unnamed_addr constant [2 x i8] c\"hi\\00\"\n\ndefine i32 @main() {\n  ret i32 0\n}\n"
	if got := OptimizeIR(ir, 0); got != ir {
		t.Fatalf("level 0 changed IR:\n%s", got)
	}
}

// TestOptimizeIRDeadGlobalElim: a program with a pure literal statement whose
// string global is never referenced by the body should have that global pruned
// at level 1. The codegen eagerly emits a @.strN global for the literal even
// though its value is unused.
func TestOptimizeIRDeadGlobalElim(t *testing.T) {
	ir := `@.str0 = private unnamed_addr constant [2 x i8] c"hi\00"
@.str1 = private unnamed_addr constant [4 x i8] c"bye\00"

define i32 @main() {
entry:
  call i32 (i8*, ...) @printf(i8* getelementptr inbounds ([4 x i8], [4 x i8]* @.str1, i32 0, i32 0))
  ret i32 0
}
`
	opt := OptimizeIR(ir, 1)
	// @.str1 is referenced by the body -> kept; @.str0 is dead -> dropped.
	if strings.Contains(opt, "@.str0") {
		t.Fatalf("dead global @.str0 not pruned:\n%s", opt)
	}
	if !strings.Contains(opt, "@.str1") {
		t.Fatalf("live global @.str1 wrongly pruned:\n%s", opt)
	}
}

// TestOptimizeIREmitsFromRealProgram confirms the pass runs over real
// emitted IR from GenerateIR without corrupting the module shape.
func TestOptimizeIREmitsFromRealProgram(t *testing.T) {
	res, err := Compile("print(1)\n")
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	opt := OptimizeIR(res.IR, 1)
	if !strings.Contains(opt, "define i32 @main()") {
		t.Fatalf("optimization lost main():\n%s", opt)
	}
	if !strings.Contains(opt, "@printf") {
		t.Fatalf("optimization lost printf call:\n%s", opt)
	}
}
