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
//	print(xs[0][0])          # CPython 7, the interpreter 7, the compiler: exit 1
//
// `xs` has no literal — the container was built by `append`, so the promise was never made, and the
// read one level below it had no answer. The answer is the same pair every other tagged context in
// this runtime consumes, and the object is the one that describes it: the tag written beside the
// outer slot says whether the payload names a list, a dict or a text, and the tag written beside the
// inner slot says what the value that comes back means. A slot that names neither raises the sentence
// CPython raises for that kind — `xs.append(5)` then `xs[0][0]` is `'int' object is not
// subscriptable`, on all three engines, rather than a refusal or a number.
//
// Three tables: the shapes that now print CPython's answer, the failures that must be *raised* rather
// than refused, and the uses the door still declines with the missing half named. A slot that holds a
// set is a fourth case, and not a CPython one: subscripting a set is a documented gusty extension, so
// it gets its own table that pins the two engines against *each other* (ADR 0186's third leg is why
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
	} {
		t.Run(tc.name, func(t *testing.T) {
			path := writeSrc(t, t.TempDir(), "rt_read.gy", tc.src)
			if py, ok := cpythonOut(t, path); ok && py != tc.want {
				t.Fatalf("the expectation is not CPython's: got %q want %q", py, tc.want)
			}
			for _, engine := range []string{"--interp", "--aot"} {
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
			for _, engine := range []string{"--interp", "--aot"} {
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
// only the run time can describe is answered; the *number*, the *ordering*, the membership test and
// the loop over that same slot are not, and each says which half is missing rather than answering with
// a verdict. CPython's answer is written into the roadmap row beside each.
func TestRunTimeBuiltNestedSlotRefusalsNameTheMissingHalf(t *testing.T) {
	for _, tc := range []struct{ name, src, want string }{
		{"number_use_of_an_unseen_slot", "xs = []\nxs.append([7, 8])\nprint(xs[0][0] + 1)\n", "cannot reach into xs's slots"},
		{"negation_of_an_unseen_slot", "xs = []\nxs.append([7, 8])\nprint(-xs[0][0])\n", "cannot reach into xs's slots"},
		{"ordering_of_an_unseen_slot", "xs = []\nxs.append([7, 8])\nprint(1 if xs[0][0] > 1 else 0)\n", "cannot reach into xs's slots"},
		{"membership_in_a_built_container", "xs = []\nxs.append([7, 8])\nprint(1 if 7 in xs[0] else 0)\n", "needs a single static kind"},
		{"iteration_of_a_built_container", "xs = []\nxs.append([7, 8])\nfor v in xs[0]:\n    print(v)\n", "needs a single static kind"},
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
			// The other legs: what the refusal owes, the interpreter already answers.
			interpOut, icode := cliRunCode(t, "--interp", path)
			if icode != 0 {
				t.Errorf("--interp exited %d on a program the oracle runs: %s", icode, interpOut)
			}
			if py, ok := cpythonOut(t, path); ok && interpOut != py {
				t.Errorf("--interp printed %q, want CPython's %q", interpOut, py)
			}
		})
	}
}

// TestSetSlotSubscriptIsTheDocumentedExtensionBothWays keeps a gusty-only surface honest. Subscripting
// a set asks the set a membership question — `docs/language.md § Dicts & sets` says so, the corpus rows
// that use it are `oracle: not_applicable` because CPython rejects the shape, and ADR 0186's third leg
// is exactly why that has to be *written down* rather than assumed. Reading a set slot one level down
// is the same question, so it gets the same answer and the same KeyError when the member is absent;
// the two engines may diverge from CPython here, but never from each other.
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
			for _, engine := range []string{"--interp", "--aot"} {
				out, code := cliRunCode(t, engine, path)
				if code != 0 {
					t.Fatalf("%s exited %d: %s", engine, code, cliRun(t, engine, path))
				}
				if out != tc.want {
					t.Errorf("%s printed %q, want the extension's %q", engine, out, tc.want)
				}
				outs[engine] = out
			}
			if outs["--interp"] != outs["--aot"] {
				t.Errorf("the two backends disagree about a gusty-only surface: %q vs %q", outs["--interp"], outs["--aot"])
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
			for _, engine := range []string{"--interp", "--aot"} {
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
