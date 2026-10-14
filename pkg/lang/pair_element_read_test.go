package lang

import (
	"strings"
	"testing"
)

// pair_element_read_test.go — a container literal's element that is itself a read is stored as the
// (payload, tag) pair that read answers, roadmap Gap R.224.
//
// Four shapes stood in front of one missing half:
//
//	print([{"a": "x"}["a"], 2])   # CPython ['x', 2]   · this backend [1, 2]  — the text's @str_tab position
//	print([["a"][0], 2])          # CPython ['a', 2]   · this backend refused — the one-word sentence
//	print([[1.5, 2][0], 3])       # CPython [1.5, 3]   · this backend refused
//	xs = [1, "a"]
//	print([xs[1], 2])             # CPython ['a', 2]   · this backend refused — Gap R.82's sentence
//
// while `print(xs[1])` had printed `a` for as long as the pair door existed. The print door, the binding
// door, the membership door and the length door all ask a subscript for two words; the container builder
// alone asked for one, and `pairElemPair` — the door that hands over the register the objects wrote —
// admitted only a `*Name`. A slot written that way holds a payload whose meaning lives in a tag nobody
// stored, which is the answer-at-exit-0 this family exists to refuse instead of ship.
//
// Every `want` below is CPython's, read off the reference (the same programs run through `python3 -` by
// `integration/pair_element_read_test.go`, which derives its expectations from the interpreter rather than
// from this file).

func TestALiteralElementThatIsAReadAnswersTheValueItDenotes(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct{ src, want string }{
		// The row's own four shapes.
		{`print([{"a": "x"}["a"], 2])`, "['x', 2]"},
		{`print([["a"][0], 2])`, "['a', 2]"},
		{`print([[1.5, 2][0], 3])`, "[1.5, 3]"},
		{"xs = [1, \"a\"]\nprint([xs[1], 2])", "['a', 2]"},
		// The same read in every position of the three containers, so a builder cannot be fixed one at a
		// time (ADR 0306's lesson, asked of the door ADR 0331 widened).
		{`print({1: {"a": "x"}["a"]})`, "{1: 'x'}"},
		{`print({{"a": "x"}["a"]: 1})`, "{'x': 1}"},
		{`print({{"a": "x"}["a"]})`, "{'x'}"},
		{`print({"k": {"a": "x"}["a"], "n": 2})`, "{'k': 'x', 'n': 2}"},
		// One element per kind the tag carries — the payload is meaningless for each of them on its own.
		{`print([[None][0], True])`, "[None, True]"},
		{`print([[True, False][1], 0])`, "[False, 0]"},
		{`print([["a", "b"][-1], 1])`, "['b', 1]"},
		{`print([[1, 2, 3][1], 4])`, "[2, 4]"},
		{`print([{1: 2}[1], 3])`, "[2, 3]"},
		// A slot of a container the program BUILT, not spelled: only the object knows what it holds.
		{"xs = []\nxs.append([7, 8])\nprint([xs[0][1], 0])", "[8, 0]"},
		{"xs = []\nxs.append(\"s\")\nprint([xs[0], 1])", "['s', 1]"},
		{"d = {}\nd[\"k\"] = \"v\"\nprint([d[\"k\"], 1])", "['v', 1]"},
		{"d = {\"k\": [1, 2]}\nprint([d[\"k\"], 0])", "[[1, 2], 0]"},
		{"xs = [[7, 8]]\nprint([xs[0], 0])", "[[7, 8], 0]"},
		// Deeper than one level: the chain is read one pair at a time and the last answer is the slot.
		{"d = {\"a\": {\"b\": \"z\"}}\nprint([d[\"a\"][\"b\"], 1])", "['z', 1]"},
		{"m = {\"a\": {\"b\": [1, 2]}}\nprint([m[\"a\"][\"b\"][0], 0])", "[1, 0]"},
		{"nested = [[1, \"q\"], [2, \"r\"]]\nprint([nested[1][1], nested[0][1]])", "['r', 'q']"},
		{`print([[{"a": "x"}["a"]], 2])`, "[['x'], 2]"},
		{`print([{"a": "x"}["a"], [1, 2], None, True, 1.5])`, "['x', [1, 2], None, True, 1.5]"},
		// A number read through a subscript keeps answering as a number, in a slot and out of one.
		{"xs = [1, 2]\nprint([xs[1] - 1, 0])", "[1, 0]"},
		{"xs = [1, \"a\", None, 2.5, True, [7]]\nprint([xs[0], xs[1], xs[2], xs[3], xs[4], xs[5]])", "[1, 'a', None, 2.5, True, [7]]"},
		// The list the element lands in is an ordinary list afterwards: bound, measured, subscripted,
		// searched, looped over, rendered, compared.
		{"ys = [{\"a\": \"x\"}[\"a\"], 2]\nprint(ys)", "['x', 2]"},
		{"ys = [{\"a\": \"x\"}[\"a\"], 2]\nprint(len(ys))", "2"},
		{"ys = [{\"a\": \"x\"}[\"a\"], 2]\nprint(ys[0])", "x"},
		{"ys = [{\"a\": \"x\"}[\"a\"], 2]\nprint(\"x\" in ys)", "True"},
		{"ys = [{\"a\": \"x\"}[\"a\"], 2]\nfor v in ys:\n    print(v)", "x\n2"},
		{"n = {\"a\": \"x\"}[\"a\"]\nprint([n, 1])", "['x', 1]"},
		{`print(str([{"a": "x"}["a"], 2]))`, "['x', 2]"},
		{`print(repr([[{"a": "x"}["a"]], 2]))`, "[['x'], 2]"},
		{`print([{"a": "x"}["a"], 2] == ["x", 2])`, "True"},
		{`print([{"a": "x"}["a"], 2] == [1, 2])`, "False"},
		// The fold keeps the shape it reads: an all-integer literal at an integer position is a constant,
		// and the gate declines it so the pair door never opens for it.
		{`print([[1, 2][0], 3])`, "[1, 3]"},
		{`print([1, 2][0])`, "1"},
	} {
		t.Run(strings.ReplaceAll(tt.src, "\n", "; "), func(t *testing.T) {
			t.Parallel()
			if out := strings.TrimRight(compiledOut(t, tt.src), "\n"); out != tt.want {
				t.Fatalf("compiled %q, printed %q, want %q", tt.src, out, tt.want)
			}
		})
	}
}

