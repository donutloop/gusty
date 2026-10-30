package integration

// sorted_ordering_argument_test.go — the ordering doors ask an iterable what it IS, through the CLI and
// against the pinned reference (roadmap **Gap R.247**, **ADR 0473**).
//
// The reference builds a container from an ITERABLE before it orders anything: `sorted("bca")` is a list of
// one-character strings, `sorted(d)` is a list of the dict's KEYS, `sorted(range(3))` is a list of integers.
// This backend's container doors answered that question with a spelling — `staticContainerKind`, a record of
// what a NAME holds — so the three iterables a program could not name fell out of every door at once:
//
//	for v in sorted("bca")        refused      (the reference prints a, b, c)
//	for k in sorted(d)            refused      (the reference prints its keys)
//	for v in sorted(range(3))     refused      (the reference prints 0, 1, 2)
//	print(sorted("bca"))          printed 0    — the heap handle, at exit 0
//	print(sorted(range(3)))       printed the address of @.lst1 with %d
//	xs = ["b", "a"]               printed ['b', 'a'] — @rt_sort chose its comparator from the
//	print(sorted(xs[0:2]))                       argument's SPELLING, so text went by arrival
//
// The row substitutes the list the reference would build, once, asked as one question (`orderingSequenceArg`)
// that the fold, the loop, the print door, the equality and membership doors and the BINDING all ask; and it
// asks @rt_sort for its comparator from the OBJECT. Every expectation below was measured against the pinned
// CPython in the same case that asserts it, and exit 2 is forbidden on every row.

import (
	"regexp"
	"strings"
	"testing"

	"github.com/donutloop/gusty/pkg/lang"
)

// orderingArgumentIR is the whole module the CLI's own compiler entry emitted — the fold's globals live
// outside the function bodies, and the counter-rows below are about exactly those.
func orderingArgumentIR(t *testing.T, src string) string {
	t.Helper()
	res, err := lang.Compile(src)
	if err != nil {
		t.Fatalf("%q did not compile: %v", src, err)
	}
	return res.IR
}

// orderingArgumentCLIRows are the programs the reference answers, through `gustyc --eval`.
var orderingArgumentCLIRows = []struct {
	src  string
	want string
}{
	// a TEXT is its characters, in code-point order — `é` is one member, not two bytes
	{`print(sorted("bca"))`, "['a', 'b', 'c']"},
	{`print(sorted("café"))`, "['a', 'c', 'f', 'é']"},
	{`print(sorted(""))`, "[]"},
	{`print(sorted("bca", reverse=True))`, "['c', 'b', 'a']"},
	{`t = "bca"` + "\n" + `print(sorted(t))`, "['a', 'b', 'c']"},
	{`for v in sorted("bca"):` + "\n    " + `print(v)`, "a\nb\nc"},
	// a DICT is its KEYS, in the order the entries were written
	{`print(sorted({"b": 1, "a": 2}))`, "['a', 'b']"},
	{`print(sorted({3: 1, 1: 2}))`, "[1, 3]"},
	{`d = {"b": 1, "a": 2}` + "\n" + `for k in sorted(d):` + "\n    " + `print(k)`, "a\nb"},
	{`for v in reversed({1: 2}):` + "\n    " + `print(v)`, "1"},
	// a `range(...)` the program wrote out is its integers
	{`print(sorted(range(3)))`, "[0, 1, 2]"},
	{`print(sorted(range(2, 5)))`, "[2, 3, 4]"},
	{`print(sorted(range(5, 2, -1)))`, "[3, 4, 5]"},
	{`print(sorted(range(0)))`, "[]"},
	{`print(sorted(range(3), reverse=True))`, "[2, 1, 0]"},
	{`for v in sorted(range(3)):` + "\n    " + `print(v)`, "0\n1\n2"},
	// the doors beside the loop read the same sequence the print door prints
	{`print("a" in sorted("bca"))`, "True"},
	{`print(1 in sorted(range(3)))`, "True"},
	{`print(sorted({"b": 1, "a": 2}) == ["a", "b"])`, "True"},
	{`print([sorted("bca")])`, "[['a', 'b', 'c']]"},
	{`print([sorted({"b": 1, "a": 2})])`, "[['a', 'b']]"},
	{`print([sorted(range(2))])`, "[[0, 1]]"},
	{`print(sorted(sorted("bca")))`, "['a', 'b', 'c']"},
	// the OBJECT chooses the comparator: the exit-0 wrong answers, in both directions
	{`xs = ["b", "a"]` + "\n" + `print(sorted(xs[0:2]))`, "['a', 'b']"},
	{`xs = ["b", "a"]` + "\n" + `print(sorted(xs[0:2], reverse=True))`, "['b', 'a']"},
	// the BINDING: the name receives the list and says what its slots hold
	{`xs = sorted("bca")` + "\n" + `print(xs)`, "['a', 'b', 'c']"},
	{`xs = sorted("bca")` + "\n" + `print(xs[0])`, "a"},
	{`xs = sorted("café")` + "\n" + `print(xs[3])`, "é"},
	{`xs = sorted("bca")` + "\n" + `for v in xs:` + "\n    " + `print(v)`, "a\nb\nc"},
	{`xs = sorted("bca")` + "\n" + `print(xs[0:2])`, "['a', 'b']"},
	{`xs = sorted("bca")` + "\n" + `print(xs[0] + "x")`, "ax"},
	{`xs = sorted("bca")` + "\n" + `print(xs == ["a", "b", "c"])`, "True"},
	{`xs = sorted(range(3))` + "\n" + `print(xs[1])`, "1"},
	{`xs = sorted({"b": 1, "a": 2})` + "\n" + `print(xs[1])`, "b"},
	// the neighbours the row had no business touching
	{`print(sorted([3, 1, 2]))`, "[1, 2, 3]"},
	{`print(sum(sorted([3, 1, 2])))`, "6"},
	{`xs = [3, 1, 2]` + "\n" + `for v in reversed(xs):` + "\n    " + `print(v)`, "2\n1\n3"},
}

