package lang

import (
	"strings"
	"testing"
)

// dict_none_bool_key_test.go — a dict read asked with a `None`, `True` or `False` key is answered by
// the pair door, roadmap Gap R.221 (ADR 0353).
//
// `d = {None: 3.5}` / `print(d[None])` printed `3` — the handle of the `@float_box` object — and
// `d = {None: "hi"}` printed `0`, the text's position in `@str_tab`. Neither raised: the entry is
// reached, the payload comes back, and the tag that says what the payload *is* stays in the slot,
// because the read took the one-word road. That road was chosen by a list of key spellings kept beside
// the read door, and the list named a name, an int, a text and a float — but not a `None` or a bool,
// the three keys whose payload is 0 or 1 and whose tag the runtime's table has always had.
//
// The rule this file exists to keep honest is the one every wrong answer in this family broke: a read
// of a dict whose slots describe themselves carries its tag with its payload, and a road that cannot
// carry the pair declines rather than prints the payload. The print door, the binding door and the
// `and`/`or` doors must not each keep their own list of keys — so the list moved out to
// `dictKeyCarriesPair` (`pkg/lang/dict_key_pair.go`), which is asked as a table below in both directions.

// dictSelfDescribingValues are the values a self-describing dict can hold: each is stored with a tag
// the compiler does not have to guess, and each prints differently from its bare payload — which is
// what makes these the right cross-product for a read that must carry the pair.
var dictSelfDescribingValues = []struct{ value, want string }{
	{"3.5", "3.5\n"}, // the payload is a @float_box handle; %d of it is the slot number
	{"0.0", "0.0\n"}, // the value that printed 0 for a whole class of reads
	{"3", "3\n"},     // the one kind whose payload IS the value
	{`"hi"`, "hi\n"}, // the payload is an @str_tab index
	{"None", "None\n"},
	{"True", "True\n"}, // payload 1, like the number 1 — the tag is not decoration
	{"False", "False\n"},
	{"[2]", "[2]\n"},
	{"[1, 2]", "[1, 2]\n"},
	{"[]", "[]\n"},
	{`{"k": 1}`, "{'k': 1}\n"},
}

// TestADictEntryReadByABooleanOrNoneKeyAnswersWhatTheReferencePrints is the row's answering half.
// Every `want` was read off CPython 3.12 before the case was written; on the commit before ADR 0353
// all thirty-three of these printed a payload at exit 0.
func TestADictEntryReadByABooleanOrNoneKeyAnswersWhatTheReferencePrints(t *testing.T) {
	t.Parallel()
	for _, kc := range []struct{ key, name string }{{"None", "none"}, {"True", "true"}, {"False", "false"}} {
		for _, vk := range dictSelfDescribingValues {
			t.Run(kc.name+"_key/"+vk.value, func(t *testing.T) {
				t.Parallel()
				src := "d = {" + kc.key + ": " + vk.value + "}\nprint(d[" + kc.key + "])\n"
				got := compiledOut(t, src)
				if got != vk.want {
					t.Errorf("%s printed %q, want %q — the payload reached the printer without its tag (roadmap Gap R.221)", src, got, vk.want)
				}
			})
		}
	}
}

