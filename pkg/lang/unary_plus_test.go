package lang

// unary_plus_test.go — the witnesses for the prefix `+` (roadmap **Gap R.244**, **ADR 0469**).
//
// Every `want` here is what `python3` prints for the program, and every raise row is the class AND the
// sentence the reference writes; a row that paraphrases the message is not a witness. The legs are the
// two the contract allows: the record (`goldenLookup`, `HasGoldenAnswer` — a missing record fails the
// case) and the reference (the CLI, in `integration/unary_plus_test.go`).
//
// The rows a gate breaks are:
//   - a parser that has no prefix `+` rule — every answered row comes back a parse error;
//   - a road that lowers `+x` as the NEGATION — `print(+1.5)` answers `-1.5`, and the IR rows read the
//     absence of `sub i32 0`/`fsub double 0.0` with a counter-row so the absence cannot be earned by
//     deleting the negation instead;
//   - a road that lowers `+x` as `x + 0` — the float rows print `1` and the raise rows print `0`;
//   - a road that trusts an operand it cannot name — the raise rows come back as answers at exit 0;
//   - a road that refuses what it CAN prove — the answered rows come back refusals, and a refusal test
//     that only checks "did it refuse" would not notice.

import (
	"strings"
	"testing"
)

// unaryPlusAnswered is the family the reference answers and this toolchain must answer identically.
// The reference column was measured, not remembered: each row was run through `python3` beside the
// compiled leg before it was written down here.
var unaryPlusAnswered = []struct {
	src  string
	want string
}{
	// ---- the literal operand: the row's own definition of done ----
	{"print(+1)", "1"},
	{"print(+0)", "0"},
	{"print(+1.5)", "1.5"},
	{"print(+1.0)", "1.0"},
	{"print(+True)", "1"},    // the reference's __pos__ of a bool is an INT, not True
	{"print(+False)", "0"},   // and not False
	{"print(+1, +2)", "1 2"}, // a `+` between the arguments is still an argument separator

	// ---- the prefixes compose, and compose with the negation the reference composes them ----
	{"print(-+1)", "-1"},
	{"print(-+-1)", "1"},
	{"print(+ +1)", "1"},
	{"print(+-1)", "-1"},
	{"print(+ + -1)", "-1"},
	{"print(-(-(+1)))", "1"},
	{"print(+ 1)", "1"}, // the prefix does not have to touch its operand

	// ---- `+` is a prefix; the infix is still the addition, and `+=` is still the augmentation ----
	{"print(1+1)", "2"},
	{"print(+1+1)", "2"},
	{"print(1 ++ 2)", "3"},
	{"print(1-+1)", "0"},
	{"print(5-+1)", "4"},
	{"print(2*+3)", "6"},
	{"print(+2 ** 2)", "4"},
	{"print(+3 // 2)", "1"},
	{"print(abs(+3))", "3"},
	{"print(max(+1, 2))", "2"},
	{"x = 1\nx += +1\nprint(x)", "2"},
	{"print(+1 == 1)", "True"},
	{"print(+1 > 0)", "True"},
	{"print(+1 in [1, 2])", "True"},
	{"print(str(+1))", "1"},
	{"print(f\"{+1}\")", "1"},

	// ---- a name the records describe ----
	{"x = 5\nprint(+x)", "5"},
	// the last binding decides, which is the control that keeps the proof honest: a name a container
	// bound and a number rebound answers the number, and the same pair in the other order raises
	{"xs = [1, 2]\nxs = 5\nprint(+xs)", "5"},
	{"x = 5\nprint(x-+1)", "4"},
	{"x = 5.5\nprint(+x)", "5.5"},
	{"x = True\nprint(+x)", "1"},
	{"c = 1 if True else 2\nprint(+c)", "1"},
	{"print(+1 if True else 2)", "1"},

	// ---- a slot the literal describes, and a container's elements ----
	{"xs = [7]\nprint(+xs[0])", "7"},
	{"xs = [1, 2, 3]\nprint(+xs[1-1])", "1"},
	{"d = {\"a\": 1}\nprint(+d[\"a\"])", "1"},
	{"xs = [1, 2]\nprint(+xs[0] + +xs[1])", "3"},
	{"xs = [1, 2]\nys = [+x for x in xs]\nprint(ys)", "[1, 2]"},
	{"print([+1])", "[1]"},
	{"print((+1, +2))", "(1, 2)"},
	{"print({\"a\": +1})", "{'a': 1}"},
	{"for v in [+1, +2]:\n    print(v)", "1\n2"},

	// ---- a callee's answer, proven from its own body or from the builtin's contract ----
	{"def f():\n    return 1\n\nprint(+f())", "1"},
	{"def h():\n    return 1.5\n\nprint(+h())", "1.5"},
	{"print(+len(\"abc\"))", "3"},
	{"print(+ord(\"a\"))", "97"},
	{"print(+int(\"2\"))", "2"},
	{"print(+int(True))", "1"},
	{"print(+float(\"1.5\"))", "1.5"},
	{"print(+sum([1, 2]))", "3"},
}

