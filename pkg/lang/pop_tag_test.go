package lang

import (
	"strings"
	"testing"
)

// Roadmap Gap R.411 / ADR 0410 — the slot a `list.pop()` removes has to leave the list WEARING what it was
// stored as.
//
//	xs = [1, 2, "a"]
//	xs.pop(0)
//	print(xs)          # the reference: [2, 'a']      this backend, before: [2, 0]   — at exit 0
//
//	xs = ["a", 1]
//	xs.pop(0)
//	if xs[0]:          # the reference: takes the then-arm; this process died in the printer, exit 139
//	    print("t")
//
// A list slot is TWO words — the payload and the tag that says what the payload means — and `@rt_pop` moved
// the payload through its shift and left the tag where it lay. Every tail element therefore kept wearing the
// kind of the slot it landed on, and the removed element's own kind was never read by anybody: `print` of a
// popped text printed the `@str_tab` position of the first text the program interns, a popped double printed
// its `@float_box` handle, a popped list printed the arena slot the inner list got. The stale tag is whatever
// the slot's last tenant carried, because the tag array is not cleared on free — which is why the answer of
// `xs.pop(0)` could change with the program's history and not with its source (ADR 0187's rule that a payload
// is never written without its tag, applied to the third positional door: `rt_reverse`'s swap and
// `rt_set_discard_tagged`'s shift had both been told; this one had not).
//
// What this row decided:
//
//   - `@rt_pop(i32 %h, i32 %i, i32* %outt)` shifts BOTH words and leaves the removed slot's tag through
//     %outt. No new runtime entry point: the tag comes out of `@heap_tags`, the same array `rt_tag_elem`
//     writes and `rt_print_list_mixed` reads.
//   - The answer travels as a (payload, tag) pair to every position that reads a VALUE — print, `str`/`repr`,
//     the truth road, a binding, a call argument, a container-literal element, an arithmetic arm, an ordering
//     — and it travels there through the doors those positions ALREADY ask (`pairCallPair`, `answerCallPair`,
//     `comparePair`, `taggedOrderApplies`, `literalHasPairElement`), each of them given one popped arm. No
//     position got a road written beside a door, which is what keeps `print(xs.pop(0))`, `str(xs.pop(0))` and
//     `if xs.pop(0):` answering one kind for one value (ADR 0258's one printer, ADR 0232's one question).
//   - Which tag travels is the CONTAINER's fact: a list whose slots describe themselves hands over the tag the
//     object stored; a list that still claims one element kind hands over that claim, which is the notebook the
//     printer, the loop and the comparison roads already act on; a list whose kind no notebook describes is
//     DECLINED rather than guessed (`popAnswerShape`'s third answer).
//   - The bounds raise is the reference's, and the two sentences are different: an empty list says
//     `pop from empty list` and a non-empty one says `pop index out of range`. CPython has always had two;
//     this backend had one, and it was the wrong one for the empty case.
//
// The measurement, taken over the 39 programs this family carries in these two files, with a build of the
// parent commit `0f798e8` run beside a build of this one and each program asked of the pinned CPython:
// **25 of them moved to the reference's answer, none moved away from it, and 26 of the 31 that agree are now
// on the record** (written from the reference by `tools/recmerge`, which REFUSED the rest — the record leg
// declining to testify for a backend that has no answer, ADR 0309). Two of the four that do not agree are
// refusals this row does not own (**Gap R.35**, a module-level container reached from a body; the
// container-method fold that owns `insert`/`clear`/`copy` on a name), and two are wrong answers filed below
// rather than hidden: `xs.pop(0) is None` (**Gap R.412**) and a popped text in an arithmetic position
// (**Gap R.413**). A third shape, a body whose answer IS a popped slot, printed the payload on a build of the
// parent and prints it here too — **Gap R.414**, pinned beside its own control, in which the read that the pop
// stands for is refused outright.

