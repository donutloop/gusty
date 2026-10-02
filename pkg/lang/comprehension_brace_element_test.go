package lang

import (
	"fmt"
	"strings"
	"testing"
)

// A `{…}` display finishes at its `}`. A `for` after that brace belongs to whatever *encloses* the
// display, which for `[{1, 2} for x in xs]` is the list comprehension — not the set literal.
//
// The parser used to have one branch for both readings: after a display's `}`, a following `for`
// finished the display into a comprehension of its own. Inside a `[` that stole the enclosing
// comprehension's clause, so the program above parsed as *a list containing one set comprehension*:
//
//	                                   before                     now
//	`[{1, 2} for x in [1]]`            ListLit[Comp(set, [1,2])]   Comp(list)[SetLit{1,2}]
//	`[{"k": x} for x in [1, 2]]`       ListLit[Comp(dict, …)]      Comp(list)[DictLit]
//
// and both backends answered the mis-parse, in agreement — `[{1,2} for x in [1]]` printed `{1}`
// where CPython prints `{1, 2}`, and `[{"k": x} for x in [1,2]]` printed one dict holding both
// entries, `{'k': 1, 'k': 2}`. Two engines agreeing on a wrong answer is the reason this row is
// pinned by the AST shape as well as by the output (roadmap Gap R.74, ADR 0244).
//
// The call-argument form keeps its reading: `len({x*x} for x in xs)` is still a comprehension built
// from the display, because there the display *is* the whole expression the `for` completes.

func TestBraceDisplayDoesNotStealTheEnclosingFor(t *testing.T) {
	for _, tc := range []struct {
		name string
		expr string
		want string
	}{{
		"a set literal is an element of the list comprehension",
		"[{1, 2} for x in [1]]",
		"Comp(list)[SetLit]",
	}, {
		"a dict literal is an element of the list comprehension",
		"[{\"k\": x} for x in [1]]",
		"Comp(list)[DictLit]",
	}, {
		"a filter does not change the reading",
		"[{1, 2} for x in [1] if x]",
		"Comp(list)[SetLit]",
	}, {
		"a nested display is still an element",
		"[{1, 2} for x in y]",
		"Comp(list)[SetLit]",
	}, {
		"a list element keeps the reading it always had",
		"[[1, 2] for x in [1]]",
		"Comp(list)[ListLit]",
	}, {
		// The display-then-for form outside a list display is the call-argument comprehension reading,
		// and this change must not take it away. CPython reads `f({...} for x in xs)` as a *generator*
		// argument (so `len(...)` of it raises TypeError there); that divergence is its own row, and
		// the shape asserted here is the reading this language gives the form today.
		"a set display followed by for is still a set comprehension",
		"{x * x} for x in [1, 2]",
		"Comp(set)[BinOp]",
	}, {
		"a dict display followed by for is still a dict comprehension",
		"{x: x * 10} for x in [1, 2]",
		"Comp(dict)[Name]",
	}, {
		"a set comprehension inside the braces is unaffected",
		"{x for x in [1, 2]}",
		"Comp(set)[Name]",
	}, {
		"a bare set literal is a set literal",
		"{1, 2}",
		"SetLit",
	}, {
		"a list of set literals is a list of set literals",
		"[{1, 2}, {3}]",
		"ListLit",
	}} {
		prog, err := Parse(tc.expr + "\n")
		if err != nil {
			t.Fatalf("%s: %q did not parse: %v", tc.name, tc.expr, err)
		}
		if len(prog.Stmts) != 1 {
			t.Fatalf("%s: %q parsed as %d statements", tc.name, tc.expr, len(prog.Stmts))
		}
		got := shape(prog.Stmts[0].(*ExprStmt).Expr)
		if got != tc.want {
			t.Errorf("%s: %q parsed as %s, want %s", tc.name, tc.expr, got, tc.want)
		}
	}
}

