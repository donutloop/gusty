package integration

import (
	"strings"
	"testing"
)

// This file is the reference leg of roadmap **Gap R.440** (**ADR 0424**): the kind an unrolled loop item
// carries, asked by the container literal standing beside it.
//
// The unit file (pkg/lang/loop_item_literal_test.go) pins what the roads emit — the tagged build, the
// mixed printer, the four gate arms and the builders' door reading ONE record. This one asks the only
// oracle left since ADR 0302, CPython, what each program prints, and requires the compiled toolchain to
// answer the same bytes. Where an answer is still owed it is pinned with today's bytes beside the
// reference's and the row that owns the fix is named, so a later fix fails the pin instead of quietly
// leaving it behind.
//
// The row's shape was one record read by one door and not by the four gates beside it: `print(k)` asked
// the kind the `for` road keeps per iteration and printed `None`, `1.5`, while `print([k])` on the next
// line asked the name and printed the void's payload and the float's box handle — `[0]`, `[1]`, at the
// exit code of success.

// TestALoopItemPutIntoALiteralAtTheCLI is the answering half: every expectation below is the bytes the
// live reference printed for the same file, asked in the same test, so the compiled leg cannot certify
// itself (ADR 0302, ADR 0424).
func TestALoopItemPutIntoALiteralAtTheCLI(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct{ name, src string }{
		{"the_rows_own_program", "for k in [None, 1.5]:\n    print([k])\n"},
		{"a_fractional_item", "for k in [1.5, 2.5]:\n    print([k])\n"},
		{"the_void_item_alone", "for k in [None, None]:\n    print([k])\n"},
		{"a_verdict_item", "for k in [True, False]:\n    print([k])\n"},
		{"a_container_item_that_is_a_list", "for k in [[1], [2, 3]]:\n    print([k])\n"},
		{"a_container_item_that_is_a_dict", "for k in [{\"a\": 1}]:\n    print([k])\n"},
		{"a_container_item_that_is_a_tuple", "for k in [(1, 2)]:\n    print([k])\n"},
		{"a_container_item_that_is_a_set", "for k in [{1}]:\n    print([k])\n"},
		{"every_kind_in_one_iterable", "for k in [None, 1.5, True, \"a\", [1], 7]:\n    print([k])\n"},
		{"the_item_twice_in_one_literal", "for k in [None, 1.5]:\n    print([k, k])\n"},
		{"the_item_beside_numbers", "for k in [None, 1.5]:\n    print([k, 1, 2])\n"},
		{"the_item_read_back_out_of_the_slot", "for k in [None, 1.5]:\n    print([k][0])\n"},
		{"the_item_concatenated", "for k in [None, 1.5]:\n    print([k] + [1])\n"},
		{"the_item_compiled_as_a_list", "for k in [None, 1.5]:\n    print([k] == [None])\n"},
		{"the_item_measured", "for k in [None, 1.5]:\n    print(len([k]))\n"},
		{"the_item_behind_str", "for k in [None, 1.5]:\n    print(str([k]))\n"},
		{"the_item_printed_and_put_in_a_literal_at_once", "for k in [None, 1.5]:\n    print(k, [k])\n"},
		{"the_item_in_a_set_literal", "for v in [None, 2.5]:\n    print({v})\n"},
		{"the_item_as_a_dict_value", "for v in [None, 2.5]:\n    print({\"k\": v})\n"},
		{"the_item_as_a_dict_key", "for v in [None, 1.5]:\n    print({v: \"x\"})\n"},
		{"the_item_in_a_literal_inside_a_dict", "for k in [None, 1.5]:\n    print({\"v\": [k]})\n"},
		{"a_container_item_nested_one_level_down", "for k in [[1], [2]]:\n    print([[k]])\n"},
		{"the_item_bound_to_a_name_first", "for k in [None, 1.5]:\n    ys = [k]\n    print(ys)\n"},
		{"the_item_in_a_nested_loop", "for i in [0, 1]:\n    for k in [None, 1.5]:\n        print([k, i])\n"},
		{"a_second_loop_reuses_the_name", "for k in [None, 1.5]:\n    print([k])\nfor k in [2, 3]:\n    print([k])\n"},
		{"the_item_of_a_loop_over_a_name_the_program_built", "xs = [None, 1.5]\nfor k in xs:\n    print([k])\n"},
		{"a_text_item", "for k in [\"a\", \"b\"]:\n    print([k, k])\n"},
		// The controls: an item that is a number keeps every road it always had, including the record
		// this row did not acquire for it (**ADR 0239**'s fold) and the name's older bindings.
		{"a_numeric_item_keeps_the_folds_static_array", "for k in [1, 2]:\n    print(sum([k]))\n"},
		{"min_and_max_over_a_numeric_item", "for k in [3, 4]:\n    print(min([k]), max([k]))\n"},
		{"a_numeric_item_used_as_a_number", "for k in [1, 2]:\n    print([k, k * 2])\n"},
		{"a_numeric_item_after_a_void_binding", "k = None\nfor k in [1, 2]:\n    print([k])\n"},
		{"a_numeric_item_after_a_verdict_binding", "k = True\nfor k in [1, 2]:\n    print([k])\n"},
		{"a_numeric_item_after_a_container_binding", "xs = [7]\nfor xs in [1, 2]:\n    print([xs])\n"},
		{"a_float_loop_followed_by_a_numeric_one", "for k in [1.5]:\n    print([k])\nfor k in [1, 2]:\n    print([k])\n"},
	} {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			path := writeSrc(t, t.TempDir(), "loop_item.gy", tc.src)
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
					t.Errorf("%s printed %q, the reference printed %q", engine, combined, refOut)
				}
			}
		})
	}
}

