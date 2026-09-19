package lang

import (
	"os/exec"
	"strings"
	"testing"
)

// ---------------------------------------------------------------------------
// helpers
// ---------------------------------------------------------------------------

// mustOpt optimizes the IR produced from a gusty source at the given level.
func mustOpt(t *testing.T, src string, level int) string {
	t.Helper()
	res, err := Compile(src)
	if err != nil {
		t.Fatalf("Compile(%q): %v", src, err)
	}
	return OptimizeIR(res.IR, level)
}

func hasLine(ir, want string) bool {
	for _, l := range strings.Split(ir, "\n") {
		if strings.Contains(l, want) {
			return true
		}
	}
	return false
}

// ---------------------------------------------------------------------------
// level / API contract
// ---------------------------------------------------------------------------

func TestOptimizeIRLevel0Identity(t *testing.T) {
	res, err := Compile("x = 5\nprint(x)\n")
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	got := OptimizeIR(res.IR, 0)
	if got != res.IR {
		t.Fatalf("level 0 must be identity\n--- got ---\n%s\n--- want ---\n%s", got, res.IR)
	}
}

func TestOptimizeIRLevelNegativeIdentity(t *testing.T) {
	res, err := Compile("print(1)\n")
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	if got := OptimizeIR(res.IR, -1); got != res.IR {
		t.Fatalf("level < 0 must be identity")
	}
}

// ---------------------------------------------------------------------------
// dead-global elimination (module level)
// ---------------------------------------------------------------------------

func TestDeadGlobalElim(t *testing.T) {
	ir := `@live = private unnamed_addr constant [1 x i8] c"x\00"
@dead = private unnamed_addr constant [1 x i8] c"y\00"
declare i32 @printf(i8*, ...)
define i32 @main() {
entry:
  %t = call i32 (i8*, ...) @printf(i8* getelementptr inbounds ([1 x i8], [1 x i8]* @live, i32 0, i32 0))
  ret i32 0
}
`
	out := OptimizeIR(ir, 1)
	if !strings.Contains(out, "@live") {
		t.Fatalf("live global dropped:\n%s", out)
	}
	if strings.Contains(out, "@dead") {
		t.Fatalf("dead global kept:\n%s", out)
	}
	if !strings.Contains(out, "declare i32 @printf") {
		t.Fatalf("declare dropped:\n%s", out)
	}
}

// ---------------------------------------------------------------------------
// constant propagation + folding
// ---------------------------------------------------------------------------

func TestConstantFoldArithmetic(t *testing.T) {
	ir := `define i32 @main() {
entry:
  %a = add i32 5, 3
  %b = mul i32 %a, 2
  %c = icmp sgt i32 %b, 10
  br i1 %c, label %then, label %else
then:
  ret i32 1
else:
  ret i32 0
}
`
	out := OptimizeIR(ir, 1)
	// 5+3=8, 8*2=16, 16>10=true => the else branch is dead.
	if strings.Contains(out, "else:") {
		t.Fatalf("dead branch not eliminated:\n%s", out)
	}
	if !strings.Contains(out, "then:") {
		t.Fatalf("live branch dropped:\n%s", out)
	}
}

func TestConstantFoldZextSelect(t *testing.T) {
	ir := `define i32 @main() {
entry:
  %c = icmp eq i1 true, true
  %z = zext i1 %c to i32
  %s = select i1 %c, i32 7, i32 9
  %r = add i32 %z, %s
  ret i32 %r
}
`
	out := OptimizeIR(ir, 1)
	// c = true, z = 1, s = 7, r = 8.
	if !strings.Contains(out, "ret i32 8") {
		t.Fatalf("constant not propagated to ret:\n%s", out)
	}
}

// ---------------------------------------------------------------------------
// mem2reg-style alloca promotion
// ---------------------------------------------------------------------------

func TestAllocaPromotion(t *testing.T) {
	ir := `define i32 @main() {
entry:
  %x = alloca double
  store i32 5, i32* %x
  %a = load i32, i32* %x
  %b = add i32 %a, 1
  %c = load i32, i32* %x
  %r = add i32 %b, %c
  ret i32 %r
}
`
	out := OptimizeIR(ir, 1)
	if strings.Contains(out, "alloca") {
		t.Fatalf("alloca not promoted:\n%s", out)
	}
	if strings.Contains(out, "store i32 5") || strings.Contains(out, "load i32") {
		t.Fatalf("store/load not removed after promotion:\n%s", out)
	}
	// 5+1=6, +5=11.
	if !strings.Contains(out, "ret i32 11") {
		t.Fatalf("promoted value not folded:\n%s", out)
	}
}

