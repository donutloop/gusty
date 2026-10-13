package integration

import (
	"os"
	"strings"
	"testing"

	"github.com/donutloop/gusty/pkg/lang"
)

// A slice whose bounds are not positions, through the CLI (roadmap Gap R.233, ADR 0336).
//
// ADR 0335 put the copy's slots right; this row asks the question of the bounds handed to the copy. A bound
// lowered to whatever word its expression emitted and the number was never asked what it is — so a bound that
// names a text answered by the interned index of that text, and a bound that names a float answered by the
// box's address:
//
//	xs = [1, 2, 3]
//	txt = "a"
//	print(xs[0:txt])   # CPython: TypeError: slice indices must be integers or None or have an __index__ method
//	                   # this toolchain before the row: [1]  at exit 0
//	flt = 1.5
//	print(xs[flt:3])   # CPython: the same TypeError
//	                   # this toolchain before the row: [1, 2]  at exit 0 — the box handle as a position
//
// Those are the worst shapes in the contract: a wrong value on stdout at the code that means "the program
// ran". Every row below is judged against CPython: the expectation is what `python3` printed for the same
// source, never what this file remembers (Gap N's standing rule).

// sliceBoundsAnswered are the shapes the row must leave answering: the gate's negative control. A fix that
// turns any of these into a refusal is the failure mode this row's own predecessor shipped.
var sliceBoundsAnswered = []struct {
	src  string
	want string
}{
	{"xs = [1, 2, 3]\nprint(xs[0:2])", "[1, 2]"},
	{"xs = [1, 2, 3]\nprint(xs[1:])", "[2, 3]"},
	{"xs = [1, 2, 3]\nprint(xs[:2])", "[1, 2]"},
	{"xs = [1, 2, 3]\nprint(xs[::2])", "[1, 3]"},
	{"xs = [1, 2, 3]\nprint(xs[::-1])", "[3, 2, 1]"},
	{"xs = [1, 2, 3]\nprint(xs[-3:])", "[1, 2, 3]"},
	{"xs = [1, 2, 3]\nprint(xs[-1:])", "[3]"},
	{"xs = [1, 2, 3]\nprint(xs[0:3:2])", "[1, 3]"},
	{"print([1.5, \"a\", [7]][0:2])", "[1.5, 'a']"},
	{"xs = [1.5, \"a\", [7]]\nprint(xs[0:2])", "[1.5, 'a']"},
	// An omitted bound and a `None` bound are the same absence, and both mean "the whole way".
	{"xs = [1, 2, 3]\nprint(xs[None:None])", "[1, 2, 3]"},
	{"xs = [1, 2, 3]\nprint(xs[::None])", "[1, 2, 3]"},
	{"xs = [1, 2, 3]\nprint(xs[None:2])", "[1, 2]"},
	{"s = \"abcd\"\nprint(s[None:None])", "abcd"},
	{"s = \"abcd\"\nprint(s[None::None])", "abcd"},
	// A truth value is an integer; the reference reads it as one, and so does the gate.
	{"xs = [1, 2, 3]\nprint(xs[True:True])", "[]"},
	{"xs = [1, 2, 3]\nprint(xs[0:True])", "[1]"},
	{"xs = [1.5, \"a\"]\nprint(xs[True:True])", "[]"},
	// The bounds are expressions: a bound the program named, a bound it computed, a bound a call answered.
	{"xs = [1, 2, 3]\nlo = 0\nhi = 2\nprint(xs[lo:hi])", "[1, 2]"},
	{"xs = [1, 2, 3]\nprint(xs[1 - 1:2 * 1])", "[1, 2]"},
	{"def b():\n    return [1, 2, 3]\nprint(b()[0:2])", "[1, 2]"},
	{"def b():\n    return [1.5, \"a\", [7]]\nys = b()[0:2]\nprint(ys)", "[1.5, 'a']"},
	{"xs = [1, 2, 3]\nprint(len(xs[1:3]))", "2"},
	{"xs = [1, 2, 3]\nprint(xs[1:3] == [2, 3])", "True"},
	{"xs = [1, 2, 3]\nfor v in xs[0:2]:\n    print(v)", "1\n2"},
	// A text's bounds are positions too, and the fold that answers them is left alone.
	{"s = \"abcdef\"\nprint(s[1:3])", "bc"},
	{"s = \"abcdef\"\nprint(s[::-1])", "fedcba"},
}

