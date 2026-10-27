package lang

// ADR 0427 · roadmap Gap R.388 — a body that answers the void answers it wherever its call is read.
//
// `def f():` / `    return None` is the void, and this backend has always been able to SAY that when it
// prints: `print(f())` writes `None`. The same value asked the next question was answered by a different
// road — the `is`/`==` fold, which decides "from what the SOURCE says each side is" — and that road asked
// whether the body has a `return` at all. A body that falls off its end has none and was read as the void
// (`f() is None` printed `True`); a body that WRITES `return None` has one, so the fold called it a
// non-void and compared its payload — the word `0` — against nothing, and printed `False` at the exit
// code of success:
//
//	def f():
//	    return None
//	print(f())          # None   ← the print road, via isNoneExpr's fall-off-the-end answer
//	print(f() is None)  # False  ← the fold; the reference prints True
//	print(f() == None)  # False  ← the same fold; the reference prints True
//
// One predicate owns this question — `isNoneExpr`, the same one `print`, `str()` and the REPL consult —
// and it now asks **ADR 0368**'s answer plan (`answersOnlyTheVoid`), which is grown for the pair-served
// bodies too (**Gap R.228**'s Definition of done reads "`print(f())` writes `None` **and `f() is None` is
// `True`**"; the first half held for both spellings, the second only for one, and no test had asked).
//
// This file is that suite. Every answered row is judged on BOTH witness legs — the module this compiler
// built and ran, and the answer `testdata/interpreter-golden.json` holds for that exact source, where a
// MISSING record fails the case (**ADR 0302**). The controls are as load-bearing as the fixes: a body
// whose arms answer `None` in one branch and `1` in another is NOT a void body, and reading it as one
// would print `None` for the arm that answered `1`.

import (
	"regexp"
	"strings"
	"testing"
)

// voidAnswerFamily is the answered family: every question the row's own program may be asked, in the
// spelling the reference's docs use. `want` is what BOTH legs give — the compiled leg is run here, the
// record leg is consulted by the case below, and the reference is asked in the CLI suite.
func voidAnswerFamily() []struct {
	name string
	src  string
	want string
} {
	return []struct {
		name string
		src  string
		want string
	}{
		// The row's own program, and its four spellings.
		{"the identity question", "def f():\n    return None\nprint(f() is None)\n", "True\n"},
		{"its equality twin", "def f():\n    return None\nprint(f() == None)\n", "True\n"},
		{"the negated equality", "def f():\n    return None\nprint(f() != None)\n", "False\n"},
		{"the negated identity", "def f():\n    return None\nprint(f() is not None)\n", "False\n"},
		// The void on the left: the fold reads whichever side is the written None, and the answer may not
		// depend on which side the program happened to write it on.
		{"the void written first (equality)", "def f():\n    return None\nprint(None == f())\n", "True\n"},
		{"the void written first (identity)", "def f():\n    return None\nprint(None is f())\n", "True\n"},
		// The same body written the way the pass sees it: a lambda is hoisted to a `def`, and the answer
		// must not depend on which spelling the program typed (**Gap R.228**'s DoD names both twins).
		{"the applied lambda's identity", "print((lambda: None)() is None)\n", "True\n"},
		{"the applied lambda's equality", "print((lambda: None)() == None)\n", "True\n"},
		{"a lambda bound to a name", "g = lambda: None\nprint(g() == None)\n", "True\n"},
		// The collisions this row exists for: the void's payload is `0`, and the numbers `0` and `False`
		// legally carry it. Answering the void may not turn those into members.
		{"the void answer against zero", "def f():\n    return None\nprint(f() == 0)\n", "False\n"},
		{"the void answer against False", "def f():\n    return None\nprint(f() == False)\n", "False\n"},
		{"the void answer against one", "def f():\n    return None\nprint(f() == 1)\n", "False\n"},
		// The positions the answer is read at. Each of these runs the call, so the fold may not drop it.
		{"printed", "def f():\n    return None\nprint(f())\n", "None\n"},
		{"rendered by str and repr", "def f():\n    return None\nprint(str(f()), repr(f()))\n", "None None\n"},
		{"in a list literal", "def f():\n    return None\nprint([f()])\n", "[None]\n"},
		{"in a dict value", "def f():\n    return None\nprint({\"k\": f()})\n", "{'k': None}\n"},
		{"asked for its truth", "def f():\n    return None\nprint(bool(f()))\n", "False\n"},
		{"against itself", "def f():\n    return None\nprint(f() == f())\n", "True\n"},
		{"of a body that takes a parameter", "def f(x):\n    return None\nprint(f(1) is None)\n", "True\n"},
		{"membership in the list that holds a void", "def f():\n    return None\nprint(f() in [None])\n", "True\n"},
		{"bound, then asked", "def f():\n    return None\nr = f()\nprint(r is None, r)\n", "True None\n"},
		// The body with no `return` at all — the spelling that was always right, kept here so the two
		// spellings cannot drift apart again.
		{"the body that falls off its end", "def f():\n    pass\nprint(f() is None)\n", "True\n"},
		// The in-place mutation neighbour: `append` answers the void by SHAPE, and this row must not
		// disturb how that is recognised (**Gap R.187**, **ADR 0300**).
		{"an in-place method's void", "xs = [1]\nprint(xs.append(2) is None)\n", "True\n"},
	}
}

