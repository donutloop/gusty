package lang

import (
	"strings"
	"testing"
)

// empty_container_tag_test.go — an empty container written as a CALL is the object its literal spells,
// and every door that asks "what kind is this slot?" has to be told so (roadmap Gap R.360, ADR 0391).
//
// `d = {None: set()}` / `print(d[None])` wrote `3` — the heap handle — where the reference writes `set()`.
// `print(bool({None: set()}[None]))` wrote `True`, `print([bool(x) for x in [set()]])` wrote `[True]`, and
// `print([set(), list(), dict()])` wrote `[1, 2, 3]`: three handles, one line apart from each other and
// none of them a value the program wrote. No diagnostic, exit 0.
//
// The tag table has had a set in it since the beginning (`value.go`'s `TagSet`), and ADR 0388 gave the
// condition the pair table it consults. What was missing was not a runtime helper but a LABEL: the roads
// that write a slot ask `elemKindTag` what the value in that slot is, and `set()`, `list()` and `dict()` —
// being calls, not literals — fell through every spelling-based case to the default answer, "an integer".
// The handle went in beside the tag for an integer, and from then on the printer, the truth table, `len`
// and the comparison all believed the label.
//
// The gates that DECIDE whether a value is a container (`taggableNestedElem`, `foldArgCarriesAContainer`,
// the print gate) already asked `emptyContainerLiteral`. The roads that LABEL it did not — one question
// asked in two places, drifting (ADR 0232). This file asserts the answer, the label, and the doors the
// label still does not reach.

// emptyContainerSlotPrograms is the answer ladder: every position an empty container spelled as a call can
// be written into, and the value the reference gives back for it.
func emptyContainerSlotPrograms() []struct {
	name, src, want string
} {
	return []struct {
		name, src, want string
	}{
		// A slot of a list literal.
		{"list_slot_set", "xs = [set()]\nprint(xs[0])\n", "set()\n"},
		{"list_slot_list", "xs = [list()]\nprint(xs[0])\n", "[]\n"},
		{"list_slot_dict", "xs = [dict()]\nprint(xs[0])\n", "{}\n"},
		// A slot of a dict literal, read back by its key.
		{"dict_entry_set", "d = {None: set()}\nprint(d[None])\n", "set()\n"},
		{"dict_entry_list", "d = {None: list()}\nprint(d[None])\n", "[]\n"},
		{"dict_entry_dict", "d = {None: dict()}\nprint(d[None])\n", "{}\n"},
		// Printed whole, without a binding in between: the road that printed three handles in a row.
		{"literal_whole_three", "print([set(), list(), dict()])\n", "[set(), [], {}]\n"},
		{"literal_among_other_kinds", "print([1, list(), None, \"a\", {1: 2}])\n", "[1, [], None, 'a', {1: 2}]\n"},
		{"bound_mixed_list", "xs = [set(), 1]\nprint(xs)\n", "[set(), 1]\n"},
		{"dict_value_whole", "print({\"k\": set()})\n", "{'k': set()}\n"},
		// Built by the program rather than written as a literal.
		{"append_empty_set", "xs = []\nxs.append(set())\nprint(xs)\n", "[set()]\n"},
		{"append_empty_list_read_back", "xs = []\nxs.append(list())\nprint(xs[0])\n", "[]\n"},
		{"assign_entry_set", "d = {}\nd[\"k\"] = set()\nprint(d)\n", "{'k': set()}\n"},
		// The truth question ADR 0388 opened and the set never reached.
		{"truth_of_a_read_slot", "print(bool([set()][0]))\n", "False\n"},
		{"truth_of_comprehension_items", "print([bool(x) for x in [set(), list()]])\n", "[False, False]\n"},
		{"condition_arm", "d = {None: set()}\nif d[None]:\n    print(\"t\")\nelse:\n    print(\"f\")\n", "f\n"},
		{"ternary_test", "d = {None: set()}\nprint(\"y\" if d[None] else \"n\")\n", "n\n"},
		{"filter_keeps_empties", "print([x for x in [set(), list()] if not x])\n", "[set(), []]\n"},
		{"filter_drops_empties", "print([x for x in [set(), list()] if x])\n", "[]\n"},
		// A loop, and the count the object gives of itself.
		{"loop_over_the_slot", "xs = [set()]\nfor v in xs:\n    print(v)\n", "set()\n"},
		{"len_of_a_set_slot", "xs = [set()]\nprint(len(xs[0]))\n", "0\n"},
		{"len_of_a_list_slot", "xs = [list()]\nprint(len(xs[0]))\n", "0\n"},
		{"len_of_a_dict_slot", "xs = [dict()]\nprint(len(xs[0]))\n", "0\n"},
		// Equality, which used to compare a handle with a handle.
		{"two_built_lists_of_sets", "print([set()] == [set()])\n", "True\n"},
		{"slot_equals_the_spelling", "xs = [set()]\nprint(xs[0] == set())\n", "True\n"},
		{"bound_list_equals_literal", "xs = [set()]\nprint(xs == [set()])\n", "True\n"},
		// Deeper: an empty container inside a container inside a dict.
		{"nested_literal", "print([[set()], [list()]])\n", "[[set()], [[]]]\n"},
		{"dict_of_list_of_set", "print({\"a\": [set()]})\n", "{'a': [set()]}\n"},
		{"dict_mixed_values", "d = {1: [set()], 2: 5}\nprint(d)\n", "{1: [set()], 2: 5}\n"},
		// The rendering roads and a binding between the slot and the print.
		{"bound_from_a_slot", "d = {\"a\": set()}\ns = d[\"a\"]\nprint(s)\n", "set()\n"},
		{"str_of_a_slot", "d = {\"a\": set()}\nprint(str(d[\"a\"]))\n", "set()\n"},
		{"repr_of_a_slot", "d = {\"a\": set()}\nprint(repr(d[\"a\"]))\n", "set()\n"},
		// A name that was built first and stored afterwards already had its kind; it keeps it.
		{"name_then_slot", "s = set()\nxs = [s]\nprint(xs[0])\n", "set()\n"},
		// The comprehension roads, one per container built.
		{"comprehension_of_items", "print([x for x in [set()]])\n", "[set()]\n"},
		{"comprehension_of_lists", "print([[x] for x in [set(), list()]])\n", "[[set()], [[]]]\n"},
	}
}

