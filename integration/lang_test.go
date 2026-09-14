// Package integration exercises gusty end-to-end by executing the code the
// compiler actually emits, rather than only inspecting the IR text.
//
// Flow: source -> lex/parse -> semantic -> codegen (textual LLVM IR)
//   -> llc-20 (LLVM 20 module verification + object code)
//   -> cc (link) -> run the native binary -> compare captured stdout.
//
// Running llc-20 on the emitted IR is the module-verification step: a module
// that does not verify aborts here with the verifier output in the failure.
package integration

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/donutloop/gusty/pkg/lang"
)

const llc = "llc-20"

// compileAndRun drives the full native pipeline and returns the program's stdout.
func compileAndRun(t *testing.T, src string) string {
	t.Helper()
	res, err := lang.Compile(src)
	if err != nil {
		t.Fatalf("compile %q: %v", src, err)
	}
	if res.IR == "" {
		t.Fatalf("compile %q: empty IR", src)
	}
	dir := t.TempDir()
	irPath := filepath.Join(dir, "prog.ll")
	objPath := filepath.Join(dir, "prog.o")
	binPath := filepath.Join(dir, "prog")
	if err := os.WriteFile(irPath, []byte(res.IR), 0o600); err != nil {
		t.Fatalf("write IR: %v", err)
	}

	// llc-20: verifies the module and lowers it to object code.
	// Opaque pointers are the default in LLVM 20, so no -opaque-pointers flag.
	// -relocation-model=pic: llc otherwise defaults to the static relocation
	// model, which emits 32-bit absolute relocations (e.g. R_X86_64_32) for the
	// string constants in .rodata. The default PIE link (cc) rejects those with
	// "relocation R_X86_64_32 against '.rodata.str1.1' can not be used when
	// making a PIE object". PIC codegen uses RIP-relative references instead.
	out, err := exec.Command(llc, "-filetype=obj", "-relocation-model=pic", irPath, "-o", objPath).CombinedOutput()
	if err != nil {
		t.Fatalf("llc-20 rejected module for %q: %v\n%s\nIR:\n%s", src, err, out, res.IR)
	}

	// link the object into a native executable.
	if out, err := exec.Command("cc", objPath, "-o", binPath).CombinedOutput(); err != nil {
		t.Fatalf("link failed for %q: %v\n%s", src, err, out)
	}

	// execute and capture stdout.
	out, err = exec.Command(binPath).Output()
	if err != nil {
		t.Fatalf("run failed for %q: %v", src, err)
	}
	return string(out)
}

// assertOutput compiles src, executes it, and asserts the captured stdout.
func assertOutput(t *testing.T, src, want string) {
	t.Helper()
	if got := compileAndRun(t, src); got != want {
		t.Fatalf("output for %q:\n got %q\nwant %q", src, got, want)
	}
}

func TestExecPrintArithmetic(t *testing.T) {
	assertOutput(t, "print(40 + 2)", "42\n")
	assertOutput(t, "print(2 * 21)", "42\n")
	assertOutput(t, "print(84 / 2)", "42\n")
	assertOutput(t, "print(7 - 1 + 36)", "42\n")
}

func TestExecAssignAndRead(t *testing.T) {
	assertOutput(t, "x = 5\nprint(x + 1)", "6\n")
	assertOutput(t, "a = 2\nb = 40\nprint(a * b)", "80\n")
}

func TestExecIfElse(t *testing.T) {
	assertOutput(t, "x = 1\nif x < 2:\n    print(10)\nelse:\n    print(20)", "10\n")
	assertOutput(t, "x = 3\nif x < 2:\n    print(10)\nelse:\n    print(20)", "20\n")
}

func TestExecWhileSum(t *testing.T) {
	src := "s = 0\ni = 0\nwhile i < 5:\n    s = s + i\n    i = i + 1\nprint(s)"
	assertOutput(t, src, "10\n")
}

func TestExecForRange(t *testing.T) {
	src := "s = 0\nfor i in range(5):\n    s = s + i\nprint(s)"
	assertOutput(t, src, "10\n")
}

func TestExecForRangeTwoArg(t *testing.T) {
	src := "s = 0\nfor i in range(2, 5):\n    s = s + i\nprint(s)"
	assertOutput(t, src, "9\n")
}

func TestExecNestedLoops(t *testing.T) {
	src := "s = 0\nfor i in range(3):\n    for j in range(3):\n        s = s + i + j\nprint(s)"
	// rows: (0+0)+(0+1)+(0+2)=3, (1+0)+(1+1)+(1+2)=6, (2+0)+(2+1)+(2+2)=9 => 18
	assertOutput(t, src, "18\n")
}

func TestExecFunctionReturn(t *testing.T) {
	src := "def double(x):\n    return x * 2\nprint(double(5))"
	assertOutput(t, src, "10\n")
}

func TestExecFunctionTwoParams(t *testing.T) {
	src := "def add(a, b):\n    return a + b\nprint(add(3, 4))"
	assertOutput(t, src, "7\n")
}

func TestExecFunctionComposition(t *testing.T) {
	src := "def double(x):\n    return x * 2\ndef add(a, b):\n    return a + b\nprint(add(double(3), double(4)))"
	assertOutput(t, src, "14\n")
}

func TestExecMatch(t *testing.T) {
	src := "x = 2\nmatch x:\n    case 1:\n        print(1)\n    case 2:\n        print(2)\n    case 3:\n        print(3)"
	assertOutput(t, src, "2\n")
}

func TestExecBreak(t *testing.T) {
	src := "i = 0\nwhile i < 100:\n    i = i + 1\n    if i == 3:\n        break\nprint(i)"
	assertOutput(t, src, "3\n")
}

func TestExecContinue(t *testing.T) {
	// sum 0..4 skipping 2 => 0+1+3+4 = 8
	src := "s = 0\nfor i in range(5):\n    if i == 2:\n        continue\n    s = s + i\nprint(s)"
	assertOutput(t, src, "8\n")
}

func TestExecMultiplePrints(t *testing.T) {
	src := "print(1)\nprint(2)\nprint(3)"
	assertOutput(t, src, "1\n2\n3\n")
}

func TestExecComparison(t *testing.T) {
	src := "x = 10\nif x > 5:\n    print(1)\nif x == 10:\n    print(2)\nif x < 5:\n    print(3)"
	assertOutput(t, src, "1\n2\n")
}

// TestModuleVerifiesUnderLLVM20 asserts every compiled module is accepted by
// llc-20. The compileAndRun helper already aborts on verifier failure, so this
// just runs a representative set through the flow to confirm clean verification.
func TestModuleVerifiesUnderLLVM20(t *testing.T) {
	srcs := []string{
		"print(1)",
		"x = 1\nif x == 1:\n    print(2)",
		"for i in range(3):\n    print(i)",
		"def f(x):\n    return x\nprint(f(1))",
	}
	for _, src := range srcs {
		compileAndRun(t, src) // aborts on llc-20 verifier failure
	}
}

// TestCompileProducesExecutable asserts the emitted IR actually contains a
// callable main entry point (the flow depends on it).
func TestCompileProducesExecutable(t *testing.T) {
	res, err := lang.Compile("print(1)")
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	if !strings.Contains(res.IR, "define i32 @main") {
		t.Fatalf("IR missing @main entry:\n%s", res.IR)
	}
}
