package lang

// A read is a read wherever it sits, or the compiler starts deleting programs.
//
// `deadListAssignments` answers one question — is this name ever read? — and acts on it by not emitting the
// allocation at all. For as long as the answer came from walkers that knew about binary operators, call
// arguments in one shape, comparisons, `if`s and returns, a read in any other construct was invisible to it.
// The visible symptom was the wrong-answer class, not a crash:
//
//	xs = [1, 2, 3]
//	print(f"{xs[0]}")      # wrote nothing at all — the list had been "deleted", and slot 0 read back as 0
//
// The same mechanism sat behind four refusals this cycle also unblocked: a comprehension whose iterable was a
// "dead" static list, a slice over one, a comparison over one, an `in` test over one. Each time the compiler
// had decided, from an incomplete view of the program, that the program did not look at the value — and the
// sentence it printed was about a construct the source did not contain.
//
// The fix is a walker driven from the AST's own field types rather than a list of node names (roadmap
// Gap R.289, ADR 0356), plus the guard below that fails when ast.go grows an expression the walk has no row
// for — so the next node type is a failing test rather than a missing answer discovered a cycle later.
//
// The second half of this file is the policy a compiled `def` body keeps: it is a separate function and
// cannot read module-level state at all (Gap R.35 says so out loud), so counting its reads as module reads
// would spend the permission the module body genuinely needs. A lambda body is an expression in this module
// and IS a read; a `def` body is not. That asymmetry is asserted here rather than left to the fall of a
// switch, and the day it changes is Gap R.290's row.

import (
	goast "go/ast"
	goparser "go/parser"
	gotoken "go/token"
	"reflect"
	"sort"
	"strings"
	"testing"
)

// irOf compiles source to textual IR without the forbidden-pattern gate, for the rows below that ask which
// calls a module emits.
func irOf(t *testing.T, src string) string {
	t.Helper()
	res, err := Compile(src)
	if err != nil {
		t.Fatalf("compile %q: %v", src, err)
	}
	return res.IR
}

// slotWriteCount counts the stores the container road emits: `rt_set_elem` is what a heap slot costs, and a
// list the compiler decided nobody reads emits none of them.
func slotWriteCount(t *testing.T, src string) int {
	t.Helper()
	return strings.Count(irOf(t, src), "call void @rt_set_elem")
}

