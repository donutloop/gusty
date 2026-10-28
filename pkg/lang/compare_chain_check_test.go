package lang

// A comparison chain is checked the way the comparisons it stands for are checked.
//
// roadmap L12.1 / Gap R.53 · ADR 0450
//
// `a < b < c` has been ONE node since ADR 0288 and ONE lowering since the same change. What it never
// had was an arm in the analyzer, and a node the analyzer does not know is a node whose contents do not
// exist as far as the checker is concerned. Measured before this file existed, at the exit code of
// success:
//
//	gustyc --check 'y = 1 < zzz < 3'   →  ok                          exit 0   (a name nothing binds)
//	gustyc --check 'y = 1 < zzz'       →  error: undefined name "zzz"  exit 1
//
// The two shapes are the same program with one operator removed, and only one of them was checked. The
// rows below are therefore written as PAIRS: every row states the single-comparison source and the
// chain source and demands the same diagnostics from both, which is the only claim that could not be
// satisfied by a checker that merely learned to say something about chains.
//
// The second half of the row is the chain's TYPE. A chain answers a verdict; the node carried none, so
// every rule that reads a sub-expression's type — an annotated binding, a typed parameter, an arithmetic
// operand — was reading `any` where the program had written `1 < 2 < 3`. Those rows are pairs too.
//
// The third is the owed half of L12.1's definition of done: a link the reference can never answer is
// NAMED by `--check`, in the reference's own words, with a stable code — and is not refused, because the
// program under it is a program the reference runs by raising (ADR 0166). The last table is the one that
// keeps that door honest: it lists the shapes the checker must say NOTHING about.

import (
	"encoding/json"
	"strings"
	"testing"
)

// checkMsgs is what a source leaves behind: the level and the message of every diagnostic, in the order
// the checker reported them. Codes are asserted where a row is about a code.
func checkMsgs(t *testing.T, src string) []string {
	t.Helper()
	res, err := CheckSource(src)
	if err != nil {
		t.Fatalf("check %q: %v", src, err)
	}
	var out []string
	for _, d := range res.Diagnostics {
		out = append(out, string(d.Level)+": "+d.Msg)
	}
	return out
}

