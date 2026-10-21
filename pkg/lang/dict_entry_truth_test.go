package lang

import (
	"strings"
	"testing"
)

// dict_entry_truth_test.go — the condition asks a self-describing dict entry the (payload, tag) pair that
// print already asks (roadmap Gap R.284, ADR 0388).
//
// `d = {None: "hi"}` / `print(d[None])` has written `hi` since ADR 0353, while `if d[None]:` took the
// `else` arm: the read reached the condition road, which tested the interned text's `@str_tab` index
// against zero. `bool({1: []}[1])` answered `True` for the same reason — the payload of an empty list is a
// heap handle, and a handle is never zero. Both are wrong answers of the worst kind: exit 0, no diagnostic,
// and the branch that did not run leaves no trace for a reader to find.
//
// The door already existed. `@rt_pair_truth` is the table `and`/`or` consult for the operand it chooses
// (ADR 0275) and the one a literal's element was decided by (ADR 0331); the condition's head simply never
// asked it for a dict entry. What this row changes is therefore a gate, in two places: the head of
// `if`/`elif`/`while` and the head of a ternary (`truthOperandErr`), and the builtin `bool()`. Both now ask
// the same question the print door asks — `entryReadNeedsTheTag` — and hand the pair to the same table.
//
// A row is silent where the entry's kind is already the program's fact: `d = {1: 2}` / `if d[1]:` keeps the
// integer compare it always had. Those two programs are asserted below with the IR claim that the pair
// table is *absent*, so a gate that widened past what it needs is caught by this file rather than by a
// reader.

// entryTruthKinds is the answer ladder: every entry kind whose payload means something other than itself,
// in both the arm question and the `bool()` question, read off CPython before this file was written.
func entryTruthKinds() []struct {
	name, src, want string
} {
	arms := func(entry, yes, no string) []struct {
		name, src, want string
	} {
		return []struct {
			name, src, want string
		}{
			{"arm_" + entry, "d = {None: " + yes + "}\nif d[None]:\n    print(\"t\")\nelse:\n    print(\"f\")\n", "t\n"},
			{"not_" + entry, "d = {None: " + yes + "}\nif not d[None]:\n    print(\"t\")\nelse:\n    print(\"f\")\n", "f\n"},
			{"bool_" + entry, "d = {None: " + yes + "}\nprint(bool(d[None]))\n", "True\n"},
			{"tern_" + entry, "d = {None: " + yes + "}\nprint(\"y\" if d[None] else \"n\")\n", "y\n"},
			{"armno_" + entry, "d = {None: " + no + "}\nif d[None]:\n    print(\"t\")\nelse:\n    print(\"f\")\n", "f\n"},
			{"boolno_" + entry, "d = {None: " + no + "}\nprint(bool(d[None]))\n", "False\n"},
		}
	}
	var out []struct {
		name, src, want string
	}
	out = append(out, arms("text", `"hi"`, `""`)...)
	out = append(out, arms("float", `3.5`, `0.0`)...)
	out = append(out, arms("negzero_float", `3.5`, `-0.0`)...)
	out = append(out, arms("int", `7`, `0`)...)
	out = append(out, arms("verdict", `True`, `False`)...)
	out = append(out, arms("list", `[1]`, `[]`)...)
	out = append(out, arms("dict", `{1: 2}`, `{}`)...)
	out = append(out,
		struct {
			name, src, want string
		}{"none_entry_arm", "d = {None: None}\nif d[None]:\n    print(\"t\")\nelse:\n    print(\"f\")\n", "f\n"},
		struct {
			name, src, want string
		}{"none_entry_bool", "d = {None: None}\nprint(bool(d[None]))\n", "False\n"},
	)
	return out
}

func TestTheTruthOfASelfDescribingDictEntryTakesTheReferencesArm(t *testing.T) {
	t.Parallel()
	for _, tc := range entryTruthKinds() {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := compiledOut(t, tc.src); got != tc.want {
				t.Errorf("%s printed %q, want %q — the condition tested the entry's payload, not its tag", tc.src, got, tc.want)
			}
		})
	}
}

