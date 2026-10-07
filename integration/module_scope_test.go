package integration

import (
	"strings"
	"testing"
	"time"

	"github.com/donutloop/gusty/pkg/lang"
)

// Integration coverage for Gap R.35 (roadmap), ADR 0220: the module is a scope, and a function
// reads it at call time. the record leg is the one that works; the compiled leg is asserted
// for what it does today by TestModuleScopeIsStillOutOfReachForCompiledCode below.

func TestModuleScopeRunsLikeCPython(t *testing.T) {
	cases := []struct{ name, src, want string }{
		{"module constant", "v = 1\n\ndef g() -> int:\n    return v\n\nprint(g())\n", "1\n"},
		{"defined below the def", "def g() -> int:\n    return v\n\nv = 40\nprint(g())\n", "40\n"},
		{"module list", "xs = [1, 2, 3]\n\ndef n() -> int:\n    return len(xs)\n\nprint(n())\n", "3\n"},
		{"method reads a module name", "k = 5\n\nclass C:\n    def get(self) -> int:\n        return k\n\nprint(C().get())\n", "5\n"},
		{"nested def reaches the module", "G = 2\n\ndef outer() -> int:\n    def inner() -> int:\n        return G\n    return inner()\n\nprint(outer())\n", "2\n"},
		{"closure still wins", "q = 100\n\ndef outer():\n    q = 3\n    def inner():\n        return q\n    return inner()\n\nprint(outer())\n", "3\n"},
		{"parameter shadows", "n = 7\n\ndef f(n: int) -> int:\n    return n * 2\n\nprint(f(4), n)\n", "8 7\n"},
		{"assignment in the body is local", "t = 1\n\ndef f() -> int:\n    t = 99\n    return t\n\nprint(f(), t)\n", "99 1\n"},
		{"mutating a module container", "xs = [1]\n\ndef add() -> int:\n    xs.append(2)\n    return len(xs)\n\nprint(add(), xs)\n", "2 [1, 2]\n"},
		{"the module is read live", "c = 1\n\ndef get() -> int:\n    return c\n\nprint(get())\nc = 5\nprint(get())\n", "1\n5\n"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			iout := runCompiled(t, tc.src)
			if iout != tc.want {
				t.Fatalf("interpreter printed %q, want %q\nsource:\n%s", iout, tc.want, tc.src)
			}
			pout, perrText, perr := lang.PythonRun(tc.src)
			if perr != nil {
				t.Fatalf("CPython disagreed with the expectation %q: %v\n%s", tc.want, perr, perrText)
			}
			if pout != tc.want {
				t.Fatalf("CPython printed %q, want %q — the expectation itself is wrong", pout, tc.want)
			}
		})
	}
}

// TestModuleScalarsReachCompiledFunctionBodies is the other side of that record: the compiled half of
// Gap R.35 (ADR 0227). A module binding that cannot change is a value, so a body may read it; one the
// module rebinds lives in a module global, so a body reads what the module had assigned when the call
// ran (ADR 0220). A container is still refused, and refused with the true reason.
func TestModuleScalarsReachCompiledFunctionBodies(t *testing.T) {
	cases := []struct{ name, src, want string }{
		{"module constant in a function", "MAX = 40\n\ndef twice() -> int:\n    return MAX * 2\n\nprint(twice())\n", "80"},
		{"constant read by a method", "k = 5\n\nclass C:\n    def get(self) -> int:\n        return k\n\nprint(C().get())\n", "5"},
		{"constant read by a nested def", "G = 2\n\ndef outer() -> int:\n    def inner() -> int:\n        return G\n    return inner()\n\nprint(outer())\n", "2"},
		{"rebound module name read at call time", "LATE = 0\n\ndef read() -> int:\n    return LATE\n\nLATE = 3\nprint(read())\n", "3"},
		{"a body's binding is its own", "K = 5\n\ndef f() -> int:\n    K = 1\n    return K\n\nprint(f(), K)\n", "1 5"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			want, perrText, perr := lang.PythonRun(tc.src)
			if perr != nil {
				t.Fatalf("CPython disagreed with the expectation %q: %v\n%s", tc.want, perr, perrText)
			}
			if want != tc.want+"\n" {
				t.Fatalf("CPython printed %q, want %q — the expectation itself is wrong", want, tc.want+"\n")
			}
			if got := runCompiled(t, tc.src); got != tc.want+"\n" {
				t.Fatalf("interpreter printed %q, want %q", got, tc.want+"\n")
			}
			res, err := lang.Compile(tc.src)
			if err != nil {
				t.Fatalf("compile: %v", err)
			}
			if _, err := lang.VerifyModuleIR(res.IR, 0); err != nil {
				t.Fatalf("the emitted module does not verify: %v\n%s", err, res.IR)
			}
			if out, err := runAOTWithTimeout(t, tc.src, 60*time.Second); err != nil || out != tc.want+"\n" {
				t.Fatalf("compiled output %q (err %v), want %q", out, err, tc.want+"\n")
			}
		})
	}
	// A rebound name read by a body is module state, and module state is a global: an alloca in
	// main's frame would be gone by the time the callee ran.
	res, err := lang.Compile("LATE = 0\n\ndef read() -> int:\n    return LATE\n\nLATE = 3\nprint(read())\n")
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	if !strings.Contains(res.IR, "@gy_mod_LATE = global i32") {
		t.Fatalf("the module binding is not module state; the IR must keep it in a global the callee can read\n%s", res.IR)
	}
	// The shape that used to print 0 through a dropped closure body must never do so again.
	if strings.Contains(res.IR, "ret i32 0\n}\n\ndef ") && strings.Contains(res.IR, "gy_mod") {
		t.Logf("note: an empty function body precedes another definition; check it is not a dropped body")
	}
	if out, err := runAOTWithTimeout(t, "G = 2\n\ndef outer() -> int:\n    def inner() -> int:\n        return G\n    return inner()\n\nprint(outer())\n", 60*time.Second); err != nil || out != "2\n" {
		t.Fatalf("the nested-def shape printed %q (err %v), want 2 — the closure body must not be dropped", out, err)
	}
	// Containers stay out of reach, and say so truthfully: they are module state, not strings.
	if _, err := lang.Compile("xs = [1, 2]\n\ndef n() -> int:\n    return len(xs)\n\nprint(n())\n"); err == nil {
		t.Fatalf("the compiled backend accepted a body reading a module container; that needs the container machinery (Gap R.35's remaining half)")
	} else if !strings.Contains(err.Error(), "bound at module level") || strings.Contains(err.Error(), "non-string") {
		t.Fatalf("the refusal must name module state, not misdescribe the value as a string: %q", err.Error())
	}
}
