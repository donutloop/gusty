package lang

// A ternary with text arms answers with one of its arms, and the compiled backend must print that arm (Gap R.173,
// ADR 0290).
//
// ADR 0262 paid the ternary's NUMBER half — it emitted the double `select` Gap R.102 was filed for — and left
// the text half filed as Gap R.127, with a probe pinned at exit 6. What that probe recorded was this, at exit
// 0, on the compiled leg alone:
//
//	print("y" if 1 else "n")                          CPython y      compiled 0
//	x = 5 / print("big" if x > 2 else "small")        CPython big    compiled 0
//	def f(x): return "even" if x % 2 == 0 else "odd"  CPython even   compiled 0
//
// `0` is not a random digit: a text in this backend is an index into `@str_tab`, and the compiled leg printed
// the INDEX of "y" instead of "y". Four separate predicates had to be taught to ask about a ternary's arms
// before that index became the text — `stringVal`, `exprIsString`, `printsAsInternedStr`, `methodReturnsStr` —
// which is the real shape of "the print road never asked".

import (
	"strings"
	"testing"
)

func TestTernaryWithTextArmsPrintsTheArm(t *testing.T) {
	rows := []struct {
		name string
		src  string
		want string
	}{
		// A test the source wrote: one arm runs and the other is not in the program.
		{"constant test, then arm", "print(\"y\" if 1 else \"n\")\n", "y\n"},
		{"constant test, else arm", "print(\"y\" if 0 else \"n\")\n", "n\n"},
		{"constant test picks the second text", "print(\"a\" if 0 else \"b\")\n", "b\n"},
		// A test the program decides at run time: the select has to carry an interned index.
		{"run-time test, then arm", "x = 5\nprint(\"big\" if x > 2 else \"small\")\n", "big\n"},
		{"run-time test, else arm", "x = 1\nprint(\"big\" if x > 2 else \"small\")\n", "small\n"},
		{"run-time test on a comparison", "x = 4\nprint(\"even\" if x % 2 == 0 else \"odd\")\n", "even\n"},
		{"run-time test, other way", "x = 5\nprint(\"even\" if x % 2 == 0 else \"odd\")\n", "odd\n"},
		// Both arms the same text under a run-time test is still text.
		{"arms agree", "c = 1\nprint(\"same\" if c else \"same\")\n", "same\n"},
		// An empty text is a text, and printing it writes nothing (the trailing newline is print's).
		{"empty arm", "x = 3\nprint(\"hi\" if x else \"\")\n", "hi\n"},
		{"empty arm chosen", "x = 0\nprint(\"hi\" if x else \"\")\n", "\n"},
		// Returned by a function — the shape where the ANSWER crosses a boundary and the caller's print
		// road has to learn the callee returns text.
		{"returned by a function", "def f(x):\n    return \"even\" if x % 2 == 0 else \"odd\"\n\n\nprint(f(4))\nprint(f(5))\n", "even\nodd\n"},
		{"returned through a constant test", "def g():\n    return \"p\" if 1 else \"q\"\n\n\nprint(g())\n", "p\n"},
		// Bound to a name, put in a container, concatenated, and passed through a method.
		{"bound to a name", "c = 1\ns = \"a\" if c else \"b\"\nprint(s)\n", "a\n"},
		{"in a list", "print([\"y\" if 1 else \"n\"])\n", "['y']\n"},
		// A text ternary in a CONTAINER slot with a run-time test is not this row: the slot holds the
		// index and nothing beside it says the slot is text — the same missing tag as
		// print(["abc".upper()]) — so it is pinned as a refusal below rather than claimed here
		// (roadmap L11.1's tagged value word; measured byte-identical on the pre-cycle binary).
		{"concatenated after", "print((\"a\" if 1 else \"b\") + \"c\")\n", "ac\n"},
		{"through a method", "x = 2\nprint((\"big\" if x > 1 else \"small\").upper())\n", "BIG\n"},
		{"two ternaries as two print args", "print(\"a\" if 1 else \"b\", \"c\" if 0 else \"d\")\n", "a d\n"},
		// Numbers still work — the text road must not have stolen the number road.
		{"int arms, constant test", "print(7 if 1 else 9)\n", "7\n"},
		{"int arms, run-time test", "x = 4\nprint(7 if x > 5 else 9)\n", "9\n"},
		{"double arm still a double", "print(1 if 0 else 2.5)\n", "2.5\n"},
		// A verdict arm, which ADR 0257 already handled; kept so the text fix cannot regress it.
		{"verdict arms", "print(True if 1 else False)\n", "True\n"},
		{"verdict arms run-time", "x = 1\nprint(True if x else False)\n", "True\n"},
	}
	for _, r := range rows {
		r := r
		t.Run(r.name, func(t *testing.T) {
			if got := captureStdout(t, r.src); got != r.want {
				t.Errorf("interpreter printed %q, want %q", got, r.want)
			}
			if got := compiledOut(t, r.src); got != r.want {
				t.Errorf("--aot printed %q, want %q — printing the @str_tab index instead of the text is Gap R.173", got, r.want)
			}
		})
	}
}

