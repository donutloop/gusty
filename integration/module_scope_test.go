package integration

import (
	"strings"
	"testing"
	"time"

	"github.com/donutloop/gusty/pkg/lang"
)

// Integration coverage for Gap R.35 (roadmap), ADR 0220: the module is a scope, and a function
// reads it at call time. The record leg is the answer on record; the compiled leg is what this
// file asks — scalars since **ADR 0227**, the module `double` since **ADR 0376**, and a read-only
// literal container handed to the reading frame as a binding of that literal since **ADR 0407**
// (`module_container_test.go` is that row's family at this altitude).

func TestModuleScopeRunsLikeCPython(t *testing.T) {
	t.Parallel()
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
			t.Parallel()
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
// ran (ADR 0220). The container half of that sentence closed with **ADR 0407** — a read-only literal the
// module bound once is handed to the reading frame as a binding of that literal — so this case now walks
// the read at this altitude too, and keeps the write-through shape refused.
func TestModuleScalarsReachCompiledFunctionBodies(t *testing.T) {
	t.Parallel()
	cases := []struct{ name, src, want string }{
		{"module constant in a function", "MAX = 40\n\ndef twice() -> int:\n    return MAX * 2\n\nprint(twice())\n", "80"},
		{"constant read by a method", "k = 5\n\nclass C:\n    def get(self) -> int:\n        return k\n\nprint(C().get())\n", "5"},
		{"constant read by a nested def", "G = 2\n\ndef outer() -> int:\n    def inner() -> int:\n        return G\n    return inner()\n\nprint(outer())\n", "2"},
		{"rebound module name read at call time", "LATE = 0\n\ndef read() -> int:\n    return LATE\n\nLATE = 3\nprint(read())\n", "3"},
		{"a body's binding is its own", "K = 5\n\ndef f() -> int:\n    K = 1\n    return K\n\nprint(f(), K)\n", "1 5"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
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
	// Containers reached the compiled bodies with **ADR 0407**: a name the module bound ONCE to a literal
	// container, and never writes through, is handed to the reading body as a binding of that literal, so
	// the frame owns the slot, the root and every notebook the container doors read by name. The shape that
	// used to be refused here answers, on both CLI roads, and its module holds no `@gy_mod_` global for the
	// name — a container is a binding a frame owns, not module state (roadmap Gap R.35's container half).
	containerSrc := "xs = [1, 2]\n\ndef n() -> int:\n    return len(xs)\n\nprint(n())\n"
	if want, _, perr := lang.PythonRun(containerSrc); perr != nil || want != "2\n" {
		t.Fatalf("CPython disagreed with the container expectation: %q (%v)", want, perr)
	}
	if got := runCompiled(t, containerSrc); got != "2\n" {
		t.Fatalf("a body reading the module's list printed %q, want 2", got)
	}
	res, err = lang.Compile(containerSrc)
	if err != nil {
		t.Fatalf("a body reading the module's literal container was refused: %v", err)
	}
	if strings.Contains(res.IR, "@gy_mod_xs") {
		t.Fatalf("the module's container went to a global; the copy is a frame binding, not module state\n%s", res.IR)
	}
	if _, err := lang.VerifyModuleIR(res.IR, 0); err != nil {
		t.Fatalf("the emitted module does not verify: %v\n%s", err, res.IR)
	}
	if out, err := runAOTWithTimeout(t, containerSrc, 60*time.Second); err != nil || out != "2\n" {
		t.Fatalf("the linked program printed %q (err %v), want 2", out, err)
	}
	// And the half ADR 0407 deliberately did NOT open: a program that WRITES through the module's object
	// keeps the refusal, because a per-frame copy answers whichever literal the emitter copied rather than
	// whichever object the call reads (roadmap Gap R.398).
	if _, err := lang.Compile("xs = [1, 2]\n\ndef push():\n    xs.append(3)\n\npush()\nprint(xs)\n"); err == nil {
		t.Fatalf("the gate accepted a body writing through a module container; a copy is not the object")
	} else if !strings.Contains(err.Error(), "bound at module level") || strings.Contains(err.Error(), "non-string") {
		t.Fatalf("the refusal must name module state, not misdescribe the value as a string: %q", err.Error())
	}
}
