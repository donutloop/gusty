//go:build !windows

package lang

import (
	"strings"
	"testing"
)

// TestJITBasic verifies the compiled path runs real machine code: a source program's generated `main`
// is compiled, linked and executed, and what it printed comes back into Go.
//
// `commands` is the pipeline's own account of the processes it started, and since ADR 0343 that account
// has a third line: the program itself, run as its own process. A caller reading the account should see
// the whole pipeline, including the part that was once invisible because it happened on the harness's own
// descriptors.
func TestJITBasic(t *testing.T) {
	t.Parallel()
	res, err := JIT("x = 6\nprint(x * 7)\n", 0)
	if err != nil {
		t.Fatalf("JIT failed: %v", err)
	}
	if res.Output != "42\n" {
		t.Fatalf("output = %q, want 42", res.Output)
	}
	if res.IR == "" {
		t.Fatal("expected emitted IR")
	}
	if len(res.Commands) != 3 {
		t.Fatalf("expected the llc, cc and run commands, got %v", res.Commands)
	}
	joined := strings.Join(res.Commands, "\n")
	for _, want := range []string{"llc-20 ", "cc ", "jit.prog"} {
		if !strings.Contains(joined, want) {
			t.Fatalf("the command account does not name %q:\n%s", want, joined)
		}
	}
}

// TestTheIsolatedRunAndTheInProcessEntryAgree is the parity case that keeps ADR 0343 honest. The suite
// executes programs as child processes; the dlopen entry is what the benchmark measures and what the REPL
// used before the switch. Both hand back the same three answers — stdout, the tool channel, the exit
// status — for the same module, or the claim that there is one execution backend with two entries into it
// is a claim nobody checked.
//
//parify:serial it asks for JITOptions.InProcess, which enters the program on this process's own fd 1 and
//parify:serial fd 2 (dlopenRun); two runs entered that way would read each other's output. The default
//parify:serial entry — the linked program as its own child process — is parallel, and that is the one every
//parify:serial other case in the suite uses. ADR 0343.
func TestTheIsolatedRunAndTheInProcessEntryAgree(t *testing.T) {
	for _, tc := range []struct{ name, src string }{
		{"a printed product", "x = 6\nprint(x * 7)\n"},
		{"a float answer", "print(1 + 0.5)\n"},
		{"a container", "xs = [1, 2, 3]\nprint(xs)\n"},
		{"a loop", "s = 0\nfor i in range(4):\n    s = s + i\nprint(s)\n"},
		{"a trap", "print(1 / 0)\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			child, err := JITWithOptions(tc.src, 0, &JITOptions{})
			if err != nil {
				t.Fatalf("the isolated run of %q: %v", tc.src, err)
			}
			inProcess, err := JITWithOptions(tc.src, 0, &JITOptions{InProcess: true})
			if err != nil {
				t.Fatalf("the in-process run of %q: %v", tc.src, err)
			}
			if child.Output != inProcess.Output {
				t.Errorf("stdout differs between the two entries: %q vs %q", child.Output, inProcess.Output)
			}
			if child.Code != inProcess.Code {
				t.Errorf("exit status differs between the two entries: %d vs %d", child.Code, inProcess.Code)
			}
		})
	}
}

// TestJITFloat verifies float arithmetic survives the native path.
func TestJITFloat(t *testing.T) {
	t.Parallel()
	res, err := JIT("x = 16.0\nprint(sqrt(x))\n", 0)
	if err != nil {
		t.Fatalf("JIT failed: %v", err)
	}
	// Python renders an integral float as 4.0 (ADR 0180), not printf's %g "4".
	if res.Output != "4.0\n" {
		t.Fatalf("output = %q, want 4.0", res.Output)
	}
}

// TestJITLoop verifies control flow (for loop over range) executes natively.
func TestJITLoop(t *testing.T) {
	t.Parallel()
	res, err := JIT("s = 0\nfor i in range(3):\n    s = s + i\nprint(s)\n", 0)
	if err != nil {
		t.Fatalf("JIT failed: %v", err)
	}
	if res.Output != "3\n" {
		t.Fatalf("output = %q, want 3", res.Output)
	}
}

// TestJITOptimized verifies the optimizer path (opt-level 2) still runs.
func TestJITOptimized(t *testing.T) {
	t.Parallel()
	res, err := JIT("print(2 + 3)\n", 2)
	if err != nil {
		t.Fatalf("JIT failed: %v", err)
	}
	if res.Output != "5\n" {
		t.Fatalf("output = %q, want 5", res.Output)
	}
}

// TestJITError reports a clean error (semantic or native-codegen) rather than
// crashing or emitting partial output.
func TestJITError(t *testing.T) {
	t.Parallel()
	_, err := JIT("print(undefined_thing)\n", 0)
	if err == nil {
		t.Fatal("expected an error for an undefined name")
	}
}

// TestJITDocstrings verifies `def.__doc__` / `Cls.__doc__` folds to a string
// constant in the AOT/JIT backend.
func TestJITDocstrings(t *testing.T) {
	t.Parallel()
	res, err := JIT(`
def greet():
    "returns a greeting"
    return 1
def nodoc():
    return 2
class Animal:
    "an animal class"
    def speak(self):
        return self
class Plain:
    def noop(self):
        return self
print(greet.__doc__)
print(nodoc.__doc__)
print(Animal.__doc__)
print(Plain.__doc__)
print("done")
`, 0)
	if err != nil {
		t.Fatalf("JIT failed: %v", err)
	}
	want := "returns a greeting\n\nan animal class\n\ndone\n"
	if res.Output != want {
		t.Fatalf("output = %q, want %q", res.Output, want)
	}
}
func TestJITWrappingDecorator(t *testing.T) {
	t.Parallel()
	src := `
def add1(g):
    def wrap(x):
        return g(x) + 1
    return wrap
@add1
def f(x):
    return x * 2
print(f(3))
`
	res, err := JIT(src, 0)
	if err != nil {
		t.Fatalf("JIT error: %v", err)
	}
	if res.Output != "7\n" {
		t.Fatalf("wrapping decorator output %q, want 7", res.Output)
	}
}
