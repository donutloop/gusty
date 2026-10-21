package lang

import (
	"strings"
	"testing"
)

// dict_entry_pair_sign_test.go — the signless call and the folds ask a self-describing dict entry the
// (payload, tag) pair that print already asks (roadmap Gap R.287, ADR 0387).
//
// `d = {None: 3.5}` / `print(d[None])` has printed `3.5` since ADR 0353. Asking the very same read for its
// magnitude printed `3`: the dict entry had learned to travel as two words at the printer, while `abs` still
// lowered one word and subtracted the float box's heap handle from zero at exit 0. The folds made the same
// mistake by choosing between two untagged words — `max(d[None], 2)` printed `3`, the handle, where CPython
// returns the winning entry and therefore prints `3.5`.
//
// ADR 0309 and ADR 0316 had already built the doors. What was missing is that their gates asked a narrower
// question than the print door: `arithWouldRefuse` turns away a read whose container literal still looks
// statically kinded, while `isPairElem` — the question the tagged container builders ask — knows that the
// slot's meaning is the object's fact. The widening is deliberately local to the two call families. The
// one-word roads keep the tag, and the boundary they refuse is asserted below rather than quietly widened.

func TestTheSelfDescribingDictEntryAnswersAbsKeyIndependently(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name string
		src  string
		want string
	}{
		{"none_key_float", "d = {None: -3.5}\nprint(abs(d[None]))\n", "3.5\n"},
		{"true_key_float", "d = {True: -3.5}\nprint(abs(d[True]))\n", "3.5\n"},
		{"false_key_float", "d = {False: -3.5}\nprint(abs(d[False]))\n", "3.5\n"},
		{"int_key_float", "d = {1: -3.5}\nprint(abs(d[1]))\n", "3.5\n"},
		{"text_key_float", "d = {\"k\": -2.75}\nprint(abs(d[\"k\"]))\n", "2.75\n"},
		{"positive_entry", "d = {None: 4.25}\nprint(abs(d[None]))\n", "4.25\n"},
		{"zero_float", "d = {None: -0.0}\nprint(abs(d[None]))\n", "0.0\n"},
		{"integer_entry", "d = {None: -7}\nprint(abs(d[None]))\n", "7\n"},
		{"bool_entry", "d = {None: True}\nprint(abs(d[None]))\n", "1\n"},
		{"run_built_dict", "d = {}\nd[None] = -3.5\nprint(abs(d[None]))\n", "3.5\n"},
		{"nested_dict_entry", "xs = [{None: -1.5}]\nprint(abs(xs[0][None]))\n", "1.5\n"},
		{"bound_then_abs", "d = {None: -3.5}\nx = d[None]\nprint(abs(x))\n", "3.5\n"},
		{"abs_then_fold", "d = {None: -3.5}\nprint(min(abs(d[None]), 2))\n", "2\n"},
		{"same_read_prints_and_abses_alike", "d = {None: -3.5}\nprint(d[None])\nprint(abs(d[None]))\n", "-3.5\n3.5\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := compiledOut(t, tc.src); got != tc.want {
				t.Errorf("%s printed %q, want %q — abs read the payload without its tag", tc.src, got, tc.want)
			}
		})
	}
}

func TestTheSelfDescribingDictEntryAnswersTheFoldsWithItsOwnKind(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name string
		src  string
		want string
	}{
		{"min_float_below", "d = {None: -3.5}\nprint(min(d[None], 2))\n", "-3.5\n"},
		{"max_float_above", "d = {None: 3.5}\nprint(max(d[None], 2))\n", "3.5\n"},
		{"max_chooses_int_candidate", "d = {None: -3.5}\nprint(max(d[None], 2))\n", "2\n"},
		{"min_chooses_int_candidate", "d = {None: 3.5}\nprint(min(d[None], 2))\n", "2\n"},
		{"int_key_float", "d = {1: 2.5}\nprint(max(d[1], 1))\n", "2.5\n"},
		{"text_key_float", "d = {\"k\": -2.5}\nprint(min(d[\"k\"], 1))\n", "-2.5\n"},
		{"entry_against_entry", "d = {None: 1.5, True: 2.5}\nprint(max(d[None], d[True]))\n", "2.5\n"},
		{"float_vs_int_winner_keeps_its_kind", "d = {None: 2.0}\nprint(max(d[None], 1))\n", "2.0\n"},
		{"sum_a_float_entry", "d = {None: -3.5}\nprint(sum([d[None], 0]))\n", "-3.5\n"},
		{"sum_an_int_entry", "d = {None: -3}\nprint(sum([d[None], 0]))\n", "-3\n"},
		{"bool_winner_keeps_bool", "d = {None: True}\nprint(max(d[None], 1))\n", "True\n"},
		{"nested_entry", "xs = [{None: 2.5}]\nprint(max(xs[0][None], 1))\n", "2.5\n"},
		{"run_built_entry", "d = {}\nd[None] = 4.5\nprint(min(d[None], 9))\n", "4.5\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := compiledOut(t, tc.src); got != tc.want {
				t.Errorf("%s printed %q, want %q — the fold chose between payloads rather than pairs", tc.src, got, tc.want)
			}
		})
	}
}

