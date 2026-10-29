package lang

// The element of a container literal whose ANSWER is a container — `print(1 in [f()])`,
// `print([1, 2] in [f()])`, `print([f()])`, `xs = [f()]` — with `def f(): return [1, 2]`.
//
// roadmap Gap R.530 · ADR 0457
//
// One program, three answers, before this file existed:
//
//	def f():
//	    return [1, 2]
//
//	print(len([f()]))        →  1        (compiled, always)
//	print([1, 2] in [f()])   →  exit 1: codegen: list literal elements must be integers, not list[int]
//	print(1 in [f()])        →  exit 1: the same sentence, about an element kind the program had proven
//
// and the reference prints `1`, `True`, `False`. The membership door **ADR 0456** had just landed asks
// `containerOperand`, which asks the four DECIDING doors — and none of them had ever asked **ADR 0333**'s
// container-answer scan a question in its life: `literalNeedsHeap`, `literalNeedsTags`, `nestedContainerElem`
// and `elemKindTag` all stopped at the shapes the SOURCE spells. So `[f()]` was declared a constant int array,
// the constant emitter refused the handle, the door swallowed that error as "not mine", and the ordinary road
// printed the emitter's sentence. This is **Gap R.487**/**Gap R.488** (the comprehension element) one construct
// over, and it carries the same rule: a gate that asks a different question from the door it gates is the bug
// (**ADR 0249**), and the fix is ONE predicate the gate and the door share (`containerAnswerSlotIsContainer`).
//
// Every expectation here is derived from `python3` — including the rows that answer `False`, which are the
// ones a hand-written table gets wrong: `[f()]` holds ONE element (the answer), so `1 in [f()]` is `False`
// even though `1` is a member of the answer itself, and `"a" in [g()]` is `False` even though `"a"` is a key
// of the dict the callee returned. Asking the haystack about the answer's own contents would be the wrong
// answer at the code that means the program ran, and the two rows beside it are what stop that.
//
// The refusals stay refusals where the tag would lie: a tuple answer carries its bit in the object and no slot
// printer reads it (**Gap R.533**), an attribute call or a `lambda` has no kind any table records
// (**Gap R.531**), and a callee whose slots the scan could only prove and not describe keeps **Gap R.261**'s
// sentence rather than wearing a label nobody could honour.

import (
	"os"
	"strings"
	"testing"
)

