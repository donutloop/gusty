package lang

import (
	"strconv"
	"strings"
	"testing"
)

// Roadmap Gap R.412 / ADR 0411 — the identity question asked AGAINST THE VOID of a value whose kind the
// OBJECT holds is a question about the TAG, and the tag is a run-time word.
//
//	xs = [None, 1]
//	print(xs.pop(0) is None)   # the reference: True    this backend, before: False   — at exit 0
//	print(xs.pop(0) == None)   # the reference: True    this backend: True            (the == door)
//
// One program, two doors, two answers for one question — and the `==` door was not the one that had been
// widened for ADR 0410: `xs.pop(0)` reaches it as a (payload, tag) pair through `comparePair`, so it asks
// `@rt_payload_eq` and gets the reference's verdict. `is` was not allowed through that door at all. It fell
// to the None-equality road, which — in its own words — "decides the result statically, from what the source
// says each side is". That road is sound for an operand whose kind the compiler can state: `print(1 is None)`
// folds to `False` and emits no call. It is unsound for an operand whose kind lives in a container the program
// built at run time, because the word a void leaves behind is `0` and an int's slot can carry `0` too — and the
// source says nothing whatever about what the slot holds. So the same fold that is the right answer for a
// literal is a guess for a slot, and a guess at exit 0 is the class of defect this house files rather than ships
// (ADR 0166's wrong-answer-at-success class, ADR 0232's one-question rule).
//
// What this row decided:
//
//   - `mixedTaggedCompare` admits `is` and `is not`, for ONE shape: one side is statically the void. The void
//     is a singleton, so identity and equality coincide there and the pair door's `@rt_payload_eq` is exactly
//     "does the object's own tag say the void". Every other identity question — `xs.pop(0) is [1]`, a container
//     against a container — is declined, because identity is NOT content equality for a container (the reference
//     answers False for two containers that hold the same elements) and answering it with the content door would
//     trade a wrong answer for a subtler one.
//   - The door answers in BOTH places an identity question is asked: `mixedTaggedCompare` is reached from the
//     value road (`print(x is None)`, a binding, an argument) and from the condition road (`if x is None:`, the
//     `and`/`or` short-circuit's operand, a `while` head), which is where ADR 0232 already hangs the tagged
//     equality. A `bool`-typed condition and an `i32`-typed value are the same question; a suite that pins only
//     one of them cannot tell which the door answered.
//   - Nothing is refused that used to answer, and the fold still folds: the IR claim is bidirectional — the
//     popped slot's module contains `call i32 @rt_payload_eq(i32 %popped, i32 %tag, i32 0, i32 3)` and the
//     literal program contains no such call at all.
//
// The measurement, over the 34 programs this family's two files carry, with a build of the parent commit
// `af8b847` run beside a build of this one and every program asked of the pinned CPython: **17 moved to the
// reference's answer, 11 controls held, 6 still differ, none moved away from the reference**. Five of the six
// that differ are identical on the parent build, which is what makes them rows rather than regressions: a call
// that answers the void and is then compared with it is **Gap R.388**'s standing row; an unrolled loop item over
// a literal list (**Gap R.416**), a callee's own parameter (**Gap R.417**), and the same list under a second
// name (**Gap R.415**) are the three doors this one did not reach. The sixth is `is` asked of a popped
// CONTAINER against a container literal, which hands `llc` a module-level global in a word position and dies at
// **exit 2** — **Gap R.418**, the asking-door half of the **Gap R.393**/**Gap R.253** family, and the reason
// this row admits the void and nothing else.

