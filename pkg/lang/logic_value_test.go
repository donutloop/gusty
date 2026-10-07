package lang

// pkg/lang/logic_value.go tests — `and`/`or` choose an **operand**, not a verdict (roadmap Gap R.147,
// ADR 0269).
//
// CPython's `and` and `or` are the two operators that are not operators: they test the left operand and hand
// back whichever operand the test chose, unconverted. Both backends used to answer the verdict — `print(2
// and 3)` said `1` and `print("" or "d")` said `1`, at exit 0, no diagnostic, on an operator every Python
// program uses. Four things are pinned here, each a different way to be wrong:
//
//   - the parity rows — the chosen operand answers, in its own representation, on both legs;
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

// TestTheConstantTestDropsTheOperandTheReferenceDrops is the trivial case of the rule below — a test the
// source wrote — pinned separately because it is decided by the fold (`constantLogicArm`) and not by the
// blocks: the operand the test cannot reach is not in the program, and the literal that made the test
// decidable has no effects to keep.
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

// --- the operand the test did not choose (roadmap Gap R.149, ADR 0275) ---------
//
// `and`/`or` test one operand and then look at the other. Both engines used to look at both and choose
// afterwards, because that is what a `select` is: `print(x and boom())` printed `boom` with x bound to 0,
// `print(x and (1 // 0))` died with ZeroDivisionError where the reference answers 0, `if x and boom():` ran
// the body's test with an operand a condition never reaches — and, the half only the compiled leg had,
// `print(boom() and 2)` called `boom` twice, because the operand's truth and its value were each lowered
// separately. The cure is three blocks and a phi (`logicSkeleton`), plus asking the *expression* for the
// truth of an operand it has already evaluated instead of evaluating it again.

const shortCircuitBoom = "def boom():\n    print(\"boom\")\n    return 9\n\n"

