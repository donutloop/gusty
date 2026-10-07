package lang

// pkg/lang/pair_binding_test.go — the arithmetic a slot's answer takes is bound to a name as the
// (payload, tag) pair it already arrived in (roadmap Gap R.138, ADR 0267).
//
// ADR 0265 opened the pair door where the print dispatch asks for a value: `print(xs[0][0] * 2)`
// answers `14`. One statement earlier the same expression asked the ordinary numeric road, which wants
// one i32 with no kind beside it, and refused the slot it cannot see into — exit 1 on a program the
// reference runs. This file pins four things, each a different way to be wrong:
//
//   - the bindings answer, on both legs, for every operator the door serves (`+`, `-`, `*`, the
//     negation) and for a slot the literal never described;
//   - the module shape: the name's value slot and its companion tag slot are both written, and a
//     program that never asks the question does not carry the door;
//   - the retraction half of the rule — a binding that is *not* a pair retires the tag, which is what
//     makes `n = xs[0][0] * 2` followed by `n = [1, 2]` print the list instead of the handle the pair
//     left behind (roadmap Gap R.142, found by the first version of this file);
//   - the positions the pair still does not reach, refused in words that name where the tag came from
//     rather than answered by a payload wearing another object's bits (roadmap Gap R.143).
//
// The three-engine comparison against CPython, through the CLI, lives in
// integration/pair_binding_test.go.

import (
	"strings"
	"testing"
)

const builtList = "xs = []\nxs.append([7, 8])\n"

