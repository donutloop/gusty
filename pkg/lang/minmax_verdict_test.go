package lang

import (
	"os"
	"strings"
	"testing"
)

// The operator that *chooses* an operand — `min` / `max` — has to say what the chosen candidate is,
// not merely what its number was (roadmap Gap R.117, ADR 0261).
//
// ADR 0256 already made a fold return the candidate it chose rather than the comparison that found it,
// and ADR 0257/0259 made a verdict printable as a value and as a container slot. What was left is the
// seam between them: the fold compared two values, kept one, and the print site was then asked what
// kind to render — with the expression that decided the answer sitting on the far side of the
// comparison. So `print(max([True, 0]))` printed `1` compiled while CPython and the record printed
// `True`, and the compiled answer was *plausible*: a dict-shaped, exit-0, number-looking wrong answer.
//
// The rule the rows below keep, all taken from `python3` on the same source:
//   - the call is a verdict exactly when the candidate the comparison chose is one;
//   - the comparison is strict, so a tie keeps the first candidate — `max([True, 1])` is `True` and
//     `max([1, True])` is `1`, which no “are the elements bools?“ question can answer;
//   - the number is still underneath: `max([True, 0]) + 1` is `2` and `max([True, 0]) == 1` is True,
//     because a verdict is the number every numeric path reads (ADR 0259).
func TestTheVerdictAFoldChoosesPrintsItsName(t *testing.T) {
	for _, tc := range []struct{ src, want string }{
		// The two lines Gap R.117 was filed with.
		{"print(max([True, 0]))\n", "True\n"},
		{"print(min([False, 1]))\n", "False\n"},
		// The tie, both ways round: the strict comparison keeps the first candidate.
		{"print(max([True, 1]))\n", "True\n"},
		{"print(max([1, True]))\n", "1\n"},
		{"print(min([False, 0]))\n", "False\n"},
		{"print(min([0, False]))\n", "0\n"},
		{"print(max([0, 0, True]))\n", "True\n"},
		{"print(min([False, True, 0]))\n", "False\n"},
		// The varargs spelling, and a float that had been truncated by the fold declining to see a
		// verdict as the number it compares to.
		{"print(max(True, 0))\n", "True\n"},
		{"print(min(False, 1))\n", "False\n"},
		{"print(max(0, True))\n", "True\n"},
		{"print(max(1.5, True))\n", "1.5\n"},
		{"print(max([True, 1.5]))\n", "1.5\n"},
		{"print(max([0.0, True]))\n", "True\n"},
		// The same value through every renderer the language has: one rule, four doors.
		{"print(str(max([True, 0])))\n", "True\n"},
		{"print(repr(min([False, 1])))\n", "False\n"},
		{"print(f\"{max([True, 0])}\")\n", "True\n"},
		{"print([max([True, 0])])\n", "[True]\n"},
		{"d = {\"k\": max([True, 0])}\nprint(d)\n", "{'k': True}\n"},
		{"ys = [max([True, 0])]\nprint(ys, len(ys))\n", "[True] 1\n"},
		// A slot the source wrote is a candidate the compiler can read.
		{"xs = [0]\nprint(max([True, xs[0]]))\n", "True\n"},
		{"xs = [0, 1]\nprint(max([True, xs[0]]), max([False, xs[1]]))\n", "True 1\n"},
		// …and the number is still there for every question a number answers.
		{"print(max([True, 0]) + 1, max([True, 0]) * 2, max([True, 0]) == 1)\n", "2 2 True\n"},
		{"print(sum([max([True, 0]), 1]))\n", "2\n"},
		{"print(max([True, 2]))\n", "2\n"},
		{"print(max([True, 0]) in [True])\n", "True\n"},
	} {
		name := strings.ReplaceAll(strings.Split(tc.src, "\n")[0], " ", "_")
		if got := captureStdout(t, tc.src); got != tc.want {
			t.Errorf("interpreter %s = %q, want %q", name, got, tc.want)
		}
		res, err := Compile(tc.src)
		if err != nil {
			t.Errorf("compiled %s refused: %v", name, err)
			continue
		}
		if out := runIR(t, res.IR); out != tc.want {
			t.Errorf("compiled %s = %q, want %q", name, out, tc.want)
		}
	}
}

