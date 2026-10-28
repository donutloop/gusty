// Roadmap **Gap R.487** / **Gap R.488**, ADR 0439 — a COMPREHENSION written as the element of a
// container literal answers the container its own brace spells.
//
//	print([[{"a": 1} for x in range(1)], 1])   # [[{'a': 1}], 1]   the reference
//	                                           # [1, 1]            this backend, before ADR 0439
//	print([[[x for x in [1]]]])                # [[[1]]]           the reference
//	                                           # exit 2, llc: "global variable reference must have
//	                                           #   pointer type" on `rt_set_elem(i32 %t, 0, i32 @.lst1)`
//
// One construct, two faces, and both came from doors that ask a slot what it holds without asking the
// comprehension. A slot of a compiled container is a pair — a word and the tag that says what the word
// is — and the BUILD door had understood a comprehension since **ADR 0192**: `containerOperand` and
// `containerHandleOf` each carry a `*Comp` arm, and the constant-folded spelling is materialised into the
// heap there precisely because **ADR 0188** forbade handing `@.lst1` to a helper whose argument is an
// `i32`. The doors that DECIDE never grew the arm. `nestedContainerElem` stopped at the three literal
// spellings and a name, so `heapElemKind` walked past the container road into the ordinary value road —
// which for a folded comprehension answers the NAME OF A GLOBAL, and the module `llc` rejects is exit
// **2**, the code **ADR 0166** reserves for a compiler bug, paid for an ordinary program (**Gap R.488**).
// `elemKindTag` had no `*Comp` case either, so the spelling that did reach the runtime builder stored a
// good handle beside the blanket `TagInt`, and the printer read the handle as the allocator's number:
// `[1, 1]` at the exit code that means the program ran (**Gap R.487**).
//
// The fix is the missing arm on the deciding side, taken from the one reading `compSlotIsContainer` gives:
// the tag, the store and the census all ask it, so a program's element cannot be a list to the printer and
// an integer to the builder (**ADR 0249**'s gate-and-door rule, **ADR 0232**'s one question, **ADR 0438**'s
// same fix one spelling earlier). The taggability of the comprehension's CONTENTS is asked of the door the
// literals already ask (`taggableNestedElem`), so a comprehension whose element nests deeper than any slot
// can be tagged declines with the sentence that names the missing rule instead of emitting a word nobody can
// lower — and a comprehension whose contents are fine but whose KEY is a container declines too:
// `print({[x for x in [1]]: 1})` refuses with the hashing rule, naming "a list comprehension" rather than
// `*lang.Comp` (**Gap R.232**'s and **Gap R.249**'s rule about a sentence that prints Go where the source
// belongs).
//
// What this file pins:
//
//   - the answering family on both witness legs — the compiled leg and the record leg — over every door
//     that writes a slot with a comprehension in it: a list element, a dict value, a set and a dict
//     comprehension as an element, an `append`, a `d[k] =`, a tuple element, the fold and the runtime
//     builder, a comprehension over a call, and the roads that READ the element back (two levels of
//     subscript, `len`, `in`, a loop, `==`, `str`, a repeat by a number);
//   - the controls that answered before and must not move: a numeric literal still takes the plain printer
//     and keeps its static array, and a spelled nested container literal still travels tagged;
//   - the IR, so the answer cannot be a print-time lie: the slot carries (register, TagList/TagDict/TagSet),
//     the object says its slots are the truth, the mixed printer is chosen, and no compile-time container
//     global sits in an operand position — the instruction shape **Gap R.488** was measured on;
//   - the door that still declines, in words: a dict keyed by a comprehension, whose sentence names the
//     missing hashing rule and the construct, and never a Go type.
package lang

import (
	"strings"
	"testing"
)

