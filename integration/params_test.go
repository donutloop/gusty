package integration

// A parameter is a local variable that starts out bound to an argument. The compiled
// backend used to resolve every reference to one from its incoming argument register,
// so the store an assignment performed went into a slot nothing ever read. `def bump(n):
// n = n + 1; return n` answered 0; an accumulator that decrements its argument looped
// forever, because the loop condition compared the argument. `for i in range(3)` shared
// the counter with the variable, so the variable answered 3 after the loop and writing
// to it in the body moved the iteration. Both were silent — the compiled binary produced
// a number, and only a number, forever (roadmap Gap R.3, ADR 0196).
//
// These are the shapes, run through both backends and CPython, with a timeout on the
// compiled leg: a hang is not a failing test, it is a test suite that never reports.

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/donutloop/gusty/pkg/lang"
)

// runAOTWithTimeout compiles and runs one program, refusing to wait forever for it.
// A loop that never terminates is the worst failure a compiler can produce — there is
// no output to compare and no error to read — so every leg here is bounded.
func runAOTWithTimeout(t *testing.T, src string, wait time.Duration) (string, error) {
	t.Helper()
	res, err := lang.Compile(src)
	if err != nil {
		return "", err
	}
	if res.IR == "" {
		t.Fatalf("compile %q: empty IR", src)
	}
	dir := t.TempDir()
	irPath := filepath.Join(dir, "prog.ll")
	objPath := filepath.Join(dir, "prog.o")
	binPath := filepath.Join(dir, "prog")
	if err := os.WriteFile(irPath, []byte(res.IR), 0o600); err != nil {
		t.Fatal(err)
	}
	if out, err := exec.Command(llc, "-filetype=obj", "-relocation-model=pic", irPath, "-o", objPath).CombinedOutput(); err != nil {
		t.Fatalf("llc rejected the module: %v\n%s\nIR:\n%s", err, out, res.IR)
	}
	if out, err := exec.Command("cc", objPath, "-lm", "-o", binPath).CombinedOutput(); err != nil {
		t.Fatalf("link failed: %v\n%s", err, out)
	}
	ctx, cancel := context.WithTimeout(context.Background(), wait)
	defer cancel()
	cmd := exec.CommandContext(ctx, binPath)
	out, err := cmd.CombinedOutput()
	if ctx.Err() == context.DeadlineExceeded {
		return string(out), errProgramHung
	}
	return string(out), err
}

// errProgramHung is the failure this round exists to prevent: a compiled program that
// never returns has no output to diff and no error to read.
var errProgramHung = errors.New("the compiled program did not terminate")

// TestReboundParameterShapesAgree is the class, shape by shape: what the interpreter
// prints is what the compiled binary must print, and where the shape is legal Python,
// what CPython prints is what both must print.
func TestReboundParameterShapesAgree(t *testing.T) {
	for _, tc := range reboundShapeCases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			interp, ierr := safeInterpreterRun(t, tc.src)
			if ierr != nil {
				t.Fatalf("interpreter: %v (out=%q)", ierr, interp)
			}
			if interp != tc.want {
				t.Errorf("interpreter printed %q, want %q", interp, tc.want)
			}
			aot, aerr := runAOTWithTimeout(t, tc.src, 90*time.Second)
			if aerr != nil {
				t.Fatalf("compiled leg: %v (out=%q)", aerr, aot)
			}
			if aot != tc.want {
				t.Errorf("compiled backend printed %q, interpreter printed %q — the two backends must answer the same program with the same answer", aot, interp)
			}
			if tc.python == "" {
				return
			}
			py, perr := safeOracleRun(t, tc.src)
			if perr != nil {
				t.Logf("no oracle for this shape: %v", perr)
				return
			}
			if py != tc.python {
				t.Errorf("CPython printed %q, want %q (both backends must be judged against this)", py, tc.python)
			}
		})
	}
}

