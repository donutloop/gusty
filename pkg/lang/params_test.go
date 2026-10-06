package lang

import (
	"strings"
	"testing"
)

// A parameter is a local variable that starts out bound to an argument. The compiled
// backend used to answer every reference to one from its incoming argument register,
// so a body that assigned to it stored into a slot nothing read: `def bump(n): n = n
// + 1; return n` answered 0, and an accumulator loop never terminated (roadmap
// Gap R.3, ADR 0196). These tests pin both halves — the scan that decides which
// parameters need an entry slot, and the IR that has to come out.

func mustIR(t *testing.T, src string) string {
	t.Helper()
	ir, err := GenerateIR(parseOrFatal(t, src))
	if err != nil {
		t.Fatalf("GenerateIR: %v\n%s", err, src)
	}
	return ir
}

func TestReboundParamsDetectsEachWayANameIsBound(t *testing.T) {
	cases := []struct {
		name string
		src  string
		fn   string
		want []string
		none []string
	}{
		{
			name: "plain assign",
			src:  "def f(n, k):\n    n = n + 1\n    return n + k\nprint(f(1, 2))\n",
			fn:   "f", want: []string{"n"}, none: []string{"k"},
		},
		{
			name: "augmented assign",
			src:  "def f(n, k):\n    k += 2\n    return n + k\nprint(f(1, 2))\n",
			fn:   "f", want: []string{"k"}, none: []string{"n"},
		},
		{
			name: "the loop variable is the parameter",
			src:  "def f(n):\n    for n in range(3):\n        print(n)\n    return n\nprint(f(9))\n",
			fn:   "f", want: []string{"n"}, none: nil,
		},
		{
			name: "with-as target",
			src:  "class M:\n    def __enter__(self):\n        return 1\n    def __exit__(self):\n        return 0\n\ndef f(n):\n    with M() as n:\n        print(n)\n    return n\nprint(f(4))\n",
			fn:   "f", want: []string{"n"}, none: nil,
		},
		{
			name: "a case pattern captures it",
			src:  "def f(n):\n    match 1:\n        case n:\n            print(n)\n    return n\nprint(f(4))\n",
			fn:   "f", want: []string{"n"}, none: nil,
		},
		{
			name: "a comprehension reuses the name",
			src:  "def f(n):\n    xs = [n for n in range(3)]\n    return len(xs)\nprint(f(4))\n",
			fn:   "f", want: []string{"n"}, none: nil,
		},
		{
			name: "destructuring target",
			src:  "def f(a, b):\n    a, b = b, a\n    return a + b\nprint(f(1, 2))\n",
			fn:   "f", want: []string{"a", "b"}, none: nil,
		},
		{
			// A float rebind is deliberately left to the float-kind path, which
			// already reads the slot; making the entry decide that slot's LLVM type
			// would duplicate a decision the store makes.
			name: "a float binding is not a scalar rebind",
			src:  "def f(n):\n    n = n / 2.0\n    return n\nprint(f(4))\n",
			fn:   "f", want: nil, none: []string{"n"},
		},
		{
			// A nested def owns its own names: rebinding `m` there says nothing
			// about the outer `m`, whose register is still the whole story.
			name: "a nested def's parameter is a different binding",
			src:  "def outer(m):\n    def inner(m):\n        m = m * 3\n        return m\n    return inner(m)\nprint(outer(2))\n",
			fn:   "outer", want: nil, none: []string{"m"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			prog := parseOrFatal(t, tc.src)
			fd := findFunc(t, prog, tc.fn)
			got := reboundParams(fd, floatFromSyntax)
			for _, w := range tc.want {
				if !got[w] {
					t.Errorf("param %q rebinds and is missing from %v", w, got)
				}
			}
			for _, n := range tc.none {
				if got[n] {
					t.Errorf("param %q must not be in %v", n, got)
				}
			}
		})
	}
}

func findFunc(t *testing.T, prog *Program, name string) *FuncDef {
	t.Helper()
	var fd *FuncDef
	var walk func([]Stmt)
	walk = func(list []Stmt) {
		for _, st := range list {
			switch s := st.(type) {
			case *FuncDef:
				if s.Name == name && fd == nil {
					fd = s
				}
				walk(s.Body)
			case *ClassDef:
				walk(s.Body)
			case *IfStmt:
				walk(s.Then)
				walk(s.Else)
			case *WhileStmt:
				walk(s.Body)
			case *ForStmt:
				walk(s.Body)
			}
		}
	}
	walk(prog.Stmts)
	if fd == nil {
		t.Fatalf("no function %q in the program", name)
	}
	return fd
}

// TestReboundParamGetsAnEntrySlot is the IR contract: the parameter's slot is
// allocated once, at the top of the entry block, and initialized from the incoming
// register — so every path through the body reads initialized storage, including a
// read textually before the first assignment.
func TestReboundParamGetsAnEntrySlot(t *testing.T) {
	ir := mustIR(t, `def bump(n):
    n = n + 1
    return n
print(bump(0))
`)
	body := irFunctions(t, ir)["bump"]
	if body == "" {
		t.Fatalf("no @bump in the module:\n%s", ir)
	}
	if !strings.Contains(body, "%_n = alloca i32") {
		t.Errorf("@bump never allocates the parameter's own slot:\n%s", body)
	}
	if !strings.Contains(body, "store i32 %p0, i32* %_n") {
		t.Errorf("@bump never copies the incoming argument into %%_n:\n%s", body)
	}
	if n := strings.Count(body, "%_n = alloca"); n != 1 {
		t.Errorf("@bump allocated %%_n %d times; llc calls a second one \"multiple definition of local value\":\n%s", n, body)
	}
	// The copy has to precede the body's own work, or a read on a path that reaches
	// it before any assignment loads whatever the stack happened to hold.
	allocaAt := strings.Index(body, "%_n = alloca")
	storeAt := strings.Index(body, "store i32 %p0, i32* %_n")
	addAt := strings.Index(body, "add i32")
	if !(allocaAt >= 0 && storeAt > allocaAt && addAt > storeAt) {
		t.Errorf("the entry copy must come before the body computes with the parameter (alloca=%d store=%d add=%d):\n%s", allocaAt, storeAt, addAt, body)
	}
}