// unaryPlusRaiseRows are the programs the reference STOPS on, with its class and its sentence written
// out whole. Each of them is what the road would answer if it handed an operand it cannot name to the
// printer: `+"abc"` is the interned index of the text printed as a negative number, `+[1, 2]` is the
// heap handle, and `+None` is the 0 that the fold used to hand back.
var unaryPlusRaiseRows = []struct {
	src     string
	class   string
	message string
}{
	{"print(+\"abc\")", "TypeError", "bad operand type for unary +: 'str'"},
	{"print(+\"\")", "TypeError", "bad operand type for unary +: 'str'"},
	{"x = \"a\"\nprint(+x)", "TypeError", "bad operand type for unary +: 'str'"},
	{"print(+None)", "TypeError", "bad operand type for unary +: 'NoneType'"},
	{"print(+[1, 2])", "TypeError", "bad operand type for unary +: 'list'"},
	{"xs = [1, 2]\nprint(+xs)", "TypeError", "bad operand type for unary +: 'list'"},
	{"print(+(1, 2))", "TypeError", "bad operand type for unary +: 'tuple'"},
	{"print(+{1, 2})", "TypeError", "bad operand type for unary +: 'set'"},
	{"print(+{1: 2})", "TypeError", "bad operand type for unary +: 'dict'"},
	{"xs = [\"a\"]\nprint(+xs[0])", "TypeError", "bad operand type for unary +: 'str'"},
	{"def g():\n    return \"a\"\n\nprint(+g())", "TypeError", "bad operand type for unary +: 'str'"},
	{"class Token:\n    pass\n\nprint(+Token())", "TypeError", "bad operand type for unary +: 'Token'"},
	{"def f(x):\n    return x\n\nprint(+f)", "TypeError", "bad operand type for unary +: 'function'"},
	{"print(+(lambda x: x))", "TypeError", "bad operand type for unary +: 'function'"},
	// a container the program got from a call, and a name a text rebound over: the records the binding
	// road wrote are what the door reads, in both directions
	{"xs = sorted([\"b\", \"a\"])\nprint(+xs)", "TypeError", "bad operand type for unary +: 'list'"},
	{"x = 5\nx = \"a\"\nprint(+x)", "TypeError", "bad operand type for unary +: 'str'"},
	{"xs = [i for i in [1, 2]]\nprint(+xs)", "TypeError", "bad operand type for unary +: 'list'"},
}

// unaryPlusCatchRows are the same raises through `except`: the raise is the ordinary emitted one, so the
// handler runs and the program leaves at 0 (ADR 0211's misclassing is the failure this half witnesses).
var unaryPlusCatchRows = []struct {
	src  string
	want string
}{
	{"try:\n    print(+None)\nexcept TypeError as e:\n    print(e)", "bad operand type for unary +: 'NoneType'"},
	{"try:\n    print(+\"a\")\nexcept TypeError:\n    print(\"caught\")", "caught"},
	{"try:\n    print(+[1, 2])\nexcept TypeError as e:\n    print(e)", "bad operand type for unary +: 'list'"},
	{"try:\n    print(+{1: 2})\nexcept ValueError:\n    print(\"wrong arm\")\nexcept TypeError:\n    print(\"right arm\")", "right arm"},
}

