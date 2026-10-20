package lang

// pkg/lang/comp_clause_test.go — a comprehension's CLAUSE LIST is the nested loop its clauses spell
// (roadmap Gap R.352, ADR 0383).
//
// `[y for x in rows for y in x]` is two clauses. The reference has always read it that way; this
// toolchain stopped at the second `for` with `parse error: expected "]"`, so the flattening idiom —
// rows of rows, a matrix, a pair of ranges — did not parse at all. The parser now reads the whole list,
// the AST carries it, the formatter writes it back, every scanner that asked "what does this
// comprehension bind?" asks the list, and the comprehension road nests: one arm folds the element under
// all the bindings, the other builds the heap object one append per combination.
//
// Six things are pinned here, each a separate test:
//
//   - the clause list PARSES, into the shape the AST says, for a list, a set, a dict and a generator,
//     with a filter on either clause and with the later clause walking a name the earlier one bound;
//   - what the reference answers, the compiled leg answers, byte for byte, and no row of that table is a
//     refusal — a program the reference runs coming back as `codegen:` is the failure this row exists to
//     remove, and a wrong value at exit 0 is the one below it;
//   - every answer row is on the RECORD leg too (`testdata/interpreter-golden.json`), so a future cycle
//     that breaks one of them fails rather than skips;
//   - what the door still cannot walk is REFUSED in its own words, naming the shape and the row, with the
//     compiler-bug exit code forbidden — `rows = [[1,2]]` / `for x in rows` needs a real nested run-time
//     loop, and a comprehension over texts needs the value word that carries a text's kind (L11.1);
//   - the FORMATTER writes the clause list it read: a `gusty fmt` that printed only the first clause
//     would quietly delete the rest of the program, which is a worse defect than the one this row fixes;
//   - one clause is still one clause — the single-clause roads are the same roads they were, and the
//     table of one-clause programs at the bottom answers exactly as it did before the list existed.

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
)

// ---------------------------------------------------------------------------
// The clause list the parser reads.
// ---------------------------------------------------------------------------

func clauseParseRows() []struct {
	name    string
	src     string
	targets []string
	iters   []string
	filters int
} {
	return []struct {
		name    string
		src     string
		targets []string
		iters   []string
		filters int
	}{
		{"two clauses over two literals", "print([x + y for x in [1, 2] for y in [3, 4]])\n", []string{"x", "y"}, []string{"[1, 2]", "[3, 4]"}, 0},
		{"the second clause walks what the first bound", "print([y for x in [[1, 2]] for y in x])\n", []string{"x", "y"}, []string{"[[1, 2]]", "x"}, 0},
		{"three clauses", "print([x + y + z for x in [1] for y in [2] for z in [3]])\n", []string{"x", "y", "z"}, []string{"[1]", "[2]", "[3]"}, 0},
		{"a filter on the first clause", "print([x + y for x in [1, 2] if x > 1 for y in [3]])\n", []string{"x", "y"}, []string{"[1, 2]", "[3]"}, 1},
		{"a filter on each clause", "print([x for x in [1, 2, 3] if x > 1 for y in [10, 20] if y > 10])\n", []string{"x", "y"}, []string{"[1, 2, 3]", "[10, 20]"}, 2},
		{"a set comprehension", "print({x + y for x in [1] for y in [2]})\n", []string{"x", "y"}, []string{"[1]", "[2]"}, 0},
		{"a dict comprehension", "print({x: y for x in [1] for y in [2]})\n", []string{"x", "y"}, []string{"[1]", "[2]"}, 0},
		{"a range on either side", "print([x*10 + y for x in range(2) for y in range(2)])\n", []string{"x", "y"}, []string{"range(2)", "range(2)"}, 0},
		{"one clause is one clause", "print([x for x in [1, 2] if x > 1])\n", []string{"x"}, []string{"[1, 2]"}, 1},
	}
}

