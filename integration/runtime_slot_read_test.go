package integration

import (
	"strings"
	"testing"
)

// runtime_slot_read_test.go — the nested read of a container the program built (roadmap L11.1,
// ADR 0251).
//
// The compiled backend reads a slot out of a container in two ways. The first is a compile-time
// promise: the name was bound once to a container literal and nothing has touched the object since,
// so the tag the literal's builder wrote is still the tag the slot holds (ADR 0241). The second had
// to wait for the object to be asked:
//
//	xs = []
//	xs.append([7, 8])
//	print(xs[0][0])          # CPython 7, the record 7, the compiler: exit 1
//
// `xs` has no literal — the container was built by `append`, so the promise was never made, and the
// read one level below it had no answer. The answer is the same pair every other tagged context in
// this runtime consumes, and the object is the one that describes it: the tag written beside the
// outer slot says whether the payload names a list, a dict or a text, and the tag written beside the
// inner slot says what the value that comes back means. A slot that names neither raises the sentence
// CPython raises for that kind — `xs.append(5)` then `xs[0][0]` is `'int' object is not
// subscriptable`, on both legs, rather than a refusal or a number.
//
// Three tables: the shapes that now print CPython's answer, the failures that must be *raised* rather
// than refused, and the uses the door still declines with the missing half named. A slot that holds a
// set is a fourth case, and not a CPython one: subscripting a set is a documented gusty extension, so
// it gets its own table that pins both legs against *each other* (ADR 0186's third leg is why
// that has to be written down rather than assumed).

