package integration

import (
	"strings"
	"testing"

	"github.com/donutloop/gusty/pkg/lang"
)

// The subscript that stands on a read the compiler answered at compile time, through the CLI (roadmap
// **Gap R.227**, **ADR 0440**).
//
// At this altitude the symptom is an exit code and a line on stderr. `gustyc --eval
// 'print([[1, 2][0][0], 1])'` stopped the build at exit **1** with `index requires an inline list/dict/set
// literal` — a sentence about a literal the program did write, aimed at the wrong half — while CPython
// raises `TypeError: 'int' object is not subscriptable`, which the program can catch. The fold is not the
// bug: `[1, 2][0]` IS the integer 2, and `(*irGen).constantSubscriptKind` walks that one fold level so the
// outer subscript sees `1[0]` and emits the raise through the same `constantTrap` the KeyError and
// IndexError folds use. Every expectation below is what `python3` printed for the same source, and exit **2**
// — the code the contract reserves for a compiler bug (ADR 0166) — is forbidden on every row (ADR 0006).

// TestTheFoldedSubscriptRaisesThroughTheCLI is the raise half: class, sentence, an empty stdout, and the
// trap's exit class rather than a refusal's.
func TestTheFoldedSubscriptRaisesThroughTheCLI(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		src      string
		contains string
	}{
		{`print([[1, 2][0][0], 1])`, "TypeError: 'int' object is not subscriptable"},
		{`print([[1][0][0], 1])`, "TypeError: 'int' object is not subscriptable"},
		{`print([[1, 2][0][0]])`, "TypeError: 'int' object is not subscriptable"},
		{`print([1, 2, 3][0][0])`, "TypeError: 'int' object is not subscriptable"},
		{`print({(1, 2)[0][0]})`, "TypeError: 'int' object is not subscriptable"},
		{`print([[1, 2][0][0] + 1])`, "TypeError: 'int' object is not subscriptable"},
		{`print([[1, 2][0][0] == 1])`, "TypeError: 'int' object is not subscriptable"},
		{`print([[1, 2][0][0] in [1]])`, "TypeError: 'int' object is not subscriptable"},
		{`print(str([[1, 2][0][0], 1]))`, "TypeError: 'int' object is not subscriptable"},
		{"xs = [[1, 2][0][0], 1]\nprint(xs)", "TypeError: 'int' object is not subscriptable"},
		{`print([1, 2][0][0] if 1 else 0)`, "TypeError: 'int' object is not subscriptable"},
		{`print([[1, 2][0][0], [3][0][0]])`, "TypeError: 'int' object is not subscriptable"},
		{`print(sorted([[1, 2][0][0], 1]))`, "TypeError: 'int' object is not subscriptable"},
		{`print([[1, 2][0][0] for x in [1]])`, "TypeError: 'int' object is not subscriptable"},
		{`print([x for x in [[1, 2][0][0]]])`, "TypeError: 'int' object is not subscriptable"},
		{`print({[1, 2][0][0]})`, "TypeError: 'int' object is not subscriptable"},
		{`print([[True][0][0]])`, "TypeError: 'bool' object is not subscriptable"},
		{`print([[1.5][0][0]])`, "TypeError: 'float' object is not subscriptable"},
		{`print([[None][0][0]])`, "TypeError: 'NoneType' object is not subscriptable"},
		{`print([[[None]][0][0][0]])`, "TypeError: 'NoneType' object is not subscriptable"},
		// The inner read's own accident wins the order, because it is the one the program wrote first.
		{`print([[[1, 2][5]][0]])`, "IndexError: list index out of range"},
	} {
		t.Run(strings.ReplaceAll(tt.src, "\n", "; "), func(t *testing.T) {
			t.Parallel()
			stdout, stderr, code := cliStreams(t, "--eval", tt.src)
			if code == 2 {
				t.Fatalf("the toolchain failed on its own program:\n%s", stderr)
			}
			if code == 1 && strings.Contains(stderr, "codegen") {
				t.Fatalf("the program stopped the build (%q) where the reference raises %s — a fold that refuses instead of raising is this row's regression", stderr, tt.contains)
			}
			if code != 3 {
				t.Fatalf("exit %d, want the trap class 3; stdout %q stderr %q", code, stdout, stderr)
			}
			if strings.TrimSpace(stdout) != "" {
				t.Fatalf("the trap printed %q before raising", stdout)
			}
			if !strings.Contains(stderr, tt.contains) {
				t.Fatalf("raised the wrong sentence, want %q, got:\n%s", tt.contains, stderr)
			}
		})
	}
}