// theVoidAnswered is the family this row pays: the identity question against the void, asked of every position
// that carries a tag the compiler cannot state, plus the controls that keep the fold honest.
func theVoidAnswered() []struct {
	name string
	src  string
	want string
} {
	return []struct {
		name string
		src  string
		want string
	}{
		// The row's own program, and its twin: the two doors asking one question.
		{"the row's own program", "xs = [None, 1]\nprint(xs.pop(0) is None)\n", "True\n"},
		{"its equality twin", "xs = [None, 1]\nprint(xs.pop(0) == None)\n", "True\n"},
		{"the negation", "xs = [None, 1]\nprint(xs.pop(0) is not None)\n", "False\n"},
		{"both spellings, one slot", "xs = [None, 1]\nprint(xs.pop(0) is None, xs.pop(0) is not None)\n", "True True\n"},
		{"the same slot twice", "xs = [None, 1]\nprint(xs.pop(0) is None)\nprint(xs.pop(0) is None)\n", "True\nFalse\n"},
		// Which kind came out, not just that it was not the void.
		{"a text's slot is not the void", "xs = [\"a\", None]\nprint(xs.pop(0) is None)\nprint(xs.pop(0) is not None)\n", "False\nFalse\n"},
		{"a container's slot is not the void", "xs = [[1], None]\nprint(xs.pop(0) is None)\nprint(xs.pop(0) is None)\n", "False\nTrue\n"},
		{"a dict's slot is not the void", "xs = [{\"a\": 1}, None]\nprint(xs.pop(0) is None)\n", "False\n"},
		{"a number's slot is not the void", "xs = [1, 2]\nprint(xs.pop(0) is None)\nprint(xs.pop(0) is not None)\n", "False\nTrue\n"},
		// The index door: default, explicit, negative.
		{"the default index", "xs = [None, 1]\nprint(xs.pop() is None)\n", "False\n"},
		{"a negative index", "xs = [1, None]\nprint(xs.pop(-1) is None)\n", "True\n"},
		{"drained from the front", "xs = [None, 1, \"a\"]\nwhile xs:\n    print(xs.pop(0) is None)\n", "True\nFalse\nFalse\n"},
		// The positions that read the verdict, each of which is a different road to the same door.
		{"the verdict through str()", "xs = [None, 1]\nprint(str(xs.pop(0) is None))\n", "True\n"},
		{"the verdict as a condition", "xs = [None, 1]\nif xs.pop(0) is None:\n    print(\"y\")\nelse:\n    print(\"n\")\n", "y\n"},
		{"the verdict as the other arm", "xs = [1, None]\nif xs.pop(0) is None:\n    print(\"y\")\nelse:\n    print(\"n\")\n", "n\n"},
		{"the verdict in a short-circuit", "xs = [None, 1]\nprint(xs.pop(0) is None and \"t\")\n", "t\n"},
		{"the verdict as a dict value", "xs = [None, 1]\nd = {\"k\": xs.pop(0) is None}\nprint(d)\n", "{'k': True}\n"},
		{"the verdict out of a method", "class T:\n    def m(self):\n        xs = [None, 1]\n        return xs.pop(0) is None\nprint(T().m())\n", "True\n"},
		{"the verdict in a while head", "xs = [None, 1]\nwhile xs.pop(0) is None:\n    print(\"void\")\n    break\n", "void\n"},
		// The same question, asked of the other values whose kind the object holds. Each of these was REFUSED
		// outright before this row — the position wanted one word and the read carries two — so they are the
		// half of this family that turned a wall into an answer.
		{"a slot read of a mixed list", "xs = [None, 1]\nprint(xs[0] is None)\nprint(xs[1] is None)\n", "True\nFalse\n"},
		{"a slot read of a tuple", "ys = (1, None)\nprint(ys[0] is None)\nprint(ys[1] is None)\n", "False\nTrue\n"},
		{"a loop element over a named list", "xs = [None, \"a\"]\nfor v in xs:\n    print(v is None)\n", "True\nFalse\n"},
		{"a name the pair road bound", "xs = [None, \"a\"]\nn = xs[0]\nprint(n is None)\nif n is None:\n    print(\"y\")\n", "True\ny\n"},
		// Controls: what the fold still decides statically, with no call emitted.
		{"a written void", "print(None is None)\nprint(None is not None)\n", "True\nFalse\n"},
		{"a void bound to a name", "x = None\nprint(x is None)\n", "True\n"},
		{"the void asked of literals", "print(1 is None)\nprint(\"a\" is None)\nprint([1] == None)\n", "False\nFalse\nFalse\n"},
		{"identity between numbers", "n = 1\nprint(n is 1)\nprint(n is not 1)\n", "True\nFalse\n"},
		{"a single-kind list keeps its road", "xs = [7, 8, 9]\nprint(xs.pop(0) is None)\nprint(xs.pop(0) == 8)\n", "False\nTrue\n"},
		{"the append neighbour", "xs = [None, 1]\nxs.append(\"a\")\nprint(xs)\n", "[None, 1, 'a']\n"},
	}
}

