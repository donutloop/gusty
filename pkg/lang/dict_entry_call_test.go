package lang

import (
	"fmt"
	"strings"
	"testing"
)

// dict_entry_call_test.go — the CALL ARGUMENT asks a self-describing read for the (payload, tag) pair the
// print road already asks (roadmap Gap R.220, ADR 0389).
//
// `d = {"a": "x"}` / `def id(v): return v` / `print(id(d["a"]))` printed `1` at the exit code of success:
// the text's `@str_tab` index. A float entry printed its `@float_box` handle, a `None` entry printed `0`,
// a container entry printed a heap handle — every one of them the payload with its kind left behind in the
// dict, because the parameter keeps one word and the argument road hands over that word. The same program
// with the literal written in place (`id({"a": "x"}["a"])`) refused at exit 1, which is the asymmetry
// `TestTheLiteralRoadIsNeverWorseThanTheVariableRoad` was written to catch: two spellings of one read
// answering two different ways is what made the row measurable.
//
// Four roads read a self-describing read, and now ask the same dispatch (`subscriptPairOf`):
//
//   - the printer asks `@rt_print_mixed_value` (Gap R.221, ADR 0353),
//   - a condition asks `@rt_pair_truth` (Gap R.284, ADR 0388),
//   - `abs`/`min`/`max`/`sum` ask it (Gap R.287, ADR 0387),
//   - and — this row — the call argument, which hands the callee both words per call site (Gap R.220).
//
// The scan that decides the arity is `pairCallScan`, and its new arm is asked the same two questions the
// door asks: is the side a container the program spelled out and never wrote again, and is some slot's
// payload a word that means something only beside its kind? Where the door declines, the argument keeps
// the ordinary road and that road's own refusal, word for word — a paid door trades a refusal for an
// answer, never an answer for a wrong number, and never an answer for a module `llc` rejects (which is why
// `pairBodyIsFloatReturnRoad` exists: ADR 0274's promotion owns those bodies, roadmap Gap R.344 their
// answer word).
//
// What the row does NOT claim is pinned below rather than left to infer: a binding of the entry keeps
// Gap R.146's refusal, `len` of a container parameter keeps Gap R.229's, the arithmetic a text or a
// container operand still cannot do keeps the operator roads' rows, and a body emitted under the
// string-return convention still renders its parameter's payload (roadmap Gap R.368). What the callee's own
// CONDITION asks is no longer on that list: it asks the tag that crossed with the argument, which is
// roadmap Gap R.361, paid by ADR 0393 in `pkg/lang/pair_param_truth_test.go`.

type entryCallRow struct {
	name, src, want string
}

// entryCallAnswers is the answer ladder: every entry kind whose payload means something other than
// itself, handed to a call. Every `want` below is what `python3` printed for the same source — read off the
// reference before the case was written, never off this backend.
func entryCallAnswers() []entryCallRow {
	const idf = "def id(v):\n    return v\n\n"
	row := func(name, dict, key, want string) entryCallRow {
		return entryCallRow{name, dict + "\n" + idf + "print(id(" + key + "))\n", want}
	}
	return []entryCallRow{
		row("text_entry", "d = {\"a\": \"x\"}", "d[\"a\"]", "x\n"),
		row("empty_text_entry", "d = {\"a\": \"\"}", "d[\"a\"]", "\n"),
		row("float_entry", "d = {\"a\": 3.5}", "d[\"a\"]", "3.5\n"),
		row("negzero_float_entry", "d = {\"a\": -0.0}", "d[\"a\"]", "-0.0\n"),
		row("none_entry", "d = {\"a\": None}", "d[\"a\"]", "None\n"),
		row("verdict_entry", "d = {\"a\": True}", "d[\"a\"]", "True\n"),
		row("false_entry", "d = {\"a\": False}", "d[\"a\"]", "False\n"),
		row("list_entry", "d = {\"a\": [1, 2]}", "d[\"a\"]", "[1, 2]\n"),
		row("empty_list_entry", "d = {\"a\": []}", "d[\"a\"]", "[]\n"),
		row("dict_entry", "d = {\"a\": {\"k\": 1}}", "d[\"a\"]", "{'k': 1}\n"),
		row("int_entry", "d = {\"a\": 7}", "d[\"a\"]", "7\n"),
		row("zero_int_entry", "d = {\"a\": 0}", "d[\"a\"]", "0\n"),
		// The key is a spelling; the entry is the fact. ADR 0353's rule, restated at the call boundary.
		row("none_key", "d = {None: 3.5}", "d[None]", "3.5\n"),
		row("true_key", "d = {True: [3]}", "d[True]", "[3]\n"),
		row("int_key", "d = {0: 2.5}", "d[0]", "2.5\n"),
	}
}

