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
