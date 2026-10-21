package integration

import (
	"os"
	"strings"
	"testing"

	"github.com/donutloop/gusty/pkg/lang"
)

// A list literal read by a position, through the CLI (roadmap Gap R.218, ADR 0331). The four shapes that
// used to answer a payload — a text, a float, None and a bool, each printed as the number its payload
// happens to be — are asserted here as answers, and the fold's own shape is asserted to have stayed a
// fold: a program the compiler can read outright must not start allocating because the door beside it
// widened.

func TestAListLiteralElementAnswersThroughTheCLI(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		src  string
		want string
	}{
		{`print(["a", "b"][0])`, "a"},
		{`print([1.5, 2][0])`, "1.5"},
		{`print([None, 2][0])`, "None"},
		{`print([True, 2][0])`, "True"},
		{`print(["a", "b"][-1])`, "b"},
		{`print([[1, 2], [3, 4]][0][1])`, "2"},
		{`print([{"a": 1}][0])`, "{'a': 1}"},
		{`print(repr(["a", 1.5][0]))`, "'a'"},
		{`print(len(["a", "b", "c"]))`, "3"},
		{`first = ["a", "b"][0]` + "\n" + `print(first)`, "a"},
		{`print([1, 2, 3][1] + 10)`, "12"},
	} {
		t.Run(strings.ReplaceAll(tt.src, "\n", "; "), func(t *testing.T) {
			t.Parallel()
			stdout, stderr, code := cliStreams(t, "--eval", tt.src)
			if code != 0 {
				t.Fatalf("exit %d, want 0: the reference answers this program\nstderr %q", code, stderr)
			}
			if got := strings.TrimSpace(stdout); got != tt.want {
				t.Fatalf("printed %q, want %q", got, tt.want)
			}
		})
	}
}

// The residue, at the same altitude: a one-word position cannot carry the element's kind, and the road says
// so by name instead of printing a payload at exit 0. The call argument left this table with roadmap
// Gap R.220 — it is asserted as an answer in `TestACallArgumentOfAListLiteralReadAnswersAtTheCLI`.
func TestAOneWordPositionRefusesTheListLiteralReadThroughTheCLI(t *testing.T) {
	t.Parallel()
	for _, src := range []string{
		"def f():\n    return [\"a\",\"b\"][0]\nprint(f())",
		`print([1.5, 2][0] + 1)`,
	} {
		t.Run(strings.ReplaceAll(src, "\n", "; "), func(t *testing.T) {
			t.Parallel()
			stdout, stderr, code := cliStreams(t, "--eval", src)
			if code == 0 {
				t.Fatalf("exit 0 with stdout %q: a one-word position printed the element's payload", stdout)
			}
			if !strings.Contains(stderr, "Gap R.218") {
				t.Fatalf("the refusal did not name its row: %s", stderr)
			}
		})
	}
}

// The paid half beside it: the same element handed to a call arrives with its tag, prints as the element, and
// leaves the all-integer literal on the one-word road it always took.
func TestACallArgumentOfAListLiteralReadAnswersAtTheCLI(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct{ name, src, want string }{
		{"a text element", "def id(v):\n    print(v)\n\nid([\"a\",\"b\"][0])\n", "a\n"},
		{"a float element", "def id(v):\n    print(v)\n\nid([1,2.5][1])\n", "2.5\n"},
		{"an int element keeps one word", "def twice(v):\n    print(v)\n\ntwice([3,4][0])\n", "3\n"},
	} {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			stdout, stderr, code := cliStreams(t, "--eval", tc.src)
			if code != 0 {
				t.Fatalf("exit %d, want the answer\nstdout %q\nstderr %q", code, stdout, stderr)
			}
			if stdout != tc.want {
				t.Fatalf("printed %q, the reference prints %q", stdout, tc.want)
			}
		})
	}
}

