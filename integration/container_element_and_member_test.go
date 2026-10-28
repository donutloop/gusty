package integration

import (
	"strings"
	"testing"
)

// The shapes ADR 0437 measured and filed, at the command line (roadmap Gaps R.485, R.492; its
// R.486 half is paid by **ADR 0438** and answers in `integration/named_container_element_test.go`, its
// R.487/R.488 halves are paid by **ADR 0439** and answer in
// `integration/comprehension_element_test.go`, its R.490/R.500 halves are paid by **ADR 0443** and answer in
// `integration/comp_loop_truth_test.go`, and its R.491 half is paid by **ADR 0444** and answers in
// `integration/built_comp_subscript_test.go`). Each row is the compiled backend's answer beside a live
// CPython's, in one case, and the exit code is asserted rather than inferred — these are rows whose wrongness
// is that they exit **0**, and the ones whose wrongness is that they exit **2** (ADR 0166's own-bug code) are
// pinned at 2 so that the day they become a wrong number at 0, this case says so.
//
// The unit twin is `pkg/lang/container_element_and_member_test.go`; the two lists are the same programs, and
// neither suite is allowed to answer for the other (AGENTS.md § the two witness legs).
func TestWhatTheContainerElementAndMemberDoorsOweAtTheCLI(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name      string
		src       string
		wantOut   string // the bytes this backend prints today, when it answers
		wantErr   string // a fragment of the sentence it refuses with, when it refuses
		wantCode  int
		reference string // what CPython prints (or raises) for the same file
		row       string
	}{
		// **Gap R.490**'s three comprehension rows — the LOOP head, the truth test and `bool()` over a
		// comprehension the fold ANSWERED, each of which reached `llc` with a container GLOBAL in an `i32`
		// operand and died at **ADR 0166**'s exit **2** — MOVED with **ADR 0443** to
		// `integration/comp_loop_truth_test.go`, where the same programs are asserted beside a live `python3`
		// (`TestTheLoopWalksAComprehensionThroughTheCLI`, `TestTheConditionAndBoolAskAComprehensionThroughTheCLI`)
		// with exit 2 forbidden on every row. The same three doors over a comprehension the run time BUILDS were
		// **Gap R.500**, answered by the same commit.
		//
		// **Gap R.491**'s container-of-containers row MOVED with **ADR 0444**: the comprehension now walks the
		// object its iterable built, the subscript reads the bound item through the pair road, and the same
		// program is asserted beside a live `python3` in
		// `integration/built_comp_subscript_test.go::TestASubscriptOfABuiltComprehensionThroughTheCLI`.
		// The four SET-MEMBER rows this table carried while **Gap R.485** and **Gap R.492** were open raise the
		// reference's own `TypeError: unhashable type: …` now, at exit **3** and catchably, and moved to
		// `TestTheMemberDoorRaisesWhatTheReferenceRaisesThroughTheCLI` with **ADR 0445**: the guard that asks the
		// hashing question lives on the road that stores a kind word, and the gate that opens that road now
		// admits a member whatever produced its tag.
	} {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			path := writeSrc(t, t.TempDir(), "owed.gy", tc.src)
			refOut, refCode := oracleTrap(t, path)
			if strings.HasPrefix(tc.reference, "raises ") {
				if refCode == 0 {
					t.Fatalf("the reference answered %q for a row filed as a raise (%s)", refOut, tc.row)
				}
			} else if refCode != 0 || refOut != tc.reference+"\n" {
				t.Fatalf("the reference prints %q exit %d, this row recorded %q (%s)", refOut, refCode, tc.reference, tc.row)
			}
			stdout, stderr, code := cliStreams(t, cliEngines[0], path)
			if code != tc.wantCode {
				t.Fatalf("%q exited %d, this row pins exit %d (%s)\nstdout: %s\nstderr: %s\nreference: %s", tc.src, code, tc.wantCode, tc.row, stdout, stderr, tc.reference)
			}
			if tc.wantOut != "" {
				if stdout != tc.wantOut {
					t.Errorf("%q printed %q, this row pins %q — the reference prints %s (%s)", tc.src, stdout, tc.wantOut, tc.reference, tc.row)
				}
				return
			}
			if !strings.Contains(stdout+stderr, tc.wantErr) {
				t.Errorf("%q refused without naming %q (%s):\n%s", tc.src, tc.wantErr, tc.row, stdout+stderr)
			}
		})
	}
}

