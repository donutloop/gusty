// Roadmap **Gap R.435** / **ADR 0421** — the void a SLOT holds is the void its binding wrote, and the
// container literal asks the ELEMENT that question.
//
//	k = None
//	print([k])            #  [None]  ← this backend: [0], at the exit code of success
//	print([k, 1])         #  [None, 1] ← and [0, 1]
//	print([None])         #  [None]  ← the spelled singleton was answered all along
//	ys = [None, 1, "a"]
//	print(ys)             #  [None, 1, 'a'] ← the mixed road was sound too
//
// One question, asked of two neighbours and answered of only one. The tag gate
// (`literalNeedsTags`) asks each element whether the slot describes itself, and its float arm
// (`floatSlotExpr`) and verdict arm (`boolSlotExpr`) have always gone past the spelling into the
// ADR 0172 records — `print([verdict(), 0])` is built tagged because the gate asks what the CALL
// answers, not what the source writes. Its void arm was the bare `isNoneLitExpr`, so a slot holding
// the void THROUGH A BINDING was not a void to the gate: the literal stayed on the static
// `{i32, [N x i32]}` array, whose element reader folds a void-bound name to the payload the void
// happens to carry, and the screen printed the number.
//
// **ADR 0420** gave the READ door exactly this arm two commits ago — `d = {None: 3.5}` / `k = None` /
// `print(d[k])` prints `3.5` — and left the literal beside it, which is why this row is its own row
// and not a clause of that one: the two doors ask the same predicate and were widened separately.
//
// A gate that outruns its door is the failure mode this file spends a case on (ADR 0249's rule). The
// first cut of the void arm sent `for k in [1, 2]: print([k])` — after an earlier `k = None` — to the
// tagged road, where `elemsTaggable` refused the program it had just been told to build: a name the
// unroll gave a per-item tag is THIS item and must stand aside, the same exemption `elemKindTag`'s
// void arm carries. And the void a CALL answers needed the door to know it too, or
// `def f(): pass` / `print([f()])` went from a wrong answer to a refusal — exit 1 instead of exit 0,
// which is a worse witness, not a better one.
package lang

import (
	"strings"
	"testing"
)

// voidSlotBindingFamily is the answering half: every element that reaches a container literal through
// something other than its own spelling, judged on the compiled leg AND the record leg. Every `want`
// was read off CPython 3.12 before the case was written, and every source is on the record — ADR 0302's
// rule, so an answer here is not certified by the backend that earned it.
func voidSlotBindingFamily() []struct{ name, src, want string } {
	return []struct{ name, src, want string }{
		// The program the row is named for, and its immediate siblings.
		{"the void in a name, put into a list literal", "k = None\nprint([k])\n", "[None]\n"},
		{"the void beside a number in one literal", "k = None\nprint([k, 1])\n", "[None, 1]\n"},
		{"the void leading, the number following", "k = None\nprint([1, k])\n", "[1, None]\n"},
		{"the same list through str", "k = None\nprint(str([k]))\n", "[None]\n"},
		{"the void in a name, put into a set literal", "k = None\nprint({k})\n", "{None}\n"},
		{"the void in a name, put into a tuple", "k = None\nprint((k,))\n", "(None,)\n"},
		{"the void as a dict value", "k = None\nprint({1: k})\n", "{1: None}\n"},
		{"the void as a dict key", "k = None\nprint({k: 1})\n", "{None: 1}\n"},
		{"the void one level down", "k = None\nprint([[k]])\n", "[[None]]\n"},
		{"the void inside a dict's list value", "k = None\nprint({\"a\": [k]})\n", "{'a': [None]}\n"},
		// The roads that read the literal back, rather than print it.
		{"the void in a name, measured", "k = None\nprint(len([k]))\n", "1\n"},
		{"the void in a name, tested for membership", "k = None\nprint(None in [k])\n", "True\n"},
		{"the void in a name, looped over", "k = None\nfor v in [k]:\n    print(v)\n", "None\n"},
		{"the void in a name, read back out of the slot", "k = None\nprint([k][0])\n", "None\n"},
		{"the void in a name, concatenated with another list", "k = None\nprint([k] + [1])\n", "[None, 1]\n"},
		{"the void in a name, compared as a list", "k = None\nprint([k] == [None])\n", "True\n"},
		{"the void in a name, put behind a mutation", "k = None\nxs = [k]\nxs.append(1)\nprint(xs)\n", "[None, 1]\n"},
		{"the void and a text in one literal", "k = None\nprint([k, \"a\"])\n", "[None, 'a']\n"},
		{"the void twice in one literal", "k = None\nprint([k, k])\n", "[None, None]\n"},
		{"the void and a number in one set", "k = None\nprint({k, 1})\n", "{None, 1}\n"},
		{"a sort over the void", "k = None\nprint(sorted([k]))\n", "[None]\n"},
		{"the void where the number's twin is answered", "k = None\nprint([k, 1][1])\n", "1\n"},
		{"the void in a name, on the ternary's arm", "k = None\nprint([k] if k == None else [])\n", "[None]\n"},
	}
}

