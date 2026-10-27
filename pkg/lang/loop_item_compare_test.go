package lang

// ADR 0426 · roadmap Gap R.416 — the comparison asked of an unrolled loop ITEM asks the ITEM.
//
// An unrolled `for` over a literal list binds its variable twice over, once per element, and records the
// element's kind once per iteration in `loopElemTag` (**ADR 0233**, `codegen_for.go`). Two readers had been
// told about that record: the print door since **Gap R.358** and the truth door since **ADR 0388**. Every
// other door consulted the notebook `isTaggedCompareSide` keeps — and the literal loop's item has no entry
// there, because the road writes no `_tag` slot for it. So one value was answered twice:
//
//	for v in [None, "a"]:
//	    print(v)          # None   a     ← the read road, which asks the item
//	    print(v is None)  # False  False ← the compare road, which asks the payload
//	    print(v == None)  # False  False ← the same fold, "from what the SOURCE says each side is"
//	    print(v in [v])   # True   True  ← the untagged scan: the void's payload 0 == "a"'s intern index 0
//
// — every one of them at the exit code of success, while the reference prints `True`/`False`,
// `True`/`False` and `True`/`False`. The word that decides all three questions is the same word the printer
// already reads: a text's payload is an index into `@str_tab`, a float's is the handle of a `@float_box`, the
// void's is `0` and an int's slot can carry `0` too (**ADR 0187**, **ADR 0232**). This is **ADR 0411**'s
// finding one origin over — a call's answer and a slot's answer and now an item's answer all arrive as a
// payload whose kind lives somewhere else — and **ADR 0232**'s rule restated: two roads that answer one
// question must ask it once, which is why the door is hung in the value road AND the condition road.
//
// What this row changed is three arms, all reading the ONE predicate the print door reads
// (`loopItemKindTag`, `heapargs.go`):
//
//   - `taggedOperand` answers the item with its slot's payload and the recorded constant tag. It does NOT
//     re-derive the payload: asking `floatValue` or `containerOperand` of the item's name would mint a second
//     object out of whichever number the slot holds — the **ADR 0233** rule, read from the other side.
//   - `isTaggedCompareSide` admits the item, which is the gate that opens `mixedTaggedCompare` (so `==`,
//     `!=`, `is`, `is not` reach `@rt_payload_eq` through `comparePair`, the door ADR 0410/0411 already own).
//   - the literal-haystack membership road takes `@rt_contains_tagged` when the needle is such an item,
//     because the haystack builder writes a kind beside every slot (**ADR 0187**) and the untagged scan
//     compares payloads alone.
//
// What the reading deliberately does NOT reach is pinned in the last table with the reference's bytes beside
// today's: the ordering door has its own shape machinery and never asks the item (**Gap R.448**), the `if`
// head answers `x == None` from the two payload words while the value road folds it (**Gap R.449**), and an
// item whose kind is a container, compared with a container LITERAL, is refused by the container-equality
// road (**Gap R.450**). None of those three is caused by this row — each is byte-identical on its parent —
// and each is filed rather than folded in, because a door widened for a question nobody asked is how a row
// stops being one feature (**ADR 0166**'s honest-refusal rule, which they still satisfy: none reaches exit 2).
//
// An item that is a plain integer or an interned text answers `loopItemKindTag` "not a kind a compiled word
// cannot render", so those keep the source-side fold and emit no pair call at all — the Definition of Done's
// second clause, asserted below from the module rather than from the answer.

import (
	"regexp"
	"strings"
	"testing"
)