// TestTheBoundsTheReferenceAnswersStillAnswerThroughTheCLI is the gate's negative control at this altitude:
// each row runs the same source through the reference and through the CLI, and the two have to agree line for
// line — and no row may end at a refusal, which is the failure mode of a gate that asks too much.
func TestTheBoundsTheReferenceAnswersStillAnswerThroughTheCLI(t *testing.T) {
	for _, tt := range sliceBoundsAnswered {
		t.Run(strings.ReplaceAll(tt.src, "\n", "; "), func(t *testing.T) {
			want := pythonOutput(t, tt.src+"\n")
			stdout, stderr, code := cliStreams(t, "--eval", tt.src)
			if code == 2 {
				t.Fatalf("the toolchain failed on its own program:\n%s", stderr)
			}
			if code != 0 {
				t.Fatalf("exit %d, want 0: the reference answers this program, so a refusal is the wrong answer\nstderr %q", code, stderr)
			}
			if got := strings.TrimSpace(stdout); got != strings.TrimSpace(want) {
				t.Fatalf("printed %q, the reference prints %q", got, strings.TrimSpace(want))
			}
		})
	}
}

// TestABoundThatIsNotAPositionRaisesThroughTheCLI is the row's symptom, judged the way the contract judges a
// raise: the exit class that is not a compiler bug, silence on stdout, the reference's sentence on stderr, and
// never the compiler's own `codegen:` prefix — which is how a reader tells a refusal from a raise (ADR 0006,
// ADR 0166, ADR 0214).
func TestABoundThatIsNotAPositionRaisesThroughTheCLI(t *testing.T) {
	const msg = "TypeError: slice indices must be integers or None or have an __index__ method"
	for _, tt := range []struct {
		name string
		src  string
	}{
		{"text low", "txt = \"a\"\nxs = [1, 2, 3]\nprint(xs[0:txt])"},
		{"text high", "txt = \"a\"\nxs = [1, 2, 3]\nprint(xs[txt:3])"},
		{"text step", "txt = \"a\"\nxs = [1, 2, 3]\nprint(xs[0:2:txt])"},
		{"float low", "flt = 1.5\nxs = [1, 2, 3]\nprint(xs[flt:3])"},
		{"float high", "flt = 1.5\nxs = [1, 2, 3]\nprint(xs[0:flt])"},
		{"float step", "flt = 1.5\nxs = [1, 2, 3]\nprint(xs[0:3:flt])"},
		{"list low", "lst = [1]\nxs = [1, 2, 3]\nprint(xs[lst:3])"},
		{"dict low", "dct = {1: 2}\nxs = [1, 2, 3]\nprint(xs[0:dct])"},
		{"tuple literal", "xs = [1, 2, 3]\nprint(xs[0:(1, 2)])"},
		{"text literal", "xs = [1, 2, 3]\nprint(xs[0:\"b\"])"},
		{"float literal", "xs = [1, 2, 3]\nprint(xs[1.5:3])"},
		{"list literal", "xs = [1, 2, 3]\nprint(xs[0:[1]])"},
		{"length", "xs = [1, 2, 3]\ntxt = \"a\"\nprint(len(xs[0:txt]))"},
		{"looped over", "xs = [1, 2, 3]\ntxt = \"a\"\nfor v in xs[0:txt]:\n    print(v)"},
		{"tested for truth", "xs = [1, 2, 3]\ntxt = \"a\"\nif xs[0:txt]:\n    print(\"truthy\")"},
		{"compared", "xs = [1, 2, 3]\ntxt = \"a\"\nprint(xs[0:txt] == [1])"},
		{"tested for membership", "xs = [1, 2, 3]\ntxt = \"a\"\nprint(1 in xs[0:txt])"},
		{"bound to a name", "xs = [1, 2, 3]\ntxt = \"a\"\nys = xs[0:txt]\nprint(ys)"},
		{"sliced again", "xs = [1, 2, 3]\ntxt = \"a\"\nprint(xs[0:txt][0:1])"},
		{"of a call's answer", "def b():\n    return [1, 2, 3]\ntxt = \"a\"\nprint(b()[0:txt])"},
		{"of a text", "s = \"abcd\"\ntxt = \"a\"\nprint(s[0:txt])"},
		{"float bounds on a text", "s = \"abcd\"\nflt = 1.5\nprint(s[flt:3])"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			stdout, stderr, code := cliStreams(t, "--eval", tt.src)
			if code == 2 {
				t.Fatalf("exit 2 on a program the reference raises: the code the contract reserves for a compiler bug (ADR 0166)\n%s", stderr)
			}
			if code == 0 {
				t.Fatalf("answered %q at exit 0 where the reference raises: a position the program cannot name is the program's exception, not a number the compiler invented\nstderr %q", stdout, stderr)
			}
			if strings.Contains(stderr, "codegen:") {
				t.Fatalf("the program was refused by the compiler rather than raised by the runtime:\n%s", stderr)
			}
			if strings.TrimSpace(stdout) != "" {
				t.Fatalf("the raise printed %q on stdout before it raised: the read must be asked before the copy is built", stdout)
			}
			if !strings.Contains(stderr, msg) {
				t.Fatalf("the raise does not carry the reference's sentence %q:\n%s", msg, stderr)
			}
		})
	}
}

