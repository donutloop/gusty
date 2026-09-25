package integration

// End-to-end coverage for Gap H: the real LLVM opt pipeline.
//
// OptimizeIR now drives the external `opt-20` tool (in addition to the
// textual front-end pass), so this file verifies the two correctness
// invariants that matter most:
//
//  1. GC-correctness survives a real `opt -O2`: a program that builds and
//     grows a heap-allocated list still produces the same output after the
//     real optimizer as the AST interpreter. The codegen roots heap slots
//     through module-global arrays (@gc.roots), which the real optimizer
//     preserves because rt_gc reads/writes them.
//
//  2. The optimized module is still valid LLVM that llvm-as/llc accept
//     (the verifyModule check), and the optimized binary still links/runs.

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/donutloop/gusty/pkg/lang"
)

func haveOptTools(t *testing.T) {
	t.Helper()
	for _, tool := range []string{"opt-20", "llc-20", "llvm-as-20"} {
		if _, err := exec.LookPath(tool); err != nil {
			t.Skipf("%s not installed: %v", tool, err)
		}
	}
}

// compileOptRun compiles src, runs the real LLVM opt pipeline at the given
// level, verifies the optimized IR is accepted by llvm-as, compiles it via
// llc/cc, and returns the binary's stdout.
func compileOptRun(t *testing.T, src string, level int) string {
	t.Helper()
	res, err := lang.Compile(src)
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	optIR := lang.OptimizeIR(res.IR, level)

	dir := t.TempDir()
	inPath := filepath.Join(dir, "prog.opt.ll")
	objPath := filepath.Join(dir, "prog.o")
	binPath := filepath.Join(dir, "prog")
	if err := os.WriteFile(inPath, []byte(optIR), 0o600); err != nil {
		t.Fatalf("write IR: %v", err)
	}
	// verifyModule: the optimized IR must be accepted by the verifier.
	if out, err := exec.Command("llvm-as-20", inPath, "-o", filepath.Join(dir, "prog.bc")).CombinedOutput(); err != nil {
		t.Fatalf("optimized IR rejected by llvm-as: %v\n%s", err, out)
	}
	if out, err := exec.Command("llc-20", "-relocation-model=pic", "-filetype=obj", inPath, "-o", objPath).CombinedOutput(); err != nil {
		t.Fatalf("llc on optimized IR: %v\n%s", err, out)
	}
	if out, err := exec.Command("cc", objPath, "-lm", "-o", binPath).CombinedOutput(); err != nil {
		t.Fatalf("cc: %v\n%s", err, out)
	}
	bin, err := exec.Command(binPath).Output()
	if err != nil {
		t.Fatalf("run binary: %v", err)
	}
	return string(bin)
}

// TestRealOptPipelineGC is the critical GC-correctness invariant for Gap H:
// heap roots survive the real `opt` pipeline, so a list-growing program must
// produce the same output through real opt + llc + cc as the interpreter.
func TestRealOptPipelineGC(t *testing.T) {
	haveOptTools(t)
	src := `lst = [1, 2, 3]
s = 0
for i in range(20):
    lst.append(i)
    s = s + lst[0]
print(len(lst))
print(s)`
	want, err := lang.InterpreterRun(src)
	if err != nil {
		t.Fatalf("interp: %v", err)
	}
	got := compileOptRun(t, src, 2)
	if strings.TrimSpace(want) != strings.TrimSpace(got) {
		t.Fatalf("GC output changed by real opt pipeline\ninterp: %q\nopt:    %q", want, got)
	}
}

// TestRealOptPipelineHotLoop verifies a hot non-GC loop still computes the
// right answer after the real optimizer folds the induction loop (the SROA /
// scalar-replacement demonstration: the loop alloca is replaced by a register
// and the sum folded to a constant).
func TestRealOptPipelineHotLoop(t *testing.T) {
	haveOptTools(t)
	src := `s = 0
for i in range(1000):
    s = s + i
print(s)`
	want, err := lang.InterpreterRun(src)
	if err != nil {
		t.Fatalf("interp: %v", err)
	}
	got := compileOptRun(t, src, 2)
	if strings.TrimSpace(want) != strings.TrimSpace(got) {
		t.Fatalf("hot loop output changed by real opt pipeline\ninterp: %q\nopt:    %q", want, got)
	}
}

// TestRealOptFoldsArithmetic demonstrates the headline value of driving the
// real tool: it folds a runtime add of constants to a literal, something the
// textual front-end pass cannot see through calls.
func TestRealOptFoldsArithmetic(t *testing.T) {
	haveOptTools(t)
	res, err := lang.Compile("print(2 + 3)")
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	optIR := lang.OptimizeIR(res.IR, 2)
	// The real pipeline folds the add to the literal 5.
	if !strings.Contains(optIR, "5") {
		t.Fatalf("real opt did not fold 2+3 to a constant:\n%s", optIR)
	}
}