// shape is the AST answer, printed the way the table above reads it. The output tables below can
// only show that two engines agree; this shows they agree on the right *program*.
func shape(e Expr) string {
	switch n := e.(type) {
	case *Comp:
		kind := "list"
		switch n.Kind {
		case CompDict:
			kind = "dict"
		case CompSet:
			kind = "set"
		}
		var parts []string
		for _, el := range n.Elems {
			parts = append(parts, shape(el))
		}
		for _, k := range n.Keys {
			parts = append(parts, shape(k))
		}
		return fmt.Sprintf("Comp(%s)[%s]", kind, strings.Join(parts, ","))
	case *ListLit:
		return "ListLit"
	case *SetLit:
		return "SetLit"
	case *DictLit:
		return "DictLit"
	case *Name:
		return "Name"
	case *IntLit:
		return "Int"
	case *StrLit:
		return "Str"
	case *BinOp:
		return "BinOp"
	}
	return fmt.Sprintf("%T", e)
}

// TestBraceElementComprehensionRunsLikeCPython is the measured half of the parser fix: the program
// the AST says, run. The expectations are CPython's, taken before the table was written.
//
// The compiled leg runs only the rows whose lowering the compiled backend can finish. A container
// element used to reach `rt_append_tagged(i32 %h1, i32 @.set1, i32 7)` — the *global* in a value
// position, which `llc` rejects — and that was Gap R.75, paid by ADR 0244's materialise-into-the-heap
// door; the rows still carrying it are marked below and measured in
// integration/comprehension_brace_element_test.go, where the exit-code contract is asserted rather
// than hidden. The marks were re-measured for ADR 0247: `print(1 if d[0] == d[1] else 0)` over a
// comprehension of set literals answers on both engines now, so it is no longer skipped.
func TestBraceElementComprehensionRunsLikeCPython(t *testing.T) {
	for _, tc := range []struct {
		name string
		src  string
		want string
		// aotContainerElement marks the rows whose compiled leg still refuses; each names the
		// missing half in its refusal (`in` of a built slot, and indexing a built dict slot).
		aotContainerElement bool
	}{
		{"list comprehension over set literals", "d = [{1, 2} for x in [1, 2]]\nprint(len(d))\n", "2\n", false},
		{"its element is a two-member set", "d = [{1, 2} for x in [1]]\nprint(len(d[0]))\n", "2\n", false},
		{"membership in the element", "d = [{1, 2} for x in [1]]\nprint(1 if 2 in d[0] else 0)\n", "1\n", true},
		{"one set per iteration", "d = [{x} for x in [1, 2]]\nprint(1 if d[0] == d[1] else 0)\n", "0\n", false},
		{"list comprehension over dict literals", "d = [{\"k\": x} for x in [1, 2]]\nprint(len(d))\n", "2\n", false},
		{"each dict holds its own entry", "d = [{\"k\": x} for x in [1, 2]]\nprint(d[1][\"k\"])\n", "2\n", true},
		{"first dict is not the second", "d = [{\"k\": x} for x in [1, 2]]\nprint(len(d[0]))\n", "1\n", false},
		{"a set element keeps its own members", "s = [{1, 2}, {3}]\nprint(len(s[0]), len(s[1]))\n", "2 1\n", false},
		{"nested display elements", "d = [{1, 2}, [3]]\nprint(len(d), len(d[0]), len(d[1]))\n", "2 2 1\n", false},
	} {
		out, err := InterpreterRun(tc.src)
		if err != nil {
			t.Fatalf("%s: interpreter: %v", tc.name, err)
		}
		if out != tc.want {
			t.Errorf("%s: interpreter printed %q, want CPython's %q\nsrc: %s", tc.name, out, tc.want, tc.src)
		}
		if tc.aotContainerElement {
			continue
		}
		res, cerr := Compile(tc.src)
		if cerr != nil {
			t.Fatalf("%s: compiled backend refused: %v", tc.name, cerr)
		}
		if got := runIR(t, res.IR); got != tc.want {
			t.Errorf("%s: compiled ran %q, want CPython's %q\nsrc: %s", tc.name, got, tc.want, tc.src)
		}
	}
}

