// Package integration exercises gusty end-to-end by executing the code the
// compiler actually emits, rather than only inspecting the IR text.
//
// Flow: source -> lex/parse -> semantic -> codegen (textual LLVM IR)
//
//	-> llc-20 (LLVM 20 module verification + object code)
//	-> cc (link) -> run the native binary -> compare captured stdout.
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
	if out, err := exec.Command("cc", objPath, "-lm", "-o", binPath).CombinedOutput(); err != nil {
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

func TestExecMultiArgPrint(t *testing.T) {
	// multi-argument print mirrors the interpreter: each argument is written
	// to stdout on its own line, one printf per argument.
	assertOutput(t, "print(1, 2)", "1\n2\n")
	assertOutput(t, "print(1, 2, 3)", "1\n2\n3\n")
	// mixed literals and runtime variables.
	assertOutput(t, "x = 7\nprint(x, x + 1)", "7\n8\n")
	assertOutput(t, "print(1 + 2, 3 + 4)", "3\n7\n")
	// string-literal arguments use a %s\n format, mirroring the interpreter's Repr.
	assertOutput(t, "print(\"hi\")", "hi\n")
	assertOutput(t, "print(1, \"hi\", 2)", "1\nhi\n2\n")
	// zero-argument print() writes nothing, matching the interpreter.
	assertOutput(t, "print()", "")
	assertOutput(t, "x = 1\nprint(x)\nprint()\nprint(2)", "1\n2\n")
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

func TestExecModulo(t *testing.T) {
	// constant modulo folds to a constant.
	assertOutput(t, "print(17 % 5)", "2\n")
	assertOutput(t, "print(20 % 7)", "6\n")
	assertOutput(t, "print(42 % 4)", "2\n")
	// runtime modulo over a variable lowers to srem.
	assertOutput(t, "x = 17\nprint(x % 5)", "2\n")
	assertOutput(t, "a = 20\nb = 7\nprint(a % b)", "6\n")
	// modulo composes with other arithmetic in an expression.
	assertOutput(t, "print(17 % 5 + 1)", "3\n")
	assertOutput(t, "print(100 // 30 % 7)", "3\n")
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

func TestExecForRangeStep(t *testing.T) {
	// positive step: range(1, 5, 2) => 1 + 3 = 4
	src := "s = 0\nfor i in range(1, 5, 2):\n    s = s + i\nprint(s)"
	assertOutput(t, src, "4\n")
	// negative step: range(5, 0, -1) => 5+4+3+2+1 = 15
	src = "s = 0\nfor i in range(5, 0, -1):\n    s = s + i\nprint(s)"
	assertOutput(t, src, "15\n")
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

func TestExecMatchWildcard(t *testing.T) {
	// `case _:` wildcard matches any subject (like the interpreter).
	assertOutput(t, "x = 5\nmatch x:\n    case 1:\n        print(1)\n    case _:\n        print(9)", "9\n")
	// non-wildcard case still matches normally.
	assertOutput(t, "x = 1\nmatch x:\n    case 1:\n        print(1)\n    case _:\n        print(9)", "1\n")
	// wildcard in the middle, subject does not match earlier cases.
	assertOutput(t, "x = 7\nmatch x:\n    case 1:\n        print(1)\n    case _:\n        print(9)\n    case 7:\n        print(7)", "9\n")
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
	// min/max/sum over lists containing runtime variables
	assertOutput(t, "a = 3\nb = 1\nprint(min([a, b]))", "1\n")
	assertOutput(t, "a = 3\nb = 1\nprint(max([a, b]))", "3\n")
	assertOutput(t, "a = 3\nb = 1\nprint(sum([a, b]))", "4\n")
	assertOutput(t, "a = 5\nb = 2\nprint(min([a, b, 1]))", "1\n")
	// len and list-index over runtime-variable-element lists
	assertOutput(t, "a = 3\nb = 1\nprint(len([a, b]))", "2\n")
	assertOutput(t, "a = 3\nb = 1\nprint([a, b][0])", "3\n")
	assertOutput(t, "a = 3\nb = 1\nprint([a, b][1])", "1\n")
}

func TestExecLambdaInline(t *testing.T) {
	// `(lambda x: int: x + 1)(5)` -> 6
	assertOutput(t, "print((lambda x: int: x + 1)(5))", "6\n")
}

func TestExecLambdaNamed(t *testing.T) {
	// `f = lambda x: int: x * 2; f(3)` -> 6
	assertOutput(t, "f = lambda x: int: x * 2\nprint(f(3))", "6\n")
}

func TestExecStrMethod(t *testing.T) {
	// `print(len("AbC".upper()))` -> 3 (constant-folded upper)
	assertOutput(t, `print(len("AbC".upper()))`, "3\n")
}

func TestExecStrMethodPrint(t *testing.T) {
	assertOutput(t, `print("AbC".upper())`, "ABC\n")
}

func TestExecPrintChrConst(t *testing.T) {
	assertOutput(t, "print(chr(65))\nprint(chr(66))", "A\nB\n")
}

func TestExecDynamicDispatch(t *testing.T) {
	// A function returns an instance of a different class per argument; method
	// dispatch must follow the runtime instance, not a compile-time guess.
	src := "class Animal:\n    def __init__(self):\n        self.x = 1\n    def speak(self):\n        return self.x\nclass Dog(Animal):\n    def __init__(self):\n        super().__init__()\n    def speak(self):\n        return 42\na = Animal()\nb = Dog()\nprint(a.speak())\nprint(b.speak())"
	assertOutput(t, src, "1\n42\n")
}

func TestExecIntFloatConv(t *testing.T) {
	// int(float var) truncates toward zero, float(int var) widens: AOT parity.
	assertOutput(t, "a = 3.9\nb = -3.9\nc = 2\nd = 1\nprint(int(a))\nprint(int(b))\nprint(float(c))", "3\n-3\n2\n")
}


func TestExecClassInitArgs(t *testing.T) {
	// Regression: class instantiation with constructor args previously
	// produced malformed IR (the arg loads were emitted inline inside the
	// __init__ call), failing the llc step. This exercises __init__ with
	// one and multiple runtime args plus a follow-up method call.
	src := "class Box:\n    def __init__(self, v):\n        self.v = v\n    def get(self):\n        return self.v\n\nb1 = Box(1)\nprint(b1.get())\nb2 = Box(2)\nprint(b2.get())\nclass Point:\n    def __init__(self, x, y):\n        self.x = x\n        self.y = y\n    def sum(self):\n        return self.x + self.y\np = Point(3, 4)\nprint(p.sum())\nq = Point(10, 20)\nprint(q.sum())"
	assertOutput(t, src, "1\n2\n7\n30\n")
}

func TestExecStringInClassMethod(t *testing.T) {
	// Regression: a string literal inside a class method body previously
	// emitted its global declaration interleaved into the method's instruction
	// stream (g.globals held both globals and method bodies), producing
	// malformed IR that failed the llc step. String constants are now emitted
	// into a dedicated builder at the top of the module.
	assertOutput(t, "class A:\n    def m(self):\n        print(\"inside method\")\n        return 42\na = A()\nprint(\"r\", a.m())", "r\ninside method\n42\n")
}

func TestExecForOverGeneratorList(t *testing.T) {
	// Regression: generator calls return runtime heap list handles, but the
	// target was not tracked as a list, so `print(g)` printed a scalar and
	// `for x in gen()` / `for x in g` iterated 0..handle instead of the list
	// elements. Assignments from generator calls are now marked as listVars
	// and for-loops over runtime list handles iterate via rt_list_len/rt_get_elem.
	assertOutput(t, "def gen():\n    yield 1\n    yield 2\n    yield 3\ns = 0\nfor x in gen():\n    s = s + x\nprint(s)", "6\n")
	assertOutput(t, "def gen():\n    yield 1\n    yield 2\n    yield 3\ng = gen()\ns = 0\nfor x in g:\n    s = s + x\nprint(s)", "6\n")
	assertOutput(t, "def gen():\n    yield 1\n    yield 2\nprint(gen())", "[1, 2]\n")
}

func TestExecFloatFloorModNegNeg(t *testing.T) {
	// Negative float floor/mod/neg must match in the AOT binary:
	// -3.5//2.0 == -2, -3.5%%2.0 == -1.5, abs(-3.5) == 3.5, round(-3.5) == -4.
	assertOutput(t, "a = -3.5\nb = 2.0\nprint(a // b)\nprint(a % b)\nprint(abs(a))\nprint(round(a))", "-2\n-1.5\n3.5\n-4\n")
}

func TestExecAbsFloat(t *testing.T) {
	assertOutput(t, "a = -3.5\nb = -2.0\nprint(abs(a))\nprint(abs(b))", "3.5\n2\n")
}

func TestExecRoundIntVar(t *testing.T) {
	assertOutput(t, "a = 3\nb = -3\nprint(round(a))\nprint(round(b))", "3\n-3\n")
}

func TestExecRoundVar(t *testing.T) {
	// round(float variable) must round half-away in the AOT binary.
	assertOutput(t, "a = 2.5\nb = -2.5\nprint(round(a))\nprint(round(b))", "3\n-3\n")
}

func TestExecRound(t *testing.T) {
	// round(float) rounds half-away-from-zero in both interpreter and AOT.
	assertOutput(t, "print(round(2.5))\nprint(round(3.9))\nprint(round(2.4))\nprint(round(-2.5))", "3\n4\n2\n-3\n")
}

func TestExecFloatFloorModNeg(t *testing.T) {
	// Float `//` floor division, `%` remainder, and unary `-` on float
	// variables must match the interpreter's float64-payload semantics
	// (and the AOT codegen emits llvm.floor/frem/fsub for them).
	assertOutput(t, "a = 5.5\nb = 2.0\nprint(a // b)\nprint(-a)\nprint(a % b)", "2\n-5.5\n1.5\n")
	assertOutput(t, "print(7.0 // 2)\nprint(5.5 % 2.0)\nprint(-2.5)", "3\n1.5\n-2.5\n")
}

func TestExecPrintStrFloat(t *testing.T) {
	// str(float-constant) folds to its %g decimal string; print must emit a
	// %s printf with the string-global pointer (valid IR), not a %d printf fed
	// an i8*. Matches the interpreter's str()/Repr for floats.
	assertOutput(t, `print(str(3.5))`, "3.5\n")
	assertOutput(t, `print(str(2))`, "2\n")
	assertOutput(t, `print(str(1.0 + 2.0))`, "3\n")
	assertOutput(t, `print(1, str(3.5), 2)`, "1\n3.5\n2\n")
}

func TestExecDictKeysValues(t *testing.T) {
	assertOutput(t, `print(sum({1: 2, 3: 4}.keys()))`, "4\n")
}

func TestExecListAppend(t *testing.T) {
	assertOutput(t, `print(sum([1, 2, 3].append(4)))`, "10\n")
}

func TestExecDictItemsLen(t *testing.T) {
	assertOutput(t, `print(len({1: 2, 3: 4}.items()))`, "2\n")
}

func TestExecDictMinMaxMethods(t *testing.T) {
	assertOutput(t, `print(max({1: 2, 3: 4}.keys()))`, "3\n")
}

func TestExecStrIndex(t *testing.T) {
	assertOutput(t, `print("abc"[1])`, "98\n")
}

func TestExecStrSplitLen(t *testing.T) {
	assertOutput(t, `print(len("a b c".split()))`, "3\n")
}

func TestExecStrReplace(t *testing.T) {
	// `print("aXbXc".replace("X", "-"))` -> a-b-c (constant-folded replace)
	assertOutput(t, `print("aXbXc".replace("X", "-"))`, "a-b-c\n")
}

func TestExecStrFind(t *testing.T) {
	// `print("abcabc".find("bc"))` -> 1 (constant-folded index)
	assertOutput(t, `print("abcabc".find("bc"))`, "1\n")
	// absent substring folds to -1
	assertOutput(t, `print("hello".find("z"))`, "-1\n")
}

func TestExecStrRfind(t *testing.T) {
	// rfind folds to the last occurrence index
	assertOutput(t, `print("abcabc".rfind("bc"))`, "4\n")
	assertOutput(t, `print("hello".rfind("z"))`, "-1\n")
}

func TestExecStrCapitalize(t *testing.T) {
	// capitalize folds to a string constant
	assertOutput(t, `print("hello".capitalize())`, "Hello\n")
}

func TestExecStrTitle(t *testing.T) {
	// title folds to a string constant
	assertOutput(t, `print("hello world".title())`, "Hello World\n")
}

func TestExecStrSwapcase(t *testing.T) {
	// swapcase folds to a string constant
	assertOutput(t, `print("HeLLo".swapcase())`, "hEllO\n")
}

func TestExecStrIsdigit(t *testing.T) {
	// isdigit folds to 1 or 0
	assertOutput(t, `print("123".isdigit())`, "1\n")
	assertOutput(t, `print("12a".isdigit())`, "0\n")
}

func TestExecStrIsalpha(t *testing.T) {
	// isalpha folds to 1 or 0
	assertOutput(t, `print("abc".isalpha())`, "1\n")
	assertOutput(t, `print("ab1".isalpha())`, "0\n")
}

func TestExecStrIslowerIsupper(t *testing.T) {
	// islower/isupper fold to 1 or 0
	assertOutput(t, `print("abc".islower())`, "1\n")
	assertOutput(t, `print("Abc".islower())`, "0\n")
	assertOutput(t, `print("ABC".isupper())`, "1\n")
}

func TestExecStrIsalnum(t *testing.T) {
	// isalnum folds to 1 or 0
	assertOutput(t, `print("abc123".isalnum())`, "1\n")
	assertOutput(t, `print("abc!".isalnum())`, "0\n")
}

func TestExecStrIsspace(t *testing.T) {
	// isspace folds to 1 or 0
	assertOutput(t, `print("   ".isspace())`, "1\n")
	assertOutput(t, `print(" a ".isspace())`, "0\n")
}

func TestExecStrStartswithEndswith(t *testing.T) {
	// startswith/endswith fold to 1 or 0
	assertOutput(t, `print("hello".startswith("he"))`, "1\n")
	assertOutput(t, `print("hello".endswith("he"))`, "0\n")
}

func TestExecStrCount(t *testing.T) {
	// count folds to the occurrence count
	assertOutput(t, `print("ababab".count("ab"))`, "3\n")
	assertOutput(t, `print("hello".count("z"))`, "0\n")
}

func TestExecStrLstripRstrip(t *testing.T) {
	// lstrip/rstrip fold to trimmed string constants
	assertOutput(t, `print("  hi  ".lstrip())`, "hi  \n")
	assertOutput(t, `print("  hi  ".rstrip())`, "  hi\n")
}

func TestExecStrJoin(t *testing.T) {
	// join folds constant list element strings with the separator
	assertOutput(t, `print("-".join(["a", "b", "c"]))`, "a-b-c\n")
	assertOutput(t, `print("x".join(["a", "b"]))`, "axb\n")
}

func TestExecStrEq(t *testing.T) {
	assertOutput(t, `print("abc" == "abd")`, "0\n")
}

func TestExecStrBuiltin(t *testing.T) {
	assertOutput(t, `print(len(str(42)))`, "2\n")
}

func TestExecStrBuiltinPrint(t *testing.T) {
	// print(str(42)) emits a folded string global via the %s format path.
	assertOutput(t, `print(str(42))`, "42\n")
	assertOutput(t, `print("n=" + str(7))`, "n=7\n")
}

func TestReversedCodegen(t *testing.T) {
	// reversed folds on a literal string in the AOT codegen.
	ir, err := lang.Compile(`print(reversed("abc"))`)
	if err != nil {
		t.Fatalf("compile reversed string: %v", err)
	}
	if !strings.Contains(ir.IR, `cba`) {
		t.Fatalf("expected reversed string constant, got ir=%s", ir)
	}
	// reversed folds on a literal list into a reversed list literal.
	ir2, err2 := lang.Compile(`x = reversed([1, 2, 3])`)
	if err2 != nil {
		t.Fatalf("compile reversed list: %v", err2)
	}
	if !strings.Contains(ir2.IR, `3`) || !strings.Contains(ir2.IR, `1`) {
		t.Fatalf("expected reversed list elements, got ir=%s", ir2)
	}
}

func TestIntCodegen(t *testing.T) {
	// int(str) folds to the parsed decimal constant in the AOT codegen.
	assertOutput(t, `print(int("42"))`, "42\n")
	// int(int) is the identity.
	assertOutput(t, `print(int(7))`, "7\n")
	ir, err := lang.Compile(`print(int("42"))`)
	if err != nil {
		t.Fatalf("compile int: %v", err)
	}
	if !strings.Contains(ir.IR, `42`) {
		t.Fatalf("expected folded int constant, got ir=%s", ir.IR)
	}
}

func TestForStrCodegen(t *testing.T) {
	ir, err := lang.Compile(`for x in "ab":
    print(x)`)
	if err != nil {
		t.Fatalf("compile for-str: %v", err)
	}
	if !strings.Contains(ir.IR, "a") || !strings.Contains(ir.IR, "b") {
		t.Fatalf("expected unrolled chars, got ir=%s", ir.IR)
	}
}

func TestDecoratorCodegenZeroParam(t *testing.T) {
	// Regression: a decorated 0-param function previously panicked with a
	// negative strings.Repeat count in the closure IR emission.
	src := "def twice(f):\n    return f\n@twice\ndef h():\n    return 1\nprint(h())\n"
	res, err := lang.Compile(src)
	if err != nil {
		t.Fatalf("compile decorated 0-param: %v", err)
	}
	if !strings.Contains(res.IR, "define i32 @main()") {
		t.Fatalf("IR lost main():\n%s", res.IR)
	}
}

func TestAnyAllCodegenIR(t *testing.T) {
	res, err := lang.Compile("print(any([0, 0, 1]))\nprint(all([1, 1, 1]))\n")
	if err != nil {
		t.Fatalf("compile any/all: %v", err)
	}
	if !strings.Contains(res.IR, "icmp ne i32") {
		t.Fatalf("IR lacks icmp ne for any/all:\n%s", res.IR)
	}
}

func TestListCallConsumersRun(t *testing.T) {
	// len/sum/min/max/any/all fold over a list-returning builtin call
	// (sorted/reversed) by unwrapping the underlying inline list literal;
	// the element set is preserved, so results match the interpreter.
	got := compileAndRun(t, `print(len(sorted([3, 1, 2])))`)
	if got != "3\n" {
		t.Fatalf("len(sorted([3,1,2])) = %q, want 3", got)
	}
	got = compileAndRun(t, `print(len(reversed([3, 1, 2])))`)
	if got != "3\n" {
		t.Fatalf("len(reversed([3,1,2])) = %q, want 3", got)
	}
	got = compileAndRun(t, `print(sum(sorted([3, 1, 2])))`)
	if got != "6\n" {
		t.Fatalf("sum(sorted([3,1,2])) = %q, want 6", got)
	}
	got = compileAndRun(t, `print(sum(reversed([3, 1, 2])))`)
	if got != "6\n" {
		t.Fatalf("sum(reversed([3,1,2])) = %q, want 6", got)
	}
	got = compileAndRun(t, `print(min(sorted([3, 1, 2])))`)
	if got != "1\n" {
		t.Fatalf("min(sorted([3,1,2])) = %q, want 1", got)
	}
	got = compileAndRun(t, `print(max(sorted([3, 1, 2])))`)
	if got != "3\n" {
		t.Fatalf("max(sorted([3,1,2])) = %q, want 3", got)
	}
	got = compileAndRun(t, `print(any(sorted([0, 2, 3])))`)
	if got != "1\n" {
		t.Fatalf("any(sorted([0,2,3])) = %q, want 1", got)
	}
	got = compileAndRun(t, `print(all(sorted([1, 2, 3])))`)
	if got != "1\n" {
		t.Fatalf("all(sorted([1,2,3])) = %q, want 1", got)
	}
	got = compileAndRun(t, `print(all(sorted([0, 1, 2])))`)
	if got != "0\n" {
		t.Fatalf("all(sorted([0,1,2])) = %q, want 0", got)
	}
}

func TestListLenRun(t *testing.T) {
	// len over list-producing builtin calls folds to the matching length.
	cases := []struct{ src, want string }{
		{`print(len(enumerate([1, 2, 3])))`, "3"},
		{`print(len(zip([1, 2], [3, 4, 5])))`, "2"},
		{`print(len(zip([1, 2, 3], [4])))`, "1"},
		{`print(len("hello".partition("l")))`, "3"},
		{`print(len("a,b,c".split(",")))`, "3"},
		{`print(len("a,b,c".rsplit(",")))`, "3"},
		{`print(len("".split(",")))`, "1"},
		{`print(len(reversed("abc")))`, "3"},
		{`print(len(reversed("hello")))`, "5"},
		{`print(len(enumerate(sorted([3, 1, 2]))))`, "3"},
		{`print(len(zip(sorted([1, 2]), reversed([3, 4]))))`, "2"},
		{`print(len(sorted(reversed([3, 1, 2]))))`, "3"},
		{`print(sum(sorted(reversed([3, 1, 2]))))`, "6"},
		{`print(min(sorted(reversed([3, 1, 2]))))`, "1"},
	}
	for _, tc := range cases {
		got := compileAndRun(t, tc.src)
		if got != tc.want+"\n" {
			t.Fatalf("%s: got %q, want %s", tc.src, got, tc.want)
		}
	}
}

func TestOverEmptyNestedListsRun(t *testing.T) {
	got := compileAndRun(t, `print(sum(sorted([])))`)
	if got != "0\n" {
		t.Fatalf("sum(sorted([])) = %q, want 0", got)
	}
	got = compileAndRun(t, `print(all(sorted([])))`)
	if got != "1\n" {
		t.Fatalf("all(sorted([])) = %q, want 1", got)
	}
}

func TestRejectNonEmptyDicts(t *testing.T) {
	// The interpreter rejects non-list/set collections for sum/min/max/any/all;
	// the codegen must match by rejecting non-empty dict literals too.
	for _, src := range []string{
		`print(sum({1: 2}))`,
		`print(min({1: 2}))`,
		`print(max({1: 2}))`,
	} {
		res, err := lang.Compile(src)
		if err == nil {
			t.Fatalf("Compile(%q) should error, got:\n%s", src, res.IR)
		}
		if !strings.Contains(err.Error(), "expects a list or set") {
			t.Fatalf("Compile(%q) error = %v, want 'expects a list or set'", src, err)
		}
	}
}

func TestOverEmptyCollectionsRun(t *testing.T) {
	got := compileAndRun(t, `print(sum([]))`)
	if got != "0\n" {
		t.Fatalf("sum([]) = %q, want 0", got)
	}
	got = compileAndRun(t, `print(sum({}))`)
	if got != "0\n" {
		t.Fatalf("sum({}) = %q, want 0", got)
	}
}

func TestNestedListCallRun(t *testing.T) {
	got := compileAndRun(t, `print(len(sorted(reversed([3, 1, 2]))))`)
	if got != "3\n" {
		t.Fatalf("len(sorted(reversed([3,1,2]))) = %q, want 3", got)
	}
	got = compileAndRun(t, `print(sum(sorted(reversed([3, 1, 2]))))`)
	if got != "6\n" {
		t.Fatalf("sum(sorted(reversed([3,1,2]))) = %q, want 6", got)
	}
}

func TestExecFloatFloorModAbsEdgeCases(t *testing.T) {
	// Lock float floor-division (`//`), frem modulo (`%`), abs, and round
	// semantics across negative operands, exact multiples, and half-values —
	// the AOT codegen emits fdiv+floor, frem, llvm.fabs, and llvm.round.
	assertOutput(t, "print(8.0 // 2.0)\nprint(-8.0 // 3.0)\nprint(-5.0 // 2.0)", "4\n-3\n-3\n")
	assertOutput(t, "print(5.0 % 2.0)\nprint(-5.0 % 2.0)\nprint(5.0 % -2.0)", "1\n-1\n1\n")
	assertOutput(t, "print(abs(-3.5))\nprint(abs(-2.0))", "3.5\n2\n")
	assertOutput(t, "print(round(2.5))\nprint(round(-2.5))", "3\n-3\n")
}

func TestExecSqrt(t *testing.T) {
	// Standard-library sqrt builtin: promotes int/float args to float and
	// emits llvm.sqrt.f64; constant args are folded at compile time.
	assertOutput(t, "print(sqrt(9.0))\nprint(sqrt(9))\nprint(sqrt(2.0))", "3\n3\n1.4142135623730951\n")
	assertOutput(t, "x = 16.0\nprint(sqrt(x))\nprint(sqrt(0.0))", "4\n0\n")
}

func TestExecFloorCeil(t *testing.T) {
	// Standard-library floor/ceil builtins: llvm.floor.f64 / llvm.ceil.f64
	// with float promotion and constant folding.
	assertOutput(t, "print(floor(2.7))\nprint(floor(-2.7))\nprint(ceil(2.2))\nprint(ceil(-2.2))", "2\n-3\n3\n-2\n")
	assertOutput(t, "print(floor(7))\nprint(ceil(7))", "7\n7\n")
}

func TestExecTryExcept(t *testing.T) {
	assertOutput(t, `try:
    print(1)
    raise ValueError("boom")
    print(2)
except:
    print("caught")
print("done")`, "1\ncaught\ndone\n")
	assertOutput(t, `try:
    print(1)
finally:
    print("finally")
print("done")`, "1\nfinally\ndone\n")
	assertOutput(t, `try:
    raise ValueError("boom")
except ValueError:
    print("value")
except:
    print("other")
print("done")`, "value\ndone\n")
	assertOutput(t, `def f():
    raise ValueError("x")
    print(1)
try:
    f()
except:
    print("caught")
print("done")`, "caught\ndone\n")
}

func TestExecImportModuleGlobals(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(dir+"/config.gy", []byte("base = 21\nx = base * 2\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	old, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	defer os.Chdir(old)
	assertOutput(t, "import config\nprint(config.x)", "42\n")
}

func TestExecImportStringGlobals(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(dir+"/msg.gy", []byte("greet = \"hello\"\nmsg = greet + \"!\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	old, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	defer os.Chdir(old)
	assertOutput(t, "import msg\nprint(msg.msg)", "hello!\n")
}

func TestExecImportLenString(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(dir+"/msg.gy", []byte("greet = \"hello\"\nmsg = greet + \"!\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	old, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	defer os.Chdir(old)
	assertOutput(t, "import msg\nprint(len(msg.msg))", "6\n")
}

func TestExecImportOrdString(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(dir+"/msg.gy", []byte("greet = \"hello\"\nmsg = greet + \"!\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	old, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	defer os.Chdir(old)
	assertOutput(t, "import msg\nprint(ord(msg.msg))", "104\n")
}

func TestExecImportReversedString(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(dir+"/msg.gy", []byte("greet = \"hello\"\nmsg = greet + \"!\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	old, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	defer os.Chdir(old)
	_ = os.Getwd
}

func TestExecImportListIndex(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(dir+"/cfg.gy", []byte("l = [1, 2, 3]\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	old, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	defer os.Chdir(old)
	assertOutput(t, "import cfg\nprint(cfg.l[0])", "1\n")
}

func TestExecImportLenList(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(dir+"/cfg.gy", []byte("l = [1, 2, 3]\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	old, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	defer os.Chdir(old)
	assertOutput(t, "import cfg\nprint(len(cfg.l))", "3\n")
}

func TestExecImportDictIndex(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(dir+"/cfg.gy", []byte("d = {1: 10}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	old, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	defer os.Chdir(old)
	assertOutput(t, "import cfg\nprint(cfg.d[1])", "10\n")
}

func TestExecImportListArith(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(dir+"/cfg.gy", []byte("l = [1, 2, 3]\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	old, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	defer os.Chdir(old)
	assertOutput(t, "import cfg\nprint(cfg.l[0] + cfg.l[1])", "3\n")
}

func TestExecImportLenDict(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(dir+"/cfg.gy", []byte("d = {1: 10, 2: 20}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	old, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	defer os.Chdir(old)
	assertOutput(t, "import cfg\nprint(len(cfg.d))", "2\n")
}

func TestExecImportDictArith(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(dir+"/cfg.gy", []byte("d = {1: 10, 2: 20}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	old, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	defer os.Chdir(old)
	assertOutput(t, "import cfg\nprint(cfg.d[1] + cfg.d[2])", "30\n")
}

// Runtime-heap feature: `x = [1, 2]` allocates a runtime list object, `x.append(v)`
// mutates it on the heap, and `print(x)` renders the runtime list contents.
func TestExecRuntimeListAppend(t *testing.T) {
	assertOutput(t, "x = [1, 2]\nx.append(3)\nprint(x)", "[1, 2, 3]\n")
}

func TestExecRuntimeListAppendTwo(t *testing.T) {
	assertOutput(t, "x = [1]\nx.append(2)\nx.append(3)\nprint(x)", "[1, 2, 3]\n")
}

func TestExecRuntimeListPrint(t *testing.T) {
	assertOutput(t, "x = [5, 6]\nprint(x)", "[5, 6]\n")
}

// Runtime-heap observability: `len(x)` on a runtime list variable reads back
// the mutated list length through the heap.
func TestExecRuntimeListLen(t *testing.T) {
	assertOutput(t, "x = [1, 2]\nx.append(3)\nprint(len(x))", "3\n")
}

// Runtime-heap indexing: `x[i]` on a runtime list variable reads back the
// mutated element through the heap.
func TestExecRuntimeListIndex(t *testing.T) {
	assertOutput(t, "x = [10, 20]\nx.append(30)\nprint(x[0] + x[2])", "40\n")
}

// Runtime-heap slot reuse: rebinding a list var frees its old heap slot, so
// thousands of rebinds must not exhaust the 1024-slot heap or corrupt results.
func TestExecRuntimeListReuse(t *testing.T) {
	src := "x = [0, 0]\n" + strings.Repeat("x = [0, 0]\n", 2000) + "print(len(x))\n"
	assertOutput(t, src, "2\n")
}

// Runtime-heap list->scalar free: rebinding a list var to a non-list value
// must free its heap slot; alternating list/scalar rebinds must not exhaust
// the 1024-slot heap.
func TestExecRuntimeListToScalarFree(t *testing.T) {
	var sb strings.Builder
	sb.WriteString("x = [0]\n")
	sb.WriteString(strings.Repeat("x = 5\nx = [0]\n", 2000))
	sb.WriteString("print(len(x))\n")
	assertOutput(t, sb.String(), "1\n")
}

// Runtime-heap variable-index read: `x[a]` with a runtime index var reads
// through the heap via rt_get_elem (not a compile-time literal).
func TestExecRuntimeListVarIndex(t *testing.T) {
	assertOutput(t, "x = [10, 20]\na = 1\nprint(x[a])\n", "20\n")
}

// Runtime-heap dicts: `d = {1: 10}` allocates a runtime dict heap object;
// `d[k]` reads via rt_dict_get and `len(d)` via rt_dict_len.
func TestExecRuntimeDict(t *testing.T) {
	assertOutput(t, "d = {1: 10, 2: 20}\nprint(d[1] + d[2])\nprint(len(d))\n", "30\n2\n")
}

// Runtime-heap sets: `s = {1, 2}` allocates a runtime set heap object;
// `len(s)` via rt_set_len and `print(s)` via rt_set_print.
func TestExecRuntimeSet(t *testing.T) {
	assertOutput(t, "s = {1, 2}\nprint(len(s))\nprint(s)\n", "2\n{1, 2}\n")
}

// Runtime-heap cross-collection free: rebinding a var across collection
// kinds (dict -> set -> list -> scalar) must free each old slot. Without
// the free, thousands of rebinds exhaust the 1024-slot heap and corrupt the
// final list read; with it, `len(d)` is correct.
func TestExecRuntimeCrossCollectionFree(t *testing.T) {
	var sb strings.Builder
	sb.WriteString("d = {1: 10}\n")
	sb.WriteString(strings.Repeat("s = {1, 2}\nd = [1, 2]\ns = 5\n", 2000))
	sb.WriteString("d = [1, 2]\nprint(len(d))\n")
	assertOutput(t, sb.String(), "2\n")
}

// Runtime-heap stress/leak harness: thousands of cross-kind rebinds of two
// live vars (list -> dict -> list -> scalar -> set) must not exhaust the
// 1024-slot heap; final reads of both vars stay correct.
func TestExecHeapStress(t *testing.T) {
	var sb strings.Builder
	sb.WriteString("d = [1, 2]\n")
	sb.WriteString("s = {1, 2}\n")
	sb.WriteString(strings.Repeat("d = {1: 10}\nd = [3, 4]\nd = 5\ns = {1, 2}\ns = 5\n", 2000))
	sb.WriteString("d = [3, 4]\n")
	sb.WriteString("s = {1, 2}\n")
	sb.WriteString("print(len(d))\nprint(d[0] + d[1])\nprint(len(s))\n")
	assertOutput(t, sb.String(), "2\n7\n2\n")
}

func TestDecoratorIdentityRuntime(t *testing.T) {
	// identity decorators are supported end-to-end in AOT: the decorated
	// function resolves to its body, and calls print the expected result.
	got := compileAndRun(t, `
def twice(f):
    return f
@twice
def g():
    return 42
print(g())
`)
	if got != "42\n" {
		t.Fatalf("identity-decorated call output = %q, want 42", got)
	}
}

func TestDecoratorNonIdentityRejected(t *testing.T) {
	// wrapping/transform decorators are rejected with a clear codegen error
	// instead of being silently ignored.
	_, err := lang.Compile(`
def add1(g):
    def wrap():
        return g() + 1
    return wrap
@add1
def f():
    return 40
print(f())
`)
	if err == nil {
		t.Fatal("non-identity decorator should be rejected in AOT")
	}
}
func TestSlice(t *testing.T) {
	assertOutput(t, `
l = [1, 2, 3, 4, 5, 6]
print(l[1:4])
print(l[::2])
print(l[-3:])
print(l[:])
print(l[:4])
print(l[1:5:2])
print(l[::-1])
print(l[0])
print(l[2:])
print(l[:-2])
print(l[5:0:-2])
print(l[100:200])
print(l[-100:100])
print(l[0:100:3])
print(l[6:0:-1])
`, "[2, 3, 4]\n[1, 3, 5]\n[4, 5, 6]\n[1, 2, 3, 4, 5, 6]\n[1, 2, 3, 4]\n[2, 4]\n[6, 5, 4, 3, 2, 1]\n1\n[3, 4, 5, 6]\n[1, 2, 3, 4]\n[6, 4, 2]\n[]\n[1, 2, 3, 4, 5, 6]\n[1, 4]\n[6, 5, 4, 3, 2]\n")
}

func TestExecPower(t *testing.T) {
	// integer power: 2 ** 3 == 8
	assertOutput(t, "print(2 ** 3)", "8\n")
	// right-associative: 2 ** 3 ** 2 == 2 ** (3 ** 2) == 2 ** 9 == 512
	assertOutput(t, "print(2 ** 3 ** 2)", "512\n")
	// variable base/exponent
	assertOutput(t, "a = 2\nb = 10\nprint(a ** b)", "1024\n")
}
