package lang

// pkg/lang/pair_number_float_test.go — a name bound from a container slot enters the DOUBLE domain
// (roadmap L11.1, Gap R.148; ADR 0303's pair binding, ADR 0304's arithmetic door, ADR 0253's per-kind raise).
//
// Gap R.148 was filed with the shape that made the asymmetry visible: `print(n / 4)` and `print(n > d)` were
// refused while `print(2.5 - n)` answered, and the naive route was worse than the refusal — the double road
// storing a `double` into the i32 slot a tagged name owns is a module `llc` rejects, which ADR 0166 counts as
// the compiler's own bug.
//
// What the pair road bound is two words, and the payload means a different thing per kind: the number itself
// (int, bool), a handle on an @float_box (float), an interned index (text), an entry count (a container). Only
// the tag says which. So the float domain reaches a pair-bound name through the SAME per-tag arms a slot read
// walks — the float arm unboxes, the int/bool arm converts, every other tag raises the sentence CPython writes
// for this operator and that kind (ADR 0252's closed tag set, ADR 0253's raise placement inside the arm that
// knows what it lifted).
//
// That last clause is the whole file's reason for existing. The tempting implementation was one call to
// `rt_lift_num`, which unboxes a float and `sitofp`s everything else — so `n / 4` over a slot holding "a"
// prints the interned index of "a" as a number, at exit 0. That is the wrong-answer family Gap R.38 and
// Gap R.95 keep filing, and it is why the trap rows here are asserted word for word rather than for "some
// TypeError".
//
// The CLI comparison against CPython lives in integration/pair_number_float_test.go.

import (
	"strings"
	"testing"
)

const numFloatSlot = "xs = []\nxs.append(7)\nn = xs[0]\n"

