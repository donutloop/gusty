package integration

import (
	"bytes"
	"io"
	"os"
	"os/exec"
	"strings"
	"testing"

	"github.com/donutloop/gusty/pkg/lang"
)

// integration/join_seam_test.go — the REFERENCE leg of **ADR 0408** (roadmap Gap R.403): what the LINKED
// PROGRAM prints, and what it raises, when `str.join` is asked of a list whose slots the compiler cannot read
// as literals.
//
// The row's claim is that the seam is the concatenation. `"-".join([f"a{n}", "z"])` is the same three texts
// the `+` road has bolted together while the program ran since **ADR 0229**, and the road refused anyway
// because its fold only accepted `*StringLit`s — so the family here is deliberately made of the shapes a
// person writes when they mean to build a string: an interpolation in a slot, a method's answer in a slot, a
// call's answer, a ternary, a tuple, a separator that is not ASCII. Each is asked of the pinned CPython
// line-for-line, on every CLI engine.
//
// The trap family is the half a fold usually gets wrong twice: an element that is not a text is a
// **TypeError the program can catch**, and the reference counts which item it gave up on. `sequence item 1:
// expected str instance, int found` is pinned byte-for-byte — including `bool`, which the reference does not
// call `int` here, and `NoneType`, which it does not call `None`.
//
// The measurement, taken with a build of the parent commit (`72d1755`) beside a build of this one over the 36
// unique programs this family carries across these two files, each asked of the pinned CPython: **3 of them agreed
// with the reference before, 31 after — 28 newly answering, none regressed.** The 5 that do not are the refusals
// pinned in `TestWhatTheJoinRoadStillOwesAtTheCLI` and `TestTheJoinSeamRefusalsAreMachineReadable` below — the list
// the program built (**Gap R.404**), the separator the program built (**Gap R.405**), the slot whose kind no road
// can name (**Gap R.400** / **L11.1**) — and the two wrong answers pinned in
// `TestTheJoinSeamAnswersWrongAtTheCLIFilesWhatTheCensusStillMisses` (**Gap R.406**). `tools/recmerge` wrote 33
// entries from CPython for this family and REFUSED those, which is the record leg refusing to testify for a backend
// that has no answer (ADR 0309) — and the reason each of them exists as a row rather than as a passing test.

