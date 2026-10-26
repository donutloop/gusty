package integration

import (
	"strings"
	"testing"
)

// This file is the reference leg of roadmap **Gap R.441** (**ADR 0422**): the element a container slot is
// WRITTEN with, and the container's obligation to stop claiming a kind it no longer holds.
//
// The unit file (pkg/lang/item_store_kind_test.go) pins what the roads emit — the box, the tag, the
// container's bit 8, the mixed printer, the arm and the door asking one predicate. This one asks the only
// oracle left since ADR 0302, CPython, what each program prints, and requires the compiled toolchain to
// answer the same bytes. Where an answer is still owed it is pinned with today's bytes beside the
// reference's and the row that owns the fix is named, so a later fix fails the pin instead of quietly
// leaving it behind.
//
// The row's shape is three roads that write one kind of slot, two of which had long since learned to ask
// the VALUE its kind (`append` since **ADR 0233**, the dict's entry store since **ADR 0232**) and one of
// which — `xs[i] = v` — asked only whether the new word was text or a number. The float reached its slot
// whole, box handle and tag both; the container was never told, so the printer read the slot as the kind
// the container still claimed and printed the handle: `[1]` where the reference prints `[2.5]`, at the exit
// code of success, beside `xs.append(2.5)`, which printed `2.5` all along.

// itemStoreCLICases is the answering half: every one of these is a program the reference prints, and the
// compiled leg has to print the same bytes.
func itemStoreCLICases() []struct{ name, src string } {
	return []struct{ name, src string }{
		{"a_fraction_stored_in_an_int_slot", "xs = [1]\nxs[0] = 2.5\nprint(xs)\n"},
		{"a_fraction_stored_beside_an_untouched_number", "xs = [1, 2]\nxs[0] = 2.5\nprint(xs)\n"},
		{"the_void_stored_in_an_int_slot", "xs = [1]\nxs[0] = None\nprint(xs)\n"},
		{"a_verdict_stored_in_an_int_slot", "xs = [1]\nxs[0] = True\nprint(xs)\n"},
		{"the_false_verdict_stored_in_the_second_slot", "xs = [1, 2]\nxs[1] = False\nprint(xs)\n"},
		{"a_list_stored_in_an_int_slot", "xs = [1]\nxs[0] = [2]\nprint(xs)\n"},
		{"a_dict_stored_in_an_int_slot", "xs = [1]\nxs[0] = {\"a\": 1}\nprint(xs)\n"},
		{"a_set_stored_in_an_int_slot", "xs = [1]\nxs[0] = {1, 2}\nprint(xs)\n"},
		{"the_tuple_spelling_stored_in_an_int_slot", "xs = [1]\nxs[0] = (1, 2)\nprint(xs)\n"},
		{"the_void_stored_through_a_negative_index", "xs = [1, 2]\nxs[-1] = None\nprint(xs)\n"},
		{"a_fraction_bound_to_a_name_stored", "v = 2.5\nxs = [1]\nxs[0] = v\nprint(xs)\n"},
		{"a_fraction_a_body_hands_back_stored", "def f():\n    return 2.5\nxs = [1]\nxs[0] = f()\nprint(xs)\n"},
		{"a_container_under_a_name_stored", "ys = [2]\nxs = [1]\nxs[0] = ys\nprint(xs)\n"},
		{"a_dict_under_a_name_stored", "d = {\"a\": 1}\nxs = [1]\nxs[0] = d\nprint(xs)\n"},
		{"the_stored_fraction_read_out_of_the_slot", "xs = [1]\nxs[0] = 2.5\nprint(xs[0])\n"},
		{"the_stored_verdict_read_out_of_the_slot", "xs = [1]\nxs[0] = True\nprint(xs[0])\n"},
		{"the_stored_void_asked_its_identity", "xs = [1]\nxs[0] = None\nprint(xs[0] is None)\n"},
		{"the_stored_void_compared", "xs = [1]\nxs[0] = None\nprint(xs[0] == None)\n"},
		{"the_stored_fraction_compared", "xs = [1, 2]\nxs[0] = 2.5\nprint(xs[0] > 3)\n"},
		{"the_stored_fraction_multiplied", "xs = [1, 2]\nxs[0] = 2.5\nprint(xs[0] * 2)\n"},
		{"the_promoted_list_looped_over", "xs = [1, 2]\nxs[0] = 2.5\nfor k in xs:\n    print(k)\n"},
		{"the_promoted_list_sliced", "xs = [1, 2, 3]\nxs[0] = 2.5\nprint(xs[0:2])\n"},
		{"the_promoted_list_popped", "xs = [1, 2]\nxs[0] = 2.5\nprint(xs.pop(0))\nprint(xs)\n"},
		{"the_promoted_list_reversed", "xs = [1, 2]\nxs[0] = 2.5\nxs.reverse()\nprint(xs)\n"},
		{"the_promoted_list_through_str", "xs = [1]\nxs[0] = 2.5\nprint(str(xs))\n"},
		{"the_promoted_list_through_repr", "xs = [1]\nxs[0] = 2.5\nprint(repr(xs))\n"},
		{"the_promoted_list_compared_as_a_list", "xs = [1]\nxs[0] = 2.5\nprint(xs == [2.5])\n"},
		{"the_promoted_list_concatenated", "xs = [1]\nxs[0] = 2.5\nprint(xs + [3])\n"},
		{"the_promoted_list_searched", "xs = [1, 2]\nxs[0] = 2.5\nprint(2.5 in xs)\n"},
		{"the_promoted_list_measured", "xs = [1, 2]\nxs[0] = 2.5\nprint(len(xs))\n"},
		{"the_stored_tuple_compared", "xs = [1]\nxs[0] = (1, 2)\nprint(xs[0] == (1, 2))\n"},
		{"two_stores_of_two_different_kinds", "xs = [1, 2, 3]\nxs[0] = None\nxs[2] = 2.5\nprint(xs)\n"},
		{"a_store_over_a_list_a_comprehension_built", "xs = [k for k in [1, 2]]\nxs[0] = 2.5\nprint(xs)\n"},
		{"a_store_whose_index_is_a_loop_item", "xs = [1, 2]\nfor i in [0]:\n    xs[i] = None\nprint(xs)\n"},
		{"a_store_whose_index_is_a_while_counter", "xs = [1, 2]\ni = 0\nwhile i < 2:\n    xs[i] = None\n    i += 1\nprint(xs)\n"},
		{"the_store_through_an_alias_read_from_the_original", "xs = [1, 2]\nys = xs\nys[0] = 2.5\nprint(xs)\n"},
		{"the_void_through_an_alias_read_from_the_original", "xs = [1]\nys = xs\nys[0] = None\nprint(xs)\n"},
	}
}

