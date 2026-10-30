package integration

// integration/leading_dot_float_test.go — `print(.5)` answers `0.5` at the CLI, against a live reference
// (roadmap **Gap R.245**, **ADR 0464**).
//
// The refusal that filed the row was one character wide and looked like a syntax error in the user's program:
//
//	$ gustyc --eval 'print(.5)'
//	gustyc: parse error at 1:7: unexpected token
//	$ python3 -c 'print(.5)'
//	0.5
//
// `lexNumber` had cases for a digit and for the base prefixes and none for a leading dot. The row's own
// `Next concrete action` names the trap in the obvious fix — a `.` is also the MEMBER operator, so the decision
// belongs to the previous token, "exactly as `minusBeginsLiteral` decides for `-`" (**ADR 0338**) — and the two
// refused rows below (`obj.5`, `1 .5`) are that trap pinned from the CLI: they are syntax errors in CPython and
// must stay front-end errors here, never answers and never **ADR 0166**'s exit 2.
//
// Every `want` here is what a live `python3` printed for the same bytes inside the case; the tables that hard-code
// an answer (`TestWhatALeadingDotFloatRefusesWhatTheReferenceRefuses`) assert the REFUSAL on both sides, so the
// row cannot be closed by a toolchain that accepts what the reference rejects.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// leadingDotAnsweredSources is the family at this altitude: the five shapes the row measured as refusals, plus
// the positions a literal has to reach once it exists.
func leadingDotAnsweredSources() []string {
	return []string{
		"print(.5)\n",
		"print(1+.5)\n",
		"print(-.5)\n",
		"print(.5+.5)\n",
		"x = .5\nprint(x)\n",
		"print([.5, 1.5])\n",
		"d = {1: .5}\nprint(d)\n",
		"print(round(.5))\n",
		"print(.5 > .4)\n",
		"print(str(.5))\n",
		"print(f\"{.5:.2f}\")\n",
		"print([x for x in [1, 2] if x > .5])\n",
		"d = {.5: \"half\"}\nprint(d[.5])\n",
		"print(.5*.2)\n",
		"print(.5_1)\n",
		"if .5:\n    print(\"truthy\")\n\nelse:\n    print(\"falsy\")\n",
		// The spellings that already worked, pinned so an addition to the number grammar cannot become a
		// replacement of it.
		"print(0.5)\n",
		"print(1.5)\n",
		"print(1.)\n",
		"print(1_0.5)\n",
		"print(0x1f)\n",
		"print(0b101)\n",
		"print(1-1)\n",
		"y = 5-1\nprint(y)\n",
	}
}

// TestTheLeadingDotFloatAnswersWhatCPythonPrints is the reference leg at the CLI: the toolchain and a live
// `python3` are handed the same bytes, and the two must agree — including on the exit code, because a program
// the reference runs is not allowed to be a front-end error here (**Gap N**'s standing rule: the expectation is
// read off the reference inside the case, never off this file).
func TestTheLeadingDotFloatAnswersWhatCPythonPrints(t *testing.T) {
	t.Parallel()
	for _, src := range leadingDotAnsweredSources() {
		src := src
		t.Run(strings.ReplaceAll(strings.TrimRight(src, "\n"), "\n", "; "), func(t *testing.T) {
			t.Parallel()
			want := pythonOutput(t, src)
			stdout, stderr, code := cliStreams(t, "--eval", src)
			if code == 2 {
				t.Fatalf("exit 2 on a program the reference runs — the code the contract reserves for a compiler bug (ADR 0166)\n%s\n%s", stderr, stdout)
			}
			if code != 0 {
				t.Fatalf("the toolchain refused a program the reference runs (exit %d, it prints %q)\nstdout:%s\nstderr:%s", code, want, stdout, stderr)
			}
			// `--eval` echoes a snippet's final bare expression, which is the REPL courtesy, not the
			// program's stdout; every source here ends in a `print`, so the two are the same bytes.
			if got := strings.TrimRight(stdout, "\n"); got != strings.TrimRight(want, "\n") {
				t.Fatalf("compiled=%q reference=%q\n%s", got, want, src)
			}
		})
	}
}

