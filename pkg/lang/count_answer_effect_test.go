package lang

import (
	"os"
	"strings"
	"testing"
)

// The count door PERFORMS an element whose answer is a container (roadmap **Gap R.534**, **ADR 0460**).
//
// **ADR 0441** taught the length door to run the elements whose count it reads off the spelling, and the
// lesson of this row is that the door had learned it only for the elements the ONE-WORD value road can
// lower. An element that is a call handing back a container is refused at that road — `Gap R.67`'s sentence,
// "whose word is a heap object and whose kind only this table knows" — and the effects door treated a
// refusal as "nothing to do":
//
//	def probe():
//	    print("built")
//	    return [1]
//	print(len([probe()]))      →  `1`            reference: `built` then `1`
//
//	def g(x):
//	    z = 5
//	    raise ValueError("boom")
//	    return [x, z]
//	print(len([g(1)]))         →  `1` at exit 0  reference: raises ValueError: boom
//
// Both at the exit code of SUCCESS — the class **ADR 0166** ranks above every other defect, because the
// program is told nothing happened and the toolchain says nothing is wrong. The count door had already
// decided the answer; the elements were, to it, decoration. They are not: an element is an operation
// (**Gap R.149**/**ADR 0275**'s rule, one construct over).
//
// The fix is NOT a third call road. `lowerLiteralElementForEffect` asks `containerOperand` — the same
// `*Call` arm **ADR 0333** grew for `print([build(), 3])` and **ADR 0344** narrowed to the positions that
// can hold a handle — and DROPS the handle: a position that keeps no word has no reason to repeat a
// refusal that is about a word (**ADR 0232**: one question, one door, at the altitude that decides). What
// that arm declines leaves the whole attempt and the door keeps the count it answers today, which is
// **ADR 0441**'s own rule kept: a pinned, owned divergence beats a new unowned one.
//
// No row here claims a second engine ran. The answered rows are measured on the compiled leg and against
// `testdata/interpreter-golden.json` (`runGoldenStdout`, `recordTrapOf` — the record leg), and the sources
// first asked after **ADR 0302** retired the AST interpreter are recorded from the reference by
// `tools/recmerge` via this file's own recorder; the CLI twin (`integration/count_answer_effect_test.go`)
// reads every expectation out of a live `python3` inside the case.

// countAnswerProbe is the element this family is about: a call whose ANSWER is a container, so the one-word
// value road has never been able to lower it and — until this row — neither could the count door.
const (
	countAnswerListProbe  = "def probe():\n    print(\"built\")\n\n    return [1]\n"
	countAnswerDictProbe  = "def probe():\n    print(\"built\")\n\n    return {\"k\": 1}\n"
	countAnswerSetProbe   = "def probe():\n    print(\"built\")\n\n    return {1}\n"
	countAnswerTupleProbe = "def probe():\n    print(\"built\")\n\n    return (1, 2)\n"
)