// TestASlotBoundNameEntersTheDoubleDomain is the row Gap R.148 named. Every shape is a name the pair road
// bound, asked for a double; each was refused before this cycle.
func TestASlotBoundNameEntersTheDoubleDomain(t *testing.T) {
	for _, tc := range []struct{ name, src, want string }{
		{"true division", numFloatSlot + "print(n / 4)\n", "1.75\n"},
		{"division by a float literal", numFloatSlot + "print(n / 2.5)\n", "2.8\n"},
		{"ordering against a float", numFloatSlot + "print(n > 2.5)\n", "True\n"},
		{"ordering the other way", numFloatSlot + "print(n < 2.5)\n", "False\n"},
		{"equality against a float", numFloatSlot + "print(n == 2.5)\n", "False\n"},
		{"addition with a float", numFloatSlot + "print(n + 2.5)\n", "9.5\n"},
		{"product with a float", numFloatSlot + "print(n * 2.5)\n", "17.5\n"},
		{
			"arithmetic over the name, then a float",
			numFloatSlot + "print(n - 1 + 0.5)\n", "6.5\n",
		},
		{
			"the float on the left", "xs = []\nxs.append(7)\nn = xs[0]\nprint(2.5 - n)\n", "-4.5\n",
		},
		{
			"a comparison the condition asks",
			numFloatSlot + "if n > 2.5:\n    print(\"big\")\n", "big\n",
		},
		{
			"the double reaches a builtin",
			numFloatSlot + "print(round(n / 4))\n", "2\n",
		},
		{
			"a float slot keeps its float",
			"xs = []\nxs.append(7.5)\nn = xs[0]\nprint(n / 2)\n", "3.75\n",
		},
		{
			"a float slot compared with an int",
			"xs = []\nxs.append(7.5)\nn = xs[0]\nprint(n > 7)\n", "True\n",
		},
		{
			"a negative slot divided",
			"xs = []\nxs.append(-8)\nn = xs[0]\nprint(n / 4)\n", "-2.0\n",
		},
		{
			"a bool slot is the number it is",
			"xs = []\nxs.append(True)\nn = xs[0]\nprint(n / 2)\n", "0.5\n",
		},
		{
			"a dict slot by key",
			"d = {}\nd[\"k\"] = 9\nn = d[\"k\"]\nprint(n / 3)\n", "3.0\n",
		},
		{
			"a slot read by a computed position",
			"xs = []\nxs.append(7)\nxs.append(9)\ni = 1\nn = xs[i]\nprint(n / 3)\n", "3.0\n",
		},
		{
			"a slot two levels down",
			"xs = []\nxs.append([7, 8])\nn = xs[0][1]\nprint(n / 2)\n", "4.0\n",
		},
		{
			"the loop variable, in the double domain",
			"xs = []\nxs.append(7)\nfor v in xs:\n    print(v / 2)\n", "3.5\n",
		},
		{
			"the answer bound and used again",
			numFloatSlot + "h = n / 2\nprint(h)\nprint(h + 1)\n", "3.5\n4.5\n",
		},
		{
			// The ordering against a STATIC INT is the shape Gap R.148 was measured with, and the one
			// the float-domain door alone did not reach: `n > 2.5` went to the double road, `n > 7`
			// stayed on the comparison road, whose pair door admits only names whose tag is a proof.
			// The ordering door now walks a pair-bound name through the same three arms a slot read
			// walks — numbers compare, and every other kind raises the reference's sentence naming BOTH
			// operand types in source order (ADR 0250, ADR 0252).
			// 7 > 7 is False — the record, CPython and the compiled run agree on it, and the row keeps
			// the shape's own terms rather than a lucky operand.
			"an ordering against an int literal",
			numFloatSlot + "print(n > 7)\n", "False\n",
		},
		{
			"an ordering that is true",
			numFloatSlot + "print(n > 3)\n", "True\n",
		},
		{
			"the int literal on the left",
			"xs = []\nxs.append(7)\nn = xs[0]\nprint(7 > n)\n", "False\n",
		},
		{
			"a reversed ordering",
			numFloatSlot + "print(n >= 7)\n", "True\n",
		},
		{
			"a float slot ordered against an int",
			"xs = []\nxs.append(7.5)\nn = xs[0]\nprint(n > 7)\n", "True\n",
		},
		{
			"the ordering runs a while head",
			"xs = []\nxs.append(5)\nn = xs[0]\nwhile n > 3:\n    n = n - 1\nprint(n)\n", "3\n",
		},
		{
			"two orderings under an and",
			numFloatSlot + "if n > 1 and n < 9:\n    print(\"mid\")\n", "mid\n",
		},
		{
			"the loop variable ordered",
			"xs = []\nxs.append(5)\nfor v in xs:\n    print(v > 3)\n", "True\n",
		},
		{
			"a double variable on the other side",
			"xs = []\nxs.append(7)\nn = xs[0]\nd = 2.5\nprint(n < d)\n", "False\n",
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

// TestADoubleDomainUseOfASlotBoundNameRaisesTheKindItHolds is the safety half, and the reason this file does
// not simply call `rt_lift_num`: the lift unboxes a float and converts *everything else*, so a text slot would
// answer the interned index of "a" as a number with exit 0. Each row is compared by class AND message, because
// a door that raised `TypeError: x` would pass a weaker test and still be a language whose `except` cannot
// match anything.
func TestADoubleDomainUseOfASlotBoundNameRaisesTheKindItHolds(t *testing.T) {
	for _, tc := range []struct{ name, src, class, message string }{
		{
			"a text slot divided",
			"xs = []\nxs.append(\"a\")\nn = xs[0]\nprint(n / 4)\n",
			"TypeError", "unsupported operand type(s) for /: 'str' and 'int'",
		},
		{
			"a None slot divided",
			"xs = []\nxs.append(None)\nn = xs[0]\nprint(n / 4)\n",
			"TypeError", "unsupported operand type(s) for /: 'NoneType' and 'int'",
		},
		{
			"a container slot divided",
			"xs = []\nxs.append([1])\nn = xs[0]\nprint(n / 4)\n",
			"TypeError", "unsupported operand type(s) for /: 'list' and 'int'",
		},
		{
			"a dict slot divided",
			"xs = []\nxs.append({\"k\": 1})\nn = xs[0]\nprint(n / 4)\n",
			"TypeError", "unsupported operand type(s) for /: 'dict' and 'int'",
		},
		{
			"an ordering the reference refuses",
			"xs = []\nxs.append(\"a\")\nn = xs[0]\nprint(n > 2.5)\n",
			"TypeError", "'>' not supported between instances of 'str' and 'float'",
		},
		{
			// The `+` case the double domain reaches: with a float operand CPython has its own
			// sentence, which is NOT the generic one — asserting the generic wording here would
			// pin a wrong message and call it a pass.
			"joining a text slot to a float",
			"xs = []\nxs.append(\"a\")\nn = xs[0]\nprint(n + 2.5)\n",
			"TypeError", "can only concatenate str (not \"float\") to str",
		},
		{
			"the arithmetic over a text slot, then a float",
			"xs = []\nxs.append(\"a\")\nn = xs[0]\nprint(n - 1 + 0.5)\n",
			"TypeError", "unsupported operand type(s) for -: 'str' and 'int'",
		},
		{
			"zero divisor, two ints",
			numFloatSlot + "print(n / 0)\n",
			"ZeroDivisionError", "division by zero",
		},
		{
			"zero divisor with a float in the pair",
			numFloatSlot + "print(n / 0.0)\n",
			"ZeroDivisionError", "float division by zero",
		},
		{
			// The ordering's raise names BOTH operand types, in source order — the reason the pair
			// side goes through the tagged ordering door rather than a bare lift.
			"an ordering a text slot refuses",
			"xs = []\nxs.append(\"a\")\nn = xs[0]\nprint(n > 7)\n",
			"TypeError", "'>' not supported between instances of 'str' and 'int'",
		},
		{
			"the same ordering with the literal first names them the other way",
			"xs = []\nxs.append(\"a\")\nn = xs[0]\nprint(7 > n)\n",
			"TypeError", "'>' not supported between instances of 'int' and 'str'",
		},
		{
			"a None slot ordered",
			"xs = []\nxs.append(None)\nn = xs[0]\nprint(n >= 1)\n",
			"TypeError", "'>=' not supported between instances of 'NoneType' and 'int'",
		},
		{
			// The shape mixed_list_test.go's refusal table used to pin: one element is a number and the
			// next is a text, so the loop variable compares and then raises — the reference's behaviour,
			// which the compiled backend now matches line for line (roadmap Gap R.148, ADR 0304).
			"a loop over a container that mixes kinds, ordered",
			"xs = [1, \"a\"]\nfor x in xs:\n    print(x > 2)\n",
			"TypeError", "'>' not supported between instances of 'str' and 'int'",
		},
		{
			"a container slot ordered",
			"xs = []\nxs.append([1])\nn = xs[0]\nprint(n > 2)\n",
			"TypeError", "'>' not supported between instances of 'list' and 'int'",
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

// TestTheDoubleDomainTrapOfASlotBoundNameIsCatchable: the raise is emitted inside the arm that knows what it
// lifted, and it leaves through the open handler — so the program's own `except` runs (ADR 0228).
func TestTheDoubleDomainTrapOfASlotBoundNameIsCatchable(t *testing.T) {
	for _, tc := range []struct{ name, src, want string }{
		{
			"a TypeError arm runs on a slot's division",
			"xs = []\nxs.append(\"a\")\nn = xs[0]\ntry:\n    print(n / 4)\nexcept TypeError:\n    print(\"caught\")\n",
			"caught\n",
		},
		{
			"a ZeroDivisionError arm runs on a slot's division",
			numFloatSlot + "try:\n    print(n / 0)\nexcept ZeroDivisionError:\n    print(\"caught\")\n",
			"caught\n",
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

// TestTheDoubleDomainOfASlotBoundNameBranchesOnTheTag is the IR half. The empty-operand `fdiv`/`fadd` is the
// instruction ADR 0253 blacklisted module-wide — it substituted silence for an operand the compiler never
// read — and a module that answers these shapes without testing the tag has quietly gone back to guessing.
func TestTheDoubleDomainOfASlotBoundNameBranchesOnTheTag(t *testing.T) {
	res, err := Compile(numFloatSlot + "print(n / 4)\n")
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	assertNoForbiddenIR(t, numFloatSlot+"print(n / 4)\n", res.IR)
	for _, forbidden := range []string{"fdiv double ,", "fadd double ,", "fmul double ,", "fsub double ,"} {
		if strings.Contains(res.IR, forbidden) {
			t.Errorf("the module substituted an operand it never read (%s) — ADR 0253's blacklist", forbidden)
		}
	}
	if !strings.Contains(res.IR, "@rt_float_of") {
		t.Errorf("the module never unboxed a float slot, so the float arm is missing:\n%s", firstLines(res.IR, 24))
	}
	if !strings.Contains(res.IR, "_n_tag") {
		t.Errorf("the module read the name without its tag, so the arm is a guess")
	}
}

// TestTheDoubleDomainStillRefusesThePositionsThatTakeOneWord is the half this cycle did not open, so that
// the next one cannot quietly re-pin it as a pass: `float(n)`, a `sum` element, an `abs` operand and a `min`
// argument each keep ONE word for the value, and the sentence names the origin it was bound from — never a
// loop the program does not contain (Gap R.38). The f-string field and the list element left this table when
// those roads learned to ask the tag (ADR 0306, ADR 0307).
func TestTheDoubleDomainStillRefusesThePositionsThatTakeOneWord(t *testing.T) {
	for _, tc := range []struct{ name, src string }{
		{"float(n)", numFloatSlot + "print(float(n))\n"},
		{"a sum element", numFloatSlot + "print(sum([n]))\n"},
		{"an abs operand", "xs = []\nxs.append(-7)\nn = xs[0]\nprint(abs(n))\n"},
		{"a min argument", numFloatSlot + "print(min(n, 3))\n"},
		// `print(f"{n - 1}")` was on this table and answers since ADR 0307: an f-string field asks the
		// module's one tag-reading printer, so it is pinned in pair_fstring_test.go's answer table.
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
			if strings.Contains(msg, "loop") {
				t.Errorf("the refusal blames a loop for a program that has none: %s", msg)
			}
		})
	}
}