// The positions the pair door answers, crossed with the three key spellings: a read is a value, so it
// is bound, measured, tested, looped over, rendered by `str`/`repr` and compared with `==` — and the
// direct form and the bound form must answer alike, because a binding is the same read with a name in
// front of it (ADR 0267's rule, asked of a key for the first time here).
func TestABooleanOrNoneKeyReadIsAValueInEveryPositionTheDoorAnswers(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct{ name, src, want string }{
		{"bound_then_printed", "d = {None: 3.5}\nx = d[None]\nprint(x)\n", "3.5\n"},
		{"bound_text_then_printed", "d = {None: \"hi\"}\nx = d[None]\nprint(x)\n", "hi\n"},
		{"bound_container_then_printed", "d = {True: [1, 2]}\nx = d[True]\nprint(x)\n", "[1, 2]\n"},
		{"twice_in_one_line", "d = {False: 3.5}\nprint(d[False], d[False])\n", "3.5 3.5\n"},
		{"three_keys_one_dict", "d = {None: 1.5, True: \"a\", False: [2]}\nprint(d[None], d[True], d[False])\n", "1.5 a [2]\n"},
		{"measured", "d = {None: [1, 2]}\nprint(len(d[None]))\n", "2\n"},
		{"looped_over", "d = {True: [1, 2]}\nfor v in d[True]:\n    print(v)\n", "1\n2\n"},
		{"membership_in_a_read", "d = {False: [1, 2]}\nprint(1 in d[False])\n", "True\n"},
		{"rendered_by_str_and_repr", "d = {None: \"hi\"}\nprint(str(d[None]), repr(d[None]))\n", "hi 'hi'\n"},
		{"compared_with_equals", "d = {None: 3.5}\nprint(d[None] == 3.5)\n", "True\n"},
		{"container_compared_with_equals", "d = {True: [1, 2]}\nprint(d[True] == [1, 2])\n", "True\n"},
		{"number_arith", "d = {True: 3}\nprint(d[True] + 1)\n", "4\n"},
		{"float_arith", "d = {None: 3.5}\nprint(d[None] + 1)\n", "4.5\n"},
		{"nested_in_a_list", "d = {None: 3.5}\nprint([d[None], 1])\n", "[3.5, 1]\n"},
		{"key_membership", "d = {None: [1, 2]}\nprint(None in d)\n", "True\n"},
		{"or_answers_the_entry", "d = {None: \"hi\"}\nprint(d[None] or \"z\")\n", "hi\n"},
		{"or_skips_a_falsy_entry", "d = {True: 0}\nprint(d[True] or \"z\")\n", "z\n"},
		{"and_answers_the_entry", "d = {False: [1, 2]}\nprint(d[False] and 7)\n", "7\n"},
		// A bool key and an int key are the same key, because the reference says so — and which entry
		// they find is the object's answer, not a pick made by comparing payloads (ADR 0328's door).
		{"zero_key_read_by_false", "d = {0: \"zero\"}\nprint(d[False])\n", "zero\n"},
		{"one_key_read_by_true", "d = {1: \"one\"}\nprint(d[True])\n", "one\n"},
		{"false_key_read_by_zero", "d = {False: 2.5}\nprint(d[0])\n", "2.5\n"},
		{"later_write_of_one_key_wins", "d = {True: 1.5, 1: \"one\"}\nprint(d[True])\n", "one\n"},
		{"run_built_dict_none_key", "d = {}\nd[None] = 3.5\nprint(d[None])\n", "3.5\n"},
		{"run_built_dict_true_key", "d = {}\nd[True] = \"hi\"\nprint(d[True])\n", "hi\n"},
		{"run_built_dict_false_key", "d = {}\nd[False] = [1, 2]\nprint(d[False])\n", "[1, 2]\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := compiledOut(t, tc.src); got != tc.want {
				t.Errorf("%s printed %q, want %q", tc.src, got, tc.want)
			}
		})
	}
}

// A key that names no entry is still the reference's trap — class, message and catchability — and not a
// compiler decision about which entry was "close enough". The exit code for a trap is 3 here and 1 in
// CPython, so what the two legs agree on is the sentence.
func TestABooleanOrNoneKeyThatNamesNoEntryRaisesWhatTheReferenceRaises(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct{ name, src, want string }{
		{"none_key_on_a_bool_keyed_dict", "d = {True: 3.5}\nprint(d[None])\n", "KeyError: None"},
		{"true_key_on_a_none_keyed_dict", "d = {None: 3.5}\nprint(d[True])\n", "KeyError: True"},
		{"text_key_on_a_none_keyed_dict", "d = {None: 3.5}\nprint(d[\"nope\"])\n", "KeyError: 'nope'"},
		{
			name: "catchable_by_the_programs_own_except",
			src:  "d = {None: 3.5}\ntry:\n    print(d[True])\nexcept KeyError:\n    print(\"caught\")\n",
			want: "caught\n",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			res, err := JIT(tc.src, 0)
			if err != nil {
				t.Fatalf("the compiled leg refused a program the reference runs (%v): %s", err, tc.src)
			}
			if strings.Contains(res.Stderr, "codegen:") {
				t.Fatalf("%s was refused rather than run: %s", tc.src, res.Stderr)
			}
			if strings.HasPrefix(tc.want, "KeyError") {
				if res.Code != 3 {
					t.Fatalf("%s exited %d where a trap is the answer (ADR 0166); stdout %q stderr %q", tc.src, res.Code, res.Output, res.Stderr)
				}
				if !strings.Contains(res.Stderr, tc.want) {
					t.Errorf("%s raised %q, the reference raises %q", tc.src, strings.TrimSpace(res.Stderr), tc.want)
				}
				return
			}
			if res.Code != 0 || res.Output != tc.want {
				t.Errorf("%s printed %q (exit %d), want %q — stderr %q", tc.src, res.Output, res.Code, tc.want, res.Stderr)
			}
		})
	}
}