// TestAStoredSlotSaysWhatItsValueIsAtTheCLI is the answering half against a live reference: every
// expectation below is the bytes the reference printed for the same file, asked in the same test, so the
// compiled leg cannot certify itself (ADR 0302, ADR 0422).
func TestAStoredSlotSaysWhatItsValueIsAtTheCLI(t *testing.T) {
	t.Parallel()
	for _, tc := range itemStoreCLICases() {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			path := writeSrc(t, t.TempDir(), "item_store.gy", tc.src)
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

// TestTheStoreThatNeededNoPromotionIsUntouchedAtTheCLI is the control half: the roads the promotion must
// not disturb, each also asked of the live reference.
func TestTheStoreThatNeededNoPromotionIsUntouchedAtTheCLI(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct{ name, src string }{
		{"a_number_stored_in_a_number_slot", "xs = [1]\nxs[0] = 3\nprint(xs)\n"},
		{"a_text_stored_in_an_int_slot", "xs = [1]\nxs[0] = \"s\"\nprint(xs)\n"},
		{"a_fraction_stored_in_a_text_slot", "xs = [\"a\"]\nxs[0] = 2.5\nprint(xs)\n"},
		{"the_append_road", "xs = [1]\nxs.append(2.5)\nprint(xs)\n"},
		{"the_dict_entry_store", "d = {\"a\": 1}\nd[\"a\"] = 2.5\nprint(d)\n"},
		{"a_store_into_a_float_kind_list", "xs = [1.5]\nxs[0] = None\nprint(xs)\n"},
		{"a_number_stored_over_the_promoted_fraction", "xs = [1]\nxs[0] = 2.5\nxs[0] = 3\nprint(xs)\n"},
		{"the_fold_over_a_slot_the_store_promoted", "xs = [1]\nxs[0] = 2.5\nprint(sum([xs[0], 1.0]))\n"},
		{"the_arithmetic_over_the_promoted_slot", "xs = [1]\nxs[0] = 2.5\nprint(xs[0] + 1)\n"},
		{
			// The two positions Gap R.442 was filed for, moved out of this file's pin tables by ADR 0423.
			"the_chained_comparison_over_the_promoted_slot",
			"xs = [1, 2]\nxs[0] = 2.5\nprint(0 < xs[0] < 3)\n",
		},
		{
			"the_chained_comparison_the_stale_payload_answered_wrongly",
			"xs = [1, 2]\nxs[0] = 5.5\nprint(0 < xs[0] < 3)\n",
		},
		{
			"the_numeric_use_of_a_slot_a_runtime_store_promoted",
			"xs = [1]\nxs[0] = 2.5\nprint(xs[0] + 1)\nfor i in [0]:\n    xs[i] = None\nprint(xs)\n",
		},
		{"the_truth_of_a_stored_void", "xs = [1]\nxs[0] = None\nprint(bool(xs[0]))\n"},
		{"the_truth_of_a_stored_fraction", "xs = [1]\nxs[0] = 2.5\nprint(bool(xs[0]))\n"},
		{"a_verdict_read_as_the_number_it_is", "xs = [1]\nxs[0] = True\nprint(xs[0] + 1)\n"},
		{"a_text_stored_then_a_void", "xs = [1, 2]\nxs[0] = \"a\"\nxs[1] = None\nprint(xs)\n"},
	} {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			path := writeSrc(t, t.TempDir(), "item_store_control.gy", tc.src)
			refOut, refCode := oracleTrap(t, path)
			for _, engine := range cliEngines {
				stdout, stderr, code := cliStreams(t, engine, path)
				if code != 0 && refCode == 0 {
					t.Errorf("%s exited %d on a program the reference answers: %s", engine, code, stdout+stderr)
				}
				if stdout+stderr != refOut {
					t.Errorf("%s printed %q / raised %q, the reference answered %q (exit %d)", engine, stdout, stderr, refOut, refCode)
				}
			}
		})
	}
}

