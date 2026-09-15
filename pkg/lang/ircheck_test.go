package lang

import (
	"os/exec"
	"strings"
	"testing"
)

func llcCompiles(t *testing.T, src string) string {
	t.Helper()
	res, err := Compile(src)
	if err != nil {
		t.Fatalf("compile %q: %v", src, err)
	}
	// -relocation-model=pic: llc otherwise defaults to the static relocation
	// model, emitting R_X86_64_32 relocations for .rodata string constants that
	// the default PIE link (cc) rejects. PIC codegen uses RIP-relative refs.
	cmd := exec.Command("llc-20", "-relocation-model=pic", "-o", "/tmp/ircheck.o")
	cmd.Stdin = strings.NewReader(res.IR)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("llc failed for %q: %v\n%s\nIR:\n%s", src, err, out, res.IR)
	}
	return res.IR
}

func TestIRCompilesWithLLC(t *testing.T) {
	llcCompiles(t, "x = 3\nprint(x + 4 * 2)")
}

func TestIRWhileCompilesWithLLC(t *testing.T) {
	llcCompiles(t, "i = 0\nwhile i < 3:\n    i = i + 1\nprint(i)")
}

func TestIRForCompilesWithLLC(t *testing.T) {
	llcCompiles(t, "for i in range(5):\n    print(i)")
}

func TestIRFuncCompilesWithLLC(t *testing.T) {
	llcCompiles(t, "def double(x):\n    return x * 2\nprint(double(5))")
}

func TestIRFuncTwoParamsCompilesWithLLC(t *testing.T) {
	llcCompiles(t, "def add(a, b):\n    return a + b\nprint(add(3, 4))")
}

func TestIRIfCompilesWithLLC(t *testing.T) {
	llcCompiles(t, "x = 1\nif x < 2:\n    print(10)\nelse:\n    print(20)")
}

func TestIRMatchCompilesWithLLC(t *testing.T) {
	llcCompiles(t, "x = 2\nmatch x:\n    case 1:\n        print(1)\n    case 2:\n        print(2)\nx")
}

func TestIRBreakContinueCompilesWithLLC(t *testing.T) {
	llcCompiles(t, "i = 0\nwhile i < 100:\n    i = i + 1\n    if i == 3:\n        break\ni")
	llcCompiles(t, "s = 0\nfor i in range(5):\n    if i == 2:\n        continue\n    s = s + i\ns")
}

func TestIRRangeTwoArgCompilesWithLLC(t *testing.T) {
	llcCompiles(t, "s = 0\nfor i in range(2, 5):\n    s = s + i\nprint(s)")
}

func TestIRForElseCompilesWithLLC(t *testing.T) {
	llcCompiles(t, "s = 0\nfor i in range(3):\n    s = s + i\nelse:\n    s = s + 100\nprint(s)")
}

func TestIRWhileElseCompilesWithLLC(t *testing.T) {
	llcCompiles(t, "i = 0\ns = 0\nwhile i < 3:\n    s = s + i\n    i = i + 1\nelse:\n    s = s + 10\nprint(s)")
}

func TestIRForElseBreakCompilesWithLLC(t *testing.T) {
	llcCompiles(t, "s = 0\nfor i in range(3):\n    if i == 1:\n        break\n    s = s + i\nelse:\n    s = s + 100\nprint(s)")
}

func TestIRDefaultArgCompilesWithLLC(t *testing.T) {
	ir := llcCompiles(t, "def f(a, b=10):\n    return a + b\nprint(f(5))")
	// codegen must fill the default b=10 as the second call argument
	if !strings.Contains(ir, "call i32 @f(i32 5, i32 10)") {
		t.Fatalf("missing default-arg call in IR:\n%s", ir)
	}
}

func TestIRKeywordArgCompilesWithLLC(t *testing.T) {
	ir := llcCompiles(t, "def f(a, b):\n    return a * b\nprint(f(a=3, b=4))")
	// keyword args must be emitted in parameter order a,b => 3,4
	if !strings.Contains(ir, "call i32 @f(i32 3, i32 4)") {
		t.Fatalf("missing keyword-arg call in IR:\n%s", ir)
	}
}

func TestIRKeywordOutOfOrderCompilesWithLLC(t *testing.T) {
	ir := llcCompiles(t, "def f(a, b):\n    return a - b\nprint(f(b=3, a=10))")
	// out-of-order keyword args must be reordered to (a=10, b=3)
	if !strings.Contains(ir, "call i32 @f(i32 10, i32 3)") {
		t.Fatalf("missing reordered keyword call in IR:\n%s", ir)
	}
}