// voidSlotCallFamily is the same question asked of a CALL: the void a body leaves behind — ADR 0172's
// third arm, and the answer of every in-place mutator — has to reach the slot with the same label the
// spelled singleton gets, or the element is built as an integer.
func voidSlotCallFamily() []struct{ name, src, want string } {
	return []struct{ name, src, want string }{
		{"a void body in a list", "def f():\n    pass\nprint([f()])\n", "[None]\n"},
		{"a body that returns the void explicitly", "def f():\n    return None\nprint([f()])\nprint(len([f()]))\n", "[None]\n1\n"},
		{"a void body beside a number", "def f():\n    pass\nprint([f(), 1])\n", "[None, 1]\n"},
		{"a void body one level down", "def f():\n    pass\nprint([[f()]])\n", "[[None]]\n"},
		{"a void body bound to a name", "def f():\n    pass\nxs = [f()]\nprint(xs)\n", "[None]\n"},
		{"a void body in a set", "def f():\n    pass\nprint({f()})\n", "{None}\n"},
		{"a void body in a tuple", "def f():\n    pass\nprint((f(),))\n", "(None,)\n"},
		{"a void body as a dict value", "def f():\n    pass\nprint({1: f()})\n", "{1: None}\n"},
		{"a void body tested for membership", "def f():\n    pass\nprint(None in [f()])\n", "True\n"},
		{"a void body counted", "def f():\n    pass\nprint(len([f(), f()]))\n", "2\n"},
		{"a void body handed back by another body", "def f():\n    pass\ndef g():\n    return [f()]\nprint(g())\n", "[None]\n"},
		{"a dict miss in a list", "print([{\"a\": 1}.get(\"z\")])\n", "[None]\n"},
		{"a mutator's answer in a list", "xs = [1]\nprint([xs.append(2)])\nprint(xs)\n", "[None]\n[1, 2]\n"},
	}
}

// TestAVoidSlotSaysWhatItsBindingWrote is the row's answering half: the element is the void, the source
// does not spell it, and the literal has to find out from the binding — the same ADR 0172 record the
// print door, `str`, truthiness and equality live by.
func TestAVoidSlotSaysWhatItsBindingWrote(t *testing.T) {
	t.Parallel()
	for _, tc := range voidSlotBindingFamily() {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, err := compileAndRun(t, tc.src)
			if err != nil {
				t.Fatalf("the compiled leg refused a program the reference prints %q: %v\n%s", tc.want, err, tc.src)
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
				t.Fatalf("the record reads %q and the compiled leg printed %q\n%s", recorded, got, tc.src)
			}
		})
	}
}

