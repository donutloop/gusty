package lang

// Gap R.53 / L12.1 — comparison chains are Python's construct, not two nested comparisons (ADR 0288).
//
// `a < b < c` asks TWO questions — `a < b` and `b < c` — and reads the middle operand ONCE. This grammar
// used to parse it left-associatively as `(a < b) < c`, which compares an int against a boolean, and this
// front end ANSWERS that question rather than refusing it. The result was a family of exit-0 wrong
// numbers on both engines:
//
//	print(1 > 2 < 3)   CPython False · both backends True
//	print(1 < 2 > 1)   CPython True  · both backends False
//	x = 50 / print(1 < x < 10)   CPython False · both backends True
//
// `print(1 < 2 < 3)` printed True throughout — the accidental pass, and the reason a chain test has to
// contain a failing chain (roadmap L12.1's own note about which test would not have caught it).

import (
	"strings"
	"testing"
)

// TestComparisonChainsAnswerWhatPythonAnswers is the table. Every row is a chain whose nested reading
// gives the OPPOSITE verdict, so a regression to left-associativity cannot hide behind the passing rows.
func TestComparisonChainsAnswerWhatPythonAnswers(t *testing.T) {
	rows := []struct {
		name string
		src  string
		want string
	}{
		{"ascending", "print(1 < 2 < 3)\n", "True\n"},
		{"descending", "print(3 < 2 < 1)\n", "False\n"},
		{"up then down, true", "print(1 < 2 > 1)\n", "True\n"},
		{"down then up, false", "print(1 > 2 < 3)\n", "False\n"},
		{"all equal", "print(1 <= 1 <= 1)\n", "True\n"},
		{"first link fails", "print(2 == 1 < 3)\n", "False\n"},
		{"second link fails", "print(1 < 2 == 2)\n", "True\n"},
		{"four operands ascending", "print(1 < 2 < 3 < 4)\n", "True\n"},
		{"four operands, last fails", "print(1 < 2 < 3 < 1)\n", "False\n"},
		// A chain over a name — the shape a real program writes.
		{"name inside the range", "x = 5\nprint(1 < x < 10)\n", "True\n"},
		{"name outside the range", "x = 50\nprint(1 < x < 10)\n", "False\n"},
		{"name in a failing lower bound", "x = 0\nprint(1 < x < 10)\n", "False\n"},
		// Texts, so the chain is not only ever written over numbers.
		{"texts ascending", "print(\"a\" < \"b\" < \"c\")\n", "True\n"},
		{"texts out of order", "print(\"c\" < \"b\" < \"a\")\n", "False\n"},
		{"text name between texts", "x = \"m\"\nprint(\"a\" < x < \"z\")\n", "True\n"},
		// The middle operand is something computed, which is where "evaluated once" becomes observable.
		{"call in the middle", "def g():\n    return 5\n\nprint(1 < g() < 10)\n", "True\n"},
		{"call in the middle, false", "def g():\n    return 50\n\nprint(1 < g() < 10)\n", "False\n"},
		{"container slot in the middle", "xs = [1, 2, 3]\nprint(0 < xs[1] < 3)\n", "True\n"},
		{"container slot, false", "xs = [1, 2, 3]\nprint(3 < xs[1] < 5)\n", "False\n"},
		{"dict slot in the middle", "d = {\"a\": 5}\nprint(1 < d[\"a\"] < 10)\n", "True\n"},
		// A chain as a test, which is where the wrong verdict chose the wrong branch.
		{"chain as a test, false", "if 1 < 5 < 3:\n    print(\"in\")\nelse:\n    print(\"out\")\n", "out\n"},
		{"chain as a test, true", "if 1 < 2 < 3:\n    print(\"in\")\nelse:\n    print(\"out\")\n", "in\n"},
		{"chain in a function body", "def f(x):\n    if 1 < x < 10:\n        return 1\n\n    return 2\n\nprint(f(5))\nprint(f(50))\n", "1\n2\n"},
		{"is in the middle", "print(1 is 1 < 2)\n", "True\n"},
		{"equality then ordering", "print(2 == 2 <= 3)\n", "True\n"},
	}
	for _, r := range rows {
		r := r
		t.Run(r.name, func(t *testing.T) {
			got := captureStdout(t, r.src)
			if got != r.want {
				t.Errorf("interpreter printed %q, want %q", got, r.want)
			}
			compiled := compiledOut(t, r.src)
			if compiled != r.want {
				t.Errorf("--aot printed %q, want %q", compiled, r.want)
			}
		})
	}
}