func TestTheSelfDescribingDictEntrySignlessTrapsNameTheEntriesKind(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name string
		src  string
		want string
	}{
		{"text_entry_abs", "d = {None: \"hi\"}\nprint(abs(d[None]))\n", "TypeError: bad operand type for abs(): 'str'"},
		{"none_entry_abs", "d = {None: None}\nprint(abs(d[None]))\n", "TypeError: bad operand type for abs(): 'NoneType'"},
		{"list_entry_abs", "d = {None: [1]}\nprint(abs(d[None]))\n", "TypeError: bad operand type for abs(): 'list'"},
		{"dict_entry_abs", "d = {None: {\"a\": 1}}\nprint(abs(d[None]))\n", "TypeError: bad operand type for abs(): 'dict'"},
		{"set_entry_abs", "d = {None: {1}}\nprint(abs(d[None]))\n", "TypeError: bad operand type for abs(): 'set'"},
		{"text_entry_fold", "d = {None: \"hi\"}\nprint(max(d[None], 1))\n", "TypeError: '>' not supported between instances of 'int' and 'str'"},
		{"list_entry_fold", "d = {None: [1]}\nprint(min(d[None], 1))\n", "TypeError: '<' not supported between instances of 'int' and 'list'"},
		{
			name: "catchable_abs",
			src:  "d = {None: \"hi\"}\ntry:\n    print(abs(d[None]))\nexcept TypeError:\n    print(\"caught\")\n",
			want: "caught\n",
		},
		{
			name: "catchable_fold",
			src:  "d = {None: [1]}\ntry:\n    print(min(d[None], 1))\nexcept TypeError:\n    print(\"caught\")\n",
			want: "caught\n",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if tc.want == "caught\n" {
				if got := compiledOut(t, tc.src); got != tc.want {
					t.Errorf("%s printed %q, want %q", tc.src, got, tc.want)
				}
				return
			}
			expectTrap(t, tc.src, tc.want)
		})
	}
}

func TestTheSelfDescribingDictEntrySignlessRoadUsesTheOnePairDoors(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name  string
		src   string
		needs []string
	}{
		{
			name:  "abs_asks_the_subscript_pair_then_the_number_door",
			src:   "d = {None: -3.5}\nprint(abs(d[None]))\n",
			needs: []string{"@rt_dict_get_tagged", "@rt_dict_value_tag", "@rt_num_arith", "@rt_print_mixed_value"},
		},
		{
			name:  "fold_asks_the_subscript_pair_then_the_fold_door",
			src:   "d = {None: 3.5}\nprint(max(d[None], 2))\n",
			needs: []string{"@rt_dict_get_tagged", "@rt_dict_value_tag", "@rt_pair_fold", "@rt_print_mixed_value"},
		},
		{
			name:  "sum_asks_the_number_addition_door",
			src:   "d = {None: 3.5}\nprint(sum([d[None], 0]))\n",
			needs: []string{"@rt_dict_get_tagged", "@rt_dict_value_tag", "@rt_num_arith", "@rt_print_mixed_value"},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			body := mainRegion(t, tc.src)
			for _, helper := range tc.needs {
				if !strings.Contains(body, helper) {
					t.Errorf("%s: the answer did not travel through %s:\n%s", tc.name, helper, body)
				}
			}
			if strings.Contains(body, "sub i32 0") {
				t.Errorf("%s: the signless call returned to subtracting an untagged handle:\n%s", tc.name, body)
			}
		})
	}
}