func TestRunTimeBuiltNestedSlotReadsMatchCPython(t *testing.T) {
	for _, tc := range []struct{ name, src, want string }{
		// The shape the roadmap names as L11.1's remaining clause.
		{"appended_list_read_below", "xs = []\nxs.append([7, 8])\nprint(xs[0][0])\n", "7\n"},
		{"appended_list_second_slot", "xs = []\nxs.append([7, 8])\nprint(xs[0][1])\n", "8\n"},
		{"second_appended_list", "xs = []\nxs.append([7, 8])\nxs.append([9, 10])\nprint(xs[1][0], xs[1][1])\n", "9 10\n"},
		{"negative_position_below_slot", "xs = []\nxs.append([7, 8])\nprint(xs[0][-1])\n", "8\n"},
		{"computed_position_below_slot", "xs = []\nxs.append([7, 8])\ni = 1\nprint(xs[0][i])\n", "8\n"},
		// The dict the program filled rather than wrote out.
		{"dict_key_to_list_slot", "d = {}\nd[\"a\"] = [1, 2]\nd[\"b\"] = [3, 4]\nprint(d[\"a\"][1], d[\"b\"][0])\n", "2 3\n"},
		{"computed_key_to_list_slot", "d = {}\nk = \"a\"\nd[k] = [1, 2]\nprint(d[k][0])\n", "1\n"},
		{"int_key_to_list_slot", "d = {}\nd[7] = [\"x\", \"y\"]\nprint(d[7][1])\n", "y\n"},
		// The tag, not the spelling, picks the arm. Two slots of one container answer two different
		// questions — the shape no static kind could have served.
		{"list_slot_then_dict_slot", "xs = []\nxs.append([1, 2])\nxs.append({\"k\": 5})\nprint(xs[0][1], xs[1][\"k\"])\n", "2 5\n"},
		{"dict_under_list_slot", "xs = []\nxs.append({\"k\": [1, 2]})\nprint(xs[0][\"k\"][1])\n", "2\n"},
		{"three_levels_under_append", "xs = []\nxs.append([[1, 2], [3, 4]])\nprint(xs[0][1][0])\n", "3\n"},
		{"text_slot_gives_a_character", "xs = []\nxs.append(\"abc\")\nprint(xs[0][1])\n", "b\n"},
		{"text_under_dict_key", "d = {}\nd[\"a\"] = \"abc\"\nprint(d[\"a\"][2])\n", "c\n"},
		{"float_slot_below", "xs = []\nxs.append([1.5, 2])\nprint(xs[0][0])\n", "1.5\n"},
		{"none_slot_below", "xs = []\nxs.append([None, 2])\nprint(xs[0][0])\n", "None\n"},
		// The uses a (payload, tag) pair is worth.
		{"bound_then_printed", "xs = []\nxs.append([7, 8])\ny = xs[0][1]\nprint(y)\n", "8\n"},
		{"bound_from_dict_slot", "d = {}\nd[\"a\"] = [7, 8]\ny = d[\"a\"][0]\nprint(y)\n", "7\n"},
		{"equal_to_its_number", "xs = []\nxs.append([7, 8])\nprint(1 if xs[0][1] == 8 else 0)\n", "1\n"},
		{"not_equal_to_the_other_number", "xs = []\nxs.append([7, 8])\nprint(1 if xs[0][1] == 7 else 0)\n", "0\n"},
		{
			// The intern-table collision ADR 0232 named: a text slot and the integer its index happens
			// to be are the same word and different values, and only the pair can tell them apart.
			"text_slot_is_not_its_interned_index",
			"xs = []\nxs.append([\"a\", 1])\nprint(1 if xs[0][0] == 1 else 0)\nprint(1 if xs[0][0] == \"a\" else 0)\n",
			"0\n1\n",
		},
		{"float_slot_equals_its_number", "xs = []\nxs.append([1.5])\nprint(1 if xs[0][0] == 1.5 else 0)\n", "1\n"},
		{"printed_as_itself", "xs = []\nxs.append([7, 8])\nprint(xs[0])\n", "[7, 8]\n"},
		{"length_of_the_slot_below", "xs = []\nxs.append([[1, 2]])\nprint(len(xs[0][0]))\n", "2\n"},
		{"length_of_a_text_slot_below", "xs = []\nxs.append([\"abc\"])\nprint(len(xs[0][0]))\n", "3\n"},
		// A comprehension is a container the program built as much as an appended one is.
		{"comp_built_dict_slot", "d = [{\"k\": x} for x in [1, 2]]\nprint(d[1][\"k\"])\n", "2\n"},
		{"comp_built_list_slot", "xs = [[x, x * 2] for x in [1, 2]]\nprint(xs[1][1])\n", "4\n"},
		// What the static door already answered, pinned so the new door cannot disturb it.
		{"literal_list_still_reads", "xs = [[1, 2], [3, 4]]\nprint(xs[0][1])\n", "2\n"},
		{"literal_dict_still_reads", "d = {\"a\": [1, 2]}\nprint(d[\"a\"][1])\n", "2\n"},
		{"literal_three_levels", "t = [[[1]]]\nprint(t[0][0][0])\n", "1\n"},
		// ---- Gap R.95: the membership test and the loop over the same slot. Both were refusals, and the
		// loop was worse than a refusal wherever the compiler had already written a kind for the container:
		// a dict whose value was a list walked the dict's own words and printed a single 0 for CPython's
		// 1 and 2, and the same shape holding a text printed nothing. Choosing the helper by the tag the
		// writer left beside the payload — the rule ADR 0251 gave the read — is the whole change.
		{"membership_in_a_built_container", "xs = []\nxs.append([7, 8])\nprint(1 if 7 in xs[0] else 0)\n", "1\n"},
		{"membership_absent_in_a_built_container", "xs = []\nxs.append([7, 8])\nprint(1 if 9 in xs[0] else 0)\n", "0\n"},
		{"iteration_of_a_built_container", "xs = []\nxs.append([7, 8])\nfor v in xs[0]:\n    print(v)\n", "7\n8\n"},
		{"loop_over_a_list_under_a_dict_key", "d = {}\nd[\"a\"] = [1, 2]\nfor v in d[\"a\"]:\n    print(v)\n", "1\n2\n"},
		{"loop_over_a_text_under_a_dict_key", "d = {}\nd[\"a\"] = \"abc\"\nfor v in d[\"a\"]:\n    print(v)\n", "a\nb\nc\n"},
		{"loop_over_a_dict_under_a_dict_key", "d = {}\nd[\"a\"] = {\"k\": 1, \"j\": 2}\nfor v in d[\"a\"]:\n    print(v)\n", "k\nj\n"},
		{"membership_of_a_list_under_a_dict_key", "d = {}\nd[\"a\"] = [1, 2]\nprint(1 in d[\"a\"], 3 in d[\"a\"])\n", "True False\n"},
		{
			"four_kinds_of_slot_in_one_container",
			"xs = []\nxs.append([7, 8])\nxs.append({\"k\": 1})\nxs.append({5})\nxs.append(\"abc\")\nprint(7 in xs[0], \"k\" in xs[1], 5 in xs[2], \"b\" in xs[3])\n",
			"True True True True\n",
		},
		{
			"the_same_four_slots_stepped_over",
			"xs = []\nxs.append([1, 2])\nxs.append({\"k\": 9})\nxs.append({5})\nxs.append(\"ab\")\nfor v in xs[0]:\n    print(v)\nfor v in xs[1]:\n    print(v)\nfor v in xs[2]:\n    print(v)\nfor v in xs[3]:\n    print(v)\n",
			"1\n2\nk\n5\na\nb\n",
		},
		{"a_dict_slot_is_tested_by_its_keys", "d = {}\nd[\"a\"] = {7: \"x\"}\nprint(7 in d[\"a\"], \"7\" in d[\"a\"])\n", "True False\n"},
		{"a_set_slot_is_tested_by_its_members", "xs = []\nxs.append({1, 2})\nprint(1 in xs[0], 3 in xs[0])\n", "True False\n"},
		{"a_text_slot_is_a_substring_test", "xs = []\nxs.append(\"abc\")\nprint(\"b\" in xs[0], \"z\" in xs[0])\n", "True False\n"},
		{"not_in_a_built_container", "xs = []\nxs.append([1, 2])\nprint(9 not in xs[0], 1 not in xs[0])\n", "True False\n"},
		{
			"a_loop_binds_containers_as_well_as_numbers",
			"xs = []\nxs.append([[1, 2], 3])\nfor v in xs[0]:\n    print(v)\n",
			"[1, 2]\n3\n",
		},
		{
			"a_loop_of_every_kind_of_slot",
			"xs = []\nxs.append([1, \"a\", 2.5, None, True])\nfor v in xs[0]:\n    print(v)\n",
			"1\na\n2.5\nNone\nTrue\n",
		},
		{
			"break_and_continue_in_a_loop_over_a_slot",
			"xs = []\nxs.append([1, 2, 3, 4])\nfor v in xs[0]:\n    if v == 2:\n        continue\n    if v == 3:\n        break\n    print(v)\n",
			"1\n",
		},
		{
			"the_else_arm_runs_when_the_loop_finishes",
			"xs = []\nxs.append([1, 2])\nfor v in xs[0]:\n    print(v)\nelse:\n    print(\"done\")\n",
			"1\n2\ndone\n",
		},
		{"a_computed_position_picks_the_haystack", "xs = []\nxs.append([7, 8])\ni = 0\nprint(7 in xs[i])\n", "True\n"},
		{"an_empty_container_steps_zero_times", "xs = []\nxs.append([])\nfor v in xs[0]:\n    print(v)\nprint(\"end\")\n", "end\n"},
		{"a_slot_read_as_the_needle", "xs = []\nxs.append([7, 8])\nys = []\nys.append(7)\nprint(ys[0] in xs[0])\n", "True\n"},
		{"a_pair_bound_name_as_the_needle", "xs = []\nxs.append([7, 8])\nys = []\nys.append([7, 8])\nn = ys[0][0]\nprint(n in xs[0])\n", "True\n"},
		{"a_container_as_the_needle", "xs = []\nxs.append([[1, 2], 3])\nprint([1, 2] in xs[0], [1, 3] in xs[0])\n", "True False\n"},
		// The two raise sentences are not variations of one another and the corpus keeps them apart on
		// purpose: membership names the type (`argument of type 'int' is not iterable`) and a loop names
		// the object (`'int' object is not iterable`). Here both raises are caught, which is the point —
		// a trap the program can name has to be reachble by the program's own `except` (ADR 0228).
		{"a_loop_and_a_test_over_the_same_slot_are_both_caught", "xs = []\nxs.append(5)\ntry:\n    for v in xs[0]:\n        print(v)\nexcept TypeError:\n    print(\"loop\")\ntry:\n    print(1 in xs[0])\nexcept TypeError:\n    print(\"test\")\n", "loop\ntest\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			path := writeSrc(t, t.TempDir(), "rt_read.gy", tc.src)
			if py, ok := cpythonOut(t, path); ok && py != tc.want {
				t.Fatalf("the expectation is not CPython's: got %q want %q", py, tc.want)
			}
			for _, engine := range cliEngines {
				out, code := cliRunCode(t, engine, path)
				if code == 2 {
					t.Fatalf("%s rejected the compiler's own module (ADR 0166 / exit-code contract):\n%s",
						engine, cliRun(t, engine, path))
				}
				if code != 0 {
					t.Fatalf("%s exited %d: %s", engine, code, cliRun(t, engine, path))
				}
				if out != tc.want {
					t.Fatalf("%s printed %q, want CPython's %q", engine, out, tc.want)
				}
			}
		})
	}
}

