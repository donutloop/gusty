package integration

import (
	"os"
	"regexp"
	"strings"
	"testing"

	"github.com/donutloop/gusty/pkg/lang"
)

// for_iterable_test.go — a `for` loop asks its ITERABLE what it is, through the CLI (roadmap Gap R.246,
// ADR 0339).
//
// The loop road had three ways to step over an iterable and no question to choose between them. A container
// the compiler could write down but could not NAME — `sorted([3, 1, 2])`, `reversed([1, 2, 3])`,
// `sorted([3, 1, 2], reverse=True)`, `sorted([])`, `reversed("abc")`, `sorted(xs)`, `sorted(b())`, a slice
// copy — fell to the counter road, which asks the iterable for a BOUND and, given a container, receives the
// address of one of its globals:
//
//	%t1 = icmp slt i32 %_ctr1.ld1, @.lst1        # llc: "global variable reference must have pointer type"
//	                                             # → EXIT 2, the code ADR 0166 reserves for a compiler bug
//
// and, whenever the comparison happened to succeed, printed the COUNTER — `0` once for
// `for v in sorted([3, 1, 2][0:2])` where CPython prints `1` and `3`, and nothing at all for
// `for v in sorted(["b", "a"])` where CPython prints `a` and `b`. Both are priority-1 answers: a value the
// program never wrote, at the exit code that means the program ran.
//
// Two doors close it, and each shares its implementation with the `print` answer beside it rather than
// re-writing it (ADR 0258): the FOLD (`sortedLiteralElems`, the same ordering `print(sorted(...))` reads)
// and the container pair, which builds the copy with the builtin's own @rt_sort / @rt_list_copy + @rt_reverse
// and names it, so the loop walks the copy exactly as it walks a list variable. What neither door reaches is
// REFUSED by name (ADR 0249), and every refusal here is checked never to reach exit 2.
//
// Every expectation is what the pinned CPython printed for the same source (Gap N's standing rule).

// forIterableCLIRows are the loops the row answers, run through the CLI and compared with the reference.
var forIterableCLIRows = []struct {
	src  string
	want string
}{
	// the shapes that used to make `llc` reject the module
	{"for v in sorted([3, 1, 2]):\n    print(v)", "1\n2\n3"},
	{"for v in reversed([1, 2, 3]):\n    print(v)", "3\n2\n1"},
	{"for v in sorted([3, 1, 2], reverse=True):\n    print(v)", "3\n2\n1"},
	{"for v in sorted([]):\n    print(v)", ""},
	// the shapes that answered nothing, or answered the counter
	{"for v in reversed(\"abc\"):\n    print(v)", "c\nb\na"},
	{"for v in sorted([\"b\", \"a\"]):\n    print(v)", "a\nb"},
	{"for v in sorted([3, 1, 2][0:2]):\n    print(v)", "1\n3"},
	{"xs = [3, 1, 2]\nfor v in sorted(xs):\n    print(v)", "1\n2\n3"},
	{"xs = [1, 2, 3]\nfor v in reversed(xs):\n    print(v)", "3\n2\n1"},
	{"def b():\n    return [3, 1, 2]\nfor v in sorted(b()):\n    print(v)", "1\n2\n3"},
	{"def b():\n    return [1, 2, 3]\nfor v in reversed(b()):\n    print(v)", "3\n2\n1"},
	// the operations nest, and apply in the order the reference applies them
	{"for v in sorted(reversed([3, 1, 2])):\n    print(v)", "1\n2\n3"},
	{"for v in reversed(sorted([1, 3, 2])):\n    print(v)", "3\n2\n1"},
	{"for v in sorted([2, 2, 1]):\n    print(v)", "1\n2\n2"},
	{"for c in reversed(\"caf\u00e9\"):\n    print(c)", "\u00e9\nf\na\nc"},
	// the loop's own semantics survive the fold
	{"for v in sorted([3, 1, 2]):\n    if v == 2:\n        break\n    print(v)", "1"},
	{"for v in sorted([3, 1, 2]):\n    if v == 2:\n        continue\n    print(v)", "1\n3"},
	{"for v in sorted([3, 1]):\n    print(v)\nelse:\n    print(\"done\")", "1\n3\ndone"},
	{"for v in reversed([7, 8]):\n    print(v * 10)", "80\n70"},
	// the roads the row is not allowed to disturb
	{"for v in [1, 2, 3]:\n    print(v)", "1\n2\n3"},
	{"for c in \"abc\":\n    print(c)", "a\nb\nc"},
	{"for i in range(3):\n    print(i)", "0\n1\n2"},
	{"for v in [1, \"a\", 2.5]:\n    print(v)", "1\na\n2.5"},
	{"def gen():\n    yield 1\n    yield 2\nfor v in gen():\n    print(v)", "1\n2"},
	{"def sorted(xs):\n    return [9]\nfor v in sorted([1]):\n    print(v)", "9"},
	// a loop variable's kind claim is its OWN loop's answer: measured before the row as `b`, `a`, `a`,
	// `(null)` and `b`, `a`, `b`, `a` — the second loop printing the first loop's answer (Gap R.252)
	{"words = [\"b\", \"a\"]\nxs = [1, 2]\nfor v in words:\n    print(v)\nfor v in xs:\n    print(v)", "b\na\n1\n2"},
	{"words = [\"b\", \"a\"]\nfor v in sorted(words):\n    print(v)\nfor v in range(2):\n    print(v)", "a\nb\n0\n1"},
	// the `print` answer the loop's ordering is shared with, in the same program
	{"xs = [3, 1, 2]\nprint(sorted(xs))\nfor v in sorted(xs):\n    print(v)", "[1, 2, 3]\n1\n2\n3"},
}

