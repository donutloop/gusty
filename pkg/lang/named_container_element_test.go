// Roadmap Gap R.486 / Gap R.489 / Gap R.439, ADR 0438 — a container the program NAMED answers as an
// element, and the loop item that a container name was used for keeps its own kind.
//
//	xs = [1]
//	print([xs, 2])      # [[1], 2]            the reference
//	                    # [0, 2]              this backend, before ADR 0438
//
// One element, two roads, one missing question. A slot of a compiled container is a pair — a word and the
// tag that says what the word is — and the two halves used to be asked of two different things. The word
// and the tag came from `heapElemKind`/`elemTagFor`, which do know that `xs` holds a container (they read
// the runtime's container tables and write `TagList` beside the handle). Whether the OBJECT must be read
// through those tags came from `literalNeedsTags`, which asked the element's SPELLING and stopped at
// `isContainerLiteral` — and a name has no spelling. So the pair was written correctly and the header
// claimed one element kind, and `rt_print_list` printed the handle as the number it is: `1`, the heap slot
// the allocator handed out, at the exit code that means the program ran (**Gap R.486**, and the same wrong
// answer **Gap R.439** had filed from the other side: `xs = [1]` / `print([xs, 2])` printing `[0, 2]`).
//
// The census now asks the tag road's own answer (`elementNameHoldsContainer` reads `elemKindTag`), which is
// **ADR 0232**'s rule that the slots the builder wrote are the truth about the object, and **ADR 0359**'s
// fix for the interpolation element applied to the container element.
//
// Asking the tag road once also exposed the second defect, which was hiding behind the first. The tag road's
// *Name arm consulted the container TABLES, which are cumulative: `xs = [7]` puts `xs` in `listVars` for the
// rest of the module, and `for xs in [1, 2]:` rebinds the same name to integers. The iteration's own record
// (`loopItemTag`) is consulted first — but only when the item's kind is one it can name, and a plain integer
// is not, so the reading fell through to the stale table and the slot stored the integer 1 tagged `TagList`.
// Nothing noticed while the census ignored names; with the census honest, `xs = [7]` / `for xs in [1, 2]:` /
// `print([xs, 1.5])` printed `[, 1.5]` — the tag-reading printer went looking for a container at address 1
// and printed nothing (**Gap R.489**). The tag road now asks the ITEM when the name is the item, which is
// what **ADR 0233**/**Gap R.358** already do for the tag table, asked of the name the unroll replaced.
//
// What this file pins:
//
//   - the answering family on both witness legs — the compiled leg and the record leg, for a list, a dict, a
//     set, a name each, two in one literal, a name beside a text and a number, one nested in another literal,
//     read back out of the literal, and put into a dict value;
//   - the loop-item family: an int item beside a float, a text and a verdict, a dict name and a set name
//     rebound, and the container-item control that must keep its handles;
//   - the IR, so the fix cannot be a print-time lie: the slot carries (handle, TagList), the object says its
//     slots are the truth (bit 8), the mixed printer is chosen, the numeric control still takes the plain
//     printer, and a loop item's slot carries TagInt and never TagList;
//   - that no global reaches an operand: no `@.lst` anywhere near `rt_set_elem` (**Gap R.488** owns the shape
//     that still does, and this row does not fix it).
package lang

import (
	"strings"
	"testing"
)

// namedContainerElementFamily is the answering family: a container the program holds in a NAME, written as
// an element of a literal. Every `want` was read off `python3` beside being written here.
func namedContainerElementFamily() []struct {
	name string
	src  string
	want string
} {
	return []struct {
		name string
		src  string
		want string
	}{
		// The row this cycle was filed by — a dict the program named, beside a number.
		{"a dict the program named, beside a number", "sa = {1: \"a\", \"b\": 2}\nprint([sa, 1])\n",
			"[{1: 'a', 'b': 2}, 1]\n"},
		{"a dict the program named, alone in the literal", "sa = {1: \"a\", \"b\": 2}\nprint([sa])\n",
			"[{1: 'a', 'b': 2}]\n"},
		// The same shape **Gap R.439** filed from the other side: the name's own literal.
		{"a list the program named, beside a number", "xs = [1]\nprint([xs, 2])\n", "[[1], 2]\n"},
		{"two lists the program named, in one literal", "xs = [1]\nys = [2]\nprint([xs, ys])\n", "[[1], [2]]\n"},
		{"a longer list the program named", "xs = [1, 2]\nprint([xs, 1])\n", "[[1, 2], 1]\n"},
		{"two dicts the program named", "sa = {1: \"a\"}\nsb = {2: \"b\"}\nprint([sa, sb])\n",
			"[{1: 'a'}, {2: 'b'}]\n"},
		{"a list, a dict and a text in one literal", "xs = [1, 2]\nsd = {\"k\": 1}\nprint([xs, sd, \"z\"])\n",
			"[[1, 2], {'k': 1}, 'z']\n"},
		{"a set the program named", "st = {1}\nprint([st, 1])\n", "[{1}, 1]\n"},
		{"a named dict one level down", "sa = {1: \"a\"}\nprint([[sa], 1])\n", "[[{1: 'a'}], 1]\n"},
		// The roads that READ the slot back, which are the reason the object has to describe itself and not
		// just the print call.
		{"a named container iterated from a literal", "xs = [1, 2]\nfor v in [xs, [3]]:\n    print(v)\n",
			"[1, 2]\n[3]\n"},
		{"the named element read out of the literal by position", "xs = [1, 2]\nprint([xs, 1][0])\n", "[1, 2]\n"},
		{"the named container as a dict value", "xs = [1, 2]\nd = {\"a\": xs}\nprint(d)\n", "{'a': [1, 2]}\n"},
	}
}

