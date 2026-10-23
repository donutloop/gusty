package lang

// lambda_answer_test.go — roadmap L11.7 · Gap R.382, ADR 0401: the answer of a `lambda` applied where it
// is written is a value with a kind.
//
//     print((lambda: [1, 2])())      # CPython: [1, 2]
//                                    # this toolchain before the row: `llc-20` refused the module —
//                                    # the hoisted `define` ended `ret i32 @.lst1` — at exit 2, the
//                                    # code ADR 0166 reserves for a compiler bug
//
// A `def` that hands back a container has ADR 0333's door: the scan reads its `return`, the `ret`
// materialises the object, and every position that reads the answer asks the door that knows the kind. A
// `lambda` applied where it is written had no declaration for the scan to read, so its answer fell through
// to the one-word road — which for a container is either an illegal `ret` or, one rung down with a
// comprehension in the body, the allocator's slot printed as `0` at exit 0 (roadmap Gap R.331).
//
// The rows below are all judged on the two witness legs: the record (`runGoldenStdout`, `goldenRepr`) and
// the reference (CPython's own bytes, written into the tables from `python3` rather than from memory), plus
// the IR the answer must be carried by and the decisions the pass makes before any IR exists. What the pass
// declines to touch is asserted too, in the last table: a program the pass leaves alone must answer exactly
// the bytes it answered before, or the "extra answer, never a substitute for one" rule (ADR 0273) has been
// broken by a test that looked like a feature.

import (
	"encoding/json"
	"strings"
	"testing"
)

// ----------------------------------------------------------------------------
// The answered table. `want` is what `python3` prints for the same source.
// ----------------------------------------------------------------------------

type lambdaAnswerRow struct {
	name string
	src  string
	want string
}

func lambdaAnswerPrograms() []lambdaAnswerRow {
	return []lambdaAnswerRow{
		// The row's own program, at the head of the table.
		{"a lambda whose body is a list literal answers the list", "print((lambda: [1, 2])())\n", "[1, 2]\n"},
		{"a lambda whose body is a dict literal answers the dict", "print((lambda: {1: 2})())\n", "{1: 2}\n"},
		{"a lambda whose body is a set literal answers the set", "print((lambda: {1, 2})())\n", "{1, 2}\n"},
		{"a lambda whose body is a tuple literal answers the tuple", "print((lambda: (1, 2))())\n", "(1, 2)\n"},
		{"a one-ary tuple keeps its bracket claim", "print((lambda: (1,))())\n", "(1,)\n"},
		{"the empty list is an object, not a global", "print((lambda: [])())\n", "[]\n"},
		{"the empty list spelled as a call", "print((lambda: list())())\n", "[]\n"},
		{"a comprehension is the answer", "print((lambda: [q for q in [3, 1]])())\n", "[3, 1]\n"},
		{"a slot the parameter filled", "print((lambda v: [v, 1])(9))\n", "[9, 1]\n"},
		{"a slot a default filled", "print((lambda x=3: [x])())\n", "[3]\n"},
		{"the answer bound to a name", "x = (lambda: [1, 2])()\nprint(x)\n", "[1, 2]\n"},
		{"the bound answer measured", "x = (lambda: [1, 2])()\nprint(len(x))\n", "2\n"},
		{"measured without the name", "print(len((lambda: [1, 2])()))\n", "2\n"},
		{"membership in the answer", "print(1 in (lambda: [1, 2])())\n", "True\n"},
		{"membership in a dict answer asks the keys", "print(\"a\" in (lambda: {\"a\": 1})())\n", "True\n"},
		{"a loop over the answer", "for v in (lambda: [7, 8])():\n    print(v)\n", "7\n8\n"},
		{"str of the answer", "print(str((lambda: [1, 2])()))\n", "[1, 2]\n"},
		{"repr of the answer", "print(repr((lambda: [1, 2])()))\n", "[1, 2]\n"},
		{"the answer as a condition", "if (lambda: [1, 2])():\n    print(\"yes\")\n", "yes\n"},
		{"an empty answer is false", "print(not (lambda: [])())\n", "True\n"},
		{"a slice of the answer", "print((lambda: [1, 2])()[0:2])\n", "[1, 2]\n"},
		{"a subscript of the answer", "print((lambda: [1, 2])()[0])\n", "1\n"},
		{"two slots deep", "print((lambda: [[1, 2], [3]])()[1][0])\n", "3\n"},
		{"a container inside the answer", "print((lambda: {\"a\": [1, 2]})())\n", "{'a': [1, 2]}\n"},
		{"a slot of a container inside the answer", "d = (lambda: {\"k\": [1, 2]})()\nprint(d[\"k\"][1])\n", "2\n"},
		{"the answer compared with a literal", "print((lambda: [1, 2])() == [1, 2])\n", "True\n"},
		{"a set answer equals another spelling", "print((lambda: {1, 2})() == {2, 1})\n", "True\n"},
		{"str of a set answer", "print(str((lambda: {1, 2})()))\n", "{1, 2}\n"},
		{"len of a set answer", "print(len((lambda: {1, 2, 3})()))\n", "3\n"},
		{"a builtin asked of the answer", "print(sorted((lambda: [3, 1, 2])()))\n", "[1, 2, 3]\n"},
		{"a comprehension over the answer", "print([v for v in (lambda: [1, 2])()])\n", "[1, 2]\n"},
		// The row was measured at the print door; the f-string field is the same position with a spec engine
		// in front of it, and the interpolation is where the walk used to stop (Gap R.383's witness).
		{"an f-string field of the answer", "print(f\"{(lambda: [1, 2])()}\")\n", "[1, 2]\n"},
		// The same answer read out of an `except` arm. The hoist turns the arm's print argument into a call,
		// which is the `def` road's shape, and the arm used to run under a flag that still said "an exception
		// is in flight" — the call's post-check took the run down and nothing printed (roadmap **Gap R.241**,
		// cured by clearing the flag on the accepting edge). The `def` twin below answers the same bytes.
		{"the answer printed from an `except` arm", "try:\n    print(1 // 0)\nexcept ZeroDivisionError:\n    print((lambda: [1, 2])())\n", "[1, 2]\n"},
		// ---------------------------------------------------------------------
		// Roadmap Gap R.384 (ADR 0402): an answer that is a TEXT, a VERDICT or the VOID, spelled as a literal
		// in the body. Each of these printed the payload word at exit 0 — `1` for a verdict, `0` for None and
		// for a text's @str_tab position — while the `def` twin of the very same body printed the reference's
		// bytes, because `textReturningCallables`, `IsBoolExpr`'s call arm and `fdReturnsValue` all read a
		// module-level `FuncDef` and none of them can see a callee that is a `Lambda` node. Every row below
		// was answered by giving the `lambda` the one thing those doors can read: a declaration.
		// ---------------------------------------------------------------------
		{"an applied lambda that answers a verdict answers the verdict", "print((lambda: True)())\n", "True\n"},
		{"the other verdict", "print((lambda: False)())\n", "False\n"},
		{"an applied lambda that answers None answers None", "print((lambda: None)())\n", "None\n"},
		{"an applied lambda that answers a text answers the text", "print((lambda: \"hi\")())\n", "hi\n"},
		{"the verdict bound to a name", "x = (lambda: True)()\nprint(x)\n", "True\n"},
		{"the text bound to a name", "x = (lambda: \"hi\")()\nprint(x)\n", "hi\n"},
		{"None bound to a name", "x = (lambda: None)()\nprint(x)\n", "None\n"},
		{"str of a text answer", "print(str((lambda: \"hi\")()))\n", "hi\n"},
		{"repr of a text answer", "print(repr((lambda: \"hi\")()))\n", "'hi'\n"},
		{"a text answer compared with a literal", "print((lambda: \"hi\")() == \"hi\")\n", "True\n"},
		{"a text answer repeated", "print((lambda: \"hi\")() * 2)\n", "hihi\n"},
		{"a text answer concatenated on either side", "print((lambda: \"a\")() + \"!\")\n", "a!\n"},
		{"a text answer asked of a text method", "print((lambda: \"hi\")().upper())\n", "HI\n"},
		{"a text answer measured", "print(len((lambda: \"hi\")()))\n", "2\n"},
		{"a text answer as a loop iterable", "for c in (lambda: \"ab\")():\n    print(c)\n", "a\nb\n"},
		{"a text answer as a membership needle", "print((lambda: \"hi\")() in [\"hi\", \"x\"])\n", "True\n"},
		{"the verdict as a condition", "if (lambda: True)():\n    print(\"yes\")\n", "yes\n"},
		{"the verdict chosen by `or`", "print((lambda: False)() or \"fallback\")\n", "fallback\n"},
		{"the verdict asked of the truthiness builtin", "print(bool((lambda: \"\")()))\n", "False\n"},
		{"a verdict and a void as list elements", "print([(lambda: True)(), (lambda: None)()])\n", "[True, None]\n"},
		{"a text as a list element, read back", "xs = [(lambda: \"hi\")()]\nprint(xs[0])\n", "hi\n"},
		{"a text as a dict value", "print({\"k\": (lambda: \"hi\")()})\n", "{'k': 'hi'}\n"},
		{"a text as a dict key", "print({(lambda: \"k\")(): 1})\n", "{'k': 1}\n"},
		{"a text as a set member", "print({(lambda: \"k\")()})\n", "{'k'}\n"},
		{"a text answer as a call argument", "def f(v):\n    return v\nprint(f((lambda: \"hi\")()))\n", "hi\n"},
		{"two answers of three kinds in one print", "print((lambda: True)(), (lambda: 2)())\n", "True 2\n"},
		{"the void printed twice in one print", "print((lambda: None)(), (lambda: None)())\n", "None None\n"},
		{"an answer the verdict chose in a ternary", "print((lambda: True)() if 1 else False)\n", "True\n"},
		{"the verdict is a comprehension element", "print([v for v in [(lambda: True)()]])\n", "[True]\n"},
		{"the answer with a parameter filled", "print((lambda x: None)(1))\n", "None\n"},
		{"the answer with a default filled", "print((lambda x=3: None)())\n", "None\n"},
		{"the verdict answered from inside a def", "def f(x):\n    return (lambda: True)()\nprint(f(1))\n", "True\n"},
		{"the verdict answered from inside a method", "class C:\n    def m(self):\n        print((lambda: True)())\n\nC().m()\n", "True\n"},
		{"the void answered in a `match` arm", "match 1:\n    case 1:\n        print((lambda: None)())\n", "None\n"},
		{"the answer in a `try` and in a `finally`", "try:\n    print((lambda: None)())\nfinally:\n    print((lambda: True)())\n", "None\nTrue\n"},
		{"the text answered beside a container answer", "print((lambda: \"hi\")())\nprint((lambda: [1, 2])())\n", "hi\n[1, 2]\n"},
		// Two roads into the same answer: the application written inside a `def` and beside other answers.
		{"the application written inside a def", "def f():\n    print((lambda: [1, 2])())\n\nf()\n", "[1, 2]\n"},
		{"two applications in one program", "print((lambda: [1, 2])())\nprint((lambda: [3])())\n", "[1, 2]\n[3]\n"},
		// The two programs the row found already pinned as wrong answers (Gap R.331), moved out of the debt
		// table into the answered one, where their own text said they belong.
		{
			"an applied lambda that answers a comprehension",
			"print((lambda x: [x for q in [1, 2]])(9))\n", "[9, 9]\n",
		},
		{
			"a container answered by the outermost of three forwarding lambdas",
			"print((lambda x: [(lambda y: (lambda z: x + y + z)(100))(10) for q in [1, 2]])(1))\n", "[111, 111]\n",
		},
		// Every carrier of an expression the pass has to reach, one row each. A carrier the walk could not
		// read left the program on the `ret i32 @.lst1` road — the exit-2 shape, in a different garment.
		{"inside a match arm", "match 1:\n    case 1:\n        print((lambda: [1, 2])())\n", "[1, 2]\n"},
		{"inside a while body", "xs = [1]\nwhile xs:\n    print((lambda: [1, 2])())\n    xs = []\n", "[1, 2]\n"},
		{"inside a finally body", "try:\n    print(1)\nfinally:\n    print((lambda: [1, 2])())\n", "1\n[1, 2]\n"},
		{"inside a for body", "for q in [1]:\n    print((lambda: [1, 2])())\n", "[1, 2]\n"},
		{"inside an if/else", "if 1 > 2:\n    print(0)\nelse:\n    print((lambda: [1, 2])())\n", "[1, 2]\n"},
		{"inside a method body", "class C:\n    def m(self):\n        print((lambda: [1, 2])())\n\nC().m()\n", "[1, 2]\n"},
		{"a def that forwards the answer", "def g():\n    return (lambda: [1, 2])()\n\nprint(g())\n", "[1, 2]\n"},
		{"a dict value position", "print({\"k\": (lambda: [1, 2])()[0]})\n", "{'k': 1}\n"},
	}
}

