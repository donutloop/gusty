package lang

import (
	"strings"
	"testing"
)

// Which runtime blocks a module needs used to be a set of booleans each codegen path had to remember
// to set — `heapUsed`, `raiseUsed`, `floatFmtUsed`. The paths that forgot produced modules that call a
// helper which is never defined, and LLVM's rejection of such a module is what the exit-code contract
// calls a compiler bug. The decision is now derived from the artifact: if the emitted code mentions a
// name a block defines, the block travels with the module (roadmap Gap R.2, ADR 0209).

func emittedModuleIR(t *testing.T, src string) string {
	t.Helper()
	res, err := Compile(src)
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	return res.IR
}

// calledRuntimeHelpers lists the `@rt_*` / `@gc.*` functions the module calls.
func calledRuntimeHelpers(ir string) []string {
	out := []string{}
	for _, ln := range strings.Split(ir, "\n") {
		t2 := strings.TrimSpace(ln)
		if !strings.Contains(t2, "call ") && !strings.Contains(t2, "invoke ") {
			continue
		}
		i := strings.Index(t2, "@")
		if i < 0 {
			continue
		}
		name := t2[i+1:]
		if j := strings.IndexAny(name, "( %,"); j >= 0 {
			name = name[:j]
		}
		if strings.HasPrefix(name, "rt_") || strings.HasPrefix(name, "gc.") {
			out = append(out, name)
		}
	}
	return out
}

func definesOrDeclares(ir, name string) bool {
	for _, ln := range strings.Split(ir, "\n") {
		t2 := strings.TrimSpace(ln)
		if strings.HasPrefix(t2, "define ") && strings.Contains(t2, "@"+name+"(") {
			return true
		}
		if strings.HasPrefix(t2, "declare ") && strings.Contains(t2, "@"+name+"(") {
			return true
		}
	}
	return false
}

// TestEveryCalledHelperIsDefined is the class assertion, phrased so it does not depend on which path
// made the call. The programs are chosen to reach the helpers through different shapes — a function
// body, a container write, a raise, an index read — because the bug was that only some shapes set the
// flag.
func TestEveryCalledHelperIsDefined(t *testing.T) {
	for _, tc := range []struct{ name, src string }{
		// R.2's repro, stripped of the await and the `while True` that were both red herrings:
		// a function returning a string interns its result, and that path never marked the
		// heap runtime as used.
		{"string returned from a function", "def txt():\n    return \"hi\"\n\nprint(txt())\n"},
		{"string returned, then used twice", "def txt():\n    return \"hi\"\n\nz = txt()\nprint(z)\nprint(z)\n"},
		{"interned container iteration", "xs = [\"a\", \"b\"]\nfor x in xs:\n    print(x)\n"},
		{"unrolled string literal", "for c in \"ab\":\n    print(c)\n"},
		{"raise", "raise ValueError(\"boom\")\n"},
		{"container index read", "xs = [1, 2]\nprint(xs[0])\n"},
		{"container iteration", "xs = [1, 2, 3]\nfor x in xs:\n    print(x)\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ir := emittedModuleIR(t, tc.src)
			called := calledRuntimeHelpers(ir)
			if len(called) == 0 {
				t.Fatalf("%s: the program calls no runtime helper, so it asserts nothing", tc.name)
			}
			for _, name := range called {
				if !definesOrDeclares(ir, name) {
					t.Errorf("%s: the module calls @%s without defining it — llc will reject it (Gap R.2's signature)", tc.name, name)
				}
			}
		})
	}
}

// TestStringReturningFunctionModuleVerifies states R.2 as the artifact fact that failed: the emitted
// module had to verify, not merely be produced.
func TestStringReturningFunctionModuleVerifies(t *testing.T) {
	src := "def txt():\n    return \"hi\"\n\nprint(txt())\n"
	ir := emittedModuleIR(t, src)
	if !strings.Contains(ir, "call i32 @rt_str_intern2(") {
		t.Fatalf("the program no longer interns its returned string, so this test checks nothing:\n%s", ir)
	}
	if !strings.Contains(ir, "define internal i32 @rt_str_intern2(") {
		t.Errorf("the module calls @rt_str_intern2 without defining it — that is Gap R.2:\n%s", ir)
	}
}

// TestRuntimeBlockReferencedIsATestOfTheCodeNotAFlag exercises the predicate directly, including the
// shapes that would fool a flag-based scheme: a call that appears only inside a function body, and a
// mention that is only prose.
func TestRuntimeBlockReferencedIsATestOfTheCodeNotAFlag(t *testing.T) {
	if !runtimeBlockReferenced(heapRuntimeIR, "  %x = call i32 @rt_str_intern2(i8* @.s1, i8* @.s2)\n") {
		t.Errorf("a call to a helper defined by the heap block was not detected — the derived rule would not fire")
	}
	if runtimeBlockReferenced(heapRuntimeIR, "  %x = call i32 @printf(i8* @.fmt)\n") {
		t.Errorf("a module that calls only libc pulled in the heap runtime: the rule must be a reference test, not a blanket")
	}
	if runtimeBlockReferenced(heapRuntimeIR, "; @rt_str_intern2 mentioned in a comment only\n") {
		t.Errorf("a comment naming a helper pulled in the runtime")
	}
	if !runtimeBlockReferenced(heapRuntimeIR, "  %slot = getelementptr [256 x i8*], [256 x i8*]* @str_tab, i32 0, i32 1\n") {
		t.Errorf("a reference to a block's data global was not detected")
	}
}

// TestRuntimeStringIterableIsRefusedRatherThanSilent: iterating text that only exists at run time
// used to compile cleanly and print nothing, because the string's table index was read as a repeat
// count — the archetypal silent wrong answer (roadmap Gap R.16, ADR 0209). It was a refusal for a
// while, and ADR 0230 gave it an answer: the loop runs over code points through the string table.
// What must never come back is the silent version, so the assertions are about the answer.
func TestRuntimeStringIterableIsRefusedRatherThanSilent(t *testing.T) {
	src := "def txt():\n    return \"hi\"\n\nfor c in txt():\n    print(c)\n"
	if got := captureStdout(t, src); got != "h\ni\n" {
		t.Fatalf("interpreted output = %q, want h i", got)
	}
	res, err := Compile(src)
	if err != nil {
		t.Fatalf("the compiled backend refuses a loop it can answer: %v", err)
	}
	if !strings.Contains(res.IR, "@rt_str_char") || !strings.Contains(res.IR, "@rt_str_nchars") {
		t.Fatalf("the loop does not go through the string table:\n%s", res.IR)
	}
	if !strings.Contains(res.IR, "_strctr") {
		t.Fatalf("the loop shares its counter with the variable's slot (ADR 0196):\n%s", res.IR)
	}
	if true {
		return
	}
	if !strings.Contains(err.Error(), "interpreter") || !strings.Contains(err.Error(), "string literal") {
		t.Errorf("the refusal does not name the working backend and the usable shape: %v", err)
	}
}