// containerAnswerElementRows is the answered family: an element that is a call the scan proves answers a
// container, in every position a container literal stands in.
func containerAnswerElementRows() []struct{ name, src, want string } {
	return []struct{ name, src, want string }{
		// The two shapes the row was filed with.
		{"the row as filed: content membership", "def f():\n    return [1, 2]\n\nprint([1, 2] in [f()])\n", "True\n"},
		{"the row as filed: the number is not the answer", "def f():\n    return [1, 2]\n\nprint(1 in [f()])\n", "False\n"},
		{"the negated question", "def f():\n    return [1, 2]\n\nprint(1 not in [f()])\n", "True\n"},
		// The same literal, printed, bound, measured, walked, rendered, compared: the refusal was one door's,
		// and the roads beside it had always asked the container door.
		{"printed", "def f():\n    return [1, 2]\n\nprint([f()])\n", "[[1, 2]]\n"},
		{"bound and printed", "def f():\n    return [1, 2]\n\nxs = [f()]\nprint(xs)\n", "[[1, 2]]\n"},
		{"measured", "def f():\n    return [1, 2]\n\nprint(len([f()]))\n", "1\n"},
		{"subscripted", "def f():\n    return [1, 2]\n\nprint([f()][0])\n", "[1, 2]\n"},
		{"walked", "def f():\n    return [1, 2]\n\nfor v in [f()]:\n    print(v)\n", "[1, 2]\n"},
		{"rendered by str", "def f():\n    return [1, 2]\n\nprint(str([f()]))\n", "[[1, 2]]\n"},
		{"rendered by repr", "def f():\n    return [1, 2]\n\nprint(repr([f()]))\n", "[[1, 2]]\n"},
		{"an f-string field", "def f():\n    return [1, 2]\n\nprint(f\"{[f()]}\")\n", "[[1, 2]]\n"},
		{"compared by content", "def f():\n    return [1, 2]\n\nprint([f()] == [[1, 2]])\n", "True\n"},
		{"compared to a different content", "def f():\n    return [1, 2]\n\nprint([f()] == [[1, 3]])\n", "False\n"},
		// The other two spellings, and the dict's key rule: the LIST holds the answer, so neither a key nor a
		// member of the answer is a member of the literal — the rows a hand-written table gets wrong.
		{"a dict answer, asked by content", "def g():\n    return {\"a\": 1}\n\nprint({\"a\": 1} in [g()])\n", "True\n"},
		{"a dict answer: its key is not the list's member", "def g():\n    return {\"a\": 1}\n\nprint(\"a\" in [g()])\n", "False\n"},
		{"a dict answer printed", "def g():\n    return {\"a\": 1}\n\nprint([g()])\n", "[{'a': 1}]\n"},
		{"a set answer, asked by content", "def s():\n    return {1, 2}\n\nprint({1, 2} in [s()])\n", "True\n"},
		{"a set answer: its member is not the list's member", "def s():\n    return {1, 2}\n\nprint(1 in [s()])\n", "False\n"},
		{"a set answer printed", "def s():\n    return {1, 2}\n\nprint([s()])\n", "[{1, 2}]\n"},
		{"an empty container answer printed", "def f():\n    return set()\n\nprint([f()])\n", "[set()]\n"},
		{"an empty dict answer printed", "def f():\n    return {}\n\nprint([f()])\n", "[{}]\n"},
		// Beside other elements: the literal keeps its ONE builder whether the slot holds an answer, a number
		// or a text (**ADR 0232**'s one question).
		{"beside a number, asked", "def f():\n    return [1, 2]\n\nprint(7 in [f(), 7])\n", "True\n"},
		{"beside a number, printed", "def f():\n    return [1, 2]\n\nprint([f(), 7])\n", "[[1, 2], 7]\n"},
		{"beside a text, printed", "def f():\n    return [1, 2]\n\nprint([f(), \"a\"])\n", "[[1, 2], 'a']\n"},
		{"two answers in one literal", "def f():\n    return [1, 2]\n\ndef g():\n    return [1, 2]\n\nprint([f(), g()] == [[1, 2], [1, 2]])\n", "True\n"},
		{"the answer is a member of its own literal", "def f():\n    return [1, 2]\n\nprint(f() in [f()])\n", "True\n"},
		{"an answer that nests", "def f():\n    return [1, [2]]\n\nprint([1, [2]] in [f()])\n", "True\n"},
		{"an answer that nests, printed", "def f():\n    return [{\"k\": [1]}]\n\nprint([f()])\n", "[[{'k': [1]}]]\n"},
		// The element is an EVALUATION: the callee's own effects are the program's, whoever asks about the
		// value it returned.
		{"the element's effect still runs", "def probe():\n    print(\"built\")\n\n    return [1]\n\nprint(1 in [probe()])\n", "built\nFalse\n"},
		{"the element's effect runs before the answer prints", "def probe():\n    print(\"built\")\n\n    return [1]\n\nprint([probe()])\n", "built\n[[1]]\n"},
		// Inside a body: the answer is read in the frame that holds the call. (A `def` NESTED in another body
		// is a different defect — see the filed table: its `return` never materialises.)
		{"a module answer read from a body", "def f():\n    return [1, 2]\n\ndef h():\n    print([f()])\n\nh()\n", "[[1, 2]]\n"},
		// The position the test stands in.
		{"standing in a condition", "def f():\n    return [1, 2]\n\nif [1, 2] in [f()]:\n    print(\"yes\")\n\nelse:\n    print(\"no\")\n", "yes\n"},
		{"a comprehension element", "def f():\n    return [1, 2]\n\nprint([f()] in [[f()] for x in range(1)])\n", "True\n"},
	}
}

func TestAContainerAnswerElementAnswers(t *testing.T) {
	t.Parallel()
	for _, r := range containerAnswerElementRows() {
		r := r
		t.Run(r.name, func(t *testing.T) {
			t.Parallel()
			got, err := compileAndRun(t, r.src)
			if err != nil {
				t.Fatalf("the compiled leg refused a program the reference prints %q: %v\n%s", r.want, err, r.src)
			}
			if got != r.want {
				t.Fatalf("printed %q, want %q\n%s", got, r.want, r.src)
			}
			onRecordIs(t, r.src, r.want)
		})
	}
}