// The key spelling is not the defect. ADR 0353's lesson, re-asserted on this road: the same entry under a
// `None`, `True`, `False`, int and text key must answer alike, because the tag rides beside the value and
// nothing about the key decides it.
func TestTheTruthOfTheEntryIsAskedTheSameWayUnderEveryKey(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name, src, want string
	}{
		{"none_key_truthy", "d = {None: 3.5}\nif d[None]:\n    print(\"t\")\nelse:\n    print(\"f\")\n", "t\n"},
		{"true_key_falsy", "d = {True: 0.0}\nif d[True]:\n    print(\"t\")\nelse:\n    print(\"f\")\n", "f\n"},
		{"false_key_bool", "d = {False: \"x\"}\nprint(bool(d[False]))\n", "True\n"},
		{"int_key_falsy", "d = {7: -0.0}\nprint(bool(d[7]))\n", "False\n"},
		{"text_key_falsy", "d = {\"k\": \"\"}\nif d[\"k\"]:\n    print(\"t\")\nelse:\n    print(\"f\")\n", "f\n"},
		{"int_key_truthy_list", "d = {7: [0]}\nprint(bool(d[7]))\n", "True\n"},
		{"same_dict_two_keys_agree", "d = {None: 0.0, \"k\": 0.0}\nprint(bool(d[None]), bool(d[\"k\"]))\n", "False False\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := compiledOut(t, tc.src); got != tc.want {
				t.Errorf("%s printed %q, want %q", tc.src, got, tc.want)
			}
		})
	}
}

// The while head and the ternary head are the same road as the if head, and the loop is the shape where a
// wrong truth is not one wrong line but a wrong number of iterations.
func TestTheTruthOfTheEntryIsAskedOnEveryConditionRoad(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name, src, want string
	}{
		{
			"while_head_counts_the_entries_truth_not_the_handles",
			"d = {None: 2}\nwhile d[None]:\n    print(\"once\")\n    d[None] = 0\n",
			"once\n",
		},
		{
			"while_head_does_not_run_for_a_falsy_entry",
			"d = {None: \"\"}\nwhile d[None]:\n    print(\"never\")\nprint(\"done\")\n",
			"done\n",
		},
		{
			"while_head_over_a_container_entry",
			"xs = {None: [1]}\nwhile xs[None]:\n    print(len(xs[None]))\n    xs[None] = []\n",
			"1\n",
		},
		{
			"ternary_head_beside_a_fold",
			"d = {None: 0.0}\nprint(sum([1 if d[None] else 0, 1]))\n",
			"1\n",
		},
		{
			"ternary_head_in_a_comprehension",
			"d = {None: \"x\"}\nprint([1 if d[None] else 0][0])\n",
			"1\n",
		},
		{
			"run_built_entry",
			"d = {}\nd[None] = 0.0\nif d[None]:\n    print(\"t\")\nelse:\n    print(\"f\")\n",
			"f\n",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := compiledOut(t, tc.src); got != tc.want {
				t.Errorf("%s printed %q, want %q", tc.src, got, tc.want)
			}
		})
	}
}

// The IR claim, stated where it can be checked: an entry whose kind is the object's fact reaches the pair
// table, and the numeric format that wrote the old wrong answers is not in the program's own body.
func TestTheTruthOfTheEntryReachesThePairTableThePrinterAlreadyUses(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct{ name, src string }{
		{"text_entry", "d = {None: \"hi\"}\nif d[None]:\n    print(\"t\")\n"},
		{"float_entry", "d = {None: 3.5}\nif d[None]:\n    print(\"t\")\n"},
		{"none_entry", "d = {None: None}\nif d[None]:\n    print(\"t\")\n"},
		{"container_entry", "d = {None: []}\nif d[None]:\n    print(\"t\")\n"},
		{"bool_call", "d = {None: 0.0}\nprint(bool(d[None]))\n"},
		{"ternary", "d = {None: \"x\"}\nprint(1 if d[None] else 0)\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			body := mainRegion(t, tc.src)
			if !strings.Contains(body, "@rt_pair_truth") {
				t.Errorf("%s: the condition did not ask the pair table:\n%s", tc.name, body)
			}
			if !strings.Contains(body, "@rt_dict_value_tag") {
				t.Errorf("%s: the condition read the entry without its stored tag:\n%s", tc.name, body)
			}
			if strings.Contains(body, "%d") {
				t.Errorf("%s: the truth was written by a numeric format:\n%s", tc.name, body)
			}
		})
	}
}

