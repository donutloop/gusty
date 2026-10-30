package integration

import (
	"os"
	"strings"
	"testing"

	"github.com/donutloop/gusty/pkg/lang"
)

// A slice of a list, through the CLI (roadmap Gap R.230, ADR 0335).
//
// `xs[i:j]` had one lowering for every base: copy the base's *words* into a fresh heap list. For a list whose
// slots are all numbers that is the whole answer. For a list whose slots describe themselves — a text slot, a
// float slot, a nested container — the copy carried words with no tag beside them, and every read of the copy
// read the word as the thing itself:
//
//	xs = [1.5, "a", [7]]
//	print(xs[0:2])   # CPython: [1.5, 'a']
//	                 # this toolchain before the row: [1, 0]  at exit 0
//	ys = xs[0:2]
//	print(ys)        # CPython: [1.5, 'a']
//	                 # this toolchain before the row: 1       at exit 0
//
// Two wrong numbers, both at the code that means "the program ran": the interned index of a text and the box
// handle of a float, printed as the integers they are not, and a heap handle printed where a list belongs.
// Every row below is judged against CPython: the expectation is what `python3` printed for the same source,
// never what this file remembers (Gap N's standing rule).

// sliceAnswerAnswered are the shapes the row opens. Each is a read of a copy — printed, measured, looped
// over, tested for truth, membership and equality, bound to a name, and sliced again.
var sliceAnswerAnswered = []struct {
	src  string
	want string
}{
	// The base is a name bound to a mixed literal: the shape the row was measured on.
	{"xs = [1.5, \"a\", [7]]\nprint(xs[0:2])", "[1.5, 'a']"},
	{"xs = [1.5, \"a\", [7]]\nprint(xs[2:3])", "[[7]]"},
	{"xs = [1.5, \"a\", [7]]\nprint(xs[0:0])", "[]"},
	{"xs = [1.5, \"a\", [7]]\nprint(xs[1:])", "['a', [7]]"},
	{"xs = [1.5, \"a\", [7]]\nprint(xs[:2])", "[1.5, 'a']"},
	{"xs = [1.5, \"a\", [7]]\nprint(xs[::-1])", "[[7], 'a', 1.5]"},
	// The base is a literal node: the sliced source becomes an object first, tagged, and the copy is taken
	// from the object rather than from a compile-time global's address.
	{"print([1.5, \"a\", [7]][0:2])", "[1.5, 'a']"},
	{"print([1, 2, 3][1:])", "[2, 3]"},
	{"print([\"a\", \"b\", \"c\"][1:])", "['b', 'c']"},
	// The base's slots are one kind: the plain word copy, and the plain printer. These are what a tagging fix
	// must leave alone.
	{"xs = [1, 2, 3]\nprint(xs[0:2])", "[1, 2]"},
	{"xs = [\"a\", \"b\"]\nprint(xs[0:1])", "['a']"},
	// The copy is a list, so the reads a list answers are the copy's reads.
	{"xs = [1.5, \"a\", [7]]\nys = xs[0:2]\nprint(ys)", "[1.5, 'a']"},
	{"xs = [1.5, \"a\", [7]]\nys = xs[0:2]\nprint(len(ys))", "2"},
	{"xs = [1.5, \"a\", [7]]\nys = xs[0:2]\nfor v in ys:\n    print(v)", "1.5\na"},
	{"xs = [1.5, \"a\", [7]]\nys = xs[0:2]\nprint(ys[0], ys[1])", "1.5 a"},
	{"xs = [1.5, \"a\", [7]]\nprint(not xs[0:0])", "True"},
	{"xs = [1.5, \"a\", [7]]\nprint(not xs[0:2])", "False"},
	{"xs = [1, 2]\nprint(2 in xs[0:2])", "True"},
	{"xs = [1.5, \"a\", [7]]\nprint(2 in xs[0:2])", "False"},
	{"xs = [1.5, \"a\", [7]]\nprint(xs[0:2] == [1.5, \"a\"])", "True"},
	{"xs = [1.5, \"a\", [7]]\nprint(xs[0:2] == [1.5])", "False"},
	// The base is a call's container answer: the slice asks the road that slices a container variable, so the
	// answer's own tags travel into the copy (ADR 0333's answer, read by ADR 0334's door, copied by this row).
	{"def b():\n    return [1, 2, 3]\nprint(b()[0:2])", "[1, 2]"},
	{"def b():\n    return [1.5, \"a\", [7]]\nprint(b()[0:2])", "[1.5, 'a']"},
	{"def b():\n    return [1.5, \"a\", [7]]\nys = b()[0:2]\nprint(ys)", "[1.5, 'a']"},
	{"def b():\n    return [1, 2, 3]\nprint(len(b()[0:2]))", "2"},
	{"def n():\n    return [10, 20, 30]\nprint(n()[1:])", "[20, 30]"},
	{"def b():\n    return [1, \"a\", None, [4]]\nprint(b()[1:3])", "['a', None]"},
	// A copy of a copy: the second door reads the object the first one built.
	{"xs = [1.5, \"a\", [7]]\nprint(xs[0:2][0:1])", "[1.5]"},
	{"xs = [1, 2, 3]\nprint(xs[0:2][0:1])", "[1]"},
	// A slot of a copy, and a copy of a copy read by position: the copy the binding door could always read
	// is read by the same doors unbound (roadmap Gap R.235, ADR 0459).
	{"xs = [1.5, \"a\", [7]]\nprint(xs[0:2][0])", "1.5"},
	{"xs = [1.5, \"a\", [7]]\nprint(xs[0:2][1])", "a"},
	{"xs = [1, 2, 3]\nprint(xs[0:2][1])", "2"},
	{"xs = [1.5, \"a\", [7]]\nprint(len(xs[0:2]))", "2"},
	// The base is an object the records reach only through another object: a slot that holds a list, and a
	// comprehension's answer. Both were refusals that named the missing record (Gap R.235).
	{"def m():\n    return {\"k\": [1.5, \"a\"]}\nprint(m()[\"k\"][0:1])", "[1.5]"},
	{"def m():\n    return {\"k\": [1.5, \"a\"]}\nprint(m()[\"k\"][1:2])", "['a']"},
	{"d = [[1, 2]]\nprint(d[0][0:1])", "[1]"},
	{"d = {\"k\": [1, 2, 3]}\nprint(d[\"k\"][1:3])", "[2, 3]"},
	{"print([x for x in [1, 2, 3]][0:2])", "[1, 2]"},
	{"ys = [3, 1, 2]\nprint([x for x in ys][0:2])", "[3, 1]"},
	{"print([x for x in range(4)][1:3])", "[1, 2]"},
	// A text slice is a text: the fold reads the interned characters, and this row leaves that road alone.
	{"s = \"abcdef\"\nprint(s[1:3])", "bc"},
	{"print(\"abcdef\"[::-1])", "fedcba"},
}

