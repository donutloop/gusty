package integration

import (
	"strings"
	"testing"
)

// An ordering whose operands are slots, at the command line (roadmap L11.1's ordering clause, Gap R.82).
//
// `xs = [1, "a"]` holds a number and a text, so `xs[i] > "z"` is three programs in one: the two numbers
// CPython orders, the two texts it orders, and the pair it refuses with a TypeError. Which one it is can
// only be answered by the tag the object carries, and the compiled backend now emits all three arms and
// lets the tags pick (ADR 0250).
//
// Three things are checked here that the unit table in pkg/lang cannot:
//
//   - the expectations come from python3, for both backends, so a row cannot be written that flatters
//     the compiler;
//   - the traps really trap: the compiled program has to raise CPython's own sentence rather than be
//     refused by the compiler, which is how a build could otherwise pass a table like this one;
//   - what is refused leaves through the refusal exit class. Exit 2 means LLVM rejected a module the
//     compiler wrote, which ADR 0166 counts as the compiler's bug, so exit 2 anywhere in this file is a
//     failure — including on a row whose answer is still owed.

func TestSlotOrderingMatchesCPython(t *testing.T) {
	for _, tc := range []struct{ name, src string }{
		{
			"two_text_slots_of_a_container_that_also_holds_numbers",
			"xs = [\"b\", \"a\", 1]\nprint(1 if xs[0] > xs[1] else 0)\nprint(1 if xs[1] > xs[0] else 0)\n",
		},
		{
			"two_number_slots_of_a_container_that_also_holds_text",
			"xs = [1, \"a\", 2]\nprint(1 if xs[0] < xs[2] else 0)\nprint(1 if xs[2] <= xs[0] else 0)\n",
		},
		{
			"a_slot_that_could_be_a_number_or_a_text_ordered_against_text",
			"xs = [1, \"a\"]\nprint(1 if xs[1] > \"a\" else 0)\nprint(1 if xs[1] < \"z\" else 0)\nprint(1 if xs[1] >= \"a\" else 0)\n",
		},
		{
			"the_same_slot_through_an_index_the_program_computes",
			"xs = [1, \"a\"]\ni = 0\nprint(1 if xs[i] < 5 else 0)\n",
		},
		{
			"the_number_on_the_left_in_the_source",
			"xs = [1, \"a\"]\nprint(1 if 5 > xs[0] else 0)\n",
		},
		{
			"a_float_slot_ordered_against_a_number",
			"xs = [1.5, \"a\"]\nprint(1 if xs[0] < 2 else 0)\nprint(1 if xs[0] > 2 else 0)\n",
		},
		{
			"an_int_slot_ordered_against_a_float",
			"xs = [1, \"a\"]\nprint(1 if xs[0] < 1.5 else 0)\n",
		},
		{
			"ordering_in_a_condition_of_several_parts",
			"xs = [\"b\", 1]\nif xs[0] > \"a\" and xs[0] < \"c\":\n    print(\"between\")\n",
		},
		{
			"ordering_of_a_slot_in_a_while_condition",
			"xs = [1, \"a\"]\ni = 1\nwhile xs[i] < \"z\":\n    print(\"step\")\n    break\n",
		},
		{
			"ordering_of_a_text_only_list_still_reads_the_text_not_the_index",
			"xs = [\"b\", \"a\"]\nprint(1 if xs[0] > xs[1] else 0)\nprint(1 if \"b\" > xs[1] else 0)\n",
		},
		{
			"ordering_of_a_dict_entry_as_text",
			"d = {}\nd[\"k\"] = \"b\"\nprint(1 if d[\"k\"] > \"a\" else 0)\n",
		},
		{
			"ordering_of_a_container_the_program_grew_itself",
			"xs = []\nxs.append(\"b\")\nxs.append(\"a\")\nprint(1 if xs[0] > xs[1] else 0)\n",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			path := writeSrc(t, t.TempDir(), "slot_ordering.gy", tc.src)
			want, ok := cpythonOut(t, path)
			if !ok {
				t.Skip("no python3 oracle")
			}
			for _, engine := range []string{"--interp", "--aot"} {
				out, code := cliRunCode(t, engine, path)
				if code == 2 {
					t.Errorf("%s exited 2 (a module llc rejected — the compiler's own bug, ADR 0166):\n%s", engine, out)
				}
				if code != 0 {
					t.Errorf("%s exited %d on a program the oracle answers: %s", engine, code, out)
				}
				if out != want {
					t.Errorf("%s printed\n%s\nwant CPython's\n%s", engine, out, want)
				}
			}
		})
	}
}