// TestABadBoundIsCatchableThroughTheCLI keeps the raise inside the program's world: a bad bound is caught by
// the handler that catches the reference's TypeError, and by the nameless arm too (ADR 0243). There is no row
// for `except TypeError as e:` — the `as` clause is not in the grammar at all, which is roadmap Gap R.113's
// open row, not this raise's business.
func TestABadBoundIsCatchableThroughTheCLI(t *testing.T) {
	for _, tt := range []struct {
		name string
		src  string
	}{
		{"bare", "xs = [1, 2, 3]\ntxt = \"a\"\ntry:\n    print(xs[0:txt])\nexcept TypeError:\n    print(\"caught\")"},
		{"named", "xs = [1, 2, 3]\nflt = 1.5\ntry:\n    print(xs[flt:3])\nexcept:\n    print(\"caught\")"},
		{"in a function", "def at(k):\n    xs = [1, 2, 3]\n    print(xs[0:k])\n\ntry:\n    at(\"a\")\nexcept TypeError:\n    print(\"caught\")"},
		{"after output", "xs = [1, 2, 3]\ntxt = \"a\"\nprint(\"first\")\ntry:\n    print(xs[0:txt])\nexcept TypeError:\n    print(\"caught\")\nprint(\"last\")"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			want := pythonOutput(t, tt.src+"\n")
			stdout, stderr, code := cliStreams(t, "--eval", tt.src)
			if code == 2 {
				t.Fatalf("the toolchain failed on its own program:\n%s", stderr)
			}
			if code != 0 {
				t.Fatalf("exit %d, want 0: the reference runs this handler\nstderr %q", code, stderr)
			}
			if got := strings.TrimSpace(stdout); got != strings.TrimSpace(want) {
				t.Fatalf("printed %q, the reference prints %q", got, strings.TrimSpace(want))
			}
		})
	}
}

// TestTheBoundGateIsInTheModuleIs the row asked of the emitted IR rather than of the runtime: where the
// reference raises, the module raises before it copies; where the reference answers, the module copies and
// never mentions the raise (ADR 0249 — the gate and the door grow together, visibly).
func TestTheBoundGateIsInTheModule(t *testing.T) {
	for _, src := range []string{
		"txt = \"a\"\nxs = [1, 2, 3]\nprint(xs[0:txt])",
		"flt = 1.5\nxs = [1, 2, 3]\nprint(xs[flt:3])",
		"xs = [1, 2, 3]\nprint(xs[0:[1]])",
	} {
		t.Run(strings.ReplaceAll(src, "\n", "; "), func(t *testing.T) {
			res, err := lang.Compile(src)
			if err != nil {
				t.Fatalf("compile: %v", err)
			}
			body := cliMainBody(res.IR)
			// The non-local raise road is the one every trap in this backend takes: the exception state is
			// written, the block branches to the handler in scope (or to the raise-exit), and the message the
			// program will read on stderr is interned in the module beside it.
			if !strings.Contains(body, "store i32 1, i32* @exn_flag") || !strings.Contains(body, "@exn_code") {
				t.Fatalf("the module does not raise where the reference raises:\n%s", body)
			}
			if !strings.Contains(res.IR, "slice indices must be integers") {
				t.Fatalf("the module carries no sentence for the raise the program will read:\n%s", res.IR)
			}
			if strings.Contains(body, "call i32 @rt_slice(") {
				t.Fatalf("the module builds a copy of a slice the reference refuses:\n%s", body)
			}
		})
	}
	for _, src := range []string{
		"xs = [1, 2, 3]\nprint(xs[0:2])",
		"xs = [1, 2, 3]\nprint(xs[None:None])",
		"xs = [1, 2, 3]\nprint(xs[True:True])",
	} {
		t.Run(strings.ReplaceAll(src, "\n", "; "), func(t *testing.T) {
			res, err := lang.Compile(src)
			if err != nil {
				t.Fatalf("compile: %v", err)
			}
			body := cliMainBody(res.IR)
			if !strings.Contains(body, "call i32 @rt_slice(") {
				t.Fatalf("the module no longer copies an answered slice:\n%s", body)
			}
			if strings.Contains(res.IR, "slice indices must be integers") {
				t.Fatalf("the module raises where the reference answers:\n%s", body)
			}
		})
	}
}

