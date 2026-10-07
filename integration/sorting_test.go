package integration

import (
	"strings"
	"testing"

	"github.com/donutloop/gusty/pkg/lang"
)

// TestSortingMatchesCPythonOnAllThreeLegs is the integration half of the sorting feature (roadmap
// L11.7, ADR 0191): the record, the compiled binary, and CPython must agree on the method that
// mutates, the builtin that copies, and the comparator that orders interned strings by text. The
// expectations below are CPython's, transcribed from running the same source — not from what gusty
// printed, which is the mistake ADR 0186 exists to prevent.
func TestSortingMatchesCPythonOnAllThreeLegs(t *testing.T) {
	mixed := []string{`xs = [3, 1, 2]
xs.sort()
print(xs)
ys = ["c", "a", "b"]
ys.sort()
print(ys)
zs = [1, 2, 3]
zs.reverse()
print(zs)
ws = [3, 1, 2]
print(sorted(ws))
print(ws)
vs = [5, 3, 9]
vs.sort()
vs.reverse()
print(vs)
`, `[1, 2, 3]
['a', 'b', 'c']
[3, 2, 1]
[1, 2, 3]
[3, 1, 2]
[9, 5, 3]
`}
	forEach := []struct{ src, want string }{
		{"ds = []\nds.append(4)\nds.append(1)\nds.sort()\nprint(ds)\n", "[1, 4]\n"},
		{"print(sorted([3, 1, 2], reverse=True))\n", "[3, 2, 1]\n"},
		{"print(sorted([]))\n", "[]\n"},
		{"print(sorted([10, 2, 33, 4]))\n", "[2, 4, 10, 33]\n"},
		{"print(sorted([\"pear\", \"apple\", \"fig\"]))\n", "['apple', 'fig', 'pear']\n"},
		// The comparator's whole reason for existing: two strings whose interned indices are in
		// the opposite order to their text.
		{"print(sorted([\"zebra\", \"aardvark\"]))\n", "['aardvark', 'zebra']\n"},
		// sorted() copies; sort() does not. This is the difference a program can see.
		{"xs = [3, 1, 2]\nys = sorted(xs)\nprint(ys)\nprint(xs)\n", "[1, 2, 3]\n[3, 1, 2]\n"},
	}
	for _, tc := range append([]struct{ src, want string }{{mixed[0], mixed[1]}}, forEach...) {
		lang.RecordedStdoutIs(t, tc.src, tc.want)
		res, err := lang.JIT(tc.src, 0)
		if err != nil {
			t.Fatalf("compile: %v", err)
		}
		if res.Output != tc.want {
			t.Errorf("compiled\n got %q\nwant %q", res.Output, tc.want)
		}
		if py := pythonOutput(t, tc.src); py != tc.want {
			t.Errorf("CPython disagrees with the expectation we asserted\n got %q\nwant %q", py, tc.want)
		}
	}
}

// TestSortedResultBehavesLikeAListCompiled: ys = sorted(xs) must produce a container, not a slot
// number. Assigning a runtime-built handle to a plain int variable printed the handle — the same
// class of bug as ADR 0188's print-a-handle, reached through the assignment path (ADR 0191).
func TestSortedResultBehavesLikeAListCompiled(t *testing.T) {
	src := "xs = [3, 1, 2]\nys = sorted(xs)\nprint(ys)\nprint(ys[0])\nprint(len(ys))\nfor x in ys:\n    print(x)\n"
	want := "[1, 2, 3]\n1\n3\n1\n2\n3\n"
	res, err := lang.JIT(src, 0)
	if err != nil {
		t.Fatal(err)
	}
	if res.Output != want {
		t.Fatalf("compiled\n got %q\nwant %q", res.Output, want)
	}
	if got := runCompiled(t, src); got != want {
		t.Fatalf("interpreter\n got %q\nwant %q", got, want)
	}
	if py := pythonOutput(t, src); py != want {
		t.Fatalf("CPython disagrees with the assertion\n got %q\nwant %q", py, want)
	}
}

// TestSortingRefusalsAreHonest: a mixed-kind list is Python's TypeError, and key=/reverse= need
// first-class functions — the refusal has to own the reason rather than claim the argument was a
// count (roadmap L11.8 owns the diagnostic families).
func TestSortingRefusalsAreHonest(t *testing.T) {
	for _, tc := range []struct {
		src, want string
	}{
		{"xs = [1, \"a\"]\nxs.sort()\nprint(xs)\n", "more than one kind"},
		{"print(sorted([1, \"a\"]))\n", "more than one kind"},
		{"xs = [1, 2]\nxs.sort(key=len)\nprint(xs)\n", "first-class functions"},
	} {
		res, err := lang.Compile(tc.src)
		msg := ""
		if err != nil {
			msg = err.Error()
		}
		if res != nil {
			for _, d := range res.Diagnostics {
				msg += d.Msg + "\n"
			}
		}
		if err == nil && !strings.Contains(msg, "more than one kind") && !strings.Contains(msg, "first-class") {
			t.Fatalf("%q should not compile", tc.src)
		}
		if !strings.Contains(msg, tc.want) {
			t.Fatalf("%q refusal should mention %q, got:\n%s", tc.src, tc.want, msg)
		}
	}
}
