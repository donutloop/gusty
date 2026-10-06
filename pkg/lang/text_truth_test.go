// Truth of a text, and the kind a string method answers with (roadmap Gap R.183 and Gap R.184, owner
// L11.1 / L11.2; ADR 0297, ADR 0229's one-predicate rule, ADR 0166's exit codes).
//
//	print(not "x")          CPython False   --interp False   --aot True     # asI1 compared the intern slot to 0
//	print("ab".zfill(5))    CPython 000ab   --interp 000ab   --aot 0        # print listed 3 of 14 text methods
//	print("-42".zfill(5))   CPython -0042   BOTH engines 00-42              # parity agreed on the wrong answer
package lang

import (
	"os"
	"strings"
	"testing"
)

// `not` is answered by the same road an `if` test asks, or the two disagree in the same file.
func TestNotOfATextAnswersTheVerdictTheReferenceAnswers(t *testing.T) {
	for _, tc := range []struct{ src, want string }{
		{`print(not "x")`, `False`},
		{`print(not "abc")`, `False`},
		{`print(not "")`, `True`},
		{`print(not "0")`, `False`}, // a text that LOOKS like zero is still a non-empty text
		{`print(not "False")`, `False`},
		{`print(not None)`, `True`},
		{`print(not 0)`, `True`},
		{`print(not 3)`, `False`},
		{`print(not [])`, `True`},
		{`print(not [1])`, `False`},
		{`print(not True)`, `False`},
		{`print(not False)`, `True`},
	} {
		t.Run(strings.ReplaceAll(tc.src, " ", ""), func(t *testing.T) {
			want := strings.TrimSpace(referenceOut(t, tc.src))
			if want == "" || strings.Contains(want, "Error") {
				t.Fatalf("the reference did not answer: %q", want)
			}
			if want != tc.want {
				t.Fatalf("this pin disagrees with the reference: %q vs %q", tc.want, want)
			}
			got := strings.TrimSpace(compiledOut(t, tc.src))
			if got != want {
				t.Fatalf("%q: compiled %q, reference %q", tc.src, got, want)
			}
			// The `if` test and the `not` must not disagree about the same value — that disagreement
			// is exactly this bug's shape, so it is pinned rather than left to the sweep.
			for _, probe := range []string{`"x"`, `"abc"`} {
				ifSrc := "if " + probe + ":\n    print(1)\nelse:\n    print(0)\n"
				ifWant := strings.TrimSpace(referenceOut(t, ifSrc))
				ifGot := strings.TrimSpace(compiledOut(t, ifSrc))
				if ifGot != ifWant {
					t.Fatalf("the same value answers differently to `if` and to `not`: if %q (reference %q), not %q",
						ifGot, ifWant, got)
				}
			}
		})
	}
}

// Every text-returning string method answers TEXT; the print road used to list three of them.
func TestStringMethodsAnswerTextNotTheirIndex(t *testing.T) {
	for _, tc := range []struct{ src, want string }{
		{`print("ab".zfill(5))`, `000ab`},
		{`print("42".zfill(5))`, `00042`},
		{`print("abc".zfill(1))`, `abc`},
		// Padding is OUTSIDE the quotes: trimming the answer would throw it away, so these two ask
		// the reference for a quoted rendering and compare that.
		{`print(repr("ab".ljust(4)))`, `'ab  '`},
		{`print(repr("ab".rjust(4)))`, `'  ab'`},
		{`print("abc".capitalize())`, `Abc`},
		{`print("a b".title())`, `A B`},
		{`print("ab".swapcase())`, `AB`},
		{`print(repr(" a b".lstrip()))`, `'a b'`},
		{`print(repr("a b ".rstrip()))`, `'a b'`},
		{`print("abc".replace("b", "X"))`, `aXc`},
		{`print("abc".removeprefix("a"))`, `bc`},
		{`print("abc".removesuffix("c"))`, `ab`},
		{`print("ab".upper())`, `AB`},
		// a method that answers a NUMBER still answers a number
		{`print("abc".count("b"))`, `1`},
		{`print("abc".find("b"))`, `1`},
		// and one that answers a verdict still answers a verdict
		{`print("abc".startswith("a"))`, `True`},
	} {
		t.Run(strings.ReplaceAll(tc.src, " ", "_"), func(t *testing.T) {
			want := strings.TrimSpace(referenceOut(t, tc.src))
			if strings.Contains(want, "Error") {
				t.Fatalf("the reference did not answer: %q", want)
			}
			if want != tc.want {
				t.Fatalf("this pin disagrees with the reference: %q vs %q", tc.want, want)
			}
			got := strings.TrimSpace(compiledOut(t, tc.src))
			if got != strings.TrimSpace(want) {
				t.Fatalf("%q: compiled %q, reference %q — a text method whose answer printed as a bare "+
					"number is the intern INDEX leaking out (Gap R.42's class, Gap R.183)", tc.src, got, want)
			}
		})
	}
}

// zfill pads AFTER the sign — the row both engines agreed on, which is what makes it invisible to parity.
func TestZfillPadsAfterASign(t *testing.T) {
	for _, tc := range []struct{ src, want string }{
		{`print("-42".zfill(5))`, `-0042`},
		{`print("+42".zfill(5))`, `+0042`},
		{`print("-1".zfill(4))`, `-001`},
		{`print("-42".zfill(2))`, `-42`},
		{`print("42".zfill(5))`, `00042`},
	} {
		t.Run(strings.ReplaceAll(tc.src, " ", ""), func(t *testing.T) {
			want := strings.TrimSpace(referenceOut(t, tc.src))
			if want != tc.want {
				t.Fatalf("this pin disagrees with the reference: %q vs %q", tc.want, want)
			}
			got := strings.TrimSpace(compiledOut(t, tc.src))
			if got != want {
				t.Fatalf("%q: compiled %q, reference %q (both engines said %q before ADR 0297)",
					tc.src, got, want, "00-42")
			}
		})
	}
}

// One table, asked once: a three-name list in the print road against fourteen methods in the fold is
// how nine methods printed their index, and the same list existed TWICE in codegen.go.
func TestTextMethodTableIsAskedOnce(t *testing.T) {
	body, err := os.ReadFile("codegen.go")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(body), "func (g *irGen) textMethodAnswersText(") {
		t.Fatal("expected one predicate deciding which string methods answer text")
	}
	if n := strings.Count(string(body), "case \"upper\", \"lower\", \"strip\":"); n != 0 {
		t.Fatalf("the print road still carries a hand-copied three-method list (%d copies); ask "+
			"g.textMethodAnswersText — the fold implements fourteen (Gap R.183)", n)
	}
}