func TestTheOrdinaryDictEntryKeepsItsStaticSignlessRoad(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name string
		src  string
	}{
		{"int_entry_abs", "d = {1: -2}\nprint(abs(d[1]))\n"},
		{"int_entry_fold", "d = {1: 2}\nprint(max(d[1], 3))\n"},
		{"text_key_int_entry_fold", "d = {\"k\": 2}\nprint(max(d[\"k\"], 3))\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			body := mainRegion(t, tc.src)
			if strings.Contains(body, "@rt_dict_value_tag") || strings.Contains(body, "@rt_pair_fold") {
				t.Errorf("%s reached a self-describing door though its entry is an ordinary int:\n%s", tc.name, body)
			}
		})
	}
}

func TestTheSelfDescribingDictEntrySignlessRecordLegAgreesWithTheCompiledLeg(t *testing.T) {
	t.Parallel()
	for _, src := range []string{
		"d = {None: -3.5}\nprint(abs(d[None]))\n",
		"d = {1: -3.5}\nprint(abs(d[1]))\n",
		"d = {True: -3.5}\nprint(abs(d[True]))\n",
		"d = {None: 3.5}\nprint(max(d[None], 2))\n",
		"d = {None: -3.5}\nprint(min(d[None], 2))\n",
		"d = {None: 3.5}\nprint(sum([d[None], 0]))\n",
		"d = {None: True}\nprint(max(d[None], 1))\n",
		"xs = [{None: -1.5}]\nprint(abs(xs[0][None]))\n",
	} {
		t.Run(strings.ReplaceAll(src, "\n", " / "), func(t *testing.T) {
			t.Parallel()
			want, err := runGoldenStdout(t, src)
			if err != nil {
				t.Fatalf("the record did not answer %s: %v", src, err)
			}
			if got := compiledOut(t, src); got != want {
				t.Errorf("the compiled leg printed %q, the record answers %q for %s", got, want, src)
			}
		})
	}
}

// The boundary ADR 0387 does not cross. The fold and `abs` may receive a pair; a position that keeps one
// word for the whole value still has nowhere to put the tag, and its old answer must not be reconstructed
// from the winner's payload. These rows are the negative controls for the gate.
func TestTheSelfDescribingDictEntrySignlessBoundaryStaysWhereADR0387DrewIt(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name     string
		src      string
		wantOut  string
		wantFail string
		row      string
	}{
		{
			name:    "abs_answer_handed_to_a_function",
			src:     "def f(v):\n    return v\nd = {None: -3.5}\nprint(f(abs(d[None])))\n",
			wantOut: "3\n",
			row:     "Gap R.146 (the call's one-word argument)",
		},
		{
			name:    "entry_handed_to_a_function",
			src:     "def f(v):\n    return v\nd = {None: -3.5}\nprint(f(d[None]))\n",
			wantOut: "3\n",
			row:     "Gap R.220 (the dict read at the call boundary)",
		},
		{
			name:    "entry_used_as_an_arithmetic_operand",
			src:     "d = {None: -3.5}\nprint(d[None] + 1)\n",
			wantOut: "-2.5\n",
			row:     "the int-family numeric road answers this entry; Gap R.285 owns text concatenation/repetition",
		},
		{
			name:    "fold_answer_handed_to_a_function",
			src:     "def f(v):\n    return v\nd = {None: 3.5}\nprint(f(max(d[None], 2)))\n",
			wantOut: "3\n",
			row:     "Gap R.146 (the call's one-word argument)",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			out, refused := compiledOutOrRefusal(t, tc.src)
			if refused {
				if !strings.Contains(out, tc.wantFail) {
					t.Errorf("%s: expected the refusal %q, got %q (%s)", tc.name, tc.wantFail, out, tc.row)
				}
				if strings.Contains(out, "or run it interpreted") {
					t.Errorf("%s: the refusal offers the execution backend ADR 0302 retired (%s)", tc.name, tc.row)
				}
				return
			}
			if out != tc.wantOut {
				t.Errorf("%s: the compiled leg printed %q, this row pinned %q (%s)", tc.name, out, tc.wantOut, tc.row)
			}
		})
	}
}
