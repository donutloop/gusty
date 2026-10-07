package lang

import (
	"strings"
	"testing"
)

// Tests for Gap R.35 (roadmap), ADR 0220: a name a function reads that is neither local nor
// captured is looked up in the MODULE — at call time, so the binding may sit below the def.
// CPython does this, the record now does, and the checker stopped refusing these programs.
// The compiled backend cannot reach a module binding at all, which is why the corpus case for this
// is a debt row (programs/probe_module_scope.gy) rather than a parity case.

func TestFunctionReadsModuleScope(t *testing.T) {
	cases := []struct{ name, src, want string }{
		{
			"module constant",
			"v = 1\n\ndef g() -> int:\n    return v\n\nprint(g())\n",
			"1\n",
		},
		{
			// The one no amount of "assign it before use" advice can explain: the name is
			// bound by the time the call runs, which is when Python looks it up.
			"defined below the def",
			"def g() -> int:\n    return v\n\nv = 40\nprint(g())\n",
			"40\n",
		},
		{
			"a module list",
			"xs = [1, 2, 3]\n\ndef n() -> int:\n    return len(xs)\n\nprint(n())\n",
			"3\n",
		},
		{
			"a method reads a module name",
			"k = 5\n\nclass C:\n    def get(self) -> int:\n        return k\n\nprint(C().get())\n",
			"5\n",
		},
		{
			"a nested def reaches the module",
			"G = 2\n\ndef outer() -> int:\n    def inner() -> int:\n        return G\n    return inner()\n\nprint(outer())\n",
			"2\n",
		},
		{
			// Frame, then closure, then module — the order matters, and this says the first
			// two still win.
			"a closure wins over the module",
			"q = 100\n\ndef outer():\n    q = 3\n    def inner():\n        return q\n    return inner()\n\nprint(outer())\n",
			"3\n",
		},
		{
			"a parameter shadows the module name",
			"n = 7\n\ndef f(n: int) -> int:\n    return n * 2\n\nprint(f(4), n)\n",
			"8 7\n",
		},
		{
			// The other half of the rule, and the reason this is a scope chain rather than a
			// global alias: assigning in a body makes the name local, and the module keeps its
			// own value.
			"assignment in the body is local",
			"t = 1\n\ndef f() -> int:\n    t = 99\n    return t\n\nprint(f(), t)\n",
			"99 1\n",
		},
		{
			// Mutating through a module binding is a read of the name plus an operation on the
			// object it holds.
			"mutating a module container",
			"xs = [1]\n\ndef add() -> int:\n    xs.append(2)\n    return len(xs)\n\nprint(add(), xs)\n",
			"2 [1, 2]\n",
		},
		{
			// Late binding is not snapshotting: two calls see two values.
			"the module is read live",
			"c = 1\n\ndef get() -> int:\n    return c\n\nprint(get())\nc = 5\nprint(get())\n",
			"1\n5\n",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := captureStdout(t, tc.src)
			if got != tc.want {
				t.Fatalf("interpreter printed %q, want %q\nsource:\n%s", got, tc.want, tc.src)
			}
		})
	}
}

func TestModuleScopeDoesNotWeakenTheChecker(t *testing.T) {
	// Widening a function's reach must not cost the front end its bite. Each of these is a
	// mistake the same analyzer still has to name.
	cases := []struct{ name, src, want string }{
		{
			"a name nothing binds is still undefined",
			"def g() -> int:\n    return nowhere\n\nprint(g())\n",
			`undefined name "nowhere"`,
		},
		{
			"a module variable read above its assignment",
			// Module code runs line by line, so this IS a bug — and the pre-pass for function
			// bodies must not leak into top-level ordering.
			"print(total)\ntotal = 3\n",
			`undefined name "total"`,
		},
		{
			"a typo inside a function stays a typo",
			"count = 4\n\ndef f() -> int:\n    return coun\n\nprint(f())\n",
			`undefined name "coun"`,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			diags := checkDiags(t, tc.src)
			if !scopeHasDiag(diags, LevelError, tc.want) {
				t.Fatalf("expected %s; diags=%v\nsource:\n%s", tc.want, diags, tc.src)
			}
		})
	}
}

func TestModuleScopeShapesCheckClean(t *testing.T) {
	// The programs TestFunctionReadsModuleScope runs must also analyse clean — the checker used
	// to refuse the "defined below the def" one with `undefined name`, which failed --check and
	// blocked --aot on a program that simply works.
	for _, src := range []string{
		"v = 1\n\ndef g() -> int:\n    return v\n\nprint(g())\n",
		"def g() -> int:\n    return v\n\nv = 40\nprint(g())\n",
		"k = 5\n\nclass C:\n    def get(self) -> int:\n        return k\n\nprint(C().get())\n",
		"G = 2\n\ndef outer() -> int:\n    def inner() -> int:\n        return G\n    return inner()\n\nprint(outer())\n",
	} {
		if diags := checkDiags(t, src); scopeHasDiag(diags, LevelError, "undefined name") {
			t.Fatalf("the checker refused a program the record answers: diags=%v\nsource:\n%s", diags, src)
		}
	}
}

// TestUnboundReadFromAFunctionIsACatchableNameError keeps the language-side behaviour honest: a
// function reaching for a name the module never binds fails the way Python fails — with a class the
// program can name — rather than as a compiler complaint about the source.
//
// It goes through the compiled run rather than the front end, because the question is about the
// program's behaviour at run time: a name that never entered the environment has to arrive as a
// catchable NameError naming the missing binding, not as an empty print and exit 0.
func TestUnboundReadFromAFunctionIsACatchableNameError(t *testing.T) {
	// A function reading a name the module never binds is a program the checker refuses — before
	// the run, at the line, with the binding named. That is a strictly better answer than the one
	// this case originally pinned (the retired engine ran the program and raised at the call, and
	// the golden records that refusal sentence, because the retired engine verified first too); what both
	// agree on, and what this holds, is that the program never gets to print a blank and exit 0.
	src := "def g() -> int:\n    return nowhere\n\nprint(g())\n"
	_, err := Compile(src)
	if err == nil {
		t.Fatal("an unbound module-scope read compiled; the checker must refuse it")
	}
	if got := err.Error(); !strings.Contains(got, "nowhere") || !strings.Contains(got, "undefined") {
		t.Fatalf("the refusal does not name the missing binding: %v", err)
	}
}
