package lang

import (
	"errors"
	"strings"
	"testing"
)

// forwarded_answer_test.go — the element, key and member of a container the run time BUILDS is a VALUE, and
// the kind word written beside it in the heap's tag array is the kind the ANSWER means (roadmap
// **Gap R.502**, **ADR 0273**'s element position, **ADR 0232**'s self-describing slots, **ADR 0334**'s
// call-result door).
//
// The case, in the compiler's own words. A comprehension the run time builds puts each element into a heap
// slot, and the KIND it wrote there came from the element EXPRESSION. An element that is a call whose body
// hands one of its own parameters straight back — `def g(v): return v` — spells no kind at all: the table
// fell through to Int, so a text landed in the slot wearing the tag of a number, and a dict comprehension's
// KEY printed its interned id instead of the text.
//
//	def g(v):
//	    return v
//	print([g(x) for x in [1, "a", 2.5, None]])   # CPython: [1, 'a', 2.5, None]
//	                                            # this toolchain: [1, 0, 1, 0]
//	print({g(x): 1 for x in ["a", "b"]})         # CPython: {'a': 1, 'b': 1}
//	                                            # this toolchain: {0: 1, 1: 1}
//
// A hand-off function has no answer of its own: its answer IS the caller's argument, unchanged. So the kind
// is asked of the ARGUMENT, at the call site, where the caller's own expression knows it
// (`forwardedParameterOperand`), and the answer travels to the store through the tagged door that already
// exists — `rt_append_tagged`, `rt_set_add_tagged`, `rt_dict_put_tagged` — and never through the untagged
// one that guesses.
//
// The permission is as important as the door. A hand-off is a function whose EVERY `return` names one
// parameter and whose body never rebinds it, reached through a module-level `def`; a body that computes its
// answer (`return v + 0`) keeps **Gap R.146**'s refusal, because an element built from a tag this pass cannot
// read prints a float box's address as a number — the wrong answer the refusal exists to prevent.
//
// Every expectation below is CPython's, checked at the record leg (`testdata/interpreter-golden.json`) and
// through the CLI against a live `python3` in `integration/forwarded_answer_test.go`.

const handOffDef = "def g(v):\n    return v\n\n"

