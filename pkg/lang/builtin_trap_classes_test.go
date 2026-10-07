package lang

import (
	"strings"
	"testing"
)

// Gap R.25 (ADR 0214): a trap that carries a message but no exception class is invisible to the
// language — `except TypeError:` cannot match what has no class, and the traceback prints a bare
// sentence where an exception should be named. These shapes raised `*TrapError` with an empty
// ExnType; each now raises what CPython raises, in CPython's words.

type trapCase struct {
	name    string
	src     string
	class   string
	message string
}

var trapCases = []trapCase{
	{"missing attribute on an instance",
		"class P:\n    pass\np = P()\nprint(p.nope)\n",
		"AttributeError", "'P' object has no attribute 'nope'"},
	{"attribute through a missing attribute",
		"class P:\n    pass\np = P()\nprint(p.x.y)\n",
		"AttributeError", "'P' object has no attribute 'x'"},
	{"int() on a non-numeric string",
		"print(int(\"abc\"))\n",
		"ValueError", "invalid literal for int() with base 10: 'abc'"},
	{"float() on a non-numeric string",
		"print(float(\"zzz\"))\n",
		"ValueError", "could not convert string to float: 'zzz'"},
	{"unpacking too few values",
		"a, b = [1]\nprint(a)\n",
		"ValueError", "not enough values to unpack (expected 2, got 1)"},
	{"unpacking too many values",
		"a, b = [1, 2, 3]\nprint(a)\n",
		"ValueError", "too many values to unpack (expected 2)"},
	{"calling a value that is not callable",
		"x = 5\nx()\n",
		"TypeError", "'int' object is not callable"},
	{"len of a non-container",
		"print(len(5))\n",
		"TypeError", "object of type 'int' has no len()"},
	{"subscripting a non-container",
		"x = 5\nprint(x[0])\n",
		"TypeError", "'int' object is not subscriptable"},
}

// trapRun evaluates src and returns the typed error, failing if nothing was raised.
// trapRun runs a program that must fail and hands back the typed failure, so a case can ask which
// class escaped. The record is what says the program was owed a failure at all: a snippet that
// answers instead of trapping is a divergence, and a snippet the compiler cannot build is one too.
func trapRun(t *testing.T, src string) *TrapError {
	t.Helper()
	err := goldenRunError(t, src)
	if err == nil {
		t.Fatalf("the program raised nothing where the record says it traps")
	}
	ee, ok := err.(*TrapError)
	if !ok {
		t.Fatalf("want a *TrapError, got %T: %v", err, err)
	}
	return ee
}
func TestBuiltInTrapsCarryTheirClass(t *testing.T) {
	for _, tc := range trapCases {
		t.Run(tc.name, func(t *testing.T) {
			ee := trapRun(t, tc.src)
			// The class is the whole point: an empty ExnType is what made these
			// uncatchable, so it is the first thing asserted.
			if ee.ExnType != tc.class {
				t.Errorf("class = %q, want %q (msg %q)", ee.ExnType, tc.class, ee.ExnMsg)
			}
			if ee.ExnMsg != tc.message {
				t.Errorf("message = %q,\n         want %q", ee.ExnMsg, tc.message)
			}
		})
	}
}

// The user-visible form: the traceback names the class, so a report reads like a language event
// and greps like one. An untyped trap printed a bare sentence with no class to search for.
func TestTrapTracebacksNameTheirClass(t *testing.T) {
	for _, tc := range trapCases {
		t.Run(tc.name, func(t *testing.T) {
			ee := trapRun(t, tc.src)
			report := ee.ExnType + ": " + ee.ExnMsg
			if !strings.HasPrefix(report, tc.class+": ") {
				t.Errorf("report %q does not read as %q", report, tc.class+": ...")
			}
		})
	}
}

func TestBuiltInTrapsAreCatchableByTheirClass(t *testing.T) {
	for _, tc := range trapCases {
		t.Run(tc.name, func(t *testing.T) {
			src := "try:\n    " + strings.ReplaceAll(strings.TrimRight(tc.src, "\n"), "\n", "\n    ") +
				"\nexcept " + tc.class + ":\n    print(\"handled\")\n"
			out, evalErr := runGoldenStdout(t, src)
			if evalErr != nil {
				t.Fatalf("the %s handler did not run: %v (stdout %q)", tc.class, evalErr, out)
			}
			if out != "handled\n" {
				t.Errorf("stdout = %q, want %q", out, "handled\n")
			}
		})
	}
}

// A handler for the wrong class must not collect the exception — the other half of "the class
// means something".
func TestWrongClassDoesNotCatchATrap(t *testing.T) {
	src := "x = 5\ntry:\n    x()\nexcept ValueError:\n    print(\"wrong arm\")\n"
	ee := trapRun(t, src)
	if ee.ExnType != "TypeError" {
		t.Errorf("the TypeError was relabelled: %+v", ee)
	}
}