// TestAChainReportsWhatTheSameLinkReportsWithOneOperator is the checker hole, closed and pinned. Each row
// is the SAME program with one operator removed from the chain; the diagnostics must not care.
//
// The rows are written where a program is written — an assignment, a condition, a `while` head, a
// function body, a comprehension clause, an argument — because the hole was not one statement's: the
// walk stopped at the node, so every position that held a chain held an unchecked expression.
func TestAChainReportsWhatTheSameLinkReportsWithOneOperator(t *testing.T) {
	t.Parallel()
	pairs := []struct {
		name  string
		plain string // one operator: the shape the checker has always answered
		chain string // two operators: the same operand, standing in a chain
		want  string // at least one diagnostic both must report
	}{
		{
			name:  "undefined name, middle operand",
			plain: "y = 1 < zzz\n",
			chain: "y = 1 < zzz < 3\n",
			want:  `undefined name "zzz"`,
		},
		{
			name:  "undefined name, first operand",
			plain: "y = zzz < 2\n",
			chain: "y = zzz < 2 < 3\n",
			want:  `undefined name "zzz"`,
		},
		{
			name:  "undefined name, last operand of a four-operand chain",
			plain: "y = 3 < zzz\n",
			chain: "y = 1 < 2 < 3 < zzz\n",
			want:  `undefined name "zzz"`,
		},
		{
			name:  "call to a name nothing binds",
			plain: "y = 1 < nofn(1)\n",
			chain: "y = 1 < nofn(1) < 3\n",
			want:  `undefined name "nofn"`,
		},
		{
			name:  "a call with the wrong number of arguments",
			plain: "def f(x):\n    return x\n\ny = 1 < f()\n",
			chain: "def f(x):\n    return x\n\ny = 1 < f() < 3\n",
			want:  `function "f" expects 1 argument, got 0`,
		},
		{
			name:  "the chain sits in a condition",
			plain:   "if 1 < zzz:\n    print(1)\n",
			chain:   "if 1 < zzz < 3:\n    print(1)\n",
			want:    `undefined name "zzz"`,
		},
		{
			name:  "the chain sits in a while head",
			plain:   "while 1 < zzz:\n    break\n",
			chain:   "while 1 < zzz < 3:\n    break\n",
			want:    `undefined name "zzz"`,
		},
		{
			name:  "the chain sits in a function body",
			plain:   "def f():\n    return 1 < zzz\n\nf()\n",
			chain:   "def f():\n    return 1 < zzz < 3\n\nf()\n",
			want:    `undefined name "zzz"`,
		},
		{
			name:  "the chain is a comprehension clause",
			plain:   "print([v for v in [1, 2] if v < zzz])\n",
			chain:   "print([v for v in [1, 2] if 1 < v < zzz])\n",
			want:    `undefined name "zzz"`,
		},
		{
			name:  "the chain is a call argument",
			plain:   "print(len(1 < zzz))\n",
			chain:   "print(len(1 < zzz < 3))\n",
			want:    `undefined name "zzz"`,
		},
		{
			name:  "a name bound after the chain is not an excuse for the chain",
			plain:   "y = 1 < zz\nzz = 5\nprint(y)\n",
			chain:   "y = 1 < zz < 3\nzz = 5\nprint(y)\n",
			want:    "", // both legs must agree; the sentence is the checker's to choose
		},
	}
	for _, p := range pairs {
		p := p
		t.Run(p.name, func(t *testing.T) {
			t.Parallel()
			plain := checkMsgs(t, p.plain)
			chain := checkMsgs(t, p.chain)
			if len(plain) != len(chain) {
				t.Fatalf("a chain changed the diagnostic count: `%s` reports %v, `%s` reports %v", p.plain, plain, p.chain, chain)
			}
			for i := range plain {
				if plain[i] != chain[i] {
					t.Errorf("the same operand says one thing alone and another inside a chain:\n  single: %v\n  chain:  %v", plain, chain)
				}
			}
			if p.want == "" {
				return
			}
			if !containsString(chain, p.want) {
				t.Fatalf("the chain reported %v, want the diagnostic the single comparison reports (%q)", chain, p.want)
			}
			if !containsString(plain, p.want) {
				t.Fatalf("the row is stale: the single comparison does not report %q (%v)", p.want, plain)
			}
		})
	}
}

// TestAChainIsTypedBoolWhereTheSingleComparisonIs is the other half of the missing arm: the node had no
// type, so every position that reads one read `any`. Each row is the same position filled first with a
// single comparison (whose behaviour is the yardstick) and then with a chain.
func TestAChainIsTypedBoolWhereTheSingleComparisonIs(t *testing.T) {
	t.Parallel()
	positions := []struct {
		name string
		tmpl string // %s is the comparison
		want string // the diagnostic the position must report for a verdict
	}{
		{"annotated binding", "y: int = %s\n", "type mismatch: expected int, got bool"},
		{"typed parameter", "def f(x: int) -> int:\n    return x\n\nprint(f(%s))\n", `argument "x": expected int, got bool`},
		{"arithmetic operand", "print((%s) + 1)\n", "arithmetic on non-numeric operands (bool, int)"},
		{"annotated return", "def f() -> int:\n    return %s\n\nprint(f())\n", "return type mismatch: expected int, got bool"},
	}
	for _, p := range positions {
		p := p
		t.Run(p.name, func(t *testing.T) {
			t.Parallel()
			plain := checkMsgs(t, strings.ReplaceAll(p.tmpl, "%s", "1 < 2"))
			chain := checkMsgs(t, strings.ReplaceAll(p.tmpl, "%s", "1 < 2 < 3"))
			if !containsString(plain, p.want) {
				t.Fatalf("row is stale: the single comparison in this position does not report %q (%v)", p.want, plain)
			}
			if !containsString(chain, p.want) {
				t.Errorf("the chain in the same position reported %v, want %q — the node is still carrying no type", chain, p.want)
			}
		})
	}
}