func joinCLIAnswered() []struct {
	name string
	src  string
	want string
} {
	return []struct {
		name string
		src  string
		want string
	}{
		{"the row's own program", "n = 3\nprint(\"-\".join([f\"a{n}\", \"z\"]))\n", "a3-z\n"},
		{"one runtime slot", "n = 3\nprint(\"-\".join([f\"a{n}\"]))\n", "a3\n"},
		{"an empty separator", "n = 3\nprint(\"\".join([f\"a{n}\", \"z\"]))\n", "a3z\n"},
		{"a tuple written at the call", "n = 3\nprint(\"-\".join((\"a\", f\"b{n}\")))\n", "a-b3\n"},
		{"a call's answer in a slot", "def t():\n    return \"q\"\n\nn = 3\nprint(\"-\".join([f\"a{n}\", t()]))\n", "a3-q\n"},
		{"a method's answer in a slot", "n = 3\nprint(\"-\".join([f\"a{n}\".upper(), \"z\"]))\n", "A3-z\n"},
		{"three runtime slots", "n = 1\nprint(\", \".join([f\"x{n}\", f\"y{n}\", \"z\"]))\n", "x1, y1, z\n"},
		{"a non-ASCII separator", "n = 1\nprint(\"é\".join([f\"a{n}\", \"z\"]))\n", "a1éz\n"},
		{"a concatenated slot", "n = 3\nprint(\"-\".join([f\"a{n}\" + \"!\", \"z\"]))\n", "a3!-z\n"},
		{"a ternary slot", "c = 1\nn = 2\nprint(\"-\".join([f\"a{n}\" if c else \"f\", \"z\"]))\n", "a2-z\n"},
		{"the join inside a list", "n = 3\nprint([\"-\".join([f\"a{n}\", \"z\"]), 1])\n", "['a3-z', 1]\n"},
		{"the join bound and asked twice", "n = 3\ns = \"-\".join([f\"a{n}\", \"z\"])\nprint(s.upper(), len(s))\n", "A3-Z 4\n"},
		{"the join compared by content", "n = 3\nprint(\"-\".join([f\"a{n}\", \"z\"]) == \"a3-z\")\n", "True\n"},
		{"a repeated separator", "n = 3\nprint(\"--\".join([f\"a{n}\", \"z\"]))\n", "a3--z\n"},
		{"the empty list", "print(\"-\".join([]))\n", "\n"},
		{"the one-element list", "print(\"-\".join([\"a\"]))\n", "a\n"},
		{"the fold's own control", "print(\"-\".join([\"a\", \"b\"]))\n", "a-b\n"},
		// The seam has to survive LEAVING A BODY: the caller holds one word and a name, and which printer that
		// word reaches is decided by a pre-scan that never sees the body (roadmap Gap R.219 / ADR 0368's lesson,
		// applied to this road by ADR 0408).
		{"the seam handed back by a body", "def f():\n    n = 3\n    return \"-\".join([f\"a{n}\", \"z\"])\n\nprint(f())\n", "a3-z\n"},
		{"the seam bound in a frame and handed back", "def f(n):\n    s = \"-\".join([f\"a{n}\", \"z\"])\n    return s\n\nprint(f(3))\nprint(f(4))\n", "a3-z\na4-z\n"},
		{"the seam as a call argument", "def show(s):\n    print(s)\n\nshow(\"-\".join([f\"a{3}\", \"z\"]))\n", "a3-z\n"},
		{"the seam beside a body's number arm", "def f(k):\n    if k:\n        return \"-\".join([\"a\", \"b\"])\n    return 7\n\nprint(f(True))\nprint(f(False))\n", "a-b\n7\n"},
		{"the seam in a method's answer", "class T:\n    def row(self, n):\n        return \"-\".join([f\"a{n}\", \"z\"])\n\nprint(T().row(3))\n", "a3-z\n"},
		{"the seam compared from a body's answer", "def f(n):\n    return \"-\".join([f\"a{n}\", \"z\"])\n\nif f(3) == \"a3-z\":\n    print(\"eq\")\nelse:\n    print(\"ne\")\n", "eq\n"},
	}
}

// TestTheJoinSeamFamilyAnswersLikeTheReference is the reference leg: every program above is asked of the
// pinned CPython first, so a table whose expectation drifted fails here rather than certifying a drift.
func TestTheJoinSeamFamilyAnswersLikeTheReference(t *testing.T) {
	t.Parallel()
	for _, tc := range joinCLIAnswered() {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			path := writeSrc(t, t.TempDir(), "join_seam.gy", tc.src)
			want, ok := cpythonOut(t, path)
			if !ok {
				t.Skip("no live python3 to ask")
			}
			if want != tc.want {
				t.Fatalf("the reference no longer prints %q for this program (prints %q) — this table's expectation went stale\n%s", tc.want, want, tc.src)
			}
			for _, engine := range cliEngines {
				stdout, stderr, code := cliStreams(t, engine, path)
				if code == 2 {
					t.Fatalf("%s rejected the compiler's own module (ADR 0166 reserves exit 2 for a compiler bug):\n%s\n%s", engine, stderr, tc.src)
				}
				if code != 0 {
					t.Fatalf("%s exited %d on a program the reference prints %q — the join seam refused it:\n%s", engine, code, want, stderr)
				}
				if stdout != want {
					t.Fatalf("%s printed %q, CPython printed %q — for %s", engine, stdout, want, tc.src)
				}
			}
		})
	}
}