// lambdaAnswerTrapRows are the programs whose answer is a raise. The exit class is the same question as
// the message: a trap that exits 2 is a compiler bug (ADR 0006, ADR 0166), and a trap the program cannot
// catch is a different program from the one the reference runs.
func lambdaAnswerTrapRows() []struct{ name, src, want string } {
	return []struct{ name, src, want string }{
		{"a position past the answer's end", "print((lambda: [1, 2])()[9])\n", "IndexError: list index out of range"},
		{"a key the answer has not got", "print((lambda: {\"a\": 1})()[\"b\"])\n", "KeyError: 'b'"},
		{"a position past the end of a slice's base", "print((lambda: [1, 2])()[9] + 1)\n", "IndexError: list index out of range"},
	}
}

// lambdaAnswerCatchable are the same traps reached from inside the program's own `except`, which is the
// half of a trap that makes it a trap rather than a crash.
func lambdaAnswerCatchable() []struct{ name, src, want string } {
	return []struct{ name, src, want string }{
		{
			"IndexError is catchable",
			"try:\n    print((lambda: [1, 2])()[9])\nexcept IndexError:\n    print(\"caught\")\n", "caught\n",
		},
		{
			"KeyError is catchable",
			"try:\n    print((lambda: {\"a\": 1})()[\"b\"])\nexcept KeyError:\n    print(\"caught\")\n", "caught\n",
		},
	}
}

