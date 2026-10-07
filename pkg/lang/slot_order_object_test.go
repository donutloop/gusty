package lang

import (
	"strings"
	"testing"
)

// slot_order_object_test.go — the ordering comparison of a slot **no literal describes** (roadmap
// L11.1, Gap R.93; ADR 0252).
//
// ADR 0250 gave `<`, `<=`, `>`, `>=` three arms — two numbers, two texts, and CPython's TypeError for
// the pair that does not order — and read the arms off the literal that built the container. That left
// out the containers the program built for itself:
//
//	xs = []
//	for i in [1, 2]:
//	    xs.append(i)
//	print(1 if xs[0] > "a" else 0)   # CPython: TypeError · --interp: TypeError · --aot printed 1
//
// A verdict for a program that crashes is the defect class Gap R.82, Gap R.85 and Gap R.93 keep
// measuring, and the compiled leg was still answering it. The fix is not a second table of "what xs
// could hold" — a container built by `append` has no such table — it is asking the object: the tag
// beside the slot answers "are you one of the three number kinds?", "are you text?", and, on the arm
// that neither, "what name goes in the sentence CPython writes?". One test per kind the writers can
// store, each raising its own wording, with the text tag as the unconditional last arm because the set
// of tags ADR 0187's writers leave beside a payload is closed.
//
// Same shape one level down, which ADR 0251's read answered and the ordering had not:
//
//	xs = []
//	xs.append([3, "a"])
//	print(1 if xs[0][0] > 1 else 0)   # 1 on both legs, was exit 1 compiled
//
// Both legs below, and CPython in the sibling integration file: parity rows, the traps that must be
// raised rather than refused, the refusals that remain, and an IR row that fails if the module stops
// branching on the tag.