// TestRunTimeBuiltNestedSlotTrapsAreRaisedNotRefused pins the failures. A slot holding a number, None
// or a set has no slot below it; a position can be out of range; a key can be missing. CPython raises,
// and both gusty backends have to raise the same class and the same sentence — a refusal here
// (`codegen: …`) would be exit 1, the contract's "your program is wrong" class, issued for a program
// whose only crime is a wrong index, and a silent 0 would be worse.
func TestRunTimeBuiltNestedSlotTrapsAreRaisedNotRefused(t *testing.T) {
	for _, tc := range []struct {
		name, src, msg string
		// oracle is what CPython has to say. It is written separately where the two differ, which is
		// exactly one family: gusty says `index out of range`, CPython says `list index out of range`
		// (roadmap Gap R.90) — a wording debt, not a class debt, and the row pins both truths.
		oracle string
	}{
		{
			name: "number_slot_subscripted",
			src:  "xs = []\nxs.append([7, 8])\nxs.append(5)\nprint(xs[1][0])\n",
			msg:  "TypeError: 'int' object is not subscriptable",
		},
		{
			name: "float_slot_subscripted",
			src:  "xs = []\nxs.append([7, 8])\nxs.append(1.5)\nprint(xs[1][0])\n",
			msg:  "TypeError: 'float' object is not subscriptable",
		},
		{
			name: "none_slot_subscripted",
			src:  "xs = []\nxs.append([7, 8])\nxs.append(None)\nprint(xs[1][0])\n",
			msg:  "TypeError: 'NoneType' object is not subscriptable",
		},
		{
			name: "position_below_the_slot_is_out_of_range",
			src:  "xs = []\nxs.append([7, 8])\nprint(xs[0][9])\n",
			msg:  "IndexError: index out of range",
			// gusty's own wording; the class and the raise are what the row is about (Gap R.90).
			oracle: "IndexError",
		},
		{
			name: "negative_position_below_the_slot_is_out_of_range",
			src:  "xs = []\nxs.append([7, 8])\nprint(xs[0][-9])\n",
			msg:  "IndexError: index out of range",
			// gusty's own wording; the class and the raise are what the row is about (Gap R.90).
			oracle: "IndexError",
		},
		{
			name: "missing_key_below_the_slot",
			src:  "d = {}\nd[\"a\"] = [1, 2]\nprint(d[\"z\"][0])\n",
			msg:  "KeyError",
		},
		// ---- Gap R.95's failures: a slot holding something with nothing to walk. Each of these used to
		// be answered at the exit code of success — the membership test read the payload as a heap handle
		// and said False, the loop stepped over nothing — which is the class of bug the exit-code contract
		// counts as ours rather than the program's.
		{
			name: "membership_of_an_int_slot",
			src:  "xs = []\nxs.append(5)\nprint(1 in xs[0])\n",
			msg:  "TypeError: argument of type 'int' is not iterable",
		},
		{
			name: "membership_of_a_none_slot",
			src:  "xs = []\nxs.append(None)\nprint(1 in xs[0])\n",
			msg:  "TypeError: argument of type 'NoneType' is not iterable",
		},
		{
			name: "membership_of_a_verdict_slot",
			src:  "xs = []\nxs.append(True)\nprint(1 in xs[0])\n",
			msg:  "TypeError: argument of type 'bool' is not iterable",
		},
		{
			name: "membership_of_a_number_stored_by_assignment",
			src:  "d = {}\nd[\"a\"] = 5\nprint(1 in d[\"a\"])\n",
			msg:  "TypeError: argument of type 'int' is not iterable",
		},
		{
			name: "a_text_haystack_rejects_a_number_needle",
			src:  "xs = []\nxs.append(\"abc\")\nprint(1 in xs[0])\n",
			msg:  "TypeError: 'in <string>' requires string as left operand, not int",
		},
		{
			name: "a_loop_over_an_int_slot",
			src:  "xs = []\nxs.append(5)\nfor v in xs[0]:\n    print(v)\n",
			msg:  "TypeError: 'int' object is not iterable",
		},
		{
			name: "a_loop_over_a_none_slot",
			src:  "xs = []\nxs.append(None)\nfor v in xs[0]:\n    print(v)\n",
			msg:  "TypeError: 'NoneType' object is not iterable",
		},
		{
			name: "a_loop_over_a_float_stored_by_assignment",
			src:  "d = {}\nd[\"a\"] = 1.5\nfor v in d[\"a\"]:\n    print(v)\n",
			msg:  "TypeError: 'float' object is not iterable",
		},
		{
			name: "character_position_out_of_range",
			src:  "xs = []\nxs.append(\"ab\")\nprint(xs[0][5])\n",
			msg:  "IndexError: string index out of range",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			path := writeSrc(t, t.TempDir(), "rt_trap.gy", tc.src)
			want, pyCode := oracleTrap(t, path)
			oracleWant := tc.msg
			if tc.oracle != "" {
				oracleWant = tc.oracle
			}
			if !strings.Contains(want, oracleWant) {
				t.Fatalf("the oracle does not say %q, it says %q", oracleWant, want)
			}
			if pyCode == 0 {
				t.Fatalf("the oracle exited 0 on a program meant to trap:\n%s", want)
			}
			for _, engine := range cliEngines {
				combined := cliRun(t, engine, path)
				_, code := cliRunCode(t, engine, path)
				if code == 2 {
					t.Errorf("%s exited 2 (a compiler bug, ADR 0166) on a trapping program:\n%s", engine, combined)
				}
				if code == 0 {
					t.Errorf("%s exited 0 on a program the oracle dies on:\n%s", engine, combined)
				}
				if code != 3 {
					t.Errorf("%s exited %d where a trap is the answer (ADR 0166): %s", engine, code, combined)
				}
				if !strings.Contains(combined, "Traceback (most recent call last):") {
					t.Errorf("%s printed no traceback:\n%s", engine, combined)
				}
				if !strings.Contains(combined, tc.msg) {
					t.Errorf("%s did not raise %q:\n%s", engine, tc.msg, combined)
				}
				if strings.Contains(combined, "codegen:") {
					t.Errorf("%s refused what the oracle traps on:\n%s", engine, combined)
				}
			}
		})
	}
}

