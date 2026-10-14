package integration

import (
	"os"
	"strings"
	"testing"

	"github.com/donutloop/gusty/pkg/lang"
)

// An index that is not a position, through the CLI (roadmap Gap R.222, ADR 0329). These doors used to
// ANSWER — `xs["a"]` printed an element, `xs[[0]]` broke the module and the toolchain exited 2 — so the
// exit code and the empty stdout are asserted here as much as the sentence: a wrong answer at exit 0 is
// the defect, and a toolchain failure is the contract's own bug code.

func TestAnIndexThatIsNotAPositionExitsAsATrapThroughTheCLI(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		name string
		src  string
		want string
	}{
		{"a text index into a list", "xs = [1, 2]\nprint(xs[\"a\"])", "TypeError: list indices must be integers or slices, not str"},
		{"a float index into a list", "xs = [1, 2]\nprint(xs[1.5])", "TypeError: list indices must be integers or slices, not float"},
		{"a nothing index into a list", "xs = [1, 2]\nprint(xs[None])", "TypeError: list indices must be integers or slices, not NoneType"},
		{"a list index into a list", "xs = [1, 2]\nprint(xs[[0]])", "TypeError: list indices must be integers or slices, not list"},
		{"a tuple index into a list", "xs = [1, 2]\nprint(xs[(0, 1)])", "TypeError: list indices must be integers or slices, not tuple"},
		{"a text index into a string", "s = \"ab\"\nprint(s[\"a\"])", "TypeError: string indices must be integers, not 'str'"},
		{"a float index into a string", "s = \"ab\"\nprint(s[1.5])", "TypeError: string indices must be integers, not 'float'"},
		{"a store under a text index", "xs = [1, 2]\nxs[\"a\"] = 1", "TypeError: list indices must be integers or slices, not str"},
		{"an index bound to a text", "xs = [1, 2, 3]\nt = \"a\"\nprint(xs[t])", "TypeError: list indices must be integers or slices, not str"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			stdout, stderr, code := cliStreams(t, "--eval", tt.src)
			if code == 2 {
				t.Fatalf("%q exited 2 — the toolchain's own failure code:\n%s", tt.src, stderr)
			}
			if code != 3 {
				t.Fatalf("exit %d, want 3\nstdout %q\nstderr %q", code, stdout, stderr)
			}
			if out := strings.TrimSpace(stdout); out != "" {
				t.Fatalf("%q printed %q on the way to the raise; the reference prints nothing", tt.src, out)
			}
			if !strings.Contains(stderr, tt.want) {
				t.Fatalf("%q did not raise %q:\n%s", tt.src, tt.want, stderr)
			}
		})
	}
}

// The containers that keep answering, at the same altitude: positions, negative positions, a bool, a
// slice, a nested list, a string character, a dict key.
func TestAPositionStillIndexesThroughTheCLI(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		src  string
		want string
	}{
		{"xs = [1, 2]\nprint(xs[1])", "2"},
		{"xs = [1, 2]\nprint(xs[-1])", "2"},
		{"xs = [1, 2]\nprint(xs[True])", "2"},
		{"xs = [1, 2, 3]\nprint(xs[0:2])", "[1, 2]"},
		{"xs = [[1], [2]]\nprint(xs[1])", "[2]"},
		{"s = \"ab\"\nprint(s[1])", "b"},
		{"d = {\"a\": 1}\nprint(d[\"a\"])", "1"},
		{"xs = [1, 2, 3]\ni = 1\nprint(xs[i])", "2"},
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

// The whole matrix as one program, line for line against CPython.
func TestTheIndexKindProgramMatchesTheReferenceThroughTheCLI(t *testing.T) {
	t.Parallel()
	src, err := os.ReadFile("programs/list_index_kind.gy")
	if err != nil {
		t.Fatalf("read program: %v", err)
	}
	if strings.TrimSpace(string(src)) == "" {
		t.Fatal("the program is empty")
	}
	want, err := os.ReadFile("testdata/list_index_kind.expected")
	if err != nil {
		t.Fatalf("read the reference's answer: %v", err)
	}
	stdout, stderr, code := cliStreams(t, "--file", "programs/list_index_kind.gy")
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

func TestTheIndexKindProgramIsAnAssertedMatrixRow(t *testing.T) {
	t.Parallel()
	cases := conformanceCases()
	var row *lang.ConformanceCase
	for i := range cases {
		if cases[i].Name == "list_index_kind.gy" {
			row = &cases[i]
			break
		}
	}
	if row == nil {
		t.Fatal("no conformance matrix row runs list_index_kind.gy")
	}
	if !row.Asserted {
		t.Fatalf("row %s is not asserted: its stdout would run unwatched", row.ID)
	}
	if row.Oracle != "match" {
		t.Fatalf("row %s declares oracle %q; the compiled leg answers this program, so it is a match", row.ID, row.Oracle)
	}
	for _, want := range []string{"a text index raises TypeError", "a text store index raises TypeError", "a position out of range raises IndexError"} {
		if !strings.Contains(row.Source, want) {
			t.Fatalf("row %s runs a program without %q", row.ID, want)
		}
	}
}