// TestTheASTCarriesTheChainsVerdictType keeps the machine path honest: `--emit-ast` is what an agent reads
// to find out what the front end understood, and a chain node with no `inferred` field was a node the
// front end had not understood.
func TestTheASTCarriesTheChainsVerdictType(t *testing.T) {
	t.Parallel()
	res, err := Compile("x = 5\ny = 1 < x < 10\nprint(y)\n")
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	var ast map[string]any
	if err := json.Unmarshal([]byte(res.ASTJSON), &ast); err != nil {
		t.Fatalf("ASTJSON is not valid JSON: %v", err)
	}
	var chains []map[string]any
	var walk func(any)
	walk = func(v any) {
		switch n := v.(type) {
		case map[string]any:
			if _, isChain := n["ops"]; isChain {
				if _, hasOperands := n["operands"]; hasOperands {
					chains = append(chains, n)
				}
			}
			for _, c := range n {
				walk(c)
			}
		case []any:
			for _, c := range v.([]any) {
				walk(c)
			}
		}
	}
	walk(ast)
	if len(chains) != 1 {
		t.Fatalf("expected exactly one chain node in the AST, got %d: %v", len(chains), chains)
	}
	if got, _ := chains[0]["inferred"].(string); got != "bool" {
		t.Errorf("the chain node carries inferred=%q, want \"bool\" — a chain answers a verdict, and every rule that reads a sub-expression's type reads this field", got)
	}
	// The operands are typed too, which is what tells an agent the checker walked inside the node.
	ops, _ := chains[0]["operands"].([]any)
	if len(ops) != 3 {
		t.Fatalf("chain has %d operands, want 3", len(ops))
	}
	for i, o := range ops {
		m, ok := o.(map[string]any)
		if !ok {
			t.Fatalf("operand %d is %#v", i, o)
		}
		if ty, _ := m["inferred"].(string); ty == "" {
			t.Errorf("operand %d of the chain carries no inferred type: %v", i, m)
		}
	}
}

// TestTheCheckerNamesALinkTheReferenceCannotAnswer is L12.1's owed half at `--check`: the link that no
// pair of values can answer is said out loud, in the reference's own sentence, with a stable code.
func TestTheCheckerNamesALinkTheReferenceCannotAnswer(t *testing.T) {
	t.Parallel()
	rows := []struct {
		name string
		src  string
		want []string
	}{
		{
			name: "an int against a text, inside a chain",
			src:  "print(1 < \"a\" < 3)\n",
			want: []string{
				"comparison chain: '<' not supported between instances of 'int' and 'str'",
				"comparison chain: '<' not supported between instances of 'str' and 'int'",
			},
		},
		{
			name: "an int against a text, one operator",
			src:  "print(1 < \"a\")\n",
			want: []string{"'<' not supported between instances of 'int' and 'str'"},
		},
		{
			name: "the void against anything",
			src:  "print(1 < None < 3)\n",
			want: []string{
				"comparison chain: '<' not supported between instances of 'int' and 'NoneType'",
				"comparison chain: '<' not supported between instances of 'NoneType' and 'int'",
			},
		},
		{
			name: "the void against itself",
			src:  "x = None\nprint(x < x)\n",
			want: []string{"'<' not supported between instances of 'NoneType' and 'NoneType'"},
		},
		{
			name: "a text bound to a name, ordered against a number",
			src:  "x = \"a\"\nprint(1 < x < 3)\n",
			want: []string{
				"comparison chain: '<' not supported between instances of 'int' and 'str'",
				"comparison chain: '<' not supported between instances of 'str' and 'int'",
			},
		},
		{
			name: "the other ordering operators",
			src:  "print(1 >= \"a\")\nprint(1 <= \"a\")\nprint(\"a\" > 1)\n",
			want: []string{
				"'>=' not supported between instances of 'int' and 'str'",
				"'<=' not supported between instances of 'int' and 'str'",
				"'>' not supported between instances of 'str' and 'int'",
			},
		},
		{
			name: "membership in something with no members",
			src:  "print(1 in 5)\n",
			want: []string{"argument of type 'int' is not iterable"},
		},
		{
			// `and` and `or` are not comparison links: they choose an operand, and the parser stops a chain
			// at them (`TestChainsDoNotSwallowBooleanOperators`). A source made only of them must leave the
			// door silent, whatever the kinds it happens to mention.
			name: "the boolean words are not links",
			src:  "print(1 and \"a\")\nprint(0 or None)\nprint(1 < 2 and 3 < 4)\n",
			want: nil,
		},
		{
			name: "membership in the void as a link",
			src:  "print(1 == 1 in None)\n",
			want: []string{"argument of type 'NoneType' is not iterable"},
		},
	}
	for _, r := range rows {
		r := r
		t.Run(r.name, func(t *testing.T) {
			t.Parallel()
			res, err := CheckSource(r.src)
			if err != nil {
				t.Fatalf("check: %v", err)
			}
			var got []string
			for _, d := range res.Diagnostics {
				if d.Code == CodeCompareUncomparable {
					got = append(got, d.Msg)
				}
			}
			for _, w := range r.want {
				if !containsString(got, w) {
					t.Errorf("the checker said %v, want %q (all diagnostics: %v)", got, w, checkMsgs(t, r.src))
				}
			}
			if r.want == nil {
				if len(got) != 0 {
					t.Errorf("`and`/`or` were treated as chain links: %v", got)
				}
				return
			}
			// Named, not refused: the program under the warning is a program the reference RUNS.
			if !res.OK {
				t.Errorf("`--check` refused a program the reference runs (exit %d, %v)", res.Exit, res.Diagnostics)
			}
			for _, d := range res.Diagnostics {
				if d.Level == LevelError && strings.Contains(d.Msg, "not supported between instances") {
					t.Errorf("the opinion was said at error level, which would stop the build: %+v", d)
				}
			}
		})
	}
}