// TestTheMemberDoorRaisesWhatTheReferenceRaisesThroughTheCLI is the answered half of the member door: a set
// literal whose member is a list, a dict, a set or a comprehension that builds one dies with the reference's
// own sentence, at the program's exit code rather than the compiler's, and the program can catch it
// (roadmap **Gap R.485** + **Gap R.492**, paid by **ADR 0445**; **Gap R.81** owns the `add` door, and the
// rows that stayed filed there are pinned beside it, not here).
//
// The raise was never a missing sentence — it was a question nobody could ask. `guardHashableTag` compares the
// tag a slot arrived with, and the untagged set road handed it nothing to compare, so the member was filed
// under its own handle and printed back. The gate that opens the tagged member road now admits a member
// whatever produced its kind word, and the guard is asked of the tag the store receives.
func TestTheMemberDoorRaisesWhatTheReferenceRaisesThroughTheCLI(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name    string
		src     string
		message string
	}{
		{"a list member", "print({[1], [2]})\n", "unhashable type: 'list'"},
		{"a dict member, held in a name", "sa = {1: \"a\", \"b\": 2}\nprint({sa})\n", "unhashable type: 'dict'"},
		{"a set member", "print({{1}})\n", "unhashable type: 'set'"},
		{"a comprehension that builds a list", "print({[x for x in [1]]})\n", "unhashable type: 'list'"},
		{"a spelled set inside a set", "print({{1, 2}})\n", "unhashable type: 'set'"},
	} {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			path := writeSrc(t, t.TempDir(), "member.gy", tc.src)
			refOut, refCode := oracleTrap(t, path)
			if refCode == 0 {
				t.Fatalf("the reference answered %q where this row expects %s to raise", refOut, tc.message)
			}
			if !strings.Contains(refOut, tc.message) {
				t.Fatalf("the reference does not raise %q: %s", tc.message, refOut)
			}
			stdout, stderr, code := cliStreams(t, cliEngines[0], path)
			if code == 2 {
				t.Fatalf("the toolchain failed on its own program (exit 2 — ADR 0166's code):\n%s", stderr)
			}
			if code != 3 {
				t.Fatalf("exit %d, want the program's raise at exit 3; stdout %q stderr %q", code, stdout, stderr)
			}
			if stdout != "" {
				t.Fatalf("stdout %q, want the raise to print nothing", stdout)
			}
			if !strings.Contains(stderr, tc.message) {
				t.Fatalf("stderr does not carry %q:\n%s", tc.message, stderr)
			}
		})
	}
}

// TestTheMemberDoorStaysQuietForAHashableMember is the control the raise has to keep: a tuple is the container
// this language hashes (**ADR 0361**), and a guard that fired on the heap's layout instead of the AST would
// turn every one of these rows into a raise the reference does not raise.
func TestTheMemberDoorStaysQuietForAHashableMember(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		src  string
		want string
	}{
		{"print({(1, 2)})\n", "{(1, 2)}\n"},
		{"print({(1, 2), (3, 4)})\n", "{(1, 2), (3, 4)}\n"},
		{"print({1, 2})\n", "{1, 2}\n"},
		{"print({1, 2.5})\n", "{1, 2.5}\n"},
	} {
		tc := tc
		t.Run(strings.TrimSpace(tc.src), func(t *testing.T) {
			t.Parallel()
			path := writeSrc(t, t.TempDir(), "hashable.gy", tc.src)
			refOut, refCode := oracleTrap(t, path)
			if refCode != 0 || refOut != tc.want {
				t.Fatalf("the reference itself answered %q exit %d, this row expects %q", refOut, refCode, tc.want)
			}
			stdout, stderr, code := cliStreams(t, cliEngines[0], path)
			if code != 0 {
				t.Fatalf("exit %d, want 0 — a hashable member is not an accident\nstderr %s", code, stderr)
			}
			if stdout != tc.want {
				t.Fatalf("printed %q, the reference prints %q", stdout, tc.want)
			}
		})
	}
}