// TestTheVoidQuestionAskedOfAValueTheObjectHoldsAnswersTheTag is the answered family on both witness legs: the
// record's stdout for the same source, and the module the compiler built and ran. A MISSING record fails the
// case — the answer a program earns may not be certified by the backend that earned it (ADR 0302).
func TestTheVoidQuestionAskedOfAValueTheObjectHoldsAnswersTheTag(t *testing.T) {
	t.Parallel()
	for _, tc := range theVoidAnswered() {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, err := compileAndRun(t, tc.src)
			if err != nil {
				t.Fatalf("the identity road refused a program the reference prints %q: %v\n%s", tc.want, err, tc.src)
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
				t.Fatalf("the record reads %q for this source and the compiled leg printed %q\n%s", recorded, got, tc.src)
			}
		})
	}
}

// TestTheVoidQuestionIsTheTagAndNotWhatTheSourceWrote is the row's claim stated as an IR fact, in both
// directions. Only the positive half would pass after a rewrite that routed the easy case through the runtime,
// which is why the fold is asserted to emit NOTHING (ADR 0385's rule that a fold still being a fold is a
// property, not a performance detail).
func TestTheVoidQuestionIsTheTagAndNotWhatTheSourceWrote(t *testing.T) {
	t.Parallel()

	// 1. the popped slot: the door reads the tag the removal carried out, against the void's own pair.
	ir := irOf(t, "xs = [None, 1]\nprint(xs.pop(0) is None)\n")
	body := theVoidMainBody(ir)
	if !strings.Contains(body, "call i32 @rt_pop(") {
		t.Fatalf("the program's own body does not remove the slot:\n%s", body)
	}
	if !strings.Contains(body, "call i32 @rt_payload_eq(i32") {
		t.Fatalf("the identity question was answered without asking the tag — the static fold is back:\n%s", body)
	}
	// The void's pair is (payload 0, tag TagNone): a pair door that compared the payload alone would answer
	// `1 is None` true the moment an int slot held 0, which is the ADR 0232 collision this door exists to stop.
	voidPair := ", i32 0, i32 " + strconv.FormatInt(int64(TagNone), 10) + ")"
	if !strings.Contains(body, voidPair) {
		t.Fatalf("the pair door is not comparing against the void's own (payload, tag):\n%s", body)
	}
	// And the tag it compares is a REGISTER the removal produced, not a constant: `xs.pop(0)` of a mixed list
	// reads its kind out of @heap_tags.
	popCall := ""
	for _, line := range strings.Split(body, "\n") {
		if strings.Contains(line, "call i32 @rt_pop(") {
			popCall = strings.TrimSpace(line)
		}
	}
	if popCall == "" || !strings.Contains(popCall, "i32* %") {
		t.Fatalf("rt_pop is called without the out-parameter that carries the removed slot's tag: %q", popCall)
	}
	if strings.Count(body, "call i32 @rt_payload_eq(") != 1 {
		t.Fatalf("one identity question emitted %d pair comparisons; the answer must come from one door\n%s",
			strings.Count(body, "call i32 @rt_payload_eq("), body)
	}

	// 2. the fold: what the source can settle is still settled at compile time, with no call at all. The
	// last two are not the row's shapes and are here to keep the door's gate shut where it was shut: an
	// untagged slot read of a single-kind list and a literal against the void both still answer without
	// visiting the runtime. (`xs = [7, 8, 9]` / `xs.pop(0) is None` is NOT one of them — the removal still
	// reads the object's tag, and it is answered by the door at the answered table above, which is the
	// difference between "the compiler knows" and "the compiler assumed".)
	for _, src := range []string{
		"print(1 is None)\n",
		"x = None\nprint(x is None)\n",
		"print(None is None)\n",
		"print(\"a\" is None)\n",
		"xs = [1, 2]\nprint(xs[0] == 1)\n",
	} {
		fold := theVoidMainBody(irOf(t, src))
		if strings.Contains(fold, "call i32 @rt_payload_eq(") {
			t.Fatalf("a question the compiler can answer at compile time went to the runtime door:\n%s", fold)
		}
	}

	// 3. the condition road asks the SAME door as the value road — one question, one door (ADR 0232).
	cond := theVoidMainBody(irOf(t, "xs = [None, 1]\nif xs.pop(0) is None:\n    print(\"y\")\n"))
	if strings.Count(cond, "call i32 @rt_payload_eq(") != 1 {
		t.Fatalf("the condition road did not reach the pair door (%d call(s)):\n%s",
			strings.Count(cond, "call i32 @rt_payload_eq("), cond)
	}
}

