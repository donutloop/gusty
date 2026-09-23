package lang

// PropGen is a deterministic property-based whole-program generator. It builds
// random *Program ASTs over the AOT+interpreter *shared* lowering surface and
// renders them back to canonical source via Format, so a property test can run
// the same source through BOTH backends (the tree-walking interpreter and the
// LLVM AOT compiler) and compare stdout. This directly guards the two-backend
// semantics-drift risk the roadmap calls out.
//
// The generator is seeded: PropSource(seed, n) always yields the same n
// programs, so failures/drift reports are reproducible by re-running with the
// seed printed by the test.
//
// Scope discipline keeps every generated program well-formed (no undefined
// references, no conditional/forward bindings):
//   - top-level expressions may read top-level variables (always bound before
//     use, because generation is sequential);
//   - suite bodies (if/for/function) read only their own locals (loop var /
//     params) plus literals.
//
// The generated surface is deliberately constrained to constructs the AOT
// backend lowers cleanly (integer arithmetic, integer comparison conditions,
// list literals, len/abs/min/max/range, for-over-range, functions), because
// the AOT has several real codegen drift bugs (mixed-type conditions, float
// and bool arithmetic, tuple unpack) that the harness surfaces rather than
// hides: see integration/proptest_test.go, where drift is logged per seed.

import (
	"math/rand"
	"sort"
	"strconv"
)

// PropGrammar is the shared-surface grammar the generator samples from. Keeping
// it explicit makes the covered surface auditable.
type PropGrammar struct {
	MaxDepth      int // max expression nesting depth
	MaxStmts      int // max statements per program
	MaxBodyStmts  int // max statements per suite body
	MaxListElems  int // max elements in a collection literal
	MaxFuncParams int // max parameters per function
}

// DefaultPropGrammar returns the standard grammar bounds used by PropSource.
func DefaultPropGrammar() PropGrammar {
	return PropGrammar{
		MaxDepth:      4,
		MaxStmts:      14,
		MaxBodyStmts:  6,
		MaxListElems:  5,
		MaxFuncParams: 3,
	}
}

// propGen carries the PRNG + grammar used to build one program.
type propGen struct {
	r *rand.Rand
	g PropGrammar
	// globals is the set of top-level variable names bound by a direct
	// top-level assign in an earlier statement; top-level expressions may
	// read these. Suite bodies never read globals (scope discipline).
	globals map[string]bool
}

// newPropGen seeds a generator with the given grammar.
func newPropGen(seed int64, g PropGrammar) *propGen {
	return &propGen{r: rand.New(rand.NewSource(seed)), g: g, globals: map[string]bool{}}
}

// PropSource deterministically generates n whole-program sources for a seed.
// Each generated program is rendered canonical gusty source; parse the source
// and run it through both backends to compare parity.
func PropSource(seed int64, n int, g PropGrammar) []string {
	out := make([]string, 0, n)
	gen := newPropGen(seed, g)
	for i := 0; i < n; i++ {
		out = append(out, Format(gen.genProgram()))
	}
	return out
}

// PropPrograms deterministically generates n AST programs for a seed, the same
// corpus PropSource renders. Callers that want to introspect the AST can walk
// these directly.
func PropPrograms(seed int64, n int, g PropGrammar) []*Program {
	out := make([]*Program, 0, n)
	gen := newPropGen(seed, g)
	for i := 0; i < n; i++ {
		out = append(out, gen.genProgram())
	}
	return out
}

// genProgram builds a random top-level program. The global scope is reset each
// call so programs are independent (no names leak across the corpus).
func (g *propGen) genProgram() *Program {
	g.globals = map[string]bool{}
	stmts := make([]Stmt, 0, g.g.MaxStmts)
	for i := 0; i < g.g.MaxStmts; i++ {
		switch r := g.r.Float64(); {
		case r < 0.45:
			stmts = append(stmts, g.genAssign())
		case r < 0.62:
			stmts = append(stmts, g.genExprStmt())
		case r < 0.78:
			stmts = append(stmts, g.genIf())
		case r < 0.9:
			stmts = append(stmts, g.genLoop())
		default:
			stmts = append(stmts, g.genFunc())
		}
	}
	return &Program{Stmts: stmts}
}

