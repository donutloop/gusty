package integration

import (
	"strings"
	"testing"

	"github.com/donutloop/gusty/pkg/lang"
)

// frame_slots_test.go — a body's stack slots belong to the frame that emits the body, at the CLI and
// against CPython (roadmap L11.6 · Gap R.342; ADR 0378).
//
// The unit file owns the rule and the IR; this file owns the two things only the CLI can witness: that the
// program the reference runs is the program the toolchain runs (both compiled legs, CPython's bytes, exit 0),
// and that the compiler-bug class is unreachable for these shapes — exit 2 anywhere below is a module `llc`
// rejected, which ADR 0166 says is never an answer a program gets. Before ADR 0378 several of these rows
// *were* exit 2: the nested `def` that shared a name with its enclosing body's float local died with
// `use of undefined value '%_w'`, the lifted `def` and the method with a possibly-unwritten local died with
// `use of undefined value '%bnd_z'`, and the nested `def` that bound the container or the text its enclosing
// body binds died with `use of undefined value '%_xs'` / `'%_s'` — three lines of ordinary source each.
//
// Every `want` is what the pinned CPython printed for that exact source, asserted in the test itself, so a
// row cannot be certified by the two compiled legs agreeing with each other (they share the table that was
// the bug).

var frameSlotsPrograms = []struct {
	name, src, want string
}{
	{
		// the row Gap R.342 was named for: `inner` and `outer` both bind `w`, and one of the two
		// allocas was missing from the module — the store survived, the `alloca` did not.
		name: "a_nested_def_sharing_a_name_with_its_enclosing_bodys_float_local",
		src: "def outer():\n    def inner():\n        w = 3\n        return w\n" +
			"    w = 1.5\n    return inner()\n\nprint(outer())\n",
		want: "3\n",
	},
	{
		name: "a_nested_def_binding_the_container_its_enclosing_body_binds",
		src: "def outer():\n    def inner():\n        xs = [7, 8]\n        return xs[0] + len(xs)\n" +
			"    xs = [1, 2]\n    return inner() + len(xs)\n\nprint(outer())\n",
		want: "11\n",
	},
	{
		name: "a_nested_def_binding_the_text_its_enclosing_body_binds",
		src: "def outer():\n    def inner():\n        s = \"hi\"\n        return len(s)\n" +
			"    s = \"hello\"\n    return inner() + len(s)\n\nprint(outer())\n",
		want: "7\n",
	},
	{
		name: "three_bodies_one_name",
		src: "def outer():\n    def a():\n        v = 1\n        return v\n    def b():\n        v = 2\n" +
			"        return v\n    v = 3\n    return a() + b() + v\n\nprint(outer())\n",
		want: "6\n",
	},
	{
		name: "a_method_and_a_function_binding_the_same_container_name",
		src: "class C:\n    def m(self):\n        ys = [1, 2]\n        return len(ys)\n\ndef use():\n" +
			"    ys = [3, 4, 5]\n    return C().m() + len(ys)\n\nprint(use())\n",
		want: "5\n",
	},
	{
		// main's written-flags were the ones a lifted closure paid for: the module's `q` got its alloca
		// inside the closure's define, and module-level code stored through it.
		name: "a_lifted_def_does_not_pay_for_the_modules_unwritten_local",
		src: "if 1 > 0:\n    q = 5\nprint(q)\n\ndef holder():\n    def nested():\n        return 1\n" +
			"    return nested()\n\nprint(q + holder())\n",
		want: "5\n6\n",
	},
	{
		// the controls: what the frame entry must leave exactly as it was.
		name: "a_nested_def_reading_the_number_its_frame_owns",
		src:  "def outer():\n    n = 40\n    def add(k):\n        return k + n\n    return add(2)\n\nprint(outer())\n",
		want: "42\n",
	},
	{
		name: "a_decorators_closure_still_runs_through_the_trampoline",
		src: "def deco(f):\n    def wrapper(x):\n        return f(x) + 1\n    return wrapper\n\n@deco\n" +
			"def add1(x):\n    return x + 1\n\nprint(add1(1))\n",
		want: "3\n",
	},
	{
		name: "a_method_local_beside_a_module_name_of_the_same_spelling",
		src:  "m = 2.5\n\nclass C:\n    def m1(self):\n        m = 3\n        return m\n\nprint(C().m1())\n",
		want: "3\n",
	},
}

