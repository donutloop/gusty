package integration

import (
	"fmt"
	"testing"
)

// The reference leg of the family **ADR 0438** pays (roadmap **Gap R.486**, **Gap R.489**, and the
// **Gap R.439** half of the same shape). Each case writes the source, asks a live CPython what it prints,
// and requires this backend's bytes to be those bytes at exit **0** — the wrongness of this family was
// always exit **0** with a heap slot in it (`[0, 2]`, `[, 1.5]`), so the exit code is asserted and not
// inferred, and a refusal fails the case as loudly as a wrong number does.
//
// The unit twin is `pkg/lang/named_container_element_test.go`, which also asks the record leg and the
// emitted IR; neither suite is allowed to answer for the other (AGENTS.md § the two witness legs).
func TestAContainerTheProgramNamedAnswersAsAnElementAtTheCLI(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct{ name, src string }{
		// The rows this cycle was filed by, and the two **Gap R.439** left standing.
		{"a_dict_the_program_named_beside_a_number", "sa = {1: \"a\", \"b\": 2}\nprint([sa, 1])\n"},
		{"a_dict_the_program_named_alone_in_the_literal", "sa = {1: \"a\", \"b\": 2}\nprint([sa])\n"},
		{"a_list_the_program_named_beside_a_number", "xs = [1]\nprint([xs, 2])\n"},
		{"two_lists_the_program_named_in_one_literal", "xs = [1]\nys = [2]\nprint([xs, ys])\n"},
		{"a_longer_list_the_program_named", "xs = [1, 2]\nprint([xs, 1])\n"},
		{"two_dicts_the_program_named", "sa = {1: \"a\"}\nsb = {2: \"b\"}\nprint([sa, sb])\n"},
		{"a_list_a_dict_and_a_text_in_one_literal", "xs = [1, 2]\nsd = {\"k\": 1}\nprint([xs, sd, \"z\"])\n"},
		{"a_set_the_program_named", "st = {1}\nprint([st, 1])\n"},
		{"a_named_dict_one_level_down", "sa = {1: \"a\"}\nprint([[sa], 1])\n"},
		// The roads that READ the slot: the object has to describe itself, not the print call.
		{"a_named_container_iterated_from_a_literal", "xs = [1, 2]\nfor v in [xs, [3]]:\n    print(v)\n"},
		{"the_named_element_read_back_out_of_the_literal", "xs = [1, 2]\nprint([xs, 1][0])\n"},
		{"the_named_container_as_a_dict_value", "xs = [1, 2]\nd = {\"a\": xs}\nprint(d)\n"},
		{"the_named_container_through_str", "xs = [1, 2]\nprint(str([xs, 1]))\n"},
		{"the_named_container_tested_for_equality", "xs = [1, 2]\nprint([xs] == [[1, 2]])\n"},
		// The **Gap R.489** family: the name was a container first, and the iteration rebound it.
		{"an_int_item_beside_a_float_after_a_list_binding", "xs = [7]\nfor xs in [1, 2]:\n    print([xs, 1.5])\n"},
		{"an_int_item_beside_a_text_after_a_list_binding", "xs = [7]\nfor xs in [1, 2]:\n    print([xs, \"a\"])\n"},
		{"an_int_item_compared_as_a_literal", "xs = [7]\nfor xs in [1, 2]:\n    print([xs] == [1])\n"},
		{"an_int_item_after_a_dict_binding", "sd = {1: 2}\nfor sd in [1, 2]:\n    print([sd, \"a\"])\n"},
		{"a_float_item_after_a_set_binding", "st = {1}\nfor st in [1.5, 2]:\n    print([st, 1])\n"},
		{"a_container_item_after_a_container_binding", "xs = [7]\nfor xs in [[1], [2]]:\n    print([xs, 1])\n"},
		{"an_int_item_alone_after_a_list_binding", "xs = [7]\nfor xs in [1, 2]:\n    print([xs])\n"},
		// The controls: the shapes that answered before the census learned to ask, which must not move.
		{"a_spelled_container_literal_element", "print([[1, 2], 1])\n"},
		{"a_numeric_literal", "print([1, 2, 3])\n"},
		{"a_text_literal", "print([\"a\", 1])\n"},
		{"a_nested_container_literal", "print([[[1, 2], [3]], 1])\n"},
		{"a_dict_literal_in_a_list", "print([{\"a\": 1}, 2])\n"},
		{"a_named_empty_list", "ys = []\nprint([ys, 1])\n"},
	} {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			path := writeSrc(t, t.TempDir(), "named_container_element.gy", tc.src)
			refOut, refCode := oracleTrap(t, path)
			if refCode != 0 {
				t.Fatalf("the reference exited %d on a program meant to answer:\n%s", refCode, refOut)
			}
			for _, engine := range cliEngines {
				combined := cliRun(t, engine, path)
				if _, code := cliRunCode(t, engine, path); code != 0 {
					t.Errorf("%s exited %d on a program the reference answers: %s", engine, code, combined)
				}
				if combined != refOut {
					t.Errorf("%s printed %q, the reference printed %q (%s)", engine, combined, refOut, tc.src)
				}
			}
		})
	}
}

// TestTheNamedContainerElementNeverExitsTwo is the ADR 0166 half of the same sweep. Two words printed as two
// numbers, and one module that `llc` rejects, came out of one door; the module half — a compile-time list
// global in an operand position — is **Gap R.488**, filed and NOT fixed, and the shapes that reach it are a
// comprehension written as an element (`integration/container_element_and_member_test.go` pins them at exit
// 2). What this case holds is that no program in the family this cycle ANSWERS reaches exit 2: the day the
// census fix starts rejecting modules, it fails here rather than in a roadmap row.
func TestTheNamedContainerElementNeverExitsTwo(t *testing.T) {
	t.Parallel()
	for i, src := range []string{
		"xs = [1]\nprint([xs, 2])\n",
		"sa = {1: \"a\", \"b\": 2}\nprint([sa, 1])\n",
		"xs = [1, 2]\nsd = {\"k\": 1}\nprint([xs, sd, \"z\"])\n",
		"xs = [7]\nfor xs in [1, 2]:\n    print([xs, 1.5])\n",
		"xs = [7]\nfor xs in [[1], [2]]:\n    print([xs, 1])\n",
		"st = {1}\nprint([st, 1])\n",
	} {
		src := src
		t.Run(fmt.Sprintf("%d", i), func(t *testing.T) {
			t.Parallel()
			path := writeSrc(t, t.TempDir(), "named_container_no_exit2.gy", src)
			for _, engine := range cliEngines {
				if _, code := cliRunCode(t, engine, path); code == 2 {
					t.Errorf("%s exited 2 (a compiler bug, %s) on %q — the census fix must refuse in words or answer, never emit a module %s rejects",
						engine, "ADR 0166", src, "llc")
				}
			}
		})
	}
}