// forwardedAnswerCases are the container positions a hand-off answer reaches: a list element, a dict entry,
// a set member, an `append`, an assigned slot — the positions ADR 0273 lists as reading BOTH words of an
// answer that arrives in a pair, and the positions whose guessed kind word this row replaces. The last rows
// are the door's negative controls: an answer with a kind of its own wears its OWN kind, whatever the
// argument says.
var forwardedAnswerCases = []struct {
	name string
	src  string
	want string
}{
	{
		"a list element the hand-off hands back keeps the double's kind",
		handOffDef + "print([g(2.5), 1])",
		"[2.5, 1]",
	},
	{
		"a list element the hand-off hands back keeps the text's kind",
		handOffDef + "print([g(\"a\"), 1])",
		"['a', 1]",
	},
	{
		"a list element the hand-off hands back keeps the void's kind",
		handOffDef + "print([g(None), 1])",
		"[None, 1]",
	},
	{
		"a list element the hand-off hands back keeps a list's kind",
		handOffDef + "print([g([1, 2]), 3])",
		"[[1, 2], 3]",
	},
	{
		"a list element the hand-off hands back keeps a tuple's kind",
		handOffDef + "print([g((1, 2)), 3])",
		"[(1, 2), 3]",
	},
	{
		"a dict entry whose VALUE the hand-off hands back prints the value, not an id",
		handOffDef + "print({1: g(None)})",
		"{1: None}",
	},
	{
		"a set member the hand-off hands back prints the member, not an id",
		handOffDef + "print({g(None)})",
		"{None}",
	},
	{
		"a set member the hand-off hands back keeps a text's kind",
		handOffDef + "print({g(\"a\")})",
		"{'a'}",
	},
	{
		"append stores what the hand-off handed back",
		handOffDef + "xs = []\nxs.append(g(None))\nprint(xs)",
		"[None]",
	},
	{
		"a slot assignment stores what the hand-off handed back",
		handOffDef + "xs = [0]\nxs[0] = g(2.5)\nprint(xs)",
		"[2.5]",
	},
	{
		// The kind is read from the CALLER'S argument, so it survives the spellings that hide it: a keyword,
		// and a default the call leaves out. A table keyed by the parameter could answer neither.
		"the kind comes from the keyword argument, not from the parameter",
		handOffDef + "print([g(v=2.5), 1])",
		"[2.5, 1]",
	},
	{
		"the kind comes from the DEFAULT when the call leaves the parameter out",
		"def g(v = 2.5):\n    return v\n\nprint([g(), 1])",
		"[2.5, 1]",
	},
	{
		// The three spellings of the same omission: a void, a list and a dict default. Each was pinned as
		// today's `0` in `lambda_default_param_test.go` and `lambda_answer_test.go` (**Gap R.277**, **Gap R.171**)
		// and moved here when the door that reads the argument started reading the default too.
		"a DEFAULT the body hands back keeps the void's kind",
		"def d(y=None):\n    return y\n\nprint(d())",
		"None",
	},
	{
		"a DEFAULT the body hands back keeps a list's kind",
		"def d(xs=[]):\n    return xs\n\nprint(d())",
		"[]",
	},
	{
		"a DEFAULT the body hands back keeps a dict's kind",
		"def d(w={}):\n    return w\n\nprint(d())",
		"{}",
	},
	{
		"the void handed to a hand-off by an applied lambda is the void, not the digit 0",
		"def f(v):\n    return v\n\nprint(f((lambda: None)()))",
		"None",
	},
	{
		"the void handed to a hand-off by a hoisted def is the void too — the parity the row claimed, at the right byte",
		"def a1():\n    return None\n\ndef f(v):\n    return v\n\nprint(f(a1()))",
		"None",
	},
	{
		"the kind comes from the argument the OTHER parameter's position hands back",
		"def g(v, w):\n    return w\n\nprint([g(2.5, \"a\"), 1])",
		"['a', 1]",
	},
	// Negative controls: the door reads the argument only when the body has nothing else to answer with.
	{
		"a body that answers with a number of its own wears the number's kind",
		"def g(v):\n    return 1\n\nprint([g(2.5), 1])",
		"[1, 1]",
	},
	{
		"a body that builds its answer wears the answer's kind, not the argument's",
		"def g(v):\n    return v + v\n\nprint([g(1), 1])",
		"[2, 1]",
	},
	{
		"an integer hand-off in an integer container is the control that nothing else moved",
		handOffDef + "print([g(x) for x in [1, 2, 3]])",
		"[1, 2, 3]",
	},
	{
		"a comprehension with no call at all still answers from the fold",
		"print([x * 2 for x in [1, 2, 3]])",
		"[2, 4, 6]",
	},
}

// TestAHandOffAnswerWearsItsArgumentsKindInAContainerSlot is the answering half: the compiled leg first, then
// the record leg, for every slot. `runGoldenStdout` is not optional here — **Gap R.502**'s row says the
// answer must live on the record, and the recorder wrote these entries only after the reference leg and the
// compiled leg agreed.
func TestAHandOffAnswerWearsItsArgumentsKindInAContainerSlot(t *testing.T) {
	t.Parallel()
	for _, tt := range forwardedAnswerCases {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			out := strings.TrimRight(compiledOut(t, tt.src), "\n")
			if out != tt.want {
				t.Fatalf("%q printed %q, the reference prints %q", tt.src, out, tt.want)
			}
			rec, err := runGoldenStdout(t, tt.src)
			if err != nil {
				t.Fatalf("the record does not answer a program both legs agree on: %v", err)
			}
			if strings.TrimRight(rec, "\n") != tt.want {
				t.Fatalf("the record says %q and the compiled leg says %q for the same source", rec, out)
			}
		})
	}
}