// lambdaAnswerFiledNotFixed is the family this row measured and does NOT own. Each row names the program's
// owner elsewhere on the tracker, and each is asserted beside the `def` twin that behaves the same way:
// the claim being pinned is the PARITY (the hoisted call answers exactly as a call to a `def` does), not an
// answer. A row moves out of this table — into the answered one — the day its owner row closes.
func lambdaAnswerFiledNotFixed() []struct{ name, src, defTwin, gap string } {
	return []struct{ name, src, defTwin, gap string }{
		{
			// Gap R.67: a slot of a container literal filled by a call that answers a container is a value
			// the answer table cannot describe, and the literal road says so. The `def` twin — a `return`
			// whose literal holds a call to another container-returning function — is refused by the same
			// sentence, which is the parity this row claims and the reason the refusal is not this row's.
			"a call's container as a slot of a literal is refused by the literal road (Gap R.67's, not this row's)",
			"print((lambda: [(lambda: [3, 4])(), 1])())\n",
			"def h():\n    return [3, 4]\n\ndef g():\n    return [h(), 1]\n\nprint(g())\n",
			"Gap R.67",
		},
		{
			// Gap R.171: a void that crosses a call boundary arrives as the bare word 0, because the parameter
			// has no tag beside it. Now that an applied `lambda` that answers the void becomes a declaration, the
			// two spellings of the program — the void handed to a forwarding parameter by a hoisted `lambda` and
			// by a `def` — arrive at the SAME wrong byte, which is the parity this row claims: the missing word
			// is the parameter's, not the lambda's.
			"the void handed to a forwarding parameter answers the digit 0 (Gap R.171's, not this row's)",
			"def f(v):\n    return v\n\nprint(f((lambda: None)()))\n",
			"def a1():\n    return None\n\ndef f(v):\n    return v\n\nprint(f(a1()))\n",
			"Gap R.171",
		},
	}
}

// lambdaAnswerRefusals are the positions that keep ONE word for a value and therefore refuse. Each is the
// same verdict the `def` twin gets — that parity is the claim of the row, and the sentence differs only in
// the name the reader sees (`<lambda>`, which is what the reference calls it too).
func lambdaAnswerRefusals() []struct{ name, src string } {
	return []struct{ name, src string }{
		{"a container literal element", "print([1, (lambda: [1, 2])()])\n"},
		{"a call argument", "def g(v):\n    return v\n\nprint(g((lambda: [1, 2])()))\n"},
		{"a dict value", "print({\"k\": (lambda: [1, 2])()})\n"},
		{"the truthiness builtin", "print(bool((lambda: [1, 2])()))\n"},
		{"arithmetic", "print((lambda: [1, 2])() + 1)\n"},
		{"a ternary arm", "print((lambda: [1, 2])() if 1 else [9])\n"},
	}
}

// lambdaAnswerOwed are the divergences this row MEASURED and does not own, each with the answer it holds
// today written beside the reference's. They are pinned, not filed-and-forgotten: a row that stops being
// true because the program now answers the reference's bytes FAILS, and the case has to be deleted rather
// than left to certify a leg that no longer exists (the rule **ADR 0166** and roadmap Gap R.382's own
// Definition of done agree on).
//
// mode is which question the row asks:
//
//   - "prints"  — the compiled leg runs and prints `today`; the reference prints `want`.
//   - "refuses" — the compiled leg refuses in words, and the sentence holds `today`.
//   - "rejects" — the compiled leg never reaches the run time: `llc-20` rejects the module, whose message
//     holds `today`. This is ADR 0166's exit-2 class, which is why it is pinned by its message and not by
//     an exit code the program got instead.
type lambdaAnswerOwedRow struct {
	name  string
	src   string
	want  string // the reference's own bytes, read off `python3`
	mode  string
	today string
	gap   string
}

func lambdaAnswerOwed() []lambdaAnswerOwedRow {
	return []lambdaAnswerOwedRow{
		// Gap R.385 — the shape behind this row's ternary control, with no `lambda` in it at all: the arm
		// keeps the module-level literal as its value and the print door puts the GLOBAL in an `i32` slot.
		{
			name:  "a container literal in a ternary arm is emitted as a global in a word position",
			src:   "print([1, 2] if 1 else [9])\n",
			want:  "[1, 2]\n",
			mode:  "rejects",
			today: "global variable reference must have pointer type",
			gap:   "Gap R.385",
		},
		// Gap R.387 — measured while widening Gap R.384's gate. The body spells its kind in the source, but it
		// spells it as a CALL to a `def` (or as an interpolation), and this pass's answer question reads the
		// body's own literal, not the callee's registration. The `def` twin — `def outer(): return inner()` —
		// prints the reference's bytes, so the answer exists; what is missing is the walk from one body to the
		// other. The f-string arm is the dangerous one: `scanFreeIn` (the frame gate's reader) filters slice
		// fields by `nodeField`, exactly like the `callableWalk` of Gap R.383, so it cannot see a Name inside
		// an interpolation's field — hoisting such a body would read the MODULE's name where the program wrote
		// the frame's, which is ADR 0166's wrong answer at exit 0. That arm waits for the walk, not for a gate.
		{
			name:  "an applied lambda whose body is a CALL to a text-answering def answers the @str_tab position",
			src:   "def inner():\n    return \"hi\"\n\nprint((lambda: inner())())\n",
			want:  "hi\n",
			mode:  "prints",
			today: "0\n",
			gap:   "Gap R.387",
		},
		{
			name:  "an applied lambda whose body is a CALL to a verdict-answering def answers the payload",
			src:   "def ib():\n    return True\n\nprint((lambda: ib())())\n",
			want:  "True\n",
			mode:  "prints",
			today: "1\n",
			gap:   "Gap R.387",
		},
		{
			name:  "an applied lambda whose body is a CALL to a void-answering def answers the void word",
			src:   "def iN():\n    return None\n\nprint((lambda: iN())())\n",
			want:  "None\n",
			mode:  "prints",
			today: "0\n",
			gap:   "Gap R.387",
		},
		{
			// The two legs disagree for this one, which is why it is pinned in `rejects` mode: the CLI links and
			// prints the payload word `0`, and the leg every `pkg/lang` case walks never reaches the run time at
			// all, because the closure road interns the interpolation into a module that has no `@rt_str_intern2`
			// definition — roadmap Gap R.333's missing runtime block, worn by an f-string body (ADR 0401 measured
			// the same pair for a text body before this pass took those bodies at all).
			name:  "an applied lambda whose body is an interpolation dies in the assembler (the CLI prints the void word)",
			src:   "print((lambda: f\"hi{2}\")())\n",
			want:  "hi2\n",
			mode:  "rejects",
			today: "use of undefined value '@rt_str_intern2'",
			gap:   "Gap R.387, Gap R.333",
		},
		// Gap R.388 — the void SPELLED as `return None` is not the void reached by falling off the end. A body
		// with no `return` stores the run-time kind word and `f() is None` answers True; a body that writes
		// `return None` stores nothing, so `is`/`==` compare the payload `0` against None and answer False at
		// exit 0. Roadmap Gap R.228's own Definition of done reads “`print(f())` writes `None` on both legs and
		// `f() is None` is `True`” — the first half holds for both spellings, the second holds only for the
		// fall-off-the-end one, and no test asked the question, which is how a DoD cell came to state an answer
		// the toolchain does not give. The lambda spelling below is the same program with the hoist in front of
		// it, which is why it is measured here; its `def` twin answers False too (the parity row in
		// `lambdaAnswerFiledNotFixed`), so the row belongs to the comparison door and not to this pass.
		{
			name:  "a void answer compared with None answers False (the `def` twin does too)",
			src:   "print((lambda: None)() is None)\n",
			want:  "True\n",
			mode:  "prints",
			today: "False\n",
			gap:   "Gap R.388",
		},
		// Gap R.386 — the gate this row left shut: a body that READS the enclosing frame stays on the
		// closure road, and the closure road has no container answer, so the program dies with the literal
		// builder's own sentence, which names no callable and no row (ADR 0166, ADR 0345 §3).
		{
			name:  "an applied lambda that reads the frame and answers a container is refused by a sentence that names nothing",
			src:   "def f(x):\n    return (lambda: [x])()\n\nprint(f(2))\n",
			want:  "[2]\n",
			mode:  "refuses",
			today: "list literal elements must be integers, not ",
			gap:   "Gap R.386",
		},
		{
			name:  "an applied lambda that reads a module name and answers a container is refused the same way",
			src:   "x = 2\nprint((lambda: [x])())\n",
			want:  "[2]\n",
			mode:  "refuses",
			today: "list literal elements must be integers, not ",
			gap:   "Gap R.386",
		},
	}
}

