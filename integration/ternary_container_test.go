package integration

import (
	"strings"
	"testing"

	"github.com/donutloop/gusty/pkg/lang"
)

// This file is the REFERENCE leg of **ADR 0405** (roadmap Gap R.385): what the LINKED PROGRAM answers where
// an arm of a ternary is a container. The pkg/lang side of the row (ternary_container_test.go) asks the
// codegen its IR claim; this side asks the program its bytes, against CPython, on the execution backend the
// CLI offers. Every answered row was recorded from the reference by tools/recmerge first
// (TestTheContainerArmFamilyIsOnTheRecord): the compiled leg is the thing under test and never writes its own
// answers (ADR 0309), and the shapes it could not agree on are the pinned table below, not the record.
//
// The bug this row closes is the exit-2 class ADR 0166 reserves for a compiler bug. `print([1, 2] if 1 else
// [9])` lowered its arm through the number road, which renders a container literal as the ADDRESS of its
// compile-time global, and handed that to printf in an `i32` slot —
//
//	%t1 = call i32 (i8*, ...) @printf(i8* getelementptr(…@.fmt1…), i32 @.lst1)
//	llc-20: error: global variable reference must have pointer type      → EXIT 2
//
// — and two of the family were quieter than the crash: the dict pair and the tuple pair printed `0`, the heap
// handle as the number the value is not, at the code of success (ADR 0188's rule, ADR 0192's rule, one
// construct later). Both legs were measured against the parent commit (`5985031`): 20 of the 25 programs in
// the answered family moved, 12 of them off exit 2, 5 off a wrong answer at exit 0 and 3 off a refusal in
// words; the other 5 answered before this cycle and are here as the control that the fix did not move them
// (ADR 0273).
//
// What the family still owes is written out below rather than hidden: the doors that ASK a container ternary
// a question — `str(...)`, a loop's iterable, a `def`'s `return`, a `%` format, an interpolation, `len` and a
// position — still read the arm as a word (roadmap Gap R.393, pinned with today's bytes beside the
// reference's), and an arm pair that agrees on no kind is refused in words instead of printed (roadmap Gap
// R.394, whose answer is the tag printer Gap R.67 / ADR 0333 built for the answer of a call).

// ternaryContainerCase is one answered program of the family: what the source writes, and what the reference
// prints for it.
type ternaryContainerCase struct {
	name string
	src  string
	want string
}