// comprehensionElementFamily is the answering family: a comprehension written where a container literal
// keeps a slot. Every `want` was read off `python3` beside being written here, and the same programs run
// through the CLI against a live CPython in `integration/comprehension_element_test.go`.
func comprehensionElementFamily() []struct {
	name string
	src  string
	want string
} {
	return []struct {
		name string
		src  string
		want string
	}{
		// The two programs **Gap R.487** was filed by: the element is a comprehension the folder cannot
		// see, so the runtime builder made the object and the tag road called it an integer.
		{"a dict comprehension as an element", "print([[{\"a\": 1} for x in range(1)], 1])\n", "[[{'a': 1}], 1]\n"},
		{"a comprehension over a call, as an element", "def f():\n    return 1\nprint([[f() for x in range(1)], 2])\n", "[[1], 2]\n"},
		// **Gap R.488**'s program: the same element one level deeper, where the road reached `llc` and the
		// toolchain answered with exit 2 — ADR 0166's own-bug code.
		{"a comprehension two levels down", "print([[[x for x in [1]]]])\n", "[[[1]]]\n"},
		// The fold and the runtime builder, both as an element: the first is the spelling whose value IS a
		// global name and must be materialised, the second is the spelling whose value is already a handle
		// and only needed its kind named.
		{"a folded comprehension beside a number", "print([[x for x in [1]], 1])\n", "[[1], 1]\n"},
		{"a comprehension with a filter, as an element", "print([[x for x in [1] if x], 1])\n", "[[1], 1]\n"},
		{"a text comprehension, as an element", "print([[v for v in [\"a\", \"b\"]], 1])\n", "[['a', 'b'], 1]\n"},
		{"a comprehension of dicts, as an element", "print([[{\"a\": x} for x in range(2)], 1])\n", "[[{'a': 0}, {'a': 1}], 1]\n"},
		{"a nested-clause comprehension, as an element", "print([[[x for x in [y]] for y in [1, 2]], 3])\n", "[[[1], [2]], 3]\n"},
		{"two comprehensions in one literal", "print([[x for x in [1]], [y for y in [2]]])\n", "[[1], [2]]\n"},
		{"a comprehension beside a text", "print([[x for x in [1]], \"a\"])\n", "[[1], 'a']\n"},
		// Every door that WRITES the slot: the list literal, the dict's value, the set and the dict spelled
		// as elements, the mutation roads and the immutable spelling. They ask one question, so one reading
		// answers all of them.
		{"a comprehension as a dict value", "print([{\"k\": [x for x in [1]]}])\n", "[{'k': [1]}]\n"},
		{"a set comprehension, as an element", "print([{x for x in [1]}])\n", "[{1}]\n"},
		{"a dict comprehension, as an element", "print([{1: x for x in [1]}])\n", "[{1: 1}]\n"},
		{"a comprehension appended to a list", "xs = []\nxs.append([x for x in [1]])\nprint(xs)\n", "[[1]]\n"},
		{"a comprehension written into a dict entry", "d = {}\nd[\"k\"] = [x for x in [1]]\nprint(d)\n", "{'k': [1]}\n"},
		{"a comprehension as a tuple element", "print(([x for x in [1]], 2))\n", "([1], 2)\n"},
		{"a comprehension bound inside a literal", "ys = [[x for x in [1]]]\nprint(ys)\n", "[[1]]\n"},
		// The roads that READ the element back — the reason the object has to describe itself and not just
		// the print call, which is **ADR 0232**'s rule and the reason a print-time fix was rejected.
		{"the comprehension element read two levels down", "print([[x for x in [1]]][0][0])\n", "1\n"},
		{"the comprehension element measured", "print(len([[x for x in [1]]][0]))\n", "1\n"},
		{"the comprehension element tested for membership", "print(1 in [[x for x in [1]]][0])\n", "True\n"},
		{"the comprehension element looped over", "for v in [[x for x in [1]]][0]:\n    print(v)\n", "1\n"},
		{"a literal of comprehension elements compared for equality", "print([[x for x in [1]]] == [[1]])\n", "True\n"},
		{"a literal of comprehension elements rendered by str", "print(str([[x for x in [1]]]))\n", "[[1]]\n"},
		{"the comprehension element repeated by a number", "print([[x for x in [1]]][0] * 2)\n", "[1, 1]\n"},
		{"a comprehension over a call, bound and read back", "def mk():\n    return [1]\nxs = [[x for x in mk()]]\nprint(xs[0] == [1])\n", "True\n"},
	}
}

// TestAComprehensionWrittenAsAnElementAnswersItsOwnKind is **Gap R.487** and **Gap R.488** paid: every row
// answers the reference's bytes on the compiled leg and the record leg.
func TestAComprehensionWrittenAsAnElementAnswersItsOwnKind(t *testing.T) {
	t.Parallel()
	for _, tc := range comprehensionElementFamily() {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			runBothLegs(t, tc.src, tc.want)
		})
	}
}

