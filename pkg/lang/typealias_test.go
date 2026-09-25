package lang

import "testing"

// TestTypeAliasParsesAndResolvesStructurally verifies L5.7: `type NAME = T`
// parses into a TypeAliasStmt and later annotations referencing NAME resolve
// structurally (a copy of T), not nominally.
func TestTypeAliasParsesAndResolvesStructurally(t *testing.T) {
	src := "type Vec = list[int]\ntype Pair = tuple[int, str]\ndef sumv(v: Vec):\n    return v\n"
	prog, err := Parse(src)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	// The two aliases must be present as TypeAliasStmt nodes.
	aliases := map[string]*Type{}
	for _, st := range prog.Stmts {
		if ta, ok := st.(*TypeAliasStmt); ok {
			aliases[ta.Name] = ta.Annot
		}
	}
	if aliases["Vec"] == nil || aliases["Vec"].Kind != KindList {
		t.Fatalf("Vec alias not parsed as list: %+v", aliases["Vec"])
	}
	if aliases["Pair"] == nil || aliases["Pair"].Kind != KindTuple {
		t.Fatalf("Pair alias not parsed as tuple: %+v", aliases["Pair"])
	}

	// sumv's param annotation must have been resolved structurally to list[int].
	param := aliases["Vec"]
	_ = param
	for _, st := range prog.Stmts {
		fd, ok := st.(*FuncDef)
		if !ok || fd.Name != "sumv" {
			continue
		}
		if len(fd.Params) != 1 || fd.Params[0].Annot == nil {
			t.Fatalf("sumv param annotation missing")
		}
		a := fd.Params[0].Annot
		if a.Kind != KindList || a.Elem == nil || a.Elem.Kind != KindInt {
			t.Errorf("sumv param annot = %+v, want structural list[int]", a)
		}
	}
}

// TestTypeAliasInterpNoOp verifies the interpreter treats TypeAliasStmt as a
// compile-time no-op and that a function using an alias runs without error.
func TestTypeAliasInterpNoOp(t *testing.T) {
	src := "type Count = int\ndef twice(c: Count):\n    return c * 2\nprint(twice(21))\n"
	if _, diags, err := EvalExpr(src); err != nil {
		t.Fatalf("eval: %v (diags=%v)", err, diags)
	}
}

// TestTypeAliasFormatRoundTrip verifies the canonical formatter preserves the
// alias declaration and expands references structurally.
func TestTypeAliasFormatRoundTrip(t *testing.T) {
	src := "type Vec = list[int]\n\ndef sumv(v: Vec):\n    return v\n"
	prog, err := Parse(src)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	got := Format(prog)
	if got != "type Vec = list[int]\ndef sumv(v: list[int]):\n  return v" {
		t.Errorf("formatted output mismatch:\ngot=%q", got)
	}
}
