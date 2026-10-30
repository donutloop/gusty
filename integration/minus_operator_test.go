package integration

import (
	"os"
	"strings"
	"testing"

	"github.com/donutloop/gusty/pkg/lang"
)

// minus_operator_test.go — `a-b` IS a subtraction, through the CLI (roadmap Gap R.242, ADR 0338).
//
// The lexer's number rule read `-` followed by a digit as one negative integer wherever it appeared, so the
// program below never contained a subtraction at all:
//
//	x = 5
//	y = x-1
//	print(y)     # CPython: 4
//	             # this toolchain before the row: 5  at exit 0
//
// It lexed as `y = x` and an expression statement `-1`, so the answer was the value the subtraction never
// produced — the priority-1 class: a wrong number on stdout at the code that means the program ran, with no
// diagnostic anywhere. Write the same expression with a space (`x - 1`) and it was right.
//
// The other face is the shapes the parser could not swallow:
//
//	print(1-1)              # parse error at 1:8: expected ")"
//	print(len(xs)-1)        # parse error at 1:19: expected ")"
//	print(xs[2-1])          # parse error: expected "]"
//	for i in range(3-1):    # two parse errors
//	def g(n): return n-1    # answered "5 0" where CPython answers "4 -1"
//
// Every expectation below is what the pinned CPython printed for the same source (Gap N's standing rule), and
// the sign rows are the contract's other half: a lexer that splits every `-N` answers `print(-1)` through the
// parser's unary minus and would pass every arithmetic row while breaking these.

// minusOperatorCLIRows are the shapes the reference answers, operator side and sign side together.
var minusOperatorCLIRows = []struct {
	src  string
	want string
}{
	// ---- the operator: what this row newly answers ----
	{"x = 5\ny = x-1\nprint(y)", "4"},
	{"x = 5\nprint(x-1)", "4"},
	{"print(1-1)", "0"},
	{"print(1-1-1)", "-1"},
	{"print(10-3*2)", "4"},
	{"print(len([1, 2, 3])-1)", "2"},
	{"xs = [1, 2, 3]\nprint(xs[2-1])", "2"},
	{"xs = [1, 2, 3]\nprint(xs[len(xs)-1])", "3"},
	{"print(3.5-1)", "2.5"},
	{"print(2-2.0)", "0.0"},
	{"print(10//3-1)", "2"},
	{"print(int(\"5\")-1)", "4"},
	{"print(max(1, 2)-1)", "1"},
	{"print(sum([1, 2, 3])-1)", "5"},
	{"print(True-1)", "0"},
	{"w = {\"a\": 1}\nprint(w[\"a\"]-1)", "0"},
	{"def g(n):\n    return n-1\nprint(g(5), g(0))", "4 -1"},
	{"def g(n):\n    return n-1\nprint(g(5)-1)", "3"},
	{"x = 5\ndef d(a=x-1):\n    return a\nprint(d(), d(2))", "4 2"},
	{"for i in range(3-1):\n    print(i)", "0\n1"},
	{"i = 3\nwhile i-1 > 0:\n    print(i)\n    i = i-1", "3\n2"},
	{"x = 10\nx -= 1\nprint(x)", "9"},
	{"s = \"abcd\"\nprint(s[1-1:3-1])", "ab"},
	{"print((1)-1)", "0"},
	{"print(str(1-1))", "0"},
	{"class C:\n    def __init__(self):\n        self.n = 5\n\n    def m(self):\n        return self.n-1\n\nprint(C().m())", "4"},
	// ---- the sign: the answers the rule must not move ----
	{"print(-1)", "-1"},
	{"print(-1.5)", "-1.5"},
	{"x = -1\nprint(x)", "-1"},
	{"print(1, -1)", "1 -1"},
	{"print(-1 if True else 1)", "-1"},
	{"print(--1)", "1"},
	{"print(-(1))", "-1"},
	{"xs = [1, 2, 3]\nprint(xs[-1], xs[-2])", "3 2"},
	{"print([1, 2, 3][-1])", "3"},
	{"print(abs(-3))", "3"},
	{"print(2 ** -1)", "0.5"},
	{"def f(a=-1):\n    return a\nprint(f(), f(2))", "-1 2"},
	{"xs = [1, 2, 3]\nxs[-1] = 9\nprint(xs)", "[1, 2, 9]"},
	{"print(5*-1)", "-5"},
	{"print(5--1)", "6"},
	{"print(5- -1)", "6"},
	{"print(2*-3+1)", "-5"},
	{"print(int(\"-7\"))", "-7"},
	{"x = 5 # a-1\nprint(x-1)", "4"},
}

