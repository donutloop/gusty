package lang

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// writeTemp writes src to dir/name and returns the path.
func writeTemp(t *testing.T, dir, name, src string) string {
	t.Helper()
	p := filepath.Join(dir, name)
	if err := os.WriteFile(p, []byte(src), 0o600); err != nil {
		t.Fatalf("write %s: %v", p, err)
	}
	return p
}

// TestBuildProducesExecutable exercises Build end-to-end: two source files are
// merged, analyzed, codegen'd, lowered with llc, and linked with cc into a
// native binary that actually runs.
func TestBuildProducesExecutable(t *testing.T) {
	dir := t.TempDir()
	a := writeTemp(t, dir, "a.gy", "def double(x):\n    return x * 2\n")
	b := writeTemp(t, dir, "b.gy", "s = 0\nfor i in range(1, 4):\n    s = s + i\nprint(s + double(5))\n")
	out := filepath.Join(dir, "prog")

	res, err := Build([]string{a, b}, out, 0)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if res.Output != out {
		t.Errorf("output = %q, want %q", res.Output, out)
	}
	if len(res.Objects) != 1 {
		t.Errorf("objects = %v, want 1 intermediate object", res.Objects)
	}
	if len(res.Commands) != 2 {
		t.Errorf("commands = %v, want llc + cc steps", res.Commands)
	}
	if res.IR == "" {
		t.Errorf("IR not captured")
	}
	if !strings.Contains(res.IR, "define i32 @main") {
		t.Errorf("IR missing main entry:\n%s", res.IR)
	}

	// The produced binary is a real, runnable native executable.
	got, err := exec.Command(out).Output()
	if err != nil {
		t.Fatalf("run %s: %v", out, err)
	}
	if string(got) != "16\n" {
		t.Errorf("program output = %q, want 16 (sum 1..3 + double(5))", got)
	}
}

// TestBuildReportsDiagnostics verifies a semantic error in any source file
// surfaces as structured diagnostics on the (partial) BuildResult instead of
// producing a binary.
func TestBuildReportsDiagnostics(t *testing.T) {
	dir := t.TempDir()
	bad := writeTemp(t, dir, "bad.gy", "x = nope + 1\nprint(x)\n")
	out := filepath.Join(dir, "prog")

	res, err := Build([]string{bad}, out, 0)
	if err == nil {
		t.Fatalf("Build should fail on undefined name")
	}
	if res == nil {
		t.Fatalf("partial BuildResult expected on diagnostics")
	}
	if len(res.Diagnostics) == 0 {
		t.Fatalf("diagnostics empty: %+v", res)
	}
	if _, statErr := os.Stat(out); !os.IsNotExist(statErr) {
		t.Errorf("no binary should be produced on error")
	}
}

// TestBuildMissingFile verifies a non-existent source file fails with a clear
// error before any toolchain step runs.
func TestBuildMissingFile(t *testing.T) {
	dir := t.TempDir()
	out := filepath.Join(dir, "prog")
	if _, err := Build([]string{filepath.Join(dir, "nope.gy")}, out, 0); err == nil {
		t.Fatalf("Build should fail for missing source file")
	}
}