// countAnswerEffectRows are the programs: every position the length door answers from a spelling, with an
// element that is a call handing back a container. `want` is what CPython writes for the same source.
func countAnswerEffectRows() []struct {
	name, src, want string
} {
	return []struct {
		name, src, want string
	}{
		// The row as filed: the line inside the callee is the program's, and the count door had eaten it.
		{"the filed row — a container answer as the only element", countAnswerListProbe + "print(len([probe()]))\n", "built\n1"},
		{"a container answer beside a spelling", countAnswerListProbe + "print(len([probe(), 2]))\n", "built\n2"},
		{"a container answer beside another container answer", countAnswerListProbe + "print(len([probe(), probe()]))\n", "built\nbuilt\n2"},
		{"a container answer one level down", countAnswerListProbe + "print(len([[probe()], 2]))\n", "built\n2"},
		{"a container answer three levels down", countAnswerListProbe + "print(len([[[probe()]]]))\n", "built\n1"},
		{"a container answer in a dict literal's VALUE", countAnswerListProbe + "print(len({\"a\": probe()}))\n", "built\n1"},
		{"a container answer in a dict literal beside a spelling", countAnswerListProbe + "print(len({\"a\": probe(), \"b\": 2}))\n", "built\n2"},
		{"a container answer inside a dict literal inside a list", countAnswerListProbe + "print(len([{\"k\": probe()}]))\n", "built\n1"},
		{"a container answer as a tuple literal's element", countAnswerListProbe + "print(len((probe(), 1)))\n", "built\n2"},
		{"a container answer beside a read", countAnswerListProbe + "print(len([probe(), [7][0]]))\n", "built\n2"},
		// The answer's KIND is the callee's, and the door performs the call whatever it is: **ADR 0457**
		// declined a tuple answer at the SLOTS door (the slot wears the tag of the object it points at), and
		// this position has no slot — no tag, no claim, no reason to decline.
		{"a DICT answer", countAnswerDictProbe + "print(len([probe()]))\n", "built\n1"},
		{"a SET answer", countAnswerSetProbe + "print(len([probe()]))\n", "built\n1"},
		{"a TUPLE answer — declined at the slot door, performed here", countAnswerTupleProbe + "print(len([probe()]))\n", "built\n1"},
		// A body the answer scan could PROVE but not DESCRIBE (its slots are names, not literals) still
		// runs: the scan's silence about the slots was never a statement about the call.
		{"an answer whose slots the scan cannot describe", "def g(x):\n    print(\"g\")\n\n    z = 5\n\n    return [x, z]\n\nprint(len([g(1)]))\n", "g\n1"},
		// A body that only FORWARDS another call's answer — **Gap R.533**'s decline at the element door, and
		// the same answer for the same reason: nobody here stores the handle.
		{"a body that forwards another call's answer", "def inner():\n    print(\"in\")\n\n    return [1, 2]\n\ndef f():\n    return inner()\n\nprint(len([f()]))\n", "in\n1"},
		// A method's container answer, carried across the class boundary by ADR 0430's door.
		{"a method's container answer", "class C:\n    def m(self):\n        print(\"m\")\n\n        return [1]\n\nc = C()\nprint(len([c.m()]))\n", "m\n1"},
		// The positions that ask the count door, one after another. The count is used, tested, bound,
		// interpolated, looped over — and the element's line is the program's in every one of them.
		{"the count is bound and printed", countAnswerListProbe + "n = len([probe()])\nprint(n)\n", "built\n1"},
		{"the count tests a condition", countAnswerListProbe + "if len([probe()]) > 0:\n    print(\"yes\")\n\nelse:\n    print(\"no\")\n", "built\nyes"},
		{"the count is an f-string field", countAnswerListProbe + "print(f\"n={len([probe()])}\")\n", "built\nn=1"},
		{"the count is taken in a loop body", countAnswerListProbe + "for i in range(2):\n    print(len([probe()]))\n", "built\n1\nbuilt\n1"},
		{"the count is taken inside a function body", countAnswerListProbe + "def h():\n    print(\"h\")\n\n    return len([probe(), 1])\n\nprint(h())\n", "h\nbuilt\n2"},
		{"two counts, each performing its own element", countAnswerListProbe + "print(len([probe()]) + len([probe()]))\n", "built\nbuilt\n2"},
		{"the count beside the print of the same literal", countAnswerListProbe + "print(len([probe()]))\nprint([probe()])\n", "built\n1\nbuilt\n[[1]]"},
		{"the count of a sorted literal whose element answers a container", countAnswerListProbe + "print(len(sorted([probe()])))\n", "built\n1"},
		{"the empty literal costs nothing, and the non-empty one performs", countAnswerListProbe + "print(len([]))\nprint(len([probe()]))\n", "0\nbuilt\n1"},
		// The negative control, written beside every row above: the elements are SPELLINGS, so the door
		// states the number and the module carries no builder, no allocation and no call for them.
		{"a container of spellings still costs nothing", "print(len([1, 2, 3]))\nprint(len([[1, 2], [3]]))\n", "3\n2"},
	}
}

