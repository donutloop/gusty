package lang

import (
	"strings"
	"testing"
)

func TestVerifyModuleIRAcceptsGeneratedModule(t *testing.T) {
	res, err := Compile("s = 0\nfor i in range(10):\n    s = s + i\nprint(s)\n")
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}
	ver, verr := VerifyModuleIR(res.IR, 0)
	if verr != nil {
		t.Fatalf("verifier rejected a good module: %v\n%s", verr, res.IR)
	}
	if !ver.OK || ver.Skipped {
		t.Fatalf("verdict = %+v, want ok", ver)
	}
	if ver.Tool == "" || len(ver.Pipeline) == 0 {
		t.Errorf("verdict must name the tool and the pipeline that ran: %+v", ver)
	}
	if !strings.Contains(ver.Toolchain, PinnedLLVMVersion) {
		t.Errorf("toolchain = %q, want it to name LLVM %s", ver.Toolchain, PinnedLLVMVersion)
	}
}

func TestVerifyModuleIROptLevels(t *testing.T) {
	res, err := Compile("def f(x) -> int:\n    return x * 3\n\nprint(f(14))\n")
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}
	for _, lvl := range []int{0, 1, 2, 3} {
		ver, verr := VerifyModuleIR(res.IR, lvl)
		if verr != nil {
			t.Fatalf("opt level %d rejected the module: %v", lvl, verr)
		}
		if !ver.OK {
			t.Errorf("opt level %d: not ok: %+v", lvl, ver)
		}
	}
}

// TestVerifyModuleIRRejectsBrokenModule is the point of L8.2: a module that only
// *looks* like IR must be caught by the verifier, with the verifier's own words
// in the result. This is the shape of the two codegen bugs found this session
// (a global passed where an i32 handle is required).
func TestVerifyModuleIRRejectsBrokenModule(t *testing.T) {
	broken := `@.str1 = private global [3 x i8] c"hi\00"
define internal void @rt_set_elem(i32 %h, i32 %i, i32 %v) {
  ret void
}
define i32 @main() {
entry:
  call void @rt_set_elem(i32 1, i32 0, i32 @.str1)
  ret i32 0
}
`
	ver, err := VerifyModuleIR(broken, 0)
	if err == nil {
		t.Fatalf("expected the verifier to reject the module, got %+v", ver)
	}
	if ver.OK {
		t.Errorf("OK must be false for a rejected module")
	}
	if ver.Skipped {
		t.Errorf("a rejection must never be reported as skipped")
	}
	if len(ver.Errors) == 0 {
		t.Errorf("expected the verifier's diagnostics in the result")
	}
	if !strings.Contains(ver.Note, "compiler bug") {
		t.Errorf("the note must say whose bug this is: %q", ver.Note)
	}
}

func TestVerifyModuleIREmptyModule(t *testing.T) {
	ver, err := VerifyModuleIR("   ", 0)
	if err != nil {
		t.Fatalf("empty module should not error: %v", err)
	}
	if !ver.Skipped || ver.OK {
		t.Errorf("empty module verdict = %+v, want skipped", ver)
	}
}

// TestVerifyModuleIRSkipsWithoutToolchain: no toolchain is a *known-unknown*,
// never a pass — an unverified build must not look verified.
func TestVerifyModuleIRSkipsWithoutToolchain(t *testing.T) {
	oldOpt, oldLLC := optCmd, llcCmd
	defer func() { optCmd, llcCmd = oldOpt, oldLLC }()
	optCmd, llcCmd = "gusty-no-such-tool", "gusty-no-such-tool-either"

	res, err := Compile("print(1)")
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}
	ver, verr := VerifyModuleIR(res.IR, 0)
	if verr != nil {
		t.Fatalf("missing toolchain must not fail the build: %v", verr)
	}
	if !ver.Skipped || ver.OK {
		t.Fatalf("verdict = %+v, want skipped (and not ok)", ver)
	}
	if !strings.Contains(ver.Note, "not verified") {
		t.Errorf("skipped verdict must say the module was not verified: %q", ver.Note)
	}
}

func TestBuildReportsVerification(t *testing.T) {
	dir := t.TempDir()
	src := dir + "/p.gy"
	writeFile(t, src, "def add(a, b) -> int:\n    return a + b\n\nprint(add(2, 3))\n")
	bin := dir + "/p"
	res, err := BuildWithOptions([]string{src}, bin, 1, &BuildOptions{})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if res.Verification == nil {
		t.Fatalf("build must report the verifier verdict (L8.2)")
	}
	if !res.Verification.OK {
		t.Errorf("verdict = %+v, want ok", res.Verification)
	}

	res2, err := BuildWithOptions([]string{src}, bin, 1, &BuildOptions{NoVerify: true})
	if err != nil {
		t.Fatalf("Build (NoVerify): %v", err)
	}
	if res2.Verification != nil {
		t.Errorf("--no-verify must skip the stage entirely, got %+v", res2.Verification)
	}
}

// TestEveryGeneratedModuleVerifies is the L8.2 contract in one place: modules
// produced across the surface of the language all pass LLVM's verifier.
func TestEveryGeneratedModuleVerifies(t *testing.T) {
	progs := []string{
		"print(1)",
		"x = [1, 2, 3]\nprint(len(x))",
		"def f(a, b=2):\n    return a * b\n\nprint(f(3))",
		"class A:\n    def __init__(self, x):\n        self.x = x\n\n    def get(self):\n        return self.x\n\nprint(A(4).get())",
		"def g(n):\n    i = 0\n    while i < n:\n        yield i\n        i = i + 1\n\nfor y in g(3):\n    print(y)",
		"try:\n    x = 1 / 0\nexcept:\n    print(0)",
		"ys = [x * 2 for x in [1, 2]]\nprint(ys)",
		"def total(xs) -> int:\n    t = 0\n    for x in xs:\n        t = t + x\n    return t\n\nprint(total([1, 2, 3]))",
		"m = {1: 2}\nprint(len(m))",
		"s = {1, 2}\nprint(len(s))",
		"match 2:\n    case 1:\n        print(1)\n    case _:\n        print(9)",
	}
	for _, p := range progs {
		res, err := Compile(p)
		if err != nil {
			t.Errorf("Compile(%q): %v", p, err)
			continue
		}
		ver, verr := VerifyModuleIR(res.IR, 0)
		if verr != nil {
			t.Errorf("module for %q failed verification: %v (%+v)\n%s", p, verr, ver, res.IR)
		}
	}
}
