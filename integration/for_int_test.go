package integration

import (
	"strings"
	"testing"
	"time"

	"github.com/donutloop/gusty/pkg/lang"
)

// `for x in <integer>` is a repeat count in this language: the compiled path bind 0, 1, … n-1, and CPython
// refuses the construct outright. Two engines agreeing makes it a feature; the missing piece was the
// declaration — a document, a ledger row that excludes the oracle, and tests that fail if it moves
// (roadmap Gap R.14, ADR 0207).

func TestIntegerRepeatCountAgreesOnEveryPath(t *testing.T) {
	src := readProgramSrc("for_int_count")
	want := "0\n1\n2\n3\n0\n10\n20\n100\n101\n102\n10\ndone\n"

	lang.RecordedStdoutIs(t, src, want)
	built, err := runAOTWithTimeout(t, src, 120*time.Second)
	if err != nil {
		t.Fatalf("compiled run: %v", err)
	}
	if built != want {
		t.Errorf("compiled output =\n%q\nwant\n%q", built, want)
	}
	// The oracle leg, stated as a fact rather than a skip: CPython stops at the first loop.
	pyOut, pyErr, perr := lang.PythonRun(src)
	if perr == nil && strings.Contains(pyOut, "0") {
		t.Errorf("CPython ran the integer-repeat program; the ledger row says it cannot:\n%s", pyOut)
	}
	if perr != nil && !strings.Contains(pyErr, "not iterable") {
		t.Logf("oracle failed with an unexpected message: %s", pyErr)
	}
}

// TestIntegerRepeatCountBoundaries pins the edges the documentation now promises: an expression count,
// a count of zero, and a negative count, which behave the way an empty range does.
func TestIntegerRepeatCountBoundaries(t *testing.T) {
	src := `n = 3
for j in n:
    print(j * 10)

for k in 0:
    print("never zero")

for m in -2:
    print("never negative")

print("boundary done")
`
	want := "0\n10\n20\nboundary done\n"
	lang.RecordedStdoutIs(t, src, want)
	built, err := runAOTWithTimeout(t, src, 90*time.Second)
	if err != nil {
		t.Fatalf("compiled run: %v", err)
	}
	if built != want {
		t.Errorf("compiled output =\n%q\nwant\n%q", built, want)
	}
}
