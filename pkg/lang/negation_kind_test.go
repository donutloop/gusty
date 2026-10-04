package lang

// pkg/lang/negation_kind_test.go — the unary minus asks what kind its operand is, on both backends
// (roadmap Gaps R.89 and R.137, ADR 0266).
//
// The defect had two halves and they failed differently. The interpreter handed the operand straight to
// its int evaluator, so a text reached `-` holding the interned index it is stored as and `print(-"hi")`
// answered -281474976710658 — the negation of 2^48+2 — at exit 0. The compiled backend wrote
// `sub i32 0, <storage>` for the same expression: 0 for a text or a dict, and for a *list literal* an
// operand that was a global, which `llc` rejects, so a program the reference merely stops on spent exit 2.
//
// Four things are pinned here, and each is a different way to be wrong:
//
//   - the parity rows — a negation whose operand does have a sign still answers, on both engines, so the
//     raise cannot be bought by breaking `-7`, `-1.5`, `-True` or a slot read;
//   - the traps — every shape the reference stops on raises its sentence, on both engines: the
//     interpreter asks the *value*, the module asks the *expression*;
//   - the raise is a program-visible one (`except TypeError:` reaches it), not a runtime abort;
//   - the IR shape — no global sitting in an i32 arithmetic instruction, and the sentence appears once
//     per kind the program can actually reach rather than once for all kinds.
//
// The three-engine comparison against CPython, through the CLI, lives in
// integration/negation_kind_test.go.

import (
	"strings"
	"testing"
)

