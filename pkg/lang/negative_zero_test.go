package lang

import (
	"math"
	"regexp"
	"strings"
	"testing"
)

// negative_zero_test.go — roadmap Gap R.132, ADR 0349: a negative zero the COMPILER wrote had no sign.
//
// `print(-0.0)` printed `0.0` and `x = -0.0` / `print(x)` printed `0.0`, both at exit 0, while
// `print(0.0 * -1)` printed `-0.0` — because the two roads differ in who makes the number. The thirteen
// sites that materialise a compile-time double into a register said `fadd double 0.0, <const>`, a no-op
// addition, and IEEE answers `-0.0 + +0.0` with `+0.0`: the sign was gone before the module existed, and
// `rt_fmt_double` — the one renderer that handles the sign correctly — was never handed anything to
// render. A value whose sign bit is set now takes the IEEE bit pattern, bitcast, exactly as ADR 0264
// already does for the two values a decimal spelling cannot write.
//
// Every expectation here is what `python3` prints for the same source.

// minusZeroBits is the IEEE 754 bits of -0.0 written the only way llc accepts in an integer position:
// decimal. (A bare `0x…` token is read as a floating-point constant whatever type precedes it, and
// `bitcast i64 0x8000000000000000 to double` dies in the assembler — exit 2, ADR 0166.)
const minusZeroBits = "9223372036854775808"

// signClearMaskBits is 0x7FFFFFFFFFFFFFFF — the IEEE double with its sign bit kept off everything
// else — spelled out because a mask is exactly the kind of constant that gets re-typed from memory and
// a wrong one is silent: an early version of ADR 0349 wrote 0x7FEFFFFFFFFFFFFF, which still turned
// -0.0 into 0.0 and turned `abs(-4)` into 2.
const signClearMaskBits = "9223372036854775807"

// TestAConstantNegativeZeroKeepsItsSignWhenPrinted is the answer, read off the linked binary: the four
// lines of programs/probe_negative_zero_constant.gy and the roads around them. The two that were wrong
// are the first pair; the second pair was already right, and is here because a fix that broke them would
// be fixing the wrong thing.
func TestAConstantNegativeZeroKeepsItsSignWhenPrinted(t *testing.T) {
	t.Parallel()
	tests := []struct {
		src, want string
	}{
		// The row's own four lines. CPython prints `-0.0` for all of them; before ADR 0349 the
		// compiled leg printed `0.0` for the first two.
		{"print(-0.0)\n", "-0.0\n"},
		{"y = -0.0\nprint(y)\n", "-0.0\n"},
		{"print(0.0 * -1)\n", "-0.0\n"},
		{"print(round(-0.5, 0))\n", "-0.0\n"},
		// The same value through the roads that render it: str(), a container's slots, a loop, and
		// a call boundary — each is a different emitter, and the sign is one value's property, not
		// one renderer's business.
		{"print(str(-0.0))\n", "-0.0\n"},
		{"print([-0.0, 0.0])\n", "[-0.0, 0.0]\n"},
		{"print({1: -0.0})\n", "{1: -0.0}\n"},
		{"xs = [-0.0]\nfor v in xs:\n    print(v)\n", "-0.0\n"},
		{"def p(v):\n    print(v)\n\np(-0.0)\n", "-0.0\n"},
		// And what must NOT change: a negative zero still equals a positive one, still sorts with
		// them, and a positive zero still prints without a sign.
		{"print(-0.0 == 0.0)\n", "True\n"},
		{"print(0.0)\n", "0.0\n"},
		{"print(-1.5)\n", "-1.5\n"},
		{"print(-0.0 if 1 < 2 else -1.0)\n", "-0.0\n"},
		// The other half of the promise, and the one the RECORD caught: a sign that is now stated is a
		// sign the sign-removing operations have to actually remove. `abs` over a slot the runtime owns
		// answered -0.0 the moment ADR 0349 stopped the constant's sign being eaten upstream, because
		// the door asked `fcmp olt …, 0.0` — false for -0.0 — instead of clearing the bit. CPython says
		// 0.0 for all five of these, and the retired engine's record said 0.0 for the first, which is
		// how a fix that looked like progress became a wrong answer for a day (roadmap Gap R.132,
		// ADR 0349; the same lesson ADR 0236 wrote about two renderers of one value).
		{"xs = []\nxs.append(-0.0)\nn = xs[0]\nprint(abs(n))\n", "0.0\n"},
		{"xs = [-0.0]\nprint(abs(xs[0]))\n", "0.0\n"},
		{"print(-(-0.0))\n", "0.0\n"},
		{"xs = [-0.0]\nprint(-xs[0])\n", "0.0\n"},
		{"xs = [-0.0]\nprint(xs[0] * -1)\n", "0.0\n"},
		// The same door's INT and BOOL families, pinned beside the float one because the first version of
		// the magnitude cleared the wrong bits — a mask of 0x7FEFFF… instead of 0x7FFF… — and printed 2
		// for `abs(-4)` while still printing 0.0 for `abs(-0.0)`. A row that only tests the case the fix
		// was written for tests nothing: these three are what the mask being right actually means.
		{"xs = []\nxs.append(-4)\nn = xs[0]\nprint(abs(n))\n", "4\n"},
		{"xs = []\nxs.append(7)\nn = xs[0]\nprint(abs(n))\n", "7\n"},
		{"xs = []\nxs.append(True)\nn = xs[0]\nprint(abs(n))\n", "1\n"},
		{"xs = [-3.5]\nprint(abs(xs[0]))\n", "3.5\n"},
	}
	for _, tc := range tests {
		code, out := negBuildRun(t, "negzero", tc.src)
		if code != 0 {
			t.Errorf("%q: exit %d, want 0:\n%s", tc.src, code, out)
			continue
		}
		if out != tc.want {
			t.Errorf("%q = %q, want %q (Gap R.132 — CPython prints the sign)", tc.src, out, tc.want)
		}
	}
}

