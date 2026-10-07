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

// Gap R.18 (ADR 0212): division by zero is a *language event*. Before this the compiled
// backend emitted the instruction and kept walking — `print(1 / 0)` printed `inf`, and
// `print(7 % 0)` printed a different garbage number on every run with exit 0 — while the
// interpreter raised an error with a message and no exception class, which made the trap
// uncatchable: `except ZeroDivisionError:` matches on the class, and there wasn't one.

// zdRun runs src through the record, returning what it printed and the error.
// zdRun answers what a program printed and how it failed, the compiled program judged against
// the record the retired engine left behind (ADR 0302).
func zdRun(t *testing.T, src string) (string, error) {
	t.Helper()
	return runGoldenStdout(t, src)
}

// zdBuild runs src through the full compiled pipeline and runs the binary.
func zdBuild(t *testing.T, name, src string) (int, string, string) {
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
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	code := 0
	if ee, ok := err.(*exec.ExitError); ok {
		code = ee.ExitCode()
	} else if err != nil {
		t.Fatalf("run: %v", err)
	}
	return code, stdout.String(), stderr.String()
}

// The table CPython is the reference for: shape, expected class, expected message. The
// message is pinned too, because a trap whose wording drifts from the reference is a trap
// nobody can grep a codebase for — and because the record's old text ("division by zero"
// for a float floor division) shows how quietly the wording alone had gone wrong.
var zdCases = []struct {
	name    string
	src     string
	class   string
	message string
}{
	{"int division", "x = 1\nprint(x / 0)\n", "ZeroDivisionError", "division by zero"},
	{"int floor division", "x = 7\nprint(x // 0)\n", "ZeroDivisionError", "integer division or modulo by zero"},
	{"int modulo", "x = 7\nprint(x % 0)\n", "ZeroDivisionError", "integer modulo by zero"},
	{"float division", "print(1.0 / 0)\n", "ZeroDivisionError", "float division by zero"},
	{"float floor division", "print(7.0 // 0)\n", "ZeroDivisionError", "float floor division by zero"},
	{"float modulo", "print(7.0 % 0)\n", "ZeroDivisionError", "float modulo"},
	{"divisor computed at run time", "def z():\n    return 0\n\nx = 5\nprint(x / z())\n", "ZeroDivisionError", "division by zero"},
}

func TestDivisionByZeroRaisesATypedExceptionOnTheCompiledBackend(t *testing.T) {
	for _, tc := range zdCases {
		t.Run(tc.name, func(t *testing.T) {
			out, err := zdRun(t, tc.src)
			if err == nil {
				t.Fatalf("the interpreter accepted a division by zero; it printed %q", out)
			}
			var evalErr *TrapError
			if !errors.As(err, &evalErr) {
				t.Fatalf("want an *TrapError, got %T: %v", err, err)
			}
			if evalErr.ExnType != tc.class {
				t.Errorf("class = %q, want %q — an untyped error cannot be caught by `except %s:`",
					evalErr.ExnType, tc.class, tc.class)
			}
			if evalErr.ExnMsg != tc.message {
				t.Errorf("message = %q, want %q (CPython's wording for this operation)", evalErr.ExnMsg, tc.message)
			}
			if out != "" {
				t.Errorf("stdout must hold only what the program printed, got %q", out)
			}
		})
	}
}

// The compiled half, and the half that was worst: an unguarded `srem` does not fault on
// AArch64, so `print(7 % 0)` printed a garbage number and exited 0. A trap that produces
// output is not a trap.
func TestDivisionByZeroTrapsInTheCompiledBackend(t *testing.T) {
	for _, tc := range zdCases {
		t.Run(tc.name, func(t *testing.T) {
			code, stdout, stderr := zdBuild(t, "zdiv_"+strings.ReplaceAll(tc.name, " ", "_"), tc.src)
			if code == 0 {
				t.Fatalf("the compiled program accepted a division by zero: exit 0, stdout %q", stdout)
			}
			if stdout != "" {
				t.Errorf("a trapping program printed %q; the value must never reach stdout (the measured bug was `inf` and garbage integers)", stdout)
			}
			if !strings.Contains(stderr, tc.class) {
				t.Errorf("stderr does not name %s: %q", tc.class, stderr)
			}
			if !strings.Contains(stderr, tc.message) {
				t.Errorf("stderr does not carry CPython's wording %q: %q", tc.message, stderr)
			}
		})
	}
}

