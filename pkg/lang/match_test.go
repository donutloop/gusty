package lang

import "testing"

func TestMatchListDestructure(t *testing.T) {
	// match [10, 20]: case [a, b]: a + b  -> 30
	src := "x = [10, 20]\nmatch x:\n    case [a, b]:\n        a + b\n"
	v := evalStr(t, src)
	if v != 30 {
		t.Fatalf("destructure sum = %d, want 30", v)
	}
}

func TestMatchListDestructureMismatch(t *testing.T) {
	// subject not a list of matching arity -> falls through to wildcard
	src := "x = [1, 2, 3]\nmatch x:\n    case [a, b]:\n        a + b\n    case _:\n        99\n"
	v := evalStr(t, src)
	if v != 99 {
		t.Fatalf("mismatch fell to wildcard = %d, want 99", v)
	}
}