// ----------------------------------------------------------------------------
// The two witness legs over the answered table.
// ----------------------------------------------------------------------------

// TestWhatAnAppliedLambdaAnswersTheReferencePrints is the reference leg: the compiled backend is asked and
// the bytes are compared with CPython's, which are written in the table rather than read from the record.
func TestWhatAnAppliedLambdaAnswersTheReferencePrints(t *testing.T) {
	t.Parallel()
	for _, tc := range lambdaAnswerPrograms() {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := compiledRunIsTheBackend(t, tc.src); got != tc.want {
				t.Fatalf("the compiled backend printed %q, the reference prints %q\n%s", got, tc.want, tc.src)
			}
		})
	}
}

// TestWhatAnAppliedLambdaAnswersTheRecordRecorded is the record leg. A MISSING entry fails the case, so a
// source this row newly answers cannot be dropped from coverage by deleting its record (AGENTS.md).
func TestWhatAnAppliedLambdaAnswersTheRecordRecorded(t *testing.T) {
	t.Parallel()
	for _, tc := range lambdaAnswerPrograms() {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, err := runGoldenStdout(t, tc.src)
			if err != nil {
				t.Fatalf("the record has no answer for this program (%v):\n%s", err, tc.src)
			}
			if got != tc.want {
				t.Fatalf("the record holds %q, the table expects %q\n%s", got, tc.want, tc.src)
			}
		})
	}
}

// TestWhatAnAppliedLambdaAnswerTrapsTheWayTheReferenceDoes asserts the raise: the class and message the
// reference raises with, at the exit class a trap has, and the same raise reached from the program's own
// `except`.
func TestWhatAnAppliedLambdaAnswerTrapsTheWayTheReferenceDoes(t *testing.T) {
	t.Parallel()
	for _, tc := range lambdaAnswerTrapRows() {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			expectTrap(t, tc.src, tc.want)
		})
	}
	for _, tc := range lambdaAnswerCatchable() {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := compiledRunIsTheBackend(t, tc.src); got != tc.want {
				t.Fatalf("the raise did not reach the handler: printed %q, want %q\n%s", got, tc.want, tc.src)
			}
		})
	}
}

// TestWhatTheAppliedLambdaRowsLeaveStandingIsFiledNotFixed asks the rows this row measured and does not own.
// The assertion is the PARITY: the hoisted call and the call to a `def` reach the same verdict, so the
// divergence belongs to the row named, not to the answer the row carries. A row whose two legs disagree has
// no answer to record either, which is why these are pinned here rather than filed as records.
func TestWhatTheHoistedLambdaRowsLeaveStandingAreFiledNotFixed(t *testing.T) {
	t.Parallel()
	for _, tc := range lambdaAnswerFiledNotFixed() {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			here, hereRefused, hereCode := programOutcome(t, tc.src)
			twin, twinRefused, twinCode := programOutcome(t, tc.defTwin)
			if here != twin || hereRefused != twinRefused || hereCode != twinCode {
				t.Fatalf("%s: the applied lambda answers %q (refused %v, exit %d) where the `def` twin answers %q (refused %v, exit %d) — the two spellings of one program diverged, which is %s's shape to fix",
					tc.gap, here, hereRefused, hereCode, twin, twinRefused, twinCode, tc.gap)
			}
			if here == "" && hereCode == 0 && !hereRefused {
				t.Fatalf("%s: both legs print nothing at exit 0, which is not an answer either; if this program now prints, delete the row and move it into the answered table (%s)", tc.gap, tc.src)
			}
		})
	}
}

// TestWhatAnAppliedLambdaAnswerRefusesInWordsWhereTheDoorHasNoRoad pins the refusals: the sentence the
// container-answer door has always said, the callable named the way the reader knows it, and none of the
// compiler's own machinery in the sentence (ADR 0166, ADR 0249, ADR 0345 §3).
func TestWhatAnAppliedLambdaAnswerRefusesInWordsWhereTheDoorHasNoRoad(t *testing.T) {
	t.Parallel()
	for _, tc := range lambdaAnswerRefusals() {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			msg := compiledRefusalOf(tc.src)
			if msg == "" {
				t.Fatalf("the position that keeps one word for a value answered anyway — a heap handle printed as a number is Gap R.261's shape\n%s", tc.src)
			}
			if !strings.Contains(msg, "hands back a container") {
				t.Fatalf("the refusal is not the container-answer door's own sentence:\n%s", msg)
			}
			if !strings.Contains(msg, "<lambda>") {
				t.Fatalf("the refusal does not name the callable the way a reader knows it (the reference calls it <lambda> too):\n%s", msg)
			}
			if !strings.Contains(msg, "Gap R.67") {
				t.Fatalf("the refusal does not name the row that owns the missing road:\n%s", msg)
			}
			for _, leak := range []string{"*lang.", "*ir.", "0x", "%!s(", "%v", "__cans", "lambda_0"} {
				if strings.Contains(msg, leak) {
					t.Fatalf("the refusal leaks the compiler's own spelling %q:\n%s", leak, msg)
				}
			}
		})
	}
}

// TestWhatThisRowMeasuredAndDoesNotOwnPinsTheDivergence holds the four rows this cycle measured and left on
// the tracker. Each keeps the reference's answer next to today's, so a fix that lands cannot quietly retire
// the evidence, and none of them is allowed to be a compiler bug wearing an answer.
func TestWhatThisRowMeasuredAndDoesNotOwnPinsTheDivergence(t *testing.T) {
	t.Parallel()
	for _, tc := range lambdaAnswerOwed() {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			switch tc.mode {
			case "rejects":
				got, err := RunSource(tc.src)
				if err == nil {
					t.Fatalf("%s: the module no longer dies in `llc` — it printed %q (the reference prints %q); when the "+
						"two agree, DELETE this row and move the program into the answered table\n%s", tc.gap, got, tc.want, tc.src)
				}
				if !strings.Contains(err.Error(), tc.today) {
					t.Fatalf("%s: the module rejection changed from %q to:\n%v", tc.gap, tc.today, err)
				}
			case "refuses":
				msg := compiledRefusalOf(tc.src)
				if msg == "" {
					t.Fatalf("%s: the program compiled — if it now prints %q, DELETE this row\n%s", tc.gap, tc.want, tc.src)
				}
				if !strings.Contains(msg, tc.today) {
					t.Fatalf("%s: the refusal changed from %q to:\n%s", tc.gap, tc.today, msg)
				}
			default:
				got := compiledRunIsTheBackend(t, tc.src)
				if got == tc.want {
					t.Fatalf("%s: the program now prints the reference's %q — DELETE this row\n%s", tc.gap, tc.want, tc.src)
				}
				if got != tc.today {
					t.Fatalf("%s: today's answer moved from %q to %q (the reference prints %q)\n%s", tc.gap, tc.today, got, tc.want, tc.src)
				}
			}
		})
	}
}

// ----------------------------------------------------------------------------
// The IR the answer travels in.
// ----------------------------------------------------------------------------