// TestAContainerAnswerElementMatchesTheReference runs the same programs through CPython, so the answered
// family is judged on both legs and not on this backend's own table.
func TestAContainerAnswerElementMatchesTheReference(t *testing.T) {
	t.Parallel()
	for _, r := range containerAnswerElementRows() {
		r := r
		t.Run(r.name, func(t *testing.T) {
			t.Parallel()
			got, _, err := PythonRun(r.src)
			if err != nil {
				t.Fatalf("the reference does not run this program to completion: %v\n%s", err, r.src)
			}
			if got != r.want {
				t.Fatalf("the reference prints %q and this backend was asked to print %q — one of them is wrong\n%s", got, r.want, r.src)
			}
		})
	}
}

// TestWhatTheContainerAnswerElementStillRefuses pins the residue with the refusal's OWN bytes and the row that
// owns each. The decline table is written to FAIL when a shape starts compiling, so paying one of these rows
// moves it into the table above rather than leaving a stale claim behind.
func TestWhatTheContainerAnswerElementStillRefuses(t *testing.T) {
	t.Parallel()
	rows := []struct{ name, src, contains, owner string }{
		{
			// A tuple answer carries its bit in the object's own `@estr[h]` word (**ADR 0361**) and the outer
			// list's printer has no road that reads it through a slot: tried, `xs = [t()]` printed `[1]` at
			// exit 0 for the reference's `[(1, 2)]`. A tag no printer honours is worse than a refusal.
			"a tuple answer",
			"def t():\n    return (1, 2)\n\nprint([t()])\n",
			"hands back a container",
			"Gap R.533",
		},
		{
			// An attribute call: no name to ask the scan about, so no table records the element's kind.
			"an attribute call",
			"import math\nprint(1 in [math.sqrt(4)])\n",
			"list literal elements must be integers",
			"Gap R.531",
		},
		{
			// The message's own defect, still: `exprTyName` has no word for a lambda, so the refusal ends in
			// `not ` and names neither the element nor what the slot needs (**ADR 0166**'s second clause).
			"a lambda",
			"print(1 in [lambda: 1])\n",
			"list literal elements must be integers",
			"Gap R.531",
		},
		{
			// A callee whose slots the scan could only PROVE and not describe: the doors that read the whole
			// object may take the handle, and the doors that read a slot may not — so the element door has no
			// kind to write, and the literal road below answers with its own sentence. It is **Gap R.531**'s
			// complaint about that sentence, not a new one: the element IS a container, and the message says
			// the element must be an integer.
			"an undescribed container answer",
			"def f():\n    out = []\n    out.append(print)\n    return out\n\nprint(1 in [f()])\n",
			"list literal elements must be integers",
			"Gap R.261",
		},
		{
			// A `def` nested inside another `def`: **ADR 0333**'s materialising return is not reached for a
			// nested body, so `ret i32 @.lst1` still reaches `llc` and the module is rejected — **ADR 0166**'s
			// exit-2 class, identical on the parent commit, so this door neither made it nor fixed it.
			"a container answer from a nested def",
			"def h():\n    def f():\n        return [1, 2]\n\n    print([1, 2] in [f()])\n\nh()\n",
			"global variable reference must have pointer type",
			"Gap R.535",
		},
		{
			// The argument crosses as a (payload, tag) pair, which is one word too many for a `ret`: the
			// carriable question the BUILD door asks is asked by the DECIDE door too, so the two cannot
			// disagree (**ADR 0333**).
			"a call whose argument the pair road owns",
			"def f(a):\n    return [a]\n\nprint(1 in [f(2.5)])\n",
			"list literal elements must be integers",
			"Gap R.531",
		},
	}
	for _, r := range rows {
		r := r
		t.Run(r.name, func(t *testing.T) {
			t.Parallel()
			_, err := RunSnippet(r.src)
			if err == nil {
				t.Fatalf("the shape compiles today — the row that owns it (%s) has paid, so move it to the answered table\n%s", r.owner, r.src)
			}
			if !strings.Contains(err.Error(), r.contains) {
				t.Fatalf("it failed for another reason (%v); the row pins the refusal %s owns\n%s", err, r.owner, r.src)
			}
		})
	}
}