// …and the negative control that makes the claim above a measurement: an entry the literal already described
// as an integer keeps the one-word compare, and must not have grown the pair table because the gate learned
// to ask the object. Same spelling, provable kind.
func TestTheTruthOfAProvableIntegerEntryKeepsTheOneWordRoad(t *testing.T) {
	t.Parallel()
	for _, src := range []string{
		"d = {1: 2}\nif d[1]:\n    print(\"t\")\nelse:\n    print(\"f\")\n",
		"d = {1: 0}\nif d[1]:\n    print(\"t\")\nelse:\n    print(\"f\")\n",
		"d = {1: 2, 2: 3}\nif d[2]:\n    print(\"t\")\n",
		"d = {1: 2}\nprint(bool(d[1]))\n",
	} {
		t.Run(strings.ReplaceAll(src, "\n", " / "), func(t *testing.T) {
			t.Parallel()
			body := mainRegion(t, src)
			for _, helper := range []string{"@rt_pair_truth", "@rt_dict_value_tag"} {
				if strings.Contains(body, helper) {
					t.Errorf("%s reached %s though its entry is a provable integer:\n%s", src, helper, body)
				}
			}
		})
	}
}

// The boundary ADR 0388 drew. `and`/`or` are not the truth road: they hand back the operand they chose, so
// the answer itself has to carry the kind, which is Gap R.147's tagged value word and still refuses —
// The boundary ADR 0388 drew. `and`/`or` choose an operand by its truth and now reach the pair for a dict
// entry, so they are answered in TestTheEntryChoosesItsOperandInTheSamePairTable. What this row pins is the
// positions the door does NOT own: a binding and a call argument, which keep one word for a whole value.
// They are pinned as the answers and refusals they are today so this file cannot be read as claiming them.
func TestTheTruthOfTheEntryBoundaryStaysWhereADR0388DrewIt(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name, src, wantSub, row string
	}{
		{
			// The binding is a one-word position: the name keeps the payload and loses the tag, so `bool` of
			// the bound name is the missing tagged value word (Gap R.146), not this row's.
			name:    "a_bound_entry_keeps_its_own_row",
			src:     "d = {None: []}\nx = d[None]\nprint(bool(x))\n",
			wantSub: "holds a slot the program built at run time",
			row:     "Gap R.146",
		},
		{
			// The operator's chosen operand reaches `print`, which renders a pair; handed to `bool()` it has to
			// be ONE word, and that is Gap R.147's missing tagged value word.
			name:    "the_chosen_operand_handed_to_a_one_word_position_keeps_its_own_row",
			src:     "d = {None: \"hi\"}\nprint(bool(d[None] and 1))\n",
			wantSub: "chooses between two values whose kinds this pass cannot state in one word",
			row:     "Gap R.147",
		},
		{
			name:    "the_entry_read_handed_to_a_call_is_the_one_word_call_argument",
			src:     "def f(v):\n    return v\n\nd = {None: 3.5}\nprint(f(d[None]))\n",
			wantSub: "", // pinned as today's answer, not a refusal: see the note below
			row:     "Gap R.220",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if tc.wantSub != "" {
				out, isRefusal := compiledOutOrRefusal(t, tc.src)
				if !isRefusal {
					t.Fatalf("%s answered %q, want the %s refusal", tc.src, out, tc.row)
				}
				if !strings.Contains(out, tc.wantSub) {
					t.Errorf("%s refused without naming %q:\n%s", tc.src, tc.wantSub, out)
				}
				return
			}
			// The call argument keeps the payload-only answer: the one-word call boundary is Gap R.220's, and
			// this file pins it rather than pretending the truth road reaches it.
			if got := compiledOut(t, tc.src); got != "3\n" {
				t.Errorf("%s printed %q, want today's `3` — Gap R.220's one-word call argument, not this row's", tc.src, got)
			}
		})
	}
}