// TestUnreboundParamKeepsItsRegister: the fix must not cost a slot per parameter. A
// body that never assigns to one still reads the argument register directly.
func TestUnreboundParamKeepsItsRegister(t *testing.T) {
	ir := mustIR(t, `def add(a, b):
    return a + b
print(add(1, 2))
`)
	body := irFunctions(t, ir)["add"]
	for _, name := range []string{"%_a = alloca", "%_b = alloca"} {
		if strings.Contains(body, name) {
			t.Errorf("a parameter nobody assigns to should not need a slot (%s):\n%s", name, body)
		}
	}
	if !strings.Contains(body, "add i32 %p0, %p1") {
		t.Errorf("@add should compute from the argument registers:\n%s", body)
	}
}

// TestMethodParamGetsAnEntrySlot is the same contract for a method, whose self is
// register %self and whose parameters start at %p1.
func TestMethodParamGetsAnEntrySlot(t *testing.T) {
	ir := mustIR(t, `class C:
    def bumped(self, n):
        n = n + 1
        return n
print(C().bumped(3))
`)
	var body string
	for name, b := range irFunctions(t, ir) {
		if strings.HasSuffix(name, "bumped") {
			body = b
		}
	}
	if body == "" {
		t.Fatal("no method ending in bumped in the module")
	}
	if !strings.Contains(body, "%_n = alloca i32") || !strings.Contains(body, "store i32 %p1, i32* %_n") {
		t.Errorf("the method never copied its rebound parameter into a slot:\n%s", body)
	}
	if n := strings.Count(body, "%_n = alloca"); n != 1 {
		t.Errorf("the method allocated %%_n %d times:\n%s", n, body)
	}
}

// TestRangeLoopHasItsOwnCounter pins the other half of the round. The loop used to
// drive iteration through the loop variable's slot, which meant the variable answered
// the *bound* after the loop (3 where Python answers 2), and an assignment to it in
// the body moved the iteration — `for i in range(3): i = i * 100` ran twice.
func TestRangeLoopHasItsOwnCounter(t *testing.T) {
	ir := mustIR(t, `total = 0
for i in range(3):
    total = total + i
print(total)
print(i)
`)
	if !strings.Contains(ir, "_ctr1 = alloca i32") {
		t.Errorf("the range loop has no counter of its own:\n%s", ir)
	}
	if strings.Contains(ir, "add i32 %_i.ld") {
		t.Errorf("the loop still increments the user's loop variable:\n%s", ir)
	}
	// The variable is bound from the counter at the top of the body — that store is
	// the whole difference between "the counter is the variable" and Python.
	if !strings.Contains(ir, "store i32 %_ctr1.ld") {
		t.Errorf("the loop variable is never bound from the counter:\n%s", ir)
	}
}

// TestFloatRetParamAllocatedOnce: a float-returning function already copies every
// parameter into a double slot. The body assigning to one must reuse it — before the
// registration it emitted a second alloca of the same name and llc refused the module.
func TestFloatRetParamAllocatedOnce(t *testing.T) {
	ir := mustIR(t, `def scale(x):
    x = x * 2.0
    return x + 0.0
print(scale(1.5))
`)
	body := irFunctions(t, ir)["scale"]
	if n := strings.Count(body, "%_x = alloca"); n != 1 {
		t.Errorf("@scale allocated %%_x %d times, want 1:\n%s", n, body)
	}
	if !strings.Contains(body, "store double %p0, double* %_x") {
		t.Errorf("@scale never copied its float argument into the slot:\n%s", body)
	}
}

// TestReboundParamSemanticsBothWays is the behaviour, on the path that runs source
// directly: the interpreter is the reference here, and every case below is one the
// compiled backend used to get wrong without a diagnostic.
func TestReboundParamSemantics(t *testing.T) {
	cases := []struct {
		src  string
		want int64
	}{
		{"def bump(n):\n    n = n + 1\n    return n\nbump(0)\n", 1},
		{"def twice(n):\n    n = n * 2\n    n = n + 1\n    return n\ntwice(3)\n", 7},
		{"def acc(n):\n    total = 0\n    while n > 0:\n        total = total + n\n        n = n - 1\n    return total\nacc(4)\n", 10},
		{"def clamp(x):\n    if x < 0:\n        x = 0\n    return x\nclamp(-4)\n", 0},
		{"def count(n):\n    last = 0\n    for n in range(3):\n        last = n\n    return last\ncount(9)\n", 2},
		{"class C:\n    def bumped(self, n):\n        n = n + 1\n        return n\nC().bumped(3)\n", 4},
		{"def outer(n):\n    def inner(m):\n        m = m * 3\n        return m\n    return inner(n)\nouter(2)\n", 6},
	}
	for _, tc := range cases {
		v, _, err := evalGolden(t, tc.src)
		if err != nil {
			t.Errorf("evalGolden(t, %q): %v", tc.src, err)
			continue
		}
		if v != tc.want {
			t.Errorf("evalGolden(t, %q) = %d, want %d", tc.src, v, tc.want)
		}
	}
}