// TestMisParsedShapeIsNotAcceptedAnymore pins the AST the old branch produced, so a regression that
// quietly reintroduces it fails here even if some future printer makes the output look right again.
func TestMisParsedShapeIsNotAcceptedAnymore(t *testing.T) {
	prog, err := Parse("[{1, 2} for x in [1]]\n")
	if err != nil {
		t.Fatal(err)
	}
	lit, ok := prog.Stmts[0].(*ExprStmt).Expr.(*Comp)
	if !ok || lit.Kind != CompList || len(lit.Elems) != 1 {
		t.Fatalf("the old parse (a list containing a set comprehension) is back: %s", shape(prog.Stmts[0].(*ExprStmt).Expr))
	}
	if _, isSet := lit.Elems[0].(*SetLit); !isSet {
		t.Fatalf("the comprehension's element should be a set literal, got %s", shape(lit.Elems[0]))
	}
}

// TestComprehensionOverAOneKindContainerTagsItsElement is ADR 0244's rule about the element, measured
// on both engines: a comprehension that walks a container registers the list it builds with the kind
// its slots hold, so the container and one of its slots tell the same story.
//
// The row that motivated this is quieter than the exit-2 family and just as wrong: the element *is*
// the loop variable, the loop's own facts about it (`internedVars`) are gone by the time the result is
// bound, and the list was registered as a list of numbers. `print(out)` asked the object and printed
// ['a']; `print(out[0])` asked the compiler and printed 0. Every row here therefore prints the
// container and reads a slot of it, because a half-set pair is exactly what the table cannot see.
func TestComprehensionOverAOneKindContainerTagsItsElement(t *testing.T) {
	for _, tc := range []struct{ name, src, want string }{
		{"grown_text_list", "names = [\"a\", \"b\"]\nnames.append(\"c\")\nout = [n for n in names]\nprint(out)\nprint(out[0])\n", "['a', 'b', 'c']\na\n"},
		{"grown_text_list_every_slot", "names = [\"a\", \"b\"]\nnames.append(\"c\")\nout = [n for n in names]\nprint(out[0])\nprint(out[1])\nprint(out[2])\n", "a\nb\nc\n"},
		{"grown_text_list_filter", "names = []\nnames.append(\"a\")\nnames.append(\"b\")\nout = [n for n in names if n == \"a\"]\nprint(len(out))\nprint(out[0])\nprint(out)\n", "1\na\n['a']\n"},
		{
			// A set's iteration order is the runtime's own business (CPython's even moves with the
			// hash seed), so this row asks length and membership rather than the printed list.
			"text_set_element",
			"sa = {\"a\", \"b\"}\nout = [n for n in sa]\nprint(len(out))\nprint(1 if \"a\" in out else 0)\nprint(1 if \"z\" in out else 0)\n",
			"2\n1\n0\n",
		},
		{"dict_keys_all_text", "d = {}\nd[\"a\"] = 1\nd[\"b\"] = 2\nout = [k for k in d]\nprint(out)\n", "['a', 'b']\n"},
		{"dict_values_all_text", "d = {}\nd[\"a\"] = 1\nout = [v for v in d]\nprint(out)\nprint(out[0])\n", "['a']\na\n"},
		{"grown_int_list", "xs = []\nxs.append(1)\nxs.append(2)\nout = [x * 2 for x in xs]\nprint(out)\nprint(out[1])\n", "[2, 4]\n4\n"},
	} {
		out, err := InterpreterRun(tc.src)
		if err != nil {
			t.Fatalf("%s: interpreter: %v", tc.name, err)
		}
		if out != tc.want {
			t.Errorf("%s: interpreter printed %q, want CPython's %q\nsrc: %s", tc.name, out, tc.want, tc.src)
		}
		res, cerr := Compile(tc.src)
		if cerr != nil {
			t.Fatalf("%s: compiled backend refused: %v", tc.name, cerr)
		}
		if got := runIR(t, res.IR); got != tc.want {
			t.Errorf("%s: compiled ran %q, want CPython's %q\nsrc: %s", tc.name, got, tc.want, tc.src)
		}
	}
}