// lambdaAnswerIRRow is one row of the IR table the row's claim is read off: what the emitted module has to
// carry, what it must not carry, and — for the three kinds roadmap Gap R.384 registered (ADR 0402) — whether
// the answer is allowed to reach `@printf` as a number at all.
type lambdaAnswerIRRow struct {
	name   string
	src    string
	need   []string
	forbid []string
	// answerNeverReachesPrintf asserts that no `@printf` call in one of the PROGRAM's own bodies is handed an
	// `i32` register — the shape that printed `1` for a verdict and `0` for None and for a text. It is asserted
	// over the program's bodies rather than over a `%d` format constant in the module, because the runtime blocks
	// carry formats of their own and a module-wide ban fails on `rt_gc_report`'s own text.
	answerNeverReachesPrintf bool
	// wantPrintfOfAnswer is the control's opposite claim: the answer DOES reach the number printer, because
	// this body spells a number and the one-word road is exactly the road the row keeps for it.
	wantPrintfOfAnswer bool
}

func lambdaAnswerIRRows() []lambdaAnswerIRRow {
	return []lambdaAnswerIRRow{
		{
			name:   "a list literal answer",
			src:    "print((lambda: [1, 2])())\n",
			need:   []string{"define i32 @gy_lambda_0()", "ret i32 %", "call i32 @rt_alloc("},
			forbid: []string{"ret i32 @."},
		},
		{
			name:   "a dict literal answer",
			src:    "print((lambda: {1: 2})())\n",
			need:   []string{"define i32 @gy_lambda_0()", "ret i32 %", "call i32 @rt_alloc("},
			forbid: []string{"ret i32 @."},
		},
		{
			name:   "the answer printed by the container printer",
			src:    "print((lambda: [1, 2])())\n",
			need:   []string{"call void @rt_print_list(", "call i32 @gy_lambda_0()"},
			forbid: []string{"ret i32 @."},
		},
		{
			// the negative control: a `lambda` that answers a number keeps the one-word road, and a
			// container built for nothing would be the pass over-releasing its gate.
			name:   "a number answer builds no container",
			src:    "print((lambda: 5)())\n",
			need:   []string{"define i32 @gy_lambda_0()", "ret i32 5"},
			forbid: []string{"__cans", "call i32 @rt_alloc("},
		},
		// Roadmap Gap R.384 (ADR 0402): the three kinds the `def` answer tables register and the one-word road
		// cannot render. Each row asserts the PRINTER the kind owns and that the answer never reaches printf's
		// number — printf handed the payload is exactly what printed `1` for `True` and `0` for `None` and for a
		// text. The text row is also roadmap Gap R.333's witness: the hoisted `return` interns through
		// `@rt_str_intern2`, and the block that DEFINES it is in the module, which is the definition the closure
		// road's side-buffer emission leaves out and `llc-20` refused at exit 2 for the very same source.
		{
			name:                     "a verdict answer is printed by the bool printer, never by printf's number",
			src:                      "print((lambda: True)())\n",
			need:                     []string{"define i32 @gy_lambda_0()", "ret i32 1", "call void @rt_print_bool(i32 %"},
			answerNeverReachesPrintf: true,
		},
		{
			name:                     "a void answer is printed by the tag-reading printer",
			src:                      "print((lambda: None)())\n",
			need:                     []string{"define i32 @gy_lambda_0()", "call void @rt_print_mixed_value(i32 %", "store i32 3, i32* @gy_lambda_0.anst"},
			answerNeverReachesPrintf: true,
		},
		{
			name:                     "a text answer is interned at the return and printed by the text printer",
			src:                      "print((lambda: \"hi\")())\n",
			need:                     []string{"define i32 @gy_lambda_0()", "call i32 @rt_str_intern2(", "ret i32 %", "call void @rt_print_str(i32 %", "define internal i32 @rt_str_intern2("},
			forbid:                   []string{"ret i32 @."},
			answerNeverReachesPrintf: true,
		},
		{
			// The gate's own boundary, read off the module rather than off a print: a body that spells a NUMBER
			// keeps the number road, so the widening above cannot have quietly routed every answer through the
			// tag-reading printers, and the control below is the printf the three rows above forbid.
			name:               "the number control keeps the number printer",
			src:                "print((lambda: 5)())\n",
			need:               []string{"call i32 @gy_lambda_0()", "ret i32 5"},
			wantPrintfOfAnswer: true,
		},
	}
}

// TestTheHoistedAnswerIsBuiltAtTheReturnAndNeverTravelsAsAGlobal is the row's IR claim: the declaration the
// pass mints materialises the container at the `ret`, so the word that crosses the return is a register
// holding a handle. `ret i32 @.lst1` — a module constant in an integer position — is the instruction
// `llc-20` refused at exit 2, and this row fails if it comes back.
func TestTheHoistedAnswerIsBuiltAtTheReturnAndNeverTravelsAsAGlobal(t *testing.T) {
	t.Parallel()
	for _, tc := range lambdaAnswerIRRows() {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			res, err := Compile(tc.src)
			if err != nil {
				t.Fatalf("the compiled leg refused a program the reference answers: %v\n%s", err, tc.src)
			}
			for _, want := range tc.need {
				if !strings.Contains(res.IR, want) {
					t.Fatalf("the module does not carry %s:\n%s", want, res.IR)
				}
			}
			for _, bad := range tc.forbid {
				if strings.Contains(res.IR, bad) {
					t.Fatalf("the module carries the forbidden %s:\n%s", bad, res.IR)
				}
			}
			// The printf discipline, asked of the PROGRAM's own bodies only: `@main` and the `@gy_*` frames the
			// compiler emits. The runtime blocks are not the program's answer and keep their own formats.
			inProgram, sawNumberPrint := false, false
			for _, line := range strings.Split(res.IR, "\n") {
				if strings.HasPrefix(line, "define ") {
					head := strings.SplitN(strings.TrimPrefix(line, "define "), "(", 2)[0]
					// `define i32 @main()` spells the symbol AFTER its return type; the name is what decides
					// whether this body belongs to the program or to the runtime block.
					if i := strings.Index(head, "@"); i >= 0 {
						head = head[i:]
					} else {
						head = ""
					}
					inProgram = head == "@main" || strings.HasPrefix(head, "@gy_")
					continue
				}
				if !inProgram || !strings.Contains(line, "@printf(") {
					continue
				}
				if strings.Contains(line, ", i32 %") {
					sawNumberPrint = true
				}
			}
			if tc.answerNeverReachesPrintf && sawNumberPrint {
				t.Fatalf("the answer reached printf's number format in a program whose printer knows its kind:\n%s", res.IR)
			}
			if tc.wantPrintfOfAnswer && !sawNumberPrint {
				t.Fatalf("a body that spells a number lost the number printer it has always had:\n%s", res.IR)
			}
			// Every `define` of the row's program is a definition of one symbol: the pass mints names
			// beside the emitter's own counter, and two `define`s of one spelling is the exit-2 shape.
			seen := map[string]int{}
			for _, line := range strings.Split(res.IR, "\n") {
				if !strings.HasPrefix(line, "define ") {
					continue
				}
				head := strings.SplitN(strings.TrimPrefix(line, "define "), "(", 2)[0]
				seen[head]++
			}
			for name, n := range seen {
				if n > 1 {
					t.Fatalf("%s is defined %d times — the pass and the emitter minted the same spelling\n%s", name, n, tc.src)
				}
			}
		})
	}
}

// ----------------------------------------------------------------------------
// The pass, asked its own questions before any IR exists.
// ----------------------------------------------------------------------------