// The IR shape the row turns on: the element's tag is the REGISTER the objects wrote, not a constant the
// compiler guessed, and the container says its slots are the truth. Delete the register and the slot keeps
// a payload whose tag says `int` — which is precisely the `[1, 2]` this row started from, so these rows fail
// the day someone "simplifies" the builder back to one word (roadmap L11.1's payload-never-without-tag rule,
// ADR 0187).
func TestAPairElementWritesTheTagTheObjectsWrote(t *testing.T) {
	t.Parallel()
	ir := emittedModuleIR(t, `print([{"a": "x"}["a"], 2])`)
	for _, want := range []string{
		"@rt_dict_get_tagged", // the read the dict variable takes — one door, not a second one
		"@rt_dict_value_tag",
		"call void @rt_tag_elem(i32 %t1, i32 0, i32 %", // slot 0's tag is a register
		"call void @rt_set_elem(i32 %t1, i32 0, i32 %", // and its payload is the other one
		"call void @rt_mark_estr(i32 %t1, i32 8)",      // the object says its slots describe it
	} {
		if !strings.Contains(ir, want) {
			t.Fatalf("the module lacks %q:\n%s", want, ir)
		}
	}
	// The sibling slot keeps the constant tag it always had: only the element that is a read needs a
	// register, and a builder that tagged everything from a register would be re-deriving what the
	// spelling already says.
	if !strings.Contains(ir, "call void @rt_tag_elem(i32 %t1, i32 1, i32 0)") {
		t.Fatalf("the plain integer element stopped carrying a constant tag:\n%s", ir)
	}
}

