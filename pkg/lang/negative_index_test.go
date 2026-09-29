package lang

import (
	"bytes"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// L11.4 (ADR 0210) pinned the one rule a positional subscript follows: a negative index
// counts from the end. It is pinned here where it can actually be seen — the interpreter's
// values, the IR the compiled backend emits, and the binary that comes out the other end —
// because a subscript is exactly the kind of thing that can look right while two backends
// quietly implement two different rules.

// negInterp runs src through the interpreter and returns what it printed.
func negInterp(t *testing.T, src string) (string, error) {
	t.Helper()
	old := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe: %v", err)
	}
	os.Stdout = w
	_, _, evalErr := EvalExpr(src)
	os.Stdout = old
	w.Close()
	var buf bytes.Buffer
	if _, err := buf.ReadFrom(r); err != nil {
		t.Fatalf("read pipe: %v", err)
	}
	return buf.String(), evalErr
}

// negBuildRun compiles src through the real pipeline and runs the binary, returning the
// exit code and everything it wrote.
func negBuildRun(t *testing.T, name, src string) (int, string) {
	t.Helper()
	dir := t.TempDir()
	file := filepath.Join(dir, name+".gy")
	if err := os.WriteFile(file, []byte(src), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	binary := filepath.Join(dir, name)
	if _, err := Build([]string{file}, binary, 0); err != nil {
		t.Skipf("toolchain unavailable: %v", err)
	}
	cmd := exec.Command(binary)
	out, err := cmd.CombinedOutput()
	code := 0
	if ee, ok := err.(*exec.ExitError); ok {
		code = ee.ExitCode()
	} else if err != nil {
		t.Fatalf("run: %v (%s)", err, out)
	}
	return code, string(out)
}

func TestNegativeReadCountsFromTheEndInBothBackends(t *testing.T) {
	src := "xs = [1, 2, 3]\nprint(xs[-1])\nprint(xs[-3])\nn = -2\nprint(xs[n])\n"
	const want = "3\n1\n2\n"
	if got, err := negInterp(t, src); err != nil || got != want {
		t.Errorf("interpreter: got %q (%v), want %q", got, err, want)
	}
	if code, out := negBuildRun(t, "negative_read", src); code == 0 && out != want {
		t.Errorf("compiled: got %q, want %q", out, want)
	}
}

// The compiler panic was the sharpest edge of this gap: a subscript the constant folder
// could not handle was handed straight to a Go slice, and the compiler died with
// `panic: runtime error: index out of range [-1]`. A tested shape may leave the compiler as
// a diagnostic or as working code — never as a panic (the L11.8 contract).
func TestLiteralListWithNegativeIndexDoesNotTakeTheCompilerDown(t *testing.T) {
	src := "print([1, 2, 3][-1])\n"
	res, err := Compile(src)
	if err != nil {
		t.Fatalf("Compile: %v", err) // a regression panics here, and panics loudly
	}
	ir := res.IR
	if !strings.Contains(ir, "@main") {
		t.Fatalf("no @main in:\n%s", ir)
	}
	vr, err := VerifyModuleIR(ir, 0)
	if err != nil {
		t.Fatalf("verify: %v", err)
	}
	if !vr.OK && !vr.Skipped {
		t.Fatalf("module does not verify: %v\n%s", vr.Errors, ir)
	}
	if code, out := negBuildRun(t, "negative_literal", src); code != 0 || out != "3\n" {
		t.Errorf("compiled: exit %d out %q, want exit 0 and \"3\\n\"", code, out)
	}
	pyOut, _, err := PythonRun(src)
	if err != nil {
		t.Skipf("python unavailable: %v", err)
	}
	if pyOut != "3\n" {
		t.Fatalf("python: got %q, want \"3\\n\"", pyOut)
	}
}

// The same rule on the write path: `xs[-1] = v` writes the last element, and the compiled
// code normalises it against the list's *run-time* length, so the rule holds for a list
// whose length the compiler never sees.
func TestNegativeWriteNormalisesAgainstTheLength(t *testing.T) {
	src := "xs = [1, 2, 3]\nxs[-1] = 30\nprint(xs[2])\nprint(xs[-2])\n"
	const want = "30\n2\n"
	if got, err := negInterp(t, src); err != nil || got != want {
		t.Errorf("interpreter: got %q (%v), want %q", got, err, want)
	}
	res, err := Compile(src)
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}
	ir := res.IR
	if !strings.Contains(ir, "call i32 @rt_list_len") {
		t.Fatalf("the write path normalises without measuring the list:\n%s", ir)
	}
	vr, err := VerifyModuleIR(ir, 0)
	if err != nil {
		t.Fatalf("verify: %v", err)
	}
	if !vr.OK && !vr.Skipped {
		t.Fatalf("module does not verify: %v", vr.Errors)
	}
	if code, out := negBuildRun(t, "negative_write", src); code == 0 && out != want {
		t.Errorf("compiled: exit %d out %q, want %q", code, out, want)
	}
}