// freshName returns a new top-level variable name bound in globals.
func (g *propGen) freshName() string {
	for i := 0; ; i++ {
		n := "v" + strconv.Itoa(i)
		if !g.globals[n] {
			g.globals[n] = true
			return n
		}
	}
}

// genAssign produces `name = <topexpr>`. The RHS is generated before any new
// target name is bound, so the RHS never reads its own just-bound target (no
// self/forward references). Tuple unpack is deliberately NOT generated: the
// AOT backend does not lower tuple expressions.
func (g *propGen) genAssign() Stmt {
	rhs := g.genTopExpr(0)
	return &AssignStmt{Target: &Name{Value: g.freshName()}, Value: rhs}
}

// genExprStmt produces a print call. Bare (non-print) expression statements are
// not reliably lowered by the AOT backend, so every generated expression
// statement is a print.
func (g *propGen) genExprStmt() Stmt {
	n := 1 + g.r.Intn(3)
	args := make([]Expr, 0, n)
	for i := 0; i < n; i++ {
		args = append(args, g.genTopExpr(0))
	}
	return &ExprStmt{Expr: &Call{Fn: &Name{Value: "print"}, Args: args}}
}

// genIf produces `if <cond>:` with an optional else. Conditions are integer
// comparisons: float/bool arithmetic in conditions triggers an AOT codegen
// drift, so conditions stay integer-only.
func (g *propGen) genIf() Stmt {
	s := &IfStmt{Cond: g.genCond(), Then: g.genBody(nil, false)}
	if g.r.Float64() < 0.4 {
		s.Else = g.genBody(nil, false)
	}
	return s
}

// genCond returns an integer-only comparison condition.
func (g *propGen) genCond() Expr {
	op := []string{"<", ">", "<=", ">="}[g.r.Intn(4)]
	return &BinOp{Op: op, L: g.genCondOperand(), R: g.genCondOperand()}
}

// genCondOperand returns a simple integer literal operand for a condition.
func (g *propGen) genCondOperand() Expr {
	return &IntLit{Value: int64(g.r.Intn(10))}
}

// genLoop produces a for loop over range(n), which always terminates. A while
// loop with a non-mutating body would infinite-loop, so only for loops are
// generated.
func (g *propGen) genLoop() Stmt {
	loopVar := "i" + strconv.Itoa(g.r.Intn(1000))
	return &ForStmt{
		Var:  &Name{Value: loopVar},
		Iter: &Call{Fn: &Name{Value: "range"}, Args: []Expr{&IntLit{Value: int64(1 + g.r.Intn(6))}}},
		Body: g.genBody(map[string]bool{loopVar: true}, true),
	}
}

// genBody builds a short suite body (prints, plus break/continue in loops).
// locals holds names bound in the body scope (e.g. the loop var); bodies never
// read globals and never bind new locals, so references stay in-scope.
func (g *propGen) genBody(locals map[string]bool, inLoop bool) []Stmt {
	body := make([]Stmt, 0, g.g.MaxBodyStmts)
	for i := 0; i < g.g.MaxBodyStmts; i++ {
		r := g.r.Float64()
		if r < 0.55 {
			body = append(body, &ExprStmt{Expr: &Call{
				Fn:   &Name{Value: "print"},
				Args: []Expr{g.genLocalExpr(locals, 0)},
			}})
		} else if inLoop {
			if g.r.Float64() < 0.5 {
				body = append(body, &BreakStmt{})
			} else {
				body = append(body, &ContinueStmt{})
			}
		}
	}
	return body
}

// genFunc produces a non-nested function; its body reads only its params.
func (g *propGen) genFunc() Stmt {
	name := "f" + strconv.Itoa(g.r.Intn(100000))
	n := g.r.Intn(g.g.MaxFuncParams + 1)
	params := make([]*Param, 0, n)
	locals := map[string]bool{}
	for i := 0; i < n; i++ {
		p := "p" + strconv.Itoa(i)
		params = append(params, &Param{Name: p})
		locals[p] = true
	}
	body := []Stmt{
		&ExprStmt{Expr: &Call{Fn: &Name{Value: "print"}, Args: []Expr{g.genLocalExpr(locals, 0)}}},
		&ReturnStmt{Expr: g.genLocalExpr(locals, 0)},
	}
	return &FuncDef{Name: name, Params: params, Body: body}
}