// The fold's own shape must not have been dragged to the heap by the widened door: ADR 0249's gate-and-door
// rule, asserted as a negative control beside the positive rows above.
func TestTheFoldStillOwnsTheReadItCanAnswer(t *testing.T) {
	t.Parallel()
	for _, src := range []string{`print([[1, 2][0], 3])`, `print([{1: 2}[1], 3])`} {
		ir := emittedModuleIR(t, src)
		for _, forbidden := range []string{"@rt_get_elem", "@rt_dict_get_tagged", "@rt_tag_of"} {
			if strings.Contains(mainBodyOf(ir), forbidden) {
				t.Fatalf("%q opened the pair door for a read the fold answers (%s):\n%s", src, forbidden, mainBodyOf(ir))
			}
		}
	}
	// And the fold's plain answer is still a constant with no allocation behind it at all.
	ir := emittedModuleIR(t, `print([1, 2][0])`)
	if strings.Contains(mainBodyOf(ir), "@rt_alloc") {
		t.Fatalf("the all-integer literal read allocated an object:\n%s", mainBodyOf(ir))
	}
}

// The gate asked directly, so a widening or narrowing of the door fails a decision row rather than silently
// changing what every literal in the language does (ADR 0249: the gate and the door answer one question).
func TestThePairElementGateDecidesTheReadTheDoorAnswers(t *testing.T) {
	t.Parallel()
	g := &irGen{}
	intLit := func(v int64) Expr { return &IntLit{Value: v} }
	strLit := func(v string) Expr { return &StrLit{Value: v} }
	ints := &ListLit{Elems: []Expr{intLit(1), intLit(2)}}
	strs := &ListLit{Elems: []Expr{strLit("a"), strLit("b")}}
	mixed := &ListLit{Elems: []Expr{intLit(1), strLit("a")}}
	intDict := &DictLit{Keys: []Expr{intLit(1)}, Vals: []Expr{intLit(2)}}
	strDict := &DictLit{Keys: []Expr{strLit("a")}, Vals: []Expr{strLit("x")}}
	setLit := &SetLit{Elems: []Expr{intLit(1)}}
	// The variable half: `xs` is a list the compiler allocated, `dm` a dict it allocated, `kept` a name the
	// escape analysis kept as a compile-time constant and so has no slot to read.
	g.allocd = map[string]bool{"xs": true, "dm": true, "kept": true}
	g.listVars = map[string]bool{"xs": true}
	g.mixedLists = map[string]bool{"xs": true}
	g.runtimeDicts = map[string]bool{"dm": true}
	for _, tc := range []struct {
		name string
		ix   Expr
		want bool
		why  string
	}{
		{"a position into an all-integer literal", &Index{Obj: ints, Idx: intLit(0)}, false,
			"the payload is the value, so no tag has to travel and the fold keeps it"},
		{"a position into a literal of texts", &Index{Obj: strs, Idx: intLit(0)}, true,
			"the payload is an @str_tab index and only the tag says so"},
		{"a position into a mixed literal", &Index{Obj: mixed, Idx: intLit(1)}, true, "same"},
		{"a negative position", &Index{Obj: strs, Idx: intLit(-1)}, true, "the door normalises it; the fold does not read it"},
		{"a position the literal does not have", &Index{Obj: strs, Idx: intLit(9)}, true,
			"the bounds door raises this one, and it reaches the raise through the pair door"},
		{"an integer key into an all-integer dict", &Index{Obj: intDict, Idx: intLit(1)}, false, "the fold's shape"},
		{"a text key into a dict of texts", &Index{Obj: strDict, Idx: strLit("a")}, true, "the entry's kind is the object's fact"},
		{"a subscript of a list variable", &Index{Obj: &Name{Value: "xs"}, Idx: intLit(0)}, true, "the read ADR 0251 serves"},
		{"a subscript of a dict variable", &Index{Obj: &Name{Value: "dm"}, Idx: strLit("a")}, true, "same door"},
		{"a subscript of a name kept as a compile-time value", &Index{Obj: &Name{Value: "kept"}, Idx: intLit(0)}, false,
			"there is no slot to load, and emitting the load is the module llc rejects (ADR 0192)"},
		{"a subscript of a name the compiler knows nothing about", &Index{Obj: &Name{Value: "stranger"}, Idx: intLit(0)}, false,
			"not a container the run time owns"},
		{"a subscript of a set literal", &Index{Obj: setLit, Idx: intLit(0)}, false,
			"a set has no slots to read; its subscript is the documented member extension"},
		{"a read of a read", &Index{Obj: &Index{Obj: strs, Idx: intLit(0)}, Idx: intLit(1)}, true,
			"the chain is one pair per level (ADR 0251)"},
		{"a name, which is the other pair shape", strLit("a"), false, "not a subscript at all"},
		{"an integer literal element", intLit(3), false, "not a subscript at all"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := g.subscriptIsPairElem(tc.ix); got != tc.want {
				t.Fatalf("subscriptIsPairElem = %v, want %v — %s", got, tc.want, tc.why)
			}
			if got := g.isPairElem(tc.ix); got != tc.want {
				t.Fatalf("isPairElem = %v, want %v — %s", got, tc.want, tc.why)
			}
		})
	}
	// The name half still answers on its own, and the union does not swallow it.
	g.taggedVars = map[string]bool{"n": true}
	g.taggedOrigin = map[string]string{"n": taggedOriginSlot}
	if !g.isPairElem(&Name{Value: "n"}) {
		t.Fatal("a name the pair road bound stopped being a pair element (ADR 0306's row)")
	}
	if g.isPairElem(&Name{Value: "untagged"}) {
		t.Fatal("an ordinary name is not a pair element; the tag has to come from somewhere")
	}
}