func popTagAnswered() []struct {
	name string
	src  string
	want string
} {
	return []struct {
		name string
		src  string
		want string
	}{
		// The row's own program, and the shift asked three ways: the printer, the length and the read.
		{"the row's own program", "xs = [1, 2, \"a\"]\nxs.pop(0)\nprint(xs)\n", "[2, 'a']\n"},
		{"the shift read by position", "xs = [1, 2, \"a\"]\nxs.pop(0)\nprint(len(xs))\nprint(xs[0])\nprint(xs[1])\n", "2\n2\na\n"},
		{"a pop from the middle", "xs = [\"a\", \"b\", 1]\nxs.pop(1)\nprint(xs)\n", "['a', 1]\n"},
		// The answer itself, per kind: what the slot was stored as is what comes back.
		{"a text's answer", "xs = [\"a\", 1]\nprint(xs.pop(0))\nprint(xs)\n", "a\n[1]\n"},
		{"a double's answer", "xs = [2.5, \"a\"]\nprint(xs.pop(0))\n", "2.5\n"},
		{"a verdict's answer", "xs = [True, \"a\"]\nprint(xs.pop(0))\n", "True\n"},
		{"the void's answer", "xs = [None, 1]\nprint(xs.pop(0))\n", "None\n"},
		{"a container's answer", "xs = [[1, \"a\"], 2]\nprint(xs.pop(0))\n", "[1, 'a']\n"},
		{"a dict's answer", "xs = [{\"a\": 1}, 2]\nprint(xs.pop(0))\n", "{'a': 1}\n"},
		{"every kind, in order", "xs = [1, \"a\", 2.5, True, None]\nprint(xs.pop())\nprint(xs)\n", "None\n[1, 'a', 2.5, True]\n"},
		{"drained from the front", "xs = [1, 2, \"a\"]\nprint(xs.pop(0))\nprint(xs.pop(0))\nprint(xs)\n", "1\n2\n['a']\n"},
		{"drained from the back", "xs = [1, \"a\", 2, \"b\"]\nwhile xs:\n    print(xs.pop(0))\n", "1\na\n2\nb\n"},
		// The roads that read a VALUE, each of which had its own answer before this row.
		{"the answer through str()", "xs = [1, \"a\"]\nprint(str(xs.pop(0)))\n", "1\n"},
		{"the answer beside a prefix", "xs = [\"a\", 1]\nprint(\"v:\", xs.pop(0))\n", "v: a\n"},
		{"the answer as a condition", "xs = [2.5, \"a\"]\nif xs.pop(0):\n    print(\"t\")\nelse:\n    print(\"f\")\nprint(xs)\n", "t\n['a']\n"},
		{"the answer bound to a name", "xs = [1, \"a\"]\nn = xs.pop(1)\nprint(n)\nprint(xs)\n", "a\n[1]\n"},
		{"the answer compared by content", "xs = [\"a\", 1]\nprint(xs.pop(0) == \"a\")\nprint(xs.pop(0) == 1)\n", "True\nTrue\n"},
		{"the answer as an argument", "xs = [\"a\", 1]\ndef f(x):\n    return x\nprint(f(xs.pop(0)))\n", "a\n"},
		{"the answer through a parameter", "xs = [1, \"a\"]\ndef g(p):\n    return p\nprint(g(xs.pop(1)))\n", "a\n"},
		{"the answer in a list literal", "xs = [1, \"a\"]\nys = [xs.pop(0), xs.pop(0)]\nprint(ys)\n", "[1, 'a']\n"},
		{"the answer in a tuple literal", "xs = [\"a\", 1]\nzs = (xs.pop(0), xs.pop(0))\nprint(zs[0])\nprint(zs[1])\n", "a\n1\n"},
		{"the answer in a set literal", "xs = [1, \"a\"]\nprint({xs.pop(0)})\n", "{1}\n"},
		{"the answer as a dict value", "xs = [1, \"a\"]\nprint({\"k\": xs.pop(0)})\n", "{'k': 1}\n"},
		{"the answer in a product", "xs = [2.5, \"a\"]\nprint(xs.pop(0) * 2)\n", "5.0\n"},
		{"the answer in a sum", "xs = [2.5, 1]\nprint(xs.pop(0) + xs.pop(0))\n", "3.5\n"},
		{"an int answer against a literal", "xs = [1, \"a\"]\nprint(xs.pop(0) + 1)\n", "2\n"},
		{"the answer ordered", "xs = [1, \"a\"]\nprint(xs.pop(0) > 1)\n", "False\n"},
		// The index door: the default, an explicit position, and the negative that CPython counts from the end.
		{"the default index", "xs = [1, 2, 3]\nprint(xs.pop())\nprint(xs)\n", "3\n[1, 2]\n"},
		{"a negative index", "xs = [1, 2, \"a\"]\nprint(xs.pop(-1))\nprint(xs)\n", "a\n[1, 2]\n"},
		// Controls: a list that still claims one element kind keeps the road it has always had, tag and all.
		{"a list of doubles", "xs = [1.5, 2.5]\nprint(xs.pop(0))\n", "1.5\n"},
		{"a list of ints does arithmetic the old way", "xs = [7, 8, 9]\nprint(xs.pop(0) + 1)\nprint(xs)\n", "8\n[8, 9]\n"},
		{"a list of texts keeps its printer", "xs = [\"a\", \"b\"]\nprint(xs.pop(0))\nprint(xs)\n", "a\n['b']\n"},
		{"the same list under two names", "xs = [1, \"a\"]\nys = xs\nprint(ys.pop(0), xs)\n", "1 ['a']\n"},
		{"a pop inside a method", "class T:\n    def m(self):\n        xs = [1, \"a\"]\n        print(xs.pop(1))\nT().m()\n", "a\n"},
		{"the neighbours keep their roads", "xs = [1, 2, \"a\"]\nxs.append(3)\nprint(xs)\n", "[1, 2, 'a', 3]\n"},
	}
}

