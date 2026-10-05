package lang

// pkg/lang/logic_value.go tests — `and`/`or` choose an **operand**, not a verdict (roadmap Gap R.147,
// ADR 0269).
//
// CPython's `and` and `or` are the two operators that are not operators: they test the left operand and hand
// back whichever operand the test chose, unconverted. Both backends used to answer the verdict — `print(2
// and 3)` said `1` and `print("" or "d")` said `1`, at exit 0, no diagnostic, on an operator every Python
// program uses. Four things are pinned here, each a different way to be wrong:
//
//   - the parity rows — the chosen operand answers, in its own representation, on both engines;
//   - the conservative rows — `1 or True` is the number `1` and `True or 1` is the verdict `True`, the pair
//     ADR 0261 refuses to break, asked of the operand the test chose rather than of the operator;
//   - the condition rows — a condition asks only whether the answer is true, so two operands that share no
//     word still branch there, and the value door's refusal must not reach them;
//   - the refusals — a chosen operand whose kind the module cannot state is refused in words naming the tag,
//     never answered with the number underneath (ADR 0166), and never with exit 2.
//
// The three-engine comparison through the CLI, and the parity program that carries the whole family, live
// in integration/logic_value_test.go and integration/programs/and_or_answer_like_python.gy.

import (
	"strings"
	"testing"
)