// TestTheOutOfRangeStoreRaisesWhatTheReferenceRaises keeps the bounds door this row shares with the read
// path (**ADR 0210**): promoting the container is no licence to write past the end. A traceback cannot be
// compared byte for byte with the reference's — the file name is the temp path and the reference quotes the
// source line — so the parity claim is the one the language makes: the exception class and its sentence.
func TestTheOutOfRangeStoreRaisesWhatTheReferenceRaises(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct{ name, src string }{
		{"a_fraction_past_the_end", "xs = [1]\nxs[5] = 2.5\nprint(xs)\n"},
		{"the_void_past_the_end", "xs = [1]\nxs[5] = None\nprint(xs)\n"},
		{"a_container_before_the_start", "xs = [1]\nxs[-5] = [2]\nprint(xs)\n"},
		{"a_number_past_the_end", "xs = [1]\nxs[5] = 2\nprint(xs)\n"},
	} {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			path := writeSrc(t, t.TempDir(), "item_store_bounds.gy", tc.src)
			refOut, refCode := oracleTrap(t, path)
			refLast := strings.TrimRight(refOut, "\n")
			if i := strings.LastIndex(refLast, "\n"); i >= 0 {
				refLast = refLast[i+1:]
			}
			if refCode == 0 || !strings.Contains(refLast, "IndexError: list assignment index out of range") {
				t.Fatalf("the reference answered %q (exit %d) where an IndexError was expected — re-measure", refOut, refCode)
			}
			for _, engine := range cliEngines {
				stdout, stderr, code := cliStreams(t, engine, path)
				if code == 0 {
					t.Errorf("%s exited 0 on an out-of-range write the reference raises: %q", engine, stdout)
					continue
				}
				if !strings.Contains(stdout+stderr, "IndexError: list assignment index out of range") {
					t.Errorf("%s raised %q / %q, not the reference's %q", engine, stdout, stderr, refLast)
				}
			}
		})
	}
}