// TestTheClauseListParsesAsTheLoopItMeans is the front-end half of the row: the second `for` is a
// clause, not a syntax error, and the AST says how many there were.
func TestTheClauseListParsesAsTheLoopItMeans(t *testing.T) {
	t.Parallel()
	for _, tc := range clauseParseRows() {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			prog, err := Parse(tc.src)
			if err != nil {
				t.Fatalf("the reference runs this program and the parser refused it: %v\nsrc: %s", err, tc.src)
			}
			c := firstCompIn(prog)
			if c == nil {
				t.Fatalf("no comprehension in %s", tc.src)
			}
			clauses := c.compClauses()
			if len(clauses) != len(tc.targets) {
				t.Fatalf("the clause list has %d clauses, want %d (%v)\nsrc: %s", len(clauses), len(tc.targets), tc.targets, tc.src)
			}
			for i, cl := range clauses {
				if cl.Target == nil {
					t.Fatalf("clause %d binds no name", i)
				}
				if cl.Target.Value != tc.targets[i] {
					t.Errorf("clause %d binds %q, want %q", i, cl.Target.Value, tc.targets[i])
				}
				if got := exprSurface(cl.Iter); got != tc.iters[i] {
					t.Errorf("clause %d walks %s, want %s", i, got, tc.iters[i])
				}
			}
			withFilters := 0
			for _, cl := range clauses {
				if cl.Cond != nil {
					withFilters++
				}
			}
			if withFilters != tc.filters {
				t.Errorf("%d clauses carry a filter, want %d", withFilters, tc.filters)
			}
			// The first clause is ALSO spelled out in ForVar/Iter/Cond, because that is how every road
			// that ever read a comprehension reads one; the list and the three fields may not disagree.
			if len(clauses) > 0 {
				if c.ForVar != clauses[0].Target || c.Iter != clauses[0].Iter || c.Cond != clauses[0].Cond {
					t.Fatalf("the first clause and the fields every road reads are not the same clause")
				}
			}
		})
	}
}

// TestTheEmitASTCarriesTheClauseList is the machine path: an agent reading the AST out of the CLI sees
// the clause list, not one clause and a mystery.
func TestTheEmitASTCarriesTheClauseList(t *testing.T) {
	t.Parallel()
	const src = "print([x + y for x in [1, 2] for y in [3, 4]])\n"
	prog, err := Parse(src)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	raw, err := json.Marshal(prog)
	if err != nil {
		t.Fatalf("the AST does not marshal: %v", err)
	}
	var doc map[string]any
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatalf("re-parse: %v", err)
	}
	list := findJSONKey(doc, "clauses")
	if list == nil {
		t.Fatalf("no `clauses` in the AST json for %s", src)
	}
	two, ok := list.([]any)
	if !ok || len(two) != 2 {
		t.Fatalf("`clauses` is %v, want the two-clause list", list)
	}
	first, ok := two[0].(map[string]any)
	if !ok {
		t.Fatalf("clause 0 is not an object: %v", two[0])
	}
	for _, key := range []string{"target", "iter"} {
		if _, seen := first[key]; !seen {
			t.Errorf("clause 0 has no %q: %v", key, first)
		}
	}
}

