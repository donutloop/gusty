package lang

import (
	"strings"
	"testing"
)

// min_max_values_test.go — `min(a, b, ...)` and `max(a, b, ...)` choose a value, and the value's
// kind is the answer's kind (roadmap L11.6 / Gap R.73 / Gap R.104; ADR 0256).
//
// The family had three answers written by three unrelated rules. The interpreter refused every
// side-by-side spelling with `min/max expects 1 argument`. The compiled backend reached only for
// its float domain, where every candidate is promoted to a double and an int winner therefore came
// back `1.0`; int-only candidates refused outright. The oracle instead keeps the candidate it chose
// — not the comparison that found it — so the winner's own kind is the fact the whole feature has
// to preserve.
//
// The second rule is that candidates must *mean* something comparable. Text is an @str_tab index,
// None is an untagged 0, and a container is a handle; an `icmp` on those words orders the heap or
// the intern table and prints a plausible number. Here text candidates ask `rt_str_order` (ADR
// 0248), and a source-visible text/number/None/container collision raises the oracle's TypeError
// rather than answering by payload. The runtime half of that collision needs L11.1's tagged word
// and stays filed below.

func TestMinMaxTakeValuesSideBySideAndKeepTheWinnersKind(t *testing.T) {
	for _, tc := range []struct{ name, src, want string }{
		// ---- the row Gap R.104 was named for, now on both engines.
		{
			"the row the gap carried",
			"print(min(1.0, 2), max(1, 2.5))\n", "1.0 2.5\n",
		},
		{
			"an int winner beside a double stays an int",
			"print(min(2.5, 1), max(1, 2.5))\n", "1 2.5\n",
		},
		{
			"the same pair in the opposite source order",
			"print(min(1, 2.5), max(2.5, 1))\n", "1 2.5\n",
		},
		{
			"a tie whose first candidate is the int stays an int",
			"print(min(1, 1.0), max(1, 1.0))\n", "1 1\n",
		},
		{
			"all-int candidates, which used to have no compiled path at all",
			"print(min(1, 5), max(1, 5))\n", "1 5\n",
		},
		{
			"three int candidates",
			"print(min(2, 1, 3), max(2, 1, 3))\n", "1 3\n",
		},
		{
			"three candidates whose first int winner survives a later double",
			"print(min(1, 2.0, 3), max(1, 2.0, 3))\n", "1 3\n",
		},
		{
			"three float candidates",
			"print(min(1.5, 2.5, 0.25), max(1.5, 2.5, 0.25))\n", "0.25 2.5\n",
		},
		{
			"candidates that are expressions, not literals",
			"print(min(3 + 1, 2 * 2), max(3 + 1, 2 * 2))\n", "4 4\n",
		},
		{
			"settled int variables",
			"x = 2\ny = 3\nprint(min(x, y), max(x, y))\n", "2 3\n",
		},
		{
			"the result participates in arithmetic",
			"print(min(1, 5) + max(1, 5), max(2.5, 1) * 2)\n", "6 5.0\n",
		},
		{
			"the result participates in a condition",
			"print(1 if min(1, 2) == 1 else 0)\n", "1\n",
		},
		{
			"the result is an element of a list",
			"print([min(1, 2), max(1, 2)])\n", "[1, 2]\n",
		},
		{
			"the result is the value of a dict entry",
			"print({\"k\": min(1.0, 2)})\n", "{'k': 1.0}\n",
		},
		{
			"the result of a nested fold",
			"print(min([min(1, 2), 0]))\n", "0\n",
		},
		// ---- texts order by their content, not by their intern table position.
		{
			"two text candidates",
			"print(min(\"b\", \"a\"), max(\"a\", \"b\"))\n", "a b\n",
		},
		{
			"settled text variables",
			"s1 = \"pear\"\ns2 = \"apple\"\nprint(min(s1, s2), max(s1, s2))\n", "apple pear\n",
		},
		{
			"a bound text result is still text",
			"t = min(\"pear\", \"apple\")\nprint(t, t.upper())\n", "apple APPLE\n",
		},
		{
			"a text-returning user callee is a text candidate",
			"def s(x):\n    if x:\n        return \"z\"\n    return \"a\"\n\nprint(min(s(True), s(False)))\n", "a\n",
		},
		{
			"a text-returning callee with an argument is a text candidate",
			"def greet(x):\n    return \"hi \" + x\n\nprint(min(greet(\"b\"), greet(\"a\")))\n", "hi a\n",
		},
		// ---- the container spellings keep their old rule, and gain the parts they were missing.
		{
			"an inline numeric list chooses its winner",
			"print(min([3, 1, 2]), max([3, 1, 2]))\n", "1 3\n",
		},
		{
			"an inline numeric list's float winner keeps the double",
			"print(max([1, 2.5]))\n", "2.5\n",
		},
		{
			"an inline numeric list's int winner keeps the int",
			"print(min([1, 2.5]))\n", "1\n",
		},
		{
			"an inline text list orders by text",
			"print(min([\"b\", \"a\", \"c\"]), max([\"b\", \"a\", \"c\"]))\n", "a c\n",
		},
		{
			"a single-None list prints None, not the untagged zero",
			"print(min([None]))\n", "None\n",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			res, err := Compile(tc.src)
			if err != nil {
				t.Fatalf("%s (%q): refused: %v", tc.name, tc.src, err)
			}
			assertNoForbiddenIR(t, tc.src, res.IR)
			if out := runIR(t, res.IR); out != tc.want {
				t.Errorf("%s: AOT ran %q, want CPython's %q\nsrc: %s", tc.name, out, tc.want, tc.src)
			}
			if out := captureStdout(t, tc.src); out != tc.want {
				t.Errorf("%s: interpreter printed %q, want CPython's %q\nsrc: %s", tc.name, out, tc.want, tc.src)
			}
		})
	}
}

