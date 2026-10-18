package integration

import (
	"testing"
)

// float_frame_test.go — the float-variable table is a frame fact, at the CLI and against CPython
// (roadmap L11.6, Gap R.339; ADR 0375).
//
// `g.floatVars` says whose last assignment in a body produced a double, and the whole module shared
// one map: a body read whatever the previously-emitted body had written. The measured defect is two
// functions and one name —
//
//	def bound():
//	    u = 9.5
//	    return u
//
//	def lastWrite():
//	    u = 2.5
//	    u = 3
//	    return u
//
//	print(bound())       # 9.5
//	print(lastWrite())   # CPython 3 · this toolchain 3.0, exit 0
//
// — and the same second body in a file of its own answered `3`, which is the signature of an answer
// read off the emission order rather than off the program. ADR 0375 installs the table per body, in
// the one place the module already installs every other per-frame kind table (`beginScope`, plus
// `enterBody` for the roads — the method road and the closure lift — that emit a body without it).
//
// Every row below is CPython's own bytes. Exit 2 in any row here fails the file: a module `llc`
// rejects is a compiler bug (ADR 0166), never an answer a program gets — the one filed exception is
// the row that names itself as Gap R.342 and says what to delete when it goes away.

var floatFramePrograms = []struct {
	name, src, want string
}{
	{
		name: "two_bodies_one_name_the_second_int",
		src: "def bound():\n    u = 9.5\n    return u\n\n" +
			"def lastWrite():\n    u = 2.5\n    u = 3\n    return u\n\n" +
			"print(bound())\nprint(lastWrite())\n",
		want: "9.5\n3\n",
	},
	{
		name: "the_same_two_bodies_in_the_other_order",
		src: "def lastWrite():\n    u = 2.5\n    u = 3\n    return u\n\n" +
			"def bound():\n    u = 9.5\n    return u\n\n" +
			"print(lastWrite())\nprint(bound())\n",
		want: "3\n9.5\n",
	},
	{
		name: "three_bodies_one_name_three_kinds",
		src:  "def a():\n    v = 1.5\n    return v\n\ndef b():\n    v = 7\n    return v\n\ndef c():\n    v = 2.0\n    return v\n\nprint(a(), b(), c())\n",
		want: "1.5 7 2.0\n",
	},
	{
		name: "one_int_body_between_two_float_calls",
		src:  "def a():\n    v = 1.5\n    return v\n\ndef b():\n    v = 7\n    return v\n\nprint(b(), a(), b())\n",
		want: "7 1.5 7\n",
	},
	{
		name: "a_methods_float_local_is_not_the_next_functions_float",
		src:  "class Box:\n    def show(self):\n        u = 9.5\n        return 1\n\ndef later():\n    u = 3\n    return u\n\nprint(Box().show())\nprint(later())\n",
		want: "1\n3\n",
	},
	{
		name: "a_functions_float_local_is_not_the_next_methods_float",
		src:  "def first():\n    s = 2.5\n    return s\n\nclass Box:\n    def show(self):\n        s = 3\n        return s\n\nprint(first())\nprint(Box().show())\n",
		want: "2.5\n3\n",
	},
	{
		name: "the_int_body_alone",
		src:  "def lastWrite():\n    u = 2.5\n    u = 3\n    return u\n\nprint(lastWrite())\n",
		want: "3\n",
	},
	{
		name: "the_float_body_alone",
		src:  "def bound():\n    u = 9.5\n    return u\n\nprint(bound())\n",
		want: "9.5\n",
	},
	{
		name: "mains_float_local_is_not_the_callees_local",
		src:  "def body():\n    u = 3\n    return u\n\nu = 9.5\nprint(body())\nprint(u)\n",
		want: "3\n9.5\n",
	},
	{
		name: "a_promoted_parameter_is_not_the_next_body_s_float",
		src:  "def addf(x):\n    x = x + 1.5\n    return x\n\ndef later():\n    x = 4\n    return x\n\nprint(addf(1.0))\nprint(later())\n",
		want: "2.5\n4\n",
	},
	{
		name: "a_float_returning_neighbour_leaves_an_int_body_alone",
		src:  "def half(x):\n    return x / 2\n\ndef later():\n    h = 5\n    return h\n\nprint(half(3))\nprint(later())\n",
		want: "1.5\n5\n",
	},
	{
		name: "a_lambda_applied_inside_a_float_owning_body",
		src:  "def mixed():\n    z = 2.5\n    return (lambda: 8)()\n\nprint(mixed())\n",
		want: "8\n",
	},
	{
		// the control for Gap R.341: the same accumulator the body cannot answer, answered at module level
		name: "the_accumulator_at_module_level",
		src:  "t = 0.0\nfor i in [1, 2, 3]:\n    t = t + i\nprint(t)\n",
		want: "6.0\n",
	},
}

// TestWhatFloatTableABodyReadsAnswersLikeCPython runs the table through both compiled legs and asks
// CPython the same program, so a row cannot be certified by the two compiled legs agreeing with each
// other (they share the table that was the bug).
func TestWhatFloatTableABodyReadsAnswersLikeCPython(t *testing.T) {
	for _, tc := range floatFramePrograms {
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
			path := writeSrc(t, t.TempDir(), "float_frame.gy", tc.src)
			for _, engine := range []string{"--jit", "--aot"} {
				out, code := cliRunCode(t, engine, path)
				if code == 2 {
					t.Fatalf("%s: the compiled leg rejected the compiler's own module (ADR 0166):\n%s",
						engine, cliRun(t, engine, path))
				}
				if code != 0 {
					t.Fatalf("%s: exit %d, want 0: %s", engine, code, cliRun(t, engine, path))
				}
				if out != tc.want {
					t.Errorf("%s: %s printed %q, want CPython's %q\nsrc: %s", engine, engine, out, tc.want, tc.src)
				}
			}
		})
	}
}

// The row this file used to keep here — Gap R.342, the nested `def` that stored through its ENCLOSING
// frame's alloca and left exit 2 with `llc`'s `use of undefined value '%_w'` — is DELETED, as its own text
// required: ADR 0378 gives every road that emits a body the frame entry that body's slots live in
// (`pkg/lang/frame_slots.go`), and the program now prints the reference's `3` on the compiled leg, the record
// leg and the reference leg. It answers in `integration/frame_slots_test.go`, beside the CLI rows that fail on
// exit 2 in any shape and the conformance program
// `integration/programs/nested_def_shares_a_slot.gy` (asserted, `OracleMatch`).
//
// The table went with the row rather than staying behind: an empty answer table passes, and a passing test
// that asserts nothing is how a deleted row keeps its green (the lesson the ADR 0377 cycle wrote down). What
// this file still owns is the frame rule's own answer table above (`floatFramePrograms`), and the two rows
// that keep ADR 0375's frame rule apart from Gap R.35's module-state question — `pkg/lang/module_float_test.go`
// owns those rows (`TestTheModuleFloatDoorDoesNotAnswerWhatTheFrameAnswers`,
// `TestTheModuleFloatGateRefusesWhatItCannotPromise`), and what they refuse is still refused.
