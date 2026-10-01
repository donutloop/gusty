package lang

import (
	"strings"
	"testing"
)

// Gap R.50 (ADR 0236): a tie goes to the nearest EVEN value.
//
// round(2.5) answered 3 on both backends. Neither answer was a guess — each was the honest output of
// a correct implementation of the *wrong* rule: `math.Round` in the evaluator (ties away from zero)
// and `@llvm.round.f64` in the compiled runtime (the same). Two independent implementations of one
// wrong idea is precisely what a parity matrix reads as agreement, and it is why this file asserts
// values rather than agreement.
//
// The literal and the variable are separate rows because they are separate codegen paths — the
// constant fold (`floatEval`) and the runtime intrinsic — and the fold was wrong for literals while
// the call was wrong for variables. A change that fixes only one leaves half of every program wrong.

func TestRoundTiesToEvenInBothBackends(t *testing.T) {
	cases := []struct{ expr, want string }{
		{"round(0.5)", "0"},
		{"round(1.5)", "2"},
		{"round(2.5)", "2"},
		{"round(3.5)", "4"},
		{"round(4.5)", "4"},
		{"round(-0.5)", "0"},
		{"round(-1.5)", "-2"},
		{"round(-2.5)", "-2"},
		{"round(-3.5)", "-4"},
		// Not ties: the nearest value still wins, so these must not move.
		{"round(1.4)", "1"},
		{"round(1.6)", "2"},
		{"round(-1.4)", "-1"},
		// An integer is the identity.
		{"round(7)", "7"},
		{"round(-7)", "-7"},
	}
	for _, tc := range cases {
		src := "print(" + tc.expr + ")\n"
		t.Run(tc.expr, func(t *testing.T) {
			res, err := JIT(src, 0)
			if err != nil {
				t.Fatalf("compile %s: %v", tc.expr, err)
			}
			if got := strings.TrimSpace(res.Output); got != tc.want {
				t.Errorf("compiled %s = %q, want %q", tc.expr, got, tc.want)
			}
		})
	}
}

func TestRoundTiesToEvenInTheInterpreter(t *testing.T) {
	// The same table through the evaluator, so the two implementations of the rule are pinned to the
	// same numbers rather than to each other.
	for _, tc := range []struct {
		expr string
		want int64
	}{
		{"round(0.5)", 0},
		{"round(2.5)", 2},
		{"round(3.5)", 4},
		{"round(-2.5)", -2},
		{"round(-0.5)", 0},
		{"round(2.4)", 2},
		{"round(7)", 7},
	} {
		v := evalStr(t, "x = "+tc.expr+"\nprint(x)\nx\n")
		if v != tc.want {
			t.Errorf("interpreter %s = %d, want %d", tc.expr, v, tc.want)
		}
	}
}

func TestRoundRuntimePathIsTheEvenIntrinsic(t *testing.T) {
	mod := compileSrcIR(t, "a = 2.5\nprint(round(a))\n")
	if !strings.Contains(mod, "call double @llvm.roundeven.f64") {
		t.Errorf("the runtime path did not emit the ties-to-even intrinsic:\n%s", mod)
	}
	if strings.Contains(mod, "@llvm.round.f64") {
		t.Errorf("the ties-away-from-zero intrinsic is still in the module:\n%s", mod)
	}
}
