// Package integration exercises the full pipeline (lex -> parse -> semantic
// -> codegen). This file drives the standalone type-check path
// (`lang.CheckSource`, the `gusty check` mode) for the match-exhaustiveness
// and definite-assignment analysis introduced with the L6.1/L6.2 semantic
// work (Gap B, roadmap).
//
// Rules exercised here:
//   - A `match` with no irrefutable case (no `case _:` and no bare-name
//     binding case) is NON-exhaustive: the semantic pass emits a mypy-style
//     *warning* (exit 0), and the machine (JSON) path carries it.
//   - A `match` with a wildcard `case _:` or an always-matching bare-name
//     binding (`case y:`) is exhaustive: no warning.
//   - Definite assignment: a name bound by an irrefutable case in EVERY
//     branch of a match is usable after the match (no `undefined name`
//     error); a name bound in only some branches is NOT definitely assigned
//     and is reported undefined when read after the match.
package integration

import (
	"strings"
	"testing"

	"github.com/donutloop/gusty/pkg/lang"
)

// hasWarning reports whether any warning diagnostic mentions substr.
func hasWarning(t *testing.T, res *lang.CheckResult, substr string) bool {
	t.Helper()
	for _, d := range res.Diagnostics {
		if d.Level == lang.LevelWarning && strings.Contains(d.Msg, substr) {
			return true
		}
	}
	return false
}

// hasError reports whether any error diagnostic mentions substr.
func hasError(t *testing.T, res *lang.CheckResult, substr string) bool {
	t.Helper()
	for _, d := range res.Diagnostics {
		if d.Level == lang.LevelError && strings.Contains(d.Msg, substr) {
			return true
		}
	}
	return false
}

func TestCheckNonExhaustiveMatchWarns(t *testing.T) {
	// Two literal cases and no wildcard / bare-name case: the match cannot
	// cover all possible subjects, so the checker warns (mypy-style).
	src := `def f(x):
    match x:
        case 1:
            print(1)
        case 2:
            print(2)
    return 0
`
	res, err := lang.CheckSource(src)
	if err != nil {
		t.Fatalf("CheckSource: %v", err)
	}
	if res.Exit != 0 {
		t.Fatalf("a warning-only check should exit 0, got %d", res.Exit)
	}
	if !hasWarning(t, res, "not exhaustive") {
		t.Fatalf("expected a non-exhaustive match warning, diags=%v", res.Diagnostics)
	}
}

func TestCheckExhaustiveWildcardNoWarning(t *testing.T) {
	// `case _:` is a wildcard that matches any subject: exhaustive.
	src := `def f(x):
    match x:
        case 1:
            print(1)
        case _:
            print(0)
    return 0
`
	res, err := lang.CheckSource(src)
	if err != nil {
		t.Fatalf("CheckSource: %v", err)
	}
	if hasWarning(t, res, "not exhaustive") {
		t.Fatalf("wildcard match should be exhaustive, diags=%v", res.Diagnostics)
	}
}

func TestCheckExhaustiveBareNameNoWarning(t *testing.T) {
	// `case y:` is an always-matching bare-name binding: exhaustive.
	src := `def f(x):
    match x:
        case 1:
            print(1)
        case y:
            print(y)
    return 0
`
	res, err := lang.CheckSource(src)
	if err != nil {
		t.Fatalf("CheckSource: %v", err)
	}
	if hasWarning(t, res, "not exhaustive") {
		t.Fatalf("bare-name case should be exhaustive, diags=%v", res.Diagnostics)
	}
}

func TestCheckDefiniteAssignmentEveryBranch(t *testing.T) {
	// `y` is bound by the irrefutable `case y:` on every path, so reading it
	// after the match is definitely-assigned: no `undefined name` error.
	src := `def f(x):
    match x:
        case y:
            pass
    return y
`
	res, err := lang.CheckSource(src)
	if err != nil {
		t.Fatalf("CheckSource: %v", err)
	}
	if hasError(t, res, "undefined name") {
		t.Fatalf("y is definitely assigned after the match, diags=%v", res.Diagnostics)
	}
}

func TestCheckNoDefiniteAssignmentPartialBranch(t *testing.T) {
	// `y` is only bound by `case y:`, which fires only when x does not match
	// the earlier literal case; on the literal path y is unbound, so reading
	// it after the match IS undefined.
	src := `def f(x):
    match x:
        case 1:
            pass
        case y:
            pass
    return y
`
	res, err := lang.CheckSource(src)
	if err != nil {
		t.Fatalf("CheckSource: %v", err)
	}
	if !hasError(t, res, "undefined name") {
		t.Fatalf("y is not definitely assigned on every path, diags=%v", res.Diagnostics)
	}
}

func TestCheckMatchRuntimeParity(t *testing.T) {
	// The exhaustive-match path must still lower and run: a wildcard case
	// executes the fallback body, so the compiled program prints 9 for x=7.
	assertOutput(t, "x = 7\nmatch x:\n    case 1:\n        print(1)\n    case _:\n        print(9)\n", "9\n")
}