func TestAnEmptyContainerWrittenAsACallAnswersItsOwnKind(t *testing.T) {
	t.Parallel()
	for _, tc := range emptyContainerSlotPrograms() {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if _, err := Compile(tc.src); err != nil {
				t.Fatalf("refused: %v", err)
			}
			if got := compiledOut(t, tc.src); got != tc.want {
				t.Fatalf("compiled = %q, want %q — the slot was labelled by its spelling, not by its kind", got, tc.want)
			}
			if got, _, err := PythonRun(tc.src); err != nil || got != tc.want {
				t.Fatalf("reference = %q (%v), want %q", got, err, tc.want)
			}
		})
	}
}

// The record leg: the same sources, judged against the answer on record. A source the record does not
// cover is a hard failure, not a skip — the record leg is what makes "the compiled answer changed toward
// the reference rather than away from it" a checkable claim (ADR 0302's witness rule).
func TestTheEmptyContainerSlotProgramsAreOnTheRecord(t *testing.T) {
	asked := 0
	for _, tc := range emptyContainerSlotPrograms() {
		t.Run(tc.name, func(t *testing.T) {
			if !HasGoldenAnswer(tc.src) {
				t.Fatalf("%q is not on the record — record it from the reference with tools/recmerge, never from this backend", tc.src)
			}
			asked++
			want := strings.TrimSpace(goldenStdout(silentReporter{}, tc.src))
			if got := strings.TrimSpace(compiledRunIsTheBackend(t, tc.src)); got != want {
				t.Fatalf("compiled=%q record=%q", got, want)
			}
		})
	}
	if asked == 0 {
		t.Fatal("not one of this row's programs is on the record: the record leg is not being walked")
	}
}

