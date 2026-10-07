package lang

import (
	"strings"
	"testing"
)

// numeric_fold_test.go — the numeric folds ask each element what it is.
//
// sum, min and max over an inline literal are compile-time folds, and the fold used to ask nothing
// of the elements: `total += el` in the record added raw handles (so sum([1.5, 2.5]) printed
// 562949953421319, the bits of a float box read as an int, and sum([[1], [2]]) printed a list
// handle where CPython raises TypeError), and the compiled fold collected float values only when
// *every* element was a float literal — a single integer in the list sent the whole sum through the
// i32 path, where 2.5 became 2 and sum([1, 2.5]) answered 3.0 against CPython's 3.5.
//
// min and max have a second rule, the one CPython's own documentation states: they return the
// element they choose, so the answer's type is the winner's. max([1, 2.5]) is the float 2.5 and
// min([2.5, 1]) is the integer 1 — printing 1.0 for the second is the same truncation error in
// the other direction (roadmap L11.1, L11.6; ADR 0221's float rules).

func TestNumericFoldsAnswersOnTheCompiledBackend(t *testing.T) {
	for _, tc := range []struct{ src, want string }{
		{"print(sum([1.5, 2.5]))\n", "4.0\n"},
		{"print(sum([1, 2.5]))\n", "3.5\n"},
		{"print(sum([1, 2, 3]))\n", "6\n"},
		{"print(sum([]))\n", "0\n"},
		{"print(max([1, 2.5]))\n", "2.5\n"},
		{"print(max([2.5, 1]))\n", "2.5\n"},
		{"print(min([2.5, 1]))\n", "1\n"},
		{"print(min([1, 2.5]))\n", "1\n"},
		{"print(min([1.5, 2.5]))\n", "1.5\n"},
		{"print(max([1.5, 2.5]))\n", "2.5\n"},
		{"x = sum([1, 2.5])\nprint(x + 1)\n", "4.5\n"},
	} {
		if out := captureStdout(t, tc.src); out != tc.want {
			t.Errorf("%q interpreter printed %q, want CPython's %q", tc.src, out, tc.want)
		}
		res, cerr := Compile(tc.src)
		if cerr != nil {
			t.Fatalf("%q refused: %v", tc.src, cerr)
		}
		assertNoForbiddenIR(t, tc.src, res.IR)
		if out := runIR(t, res.IR); out != tc.want {
			t.Errorf("%q AOT printed %q, want CPython's %q", tc.src, out, tc.want)
		}
	}
}

// The shapes the folds cannot answer honestly are refused, and the refusal names the element. The
// forbidden alternative is what used to happen: an int printed for a list handle, or a truncated
// float, both of which are exit 0 with an answer nobody asked for (ADR 0166).
func TestNumericFoldsRefuseWhatTheyCannotAnswer(t *testing.T) {
	for _, tc := range []struct{ src, want string }{
		{"print(sum([[1], [2]]))\n", "sum adds numbers"},
		{"print(sum([\"a\"]))\n", "is text"},
		{"print(any([[1], [2]]))\n", "no word for"},
		{"print(all([[1], [2]]))\n", "no word for"},
		{"print(max([[1], [2]]))\n", "no word for comparing two containers"},
	} {
		res, err := Compile(tc.src)
		if err == nil {
			t.Fatalf("%q answered %q; the fold has no honest value for it", tc.src, runIR(t, res.IR))
		}
		if !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%q refused with %q, want it to mention %q", tc.src, err.Error(), tc.want)
		}
		if strings.Contains(err.Error(), "LLVM ERROR") || strings.Contains(err.Error(), "verifier") {
			t.Errorf("%q failed as an IR problem instead of a front-end refusal: %v", tc.src, err)
		}
	}
	// the record, whose elements are boxed values, raises what CPython raises.
	for _, tc := range []struct{ src, want string }{
		{"print(sum([[1], [2]]))\n", "unsupported operand type(s) for +: 'int' and 'list'"},
		{"print(sum([\"a\"]))\n", "unsupported operand type(s) for +: 'int' and 'str'"},
	} {
		_, _, err := evalGolden(t, tc.src)
		if err == nil {
			t.Fatalf("%q answered; CPython raises %q", tc.src, tc.want)
		}
		if !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%q failed with %q, want %q", tc.src, err.Error(), tc.want)
		}
	}
}