// TestWhatTheBoundDoorStillCannotAnswerThroughTheCLI is the filed half at this altitude: six shapes the
// reference answers or raises and this backend does not, each pinned with what it says today and what the
// reference says, so the day a road grows the answer the row fails and the row moves (it cannot be closed by
// deleting a table). None of them may reach exit 2 — the code the contract reserves for a compiler bug
// (ADR 0166) — including the three the slice family still owes a record for.
//
// The first three are the silent half of this row's own gate: it names a kind only where a record names one, and
// a slot, a call's answer and a float-carrying parameter each hold theirs one word too far away (roadmap Gap
// R.237). The last three are the neighbours this row measured and filed rather than fixed: a comprehension whose
// iterable is a copy (Gap R.239), `len` of a slice of a text (Gap R.240), and an `except` arm that calls a
// function (Gap R.241 — the arm runs with the exception flag still set, so the call's own post-call check takes
// it back, the traceback prints, and the statement after the `try` never runs).
func TestWhatTheBoundDoorStillCannotAnswerThroughTheCLI(t *testing.T) {
	for _, tc := range []struct {
		name     string
		src      string
		compiled string // what this backend answers today ("" = it refuses)
		oracle   string // what the reference answers
		gap      string
	}{
		{
			"a text hidden in a slot, used as a bound",
			"xs = [1, 2, 3]\nys = [\"a\"]\nprint(xs[ys[0]:2])",
			"[1, 2]", "TypeError: slice indices must be integers or None or have an __index__ method",
			"roadmap Gap R.237 (the bound is a (payload, tag) pair the compiler holds one word of)",
		},
		{
			"a text handed back by a call, used as a bound",
			"def g():\n    return \"a\"\nxs = [1, 2, 3]\nprint(xs[g():2])",
			"[1, 2]", "TypeError: slice indices must be integers or None or have an __index__ method",
			"roadmap Gap R.237 (the same one-word position, reached through a call — a call handing back `1` answers `[2, 3]` for `xs[g():3]` for the same reason)",
		},
		{
			"a float handed to a parameter that slices by it",
			"def at(k):\n    xs = [1, 2, 3]\n    return xs[0:k]\nprint(at(1.5))",
			"1", "TypeError: slice indices must be integers or None or have an __index__ method",
			"roadmap Gap R.237 (a float parameter is the pair road's, so the body holds no static kind to ask; a text argument DOES raise — the gate reads that kind)",
		},
		{
			"a comprehension over a copy",
			"xs = [1, 2, 3]\nprint([x * 2 for x in xs[0:2]])",
			"", "[2, 4]",
			"roadmap Gap R.239 (the same copy, named, iterates fine: `zs = xs[0:2]` then `[x * 2 for x in zs]` answers — one program, two doors)",
		},
		{
			"the length of a slice of a text",
			"s = \"abcd\"\nprint(len(s[1:3]))",
			"", "2",
			"roadmap Gap R.240 (`len` of the text answers 4 and `len` of a list copy answers 2; the door has no arm for a copy of a text)",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			want := pythonOutput(t, tc.src+"\n")
			stdout, stderr, code := cliStreams(t, "--eval", tc.src)
			if code == 2 {
				t.Fatalf("exit 2 on a filed row — the code the contract reserves for a compiler bug (ADR 0166):\n%s", stderr)
			}
			if tc.compiled == "" {
				if code != 1 {
					t.Fatalf("exit %d, want 1 (the row records a refusal):\nstdout %q\nstderr %q", code, stdout, stderr)
				}
				if strings.TrimSpace(stdout) != "" {
					t.Fatalf("the refusal printed %q on stdout: a refusal is not an answer", stdout)
				}
				if !strings.Contains(stderr, "codegen:") {
					t.Fatalf("the row records a front-end refusal and got something else:\n%s", stderr)
				}
				return
			}
			if code != 0 {
				t.Fatalf("exit %d; the row records the answer %q for %s\nstderr %q", code, tc.compiled, tc.gap, stderr)
			}
			if got := strings.TrimRight(stdout, "\n"); got != tc.compiled {
				t.Fatalf("%q answered %q, the answer recorded for %s was %q (reference: %q)", tc.src, got, tc.gap, tc.compiled, tc.oracle)
			}
			if strings.TrimSpace(want) == strings.TrimSpace(tc.compiled) {
				t.Fatalf("the row is filed as a divergence but the reference agrees with the compiled answer: %q", tc.oracle)
			}
		})
	}
}