func TestMinMaxRaiseWhenCandidatesHaveNoOrder(t *testing.T) {
	for _, tc := range []struct{ name, src, class, message string }{
		// The operator is not decoration: `min` asks `<`, `max` asks `>`, and the reference
		// implementation puts the failing one in the sentence.
		{
			"min's text candidate names the failing `<`",
			"print(min(1, \"a\"))\n",
			"TypeError", "'<' not supported between instances of 'str' and 'int'",
		},
		{
			"max's text candidate names the failing `>`",
			"print(max(1, \"a\"))\n",
			"TypeError", "'>' not supported between instances of 'str' and 'int'",
		},
		{
			"a text incumbent orders its number candidate the other way round",
			"print(min(\"a\", 1))\n",
			"TypeError", "'<' not supported between instances of 'int' and 'str'",
		},
		{
			"max's number candidate against a text incumbent",
			"print(max(\"a\", 1))\n",
			"TypeError", "'>' not supported between instances of 'int' and 'str'",
		},
		// None and containers are values too; comparing them is the oracle's TypeError, not an
		// untagged payload comparison wearing the shape of a result.
		{
			"None beside an int",
			"print(min(None, 1))\n",
			"TypeError", "'<' not supported between instances of 'int' and 'NoneType'",
		},
		{
			"an int incumbent and a None candidate",
			"print(max(1, None))\n",
			"TypeError", "'>' not supported between instances of 'NoneType' and 'int'",
		},
		{
			"a list candidate beside an int",
			"print(min(1, [2]))\n",
			"TypeError", "'<' not supported between instances of 'list' and 'int'",
		},
		{
			"a list incumbent beside an int candidate",
			"print(min([1], 2))\n",
			"TypeError", "'<' not supported between instances of 'int' and 'list'",
		},
		{
			"a float incumbent names its own kind, not a generic number",
			"print(min(1.5, [2]))\n",
			"TypeError", "'<' not supported between instances of 'list' and 'float'",
		},
		{
			"a set candidate names its own kind",
			"print(max(1.5, {2}))\n",
			"TypeError", "'>' not supported between instances of 'set' and 'float'",
		},
		{
			"a dict candidate names its own kind",
			"print(min(1, {\"k\": 2}))\n",
			"TypeError", "'<' not supported between instances of 'dict' and 'int'",
		},
		// The one-argument container form gets the same rule from its elements.
		{
			"min's mixed list raises where it once answered by intern index",
			"print(min([1, \"a\"]))\n",
			"TypeError", "'<' not supported between instances of 'str' and 'int'",
		},
		{
			"max's mixed list names the `>` it was asking",
			"print(max([1, \"a\"]))\n",
			"TypeError", "'>' not supported between instances of 'str' and 'int'",
		},
		// A verdict candidate is a bool in the sentence, not an int: the fold settles the comparison
		// as a number (True is 1) and names the kind the candidate has, which is what the interpreter's
		// compareOrder and ADR 0259's element tag both say for the same operand (roadmap Gap R.117).
		{
			"a verdict candidate beside a text incumbent says 'bool'",
			"print(max([\"a\", True]))\n",
			"TypeError", "'>' not supported between instances of 'bool' and 'str'",
		},
		{
			"a verdict incumbent says 'bool' the other way round",
			"print(max([True, None]))\n",
			"TypeError", "'>' not supported between instances of 'NoneType' and 'bool'",
		},
		{
			"min's verdict candidate beside a text incumbent says 'bool'",
			"print(min([\"a\", False]))\n",
			"TypeError", "'<' not supported between instances of 'bool' and 'str'",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ee := trapRun(t, tc.src)
			if ee.ExnType != tc.class {
				t.Errorf("interpreter raised %q, want %q (msg %q)", ee.ExnType, tc.class, ee.ExnMsg)
			}
			if ee.ExnMsg != tc.message {
				t.Errorf("interpreter message =\n  %q\nwant\n  %q", ee.ExnMsg, tc.message)
			}
			res, err := Compile(tc.src)
			if err != nil {
				t.Fatalf("the compiled backend refused a program the oracle traps on: %v", err)
			}
			out := runIRMayTrap(t, res.IR)
			if !strings.Contains(out, "Traceback (most recent call last):") {
				t.Errorf("the compiled program printed no traceback:\n%s", out)
			}
			if !strings.Contains(out, tc.message) {
				t.Errorf("compiled message missing %q:\n%s", tc.message, out)
			}
		})
	}
}

