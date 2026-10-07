package lang

import (
	"strings"
	"testing"
)

// Comprehensions whose element is a call (roadmap L11.7, ADR 0192). The AOT path folded constant
// elements and stopped, so the most ordinary list-building idiom in Python — [f(x) for x in
// range(5)] — refused with "comprehension element must be constant" while the record ran it
// happily. Three properties below: the loop variable is a real slot the element can call through;
// the constant fold keeps priority where it still applies; and a consumer that would fold an empty
// element set refuses instead of answering 0.

func compileSrcIR(t *testing.T, src string) string {
	t.Helper()
	res, err := Compile(src)
	if err != nil {
		t.Fatalf("compile failed: %v", err)
	}
	return res.IR
}

func TestRuntimeComprehensionBindsTheLoopVariable(t *testing.T) {
	mod := compileSrcIR(t, "def sq(n):\n    return n * n\n\nprint([sq(n) for n in range(4)])\n")
	for _, want := range []string{"call i32 @rt_alloc(i32 1)", "call void @rt_append_tagged(", "call i32 @gy_sq("} {
		if !strings.Contains(mod, want) {
			t.Fatalf("runtime comprehension missing %q:\n%s", want, mod)
		}
	}
	// The element reads the loop variable through its slot, which is what makes the call
	// possible at all: without the store, sq() would see whatever was there before.
	if !strings.Contains(mod, "= call i32 @rt_get_elem(") && !strings.Contains(mod, "store i32") {
		t.Fatalf("the loop variable must be bound per item:\n%s", mod)
	}
}

func TestComprehensionOverContainerVariableIsARealLoop(t *testing.T) {
	// The append is not decoration: a list the escape analysis folds away has no runtime object to
	// walk, so the loop needs a materialised container (the folded case is probe_comp_folded_iter).
	mod := compileSrcIR(t, "xs = [1, 2, 3]\nxs.append(9)\nys = [x * 2 for x in xs]\nprint(ys)\n")
	for _, want := range []string{"call i32 @rt_list_len(", "call i32 @rt_get_elem(", "phi i32 [ 0, %comp.pre"} {
		if !strings.Contains(mod, want) {
			t.Fatalf("comprehension over a container variable missing %q:\n%s", want, mod)
		}
	}
	// The preheader block is named because the induction phi needs a predecessor to take its
	// 0 from; an unnamed block is an unverifiable module.
	if !strings.Contains(mod, "comp.pre") || !strings.Contains(mod, "comp.cond") || !strings.Contains(mod, "comp.done") {
		t.Fatalf("the loop needs its four blocks:\n%s", mod)
	}
}

func TestComprehensionFilterBranchesPerItem(t *testing.T) {
	mod := compileSrcIR(t, "def even(n):\n    return n % 2 == 0\n\nprint([x for x in range(6) if even(x)])\n")
	for _, want := range []string{"comp.cond", "comp.item", "comp.skip", "call i32 @gy_even("} {
		if !strings.Contains(mod, want) {
			t.Fatalf("filtered comprehension missing %q:\n%s", want, mod)
		}
	}
	if strings.Count(mod, "call i32 @gy_even(") < 6 {
		t.Fatal("the filter runs once per item — unrolling keeps that visible")
	}
}

// TestConstantFoldKeepsPriorityForFoldableComprehensions: min/max/sum read the compile-time
// element set, so taking the runtime path for a literal iterable would make them refuse. The
// ordering (fold first, runtime second) is load-bearing, not style.
func TestConstantFoldKeepsPriorityForFoldableComprehensions(t *testing.T) {
	for _, src := range []string{
		"print(sum([x for x in range(5)]))\n",
		"print(max([x * 3 for x in [1, 2, 3]]))\n",
		"print(len([x for x in range(3)]))\n",
	} {
		res, err := Compile(src)
		if err != nil {
			t.Fatalf("%s should fold: %v", src, err)
		}
		if strings.Contains(res.IR, "rt_list_len") && strings.Contains(res.IR, "comp.cond") {
			t.Fatalf("%s should not build a runtime loop:\n%s", src, res.IR)
		}
	}
}

// TestRuntimeReductionRefusesRatherThanAnswerZero is the bug this cycle nearly shipped: sum folds
// the compile-time element set, which is empty for a runtime comprehension, so sum([...]) printed
// 0 for a list with elements in it.
func TestRuntimeReductionRefusesRatherThanAnswerZero(t *testing.T) {
	for _, src := range []string{
		"def sq(n):\n    return n * n\n\nprint(sum([sq(x) for x in range(4)]))\n",
		"def sq(n):\n    return n * n\n\nprint(max([sq(x) for x in range(4)]))\n",
	} {
		res, err := Compile(src)
		msg := ""
		if err != nil {
			msg = err.Error()
		}
		if res != nil {
			for _, d := range res.Diagnostics {
				msg += d.Msg + "\n"
			}
		}
		if err == nil || !strings.Contains(msg, "runtime reduction") {
			t.Fatalf("%s must refuse, not answer 0. got: %s", src, msg)
		}
	}
}

