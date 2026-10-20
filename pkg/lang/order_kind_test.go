package lang

// pkg/lang/order_kind_test.go — an ordering between two kinds that do not order against each other
// raises CPython's `TypeError` instead of answering a verdict (roadmap Gap R.85 and Gap R.202,
// ADR 0382).
//
// The shape of the defect was the shape of the value word. `None` is the void and its payload is 0;
// a text is an index into `@str_tab`. Both are i32s, so `<` between them was an `icmp`/`fcmp` away
// from an answer:
//
//	print(1 < None)       # 0 → `icmp slt i32 1, 0` → False at exit 0   (reference: TypeError)
//	print(None < 1)       # 0 → `icmp slt i32 0, 1` → True  at exit 0   (reference: TypeError)
//	print(None <= None)   # 0 → `icmp sle i32 0, 0` → True  at exit 0   (reference: TypeError)
//	print("a" < 1.5)      # @str_tab index → `sitofp` → `fcmp olt` → True at exit 0
//
// Thirty-six such programs sat at exit 0 with nothing raised and nothing refused — this project's
// priority-1 class — and four more raised the right class with the wrong type named ('int' for a
// verdict, a void and a text alike, which is Gap R.202). Four things are pinned here, each a
// different way to be wrong:
//
//   - the ANSWER table — every ordering the reference answers must answer byte-for-byte the same way
//     it did before the door existed, on both legs, because a gate that raises everything mistyped is
//     wrong in the other direction (ADR 0273's ladder);
//   - the TRAP table — class and message compared exactly, on both legs, in every position an
//     ordering sits in (a print, an `if`, a `while` head, a ternary, an `and`, a function body, a
//     comparison chain);
//   - the door asked DIRECTLY — the classifier and the pair table, so a widening that claims an
//     operand the compiler cannot see fails a decision row rather than a program row;
//   - the IR — a raising module carries the sentence and never the comparison, a program with no
//     cross-kind ordering carries neither the sentence nor a raise block, and no module carries a
//     string-table index into an `fcmp`.
//
// The CLI comparison against CPython lives in integration/order_kind_test.go, and the one-program
// witness is integration/programs/order_names_the_kinds.gy.

import (
	"strings"
	"testing"
)