// orderingArgumentCLIRefusals are the shapes the doors still decline at the CLI, in words that describe the
// shape they were handed (ADR 0249), at exit 1 — never with a fabricated verdict and never through the
// toolchain.
var orderingArgumentCLIRefusals = []struct {
	src      string
	sentence string
	roadmap  string
	cites    bool // the sentence itself names the row that owns the half (ADR 0166)
}{
	{`for v in sorted([1.5, 0.5]):` + "\n    " + `print(v)`, "its elements are not all integers or all text", "Gap R.248", true},
	{`xs = [1, "a"]` + "\n" + `for v in sorted(xs):` + "\n    " + `print(v)`, "ELEMENT kind", "Gap R.248", true},
	{`for v in sorted({1, 2}):` + "\n    " + `print(v)`, "its argument is a SET", "Gap R.247", true},
	{`for v in sorted([3, 1, 2], True):` + "\n    " + `print(v)`, "second POSITIONAL argument", "Gap R.249", true},
	{`n = 3` + "\n" + `for v in sorted(range(n)):` + "\n    " + `print(v)`, "whose bound the program did not write", "Gap R.250", true},
	{`print(sorted("bca")[0])`, "index requires an inline list/dict/set literal", "Gap R.576", false},
	{`xs = sorted("bca")` + "\n" + `yy = xs` + "\n" + `print(yy[0])`, "index of a non-literal variable", "Gap R.578", false},
}

// orderingArgumentFiledCLIDivergences pins, at the CLI altitude, the shapes this row measured and filed
// rather than fixed: the compiled leg prints WHAT IT PRINTS, the reference prints something else, and the row
// that owns the fix is named. `want` is asserted exactly — a filed row is not a TODO, and one that quietly
// changed answer is the next cycle's regression.
var orderingArgumentFiledCLIDivergences = []struct {
	src      string
	compiled string
	row      string
}{
	{`print(sorted([1.5, 0.5]))`, "[1.5, 0.5]", "Gap R.248"}, // the reference answers [0.5, 1.5]
	{`print(1 in sorted("bca"))`, "True", "Gap R.575"},       // a number needle against interned payloads
	{`print(reversed("abc"))`, "0", "Gap R.251"},             // the iterator's handle, printed as a number
}

// TestTheFiledOrderingDivergencesStillHold runs the filed rows through the CLI: exit 0, the compiled answer
// exactly, and the reference NOT agreeing — a filed row whose legs have silently converged is a row that
// should have been deleted, and the failure says so.
func TestTheFiledOrderingDivergencesStillHold(t *testing.T) {
	t.Parallel()
	for _, tt := range orderingArgumentFiledCLIDivergences {
		t.Run(strings.ReplaceAll(tt.src, "\n", "; "), func(t *testing.T) {
			t.Parallel()
			stdout, stderr, code := cliStreams(t, "--eval", tt.src)
			if code != 0 {
				t.Fatalf("%q left %d; the filed row %s expects the compiled answer %q at exit 0\n%s", tt.src, code, tt.row, tt.compiled, stderr)
			}
			if got := strings.TrimRight(stdout, "\n"); got != tt.compiled {
				t.Fatalf("%q printed %q, but the filed row %s pins %q — the divergence moved; update the row and the roadmap together", tt.src, got, tt.row, tt.compiled)
			}
			if py := strings.TrimRight(pythonOutput(t, tt.src+"\n"), "\n"); py == tt.compiled {
				t.Fatalf("%q: the reference now answers %q, the same as the compiled leg — %s is fixed, and this row has to go with it", tt.src, py, tt.row)
			}
		})
	}
}

