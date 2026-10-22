package lang

import (
	"strings"
	"testing"
)

// empty_container_argument_test.go — the container the program spells as a CALL crosses a call argument
// the way the literal spells it (roadmap L11.1, Gap R.369, ADR 0394).
//
// ADR 0391 put `(*irGen).emptyContainerCall` into `elemKindTag`, so the BUILDER writes a `set()` / `list()`
// / `dict()` slot with the tag of the container the call spells. ADR 0392 put the same door into the roads
// that DECIDE an empty container's kind — `staticContainerKind`, `literalNeedsHeap`, `literalNeedsTags` — so
// the object is built self-describing at all. What neither could see is that the pair scan keeps a THIRD
// copy of the spelling test: `paircall.go`'s `pairSlotIsSpelling` asks "can the builder write this slot from
// its spelling alone?" before it will open a parameter for the read of it, and a `Call` was not on its list.
// So the read of an entry the program spelled `{"k": list()}` was never marked, the position closed, the
// ordinary road handed over one word, and:
//
//	def f(v):
//	    print(v)
//	    if v:
//	        print("t")
//	    else:
//	        print("f")
//
//	d = {"k": list()}
//	f(d["k"])
//
// printed `3` — the heap handle of the empty list — and took the then arm, at the exit code of success,
// where the reference prints `[]` and `f`. Same answer for `set()` and `dict()`, and for the list element
// `xs = [list()]` / `f(xs[0])` the program refused outright with Gap R.82's mixed-list sentence.
//
// The three roads that answer "what kind is the object in this slot" now ask one door. This file asserts the
// answer at the argument and its positions, the IR that carries it, the shadowing controls that keep the door
// shut where the program took the name, and — in the last table — the shapes the row still cannot serve, each
// naming the row that owns it.

