package lang

// ADR 0428 · roadmap Gap R.451 — a body that FORWARDS the void answers the void.
//
// **ADR 0427** taught the comparison fold to ask **ADR 0368**'s answer plan whether a callee answers
// nothing but the void, and one call deeper the same question was still answered by the payload:
//
//	def f():
//	    return None
//	def g():
//	    return f()
//	print(g() is None)   # the reference: True    this backend, before: False   — at exit 0
//	print(g() == 0)      # the reference: False   this backend, before: True
//
// The second line is the one that shows the defect is not cosmetic: the fold was comparing the word
// `0` (**ADR 0187**: the void's payload is a legal integer) and calling it a number. The control that
// localises it is in the same module — the identical question asked of `f()` directly prints `True`,
// because ADR 0427's verdict reaches that far and no further.
//
// Why one call deeper was not covered: `answerArmOf` admits a `return other(...)` arm only once OTHER
// is CARRIED, which is **ADR 0368**'s fixed-point rule so an inner frame writes its kind word before an
// outer caller reads it. A void-only callee is exactly the callee that rule skips — it is served by the
// PAIR road, never carried — so the forwarding body's arm declined, `answerBodyPlan` declines a body
// with any un-nameable arm, and `g` kept the roads this whole family was filed for.
//
// The fix is in the arm, not the fold: a void-only callee has NO word for the outer frame to wait on
// (every road of it answers `None`), so such an arm answers the CONSTANT void word instead of
// forwarding one, and is asked before the carried-only rule that would have declined it. The call is
// still evaluated — the arm is a kind, not a substitution — so a forwarding body that prints still
// prints.
//
// Rows are judged on BOTH witness legs (**ADR 0302**): this compiler's module, and the answer
// `testdata/interpreter-golden.json` holds for that exact source, where a MISSING record fails the
// case. The controls are as load-bearing as the fixes: forwarding a NUMBER must not borrow the void,
// and a body whose other arm answers a number is a run-time fact this door still has no word for.

import (
	"regexp"
	"strings"
	"testing"
)

// voidForwardFamily is the answered family: one void-answering body, reached through a second and a
// third body, asked every question ADR 0427 asked of the body itself.
func voidForwardFamily() []struct {
	name string
	src  string
	want string
} {
	const row = "def f():\n    return None\ndef g():\n    return f()\n"
	return []struct {
		name string
		src  string
		want string
	}{
		// The row's own program, and the four spellings of the question.
		{"the identity question", row + "print(g() is None)\n", "True\n"},
		{"its equality twin", row + "print(g() == None)\n", "True\n"},
		{"the negated equality", row + "print(g() != None)\n", "False\n"},
		{"the negated identity", row + "print(g() is not None)\n", "False\n"},
		// The void written on either side of the question.
		{"the void written first (equality)", row + "print(None == g())\n", "True\n"},
		{"the void written first (identity)", row + "print(None is g())\n", "True\n"},
		// The positions the forwarded answer is read at.
		{"printed", row + "print(g())\n", "None\n"},
		{"rendered by str and repr", row + "print(str(g()), repr(g()))\n", "None None\n"},
		{"in a list, a dict and a truth test", row + "print([g()], {1: g()}, bool(g()))\n", "[None] {1: None} False\n"},
		{"membership in the list that holds it", row + "print(g() in [None])\n", "True\n"},
		{"bound, then asked", row + "r = g()\nprint(r is None, r)\n", "True None\n"},
		// The collisions this row exists for: the forwarded void's word is still `0`.
		{"the forwarded void against zero, False and one", row + "print(g() == 0, g() == False, g() == 1)\n", "False False False\n"},
		// One frame deeper still, and one deeper than that.
		{"forwarded twice", row + "def h():\n    return g()\nprint(h() is None, h(), bool(h()))\n", "True None False\n"},
		{"forwarded three times", row + "def h():\n    return g()\ndef i():\n    return h()\nprint(i() is None)\n", "True\n"},
		// The forwarding body has a parameter of its own.
		{"a forwarding body that takes a parameter", "def f():\n    return None\ndef g(x):\n    return f()\nprint(g(1) is None, g(2) == None)\n", "True True\n"},
		// A forwarding body that ALSO falls off its end answers the void on both roads.
		{
			"a forwarding body that also falls off its end",
			"def f():\n    return None\ndef g(x):\n    if x:\n        return f()\nprint(g(True), g(False), g(True) is None, g(False) is None)\n",
			"None None True True\n",
		},
		// The callee's own side effect, and the forwarder's, both survive a folded comparison.
		{
			"the callee's side effect survives the fold",
			"def f():\n    print(\"inner\")\n    return None\ndef g():\n    return f()\nr = g() == None\nprint(r)\n",
			"inner\nTrue\n",
		},
		{
			"the forwarder's own side effect survives the fold",
			"def f():\n    return None\ndef g():\n    print(\"side\")\n    return f()\nr = g() == None\nprint(r)\n",
			"side\nTrue\n",
		},
		// Two shapes the parent REFUSED outright and the reference prints: the callee is a `lambda`
		// bound to a name, and the void is reached through a mutual pair.
		{"a lambda-bound void callee, forwarded", "f = lambda: None\ndef g():\n    return f()\nprint(g() is None)\n", "True\n"},
		{"a mutual pair bottoming out in the void", "def a():\n    return b()\ndef b():\n    return None\nprint(a() is None)\n", "True\n"},
		// The controls: a forwarded NUMBER, TEXT, VERDICT and CONTAINER keep their own roads. Reading
		// any of these as the void would print `None` for the value the program answers.
		{"a forwarded number keeps its answer", "def f():\n    return 1\ndef g():\n    return f()\nprint(g(), g() == None, g() == 1)\n", "1 False True\n"},
		{"a forwarded text keeps its answer", "def f():\n    return \"s\"\ndef g():\n    return f()\nprint(g(), g() == None, g() == \"s\")\n", "s False True\n"},
		{"a forwarded verdict keeps its answer", "def f():\n    return True\ndef g():\n    return f()\nprint(g(), g() is None, bool(g()))\n", "True False True\n"},
		{"a forwarded container keeps its answer", "def f():\n    return [1, 2]\ndef g():\n    return f()\nprint(g())\n", "[1, 2]\n"},
	}
}

