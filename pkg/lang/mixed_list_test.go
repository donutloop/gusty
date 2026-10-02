package lang

import (
	"regexp"
	"strings"
	"testing"
)

// A compiled container used to record one element kind for the whole object (@estr[h]), which
// is why a heterogeneous xs = [1, "a"] was refused outright rather than printed (ADR 0175).
// @heap_tags gives every slot its own canonical ValueTag, so a list can hold numbers, interned
// strings and None together (roadmap L11.1, ADR 0184) -- and everything that would read an
// element out through one static kind still refuses instead of printing something wrong.

func TestElemKindTagDecidesWhatAMixedListMayHold(t *testing.T) {
	g := &irGen{} // nil maps read as empty; this decision needs no module state
	for _, tc := range []struct {
		src string
		tag int32
		ok  bool
		why string
	}{
		{`1`, int32(TagInt), true, "an integer"},
		{`"a"`, int32(TagStr), true, "a string literal"},
		{`None`, int32(TagNone), true, "the None singleton"},
		// A bool is tagged TagInt, because that is what both backends store today: the
		// interpreter keeps bool as Int(1) and renders it through the number path, which is
		// the difference probe_bool_value pins. When L11.2 gives bool its own kind this case
		// flips to TagBool and every container follows (ADR 0232).
		{`True`, int32(TagInt), true, "a bool, stored as the number it behaves like"},
		{`1.5`, int32(TagFloat), true, "a float's slot is the handle of a float box, which the mixed printer renders and rt_payload_eq compares by value (ADR 0233)"},
		// A container element is stored as the inner object's handle; the tag is what routes the
		// print to the container printer and the comparison to rt_container_eq instead of an
		// integer compare (roadmap L11.1, ADR 0189).
		{`[1]`, int32(TagList), true, "a nested list: the slot holds the inner object's handle"},
		{`{"a": 1}`, int32(TagDict), true, "a nested dict likewise"},
		{`{1, 2}`, int32(TagSet), true, "a nested set likewise"},
	} {
		prog, err := parseProgram(tc.src)
		if err != nil {
			t.Fatalf("parse %q: %v", tc.src, err)
		}
		expr, ok := prog.Stmts[0].(*ExprStmt)
		if !ok {
			t.Fatalf("%q is not an expression statement", tc.src)
		}
		tag, ok := g.elemKindTag(expr.Expr)
		if ok != tc.ok {
			t.Errorf("elemKindTag(%s) accepted=%v, want %v (%s)", tc.src, ok, tc.ok, tc.why)
			continue
		}
		if ok && tag != tc.tag {
			t.Errorf("elemKindTag(%s) = %d, want %d", tc.src, tag, tc.tag)
		}
	}
}

func TestTaggableMixedListRequiresActualMixing(t *testing.T) {
	g := &irGen{}
	for _, tc := range []struct {
		src  string
		want bool
	}{
		{`[1, 2, 3]`, false},
		{`["a", "b"]`, false},
		{`[1, "a"]`, true},
		{`[1, "a", None]`, true},
		{`[]`, false},
		{`[1, True]`, false},   // a bool stored as a number is not a second kind
		{`[True, "a"]`, true},  // number and string still mix
		{`[1.5, "a"]`, true},   // a float slot can only be read through its tag (ADR 0233)
		{`[1.5]`, true},        // a literal of nothing but floats still has no untagged representation
		{`[[1], "a"]`, true},   // a container slot is a handle: only its tag makes it readable
		{`[None, None]`, true}, // a None slot holds nothing; the tag is the whole answer (ADR 0233)
	} {
		prog, err := parseProgram(tc.src)
		if err != nil {
			t.Fatalf("parse %q: %v", tc.src, err)
		}
		stmt, ok := prog.Stmts[0].(*ExprStmt)
		if !ok {
			t.Fatalf("%q is not an expression statement", tc.src)
		}
		lit, ok := stmt.Expr.(*ListLit)
		if !ok {
			t.Fatalf("%q is not a list literal", tc.src)
		}
		if got := g.taggableMixedList(lit); got != tc.want {
			t.Errorf("taggableMixedList(%s) = %v, want %v", tc.src, got, tc.want)
		}
	}
}