// TestTheFormatterWritesTheClauseListItRead is the round trip. A formatter that wrote one clause of a
// two-clause loop would delete half the program on save, so this asserts the written source re-parses to
// the same clause list, and that formatting is a fixed point.
func TestTheFormatterWritesTheClauseListItRead(t *testing.T) {
	t.Parallel()
	for _, src := range []string{
		"print([x + y for x in [1, 2] for y in [3, 4]])\n",
		"print([y for x in [[1, 2]] for y in x])\n",
		"print([x for x in [1, 2, 3] if x > 1 for y in [10, 20] if y > 10])\n",
		"print({x: y for x in [1] for y in [2]})\n",
		"print([x + y + z for x in [1] for y in [2] for z in [3]])\n",
	} {
		name := src[:min(30, len(src))]
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			prog, err := Parse(src)
			if err != nil {
				t.Fatalf("parse: %v", err)
			}
			want := countClauses(prog)
			out := strings.TrimRight(Format(prog), " \t\n") + "\n"
			again, err := Parse(out)
			if err != nil {
				t.Fatalf("the formatter wrote source that does not parse: %v\nwrote: %s", err, out)
			}
			if got := countClauses(again); got != want {
				t.Fatalf("the formatter wrote %d clauses where the source has %d\nwrote: %s", got, want, out)
			}
			twice := strings.TrimRight(Format(compMustParse(t, out)), " \t\n") + "\n"
			if twice != out {
				t.Fatalf("formatting is not a fixed point:\n once: %q\n twice: %q", out, twice)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// The answers.
// ---------------------------------------------------------------------------

func clauseAnswerRows() []struct{ name, src, want string } {
	return []struct{ name, src, want string }{
		{"the row's own flattening", "print([y for x in [[1, 2], [3, 4]] for y in x])\n", "[1, 2, 3, 4]\n"},
		{"the row's own product", "print([[x, y] for x in [1, 2] for y in [3, 4]])\n", "[[1, 3], [1, 4], [2, 3], [2, 4]]\n"},
		{"an int sum over a product", "print([x + y for x in [1, 2] for y in [3, 4]])\n", "[4, 5, 5, 6]\n"},
		{"a range on either side", "print([x*10 + y for x in range(2) for y in range(2)])\n", "[0, 1, 10, 11]\n"},
		{"three clauses", "print([x + y + z for x in [1] for y in [2] for z in [3]])\n", "[6]\n"},
		{"a filter on the second clause", "print([x + y for x in [1, 2] for y in [3, 4] if y > 3])\n", "[5, 6]\n"},
		{"a filter on each clause", "print([x for x in [1, 2, 3] if x > 1 for y in [10, 20] if y > 10])\n", "[2, 3]\n"},
		{"an outer filter that skips the whole inner loop", "print([y for x in [1, 2] if x > 1 for y in [7, 8]])\n", "[7, 8]\n"},
		{"a set comprehension", "print({x + y for x in [1] for y in [2]})\n", "{3}\n"},
		{"a dict comprehension", "print({x: y for x in [1] for y in [2]})\n", "{1: 2}\n"},
		{"pairs as elements", "print([(x, y) for x in [1] for y in [2]])\n", "[(1, 2)]\n"},
		{"a row of one element", "print([y for x in [[5]] for y in x])\n", "[5]\n"},
		{"an empty inner row", "print([y for x in [[]] for y in x])\n", "[]\n"},
		{"an empty outer row", "print([y for x in [] for y in [1]])\n", "[]\n"},
		{"the same spelling twice", "print([x for x in [1] for x in [2]])\n", "[2]\n"},
		{"a comprehension the module reads back", "xs = [y for x in [[1, 2], [3]] for y in x]\nprint(xs)\nprint(len(xs))\n", "[1, 2, 3]\n3\n"},
		// A generator expression with a clause list asks the comprehension road's nested door and comes
		// back as the eager heap list this backend has always made of a generator; what it is LOOPED over
		// is the shape both legs answer alike. Printing one is the pre-existing eagerness divergence the
		// record already carries for a single clause (`print((x * 2 for x in range(4)))` is `[]` on the
		// record), so it is not this row's to fix — only the loop below is.
		{"a generator expression with two clauses, looped", "xs = (x + y for x in [1, 2] for y in [3])\nfor v in xs:\n    print(v)\n", "4\n5\n"},
		{"a generator expression with two clauses, folded by hand", "xs = (x * y for x in [1, 2] for y in [10, 20])\nt = 0\nfor v in xs:\n    t = t + v\nprint(t)\n", "90\n"},
	}
}

// TestAClauseListAnswersWhatTheReferenceAnswers is the compiled leg on the shapes the row measured.
func TestAClauseListAnswersWhatTheReferenceAnswers(t *testing.T) {
	t.Parallel()
	for _, tc := range clauseAnswerRows() {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if msg := compiledRefusalOf(tc.src); msg != "" {
				t.Fatalf("the compiler refused a program the reference runs: %s\nsrc: %s", msg, tc.src)
			}
			out := captureStdout(t, tc.src)
			if out != tc.want {
				t.Errorf("stdout %q, want %q\nsrc: %s", out, tc.want, tc.src)
			}
			res, err := Compile(tc.src)
			if err != nil {
				t.Fatalf("compile: %v", err)
			}
			if res.IR == "" {
				t.Fatalf("no IR for %s", tc.src)
			}
		})
	}
}

// TestEveryClauseListAnswerRowIsOnTheRecord asks the same programs of the record leg. A missing record is
// a hard failure, so the shapes cannot be dropped by dropping a line of the artifact.
func TestEveryClauseListAnswerRowIsOnTheRecord(t *testing.T) {
	// Serial on purpose: the tally below counts the subtests, and a parallel subtest runs after the
	// parent function has already returned — the count would be zero and the guard would fire on a run
	// that asked every row.
	asked := 0
	for _, tc := range clauseAnswerRows() {
		t.Run(tc.name, func(t *testing.T) {
			out, err := runGoldenStdout(t, tc.src)
			if err != nil {
				if err == errGoldenSkipped {
					t.Fatalf("this program is not on the record leg — record it with tools/recmerge\nsrc: %s", tc.src)
				}
				t.Fatalf("the record leg refused the program: %v\nsrc: %s", err, tc.src)
			}
			if out != tc.want {
				t.Errorf("the record prints %q, want %q\nsrc: %s", out, tc.want, tc.src)
			}
			asked++
		})
	}
	if asked == 0 {
		t.Fatal("not one of this row's programs is on the record: the record leg is not being walked")
	}
}

// ---------------------------------------------------------------------------
// What is filed rather than answered.
// ---------------------------------------------------------------------------

func clauseFiledRows() []struct{ name, src, wantCompiled, row, reference string } {
	return []struct{ name, src, wantCompiled, row, reference string }{
		{
			"an outer clause over a container the run time built",
			"rows = [[1, 2], [3, 4]]\nprint([y for x in rows for y in x])\n",
			"a container the run time builds", "Gap R.356",
			"[1, 2, 3, 4]",
		},
		{
			"a clause over a text, which needs a text's kind in the word",
			"print([c for w in [\"ab\", \"cd\"] for c in w])\n",
			"walks an element the comprehension road cannot carry", "Gap R.352",
			"['a', 'b', 'c', 'd']",
		},
		{
			"a clause over floats",
			"print([x + y for x in [1.5] for y in [1]])\n",
			"walks an element the comprehension road cannot carry", "Gap R.352",
			"[2.5]",
		},
		{
			"an element that is a container storing a clause's non-number binding",
			"print([[x, y] for x in [1] for y in [[3]]])\n",
			"the container literal the element builds keeps one word per slot", "Gap R.355",
			"[[1, [3]]]",
		},
	}
}

// TestWhatTheClauseListStillCannotWalkIsFiledNotFixed pins the remaining half of the row: the nested
// run-time loop, and a clause that walks a text or a float. Each row pins the refusal's own words, that
// the compiled leg REFUSES rather than answers, and that the compiler's own module is never rejected —
// exit 2 for a program the reference runs is a compiler bug (ADR 0166), not a refusal.
func TestWhatTheClauseListStillCannotWalkIsFiledNotFixed(t *testing.T) {
	t.Parallel()
	for _, tc := range clauseFiledRows() {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			msg := compiledRefusalOf(tc.src)
			if msg == "" {
				t.Fatalf("this shape compiled, which means the filed row (Gap R.352) is paid and must be promoted, not re-asserted:\nsrc: %s", tc.src)
			}
			if !strings.Contains(msg, tc.wantCompiled) {
				t.Fatalf("the refusal no longer names the shape it files:\n got %q\nwant it to contain %q", msg, tc.wantCompiled)
			}
			if !strings.Contains(msg, tc.row) {
				t.Fatalf("a refusal must name the row that owns it (ADR 0346):\n got %q\nwant it to cite %q", msg, tc.row)
			}
			if strings.Contains(msg, "LLVM ERROR") || strings.Contains(msg, "COMPILER BUG") || strings.Contains(msg, "reference operand") {
				t.Fatalf("the refusal is an assembler rejection, which is exit 2 and a compiler bug (ADR 0166):\n%s", msg)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// One clause is still one clause.
// ---------------------------------------------------------------------------

// TestOneClauseComprehensionsAreUntouched is the ladder: every shape this backend answered before the
// clause list exists still answers, through the same roads, with the same bytes.
func TestOneClauseComprehensionsAreUntouched(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct{ name, src, want string }{
		{"a list over a literal", "print([x * 2 for x in [1, 2, 3]])\n", "[2, 4, 6]\n"},
		{"a list over range", "print([x for x in range(3)])\n", "[0, 1, 2]\n"},
		{"one filter", "print([x for x in [1, 2, 3, 4] if x % 2 == 0])\n", "[2, 4]\n"},
		{"a set", "print({x for x in [1, 1, 2]})\n", "{1, 2}\n"},
		{"a dict", "print({x: x * 2 for x in [1, 2]})\n", "{1: 2, 2: 4}\n"},
		{"a container variable's elements", "xs = [1, 2, 3]\nprint([x + 1 for x in xs])\n", "[2, 3, 4]\n"},
		{"a nested container element", "print([[x, x] for x in [1, 2]])\n", "[[1, 1], [2, 2]]\n"},
		{"sum over a comprehension", "print(sum([x for x in [1, 2, 3]]))\n", "6\n"},
		{"len of a comprehension", "print(len([x for x in [1, 2, 3]]))\n", "3\n"},
		{"a comprehension inside a function", "def f():\n    return [x * 2 for x in [1, 2]]\n\nprint(f())\n", "[2, 4]\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if msg := compiledRefusalOf(tc.src); msg != "" {
				t.Fatalf("a one-clause comprehension stopped compiling: %s\nsrc: %s", msg, tc.src)
			}
			prog := compMustParse(t, tc.src)
			if c := firstCompIn(prog); c != nil && !compIsSingleClause(c) {
				t.Fatalf("a one-clause source built a clause list of %d\nsrc: %s", len(c.compClauses()), tc.src)
			}
			if out := captureStdout(t, tc.src); out != tc.want {
				t.Errorf("stdout %q, want %q\nsrc: %s", out, tc.want, tc.src)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// helpers
// ---------------------------------------------------------------------------

// firstCompIn answers the first comprehension the program contains. It walks the AST with the same
// reflection the front end's own free-name scans use, so it names no field of the grammar and cannot
// fall behind a node type added later.
func firstCompIn(prog *Program) *Comp {
	if prog == nil {
		return nil
	}
	var found *Comp
	var visit func(rv reflect.Value)
	visit = func(rv reflect.Value) {
		if found != nil || !rv.IsValid() {
			return
		}
		switch rv.Kind() {
		case reflect.Ptr:
			if rv.IsNil() {
				return
			}
			if c, ok := rv.Interface().(*Comp); ok {
				found = c
				return
			}
			visit(rv.Elem())
		case reflect.Slice, reflect.Array:
			for i := 0; i < rv.Len(); i++ {
				visit(rv.Index(i))
			}
		case reflect.Interface:
			if rv.IsNil() {
				return
			}
			visit(rv.Elem())
		case reflect.Struct:
			for i := 0; i < rv.NumField(); i++ {
				visit(rv.Field(i))
			}
		case reflect.Map:
			for it := rv.MapRange(); it.Next(); {
				visit(it.Value())
			}
		}
	}
	visit(reflect.ValueOf(prog.Stmts))
	return found
}

// countClauses counts the clauses of every comprehension in a program.
func countClauses(prog *Program) int {
	if prog == nil {
		return 0
	}
	raw, err := json.Marshal(prog)
	if err != nil {
		return 0
	}
	var doc any
	if err := json.Unmarshal(raw, &doc); err != nil {
		return 0
	}
	return countJSONArrayKey(doc, "clauses")
}

func countJSONArrayKey(doc any, key string) int {
	total := 0
	switch v := doc.(type) {
	case map[string]any:
		for k, child := range v {
			if k == key {
				if list, ok := child.([]any); ok {
					total += len(list)
					continue
				}
			}
			total += countJSONArrayKey(child, key)
		}
	case []any:
		for _, child := range v {
			total += countJSONArrayKey(child, key)
		}
	}
	return total
}

// findJSONKey answers the first value stored under key, anywhere in a decoded AST.
func findJSONKey(doc any, key string) any {
	switch v := doc.(type) {
	case map[string]any:
		if got, seen := v[key]; seen {
			return got
		}
		for _, child := range v {
			if got := findJSONKey(child, key); got != nil {
				return got
			}
		}
	case []any:
		for _, child := range v {
			if got := findJSONKey(child, key); got != nil {
				return got
			}
		}
	}
	return nil
}

func compMustParse(t *testing.T, src string) *Program {
	t.Helper()
	prog, err := Parse(src)
	if err != nil {
		t.Fatalf("parse %q: %v", src, err)
	}
	return prog
}