// TestTheContainerAnswerElementGatesAskTheScanDirectly is the gate asked as a decision, not through a program:
// the four DECIDING doors take their permission from ONE predicate, so a widening that lets the folded global
// claim a literal the object's builder is tagging — or a narrowing that takes the tag away from a slot that
// stores a handle — fails a row here rather than turning into a wrong answer somewhere else. The scan is built
// from the SAME whole-program walk codegen runs, so these rows cannot be satisfied by a table the test wrote.
func TestTheContainerAnswerElementGatesAskTheScanDirectly(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name string
		// prog defines the callee; callee is the element the doors are asked about.
		prog, callee string
		arg          Expr
		want         bool
	}{
		{"a list answer", "def f():\n    return [1, 2]\n", "f", nil, true},
		{"a dict answer", "def f():\n    return {1: 2}\n", "f", nil, true},
		{"a set answer", "def f():\n    return {1}\n", "f", nil, true},
		{"an empty container answer", "def f():\n    return set()\n", "f", nil, true},
		// A forward hands on a handle that belongs to another body's builders: the element door claims an
		// answer whose contents it can see, and this one it cannot (**Gap R.533**).
		{"a forwarded answer is declined", "def inner():\n    return [1, 2]\n\ndef f():\n    return inner()\n", "f", nil, false},
		{"the forwarded callee itself is an answer", "def inner():\n    return [1, 2]\n\ndef f():\n    return inner()\n", "inner", nil, true},
		{"a number answer is not a container", "def f():\n    return 1\n", "f", nil, false},
		{"a text answer is not a container", "def f():\n    return \"a\"\n", "f", nil, false},
		{"a void answer is not a container", "def f():\n    pass\n", "f", nil, false},
		{"a body that can fall off its end is not described", "def f():\n    if 1:\n        return [1, 2]\n", "f", nil, false},
		{"a body with two container kinds is not one answer", "def f(x):\n    if x:\n        return [1, 2]\n\n    return {1}\n", "f", nil, false},
		{"an answer whose slot the scan cannot follow is not described", "def f():\n    out = []\n    out.append(print)\n    return out\n", "f", nil, false},
		{"a call to a name the program never defined", "def other():\n    return [1]\n", "f", nil, false},
		// The argument crosses as one word or not at all — the carriable question the BUILD door asks, asked by
		// the DECIDE door too (**ADR 0333**, **ADR 0249**'s gate-and-door pair).
		{"a float argument the pair road owns", "def f(a):\n    return [1, 2]\n", "f", &FloatLit{Value: 2.5}, false},
		{"an integer argument", "def f(a):\n    return [1, 2]\n", "f", &IntLit{Value: 2}, true},
	} {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			prog, perr := Parse(tc.prog)
			if perr != nil {
				t.Fatalf("parse: %v", perr)
			}
			described, undescribed := containerAnswerScan(prog)
			g := &irGen{
				containerRet:       described,
				containerRetOpaque: undescribed,
				containerAsked:     map[string]bool{},
				// The element door asks the program's own declarations whether the name is one the SOURCE
				// wrote — an applied `lambda`'s hoisted declaration is not, and its handle belongs to the
				// hoisted-lambda road (**ROAD-R.536**). The scan is not enough on its own, so the gate test
				// hands the generator the same table the compiler builds (**ADR 0232**'s one source).
				fds: sourceFuncDefs(prog),
			}
			var arg []Expr
			if tc.arg != nil {
				arg = []Expr{tc.arg}
			}
			element := &Call{Fn: &Name{Value: tc.callee}, Args: arg}
			if got := g.containerAnswerIsContainerSlotExpr(element); got != tc.want {
				t.Fatalf("the element door answered %v, want %v\n%s", got, tc.want, tc.prog)
			}
			// The same verdict decides the LABEL, the BUILD and the GATES — one predicate, four doors
			// (**ADR 0232**'s one question).
			_, tagOK := g.containerAnswerSlotIsContainer(element)
			if tagOK != tc.want {
				t.Fatalf("the tag road and the gate disagreed: %v vs %v", tagOK, tc.want)
			}
			if nested := g.taggableNestedElem(element); nested != tc.want {
				t.Fatalf("taggableNestedElem said %v where the gate said %v", nested, tc.want)
			}
		})
	}
	// An attribute call has no name to ask the scan about, whatever the scan knows.
	t.Run("an attribute call is not a name the scan answers", func(t *testing.T) {
		t.Parallel()
		prog, perr := Parse("def f():\n    return [1, 2]\n")
		if perr != nil {
			t.Fatalf("parse: %v", perr)
		}
		described, undescribed := containerAnswerScan(prog)
		g := &irGen{containerRet: described, containerRetOpaque: undescribed, containerAsked: map[string]bool{}, fds: sourceFuncDefs(prog)}
		attr := &Call{Fn: &Attr{Obj: &Name{Value: "math"}, Name: &Name{Value: "sqrt"}}, Args: []Expr{&IntLit{Value: 4}}}
		if g.containerAnswerIsContainerSlotExpr(attr) {
			t.Fatalf("an attribute call was taken for a container answer")
		}
		if g.containerAnswerIsContainerSlotExpr(&IntLit{Value: 1}) {
			t.Fatalf("an integer was taken for a container answer")
		}
		if g.containerAnswerIsContainerSlotExpr(&ListLit{Elems: []Expr{&IntLit{Value: 1}}}) {
			t.Fatalf("a literal was taken for an answer — the literal road owns that shape")
		}
	})
}

