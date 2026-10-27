package lang

// ADR 0424 · roadmap Gap R.440 — the kind an unrolled loop item carries is recorded once per iteration
// by the `for` road (`codegen_for.go`, `loopElemTag`), and the print door has read that record since
// **Gap R.358**. The container literal standing one pair of parentheses away had never read it: four
// gate arms (`floatSlotExpr`, `boolSlotExpr`, `noneSlotExpr`, and the container arm of
// `literalNeedsTags`) asked the NAME and its own records, so one value was answered twice —
//
//	for k in [None, 1.5]:
//	    print(k)      # None   1.5   ← the read road, which asks the record
//	    print([k])    # [0]    [1]   ← the literal, which asks the name
//
// — at the exit code of success, with `[0]` and `[1]` being the void's payload and the float's box
// handle. The fix is not a new door: it is the record the road already keeps being read by the gates
// beside it too, through ONE predicate (`loopItemTag`, `loopItemIsThis`, `loopItemKindTag`) that the
// builders' own `elemKindTag`/`elemTagFor` now ask as well, so the gate and the door cannot keep two
// readings of one map (**ADR 0249**). Reading the record has a second half that is easy to get wrong,
// and the record-corpus sweep caught it: for `k = None` / `for k in [1, 2]: print([k])` the item's kind
// IS a plain number, a name whose stale void record then reaches `noneSlotExpr` sends the literal onto
// the tagged road and `elemsTaggable` refuses it — so the reading answers "this name is an item"
// whatever the item is, not only "this item has a kind a compiled word cannot render".
//
// What the reading deliberately does NOT reach is named in the last case here and filed in the roadmap,
// not smoothed over: the fold's census over a fractional item, the interpolation field, and the
// compare door's item (**Gap R.416**).

import (
	"strings"
	"testing"
)

// loopItemLiteralFamily is the answering half: every element that reaches a container literal as an
// item of an unrolled `for`, judged on the compiled leg AND the record leg. Every `want` was read off
// CPython 3.12 before the case was written, and every source is on the record — ADR 0302's rule, so an
// answer here is not certified by the backend that earned it.
func loopItemLiteralFamily() []struct{ name, src, want string } {
	return []struct{ name, src, want string }{
		// The program the row is named for, and the item kinds that do not fit one machine word.
		{"the row's own program", "for k in [None, 1.5]:\n    print([k])\n", "[None]\n[1.5]\n"},
		{"a fractional item", "for k in [1.5, 2.5]:\n    print([k])\n", "[1.5]\n[2.5]\n"},
		{"the void item, alone", "for k in [None, None]:\n    print([k])\n", "[None]\n[None]\n"},
		{"a verdict item", "for k in [True, False]:\n    print([k])\n", "[True]\n[False]\n"},
		{"a container item that is a list", "for k in [[1], [2, 3]]:\n    print([k])\n", "[[1]]\n[[2, 3]]\n"},
		{"a container item that is a dict", "for k in [{\"a\": 1}]:\n    print([k])\n", "[{'a': 1}]\n"},
		{"a container item that is a tuple", "for k in [(1, 2)]:\n    print([k])\n", "[(1, 2)]\n"},
		{"a container item that is a set", "for k in [{1}]:\n    print([k])\n", "[{1}]\n"},
		{"every kind in one iterable", "for k in [None, 1.5, True, \"a\", [1], 7]:\n    print([k])\n",
			"[None]\n[1.5]\n[True]\n['a']\n[[1]]\n[7]\n"},
		// The roads that read the built literal back, rather than print it.
		{"the item twice in one literal", "for k in [None, 1.5]:\n    print([k, k])\n", "[None, None]\n[1.5, 1.5]\n"},
		{"the item beside numbers", "for k in [None, 1.5]:\n    print([k, 1, 2])\n", "[None, 1, 2]\n[1.5, 1, 2]\n"},
		{"the item read back out of the slot", "for k in [None, 1.5]:\n    print([k][0])\n", "None\n1.5\n"},
		{"the item concatenated", "for k in [None, 1.5]:\n    print([k] + [1])\n", "[None, 1]\n[1.5, 1]\n"},
		{"the item compared as a list", "for k in [None, 1.5]:\n    print([k] == [None])\n", "True\nFalse\n"},
		{"the item measured", "for k in [None, 1.5]:\n    print(len([k]))\n", "1\n1\n"},
		{"the item behind str", "for k in [None, 1.5]:\n    print(str([k]))\n", "[None]\n[1.5]\n"},
		{"the item, printed and put in a literal at once", "for k in [None, 1.5]:\n    print(k, [k])\n",
			"None [None]\n1.5 [1.5]\n"},
		// The other literal spellings: the same record, asked of a slot in a set and in a dict.
		{"the item in a set literal", "for v in [None, 2.5]:\n    print({v})\n", "{None}\n{2.5}\n"},
		{"the item as a dict value", "for v in [None, 2.5]:\n    print({\"k\": v})\n", "{'k': None}\n{'k': 2.5}\n"},
		{"the item as a dict key", "for v in [None, 1.5]:\n    print({v: \"x\"})\n", "{None: 'x'}\n{1.5: 'x'}\n"},
		{"the item in a literal inside a dict", "for k in [None, 1.5]:\n    print({\"v\": [k]})\n",
			"{'v': [None]}\n{'v': [1.5]}\n"},
		{"a container item nested one level down", "for k in [[1], [2]]:\n    print([[k]])\n", "[[[1]]]\n[[[2]]]\n"},
		// The bindings and nestings the record has to survive.
		{"the item bound to a name first", "for k in [None, 1.5]:\n    ys = [k]\n    print(ys)\n",
			"[None]\n[1.5]\n"},
		{"the item in a nested loop", "for i in [0, 1]:\n    for k in [None, 1.5]:\n        print([k, i])\n",
			"[None, 0]\n[1.5, 0]\n[None, 1]\n[1.5, 1]\n"},
		{"a second loop reuses the name", "for k in [None, 1.5]:\n    print([k])\nfor k in [2, 3]:\n    print([k])\n",
			"[None]\n[1.5]\n[2]\n[3]\n"},
		{"the item of a loop over a name the program built", "xs = [None, 1.5]\nfor k in xs:\n    print([k])\n",
			"[None]\n[1.5]\n"},
		{"a text item, which the record describes already", "for k in [\"a\", \"b\"]:\n    print([k, k])\n",
			"['a', 'a']\n['b', 'b']\n"},
	}
}