// TestAndOrAnswerWithTheOperandTheTestChose is the row: the operand the test picks is the answer, on both
// engines, and it keeps the representation it arrived with.
func TestAndOrAnswerWithTheOperandTheTestChose(t *testing.T) {
	for _, tc := range []struct{ name, src, want string }{
		{"an int picks the right operand", "print(2 and 3)\n", "3\n"},
		{"a falsy int keeps itself", "print(0 and 3)\n", "0\n"},
		{"an or takes the fallback", "print(0 or 5)\n", "5\n"},
		{"an or keeps a truthy first operand", "print(7 or 0)\n", "7\n"},
		{"zero and zero", "print(0 and 0)\n", "0\n"},
		{"an int variable takes the operand", "x = 1\ny = 2\nprint(x and y)\n", "2\n"},
		{"an int variable keeps itself when falsy", "x = 0\ny = 2\nprint(x and y)\n", "0\n"},
		{"a text default", "print(\"\" or \"d\")\n", "d\n"},
		{"a text keeps itself", "print(\"a\" or \"d\")\n", "a\n"},
		{"chained and", "print(\"a\" and \"b\" and \"c\")\n", "c\n"},
		{"chained or", "print(0 or \"\" or \"x\")\n", "x\n"},
		{"a float answer keeps the double", "print(1.5 and 2.5)\n", "2.5\n"},
		{"the double a data import declares", "import math\nx = 0\nprint(x or math.PI)\n", "3.141592653589793\n"},
		{"a module constant as the operand a test keeps", "import math\nx = 1\nprint(x and math.E)\n", "2.718281828459045\n"},
		{"a falsy float takes the float fallback", "print(0.0 or 2.5)\n", "2.5\n"},
		{"a float chosen by an int operand", "print(2 and 0.0)\n", "0.0\n"},
		{"a list literal picks the right container", "print([1] and [2])\n", "[2]\n"},
		{"an empty list takes the fallback container", "print([] or [1, 2])\n", "[1, 2]\n"},
		{"a container variable or a text", "xs = [1, 2]\nprint(xs or \"empty\")\n", "[1, 2]\n"},
		{"an empty container falls back to text", "d = {}\nprint(d or \"empty\")\n", "empty\n"},
		{"None takes the fallback", "print(None or 3)\n", "3\n"},
		{"None is the fallback", "print(0 or None)\n", "None\n"},
		{"a run-time test between texts", "x = \"\"\nprint(x or \"d\")\n", "d\n"},
		{"a run-time test keeps the text", "x = \"abc\"\nprint(x or \"d\")\n", "abc\n"},
		{"a run-time test between numbers", "x = 3\nprint(x or 4)\n", "3\n"},
		{"a run-time float fallback", "x = 0\nprint(x or 2.5)\n", "2.5\n"},
		{"a float variable and a float literal", "x = 1.5\nprint(x or 2.5)\n", "1.5\n"},
		{"inside an arithmetic operand", "print((2 and 3) + 1)\n", "4\n"},
		{"inside a product", "print((0 or 2) * 3)\n", "6\n"},
		{"as a container element", "x = 0\nprint([x or 1])\n", "[1]\n"},
		{"as a set element", "x = 0\nprint({x or 1})\n", "{1}\n"},
		{"bound to a name", "x = 3\ny = x or 4\nprint(y)\n", "3\n"},
		{"a comparison's answer", "print((1 < 2) and (3 < 4))\n", "True\n"},
		{"a comparison and a number", "print((1 < 2) and (3 < 4) + 1)\n", "2\n"},
		{"a function's return", "def pick(a, b):\n    return a or b\n\nprint(pick(0, 7))\n", "7\n"},
		{"a pair-bound name as the operand", "xs = []\nxs.append([7, 8])\nn = xs[0][0] * 2\nprint(n and 3)\n", "3\n"},
		{"the pair-bound name answers when the test keeps it", "xs = []\nxs.append([7, 8])\nn = xs[0][0] * 2\nprint(1 and n)\n", "14\n"},
		{"in a print with other arguments", "print(2 and 3, \"\" or \"d\", [1] and [2])\n", "3 d [2]\n"},
		{"a mixed-list slot chooses its operand", "ys = [True, 1]\nprint(ys[0] and ys[1])\n", "1\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			out := captureStdout(t, tc.src)
			if out != tc.want {
				t.Errorf("interpreter: exit 0 stdout %q, want %q\nsrc: %s", out, tc.want, tc.src)
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

// TestTheOperandTheTestChoseDecidesTheRendering keeps ADR 0261's pair intact: the verdict-ness of the answer
// is a fact about the operand the test chose, not about the operator. `True or 1` is the verdict True because
// the left operand won; `1 or True` is the number 1 because the other one did. Reading the operator instead
// is what made both of them `1`, and promoting every chosen operand to a verdict would break the second row
// — the row ADR 0261 has kept unbroken since it landed.
func TestTheOperandTheTestChoseDecidesTheRendering(t *testing.T) {
	for _, tc := range []struct{ name, src, want string }{
		{"a verdict chosen by a constant test", "print(True and False)\n", "False\n"},
		{"a verdict taken by an or", "print(False or True)\n", "True\n"},
		{"the left operand wins an or", "print(True or 1)\n", "True\n"},
		{"the right operand loses an or", "print(1 or True)\n", "1\n"},
		{"a verdict-bound name wins the test", "x = True\nprint(x or 2)\n", "True\n"},
		{"an int-bound name wins the test", "x = 1\nprint(x or True)\n", "1\n"},
		{"a verdict in a container element", "x = False\nprint([x or True])\n", "[True]\n"},
		{"str() of a chosen verdict", "print(str(0 or True))\n", "True\n"},
		{"both operands verdicts", "print((1 < 2) or (3 < 2))\n", "True\n"},
		{"a verdict and a number print the number", "print(0 or 2)\n", "2\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			out := captureStdout(t, tc.src)
			if out != tc.want {
				t.Errorf("interpreter: exit 0 stdout %q, want %q\nsrc: %s", out, tc.want, tc.src)
			}
			res, err := Compile(tc.src)
			if err != nil {
				t.Fatalf("compiled leg refused a program the oracle prints (%v): %s", err, tc.src)
			}
			if got := runIR(t, res.IR); got != tc.want {
				t.Errorf("compiled: stdout %q, want %q\nsrc: %s", got, tc.want, tc.src)
			}
		})
	}
}

// TestAConditionAsksOnlyWhetherTheAnswerIsTrue keeps the condition door open. `truth(a and b)` is
// `truth(a) and truth(b)`, so a condition never needs the two operands to share a word — including the
// shapes the value door refuses. If these rows started refusing, the value door had leaked into the head of
// an `if`, which is the wrong question asked at the wrong place.
func TestAConditionAsksOnlyWhetherTheAnswerIsTrue(t *testing.T) {
	for _, tc := range []struct{ name, src, want string }{
		{"two numbers", "x = 3\ny = 4\nif x and y:\n    print(\"both\")\n", "both\n"},
		{"a falsy number short of the branch", "x = 0\ny = 4\nif x and y:\n    print(\"both\")\nelse:\n    print(\"neither\")\n", "neither\n"},
		{"a number and a text", "x = 1\ny = \"t\"\nif x and y:\n    print(\"both\")\n", "both\n"},
		{"an empty text fails the test", "x = 1\ny = \"\"\nif x and y:\n    print(\"both\")\nelse:\n    print(\"empty\")\n", "empty\n"},
		{"an or of a number and a text", "x = 0\ny = \"d\"\nif x or y:\n    print(\"truthy\")\n", "truthy\n"},
		{"a container and a number", "xs = [1]\nn = 2\nif xs or n:\n    print(\"yes\")\n", "yes\n"},
		{"an empty container and zero", "xs = []\nn = 0\nif xs or n:\n    print(\"yes\")\nelse:\n    print(\"no\")\n", "no\n"},
		{"comparisons composed", "a = 1\nb = 2\nif a < b and b < 9:\n    print(\"chain\")\n", "chain\n"},
		{"a while head", "i = 3\nt = 0\nwhile i and t < 6:\n    t = t + i\n    i = i - 1\nprint(t)\n", "6\n"},
		{"a ternary test", "x = 3\nprint(1 if x and 1 else 0)\n", "1\n"},
		{"None fails the test", "x = None\nif x and 1:\n    print(\"yes\")\nelse:\n    print(\"no\")\n", "no\n"},
		{"a float's zero", "x = 0.0\nif x or 2.5:\n    print(\"fallback is truthy\")\n", "fallback is truthy\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			out := captureStdout(t, tc.src)
			if out != tc.want {
				t.Errorf("interpreter: exit 0 stdout %q, want %q\nsrc: %s", out, tc.want, tc.src)
			}
			res, err := Compile(tc.src)
			if err != nil {
				t.Fatalf("compiled leg refused a condition the oracle answers (%v): %s", err, tc.src)
			}
			if got := runIR(t, res.IR); got != tc.want {
				t.Errorf("compiled: stdout %q, want %q\nsrc: %s", got, tc.want, tc.src)
			}
		})
	}
}

// TestTheFoldAnswersTheOperandAndNotTheVerdict reaches the two constant folders. A fold that returns the
// verdict is worse than the door's wrong answer because it disappears into an index, a repeat count or a
// constant argument, where no later pass can see the operator that invented it.
func TestTheFoldAnswersTheOperandAndNotTheVerdict(t *testing.T) {
	for _, tc := range []struct{ name, src, want string }{
		{"an index computed by and", "xs = [10, 20, 30]\nprint(xs[1 and 2])\n", "30\n"},
		{"an index computed by or", "xs = [10, 20, 30]\nprint(xs[0 or 1])\n", "20\n"},
		{"a constant product", "print((2 and 3) * (0 or 4))\n", "12\n"},
		{"a product that folds", "print((2 and 3) ** 2)\n", "9\n"},
		{"a condition that folds", "print(1 if 1 and 0 else 9)\n", "9\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			out := captureStdout(t, tc.src)
			if out != tc.want {
				t.Errorf("interpreter: exit 0 stdout %q, want %q\nsrc: %s", out, tc.want, tc.src)
			}
			res, err := Compile(tc.src)
			if err != nil {
				t.Fatalf("compiled leg refused a program the oracle prints (%v): %s", err, tc.src)
			}
			if got := runIR(t, res.IR); got != tc.want {
				t.Errorf("compiled: stdout %q, want %q\nsrc: %s", got, tc.want, tc.src)
			}
		})
	}
}

// TestTheShapesWhoseAnswerHasNoWordRefuseThemInWords is the honest half of ADR 0166 applied to an operator:
// a chosen operand whose kind the module cannot state is *refused*, named by both operands and by the word
// that is missing. Before this cycle each of these printed a number the language never produced, at exit 0.
func TestTheShapesWhoseAnswerHasNoWordRefuseThemInWords(t *testing.T) {
	for _, tc := range []struct{ name, src, want string }{
		{"a text bound to a name", "x = 0\nz = x or \"d\"\nprint(z)\n", "`x or \"d\"` chooses between two values"},
		{"a text as a container element", "x = 0\nw = [x or \"b\"]\nprint(w)\n", "chooses between two values"},
		{"a float among integers", "x = 0\nprint((x or 2.5) * 2)\n", "`x or 2.5` chooses between two values"},
		{"a container bound to a name", "x = 0\nys = x or [[1, 2]]\nprint(ys)\n", "chooses between two values"},
		{"two containers in a value position", "xs = [1]\nys = [2]\nc = xs and ys\nprint(c)\n", "chooses between two values"},
		// `math.PI` used to sit in the refusal table below: the pass could not name its kind, so
		// `x or math.PI` had no word to hold. It can now — a data import declares the type
		// (roadmap L11.6, ADR 0272) — and the answer is the reference's own number.
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := Compile(tc.src)
			if err == nil {
				t.Fatalf("%q compiled; the answer has no word and the door must refuse it (roadmap Gap R.147, ADR 0269)\nsrc: %s", tc.src, tc.src)
			}
			msg := err.Error()
			for _, want := range []string{tc.want, "roadmap L11.1", "Gap R.147"} {
				if !strings.Contains(msg, want) {
					t.Errorf("refusal %q does not mention %q", msg, want)
				}
			}
			if strings.Contains(msg, "LLVM ERROR") || strings.Contains(msg, "verifier") || strings.Contains(msg, "llc") {
				t.Fatalf("%q failed as an IR problem instead of a front-end refusal: %v", tc.src, err)
			}
		})
	}
}