// TestTheContainerAnswerElementBuildsTheObjectOnce is the claim of unity in the module's own bytes: the
// program that PRINTS `[f()]` and the program that asks MEMBERSHIP of `[f()]` go to the same builder, and
// neither of them uses the payload-only scan the door replaces. A "the two roads agree" that is not a count is
// a claim about my reading of the code (**ADR 0456**'s lesson, applied here).
func TestTheContainerAnswerElementBuildsTheObjectOnce(t *testing.T) {
	t.Parallel()
	counts := func(t *testing.T, src string) map[string]int {
		t.Helper()
		res, err := Compile(src)
		if err != nil {
			t.Fatalf("compile: %v\n%s", err, src)
		}
		main := inMainBody(t, res.IR)
		out := map[string]int{}
		for _, needle := range []string{"@rt_alloc(", "@rt_set_elem(", "@rt_tag_elem(", "@rt_contains_tagged(", "@rt_contains("} {
			out[needle] = strings.Count(main, needle)
		}
		if strings.Contains(main, "@rt_contains(") {
			t.Fatalf("the payload-only scan is back in a program the object answers:\n%s", main)
		}
		return out
	}
	const decl = "def f():\n    return [1, 2]\n\n"
	printed := counts(t, decl+"print([f()])\n")
	// The same literal with a needle the compiler can name without building one: the two programs build one
	// list apiece, and the only difference between them is the question asked of it.
	asked := counts(t, decl+"print(1 in [f()])\n")
	for _, build := range []string{"@rt_alloc(", "@rt_set_elem(", "@rt_tag_elem("} {
		if printed[build] == 0 {
			t.Fatalf("the printed literal did not go through the container builder (%s): %v", build, printed)
		}
		if asked[build] != printed[build] {
			t.Errorf("one literal, two roads: the printed program calls %s %d times and the membership program %d",
				build, printed[build], asked[build])
		}
	}
	if asked["@rt_contains_tagged("] != 1 {
		t.Errorf("the membership test asked the tagged scan %d times, want 1", asked["@rt_contains_tagged("])
	}
}

// TestAFoldedHaystackStillCostsNoObjectAfterTheElementDoor is the economy guard. The door gained a new way to
// take the object's road; it must not have taken the fold's with it — `1 in [1, 2, 3]` still emits no
// allocation and no call, and a row fails if a literal the fold serves grows a builder.
func TestAFoldedHaystackStillCostsNoObjectAfterTheElementDoor(t *testing.T) {
	t.Parallel()
	for _, src := range []string{
		"print(1 in [1, 2, 3])\n",
		"print(4 not in [1, 2, 3])\n",
		"x = 2\nprint(x in [1, 2, 3])\n",
		"def f():\n    return [1, 2]\n\nprint(len([f()]))\n",
	} {
		src := src
		t.Run(strings.SplitN(src, "\n", 2)[0], func(t *testing.T) {
			t.Parallel()
			res, err := Compile(src)
			if err != nil {
				t.Fatalf("compile: %v\n%s", err, src)
			}
			main := inMainBody(t, res.IR)
			for _, needle := range []string{"@rt_alloc(", "@rt_set_elem(", "@rt_tag_elem(", "@rt_contains("} {
				if strings.Contains(main, needle) {
					t.Errorf("a program the fold serves now asks %s:\n%s", needle, main)
				}
			}
		})
	}
}

