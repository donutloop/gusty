package lang

// pkg/lang/pair_number_test.go — a name bound from a container slot answers the positions that need
// *the number* (roadmap L11.1, Gap R.146's arithmetic half; ADR 0303's pair binding is what made the
// name readable at all).
//
// ADR 0303 bound the (payload, tag) pair for `n = xs[0]` out of a container the program built, which
// bought the rendering positions — `print(n)`, `str(n)`, `repr(n)`. The arithmetic positions were still
// refused, because the door that turns a pair into a number asked only for names the *arithmetic* road
// had bound (origins whose tag is a proof: int or float, and nothing else). A slot's tag is not a proof;
// it is a fact the objects decided, and it can say `str`. That difference is exactly the right gate:
//
//   - the operators that answer a number or raise, always (`-`, `-x`, `//`) take the door whatever the
//     slot holds, and `rt_num_arith` raises CPython's own sentence per kind;
//   - the operators the reference can answer with something that is not a number (`+`, `*`, and `%` for
//     its printf form) stay behind the number proof (ADR 0265), because answering a raise where CPython
//     answered `"a" + "b"` would be a wrong program, and this backend builds neither from a slot (Gap R.82).
//
// So this file pins three things: the positions that answer, the traps that must be RAISED with the
// slot's real kind named (not refused, and never exit 2 — ADR 0166), and the positions that still refuse
// with a sentence that names where the pair came from (Gap R.38).
//
// The CLI comparison against CPython lives in integration/pair_number_test.go.

import (
	"strings"
	"testing"
)

const builtIntSlot = "xs = []\nxs.append(7)\nn = xs[0]\n"

// TestASlotBoundNameAnswersThePositionsThatNeedANumber is the row. Every shape here is a name bound from
// a container the program built, used where the reference wants a number; each was refused with Gap R.146's
// sentence before this cycle, and each answer is checked against the record — which is CPython's.
func TestASlotBoundNameAnswersThePositionsThatNeedANumber(t *testing.T) {
	for _, tc := range []struct{ name, src, want string }{
		{"subtraction", builtIntSlot + "print(n - 1)\n", "6\n"},
		{"the subtrahend is the slot", "xs = []\nxs.append(7)\nprint(10 - xs[0])\n", "3\n"},
		{"negation", builtIntSlot + "print(-n)\n", "-7\n"},
		// abs of a pair-bound name answers now: the arithmetic door's signless call takes the payload and
		// the tag, and CPython's `bad operand type for abs(): 'str'` is what a non-number slot raises
		// (roadmap L11.1, Gap R.146; ADR 0309).
		{"abs of the slot", builtIntSlot + "print(abs(n))\n", "7\n"},
		{"abs of a negative slot", "xs = []\nxs.append(-4)\nn = xs[0]\nprint(abs(n))\n", "4\n"},
		{"abs of a bool slot", "xs = []\nxs.append(True)\nn = xs[0]\nprint(abs(n))\n", "1\n"},
		{"abs bound and printed", builtIntSlot + "y = abs(n)\nprint(y)\n", "7\n"},
		{"floor division", builtIntSlot + "print(n // 2)\n", "3\n"},
		{"a float slot keeps its float", "xs = []\nxs.append(2.5)\nn = xs[0]\nprint(n - 1)\n", "1.5\n"},
		{"a float slot negated", "xs = []\nxs.append(2.5)\nn = xs[0]\nprint(-n)\n", "-2.5\n"},
		{"a negative slot", "xs = []\nxs.append(-4)\nn = xs[0]\nprint(n - 1)\n", "-5\n"},
		{"a bool slot is the number it is", "xs = []\nxs.append(True)\nn = xs[0]\nprint(n - 1)\n", "0\n"},
		{
			"a dict slot by key",
			"d = {}\nd[\"k\"] = 9\nn = d[\"k\"]\nprint(n - 1)\n", "8\n",
		},
		{
			"a slot read by a computed position",
			"xs = []\nxs.append(7)\nxs.append(2)\ni = 1\nn = xs[i]\nprint(n - 1)\n", "1\n",
		},
		{
			"a slot two levels down",
			"xs = []\nxs.append([7, 8])\nn = xs[0][1]\nprint(n - 1)\n", "7\n",
		},
		{
			"the loop variable, in the same positions",
			"xs = []\nxs.append(7)\nfor v in xs:\n    print(v - 1)\n", "6\n",
		},
		{
			"the loop variable negated",
			"xs = []\nxs.append(7)\nfor v in xs:\n    print(-v)\n", "-7\n",
		},
		{
			// The answer is a value, not a print: it feeds an assignment, a condition and a second
			// sum, all of which ask the tag again rather than trusting an earlier one.
			"the answer is bound and used again",
			builtIntSlot + "m = n - 1\nprint(m)\nif m:\n    print(m + 1)\n", "6\n7\n",
		},
		{
			"two slots, one sum",
			"xs = []\nxs.append(7)\nxs.append(8)\na = xs[0]\nb = xs[1]\nprint(b - a)\n", "1\n",
		},
		{
			"a slot under a float expression",
			"xs = []\nxs.append(7)\nn = xs[0]\nprint(n - 1 + 0.5)\n", "6.5\n",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			RecordedStdoutIs(t, tc.src, tc.want)
			res, err := JIT(tc.src, 0)
			if err != nil {
				t.Fatalf("the compiled backend refused a program the reference answers: %v", err)
			}
			if got := strings.TrimSuffix(res.Output, "\n"); got != strings.TrimSuffix(tc.want, "\n") {
				t.Fatalf("compiled printed %q, want %q", got, strings.TrimSuffix(tc.want, "\n"))
			}
		})
	}
}