// The invariant that made this a defect rather than a missing feature: the element a container stores is the
// value the same read prints. The expectation is the reference's own — asked of CPython by the same case,
// not remembered here — and the second half of the row is that the element road may not refuse a read the
// print road answers (Gap N's rule that an expectation is derived, ADR 0331's literal-vs-variable parity
// asked of the element instead of the subscript).
func TestTheElementRoadIsNeverWorseThanThePrintRoad(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct{ read, element string }{
		{`print({"a": "x"}["a"])`, `print([{"a": "x"}["a"], 2])`},
		{`print(["a"][0])`, `print([["a"][0], 2])`},
		{`print([1.5][0])`, `print([[1.5][0], 2])`},
		{`print([None][0])`, `print([[None][0], 2])`},
		{"xs = [1, \"a\"]\nprint(xs[1])", "xs = [1, \"a\"]\nprint([xs[1], 2])"},
		{"xs = {\"a\": \"x\"}\nprint(xs[\"a\"])", "xs = {\"a\": \"x\"}\nprint([xs[\"a\"], 2])"},
		{"xs = []\nxs.append([7, 8])\nprint(xs[0][1])", "xs = []\nxs.append([7, 8])\nprint([xs[0][1], 2])"},
		{"xs = []\nxs.append(\"s\")\nprint(xs[0])", "xs = []\nxs.append(\"s\")\nprint([xs[0], 2])"},
	} {
		t.Run(tt.element, func(t *testing.T) {
			t.Parallel()
			readOut, readRefused := compiledOutOrRefusal(t, tt.read)
			if readRefused {
				t.Skipf("the print road itself refuses: %s", readOut)
			}
			want, _, err := PythonRun(tt.element + "\n")
			if err != nil {
				t.Fatalf("the reference could not run the program: %v", err)
			}
			elemOut, elemRefused := compiledOutOrRefusal(t, tt.element)
			if elemRefused {
				t.Fatalf("the read printed %q and the element road refused: %s (the reference prints %q)",
					strings.TrimSpace(readOut), elemOut, strings.TrimSpace(want))
			}
			if got := strings.TrimSpace(elemOut); got != strings.TrimSpace(want) {
				t.Fatalf("the element road answered %q, the reference answers %q (the read prints %q)",
					got, strings.TrimSpace(want), strings.TrimSpace(readOut))
			}
		})
	}
}