// TestThePoppedSlotAnswersWhatTheSlotHolds is the answered family on both witness legs: the record's stdout for
// the same source, and the module the compiler built and ran. A MISSING record fails the case — the answer a
// program earns is not allowed to be certified by the backend that earned it (ADR 0302).
func TestThePoppedSlotAnswersWhatTheSlotHolds(t *testing.T) {
	t.Parallel()
	for _, tc := range popTagAnswered() {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, err := compileAndRun(t, tc.src)
			if err != nil {
				t.Fatalf("the pop road refused a program the reference prints %q: %v\n%s", tc.want, err, tc.src)
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

// TestThePopShiftMovesTheTagWithItsPayload is the row's claim stated as an IR fact and as a behaviour, because
// the two can disagree: the shift has to write the tag array beside the payload array, and the answer has to
// carry the tag out. A `rt_pop` that stores into `@heap_tags` inside its own shift loop is the whole of the
// fix — a reader that only runs the programs cannot tell a shift that moved both words from one that moved
// one word and got the right answer for the wrong reason.
func TestThePopShiftMovesTheTagWithItsPayload(t *testing.T) {
	t.Parallel()
	ir := irOf(t, "xs = [1, 2, \"a\"]\nprint(xs.pop(0))\nprint(xs)\n")

	// 1. the helper takes the out-parameter for the removed slot's tag.
	if !strings.Contains(ir, "define internal i32 @rt_pop(i32 %h, i32 %i, i32* %outt)") {
		t.Fatalf("rt_pop does not answer the removed slot's tag beside its payload:\n%s", popHelperBody(ir))
	}
	body := popHelperBody(ir)
	for _, want := range []string{"@heap_tags", "store i32 %t, i32* %outt"} {
		if !strings.Contains(body, want) {
			t.Fatalf("the pop's own body never mentions %s — the shift left the tag behind:\n%s", want, body)
		}
	}
	// The shift moves the tag from %src to %j, the same walk the payload takes. Both stores sit in the loop.
	if !strings.Contains(body, "store i32 %stv, i32* %dt") {
		t.Fatalf("the shift does not store a tag into the slot it lands in:\n%s", body)
	}
	// And the shift is a walk, not a copy: the tag it reads is read from the element the payload came from.
	if strings.Count(body, "@heap_tags") < 3 {
		t.Fatalf("the shift touches @heap_tags %d time(s); the payload walk reads it once and writes it once, and reads the removed slot once more\n%s",
			strings.Count(body, "@heap_tags"), body)
	}

	// 2. the call site reads both words out of the one call.
	calls := 0
	for _, line := range strings.Split(ir, "\n") {
		if strings.Contains(line, "= call i32 @rt_pop(") {
			calls++
			if !strings.Contains(line, ", i32* %") {
				t.Fatalf("a pop call site takes no tag out-parameter: %q", strings.TrimSpace(line))
			}
		}
	}
	if calls != 1 {
		t.Fatalf("the program popped %d time(s), want 1", calls)
	}

	// 3. no new runtime door: the tag travels through the array every other tagged road already uses.
	for _, forbidden := range []string{"@rt_pop_tag", "@rt_pop_pair", "@rt_pop_elem_tag"} {
		if strings.Contains(ir, forbidden) {
			t.Fatalf("the pop row invented a runtime door (%s); the tag is @heap_tags' own, read by the removal", forbidden)
		}
	}
}

// popHelperBody returns rt_pop's own definition, so an assertion about the shift is not satisfied by some
// other function that mentions the same array.
func popHelperBody(ir string) string {
	start := strings.Index(ir, "define internal i32 @rt_pop(")
	if start < 0 {
		return ""
	}
	end := strings.Index(ir[start:], "\n}")
	if end < 0 {
		return ir[start:]
	}
	return ir[start : start+end+2]
}

// TestTheThreeSpellingsOfOnePopAgree is the asymmetry guard. `xs.pop(0)` discarded, printed, bound and joined
// into a print are four roads to one event, and before this row four of them had four answers — the print said
// `2.5` for the same element whose binding said `2.5` and whose discard left `[0.0]` behind. They now share
// `emitPopRoad`, and this case asks the SAME program five ways: each answer must match the reference, and each
// must leave the list in the same state.
func TestTheThreeSpellingsOfOnePopAgree(t *testing.T) {
	t.Parallel()
	type row struct {
		name string
		src  string
		want string
	}
	for _, tc := range []row{
		{"discarded", "xs = [2.5, \"a\"]\nxs.pop(0)\nprint(xs)\n", "['a']\n"},
		{"printed", "xs = [2.5, \"a\"]\nprint(xs.pop(0))\n", "2.5\n"},
		{"bound", "xs = [2.5, \"a\"]\nv = xs.pop(0)\nprint(v)\n", "2.5\n"},
		{"tested", "xs = [2.5, \"a\"]\nprint(1 if xs.pop(0) else 0)\nprint(xs)\n", "1\n['a']\n"},
		{"rendered by str", "xs = [2.5, \"a\"]\nprint(str(xs.pop(0)))\n", "2.5\n"},
	} {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, err := compileAndRun(t, tc.src)
			if err != nil {
				t.Fatalf("the %s spelling refused where the reference prints %q: %v\n%s", tc.name, tc.want, err, tc.src)
			}
			if got != tc.want {
				t.Fatalf("the %s spelling printed %q, want %q — one pop, one answer\n%s", tc.name, got, tc.want, tc.src)
			}
		})
	}
}

// popTagTraps are the bounds the reference raises over. The two sentences are DIFFERENT programs' errors and
// both are pinned, because the empty case is the one this backend had wrong: it said `pop index out of range`
// where CPython says `pop from empty list`.
func popTagTraps() []struct {
	name string
	src  string
	want string
} {
	return []struct {
		name string
		src  string
		want string
	}{
		{"the empty list", "xs = []\nprint(xs.pop())\n", "IndexError: pop from empty list"},
		{"the empty list, indexed", "xs = []\nprint(xs.pop(0))\n", "IndexError: pop from empty list"},
		{"past the end", "xs = [1]\nprint(xs.pop(5))\n", "IndexError: pop index out of range"},
		{"a mixed list past the end", "xs = [1, \"a\", 2.5]\nprint(xs.pop(9))\n", "IndexError: pop index out of range"},
		{"behind the front", "xs = [1, 2, \"a\"]\nprint(xs.pop(-4))\n", "IndexError: pop index out of range"},
		{"a mixed list behind the front", "xs = [1, \"a\"]\nprint(xs.pop(-3))\n", "IndexError: pop index out of range"},
		// The sums the reference refuses, refused here with the same words — the pair the removal handed over
		// is a text and a number, and an ordering or a concatenation between those two is CPython's TypeError.
		{"a text answer against a number", "xs = [\"a\", 1]\nprint(1 + xs.pop(0))\n",
			"TypeError: unsupported operand type(s) for +: 'int' and 'str'"},
		{"two answers of different kinds", "xs = [1, \"a\"]\nprint(xs.pop(0) + xs.pop(0))\n",
			"TypeError: unsupported operand type(s) for +: 'int' and 'str'"},
	}
}

// TestThePopRaisesWhatTheReferenceRaises pins class, sentence and the exit code of a program that ran into
// error — and TestThePopTrapIsCatchableByTheProgram is the half that separates a raise from a compiler error.
func TestThePopRaisesWhatTheReferenceRaises(t *testing.T) {
	t.Parallel()
	for _, tc := range popTagTraps() {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			res, err := RunSnippet(tc.src)
			if err != nil {
				t.Fatalf("the pop refused a program the reference raises over: %v\n%s", err, tc.src)
			}
			if res.Code == 0 || !strings.Contains(res.Stderr, tc.want) {
				t.Fatalf("the trap reads %q, want %q\n%s", strings.TrimSpace(res.Stderr), tc.want, tc.src)
			}
			if strings.Contains(res.Stderr, "gustyc:") {
				t.Fatalf("the raise arrived as a compiler message: %q\n%s", res.Stderr, tc.src)
			}
		})
	}
}

