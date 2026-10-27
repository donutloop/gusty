package integration

import (
	"strings"
	"testing"
)

// This file is the reference leg of roadmap **Gap R.435** (**ADR 0421**): the void a container SLOT holds,
// reached through a binding or a call rather than through its own spelling.
//
// The unit file (pkg/lang/void_slot_literal_test.go) pins what the roads emit — the tagged build, the
// mixed printer, the gate and the door agreeing. This one asks the only oracle left since ADR 0302,
// CPython, what each program prints, and requires the compiled toolchain to answer the same bytes. Where
// an answer is still owed it is pinned with today's bytes beside the reference's and the row that owns the
// fix is named, so a later fix fails the pin instead of quietly leaving it behind.
//
// The row's shape is one gate that asked the SPELLING of an element and a print road that asked the
// BINDING: `print([None])` was built self-describing and printed `[None]`, and `k = None` / `print([k])`
// fell to the static int array and printed the void's payload — `[0]`, at the exit code of success.

// TestAVoidSlotSaysWhatItsBindingWroteAtTheCLI is the answering half: every expectation below is the bytes
// the live reference printed for the same file, asked in the same test, so the compiled leg cannot
// certify itself (ADR 0302, ADR 0421).
func TestAVoidSlotSaysWhatItsBindingWroteAtTheCLI(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct{ name, src string }{
		{"the_void_in_a_name_put_into_a_list_literal", "k = None\nprint([k])\n"},
		{"the_void_beside_a_number_in_one_literal", "k = None\nprint([k, 1])\n"},
		{"the_void_leading_the_number_following", "k = None\nprint([1, k])\n"},
		{"the_same_list_through_str", "k = None\nprint(str([k]))\n"},
		{"the_void_in_a_name_put_into_a_set_literal", "k = None\nprint({k})\n"},
		{"the_void_in_a_name_put_into_a_tuple", "k = None\nprint((k,))\n"},
		{"the_void_as_a_dict_value", "k = None\nprint({1: k})\n"},
		{"the_void_as_a_dict_key", "k = None\nprint({k: 1})\n"},
		{"the_void_one_level_down", "k = None\nprint([[k]])\n"},
		{"the_void_inside_a_dicts_list_value", "k = None\nprint({\"a\": [k]})\n"},
		{"the_void_in_a_name_measured", "k = None\nprint(len([k]))\n"},
		{"the_void_in_a_name_tested_for_membership", "k = None\nprint(None in [k])\n"},
		{"the_void_in_a_name_looped_over", "k = None\nfor v in [k]:\n    print(v)\n"},
		{"the_void_in_a_name_read_back_out_of_the_slot", "k = None\nprint([k][0])\n"},
		{"the_void_in_a_name_concatenated_with_another_list", "k = None\nprint([k] + [1])\n"},
		{"the_void_in_a_name_compiled_as_a_list", "k = None\nprint([k] == [None])\n"},
		{"the_void_in_a_name_put_behind_a_mutation", "k = None\nxs = [k]\nxs.append(1)\nprint(xs)\n"},
		{"the_void_and_a_text_in_one_literal", "k = None\nprint([k, \"a\"])\n"},
		{"the_void_and_a_number_in_one_set", "k = None\nprint({k, 1})\n"},
		{"a_sort_over_the_void", "k = None\nprint(sorted([k]))\n"},
		{"the_void_in_a_name_on_the_ternarys_arm", "k = None\nprint([k] if k == None else [])\n"},
		{"a_void_body_in_a_list", "def f():\n    pass\nprint([f()])\n"},
		{"a_void_body_beside_a_number", "def f():\n    pass\nprint([f(), 1])\n"},
		{"a_void_body_in_a_set", "def f():\n    pass\nprint({f()})\n"},
		{"a_void_body_as_a_dict_value", "def f():\n    pass\nprint({1: f()})\n"},
		{"a_void_body_handed_back_by_another_body", "def f():\n    pass\ndef g():\n    return [f()]\nprint(g())\n"},
		{"a_dict_miss_in_a_list", "print([{\"a\": 1}.get(\"z\")])\n"},
		{"a_mutators_answer_in_a_list", "xs = [1]\nprint([xs.append(2)])\nprint(xs)\n"},
		{"a_void_bound_name_beside_the_controls", "k = None\nprint([k])\nprint([None])\nprint([1, 2, 3])\n"},
		{"a_name_rebound_away_from_the_void", "k = None\nk = 5\nprint([k])\n"},
		{"a_loop_variable_is_this_item", "k = None\nfor k in [1, 2]:\n    print([k])\n"},
		// The row that used to sit in the wrong-answer table below — `for k in [None, 1.5]:` `print([k])` —
		// joined this table with **ADR 0424**: the literal beside the item now asks the record the unroll
		// wrote, which is roadmap **Gap R.440** closed (roadmap **Gap R.440**, `loop_item_literal_test.go`).
		{"a_loop_items_own_kind_put_into_a_literal", "for k in [None, 1.5]:\n    print([k])\n"},
	} {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			path := writeSrc(t, t.TempDir(), "void_slot.gy", tc.src)
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

// TestTheVoidSlotRoadsAnswerWrongArePinnedBesideTheReference holds what this row leaves owed. Every
// `todayBytes` and `todayExit` is what THIS backend does, measured 2026-10-24 against a build of the row's
// parent (the two are byte-identical, which is the evidence that the arm landed here did not write them),
// and every `reference` is what CPython prints for the same file, asked inside the case. An answer that
// arrives here fails the case, and the failure text says to delete the row and close the named one.
func TestTheVoidSlotRoadsAnswerWrongArePinnedBesideTheReference(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name       string
		src        string
		todayExit  int
		todayBytes string
		reference  string
		owedBy     string
	}{
		{"the_void_handed_to_a_body", "def g(v):\n    return [v]\nprint(g(None))\n", 0, "[0]\n", "[None]\n",
			"Gap R.437 (the void arm asks the FRAME's records, and a parameter's argument was never in them)"},
		{"the_modules_void_read_from_inside_a_body", "k = None\ndef g():\n    print([k])\ng()\n", 0, "[0]\n", "[None]\n",
			"Gap R.437 (a body keeps its own void record; the module's never reaches the gate)"},
		{"the_fold_over_a_void_element", "print(sum([None]))\n", 0, "0\n", "",
			"Gap R.438 (the reference raises TypeError for `int` + `NoneType`; the fold added the void's payload)"},
		{"a_sort_over_the_void_and_a_number", "print(sorted([1, None]))\n", 0, "[None, 1]\n", "",
			"Gap R.438 (the comparator asked the payloads and never asked the kinds)"},
		{"a_container_the_program_named_beside_a_number", "xs = [1]\nprint([xs, 2])\n", 0, "[0, 2]\n", "[[1], 2]\n",
			"Gap R.439 (the gate's element census stops at the spelled container and never asks the name)"},
		{"two_containers_the_program_named_in_one_literal", "xs = [1]\nys = [2]\nprint([xs, ys])\n", 0, "[0, 1]\n", "[[1], [2]]\n",
			"Gap R.439 (the two heap handles printed as the two numbers the allocator handed out)"},
	} {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			path := writeSrc(t, t.TempDir(), "void_slot_wrong.gy", tc.src)
			refOut, refCode := oracleTrap(t, path)
			if tc.reference == "" {
				// The reference's answer is a raise, so the bytes to compare are its last line.
				last := strings.TrimRight(refOut, "\n")
				if refCode == 0 {
					t.Fatalf("the reference exited 0 on a program meant to raise: %q — re-measure the row", last)
				}
				if !strings.Contains(last, "TypeError") {
					t.Fatalf("the reference's raise is %q, not the TypeError this pin names — re-measure the row", last)
				}
			} else if refCode != 0 || refOut != tc.reference {
				t.Fatalf("the reference moved: %q exit %d, this pin expects %q — re-measure the row",
					refOut, refCode, tc.reference)
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

// TestTheVoidSlotRoadsStillRefuseWhatTheNamedRowsOwe is the refusals table: a program the reference
// answers that this backend declines, named by the row that owes the answer. A refusal is not a wrong
// answer, and these are not this row's debt — but they are the neighbours a reader of this row will try
// next, so they are said out loud with their owner attached.
func TestTheVoidSlotRoadsStillRefuseWhatTheNamedRowsOwe(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct{ name, src, owedBy string }{
		{"a_void_slot_read_into_a_one_word_position", "k = None\nprint([k][0] == None)\n",
			"Gap R.146 / ADR 0331 (the pair door answers print, str, len, membership and the loop; the equality position keeps one word)"},
		{"the_fold_of_a_plain_int_element_beside_a_void", "k = None\nprint([k, 1][1] + 1)\n",
			"Gap R.198 (the fold door's untaggable shapes stay refusals in words)"},
		{"the_truth_of_a_container_asked_as_a_call", "k = None\nprint(bool([k]))\n",
			"Gap R.131 (bool() over a container has no length door in this backend)"},
		{"count_over_the_void_slot", "k = None\nprint([k].count(None))\n",
			"Gap I.2 (a run-time search needs the value word that carries its own kind)"},
	} {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			path := writeSrc(t, t.TempDir(), "void_slot_owed.gy", tc.src)
			for _, engine := range cliEngines {
				out, code := cliRunCode(t, engine, path)
				if code == 0 {
					t.Errorf("%s answers a source the roadmap says %s owes: %q — retire this pin", engine, tc.owedBy, out)
				}
			}
		})
	}
}