var reboundShapeCases = []struct {
	name   string
	src    string
	want   string
	python string
}{
	{
		name:   "the store the read ignored",
		src:    "def bump(n):\n    n = n + 1\n    return n\nprint(bump(0))\n",
		want:   "1\n",
		python: "1\n",
	},
	{
		name:   "two rebinds, last one wins",
		src:    "def twice(n):\n    n = n * 2\n    n = n + 1\n    return n\nprint(twice(3))\n",
		want:   "7\n",
		python: "7\n",
	},
	{
		name:   "an accumulator over its own argument",
		src:    "def acc(n):\n    total = 0\n    while n > 0:\n        total = total + n\n        n = n - 1\n    return total\nprint(acc(4))\nprint(acc(0))\n",
		want:   "10\n0\n",
		python: "10\n0\n",
	},
	{
		name:   "a clamp on the way in",
		src:    "def clamp(x):\n    if x < 0:\n        x = 0\n    if x > 10:\n        x = 10\n    return x\nprint(clamp(-4))\nprint(clamp(7))\nprint(clamp(99))\n",
		want:   "0\n7\n10\n",
		python: "0\n7\n10\n",
	},
	{
		name:   "a loop that reuses the parameter, read after the loop",
		src:    "def count(n):\n    for n in range(3):\n        print(n)\n    return n\nprint(count(9))\n",
		want:   "0\n1\n2\n2\n",
		python: "0\n1\n2\n2\n",
	},
	{
		// Python binds the loop variable per element; writing to it does not move
		// the iteration, and the variable keeps the last value the loop bound.
		name:   "the body writes to its own loop variable",
		src:    "def squares(step):\n    for i in range(3):\n        i = i * 100\n        print(i)\n    return i\nprint(squares(1))\n",
		want:   "0\n100\n200\n200\n",
		python: "0\n100\n200\n200\n",
	},
	{
		name:   "a loop variable read after the loop",
		src:    "total = 0\nfor i in range(4):\n    total = total + i\nprint(total)\nprint(i)\n",
		want:   "6\n3\n",
		python: "6\n3\n",
	},
	{
		name:   "a method rebinds its parameter",
		src:    "class C:\n    def bumped(self, n):\n        n = n + 1\n        n = n * 2\n        return n\nprint(C().bumped(3))\n",
		want:   "8\n",
		python: "8\n",
	},
	{
		name:   "a nested def rebinds its own parameter",
		src:    "def outer(n):\n    def inner(m):\n        m = m * 3\n        return m\n    return inner(n) + n\nprint(outer(2))\n",
		want:   "8\n",
		python: "8\n",
	},
	{
		// A parameter rebound to a non-scalar was already handled, by a different
		// path — pinned so the fix cannot quietly become the only path that works.
		name:   "a parameter rebound to a container",
		src:    "def show(xs):\n    xs = [9, 9]\n    print(xs)\n    return len(xs)\nprint(show([1, 2, 3]))\n",
		want:   "[9, 9]\n2\n",
		python: "[9, 9]\n2\n",
	},
	{
		name:   "a parameter rebound to a string",
		src:    "def greet(name):\n    name = \"world\"\n    return name\nprint(greet(\"ada\"))\n",
		want:   "world\n",
		python: "world\n",
	},
}

// TestReboundParameterLoopTerminates is the hang guard, stated on its own so a
// regression says "it hung" rather than looking like a wrong answer. Compiled, this
// looped forever: the condition compared the incoming register, and the body's
// `n = n + 1` stored where nothing read.
func TestReboundParameterLoopTerminates(t *testing.T) {
	src := "def loop(n):\n    while n < 3:\n        n = n + 1\n    return n\nprint(loop(0))\n"
	out, err := runAOTWithTimeout(t, src, 60*time.Second)
	if err != nil {
		if strings.Contains(err.Error(), "did not terminate") {
			t.Fatalf("the compiled program hung: a loop whose condition reads a parameter the body reassigns never terminated (Gap R.3)")
		}
		t.Fatalf("compiled leg: %v (out=%q)", err, out)
	}
	if out != "3\n" {
		t.Errorf("compiled printed %q, want \"3\\n\"", out)
	}
}

// TestParamRebindProgramPinsTheLedger runs the corpus program that carries this class
// and pins its output, so the registry's declared verdict cannot drift from it.
func TestParamRebindProgramPinsTheLedger(t *testing.T) {
	src := readProgramSrc("param_rebind")
	const want = "10\n1\n0\n1\n42\n0\n7\n10\n0\n1\n2\n2\n0\n100\n200\n200\n8\n8\n10\n"
	interp, ierr := safeInterpreterRun(t, src)
	if ierr != nil {
		t.Fatalf("interpreter: %v", ierr)
	}
	if interp != want {
		t.Errorf("interpreter printed:\n%s\nwant:\n%s", interp, want)
	}
	aot, aerr := runAOTWithTimeout(t, src, 120*time.Second)
	if aerr != nil {
		t.Fatalf("compiled leg: %v (out=%q)", aerr, aot)
	}
	if aot != want {
		t.Errorf("compiled backend printed:\n%s\nwant:\n%s", aot, want)
	}
	py, perr := safeOracleRun(t, src)
	if perr != nil {
		t.Logf("no oracle: %v", perr)
	} else if py != want {
		t.Errorf("CPython printed:\n%s\nwant:\n%s", py, want)
	}
}
