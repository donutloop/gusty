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

func TestEvalMatch(t *testing.T) {
	v, _, err := EvalExpr("x = 2\nmatch x:\n    case 1:\n        print(1)\n    case 2:\n        print(2)\nx")
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if v != 2 {
		t.Fatalf("got %d, want 2", v)
	}
}

func TestEvalBreakContinue(t *testing.T) {
	// break out of while
	v, _, err := EvalExpr("i = 0\nwhile i < 100:\n    i = i + 1\n    if i == 3:\n        break\ni")
	if err != nil {
		t.Fatalf("break err: %v", err)
	}
	if v != 3 {
		t.Fatalf("break got %d, want 3", v)
	}
	// continue skips increment in for loop
	v2, _, err := EvalExpr("s = 0\nfor i in range(5):\n    if i == 2:\n        continue\n    s = s + i\ns")
	if err != nil {
		t.Fatalf("continue err: %v", err)
	}
	if v2 != 8 { // 0+1+3+4 (skip 2)
		t.Fatalf("continue got %d, want 8", v2)
	}
}

func TestEvalRangeTwoArg(t *testing.T) {
	// sum range(2, 5) = 2+3+4 = 9
	v, _, err := EvalExpr("s = 0\nfor i in range(2, 5):\n    s = s + i\ns")
	if err != nil {
		t.Fatalf("range(a,b) err: %v", err)
	}
	if v != 9 {
		t.Fatalf("range(2,5) sum got %d, want 9", v)
	}
}