// TestTheOperandTheTestDidNotChooseIsNeverRunOnTheCompiledBackend is the row: the effects, the traps and the
// count are the reference's, on the record and in the compiled module.
func TestTheOperandTheTestDidNotChooseIsNeverRunOnTheCompiledBackend(t *testing.T) {
	for _, tc := range []struct{ name, src, want string }{
		{"a falsy test skips a call", shortCircuitBoom + "x = 0\nprint(x and boom())\n", "0\n"},
		{"a truthy test skips the fallback", shortCircuitBoom + "y = 1\nprint(y or boom())\n", "1\n"},
		{"a skipped division does not trap", "x = 0\nprint(x and (1 // 0))\n", "0\n"},
		{"a skipped division on the other side", "y = 1\nprint(y or (1 // 0))\n", "1\n"},
		{"a binding skips too", shortCircuitBoom + "x = 0\nv = x and boom()\nprint(v)\n", "0\n"},
		{"a binding keeps what the test chose", shortCircuitBoom + "y = 1\nw = y or boom()\nprint(w)\n", "1\n"},
		{"an if head does not run the skipped operand", shortCircuitBoom + "x = 0\nif x and boom():\n    print(\"then\")\nprint(\"done\")\n", "done\n"},
		{"an or head does not run the skipped operand", shortCircuitBoom + "y = 1\nif y or boom():\n    print(\"taken\")\n", "taken\n"},
		{"a while head never enters", shortCircuitBoom + "x = 0\nwhile x and boom():\n    print(\"loop\")\nprint(\"ended\")\n", "ended\n"},
		{"the operand the test reaches runs once", shortCircuitBoom + "print(boom() and 2)\n", "boom\n2\n"},
		{"the tested operand runs once", shortCircuitBoom + "print(boom() or 2)\n", "boom\n9\n"},
		{"twice in one line, twice in the module", shortCircuitBoom + "x = 0\nprint(x and boom(), x and boom())\n", "0 0\n"},
		{"a chain runs only what it reaches", shortCircuitBoom + "x = 0\ny = 0\nprint(x or y or boom())\n", "boom\n9\n"},
		{"a chain stops at the first answer", shortCircuitBoom + "y = 2\nprint(y or boom() or 3)\n", "2\n"},
		{"a condition chain skips the second test", shortCircuitBoom + "x = 0\ny = 1\nif x and boom() and y:\n    print(\"y\")\nelse:\n    print(\"n\")\n", "n\n"},
		{"a truthy text skips the fallback", shortCircuitBoom + "s = \"x\"\nprint(s or boom())\n", "x\n"},
		{"a truthy container skips the fallback", shortCircuitBoom + "xs = [1]\nprint(xs or boom())\n", "[1]\n"},
		{"None skips the fallback", shortCircuitBoom + "print(None and boom())\n", "None\n"},
		{"a call's own arguments still run", shortCircuitBoom + "def pick(a, b):\n    return a and b\n\nprint(pick(0, boom()))\n", "boom\n0\n"},
		// The truth of an operand whose kind only the object carries is a run-time question, and the
		// branch has to ask the same table the printer and the comparison do — @rt_pair_truth.
		{"a tagged slot that is true skips the fallback", shortCircuitBoom + "ys = [True, 1]\nprint(ys[0] or boom())\n", "True\n"},
		{"a tagged slot that is false takes it", shortCircuitBoom + "ys = [0, 1]\nprint(ys[0] or boom())\n", "boom\n9\n"},
		{"a text slot's emptiness decides", shortCircuitBoom + "d = {}\nd[\"k\"] = \"\"\nprint(d[\"k\"] or boom())\n", "boom\n9\n"},
		{"a container slot's emptiness decides", shortCircuitBoom + "d = {}\nd[\"k\"] = []\nprint(d[\"k\"] or \"fallback\")\n", "fallback\n"},
		{"an operand the test reaches still runs in a container", shortCircuitBoom + "y = 1\nprint([y and boom()])\n", "boom\n[9]\n"},
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

// TestTheSkippedOperandIsNotInTheCompiledModule is the IR half of the row. Running the program proves the
// bytes came out right; this proves *why*: the skipped operand has no instruction in the module, the
// operand it does reach has exactly one, and the merge is a branch-and-phi rather than the `select` that
// made both operands run.
// functionHolding returns the body of the `define` whose lines contain marker — the unit of user code an
// and/or was lowered into, as opposed to the runtime helper blocks the module carries beside it.
func functionHolding(ir, marker string) string {
	i := strings.Index(ir, marker)
	if i < 0 {
		return ""
	}
	start := strings.LastIndex(ir[:i], "\ndefine ")
	if start < 0 {
		return ""
	}
	rest := ir[start:]
	end := strings.Index(rest, "\n}\n")
	if end < 0 {
		return rest
	}
	return rest[:end]
}

func TestTheSkippedOperandIsNotInTheCompiledModule(t *testing.T) {
	for _, tc := range []struct {
		name, src, fn string
		calls         int
		guarded       bool
		want          []string
	}{
		{
			// The operand the running test skips is still in the module — on the path the branch
			// enters — but it is in it *once*. Before the row this program carried the call twice:
			// once from the generic operand lowering and once from the door that selected.
			"a call the test skipped is emitted once, behind the branch",
			shortCircuitBoom + "x = 0\nprint(x and boom())\n", "gy_boom", 1, true,
			[]string{"br i1", "label %logic.rhs", "label %logic.lhs", "label %logic.merge"},
		},
		{
			"a call the test reached is emitted once, not twice",
			shortCircuitBoom + "print(boom() and 2)\n", "gy_boom", 1, false,
			[]string{"br i1", "phi i32", "label %logic.rhsfwd"},
		},
		{
			"both operands of a chain are emitted once each",
			shortCircuitBoom + "print(boom() or boom())\n", "gy_boom", 2, false,
			[]string{"phi i32"},
		},
		{
			// Which arm a tagged slot is — a number, a text, an empty container, None — is the object's
			// fact, so the branch that decides whether the second operand exists asks the same table the
			// printer and the comparison read their tag from.
			"a tagged test asks the runtime door",
			shortCircuitBoom + "ys = [True, 1]\nprint(ys[0] or boom())\n", "gy_boom", 1, true,
			[]string{"@rt_pair_truth", "phi i32", "rt_print_mixed_value"},
		},
		{
			"a condition merges two predicates",
			shortCircuitBoom + "x = 0\nif x and boom():\n    print(\"then\")\n", "gy_boom", 1, true,
			[]string{"br i1", "phi i1"},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			res, err := Compile(tc.src)
			if err != nil {
				t.Fatalf("compile: %v\n%s", err, tc.src)
			}
			assertNoForbiddenIR(t, tc.src, res.IR)
			call := "call i32 @" + tc.fn + "("
			if got := strings.Count(res.IR, call); got != tc.calls {
				t.Errorf("the module calls @%s %d times, want %d\n%s", tc.fn, got, tc.calls, res.IR)
			}
			if tc.guarded {
				// The call is only on the path the branch enters: it sits after the right operand's
				// block opens, and that block is reached only from the test.
				if i, j := strings.Index(res.IR, "logic.rhs"), strings.Index(res.IR, call); i < 0 || j < i {
					t.Errorf("%q does not open before the call — the operand is not behind the branch\n%s", "logic.rhs", res.IR)
				}
			}
			for _, w := range tc.want {
				if !strings.Contains(res.IR, w) {
					t.Errorf("the module is missing %q\n%s", w, res.IR)
				}
			}
			// The shape that made the bug: a `select` between two operands evaluates both. Asked of
			// the function that holds the operator — the runtime helpers select legitimately, and the
			// min/max fold and a ternary's arms are allowed to; what may not come back is a select
			// between the two operands of an `and`/`or`.
			if body := functionHolding(res.IR, "logic.merge"); body == "" {
				t.Errorf("no function holds the operator's blocks\n%s", res.IR)
			} else {
				for _, ln := range strings.Split(body, "\n") {
					if strings.Contains(ln, "select i1") {
						t.Errorf("the operator still chooses an operand with a select: %s\n%s", ln, tc.src)
					}
				}
			}
		})
	}
}

// TestTheOperandTheTestReachedStillTraps keeps the other half of the promise: skipping what the test
// excluded must not swallow what it included. The division the program reached raises the reference's
// error, catchably, on both legs — otherwise the branch would be a way to lose a trap.
func TestTheOperandTheTestReachedStillTrapsOnTheCompiledBackend(t *testing.T) {
	for _, tc := range []struct{ name, src, message string }{
		{
			"a chosen division traps",
			"y = 1\nprint(y and (1 // 0))\n",
			"integer division or modulo by zero",
		},
		{
			"a fallback division traps",
			"x = 0\nprint(x or (1 // 0))\n",
			"integer division or modulo by zero",
		},
		{
			"a chosen division in a condition traps",
			"y = 1\nif y and (1 // 0) == 0:\n    print(\"no\")\n",
			"integer division or modulo by zero",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ee := trapRun(t, tc.src)
			if ee.ExnType != "ZeroDivisionError" {
				t.Errorf("interpreter raised %q, want ZeroDivisionError (msg %q)", ee.ExnType, ee.ExnMsg)
			}
			if ee.ExnMsg != tc.message {
				t.Errorf("interpreter message =\n  %q\nwant\n  %q", ee.ExnMsg, tc.message)
			}
			res, err := Compile(tc.src)
			if err != nil {
				t.Fatalf("the compiled backend refused a program the oracle traps on: %v\n%s", err, tc.src)
			}
			assertNoForbiddenIR(t, tc.src, res.IR)
			out := runIRMayTrap(t, res.IR)
			if !strings.Contains(out, "Traceback (most recent call last):") {
				t.Errorf("the compiled program printed no traceback:\n%s", out)
			}
			if !strings.Contains(out, tc.message) {
				t.Errorf("compiled message missing %q:\n%s", tc.message, out)
			}
			if strings.Contains(out, "codegen:") {
				t.Errorf("the compiled backend refused what the oracle traps on:\n%s", out)
			}
		})
	}
}