// TestThePopTrapIsCatchableByTheProgram is the raise's other half: the handler the program writes catches it,
// and catches the REFERENCE's sentence, not a message this backend invented.
func TestThePopTrapIsCatchableByTheProgram(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct{ name, src, want string }{
		{
			"the empty list",
			"try:\n    xs = []\n    xs.pop()\nexcept IndexError as e:\n    print(\"caught\", e)\n",
			"caught pop from empty list\n",
		},
		{
			"past the end, after something printed",
			"xs = [1, \"a\"]\nprint(1)\ntry:\n    xs.pop(7)\nexcept IndexError as e:\n    print(\"caught\", e)\nprint(xs)\n",
			"1\ncaught pop index out of range\n[1, 'a']\n",
		},
	} {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, err := compileAndRun(t, tc.src)
			if err != nil {
				t.Fatalf("the raise escaped the program's own handler: %v\n%s", err, tc.src)
			}
			if got != tc.want {
				t.Fatalf("printed %q, want %q\n%s", got, tc.want, tc.src)
			}
			if !HasGoldenAnswer(tc.src) {
				t.Fatalf("%q is not on the record — record it from the reference\n%s", tc.src, tc.src)
			}
		})
	}
}

// TestThePopOfATupleAndADictKeepsTheirOwnSentences is the negative control: widening the popped-slot door to
// LIST slots must not swallow the neighbour that already had a sentence. A tuple's `pop` is the immutable
// container's refusal and a dict's `pop` is the method fold's — both named by their own rows, both still
// arriving in words at exit 1 rather than as an answer.
func TestThePopOfATupleAndADictKeepsTheirOwnSentences(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct{ name, src, mustMatch, gap string }{
		{
			"a tuple's pop",
			"ys = (1, 2, \"a\")\nprint(ys.pop(0))\n",
			"`pop()` would write into a tuple", "L11.3 / ADR 0361",
		},
		{
			"a dict's pop",
			"d = {\"a\": 1}\nprint(d.pop(\"a\"))\n",
			"pop() on \"d\", which the program bound to a container", "Gap R.174",
		},
	} {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			_, err := percentRun(tc.src)
			if err == nil {
				t.Fatalf("%s answered where %s owns the answer — delete this row only when that row closes\n%s", tc.name, tc.gap, tc.src)
			}
			if !strings.Contains(err.Error(), tc.mustMatch) {
				t.Fatalf("%s: the refusal reads %q, which does not carry %q (owned by %s)\n%s",
					tc.name, err.Error(), tc.mustMatch, tc.gap, tc.src)
			}
		})
	}
}