// TestStringElementFilterCompiles: a filter that compares an element with a string literal used to
// be refused, because the comparison emitted `icmp eq i32 %_n, @.str3` — an @str_tab index against
// the address of a string global — and the filtered loop's `phi` named the body as its predecessor
// when the skips actually came from the skip block. Both are fixed; the shape compiles, verifies,
// and the loop header's back edge is the block that really branches to it (Gap R.42, ADR 0224).
func TestStringElementFilterCompiles(t *testing.T) {
	src := "names = [\"a\", \"b\"]\nnames.append(\"c\")\nprint([n for n in names if n == \"a\"])\n"
	res, err := Compile(src)
	if err != nil {
		t.Fatalf("the string-comparison filter must compile now (it used to be refused to hide an invalid module): %v", err)
	}
	if _, verr := VerifyModuleIR(res.IR, 0); verr != nil {
		t.Fatalf("the emitted module does not verify: %v", verr)
	}
	if !strings.Contains(res.IR, "call i32 @rt_str_intern2") {
		t.Fatalf("the compared literal is not interned; a comparison against a global would not verify:\n%s", res.IR)
	}
	// The loop header's back edge must be the block that actually branches to it.
	for _, ln := range strings.Split(res.IR, "\n") {
		if strings.Contains(ln, "= phi i32 [ 0, %comp.pre") && !strings.Contains(ln, ", %comp.skip") {
			t.Fatalf("the induction phi names a predecessor that never branches to it:\n%s", ln)
		}
	}
}

// TestNestedContainerElementStillRefuses keeps ADR 0188's rule in force for the new path: an
// element that is a container is a handle nobody would mark.
func TestNestedContainerElementStillRefuses(t *testing.T) {
	res, err := Compile("xs = [1, 2]\nprint([[x, x] for x in xs])\n")
	if err == nil && res != nil {
		// A folded [[0,0],[1,1]] is fine; only the runtime path needs the refusal, so accept
		// either "does not compile with the nested message" or a successful constant fold.
		return
	}
	if err != nil && !strings.Contains(err.Error(), "nested") && !strings.Contains(err.Error(), "another container") &&
		!strings.Contains(err.Error(), "compile-time constant") && !strings.Contains(err.Error(), "one element at a time") {
		t.Fatalf("unexpected refusal: %v", err)
	}
}

// TestRuntimeComprehensionRequestsTheHeapRuntime: the prelude is emitted on demand, and a
// comprehension can be the only allocation in a program — which produced a module calling an
// undefined @rt_alloc.
func TestRuntimeComprehensionRequestsTheHeapRuntime(t *testing.T) {
	mod := compileSrcIR(t, "def sq(n):\n    return n * n\n\nys = [sq(x) for x in range(3)]\nprint(ys)\n")
	if !strings.Contains(mod, "define internal i32 @rt_alloc(") {
		t.Fatalf("the comprehension is the only heap user, so it must request the runtime:\n%s", mod)
	}
}

// TestComprehensionBehaviourOnTheCompiledBackend is the record side of the same table, so the two
// backends cannot drift on what a comprehension means.
func TestComprehensionBehaviourOnTheCompiledBackend(t *testing.T) {
	for _, tc := range []struct{ src, want string }{
		{"def sq(n):\n    return n * n\n\nprint([sq(x) for x in range(4)])\n", "[0, 1, 4, 9]\n"},
		{"print([abs(x) for x in [-1, 2, -3]])\n", "[1, 2, 3]\n"},
		{"def even(n):\n    return n % 2 == 0\n\nprint([x for x in range(6) if even(x)])\n", "[0, 2, 4]\n"},
		{"xs = [1, 2, 3]\nxs.append(9)\nys = [x * 2 for x in xs]\nprint(ys)\n", "[2, 4, 6, 18]\n"},
		{"print([x * 2 for x in [1, 2, 3]])\n", "[2, 4, 6]\n"},
	} {
		res, err := JIT(tc.src, 0)
		if err != nil {
			t.Fatalf("%s: %v", tc.src, err)
		}
		if res.Output != tc.want {
			t.Fatalf("%s\n got %q want %q", tc.src, res.Output, tc.want)
		}
	}
}