func TestTaggedListEmitsTagsAndPrintsThroughTheTaggedPrinter(t *testing.T) {
	res, err := Compile("xs = [1, \"a\", None]\nprint(xs)\n")
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}
	// The tag array exists, three slots got tagged, and printing went to the mixed printer.
	for _, want := range []string{"@heap_tags", "call void @rt_tag_elem(", "call void @rt_print_list_mixed("} {
		if !strings.Contains(res.IR, want) {
			t.Errorf("module is missing %q", want)
		}
	}
	// Each element carries the canonical tag: int=0, str=4, None=3.
	// The handle temp's name is codegen's business; the slot index and tag value are ours.
	for _, want := range []string{", i32 0, i32 0)", ", i32 1, i32 4)", ", i32 2, i32 3)"} {
		if !strings.Contains(res.IR, want) {
			t.Errorf("module is missing the tag write %q\ngot: %s", want, irLinesContaining(res.IR, "rt_tag_elem"))
		}
	}
	for _, line := range strings.Split(res.IR, "\n") {
		if strings.Contains(line, "store i32 @") {
			t.Fatalf("module stores a global in value position: %s", strings.TrimSpace(line))
		}
	}
}

func TestMixedListElementUsesStillRefuse(t *testing.T) {
	// Printing an element, binding one, appending one and writing one all carry the tag now
	// (see TestElementReadCarriesItsTag / TestElementWriteAndAppendTagTheSlot). What is left
	// here is the set of uses that genuinely have no tag to carry: a context that demands one
	// static kind, or an element the tag cannot describe at all.
	for _, tc := range []struct {
		src    string
		wanted string
	}{
		// A tagged element reaching a context that needs a plain i32 refuses rather than
		// computing on what is, for a string element, an index into the interned table. A number
		// the compiler can see in a slot is answered instead (ADR 0243); what is left here is the
		// element the read cannot resolve to a literal — text in the slot, a loop variable, or a
		// value handed to a function.
		{"xs = [1, \"a\"]\nprint(xs[1] + 1)\n", "needs a single static kind"},
		// `xs[1] > 2` used to be on this table. It is not a compile-time refusal any more: a slot read
		// whose kind the object carries goes to the float arms with its tag, and text against a number
		// raises CPython's TypeError at run time — which is what CPython does, so the row now lives in
		// the trap table of tagged_numeric_test.go (roadmap L11.1, Gap R.88).
		{"def head(v):\n    print(v)\n    return 1\n\nxs = [1, \"a\", None]\nhead(xs[1])\n", "needs a single static kind"},
		{"xs = [1, \"a\"]\nfor x in xs:\n    print(x + 1)\n", "using it as a number needs a tagged value"},
		{"xs = [1, \"a\"]\nfor x in xs:\n    print(x > 2)\n", "using it as a number needs a tagged value"},
		// An element the tag table has no entry for at all. A bool is not in this set: it is
		// tagged TagInt, which is what both backends store today (the rendering difference is the
		// pinned probe_bool_value debt, L11.2), so appending one is answered, not refused.
		{"xs = [1, \"a\"]\nxs.append(lambda x: x)\nprint(xs)\n", "must carry a tag"},

		// The nested container answers when it is *read back* through its own printer, its length, its
		// membership test or a further subscript (see TestContainerSlotReadsAreAnswered); asking it to
		// be a number is still the tagged-value gap, and the answer is a refusal, not 0.
		{"xs = [[1, 2], [3]]\nprint(xs[0] + 1)\n", "needs a single static kind"},
		{"xs = [[1, 2], [3]]\nprint(xs[0] * 2)\n", "needs a single static kind"},
		// A fold over slots asks for the elements as numbers before the tag can be consulted; that is
		// the same hole the runtime-computed folds fell into (Gap R.71), and it stays a refusal.
		{"xs = [[1, 2], [3]]\nprint(max(xs[0]))\n", "max requires an inline list/set/dict literal"},
	} {
		_, err := Compile(tc.src)
		if err == nil {
			t.Fatalf("%q compiled; want a refusal", tc.src)
		}
		if !strings.Contains(err.Error(), tc.wanted) {
			t.Errorf("%q refused with %q, want it to mention %q", tc.src, err.Error(), tc.wanted)
		}
		if strings.Contains(err.Error(), "LLVM ERROR") || strings.Contains(err.Error(), "verifier") {
			t.Errorf("%q failed as an IR problem instead of a front-end refusal: %v", tc.src, err)
		}
	}
}

