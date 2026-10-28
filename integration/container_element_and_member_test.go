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
		{
			name:      "a set literal accepts a list member",
			src:       "print({[1], [2]})\n",
			wantOut:   "{[1], [2]}\n",
			wantCode:  0,
			reference: "raises TypeError: unhashable type: 'list'",
			row:       "Gap R.485 (Gap R.81 owns the `add` door)",
		},
		{
			name:      "a set literal accepts a dict member",
			src:       "sa = {1: \"a\", \"b\": 2}\nprint({sa})\n",
			wantOut:   "{{1: 'a', 'b': 2}}\n",
			wantCode:  0,
			reference: "raises TypeError: unhashable type: 'dict'",
			row:       "Gap R.485",
		},
		{
			name:      "a set literal accepts a set member",
			src:       "print({{1}})\n",
			wantOut:   "{{1}}\n",
			wantCode:  0,
			reference: "raises TypeError: unhashable type: 'set'",
			row:       "Gap R.485",
		},
		{
			// The member door's comprehension spelling: on ADR 0439's arm it reaches the same door the literal
			// member stands on, so it prints the member the reference refuses to hash — one raise for the
			// family, owned by Gap R.485 and filed as Gap R.492.
			name:      "a set literal accepts a comprehension that builds a list",
			src:       "print({[x for x in [1]]})\n",
			wantOut:   "{[1]}\n",
			wantCode:  0,
			reference: "raises TypeError: unhashable type: 'list'",
			row:       "Gap R.492 (with Gap R.485)",
		},
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