// TestTheModuleSelectsTheOperandAndItsKind asks the module rather than the answer: the print door's pair is
// two `select`s over one test — the payload *and* the tag — and the value door's is a select in the word
// both operands share. A regression to the old lowering would leave a `zext i1` of a composed verdict here,
// which is exactly the instruction that printed `1` for `2 and 3`.
func TestTheModuleSelectsTheOperandAndItsKind(t *testing.T) {
	mixed := "x = 0\nprint(x or \"d\")\n"
	res, err := Compile(mixed)
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	for _, want := range []string{"select i1", "call void @rt_print_mixed_value(i32"} {
		if !strings.Contains(res.IR, want) {
			t.Errorf("the module does not carry %q; the chosen operand was rendered without its kind:\n%s", want, res.IR)
		}
	}

	same := "x = 1\ny = 2\nprint(x and y)\n"
	res2, err := Compile(same)
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	if !strings.Contains(res2.IR, "select i1") {
		t.Errorf("two operands in one word should be answered by a select:\n%s", res2.IR)
	}

	doubles := "x = 0.0\nprint(x or 2.5)\n"
	res3, err := Compile(doubles)
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	if !strings.Contains(res3.IR, "select i1") || !strings.Contains(res3.IR, "double") {
		t.Errorf("a float answer must travel in a double:\n%s", res3.IR)
	}

	// A program that never uses the pair road must not pay for the tag printer.
	plain := "print(2 and 3)\n"
	res4, err := Compile(plain)
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	if strings.Contains(res4.IR, "call void @rt_print_mixed_value") {
		t.Errorf("a constant test folds to its chosen operand and needs no tag printer:\n%s", res4.IR)
	}
}