// loopItemCompareFamily is the answering half: the identity, equality and membership questions asked of an
// item of an unrolled `for` over a literal list, plus the controls that keep the cheap roads cheap. Every
// `want` is what CPython 3.12.3 printed for the same source, read off before the case was written, and every
// source is on the record — ADR 0302's rule, so no answer here is certified by the backend that earned it.
func loopItemCompareFamily() []struct{ name, src, want string } {
	return []struct{ name, src, want string }{
		// The row's own program and its three spellings.
		{"the row's own program (identity)", "for v in [None, \"a\"]:\n    print(v is None)\n", "True\nFalse\n"},
		{"the row's own program (equality)", "for v in [None, \"a\"]:\n    print(v == None)\n", "True\nFalse\n"},
		{"the negated identity", "for v in [None, \"a\"]:\n    print(v is not None)\n", "False\nTrue\n"},
		{"the negated equality", "for v in [None, \"a\"]:\n    print(v != None)\n", "False\nTrue\n"},
		{"both spellings of one question", "for v in [None, \"a\"]:\n    print(v == None, v is None)\n", "True True\nFalse False\n"},
		// The payload collision this row exists for: the void's payload is 0 and an int's slot carries 0.
		{"the void item beside a zero", "for v in [0, None]:\n    print(v == None)\n", "False\nTrue\n"},
		{"the void item against zero", "for v in [None, 2]:\n    print(v == 0)\n", "False\nFalse\n"},
		// A float item: its payload is the handle of a @float_box, so the untagged compare compared handles
		// and `1.5 == 1.5` was False while `print(v)` printed 1.5 one line earlier.
		{"a fractional item equals itself", "for v in [1.5, 2.5]:\n    print(v == 1.5)\n", "True\nFalse\n"},
		{"a fractional item equals a number", "for v in [1.5, 2]:\n    print(v == 2)\n", "False\nTrue\n"},
		{"the item equals itself", "for v in [None, 1.5]:\n    print(v == v)\n", "True\nTrue\n"},
		// A verdict item is a NUMBER to the reference, and the numeric family is decided across tags.
		{"a verdict item equals a number", "for v in [True, 1]:\n    print(v == True)\n", "True\nTrue\n"},
		// The membership test, the third spelling of the same question.
		{"the item in its own iterable", "for k in [None, 1.5]:\n    print(k in [None, 1.5])\n", "True\nTrue\n"},
		{"the item not in its own iterable", "for k in [None, 1.5]:\n    print(k not in [None, 1.5])\n", "False\nFalse\n"},
		{"a void item in a text list", "for v in [\"a\", None]:\n    print(v in [\"a\"])\n", "True\nFalse\n"},
		{"a fractional item in a literal", "for v in [1.5, 2.5]:\n    print(v in [2.5])\n", "False\nTrue\n"},
		{"a container item in its own iterable", "for v in [[1, 2], 3]:\n    print(v in [[1, 2], 3])\n", "True\nTrue\n"},
		{"the item in an unrelated literal", "for k in [None, 1.5]:\n    print(k in [1, 2])\n", "False\nFalse\n"},
		// Every kind as the needle of a haystack that holds every kind — the needle's record and the
		// haystack's element kinds have to agree for all seven, and an int needle meets a float and a
		// verdict in its own list the way the reference's numeric equality says it must.
		{
			"every kind in the iterable it came from",
			"for v in [\"a\", None, 1, 1.5, True, [1], {\"k\":1}]:\n    print(v in [\"a\", None, 1, 1.5, True, [1], {\"k\":1}])\n",
			"True\nTrue\nTrue\nTrue\nTrue\nTrue\nTrue\n",
		},
		// A removal decided by the item's KIND rather than by its slot word. `s = {1, 2}` / `for k in [1.5]:` /
		// `s.discard(k)` is **ADR 0425**'s owed pin, which printed `1` because the pair door was handed a 1.0
		// (the number the box handle looked like) and removed a member the reference leaves alone. With the item
		// answering its own tag the tagged match declines, nothing is removed, and the length is the
		// reference's `2` — the same load-and-tag the equality door uses, asked by the removal door beside it.
		{
			"the item's kind decides a removal the slot word got wrong",
			"s = {1, 2}\nfor k in [1.5]:\n    s.discard(k)\nprint(len(s))\n",
			"2\n",
		},
		// Four kinds in one iterable, each asked all four questions: one iterable, sixteen verdicts, and the
		// row's claim is that no line of this table depends on which road happened to be reached first.
		{"every kind asked every question", "for v in [None, True, 1.5, \"s\"]:\n    print(v == None, v == True, v == 1.5, v == \"s\")\n",
			"True False False False\nFalse True False False\nFalse False True False\nFalse False False True\n"},
		// The controls: the named-list twin the pair road already answered (**ADR 0411**), and the two item
		// kinds that must keep the fold — asserted from the module in the IR table below.
		{"the named-list twin (identity)", "xs = [None, \"a\"]\nfor v in xs:\n    print(v is None)\n", "True\nFalse\n"},
		{"an integer item keeps its road", "for v in [1, 2]:\n    print(v == 1)\n", "True\nFalse\n"},
		{"an interned text item keeps its road", "for v in [\"a\", \"b\"]:\n    print(v == \"a\")\n", "True\nFalse\n"},
	}
}