// TestTheForwardedVoidAnswerAnswersEveryQuestion is the family on both witness legs.
func TestTheForwardedVoidAnswerAnswersEveryQuestion(t *testing.T) {
	t.Parallel()
	for _, tc := range voidForwardFamily() {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, err := compileAndRun(t, tc.src)
			if err != nil {
				t.Fatalf("refused a program the reference prints %q: %v\n%s", tc.want, err, tc.src)
			}
			if got != tc.want {
				t.Fatalf("printed %q, want %q\n%s", got, tc.want, tc.src)
			}
			if !HasGoldenAnswer(tc.src) {
				t.Fatalf("%q is not on the record — record it from the reference with tools/recmerge\n%s", tc.src, tc.src)
			}
			recorded, err := runGoldenStdout(t, tc.src)
			if err != nil {
				t.Fatalf("the record leg could not answer this source: %v\n%s", err, tc.src)
			}
			if recorded != tc.want {
				t.Fatalf("the record reads %q for this source and the compiled leg printed %q — one of them is "+
					"stale against the reference\n%s", recorded, got, tc.src)
			}
		})
	}
}

// TestTheForwardedVoidAnswerHoldsInEveryPosition walks the places a comparison may stand. Each has a
// road of its own (**Gap R.449** is the proof that roads can disagree), so the family's module-level
// answers are not evidence for them.
func TestTheForwardedVoidAnswerHoldsInEveryPosition(t *testing.T) {
	t.Parallel()
	const row = "def f():\n    return None\ndef g():\n    return f()\n"
	for _, tc := range []struct{ name, src, want string }{
		{"an if head", row + "if g() is None:\n    print(\"y\")\nelse:\n    print(\"n\")\n", "y\n"},
		{"a negated if head", row + "if g() != None:\n    print(\"y\")\nelse:\n    print(\"n\")\n", "n\n"},
		{"a while head", row + "while g() is None:\n    print(\"once\")\n    break\n", "once\n"},
		{"a short-circuit's right operand", row + "print(g() is None and \"t\")\n", "t\n"},
		{"a ternary test", row + "print(1 if g() is None else 0)\n", "1\n"},
		{"a loop element", row + "for v in [g()]:\n    print(v is None)\n", "True\n"},
		{"the answer used twice", row + "print(g() == None, g() == None)\n", "True True\n"},
		{"printed and compared on one line", row + "print(g(), g() == None, g() is None)\n", "None True True\n"},
		// The controls, in the same positions: a forwarded number asked the void's question keeps its
		// own answer, and gets it through its own road, not through the void fold.
		{"a forwarded number in an if head", "def f():\n    return 1\ndef g():\n    return f()\nif g() == None:\n    print(\"y\")\nelse:\n    print(\"n\")\n", "n\n"},
		{"a forwarded verdict asked for its truth", "def f():\n    return True\ndef g():\n    return f()\nif g():\n    print(\"yes\")\nelse:\n    print(\"no\")\n", "yes\n"},
	} {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			res, err := JIT(tc.src, 0)
			if err != nil {
				t.Fatalf("refused a program the reference prints %q: %v\n%s", tc.want, err, tc.src)
			}
			if res.Code != 0 {
				t.Fatalf("exited %d with stderr %q\n%s", res.Code, res.Stderr, tc.src)
			}
			if res.Output != tc.want {
				t.Fatalf("printed %q, want %q\n%s", res.Output, tc.want, tc.src)
			}
		})
	}
}