// TestABoundArithmeticAnswerPrintsLikeTheReferenceOnTheCompiledBackend is the row itself: the expression
// ADR 0265 answers in a print argument is the same expression this statement binds to a name.
func TestABoundArithmeticAnswerPrintsLikeTheReferenceOnTheCompiledBackend(t *testing.T) {
	for _, tc := range []struct{ name, src, want string }{
		{"the row's own shape", builtList + "n = xs[0][0] * 2\nprint(n)\n", "14\n"},
		{"addition", builtList + "n = xs[0][0] + 1\nprint(n)\n", "8\n"},
		{"subtraction", builtList + "n = xs[0][1] - 3\nprint(n)\n", "5\n"},
		{"the negation", builtList + "n = -xs[0][0]\nprint(n)\n", "-7\n"},
		{"both operands are slots", builtList + "n = xs[0][0] + xs[0][1]\nprint(n)\n", "15\n"},
		{
			// The answer's kind is the answer's own: the pair says float, and the printer follows
			// it. Nothing in the binding was rewritten to a double domain to get here.
			"a float slot keeps the float",
			"xs = []\nxs.append([7.5, 8])\nn = xs[0][0] * 2\nprint(n)\n", "15.0\n",
		},
		{
			// int beside float: one slot is an int and the arithmetic lifts it, so the pair that
			// travels says which answer came out, per binding.
			"two bindings of two families",
			builtList + "a = xs[0][0] + 1\nb = xs[0][0] * 2.5\nprint(a, b)\n", "8 17.5\n",
		},
		{"a bool slot is a number", "xs = []\nxs.append([True, 2])\nn = xs[0][0] + 1\nprint(n)\n", "2\n"},
		{"a dict value by key", "d = {}\nd[\"k\"] = 40\nn = d[\"k\"] + 2\nprint(n)\n", "42\n"},
		{
			"three levels deep",
			"xs = []\nxs.append([[7, 8]])\nn = xs[0][0][1] - 1\nprint(n)\n", "7\n",
		},
		{
			"two slots multiplied, the answer a whole number",
			builtList + "n = xs[0][0] * xs[0][1]\nprint(n)\n", "56\n",
		},
		{
			// The operand the compiler cannot see is on the left and the double is on the right:
			// the pair says float, and the name carries that answer to the printer.
			"a slot times a double literal",
			builtList + "b = xs[0][0] * 2.5\nprint(b)\n", "17.5\n",
		},
		{"bound between two prints", builtList + "print(0)\nn = xs[0][0] * 2\nprint(n)\nprint(9)\n", "0\n14\n9\n"},
		{"bound inside an if arm", builtList + "if 1 > 0:\n    n = xs[0][0] * 2\n    print(n)\n", "14\n"},
		{"bound inside a while arm", builtList + "i = 0\nwhile i < 1:\n    n = xs[0][0] * 2\n    print(n)\n    i = 1\n", "14\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			out := captureStdout(t, tc.src)
			if out != tc.want {
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

// TestABoundArithmeticAnswerWritesBothSlots is the module half: a pair is two words, and a binding that
// writes only the payload is the bug ADR 0187 closed for container slots.
func TestABoundArithmeticAnswerWritesBothSlots(t *testing.T) {
	src := builtList + "n = xs[0][0] * 2\nprint(n)\n"
	ir := slotIR(t, src)
	for _, want := range []string{
		"@rt_num_arith",         // the arithmetic was asked of the object
		"@rt_lift_num",          // and the lift lives in the one word that holds both families
		"%_n_tag = alloca i32",  // the name's companion slot exists
		"i32* %_n\n",            // the payload was written to the name's word...
		"i32* %_n_tag\n",        // ...and the tag with it
		"@rt_print_mixed_value", // and print asks the tag the binding wrote
	} {
		if !strings.Contains(ir, want) {
			t.Errorf("the binding is missing %q", want)
		}
	}
	// A float answer is a handle on a box, not a double written into a slot sized for an int:
	// `store double` beside the pair is the Gap R.98 emission, and the module llc rejects.
	if tail := ir[strings.LastIndex(ir, "@rt_num_arith"):]; strings.Contains(tail, "store double %") {
		t.Error("the pair road stored a double into the name's word")
	}
}

// TestAPlainBindingDoesNotRideAlongWithTheDoor is the cost half, the same rule ADR 0173/ADR 0192 state
// for the float and heap blocks: a program that never asks the object a question must not carry the
// door that asks it.
func TestAPlainBindingDoesNotRideAlongWithTheDoor(t *testing.T) {
	ir := slotIR(t, "xs = [1, 2]\nn = xs[0] * 2\nprint(n)\nm = 3 * 4\nprint(m)\n")
	for _, absent := range []string{"@rt_num_arith", "@rt_lift_num", "@rt_num_bad"} {
		if strings.Contains(ir, absent) {
			t.Errorf("a program that asks the compiler, not the object, carries %s", absent)
		}
	}
}

// TestANonPairBindingRetiresTheTag is the retraction half of the rule, and the reason it had to land in
// the same commit as the binding: ADR 0172 decides print, truthiness and equality from a variable's
// *latest* binding, and the container, comprehension, lambda, module and float bindings all return
// early. With the record left standing, `n = xs[0][0] * 2` then `n = [1, 2]` printed `2` — the heap
// handle — and `n = 2.5` printed `0` (roadmap Gap R.142).
func TestANonPairBindingRetiresTheTag(t *testing.T) {
	for _, tc := range []struct{ name, src, want string }{
		{"a plain number retires it", builtList + "n = xs[0][0] * 2\nn = 3\nprint(n)\n", "3\n"},
		{"a float retires it", builtList + "n = xs[0][0] * 2\nn = 2.5\nprint(n)\n", "2.5\n"},
		{"a text retires it", builtList + "n = xs[0][0] * 2\nn = \"hi\"\nprint(n)\n", "hi\n"},
		{"a list retires it", builtList + "n = xs[0][0] * 2\nn = [1, 2]\nprint(n)\n", "[1, 2]\n"},
		{"a set retires it", builtList + "n = xs[0][0] * 2\nn = {5, 6}\nprint(n)\n", "{5, 6}\n"},
		{"a dict retires it", builtList + "n = xs[0][0] * 2\nn = {\"a\": 1}\nprint(n)\n", "{'a': 1}\n"},
		{"a comprehension retires it", builtList + "n = xs[0][0] * 2\nn = [v for v in [1, 2]]\nprint(n)\n", "[1, 2]\n"},
		{"and the pair comes back on the next binding", builtList + "n = xs[0][0] * 2\nn = 3\nn = xs[0][1] * 2\nprint(n)\n", "16\n"},
		{"a loop variable's tag retires the same way", "xs = [1.5, \"a\"]\nfor v in xs:\n    print(v)\n\nv = [1, 2]\nprint(v)\n", "1.5\na\n[1, 2]\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			out := captureStdout(t, tc.src)
			if out != tc.want {
				t.Errorf("interpreter: stdout %q, want %q\nsrc: %s", out, tc.want, tc.src)
			}
			res, err := Compile(tc.src)
			if err != nil {
				t.Fatalf("compiled leg refused the program (%v): %s", err, tc.src)
			}
			assertNoForbiddenIR(t, tc.src, res.IR)
			if got := runIR(t, res.IR); got != tc.want {
				t.Errorf("compiled: stdout %q, want %q — the tag outlived its binding\nsrc: %s", got, tc.want, tc.src)
			}
		})
	}
}

// TestABoundAnswerSurvivesACollection is the handle half of a float answer: the payload a float pair
// carries is the handle of a @float_box, and the tagged binding registers no root for it — the same
// footing the loop-variable binding has had since ADR 0185. A storm of short-lived containers between
// the binding and the print is what would notice, so the row runs one (roadmap L7.1/L7.2 own precise
// rooting of every slot; this is the measurement that keeps it visible).
func TestABoundAnswerSurvivesACollection(t *testing.T) {
	stress := "xs = []\nxs.append([7.5, 8])\nn = xs[0][0] * 2\n" +
		"for i in range(200):\n    ys = [i, [i, i], \"t\"]\nprint(n)\n"
	if out := captureStdout(t, stress); out != "15.0\n" {
		t.Errorf("interpreter: stdout %q, want the answer the pair carried\nsrc: %s", out, stress)
	}
	res, err := Compile(stress)
	if err != nil {
		t.Fatalf("compiled leg refused the program: %v", err)
	}
	if got := runIR(t, res.IR); got != "15.0\n" {
		t.Errorf("compiled: stdout %q, want 15.0 — a collection reclaimed the float box\nsrc: %s", got, stress)
	}
}

// TestAPairBoundNameIsReadWhereverANumberIsAsked is roadmap Gap R.143 paid: the positions that ask for
// one static number now ask the pair. A name the arithmetic door bound is provably int-or-float — the
// only road that wrote it either raised or stored one of those two tags — so a position can lift it into
// the word that holds both families (@rt_lift_num) or hand the pair to the printer (@rt_str_of_value),
// which is the same helper print and str already agree on. Reading the payload alone would still be a
// number wearing another object's bits, which is what the refusal below is for.
func TestAPairBoundNameIsReadWhereverANumberIsAsked(t *testing.T) {
	for _, tc := range []struct{ name, src, want string }{
		{"an operand of a sum", builtList + "n = xs[0][0] * 2\nprint(n + 1)\n", "15\n"},
		{"the sum of two of them", builtList + "n = xs[0][0] * 2\nm = n + n\nprint(m)\n", "28\n"},
		{"negated", builtList + "n = xs[0][0] * 2\nprint(-n)\n", "-14\n"},
		{"handed to abs", builtList + "n = xs[0][0] * 2\nprint(abs(n))\n", "14\n"},
		{"handed to abs with the sign already gone", builtList + "n = xs[0][0] * -2\nprint(abs(n))\n", "14\n"},
		{"asked for its truth", builtList + "n = xs[0][0] * 2\nif n:\n    print(\"yes\")\n", "yes\n"},
		{"a zero answers false", builtList + "n = xs[0][0] * 0\nif n:\n    print(\"yes\")\nelse:\n    print(\"no\")\n", "no\n"},
		{"a while head", builtList + "n = xs[0][0] * 2\nwhile n > 0:\n    print(n)\n    n = 0\n", "14\n"},
		{"ordered against a number", builtList + "n = xs[0][0] * 2\nprint(n > 13)\nprint(13 > n)\n", "True\nFalse\n"},
		{"a condition's head", builtList + "n = xs[0][0] * 2\nprint(1 if n > 1 else 0)\n", "1\n"},
		{"interpolated", builtList + "n = xs[0][0] * 2\nprint(f\"{n}\")\n", "14\n"},
		{"interpolated with text around it", builtList + "n = xs[0][0] * 2\nprint(f\"v={n}!\")\n", "v=14!\n"},
		{"str() of it", builtList + "n = xs[0][0] * 2\nprint(str(n))\n", "14\n"},
		{"repr() of it", builtList + "n = xs[0][0] * 2\nprint(repr(n))\n", "14\n"},
		{"str() of the float family", builtList + "n = xs[0][0] * 2.5\nprint(str(n))\n", "17.5\n"},
		{"augmented assignment onto it", builtList + "n = xs[0][0] * 2\nn += 1\nprint(n)\n", "15\n"},
		{"augmented product onto it", builtList + "n = xs[0][0] * 2\nn *= 2\nprint(n)\n", "28\n"},
		{"the float family keeps its digits", builtList + "n = xs[0][0] * 2.5\nprint(n)\nprint(n > 17)\n", "17.5\nTrue\n"},
		// `and` hands back an operand, and a pair-bound name is an operand the run time just described:
		// the print door selects the (payload, tag) pair and the tag says which rendering runs, so this
		// line is CPython's `3` on all three engines (roadmap Gap R.147, ADR 0269 — it used to be the
		// refusal in the table below, and before that the verdict `1`).
		{"the operand of and", builtList + "n = xs[0][0] * 2\nprint(n and 3)\nprint(1 and n)\n", "3\n14\n"},
		{"the operand of or", builtList + "n = xs[0][0] * 0\nprint(n or 9)\n", "9\n"},
	} {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			if out := captureStdout(t, tc.src); out != tc.want {
				t.Errorf("interpreter: stdout %q, want %q\nsrc: %s", out, tc.want, tc.src)
			}
			res, err := Compile(tc.src)
			if err != nil {
				t.Fatalf("compiled leg refused a program the reference answers: %v\nsrc: %s", err, tc.src)
			}
			if got := runIR(t, res.IR); got != tc.want {
				t.Errorf("compiled: stdout %q, want %q\nsrc: %s", got, tc.want, tc.src)
			}
		})
	}
}

// TestThePairRoadCarriesTheLiftAndTheRenderer asks the module rather than the answer: those positions are
// answered by two runtime doors and nothing else, and a program that binds no pair must not pay for
// either (the same argument numeric_slot_arith_test.go makes about @rt_num_arith).
func TestThePairRoadCarriesTheLiftAndTheRenderer(t *testing.T) {
	asked := builtList + "n = xs[0][0] * 2\nif n:\n    print(1)\nprint(f\"{n}\")\nprint(n > 1)\n"
	res, err := Compile(asked)
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	for _, want := range []string{"@rt_lift_num", "@rt_str_of_value", "fcmp one double", "fcmp ogt double"} {
		if !strings.Contains(res.IR, want) {
			t.Errorf("the module does not carry %s; the position was answered without the pair's own door",
				want)
		}
	}
	plain := "v = 1\nprint(v)\n"
	res2, err := Compile(plain)
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	for _, absent := range []string{"@rt_lift_num", "@rt_str_of_value"} {
		if strings.Contains(res2.IR, absent) {
			t.Errorf("a program that binds no pair carries %s", absent)
		}
	}
}

// TestThePairRoadStillRefusesThePositionsThatTakeAValue keeps the remaining road honest: a position that
// takes a whole *value* — a builtin's argument, a container's element — has nowhere to put a tag, and says
// so in words rather than reading the payload alone (roadmap Gap R.146, the same missing word Gap R.139
// names on the calling side). The `and` row that used to be here answers since ADR 0269 and moved up to the
// parity table; a row that stops refusing has to move, not disappear. The list-element row moved to
// TestAPairBoundNameEntersAContainerByWayOfItsTag once the builders learned to ask for the tag (ADR 0306).
func TestThePairRoadStillRefusesThePositionsThatTakeAValue(t *testing.T) {
	for _, tc := range []struct{ name, src string }{
		{"handed to min", builtList + "n = xs[0][0] * 2\nprint(min(n, 3))\n"},
	} {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			_, err := Compile(tc.src)
			if err == nil {
				t.Fatalf("%q compiled; this position takes a value, not a pair (roadmap Gap R.146)", tc.src)
			}
			msg := err.Error()
			for _, want := range []string{"holds the answer of arithmetic over a slot", "roadmap L11.1"} {
				if !strings.Contains(msg, want) {
					t.Errorf("refused without naming the shape: %q does not mention %q", msg, want)
				}
			}
			if strings.Contains(msg, "LLVM ERROR") || strings.Contains(msg, "verifier") {
				t.Fatalf("%q failed as an IR problem instead of a front-end refusal: %v", tc.src, err)
			}
		})
	}
}

