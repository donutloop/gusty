package integration

import (
	"strings"
	"testing"
)

// The shapes ADR 0437 measured and filed, at the command line (roadmap Gaps R.485, R.490, R.491, R.492 — its
// R.486 half is paid by **ADR 0438** and answers in `integration/named_container_element_test.go`, and its
// R.487/R.488 halves are paid by **ADR 0439** and answer in
// `integration/comprehension_element_test.go`). Each row is the compiled backend's answer beside a live
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
		{
			// The comprehension standing where the LOOP, the truth test and `bool()` ask for a container: the
			// deciding doors have no arm for it, so the word they read is the folded compile-time global and
			// `llc` refuses the module — exit 2, the code ADR 0166 reserves for a compiler bug.
			name:      "a comprehension as a loop iterable is rejected by llc",
			src:       "for v in [x for x in [1]]:\n    print(v)\n",
			wantErr:   "@.lst",
			wantCode:  2,
			reference: "1",
			row:       "Gap R.490 (the loop head)",
		},
		{
			name:      "a comprehension in a truth test is rejected by llc",
			src:       "if [x for x in [1]]:\n    print(\"yes\")\n",
			wantErr:   "@.lst",
			wantCode:  2,
			reference: "yes",
			row:       "Gap R.490 (the condition door)",
		},
		{
			name:      "bool() of a comprehension is rejected by llc",
			src:       "print(bool([x for x in [1]]))\n",
			wantErr:   "@.lst",
			wantCode:  2,
			reference: "True",
			row:       "Gap R.490 (`icmp ne i32 @.lst1, 0`)",
		},
		{
			// The item door names an integer literal or a `range` and nothing else, so a comprehension over a
			// container of containers is answered with an index and the program raises what the reference prints.
			name:      "a comprehension over a container of containers raises where the reference answers",
			src:       "print([[x for x in [[1]]][0], 2])\n",
			wantErr:   "IndexError: list index out of range",
			wantCode:  3,
			reference: "[[1], 2]",
			row:       "Gap R.491",
		},
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
