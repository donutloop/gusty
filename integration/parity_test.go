// Package integration contains end-to-end tests that run whole programs through the compiler —
// lex, parse, analyse, emit, `llc`, link, execute — and check what the native binary printed.
//
// These cases used to run every program twice, once on the AST interpreter and once on the compiled
// artifact, and fail when the two disagreed. That made them a parity test between two engines; ADR
// 0302 retired one of them, so the same programs are now checked against the answers the retired
// engine recorded — see `pkg/lang/testdata/interpreter-golden.json` and `lang.RecordedStdout` — and
// against CPython where the shape is legal Python. A program that disagrees with the record is not
// skipped silently: it becomes a row in this package's drift ledger and a row in `roadmap.md`,
// because two engines agreeing (or one engine agreeing with itself) is not evidence of anything, and
// coverage quietly deleted is worse than coverage honestly owed.
package integration

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/donutloop/gusty/pkg/lang"
)

// integrationDriftFile is this package's own list of disagreements with the record. It is separate
// from pkg/lang's because the two binaries run different programs, and a debt paid in one is not a
// debt paid in the other.
const integrationDriftFile = "testdata/interpreter-golden-drift.json"

func TestMain(m *testing.M) {
	code := lang.GoldenDriftMain(m, integrationDriftFile)
	if os.Getenv("GUSTY_DEBT_UPDATE") == "1" {
		writeReferenceDebt(&testing.T{}, referenceDebtFile)
	}
	// The two-way half of the reference-debt ratchet, after every case in the package has had its
	// turn: a row whose case never ran, and a row whose case says the debt is paid, are both failures,
	// and neither can be judged while the package is still mid-run.
	if n, msgs := referenceDebtRatchetSince(isPartialRun()); n >= 0 {
		for _, msg := range msgs {
			fmt.Printf("reference-debt ledger: %s\n", msg)
			code = 1
		}
	}
	if n := CompiledGapCount(); n > 0 {
		// Printed after the run so the number is impossible to miss: this many programs were "passing"
		// because the compiler refused them loudly and a roadmap row owns the refusal. The figure going
		// up while the suite stays green is the suite going soft, and this is the line that catches it.
		// GUSTY_GAP_LEDGER=<path> writes the programs and their sentences for filing as roadmap rows.
		fmt.Printf("compiled refusals this run: %d (filed gaps, not answers)\n", n)
		if pth := os.Getenv("GUSTY_GAP_LEDGER"); pth != "" {
			writeGapLedger(pth)
		}
	}
	os.Exit(code)
}

// writeGapLedger dumps the refusals collected during the run, sorted, for filing.
func writeGapLedger(path string) {
	body := gapLedgerJSON()
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		fmt.Printf("could not write the gap ledger: %v\n", err)
	}
}

// runCompiled keeps the name the suite grew up with. It no longer runs the retired engine: it runs the
// compiled binary and checks its stdout against that engine's recorded answer, so the ~3,000 call
// sites keep their meaning. A program the compiler refuses, or that traps where an answer is recorded,
// fails here rather than returning an empty string.
func runCompiled(t *testing.T, src string) string {
	t.Helper()
	return lang.RecordedStdout(t, src)
}

// runAOT compiles src through the LLVM pipeline (Compile -> llc-20 -> cc) and returns the native
// binary's stdout. This is the artifact users run, so it is the thing under test.
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

// agreesWithRecord runs src natively and asserts the answer is the one on record. It is what `parity`
// became: the comparison is no longer between two engines but between the compiled artifact and the
// answer CPython's semantics produced, so a match means something a self-comparison never did.
func agreesWithRecord(t *testing.T, src string) {
	t.Helper()
	// The record is consulted first: if the compiled backend cannot reproduce it, that is the failure
	// worth hearing about, and it arrives with the expected and actual answers side by side.
	want := runCompiled(t, src)
	got := runAOT(t, src)
	if want != got {
		t.Fatalf("the linked binary disagrees with the compiled run for:\n%s\n compiled run: %q\n linked binary: %q", src, want, got)
	}
}

// cliEngines are the ways the CLI is asked to produce a program: one path, named explicitly rather
// than left to a default, because these tables exist to say which path answered.
var cliEngines = []string{"--aot"}

