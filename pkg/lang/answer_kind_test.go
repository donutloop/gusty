package lang

import (
	"os"
	"strconv"
	"strings"
	"testing"
)

// The kind words the answer direction stores must be the tag table's own words, or a carried answer
// arrives at the printer wearing a kind the printer does not know: `rt_print_mixed_value` answers a
// number for a tag it cannot name, which is the bug this row exists to close rather than a new one.
// The pair road writes two of these digits from its own constants; this is the seam between them.
func TestTheAnswerKindWordsAreTheTagTable(t *testing.T) {
	must := map[string]struct {
		tag  ValueTag
		word string
	}{
		"TagInt":  {TagInt, answerWordInt},
		"TagBool": {TagBool, answerWordBool},
		"TagNone": {TagNone, answerWordNone},
		"TagStr":  {TagStr, answerWordStr},
		"TagList": {TagList, answerWordList},
		"TagDict": {TagDict, answerWordDict},
		"TagSet":  {TagSet, answerWordSet},
	}
	for name, want := range must {
		if got := strconv.Itoa(int(want.tag)); got != want.word {
			t.Errorf("answerWord for %s is %q, the tag table says %q: a carried answer would arrive at the printer wearing a kind no switch arm reads", name, want.word, got)
		}
	}
	if pairTagNone != answerWordNone {
		t.Errorf("pairTagNone=%s but answerWordNone=%s: the two roads that store the void disagree about its word", pairTagNone, answerWordNone)
	}
}

// The plan is a whole-program fact, so the test that owns it reads whole programs: which bodies are
// carried, which are left alone, and — the part that matters most — that a body whose kind is not
// written in its spelling is left on the roads it always had rather than carried into a wrong answer.
func TestAnswerKindPlanCarriesOnlyMixedBodies(t *testing.T) {
	cases := []struct {
		name    string
		src     string
		carried bool
	}{
		{"text and int", "def f(x):\n    if x:\n        return \"x\"\n    return 0\n", true},
		{"one kind", "def f(x):\n    return x + 1\n", false},
		{"one kind text", "def f(x):\n    return \"x\"\n", false},
		{"number and none", "def f(x):\n    if x:\n        return 7\n", true},
		{"only the void", "def f(x):\n    print(x)\n", false},
		{"bool and int", "def f(x):\n    if x:\n        return True\n    return 2\n", true},
		{"a name's kind is unseen", "def f(x, y):\n    if x:\n        return y\n    return 0\n", false},
		{"a float is not spelled", "def f(x):\n    if x:\n        return 1.5\n    return 2\n", false},
		{"list and int", "def f(x):\n    if x:\n        return [1, 2]\n    return 9\n", true},
		{"dict and int", "def f(x):\n    if x:\n        return {\"k\": 1}\n    return 42\n", true},
		{"generator", "def f(x):\n    yield 1\n    return 2\n", false},
		{"decorated", "@dec\ndef f(x):\n    if x:\n        return \"x\"\n    return 0\n", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			prog, err := Parse(tc.src)
			if err != nil {
				t.Fatalf("parse: %v", err)
			}
			p := answerKindPlanOf(prog, map[string]bool{})
			if got := p.carries("f"); got != tc.carried {
				t.Fatalf("carries(\"f\") = %v, want %v for:\n%s", got, tc.carried, tc.src)
			}
		})
	}
}

// A carried body stores the constant word of the arm that ran, and a fall-off-the-end stores the
// void's. Without the second half `def f(x): if x: return 7` answers `7`/`0` where the reference
// answers `7`/`None` (roadmap Gap R.228).
func TestAnswerKindPlanStoresTheVoidOfAFallOffTheEnd(t *testing.T) {
	prog, err := Parse("def f(x):\n    if x:\n        return 7\n")
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	p := answerKindPlanOf(prog, map[string]bool{})
	if !p.carries("f") {
		t.Fatal("a body whose arms are an int and the void must carry its kind")
	}
	if p.answersOnlyTheVoid("f") {
		t.Fatal("the body answers 7 on one arm, so it does not answer only the void")
	}
	if len(p.arms) != 1 {
		t.Fatalf("the plan saw %d return roads, want the one `return 7` (the fall-off is the epilogue's, not a road): %v", len(p.arms), p.arms)
	}
	for _, arm := range p.arms {
		if arm.word != answerWordInt {
			t.Fatalf("the `return 7` road stores %q, want the int's word %q", arm.word, answerWordInt)
		}
	}
	if !answerDeclareStoresTheVoidWord(prog) {
		t.Fatal("a carried body must reach the epilogue that stores the void's word")
	}
}