// The rows that must not move, pinned in the same file as the fix because the temptation this cycle
// creates is to make every chosen operand print like a verdict. CPython hands `and`/`or` back an
// operand too, and prints its number; a winner that arrived as a number prints as that number; a text
// candidate is ordered by its content and printed as text.
func TestAFoldStillPrintsWhatTheWinnerActuallyIs(t *testing.T) {
	for _, tc := range []struct{ src, want string }{
		{"ys = [True, 1]\nprint(ys[0] and ys[1])\n", "1\n"}, // CPython's own answer: the operand
		{"print(min([2.5, 1]))\n", "1\n"},                   // the int winner among doubles
		{"print(max([1, 2.5]))\n", "2.5\n"},
		{"print(min(1.0, 2), max(1, 2.5))\n", "1.0 2.5\n"},
		{"print(max([\"a\", \"b\"]))\n", "b\n"},
		{"print(min([0, 1]))\n", "0\n"},
		{"print(max([3]))\n", "3\n"},
		{"print(max(3))\n", "3\n"}, // gusty's declared scalar extension, answered by the compiled backend
	} {
		name := strings.ReplaceAll(strings.Split(tc.src, "\n")[0], " ", "_")
		if got := captureStdout(t, tc.src); got != tc.want {
			t.Errorf("interpreter %s = %q, want %q", name, got, tc.want)
		}
		res, err := Compile(tc.src)
		if err != nil {
			t.Errorf("compiled %s refused: %v", name, err)
			continue
		}
		if out := runIR(t, res.IR); out != tc.want {
			t.Errorf("compiled %s = %q, want %q", name, out, tc.want)
		}
	}
}

// The shape this cycle measures and does not fix, pinned rather than skipped: a candidate the source
// does not write as a number — a name the compiler has not folded — leaves the compiled renderer with
// no winner to ask, and it prints the number underneath. the record, which really compares and
// really keeps the winning object, prints CPython's line. Refusing would be a defensible answer;
// printing `1` is not, which is why this is a row and not a skip (roadmap Gap R.124).
func TestACandidateTheCompilerCannotReadStillPrintsTheNumber(t *testing.T) {
	const src = "i = 0\nprint(max([True, i]))\n"
	if got, want := captureStdout(t, src), "True\n"; got != want {
		t.Errorf("the record leg = %q, want CPython's %q", got, want)
	}
	res, err := Compile(src)
	if err != nil {
		t.Fatalf("the compiled backend refused a program the oracle answers: %v", err)
	}
	const recorded = "1\n" // the number, because no candidate names its kind to this backend
	if out := runIR(t, res.IR); out != recorded {
		t.Errorf("compiled = %q, want the recorded answer %q (Gap R.124 — a fix has to move this row and the ledger with it)", out, recorded)
	}
}

// The tripwire: the fold and the verdict question choose the same candidate because they ask one
// function to do it. Two transcribed comparisons is exactly how this family keeps producing rows —
// `taggableMixedList`/`Set`/`Dict` (ADR 0259) and the three dict builders (ADR 0260) both started the
// same way — so the shape is asserted at the source, not only in the behaviour rows above.
func TestTheFoldAndTheVerdictQuestionChooseFromOneDoor(t *testing.T) {
	codegen, err := os.ReadFile("codegen.go")
	if err != nil {
		t.Fatalf("read codegen.go: %v", err)
	}
	pred, err := os.ReadFile("boolvalue.go")
	if err != nil {
		t.Fatalf("read boolvalue.go: %v", err)
	}
	source, predicate := string(codegen), string(pred)
	for _, want := range []string{
		"minMaxCandidateValue(el, g.foldableCandidate)", // the fold asks the shared candidate rule
		"numericWinner(vals, wantMin)",                  // …and the shared comparison
		"NumericCandidate: g.foldableCandidate",         // …the same one the print path asks
	} {
		if !strings.Contains(source, want) {
			t.Errorf("codegen.go stopped walking the shared candidate rule (%s is missing)", want)
		}
	}
	for _, want := range []string{
		"func numericWinner(vals []float64, wantMin bool) int",
		"func minMaxCandidateValue(e Expr, fold func(Expr) (float64, bool, bool)) (float64, bool, bool)",
		"return env.of(cands[numericWinner(vals, wantMin)], depth)",
	} {
		if !strings.Contains(predicate, want) {
			t.Errorf("boolvalue.go lost the one door the chosen-candidate question lives in (%s)", want)
		}
	}
	// A second comparison written down beside the first is the drift this prevents.
	for _, forbidden := range []string{"vals[i] < vals[best]", "vals[i] > vals[best]"} {
		if n := strings.Count(source, forbidden); n != 0 {
			t.Errorf("codegen.go compares min/max candidates %d time(s) by hand (%s); the winner is chosen in numericWinner", n, forbidden)
		}
	}
}
