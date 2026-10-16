package lang

// The record leg for printf-style formatting (roadmap Gap R.31, L11.1, ADR 0360).
//
// Every source here was refused by both witnesses before this round: the retired engine raised the
// operand TypeError for `text % x` (ADR 0215's gate) and the compiled backend refused to lower it. So
// none of them has an answer the retired engine ever gave — the record's `meta.added_after_0302` says
// what that means, and it is the reference (CPython) that these entries were recorded from, with the
// compiled leg cross-checked here and by the conformance matrix. A missing record fails the case, so
// what this file buys is coverage that cannot be deleted by deleting a row.
//
// The trap rows are half-judged here and half elsewhere. `runGoldenStdout` hands back the record's own
// class and sentence for a program that ended on a raise, so asserting them against the record would be
// reading the ledger back to itself; what this file can honestly assert is that the compiled leg TRAPPED
// (exit non-zero, a raise that unwound) and printed exactly what the record says it printed on the way.
// The class and sentence themselves are pinned to the reference's wording, in plain words, by
// percent_answer_test.go — which is the row that would notice if the door started saying
// `KeyError: key not found` where the reference says `KeyError: 'missing'`.

import (
	"strings"
	"testing"
)

func TestPercentRecordLegAnswersTheProgramsTheReferenceAnswers(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct{ name, src, want string }{
		{
			"the row: %s over a number",
			"print(\"%s\" % 2)\n",
			"2\n",
		},
		{
			"two conversions, one tuple",
			"print(\"%d-%d\" % (1, 2))\n",
			"1-2\n",
		},
		{
			"a sentence of mixed conversions",
			"print(\"hi %s, you are %d\" % (\"Ada\", 36))\n",
			"hi Ada, you are 36\n",
		},
		{
			"the flags and the widths",
			"print(\"%05d|%-5d|%+d|% d\" % (42, 42, 7, 7))\n",
			"00042|42   |+7| 7\n",
		},
		{
			"the alternate forms",
			"print(\"%#x %#X %#o\" % (255, 255, 8))\n",
			"0xff 0XFF 0o10\n",
		},
		{
			"the real conversions, with the banker's rounding",
			"print(\"%5.2f|%08.3f|%.0f\" % (3.14159, 3.14159, 2.5))\n",
			" 3.14|0003.142|2\n",
		},
		{
			"%r quotes, %a quotes the way ascii() does",
			"print(\"%r\" % \"text\")\n",
			"'text'\n",
		},
		{
			"%a keeps the double quotes a single quote needs",
			"print(\"%a\" % \"it's\")\n",
			"\"it's\"\n",
		},
		{
			"%c renders a code point, not its number",
			"print(\"%c %c\" % (65, 8364))\n",
			"A \u20ac\n",
		},
		{
			"a field is measured in characters",
			"print(\"%10s|%-10s|\" % (\"hi\", \"hi\"))\n",
			"        hi|hi        |\n",
		},
		{
			"a width and a precision the program supplies",
			"print(\"%*d|%.2f|\" % (5, 42, 3.14159))\n",
			"   42|3.14|\n",
		},
		{
			"the mapping form reads its keys",
			"print(\"%(who)s scored %(score)d\" % {\"who\": \"Ada\", \"score\": 7})\n",
			"Ada scored 7\n",
		},
		{
			"%% is one percent and spends nothing",
			"print(\"100%% of %d\" % 100)\n",
			"100% of 100\n",
		},
		{
			"a container renders through the printer",
			"print(\"%s\" % [1, 2])\n",
			"[1, 2]\n",
		},
		{
			"a verdict is a number to %d and a word to %s",
			"print(\"%d\" % True)\n",
			"1\n",
		},
		{
			"the answer is a text: bound, measured, upper-cased",
			"s = \"%s!\" % \"hi\"\nprint(s)\nprint(len(s))\nprint(s.upper())\n",
			"hi!\n3\nHI!\n",
		},
		{
			"the answer is a text: a container slot carries its kind",
			"xs = [\"%d\" % 7]\nprint(xs)\nprint(xs[0])\n",
			"['7']\n7\n",
		},
		{
			"the answer is a text: a dict entry",
			"d = {\"k\": \"%s\" % 1}\nprint(d[\"k\"])\n",
			"1\n",
		},
		{
			"the answer is a text: across a function boundary",
			"def f(n):\n    return \"%d items\" % n\n\nprint(f(3))\nprint(f(12))\n",
			"3 items\n12 items\n",
		},
		{
			"the answer is a text: in an instance slot",
			"class C:\n    def __init__(self):\n        self.w = \"%s!\" % \"hi\"\n\nprint(C().w)\n",
			"hi!\n",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, err := runGoldenStdout(t, tc.src)
			if err != nil {
				t.Fatalf("the record leg failed: %v\nsrc: %s", err, tc.src)
			}
			if got != tc.want {
				t.Errorf("record leg %q, want %q\nsrc: %s", got, tc.want, tc.src)
			}
		})
	}
}

func TestPercentRecordLegHoldsTheTrapsAsTraps(t *testing.T) {
	t.Parallel()
	// What the reference raises for each of these, and what the record therefore holds; the assertion
	// is that the compiled leg agrees it raised and printed what the record says it printed.
	for _, tc := range []struct{ name, src, class, stdoutSoFar string }{
		{"%d cannot read a text", "print(\"%d\" % \"text\")\n", "TypeError", ""},
		{"%x cannot read a text", "print(\"%x\" % \"text\")\n", "TypeError", ""},
		{"%f cannot read a text", "print(\"%f\" % \"text\")\n", "TypeError", ""},
		{"too few arguments", "print(\"%s %s\" % (\"one\",))\n", "TypeError", ""},
		{"a spare argument", "print(\"%s\" % (\"a\", \"b\"))\n", "TypeError", ""},
		{"a key the mapping does not hold", "print(\"%(missing)s\" % {\"a\": 1})\n", "KeyError", ""},
		{"a keyed format against a number", "print(\"%(a)s\" % 5)\n", "TypeError", ""},
		{"a code point beyond the range", "print(\"%c\" % 3000000)\n", "OverflowError", ""},
		{"two characters are not one", "print(\"%c\" % \"ab\")\n", "TypeError", ""},
		{"a conversion the reference does not have", "print(\"%Q\" % 1)\n", "ValueError", ""},
		{"a percent with nothing after it", "print(\"%s %\" % (\"a\",))\n", "ValueError", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, err := runGoldenStdout(t, tc.src)
			if err == nil {
				t.Fatalf("the compiled leg answered this program with no raise; the record holds a %s\nsrc: %s", tc.class, tc.src)
			}
			if got != tc.stdoutSoFar {
				t.Errorf("the program printed %q before the raise, want %q\nsrc: %s", got, tc.stdoutSoFar, tc.src)
			}
			// The record's own class is the reference's class; a compiled leg that raised a different
			// one would have been reported as a divergence by TestMain, and this line says which class
			// the case was written to expect.
			if !strings.Contains(err.Error(), tc.class) && tc.class != "" {
				t.Errorf("the record holds a %s for this program; the divergence ledger must agree: %v", tc.class, err)
			}
		})
	}
}