// `and` and `or` choose an operand by its truth, so they already read the pair table for the operand they
// tested — for a literal's element (ADR 0331) and for the operand a name carries (ADR 0275). What they could
// not do was *reach* the pair for a dict entry: the gate asked a narrower question and the road refused
// rather than guess. Now the entry's spelling chooses in the same table, and the operand it hands back keeps
// its own kind — the reference's answer, not the verdict the operator is not (roadmap Gap R.147's entry
// spelling, paid as a side of this door).
func TestTheEntryChoosesItsOperandInTheSamePairTable(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name, src, want string
	}{
		{"text_entry_and", "d = {None: \"hi\"}\nprint(d[None] and 1)\n", "1\n"},
		{"empty_text_entry_and", "d = {None: \"\"}\nprint(d[None] and 1)\n", "\n"},
		{"text_entry_or", "d = {None: \"hi\"}\nprint(d[None] or 2)\n", "hi\n"},
		{"empty_text_entry_or", "d = {None: \"\"}\nprint(d[None] or 2)\n", "2\n"},
		{"falsy_float_entry_or", "d = {None: 0.0}\nprint(d[None] or 2)\n", "2\n"},
		{"filled_float_entry_and", "d = {None: 3.5}\nprint(d[None] and 2)\n", "2\n"},
		{"empty_list_entry_or", "d = {None: []}\nprint(d[None] or 2)\n", "2\n"},
		{"filled_list_entry_and", "d = {None: [1]}\nprint(d[None] and 2)\n", "2\n"},
		{"void_entry_or_text", "d = {None: None}\nprint(d[None] or \"d\")\n", "d\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := compiledOut(t, tc.src); got != tc.want {
				t.Errorf("%s printed %q, want %q", tc.src, got, tc.want)
			}
		})
	}
}

// The same program asked of the record leg: the answer on record for these sources is the reference's, so a
// condition that branches the tag's way and a record that says otherwise would disagree out loud.
func TestTheTruthOfTheEntryRecordLegAgreesWithTheCompiledLeg(t *testing.T) {
	t.Parallel()
	srcs := []string{
		"d = {None: \"hi\"}\nif d[None]:\n    print(\"t\")\nelse:\n    print(\"f\")\n",
		"d = {None: \"\"}\nif d[None]:\n    print(\"t\")\nelse:\n    print(\"f\")\n",
		"d = {None: 0.0}\nprint(bool(d[None]))\n",
		"d = {None: -0.0}\nprint(bool(d[None]))\n",
		"d = {None: []}\nprint(bool(d[None]))\n",
		"d = {None: {}}\nprint(bool(d[None]))\n",
		"d = {None: None}\nprint(bool(d[None]))\n",
		"d = {None: \"hi\"}\nprint(\"y\" if d[None] else \"n\")\n",
		"d = {None: 2}\nwhile d[None]:\n    print(\"once\")\n    d[None] = 0\n",
		"d = {1: []}\nprint(bool(d[1]))\n",
		"d = {None: \"hi\"}\nprint(d[None] and 1)\n",
		"d = {None: \"\"}\nprint(d[None] or 2)\n",
		"d = {\"k\": \"\"}\nif d[\"k\"]:\n    print(\"t\")\nelse:\n    print(\"f\")\n",
		"d = {None: [1]}\nprint(d[None] and 2)\n",
		// The unrolled item, asked by the same table.
		"print([bool(x) for x in [0.0, 1.5, \"\", \"a\", None, True, False, [0], []]])\n",
		"print([1 if x else 0 for x in [0.0, \"\", None, [0], []]])\n",
		"ys = []\nfor x in [0.0, \"\", None, [0]]:\n    ys.append(bool(x))\nprint(ys)\n",
		"print([x for x in [0.0, \"\", None, [0], [], \"z\"] if x])\n",
		"print([bool(x) for x in [[], [0], \"\", \"a\"]])\n",
		"print(bool(\"\"))\n",
	}
	for _, src := range srcs {
		t.Run(strings.ReplaceAll(src, "\n", " / "), func(t *testing.T) {
			t.Parallel()
			got, err := runGoldenStdout(t, src)
			if err != nil {
				t.Fatalf("the record leg did not answer %s: %v", src, err)
			}
			want := compiledOut(t, src)
			if got != want {
				t.Errorf("the record prints %q, the compiled leg prints %q for %s", got, want, src)
			}
		})
	}
}