// emptyContainerArgumentAnswerRows is the answer table. Every `want` is what `python3` printed for the same
// source, read off the reference before the row was written.
func emptyContainerArgumentAnswerRows() []struct{ name, src, want string } {
	return []struct{ name, src, want string }{
		// The print door: the entry, spelled three ways, on both container sides.
		{
			name: "dict_print_door_empty_list_call",
			src:  "def f(v):\n    print(v)\n\nd = {\"k\": list()}\nf(d[\"k\"])\n",
			want: "[]\n",
		},
		{
			name: "list_print_door_empty_list_call",
			src:  "def f(v):\n    print(v)\n\nxs = [list()]\nf(xs[0])\n",
			want: "[]\n",
		},
		{
			name: "dict_print_door_empty_set_call",
			src:  "def f(v):\n    print(v)\n\nd = {\"k\": set()}\nf(d[\"k\"])\n",
			want: "set()\n",
		},
		{
			name: "list_print_door_empty_set_call",
			src:  "def f(v):\n    print(v)\n\nxs = [set()]\nf(xs[0])\n",
			want: "set()\n",
		},
		{
			name: "dict_print_door_empty_dict_call",
			src:  "def f(v):\n    print(v)\n\nd = {\"k\": dict()}\nf(d[\"k\"])\n",
			want: "{}\n",
		},
		{
			name: "list_print_door_empty_dict_call",
			src:  "def f(v):\n    print(v)\n\nxs = [dict()]\nf(xs[0])\n",
			want: "{}\n",
		},
		// `str` — the door that renders a value as text.
		{
			name: "dict_str_door_empty_list_call",
			src:  "def f(v):\n    print(str(v))\n\nd = {\"k\": list()}\nf(d[\"k\"])\n",
			want: "[]\n",
		},
		{
			name: "list_str_door_empty_list_call",
			src:  "def f(v):\n    print(str(v))\n\nxs = [list()]\nf(xs[0])\n",
			want: "[]\n",
		},
		{
			name: "dict_str_door_empty_set_call",
			src:  "def f(v):\n    print(str(v))\n\nd = {\"k\": set()}\nf(d[\"k\"])\n",
			want: "set()\n",
		},
		{
			name: "dict_str_door_empty_dict_call",
			src:  "def f(v):\n    print(str(v))\n\nd = {\"k\": dict()}\nf(d[\"k\"])\n",
			want: "{}\n",
		},
		// The truth door ADR 0393 built, fed by the entry this row finally lets cross.
		{
			name: "dict_truth_empty_list_call",
			src:  "def f(v):\n    if v:\n        print(\"t\")\n    else:\n        print(\"f\")\n\nd = {\"k\": list()}\nf(d[\"k\"])\n",
			want: "f\n",
		},
		{
			name: "list_truth_empty_list_call",
			src:  "def f(v):\n    if v:\n        print(\"t\")\n    else:\n        print(\"f\")\n\nxs = [list()]\nf(xs[0])\n",
			want: "f\n",
		},
		{
			name: "dict_truth_empty_set_call",
			src:  "def f(v):\n    if v:\n        print(\"t\")\n    else:\n        print(\"f\")\n\nd = {\"k\": set()}\nf(d[\"k\"])\n",
			want: "f\n",
		},
		{
			name: "dict_truth_empty_dict_call",
			src:  "def f(v):\n    if v:\n        print(\"t\")\n    else:\n        print(\"f\")\n\nd = {\"k\": dict()}\nf(d[\"k\"])\n",
			want: "f\n",
		},
		// The nested spellings: the scan's question is asked at every depth, and a nested dict's KEYS stay
		// the strict question because a member the tag table cannot name is a key that cannot be hashed.
		{
			name: "nested_list_element",
			src:  "def f(v):\n    print(v)\n\nxs = [[list()], [1, 2]]\nf(xs[0])\nf(xs[1])\n",
			want: "[[]]\n[1, 2]\n",
		},
		{
			name: "nested_dict_value",
			src:  "def f(v):\n    print(v)\n\nd = {\"k\": {\"m\": set()}}\nf(d[\"k\"])\n",
			want: "{'m': set()}\n",
		},
		{
			name: "nested_list_in_dict",
			src:  "def f(v):\n    print(v)\n\nd = {\"k\": [list()]}\nf(d[\"k\"])\n",
			want: "[[]]\n",
		},
		// One dict, all three spellings, and the ordinary entries beside them: the row's own claim is that
		// one entry's spelling no longer decides the answer for the whole literal.
		{
			name: "three_spellings_one_dict",
			src:  "def f(v):\n    print(v)\n    if v:\n        print(\"t\")\n    else:\n        print(\"f\")\n\nd = {\"a\": list(), \"b\": set(), \"c\": dict()}\nf(d[\"a\"])\nf(d[\"b\"])\nf(d[\"c\"])\n",
			want: "[]\nf\nset()\nf\n{}\nf\n",
		},
		{
			name: "list_of_the_three_calls",
			src:  "def f(v):\n    print(v)\n\nxs = [list(), 5, set(), \"hi\"]\nprint(xs)\nf(xs[0])\nf(xs[2])\nf(xs[1])\nf(xs[3])\n",
			want: "[[], 5, set(), 'hi']\n[]\nset()\n5\nhi\n",
		},
		{
			name: "empty_call_amid_ordinary_entries",
			src:  "def f(v):\n    print(v)\n\nd = {\"a\": list(), \"b\": 5, \"c\": \"hi\", \"d\": 1.5, \"e\": None, \"g\": [1]}\nprint(d)\nf(d[\"b\"])\nf(d[\"c\"])\nf(d[\"d\"])\nf(d[\"e\"])\nf(d[\"g\"])\n",
			want: "{'a': [], 'b': 5, 'c': 'hi', 'd': 1.5, 'e': None, 'g': [1]}\n5\nhi\n1.5\nNone\n[1]\n",
		},
		{
			name: "filled_by_the_call_spelling",
			src:  "def f(v):\n    print(v)\n\nxs = [list()]\nxs.append(set())\nprint(xs)\nf(xs[0])\nf(xs[1])\n",
			want: "[[], set()]\n[]\nset()\n",
		},
		// The roads that already answered this value on their own keep answering it: a binding and a direct
		// argument never needed the scan, and a run-time store writes the tag when it stores.
		{
			name: "bound_then_crossed",
			src:  "def f(v):\n    print(v)\n\nw = list()\nf(w)\n",
			want: "[]\n",
		},
		{
			name: "crossed_directly",
			src:  "def f(v):\n    print(v)\n\nf(list())\n",
			want: "[]\n",
		},
		{
			name: "run_built_entry",
			src:  "def f(v):\n    print(v)\n    if v:\n        print(\"t\")\n    else:\n        print(\"f\")\n\nd = {}\nd[\"k\"] = list()\nf(d[\"k\"])\n",
			want: "[]\nf\n",
		},
		{
			name: "loop_written_entry",
			src:  "def f(v):\n    print(v)\n\nd = {}\nfor i in [0, 1]:\n    d[i] = list()\nprint(d)\nf(d[0])\n",
			want: "{0: [], 1: []}\n[]\n",
		},
		{
			name: "entry_read_by_a_loop_variable_key",
			src:  "def f(v):\n    print(v)\n\nd = {\"a\": list(), \"b\": 5, \"c\": \"hi\"}\nfor k in [\"a\", \"b\", \"c\"]:\n    print(d[k])\n",
			want: "[]\n5\nhi\n",
		},
		{
			name: "two_arguments_one_pair",
			src:  "def f(v, w):\n    print(v, w)\n\nd = {\"k\": list(), \"j\": 5}\nf(d[\"k\"], d[\"j\"])\n",
			want: "[] 5\n",
		},
	}
}