// TestASliceAnswersThroughTheCLI is the answering half: each row runs the same source through the reference
// and through the CLI, and the two have to agree line for line.
func TestASliceAnswersThroughTheCLI(t *testing.T) {
	t.Parallel()
	for _, tt := range sliceAnswerAnswered {
		t.Run(strings.ReplaceAll(tt.src, "\n", "; "), func(t *testing.T) {
			t.Parallel()
			want := pythonOutput(t, tt.src+"\n")
			stdout, stderr, code := cliStreams(t, "--eval", tt.src)
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

// TestTheTaggedCopyIsPrintedByThePrinterThatReadsATag is the row's claim asked of the emitted module rather
// than of the runtime: the copy is built by the tagged builder and printed by the tagged printer, and the
// untagged copy keeps the plain pair — the gate and the door growing together (ADR 0249) rather than the
// whole slice road being moved to the runtime.
func TestTheTaggedCopyIsPrintedByThePrinterThatReadsATag(t *testing.T) {
	t.Parallel()
	taggedIR, taggedErr, taggedCode := cliStreams(t, "--emit-llvm", "xs = [1.5, \"a\", [7]]\nprint(xs[0:2])")
	if taggedCode != 0 {
		t.Fatalf("--emit-llvm exited %d:\n%s", taggedCode, taggedErr)
	}
	tagged := cliMainBody(taggedIR)
	for _, want := range []string{"call i32 @rt_slice(", "@rt_tag_elem(", "call void @rt_print_list_mixed("} {
		if !strings.Contains(tagged, want) {
			t.Fatalf("a slice of a mixed list does not ask %s:\n%s", want, tagged)
		}
	}
	plainIR, plainErr, plainCode := cliStreams(t, "--emit-llvm", "xs = [1, 2, 3]\nprint(xs[0:2])")
	if plainCode != 0 {
		t.Fatalf("--emit-llvm exited %d:\n%s", plainCode, plainErr)
	}
	plain := cliMainBody(plainIR)
	if !strings.Contains(plain, "call i32 @rt_slice(") {
		t.Fatalf("a slice of a plain list no longer copies an object:\n%s", plain)
	}
	if strings.Contains(plain, "rt_print_list_mixed") {
		t.Fatalf("a slice of a plain list asks the tagged printer:\n%s", plain)
	}
}

// TestASliceTrapStillRaisesThroughTheCLI covers the copy read past its end and read as a container it is not.
// The reference raises these; the row keeps the raise a program-level exception — the right exit class, the
// sentence on stderr, silence on stdout, and never the code a compiler bug owns (ADR 0006, ADR 0166, ADR 0214).
func TestASliceTrapStillRaisesThroughTheCLI(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		src      string
		contains string
	}{
		{"xs = [1.5, \"a\", [7]]\nprint(xs[0:2][0][9])", "TypeError: 'float' object is not subscriptable"},
		{"xs = [1.5, \"a\", [7]]\ntry:\n    print(xs[0:2][0][9])\nexcept TypeError:\n    print(\"caught\")", "caught"},
		// A copy read past its end raises the reference's raise through the copy door, and the raise is a
		// program-level one a handler catches (Gap R.235's DoD).
		{"xs = [1, 2, 3]\nprint(xs[0:2][5])", "IndexError: list index out of range"},
		{"xs = [1, 2, 3]\ntry:\n    print(xs[0:2][5])\nexcept IndexError:\n    print(\"caught\")", "caught"},
		{"xs = [1, 2, 3]\nprint(xs[0:2][-4])", "IndexError: list index out of range"},
		{"s = \"abc\"\nprint(s[0:2][5])", "IndexError: string index out of range"},
		{"s = \"abc\"\ntry:\n    print(s[0:2][5])\nexcept IndexError:\n    print(\"caught\")", "caught"},
	} {
		t.Run(strings.ReplaceAll(tt.src, "\n", "; "), func(t *testing.T) {
			t.Parallel()
			stdout, stderr, code := cliStreams(t, "--eval", tt.src)
			if code == 2 {
				t.Fatalf("the toolchain failed on its own program:\n%s", stderr)
			}
			if strings.Contains(stderr, "codegen:") {
				t.Fatalf("the program was refused by the compiler rather than raised by the runtime:\n%s", stderr)
			}
			if !strings.Contains(stderr, tt.contains) && !strings.Contains(stdout, tt.contains) {
				t.Fatalf("neither stream carries %q:\nstdout %q\nstderr %q", tt.contains, stdout, stderr)
			}
		})
	}
}

// TestAPositionThatCannotSliceRefusesThroughTheCLI is the half the row leaves to other roads, asserted as a
// contract: the shape is refused at exit 1, with nothing on stdout, and with a sentence that names the missing
// record rather than the closed door's words. A refusal never arrives at exit 0 (ADR 0249, ADR 0166).
//
// The gate in front of the copy is what these rows are about. Before it, the slice road emitted `rt_slice`
// over whatever word the base lowered to and the caller printed that word — `print(m()["k"][0:1])` answered
// the copy's heap handle as `3`, and `print({1, 2}[0:1])` handed a set literal's i32 global to a helper whose
// argument is an i32 register, which `llc` rejected: exit 2, the code the contract reserves for a compiler bug
// (ADR 0166). The gate stands; the base question has since been asked too, so a base the records NAME as a set
// or a dict raises the reference's own sentence at exit 3 (**roadmap Gap R.234**, **ADR 0466**) and is witnessed
// in `integration/slice_base_kind_test.go`. What this table keeps is the shape whose base no record describes.
func TestAPositionThatCannotSliceRefusesThroughTheCLI(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		name    string
		src     string
		mustSay string
	}{
		// Two rows this table used to file left with **ADR 0459** — a copy of a slot that is itself a list,
		// and a copy of a comprehension's answer. Both answer now, and they are witnessed as answered rows
		// in `sliceAnswerAnswered` above, each against a live `python3`.
		//
		// A third pair left with **ADR 0466**: `print({1, 2}[0:1])` and `d = {1: 2}` / `print(d[0:1])` no longer
		// arrive here, because the slice road asks the base's kind first and raises the reference's sentence for
		// them. They are refused no longer, so they are not filed here; `TestASliceOfASetOrADictRaisesThroughTheCLI`
		// in `integration/slice_base_kind_test.go` is where they are judged, exit 3 beside a live `python3`.
		{
			// The base is a set, but spelled by its CONSTRUCTOR: no record names what the call hands back, so the
			// gate declines to name a kind and the road keeps its own refusal rather than raise a sentence the
			// compiler invented (roadmap Gap R.554; the reference raises 'set' object is not subscriptable).
			name:    "a slice of a set spelled by its constructor",
			src:     "print(set([1, 2])[0:1])",
			mustSay: "cannot slice",
		},
		{
			// The base IS described as a dict, but the bounds are expressions, so there is no key repr to name and
			// the miss would have to be built at run time (roadmap Gap R.555; the reference raises
			// KeyError: slice(0, 1, None)).
			name:    "a slice of a dict whose bounds the compiler cannot read",
			src:     "d = {1: 2}\ni = 0\nprint(d[i:i+1])",
			mustSay: "cannot slice",
		},
		{
			// Gap R.540: the name holds a slot the records only describe through the object it was read out of.
			name:    "a slice taken from a name bound by a slot read",
			src:     "def m():\n    return {\"k\": [1.5]}\n\nd = m()[\"k\"]\nprint(d[0:1])",
			mustSay: "cannot slice",
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			stdout, stderr, code := cliStreams(t, "--eval", tt.src)
			if code == 2 {
				t.Fatalf("exit 2 on a legal program — the code the contract reserves for a compiler bug (ADR 0166):\n%s", stderr)
			}
			if code != 1 {
				t.Fatalf("exit %d, want 1 (a position this backend declines to build):\nstdout %q\nstderr %q", code, stdout, stderr)
			}
			if strings.TrimSpace(stdout) != "" {
				t.Fatalf("the refusal printed %q on stdout: a refusal is not an answer", stdout)
			}
			if !strings.Contains(stderr, tt.mustSay) {
				t.Fatalf("the refusal does not name %q (ADR 0249):\n%s", tt.mustSay, stderr)
			}
		})
	}
}

// TestNoSliceModuleReturnsAGlobalToAValuePosition is the exit-2 guard at this altitude: the row's own history
// is a shape that reached `rt_slice` with a compile-time global in its place and had the module rejected, so
// the emitted module is checked for the shape rather than only run.
func TestNoSliceModuleReturnsAGlobalToAValuePosition(t *testing.T) {
	t.Parallel()
	for _, tt := range sliceAnswerAnswered {
		t.Run(strings.ReplaceAll(tt.src, "\n", "; "), func(t *testing.T) {
			t.Parallel()
			res, err := lang.Compile(tt.src)
			if err != nil {
				t.Fatalf("compile: %v", err)
			}
			if strings.Contains(res.IR, "ret i32 @.") || strings.Contains(res.IR, "@rt_slice(i32 @.") {
				t.Fatalf("a global sits in a value position — the shape that made `llc` reject the module (ADR 0166):\n%s",
					bodyAroundGlobalReturn(res.IR))
			}
			if !strings.Contains(res.IR, "@main") {
				t.Fatalf("the module has no @main to read: %s", res.IR)
			}
		})
	}
}

// TestTheSliceProgramMatchesTheReferenceThroughTheCLI is the whole row as one program, line for line against
// the recorded reference answer.
func TestTheSliceProgramMatchesTheReferenceThroughTheCLI(t *testing.T) {
	t.Parallel()
	src, err := os.ReadFile("programs/slice_answer.gy")
	if err != nil {
		t.Fatalf("read program: %v", err)
	}
	if strings.TrimSpace(string(src)) == "" {
		t.Fatal("the program is empty")
	}
	want, err := os.ReadFile("testdata/slice_answer.expected")
	if err != nil {
		t.Fatalf("read the reference's answer: %v", err)
	}
	stdout, stderr, code := cliStreams(t, "--file", "programs/slice_answer.gy")
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

// TestTheSliceProgramIsOnTheRecord is ADR 0302's rule at this altitude: a program whose record disappeared
// fails here rather than passing by having nothing to compare against.
func TestTheSliceProgramIsOnTheRecord(t *testing.T) {
	t.Parallel()
	src, err := os.ReadFile("programs/slice_answer.gy")
	if err != nil {
		t.Fatalf("read program: %v", err)
	}
	if !lang.HasGoldenAnswer(string(src)) {
		t.Fatal("programs/slice_answer.gy has no recorded answer: the record leg cannot check a program that is not on it")
	}
}
