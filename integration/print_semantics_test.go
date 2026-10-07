package integration

import (
	"strings"
	"testing"

	"github.com/donutloop/gusty/pkg/lang"
)

// print(*args, sep=" ", end="\n") — Python's separator and terminator semantics,
// agreed on by the compiled path (ADR 0165).
//
// The old backend behaviour printed one argument per line, which quietly matched
// between backends (so parity could not see it) but made the most-used builtin in
// the language behave nothing like Python: `print("n =", n)` wrote two lines. Now
// arguments are joined with `sep` and terminated by `end`, and the argument
// *ordering* (an argument whose evaluation itself prints) is identical per backend.

func TestPrintSeparationParity(t *testing.T) {
	tests := []struct {
		name string
		src  string
		want string
	}{
		{"two ints are space joined", "print(1, 2)\n", "1 2\n"},
		{"label and value", "n = 41\nprint(\"n =\", n + 1)\n", "n = 42\n"},
		{"mixed literal kinds", "print(\"a\", 1, \"b\", 2)\n", "a 1 b 2\n"},
		{"bare print is a blank line", "print(1)\nprint()\nprint(2)\n", "1\n\n2\n"},
		{"sep joins with the given text", "print(1, 2, 3, sep=\", \")\n", "1, 2, 3\n"},
		{"sep may be empty", "print(\"a\", \"b\", sep=\"\")\n", "ab\n"},
		{"end replaces the newline", "print(\"tick\", end=\"!\")\nprint(\"tock\")\n", "tick!tock\n"},
		{"sep and end together (end replaces the newline entirely)", "print(\"a\", \"b\", sep=\"|\", end=\"?\")\n", "a|b?"},
		{"a percent in sep is literal text", "print(1, 2, sep=\"%\")\n", "1%2\n"},
		{
			"container argument inside a joined line",
			"xs = [1, 2, 3]\nprint(\"xs =\", xs)\n",
			"xs = [1, 2, 3]\n",
		},
		{
			"two containers with a custom separator",
			"xs = [1]\nys = [2]\nprint(xs, ys, sep=\"|\")\n",
			"[1]|[2]\n",
		},
		{
			"dict argument",
			"m = {1: 2}\nprint(\"m =\", m)\n",
			"m = {1: 2}\n",
		},
		{
			"set argument renders like Python",
			"sa = {7}\nprint(\"s =\", sa)\n",
			"s = {7}\n",
		},
		{
			// The argument is written after the separator, then its evaluation may
			// print of its own accord; the compiled path interleave identically.
			"an argument that prints keeps its place in the line",
			"def twice(v):\n    print(\"<<\", v, \">>\")\n    return v + v\n\nprint(\"got\", twice(21))\n",
			"got << 21 >>\n42\n",
		},
		{
			"floats keep their numeric form when joined",
			"x = 2.5\nprint(\"x\", x)\n",
			"x 2.5\n",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			recordOut := runCompiled(t, tc.src)
			aot := runAOT(t, tc.src)
			if recordOut != tc.want {
				t.Errorf("the record leg = %q, want %q", recordOut, tc.want)
			}
			if aot != tc.want {
				t.Errorf("AOT        = %q, want %q", aot, tc.want)
			}
		})
	}
}

// TestPrintKeywordArgsStayChecked: the checker must not reject print's keyword
// arguments, and the AOT backend must say so plainly when one is not a compile-time
// string instead of emitting IR that only LLVM's verifier complains about.
func TestPrintKeywordArgsStayChecked(t *testing.T) {
	src := "print(\"a\", 1, sep=\"|\")\n"
	prog, perr := lang.Parse(src)
	if perr != nil {
		t.Fatalf("parse: %v", perr)
	}
	for _, d := range lang.Analyze(prog) {
		if d.Level == lang.LevelError {
			t.Errorf("checker rejected print's sep: %v", d)
		}
	}
	_, err := lang.Compile("x = 2\nprint(\"a\", 1, sep=x)\n")
	if err == nil {
		t.Fatalf("expected an actionable codegen error for a non-constant sep, got IR")
	}
	if !strings.Contains(err.Error(), "sep") || !strings.Contains(err.Error(), "compile-time string") {
		t.Errorf("error should name the argument and what is required, got: %v", err)
	}
}

// TestPrintTerminatorIsNotBakedIntoArgumentFormats pins the codegen shape: an
// argument's printf format carries no newline, so `end` is honoured for every
// kind of argument (int, float, string, container). If a format grew a \n again,
// end="" would leave a stray line break and the backends would diverge.
func TestPrintTerminatorIsNotBakedIntoArgumentFormats(t *testing.T) {
	cases := []struct {
		src  string
		want string
	}{
		{"print(1, end=\"\")\n", "1"},
		{"x = 2.5\nprint(x, end=\"\")\n", "2.5"},
		{"print(\"s\", end=\"\")\n", "s"},
		{"xs = [1]\nprint(xs, end=\"\")\n", "[1]"},
		{"m = {1: 2}\nprint(m, end=\"\")\n", "{1: 2}"},
		{"sa = {1}\nprint(sa, end=\"\")\n", "{1}"},
	}
	for _, tc := range cases {
		lang.RecordedStdoutIs(t, tc.src, tc.want)
		if got := runAOT(t, tc.src); got != tc.want {
			t.Errorf("AOT wrote %q, want %q (%q)", got, tc.want, tc.src)
		}
	}
}