// The condition road's other self-describing read: the loop variable of an UNROLLED comprehension or `for`.
// ADR 0386 records each item's kind in `loopElemTag` and taught the print, the binding and the container
// doors to read it; the condition never did, so a float item (a `@float_box` handle) and an empty container
// (a handle of zero length) were both true. ADR 0388 sends this read to the same `@rt_pair_truth` table.
func TestTheTruthOfAnUnrolledLoopItemIsAskedOfItsOwnKind(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name, src, want string
	}{
		{
			"bool_of_each_kind",
			"print([bool(x) for x in [0.0, 1.5, \"\", \"a\", None, True, False, [0], []]])\n",
			"[False, True, False, True, False, True, False, True, False]\n",
		},
		{
			"ternary_in_the_element",
			"print([1 if x else 0 for x in [0.0, \"\", None, [0], []]])\n",
			"[0, 0, 0, 1, 0]\n",
		},
		{
			"bool_in_a_for_statement",
			"ys = []\nfor x in [0.0, \"\", None, [0]]:\n    ys.append(bool(x))\nprint(ys)\n",
			"[False, False, False, True]\n",
		},
		{
			"the_filter_clause",
			"print([x for x in [0.0, \"\", None, [0], [], \"z\"] if x])\n",
			"[[0], 'z']\n",
		},
		{
			"containers_ask_their_length",
			"print([bool(x) for x in [[], [0], \"\", \"a\"]])\n",
			"[False, True, False, True]\n",
		},
		{
			"negative_zero",
			"print([bool(x) for x in [0.0, -0.0, 0, -0]])\n",
			"[False, False, False, False]\n",
		},
		{
			"the_if_head_over_a_loop_item",
			"for x in [0.0, [0]]:\n    if x:\n        print(\"t\")\n    else:\n        print(\"f\")\n",
			"f\nt\n",
		},
		{
			"the_item_still_prints_its_own_kind",
			"print([x for x in [0.0, 1.5, \"\", \"a\", None, True, False, [0], []]])\n",
			"[0.0, 1.5, '', 'a', None, True, False, [0], []]\n",
		},
		{
			"an_integer_item_keeps_the_ordinary_answer",
			"print([bool(x) for x in [0, 1, 2]])\n",
			"[False, True, True]\n",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := compiledOut(t, tc.src); got != tc.want {
				t.Errorf("%s printed %q, want %q — the condition tested the item's payload, not its recorded kind", tc.src, got, tc.want)
			}
		})
	}
}

// …and the IR claim: the item's truth reaches the table, and the integer item — the case the loop has always
// answered — keeps the plain compare it had before this door opened.
func TestTheTruthOfTheLoopItemReachesThePairTableOnlyWhereTheKindIsTheItems(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct{ name, src string }{
		{"float_item", "print([bool(x) for x in [0.0, 1.5]])\n"},
		{"container_item", "print([bool(x) for x in [[], [0]]])\n"},
		{"void_item", "print([bool(x) for x in [None]])\n"},
		{"filter_over_items", "print([x for x in [0.0, [0]] if x])\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			body := mainRegion(t, tc.src)
			if !strings.Contains(body, "@rt_pair_truth") {
				t.Errorf("%s: the item's truth did not ask the pair table:\n%s", tc.name, body)
			}
		})
	}
	for _, src := range []string{
		"print([bool(x) for x in [0, 1, 2]])\n",
		"print([1 if x else 0 for x in [0, 1]])\n",
	} {
		t.Run("int_item_"+strings.ReplaceAll(src, "\n", " / "), func(t *testing.T) {
			t.Parallel()
			body := mainRegion(t, src)
			if strings.Contains(body, "@rt_pair_truth") {
				t.Errorf("%s reached the pair table though every item is a literal integer:\n%s", src, body)
			}
		})
	}
}

// The row ADR 0388 leaves open: the SET is the one kind whose truth the pair table never gets to answer, for
// the tag the objects carry for a set is not written into a dict entry or a literal's slot. Pinned here as
// today's answer, with the row that owes it, so the door above cannot be read as paying this one.
func TestTheTruthOfASetEntryIsStillAskedOfTheHandle(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name, src, actual, row string
	}{
		{
			name:   "an_empty_set_entry_is_true",
			src:    "d = {None: set()}\nprint(bool(d[None]))\n",
			actual: "True\n", // CPython: False
			row:    "Gap R.360",
		},
		{
			name:   "an_empty_set_item_is_true",
			src:    "print([bool(x) for x in [set()]])\n",
			actual: "[True]\n", // CPython: [False]
			row:    "Gap R.360",
		},
		{
			name:   "an_empty_set_entry_prints_its_handle",
			src:    "d = {None: set()}\nprint(d[None])\n",
			actual: "3\n", // CPython: set()
			row:    "Gap R.360",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := compiledOut(t, tc.src); got != tc.actual {
				t.Errorf("%s printed %q, want today's %q — the row that owns it is %s", tc.src, got, tc.actual, tc.row)
			}
		})
	}
}
