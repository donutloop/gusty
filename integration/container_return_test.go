package integration

import (
	"os"
	"strings"
	"testing"

	"github.com/donutloop/gusty/pkg/lang"
)

// A container handed back by a call, through the CLI (roadmap Gap R.67, ADR 0333).
//
// The symptom at this altitude was an exit code the contract reserves for a compiler bug.
//
//	gustyc --eval 'def build():
//	    return [1, 2]
//	print(build())'
//	# llc: "global variable reference must have pointer type: ret i32 @.lst1"
//	# gustyc: LLVM rejected the module; this is a compiler bug, not a source error   → exit 2
//
// The same was true of `{1, 2}`, `{1: 2}`, `[]`, `{}`, `set()`, a literal nested in another, and a
// `return` reached through an `if`. The sibling shape — build the container with `xs.append(1)` and
// `return xs` — did build a module, and then answered `print(build())` with `0`: the heap handle,
// printed as the number the answer is not, at the code of success. `for v in build():` printed nothing
// at all and `if build():` called a non-empty list false, both at exit 0 as well.
//
// Every expectation below is CPython's, read off the reference at test time; the refusal rows are
// judged by exit class and by the sentence, and exit 2 is forbidden on all of them (ADR 0166).

// containerAnswerAnswered are the shapes the row answers: the three kinds, the ways a body arrives at
// one, and every position that reads a container.
var containerAnswerAnswered = []string{
	"def build():\n    return [1, 2]\n\nprint(build())",
	"def build():\n    return {1, 2}\n\nprint(build())",
	"def build():\n    return {\"a\": 1}\n\nprint(build())",
	"def build():\n    return []\n\nprint(build())",
	"def build():\n    return {}\n\nprint(build())",
	"def build():\n    return set()\n\nprint(build())",
	"def build():\n    return list()\n\nprint(build())",
	"def build():\n    return [1, \"a\", None, True, [7]]\n\nprint(build())",
	"def build():\n    return [[1, 2], [3]]\n\nprint(build())",
	"def build():\n    xs = []\n    xs.append(1)\n    xs.append(2)\n    return xs\n\nprint(build())",
	"def build():\n    xs = []\n    for i in range(3):\n        xs.append(i)\n    return xs\n\nprint(build())",
	"def build():\n    d = {}\n    d[\"a\"] = \"x\"\n    return d\n\nprint(build())",
	"def build():\n    return [x for x in [1, 2]]\n\nprint(build())",
	"def build():\n    return [1, 2]\n\ndef g():\n    return build()\n\nprint(g())",
	"def build(n):\n    return [n, n + 1]\n\nprint(build(4))",
	"def build():\n    return [1, 2]\n\nprint(str(build()), repr(build()))",
	"def build():\n    return [1, 2]\n\nprint(len(build()), 2 in build(), 7 in build(), 7 not in build())",
	"def build():\n    return [1, 2]\n\nfor v in build():\n    print(v)",
	"def build():\n    return {\"a\": 1, \"b\": 2}\n\nfor k in build():\n    print(k)",
	"def build():\n    return [1, 2]\n\nif build():\n    print(\"t\")\nelse:\n    print(\"f\")",
	"def build():\n    return []\n\nif build():\n    print(\"t\")\nelse:\n    print(\"f\")",
	"def build():\n    return [1, 2]\n\nprint(not build())",
	"def build():\n    return [1, 2]\n\nys = build()\nprint(ys, len(ys), ys[0], ys[-1])",
	"def build():\n    return [1, 2]\n\nys = build()\nys.append(3)\nprint(ys)",
	"def build():\n    return [1, 2]\n\nprint(build() == [1, 2], build() == [1, 3])",
	"def build():\n    return {\"a\": 1}\n\nys = build()\nprint(ys[\"a\"], \"a\" in ys)",
	"def build():\n    return [1, 2]\n\nprint(build(), build())",
	"def build():\n    return [1]\nprint(build())",
	"def num():\n    return 5\n\nprint(num() + 1)",
	"def txt():\n    return \"ab\"\n\nprint(txt().upper())",
	"def dbl():\n    return 1.5\n\nprint(dbl())",
	"def verdict():\n    return True\n\nprint(verdict())",
}