// TestSubtractionAnswersWhatTheReferenceAnswersThroughTheCLI runs every row through the reference
// and through the CLI in the same breath, and forbids the two answers this row used to give: exit 2 is
// forbidden because it is the compiler's own bug (ADR 0166), and a non-zero exit is forbidden because a
// refusal is the wrong answer to a program the reference runs (ADR 0249).
func TestSubtractionAnswersWhatTheReferenceAnswersThroughTheCLI(t *testing.T) {
	t.Parallel()
	for _, tt := range minusOperatorCLIRows {
		t.Run(strings.ReplaceAll(tt.src, "\n", "; "), func(t *testing.T) {
			t.Parallel()
			want := pythonOutput(t, tt.src+"\n")
			stdout, stderr, code := cliStreams(t, "--eval", tt.src)
			if code == 2 {
				t.Fatalf("the toolchain failed on its own program:\n%s", stderr)
			}
			if code != 0 {
				t.Fatalf("exit %d, want 0: the reference runs this program, so a refusal is a wrong answer\nstderr %q", code, stderr)
			}
			if got := strings.TrimSpace(stdout); got != strings.TrimSpace(want) {
				t.Fatalf("printed %q, the reference prints %q", got, strings.TrimSpace(want))
			}
		})
	}
}

// TestTheSubtractionIsInTheModuleIs the row asked of the emitted IR. Before ADR 0338 the module for
// `y = x-1` had no subtraction in it at all — the assignment copied `x` and the `-1` was a statement whose
// value nothing printed — so this is the defect's own shape rather than a proxy for it: the arithmetic the
// program wrote appears in the code the program runs. The two rows beside it are the negative controls: a
// program with no subtraction earns no arithmetic, and a negated literal is still a constant, not a subtraction
// from zero the compiler invented.
func TestTheSubtractionIsInTheModule(t *testing.T) {
	t.Parallel()
	for _, src := range []string{
		"x = 5\ny = x-1\nprint(y)",
		"x = 5\nprint(x-1)",
		"def g(n):\n    return n-1\nprint(g(5))",
		"xs = [1, 2, 3]\nprint(xs[len(xs)-1])",
	} {
		t.Run(strings.ReplaceAll(src, "\n", "; "), func(t *testing.T) {
			t.Parallel()
			res, err := lang.Compile(src)
			if err != nil {
				t.Fatalf("compile: %v", err)
			}
			body := programFunctionBodies(t, res.IR)
			if !strings.Contains(body, "sub i32") {
				t.Fatalf("the module has no subtraction in it; the program wrote one:\n%s", body)
			}
		})
	}
	for _, tc := range []struct {
		src  string
		want string
	}{
		{"x = 5\ny = x\nprint(y)", ""},           // no subtraction written, none emitted
		{"x = -1\nprint(x)", "-1\n"},             // a sign is a constant, not `0 - 1`
		{"xs = [1, 2, 3]\nprint(xs[-1])", "3\n"}, // the negative index is a constant position
		{"print(5*-1)", "-5\n"},                  // the sign is folded into the literal, not computed
	} {
		t.Run(strings.ReplaceAll(tc.src, "\n", "; "), func(t *testing.T) {
			t.Parallel()
			res, err := lang.Compile(tc.src)
			if err != nil {
				t.Fatalf("compile: %v", err)
			}
			if strings.Contains(programFunctionBodies(t, res.IR), "sub i32") {
				t.Fatalf("%q emits arithmetic the program never wrote — the sign stopped merging into the literal:\n%s", tc.src, programFunctionBodies(t, res.IR))
			}
		})
	}
}

// programFunctionBodies is the code the program's own source produced: `@main` plus every function the program
// defined (`@gy_*`), and none of the runtime's helpers. The helpers do arithmetic of their own — `rt_str_index`
// counts backwards with a `sub` — so an assertion that cannot tell the two apart asserts nothing (ADR 0338).
func programFunctionBodies(t *testing.T, ir string) string {
	t.Helper()
	rest := "\n" + ir
	var out []string
	for {
		i := strings.Index(rest, "\ndefine ")
		if i < 0 {
			break
		}
		head := rest[i:]
		end := strings.Index(head, "\n}\n")
		if end < 0 {
			break
		}
		block := head[:end+3]
		if strings.Contains(block, "@main(") || strings.Contains(block, "@gy_") {
			out = append(out, block)
		}
		rest = head[end:]
	}
	if len(out) == 0 {
		t.Fatalf("the module has no @main and no @gy_ function in it:\n%s", ir)
	}
	return strings.Join(out, "\n")
}