// printfPrintsAContainerGlobal is the shape the print door exists to prevent, at the CLI altitude: the
// ordering's compile-time constant handed to `%d` instead of to a container printer.
var printfPrintsAContainerGlobal = regexp.MustCompile(`printf\([^)]*i32 @\.`)

// TestTheOrderingArgumentAnswersThroughTheCLI runs every answered row through the CLI and through CPython in
// the same case, so the reference column cannot drift from the program it cites.
func TestTheOrderingArgumentAnswersThroughTheCLI(t *testing.T) {
	t.Parallel()
	for _, tt := range orderingArgumentCLIRows {
		t.Run(strings.ReplaceAll(tt.src, "\n", "; "), func(t *testing.T) {
			t.Parallel()
			stdout, stderr, code := cliStreams(t, "--eval", tt.src)
			if code == 2 {
				t.Fatalf("exit 2 on a program the reference runs: the contract's compiler-bug code\n%s", stderr)
			}
			if code != 0 {
				t.Fatalf("%q left %d: %s\nstdout %q", tt.src, code, strings.TrimSpace(stderr), stdout)
			}
			if got := strings.TrimRight(stdout, "\n"); got != tt.want {
				t.Fatalf("%q printed %q, want %q", tt.src, got, tt.want)
			}
			if py := strings.TrimRight(pythonOutput(t, tt.src+"\n"), "\n"); py != tt.want {
				t.Fatalf("the CPython column of this row is stale: it answers %q, the row says %q", py, tt.want)
			}
		})
	}
}

// TestTheOrderingArgumentRefusesThroughTheCLI keeps the door narrow: a shape no door can name stays a
// refusal that says which half is missing, at exit 1.
func TestTheOrderingArgumentRefusesThroughTheCLI(t *testing.T) {
	t.Parallel()
	for _, tt := range orderingArgumentCLIRefusals {
		t.Run(strings.ReplaceAll(tt.src, "\n", "; "), func(t *testing.T) {
			t.Parallel()
			stdout, stderr, code := cliStreams(t, "--eval", tt.src)
			if code == 2 {
				t.Fatalf("exit 2 on a program this backend refuses in words: %s\n%s", tt.roadmap, stderr)
			}
			if code == 0 {
				t.Fatalf("%q answered %q at exit 0; the door is meant to refuse it (%s)", tt.src, stdout, tt.roadmap)
			}
			if code != 1 {
				t.Fatalf("%q left %d; a front-end refusal is exit 1 (ADR 0166's contract)\n%s", tt.src, code, stderr)
			}
			if !strings.Contains(stderr, tt.sentence) {
				t.Fatalf("%q refused without the sentence a reader can act on (%q): %s", tt.src, tt.sentence, strings.TrimSpace(stderr))
			}
			if tt.cites && !strings.Contains(stderr, "roadmap") {
				t.Fatalf("%q refused without naming the row that owns the missing half: %s", tt.src, strings.TrimSpace(stderr))
			}
		})
	}
}

// TestNoOrderingAnswerIsPrintedByFormat is the IR witness at the CLI altitude: the ordering's answer reaches
// a container printer, never `%d` — and the counter-rows show the guard has something to bite on, that the
// value position still folds its constant and the row did not delete a road it was not asked about.
func TestNoOrderingAnswerIsPrintedByFormat(t *testing.T) {
	t.Parallel()
	for _, tt := range orderingArgumentCLIRows {
		ir := orderingArgumentIR(t, tt.src)
		if printfPrintsAContainerGlobal.MatchString(ir) {
			t.Fatalf("%q prints a container global with %%d — the ordering's answer never reached the printer", tt.src)
		}
	}
	// The text sort asks the runtime door for its TEXT comparator (mode 1) rather than keeping a
	// compile-time copy of an ordering this backend would have to keep in step with strcmp.
	if ir := orderingArgumentIR(t, `print(sorted("bca"))`); !regexp.MustCompile(`call void @rt_sort\(i32 %[^,]+, i32 1\)`).MatchString(ir) {
		t.Fatalf("the text sort did not ask @rt_sort for its text comparator")
	}
	// The counter-row: the value position keeps the constant **ADR 0472** left alone.
	if ir := orderingArgumentIR(t, "xs = sorted([3, 1, 2])\nprint(xs[0])"); !strings.Contains(ir, "@.lst") {
		t.Fatalf("the value road no longer emits the fold's constant — the repair deleted a road it was not asked about")
	}
	// …and a plain integer sort keeps the numeric comparator (mode 0), so the text claim above cannot be
	// satisfied by sending everything through the text comparator.
	if ir := orderingArgumentIR(t, `print(sorted([3, 1, 2]))`); !regexp.MustCompile(`call void @rt_sort\(i32 %[^,]+, i32 0\)`).MatchString(ir) {
		t.Fatalf("the numeric sort did not ask @rt_sort for its numeric comparator")
	}
}