// TestTheTrapTheTestSkippedIsNotRaised is the same rule read the other way: the arm the program wrote is
// not entered, because the raise never happened — `except` finds nothing to catch and the line answers.
func TestTheTrapTheTestSkippedIsNotRaised(t *testing.T) {
	for _, tc := range []struct{ name, src, want string }{
		{
			"the skipped division is not caught",
			"x = 0\ntry:\n    print(x and (1 // 0))\nexcept ZeroDivisionError:\n    print(\"caught\")\n",
			"0\n",
		},
		{
			"the other skipped division is not caught",
			"y = 1\ntry:\n    print(y or (1 // 0))\nexcept ZeroDivisionError:\n    print(\"caught\")\n",
			"1\n",
		},
		{
			"the chosen division is caught",
			"y = 1\ntry:\n    print(y and (1 // 0))\nexcept ZeroDivisionError:\n    print(\"caught\")\nprint(\"after\")\n",
			"caught\nafter\n",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			out := captureStdout(t, tc.src)
			if out != tc.want {
				t.Errorf("interpreter: stdout %q, want %q\nsrc: %s", out, tc.want, tc.src)
			}
			res, err := Compile(tc.src)
			if err != nil {
				t.Fatalf("compile: %v\n%s", err, tc.src)
			}
			assertNoForbiddenIR(t, tc.src, res.IR)
			if got := runIR(t, res.IR); got != tc.want {
				t.Errorf("compiled: stdout %q, want %q\nsrc: %s", got, tc.want, tc.src)
			}
		})
	}
}