// TestRunTimeBuiltNestedSlotRefusalsNameTheMissingHalf is the debt, kept visible. The *read* of a slot
// only the run time can describe is answered; so is the membership test and the loop over that same
// slot (Gap R.95, whose two rows this table used to hold — they are parity rows above now). What the
// door still declines is a haystack two slots deep: reaching it would mean dispatching on a tag that is
// itself behind a dispatch, and each row says which half is missing rather than answering with a
// verdict. CPython's answer is written into the roadmap row beside each.
func TestRunTimeBuiltNestedSlotRefusalsNameTheMissingHalf(t *testing.T) {
	for _, tc := range []struct{ name, src, want string }{
		// The **number** use of the same slot, and its negation, were in this table until ADR 0265 answered
		// them: the pair (payload, tag) goes to the target, which does the sum and answers a pair back, so
		// the answer brings its own kind. They live in numeric_slot_arith_test.go now, together with the
		// gate that decides which programs may take the road at all.
		// The *ordering* of the same slot used to be in this table; ADR 0252 answers it, and the rows
		// moved to TestSlotOrderOfAnUnliteralisedSlotMatchesCPython in slot_order_object_test.go.
		// The membership test and the loop used to be the two rows here; Gap R.95 paid them, and they are
		// parity rows in TestRunTimeBuiltNestedSlotReadsMatchCPython above.
		{"membership_two_slots_deep", "xs = []\nxs.append([[7, 8]])\nprint(1 if 7 in xs[0][0] else 0)\n", "index cannot reach into xs's slots"},
		{"iteration_two_slots_deep", "xs = []\nxs.append([[7, 8]])\nfor v in xs[0][0]:\n    print(v)\n", "index cannot reach into xs's slots"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			path := writeSrc(t, t.TempDir(), "rt_refuse.gy", tc.src)
			if _, pyCode := oracleTrap(t, path); pyCode != 0 {
				t.Fatalf("the oracle does not run this program; the row claims a refusal, not a trap")
			}
			_, code := cliRunCode(t, "--aot", path)
			if code != 1 {
				t.Fatalf("--aot exited %d, want the front-end refusal class 1: %s", code, cliRun(t, "--aot", path))
			}
			combined := cliRun(t, "--aot", path)
			if !strings.Contains(combined, tc.want) {
				t.Errorf("--aot refused without naming %q: %s", tc.want, combined)
			}
			if strings.Contains(combined, "LLVM ERROR") || strings.Contains(combined, "verifier") {
				t.Errorf("the refusal was an IR problem rather than a front-end one: %s", combined)
			}
			// And the same program, without the refusal this row is about, must still be a program the
			// compiled path can run — answering what the reference answers, or refusing with the half it
			// is missing named (the shapes here are the ones whose slot kind is a run-time fact).
			compiledOut, icode := cliRunCode(t, "--aot", path)
			py, ok := cpythonOut(t, path)
			if !ok {
				t.Fatalf("the reference failed to answer this program: %s", path)
			}
			checkCompiledRow(t, compiledOut, icode, tc.src, py)
		})
	}
}

