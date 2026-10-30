package integration

// sorted_container_test.go — the membership, equality and element doors ask a `sorted`/`reversed` what
// its answer IS, through the CLI and against the pinned reference (roadmap **Gap R.253**, **ADR 0472**).
//
// The reference builds a container before it orders anything, and hands back a real list; this backend
// orders a literal by folding the elements into a compile-time constant. ADR 0333 made a container answer
// a PAIR — the door that decides an expression is a container and the door that builds it are one function
// — and ADR 0339 gave the `for` loop the two questions to ask of the two builtins whose job is to hand
// back a list. The doors beside the loop asked neither, and read the builtin's word instead. That word is
// `@.lst1`, a global whose layout is a length plus an array, and every helper that takes an i32 rejected
// it in `llc`, at the code ADR 0166 reserves for a compiler bug:
//
//	print(1 in sorted([3, 1, 2]))               %t2 = call i32 @rt_contains(i32 @.lst1, i32 1)
//	print(sorted([3,1,2]) == sorted([1,2,3]))   %t1 = icmp eq i32 @.lst1, @.lst2
//	xs = []                                     call void @rt_append_tagged(i32 %h2, i32 @.lst1, i32 0)
//	xs.append(sorted([3, 1, 2]))                → EXIT 2 for three programs CPython answers `True`
//
// and one shape away, `print(sorted([3, 1, 2]) == [1, 2, 3])` refused at exit 1 with ADR 0189's "comparing
// a container with *lang.Call needs a tagged value".
//
// Every expectation below was measured against the pinned CPython in the same case that asserts it — the
// answered rows re-run the reference so its column cannot rot — and exit 2 is forbidden on every row.
// `reversed` is the half with two answers: the loop and the membership test walk the flipped copy (that is
// what the iterator yields), while the comparison doors answer the reference's False, because an iterator
// has no `__eq__` and equals no list (**Gap R.251**, **Gap R.195** keep the rest of that story).

import (
	"os"
	"regexp"
	"strings"
	"testing"

	"github.com/donutloop/gusty/pkg/lang"
)