// orderTrapRows are the programs the reference stops, with the class and the message it stops them
// with. `left`/`right` order matters to the sentence: the reference names the operand whose
// `__lt__` was tried first.
func orderTrapRows() []struct{ name, src, class, message string } {
	return []struct{ name, src, class, message string }{
		// ---- the void against a number: the word 0 was the whole story ----
		{"an int against None", "print(1 < None)\n", "TypeError", "'<' not supported between instances of 'int' and 'NoneType'"},
		{"None against an int", "print(None < 1)\n", "TypeError", "'<' not supported between instances of 'NoneType' and 'int'"},
		{"an int against None (<=)", "print(1 <= None)\n", "TypeError", "'<=' not supported between instances of 'int' and 'NoneType'"},
		{"None against an int (>)", "print(None > 1)\n", "TypeError", "'>' not supported between instances of 'NoneType' and 'int'"},
		{"an int against None (>=)", "print(1 >= None)\n", "TypeError", "'>=' not supported between instances of 'int' and 'NoneType'"},
		{"a float against None", "print(1.5 < None)\n", "TypeError", "'<' not supported between instances of 'float' and 'NoneType'"},
		{"None against a float", "print(None >= 1.5)\n", "TypeError", "'>=' not supported between instances of 'NoneType' and 'float'"},
		{"a verdict against None", "print(True > None)\n", "TypeError", "'>' not supported between instances of 'bool' and 'NoneType'"},
		{"None against a verdict", "print(None <= True)\n", "TypeError", "'<=' not supported between instances of 'NoneType' and 'bool'"},
		{"None against None", "print(None <= None)\n", "TypeError", "'<=' not supported between instances of 'NoneType' and 'NoneType'"},
		{"a computed int against None", "print((3 + 4) < None)\n", "TypeError", "'<' not supported between instances of 'int' and 'NoneType'"},
		{"a negated number against None", "print(-1 < None)\n", "TypeError", "'<' not supported between instances of 'int' and 'NoneType'"},

		// ---- a text against a number: the intern index was the whole story ----
		{"a text against a float", "print(\"a\" < 1.5)\n", "TypeError", "'<' not supported between instances of 'str' and 'float'"},
		{"a float against a text", "print(1.5 < \"a\")\n", "TypeError", "'<' not supported between instances of 'float' and 'str'"},
		{"a text against an int", "print(\"a\" < 1)\n", "TypeError", "'<' not supported between instances of 'str' and 'int'"},
		{"an int against a text", "print(1 < \"a\")\n", "TypeError", "'<' not supported between instances of 'int' and 'str'"},
		{"a verdict against a text", "print(True >= \"a\")\n", "TypeError", "'>=' not supported between instances of 'bool' and 'str'"},
		{"a text against None", "print(\"a\" < None)\n", "TypeError", "'<' not supported between instances of 'str' and 'NoneType'"},

		// ---- the names the RECORDS know: the binding, not the literal ----
		{"a name bound to None", "x = None\nprint(1 < x)\n", "TypeError", "'<' not supported between instances of 'int' and 'NoneType'"},
		{"a name bound to a text, against a float", "s = \"a\"\nprint(s < 1.5)\n", "TypeError", "'<' not supported between instances of 'str' and 'float'"},
		{"a name bound to a float, against a text", "n = 1.5\nprint(n < \"a\")\n", "TypeError", "'<' not supported between instances of 'float' and 'str'"},
		{"a name bound to None, in a condition", "x = None\nif 1 < x:\n    print(\"y\")\nelse:\n    print(\"n\")\n", "TypeError", "'<' not supported between instances of 'int' and 'NoneType'"},

		// ---- the answer of a call ----
		{"a call that answers the void", "def f():\n    return None\n\nprint(1 < f())\n", "TypeError", "'<' not supported between instances of 'int' and 'NoneType'"},
		{"a call with no return at all", "def f():\n    pass\n\nprint(1 < f())\n", "TypeError", "'<' not supported between instances of 'int' and 'NoneType'"},
		{"a call that answers a float, against a text", "def f():\n    return 1.5\n\nprint(\"a\" < f())\n", "TypeError", "'<' not supported between instances of 'str' and 'float'"},
		{"a builtin's count against None", "print(len(\"abc\") < None)\n", "TypeError", "'<' not supported between instances of 'int' and 'NoneType'"},
		{"a rendered number against an int", "print(str(1) < 2)\n", "TypeError", "'<' not supported between instances of 'str' and 'int'"},

		// ---- Gap R.202's three rows: the raise was right, the sentence named 'int' ----
		{"a verdict against a list names 'bool'", "print(True < [1])\n", "TypeError", "'<' not supported between instances of 'bool' and 'list'"},
		{"None against a list names 'NoneType'", "print(None < [1])\n", "TypeError", "'<' not supported between instances of 'NoneType' and 'list'"},
		{"a text against a list names 'str'", "print(\"a\" < [1])\n", "TypeError", "'<' not supported between instances of 'str' and 'list'"},
		{"a float against a dict", "print(1.5 > {\"a\": 1})\n", "TypeError", "'>' not supported between instances of 'float' and 'dict'"},
		{"None against a set", "print(None <= {1})\n", "TypeError", "'<=' not supported between instances of 'NoneType' and 'set'"},

		// ---- the positions an ordering sits in ----
		{"in a ternary test", "print(\"yes\" if None < 1 else \"no\")\n", "TypeError", "'<' not supported between instances of 'NoneType' and 'int'"},
		{"in an `and` compound", "print(1 < 2 and None < 3)\n", "TypeError", "'<' not supported between instances of 'NoneType' and 'int'"},
		{"in a while head, both kinds visible", "s = \"a\"\ni = 0\nwhile s < 1.5:\n    print(i)\n    i = i + 1\n", "TypeError", "'<' not supported between instances of 'str' and 'float'"},
		{"in a comparison chain, on the link that fails", "print(1.5 < \"a\" < 2)\n", "TypeError", "'<' not supported between instances of 'float' and 'str'"},
		{"as the operand of `not`", "print(not (None < 1))\n", "TypeError", "'<' not supported between instances of 'NoneType' and 'int'"},
	}
}