// TestSetSlotSubscriptIsTheDocumentedExtensionBothWays keeps a gusty-only surface honest. Subscripting
// a set asks the set a membership question — `docs/language.md § Dicts & sets` says so, the corpus rows
// that use it are `oracle: not_applicable` because CPython rejects the shape, and ADR 0186's third leg
// is exactly why that has to be *written down* rather than assumed. Reading a set slot one level down
// is the same question, so it gets the same answer and the same KeyError when the member is absent;
// both legs may diverge from CPython here, but never from each other.
func TestSetSlotSubscriptIsTheDocumentedExtensionBothWays(t *testing.T) {
	for _, tc := range []struct{ name, src, want string }{
		{"set_slot_answers_its_member", "xs = []\nxs.append({5, 6, 7})\nprint(xs[0][6])\n", "6\n"},
		{"set_slot_under_a_dict_key", "d = {}\nd[\"a\"] = {5, 6, 7}\nprint(d[\"a\"][7])\n", "7\n"},
		{"set_literal_answers_its_member", "print({5, 6, 7}[6])\n", "6\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			path := writeSrc(t, t.TempDir(), "rt_set.gy", tc.src)
			_, pyCode := oracleTrap(t, path)
			if pyCode == 0 {
				t.Fatalf("the oracle runs this program, so it belongs in the parity table")
			}
			outs := map[string]string{}
			for _, engine := range cliEngines {
				out, code := cliRunCode(t, engine, path)
				if code != 0 {
					t.Fatalf("%s exited %d: %s", engine, code, cliRun(t, engine, path))
				}
				if out != tc.want {
					t.Errorf("%s printed %q, want the extension's %q", engine, out, tc.want)
				}
				outs[engine] = out
			}
			if outs["--aot"] != outs["--aot"] {
				t.Errorf("the two backends disagree about a gusty-only surface: %q vs %q", outs["--aot"], outs["--aot"])
			}
		})
	}

	for _, tc := range []struct{ name, src, msg string }{
		{
			"set_slot_names_no_member",
			"xs = []\nxs.append({5, 6, 7})\nprint(xs[0][9])\n",
			"KeyError: not in set",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			path := writeSrc(t, t.TempDir(), "rt_set_trap.gy", tc.src)
			for _, engine := range cliEngines {
				combined := cliRun(t, engine, path)
				_, code := cliRunCode(t, engine, path)
				if code != 3 {
					t.Errorf("%s exited %d, want the trap class 3: %s", engine, code, combined)
				}
				if !strings.Contains(combined, tc.msg) {
					t.Errorf("%s did not raise %q: %s", engine, tc.msg, combined)
				}
			}
		})
	}
}