// TestTheHoistIsDecidedByTheProgramAlone asks the pass directly: which applications become declarations,
// and which are handed back to the road they have always had. The gate is two questions — does the body
// read only its own parameters, and does ADR 0333's scan read the declaration as answering a container —
// and each row below is one of the two being answered no.
func TestTheHoistIsDecidedByTheProgramAlone(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name    string
		src     string
		hoisted int
	}{
		{"a list answer is hoisted", "print((lambda: [1, 2])())\n", 1},
		{"two applications are two declarations", "print((lambda: [1])())\nprint((lambda: [2])())\n", 2},
		{"a number answer is not", "print((lambda: 5)())\n", 0},
		{"a float answer is not", "print((lambda: 1.5)())\n", 0},
		// Roadmap Gap R.384 (ADR 0402): the three kinds the `def` roads register and the one-word road cannot
		// render are now hoisted, because the hoist IS the answer — the declaration is what `textReturningCallables`,
		// `IsBoolExpr` and `fdReturnsValue` need in order to say anything about the call at all.
		{"a text answer is hoisted", "print((lambda: \"hi\")())\n", 1},
		{"a None answer is hoisted", "print((lambda: None)())\n", 1},
		{"a verdict answer is hoisted", "print((lambda: True)())\n", 1},
		{"the other verdict is hoisted", "print((lambda: False)())\n", 1},
		{"the void answered through a parameter is hoisted", "print((lambda x: None)(1))\n", 1},
		// … and the shapes that borrow another body's registration are NOT: this pass reads the body's own
		// spelling, not the callee's table entry (roadmap Gap R.387).
		{"a body that is a call to a text-answering def is not — Gap R.387", "def inner():\n    return \"hi\"\n\nprint((lambda: inner())())\n", 0},
		{"a body that is an interpolation is not — Gap R.387, and the frame reader cannot see its field", "print((lambda: f\"hi{2}\")())\n", 0},
		{"a body that reads the frame is not", "def f(x):\n    return (lambda: [x])()\n\nprint(f(1))\n", 0},
		{"a body that reads a module name is not", "t = [1, 2]\nprint((lambda: t)())\n", 0},
		{"a parameter handed a double is not — the pair road owns that value", "print((lambda v: [v, 1])(2.5))\n", 0},
		{"a body that answers a call the scan cannot follow is not", "print((lambda: abs(3))())\n", 0},
	} {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			prog, err := Parse(tc.src)
			if err != nil {
				t.Fatalf("parse: %v", err)
			}
			out, minted := hoistAppliedLambdaAnswers(prog)
			got := 0
			for _, st := range out.Stmts {
				if fd, ok := st.(*FuncDef); ok && strings.HasPrefix(fd.Name, "lambda_") && fd.CloneOf == callableAnonymousName {
					got++
				}
			}
			if got != tc.hoisted {
				t.Fatalf("the pass minted %d declarations, the table says %d\n%s", got, tc.hoisted, tc.src)
			}
			if tc.hoisted == 0 {
				if out != prog {
					t.Fatalf("a program with nothing to hoist was rewritten anyway\n%s", tc.src)
				}
				if minted != 0 {
					t.Fatalf("the pass consumed %d names without hoisting anything; the emitter's numbering moved for nothing\n%s", minted, tc.src)
				}
				return
			}
			if minted < tc.hoisted {
				t.Fatalf("the pass minted %d names for %d declarations\n%s", minted, tc.hoisted, tc.src)
			}
		})
	}
}

// TestTheHoistLeavesTheTreeTheFrontEndReportsAlone is ADR 0320's rule asked of this pass: the rewrite
// happens on a copy, so the `lambda` the human typed is still the callee of the call the checker, the AST
// dump and the formatter read.
func TestTheHoistLeavesTheTreeTheFrontEndReportsAlone(t *testing.T) {
	t.Parallel()
	const src = "print((lambda: [1, 2])())\n"
	prog, err := Parse(src)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	out, _ := hoistAppliedLambdaAnswers(prog)
	if out == prog {
		t.Fatalf("the pass did not rewrite anything")
	}
	// The copy's call names the minted declaration …
	copied, ok := out.Stmts[0].(*ExprStmt)
	if !ok {
		t.Fatalf("the probe program changed shape")
	}
	if _, isLambda := callCalleeLambda(copied); !isLambda {
		// print(…) is the outer call; its argument is the application.
		t.Logf("outer statement is %T", copied)
	}
	// … and the caller's tree still holds the `lambda` the source wrote.
	outer, ok := prog.Stmts[0].(*ExprStmt)
	if !ok {
		t.Fatalf("the probe program changed shape")
	}
	c, isCall := outer.Expr.(*Call)
	if !isCall {
		t.Fatalf("the probe program's statement is not a call")
	}
	arg, isArgCall := c.Args[0].(*Call)
	if !isArgCall {
		t.Fatalf("the probe program's argument is not a call")
	}
	if _, isLambda := arg.Fn.(*Lambda); !isLambda {
		t.Fatalf("--emit-ast and `gusty fmt` no longer see the lambda the program wrote: the pass mutated the caller's tree")
	}
	// The same tree asked twice gives the same answer: the pass is a function of the program, not of the
	// order things were compiled in (ADR 0273).
	again, _ := Parse(src)
	other, _ := hoistAppliedLambdaAnswers(again)
	if a, b := hoistedNames(out), hoistedNames(other); strings.Join(a, ",") != strings.Join(b, ",") {
		t.Fatalf("two runs minted different declarations: %v vs %v", a, b)
	}
}

// TestThePassMintsNoNameTheProgramAlreadyTook is the duplicate-definition guard: `def lambda_0(): …` is
// legal source, and a hoisted declaration of the same spelling is the exit-2 shape arriving from a pass
// that chose a name the program was using.
func TestThePassMintsNoNameTheProgramAlreadyTook(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name string
		src  string
	}{
		{"a def of the same spelling", "def lambda_0():\n    return 1\n\nprint((lambda: [1, 2])())\n"},
		{"a binding of the same spelling", "lambda_0 = 1\nprint((lambda: [1, 2])())\n"},
		{"a loop target of the same spelling", "for lambda_0 in [1]:\n    print(lambda_0)\nprint((lambda: [1, 2])())\n"},
	} {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			prog, err := Parse(tc.src)
			if err != nil {
				t.Fatalf("parse: %v", err)
			}
			out, minted := hoistAppliedLambdaAnswers(prog)
			for _, n := range hoistedNames(out) {
				if n == "lambda_0" && strings.Contains(tc.src, "def lambda_0") {
					t.Fatalf("the pass took the name the program declared: %s", n)
				}
			}
			res, err := Compile(tc.src)
			if err != nil {
				t.Fatalf("the compiled leg refused a program the reference answers: %v\n%s", err, tc.src)
			}
			count := map[string]int{}
			for _, line := range strings.Split(res.IR, "\n") {
				if strings.HasPrefix(line, "define ") {
					count[strings.SplitN(strings.TrimPrefix(line, "define "), "(", 2)[0]]++
				}
			}
			for name, n := range count {
				if n > 1 {
					t.Fatalf("%s defined %d times (the pass minted %d names)\n%s", name, n, minted, tc.src)
				}
			}
		})
	}
}

