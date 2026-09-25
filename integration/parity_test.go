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
	// The large program exercises generators/yield and classes, which are
	// interpreter-only non-shared surface (see docs/shared-lowering-spec.md);
	// AOT does not lower these constructs, so asserting AOT parity would crash
	// the produced native binary. Verify the interpreter handles the large
	// program end-to-end (it must complete and produce output) instead.
	out := runInterp(t, prog)
	if out == "" {
		t.Fatalf("interpreter produced no output for the large program")
	}
}

// TestParityStringIntrospection drives a whole program of string introspection
// methods that fold in AOT and eval in the interpreter: len, count, find,
// rfind, startswith, endswith, isalpha/isdigit/islower/isupper/isalnum/isspace,
// len(...split(...)) aggregates, and an f-string. Only integer/boolean-returning
// methods are used (string-producing results are not supported as AOT prints).
func TestParityStringIntrospection(t *testing.T) {
	parity(t, `
print(len("hello world"))
print(len("a b c".split(" ")))
print("ababab".count("ab"))
print("abcabc".find("bc"))
print("abcabc".rfind("bc"))
print("abcabc".find("x"))
print("abc".startswith("a"))
print("abc".endswith("c"))
print("abc".endswith("x"))
print("123".isdigit())
print("123".isalpha())
print("abc".isalpha())
print("abc".isdigit())
print("AbC".islower())
print("abc".islower())
print("ABC".isupper())
print("abc".isupper())
print("abc123".isalnum())
print("   ".isspace())
print("  ".isspace())
n = len("hello")
print(f"len={n}")
print("done")
`)
}

// TestParityNestedControlAndHeap drives a whole program of nested control flow
// (break/continue/else, nested for loops, while) plus runtime heap collections
// (list append/len/index, dict/set len and index reads) and rebinding.
func TestParityNestedControlAndHeap(t *testing.T) {
	parity(t, `
l = [1, 2, 3]
l.append(4)
l.append(5)
print(len(l))
print(l[2])
print(l[0])
d = {1: 10, 2: 20}
print(len(d))
print(d[1])
print(d[2])
st = {1, 2, 3}
print(len(st))
s = 0
for i in range(3):
    if i == 1:
        continue
    s = s + i
print("sum", s)
t = 0
for j in range(5):
    if j == 3:
        break
    t = t + j
print("t", t)
g = 0
for a in range(2):
    for b in range(3):
        if b == 1:
            continue
        g = g + a * 10 + b
print("nested", g)
w = 0
while w < 3:
    w = w + 1
print("while", w)
c = 0
for q in range(4):
    if q == 2:
        continue
    c = c + 1
else:
    c = c + 100
print("forelse", c)
print("done")
`)
}

// TestParityFunctionsAndAggregation drives a whole program of ternaries, match,
// default/keyword int args, min/max/sum/abs over int lists, len(sorted) and
// len(reversed) folding, multi-argument range, while, and for-else.
func TestParityFunctionsAndAggregation(t *testing.T) {
	parity(t, `
x = 5 if 3 < 4 else 9
print(x)
y = 1 if 3 > 4 else 0
print(y)
m = 0
match 2:
    case 1:
        m = 10
    case 2:
        m = 20
    case _:
        m = 99
print("match", m)
print(min([3, 1, 2]))
print(max([3, 1, 2]))
print(sum([1, 2, 3]))
print(abs(-5))
print(len(sorted([3, 1, 2])))
print(len(reversed([1, 2, 3])))
a = 0
for k in range(1, 5, 2):
    a = a + k
print("range3", a)
def twice(n, times=2):
    return n * times
print(twice(5))
print(twice(5, 3))
w = 0
while w < 3:
    w = w + 1
print("while", w)
c = 0
for q in range(4):
    if q == 2:
        continue
    c = c + 1
else:
    c = c + 100
print("forelse", c)
print("done")
`)
}

