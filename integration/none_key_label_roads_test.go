package integration

import (
	"strings"
	"testing"
)

// This file is the reference leg of Gap R.285 (ADR 0419).
//
// The unit file (pkg/lang/none_key_label_roads_test.go) pins what the roads emit. This one asks the only
// oracle left since ADR 0302 — CPython — what each of these programs prints, traps and exits, and requires
// the compiled toolchain, through `--aot` and `opt`/`llc`, to answer the same thing. Where an answer is
// refused rather than wrong, that is said, and the row that owes it is named.
//
// The row's own shape is the disagreement between roads: `d = {None: "a"}` / `print(d[None] + "b")` was
// refused while the `1`-keyed twin answered, and `print(d[None] * 2)` printed `0` — the interned index of
// the entry's text, added as a number. Both halves are answered here, and both halves of the *key*
// identity are pinned: the void is its own key, and a verdict is the number the reference says it is.

// TestTheVoidKeyLabelRoadsAnswerWhatCPythonAnswers is the answering half of the reference leg: every road
// that labels a dict entry's operand, driven by a `None`- or verdict-keyed literal.
func TestTheVoidKeyLabelRoadsAnswerWhatCPythonAnswers(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct{ name, src, want string }{
		{"concatenation", "d = {None: \"a\"}\nprint(d[None] + \"b\")\n", "ab\n"},
		{"repeat", "d = {None: \"a\"}\nprint(d[None] * 2)\n", "aa\n"},
		{"repeat_with_the_number_first", "d = {None: \"a\"}\nprint(2 * d[None])\n", "aa\n"},
		{"method", "d = {None: \"a\"}\nprint(d[None].upper())\n", "A\n"},
		{"interpolation", "d = {None: \"a\"}\nprint(f\"x{d[None]}y\")\n", "xay\n"},
		{"str_and_repr", "d = {None: \"a\"}\nprint(str(d[None]), repr(d[None]))\n", "a 'a'\n"},
		{"length_and_equality", "d = {None: \"abc\"}\nprint(len(d[None]) == 3, d[None] == \"abc\")\n", "True True\n"},
		{"two_key_kinds", "d = {None: \"a\", 1: \"b\"}\nprint(d[None] + d[1])\n", "ab\n"},
		{"a_text_that_spells_a_verdict", "d = {None: \"True\"}\nprint(d[None] + \"z\")\n", "Truez\n"},
		{"membership_over_two_key_kinds", "d = {None: 1, \"name\": 2}\nprint(None in d, \"name\" in d, 0 in d)\n", "True True False\n"},
		{"the_void_key_prints_itself", "d = {None: \"a\", 0: \"b\"}\nfor k in d:\n    print(k)\n", "None\n0\n"},
		{"the_loop_over_a_text_entry_steps_the_characters", "d = {None: \"ab\"}\nfor c in d[None]:\n    print(c)\n", "a\nb\n"},
		{"verdict_key_concatenation", "d = {True: \"a\"}\nprint(d[True] + \"b\")\n", "ab\n"},
		{"verdict_key_keeps_the_number_identity", "d = {True: \"a\"}\nprint(d[1] + \"b\")\n", "ab\n"},
		{"number_key_keeps_its_own_roads", "d = {0: \"a\"}\nprint(d[0] * 3)\n", "aaa\n"},
		{"container_slot_under_the_tag", "d = {None: [1, 2, 3]}\nprint(len(d[None]))\n", "3\n"},
		{"number_slot_under_the_tag", "d = {None: 2}\nprint(d[None] + 1)\n", "3\n"},
		{"list_slot_repeats", "d = {None: [1]}\nprint(d[None] * 2)\n", "[1, 1]\n"},
		{"both_entries_print", "d = {None: 1, 0: \"two\"}\nprint(d)\n", "{None: 1, 0: 'two'}\n"},
		{"the_entry_in_a_list", "d = {None: \"a\"}\nprint([d[None], \"b\"])\n", "['a', 'b']\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			path := writeSrc(t, t.TempDir(), "void_key_roads.gy", tc.src)
			refOut, refCode := oracleTrap(t, path)
			if refCode != 0 {
				t.Fatalf("the reference exited %d on a program meant to answer:\n%s", refCode, refOut)
			}
			if refOut != tc.want {
				t.Fatalf("the reference prints %q, want %q", refOut, tc.want)
			}
			for _, engine := range cliEngines {
				combined := cliRun(t, engine, path)
				if _, code := cliRunCode(t, engine, path); code != 0 {
					t.Errorf("%s exited %d on a program the reference answers: %s", engine, code, combined)
				}
				if combined != tc.want {
					t.Errorf("%s printed %q, the reference printed %q", engine, combined, tc.want)
				}
			}
		})
	}
}