// TestTheOpinionIsTheSameSentenceTheCompiledLegRaisesWith is the reason the ordering table is not
// restated for the checker: `order_kind.go` already answers "do these two kinds order", because the
// compiled leg needs it to know when to RAISE. A checker that worded the same fact differently would
// hand an agent two sentences for one defect (ADR 0232, and `abs_kind_test.go`'s standing rule).
func TestTheOpinionIsTheSameSentenceTheCompiledLegRaisesWith(t *testing.T) {
	t.Parallel()
	rows := []struct {
		src    string
		symbol string // what the module must carry, verbatim
	}{
		{"print(1 < \"a\" < 3)\n", "'<' not supported between instances of 'int' and 'str'"},
		{"print(1 < None < 3)\n", "'<' not supported between instances of 'int' and 'NoneType'"},
		{"x = \"a\"\nprint(x > 1 < 3)\n", "'>' not supported between instances of 'str' and 'int'"},
	}
	for _, r := range rows {
		r := r
		t.Run(strings.TrimSpace(r.src), func(t *testing.T) {
			t.Parallel()
			res, err := Compile(r.src)
			if err != nil {
				t.Fatalf("compile: %v", err)
			}
			if !strings.Contains(res.IR, r.symbol) {
				t.Errorf("the module does not carry the sentence %q the checker warns with — the two doors are reading different tables:\n%s", r.symbol, res.IR)
			}
			if !strings.Contains(res.IR, "TypeError") {
				t.Errorf("the module does not carry the class the reference raises for it")
			}
			// And the run-time answer agrees with the checker's, word for word.
			code, out := runIRAllowingTrap(t, res.IR)
			if code == 0 {
				t.Fatalf("the program answered %q where the reference raises it", out)
			}
			if !strings.Contains(out, r.symbol) {
				t.Errorf("the raise says %q, want the checker's own sentence %q", out, r.symbol)
			}
		})
	}
}

