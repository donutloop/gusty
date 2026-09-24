package lang

import "testing"

// TestTrailingCommaCall verifies `f(a, b,)` parses and runs identically to
// `f(a, b)` (roadmap L5.3).
func TestTrailingCommaCall(t *testing.T) {
	src := "def f(a, b):\n    return a + b\nf(1, 2,)"
	v, _, err := EvalExpr(src)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if v != 3 {
		t.Fatalf("got %d, want 3", v)
	}
}

// TestTrailingCommaList verifies `[1, 2,]` parses and produces a 2-element list.
func TestTrailingCommaList(t *testing.T) {
	src := "a = [1, 2,]\nlen(a)"
	v, _, err := EvalExpr(src)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if v != 2 {
		t.Fatalf("got %d, want 2", v)
	}
}

// TestTrailingCommaDict verifies `{1: 2,}` parses and produces a 1-entry dict.
func TestTrailingCommaDict(t *testing.T) {
	src := "a = {1: 2,}\nlen(a)"
	v, _, err := EvalExpr(src)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if v != 1 {
		t.Fatalf("got %d, want 1", v)
	}
}

// TestTrailingCommaSet verifies `{1, 2,}` parses and produces a 2-element set.
func TestTrailingCommaSet(t *testing.T) {
	src := "a = {1, 2,}\nlen(a)"
	v, _, err := EvalExpr(src)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if v != 2 {
		t.Fatalf("got %d, want 2", v)
	}
}

// TestTrailingCommaTuple verifies `(1, 2,)` parses as a 2-tuple and `(1,)`
// as a 1-tuple (Python semantics).
func TestTrailingCommaTuple(t *testing.T) {
	src := "a = (1, 2,)\nlen(a)"
	v, _, err := EvalExpr(src)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if v != 2 {
		t.Fatalf("got %d, want 2", v)
	}
	src1 := "a = (1,)\nlen(a)"
	v1, _, err := EvalExpr(src1)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if v1 != 1 {
		t.Fatalf("got %d, want 1", v1)
	}
}

// TestTrailingCommaMatchArg verifies a match class-pattern arg list tolerates
// a trailing comma: `case Point(x, y,)`.
func TestTrailingCommaMatchArg(t *testing.T) {
	src := `class Point:
    def __init__(self, x, y):
        self.x = x
        self.y = y
def area(p):
    match p:
        case Point(x, y,):
            return x * y
        case _:
            return 0
area(Point(3, 4))`
	v, _, err := EvalExpr(src)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if v != 12 {
		t.Fatalf("got %d, want 12", v)
	}
}

// TestTrailingCommaFmtRoundTrip verifies the canonical formatter accepts
// trailing-comma source and its output re-parses cleanly (format round-trip).
func TestTrailingCommaFmtRoundTrip(t *testing.T) {
	src := "f(1, 2,)\n[1, 2,]\n{1: 2,}\n{1, 2,}\n(1, 2,)"
	f, err := FormatSrc(src)
	if err != nil {
		t.Fatalf("FormatSrc: %v", err)
	}
	// The formatter normalizes trailing commas away; its output must re-parse.
	if _, err := Parse(f); err != nil {
		t.Fatalf("re-parse formatted output %q: %v", f, err)
	}
}
