package lang

// The printf-style `%` on a text, and the half of it this backend still declines.
//
// The formatting itself is answered — by the plan in percent_format.go and the run-time door in
// pkg/lang/percent_runtime.go — and its answers are pinned in percent_answer_test.go. This file is the
// other half of the same promise: the shapes the door will NOT lower are refused by the front end, in
// words that name their own cause, rather than answered with a number the program never asked for.
//
// That promise is the reason the operator was refused outright for eleven ADRs. `print("%.2f" % 3.5)` is
// CPython's `3.50`; before ADR 0282 the compiled leg printed `0.0` at **exit 0**, because the float road
// lifted the format string's `@str_tab` index through `sitofp` and `frem`'d it against 3.5 (roadmap
// L11.2, Gap R.165). A door that renders a text cannot also be the door that lifts its index as a number,
// so every row here is measured against `python3`, and the numeric controls in the second half are what
// keep the guard from being a ban on the operator (roadmap Gap R.31, L11.1, ADR 0360).

import (
	"strings"
	"testing"
)

// TestPercentRefusesWhatHasNoRenderer is the honesty table: a conversion the door has no renderer for is
// said at compile time, in the source it was written in, rather than reaching the door to be answered by
// the nearest thing to hand. `%b` and `%u` are not text conversions in the reference either — `%b` belongs
// to bytes and `%u` was removed — and `%p` is not implemented there at all, so the door's run-time
// unsupported-format-character ValueError covers it and the compile-time refusal covers only what would
// otherwise be guessed.
func TestPercentRefusesWhatHasNoRenderer(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct{ name, src, want string }{
		{"%b is bytes, not text", "print(\"%b\" % 3)\n", "%b"},
		{"%B is bytes, not text", "print(\"%B\" % 3)\n", "%B"},
		{"%u is the removed obsolete conversion", "print(\"%u\" % 3)\n", "%u"},
		{"the refusal names the roadmap row", "print(\"%b\" % 3)\n", "Gap R.31"},
		{"the refusal names the decision", "print(\"%b\" % 3)\n", "ADR 0360"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			_, err := Compile(tc.src)
			if err == nil {
				t.Fatalf("the door lowered a conversion it has no renderer for (Gap R.31): %s", tc.src)
			}
			if !strings.Contains(err.Error(), "codegen:") {
				t.Errorf("the refusal must be a front-end diagnostic, got %v", err)
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Errorf("the refusal must name %q, got: %s", tc.want, err)
			}
		})
	}
}

// TestPercentRefusesWhatHasNoValue is the L11.1 half of the honesty rule: an argument this backend has no
// value for cannot be rendered, and a payload without a kind beside it would print the number underneath
// it. Tuples are the case — still not values (L11.3) — and the refusal has to say which argument and why,
// not that the program is malformed.
func TestPercentRefusesWhatHasNoValue(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct{ name, src string }{
		{"a tuple standing where a value goes", "print(\"%s %s\" % (\"a\", (\"b\", \"c\")))\n"},
		{"a nested tuple", "print(\"%s\" % ((1, 2),))\n"},
		{"a lambda", "print(\"%s\" % (lambda: 1))\n"},
		{"a generator", "print(\"%s\" % (i for i in [1, 2]))\n"},
		{"one argument per conversion, more than the table holds", "print(\"%s\" % (" + strings.TrimSuffix(strings.Repeat("1, ", 65), ", ") + "))\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			_, err := Compile(tc.src)
			if err == nil {
				t.Fatalf("a %s was lowered as if it had a kind (roadmap L11.1, Gap R.31): %s", tc.name, tc.src)
			}
			if !strings.Contains(err.Error(), "codegen:") {
				t.Errorf("the refusal must be a front-end diagnostic, got %v", err)
			}
			// The refusal must blame the argument, not the format: it is the value that has no kind, and
			// a reader looking at the format will find nothing wrong with it (ADR 0181's wording rule).
			if strings.Contains(err.Error(), "format string is malformed") {
				t.Errorf("the refusal must not blame the format, got: %s", err)
			}
		})
	}
}