// TestACallArgumentCarriesTheEntryAndItsKind is the answering half: what the reference prints is what the
// compiled leg prints, or the row has not been paid.
func TestACallArgumentCarriesTheEntryAndItsKind(t *testing.T) {
	t.Parallel()
	for _, tc := range entryCallAnswers() {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := compiledOut(t, tc.src); got != tc.want {
				t.Errorf("printed %q, want %q\nsrc: %s", got, tc.want, tc.src)
			}
		})
	}
	t.Run("the_literal_written_in_place", func(t *testing.T) {
		t.Parallel()
		src := "def id(v):\n    return v\n\nprint(id({\"a\": \"x\"}[\"a\"]))\n"
		if got := compiledOut(t, src); got != "x\n" {
			t.Errorf("printed %q, want %q — the same read with the literal in place", got, "x\n")
		}
	})
}

// TestTheCallArgumentIsAskedOfTheEntryNotTheKey is ADR 0353's rule one position further out: the same
// entry travels the same two words whether the key was spelled `None`, `True`, `0` or `"k"`. A door that
// keyed its dispatch on the key's spelling would answer the float under one key and the handle under
// another, which is how Gap R.221's row printed `3` for `d[None]` and `3.5` for `d[0]`.
func TestTheCallArgumentIsAskedOfTheEntryNotTheKey(t *testing.T) {
	t.Parallel()
	for _, entry := range []string{"3.5", `"word"`, "[1, 2]", "None", "True"} {
		for _, key := range []string{"None", "True", "0", "\"k\""} {
			entry, key := entry, key
			src := "def id(v):\n    return v\n\nd = {" + key + ": " + entry + "}\nprint(id(d[" + key + "]))\n"
			t.Run(entry+"_under_"+key, func(t *testing.T) {
				t.Parallel()
				ref, err := runGoldenStdout(t, src)
				if err != nil {
					t.Fatalf("the record leg did not answer %s: %v", src, err)
				}
				got := compiledOut(t, src)
				if got != ref {
					t.Errorf("the compiled leg printed %q, the record prints %q for %s", got, ref, src)
				}
			})
		}
	}
}