// TestWhatALeadingDotFloatRefusesWhatTheReferenceRefuses is the gate's other side, and the row's real risk: a
// lexer that accepted a dot anywhere would answer `obj.5` and `1 .5`, because both are `.` followed by a digit.
// Each row asks the reference first, so the row cannot be closed by drifting the language.
func TestWhatALeadingDotFloatRefusesWhatTheReferenceRefuses(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name string
		src  string
		says string // what the toolchain's refusal must contain
	}{
		{
			name: "an attribute written with a number",
			src:  "obj = 1\nprint(obj.5)\n",
			says: "parse error",
		},
		{
			name: "a number, a space, and a number with no integer part",
			src:  "print(1 .5)\n",
			says: "parse error",
		},
		{
			name: "an underscore where the fraction's first digit belongs",
			src:  "print(._1)\n",
			says: "",
		},
	} {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if out := pythonOutput(t, tc.src); out != "" {
				t.Fatalf("the reference RUNS this program (it printed %q), so refusing it here is a language drift, not a fix:\n%s", out, tc.src)
			}
			stdout, stderr, code := cliStreams(t, "--eval", tc.src)
			if code == 2 {
				t.Fatalf("exit 2 on a program the reference refuses (ADR 0166's code)\n%s\n%s", stderr, stdout)
			}
			if code != 1 {
				t.Fatalf("a program the reference rejects as a syntax error must be the front-end class (exit 1), got %d\nstdout:%s\nstderr:%s", code, stdout, stderr)
			}
			if tc.says != "" && !strings.Contains(stdout+stderr, tc.says) {
				t.Fatalf("the refusal must say %q, said:\n%s\n%s", tc.says, stdout, stderr)
			}
		})
	}
}

// TestALeadingDotFloatRunsFromAFileToo pins the shape that is the CLI's normal use: a program on disk, run by
// path, so the row is not a `--eval` special case.
func TestALeadingDotFloatRunsFromAFileToo(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	src := "half = .5\nprint(half, half + .25, [half], {half: .25})\n"
	prog := filepath.Join(dir, "leading_dot.gy")
	if err := os.WriteFile(prog, []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	want := strings.TrimRight(pythonOutput(t, src), "\n")
	stdout, stderr, code := cliStreams(t, "--file", prog)
	if code != 0 {
		t.Fatalf("exit %d, the reference exits 0 printing %q\nstdout:%s\nstderr:%s", code, want, stdout, stderr)
	}
	if got := strings.TrimRight(stdout, "\n"); got != want {
		t.Fatalf("compiled=%q reference=%q", got, want)
	}
}

// TestTheExponentMarkerIsRefusedWithItsSiblings keeps this row inside its own edge: the toolchain refuses
// `1e5` and `1.5e2` (**Gap R.135**'s row owns the marker), so `.5e2` must be refused too. Accepting the leading-dot
// spelling of an exponent while refusing the ordinary one would ship a number grammar in two pieces.
func TestTheExponentMarkerIsRefusedWithItsSiblings(t *testing.T) {
	t.Parallel()
	for _, src := range []string{"print(.5e2)\n", "print(1e5)\n", "print(1.5e2)\n"} {
		src := src
		t.Run(strings.TrimRight(src, "\n"), func(t *testing.T) {
			t.Parallel()
			stdout, stderr, code := cliStreams(t, "--eval", src)
			if code == 2 {
				t.Fatalf("exit 2 on a program the front end should refuse (ADR 0166)\n%s\n%s", stderr, stdout)
			}
			if code == 0 {
				t.Fatalf("%s ran and printed %q — the exponent belongs to Gap R.135 and must be refused WITH `1e5`\n%s", src, stdout, stderr)
			}
		})
	}
}

// TestTheCorpusProgramOfTheLeadingDotFloatAgreesWithTheReference runs the conformance program itself, so the
// `match` row in the matrix is not the only thing that ever executes it.
func TestTheCorpusProgramOfTheLeadingDotFloatAgreesWithTheReference(t *testing.T) {
	t.Parallel()
	prog := "programs/leading_dot_float.gy"
	stdout, stderr, code := cliStreams(t, "--file", prog)
	if code != 0 {
		t.Fatalf("the corpus program exits %d\nstdout:%s\nstderr:%s", code, stdout, stderr)
	}
	if !strings.Contains(stdout, "0.5") {
		t.Fatalf("the corpus program never printed the float this row owns:\n%s", stdout)
	}
	if strings.Contains(stdout, "parse error") || strings.Contains(stderr, "parse error") {
		t.Fatalf("the corpus program still reports a parse error:\n%s\n%s", stdout, stderr)
	}
}
