package integration

import (
	"strings"
	"testing"
)

// An int that meets `/=` becomes a float, and an int variable handed a double by a
// later assignment becomes one too (roadmap L11.6, Gap P.1's `/=`, Gap R.155; ADR 0274).
//
//     y = 12345
//     x = 8
//     x = 2.5          # the variable's own binding changes state
//     print(x, y)      # 2.5 12345
//
// and
//
//     x = 7
//     x /= 2           # true division: the answer is a float whatever arrives
//     print(x)         # 3.5
//
// Both statements used to end with the same eight instructions — a `store double` into
// the four-byte `alloca` the variable's first binding chose. LLVM's module verifier
// cannot see through an opaque pointer, so the module verified, `llc` accepted it, and
// the *neighbour* paid: `y` printed `1074003968` with the exit code of success. That is
// the corruption Gap R.155 files, and it is why this row could not be closed by choosing
// the double domain alone — the first measurement of `/=` did exactly that, printed `3`
// for `7 /= 2` because the store also destroyed the flag the integer road consulted, and
// passed.
//
// The pair is what a change of state needs, and it is the pair the rest of the family
// already carries (ADR 0264, ADR 0267, ADR 0269, ADR 0273): the double goes into a float
// box, the variable is bound to the `(payload, tag)` pair with the float's tag, and every
// later read asks the tag. A variable that never changes state keeps the plain slot and
// the plain load it had — the `fibonacci` and `function_calls` benchmarks emit no box at
// all.
//
// The probe prints on both legs. The refusals below are the door's honest half:
// each names the position that keeps one word, and none of them is a wrong number.

const floatStateProbe = "probe_int_state_becomes_float"

var floatStateWant = strings.Join([]string{
	"2.5 12345",
	"3.5",
	"0.3333333333333333",
	"1.3333333333333333 0.6666666666666666",
	"True True",
	"1.5",
	"2.0",
	"1.25",
	"big",
	"0.5",
	"0.5",
	"",
}, "\n")

// TestIntStateBecomesFloatMatchesCPython is the row paid, at the CLI: `/=`, and a double
// assigned over an int, leave the variable holding a float on both legs and next to its
// neighbours.
func TestIntStateBecomesFloatMatchesCPython(t *testing.T) {
	src := readProgram(t, floatStateProbe+".gy")
	path := writeSrc(t, t.TempDir(), "int_state_becomes_float.gy", src)
	if py, ok := cpythonOut(t, path); ok && py != floatStateWant {
		t.Fatalf("the expectation is not CPython's: got\n%s\nwant\n%s", py, floatStateWant)
	}
	for _, engine := range cliEngines {
		out, code := cliRunCode(t, engine, path)
		if code == 2 {
			t.Fatalf("the %s leg rejected the compiler's own module (ADR 0166):\n%s", engine, cliRun(t, engine, path))
		}
		if code != 0 {
			t.Fatalf("the %s leg exited %d: %s", engine, code, cliRun(t, engine, path))
		}
		if out != floatStateWant {
			t.Errorf("the %s leg printed\n%s\nwant\n%s", engine, out, floatStateWant)
		}
	}
}

// TestAFloatRebindingDoesNotReachIntoItsNeighbour is Gap R.155, stated as the program that
// was broken: the double written over an int variable's slot reached four bytes past the
// allocation, and the variable beside it answered a different number. The old shape
// verified and printed `1074003968`; the exit code was 0 either way, which is the whole
// reason the row had to be measured by its answer rather than by its diagnostics.
func TestAFloatRebindingDoesNotReachIntoItsNeighbour(t *testing.T) {
	for _, tc := range []struct{ name, src, want string }{
		{
			"the neighbour keeps its number",
			"y = 12345\nx = 8\nx = 2.5\nprint(x, y)\n",
			"2.5 12345\n",
		},
		{
			"two neighbours keep theirs",
			"a = 1\nb = 2\nc = 3\nb = 2.5\nprint(a, b, c)\n",
			"1 2.5 3\n",
		},
		{
			"true division keeps the neighbour too",
			"y = 12345\nx = 7\nx /= 2\nprint(x, y)\n",
			"3.5 12345\n",
		},
		{
			"a float that was born a float is unchanged",
			"y = 12345\nx = 8.0\nx = 2.5\nprint(x, y)\n",
			"2.5 12345\n",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			path := writeSrc(t, t.TempDir(), "float_rebind.gy", tc.src)
			if py, ok := cpythonOut(t, path); ok && py != tc.want {
				t.Fatalf("the expectation is not CPython's: got %q want %q", py, tc.want)
			}
			for _, engine := range cliEngines {
				out, code := cliRunCode(t, engine, path)
				if code != 0 {
					t.Fatalf("the %s leg exited %d: %s", engine, code, cliRun(t, engine, path))
				}
				if out != tc.want {
					t.Errorf("the %s leg printed %q, want %q", engine, out, tc.want)
				}
			}
		})
	}
}