// TestACallArgumentCrossesPerCallSite is the half that makes the row a per-call-site fact rather than a
// per-function one. One `def` is called several ways in one program: the parameter either carries the pair
// for all of them or for none, so an integer entry and a text entry have to be answered by the same
// parameter's two words — and the ordinary call sites (a literal, a verdict, the void) must keep working.
func TestACallArgumentCrossesPerCallSite(t *testing.T) {
	t.Parallel()
	for _, tc := range []entryCallRow{
		{
			name: "int_and_float_entries_of_one_dict",
			src:  "def id(v):\n    return v\n\nd = {0: 1, 1: 2.5}\nprint(id(d[0]), id(d[1]))\n",
			want: "1 2.5\n",
		},
		{
			name: "an_entry_beside_plain_arguments",
			src:  "def id(v):\n    return v\n\nd = {\"a\": \"x\"}\nprint(id(d[\"a\"]), id(4), id(True), id(2.5), id(None))\n",
			want: "x 4 True 2.5 None\n",
		},
		{
			name: "a_keyword_argument",
			src:  "def id(v):\n    return v\n\nd = {\"a\": 2.5}\nprint(id(v=d[\"a\"]))\n",
			want: "2.5\n",
		},
		{
			name: "across_a_default",
			src:  "def f(v=1.5):\n    return v\n\nd = {\"a\": 2.5}\nprint(f(d[\"a\"]), f())\n",
			want: "2.5 1.5\n",
		},
		{
			name: "two_parameters_two_entries",
			src:  "def add(x, y):\n    return x + y\n\nd = {\"a\": 2.5, \"b\": 1.5}\nprint(add(d[\"a\"], d[\"b\"]))\n",
			want: "4.0\n",
		},
		{
			name: "forwarded_to_a_second_callee",
			src:  "def g(v):\n    return v * 2\n\ndef f(v):\n    return g(v)\n\nd = {\"a\": 2.5}\nprint(f(d[\"a\"]))\n",
			want: "5.0\n",
		},
		{
			name: "the_body_arithmetics_on_it",
			src:  "def f(v):\n    return v + 1\n\nd = {\"a\": 3.5}\nprint(f(d[\"a\"]))\n",
			want: "4.5\n",
		},
		{
			// An ordering returned as a VALUE, not tested: the pair road serves it, which is the half of
			// Gap R.161's comparison family this door reaches and the reason the row is an answer here rather
			// than a refusal.
			name: "a_comparison_returned_as_a_value",
			src:  "def f(v):\n    return v > 2\n\nd = {\"a\": 3.5}\nprint(f(d[\"a\"]))\n",
			want: "True\n",
		},
		{
			name: "the_body_orders_on_it",
			src:  "def f(v):\n    if v > 2:\n        return 1\n    return 0\n\nd = {\"a\": 3.5, \"b\": 1.5}\nprint(f(d[\"a\"]), f(d[\"b\"]))\n",
			want: "1 0\n",
		},
		{
			// The floor of the two families: the same door decides whether the answer is `1` or `1.0`,
			// and only the tag says which operand arrived.
			name: "the_body_floor-divides_it",
			src:  "def f(v):\n    return v // 2\n\nd = {\"a\": 3.5}\nprint(f(d[\"a\"]))\n",
			want: "1.0\n",
		},
		{
			// A body that PRINTS its parameter is served by ADR 0268's renderer road; the entry's kind is
			// what makes the printed line the reference's line.
			name: "the_body_prints_the_parameter",
			src:  "def p(v):\n    print(v)\n\nd = {\"a\": [1, 2]}\np(d[\"a\"])\np(3)\n",
			want: "[1, 2]\n3\n",
		},
		{
			// The read need not be a dict's: a literal's element is the same pair, and the mixed list used
			// to refuse the whole call.
			name: "a_mixed_list_literal_element",
			src:  "def id(v):\n    return v\n\nxs = [1, \"a\", None]\nprint(id(xs[1]), id(xs[0]))\n",
			want: "a 1\n",
		},
		{
			name: "a_tuple_literal_element",
			src:  "def id(v):\n    return v\n\nt = (1, \"a\")\nprint(id(t[1]))\n",
			want: "a\n",
		},
		{
			name: "a_nested_list_literal_element",
			src:  "def id(v):\n    return v\n\nxs = [[1, 2], \"z\"]\nprint(id(xs[0]))\n",
			want: "[1, 2]\n",
		},
		{
			// The name was written twice, so the literal the door reads is the second one — and the read
			// that answers the first would be the answer of a freed object.
			name: "a_rebound_name_answers_the_literal_it_holds",
			src:  "def id(v):\n    return v\n\nd = {\"a\": \"x\"}\nd = {\"a\": \"y\"}\nprint(id(d[\"a\"]))\n",
			want: "y\n",
		},
		{
			name: "every_kind_one_dict",
			src:  "def id(v):\n    return v\n\nd = {\"a\": \"x\", \"f\": 2.5, \"n\": None, \"l\": [1, 2], \"i\": 7, \"e\": []}\nprint(id(d[\"a\"]), id(d[\"f\"]), id(d[\"n\"]), id(d[\"l\"]), id(d[\"i\"]), id(d[\"e\"]))\n",
			want: "x 2.5 None [1, 2] 7 []\n",
		},
	} {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := compiledOut(t, tc.src); got != tc.want {
				t.Errorf("printed %q, want %q\nsrc: %s", got, tc.want, tc.src)
			}
		})
	}
}

// TestTheCallArgumentRoadReachesThePairDoors is the IR half: the `define` grows the kind word beside the
// payload, the `call` passes both, and the dict the read came out of is the tagged one.
func TestTheCallArgumentRoadReachesThePairDoors(t *testing.T) {
	t.Parallel()
	ir := emittedModuleIR(t, "def id(v):\n    return v\n\nd = {\"a\": \"x\"}\nprint(id(d[\"a\"]))\n")
	for _, want := range []string{
		"define i32 @gy_id(i32 %p0, i32 %q0) {", // the parameter's second word
		"call i32 @gy_id(i32 %t",                // the call site passes it
		"@rt_tag_elem",                          // the dict recorded what each slot holds
	} {
		if !strings.Contains(ir, want) {
			t.Errorf("emitted module has no %q\n%s", want, ir)
		}
	}
	if strings.Count(ir, "define i32 @gy_id(") != 1 {
		t.Errorf("the callee was declared more than once\n%s", ir)
	}
}