// TestTheVoidAnswerAnswersEveryQuestion is the family on both witness legs.
func TestTheVoidAnswerAnswersEveryQuestion(t *testing.T) {
	t.Parallel()
	for _, tc := range voidAnswerFamily() {
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

// TestTheVoidAnswerHoldsInEveryPosition walks the places a comparison may stand: an `if` head, a `while`
// head, a short-circuit's operand, a ternary, a loop element, and a binding the program reads twice. The
// fold these answers come from is a road of its own per position (**Gap R.449** is the proof that the
// roads can disagree), so the family's module-level answers are not evidence for them.
func TestTheVoidAnswerHoldsInEveryPosition(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct{ name, src, want string }{
		{"an if head", "def f():\n    return None\nif f() is None:\n    print(\"y\")\nelse:\n    print(\"n\")\n", "y\n"},
		{"a negated if head", "def f():\n    return None\nif f() != None:\n    print(\"y\")\nelse:\n    print(\"n\")\n", "n\n"},
		{"a while head", "def f():\n    return None\nwhile f() is None:\n    print(\"once\")\n    break\n", "once\n"},
		{"a short-circuit's right operand", "def f():\n    return None\nprint(f() is None and \"t\")\n", "t\n"},
		{"a short-circuit that chooses the void", "def f():\n    return None\nprint(f() is None or \"t\")\n", "True\n"},
		{"a ternary test", "def f():\n    return None\nprint(1 if f() is None else 0)\n", "1\n"},
		{"a loop element", "def f():\n    return None\nfor v in [f()]:\n    print(v is None)\n", "True\n"},
		// The call runs for its effects even where its answer folds to a constant: `r = f() == None` is
		// decided by the source, and the statement still calls f (**ADR 0172**'s "both operands are still
		// evaluated", which is what makes a fold different from a deletion).
		{"a folded answer whose call prints", "def f():\n    print(\"side\")\n    return None\nr = f() == None\nprint(r)\n", "side\nTrue\n"},
		{"the answer used twice", "def f():\n    return None\nprint(f() == None, f() == None)\n", "True True\n"},
		// The controls: a body whose arms answer two kinds is NOT a void body, and a body that answers a
		// number is not one either. Reading either as the void would print `None` for the arm that
		// answered `1` — the defect **ADR 0368** was written to stop.
		{"a two-arm body prints both arms", "def f(x):\n    if x:\n        return None\n    return 1\nprint(f(True), f(False))\n", "None 1\n"},
		{"a body that answers a number", "def f():\n    return 1\nprint(f() == None)\n", "False\n"},
		{"a body that answers a number, asked its own question", "def f():\n    return 1\nprint(f() == 1)\n", "True\n"},
		{"a body that answers a text", "def f():\n    return \"s\"\nprint(f() == None)\n", "False\n"},
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

// TestTheVoidAnswerPrintsAndComparesAsOneValue is the ADR 0232 claim in this row's shape: the printer and
// the fold may not give one call two answers. Each line asks both from the same body copy.
func TestTheVoidAnswerPrintsAndComparesAsOneValue(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct{ name, src, want string }{
		{"printed and compared on one line", "def f():\n    return None\nprint(f(), f() == None, f() is None)\n", "None True True\n"},
		{"printed and negated", "def f():\n    return None\nprint(f(), f() != None, f() is not None)\n", "None False False\n"},
		{"the fall-off-the-end twin", "def f():\n    pass\nprint(f(), f() == None)\n", "None True\n"},
		{"a number body keeps its answer", "def f():\n    return 2\nprint(f(), f() == None)\n", "2 False\n"},
	} {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, err := compileAndRun(t, tc.src)
			if err != nil {
				t.Fatalf("refused: %v\n%s", err, tc.src)
			}
			if got != tc.want {
				t.Fatalf("printed %q, want %q — the print road and the fold gave one call two answers\n%s",
					got, tc.want, tc.src)
			}
		})
	}
}

// TestTheVoidAnswerIsAskedOfThePlanAndNotOfTheSource asks the one predicate this row changed, DIRECTLY,
// over a parsed program. The end-to-end rows above cannot tell "the plan answered" from "the body happened
// to look like a void"; this can, and it is where a widening beyond `answersOnlyTheVoid` fails.
func TestTheVoidAnswerIsAskedOfThePlanAndNotOfTheSource(t *testing.T) {
	t.Parallel()
	const src = `def only():
    return None
def fallOff():
    pass
def number():
    return 1
def varied(x):
    if x:
        return None
    return 1
print(only())
`
	prog, err := Parse(src)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	g := &irGen{answer: answerKindPlanOf(prog, nil), fds: map[string]*FuncDef{}, pairRetDone: map[string]bool{}}
	for _, st := range prog.Stmts {
		if fd, ok := st.(*FuncDef); ok {
			g.fds[fd.Name] = fd
		}
	}
	call := func(name string) Expr { return &Call{Fn: &Name{Value: name}} }

	// The two spellings of a void body, and the two bodies that answer something else. The varied one is
	// the row's real safety: its arms are None AND a number, so no road may call the body a void.
	for _, tc := range []struct {
		name string
		fn   string
		want bool
	}{
		{"a body that writes return None", "only", true},
		{"a body that falls off its end", "fallOff", true},
		{"a body that answers a number", "number", false},
		{"a body whose arms answer two kinds", "varied", false},
	} {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := g.isNoneExpr(call(tc.fn))
			if got != tc.want {
				t.Fatalf("isNoneExpr(%s()) = %v, want %v — the answer comes from the plan's void verdict, "+
					"and a body that answers a value must never be read as the void (ADR 0368)", tc.fn, got, tc.want)
			}
			// Two roads may answer this predicate, and the row's claim is that neither of them guesses:
			// the plan's void verdict (**ADR 0368**, what this row grew) and the older answer for a body
			// that has no `return` at all (`fdReturnsValue`). A body that RETURNED a value may never be
			// read as the void by either.
			if plan := g.answer.answersOnlyTheVoid(tc.fn); plan && !got {
				t.Fatalf("the plan says %s() answers only the void and the predicate says it does not — one "+
					"predicate, one answer (ADR 0232)", tc.fn)
			}
			if got && !g.answer.answersOnlyTheVoid(tc.fn) && fdReturnsValue(g.fds[tc.fn]) {
				t.Fatalf("the predicate read %s() as the void although its body returns a value and the plan "+
					"says nothing of the kind — neither road may guess (ADR 0368)", tc.fn)
			}
		})
	}

	t.Run("a name no program defined", func(t *testing.T) {
		t.Parallel()
		if g.isNoneExpr(&Call{Fn: &Name{Value: "nowhere"}}) {
			t.Fatal("an undefined callee was read as the void")
		}
	})
	t.Run("a written None is still the void", func(t *testing.T) {
		t.Parallel()
		if !g.isNoneExpr(&NoneLit{}) {
			t.Fatal("NoneLit stopped answering")
		}
	})
	t.Run("a name bound to the void", func(t *testing.T) {
		t.Parallel()
		g2 := &irGen{answer: g.answer, fds: g.fds, noneVars: map[string]bool{"v": true}}
		if !g2.isNoneExpr(&Name{Value: "v"}) {
			t.Fatal("a void binding stopped answering — the older half of this predicate must not move")
		}
	})
}

