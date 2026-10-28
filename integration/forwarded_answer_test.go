package integration

import (
	"strings"
	"testing"

	"github.com/donutloop/gusty/pkg/lang"
)

// A hand-off call — `def g(v): return v` — answers with the CALLER'S argument, so the kind word written into
// a container slot, a dict entry or a comprehension builder is the argument's kind, not the compiler's guess
// (roadmap **Gap R.502**, **ADR 0445**, **ADR 0273**'s element position, **ADR 0232**'s self-describing slots).
//
// Every row here is run through the CLI and against a live `python3` on the same bytes; the record leg
// (`lang.RecordedStdoutIs`) is what keeps the compiled answer from being the only answer on file
// (**Gap R.204**). The answers asked through this door:
//
//	def g(v):
//	    return v
//	print([g(x) for x in [1, "a", 2.5, None]])   # [1, 'a', 2.5, None] — was [1, 0, 1, 0]
//	print({g(x): 1 for x in ["a", "b"]})          # {'a': 1, 'b': 1}    — was {0: 1, 1: 1}
//	print([g(None), 1])                           # [None, 1]           — was [0, 1]
//
// and the accident it now raises: an answer that hands back a list, a dict or a set dies with the
// reference's own `TypeError: unhashable type: ...` when it is stored as a set member or a dict key, and an
// answer that hands back a TUPLE does not, because a tuple of hashables hashes.

const cliHandOff = "def g(v):\n    return v\n\n"

func TestAHandOffAnswerWearsItsArgumentsKindThroughTheCLI(t *testing.T) {
	t.Parallel()
	for _, src := range []string{
		cliHandOff + "print([g(2.5), 1])",
		cliHandOff + "print([g(\"a\"), 1])",
		cliHandOff + "print([g(None), 1])",
		cliHandOff + "print([g([1, 2]), 3])",
		cliHandOff + "print([g((1, 2)), 3])",
		cliHandOff + "print({1: g(None)})",
		cliHandOff + "print({g(None)})",
		cliHandOff + "print({g(\"a\")})",
		cliHandOff + "xs = []\nxs.append(g(None))\nprint(xs)",
		cliHandOff + "xs = [0]\nxs[0] = g(2.5)\nprint(xs)",
		cliHandOff + "print([g(v=2.5), 1])",
		"def g(v = 2.5):\n    return v\n\nprint([g(), 1])",
		// The three spellings of an omitted argument that used to answer the digit 0 (**Gap R.277**, **Gap R.171**):
		// the default IS the argument, and a hand-off reads its kind off it.
		"def d(y=None):\n    return y\n\nprint(d())",
		"def d(xs=[]):\n    return xs\n\nprint(d())",
		"def d(w={}):\n    return w\n\nprint(d())",
		"def f(v):\n    return v\n\nprint(f((lambda: None)()))",
		"def a1():\n    return None\n\ndef f(v):\n    return v\n\nprint(f(a1()))",
		// The parameter the `return` names need not be the first one: the kind is read off whichever argument
		// that position received.
		"def g(v, w):\n    return w\n\nprint([g(2.5, \"a\"), 1])",
		cliHandOff + "print([g(x) for x in [1, \"a\", 2.5, None]])",
		cliHandOff + "for v in [g(x) for x in [1, \"a\", 2.5, None]]:\n    print(v)",
		cliHandOff + "print({g(x): 1 for x in [\"a\", \"b\"]})",
		cliHandOff + "for k in {g(x): 1 for x in [\"a\", \"b\"]}:\n    print(k)",
		cliHandOff + "print({1: g(x) for x in [\"a\", 2.5]})",
		cliHandOff + "print({g(x) for x in [1, 2.5]})",
		cliHandOff + "print([g(x) for x in [1, \"a\", 2.5]][1])",
		cliHandOff + "print([g(x) for x in [1, \"a\", 2.5]][1] == \"a\" and len([g(x) for x in [1, \"a\", 2.5]]) == 3)",
		cliHandOff + "print({g((1, 2))})",
		cliHandOff + "print({g((1, 2)): 1})",
		// The negative controls, beside the rows they keep: an answer with a kind of its own wears its OWN
		// kind, and a comprehension with no call at all still answers from the compile-time fold.
		"def g(v):\n    return 1\n\nprint([g(2.5), 1])",
		"def g(v):\n    return v + v\n\nprint([g(1), 1])",
		cliHandOff + "print([g(x) for x in [1, 2, 3]])",
		"print([x * 2 for x in [1, 2, 3]])",
		"print({(1, 2): 5})",
	} {
		t.Run(strings.ReplaceAll(src, "\n", "; "), func(t *testing.T) {
			t.Parallel()
			want := pythonOutput(t, src+"\n")
			stdout, stderr, code := cliStreams(t, "--eval", src)
			if code == 2 {
				t.Fatalf("the toolchain failed on its own program (exit 2 — ADR 0166's code):\n%s", stderr)
			}
			if code != 0 {
				t.Fatalf("exit %d, want 0: the reference prints %q\nstderr %q", code, strings.TrimSpace(want), stderr)
			}
			if got := strings.TrimSpace(stdout); got != strings.TrimSpace(want) {
				t.Fatalf("printed %q, the reference prints %q", got, strings.TrimSpace(want))
			}
			// The record leg is not optional: **Gap R.502** moved these rows out of the filed tables only
			// after `tools/recmerge` wrote the entry from a reference/compiled agreement.
			lang.RecordedStdoutIs(t, src, strings.TrimSpace(want)+"\n")
		})
	}
}