// forIterableRefusalRows are the loops neither door reaches. `names` is what the refusal has to say; `oracle`
// is the citation of what the reference answers (or the error it stops with), re-checked here rather than
// remembered.
var forIterableRefusalRows = []struct {
	src     string
	names   string
	oracle  string
	roadmap string
}{
	{"for v in sorted(\"bca\"):\n    print(v)", "`for` over sorted(\"bca\")", "a\nb\nc", "Gap R.247"},
	{"for v in sorted(range(3)):\n    print(v)", "not a container this backend can name", "0\n1\n2", "Gap R.247"},
	{"d = {'b': 1, 'a': 2}\nfor k in sorted(d):\n    print(k)", "its argument is a DICT", "a\nb", "Gap R.247"},
	{"xs = [\"b\", \"a\"]\nfor v in sorted(xs[0:2]):\n    print(v)", "comparator", "a\nb", "Gap R.247"},
	{"xs = [1.5, 0.5]\nfor v in sorted(xs):\n    print(v)", "ELEMENT kind", "0.5\n1.5", "Gap R.248"},
	{"for v in sorted([3, 1, 2], key=abs):\n    print(v)", "key=", "1\n2\n3", "Gap R.249"},
	{"xs = [3, 1, 2]\nfor v in sorted(sorted(xs)):\n    print(v)", "not a container this backend can name", "1\n2\n3", "Gap R.250"},
	{"for v in reversed({1: 2}):\n    print(v)", "`for` over reversed({1: 2})", "1", "Gap R.247"},
	// a second POSITIONAL argument is the reference's `key`, and the reference itself stops the program
	{"for v in sorted([3, 1, 2], True):\n    print(v)", "POSITIONAL argument", "TypeError: 'bool' object is not callable", "Gap R.249"},
}

// loopCounterAgainstContainerIR is the exit-2 instruction itself.
var loopCounterAgainstContainerIR = regexp.MustCompile(`icmp [a-z]+ i32 %[^,]+, @\.(lst|set|dict)\w*`)