// TestTheEmptyContainerWrittenAsACallCrossesTheArgument is the row itself on the compiled leg.
func TestTheEmptyContainerWrittenAsACallCrossesTheArgument(t *testing.T) {
	t.Parallel()
	for _, tc := range emptyContainerArgumentAnswerRows() {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := compiledOut(t, tc.src); got != tc.want {
				t.Errorf("printed %q, want %q — the read of the entry crossed as one word\nsrc: %s", got, tc.want, tc.src)
			}
		})
	}
}

// TestTheEmptyContainerCallArgumentAgreesWithTheRecord is the record leg: every answering program is on the
// record with the reference's answer beside it, and a ladder row whose record went missing fails here rather
// than being witnessed by one leg only.
func TestTheEmptyContainerCallArgumentAgreesWithTheRecord(t *testing.T) {
	t.Parallel()
	for _, tc := range emptyContainerArgumentAnswerRows() {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if !HasGoldenAnswer(tc.src) {
				t.Fatalf("the record does not cover %s — add it with tools/recmerge (it is answered on the reference leg)", tc.src)
			}
			rec, err := runGoldenStdout(t, tc.src)
			if err != nil {
				t.Fatalf("the record leg did not answer %s: %v", tc.src, err)
			}
			if rec != tc.want {
				t.Errorf("the record prints %q, the reference prints %q", rec, tc.want)
			}
			if got := compiledOut(t, tc.src); got != rec {
				t.Errorf("the compiled leg prints %q, the record prints %q for %s", got, rec, tc.src)
			}
		})
	}
}

