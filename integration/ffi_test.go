package integration

import (
	"os/exec"
	"strings"
	"testing"
)

// TestFFIBuild compiles an `extern fn` (C FFI) program end-to-end and runs it:
// extern abs calls the C stdlib abs(), and strlen reads a string literal.
func TestFFIBuild(t *testing.T) {
	bin := t.TempDir() + "/gustyc"
	buildCLI(t, bin)

	dir := t.TempDir()
	absSrc := writeSrc(t, dir, "abs.gy", "extern fn abs(x: int) -> int\nprint(abs(-5))\n")
	absBin := dir + "/abs"
	out, err := exec.Command(bin, "--build", absBin, absSrc).CombinedOutput()
	if err != nil {
		t.Fatalf("build abs: %v\n%s", err, out)
	}
	runOut, err := exec.Command(absBin).CombinedOutput()
	if err != nil {
		t.Fatalf("run abs: %v\n%s", err, runOut)
	}
	if !strings.Contains(string(runOut), "5") {
		t.Fatalf("abs(-5) output = %q, want 5", runOut)
	}

	// strlen: string-literal arg lowered to an i8* for the C call.
	strSrc := writeSrc(t, dir, "str.gy", "extern fn strlen(s: str) -> int\nprint(strlen(\"hello\"))\n")
	strBin := dir + "/str"
	out2, err := exec.Command(bin, "--build", strBin, strSrc).CombinedOutput()
	if err != nil {
		t.Fatalf("build strlen: %v\n%s", err, out2)
	}
	run2, err := exec.Command(strBin).CombinedOutput()
	if err != nil {
		t.Fatalf("run strlen: %v\n%s", err, run2)
	}
	if !strings.Contains(string(run2), "5") {
		t.Fatalf("strlen(hello) output = %q, want 5", run2)
	}
}
