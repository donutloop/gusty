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
		{`True`, 0, false, "bools are not values in either backend yet"},
		{`1.5`, 0, false, "the mixed printer has no float rendering"},
		{`[1]`, 0, false, "a nested container would need the collector to mark it"},
		{`{"a": 1}`, 0, false, "a nested dict likewise"},
		{`{1, 2}`, 0, false, "a nested set likewise"},
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
		{`[1, True]`, false},    // bool gate
		{`[1.5, "a"]`, false},   // float gate
		{`[[1], "a"]`, false},   // container gate
		{`[None, None]`, false}, // all one kind is not a mix
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
	// Printing a mixed list is supported; every other element-wise use must refuse with an
	// actionable message rather than read a tagged slot through one static kind.
	for _, tc := range []struct {
		src    string
		wanted string
	}{
		{"xs = [1, \"a\"]\nprint(xs[0])\n", "tagged value at the use site"},
		{"xs = [1, \"a\"]\nxs.append(5)\nprint(xs)\n", "adding to it needs the new element tagged"},
		// A loop over a mixed list binds (value, tag); arithmetic has no tag to carry, so it
		// refuses with the same honesty rather than computing on a string table index (ADR 0185).
		{"xs = [1, \"a\"]\nfor x in xs:\n    print(x + 1)\n", "using it as a number needs a tagged value"},
		{"xs = [1, \"a\"]\nfor x in xs:\n    print(x > 2)\n", "using it as a number needs a tagged value"},
		// Mixing the still-unsupported kinds keeps the original, pre-tag refusal.
		{"xs = [True, \"a\"]\nprint(xs)\n", "either strings or numbers"},
		{"xs = [1.5, \"a\"]\nprint(xs)\n", "either strings or numbers"},
		{"xs = [[1], \"a\"]\nprint(xs)\n", "either strings or numbers"},
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