// joinCLITraps are the elements the reference raises over, through the linked binary: the exit code, the
// class, the sentence, and the position it counted.
func joinCLITraps() []struct {
	name string
	src  string
	want string
} {
	return []struct {
		name string
		src  string
		want string
	}{
		{"an int slot", "n = 3\nprint(\"-\".join([1, \"z\"]))\n", "TypeError: sequence item 0: expected str instance, int found"},
		{"an int after a text", "n = 3\nprint(\"-\".join([f\"a{n}\", 1]))\n", "TypeError: sequence item 1: expected str instance, int found"},
		{"a dict slot", "n = 3\nprint(\"-\".join([{\"a\": 1}, \"z\"]))\n", "TypeError: sequence item 0: expected str instance, dict found"},
		{"a singleton slot", "print(\"-\".join([None, \"z\"]))\n", "TypeError: sequence item 0: expected str instance, NoneType found"},
		{"a verdict slot", "print(\"-\".join([True, \"z\"]))\n", "TypeError: sequence item 0: expected str instance, bool found"},
		{"a double slot", "print(\"-\".join([1.5, \"z\"]))\n", "TypeError: sequence item 0: expected str instance, float found"},
		{"a list slot", "print(\"-\".join([[1], \"z\"]))\n", "TypeError: sequence item 0: expected str instance, list found"},
	}
}

// joinReferenceTrap asks the REFERENCE to run a program the reference itself raises over, and hands back what
// it wrote to stderr whatever its exit code. `cpythonOut` fatals on a non-zero exit — which is right for the
// answered family and wrong here, where the raise IS the answer: the sentence a trap carries is pinned from the
// reference's own words, not from this backend's dialect of them (ADR 0302's reference leg).
func joinReferenceTrap(t *testing.T, path string) (stderr string, ok bool) {
	t.Helper()
	py := oracleBinaryName()
	if _, err := exec.LookPath(py); err != nil {
		return "", false
	}
	cmd := exec.Command(py, path)
	cmd.Env = append(os.Environ(), "PYTHONHASHSEED=0")
	var errBuf bytes.Buffer
	cmd.Stdout = io.Discard
	cmd.Stderr = &errBuf
	// Any non-zero exit is the raise itself: `exec.LookPath` above already settled that the reference can be
	// spawned at all, and a program this table carries that the reference REFUSES to run is not a spawn failure
	// waiting to be mistaken for a trap.
	_ = cmd.Run()
	return errBuf.String(), true
}

// TestTheJoinSeamFamilyRaisesLikeTheReference asks the raise family of the linked binary AND of CPython, so
// the sentence cannot quietly become this backend's own dialect.
func TestTheJoinSeamFamilyRaisesLikeTheReference(t *testing.T) {
	t.Parallel()
	for _, tc := range joinCLITraps() {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			path := writeSrc(t, t.TempDir(), "join_seam_trap.gy", tc.src)
			out, ok := joinReferenceTrap(t, path)
			if !ok {
				t.Skip("no live python3 to ask")
			}
			if !strings.Contains(out, tc.want) {
				t.Fatalf("the reference does not raise %q (it says %q) — this table's expectation went stale\n%s", tc.want, out, tc.src)
			}
			for _, engine := range cliEngines {
				_, stderr, code := cliStreams(t, engine, path)
				if code == 0 || code == 2 {
					t.Fatalf("%s exited %d on a program the reference raises TypeError over (exit 2 is a compiler bug):\n%s\n%s", engine, code, stderr, tc.src)
				}
				if !strings.Contains(stderr, tc.want) {
					t.Fatalf("%s raises %q, want %q\n%s", engine, strings.TrimSpace(stderr), tc.want, tc.src)
				}
			}
		})
	}
}