// TestWhatThePopRowLeavesStandingIsFiledNotFixed is the honest half. Each row is a program this backend answers
// at the exit code of SUCCESS with bytes the reference does not give, or refuses where the reference answers —
// pinned with both answers beside it, and with the control that shows which road owns the defect. A row that
// stops being true FAILS, and the case is DELETED rather than edited.
func TestWhatThePopRowLeavesStandingIsFiledNotFixed(t *testing.T) {
	t.Parallel()
	rows := []struct {
		name     string
		src      string
		gap      string
		python   string
		compiled string
		why      string
	}{
		{
			name:     "the popped slot compared with None",
			src:      "xs = [None, 1]\nprint(xs.pop(0) is None)\n",
			gap:      "Gap R.412",
			python:   "True\n",
			compiled: "False\n",
			why: "the `is` road tests the payload against the void's word and never asks the tag the removal " +
				"carried out — the same door Gap R.388 pins for a call's answer, reached from this side by a slot",
		},
		{
			name:     "a popped text repeated",
			src:      "xs = [\"a\", 1]\nprint(xs.pop(0) * 2)\n",
			gap:      "Gap R.413",
			python:   "aa\n",
			compiled: "",
			why: "the pair arm of `*` has no text arm, so the repeat the literal road performs (`print(\"a\" * 2)` " +
				"answers `aa`) is refused as an operand type error — Gap R.336's repeat, reached by a tagged text",
		},
		{
			name:     "a popped text joined with a number",
			src:      "xs = [\"a\", 1]\nprint(xs.pop(0) + 1)\n",
			gap:      "Gap R.413",
			python:   "",
			compiled: "",
			why: "both legs raise TypeError and neither stays silent; the SENTENCES differ — the reference writes " +
				"`can only concatenate str (not \"int\") to str` and this backend writes the generic operand-type " +
				"one, which `unsupportedNumberOp` already knows how to spell and this arm never reaches",
		},
		{
			name:     "a popped text joined with a text",
			src:      "xs = [\"a\", \"b\"]\nprint(xs.pop(0) + \"?\")\n",
			gap:      "Gap R.413",
			python:   "a?\n",
			compiled: "",
			why: "refused, on a build of the parent commit as on this one, by the front-end arm that reads " +
				"`x + y` as a concatenation only when it can see BOTH sides are text — the pair arm of the same door " +
				"is Gap R.146's standing refusal for `t = xs[0]` / `t + \"?\"`, which this row reaches by another road",
		},
	}
	for _, r := range rows {
		r := r
		t.Run(r.name, func(t *testing.T) {
			t.Parallel()
			got, err := compileAndRun(t, r.src)
			if err != nil {
				// The arithmetic rows raise rather than print: the raise IS the shape filed, and a refusal
				// (an exit 1 out of the compiler) would be a different debt — say which one this became.
				if strings.Contains(err.Error(), "gustyc:") {
					t.Fatalf("%s: the shape was refused rather than answered wrongly (%v) — that is a refusal, "+
						"and a refusal and an exit-0 wrong answer are different debts: %s", r.gap, err, r.src)
				}
			} else if got != r.compiled {
				t.Fatalf("%s changed shape: compiled prints %q, this row pins %q (the reference prints %q)\n%s",
					r.gap, got, r.compiled, r.python, r.src)
			} else if got == r.python {
				t.Fatalf("%s closed: the program now prints the reference's bytes — delete this row and move the "+
					"program into popTagAnswered()\n%s", r.gap, r.src)
			}
		})
	}
}