// orderFiledRows are the shapes this door still cannot name, pinned with the reference's answer beside
// what the compiled leg spends today, so the defect cannot move and cannot be quietly fixed by making
// the row an answer. Both are the same missing word as L11.1's tagged value word seen from the
// ordering door's seat: when only ONE side has a kind the compiler can read, the raise would have to
// name the other kind, and a word that is not carried cannot be named — `None`'s payload is the
// integer 0 and a parameter's word is whatever its call site wrote, and no tag distinguishes them.
// Roadmap Gap R.354.
func orderFiledRows() []struct{ name, src, wantCompiled, reference string } {
	return []struct{ name, src, wantCompiled, reference string }{
		{
			"the void against a name whose kind the records do not carry",
			"i = 0\nwhile None < i:\n    print(i)\n    i = i + 1\n",
			"",
			"TypeError: '<' not supported between instances of 'NoneType' and 'int'",
		},
		{
			"the void against a parameter",
			"def g(v):\n    return v < None\n\nprint(g(3))\n",
			"False\n",
			"TypeError: '<' not supported between instances of 'int' and 'NoneType'",
		},
		{
			"a text against a call the scan cannot describe",
			"def pick(c):\n    if c:\n        return \"z\"\n    return 7\n\nprint(1.5 < pick(1))\n",
			"False\n",
			"TypeError: '<' not supported between instances of 'float' and 'str'",
		},
		// The int has no record of its own: the front end keeps a record for a name whose latest
		// binding was the void, a verdict, a float, a text or a container, and an int is what a
		// slot holds when nothing was recorded. So one half of this pair is a fact the compiler
		// never wrote down, the door stays out, and the comparison road compares the two words.
		{
			"the void against a name bound to an int",
			"x = None\ny = 1\nprint(x < y)\n",
			"True\n",
			"TypeError: '<' not supported between instances of 'NoneType' and 'int'",
		},
		{
			"the void against a name bound to an int, reversed",
			"x = 1\ny = None\nprint(x < y)\n",
			"False\n",
			"TypeError: '<' not supported between instances of 'int' and 'NoneType'",
		},
	}
}

// TestWhatTheOrderingDoorStillCannotNameIsFiledNotFixed is Gap R.354: one side's kind is a run-time
// fact, so the door stays out and the ordinary comparison road answers the two words it was handed.
// The rows pin the compiled answer EXACTLY, pin that it is not a refusal, and pin that no module
// reaches the compiler-bug exit code. They are not on the record: `tools/recmerge` asks the reference
// and cross-checks the compiled leg, and a source whose two legs disagree has no answer to write.
func TestWhatTheOrderingDoorStillCannotNameIsFiledNotFixed(t *testing.T) {
	t.Parallel()
	for _, tc := range orderFiledRows() {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if msg := compiledRefusalOf(tc.src); msg != "" {
				t.Fatalf("the filed shape became a refusal (%s): %s", msg, tc.src)
			}
			res, err := Compile(tc.src)
			if err != nil {
				t.Fatalf("compile: %v", err)
			}
			assertNoForbiddenIR(t, tc.src, res.IR)
			if strings.Contains(res.IR, "COMPILER BUG") || strings.Contains(res.IR, "LLVM ERROR") {
				t.Fatalf("the filed shape became a toolchain failure:\n%s", res.IR)
			}
			if got := runIR(t, res.IR); got != tc.wantCompiled {
				t.Fatalf("the filed answer moved: got %q, pinned %q\nthe reference answers: %s\nsrc: %s",
					got, tc.wantCompiled, tc.reference, tc.src)
			}
			if HasGoldenAnswer(tc.src) {
				t.Fatalf("a source whose legs disagree is on the record: %s", tc.src)
			}
		})
	}
}