// TestTheJoinSeamTrapIsCatchableAtTheCLI is the difference between a raise and a compiler error, asked of the
// linked program: the handler runs, the program goes on printing.
func TestTheJoinSeamTrapIsCatchableAtTheCLI(t *testing.T) {
	t.Parallel()
	src := "n = 3\nprint(1)\ntry:\n    print(\"-\".join([1, f\"a{n}\"]))\nexcept TypeError as e:\n    print(\"caught:\", e)\n"
	path := writeSrc(t, t.TempDir(), "join_seam_catch.gy", src)
	want, ok := cpythonOut(t, path)
	if !ok {
		t.Skip("no live python3 to ask")
	}
	for _, engine := range cliEngines {
		stdout, stderr, code := cliStreams(t, engine, path)
		if code != 0 {
			t.Fatalf("%s exited %d where the program handles its own TypeError: %s\n%s", engine, code, stderr, src)
		}
		if stdout != want {
			t.Fatalf("%s printed %q, CPython printed %q\n%s", engine, stdout, want, src)
		}
	}
	if !strings.Contains(want, "sequence item 0: expected str instance, int found") {
		t.Fatalf("the reference's own bytes do not carry the sentence this row raises with: %q", want)
	}
}

// joinSeamOwedCLI is the pair this row measured and does not own.
func joinSeamOwedCLI() []struct {
	name     string
	gap      string
	src      string
	want     string
	mustMach string
} {
	return []struct {
		name     string
		gap      string
		src      string
		want     string
		mustMach string
	}{
		{"the list the program built", "Gap R.404",
			"n = 3\nxs = [f\"a{n}\", \"z\"]\nprint(\"-\".join(xs))\n", "a3-z\n", "join() argument must be a constant list"},
		{"the separator the program built", "Gap R.405",
			"n = 3\ns = f\"-{n}\"\nprint(s.join([\"a\", \"z\"]))\n", "a-3z\n", "join on a receiver that is not a text the compiler can read"},
	}
}

// TestWhatTheJoinRoadStillOwesAtTheCLI pins the two refusals: the reference's bytes for the same source, this
// backend's sentence today, and the roadmap row that owns the fix. A row that stops being true FAILS, and the
// case is DELETED rather than edited.
func TestWhatTheJoinRoadStillOwesAtTheCLI(t *testing.T) {
	t.Parallel()
	for _, tc := range joinSeamOwedCLI() {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			path := writeSrc(t, t.TempDir(), "join_seam_owed.gy", tc.src)
			want, ok := cpythonOut(t, path)
			if !ok {
				t.Skip("no live python3 to ask")
			}
			if want != tc.want {
				t.Fatalf("the reference no longer prints %q (prints %q) — this row's premise went stale\n%s", tc.want, want, tc.src)
			}
			for _, engine := range cliEngines {
				stdout, stderr, code := cliStreams(t, engine, path)
				if code == 0 && stdout == want {
					t.Fatalf("%s closed %s: the program now prints the reference's bytes — delete this row and move it into the answered table\n%s", engine, tc.gap, tc.src)
				}
				if code == 2 {
					t.Fatalf("%s reached the assembler with a broken module (ADR 0166): %s\n%s", engine, stderr, tc.src)
				}
				if !strings.Contains(stderr, tc.mustMach) {
					t.Fatalf("%s: %s's refusal reads %q, which does not carry %q\n%s", engine, tc.gap, stderr, tc.mustMach, tc.src)
				}
			}
		})
	}
}