// TestTheVoidACallAnswersIsTheVoidTheSlotCarries is the same row's third arm: the void that arrives as a
// CALL — a body with no value to give back, or a mutator, which answers the void in the reference too.
// The element's side effect is EMITTED and the payload forced to 0, because the tag, not the payload,
// is what the printer reads.
func TestTheVoidACallAnswersIsTheVoidTheSlotCarries(t *testing.T) {
	t.Parallel()
	for _, tc := range voidSlotCallFamily() {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, err := compileAndRun(t, tc.src)
			if err != nil {
				t.Fatalf("the compiled leg refused a program the reference prints %q: %v\n%s", tc.want, err, tc.src)
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
				t.Fatalf("the record reads %q and the compiled leg printed %q\n%s", recorded, got, tc.src)
			}
		})
	}
}

// TestTheLiteralThatNeverMentionedTheVoidIsUntouched is the control table. The gate gained an arm and
// changed no other answer: the spelled singleton, the single-kind numeric literal, the float and text
// bindings ADR 0233 and ADR 0262 paid, the verdict ADR 0259 paid, and the mixed road that was already
// sound all still answer as they did — and a name the LAST binding made a number is a number, which is
// ADR 0172's rule this arm inherits rather than renegotiates.
func TestTheLiteralThatNeverMentionedTheVoidIsUntouched(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct{ name, src, want string }{
		{"the spelled void", "print([None])\n", "[None]\n"},
		{"the mixed sibling the row points at", "ys = [None, 1, \"a\"]\nprint(ys)\n", "[None, 1, 'a']\n"},
		{"a numeric literal", "print([1, 2, 3])\n", "[1, 2, 3]\n"},
		{"a fractional binding", "a = 1.5\nprint([a])\n", "[1.5]\n"},
		{"a text binding", "a = \"z\"\nprint([a])\n", "['z']\n"},
		{"a verdict binding", "k = True\nprint([k])\n", "[True]\n"},
		{"a name rebound away from the void follows the last binding", "k = None\nk = 5\nprint([k])\n", "[5]\n"},
		{"a loop variable is THIS item, not the void before the loop", "k = None\nfor k in [1, 2]:\n    print([k])\n", "[1]\n[2]\n"},
		{"the empty container spelled as a call", "print([set(), list()])\n", "[set(), []]\n"},
	} {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, err := compileAndRun(t, tc.src)
			if err != nil {
				t.Fatalf("the compiled leg refused a program the reference prints %q: %v\n%s", tc.want, err, tc.src)
			}
			if got != tc.want {
				t.Fatalf("printed %q, want %q\n%s", got, tc.want, tc.src)
			}
		})
	}
}

