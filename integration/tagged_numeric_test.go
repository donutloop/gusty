package integration

import (
	"strings"
	"testing"
)

// TestTaggedNumericKeepsTheBoundsCheck asks the one question the door could have quietly broken: the
// read now goes to the float arms with a (payload, tag) pair, so does the index still get checked?
// It does — the compiled path die, and neither prints a number. The *wording* of the sentence is a
// separate gap (the compiled backend says `index out of range` where CPython says `list index out of
// range`), recorded as Gap R.89 rather than fixed under this feature.
func TestTaggedNumericKeepsTheBoundsCheck(t *testing.T) {
	for _, src := range []string{
		"xs = [1.5, 2.5]\ni = 5\nprint(xs[i] + 1)\n",
		"xs = [1.5, 2.5]\ni = -3\nprint(xs[i] * 2)\n",
	} {
		path := writeSrc(t, t.TempDir(), "tagged_numeric_bounds.gy", src)
		for _, engine := range cliEngines {
			out, code := cliRunCode(t, engine, path)
			if code == 0 {
				t.Errorf("%s printed a number for an out-of-range slot read: %q", engine, out)
			}
			if code == 2 {
				t.Errorf("%s exited 2 (compiler bug, ADR 0166) on an out-of-range slot read:\n%s", engine, out)
			}
			// The report goes to stderr, which cliRunCode does not capture.
			if combined := cliRun(t, engine, path); !strings.Contains(combined, "IndexError") {
				t.Errorf("%s did not report an IndexError: %s", engine, combined)
			}
		}
	}
}

// A number the object describes, at the command line (roadmap L11.1's open clause, Gap R.88).
//
// The unit table in pkg/lang/tagged_numeric_test.go holds the hand-derived expectations; this is the
// file with the oracle in it, so a row cannot be written that flatters the compiler. Three things are
// checked here that were not checked before, and each is a way this feature could have been faked:
//
//   - the answers come from python3, for the compiled path, including the float forms Python prints for
//     results that start life as literal ints (`10 / 4` is 2.5, not 2);
//   - the traps are traps: the compiled program has to raise CPython's own sentence, not merely be
//     refused by the compiler, which is how the old build passed a test like this one;
//   - what *is* refused leaves through the refusal exit class. `xs[i] / 4` used to reach `llc` as
//     `fdiv double , %t1` and exit 2, and ADR 0166 counts that as the compiler's bug, so exit 2 on
//     any row here is a failure of this file, not a passing one.

