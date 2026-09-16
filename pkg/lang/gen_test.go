package lang

import "testing"

func TestGenSum(t *testing.T) {
	src := "def g():\n    yield 1\n    yield 2\n    yield 3\ns = 0\nfor x in g():\n    s = s + x\ns"
	v, _, err := EvalExpr(src)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if v != 6 {
		t.Fatalf("got %d, want 6", v)
	}
}

func TestListLit(t *testing.T) {
	src := "a = [1, 2, 3]\ns = 0\nfor x in a:\n    s = s + x\ns"
	v, _, err := EvalExpr(src)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if v != 6 {
		t.Fatalf("got %d, want 6", v)
	}
}

func TestLenList(t *testing.T) {
	src := "a = [10, 20, 30]\nlen(a)"
	v, _, err := EvalExpr(src)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if v != 3 {
		t.Fatalf("got %d, want 3", v)
	}
}

func TestGenSetComprehensionLen(t *testing.T) {
	// {x * x} for x in [1, 2, 2] dedups to {1, 4}.
	if v, _, err := EvalExpr("len({x * x} for x in [1, 2, 2])"); err != nil {
		t.Fatalf("set comprehension: %v", err)
	} else if v != 2 {
		t.Fatalf("expected len 2, got %d", v)
	}
}

func TestGenDictComprehensionLen(t *testing.T) {
	// {x: x * 10} for x in [1, 2] builds a 2-entry dict.
	if v, _, err := EvalExpr("len({x: x * 10} for x in [1, 2])"); err != nil {
		t.Fatalf("dict comprehension: %v", err)
	} else if v != 2 {
		t.Fatalf("expected len 2, got %d", v)
	}
}
