package lang

// pkg/lang/abs_kind_test.go — `abs` asks what kind its operand is, on both backends, in the same door the
// unary minus asks it (roadmap Gap R.140, ADR 0271).
//
// The reference stops on the absolute value of anything without a sign: `TypeError: bad operand type for
// abs(): 'str'`, `'NoneType'`, `'list'`, `'dict'`, `'set'`, `'C'`. Every one of them was answered here by a
// *number at exit 0*: the interpreter handed the operand straight to its int evaluator, so a text reached
// `abs` holding the interned index it is stored as and `print(abs("hi"))` printed `hi` — the index rendered
// back through the print door — while the compiled backend wrote `sub i32 0, @.str1` and printed `0`. Two
// of the shapes went one step further and had `llc` reject the module: `abs([1])` and `abs({1})` each spent
// exit 2, the contract's "the compiler is broken" code, on a program the reference merely stops on.
//
// The door is not new — ADR 0266 built it for the unary minus, and this file is the same question asked of
// the other builtin that has no meaning for an operand without a sign. Two things had to change for that:
// the sentence table learned `abs`'s own wording (CPython names the *call*, not an operator), and the door
// is asked by both the i32 road and the double road — `print(abs("hi") * 2.5)` had been answering `0.0`,
// because the double door lowers its operand through `floatValue`, which produces nothing for a text.
//
// Four claims, each a different way to be wrong: the parity rows (a sign is a real operation on the kinds
// that have one, so the raise must not be bought by breaking `abs(-3)`, `abs(-3.5)` or `abs(True)`); the
// traps, whose class and message are compared exactly on both engines; the catchability of each raise; and
// the IR shape — the sentence is written per kind reached, in the emitted store-and-branch form, and never
// as a front-end refusal for a program the reference traps on.
//
// The three-engine comparison against CPython, through the CLI, lives in integration/abs_kind_test.go.

import (
	"strings"
	"testing"
)