// TestTheComparisonAskedOfAnUnrolledItemAsksTheItem is the answered family on both witness legs: the module
// this compiler built and ran, and the answer the record holds for that exact source. A MISSING record fails
// the case, so this coverage cannot be deleted by deleting a record.
func TestTheComparisonAskedOfAnUnrolledItemAsksTheItem(t *testing.T) {
	t.Parallel()
	for _, tc := range loopItemCompareFamily() {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, err := compileAndRun(t, tc.src)
			if err != nil {
				t.Fatalf("the compare road refused a program the reference prints %q: %v\n%s", tc.want, err, tc.src)
			}
			if got != tc.want {
				t.Fatalf("printed %q, want %q — one item, one tag, asked by every door that reads it\n%s",
					got, tc.want, tc.src)
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

// TestThePrintAndTheCompareRoadAnswerOneQuestionTogether is the ADR 0232 claim: the same item, printed and
// compared in the same body copy, may not be the void to one door and a number to the other. Each case puts
// the two questions on one line so a divergence shows up as a pair that disagrees, not as a missing line.
func TestThePrintAndTheCompareRoadAnswerOneQuestionTogether(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct{ name, src, want string }{
		{"the void prints as the void and compares as it", "for v in [None]:\n    print(v, v == None, v is None)\n", "None True True\n"},
		{"a text prints and compares as itself", "for v in [\"a\"]:\n    print(v, v == \"a\", v is None)\n", "a True False\n"},
		{"a fraction prints and compares as itself", "for v in [1.5]:\n    print(v, v == 1.5, v == 1)\n", "1.5 True False\n"},
		{"a verdict prints and compares as a number", "for v in [True]:\n    print(v, v == 1, v is None)\n", "True True False\n"},
		{"a container prints and is a member", "for v in [[1, 2]]:\n    print(v, v in [[1, 2], 3])\n", "[1, 2] True\n"},
	} {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, err := compileAndRun(t, tc.src)
			if err != nil {
				t.Fatalf("refused a program the reference prints %q: %v\n%s", tc.want, err, tc.src)
			}
			if got != tc.want {
				t.Fatalf("printed %q, want %q — the print door and the compare door gave one item two answers\n%s",
					got, tc.want, tc.src)
			}
		})
	}
}

// TestTheCompareGateAnswersForTheItemAndOnlyTheItem asks the two doors DIRECTLY, over hand-built generator
// states. The end-to-end table above cannot tell "the door answered" from "some other road happened to give
// the same bytes", and the gate's NEGATIVE half — an int or interned-text item is not a kind a compiled word
// cannot render — is what keeps the fold reachable, so it is asserted here rather than inferred from output.
func TestTheCompareGateAnswersForTheItemAndOnlyTheItem(t *testing.T) {
	t.Parallel()
	item := func(tag int32) *irGen {
		return &irGen{
			allocd:      map[string]bool{"k": true},
			loopElemTag: map[string]int32{"k": tag},
			loopElem:    map[string]Expr{"k": &IntLit{Value: 1}},
		}
	}
	k := func() Expr { return &Name{Value: "k"} }

	// The kinds the unroll's record can name and a compiled word cannot render: every one of them is a
	// compare side now, and every one of them travels as (payload, tag).
	for _, tag := range []struct {
		n      string
		tag    int32
		tagStr string
	}{
		{"the void", int32(TagNone), "3"},
		{"a fraction", int32(TagFloat), "1"},
		{"a verdict", int32(TagBool), "2"},
		{"a list", int32(TagList), "5"},
		{"a dict", int32(TagDict), "6"},
		{"a set", int32(TagSet), "7"},
		{"an instance", int32(TagInstance), "10"},
	} {
		tag := tag
		t.Run("the item is "+tag.n, func(t *testing.T) {
			t.Parallel()
			g := item(tag.tag)
			if !g.isTaggedCompareSide(k()) {
				t.Fatalf("the compare gate declined an item whose record says tag %d — the door that prints it answers", tag.tag)
			}
			if _, ok := g.loopItemKindTag(k()); !ok {
				t.Fatalf("loopItemKindTag declined the same item — the gate and the record must read ONE predicate")
			}
			var b strings.Builder
			v, tg, ok := g.taggedOperand(&b, k())
			if !ok || v == "" || tg == "" {
				t.Fatalf("taggedOperand declined the item: (%q,%q,%v)\n%s", v, tg, ok, b.String())
			}
			if tg != tag.tagStr {
				t.Fatalf("the item's tag came back %q, the record says %q", tg, tag.tagStr)
			}
			// The pair is LOADED, never rebuilt: the payload the slot holds is already the box handle, the
			// interned index or the object the element built (ADR 0233), and a second object out of that
			// number is the wrong answer this row's sibling rows keep filing.
			if !strings.Contains(b.String(), "load i32, i32* %_k") {
				t.Fatalf("the item's payload was not loaded from its own slot:\n%s", b.String())
			}
			if strings.Contains(b.String(), "rt_float_new") {
				t.Fatalf("the item's payload was RE-BOXED rather than loaded:\n%s", b.String())
			}
		})
	}

	// The negative half, which is the Definition of Done's second clause stated at the door.
	for _, tc := range []struct {
		name string
		tag  int32
	}{
		{"an integer item keeps the fold", int32(TagInt)},
		{"an interned text item keeps the fold", int32(TagStr)},
	} {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			g := item(tc.tag)
			if g.isTaggedCompareSide(k()) {
				t.Fatalf("the compare gate admitted an item whose kind a compiled word already renders (tag %d) — the fold it must keep is pinned in the IR table", tc.tag)
			}
			if _, ok := g.loopItemKindTag(k()); ok {
				t.Fatalf("loopItemKindTag answered tag %d as a self-describing kind", tc.tag)
			}
		})
	}

	// The record speaks for a NAME the loop bound and for nothing else: an unbound name, a spelled literal,
	// and a name the loop bound to a plain number all hand the question back.
	t.Run("a name no loop bound", func(t *testing.T) {
		t.Parallel()
		g := &irGen{}
		if g.isTaggedCompareSide(k()) {
			t.Fatalf("a name no loop bound was admitted as a tagged compare side")
		}
		var b strings.Builder
		if _, _, ok := g.taggedOperand(&b, k()); ok {
			t.Fatalf("taggedOperand invented a pair for a name with no record:\n%s", b.String())
		}
	})
	t.Run("a spelled literal", func(t *testing.T) {
		t.Parallel()
		g := item(int32(TagNone))
		if g.isTaggedCompareSide(&NoneLit{}) {
			t.Fatalf("a written None was admitted through the item's arm — the road writes names, not literals")
		}
		if _, ok := g.loopItemKindTag(&NoneLit{}); ok {
			t.Fatalf("loopItemKindTag answered for a literal")
		}
	})
	t.Run("a different name", func(t *testing.T) {
		t.Parallel()
		g := item(int32(TagNone))
		if g.isTaggedCompareSide(&Name{Value: "other"}) {
			t.Fatalf("a name the road never bound asked this item's record")
		}
	})
}