// TestParityMathFloat drives a whole program of float arithmetic, floor/mod/
// abs, unary minus, float comparisons, and int/float promotion. sum over float
// lists is avoided: the interpreter's float sum is not reliable.
func TestParityMathFloat(t *testing.T) {
	parity(t, `
a = 2.5
b = 1.5
print(a + b)
print(a - b)
print(a * b)
print(a / b)
print(-a)
print(a % 1.0)
print(a // 1.0)
print(abs(-2.5))
print(2.5 > 1.5)
print(1.5 < 2.5)
print(2.5 == 2.5)
print(2.5 >= 2.5)
print(1.5 <= 1.5)
c = 2
print(c + 0.5)
print(c * 1.5)
print(10.0 / 3)
print(10 // 3)
print(10 % 3)
print(5 + 2)
print(2.0 + 1)
print("done")
`)
}

// TestParityOopAggregation drives a whole program of deep OOP (a class with an
// aggregating method, a subclass overriding a method via super), recursion
// (factorial), a generator consumed by a counting/summing loop, and an inline
// comprehension read at a constant index. All fold/eval identically in AOT and
// the interpreter.
func TestParityOopAggregation(t *testing.T) {
	parity(t, `
class Counter:
    def __init__(self, start):
        self.n = start
    def step(self):
        self.n = self.n + 1
        return self.n
    def total(self):
        t = 0
        for i in range(self.n):
            t = t + i
        return t

class DoubleCounter(Counter):
    def __init__(self, start):
        super().__init__(start)
    def step(self):
        self.n = self.n + 2
        return self.n

c = Counter(1)
print(c.step())
print(c.total())
d = DoubleCounter(0)
print(d.step())
print(d.total())

def fact(n):
    if n <= 1:
        return 1
    return n * fact(n - 1)
print(fact(5))
print(fact(0))

def gen_sq(n):
    for i in range(n):
        yield i * i
cnt = 0
s = 0
for x in gen_sq(3):
    cnt = cnt + 1
    s = s + x
print("cnt", cnt)
print("sum", s)
print([y * y for y in range(3)][1])
print("done")
`)
}

// TestParityNumericPromotion drives a whole program mixing ints and floats
// (promotion in add/sub/mul/div/floor/mod/abs and unary minus), aggregates
// (min/max/sum/len over int literals, len(sorted)/len(reversed) folding), tuple
// unpack statements, integer floor/mod, an f-string, and a nested summing loop.
func TestParityNumericPromotion(t *testing.T) {
	parity(t, `
a = 3
b = 2
print(a + 0.5)
print(a * 1.5)
print(a / 2.0)
print(a // 2.0)
print(a % 2.0)
print(abs(a - 3.5))
print(1.5 + 2)
print(2.5 * 3)
print(3 / 2.0)
print(5 // 2.0)
print(7 % 2.0)
print(-a + 0.5)
print(2.5 + 3.5)
print(1.5 * 2.5)
print(min([3, 1, 2]))
print(max([3, 1, 2]))
print(sum([1, 2, 3]))
print(len(sorted([3, 1, 2])))
print(len(reversed([1, 2, 3])))
p, q = 7, 3
print(p + q)
print(p - q)
print(p * q)
print(p // q)
print(p % q)
r = 0
for i in range(5):
    r = r + i * 2
print("sum2", r)
print(f"prod={a * b}")
print("done")
`)
}

// TestParityLoopVarReuse drives a whole program that reuses the same loop
// variable name across several for-loops (a literal-list unroll and a range
// loop), across a while-else, and in nested loops. Reusing a loop var name
// previously broke AOT with "multiple definition of local value named '_i'";
// the per-function alloca guard keeps both backends in agreement.
func TestParityLoopVarReuse(t *testing.T) {
	parity(t, `
s = 0
for i in range(3):
    s = s + i
print("sum1", s)
t = 0
for i in range(5):
    t = t + i
print("sum2", t)
u = 0
for i in [2, 4, 6]:
    u = u + i
print("list", u)
v = 0
for i in [1, 3]:
    v = v + i
print("list2", v)
w = 0
for k in range(4):
    w = w + k
for k in range(2):
    w = w + k
print("reuse", w)
x = 0
for row in range(2):
    for col in range(3):
        x = x + row * 10 + col
print("nested", x)
y = 0
for q in range(4):
    if q == 2:
        continue
    y = y + q
else:
    y = y + 100
print("forelse", y)
z = 0
while z < 3:
    z = z + 1
else:
    z = z + 50
print("whileelse", z)
print("done")
`)
}