// TestAnOrderingBetweenKindsThatDoNotOrderRaisesWhatTheReferenceRaises is the row itself: the class
// is what `except` matches and the message is what a reader searches for, so both are compared
// exactly, on the compiled leg and on the record.
func TestAnOrderingBetweenKindsThatDoNotOrderRaisesWhatTheReferenceRaises(t *testing.T) {
	t.Parallel()
	for _, tc := range orderTrapRows() {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			res, err := Compile(tc.src)
			if err != nil {
				t.Fatalf("the compiled backend refused a program the oracle traps on: %v\n%s", err, tc.src)
			}
			assertNoForbiddenIR(t, tc.src, res.IR)
			out := runIRMayTrap(t, res.IR)
			if !strings.Contains(out, "Traceback (most recent call last):") {
				t.Errorf("the compiled program printed no traceback, it answered:\n%s", out)
			}
			if !strings.Contains(out, tc.message) {
				t.Errorf("compiled message missing\n  %q\nin\n%s", tc.message, out)
			}
			if strings.Contains(out, "codegen:") {
				t.Errorf("the compiled backend refused what the oracle traps on:\n%s", out)
			}
			if strings.Contains(out, "COMPILER BUG") || strings.Contains(out, "LLVM ERROR") {
				t.Errorf("the trap became a toolchain failure:\n%s", out)
			}
			if HasGoldenAnswer(tc.src) {
				ee, ok := recordTrapOf(t, tc.src)
				if !ok {
					t.Fatalf("the record says this program answers where the reference traps it")
				}
				if ee.ExnType != tc.class {
					t.Errorf("the record raised %q, want %q", ee.ExnType, tc.class)
				}
				if ee.ExnMsg != tc.message {
					t.Errorf("the record's message =\n  %q\nwant\n  %q", ee.ExnMsg, tc.message)
				}
			}
		})
	}
}

// TestAnOrderingThatRefusesToAnswerIsNotFiledOnTheLedger is the discipline behind the table: a row
// that traps must trap on the RECORD leg too, so a divergence cannot be invented by the compiled
// side alone. Every source here was recorded from the reference by tools/recmerge, which refuses an
// entry whose two legs disagree.
func TestEveryOrderTrapRowIsOnTheRecordAsATrap(t *testing.T) {
	asked := 0
	for _, tc := range orderTrapRows() {
		t.Run(tc.name, func(t *testing.T) {
			if !HasGoldenAnswer(tc.src) {
				t.Fatalf("no record covers this program: run it through tools/recmerge")
			}
			asked++
			ee, ok := recordTrapOf(t, tc.src)
			if !ok {
				t.Fatalf("the record answers a program the reference traps: %s", tc.src)
			}
			if ee.ExnType != tc.class || ee.ExnMsg != tc.message {
				t.Fatalf("record = %s: %q, want %s: %q", ee.ExnType, ee.ExnMsg, tc.class, tc.message)
			}
		})
	}
	if asked == 0 {
		t.Fatal("not one of this row's programs is on the record: the record leg is not being walked")
	}
}

// TestAnOrderingTheReferenceAnswersStillAnswers is the other half of the gate, and the half the
// ladder insists on: two numbers order, two texts order, a container orders against its own kind,
// and equality is total. None of that may be bought with a raise.
func TestAnOrderingTheReferenceAnswersStillAnswers(t *testing.T) {
	t.Parallel()
	for _, tc := range orderAnswerRows() {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if msg := compiledRefusalOf(tc.src); msg != "" {
				t.Fatalf("refused a program the reference answers: %s\n%s", msg, tc.src)
			}
			out := captureStdout(t, tc.src)
			if out != tc.want {
				t.Errorf("stdout %q, want %q\nsrc: %s", out, tc.want, tc.src)
			}
			res, err := Compile(tc.src)
			if err != nil {
				t.Fatalf("the compiled backend refused a program the oracle prints: %v\n%s", err, tc.src)
			}
			assertNoForbiddenIR(t, tc.src, res.IR)
			if got := runIR(t, res.IR); got != tc.want {
				t.Errorf("compiled stdout %q, want %q\nsrc: %s", got, tc.want, tc.src)
			}
		})
	}
}