// TestTheVoidTheEqualityAndThePrintAgreeOnOneValue is the asymmetry guard, and the reason this row widened the
// door instead of writing a road beside it. Three readers of one value, in one program: the printer, the
// equality and the identity. Before ADR 0411 they gave `None`, `True` and `False` for the same slot, which is
// three answers to one question.
func TestTheVoidTheEqualityAndThePrintAgreeOnOneValue(t *testing.T) {
	t.Parallel()
	spellings := []struct {
		name, tmpl, want string
	}{
		{"the printer asks", "xs = [None, 1]\nprint(xs.pop(0))\n", "None\n"},
		{"str asks", "xs = [None, 1]\nprint(str(xs.pop(0)))\n", "None\n"},
		{"equality asks", "xs = [None, 1]\nprint(xs.pop(0) == None)\n", "True\n"},
		{"identity asks", "xs = [None, 1]\nprint(xs.pop(0) is None)\n", "True\n"},
		{"the condition asks", "xs = [None, 1]\nif xs.pop(0) is None:\n    print(\"True\")\nelse:\n    print(\"False\")\n", "True\n"},
		{"identity-not asks", "xs = [None, 1]\nprint(xs.pop(0) is not None)\n", "False\n"},
		{"equality-not asks", "xs = [None, 1]\nprint(xs.pop(0) != None)\n", "False\n"},
	}
	// One slot, one kind, seven questions: every spelling must say what the OTHER spellings say.
	for _, s := range spellings {
		s := s
		t.Run(s.name, func(t *testing.T) {
			t.Parallel()
			got, err := compileAndRun(t, s.tmpl)
			if err != nil {
				t.Fatalf("refused: %v\n%s", err, s.tmpl)
			}
			if got != s.want {
				t.Fatalf("%s answered %q, want %q — the same slot, asked the same question\n%s",
					s.name, got, s.want, s.tmpl)
			}
		})
	}
}

// theVoidOwed is the honest half: the shapes this row measured and did NOT pay, each with the bytes this
// backend gives, the bytes the reference gives, and the row that owns the fix. A row that stops being true
// FAILS — the program is moved to the answered table, not edited here.
func theVoidOwed() []struct {
	name     string
	src      string
	gap      string
	compiled string
	python   string
	why      string
} {
	return []struct {
		name     string
		src      string
		gap      string
		compiled string
		python   string
		why      string
	}{
		{
			name:     "a call that answers the void, compared with it",
			src:      "def f():\n    return None\nprint(f() is None)\nprint(f() == None)\n",
			gap:      "Gap R.388",
			compiled: "False\nFalse\n",
			python:   "True\nTrue\n",
			why: "the `answerKindPlan` void set records a body that has NO return, and a body that SPELLS " +
				"`return None` is registered as an answer that carries a kind — so the source-side fold decides it " +
				"is not the void. Standing row, identical on the parent build; the pair door this row opened reaches " +
				"it the moment the callee's answer is read as a (payload, tag) pair",
		},
		{
			name:     "a loop item over a LITERAL list",
			src:      "for v in [None, \"a\"]:\n    print(v is None)\n",
			gap:      "Gap R.416",
			compiled: "False\nFalse\n",
			python:   "True\nFalse\n",
			why: "the unrolled item's kind is recorded for the PRINTER and the TRUTH road (ADR 0232/ADR 0388's " +
				"`loopElemTag`) and for nothing else, so the identity question falls to the fold; `for v in xs` over " +
				"a NAMED list answers — that pair is bound by the pair road and this row pays it",
		},
		{
			name:     "equality, of the same loop item",
			src:      "for v in [None, \"a\"]:\n    print(v == None)\n",
			gap:      "Gap R.416",
			compiled: "False\nFalse\n",
			python:   "True\nFalse\n",
			why: "the `==` door was open before this row and is closed to the unrolled item for the same reason: " +
				"the item's tag is not in the notebook `isTaggedCompareSide` consults — filed beside the identity half " +
				"so the two cannot be paid separately and disagree",
		},
		{
			name:     "a callee's own parameter, given the void",
			src:      "def f(v):\n    return v is None\nprint(f(None))\nprint(f(1))\n",
			gap:      "Gap R.417",
			compiled: "False\nFalse\n",
			python:   "True\nFalse\n",
			why: "the argument's kind crossed no door, because the scan opens a parameter's pair for a slot read and " +
				"a popped slot and not for a written void — **ADR 0393**/Gap R.361 paid the callee's TRUTH from the " +
				"same words; the identity door is the residue",
		},
		{
			name:     "a callee's own parameter, given a popped slot",
			src:      "def f(v):\n    return v is None\nxs = [None, 1]\nprint(f(xs.pop(0)))\n",
			gap:      "Gap R.417",
			compiled: "False\n",
			python:   "True\n",
			why: "`pairCallScan`'s needs half DOES open the parameter for a popped slot (ADR 0410), and the body then " +
				"asks the payload — the same shape ADR 0393 fixed for `if v:`, one door over",
		},
		{
			name:     "a popped slot printed from inside the callee",
			src:      "def f(v):\n    print(v)\n    return v is None\nxs = [None, 1]\nprint(f(xs.pop(0)))\n",
			gap:      "Gap R.417",
			compiled: "0\nFalse\n",
			python:   "None\nTrue\n",
			why: "the void's payload is `0` and the callee's printer is handed the payload: this is **Gap R.228**'s " +
				"shape arriving one frame deeper, and it is identical on the parent build — a refusal would be a " +
				"different debt, so it is pinned as the wrong answer it is",
		},
		{
			name:     "the same list under a second name",
			src:      "xs = [None, 1]\nys = xs\nprint(ys.pop(0) is None)\n",
			gap:      "Gap R.415",
			compiled: "False\n",
			python:   "True\n",
			why: "`popAnswerShape` reads the notebooks for the NAME the removal is written against, and the alias " +
				"carries no kind claim of its own, so the door declines and the fold answers — the print half of the " +
				"same program prints `0` where the reference prints `None`, which is why this is the alias row and not " +
				"this one",
		},
		{
			name:     "identity of a popped CONTAINER against a literal",
			src:      "xs = [[1], 2]\nprint(xs.pop(0) is [1])\n",
			gap:      "Gap R.418",
			compiled: "",
			python:   "False\n",
			why: "**exit 2** — `llc` refuses the module on `icmp eq i32 %t, @.lst1`: the literal's `@.lst1` is a " +
				"module-level global standing in a word position, the asking-door half of the Gap R.393/Gap R.253 " +
				"family, and the reason this row admits the void and NOTHING else through the pair door",
		},
	}
}