// TestTheVoidGateAndTheTagDoorAnswerTheSameElement is ADR 0249's rule asked of this row's two doors: a
// gate may not send a program down a road its door then declines. The first cut of the void arm
// violated exactly this — `k = None` / `for k in [1, 2]: print([k])` reached the tagged road and
// `elemsTaggable` refused it — and the refusal, not the wrong answer, was the visible symptom.
func TestTheVoidGateAndTheTagDoorAnswerTheSameElement(t *testing.T) {
	t.Parallel()
	i := func(v int64) Expr { return &IntLit{Value: v} }
	s := func(v string) Expr { return &StrLit{Value: v} }
	n := func() Expr { return &NoneLit{} }
	k := func() Expr { return &Name{Value: "k"} }
	v := func() Expr { return &Name{Value: "v"} }
	m := func() Expr { return &Name{Value: "xs"} }

	// The gate, asked of elements the records claim — and of the two names that must stand aside.
	gate := &irGen{
		noneVars:    map[string]bool{"k": true},
		loopElemTag: map[string]int32{"v": int32(TagInt)},
		listVars:    map[string]bool{"xs": true},
	}
	for _, tc := range []struct {
		name string
		el   Expr
		want bool
		why  string
	}{
		{"the spelled void", n(), true, "the arm ADR 0233 paid"},
		{"a name the void record claims", k(), true, "ADR 0172's latest binding, ADR 0421's arm"},
		{"a name the unroll gave a per-item tag", v(), false, "the exemption elemKindTag carries (Gap R.358)"},
		{"a name the container records claim", m(), false, "a handle whose kind the object carries"},
		{"a number", i(1), false, "nothing to tag"},
		{"a text", s("a"), false, "interned, and the gate asks for it elsewhere"},
	} {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := gate.noneSlotExpr(tc.el); got != tc.want {
				t.Errorf("noneSlotExpr = %v, want %v (%s)", got, tc.want, tc.why)
			}
		})
	}

	// The gate and the door, over whole literals: whatever the gate sends to the tagged road, the door
	// must be able to build. A gate that says "needs tags" and a door that says "not taggable" is the
	// refusal this row measured, and no source has to be compiled to catch it here.
	for _, tc := range []struct {
		name string
		lit  *ListLit
	}{
		{"the void through a binding", &ListLit{Elems: []Expr{k()}}},
		{"the void through a binding, beside a number", &ListLit{Elems: []Expr{k(), i(1)}}},
		{"the spelled void", &ListLit{Elems: []Expr{n()}}},
		{"the spelled void and a text", &ListLit{Elems: []Expr{n(), s("a")}}},
		{"a loop item that happens to follow a void binding", &ListLit{Elems: []Expr{v()}}},
		{"a container name", &ListLit{Elems: []Expr{m()}}},
		{"numbers only", &ListLit{Elems: []Expr{i(1), i(2)}}},
	} {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			needsTags := gate.literalNeedsTags(tc.lit)
			if !needsTags {
				return
			}
			if !gate.taggableMixedList(tc.lit) {
				t.Fatalf("the gate sends this literal to the tagged road and the door declines to build it — the " +
					"program is refused for want of an arm in one of the two (ADR 0249's rule)")
			}
		})
	}

	// The tag question itself, asked of the same elements: the void bound is a void, and a void the CALL
	// answers is a void too — the arm whose absence turned a wrong answer into a refusal in the first cut.
	tagged := &irGen{noneVars: map[string]bool{"k": true}}
	if tag, ok := tagged.elemKindTag(&Name{Value: "k"}); !ok || tag != int32(TagNone) {
		t.Fatalf("a name the void record claims answered (%d, %v), want TagNone", tag, ok)
	}
	if tag, ok := tagged.elemKindTag(&Name{Value: "unbound"}); ok && tag == int32(TagNone) {
		t.Fatalf("a name no record claims answered TagNone — the gate would build a void slot nobody wrote")
	}
}

// TestTheVoidSlotIRAsksTheObjectNotTheSpelling pins the emitted IR, because every answer above could in
// principle be reached by folding the literal away. The row's claim is that the literal becomes a HEAP
// object whose SLOTS say what they hold: the void element is tagged, and the module no longer carries a
// static int array for it.
func TestTheVoidSlotIRAsksTheObjectNotTheSpelling(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name      string
		src       string
		want      []string
		deny      []string
		denialWhy string
	}{
		{
			// TagNone is 3 (value.go's iota), the payload is forced to 0, the object says its slots are
			// the truth (bit 8), and the printer is the one that reads the pair.
			name: "the void in a name is built tagged",
			src:  "k = None\nprint([k])\n",
			want: []string{
				"call void @rt_tag_elem(i32 %t1, i32 0, i32 3)",
				"call void @rt_set_elem(i32 %t1, i32 0, i32 0)",
				"call void @rt_mark_estr(i32 %t1, i32 8)",
				"call void @rt_print_list_mixed(i32 %t1, i32 0)",
			},
			deny: []string{"@.lst", "call void @rt_print_list(i32 %t1, i32 0)"},
			denialWhy: "the static int array and the single-kind printer are the roads that printed the " +
				"payload: `[0]` at exit 0 (**Gap R.435**); a void element must reach neither",
		},
		{
			// The same print road, the same literal shape, no void in it: every slot is tagged TagInt and
			// the plain printer answers. The gate gained an arm and did not acquire the numeric literal.
			name: "a numeric literal is untouched",
			src:  "print([1, 2, 3])\n",
			want: []string{
				"call void @rt_set_elem(i32 %t1, i32 2, i32 3)",
				"call void @rt_tag_elem(i32 %t1, i32 2, i32 0)",
				"call void @rt_print_list(i32 %t1, i32 0)",
			},
			deny: []string{"call void @rt_print_list_mixed(i32 %t1, i32 0)"},
			denialWhy: "the mixed printer is the one that walks two words per slot; a literal the records " +
				"still describe as numbers does not need it, and `sum`/`min`/`max` fold from that record (**ADR 0239**)",
		},
	} {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			ir := compileSrcIR(t, tc.src)
			for _, w := range tc.want {
				if !strings.Contains(ir, w) {
					t.Fatalf("emitted IR lacks %q:\n%s", w, ir)
				}
			}
			for _, d := range tc.deny {
				if strings.Contains(ir, d) {
					t.Fatalf("emitted IR carries the denied %q — %s\n%s", d, tc.denialWhy, ir)
				}
			}
		})
	}
}

