// Roadmap **Gap R.441** / **ADR 0422** — the element a slot is WRITTEN with answers what it is, and the
// container is told.
//
//	xs = [1]
//	xs[0] = 2.5
//	print(xs)             #  [2.5]  ← this backend: [1], at the exit code of success
//	xs[0] = None
//	print(xs)             #  [None] ← and [0]
//	xs.append(2.5)
//	print(xs)             #  [1, 2.5] ← the append road answered all along
//
// Three roads write a container slot: `append` (which has asked the ELEMENT its kind since **ADR 0233**),
// the dict's entry store (which has since **ADR 0232**), and `xs[i] = v` — the item store — which asked
// only two questions, both about the container: is the new word text, is it a number. The float got
// written correctly, in full: `%t3 = call i32 @rt_float_new(double 2.5)` boxed it, `rt_put_elem` stored
// the handle and `rt_tag_elem(…, i32 1)` labelled the slot float. What was never written is the
// container's own sentence about its slots — `rt_mark_estr(…, i32 8)`, the bit that says "ask each slot" —
// because the road reached for it only when text and number contradicted each other. With that bit clear
// the printer chosen for the list is `rt_print_list`, which reads every slot as the container's recorded
// kind, and so printed the float box's handle as the number it is: `[1]`. The same program through
// `append` printed `2.5`.
//
// The fix is the arm the other two stores already carry: ask `elemKindTag` of the VALUE, and when the
// answer is a kind whose payload only means something through its tag — a float box's handle, the void's
// nothing, a verdict's bare 0/1, another container's handle — promote the container to describing its
// slots (`slotTagSelfDescribing` → `promoteMixed`). One predicate, three writers of one object
// (**ADR 0249**'s rule that a gate and its door ask the same question, applied here to three doors).
//
// The row also PAIRS the promotion with the census it must not disturb: `append`'s road, the dict entry
// store, the str/int contradiction that already promoted, and — the one that bites — the numeric fold
// **ADR 0239** leaves on a single-kind list. `sum`/`min`/`max` read the static array; a store that
// promoted a list the fold was reading moves it to the tagged road, and where a position cannot carry the
// tag the program is REFUSED rather than answered from a stale payload. That is filed as **Gap R.442**
// with the measurement that shows the old "correct" chained comparison was the stale payload answering by
// luck (`xs[0] = 5.5` / `print(0 < xs[0] < 3)` said `True`, where the reference says `False`).
package lang

import (
	"strings"
	"testing"
)

