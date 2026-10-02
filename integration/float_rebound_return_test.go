package integration

import (
	"strings"
	"testing"
)

// float_rebound_return_test.go — the function's return word comes from what the body does with the
// value, at the CLI and against CPython (roadmap L11.6, Gap R.3c; ADR 0254).
//
// The measured defect, on the books since ADR 0196 and pinned as a debt row in the oracle ledger:
//
//	def addf(x):
//	    x = x + 1.5
//	    return x
//	print(addf(1.0))   # CPython 2.5 · --interp 2.5 · --aot answered 1, exit 0
//
// The argument type and the return type were decided twice, from two different pieces of evidence —
// the call sites for the arguments, the *shape of the return expression* for the answer — and a body
// that stores a double into its parameter's slot had no word left to return it in. The compiled leg
// printed the incoming argument and exited 0: a number-shaped answer that is simply the wrong one.
//
// The gate now reads the body. Where the answer is that double, and every parameter can share the
// `double` word the convention hands out, the function is emitted double-returning and the program
// answers; where a parameter carries a heap handle or an interned text the function is refused in
// words, because emitting it anyway is the module `llc` rejects (exit 2, ADR 0166's own class).
// Exit 2 in any row here fails the file.

func TestAFunctionReturningAReboundFloatAnswersLikeCPython(t *testing.T) {
	for _, tc := range []struct{ name, src, want string }{
		{"param_rebound_and_returned_bare",
			"def addf(x):\n    x = x + 1.5\n    return x\n\nprint(addf(1.0))\n", "2.5\n"},
		{"param_rebound_returned_with_an_int_argument",
			"def addf(x):\n    x = x + 1.5\n    return x\n\nprint(addf(2))\n", "3.5\n"},
		{"param_rebound_returned_with_a_bool_argument",
			"def addf(x):\n    x = x + 1.5\n    return x\n\nprint(addf(True))\n", "2.5\n"},
		{"local_bound_to_a_float_and_returned_bare",
			"def f(x):\n    y = x + 0.5\n    return y\n\nprint(f(1.0))\n", "1.5\n"},
		{"returned_through_an_arithmetic_expression",
			"def k(x):\n    x = x + 1.5\n    return x + 0\n\nprint(k(1.0))\n", "2.5\n"},
		{"returned_through_abs",
			"def f(x):\n    x = x + 1.5\n    return abs(x)\n\nprint(f(1.0))\n", "2.5\n"},
		{"returned_negated",
			"def f(x):\n    x = x + 1.5\n    return -x\n\nprint(f(1.0))\n", "-2.5\n"},
		{"returned_through_abs_of_a_negation",
			"def f(x):\n    x = x + 1.5\n    return abs(-x)\n\nprint(f(1.0))\n", "2.5\n"},
		{"returned_through_int_which_is_an_int_anyway",
			"def f(x):\n    x = x + 1.5\n    return int(x)\n\nprint(f(1.0))\n", "2\n"},
		{"returned_through_round_which_is_an_int_anyway",
			"def f(x):\n    x = x + 1.5\n    return round(x)\n\nprint(f(1.0))\n", "2\n"},
		{"two_parameters_one_rebound",
			"def acc(a, b):\n    a = a + 0.5\n    b = b + 1\n    return a\n\nprint(acc(1.0, 2))\nprint(acc(2, 3))\n", "1.5\n2.5\n"},
		{"return_inside_an_if",
			"def f(x):\n    x = x + 1.5\n    if x > 2:\n        return x\n    return 0.0\n\nprint(f(2.0), f(0.0))\n", "3.5 0.0\n"},
		{"return_inside_a_while",
			"def f(x):\n    while x > 1:\n        x = x - 0.5\n    return x\n\nprint(f(2.0))\n", "1.0\n"},
		{"recursive_promoted_function",
			"def fib(n):\n    n = n + 0.0\n    if n < 2:\n        return n\n    return fib(n - 1) + fib(n - 2)\n\nprint(fib(6))\n", "8.0\n"},
		{"promoted_function_with_a_default",
			"def g(x=2.0):\n    x = x + 1.5\n    return x\n\nprint(g())\nprint(g(1.0))\n", "3.5\n2.5\n"},
		{"answer_used_in_a_larger_expression",
			"def h(x):\n    x = x + 1.5\n    return x\n\nprint(h(1.0) + h(2.0))\n", "6.0\n"},
		{"promoted_function_beside_a_nested_def",
			"def outer(x):\n    x = x + 0.5\n    def inner(y):\n        return y\n    return x + inner(0)\n\nprint(outer(1.0))\n", "1.5\n"},
		// The convention always saw these; pinned so promotion cannot unsee them.
		{"return_expression_already_a_float",
			"def scaled(x):\n    x = x * 2.0\n    return x + 0.0\n\nprint(scaled(1.5))\n", "3.0\n"},
		{"param_rebound_to_an_int_is_still_an_int",
			"def f(x):\n    x = x + 1\n    return x\n\nprint(f(1))\n", "2\n"},
		{"param_rebound_to_text_is_still_text",
			"def f(s):\n    s = \"b\"\n    return s\n\nprint(f(\"a\"))\n", "b\n"},
		{"no_rebind_is_unaffected",
			"def f(x):\n    return x * 2\n\nprint(f(3))\n", "6\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			path := writeSrc(t, t.TempDir(), "float_return.gy", tc.src)
			if py, ok := cpythonOut(t, path); ok && py != tc.want {
				t.Fatalf("the expectation is not CPython's: got %q want %q", py, tc.want)
			}
			for _, engine := range []string{"--interp", "--aot"} {
				out, code := cliRunCode(t, engine, path)
				if code == 2 {
					t.Fatalf("%s rejected the compiler's own module (ADR 0166 / exit-code contract):\n%s",
						engine, cliRun(t, engine, path))
				}
				if code != 0 {
					t.Fatalf("%s exited %d: %s", engine, code, cliRun(t, engine, path))
				}
				if out != tc.want {
					t.Fatalf("%s printed %q, want CPython's %q", engine, out, tc.want)
				}
			}
		})
	}
}

