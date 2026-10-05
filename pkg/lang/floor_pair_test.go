package lang

// pkg/lang/floor_pair_test.go — `//` and `%` answer over a (payload, tag) pair (roadmap Gap R.162, ADR 0278).
//
// ADR 0216 chose the flooring rules by the operator and the operand's written kind, and ADR 0273/0276/0277
// carried a number's *kind* across a call in a second word beside its payload. The two never met: the pair
// door served `+ - *`, the condition doors served the comparisons, and the flooring operators were in neither
// list — so a parameter that could hold a double stayed on the ordinary one-word road and the double was
// truncated before the floor ran. `def floorit(v): return v // 2` with `floorit(5.0)` printed `2` at exit 0;
// `modop(7.5, 2)` printed `1` where the reference prints `1.5`; `modop(-7.5, 2)` printed `1` for `0.5`. The
// interpreter, which boxes the value and asks it, was right on every one, which is what made the family a
// compiled-backend defect rather than a language question.
//
// Four claims, four tables:
//
//   - the answers on both engines, including the two sign rules that make flooring worth testing — the floor
//     goes down rather than toward zero, and the remainder takes the divisor's sign — and the integer control
//     rows, which must keep answering integers from the same `define`;
//   - the module: the two new operator codes are asked of `@rt_num_arith`, the divide-by-zero guard is on the
//     path, and the answer's tag is the operands' (`bothint`), not a guess;
//   - the traps: which of the reference's four ZeroDivisionError sentences a line gets is the *operand's*
//     kind's fact, so the same source line names itself differently for `f(5)` and `f(5.0)`, catchably;
//   - what still refuses: the flooring answer combined with other arithmetic in one expression, which is
//     Gap R.166's shape — refusal, not a number.

import (
	"os"
	"os/exec"
	"strings"
	"testing"
)

// runIRAllowingTrap runs the module the way runIR does — `llvm-as-20`, then `lli-20` — but reports a
// non-zero exit instead of failing the test, because a trap that exits 3 with the reference's sentence is
// what these rows are asserting. The combined stream is returned: the traceback is the program's report.
func runIRAllowingTrap(t *testing.T, ir string) (int, string) {
	t.Helper()
	tmp, err := os.CreateTemp("", "floor-trap-*.ll")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := tmp.WriteString(ir); err != nil {
		t.Fatal(err)
	}
	tmp.Close()
	defer os.Remove(tmp.Name())
	bc := tmp.Name() + ".bc"
	defer os.Remove(bc)
	if out, err := exec.Command("llvm-as-20", tmp.Name(), "-o", bc).CombinedOutput(); err != nil {
		t.Fatalf("llvm-as: %v\n%s", err, out)
	}
	out, err := exec.Command("lli-20", bc).CombinedOutput()
	code := 0
	if ee, ok := err.(*exec.ExitError); ok {
		code = ee.ExitCode()
	} else if err != nil {
		t.Fatalf("lli: %v\n%s", err, out)
	}
	return code, string(out)
}