func TestIRKeywordRejectedInBuiltin(t *testing.T) {
	res, err := Compile("print(x=1)")
	if err == nil {
		t.Fatal("expected error for keyword arg to print")
	}
	_ = res
}
func TestIRClosureCompilesWithLLC(t *testing.T) {
	ir := llcCompiles(t, "def outer(x):\n    def inc():\n        return x + 1\n    y = inc()\n    return y\nprint(outer(5))")
	if !strings.Contains(ir, "@inc_env") {
		t.Fatalf("missing closure define in IR:\n%s", ir)
	}
	if !strings.Contains(ir, "@inc_slot") {
		t.Fatalf("missing closure env slot in IR:\n%s", ir)
	}
}

func TestIRDecoratorCompilesWithLLC(t *testing.T) {
	ir := llcCompiles(t, "def dec(g):\n    return g\n@dec\ndef f(x):\n    return x + 1\nprint(f(3))")
	if !strings.Contains(ir, "@f_impl") {
		t.Fatalf("missing decorated impl in IR:\n%s", ir)
	}
	if !strings.Contains(ir, "@f_apply") {
		t.Fatalf("missing decorator apply in IR:\n%s", ir)
	}
}

func TestIRAndOrCompilesWithLLC(t *testing.T) {
	llcCompiles(t, "print(1 and 0)\nprint(1 or 0)")
	llcCompiles(t, "x = 1\ny = 0\nprint(x and y)\nprint(x or y)")
	llcCompiles(t, "print(9 // 2)")
}

func TestIRDictSetCompilesWithLLC(t *testing.T) {
	llcCompiles(t, "print({1: 10, 2: 20}[1])\nprint(len({1: 10, 2: 20}))")
	llcCompiles(t, "print({1, 2, 3}[2])\nprint(len({1, 2, 3}))")
}

func TestIRDictSetGlobals(t *testing.T) {
	// dict/set literals lower to dedicated global structs with a count field.
	res, err := Compile("print(len({1: 10, 2: 20}))")
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	if !strings.Contains(res.IR, "@.dict1 = private global {i32, [2 x i32], [2 x i32]}") {
		t.Fatalf("missing dict global struct:\n%s", res.IR)
	}
	res, err = Compile("print(len({1, 2, 3}))")
	if err != nil {
		t.Fatalf("compile set: %v", err)
	}
	if !strings.Contains(res.IR, "@.set1 = private global {i32, [3 x i32]}") {
		t.Fatalf("missing set global struct:\n%s", res.IR)
	}
}

func TestIRStringConstLenCompilesWithLLC(t *testing.T) {
	llcCompiles(t, "print(len(\"hello\"))\nprint(len(\"ab\" + \"cd\"))")
	llcCompiles(t, "print(len({1, 2, 3}))")
}

func TestIRStringConstLenFolds(t *testing.T) {
	// len of a string literal and a string-concat fold to constants, so no
	// getelementptr/load count is emitted for the string case.
	res, err := Compile("print(len(\"ab\" + \"cd\"))")
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	if !strings.Contains(res.IR, "i32 4") {
		t.Fatalf("len(\"ab\" + \"cd\") should fold to 4:\n%s", res.IR)
	}
	// the folded result must not emit a runtime count-field load.
	if strings.Contains(res.IR, "load i32") {
		t.Fatalf("len(string-concat) should constant-fold, got:\n%s", res.IR)
	}
}

func TestIRConstantFolding(t *testing.T) {
	res, err := Compile("x = 1 + 2")
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	if strings.Contains(res.IR, "add i32") {
		t.Fatalf("expected folded constant, got: %s", res.IR)
	}
	if !strings.Contains(res.IR, "i32 3") {
		t.Fatalf("expected folded constant 3 in IR: %s", res.IR)
	}
}

func TestIRForListCompilesWithLLC(t *testing.T) {
	llcCompiles(t, "s = 0\nfor x in [1, 2, 3]:\n    s = s + x\nprint(s)")
	llcCompiles(t, "s = 0\nfor x in [1, 2, 3]:\n    if x == 2:\n        continue\n    s = s + x\nprint(s)")
	llcCompiles(t, "s = 0\nfor x in [1, 2, 3]:\n    if x == 2:\n        break\n    s = s + x\nelse:\n    s = s + 100\nprint(s)")
}

