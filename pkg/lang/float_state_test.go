package lang

import (
	"strings"
	"testing"
)

// The assignment that changes a variable's state from int to float, asked of the compiler itself
// (roadmap L11.6, Gap P.1's `/=` half, Gap R.155, ADR 0274).
//
// `x /= 2` is true division, so the answer is a float whatever the operands were; and `x = 2.5` over a
// name that was born an int is the same change of mind written by hand. Both end with a double where the
// variable's slot is the `i32` its first binding chose, and the pair is what carries it: the double goes
// into a float box, the name is bound to the `(payload, tag)` pair with the float's tag, and every read
// asks the tag.
//
// The shape these rows exist to keep out is the one the toolchain cannot see: a `store double` into a
// four-byte `alloca` verifies, links, runs, and corrupts the variable stored beside it. So the interesting
// assertion here is not only that the number is right — it is that the module contains a `@rt_float_new`
// beside the rebinding and no `store double` into the variable's own slot.

func TestAFloatRebindingBindsThePairRatherThanWideningTheStore(t *testing.T) {
	for _, tc := range []struct {
		name, src, want string
	}{
		{"true division into an int variable", "x = 7\nx /= 2\nprint(x)\n", "3.5\n"},
		{"an even division is still a float", "x = 8\nx /= 2\nprint(x)\n", "4.0\n"},
		{"a double assigned over an int", "y = 12345\nx = 8\nx = 2.5\nprint(x, y)\n", "2.5 12345\n"},
		{"a float summand into an int accumulator", "t = 0\nt += 1.5\nprint(t)\n", "1.5\n"},
		{
			"a divided parameter returns the double",
			"def f(x):\n    x /= 2\n    return x\n\nprint(f(7), f(3))\n",
			"3.5 1.5\n",
		},
		{
			"a divided local inside a body returns the double",
			"def avg(a, b):\n    s = a + b\n    s /= 2\n    return s\n\nprint(avg(7, 8))\n",
			"7.5\n",
		},
		// The neighbours that must not move: the other operators answer an int for two ints, and a
		// variable that was born a float keeps the double slot it always had.
		{"floor division keeps its kind", "x = 7\nx //= 2\nprint(x)\n", "3\n"},
		{"the remainder keeps its kind", "x = 7\nx %= 2\nprint(x)\n", "1\n"},
		{"a sum keeps its kind", "x = 2\nx += 3\nprint(x)\n", "5\n"},
		{"a variable born a float is unchanged", "y = 12345\nx = 8.0\nx = 2.5\nprint(x, y)\n", "2.5 12345\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			res, err := Compile(tc.src)
			if err != nil {
				t.Fatalf("the compiler refused a program the oracle answers: %v", err)
			}
			if out := runIR(t, res.IR); out != tc.want {
				t.Errorf("compiled program printed %q, want %q", out, tc.want)
			}
			if out := captureStdout(t, tc.src); out != tc.want {
				t.Errorf("interpreter printed %q, want %q", out, tc.want)
			}
		})
	}
}

// TestTheFloatRebindingEmitsABoxedPairNotAWideStore is the IR-shape half: the rebinding's own slot may
// never take a `store double`, and the pair has to be written with the float's tag and rooted.
func TestTheFloatRebindingEmitsABoxedPairNotAWideStore(t *testing.T) {
	for _, tc := range []struct {
		name, src, stmt string
	}{
		{"the augmented form", "x = 7\nx /= 2\nprint(x)\n", "%_x_tag"},
		{"the plain rebinding", "y = 12345\nx = 8\nx = 2.5\nprint(x, y)\n", "%_x_tag"},
		{"the augmented float", "t = 0\nt += 1.5\nprint(t)\n", "%_t_tag"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			res, err := Compile(tc.src)
			if err != nil {
				t.Fatalf("refused: %v", err)
			}
			if !hasLine(res.IR, "call i32 @rt_float_new(double") {
				t.Errorf("the rebinding boxed nothing — no @rt_float_new in:\n%s", res.IR)
			}
			if !hasLine(res.IR, "store i32 1, i32* "+tc.stmt) {
				t.Errorf("the rebinding left the float's tag out of %s", tc.stmt)
			}
			// The corruption Gap R.155 measured was this instruction with the variable's own slot on
			// its left — eight bytes into the four-byte allocation the first binding chose.
			for _, line := range strings.Split(res.IR, "\n") {
				trimmed := strings.TrimSpace(line)
				if strings.HasPrefix(trimmed, "store double") && strings.HasSuffix(trimmed, "i32* %_x") {
					t.Errorf("the rebinding stored a double into the int variable's slot:\n%s", trimmed)
				}
			}
		})
	}
}

// TestTheFloatRebindingRootsTheBoxItJustMade pins the GC half of ADR 0274. The slot used to hold an
// immediate and now holds a heap handle; unrooted, the collector recycles the box and the next
// @rt_float_new writes a different double over the value the variable still names — which is what made
// `print(h + 1)` correct and `print(h * 2)` print the first line's answer, at exit 0.
func TestTheFloatRebindingRootsTheBoxItJustMade(t *testing.T) {
	const src = "h = 1\nh /= 3\nprint(h + 1)\nprint(h * 2)\nprint(h + 1)\n"
	const want = "1.3333333333333333\n0.6666666666666666\n1.3333333333333333\n"
	res, err := Compile(src)
	if err != nil {
		t.Fatalf("refused: %v", err)
	}
	if !hasLine(res.IR, "call void @rt_root_put(i32* %_h)") {
		t.Errorf("the rebinding never rooted the variable's slot, so the box under it is collectible")
	}
	if out := runIR(t, res.IR); out != want {
		t.Errorf("the recycled box answered again: got\n%s\nwant\n%s", out, want)
	}
	if out := captureStdout(t, src); out != want {
		t.Errorf("interpreter printed\n%s\nwant\n%s", out, want)
	}
}

// TestAFloatStateVariableRefusesWhereAPositionKeepsOneWord is the honest half: the state reaching a
// position with one word is refused by name, never answered with the payload.
func TestAFloatStateVariableRefusesWhereAPositionKeepsOneWord(t *testing.T) {
	for _, tc := range []struct{ name, src, want string }{
		{
			"a container element",
			"x = 8\nx = 2.5\nprint([x, 1])\n",
			"this position keeps one word",
		},
		{
			"the state changed twice in the same variable",
			"t = 0\nfor i in [4, 9]:\n    t += i / 2\nprint(t)\n",
			"this position keeps one word",
		},
		{
			"a call handed the float-state name",
			"def twice(v):\n    return v * 2\n\nx = 8\nx = 2.5\nprint(twice(x))\n",
			"this position keeps one word",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := Compile(tc.src)
			if err == nil {
				t.Fatalf("%s (%q): compiled; want a refusal", tc.name, tc.src)
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Errorf("%s refused with %q, want it to mention %q", tc.name, err.Error(), tc.want)
			}
			for _, bad := range []string{"LLVM ERROR", "verifier", "Instruction does not dominate"} {
				if strings.Contains(err.Error(), bad) {
					t.Errorf("%s failed as an IR problem instead of a front-end refusal: %v", tc.name, err)
				}
			}
		})
	}
}