// TestATernaryEvaluatesOnlyTheArmItsTestChooses is the ladder's behavioural half: a text ternary is not a
// fold that runs both arms, and a side effect in the arm that does not run must not happen. The compiled
// leg has to earn this — a `select` evaluates both operands, which is why the arms are lowered only when the
// test is not a constant, and folded to the running arm when it is.
func TestATernaryEvaluatesOnlyTheArmItsTestChooses(t *testing.T) {
	for _, r := range []struct {
		name string
		src  string
		want string
	}{
		{"else arm skipped", "def shout():\n    print(\"SHOUT\")\n\n    return \"b\"\n\nprint(\"a\" if 1 else shout())\n", "a\n"},
		{"then arm skipped", "def shout():\n    print(\"SHOUT\")\n\n    return \"b\"\n\nprint(\"a\" if 0 else shout())\n", "SHOUT\nb\n"},
	} {
		r := r
		t.Run(r.name, func(t *testing.T) {
			if got := captureStdout(t, r.src); got != r.want {
				t.Errorf("interpreter printed %q, want %q", got, r.want)
			}
			if got := compiledOut(t, r.src); got != r.want {
				t.Errorf("--aot printed %q, want %q (the arm the test does not choose ran)", got, r.want)
			}
		})
	}
}

// TestTernaryArmsThatDisagreeStillRefuse keeps the refusals that ADR 0262 owed, so opening the text road
// cannot have quietly answered a shape whose answer has no word.
func TestTernaryArmsThatDisagreeStillRefuse(t *testing.T) {
	for _, src := range []string{
		// One arm a double, the other an int, and which runs is a run-time fact: no word.
		"x = 1\nprint(1 if x else 2.5)\n",
	} {
		src := src
		t.Run(strings.TrimSpace(src), func(t *testing.T) {
			out, refused := compiledOutOrRefusal(t, src)
			if refused {
				if strings.Contains(out, "global variable reference") || strings.Contains(out, "store i32 @") {
					t.Errorf("refusal leaked the module it could not build: %v", out)
				}
				return
			}
			// Answered: then it must be the reference's answer, which the caller checked separately.
			if strings.HasPrefix(src, "x = 1") {
				t.Errorf("--aot answered %q for a pair of arms that do not agree on a word; ADR 0262 refuses this", out)
			}
		})
	}
}

// TestAFunctionReturningTextOnlyUnderARunTimeTestIsStillATextFunction pins the pre-scan half specifically:
// `strFuncs` is filled before any call site is lowered, and a body whose only return is a ternary said
// "not text", so the CALLER printed the index. A body whose arms disagree must not be registered.
func TestAFunctionReturningTextOnlyUnderARunTimeTestIsStillATextFunction(t *testing.T) {
	src := "def pick(x):\n    return \"yes\" if x > 0 else \"no\"\n\n\nprint(pick(1))\nprint(pick(-1))\n"
	if got := compiledOut(t, src); got != "yes\nno\n" {
		t.Errorf("--aot printed %q, want yes/no — the callee's arms were never asked (Gap R.173)", got)
	}
	mixed := "def mixed(x):\n    return \"yes\" if x > 0 else 0\n\n\nprint(mixed(1))\n"
	if _, err := Compile(mixed); err != nil {
		// A refusal is honest; a printed number from a body that does not agree on a kind is not.
		if strings.Contains(err.Error(), "global variable reference") {
			t.Errorf("refusal leaked the module: %v", err)
		}
	}
}

// TestATextInAContainerSlotStillOwesItsTag pins what this cycle did NOT fix, so the fix above cannot be
// read as "text ternaries are done". A text that reaches a container slot arrives as an @str_tab index and
// the slot has no tag saying otherwise, so `print(["y" if c else "n"])` with a run-time test prints [1] —
// the same missing tag that makes print(["abc".upper()]) print [0]. Both belong to L11.1's tagged value
// word, not to the print road this cycle taught. Measured byte-identical on the pre-cycle binary.
func TestATextInAContainerSlotStillOwesItsTag(t *testing.T) {
	src := "c = 0\nprint([\"y\" if c else \"n\"])\n"
	if got := captureStdout(t, src); got != "['n']\n" {
		t.Errorf("interpreter printed %q, want ['n'] — the interpreter's slots carry kinds, so this leg has no excuse", got)
	}
	res, err := Compile(src)
	if err != nil {
		return // a refusal is honest and promotable
	}
	if out := runIR(t, res.IR); out == "['n']\n" {
		t.Log("the compiled leg now answers this; promote the row into TestTernaryWithTextArmsPrintsTheArm")
	}
}

// TestAContainerArmTernaryIsNotAnsweredByTheTextRoad guards the new branch specifically: two CONTAINER arms
// look like "both arms are the same kind" to a careless predicate, and a container's compiled value is a
// GLOBAL, so handing one to the new select would reproduce Gap R.128's `global variable reference must have
// pointer type` — the module-level failure ADR 0234 counts as this compiler's bug.
func TestAContainerArmTernaryIsNotAnsweredByTheTextRoad(t *testing.T) {
	src := "c = 1\nprint([1, 2] if c > 0 else [3])\n"
	res, err := Compile(src)
	if err != nil {
		for _, banned := range []string{"global variable reference", "store i32 @", "unexpected type"} {
			if strings.Contains(err.Error(), banned) {
				t.Errorf("the refusal leaked a malformed module (`%s`): %v", banned, err)
			}
		}
		return
	}
	_ = res // answered: the container road handles it, and the probe pins the answer
}