func TestIRForListUnrolls(t *testing.T) {
	// for-over-list unrolls one body block per constant element, so the IR
	// must contain one for.list.body block per element.
	res, err := Compile("s = 0\nfor x in [1, 2, 3]:\n    s = s + x\nprint(s)")
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	// count label definitions (each starts a line) rather than the total,
	// which also counts the `br label %for.list.bodyN` branch targets.
	if strings.Count(res.IR, "\nfor.list.body") != 3 {
		t.Fatalf("expected 3 unrolled body blocks, got:\n%s", res.IR)
	}
	if !strings.Contains(res.IR, "store i32 1, i32* %_x") ||
		!strings.Contains(res.IR, "store i32 2, i32* %_x") ||
		!strings.Contains(res.IR, "store i32 3, i32* %_x") {
		t.Fatalf("for-over-list should store each element into the loop var:\n%s", res.IR)
	}
}

func TestIRComprehensionCompilesWithLLC(t *testing.T) {
	// Inline list comprehension over a constant list literal, including
	// indexing into the lowered comprehension result.
	llcCompiles(t, "print([x * 2 for x in [1, 2, 3]][1])")
	// Comprehension over range(n) with a constant condition.
	llcCompiles(t, "print([y * y for y in range(4) if y > 1][0])")
	// Chained use: fold and index in the same expression.
	llcCompiles(t, "print([x * 2 for x in [1, 2, 3]][0] + [x * 2 for x in [1, 2, 3]][2])")
}

func TestIRComprehensionLowersToGlobalStruct(t *testing.T) {
	res, err := Compile("print([x * 2 for x in [1, 2, 3]][1])")
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	if !strings.Contains(res.IR, ".lst") {
		t.Fatalf("comprehension should lower to a global struct, got:\n%s", res.IR)
	}
	// the folded elements 2, 4, 6 must appear as constant i32 initializers.
	if !strings.Contains(res.IR, "i32 2") || !strings.Contains(res.IR, "i32 6") {
		t.Fatalf("comprehension elements should be folded to constants:\n%s", res.IR)
	}
	// a comprehension with a condition must exclude filtered-out elements.
	res2, err := Compile("print([y * y for y in range(4) if y > 1][0])")
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	// y=0 and y=1 are filtered (0,1), so 4 and 9 must be present but 0/1 not.
	if !strings.Contains(res2.IR, "i32 4") || !strings.Contains(res2.IR, "i32 9") {
		t.Fatalf("condition should keep y>1 elements:\n%s", res2.IR)
	}
	if strings.Contains(res2.IR, "[2 x i32] [i32 0") || strings.Contains(res2.IR, "[2 x i32] [i32 1") {
		t.Fatalf("condition should filter y<=1 elements:\n%s", res2.IR)
	}
}

func TestIRSumMinMaxAbs(t *testing.T) {
	// sum: unrolled adds over the inline list literal's global struct.
	res, err := Compile("sum([1, 2, 3])")
	if err != nil {
		t.Fatalf("compile sum: %v", err)
	}
	if !strings.Contains(res.IR, "add i32") {
		t.Fatalf("sum IR missing add:\n%s", res.IR)
	}

	// min: icmp slt + select fold.
	res, err = Compile("min([3, 1, 2])")
	if err != nil {
		t.Fatalf("compile min: %v", err)
	}
	if !strings.Contains(res.IR, "icmp slt") || !strings.Contains(res.IR, "select i1") {
		t.Fatalf("min IR missing icmp/select:\n%s", res.IR)
	}

	// max: icmp sgt + select fold.
	res, err = Compile("max([3, 1, 2])")
	if err != nil {
		t.Fatalf("compile max: %v", err)
	}
	if !strings.Contains(res.IR, "icmp sgt") || !strings.Contains(res.IR, "select i1") {
		t.Fatalf("max IR missing icmp/select:\n%s", res.IR)
	}

	// abs of a negative literal is constant-folded to the positive value.
	res, err = Compile("abs(-5)")
	if err != nil {
		t.Fatalf("compile abs: %v", err)
	}
	if strings.Contains(res.IR, "icmp slt") || strings.Contains(res.IR, "select i1") {
		t.Fatalf("abs(-5) should constant-fold, got:\n%s", res.IR)
	}

	// abs of a non-literal emits icmp slt + select.
	res, err = Compile("x = 5\nabs(-x)")
	if err != nil {
		t.Fatalf("compile abs(-x): %v", err)
	}
	if !strings.Contains(res.IR, "icmp slt") || !strings.Contains(res.IR, "select i1") {
		t.Fatalf("abs(-x) IR missing icmp/select:\n%s", res.IR)
	}
}