// TestTheCountDoorPerformsACallThatHandsBackAContainer is the answering half on both witness legs: the
// compiled module (`compiledOut` runs it through `llc`/`lli`, so a module the assembler rejects cannot pass
// as an answer) and the record's answer for the same source.
func TestTheCountDoorPerformsACallThatHandsBackAContainer(t *testing.T) {
	t.Parallel()
	for _, tt := range countAnswerEffectRows() {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := strings.TrimRight(compiledOut(t, tt.src), "\n"); got != tt.want {
				t.Fatalf("printed %q, the reference prints %q", got, tt.want)
			}
			rec, err := runGoldenStdout(t, tt.src)
			if err != nil {
				t.Fatalf("the record does not answer a program both legs agree on: %v", err)
			}
			if strings.TrimRight(rec, "\n") != tt.want {
				t.Fatalf("the record says %q and the compiled leg says %q for the same source", rec, tt.want)
			}
		})
	}
}

// TestThePerformedAnswerIsPerformedExactlyOnce is the half a fold can get wrong in the other direction: an
// element lowered by a trial road AND by the real one prints twice. The door's scratch buffer is what keeps
// the two in line, so it is asserted in the module's own bytes as well as in the program's.
func TestThePerformedAnswerIsPerformedExactlyOnce(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		name string
		src  string
		call string // the callee's symbol, called once per literal in the module and once on the tool channel
		want string
	}{
		{"one element, one call", countAnswerListProbe + "print(len([probe()]))\n", "@gy_probe", "built\n1"},
		{"two elements, two calls", countAnswerListProbe + "print(len([probe(), probe()]))\n", "@gy_probe", "built\nbuilt\n2"},
		{"a nested element, one call", countAnswerListProbe + "print(len([[probe()], 2]))\n", "@gy_probe", "built\n2"},
		{"a count used twice, two calls", countAnswerListProbe + "print(len([probe()]) + len([probe()]))\n", "@gy_probe", "built\nbuilt\n2"},
	} {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := strings.TrimRight(compiledOut(t, tt.src), "\n"); got != tt.want {
				t.Fatalf("printed %q, the reference prints %q", got, tt.want)
			}
			main := inMainBody(t, mustCompileIR(t, tt.src))
			if n := strings.Count(main, "call i32 "+tt.call+"("); n != strings.Count(tt.want, "built") {
				t.Fatalf("the module calls %s %d times, the program runs it %d:\n%s", tt.call, n, strings.Count(tt.want, "built"), main)
			}
		})
	}
}

// countAnswerTraps are the elements that RAISE from inside the callee. A count door that skips a raising
// element does not lose a line of output — it answers, at the exit code of success, a program the reference
// never lets reach an answer at all.
var countAnswerTraps = []struct {
	name    string
	src     string
	class   string
	message string
	stdout  string // what the callee printed before it raised
}{
	{
		"the filed accident: a slot read past the end, inside the answer",
		"def probe():\n    xs = [1]\n\n    y = xs[7]\n\n    return [y]\n\nprint(len([probe()]))\n",
		"IndexError", "list index out of range", "",
	},
	{
		"a raise written after a binding, before the answer",
		"def g(x):\n    z = 5\n\n    raise ValueError(\"boom\")\n\n    return [x, z]\n\nprint(len([g(1)]))\n",
		"ValueError", "boom", "",
	},
	{
		"a call prints on the way to the raise",
		"def probe():\n    print(\"built\")\n\n    raise ValueError(\"boom\")\n\n    return [1]\n\nprint(len([probe()]))\n",
		"ValueError", "boom", "built\n",
	},
	{
		"the FIRST element's accident wins the order",
		"def boom():\n    raise ValueError(\"first\")\n\n    return [1]\n\ndef other():\n    raise ValueError(\"second\")\n\n    return [2]\n\nprint(len([boom(), other()]))\n",
		"ValueError", "first", "",
	},
	{
		"a division by zero inside the body that answers a container",
		"def probe():\n    x = 1 // 0\n\n    return [x]\n\nprint(len([probe()]))\n",
		"ZeroDivisionError", "integer division or modulo by zero", "",
	},
}

