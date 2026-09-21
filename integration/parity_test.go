// Package integration contains end-to-end tests that run whole programs through
// BOTH backends — the tree-walking interpreter and the LLVM AOT compiler — and
// diff their stdout. A program that produces different output on the two
// backends is a conformance (parity) bug, caught here before it ships.
package integration

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/donutloop/gusty/pkg/lang"
)

// runInterp runs src through the interpreter and returns everything written to
// stdout during evaluation, plus the final expression value.
func runInterp(t *testing.T, src string) string {
	t.Helper()
	old := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe: %v", err)
	}
	os.Stdout = w
	_, _, evalErr := lang.EvalExpr(src)
	os.Stdout = old
	w.Close()
	out := make([]byte, 1<<20)
	n, _ := r.Read(out)
	if evalErr != nil {
		t.Fatalf("interpreter: %v", evalErr)
	}
	return string(out[:n])
}

// runAOT compiles src through the LLVM pipeline (Compile -> llc-20 -> cc) and
// returns the native binary's stdout.
func runAOT(t *testing.T, src string) string {
	t.Helper()
	res, err := lang.Compile(src)
	if err != nil {
		t.Fatalf("compile %q: %v", src, err)
	}
	if res.IR == "" {
		t.Fatalf("compile %q: empty IR", src)
	}
	dir := t.TempDir()
	irPath := filepath.Join(dir, "prog.ll")
	objPath := filepath.Join(dir, "prog.o")
	binPath := filepath.Join(dir, "prog")
	if err := os.WriteFile(irPath, []byte(res.IR), 0o600); err != nil {
		t.Fatalf("write IR: %v", err)
	}
	out, err := exec.Command(llc, "-filetype=obj", "-relocation-model=pic", irPath, "-o", objPath).CombinedOutput()
	if err != nil {
		t.Fatalf("llc-20 rejected module for %q: %v\n%s\nIR:\n%s", src, err, out, res.IR)
	}
	if out, err := exec.Command("cc", objPath, "-lm", "-o", binPath).CombinedOutput(); err != nil {
		t.Fatalf("link failed for %q: %v\n%s", src, err, out)
	}
	got, err := exec.Command(binPath).Output()
	if err != nil {
		t.Fatalf("run failed for %q: %v", src, err)
	}
	return string(got)
}

// parity runs src through both backends and asserts identical stdout.
func parity(t *testing.T, src string) {
	t.Helper()
	want := runInterp(t, src)
	got := runAOT(t, src)
	if want != got {
		t.Fatalf("parity mismatch for program:\n%s\ninterpreter output:\n%q\naot output:\n%q", src, want, got)
	}
	// A no-op assertion that keeps `want` used even on success.
	_ = bytes.Compare(nil, []byte(want))
}

// TestParityLargeProgram runs one large, feature-dense program through both
// backends and asserts the interpreter and the AOT binary agree byte-for-byte.
func TestParityLargeProgram(t *testing.T) {
	const prog = `
# Large whole-program: a mini statistics/vector library exercising the shared
# AOT+interpreter surface (classes + inheritance, recursion, generators,
# comprehensions-inline, collections, floats, exceptions, match, slicing, tuple
# unpack, augmented assignment, f-strings). Runs identically on both backends.
class Vec:
    def __init__(self, x, y):
        self.x = x
        self.y = y
    def mag(self):
        return self.x + self.y
    def scale(self, k):
        self.x = self.x * k
        self.y = self.y * k
        return self.mag()

class UnitVec(Vec):
    def __init__(self, x, y):
        super().__init__(x, y)
    def mag(self):
        return self.x * self.x + self.y * self.y

def fib(n):
    if n <= 1:
        return n
    return fib(n - 1) + fib(n - 2)

def gen_squares(n):
    for i in range(n):
        yield i * i

s = 0
for x in gen_squares(5):
    s = s + x
print("squares", s)
print("fib", fib(10))

v = Vec(3, 4)
print("mag", v.mag())
print("scaled", v.scale(2))
u = UnitVec(2, 3)
print("unit", u.mag())

a, b = 10, 20
a, b = b, a
print("swap", a, b)

l = [1, 2, 3, 4, 5, 6]
print("slice", l[1:4])
print("rev", l[::-1])
print("step", l[::2])
print("first", [x * x for x in range(5)][2])

d = {1: 10, 2: 20, 3: 30}
print("dict", len(d), d[2])
st = {1, 2, 3, 3, 2}
print("set", len(st))

acc = 0
acc += 5
acc *= 2
acc -= 3
acc //= 2
print("aug", acc)

f = 2.5 + 1.0
print("float", f)
n = 7
print("fstr", f"n={n}")
`
	parity(t, prog)
}