// unaryPlusUntouchedRows are the neighbours the new prefix rule must not move: the negation's own
// answers and sentences, `abs`'s sentence, the augmented assignment, and the infix addition. Each is an
// answer this backend already gave; a `+` arm written in the wrong place takes one of them over.
var unaryPlusUntouchedRows = []struct {
	src  string
	want string
}{
	{"print(-1)", "-1"},
	{"print(-1.5)", "-1.5"},
	{"print(-True)", "-1"},
	{"print(5--1)", "6"},
	{"print(5- -1)", "6"},
	{"print(-\"hi\")", "trap:TypeError: bad operand type for unary -: 'str'"},
	{"print(-[1, 2])", "trap:TypeError: bad operand type for unary -: 'list'"},
	{"print(-None)", "trap:TypeError: bad operand type for unary -: 'NoneType'"},
	{"print(abs(\"hi\"))", "trap:TypeError: bad operand type for abs(): 'str'"},
	{"x = 10\nx -= 1\nprint(x)", "9"},
	{"x = 2\nx *= 3\nprint(x)", "6"},
	{"print(1+2*3)", "7"},
	{"print(\"a\" + \"b\")", "ab"},
	{"xs = [1]\nprint(xs + [2])", "[1, 2]"},
	{"print(2 ** -1)", "0.5"},
	{"xs = [1, 2, 3]\nprint(xs[-1], xs[-2])", "3 2"},
}

// unaryPlusFiledNotFixed is what this row does NOT repair, measured on the same day. Every one is the
// reference's answer beside the refusal this toolchain gives, and every one is filed under
// **Gap R.564** rather than guessed at: the road hands the operand back, so an operand whose kind no
// record names is a refusal, not a value read off the wire.
var unaryPlusFiledNotFixed = []struct {
	src     string
	refWant string // what `python3` prints (or raises) — written here so a reader can see the gap
	want    string // the word the refusal must contain
}{
	{
		// A parameter is whatever the call sites hand it: the body cannot name `a`'s kind, and
		// `def f(a): return +a` therefore refuses rather than trusts the one call in the program.
		"def f(a):\n    return +a\n\nprint(f(3))", "3", "cannot answer",
	},
	{
		// Two levels of slot: the proof is stated for the container a literal describes, one level deep.
		"xs = [[1], [2]]\nprint(+xs[0][0])", "1", "cannot answer",
	},
	{
		// A name the BODY does not own: the binding the records hold is the module's, and the function's
		// notebooks never saw it, so the same `+x` answers at top level and declines inside a `def`.
		"x = 5\ndef g():\n    return +x\n\nprint(g())", "5", "cannot answer",
	},
	{
		// `min` answers whichever operand wins, and the winner's kind is the container's business.
		"print(+min([3, 1]))", "1", "cannot answer",
	},
	{
		// A callee that hands back a container: the reference stops on it, and the door has no name
		// for a call's container answer yet (the same silence as Gap R.229's doors).
		"def h():\n    return [1]\n\nprint(+h())", "TypeError: bad operand type for unary +: 'list'", "cannot answer",
	},
	{
		// A callee that returns nothing answers None, which the door can name only for a literal.
		"def h():\n    pass\n\nprint(+h())", "TypeError: bad operand type for unary +: 'NoneType'", "cannot answer",
	},
	{
		// `len` never lowers its argument at all, so the raise that belongs to `+` is never asked —
		// the same door as Gap R.240's `len(s[1:3])`.
		"print(len(+\"ab\"))", "TypeError: bad operand type for unary +: 'str'", "len requires",
	},
	{
		// A mixed slot: only the object's tag knows the element, and the ordinary road declines it.
		"xs = [\"a\", 1]\nprint(+xs[0])", "TypeError: bad operand type for unary +: 'str'", "",
	},
}

