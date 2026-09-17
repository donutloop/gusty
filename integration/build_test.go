package integration

import (
	"bytes"
	"encoding/json"
	"errors"
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

// exitCode extracts the child process exit code from an exec error.
func exitCode(t *testing.T, err error) int {
	t.Helper()
	var ee *exec.ExitError
	if ok := errors.As(err, &ee); !ok {
		t.Fatalf("expected exec.ExitError, got %v", err)
	}
	return ee.ExitCode()
}

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

// TestCLIBuildSingleFile verifies the build command works with exactly one
// source file, not just a multi-file merge.
func TestCLIBuildSingleFile(t *testing.T) {
	dir := t.TempDir()
	bin := filepath.Join(dir, "gustyc")
	buildCLI(t, bin)

	src := writeSrc(t, dir, "s.gy", "x = 0\nfor i in range(3):\n    x = x + i\nprint(x)\n")
	out := filepath.Join(dir, "prog")

	build := exec.Command(bin, "--build", out, src)
	if outb, err := build.CombinedOutput(); err != nil {
		t.Fatalf("gustyc --build: %v\n%s", err, outb)
	}

	got, err := exec.Command(out).Output()
	if err != nil {
		t.Fatalf("run built binary: %v", err)
	}
	if string(got) != "3\n" {
		t.Errorf("output = %q, want 3 (0+1+2)", got)
	}
}

// TestCLIBuildOptLevel verifies --opt-level is honored end-to-end: the CLI
// passes it through to Build, and the produced binary still runs correctly.
func TestCLIBuildOptLevel(t *testing.T) {
	dir := t.TempDir()
	bin := filepath.Join(dir, "gustyc")
	buildCLI(t, bin)

	src := writeSrc(t, dir, "s.gy", "def sq(x):\n    return x * x\nprint(sq(7))\n")
	out := filepath.Join(dir, "prog")

	build := exec.Command(bin, "--opt-level", "2", "--build", out, src)
	if outb, err := build.CombinedOutput(); err != nil {
		t.Fatalf("gustyc --opt-level 2 --build: %v\n%s", err, outb)
	}

	got, err := exec.Command(out).Output()
	if err != nil {
		t.Fatalf("run built binary: %v", err)
	}
	if string(got) != "49\n" {
		t.Errorf("output = %q, want 49", got)
	}
}

// TestCLIBuildMissingFile verifies a nonexistent source file fails with a
// non-zero exit and an error mentioning the missing path, without producing a
// binary.
func TestCLIBuildMissingFile(t *testing.T) {
	dir := t.TempDir()
	bin := filepath.Join(dir, "gustyc")
	buildCLI(t, bin)

	out := filepath.Join(dir, "prog")
	missing := filepath.Join(dir, "nope.gy")
	build := exec.Command(bin, "--build", out, missing)
	outb, err := build.CombinedOutput()
	if err == nil {
		t.Fatalf("gustyc --build should fail for missing source; got success\n%s", outb)
	}
	if _, statErr := os.Stat(out); !os.IsNotExist(statErr) {
		t.Errorf("no binary should be produced")
	}
}

// TestCLIBuildExitCodes asserts the whole CLI's exit-code contract: 0 on
// success, 1 on compile/link error, 2 on usage error.
func TestCLIBuildExitCodes(t *testing.T) {
	dir := t.TempDir()
	bin := filepath.Join(dir, "gustyc")
	buildCLI(t, bin)

	src := writeSrc(t, dir, "s.gy", "print(1)\n")
	bad := writeSrc(t, dir, "bad.gy", "x = nope + 1\nprint(x)\n")
	okOut := filepath.Join(dir, "ok")
	badOut := filepath.Join(dir, "bad")

	// success -> 0
	if err := exec.Command(bin, "--build", okOut, src).Run(); err != nil {
		t.Fatalf("success build should exit 0: %v", err)
	}
	// compile error -> 1
	if err := exec.Command(bin, "--build", badOut, bad).Run(); err == nil {
		t.Fatalf("compile-error build should exit non-zero")
	} else if code := exitCode(t, err); code != 1 {
		t.Errorf("compile error exit = %d, want 1", code)
	}
	// usage error (no sources) -> 2
	if err := exec.Command(bin, "--build", filepath.Join(dir, "u")).Run(); err == nil {
		t.Fatalf("no-sources build should exit non-zero")
	} else if code := exitCode(t, err); code != 2 {
		t.Errorf("usage error exit = %d, want 2", code)
	}
}

// TestCLIBuildDuplicateFunction verifies that two files defining the same
// top-level function produce a build error (duplicate symbol) rather than a
// silently broken binary.
func TestCLIBuildDuplicateFunction(t *testing.T) {
	dir := t.TempDir()
	bin := filepath.Join(dir, "gustyc")
	buildCLI(t, bin)

	a := writeSrc(t, dir, "a.gy", "def f():\n    return 1\n")
	b := writeSrc(t, dir, "b.gy", "def f():\n    return 2\nprint(f())\n")
	out := filepath.Join(dir, "prog")

	build := exec.Command(bin, "--build", out, a, b)
	if outb, err := build.CombinedOutput(); err == nil {
		t.Fatalf("duplicate def across files should fail; got success\n%s", outb)
	}
	if _, statErr := os.Stat(out); !os.IsNotExist(statErr) {
		t.Errorf("no binary should be produced on duplicate symbol")
	}
}

// TestCLIBuildJSONDiagnostics verifies that on a compile error, --json emits a
// parseable BuildResult carrying the diagnostics, and stderr carries the error.
func TestCLIBuildJSONDiagnostics(t *testing.T) {
	dir := t.TempDir()
	bin := filepath.Join(dir, "gustyc")
	buildCLI(t, bin)

	bad := writeSrc(t, dir, "bad.gy", "x = nope + 1\nprint(x)\n")
	out := filepath.Join(dir, "prog")

	build := exec.Command(bin, "--json", "--build", out, bad)
	var stderr bytes.Buffer
	build.Stderr = &stderr
	outb, err := build.Output() // stdout carries the JSON BuildResult
	if err == nil {
		t.Fatalf("build should fail on undefined name; got success\n%s", outb)
	}
	var res map[string]any
	if jerr := json.Unmarshal(outb, &res); jerr != nil {
		t.Fatalf("json diagnostics not parseable: %v\n%s", jerr, outb)
	}
	if stderr.Len() == 0 {
		t.Errorf("expected human error on stderr, got none")
	}
	if _, ok := res["diagnostics"]; !ok {
		t.Errorf("diagnostics key missing in JSON result: %v", res)
	}
	if _, statErr := os.Stat(out); !os.IsNotExist(statErr) {
		t.Errorf("no binary should be produced on error")
	}
}

// TestCLIBuildRebuildOverwrite verifies building a second time to the same
// output path succeeds and the binary still runs.
func TestCLIBuildRebuildOverwrite(t *testing.T) {
	dir := t.TempDir()
	bin := filepath.Join(dir, "gustyc")
	buildCLI(t, bin)

	a := writeSrc(t, dir, "a.gy", "def double(x):\n    return x * 2\n")
	b := writeSrc(t, dir, "b.gy", "print(double(21))\n")
	out := filepath.Join(dir, "prog")

	for i := 0; i < 2; i++ {
		build := exec.Command(bin, "--build", out, a, b)
		if outb, err := build.CombinedOutput(); err != nil {
			t.Fatalf("build #%d: %v\n%s", i+1, err, outb)
		}
	}
	got, err := exec.Command(out).Output()
	if err != nil {
		t.Fatalf("run rebuilt binary: %v", err)
	}
	if string(got) != "42\n" {
		t.Errorf("output = %q, want 42", got)
	}
}

// allFeaturesSrcA and allFeaturesSrcB are a two-file program exercising every
// construct the AOT/build path supports: arithmetic (+,-,*,/,//,%), floats,
// strings (concat/len/index/upper/lower), functions, keyword args, lambdas,
// list/dict/set literals, len/sum/min/max/abs/int/float/str builtins,
// for-range (+step), while, break/continue, and if/elif/else. Each variable
// name is unique across the merged program (codegen names SSA regs by var).
const allFeaturesSrcA = `sumx = 0
for ia in range(5):
    sumx = sumx + ia
print("sum", sumx)
print("arith", 10 // 3, 10 % 3, 10 / 2, 2 * 3, 2 - 3, 2 + 3)
fval = 2.5 + 1.0
print("float", fval) # codegen truncates float literals to int
print("str", "a" + "b" + "c")
print("slen", len("hello"))
print("sidx", "abc"[1])
print("sup", "abc".upper())
print("slow", "ABC".lower())
`

const allFeaturesSrcB = `def add(a, b):
    return a + b
def kw(a, b):
    return a - b
print("func", add(2, 3))
print("kw", kw(a=9, b=4))
g = lambda a: a * a
print("lambda", g(7))
print("len", len([1, 2, 3]))
print("idx", [1, 2, 3][1])
print("sum", sum([1, 2, 3]))
print("minmax", min([1, 2, 3]), max([1, 2, 3]))
print("dict", len({1: 10, 2: 20}), {1: 10, 2: 20}[1])
print("set", len({1, 2, 3}), {1, 2, 3}[2])
wi = 0
wt = 0
while wi < 10:
    wi = wi + 1
    if wi == 3:
        continue
    if wi == 7:
        break
    wt = wt + wi
print("while", wt)
xf = 3
if xf == 1:
    print("if", "one")
elif xf == 2:
    print("if", "two")
else:
    print("if", "many")
stp = 0
for si in range(0, 10, 2):
    stp = stp + si
print("step", stp)
print("abs", abs(-5))
print("conv", int("42"), float(1), str(42))
`

// allFeaturesWant is the exact stdout the built binary must produce. Each
// print argument lands on its own line (gusty print emits one value per line).
const allFeaturesWant = `sum
10
arith
3
1
5
6
-1
5
float
3
str
abc
slen
5
sidx
98
sup
ABC
slow
abc
func
5
kw
5
lambda
49
len
3
idx
2
sum
6
minmax
1
3
dict
2
10
set
3
2
while
18
if
many
step
20
abs
5
conv
42
1
42
`

// TestCLIBuildAllFeatures drives the whole gustyc program against a large
// two-file source covering every AOT-supported language feature, then runs the
// produced native binary and asserts its entire stdout byte-for-byte.
func TestCLIBuildAllFeatures(t *testing.T) {
	dir := t.TempDir()
	bin := filepath.Join(dir, "gustyc")
	buildCLI(t, bin)

	a := writeSrc(t, dir, "features_a.gy", allFeaturesSrcA)
	b := writeSrc(t, dir, "features_b.gy", allFeaturesSrcB)
	out := filepath.Join(dir, "prog")

	build := exec.Command(bin, "--build", out, a, b)
	if outb, err := build.CombinedOutput(); err != nil {
		t.Fatalf("gustyc --build (all features): %v\n%s", err, outb)
	}

	got, err := exec.Command(out).Output()
	if err != nil {
		t.Fatalf("run all-features binary: %v", err)
	}
	if string(got) != allFeaturesWant {
		t.Errorf("all-features output mismatch:\n got:\n%s\nwant:\n%s", got, allFeaturesWant)
	}
}

// Large multi-file programs. Each file is big (many statements); the tests
// drive the real gustyc binary end-to-end and assert the produced binary's
// entire stdout against an exact expected constant.

const largeMathLib = `def add(a, b):
    return a + b
def mul(a, b):
    return a * b
def scale(x, n):
    return x * n
`
const largeMathCalc = `ac = 0
for ic in range(1, 6):
    ac = ac + ic
bc = add(ac, mul(2, 3))
print("calc", bc)
print("scaled", scale(bc, 2))
`
const largeMathMain = `mc = 0
for jc in range(0, 10, 2):
    mc = mc + jc
mc2 = mc + 10
print("main", mc2)
print("addcall", add(mc2, 5))
print("mulcall", mul(mc2, 2))
`
const largeMathWant = `calc
21
scaled
42
main
30
addcall
35
mulcall
60
`

const largeCtrlA = `acc = 0
for i1 in range(1, 6):
    acc = acc + i1
print("acc", acc)
acc2 = 0
for i2 in range(0, 10, 2):
    acc2 = acc2 + i2
print("acc2", acc2)
acc3 = 0
for i3 in range(10, 0, -2):
    acc3 = acc3 + i3
print("acc3", acc3)
`
const largeCtrlB = `w1 = 0
w2 = 0
while w1 < 20:
    w1 = w1 + 1
    if w1 == 5:
        continue
    if w1 == 15:
        break
    w2 = w2 + w1
print("while", w2)
w3 = 0
w4 = 0
while w3 < 8:
    w3 = w3 + 2
    w4 = w4 + w3
print("while2", w4)
`
const largeCtrlC = `def classify(n):
    if n == 1:
        return 1
    elif n == 2:
        return 2
    elif n == 3:
        return 3
    else:
        return 0
print("cls", classify(2), classify(9), classify(3))
tot = 0
for j1 in range(3):
    for j2 in range(3):
        tot = tot + j2
print("nested", tot)
tot2 = 0
for k1 in range(5):
    tot2 = tot2 + k1
    if k1 == 2:
        continue
    tot2 = tot2 + 1
print("cont", tot2)
`
const largeCtrlWant = `acc
15
acc2
20
acc3
30
while
100
while2
20
cls
2
0
3
nested
9
cont
14
`

const largeDataA = `da1 = 0
for di in range(1, 6):
    da1 = da1 + di
print("dsum", da1)
print("dmin", min([3, 1, 4, 1, 5]))
print("dmax", max([3, 1, 4, 1, 5]))
print("dlen", len([10, 20, 30]))
print("didx", [7, 8, 9][1])
`
const largeDataB = `print("dkeys", len({1: 10, 2: 20, 3: 30}))
print("dval", {1: 10, 2: 20}[2])
print("slen", len({5, 6, 7}))
print("sidx", {5, 6, 7}[6])
db1 = 0
for dj in range(1, 10, 2):
    db1 = db1 + dj
print("odd", db1)
`
const largeDataC = `def total(n):
    t = 0
    for tk in range(1, n):
        t = t + tk
    return t
dc1 = total(7)
print("fsum", dc1)
dc2 = 0
for dl in range(1, 10):
    dc2 = dc2 + dl
print("loop", dc2)
dc3 = dc1 + dc2
print("big", dc3)
print("abs", abs(-42))
print("conv", int("100"), float(2), str(7))
`
const largeDataWant = `dsum
15
dmin
1
dmax
5
dlen
3
didx
8
dkeys
3
dval
20
slen
3
sidx
6
odd
25
fsum
21
loop
45
big
66
abs
42
conv
100
2
7
`

// buildWant runs `gustyc --build` over files, then runs the produced binary
// and asserts its stdout equals want.
func buildWant(t *testing.T, bin string, files []string, want string) {
	t.Helper()
	out := filepath.Join(t.TempDir(), "prog")
	args := append([]string{"--build", out}, files...)
	build := exec.Command(bin, args...)
	if outb, err := build.CombinedOutput(); err != nil {
		t.Fatalf("gustyc --build: %v\n%s", err, outb)
	}
	got, err := exec.Command(out).Output()
	if err != nil {
		t.Fatalf("run built binary: %v", err)
	}
	if string(got) != want {
		t.Errorf("output mismatch:\n got:\n%s\nwant:\n%s", got, want)
	}
}

// TestCLIBuildLargeMath builds a large 3-file math program (functions defined
// in one file, called from the others) and checks the binary's exact stdout.
func TestCLIBuildLargeMath(t *testing.T) {
	dir := t.TempDir()
	bin := filepath.Join(dir, "gustyc")
	buildCLI(t, bin)
	buildWant(t, bin, []string{
		writeSrc(t, dir, "mathlib.gy", largeMathLib),
		writeSrc(t, dir, "calc.gy", largeMathCalc),
		writeSrc(t, dir, "main.gy", largeMathMain),
	}, largeMathWant)
}

// TestCLIBuildLargeControl builds a large 3-file control-flow program (loops,
// while, break/continue, if/elif/else, nested loops) and checks exact stdout.
func TestCLIBuildLargeControl(t *testing.T) {
	dir := t.TempDir()
	bin := filepath.Join(dir, "gustyc")
	buildCLI(t, bin)
	buildWant(t, bin, []string{
		writeSrc(t, dir, "ctrl_a.gy", largeCtrlA),
		writeSrc(t, dir, "ctrl_b.gy", largeCtrlB),
		writeSrc(t, dir, "ctrl_c.gy", largeCtrlC),
	}, largeCtrlWant)
}

// TestCLIBuildLargeData builds a large 3-file data program (list/dict/set
// literals, builtins, functions, loops) and checks exact stdout.
func TestCLIBuildLargeData(t *testing.T) {
	dir := t.TempDir()
	bin := filepath.Join(dir, "gustyc")
	buildCLI(t, bin)
	buildWant(t, bin, []string{
		writeSrc(t, dir, "data_a.gy", largeDataA),
		writeSrc(t, dir, "data_b.gy", largeDataB),
		writeSrc(t, dir, "data_c.gy", largeDataC),
	}, largeDataWant)
}