// TestTheCountDoorsContainerAnsweringElementRaises asks the trap family on the compiled leg and of the
// record, and pins the ORDER half (`stdout` holds exactly what the callee printed before it raised) in the
// same pass — the count is never printed, because the reference never reaches the print.
func TestTheCountDoorsContainerAnsweringElementRaises(t *testing.T) {
	t.Parallel()
	for _, tt := range countAnswerTraps {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			class, message := jitTrap(t, tt.src)
			if class != tt.class || message != tt.message {
				t.Fatalf("raised %s: %s, want %s: %s", class, message, tt.class, tt.message)
			}
			res, err := JIT(tt.src, 0)
			if err != nil {
				t.Fatalf("the compiled leg refused a program the reference answers with a raise: %v", err)
			}
			if res.Code == 2 {
				t.Fatalf("the toolchain failed on its own program (exit 2):\n%s", res.Stderr)
			}
			if res.Code != 3 {
				t.Fatalf("exit %d, want the trap class 3; stdout %q", res.Code, res.Output)
			}
			if res.Output != tt.stdout {
				t.Fatalf("the program printed %q before the raise, want %q — the count door answered a program that never reaches it", res.Output, tt.stdout)
			}
			if !strings.Contains(res.Stderr, tt.class+": "+tt.message) {
				t.Fatalf("the raise on the tool channel is not the reference's %s: %s:\n%s", tt.class, tt.message, res.Stderr)
			}
			if rec, trapped := recordTrapOf(t, tt.src); !trapped {
				t.Fatalf("the record ANSWERS this source; the compiled leg raises %s: %s, so one of the two is wrong", tt.class, tt.message)
			} else if rec.ExnType != tt.class || rec.ExnMsg != tt.message {
				t.Fatalf("the record holds %s: %s and the compiled leg raised %s: %s — the reference is the definition, the record is the evidence",
					rec.ExnType, rec.ExnMsg, class, message)
			}
		})
	}
}

// The raises are the program's, so the program can catch them with the handler it wrote — the half a
// host-side refusal could never pass.
func TestTheCountDoorsContainerAnsweringRaisesReachTheProgramsOwnHandler(t *testing.T) {
	t.Parallel()
	for _, tt := range countAnswerTraps {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			at := strings.LastIndex(tt.src, "print(len(")
			if at < 0 {
				t.Fatalf("the row is not shaped the way this case wraps it: %q", tt.src)
			}
			head, call := tt.src[:at], tt.src[at:]
			prog := head + "try:\n    " + strings.TrimRight(call, "\n") + "\nexcept " + tt.class + " as e:\n    print(\"caught\", e)\n"
			want := tt.stdout + "caught " + tt.message
			if out := strings.TrimRight(compiledOut(t, prog), "\n"); out != want {
				t.Fatalf("the raise is not catchable with its sentence: got %q, want %q", out, want)
			}
		})
	}
}