// TestAProvableIntEntryCrossesTheCallInOneWord is the negative control, and the reason a paid row is not
// simply "everything through the object": an entry whose VALUE is a provable integer has a payload that IS
// the number, and ADR 0387 draws the same line at the fold. The claim is the ABSENCE of the pair road, so
// a gate that opens wider than it argues fails here rather than in review. A text KEY does not open it —
// the key's kind is the door's own business (`dictKeyCarriesPair`, ADR 0353), never the parameter's.
func TestAProvableIntEntryCrossesTheCallInOneWord(t *testing.T) {
	t.Parallel()
	for _, src := range []string{
		"def id(v):\n    return v\n\nd = {\"a\": 7}\nprint(id(d[\"a\"]))\n",
		"def id(v):\n    return v\n\nd = {1: 2, 2: 3}\nprint(id(d[1]))\n",
		"def id(v):\n    return v\n\nxs = [4, 5]\nprint(id(xs[0]))\n",
	} {
		src := src
		t.Run(src, func(t *testing.T) {
			t.Parallel()
			ir := emittedModuleIR(t, src)
			if strings.Contains(ir, "define i32 @gy_id(i32 %p0, i32 %q0)") {
				t.Errorf("an integer slot's parameter was given a tag word it will never read\n%s", ir)
			}
			if !strings.Contains(ir, "define i32 @gy_id(i32 %p0) {") {
				t.Errorf("the integer-slot callee is not the one-word declare it should be\n%s", ir)
			}
		})
	}
	for _, tc := range []struct{ src, want string }{
		{"def id(v):\n    return v\n\nd = {\"a\": 7}\nprint(id(d[\"a\"]))\n", "7\n"},
		{"def id(v):\n    return v\n\nxs = [4, 5]\nprint(id(xs[0]))\n", "4\n"},
	} {
		tc := tc
		t.Run(tc.want, func(t *testing.T) {
			t.Parallel()
			if got := compiledOut(t, tc.src); got != tc.want {
				t.Errorf("printed %q, want %q", got, tc.want)
			}
		})
	}
}

// TestTheFloatReturnRoadKeepsWhatThisRowDoesNotTake is the containment, and it is asserted as an ANSWER:
// `def f(v): return v + 0.5` is ADR 0274's promoted body, emitted `define double`, a word a pair's tag has
// nowhere beside. Opening the parameter there would trade a program that answers today for one that dies in
// `llc` at the code the exit contract reserves for a compiler bug, so the scan's new arm stands aside and
// the ordinary road keeps the answer it has always produced (roadmap Gap R.344 owns the answer word).
func TestTheFloatReturnRoadKeepsWhatThisRowDoesNotTake(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct{ name, src, want string }{
		{
			name: "an_entry_read_handed_to_a_promoted_body",
			src:  "d = {\"a\": 2.5}\n\ndef f(v):\n    return v + 0.5\n\nprint(f(d[\"a\"]))\n",
			want: "3.0\n",
		},
		{
			name: "a_promoted_body_written_the_other_way",
			src:  "d = {\"a\": 2.0}\n\ndef f(v):\n    return 1.0 + v\n\nprint(f(d[\"a\"]))\n",
			want: "3.0\n",
		},
		{
			name: "true_division_is_the_promotions_shape_too",
			src:  "d = {\"a\": 5}\n\ndef f(v):\n    return v / 2\n\nprint(f(d[\"a\"]))\n",
			want: "2.5\n",
		},
	} {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			res, err := Compile(tc.src)
			if err != nil {
				t.Fatalf("%s refused: %v\nsrc: %s", tc.name, err, tc.src)
			}
			if res.IR == "" {
				t.Fatalf("%s emitted no module", tc.name)
			}
			if got := compiledOut(t, tc.src); got != tc.want {
				t.Errorf("printed %q, want %q\nsrc: %s", got, tc.want, tc.src)
			}
			if strings.Contains(res.IR, "define double @gy_f(double %p0, i32 %q0)") {
				t.Errorf("the promoted body was given a tag word beside its double — that is the module llc rejects\n%s", res.IR)
			}
		})
	}
}