func TestMinMaxTrapsAreCatchable(t *testing.T) {
	for _, tc := range []struct{ name, src, want string }{
		{
			"the text/number TypeError",
			"try:\n    print(min(1, \"a\"))\nexcept TypeError:\n    print(\"caught\")\n",
			"caught\n",
		},
		{
			"the container/number TypeError",
			"try:\n    print(max(1, [2]))\nexcept TypeError:\n    print(\"caught\")\n",
			"caught\n",
		},
		{
			"a mixed list's raise does not end the program",
			"try:\n    print(min([1, \"a\"]))\nexcept TypeError:\n    print(min(3, 1))\n",
			"1\n",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			res, err := Compile(tc.src)
			if err != nil {
				t.Fatalf("refused a catchable raise: %v", err)
			}
			if out := runIR(t, res.IR); out != tc.want {
				t.Errorf("AOT ran %q, want %q", out, tc.want)
			}
			if out := captureStdout(t, tc.src); out != tc.want {
				t.Errorf("interpreter printed %q, want %q", out, tc.want)
			}
		})
	}
}

// TestMinMaxArityAndEmptyRaiseTheirOwnClasses covers the interpreter path. The compiled path cannot
// reach a runtime error for these literals and refuses at the front end; Gap R.37 owns turning that
// class of constant trap into an emitted raise.
func TestMinMaxArityAndEmptyRaiseTheirOwnClasses(t *testing.T) {
	for _, tc := range []struct{ name, src, class, message string }{
		{
			"min with no argument",
			"print(min())\n",
			"TypeError", "min expected at least 1 argument, got 0",
		},
		{
			"max with no argument",
			"print(max())\n",
			"TypeError", "max expected at least 1 argument, got 0",
		},
		{
			"an empty list",
			"print(min([]))\n",
			"ValueError", "min() iterable argument is empty",
		},
		{
			"an empty max",
			"print(max([]))\n",
			"ValueError", "max() iterable argument is empty",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ee := trapRun(t, tc.src)
			if ee.ExnType != tc.class {
				t.Errorf("interpreter raised %q, want %q (msg %q)", ee.ExnType, tc.class, ee.ExnMsg)
			}
			if ee.ExnMsg != tc.message {
				t.Errorf("interpreter message =\n  %q\nwant\n  %q", ee.ExnMsg, tc.message)
			}
		})
	}
}

func TestMinMaxLowerTheOrderingEachDomainActuallyHas(t *testing.T) {
	for _, tc := range []struct {
		name, src string
		want      []string
		deny      []string
	}{
		{
			"runtime int candidates compare integers",
			"x = 2\ny = 3\nprint(min(x, y))\n",
			[]string{"= icmp slt i32", "= select i1"},
			nil,
		},
		{
			"runtime double candidates compare doubles",
			"a = 1.5\nb = 2.5\nprint(max(a, b))\n",
			[]string{"= fcmp ogt double", "= select i1"},
			nil,
		},
		{
			"text candidates call the text-order helper",
			"a = \"pear\"\nb = \"apple\"\nprint(min(a, b))\n",
			[]string{"call i32 @rt_str_order(i32", "= select i1"},
			nil,
		},
		{
			"an int winner is not promoted to the double domain",
			"print(min(2.5, 1))\n",
			nil,
			[]string{"fcmp", "fadd double"},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			res, err := Compile(tc.src)
			if err != nil {
				t.Fatalf("refused: %v\nsrc: %s", err, tc.src)
			}
			for _, want := range tc.want {
				if !strings.Contains(res.IR, want) {
					t.Errorf("module is missing %q:\n%s", want, res.IR)
				}
			}
			for _, deny := range tc.deny {
				if strings.Contains(res.IR, deny) {
					t.Errorf("module says the int winner is a double (%q):\n%s", deny, res.IR)
				}
			}
		})
	}
}

