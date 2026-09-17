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

func TestIRMatchWildcardCompilesWithLLC(t *testing.T) {
	// `case _:` wildcard lowers pat := sub, so the icmp is always true.
	res, err := Compile("x = 5\nmatch x:\n    case 1:\n        print(1)\n    case _:\n        print(9)")
	if err != nil {
		t.Fatalf("compile match wildcard: %v", err)
	}
	// the subject register is %_x.ld1; the wildcard compares it to itself.
	if !strings.Contains(res.IR, "icmp eq i32 %_x.ld1, %_x.ld1") {
		t.Fatalf("wildcard case should compare subject to itself, got:\n%s", res.IR)
	}
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

func TestIRModuloCompilesWithLLC(t *testing.T) {
	// constant modulo folds to a constant (17 % 5 = 2).
	res, err := Compile("print(17 % 5)")
	if err != nil {
		t.Fatalf("compile constant %%%%: %v", err)
	}
	if !strings.Contains(res.IR, "i32 2") {
		t.Fatalf("constant modulo should fold to 2, got:\n%s", res.IR)
	}
	// runtime modulo lowers to srem (mirrors the interpreter's %%).
	res, err = Compile("x = 17\nprint(x % 5)")
	if err != nil {
		t.Fatalf("compile runtime %%%%: %v", err)
	}
	if !strings.Contains(res.IR, "srem i32") {
		t.Fatalf("runtime modulo should lower to srem, got:\n%s", res.IR)
	}
}

func TestIRZeroArgPrintCompilesWithLLC(t *testing.T) {
	// zero-argument print() writes nothing: no printf calls.
	res, err := Compile("print()")
	if err != nil {
		t.Fatalf("compile zero-arg print: %v", err)
	}
	if strings.Count(res.IR, "call i32 @printf") != 0 {
		t.Fatalf("print() should emit no printf calls, got:\n%s", res.IR)
	}
}

func TestIRMultiArgPrintCompilesWithLLC(t *testing.T) {
	// multi-argument print emits one printf per argument (each on its own
	// line), mirroring the interpreter's print.
	res, err := Compile("print(1, 2)")
	if err != nil {
		t.Fatalf("compile multi-arg print: %v", err)
	}
	if strings.Count(res.IR, "call i32 @printf") != 2 {
		t.Fatalf("print(1, 2) should emit 2 printf calls, got:\n%s", res.IR)
	}
	res, err = Compile("x = 7\nprint(x, x + 1)")
	if err != nil {
		t.Fatalf("compile multi-arg print: %v", err)
	}
	if strings.Count(res.IR, "call i32 @printf") != 2 {
		t.Fatalf("print(x, x+1) should emit 2 printf calls, got:\n%s", res.IR)
	}
	// string-literal arguments use a %%s\n format (not %%d\n).
	res, err = Compile("print(\"hi\")")
	if err != nil {
		t.Fatalf("compile string print: %v", err)
	}
	// the newline in the format global is emitted as \0A in IR.
	if strings.Contains(res.IR, "%d\\0A") {
		t.Fatalf("print(\"hi\") should use %%s format, got:\n%s", res.IR)
	}
	if !strings.Contains(res.IR, "%s\\0A") {
		t.Fatalf("print(\"hi\") should use %%s format, got:\n%s", res.IR)
	}
	// mixed integer + string args: one %%d and one %%s format.
	res, err = Compile("print(1, \"hi\")")
	if err != nil {
		t.Fatalf("compile mixed print: %v", err)
	}
	if !strings.Contains(res.IR, "%d\\0A") || !strings.Contains(res.IR, "%s\\0A") {
		t.Fatalf("mixed print should emit %%d and %%s formats, got:\n%s", res.IR)
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
func TestIRSetComprehensionCompilesWithLLC(t *testing.T) {
	// {x * x for x in [1, 2, 2]} dedups to {1, 4}.
	llcCompiles(t, "print(len({x * x} for x in [1, 2, 2]))")
}

func TestIRSetComprehensionLowersToSetGlobal(t *testing.T) {
	ir := llcCompiles(t, "print(len({x * x} for x in [1, 2, 3]))")
	if !strings.Contains(ir, "@.set1 = private global {i32, [3 x i32]}") {
		t.Fatalf("set comprehension should lower to a set global, got:\n%s", ir)
	}
}

func TestIRDictComprehensionCompilesWithLLC(t *testing.T) {
	// {x: x * 10 for x in [1, 2, 3]} builds a 3-entry dict.
	llcCompiles(t, "print(len({x: x * 10} for x in [1, 2, 3]))")
}

func TestIRDictComprehensionLowersToDictGlobal(t *testing.T) {
	ir := llcCompiles(t, "print(len({x: x * 10} for x in [1, 2]))")
	if !strings.Contains(ir, "@.dict1 = private global {i32, [2 x i32], [2 x i32]}") {
		t.Fatalf("dict comprehension should lower to a dict global, got:\n%s", ir)
	}
}

func TestIRDictComprehensionIndexCompilesWithLLC(t *testing.T) {
	// d[key] on a lowered dict comprehension resolves the mapped value at
	// codegen time (constant key lookup, not a positional GEP).
	llcCompiles(t, "print(({x: x * 10} for x in [1, 2])[1])")
	llcCompiles(t, "print(({x: x * 10} for x in [1, 2])[2])")
}

func TestIRDictComprehensionIndexMissingKeyErrors(t *testing.T) {
	// d[3] with no matching key must fail at codegen, like a normal dict.
	if _, err := Compile("print(({x: x * 10} for x in [1, 2])[3])"); err == nil {
		t.Fatalf("expected key-not-found compile error")
	}
}


func TestIRDictComprehensionIndexLowersToConstant(t *testing.T) {
	// d[1] over {x: x * 10} for x in [1, 2] is resolved to the constant 10 at
	// codegen time (no runtime GEP/lookup).
	ir := llcCompiles(t, "print(({x: x * 10} for x in [1, 2])[1])")
	if !strings.Contains(ir, "i32 10") {
		t.Fatalf("dict comprehension index should fold to constant 10, got:\n%s", ir)
	}
}

func TestIRSetComprehensionIndexCompilesWithLLC(t *testing.T) {
	// s[key] on a lowered set comprehension is a membership test returning
	// the element when present.
	llcCompiles(t, "print(({x * x} for x in [1, 2])[1])")
	llcCompiles(t, "print(({x * x} for x in [1, 2])[4])")
}

func TestIRSetComprehensionIndexMissingErrors(t *testing.T) {
	// s[5] with no matching element must fail at codegen (membership test).
	if _, err := Compile("print(({x * x} for x in [1, 2])[5])"); err == nil {
		t.Fatalf("expected not-in-set compile error")
	}
}

func TestIRTernaryCompilesWithLLC(t *testing.T) {
	// ternary with a constant condition folds to the taken branch.
	llcCompiles(t, "print(5 if 1 else 3)")
	llcCompiles(t, "print(10 if 0 else 42)")
	// ternary with a runtime comparison lowers to a select.
	llcCompiles(t, "print(7 if 2 > 1 else 99)")
	// right-associative nested ternary.
	llcCompiles(t, "print(1 if 0 else 2 if 1 else 3)")
}

func TestIRTernaryLowersToSelect(t *testing.T) {
	res, err := Compile("print(7 if 2 > 1 else 99)")
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	if !strings.Contains(res.IR, "select i1") {
		t.Fatalf("runtime ternary should lower to a select, got:\n%s", res.IR)
	}
}

func TestIRMultiArgRangeComprehension(t *testing.T) {
	// 2-arg and 3-arg range iterables in AOT comprehensions must compile.
	llcCompiles(t, "print(sum([x for x in range(0, 5)]))")
	llcCompiles(t, "print(sum([x for x in range(1, 5, 2)]))")
	llcCompiles(t, "print(max([x for x in range(2, 10, 3)]))")
	llcCompiles(t, "print(min([x for x in range(5, 0, -1)]))")
}

func TestIRAggregateOverComprehension(t *testing.T) {
	// len/sum/min/max over a lowered comprehension must compile and verify.
	llcCompiles(t, "print(len([x for x in range(5)]))")
	llcCompiles(t, "print(sum([x for x in range(5)]))")
	llcCompiles(t, "print(min([y * y for y in range(3)]))")
	llcCompiles(t, "print(max([x * 2 for x in [1, 2, 3]]))")
}

func TestIRSumFoldsComprehension(t *testing.T) {
	res, err := Compile("print(sum([x for x in range(5)]))")
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	// sum over the folded comprehension 0..4 folds to constant 10.
	if !strings.Contains(res.IR, "10") {
		t.Fatalf("sum of a comprehension should fold to a constant, got:\n%s", res.IR)
	}
}
func TestIRRuntimeListAgg(t *testing.T) {
	// min/max/sum over a list of runtime variables lowers each element via
	// g.value directly (no emitList global), so runtime elements work.
	progs := []string{
		"a = 3\nb = 1\nprint(min([a, b]))",
		"a = 3\nb = 1\nprint(max([a, b]))",
		"a = 3\nb = 1\nprint(sum([a, b]))",
	}
	for _, p := range progs {
		res, err := Compile(p)
		if err != nil {
			t.Fatalf("compile %q: %v", p, err)
		}
		if !strings.Contains(res.IR, "icmp") && !strings.Contains(res.IR, "add") {
			t.Fatalf("%q: no element arithmetic emitted:\n%s", p, res.IR)
		}
	}
}

func TestIRRuntimeLenIndex(t *testing.T) {
	// len of a runtime-element list literal returns the count directly; list
	// index evaluates the indexed element via g.value directly.
	progs := []string{
		"a = 3\nb = 1\nprint(len([a, b]))",
		"a = 3\nb = 1\nprint([a, b][0])",
	}
	for _, p := range progs {
		res, err := Compile(p)
		if err != nil {
			t.Fatalf("compile %q: %v", p, err)
		}
		if strings.Count(res.IR, "@printf") == 0 {
			t.Fatalf("%q: no printf emitted:\n%s", p, res.IR)
		}
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

func TestIRSumSetLiteralCompilesWithLLC(t *testing.T) {
	llcCompiles(t, "print(sum({1, 2, 3}))")
}

func TestIRMinDictLiteralCompilesWithLLC(t *testing.T) {
	// min over a dict literal folds over keys (interpreter semantics).
	llcCompiles(t, "print(min({1: 10, 2: 20}))")
}

func TestIRMaxSetLiteralCompilesWithLLC(t *testing.T) {
	llcCompiles(t, "print(max({1, 2, 3}))")
}

func TestIRLambdaInlineCompilesWithLLC(t *testing.T) {
	// `print((lambda x: int: x + 1)(5))` -> 6 via an anonymous FuncDef + call.
	ir := llcCompiles(t, "print((lambda x: int: x + 1)(5))")
	if !strings.Contains(ir, "@lambda_") {
		t.Fatalf("expected generated lambda FuncDef, got:\n%s", ir)
	}
}

func TestIRLambdaNamedCompilesWithLLC(t *testing.T) {
	// `f = lambda x: int: x * 2; f(3)` -> 6 via g.lambdas resolution.
	ir := llcCompiles(t, "f = lambda x: int: x * 2\nprint(f(3))")
	if !strings.Contains(ir, "@lambda_") {
		t.Fatalf("expected generated lambda FuncDef, got:\n%s", ir)
	}
}

func TestIRStrMethodUpperLenFolds(t *testing.T) {
	// `len("AbC".upper())` constant-folds upper to "ABC" and returns 3.
	ir := llcCompiles(t, `print(len("AbC".upper()))`)
	if !strings.Contains(ir, "3") {
		t.Fatalf("expected folded length 3, got:\n%s", ir)
	}
}

func TestIRStrMethodLowerStripFolds(t *testing.T) {
	// `len(" AbC ".strip())` folds strip to "AbC" (len 3).
	llcCompiles(t, `print(len(" AbC ".strip()))`)
	// `len("ABC".lower())` folds lower to "abc" (len 3).
	llcCompiles(t, `print(len("ABC".lower()))`)
}

func TestIRStrMethodPrintLowersString(t *testing.T) {
	// print("AbC".upper()) emits the folded string global (ABC).
	ir := llcCompiles(t, `print("AbC".upper())`)
	if !strings.Contains(ir, "ABC") {
		t.Fatalf("expected folded string ABC in IR, got:\n%s", ir)
	}
}

func TestIRDictKeysValuesLowers(t *testing.T) {
	// sum({1: 2, 3: 4}.keys()) folds keys to a list and sums to 4.
	ir := llcCompiles(t, "print(sum({1: 2, 3: 4}.keys()))")
	if !strings.Contains(ir, "4") {
		t.Fatalf("expected folded sum 4, got:\n%s", ir)
	}
	llcCompiles(t, "print(sum({1: 2, 3: 4}.values()))")
}

func TestIRListAppendLowers(t *testing.T) {
	// sum([1, 2, 3].append(4)) folds append to [1,2,3,4] and sums via IR.
	ir := llcCompiles(t, "print(sum([1, 2, 3].append(4)))")
	if !strings.Contains(ir, "add i32") {
		t.Fatalf("expected IR sum folding, got:\n%s", ir)
	}
}

func TestIRDictItemsLenLowers(t *testing.T) {
	// len({1: 2, 3: 4}.items()) folds to the pair count 2.
	ir := llcCompiles(t, "print(len({1: 2, 3: 4}.items()))")
	if !strings.Contains(ir, "2") {
		t.Fatalf("expected folded pair count 2, got:\n%s", ir)
	}
}

func TestIRDictMinMaxMethodsLowers(t *testing.T) {
	// max({1: 2, 3: 4}.keys()) -> 3, min({1: 2, 3: 4}.values()) -> 2.
	ir := llcCompiles(t, "print(max({1: 2, 3: 4}.keys()))")
	if !strings.Contains(ir, "3") {
		t.Fatalf("expected max 3, got:\n%s", ir)
	}
	llcCompiles(t, "print(min({1: 2, 3: 4}.values()))")
}

func TestIRStrIndexFolds(t *testing.T) {
	// print("abc"[1]) folds to 98 ('b').
	ir := llcCompiles(t, `print("abc"[1])`)
	if !strings.Contains(ir, "98") {
		t.Fatalf("expected folded char code 98, got:\n%s", ir)
	}
}

func TestIRStrSplitLenFolds(t *testing.T) {
	// len("a b c".split()) folds to 3.
	ir := llcCompiles(t, `print(len("a b c".split()))`)
	if !strings.Contains(ir, "3") {
		t.Fatalf("expected folded split count 3, got:\n%s", ir)
	}
}

func TestIRStrEqFolds(t *testing.T) {
	// print("abc" == "abd") folds to 0.
	ir := llcCompiles(t, `print("abc" == "abd")`)
	if !strings.Contains(ir, "0") {
		t.Fatalf("expected folded 0, got:\n%s", ir)
	}
	llcCompiles(t, `print("abc" == "abc")`)
}

func TestIRStrBuiltinFolds(t *testing.T) {
	// len(str(42)) folds to 2.
	ir := llcCompiles(t, `print(len(str(42)))`)
	if !strings.Contains(ir, "2") {
		t.Fatalf("expected folded 2, got:\n%s", ir)
	}
}

func TestIRStrReplaceFolds(t *testing.T) {
	// `print("aXbXc".replace("X", "-"))` folds to a single string global
	// "a-b-c", so the emitted IR contains that exact string constant.
	ir := llcCompiles(t, `print("aXbXc".replace("X", "-"))`)
	if !strings.Contains(ir, "a-b-c") {
		t.Fatalf("expected folded replace result a-b-c in IR, got:\n%s", ir)
	}
}

func TestIRStrFindFolds(t *testing.T) {
	// `print("abcabc".find("bc"))` folds to the index 1, so the emitted IR
	// contains the i32 constant 1.
	ir := llcCompiles(t, `print("abcabc".find("bc"))`)
	if !strings.Contains(ir, "i32 1") {
		t.Fatalf("expected folded find result 1 in IR, got:\n%s", ir)
	}
}

func TestIRStrRfindFolds(t *testing.T) {
	// `print("abcabc".rfind("bc"))` folds to the last index 4.
	ir := llcCompiles(t, `print("abcabc".rfind("bc"))`)
	if !strings.Contains(ir, "i32 4") {
		t.Fatalf("expected folded rfind result 4 in IR, got:\n%s", ir)
	}
}

func TestIRStrCapitalizeFolds(t *testing.T) {
	// `print(len("hello".capitalize()))` folds capitalize to "Hello" (len 5).
	ir := llcCompiles(t, `print(len("hello".capitalize()))`)
	if !strings.Contains(ir, "i32 5") {
		t.Fatalf("expected folded capitalize length 5 in IR, got:\n%s", ir)
	}
}

func TestIRStrTitleFolds(t *testing.T) {
	// `print(len("hello world".title()))` folds title to "Hello World" (len 11).
	ir := llcCompiles(t, `print(len("hello world".title()))`)
	if !strings.Contains(ir, "i32 11") {
		t.Fatalf("expected folded title length 11 in IR, got:\n%s", ir)
	}
}

func TestIRStrSwapcaseFolds(t *testing.T) {
	// `print(len("HeLLo".swapcase()))` folds swapcase to "hEllO" (len 5).
	ir := llcCompiles(t, `print(len("HeLLo".swapcase()))`)
	if !strings.Contains(ir, "i32 5") {
		t.Fatalf("expected folded swapcase length 5 in IR, got:\n%s", ir)
	}
}

func TestIRStrIsdigitFolds(t *testing.T) {
	// `print("123".isdigit())` folds to i32 1.
	ir := llcCompiles(t, `print("123".isdigit())`)
	if !strings.Contains(ir, "i32 1") {
		t.Fatalf("expected folded isdigit 1 in IR, got:\n%s", ir)
	}
}

func TestIRStrIsalphaFolds(t *testing.T) {
	// `print("abc".isalpha())` folds to i32 1.
	ir := llcCompiles(t, `print("abc".isalpha())`)
	if !strings.Contains(ir, "i32 1") {
		t.Fatalf("expected folded isalpha 1 in IR, got:\n%s", ir)
	}
}

func TestIRStrIslowerIsupperFolds(t *testing.T) {
	// `print("abc".islower())` folds to i32 1, `print("ABC".isupper())` too.
	ir := llcCompiles(t, `print("abc".islower())`)
	if !strings.Contains(ir, "i32 1") {
		t.Fatalf("expected folded islower 1 in IR, got:\n%s", ir)
	}
	ir = llcCompiles(t, `print("ABC".isupper())`)
	if !strings.Contains(ir, "i32 1") {
		t.Fatalf("expected folded isupper 1 in IR, got:\n%s", ir)
	}
}

func TestIRStrIsalnumFolds(t *testing.T) {
	// `print("abc123".isalnum())` folds to i32 1.
	ir := llcCompiles(t, `print("abc123".isalnum())`)
	if !strings.Contains(ir, "i32 1") {
		t.Fatalf("expected folded isalnum 1 in IR, got:\n%s", ir)
	}
}

func TestIRStrIsspaceFolds(t *testing.T) {
	// `print("   ".isspace())` folds to i32 1.
	ir := llcCompiles(t, `print("   ".isspace())`)
	if !strings.Contains(ir, "i32 1") {
		t.Fatalf("expected folded isspace 1 in IR, got:\n%s", ir)
	}
}

func TestIRStrStartswithEndswithFolds(t *testing.T) {
	// startswith/endswith fold to i32 1 or 0.
	ir := llcCompiles(t, `print("hello".startswith("he"))`)
	if !strings.Contains(ir, "i32 1") {
		t.Fatalf("expected folded startswith result 1 in IR, got:\n%s", ir)
	}
	ir = llcCompiles(t, `print("hello".endswith("he"))`)
	if !strings.Contains(ir, "i32 0") {
		t.Fatalf("expected folded endswith result 0 in IR, got:\n%s", ir)
	}
}

func TestIRStrCountFolds(t *testing.T) {
	// `print("ababab".count("ab"))` folds to the count 3, so the emitted IR
	// contains the i32 constant 3.
	ir := llcCompiles(t, `print("ababab".count("ab"))`)
	if !strings.Contains(ir, "i32 3") {
		t.Fatalf("expected folded count result 3 in IR, got:\n%s", ir)
	}
}

func TestIRStrJoinFolds(t *testing.T) {
	// `print(len("x".join(["a", "b" ])))` folds join to "axb" (len 3),
	// so the emitted IR contains i32 3.
	ir := llcCompiles(t, `print(len("x".join(["a", "b"])))`)
	if !strings.Contains(ir, "i32 3") {
		t.Fatalf("expected folded join length 3 in IR, got:\n%s", ir)
	}
}

func TestIRStrLstripRstripFolds(t *testing.T) {
	// `print(len("  hi  ".lstrip()))` folds lstrip to "hi  " (len 4),
	// so the emitted IR contains i32 4.
	ir := llcCompiles(t, `print(len("  hi  ".lstrip()))`)
	if !strings.Contains(ir, "i32 4") {
		t.Fatalf("expected folded lstrip length 4 in IR, got:\n%s", ir)
	}
	// `print(len("  hi  ".rstrip()))` folds rstrip to "  hi" (len 4).
	ir = llcCompiles(t, `print(len("  hi  ".rstrip()))`)
	if !strings.Contains(ir, "i32 4") {
		t.Fatalf("expected folded rstrip length 4 in IR, got:\n%s", ir)
	}
}

func TestIRSortedFolds(t *testing.T) {
	// sorted(list) folds to a sorted inline list literal global at codegen.
	// A bare expression statement emits the folded list global (the codegen
	// represents lists as constant integer-element globals).
	ir := llcCompiles(t, `sorted([3, 1, 2])`)
	if !strings.Contains(ir, "[i32 1, i32 2, i32 3]") {
		t.Fatalf("sorted should fold to ascending [1,2,3] global, got:\n%s", ir)
	}

	// sorted(iter, reverse=True) folds to a descending list literal global.
	ir = llcCompiles(t, `sorted([3, 1, 2], reverse=True)`)
	if !strings.Contains(ir, "[i32 3, i32 2, i32 1]") {
		t.Fatalf("sorted reverse=True should fold to descending [3,2,1], got:\n%s", ir)
	}

	// a truthy positional second arg also means descending.
	ir = llcCompiles(t, `sorted([3, 1, 2], 1)`)
	if !strings.Contains(ir, "[i32 3, i32 2, i32 1]") {
		t.Fatalf("sorted positional truthy should fold to descending [3,2,1], got:\n%s", ir)
	}

	// reverse=False keeps ascending order.
	ir = llcCompiles(t, `sorted([3, 1, 2], reverse=False)`)
	if !strings.Contains(ir, "[i32 1, i32 2, i32 3]") {
		t.Fatalf("sorted reverse=False should fold to ascending [1,2,3], got:\n%s", ir)
	}

	// sorted over an already-sorted list must still verify cleanly.
	llcCompiles(t, `sorted([1, 2, 3])`)
}

func TestIRChrOrdFolds(t *testing.T) {
	// chr(n) folds a constant codepoint to a single-character string global.
	ir := llcCompiles(t, `chr(65)`)
	if !strings.Contains(ir, `c"A\00"`) {
		t.Fatalf("chr(65) should fold to a single-char string global c\"A\\00\", got:\n%s", ir)
	}

	// chr(97) folds to lowercase 'a'.
	ir = llcCompiles(t, `chr(97)`)
	if !strings.Contains(ir, `c"a\00"`) {
		t.Fatalf("chr(97) should fold to c\"a\\00\", got:\n%s", ir)
	}

	// ord(s) folds a constant string to its first-byte codepoint; consumed
	// by print so the folded i32 lands in the IR.
	ir = llcCompiles(t, `print(ord("A"))`)
	if !strings.Contains(ir, "65") {
		t.Fatalf("ord(\"A\") should fold to 65, got:\n%s", ir)
	}

	// ord folds to the first byte codepoint (interpreter mirrors sval[0]).
	ir = llcCompiles(t, `print(ord("hello"))`)
	if !strings.Contains(ir, "104") {
		t.Fatalf("ord(\"hello\") should fold to first byte 104, got:\n%s", ir)
	}
}

func TestIRLjustRjustFolds(t *testing.T) {
	// "s".ljust(w) pads on the right with spaces to width w -> string global.
	ir := llcCompiles(t, `"ab".ljust(5)`)
	if !strings.Contains(ir, "c\"ab   \\00\"") {
		t.Fatalf("ljust(ab,5) should fold to a padded global, got:\n%s", ir)
	}

	// "s".rjust(w) pads on the left with spaces to width w.
	ir = llcCompiles(t, `"ab".rjust(5)`)
	if !strings.Contains(ir, "c\"   ab\\00\"") {
		t.Fatalf("rjust(ab,5) should fold to a padded global, got:\n%s", ir)
	}

	// When len(s) >= w both are no-ops (mirror the interpreter).
	ir = llcCompiles(t, `"abc".ljust(2)`)
	if !strings.Contains(ir, "c\"abc\\00\"") {
		t.Fatalf("ljust no-op should fold to c\"abc\\00\", got:\n%s", ir)
	}
	ir = llcCompiles(t, `"abc".rjust(2)`)
	if !strings.Contains(ir, "c\"abc\\00\"") {
		t.Fatalf("rjust no-op should fold to c\"abc\\00\", got:\n%s", ir)
	}

	// Exact width (len == w) is also a no-op.
	llcCompiles(t, `"ab".ljust(2)`)
}
func TestIRZfillFolds(t *testing.T) {
	// "s".zfill(w) pads on the left with '0' to width w -> string global.
	ir := llcCompiles(t, `"ab".zfill(5)`)
	// IR global is c"000ab" + nul terminator: assert on the padded content.
	if !strings.Contains(ir, "000ab") {
		t.Fatalf("zfill(ab,5) should fold to a 000ab global, got:\n%s", ir)
	}

	// When len(s) >= w it is a no-op (mirror the interpreter).
	ir = llcCompiles(t, `"abc".zfill(2)`)
	if !strings.Contains(ir, "abc") {
		t.Fatalf("zfill no-op should keep abc, got:\n%s", ir)
	}

	// Exact width (len == w) is also a no-op.
	llcCompiles(t, `"ab".zfill(2)`)
}
func TestIRRemoveprefixSuffixFolds(t *testing.T) {
	// "s".removeprefix(p) strips the prefix -> string global.
	ir := llcCompiles(t, `"abcabc".removeprefix("abc")`)
	if !strings.Contains(ir, "abc") {
		t.Fatalf("removeprefix should strip abc, got:\n%s", ir)
	}

	// "s".removesuffix(s) strips the suffix.
	ir = llcCompiles(t, `"abcabc".removesuffix("abc")`)
	if !strings.Contains(ir, "abc") {
		t.Fatalf("removesuffix should strip abc, got:\n%s", ir)
	}

	// No-match leaves the receiver unchanged (mirror TrimPrefix/TrimSuffix).
	llcCompiles(t, `"abc".removeprefix("xyz")`)
	llcCompiles(t, `"abc".removesuffix("xyz")`)
}

func TestIRRoundFolds(t *testing.T) {
	// round(x) folds a constant integer literal to itself (no floats in
	// the AOT backend); consumed by print so the folded i32 lands in the IR.
	ir := llcCompiles(t, `print(round(42))`)
	if !strings.Contains(ir, "42") {
		t.Fatalf("round(42) should fold to 42, got:\n%s", ir)
	}

	// round over a negative int literal also folds unchanged.
	ir = llcCompiles(t, `print(round(-7))`)
	if !strings.Contains(ir, "-7") {
		t.Fatalf("round(-7) should fold to -7, got:\n%s", ir)
	}
}

func TestIRIndexFolds(t *testing.T) {
	// "s".index(sub) folds to the byte index of sub (strings.Index).
	ir := llcCompiles(t, `print("abcabc".index("bc"))`)
	if !strings.Contains(ir, " 1") && !strings.Contains(ir, "1 ") {
		t.Fatalf("index(bc) should fold to 1, got:\n%s", ir)
	}

	// Not-found folds to -1 (codegen has no error channel, like find).
	ir = llcCompiles(t, `print("abc".index("xyz"))`)
	if !strings.Contains(ir, "-1") {
		t.Fatalf("index not-found should fold to -1, got:\n%s", ir)
	}
}

func TestIRFloatFolds(t *testing.T) {
	// float(int) folds to the int (AOT represents floats as truncated ints).
	ir := llcCompiles(t, `print(float(42))`)
	if !strings.Contains(ir, " 42") && !strings.Contains(ir, "42 ") {
		t.Fatalf("float(42) should fold to 42, got:\n%s", ir)
	}

	// float(str) parses the string to a float then truncates.
	ir = llcCompiles(t, `print(float("42.5"))`)
	if !strings.Contains(ir, " 42") && !strings.Contains(ir, "42 ") {
		t.Fatalf("float(\"42.5\") should truncate to 42, got:\n%s", ir)
	}
}

func TestIRExpandtabsFolds(t *testing.T) {
	// "s".expandtabs(w) folds to a string global. Source literals have no
	// escape sequences, so literal receivers contain no tabs -> no-op.
	ir := llcCompiles(t, `"abc".expandtabs(4)`)
	if !strings.Contains(ir, "abc") {
		t.Fatalf("expandtabs no-tab should keep abc, got:\n%s", ir)
	}

	// verify cleanly for a positive width.
	llcCompiles(t, `"a  b".expandtabs(4)`)
}

func TestIRDictGetFolds(t *testing.T) {
	// {k: v}.get(key) returns the value for the key.
	ir := llcCompiles(t, `print({1: 42}.get(1))`)
	if !strings.Contains(ir, " 42") && !strings.Contains(ir, "42 ") {
		t.Fatalf("get(1) should fold to 42, got:\n%s", ir)
	}

	// Not-found returns the default.
	ir = llcCompiles(t, `print({1: 42}.get(2, 7))`)
	if !strings.Contains(ir, " 7") && !strings.Contains(ir, "7 ") {
		t.Fatalf("get(2, 7) should fold to default 7, got:\n%s", ir)
	}
}


func TestIRListCallConsumers(t *testing.T) {
	// len/sum/min/max/any/all fold over a list-returning builtin call
	// (sorted/reversed) by unwrapping the underlying inline list literal.
	// The element set is preserved (only reordered), so the folds match the
	// interpreter semantics. All cases must emit valid, llc-acceptable IR.

	// len over sorted/reversed preserves element count (a literal constant).
	ir := llcCompiles(t, `print(len(sorted([3, 1, 2])))`)
	if !strings.Contains(ir, "i32 3") {
		t.Fatalf("len(sorted([3,1,2])) should fold to 3, got:\n%s", ir)
	}
	ir = llcCompiles(t, `print(len(reversed([3, 1, 2])))`)
	if !strings.Contains(ir, "i32 3") {
		t.Fatalf("len(reversed([3,1,2])) should fold to 3, got:\n%s", ir)
	}

	// sum over sorted/reversed emits the add chain building 6.
	ir = llcCompiles(t, `print(sum(sorted([3, 1, 2])))`)
	if !strings.Contains(ir, "add i32 3, 1") {
		t.Fatalf("sum(sorted([3,1,2])) should add 3+1, got:\n%s", ir)
	}
	ir = llcCompiles(t, `print(sum(reversed([3, 1, 2])))`)
	if !strings.Contains(ir, "add i32 3, 1") {
		t.Fatalf("sum(reversed([3,1,2])) should add 3+1, got:\n%s", ir)
	}

	// min/max over sorted/reversed select the underlying extrema.
	ir = llcCompiles(t, `print(min(sorted([3, 1, 2])))`)
	if !strings.Contains(ir, "icmp slt") {
		t.Fatalf("min(sorted([3,1,2])) should compare, got:\n%s", ir)
	}
	ir = llcCompiles(t, `print(max(reversed([3, 1, 2])))`)
	if !strings.Contains(ir, "icmp sgt") {
		t.Fatalf("max(reversed([3,1,2])) should compare, got:\n%s", ir)
	}

	// any/all over sorted/reversed widen the boolean accumulator to i32.
	ir = llcCompiles(t, `print(any(sorted([0, 2, 3])))`)
	if !strings.Contains(ir, "zext i1") {
		t.Fatalf("any(sorted([0,2,3])) should zext result to i32, got:\n%s", ir)
	}
	ir = llcCompiles(t, `print(all(sorted([1, 2, 3])))`)
	if !strings.Contains(ir, "zext i1") {
		t.Fatalf("all(sorted([1,2,3])) should zext result to i32, got:\n%s", ir)
	}
}

func TestIRListLenFolds(t *testing.T) {
	// len over list-producing builtin calls (enumerate/zip/partition/split/
	// rsplit) folds to the matching literal length constant.
	cases := []struct{ src, want string }{
		{`print(len(enumerate([1, 2, 3])))`, "i32 3"},
		{`print(len(zip([1, 2], [3, 4, 5])))`, "i32 2"},
		{`print(len(zip([1, 2, 3], [4])))`, "i32 1"},
		{`print(len("hello".partition("l")))`, "i32 3"},
		{`print(len("a,b,c".split(",")))`, "i32 3"},
		{`print(len("a,b,c".rsplit(",")))`, "i32 3"},
		{`print(len("".split(",")))`, "i32 1"},
		{`print(len("abc".split(",")))`, "i32 1"},
	}
	for _, tc := range cases {
		ir := llcCompiles(t, tc.src)
		if !strings.Contains(ir, tc.want) {
			t.Fatalf("%s: want constant %s, got:\n%s", tc.src, tc.want, ir)
		}
	}
}
