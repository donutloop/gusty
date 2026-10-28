package integration

import (
	"strings"
	"testing"
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

// What the sweep left owed is FILED, and it lives beside the door that owes it, so nothing is pinned
// here (**ADR 0440**'s row-hygiene rule). **Gap R.494** — `len` of a literal counted without performing
// its elements — is CLOSED by **ADR 0441** and its two CLI rows moved to
// integration/length_element_effects_test.go, where they now pin the reference's answer rather than
// today's; the `len` rows that are still wrong (`len` of a comprehension the run time builds, exit 2,
// **Gap R.496**) are pinned there too, and **Gap R.495**'s refusal stays pinned in
// pkg/lang/pair_element_read_test.go's filed table.
