package lang

import (
	"strings"
	"testing"
)

// An ordering of two texts reads the text (roadmap Gap R.84, ADR 0248).
//
// A text value is an index into @str_tab, and the index is handed out in the order the program
// mentions each spelling. Equality is safe for that — interning is content-addressed, so two equal
// texts are the same index — but an ordering is not: `print(1 if "b" > "a" else 0)` asked which of
// the two the program wrote first and printed 0 where CPython prints 1. Every `<`, `<=`, `>`, `>=`
// between texts had that answer, in both value and condition position, and the fix is the one the
// sorter already used: rt_sort has taken a mode argument and compares its elements with strcmp
// since ADR 0173, while the comparison operators were never given the same question.
//
// The rows below are ordered so that the *arrival* order and the *text* order disagree: a table
// written with "a" first would have passed the old code, which is how this survived.

func TestTextOrderingMatchesCPythonInBothEngines(t *testing.T) {
	for _, tc := range []struct {
		name string
		src  string
		want string
	}{
		{
			"the literal pair whose arrival order is backwards",
			"print(1 if \"b\" > \"a\" else 0)\nprint(1 if \"b\" < \"a\" else 0)\n", "1\n0\n",
		},
		{
			"two text variables",
			"a = \"b\"\nb = \"a\"\nprint(1 if a > b else 0)\nprint(1 if a >= b else 0)\n", "1\n1\n",
		},
		{
			"the same text against itself",
			"a = \"a\"\nb = \"a\"\nprint(1 if a <= b else 0, 1 if a >= b else 0, 1 if a == b else 0)\n",
			"1 1 1\n",
		},
		{
			// The pair that shows an ordering is not equality: both compare equal by index, and only
			// strcmp knows that "b" is the one that comes later.
			"the empty text is below everything, as Python counts it",
			"a = \"\"\nb = \"z\"\nprint(1 if a < b else 0)\nprint(1 if b > a else 0)\nprint(1 if a < \"\" else 0)\n",
			"1\n1\n0\n",
		},
		{
			"uppercase is below lowercase, which the intern table certainly is not",
			"a = \"B\"\nb = \"a\"\nprint(1 if a < b else 0)\nprint(1 if b < a else 0)\n", "1\n0\n",
			// arrival: "B" then "a" — the index order says "B" first, which is also the text order, so
			// the pair is repeated below in the other arrival order.
		},
		{
			"the same pair, mentioned the other way round",
			"a = \"a\"\nb = \"B\"\nprint(1 if a < b else 0)\nprint(1 if b < a else 0)\n", "0\n1\n",
		},
		{
			"a prefix is below the longer text",
			"a = \"app\"\nb = \"apple\"\nprint(1 if a < b else 0)\nprint(1 if b <= a else 0)\n", "1\n0\n",
		},
		{
			"in a condition, where the comparison is an i1 rather than a value",
			"a = \"b\"\nif a > \"a\":\n    print(\"later\")\nelse:\n    print(\"earlier\")\n", "later\n",
		},
		{
			"in a condition the other way round",
			"a = \"a\"\nif a > \"b\":\n    print(\"later\")\nelse:\n    print(\"earlier\")\n", "earlier\n",
		},
		{
			"the result of a string method orders by its text",
			"a = \"B\"\nprint(1 if a.lower() > \"a\" else 0)\nprint(1 if a.lower() < \"a\" else 0)\n", "1\n0\n",
		},
		{
			"in a sort, where the elements arrive out of order",
			"xs = [\"b\", \"a\", \"c\"]\nprint(sorted(xs))\n", "['a', 'b', 'c']\n",
		},
		{
			// The row the two doors disagreed over: print(1 if a > b else 0) answered the interned
			// index, and the same comparison stored in a variable was refused outright. The answer is
			// the i32 0/1 a comparison lowers to in this backend; printing it as True is L11.1's
			// "bools are values" row, and both engines print 1 today.
			"stored in a variable and printed as a value",
			"a = \"b\"\nb = \"a\"\nlater = a > b\nprint(later)\n", "1\n",
		},
		{
			"a text comparison returned from a function",
			"def after(w):\n    return w > \"a\"\n\nprint(1 if after(\"b\") else 0)\nprint(1 if after(\"Z\") else 0)\n",
			"1\n0\n",
		},
		{
			"in a loop body, ordered against the literal it arrives after",
			"for w in [\"pear\", \"apple\"]:\n    print(1 if w > \"a\" else 0)\n", "1\n1\n",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			res, err := Compile(tc.src)
			if err != nil {
				t.Fatalf("%q refused: %v", tc.src, err)
			}
			if out := runIR(t, res.IR); out != tc.want {
				t.Errorf("AOT ran %q, want CPython's %q\nsrc: %s", out, tc.want, tc.src)
			}
			if out := captureStdout(t, tc.src); out != tc.want {
				t.Errorf("interpreter printed %q, want CPython's %q\nsrc: %s", out, tc.want, tc.src)
			}
		})
	}
}

// One helper answers the question, in both the value position and the condition position. Two doors
// is how the sorter got it right and the operators got it wrong.
func TestTextOrderingAsksStrcmp(t *testing.T) {
	for _, src := range []string{
		"a = \"b\"\nb = \"a\"\nprint(1 if a > b else 0)\n",        // value position
		"a = \"b\"\nif a > \"a\":\n    print(\"y\")\n",            // condition position
		"xs = [\"b\", \"a\"]\nprint(1 if xs[0] > xs[1] else 0)\n", // slot reads
	} {
		ir := compileOrFatal(t, src)
		if !strings.Contains(ir, "call i32 @rt_str_order(") {
			t.Errorf("module orders %q without strcmp\norder calls: %v", src, irLinesContaining(ir, "rt_str_order"))
		}
		for _, ln := range irLinesContaining(ir, "call i32 @rt_str_order(") {
			if strings.Contains(ln, " i8* @.") || strings.Contains(ln, " i32 @.") {
				t.Errorf("a compile-time global sits in a value position (ADR 0166): %s", ln)
			}
		}
	}
}

// Equality of interned texts stays an index comparison — content-addressed interning makes it sound
// (ADR 0173), and routing == through strcmp as well would pay for a call to learn something the
// representation already guarantees.
func TestTextEqualityIsStillAnIndexComparison(t *testing.T) {
	ir := compileOrFatal(t, "a = \"b\"\nb = \"a\"\nprint(1 if a == b else 0)\n")
	if strings.Contains(ir, "call i32 @rt_str_order(") {
		t.Errorf("an equality went to strcmp:\n%s", strings.Join(irLinesContaining(ir, "rt_str_order"), "\n"))
	}
}
