package lang

// Text predicates print a verdict, not the word the fold holds (Gap R.172, ADR 0289).
//
// `s.startswith(p)`, `s.endswith(p)`, `isdigit`, `isalpha`, `isalnum`, `isspace`, `islower`, `isupper` are
// eight questions the language already asks as yes-or-no, and the compiled backend already answered each with the
// same 0/1 a comparison answers with. Only the print road never learned, because a method call's callee is
// not a `*Name` — `"abc".startswith("ab")` parses as `Call{Fn: Attr{Obj: "abc", Name: startswith}}` — and
// `callReturnsBool` bailed on the very first line for anything that was not a plain name. So the answer was
// right and its RENDERING was wrong on both legs at exit 0:
//
//	print("abc".startswith("ab"))   CPython True   the compiled backend 1
//	print("abc".isdigit())          CPython False  the compiled backend 0
//
// which is ADR 0257's bug, reported once for comparisons and silently still open for methods. The tests
// below pin it against `python3` for every method, in every position a verdict can sit in.

import (
	"strings"
	"testing"
)

// TestTextPredicatesPrintAVerdict is the table. Every row asks a question whose answer differs between the
// two arms, so a fold that stopped answering at all cannot hide behind the rows that pass.
func TestTextPredicatesPrintAVerdict(t *testing.T) {
	rows := []struct {
		name string
		src  string
		want string
	}{
		{"startswith true", "print(\"abc\".startswith(\"ab\"))\n", "True\n"},
		{"startswith false", "print(\"abc\".startswith(\"z\"))\n", "False\n"},
		{"endswith true", "print(\"abc\".endswith(\"bc\"))\n", "True\n"},
		{"endswith false", "print(\"abc\".endswith(\"ab\"))\n", "False\n"},
		{"isdigit true", "print(\"123\".isdigit())\n", "True\n"},
		{"isdigit false", "print(\"12a\".isdigit())\n", "False\n"},
		{"isdigit of empty", "print(\"\".isdigit())\n", "False\n"},
		{"isalpha true", "print(\"abc\".isalpha())\n", "True\n"},
		{"isalpha false", "print(\"ab1\".isalpha())\n", "False\n"},
		{"isalnum true", "print(\"abc123\".isalnum())\n", "True\n"},
		{"isalnum false", "print(\"abc!\".isalnum())\n", "False\n"},
		{"isspace true", "print(\"   \".isspace())\n", "True\n"},
		{"isspace false", "print(\" a \".isspace())\n", "False\n"},
		{"islower true", "print(\"abc\".islower())\n", "True\n"},
		{"islower false", "print(\"Abc\".islower())\n", "False\n"},
		{"isupper true", "print(\"ABC\".isupper())\n", "True\n"},
		{"isupper false", "print(\"abc\".isupper())\n", "False\n"},
		// A verdict mixed into a verdict-bearing expression: the print road asks the SAME question of
		// the whole, so a method that answered True while `and` printed 1/0 would be half a fix.
		{"verdict and verdict", "print(\"abc\".startswith(\"ab\") and \"abc\".endswith(\"bc\"))\n", "True\n"},
		{"verdict and false", "print(\"abc\".startswith(\"z\") and \"abc\".endswith(\"bc\"))\n", "False\n"},
		{"verdict or verdict", "print(\"abc\".startswith(\"z\") or \"abc\".endswith(\"bc\"))\n", "True\n"},
		{"not verdict", "print(not \"abc\".isdigit())\n", "True\n"},
		{"not verdict false", "print(not \"1\".isdigit())\n", "False\n"},
		// A verdict compared against a verdict is still a verdict (True == True), which is only true if
		// both sides print from the same predicate.
		{"verdict equals literal", "print(\"abc\".isdigit() == True)\n", "False\n"},
		{"verdict not equals literal", "print(\"abc\".isdigit() != True)\n", "True\n"},
		// Bound to a name: what the name HOLDS is a verdict, so the print road must read the record
		// rather than the word (ADR 0257's rule for `flag = 1 == 1`).
		{"bound true", "flag = \"abc\".startswith(\"ab\")\nprint(flag)\n", "True\n"},
		{"bound false", "flag = \"abc\".startswith(\"z\")\nprint(flag)\n", "False\n"},
		{"bound then rebound to a number", "flag = \"abc\".isdigit()\nflag = 5\nprint(flag)\n", "5\n"},
		// As a test, where a wrong answer would pick a branch.
		{"as a test, taken", "if \"abc\".startswith(\"ab\"):\n    print(\"yes\")\nelse:\n    print(\"no\")\n", "yes\n"},
		{"as a test, not taken", "if \"abc\".startswith(\"z\"):\n    print(\"yes\")\nelse:\n    print(\"no\")\n", "no\n"},
		// In a container slot, where there is no expression left to ask — the ADR 0259 case.
		{"in a list", "print([\"abc\".isdigit()])\n", "[False]\n"},
		{"in a list, true", "print([\"abc\".isalpha()])\n", "[True]\n"},
		// A verdict on a NAME whose value the compiler can see. (`def check(s): return s.isalpha()`
		// is the same program through a PARAMETER, and the compiled leg refuses that receiver —
		// pinned in TestThePredicateRefusalIsStillHonest rather than here.)
		{"verdict on a bound name", "s = \"abc\"\nprint(s.isalpha())\n", "True\n"},
		{"verdict on a bound name, false", "s = \"ab1\"\nprint(s.isalpha())\n", "False\n"},
	}
	for _, r := range rows {
		r := r
		t.Run(r.name, func(t *testing.T) {
			if got := captureStdout(t, r.src); got != r.want {
				t.Errorf("interpreter printed %q, want %q", got, r.want)
			}
			compiled := compiledOut(t, r.src)
			if compiled != r.want {
				t.Errorf("--aot printed %q, want %q", compiled, r.want)
			}
		})
	}
}

