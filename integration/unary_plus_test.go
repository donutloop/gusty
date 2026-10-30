package integration

import (
	"os"
	"strings"
	"testing"

	"github.com/donutloop/gusty/pkg/lang"
)

// unary_plus_test.go — the prefix `+` through the CLI, against the reference (roadmap **Gap R.244**,
// **ADR 0469**).
//
// Before this row the grammar had no prefix `+` rule at all, so the whole family died in the parser:
//
//	print(+1)      # 1     in the reference;   here: parse error at 1:7: unexpected token, exit 1
//	print(+1.5)    # 1.5                       parse error
//	print(+True)   # 1                         parse error
//	x = 5
//	print(x-+1)    # 4                         parse error at 2:9
//
// while `print(-1)` had always answered. The lowering first tried was the obvious one — `+x` as `x + 0` —
// and the CLI is where it fell over: `print(+1.5)` printed `1`, `x = 5.5` / `print(+x)` printed `0`,
// `print(+None)` printed `0` at exit 0 where the reference raises, and `+[1, 2]` raised a concatenation
// sentence about an `int` the source never mentions. Every expectation below is what the pinned CPython
// printed for the same source; the raise rows assert the whole last line of its traceback, not a prefix
// of it, because the sentence is the thing an agent acts on (ADR 0249).

// unaryPlusCLIRows are the shapes the reference answers, measured against it inside the case rather than
// paraphrased here: each row runs CPython too and the two must agree.
var unaryPlusCLIRows = []struct {
	src  string
	want string
}{
	{"print(+1)", "1"},
	{"print(+0)", "0"},
	{"print(+1.5)", "1.5"},
	{"print(+1.0)", "1.0"},
	{"print(+True)", "1"},
	{"print(+False)", "0"},
	{"print(+1, +2)", "1 2"},
	{"print(-+1)", "-1"},
	{"print(-+-1)", "1"},
	{"print(+ +1)", "1"},
	{"print(+-1)", "-1"},
	{"print(+ + -1)", "-1"},
	{"print(+ 1)", "1"},
	{"print(1 ++ 2)", "3"},
	{"print(1-+1)", "0"},
	{"print(5-+1)", "4"},
	{"print(2*+3)", "6"},
	{"print(+2 ** 2)", "4"},
	{"print(+3 // 2)", "1"},
	{"print(abs(+3))", "3"},
	{"print(max(+1, 2))", "2"},
	{"x = 1\nx += +1\nprint(x)", "2"},
	{"x = 5\nprint(+x)", "5"},
	{"x = 5\nprint(x-+1)", "4"},
	{"x = 5.5\nprint(+x)", "5.5"},
	{"x = True\nprint(+x)", "1"},
	{"print(+1 if True else 2)", "1"},
	{"print([+1])", "[1]"},
	{"print((+1, +2))", "(1, 2)"},
	{"print({\"a\": +1})", "{'a': 1}"},
	{"for v in [+1, +2]:\n    print(v)", "1\n2"},
	{"xs = [7]\nprint(+xs[0])", "7"},
	{"d = {\"a\": 1}\nprint(+d[\"a\"])", "1"},
	{"def f():\n    return 1\nprint(+f())", "1"},
	{"def h():\n    return 1.5\nprint(+h())", "1.5"},
	{"print(+len(\"abc\"))", "3"},
	{"print(+int(\"2\"))", "2"},
	{"print(+sum([1, 2]))", "3"},
	{"print(str(+1))", "1"},
	{"print(f\"{+1}\")", "1"},
	{"xs = [1, 2]\nys = [+x for x in xs]\nprint(ys)", "[1, 2]"},
}