// TestRemainderStillAnswersIsTheOtherHalfOfGapR165 pins what the guard must not do: `%` is the
// remainder for numbers, on both legs, with both divide-by-zero sentences intact (ADR 0278's
// four). A guard that banned the operator would pass the table above and fail this one.
func TestRemainderStillAnswersIsTheOtherHalfOfGapR165(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct{ name, src, want string }{
		{"int remainder", "print(7 % 3)\n", "1\n"},
		{"negative int remainder", "print(-7 % 3)\n", "2\n"},
		{"float remainder", "print(7.5 % 2)\n", "1.5\n"},
		{"negative float remainder", "print(-7.5 % 2)\n", "0.5\n"},
		{"both double", "print(7.5 % 2.5)\n", "0.0\n"},
		{"remainder of a float by a float to zero result", "print(7.5 % 2.5) \n", "0.0\n"},
		{"remainder through a parameter", "def f(v):\n    return v % 2\n\nprint(f(7.5))\nprint(f(7))\n", "1.5\n1\n"},
		// The equality road lifts both of its operands through the same helper; a comparison is not a
		// numeric use, so unlike kinds must keep answering rather than being refused (ADR 0282's
		// narrowing, measured before the guard was scoped).
		{"float compared to a text", "print(1 if 1.0 == \"a\" else 0)\n", "0\n"},
		{"text compared to a float", "print(1 if \"a\" == 1.0 else 0)\n", "0\n"},
		{"float compared to a container", "print(1 if 1.0 == [1] else 0)\n", "0\n"},
		{"a float still prints", "print(1.5 + 2)\n", "3.5\n"},
		{"true division still answers", "print(7 / 2)\n", "3.5\n"},
		// A text on the LEFT of `%` is a formatting, and a number on the left is a remainder: the two
		// questions are asked by one plan (percentPlan), so the arm that decides cannot be a spelling
		// test in two places (ADR 0229, roadmap Gap R.31).
		{"a text on the right is still a remainder's neighbour", "print(7 % 3 if \"a\" == \"a\" else 0)\n", "1\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if out := captureStdout(t, tc.src); out != tc.want {
				t.Errorf("interpreter: stdout %q, want %q\nsrc: %s", out, tc.want, tc.src)
			}
			res, err := Compile(tc.src)
			if err != nil {
				t.Fatalf("compiled leg refused a program the oracle answers (%v): %s", err, tc.src)
			}
			if got := runIR(t, res.IR); got != tc.want {
				t.Errorf("compiled: stdout %q, want %q\nsrc: %s", got, tc.want, tc.src)
			}
		})
	}
}

// TestThePercentRefusalNamesItsOwnCause is the wording row. A refusal below the formatting door used to
// arrive with the *slot* sentence ("a slot whose kind only the object knows … needs the run-time tag"),
// which is true of a container read and false of a literal: it sent a reader looking for a container that
// was never in the program. The door's refusals name the argument they could not build a pair for, and the
// conversion refusals name the conversion (ADR 0282's rule, kept for the new door).
func TestThePercentRefusalNamesItsOwnCause(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name, src string
		wants     []string
		nots      []string
	}{
		{
			name: "a conversion with no renderer names the character",
			src:  "print(\"%b\" % 3)\n",
			wants: []string{
				"%b", "binary", "Gap R.31", "ADR 0360",
			},
			nots: []string{"run-time tag", "only the object knows"},
		},
		{
			name: "an argument with no value names the argument",
			src:  "print(\"%s %s\" % (\"a\", (\"b\", \"c\")))\n",
			wants: []string{
				"kind this backend cannot name", "number underneath", "Gap R.31", "L11.1",
			},
			// It must not blame the format string, which is well-formed, nor claim the operator is
			// missing, which it no longer is.
			nots: []string{"unsupported operator", "not implemented"},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			_, err := Compile(tc.src)
			if err == nil {
				t.Fatalf("expected the program to be refused: %s", tc.src)
			}
			msg := err.Error()
			for _, want := range tc.wants {
				if !strings.Contains(msg, want) {
					t.Errorf("the refusal must name %q, got: %s", want, msg)
				}
			}
			for _, not := range tc.nots {
				if strings.Contains(msg, not) {
					t.Errorf("the refusal must not say %q, got: %s", not, msg)
				}
			}
		})
	}
}