// TestWhatThePairDoorEmitsAndWhatItNeverEmits is the IR half, in both directions: the shapes this row opened
// must carry the pair call, and the shapes it left alone must carry none — a table that only asserts presence
// cannot tell a widening from a rewrite that moved every program onto the new road.
func TestWhatThePairDoorEmitsAndWhatItNeverEmits(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name    string
		src     string
		must    []string
		mustNot []string
	}{
		{
			name: "the void item's equality asks the pair",
			src:  "for v in [None]:\n    print(v == None)\n",
			must: []string{
				"load i32, i32* %_v",
				// payload, tag 3 (the void), the written void's payload 0, tag 3 — the same call ADR 0411
				// pinned for the popped slot, reached from the item's side this time.
				"call i32 @rt_payload_eq(i32 %",
				"i32 3, i32 0, i32 3",
			},
		},
		{
			name: "the fractional item's equality asks the pair",
			src:  "for v in [1.5]:\n    print(v == 1.5)\n",
			must: []string{"load i32, i32* %_v", "call i32 @rt_payload_eq(i32 %"},
		},
		{
			name: "the item's membership asks the tagged scan",
			src:  "for k in [None]:\n    print(k in [None, 1.5])\n",
			must: []string{"call i32 @rt_contains_tagged(i32 %"},
		},
		{
			// The controls: the fold and the untagged scan keep their roads, and neither gains a call.
			name:    "an integer item's equality emits no pair call",
			src:     "for v in [1]:\n    print(v == 1)\n",
			mustNot: []string{"rt_payload_eq", "rt_contains_tagged"},
		},
		{
			name:    "a text item's equality emits no pair call",
			src:     "for v in [\"a\"]:\n    print(v == \"a\")\n",
			mustNot: []string{"rt_payload_eq"},
		},
		{
			name:    "an integer item's membership keeps the constant scan",
			src:     "for k in [1]:\n    print(k in [1, 2])\n",
			mustNot: []string{"rt_contains_tagged", "rt_payload_eq"},
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
			if main == "" {
				t.Fatalf("no @main in the module\n%s", tc.src)
			}
			for _, want := range tc.must {
				if !strings.Contains(main, want) {
					t.Errorf("@main is missing %q:\n%s", want, main)
				}
			}
			for _, ban := range tc.mustNot {
				if strings.Contains(main, ban) {
					t.Errorf("@main carries %q, which this row promised NOT to emit for this shape:\n%s", ban, main)
				}
			}
		})
	}
}