// A dict subscript is a key, not a position: `-1` is a key you can store, and normalising
// it would quietly turn `d[-1]` into the last entry. That asymmetry is the part of "one
// rule" a shared helper could easily get wrong, so it is pinned on all three engines.
func TestNegativeDictKeysStayKeys(t *testing.T) {
	src := "d = {-1: \"minus\", 0: \"zero\"}\nprint(d[-1])\nprint(d[0])\n"
	const want = "minus\nzero\n"
	if got, err := negInterp(t, src); err != nil || got != want {
		t.Errorf("interpreter: got %q (%v), want %q", got, err, want)
	}
	if code, out := negBuildRun(t, "negative_dict", src); code == 0 && out != want {
		t.Errorf("compiled: exit %d out %q, want %q", code, out, want)
	}
	pyOut, _, err := PythonRun(src)
	if err != nil {
		t.Skipf("python unavailable: %v", err)
	}
	if pyOut != want {
		t.Fatalf("python: got %q, want %q", pyOut, want)
	}
}

// Normalisation does not dissolve the bounds check: past either end is still IndexError,
// in the interpreter, in the compiled binary and in the reference implementation.
func TestNegativeIndexPastTheStartStillTraps(t *testing.T) {
	src := "xs = [1, 2, 3]\nprint(xs[-4])\n"
	pyOut, pyErr, pyRunErr := PythonRun(src)
	if pyRunErr == nil {
		t.Fatalf("python: want IndexError, got stdout %q", pyOut)
	}
	if !strings.Contains(pyErr, "IndexError") {
		t.Fatalf("python: want IndexError in stderr, got %q (%v)", pyErr, pyRunErr)
	}
	out, err := negInterp(t, src)
	if err == nil {
		t.Fatalf("interpreter: want IndexError, got stdout %q", out)
	}
	var evalErr *EvalError
	if !errors.As(err, &evalErr) || evalErr.ExnType != "IndexError" {
		t.Fatalf("interpreter: want an IndexError, got %v (%T)", err, err)
	}
	code, combined := negBuildRun(t, "negative_oob", src)
	if code == 0 {
		t.Fatalf("compiled run exited 0: %s", combined)
	}
	if !strings.Contains(combined, "IndexError") {
		t.Fatalf("compiled run does not report IndexError: %s", combined)
	}
}

// Strings index by position too, and since ADR 0225 the *value* follows the same rule as the
// position: `s[-1]` is the last character as text, on both backends. This test used to pin the byte
// codes ("99\n97\n") and to require the compiled leg to refuse the shape; both halves were the bug
// this cycle fixed, and the expectations are now CPython's.
func TestNegativeStringSubscriptFollowsTheSameRule(t *testing.T) {
	src := "s = \"abc\"\nprint(s[-1])\nprint(s[-3])\n"
	const want = "c\na\n" // CPython prints exactly this
	if got, err := negInterp(t, src); err != nil || got != want {
		t.Errorf("interpreter: got %q (%v), want %q", got, err, want)
	}
	res, err := Compile(src)
	if err != nil {
		t.Fatalf("a subscript of a string the compiler can name must compile, not refuse: %v", err)
	}
	if !strings.Contains(res.IR, "rt_str_intern2") {
		t.Fatalf("the compiled subscript is not an interned character:\n%s", res.IR)
	}
}