// TestUnaryPlusAnswersWhatTheReferenceAnswers runs every answered row through the compiler and through
// the JIT, and refuses to accept a refusal: these programs were parse errors before ADR 0469, which is
// not a capability boundary the suite should ever learn to tolerate (ADR 0249).
func TestUnaryPlusAnswersWhatTheReferenceAnswers(t *testing.T) {
	t.Parallel()
	for _, tt := range unaryPlusAnswered {
		t.Run(strings.ReplaceAll(tt.src, "\n", "; "), func(t *testing.T) {
			t.Parallel()
			got, refused := compiledOutOrRefusal(t, tt.src)
			if refused {
				t.Fatalf("%q refused (%s); the reference answers %q", tt.src, got, tt.want+"\n")
			}
			if got != tt.want+"\n" {
				t.Fatalf("%q printed %q through the compiled leg, want %q", tt.src, got, tt.want+"\n")
			}
			out, trap := runForParity(t, tt.src)
			if trap != "" {
				t.Fatalf("%q raised %s; the reference prints %q", tt.src, trap, tt.want)
			}
			if out != tt.want {
				t.Fatalf("%q printed %q through the JIT, want %q", tt.src, out, tt.want)
			}
		})
	}
}

// TestTheUnaryPlusRaisesWhatHasNoSign asks the raise table of the compiled leg: the class and the whole
// sentence, not a prefix of it. Exit 2 is a toolchain bug and fails the row on its own.
func TestTheUnaryPlusRaisesWhatHasNoSign(t *testing.T) {
	t.Parallel()
	for _, tt := range unaryPlusRaiseRows {
		t.Run(strings.ReplaceAll(tt.src, "\n", "; "), func(t *testing.T) {
			t.Parallel()
			class, message := jitTrap(t, tt.src)
			if class != tt.class {
				t.Fatalf("%q raised %s: %s; the reference raises %s", tt.src, class, message, tt.class)
			}
			if message != tt.message {
				t.Fatalf("%q wrote %q; the reference writes %q", tt.src, message, tt.message)
			}
		})
	}
}

// TestTheUnaryPlusRaiseIsCatchable witnesses the same raises through a handler.
func TestTheUnaryPlusRaiseIsCatchable(t *testing.T) {
	t.Parallel()
	for _, tt := range unaryPlusCatchRows {
		t.Run(strings.ReplaceAll(tt.src, "\n", "; "), func(t *testing.T) {
			t.Parallel()
			out, trap := runForParity(t, tt.src)
			if trap != "" {
				t.Fatalf("%q raised %s; the handler was meant to run", tt.src, trap)
			}
			if out != tt.want {
				t.Fatalf("%q printed %q, want %q", tt.src, out, tt.want)
			}
		})
	}
}

// TestTheUnaryPlusRowsAreRecorded is the record leg's own guard: every answered and raise row has an
// entry in `testdata/interpreter-golden.json`, so neither half of this row can be witnessed by the
// compiled leg alone, and neither can be retired by deleting its evidence (ADR 0302).
func TestTheUnaryPlusRowsAreRecorded(t *testing.T) {
	t.Parallel()
	for _, tt := range unaryPlusAnswered {
		t.Run(strings.ReplaceAll(tt.src, "\n", "; "), func(t *testing.T) {
			t.Parallel()
			if !HasGoldenAnswer(tt.src) {
				t.Fatalf("%q is not on the record: a newly answered program is recorded, not self-certified", tt.src)
			}
			rec := goldenLookup(t, tt.src)
			if !rec.HasStd || rec.Stdout != tt.want+"\n" {
				t.Fatalf("%q: the record holds %q, the reference prints %q", tt.src, rec.Stdout, tt.want+"\n")
			}
		})
	}
	for _, tt := range unaryPlusRaiseRows {
		t.Run(strings.ReplaceAll(tt.src, "\n", "; "), func(t *testing.T) {
			t.Parallel()
			rec := goldenLookup(t, tt.src)
			if rec.ExnType == "" && rec.ExnMsg == "" {
				t.Fatalf("%q is not on the record as a raise; record it with `go run -tags=llvm20 ./tools/recmerge -sources …` (ADR 0302)", tt.src)
			}
			if rec.ExnType != tt.class {
				t.Errorf("%q: the record raised %q, the reference raises %q", tt.src, rec.ExnType, tt.class)
			}
			if rec.ExnMsg != tt.message {
				t.Errorf("%q: the record wrote %q, the reference writes %q", tt.src, rec.ExnMsg, tt.message)
			}
		})
	}
	for _, tt := range unaryPlusCatchRows {
		t.Run(strings.ReplaceAll(tt.src, "\n", "; "), func(t *testing.T) {
			t.Parallel()
			rec := goldenLookup(t, tt.src)
			if rec.Stdout != tt.want+"\n" {
				t.Fatalf("%q: the record printed %q, the handler prints %q", tt.src, rec.Stdout, tt.want+"\n")
			}
		})
	}
}