// loopItemOverAFrozenContainerName is **Gap R.489**'s family: the same NAME used first for a container and
// then by a `for`, so the container table and the iteration disagree about what the name is.
func loopItemOverAFrozenContainerName() []struct {
	name string
	src  string
	want string
} {
	return []struct {
		name string
		src  string
		want string
	}{
		{"an int item beside a float, after a list binding", "xs = [7]\nfor xs in [1, 2]:\n    print([xs, 1.5])\n",
			"[1, 1.5]\n[2, 1.5]\n"},
		{"an int item beside a text, after a list binding", "xs = [7]\nfor xs in [1, 2]:\n    print([xs, \"a\"])\n",
			"[1, 'a']\n[2, 'a']\n"},
		{"an int item compared as a literal, after a list binding", "xs = [7]\nfor xs in [1, 2]:\n    print([xs] == [1])\n",
			"True\nFalse\n"},
		{"an int item after a DICT binding", "sd = {1: 2}\nfor sd in [1, 2]:\n    print([sd, \"a\"])\n",
			"[1, 'a']\n[2, 'a']\n"},
		{"a float item after a SET binding", "st = {1}\nfor st in [1.5, 2]:\n    print([st, 1])\n",
			"[1.5, 1]\n[2, 1]\n"},
		// The control: an item that IS a container, in a name that was a container before. The handles must
		// still travel, which is why the fix reads the item rather than refusing the name.
		{"a container item after a container binding", "xs = [7]\nfor xs in [[1], [2]]:\n    print([xs, 1])\n",
			"[[1], 1]\n[[2], 1]\n"},
		{"an int item alone, after a list binding", "xs = [7]\nfor xs in [1, 2]:\n    print([xs])\n", "[1]\n[2]\n"},
		{"a container item after a dict binding", "sa = {1: \"a\"}\nfor sa in [1, 2]:\n    print([sa, [3]])\n",
			"[1, [3]]\n[2, [3]]\n"},
	}
}

func runBothLegs(t *testing.T, src, want string) {
	t.Helper()
	got, err := compileAndRun(t, src)
	if err != nil {
		t.Fatalf("the compiled leg refused a program the reference prints %q: %v\n%s", want, err, src)
	}
	if got != want {
		t.Fatalf("printed %q, want %q\n%s", got, want, src)
	}
	if !HasGoldenAnswer(src) {
		t.Fatalf("%q is not on the record — record it from the reference with tools/recmerge\n%s", src, src)
	}
	recorded, err := runGoldenStdout(t, src)
	if err != nil {
		t.Fatalf("the record leg could not answer this source: %v\n%s", err, src)
	}
	if recorded != want {
		t.Fatalf("the record reads %q and the compiled leg printed %q\n%s", recorded, got, src)
	}
}

// TestAContainerTheProgramNamedAnswersAsAnElement is Gap R.486 (and the Gap R.439 half of the same shape)
// paid: every row answers the reference's bytes on the compiled leg and the record leg.
func TestAContainerTheProgramNamedAnswersAsAnElement(t *testing.T) {
	t.Parallel()
	for _, tc := range namedContainerElementFamily() {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			runBothLegs(t, tc.src, tc.want)
		})
	}
}