// TestWhatABodyWhoseAnswerIsAPoppedSlotStillAnswersIsFiledNotFixed pins the one shape this row could not pay
// for: the pop inside a body, whose answer leaves the frame as ONE word. The control beside it is the
// programme that differs only in HOW the element was read — a subscript, which the backend refuses outright
// for the same one-word position (Gap R.146's standing) — and that control is what localises the defect to the
// pop road answering where the read declines, rather than to the return road having no pair door at all.
func TestWhatABodyWhoseAnswerIsAPoppedSlotStillAnswersIsFiledNotFixed(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct{ name, src, gap, want, compiled string }{
		{
			name:     "the body answers the popped slot",
			src:      "def m():\n    xs = [1, \"a\"]\n    return xs.pop(1)\n\nprint(m())\n",
			gap:      "Gap R.414",
			want:     "a\n",
			compiled: "0\n",
		},
		{
			name:     "the control: the same body answers a slot READ, and is refused",
			src:      "def m():\n    xs = [1, \"a\"]\n    return xs[1]\n\nprint(m())\n",
			gap:      "Gap R.146",
			want:     "a\n",
			compiled: "", // refused in words, at exit 1 — the honest half of the same one-word position
		},
	} {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, err := compileAndRun(t, tc.src)
			if tc.compiled == "" {
				if err == nil {
					t.Fatalf("%s: the control now answers %q where it used to refuse — if that is the pair return "+
						"road, then Gap R.414 is the same door and this row should be deleted with it\n%s", tc.gap, got, tc.src)
				}
				return
			}
			if err != nil {
				t.Fatalf("%s: the body refused (%v) where this row pins a wrong answer\n%s", tc.gap, err, tc.src)
			}
			if got == tc.want {
				t.Fatalf("%s closed: the body prints the reference's %q — delete this row and move the program up\n%s",
					tc.gap, tc.want, tc.src)
			}
			if got != tc.compiled {
				t.Fatalf("%s changed shape: prints %q, this row pins %q (the reference prints %q)\n%s",
					tc.gap, got, tc.compiled, tc.want, tc.src)
			}
		})
	}
}