// TestAReboundFloatReturnIsRefusedWhereItCannotBeCarried pins the honest half: the convention that
// picks the return word picks every parameter's word with it, so a signature that also carries a
// container handle or an interned text cannot be promoted — and emitting it anyway is `sitofp i32
// @.lst1 to double`, which `llc` rejects. Those are exit 1 with the missing word named, never exit 2.
func TestAReboundFloatReturnIsRefusedWhereItCannotBeCarried(t *testing.T) {
	for _, tc := range []struct{ name, src, want string }{
		{
			"container_parameter_in_the_signature",
			"def f(xs, y):\n    y = y + 0.5\n    return y\n\nprint(f([1, 2], 1.0))\n",
			"the double the body computed has no word to travel in",
		},
		{
			"text_parameter_in_the_signature",
			"def f(x, s):\n    x = x + 0.5\n    return x\n\nprint(f(1.0, \"z\"))\n",
			"which its own body binds to a float",
		},
		// The two rows above could, in principle, be carried — a body that never reads the
		// container or the text leaves the convention nothing to get wrong. These two cannot:
		// the body reads the parameter, so forcing the `double` word through the signature hands
		// `rt_print_str` a `double %p1` where it wants the interned index, and `xs[0]` a
		// `sitofp i32 @.lst1`. Both are the module `llc` rejects — exit 2, ADR 0166's own class —
		// which is why the gate asks before writing a single instruction.
		{
			"text_parameter_the_body_reads",
			"def f(x, s):\n    x = x + 0.5\n    print(s)\n    return x\n\nprint(f(1.0, \"z\"))\n",
			"which its own body binds to a float",
		},
		{
			"container_parameter_the_body_reads",
			"def f(xs, y):\n    y = y + 0.5\n    print(xs[0])\n    return y\n\nprint(f([7, 2], 1.0))\n",
			"the double the body computed has no word to travel in",
		},
		{
			"ternary_arm_holding_the_rebound_parameter",
			// Answers a truncated `1` with exit 0 up to this round: the return word came from the
			// shape of the return expression, and a ternary says nothing either (Gap R.102).
			"def f(x):\n    x = x + 1.5\n    return x if x > 2 else 0.0\n\nprint(f(1.0))\n",
			"no number-typed `select`",
		},
		{
			"method_ternary_arm_holding_the_rebound_parameter",
			// The same shape on the path with no double word at all: also `1` with exit 0, and
			// refused by the method gate for every shape the return reads the name in.
			"class C:\n    def m(self, x):\n        x = x + 1.5\n        return x if x > 2 else 0.0\n\nc = C()\nprint(c.m(1.0))\n",
			"rebound to a float by this body and returned by bare name",
		},
		{
			"method_returning_a_rebound_float",
			"class C:\n    def m(self, x):\n        x = x + 0.5\n        return x\n\nc = C()\nprint(c.m(1.0))\n",
			"rebound to a float by this body and returned by bare name",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			path := writeSrc(t, t.TempDir(), "float_return_refuse.gy", tc.src)
			// The interpreter answers all three — this is the compiled backend's own limit, and the
			// refusal says so rather than answering the argument.
			if out, code := cliRunCode(t, "--interp", path); code != 0 || out == "" {
				t.Fatalf("the interpreter leg failed (%d): %s", code, cliRun(t, "--interp", path))
			}
			_, code := cliRunCode(t, "--aot", path)
			if code == 2 {
				t.Fatalf("the compiled leg rejected the compiler's own module (ADR 0166):\n%s", cliRun(t, "--aot", path))
			}
			if code != 1 {
				t.Fatalf("exit %d, want 1 (a front-end refusal)", code)
			}
			combined := cliRun(t, "--aot", path)
			if !strings.Contains(combined, tc.want) {
				t.Errorf("refusal does not name the missing word (%q):\n%s", tc.want, combined)
			}
			for _, bad := range []string{"LLVM ERROR", "verifier", "must have pointer type", "defined with type"} {
				if strings.Contains(combined, bad) {
					t.Errorf("the refusal arrived as a toolchain problem (%s):\n%s", bad, combined)
				}
			}
		})
	}
}

// TestFloatReturnCorpusProgramsAgreeOnBothLegs is the corpus-side pin: the promoted program that has
// been a debt row since ADR 0196 is parity surface now, and the matrix says so with a `match` verdict
// rather than a pin that expects a failure.
func TestFloatReturnCorpusProgramsAgreeOnBothLegs(t *testing.T) {
	for _, name := range []string{"probe_float_param_rebind"} {
		path := writeSrc(t, t.TempDir(), name+".gy", readProgramSrc(name))
		py, ok := cpythonOut(t, path)
		if !ok {
			t.Skip("no CPython to act as the oracle")
		}
		for _, engine := range []string{"--interp", "--aot"} {
			out, code := cliRunCode(t, engine, path)
			if code != 0 || out != py {
				t.Fatalf("%s printed %q (exit %d), want the oracle's %q\n%s", engine, out, code, py, cliRun(t, engine, path))
			}
		}
	}
}