func TestTaggedNumericSlotUsesMatchCPython(t *testing.T) {
	for _, tc := range []struct{ name, src string }{
		{
			// The program that used to exit 2: an empty operand in the fdiv.
			"true_division_of_an_int_slot_through_a_computed_index",
			"xs = [10, 4]\ni = 0\nprint(xs[i] / 4)\n",
		},
		{
			"true_division_of_a_float_slot",
			"xs = [10.0, 4.0]\ni = 0\nprint(xs[i] / 4)\n",
		},
		{
			"a_float_slot_against_a_number_in_every_operator",
			"xs = [1.5, 2.5]\ni = 0\nprint(xs[i] + 1, xs[i] - 1, xs[i] * 2, xs[i] / 2, xs[i] // 1, xs[i] % 2, xs[i] ** 2)\n",
		},
		{
			"an_int_slot_against_a_number_in_every_operator",
			"xs = [6, 7]\ni = 0\nprint(xs[i] + 1, xs[i] - 1, xs[i] * 2, xs[i] / 4, xs[i] // 4, xs[i] % 4)\n",
		},
		{
			"a_slot_read_held_by_a_loop_index",
			"xs = [1.5, 2.5, \"x\"]\nfor i in [0, 1]:\n    print(xs[i] + 1)\n",
		},
		{
			"a_fold_over_slots",
			"xs = [1.5, 2.5, \"x\"]\ntotal = 0.0\nfor i in [0, 1]:\n    total = total + xs[i]\nprint(total)\n",
		},
		{
			"a_bool_slot_is_the_number_it_behaves_like",
			"xs = [True, 1]\ni = 0\nprint(xs[i] + 1)\nprint(xs[i] * 3)\n",
		},
		{
			"a_slot_negated",
			"xs = [1.5, \"x\"]\ni = 0\nprint(-xs[i])\n",
		},
		{
			// The comparisons are asked in the `1 if ... else 0` spelling rather than printed raw,
			// A slot used as a number asks the object the same question the printer asks it, and the
			// comparison itself is a verdict the front end can name: `print(xs[i] > 1.0)` prints True on
			// the compiled path and in CPython (ADR 0257's predicate for the result, ADR 0259's tag for the
			// operand). The `1 if ... else 0` spelling stays pinned because it is the shape that was
			// written when a bool still printed as a number.
			"a_slot_ordered_against_a_number",
			"xs = [1.5, \"a\"]\ni = 0\nprint(1 if xs[i] > 1.0 else 0)\nprint(1 if xs[i] < 1.0 else 0)\nprint(1 if xs[i] >= 1.5 else 0)\n",
		},
		{
			"a_slot_ordered_against_a_number_prints_the_verdict",
			"xs = [1.5, 2.5]\ni = 0\nprint(xs[i] > 1.0)\nprint(xs[i] < 1.0)\nprint(xs[i] == 2.5)\n",
		},
		{
			// A container handed to a function, from the refusal list: a literal in a value position is
			// built as the heap object its slots need, so the body's arithmetic has something to ask.
			"a_float_slot_handed_to_a_function",
			"def half(xs):\n    return xs[0] / 2\n\nprint(half([1.5]))\n",
		},
		{
			"a_slot_in_a_condition_decides_a_branch",
			"xs = [1.5, \"a\"]\ni = 0\nif xs[i] > 1.0:\n    print(\"big\")\nelse:\n    print(\"small\")\n",
		},
		{
			"an_int_slot_ordered_against_a_number",
			"xs = [1, \"a\"]\ni = 0\nprint(1 if xs[i] > 2 else 0)\nprint(1 if xs[i] <= 1 else 0)\n",
		},
		{
			"a_negative_index_the_program_computed",
			"xs = [1.5, 2.5]\nk = -1\nprint(xs[k] + 1)\n",
		},
		{
			// The slot and a plain variable: the variable's kind is known, the slot's travels with it.
			"a_slot_and_a_variable",
			"xs = [1.5, \"a\"]\ni = 0\nk = 2\nprint(xs[i] + k)\n",
		},
		{
			// A variable the compiler can name on one side, a slot on the other.
			"a_slot_multiplied_by_a_variable",
			"xs = [1.5, 2.5]\ni = 0\nk = 2\nprint(xs[i] * k)\n",
		},
		{
			// Two reads of one container, both through an index.
			"two_slot_reads_of_one_container",
			"xs = [1.5, 2.5]\ni = 0\nj = 1\nprint(xs[i] + xs[j])\nprint(1 if xs[i] < xs[j] else 0)\n",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			path := writeSrc(t, t.TempDir(), "tagged_numeric.gy", tc.src)
			pyOut, ok := cpythonOut(t, path)
			if !ok {
				t.Skip("no oracle")
			}
			for _, engine := range cliEngines {
				out, code := cliRunCode(t, engine, path)
				if code != 0 {
					t.Errorf("%s exited %d on a program the oracle answers\nsrc:\n%s\noutput:\n%s", engine, code, tc.src, out)
					continue
				}
				if out != pyOut {
					t.Errorf("%s printed %q, want CPython's %q\nsrc:\n%s", engine, out, pyOut, tc.src)
				}
			}
		})
	}
}

