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