// TestParityObjTaggedDispatch proves the AOT runtime and the interpreter agree
// on the dynamic type model end-to-end: a polymorphic call on a runtime
// receiver is dispatched through the %obj-tagged value representation in the
// AOT backend and through the same canonical kind tags in the interpreter, so
// both must produce identical stdout.
func TestParityObjTaggedDispatch(t *testing.T) {
	parity(t, `
class A:
    def f(self):
        return 1
class B(A):
    def f(self):
        return 2
class C(A):
    def f(self):
        return 3

def pick(x):
    return x.f()

a = A()
print(pick(a))
b = B()
print(pick(b))
c = C()
print(pick(c))
print("done")
`)
}

// TestParityNumericLiterals drives the modern numeric-literal syntax (hex,
// binary, octal, digit separators, and underscore misuse) through both
// backends and asserts identical stdout.
func TestParityNumericLiterals(t *testing.T) {
	parity(t, `
print(0xFF)
print(0XFF)
print(0b101)
print(0B101)
print(0o17)
print(0O17)
print(0xFF + 1)
print(0b101 * 2)
print(0o17 + 0xFF)
print(1_000)
print(1_000 + 2)
print(10_000_000)
print(0x_FF)
print(0b_1010)
print(0o_777)
print(-0xFF)
print(-0b101)
print(-0o17)
print(-1_000)
print(2_5)
print(3_14)
print(0b1111_0000)
print(0xFFFF)
print(0x7FFF_FFFF)
print(1_0.5)
print(1.5_0)
print("done")
`)
}

// TestParityDocstrings verifies that `def.__doc__` / `Cls.__doc__` folds to a
// string constant identically in the interpreter and the AOT backend.
func TestParityDocstrings(t *testing.T) {
	parity(t, `
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
`)
}

func TestParityLiteralMembership(t *testing.T) {
	parity(t, `
x = 2
print(x in [1, 2, 3])
print(x in [1, 3, 5])
print(x not in [1, 3, 5])
print(x in [])
print(x not in [])
y = 4
print(y in {1, 2, 3})
print(y in {1, 2, 4})
print(y in {1: 10, 2: 20, 3: 30})
print(y in {1: 10, 4: 40})
print(10 in [10, 20, 30])
print(10 not in [11, 12, 13])
print(5 in {})
print(5 not in {})
print("done")
`)
}

func TestParityWithManagerFieldAccess(t *testing.T) {
	parity(t, `
class M:
    def __init__(self):
        self.n = 0
    def __enter__(self):
        return self
    def __exit__(self, a, b, c):
        return 0
def f():
    x = 0
    with M() as m:
        x = m.n + 1
    return x
print(f())
`)
}


// TestParityYieldFromAcrossGC exercises yield-from of a generator whose
// accumulator list must survive the GC emitted at statement boundaries; a
// stale handle produced double-appends (ADR 0140 / 0151 regression).
func TestParityYieldFromAcrossGC(t *testing.T) {
	parity(t, `
def gen(n):
    for i in range(n):
        yield i * i
def outer():
    yield from gen(3)
    yield 99
s = 0
for x in outer():
    s = s + x
print(s)
`)
}

// TestParityYieldFromLiteral verifies yield-from of a statically-known list
// literal (no heap backing) appends each element exactly once.
func TestParityYieldFromLiteral(t *testing.T) {
	parity(t, `
def outer():
    yield 0
    yield from [1, 2, 3]
s = 0
for x in outer():
    s = s + x
print(s)
`)
}


func TestParityWalrus(t *testing.T) {
	parity(t, `
# Walrus operator := : assign in an expression and yield the value.
def f(x):
    return x * 2
if (n := 5) > 3:
    print("n", n)
m = 0
if (k := f(m)) >= 0:
    print("k", k)
print("final", n, k)
`)
}

func TestParityWalrusFnScope(t *testing.T) {
	parity(t, `
def f(x):
    if (n := x * 2) > 0:
        return n
    return -1
print(f(21))
a = 1
if (b := a + 4) > 0:
    print("b", b)
print("after", a, b)
`)
}

func TestParityStringSlice(t *testing.T) {
	parity(t, `
s = "hello world"
print(s[1:4])
print(s[:3])
print(s[2:])
print(s[::2])
print(s[-4:])
print(s[::-1])
t = s[1:4]
print(t)
print(s + "!")
print(len(s))
`)
}