// itemStoreFamily is the answering half: a slot written with a value whose kind the container does not
// claim. Every `want` was read off CPython 3.12 before the case was written, and every source is on the
// record — ADR 0302's rule, so an answer here is not certified by the backend that earned it.
func itemStoreFamily() []struct{ name, src, want string } {
	return []struct{ name, src, want string }{
		// The program the row is named for, and the kinds beside it.
		{"a fraction stored in an int slot", "xs = [1]\nxs[0] = 2.5\nprint(xs)\n", "[2.5]\n"},
		{"a fraction stored beside an untouched number", "xs = [1, 2]\nxs[0] = 2.5\nprint(xs)\n", "[2.5, 2]\n"},
		{"the void stored in an int slot", "xs = [1]\nxs[0] = None\nprint(xs)\n", "[None]\n"},
		{"a verdict stored in an int slot", "xs = [1]\nxs[0] = True\nprint(xs)\n", "[True]\n"},
		{"the false verdict stored in the second slot", "xs = [1, 2]\nxs[1] = False\nprint(xs)\n", "[1, False]\n"},
		{"a list stored in an int slot", "xs = [1]\nxs[0] = [2]\nprint(xs)\n", "[[2]]\n"},
		{"a dict stored in an int slot", "xs = [1]\nxs[0] = {\"a\": 1}\nprint(xs)\n", "[{'a': 1}]\n"},
		{"a set stored in an int slot", "xs = [1]\nxs[0] = {1, 2}\nprint(xs)\n", "[{1, 2}]\n"},
		{
			// The immutable spelling is a container node carrying the tuple bit (ADR 0251), so it rides
			// the container arm of the tag question and the object's own bit answers the printer.
			"the tuple spelling stored in an int slot", "xs = [1]\nxs[0] = (1, 2)\nprint(xs)\n", "[(1, 2)]\n",
		},
		{"the void stored through a negative index", "xs = [1, 2]\nxs[-1] = None\nprint(xs)\n", "[1, None]\n"},
		// The value arriving through something other than its own spelling.
		{"a fraction bound to a name, stored", "v = 2.5\nxs = [1]\nxs[0] = v\nprint(xs)\n", "[2.5]\n"},
		{"a fraction a body hands back, stored", "def f():\n    return 2.5\nxs = [1]\nxs[0] = f()\nprint(xs)\n", "[2.5]\n"},
		{
			// The container under a NAME rather than spelled at the store: the payload is a heap handle and
			// the name alone says nothing, which is the census **Gap R.439** still owes for a literal.
			"a container under a name, stored", "ys = [2]\nxs = [1]\nxs[0] = ys\nprint(xs)\n", "[[2]]\n",
		},
		{
			"a dict under a name, stored", "d = {\"a\": 1}\nxs = [1]\nxs[0] = d\nprint(xs)\n", "[{'a': 1}]\n",
		},
		// The roads that read the container back after the store.
		{"the stored fraction read out of the slot", "xs = [1]\nxs[0] = 2.5\nprint(xs[0])\n", "2.5\n"},
		{"the stored verdict read out of the slot", "xs = [1]\nxs[0] = True\nprint(xs[0])\n", "True\n"},
		{"the stored void asked its identity", "xs = [1]\nxs[0] = None\nprint(xs[0] is None)\n", "True\n"},
		{"the stored void compared", "xs = [1]\nxs[0] = None\nprint(xs[0] == None)\n", "True\n"},
		{"the promoted list looped over", "xs = [1, 2]\nxs[0] = 2.5\nfor k in xs:\n    print(k)\n", "2.5\n2\n"},
		{"the promoted list sliced", "xs = [1, 2, 3]\nxs[0] = 2.5\nprint(xs[0:2])\n", "[2.5, 2]\n"},
		{"the promoted list popped", "xs = [1, 2]\nxs[0] = 2.5\nprint(xs.pop(0))\nprint(xs)\n", "2.5\n[2]\n"},
		{"the promoted list reversed", "xs = [1, 2]\nxs[0] = 2.5\nxs.reverse()\nprint(xs)\n", "[2, 2.5]\n"},
		{"the promoted list through str", "xs = [1]\nxs[0] = 2.5\nprint(str(xs))\n", "[2.5]\n"},
		{"the promoted list through repr", "xs = [1]\nxs[0] = 2.5\nprint(repr(xs))\n", "[2.5]\n"},
		{"the promoted list compared as a list", "xs = [1]\nxs[0] = 2.5\nprint(xs == [2.5])\n", "True\n"},
		{"the promoted list concatenated", "xs = [1]\nxs[0] = 2.5\nprint(xs + [3])\n", "[2.5, 3]\n"},
		{"the promoted list searched", "xs = [1, 2]\nxs[0] = 2.5\nprint(2.5 in xs)\n", "True\n"},
		{"the promoted list measured", "xs = [1, 2]\nxs[0] = 2.5\nprint(len(xs))\n", "2\n"},
		{"the stored tuple compared", "xs = [1]\nxs[0] = (1, 2)\nprint(xs[0] == (1, 2))\n", "True\n"},
		// More than one store, and stores whose index only exists at run time.
		{"two stores of two different kinds", "xs = [1, 2, 3]\nxs[0] = None\nxs[2] = 2.5\nprint(xs)\n", "[None, 2, 2.5]\n"},
		{"a store over a list a comprehension built", "xs = [k for k in [1, 2]]\nxs[0] = 2.5\nprint(xs)\n", "[2.5, 2]\n"},
		{"a store whose index is a loop item", "xs = [1, 2]\nfor i in [0]:\n    xs[i] = None\nprint(xs)\n", "[None, 2]\n"},
		{
			"a store whose index is a while counter",
			"xs = [1, 2]\ni = 0\nwhile i < 2:\n    xs[i] = None\n    i += 1\nprint(xs)\n", "[None, None]\n",
		},
		// The same object under a second name (ADR 0412's sharer walk, asked of this store too).
		{"the store through an alias, read from the original", "xs = [1, 2]\nys = xs\nys[0] = 2.5\nprint(xs)\n", "[2.5, 2]\n"},
		{"the void through an alias, read from the original", "xs = [1]\nys = xs\nys[0] = None\nprint(xs)\n", "[None]\n"},
	}
}