// TestPercentPlanIsAskedOnceByEveryRoad is ADR 0229's and ADR 0359's rule pinned to the one thing this
// feature adds: a formatting is a text on the road that prints it, on the road that binds it, on the road
// that stores it in a container, and on the road that returns it from a function — and it is the same
// answer, because it is the same predicate. Each pair below is the formatting and the same formatting
// reached through one more road; a road that kept its own idea of what a formatting is would print the
// @str_tab position, which is how Gap R.146 and Gap R.165 both started.
func TestPercentPlanIsAskedOnceByEveryRoad(t *testing.T) {
	t.Parallel()
	// Each road is the same formatting one step further from the print, and each owes the answer the
	// reference owes — written out row by row, because "the same answer as the direct print" is not what
	// a road that measures or concatenates owes anybody. What every row shares is that none of them can
	// answer the @str_tab POSITION: `3` where `hi!` is due, or `1` where a text is due, is the failure
	// this table exists to catch (roadmap Gap R.146, Gap R.165, Gap R.31).
	for _, tc := range []struct{ name, src, want string }{
		{"direct", "print(\"%s!\" % \"hi\")\n", "hi!\n"},
		{"bound to a name", "s = \"%s!\" % \"hi\"\nprint(s)\n", "hi!\n"},
		{"returned from a function", "def f():\n    return \"%s!\" % \"hi\"\n\nprint(f())\n", "hi!\n"},
		{"returned from an annotated function", "def f() -> str:\n    return \"%s!\" % \"hi\"\n\nprint(f())\n", "hi!\n"},
		{"stored in a list", "xs = [\"%s!\" % \"hi\"]\nprint(xs[0])\n", "hi!\n"},
		{"printed as a list", "xs = [\"%s!\" % \"hi\"]\nprint(xs)\n", "['hi!']\n"},
		{"stored in a dict", "d = {\"k\": \"%s!\" % \"hi\"}\nprint(d[\"k\"])\n", "hi!\n"},
		{"stored in a set", "s = {\"%s!\" % \"hi\"}\nfor v in s:\n    print(v)\n", "hi!\n"},
		{"stored in an attribute", "class C:\n    def __init__(self):\n        self.w = \"%s!\" % \"hi\"\n\nprint(C().w)\n", "hi!\n"},
		{"concatenated", "s = \"%s!\" % \"hi\"\nprint(s + \"?\")\n", "hi!?\n"},
		{"measured", "s = \"%s!\" % \"hi\"\nprint(len(s))\n", "3\n"},
		{"compared", "s = \"%s!\" % \"hi\"\nprint(s == \"hi!\")\n", "True\n"},
		{"upper-cased by a text method", "s = \"%s!\" % \"hi\"\nprint(s.upper())\n", "HI!\n"},
		{"interpolated", "s = \"%s!\" % \"hi\"\nprint(f\"<{s}>\")\n", "<hi!>\n"},
		{"formatted again", "print(\"[%s]\" % (\"%s!\" % \"hi\"))\n", "[hi!]\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			// Every road owes the answer the reference owes, never the @str_tab position under it.
			if out := captureStdout(t, tc.src); out != tc.want {
				t.Errorf("road %q: stdout %q, want %q — the road read the @str_tab position instead of the text\nsrc: %s", tc.name, out, tc.want, tc.src)
			}
		})
	}
	// The same census through the compiled leg's own runner, so a road that only the record leg exercises
	// cannot disagree with the road the CLI takes (the two-leg rule, AGENTS.md § the one backend).
	for _, tc := range []struct{ src, want string }{
		{"def f():\n    return \"%d items\" % 3\n\nprint(f())\n", "3 items\n"},
		{"d = {\"k\": \"%s!\" % \"hi\"}\nprint(d[\"k\"])\n", "hi!\n"},
	} {
		res, err := Compile(tc.src)
		if err != nil {
			t.Fatalf("compiled leg refused a program the reference answers (%v): %s", err, tc.src)
		}
		if got := runIR(t, res.IR); got != tc.want {
			t.Errorf("compiled leg: stdout %q, want %q\nsrc: %s", got, tc.want, tc.src)
		}
	}
}