func TestSlotOrderOfAnUnliteralisedSlotAnswersOnBothLegs(t *testing.T) {
	for _, tc := range []struct{ name, src, want string }{
		// ---- the container `append` built, against a number the compiler read.
		{"appended int above a number", "xs = []\nxs.append(3)\nprint(1 if xs[0] > 1 else 0)\n", "1\n"},
		{"appended int below a number", "xs = []\nxs.append(3)\nprint(1 if xs[0] < 5 else 0)\n", "1\n"},
		{"appended int against a float", "xs = []\nxs.append(3)\nprint(1 if xs[0] > 1.5 else 0)\n", "1\n"},
		{"appended float against an int", "xs = []\nxs.append(1.5)\nprint(1 if xs[0] > 1 else 0)\n", "1\n"},
		{"appended float against its own value", "xs = []\nxs.append(1.5)\nprint(1 if xs[0] >= 1.5 else 0)\nprint(1 if xs[0] > 1.5 else 0)\n", "1\n0\n"},
		{"appended negative", "xs = []\nxs.append(-3)\nprint(1 if xs[0] < 0 else 0)\n", "1\n"},
		{"le_and_ge", "xs = []\nxs.append(2)\nprint(1 if xs[0] <= 2 else 0)\nprint(1 if xs[0] >= 3 else 0)\n", "1\n0\n"},
		// ---- one container, several kinds: the arm that runs is a run-time question per slot.
		{
			"int slot and text slot in one built container",
			"xs = []\nxs.append(3)\nxs.append(\"a\")\nprint(1 if xs[0] > 1 else 0)\nprint(1 if xs[1] > \"A\" else 0)\n",
			"1\n1\n",
		},
		{
			"three kinds in one built container",
			"xs = []\nxs.append(3)\nxs.append(\"b\")\nxs.append(1.5)\nprint(1 if xs[0] > 2 else 0)\nprint(1 if xs[1] > \"a\" else 0)\nprint(1 if xs[2] < 2 else 0)\n",
			"1\n1\n1\n",
		},
		// ---- texts order by their characters, not by the order they were interned in (ADR 0248's rule,
		// read through the tag this time).
		{
			"appended texts order as text",
			"xs = []\nxs.append(\"b\")\nxs.append(\"a\")\nprint(1 if xs[0] > \"a\" else 0)\nprint(1 if xs[1] > \"a\" else 0)\n",
			"1\n0\n",
		},
		// ---- the loop-built container Gap R.93 measured.
		{
			"loop-built container against a number",
			"xs = []\nfor i in [1, 2]:\n    xs.append(i)\nprint(1 if xs[0] > 1 else 0)\n",
			"0\n",
		},
		{
			"loop-built container of texts",
			"xs = []\nfor s in [\"b\", \"a\"]:\n    xs.append(s)\nprint(1 if xs[0] > \"a\" else 0)\nprint(1 if xs[1] > \"a\" else 0)\n",
			"1\n0\n",
		},
		// ---- the dict the program filled by assignment.
		{"dict slot against a number", "d = {}\nd[\"a\"] = 5\nprint(1 if d[\"a\"] > 4 else 0)\n", "1\n"},
		{"dict slot against a float", "d = {}\nd[\"a\"] = 1.5\nprint(1 if d[\"a\"] < 2 else 0)\n", "1\n"},
		// ---- the same door one level below, which ADR 0251 opened for the read alone.
		{"nested slot against a number", "xs = []\nxs.append([3, \"a\"])\nprint(1 if xs[0][0] > 1 else 0)\n", "1\n"},
		{"nested text slot against a text", "xs = []\nxs.append([3, \"a\"])\nprint(1 if xs[0][1] > \"A\" else 0)\n", "1\n"},
		{"nested float slot", "xs = []\nxs.append([1.5, 2])\nprint(1 if xs[0][0] >= 1.5 else 0)\n", "1\n"},
		{"nested dict slot by key", "xs = []\nxs.append({\"k\": 3})\nprint(1 if xs[0][\"k\"] > 1 else 0)\n", "1\n"},
		{"dict of lists read twice", "d = {}\nd[\"a\"] = [1, 9]\nprint(1 if d[\"a\"][1] > 5 else 0)\n", "1\n"},
		// ---- the settled side may be a variable the compiler can name a kind for, not only a literal.
		{
			"appended int against an int variable",
			"xs = []\nxs.append(3)\ni = 2\nprint(1 if xs[0] > i else 0)\nprint(1 if xs[0] < i else 0)\n",
			"1\n0\n",
		},
		{
			"appended float against a float variable",
			"xs = []\nxs.append(1.5)\nf = 2.0\nprint(1 if xs[0] < f else 0)\nprint(1 if f > xs[0] else 0)\n",
			"1\n1\n",
		},
		{
			"appended text against a text variable",
			"xs = []\nxs.append(\"b\")\nt = \"a\"\nprint(1 if xs[0] > t else 0)\nprint(1 if t > xs[0] else 0)\n",
			"1\n0\n",
		},
		// ---- the positions a comparison is used in (ADR 0250's four, re-pinned for this door).
		{"in a condition", "xs = []\nxs.append(3)\nif xs[0] > 2:\n    print(\"big\")\nelse:\n    print(\"small\")\n", "big\n"},
		{"in a while head", "xs = []\nxs.append(3)\ni = 0\nwhile xs[0] > i:\n    print(i)\n    i = i + 1\n", "0\n1\n2\n"},
		{"in an and compound", "xs = []\nxs.append(3)\nprint(1 if (xs[0] > 1 and xs[0] < 5) else 0)\n", "1\n"},
		// ---- shapes the older doors already owned, pinned so this one cannot take them over.
		{"literal container still orders from the literal", "xs = [3, \"a\"]\nprint(1 if xs[0] > 1 else 0)\nprint(1 if xs[1] > \"A\" else 0)\n", "1\n1\n"},
		{"literal nested read still orders", "m = [[1, 2]]\nprint(1 if m[0][1] > 1 else 0)\n", "1\n"},
		{"literal dict nested read still orders", "d = {\"a\": [1, 2]}\nprint(1 if d[\"a\"][1] > 3 else 0)\n", "0\n"},
		{"comprehension-built container orders", "xs = [x for x in [3, 4]]\nprint(1 if xs[0] > 2 else 0)\n", "1\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			res, err := Compile(tc.src)
			if err != nil {
				t.Fatalf("%s (%q): refused: %v", tc.name, tc.src, err)
			}
			if out := runIR(t, res.IR); out != tc.want {
				t.Errorf("%s: AOT ran %q, want CPython's %q\nsrc: %s", tc.name, out, tc.want, tc.src)
			}
			if out := captureStdout(t, tc.src); out != tc.want {
				t.Errorf("%s: interpreter printed %q, want CPython's %q\nsrc: %s", tc.name, out, tc.want, tc.src)
			}
		})
	}
}