// answerDeclareStoresTheVoidWord is the seam between the two roads that store a kind word: the answer
// direction declares the pair global for a carried body, and the function epilogue — which answers the
// fall-off-the-end — stores the void's word for any body that declared it. Without that seam
// `def f(x): if x: return 7` stores nothing on the road that answers None, and the caller reads
// whatever the previous call left in the word (roadmap Gap R.228, ADR 0273's rule about an invented
// default, ADR 0368).
func answerDeclareStoresTheVoidWord(prog *Program) bool {
	src, err := os.ReadFile("codegen_func.go")
	if err != nil {
		return false
	}
	return strings.Contains(string(src), "g.answerDeclare(g.fnName(fd))")
}

// A body the pair road already serves stores a RUN-TIME tag, which is a better answer than the
// constant one this pass writes; the plan must leave those functions to it, or the two roads write
// the same global from two bodies and the caller reads the wrong one.
func TestAnswerKindPlanLeavesThePairRoadAlone(t *testing.T) {
	prog, err := Parse("def g(a):\n    return a + 1\n\ndef f(x):\n    if x:\n        return g(1)\n    return g(2)\n")
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	p := answerKindPlanOf(prog, map[string]bool{"g": true})
	if p.carries("g") {
		t.Fatal("the pair road's answer was taken by the answer direction")
	}
}

// `return other(...)` forwards a kind the answer direction cannot name until OTHER is itself carried.
// The scan reaches that by rounds; a single pass over the source would answer `false` for a body
// written above the callee and `true` for the same body written below it, which is two answers to
// one question (roadmap ADR 0273's fixed point, ADR 0368).
func TestAnswerKindPlanForwardsACalleeWordAcrossFileOrder(t *testing.T) {
	mixed := "    if x:\n        return \"s\"\n    return 1\n"
	below := "def f(x):\n    if x:\n        return other(x)\n    return 0\n\ndef other(x):\n" + mixed
	above := "def other(x):\n" + mixed + "\ndef f(x):\n    if x:\n        return other(x)\n    return 0\n"
	for _, tc := range []struct {
		name string
		src  string
	}{{"callee below", below}, {"callee above", above}} {
		prog, err := Parse(tc.src)
		if err != nil {
			t.Fatalf("parse: %v", err)
		}
		p := answerKindPlanOf(prog, map[string]bool{})
		if !p.carries("other") {
			t.Fatalf("`other` answers a text on one arm and an int on the other:%s\n%s", tc.name, tc.src)
		}
		if !p.carries("f") {
			t.Fatalf("`f` forwards another's answer on one arm and answers an int on the other:%s\n%s", tc.name, tc.src)
		}
		var forwarded bool
		for _, arm := range p.arms {
			if arm.dyn == "other" {
				forwarded = true
			}
		}
		if !forwarded {
			t.Fatalf("the forwarding road did not name its callee: %v\n%s", p.arms, tc.src)
		}
	}
}

// One compile must not be answered by another's plan: the scan used to leave its result in a package
// variable, and the answer direction is the same shape of fact. This is ADR 0343's rule restated for
// a new scan — the two plans built from two programs stay separate objects.
func TestAnswerKindPlanIsNotAPackageVariable(t *testing.T) {
	a, err := Parse("def f(x):\n    if x:\n        return \"x\"\n    return 0\n")
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	b, err := Parse("def f(x):\n    return 0\n")
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	pa := answerKindPlanOf(a, map[string]bool{})
	pb := answerKindPlanOf(b, map[string]bool{})
	if !pa.carries("f") {
		t.Fatal("the mixed body lost its verdict")
	}
	if pb.carries("f") {
		t.Fatal("the one-kind body was answered out of the other plan")
	}
}