// TestTheForwardedVoidAnswerIsThePlanAndNotTheSource asks the plan DIRECTLY, over a parsed program, in
// both directions. The end-to-end rows cannot tell "the plan forwarded the verdict" from "the body
// happened to print None"; these can, and they are where a widening beyond a VOID-only callee fails.
func TestTheForwardedVoidAnswerIsThePlanAndNotTheSource(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name        string
		src         string
		fn          string
		wantCarried bool
		wantVoid    bool
	}{
		{
			// The row's own body: forwarding a void-answering callee is itself a void answer.
			name: "a body forwarding the void",
			src:  "def f():\n    return None\ndef g():\n    return f()\nprint(1)\n",
			fn:   "g", wantCarried: true, wantVoid: true,
		},
		{
			name: "the void-answering callee itself",
			src:  "def f():\n    return None\ndef g():\n    return f()\nprint(1)\n",
			fn:   "f", wantCarried: true, wantVoid: true,
		},
		{
			name: "a body forwarded twice",
			src:  "def f():\n    return None\ndef g():\n    return f()\ndef h():\n    return g()\nprint(1)\n",
			fn:   "h", wantCarried: true, wantVoid: true,
		},
		{
			name: "a mutual pair bottoming out in the void",
			src:  "def a():\n    return b()\ndef b():\n    return None\nprint(1)\n",
			fn:   "a", wantCarried: true, wantVoid: true,
		},
		{
			// The controls. A forwarded number, text or verdict is NOT the void, and a body whose arms
			// answer the void in one road and a number in another is a run-time fact (**Gap R.324**): no
			// road may call the body a void.
			name: "a body forwarding a number",
			src:  "def f():\n    return 1\ndef g():\n    return f()\nprint(1)\n",
			fn:   "g", wantCarried: false, wantVoid: false,
		},
		{
			name: "a body forwarding a text",
			src:  "def f():\n    return \"s\"\ndef g():\n    return f()\nprint(1)\n",
			fn:   "g", wantCarried: false, wantVoid: false,
		},
		{
			name: "a body whose other arm answers a number",
			src:  "def f():\n    return None\ndef g(x):\n    if x:\n        return f()\n    return 1\nprint(1)\n",
			fn:   "g", wantCarried: true, wantVoid: false,
		},
	} {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			prog, err := Parse(tc.src)
			if err != nil {
				t.Fatalf("parse: %v", err)
			}
			p := answerKindPlanOf(prog, map[string]bool{})
			if got := p.carries(tc.fn); got != tc.wantCarried {
				t.Fatalf("carries(%s) = %v, want %v\n%s", tc.fn, got, tc.wantCarried, tc.src)
			}
			if got := p.answersOnlyTheVoid(tc.fn); got != tc.wantVoid {
				t.Fatalf("answersOnlyTheVoid(%s) = %v, want %v\n%s", tc.fn, got, tc.wantVoid, tc.src)
			}
			// The two halves of the invariant the fold depends on: a body the plan calls void-only is
			// carried (it has a word to answer with), and a body that is not carried cannot be void.
			if p.answersOnlyTheVoid(tc.fn) && !p.carries(tc.fn) {
				t.Fatalf("%s is void-only but not carried — a road that folds from the source would read "+
					"an answer the module never stores\n%s", tc.fn, tc.src)
			}
		})
	}

	// A void-only callee the PAIR road serves (which is the shape this row is about: it is never
	// carried, so the carried-only rule would decline the forwarding arm) answers the same way, and the
	// row's claim is that the ONE arm is asked before that rule rather than after it.
	t.Run("a void callee the pair road serves", func(t *testing.T) {
		t.Parallel()
		src := "def f():\n    return None\ndef g():\n    return f()\nprint(1)\n"
		prog, err := Parse(src)
		if err != nil {
			t.Fatalf("parse: %v", err)
		}
		p := answerKindPlanOf(prog, map[string]bool{"f": true})
		if !p.answersOnlyTheVoid("g") {
			t.Fatalf("with f served by the pair road, g is not read as answering the void\n%s", src)
		}
	})

	// A body that answers a call to ITSELF keeps the exclusion **ADR 0273** made of the pair road, even
	// when the recursion bottoms out in the void: the inner frame would be the outer caller.
	t.Run("a body answering a call to itself", func(t *testing.T) {
		t.Parallel()
		src := "def f():\n    return f()\nprint(1)\n"
		prog, err := Parse(src)
		if err != nil {
			t.Fatalf("parse: %v", err)
		}
		p := answerKindPlanOf(prog, map[string]bool{})
		if p.carries("f") || p.answersOnlyTheVoid("f") {
			t.Fatalf("a self-answering body was read as a void answer, which is an infinite regress wearing "+
				"the row's word\n%s", src)
		}
	})
}

