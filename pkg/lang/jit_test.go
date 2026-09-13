package lang

import "testing"

func TestEvalUserFunc(t *testing.T) {
	v, _, err := EvalExpr("def double(x):\n    return x * 2\ndouble(5)")
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if v != 10 {
		t.Fatalf("got %d, want 10", v)
	}
}

func TestEvalUserFuncTwoParams(t *testing.T) {
	v, _, err := EvalExpr("def add(a, b):\n    return a + b\nadd(3, 4)")
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if v != 7 {
		t.Fatalf("got %d, want 7", v)
	}
}

func TestEvalIfElse(t *testing.T) {
	v, _, err := EvalExpr("x = 1\nif x < 2:\n    print(10)\nelse:\n    print(20)\nx")
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if v != 1 {
		t.Fatalf("got %d, want 1", v)
	}
}

func TestEvalWhileSum(t *testing.T) {
	v, _, err := EvalExpr("i = 0\ns = 0\nwhile i < 3:\n    s = s + i\n    i = i + 1\ns")
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if v != 3 {
		t.Fatalf("got %d, want 3", v)
	}
}

func TestEvalForSum(t *testing.T) {
	v, _, err := EvalExpr("s = 0\nfor i in range(5):\n    s = s + i\ns")
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if v != 10 {
		t.Fatalf("got %d, want 10", v)
	}
}