// TestMinMaxShapesStillFiledNotFixed is where this feature admits what it still cannot do. None of
// these rows is a silent wrong answer: each compiled leg either refuses honestly or, in the case
// of a parameter's inferred kind, is pinned at the wrong answer the tagged value word owns. None
// reaches llc, and exit 2 remains the compiler's bug.
func TestMinMaxShapesStillFiledNotFixed(t *testing.T) {
	for _, tc := range []struct{ name, src, aotWant, oracle, gap string }{
		{
			"runtime int and double candidates have no untagged winner word",
			"a = 2.5\nb = 1\nprint(min(a, b), max(a, b))\n",
			"winner's own kind needs the tagged value word", "1 2.5\n", "roadmap Gap R.109",
		},
		{
			"one container variable, whose elements the compiler cannot see",
			"xs = [3, 1, 2]\nprint(min(xs), max(xs))\n",
			"min requires an inline list/set/dict literal", "1 3\n", "roadmap Gap R.107",
		},
		{
			"one text container variable",
			"xs = [\"b\", \"a\"]\nprint(min(xs))\n",
			"min requires an inline list/set/dict literal", "a\n", "roadmap Gap R.108",
		},
		{
			"a parameter pair whose call-site kinds disagree",
			"def choose(a, b):\n    return max(a, b)\n\nprint(choose(2.0, 1))\n",
			"", "2.0\n", "roadmap Gap R.110",
		},
		{
			"no arguments, whose ValueError would be raised rather than parsed",
			"print(min())\n",
			"min/max expect at least one argument", "TypeError: min expected at least 1 argument, got 0", "roadmap Gap R.37",
		},
		{
			"empty literal, whose ValueError the constant path refuses",
			"print(min([]))\n",
			"min requires an inline list/set/dict literal", "ValueError: min() iterable argument is empty", "roadmap Gap R.37",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if tc.gap == "roadmap Gap R.37" {
				class, msg, _ := strings.Cut(tc.oracle, ": ")
				ee := trapRun(t, tc.src)
				if ee.ExnType != class || ee.ExnMsg != msg {
					t.Errorf("%s: interpreter raised %s: %s, want %s", tc.name, ee.ExnType, ee.ExnMsg, tc.oracle)
				}
			} else if got := captureStdout(t, tc.src); got != tc.oracle {
				t.Errorf("%s: interpreter printed %q, want %q", tc.name, got, tc.oracle)
			}
			res, err := Compile(tc.src)
			if err != nil {
				if !strings.Contains(err.Error(), tc.aotWant) {
					t.Errorf("%s refused with %q, want the recorded %q (%s)", tc.name, err.Error(), tc.aotWant, tc.gap)
				}
				return
			}
			if tc.gap != "roadmap Gap R.110" {
				t.Fatalf("%s compiled; %s owes the refusal this row pins", tc.name, tc.gap)
			}
			// Gap R.110 is a wrong answer, not a refusal, and pinning it is the only honest way
			// to leave the existing numeric-return convention open without hiding its symptom.
			if out := runIR(t, res.IR); out != "2\n" {
				t.Errorf("%s: AOT printed %q, want the recorded wrong answer %q (%s)", tc.name, out, "2\n", tc.gap)
			}
		})
	}
}

// TestBoolCandidatesPrintWhatTheChosenCandidateIs is the row that used to pin the wrong answer: this
// family's one member both backends answered identically and the oracle did not. A verdict is a value
// (ADR 0257) and a container slot may say so (ADR 0259); what was missing was the *chosen* candidate's
// kind crossing out of the fold, which is roadmap Gap R.117 and ADR 0261. The tie rows are the point:
// `max(True, 1)` keeps the verdict because the comparison is strict and the first candidate stays, and
// `max(1, True)` keeps the number — a rule no “are the elements bools?“ answer can produce.
func TestBoolCandidatesPrintWhatTheChosenCandidateIs(t *testing.T) {
	for _, tc := range []struct{ src, want string }{
		{"print(min(True, 0), max(True, 1))\n", "0 True\n"},                // CPython's own line
		{"print(max(True, 1), max(1, True))\n", "True 1\n"},                // the tie, both ways round
		{"print(min(False, 0), min(0, False))\n", "False 0\n"},             // …and the same for min
		{"print(max([True, 0]) + 1, str(min([False, 1])))\n", "2 False\n"}, // the number is still there
	} {
		res, err := Compile(tc.src)
		if err != nil {
			t.Fatalf("%q refused: %v", tc.src, err)
		}
		if out := runIR(t, res.IR); out != tc.want {
			t.Errorf("AOT %q printed %q, want %q", tc.src, out, tc.want)
		}
		if out := captureStdout(t, tc.src); out != tc.want {
			t.Errorf("interpreter %q printed %q, want %q", tc.src, out, tc.want)
		}
	}
}