// TestAConstantNegativeZeroIsMaterialisedByItsBits is the IR half of the same claim, and the half that
// says why the fix is a rule rather than a special case for zero: the emitter writes the number, and the
// only instruction between the compiler's value and the runtime formatter's argument is a move.
func TestAConstantNegativeZeroIsMaterialisedByItsBits(t *testing.T) {
	t.Parallel()
	ir := llcCompiles(t, `print(-0.0)`)
	if !strings.Contains(ir, "bitcast i64 "+minusZeroBits+" to double") {
		t.Errorf("print(-0.0) should materialise 0x8000000000000000, got:\n%s", ir)
	}
	if strings.Contains(ir, "fadd double 0.0, -") {
		t.Errorf("a signed constant was materialised by adding +0.0, which is exactly how the sign was lost:\n%s", ir)
	}
	// The other side of the rule: an UNSIGNED constant keeps the addition, because `+0.0 + x` is x
	// exactly there, and a great deal of pinned IR (and no wrong answer) depends on that shape.
	ir = llcCompiles(t, `print(2.5)`)
	if !strings.Contains(ir, "fadd double 0.0, 2.5e+00") {
		t.Errorf("print(2.5) should keep the addition it always used, got:\n%s", ir)
	}
}

// TestSignedConstantsAreNeverMaterialisedByAddingZero is the module-wide tripwire, asked of a program
// that reaches many of the thirteen emitters at once: arithmetic, a fold, min/max, round, a comprehension,
// a container. A new emitter that reaches for the old idiom fails here even if every answer it produces
// happens to be right today — with one exception it should have, and does not: the two-value spelling
// float("inf") and float("nan"), which floatConst already states in hex (ADR 0264) and which carry no
// sign bit into the addition that would eat it.
func TestSignedConstantsAreNeverMaterialisedByAddingZero(t *testing.T) {
	t.Parallel()
	programs := []string{
		"print(-1.5 + 2.0)\n",
		"print(-1.5 * 2)\nprint(-1.5 / 2)\nprint(-2.5 - 1.0)\n",
		"print(max(-1.5, -2.5))\nprint(min(-1.5, -2.5))\n",
		"print(round(-2.675, 2))\n",
		"xs = [-1.5, -0.25]\nprint(xs)\n",
		"print(sum([-1.5, 0.5]))\n",
		"print(-1.5 ** 2)\n",
		"y = -0.5\nprint(y * 2.0)\n",
		"print([-0.5, 1.25])\nprint({1: -0.5})\n",
		// Not in this table, because it cannot be compiled at all yet: `def f(v): return v * 2.0` /
		// `print(f(0.5))` reaches llc with `%t8 = call double @gy_f(...)` and then stores that double
		// through `store i32 %t8` — the numeric door's double arriving at a context that stores an i32
		// word, which is roadmap Gap R.98's row (🟨 PARTIAL since ADR 0253); it reproduced identically
		// against the pre-cycle binary. That is the exit-2 class, so it is that row's business, not a
		// line in this one.
	}
	addingSigned := regexp.MustCompile(`fadd double 0\.0, -`)
	for _, src := range programs {
		ir := llcCompiles(t, src)
		if got := addingSigned.FindString(ir); got != "" {
			t.Errorf("%q emitted %q: a signed constant is materialised by its bits (Gap R.132)\n%s", src, got, ir)
		}
	}
}

