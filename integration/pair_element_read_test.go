package integration

import (
	"os"
	"strings"
	"testing"

	"github.com/donutloop/gusty/pkg/lang"
)

// A container literal's element that is itself a read, through the CLI (roadmap Gap R.224, ADR 0332).
//
// The symptom at this altitude is an exit code. `gustyc --eval 'print([{"a": "x"}["a"], 2])'` printed
// `[1, 2]` and exited 0 — the text's position in `@str_tab`, rendered as the number it is — while the three
// shapes whose element was a read of a *variable* or of a literal with a non-integer element stopped the
// build at exit 1 with a sentence about one-word positions. CPython answers all four, and every row below is
// judged against CPython: the expectation is what `python3` printed for the same source, never what this
// file remembers (Gap N's standing rule).

// TestAPairElementAnswersThroughTheCLI is the answering half: each row runs the same source through the
// reference and through the CLI, and the two have to agree line for line.
func TestAPairElementAnswersThroughTheCLI(t *testing.T) {
	t.Parallel()
	for _, src := range []string{
		`print([{"a": "x"}["a"], 2])`,
		`print([["a"][0], 2])`,
		`print([[1.5, 2][0], 3])`,
		"xs = [1, \"a\"]\nprint([xs[1], 2])",
		"xs = [1, \"a\"]\nprint([xs[0], xs[1]])",
		"xs = {\"a\": \"x\"}\nprint([xs[\"a\"], 2])",
		`print({1: {"a": "x"}["a"]})`,
		`print({{"a": "x"}["a"]: 1})`,
		`print({{"a": "x"}["a"]})`,
		`print({"k": {"a": "x"}["a"], "n": 2})`,
		`print([[None][0], True])`,
		`print([[True, False][1], 0])`,
		`print([["a", "b"][-1], 1])`,
	} {
		t.Run(strings.ReplaceAll(src, "\n", "; "), func(t *testing.T) {
			t.Parallel()
			want := pythonOutput(t, src+"\n")
			stdout, stderr, code := cliStreams(t, "--eval", src)
			if code == 2 {
				t.Fatalf("the toolchain failed on its own program:\n%s", stderr)
			}
			if code != 0 {
				t.Fatalf("exit %d, want 0: the reference answers this program\nstderr %q", code, stderr)
			}
			if got := strings.TrimSpace(stdout); got != strings.TrimSpace(want) {
				t.Fatalf("printed %q, the reference prints %q", got, strings.TrimSpace(want))
			}
		})
	}
}

// The raises inside an element are the program's exceptions, judged by exit class, by the silence on stdout
// and by the sentence on stderr — and exit 2, the code the contract reserves for a compiler bug, is
// forbidden on every one of them (ADR 0006, ADR 0214).
func TestATrapInsideALiteralElementExitsAsATrapThroughTheCLI(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		src      string
		contains string
	}{
		{`print([{"a": 1}["b"], 2])`, "KeyError: 'b'"},
		{"xs = [1, \"a\"]\nprint([xs[5], 1])", "IndexError: list index out of range"},
		{`print([[1, 2]["a"], 1])`, "TypeError: list indices must be integers or slices, not str"},
		{"xs = [1, 2]\nprint([xs[0][0], 1])", "TypeError: 'int' object is not subscriptable"},
	} {
		t.Run(strings.ReplaceAll(tt.src, "\n", "; "), func(t *testing.T) {
			t.Parallel()
			stdout, stderr, code := cliStreams(t, "--eval", tt.src)
			if code == 2 {
				t.Fatalf("the toolchain failed on its own program:\n%s", stderr)
			}
			if code == 0 {
				t.Fatalf("exit 0 with stdout %q: the reference raises here", stdout)
			}
			if code != 3 {
				t.Fatalf("exit %d, want the trap class 3\nstderr %q", code, stderr)
			}
			if stdout != "" {
				t.Fatalf("the trap printed %q before it raised", stdout)
			}
			if !strings.Contains(stderr, tt.contains) {
				t.Fatalf("stderr does not carry %q:\n%s", tt.contains, stderr)
			}
			if strings.Contains(stderr, "codegen:") {
				t.Fatalf("the program was refused by the compiler rather than raised by the runtime:\n%s", stderr)
			}
		})
	}
}