// TestSlotOrderOfAnUnliteralisedSlotTrapsLikeCPython pins the other half of the claim: a slot whose
// kind the object reports does not merely fail to order against an operand it cannot order with — it
// *raises*, with CPython's wording and the left operand's type named first. Before this door the
// compiled leg printed a verdict for these programs (Gap R.93's measurement), which is the one answer
// worse than a refusal: the program looks like it worked.
func TestSlotOrderOfAnUnliteralisedSlotTrapsLikeCPython(t *testing.T) {
	for _, tc := range []struct{ name, src, class, message string }{
		{
			"an int slot ordered against a text",
			"xs = []\nxs.append(3)\nprint(1 if xs[0] > \"a\" else 0)\n",
			"TypeError", "'>' not supported between instances of 'int' and 'str'",
		},
		{
			// CPython names the *left* operand first, so the sentence is not one string: it is a
			// question about which side the tag had to be asked about.
			"a text on the left of an int slot",
			"xs = []\nxs.append(3)\nprint(1 if \"a\" < xs[0] else 0)\n",
			"TypeError", "'<' not supported between instances of 'str' and 'int'",
		},
		{
			"a float slot ordered against a text",
			"xs = []\nxs.append(1.5)\nprint(1 if xs[0] >= \"a\" else 0)\n",
			"TypeError", "'>=' not supported between instances of 'float' and 'str'",
		},
		{
			"a None slot ordered against a number",
			"xs = []\nxs.append(None)\nprint(1 if xs[0] > 1 else 0)\n",
			"TypeError", "'>' not supported between instances of 'NoneType' and 'int'",
		},
		{
			"a list slot ordered against a number",
			"xs = []\nxs.append([1, 2])\nprint(1 if xs[0] > 1 else 0)\n",
			"TypeError", "'>' not supported between instances of 'list' and 'int'",
		},
		{
			"a dict slot ordered against a number",
			"xs = []\nxs.append({\"k\": 1})\nprint(1 if xs[0] < 1 else 0)\n",
			"TypeError", "'<' not supported between instances of 'dict' and 'int'",
		},
		{
			"a set slot ordered against a number",
			"xs = []\nxs.append({1, 2})\nprint(1 if xs[0] > 1 else 0)\n",
			"TypeError", "'>' not supported between instances of 'set' and 'int'",
		},
		{
			"a text slot ordered against an int variable",
			"xs = []\nxs.append(\"a\")\ni = 1\nprint(1 if xs[0] > i else 0)\n",
			"TypeError", "'>' not supported between instances of 'str' and 'int'",
		},
		{
			"an int slot ordered against a text variable",
			"xs = []\nxs.append(3)\nt = \"a\"\nprint(1 if xs[0] <= t else 0)\n",
			"TypeError", "'<=' not supported between instances of 'int' and 'str'",
		},
		{
			"nested: an int slot ordered against a text",
			"xs = []\nxs.append([3, \"a\"])\nprint(1 if xs[0][0] > \"a\" else 0)\n",
			"TypeError", "'>' not supported between instances of 'int' and 'str'",
		},
		{
			"the bounds check still fires, in the block the comparison starts in",
			"xs = []\nxs.append([1, 2])\nprint(1 if xs[0][9] > 1 else 0)\n",
			"IndexError", "index out of range",
		},
		{
			"a missing key still raises its own KeyError",
			"d = {}\nd[\"a\"] = [1, 2]\nprint(1 if d[\"z\"][0] > 1 else 0)\n",
			// The INTERPRETED KeyError carries the key's repr ('z' here); the compiled leg still
			// prints the module's constant sentence, because its raise is a compile-time string and
			// naming the key needs the key rendered at run time -- L11.1's tagged word, and why this
			// row is PARTIAL rather than closed (Gap R.189, ADR 0301).
			"KeyError", "'z'",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ee := trapRun(t, tc.src)
			if ee.ExnType != tc.class {
				t.Errorf("interpreter raised %q, want %q (msg %q)", ee.ExnType, tc.class, ee.ExnMsg)
			}
			if ee.ExnMsg != tc.message {
				t.Errorf("interpreter message =\n  %q\nwant\n  %q", ee.ExnMsg, tc.message)
			}
			res, err := Compile(tc.src)
			if err != nil {
				t.Fatalf("the compiled backend refused a program the oracle traps on: %v", err)
			}
			out := runIRMayTrap(t, res.IR)
			if !strings.Contains(out, "Traceback (most recent call last):") {
				t.Errorf("the compiled program printed no traceback:\n%s", out)
			}
			// A KeyError carries the key's repr, in the compiled program as in the reference — the
			// raise site renders a literal key into the message the way the reference words it
			// (roadmap Gap R.189, paid by the compiled leg in ADR 0302's cycle). A key the compiler
			// cannot see as a literal keeps the generic sentence, and L11.1's tagged word is what would
			// name those too; a row here asserting the old prose would be asserting a regression.
			compiledWant := tc.message
			if !strings.Contains(out, compiledWant) {
				t.Errorf("compiled message missing %q:\n%s", compiledWant, out)
			}
		})
	}
}

