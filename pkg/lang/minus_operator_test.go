package lang

import (
	"strings"
	"testing"
)

// minus_operator_test.go — the minus BETWEEN two expressions is the subtraction operator,
// and the minus before a literal in an expression position is its SIGN (roadmap Gap R.242,
// ADR 0338). One lexer case decided both from the character AFTER the sign:
//
//	x = 5
//	y = x-1
//	print(y)      # the reference writes 4;   this toolchain wrote 5, at exit 0
//	print(1-1)    # the reference writes 0;   this toolchain: parse error at 1:8: expected ")"
//
// The first is the priority-1 class — a wrong value at the code that means the program ran —
// and it was not a wrong evaluation: the program never contained a subtraction. `-1` was one
// token, so the line was `y = x` and an expression statement `-1` whose value nothing prints.
// The second face is the same rule meeting an expression it could not swallow: `print(x-1)`,
// `print(len(xs)-1)`, `print(xs[2-1])`, `print(3.5-1)`, `for i in range(3-1):` and
// `def g(n): return n-1` all died in the parser, while the spaced spelling (`x - 1`) answered
// correctly, which is the only reason a suite written in this house's style never saw it.
//
// Every `want` below is what `python3` prints for the program, and every row is on both legs:
// the record (`evalGolden`, `runGoldenStdout`) and the reference (the CLI, in the integration
// file). The rows a gate breaks are the sign rows: a lexer that splits every `-N` answers
// `print(-1)` with the parser's unary minus and would pass the arithmetic while failing here.

// minusOperatorAnswered is the family the reference answers and this toolchain must answer
// identically — operator side and sign side together, so one table shows both halves of the
// rule and neither can regress without a row failing.
var minusOperatorAnswered = []struct {
	src  string
	want string
}{
	// ---- the operator side: this row's answer ----
	{"x = 5\ny = x-1\nprint(y)", "4"},
	{"x = 5\nprint(x-1)", "4"},
	{"print(1-1)", "0"},
	{"print(3-1)", "2"},
	{"print(1-1-1)", "-1"},
	{"print(10-3*2)", "4"},
	{"print(2-2.0)", "0.0"},
	{"print(3.5-1)", "2.5"},
	{"print(10//3-1)", "2"},
	{"print(len([1, 2, 3])-1)", "2"},
	{"xs = [1, 2, 3]\nprint(xs[2-1])", "2"},
	{"xs = [1, 2, 3]\nprint(xs[len(xs)-1])", "3"},
	{"print(int(\"5\")-1)", "4"},
	{"print(max(1, 2)-1)", "1"},
	{"print(min(3, 1, 2)-1)", "0"},
	{"print(sum([1, 2, 3])-1)", "5"},
	{"print(round(2.7)-1)", "2"},
	{"print(True-1)", "0"},
	{"print(False-1)", "-1"},
	{"print(True-True)", "0"},
	{"w = {\"a\": 1}\nprint(w[\"a\"]-1)", "0"},
	{"def g(n):\n    return n-1\n\nprint(g(5), g(0))", "4 -1"},
	{"def g(n):\n    return n-1\n\nprint(g(5)-1)", "3"},
	{"x = 5\ndef d(a=x-1):\n    return a\n\nprint(d(), d(2))", "4 2"},
	{"for i in range(3-1):\n    print(i)", "0\n1"},
	{"for i in range(10-8):\n    print(i)", "0\n1"},
	{"i = 3\nwhile i-1 > 0:\n    print(i)\n    i = i-1", "3\n2"},
	{"x = 10\nx -= 1\nprint(x)", "9"},
	{"s = \"abcd\"\nprint(s[1-1:3-1])", "ab"},
	{"print((1)-1)", "0"},
	{"print(str(1-1))", "0"},
	{"class C:\n    def __init__(self):\n        self.n = 5\n\n    def m(self):\n        return self.n-1\n\nprint(C().m())", "4"},

	// ---- the sign side: the answer the rule must not move ----
	{"print(-1)", "-1"},
	{"print(-1.5)", "-1.5"},
	{"print(-0)", "0"},
	{"x = -1\nprint(x)", "-1"},
	{"print(1, -1)", "1 -1"},
	{"print(-1 if True else 1)", "-1"},
	{"print(--1)", "1"},
	{"print(-(1))", "-1"},
	{"xs = [1, 2, 3]\nprint(xs[-1], xs[-2])", "3 2"},
	{"xs = [1, 2, 3]\nprint(xs[0-1])", "3"},
	{"print([1, 2, 3][-1])", "3"},
	{"print([1, 2, 3][1-1])", "1"},
	{"print(abs(-3))", "3"},
	{"print(2 ** -1)", "0.5"},
	{"def f(a=-1):\n    return a\n\nprint(f(), f(2))", "-1 2"},
	{"xs = [1, 2, 3]\nxs[-1] = 9\nprint(xs)", "[1, 2, 9]"},
	{"d = {}\nd[-1] = \"k\"\nprint(d[-1])", "k"},
	{"t = -1\nprint({t: \"v\"}[t])", "v"},
	// both faces in one line: `-1` is a sign, `0-1` is a subtraction that lands on the same value
	{"xs = [1, 2, 3]\nprint(xs[-1], xs[0-1])", "3 3"},
	// a minus inside a text is a character, not an operator, and a `-` inside a comment is neither
	{"print(int(\"-7\"))", "-7"},
	{"print(\"-42\".zfill(5))", "-0042"},
	{"x = 5 # a-1\nprint(x-1)", "4"},
	// the doubled spellings the reference resolves with a unary minus under a binary one
	{"print(5*-1)", "-5"},
	{"print(5--1)", "6"},
	{"print(5- -1)", "6"},
	{"print(2*-3+1)", "-5"},
	{"print(5-1, 5--1)", "4 6"},
}