// builtComprehensionHandOffCases are the rows **Gap R.502** was filed with. They move out of
// `comp_loop_truth_test.go`'s filed tables into this answering one: the element, KEY and member of a
// comprehension the run time builds, where what the builder stores came through a hand-off.
var builtComprehensionHandOffCases = []struct {
	name string
	src  string
	want string
}{
	{
		"the element of a built list comprehension keeps each kind",
		handOffDef + "print([g(x) for x in [1, \"a\", 2.5, None]])",
		"[1, 'a', 2.5, None]",
	},
	{
		"the loop item of that comprehension prints each value",
		handOffDef + "for v in [g(x) for x in [1, \"a\", 2.5, None]]:\n    print(v)",
		"1\na\n2.5\nNone",
	},
	{
		"the KEY of a built dict comprehension keeps its kind",
		handOffDef + "print({g(x): 1 for x in [\"a\", \"b\"]})",
		"{'a': 1, 'b': 1}",
	},
	{
		"looping the keys of a built dict comprehension prints the keys",
		handOffDef + "for k in {g(x): 1 for x in [\"a\", \"b\"]}:\n    print(k)",
		"a\nb",
	},
	{
		"the VALUE of a built dict comprehension keeps its kind",
		handOffDef + "print({1: g(x) for x in [\"a\", 2.5]})",
		"{1: 2.5}",
	},
	{
		"the member of a built set comprehension keeps its kind",
		handOffDef + "print({g(x) for x in [1, 2.5]})",
		"{1, 2.5}",
	},
	{
		"a hand-off element read back out of a built comprehension by position keeps its kind",
		handOffDef + "print([g(x) for x in [1, \"a\", 2.5]][1])",
		"a",
	},
	{
		// The count of a built comprehension and the element inside it are the same object's answers, so the
		// two roads cannot disagree about what is in there (**ADR 0442**'s one-object rule).
		"the count and the element of one built comprehension agree",
		handOffDef + "print([g(x) for x in [1, \"a\", 2.5]][1] == \"a\" and len([g(x) for x in [1, \"a\", 2.5]]) == 3)",
		"True",
	},
	{
		"a text handed back inside a built comprehension is not a number",
		handOffDef + "for v in [g(x) for x in [\"a\"]]:\n    print(v)",
		"a",
	},
}

func TestAComprehensionTheRunTimeBuildsStoresWhatTheHandOffHandedBack(t *testing.T) {
	t.Parallel()
	for _, tt := range builtComprehensionHandOffCases {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			out := strings.TrimRight(compiledOut(t, tt.src), "\n")
			rec, err := runGoldenStdout(t, tt.src)
			if err != nil {
				t.Fatalf("the record does not answer a program both legs agree on: %v", err)
			}
			rec = strings.TrimRight(rec, "\n")
			if out != tt.want {
				t.Fatalf("%q printed %q, the reference prints %q", tt.src, out, tt.want)
			}
			if rec != out {
				t.Fatalf("the record says %q and the compiled leg says %q for the same source", rec, out)
			}
		})
	}
}

// TestTheHandOffAnswerReachesTheRunTimeByTheTaggedDoor is the module-shape half of the contract. The untagged
// doors — `rt_append`, `rt_set_add`, `rt_dict_put` — take a payload and NO kind, which is exactly why they
// guessed; every row names the tagged door, so the answer cannot quietly fall back to the guessing one. The
// `rt_mark_estr` row is the other half of the same promise: a slot whose payload means nothing without its
// tag has to be able to SAY its tag, and a builder that never marked the slot prints a stale kind.
func TestTheHandOffAnswerReachesTheRunTimeByTheTaggedDoor(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		name    string
		src     string
		needles []string
	}{
		{
			"a list element that came through a hand-off is appended WITH its kind",
			handOffDef + "print([g(2.5), 1])",
			[]string{"@rt_append_tagged("},
		},
		{
			"a set member that came through a hand-off is added WITH its kind",
			handOffDef + "print({g(2.5), 1})",
			[]string{"@rt_set_add_tagged("},
		},
		{
			"a dict entry whose value came through a hand-off is stored WITH its kind",
			handOffDef + "print({1: g(2.5)})",
			[]string{"@rt_dict_put_tagged("},
		},
		{
			"append stores a hand-off answer WITH its kind",
			handOffDef + "xs = []\nxs.append(g(2.5))\nprint(xs)",
			[]string{"@rt_append_tagged("},
		},
		{
			"the element of a built comprehension is stored in a SELF-DESCRIBING slot",
			handOffDef + "print([g(x) for x in [1, 2.5]])",
			// bits 8 is the mixed/self-describing mark the builder sets for a slot whose payload is not its
			// own kind; the tagged append is the store that hands the kind over with the payload.
			[]string{"@rt_mark_estr(", "@rt_append_tagged("},
		},
		{
			"the loop item of a built comprehension is printed BY KIND",
			handOffDef + "for v in [g(x) for x in [1, \"a\"]]:\n    print(v)",
			[]string{"@rt_print_mixed_value("},
		},
	} {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			ir := compileSrcIR(t, tt.src)
			for _, needle := range tt.needles {
				if !strings.Contains(ir, needle) {
					t.Fatalf("the module has no %q — this slot was written without a kind word\n%s", needle, tt.src)
				}
			}
		})
	}
}

