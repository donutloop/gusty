package integration

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/donutloop/gusty/pkg/lang"
)

// TestPipelineFloatVars simulates the CI build pipeline (`lang.Build`:
// codegen -> llc-20 -> cc -> executable) and then executes the produced
// binary, checking that float variables survive store/load across sqrt,
// floor-division, and modulo. This guards the float-variable alloca path
// exercised by the CLI build pipeline (`gustyc -build` / `make testllvmcode`).
func TestPipelineFloatVars(t *testing.T) {
	src := "x = 16.0\nprint(sqrt(x))\nprint(x // 2.0)\nprint(x % 3.0)\n"
	dir := t.TempDir()
	srcPath := filepath.Join(dir, "t.gy")
	exePath := filepath.Join(dir, "t.out")
	if err := os.WriteFile(srcPath, []byte(src), 0o600); err != nil {
		t.Fatalf("write source: %v", err)
	}
	if _, err := lang.Build([]string{srcPath}, exePath, 0); err != nil {
		t.Fatalf("pipeline build failed: %v", err)
	}
	cmd := exec.Command(exePath)
	cmd.Stderr = os.Stderr
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("pipeline run failed: %v", err)
	}
	want := "4\n8\n1\n"
	if got := string(out); got != want {
		t.Fatalf("pipeline output %q, want %q", got, want)
	}
}

func TestGeneratorFunctionsAndExpressions(t *testing.T) {
	src := `def g(n):
    yield n * 2
    yield n * 3
print(g(5))
print((x * 2 for x in range(4)))
print((x for x in [1, 2, 3] if x > 1))
print((x for x in range(6) if x % 2 == 0))
`
	dir := t.TempDir()
	srcPath := filepath.Join(dir, "gen.gy")
	if err := os.WriteFile(srcPath, []byte(src), 0o600); err != nil {
		t.Fatal(err)
	}
	exe := filepath.Join(dir, "prog")
	if _, err := lang.Build([]string{srcPath}, exe, 0); err != nil {
		t.Fatalf("build failed: %v", err)
	}
	out, err := exec.Command(exe).CombinedOutput()
	if err != nil {
		t.Fatalf("run failed: %v\n%s", err, out)
	}
	want := "[10, 15]\n[0, 2, 4, 6]\n[2, 3]\n[0, 2, 4]\n"
	if string(out) != want {
		t.Fatalf("output = %q, want %q", out, want)
	}
}