// TestTheCallArgumentRefusesWhatThePairRoadCannotServe is the half that keeps the row honest: every one of
// these programs must leave exit 1 with a sentence about the construct, never exit 0 with a payload dressed
// as a value, and each row names the position it leaves to another row so a future reader can tell "not
// yet" from "not ever".
func TestTheCallArgumentRefusesWhatThePairRoadCannotServe(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name    string
		src     string
		wantSub string
		row     string
	}{
		{
			// The binding is still the one-word position Gap R.146 owns: the name keeps the payload and
			// loses the tag, so the call has nothing to hand over.
			name:    "a_bound_entry_keeps_its_own_row",
			src:     "def id(v):\n    return v\n\nd = {\"a\": []}\nx = d[\"a\"]\nprint(id(x))\n",
			wantSub: "holds a slot the program built at run time",
			row:     "Gap R.146",
		},
		{
			// `len` of a container PARAMETER is the one-word `len` road's, which still believes its
			// argument is a string.
			name:    "len_of_a_container_parameter",
			src:     "def f(v):\n    return len(v)\n\nd = {\"a\": [1, 2, 3]}\nprint(f(d[\"a\"]))\n",
			wantSub: "len of a non-string variable",
			row:     "Gap R.229",
		},
	} {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			out, refused := compiledOutOrRefusal(t, tc.src)
			if !refused {
				t.Fatalf("%s answered %q at exit 0; the pair road cannot serve this position, so a payload must not "+
					"be handed to the callee instead (%s)\nsrc: %s", tc.name, out, tc.row, tc.src)
			}
			if !strings.Contains(out, tc.wantSub) {
				t.Errorf("%s refused without naming %q (%s):\n%s", tc.name, tc.wantSub, tc.row, out)
			}
			if strings.Contains(out, "or run it interpreted") {
				t.Errorf("%s's refusal offers the execution backend ADR 0302 retired (%s)", tc.name, tc.row)
			}
		})
	}
}

// TestTheCallArgumentTrapsWhereTheReferenceTraps pins the shapes where the callee's own door raises: the
// arithmetic road answers a text operand with CPython's own TypeError, where the reference repeats the
// text. That is strictly better than the `2` the one-word road printed for it (the interned index times
// two), and it is still not the reference's answer — which is why it is pinned here and owned by the
// operator road's row rather than claimed by this one.
func TestTheCallArgumentTrapsWhereTheReferenceTraps(t *testing.T) {
	t.Parallel()
	t.Run("a_text_entry_repeated", func(t *testing.T) {
		t.Parallel()
		src := "def f(v):\n    return v * 2\n\nd = {\"a\": \"ab\"}\nprint(f(d[\"a\"]))\n"
		class, message := jitTrap(t, src)
		if class != "TypeError" || !strings.Contains(message, "unsupported operand type(s) for *: 'str' and 'int'") {
			t.Errorf("the compiled leg traps %s: %q, pinned today for the reference's `abab` (Gap R.82, Gap R.285)", class, message)
		}
		// The record only holds a source the reference answered AND the compiled leg agreed with — that is
		// how tools/recmerge writes it — so this row is checked when the day comes that the shape is on it.
		if HasGoldenAnswer(src) {
			if got, err := runGoldenStdout(t, src); err == nil && got == "abab\n" {
				t.Errorf("the record answers the reference's bytes while the compiled leg still traps — this row has closed, delete it (Gap R.82)")
			}
		}
	})
	t.Run("a_missing_key_still_raises_from_the_read", func(t *testing.T) {
		t.Parallel()
		src := "def id(v):\n    return v\n\nd = {\"a\": \"x\"}\nprint(id(d[\"b\"]))\n"
		class, message := jitTrap(t, src)
		if class != "KeyError" || !strings.Contains(message, "'b'") {
			t.Errorf("the compiled leg traps %s: %q, want the reference's KeyError: 'b'", class, message)
		}
	})
}

