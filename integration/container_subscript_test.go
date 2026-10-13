package integration

import (
	"os"
	"strings"
	"testing"

	"github.com/donutloop/gusty/pkg/lang"
)

// A subscript read off the answer a call hands back, through the CLI (roadmap Gap R.214, ADR 0334).
//
// The symptom at this altitude is a refusal that should have been an answer. `gustyc --eval
// 'def build(): return [1, 2]
// print(build()[7])'` stopped the build at exit 1 with `codegen: index requires an inline list/dict/set
// literal`, while the same source bound to a name (`ys = build()`) raised `IndexError: list index out of
// range` at exit 3 and the literal spelling (`print([1, 2][7])`) had raised the same trap since ADR 0326.
// One question, three doors, and the subscript chose which road to take from the *spelling of its base*
// rather than from what the compiler holds about it. Every row below is judged against CPython: the
// expectation is what `python3` printed for the same source, never what this file remembers (Gap N's
// standing rule).

// TestACallResultSubscriptAnswersThroughTheCLI is the answering half: each row runs the same source through
// the reference and through the CLI, and the two have to agree line for line.
func TestACallResultSubscriptAnswersThroughTheCLI(t *testing.T) {
	for _, src := range []string{
		"def build():\n    return [1, 2, 3]\n\nprint(build()[0], build()[2])",
		"def cfg():\n    return {\"a\": 1, \"b\": 2}\n\nprint(cfg()[\"b\"])",
		"def words():\n    return [\"hi\", \"ho\"]\n\nprint(words()[1])",
		"def cfg():\n    return {\"a\": 1}\n\nprint(cfg()[\"a\"] + 1)",
		"def rows():\n    return [{\"name\": \"zed\"}]\n\nprint(rows()[0][\"name\"])",
		"def grid():\n    return [[1, 2], [3, 4]]\n\nprint(grid()[1][0])",
		"def vals():\n    return {\"s\": \"t\", \"i\": 7}\n\nprint(vals()[\"s\"], vals()[\"i\"])",
		"def xs():\n    return [1, 2]\n\nprint(2 in xs())",
		"def xs():\n    return [1, 2, 3]\n\nprint(len(xs()))",
		"def xs():\n    return [1, 2]\n\ntotal = 0\nfor x in xs():\n    total = total + x\nprint(total)",
		"def xs():\n    return [4, 5]\n\nprint(max(xs()[1], 2))",
		"def xs():\n    return [4, 5]\n\nq = xs()[1]\nprint(q)",
		"def xs():\n    return [1.5, 2]\n\nprint(xs()[0])",
		"def xs():\n    return [\"a\", \"b\"]\n\nprint(str(xs()[1]), repr(xs()[0]))",
		"def mk():\n    return [{}]\n\nprint(mk()[0])",
		"def mk():\n    return [{\"k\": 1}]\n\nprint(mk()[0][\"k\"])",
		"def mk():\n    return [[3]]\n\nprint(mk()[0][0])",
		"def m():\n    return {1: 2}\n\nprint(m()[1])",
		"def m():\n    return {\"a\": \"x\", \"b\": \"y\"}\n\nprint(m()[\"b\"])",
		// A slice of an answer is the same read one door further out: the copy `rt_slice` builds carries the
		// answer's slots with their tags, and the printer is the one that reads a tag
		// (roadmap Gap R.230, ADR 0335).
		"def b():\n    return [1, 2, 3]\n\nprint(b()[0:2])",
		"def b():\n    return [1.5, \"a\", [7]]\n\nprint(b()[0:2])",
		"def b():\n    return [1.5, \"a\", [7]]\n\nys = b()[0:2]\nprint(ys)",
		"def b():\n    return [1, 2, 3]\n\nprint(len(b()[0:2]))",
		"def z():\n    return {0: \"zero\"}\n\nprint(z()[0])",
		"def b():\n    return [1.5, \"a\", [7]]\n\nprint(b()[2][0], b()[1])",
		"def built():\n    xs = []\n    xs.append(9)\n    xs.append(8)\n    return xs\n\nprint(built()[1])",
		"def fill():\n    ys = []\n    for i in range(3):\n        ys.append(i * 2)\n    return ys\n\nprint(fill()[2])",
		"def nums():\n    return [1, 2, 3]\n\ni = 1\nprint(nums()[i])",
	} {
		t.Run(strings.ReplaceAll(src, "\n", "; "), func(t *testing.T) {
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

// The raises the subscript of an answer now produces are the program's exceptions, judged by exit class, by
// the silence on stdout and by the sentence on stderr — and exit 2, the code the contract reserves for a
// compiler bug, is forbidden on every one of them (ADR 0006, ADR 0214). Before the row these were exit 1
// refusals; a refusal that arrives where the reference raises is the wrong-answer class one step away.
func TestACallResultSubscriptRaisesThroughTheCLI(t *testing.T) {
	for _, tt := range []struct {
		src      string
		contains string
	}{
		{"def b():\n    return [1, 2]\n\nprint(b()[7])", "IndexError: list index out of range"},
		{"def b():\n    return [1, 2]\n\nprint(b()[-9])", "IndexError: list index out of range"},
		{"def c():\n    return {1: \"one\"}\n\nprint(c()[9])", "KeyError: 9"},
		{"def c():\n    return {\"a\": 1}\n\nprint(c()[\"zz\"])", "KeyError: 'zz'"},
		{"def c():\n    return {0: \"zero\"}\n\nprint(c()[7])", "KeyError: 7"},
		{"def mk():\n    return [{}]\n\nprint(mk()[0][\"k\"])", "KeyError: 'k'"},
		{"def b():\n    return [1, 2]\n\nprint(b()[\"a\"])", "TypeError: list indices must be integers or slices, not str"},
		{"def b():\n    return [1, 2]\n\ntry:\n    print(b()[7])\nexcept IndexError:\n    print(\"caught\")", "caught"},
	} {
		t.Run(strings.ReplaceAll(tt.src, "\n", "; "), func(t *testing.T) {
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

// The positions the row leaves to another road keep that road's sentence. What this row owns is the promise
// that none of them repeats the closed door's words — `index requires an inline list/dict/set literal` would
// admit that the answer is not a value at the subscript door, which is the defect being fixed here — and
// that a refusal never arrives at exit 0 or at the code a compiler bug owns (ADR 0249, ADR 0166).
func TestAPositionThatCannotReadACallResultSubscriptRefusesThroughTheCLI(t *testing.T) {
	for _, tt := range []struct {
		name       string
		src        string
		mustSay    string
		mustNotSay string
	}{
		// A set has no subscript door at all — that is Gap R.217's, and the answer lands on exactly the
		// sentence the set sitting in a variable lands on: the answer IS a value, and the door it reaches is
		// the one a set has never had.
		{
			name:       "a set answered by a call",
			src:        "def s():\n    return {1, 2}\n\nprint(s()[0])",
			mustSay:    "index of a non-literal variable",
			mustNotSay: "index requires an inline list/dict/set literal",
		},
		// An answer's slot written to is Gap R.229's door.
		{
			name:    "an answer as an item-assignment target",
			src:     "def b():\n    return [1, 2]\n\nb()[0] = 9\nprint(b())",
			mustSay: "container variable on the left",
		},
		// A slot of an answer used as a membership needle is refused by the membership door (Gap R.232) — the
		// same answer for the container in a variable, so the row keeps parity while it waits for the road.
		// The sentence names the door but prints the needle's Go representation for the construct (the same
		// row owns making it honest, ADR 0249).
		{
			name:       "a slot of an answer as a membership needle",
			src:        "def words():\n    return [\"hi\", \"ho\"]\n\nprint(words()[0] in words())",
			mustSay:    "needle",
			mustNotSay: "index requires an inline list/dict/set literal",
		},
		// An answer that is an element of a literal is Gap R.229's too; the refusal is that road's sentence,
		// so the contract here is the exit class, the silence on stdout and the wording of the road asked.
		{
			name:    "an answer that is an element of a literal",
			src:     "def xs():\n    return [1]\n\nprint([[9], xs()][1][0])",
			mustSay: "index",
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
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
			if tt.mustNotSay != "" && strings.Contains(stderr, tt.mustNotSay) {
				t.Fatalf("the refusal repeats the closed door's sentence %q — the answer must be a value at the subscript door:\n%s", tt.mustNotSay, stderr)
			}
		})
	}
}

// The pair door keeps what this row leaves it. A slot of an answer that carries a (payload, tag) pair is
// printed by ADR 0332's tagged read, and the numeric use of two such slots stays the pair road's refusal —
// the door this row grew must not grow by stealing the sentence that road owes them (ADR 0249's gate-and-door
// rule, and the reason the row is a rewrite rather than a second reader).
func TestThePairRoadKeepsWhatTheCallResultSubscriptLeavesIt(t *testing.T) {
	stdout, stderr, code := cliStreams(t, "--eval", "def mk():\n    return [1.0]\n\nprint(mk()[0])")
	if code != 0 {
		t.Fatalf("a float slot of an answer did not print at exit 0 (exit %d):\nstdout %q\nstderr %q", code, stdout, stderr)
	}
	if !strings.Contains(stdout, "1.0") {
		t.Fatalf("the float slot of an answer printed %q, the reference prints \"1.0\"", stdout)
	}
	for _, tt := range []struct {
		src  string
		must string
	}{
		{"def mk():\n    return [1.5, 2.5]\n\nprint(mk()[0] + mk()[1])", "more than one kind"},
		{"def mk():\n    return [{}]\n\nprint(mk()[0] + mk()[0])", "more than one kind"},
		{"def mk():\n    return [{\"k\": 1}]\n\nprint(mk()[0][\"k\"] + 1)", "index must be a constant"},
	} {
		t.Run(strings.ReplaceAll(tt.src, "\n", "; "), func(t *testing.T) {
			stdout, stderr, code := cliStreams(t, "--eval", tt.src)
			if code == 2 {
				t.Fatalf("exit 2 on a legal program — the code the contract reserves for a compiler bug (ADR 0166):\n%s", stderr)
			}
			if code != 1 {
				t.Fatalf("exit %d, want 1: the pair road owns this position\nstdout %q\nstderr %q", code, stdout, stderr)
			}
			if strings.TrimSpace(stdout) != "" {
				t.Fatalf("the refusal printed %q on stdout", stdout)
			}
			if !strings.Contains(stderr, tt.must) {
				t.Fatalf("the pair road's sentence is missing %q:\n%s", tt.must, stderr)
			}
			if strings.Contains(stderr, "index requires an inline list/dict/set literal") {
				t.Fatalf("the answer was not a value at the subscript door:\n%s", stderr)
			}
		})
	}
}

// The shape of the answer stays the shape ADR 0333 gave it: the subscript road rewrites to the rooted name
// and asks the existing read, so no module puts a container global in a value position — the one line that
// made `llc` reject the module, and the code the exit-code contract counts as the compiler's bug.
func TestNoCallResultSubscriptModuleReturnsAGlobalToAValuePosition(t *testing.T) {
	for _, src := range []string{
		"def build():\n    return [1, 2, 3]\n\nprint(build()[0], build()[2])",
		"def cfg():\n    return {\"a\": 1, \"b\": 2}\n\nprint(cfg()[\"b\"])",
		"def rows():\n    return [{\"name\": \"zed\"}]\n\nprint(rows()[0][\"name\"])",
		"def vals():\n    return {\"s\": \"t\", \"i\": 7}\n\nprint(vals()[\"s\"], vals()[\"i\"])",
		"def grid():\n    return [[1, 2], [3, 4]]\n\nprint(grid()[1][0])",
		"def fill():\n    ys = []\n    for i in range(3):\n        ys.append(i * 2)\n    return ys\n\nprint(fill()[2])",
	} {
		t.Run(strings.ReplaceAll(src, "\n", "; "), func(t *testing.T) {
			res, err := lang.Compile(src)
			if err != nil {
				t.Fatalf("compile: %v", err)
			}
			if strings.Contains(res.IR, "ret i32 @.") {
				t.Fatalf("a global sits in a `ret` — the shape that made `llc` reject the module (ADR 0166):\n%s",
					bodyAroundGlobalReturn(res.IR))
			}
			if !strings.Contains(res.IR, "@main") {
				t.Fatalf("the module has no @main to read: %s", res.IR)
			}
		})
	}
}

// The fold is the negative control beside these rows: an all-integer literal read at an integer position
// still emits a constant, allocates nothing and asks none of the object doors — the subscript road opens
// those doors for a call's answer and for nothing the fold already answers (ADR 0249's gate-and-door rule,
// asserted where a reader can see the IR).
func TestTheFoldRoadStillCarriesNoDoorThroughACallResult(t *testing.T) {
	for _, src := range []string{`print([1, 2][0])`, `print([[1, 2][0], 3])`, `print([{1: 2}[1], 3])`} {
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
		body := cliMainBody(ir)
		for _, forbidden := range []string{"call i32 @rt_get_elem(", "call i32 @rt_dict_get_tagged(", "call i32 @rt_tag_of(", "call i8* @rt_alloc("} {
			if strings.Contains(body, forbidden) {
				t.Fatalf("%q opened the pair door (%s) for a read the fold answers:\n%s", src, forbidden, body)
			}
		}
	}
}

// The whole matrix as one program, line for line against the recorded reference answer.
func TestTheCallResultSubscriptProgramMatchesTheReferenceThroughTheCLI(t *testing.T) {
	src, err := os.ReadFile("programs/container_subscript.gy")
	if err != nil {
		t.Fatalf("read program: %v", err)
	}
	if strings.TrimSpace(string(src)) == "" {
		t.Fatal("the program is empty")
	}
	want, err := os.ReadFile("testdata/container_subscript.expected")
	if err != nil {
		t.Fatalf("read the reference's answer: %v", err)
	}
	stdout, stderr, code := cliStreams(t, "--file", "programs/container_subscript.gy")
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

func TestTheCallResultSubscriptProgramIsAnAssertedMatrixRow(t *testing.T) {
	cases := conformanceCases()
	var row *lang.ConformanceCase
	for i := range cases {
		if cases[i].Name == "container_subscript.gy" {
			row = &cases[i]
			break
		}
	}
	if row == nil {
		t.Fatal("no conformance matrix row runs container_subscript.gy")
	}
	if !row.Asserted {
		t.Fatalf("row %s is not asserted: its stdout would run unwatched", row.ID)
	}
	if row.Oracle != "match" {
		t.Fatalf("row %s declares oracle %q; the compiled leg answers this program, so it is a match", row.ID, row.Oracle)
	}
	for _, want := range []string{
		`print(make_list()[0], make_list()[-1], make_list()[-3])`,
		`print(make_dict()["a"], make_dict()["b"])`,
		`make_nested()[0][1]`,
		`make_mixed()[1]`,
		`print(make_list()[9])`,
		`print(make_dict()["zz"])`,
	} {
		if !strings.Contains(row.Source, want) {
			t.Fatalf("row %s runs a program without %q", row.ID, want)
		}
	}
}
