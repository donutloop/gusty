package lang

import (
	"strings"
	"testing"
)

// CPython rejects `def f(a, b=1, c)` outright — `SyntaxError: parameter without a default follows
// parameter with a default` — because in Python that parameter really can never be filled. It cannot
// be filled there because Python's positional binding stops at the first default; this language has no
// such rule, so `f(1, 2, 3)` and `f(1, b=2, c=3)` both fill every parameter and both run.
//
// The gap (roadmap R.11, ADR 0206) was not the missing refusal — a refusal here would forbid a shape
// this language calls correctly — it was that nothing said which rule applies, so the shape read like a
// bug, tested like one, and no document claimed it.

func paramOrderDiags(t *testing.T, src string) []Diagnostic {
	t.Helper()
	prog, err := Parse(src)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	return Analyze(prog)
}

// TestDefaultInAnyPositionIsAccepted is the rule stated positively: a default marks a parameter that
// may be omitted, and nothing about the parameters around it.
func TestDefaultInAnyPositionIsAccepted(t *testing.T) {
	for _, tc := range []struct{ name, src string }{
		{"default first", "def f(a=1, b=2):\n    return a + b\n\nz = f(3, 4)\n"},
		{"default in the middle", "def f(a, b=1, c):\n    return a + b + c\n\nz = f(1, 2, 3)\n"},
		{"defaults around a plain one", "def f(a=1, b=2, c=3, d=4):\n    return a\n\nz = f()\n"},
		{"default then plain then default", "def f(a, b=1, c, d=2):\n    return a + b + c + d\n\nz = f(1, 2, 3, 4)\n"},
		{"a method with a default in the middle", "class C:\n    def m(self, a, b=1, c):\n        return a + b + c\n\nz = C().m(1, 2, 3)\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			for _, d := range paramOrderDiags(t, tc.src) {
				if d.Level == LevelError {
					t.Errorf("%s: a signature with a default in an unusual position was refused: %v", tc.name, d)
				}
			}
		})
	}
}

// TestBothBindingFormsFillEveryParameter is the positive evidence that no refusal is owed here: the
// call that would be illegal to *define* in Python is checked, here, exactly like any other call — and
// a call that really does leave a parameter unfilled is still caught (Gap R.10).
func TestBothBindingFormsFillEveryParameter(t *testing.T) {
	diags := paramOrderDiags(t, "def f(a, b=1, c):\n    return a + b + c\n\nz = f(1, b=2, c=3)\n")
	for _, d := range diags {
		if d.Level == LevelError {
			t.Errorf("the keyword form was refused: %v", d)
		}
	}

	short := paramOrderDiags(t, "def f(a, b=1, c):\n    return a + b + c\n\nz = f(1)\n")
	var found bool
	for _, d := range short {
		if d.Level == LevelError && strings.Contains(d.Msg, "expects 3 arguments, got 1") {
			found = true
		}
	}
	if !found {
		t.Errorf("a call that leaves a defaulted-middle signature short was not reported: %v", short)
	}

	skipped := paramOrderDiags(t, "def f(a, b=1, c):\n    return a + b + c\n\nz = f(a=1, b=2)\n")
	found = false
	for _, d := range skipped {
		if d.Level == LevelError && strings.Contains(d.Msg, `missing argument "c"`) {
			found = true
		}
	}
	if !found {
		t.Errorf("a keyword call that never names the last parameter was not reported: %v", skipped)
	}
}

// TestDefaultIsNotRequiredToBeLastNearAnnotations too: an annotated parameter after a defaulted one is
// the same shape, and its checks must still run.
func TestDefaultIsNotRequiredToBeLastNearAnnotations(t *testing.T) {
	diags := paramOrderDiags(t, "def f(a, b=1, c: int = 0) -> int:\n    return a + b + c\n\nz = f(1, 2)\n")
	for _, d := range diags {
		if d.Level == LevelError {
			t.Errorf("a legal call against an annotated tail parameter was refused: %v", d)
		}
	}
	bad := paramOrderDiags(t, "def f(a, b=1, c: int = 0) -> int:\n    return a + b + c\n\nz = f(1, 2, \"three\")\n")
	var found bool
	for _, d := range bad {
		if d.Level == LevelError && strings.Contains(d.Msg, "expected int, got str") {
			found = true
		}
	}
	if !found {
		t.Errorf("the annotation after a defaulted parameter was not enforced: %v", bad)
	}
}