// What stays refused stays refused, with the missing half named. These are the positions that keep ONE word
// for a whole value: the argument of a call hands the callee an `i32` and has nowhere to put a tag register,
// and the answer for such an element is Gap R.146's sentence, not a payload at exit 0.
func TestTheOneWordPositionsStillRefuseAPairElement(t *testing.T) {
	t.Parallel()
	for _, src := range []string{
		"def id(v):\n    return v\nprint([id({\"a\": \"x\"}[\"a\"]), 1])",
	} {
		t.Run(strings.ReplaceAll(src, "\n", "; "), func(t *testing.T) {
			t.Parallel()
			out, refused := compiledOutOrRefusal(t, src)
			if !refused {
				t.Fatalf("%q answered %q; a one-word position cannot carry the element's kind, and printing the payload is the wrong answer this row had", src, out)
			}
			if !strings.Contains(out, "one word") || !strings.Contains(out, "Gap R.146") {
				t.Fatalf("%q refused without naming the missing half and its row: %s", src, out)
			}
		})
	}
}

// A read inside an element that fails is the program's exception, not the compiler's: the class, the
// sentence and the catchability are the reference's, and exit 2 — the code the contract reserves for a
// compiler bug — is forbidden on every one of them.
func TestATrapInsideALiteralElementIsRaisedNotRefused(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		src            string
		class, message string
		catch          string
	}{
		{`print([{"a": 1}["b"], 2])`, "KeyError", "'b'", "KeyError"},
		{"xs = [1, \"a\"]\nprint([xs[5], 1])", "IndexError", "list index out of range", "IndexError"},
		{`print([[1, 2]["a"], 1])`, "TypeError", "list indices must be integers or slices, not str", "TypeError"},
		{"xs = [1, \"a\"]\nprint([xs[\"a\"], 1])", "TypeError", "list indices must be integers or slices, not str", "TypeError"},
		{"xs = [1, 2]\nprint([xs[0][0], 1])", "TypeError", "'int' object is not subscriptable", "TypeError"},
		{"xs = {\"a\": 1}\nprint([xs[\"a\"][\"b\"], 1])", "TypeError", "'int' object is not subscriptable", "TypeError"},
	} {
		t.Run(tt.src, func(t *testing.T) {
			t.Parallel()
			class, message := jitTrap(t, tt.src)
			if class != tt.class || message != tt.message {
				t.Fatalf("raised %s: %s, want %s: %s", class, message, tt.class, tt.message)
			}
			prog := "try:\n    " + strings.ReplaceAll(tt.src, "\n", "\n    ") + "\nexcept " + tt.catch + ":\n    print(\"caught\")"
			if out := strings.TrimRight(compiledOut(t, prog), "\n"); out != "caught" {
				t.Fatalf("the raise is not catchable from the program's own handler: %q", out)
			}
		})
	}
}