func orderAnswerRows() []struct{ name, src, want string } {
	return []struct{ name, src, want string }{
		{"two ints", "print(1 < 2)\n", "True\n"},
		{"a float against an int", "print(1.5 > 1)\n", "True\n"},
		{"a verdict is a number", "print(True < 2)\n", "True\n"},
		{"a negated number", "print(-7 < -1)\n", "True\n"},
		{"a computed pair", "print((2 + 2) * 2 <= 9)\n", "True\n"},
		{"a builtin's count", "print(len(\"abc\") < 4)\n", "True\n"},
		{"the answer of true division", "print(7 / 2 > 3)\n", "True\n"},
		{"two texts order by characters", "print(\"a\" < \"b\")\n", "True\n"},
		{"two texts, the other way", "print(\"b\" < \"a\")\n", "False\n"},
		{"a text against a text variable", "s = \"b\"\nprint(\"a\" < s)\n", "True\n"},
		{"equality is total: None against an int", "print(1 == None)\n", "False\n"},
		{"inequality is total: None against None", "print(None != 1)\n", "True\n"},
		{"identity of the void", "print(None is None)\n", "True\n"},
		{"a container against its own kind", "a = [1]\nb = [2]\nprint(a < b)\n", "True\n"},
		{"a chain of numbers", "print(1 < 2 < 3)\n", "True\n"},
		{"a chain the middle fails", "print(1 < 2 < 1)\n", "False\n"},
		{"in an if", "if 1 < 2:\n    print(\"y\")\nelse:\n    print(\"n\")\n", "y\n"},
		{"in a while head", "i = 0\nwhile i < 3:\n    print(i)\n    i = i + 1\n", "0\n1\n2\n"},
		{"in a ternary", "print(\"yes\" if 2 > 1 else \"no\")\n", "yes\n"},
		{"in an and compound", "print(1 < 2 and 3 > 2)\n", "True\n"},
		{"a slot of a literal list", "xs = [3, 4]\nprint(xs[1] > 3)\n", "True\n"},
		{"a call's numeric answer", "def f():\n    return 4\n\nprint(f() > 3)\n", "True\n"},
		{"a parameter, both numbers", "def g(v):\n    return v < 10\n\nprint(g(3))\n", "True\n"},
	}
}

// TestTheOrderDoorAsksThePairTableDirectly asks the classifier and the family table as functions, so
// the rule can be broken only where somebody can see it break: a widening that names an operand the
// compiler cannot see, or a narrowing that claims a pair the reference orders, fails a decision row.
func TestTheOrderDoorAsksThePairTableDirectly(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name string
		lhs  string
		want string
	}{
		{"an int literal", "1", "int"},
		{"a float literal", "1.5", "float"},
		{"a verdict", "True", "bool"},
		{"a text literal", "\"a\"", "str"},
		{"the void", "None", "NoneType"},
		{"a list literal", "[1]", "list"},
		{"a dict literal", "{\"a\": 1}", "dict"},
		{"a set literal", "{1}", "set"},
		{"a tuple literal", "(1, 2)", "tuple"},
		{"a negated number", "-3", "int"},
		{"an arithmetic answer", "1 + 2", "int"},
		{"an arithmetic answer with a float", "1 + 2.5", "float"},
		{"a text built by concatenation", "\"a\" + \"b\"", "str"},
		{"the count of a text", "len(\"abc\")", "int"},
		{"a rendered value", "str(1)", "str"},
		{"the magnitude of a number", "abs(-3)", "int"},
		{"the magnitude of a float", "abs(-3.5)", "float"},
		{"a name bound to None", "theNone", "NoneType"},
		{"a name bound to a text", "theText", "str"},
		{"a name bound to a float", "theFloat", "float"},
		{"a name bound to a list", "theList", "list"},
		{"a call with no value return", "voidFn()", "NoneType"},
		// What the door must NOT claim: a kind that is a run-time fact.
		{"a slot read", "xs[0]", ""},
		{"a parameter", "param", ""},
		{"the magnitude of a slot", "abs(xs[0])", ""},
		{"an unknown call", "mystery()", ""},
		{"an attribute", "obj.field", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			g := orderProbeGen(t)
			src := "print((" + tc.lhs + ") < 0)\n"
			prog, err := Parse(src)
			if err != nil {
				t.Fatalf("cannot parse the probe %q: %v", src, err)
			}
			left := firstOrderingLeft(prog)
			if left == nil {
				t.Fatalf("the probe %q does not hold an ordering", src)
			}
			got, known := g.orderKindOf(left)
			if tc.want == "" {
				if known {
					t.Fatalf("the door names %q for %q; a run-time fact is not its question", got, tc.lhs)
				}
				return
			}
			if !known {
				t.Fatalf("the door cannot name %q (%s)", tc.lhs, tc.want)
			}
			if got != tc.want {
				t.Fatalf("the door names %q for %q, want %q", got, tc.lhs, tc.want)
			}
		})
	}
}