// TestASlotHoldingAnInstanceNamesTheWrongKindForMembershipAndLoop is filed, measured and not fixed.
// Asking a slot for its contents when the slot holds an instance of a user class reaches the arm for a
// value with nothing to walk, and the arm reports the kind the *slot* says — `int` — where the reference
// reports the class. The arm is right and the tag is wrong: `xs.append(C())` stores the instance's heap
// handle beside a tag of TagInt, so every question the tag answers in this program answers about a kind
// the program never put there. It is the same missing word Gap R.112 found for a bool in a slot, three
// levels up from where anyone had looked for it.
//
// The row is pinned rather than fixed here because the fix is the tag the *writer* leaves, not the door
// that reads it, and a door that invented the class name would be guessing at an object it cannot see.
func TestASlotHoldingAnInstanceNamesTheWrongKindForMembershipAndLoop(t *testing.T) {
	for _, tc := range []struct{ name, src, compiled, reference string }{
		{
			"membership of an instance slot",
			"class C:\n    pass\nxs = []\nxs.append(C())\nprint(1 in xs[0])\n",
			"TypeError: argument of type 'int' is not iterable",
			"TypeError: argument of type 'C' is not iterable",
		},
		{
			"a loop over an instance slot",
			"class C:\n    pass\nxs = []\nxs.append(C())\nfor v in xs[0]:\n    print(v)\n",
			"TypeError: 'int' object is not iterable",
			"TypeError: 'C' object is not iterable",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			path := writeSrc(t, t.TempDir(), "rt_inst.gy", tc.src)
			want, pyCode := oracleTrap(t, path)
			if !strings.Contains(want, tc.reference) {
				t.Fatalf("the reference does not say %q, it says %q", tc.reference, want)
			}
			if pyCode == 0 {
				t.Fatalf("the reference exited 0 on a program meant to trap")
			}
			combined := cliRun(t, "--aot", path)
			_, code := cliRunCode(t, "--aot", path)
			if code == 2 {
				t.Errorf("--aot exited 2 (a compiler bug, ADR 0166):\n%s", combined)
			}
			if code != 3 {
				t.Errorf("--aot exited %d where the reference's trap class 3 is the answer: %s", code, combined)
			}
			if !strings.Contains(combined, tc.compiled) {
				t.Errorf("--aot raised %q, want the tag's own answer %q (the class name is the filed half)", combined, tc.compiled)
			}
			if strings.Contains(combined, "codegen:") {
				t.Errorf("--aot refused what the reference traps on:\n%s", combined)
			}
		})
	}
}