// TestTheUnaryPlusIsNotTheNegation is the IR witness. A road that lowered `+x` by reusing the subtraction
// would answer most of the value table anyway, so the value rows cannot catch it; the emitted module can.
// The counter-rows are what makes the witness mean something: the negation's instructions MUST still be
// there for the programs that are negations, so the absence above is a fact about `+` rather than a fact
// about a deleted road (ADR 0337's negative-control discipline).
func TestTheUnaryPlusIsNotTheNegation(t *testing.T) {
	t.Parallel()
	for _, src := range []string{
		"print(+1)",
		"print(+1+1)",
		"print(+1.5)",
		"x = 5\nprint(+x)",
		"x = 5.5\nprint(+x)",
		"xs = [7]\nprint(+xs[0])",
	} {
		t.Run(strings.ReplaceAll(src, "\n", "; "), func(t *testing.T) {
			t.Parallel()
			body := mainBodyOf(mustCompileIR(t, src))
			if strings.Contains(body, "sub i32 0,") {
				t.Fatalf("%q emitted a subtraction from zero:\n%s", src, body)
			}
			if strings.Contains(body, "fsub double 0.0,") {
				t.Fatalf("%q emitted a float subtraction from zero:\n%s", src, body)
			}
		})
	}
	// The counter-rows are NOT constant expressions: a folded `-1` answers from the literal and writes no
	// subtraction at all, which would make the witness pass for the wrong reason.
	for _, src := range []string{
		"x = 5\nprint(-x)",
		"x = 1.5\nprint(-x)",
	} {
		t.Run("counter-row "+strings.ReplaceAll(src, "\n", "; "), func(t *testing.T) {
			t.Parallel()
			body := mainBodyOf(mustCompileIR(t, src))
			if !strings.Contains(body, "sub i32 0,") && !strings.Contains(body, "fsub double 0.0,") {
				t.Fatalf("%q no longer writes the negation it has always written:\n%s", src, body)
			}
		})
	}
}