// TestComprehensionOverAMixedContainerTagsItsLoopVariable is Gap R.76 paid rather than refused, on
// both engines.
//
// The comprehension's loop variable was a plain load while `for` over the same container bound the
// (payload, tag) pair ADR 0185 put in `%_x` and `%_x_tag`. The compiled backend therefore printed
// [1, 0, 0] for `[x for x in sa]` over {1, "a", None} — the interned index of "a" and the fold's
// integer for None, both wearing numbers — and walked a dict's two-word entries at stride 1, so
// [k for k in d] over {"a": 1, 2: "b"} printed a key and a value. It now binds the pair, strides a
// dict by its entries, and marks the container it fills self-describing, because nothing static says
// what its slots hold.
func TestComprehensionOverAMixedContainerTagsItsLoopVariable(t *testing.T) {
	for _, tc := range []struct{ name, src, want string }{
		{"mixed_list", "xs = []\nxs.append(1)\nxs.append(\"a\")\nxs.append(None)\nout = [x for x in xs]\nprint(out)\n", "[1, 'a', None]\n"},
		{"mixed_list_every_slot", "xs = []\nxs.append(1)\nxs.append(\"a\")\nxs.append(None)\nout = [x for x in xs]\nprint(out[0])\nprint(out[1])\nprint(out[2])\n", "1\na\nNone\n"},
		{"mixed_list_with_a_float", "xs = []\nxs.append(1)\nxs.append(1.5)\nxs.append(\"a\")\nout = [x for x in xs]\nprint(out)\n", "[1, 1.5, 'a']\n"},
		{"mixed_list_membership", "xs = []\nxs.append(1)\nxs.append(\"a\")\nout = [x for x in xs]\nprint(1 if \"a\" in out else 0)\nprint(len(out))\n", "1\n2\n"},
		{"mixed_list_filtered", "xs = []\nxs.append(1)\nxs.append(\"a\")\nsel = [x for x in xs if x == 1]\nprint(len(sel))\nprint(sel[0])\n", "1\n1\n"},
		{"mixed_list_against_an_equal_list", "xs = []\nxs.append(1)\nxs.append(\"a\")\nys = [x for x in xs]\nzs = [x for x in xs]\nprint(1 if ys == zs else 0)\n", "1\n"},
		{"mixed_set_by_length", "sa = {1, \"a\", None}\nout = [x for x in sa]\nprint(len(out))\n", "3\n"},
		{"mixed_set_membership", "sa = {1, \"a\", None}\nout = [x for x in sa]\nprint(1 if \"a\" in out else 0)\nprint(1 if None in out else 0)\n", "1\n1\n"},
		{"mixed_set_grown", "sa = set()\nsa.add(1)\nsa.add(\"a\")\nout = [x for x in sa]\nprint(out)\n", "[1, 'a']\n"},
		{"mixed_set_into_a_set", "sa = {1, \"a\", None}\nout = {x for x in sa}\nprint(len(out))\nprint(1 if 1 in out else 0)\n", "3\n1\n"},
		{"mixed_dict_keys", "d = {}\nd[\"a\"] = 1\nd[2] = \"b\"\nout = [k for k in d]\nprint(out)\n", "['a', 2]\n"},
		{"mixed_dict_values_asked_as_keys", "d = {1: \"x\", \"k\": 2}\nout = [v for v in d]\nprint(out)\n", "[1, 'k']\n"},
		{"mixed_dict_into_a_dict", "d = {}\nd[\"a\"] = 1\nd[2] = \"b\"\nout = {k: 1 for k in d}\nprint(out)\nprint(len(out))\n", "{'a': 1, 2: 1}\n2\n"},
	} {
		out, err := InterpreterRun(tc.src)
		if err != nil {
			t.Fatalf("%s: interpreter: %v", tc.name, err)
		}
		if out != tc.want {
			t.Errorf("%s: interpreter printed %q, want CPython's %q\nsrc: %s", tc.name, out, tc.want, tc.src)
		}
		res, cerr := Compile(tc.src)
		if cerr != nil {
			t.Fatalf("%s: compiled backend refused: %v", tc.name, cerr)
		}
		if got := runIR(t, res.IR); got != tc.want {
			t.Errorf("%s: compiled ran %q, want CPython's %q\nsrc: %s", tc.name, got, tc.want, tc.src)
		}
	}
}

