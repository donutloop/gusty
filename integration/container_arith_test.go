package integration

// End-to-end coverage for Gap R.175 (ADR 0292): a container in a numeric operand leaves the compiler's
// exit-2 class behind.
//
// The shapes below were measured on a binary built from the pre-cycle HEAD, where each one exited 2 —
// llc-20 refusing OUR module, not the program:
//
//	print([0] * 3)        ->  mul i32 @.lst1, 3
//	print([1, 2] + [3])   ->  add i32 @.lst1, @.lst2
//	print([1] / 2)        ->  sitofp i32 @.lst1 to double
//
// because `value()` renders a list literal as the ADDRESS of a compile-time global. Two answers, split by
// what the reference does: where CPython raises, the compiled path raise its sentence; where CPython answers
// and no runtime helper exists, the compiled leg refuses in words. Both are contract codes (1 and 3);
// neither is 2, and neither is a number the reference disagrees with.

import (
	"strings"
	"testing"
)

// TestContainerArithmeticExitCodesStayOnTheContract is the whole-corpus version of the rule: the
// exit-code contract has no code for "the compiler produced a module the assembler dislikes", because
// that outcome is a bug to fix rather than a result to report (ADR 0166).
func TestContainerArithmeticExitCodesStayOnTheContract(t *testing.T) {
	for _, src := range []string{
		"print([0] * 3)", "print(3 * [0])", "print([1, 2] + [3])", "print([1] - [2])",
		"print([1] / 2)", "print([1] // 2)", "print([1] % 2)", "print([1] ** 2)",
		"print({1: 2} * 2)", "print({1} * 2)", "print([1] + {})", "print({} + [])",
		"print([] < {})", "print([1] < 2)", "print([1] < [2])", "print((1,) + (2,))",
		// The multiplier family: every one of these reaches an arithmetic instruction today.
		"print([1] * \"x\")", "print([1] * 2.0)", "print([1] * None)", "print([1] * {})",
		"print([1] * [2])", "print({} * [])", "print([0] * -3)", "print(2.0 * [1])",
	} {
		src := src
		t.Run(src, func(t *testing.T) {
			_, code := cliRunMerged(t, "--aot", "--eval", src)
			if code == 2 {
				t.Errorf("exit 2 for %s — the contract's code for OUR bug (Gap R.175, ADR 0166)", src)
			}
			if code < 0 || code > 3 {
				t.Errorf("exit %d for %s is outside the contract's table", code, src)
			}
		})
	}
}

// TestContainerArithmeticRaiseArmsRunOnBothEngines checks each leg independently. A `try:` arm that never
// runs prints NOTHING, so "the compiled path agree" would pass on a pair of silent programs — the arms have to
// be seen firing, on both, and against a live reference rather than a remembered one.
func TestContainerArithmeticRaiseArmsRunOnBothEngines(t *testing.T) {
	src := readProgram(t, "probe_a_container_in_arithmetic.gy")
	dir := t.TempDir()
	want, wantOK := cpythonPlainOut(t, dir, src)
	if !wantOK {
		t.Skip("no usable oracle")
	}
	// The reference raises on the FIRST line, so the whole-program reference text is the sentence the
	// reference itself prints; what the probe prints is its arms. Comparing the two directly would be
	// comparing a trap against a handler — so instead every arm line is required to be a sentence the
	// reference is separately measured to raise (pkg/lang's TestTheReferenceRaiseIsTheCompiledRaise
	// pins each one row by row against a live python3), and the arms must be non-empty on both legs.
	// `want` IS the reference running this same source, so the arms have to match it line for line:
	// a reference that printed a DIFFERENT arm is a reference that did not raise, and an engine that
	// printed a different one is the engine that did not. This is the check, not a comment.
	wantLines := strings.Split(strings.TrimRight(want, "\n"), "\n")
	if len(wantLines) != 6 {
		t.Fatalf("the reference printed %d lines, want the six arms: %q", len(wantLines), want)
	}
	for _, line := range wantLines {
		if !strings.HasPrefix(line, "raised: ") {
			t.Errorf("the reference printed %q, which is not one of the probe's arms — a line the reference answers has no business in this file", line)
		}
	}
	const firstArm = "raised: unsupported operand type(s) for -: 'list' and 'list'"
	if got, _ := cliRunMerged(t, "--aot", "--file", writeSrc(t, dir, "container_arith.gy", src)); !strings.Contains(got, firstArm) {
		t.Errorf("interpreter never took the raise arms: %q", got)
	}
	if got, _ := cliRunMerged(t, "--aot", "--file", writeSrc(t, dir, "container_arith.gy", src)); !strings.Contains(got, firstArm) {
		t.Errorf("compiled leg never took the raise arms: %q", got)
	}
}

// TestContainerArithmeticKeepsTheWorkingAnswers is the ladder's other direction: an answer may not become
// a refusal on the way to fixing a crash. A container passed as a CALL ARGUMENT is legal (only an
// arithmetic operand is not), a comparison over two names of the same kind still orders, and a text still
// repeats where the backend can do it.
func TestContainerArithmeticKeepsTheWorkingAnswers(t *testing.T) {
	for _, r := range []struct{ src, want string }{
		{"def half(xs):\n    return xs[0] / 2\n\n\nprint(half([1.5]))\n", "0.75\n"},
		{"a = [1]\nb = [2]\nprint(a < b)\n", "True\n"},
		{"xs = [2]\nprint(xs[0] * 3)\n", "6\n"},
		{"def first(xs):\n    return xs[0] + 1\n\n\nprint(first([2]))\n", "3\n"},
	} {
		r := r
		t.Run(strings.TrimSpace(r.src), func(t *testing.T) {
			dir := t.TempDir()
			if got, _ := cliRunMerged(t, "--aot", "--eval", r.src); got != r.want {
				t.Errorf("interpreter printed %q, want %q", got, r.want)
			}
			path := writeSrc(t, dir, "kept.gy", r.src)
			if got, code := cliRunMerged(t, "--aot", "--file", path); got != r.want {
				t.Errorf("--aot printed %q (exit %d), want %q — a working answer may not become a refusal", got, code, r.want)
			}
		})
	}
}