// TestAPredicateIsNotConfusedWithAValueMethod keeps the table honest the other way. `upper`, `strip` and
// `replace` answer a TEXT, and a text must never be handed to the bool printer: making the predicate
// program-wide rather than a list of the eight methods would print True for `"abc".upper()`.
func TestAPredicateIsNotConfusedWithAValueMethod(t *testing.T) {
	for _, r := range []struct {
		src  string
		want string
	}{
		{"print(\"abc\".upper())\n", "ABC\n"},
		{"print(\" ab \".strip())\n", "ab\n"},
		{"print(\"a-b\".replace(\"-\", \"+\"))\n", "a+b\n"},
		{"print(\"ab\".join([\"x\", \"y\"]))\n", "xaby\n"},
		{"print(\"banana\".count(\"a\"))\n", "3\n"},
		{"print(\"hello\".find(\"ll\"))\n", "2\n"},
	} {
		r := r
		t.Run(strings.TrimSpace(r.src), func(t *testing.T) {
			if got := captureStdout(t, r.src); got != r.want {
				t.Errorf("interpreter printed %q, want %q", got, r.want)
			}
			if got := compiledOut(t, r.src); got != r.want {
				t.Errorf("--aot printed %q, want %q (a value method rendered as a verdict is a new wrong answer)", got, r.want)
			}
		})
	}
}

// TestAPredicateOnAnInstanceIsNotAssumedAVerdict pins the reason the receiver is asked a question at all.
// A class the program defines can name any of these eight attributes itself; what its method returns is a
// fact about that class body, not about the table — the same dunder evidence that keeps `print(a < 4)`
// printing 1 for dunder.gy (ADR 0257).
func TestAPredicateOnAnInstanceIsNotAssumedAVerdict(t *testing.T) {
	src := "class Box:\n    def isdigit(self):\n        return 1\n\n\nprint(Box().isdigit())\n"
	// CPython prints 1: `Box().isdigit()` runs the CLASS's method, which returns the integer 1, and an
	// integer prints as a number. If the eight names were matched on the attribute alone, with no
	// question about the receiver, this row would print True.
	const want = "1\n"
	if got := captureStdout(t, src); got != want {
		t.Errorf("interpreter printed %q, want %q — an instance's own attribute answers for itself", got, want)
	}
}

// TestThePredicateRefusalIsStillHonest pins what the compiled leg declines rather than guesses. A receiver
// the compiler cannot see through — a parameter — has no compile-time text to fold, and the road refuses.
// That refusal is pre-existing and byte-identical on the pre-cycle binary; it is pinned so that this
// verdict-rendering change cannot have quietly turned it into a number, and so that it stays WORDS.
func TestThePredicateRefusalIsStillHonest(t *testing.T) {
	for _, src := range []string{
		"def check(s):\n    return s.startswith(\"x\")\n\n\nprint(check(\"xyz\"))\n",
		"def check(s):\n    return s.isdigit()\n\n\nprint(check(\"123\"))\n",
	} {
		src := src
		t.Run(strings.TrimSpace(src), func(t *testing.T) {
			out, refused := compiledOutOrRefusal(t, src)
			if !refused {
				return // answered: fine, an answer is never worse than a refusal
			}
			// The wording is "string-method ... on a receiver that is not a text the compiler can
			// read" (Gap R.174 renamed it, because calling a DICT a "non-constant string" was a lie
			// about the program); match the stem, not the whole phrase.
			if !strings.Contains(out, "string-method") && !strings.Contains(out, "string method") {
				t.Errorf("refusal %q does not name what the program asked for (Gap R.38)", out)
			}
		})
	}
}

// TestTheTenThousandthPredicateStillCounts is the ladder guard in the other direction: nothing about the
// print road may change how a verdict behaves as a NUMBER. `True + 1` is 2, `[True] == [1]` is True, and a
// predicate's 0/1 must keep feeding arithmetic exactly as before (ADR 0259's int/bool/float family).
func TestThePredicateAnswerStillCountsAsANumber(t *testing.T) {
	for _, r := range []struct {
		src  string
		want string
	}{
		{"print(\"1\".isdigit() + 1)\n", "2\n"},
		{"print(\"a\".isdigit() + 1)\n", "1\n"},
		{"print(\"a\".isalpha() * 3)\n", "3\n"},
		{"print(len([\"a\".isdigit(), \"b\".isdigit()]))\n", "2\n"},
	} {
		r := r
		t.Run(strings.TrimSpace(r.src), func(t *testing.T) {
			if got := captureStdout(t, r.src); got != r.want {
				t.Errorf("interpreter printed %q, want %q", got, r.want)
			}
			if got := compiledOut(t, r.src); got != r.want {
				t.Errorf("--aot printed %q, want %q", got, r.want)
			}
		})
	}
}
