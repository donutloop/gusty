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

func TestEvalMatchWildcard(t *testing.T) {
	// match with _ wildcard catches unmatched value
	v, _, err := EvalExpr("x = 42\nmatch x:\n    case 1:\n        print(1)\n    case _:\n        print(42)\nx")
	if err != nil {
		t.Fatalf("match wildcard err: %v", err)
	}
	if v != 42 {
		t.Fatalf("got %d, want 42", v)
	}
}

func TestEvalForElse(t *testing.T) {
	// for without break: else runs
	v, _, err := EvalExpr("s = 0\nfor i in range(3):\n    s = s + i\nelse:\n    s = s + 100\ns")
	if err != nil {
		t.Fatalf("for-else err: %v", err)
	}
	if v != 103 { // 0+1+2 + 100
		t.Fatalf("got %d, want 103", v)
	}
}

func TestEvalForElseBreak(t *testing.T) {
	// break skips else
	v, _, err := EvalExpr("s = 0\nfor i in range(3):\n    if i == 1:\n        break\n    s = s + i\nelse:\n    s = s + 100\ns")
	if err != nil {
		t.Fatalf("for-else break err: %v", err)
	}
	if v != 0 { // else skipped; only i=0 added before break
		t.Fatalf("got %d, want 0", v)
	}
}

func TestEvalWhileElse(t *testing.T) {
	v, _, err := EvalExpr("i = 0\ns = 0\nwhile i < 3:\n    s = s + i\n    i = i + 1\nelse:\n    s = s + 10\ns")
	if err != nil {
		t.Fatalf("while-else err: %v", err)
	}
	if v != 13 { // 0+1+2 + 10
		t.Fatalf("got %d, want 13", v)
	}
}

func TestEvalClassMethod(t *testing.T) {
	src := "class Point:\n    def __init__(self, x, y):\n        self.x = x\n        self.y = y\n    def sum(self):\n        return self.x + self.y\np = Point(2, 3)\np.sum()"
	v, _, err := EvalExpr(src)
	if err != nil {
		t.Fatalf("class err: %v", err)
	}
	if v != 5 {
		t.Fatalf("got %d, want 5", v)
	}
}

func TestEvalClassAttrSet(t *testing.T) {
	src := "class C:\n    def __init__(self):\n        self.n = 0\n    def bump(self):\n        self.n = self.n + 1\n        return self.n\nc = C()\nc.bump()\nc.bump()"
	v, _, err := EvalExpr(src)
	if err != nil {
		t.Fatalf("class err: %v", err)
	}
	if v != 2 {
		t.Fatalf("got %d, want 2", v)
	}
}

func TestEvalTryExcept(t *testing.T) {
	src := "x = 0\ntry:\n    x = 1 // 0\nexcept Exception:\n    x = 42\nx"
	v, _, err := EvalExpr(src)
	if err != nil {
		t.Fatalf("try err: %v", err)
	}
	if v != 42 {
		t.Fatalf("got %d, want 42", v)
	}
}


func TestEvalRaiseCaught(t *testing.T) {
	src := "x = 0\ntry:\n    raise Exception\n    x = 1\nexcept Exception:\n    x = 42\nx"
	v, _, err := EvalExpr(src)
	if err != nil {
		t.Fatalf("raise-catch err: %v", err)
	}
	if v != 42 {
		t.Fatalf("got %d, want 42", v)
	}
}