// TestTheComprehensionElementIsBuiltAsAnObject asks the module, because an answer that comes from the print
// call rather than from the object is a print-time lie the next reader of the slot does not share — and the
// next reader is in this very file (`[0][0]`, `len`, `in`, the loop). The rows are the pair the emitter
// writes (tag beside word, census beside both) in both directions: the element that must gain the census bit
// and the numeric literal that must not.
func TestTheComprehensionElementIsBuiltAsAnObject(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name      string
		src       string
		want      []string
		deny      []string
		denialWhy string
	}{
		{
			// The shape **Gap R.488** was measured on. The element is folded, so the value road answers `@.lst1`
			// and the slot would carry a GLOBAL: `llc` rejects `rt_set_elem(i32 %h, i32 0, i32 @.lst1)` at exit
			// 2. The slot now carries the register the heap builder made and the tag that says list (5,
			// value.go's iota), the object says its slots are the truth (bit 8), and the printer is the one
			// that reads the pair.
			name: "a folded comprehension element is materialised and tagged list",
			src:  "print([[x for x in [1]], 1])\n",
			want: []string{
				"call void @rt_tag_elem(i32 %t1, i32 0, i32 5)",
				"call void @rt_set_elem(i32 %t1, i32 0, i32 %t3)",
				"call void @rt_mark_estr(i32 %t1, i32 8)",
				"call void @rt_print_list_mixed(i32 %t1, i32 0)",
			},
			deny: []string{"i32 @.lst", "call void @rt_print_list(i32 %t1, i32 0)"},
			denialWhy: "a compile-time list global in an operand position is the module `llc` rejects at exit 2 " +
				"(**Gap R.488**), and the single-kind printer is the road that printed the handle as the number " +
				"the allocator handed out — `[1, 1]` where the reference prints `[[1], 1]` (**Gap R.487**)",
		},
		{
			// The runtime spelling: the comprehension's own builder made the object (`%h1`), and the tag the
			// slot carries is the container's kind, read out of the brace rather than out of a default.
			name: "a runtime comprehension element carries the tag its brace spells",
			src:  "print([[{\"a\": 1} for x in range(1)], 1])\n",
			want: []string{
				"call void @rt_tag_elem(i32 %t1, i32 0, i32 5)",
				"call void @rt_set_elem(i32 %t1, i32 0, i32 %h1)",
				"call void @rt_dict_put(i32 %t4, i32 %t6, i32 1)",
				"call void @rt_print_list_mixed(i32 %t1, i32 0)",
			},
			deny: []string{"i32 @.dict", "call void @rt_tag_elem(i32 %t1, i32 0, i32 0)"},
			denialWhy: "the dict global in an operand position is the same exit-2 shape, and TagList (5) beside " +
				"the register is the fix: TagInt (0) beside it is the blanket reading that printed `[1, 1]` " +
				"(**Gap R.487**)",
		},
		{
			// A set comprehension and a dict comprehension as elements answer TagSet (7) and TagDict (6),
			// which is why the arm reads the comprehension's own kind rather than assuming a list.
			name: "a set comprehension element is tagged set",
			src:  "print([{x for x in [1]}])\n",
			want: []string{
				"call void @rt_tag_elem(i32 %t1, i32 0, i32 7)",
				"call void @rt_mark_estr(i32 %t1, i32 8)",
				"call void @rt_print_list_mixed(i32 %t1, i32 0)",
			},
			deny: []string{"i32 @.set", "call void @rt_tag_elem(i32 %t1, i32 0, i32 0)"},
			denialWhy: "the set global in an operand position is the exit-2 class, and TagInt beside a handle is " +
				"the printer reading the allocator's slot as the value (**Gap R.487**)",
		},
		{
			name: "a dict comprehension element is tagged dict",
			src:  "print([{1: x for x in [1]}])\n",
			want: []string{
				"call void @rt_tag_elem(i32 %t1, i32 0, i32 6)",
				"call void @rt_mark_estr(i32 %t1, i32 8)",
				"call void @rt_print_list_mixed(i32 %t1, i32 0)",
			},
			deny: []string{"i32 @.dict", "call void @rt_tag_elem(i32 %t1, i32 0, i32 0)"},
			denialWhy: "the dict global in an operand position is the exit-2 class, and TagInt beside a handle is " +
				"the printer reading the allocator's slot as the value (**Gap R.487**)",
		},
		{
			// The control the new arms had to survive: a literal of numbers, every slot TagInt, the plain
			// printer, and `sum`/`min`/`max` still folding from the static array (**ADR 0239**). The census
			// may not fire here, or every numeric loop loses its fold.
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
			// …and the control beside it: the comprehension element did not steal the spelled container
			// literal's road — the inner object is still built by the same builder and tagged the same way
			// (**ADR 0189**, **ADR 0438**).
			name: "a spelled nested container literal is untouched",
			src:  "print([[1, 2], 1])\n",
			want: []string{
				"call void @rt_tag_elem(i32 %t1, i32 0, i32 5)",
				"call void @rt_print_list_mixed(i32 %t1, i32 0)",
			},
			deny:      []string{"i32 @.lst"},
			denialWhy: "the handle in a register, never the global name (**Gap R.488**'s instruction shape)",
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

// TestAComprehensionKeyRefusesByNamingTheHashingRule is the door that still declines, declining in words.
// A comprehension builds a list, a dict or a set, and none of the three has a hash: the entry would be filed
// under whichever handle the allocator handed out, which is the rule this backend already states for the
// literal spelling (`{[1]: 1}`) and had said nothing about for this one. The sentence is an interface
// (docs/operations.md), so it names the construct in the program's own words — `*lang.Comp` is **Gap
// R.232**'s and **Gap R.249**'s disease, and a compiler temp where the source belongs is the same disease.
func TestAComprehensionKeyRefusesByNamingTheHashingRule(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name string
		src  string
		need []string
		deny []string
	}{
		{
			name: "a list comprehension as a key",
			src:  "print({[x for x in [1]]: 1})\n",
			need: []string{"a list comprehension", "hash", "Key the dict by a value"},
			deny: []string{"*lang.", "%!s(", "@.lst", "__cans"},
		},
		{
			name: "a dict comprehension as a key",
			src:  "print({{1: x for x in [1]}: 1})\n",
			need: []string{"a dict comprehension", "hash"},
			deny: []string{"*lang.", "%!s(", "@.dict"},
		},
		{
			name: "a set comprehension as a key",
			src:  "print({{x for x in [1]}: 1})\n",
			need: []string{"a set comprehension", "hash"},
			deny: []string{"*lang.", "%!s(", "@.set"},
		},
		// The controls: the literal spellings keep the sentence they always had, and the one container the
		// language DOES hash keeps answering (**ADR 0361**).
		{
			name: "a list literal as a key still refuses",
			src:  "print({[1]: 1})\n",
			need: []string{"a list literal", "hash"},
			deny: []string{"*lang.", "%!s("},
		},
	} {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			msg := compileErr(t, tc.src)
			for _, n := range tc.need {
				if !strings.Contains(msg, n) {
					t.Fatalf("the refusal never names %q: %s\n%s", n, tc.src, msg)
				}
			}
			for _, d := range tc.deny {
				if strings.Contains(msg, d) {
					t.Fatalf("the refusal prints the denied %q — a refusal sentence is an interface, not a Go dump: %s", d, msg)
				}
			}
		})
	}
}

// TestATupleCarryingAComprehensionKeepsItsOwnHashRule is the control on the tuple side of the same door:
// `(1, 2)` is the container this backend hashes, and a tuple that CARRIES an unhashable object stays
// refused. A comprehension inside the tuple builds a list, so the walk that decides is the reference's own
// walk, asked of the brace too (roadmap L11.3, **ADR 0361**).
func TestATupleCarryingAComprehensionKeepsItsOwnHashRule(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name string
		src  string
		want string
		need string
	}{
		{name: "a tuple of numbers hashes", src: "print({(1, 2): 1})\n", want: "{(1, 2): 1}\n"},
		{name: "a tuple carrying a list literal stays refused", src: "print({([1],): 1})\n", need: "hash"},
	} {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if tc.want != "" {
				runBothLegs(t, tc.src, tc.want)
				return
			}
			msg := compileErr(t, tc.src)
			if !strings.Contains(msg, tc.need) {
				t.Fatalf("the refusal never names %q: %s\n%s", tc.need, tc.src, msg)
			}
		})
	}
}
