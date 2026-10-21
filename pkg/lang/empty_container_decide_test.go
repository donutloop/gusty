package lang

import (
	"strings"
	"testing"
)

// empty_container_decide_test.go — the doors that DECIDE an empty container's kind ask the same door the
// roads that STORE it ask (roadmap Gap R.364, ADR 0392).
//
// ADR 0391 wrote the set's, list's and dict's tag wherever a value was stored. Six shapes kept answering
// anyway, because a different pair of doors — `staticContainerKind` ("what would the handle this expression
// denotes be, if the compiler is willing to say") and `containerHandleOf` ("then build it") — meets before
// anything is stored and asks the SPELLING:
//
//	print(len(set()))      # 0 in the reference; here: "len requires an inline list/dict/set literal"
//	print(list() == [])    # True in the reference; here: "comparing a container with an empty list needs a tagged value"
//	print(set() == set())  # True in the reference; here: False, at exit 0, no diagnostic
//	print(abs(list()))     # TypeError: bad operand type for abs(): 'list'; here: 0, at exit 0
//
// The literal spellings answer all four, `len(xs)` over `xs = set()` answers, and `abs([1])` raises the
// reference's sentence word for word — so the value was never the problem, the three ways of spelling it
// were. This file asserts the answer at each door, the raise with its class and its sentence, and the
// controls that keep the doors shut where the reference has no answer either.

// emptyContainerDecidePrograms are the answers, measured off `python3` before this file was written.
func emptyContainerDecidePrograms() []struct{ name, src, want string } {
	return []struct{ name, src, want string }{
		// `len` — the door that refused outright.
		{"len_of_an_empty_list_call", "print(len(list()))\n", "0\n"},
		{"len_of_an_empty_set_call", "print(len(set()))\n", "0\n"},
		{"len_of_an_empty_dict_call", "print(len(dict()))\n", "0\n"},
		{"len_of_two_empty_calls", "print(len(list()) + len(set()))\n", "0\n"},
		{"len_after_a_fill", "s = set()\ns.add(1)\nprint(len(s))\n", "1\n"},
		// The comparison door, which had a road for a container and no way to see this one.
		{"empty_list_equals_literal", "print(list() == [])\n", "True\n"},
		{"literal_equals_empty_list", "print([] == list())\n", "True\n"},
		{"two_empty_sets_are_equal", "print(set() == set())\n", "True\n"},
		{"empty_dict_equals_literal", "print(dict() == {})\n", "True\n"},
		{"inequality_answers_the_other_way", "print(list() != [])\n", "False\n"},
		{"empty_set_is_not_a_filled_one", "print(set() == {1})\n", "False\n"},
		{"empty_list_is_not_a_filled_one", "print(list() == [1])\n", "False\n"},
		{"two_built_lists_of_empty_sets", "print([set()] == [set()])\n", "True\n"},
		{"membership_by_content", "print(list() in [[]])\n", "True\n"},
		// The roads that build: a sort, a concatenation, a repeat.
		{"sorting_an_empty_set", "print(sorted(set()))\n", "[]\n"},
		{"concatenating_on_the_right", "print(list() + [1])\n", "[1]\n"},
		{"concatenating_on_the_left", "print([1] + list())\n", "[1]\n"},
		{"repeating_an_empty_list", "print(list() * 2)\n", "[]\n"},
		{"an_empty_call_printed_after_a_sort", "print(sorted(list()))\n", "[]\n"},
	}
}

func TestTheDecidingDoorsAnswerTheEmptyContainerCallLikeTheLiteral(t *testing.T) {
	t.Parallel()
	for _, tc := range emptyContainerDecidePrograms() {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if _, err := Compile(tc.src); err != nil {
				t.Fatalf("refused: %v", err)
			}
			if got := compiledOut(t, tc.src); got != tc.want {
				t.Fatalf("compiled = %q, want %q", got, tc.want)
			}
			if got, _, err := PythonRun(tc.src); err != nil || got != tc.want {
				t.Fatalf("reference = %q (%v), want %q", got, err, tc.want)
			}
		})
	}
}