// TestWhatTheVoidAnswerEmitsAndWhatItNeverEmits is the module half. The fold answers these questions with
// a CONSTANT, so the witness is threefold: the call is still emitted (a fold that dropped it would print
// `True` for a body whose `return` prints), no comparison door is reached for it, and the kind word the
// plan depends on is actually stored by the callee.
func TestWhatTheVoidAnswerEmitsAndWhatItNeverEmits(t *testing.T) {
	t.Parallel()
	callOnce := regexp.MustCompile(`call i32 @gy_f\(\)`)
	for _, tc := range []struct {
		name       string
		src        string
		wantReg    []string
		wantAbsent []string
		calls      int
	}{
		{
			name:    "the equality folds, and the call still runs",
			src:     "def f():\n    print(\"side\")\n    return None\nr = f() == None\nprint(r)\n",
			calls:   1,
			wantReg: []string{`store i32 1, i32\* %_r`, `call i32 @gy_f\(\)`, `@rt_print_bool`},
			// The fold may not reach a run-time equality door for a value it can name from the source —
			// that road is Gap R.449's, and a constant here proves the fold was reached at all.
			wantAbsent: []string{`@rt_payload_eq`, `@rt_pair_truth`},
		},
		{
			name:    "the identity folds in an if head, where the callee keeps its tag",
			src:     "def f():\n    return None\nif f() == None:\n    print(\"y\")\nelse:\n    print(\"n\")\n",
			calls:   1,
			wantReg: []string{`store i32 3, i32\* @gy_f\.anst`, `call i32 @gy_f\(\)`},
			wantAbsent: []string{
				// The void is a singleton, so the fold may not send this to the tagged ORDER door
				// (@rt_icmp_values is spelled with an s), nor invent a helper for it.
				`@rt_icmp_values`, `@gy_call_eq`,
			},
		},
		{
			name:    "the number body keeps its own road",
			src:     "def f():\n    return 1\nprint(f() == None)\n",
			calls:   1,
			wantReg: []string{`call i32 @gy_f\(\)`},
			// Answering `False` for a number body is the control's claim, and it must not arrive through a
			// pair door either: the fold knows the body answers an int.
			wantAbsent: []string{`@rt_payload_eq`},
		},
	} {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			res, err := JIT(tc.src, 0)
			if err != nil {
				t.Fatalf("the module would not build: %v\n%s", err, tc.src)
			}
			main := runtimeFnBody(res.IR, "define i32 @main()")
			for _, pat := range tc.wantReg {
				if !regexp.MustCompile(pat).MatchString(main) && !regexp.MustCompile(pat).MatchString(res.IR) {
					t.Fatalf("%s is absent from the module:\n%s\n%s", pat, main, tc.src)
				}
			}
			for _, pat := range tc.wantAbsent {
				if regexp.MustCompile(pat).MatchString(main) {
					t.Fatalf("%s must not be in main — the fold decides this answer from the source:\n%s\n%s", pat, main, tc.src)
				}
			}
			if tc.calls >= 0 {
				if got := len(callOnce.FindAllString(main, -1)); got != tc.calls {
					t.Fatalf("main calls @gy_f %d time(s), want %d — a fold that drops the call would keep the "+
						"answer and lose the program's effects:\n%s\n%s", got, tc.calls, main, tc.src)
				}
			}
		})
	}
}

