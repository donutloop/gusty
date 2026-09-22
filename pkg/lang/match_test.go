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

func TestMatchOrPatterns(t *testing.T) {
	src := "x = 3\nmatch x:\n    case 1 | 2:\n        111\n    case 3 | 4:\n        333\n    case _:\n        999"
	if got := evalStr(t, src); got != 333 {
		t.Fatalf("or-patterns got %d, want 333", got)
	}
}

func TestMatchGuardFallsThrough(t *testing.T) {
	src := "x = 5\nmatch x:\n    case 5 if x > 10:\n        100\n    case 5:\n        555\n    case _:\n        999"
	if got := evalStr(t, src); got != 555 {
		t.Fatalf("guard got %d, want 555", got)
	}
}

func TestMatchGuardPasses(t *testing.T) {
	src := "x = 20\nmatch x:\n    case 20 if x > 10:\n        200\n    case _:\n        999"
	if got := evalStr(t, src); got != 200 {
		t.Fatalf("guard got %d, want 200", got)
	}
}

func TestMatchDictPattern(t *testing.T) {
	src := "d = {\"a\": 10}\nmatch d:\n    case {\"a\": v}:\n        v\n    case _:\n        0"
	if got := evalStr(t, src); got != 10 {
		t.Fatalf("dict pattern got %d, want 10", got)
	}
}

func TestMatchDictPatternMissingKey(t *testing.T) {
	src := "d = {\"b\": 5}\nmatch d:\n    case {\"a\": v}:\n        v\n    case _:\n        0"
	if got := evalStr(t, src); got != 0 {
		t.Fatalf("dict pattern missing key got %d, want 0", got)
	}
}