// TestTheMiddleOperandIsEvaluatedOnce is L12.1's stated requirement, asserted where it is observable: an
// effectful middle operand records itself, and a chain must record it exactly once per occurrence. The
// compiled backend does this by storing each operand into a slot and reading the slot; my first version
// handed the ORIGINAL expression to every link and printed the call's output three times at exit 0.
func TestTheMiddleOperandIsEvaluatedOnce(t *testing.T) {
	countRuns := func(out string) int {
		return strings.Count(out, "evaluated")
	}
	for _, engine := range []struct {
		name string
		run  func(*testing.T, string) string
	}{
		{"interpreter", func(t *testing.T, s string) string { return captureStdout(t, s) }},
		{"aot", func(t *testing.T, s string) string { return compiledOut(t, s) }},
	} {
		engine := engine
		t.Run(engine.name+" middle call", func(t *testing.T) {
			src := "def g():\n    print(\"evaluated\")\n\n    return 5\n\nprint(1 < g() < 10)\n"
			out := engine.run(t, src)
			if n := countRuns(out); n != 1 {
				t.Errorf("%s evaluated the middle operand %d times, want 1 (output %q) — Gap R.53's whole point", engine.name, n, out)
			}
			if !strings.Contains(out, "True") {
				t.Errorf("%s printed %q, want the verdict True", engine.name, out)
			}
		})
		t.Run(engine.name+" both ends counted", func(t *testing.T) {
			// `g() < g() < g()` has three DISTINCT occurrences; each must run once, not twice for the
			// middle one — which is what a nested BinOp reading the original nodes does.
			src := "def g():\n    print(\"evaluated\")\n\n    return 5\n\nprint(1 < g() < 6)\nprint(g())\n"
			out := engine.run(t, src)
			if n := countRuns(out); n != 2 {
				t.Errorf("%s ran g %d times, want 2 (one chain operand + one call): %q", engine.name, n, out)
			}
		})
	}
}

// TestChainVerdictsPrintTrueAndFalse keeps the rendering half pinned. A chain answers a VERDICT, so the
// print road must take the bool printer; before this the compiled leg fell through to printf("%d") and
// `print(1 < 2 < 3)` answered `1` — the same wrong number the chain itself fixes, arriving from the other
// end (ADR 0257's rule that a verdict writes its own name).
func TestChainVerdictsPrintTrueAndFalse(t *testing.T) {
	for _, r := range []struct{ src, want string }{
		{"print(1 < 2 < 3)\n", "True\n"},
		{"print(3 < 2 < 1)\n", "False\n"},
		{"x = 5\nprint(1 < x < 10)\n", "True\n"},
	} {
		r := r
		t.Run(strings.TrimSpace(r.src), func(t *testing.T) {
			if got := compiledOut(t, r.src); got != r.want {
				t.Errorf("--aot printed %q, want %q (a verdict printed through %%d is Gap R.53's answer from the print side)", got, r.want)
			}
		})
	}
}

// TestChainParsesAsOneNode pins the shape rather than only the answer, because the shape is what makes
// "each middle operand once" true: one node over n operands and n-1 operators, not nested BinOps.
func TestChainParsesAsOneNode(t *testing.T) {
	prog, err := Parse("print(1 < 2 < 3)")
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	es, ok := prog.Stmts[0].(*ExprStmt)
	if !ok {
		t.Fatalf("expected ExprStmt, got %T", prog.Stmts[0])
	}
	call, ok := es.Expr.(*Call)
	if !ok || len(call.Args) != 1 {
		t.Fatalf("expected a one-argument call, got %T", es.Expr)
	}
	ch, ok := call.Args[0].(*ChainCompare)
	if !ok {
		t.Fatalf("a chain parsed as %T, want *ChainCompare — nested BinOps are the bug this row exists to remove", call.Args[0])
	}
	if len(ch.Operands) != 3 || len(ch.Ops) != 2 {
		t.Fatalf("chain has %d operands and %d operators, want 3 and 2", len(ch.Operands), len(ch.Ops))
	}
	// A single comparison must NOT become a chain: every ordinary comparison in the language would
	// gain a second codegen path to get wrong.
	single, err := Parse("print(1 < 2)")
	if err != nil {
		t.Fatalf("parse single: %v", err)
	}
	ses := single.Stmts[0].(*ExprStmt)
	sc := ses.Expr.(*Call)
	if _, isChain := sc.Args[0].(*ChainCompare); isChain {
		t.Fatalf("a single comparison became a ChainCompare; it must stay a *BinOp")
	}
	if _, isBin := sc.Args[0].(*BinOp); !isBin {
		t.Fatalf("a single comparison parsed as %T, want *BinOp", sc.Args[0])
	}
}