// The pairs CPython refuses. A compiled program that prints a verdict for one of these is the worst
// answer this feature can give — it looks like a result — so both engines have to raise the same
// sentence, naming the left operand's type first.
func TestSlotOrderingTrapsAreRaisedNotRefused(t *testing.T) {
	for _, tc := range []struct {
		name string
		src  string
		msg  string
	}{
		{
			"a_number_slot_ordered_against_text",
			"xs = [1, \"a\"]\nprint(1 if xs[0] > \"a\" else 0)\n",
			"TypeError: '>' not supported between instances of 'int' and 'str'",
		},
		{
			"a_text_slot_ordered_against_a_number",
			"xs = [\"a\", 1]\nprint(1 if xs[0] < 1 else 0)\n",
			"TypeError: '<' not supported between instances of 'str' and 'int'",
		},
		{
			"the_number_on_the_left_in_the_source",
			"xs = [1, \"a\"]\nprint(1 if 2 > xs[1] else 0)\n",
			"TypeError: '>' not supported between instances of 'int' and 'str'",
		},
		{
			"the_text_on_the_left_in_the_source",
			"xs = [\"a\", 1]\nprint(1 if \"z\" < xs[1] else 0)\n",
			"TypeError: '<' not supported between instances of 'str' and 'int'",
		},
		{
			"a_float_slot_ordered_against_text_names_float",
			"xs = [1.5, \"a\"]\nprint(1 if xs[0] > \"a\" else 0)\n",
			"TypeError: '>' not supported between instances of 'float' and 'str'",
		},
		{
			"both_sides_are_slots_and_the_pair_is_a_number_and_a_text",
			"xs = [1, \"a\"]\nprint(1 if xs[0] > xs[1] else 0)\n",
			"TypeError: '>' not supported between instances of 'int' and 'str'",
		},
		{
			"a_container_slot_ordered_against_a_number",
			"xs = [[1, 2], \"a\"]\nprint(1 if xs[0] < 1 else 0)\n",
			"TypeError: '<' not supported between instances of 'list' and 'int'",
		},
		{
			"None_ordered_against_a_number",
			"xs = [None, \"a\"]\nprint(1 if xs[0] >= 1 else 0)\n",
			"TypeError: '>=' not supported between instances of 'NoneType' and 'int'",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			path := writeSrc(t, t.TempDir(), "slot_ordering_trap.gy", tc.src)
			want, code := oracleTrap(t, path)
			if !strings.Contains(want, tc.msg) {
				t.Fatalf("the oracle does not say %q, it says %q", tc.msg, want)
			}
			if code == 0 {
				t.Fatalf("the oracle exited 0 on a program meant to trap:\n%s", want)
			}
			for _, engine := range []string{"--interp", "--aot"} {
				combined := cliRun(t, engine, path)
				if code == 2 {
					t.Errorf("%s exited 2 (compiler bug, ADR 0166) on a trapping program:\n%s", engine, combined)
				}
				if !strings.Contains(combined, tc.msg) {
					t.Errorf("%s did not raise %q for a program the oracle traps on:\n%s", engine, tc.msg, combined)
				}
				if strings.Contains(combined, "codegen:") {
					t.Errorf("%s refused what the oracle traps on:\n%s", engine, combined)
				}
			}
		})
	}
}

// What the door cannot prove is refused in words, and the refusal is exit 1 — never a module `llc`
// rejects, and never a verdict. The rows name the answer CPython still prints, which is the debt the
// roadmap carries: the elementwise order of two containers (Gap R.86, Gap R.87) and the ordering across
// kinds the compiler can see but has no arm for (Gap R.85).
func TestSlotOrderingRefusalsLeaveTheCompilerOutOfIt(t *testing.T) {
	for _, tc := range []struct{ name, src string }{
		{
			"two_containers_ordered_against_each_other",
			"xs = [[1], [2]]\nprint(1 if xs[0] < xs[1] else 0)\n",
		},
		{
			"two_dicts_ordered_against_each_other",
			"xs = [{1: 2}, {1: 3}]\nprint(1 if xs[0] < xs[1] else 0)\n",
		},
		{
			"a_container_of_containers_ordered_against_a_literal_container",
			"xs = [[1, 2]]\nprint(1 if xs[0] < [1, 3] else 0)\n",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			path := writeSrc(t, t.TempDir(), "slot_ordering_refuse.gy", tc.src)
			for _, engine := range []string{"--interp", "--aot"} {
				out, code := cliRunCode(t, engine, path)
				if code == 2 {
					t.Errorf("%s exited 2 (the compiler's own bug, ADR 0166) instead of refusing:\n%s", engine, out)
				}
				if code == 0 {
					if _, isRefusal := refusalOf(out); isRefusal {
						t.Errorf("%s reported a refusal but exited 0:\n%s", engine, out)
					}
				}
			}
		})
	}
}

// refusalOf recognises the compiled backend's own words about a shape it cannot settle.
func refusalOf(out string) (string, bool) {
	if strings.Contains(out, "codegen:") || strings.Contains(out, "needs a single static kind") {
		return out, true
	}
	return "", false
}

// The bounds check survives the tagged read: a slot that is not there traps with IndexError before any
// arm runs, in both engines. The wording of that sentence is a separate measured debt (Gap R.90).
func TestSlotOrderingKeepsTheBoundsCheck(t *testing.T) {
	for _, src := range []string{
		"xs = [1, \"a\"]\nprint(1 if xs[7] > \"a\" else 0)\n",
		"xs = [1, \"a\"]\nprint(1 if xs[7] < 5 else 0)\n",
	} {
		path := writeSrc(t, t.TempDir(), "slot_ordering_bounds.gy", src)
		for _, engine := range []string{"--interp", "--aot"} {
			out, code := cliRunCode(t, engine, path)
			if code == 0 {
				t.Errorf("%s printed a verdict for an out-of-range slot read: %q", engine, out)
			}
			if code == 2 {
				t.Errorf("%s exited 2 (compiler bug, ADR 0166) on an out-of-range slot read:\n%s", engine, out)
			}
			if combined := cliRun(t, engine, path); !strings.Contains(combined, "IndexError") {
				t.Errorf("%s did not report an IndexError: %s", engine, combined)
			}
		}
	}
}
