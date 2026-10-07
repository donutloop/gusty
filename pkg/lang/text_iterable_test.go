// A text used as an iterable (roadmap Gap R.185, owner L11.1 / L11.5; ADR 0298, ADR 0166's exit codes).
//
// Two silently-wrong answers on the record, at exit 0, where the compiled leg at least refused:
//
//	[c for c in "abc"]      CPython ['a','b','c']   --interp []      the characters live in sval, not elems
//	max("abc") / min("abc") CPython c / a           --interp abc     a text fell to the bare-scalar arm
//
// The `for` statement already iterated a text one rune at a time; these two roads asked a different
// question and got a different answer (ADR 0279/0280's one-question rule, applied to iteration).
package lang

import (
	"strings"
	"testing"
)

func TestComprehensionOverATextIteratesCharacters(t *testing.T) {
	for _, tc := range []struct{ src, want string }{
		{`print([c for c in "abc"])`, `['a', 'b', 'c']`},
		{`print([c for c in "abc" if c != "b"])`, `['a', 'c']`},
		{`print([c + c for c in "ab"])`, `['aa', 'bb']`},
		{`print([c for c in ""])`, `[]`},
		{`print(len([c for c in "abcd"]))`, `4`},
		{`print(sorted([c for c in "cba"]))`, `['a', 'b', 'c']`},
		// the answer the row replaced, pinned as no-longer-true
		{`print([c for c in "abc"] == [])`, `False`},
		// a list haystack keeps working
		{`print([x for x in [1, 2, 3]])`, `[1, 2, 3]`},
		{`print([x for x in "abc" if False])`, `[]`},
	} {
		t.Run(strings.ReplaceAll(tc.src, " ", ""), func(t *testing.T) {
			want := strings.TrimSpace(referenceOut(t, tc.src))
			if want == "" || strings.Contains(want, "Error") {
				t.Fatalf("the reference did not answer: %q", want)
			}
			if want != tc.want {
				t.Fatalf("this pin disagrees with the reference: %q vs %q", tc.want, want)
			}
			got := strings.TrimSpace(captureStdout(t, tc.src))
			if got != want {
				t.Fatalf("%q: interpreter %q, reference %q", tc.src, got, want)
			}
		})
	}
}

// min/max of a text compare CHARACTERS, because a text is an iterable and not a scalar.
func TestMinMaxOverATextAnswerACharacter(t *testing.T) {
	for _, tc := range []struct{ src, want string }{
		{`print(max("abc"))`, `c`},
		{`print(min("abc"))`, `a`},
		{`print(max("cba"))`, `c`},
		{`print(min("Za"))`, `Z`},
		{`print(max("a"))`, `a`},
		// and they still answer the whole value for the shapes that already worked
		{`print(max([1, 2, 3]))`, `3`},
		{`print(min([1, 2, 3]))`, `1`},
		{`print(max(3, 5))`, `5`},
	} {
		t.Run(strings.ReplaceAll(tc.src, " ", ""), func(t *testing.T) {
			want := strings.TrimSpace(referenceOut(t, tc.src))
			if want != tc.want {
				t.Fatalf("this pin disagrees with the reference: %q vs %q", tc.want, want)
			}
			got := strings.TrimSpace(captureStdout(t, tc.src))
			if got != want {
				t.Fatalf("%q: interpreter %q, reference %q (a text treated as a one-element collection "+
					"answered the whole string)", tc.src, got, want)
			}
		})
	}
}

// Iterating a text is ONE question, asked by three roads — `for`, a comprehension, and an iterable
// builtin. If the comprehension disagrees with the `for`, the language has two answers.
func TestTextIterationIsOneQuestionAcrossRoads(t *testing.T) {
	const src = `"abc"`
	forLoop := "for c in " + src + ":\n    print(c)\n"
	comp := "print([c for c in " + src + "])\n"
	forWant := strings.Split(strings.TrimSpace(referenceOut(t, forLoop)), "\n")
	compWant := strings.TrimSpace(referenceOut(t, comp))
	compGot := strings.TrimSpace(captureStdout(t, comp))
	if compGot != compWant {
		t.Fatalf("comprehension over a text: %q, reference %q", compGot, compWant)
	}
	if len(forWant) != 3 {
		t.Fatalf("the reference's for-loop leg did not produce three lines: %v", forWant)
	}
	// The same three characters must be what each road yields: strip the list's punctuation and compare
	// the characters themselves, so a comprehension that answers [] cannot pass beside a working `for`.
	flat := strings.NewReplacer("[", "", "]", "", "'", "", " ", "", ",", "").Replace(compGot)
	if flat == "" {
		t.Fatalf("the comprehension yielded nothing where the reference's `for` yields %v", forWant)
	}
	if flat != "abc" {
		t.Fatalf("the comprehension yielded %q (%q), where the reference's `for` yields %v", flat, compGot, forWant)
	}
}

// The compiled leg refuses these; a refusal is allowed, an invented answer is not. If it ever starts
// answering, this logs — and the row moves into the tables above rather than being deleted.
func TestCompiledLegRefusesOrAnswersButNeverInvents(t *testing.T) {
	for _, src := range []string{
		`print([c for c in "abc"])`,
		`print(max("abc"))`,
	} {
		got, refused := compiledOutOrRefusal(t, src)
		if refused {
			continue
		}
		want := strings.TrimSpace(referenceOut(t, src))
		if got != want {
			t.Fatalf("%q: compiled %q, reference %q — if the compiled leg answers it must answer correctly",
				src, got, want)
		}
		t.Log("the compiled leg now answers " + src + " — move this row into the tables above")
	}
}