// TestChainsDoNotSwallowBooleanOperators keeps the boundary: `a < b and b < c` is TWO comparisons joined
// by `and` (which short-circuits), not one chain of three operands. The parser stops a chain at any
// operator that is not itself a comparison, and a chain that ran through `and` would evaluate the right
// hand side of the `and` unconditionally — the exact behaviour L12.1 rejects.
func TestChainsDoNotSwallowBooleanOperators(t *testing.T) {
	prog, err := Parse("x = 1\ny = 2\nz = 9\nprint(x < y and y < z)")
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	es := prog.Stmts[3].(*ExprStmt)
	call := es.Expr.(*Call)
	bin, ok := call.Args[0].(*BinOp)
	if !ok || bin.Op != "and" {
		t.Fatalf("`a < b and b < c` parsed as %T/%v; `and` must stay the outer node", call.Args[0], bin)
	}
	if _, isChain := bin.L.(*ChainCompare); isChain {
		t.Fatalf("`and`'s arm became a chain; a single comparison is a BinOp")
	}
}

// TestChainWithTwoWordOperatorsStillParses guards the ordering mistake I made: building the chain BEFORE
// the two-token handling left `1 not in [1]` and `1 is not 2` unparseable, which is a worse bug than the
// one being fixed.
func TestChainWithTwoWordOperatorsStillParses(t *testing.T) {
	for _, src := range []string{
		"print(1 not in [2])",
		"print(1 is not 2)",
		"print(1 is 1)",
		"print(\"a\" in \"abc\")",
		"print(1 is 1 < 2)",
	} {
		src := src
		t.Run(src, func(t *testing.T) {
			if _, err := Compile(src + "\n"); err != nil {
				t.Fatalf("refused a program the reference answers (%v): %s", err, src)
			}
		})
	}
}

// TestChainWithAContainerOperandRefusesOnTheCompiledLeg pins the honest half. The reference chains over
// containers and the interpreter matches it, but the compiled leg cannot: a container literal's compiled
// value is the address of a compile-time global, while the slot a chain gives a repeated operand is an
// `i32` alloca, so the store would read `store i32 @.lst1, i32* %_chain1` — the shape llc rejects, which
// ADR 0234 already classed as a compiler bug for an ordinary program. A refusal is owed until L11.1's
// tagged value word lets a container travel as a word. The test fails if the refusal silently becomes a
// number, and also if it leaks the module it could not build.
func TestChainWithAContainerOperandRefusesOnTheCompiledLeg(t *testing.T) {
	for _, src := range []string{
		"print(1 not in [2] == 1)\n",
		"print(1 in [1, 2] == 1)\n",
	} {
		src := src
		t.Run(strings.TrimSpace(src), func(t *testing.T) {
			want := captureStdout(t, src) // the record holds the reference answer here
			res, err := Compile(src)
			if err == nil {
				out := runIR(t, res.IR)
				if out != want {
					t.Errorf("--aot answered %q where the reference answers %q", out, want)
				}
				return
			}
			for _, banned := range []string{"global variable reference", "store i32 @.", "@.lst"} {
				if strings.Contains(err.Error(), banned) {
					t.Errorf("refusal leaked the module it failed to build (`%s`): %v", banned, err)
				}
			}
			if !strings.Contains(err.Error(), "container") {
				t.Errorf("refusal %q does not name what the program asked for (Gap R.38)", err)
			}
		})
	}
}
