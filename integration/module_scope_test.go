package integration

import (
	"strings"
	"testing"
	"time"

	"github.com/donutloop/gusty/pkg/lang"
)

// Integration coverage for Gap R.35 (roadmap), ADR 0220: the module is a scope, and a function
// reads it at call time. The interpreter leg is the one that works; the compiled leg is asserted
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
			iout := runInterp(t, tc.src)
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

// TestModuleScopeIsStillOutOfReachForCompiledCode pins a KNOWN compiled-backend defect, roadmap
// Gap R.35's remaining half, so it stays measured rather than remembered: the compiled leg cannot
// reach a module binding from a function body. Three shapes refuse, with a message whose claim about
// the interpreter is false (it reports no error at all — that untrue sentence is Gap R.38), and two
// shapes compile and print `0` where CPython prints the module's value — a silent wrong answer, the
// same signature as Gap R.36.
//
// DELETE THIS TEST when the compiled half of Gap R.35 lands, and move these programs into
// TestModuleScopeRunsLikeCPython's table plus the standalone corpus: asserting a refusal after the
// feature exists would be asserting a bug that no longer has a home.
func TestModuleScopeIsStillOutOfReachForCompiledCode(t *testing.T) {
	refusals := []struct{ name, src, needle string }{
		{"module constant", "v = 1\n\ndef g() -> int:\n    return v\n\nprint(g())\n", `undefined name "v"`},
		{"two functions, one constant", "T = 6\n\ndef a() -> int:\n    return T\n\ndef b() -> int:\n    return T * 2\n\nprint(a(), b())\n", `undefined name "T"`},
		{"constant read by a nested def", "c = 1\n\ndef get() -> int:\n    return c\n\nprint(get())\n", `undefined name "c"`},
		// Measured again in cycle 171: this shape used to compile and print `0`, because the
		// refusal below was emitted into a method body and thrown away (Gap R.41, ADR 0223).
		// A refusal is a bad answer but an honest one; a silent zero is not.
		{"constant read by a method", "k = 5\n\nclass C:\n    def get(self) -> int:\n        return k\n\nprint(C().get())\n", `undefined name "k"`},
	}
	for _, tc := range refusals {
		t.Run(tc.name, func(t *testing.T) {
			res, err := lang.Compile(tc.src)
			if err == nil {
				t.Fatalf("the compiled backend accepted a program it cannot run; if Gap R.35's compiled half has landed, delete this test\nIR:\n%s", res.IR)
			}
			if !strings.Contains(err.Error(), tc.needle) {
				t.Fatalf("compiled refusal = %q, want it to mention %q", err.Error(), tc.needle)
			}
		})
	}
	silentZero := []struct{ name, src string }{
		// Still a silent zero. A nested `def` reaching past its parent frame to the module is
		// lowered to a slot nothing writes; the method-shaped version of this no longer appears
		// here, because a method body's codegen refusal used to be thrown away (ADR 0223) and now
		// surfaces as the honest refusal in the table above.
		{"nested def reaches the module", "G = 2\n\ndef outer() -> int:\n    def inner() -> int:\n        return G\n    return inner()\n\nprint(outer())\n"},
	}
	for _, tc := range silentZero {
		t.Run(tc.name, func(t *testing.T) {
			out, err := runAOTWithTimeout(t, tc.src, 60*time.Second)
			if err != nil {
				t.Fatalf("this shape used to compile and print 0; it now fails differently — re-measure before changing Gap R.35: %v\n%s", err, out)
			}
			if strings.TrimSpace(out) != "0" {
				t.Fatalf("compiled output %q is no longer the pinned silent zero; if it prints the module value, Gap R.35 is fixed — delete this test", out)
			}
		})
	}
}