// TestSubtractionAnswersWhatTheReferenceAnswers runs every row through the compiler and
// through the JIT, and refuses to accept a refusal: the whole point of the row is that these
// programs were parse errors and wrong answers, not capability boundaries (ADR 0249).
func TestSubtractionAnswersWhatTheReferenceAnswers(t *testing.T) {
	t.Parallel()
	for _, tt := range minusOperatorAnswered {
		t.Run(strings.ReplaceAll(tt.src, "\n", "; "), func(t *testing.T) {
			t.Parallel()
			got, refused := compiledOutOrRefusal(t, tt.src)
			if refused {
				t.Fatalf("%q refused (%s); the reference answers %q", tt.src, got, tt.want+"\n")
			}
			if got != tt.want+"\n" {
				t.Fatalf("%q printed %q, want %q", tt.src, got, tt.want+"\n")
			}
			out, trap := runForParity(t, tt.src)
			if trap != "" {
				t.Fatalf("%q raised %s; the reference prints %q", tt.src, trap, tt.want)
			}
			if out != tt.want {
				t.Fatalf("%q printed %q through the JIT, want %q", tt.src, out, tt.want)
			}
		})
	}
}

// TestTheSubtractionIsOnTheRecord asks the same programs of the record leg — the answer the
// retired engine gave, kept as an expectation. The row is newly answered surface: a source
// whose answer the record does not hold fails, so coverage cannot be quietly dropped.
func TestTheSubtractionIsOnTheRecord(t *testing.T) {
	t.Parallel()
	for _, tt := range minusOperatorAnswered {
		t.Run(strings.ReplaceAll(tt.src, "\n", "; "), func(t *testing.T) {
			t.Parallel()
			if !HasGoldenAnswer(tt.src) {
				t.Fatalf("%q is not on the record: a newly answered program is recorded, not self-certified", tt.src)
			}
			rec := goldenLookup(t, tt.src)
			if !rec.HasStd {
				t.Fatalf("%q: the record holds no stdout for a program that prints", tt.src)
			}
			if rec.Stdout != tt.want+"\n" {
				t.Errorf("%q: the record printed %q, the reference prints %q", tt.src, rec.Stdout, tt.want+"\n")
			}
		})
	}
}

// TestWhatTheMinusRowLeavesStandingIsFiledNotFixed keeps the neighbours honest: the row fixes
// the lexer's `-N` rule and nothing else. The shape below behaves identically before and after
// ADR 0338 — it is the reference's answer this toolchain does not give, filed as its own row
// rather than smuggled in beside a one-line rule.
func TestWhatTheMinusRowLeavesStandingIsFiledNotFixed(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		src    string
		want   string // the shape the refusal must name
		filing string
	}{
		// (`print(+1)` sat here as the parser's missing prefix rule and is answered now — roadmap
		// Gap R.244, ADR 0469 — with its rows in `unary_plus_test.go`, both legs and the reference's
		// sentences included. It failed first, which is how a filed row closes instead of vanishing.)
		// (A second row sat here demanding that a comprehension over a list the compiler kept static
		// refuse with `compile-time constant`. It was right when written and wrong afterwards: the
		// sentence was emitted because the escape analysis could not see the comprehension's iterable
		// as a READ (it walked a `switch` of node types and `Comp.Iter` was not one), so the binding's
		// allocation was deleted and the comprehension had no object to walk. `xs = [1,2,3]` /
		// `ys = [x*2 for x in xs]` / `print(ys)` now prints `[2, 4, 6]` on both legs — ADR 0356, and
		// it is asserted in `TestAContainerReadOnlyInsideAConstructTheWalkMustCoverKeepsItsBinding`.)
	} {
		t.Run(strings.ReplaceAll(tc.src, "\n", "; "), func(t *testing.T) {
			t.Parallel()
			out, refused := compiledOutOrRefusal(t, tc.src)
			if !refused {
				t.Fatalf("%q answered %q; the row expects the filed refusal to stand", tc.src, out)
			}
			if !strings.Contains(out, tc.want) {
				t.Fatalf("%q refused with %q, which does not name what is missing (ADR 0249), filed %s", tc.src, out, tc.filing)
			}
			if strings.HasPrefix(out, "gustyc: jit: codegen:") && strings.Contains(out, "unsupported") {
				t.Errorf("%q: a refusal that says only %q is not a name for the missing shape, filed %s", tc.src, out, tc.filing)
			}
		})
	}
}