// TestTheCountDoorOfSpellingsStillCostsNothing is the negative control, and the row's real risk: a door
// that performs an element by BUILDING the container would put a builder into every module that only wants a
// digit. The count of a literal of spellings is a constant, and the module says so.
func TestTheCountDoorOfSpellingsStillCostsNothing(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		src  string
		want string
	}{
		{"print(len([1, 2, 3]))", "3"},
		{"print(len([[1, 2], [3, 4]]))", "2"},
		{"print(len({\"a\": 1, \"b\": 2}))", "2"},
		{"print(len([1, 2]) * len([3]))", "2"},
		{"xs = [1, 2, 3]\nprint(len(xs))", "3"},
		{countAnswerListProbe + "print(len([probe()]))", "built\n1"},
	} {
		tt := tt
		t.Run(tt.src, func(t *testing.T) {
			t.Parallel()
			if got := strings.TrimRight(compiledOut(t, tt.src), "\n"); got != tt.want {
				t.Fatalf("answered %q, want the reference's %q", got, tt.want)
			}
		})
	}
	for _, src := range []string{
		"print(len([1, 2, 3]))",
		"print(len([[1, 2], [3, 4]]))",
		"print(len([1, 2]) * len([3]))",
	} {
		src := src
		t.Run("module/"+src, func(t *testing.T) {
			t.Parallel()
			main := inMainBody(t, mustCompileIR(t, src))
			for _, forbidden := range []string{"call i32 @rt_alloc(", "call i32 @rt_list_new(", "call i32 @rt_dict_new(", "call i32 @rt_set_new(", "call i32 @rt_set_elem("} {
				if strings.Contains(main, forbidden) {
					t.Fatalf("the count door built a container just to throw it away (%s):\n%s", forbidden, main)
				}
			}
		})
	}
	// And the answered family DOES carry the callee's call — the claim that the element is performed is a
	// claim about the module, not only about stdout.
	for _, src := range []string{
		countAnswerListProbe + "print(len([probe()]))",
		countAnswerDictProbe + "print(len([probe()]))",
		countAnswerTupleProbe + "print(len([probe()]))",
	} {
		src := src
		t.Run("answered/"+strings.SplitN(src, "\n", 2)[0], func(t *testing.T) {
			t.Parallel()
			main := inMainBody(t, mustCompileIR(t, src))
			if !strings.Contains(main, "call i32 @gy_probe(") {
				t.Fatalf("the element's call is not in `main` — the count door skipped the operation:\n%s", main)
			}
		})
	}
}

// TestTheCountDoorAsksTheContainerDoorAndNotItself is the gate asked DIRECTLY, so a widening of the door
// fails a decision row instead of quietly becoming a second way to lower a call: the permission to perform
// an element comes from the SAME scan `containerOperand` consults (**ADR 0333**, **ADR 0344**), and a callee
// the scan does not name keeps the ordinary road's refusal.
func TestTheCountDoorAsksTheContainerDoorAndNotItself(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		name   string
		src    string
		expect bool // is the callee in the answer scan — the door's whole permission
	}{
		{"a def answering a list", countAnswerListProbe, true},
		{"a def answering a dict", countAnswerDictProbe, true},
		{"a def answering a set", countAnswerSetProbe, true},
		{"a def answering a tuple", countAnswerTupleProbe, true},
		{"a def answering an integer", "def probe():\n    print(\"built\")\n\n    return 5\n", false},
	} {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			prog, perr := Parse(tt.src)
			if perr != nil {
				t.Fatalf("parse: %v", perr)
			}
			described, undescribed := containerAnswerScan(prog)
			g := &irGen{
				containerRet:       described,
				containerRetOpaque: undescribed,
				containerAsked:     map[string]bool{},
				fds:                sourceFuncDefs(prog),
			}
			_, asked := g.containerAnswerOf("probe")
			if asked != tt.expect {
				t.Fatalf("the answer scan named `probe` = %v, want %v (the count door's permission comes from this one table)", asked, tt.expect)
			}
			// …and the door asks THAT question, not a table of its own: a callee the scan names is performed,
			// a callee it does not is left to the ordinary road.
			if got := literalElementAsksToBeRun(&Call{Fn: &Name{Value: "probe"}}); !got {
				t.Fatal("a call element did not ask to be run")
			}
		})
	}
}