// TestTheItemPairTravelsToTheCompareInBothOrders pins the operand-order half: the pair door is asked of each
// side independently, so `None == v` must be as sound as `v == None` — a door hung on one side of the operator
// only is how a family like this one acquires its next row.
func TestTheItemPairTravelsToTheCompareInBothOrders(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct{ name, src, want string }{
		{"item on the left", "for v in [None, \"a\"]:\n    print(v == None)\n", "True\nFalse\n"},
		{"item on the right", "for v in [None, \"a\"]:\n    print(None == v)\n", "True\nFalse\n"},
		{"item against a fraction, left", "for v in [1.5, 2]:\n    print(v == 1.5)\n", "True\nFalse\n"},
		{"item against a fraction, right", "for v in [1.5, 2]:\n    print(1.5 == v)\n", "True\nFalse\n"},
		{"item in a condition", "for v in [None, 1.5]:\n    if v is None:\n        print(\"void\")\n", "void\n"},
		{"item in a short-circuit", "for v in [None, 1.5]:\n    print(v is None and \"t\")\n", "t\nFalse\n"},
		{"item in a while head", "for v in [None]:\n    while v is None:\n        print(\"void\")\n        break\n", "void\n"},
		{"item bound from the verdict", "for v in [None, 1.5]:\n    r = v == None\n    print(r)\n", "True\nFalse\n"},
	} {
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
		})
	}
}

// loadOfSlot matches the register that carries an item's payload out of its own slot.
var loadOfSlot = regexp.MustCompile(`%(\w+) = load i32, i32\* %_v`)