// TestAbsOfANumberStillAnswersOnBothBackends is the family that must keep answering.
func TestAbsOfANumberStillAnswersOnBothBackends(t *testing.T) {
	for _, tc := range []struct{ name, src, want string }{
		{"a negative literal", "print(abs(-3))\n", "3\n"},
		{"a positive literal", "print(abs(4))\n", "4\n"},
		{"a negative float", "print(abs(-3.5))\n", "3.5\n"},
		{"a positive float", "print(abs(2.5))\n", "2.5\n"},
		{"a verdict is a number", "print(abs(True))\n", "1\n"},
		{"a computed negative", "x = 3\nprint(abs(-x))\n", "3\n"},
		{"an expression argument", "print(abs(1 - 5))\n", "4\n"},
		{"a float argument", "print(abs(1.0 - 2.5))\n", "1.5\n"},
		{"a variable that changes kind of number", "x = 3\nx = -4\nprint(abs(x))\n", "4\n"},
		{"a slot of a literal list", "xs = [3, -4]\nprint(abs(xs[1]))\n", "4\n"},
		{"a slot of a container the program built", "xs = []\nxs.append(-9)\nprint(abs(xs[0]))\n", "9\n"},
		{"a dict value", "d = {\"k\": -6}\nprint(abs(d[\"k\"]))\n", "6\n"},
		{"a function parameter", "def f(v):\n    return abs(v)\n\nprint(f(-5))\n", "5\n"},
		{"in a comprehension", "print([abs(v) for v in [-1, 2, -3]])\n", "[1, 2, 3]\n"},
		{"nested", "print(abs(abs(-9)))\n", "9\n"},
		{"inside a sum", "print(sum([abs(-3), abs(2)]))\n", "5\n"},
		{"over min", "print(abs(min(-4, -2)))\n", "4\n"},
		{"the answer of true division", "print(abs(-7 / 2))\n", "3.5\n"},
		{"in a while body", "i = -2\nwhile i < 0:\n    print(abs(i))\n    i = i + 1\n", "2\n1\n"},
		{"abs of a length", "print(abs(len(\"abc\")))\n", "3\n"},
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

// TestAbsOfANonNumberRaisesTheReferenceSentenceOnBothBackends is the row itself: the class is what a handler
// matches and the message is what a user searches for, so both are compared exactly, on both engines —
// including the interpreted one, whose half of the defect the compiled path had been pinning alone.
func TestAbsOfANonNumberRaisesTheReferenceSentenceOnBothBackends(t *testing.T) {
	for _, tc := range []struct{ name, src, class, message string }{
		{
			"a text literal — the row's own shape",
			"print(abs(\"hi\"))\n",
			"TypeError", "bad operand type for abs(): 'str'",
		},
		{
			"a name that holds a text",
			"x = \"hi\"\nprint(abs(x))\n",
			"TypeError", "bad operand type for abs(): 'str'",
		},
		{
			"a call that answers text",
			"def f():\n    return \"hi\"\n\nprint(abs(f()))\n",
			"TypeError", "bad operand type for abs(): 'str'",
		},
		{
			"a parameter the caller filled with text",
			"def g(v):\n    print(abs(v))\n\ng(\"hi\")\n",
			"TypeError", "bad operand type for abs(): 'str'",
		},
		{
			"a slot of a literal list of texts",
			"xs = [\"a\"]\nprint(abs(xs[0]))\n",
			"TypeError", "bad operand type for abs(): 'str'",
		},
		{
			"a slot of a container the program built",
			"xs = []\nxs.append(\"hi\")\nprint(abs(xs[0]))\n",
			"TypeError", "bad operand type for abs(): 'str'",
		},
		{
			"a character read out of a text",
			"s = \"abc\"\nprint(abs(s[1]))\n",
			"TypeError", "bad operand type for abs(): 'str'",
		},
		{
			"a text built at run time",
			"x = \"ab\"\nprint(abs(x + \"c\"))\n",
			"TypeError", "bad operand type for abs(): 'str'",
		},
		{
			"None",
			"print(abs(None))\n",
			"TypeError", "bad operand type for abs(): 'NoneType'",
		},
		{
			"a list literal — the shape that had llc rejecting the module",
			"print(abs([1, 2]))\n",
			"TypeError", "bad operand type for abs(): 'list'",
		},
		{
			"a dict literal",
			"print(abs({\"a\": 1}))\n",
			"TypeError", "bad operand type for abs(): 'dict'",
		},
		{
			"a set literal — the other exit-2 shape",
			"print(abs({1, 2}))\n",
			"TypeError", "bad operand type for abs(): 'set'",
		},
		{
			"a container variable",
			"xs = [1, 2]\nprint(abs(xs))\n",
			"TypeError", "bad operand type for abs(): 'list'",
		},
		{
			"an instance names its own class",
			"class Token:\n    text = \"t\"\n\nprint(abs(Token()))\n",
			"TypeError", "bad operand type for abs(): 'Token'",
		},
		{
			// The double door is a second lowering of the same call: the operand reaches `floatValue`,
			// which produces nothing for a text, and the intrinsic used to take an empty operand.
			"a text inside a float expression",
			"print(abs(\"hi\") * 2.5)\n",
			"TypeError", "bad operand type for abs(): 'str'",
		},
		{
			"a name whose latest binding is a container",
			"x = \"a\"\nx = [1]\nprint(abs(x))\n",
			"TypeError", "bad operand type for abs(): 'list'",
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
			if strings.Contains(out, "COMPILER BUG") || strings.Contains(out, "LLVM ERROR") {
				t.Errorf("the trap became a toolchain failure:\n%s", out)
			}
		})
	}
}

// TestTheAbsTrapIsCatchableOnBothBackends: the raise leaves through the emitted store-and-branch, so the arm
// the program wrote runs — the compiled half is the one that decides, because a raise a helper performed
// for itself would be unreachable to the program (ADR 0228, and the reason a raise is chosen over a
// refusal in ADR 0266's argument).
func TestTheAbsTrapIsCatchableOnBothBackends(t *testing.T) {
	for _, tc := range []struct{ name, src, want string }{
		{"a text literal", "try:\n    print(abs(\"hi\"))\nexcept TypeError:\n    print(\"caught\")\nprint(\"after\")\n", "caught\nafter\n"},
		{"None", "try:\n    print(abs(None))\nexcept TypeError:\n    print(\"caught\")\n", "caught\n"},
		{"a list literal", "try:\n    print(abs([1]))\nexcept TypeError:\n    print(\"caught\")\n", "caught\n"},
		{"a slot the literal describes", "xs = [\"a\"]\ntry:\n    print(abs(xs[0]))\nexcept TypeError:\n    print(\"caught\")\n", "caught\n"},
		{"a slot only the object describes", "xs = []\nxs.append(\"hi\")\ntry:\n    print(abs(xs[0]))\nexcept TypeError:\n    print(\"caught\")\n", "caught\n"},
		{
			"the raise skips what follows it",
			"try:\n    print(abs(\"hi\"))\n    print(\"not reached\")\nexcept TypeError:\n    print(\"caught\")\n",
			"caught\n",
		},
		{
			"a value around it never runs either",
			"try:\n    print(abs(\"hi\") * 2.5)\nexcept TypeError:\n    print(\"caught\")\n",
			"caught\n",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if out := captureStdout(t, tc.src); out != tc.want {
				t.Errorf("interpreter: stdout %q, want %q\nsrc: %s", out, tc.want, tc.src)
			}
			res, err := Compile(tc.src)
			if err != nil {
				t.Fatalf("compiled leg refused a catchable program: %v\n%s", err, tc.src)
			}
			assertNoForbiddenIR(t, tc.src, res.IR)
			if out := runIR(t, res.IR); out != tc.want {
				t.Errorf("compiled: stdout %q, want %q\nsrc: %s", out, tc.want, tc.src)
			}
		})
	}
}