// TestTheConditionsAndComparisonsAroundAnOperandChoosingOperatorStillCompose is the regression net for the
// door boundary: `and`/`or` sit inside comparisons, ternaries and loops, and each of those contexts asks its
// own question of the answer rather than assuming the old 0/1.
func TestTheConditionsAndComparisonsAroundAnOperandChoosingOperatorStillCompose(t *testing.T) {
	for _, tc := range []struct{ name, src, want string }{
		{"an and inside a comparison", "x = 0\nprint((x or 1) < 2)\n", "True\n"},
		{"an and as both sides", "print((1 and 2) == 2)\n", "True\n"},
		{"an or inside a while head", "x = 0\nwhile (x or 3) < 5:\n    print(x)\n    x = x + 1\n", "0\n1\n2\n3\n4\n"},
		{"a ternary whose arms are ands", "print((1 and 2) if 1 else (3 or 4))\n", "2\n"},
		{"nested in a comprehension filter", "print([v for v in [0, 1, 2] if v and 1])\n", "[1, 2]\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			out := captureStdout(t, tc.src)
			if out != tc.want {
				t.Errorf("interpreter: exit 0 stdout %q, want %q\nsrc: %s", out, tc.want, tc.src)
			}
			res, err := Compile(tc.src)
			if err != nil {
				t.Fatalf("compiled leg refused a program the oracle prints (%v): %s", err, tc.src)
			}
			if got := runIR(t, res.IR); got != tc.want {
				t.Errorf("compiled: stdout %q, want %q\nsrc: %s", got, tc.want, tc.src)
			}
		})
	}
}

// TestTheConstantTestDropsTheOperandTheReferenceDrops is the half of short-circuiting this language *can*
// answer — a test the source wrote — pinned on both engines so the owed half (roadmap Gap R.149) cannot
// quietly widen: the compiled `select` never emits the operand it cannot take, and the interpreter must not
// run it either, or the two backends disagree about the effects.
func TestTheConstantTestDropsTheOperandTheReferenceDrops(t *testing.T) {
	falsy := "def boom():\n    print(\"boom\")\n    return 9\n\nprint(0 and boom())\n"
	if out := captureStdout(t, falsy); out != "0\n" {
		t.Errorf("interpreter ran the operand a constant test rejected: %q", out)
	}
	res, err := Compile(falsy)
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	if got := runIR(t, res.IR); got != "0\n" {
		t.Errorf("compiled ran the operand a constant test rejected: %q", got)
	}

	truthy := "def boom():\n    print(\"boom\")\n    return 9\n\nprint(1 or boom())\n"
	if out := captureStdout(t, truthy); out != "1\n" {
		t.Errorf("interpreter ran the operand a constant test rejected: %q", out)
	}
	res2, err := Compile(truthy)
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	if got := runIR(t, res2.IR); got != "1\n" {
		t.Errorf("compiled ran the operand a constant test rejected: %q", got)
	}
}
