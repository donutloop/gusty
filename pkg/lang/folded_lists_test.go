package lang

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// mustVerifyWithLLC asks LLVM itself whether the module is well-formed. It is
// the offline form of lang.VerifyModuleIR (L8.2), used here so the fix lands
// independently of the verifier pipeline.
func mustVerifyWithLLC(t *testing.T, ir string) {
	t.Helper()
	dir := t.TempDir()
	p := filepath.Join(dir, "prog.ll")
	if err := os.WriteFile(p, []byte(ir), 0o600); err != nil {
		t.Fatal(err)
	}
	out, err := exec.Command("llc-20", "-filetype=null", "-relocation-model=pic", p).CombinedOutput()
	if err != nil {
		t.Fatalf("LLVM rejected the module: %v\n%s\n%s", err, out, ir)
	}
}

// TestFoldedListAssignmentMaterializesToHeap locks the codegen bug the L8.2
// verifier caught: a comprehension whose elements are all constants is folded
// into a compile-time list global (@.lstN). Assigning such a list to a variable
// used to store that global straight into an i32 slot —
//
//	store i32 @.lst1, i32* %_ys
//
// which LLVM rejects ("global variable reference must have pointer type"), and
// which made print(ys) print the address instead of the list. The variable must
// hold a runtime heap handle instead.
func TestFoldedListAssignmentMaterializesToHeap(t *testing.T) {
	res, err := Compile("ys = [x * 2 for x in [1, 2]]\nprint(ys)\n")
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}
	if strings.Contains(res.IR, "store i32 @.lst") {
		t.Errorf("a folded list global must never be stored as an i32:\n%s", res.IR)
	}
	if !strings.Contains(res.IR, "call i32 @rt_alloc(i32 1)") {
		t.Errorf("assigning a comprehension must allocate a runtime list:\n%s", res.IR)
	}
	if !strings.Contains(res.IR, "call void @rt_set_elem(") {
		t.Errorf("the folded elements must be written into the heap list:\n%s", res.IR)
	}
	if !strings.Contains(res.IR, "rt_print_list") {
		t.Errorf("print(ys) must print the container, not a number:\n%s", res.IR)
	}
	mustVerifyWithLLC(t, res.IR)
}

// TestFoldedListAssignmentsMatchLiteralShape: `ys = [..comprehension..]` behaves
// like `ys = [..literal..]` for every consumer — indexing, len, and passing to a
// function — because both end up holding a runtime handle.
func TestFoldedListAssignmentsMatchLiteralShape(t *testing.T) {
	res, err := Compile("ys = [i * i for i in range(4)]\nprint(len(ys))\nprint(ys[2])\n")
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}
	for _, want := range []string{"rt_list_len", "rt_get_elem"} {
		if !strings.Contains(res.IR, want) {
			t.Errorf("len()/indexing on an assigned comprehension must use %s:\n%s", want, res.IR)
		}
	}
	if strings.Contains(res.IR, "store i32 @.lst") {
		t.Errorf("no folded global may be stored as a scalar:\n%s", res.IR)
	}
}

// TestFoldedListStillConstantFolds: the compile-time global is still emitted for
// consumers that read it structurally (indexing into the literal itself), so the
// fix costs the optimization, only the scalar store.
func TestFoldedListStillConstantFolds(t *testing.T) {
	res, err := Compile("ys = [x * 2 for x in [1, 2, 3]]\nprint(ys)\n")
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}
	if !strings.Contains(res.IR, "@.lst1 = private global {i32, [3 x i32]}") {
		t.Errorf("the folded global should still exist:\n%s", res.IR)
	}
}

// TestFoldedListRebindFreesPreviousSlot keeps the GC discipline: reassigning a
// list variable releases the previous heap slot, as it does for literals.
func TestFoldedListRebindFreesPreviousSlot(t *testing.T) {
	res, err := Compile("ys = [x * 2 for x in [1, 2]]\nys = [i for i in range(3)]\nprint(ys)\n")
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}
	if !strings.Contains(res.IR, "call void @rt_free(") {
		t.Errorf("rebinding a list variable must free its old heap slot:\n%s", res.IR)
	}
	if strings.Contains(res.IR, "store i32 @.lst") {
		t.Errorf("no folded global may be stored as a scalar:\n%s", res.IR)
	}
}