// TestAHashableHandOffAnswerIsNotRefusedAsUnhashable is the trap half. The kind word now reaches the
// hashability guard, so an answer that hands back a LIST, a DICT or a SET raises the reference's own
// `TypeError: unhashable type: ...` at the moment the slot is written — and an answer that hands back a
// TUPLE must NOT, because a tuple of hashables hashes. A tag cannot make that distinction on its own: a
// tuple and a list share the heap's layout (**ADR 0361**), so the AST gets the last word, and it is asked of
// the hand-off's ARGUMENT, the only place the tuple's spelling survives the call (**Gap R.81**).
func TestAHashableHandOffAnswerIsNotRefusedAsUnhashable(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		name string
		src  string
		want string // what the program prints, when it prints
		// trapClass/trapMsg are the exception the reference raises instead.
		trapClass string
		trapMsg   string
	}{
		{
			"a hashable tuple handed back is a set member",
			handOffDef + "print({g((1, 2))})",
			"{(1, 2)}", "", "",
		},
		{
			"a hashable tuple handed back is a dict key",
			handOffDef + "print({g((1, 2)): 1})",
			"{(1, 2): 1}", "", "",
		},
		{
			"a spelled hashable tuple is a dict key",
			"print({(1, 2): 5})",
			"{(1, 2): 5}", "", "",
		},
		{
			"a handed-back list is refused as a set member",
			handOffDef + "print({g([1, 2])})",
			"", "TypeError", "unhashable type: 'list'",
		},
		{
			"a handed-back dict is refused as a set member",
			handOffDef + "print({g({\"k\": 1})})",
			"", "TypeError", "unhashable type: 'dict'",
		},
		{
			"a handed-back set is refused as a dict key",
			handOffDef + "print({g({1, 2}): 1})",
			"", "TypeError", "unhashable type: 'set'",
		},
		{
			"a handed-back list is refused as a built set comprehension's member",
			handOffDef + "print({g([1, 2]) for x in [1]})",
			"", "TypeError", "unhashable type: 'list'",
		},
		{
			// The member the kind table used to call an integer: the payload is a heap handle, and a set that
			// holds an address is a set the program can never look up again.
			"a handed-back container VARIABLE is refused as a set member",
			handOffDef + "xs = [1]\nprint({g(xs), 2})",
			"", "TypeError", "unhashable type: 'list'",
		},
		{
			// The spelled spellings of the same question. The guard lives on the road that stores a tag, and the
			// gate that opens that road never admitted a container member, so all three printed the container
			// the reference refuses to hash (**Gap R.492**'s family, **Gap R.81**'s rule).
			"a spelled list is refused as a set member",
			"print({[1, 2]})",
			"", "TypeError", "unhashable type: 'list'",
		},
		{
			"a spelled dict is refused as a set member",
			"print({{\"k\": 1}})",
			"", "TypeError", "unhashable type: 'dict'",
		},
		{
			"a spelled set is refused as a set member",
			"print({{1, 2}})",
			"", "TypeError", "unhashable type: 'set'",
		},
		{
			"a comprehension is refused as a set member",
			"print({{x for x in [1, 2]}})",
			"", "TypeError", "unhashable type: 'set'",
		},
	} {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if tt.trapClass == "" {
				out := strings.TrimRight(compiledOut(t, tt.src), "\n")
				if out != tt.want {
					t.Fatalf("%q printed %q, the reference prints %q", tt.src, out, tt.want)
				}
				rec, err := runGoldenStdout(t, tt.src)
				if err != nil {
					t.Fatalf("the record does not answer a program both legs agree on: %v", err)
				}
				if strings.TrimRight(rec, "\n") != tt.want {
					t.Fatalf("the record says %q, the reference prints %q", rec, tt.want)
				}
				return
			}
			class, message := jitTrap(t, tt.src)
			if class != tt.trapClass || message != tt.trapMsg {
				t.Fatalf("%q raised %s: %s; the reference raises %s: %s", tt.src, class, message, tt.trapClass, tt.trapMsg)
			}
			err := goldenRunError(t, tt.src)
			if err == nil {
				t.Fatalf("%q has no recorded trap, although the compiled run raises %s: %s", tt.src, class, message)
			}
			var te *TrapError
			if errors.As(err, &te) {
				if te.ExnType != tt.trapClass || te.ExnMsg != tt.trapMsg {
					t.Fatalf("the record traps %s: %s; the reference raises %s: %s", te.ExnType, te.ExnMsg, tt.trapClass, tt.trapMsg)
				}
			} else if !strings.Contains(err.Error(), tt.trapMsg) {
				t.Fatalf("the record's failure %q does not carry %q", err, tt.trapMsg)
			}
		})
	}
}

