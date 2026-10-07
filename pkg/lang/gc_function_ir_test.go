package lang

import (
	"os"
	"os/exec"
	"strings"
	"testing"
)

// The compiled backend's collector, asked through the module it emits.
//
// These two cases came out of the AST record's memory-model suite (deleted with that engine
// by ADR 0302) because they never tested the interpreter: they compile a program and run the IR,
// which is the only way there is now to ask whether a function's locals are rooted and whether a
// body with more allocations than the heap's capacity survives a collection.
// TestIRGCFunctionLocalAllocGC verifies that GC runs inside function bodies at
// top-level statement boundaries, freeing unreachable heap objects so a
// function with more allocations than the 1024-object heap cap completes.
func TestIRGCFunctionLocalAllocGC(t *testing.T) {
	var b strings.Builder
	b.WriteString("def f():\n")
	for i := 0; i < 1200; i++ {
		b.WriteString("    x = [0]\n")
	}
	b.WriteString("    return 0\n")
	b.WriteString("print(f())\n")
	res, err := Compile(b.String())
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	out := runIR(t, res.IR)
	if out != "0\n" {
		t.Fatalf("expected 0\n, got %q", out)
	}
}

// TestIRGCParamRooting verifies that a heap-object parameter survives GC at
// function-body statement boundaries (params are rooted).
func TestIRGCFunctionLocalRooting(t *testing.T) {
	var b strings.Builder
	b.WriteString("def f():\n")
	b.WriteString("    a = [42]\n")
	for i := 0; i < 1100; i++ {
		b.WriteString("    x = [0]\n")
	}
	b.WriteString("    return a[0]\n")
	b.WriteString("print(f())\n")
	res, err := Compile(b.String())
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	out := runIR(t, res.IR)
	if out != "42\n" {
		t.Fatalf("expected 42\n, got %q", out)
	}
}

// runIR lowers a module with llvm-as/lli and returns what the program printed. It came over from
// the interpreter's memory-model file with the two cases above (ADR 0302); a third of this
// package's tests ask the emitted module directly, and this is how they run it.
func runIR(t *testing.T, ir string) string {
	t.Helper()
	tmp, err := os.CreateTemp("", "gc-*.ll")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := tmp.WriteString(ir); err != nil {
		t.Fatal(err)
	}
	tmp.Close()
	defer os.Remove(tmp.Name())
	bc := tmp.Name() + ".bc"
	defer os.Remove(bc)
	if out, err := exec.Command("llvm-as-20", tmp.Name(), "-o", bc).CombinedOutput(); err != nil {
		t.Fatalf("llvm-as: %v\n%s", err, out)
	}
	out, err := exec.Command("lli-20", bc).CombinedOutput()
	if err != nil {
		t.Fatalf("lli: %v\n%s", err, out)
	}
	return string(out)
}