// TestDictComprehensionEntriesCarryTheirOwnKeysAndValues is the library-path half of Gap R.77 and
// Gap R.78: a dict walked at the stride its entries really have, and an entry written with the tag its
// key really carries (an interned index is text, not the integer it happens to equal).
func TestDictComprehensionEntriesCarryTheirOwnKeysAndValues(t *testing.T) {
	for _, tc := range []struct{ name, src, want string }{
		{"text_key_is_findable_by_its_text", "d = {}\nd[\"a\"] = 1\nout = {k: 1 for k in d}\nprint(len(out))\nprint(out[\"a\"])\n", "1\n1\n"},
		{"text_key_prints_as_text", "d = {}\nd[\"a\"] = 1\nout = {k: 1 for k in d}\nprint(out)\n", "{'a': 1}\n"},
		{"text_key_as_its_own_value", "d = {}\nd[\"a\"] = 1\nout = {k: k for k in d}\nprint(out)\n", "{'a': 'a'}\n"},
		{"int_keys_are_both_keys", "d = {}\nd[1] = \"x\"\nd[2] = \"y\"\nout = [k for k in d]\nprint(out)\nprint(len(out))\n", "[1, 2]\n2\n"},
		{"int_keys_into_a_dict", "d = {}\nd[1] = \"x\"\nd[2] = \"y\"\nout = {k: 1 for k in d}\nprint(out)\nprint(out[2])\n", "{1: 1, 2: 1}\n1\n"},
		{"text_dict_keys_into_a_list", "d = {}\nd[\"a\"] = 1\nd[\"b\"] = 2\nout = [k for k in d]\nprint(out)\n", "['a', 'b']\n"},
		// A set of strings has no agreed iteration order (CPython's moves with the hash seed),
		// so this row asks length and a lookup rather than the printed dict.
		{"text_set_into_a_dict", "sa = {\"a\", \"b\"}\nout = {x: 1 for x in sa}\nprint(len(out))\nprint(out[\"b\"])\n", "2\n1\n"},
		{"mixed_dict_keys_into_a_list", "d = {}\nd[\"a\"] = 1\nd[2] = \"b\"\nout = [k for k in d]\nprint(out)\n", "['a', 2]\n"},
	} {
		out, err := InterpreterRun(tc.src)
		if err != nil {
			t.Fatalf("%s: interpreter: %v", tc.name, err)
		}
		if out != tc.want {
			t.Errorf("%s: interpreter printed %q, want CPython's %q\nsrc: %s", tc.name, out, tc.want, tc.src)
		}
		res, cerr := Compile(tc.src)
		if cerr != nil {
			t.Fatalf("%s: compiled backend refused: %v", tc.name, cerr)
		}
		if got := runIR(t, res.IR); got != tc.want {
			t.Errorf("%s: compiled ran %q, want CPython's %q\nsrc: %s", tc.name, got, tc.want, tc.src)
		}
	}
}