// TestABoundKeyCarriesTheKindItsBindingWroteAtTheCLI is the reference leg of roadmap **Gap R.286**: the
// key of a dict read arrived through a name, and the read asked the object for the name's payload with no
// tag beside it — so the void's 0 was looked up as an integer and `d = {None: 3.5}` / `k = None` /
// `print(d[k])` died `KeyError` where CPython prints `3.5`. Every expectation below is the bytes the live
// reference printed for the same file, asked in the same test (`oracleTrap`), so the compiled leg cannot
// certify itself (ADR 0302, ADR 0420).
func TestABoundKeyCarriesTheKindItsBindingWroteAtTheCLI(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct{ name, src string }{
		{"the_void_key_through_a_name_prints_its_entry", "d = {None: 3.5}\nk = None\nprint(d[k])\n"},
		{"the_void_key_through_a_name_concatenates", "d = {None: \"a\"}\nk = None\nprint(d[k] + \"b\")\n"},
		{"the_void_key_through_a_name_repeats", "d = {None: \"a\"}\nk = None\nprint(d[k] * 2)\n"},
		{"the_void_key_through_a_name_takes_the_method_road", "d = {None: \"a\"}\nk = None\nprint(d[k].upper())\n"},
		{"the_void_key_through_a_name_loops_over_the_entry", "d = {None: \"ab\"}\nk = None\nfor ch in d[k]:\n    print(ch)\n"},
		{"the_void_key_through_a_name_measures_a_list_entry", "d = {None: [1, 2]}\nk = None\nprint(len(d[k]))\n"},
		{"the_void_and_the_number_stay_two_keys_through_names", "d = {None: 1, 0: 2}\nk = None\nprint(d[k])\n"},
		{"the_number_key_through_a_name_is_untouched", "d = {None: 1, 0: 2}\nk = 0\nprint(d[k])\n"},
		{"a_verdict_reads_the_verdict_key", "d = {True: \"one\"}\nk = True\nprint(d[k])\n"},
		{"a_verdict_reads_the_number_key_it_is", "d = {1: \"one\"}\nk = True\nprint(d[k])\n"},
		{"a_bound_dict_loops_and_adds_its_slots", "d = {1: 2, 3: 4}\nk = 1\nt = 0\nfor key in d:\n    t = t + d[key] + d[k]\nprint(t)\n"},
	} {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			path := writeSrc(t, t.TempDir(), "bound_key.gy", tc.src)
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

// TestTheVoidKeyCrossingsTrapTheWayCPythonTraps is the raising half. The crossings the static key door must
// NOT make — the void with 0, False, 0.0 and "" — are traps, and the compiled leg's last line is the
// reference's own last line, one class name and one sentence.
func TestTheVoidKeyCrossingsTrapTheWayCPythonTraps(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct{ name, src string }{
		{"void_reads_the_zero_entry", "d = {0: \"a\"}\nprint(d[None] + \"b\")\n"},
		{"zero_reads_the_void_entry", "d = {None: \"a\"}\nprint(d[0] * 2)\n"},
		{"void_reads_the_false_entry", "d = {False: \"a\"}\nprint(d[None] + \"b\")\n"},
		{"false_reads_the_void_entry", "d = {None: \"a\"}\nprint(d[False] + \"b\")\n"},
		{"void_reads_the_empty_text_entry", "d = {\"\": \"a\"}\nprint(d[None] + \"b\")\n"},
		{"void_reads_the_fraction_entry", "d = {0.0: \"a\"}\nprint(d[None] + \"b\")\n"},
		{"the_loop_over_a_number_entry", "d = {None: 5}\nfor x in d[None]:\n    print(x)\n"},
		{"membership_in_a_number_entry", "d = {None: 5}\nprint(1 in d[None])\n"},
		// The element-of-an-element read, which this row was first filed against and which the pair door
		// already answered correctly: the base of the second subscript is the INTEGER 1, and the compiled
		// leg raises the reference's own sentence for it.
		{"an_integer_slot_subscripted", "print([1, [5, 6]][0][1])\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			path := writeSrc(t, t.TempDir(), "void_key_traps.gy", tc.src)
			pyOut, pyCode := oracleTrap(t, path)
			if pyCode == 0 {
				t.Fatalf("the reference answers a program the table says raises: %q", pyOut)
			}
			pyLine := lastLine(pyOut)
			for _, engine := range cliEngines {
				// The traceback is the report, and the report goes on stderr: `cliRun` is the combined
				// stream the sentence has to be read out of, and `cliRunCode` the exit the contract owns.
				combined := cliRun(t, engine, path)
				if _, code := cliRunCode(t, engine, path); code == 0 {
					t.Errorf("%s exited 0 on a program the reference dies on: %q", engine, combined)
					continue
				}
				if got := lastLine(combined); got != pyLine {
					t.Errorf("%s raised %q, the reference raised %q", engine, got, pyLine)
				}
			}
		})
	}
}

// TestTheVoidKeyTextRoadIsTheTextRoad is the IR assertion. The bug this row fixes was invisible in
// behaviour: a repeat of a void-keyed text entry emitted an `add` over the interned index and printed `0`.
// The compiled module must put the entry on the runtime's text roads.
func TestTheVoidKeyTextRoadIsTheTextRoad(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name, src, want string
		deny            []string
	}{
		{
			name: "concatenation",
			src:  "d = {None: \"a\"}\nprint(d[None] + \"b\")\n",
			want: "rt_str_cat",
			deny: []string{"add i32"},
		},
		{
			name: "repeat",
			src:  "d = {None: \"a\"}\nprint(d[None] * 2)\n",
			want: "rt_str_repeat",
			deny: []string{"add i32"},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			ir, irErr, irCode := cliStreams(t, "--emit-llvm", tc.src)
			if irCode != 0 {
				t.Fatalf("--emit-llvm exited %d:\n%s", irCode, irErr)
			}
			body := cliMainBody(ir)
			if !strings.Contains(body, "call i32 @"+tc.want+"(") {
				t.Errorf("the %s of a void-keyed text entry never reached the runtime's text road (%s):\n%s",
					tc.name, tc.want, body)
			}
			for _, deny := range tc.deny {
				if strings.Contains(body, deny) {
					t.Errorf("the %s road arithmetic over the interned index is back (%s):\n%s", tc.name, deny, body)
				}
			}
		})
	}
}