// A slot holding something with no number in it: the compiled program has to *raise*, per kind, the
// way the interpreter does. The old build passed a test shaped like this by refusing to compile, so
// each row asserts the sentence itself arrives, and not a `codegen:` line.
func TestTaggedNumericSlotTrapsAreRaisedNotRefused(t *testing.T) {
	for _, tc := range []struct {
		name, src, want string
		// aotOnly marks the one row whose interpreter half is wrong for a reason of its own: the
		// interpreter has never consulted a tag for a unary operator, and prints a garbage number
		// where CPython raises. That is roadmap Gap R.89, so the compiled path is pinned here and
		// the record's answer is not laundered into a pass by this table.
		aotOnly bool
	}{
		{
			// The pair that tells the two messages apart: same slot, same container, one operator
			// each — and `"a" + 1` is not `"a" - 1`.
			"text_in_the_slot_added",
			"xs = [1.5, \"a\"]\ni = 1\nprint(xs[i] + 1)\n",
			"can only concatenate str (not \"int\") to str", false,
		},
		{
			"text_in_the_slot_subtracted",
			"xs = [1.5, \"a\"]\ni = 1\nprint(xs[i] - 1)\n",
			"unsupported operand type(s) for -: 'str' and 'int'", false,
		},
		{
			"text_in_the_slot_divided",
			"xs = [1.5, \"a\"]\ni = 1\nprint(xs[i] / 2)\n",
			"unsupported operand type(s) for /: 'str' and 'int'", false,
		},
		{
			"text_in_the_slot_floor_divided",
			"xs = [1.5, \"a\"]\ni = 1\nprint(xs[i] // 1)\n",
			"unsupported operand type(s) for //: 'str' and 'int'", false,
		},
		{
			"None_in_the_slot",
			"xs = [1.5, None]\ni = 1\nprint(xs[i] - 1)\n",
			"unsupported operand type(s) for -: 'NoneType' and 'int'", false,
		},
		{
			"a_container_in_the_slot_subtracted",
			"xs = [1.5, [7]]\ni = 1\nprint(xs[i] - 1)\n",
			"unsupported operand type(s) for -: 'list' and 'int'", false,
		},
		{
			// An ordering needs no float result, so an all-int container is admitted here and the
			// text slot has to say so — which is the row that used to be a compile-time refusal in
			// mixed_list_test.go.
			"text_in_the_slot_ordered_against_a_number",
			"xs = [1, \"a\"]\ni = 1\nprint(xs[i] > 1)\n",
			"'>' not supported between instances of 'str' and 'int'", false,
		},
		{
			"text_in_the_slot_ordered_the_other_way_round",
			"xs = [1, \"a\"]\ni = 1\nprint(1 > xs[i])\n",
			"'>' not supported between instances of 'int' and 'str'", false,
		},
		{
			// The loop variable is the same read with the index computed by the loop. Both legs raise:
			// the reference of this row was Gap R.89's `aotOnly` pin, and ADR 0266 paid it.
			"text_in_the_slot_through_a_loop_index",
			"xs = [1.5, \"a\"]\ni = 1\nprint(-xs[i])\n",
			"bad operand type for unary -: 'str'", false,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			path := writeSrc(t, t.TempDir(), "tagged_numeric_trap.gy", tc.src)
			py, pyCode := oracleTrap(t, path)
			if pyCode == 0 ||
				!strings.Contains(py, "Traceback (most recent call last):") ||
				!strings.Contains(py, tc.want) {
				t.Fatalf("the oracle does not raise what the table claims: exit %d, %q", pyCode, py)
			}
			engines := cliEngines
			if tc.aotOnly {
				engines = []string{"--aot"}
			}
			for _, engine := range engines {
				out, code := cliRunCode(t, engine, path)
				if code == 0 {
					t.Errorf("%s exited 0 on a program the oracle dies on: stdout=%q", engine, out)
				}
				if code == 2 {
					t.Errorf("%s exited 2 (compiler bug, ADR 0166) on a program the oracle dies on:\n%s", engine, out)
				}
				combined := cliRun(t, engine, path)
				if strings.Contains(combined, "codegen:") {
					t.Errorf("%s refused at compile time what the oracle raises at run time:\n%s", engine, combined)
				}
				if !strings.Contains(combined, "Traceback (most recent call last):") || !strings.Contains(combined, tc.want) {
					t.Errorf("%s did not raise %q:\n%s", engine, tc.want, combined)
				}
			}
		})
	}
}

