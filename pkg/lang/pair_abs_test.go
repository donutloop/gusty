package lang

// pkg/lang/pair_abs_test.go — the signless call asks the tag (roadmap L11.1, Gap R.146's positions that keep
// one word for a whole value; ADR 0309, ADR 0265's per-operator arithmetic door, ADR 0271's raise wording).
//
// `print(n - 1)` and `print(-n)` have asked the tag since ADR 0304, but `print(abs(n))` kept refusing: `abs`
// is a CALL, and the arithmetic door was written for operators. Its numeric road reads one word — the
// payload — and a pair-bound name has two (ADR 0252's closed tag set). Reading a float's box handle or a
// text's `@str_tab` index as the number to take the magnitude of is the Gap R.38 family: a wrong answer, at
// the exit code of success.
//
// `abs` is now the arithmetic door's own op — the signless call, beside the unary minus — so the operand's
// kind comes from the tag and the answer's kind follows it: int or bool in, int out; float in, float out.
// CPython answers a number or raises, so the door takes the operand whatever the slot holds, which is the
// same rule `-x` follows.
//
// The raise is the other half of the row. ADR 0271's rule is that a trap says what the program wrote, and the
// signless call's sentence is not the minus's: `abs("a")` is `bad operand type for abs(): 'str'`, not
// `bad operand type for unary -: 'str'`. `rt_num_bad` therefore picks its format by WHICH CALL the operand
// came from, not by which op the shared helper implements — sharing one format is how that rule broke the
// first time.
//
// The CLI comparison against CPython lives in integration/pair_abs_test.go.

import (
	"strings"
	"testing"
)

const absIntSlot = "xs = []\nxs.append(7)\nn = xs[0]\n"

// TestTheSignlessCallAnswersForAPairBoundName is the row this cycle exists for. Every shape is `abs` over a
// name the pair road bound, and every answer is the record's, which is CPython's.
func TestTheSignlessCallAnswersForAPairBoundName(t *testing.T) {
	for _, tc := range []struct{ name, src, want string }{
		{"an int slot", absIntSlot + "print(abs(n))\n", "7\n"},
		{"a negative slot", "xs = []\nxs.append(-8)\nn = xs[0]\nprint(abs(n))\n", "8\n"},
		{"a float slot", "xs = []\nxs.append(-2.5)\nn = xs[0]\nprint(abs(n))\n", "2.5\n"},
		{"a positive float slot keeps its .0", "xs = []\nxs.append(2.5)\nn = xs[0]\nprint(abs(n))\n", "2.5\n"},
		{"a bool slot is the number it is", "xs = []\nxs.append(True)\nn = xs[0]\nprint(abs(n))\n", "1\n"},
		{"a zero slot", "xs = []\nxs.append(0)\nn = xs[0]\nprint(abs(n))\n", "0\n"},
		{"a negative zero answers zero", "xs = []\nxs.append(-0.0)\nn = xs[0]\nprint(abs(n))\n", "0.0\n"},
		{"the answer is bound and printed", absIntSlot + "y = abs(n)\nprint(y)\n", "7\n"},
		{"the answer is bound and measured", absIntSlot + "y = abs(n)\nprint(len([y]))\n", "1\n"},
		{"a slot of the arithmetic over it", "xs = []\nxs.append([7, 8])\nn = xs[0][0] * 2\nprint(abs(n))\n", "14\n"},
		{"the sign already gone", "xs = []\nxs.append([7, 8])\nn = xs[0][0] * -2\nprint(abs(n))\n", "14\n"},
		{"a dict slot", "d = {}\nd[\"k\"] = -9\nn = d[\"k\"]\nprint(abs(n))\n", "9\n"},
		{"the loop variable", "xs = []\nxs.append(-5)\nfor v in xs:\n    print(abs(v))\n", "5\n"},
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

// TestTheSignlessCallRaisesWhatTheReferenceRaises is the half a friendly-looking door forgets: a magnitude
// exists only for a number, and the program must hear CPython's sentence with the KIND the tag named. These
// are the rows where reading one word would have been silent — a text slot's payload is an interned index,
// and `abs` of an index is a number.
func TestTheSignlessCallRaisesWhatTheReferenceRaises(t *testing.T) {
	for _, tc := range []struct {
		name, src, class, message string
	}{
		{"a text slot", "xs = []\nxs.append(\"a\")\nn = xs[0]\nprint(abs(n))\n", "TypeError", "bad operand type for abs(): 'str'"},
		{"a None slot", "xs = []\nxs.append(None)\nn = xs[0]\nprint(abs(n))\n", "TypeError", "bad operand type for abs(): 'NoneType'"},
		{"a list slot", "xs = []\nxs.append([1])\nn = xs[0]\nprint(abs(n))\n", "TypeError", "bad operand type for abs(): 'list'"},
		{"a dict slot", "xs = []\nxs.append({\"k\": 1})\nn = xs[0]\nprint(abs(n))\n", "TypeError", "bad operand type for abs(): 'dict'"},
		{"a set slot", "xs = []\nxs.append({1})\nn = xs[0]\nprint(abs(n))\n", "TypeError", "bad operand type for abs(): 'set'"},
		{
			// ADR 0271's rule, measured: the minus's sentence must not arrive from a program that
			// never wrote a minus.
			"a text slot names abs, not the minus",
			"xs = []\nxs.append(\"a\")\nn = xs[0]\nprint(abs(n))\n", "TypeError", "bad operand type for abs(): 'str'",
		},
		{
			"the minus keeps its own sentence",
			"xs = []\nxs.append(\"a\")\nn = xs[0]\nprint(-n)\n", "TypeError", "bad operand type for unary -: 'str'",
		},
		{
			"a raise the program catches runs its own arm",
			"xs = []\nxs.append(\"a\")\nn = xs[0]\ntry:\n    print(abs(n))\nexcept TypeError:\n    print(\"caught\")\n", "", "",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			res, err := JIT(tc.src, 0)
			if err != nil {
				t.Fatalf("compiled to nothing where the reference raises: %v", err)
			}
			if tc.message == "" {
				if res.Output != "caught\n" {
					t.Fatalf("the arm did not run: %q / %s", res.Output, res.Stderr)
				}
				return
			}
			if !strings.Contains(res.Stderr, tc.class+": "+tc.message) {
				t.Fatalf("raised %q, want %s: %s", firstLine(res.Stderr), tc.class, tc.message)
			}
			if res.Code == 0 {
				t.Fatalf("the trap left at the exit code of success")
			}
		})
	}
}

// TestTheSignlessCallOfAKnownValueIsStillFolded keeps the door off the positions that never needed it: `abs`
// of a literal has a compile-time answer, and a door that routes it through the run time both costs an
// instruction and lets a module grow a helper call for a constant.
func TestTheSignlessCallOfAKnownValueIsStillFolded(t *testing.T) {
	for _, tc := range []struct {
		name, src, want string
	}{
		{"a positive literal", "print(abs(7))\n", "7\n"},
		{"a negative literal", "print(abs(-8))\n", "8\n"},
		{"a float literal", "print(abs(-2.5))\n", "2.5\n"},
		{"a bool literal", "print(abs(True))\n", "1\n"},
		{"a plain variable", "x = -7\nprint(abs(x))\n", "7\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			RecordedStdoutIs(t, tc.src, tc.want)
			res, err := Compile(tc.src)
			if err != nil {
				t.Fatalf("refused a program the reference prints: %v", err)
			}
			if strings.Contains(res.IR, "call double @llvm.fabs") && strings.Contains(tc.src, "abs(7)") {
				t.Errorf("abs of a constant asked the run time for a magnitude")
			}
		})
	}
}

