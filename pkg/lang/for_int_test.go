package lang

import "testing"

// `for x in <integer>` binds 0, 1, … n-1. The two backends have always agreed about it, which is what
// makes it a feature; the gap (roadmap R.14, ADR 0207) was that no document said it and no test would
// notice if it moved — and that its boundary (counts of zero or less, comprehensions) was unstated.

func forIntDiags(t *testing.T, src string) []Diagnostic {
	t.Helper()
	prog, err := Parse(src)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	return Analyze(prog)
}

func TestIntegerIterableIsAccepted(t *testing.T) {
	for _, tc := range []struct{ name, src string }{
		{"literal count", "for i in 4:\n    print(i)\n"},
		{"variable count", "n = 3\nfor j in n:\n    print(j)\n"},
		{"expression count", "for k in 2 + 1:\n    print(k)\n"},
		{"zero", "for i in 0:\n    print(i)\n"},
		{"negative", "for i in -2:\n    print(i)\n"},
		{"nested", "for i in 2:\n    for j in 3:\n        print(i * j)\n"},
		{"count from a call", "def bound() -> int:\n    return 2\n\nfor i in bound():\n    print(i)\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			for _, d := range forIntDiags(t, tc.src) {
				if d.Level == LevelError {
					t.Errorf("%s: an integer iterable was refused: %v", tc.name, d)
				}
			}
		})
	}
}

// TestLoopVariableIsBoundInScopeAfterwards keeps the scoping half of the construct: the variable is the
// same name a `for ... in range(n)` loop would leave behind (docs/language.md § Control flow), so the
// integer form must not become a second, weirder loop.
func TestLoopVariableIsBoundInScopeAfterwards(t *testing.T) {
	for _, d := range forIntDiags(t, "total = 0\nfor a in 5:\n    total = total + a\n\nprint(total)\n") {
		if d.Level == LevelError {
			t.Errorf("the loop variable or the accumulator was refused: %v", d)
		}
	}
}