// TestTheContainerAnswerAnswersThroughTheCLI is the answering half: the same source, the reference and
// this toolchain, agreeing line for line — with the compiler-bug code forbidden on every row.
func TestTheContainerAnswerAnswersThroughTheCLI(t *testing.T) {
	t.Parallel()
	for _, src := range containerAnswerAnswered {
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

// The refusals. Each position keeps one word for a whole value and has no road that reads a container
// answer, so the row is honest about it: exit 1, a sentence naming the function, the door that does
// answer and the roadmap row that owns the rest (ADR 0249), and never the code a compiler bug owns.
func TestAPositionThatCannotReadAContainerAnswerRefusesThroughTheCLI(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		name string
		src  string
		mine bool // the refusal is this row's own sentence, not another road's
	}{
		{"a container literal's element", "def build():\n    return [1, 2]\n\nprint([build(), 3])", true},
		{"an answer added to another container", "def build():\n    return [1, 2]\n\nprint(build() + [9])", true},
		{"a format field", "def build():\n    return [1, 2]\n\nprint(f\"{build()}\")", true},
		// Two positions the container answer never reached: the method road and the length road each
		// refused this program in their own words before this row existed, and they keep those words —
		// the row's contract is that a refusal names what is missing, not that every refusal is this
		// table's to make (ADR 0249). They are here so the exit class is witnessed all the same.
		{"an answer as a method receiver", "def build():\n    return [1, 2]\n\nprint(build().append(3))", false},
		{"an answer handed to a parameter", "def build():\n    return [1, 2]\n\ndef takes(xs):\n    return len(xs)\n\nprint(takes(build()))", false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			stdout, stderr, code := cliStreams(t, "--eval", tt.src)
			if code == 2 {
				t.Fatalf("exit 2 on a legal program — the code the contract reserves for a compiler bug (ADR 0166):\n%s", stderr)
			}
			if code != 1 {
				t.Fatalf("exit %d, want 1 (a position this backend declines to build):\n%s", code, stderr)
			}
			if strings.TrimSpace(stdout) != "" {
				t.Fatalf("the refusal printed %q on stdout: a refusal is not an answer", stdout)
			}
			if tt.mine {
				for _, want := range []string{"build", "container", "Gap R.67", "print(...)"} {
					if !strings.Contains(stderr, want) {
						t.Fatalf("%q refused without naming %q (ADR 0249):\n%s", tt.name, want, stderr)
					}
				}
			}
		})
	}
}

// The pair door's own carve-outs, witnessed at the CLI. The body is the same on both sides of each pair
// of rows; what tells them apart is the word the caller wrote, because a parameter is a hole through
// which the caller's value becomes a slot of the answer. A double crossing it is the (payload, tag)
// pair the pair road owns, and that road's sentence has to survive this table — a scan that opened the
// door for every call would print `2` for `2.5` at the code of success.
func TestThePairRoadKeepsWhatTheContainerAnswerLeavesIt(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		name string
		src  string
		want string // "" when the row is the pair road's; otherwise the answer both legs print
	}{
		{
			"a parameter's slot filled by a number",
			"def pack(v):\n    return [v, 1]\n\nprint(pack(2))",
			"[2, 1]",
		},
		{
			"a parameter's slot filled by a double",
			"def pack(v):\n    return [v, 1]\n\nprint(pack(2.5))",
			"",
		},
		{
			"a parameter's slot filled by a name the float road bound",
			"x = 2.5\ndef pack(v):\n    return [v, 1]\n\nprint(pack(x))",
			"",
		},
		{
			"a slot the program built at run time handed through a parameter",
			"xs = []\nxs.append(7)\nn = xs[0]\ndef pack(v):\n    return [v, 1]\n\nprint(pack(n))",
			"",
		},
		{
			"a dict entry handed through a parameter",
			"xs = []\nxs.append(7)\nn = xs[0]\ndef build(k):\n    return {\"k\": k}\n\nprint(build(n))",
			"",
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			stdout, stderr, code := cliStreams(t, "--eval", tt.src)
			if code == 2 {
				t.Fatalf("exit 2 on a legal program (ADR 0166):\n%s", stderr)
			}
			if tt.want == "" {
				if code != 1 {
					t.Fatalf("exit %d, want 1: the pair road has to keep this program, printed %q\n%s", code, stdout, stderr)
				}
				if strings.Contains(stderr, "Gap R.67") {
					t.Fatalf("the container answer spoke over the pair road's own refusal:\n%s", stderr)
				}
				if !strings.Contains(stderr, "float") && !strings.Contains(stderr, "pair") {
					t.Fatalf("the refusal does not name the pair or the double it cannot carry:\n%s", stderr)
				}
				return
			}
			if code != 0 {
				t.Fatalf("exit %d, want 0: %s\n%s", code, tt.want, stderr)
			}
			if got := strings.TrimSpace(stdout); got != tt.want {
				t.Fatalf("printed %q, want %q", got, tt.want)
			}
			if py := pythonOutput(t, tt.src+"\n"); strings.TrimSpace(py) != tt.want {
				t.Fatalf("the reference prints %q, not the %q this row recorded", py, tt.want)
			}
		})
	}
}