// ternaryContainerCLIRows is the answered family at this altitude: the three kinds, the comprehension
// spellings, the positions that read an arm, and the control rows whose arms answer a word. Every `want` is
// what the pinned CPython printed for the same source (Gap N's standing rule), re-asked at run time below so
// a stale expectation fails rather than passing quietly.
func ternaryContainerCLIRows() []ternaryContainerCase {
	return []ternaryContainerCase{
		{"list arm at the print door", "print([1, 2] if 1 else [9])", "[1, 2]\n"},
		{"dict arm at the print door", "print({\"a\": 1} if 1 else {\"b\": 2})", "{'a': 1}\n"},
		{"set arm at the print door", "print({1, 2} if 1 else {3})", "{1, 2}\n"},
		{"tuple arm at the print door", "print((1, 2) if 1 else (3, 4, 5))", "(1, 2)\n"},
		{"arm the reference has to evaluate", "print([1, 2] if (lambda v: v)(1) else [9])", "[1, 2]\n"},
		{"arm that builds the container itself", "print((lambda: [1, 2])() if 1 else [9])", "[1, 2]\n"},
		{"list comprehension arm", "print([v for v in [1, 2, 3] if v > 1] if 1 else [9])", "[2, 3]\n"},
		{"dict comprehension arm", "print({v: 1 for v in [1, 2]} if 1 else {9: 0})", "{1: 1, 2: 1}\n"},
		{"set comprehension arm", "print({v for v in [1, 2]} if 1 else {9})", "{1, 2}\n"},
		{"comprehension arm of containers", "print([[1] for _ in [1, 2]] if 1 else [9])", "[[1], [1]]\n"},
		{"comprehension arm whose elements carry their own kinds", "print([v for v in [1, \"a\", True]] if 1 else [9])", "[1, 'a', True]\n"},
		{"arms bound and read by position", "arms = [1, 2] if 1 else [9]\nprint(arms[0], arms[1])", "1 2\n"},
		{"arm handed to a function", "def head(xs):\n    return xs[0]\n\nprint(head([1, 2] if 1 else [9]))", "1\n"},
		{"dict arm handed to a function", "def headOf(d):\n    return d[\"a\"]\n\nprint(headOf({\"a\": 7} if 1 else {\"a\": 0}))", "7\n"},
		{"arm handed to the ordering builtin", "print(sorted([2, 1] if 1 else [9]))", "[1, 2]\n"},
		{"arm asked a membership question", "print(1 in ([1, 2] if 1 else [9]))", "True\n"},
		{"chain of ternaries with container arms", "print([1] if 1 else [2] if 0 else [3])", "[1]\n"},
		{
			"the run-time test takes each arm in turn, and the arm it rejects does not run",
			"def hot():\n    print(\"hot\")\n    return [1, 2]\n\ndef cold():\n    print(\"cold\")\n    return [9]\n\ngo = 1 < 2\nprint(hot() if go else cold())\nback = 1 > 2\nprint(hot() if back else cold())",
			"hot\n[1, 2]\ncold\n[9]\n",
		},
		{"comprehension arm chosen by a run-time test", "t = 1 < 2\nprint([v for v in [1, 2, 3] if v > t] if t else [9])", "[2, 3]\n"},
		{"arm inside a method body", "class C:\n    def m(self):\n        print([1, 2] if 1 else [9])\n\nC().m()", "[1, 2]\n"},
		{"arm inside a while body", "n = 2\nwhile n > 0:\n    print([n, n] if n else [9])\n    n = n - 1", "[2, 2]\n[1, 1]\n"},
		{"arm inside a for body", "for v in [1, 2]:\n    print([v] if v else [])", "[1]\n[2]\n"},
		{"arm inside an except handler", "try:\n    print([1, 2][9])\nexcept IndexError:\n    print([3, 4] if 1 else [9])", "[3, 4]\n"},
		{"arm inside a match arm", "match 1:\n    case 1:\n        print([1, 2] if 1 else [9])", "[1, 2]\n"},
		{"arm compared to a container", "print(([1, 2] if 1 else [9]) == [1, 2])", "True\n"},
		{"arm whose test is a call that hands back a container", "def yes():\n    return [1]\n\nprint([1, 2] if yes() else [9])", "[1, 2]\n"},
		{"tuple arm compared to a tuple", "print((1, 2) == ((1, 2) if 1 else (5,)))", "True\n"},
		{"dict arm of containers", "print({\"a\": [1, 2]} if 1 else {\"b\": {1: 2}})", "{'a': [1, 2]}\n"},
		{"empty list arm", "print([] if 1 else [9])", "[]\n"},
		{"empty dict arm", "print({} if 1 else {9: 1})", "{}\n"},
		{"empty set arm", "print(set() if 1 else {9})", "set()\n"},
		{"bound dict arm read through two positions", "xs = {\"k\": [1, 2]} if 1 else {}\nprint(xs[\"k\"][1])", "2\n"},
		{"length of a container arm's object", "print(len({v for v in [1, 2, 3] if v > 1}) if 1 else 0)", "2\n"},
		// The controls (ADR 0273): an arm that answers a word keeps the one-word road, and the arm a written
		// test rejected is not in the program at all. These answered before this cycle; the row's claim is only
		// meaningful beside them.
		{"control: a number arm", "print(1 if 1 else [9])", "1\n"},
		{"control: a text arm", "print(\"t\" if 1 else [9])", "t\n"},
	}
}

// TestTheContainerArmFamilyAnswersLikeTheReference is the reference leg: every answered program prints,
// linked, exactly what CPython prints for the same source, and none of them reaches exit 2.
func TestTheContainerArmFamilyAnswersLikeTheReference(t *testing.T) {
	t.Parallel()
	for _, tc := range ternaryContainerCLIRows() {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			// One directory per subtest: these cases are parallel, and a shared name written by each of them
			// would let one program's bytes be run under another's name.
			path := writeSrc(t, t.TempDir(), "ternary_container.gy", tc.src)
			want, ok := cpythonOut(t, path)
			if !ok {
				t.Skip("no live python3 to ask")
			}
			if want != tc.want {
				t.Fatalf("the reference no longer prints %q for this program (prints %q) — this table's expectation went stale\n%s", tc.want, want, tc.src)
			}
			for _, engine := range cliEngines {
				out, code := cliRunCode(t, engine, path)
				if code == 2 {
					t.Fatalf("%s rejected the compiler's own module (ADR 0166 reserves exit 2 for a compiler bug):\n%s", engine, out)
				}
				if code != 0 {
					t.Fatalf("%s exited %d on a program the reference prints %q — an arm was refused:\n%s", engine, code, want, out)
				}
				if out != want {
					t.Fatalf("%s printed %q, CPython printed %q — for %s", engine, out, want, tc.src)
				}
			}
		})
	}
}