// TestTheAbsRaiseIsWrittenPerKindAndNotPerProgram is the IR half. Every raise in this language is the
// store-and-branch the emitted code owns; a module that never abses a non-number must not carry the
// sentence at all (ADR 0173/0192's cost rule), and a module that abses two kinds carries two names — which
// is also what lets each raise name the kind the *slot* really holds rather than one word for all of them.
func TestTheAbsRaiseIsWrittenPerKindAndNotPerProgram(t *testing.T) {
	ir := mustCompileIR(t, "print(abs(\"hi\"))\n")
	for _, want := range []string{
		"TypeError: bad operand type for abs(): 'str'",
		"store i32 1, i32* @exn_flag",
		"i32* @exn_code",
		"@exn_msg",
	} {
		if !strings.Contains(ir, want) {
			t.Errorf("the abs raise is missing %s:\n%s", want, ir)
		}
	}
	// A module that only abses numbers carries neither the sentence nor the raise machinery for it.
	clean := mustCompileIR(t, "print(abs(-3))\nprint(abs(-2.5))\n")
	if strings.Contains(clean, "bad operand type for abs()") {
		t.Errorf("a module that never abses a non-number carries the sentence:\n%s", clean)
	}
	// Two kinds reached, two sentences: the raise is per kind, so the message always names what the
	// program really holds.
	two := mustCompileIR(t, "print(abs(\"hi\"))\nprint(abs(None))\n")
	for _, want := range []string{"'str'", "'NoneType'"} {
		if !strings.Contains(two, "bad operand type for abs(): "+want) {
			t.Errorf("the two-kind module is missing abs(): %s\n%s", want, two)
		}
	}
}

// TestAbsAsksTheSameQuestionTheNegationAsks is the shared-door row: one predicate names an operand for both
// operators, so the two sentences must agree about the kind. A change that gives `abs` its own table would
// fail here rather than drift silently into a second truthiness table (the class of bug ADR 0266/0269 both
// exist to end).
func TestAbsAsksTheSameQuestionTheNegationAsks(t *testing.T) {
	for _, src := range []string{
		"print(abs(\"hi\"))\n",
		"print(abs(None))\n",
		"print(abs([1]))\n",
		"x = \"hi\"\nprint(abs(x))\n",
	} {
		t.Run(src, func(t *testing.T) {
			absEE := trapRun(t, src)
			negSrc := strings.Replace(src, "abs(", "-(", 1)
			negEE := trapRun(t, negSrc)
			if absEE.ExnType != "TypeError" || negEE.ExnType != "TypeError" {
				t.Fatalf("one of the two doors stopped raising: abs %q, neg %q", absEE.ExnType, negEE.ExnType)
			}
			absKind := absEE.ExnMsg[strings.LastIndex(absEE.ExnMsg, "'"):]
			negKind := negEE.ExnMsg[strings.LastIndex(negEE.ExnMsg, "'"):]
			if absKind != negKind {
				t.Errorf("the two doors name the same operand differently: abs %q vs neg %q", absKind, negKind)
			}
		})
	}
}