// TestTrueDivisionChoosesTheFloatDomain is Gap P.1's `/=` half at the CLI: the operator
// decides the domain, not the operands, so `8 /= 2` answers `4.0` and `1 /= 3` answers the
// quotient instead of the truncated integer. The `//=` and `%=` rows are the neighbours
// that must NOT move — an int floor-divide and an int remainder are still ints.
func TestTrueDivisionChoosesTheFloatDomain(t *testing.T) {
	for _, tc := range []struct{ name, src, want string }{
		{"even division is still a float", "x = 8\nx /= 2\nprint(x)\n", "4.0\n"},
		{"the quotient survives", "x = 1\nx /= 3\nprint(x)\n", "0.3333333333333333\n"},
		{"a float divided by an int keeps its kind", "x = 2.5\nx /= 2\nprint(x)\n", "1.25\n"},
		{"the other operators keep their kinds", "x = 7\nx //= 2\nprint(x)\n", "3\n"},
		{"the remainder keeps its kind", "x = 7\nx %= 2\nprint(x)\n", "1\n"},
		{"a sum keeps its kind", "x = 2\nx += 3\nprint(x)\n", "5\n"},
		{"a float summand brings the double", "t = 0\nt += 1.5\nprint(t)\n", "1.5\n"},
		{
			"the answer of a divided int reads as a float everywhere",
			"x = 7\nx /= 2\nprint(x, x + 1, x * 2, x < 2, str(x))\n",
			"3.5 4.5 7.0 False 3.5\n",
		},
		// `/=` inside a function body, where the change of state is the body's own. The return word is
		// read off what the body does (`scanRebinds` records the quotient as the name's newest value), so
		// the callee asks for the double the way ADR 0254 reads it from a `return` expression.
		{
			"a divided parameter comes back a float",
			"def f(x):\n    x /= 2\n    return x\n\nprint(f(7), f(3))\n",
			"3.5 1.5\n",
		},
		{
			"a local accumulator divided in a body",
			"def avg(a, b):\n    s = a + b\n    s /= 2\n    return s\n\nprint(avg(7, 8))\n",
			"7.5\n",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			path := writeSrc(t, t.TempDir(), "div_domain.gy", tc.src)
			if py, ok := cpythonOut(t, path); ok && py != tc.want {
				t.Fatalf("the expectation is not CPython's: got %q want %q", py, tc.want)
			}
			for _, engine := range cliEngines {
				out, code := cliRunCode(t, engine, path)
				if code == 2 {
					t.Fatalf("the %s leg rejected the compiler's own module (ADR 0166):\n%s", engine, cliRun(t, engine, path))
				}
				if code != 0 {
					t.Fatalf("the %s leg exited %d: %s", engine, code, cliRun(t, engine, path))
				}
				if out != tc.want {
					t.Errorf("the %s leg printed %q, want %q", engine, out, tc.want)
				}
			}
		})
	}
}

// TestAFloatStateVariableEntersAListByWayOfItsTagAtTheCLI is this door's other half, and it
// changed sides when the container builders learned to ask (ADR 0306): an element is no longer a
// position that keeps one word, so the shape is no longer refused. What the row still rules out is
// the payload answering in place of the value — a float box's handle printed as an int where the
// reference prints a double (Gap R.132, ADR 0273's record) — which is why it compares against the
// reference instead of a pinned refusal sentence. The int-to-float rebinding that made the shape
// interesting (`x = 8` then `x = 2.5`) is in the program, not in the expectation.
func TestAFloatStateVariableEntersAListByWayOfItsTagAtTheCLI(t *testing.T) {
	for _, tc := range []struct{ name, src, want string }{
		{
			"the float-state variable the container element door is asked about",
			"x = 8\nx = 2.5\nprint([x, 1])\n",
			"[2.5, 1]\n",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			// The answer is what the reference prints. What the box's payload would have answered —
			// an integer handle where the reference prints a double — is the failure this row exists
			// to catch, and it is only recognisable against that answer.
			compiledAnswersExitZero(t, tc.src, tc.want)
		})
	}
}