// TestTheContainerArmFamilyIsOnTheRecord is ADR 0302's rule at this altitude: every program this row answers
// has an answer on record, written from the reference by tools/recmerge and cross-checked against the compiled
// leg — never written by this backend. A row whose record disappeared fails here.
func TestTheContainerArmFamilyIsOnTheRecord(t *testing.T) {
	t.Parallel()
	onRecord := 0
	for _, tc := range ternaryContainerCLIRows() {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			if !lang.HasGoldenAnswer(tc.src) {
				t.Fatalf("%q is not on the record — record it from the reference with tools/recmerge, never from this backend", tc.src)
			}
			onRecord++
		})
	}
	if onRecord == 0 {
		t.Fatal("not one container-arm program is on the record: the record leg is not being walked")
	}
}

// TestTheContainerArmProgramIsTheReferenceLineForLine is the conformance corpus's own claim, asked of the file
// rather than of a table: integration/programs/ternary_container.gy prints the reference's bytes for every
// line, with no line dropped between the two.
func TestTheContainerArmProgramIsTheReferenceLineForLine(t *testing.T) {
	t.Parallel()
	src := readProgramSrc("ternary_container")
	want, ok := cpythonPlainOut(t, t.TempDir(), src)
	if !ok {
		t.Skip("no live python3 to ask")
	}
	for _, engine := range cliEngines {
		path := writeSrc(t, t.TempDir(), "ternary_container.gy", src)
		out, code := cliRunCode(t, engine, path)
		if code != 0 {
			t.Fatalf("%s exited %d: %s", engine, code, out)
		}
		if out != want {
			wl, gl := strings.Split(want, "\n"), strings.Split(out, "\n")
			for i := 0; i < len(wl) || i < len(gl); i++ {
				var w, g string
				if i < len(wl) {
					w = wl[i]
				}
				if i < len(gl) {
					g = gl[i]
				}
				if w != g {
					t.Fatalf("%s differs on line %d: printed %q, CPython prints %q", engine, i+1, g, w)
				}
			}
		}
	}
}

// TestTheContainerArmProgramIsOnTheRecord keeps the corpus program itself on the record leg.
func TestTheContainerArmProgramIsOnTheRecord(t *testing.T) {
	t.Parallel()
	src := readProgramSrc("ternary_container")
	if !lang.HasGoldenAnswer(src) {
		t.Fatalf("integration/programs/ternary_container.gy is not on the record — record it from the reference with tools/recmerge")
	}
}

// ternaryContainerPinned is one shape of the family this row measured and does NOT own: the exit code the
// linked program gives today, the bytes or sentence it gives with it, the reference's answer beside it, and
// the roadmap row that owns the fix. A row that stops being true FAILS, and the case is DELETED rather than
// edited — the only way a pinned wrong answer cannot quietly become a rumour.
type ternaryContainerPinned struct {
	name     string
	gap      string
	src      string
	code     int
	today    string // the pinned stdout the program produces today (always "" for the two classes below)
	mustMach string // the substring stderr has to carry: the llc message, or the sentence the refusal says
	want     string // what CPython prints for the same source
}