// TestWhatTheCountDoorStillSkipsIsFiledNotFixed pins what this row measured and could NOT reach, with
// today's bytes beside the reference's — the shapes stay in the suite and fail the day the door grows
// (**ADR 0343**'s pinned-divergence rule). The pair-argument shape is the one this row deliberately declined:
// asking `pairCallPair` about it was BUILT and MEASURED, and it turned a silent wrong answer into exit 2.
func TestWhatTheCountDoorStillSkipsIsFiledNotFixed(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name, src, actual, reference, row string
	}{
		{
			// The element is a call whose ARGUMENT is itself a container-answering call, so the argument
			// crosses as a (payload, tag) pair — the pair road's, not this one's. The container door declines
			// it, the value road declines the call, and the count door keeps the number: the callee's line is
			// lost and nothing says so.
			"an element whose ARGUMENT is a container answer is still not performed",
			"def k():\n    print(\"k\")\n\n    return [9]\n\ndef f(x):\n    print(\"f\")\n\n    return [1]\n\nprint(len([f(k())]))\n",
			"1\n", "k\nf\n1\n", "Gap R.542",
		},
		{
			// The same crossing, and the reason the pair door is NOT asked here: measured on this row, the
			// float-answering argument was lowered as a `double` into an `i32` parameter —
			// `call i32 @gy_f(i32 %t10)` over `%t10 = fadd double 0.0, 2.5` — and `llc` spent **ADR 0166**'s
			// exit 2 on a program whose only crime was asking for a count. Identical on the build before this
			// commit, which is what keeps it a row rather than damage.
			"an element whose argument answers a float reaches `llc` with a double in an i32 argument",
			"def f(x):\n    print(\"f\")\n\n    return [x]\n\ndef g():\n    return 2.5\n\nprint(len([f(g())]))\n",
			// `actual` is unused for a shape that never becomes a program; `reference` records what CPython says.
			"", "f\n1\n", "Gap R.543",
		},
		{
			// The control that localises the two rows above: an element whose answer is a container IS
			// performed now — this row is the fix, asserted where the owed table can see it.
			"the plain container answer IS performed (the control)",
			countAnswerListProbe + "print(len([probe()]))\n",
			"built\n1\n", "built\n1\n", "Gap R.534 (paid)",
		},
		{
			// A DIFFERENT door's defect, measured beside this one and NOT made by it: the str/concat roads
			// lower their argument more than once, so the element is performed four times where the reference
			// performs it once. Identical on the build before this commit with an INTEGER-answering element
			// (`def p(): print("built")` / `return 1`) — which is what makes it the roads' row and not this
			// door's: the door performs what it is asked to perform, and the roads asked it four times.
			"the str/concat roads lower the count twice, so the element is performed four times (the roads' row)",
			countAnswerListProbe + "print(\"f\" + str(len([probe()])))\n",
			"built\nbuilt\nbuilt\nbuilt\nf1\n", "built\nf1\n", "Gap R.544",
		},
		{
			// The same roads, the simpler spelling, the same over-performance: two callees for one count.
			"the str road alone performs the element twice (the roads' row)",
			countAnswerListProbe + "print(str(len([probe()])))\n",
			"built\nbuilt\n1\n", "built\n1\n", "Gap R.544",
		},
	} {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			res, err := JIT(tc.src, 0)
			if err == nil && res.Code == 2 {
				t.Fatalf("%q reached the assembler and died there (exit 2 — ADR 0166's own code) where this row pins today's answer %q (%s):\n%s",
					tc.src, tc.actual, tc.row, res.Stderr)
			}
			if err != nil {
				if !strings.Contains(err.Error(), "double") && !strings.Contains(err.Error(), "llc") && !strings.Contains(err.Error(), "i32") {
					t.Fatalf("%q failed for a reason this row does not pin (%s): %v", tc.src, tc.row, err)
				}
				return
			}
			if res.Code != 0 {
				t.Fatalf("%q exited %d where this row pins the answer %q (%s):\n%s", tc.src, res.Code, tc.actual, tc.row, res.Stderr)
			}
			if res.Output != tc.actual {
				t.Fatalf("%q printed %q, want today's %q — the row that owns it is %s (the reference: %q)",
					tc.src, res.Output, tc.actual, tc.row, tc.reference)
			}
			if tc.actual != tc.reference && HasGoldenAnswer(tc.src) {
				t.Fatalf("%q is on the record with an answer, although the compiled leg prints %q where the reference prints %q (%s)",
					tc.src, tc.actual, tc.reference, tc.row)
			}
		})
	}
}