// TestTheUnaryPlusProofIsAskedOfARecordNotASpelling asks the road's positive proof directly, of the node
// the parser built, so a change to the proof is visible as a failing row rather than as a wrong answer.
// The nil generator is the soundness row: a proof that answered "number" with nothing to read from is the
// road handing back a text at exit 0.
func TestTheUnaryPlusProofIsAskedOfARecordNotASpelling(t *testing.T) {
	t.Parallel()
	proven := []string{
		"print(+1)",
		"print(+1.5)",
		"print(+True)",
		"print(+-1)",
		"print(-+1)",
		"print(+(1 < 2))",
		"print(1 < 2 < 3 and +1)",
		"print(+1 if True else 2)",
		"print(+len(\"abc\"))",
		"print(+2 * 3)",
	}
	for _, src := range proven {
		t.Run(src, func(t *testing.T) {
			t.Parallel()
			prog, err := parseProgram(src)
			if err != nil {
				t.Fatalf("parse %q: %v", src, err)
			}
			u := firstUnaryPos(t, prog.Stmts, src)
			if u.Op != "+" {
				t.Fatalf("%q: the row's first prefix is %q, not the unary +", src, u.Op)
			}
			if !(&irGen{}).unaryPosOperandIsANumber(u.X) {
				t.Errorf("%q: the road declines an operand it can prove from the node alone", src)
			}
		})
	}
	declined := []string{
		"def f(a):\n    return +a\n",                   // a parameter: whatever the call sites hand it
		"xs = [[1]]\nprint(+xs[0][0])",                 // two levels deep
		"print(+min([3, 1]))",                          // the winner of a min is the container's business
		"def h():\n    return [1]\n\nprint(+h())",      // a container answer
		"def h():\n    pass\n\nprint(+h())",            // an answer of None
		"x = 5\ndef g():\n    return +x\n\nprint(g())", // a binding the body's own notebooks never saw
	}
	for _, src := range declined {
		t.Run(strings.ReplaceAll(src, "\n", "; "), func(t *testing.T) {
			t.Parallel()
			prog, err := parseProgram(src)
			if err != nil {
				t.Fatalf("parse %q: %v", src, err)
			}
			u := firstUnaryPos(t, prog.Stmts, src)
			if u.Op != "+" {
				t.Fatalf("%q: the row's first prefix is %q, not the unary +", src, u.Op)
			}
			if (&irGen{}).unaryPosOperandIsANumber(u.X) {
				t.Errorf("%q: the road proved an operand no record names — the wrong-answer-at-exit-0 shape", src)
			}
		})
	}
	// The soundness row: the proof asked of nothing at all is still a refusal, never a panic and never a
	// permission (the table every other door in this directory is written against).
	var noGen *irGen
	for _, src := range append([]string{}, proven...) {
		prog, err := parseProgram(src)
		if err != nil {
			t.Fatalf("parse %q: %v", src, err)
		}
		u := firstUnaryPos(t, prog.Stmts, src)
		if noGen.unaryPosOperandIsANumber(u.X) {
			t.Errorf("%q: a road with no generator answered %q as a number", src, u.Op)
		}
	}
}

// TestTheUnaryPlusRefusesInWordsIsFiledNotFixed keeps the row honest about its edges: a refusal is only
// acceptable when it says which operand and which question (ADR 0249), and each of these is filed under
// Gap R.564 with the reference's answer beside it.
func TestTheUnaryPlusRefusesInWordsIsFiledNotFixed(t *testing.T) {
	t.Parallel()
	for _, tc := range unaryPlusFiledNotFixed {
		t.Run(strings.ReplaceAll(tc.src, "\n", "; "), func(t *testing.T) {
			t.Parallel()
			out, refused := compiledOutOrRefusal(t, tc.src)
			if !refused {
				t.Fatalf("%q answered %q; the row expects the filed refusal to stand (the reference does %s)", tc.src, out, tc.refWant)
			}
			if strings.Contains(out, "unsupported") {
				t.Errorf("%q refused with %q, which names nothing (ADR 0249)", tc.src, out)
			}
			if tc.want != "" && !strings.Contains(out, tc.want) {
				t.Fatalf("%q refused with %q, which does not say what is missing (ADR 0249), filed Gap R.564", tc.src, out)
			}
			if strings.Contains(out, " Gap R.5") && !strings.Contains(out, "Gap R.564") {
				t.Errorf("%q: the refusal cites a row that is not this one: %s", tc.src, out)
			}
		})
	}
}

// TestWhatTheUnaryPlusLeavesStanding runs the neighbours: the negation, `abs`, the augmented assignment
// and the infix addition. A prefix rule written into the wrong door shows up here and nowhere else.
func TestWhatTheUnaryPlusLeavesStanding(t *testing.T) {
	t.Parallel()
	for _, tt := range unaryPlusUntouchedRows {
		t.Run(strings.ReplaceAll(tt.src, "\n", "; "), func(t *testing.T) {
			t.Parallel()
			if want, isTrap := strings.CutPrefix(tt.want, "trap:"); isTrap {
				class, message := jitTrap(t, tt.src)
				got := class + ": " + message
				if got != want {
					t.Fatalf("%q raised %q; the neighbour's own sentence is %q", tt.src, got, want)
				}
				return
			}
			got, refused := compiledOutOrRefusal(t, tt.src)
			if refused {
				t.Fatalf("%q refused (%s); this neighbour answered before the prefix rule landed", tt.src, got)
			}
			if got != tt.want+"\n" {
				t.Fatalf("%q printed %q, want %q", tt.src, got, tt.want+"\n")
			}
		})
	}
}