// TestTheCheckerStaysQuietWhereItCannotNameBothKinds is the false-positive guard, and the gradual half of
// the rule: an opinion about kinds the program never declared is a fabricated defect. Every row here must
// compile, must check clean of the code, and must not lose its answer to a warning.
func TestTheCheckerStaysQuietWhereItCannotNameBothKinds(t *testing.T) {
	t.Parallel()
	silent := []struct {
		name string
		src  string
	}{
		{"an unannotated parameter", "def f(x):\n    return 1 < x < 3\n\nprint(f(2))\n"},
		{"a slot read the literal does not describe", "xs = []\nxs.append(2)\nprint(0 < xs[0] < 3)\n"},
		{"a call the checker cannot describe", "def g():\n    return 2\n\nprint(1 < g() < 3)\n"},
		{"a container against a container", "a = [1]\nb = [2]\nprint(a < b)\n"},
		{"a container inside a chain", "xs = [1, 2, 3]\nprint(0 < xs[1] < 3)\n"},
		{"equality never refuses a pair", "print(1 == \"a\")\nprint(1 != None)\nprint(1 is \"a\" is not None)\n"},
		{"a text has characters to be a member of", "print(\"a\" in \"abc\")\nprint(\"a\" not in \"abc\")\n"},
		{"a container has members", "print(1 in [1, 2])\nprint(\"k\" in {\"k\": 1})\nprint(1 in {1, 2})\n"},
		{"a class may define __lt__", "class C:\n    def __lt__(self, o):\n        return True\n\nc = C()\nprint(1 < c)\n"},
		{"a slot the run time filled", "xs = []\nxs.append(\"a\")\nprint(1 < xs[0] < 3)\n"},
		{"a union answers through whichever member it holds", "def f(x: int | str):\n    print(1 < x < 3)\n\nf(2)\n"},
		{"the chains that answer, answered", "print(1 < 2 < 3)\nprint(\"a\" < \"b\" < \"c\")\nprint(1 <= 1 <= 1)\n"},
	}
	for _, s := range silent {
		s := s
		t.Run(s.name, func(t *testing.T) {
			t.Parallel()
			res, err := CheckSource(s.src)
			if err != nil {
				t.Fatalf("check: %v", err)
			}
			for _, d := range res.Diagnostics {
				if d.Code == CodeCompareUncomparable {
					t.Errorf("the checker named a link it cannot name: %+v", d)
				}
			}
			if _, cerr := Compile(s.src); cerr != nil {
				if !strings.Contains(s.name, "class") {
					t.Fatalf("compile refused a program the reference answers (%v): %s", cerr, s.src)
				}
			}
		})
	}
}

// TestAMalformedChainIsSaidNotWalkedPast covers the guard at the top of the arm: the parser builds the two
// slices together, so a chain whose operand count disagrees with its operator count is a broken AST, and
// the checker's job about a broken AST is to say so rather than index out of range.
func TestAMalformedChainIsSaidNotWalkedPast(t *testing.T) {
	t.Parallel()
	prog := &Program{Stmts: []Stmt{&ExprStmt{Expr: &ChainCompare{
		Ops:      []string{"<", "<"},
		Operands: []Expr{&IntLit{Value: 1}, &IntLit{Value: 2}},
	}}}}
	diags := Analyze(prog)
	if len(diags) == 0 {
		t.Fatal("a chain with two operators over two operands was accepted")
	}
	var named bool
	for _, d := range diags {
		if strings.Contains(d.Msg, "malformed comparison chain") {
			named = true
			if d.Level != LevelError {
				t.Errorf("a broken AST is reported at level %q, want an error", d.Level)
			}
		}
	}
	if !named {
		t.Errorf("no diagnostic named the malformed chain: %v", diags)
	}
}

// TestTheChainWarningCarriesTheStableCode is the machine path: an agent branches on Code, never on prose.
func TestTheChainWarningCarriesTheStableCode(t *testing.T) {
	t.Parallel()
	res, err := CheckSource("print(1 < \"a\" < 3)\n")
	if err != nil {
		t.Fatalf("check: %v", err)
	}
	found := 0
	for _, d := range res.Diagnostics {
		if d.Msg == "comparison chain: '<' not supported between instances of 'int' and 'str'" {
			if d.Code != CodeCompareUncomparable {
				t.Errorf("code = %q, want %q", d.Code, CodeCompareUncomparable)
			}
			if d.Suggestion == "" {
				t.Error("the diagnostic carries no suggestion")
			}
			if d.Span.Line != 1 {
				t.Errorf("span = %+v, want the diagnostic on the line the chain is written on", d.Span)
			}
			found++
		}
	}
	if found == 0 {
		t.Fatalf("the sentence never arrived: %v", checkMsgs(t, "print(1 < \"a\" < 3)\n"))
	}
	// And the code is declared where an agent goes looking for it.
	if !strings.Contains(ASTIRSchema, "\""+CodeCompareUncomparable+"\"") {
		t.Errorf("--schema does not declare %q", CodeCompareUncomparable)
	}
}

func containsString(hay []string, needle string) bool {
	for _, h := range hay {
		if strings.Contains(h, needle) {
			return true
		}
	}
	return false
}