// TestThePairGateIsTheSameDoorInTheBindingAsInThePrint is ADR 0265's gate asked one statement later:
// where the reference would answer a joined text or a repeated container rather than a number or a
// raise, the door stays shut — and the program keeps the refusal it always had instead of a TypeError
// the reference never raises (Gap R.82).
func TestThePairGateIsTheSameDoorInTheBindingAsInThePrint(t *testing.T) {
	for _, tc := range []struct{ name, src, want string }{
		{
			"text beside a slot under +",
			"xs = []\nxs.append(\"hi\")\nn = xs[0] + 1\nprint(n)\n",
			"concatenating a string with a value that is not a string",
		},
		{
			"a value with no spelling",
			"def f():\n    return [7, 8]\n\nxs = []\nxs.append(f())\nn = xs[0][0] * 2\nprint(n)\n",
			"cannot reach into",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			slotRefusal(t, tc.src, tc.want)
		})
	}
}

// TestTheRaiseOfABoundAnswerLeavesThroughTheEmittedDoor is the trap run rather than the trap emitted:
// the arithmetic the binding performs raises the reference's own sentence, and `except` reaches it,
// because the raise is written by the emitted code at the statement (ADR 0228).
func TestTheRaiseOfABoundAnswerLeavesThroughTheEmittedDoor(t *testing.T) {
	for _, tc := range []struct{ name, src, want string }{
		{
			"a text slot under the negation",
			"xs = []\nxs.append(\"hi\")\ntry:\n    n = -xs[0]\n    print(n)\nexcept TypeError:\n    print(\"caught\")\n",
			"caught\n",
		},
		{
			"an answer past the compiled int word",
			builtList + "try:\n    n = xs[0][0] * 1000000000\n    print(n)\nexcept OverflowError:\n    print(\"caught\")\n",
			"caught\n",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			res, err := Compile(tc.src)
			if err != nil {
				t.Fatalf("compiled leg failed: %v", err)
			}
			if !strings.Contains(res.IR, "@rt_num_arith") {
				t.Errorf("the trap did not go through the door: %s", tc.src)
			}
			if out := runIR(t, res.IR); out != tc.want {
				t.Errorf("compiled printed %q, want the handler's %q\nsrc: %s", out, tc.want, tc.src)
			}
		})
	}
}