// TestTheUnaryPlusIsInTheGrammarTheWayTheNegationIs is the parser's own witness: the AST the agent sees
// carries the prefix the source wrote, and the lexer's `+=` is one token rather than an operator and an
// empty operand.
func TestTheUnaryPlusIsInTheGrammarTheWayTheNegationIs(t *testing.T) {
	t.Parallel()
	prog, err := parseProgram("x = 1\nx += 1\nprint(+x)\n")
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	aug, ok := prog.Stmts[1].(*AugAssignStmt)
	if !ok {
		t.Fatalf("the second statement is %T; `+=` must still be the augmented assignment it was", prog.Stmts[1])
	}
	if aug.Op != "+" {
		t.Fatalf("`+=` lowered to op %q; the prefix rule must not eat the augmentation", aug.Op)
	}
	u := firstUnaryPos(t, prog.Stmts, "print(+x)")
	if u.Op != "+" {
		t.Fatalf("the AST carries %q; the source wrote a +, and --emit-ast must show it", u.Op)
	}
	if _, isName := u.X.(*Name); !isName {
		t.Fatalf("`+x` holds a %T; the prefix takes the operand, not a copy of it", u.X)
	}
}

// firstUnaryPos digs the row's own `+` out of a parsed program, walking the places a prefix can hide:
// a function body, an `if` arm, a loop body, a comprehension element — `firstUnOp` in
// negation_kind_test.go stops at a statement's own expression, which is enough for its rows and not for
// these, where the declined shapes are deliberately written inside a `def`.
func firstUnaryPos(t *testing.T, stmts []Stmt, src string) *UnOp {
	t.Helper()
	var found *UnOp
	var walkExpr func(Expr)
	var walkStmts func([]Stmt)
	walkExpr = func(e Expr) {
		if e == nil || found != nil {
			return
		}
		switch n := e.(type) {
		case *UnOp:
			if n.Op == "+" {
				found = n
				return
			}
			walkExpr(n.X)
		case *BinOp:
			walkExpr(n.L)
			walkExpr(n.R)
		case *Call:
			for _, a := range n.Args {
				walkExpr(a)
			}
			walkExpr(n.Fn)
		case *Index:
			walkExpr(n.Obj)
			walkExpr(n.Idx)
		case *CondExpr:
			walkExpr(n.Cond)
			walkExpr(n.If)
			walkExpr(n.Else)
		case *ListLit:
			for _, el := range n.Elems {
				walkExpr(el)
			}
		case *DictLit:
			for _, k := range n.Keys {
				walkExpr(k)
			}
			for _, v := range n.Vals {
				walkExpr(v)
			}
		}
	}
	walkStmts = func(list []Stmt) {
		for _, st := range list {
			if found != nil {
				return
			}
			switch s := st.(type) {
			case *ExprStmt:
				walkExpr(s.Expr)
			case *AssignStmt:
				walkExpr(s.Value)
			case *ReturnStmt:
				walkExpr(s.Expr)
			case *AugAssignStmt:
				walkExpr(s.Value)
			case *FuncDef:
				walkStmts(s.Body)
			case *IfStmt:
				walkStmts(s.Then)
				for _, el := range s.Elifs {
					walkStmts(el.Then)
				}
				walkStmts(s.Else)
			case *ForStmt:
				walkStmts(s.Body)
			case *WhileStmt:
				walkStmts(s.Body)
			}
		}
	}
	walkStmts(stmts)
	if found == nil {
		t.Fatalf("no unary + in %q", src)
	}
	return found
}