// TestTheDictKeyGateAnswersItsOwnTableInBothDirections asks the gate as a function, which is the only
// way to pin that it decides: a door tested only through programs that already reach it proves nothing
// about the keys it turns away (the rule ADR 0351's slot gate follows).
func TestTheDictKeyGateAnswersItsOwnTableInBothDirections(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name string
		key  Expr
		want bool
	}{
		{"none", &NoneLit{}, true},
		{"true", &BoolLit{Value: true}, true},
		{"false", &BoolLit{Value: false}, true},
		{"int", &IntLit{Value: 3}, true},
		{"text", &StrLit{Value: "a"}, true},
		{"float", &FloatLit{Value: 1.5}, true},
		{"name", &Name{Value: "k"}, true},
		// A key with no tag has no arm: the object cannot be asked to hash it and the compiler cannot
		// name its kind, which is the pair of facts ADR 0328's gate and roadmap Gap R.212 both refuse on.
		{"list", &ListLit{}, false},
		{"dict", &DictLit{}, false},
		{"set", &SetLit{}, false},
		{"tuple", &Tuple{}, false},
		{"call", &Call{Fn: &Name{Value: "f"}}, false},
		{"conditional", &CondExpr{If: &BoolLit{Value: true}}, false},
		{"nothing", nil, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := dictKeyCarriesPair(tc.key); got != tc.want {
				t.Errorf("dictKeyCarriesPair(%s) = %v, want %v", tc.name, got, tc.want)
			}
		})
	}
}

// The module is checked the way ADR 0327 checked the literal read: the read is the pair call, the print
// is the printer that reads a tag, and the numeric formatter that wrote every wrong answer in this
// family is absent from the program's own body.
func TestABooleanOrNoneKeyReadIsPrintedByThePrinterThatReadsATag(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct{ name, src string }{
		{"float_entry", "d = {None: 3.5}\nprint(d[None])\n"},
		{"text_entry", "d = {None: \"hi\"}\nprint(d[None])\n"},
		{"container_entry", "d = {True: [1, 2]}\nprint(d[True])\n"},
		{"none_entry", "d = {False: None}\nprint(d[False])\n"},
		{"bool_entry", "d = {False: True}\nprint(d[False])\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			body := mainRegion(t, tc.src)
			for _, helper := range []string{"@rt_dict_get_tagged", "@rt_dict_value_tag", "@rt_print_mixed_value"} {
				if !strings.Contains(body, helper) {
					t.Errorf("%s: the read did not go through the pair door (no %s in the program's own body):\n%s", tc.name, helper, body)
				}
			}
			if strings.Contains(body, "%d") {
				t.Errorf("%s: the entry was written by a numeric format:\n%s", tc.name, body)
			}
		})
	}
}

// …and the negative control that makes the row above a measurement rather than an always-true claim: a
// dict whose values all share one kind is not self-describing, keeps its ordinary read, and must not
// have grown the value-tag door because the gate learned new key spellings.
func TestASingleKindDictKeepsItsOwnReadRoad(t *testing.T) {
	t.Parallel()
	for _, src := range []string{
		"d = {1: 2}\nprint(d[1])\n",
		"d = {1: 2, 2: 3}\nprint(d[2])\n",
	} {
		body := mainRegion(t, src)
		if strings.Contains(body, "@rt_dict_value_tag") {
			t.Errorf("%s reached the value-tag door though its values share one kind:\n%s", src, body)
		}
	}
}