func TestAnUnhashableHandOffAnswerRaisesTheReferencesOwnAccidentThroughTheCLI(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		name    string
		src     string
		class   string
		message string
	}{
		{
			"a handed-back list as a set member",
			cliHandOff + "print({g([1, 2])})",
			"TypeError", "unhashable type: 'list'",
		},
		{
			"a handed-back dict as a set member",
			cliHandOff + "print({g({\"k\": 1})})",
			"TypeError", "unhashable type: 'dict'",
		},
		{
			"a handed-back set as a dict key",
			cliHandOff + "print({g({1, 2}): 1})",
			"TypeError", "unhashable type: 'set'",
		},
		{
			"a handed-back list as a built set comprehension's member",
			cliHandOff + "print({g([1, 2]) for x in [1]})",
			"TypeError", "unhashable type: 'list'",
		},
		{
			"a handed-back container VARIABLE as a set member",
			cliHandOff + "xs = [1]\nprint({g(xs), 2})",
			"TypeError", "unhashable type: 'list'",
		},
	} {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			stdout, stderr, code := cliStreams(t, "--eval", tt.src)
			if code == 2 {
				t.Fatalf("the toolchain failed on its own program (exit 2 — ADR 0166's code):\n%s", stderr)
			}
			if code != 3 {
				t.Fatalf("exit %d, want the program's trap at exit 3; stdout %q stderr %q", code, stdout, stderr)
			}
			if stdout != "" {
				t.Fatalf("stdout %q, want the accident to print nothing", stdout)
			}
			want := tt.class + ": " + tt.message
			if !strings.Contains(stderr, want) {
				t.Fatalf("stderr does not carry %q:\n%s", want, stderr)
			}

			// The reference's own verdict, asked through the same door as every CLI row.
			pyOut, pyErr, runErr := lang.PythonRun(tt.src + "\n")
			if runErr == nil {
				t.Fatalf("the reference answered %q where this row expects %s: %s", pyOut, tt.class, tt.message)
			}
			ref := strings.TrimSpace(pyErr)
			if lines := strings.Split(ref, "\n"); len(lines) > 0 {
				ref = strings.TrimSpace(lines[len(lines)-1])
			}
			if ref != want {
				t.Fatalf("the reference reports %q, this row records %q", ref, want)
			}
			if recErr := lang.RecordedRunError(t, tt.src); recErr == nil {
				t.Fatalf("the record does not trap %q, although the reference and compiled leg both raise %q", tt.src, want)
			}
		})
	}
}