// TestAReadInTheConstructsTheOldWalkersMissedKeepsTheAllocation is the wrong-answer half. Every row is a
// program whose ONLY use of the list sits in a construct the pre-Gap-R.289 walkers did not visit; the
// allocation must still be there, and the answer must still be right.
func TestAReadInTheConstructsTheOldWalkersMissedKeepsTheAllocation(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name string
		src  string
		want string // what the program prints, read off CPython
	}{
		{"an_fstring_field", "xs = [1, 2, 3]\nprint(f\"{xs[0]}\")\n", "1\n"},
		{"an_fstring_field_of_a_mixed_slot", "xs = [1.5, \"a\"]\nprint(f\"{xs[1]}\")\n", "a\n"},
		{"an_fstring_field_of_a_dict_entry", "d = {1: 3.5}\nprint(f\"{d[1]}\")\n", "3.5\n"},
		{"a_keyword_argument", "def f(v):\n    print(v)\nxs = [1, 2, 3]\nf(v = xs[0])\n", "1\n"},
		{"a_chain_comparison", "xs = [1, 2, 3]\nprint(0 < xs[0] < 5)\n", "True\n"},
		{"a_comprehension_iterable", "xs = [1, 2, 3]\nys = [x for x in xs]\nprint(ys)\n", "[1, 2, 3]\n"},
		{"a_comprehension_condition", "xs = [1, 2, 3]\nys = [1 for x in xs if x > 1]\nprint(ys)\n", "[1, 1]\n"},
		{"a_slice_of_a_list", "xs = [1, 2, 3]\nprint(xs[0:2])\n", "[1, 2]\n"},
		{"a_slice_bound", "xs = [1, 2, 3]\nn = 2\nprint(xs[0:n])\n", "[1, 2]\n"},
		{"a_ternary_branch", "xs = [1, 2, 3]\nprint(xs[0] if True else xs[1])\n", "1\n"},
		{"a_ternary_condition", "xs = [1, 2, 3]\nprint(7 if xs[0] else 8)\n", "7\n"},
		{"an_attribute_read", "xs = []\nxs.append([7, 8])\nprint(xs[0][1])\n", "8\n"},
		{"a_match_guard", "xs = [1, 2, 3]\nmatch xs[0]:\n    case v if v > 0:\n        print(v)\n", "1\n"},
		{"a_match_subject", "xs = [1, 2, 3]\nmatch xs:\n    case [a, b, c]:\n        print(a + c)\n", "4\n"},
		{"a_raise_caught_by_a_handler", "xs = [1, 2, 3]\ntry:\n    raise Exception(str(xs[0]))\nexcept Exception:\n    print(9)\n", "9\n"},
		{"a_container_method_argument", "xs = [1, 2]\nxs.append(3)\nprint(len(xs))\n", "3\n"},
		{"a_measured_container", "xs = [1, 2, 3]\nprint(len(xs))\n", "3\n"},
	} {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if n := slotWriteCount(t, tc.src); n == 0 {
				t.Fatalf("the list was never allocated — its only read is invisible to the escape analysis, which "+
					"is how %s printed a zero instead of %q:\n%s", tc.name, tc.want, tc.src)
			}
			if got := compiledOut(t, tc.src); got != tc.want {
				t.Errorf("%s printed %q, the reference prints %q:\n%s", tc.name, got, tc.want, tc.src)
			}
		})
	}
}

// The constructs whose read the analysis has to keep, but whose ANSWER the backend does not have yet — a
// tuple in an argument list, a generator passed to a builtin, a class body reading a module name. The row
// cannot assert a printed answer without inventing one, so it asserts the half that is this file's subject:
// the name is not declared dead, whatever the codegen then does with it. Each row names the gap that owes
// its answer, so the refusal stays on the tracker instead of hiding behind a passing test.
func TestAReadCountsEvenWhereTheBackendHasNoAnswerYet(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name   string
		src    string
		row    string
		refuse string // the sentence the compiled backend gives today, kept honest
	}{
		{
			name:   "a_tuple_element",
			src:    "xs = [1, 2, 3]\nprint((xs[0], 7))\n",
			row:    "Gap R.295",
			refuse: "unsupported expression *lang.Tuple",
		},
		{
			name:   "a_generator_passed_to_a_builtin",
			src:    "xs = [1, 2, 3]\nprint(sum((x for x in xs)))\n",
			row:    "Gap R.296",
			refuse: "sum requires an inline list",
		},
		{
			name:   "a_class_body_member",
			src:    "xs = [1, 2, 3]\nclass C:\n    first = xs[0]\nprint(C.first)\n",
			row:    "Gap R.297",
			refuse: "unsupported attr expression",
		},
		{
			name:   "a_comprehension_reading_a_class_body_name",
			src:    "xs = [1, 2, 3]\nys = [x for x in xs]\nprint(ys)\n",
			row:    "Gap R.289",
			refuse: "",
		},
	} {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			prog, err := Parse(tc.src)
			if err != nil {
				t.Fatalf("the row's own source does not parse, so it proves nothing about the read set: %v\n%s", err, tc.src)
			}
			if deadListAssignments(prog.Stmts)["xs"] {
				t.Fatalf("%s: the only read of the list is invisible to the escape analysis, so its binding is "+
					"deleted under it (roadmap Gap R.289):\n%s", tc.name, tc.src)
			}
			if tc.refuse != "" {
				_, err := Compile(tc.src)
				if err == nil {
					t.Fatalf("%s now compiles: the answer landed, so this row belongs in the answered table and %s "+
						"can close:\n%s", tc.name, tc.row, tc.src)
				}
				if !strings.Contains(err.Error(), tc.refuse) {
					t.Fatalf("%s refuses with %q, not the sentence %s documents (%q):\n%s",
						tc.name, err.Error(), tc.row, tc.refuse, tc.src)
				}
			}
		})
	}
}

