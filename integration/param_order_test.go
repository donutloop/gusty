package integration

import (
	"testing"
	"time"

	"github.com/donutloop/gusty/pkg/lang"
)

// A defaulted parameter in the middle of a signature is a `SyntaxError` in CPython and ordinary
// source here, because this language's binding rules reach every parameter: positional binding fills
// left to right, a keyword call names what it fills (roadmap Gap R.11, ADR 0206). The gap was not a
// missing refusal — refusing this would forbid calls that work — it was that no test or document said
// which rule applies.

func TestDefaultedParameterInAnyPositionAgreesOnEveryPath(t *testing.T) {
	src := readProgramSrc("param_default_order")
	want := "6\n16\n6\n123\n923\n129\n9\n"

	lang.RecordedStdoutIs(t, src, want)
	built, err := runAOTWithTimeout(t, src, 120*time.Second)
	if err != nil {
		t.Fatalf("compiled run: %v", err)
	}
	if built != want {
		t.Errorf("compiled output =\n%q\nwant\n%q", built, want)
	}
	// The oracle leg is the deliberate part: CPython refuses the *definition*, so the ledger row
	// records it as not-applicable. If the oracle ever runs this program, the ledger's drift test
	// says so — this test only pins what this language owes its own programmers.
	if pyOut, pyErr, perr := lang.PythonRun(src); perr == nil && pyOut != "" {
		t.Errorf("CPython ran a program the ledger declares it cannot:\n%q\n%s", pyOut, pyErr)
	}
}

// TestBothBackendsBindTheSameParameters is the same claim stated where it matters most: a call that
// fills a mid-signature default by keyword has to bind identically in the interpreter and in
// machine code, or one of them is quietly wrong.
func TestBothBackendsBindTheSameParameters(t *testing.T) {
	src := `def offset(base, step=10, bonus):
    return base + step + bonus


print(offset(1, 2, 3))
print(offset(1, bonus=5))
print(offset(base=1, step=2, bonus=3))
`
	want := "6\n16\n6\n"
	lang.RecordedStdoutIs(t, src, want)
	built, err := runAOTWithTimeout(t, src, 90*time.Second)
	if err != nil {
		t.Fatalf("compiled run: %v", err)
	}
	if built != want {
		t.Errorf("compiled output =\n%q\nwant\n%q", built, want)
	}
}