// TestTheItemStoreRoadsAnswerWrongArePinnedBesideTheReference holds what this row leaves owed as a WRONG
// answer, with today's bytes beside the reference's. The chained comparison and the sort are the two the
// promotion EXPOSES: their `todayBytes` is a refusal, and the dossier says what the answer used to be — a
// stale payload that agreed with the reference only by luck.
func TestTheItemStoreRoadsAnswerWrongArePinnedBesideTheReference(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name       string
		src        string
		todayExit  int
		todayBytes string
		reference  string
		owedBy     string
	}{
		{
			// The reference's `range` is an object; this backend's is a loop the compiler sees through,
			// so the slot holds the word the loop was built from. Unchanged on this row's parent — there
			// was never a tag here to ask.
			name:      "a_range_stored_in_a_slot",
			src:       "xs = [1]\nxs[0] = range(2)\nprint(xs)\n",
			todayExit: 0, todayBytes: "[2]\n", reference: "[range(0, 2)]\n",
			owedBy: "Gap R.293 (a range is not a value this backend carries)",
		},
		{
			// The sort over a promoted list refuses where it used to PRODUCE an order — `[2.5, 1]` against
			// the reference's `[1, 2.5]`, the payload order. Refusing is the honest half of the same hole.
			// (Its neighbour, the chained comparison over the same slot, left this table for the answering
			// table when **ADR 0423** gave the chain its operand's pair.)
			name:      "a_sort_over_a_promoted_list",
			src:       "xs = [3, 1]\nxs[0] = 2.5\nprint(sorted(xs))\n",
			todayExit: 1, todayBytes: "cannot sort a list whose elements are of more than one kind", reference: "[1, 2.5]\n",
			owedBy: "Gap R.438 / ADR 0191 (the comparator asks the payloads and never asks the kinds)",
		},
	} {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			path := writeSrc(t, t.TempDir(), "item_store_wrong.gy", tc.src)
			refOut, refCode := oracleTrap(t, path)
			if tc.reference == "" {
				last := strings.TrimRight(refOut, "\n")
				if refCode == 0 {
					t.Fatalf("the reference exited 0 on a program meant to raise: %q — re-measure the row", last)
				}
				if !strings.Contains(last, "TypeError") && !strings.Contains(last, "Error") {
					t.Fatalf("the reference's raise is %q, not the error this pin names — re-measure the row", last)
				}
			} else if refCode != 0 || refOut != tc.reference {
				t.Fatalf("the reference moved: %q exit %d, this pin expects %q — re-measure the row", refOut, refCode, tc.reference)
			}
			for _, engine := range cliEngines {
				stdout, stderr, code := cliStreams(t, engine, path)
				if code != tc.todayExit {
					t.Fatalf("%s exited %d on a shape pinned as exit %d (%s): stdout %q stderr %q — this pin does "+
						"not describe what happened here, and the named row may have been closed",
						engine, code, tc.todayExit, tc.owedBy, stdout, stderr)
				}
				if !strings.Contains(stdout+stderr, tc.todayBytes) {
					if tc.reference == "" {
						t.Errorf("%s printed %q / raised %q; this pin holds today's bytes %q while the reference raises. "+
							"If the answer is now the reference's, DELETE this row and close the named one (%s).",
							engine, stdout, stderr, tc.todayBytes, tc.owedBy)
						return
					}
					t.Errorf("%s printed %q / raised %q; this pin holds today's bytes %q, and the reference prints %q (%s). "+
						"If the answer is now the reference's, DELETE this row and close the named one.",
						engine, stdout, stderr, tc.todayBytes, tc.reference, tc.owedBy)
				}
			}
		})
	}
}

// TestTheItemStoreRoadsStillRefuseWhatTheNamedRowsOwe is the refusals table: a program the reference
// answers that this backend declines, named by the row that owes the answer. A refusal is not a wrong
// answer, and these are not this row's debt — but they are the neighbours a reader of this row will try
// next, so they are said out loud with their owner attached.
func TestTheItemStoreRoadsStillRefuseWhatTheNamedRowsOwe(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct{ name, src, owedBy string }{
		// The two Gap R.442 refusals this table carried — the chained comparison over a promoted slot and
		// the numeric use of a slot a run-time store promoted — left for the answering table when ADR 0423
		// gave the chain and the numeric door the pair they were refusing to carry.
		{"a_reduction_over_a_container_the_compiler_does_not_hold", "xs = [1]\nxs[0] = 2.5\nprint(sum(xs))\n",
			"Gap R.71 / Gap R.107 (lower.unsupported.builtin-needs-inline-container: the fold reads a literal it can see)"},
		{"a_search_over_a_container_the_program_mutated", "xs = [1, 2]\nxs[0] = 2.5\nprint(xs.count(2.5))\n",
			"Gap R.174 / Gap I.2 (the container methods fold a literal; this list's slots are the runtime's)"},
		{"an_insert_into_a_container_the_program_mutated", "xs = [1, 2]\nxs[0] = 2.5\nxs.insert(0, 9)\nprint(xs)\n",
			"Gap R.174 (its sibling append answers; this one folds a literal)"},
		{"a_join_over_a_container_the_program_mutated", "xs = [1, 2]\nxs[0] = \"a\"\nxs[1] = \"b\"\nprint(\", \".join(xs))\n",
			"Gap I.2 / Gap R.174 (join wants a constant list and asks nothing of the slots)"},
		{"the_store_through_a_subscript_rather_than_a_name", "xs = [[1]]\nxs[0][0] = 2.5\nprint(xs)\n",
			"Gap R.381 (the read side of the position closed as Gap R.214 / ADR 0334; the write side waits)"},
		{"the_store_from_inside_a_body_over_a_module_container", "xs = [1]\ndef f():\n    xs[0] = 2.5\nf()\nprint(xs)\n",
			"Gap R.35 (a body cannot read module state that changes)"},
		{"a_value_the_tag_cannot_name_stored", "xs = [1]\nxs[0] = lambda x: x\nprint(len(xs))\n",
			"Gap R.150 / Gap R.151 / ADR 0283 (a lambda is declared, not assigned, so no slot can hold it)"},
	} {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			path := writeSrc(t, t.TempDir(), "item_store_owed.gy", tc.src)
			for _, engine := range cliEngines {
				out, code := cliRunCode(t, engine, path)
				if code == 0 {
					t.Errorf("%s answers a source the roadmap says %s owes: %q — retire this pin", engine, tc.owedBy, out)
				}
			}
		})
	}
}