// TestTheComparedItemIsTheSameWordThePrinterPrints asks the row's own claim of the module: the register the
// pair call is handed must be the one loaded from the loop variable's slot, not a register some other road
// built. That is the difference between reading the unroll's record and re-deriving the value from it.
func TestTheComparedItemIsTheSameWordThePrinterPrints(t *testing.T) {
	t.Parallel()
	res, err := JIT("for v in [None]:\n    print(v == None)\n", 0)
	if err != nil {
		t.Fatalf("the module would not build: %v", err)
	}
	main := runtimeFnBody(res.IR, "define i32 @main()")
	calls := regexp.MustCompile(`call i32 @rt_payload_eq\(i32 %(\w+),`).FindStringSubmatch(main)
	if calls == nil {
		t.Fatalf("@main carries no rt_payload_eq call:\n%s", main)
	}
	loaded := loadOfSlot.FindStringSubmatch(main)
	if loaded == nil || loaded[1] != calls[1] {
		t.Fatalf("the pair call reads %%%s, but the item's payload is loaded into %v — the compare road re-derived the value instead of loading it\n%s",
			calls[1], loaded, main)
	}
}

// TestWhatTheComparedItemStillOwesIsFiledNotFixed holds what this row leaves owed, with today's bytes beside
// the reference's. Each is byte-identical on this row's parent — measured with a build of `HEAD` taken in a
// `git worktree` before the three arms landed — which is the evidence that the arm written here neither caused
// nor fixed them. An answer that arrives here FAILS the case: the row is paid, and this table has to follow.
func TestWhatTheComparedItemStillOwesIsFiledNotFixed(t *testing.T) {
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
			// The ordering door has its own shape machinery (`orderShapeOf`) and none of it asks the
			// unroll's record: the float item is compared by its box handle, and the void item by its 0.
			name:     "the fractional item ordered against a number",
			src:      "for v in [1.5, 2.5]:\n    print(v < 2)\n",
			wantExit: 0, wantOut: "True\nTrue\n",
			dossier: "**Gap R.448** — the reference prints `True`, `False`; `taggedOrderApplies` never asks " +
				"`loopItemKindTag`, so the item's payload (a @float_box handle) reaches the icmp",
		},
		{
			name:     "the void item ordered against a number",
			src:      "for v in [None, 2.5]:\n    print(v < 3)\n",
			wantExit: 0, wantOut: "True\nTrue\n",
			dossier: "**Gap R.448** — the reference RAISES `'<' not supported between instances of 'NoneType' " +
				"and 'int'` on the first item; a wrong verdict at exit 0 where the reference spends a trap",
		},
		{
			// The value road folds `x == None` from what the SOURCE says each side is; the condition road
			// never asks that fold and compares the payload words instead. Same program, two roads — the
			// shape exists at module level too, where no loop item is involved at all.
			name:     "the item's equality asked in an if head",
			src:      "for v in [None, \"a\"]:\n    if v == None:\n        print(\"void\")\n    else:\n        print(\"other\")\n",
			wantExit: 0, wantOut: "void\nvoid\n",
			dossier: "**Gap R.449** — the reference prints `void`, `other` (and `print(v == None)` above this " +
				"table prints `True`, `False`): `truthOperandErr` asks the pair door but not the None-equality " +
				"fold the value road asks, so the text item's intern index 0 is read as the void's payload",
		},
		{
			name:     "the same fold asked outside any loop",
			src:      "s = \"a\"\nif s == None:\n    print(\"void\")\nelse:\n    print(\"other\")\n",
			wantExit: 0, wantOut: "void\n",
			dossier: "**Gap R.449** — the reference prints `other`, and `print(s == None)` prints `False`: this " +
				"half is not about loop items at all, which is what makes it a row rather than fallout",
		},
		{
			// The container-equality road asks `isProvenScalarExpr` of the side that is not a container, and
			// the unroll's record is not one of the places it asks.
			name:       "the container item compared with a container literal",
			src:        "for v in [[1, 2], 3]:\n    print(v == [1, 2])\n",
			wantExit:   1,
			wantStderr: "comparing a container with",
			dossier: "**Gap R.450** — the reference prints `True`, `False`; `containerEquality` refuses because " +
				"`isProvenScalarExpr` does not read the item's record (a REFUSAL, so exit 1 and no exit 2)",
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