// TestTheEmptyContainerCallArgumentIsWrittenWithItsKind asks the module rather than the answer. The entry is
// stored by the tagged put with the tag of the container the CALL spells — 5 for a list, 6 for a dict, 7 for
// a set, the words of ADR 0182's one table — and the callee receives the second word and asks the objects'
// own doors with it. A gate that only ever produced the right answer by luck would pass the tables above on
// a program whose first heap slot happens to be free; these rows cannot.
func TestTheEmptyContainerCallArgumentIsWrittenWithItsKind(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name, src, store, wantTag string
	}{
		{
			name:    "a_list_spelling_stores_the_lists_tag",
			src:     "def f(v):\n    print(v)\n\nd = {\"k\": list()}\nf(d[\"k\"])\n",
			store:   "call void @rt_dict_put_tagged(i32 %",
			wantTag: ", i32 4, i32 5)",
		},
		{
			name:    "a_dict_spelling_stores_the_dicts_tag",
			src:     "def f(v):\n    print(v)\n\nd = {\"k\": dict()}\nf(d[\"k\"])\n",
			store:   "call void @rt_dict_put_tagged(i32 %",
			wantTag: ", i32 4, i32 6)",
		},
		{
			name:    "a_set_spelling_stores_the_sets_tag",
			src:     "def f(v):\n    print(v)\n\nd = {\"k\": set()}\nf(d[\"k\"])\n",
			store:   "call void @rt_dict_put_tagged(i32 %",
			wantTag: ", i32 4, i32 7)",
		},
	} {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			body := mainRegion(t, tc.src)
			if !storeLineCarryingTag(body, tc.store, tc.wantTag) {
				t.Errorf("the entry was not stored with the tag of the container the call spells (%s%s):\n%s", tc.store, tc.wantTag, body)
			}
		})
	}
	// The two halves of one crossing, read where each half is emitted. The CALLER's `@main` is where the read
	// of the slot is lowered, so the tagged get and the tagged tag-read belong to it; the CALLEE is where the
	// second word is spent, so the pair-parameter signature, the tag alloca and the door that asks the object
	// belong to it. Asking a callee for the caller's call — or `@main` for the callee's — reports a missing
	// ask that is only in the other region.
	for _, tc := range []struct {
		name, src  string
		calleeMust []string
		callerMust []string
	}{
		{
			name:       "the_print_position",
			src:        "def f(v):\n    print(v)\n\nd = {\"k\": list()}\nf(d[\"k\"])\n",
			calleeMust: []string{"define i32 @gy_f(i32 %p0, i32 %q0)", "%_v_tag = alloca i32", "@rt_print_mixed_value"},
			callerMust: []string{"@rt_dict_get_tagged", "@rt_dict_value_tag"},
		},
		{
			name:       "the_truth_position",
			src:        "def f(v):\n    if v:\n        print(\"t\")\n    else:\n        print(\"f\")\n\nd = {\"k\": set()}\nf(d[\"k\"])\n",
			calleeMust: []string{"define i32 @gy_f(i32 %p0, i32 %q0)", "%_v_tag = alloca i32", "@rt_pair_truth"},
			callerMust: []string{"@rt_dict_get_tagged", "@rt_dict_value_tag"},
		},
	} {
		tc := tc
		t.Run("callee_"+tc.name, func(t *testing.T) {
			t.Parallel()
			body := functionRegion(t, tc.src, "f")
			for _, want := range tc.calleeMust {
				if !strings.Contains(body, want) {
					t.Errorf("%s: the callee never reached %s:\n%s", tc.name, want, body)
				}
			}
			caller := mainRegion(t, tc.src)
			for _, want := range tc.callerMust {
				if !strings.Contains(caller, want) {
					t.Errorf("%s: the caller never asked %s:\n%s", tc.name, want, caller)
				}
			}
		})
	}
}

// storeLineCarryingTag reads the program's own `@main` for a store call that ends with the tag the door
// wrote: the runtime block carries `@rt_dict_put_tagged` as a definition of its own, so a claim about what
// the program ASKED has to be read off the statement, one line at a time.
func storeLineCarryingTag(body, store, wantTag string) bool {
	for _, line := range strings.Split(body, "\n") {
		if strings.Contains(line, store) && strings.Contains(line, wantTag) {
			return true
		}
	}
	return false
}