// TestTheCallArgumentDebtIsPinnedNotCelebrated keeps the shapes this row reaches — or leaves where it found
// them — pinned to what the compiled leg does TODAY beside what the reference says. They are not refusals,
// so the table above cannot cover them: a wrong answer at exit 0 is exactly what a row of this family
// leaves behind when it stops short, and a pin that fails when the answer arrives is the only thing that
// keeps the residue from being read as support.
func TestTheCallArgumentDebtIsPinnedNotCelebrated(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name, src, today, row string
	}{
		{
			// A body the string-return road owns keeps its parameters one-word (ADR 0174, ADR 0360's
			// escape covers only a body that FORMATS its parameter), so `str(v)` renders the payload.
			// It sat under Gap R.361 while that row was open; the condition half of that row closed with
			// ADR 0393 and this shape is not its door — it is the convention the whole body is emitted
			// under, which is roadmap Gap R.368's.
			name:  "a_str_returning_body_reads_the_payload",
			src:   "def s(v):\n    return str(v)\n\nd = {\"a\": \"x\"}\nprint(s(d[\"a\"]))\n",
			today: "1\n",
			row:   "Gap R.368",
		},
		{
			// A recursive body is a self-call, and a self-call keeps one word for its answer (ADR 0273):
			// the frame has no second word for the tag, so the caller hands over the payload.
			name:  "a_recursive_callee_handed_the_read",
			src:   "def f(v, n):\n    if n <= 0:\n        return v\n    return f(v, n - 1)\n\nd = {\"a\": 3.5}\nprint(f(d[\"a\"], 2))\n",
			today: "3\n",
			row:   "Gap R.324 (the self-call rule of ADR 0273)",
		},
		{
			// The arms of a returned ternary are values, and the pair road serves the TEST and not the
			// answer word.
			name:  "a_ternary_arm_that_is_the_parameter",
			src:   "def pick(v):\n    return v if v else 0\n\nd = {\"a\": 2.5}\nprint(pick(d[\"a\"]))\n",
			today: "3\n",
			row:   "Gap R.324",
		},
		{
			// An instance's payload is a heap handle with a tag of its own, and the scan cannot name the
			// kind of a slot whose value is a call — `C()` is not a spelling — so the read crosses as one
			// word. Print, len, `in` and a loop over the same slot ask the tag (ADR 0325).
			name:  "an_instance_entry_handed_to_a_call",
			src:   "class C:\n    def __init__(self):\n        self.v = 1\n\ndef id(v):\n    return v\n\nd = {\"a\": C()}\nprint(id(d[\"a\"]) == 3)\n",
			today: "True\n",
			row:   "Gap R.364 (the instance side of the call argument)",
		},
	} {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, refused := compiledOutOrRefusal(t, tc.src)
			if refused {
				t.Fatalf("%s now refuses (%s) — the pin said it answers, and the row that owns it changed\nsrc: %s", tc.name, got, tc.src)
			}
			if got == tc.today {
				return
			}
			t.Errorf("%s printed %q; this row pinned %q — the shape changed answer, DELETE or rewrite the pin (%s)\nsrc: %s",
				tc.name, got, tc.today, tc.row, tc.src)
		})
	}
}

// TestTheCallArgumentAgreesWithTheRecord is ADR 0302's rule at this altitude: the answer these programs
// print is the answer the record holds, and a source that fell off the record fails here rather than
// passing by having nothing to compare against.
func TestTheCallArgumentAgreesWithTheRecord(t *testing.T) {
	t.Parallel()
	srcs := []string{}
	for _, tc := range entryCallAnswers() {
		srcs = append(srcs, tc.src)
	}
	srcs = append(srcs,
		"def id(v):\n    return v\n\nprint(id({\"a\": \"x\"}[\"a\"]))\n",
		"def id(v):\n    return v\n\nd = {0: 1, 1: 2.5}\nprint(id(d[0]), id(d[1]))\n",
		"def id(v):\n    return v\n\nd = {\"a\": \"x\"}\nprint(id(d[\"a\"]), id(4), id(True), id(2.5), id(None))\n",
		"def f(v):\n    return v + 1\n\nd = {\"a\": 3.5}\nprint(f(d[\"a\"]))\n",
		"def id(v):\n    return v\n\nxs = [1, \"a\", None]\nprint(id(xs[1]), id(xs[0]))\n",
		"def id(v):\n    return v\n\nd = {\"a\": \"x\", \"f\": 2.5, \"n\": None, \"l\": [1, 2], \"i\": 7, \"e\": []}\nprint(id(d[\"a\"]), id(d[\"f\"]), id(d[\"n\"]), id(d[\"l\"]), id(d[\"i\"]), id(d[\"e\"]))\n",
	)
	for _, src := range srcs {
		src := src
		t.Run(strings.ReplaceAll(src, "\n", " / "), func(t *testing.T) {
			t.Parallel()
			want, err := runGoldenStdout(t, src)
			if err != nil {
				t.Fatalf("the record did not answer %s: %v", src, err)
			}
			got, isRefusal := compiledOutOrRefusal(t, src)
			if isRefusal {
				t.Fatalf("the compiled leg refused a program the record answers: %s\nsrc: %s", got, src)
			}
			if got != want {
				t.Errorf("the compiled leg printed %q, the record answers %q for %s", got, want, src)
			}
		})
	}
}