// TestWhatTheVoidAnswerStillOwesIsFiledNotFixed holds what this row leaves owed, pinned with today's bytes
// beside the reference's. Every one of them is byte-identical on this row's parent (`324f5c3`, **ADR
// 0426**), measured with a build of that commit in a `git worktree` — which is the sentence that makes them
// other rows' lesions rather than this commit's damage. An answer that arrives here FAILS the case.
func TestWhatTheVoidAnswerStillOwesIsFiledNotFixed(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name       string
		src        string
		wantExit   int
		wantOut    string
		wantStderr string
		dossier    string
	}{
		// The pin that stood here — `def f():` / `    return None` / `def g():` / `    return f()` /
		// `print(g() is None)`, held at `False` beside the reference's `True` — is DELETED rather than
		// edited: **ADR 0428** asks `answerArmOf` whether the callee answers only the void before the
		// carried-only rule declines the arm, and the program prints `True`. It stands in the answered
		// family of `pkg/lang/void_forward_test.go`, on the compiled leg AND the record leg.
		// The pin that stood here — `class C():` / `    def m(self):` / `        return None` / `c = C()` /
		// `print(c.m() is None)`, held at `False` beside the reference's `True` — is DELETED rather than
		// edited: **ADR 0429** gives the answer direction its METHOD round, so the one body reading
		// (`answerBodyPlan`) answers for a class's body the way it answers for a module `def`'s. The program
		// prints `True`, and stands in the answered family of `pkg/lang/void_method_test.go`, on the compiled
		// leg AND the record leg.
		{
			// A body whose arms answer None AND a number is a run-time fact, and this door has no word for
			// one: `comparePair` declines a carried call by design and the fold answers from the source.
			name:     "the arm that answered the void, in a body that also answers a number",
			src:      "def f(x):\n    if x:\n        return None\n    return 1\nprint(f(True) is None)\n",
			wantExit: 0, wantOut: "False\n",
			dossier: "**Gap R.324** (standing, named in `comparePair`'s own comment) — the reference prints `True`; " +
				"which arm ran is a run-time word and the comparison road still has no tagged answer word",
		},
		{
			// A refusal, pinned AS one: exit 1, exit 2 forbidden (**ADR 0166**). The reference answers
			// `False`; the compiled leg declines a container `pop` over a NAME (**Gap R.174**'s standing).
			name:       "a container pop over a name, asked against the void",
			src:        "d = {\"a\": 1}\nprint(d.pop(\"a\") is None)\n",
			wantExit:   1,
			wantStderr: "the compiled backend folds container methods only over a literal written at the call",
			dossier:    "**Gap R.174** (standing) — the same refusal this toolchain has always spent here",
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