// The record leg: the same sources, asked of the compiled backend and checked against the answer on
// record. These are sources the retired engine never answered either — a read that prints a payload is
// a wrong answer, not a missing one — so every entry here is a post-ADR-0302 record, written from the
// reference and cross-checked against the compiled leg by tools/recmerge.
func TestTheRecordLegAnswersTheNoneKeyReadsTheSameProgram(t *testing.T) {
	t.Parallel()
	for _, src := range []string{
		"d = {None: 3.5}\nprint(d[None])\n",
		"d = {None: \"hi\"}\nprint(d[None])\n",
		"d = {None: [1, 2]}\nprint(d[None])\n",
		"d = {True: 3.5}\nprint(d[True])\n",
		"d = {False: [2]}\nprint(d[False])\n",
		"d = {None: None}\nprint(d[None])\n",
		"d = {None: True}\nprint(d[None])\n",
		"d = {None: 1.5, True: \"a\", False: [2]}\nprint(d[None], d[True], d[False])\n",
		"d = {}\nd[None] = 3.5\nprint(d[None])\n",
		"d = {None: 3.5}\nx = d[None]\nprint(x)\n",
	} {
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

// The ladder this row promises and has to keep: for a position the pair door answers, the `None`-key
// spelling answers what the reference prints — every one of these was read off CPython first, and the
// compiled leg answers all of them, the two traps among them (`len` of a float entry, a loop over one).
// Where the *int*-key twin now disagrees, the disagreement is the twin's own defect and has its own row
// — the loop road reading a non-iterable as a repeat count (Gap R.246), the loop road losing a run-time
// text (Gap R.16), the equality door declining an operand whose kind it cannot see (Gap R.83). A row is
// never closed by making its neighbour look better, so the twins are named rather than smoothed over.
func TestTheNoneKeyRoadAnswersWhatTheReferencePrintsInEveryDoorPosition(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name  string
		entry string // the dict's value, spelled the way the source does
		spell string // the shape, with K standing for the key
		want  string // CPython's stdout, "" where it raises
		trap  string // CPython's class-and-message, "" where it answers
		twin  string // the row that owns the int-key twin, where that road disagrees
	}{
		{"list/printed", "[1, 2]", "print(d[K])", "[1, 2]\n", "", ""},
		{"list/bound", "[1, 2]", "x = d[K]\nprint(x)", "[1, 2]\n", "", ""},
		{"list/measured", "[1, 2]", "print(len(d[K]))", "2\n", "", ""},
		{"list/looped", "[1, 2]", "for v in d[K]:\n    print(v)", "1\n2\n", "", ""},
		{"list/rendered", "[1, 2]", "print(str(d[K]))", "[1, 2]\n", "", ""},
		{"list/fstring_field", "[1, 2]", "print(f\"{d[K]}\")", "[1, 2]\n", "", ""},
		{"list/fstring_repr", "[1, 2]", "print(f\"{d[K]!r}\")", "[1, 2]\n", "", ""},
		{"list/compared", "[1, 2]", "print(d[K] == [1, 2])", "True\n", "", ""},
		{"list/in_a_list", "[1, 2]", "print([d[K], 1])", "[[1, 2], 1]\n", "", ""},
		{"float/printed", "3.5", "print(d[K])", "3.5\n", "", ""},
		{"float/bound", "3.5", "x = d[K]\nprint(x)", "3.5\n", "", ""},
		{"float/rendered", "3.5", "print(str(d[K]))", "3.5\n", "", ""},
		{"float/fstring_field", "3.5", "print(f\"{d[K]}\")", "3.5\n", "", ""},
		{"float/fstring_repr", "3.5", "print(f\"{d[K]!r}\")", "3.5\n", "", ""},
		{"float/compared", "3.5", "print(d[K] == [1, 2])", "False\n", "", ""},
		{"float/in_a_list", "3.5", "print([d[K], 1])", "[3.5, 1]\n", "", ""},
		{"float/measured", "3.5", "print(len(d[K]))", "", "TypeError: object of type 'float' has no len()", ""},
		{"float/looped", "3.5", "for v in d[K]:\n    print(v)", "", "TypeError: 'float' object is not iterable", "Gap R.246"},
		{"text/printed", `"hi"`, "print(d[K])", "hi\n", "", ""},
		{"text/bound", `"hi"`, "x = d[K]\nprint(x)", "hi\n", "", ""},
		{"text/measured", `"hi"`, "print(len(d[K]))", "2\n", "", ""},
		{"text/looped", `"hi"`, "for v in d[K]:\n    print(v)", "h\ni\n", "", "Gap R.16"},
		{"text/rendered", `"hi"`, "print(str(d[K]))", "hi\n", "", ""},
		{"text/fstring_field", `"hi"`, "print(f\"{d[K]}\")", "hi\n", "", ""},
		{"text/fstring_repr", `"hi"`, "print(f\"{d[K]!r}\")", "'hi'\n", "", ""},
		{"text/compared", `"hi"`, "print(d[K] == [1, 2])", "False\n", "", "Gap R.83"},
		{"text/in_a_list", `"hi"`, "print([d[K], 1])", "['hi', 1]\n", "", ""},
		// The operator roads, which are this dict's own defect (roadmap Gap R.285, ADR 0419): every one of
		// them asked the dict what kind its VALUES were, a mixed dict records no such kind, and so each road
		// chose the number's word for a text. The void-keyed column printed `0` for the repeat (the interned
		// index, doubled) and refused the concatenation, while the int-keyed column refused both, and CPython
		// answered all of it. The pins those two answers used to live in are DELETED below.
		{"text/concatenated", `"a"`, `print(d[K] + "b")`, "ab\n", "", ""},
		{"text/repeated", `"a"`, `print(d[K] * 2)`, "aa\n", "", ""},
		{"text/repeated_by_a_name", `"a"`, "n = 2\nprint(d[K] * n)", "aa\n", "", ""},
		{"text/repeated_the_other_way", `"a"`, `print(2 * d[K])`, "aa\n", "", ""},
		{"text/upper", `"a"`, `print(d[K].upper())`, "A\n", "", ""},
		{"text/interpolated", `"a"`, `print(f"x{d[K]}y")`, "xay\n", "", ""},
		{"text/length", `"abc"`, `print(len(d[K]) == 3)`, "True\n", "", ""},
		{"text/equality", `"abc"`, `print(d[K] == "abc")`, "True\n", "", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			noneKey := "d = {None: " + tc.entry + "}\n" + strings.Replace(tc.spell, "K", "None", 1) + "\n"
			intKey := "d = {1: " + tc.entry + "}\n" + strings.Replace(tc.spell, "K", "1", 1) + "\n"
			var answer string
			if tc.trap != "" {
				expectTrap(t, noneKey, tc.trap)
			} else {
				answer = compiledOut(t, noneKey)
				if answer != tc.want {
					t.Errorf("printed %q, the reference prints %q", answer, tc.want)
				}
			}
			intOut, intRefused, intExit := programOutcome(t, intKey)
			if tc.trap != "" {
				// The entry is not iterable / has no length: both spellings owe the reference's trap, and a
				// twin that answers instead is Gap R.246's row (a loop that asks no question of its iterable).
				if !intRefused && intExit == 0 {
					if tc.twin == "" {
						t.Errorf("the int-key twin of %s answers %q where the reference raises %q, and no row owns the twin", tc.name, intOut, tc.trap)
					}
				}
				return
			}
			if intRefused || intExit != 0 || intOut != answer {
				if tc.twin == "" {
					t.Errorf("the int-key twin of %s answers %q (refused=%v exit=%d) where the None-key road answers %q, and no row owns the twin",
						tc.name, intOut, intRefused, intExit, answer)
				}
			}
		})
	}
}