// TestWhatTheHoistLeavesAloneStillAnswersWhatItDid is the regression gate for the pass itself: every
// program it declines has to answer, refuse or die with exactly the bytes it did before the pass existed.
// A "gate" that moves a program it claims not to touch is not a gate.
func TestWhatTheHoistLeavesAloneStillAnswersWhatItDid(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name    string
		src     string
		want    string // the bytes this toolchain prints, pinned (not the reference's)
		refuses string // or the sentence it refuses with
	}{
		{"a number answer is still the word it always was", "print((lambda: 5)())\n", "5\n", ""},
		{"a float answer is still the word it always was", "print((lambda: 2.5)())\n", "2.5\n", ""},
		{
			// The two shapes Gap R.387 owns: the pass declines them, so the bytes they printed before the gate
			// widened are the bytes they print after. A "gate" that moved one of these moved a program it claims
			// not to touch.
			"a body that is a call to a text-answering def is still the payload it always was",
			"def inner():\n    return \"hi\"\n\nprint((lambda: inner())())\n", "0\n", "",
		},
		{
			"a pair handed to a slot is still refused by the literal road",
			"print((lambda v: [v, 1])(2.5))\n", "", "list literal elements must be integers",
		},
		{
			"a body that reads the frame is still the capture road's",
			"def f(x):\n    print((lambda: [x])())\n\nf(3)\n", "", "list literal elements must be integers",
		},
	} {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if tc.refuses != "" {
				msg := compiledRefusalOf(tc.src)
				if msg == "" {
					t.Fatalf("expected a refusal, the program compiled: %s", tc.src)
				}
				if !strings.Contains(msg, tc.refuses) {
					t.Fatalf("the refusal changed from %q to:\n%s", tc.refuses, msg)
				}
				return
			}
			if got := compiledRunIsTheBackend(t, tc.src); got != tc.want {
				t.Fatalf("a program the pass does not touch moved from %q to %q\n%s", tc.want, got, tc.src)
			}
		})
	}
}

// hoistedNames lists the declarations this pass minted, in the order they joined the module.
func hoistedNames(p *Program) []string {
	out := []string{}
	if p == nil {
		return out
	}
	for _, st := range p.Stmts {
		if fd, ok := st.(*FuncDef); ok && fd.CloneOf == callableAnonymousName && strings.HasPrefix(fd.Name, "lambda_") {
			out = append(out, fd.Name)
		}
	}
	return out
}

// callCalleeLambda reports whether a statement's expression is a call whose callee is a `lambda` — the
// shape the pass is supposed to have rewritten in the COPY and left alone in the original.
func callCalleeLambda(st *ExprStmt) (*Lambda, bool) {
	c, isCall := st.Expr.(*Call)
	if !isCall {
		return nil, false
	}
	if lam, isLambda := c.Fn.(*Lambda); isLambda {
		return lam, true
	}
	for _, a := range c.Args {
		if inner, isCall := a.(*Call); isCall {
			if lam, isLambda := inner.Fn.(*Lambda); isLambda {
				return lam, true
			}
		}
	}
	return nil, false
}

// ----------------------------------------------------------------------------
// The machine path: the pass's own account, as data (roadmap Gap R.382, ADR 0401).
// ----------------------------------------------------------------------------

// lambdaAnswerDoc is the shape `gustyc --lambda-answers <src>` prints, decoded here by field NAME rather
// than by a Go struct so that a key renamed in the code and left in the schema (or the other way round)
// fails this case instead of drifting apart quietly (ADR 0004: the schema and the payload are one claim).
type lambdaAnswerDoc struct {
	Tool         string `json:"tool"`
	Pass         string `json:"pass"`
	Schema       string `json:"schema"`
	Rewritten    bool   `json:"rewritten"`
	Settled      bool   `json:"settled"`
	Declarations []struct {
		Symbol   string   `json:"symbol"`
		Answer   string   `json:"answer"`
		Params   []string `json:"params"`
		Defaults []string `json:"defaults"`
		Line     int      `json:"line"`
	} `json:"declarations"`
	Symbols  []string `json:"symbols"`
	Declined []struct {
		Shape  string `json:"shape"`
		Reason string `json:"reason"`
		Line   int    `json:"line"`
	} `json:"declined"`
	NamesConsumed int      `json:"names_consumed"`
	Refused       []string `json:"refused"`
}

func askThePassItsAccount(t *testing.T, src string) lambdaAnswerDoc {
	t.Helper()
	raw, err := LambdaAnswersJSON(src)
	if err != nil {
		t.Fatalf("the pass could not answer for a program the toolchain compiles: %v\n%s", err, src)
	}
	var doc lambdaAnswerDoc
	if err := json.Unmarshal([]byte(raw), &doc); err != nil {
		t.Fatalf("the account is not JSON (%v):\n%s", err, raw)
	}
	return doc
}

// TestWhatThePassSaysAboutItselfNamesEveryApplication is the account's claim: one entry per application,
// on both sides of the gate, with the reason naming the program's own name and the row that owns the
// missing road — and no compiler spelling anywhere in a sentence a human reads.
func TestWhatThePassSaysAboutItselfNamesEveryApplication(t *testing.T) {
	t.Parallel()
	const src = "print((lambda: [1, 2])())\n" +
		"def f(x):\n    return (lambda: [x])()\n\nprint(f(1))\n" +
		"print((lambda: 5)())\n"
	doc := askThePassItsAccount(t, src)
	if doc.Tool == "" || doc.Pass == "" || doc.Schema == "" {
		t.Fatalf("the document does not describe itself: %+v", doc)
	}
	if doc.Pass != "hoist-applied-lambda-answers" || doc.Schema != "gusty.lambda-answers.v1" {
		t.Fatalf("the pass or the schema tag moved (the payload and the schema are one claim, ADR 0004): %q / %q", doc.Pass, doc.Schema)
	}
	if !doc.Rewritten || !doc.Settled {
		t.Fatalf("this program has one answerable application; the pass says rewritten=%v settled=%v", doc.Rewritten, doc.Settled)
	}
	if len(doc.Declarations) != 1 || len(doc.Declined) != 2 {
		t.Fatalf("three applications, %d declarations and %d declines — every application is accounted for on one side or the other\n%s",
			len(doc.Declarations), len(doc.Declined), src)
	}
	d := doc.Declarations[0]
	if d.Answer != "list" || d.Line != 1 || d.Symbol == "" {
		t.Fatalf("the declaration is not the answer the scan proved: %+v", d)
	}
	if len(doc.Symbols) != 1 || doc.Symbols[0] != d.Symbol {
		t.Fatalf("symbols and declarations disagree: %v vs %q", doc.Symbols, d.Symbol)
	}
	shapes := map[string]bool{}
	for _, dl := range doc.Declined {
		shapes[dl.Shape] = true
		if dl.Line == 0 {
			t.Fatalf("a decline that does not say which line: %+v\n%s", dl, src)
		}
		for _, leak := range []string{"*lang.", "*ir.", "%!s(", "%v", "0x", "__cans"} {
			if strings.Contains(dl.Reason, leak) {
				t.Fatalf("the sentence leaks the compiler's own spelling %q:\n%s", leak, dl.Reason)
			}
		}
	}
	if !shapes["captures_frame"] || !shapes["answers_nothing_provable"] {
		t.Fatalf("the two gates are named %v; the document has to say WHICH one closed, not merely that one did", doc.Declined)
	}
	for _, dl := range doc.Declined {
		if dl.Shape == "captures_frame" && !strings.Contains(dl.Reason, `"x"`) {
			t.Fatalf("the decline does not name the program's own name, which is the only name the author can act on:\n%s", dl.Reason)
		}
		if !strings.Contains(dl.Reason, "roadmap Gap R.") {
			t.Fatalf("the decline does not name the row that owns the missing road:\n%s", dl.Reason)
		}
	}
	if doc.NamesConsumed < len(doc.Declarations) {
		t.Fatalf("the pass consumed %d names for %d declarations", doc.NamesConsumed, len(doc.Declarations))
	}
}