// TestTheFlooringOperatorsAnswerOverAPairOnBothBackends is the row itself.
func TestTheFlooringOperatorsAnswerOverAPairOnBothBackends(t *testing.T) {
	for _, tc := range []struct{ name, src, want string }{
		{
			"the row: a floor and a remainder over a parameter that carries a double",
			"def floorit(v):\n    return v // 2\n\ndef modit(v):\n    return v % 2\n\nprint(floorit(5.0))\nprint(modit(5.0))\n", "2.0\n1.0\n",
		},
		{
			// One `define`, both kinds: the pair carries the argument's kind and the answer follows it,
			// which is the whole reason a `double` parameter convention could not express this.
			"the same define answers an int with an int",
			"def floorit(v):\n    return v // 2\n\nprint(floorit(5.0))\nprint(floorit(5))\n", "2.0\n2\n",
		},
		{
			// The two rules that make flooring a language feature rather than a `fdiv`: CPython's floor
			// rounds toward negative infinity, and its remainder carries the divisor's sign.
			"the floor goes down, and the remainder takes the divisor's sign",
			"def floorit(v):\n    return v // 2\n\ndef modit(v):\n    return v % 2\n\nprint(floorit(-7.5))\nprint(modit(-7.5))\nprint(floorit(-7))\nprint(modit(-7))\n",
			"-4.0\n0.5\n-4\n1\n",
		},
		{
			"a remainder of a float by an int keeps the float's fraction",
			"def modop(a, b):\n    return a % b\n\nprint(modop(7.5, 2))\nprint(modop(-7.5, 2))\nprint(modop(7, 2))\n", "1.5\n0.5\n1\n",
		},
		{
			"the floor of a float by a float, both arguments pairs",
			"def floordiv(a, b):\n    return a // b\n\nprint(floordiv(7.5, 2))\nprint(floordiv(7.0, 2.0))\nprint(floordiv(7, 2))\n", "3.0\n3.0\n3\n",
		},
		{
			// The row ADR 0273 pinned as a refusal (`return v % 3` cannot read a tagged parameter) and
			// ADR 0278 answered: the slot says int, the answer is an int, and the door is the one that said so.
			"a slot read under the remainder",
			"def f(v):\n    return v % 3\n\nxs = []\nxs.append([7, 8])\nprint(f(xs[0][0]))\n", "1\n",
		},
		{
			"a slot read under the floor, and the same function over a float slot",
			"def f(v):\n    return v // 3\n\nxs = []\nxs.append([7, 8])\nxs.append([7.5, 8])\nprint(f(xs[0][0]))\nprint(f(xs[1][0]))\n", "2\n2.0\n",
		},
		{
			// Flooring one frame deeper: the caller forwards the pair, so the callee's floor is asked of a
			// tag it never saw a literal for (roadmap Gap R.161, ADR 0277's door, ADR 0278's operators).
			"a forwarded pair under the floor",
			"def floorit(v):\n    return v // 2\n\ndef outer(x):\n    return floorit(x)\n\nprint(outer(5.0))\nprint(outer(5))\n", "2.0\n2\n",
		},
		{
			"a forwarded pair handed to the callee that takes the remainder",
			"def other(w):\n    return w % 3\n\ndef f(v):\n    return other(v)\n\nxs = []\nxs.append([7, 8])\nprint(f(xs[0][0]))\n", "1\n",
		},
		{
			// The identity ADR 0216 pins, one operand at a time: `a == (a // b) * b + (a % b)` holds of the
			// two halves separately here, and the whole of it is Gap R.166's refusal until nested pair
			// arithmetic is served.
			"half the flooring identity, over a parameter",
			"def halfer(v):\n    return v // 2\n\nprint(halfer(7))\nprint(halfer(7.5))\n", "3\n3.0\n",
		},
		{
			"a floor inside a body that prints, beside the parameter it prints",
			"def show(v):\n    print(v)\n    print(v // 2)\n    return 0\n\nshow(5.0)\n", "5.0\n2.0\n",
		},
		{
			"a condition over the parameter and a floored answer in the taken arm",
			"def big(v):\n    if v > 10:\n        return v // 2\n    return v\n\nprint(big(18.5))\nprint(big(7.5))\n", "9.0\n7.5\n",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if out := captureStdout(t, tc.src); out != tc.want {
				t.Errorf("interpreter: stdout %q, want %q\nsrc: %s", out, tc.want, tc.src)
			}
			res, err := Compile(tc.src)
			if err != nil {
				t.Fatalf("compiled leg refused a program the oracle prints (%v): %s", err, tc.src)
			}
			assertNoForbiddenIR(t, tc.src, res.IR)
			if got := runIR(t, res.IR); got != tc.want {
				t.Errorf("compiled: stdout %q, want %q\nsrc: %s", got, tc.want, tc.src)
			}
		})
	}
}

// TestTheFlooringDoorIsTheRuntimeThatFloors pins where the arithmetic lives: the operators are asked of
// `@rt_num_arith` with the two new codes, the divisor's zero test is on the path before any answer is
// written, and the flooring itself is the same `llvm.floor` / corrected-`frem` pair the statement-level road
// uses — one pair of rules, so the two roads cannot drift into two different definitions of `//`.
func TestTheFlooringDoorIsTheRuntimeThatFloors(t *testing.T) {
	for _, tc := range []struct {
		name, src string
		ops       []string
	}{
		{
			"the floor operator",
			"def floorit(v):\n    return v // 2\n\nprint(floorit(5.0))\n",
			[]string{"@rt_num_arith(i32 4,"},
		},
		{
			"the remainder operator",
			"def modit(v):\n    return v % 2\n\nprint(modit(5.0))\n",
			[]string{"@rt_num_arith(i32 5,"},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			res, err := Compile(tc.src)
			if err != nil {
				t.Fatalf("compiled leg refused a program the oracle prints (%v): %s", err, tc.src)
			}
			for _, frag := range tc.ops {
				if !strings.Contains(res.IR, frag) {
					t.Errorf("the module never asks the door %s — the flooring is happening somewhere else\nsrc: %s", frag, tc.src)
				}
			}
			// The runtime floor and the truncated-remainder correction, in the one helper the two roads
			// share (codegen.go's numArithRuntimeIR), with the four ZeroDivisionError sentences beside them.
			for _, frag := range []string{"@llvm.floor.f64", "frem double", "@rt.num.dzf", "@rt.num.dzm2"} {
				if !strings.Contains(res.IR, frag) {
					t.Errorf("the shared flooring helper is missing %s\nsrc: %s", frag, tc.src)
				}
			}
			if !strings.Contains(res.IR, "numzero") {
				t.Errorf("no divide-by-zero branch on the path — an unguarded fdiv answers inf, which is a number-shaped lie\nsrc: %s", tc.src)
			}
		})
	}
}

