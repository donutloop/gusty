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

func TestGenDictComprehensionIndex(t *testing.T) {
	// d[1] over {x: x * 10} for x in [1, 2] maps key 1 -> value 10.
	if v, _, err := EvalExpr("({x: x * 10} for x in [1, 2])[1]"); err != nil {
		t.Fatalf("dict comprehension index: %v", err)
	} else if v != 10 {
		t.Fatalf("expected 10, got %d", v)
	}
	// missing key errors like a normal dict lookup.
	if _, _, err := EvalExpr("({x: x * 10} for x in [1, 2])[3]"); err == nil {
		t.Fatalf("expected key-not-found error")
	}
}

func TestGenSetComprehensionIndex(t *testing.T) {
	// s[2] over {x * x} for x in [1, 2] returns the element when present.
	if v, _, err := EvalExpr("({x * x} for x in [1, 2])[1]"); err != nil {
		t.Fatalf("set comprehension index: %v", err)
	} else if v != 1 {
		t.Fatalf("expected 1, got %d", v)
	}
	// absent element errors.
	if _, _, err := EvalExpr("({x * x} for x in [1, 2])[5]"); err == nil {
		t.Fatalf("expected not-in-set error")
	}
}

func TestGenMinSetLiteral(t *testing.T) {
	// min over a set literal folds its elements.
	if v, _, err := EvalExpr("min({1, 2, 3})"); err != nil {
		t.Fatalf("min set: %v", err)
	} else if v != 1 {
		t.Fatalf("expected 1, got %d", v)
	}
}

func TestGenMaxSetLiteral(t *testing.T) {
	// max over a set literal folds its elements.
	if v, _, err := EvalExpr("max({1, 2, 3})"); err != nil {
		t.Fatalf("max set: %v", err)
	} else if v != 3 {
		t.Fatalf("expected 3, got %d", v)
	}
}

func TestGenLambdaInline(t *testing.T) {
	// `(lambda x: int: x + 1)(5)` -> 6
	if v, _, err := EvalExpr("(lambda x: int: x + 1)(5)"); err != nil {
		t.Fatalf("inline lambda call: %v", err)
	} else if v != 6 {
		t.Fatalf("expected 6, got %d", v)
	}
	// multi-arg lambda: `(lambda x: int, y: int: x * y)(3, 4)` -> 12
	if v, _, err := EvalExpr("(lambda x: int, y: int: x * y)(3, 4)"); err != nil {
		t.Fatalf("multi-arg lambda call: %v", err)
	} else if v != 12 {
		t.Fatalf("expected 12, got %d", v)
	}
}

func TestGenLambdaNamed(t *testing.T) {
	// `f = lambda x: int: x * 2\nf(3)` -> 6
	if v, _, err := EvalExpr("f = lambda x: int: x * 2\nf(3)"); err != nil {
		t.Fatalf("named lambda: %v", err)
	} else if v != 6 {
		t.Fatalf("expected 6, got %d", v)
	}
}

func TestGenStrMethod(t *testing.T) {
	evalStr := func(src string) string {
		prog, err := Parse(src)
		if err != nil {
			t.Fatalf("parse %s: %v", src, err)
		}
		e := NewEvaluator()
		v, err := e.EvalProgram(prog)
		if err != nil {
			t.Fatalf("eval %s: %v", src, err)
		}
		return e.Repr(v)
	}
	if got := evalStr(`"AbC".upper()`); got != "ABC" {
		t.Fatalf("expected ABC, got %q", got)
	}
	if got := evalStr(`" AbC ".strip()`); got != "AbC" {
		t.Fatalf("expected AbC, got %q", got)
	}
}