// TestWhatTheContainerAnswerElementStillOwesIsFiledNotFixed pins the two defects the sweep found that this
// door did NOT make and does NOT fix — both measured byte-identical on a build of the parent commit, which is
// the only reason they are rows rather than damage. A row here FAILS when the shape starts answering, and the
// row then moves into the answered table above.
func TestWhatTheContainerAnswerElementStillOwesIsFiledNotFixed(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name, src, actual, reference, row string
	}{
		{
			// The count door performs an element's effects (**ADR 0441**, roadmap **Gap R.494**) — but an element
			// whose answer is a container performs none of them: the number is stated and the call is not made,
			// so neither the `print` below nor a raise inside the callee reaches the program.
			"the count door still skips a container answer's effect",
			"def probe():\n    print(\"built\")\n\n    return [1]\n\nprint(len([probe()]))\n",
			"1\n", "built\n1\n", "Gap R.534",
		},
		{
			// The same door, the accident half — and the accident is skipped too, which is the half that makes
			// this the priority-1 class rather than a lost line of output: the element raises `IndexError`, and
			// the program states the count as if the call had never been written.
			"the count door still skips a container answer's raise",
			"def probe():\n    xs = [1]\n\n    y = xs[7]\n\n    return [y]\n\nprint(len([probe()]))\n",
			"1\n", "Traceback (most recent call last): IndexError: list index out of range\n", "Gap R.534",
		},
		{
			// The count door's own negative control, run beside the two rows above: an element whose answer is
			// an INTEGER is performed by **ADR 0441**'s door today. The difference is this row's defect, and it
			// is the answer's kind — so the fix belongs to the element road that asks the container-answer scan,
			// not to the count door's own table.
			"an integer answer's element IS performed (the control)",
			"def probe():\n    print(\"built\")\n\n    return 5\n\nprint(len([probe()]))\n",
			"built\n1\n", "built\n1\n", "Gap R.494 (paid — the control that localises Gap R.534)",
		},
	} {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, err := compileAndRunAny(t, tc.src)
			if err != nil {
				t.Fatalf("the shape refused where the row pins today's answer %q: %v\n%s", tc.actual, err, tc.src)
			}
			if got != tc.actual {
				t.Fatalf("printed %q, want today's %q — the row that owns it is %s (the reference: %q)\n%s",
					got, tc.actual, tc.row, tc.reference, tc.src)
			}
		})
	}
}

// TestTheRecordSourcesTheContainerAnswerElementRowAsksFor is the recorder, not a case: with
// GUSTY_RECORD_CONTAINER_ELEMENT set it writes every source the answered table asks about, in the JSON shape
// `tools/recmerge` reads, and skips. The record is the second witness a compiled answer is judged against and
// a MISSING entry fails the case (**ADR 0302**) — which only bites where a case asks, so the list lives with
// the table that asks it (**Gap R.532**'s rule).
func TestTheRecordSourcesTheContainerAnswerElementRowAsksFor(t *testing.T) {
	out := os.Getenv("GUSTY_RECORD_CONTAINER_ELEMENT")
	if out == "" {
		t.Skip("the recorder: set GUSTY_RECORD_CONTAINER_ELEMENT=<file> to dump these sources for tools/recmerge")
	}
	var srcs []string
	for _, r := range containerAnswerElementRows() {
		srcs = append(srcs, r.src)
	}
	body, merr := marshalGoldenMissing(srcs)
	if merr != nil {
		t.Fatalf("rendering the source list: %v", merr)
	}
	if err := os.WriteFile(out, append(body, '\n'), 0o644); err != nil {
		t.Fatalf("writing %s: %v", out, err)
	}
	t.Logf("wrote %d sources to %s", len(srcs), out)
}

// sourceFuncDefs is the table the compiler builds its `fds` from: the declarations the program's own text
// spells, keyed by name. The gate test hands it to the generator so the element door can ask the declaration
// whether it came from the source or from the backend's own hoisting (**ROAD-R.536**) — the same question the
// compiled leg asks, answered from the same walk (`answer_kind.go`), not from a table this file invented.
func sourceFuncDefs(prog *Program) map[string]*FuncDef {
	fds := map[string]*FuncDef{}
	for _, st := range prog.Stmts {
		if fd, ok := st.(*FuncDef); ok {
			if _, seen := fds[fd.Name]; !seen {
				fds[fd.Name] = fd
			}
		}
	}
	return fds
}