// TestTheFlooringTrapNamesTheKindTheOperandHad is the half that a truncated road could not get right even by
// accident: which of CPython's four sentences a line raises is a fact about the operand's kind, and the kind
// is the tag. One source line, two arguments, two different sentences — and both catchable, because a trap
// that cannot be caught by name is a different bug with better output.
func TestTheFlooringTrapNamesTheKindTheOperandHad(t *testing.T) {
	for _, tc := range []struct{ name, src, want string }{
		{
			"the floor by zero, over a parameter that carries a double",
			"def f(v):\n    return v // 0\n\nprint(f(5.0))\n", "ZeroDivisionError: float floor division by zero",
		},
		{
			"the floor by zero, over the same define with an int",
			"def f(v):\n    return v // 0\n\nprint(f(5))\n", "ZeroDivisionError: integer division or modulo by zero",
		},
		{
			// The sentence the truncated road could not produce at all: CPython's `%` on ints does not
			// share the floor's wording.
			"the remainder by zero, over a parameter that carries an int",
			"def f(v):\n    return v % 0\n\nprint(f(5))\n", "ZeroDivisionError: integer modulo by zero",
		},
		{
			"the remainder by zero, over a parameter that carries a double",
			"def f(v):\n    return v % 0\n\nprint(f(5.0))\n", "ZeroDivisionError: float modulo",
		},
		{
			"a zero the program computed, not a zero it wrote",
			"def z():\n    return 0\n\ndef f(v):\n    return v % 0 + v // z()\n\nprint(f(1))\n", "ZeroDivisionError: integer modulo by zero",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			res, err := Compile(tc.src)
			if err != nil {
				t.Fatalf("compiled leg refused a program the reference only raises (%v): %s", err, tc.src)
			}
			code, got := runIRAllowingTrap(t, res.IR)
			if code == 0 {
				t.Errorf("the trap exited 0 — a trap that produces output is not a trap\nsrc: %s", tc.src)
			}
			if !strings.Contains(got, tc.want) {
				t.Errorf("the trap said the wrong thing:\n  got  %s\n  want %s\nsrc: %s", firstLine(got), tc.want, tc.src)
			}
			if strings.Contains(got, "integer division or modulo by zero") && strings.Contains(tc.want, "float") {
				t.Errorf("the message named the integer sentence for a tagged double — the tag did not reach the message\nsrc: %s", tc.src)
			}
		})
	}
	// Catchable, on both engines: the class name is the contract an `except` reads.
	for _, tc := range []struct{ name, src string }{
		{"the floor", "def f(v):\n    return v // 0\n\ntry:\n    print(f(5.0))\nexcept ZeroDivisionError:\n    print(\"caught\")\n"},
		{"the remainder", "def f(v):\n    return v % 0\n\ntry:\n    print(f(5.0))\nexcept ZeroDivisionError:\n    print(\"caught\")\n"},
	} {
		t.Run("caught: "+tc.name, func(t *testing.T) {
			if out := captureStdout(t, tc.src); out != "caught\n" {
				t.Errorf("interpreter: the handler did not run, stdout %q\nsrc: %s", out, tc.src)
			}
			res, err := Compile(tc.src)
			if err != nil {
				t.Fatalf("compiled leg refused a caught trap (%v): %s", err, tc.src)
			}
			if got := runIR(t, res.IR); got != "caught\n" {
				t.Errorf("compiled: stdout %q, want the handler's %q\nsrc: %s", got, "caught\n", tc.src)
			}
		})
	}
}