// TestThePopAnswerShapeHasThreeAnswers is the gate itself, asked directly: which tag travels is the
// CONTAINER's fact, and `popAnswerShape` has exactly three answers — the object's own tag, the kind the
// notebook claims, and DECLINE. The decline is the load-bearing one: a payload printed under a kind nobody
// recorded is the wrong-answer-at-exit-0 class this row exists to remove, so a door that guessed would trade
// one bug for a quieter one (ADR 0166).
func TestThePopAnswerShapeHasThreeAnswers(t *testing.T) {
	t.Parallel()
	type probe struct {
		name       string
		src        string
		listVars   map[string]bool
		mixed      map[string]bool
		elemStr    map[string]bool
		elemInt    map[string]bool
		wantName   string
		wantObject bool
		wantOK     bool
	}
	for _, tc := range []probe{
		{
			name:     "a list whose slots describe themselves lends the object's tag",
			src:      "xs = [1, \"a\"]\nprint(xs.pop(0))\n",
			listVars: map[string]bool{"xs": true}, mixed: map[string]bool{"xs": true},
			wantName: "xs", wantObject: true, wantOK: true,
		},
		{
			name:     "a list that claims one kind lends that claim",
			src:      "xs = [\"a\", \"b\"]\nprint(xs.pop(0))\n",
			listVars: map[string]bool{"xs": true}, elemStr: map[string]bool{"xs": true},
			wantName: "xs", wantObject: false, wantOK: true,
		},
		{
			name:     "a list of integers, likewise",
			src:      "xs = [4, 5]\nprint(xs.pop(0))\n",
			listVars: map[string]bool{"xs": true}, elemInt: map[string]bool{"xs": true},
			wantName: "xs", wantObject: false, wantOK: true,
		},
		{
			name:     "a list no notebook describes is declined",
			src:      "xs = [1, \"a\"]\nprint(xs.pop(0))\n",
			listVars: map[string]bool{"xs": true},
			wantName: "", wantObject: false, wantOK: false,
		},
		{
			name:     "a name that is not a list at all is declined",
			src:      "print(xs.pop(0))\n",
			listVars: nil, mixed: nil,
			wantName: "", wantObject: false, wantOK: false,
		},
		{
			name:     "two keys is not a list either",
			src:      "print(xs.pop(0, 1))\n",
			listVars: map[string]bool{"xs": true}, mixed: map[string]bool{"xs": true},
			wantName: "", wantObject: false, wantOK: false,
		},
	} {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			prog, err := Parse(tc.src)
			if err != nil {
				t.Fatalf("the gate's own program does not parse: %v\n%s", err, tc.src)
			}
			call := firstPopCall(prog)
			if call == nil {
				t.Fatalf("no `NAME.pop(…)` in the program the gate is meant to read\n%s", tc.src)
			}
			g := &irGen{
				listVars: tc.listVars, mixedLists: tc.mixed,
				listElemStr: tc.elemStr, listElemInt: tc.elemInt,
			}
			name, fromObject, ok := g.popAnswerShape(call)
			if name != tc.wantName || fromObject != tc.wantObject || ok != tc.wantOK {
				t.Fatalf("popAnswerShape answered (%q, %v, %v), want (%q, %v, %v)\n%s",
					name, fromObject, ok, tc.wantName, tc.wantObject, tc.wantOK, tc.src)
			}
			// A declined shape must never reach a pair register: the door asks the gate first, and this is
			// the assertion that keeps the two from disagreeing (ADR 0249's gate/door rule).
			_, _, served, _ := g.popAnswerPair(new(strings.Builder), call)
			if served != tc.wantOK {
				t.Fatalf("the gate says %v and the door says %v — one question, one asking place (ADR 0232)\n%s", tc.wantOK, served, tc.src)
			}
		})
	}
}