// The shapes this list used to refuse because a float had no representation in a slot —
// `xs.append(1.5)`, `xs[0] = 2.5`, `xs = [1.5, "a"]` — are the ones ADR 0233 paid for: the float
// goes into a box, the slot keeps the handle and the TagFloat tag, and the answer is CPython's.
func TestFloatElementsInMixedListsNowAnswer(t *testing.T) {
	for _, tc := range []struct{ src, want string }{
		{"xs = [1, \"a\"]\nxs.append(1.5)\nprint(xs)\n", "[1, 'a', 1.5]\n"},
		{"xs = [1, \"a\"]\nxs[0] = 2.5\nprint(xs)\n", "[2.5, 'a']\n"},
		{"xs = [1.5, \"a\"]\nprint(xs)\n", "[1.5, 'a']\n"},
		{"xs = [1.5, \"a\"]\nprint(1.5 in xs)\nprint(len(xs))\n", "1\n2\n"},
		{"xs = [1, \"a\"]\nprint(1.0 in xs)\n", "1\n"}, // Python: [1] contains 1.0
		{"xs = [1.5, 2]\nprint(1 if xs == [1.5, 2] else 0)\n", "1\n"},
		{"xs = [1.5, \"a\", None]\nprint(xs)\n", "[1.5, 'a', None]\n"},
		{"xs = [1.5]\nfor v in xs:\n    print(v)\n", "1.5\n"},
		{"xs = [None, 1.5, \"a\"]\nprint(xs)\n", "[None, 1.5, 'a']\n"},
	} {
		res, err := Compile(tc.src)
		if err != nil {
			t.Fatalf("%q: Compile: %v", tc.src, err)
		}
		out := runIR(t, res.IR)
		if out != tc.want {
			t.Errorf("%q ran to %q, want CPython's %q", tc.src, out, tc.want)
		}
	}
}

// An element read produces (value, tag) at the read site, so print(xs[i]) dispatches on the
// tag and `v = xs[i]` binds a tagged variable — the same pair a loop variable over a mixed
// list already carried (ADR 0185), now available at an arbitrary read site (ADR 0187).
func TestElementReadCarriesItsTag(t *testing.T) {
	res, err := Compile("xs = [1, \"a\", None]\nprint(xs[1])\nv = xs[2]\nprint(v)\n")
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}
	for _, want := range []string{
		"call i32 @rt_get_elem(", "call i32 @rt_tag_of(",
		"%_v_tag = alloca i32",
		"call void @rt_print_mixed_value(i32 %t",
	} {
		if !strings.Contains(res.IR, want) {
			t.Errorf("module is missing %q\ntag loads: %s\ntag allocas: %s", want,
				strings.Join(irLinesContaining(res.IR, "rt_tag_of"), "\n"),
				strings.Join(irLinesContaining(res.IR, "_tag = alloca"), "\n"))
		}
	}
	// Both prints are top-level, so both are str(): the quote flag must be 0. (The runtime's
	// own rt_print_list_mixed calls the same helper with quote 1 — that is the repr() side,
	// and the count here is what pins *this* program's two calls to the str() side.)
	called := 0
	for _, line := range strings.Split(res.IR, "\n") {
		if !strings.Contains(line, "call void @rt_print_mixed_value(") {
			continue
		}
		if strings.HasSuffix(strings.TrimSpace(line), "i32 0)") {
			called++
		}
	}
	if called != 2 {
		t.Errorf("want two str()-flagged tag dispatches (print(xs[1]) and print(v)), got %d:\n%s", called,
			strings.Join(irLinesContaining(res.IR, "call void @rt_print_mixed_value"), "\n"))
	}
	// Top-level print is str(), not repr(): the quote flag must be 0 for both lines.
	if strings.Count(res.IR, "call void @rt_print_mixed_value(") < 2 {
		t.Errorf("both prints should go through the tag-dispatching printer:\n%s", irLinesContaining(res.IR, "rt_print_mixed_value"))
	}
	for _, line := range strings.Split(res.IR, "\n") {
		if strings.Contains(line, "store i32 @") || strings.Contains(line, "(i32 @.") {
			t.Fatalf("module puts a global in value position: %s", strings.TrimSpace(line))
		}
	}
}