// TestTheEmptyContainerArgumentRoadDeclinesWhatItCannotPromise asks the door the question the door exists to
// ask: a program that takes the name `list`, `set` or `dict` for itself does not have an empty container at
// that spelling, and this pass must not label whatever the program's own function hands back with a kind.
// The three controls below are answered with the program's own value — a text's payload, an integer, a
// number — and each asserts that the module grew no pair road at all: no second parameter word, no
// `@rt_dict_get_tagged`, because a decline that still emits the pair road is a half-open door.
func TestTheEmptyContainerArgumentRoadDeclinesWhatItCannotPromise(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name, src, want string
	}{
		{
			name: "a_program_that_defines_set_owns_that_spelling",
			src:  "def set():\n    return 1\n\ndef f(v):\n    print(v)\n    if v:\n        print(\"t\")\n    else:\n        print(\"f\")\n\nd = {\"k\": set()}\nprint(d)\nprint(d[\"k\"])\nf(d[\"k\"])\n",
			want: "{'k': 1}\n1\n1\nt\n",
		},
		{
			// The name a scan must NOT treat as taken: a parameter of an unrelated function is that body's
			// own binding, and the emitter registers no `def` under that name — a gate stricter than the
			// door it copies would close this family's parameter for a spelling the store keeps labelling.
			name: "an_unrelated_parameter_named_list_does_not_take_the_name",
			src:  "def g(list):\n    return list\n\ny = g(4)\n\ndef f(v):\n    print(v)\n\nd = {\"k\": list()}\nf(d[\"k\"])\n",
			want: "[]\n",
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
	t.Run("a_decline_emits_no_pair_road", func(t *testing.T) {
		t.Parallel()
		src := "def set():\n    return 1\n\ndef f(v):\n    print(v)\n\nd = {\"k\": set()}\nf(d[\"k\"])\n"
		body := functionRegion(t, src, "f")
		if strings.Contains(body, "%_v_tag") || strings.Contains(body, "@rt_dict_get_tagged") {
			t.Errorf("the door declined yet the callee still carries the pair road:\n%s", body)
		}
	})
	t.Run("an_entry_whose_payload_is_its_number_crosses_one_word", func(t *testing.T) {
		t.Parallel()
		// The row's negative control, restated for this door: `{"a": 7}` has a payload that IS its number,
		// and opening it for the pair would trade a working road for a slower one that can drift.
		src := "def f(v):\n    if v:\n        print(\"t\")\n    else:\n        print(\"f\")\n\nd = {\"a\": 7}\nf(d[\"a\"])\n"
		body := functionRegion(t, src, "f")
		if strings.Contains(body, "%_v_tag") || strings.Contains(body, "@rt_pair_truth") {
			t.Errorf("a provable-integer entry was opened for the pair:\n%s", body)
		}
		if !strings.Contains(body, "icmp ne i32 %p0, 0") && !strings.Contains(body, "icmp ne i32 %") {
			t.Errorf("the ordinary road's compare disappeared from a program that keeps it:\n%s", body)
		}
	})
}

// TestWhatTheEmptyContainerCallArgumentStillOwesIsFiledNotFixed is the residue, measured against a build of
// the commit before this row on every shape in the family: 51 probes, 45 of which now answer the reference
// and none of which regressed. What remains is not this row's answer — each is a different door with the same
// missing word, and each is named rather than smoothed over.
func TestWhatTheEmptyContainerCallArgumentStillOwesIsFiledNotFixed(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name, src, today, wantSub, row string
	}{
		{
			// Gap R.370: the road that decides the truth of an empty container spelled as a CALL still tests
			// the HANDLE against zero, so the first container in a program is false by luck and every later
			// one is true. The literal spelling of the same question refuses with the container-truthiness
			// sentence, which is how this stayed invisible.
			name:  "the_truth_of_the_second_empty_container_call_is_its_handle",
			src:   "print(bool(list()), bool(set()), bool(dict()))\n",
			today: "False True True\n",
			row:   "Gap R.370",
		},
		{
			name:  "not_of_the_second_empty_container_call",
			src:   "print(not list(), not list())\n",
			today: "True False\n",
			row:   "Gap R.370",
		},
		{
			// Gap R.371: the value of a caller-supplied parameter, as opposed to its truth. ADR 0389 carried
			// both words and ADR 0393 spends the tag on the condition; the doors that RENDER or MEASURE the
			// value still read the payload, or refuse naming the tagged value word.
			name:  "the_repr_of_a_caller_supplied_parameter",
			src:   "def f(v):\n    print(repr(v))\n\nd = {\"k\": list()}\nf(d[\"k\"])\n",
			today: "3\n",
			row:   "Gap R.371",
		},
		{
			name:  "an_f_string_field_that_is_a_caller_supplied_parameter",
			src:   "def f(v):\n    print(f\"{v}\")\n\nd = {\"k\": list()}\nf(d[\"k\"])\n",
			today: "3\n",
			row:   "Gap R.371",
		},
		{
			name:  "a_container_slot_written_from_a_caller_supplied_parameter",
			src:   "def f(v):\n    xs = [v]\n    print(xs)\n\nd = {\"k\": list()}\nf(d[\"k\"])\n",
			today: "[3]\n",
			row:   "Gap R.371",
		},
		{
			name:    "len_of_a_caller_supplied_parameter_refuses",
			src:     "def f(v):\n    print(len(v))\n\nd = {\"k\": list()}\nf(d[\"k\"])\n",
			wantSub: "len of a non-string variable",
			row:     "Gap R.371",
		},
		{
			name:    "a_comparison_against_a_caller_supplied_parameter_refuses",
			src:     "def f(v):\n    print(v == [])\n\nd = {\"k\": list()}\nf(d[\"k\"])\n",
			wantSub: "needs a tagged value",
			row:     "Gap R.371",
		},
		{
			// Gap R.372: a `for` variable is a binding the scan never records, so the KEY of the read has no
			// kind to be asked with and the whole read stays on the ordinary road. The same dict, printed with
			// the same loop variable, prints right — the print door asks the object and never consults the scan.
			name:  "an_entry_read_by_a_loop_variable_key_crosses_as_a_number",
			src:   "def f(v):\n    print(v)\n\nd = {\"a\": [], \"b\": list(), \"c\": \"hi\", \"d\": 1.5}\nfor k in [\"a\", \"b\", \"c\", \"d\"]:\n    f(d[k])\n",
			today: "5\n6\n3\n7\n",
			row:   "Gap R.372",
		},
		{
			// Gap R.81, seen from this row's spelling: a member the builder can name has no hashing gate.
			name:  "an_unhashable_member_written_by_the_call_spelling_is_added",
			src:   "s = {list()}\nprint(s)\n",
			today: "{[]}\n",
			row:   "Gap R.81",
		},
		{
			// Gap R.373, the face that answers: a method's name is not a module binding, and the reference
			// still resolves `set()` to the built-in. Both this scan and the door it copies register every
			// `def` the program wrote, methods included, so the entry is stored with no container tag at all —
			// and the answer is the handle, at the exit code of success.
			name:  "a_method_named_set_takes_the_builtins_spelling",
			src:   "class C:\n    def set(self):\n        return 2\n\ndef f(v):\n    print(v)\n\nd = {\"k\": set()}\nf(d[\"k\"])\n",
			today: "3\n",
			row:   "Gap R.373",
		},
		{
			// Gap R.374, measured against the parent build and NOT caused by this row: a callee's pair layout
			// is decided from one call site and reused by every other, so a second call that fills the same
			// slot with a plain integer hands the callee a word from the previous call. `f(list(), 5)` /
			// `f(1, 2)` prints `[] 2` — the pair word the first call wrote is still lying in the slot. The
			// same shape spelled as a literal (`f([1], 2)` / `f(3, 4)`) prints `[] 4` on the build before this
			// row too, which is how it stays out of this row's charge while still being named here.
			name:  "a_second_call_to_the_same_callee_reuses_the_first_calls_pair_word",
			src:   "def f(a, b):\n    print(a, b)\n\nf(list(), 5)\nf(1, 2)\n",
			today: "[] 5\n[] 2\n",
			row:   "Gap R.374",
		},
		{
			// The same layout drift with all three spellings: only the LAST call's containers survive, so
			// every earlier argument crosses as whatever the tag slot last held (Gap R.374).
			name:  "three_calls_three_spellings_one_layout",
			src:   "def f(a, b):\n    print(a, b)\n\nf(list(), 1)\nf(set(), 2)\nf(dict(), 3)\n",
			today: "{} 1\n{} 2\n{} 3\n",
			row:   "Gap R.374",
		},
	} {
		tc := tc
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
			if got := compiledOut(t, tc.src); got != tc.today {
				t.Errorf("%s printed %q; this row pinned %q — the shape changed, DELETE or rewrite the pin (%s)\nsrc: %s", tc.name, got, tc.today, tc.row, tc.src)
			}
		})
	}
}