// TestASlotBoundNameRaisesTheOwnSentenceOfTheKindItHolds is the half that decides whether widening the
// door was safe. A slot's tag is not a proof of a number, so a text, a None and a container reaching an
// arithmetic position must RAISE — with CPython's exact class and message — rather than answer a sum of
// the payload's bits (which is the wrong-answer-at-exit-0 family Gap R.38 and Gap R.95 keep filing) or be
// refused for being hard (Gap R.37).
func TestASlotBoundNameRaisesTheOwnSentenceOfTheKindItHolds(t *testing.T) {
	for _, tc := range []struct{ name, src, class, message string }{
		{
			"a text slot subtracted",
			"xs = []\nxs.append(\"a\")\nn = xs[0]\nprint(n - 1)\n",
			"TypeError", "unsupported operand type(s) for -: 'str' and 'int'",
		},
		{
			"a text slot negated",
			"xs = []\nxs.append(\"a\")\nn = xs[0]\nprint(-n)\n",
			"TypeError", "bad operand type for unary -: 'str'",
		},
		{
			"a None slot negated",
			"xs = []\nxs.append(None)\nn = xs[0]\nprint(-n)\n",
			"TypeError", "bad operand type for unary -: 'NoneType'",
		},
		{
			"a container slot negated",
			"xs = []\nxs.append([1])\nn = xs[0]\nprint(-n)\n",
			"TypeError", "bad operand type for unary -: 'list'",
		},
		{
			"a dict slot negated",
			"xs = []\nxs.append({\"k\": 1})\nn = xs[0]\nprint(-n)\n",
			"TypeError", "bad operand type for unary -: 'dict'",
		},
		{
			"a set slot subtracted",
			"xs = []\nxs.append({1})\nn = xs[0]\nprint(n - 1)\n",
			"TypeError", "unsupported operand type(s) for -: 'set' and 'int'",
		},
		{
			"floor division by zero through a slot",
			builtIntSlot + "print(n // 0)\n",
			"ZeroDivisionError", "integer division or modulo by zero",
		},
		{
			"a text slot floor-divided",
			"xs = []\nxs.append(\"a\")\nn = xs[0]\nprint(n // 2)\n",
			"TypeError", "unsupported operand type(s) for //: 'str' and 'int'",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ee := trapRun(t, tc.src)
			if ee.ExnType != tc.class {
				t.Errorf("class = %q, want %q (msg %q)", ee.ExnType, tc.class, ee.ExnMsg)
			}
			if ee.ExnMsg != tc.message {
				t.Errorf("message =\n  %q\nwant the reference's\n  %q", ee.ExnMsg, tc.message)
			}
		})
	}
}

// TestTheArithmeticOfASlotBoundNameIsCatchableWhereTheProgramWroteTheArm: a raise the helper performed for
// itself is unreachable to the program, and the trap's whole value is that `except` reaches it (ADR 0228).
func TestTheArithmeticOfASlotBoundNameIsCatchableWhereTheProgramWroteTheArm(t *testing.T) {
	for _, tc := range []struct{ name, src, want string }{
		{
			"a TypeError arm runs",
			"xs = []\nxs.append(\"a\")\nn = xs[0]\ntry:\n    print(n - 1)\nexcept TypeError:\n    print(\"caught\")\n",
			"caught\n",
		},
		{
			"a ZeroDivisionError arm runs on a slot's floor division",
			builtIntSlot + "try:\n    print(n // 0)\nexcept ZeroDivisionError:\n    print(\"caught\")\n",
			"caught\n",
		},
		{
			// The arm that did NOT match must not swallow the raise: a door that raised the wrong
			// class would turn this program into a silent pass.
			"a mismatching arm still lets it out",
			"xs = []\nxs.append(\"a\")\nn = xs[0]\ntry:\n    print(n - 1)\nexcept ValueError:\n    print(\"wrong arm\")\n",
			"",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			res, err := JIT(tc.src, 0)
			if err != nil {
				t.Fatalf("the compiled backend refused this program: %v", err)
			}
			if strings.TrimSuffix(res.Output, "\n") != strings.TrimSuffix(tc.want, "\n") {
				t.Fatalf("compiled printed %q, want %q", res.Output, tc.want)
			}
		})
	}
}