// TestAPairExpressionCombinedWithMoreArithmeticAnswersOnBothBackends is Gap R.166 paid. ADR 0278 served the
// flooring operators at the *top* of an expression; the arms of one were still asked of the ordinary road, so
// `(v - 1) * 2`, `v + 1 + 1` and ADR 0216's own flooring identity refused for a parameter that carries a pair —
// after a first draft of this cycle answered them from their payloads, which printed 7 where the reference
// prints 7.5. The arm question now walks its leaves (`slotArithmeticIsProven` recurses through `BinOp`,
// `UnOp` and `CondExpr`), so each arm is answered by the same door the whole is.
func TestAPairExpressionCombinedWithMoreArithmeticAnswersOnBothBackends(t *testing.T) {
	for _, tc := range []struct{ name, src, want string }{
		{
			// The acceptance row ADR 0216 named: floor and remainder reassemble the value they were given,
			// over a parameter as over a literal — including the negative, where a truncated pair of
			// operators would disagree with itself as well as with the reference.
			"the flooring identity over a parameter",
			"def f(v):\n    return (v // 2) * 2 + (v % 2)\n\nprint(f(7))\nprint(f(7.5))\nprint(f(-3.5))\n", "7\n7.5\n-3.5\n",
		},
		{
			"the identity asked as a question, so a wrong half cannot hide",
			"def f(v):\n    return (v // 2) * 2 + (v % 2) == v\n\nprint(f(7.5))\n", "True\n",
		},
		{
			"a product of a floored parameter, both kinds",
			"def f(v):\n    return (v // 2) * 2\n\nprint(f(7.5))\nprint(f(7))\n", "6.0\n6\n",
		},
		{
			"a product beside a literal — refused since ADR 0276",
			"def f(v):\n    return v * 2 + 1\n\nprint(f(7.5))\nprint(f(7))\n", "16.0\n15\n",
		},
		{
			"a difference under a product",
			"def f(v):\n    return (v - 1) * 2\n\nprint(f(2.5))\nprint(f(2))\n", "3.0\n2\n",
		},
		{
			"a sum of a sum (left-nested, the shape the arm question used to decline)",
			"def f(v):\n    return v + 1 + 1\n\nprint(f(2.5))\n", "4.5\n",
		},
		{
			"a product by a sum (right-nested)",
			"def f(v):\n    return 2 * (v + 1)\n\nprint(f(2.5))\n", "7.0\n",
		},
		{
			"both flooring operators in one answer",
			"def f(v):\n    return (v % 3) + (v // 3)\n\nprint(f(7.5))\nprint(f(7))\n", "3.5\n3\n",
		},
		{
			"the parameter used twice, on both sides of a product",
			"def f(v):\n    return (v + v) * (v - 1)\n\nprint(f(2.5))\nprint(f(2))\n", "7.5\n4\n",
		},
		{
			// Two doors in one body: the pair the caller supplied, and a pair the slot read supplies,
			// combined — the shape ADR 0273's refusal tables kept apart until now.
			"a floored answer scaled by a literal, in the arm of a condition over the parameter",
			"def f(v):\n    if v > 10:\n        return (v // 2) * 2 + 1\n    return v\n\nprint(f(17.5))\nprint(f(2.5))\n", "17.0\n2.5\n",
		},
		{
			// A *call*'s answer as one arm of a pair expression: roadmap Gap R.164's other half, ADR 0280.
			// ADR 0273 read a pair answer at `return other(v)` and nowhere wider, so `f(7.5)` printed nothing
			// at exit 1 while the same shape written as `print(other(7.5))` answered.
			"a call answer as an arm",
			"def other(w):\n    return w % 3\n\ndef f(v):\n    return (v // 2) + other(v)\n\nprint(f(7.5))\nprint(f(7))\n", "4.5\n4\n",
		},
		{
			// Two pair-returning calls in one expression. The trap that caught the first draft: this printed
			// `30.0` for CPython's `18.0` in one arm order and the truth in the other, because a double answer
			// leaves the arithmetic door as a heap box that nothing rooted and the next allocation recycled.
			// Order-dependence is why both orders are in one row.
			"two call answers as the two arms, either order",
			"def half(w):\n    return w // 2\n\ndef twice(u):\n    return u * 2\n\nprint(half(7.5) + twice(7.5))\nprint(twice(7.5) + half(7.5))\n", "18.0\n18.0\n",
		},
		{
			// An identity assembled from two floored call answers — the shape that printed `3.0` for
			// CPython's `7.5` for the same unrooted-box reason, one door deeper.
			"an identity assembled from two call answers",
			"def floorit(v):\n    return v // 2\n\ndef modit(v):\n    return v % 2\n\ndef idn(v):\n    return floorit(v) * 2 + modit(v)\n\nprint(idn(7.5))\nprint(idn(7))\nprint(idn(-3.5))\n", "7.5\n7\n-3.5\n",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if out := captureStdout(t, tc.src); out != tc.want {
				t.Errorf("interpreter: stdout %q, want %q\nsrc: %s", out, tc.want, tc.src)
			}
			res, err := Compile(tc.src)
			if err != nil {
				t.Fatalf("compiled leg refused a program the oracle prints (%v): %s", err, tc.src)
			}
			assertNoForbiddenIR(t, tc.src, res.IR)
			if got := runIR(t, res.IR); got != tc.want {
				t.Errorf("compiled: stdout %q, want %q\nsrc: %s", got, tc.want, tc.src)
			}
		})
	}
}