// The claim the row's Definition of Done makes, asserted on the artifact: no emitted module hands a
// global to a `ret`. That one line was the whole of the exit-2 defect — the container's folded global
// in a value position, which `llc` rejects and the exit-code contract counts as the compiler's bug.
func TestNoAnswerModuleReturnsAGlobalToAValuePosition(t *testing.T) {
	t.Parallel()
	for _, src := range append([]string{
		"def build():\n    return [[1, 2], [3]]\n\nprint(build())",
		"def build():\n    return {1, 2}\n\nprint(build())",
		"def build():\n    return 1.5\n\nprint(build())",
		"def build():\n    return \"ab\"\n\nprint(build())",
	}, containerAnswerAnswered...) {
		t.Run(strings.ReplaceAll(src, "\n", "; "), func(t *testing.T) {
			t.Parallel()
			res, err := lang.Compile(src)
			if err != nil {
				t.Fatalf("compile: %v", err)
			}
			if strings.Contains(res.IR, "ret i32 @.") {
				t.Fatalf("a global sits in a `ret` — the shape that made `llc` reject the module (ADR 0166):\n%s",
					bodyAroundGlobalReturn(res.IR))
			}
		})
	}
}

// bodyAroundGlobalReturn is the few lines of the function that committed the defect, so a failure names
// the road rather than dumping a module.
func bodyAroundGlobalReturn(ir string) string {
	for i, ln := range strings.Split(ir, "\n") {
		if strings.Contains(ln, "ret i32 @.") {
			lines := strings.Split(ir, "\n")
			lo := i - 12
			if lo < 0 {
				lo = 0
			}
			hi := i + 3
			if hi > len(lines) {
				hi = len(lines)
			}
			return strings.Join(lines[lo:hi], "\n")
		}
	}
	return ir
}

// The whole matrix as one program, line for line against CPython.
func TestTheContainerAnswerProgramMatchesTheReferenceThroughTheCLI(t *testing.T) {
	t.Parallel()
	src, err := os.ReadFile("programs/container_returns.gy")
	if err != nil {
		t.Fatalf("read program: %v", err)
	}
	if strings.TrimSpace(string(src)) == "" {
		t.Fatal("the program is empty")
	}
	want, err := os.ReadFile("testdata/container_returns.expected")
	if err != nil {
		t.Fatalf("read the reference's answer: %v", err)
	}
	stdout, stderr, code := cliStreams(t, "--file", "programs/container_returns.gy")
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

func TestTheContainerAnswerProgramIsAnAssertedMatrixRow(t *testing.T) {
	t.Parallel()
	cases := conformanceCases()
	var row *lang.ConformanceCase
	for i := range cases {
		if cases[i].Name == "container_returns.gy" {
			row = &cases[i]
			break
		}
	}
	if row == nil {
		t.Fatal("no conformance matrix row runs container_returns.gy")
	}
	if !row.Asserted {
		t.Fatalf("row %s is not asserted: its stdout would run unwatched", row.ID)
	}
	if row.Oracle != "match" {
		t.Fatalf("row %s declares oracle %q; the compiled leg answers this program, so it is a match", row.ID, row.Oracle)
	}
	for _, want := range []string{
		`print(make_list())`,
		`for k in make_dict():`,
		`ys.append(3)`,
	} {
		if !strings.Contains(row.Source, want) {
			t.Fatalf("row %s runs a program without %q", row.ID, want)
		}
	}
}