// TestTheCompiledLoopAnswersWhatTheReferenceAnswers runs every answered row through the CLI and asks the
// pinned interpreter the same program.
func TestTheCompiledLoopAnswersWhatTheReferenceAnswers(t *testing.T) {
	for _, tt := range forIterableCLIRows {
		t.Run(strings.ReplaceAll(tt.src, "\n", "; "), func(t *testing.T) {
			stdout, stderr, code := cliStreams(t, "--eval", tt.src)
			if code == 2 {
				t.Fatalf("exit 2 on a program the reference runs — the code the contract reserves for a compiler bug (ADR 0166)\n%s", stderr)
			}
			if code != 0 {
				t.Fatalf("exit %d, want 0; the reference prints %q\nstderr %q", code, tt.want, stderr)
			}
			if got := strings.TrimRight(stdout, "\n"); got != tt.want {
				t.Fatalf("the compiled loop printed %q, want %q", got, tt.want)
			}
			want := strings.TrimRight(pythonOutput(t, tt.src+"\n"), "\n")
			if want != tt.want {
				t.Fatalf("the `want` column is a citation: CPython answers %q for this program, the table says %q", want, tt.want)
			}
		})
	}
}

// TestTheLoopNeverComparesItsCounterWithAContainerThroughTheCLI asks the emitted module what it contains: no
// module this suite compiles may put a container global where an i32 count belongs, whether the loop was
// answered by the fold, by the runtime road, or refused by the guard.
func TestTheLoopNeverComparesItsCounterWithAContainerThroughTheCLI(t *testing.T) {
	srcs := []string{}
	for _, tt := range forIterableCLIRows {
		srcs = append(srcs, tt.src)
	}
	for _, tt := range forIterableRefusalRows {
		srcs = append(srcs, tt.src)
	}
	for _, src := range srcs {
		t.Run(strings.ReplaceAll(src, "\n", "; "), func(t *testing.T) {
			res, err := lang.Compile(src)
			if err != nil {
				return // a refusal is the other honest answer; the IR cannot contain what was never emitted
			}
			if m := loopCounterAgainstContainerIR.FindString(res.IR); m != "" {
				t.Fatalf("the module compares a loop counter with a container global (%s) — the shape llc rejects and the contract files as a compiler bug (ADR 0166):\n%s", m, res.IR)
			}
		})
	}
}

// TestTheRuntimeRoadWalksTheRuntimesOwnHelpers is ADR 0258's claim asked of the module: the loop's answer for
// a heap list is the SAME helper call the `sorted` answer emits, so a fix to one cannot leave the other
// behind.
func TestTheRuntimeRoadWalksTheRuntimesOwnHelpers(t *testing.T) {
	for _, tc := range []struct {
		src  string
		need string
	}{
		{"xs = [3, 1, 2]\nfor v in sorted(xs):\n    print(v)", "call void @rt_sort"},
		{"for v in sorted([\"b\", \"a\"]):\n    print(v)", "call void @rt_sort"},
		{"for v in sorted([3, 1, 2][0:2]):\n    print(v)", "call void @rt_sort"},
		{"def b():\n    return [3, 1, 2]\nfor v in sorted(b()):\n    print(v)", "call void @rt_sort"},
		{"xs = [1, 2, 3]\nfor v in reversed(xs):\n    print(v)", "call void @rt_reverse"},
		{"def b():\n    return [1, 2, 3]\nfor v in reversed(b()):\n    print(v)", "call void @rt_reverse"},
	} {
		t.Run(strings.ReplaceAll(tc.src, "\n", "; "), func(t *testing.T) {
			res, err := lang.Compile(tc.src)
			if err != nil {
				t.Fatalf("%q refused (%v); this loop is answered by the runtime road and must not be refused", tc.src, err)
			}
			if !strings.Contains(res.IR, tc.need) {
				t.Fatalf("%q did not emit %q — the loop is not walking the container helper the builtin uses (ADR 0258)\n%s", tc.src, tc.need, res.IR)
			}
		})
	}
}