// TestTheVoidKeyRoadsAnswerWrongArePinnedBesideTheReference is the ledger for the three shapes this row
// walked into rather than through: they COMPILE, and the answer they give is the reference's neighbour
// rather than its answer — or, for the concatenation, a trap the reference never raises. Pinning today's
// bytes next to the reference's is what makes a later cycle that closes the row notice (the case fails when
// the wrong answer stops being wrong), and it is the only honest shape for a defect the owed-row table
// cannot hold: that table demands a refusal, and these three are not refusals.
//
// All three reproduce on this row's parent (`/tmp/gusty.stub` at 7cbffb5 prints `[1, 1]` for the int-key
// twin of the repeat, and `3` for the fractional `max`), which is the evidence that the void arm discovered
// them instead of writing them.
func TestTheVoidKeyRoadsAnswerWrongArePinnedBesideTheReference(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name       string
		src        string
		todayExit  int    // the exit code this backend reaches, measured 2026-10-24
		todayBytes string // what this backend prints (or raises) at that exit
		reference  string // what CPython prints
		owedBy     string
	}{
		{"a_tuple_valued_slot_repeated", "d = {None: (1,)}\nprint(d[None] * 2)\n", 0, "[1, 1]\n", "(1, 1)\n",
			"Gap R.434 (the repeat road asks a KIND where the pair door handed it a TAG)"},
		{"a_tuple_valued_slot_concatenated", "d = {None: (1,)}\nprint(d[None] + (2,))\n", 3,
			`can only concatenate list (not "tuple") to list`, "(1, 2)\n",
			"Gap R.434 (the same door, the concatenation arm: the slot's tuple is a list by the time the runtime asks)"},
		{"a_fractional_slot_through_max_into_a_call", "def f(v):\n    return v\nd = {None: 3.5}\nprint(f(max(d[None], 2)))\n", 0, "3\n", "3.5\n",
			"Gap R.433 (the min/max fold declines a fractional slot read, and the pair road carries one word)"},
		{"the_void_in_a_name_put_into_a_list_literal", "k = None\nprint([k])\n", 0, "[0]\n", "[None]\n",
			"Gap R.435 (the list literal's recorded element kind outranks the element's own kind)"},
	} {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			path := writeSrc(t, t.TempDir(), "void_key_wrong.gy", tc.src)
			refOut, refCode := oracleTrap(t, path)
			if refCode != 0 || refOut != tc.reference {
				t.Fatalf("the reference moved: %q exit %d, this pin expects %q — re-measure the row",
					refOut, refCode, tc.reference)
			}
			for _, engine := range cliEngines {
				stdout, stderr, code := cliStreams(t, engine, path)
				if code != tc.todayExit {
					t.Fatalf("%s exited %d on a shape pinned as exit %d (%s): stdout %q stderr %q — this pin does not "+
						"describe what happened here, and the named row may have been closed", engine, code, tc.todayExit, tc.owedBy, stdout, stderr)
				}
				if !strings.Contains(stdout+stderr, tc.todayBytes) {
					t.Errorf("%s printed %q / raised %q; this pin holds today's bytes %q, and the reference prints %q (%s). "+
						"If the answer is now the reference's, DELETE this row and close the named one.",
						engine, stdout, stderr, tc.todayBytes, tc.reference, tc.owedBy)
				}
			}
		})
	}
}