// The same raises caught by the handler the program itself writes, at the exit code that means the program
// finished — judged, as always here, against what `python3` prints for the same source.
func TestTheFoldedSubscriptRaiseIsCatchableThroughTheCLI(t *testing.T) {
	t.Parallel()
	for _, src := range []string{
		"try:\n    print([[1, 2][0][0], 1])\nexcept TypeError as e:\n    print(\"caught\", e)",
		"try:\n    print([[1][0][0], 1])\nexcept TypeError:\n    print(\"caught\")",
		"try:\n    print([[True][0][0]])\nexcept TypeError as e:\n    print(\"caught\", e)",
		"try:\n    print([[None][0][0]])\nexcept TypeError as e:\n    print(\"caught\", e)",
		"try:\n    print([[[1, 2][5]][0]])\nexcept IndexError as e:\n    print(\"caught\", e)",
		"try:\n    print([[1, 2][0][0] + 1])\nexcept TypeError as e:\n    print(\"caught\", e)",
	} {
		t.Run(strings.ReplaceAll(src, "\n", "; "), func(t *testing.T) {
			t.Parallel()
			want := pythonOutput(t, src+"\n")
			stdout, stderr, code := cliStreams(t, "--eval", src)
			if code == 2 {
				t.Fatalf("the toolchain failed on its own program:\n%s", stderr)
			}
			if code != 0 {
				t.Fatalf("exit %d, want 0: the handler took the raise in the reference\nstderr %q", code, stderr)
			}
			if got := strings.TrimSpace(stdout); got != strings.TrimSpace(want) {
				t.Fatalf("printed %q, the reference prints %q", got, strings.TrimSpace(want))
			}
		})
	}
}

// The controls: the chains the fold and the pair door already answer must keep answering, byte for byte with
// the reference, and a text stays subscriptable twice because `"ab"[0][0]` is `'a'[0]` and answers `'a'`.
// A refusal fails the row, which is what makes this table a gate rather than a list.
func TestTheFoldedSubscriptControlsStillAnswerThroughTheCLI(t *testing.T) {
	t.Parallel()
	for _, src := range []string{
		`print([[1, 2][0], 1])`,
		`print([[[1, 2][0]][0], 1])`,
		`print([[[1], [2]][1][0], 5])`,
		`print([[[1, 2], [3, 4]][1][0], 9])`,
		`print([[1, 2, 3][1] + 1])`,
		`print([[1, 2][0] == 1])`,
		`print([1, 2][1])`,
		`print(["ab"[0][0]])`,
		`print(["ab"[0], "c"[0]])`,
		"d = {\"a\": {\"b\": [1]}}\nprint(d[\"a\"][\"b\"][0])",
		`print({"a": [1, 2][0]}["a"])`,
		`print([[[1, 2]], [3]][0][0][0])`,
		`print([[[1, 2][0], [3][0]]])`,
		`print([[[1, 2][0]], [3]][0][0])`,
		"xs = [[1, 2][0], 1]\nprint(xs[0])",
		"xs = [[1, 2][0], 1]\nfor v in xs:\n    print(v)",
	} {
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

// What the sweep left owed, pinned with today's bytes beside the reference's rather than papered over. Both
// rows measure identically on the build before the fix (`b276ddf`) and on this one, which is what keeps them
// rows rather than part of this commit.
func TestWhatTheFoldedSubscriptSweepStillOwesThroughTheCLI(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		name   string
		src    string
		today  string // what this backend does today, at this altitude
		python string // what the reference does
		gap    string
	}{
		{
			"len of a literal never evaluates the literal's elements",
			"print(len([[1, 2][0][0], 1]))",
			"2", "TypeError: 'int' object is not subscriptable",
			"roadmap Gap R.494",
		},
		{
			"and so the call inside the literal never runs either",
			"def f():\n    print(\"side\")\n    return 1\nprint(len([f()]))",
			"1", "side\n1",
			"roadmap Gap R.494",
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			stdout, stderr, code := cliStreams(t, "--eval", tt.src)
			if code == 2 {
				t.Fatalf("the toolchain failed on its own program:\n%s", stderr)
			}
			if got := strings.TrimSpace(stdout); got != tt.today {
				t.Fatalf("today's answer is %q, not the %q this row pins (the reference answers %q)", got, tt.today, tt.python)
			}
			// The reference is asked through the same door the oracle uses, and the row fails if its side of
			// the pair has stopped being what this row records: a divergence is only a row while both legs
			// still disagree (roadmap Gap R.204's rule about claims that cannot be re-measured).
			pyOut, pyErr, runErr := lang.PythonRun(tt.src + "\n")
			ref := strings.TrimSpace(pyOut)
			if runErr != nil {
				lines := strings.Split(strings.TrimSpace(pyErr), "\n")
				ref = lines[len(lines)-1]
			}
			if ref == "" {
				t.Fatalf("the reference gave no verdict for this program")
			}
			if ref != tt.python {
				t.Fatalf("the reference's answer is not what this row records: got %q, the row says %q (out %q err %q)", ref, tt.python, pyOut, pyErr)
			}
			if code != 0 {
				t.Fatalf("gusty exited %d on a program whose answer this row pins as %q:\n%s", code, tt.today, stderr)
			}
		})
	}
}
