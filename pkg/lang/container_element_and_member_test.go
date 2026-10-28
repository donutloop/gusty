// Shapes measured while landing ADR 0437 and still NOT fixed: a container that arrives as a member of a set
// literal, and the doors beside the one ADR 0439 paid. Each row is pinned with today's bytes beside the
// reference's, in this file and in `integration/container_element_and_member_test.go`, and each byte below was
// re-measured on a build of the commit before the family was filed (`4fdb87c`) as well as on this one — the
// sentences changed in that commit, these answers did not.
//
// The family had four doors and two have been paid. The ELEMENT door refused to describe a container held in a
// NAME (`sa = {1: "a", "b": 2}` / `print([sa, 1])` printed `[1, 1]`); **ADR 0438** closed that one — the
// census that labels the object now asks the tag road's answer, so a named container travels as a tagged pair
// (**Gap R.486**, paid, rows in `named_container_element_test.go`). The ELEMENT door's other half — the
// element that is a COMPREHENSION — is closed by **ADR 0439**: the tag, the store and the census now read the
// comprehension's own brace, so `print([[[{"a": 1} for x in range(1)], 1])` answers `[[{'a': 1}], 1]` and
// `print([[[x for x in [1]]]])` no longer hands `llc` a compile-time global in an operand position
// (**Gap R.487**, **Gap R.488**, paid; their rows answer in `comprehension_element_test.go`). What is left here:
//
//   - the MEMBER door. A set literal's members are hashed. A list, a dict or a set has no hash in the
//     reference, which raises `TypeError: unhashable type: 'list'`; this backend files the handle as a member
//     and prints `{[1], [2]}` at exit **0** — **Gap R.81**'s rule (`sa.add([1])` adds what cannot be a
//     member) at the literal door rather than the `add` one (**Gap R.485**). The comprehension spelling of the
//     same member (`print({[x for x in [1]]})`) walked through that door on ADR 0439's arm and lands on the
//     same answer at the same exit code; before the arm it was an `llc` rejection. The raise is one raise for
//     the family, and the row that owns it is **Gap R.492** beside **Gap R.485** — the member door's, not
//     this cycle's; and
//   - the doors that stand a comprehension in a position that is not a slot at all: the LOOP head, the truth
//     test and `bool()` ask the container question of an expression the deciding doors still cannot name, and
//     the answer is `icmp ne i32 @.lst1, 0` / `rt_step(i32 @.lst1)` — exit **2** again, and filed as
//     **Gap R.490** because the fix there is the handle at those doors, not another arm on the deciding side
//     (measured: giving `isContainerExpr`/`staticContainerKind` the `*Comp` arm leaves the same three modules
//     rejected, so an arm alone is not what that door owes); and
//   - the comprehension whose ITERABLE is a container of containers, which the item door could not name and
//     the fold used to answer with an index: `print([[x for x in [[1]]][0], 2])` once raised `IndexError` where
//     the reference prints `[[1], 2]` (**Gap R.491**). It is answered by **ADR 0444**'s built-comprehension
//     subscript door and asserted in `pkg/lang/built_comp_subscript_test.go` / its CLI twin.
//
// The sources still filed here are not on the record, and `tools/recmerge` refuses each: the compiled leg's
// bytes are not the reference's, which is the recorder's cross-check earning its keep and the reason a row
// exists rather than a record.
package lang

import (
	"strings"
	"testing"
)

// containerElementFiledRows are the doors beside the one ADR 0439 paid: the comprehension that stands where a
// slot does not go. `kind` says which witness the row asks: an ANSWER compared to the bytes pinned here, a
// REFUSAL whose sentence is pinned, or a module that never reaches a running program at all (the exit-2 class,
// where the unit leg can only ask that the assembly fails and names the global it choked on \u2014 the exit code
// itself is the CLI twin's job).
func containerElementFiledRows() []struct {
	name      string
	src       string
	kind      string
	today     string
	reference string
	row       string
} {
	return []struct {
		name      string
		src       string
		kind      string
		today     string
		reference string
		row       string
	}{
		// **Gap R.490**'s three comprehension rows — the LOOP head, the truth test and `bool()` over a
		// comprehension the fold ANSWERED — MOVED with **ADR 0443**: they are asserted, not remembered, in
		// `pkg/lang/comp_loop_truth_test.go::TestTheLoopWalksAComprehensionStandingWhereItsIterableIs` /
		// `TestTheConditionAndBoolAskAComprehensionWhetherItIsEmpty` and their CLI twin, with the forbidden
		// instruction (`icmp slt i32 %_ctr … @.`) pinned ABSENT by
		// `TestNoModuleComparesALoopCounterWithAComprehensionGlobal`. The same three doors over a comprehension the
		// run time BUILDS were **Gap R.500**, answered by the same commit.
		//
		// **Gap R.491**'s container-of-containers row MOVED with **ADR 0444**: the item is now the object the
		// runtime comprehension bound, the subscript reads it through the pair road, and the row is asserted in
		// `pkg/lang/built_comp_subscript_test.go::TestASubscriptOfABuiltComprehensionAsksTheObject` and
		// `integration/built_comp_subscript_test.go::TestASubscriptOfABuiltComprehensionThroughTheCLI`.
	}
}