// TestTheVoidKeyRoadsStillRefuseWhatTheNamedRowsOwe keeps the refusals honest: a slot the static label
// cannot name at all is still refused, and the row that owes it is named. A source that starts answering
// fails this case, so an owed row cannot be silently paid and forgotten.
func TestTheVoidKeyRoadsStillRefuseWhatTheNamedRowsOwe(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct{ name, src, owedBy string }{
		{"the_sorted_key_road", "print(sorted({None: 1}.keys()))\n", "Gap R.247 (a `sorted` argument neither door can name)"},
		{"a_slot_index_reached_through_a_loop", "d = {None: \"a\", 0: \"b\"}\nr = \"\"\nfor k in d:\n    r = r + d[k]\nprint(r)\n", "Gap R.146 (a pair in a one-word index position)"},
		{"a_key_collection_handed_to_list", "print(list({None: 1}.keys()))\n", "Gap R.247 (the same argument door, the `list` road)"},
		// The operator road over an entry addressed by a bound NUMBER key — the number twin of the row
		// ADR 0420 closes, whose READ already answers (`d = {None: 1, 0: 2}` / `k = 0` / `print(d[k])` is `2`).
		// Byte-identical on this commit's parent.
		{"an_operator_road_over_an_entry_addressed_by_a_bound_number_key", "s = {None: \"a\", 0: \"zero\"}\nk = 0\nprint(s[k] + \"!!\")\n", "Gap R.436 (the static door has no number-in-a-name record to retire)"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			path := writeSrc(t, t.TempDir(), "void_key_owed.gy", tc.src)
			for _, engine := range cliEngines {
				out, code := cliRunCode(t, engine, path)
				if code == 0 {
					t.Errorf("%s answers a source the roadmap says %s owes: %q — retire this pin", engine, tc.owedBy, out)
				}
			}
		})
	}
}