// A trap is only a trap if the language can handle it. This is the requirement the untyped
// errors failed: the handler must run, and a handler for the *wrong* class must not.
func TestDivisionByZeroIsCatchableOnTheCompiledBackend(t *testing.T) {
	src := "x = 3\ntry:\n    print(x / 0)\nexcept ZeroDivisionError:\n    print(\"handled\")\n"
	if out, err := zdRun(t, src); err != nil || out != "handled\n" {
		t.Fatalf("the record leg: got %q (err %v), want \"handled\\n\"", out, err)
	}
	// A handler for the wrong class must not swallow it: the exception has to keep going,
	// not be collected by the first arm it meets.
	miss := "x = 3\ntry:\n    print(x % 0)\nexcept ValueError:\n    print(\"wrong\")\n"
	out, err := zdRun(t, miss)
	if err == nil {
		t.Fatalf("a ValueError arm swallowed a ZeroDivisionError; stdout was %q", out)
	}
	var evalErr *TrapError
	if !errors.As(err, &evalErr) || evalErr.ExnType != "ZeroDivisionError" {
		t.Fatalf("want the ZeroDivisionError to propagate, got %v (%T)", err, err)
	}
	if strings.Contains(out, "wrong") {
		t.Errorf("the wrong arm ran: %q", out)
	}
}

func TestDivisionByZeroIsCatchableWhenCompiled(t *testing.T) {
	src := "x = 3\ntry:\n    print(x / 0)\nexcept ZeroDivisionError:\n    print(\"handled\")\n"
	code, stdout, stderr := zdBuild(t, "zero_div_caught", src)
	if code != 0 || stdout != "handled\n" {
		t.Fatalf("compiled: exit %d stdout %q stderr %q, want exit 0 and \"handled\\n\"", code, stdout, stderr)
	}
}

// The IR-level statement of the rule: an arithmetic division in the module is only legal if
// the raise machinery is present. (The check is on the emitted text because the property is
// about what reaches llc: an instruction with no guard in front of it is the bug.)
func TestEmittedDivisionCarriesTheGuard(t *testing.T) {
	src := "def half(n):\n    return n % 3\n\nprint(half(7))\nprint(7 % 0)\n"
	res, err := Compile(src)
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}
	if !strings.Contains(res.IR, "srem") {
		t.Fatalf("no srem in the module, so this test has stopped testing anything:\n%s", res.IR)
	}
	if !strings.Contains(res.IR, "ZeroDivisionError") {
		t.Fatalf("the module divides without naming ZeroDivisionError anywhere:\n%s", res.IR)
	}
	if vr, err := VerifyModuleIR(res.IR, 0); err != nil {
		t.Fatalf("verify: %v", err)
	} else if !vr.OK && !vr.Skipped {
		t.Fatalf("the guarded module does not verify: %v", vr.Errors)
	}
}

// Floating division by zero yields ±inf to LLVM, so if the emitter does not test, the program
// prints `inf` — the exact output that was measured before the fix.
func TestFloatDivisionByZeroDoesNotPrintInfinity(t *testing.T) {
	code, stdout, _ := zdBuild(t, "zero_div_float", "x = 2\nprint(x / 0.0)\n")
	if strings.Contains(stdout, "inf") {
		t.Fatalf("the compiled program printed infinity: stdout %q", stdout)
	}
	if code == 0 {
		t.Fatalf("a float division by zero exited 0 with stdout %q", stdout)
	}
}