// TestWhatTheLoopRowStillCannotAnswerThroughTheCLI is the guard at the CLI altitude: a loop the backend
// cannot walk is refused, the refusal names the iterable and the half that is missing, and none of these
// programs reaches exit 2 — the code the contract reserves for a module the compiler wrote and LLVM refused.
func TestWhatTheLoopRowStillCannotAnswerThroughTheCLI(t *testing.T) {
	for _, tc := range forIterableRefusalRows {
		t.Run(strings.ReplaceAll(tc.src, "\n", "; "), func(t *testing.T) {
			stdout, stderr, code := cliStreams(t, "--eval", tc.src)
			if code == 2 {
				t.Fatalf("exit 2 on a program the reference runs: this is the code the contract reserves for a compiler bug (ADR 0166)\n%s", stderr)
			}
			if code == 0 {
				t.Fatalf("%q answered %q at exit 0; roadmap %s filed this as a refusal, so the row moved — move it with the answer (the reference prints %q)", tc.src, stdout, tc.roadmap, tc.oracle)
			}
			joined := stderr + stdout
			if !strings.Contains(joined, tc.names) {
				t.Fatalf("%q refused with %q, which does not name %q (ADR 0249); filed as roadmap %s", tc.src, strings.TrimSpace(joined), tc.names, tc.roadmap)
			}
			// Every refusal names the roadmap door it stands at; a refusal that speaks AS THE LOOP names
			// the answer it refused to give as well (the guard's own sentence). A refusal from the
			// builtin's door beside it — `sorted(key=)` — speaks for that door and is not required to
			// recite this row's sentence.
			for _, need := range []string{"roadmap L11.1"} {
				if !strings.Contains(joined, need) {
					t.Fatalf("%q refused without saying %q: a refusal must name the half that is missing (ADR 0249)\n%s", tc.src, need, joined)
				}
			}
			if strings.Contains(joined, "`for` over") {
				for _, need := range []string{"ADR 0339", "repeat count"} {
					if !strings.Contains(joined, need) {
						t.Fatalf("%q refused as the loop without saying %q (ADR 0249)\n%s", tc.src, need, joined)
					}
				}
			}
			if strings.HasPrefix(tc.oracle, "TypeError") {
				return // the reference stops this program too; the citation is its error, not its stdout
			}
			want := strings.TrimRight(pythonOutput(t, tc.src+"\n"), "\n")
			if want != tc.oracle {
				t.Fatalf("the `oracle` column of the filed row is a citation: CPython answers %q for this program, the row says %q", want, tc.oracle)
			}
		})
	}
}

// TestTheLoopProgramMatchesTheReferenceThroughTheCLI is the whole row as one program, line for line against
// the recorded reference answer.
func TestTheLoopProgramMatchesTheReferenceThroughTheCLI(t *testing.T) {
	src, err := os.ReadFile("programs/for_iterables.gy")
	if err != nil {
		t.Fatalf("read program: %v", err)
	}
	if strings.TrimSpace(string(src)) == "" {
		t.Fatal("the program is empty")
	}
	want, err := os.ReadFile("testdata/for_iterables.expected")
	if err != nil {
		t.Fatalf("read the reference's answer: %v", err)
	}
	stdout, stderr, code := cliStreams(t, "--file", "programs/for_iterables.gy")
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

// TestTheLoopProgramIsOnTheRecord is ADR 0302's rule at this altitude: a program whose record disappeared
// fails here rather than passing by having nothing to compare against.
func TestTheLoopProgramIsOnTheRecord(t *testing.T) {
	src, err := os.ReadFile("programs/for_iterables.gy")
	if err != nil {
		t.Fatalf("read program: %v", err)
	}
	if !lang.HasGoldenAnswer(string(src)) {
		t.Fatal("integration/programs/for_iterables.gy is not on the record")
	}
}