// The record leg over the same sources. A source the record does not cover fails the case: the record is
// what proves the compiled answer moved toward the reference rather than somewhere new.
func TestTheEmptyContainerDecideProgramsAreOnTheRecord(t *testing.T) {
	asked := 0
	for _, tc := range emptyContainerDecidePrograms() {
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

// The traps the deciding doors owe. `abs` names its operand's kind (ADR 0271), the unary minus names its
// own operator (ADR 0266), and each of these raises must be reachable by the program's own `except` — a
// refusal cannot be caught, and a program that wrote `except TypeError:` was answered by the compiler.
func TestTheDecidingDoorsRaiseWhatTheReferenceRaisesForAnEmptyContainerCall(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name, src, class, msg string
	}{
		{"abs_of_an_empty_list", "print(abs(list()))\n", "TypeError", "bad operand type for abs(): 'list'"},
		{"abs_of_an_empty_set", "print(abs(set()))\n", "TypeError", "bad operand type for abs(): 'set'"},
		{"abs_of_an_empty_dict", "print(abs(dict()))\n", "TypeError", "bad operand type for abs(): 'dict'"},
		{"negating_an_empty_list", "print(-list())\n", "TypeError", "bad operand type for unary -: 'list'"},
		{"negating_an_empty_set", "print(-set())\n", "TypeError", "bad operand type for unary -: 'set'"},
		{"negating_an_empty_dict", "print(-dict())\n", "TypeError", "bad operand type for unary -: 'dict'"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			class, message := jitTrap(t, tc.src)
			if class != tc.class || message != tc.msg {
				t.Fatalf("%s raised (%q, %q), want (%q, %q)", tc.src, class, message, tc.class, tc.msg)
			}
			// Catchable — the raise is the program's, not the compiler's, and a refusal cannot be caught.
			if got, _, err := PythonRun(tc.src); err == nil {
				t.Fatalf("%s answered %q where the reference raises", tc.src, got)
			}
		})
	}
}

// The door, asserted in the emitted language: `len` reads the object's length word, the comparison asks the
// container-equality helper rather than comparing two addresses, and the signless raise leaves through
// `rt_die` with CPython's own sentence in the module — while the numeric road that would have folded a
// handle into an arithmetic instruction stays out of the module entirely.
func TestTheDecidingDoorsEmitTheContainersOwnCalls(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name, src, must, mustNot string
	}{
		{
			name: "len of an empty set asks the heap length",
			src:  "print(len(set()))\n",
			must: "call i32 @rt_heap_len(i32 %t1)",
			// …on an object allocated with the SET's kind word.
			mustNot: "",
		},
		{
			name: "two empty sets compare by content",
			src:  "print(set() == set())\n",
			must: "call i32 @rt_container_eq(i32 %t1, i32 %t3)",
			// Not the integer compare two handles would have answered with.
			mustNot: "icmp eq i32 %t1, %t3",
		},
		{
			name: "abs of an empty list raises the reference's sentence",
			src:  "print(abs(list()))\n",
			must: "TypeError: bad operand type for abs(): 'list'",
			// …and no subtract reaches the operand, which is what answered `0`.
			mustNot: "sub i32",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			ir := emittedModuleIR(t, tc.src)
			if !strings.Contains(ir, tc.must) {
				t.Fatalf("%s: the module does not carry %q", tc.name, tc.must)
			}
			body := mainRegion(t, tc.src)
			if tc.mustNot != "" && strings.Contains(body, tc.mustNot) {
				t.Fatalf("%s: the module still carries %q:\n%s", tc.name, tc.mustNot, body)
			}
		})
	}
}