// The other direction, so the analysis is a decision and not a surrender: a list nothing reads is still
// eliminated, and eliminating it does not change what the program says.
func TestAListNothingReadsIsStillNotAllocated(t *testing.T) {
	t.Parallel()
	src := "xs = [1, 2, 3]\nprint(7)\n"
	if n := slotWriteCount(t, src); n != 0 {
		t.Fatalf("a list no expression reads still cost %d slot stores; the escape analysis decides nothing:\n%s", n, src)
	}
	if got := compiledOut(t, src); got != "7\n" {
		t.Fatalf("%s printed %q, want \"7\\n\"", src, got)
	}
	if ref, err := pythonTwin(t, src); err == nil && ref != "7\n" {
		t.Fatalf("the reference prints %q for the elimination case, so this row's expectation is invented", ref)
	}
}

// The policy a compiled `def` body keeps: it is a separate function and cannot read module state (Gap R.35),
// so counting it as a read would spend the permission the module body itself needs. A lambda body is an
// expression the module evaluates here, and IS a read.
func TestALambdaBodyIsAReadAndADefBodyIsNot(t *testing.T) {
	t.Parallel()
	lambdaSrc := "xs = [10, 20]\nf = lambda v: xs[1]\nprint(f(0))\n"
	if n := slotWriteCount(t, lambdaSrc); n == 0 {
		t.Fatalf("a lambda body reading the list did not count as a read — a closure is exactly the read this "+
			"analysis must keep:\n%s", lambdaSrc)
	}
	defSrc := "xs = [1, 2, 3]\ndef f():\n    return xs[0]\nprint(1)\n"
	if n := slotWriteCount(t, defSrc); n != 0 {
		t.Fatalf("a def body counted as a read; a compiled def body cannot read module state at all (Gap R.35), "+
			"so this spends the permission the module body itself needs:\n%s", defSrc)
	}
	// …and the def's DEFAULT is a module-level expression, walked even though the body is not.
	defaultSrc := "xs = [1, 2, 3]\ndef f(v = xs[0]):\n    return 0\nprint(xs[0])\n"
	if n := slotWriteCount(t, defaultSrc); n == 0 {
		t.Fatalf("a parameter default reading the list did not count as a read, though the default runs where "+
			"the def statement runs:\n%s", defaultSrc)
	}
}

// readSetRow is one expression shape and the wrapper that puts a read of `xs` inside it.
type readSetRow struct {
	node string
	wrap func(Expr) Expr
}