// TestALoopItemPutIntoALiteralAnswersWhatTheItemIs is the answering case: the item's own kind, on both
// witness legs. The compiled leg runs the program; the record leg reads the answer on record for that
// exact source, and the two must agree — a missing record fails the case, so this coverage cannot be
// deleted by deleting a record.
func TestALoopItemPutIntoALiteralAnswersWhatTheItemIs(t *testing.T) {
	t.Parallel()
	for _, tc := range loopItemLiteralFamily() {
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

// TestTheNumericLoopItemKeepsTheFoldsStaticArray is the control table and the Definition of Done's
// second clause. The gate gained four arms that read the unroll's record, and a plain number is not one
// of the kinds that record can name: `sum`, `min`, `max` read **ADR 0239**'s static array, and a loop
// whose items are numbers must still fold. The last rows are the shape a record-corpus sweep caught —
// a name whose EARLIER binding wrote the void or a float, rebound by the loop to a number: the record
// says "this name is an item", which is what keeps the stale binding from acquiring the literal.
func TestTheNumericLoopItemKeepsTheFoldsStaticArray(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct{ name, src, want string }{
		{"sum over a numeric item", "for k in [1, 2]:\n    print(sum([k]))\n", "1\n2\n"},
		{"min and max over a numeric item", "for k in [3, 4]:\n    print(min([k]), max([k]))\n", "3 3\n4 4\n"},
		{"sum over a numeric item beside a literal", "for k in [1, 2, 3]:\n    print(sum([k, 1]))\n", "2\n3\n4\n"},
		{"a numeric item used as a number", "for k in [1, 2]:\n    print([k, k * 2])\n", "[1, 2]\n[2, 4]\n"},
		{"the numeric item measured", "for k in [1, 2]:\n    print(len([k]))\n", "1\n1\n"},
		{"the numeric item printed", "for k in [1, 2]:\n    print([k])\n", "[1]\n[2]\n"},
		{"a numeric item after a void binding", "k = None\nfor k in [1, 2]:\n    print([k])\n", "[1]\n[2]\n"},
		{"a numeric item after a verdict binding", "k = True\nfor k in [1, 2]:\n    print([k])\n", "[1]\n[2]\n"},
		{"a numeric item after a container binding", "xs = [7]\nfor xs in [1, 2]:\n    print([xs])\n", "[1]\n[2]\n"},
		{"a float loop followed by a numeric one", "for k in [1.5]:\n    print([k])\nfor k in [1, 2]:\n    print([k])\n",
			"[1.5]\n[1]\n[2]\n"},
		{"the spelled literal, which never needed the record", "print([1, 2, 3])\n", "[1, 2, 3]\n"},
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

	// The same claim as IR, because "the fold still answers" is only true if the literal is still the
	// static array the fold reads: an item that is a number emits no per-slot tag and no mixed printer.
	for _, tc := range []struct {
		name      string
		src       string
		want      []string
		deny      []string
		denialWhy string
	}{
		{
			// TagNone is 3 and TagFloat is 1 (value.go's iota): the item's kind goes in beside its
			// payload, the object says its slots are the truth (bit 8), and the printer is the one that
			// reads the pair. Before this row the literal carried neither the tag nor the bit — the
			// static int array, printed by the single-kind printer, which is how `[0]` reached the screen.
			name: "a void and a fractional item are built tagged",
			src:  "for k in [None, 1.5]:\n    print([k])\n",
			want: []string{
				"call void @rt_tag_elem(i32 %t1, i32 0, i32 3)",
				"call void @rt_mark_estr(i32 %t1, i32 8)",
				"call void @rt_print_list_mixed(i32 %t1, i32 0)",
				"call void @rt_tag_elem(i32 %t7, i32 0, i32 1)",
				"call void @rt_set_elem(i32 %t7, i32 0, i32 %_k.ld1)",
			},
			deny: []string{"call void @rt_print_list(i32 %t1, i32 0)"},
			denialWhy: "the single-kind printer reads every slot as one kind and printed the void's payload " +
				"and the float's box handle — `[0]`, `[1]` (**Gap R.440**)",
		},
		{
			name: "a container item carries its own tag",
			src:  "for k in [[1], [2, 3]]:\n    print([k])\n",
			want: []string{
				"call void @rt_tag_elem(i32 %t3, i32 0, i32 5)",
				"call void @rt_set_elem(i32 %t3, i32 0, i32 %_k.ld1)",
				"call void @rt_print_list_mixed(i32 %t3, i32 0)",
			},
			deny:      nil,
			denialWhy: "",
		},
		{
			name: "a numeric item keeps the fold's static array",
			src:  "for k in [1, 2]:\n    print(sum([k]))\n",
			want: nil,
			deny: []string{"call void @rt_tag_elem(", "call void @rt_print_list_mixed("},
			denialWhy: "the per-slot tag and the mixed printer are the tagged road; a loop whose items are " +
				"numbers still folds to **ADR 0239**'s static array, which is what `sum`, `min` and `max` read",
		},
	} {
		tc := tc
		t.Run("ir_"+tc.name, func(t *testing.T) {
			t.Parallel()
			// The claim is about the code this program wrote for itself: the runtime helpers carry
			// their own tagged-append and mixed-printer calls inside their own definitions, so a denial
			// read off the whole module would be a denial of the runtime.
			ir := mainRegion(t, tc.src)
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

// TestTheLoopItemGateAndTheTagDoorAnswerTheSameItem is **ADR 0249**'s rule asked of this row's two
// doors as a FUNCTION, over every tag the unroll can record: the gate may not send a literal to the
// tagged road the door declines to build, and the two must read the record the same way. The whole
// point of `loopItemTag`/`loopItemKindTag` is that there is one reading; this is what stops the gate
// and the door growing one map's two interpretations again.
func TestTheLoopItemGateAndTheTagDoorAnswerTheSameItem(t *testing.T) {
	t.Parallel()
	k := func() Expr { return &Name{Value: "k"} }
	i := func(v int64) Expr { return &IntLit{Value: v} }
	n := func() Expr { return &NoneLit{} }

	for _, tc := range []struct {
		name string
		tag  int32
		// needsTags: whether a literal holding this item must be built self-describing.
		needsTags bool
	}{
		{"a void item", int32(TagNone), true},
		{"a fractional item", int32(TagFloat), true},
		{"a verdict item", int32(TagBool), true},
		{"a list item", int32(TagList), true},
		{"a dict item", int32(TagDict), true},
		{"a set item", int32(TagSet), true},
		{"an instance item", int32(TagInstance), true},
		// The two kinds a compiled word renders on its own: no second word, no tagged road, and no
		// acquisition of the literal by the record — which is ADR 0239's fold surviving this row.
		{"a numeric item", int32(TagInt), false},
		{"a text item", int32(TagStr), false},
	} {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			g := &irGen{loopElemTag: map[string]int32{"k": tc.tag}}
			if answered, yes := g.loopItemIsThis(k(), tc.tag); !answered || !yes {
				t.Fatalf("the record does not describe its own item: loopItemIsThis = (%v, %v)", answered, yes)
			}
			for _, other := range []int32{int32(TagNone), int32(TagFloat), int32(TagBool), int32(TagList),
				int32(TagDict), int32(TagSet), int32(TagInstance), int32(TagInt), int32(TagStr)} {
				if other == tc.tag {
					continue
				}
				if answered, yes := g.loopItemIsThis(k(), other); !answered || yes {
					t.Fatalf("the record claims item %d is also %d", tc.tag, other)
				}
			}
			if got := g.literalNeedsTags(&ListLit{Elems: []Expr{k()}}); got != tc.needsTags {
				t.Fatalf("literalNeedsTags = %v, want %v", got, tc.needsTags)
			}
			// The four arms, asked of the same element: an arm says yes exactly when the record's kind IS
			// the arm's kind — no arm borrows a kind from a neighbouring item, and none stays silent about
			// the kind it owns.
			for _, arm := range []struct {
				what string
				tag  int32
				ask  func(Expr) bool
			}{
				{"floatSlotExpr", int32(TagFloat), g.floatSlotExpr},
				{"boolSlotExpr", int32(TagBool), g.boolSlotExpr},
				{"noneSlotExpr", int32(TagNone), g.noneSlotExpr},
				{"loopItemIsContainer", -1, g.loopItemIsContainer},
			} {
				got := arm.ask(k())
				want := arm.tag == tc.tag
				if arm.what == "loopItemIsContainer" {
					want = tc.tag == int32(TagList) || tc.tag == int32(TagDict) ||
						tc.tag == int32(TagSet) || tc.tag == int32(TagInstance)
				}
				if got != want {
					t.Fatalf("%s = %v, want %v for item tag %d", arm.what, got, want, tc.tag)
				}
			}
			// **ADR 0249**'s implication, in the direction that can break a program: whatever the gate
			// sends to the tagged road, the door must be able to build it. The converse is not a claim —
			// the door answers the tag question for reasons of its own, and a name the door can label is
			// not a name whose literal needs a second word.
			if g.literalNeedsTags(&ListLit{Elems: []Expr{k()}}) && !g.taggableMixedList(&ListLit{Elems: []Expr{k()}}) {
				t.Fatalf("the gate sends this literal to the tagged road and the door declines to build it")
			}
			if tc.needsTags {
				if tag, ok := g.elemKindTag(k()); !ok || tag != tc.tag {
					t.Fatalf("elemKindTag answered (%d, %v) for an item the record calls %d — the gate built a "+
						"slot the door labels differently", tag, ok, tc.tag)
				}
			}
			// A number beside the item does not hide it, and the spelled kinds answer on their own.
			if !g.literalNeedsTags(&ListLit{Elems: []Expr{i(1), k(), n()}}) && tc.needsTags {
				t.Fatalf("the item's kind was lost beside a number")
			}
		})
	}

	// The record speaks for a NAME the loop bound and for nothing else: an expression, an unbound name,
	// and an empty generator all hand the question back.
	bare := &irGen{}
	if _, ok := bare.loopItemKindTag(k()); ok {
		t.Fatalf("a name no loop bound answered the per-item record")
	}
	tagged := &irGen{loopElemTag: map[string]int32{"k": int32(TagNone)}}
	if _, ok := tagged.loopItemKindTag(&NoneLit{}); ok {
		t.Fatalf("a spelled literal asked the loop's record — the road writes names, not literals")
	}
	if _, ok := tagged.loopItemKindTag(&Name{Value: "other"}); ok {
		t.Fatalf("a name the road never bound answered this item's kind")
	}
}

// TestWhatTheLoopItemLiteralStillCannotAskIsFiledNotFixed pins what this row leaves owed, with today's
// bytes beside the reference's. Each is byte-identical on this row's parent — the arm written here did
// not write them, and did not take them away either.
func TestWhatTheLoopItemLiteralStillCannotAskIsFiledNotFixed(t *testing.T) {
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
			// The same item, asked the identity question: the compare door reads the name's records, not
			// the unroll's, so the void item is not equal to the void.
			name:     "the item compared to the void",
			src:      "for k in [None, 1.5]:\n    print(k == None)\n",
			wantExit: 0, wantOut: "False\nFalse\n",
			dossier: "**Gap R.416** — the reference prints `True`, `False`; the print and the literal now ask " +
				"the item's record and the compare door still does not",
		},
		{
			// The compare door's third spelling, measured beside its own: membership asks the same door
			// the identity and the equality question go through.
			name:     "the item tested for membership in its own iterable",
			src:      "for k in [None, 1.5]:\n    print(k in [None, 1.5])\n",
			wantExit: 0, wantOut: "True\nFalse\n",
			dossier: "**Gap R.416** (the compare door's third spelling) — the reference prints `True`, `True`",
		},
		{
			// The interpolation field: the print door asks the record and the field's own printer does not,
			// so `print(k)` answers `None` while `print(f"{k}")` answers the payload.
			name:     "the item interpolated rather than printed",
			src:      "for k in [None, 1.5]:\n    print(f\"{k}\")\n",
			wantExit: 0, wantOut: "0\n0\n",
			dossier: "**Gap R.445** — the reference prints `None`, `1.5`; the print road reads the item's " +
				"record (**Gap R.358**), the interpolation field reads the name's",
		},
		{
			// The fold, over an item the fold cannot spell: the literal is tagged, and `sum` still reads
			// the payload as a compile-time constant — the box handle arrives as the number it is not.
			name:     "the fold over a fractional item",
			src:      "for k in [1.5, 2.5]:\n    print(sum([k]))\n",
			wantExit: 0, wantOut: "0\n1\n",
			dossier: "**Gap R.446** — the reference prints `1.5`, `2.5`; **ADR 0239**'s fold reads a static " +
				"array and has no arm for an item whose payload is a box handle",
		},
		{
			// The item's own slot, one door earlier than this row: the name once held a float, so the slot
			// the float road declared is a `double`, and the unrolled `for` stores an i32 into it. With
			// opaque pointers the verifier cannot see the mismatch (`store i32 1, i32* %_k` over
			// `%_k = alloca double` answers `ok`), so the answer is whatever the old box left behind. Byte-
			// identical for `print(k)` on this row's parent; `print([k])` was the float's box handle there
			// and is the int payload here, which is the same defect wearing this row's symptom.
			name:     "the item whose name once held a fraction",
			src:      "k = 1.5\nfor k in [1, 2]:\n    print([k])\n",
			wantExit: 0, wantOut: "[1]\n[0]\n",
			dossier: "**Gap R.447** — the reference prints `[1]`, `[2]`; `print(k)` over the same loop prints " +
				"`1.5000000000000002`, `1.5000000000000004` on this row's parent as well, so the slot, not the " +
				"gate, is what is wrong here",
		},
		{
			name:     "the item whose name once held a fraction, printed as itself",
			src:      "k = 1.5\nfor k in [1, 2]:\n    print(k)\n",
			wantExit: 0, wantOut: "1.5000000000000002\n1.5000000000000004\n",
			dossier: "**Gap R.447** — the reference prints `1`, `2`; byte-identical on this row's parent, which " +
				"is the evidence that the gate written here neither caused nor fixed it",
		},
		{
			name:     "the truth of the container the item was put in",
			src:      "for k in [None, 1.5]:\n    print(bool([k]))\n",
			wantExit: 1, wantStderr: "a container's truthiness is its length",
			dossier: "**Gap R.131** — the reference prints `True`, `True`; `bool()` over a container has no " +
				"length door in this backend, and the row that owns it is not this one",
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