// pairCarriedParams names the (function, parameter) positions the scan opened onto the pair road. It is
// the arm's whole observable answer before any IR exists, which is what makes it askable as a function:
// an answer table can hide a gate that is always true, this cannot.
func pairCarriedParams(prog *Program) [][2]string {
	specs, _ := pairCallScan(prog)
	out := [][2]string{}
	for fnName, spec := range specs {
		if spec == nil {
			continue
		}
		for i := range spec.params {
			out = append(out, [2]string{fnName, fmt.Sprintf("%d", i)})
		}
	}
	return out
}

// TestTheScanAnswersTheLiteralReadItself is the gate asked as a function — the shape an answer table cannot
// reach: what the scan says about a read, before any IR exists, and in both directions. A program the arm
// admits must be one the door answers; a program it declines must keep the road it has always had.
func TestTheScanAnswersTheLiteralReadItself(t *testing.T) {
	t.Parallel()
	parsed := func(src string) *Program {
		t.Helper()
		prog, err := Parse(src)
		if err != nil {
			t.Fatalf("parse: %v", err)
		}
		return prog
	}
	carries := func(src string) bool {
		return len(pairCarriedParams(parsed(src))) > 0
	}
	for _, tc := range []struct {
		name, src string
		want      bool
	}{
		{"a text entry", "d = {\"a\": \"x\"}\ndef id(v):\n    return v\n\nprint(id(d[\"a\"]))\n", true},
		{"a float entry", "d = {\"a\": 3.5}\ndef id(v):\n    return v\n\nprint(id(d[\"a\"]))\n", true},
		{"a void entry", "d = {\"a\": None}\ndef id(v):\n    return v\n\nprint(id(d[\"a\"]))\n", true},
		{"a container entry", "d = {\"a\": [1]}\ndef id(v):\n    return v\n\nprint(id(d[\"a\"]))\n", true},
		{"a mixed list element", "def id(v):\n    return v\n\nxs = [1, \"a\"]\nprint(id(xs[1]))\n", true},
		{"an inline literal read", "def id(v):\n    return v\n\nprint(id({\"a\": \"x\"}[\"a\"]))\n", true},
		// Declined, each of them for a reason the road below can act on.
		{"an integer entry is the fold's", "d = {\"a\": 7}\ndef id(v):\n    return v\n\nprint(id(d[\"a\"]))\n", false},
		{"an integer list element is the fold's", "def id(v):\n    return v\n\nxs = [1, 2]\nprint(id(xs[0]))\n", false},
		{"a rebound name is the ordinary road's", "def id(v):\n    return v\n\nd = {\"a\": \"x\"}\nd = 3\nprint(id(d[\"a\"]))\n", false},
		{"a call for a key is the answer roads", "def id(v):\n    return v\n\ndef key():\n    return \"a\"\n\nd = {\"a\": \"x\"}\nprint(id(d[key()]))\n", false},
		{"an entry built by a call is not a spelling", "def id(v):\n    return v\n\ndef build():\n    return \"x\"\n\nd = {\"a\": build()}\nprint(id(d[\"a\"]))\n", false},
		{"a bare name argument is not a read", "def id(v):\n    return v\n\nd = {\"a\": \"x\"}\nprint(id(d))\n", false},
	} {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := carries(tc.src); got != tc.want {
				t.Errorf("the scan answered %v, want %v for: %s", got, tc.want, tc.src)
			}
		})
	}
}