// unaryPlusCLIRaiseRows are the programs the reference stops on. `sentence` is the last line of its
// traceback; the compiled leg must write the same line and leave through the runtime-error exit class.
var unaryPlusCLIRaiseRows = []struct {
	src      string
	sentence string
}{
	{"print(+\"abc\")", "TypeError: bad operand type for unary +: 'str'"},
	{"print(+\"\")", "TypeError: bad operand type for unary +: 'str'"},
	{"x = \"a\"\nprint(+x)", "TypeError: bad operand type for unary +: 'str'"},
	{"print(+None)", "TypeError: bad operand type for unary +: 'NoneType'"},
	{"print(+[1, 2])", "TypeError: bad operand type for unary +: 'list'"},
	{"xs = [1, 2]\nprint(+xs)", "TypeError: bad operand type for unary +: 'list'"},
	{"print(+(1, 2))", "TypeError: bad operand type for unary +: 'tuple'"},
	{"print(+{1, 2})", "TypeError: bad operand type for unary +: 'set'"},
	{"print(+{1: 2})", "TypeError: bad operand type for unary +: 'dict'"},
	{"xs = [\"a\"]\nprint(+xs[0])", "TypeError: bad operand type for unary +: 'str'"},
	{"def g():\n    return \"a\"\nprint(+g())", "TypeError: bad operand type for unary +: 'str'"},
	{"class Token:\n    pass\nprint(+Token())", "TypeError: bad operand type for unary +: 'Token'"},
	{"def f(x):\n    return x\nprint(+f)", "TypeError: bad operand type for unary +: 'function'"},
	{"print(+(lambda x: x))", "TypeError: bad operand type for unary +: 'function'"},
}

// unaryPlusCLIHandlerRows are the same raises caught: the handler runs and the program leaves normally.
var unaryPlusCLIHandlerRows = []struct {
	src  string
	want string
}{
	{"try:\n    print(+None)\nexcept TypeError as e:\n    print(e)", "bad operand type for unary +: 'NoneType'"},
	{"try:\n    print(+\"a\")\nexcept TypeError:\n    print(\"caught\")", "caught"},
	{"try:\n    print(+{1, 2})\nexcept TypeError as err:\n    print(err)", "bad operand type for unary +: 'set'"},
}