// TestWhatTheElementDoorStillOwesIsFiledNotFixed pins two shapes this sweep measured and does NOT fix —
// both byte-identical on a build of the commit before this one, which is the only reason they are rows
// rather than damage. They belong to the literal's ELEMENT door (**ADR 0457**), not to the count door:
// the element stores a handle that ends up pointing at the object being built, and the printer walks the
// object into itself. The reference answers both in one line; here the run is killed and the CLI reports
// **Gap R.203**'s artifact sentence.
func TestWhatTheElementDoorStillOwesIsFiledNotFixed(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name, src, row string
	}{
		{
			// Two bodies answering the SAME container literal: the second one's answer shares the module
			// global the first wrote, and the slot the element door fills ends up holding the object it is
			// part of. `print([f()])` — the first body — answers `[[1]]`; asking the second is the crash.
			"two bodies answering one container literal, the second asked as an element",
			"def f():\n    return [1]\n\ndef g():\n    return [1]\n\nprint([g()])\n",
			"Gap R.545",
		},
		{
			// The same runaway through the class road: a method whose body answers a container registers an
			// answer beside the plain `def`'s, and the element door stores the shared handle.
			"a class's container-answering method beside a container-answering def",
			"def p():\n    return [3]\n\nclass C:\n    def m(self):\n        return \"s\"\n\nprint([p()])\n",
			"Gap R.545",
		},
	} {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			res, err := JIT(tc.src, 0)
			if err == nil && res.Code == 0 {
				t.Fatalf("%q answered %q at exit 0 where this row is filed for a runaway print (%s)", tc.src, firstRunaway(res.Output), tc.row)
			}
			if err == nil && res.Code != 2 {
				t.Logf("%s: exit %d (the run was killed: %s)", tc.row, res.Code, lastLine(res.Stderr))
			}
			if HasGoldenAnswer(tc.src) {
				t.Fatalf("%q is on the record with an answer, although the compiled leg never finishes it (%s)", tc.src, tc.row)
			}
		})
	}
}

// firstRunaway and lastLine keep a pinned runaway out of the failure text: 36 KB of `[` is not something
// a reader can use, and the row is about the shape rather than the byte count.
func firstRunaway(s string) string {
	s = strings.ReplaceAll(s, "\n", " ")
	if len(s) > 48 {
		return s[:48] + "… (the object printing itself until the run is killed)"
	}
	return s
}

func lastLine(s string) string {
	lines := strings.Split(strings.TrimRight(s, "\n"), "\n")
	if len(lines) == 0 {
		return ""
	}
	return lines[len(lines)-1]
}

// TestTheRecordSourcesTheCountAnswerEffectRowAsksFor is the recorder, not a case: with
// GUSTY_RECORD_COUNT_ANSWER_EFFECT set it writes every source the answered table asks about, in the JSON
// shape `tools/recmerge` reads, and skips. A MISSING record entry fails a case (**ADR 0302**), which only
// bites where a case asks — so the list lives with the table that asks it (**Gap R.532**'s rule).
func TestTheRecordSourcesTheCountAnswerEffectRowAsksFor(t *testing.T) {
	out := os.Getenv("GUSTY_RECORD_COUNT_ANSWER_EFFECT")
	if out == "" {
		t.Skip("the recorder: set GUSTY_RECORD_COUNT_ANSWER_EFFECT=<file> to dump these sources for tools/recmerge")
	}
	var srcs []string
	for _, r := range countAnswerEffectRows() {
		srcs = append(srcs, r.src)
	}
	for _, r := range countAnswerTraps {
		srcs = append(srcs, r.src)
	}
	// …and the corpus row itself: a row of `integration/programs/` is judged on the record like every other
	// program (**ADR 0453**'s rule that a conformance row without a record silently stops being judged).
	if body, err := os.ReadFile("../../integration/programs/count_answer_effect.gy"); err == nil {
		srcs = append(srcs, string(body))
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