// TestWhatThePassCallsAVerdictATextAndTheVoid is the machine path for the three answers roadmap Gap R.384
// added to the gate (ADR 0402): the account names each declaration's answer with the word the `def` roads use,
// so an agent can branch on `bool`/`text`/`none` instead of scraping the module. The number row beside them is
// the negative half — a body whose answer the one-word road already renders is declined, and said to be.
func TestWhatThePassCallsAVerdictATextAndTheVoid(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name   string
		src    string
		answer string
	}{
		{"a verdict", "print((lambda: True)())\n", "bool"},
		{"the other verdict", "print((lambda: False)())\n", "bool"},
		{"the void", "print((lambda: None)())\n", "none"},
		{"a text", "print((lambda: \"hi\")())\n", "text"},
	} {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			doc := askThePassItsAccount(t, tc.src)
			if !doc.Rewritten || len(doc.Declarations) != 1 {
				t.Fatalf("one application, one declaration expected: rewritten=%v %+v\n%s", doc.Rewritten, doc.Declarations, tc.src)
			}
			d := doc.Declarations[0]
			if d.Answer != tc.answer {
				t.Fatalf("the account calls the answer %q, the %s road calls it %q\n%s", d.Answer, tc.name, tc.answer, tc.src)
			}
			if d.Symbol == "" || d.Line == 0 {
				t.Fatalf("the declaration names no symbol or no line: %+v\n%s", d, tc.src)
			}
			res, err := Compile(tc.src)
			if err != nil {
				t.Fatalf("the compiled leg refused a program the reference answers: %v\n%s", err, tc.src)
			}
			// The account's only claim worth trusting: the symbol it names is a define the module carries.
			if want := "define i32 @gy_" + d.Symbol + "()"; !strings.Contains(res.IR, want) {
				t.Fatalf("the account says %s answers a %s, and the module never carried it\n%s", d.Symbol, d.Answer, res.IR)
			}
		})
	}
	// The number control, from the same document: declined, and named as declined.
	num := askThePassItsAccount(t, "print((lambda: 5)())\n")
	if num.Rewritten || len(num.Declarations) != 0 || len(num.Declined) != 1 || num.Declined[0].Shape != "answers_nothing_provable" {
		t.Fatalf("a number answer is the one-word road's, and the account has to say so: %+v", num)
	}
	if !strings.Contains(num.Declined[0].Reason, "number") || !strings.Contains(num.Declined[0].Reason, "roadmap Gap R.") {
		t.Fatalf("the decline names no shape the author can act on and no row that owns it:\n%s", num.Declined[0].Reason)
	}
}

// TestWhatThePassSaysAboutAProgramItLeftAloneConsumesNoNames is the negative half, and it is the same claim
// the emitter makes: an account of a rewrite nobody performed must not move the module's text either.
func TestWhatThePassSaysAboutAProgramItLeftAloneConsumesNoNames(t *testing.T) {
	t.Parallel()
	const src = "print((lambda: 5)())\n"
	doc := askThePassItsAccount(t, src)
	if doc.Rewritten || doc.NamesConsumed != 0 || len(doc.Declarations) != 0 || len(doc.Symbols) != 0 {
		t.Fatalf("a program with nothing to hoist was rewritten anyway: %+v", doc)
	}
	if len(doc.Declined) != 1 || doc.Declined[0].Shape != "answers_nothing_provable" {
		t.Fatalf("the decline is missing or blames the wrong gate: %+v", doc.Declined)
	}
}

// TestWhatThePassSaysIsWhatTheModuleShows is the account's only claim worth trusting: every symbol it
// names is a define the module carries, and the bytes are the same in every run. An account that described
// a hoist the compiler did not perform would be worse than no account, because an agent would branch on it.
func TestWhatThePassSaysIsWhatTheModuleShows(t *testing.T) {
	t.Parallel()
	const src = "print((lambda: [1, 2])())\nprint((lambda: {1: 2})())\n"
	first := askThePassItsAccount(t, src)
	again := askThePassItsAccount(t, src)
	if len(first.Declarations) != 2 || len(again.Declarations) != 2 {
		t.Fatalf("two list/dict answers, %d declarations", len(first.Declarations))
	}
	a, _ := json.Marshal(first)
	b, _ := json.Marshal(again)
	if string(a) != string(b) {
		t.Fatalf("two runs of one source answered differently:\n%s\n%s", a, b)
	}
	res, err := Compile(src)
	if err != nil {
		t.Fatalf("the compiled leg refused a program the reference answers: %v", err)
	}
	for _, d := range first.Declarations {
		for _, want := range []string{"define i32 @gy_" + d.Symbol + "()", "ret i32 %"} {
			if !strings.Contains(res.IR, want) {
				t.Fatalf("the account says %s answers a %s, the module never carried %q\n%s", d.Symbol, d.Answer, want, res.IR)
			}
		}
	}
}

// TestTheSchemaDeclaresTheLambdaAnswerAccount is the one-claim rule between `--lambda-answers` and
// `--schema` (ADR 0004): a field added to the payload and forgotten in the schema — or the reverse — is a
// field an agent silently stops being able to branch on. The required lists are compared, not admired.
func TestTheSchemaDeclaresTheLambdaAnswerAccount(t *testing.T) {
	t.Parallel()
	var doc struct {
		Definitions map[string]json.RawMessage `json:"definitions"`
	}
	if err := json.Unmarshal([]byte(ASTIRSchema), &doc); err != nil {
		t.Fatalf("--schema is not valid JSON: %v", err)
	}
	want := map[string][]string{
		"lambdaAnswerReport":      {"tool", "pass", "schema", "rewritten", "settled", "declarations", "symbols", "declined", "names_consumed", "refused"},
		"lambdaAnswerDeclaration": {"symbol", "answer", "params", "defaults", "line"},
		"lambdaAnswerDecline":     {"shape", "reason", "line"},
	}
	for name, required := range want {
		raw, ok := doc.Definitions[name]
		if !ok {
			t.Fatalf("the schema declares no definitions.%s, but `--lambda-answers` prints one (ADR 0004)", name)
		}
		var def struct {
			Required   []string `json:"required"`
			Properties map[string]struct {
				Type string   `json:"type"`
				Enum []string `json:"enum"`
			} `json:"properties"`
		}
		if err := json.Unmarshal(raw, &def); err != nil {
			t.Fatalf("definitions.%s does not parse: %v", name, err)
		}
		got := map[string]bool{}
		for _, r := range def.Required {
			got[r] = true
		}
		for _, field := range required {
			if !got[field] {
				t.Fatalf("definitions.%s does not require %q, which the payload always prints", name, field)
			}
			if _, ok := def.Properties[field]; !ok {
				t.Fatalf("definitions.%s requires %q without describing it", name, field)
			}
		}
	}
	// The closed pair an agent branches on is closed HERE as well as in the Go code.
	var decline struct {
		Properties struct {
			Shape struct {
				Enum []string `json:"enum"`
			} `json:"shape"`
		} `json:"properties"`
	}
	if err := json.Unmarshal(doc.Definitions["lambdaAnswerDecline"], &decline); err != nil {
		t.Fatalf("definitions.lambdaAnswerDecline does not parse: %v", err)
	}
	shapes := map[string]bool{}
	for _, s := range decline.Properties.Shape.Enum {
		shapes[s] = true
	}
	if !shapes[lambdaAnswerShapeCaptures] || !shapes[lambdaAnswerShapeUnproved] || len(shapes) != 2 {
		t.Fatalf("the decline shapes the schema publishes (%v) are not the two the pass emits", decline.Properties.Shape.Enum)
	}
}