// What the door still will not answer stays a refusal — exit 1, the debt named, and the compiler
// never reaching `llc` with a half-built instruction (ADR 0166's exit-class rule, which is the whole
// reason the float arm records an unlift-able operand instead of emitting `fdiv double , %t1`).
func TestTaggedNumericSlotRefusalsLeaveTheCompilerOutOfIt(t *testing.T) {
	for _, tc := range []struct{ name, src string }{
		{
			"a_container_the_literal_no_longer_describes",
			"xs = []\nxs.append(1.5)\nxs.append(\"a\")\ni = 0\nprint(xs[i] + 1)\n",
		},
		// `def half(xs): return xs[0] / 2` / `print(half([1.5]))` is not on this list any more: it
		// answers 0.75 on the compiled path and in CPython, and moved to the parity table above. The gate
		// on a literal in a value position asked only literalNeedsHeap — "does a payload not fit an
		// i32?" — so a float-holding literal fell through to the static global emitter and was refused
		// there; it now asks ADR 0233's second question too, "can a slot report its own kind?", which
		// builds the heap object the answer needs (roadmap Gap R.112, ADR 0259).
		{
			// Ints here, floats there, both from the same literal: the result's kind is a run-time
			// question, which is the tagged value word Gap R.82 still owes.
			"int_and_float_slots_of_one_literal",
			"xs = [1, 2.5]\ni = 0\nj = 1\nprint(xs[i] + xs[j])\n",
		},
		{
			// `xs[i] * 2` over a container that can hold text is repetition in Python, not a TypeError,
			// and this backend has no repetition (Gap R.33).
			"repetition_shaped_multiplication",
			"xs = [\"ab\", 1]\ni = 0\nprint(xs[i] * 2)\n",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			path := writeSrc(t, t.TempDir(), "tagged_numeric_refusal.gy", tc.src)
			// What used to be corroborated by the tag-carrying interpreter is corroborated by the
			// reference now: the shape is legal Python and CPython answers it, so the compiled path owes
			// either the answer or a refusal that says which half is missing (the sentence is pinned by
			// the table). A mute refusal is the failure mode this row keeps its eye on.
			if _, code := cliRunCode(t, "--aot", path); code == exitIRVerify {
				t.Fatalf("exit 2 — LLVM rejected the module gusty emitted (ADR 0166):\n%s", cliRun(t, "--aot", path))
			} else if code != 0 {
				combined := cliRun(t, "--aot", path)
				if code != 1 || !refusesHonestly(combined) {
					t.Errorf("exit %d on a program the reference evaluates, without naming the missing half:\n%s", code, combined)
				}
				noteCompiledGap(t, tc.src, combined)
			}
			out, code := cliRunCode(t, "--aot", path)
			report := cliRun(t, "--aot", path) // stdout and stderr together: the refusal is on stderr
			if code == 2 {
				t.Errorf("the compiled backend exited 2 — a module llc rejected, which ADR 0166 counts as our bug:\n%s", report)
			}
			if code == 0 {
				t.Errorf("the compiled backend answered a shape it cannot prove (stdout %q)", out)
			}
			if !strings.Contains(report, "codegen:") {
				t.Errorf("refusal did not come from the front end:\n%s", report)
			}
			if !strings.Contains(report, "roadmap") {
				t.Errorf("refusal did not name the debt:\n%s", report)
			}
			if strings.Contains(report, "llvm") || strings.Contains(report, "verifier") {
				t.Errorf("refusal leaked an assembler complaint:\n%s", report)
			}
		})
	}
}