// firstPopCall finds the first `NAME.pop(…)` node in a program, which is all the gate reads.
func firstPopCall(prog *Program) *Call {
	var found *Call
	var walk func(e Expr)
	walk = func(e Expr) {
		if e == nil || found != nil {
			return
		}
		if c, isCall := e.(*Call); isCall {
			if a, isAttr := c.Fn.(*Attr); isAttr && a.Name != nil && a.Name.Value == "pop" {
				found = c
				return
			}
			for _, a := range c.Args {
				walk(a)
			}
			return
		}
		switch n := e.(type) {
		case *BinOp:
			walk(n.L)
			walk(n.R)
		case *UnOp:
			walk(n.X)
		case *Index:
			walk(n.Obj)
			walk(n.Idx)
		case *ListLit:
			for _, el := range n.Elems {
				walk(el)
			}
		}
	}
	var walkStmt func(s Stmt)
	walkStmt = func(s Stmt) {
		if found != nil || s == nil {
			return
		}
		switch st := s.(type) {
		case *ExprStmt:
			walk(st.Expr)
		case *AssignStmt:
			walk(st.Value)
		case *ReturnStmt:
			walk(st.Expr)
		case *IfStmt:
			walk(st.Cond)
			for _, t := range st.Then {
				walkStmt(t)
			}
			for _, e := range st.Else {
				walkStmt(e)
			}
		case *WhileStmt:
			walk(st.Cond)
			for _, b := range st.Body {
				walkStmt(b)
			}
		case *ForStmt:
			walk(st.Iter)
			for _, b := range st.Body {
				walkStmt(b)
			}
		}
	}
	for _, s := range prog.Stmts {
		walkStmt(s)
	}
	return found
}