// The residue, pinned with each leg's answer written into the row rather than fixed in this commit: an
// element that is an *operation over* a read still hands the builder one word, the loop that unrolls a
// literal binds the payload, and a container that crosses a `return` keeps Gap R.67's road. Each row fails
// the day the door grows the pair, which is what makes it a ledger rather than a comment (roadmap L11.1).
func TestWhatThePairElementDoorStillCannotReachIsFiledNotFixed(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name     string
		src      string
		compiled string // what this backend answers today
		oracle   string // what CPython answers
		gap      string
	}{
		{
			"str() of a slot read, as an element",
			"print([str({\"a\": \"x\"}[\"a\"]), 1])",
			"[1, 1]", "['x', 1]", "roadmap Gap R.225",
		},
		{
			"str() of a slot read of a mixed list variable, as an element",
			"xs = [1, \"a\"]\nprint([str(xs[1]), 1])",
			"[0, 1]", "['a', 1]", "roadmap Gap R.225",
		},
		{
			"a text concatenation over a slot read, as an element",
			"print([{\"a\": \"x\"}[\"a\"] + \"!\", 1])",
			"[3, 1]", "['x!', 1]", "roadmap Gap R.225",
		},
		{
			"a method call on a slot read, as an element",
			"d = {\"a\": \"x\"}\nprint([d[\"a\"].upper(), 1])",
			"[2, 1]", "['X', 1]", "roadmap Gap R.225",
		},
		{
			"the literal loop unrolled over an element that is a read",
			"for v in [{\"a\": \"x\"}[\"a\"], 2]:\n    print(v)",
			"1\n2", "x\n2", "roadmap Gap R.226",
		},
		{
			"a read of a read of a literal, as an element",
			"print([[1, 2][0][0], 1])",
			"", "TypeError: 'int' object is not subscriptable", "roadmap Gap R.227 (Gap R.214's shape one level in)",
		},
		// The row this table carried for Gap R.67 — `def f(): return [{"a": "x"}["a"], 2]` /
		// `print(f())`, recorded as answering `0` — answers `['x', 2]` now the return door exists, and it
		// moved to the answering table beside the element door that always got it right:
		// TestAPairElementListReturnedFromACallAnswersTheList in container_return_test.go. A row that stops
		// refusing moves; it does not disappear.
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			out, refused := compiledOutOrRefusal(t, tc.src)
			if tc.compiled == "" {
				// The shape has no answer today: it refuses, and the row's job is to notice the day it
				// starts answering something the reference does not.
				if !refused {
					t.Fatalf("%q answered %q where the reference raises %q and this row records a refusal — the door grew, so move the row to the tables above", tc.src, out, tc.oracle)
				}
				return
			}
			if refused {
				t.Fatalf("%q refuses (%s); the row records an answer, and this shape has none — %s", tc.src, out, tc.gap)
			}
			if got := strings.TrimRight(out, "\n"); got != tc.compiled {
				t.Fatalf("%q answered %q, the answer recorded for %s was %q — the door grew, so move the row to the tables above\n(reference: %q)",
					tc.src, got, tc.gap, tc.compiled, tc.oracle)
			}
		})
	}
}

// The record leg: every answered row has to have an answer on record, so a later cycle cannot take the
// gate away without taking the evidence away with it (ADR 0302's rule that a missing record fails).
func TestThePairElementRowsAreRecorded(t *testing.T) {
	t.Parallel()
	for _, src := range []string{
		`print([{"a": "x"}["a"], 2])`,
		`print([["a"][0], 2])`,
		`print([[1.5, 2][0], 3])`,
		"xs = [1, \"a\"]\nprint([xs[1], 2])",
		"xs = {\"a\": \"x\"}\nprint([xs[\"a\"], 2])",
		`print({1: {"a": "x"}["a"]})`,
		`print({{"a": "x"}["a"]: 1})`,
		`print({{"a": "x"}["a"]})`,
		`print([[{"a": "x"}["a"]], 2])`,
		`print([[1, 2][0], 3])`,
		"ys = [{\"a\": \"x\"}[\"a\"], 2]\nprint(ys[0])",
		"try:\n    print([{\"a\": 1}[\"b\"], 2])\nexcept KeyError:\n    print(\"caught\")",
	} {
		t.Run(src, func(t *testing.T) {
			t.Parallel()
			rec := goldenLookup(t, src)
			if rec.Repr == "" && rec.Stdout == "" && rec.Err == "" {
				t.Fatalf("%q has no recorded answer: re-record it before shipping the door that stores it", src)
			}
		})
	}
}

// mainBodyOf is the module's @main alone, so an assertion about "what this program emitted" is not
// satisfied or denied by the runtime library the module carries.
func mainBodyOf(ir string) string {
	start := strings.Index(ir, "define i32 @main(")
	if start < 0 {
		return ir
	}
	rest := ir[start:]
	if end := strings.Index(rest, "\n}\n"); end >= 0 {
		return rest[:end]
	}
	return rest
}
