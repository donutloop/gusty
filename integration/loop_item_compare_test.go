package integration

import (
	"strings"
	"testing"
)

// This file is the reference leg of roadmap **Gap R.416** (**ADR 0426**): the identity, equality and
// membership question asked of an item of an unrolled `for` over a literal list.
//
// The unit file (pkg/lang/loop_item_compare_test.go) pins what the roads emit — the pair door's load of the
// item's own slot, `@rt_payload_eq`, `@rt_contains_tagged`, and the two controls that must emit NEITHER. This
// one asks the only oracle left since ADR 0302, CPython, what each program prints, and requires the compiled
// toolchain to answer the same bytes at the same exit code. Where an answer is still owed it is pinned with
// today's bytes beside the reference's and the row that owns the fix is named, so a later fix fails the pin
// instead of quietly leaving it behind.
//
// The row's shape was one item answered by two doors: `print(v)` asked the kind the `for` road keeps per
// iteration and printed `None`, while `print(v is None)` asked the payload and printed `False` — and the
// payload a void leaves behind is `0`, the same word an interned text's slot carries when the string happens
// to intern to index 0 (**ADR 0187**, **ADR 0232**).

// loopItemCompareSources is the family every leg shares: the row's own programs, the payload collisions they
// stand for, and the controls whose cheap roads must survive the widening.
func loopItemCompareSources() []struct {
	name string
	src  string
} {
	return []struct {
		name string
		src  string
	}{
		{"the_rows_own_program_identity", "for v in [None, \"a\"]:\n    print(v is None)\n"},
		{"the_rows_own_program_equality", "for v in [None, \"a\"]:\n    print(v == None)\n"},
		{"the_negated_identity", "for v in [None, \"a\"]:\n    print(v is not None)\n"},
		{"the_negated_equality", "for v in [None, \"a\"]:\n    print(v != None)\n"},
		{"both_spellings_of_one_question", "for v in [None, \"a\"]:\n    print(v == None, v is None)\n"},
		{"the_void_item_beside_a_zero", "for v in [0, None]:\n    print(v == None)\n"},
		{"the_void_item_against_zero", "for v in [None, 2]:\n    print(v == 0)\n"},
		{"a_fractional_item_equals_itself", "for v in [1.5, 2.5]:\n    print(v == 1.5)\n"},
		{"a_fractional_item_equals_a_number", "for v in [1.5, 2]:\n    print(v == 2)\n"},
		{"the_item_equals_itself", "for v in [None, 1.5]:\n    print(v == v)\n"},
		{"a_verdict_item_equals_a_number", "for v in [True, 1]:\n    print(v == True)\n"},
		{"the_item_in_its_own_iterable", "for k in [None, 1.5]:\n    print(k in [None, 1.5])\n"},
		{"the_item_not_in_its_own_iterable", "for k in [None, 1.5]:\n    print(k not in [None, 1.5])\n"},
		{"a_void_item_in_a_text_list", "for v in [\"a\", None]:\n    print(v in [\"a\"])\n"},
		{"a_fractional_item_in_a_literal", "for v in [1.5, 2.5]:\n    print(v in [2.5])\n"},
		{"a_container_item_in_its_own_iterable", "for v in [[1, 2], 3]:\n    print(v in [[1, 2], 3])\n"},
		{"the_item_in_an_unrelated_literal", "for k in [None, 1.5]:\n    print(k in [1, 2])\n"},
		{"every_kind_asked_every_question", "for v in [None, True, 1.5, \"s\"]:\n    print(v == None, v == True, v == 1.5, v == \"s\")\n"},
		{"the_named_list_twin", "xs = [None, \"a\"]\nfor v in xs:\n    print(v is None)\n"},
		{"an_integer_item_keeps_its_road", "for v in [1, 2]:\n    print(v == 1)\n"},
		{"an_interned_text_item_keeps_its_road", "for v in [\"a\", \"b\"]:\n    print(v == \"a\")\n"},
		// The positions the verdict travels to: a condition, a short-circuit, a `while` head, a binding.
		{"the_verdict_in_a_condition", "for v in [None, 1.5]:\n    if v is None:\n        print(\"void\")\n    else:\n        print(\"other\")\n"},
		{"the_verdict_in_a_short_circuit", "for v in [None, 1.5]:\n    print(v is None and \"t\")\n"},
		{"the_verdict_in_a_while_head", "for v in [None]:\n    while v is None:\n        print(\"void\")\n        break\n"},
		{"the_verdict_bound_to_a_name", "for v in [None, 1.5]:\n    r = v == None\n    print(r)\n"},
		{"the_verdict_in_a_container", "for v in [None, 1.5]:\n    print([v == None, v is None])\n"},
		// The controls beside the answers: the same item printed, measured and put in a literal, which the
		// print and the builder roads answered already (**Gap R.358**, **ADR 0424**).
		{"the_item_printed_and_compared", "for v in [None, \"a\"]:\n    print(v, v == None)\n"},
		{"the_item_measured_and_compared", "for v in [None, 1.5]:\n    print(len([v]), v == None)\n"},
	}
}

// TestTheComparedLoopItemAtTheCLI is the answering half: every expectation is the bytes the live reference
// printed for the same file, asked in the same test, so the compiled leg cannot certify itself (ADR 0302).
func TestTheComparedLoopItemAtTheCLI(t *testing.T) {
	t.Parallel()
	for _, tc := range loopItemCompareSources() {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			path := writeSrc(t, t.TempDir(), "loop_item_compare.gy", tc.src)
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
					t.Errorf("%s printed %q, the reference printed %q\n%s", engine, combined, refOut, tc.src)
				}
			}
		})
	}
}