// Writing or appending an element writes its tag with it. Before ADR 0187 `xs[0] = "z"` stored
// the interned index through the slot's stale int tag and printed [1, 'a', None] — an answer,
// from the interned table, that CPython does not give.
func TestElementWriteAndAppendTagTheSlot(t *testing.T) {
	res, err := Compile("xs = [1, \"a\", None]\nxs[0] = \"z\"\nxs.append(None)\nprint(xs)\n")
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}
	if !strings.Contains(res.IR, "call void @rt_append_tagged(") {
		t.Errorf("append should write payload and tag together:\n%s", irLinesContaining(res.IR, "rt_append"))
	}
	// Slot 0 becomes a string (TagStr = 4) and the appended slot carries None (TagNone = 3).
	if !strings.Contains(strings.Join(irLinesContaining(res.IR, "rt_tag_elem"), "\n"), ", i32 0, i32 4)") {
		t.Errorf("item write must retag slot 0 as a string:\n%s", strings.Join(irLinesContaining(res.IR, "rt_tag_elem"), "\n"))
	}
	if !strings.Contains(strings.Join(irLinesContaining(res.IR, "rt_append_tagged"), "\n"), ", i32 3)") {
		t.Errorf("append must carry the None tag:\n%s", irLinesContaining(res.IR, "rt_append_tagged"))
	}
}

// A tagged element that reaches a context with no tag to carry refuses, and the refusal names what
// does work — the message is the interface while the capability grows (ADR 0166). A number the
// compiler can see in a slot is answered (ADR 0243); text in a slot is not, and neither is a loop
// variable, whose tag is chosen per iteration.
func TestTaggedElementRefusalNamesWhatWorks(t *testing.T) {
	_, err := Compile("xs = [1, \"a\"]\nprint(xs[1] + 1)\n")
	if err == nil {
		t.Fatal("arithmetic on a text element must refuse")
	}
	for _, want := range []string{"print(xs[i])", "v = xs[i]", "(value, tag) pair"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("refusal %q should mention %q", err.Error(), want)
		}
	}
	_, err = Compile("xs = [1, \"a\"]\nfor x in xs:\n    print(x + 1)\n")
	if err == nil {
		t.Fatal("arithmetic on a loop variable over a mixed list must refuse")
	}
	if !strings.Contains(err.Error(), "tagged value") {
		t.Errorf("loop-variable refusal should name the tagged value word: %q", err.Error())
	}
}

// A loop over a mixed list is where the tag has to survive from the object into a local: the
// element and its tag are bound together, print dispatches on the tag, and using the variable
// as a number refuses (ADR 0185).
func TestLoopOverMixedListBindsValueAndTag(t *testing.T) {
	res, err := Compile("xs = [1, \"a\", None]\nfor x in xs:\n    print(x)\n")
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}
	for _, want := range []string{"%_x_tag = alloca i32", "call i32 @rt_tag_of(", "call void @rt_print_mixed_value("} {
		if !strings.Contains(res.IR, want) {
			t.Errorf("module is missing %q\ngot: %s", want, irLinesContaining(res.IR, "tag"))
		}
	}
}

// print(x) at top level is str(); print(xs) is repr(). One tag, two contexts, decided by the
// call site -- the distinction Python has and a single shared printer would not.
func TestTagPrinterQuotesOnlyInsideContainers(t *testing.T) {
	loop, err := Compile("xs = [1, \"a\"]\nfor x in xs:\n    print(x)\n")
	if err != nil {
		t.Fatalf("Compile loop: %v", err)
	}
	if !regexp.MustCompile(`rt_print_mixed_value\(i32 %t\d+, i32 %t\d+, i32 0\)`).MatchString(loop.IR) {
		t.Errorf("top-level print of a tagged value should pass quote=0 (str): %s", irLinesContaining(loop.IR, "rt_print_mixed_value"))
	}
}