// The fold's shape stays the fold's, measured at the CLI: an all-integer literal read at an integer position
// emits a constant, allocates nothing and asks none of the object doors — the negative control beside the
// rows that now open them (ADR 0249's gate-and-door rule, asserted where a reader can see the IR).
func TestTheFoldRoadCarriesNoPairDoorThroughTheCLI(t *testing.T) {
	t.Parallel()
	for _, src := range []string{`print([[1, 2][0], 3])`, `print([1, 2][0])`, `print([{1: 2}[1], 3])`} {
		_, stderr, code := cliStreams(t, "--eval", src)
		if code != 0 {
			t.Fatalf("exit %d, want 0\nstderr %q", code, stderr)
		}
		out, virr, vcode := cliStreams(t, "--verify-llvm", src)
		if vcode != 0 || !strings.Contains(out, "module verified") {
			t.Fatalf("the module does not verify: exit %d, %q %q", vcode, out, virr)
		}
		ir, irErr, irCode := cliStreams(t, "--emit-llvm", src)
		if irCode != 0 {
			t.Fatalf("--emit-llvm exited %d:\n%s", irCode, irErr)
		}
		// Only the program's own block: the module carries the whole runtime library, and `rt_copy_list`
		// legitimately calls the element door — an assertion over the text would be an assertion about
		// the runtime, not about this program.
		body := cliMainBody(ir)
		for _, forbidden := range []string{"call i32 @rt_get_elem(", "call i32 @rt_dict_get_tagged(", "call i32 @rt_tag_of("} {
			if strings.Contains(body, forbidden) {
				t.Fatalf("%q opened the pair door (%s) for a read the fold answers:\n%s", src, forbidden, body)
			}
		}
	}
}

// cliMainBody is @main alone, out of the module --emit-llvm printed with the whole runtime attached.
func cliMainBody(ir string) string {
	start := strings.Index(ir, "define i32 @main(")
	if start < 0 {
		return ir
	}
	rest := ir[start:]
	if end := strings.Index(rest, "\n}\n"); end >= 0 {
		return rest[:end]
	}
	return rest
}

// The whole matrix as one program, line for line against CPython.
func TestThePairElementProgramMatchesTheReferenceThroughTheCLI(t *testing.T) {
	t.Parallel()
	src, err := os.ReadFile("programs/pair_element_read.gy")
	if err != nil {
		t.Fatalf("read program: %v", err)
	}
	if strings.TrimSpace(string(src)) == "" {
		t.Fatal("the program is empty")
	}
	want, err := os.ReadFile("testdata/pair_element_read.expected")
	if err != nil {
		t.Fatalf("read the reference's answer: %v", err)
	}
	stdout, stderr, code := cliStreams(t, "--file", "programs/pair_element_read.gy")
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

func TestThePairElementProgramIsAnAssertedMatrixRow(t *testing.T) {
	t.Parallel()
	cases := conformanceCases()
	var row *lang.ConformanceCase
	for i := range cases {
		if cases[i].Name == "pair_element_read.gy" {
			row = &cases[i]
			break
		}
	}
	if row == nil {
		t.Fatal("no conformance matrix row runs pair_element_read.gy")
	}
	if !row.Asserted {
		t.Fatalf("row %s is not asserted: its stdout would run unwatched", row.ID)
	}
	if row.Oracle != "match" {
		t.Fatalf("row %s declares oracle %q; the compiled leg answers this program, so it is a match", row.ID, row.Oracle)
	}
	for _, want := range []string{
		`print([{"a": "x"}["a"], 2])`,
		"a missing entry inside a literal raises KeyError",
		"a position past the end inside a literal raises IndexError",
	} {
		if !strings.Contains(row.Source, want) {
			t.Fatalf("row %s runs a program without %q", row.ID, want)
		}
	}
}
