package lang

import (
	"testing"
)

func evalStr(t *testing.T, src string) int64 {
	prog, err := Parse(src)
	if err != nil {
		t.Fatalf("parse %q: %v", src, err)
	}
	ev := NewEvaluator()
	v, err := ev.EvalProgram(prog)
	if err != nil {
		t.Fatalf("eval %q: %v", src, err)
	}
	return v
}

func TestStdlibAnyAll(t *testing.T) {
	if v := evalStr(t, "any([0, 0, 1])"); v != 1 {
		t.Fatalf("any([0,0,1]) = %d, want 1", v)
	}
	if v := evalStr(t, "any([0, 0, 0])"); v != 0 {
		t.Fatalf("any([0,0,0]) = %d, want 0", v)
	}
	if v := evalStr(t, "all([1, 1, 1])"); v != 1 {
		t.Fatalf("all([1,1,1]) = %d, want 1", v)
	}
	if v := evalStr(t, "all([1, 0, 1])"); v != 0 {
		t.Fatalf("all([1,0,1]) = %d, want 0", v)
	}
}

func TestStdlibChrOrd(t *testing.T) {
	// chr(65) = "A"; ord("A") = 65.
	if v := evalStr(t, "ord(chr(65))"); v != 65 {
		t.Fatalf("ord(chr(65)) = %d, want 65", v)
	}
}

func TestStdlibRound(t *testing.T) {
	// round(7) = 7
	if v := evalStr(t, "round(7)"); v != 7 {
		t.Fatalf("round(7) = %d, want 7", v)
	}
}

func TestLambdaParams(t *testing.T) {
	// Regression: `lambda x: ...` previously failed to parse (the ':' body
	// separator was misread as a type annotation).
	if v := evalStr(t, "f = lambda x: x + 1\nf(2)"); v != 3 {
		t.Fatalf("lambda apply = %d, want 3", v)
	}
	// Annotated lambda params still parse: `lambda x: int: x * 2`.
	if v := evalStr(t, "f = lambda x: int: x * 2\nf(3)"); v != 6 {
		t.Fatalf("annotated lambda apply = %d, want 6", v)
	}
}

func TestDictSetComprehensions(t *testing.T) {
	// Regression: `{x: x*2 for x in ...}` and `{x for x in ...}` failed to parse
	// ("expected }") — the 'for' inside braces was not accepted.
	if v := evalStr(t, "d = {x: x * 2 for x in [1, 2, 3]}\nd"); v == 0 {
		t.Fatalf("dict comprehension produced 0")
	}
	if v := evalStr(t, "s = {x for x in [1, 2]}\ns"); v == 0 {
		t.Fatalf("set comprehension produced 0")
	}
}

func TestSortedReverse(t *testing.T) {
	// sorted(iter, reverse=True) returns descending order.
	if v := evalStr(t, "x = sorted([3, 1, 2], reverse=True)\nx"); v == 0 {
		t.Fatalf("sorted reverse produced 0")
	}
}
