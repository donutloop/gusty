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
//   - the MEMBER door — PAID by **ADR 0445**. A set literal's members are hashed. A list, a dict or a set has no
//     hash in the reference, which raises `TypeError: unhashable type: 'list'`; this backend filed the handle as a
//     member and printed `{[1], [2]}` at exit **0** — **Gap R.81**'s rule (`sa.add([1])` adds what cannot be a
//     member) at the literal door rather than the `add` one (**Gap R.485**). The comprehension spelling of the same
//     member (`print({[x for x in [1]]})`) walked through that door on ADR 0439's arm and landed on the same answer
//     at the same exit code (**Gap R.492**). Both raise the reference's own sentence now, catchably at exit **3**,
//     in `TestTheMemberDoorRaisesWhatTheReferenceRaises` and its CLI twin; the `add` door stays **Gap R.81**'s.
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
	"errors"
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
		// The four rows this table carried while **Gap R.485** and **Gap R.492** were open raise the reference's
		// own `TypeError: unhashable type: …` now, catchably and at exit **3**, and moved to
		// `TestTheMemberDoorRaisesWhatTheReferenceRaises` with **ADR 0445**. The raise was never a missing
		// sentence — `guardHashableTag` compares the tag a slot arrived with, and the untagged set road handed it
		// nothing to compare, so the member was filed under its own handle and printed back. The gate that opens
		// the tagged member road now admits a member whatever produced its kind word (**Gap R.146**'s payload with
		// its tag), and the guard is asked of the tag the store receives, whoever wrote it.
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

// TestTheMemberDoorRaisesWhatTheReferenceRaises is the answered half of the member door (**Gap R.485** +
// **Gap R.492**, paid by **ADR 0445**): a set literal whose member is a list, a dict, a set or a comprehension
// that builds one asks the hashing question and dies with the reference's own sentence. The record leg is not
// decoration — **Gap R.204**'s rule is that a raise a reader cannot re-measure is a claim, and these four
// programs had no record at all while they printed the unhashable member; `tools/recmerge` wrote each entry
// only after the reference and the compiled leg raised the same line.
func TestTheMemberDoorRaisesWhatTheReferenceRaises(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name    string
		src     string
		class   string
		message string
	}{
		{"a list member", "print({[1], [2]})", "TypeError", "unhashable type: 'list'"},
		{"a dict member, held in a name", "sa = {1: \"a\", \"b\": 2}\nprint({sa})", "TypeError", "unhashable type: 'dict'"},
		{"a set member", "print({{1}})", "TypeError", "unhashable type: 'set'"},
		{"a comprehension that builds a list", "print({[x for x in [1]]})", "TypeError", "unhashable type: 'list'"},
		{"a spelled set inside a set", "print({{1, 2}})", "TypeError", "unhashable type: 'set'"},
	} {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			class, message := jitTrap(t, tc.src)
			if class != tc.class || message != tc.message {
				t.Fatalf("%q raised %s: %s; the reference raises %s: %s", tc.src, class, message, tc.class, tc.message)
			}
			err := goldenRunError(t, tc.src)
			if err == nil {
				t.Fatalf("%q has no recorded trap, although the compiled run raises %s: %s", tc.src, class, message)
			}
			var te *TrapError
			if errors.As(err, &te) {
				if te.ExnType != tc.class || te.ExnMsg != tc.message {
					t.Fatalf("the record traps %s: %s; the reference raises %s: %s", te.ExnType, te.ExnMsg, tc.class, tc.message)
				}
			} else if !strings.Contains(err.Error(), tc.message) {
				t.Fatalf("the record's failure %q does not carry %q", err, tc.message)
			}
		})
	}
}

// TestTheMemberDoorStaysQuietForAHashableMember is the control the raise above has to keep: a tuple is the
// container this language hashes (**ADR 0361**), so the decision is the AST's and not the heap's — a tuple and a
// list share the heap's layout, and a guard that read the layout would refuse every row below (**Gap R.81**).
func TestTheMemberDoorStaysQuietForAHashableMember(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		src  string
		want string
	}{
		{"print({(1, 2)})", "{(1, 2)}"},
		{"print({(1, 2), (3, 4)})", "{(1, 2), (3, 4)}"},
		{"print({1, 2})", "{1, 2}"},
		{"print({1, 2.5})", "{1, 2.5}"},
	} {
		tc := tc
		t.Run(tc.src, func(t *testing.T) {
			t.Parallel()
			out := strings.TrimRight(compiledOut(t, tc.src), "\n")
			if out != tc.want {
				t.Fatalf("%q printed %q; a hashable member is not an accident — the reference prints %q", tc.src, out, tc.want)
			}
			rec, err := runGoldenStdout(t, tc.src)
			if err != nil {
				t.Fatalf("the record does not answer a program both legs agree on: %v", err)
			}
			if strings.TrimRight(rec, "\n") != tc.want {
				t.Fatalf("the record says %q, the reference prints %q", rec, tc.want)
			}
		})
	}
}