// TestWhatTheForwardedVoidAnswerEmitsAndWhatItNeverEmits is the module half. The forwarded answer is a
// CONSTANT the fold reads from the plan, so the witness is threefold: the forwarder still CALLS the
// callee (a fold or an arm that dropped it would lose the program's effects), the forwarder stores the
// void's own word beside its answer, and no comparison door is reached for a question the source can
// name. The control row is the other direction: a body forwarding a number stores no answer word at all
// and keeps its ordinary return.
func TestWhatTheForwardedVoidAnswerEmitsAndWhatItNeverEmits(t *testing.T) {
	t.Parallel()
	call := func(name string) *regexp.Regexp { return regexp.MustCompile(`call i32 @` + name + `\(\)`) }

	t.Run("the forwarder calls its callee and stores the void's word", func(t *testing.T) {
		t.Parallel()
		src := "def f():\n    print(\"inner\")\n    return None\ndef g():\n    return f()\nprint(g() is None)\n"
		res, err := JIT(src, 0)
		if err != nil {
			t.Fatalf("the module would not build: %v\n%s", err, src)
		}
		gBody := runtimeFnBody(res.IR, "define i32 @gy_g()")
		if gBody == "" {
			t.Fatalf("the module carries no @gy_g:\n%s", res.IR)
		}
		if got := len(call("gy_f").FindAllString(gBody, -1)); got != 1 {
			t.Fatalf("@gy_g calls @gy_f %d time(s), want exactly 1 — the arm is a kind, not a substitution, "+
				"and a forwarder that stopped calling would drop the callee's effects\n%s\n%s", got, gBody, src)
		}
		if !strings.Contains(gBody, `store i32 3, i32* @gy_g.anst`) {
			t.Fatalf("@gy_g does not store the void's own word beside its answer — the plan says the void and "+
				"the module says nothing:\n%s\n%s", gBody, src)
		}
		main := runtimeFnBody(res.IR, "define i32 @main()")
		if got := len(call("gy_g").FindAllString(main, -1)); got == 0 {
			t.Fatalf("@main never calls @gy_g; the fold may answer from the source but it may not delete the "+
				"call\n%s\n%s", main, src)
		}
		for _, banned := range []string{`@rt_payload_eq`, `@rt_pair_truth`, `@rt_icmp_values`} {
			if strings.Contains(main, banned) {
				t.Fatalf("%s is in @main — the forwarded void is a question the compiler can name, and it went "+
					"to a run-time door anyway\n%s\n%s", banned, main, src)
			}
		}
	})

	t.Run("a folded comparison keeps the whole call chain", func(t *testing.T) {
		t.Parallel()
		src := "def f():\n    print(\"inner\")\n    return None\ndef g():\n    return f()\nr = g() == None\nprint(r)\n"
		res, err := JIT(src, 0)
		if err != nil {
			t.Fatalf("the module would not build: %v\n%s", err, src)
		}
		main := runtimeFnBody(res.IR, "define i32 @main()")
		if got := len(call("gy_g").FindAllString(main, -1)); got != 1 {
			t.Fatalf("@main calls @gy_g %d time(s), want 1 — the answer folded to a constant and the call went "+
				"with it (**ADR 0172**)\n%s\n%s", got, main, src)
		}
		if res.Output != "inner\nTrue\n" {
			t.Fatalf("printed %q, want %q\n%s", res.Output, "inner\nTrue\n", src)
		}
	})

	t.Run("a body forwarding a number stores no answer word", func(t *testing.T) {
		t.Parallel()
		src := "def f():\n    return 1\ndef g():\n    return f()\nprint(g() == 1)\n"
		res, err := JIT(src, 0)
		if err != nil {
			t.Fatalf("the module would not build: %v\n%s", err, src)
		}
		gBody := runtimeFnBody(res.IR, "define i32 @gy_g()")
		if gBody == "" {
			t.Fatalf("the module carries no @gy_g:\n%s", res.IR)
		}
		if strings.Contains(gBody, `@gy_g.anst`) {
			t.Fatalf("a body forwarding a NUMBER stores an answer kind — the void arm reached past a void-only "+
				"callee:\n%s\n%s", gBody, src)
		}
		if !strings.Contains(gBody, "ret i32 %t") {
			t.Fatalf("the forwarding body no longer returns its callee's word:\n%s\n%s", gBody, src)
		}
	})
}