// The controls. A list literal still answers `len` from its own table, a filled container still compares,
// `abs` of a container literal still raises the same sentence it always did, and the two shapes the
// reference answers that this backend has no ordering helper for still refuse rather than guess.
func TestTheDecidingDoorsStillRefuseWhatHasNoAnswer(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name, src, want string
	}{
		{"literal_len", "print(len([1, 2]))\n", "2\n"},
		{"literal_equality", "print([1] == [1])\n", "True\n"},
		{"empty_literals_still_equal", "print([] == [] and {} == {})\n", "True\n"},
		{"name_bound_set", "xs = set()\nprint(len(xs))\n", "0\n"},
		{"filled_list_sum", "print(sum([1, 2]))\n", "3\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := compiledOut(t, tc.src); got != tc.want {
				t.Fatalf("compiled = %q, want %q", got, tc.want)
			}
		})
	}
	for _, tc := range []struct{ name, src, want string }{
		{
			name: "ordering two filled sets has no compiled helper",
			src:  "print({1} < {1, 2})\n",
			want: "has no compiled lowering",
		},
		{
			name: "sum over a container the run time built",
			src:  "xs = [1, 2]\nprint(sum(xs))\n",
			want: "sum requires an inline list/set/dict literal",
		},
	} {
		t.Run("refuse_"+tc.name, func(t *testing.T) {
			t.Parallel()
			_, err := Compile(tc.src)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("%s: expected the refusal %q, got %v", tc.src, tc.want, err)
			}
		})
	}
	// `abs` of a container LITERAL raised before this row and raises still, word for word — the control
	// that proves the door was missing a spelling and not a kind.
	for _, tc := range []struct {
		name, src, class, msg string
	}{
		{"abs_of_a_list_literal", "print(abs([1]))\n", "TypeError", "bad operand type for abs(): 'list'"},
		{"abs_of_a_dict_literal", "print(abs({1: 2}))\n", "TypeError", "bad operand type for abs(): 'dict'"},
		{"abs_of_a_set_literal", "print(abs({1}))\n", "TypeError", "bad operand type for abs(): 'set'"},
	} {
		t.Run("literal_"+tc.name, func(t *testing.T) {
			t.Parallel()
			class, message := jitTrap(t, tc.src)
			if class != tc.class || message != tc.msg {
				t.Fatalf("%s raised (%q, %q), want (%q, %q)", tc.src, class, message, tc.class, tc.msg)
			}
		})
	}
}

// Filed, not fixed — with today's bytes pinned so each row fails the day its shape changes.
//
//	print(set() < set())   # False in the reference; here: True, at exit 0   — the set-ordering door
//	                                                                    #   the literal spells also lacks (Gap R.175)
//	print({set()})         # unhashable in the reference; here: {set()}          — Gap R.81's hashability rule
//	print(sum(list()))     # 0 in the reference; refused, exactly as `sum(xs)` is for a container VARIABLE
func TestWhatTheDecidingDoorsStillOweTheEmptyContainerCall(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name, src, actual, reference, row string
	}{
		{
			name:      "a strict subset test between two empty sets",
			src:       "print(set() < set())\n",
			actual:    "True\n",
			reference: "False\n",
			row:       "Gap R.175",
		},
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
	for _, tc := range []struct{ name, src, want, row string }{
		{"sum_of_an_empty_call", "print(sum(list()))\n", "sum requires an inline list/set/dict literal", "the same refusal `sum(xs)` gives a container VARIABLE — the fold door, not this row's"},
		{"str_of_a_loop_variable_over_them", "print([str(x) for x in [set(), list()]])\n", "str(x) is refused", "Gap R.171"},
		{"a_container_answering_call_as_an_element", "def f():\n    return set()\n\nprint([f()])\n", "hands back a container", "Gap R.67"},
	} {
		t.Run("refuse_"+tc.name, func(t *testing.T) {
			t.Parallel()
			_, err := Compile(tc.src)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("%s: expected today's refusal %q (owner %s), got %v", tc.src, tc.want, tc.row, err)
			}
		})
	}
}
