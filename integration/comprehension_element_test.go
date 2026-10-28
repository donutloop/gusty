package integration

import (
	"fmt"
	"strings"
	"testing"
)

// The reference leg of the family **ADR 0439** pays (roadmap **Gap R.487**, **Gap R.488**). Each case writes
// the source, asks a live CPython what it prints, and requires this backend's bytes to be those bytes at exit
// **0** — the wrongness of this family was always one of two things: exit **0** with the allocator's slot
// printed as the value (`[1, 1]` where the reference prints `[[{'a': 1}], 1]`), or exit **2** with `llc`
// refusing a module that put a compile-time list global in an `i32` operand (`print([[[x for x in [1]]]])`),
// the code **ADR 0166** reserves for a compiler bug. So the exit code is asserted and not inferred, and a
// refusal fails the case as loudly as a wrong number does.
//
// The unit twin is `pkg/lang/comprehension_element_test.go`, which also asks the record leg and the emitted
// IR; neither suite is allowed to answer for the other (AGENTS.md § the two witness legs).
func TestAComprehensionWrittenAsAnElementAnswersItsOwnKindAtTheCLI(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct{ name, src string }{
		// The two programs **Gap R.487** was filed by, and **Gap R.488**'s one level deeper.
		{"a_dict_comprehension_as_an_element", "print([[{\"a\": 1} for x in range(1)], 1])\n"},
		{"a_comprehension_over_a_call_as_an_element", "def f():\n    return 1\nprint([[f() for x in range(1)], 2])\n"},
		{"a_comprehension_two_levels_down", "print([[[x for x in [1]]]])\n"},
		// The fold and the runtime builder, and the shapes beside them.
		{"a_folded_comprehension_beside_a_number", "print([[x for x in [1]], 1])\n"},
		{"a_comprehension_with_a_filter_as_an_element", "print([[x for x in [1] if x], 1])\n"},
		{"a_text_comprehension_as_an_element", "print([[v for v in [\"a\", \"b\"]], 1])\n"},
		{"a_comprehension_of_dicts_as_an_element", "print([[{\"a\": x} for x in range(2)], 1])\n"},
		{"a_nested_clause_comprehension_as_an_element", "print([[[x for x in [y]] for y in [1, 2]], 3])\n"},
		{"two_comprehensions_in_one_literal", "print([[x for x in [1]], [y for y in [2]]])\n"},
		{"a_comprehension_beside_a_text", "print([[x for x in [1]], \"a\"])\n"},
		// Every door that WRITES the slot.
		{"a_comprehension_as_a_dict_value", "print([{\"k\": [x for x in [1]]}])\n"},
		{"a_set_comprehension_as_an_element", "print([{x for x in [1]}])\n"},
		{"a_dict_comprehension_as_an_element", "print([{1: x for x in [1]}])\n"},
		{"a_comprehension_appended_to_a_list", "xs = []\nxs.append([x for x in [1]])\nprint(xs)\n"},
		{"a_comprehension_written_into_a_dict_entry", "d = {}\nd[\"k\"] = [x for x in [1]]\nprint(d)\n"},
		{"a_comprehension_as_a_tuple_element", "print(([x for x in [1]], 2))\n"},
		{"a_comprehension_bound_inside_a_literal", "ys = [[x for x in [1]]]\nprint(ys)\n"},
		// The roads that READ the element back.
		{"the_comprehension_element_read_two_levels_down", "print([[x for x in [1]]][0][0])\n"},
		{"the_comprehension_element_measured", "print(len([[x for x in [1]]][0]))\n"},
		{"the_comprehension_element_tested_for_membership", "print(1 in [[x for x in [1]]][0])\n"},
		{"the_comprehension_element_looped_over", "for v in [[x for x in [1]]][0]:\n    print(v)\n"},
		{"a_literal_of_comprehension_elements_compared", "print([[x for x in [1]]] == [[1]])\n"},
		{"a_literal_of_comprehension_elements_rendered_by_str", "print(str([[x for x in [1]]]))\n"},
		{"the_comprehension_element_repeated_by_a_number", "print([[x for x in [1]]][0] * 2)\n"},
		{"a_comprehension_over_a_call_bound_and_read_back", "def mk():\n    return [1]\nxs = [[x for x in mk()]]\nprint(xs[0] == [1])\n"},
		// The controls: what answered before the arm, which must not move — including the one container the
		// language does hash (**ADR 0361**) and the numeric literal that keeps its static array.
		{"a_spelled_nested_container_element", "print([[1, 2], 1])\n"},
		{"a_numeric_literal", "print([1, 2, 3])\n"},
		{"a_dict_keyed_by_a_tuple", "print({(1, 2): 1})\n"},
		{"a_dict_keyed_by_a_number_and_a_text", "print({1: \"a\", \"b\": 2})\n"},
	} {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			path := writeSrc(t, t.TempDir(), "comprehension_element.gy", tc.src)
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
					t.Errorf("%s printed %q, the reference printed %q (%s)", engine, combined, refOut, tc.src)
				}
			}
		})
	}
}

