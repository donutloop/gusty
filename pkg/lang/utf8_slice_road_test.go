package lang

// A position in this language is a code point (ADR 0225), and a text has two slice roads: the fold the
// compiler takes when the source and every bound are constants, and `rt_str_slice`/`rt_str_byteoff` when
// a bound is a value. Measuring one road in bytes and the other in code points is one question asked two
// ways, and the two answers were different — `"héllo"[1:3]` said `é` written and `él` computed. Both roads
// now walk lead bytes and their continuation bytes (roadmap Gap R.47, found while the printf-style door's
// character-width padding was being matched against the reference, roadmap Gap R.31, ADR 0360).
//
// Every expectation below is what `python3` printed for the same source on this machine, and the ASCII
// controls are there so a fix that only works for accented text fails something.

import (
	"strconv"
	"strings"
	"testing"
)

// utf8Slices are the shapes: every bound written, every bound computed, and the same slice reached from
// the two roads the emitter has.
var utf8Slices = []struct{ name, src, want string }{
	{
		name: "a constant slice of an accented text",
		src:  "print(\"h\u00e9llo\"[1:3])\n",
		want: "\u00e9l\n",
	},
	{
		name: "the same slice with the bounds in variables",
		src:  "s = \"h\u00e9llo\"\ni = 1\nj = 3\nprint(s[i:j])\n",
		want: "\u00e9l\n",
	},
	{
		name: "written and computed bounds answer one answer, side by side",
		src:  "s = \"h\u00e9llo\"\ni = 1\nj = 3\nprint(s[1:3] == s[i:j])\nprint(s[1:3])\nprint(s[i:j])\n",
		want: "True\n\u00e9l\n\u00e9l\n",
	},
	{
		name: "an absent high bound on an accented text",
		src:  "print(\"h\u00e9llo\"[1:])\n",
		want: "\u00e9llo\n",
	},
	{
		name: "an absent low bound on an accented text",
		src:  "print(\"h\u00e9llo\"[:3])\n",
		want: "h\u00e9l\n",
	},
	{
		name: "a negative bound counts characters",
		src:  "print(\"h\u00e9llo\"[-3:-1])\n",
		want: "ll\n",
	},
	{
		name: "a step walks characters",
		src:  "print(\"h\u00e9llo\"[1:5:2])\n",
		want: "\u00e9l\n",
	},
	{
		name: "a reversed text keeps its characters whole",
		src:  "print(\"h\u00e9llo\"[::-1])\n",
		want: "oll\u00e9h\n",
	},
	{
		name: "four-byte characters are one position",
		src:  "print(\"a\U0001F600b\"[1:2])\n",
		want: "\U0001F600\n",
	},
	{
		name: "a folded slice is a text the text roads take",
		src:  "print(\"h\u00e9llo\"[1:3].upper())\n",
		want: "\u00c9L\n",
	},
	{
		name: "a folded slice compares by content",
		src:  "print(\"h\u00e9llo\"[1:3] == \"\u00e9l\")\n",
		want: "True\n",
	},
	{
		name: "an accent alone is one character to a field width",
		src:  "print(\"%10s|\" % \"\u00e9\")\n",
		want: "         \u00e9|\n",
	},
	{
		name: "and left-aligns as one character",
		src:  "print(\"%-10s|\" % \"\u00e9\")\n",
		want: "\u00e9         |\n",
	},
	// The ASCII controls: the byte walk is right for ASCII precisely when it is right for
	// everything, and these are the rows that fail if the character walk starts over-advancing.
	{
		name: "ascii constant slice",
		src:  "print(\"abcdef\"[1:3])\n",
		want: "bc\n",
	},
	{
		name: "ascii computed slice",
		src:  "s = \"abcdef\"\ni = 1\nj = 3\nprint(s[i:j])\n",
		want: "bc\n",
	},
	{
		name: "ascii empty middle",
		src:  "print(\"abcdef\"[2:2])\n",
		want: "\n",
	},
	{
		name: "ascii bounds past the end clamp",
		src:  "print(\"abcdef\"[4:99])\n",
		want: "ef\n",
	},
	{
		name: "ascii negative step",
		src:  "print(\"abcdef\"[::-1])\n",
		want: "fedcba\n",
	},
}

func TestACharacterSliceAnswersTheSameWhateverSpelledTheBound(t *testing.T) {
	t.Parallel()
	for _, tc := range utf8Slices {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if out := captureStdout(t, tc.src); out != tc.want {
				t.Errorf("compiled: stdout %q, want %q\nsrc: %s", out, tc.want, tc.src)
			}
			res, err := Compile(tc.src)
			if err != nil {
				t.Fatalf("the compiled leg refused a program the reference answers (%v): %s", err, tc.src)
			}
			if got := runIR(t, res.IR); got != tc.want {
				t.Errorf("JIT: stdout %q, want %q\nsrc: %s", got, tc.want, tc.src)
			}
		})
	}
}

// TestTheSliceFoldAndTheSliceHelperAskOneQuestion is the census form for the defect: the same program
// asked twice, once with the bounds written in the source and once with them in variables, and the two
// answers compared to each other rather than to an expectation. A fold that drifts back to bytes fails
// here even if nobody re-types an expectation.
func TestTheSliceFoldAndTheSliceHelperAskOneQuestion(t *testing.T) {
	t.Parallel()
	for _, text := range []string{"h\u00e9llo", "abc", "\u00e9", "a\U0001F600b", "caf\u00e9", "\u00fcberstra\u00dfe"} {
		for _, bounds := range [][2]int{{0, 1}, {1, 3}, {0, 5}, {2, 6}, {-2, 99}, {1, 1}} {
			lo, hi := bounds[0], bounds[1]
			src := "s = \"" + text + "\"\ni = " + strconv.Itoa(lo) + "\nj = " + strconv.Itoa(hi) + "\n" +
				"print(s[" + strconv.Itoa(lo) + ":" + strconv.Itoa(hi) + "])\nprint(s[i:j])\n"
			out := captureStdout(t, src)
			lines := strings.SplitN(out, "\n", 3)
			if len(lines) < 2 {
				t.Fatalf("the program printed %q, want two lines\nsrc: %s", out, src)
			}
			if lines[0] != lines[1] {
				t.Errorf("the two roads disagree for %q[%d:%d]: folded %q, run-time %q\nsrc: %s",
					text, lo, hi, lines[0], lines[1], src)
			}
		}
	}
}