// genTopExpr recursively builds a random expression that may read globals.
func (g *propGen) genTopExpr(depth int) Expr {
	if depth >= g.g.MaxDepth {
		return g.genTopAtom()
	}
	switch r := g.r.Float64(); {
	case r < 0.5:
		return g.genTopAtom()
	case r < 0.72:
		op := []string{"+", "-", "*"}[g.r.Intn(3)]
		return &BinOp{Op: op, L: g.genTopAtom(), R: g.genTopAtom()}
	case r < 0.8:
		return &UnOp{Op: "-", X: &IntLit{Value: int64(1 + g.r.Intn(9))}}
	case r < 0.87:
		b := []string{"abs", "min", "max"}[g.r.Intn(3)]
		if b == "abs" {
			return &Call{Fn: &Name{Value: b}, Args: []Expr{g.genTopAtom()}}
		}
		return &Call{Fn: &Name{Value: b}, Args: []Expr{g.genListLit()}}
	case r < 0.94:
		return g.genListLit()
	default:
		return g.genIndex()
	}
}

// genLocalExpr builds a random expression reading only locals + literals.
func (g *propGen) genLocalExpr(locals map[string]bool, depth int) Expr {
	if depth >= g.g.MaxDepth {
		return g.genLocalAtom(locals)
	}
	switch r := g.r.Float64(); {
	case r < 0.6:
		return g.genLocalAtom(locals)
	case r < 0.8:
		op := []string{"+", "-", "*"}[g.r.Intn(3)]
		return &BinOp{Op: op, L: g.genLocalAtom(locals), R: g.genLocalAtom(locals)}
	case r < 0.9:
		return &UnOp{Op: "-", X: &IntLit{Value: int64(1 + g.r.Intn(9))}}
	default:
		return &Call{Fn: &Name{Value: "abs"}, Args: []Expr{g.genLocalAtom(locals)}}
	}
}

// genTopAtom returns an integer literal or a reference to a bound global.
func (g *propGen) genTopAtom() Expr {
	switch r := g.r.Float64(); {
	case r < 0.45:
		return &IntLit{Value: int64(g.r.Intn(10))}
	case r < 0.55:
		// Reference a bound global variable; fall back to a literal if none.
		for _, n := range g.globalNames() {
			return &Name{Value: n}
		}
		return &IntLit{Value: 1}
	default:
		return &Call{Fn: &Name{Value: "len"}, Args: []Expr{g.genListLit()}}
	}
}

// genLocalAtom returns an integer literal or a reference to a local in scope.
func (g *propGen) genLocalAtom(locals map[string]bool) Expr {
	switch r := g.r.Float64(); {
	case r < 0.5:
		return &IntLit{Value: int64(g.r.Intn(10))}
	case r < 0.85:
		// Reference a local name in scope.
		names := make([]string, 0, len(locals))
		for n := range locals {
			names = append(names, n)
		}
		sort.Strings(names)
		if len(names) > 0 {
			return &Name{Value: names[0]}
		}
		return &IntLit{Value: 1}
	default:
		return &Call{Fn: &Name{Value: "len"}, Args: []Expr{g.genListLit()}}
	}
}

// globalNames returns the bound global names in random order.
func (g *propGen) globalNames() []string {
	names := make([]string, 0, len(g.globals))
	for n := range g.globals {
		names = append(names, n)
	}
	// Sort before shuffle: Go map iteration order is random, so without a
	// deterministic ordering the seeded corpus would not be reproducible.
	sort.Strings(names)
	g.r.Shuffle(len(names), func(i, j int) { names[i], names[j] = names[j], names[i] })
	return names
}

// genListLit produces a list literal with a mix of small ints.
func (g *propGen) genListLit() Expr {
	elems := make([]Expr, 0, g.g.MaxListElems)
	for i := 0; i < g.g.MaxListElems; i++ {
		elems = append(elems, &IntLit{Value: int64(g.r.Intn(10))})
	}
	return &ListLit{Elems: elems}
}

// genIndex produces a list literal indexed by a small int expression.
func (g *propGen) genIndex() Expr {
	return &Index{Obj: g.genListLit(), Idx: &IntLit{Value: int64(g.r.Intn(4))}}
}