// TestAPairAnswerHeldByAPositionThatKeepsOneWordStillRefuses is the ladder's honest half, moved here from
// Gap R.166: the door answers an expression, but a *position* that stores one word for a whole value — a
// container's element — has nowhere to put the tag, and ADR 0273's sentence is the right answer there until
// the box exists (Gap R.146's remaining half). Pinning these as refusals is what keeps the nested answers
// above from being bought with a silently truncated digit.
func TestAPairAnswerHeldByAPositionThatKeepsOneWordStillRefuses(t *testing.T) {
	for _, tc := range []struct{ name, src, want string }{
		{
			"a floored pair answer appended to a list",
			"def floorit(v):\n    return v // 2\n\nout = []\nout.append(floorit(5.0))\nout.append(floorit(5))\nprint(out[0])\nprint(out[1])\n",
			"hands back the (payload, tag) pair",
		},
		{
			"a nested pair answer as a list-literal element",
			"def f(v):\n    return [v * 2 + 1]\n\nprint(f(7.5)[0])\n",
			"list literal elements must be integers",
		},
		{
			// A *call*'s pair answer as an arm of an expression used to be ADR 0273's refusal; ADR 0280 made
			// it an answer, so the row lives in the table above. It is kept here as a comment because the
			// neighbour below is a refusal for a different reason, and the two must not be confused.
			"a pair answer handed to another function as its argument",
			"def floorit(v):\n    return v // 2\n\ndef twice(w):\n    return w * 2\n\nprint(twice(floorit(5.0)))\n",
			"hands back the (payload, tag) pair",
		},
		{
			// A pair answer handed to a function as its *argument* is still one word short: the callee
			// takes payload+tag only for arguments the scan marked, and this callee's parameter receives a
			// truncated int if the door opens. Gap R.146 owns the position (ADR 0280 measured it and left
			// the refusal standing rather than answer it wrongly, which is what the ladder forbids).
			// A pair answer handed on as an *argument*: the callee's position was marked for the pair and
			// then closed under it, so the caller is left with a double and one word. ADR 0276's supply gate
			// closes the position; ADR 0280 makes that closure a refusal instead of the truncation that
			// printed `8` for CPython's `10.0` (roadmap Gap R.164).
			"a pair answer bound and handed on as an argument",
			"def twice(v):\n    return v * 2\n\ndef outer(x):\n    y = twice(x)\n    z = twice(y)\n    return z\n\nprint(outer(2.5))\n",
			"considered for the (payload, tag) pair and closed",
		},
		{
			// Reading a pair-bound local in a position that asks for one static number — here the augmented
			// assignment's own read — is Gap R.143's family, pinned for the same reason.
			"a pair answer read back by an augmented assignment",
			"def twice(v):\n    return v * 2\n\ndef outer(x):\n    y = twice(x)\n    y += 1\n    return y\n\nprint(outer(2.5))\n",
			"holds the answer of arithmetic over a slot",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			res, err := Compile(tc.src)
			if err == nil {
				_, out := runIRAllowingTrap(t, res.IR)
				t.Fatalf("the module answered a pair in a one-word position, which Gap R.146 still owns; printed %q\nsrc: %s", out, tc.src)
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Errorf("the refusal must name the position that declined (%q), got %v", tc.want, err)
			}
			if strings.Contains(err.Error(), "LLVM ERROR") || strings.Contains(err.Error(), "verifier") || strings.Contains(err.Error(), "panic") {
				t.Fatalf("%s failed as an IR problem instead of a front-end refusal: %v", tc.name, err)
			}
		})
	}
}