// TestWhatTheVoidSlotRoadStillCannotReachIsFiledNotFixed pins what this row leaves owed, with today's
// bytes beside the reference's. Each is byte-identical on this row's parent (the HEAD build at 89b47c5
// answers the same), which is the evidence that the arm written here did not write them.
func TestWhatTheVoidSlotRoadStillCannotReachIsFiledNotFixed(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name       string
		src        string
		wantExit   int
		wantOut    string
		wantStderr string
		dossier    string
	}{
		{
			// The same void, one door earlier: the binding is a PARAMETER, and the arm asks the FRAME's
			// ADR 0172 records, which a body's fresh map never received from the module and a call site
			// never wrote. `k = None` / `print([k])` is answered by this row; `def g(v): return [v]` /
			// `print(g(None))` is not, and neither is the module name read from inside a body.
			name:     "the void handed to a body",
			src:      "def g(v):\n    return [v]\nprint(g(None))\n",
			wantExit: 0, wantOut: "[0]\n",
			dossier: "**Gap R.437** — the reference prints `[None]`; the void arm asks the frame's records " +
				"and a parameter's argument was never in them",
		},
		{
			// The other side of the same door: the name IS the module's void-bound one, but the body is
			// compiled with its own records (codegen.go's per-body save/reset), so the gate asks an empty
			// map and the literal stays single-kind.
			name:     "the module's void read from inside a body",
			src:      "k = None\ndef g():\n    print([k])\ng()\n",
			wantExit: 0, wantOut: "[0]\n",
			dossier: "**Gap R.437** — the reference prints `[None]`; the body's frame keeps its own void " +
				"record and the module's never reaches it",
		},
		{
			// The fold door, not the builder: `sum` over a literal that holds the void reduces the payload.
			// The reference raises, so this is the worst shape of the three — a program that should die
			// answers at the exit code of success.
			name:     "the fold over a void element",
			src:      "print(sum([None]))\n",
			wantExit: 0, wantOut: "0\n",
			dossier: "**Gap R.438** — the reference raises " +
				"`TypeError: unsupported operand type(s) for +: 'int' and 'NoneType'`; the fold reads the " +
				"void's payload and adds it (beside **Gap R.71**, whose claim is the element's type)",
		},
		{
			// The ordering the fold cannot ask: the reference refuses to compare the void with a number,
			// and this backend produces an order.
			name:     "a sort over the void and a number",
			src:      "print(sorted([1, None]))\n",
			wantExit: 0, wantOut: "[None, 1]\n",
			dossier: "**Gap R.438** — the reference raises " +
				"`TypeError: '<' not supported between instances of 'NoneType' and 'int'`; the comparator " +
				"asked the payloads and never asked the kinds",
		},
		{
			// The one-word position, refused rather than wrong: the slot now carries a tag the position has
			// no room for, and the door says so. Named here because this row is what put the tag there.
			name:     "a void slot read into a one-word position",
			src:      "k = None\nprint([k][0] == None)\n",
			wantExit: 1, wantStderr: "needs the element's kind to travel with the element",
			dossier: "**Gap R.146** / **ADR 0331** (**Gap R.218**) — the pair door answers print, str, len, " +
				"membership and the loop; the equality position keeps one word and refuses rather than " +
				"compare a payload",
		},
		{
			// The arithmetic position, refused rather than wrong: the plain-int element IS readable, and the
			// fold asks the question of the whole literal instead of the position — so a void sibling is
			// enough to refuse a program whose element the fold could name. Byte-identical on this row's parent.
			name:     "a plain-int position in a literal the fold cannot read whole",
			src:      "k = None\nprint([k, 1][1] + 1)\n",
			wantExit: 1, wantStderr: "list literal elements must be constant integers",
			dossier: "**Gap R.198** — the reference prints `2`; the fold door's untaggable shapes stay " +
				"refusals in words, and this is the void's spelling of one",
		},
		{
			name:     "the truth of a container asked as a call",
			src:      "k = None\nprint(bool([k]))\n",
			wantExit: 1, wantStderr: "a container's truthiness is its length",
			dossier: "**Gap R.131** — the reference prints `True`; `bool()` over a container has no length " +
				"door in this backend, and the row that owns it is not this one",
		},
		{
			// The gate's other missing record, measured beside this row's and left to its own one: a
			// CONTAINER held by a NAME, put in a literal beside a number, is built as though the gate had
			// never been told what the name is. `print([1, [2]])` — the spelled literal — answers, and
			// `print([[xs], 2])` answers, so the door is sound; it is the gate's element census that stops
			// at `isContainerLiteral`.
			name:     "a container the program named, beside a number",
			src:      "xs = [1]\nprint([xs, 2])\n",
			wantExit: 0, wantOut: "[0, 2]\n",
			dossier: "**Gap R.439** — the reference prints `[[1], 2]`; the literal's handle is the payload " +
				"the name carries, printed as the number it is",
		},
		{
			name:     "two containers the program named, in one literal",
			src:      "xs = [1]\nys = [2]\nprint([xs, ys])\n",
			wantExit: 0, wantOut: "[0, 1]\n",
			dossier: "**Gap R.439** — the reference prints `[[1], [2]]`; the two handles are the two heap " +
				"slots, printed as the numbers the allocator handed out",
		},
		// The row that used to sit at the end of this table — `for k in [None, 1.5]:` `print([k])`, the loop
		// item whose own kind the print road read and the literal standing beside it did not — MOVED with
		// **ADR 0424** to `TestALoopItemPutIntoALiteralAnswersWhatTheItemIs` in `loop_item_literal_test.go`,
		// where it is that family's first row: a row that stops refusing moves, it does not disappear
		// (roadmap Gap R.440).
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
				t.Fatalf("%s (%s): exited %d with stdout %q stderr %q — this pin expects exit %d, and an answer "+
					"that arrived here means the row was closed and this table did not follow\n%s",
					tc.name, tc.dossier, res.Code, res.Output, res.Stderr, tc.wantExit, tc.src)
			}
			if tc.wantOut != "" && res.Output != tc.wantOut {
				t.Fatalf("%s (%s): printed %q, want %q", tc.name, tc.dossier, res.Output, tc.wantOut)
			}
			if tc.wantStderr != "" && !strings.Contains(res.Stderr+res.Output, tc.wantStderr) {
				t.Fatalf("%s (%s): stderr %q does not carry %q", tc.name, tc.dossier, res.Stderr, tc.wantStderr)
			}
		})
	}
}