// TestAnUnhashableHandOffAnswerIsCatchableThroughTheCLI is the half that decides whether the raise is the
// program's own accident or the compiler's: a raised `TypeError` is catchable by the program that triggered
// it and exits 0, while a front-end refusal exits 1 and cannot be caught by anything.
func TestAnUnhashableHandOffAnswerIsCatchableThroughTheCLI(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		name string
		src  string
		want string
	}{
		{
			"a set member the hand-off hands back as a list",
			cliHandOff + "try:\n    print({g([1, 2])})\nexcept TypeError as e:\n    print(\"caught\", e)",
			"caught unhashable type: 'list'",
		},
		{
			"a dict key the hand-off hands back as a set",
			cliHandOff + "try:\n    print({g({1, 2}): 1})\nexcept TypeError as e:\n    print(\"caught\", e)",
			"caught unhashable type: 'set'",
		},
		{
			// The positive control beside the traps: the same call, a kind that hashes, no exception at all.
			"a tuple the hand-off hands back is a key, not an accident",
			cliHandOff + "try:\n    print({g((1, 2)): 1})\nexcept TypeError as e:\n    print(\"caught\", e)",
			"{(1, 2): 1}",
		},
	} {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			want := pythonOutput(t, tt.src+"\n")
			if strings.TrimSpace(want) != tt.want {
				t.Fatalf("the reference itself answered %q, this row expects %q", strings.TrimSpace(want), tt.want)
			}
			stdout, stderr, code := cliStreams(t, "--eval", tt.src)
			if code != 0 {
				t.Fatalf("exit %d, want 0 — the accident belongs to the program and the program catches it\nstderr %q",
					code, stderr)
			}
			if got := strings.TrimSpace(stdout); got != tt.want {
				t.Fatalf("printed %q, the reference prints %q", got, tt.want)
			}
			lang.RecordedStdoutIs(t, tt.src, tt.want+"\n")
		})
	}
}

// TestTheHandOffDoorRefusesWhatItDoesNotClaimThroughTheCLI pins the residue with today's exit code beside
// the reference's. A hand-off is a body of `return <parameter>` and nothing else; a body that rebinds the
// parameter or computes the answer keeps **Gap R.146**'s refusal, identically on the build before
// **ADR 0445** and on it, which is what makes each of these a row rather than damage from it.
func TestTheHandOffDoorRefusesWhatItDoesNotClaimThroughTheCLI(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		name      string
		src       string
		had       string
		reference string
		row       string
	}{
		{
			"a parameter the body rebinds is not a hand-off",
			"def g(v):\n    v = 7\n    return v\n\nprint([g(2.5), 1])",
			"this body's answer has no tag to travel with", "[7, 1]",
			"roadmap Gap R.146",
		},
		{
			"a body that computes its answer from the parameter is not a hand-off",
			"def g(v):\n    return v + 0\n\nprint([g(2.5), 1])",
			"the tag has nowhere to go", "[2.5, 1]",
			"roadmap Gap R.146",
		},
		{
			"a loop item whose kind the loop bound is a one-word position",
			cliHandOff + "xs = [1, \"a\", 2.5]\nprint([g(x) for x in xs])",
			"has nowhere to put the second one", "[1, 'a', 2.5]",
			"roadmap Gap R.146",
		},
	} {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			_, stderr, code := cliStreams(t, "--eval", tt.src)
			if code != 1 {
				t.Fatalf("exit %d, want the front-end's refusal at exit 1; stderr %q", code, stderr)
			}
			if !strings.Contains(stderr, tt.had) {
				t.Fatalf("the refusal does not carry %q:\n%s", tt.had, stderr)
			}
			want, _, err := lang.PythonRun(tt.src + "\n")
			if err != nil {
				t.Fatalf("the reference answers this program (%q): %v", want, err)
			}
			if got := strings.TrimSpace(want); got != tt.reference {
				t.Fatalf("the reference prints %q, this row records %q\nthe row: %s", got, tt.reference, tt.row)
			}
		})
	}
}
