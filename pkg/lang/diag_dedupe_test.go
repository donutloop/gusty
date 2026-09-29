package lang

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// A diagnostic is a fact about a position in the source. It is either true or not, and it cannot
// become more true by being said twice — yet the checker used to say some of them two or three
// times, because per-call-site return inference re-walks a callee's body for every call site
// (roadmap Gap R.7, ADR 0202). The scaling was observable: a function never called reported its
// body once, called once reported it twice, called twice reported it three times.

func diagTexts(diags []Diagnostic) []string {
	out := []string{}
	for _, d := range diags {
		out = append(out, d.Error())
	}
	return out
}

func countDiags(diags []Diagnostic, want string) int {
	n := 0
	for _, d := range diagTexts(diags) {
		if d == want {
			n++
		}
	}
	return n
}

func dedupeAnalyze(t *testing.T, src string) []Diagnostic {
	t.Helper()
	prog, err := Parse(src)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	return Analyze(prog)
}

// TestOneReportRegardlessOfCallSites is the measured scaling law, made an assertion.
func TestOneReportRegardlessOfCallSites(t *testing.T) {
	body := "def f(x):\n    match x:\n        case 1:\n            return \"one\"\n\n"
	calls := map[string]int{"never called": 0, "called once": 1, "called twice": 2, "called three times": 3}
	sources := map[string]string{}
	for name, n := range calls {
		src := body
		for i := 0; i < n; i++ {
			src += "print(f(1))\n"
		}
		sources[name] = src
	}
	for name, n := range calls {
		diags := dedupeAnalyze(t, sources[name])
		want := "warning at 2:5: match is not exhaustive: add a wildcard `_` or always-matching binding case"
		if got := countDiags(diags, want); got != 1 {
			t.Errorf("%s (%d call sites): the non-exhaustive match was reported %d times, want 1", name, n, got)
		}
	}
}

// TestDistinctFactsAtOnePositionAllSurvive guards the obvious over-correction: collapsing
// *identical* diagnostics must not collapse *different* ones that happen to share a line.
func TestDistinctFactsAtOnePositionAllSurvive(t *testing.T) {
	diags := dedupeAnalyze(t, "def take(n: int) -> int:\n    return n\n\nx = take(1) + \"text\"\ny = take(\"wrong\")\n")
	var mismatch, arithmetic bool
	for _, d := range diags {
		switch {
		case d.Level == LevelError && strings.Contains(d.Msg, "expected int, got str"):
			mismatch = true
		case d.Level == LevelWarning && strings.Contains(d.Msg, "arithmetic on non-numeric operands"):
			arithmetic = true
		}
	}
	if !mismatch || !arithmetic {
		t.Errorf("a distinct diagnostic went missing: %v", diagTexts(diags))
	}
}

// TestLevelsAreNotCollapsed: the same sentence said as an error and as a warning is two facts —
// one of them decides whether the program can run at all.
func TestLevelsAreNotCollapsed(t *testing.T) {
	// `x` is unbound (error) and the operand of the addition is therefore not numeric (warning),
	// both at the same place. The warning may be suppressed as derived, but if both are reported
	// neither may be silently deduplicated into the other.
	diags := dedupeAnalyze(t, "print(undefined_thing + 1)\n")
	seen := map[Level]int{}
	for _, d := range diags {
		seen[d.Level]++
	}
	if seen[LevelError] == 0 {
		t.Errorf("the unbound name was not reported as an error: %v", diagTexts(diags))
	}
}

// TestSameMessageDifferentPositionsBothSurvive: repetition is only collapsed for the *same*
// position, so a repeated mistake in two places is still two diagnostics — a count an agent can
// act on.
func TestSameMessageDifferentPositionsBothSurvive(t *testing.T) {
	diags := dedupeAnalyze(t, "def take(n: int) -> int:\n    return n\n\ntake(1)\ntake(\"a\")\ntake(\"b\")\n")
	n := 0
	for _, d := range diags {
		if d.Level == LevelError && strings.Contains(d.Msg, "expected int, got str") {
			n++
		}
	}
	if n != 2 {
		t.Errorf("two wrong calls on two lines should be two diagnostics, got %d: %v", n, diagTexts(diags))
	}
}

// TestWholeCorpusReportsNoDiagnosticTwice is the corpus-wide form of the invariant, checked the way
// it was measured: walk every program and assert none of them emits a repeated line.
func TestWholeCorpusReportsNoDiagnosticTwice(t *testing.T) {
	dir := filepath.Join("..", "..", "integration", "programs")
	matches, err := filepath.Glob(filepath.Join(dir, "*.gy"))
	if err != nil || len(matches) == 0 {
		t.Skipf("no corpus to check: %v", err)
	}
	for _, path := range matches {
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read %s: %v", path, err)
		}
		prog, perr := Parse(string(data))
		if perr != nil {
			continue // programs that do not parse are the parser's business, not the reporter's
		}
		seen := map[string]int{}
		for _, d := range prog.Diags {
			seen[d.Error()]++
		}
		for _, d := range Analyze(prog) {
			seen[d.Error()]++
		}
		for text, n := range seen {
			if n > 1 {
				t.Errorf("%s reported the same diagnostic %d times: %s", filepath.Base(path), n, text)
			}
		}
	}
}