// TestWhatTheMinusRowStillCannotAnswerThroughTheCLI is the filed half at this altitude. Both shapes behave
// identically before and after ADR 0338 — they are not this row's regressions — and each is pinned with what
// the backend says today and what the reference says, so the day a road grows the answer the row fails and the
// row moves. A `+` before an operand is Python's grammar and the parser has no rule for it at all (roadmap Gap
// R.244, filed by this row).
//
// The comprehension row used to be here, refusing `xs is a list the compiler kept as a compile-time constant`
// for `[x*2 for x in xs]`. It is answered now: the escape analysis had been deleting the list because the only
// read of it sat in a construct its walkers did not visit, and once the read counted (ADR 0356, roadmap
// **Gap R.289**) the iterable is a real object the loop walks. The row stays, as an answer, because the
// direction it was found in matters: a filed refusal that quietly becomes an answer must fail something first.
func TestWhatTheMinusRowStillCannotAnswerThroughTheCLI(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name     string
		src      string
		compiled string // what this backend says today ("" = it refuses)
		oracle   string // what the reference answers
		gap      string
	}{
		// (`print(+1)` and `x-+1` sat here as the grammar's missing prefix rule. They are answered now —
		// roadmap Gap R.244, ADR 0469 — and moved, with the reference's answers beside them, to
		// `TestTheUnaryPlusAnswersWhatTheReferenceAnswersThroughTheCLI` in `unary_plus_test.go`. A filed
		// row that becomes an answer has to fail something first, and it did.)
		{
			"a comprehension over a constant list", "xs = [1, 2, 3]\nys = [x*2 for x in xs]\nprint(ys)",
			"", "[2, 4, 6]",
			"roadmap Gap R.289 (paid, ADR 0356 — the read the escape walk could not see had deleted the iterable; L11.1 remains the row that makes every container a runtime object)",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			stdout, stderr, code := cliStreams(t, "--eval", tc.src)
			if code == 2 {
				t.Fatalf("exit 2 on a program the reference runs: the code the contract reserves for a compiler bug (ADR 0166)\n%s", stderr)
			}
			if code == 0 && tc.compiled != "" {
				t.Fatalf("%q answered %q at exit 0 where this row filed a refusal — the filed row moved; move the row with it (it is not this program's business to fail for being right)\n%s", tc.src, stdout, tc.gap)
			}
			if code != 0 && tc.compiled == "" {
				t.Fatalf("%q refused where the reference answers and this row expects an answer\nstderr %q", tc.src, stderr)
			}
			if code != 0 {
				joined := stderr + stdout
				if !strings.Contains(joined, tc.compiled) {
					t.Fatalf("%q refused with %q, which does not name the shape that is missing (ADR 0249); filed as %s", tc.src, strings.TrimSpace(joined), tc.gap)
				}
			}
			want := pythonOutput(t, tc.src+"\n")
			if strings.TrimSpace(want) != tc.oracle {
				t.Fatalf("the oracle column of the filed row is a citation: CPython answers %q for this program, the row says %q", strings.TrimSpace(want), tc.oracle)
			}
		})
	}
}

// TestTheMinusProgramMatchesTheReferenceThroughTheCLI is the whole row as one program, line for line against
// the recorded reference answer.
func TestTheMinusProgramMatchesTheReferenceThroughTheCLI(t *testing.T) {
	t.Parallel()
	src, err := os.ReadFile("programs/minus_operator.gy")
	if err != nil {
		t.Fatalf("read program: %v", err)
	}
	if strings.TrimSpace(string(src)) == "" {
		t.Fatal("the program is empty")
	}
	want, err := os.ReadFile("testdata/minus_operator.expected")
	if err != nil {
		t.Fatalf("read the reference's answer: %v", err)
	}
	stdout, stderr, code := cliStreams(t, "--file", "programs/minus_operator.gy")
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

// TestTheMinusProgramIsOnTheRecord is ADR 0302's rule at this altitude: a program whose record disappeared
// fails here rather than passing by having nothing to compare against.
func TestTheMinusProgramIsOnTheRecord(t *testing.T) {
	t.Parallel()
	src, err := os.ReadFile("programs/minus_operator.gy")
	if err != nil {
		t.Fatalf("read program: %v", err)
	}
	if !lang.HasGoldenAnswer(string(src)) {
		t.Fatal("integration/programs/minus_operator.gy is not on the record")
	}
}