// TestMaterialiseFloatStatesItsOwnRule is the predicate on its own, so a change to the emitter that
// keeps today's answers but loses the reason cannot pass unnoticed: negative means bits, positive means
// the addition, and the bits are written in decimal because the assembler reads a bare hex token as a
// float.
func TestMaterialiseFloatStatesItsOwnRule(t *testing.T) {
	t.Parallel()
	tests := []struct {
		v    float64
		want string
	}{
		{0, "  %t = fadd double 0.0, 0.0e+00\n"},
		{2.5, "  %t = fadd double 0.0, 2.5e+00\n"},
		// -0.0 and -Inf are written by their bits because a Go CONSTANT `-0.0` is the constant 0 —
		// the negation of an untyped zero is still zero, which is precisely the confusion this row
		// exists to prevent. The value has to arrive with its sign bit set to be worth testing.
		{math.Float64frombits(0x8000000000000000), "  %t = bitcast i64 " + minusZeroBits + " to double\n"},
		{-2.5, "  %t = bitcast i64 13836183955189006336 to double\n"},
		// The two values a decimal spelling cannot write keep ADR 0264's hex on the unsigned road
		// and take the bit pattern on the signed one — either way the number is stated, not derived.
		{math.Inf(1), "  %t = fadd double 0.0, 0x7FF0000000000000\n"},
		{math.Inf(-1), "  %t = bitcast i64 18442240474082181120 to double\n"},
	}
	for _, tc := range tests {
		var b strings.Builder
		materialiseFloat(&b, "%t", tc.v)
		if got := b.String(); got != tc.want {
			t.Errorf("materialiseFloat(%v) = %q, want %q", tc.v, got, tc.want)
		}
	}
	// The decimal spelling is not decoration: the same bits written 0x… is a module llc refuses.
	var b strings.Builder
	materialiseFloat(&b, "%t", math.Float64frombits(0x8000000000000000))
	if strings.Contains(b.String(), "0x") {
		t.Errorf("a hex token in an integer position is a floating-point constant to LLVM's parser:\n%s", b.String())
	}
}

// TestTheRuntimeAbsDoorClearsTheSignBit is the same rule asserted where the value is not a constant at
// all: the signless call over a slot the run time owns removes the sign by clearing the bit, and does
// not ask whether the number is less than zero — the question that is false for -0.0 (roadmap Gap
// R.132, ADR 0349).
func TestTheRuntimeAbsDoorClearsTheSignBit(t *testing.T) {
	t.Parallel()
	ir := llcCompiles(t, "xs = []\nxs.append(-0.0)\nn = xs[0]\nprint(abs(n))\n")
	if !strings.Contains(ir, "and i64 %abits, "+signClearMaskBits) {
		t.Errorf("abs over a run-time slot should clear the sign bit, got:\n%s", ir)
	}
	if strings.Contains(ir, "%absv = select") {
		t.Errorf("abs must not be an fcmp-against-zero select: that is false for -0.0 (Gap R.132):\n%s", ir)
	}
}