// TestABodysSlotsAreItsOwnFrameAtTheCLI runs every shape through both compiled legs and asks CPython the
// same program. Exit 2 in any row here fails the file: it is the compiler accusing itself, and ADR 0166
// says the reader must never be handed that as an answer.
//
// Two shapes the same probe reached are NOT rows here, because ADR 0378 does not answer them and a green
// table must not imply otherwise: a lifted `def` whose body answers a double answers `0` at exit 0 (**Gap
// R.345**, pinned in `pkg/lang/frame_slots_test.go`), and a `def` nested inside a METHOD is not lifted at
// all (**Gap R.332** / **Gap R.337**, refused in words).
func TestABodysSlotsAreItsOwnFrameAtTheCLI(t *testing.T) {
	for _, tc := range frameSlotsPrograms {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			dir := t.TempDir()
			py, ok := cpythonPlainOut(t, dir, tc.src)
			if !ok {
				t.Fatalf("the reference failed to answer this program: src: %s", tc.src)
			}
			if py != tc.want {
				t.Fatalf("this table's expectation is not the reference's: table %q, CPython %q\nsrc: %s", tc.want, py, tc.src)
			}
			path := writeSrc(t, t.TempDir(), "frame_slots.gy", tc.src)
			for _, engine := range []string{"--jit", "--aot"} {
				out, code := cliRunCode(t, engine, path)
				if code == 2 {
					t.Fatalf("%s: the compiled leg produced a module the toolchain rejected — exit 2 is ADR 0166's "+
						"compiler-bug code:\n%s", engine, cliRun(t, engine, path))
				}
				if code != 0 {
					t.Fatalf("%s: exit %d, want 0: %s", engine, code, cliRun(t, engine, path))
				}
				if out != tc.want {
					t.Errorf("%s: printed %q, want CPython's %q\nsrc: %s", engine, out, tc.want, tc.src)
				}
			}
		})
	}
}

// TestAnUnwrittenLocalOfANestedDefOrMethodTrapsThroughTheCLI is the written-flag half, at the CLI: the trap
// the reference raises is the trap the program gets, at the run-failed code rather than the compiler-bug one,
// and an `except` arm takes it.
func TestAnUnwrittenLocalOfANestedDefOrMethodTrapsThroughTheCLI(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name, src, want string
		exit            int
	}{
		{
			name: "a_nested_defs_unwritten_local_raises",
			src: "def outer():\n    def inner(c):\n        if c:\n            z = 1\n        return z\n" +
				"    return inner(False)\n\nprint(outer())\n",
			want: "UnboundLocalError: cannot access local variable 'z' where it is not associated with a value",
			exit: 3,
		},
		{
			name: "a_methods_unwritten_local_raises",
			src: "class C:\n    def m(self, c):\n        if c:\n            z = 1\n        return z\n\n" +
				"print(C().m(False))\n",
			want: "UnboundLocalError: cannot access local variable 'z' where it is not associated with a value",
			exit: 3,
		},
		{
			name: "the arm runs and the program carries on",
			src: "def outer():\n    def inner(c):\n        if c:\n            z = 1\n        return z\n" +
				"    try:\n        return inner(False)\n    except UnboundLocalError:\n        return -1\n\n" +
				"print(outer())\nprint(\"after\")\n",
			want: "-1\nafter\n",
			exit: 0,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			out0, err0, rerr := lang.PythonRun(tc.src)
			if rerr != nil && err0 == "" && out0 == "" {
				t.Fatalf("the reference neither answered nor trapped: %v\nsrc: %s", rerr, tc.src)
			}
			if !strings.Contains(out0+err0, tc.want) {
				t.Fatalf("the reference does not say %q (says %q / %q)\nsrc: %s", tc.want, out0, err0, tc.src)
			}
			path := writeSrc(t, t.TempDir(), "frame_slots_trap.gy", tc.src)
			_, code := cliRunCode(t, "--aot", path)
			if code == 2 {
				t.Fatalf("%s: exit 2 — the compiler produced a module the toolchain rejected (ADR 0166):\n%s",
					tc.name, cliRun(t, "--aot", path))
			}
			if code != tc.exit {
				t.Fatalf("%s: exit %d, want %d: %s", tc.name, code, tc.exit, cliRun(t, "--aot", path))
			}
			// The trap sentence lives on stderr, where a traceback belongs; `cliRunCode` answers stdout
			// alone because the answer rows above assert on it, so the message is asked of the combined run.
			out := cliRun(t, "--aot", path)
			if !strings.Contains(out, tc.want) {
				t.Fatalf("%s: the compiled leg says %q, the reference says %q\nsrc: %s", tc.name, out, tc.want, tc.src)
			}
		})
	}
}

// TestTheFrameSlotAuditIsTheCompilersSentenceNotLlcIs asks the machine path about a compiler-bug event the
// compiler caught itself: the class ADR 0166 reserves for a broken artifact, reachable through `--json` as
// data with the function and the slot named, and with the exit code the same one an `llc` rejection gets.
// (The audit itself is unit-tested in pkg/lang/frame_slots_test.go; what this row owns is that the CLI does
// not turn a compiler bug into a source error, which is ADR 0211's rule one class further along.)
func TestTheFrameSlotAuditIsTheCompilersSentenceNotLlcIs(t *testing.T) {
	t.Parallel()
	for _, tc := range frameSlotsPrograms {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			path := writeSrc(t, t.TempDir(), "frame_slots_json.gy", tc.src)
			out, code := cliRunCode(t, "--aot", "--json", path)
			if code == 2 && !strings.Contains(out, "internal invariant") {
				t.Fatalf("exit 2 without the compiler's own sentence — the reader was handed %q", out)
			}
			if code != 0 {
				t.Fatalf("%s: exit %d, want 0: %s", tc.name, code, out)
			}
		})
	}
}