// TestTheJoinSeamAnswersWrongAtTheCLIFilesWhatTheCensusStillMisses is the reference leg of the one shape this
// family answers at exit 0 with the WRONG bytes: a body that hands back a NAME the pre-scan holds as text,
// beside an arm of another kind. The control row spells the same body with the proven case road, which a build
// of the parent commit `72d1755` prints identically — that is what files the defect against the census
// (**Gap R.406**, beside **Gap R.219** / **ADR 0368**) instead of against the seam. Exit 2 is never allowed;
// when the program starts printing the reference's bytes the row is DELETED, not edited.
func TestTheJoinSeamAnswersWrongAtTheCLIFilesWhatTheCensusStillMisses(t *testing.T) {
	t.Parallel()
	rows := []struct {
		name     string
		src      string
		python   string
		compiled string
	}{
		{
			name:     "the seam bound in a frame, beside a number arm",
			src:      "def f(k):\n    if k:\n        s = \"-\".join([\"a\", \"b\"])\n        return s\n    return 7\n\nprint(f(True))\nprint(f(False))\n",
			python:   "a-b\n7\n",
			compiled: "a-b\n(null)\n",
		},
		{
			name:     "the same body spelled with the case road",
			src:      "def f(k):\n    if k:\n        s = \"ab\".upper()\n        return s\n    return 7\n\nprint(f(True))\nprint(f(False))\n",
			python:   "AB\n7\n",
			compiled: "AB\n(null)\n",
		},
	}
	for _, r := range rows {
		r := r
		t.Run(r.name, func(t *testing.T) {
			t.Parallel()
			path := writeSrc(t, t.TempDir(), "join_seam_wrong.gy", r.src)
			want, ok := cpythonOut(t, path)
			if !ok {
				t.Skip("no live python3 to ask")
			}
			if want != r.python {
				t.Fatalf("the reference no longer prints %q (prints %q) — this row's premise went stale\n%s", r.python, want, r.src)
			}
			for _, engine := range cliEngines {
				stdout, stderr, code := cliStreams(t, engine, path)
				if code == 2 {
					t.Fatalf("%s reached the assembler with a broken module (ADR 0166): %s\n%s", engine, stderr, r.src)
				}
				if stdout == want && code == 0 {
					t.Fatalf("Gap R.406 closed on %s: the program prints %q — delete this row and move the program into the "+
						"answered table\n%s", engine, want, r.src)
				}
				if stdout != r.compiled {
					t.Fatalf("%s: the wrong answer changed shape — prints %q, this row pins %q (the reference prints %q)\n%s",
						engine, stdout, r.compiled, want, r.src)
				}
			}
		})
	}
}

// TestTheJoinSeamRefusalsAreMachineReadable is the agentic face of the refusals this row leaves standing: a
// program the compiled backend declines exits **1** with a `capability` member naming a code, its suggestion and
// its roadmap rows — never a bare sentence an agent has to pattern-match by hand. The third row is the slot whose
// kind no road can name (**Gap R.400** / **L11.1**), where the reference raises; the compiler declines rather than
// intern a repr it cannot justify, and says so with a code.
func TestTheJoinSeamRefusalsAreMachineReadable(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct{ name, src, code string }{
		{"the list the program built", "n = 3\nxs = [f\"a{n}\", \"z\"]\nprint(\"-\".join(xs))\n", "lower.unsupported.join-needs-constant"},
		{"a slot whose kind no road can name", "def g(v):\n    return v\n\nprint(\"-\".join([g(1), \"z\"]))\n", "lower.unsupported.join-element-kind-unknown"},
	} {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			path := writeSrc(t, t.TempDir(), "join_seam_json.gy", tc.src)
			out, code := cliRunCode(t, "--json", path)
			if code != 1 {
				t.Fatalf("a refused program exited %d, want 1 (ADR 0006): %s", code, out)
			}
			for _, frag := range []string{`"ok":false`, `"exit":1`, `"phase":"codegen"`, `"code":"` + tc.code + `"`, `"suggestion":`, `"rows":`} {
				if !strings.Contains(out, frag) {
					t.Fatalf("the JSON refusal carries no %s:\n%s", frag, out)
				}
			}
			if strings.Contains(out, "%!s(") || strings.Contains(out, "*lang.") {
				t.Fatalf("the JSON refusal prints the compiler's own types at the agent:\n%s", out)
			}
		})
	}
}

// TestTheJoinSeamFamilyIsOnTheRecord is ADR 0302's rule at this altitude: every program this row answers has
// an answer on record, written from the reference by tools/recmerge and cross-checked against the compiled
// leg — never written by this backend.
func TestTheJoinSeamFamilyIsOnTheRecord(t *testing.T) {
	t.Parallel()
	onRecord := 0
	for _, tc := range joinCLIAnswered() {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			if !lang.HasGoldenAnswer(tc.src) {
				t.Fatalf("%q is not on the record — record it from the reference with tools/recmerge, never from this backend", tc.src)
			}
			onRecord++
		})
	}
	if onRecord == 0 {
		t.Fatal("not one join-seam program is on the record: the record leg is not being walked")
	}
}