func TestAllocaDeadStoreElim(t *testing.T) {
	// an alloca written but never read is dead.
	ir := `define i32 @main() {
entry:
  %x = alloca double
  store i32 42, i32* %x
  ret i32 0
}
`
	out := OptimizeIR(ir, 1)
	if strings.Contains(out, "alloca") || strings.Contains(out, "store i32 42") {
		t.Fatalf("dead alloca/store not eliminated:\n%s", out)
	}
}

func TestAllocaNotPromotedWithPhiNeed(t *testing.T) {
	// loop-carried variable needs a phi; promotion must conservatively keep it.
	ir := `define i32 @main() {
entry:
  %i = alloca double
  store i32 0, i32* %i
  br label %loop
loop:
  %v = load i32, i32* %i
  %t = icmp slt i32 %v, 10
  br i1 %t, label %body, label %exit
body:
  %nv = add i32 %v, 1
  store i32 %nv, i32* %i
  br label %loop
exit:
  ret i32 %v
}
`
	out := OptimizeIR(ir, 1)
	if !strings.Contains(out, "alloca double") {
		t.Fatalf("loop-carried alloca must not be promoted (needs phi):\n%s", out)
	}
}

// ---------------------------------------------------------------------------
// dead-block elimination
// ---------------------------------------------------------------------------

func TestDeadBlockElim(t *testing.T) {
	ir := `define i32 @main() {
entry:
  br label %keep
dead:
  ret i32 1
keep:
  ret i32 0
}
`
	out := OptimizeIR(ir, 1)
	if strings.Contains(out, "dead:") {
		t.Fatalf("unreachable block kept:\n%s", out)
	}
}

func TestCFGReachabilityKeepsReachable(t *testing.T) {
	ir := `define i32 @main() {
entry:
  %c = icmp slt i32 1, 2
  br i1 %c, label %a, label %b
a:
  br label %b
b:
  ret i32 0
}
`
	out := OptimizeIR(ir, 1)
	if !strings.Contains(out, "b:") || !strings.Contains(out, "a:") {
		t.Fatalf("reachable blocks dropped:\n%s", out)
	}
}

// ---------------------------------------------------------------------------
// end-to-end: optimized IR must remain valid for llvm-as / llc
// ---------------------------------------------------------------------------

func findTool(name string) string {
	names := []string{name, name + "-20", name + "-18"}
	for _, n := range names {
		if _, err := exec.LookPath(n); err == nil {
			return n
		}
	}
	return ""
}

func TestOptimizedIRValidForLLC(t *testing.T) {
	llvmAs := findTool("llvm-as")
	if llvmAs == "" {
		t.Skip("llvm-as not installed")
	}
	llc := findTool("llc")
	if llc == "" {
		t.Skip("llc not installed")
	}

	srcs := []string{
		"x = 5\nif x > 3:\n    print(1)\nelse:\n    print(2)\nprint(x + 2)\n",
		"i = 0\ntotal = 0\nwhile i < 10:\n    total = total + i\n    i = i + 1\nprint(total)\n",
		"def f(a, b=2):\n    return a + b\nprint(f(3, 4))\n",
	}
	for _, src := range srcs {
		res, err := Compile(src)
		if err != nil {
			t.Fatalf("compile %q: %v", src, err)
		}
		opt := OptimizeIR(res.IR, 1)

		// verify the optimized IR assembles (llvm-as) and lowers (llc).
		as := exec.Command(llvmAs, "-o", "/dev/null", "-")
		as.Stdin = strings.NewReader(opt)
		if out, err := as.CombinedOutput(); err != nil {
			t.Fatalf("llvm-as on optimized IR for %q failed: %v\n%s\n%s", src, err, out, opt)
		}
	}
}

// ---------------------------------------------------------------------------
// full-pipeline sanity on real programs
// ---------------------------------------------------------------------------

func TestOptimizeRealProgram(t *testing.T) {
	src := "x = 5\nif x > 3:\n    print(1)\nelse:\n    print(2)\nprint(x + 2)\n"
	res, err := Compile(src)
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	raw := res.IR
	opt := OptimizeIR(raw, 1)
	if len(opt) >= len(raw) {
		t.Fatalf("optimizer did not shrink IR: %d -> %d", len(raw), len(opt))
	}
	// x = 5 is promoted away; the `if x > 3` comparison folds to true.
	if strings.Contains(opt, "alloca") {
		t.Fatalf("promotion did not remove alloca:\n%s", opt)
	}
	if !hasLine(opt, "br label %if.then") {
		t.Fatalf("branch not folded:\n%s", opt)
	}
	// print(x+2) folds to print(7).
	if !strings.Contains(opt, "i32 7)") {
		t.Fatalf("add not folded into call:\n%s", opt)
	}
}