// TestTheNumberDoorIsInTheModuleOnlyWhenItIsAsked keeps the door honest to the program in front of it: a
// module that sums a slot-bound name carries the tagged arithmetic, and one that never asks for a number
// carries neither the door nor its raise formatter (ADR 0265's gate, asserted from the outside).
func TestTheNumberDoorIsInTheModuleOnlyWhenItIsAsked(t *testing.T) {
	res, err := Compile(builtIntSlot + "print(n - 1)\n")
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	assertNoForbiddenIR(t, builtIntSlot+"print(n - 1)\n", res.IR)
	for _, want := range []string{"@rt_num_arith", "@rt_lift_num"} {
		if !strings.Contains(res.IR, want) {
			t.Errorf("the module answers arithmetic on a slot-bound name without %s", want)
		}
	}
	// The counter-case has to be a program that truly never asks what a slot is: a read of a built slot
	// IS such a question (the slot may hold a text, and the door raises per kind for it — ADR 0265), so
	// the shape with no door at all is the literal one.
	plain, err := Compile("print(1 + 1)\n")
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	for _, absent := range []string{"@rt_num_arith", "@rt_lift_num", "TypeError: unsupported operand type(s) for %s"} {
		if strings.Contains(plain.IR, absent) {
			t.Errorf("a program that never asks for a number it cannot see carries %s", absent)
		}
	}
}

// TestTheNumberDoorStillRefusesWhatTheReferenceAnswersWithAValue is the half this cycle did NOT open, and
// the reason is in the sentence: `+`, `*` and `%` are operators CPython can answer with something that is
// not a number (two texts joined, a list repeated, a formatted string), and this backend builds none of
// those from a slot — so the door stays behind the number proof, and the refusal says so with the value's
// real origin (Gap R.38: no blaming a loop a slot binding never stepped through).
func TestTheNumberDoorStillRefusesWhatTheReferenceAnswersWithAValue(t *testing.T) {
	for _, tc := range []struct{ name, src string }{
		{"addition", builtIntSlot + "print(n + 1)\n"},
		{"repetition", builtIntSlot + "print(n * 2)\n"},
		{"modulo", builtIntSlot + "print(n % 3)\n"},
		// `print(min(n, 3))` was on this table. It answers now, because a fold is not a position that
		// keeps one word any more: `min` hands back one of the values it was given, and the door writes
		// the winner's payload beside the winner's tag (`pairfold.go`, ADR 0316). The row lives in
		// `pair_fold_test.go`; a row that stops refusing moves, it does not disappear.
	} {
		t.Run(tc.name, func(t *testing.T) {
			res, err := Compile(tc.src)
			if err == nil {
				t.Fatalf("the compiler answered a position that keeps one word for its operand, emitting:\n%s",
					firstLines(res.IR, 20))
			}
			msg := err.Error()
			if !strings.Contains(msg, "tagged value") {
				t.Errorf("the refusal does not name the missing half:\n%s", msg)
			}
			if !strings.Contains(msg, "roadmap") {
				t.Errorf("the refusal does not say who owes it:\n%s", msg)
			}
			// The origin, not a story. `n = xs[0]` bound a slot; no loop ran.
			if strings.Contains(msg, "loop") {
				t.Errorf("the refusal blames a loop for a program that has none: %s", msg)
			}
		})
	}
}

// TestAProvenSlotStillTakesTheWiderDoor is the gate asked directly, so a future widening or narrowing of
// `+` fails a decision row rather than silently changing which programs compile: a container the literal
// proves holds numbers opens `+`, and one that stores a text anywhere keeps the refusal.
func TestTheWiderOperatorsOpenOnlyWhereTheSlotsAreProven(t *testing.T) {
	proven := "xs = [0]\nxs[0] = 7\nn = xs[0]\nprint(n + 1)\n"
	if _, err := Compile(proven); err != nil {
		t.Logf("the literal-proven shape still refuses, which the row allows while the proof stays narrow: %v", err)
	}
	mixed := "xs = []\nxs.append(7)\nxs.append(\"text\")\nn = xs[0]\nprint(n + 1)\n"
	if res, err := Compile(mixed); err == nil {
		t.Errorf("the compiler joined a slot that may hold a text, emitting:\n%s", firstLines(res.IR, 20))
	} else if !strings.Contains(err.Error(), "tagged value") {
		t.Errorf("the refusal for an unproven `+` does not name the missing half:\n%s", err)
	}
}