// readSetRows covers every concrete expression ast.go declares (the guard below checks the census against
// it), each holding a subscript read of a module-level list.
var readSetRows = []readSetRow{
	{"Name", func(e Expr) Expr { return &BinOp{Op: "+", L: e, R: &IntLit{Value: 1}} }},
	{"IntLit", func(e Expr) Expr { return &BinOp{Op: "+", L: &IntLit{Value: 1}, R: e} }},
	{"FloatLit", func(e Expr) Expr { return &BinOp{Op: "+", L: &FloatLit{Value: 1}, R: e} }},
	{"BoolLit", func(e Expr) Expr { return &CondExpr{If: e, Cond: &BoolLit{Value: true}, Else: &IntLit{Value: 0}} }},
	{"NoneLit", func(e Expr) Expr { return &BinOp{Op: "==", L: e, R: &NoneLit{}} }},
	{"StrLit", func(e Expr) Expr { return &Call{Fn: &Name{Value: "print"}, Args: []Expr{&StrLit{Value: "a"}, e}} }},
	{"Tuple", func(e Expr) Expr { return &Tuple{Elems: []Expr{e, &IntLit{Value: 1}}} }},
	{"ListLit", func(e Expr) Expr { return &ListLit{Elems: []Expr{e}} }},
	{"SetLit", func(e Expr) Expr { return &SetLit{Elems: []Expr{e}} }},
	{"DictLit", func(e Expr) Expr { return &DictLit{Keys: []Expr{e}, Vals: []Expr{&IntLit{Value: 1}}} }},
	{"FString", func(e Expr) Expr {
		return &FString{Parts: []FStringPart{{Lit: "v="}, {Expr: e}, {Lit: "!"}}}
	}},
	{"BinOp", func(e Expr) Expr { return &BinOp{Op: "+", L: e, R: &IntLit{Value: 1}} }},
	{"UnOp", func(e Expr) Expr { return &UnOp{Op: "-", X: e} }},
	{"ChainCompare", func(e Expr) Expr {
		return &ChainCompare{Ops: []string{"<"}, Operands: []Expr{&IntLit{Value: 0}, e, &IntLit{Value: 9}}}
	}},
	{"CondExpr", func(e Expr) Expr { return &CondExpr{If: e, Cond: &BoolLit{Value: true}, Else: &IntLit{Value: 0}} }},
	{"AssignExpr", func(e Expr) Expr { return &AssignExpr{Name: &Name{Value: "y"}, Value: e} }},
	{"Call", func(e Expr) Expr { return &Call{Fn: &Name{Value: "print"}, Args: []Expr{e}} }},
	{"KeywordArg", func(e Expr) Expr {
		return &Call{Fn: &Name{Value: "print"}, Args: []Expr{&KeywordArg{Name: "v", Value: e}}}
	}},
	{"Index", func(e Expr) Expr { return &Index{Obj: e, Idx: &IntLit{Value: 1}} }},
	{"Slice", func(e Expr) Expr {
		return &Index{Obj: &Name{Value: "ss"}, Idx: &Slice{Obj: e, Low: &IntLit{Value: 0}}}
	}},
	{"Attr", func(e Expr) Expr { return &Attr{Obj: e, Name: &Name{Value: "append"}} }},
	{"Lambda", func(e Expr) Expr { return &Lambda{Params: []*Param{{Name: "v"}}, Body: e} }},
	{"AwaitExpr", func(e Expr) Expr { return &AwaitExpr{Expr: e} }},
	{"Comp", func(e Expr) Expr {
		return &Comp{Kind: CompList, Elems: []Expr{&IntLit{Value: 1}}, ForVar: &Name{Value: "i"}, Iter: e}
	}},
	{"Generator", func(e Expr) Expr {
		return &Generator{Elems: []Expr{&IntLit{Value: 1}}, ForVar: &Name{Value: "i"}, Iter: e}
	}},
}

// TestTheReadSetKeepsAReadHeldByEveryExpressionShape asks the analysis directly, on a tree it builds, that no
// expression shape lets a read fall through. The walk is driven from field TYPES, so a row per node is the
// only way to prove a node is reached rather than assumed.
func TestTheReadSetKeepsAReadHeldByEveryExpressionShape(t *testing.T) {
	t.Parallel()
	for _, tc := range readSetRows {
		tc := tc
		t.Run(tc.node, func(t *testing.T) {
			t.Parallel()
			read := &Index{Obj: &Name{Value: "xs"}, Idx: &IntLit{Value: 0}}
			prog := []Stmt{
				&AssignStmt{Target: &Name{Value: "xs"}, Value: &ListLit{Elems: []Expr{&IntLit{Value: 1}, &IntLit{Value: 2}}}},
				&ExprStmt{Expr: tc.wrap(read)},
			}
			if deadListAssignments(prog)["xs"] {
				t.Fatalf("a list read *inside* a %s was declared dead: the walk does not reach that node, so the "+
					"allocation is deleted under the read (roadmap Gap R.289)", tc.node)
			}
		})
	}
}