// TestWhatTheForwardedVoidAnswerStillOwesIsFiledNotFixed pins what this row leaves owed, with today's
// bytes beside the reference's. Every one is byte-identical on this row's parent (measured with a
// `git worktree` build of it), which is the sentence that makes them other rows' lesions rather than
// this commit's damage. An answer that arrives here FAILS the case.
func TestWhatTheForwardedVoidAnswerStillOwesIsFiledNotFixed(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name       string
		src        string
		wantExit   int
		wantOut    string
		wantStderr string
		dossier    string
	}{
		// The pin that stood here — `def f(x):` / `    if x:` / `        return None` / `    return 1` /
		// `def g():` / `    return f(True)` / `print(g(), g() is None)`, held at `None False` beside the
		// reference's `None True` — is DELETED rather than edited: **ADR 0430** gives the comparison road the
		// run-time word **Gap R.324** was owed for it, so the forwarded word IS whichever arm of `f` ran. The
		// program prints the reference's bytes and stands in the answered family above, on the compiled leg,
		// the record leg AND the reference.
		{
			// The callee forwards its own PARAMETER, whose kind is the caller's, not the source's.
			name:     "a body forwarding a call that hands back a parameter",
			src:      "def f(v):\n    return v\ndef g():\n    return f(None)\nprint(g() is None, g())\n",
			wantExit: 0, wantOut: "False 0\n",
			dossier: "**Gap R.417** / **Gap R.171** (standing) — the reference prints `True None`; a parameter " +
				"crosses a frame as one word, and this is that collision one body later",
		},
		// The pin that stood here — `def f():` / `    return None` above `class C():` /
		// `    def m(self):` / `        return f()` / `print(c.m() is None)`, held at `False` beside the
		// reference's `True` — is DELETED rather than edited: **ADR 0429** reads a method's body with the same
		// `answerBodyPlan` the module road asks, so the forwarding arm reaches through a class the way **ADR
		// 0428** made it reach through a second `def`. The program prints `True`, and stands in the answered
		// family of `pkg/lang/void_method_test.go`, on the compiled leg AND the record leg.
		{
			// A refusal, pinned AS one: exit 1, exit 2 forbidden (**ADR 0166**). The body reaches for a
			// module-level container, which is a different door's standing problem.
			name:       "a forwarder whose callee touches a module-level container",
			src:        "xs = []\ndef g():\n    return xs.clear()\nprint(g() is None, xs)\n",
			wantExit:   1,
			wantStderr: "a compiled function body cannot reach module-level containers",
			dossier:    "**Gap R.35** (standing) — the same refusal this toolchain has always spent here",
		},
	} {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			res, err := JIT(tc.src, 0)
			if err != nil {
				if tc.wantExit != 1 || !strings.Contains(err.Error(), tc.wantStderr) {
					t.Fatalf("%s (%s): failed in a way this pin does not describe: %v", tc.name, tc.dossier, err)
				}
				return
			}
			if res.Code != tc.wantExit {
				t.Fatalf("%s (%s): exited %d with stdout %q stderr %q — an answer that arrived here means the "+
					"row was closed and this table did not follow\n%s",
					tc.name, tc.dossier, res.Code, res.Output, res.Stderr, tc.src)
			}
			if tc.wantOut != "" && res.Output != tc.wantOut {
				t.Fatalf("%s (%s): printed %q, want %q\n%s", tc.name, tc.dossier, res.Output, tc.wantOut, tc.src)
			}
			if tc.wantStderr != "" && !strings.Contains(res.Stderr+res.Output, tc.wantStderr) {
				t.Fatalf("%s (%s): stderr %q does not carry %q", tc.name, tc.dossier, res.Stderr, tc.wantStderr)
			}
		})
	}
}