func ternaryContainerPinnedRows() []ternaryContainerPinned {
	return []ternaryContainerPinned{
		// Gap R.393 — the print door has the arms; the doors that ASK the arm a question read it as a word, and
		// either hand the assembler the folded global (exit 2, the code ADR 0166 reserves for a compiler bug) or
		// decline with a sentence about literals (exit 1). Each of these reproduces on the parent commit
		// (`5985031`) exactly as pinned here.
		{
			name:     "str() of a container ternary",
			gap:      "Gap R.393",
			src:      "print(str([1, 2] if 1 else [9]))",
			code:     2,
			mustMach: "global variable reference must have pointer type",
			want:     "[1, 2]\n",
		},
		{
			name:     "a loop over a container ternary",
			gap:      "Gap R.393",
			src:      "for v in ([1, 2] if 1 else [9]):\n    print(v)",
			code:     2,
			mustMach: "global variable reference must have pointer type",
			want:     "1\n2\n",
		},
		{
			name:     "a def that returns a container ternary",
			gap:      "Gap R.393",
			src:      "def f():\n    return [1, 2] if 1 else [9]\n\nprint(f())",
			code:     2,
			mustMach: "global variable reference must have pointer type",
			want:     "[1, 2]\n",
		},
		{
			name:     "a container ternary in a % format",
			gap:      "Gap R.393",
			src:      "print(\"%s\" % ([1, 2] if 1 else [9]))",
			code:     2,
			mustMach: "global variable reference must have pointer type",
			want:     "[1, 2]\n",
		},
		{
			name:     "a container ternary in an interpolation",
			gap:      "Gap R.393",
			src:      "print(f\"{[1, 2] if 1 else [9]}\")",
			code:     2,
			mustMach: "global variable reference must have pointer type",
			want:     "[1, 2]\n",
		},
		{
			name:     "the length of a container ternary",
			gap:      "Gap R.393",
			src:      "print(len([1, 2] if 1 else [9]))",
			code:     1,
			mustMach: "len requires an inline list/dict/set literal",
			want:     "2\n",
		},
		{
			name:     "a position in a container ternary",
			gap:      "Gap R.393",
			src:      "print(([1, 2] if 1 else [9])[0])",
			code:     1,
			mustMach: "index requires an inline list/dict/set literal",
			want:     "1\n",
		},
		{
			name:     "a position in a bound tuple arm",
			gap:      "Gap R.393",
			src:      "xs = (1, 2) if 1 else (3,)\nprint(xs[0], len(xs))",
			code:     1,
			mustMach: "index of a non-literal variable",
			want:     "1 2\n",
		},
		// Gap R.394 — the arm pair this row REFUSES where the parent commit handed the same program to `llc` and
		// died: ADR 0166's preferred direction, a refusal in words over a rejected module. The reference prints
		// the taken arm whatever the arms' kinds are, so the answer exists and the tag printer is what is missing.
		{
			name:     "a list arm and a dict arm, printed",
			gap:      "Gap R.394",
			src:      "t = 1 < 2\nprint([1, 2] if t else {\"a\": 1})",
			code:     1,
			mustMach: "cannot choose one printer",
			want:     "[1, 2]\n",
		},
		{
			name:     "a list arm and a number arm, printed",
			gap:      "Gap R.394",
			src:      "t = 1 < 2\nprint([1, 2] if t else 7)",
			code:     1,
			mustMach: "cannot choose one printer",
			want:     "[1, 2]\n",
		},
		{
			name:     "a number arm and a list arm, printed",
			gap:      "Gap R.394",
			src:      "t = 1 < 2\nprint(7 if t else [1, 2])",
			code:     1,
			mustMach: "cannot choose one printer",
			want:     "7\n",
		},
		{
			name:     "a text arm and a list arm, printed",
			gap:      "Gap R.394",
			src:      "t = 1 < 2\nprint(\"text\" if t else [9])",
			code:     1,
			mustMach: "cannot choose one printer",
			want:     "text\n",
		},
	}
}

// TestWhatTheContainerArmRoadStillOwesAtTheCLI holds the shapes this row measured and left on the tracker.
// Three assertions per row: the reference answers (so the row describes a program, not a broken one), the
// program produces nothing on stdout (a program whose module was refused, or which was refused in words, does
// not print), and stderr carries the message pinned here with the exit code pinned here. Where the pinned
// code is 2 the row is filing a compiler bug against a roadmap row — the code ADR 0166 reserves for one — and
// says so out loud rather than shipping it silently.
func TestWhatTheContainerArmRoadStillOwesAtTheCLI(t *testing.T) {
	t.Parallel()
	for _, tc := range ternaryContainerPinnedRows() {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			path := writeSrc(t, t.TempDir(), "ternary_container_owed.gy", tc.src)
			want, ok := cpythonOut(t, path)
			if !ok {
				t.Skip("no live python3 to ask")
			}
			if want != tc.want {
				t.Fatalf("the reference no longer prints %q for this program (prints %q) — this table's expectation went stale\n%s", tc.want, want, tc.src)
			}
			for _, engine := range cliEngines {
				stdout, stderr, code := cliStreams(t, engine, path)
				if code == 0 && stdout == want {
					t.Fatalf("%s: %s now prints the reference's %q — the row closed and this pin has to be DELETED, "+
						"with the program moved into the answered table\n%s", engine, tc.gap, want, tc.src)
				}
				if code != tc.code {
					t.Fatalf("%s: %s pinned exit %d, the program exited %d (the reference prints %q)\nstdout %q\nstderr %q",
						engine, tc.gap, tc.code, code, want, stdout, stderr)
				}
				if stdout != "" {
					t.Fatalf("%s: %s printed %q while the program was refused — a program that does not run prints nothing\n%s",
						engine, tc.gap, stdout, tc.src)
				}
				if !strings.Contains(stderr, tc.mustMach) {
					t.Fatalf("%s: %s was filed as %q and now behaves differently (exit %d, stderr %q): the row needs "+
						"re-measuring — and if it now prints %q, close the row and delete this case",
						engine, tc.gap, tc.mustMach, code, strings.TrimSpace(stderr), want)
				}
			}
		})
	}
}