// sortedContainerCLIRows are the programs the reference answers, through `gustyc --file`.
var sortedContainerCLIRows = []struct {
	src  string
	want string
}{
	// the membership door, in both directions, over a fold the builtin answers
	{"print(1 in sorted([3, 1, 2]))", "True"},
	{"print(5 not in sorted([3, 1, 2]))", "True"},
	{"print(2 in reversed([1, 2, 3]))", "True"},
	{"print(1 in sorted([]))", "False"},
	{"print(1 in reversed([]))", "False"},
	{"print(3 in sorted([1, 2, 3], reverse=True))", "True"},
	// the equality door: ADR 0189's refusal, and the `icmp eq i32 @.lst1, @.lst2` that used to beat it
	{"print(sorted([3, 1, 2]) == [1, 2, 3])", "True"},
	// the equality door over the shapes ADR 0473 substituted a sequence for: a dict's keys and an ordering
	// handed another ordering were refusals here, naming Gaps R.572 and R.573, and answer today
	{"print(sorted({\"a\": 1}) == [\"a\"])", "True"},
	{"print(sorted(reversed([3, 1, 2])) == [1, 2, 3])", "True"},
	// the membership door over the substituted sequences
	{"print(sorted({\"b\": 1, \"a\": 2}) == [\"a\", \"b\"])", "True"},
	{"print(\"a\" in sorted(\"bca\"))", "True"},
	// a sorted TEXT haystack read by a number needle is the reference's `False` and ours is `True`: that row
	// is not an answer to assert here, it lives in `orderingArgumentFiledCLIDivergences` with both legs
	{"print([1, 2] == sorted([2, 1]))", "True"},
	{"print(sorted([3, 1, 2]) == sorted([1, 2, 3]))", "True"},
	{"print(sorted([]) == [])", "True"},
	{"print(sorted([3, 1, 2]) == [1, 2, 4])", "False"},
	{"print(sorted([3, 1, 2]) != [1, 2, 3])", "False"},
	// the argument reached some other way: a name, a slice, a call's answer, a keyed copy
	{"xs = [3, 1, 2]\nprint(sorted(xs) == [1, 2, 3])", "True"},
	{"xs = [3, 1, 2]\nprint(1 in sorted(xs))", "True"},
	{"xs = [3, 1, 2]\nprint(sorted(xs) == sorted(xs))", "True"},
	{"def b():\n    return [3, 1, 2]\nprint(1 in sorted(b()))", "True"},
	{"print(1 in sorted([3, 1, 2][0:2]))", "True"},
	{"print(1 in sorted([3, 1, 2], key=abs))", "True"},
	{"def f(vals):\n    return sorted(vals) == [1, 2, 3]\nprint(f([3, 1, 2]))", "True"},
	// text elements, ordered by the comparator rather than by arrival order
	{"print(\"a\" in sorted([\"b\", \"a\"]))", "True"},
	{"print(sorted([\"b\", \"a\"]) == [\"a\", \"b\"])", "True"},
	// a CONTAINER needle: content, not the two handles
	{"print(sorted([3, 1, 2]) in [[1, 2, 3]])", "True"},
	{"print(sorted([3, 1, 2]) in [[1, 2, 4]])", "False"},
	// an ITERATOR is not the copy it mirrors
	{"print(reversed([1, 2]) == [2, 1])", "False"},
	{"print(reversed([1, 2]) != [2, 1])", "True"},
	{"print(reversed([1, 2]) == reversed([1, 2]))", "False"},
	{"print(reversed([1, 2]) in [[2, 1]])", "False"},
	// the element door: a slot that holds the answer holds the HANDLE of it, with its tag
	{"print([sorted([3, 1, 2])])", "[[1, 2, 3]]"},
	{"print([sorted([3, 1, 2]), 1])", "[[1, 2, 3], 1]"},
	{"print({\"k\": sorted([3, 1, 2])})", "{'k': [1, 2, 3]}"},
	{"print((sorted([3, 1, 2]),))", "([1, 2, 3],)"},
	{"xs = []\nxs.append(sorted([3, 1, 2]))\nprint(xs)", "[[1, 2, 3]]"},
	{"xs = [0, 0]\nxs[0] = sorted([3, 1, 2])\nprint(xs)", "[[1, 2, 3], 0]"},
	{"d = {}\nd[\"k\"] = sorted([3, 1, 2])\nprint(d[\"k\"])", "[1, 2, 3]"},
	{"xs = [sorted([3, 1, 2])]\nprint(xs[0])\nprint(xs[0][0])\nprint(xs == [[1, 2, 3]])", "[1, 2, 3]\n1\nTrue"},
	{"xs = sorted([3, 1, 2])\nxs.append(9)\nprint(xs)", "[1, 2, 3, 9]"},
	// nested builtins, and the roads the row had no reason to touch
	{"print(2 in sorted(reversed([3, 1, 2])))", "True"},
	{"print(sorted(sorted([3, 1, 2])))", "[1, 2, 3]"},
	{"print(len(sorted([3, 1, 2])))", "3"},
	{"for v in sorted([3, 1, 2]):\n    print(v)", "1\n2\n3"},
	{"for v in reversed([1, 2, 3]):\n    print(v)", "3\n2\n1"},
	{"print(sorted([3, 1, 2]))", "[1, 2, 3]"},
	{"print(1 in [3, 1, 2])", "True"},
	{"if sorted([3, 1, 2]):\n    print(\"truthy\")", "truthy"},
	// the four doors asked of one call in one program — one ordering, or the lines disagree
	{
		"print(sorted([3, 1, 2]))\nfor v in sorted([3, 1, 2]):\n    print(v)\nprint(1 in sorted([3, 1, 2]))\nprint(sorted([3, 1, 2]) == [1, 2, 3])",
		"[1, 2, 3]\n1\n2\n3\nTrue\nTrue",
	},
}