// TestTheSignlessCallStillRefusesThePositionsThatTakeOneWord is this cycle's honest half, and it is a wide
// field. `abs` answers for the position it was taught; the neighbours that keep one word for a whole value —
// an operand of a comparison, an f-string field, the float road's double — keep the sentence they have
// always printed, naming the missing half and who owes it (roadmap L11.1, Gap R.146).
//
// One refusal is worth naming in the code rather than only here, because it was found by the compiler
// breaking itself: with the door open for `abs`, `round(abs(n) / 2)` had the double door lift its own
// sibling, which lifted back, until the process ran out of stack. A compiler crash is exit 2, which ADR 0166
// reserves for the module the verifier rejected; the pair door keeps its hand off a pair-shaped sibling and
// the position refuses instead.
func TestTheSignlessCallStillRefusesThePositionsThatTakeOneWord(t *testing.T) {
	for _, tc := range []struct{ name, src string }{
		{"an abs operand of a sum the door will not prove", absIntSlot + "print(abs(n) + 1)\n"},
		{"two abs answers added together", absIntSlot + "m = xs[0]\nprint(abs(n) + abs(m))\n"},
		{"an abs operand of a product", absIntSlot + "print(abs(n) * 2)\n"},
		{"an abs operand of a modulo", absIntSlot + "print(abs(n) % 3)\n"},
		{"an abs operand of a true quotient", absIntSlot + "print(round(abs(n) / 2))\n"},
		{"abs of a negation", absIntSlot + "print(abs(-n))\n"},
		{"a min argument", absIntSlot + "print(min(abs(n), 3))\n"},
		{"a sum element", absIntSlot + "print(sum([abs(n)]))\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			CompiledRefusal(t, tc.src, "roadmap")
		})
	}
}

// TestTheSignlessCallRunsThroughTheArithmeticDoorIs the IR half of the decision: the magnitude is taken from
// the lifted value inside the module's own arithmetic helper, so a pair never reaches printf as a number and
// the sentence is written by the helper that knows the tag.
func TestTheSignlessCallRunsThroughTheArithmeticDoor(t *testing.T) {
	res, err := Compile(absIntSlot + "print(abs(n))\n")
	if err != nil {
		t.Fatalf("refused a program the reference prints: %v", err)
	}
	for _, want := range []string{"call i32 @rt_num_arith(", "llvm.fabs.f64"} {
		if !strings.Contains(res.IR, want) {
			t.Errorf("the magnitude never asked %s — abs grew a second road of its own", want)
		}
	}
	// The tag has to be part of the answer: a `%d` of the payload alone is the wrong answer this file keeps
	// impossible, and the tag register must be written back, not read and dropped.
	if !strings.Contains(res.IR, "call void @rt_print_value(") && !strings.Contains(res.IR, "@rt_print_mixed_value(") {
		t.Errorf("the answer was printed without asking the tag")
	}
}
