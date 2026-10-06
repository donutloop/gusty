package integration

import (
	"strings"
	"testing"

	"github.com/donutloop/gusty/pkg/lang"
)

// TestRoundTiesToEvenOnAllThreeLegs is the integration half of Gap R.50 (ADR 0236).
//
// `round(2.5)` answered 3 on both gusty backends and 2 in CPython. That is the defect class a
// two-backend matrix is blind to by construction: the interpreter used `math.Round` and the compiled
// runtime used `llvm.round.f64`, two independent implementations of the same *wrong* rule, so parity
// passed and said nothing. The only instrument that sees it is the oracle leg — hence expectations
// here are CPython's, run rather than remembered, and the corpus program `programs/round_ties.gy`
// carries the same table with no ledger row of its own.
//
// The literal and variable spellings are separate cases because they are separate paths in codegen:
// `floatEval` folds the first, and only the second reaches `llvm.roundeven.f64`. Fixing the runtime
// call while the fold still tied away from zero would have left half the program wrong.
func TestRoundTiesToEvenOnAllThreeLegs(t *testing.T) {
	for _, tc := range []struct{ name, src, want string }{
		{"ties_to_even_literals", "print(round(0.5))\nprint(round(1.5))\nprint(round(2.5))\nprint(round(3.5))\n", "0\n2\n2\n4\n"},
		{"ties_to_even_negatives", "print(round(-0.5))\nprint(round(-1.5))\nprint(round(-2.5))\nprint(round(-3.5))\n", "0\n-2\n-2\n-4\n"},
		{"non_ties_unchanged", "print(round(1.4))\nprint(round(1.6))\nprint(round(-1.4))\nprint(round(-1.6))\n", "1\n2\n-1\n-2\n"},
		{"ints_are_the_identity", "print(round(7))\nprint(round(-7))\nprint(round(0))\n", "7\n-7\n0\n"},
		{"ties_to_even_runtime_values", "a = 2.5\nb = 3.5\nc = -2.5\nprint(round(a))\nprint(round(b))\nprint(round(c))\n", "2\n4\n-2\n"},
		{"computed_tie", "x = 1.0\ny = 2.0\nt = x + y\nu = t + 0.5\nprint(round(u))\n", "4\n"},
		{"tie_inside_an_expression", "n = 0.5\nprint(round(n) + 10)\nprint(round(2.5) * 2)\n", "10\n4\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			lang.RecordedStdoutIs(t, tc.src, tc.want)
			res, err := lang.JIT(tc.src, 0)
			if err != nil {
				t.Fatalf("compile: %v", err)
			}
			if res.Output != tc.want {
				t.Errorf("compiled\n got %q\nwant %q", res.Output, tc.want)
			}
			if py := pythonOutput(t, tc.src); py != tc.want {
				t.Errorf("CPython disagrees with the expectation we asserted\n got %q\nwant %q", py, tc.want)
			}
		})
	}
}

func TestRoundUsesTheTiesToEvenIntrinsicNotTheAwayFromZeroOne(t *testing.T) {
	// The compiled rule lives in exactly one intrinsic; the fold and the call must not be different
	// answers to the same question.
	mod := mustIRText(t, "a = 2.5\nprint(round(a))\n")
	if !strings.Contains(mod, "call double @llvm.roundeven.f64") {
		t.Fatalf("the runtime path is not the ties-to-even intrinsic:\n%s", mod)
	}
	if strings.Contains(mod, "@llvm.round.f64") {
		t.Fatalf("the ties-away-from-zero intrinsic is still emitted:\n%s", mod)
	}
	// The constant path folds at compile time, so it leaves no call behind at all — which is exactly
	// why it needed its own assertion: a module with no `llvm.round*` in it would pass a check that
	// only looks for the right call.
	folded := mustIRText(t, "print(round(2.5))\n")
	if strings.Contains(folded, "llvm.round") {
		t.Fatalf("a constant tie should fold, not call:\n%s", folded)
	}
	if !strings.Contains(folded, "call void @rt_print_mixed_value(i32 2") &&
		!strings.Contains(folded, ", i32 2)") {
		t.Errorf("the constant tie did not fold to 2:\n%s", folded)
	}
}

// mustIRText compiles source to textual IR, failing the test if the compiler refuses.
func mustIRText(t *testing.T, src string) string {
	t.Helper()
	res, err := lang.Compile(src)
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	return res.IR
}