// TestEveryExpressionInASTHasAReadSetRow is the guard that makes the next node type a failing test rather
// than a missing answer: ast.go declares the expressions, and every one of them has to appear in the table
// above. Before the walker was driven from field types, four of these were invisible to the escape analysis
// for whole cycles — FString, KeywordArg, ChainCompare, and a comprehension's iterable.
func TestEveryExpressionInASTHasAReadSetRow(t *testing.T) {
	t.Parallel()
	have := map[string]bool{}
	for _, tc := range readSetRows {
		have[tc.node] = true
	}
	for _, name := range astExprTypeNames(t) {
		if !have[name] {
			t.Fatalf("ast.go declares expression %s and the read-set table has no row for it; the escape "+
				"analysis cannot be trusted for a construct nothing asks about (roadmap Gap R.289, ADR 0356)", name)
		}
	}
}

// astExprTypeNames reads ast.go and returns the types that carry the expression marker, so the guard above is
// a statement about the real AST rather than about someone's list of it.
func astExprTypeNames(t *testing.T) []string {
	t.Helper()
	fset := gotoken.NewFileSet()
	f, err := goparser.ParseFile(fset, "ast.go", nil, 0)
	if err != nil {
		t.Fatalf("cannot read ast.go for the expression census: %v", err)
	}
	out := map[string]bool{}
	for _, d := range f.Decls {
		fn, ok := d.(*goast.FuncDecl)
		if !ok || fn.Recv == nil || len(fn.Recv.List) != 1 || fn.Name.Name != "exprNode" {
			continue
		}
		if name := recvTypeName(fn.Recv.List[0].Type); name != "" {
			out[name] = true
		}
	}
	names := make([]string, 0, len(out))
	for n := range out {
		names = append(names, n)
	}
	sort.Strings(names)
	return names
}

func recvTypeName(e goast.Expr) string {
	star, ok := e.(*goast.StarExpr)
	if !ok {
		return ""
	}
	id, ok := star.X.(*goast.Ident)
	if !ok {
		return ""
	}
	return id.Name
}

// An unknown node — one no walker in this package has ever named — is the whole argument for the reflection
// walk, so it gets its own type and its own test rather than a paragraph.
type oddHolder struct {
	Keep Expr   `json:"keep"`
	Drop int    `json:"drop"`
	Deep []Expr `json:"deep"`
}

func (*oddHolder) Span() Span { return Span{} }
func (*oddHolder) exprNode()  {}

// TestTheWalkerIsDrivenFromFieldTypesNotNodeNames pins the mechanism the fix depends on: a node the walk has
// never heard of still gets walked, because the walk asks its FIELDS. `oddHolder` is such a node — no case in
// any walker mentions it, and the read under it is still seen. A non-AST field is none of its business, and
// walking never writes to the node.
func TestTheWalkerIsDrivenFromFieldTypesNotNodeNames(t *testing.T) {
	t.Parallel()
	inner := &Index{Obj: &Name{Value: "xs"}, Idx: &IntLit{Value: 0}}
	h := &oddHolder{Keep: inner, Drop: 3, Deep: []Expr{&IntLit{Value: 1}, inner}}
	var walked []string
	markValueReads(reflect.ValueOf(h), func(n string) { walked = append(walked, n) }, func(Stmt) {})
	joined := strings.Join(walked, ",")
	if !strings.Contains(joined, "xs") {
		t.Fatalf("an unknown node's Expr field was not walked (names seen: %q) — the walk is back to a list of "+
			"node names, which is how four constructs became invisible (roadmap Gap R.289)", joined)
	}
	if h.Drop != 3 {
		t.Fatalf("walking a node wrote to it: Drop = %d, want 3", h.Drop)
	}
}