// TestTheLoopItemLiteralRoadsAnswerWrongArePinnedBesideTheReference holds what this row leaves owed.
// Every `todayBytes` and `todayExit` is what THIS backend does, measured 2026-10-27 against a build of
// the row's parent as well as this one, and every `reference` is what CPython prints for the same file,
// asked inside the case. An answer that arrives here fails the case, and the failure text says to delete
// the row and close the named one.
func TestTheLoopItemLiteralRoadsAnswerWrongArePinnedBesideTheReference(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name       string
		src        string
		todayExit  int
		todayBytes string
		reference  string
		owedBy     string
	}{
		{"the_item_compiled_to_the_void", "for k in [None, 1.5]:\n    print(k == None)\n", 0, "False\nFalse\n",
			"True\nFalse\n",
			"Gap R.416 (the compare door asks the name's records; the print road and the literal ask the item's)"},
		{"the_item_tested_for_membership_in_its_own_iterable", "for k in [None, 1.5]:\n    print(k in [None, 1.5])\n", 0,
			"True\nFalse\n", "True\nTrue\n",
			"Gap R.416 (the compare door's third spelling: membership walks the same road the equality question does)"},
		{"the_item_interpolated_rather_than_printed", "for k in [None, 1.5]:\n    print(f\"{k}\")\n", 0, "0\n0\n",
			"None\n1.5\n",
			"Gap R.445 (the interpolation field asks the name's records; the print door reads the item's)"},
		{"the_fold_over_a_fractional_item", "for k in [1.5, 2.5]:\n    print(sum([k]))\n", 0, "0\n1\n",
			"1.5\n2.5\n",
			"Gap R.446 (ADR 0239's fold reads a static array and has no arm for an item whose payload is a box handle)"},
		{"the_item_whose_name_once_held_a_fraction", "k = 1.5\nfor k in [1, 2]:\n    print([k])\n", 0, "[1]\n[0]\n",
			"[1]\n[2]\n",
			"Gap R.447 (the `for` road stores an i32 into the double slot the float road declared for the name, and opaque pointers let the module verify)"},
		{"the_item_whose_name_once_held_a_fraction_printed_as_itself", "k = 1.5\nfor k in [1, 2]:\n    print(k)\n", 0,
			"1.5000000000000002\n1.5000000000000004\n", "1\n2\n",
			"Gap R.447 (byte-identical on this row's parent: the slot, not the gate, is what is wrong here)"},
	} {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			path := writeSrc(t, t.TempDir(), "loop_item_wrong.gy", tc.src)
			refOut, refCode := oracleTrap(t, path)
			if refCode == 0 && tc.reference == "" {
				t.Fatalf("the reference exited 0 on a program meant to raise: %q — re-measure the row", refOut)
			}
			if tc.reference != "" && refOut != tc.reference {
				t.Fatalf("this pin's reference column is stale: CPython printed %q, the pin says %q", refOut, tc.reference)
			}
			for _, engine := range cliEngines {
				combined := cliRun(t, engine, path)
				if _, code := cliRunCode(t, engine, path); code != tc.todayExit {
					t.Errorf("%s exited %d, this pin expects %d — an answer that arrived here means the row was closed and this table did not follow\n%s",
						engine, code, tc.todayExit, tc.src)
				}
				if combined != tc.todayBytes {
					t.Errorf("%s printed %q, this pin holds %q (%s)\n%s", engine, combined, tc.todayBytes, tc.owedBy, tc.src)
				}
			}
		})
	}
}

// TestTheLoopItemLiteralRefusalsLeaveTheCompilerOutOfIt is the ADR 0166 gate over this row's shapes: a
// refusal is allowed, an exit 2 spent on the compiler's own module is not, and a refusal has to name the
// missing half rather than shrug.
func TestTheLoopItemLiteralRefusalsLeaveTheCompilerOutOfIt(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct{ name, src, want string }{
		// The truth of the container the item was put in is another row's door (**Gap R.131**), and the
		// refusal says so; what this case checks is that it stays a refusal in words rather than an exit 2.
		{
			"the_truth_of_the_container_the_item_was_put_in",
			"for k in [None, 1.5]:\n    print(bool([k]))\n",
			"a container's truthiness is its length",
		},
	} {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			path := writeSrc(t, t.TempDir(), "loop_item_refuse.gy", tc.src)
			if refOut, refCode := oracleTrap(t, path); refCode != 0 {
				t.Fatalf("this row is about a program CPython answers:\n%s", refOut)
			}
			for _, engine := range cliEngines {
				combined := cliRun(t, engine, path)
				if _, code := cliRunCode(t, engine, path); code == 2 {
					t.Fatalf("%s rejected the compiler's own module (exit 2, ADR 0166): %s", engine, combined)
				} else if code != 0 {
					if code != 1 || !strings.Contains(combined, tc.want) {
						t.Fatalf("%s exited %d without naming the missing half (%q): %s", engine, code, tc.want, combined)
					}
					noteCompiledGap(t, tc.src, combined)
				}
			}
		})
	}
}