// expectTrap runs a program that is meant to die and asserts the class-and-message line the reference
// raises it with, on the compiled leg. The exit code for a trap is 3 here and 1 in CPython, so the
// sentence — not the number — is what the two legs agree on.
func expectTrap(t *testing.T, src, want string) {
	t.Helper()
	res, err := JIT(src, 0)
	if err != nil {
		t.Fatalf("the compiled leg refused a program the reference runs (%v): %s", err, src)
	}
	if res.Code != 3 {
		t.Fatalf("%s exited %d where a trap is the answer (ADR 0166); stdout %q stderr %q", src, res.Code, res.Output, res.Stderr)
	}
	if !strings.Contains(res.Stderr, want) {
		t.Errorf("%s raised %q, the reference raises %q", src, strings.TrimSpace(res.Stderr), want)
	}
}

// programOutcome is the question the ladder asks: does this program compile at all, and if it runs,
// what does it print and with which exit code. `compiledOut` fails on a trap, which is right for a row
// that claims an answer and wrong for a row that compares two roads; this is the comparing form.
func programOutcome(t *testing.T, src string) (stdout string, refused bool, exitCode int) {
	t.Helper()
	res, err := JIT(src, 0)
	if err != nil {
		return err.Error(), true, 1
	}
	return res.Output, false, res.Code
}

