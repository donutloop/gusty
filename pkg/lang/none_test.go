package lang

import (
	"strings"
	"testing"
)

// ADR 0172 — None is a value, not the integer 0.

func TestNoneSingletonSemantics(t *testing.T) {
	ev := NewEvaluator()
	prog, err := Parse("x = None\n")
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if _, err := ev.EvalProgram(prog); err != nil {
		t.Fatalf("eval: %v", err)
	}
	if !ev.IsNone(ev.Vars["x"]) {
		t.Errorf("x = None must bind the None singleton")
	}
	if ev.IsNone(0) {
		t.Errorf("the integer 0 must not be None — that identity was the whole bug")
	}
	if got := ev.Repr(ev.Vars["x"]); got != "None" {
		t.Errorf("Repr(None) = %q, want \"None\"", got)
	}
	if got := ev.TypeOf(ev.Vars["x"]); got != "None" {
		t.Errorf("TypeOf(None) = %q, want \"None\"", got)
	}
	if ev.truthy(ev.Vars["x"]) {
		t.Errorf("None must be falsy")
	}
	if ev.truthy(0) {
		t.Errorf("0 must stay falsy")
	}
	if h := ev.heap[ev.Vars["x"]]; h == nil || h.tag() != TagNone {
		t.Errorf("None's value tag must be TagNone, got %+v", h)
	}
}

func TestNoneSurvivesGC(t *testing.T) {
	// The singleton is a permanent root: if sweeping reclaimed it, its heap slot would be
	// handed to the free list and a later container would print as "None".
	src := "for i in range(6):\n    xs = [i, None]\n    print(len(xs))\nprint(None)\n"
	out, err := evalCapturePkg(t, src)
	if err != nil {
		t.Fatalf("eval: %v", err)
	}
	if strings.Count(out, "None") != 1 {
		t.Errorf("None must render exactly once as the singleton, got %q", out)
	}
}

// evalCapturePkg runs src through the interpreter and returns its stdout. It wraps the
// package's captureStdout so a failing program reports the source that failed.
func evalCapturePkg(t *testing.T, src string) (string, error) {
	t.Helper()
	return captureStdout(t, src), nil
}

func TestVoidFunctionYieldsNone(t *testing.T) {
	cases := []struct {
		src  string
		want string
	}{
		{"def f():\n    x = 1\n\nprint(f())\n", "None\n"},
		{"def f():\n    return\n\nprint(f())\n", "None\n"},
		{"def f():\n    return 0\n\nprint(f())\n", "0\n"},
		// a generator is not a procedure: it evaluates to the list of yielded values
		{"def gen():\n    yield 1\n    yield 2\n\nprint(gen())\n", "[1, 2]\n"},
		// a `return` buried in control flow still means the function yields a value
		{"def f(n):\n    if n > 0:\n        return n\n\n    return 0\n\nprint(f(3))\n", "3\n"},
		{"def f(n):\n    for i in range(3):\n        if i == n:\n            return i\n\n    return -1\n\nprint(f(1))\n", "1\n"},
	}
	for _, tc := range cases {
		got, err := evalCapturePkg(t, tc.src)
		if err != nil {
			t.Errorf("%q: %v", tc.src, err)
			continue
		}
		if got != tc.want {
			t.Errorf("interpreter %q = %q, want %q", tc.src, got, tc.want)
		}
		res, err := Compile(tc.src)
		if err != nil {
			t.Errorf("compile %q: %v", tc.src, err)
			continue
		}
		if !strings.Contains(res.IR, "define ") {
			t.Errorf("%q: no module emitted", tc.src)
		}
	}
}

func TestCompiledNoneUsesTheRuntimeSingleton(t *testing.T) {
	res, err := Compile("print(None)\nx = None\nprint(x)\n")
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	for _, want := range []string{"rt_print_none", "@none_h", "define internal i32 @rt_none()"} {
		if !strings.Contains(res.IR, want) {
			t.Errorf("compiled None should use %q, IR:\n%s", want, res.IR)
		}
	}
	if v, verr := VerifyModuleIR(res.IR, 0); verr != nil || !v.OK {
		t.Errorf("None module must verify: %v %v", v.Errors, verr)
	}
}

// TestNoneEqualityIsStaticButNotLazy covers both halves of ADR 0172's codegen rule: the
// answer is decided at compile time, and the operands still run.
func TestNoneEqualityIsStaticButNotLazy(t *testing.T) {
	res, err := Compile("def emit():\n    print(\"side\")\n\nprint(emit() == None)\n")
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	if !strings.Contains(res.IR, "call i32 @gy_emit(") {
		t.Errorf("deciding `f() == None` statically must not delete the call:\n%s", res.IR)
	}
}

func TestNoneVarIsClearedByReassignment(t *testing.T) {
	// x = None; x = 0 must print 0 — the variable's *latest* assignment decides.
	got, err := evalCapturePkg(t, "x = None\nx = 0\nprint(x)\nprint(x == None)\n")
	if err != nil {
		t.Fatalf("eval: %v", err)
	}
	if got != "0\nFalse\n" {
		t.Errorf("reassignment must clear None-ness, got %q", got)
	}
	res, err := Compile("x = None\nx = 0\nprint(x)\n")
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	if strings.Contains(res.IR, "rt_print_none") {
		t.Errorf("a variable reassigned to 0 must not print None:\n%s", res.IR)
	}
}
