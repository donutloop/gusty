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

func TestExecElifChain(t *testing.T) {
	assertOutput(t, "x = 3\nif x < 2:\n    print(10)\nelif x < 4:\n    print(20)\nelse:\n    print(30)", "20\n")
	assertOutput(t, "x = 9\nif x < 2:\n    print(10)\nelif x < 4:\n    print(20)\nelse:\n    print(30)", "30\n")
	assertOutput(t, "x = 6\nif x < 2:\n    print(10)\nelif x < 5:\n    print(20)\nelif x < 8:\n    print(30)\nelse:\n    print(40)", "30\n")
	// no elif/else taken
	assertOutput(t, "x = 9\nif x < 2:\n    print(10)\nelif x < 5:\n    print(20)\nprint(99)", "99\n")
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

func TestExecPassStatement(t *testing.T) {
	// pass in a loop body is a no-op; the loop still sums 0..2.
	assertOutput(t, "s = 0\nfor i in range(3):\n    pass\n    s = s + i\nprint(s)", "3\n")
	// pass in an if body; else branch still executes.
	assertOutput(t, "x = 0\nif 0:\n    pass\nelse:\n    x = 1\nprint(x)", "1\n")
	// bare top-level pass.
	assertOutput(t, "pass\nprint(42)", "42\n")
}

func TestExecMultiplePrints(t *testing.T) {
	src := "print(1)\nprint(2)\nprint(3)"
	assertOutput(t, src, "1\n2\n3\n")
}

func TestExecAndOrBool(t *testing.T) {
	// and/or evaluate both operands and return a boolean 0/1.
	assertOutput(t, "print(1 and 0)", "0\n")
	assertOutput(t, "print(1 and 2)", "1\n")
	assertOutput(t, "print(1 or 0)", "1\n")
	assertOutput(t, "print(0 or 0)", "0\n")
	assertOutput(t, "print(0 or 7)", "1\n")
	// and/or on runtime values.
	assertOutput(t, "x = 1\ny = 0\nprint(x and y)\nprint(x or y)", "0\n1\n")
	// floor division lowers to sdiv (mirrors the interpreter).
	assertOutput(t, "print(9 // 2)", "4\n")
	assertOutput(t, "print(20 // 5)", "4\n")
	assertOutput(t, "print(9 // 2 + 1)", "5\n")
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

func TestExecDefaultArg(t *testing.T) {
	assertOutput(t, "def f(a, b=10):\n    return a + b\nprint(f(5))", "15\n")
}

func TestExecKeywordArg(t *testing.T) {
	assertOutput(t, "def f(a, b):\n    return a * b\nprint(f(a=3, b=4))", "12\n")
}

func TestExecKeywordOutOfOrder(t *testing.T) {
	assertOutput(t, "def f(a, b):\n    return a - b\nprint(f(b=3, a=10))", "7\n")
}

func TestExecKeywordAndDefault(t *testing.T) {
	assertOutput(t, "def f(a, b=5):\n    return a + b\nprint(f(b=100, a=2))", "102\n")
}

func TestExecClosureCapturesParam(t *testing.T) {
	assertOutput(t,
		"def make(x):\n    def inc():\n        return x + 1\n    return inc()\nprint(make(5))",
		"6\n")
}

func TestExecClosureCapturesLocal(t *testing.T) {
	assertOutput(t,
		"def make():\n    n = 10\n    def get():\n        return n\n    return get()\nprint(make())",
		"10\n")
}

func TestExecClosureOwnParamsAndCapture(t *testing.T) {
	assertOutput(t,
		"def make(x):\n    def add(y):\n        return x + y\n    return add(7)\nprint(make(5))",
		"12\n")
}

func TestExecClosureCalledTwice(t *testing.T) {
	assertOutput(t,
		"def make(x):\n    def inc():\n        return x + 1\n    a = inc()\n    b = inc()\n    return a + b\nprint(make(5))",
		"12\n")
}

func TestExecTwoClosures(t *testing.T) {
	assertOutput(t,
		"def make(x):\n    def add1():\n        return x + 1\n    def add2():\n        return x + 2\n    return add1() + add2()\nprint(make(5))",
		"13\n")
}


func TestExecForListLiteral(t *testing.T) {
	// for-over-list: sum the elements of an inline list literal.
	assertOutput(t, "s = 0\nfor x in [1, 2, 3]:\n    s = s + x\nprint(s)", "6\n")
	assertOutput(t, "s = 0\nfor x in [2, 4, 6]:\n    s = s + x\nprint(s)", "12\n")
	// empty list literal iterates zero times.
	assertOutput(t, "s = 0\nfor x in []:\n    s = s + x\nprint(s)", "0\n")
	// continue skips to the next element.
	assertOutput(t, "s = 0\nfor x in [1, 2, 3, 4]:\n    if x == 2:\n        continue\n    s = s + x\nprint(s)", "8\n")
	// break stops and skips the else.
	assertOutput(t, "s = 0\nfor x in [1, 2, 3]:\n    if x == 2:\n        break\n    s = s + x\nelse:\n    s = s + 100\nprint(s)", "1\n")
	// normal completion runs the else.
	assertOutput(t, "s = 0\nfor x in [1, 2, 3]:\n    s = s + x\nelse:\n    s = s + 100\nprint(s)", "106\n")
}

func TestExecMultiArgRangeComprehensionAOT(t *testing.T) {
	assertOutput(t, "print(sum([x for x in range(0, 5)]))\n", "10\n")
	assertOutput(t, "print(sum([x for x in range(1, 5, 2)]))\n", "4\n")
	assertOutput(t, "print(max([x for x in range(2, 10, 3)]))\n", "8\n")
	assertOutput(t, "print(min([x for x in range(5, 0, -1)]))\n", "1\n")
}

func TestExecAggregateOverComprehensionAOT(t *testing.T) {
	assertOutput(t, "print(len([x for x in range(5)]))\n", "5\n")
	assertOutput(t, "print(sum([x for x in range(5)]))\n", "10\n")
	assertOutput(t, "print(min([y * y for y in range(3)]))\n", "0\n")
	assertOutput(t, "print(max([x * 2 for x in [1, 2, 3]]))\n", "6\n")
	assertOutput(t, "print(sum([x * x for x in range(4)]))\n", "14\n")
}

func TestExecTernaryAOT(t *testing.T) {
	// constant-condition ternary folds to the taken branch.
	assertOutput(t, "print(5 if 1 else 3)\n", "5\n")
	assertOutput(t, "print(10 if 0 else 42)\n", "42\n")
	// runtime-comparison ternary lowers to a select.
	assertOutput(t, "print(7 if 2 > 1 else 99)\n", "7\n")
	// right-associative nested ternary.
	assertOutput(t, "print(1 if 0 else 2 if 1 else 3)\n", "2\n")
	// used inside a larger expression.
	assertOutput(t, "print((5 if 1 else 6) + 1)\n", "6\n")
}

func TestExecListComprehensionAOT(t *testing.T) {
	// inline list comprehension over a constant list literal, folded at
	// compile time and indexed directly.
	assertOutput(t, "print([x * 2 for x in [1, 2, 3]][1])", "4\n")
	// comprehension over range(n): 0..3 doubled => 0, 2, 4, 6.
	assertOutput(t, "print([x * 2 for x in range(4)][3])", "6\n")
	// comprehension with a condition filters at compile time.
	// y in range(4) if y > 1 => 2, 3 => squares 4, 9.
	assertOutput(t, "print([y * y for y in range(4) if y > 1][0])", "4\n")
	assertOutput(t, "print([y * y for y in range(4) if y > 1][1])", "9\n")
	// multiple fold/index uses combined in one expression.
	assertOutput(t, "print([x * 2 for x in [1, 2, 3]][0] + [x * 2 for x in [1, 2, 3]][2])", "8\n")
}

func TestExecDictSetLiterals(t *testing.T) {
	// dict literal constant-key lookup folds at compile time.
	assertOutput(t, "print({1: 10, 2: 20}[1])", "10\n")
	assertOutput(t, "print({1: 10, 2: 20}[2])", "20\n")
	// len over a dict literal.
	assertOutput(t, "print(len({1: 10, 2: 20}))", "2\n")
	// set literal constant membership lookup.
	assertOutput(t, "print({1, 2, 3}[2])", "2\n")
	assertOutput(t, "print({1, 2, 3}[3])", "3\n")
	// len over a set literal.
	assertOutput(t, "print(len({1, 2, 3}))", "3\n")
	// combined expressions.
	assertOutput(t, "print({5: 50, 6: 60}[6] + len({7, 8}))", "62\n")
}

func TestExecStringConstLen(t *testing.T) {
	// len of a string literal folds to its character count.
	assertOutput(t, "print(len(\"hello\"))", "5\n")
	assertOutput(t, "print(len(\"\"))", "0\n")
	// constant string concatenation folds to the joined string, then len.
	assertOutput(t, "print(len(\"ab\" + \"cd\"))", "4\n")
	assertOutput(t, "print(len(\"a\" + \"b\" + \"c\"))", "3\n")
	// len still works on inline list/set/dict literals.
	assertOutput(t, "print(len({1, 2, 3}))", "3\n")
}

func TestListIndexAndLen(t *testing.T) {
	src := "print([1, 2, 3][1])\nprint(len([1, 2, 3]))"
	assertOutput(t, src, "2\n3\n")
}

func TestListIndexLargeAndPrint(t *testing.T) {
	src := "print([10, 20, 30, 40][3])"
	assertOutput(t, src, "40\n")
}

func TestExecSumMinMaxAbs(t *testing.T) {
	assertOutput(t, "print(sum([1, 2, 3]))", "6\n")
	assertOutput(t, "print(sum([1, 2, 3, 4, 5]))", "15\n")
	assertOutput(t, "print(min([3, 1, 2]))", "1\n")
	assertOutput(t, "print(max([3, 1, 2]))", "3\n")
	assertOutput(t, "print(min([9, 4, 7, 1, 5]))", "1\n")
	assertOutput(t, "print(max([9, 4, 7, 1, 5]))", "9\n")
	assertOutput(t, "print(abs(-5))", "5\n")
	assertOutput(t, "print(abs(5))", "5\n")
	// abs over a runtime value (function arg / negated name).
	assertOutput(t, "x = 3\nprint(abs(-x))", "3\n")
}
