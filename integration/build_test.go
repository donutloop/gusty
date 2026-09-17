package integration

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// buildCLI compiles the actual gustyc binary once per test binary and returns
// its path. This lets the whole-program tests drive the real CLI command
// rather than calling library functions in-process.
func buildCLI(t *testing.T, bin string) {
	t.Helper()
	cmd := exec.Command("go", "build", "-o", bin, "github.com/donutloop/gusty/cmd/gustyc")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("go build gustyc: %v\n%s", err, out)
	}
}

// writeSrc writes a gusty source file and returns its path.
func writeSrc(t *testing.T, dir, name, src string) string {
	t.Helper()
	p := filepath.Join(dir, name)
	if err := os.WriteFile(p, []byte(src), 0o600); err != nil {
		t.Fatalf("write %s: %v", p, err)
	}
	return p
}

// TestCLIBuildWholeProgram drives the real `gustyc --build <out> <files...>`
// command end-to-end (the whole program): CLI arg parsing -> read/merge sources
// -> semantic analysis -> LLVM codegen -> llc (module verification) -> cc
// (link) -> a runnable native binary. It then executes that binary and checks
// its stdout, exactly as a user would.
func TestCLIBuildWholeProgram(t *testing.T) {
	dir := t.TempDir()
	bin := filepath.Join(dir, "gustyc")
	buildCLI(t, bin)

	a := writeSrc(t, dir, "a.gy", "def double(x):\n    return x * 2\n")
	b := writeSrc(t, dir, "b.gy", "s = 0\nfor i in range(1, 4):\n    s = s + i\nprint(s + double(5))\n")
	out := filepath.Join(dir, "prog")

	// --build <out> with the sources as positional args.
	build := exec.Command(bin, "--build", out, a, b)
	buildOut, err := build.CombinedOutput()
	if err != nil {
		t.Fatalf("gustyc --build: %v\n%s", err, buildOut)
	}
	if _, statErr := os.Stat(out); os.IsNotExist(statErr) {
		t.Fatalf("no binary produced at %s\n%s", out, buildOut)
	}

	// The produced binary is a real native executable.
	run := exec.Command(out)
	got, err := run.Output()
	if err != nil {
		t.Fatalf("run built binary: %v", err)
	}
	if string(got) != "16\n" {
		t.Errorf("built binary output = %q, want 16 (sum 1..3 + double(5))", got)
	}

	// The CLI's JSON mode emits a machine-readable build result.
	js := exec.Command(bin, "--json", "--build", out, a, b)
	jsOut, err := js.Output()
	if err != nil {
		t.Fatalf("gustyc --json --build: %v\n%s", err, jsOut)
	}
	var res map[string]any
	if err := json.Unmarshal(jsOut, &res); err != nil {
		t.Fatalf("json build result not parseable: %v\n%s", err, jsOut)
	}
	if res["output"] != out {
		t.Errorf("json output = %v, want %q", res["output"], out)
	}
	if res["ir"] == "" {
		t.Errorf("json ir missing")
	}
}

// TestCLIBuildDiagnostics verifies the whole CLI surfaces a compile error in
// any source file and refuses to produce a binary.
func TestCLIBuildDiagnostics(t *testing.T) {
	dir := t.TempDir()
	bin := filepath.Join(dir, "gustyc")
	buildCLI(t, bin)

	bad := writeSrc(t, dir, "bad.gy", "x = nope + 1\nprint(x)\n")
	out := filepath.Join(dir, "prog")

	build := exec.Command(bin, "--build", out, bad)
	outb, err := build.CombinedOutput()
	if err == nil {
		t.Fatalf("gustyc --build should fail on undefined name; got success\n%s", outb)
	}
	if _, statErr := os.Stat(out); !os.IsNotExist(statErr) {
		t.Errorf("no binary should be produced on error")
	}
}

// TestCLIBuildRequiresSource verifies the usage error when --build has no
// positional source files.
func TestCLIBuildRequiresSource(t *testing.T) {
	dir := t.TempDir()
	bin := filepath.Join(dir, "gustyc")
	buildCLI(t, bin)

	build := exec.Command(bin, "--build", filepath.Join(dir, "prog"))
	if err := build.Run(); err == nil {
		t.Fatalf("gustyc --build with no sources should exit non-zero")
	}
}