// The gate ADR 0329 planted at the subscript stands at the pair door as well as the fold road, through the
// CLI: an index that is not a position is the reference's TypeError, and a list-valued index still leaves a
// module `llc` accepts (Gap R.222's exit-2 shape at Gap R.218's door).
func TestAnIndexThatIsNotAPositionIsRefusedThroughTheCLI(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		src  string
		want string
	}{
		{`print([1, 2]["a"])`, "TypeError: list indices must be integers or slices, not str"},
		{`print(["a", "b"][None])`, "TypeError: list indices must be integers or slices, not NoneType"},
		{`print(["a", "b"][[0]])`, "TypeError: list indices must be integers or slices, not list"},
		{`print({1: ["a", "b"]}[1]["a"])`, "TypeError: list indices must be integers or slices, not str"},
	} {
		t.Run(tt.src, func(t *testing.T) {
			t.Parallel()
			// Two asks, one row: the reference's sentence out of the run, and the verifier's verdict on the
			// module the raise leaves behind — Gap R.222's exit-2 shape excluded rather than assumed.
			stdout, stderr, code := cliStreams(t, "--eval", tt.src)
			if code == 2 {
				t.Fatalf("the toolchain failed on its own program:\n%s", stderr)
			}
			if code == 0 {
				t.Fatalf("exit 0 with stdout %q: a non-position index printed a slot", stdout)
			}
			if !strings.Contains(stderr, tt.want) {
				t.Fatalf("stderr does not carry %q:\n%s", tt.want, stderr)
			}
			out, vstderr, vcode := cliStreams(t, "--verify-llvm", tt.src)
			if vcode != 0 || !strings.Contains(out, "module verified") {
				t.Fatalf("the module behind the raise does not verify: exit %d, %q %q", vcode, out, vstderr)
			}
		})
	}
}

// The whole matrix as one program, line for line against CPython.
func TestTheListLiteralReadProgramMatchesTheReferenceThroughTheCLI(t *testing.T) {
	t.Parallel()
	src, err := os.ReadFile("programs/list_literal_read.gy")
	if err != nil {
		t.Fatalf("read program: %v", err)
	}
	if strings.TrimSpace(string(src)) == "" {
		t.Fatal("the program is empty")
	}
	want, err := os.ReadFile("testdata/list_literal_read.expected")
	if err != nil {
		t.Fatalf("read the reference's answer: %v", err)
	}
	stdout, stderr, code := cliStreams(t, "--file", "programs/list_literal_read.gy")
	if code == 2 {
		t.Fatalf("the toolchain failed on its own program:\n%s", stderr)
	}
	if code != 0 {
		t.Fatalf("exit %d, want 0\nstderr %q", code, stderr)
	}
	got := strings.Split(strings.TrimRight(stdout, "\n"), "\n")
	wantLines := strings.Split(strings.TrimRight(string(want), "\n"), "\n")
	if len(got) != len(wantLines) {
		t.Fatalf("the program printed %d lines, the reference printed %d:\n%s", len(got), len(wantLines), strings.Join(got, "\n"))
	}
	for i := range wantLines {
		if got[i] != wantLines[i] {
			t.Fatalf("line %d: the program answered %q where CPython answers %q\n%s", i+1, got[i], wantLines[i], strings.Join(got, "\n"))
		}
	}
}

func TestTheListLiteralReadProgramIsAnAssertedMatrixRow(t *testing.T) {
	t.Parallel()
	cases := conformanceCases()
	var row *lang.ConformanceCase
	for i := range cases {
		if cases[i].Name == "list_literal_read.gy" {
			row = &cases[i]
			break
		}
	}
	if row == nil {
		t.Fatal("no conformance matrix row runs list_literal_read.gy")
	}
	if !row.Asserted {
		t.Fatalf("row %s is not asserted: its stdout would run unwatched", row.ID)
	}
	if row.Oracle != "match" {
		t.Fatalf("row %s declares oracle %q; the compiled leg answers this program, so it is a match", row.ID, row.Oracle)
	}
	for _, want := range []string{`print(["a", "b"][0], ["a", "b"][1])`, "a position past the end raises IndexError"} {
		if !strings.Contains(row.Source, want) {
			t.Fatalf("row %s runs a program without %q", row.ID, want)
		}
	}
}