// containerMemberFiledRows are the set members the language refuses to hash, which this backend accepts.
func containerMemberFiledRows() []struct {
	name      string
	src       string
	kind      string
	today     string
	reference string
	row       string
} {
	return []struct {
		name      string
		src       string
		kind      string
		today     string
		reference string
		row       string
	}{
		{
			name:      "a set literal accepts a list member",
			src:       "print({[1], [2]})\n",
			kind:      "answer",
			today:     "{[1], [2]}\n",
			reference: "raises TypeError: unhashable type: 'list'",
			row:       "Gap R.485 (the literal door; Gap R.81 owns the `add` door)",
		},
		{
			name:      "a set literal accepts a dict member",
			src:       "sa = {1: \"a\", \"b\": 2}\nprint({sa})\n",
			kind:      "answer",
			today:     "{{1: 'a', 'b': 2}}\n",
			reference: "raises TypeError: unhashable type: 'dict'",
			row:       "Gap R.485",
		},
		{
			name:      "a set literal accepts a set member",
			src:       "print({{1}})\n",
			kind:      "answer",
			today:     "{{1}}\n",
			reference: "raises TypeError: unhashable type: 'set'",
			row:       "Gap R.485",
		},
		{
			// The member door's comprehension spelling. Before **ADR 0439** this was the exit-2 class
			// (`rt_set_add(i32 %t, i32 @.lst1)`); the arm moved it onto the door the literal member already
			// stands on, so the raise this backend owes is one raise for the family rather than two shapes.
			name:      "a set literal accepts a comprehension that builds a list",
			src:       "print({[x for x in [1]]})\n",
			kind:      "answer",
			today:     "{[1]}\n",
			reference: "raises TypeError: unhashable type: 'list'",
			row:       "Gap R.492 (the member door, one raise for the family with Gap R.485)",
		},
	}
}

func compiledAnswerOrRefusal(t *testing.T, src string) (string, int) {
	t.Helper()
	res, err := Compile(src)
	if err != nil {
		return "REFUSED: " + err.Error(), 1
	}
	out := runIR(t, res.IR)
	return out, 0
}

func TestWhatTheContainerElementAndMemberDoorsStillOweIsFiledNotFixed(t *testing.T) {
	t.Parallel()
	rows := append(containerElementFiledRows(), containerMemberFiledRows()...)
	for _, tc := range rows {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if tc.kind == "module" {
				// The module never becomes a program: the assembly is what fails, and the name it names is the
				// compile-time global that has no business being an operand (ADR 0188's rule, ADR 0166's code).
				_, err := JIT(tc.src, 0)
				if err == nil {
					t.Fatalf("%q assembled and ran, where this row is filed because the module is rejected (%s)", tc.src, tc.row)
				}
				if !strings.Contains(err.Error(), tc.today) {
					t.Errorf("%q failed the assembly without naming %q: %v (%s)", tc.src, tc.today, err, tc.row)
				}
				return
			}
			if tc.kind == "trap" {
				// The program RUNS and raises: a raise the reference does not raise is a wrong answer wearing
				// a traceback, and the exit class (3, not 2) is part of what the row pins.
				res, err := Compile(tc.src)
				if err != nil {
					t.Fatalf("%q was refused, where this row pins a trap the program reaches (%s): %v", tc.src, tc.row, err)
				}
				code, out := runIRAllowingTrap(t, res.IR)
				if code == 2 {
					t.Fatalf("%q exited 2 — ADR 0166's compiler-bug code — where this row pins a trap at exit 3 (%s): %s", tc.src, tc.row, out)
				}
				if !strings.Contains(out, strings.TrimPrefix(tc.today, "TRAPPED: ")) {
					t.Errorf("%q trapped without naming %q: %s (%s)", tc.src, tc.today, out, tc.row)
				}
				if HasGoldenAnswer(tc.src) {
					t.Errorf("%q is on the record with an answer, although it traps wrongly (%s)", tc.src, tc.row)
				}
				return
			}
			got, code := compiledAnswerOrRefusal(t, tc.src)
			if tc.kind == "answer" {
				if code != 0 {
					t.Fatalf("%q exited %d, this row pins exit 0 (%s): %s", tc.src, code, tc.row, got)
				}
				if got != tc.today {
					t.Errorf("%q printed %q, this row pins %q — the reference prints %s (%s)", tc.src, got, tc.today, tc.reference, tc.row)
				}
				if HasGoldenAnswer(tc.src) {
					t.Errorf("%q is on the record with an answer, although the compiled leg prints %q and the reference prints %s (%s)", tc.src, got, tc.reference, tc.row)
				}
				return
			}
			if !strings.Contains(got, strings.TrimPrefix(tc.today, "REFUSED: ")) {
				t.Errorf("%q: expected the refusal to contain %q, got %q (%s)", tc.src, strings.TrimPrefix(tc.today, "REFUSED: "), got, tc.row)
			}
			if code == 0 {
				t.Errorf("%q answered %q at exit 0 where this row pins a refusal (%s)", tc.src, got, tc.row)
			}
		})
	}
}

// TestTheContainerElementRowsAreNotSilentlyOnTheRecord is the recorder's rule asked of these rows: an
// answer that is not the reference's cannot be a record, and the row is the only place it can live.
func TestTheContainerElementRowsAreNotSilentlyOnTheRecord(t *testing.T) {
	t.Parallel()
	rows := append(containerElementFiledRows(), containerMemberFiledRows()...)
	for _, tc := range rows {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if HasGoldenAnswer(tc.src) {
				t.Errorf("%q is on the record although it is a filed defect (%s) — the record holds the reference's answer, and this is not it", tc.src, tc.row)
			}
		})
	}
}