// TestACallInsideAnExceptArmStillTakesTheRunDown is Gap R.241 on its own, because it is not the slice road's
// defect and must be seen whole: the handler runs, and the program still reports the exception it handled.
// The same program with `print` in the arm exits 0 — the call's post-check on the module-wide exception flag is
// the difference. Every row here pins the reference's output beside what the toolchain does today.
func TestACallInsideAnExceptArmStillTakesTheRunDown(t *testing.T) {
	for _, tt := range []struct {
		name       string
		src        string
		wantStdout string
		wantCode   int
	}{
		{
			"a user call in the arm",
			"def h():\n    print(\"caught\")\n\nxs = [1, 2, 3]\ntry:\n    print(xs[9])\nexcept IndexError:\n    h()\nprint(\"after\")",
			"caught", // the reference prints "caught\nafter" and exits 0
			3,
		},
		{
			"the same call, arms the raise this row built",
			"def h():\n    print(\"caught\")\n\nxs = [1, 2, 3]\ntxt = \"a\"\ntry:\n    print(xs[0:txt])\nexcept TypeError:\n    h()\nprint(\"after\")",
			"caught", // the reference prints "caught\nafter" and exits 0
			3,
		},
		{
			"the raise this row built, caught by a nameless arm",
			"xs = [1, 2, 3]\nflt = 1.5\ntry:\n    print(xs[flt:3])\nexcept:\n    print(\"caught\")\nprint(\"after\")",
			"caught\nafter",
			0,
		},
		{
			"a builtin in the arm, which is the road that works",
			"xs = [1, 2, 3]\ntxt = \"a\"\ntry:\n    print(xs[0:txt])\nexcept TypeError:\n    print(\"caught\")\nprint(\"after\")",
			"caught\nafter",
			0,
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			stdout, stderr, code := cliStreams(t, "--eval", tt.src)
			if code == 2 {
				t.Fatalf("exit 2 on a legal program (ADR 0166):\n%s", stderr)
			}
			if got := strings.TrimRight(stdout, "\n"); got != tt.wantStdout {
				t.Fatalf("printed %q, the row records %q\nstderr %q", got, tt.wantStdout, stderr)
			}
			if code != tt.wantCode {
				t.Fatalf("exit %d, want %d (roadmap Gap R.241: an arm that calls hands the handled exception back)\nstderr %q", code, tt.wantCode, stderr)
			}
		})
	}
}

// TestTheSliceBoundsProgramMatchesTheReferenceThroughTheCLI is the whole row as one program, line for line
// against the recorded reference answer.
func TestTheSliceBoundsProgramMatchesTheReferenceThroughTheCLI(t *testing.T) {
	src, err := os.ReadFile("programs/slice_bounds.gy")
	if err != nil {
		t.Fatalf("read program: %v", err)
	}
	if strings.TrimSpace(string(src)) == "" {
		t.Fatal("the program is empty")
	}
	want, err := os.ReadFile("testdata/slice_bounds.expected")
	if err != nil {
		t.Fatalf("read the reference's answer: %v", err)
	}
	stdout, stderr, code := cliStreams(t, "--file", "programs/slice_bounds.gy")
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

// TestTheSliceBoundsProgramIsOnTheRecord is ADR 0302's rule at this altitude: a program whose record
// disappeared fails here rather than passing by having nothing to compare against.
func TestTheSliceBoundsProgramIsOnTheRecord(t *testing.T) {
	src, err := os.ReadFile("programs/slice_bounds.gy")
	if err != nil {
		t.Fatalf("read program: %v", err)
	}
	if !lang.HasGoldenAnswer(string(src)) {
		t.Fatal("programs/slice_bounds.gy has no recorded answer: the record leg cannot check a program that is not on it")
	}
}