// TestTheLoopItemOverAContainerNameKeepsItsOwnKind is Gap R.489 paid: the name's container history is not
// the element's kind, and the iteration's record is.
func TestTheLoopItemOverAContainerNameKeepsItsOwnKind(t *testing.T) {
	t.Parallel()
	for _, tc := range loopItemOverAFrozenContainerName() {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			runBothLegs(t, tc.src, tc.want)
		})
	}
}

// TestTheNamedContainerElementIsBuiltTagged asks the IR, because an answer that comes from the print call
// rather than from the object is a print-time lie that the next reader of the slot does not share. The four
// rows are the pair the emitter writes — tag beside word, census beside both — in both directions: the
// container element that must gain the census bit, and the numeric literal that must not.
func TestTheNamedContainerElementIsBuiltTagged(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name      string
		src       string
		want      []string
		deny      []string
		denialWhy string
	}{
		{
			// TagList is 5 (value.go's iota). The slot carries the handle in a register and the tag that
			// says it is a list; the object says its slots are the truth (bit 8); the printer is the one
			// that reads the pair. Before ADR 0438 all four of these were true of the tag and none of the
			// census: `[0, 2]` at exit 0.
			name: "a named list is built tagged, and the object says so",
			src:  "xs = [1]\nprint([xs, 2])\n",
			want: []string{
				"call void @rt_tag_elem(i32 %t2, i32 0, i32 5)",
				"call void @rt_set_elem(i32 %t2, i32 0, i32 %t4)",
				"call void @rt_mark_estr(i32 %t2, i32 8)",
				"call void @rt_print_list_mixed(i32 %t2, i32 0)",
			},
			deny: []string{"@.lst", "call void @rt_print_list(i32 %t2, i32 0)"},
			denialWhy: "a compile-time list global in an operand position is the module `llc` rejects at exit 2 " +
				"(**Gap R.488** owns it), and the single-kind printer is the road that printed the handle as a " +
				"number — `[0, 2]` at exit 0 (**Gap R.439**, **Gap R.486**)",
		},
		{
			// The control the census change had to survive: a literal of numbers, every slot TagInt, the
			// plain printer, and `sum`/`min`/`max` still folding from the static array (**ADR 0239**).
			name: "a numeric literal is untouched",
			src:  "print([1, 2, 3])\n",
			want: []string{
				"call void @rt_set_elem(i32 %t1, i32 2, i32 3)",
				"call void @rt_tag_elem(i32 %t1, i32 2, i32 0)",
				"call void @rt_print_list(i32 %t1, i32 0)",
			},
			deny: []string{"call void @rt_print_list_mixed(i32 %t1, i32 0)"},
			denialWhy: "the mixed printer walks two words per slot; a literal the records still describe as " +
				"numbers does not need it, and a census that fired here would cost every numeric loop its fold",
		},
		{
			// **Gap R.489** asked of the module: the item is the integer 1, so its slot is tagged TagInt.
			// The stale `listVars["xs"]` tagged it TagList (5) while the census ignored names, and the
			// mixed printer then went looking for a container at address 1.
			name: "a loop item keeps TagInt after the name was a container",
			src:  "xs = [7]\nfor xs in [1, 2]:\n    print([xs, 1.5])\n",
			want: []string{
				"call void @rt_tag_elem(i32 %t2, i32 0, i32 0)",
				"call void @rt_tag_elem(i32 %t9, i32 0, i32 0)",
				"call void @rt_mark_estr(i32 %t2, i32 8)",
			},
			deny: []string{"call void @rt_tag_elem(i32 %t2, i32 0, i32 5)", "call void @rt_tag_elem(i32 %t9, i32 0, i32 5)"},
			denialWhy: "TagList beside the integer 1 is the stale container table answering for the iteration — " +
				"`[, 1.5]` where the reference prints `[1, 1.5]` (**Gap R.489**)",
		},
		{
			// …and the item that IS a container keeps its handle and its TagList, which is the control that
			// stops the fix from being "a rebound name is never a container".
			name: "a container item keeps its handle and its TagList",
			src:  "xs = [7]\nfor xs in [[1], [2]]:\n    print([xs, 1])\n",
			want: []string{
				"call void @rt_tag_elem(i32 %t4, i32 0, i32 5)",
				"call void @rt_set_elem(i32 %t4, i32 0, i32 %t6)",
				"call void @rt_mark_estr(i32 %t4, i32 8)",
				"call void @rt_print_list_mixed(i32 %t4, i32 0)",
			},
			deny: []string{"@.lst"},
			denialWhy: "the handle in a register, never the global name (**Gap R.488** owns the shape that still " +
				"emits one)",
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