// TestSlotOrderOfAnUnliteralisedSlotIsCatchable pins that the raise is a trap and not a crash: the
// program can catch it, which is the difference between exit 3 and exit 1 and the reason the arm may
// not be a refusal (roadmap ADR 0166's exit-class contract, Gap R.37's rule).
func TestSlotOrderOfAnUnliteralisedSlotIsCatchable(t *testing.T) {
	src := "xs = []\nxs.append(3)\ntry:\n    print(1 if xs[0] > \"a\" else 0)\nexcept TypeError:\n    print(\"caught\")\n"
	res, err := Compile(src)
	if err != nil {
		t.Fatalf("refused: %v", err)
	}
	const want = "caught\n"
	if out := runIR(t, res.IR); out != want {
		t.Errorf("AOT ran %q, want %q", out, want)
	}
	if out := captureStdout(t, src); out != want {
		t.Errorf("interpreter printed %q, want %q", out, want)
	}
}

// TestSlotOrderOfAnUnliteralisedSlotRefusesWhatItCannotName pins the shapes this door still declines,
// each by naming the half that is missing. The rule that keeps them out is the one in
// taggedOrderApplies: the raise sentence names *two* types, so the door takes one side whose kind the
// object reports only against an operand the compiler read itself.
func TestSlotOrderOfAnUnliteralisedSlotRefusesWhatItCannotName(t *testing.T) {
	for _, tc := range []struct{ name, src, want string }{
		{
			// Gap R.83's shape one door over: the kind of the *other* operand is what cannot be
			// proven. Measured below rather than here — it answers a verdict today.
			"a built slot ordered against a container literal",
			"xs = []\nxs.append([3, \"a\"])\nprint(1 if xs[0][0] > [0] else 0)\n",
			"cannot reach into xs's slots",
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
			if strings.Contains(err.Error(), "LLVM ERROR") || strings.Contains(err.Error(), "verifier") {
				t.Errorf("%s failed as an IR problem instead of a front-end refusal: %v", tc.name, err)
			}
		})
	}
}