// TestWholeProgramLargeProgram runs one large, feature-dense program through the compiler and holds
// the linked binary to the answer on record — classes and inheritance, recursion, generators,
// comprehensions, collections, floats, exceptions, matching, slicing, tuple unpacking, augmented
// assignment and f-strings in one file.
func TestWholeProgramLargeProgram(t *testing.T) {
	const prog = `
# Large whole-program: a mini statistics/vector library exercising the shared
# AOT+interpreter surface (classes + inheritance, recursion, generators,
# comprehensions-inline, collections, floats, exceptions, match, slicing, tuple
# unpack, augmented assignment, f-strings). Runs identically on the compiled path.
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
	// This program used to be run only on the retired engine, on the grounds that the AOT path could
	// not lower generators or classes and the produced binary would crash. That excuse expired: with
	// one backend, a construct it cannot lower is a gap to file, not a reason to stop asking. The
	// record is the answer, the linked binary is the artifact, and the two are compared here.
	agreesWithRecord(t, prog)
}

// TestWholeProgramStringIntrospection drives a whole program of string introspection
// methods that fold in AOT and eval in the record: len, count, find,
// rfind, startswith, endswith, isalpha/isdigit/islower/isupper/isalnum/isspace,
// len(...split(...)) aggregates, and an f-string. Only integer/boolean-returning
// methods are used (string-producing results are not supported as AOT prints).
func TestWholeProgramStringIntrospection(t *testing.T) {
	agreesWithRecord(t, `
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

// TestWholeProgramNestedControlAndHeap drives a whole program of nested control flow
// (break/continue/else, nested for loops, while) plus runtime heap collections
// (list append/len/index, dict/set len and index reads) and rebinding.
func TestWholeProgramNestedControlAndHeap(t *testing.T) {
	agreesWithRecord(t, `
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

// TestWholeProgramFunctionsAndAggregation drives a whole program of ternaries, match,
// default/keyword int args, min/max/sum/abs over int lists, len(sorted) and
// len(reversed) folding, multi-argument range, while, and for-else.
func TestWholeProgramFunctionsAndAggregation(t *testing.T) {
	agreesWithRecord(t, `
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

// TestWholeProgramMathFloat drives a whole program of float arithmetic, floor/mod/
// abs, unary minus, float comparisons, and int/float promotion. sum over float
// lists is avoided: the record's float sum is not reliable.
func TestWholeProgramMathFloat(t *testing.T) {
	agreesWithRecord(t, `
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

// TestWholeProgramOopAggregation drives a whole program of deep OOP (a class with an
// aggregating method, a subclass overriding a method via super), recursion
// (factorial), a generator consumed by a counting/summing loop, and an inline
// comprehension read at a constant index. All fold/eval identically in AOT and
// the record.
// TestWholeProgramConversionBuiltins checks that the AOT compiler emits real
// conversions for float(), round(), and int() on both literals and general
// (variable) arguments, matching the record.
func TestWholeProgramConversionBuiltins(t *testing.T) {
	agreesWithRecord(t, `
x = 7
f = float(x)
print(f)
g = 2.5
r = round(g)
print(r)
i = int(g)
print(i)
print(float(3))
print(round(2.5))
print(int(2.9))
`)
}

func TestWholeProgramOopAggregation(t *testing.T) {
	agreesWithRecord(t, `
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

// TestWholeProgramNumericPromotion drives a whole program mixing ints and floats
// (promotion in add/sub/mul/div/floor/mod/abs and unary minus), aggregates
// (min/max/sum/len over int literals, len(sorted)/len(reversed) folding), tuple
// unpack statements, integer floor/mod, an f-string, and a nested summing loop.
func TestWholeProgramNumericPromotion(t *testing.T) {
	agreesWithRecord(t, `
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

// TestWholeProgramLoopVarReuse drives a whole program that reuses the same loop
// variable name across several for-loops (a literal-list unroll and a range
// loop), across a while-else, and in nested loops. Reusing a loop var name
// previously broke AOT with "multiple definition of local value named '_i'";
// the per-function alloca guard keeps the compiled path in agreement.
func TestWholeProgramLoopVarReuse(t *testing.T) {
	agreesWithRecord(t, `
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

// TestWholeProgramObjTaggedDispatch proves the AOT runtime and the record agree
// on the dynamic type model end-to-end: a polymorphic call on a runtime
// receiver is dispatched through the %obj-tagged value representation in the
// AOT backend and through the same canonical kind tags in the record, so
// both must produce identical stdout.
func TestWholeProgramObjTaggedDispatch(t *testing.T) {
	agreesWithRecord(t, `
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

// TestWholeProgramNumericLiterals drives the modern numeric-literal syntax (hex,
// binary, octal, digit separators, and underscore misuse) through both
// backends and asserts identical stdout.
func TestWholeProgramNumericLiterals(t *testing.T) {
	agreesWithRecord(t, `
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

// TestWholeProgramDocstrings verifies that `def.__doc__` / `Cls.__doc__` folds to a
// string constant identically in the record and the AOT backend.
func TestWholeProgramDocstrings(t *testing.T) {
	agreesWithRecord(t, `
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

func TestWholeProgramLiteralMembership(t *testing.T) {
	agreesWithRecord(t, `
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

func TestWholeProgramWithManagerFieldAccess(t *testing.T) {
	agreesWithRecord(t, `
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

// TestWholeProgramYieldFromAcrossGC exercises yield-from of a generator whose
// accumulator list must survive the GC emitted at statement boundaries; a
// stale handle produced double-appends (ADR 0140 / 0151 regression).
func TestWholeProgramYieldFromAcrossGC(t *testing.T) {
	agreesWithRecord(t, `
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

// TestWholeProgramYieldFromLiteral verifies yield-from of a statically-known list
// literal (no heap backing) appends each element exactly once.
func TestWholeProgramYieldFromLiteral(t *testing.T) {
	agreesWithRecord(t, `
def outer():
    yield 0
    yield from [1, 2, 3]
s = 0
for x in outer():
    s = s + x
print(s)
`)
}

func TestWholeProgramWalrus(t *testing.T) {
	agreesWithRecord(t, `
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

func TestWholeProgramWalrusFnScope(t *testing.T) {
	agreesWithRecord(t, `
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

func TestWholeProgramStringSlice(t *testing.T) {
	agreesWithRecord(t, `
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
