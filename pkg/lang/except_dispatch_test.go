package lang

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// Gap R.20 (ADR 0213): the arms of a `try` are a chain, and the compiled backend lowered only
// the first link. It also cleared the exception flag unconditionally, so an exception no arm
// matched was deleted — no handler ran, no traceback was printed, and the program exited 0.
// These pin the chain and, just as importantly, pin the *unhandled* case, because "the handler
// did not run" and "there was nothing to handle" must never look the same.

func edBuild(t *testing.T, name, src string) (int, string, string) {
	t.Helper()
	dir := t.TempDir()
	file := filepath.Join(dir, name+".gy")
	if err := os.WriteFile(file, []byte(src), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	binary := filepath.Join(dir, name)
	if _, err := Build([]string{file}, binary, 0); err != nil {
		t.Fatalf("build: %v", err)
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

// edRecord answers one dispatch case the way the suite answers a behavioural question now: the
// compiled program runs, and the retired engine's recorded answer judges it (ADR 0302).
func edInterp(t *testing.T, src string) (string, error) {
	t.Helper()
	return runGoldenStdout(t, src)
}
var edCases = []struct {
	name string
	src  string
	want string
}{
	{"matching arm first",
		"try:\n    xs = [1]\n    print(xs[5])\nexcept IndexError:\n    print(\"A\")\nexcept ValueError:\n    print(\"B\")\nprint(\"done\")\n",
		"A\ndone\n"},
	{"matching arm second",
		"try:\n    xs = [1]\n    print(xs[5])\nexcept KeyError:\n    print(\"A\")\nexcept IndexError:\n    print(\"B\")\nprint(\"done\")\n",
		"B\ndone\n"},
	{"matching arm third",
		"try:\n    xs = [1]\n    print(xs[5])\nexcept KeyError:\n    print(\"a\")\nexcept ValueError:\n    print(\"b\")\nexcept IndexError:\n    print(\"c\")\nprint(\"done\")\n",
		"c\ndone\n"},
	{"bare arm after a typed one",
		"try:\n    xs = [1]\n    print(xs[5])\nexcept KeyError:\n    print(\"A\")\nexcept:\n    print(\"bare\")\nprint(\"done\")\n",
		"bare\ndone\n"},
	{"except Exception catches the base",
		"try:\n    raise RuntimeError(\"x\")\nexcept Exception:\n    print(\"base\")\nprint(\"done\")\n",
		"base\ndone\n"},
	{"an inner try that does not match reaches the outer arm",
		"try:\n    try:\n        xs = [1]\n        print(xs[5])\n    except KeyError:\n        print(\"inner\")\nexcept IndexError:\n    print(\"outer\")\nprint(\"done\")\n",
		"outer\ndone\n"},
	{"first arm runs when it matches even if a later one would too",
		"try:\n    raise ValueError(\"v\")\nexcept ValueError:\n    print(\"first\")\nexcept:\n    print(\"bare\")\nprint(\"done\")\n",
		"first\ndone\n"},
}

func TestExceptArmsDispatchInOrderWhenCompiled(t *testing.T) {
	for _, tc := range edCases {
		t.Run(tc.name, func(t *testing.T) {
			code, stdout, stderr := edBuild(t, "arms_"+strings.ReplaceAll(tc.name, " ", "_"), tc.src)
			if code != 0 {
				t.Fatalf("exit %d (stdout %q stderr %q), want a handled program to exit 0", code, stdout, stderr)
			}
			if stdout != tc.want {
				t.Errorf("compiled stdout = %q, want %q", stdout, tc.want)
			}
		})
	}
}

// The record is the reference here, not a suspect: these are the same expectations, checked against
// the answer the retired engine recorded for each program. Pinning both sides is what stops the
// compiler from agreeing with itself — and with one backend, CPython's leg in the conformance matrix
// is what stops the record and the compiler from agreeing on the wrong answer.
func TestExceptArmsDispatchInOrderAgainstTheRecord(t *testing.T) {
	for _, tc := range edCases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := edInterp(t, tc.src)
			if err != nil {
				t.Fatalf("interpreter: %v (out %q)", err, got)
			}
			if got != tc.want {
				t.Errorf("interpreted stdout = %q, want %q", got, tc.want)
			}
		})
	}
}

// The worst case of the defect: an exception no arm handles was *cleared*. A program must not
// be able to lose an exception, so this asserts the trap fires, names its class, and prints
// nothing on stdout.
func TestAnUnhandledExceptionIsNotSwallowedWhenCompiled(t *testing.T) {
	src := "try:\n    xs = [1]\n    print(xs[5])\nexcept KeyError:\n    print(\"wrong arm\")\nprint(\"after\")\n"
	code, stdout, stderr := edBuild(t, "unhandled_propagates", src)
	if code == 0 {
		t.Fatalf("the unhandled IndexError disappeared: exit 0, stdout %q", stdout)
	}
	if stdout != "" {
		t.Errorf("a program whose exception escaped printed %q; nothing after the try may run", stdout)
	}
	if !strings.Contains(stderr, "IndexError") {
		t.Errorf("the report must name the class, got %q", stderr)
	}
}

// A raise inside a handler is a new exception in the enclosing scope — it must not be captured
// by the same try, and it must not vanish either.
func TestRaisingInsideAHandlerEscapesTheSameTry(t *testing.T) {
	src := "try:\n    xs = [1]\n    print(xs[5])\nexcept IndexError:\n    raise ValueError(\"from the handler\")\nprint(\"after\")\n"
	code, stdout, stderr := edBuild(t, "handler_raises", src)
	if code == 0 {
		t.Fatalf("the exception raised in the handler was swallowed: stdout %q", stdout)
	}
	if stdout != "" {
		t.Errorf("stdout %q, want empty — control left the program at the raise", stdout)
	}
	if !strings.Contains(stderr, "ValueError") {
		t.Errorf("the report should name ValueError, got %q", stderr)
	}
	if _, err := edInterp(t, src); err == nil || !strings.Contains(err.Error()+"", "from the handler") {
		t.Fatalf("interpreter: want the ValueError to propagate, got %v", err)
	}
}

// The IR-level statement, so a regression in the emitter is visible without running anything:
// two typed arms must produce two comparisons against @exn_code, not one.
func TestEveryArmGetsItsOwnTest(t *testing.T) {
	src := "try:\n    xs = [1]\n    print(xs[5])\nexcept KeyError:\n    print(\"a\")\nexcept IndexError:\n    print(\"b\")\nexcept ValueError:\n    print(\"c\")\n"
	res, err := Compile(src)
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}
	got := strings.Count(res.IR, "load i32, i32* @exn_code")
	if got < 3 {
		t.Fatalf("three typed arms emitted %d reads of @exn_code; the chain is being shortened:\n%s", got, res.IR)
	}
	vr, err := VerifyModuleIR(res.IR, 0)
	if err != nil {
		t.Fatalf("verify: %v", err)
	}
	if !vr.OK && !vr.Skipped {
		t.Fatalf("the dispatched module does not verify: %v", vr.Errors)
	}
}