// TestSlotOrderOfTwoUnliteralisedSlotsIsFiledNotFixed pins the two shapes this door still leaves to the
// lowering underneath, with what each engine answers *today* written into the row. They are not
// expectations to keep: they are the measurement roadmap Gap R.97 and Gap R.83 carry, and the day one
// of them starts matching the oracle this test fails and points at the row (Gap R.37's rule — a known
// wrong answer has to be a failing check somewhere, not a comment).
func TestSlotOrderOfTwoUnliteralisedSlotsIsFiledNotFixed(t *testing.T) {
	for _, tc := range []struct {
		name, src, aotWant, oracle string
		gap                        string
	}{
		{
			// Both sides are slots the object would have to describe, so the raise sentence would need
			// one branch per *pair* of kinds; the door steps aside and the payload compare answers.
			"two built slots of different kinds ordered against each other",
			"xs = []\nxs.append(\"a\")\nys = []\nys.append(1)\nprint(1 if xs[0] > ys[0] else 0)\n",
			"0\n",
			"TypeError: '>' not supported between instances of 'str' and 'int'",
			"roadmap Gap R.97",
		},
		{
			// The other operand's kind is the unprovable half — a function that returns text on one
			// path and a number on another. Gap R.83 measures the same shape for `==`.
			"a built slot ordered against a call of two kinds",
			"def pick(c):\n    if c:\n        return \"z\"\n    return 7\n\nxs = []\nxs.append(3)\nprint(1 if xs[0] > pick(1) else 0)\n",
			"1\n",
			"TypeError: '>' not supported between instances of 'int' and 'str'",
			"roadmap Gap R.83",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ee := trapRun(t, tc.src)
			if ee.ExnType == "" {
				t.Errorf("the interpreter answered %q where the oracle traps; the oracle says %s",
					captureStdout(t, tc.src), tc.oracle)
			}
			res, err := Compile(tc.src)
			if err != nil {
				t.Fatalf("refused: %v (%s owes this shape an answer or a trap, not a refusal)", err, tc.gap)
			}
			out := runIR(t, res.IR)
			if out != tc.aotWant {
				t.Errorf("%s: the compiled leg prints %q, the row recorded %q — if it now prints the oracle's "+
					"answer (traps: %s), delete this row and close %s", tc.name, out, tc.aotWant, tc.oracle, tc.gap)
			}
		})
	}
}

// TestSlotOrderOfAnUnliteralisedSlotBranchesOnTheTag is the IR half: the module must carry the tests
// on the tag and one CPython sentence per kind the writers can leave beside a payload, so the name in
// the TypeError is the slot's real kind rather than the one the compiler guessed. It also keeps the
// two shapes that used to live in this path out of the module: a global in a value position (`llc`
// rejecting it is exit 2, ADR 0166's own class) and a comparison of two payloads with no tag in sight.
func TestSlotOrderOfAnUnliteralisedSlotBranchesOnTheTag(t *testing.T) {
	src := "xs = []\nxs.append(3)\nxs.append(\"a\")\nxs.append(1.5)\nxs.append(None)\nxs.append([1])\nprint(1 if xs[0] > \"z\" else 0)\nprint(1 if xs[1] > \"A\" else 0)\nprint(1 if xs[2] > 1 else 0)\n"
	res, err := Compile(src)
	if err != nil {
		t.Fatalf("refused: %v", err)
	}
	for _, want := range []string{
		// the arms are chosen by the tag the object carries
		"= icmp eq i32 %t", "br i1",
		// two numbers go through the float unbox, two texts through strcmp
		"call double @rt_float_of(", "call i32 @rt_str_order(",
		"= sitofp i32",
		// and the raise arm is one sentence per kind, not one sentence for everything
		"not supported between instances of 'int' and 'str'",
		"not supported between instances of 'bool' and 'str'",
		"not supported between instances of 'float' and 'str'",
		"not supported between instances of 'NoneType' and 'str'",
		"not supported between instances of 'list' and 'str'",
		"not supported between instances of 'dict' and 'str'",
		"not supported between instances of 'set' and 'str'",
	} {
		if !strings.Contains(res.IR, want) {
			t.Errorf("the module does not contain %q:\n%s", want, res.IR)
		}
	}
	for i, ln := range strings.Split(res.IR, "\n") {
		if strings.Contains(ln, "i32 @.") {
			t.Errorf("line %d keeps a global in a value position: %s", i+1, ln)
		}
	}
}