// negationParity is the family that must keep answering: a sign is a real operation on the kinds that
// have one, and the raise below is worthless if it bought silence by breaking these.
func TestNegationOfANumberStillAnswersOnBothBackends(t *testing.T) {
	for _, tc := range []struct{ name, src, want string }{
		{"an int literal", "print(-7)\n", "-7\n"},
		{"a float literal", "print(-1.5)\n", "-1.5\n"},
		{"a bool is a number", "print(-True)\n", "-1\n"},
		{"an int variable", "x = 5\nprint(-x)\n", "-5\n"},
		{"a float variable", "x = 2.5\nprint(-x)\n", "-2.5\n"},
		{"inside an expression", "print(-7 + 3)\n", "-4\n"},
		{"a parenthesised product", "print(-(2 * 3))\n", "-6\n"},
		{"a slot of a literal list", "xs = [3, 4]\nprint(-xs[1])\n", "-4\n"},
		{"a float slot keeps the float", "xs = [3.5, 4]\nprint(-xs[0])\n", "-3.5\n"},
		{"a slot of a container the program built", "xs = []\nxs.append(9)\nprint(-xs[0])\n", "-9\n"},
		{"a dict value", "d = {\"k\": 6}\nprint(-d[\"k\"])\n", "-6\n"},
		{"in a while head and a body", "i = 3\nwhile i > 0:\n    print(-i)\n    i = i - 1\n", "-3\n-2\n-1\n"},
		{"a function parameter", "def f(x):\n    return -x\n\nprint(f(4))\n", "-4\n"},
		{"in a comprehension", "print([-v for v in [1, 2]])\n", "[-1, -2]\n"},
		{"the answer of true division", "print(-7 / 2)\n", "-3.5\n"},
		{"an abs of a negation", "print(abs(-3))\n", "3\n"},
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

// TestNegationOfANonNumberRaisesTheReferenceSentenceOnBothBackends is the row itself. The class is what a
// handler matches; the message is what a user searches for, so both are compared exactly, on both engines
// — including the interpreted one, whose half of Gap R.89 the compiled path had been pinning alone
// (`aotOnly` in tagged_numeric_test.go).
func TestNegationOfANonNumberRaisesTheReferenceSentenceOnBothBackends(t *testing.T) {
	for _, tc := range []struct{ name, src, class, message string }{
		{
			"a text literal",
			"print(-\"hi\")\n",
			"TypeError", "bad operand type for unary -: 'str'",
		},
		{
			"a name that holds a text",
			"x = \"hi\"\nprint(-x)\n",
			"TypeError", "bad operand type for unary -: 'str'",
		},
		{
			// The interned index was the whole story on the interpreted side: the digits looked like an
			// address because the value negated was the heap handle, not the text.
			"a call that answers text",
			"def f():\n    return \"hi\"\n\nprint(-f())\n",
			"TypeError", "bad operand type for unary -: 'str'",
		},
		{
			"a parameter the caller filled with text",
			"def g(v):\n    print(-v)\n\ng(\"hi\")\n",
			"TypeError", "bad operand type for unary -: 'str'",
		},
		{
			"a slot of a literal list of texts",
			"xs = [\"a\"]\nprint(-xs[0])\n",
			"TypeError", "bad operand type for unary -: 'str'",
		},
		{
			"a slot of a container the program built",
			"xs = []\nxs.append(\"hi\")\nprint(-xs[0])\n",
			"TypeError", "bad operand type for unary -: 'str'",
		},
		{
			"a character read out of a text",
			"s = \"abc\"\nprint(-s[1])\n",
			"TypeError", "bad operand type for unary -: 'str'",
		},
		{
			"None",
			"print(-None)\n",
			"TypeError", "bad operand type for unary -: 'NoneType'",
		},
		{
			"a list literal — the shape that had llc rejecting the module",
			"print(-[1, 2])\n",
			"TypeError", "bad operand type for unary -: 'list'",
		},
		{
			"a dict literal",
			"print(-{\"a\": 1})\n",
			"TypeError", "bad operand type for unary -: 'dict'",
		},
		{
			"a set literal",
			"print(-{1, 2})\n",
			"TypeError", "bad operand type for unary -: 'set'",
		},
		{
			"a container variable",
			"xs = [1, 2]\nprint(-xs)\n",
			"TypeError", "bad operand type for unary -: 'list'",
		},
		{
			"an instance names its own class",
			"class Token:\n    text = \"t\"\n\nprint(-Token())\n",
			"TypeError", "bad operand type for unary -: 'Token'",
		},
		{
			// A float-family container read through a computed index: ADR 0265's door owned this arm and
			// the interpreter's half of it was Gap R.89's `aotOnly` pin.
			"a mixed-kind slot read through a computed index",
			"xs = [1.5, \"a\"]\ni = 1\nprint(-xs[i])\n",
			"TypeError", "bad operand type for unary -: 'str'",
		},
		{
			// The negation of a text inside a float expression still stops the program: the operand
			// never reaches an fsub, whatever the operation around it is.
			"a text negated inside a float expression",
			"print(-\"hi\" + 1.5)\n",
			"TypeError", "bad operand type for unary -: 'str'",
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

// TestTheNegationOfATupleNamesWhatTheReferenceNames pins the one kind this file cannot settle on both
// engines. A tuple literal is built as a *list* object by the interpreter, so its operand-type sentence
// names the representation the interpreter used, where the reference — and this backend's compiled leg —
// name `'tuple'`. The compiled half is compared to the reference; the interpreted half is pinned as it is
// today, because a test that asserted the reference's word here would fail and a test that asserted
// nothing would pass either way (roadmap Gap R.141, waiting on L11.3's tuple object).
func TestTheNegationOfATupleNamesWhatTheReferenceNames(t *testing.T) {
	const src = "print(-(\"a\", 1))\n"
	ee := trapRun(t, src)
	if ee.ExnType != "TypeError" {
		t.Fatalf("interpreter raised %q, want TypeError", ee.ExnType)
	}
	if ee.ExnMsg != "bad operand type for unary -: 'list'" {
		t.Errorf("interpreter message =\n  %q\nwant the pinned Gap R.141 answer\n  %q", ee.ExnMsg, "bad operand type for unary -: 'list'")
	}
	res, err := Compile(src)
	if err != nil {
		t.Fatalf("the compiled backend refused a program the oracle traps on: %v", err)
	}
	assertNoForbiddenIR(t, src, res.IR)
	out := runIRMayTrap(t, res.IR)
	if !strings.Contains(out, "TypeError: bad operand type for unary -: 'tuple'") {
		t.Errorf("the compiled leg did not name 'tuple':\n%s", out)
	}
}

// TestTheNegationTrapIsCatchableOnBothBackends: the raise leaves through the emitted store-and-branch, so
// the arm the program wrote runs — the compiled half is the one that decides, because a raise a helper
// performed for itself would be unreachable to the program (ADR 0228).
func TestTheNegationTrapIsCatchableOnBothBackends(t *testing.T) {
	for _, tc := range []struct{ name, src, want string }{
		{"a text literal", "try:\n    print(-\"hi\")\nexcept TypeError:\n    print(\"caught\")\nprint(\"after\")\n", "caught\nafter\n"},
		{"None", "try:\n    print(-None)\nexcept TypeError:\n    print(\"caught\")\n", "caught\n"},
		{"a list literal", "try:\n    print(-[1])\nexcept TypeError:\n    print(\"caught\")\n", "caught\n"},
		{"a slot the literal describes", "xs = [\"a\"]\ntry:\n    print(-xs[0])\nexcept TypeError:\n    print(\"caught\")\n", "caught\n"},
		{"a slot only the object describes", "xs = []\nxs.append(\"hi\")\ntry:\n    print(-xs[0])\nexcept TypeError:\n    print(\"caught\")\n", "caught\n"},
		{
			// A raise inside `try` skips the rest of the arm and runs the trailer: the negation is a
			// statement's event, not a value that keeps being read.
			"the raise skips what follows it",
			"try:\n    print(-\"hi\")\n    print(\"not reached\")\nexcept TypeError:\n    print(\"caught\")\n",
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

// TestTheNegationRaiseIsWrittenPerKindAndNotPerProgram is the IR half. Every raise in this language is the
// store-and-branch the emitted code owns; a module that never negates a non-number must not carry the
// sentence at all (ADR 0173/0192's cost rule), and a module that negates two kinds carries two names.
func TestTheNegationRaiseIsWrittenPerKindAndNotPerProgram(t *testing.T) {
	ir := mustCompileIR(t, "print(-\"hi\")\n")
	for _, want := range []string{
		"TypeError: bad operand type for unary -: 'str'",
		"store i32 1, i32* @exn_flag",
		"i32* @exn_code",
		"@exn_msg",
	} {
		if !strings.Contains(ir, want) {
			t.Errorf("the negation raise is missing %s:\n%s", want, ir)
		}
	}
	if strings.Contains(ir, "sub i32 0") {
		t.Errorf("a negated text still reached the int road:\n%s", ir)
	}

	two := mustCompileIR(t, "print(-\"hi\")\nprint(-None)\n")
	for _, want := range []string{"unary -: 'str'", "unary -: 'NoneType'"} {
		if !strings.Contains(two, want) {
			t.Errorf("the two-kind module is missing %s", want)
		}
	}

	quiet := mustCompileIR(t, "print(-7)\nprint(-1.5)\nxs = [3, 4]\nprint(-xs[0])\n")
	if strings.Contains(quiet, "bad operand type for unary -") {
		t.Error("a module that never negates a non-number carries the negation sentence")
	}
}

// TestTheNegationOfALiteralIsNotFoldedAway is Gap R.37's rule at this door: a constant the reference stops
// on has to trap at run time, because the fold is what turns a TypeError back into a printed number.
func TestTheNegationOfALiteralIsNotFoldedAway(t *testing.T) {
	for _, src := range []string{
		"n = -None\nprint(n)\n",
		"n = -\"hi\"\nprint(n)\n",
		"n = -[1]\nprint(n)\n",
	} {
		res, err := Compile(src)
		if err != nil {
			t.Fatalf("compiled leg refused a program the oracle traps on: %v\n%s", err, src)
		}
		out := runIRMayTrap(t, res.IR)
		if !strings.Contains(out, "TypeError: bad operand type for unary -") {
			t.Errorf("%q answered instead of raising:\n%s", src, out)
		}
		if strings.Contains(out, "Traceback") && !strings.Contains(out, "unary -") {
			t.Errorf("%q raised a different error:\n%s", src, out)
		}
	}
}

// TestNegationOperandIsLiterallyNotANumber asks the fold-path predicate directly: the shapes the constant
// folders must decline, and the ones they must keep folding (a fold that stopped working would quietly
// cost every numeric program its constant).
func TestNegationOperandIsLiterallyNotANumber(t *testing.T) {
	for _, tc := range []struct {
		src  string
		want bool
	}{
		{"print(-None)\n", true},
		{"print(-\"a\")\n", true},
		{"print(-[1])\n", true},
		{"print(-{\"a\": 1})\n", true},
		{"print(-{1})\n", true},
		{"print(-(\"a\", 1))\n", true},
		{"print(-f\"{1}\")\n", true},
		// The parser folds a negated literal into the literal, so these have no unary node to ask — which
		// is exactly why the fold paths needed their own guard.
		{"x = 7\nprint(-x)\n", false},
		{"x = 1.5\nprint(-x)\n", false},
		{"x = True\nprint(-x)\n", false},
		{"xs = [3, 4]\nprint(-xs[0])\n", false},
	} {
		prog, err := parseProgram(tc.src)
		if err != nil {
			t.Fatalf("parse %q: %v", tc.src, err)
		}
		u := firstUnOp(t, prog.Stmts, tc.src)
		if got := negationOperandIsLiterallyNotANumber(u.X); got != tc.want {
			t.Errorf("%q: predicate = %v, want %v", tc.src, got, tc.want)
		}
	}
}

func mustCompileIR(t *testing.T, src string) string {
	t.Helper()
	res, err := Compile(src)
	if err != nil {
		t.Fatalf("compiled leg failed for %q: %v", src, err)
	}
	assertNoForbiddenIR(t, src, res.IR)
	return res.IR
}

// firstUnOp digs the row's own negation out of the parsed program, so the predicate can be asked directly
// rather than inferred from emitted IR.
func firstUnOp(t *testing.T, stmts []Stmt, src string) *UnOp {
	t.Helper()
	var found *UnOp
	var walkExpr func(Expr)
	walkExpr = func(e Expr) {
		if e == nil || found != nil {
			return
		}
		switch n := e.(type) {
		case *UnOp:
			found = n
		case *Call:
			for _, a := range n.Args {
				walkExpr(a)
			}
		}
	}
	for _, st := range stmts {
		switch s := st.(type) {
		case *ExprStmt:
			walkExpr(s.Expr)
		case *AssignStmt:
			walkExpr(s.Value)
		}
		if found != nil {
			break
		}
	}
	if found == nil {
		t.Fatalf("no unary operator in %q", src)
	}
	return found
}