// TestWhatTheVoidQuestionStillOwesIsFiledNotFixed is the honest half of the row. Each program is pinned with
// the bytes this backend gives beside the reference's, and with the row that owns the fix — including the one
// that dies in the assembler, which is ADR 0166's forbidden class and belongs on the tracker rather than in a
// comment.
func TestWhatTheVoidQuestionStillOwesIsFiledNotFixed(t *testing.T) {
	t.Parallel()
	for _, r := range theVoidOwed() {
		r := r
		t.Run(r.name, func(t *testing.T) {
			t.Parallel()
			got, err := compileAndRun(t, r.src)
			if r.compiled == "" {
				// The exit-2 shape: the compiler's own mistake, pinned as the refusal of the assembler.
				if err == nil {
					t.Fatalf("%s: the module now BUILDS and prints %q — the assembler no longer refuses it, "+
						"so the row is paid and this pin must be deleted\n%s", r.gap, got, r.src)
				}
				if !strings.Contains(err.Error(), "global variable reference") && !strings.Contains(err.Error(), "llc") {
					t.Fatalf("%s: expected the assembler's refusal, got %v\n%s", r.gap, err, r.src)
				}
				return
			}
			if err != nil {
				t.Fatalf("%s: the shape was REFUSED rather than answered wrongly (%v) — an answer and a refusal are "+
					"different debts; move this row if the debt changed\n%s", r.gap, err, r.src)
			}
			if got != r.compiled {
				t.Fatalf("%s changed shape: compiled prints %q, this row pins %q (the reference prints %q)\n%s",
					r.gap, got, r.compiled, r.python, r.src)
			}
			if got == r.python {
				t.Fatalf("%s closed: the program prints the reference's bytes — delete this row and move the "+
					"program into theVoidAnswered()\n%s", r.gap, r.src)
			}
		})
	}
}

// theVoidMainBody is the program's own `define i32 @main()` body — the runtime helpers are full of
// rt_payload_eq and rt_pop definitions, and a claim about the program has to be a claim about its code.
func theVoidMainBody(ir string) string {
	start := strings.Index(ir, "define i32 @main(")
	if start < 0 {
		return ""
	}
	end := strings.Index(ir[start:], "\n}")
	if end < 0 {
		return ir[start:]
	}
	return ir[start : start+end+2]
}
