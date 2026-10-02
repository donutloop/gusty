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
// The compiled leg runs only the rows whose element is not itself a container. A container element
// reaches `rt_append_tagged(i32 %h1, i32 @.set1, i32 7)` — the *global* in a value position, which
// `llc` rejects — and that is a separate defect with its own row (Gap R.75) and its own fix; the
// rows that still carry it are in integration/comprehension_brace_element_test.go, where the
// exit-code contract is asserted rather than hidden.
func TestBraceElementComprehensionRunsLikeCPython(t *testing.T) {
	for _, tc := range []struct {
		name string
		src  string
		want string
		// aotContainerElement marks the rows whose element is a container literal: the compiled
		// leg is owed to Gap R.75, so only the interpreter is measured against CPython here.
		aotContainerElement bool
	}{
		{"list comprehension over set literals", "d = [{1, 2} for x in [1, 2]]\nprint(len(d))\n", "2\n", true},
		{"its element is a two-member set", "d = [{1, 2} for x in [1]]\nprint(len(d[0]))\n", "2\n", true},
		{"membership in the element", "d = [{1, 2} for x in [1]]\nprint(1 if 2 in d[0] else 0)\n", "1\n", true},
		{"one set per iteration", "d = [{x} for x in [1, 2]]\nprint(1 if d[0] == d[1] else 0)\n", "0\n", true},
		{"list comprehension over dict literals", "d = [{\"k\": x} for x in [1, 2]]\nprint(len(d))\n", "2\n", true},
		{"each dict holds its own entry", "d = [{\"k\": x} for x in [1, 2]]\nprint(d[1][\"k\"])\n", "2\n", true},
		{"first dict is not the second", "d = [{\"k\": x} for x in [1, 2]]\nprint(len(d[0]))\n", "1\n", true},
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
