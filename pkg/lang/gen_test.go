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
