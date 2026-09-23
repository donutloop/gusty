package lang

import "testing"

func TestRawStringLiteral(t *testing.T) {
	prog, err := Parse(`print(r"a\nb")`)
	if err != nil {
		t.Fatalf("parse raw string: %v", err)
	}
	if !containsStr(prog, "a\\nb") {
		t.Fatalf("raw string value not preserved")
	}
	// backslash before a quote keeps the string open, both chars in value
	prog2, err := Parse(`print(r"a\"b")`)
	if err != nil {
		t.Fatalf("parse raw string with embedded quote: %v", err)
	}
	if !containsStr(prog2, `a\"b`) {
		t.Fatalf("raw string embedded quote: value wrong")
	}
}

func TestTripleQuotedString(t *testing.T) {
	src := "print(\"\"\"line1\nline2\"\"\")\n"
	prog, err := Parse(src)
	if err != nil {
		t.Fatalf("parse triple string: %v", err)
	}
	if !containsStr(prog, "line1\nline2") {
		t.Fatalf("triple string value wrong")
	}
}

func TestTripleDocstring(t *testing.T) {
	src := "def f():\n    \"\"\"docs\"\"\"\n    return 1\n"
	prog, err := Parse(src)
	if err != nil {
		t.Fatalf("parse triple docstring: %v", err)
	}
	found := false
	for _, s := range prog.Stmts {
		if fd, ok := s.(*FuncDef); ok && fd.Doc == "docs" {
			found = true
		}
	}
	if !found {
		t.Fatalf("triple-quoted docstring not extracted")
	}
}

func TestRawTripleString(t *testing.T) {
	prog, err := Parse("print(r\"\"\"a\\nb\"\"\")\n")
	if err != nil {
		t.Fatalf("parse raw triple: %v", err)
	}
	if !containsStr(prog, "a\\nb") {
		t.Fatalf("raw triple value wrong")
	}
}

func containsStr(n Node, want string) bool {
	got := false
	walkContains(n, want, &got)
	return got
}

func walkContains(n Node, want string, got *bool) {
	switch x := n.(type) {
	case *Program:
		for _, s := range x.Stmts {
			walkContains(s, want, got)
		}
	case *ExprStmt:
		walkContains(x.Expr, want, got)
	case *Call:
		for _, a := range x.Args {
			walkContains(a, want, got)
		}
	case *StrLit:
		if x.Value == want {
			*got = true
		}
	}
}