// TestTheFamilyTableOrdersWhatTheReferenceOrders is the second half of the door asked on its own:
// which PAIRS have no ordering. Both columns are the reference's names.
func TestTheFamilyTableOrdersWhatTheReferenceOrders(t *testing.T) {
	t.Parallel()
	noOrder := [][2]string{
		{"int", "NoneType"}, {"NoneType", "int"}, {"NoneType", "NoneType"},
		{"float", "NoneType"}, {"bool", "NoneType"}, {"str", "NoneType"},
		{"str", "int"}, {"int", "str"}, {"str", "float"}, {"float", "str"}, {"str", "bool"},
		{"bool", "list"}, {"NoneType", "list"}, {"str", "list"}, {"float", "dict"}, {"NoneType", "set"},
	}
	// Two containers are absent from this table on purpose: same kind or not, they are the
	// container-order door beside this one, which raises the reference's sentence for a mixed
	// pair and files the same-kind pairs it cannot yet answer (Gap R.201, Gap R.86).
	for _, p := range noOrder {
		if !orderPairHasNoOrdering(p[0], p[1]) {
			t.Errorf("the table lets %q order against %q; the reference raises", p[0], p[1])
		}
	}
	order := [][2]string{
		{"int", "int"}, {"int", "float"}, {"float", "bool"}, {"bool", "bool"},
		{"str", "str"}, {"list", "list"}, {"dict", "dict"}, {"set", "set"}, {"tuple", "tuple"},
		{"dict", "list"}, {"list", "set"}, // the container door raises these, not this one
		// an unknown kind is not an unorderable kind: the door stays out.
		{"int", ""}, {"", "int"}, {"", ""},
	}
	for _, p := range order {
		if orderPairHasNoOrdering(p[0], p[1]) {
			t.Errorf("the table refuses %q against %q; the reference answers", p[0], p[1])
		}
	}
}

// TestARaisingModuleCarriesTheSentenceAndNotTheComparison is the IR leg. A door that raises must not
// also run the comparison it refused — the `icmp`/`fcmp` over the two words the lowering would have
// produced has to be gone, not merely skipped at run time — and a program that never asks the
// question must not pay for the door at all.
func TestARaisingModuleCarriesTheSentenceAndNotTheComparison(t *testing.T) {
	t.Parallel()
	t.Run("a raising module carries the sentence, with no compare of the two words", func(t *testing.T) {
		t.Parallel()
		res, err := Compile("print(1 < None)\n")
		if err != nil {
			t.Fatalf("compile: %v", err)
		}
		if !strings.Contains(res.IR, "'<' not supported between instances of 'int' and 'NoneType'") {
			t.Fatalf("the module carries no sentence:\n%s", res.IR)
		}
		if body := defineBodyOf(t, res.IR, "main"); strings.Contains(body, "icmp slt i32 1, 0") {
			t.Fatalf("the module still compares the void's word against the int:\n%s", body)
		}
	})
	t.Run("a text against a float carries its sentence, with no lift of the index", func(t *testing.T) {
		t.Parallel()
		res, err := Compile("print(\"a\" < 1.5)\n")
		if err != nil {
			t.Fatalf("compile: %v", err)
		}
		if !strings.Contains(res.IR, "'<' not supported between instances of 'str' and 'float'") {
			t.Fatalf("the module carries no sentence:\n%s", res.IR)
		}
		// Scoped to the program's own `define`: the runtime helpers carry `fcmp olt double` for the
		// float orderings they legitimately perform, and an assertion over the whole file could never fail.
		if body := defineBodyOf(t, res.IR, "main"); strings.Contains(body, "fcmp") {
			t.Fatalf("the program still lifts the intern index into an fcmp:\n%s", body)
		}
	})
	t.Run("a program that never asks the question carries neither", func(t *testing.T) {
		t.Parallel()
		res, err := Compile("xs = [1, 2]\nprint(xs[1] > 0 and \"a\" < \"b\")\n")
		if err != nil {
			t.Fatalf("compile: %v", err)
		}
		if strings.Contains(res.IR, "not supported between instances") {
			t.Fatalf("an ordinary program grew a cross-kind sentence:\n%s", res.IR)
		}
	})
	t.Run("the answer of a raising program is not printed", func(t *testing.T) {
		t.Parallel()
		res, err := Compile("def g(v):\n    return 1.5 < \"a\"\n\nprint(g(3))\n")
		if err != nil {
			t.Fatalf("compile: %v", err)
		}
		if !strings.Contains(res.IR, "'<' not supported between instances of 'float' and 'str'") {
			t.Fatalf("the raise is missing from the callee:\n%s", res.IR)
		}
	})
}

