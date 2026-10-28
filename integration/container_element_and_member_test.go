package integration

import (
	"strings"
	"testing"
)

// The four shapes ADR 0437 measured and filed, at the command line (roadmap Gaps R.485, R.486, R.487,
// R.488). Each row is the compiled backend's answer beside a live CPython's, in one case, and the exit code
// is asserted rather than inferred — these are rows whose wrongness is that they exit **0**, and the one
// whose wrongness is that it exits **2** (ADR 0166's own-bug code) is pinned at 2 so that the day it becomes
// a wrong number at 0, this case says so.
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
			name:      "a container variable as an element answers the slot",
			src:       "sa = {1: \"a\", \"b\": 2}\nprint([sa, 1])\n",
			wantOut:   "[1, 1]\n",
			wantCode:  0,
			reference: "[{1: 'a', 'b': 2}, 1]",
			row:       "Gap R.486",
		},
		{
			name:      "a container variable alone in the element list answers the slot",
			src:       "sa = {1: \"a\", \"b\": 2}\nprint([sa])\n",
			wantOut:   "[1]\n",
			wantCode:  0,
			reference: "[{1: 'a', 'b': 2}]",
			row:       "Gap R.486",
		},
		{
			name:      "a comprehension as an element answers the slot",
			src:       "print([[{\"a\": 1} for x in range(1)], 1])\n",
			wantOut:   "[1, 1]\n",
			wantCode:  0,
			reference: "[[{'a': 1}], 1]",
			row:       "Gap R.487",
		},
		{
			name:      "a comprehension over a call, as an element, answers the slot",
			src:       "def f():\n    return 1\nprint([[f() for x in range(1)], 2])\n",
			wantOut:   "[1, 2]\n",
			wantCode:  0,
			reference: "[[1], 2]",
			row:       "Gap R.487",
		},
		{
			name:      "the same shape one level deeper is rejected by llc",
			src:       "print([[[x for x in [1]]]])\n",
			wantErr:   "llc",
			wantCode:  2,
			reference: "[[[1]]]",
			row:       "Gap R.488 (the compile-time global `@.lst1` in an operand position)",
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
