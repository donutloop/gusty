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