// handOffFiledRows is what this door deliberately does not reach, pinned with today's bytes beside the
// reference's. Each is identical on the build before **ADR 0445** and on it, which is what makes it a row
// rather than damage from it (**Gap R.204**'s rule about claims that cannot be re-measured).
var handOffFiledRows = []struct {
	name     string
	src      string
	compiled string // what this toolchain does today: the refusal's words, or the answer it prints
	want     string // what CPython prints
	refusal  bool
	row      string
}{
	{
		// A parameter the body REBINDS has no hand-off to answer with — the same reason **ADR 0356**'s
		// constant-folding door refuses: the value the `return` names is no longer the one the caller passed.
		"a parameter the body rebinds is not a hand-off",
		"def g(v):\n    v = 7\n    return v\n\nprint([g(2.5), 1])",
		"this body's answer has no tag to travel with", "[7, 1]", true,
		"roadmap Gap R.146 (a position that keeps one word for a computed answer)",
	},
	{
		// The body computes: `v + 0` is an arithmetic answer whose kind is a run-time fact of the callee's
		// frame, and the print door gives this shape the same refusal.
		"a body that computes its answer from the parameter is not a hand-off",
		"def g(v):\n    return v + 0\n\nprint([g(2.5), 1])",
		"the tag has nowhere to go", "[2.5, 1]", true,
		"roadmap Gap R.146 (the same one-word position, seen identically at print)",
	},
	{
		// The iterable is a mixed list in a VARIABLE, so each ITEM arrives as a (payload, tag) pair the loop
		// bound, and handing the hand-off's parameter that pair is the one-word position that still refuses
		// — the same sentence, word for word, on the build before this commit and on it.
		"a loop item whose kind the loop bound is a one-word position this door does not widen",
		handOffDef + "xs = [1, \"a\", 2.5]\nprint([g(x) for x in xs])",
		"has nowhere to put the second one", "[1, 'a', 2.5]", true,
		"roadmap Gap R.146 (the tagged value word a call argument has nowhere to keep)",
	},
	{
		// Two inner lists built side by side are read with ONE slot tag: the handle the hand-off handed back
		// is read with the other kind's tag and answers the neighbouring slot. `9605949` printed `[0, 4]` for
		// the same slot, so neither answer is right and the row outlives this cycle. The two programs that CRASH
		// (`{g("a"), 1}` for Gap R.503, and `[g([1, 2]), g({"k": 2}), g((1, 3))]` for Gap R.504) are pinned in
		// those rows and NOT here — a table would run a crashing program on every gate.
		"two nested lists built side by side are read with one slot tag",
		handOffDef + "print([[g([1, 2]), 3], [g(None), 4]])",
		"[[None, 4], [None, 4]]", "[[[1, 2], 3], [None, 4]]", false,
		"roadmap Gap R.504 (a handle read with the other kind's tag)",
	},
	{
		// A method's first parameter is `self`, and the road to a method's answer is the method call's:
		// **ADR 0430**/**Gap R.453** own that round, and the element keeps the answer it had before.
		"a method's hand-off is the method road's turn, not this one",
		"class C:\n    def m(self, v):\n        return v\n\nc = C()\nprint([c.m(2.5), 1])",
		"[2, 1]", "[2.5, 1]", false,
		"roadmap Gap R.453 (the method answer direction)",
	},
}

// TestWhatTheHandOffDoorStillOwesIsFiledNotFixed keeps the residue honest: the door did not widen past its
// boundary, and the shapes it refused before are refused for the SAME words, not for new ones.
func TestWhatTheHandOffDoorStillOwesIsFiledNotFixed(t *testing.T) {
	t.Parallel()
	for _, tt := range handOffFiledRows {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			out, refused := compiledOutOrRefusal(t, tt.src)
			if tt.refusal {
				if !refused {
					t.Fatalf("%q answered %q; the row says this shape is REFUSED, and an answered wrong is worse\n"+
						"than the refusal the row records (%s)", tt.src, out, tt.row)
				}
				if !strings.Contains(out, tt.compiled) {
					t.Fatalf("the refusal is %q, which does not carry %q\nthe row: %s", out, tt.compiled, tt.row)
				}
				return
			}
			if refused {
				t.Fatalf("%q refused with %q; the row records the answer %q\n%s", tt.src, out, tt.compiled, tt.row)
			}
			if got := strings.TrimRight(out, "\n"); got != tt.compiled {
				t.Fatalf("the filed row says this toolchain prints %q, it printed %q\nthe row: %s",
					tt.compiled, got, tt.row)
			}
		})
	}
}