// The label itself, read off the module. A handle stored beside a tag the compiler invented is exactly the
// wrong answer this row is about, so the claim is written in the emitted language: which tag word reaches
// which store, and which store the program still does not need.
func TestAnEmptyContainerSlotWritesItsKindBesideItsHandle(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name, src, must, mustNot string
	}{
		{
			name: "a list slot tags its member 7 — the set",
			src:  "xs = [set()]\nprint(xs[0])\n",
			must: "call void @rt_tag_elem(i32 %h1, i32 0, i32 7)",
			// The self-describing bit: without it the printer never reads the slot tag at all.
			mustNot: "@.lst",
		},
		{
			name: "a dict entry takes the value's set tag",
			src:  "d = {None: set()}\nprint(d[None])\n",
			must: "call void @rt_dict_put_tagged(i32 %t1, i32 0, i32 %h1, i32 3, i32 7)",
			// Not the untagged put, which is the road that cannot carry the second word.
			mustNot: "call void @rt_dict_put(",
		},
		{
			name:    "an append of an empty list tags 5 — the list",
			src:     "xs = []\nxss = [xs]\nxss.append(list())\nprint(xss)\n",
			must:    "call void @rt_append_tagged(i32 %",
			mustNot: "",
		},
		{
			name:    "a dict value that is an empty dict tags 6",
			src:     "print({\"k\": dict()})\n",
			must:    "call void @rt_dict_put_tagged(i32 %t1, i32 %t3, i32 %h1, i32 4, i32 6)",
			mustNot: "call void @rt_dict_put(",
		},
		{
			name: "the truth of an empty container item asks the pair table with the set's tag",
			src:  "print([bool(x) for x in [set()]])\n",
			must: "call i32 @rt_pair_truth(i32 %_x.ld1, i32 7)",
			// …and the answer is printed through the bool door, not the number door.
			mustNot: "",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			body := mainRegion(t, tc.src)
			if !strings.Contains(body, tc.must) {
				t.Fatalf("%s: the module does not carry %q:\n%s", tc.name, tc.must, body)
			}
			if tc.mustNot != "" && strings.Contains(body, tc.mustNot) {
				t.Fatalf("%s: the module still carries %q:\n%s", tc.name, tc.mustNot, body)
			}
		})
	}
}

// The negative controls. An integer slot never needed the second word, an all-integer list still folds to
// the static array, and a program that defines `set` itself keeps the answer its own definition gives —
// the tag is read off the object, and a call to a user's `set` has no object behind it at all.
func TestTheEmptyContainerTagIsWrittenOnlyWhereTheKindIsTheObjects(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name, src, want string
	}{
		{"int_list_still_folds", "print([1, 2])\n", "[1, 2]\n"},
		{"int_slot_still_reads", "xs = [1, 2]\nprint(xs[0])\n", "1\n"},
		{"int_dict_entry", "d = {1: 5}\nprint(d[1])\n", "5\n"},
		{"empty_set_prints_whole", "print(set())\n", "set()\n"},
		{"empty_list_prints_whole", "print(list())\n", "[]\n"},
		{"set_members_still_print", "print({1, 2})\n", "{1, 2}\n"},
		{"list_still_prints", "print([1, \"a\", None])\n", "[1, 'a', None]\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := compiledOut(t, tc.src); got != tc.want {
				t.Fatalf("compiled = %q, want %q", got, tc.want)
			}
		})
	}
	for _, tc := range []struct {
		name, src, absent string
	}{
		{
			name:   "an integer slot asks for no second word",
			src:    "d = {1: 5}\nprint(d[1])\n",
			absent: "call void @rt_dict_put_tagged(",
		},
		{
			name:   "an all-integer literal still folds to the static array",
			src:    "print([1, 2])\n",
			absent: "call void @rt_append_tagged(",
		},
		{
			name:   "a program that defines set itself does not get a set's tag for it",
			src:    "def set():\n    return 1\n\nxs = [set()]\nprint(xs)\n",
			absent: "i32 7",
		},
	} {
		t.Run("absent_"+tc.name, func(t *testing.T) {
			t.Parallel()
			body := mainRegion(t, tc.src)
			if strings.Contains(body, tc.absent) {
				t.Fatalf("%s: the module carries %q:\n%s", tc.name, tc.absent, body)
			}
		})
	}
	// The shadowed spelling answers what the program's own definition returns, not what the builtin would.
	const shadowed = "def set():\n    return 1\n\nxs = [set()]\nprint(xs)\n"
	if got := compiledOut(t, shadowed); got != "[1]\n" {
		t.Fatalf("a shadowing `set` printed %q, want \"[1]\\n\" — the program's definition wins (Gap R.6, ADR 0199)", got)
	}
}