// What the row does not own, filed rather than fixed: each line is today's compiled answer, pinned,
// beside what the reference gives, with the row that owes the rest. They live here so a later cycle
// cannot ship one of them as a surprise and so the tracker's rows have a home for the measurement.
func TestWhatTheNoneKeyReadStillCannotReachIsFiledNotFixed(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name        string
		src         string
		wantOut     string // what the compiled leg prints today
		wantRefusal string // the sentence it refuses with
		wantTrap    string // the sentence it dies with, where the reference answers
		reference   string // what CPython does, kept in the failure text
		row         string
	}{
		// The f-string FIELD of the entry sat here, printing the payload — `f"{d[None]}"` over
		// `d = {None: 3.5}` wrote `3`, the `@float_box` handle, while `print(d[None])` on the line above wrote
		// `3.5`. It is paid: the field now asks the one subscript dispatch print asks (`subscriptPairOf`),
		// and the three spellings are asserted as answers in the ladder above (roadmap Gap R.283, ADR 0356).
		// The truth of the entry used to sit here, testing an interned text's index against zero. It is paid:
		// an `if`, `while`, ternary and `bool()` over a self-describing entry ask the same subscript pair print
		// asks and send it through `@rt_pair_truth` (roadmap Gap R.284, ADR 0388). The answers are asserted in
		// `dict_entry_truth_test.go`.
		// The two Gap R.285 rows that used to sit here — the concatenation that refused and the repeat that
		// printed `0`, the interned index of the entry's text doubled as a number — are DELETED, not edited.
		// The static key door reads the void and the verdict now, the object answers an integer key by its
		// tag, and the operator roads ask the ENTRY what it holds (ADR 0419); the seven operator shapes are
		// asserted against CPython in `TestTheNoneKeyRoadAnswersWhatTheReferencePrintsInEveryDoorPosition`,
		// over both key spellings, and `pkg/lang/none_key_label_roads_test.go` and its CLI twin in
		// `integration/none_key_label_roads_test.go` carry the rest of the row's contract.
		// The sign of a float entry used to sit here, printing `3` — the `@float_box` handle — beside
		// `print(d[None])`'s `3.5`. It is paid: `abs` now asks the subscript the same (payload, tag) pair the
		// print door asks, and `min`/`max`/`sum` ask the fold door the pair ADR 0316 already knows how to move
		// (roadmap Gap R.287, ADR 0387). The answers are asserted in `dict_entry_pair_sign_test.go`, with the
		// int-key twin among them.
		// The untaggable-key control, asked because this row's gate is a key-kind question: what the backend
		// says when the key has no tag at all. Both refusals are honest in what they decline and wrong in how
		// they say it — one offers a way to run that ADR 0302 retired, the other names an AST node.
		{
			name:        "an_untaggable_key_offers_the_retired_way_to_run",
			src:         "d = {[1]: \"x\"}\nprint(d[[1]])\n",
			wantRefusal: "or run it interpreted",
			reference:   "raises TypeError: unhashable type: 'list' — declining is this backend's honest answer, the offer beside it is not (and `--interp` is a usage error)",
			row:         "Gap R.288",
		},
		// The tuple-key sibling that used to sit here — `d = {(1, 2): "x"}` / `print(d[(1, 2)])` refusing
		// with `unsupported expression *lang.Tuple` — is PAID: ADR 0361 gave the tuple a compiled object, the
		// shape answers `x`, and the sentence the emitter used for it no longer exists. TestATupleDictKeyAnswers
		// in dict_literal_key_test.go carries it as an answer now; Gap R.288 keeps its row for the offer to run
		// a program "interpreted", which the surviving row below still pins.
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			switch {
			case tc.wantRefusal != "":
				out, refused := compiledOutOrRefusal(t, tc.src)
				if !refused || !strings.Contains(out, tc.wantRefusal) {
					t.Errorf("%s: expected the refusal %q, got output %q refused=%v (%s)", tc.name, tc.wantRefusal, out, refused, tc.row)
				}
			case tc.wantTrap != "":
				res, err := JIT(tc.src, 0)
				if err != nil {
					t.Fatalf("%s: the compiled leg refused a program the reference runs (%v): %s", tc.name, err, tc.src)
				}
				if res.Code == 0 || !strings.Contains(res.Stderr, tc.wantTrap) {
					t.Errorf("%s: expected today's trap %q, got exit %d stdout %q stderr %q — the reference %s (%s)",
						tc.name, tc.wantTrap, res.Code, res.Output, res.Stderr, tc.reference, tc.row)
				}
			default:
				out, refused := compiledOutOrRefusal(t, tc.src)
				if refused {
					t.Errorf("%s: refused instead of printing today's answer %q: %s (%s)", tc.name, tc.wantOut, out, tc.row)
				}
				if out != tc.wantOut {
					t.Errorf("%s: the compiled leg prints %q, this row pinned %q — the reference %s (%s)",
						tc.name, out, tc.wantOut, tc.reference, tc.row)
				}
			}
		})
	}
}
