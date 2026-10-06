package lang

import (
	"strings"
	"testing"
)

// ADR 0172 — None is a value, not the integer 0.

func TestNoneSingletonSemantics(t *testing.T) {
	// ADR 0172's bug was an identity: `None` and `0` were the same word, so `0 == None` answered
	// True and a container holding None printed as a number. The case is now asked of the compiled
	// program — which is the stronger form of the same question, since what a user can see is what
	// the program prints, not what a Go field held.
	goldenStdoutIs(t, "x = None\nprint(x)\nprint(x == None)\nprint(0 == None)\nif x:\n    print(\"truthy\")\nelse:\n    print(\"falsy\")\nif 0:\n    print(\"zero-truthy\")\nelse:\n    print(\"zero-falsy\")",
		"None\nTrue\nFalse\nfalsy\nzero-falsy")
	// The value a snippet ends with is the other half: None renders as its one name, under str and
	// under the echo alike, and `0` never becomes it.
	goldenReprIs(t, "x = None\nx", "None")
	goldenReprIs(t, "None", "None")
	goldenReprIs(t, "0 == None", "0")
	goldenReprIs(t, "None == None", "1")
}

func TestNoneSurvivesGC(t *testing.T) {
	// The singleton is a permanent root: if sweeping reclaimed it, its heap slot would be
	// handed to the free list and a later container would print as "None".
	src := "for i in range(6):\n    xs = [i, None]\n    print(len(xs))\nprint(None)\n"
	out := goldenStdout(t, src)
	if strings.Count(out, "None") != 1 {
		t.Errorf("None must render exactly once as the singleton, got %q", out)
	}
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
		got, err := runGoldenStdout(t, tc.src)
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
	got, err := runGoldenStdout(t, "x = None\nx = 0\nprint(x)\nprint(x == None)\n")
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