// TestTheComprehensionElementNeverExitsTwo is the ADR 0166 half of the sweep. **Gap R.488** was an exit-2 row:
// a compile-time container global in an `i32` operand, which `llc` rejects, is the shape the exit-code contract
// calls a compiler bug. Every program this cycle ANSWERS is listed here, so the day one of them stops
// assembling it fails here rather than in a roadmap row — and the shapes that still reach exit 2 for a
// different reason (the loop head, the truth test, `bool()`) are pinned at 2 in
// `TestWhatTheContainerElementAndMemberDoorsOweAtTheCLI`, not here.
func TestTheComprehensionElementNeverExitsTwo(t *testing.T) {
	t.Parallel()
	for i, src := range []string{
		"print([[[x for x in [1]]]])\n",
		"print([[[x for x in [1]], [y for y in [2]]], 3])\n",
		"print([[{\"a\": 1} for x in range(1)], 1])\n",
		"def f():\n    return 1\nprint([[f() for x in range(1)], 2])\n",
		"print([{\"k\": [x for x in [1]]}])\n",
		"xs = []\nxs.append([x for x in [1]])\nprint(xs)\n",
		"d = {}\nd[\"k\"] = [x for x in [1]]\nprint(d)\n",
		"print(([x for x in [1]], 2))\n",
		"print([[x for x in [1]]][0][0])\n",
		"print([{1: x for x in [1]}])\n",
		"print([[x for x in [1]], \"a\"])\n",
	} {
		src := src
		t.Run(fmt.Sprintf("case_%02d", i), func(t *testing.T) {
			t.Parallel()
			path := writeSrc(t, t.TempDir(), "comprehension_no_exit2.gy", src)
			for _, engine := range cliEngines {
				if _, code := cliRunCode(t, engine, path); code == 2 {
					t.Errorf("%s exited 2 (a compiler bug, %s) on %q — the arm that labels a comprehension element must refuse in words or answer, never emit a module %s rejects",
						engine, "ADR 0166", src, "llc")
				}
			}
		})
	}
}

// TestAComprehensionKeyRefusesAtTheCLIWithoutPrintingGo is the refusal's reference leg: the reference RAISES
// for a dict keyed by a comprehension (`unhashable type: 'list'`), this backend declines in words, and the
// sentence must be readable by a person and by an agent — no Go type, no compiler temp (**Gap R.232**,
// **Gap R.249**). Exit 1 is asserted: a decline is not a compiler bug (exit 2) and not a run (exit 0).
func TestAComprehensionKeyRefusesAtTheCLIWithoutPrintingGo(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name, src, need string
		deny            []string
	}{
		{
			name: "a_list_comprehension_as_a_key",
			src:  "print({[x for x in [1]]: 1})\n",
			need: "a list comprehension",
			deny: []string{"*lang.", "%!s(", "@.lst", "__cans"},
		},
		{
			name: "a_dict_comprehension_as_a_key",
			src:  "print({{1: x for x in [1]}: 1})\n",
			need: "a dict comprehension",
			deny: []string{"*lang.", "%!s(", "@.dict"},
		},
		{
			name: "a_set_comprehension_as_a_key",
			src:  "print({{x for x in [1]}: 1})\n",
			need: "a set comprehension",
			deny: []string{"*lang.", "%!s(", "@.set"},
		},
	} {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			path := writeSrc(t, t.TempDir(), "comprehension_key.gy", tc.src)
			_, refCode := oracleTrap(t, path)
			if refCode == 0 {
				t.Fatalf("the reference ANSWERED a program this row files as a raise: %s", tc.src)
			}
			stdout, stderr, code := cliStreams(t, cliEngines[0], path)
			if code == 0 {
				t.Fatalf("%q answered at exit 0 where the reference raises and this backend declines:\n%s", tc.src, stdout)
			}
			if code == 2 {
				t.Fatalf("%q exited 2 (a compiler bug, %s) where the row pins a front-end decline at exit 1:\n%s", tc.src, "ADR 0166", stdout+stderr)
			}
			if code != 1 {
				t.Fatalf("%q exited %d, this row pins exit 1:\n%s", tc.src, code, stdout+stderr)
			}
			all := stdout + stderr
			if !strings.Contains(all, tc.need) {
				t.Errorf("the refusal never names %q:\n%s", tc.need, all)
			}
			for _, d := range tc.deny {
				if strings.Contains(all, d) {
					t.Errorf("the refusal prints the denied %q — a refusal sentence is an interface, not a Go dump:\n%s", d, all)
				}
			}
		})
	}
}