// TestTheComparedLoopItemNeverSpendsTheCompilerExitCode is the ADR 0166 half: a shape this row answers may not
// reach the assembler, and a shape it declines must be declined in words at exit 1 — never at exit 2, and
// never with the compiler's own machinery in the sentence.
func TestTheComparedLoopItemNeverSpendsTheCompilerExitCode(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name string
		src  string
	}{
		{"the void item's equality", "for v in [None, \"a\"]:\n    print(v == None)\n"},
		{"the fractional item's equality", "for v in [1.5, 2.5]:\n    print(v == 1.5)\n"},
		{"the item's membership", "for k in [None, 1.5]:\n    print(k in [None, 1.5])\n"},
		{"the container item's membership", "for v in [[1, 2], 3]:\n    print(v in [[1, 2], 3])\n"},
		{"the integer item's equality", "for v in [1, 2]:\n    print(v == 1)\n"},
		{"the text item's equality", "for v in [\"a\", \"b\"]:\n    print(v == \"a\")\n"},
		{"the verdict item's equality", "for v in [True, 1]:\n    print(v == True)\n"},
		{"the item's equality in a condition", "for v in [None, 1.5]:\n    if v is None:\n        print(\"void\")\n"},
	} {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			path := writeSrc(t, t.TempDir(), "loop_item_verify.gy", tc.src)
			for _, engine := range cliEngines {
				combined, code := cliRunCode(t, engine, path)
				if code == 2 {
					t.Errorf("%s spent the compiler-bug exit code on this program: %s\n%s", engine, combined, tc.src)
				}
				if code != 0 {
					t.Errorf("%s exited %d on a program the reference answers at 0: %s\n%s", engine, code, combined, tc.src)
				}
				if strings.Contains(combined, "%!s(") || strings.Contains(combined, "*lang.") || strings.Contains(combined, "__cans") {
					t.Errorf("%s leaked the compiler's own machinery into a user's sentence: %s", engine, combined)
				}
			}
			if out, code := cliReport(t, "--verify-llvm-file", path); code != 0 || !strings.Contains(out, "module verified") {
				t.Errorf("LLVM's module verifier did not pass the program this shape emits (exit %d):\n%s", code, out)
			}
		})
	}
}

// TestTheComparedLoopItemRoadsAnswerWrongArePinnedBesideTheReference holds what this row leaves owed. Every
// `todayBytes`/`todayExit` is what THIS backend does, measured against a build of the row's parent in a
// `git worktree` as well as this one, and every `reference` is what CPython prints for the same file, asked
// inside the case. An answer that arrives here fails the case, and the failure text says to delete the row and
// close the named one.
func TestTheComparedLoopItemRoadsAnswerWrongArePinnedBesideTheReference(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name       string
		src        string
		todayExit  int
		todayBytes string
		reference  string
		owedBy     string
	}{
		{"the_fractional_item_ordered_against_a_number", "for v in [1.5, 2.5]:\n    print(v < 2)\n", 0, "True\nTrue\n",
			"True\nFalse\n",
			"Gap R.448 (the ordering door has its own shape machinery — `orderShapeOf` never asks the unroll's record, so the item's @float_box handle reaches the icmp)"},
		{"the_void_item_ordered_against_a_number", "for v in [None, 2.5]:\n    print(v < 3)\n", 0, "True\nTrue\n",
			"raises TypeError: '<' not supported between instances of 'NoneType' and 'int'\n",
			"Gap R.448 (a wrong verdict at exit 0 where the reference spends a trap — ADR 0166's second class)"},
		{"the_items_equality_asked_in_an_if_head", "for v in [None, \"a\"]:\n    if v == None:\n        print(\"void\")\n    else:\n        print(\"other\")\n", 0,
			"void\nvoid\n", "void\nother\n",
			"Gap R.449 (`truthOperandErr` asks the pair door but not the None-equality fold the value road asks, so the text item's intern index 0 reads as the void's payload)"},
		{"the_same_fold_asked_outside_any_loop", "s = \"a\"\nif s == None:\n    print(\"void\")\nelse:\n    print(\"other\")\n", 0,
			"void\n", "other\n",
			"Gap R.449 (no loop item involved at all: the condition road and the value road give `s == None` two answers)"},
		{"the_container_item_compared_with_a_container_literal", "for v in [[1, 2], 3]:\n    print(v == [1, 2])\n", 1,
			"comparing a container with", "True\nFalse\n",
			"Gap R.450 (`containerEquality` asks `isProvenScalarExpr` of the side that is not a container, and that door does not read the item's record — a refusal in words, exit 1, never exit 2)"},
	} {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			path := writeSrc(t, t.TempDir(), "loop_item_owed.gy", tc.src)
			refOut, refCode := oracleTrap(t, path)
			if strings.HasPrefix(tc.reference, "raises ") {
				if refCode == 0 {
					t.Fatalf("this pin's reference column says the reference raises, and it exited 0 with %q — re-measure the row", refOut)
				}
			} else if refCode != 0 {
				t.Fatalf("the reference exited %d on a program this pin says answers %q:\n%s", refCode, tc.reference, refOut)
			} else if tc.reference != "" && refOut != tc.reference {
				t.Fatalf("this pin's reference column is stale: CPython printed %q, the pin says %q", refOut, tc.reference)
			}
			for _, engine := range cliEngines {
				combined := cliRun(t, engine, path)
				if _, code := cliRunCode(t, engine, path); code != tc.todayExit {
					t.Errorf("%s exited %d, this pin expects %d — an answer that arrived here means the row was closed and this table did not follow\n%s",
						engine, code, tc.todayExit, tc.src)
				}
				if !strings.Contains(combined, tc.todayBytes) {
					t.Errorf("%s printed %q, this pin holds %q (%s)\n%s", engine, combined, tc.todayBytes, tc.owedBy, tc.src)
				}
			}
		})
	}
}