// sortedContainerCLIRefusals are the shapes that stay refusals: each is a program a door cannot answer,
// and the honesty of the door is that it says so in words (exit 1) rather than answering at exit 0 or
// handing `llc` a module it rejects (exit 2). `sentence` is the fragment of the refusal a reader can act
// on; `roadmap` is the row that owns the missing half.
var sortedContainerCLIRefusals = []struct {
	src      string
	sentence string
	roadmap  string
}{
	{"print(1 in sorted([1, \"a\"]))", "more than one kind", "Gap R.248"},
	{"print(1 in sorted([2, 1]) == True)", "comparison chain", "Gap R.53"},
	{"print(1 in reversed([1, 2, 3]) == True)", "comparison chain", "Gap R.53"},
	// `print(1 in sorted("bca"))` used to be a row here. It no longer refuses — it answers `True` where the
	// reference answers `False`, which is a wrong answer rather than a door that cannot speak, so it is pinned
	// with BOTH values in `orderingArgumentFiledCLIDivergences` under **Gap R.575**, and a refusal row here
	// would only hide it (**ADR 0249**). The dict-key and nested-ordering equality rows moved to the answered
	// table when **ADR 0473** substituted the sequence those arguments denote.
}

// TestSortedContainerDoorsAnswerThroughTheCLI runs every answered row through the CLI and through CPython
// in the same case, so the reference column cannot drift from the program it cites.
func TestSortedContainerDoorsAnswerThroughTheCLI(t *testing.T) {
	t.Parallel()
	for _, tt := range sortedContainerCLIRows {
		t.Run(strings.ReplaceAll(tt.src, "\n", "; "), func(t *testing.T) {
			t.Parallel()
			stdout, stderr, code := cliStreams(t, "--eval", tt.src)
			if code == 2 {
				t.Fatalf("exit 2 on a program the reference runs: the contract's compiler-bug code\n%s\n%s", tt.want, stderr)
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

// TestSortedContainerDoorsRefuseThroughTheCLIWhatTheyCannotAnswer keeps the door narrow in the other
// direction: a shape the builtins themselves decline stays a refusal that names its missing half, at exit
// 1, and never becomes a fabricated verdict or a toolchain rejection.
func TestSortedContainerDoorsRefuseThroughTheCLIWhatTheyCannotAnswer(t *testing.T) {
	t.Parallel()
	for _, tt := range sortedContainerCLIRefusals {
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
			if !strings.Contains(stderr, "roadmap") {
				t.Fatalf("%q refused without naming the row that owns the missing half: %s", tt.src, strings.TrimSpace(stderr))
			}
		})
	}
}

// containerGlobalInHelperIR is the instruction shape Gap R.253 exists to remove — a container CONSTANT in
// the i32 a runtime helper reads as a handle. It is asserted at the CLI altitude too, because the CLI is
// where a user meets the module.
var containerGlobalInHelperIR = regexp.MustCompile(`(?m)(call i32 @rt_contains(?:_tagged)?\([^)]*@\.|call void @rt_(?:append|set_add|put_elem|set_elem|dict_put)(?:_tagged)?\([^)]*@\.|call i32 @rt_container_eq\([^)]*@\.|icmp eq i32 @\.)`)

// TestNoSortedAnswerReachesAHelperAsAConstant is the IR witness: the door builds the object, so the
// program's own functions hand registers to the runtime — and the counter-rows prove the guard has
// something to bite on, that the fold is still emitted where the value road answers, and that the door did
// not become the only road that ever builds these two calls.
func TestNoSortedAnswerReachesAHelperAsAConstant(t *testing.T) {
	t.Parallel()
	for _, src := range []string{
		"print(1 in sorted([3, 1, 2]))",
		"print(2 in reversed([1, 2, 3]))",
		"print(sorted([3, 1, 2]) == [1, 2, 3])",
		"print(sorted([3, 1, 2]) == sorted([1, 2, 3]))",
		"xs = []\nxs.append(sorted([3, 1, 2]))\nprint(xs)",
		"d = {}\nd[\"k\"] = sorted([3, 1, 2])\nprint(d[\"k\"])",
		"print([sorted([3, 1, 2])])",
		"xs = [3, 1, 2]\nprint(1 in sorted(xs))",
	} {
		t.Run(strings.ReplaceAll(src, "\n", "; "), func(t *testing.T) {
			t.Parallel()
			res, err := lang.Compile(src)
			if err != nil {
				t.Fatalf("compile: %v", err)
			}
			if m := containerGlobalInHelperIR.FindString(programFunctionBodies(t, res.IR)); m != "" {
				t.Fatalf("%q hands a container global to an i32 parameter (%s):\n%s", src, m, programFunctionBodies(t, res.IR))
			}
		})
	}
	// Counter-row 1: the membership door really does ask the runtime helper — for a register.
	t.Run("counter-row the membership helper is called", func(t *testing.T) {
		t.Parallel()
		res, err := lang.Compile("print(1 in sorted([3, 1, 2]))")
		if err != nil {
			t.Fatalf("compile: %v", err)
		}
		if body := programFunctionBodies(t, res.IR); !strings.Contains(body, "call i32 @rt_contains(i32 %") {
			t.Fatalf("the membership door emitted no @rt_contains call at all — the check above would then be satisfied by nothing:\n%s", body)
		}
	})
	// Counter-row 2: the VALUE position keeps the compile-time constant the fold has always emitted. The
	// door took the container doors and nothing else.
	t.Run("counter-row the fold still answers a value position", func(t *testing.T) {
		t.Parallel()
		res, err := lang.Compile("xs = sorted([3, 1, 2])\nprint(xs[0])")
		if err != nil {
			t.Fatalf("compile: %v", err)
		}
		if !strings.Contains(res.IR, "@.lst") {
			t.Fatalf("the fold is gone from the value position too — this row left it alone:\n%s", res.IR)
		}
	})
	// Counter-row 3: the builtins' own roads are untouched — `print` of a sorted literal still prints the
	// list, and the loop still walks it, so the new door is not the only thing standing between a program
	// and an answer.
	t.Run("counter-row the print and loop roads still answer", func(t *testing.T) {
		t.Parallel()
		stdout, _, code := cliStreams(t, "--eval", "print(sorted([3, 1, 2]))\nfor v in sorted([3, 1, 2]):\n    print(v)")
		if code != 0 || stdout != "[1, 2, 3]\n1\n2\n3\n" {
			t.Fatalf("the neighbour roads moved: code %d, stdout %q", code, stdout)
		}
	})
}

// TestSortedContainerProgramAnswersNothingTheReferenceDoesNot is the corpus program, line for line against
// what CPython printed for it.
func TestSortedContainerProgramAnswersNothingTheReferenceDoesNot(t *testing.T) {
	t.Parallel()
	src, err := os.ReadFile("programs/sorted_container.gy")
	if err != nil {
		t.Fatalf("read program: %v", err)
	}
	if strings.TrimSpace(string(src)) == "" {
		t.Fatal("programs/sorted_container.gy is empty")
	}
	pyOut, _, pyRunErr := lang.PythonRun(string(src))
	if pyRunErr != nil && strings.TrimSpace(pyOut) == "" {
		t.Fatalf("CPython could not run the program: %v\n%s", pyRunErr, pyOut)
	}
	if _, compErr := lang.Compile(string(src)); compErr != nil {
		t.Fatalf("the compiled backend refuses a program the reference runs: %v", compErr)
	}
	stdout, stderr, code := cliStreams(t, "--file", "programs/sorted_container.gy")
	if code == 2 {
		t.Fatalf("exit 2 on the corpus program (ADR 0166): %s", stderr)
	}
	if code != 0 {
		t.Fatalf("the corpus program left %d: %s", code, stderr)
	}
	if stdout != pyOut {
		t.Fatalf("the corpus program and CPython disagree:\n compiled: %q\n    py: %q", stdout, pyOut)
	}
	want, err := os.ReadFile("testdata/sorted_container.expected")
	if err != nil {
		t.Fatalf("read expectation: %v", err)
	}
	if stdout != string(want) {
		t.Fatalf("the corpus program moved from its recorded expectation:\n got: %q\nwant: %q", stdout, string(want))
	}
}