// TestTheOrderDoorLeavesTheTaggedDoorItsSlots asks the neighbours: a slot's kind is a run-time fact,
// and the door that asks the object has always owned it. The compiled leg's answer must not move.
func TestTheOrderDoorLeavesTheTaggedDoorItsSlots(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct{ name, src, want string }{
		{"a slot holding a number, against a number", "xs = []\nxs.append(3)\nprint(xs[0] > 2)\n", "True\n"},
		{"a slot holding a text, against a text", "xs = []\nxs.append(\"b\")\nprint(xs[0] > \"a\")\n", "True\n"},
		{"a slot read through a computed index", "xs = [3, 4]\ni = 1\nprint(xs[i] > 3)\n", "True\n"},
		{"a dict entry against a number", "d = {\"k\": 5}\nprint(d[\"k\"] > 1)\n", "True\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if msg := compiledRefusalOf(tc.src); msg != "" {
				t.Fatalf("refused: %s", msg)
			}
			if got := captureStdout(t, tc.src); got != tc.want {
				t.Errorf("interpreted %q, want %q", got, tc.want)
			}
			res, err := Compile(tc.src)
			if err != nil {
				t.Fatalf("compile: %v", err)
			}
			if got := runIR(t, res.IR); got != tc.want {
				t.Errorf("compiled %q, want %q", got, tc.want)
			}
		})
	}
}

// recordTrapOf asks the record what a source raises. ok is false when the record answers it — which,
// for a row of the trap table, is a divergence rather than a difference of opinion.
func recordTrapOf(t *testing.T, src string) (*TrapError, bool) {
	t.Helper()
	err := goldenRunError(silentReporter{}, src)
	if err == nil {
		return nil, false
	}
	ee, ok := err.(*TrapError)
	return ee, ok
}

// orderProbeGen builds a generator with the two tables the classifier reads from the probe program —
// the function declarations and the answer direction — and the bindings the probe's own opening
// lines write. Everything else the classifier reads is the module's own record, filled by the compile
// the probe goes through.
func orderProbeGen(t *testing.T) *irGen {
	t.Helper()
	prog, err := Parse(orderProbeSource)
	if err != nil {
		t.Fatalf("the probe program does not parse: %v", err)
	}
	g := &irGen{
		answer:       answerKindPlanOf(prog, map[string]bool{}),
		fds:          map[string]*FuncDef{},
		funcs:        map[string]bool{},
		floatVars:    map[string]bool{},
		noneVars:     map[string]bool{},
		boolVars:     map[string]bool{},
		internedVars: map[string]bool{},
		strVals:      map[string]string{},
		listVars:     map[string]bool{},
	}
	for _, st := range prog.Stmts {
		if fd, ok := st.(*FuncDef); ok {
			g.fds[fd.Name] = fd
			g.funcs[fd.Name] = true
		}
	}
	// The bindings the probe writes, recorded the way the assignment road records them.
	g.noneVars["theNone"] = true
	g.strVals["theText"] = "hello"
	g.internedVars["theText"] = true
	g.floatVars["theFloat"] = true
	g.listVars["theList"] = true
	return g
}

// orderProbeSource declares the names and calls the classifier's rows reach. `param` is a parameter
// of a function the probe never binds, `mystery` is a call no table describes, and `obj.field` is an
// attribute of an instance — three kinds the door must leave alone.
const orderProbeSource = `def voidFn():
    pass

def mystery():
    return xs

class C:
    field = 1

theNone = None
theText = "hello"
theFloat = 2.5
theList = [1, 2]

def takes(param):
    return param

print(1 < 2)
`

// firstOrderingLeft digs out the left operand of the single ordering of a `print((X) < 0)` probe.
func firstOrderingLeft(prog *Program) Expr {
	var left Expr
	var walk func(n Expr)
	walk = func(n Expr) {
		if left != nil {
			return
		}
		switch e := n.(type) {
		case *BinOp:
			switch e.Op {
			case "<", "<=", ">", ">=":
				left = e.L
				return
			}
			walk(e.L)
			walk(e.R)
		case *Call:
			for _, a := range e.Args {
				walk(a)
			}
			walk(e.Fn)
		case *UnOp:
			walk(e.X)
		case *Index:
			walk(e.Obj)
			walk(e.Idx)
		}
	}
	for _, st := range prog.Stmts {
		if es, ok := st.(*ExprStmt); ok {
			walk(es.Expr)
		}
	}
	return left
}