// TestAStoredSlotSaysWhatItsValueIs is the answering half on the compiled leg, each case judged again on
// the record leg so the promotion is not certified by the road that earned it.
func TestAStoredSlotSaysWhatItsValueIs(t *testing.T) {
	t.Parallel()
	for _, tc := range itemStoreFamily() {
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

// TestTheStoreThatNeededNoPromotionIsUntouched is the control table. The arm gained a question and changed
// no other answer: the same-kind store, the text store the str/int census already promoted, the append
// road ADR 0233 paid, the dict entry store ADR 0232 paid, and the numeric fold ADR 0239 must keep.
func TestTheStoreThatNeededNoPromotionIsUntouched(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct{ name, src, want string }{
		{"a number stored in a number slot", "xs = [1]\nxs[0] = 3\nprint(xs)\n", "[3]\n"},
		{"a text stored in an int slot, which the str/int census already promoted", "xs = [1]\nxs[0] = \"s\"\nprint(xs)\n", "['s']\n"},
		{"a fraction stored in a text slot", "xs = [\"a\"]\nxs[0] = 2.5\nprint(xs)\n", "[2.5]\n"},
		{"the append road, unchanged", "xs = [1]\nxs.append(2.5)\nprint(xs)\n", "[1, 2.5]\n"},
		{"the dict entry store, unchanged", "d = {\"a\": 1}\nd[\"a\"] = 2.5\nprint(d)\n", "{'a': 2.5}\n"},
		{"a store into a float-kind list", "xs = [1.5]\nxs[0] = None\nprint(xs)\n", "[None]\n"},
		{"a number stored over the promoted fraction", "xs = [1]\nxs[0] = 2.5\nxs[0] = 3\nprint(xs)\n", "[3]\n"},
		{"the fold over a slot the store promoted", "xs = [1]\nxs[0] = 2.5\nprint(sum([xs[0], 1.0]))\n", "3.5\n"},
		{"the arithmetic over the promoted slot", "xs = [1]\nxs[0] = 2.5\nprint(xs[0] + 1)\n", "3.5\n"},
		{"the truth of a stored void", "xs = [1]\nxs[0] = None\nprint(bool(xs[0]))\n", "False\n"},
		{"the truth of a stored fraction", "xs = [1]\nxs[0] = 2.5\nprint(bool(xs[0]))\n", "True\n"},
		{"a verdict read as the number the reference says it is", "xs = [1]\nxs[0] = True\nprint(xs[0] + 1)\n", "2\n"},
		{"a text stored, then a void", "xs = [1, 2]\nxs[0] = \"a\"\nxs[1] = None\nprint(xs)\n", "['a', None]\n"},
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

// TestTheOutOfRangeStoreStillRaisesTheReferencesError keeps the bounds door this row shares with the read
// path (ADR 0210): promoting the container is no licence to write past the end.
func TestTheOutOfRangeStoreStillRaisesTheReferencesError(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct{ name, src string }{
		{"a fraction past the end", "xs = [1]\nxs[5] = 2.5\nprint(xs)\n"},
		{"the void past the end", "xs = [1]\nxs[5] = None\nprint(xs)\n"},
		{"a container before the start", "xs = [1]\nxs[-5] = [2]\nprint(xs)\n"},
	} {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			res, err := JIT(tc.src, 0)
			if err != nil {
				t.Fatalf("the compiled leg refused a program the reference runs: %v\n%s", err, tc.src)
			}
			if res.Code != 3 {
				t.Fatalf("exited %d with stdout %q — the out-of-range write must raise, not store\n%s", res.Code, res.Output, tc.src)
			}
			if !strings.Contains(res.Stderr+res.Output, "IndexError: list assignment index out of range") {
				t.Fatalf("raised %q, not the reference's IndexError sentence\n%s", res.Stderr+res.Output, tc.src)
			}
		})
	}
}

// storeProbeElems are the values one slot may be written with, hand-built so the gate/door property can be
// asked of each without compiling a program for it.
func storeProbeElems() []struct {
	name string
	el   Expr
	// selfDescribing is the claim under test: whether the value's payload only means something through
	// the tag beside it, and therefore obliges the container to stop claiming one kind.
	selfDescribing bool
	why            string
} {
	f := func(v float64) Expr { return &FloatLit{Value: v} }
	i := func(v int64) Expr { return &IntLit{Value: v} }
	b := func(v bool) Expr { return &BoolLit{Value: v} }
	n := func() Expr { return &NoneLit{} }
	s := func(v string) Expr { return &StrLit{Value: v} }
	c := func(fn string, args ...Expr) Expr { return &Call{Fn: &Name{Value: fn}, Args: args} }
	return []struct {
		name           string
		el             Expr
		selfDescribing bool
		why            string
	}{
		{"a fraction", f(2.5), true, "the payload is a @float_box handle (**ADR 0238**)"},
		{"the void spelled", n(), true, "the payload is nothing at all"},
		{"a verdict", b(true), true, "the payload is a bare 0/1 (**ADR 0259**)"},
		{"a container literal", &ListLit{Elems: []Expr{i(2)}}, true, "the payload is a heap handle (**ADR 0189**)"},
		{"a dict literal", &DictLit{Keys: []Expr{s("a")}, Vals: []Expr{i(1)}}, true, "a heap handle, one table over"},
		{"a set literal", &SetLit{Elems: []Expr{i(1)}}, true, "a heap handle, another table over"},
		{
			"the tuple spelling", &ListLit{Elems: []Expr{i(1), i(2)}, Tuple: true}, true,
			"the immutable spelling is ONE container node with the tuple bit (**ADR 0251**), so it rides the container arm",
		},
		{"a number", i(1), false, "the payload IS the value — the static road's whole point (**ADR 0239**)"},
		{"a text", s("a"), false, "the interned index, which the str/int census asks separately (**Gap I.2**)"},
		{"a name the void record claims", &Name{Value: "k"}, true, "**ADR 0172**'s latest binding, read by `elemKindTag` since **ADR 0421**"},
		{"a name the verdict record claims", &Name{Value: "b"}, true, "the same record's other singleton"},
		{"a name the container records claim", &Name{Value: "xs"}, true, "a heap handle whose kind the object carries"},
		{"a loop item", &Name{Value: "v"}, false, "a name the unroll gave a per-item tag is THIS item (**Gap R.358**)"},
		{"a call", c("f"), false, "the answer's kind is not this pass's to claim (the blanket TagInt below it)"},
	}
}

// TestTheStoreGateAndThePromotionDoorAskOnePredicate is **ADR 0249**'s rule on this row's pair, in the
// direction that matters: whatever the arm decides to promote, the door must be able to build. The arm is
// the NARROWER of the two — `promoteMixed` will mark a container for any value `elemKindTag` can name, and
// the row only asks it to when the payload cannot speak its own kind — so the invariant is the
// implication, and it fails loudly if either side is widened alone, which is how the first cut of **Gap
// R.435** reached a refusal where it should have reached an answer.
func TestTheStoreGateAndThePromotionDoorAskOnePredicate(t *testing.T) {
	for _, tc := range storeProbeElems() {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			g := newStoreProbeGen()
			tag, ok := g.elemKindTag(tc.el)
			wantsSlots := ok && slotTagSelfDescribing(tag)
			var b strings.Builder
			promoted := g.promoteMixed(&b, "%h", "probe", "list", tc.el)
			if wantsSlots && !promoted {
				t.Fatalf("the arm sent %s to the tagged road (tag %d) and the door declined to build it — the program "+
					"is refused for want of an arm in one of the two (%s)", tc.name, tag, tc.why)
			}
			if wantsSlots != tc.selfDescribing {
				t.Fatalf("the arm answered %v for %s (tag %d), this table expects %v — %s", wantsSlots, tc.name, tag, tc.selfDescribing, tc.why)
			}
			if promoted && !strings.Contains(b.String(), "call void @rt_mark_estr(i32 %h, i32 8)") {
				t.Fatalf("the door marked the container mixed but never wrote the bit that tells the printer to ask each "+
					"slot its own kind:\n%s", b.String())
			}
		})
	}
}