// A dict key written as an empty container is refused the way the same key written as a literal is
// refused: CPython raises `TypeError: unhashable type: 'set'`, and this backend's answer to a container
// key — literal or call — is one honest refusal naming the shape (roadmap Gap R.81 owns the general
// hashing rule; ADR 0310 raises from a tag where the pair, not the spelling, is the source).
func TestADictKeyWrittenAsAnEmptyContainerRefusesTheWayTheLiteralDoes(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name, src, want string
	}{
		{"set_key", "print({set(): 1})\n", "cannot hold an empty set"},
		{"list_key", "print({list(): 1})\n", "cannot hold an empty list"},
		{"dict_key", "print({dict(): 1})\n", "cannot hold an empty dict"},
		{"set_key_in_a_bound_dict", "d = {set(): 1}\nprint(d)\n", "cannot hold an empty set"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			_, err := Compile(tc.src)
			if err == nil {
				t.Fatalf("%s compiled; it must refuse with %q", tc.src, tc.want)
			}
			msg := err.Error()
			if !strings.Contains(msg, tc.want) {
				t.Fatalf("%s refused with %q, want it to contain %q", tc.src, msg, tc.want)
			}
			// A refusal that leaks Go where the source belongs is its own defect (Gap R.249's rule).
			for _, leak := range []string{"*lang.", "%!s(", "0x"} {
				if strings.Contains(msg, leak) {
					t.Fatalf("%s: the refusal leaks %q: %s", tc.src, leak, msg)
				}
			}
		})
	}
}

// Filed, not fixed: the doors that still decide an empty container's kind from its SPELLING rather than
// from the object. The label landed here reaches the roads that STORE a value; these are the roads that
// DECIDE — `staticContainerKind` and the `len`, comparison and `abs` gates that consult it — and each
// still answers as it did before this row, with the reference's answer written beside it (roadmap
// Gap R.364, beside Gap I.2 for a method on a slot and Gap R.67 for a one-word position).
func TestWhatTheEmptyContainerCallStillCannotBeIsFiledNotFixed(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name, src, actual, row string
		refuse                 bool
	}{
		{
			name:   "a method on a slot that holds an empty container",
			src:    "xs = [list()]\nxs[0].append(3)\nprint(xs)\n",
			actual: "receiver that is not a text the compiler can read",
			refuse: true,
			row:    "Gap I.2",
		},
		{
			name:   "str of a comprehension variable over empty containers",
			src:    "print([str(x) for x in [set(), list()]])\n",
			actual: "str(x) is refused",
			refuse: true,
			row:    "Gap R.364",
		},
		{
			name:   "a container-answering call as a list element",
			src:    "def f():\n    return set()\n\nprint([f()])\n",
			actual: "hands back a container",
			refuse: true,
			row:    "Gap R.67",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			_, err := Compile(tc.src)
			if err == nil || !strings.Contains(err.Error(), tc.actual) {
				t.Fatalf("%s: today's refusal is %q and the row that owns the change is %s", tc.src, err, tc.row)
			}
		})
	}
	// An answer that is not a refusal and not right: exit 0, a value, and no diagnostic. Pinned as today's
	// answer with the reference beside it, so the day the door changes this row fails — which is how a filed
	// row stops being a permanent second truth (ADR 0343's rule). The two this table used to carry —
	// `set() == set()` answering `False` and `abs(list())` answering `0` — are DELETED, as their own text
	// required: ADR 0392 gives the deciding doors the same door the store roads ask, and they answer
	// (`pkg/lang/empty_container_decide_test.go`).
	for _, tc := range []struct {
		name, src, actual, reference, row string
	}{
		{
			name:      "a set whose member is an empty set",
			src:       "print({set()})\n",
			actual:    "{set()}\n",
			reference: "raises TypeError: unhashable type: 'set'\n",
			row:       "Gap R.81",
		},
	} {
		t.Run("answer_"+tc.name, func(t *testing.T) {
			t.Parallel()
			if got := compiledOut(t, tc.src); got != tc.actual {
				t.Fatalf("%s printed %q, want today's %q — the row that owns it is %s (the reference: %s)",
					tc.src, got, tc.actual, tc.row, tc.reference)
			}
		})
	}
}