// TestTheUnaryPlusAnswersWhatTheReferenceAnswersThroughTheCLI runs every answered row through the CLI and
// through CPython in the same case, so the reference column cannot drift from the program it cites.
func TestTheUnaryPlusAnswersWhatTheReferenceAnswersThroughTheCLI(t *testing.T) {
	t.Parallel()
	for _, tt := range unaryPlusCLIRows {
		t.Run(strings.ReplaceAll(tt.src, "\n", "; "), func(t *testing.T) {
			t.Parallel()
			stdout, stderr, code := cliStreams(t, "--eval", tt.src)
			if code == 2 {
				t.Fatalf("exit 2 on a program the reference runs: %s\n%s", tt.want, stderr)
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

// TestTheUnaryPlusRaisesThroughTheCLIWhatTheReferenceRaises pins the class and the whole sentence on the
// uncaught path, and the exit code the contract reserves for a trap.
func TestTheUnaryPlusRaisesThroughTheCLIWhatTheReferenceRaises(t *testing.T) {
	t.Parallel()
	for _, tt := range unaryPlusCLIRaiseRows {
		t.Run(strings.ReplaceAll(tt.src, "\n", "; "), func(t *testing.T) {
			t.Parallel()
			stdout, stderr, code := cliStreams(t, "--eval", tt.src)
			if code == 2 {
				t.Fatalf("exit 2 on a program the reference stops on: the contract's compiler-bug code\n%s", stderr)
			}
			if code == 0 {
				t.Fatalf("%q answered %q at exit 0 where the reference raises %s", tt.src, stdout, tt.sentence)
			}
			if code != 3 {
				t.Fatalf("%q left %d; a catchable trap is exit 3 (ADR 0166's contract)\n%s", tt.src, code, stderr)
			}
			if got := lastTracebackLine(stderr); got != tt.sentence {
				t.Fatalf("%q wrote %q; the reference writes %q", tt.src, got, tt.sentence)
			}
			// The reference leg, asked rather than cited: CPython's own traceback line must be the row's
			// sentence, so the row cannot drift from the program it cites.
			_, refErr, _ := lang.PythonRun(tt.src + "\n")
			if got := lastTracebackLine(refErr); got != tt.sentence {
				t.Fatalf("the row's sentence is not what CPython writes: %q vs %q (traceback %q)", got, tt.sentence, refErr)
			}
		})
	}
}

// TestTheUnaryPlusTrapIsCaughtByItsHandlerThroughTheCLI witnesses the catchable half: a program with an
// `except TypeError:` must run the arm, not the raise, and leave at 0.
func TestTheUnaryPlusTrapIsCaughtByItsHandlerThroughTheCLI(t *testing.T) {
	t.Parallel()
	for _, tt := range unaryPlusCLIHandlerRows {
		t.Run(strings.ReplaceAll(tt.src, "\n", "; "), func(t *testing.T) {
			t.Parallel()
			stdout, stderr, code := cliStreams(t, "--eval", tt.src)
			if code != 0 {
				t.Fatalf("%q left %d (the handler was meant to run): %s\nstdout %q", tt.src, code, strings.TrimSpace(stderr), stdout)
			}
			if got := strings.TrimRight(stdout, "\n"); got != tt.want {
				t.Fatalf("%q printed %q, want %q", tt.src, got, tt.want)
			}
		})
	}
}

// TestTheUnaryPlusProgramAnswersNothingTheReferenceDoesNot is the whole family as one program, line for
// line against what CPython printed for it — the artifact rule the `minus_operator` row established.
func TestTheUnaryPlusProgramAnswersNothingTheReferenceDoesNot(t *testing.T) {
	t.Parallel()
	src, err := os.ReadFile("programs/unary_plus.gy")
	if err != nil {
		t.Fatalf("read program: %v", err)
	}
	if strings.TrimSpace(string(src)) == "" {
		t.Fatal("the program is empty")
	}
	want, err := os.ReadFile("testdata/unary_plus.expected")
	if err != nil {
		t.Fatalf("read the reference's answer: %v", err)
	}
	stdout, stderr, code := cliStreams(t, "--file", "programs/unary_plus.gy")
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

// TestTheUnaryPlusProgramIsOnTheRecord is ADR 0302's rule at this altitude.
func TestTheUnaryPlusProgramIsOnTheRecord(t *testing.T) {
	t.Parallel()
	src, err := os.ReadFile("programs/unary_plus.gy")
	if err != nil {
		t.Fatalf("read program: %v", err)
	}
	if !lang.HasGoldenAnswer(string(src)) {
		t.Fatal("integration/programs/unary_plus.gy is not on the record — a corpus program is recorded like any other source")
	}
}

// TestNoUnaryPlusIsLoweredByTheSubtraction is the IR witness at the CLI altitude: the program's own
// functions must not subtract from zero, because that is the negation's instruction and `+x` is not a
// negation. The counter-row is the same program with a `-`: the instruction must be there for it, so the
// absence above is a fact about `+` rather than about a deleted road (ADR 0337's negative-control rule).
func TestNoUnaryPlusIsLoweredByTheSubtraction(t *testing.T) {
	t.Parallel()
	for _, src := range []string{
		"print(+1)",
		"x = 5\nprint(+x)",
		"x = 5.5\nprint(+x)",
		"xs = [7]\nprint(+xs[0])",
	} {
		t.Run(strings.ReplaceAll(src, "\n", "; "), func(t *testing.T) {
			t.Parallel()
			res, err := lang.Compile(src)
			if err != nil {
				t.Fatalf("compile: %v", err)
			}
			if body := programFunctionBodies(t, res.IR); strings.Contains(body, "sub i32 0,") || strings.Contains(body, "fsub double 0.0,") {
				t.Fatalf("%q lowers the prefix with the negation's instruction:\n%s", src, body)
			}
		})
	}
	for _, src := range []string{"x = 5\nprint(-x)", "x = 1.5\nprint(-x)"} {
		t.Run("counter-row "+strings.ReplaceAll(src, "\n", "; "), func(t *testing.T) {
			t.Parallel()
			res, err := lang.Compile(src)
			if err != nil {
				t.Fatalf("compile: %v", err)
			}
			body := programFunctionBodies(t, res.IR)
			if !strings.Contains(body, "sub i32 0,") && !strings.Contains(body, "fsub double 0.0,") {
				t.Fatalf("%q stopped writing the negation it has always written:\n%s", src, body)
			}
		})
	}
}

// TestWhatTheUnaryPlusRowStillCannotAnswerThroughTheCLI is the filed half: what the reference answers and
// this door declines, with both answers on the row so the day a road grows one of them the row fails and
// moves. Every refusal must name what it could not do (ADR 0249) — a refusal that only says "unsupported"
// is not a name for a missing shape — and none of these may reach exit 2.
func TestWhatTheUnaryPlusRowStillCannotAnswerThroughTheCLI(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name     string
		src      string
		compiled string // what this backend says today ("" = it refuses)
		oracle   string // what the reference answers
		gap      string
	}{
		{
			"a parameter the body cannot name", "def f(a):\n    return +a\nprint(f(3))",
			"cannot answer", "3",
			"roadmap Gap R.564 (the proof is a record, and a parameter's kind is the call sites' business)",
		},
		{
			"two levels of slot", "xs = [[1], [2]]\nprint(+xs[0][0])",
			"cannot answer", "1",
			"roadmap Gap R.564 (the literal describes one level; the second is the object's)",
		},
		{
			"a name the body does not own", "x = 5\ndef g():\n    return +x\nprint(g())",
			"cannot answer", "5",
			"roadmap Gap R.564 (`print(+x)` answers at top level; the function's notebooks never saw the binding)",
		},
		{
			"the winner of a min", "print(+min([3, 1]))",
			"cannot answer", "1",
			"roadmap Gap R.564 (min answers whichever operand wins, so its kind is the container's)",
		},
		{
			"a callee that hands back a container", "def h():\n    return [1]\nprint(+h())",
			"cannot answer", "TypeError: bad operand type for unary +: 'list'",
			"roadmap Gap R.564 with Gap R.229 (a container answer is not yet a value at every door)",
		},
		{
			"a callee that returns nothing", "def h():\n    pass\nprint(+h())",
			"cannot answer", "TypeError: bad operand type for unary +: 'NoneType'",
			"roadmap Gap R.564 (the door names a None literal, not a body that fell off its end)",
		},
		{
			"`len` never lowers its argument", "print(len(+\"ab\"))",
			"len requires", "TypeError: bad operand type for unary +: 'str'",
			"roadmap Gap R.240 (the length door declines before the operand is asked anything)",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			stdout, stderr, code := cliStreams(t, "--eval", tc.src)
			if code == 2 {
				t.Fatalf("exit 2 on a program the reference runs: the contract's compiler-bug code (ADR 0166)\n%s", stderr)
			}
			joined := stderr + stdout
			if code == 0 && tc.compiled != "" {
				t.Fatalf("%q answered %q where this row filed a refusal — the filed row moved; move it with the fix (that is how a filed row closes)\n%s", tc.src, stdout, tc.gap)
			}
			if code != 0 && !strings.Contains(joined, tc.compiled) {
				t.Fatalf("%q refused with %q, which does not name what is missing (ADR 0249); filed as %s", tc.src, strings.TrimSpace(joined), tc.gap)
			}
			if strings.Contains(joined, "unsupported") {
				t.Fatalf("%q refused by saying only %q — not a name for the missing shape (ADR 0249), filed %s", tc.src, strings.TrimSpace(joined), tc.gap)
			}
			if tc.oracle == "" {
				return
			}
			if strings.HasPrefix(tc.oracle, "TypeError:") {
				_, refErr, _ := lang.PythonRun(tc.src + "\n")
				if got := lastTracebackLine(refErr); got != tc.oracle {
					t.Fatalf("the oracle column is a citation: CPython writes %q, the row says %q (traceback %q)", got, tc.oracle, refErr)
				}
				return
			}
			if want := strings.TrimRight(pythonOutput(t, tc.src+"\n"), "\n"); want != tc.oracle {
				t.Fatalf("the oracle column is a citation: CPython answers %q, the row says %q", want, tc.oracle)
			}
		})
	}
}