// newStoreProbeGen is an irGen with just the maps the two doors above read and write. The write is
// `markListMixed`, which walks the alias table and sets one entry per sharer — a nil map there is a panic,
// not an answer (**ADR 0166**).
func newStoreProbeGen() *irGen {
	return &irGen{
		mixedLists:     map[string]bool{},
		listElemStr:    map[string]bool{},
		listElemInt:    map[string]bool{},
		mixedSets:      map[string]bool{},
		mixedDicts:     map[string]bool{},
		containerAlias: map[string]string{},
		noneVars:       map[string]bool{"k": true},
		boolVars:       map[string]bool{"b": true},
		strVals:        map[string]string{},
		listVars:       map[string]bool{"xs": true},
		runtimeDicts:   map[string]bool{"d": true},
		runtimeSets:    map[string]bool{"t": true},
		loopElemTag:    map[string]int32{"v": int32(TagInt)},
	}
}

// TestTheItemStoreIRMarksTheContainerItsSlotsOwn pins the emitted IR, because every answer above could in
// principle be reached by folding the store away. The row's claim is precise: the payload and the tag were
// already written correctly; the missing word was the container's bit 8, and the printer that follows from
// it.
func TestTheItemStoreIRMarksTheContainerItsSlotsOwn(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name      string
		src       string
		want      []string
		deny      []string
		denialWhy string
	}{
		{
			// TagFloat is 1 (value.go's iota): the box handle went in under the right label all along, and
			// what the row adds is the container's own `rt_mark_estr …, i32 8` beside it.
			name: "a stored fraction marks the container",
			src:  "xs = [1]\nxs[0] = 2.5\nprint(xs)\n",
			want: []string{
				"%t3 = call i32 @rt_float_new(double %t2)",
				"call void @rt_mark_estr(i32 %_xs.ld1, i32 8)",
				"call void @rt_put_elem(i32 %_xs.ld1, i32 %t8, i32 %t3)",
				"call void @rt_tag_elem(i32 %_xs.ld1, i32 %t8, i32 1)",
				"call void @rt_print_list_mixed(i32 %h2, i32 0)",
			},
			deny: []string{"call void @rt_print_list(i32 %h2, i32 0)"},
			denialWhy: "the single-kind printer is the road that read the float box's handle as the container's " +
				"recorded kind and printed `[1]` at the exit code of success (**Gap R.441**)",
		},
		{
			// TagNone is 3: the payload is nothing, so the tag is the whole answer and the container has
			// to be told to go and look.
			name: "a stored void marks the container",
			src:  "xs = [1]\nxs[0] = None\nprint(xs)\n",
			want: []string{
				"call void @rt_mark_estr(i32 %_xs.ld1, i32 8)",
				"call void @rt_tag_elem(i32 %_xs.ld1, i32 %t5, i32 3)",
				"call void @rt_print_list_mixed(i32 %h2, i32 0)",
			},
			deny: []string{"call void @rt_print_list(i32 %h2, i32 0)"},
			denialWhy: "`[0]` is the void's payload printed as the number it is, which is what the single-kind " +
				"printer answers when the container never claimed its slots",
		},
		{
			// TagBool is 2: `True` and `1` are one payload, and the tag is the difference.
			name: "a stored verdict marks the container",
			src:  "xs = [1]\nxs[0] = True\nprint(xs)\n",
			want: []string{
				"call void @rt_mark_estr(i32 %_xs.ld1, i32 8)",
				"call void @rt_put_elem(i32 %_xs.ld1, i32 %t5, i32 1)",
				"call void @rt_tag_elem(i32 %_xs.ld1, i32 %t5, i32 2)",
				"call void @rt_print_list_mixed(i32 %h2, i32 0)",
			},
			deny: []string{"call void @rt_print_list(i32 %h2, i32 0)"},
			denialWhy: "the tag is what lets the printer answer `True` rather than `1` (**ADR 0259**'s rule, " +
				"taken to the third writer of a slot)",
		},
		{
			// The control: the same store road, a value the container already claims. No bit 8, the plain
			// printer, and the static array ADR 0239's fold reads stays the whole story.
			name: "a stored number leaves the container alone",
			src:  "xs = [1]\nxs[0] = 3\nprint(xs)\n",
			want: []string{
				"call void @rt_put_elem(i32 %_xs.ld1, i32 %t5, i32 3)",
				"call void @rt_tag_elem(i32 %_xs.ld1, i32 %t5, i32 0)",
				"call void @rt_print_list(i32 %h2, i32 0)",
			},
			deny: []string{"call void @rt_mark_estr(i32 %_xs.ld1, i32 8)", "call void @rt_print_list_mixed(i32 %h2, i32 0)"},
			denialWhy: "a list the records still describe as numbers is the fold's list — `sum`, `min` and `max` " +
				"read that record (**ADR 0239**), and a store that promoted it on its way would tax every numeric program",
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

// TestWhatTheItemStoreRoadStillCannotReachIsFiledNotFixed pins what this row leaves owed, with today's bytes
// beside the reference's. Two of these are NOT unchanged from the row's parent — they are the refusals the
// promotion EXPOSES, each measured against the same program with a value that shows the old answer was the
// stale payload answering by luck, and each named by the row that owns the fix.
func TestWhatTheItemStoreRoadStillCannotReachIsFiledNotFixed(t *testing.T) {
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
			// The chained comparison cannot settle a static kind once the slot carries a tag, so it
			// refuses. It is NOT a green promise this row broke: on this row's parent the same program
			// with `5.5` printed True where the reference prints False, because the comparison read the int
			// payload the store never wrote. Today it says it cannot answer, which is the honest shape of
			// the same hole (**Gap R.82**'s one-word position, **Gap R.442**'s census).
			name:     "a chained comparison over a promoted slot",
			src:      "xs = [1, 2]\nxs[0] = 2.5\nprint(0 < xs[0] < 3)\n",
			wantExit: 1, wantStderr: "needs a single static kind",
			dossier: "**Gap R.442** — the reference prints `True`; the chain lowers each operand through an " +
				"ordinary read and cannot carry the tag (**Gap R.82**), and on the parent it answered `True` " +
				"from the stale payload even for `xs[0] = 5.5`, where the reference says `False`",
		},
		{
			// The comparator over a promoted list refuses where it used to PRODUCE an order — a wrong one
			// (`[2.5, 1]` against the reference's `[1, 2.5]`), which is the worse witness of the two.
			name:     "a sort over a promoted list",
			src:      "xs = [3, 1]\nxs[0] = 2.5\nprint(sorted(xs))\n",
			wantExit: 1, wantStderr: "cannot sort a list whose elements are of more than one kind",
			dossier: "**Gap R.438** / **ADR 0191** — the reference prints `[1, 2.5]`; the sort compares payloads " +
				"and has no ordering for a slot whose kind is a tag, so it refuses rather than order them",
		},
		{
			// The same hole one position further: a numeric use of a slot in a list that a store with a
			// RUN-TIME index promoted. ADR 0249's numeric courtesy needs the literal the container was built
			// from, and this object's slots belong to the loop, so the position refuses. On the parent it
			// answered `2` and `[0]` — the stale int payload twice, where the reference says `3.5` and
			// `[None]` — so this refusal is the hole being shown, not a promise lost.
			name:     "a numeric use of a slot a run-time store promoted",
			src:      "xs = [1]\nxs[0] = 2.5\nprint(xs[0] + 1)\nfor i in [0]:\n    xs[i] = None\nprint(xs)\n",
			wantExit: 1, wantStderr: "needs a single static kind",
			dossier: "**Gap R.442** — the reference prints `3.5`, `[None]`; the numeric door (**ADR 0249**) " +
				"reads the literal the container was built from and this object's slots are the loop's " +
				"(**Gap R.82**'s one-word position again)",
		},
		{
			// The reduction that never reached the object at all: `sum`/`min`/`max` fold a literal and this
			// container's slots belong to the runtime.
			name:     "a reduction over a container the compiler does not hold",
			src:      "xs = [1]\nxs[0] = 2.5\nprint(sum(xs))\n",
			wantExit: 1, wantStderr: "sum requires an inline list/set/dict literal",
			dossier: "**Gap R.71** / **Gap R.107** (`lower.unsupported.builtin-needs-inline-container`) — the " +
				"reference prints `2.5`; the fold reads a literal it can see and never asks the object",
		},
		{
			name:     "the method that would have to search the runtime container",
			src:      "xs = [1, 2]\nxs[0] = 2.5\nprint(xs.count(2.5))\n",
			wantExit: 1, wantStderr: "count() on \"xs\"",
			dossier: "**Gap R.174** / **Gap I.2** — the reference prints `1`; the container methods fold over a " +
				"literal written at the call, and this list's slots are the runtime's",
		},
		{
			name:     "the method that would have to insert into the runtime container",
			src:      "xs = [1, 2]\nxs[0] = 2.5\nxs.insert(0, 9)\nprint(xs)\n",
			wantExit: 1, wantStderr: "insert() on \"xs\"",
			dossier: "**Gap R.174** — the reference prints `[9, 2.5, 2]`, one row over its sibling `append`, which answers",
		},
		{
			// The store through an expression rather than a name — a different door from this row's, and
			// unchanged on the parent.
			name:     "the store through a subscript rather than a name",
			src:      "xs = [[1]]\nxs[0][0] = 2.5\nprint(xs)\n",
			wantExit: 1, wantStderr: "item assignment needs a container variable on the left",
			dossier: "**Gap R.381** — the reference prints `[[2.5]]`; the read side of the same position closed " +
				"as **Gap R.214** / **ADR 0334** and the write side waits",
		},
		{
			name:     "the store from inside a body over a module container",
			src:      "xs = [1]\ndef f():\n    xs[0] = 2.5\nf()\nprint(xs)\n",
			wantExit: 1, wantStderr: "bound at module level",
			dossier: "**Gap R.35** — the reference prints `[2.5]`; a body cannot read module state that changes, " +
				"so the store never reaches the object",
		},
		{
			// A value no tag can name, written into a slot: the door declines and the refusal IS the answer,
			// unchanged on the parent — the row promotes what `elemKindTag` can name and nothing else
			// (`promoteMixed`'s own guard).
			name:     "a value the tag cannot name, stored",
			src:      "xs = [1]\nxs[0] = lambda x: x\nprint(len(xs))\n",
			wantExit: 1, wantStderr: "a lambda used as a value has no compiled representation",
			dossier: "**Gap R.150** / **Gap R.151** (**ADR 0283**) — the reference stores a function object; " +
				"a lambda is declared rather than assigned, so there is nothing for the slot to hold",
		},
		{
			// The one the promotion cannot help: a `range` is not a value this backend carries, so the slot
			// holds the loop word and the print says its number. Byte-identical on the parent — the object
			// never had a tag to be asked about.
			name:     "a range stored in a slot",
			src:      "xs = [1]\nxs[0] = range(2)\nprint(xs)\n",
			wantExit: 0, wantOut: "[2]\n",
			dossier: "**Gap R.293** — the reference prints `[range(0, 2)]`; a range is a loop the compiler sees " +
				"through, not an object, so this slot was never a container to promote",
		},
		{
			// The join the promotion cannot reach either: it wants a constant list and asks nothing of the
			// slots. Unchanged on the parent, pinned beside it because a reader of this row tries it.
			name:     "a join over a container the program mutated",
			src:      "xs = [1, 2]\nxs[0] = \"a\"\nxs[1] = \"b\"\nprint(\", \".join(xs))\n",
			wantExit: 1, wantStderr: "join() argument must be a constant list",
			dossier: "**Gap I.2** / **Gap R.174** — the reference prints `a, b`; the method folds a literal and " +
				"this list's slots are the runtime's",
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

// TestTheStalePayloadAnswerIsNotMistakenForAnAnswer is the measurement that turns the chained-comparison
// refusal from a regression into what it is: on this row's parent a comparison over the written slot read
// the number the store never wrote, so it agreed with the reference only when that stale number happened to
// satisfy the test. The ordinary comparison — the position the tag CAN travel through — answers today, and
// these two rows fail if it ever reads a payload again.
func TestTheStalePayloadAnswerIsNotMistakenForAnAnswer(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct{ name, src, want string }{
		{"the stale payload would have answered False here", "xs = [1, 2]\nxs[0] = 2.5\nprint(xs[0] > 1)\n", "True\n"},
		{"the stale payload would have answered True here", "xs = [1, 2]\nxs[0] = 2.5\nprint(xs[0] > 3)\n", "False\n"},
		{"the stale payload would have answered 2 here", "xs = [1, 2]\nxs[0] = 2.5\nprint(xs[0] * 2)\n", "5.0\n"},
	} {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, err := compileAndRun(t, tc.src)
			if err != nil {
				t.Fatalf("the compiled leg refused a program the reference prints %q: %v\n%s", tc.want, err, tc.src)
			}
			if got != tc.want {
				t.Fatalf("printed %q, want %q — the comparison is reading a payload again\n%s", got, tc.want, tc.src)
			}
		})
	}
}
