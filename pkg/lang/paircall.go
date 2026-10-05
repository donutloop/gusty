package lang

// paircall.go — the (payload, tag) pair crosses a call, in both directions (roadmap L11.1's remaining
// position, Gap R.139, ADR 0273).
//
// A slot of a container the program *built* has a kind only the object can tell (ADR 0241, ADR 0251),
// and the arithmetic that reads it answers a (payload, tag) pair (ADR 0265) so the answer brings its
// own kind. ADR 0267 carried that pair to a binding, ADR 0268 to every position that asks for one
// static number, ADR 0269 to the operators that choose an operand. What stayed outside was a call: the
// parameter has one word, the argument has two, and the callee — where the arithmetic runs — cannot see
// the caller's slot at all. `def twice(v): return v * 2` with `print(twice(xs[0][0]))` is CPython's
// `14` and the interpreter's `14`; the compiled leg spent exit 1 on it naming `xs`'s slots, because the
// argument went down the ordinary numeric road, which wants a payload alone (roadmap Gap R.139).
//
// Two words travel, one each way:
//
//   - **in**: a parameter whose argument the pair road answers arrives as *two* `i32`s — payload and
//     tag — and is bound inside the callee through the one door every other tagged value uses
//     (`bindTaggedVar`, ADR 0187), with the arithmetic origin recorded so the body asks the same door
//     the caller asked (`numericPairVar`, ADR 0267).
//   - **out**: the body's answer already is a pair, and its kind is whatever `@rt_num_arith` said. The
//     callee stores that tag beside its own return — one `internal global i32` per pair-returning
//     function, named off the function's own symbol — and returns the payload in the word it always
//     returned. A pair-aware caller loads the tag immediately after the call, so the answer's kind is
//     the one the objects chose, never one the compiler guessed. `ret` keeps its type: the return
//     convention ADR 0196 / ADR 0254 built is untouched.
//
// The gate is what makes this safe to land. A parameter is pair-carrying only where (a) some call site
// *needs* the pair — the argument is, or reaches through, a container the literal no longer describes —
// and (b) every call site of that parameter can *supply* one from its spelling alone. A function called
// with an argument this pass cannot read (a call, a text, a container, a comprehension, a name with no
// evidence behind it) keeps the convention it always had, and the pair-needing call site keeps the
// honest refusal it already had: landing this door turns refusals into answers, never answers into
// refusals and never a refusal into a wrong number.
//
// The scan is therefore a pure function of the AST, and it runs before any IR exists: the `define` of a
// function written *after* its first call has to agree with that call about the arity, and only a
// state-free decision can be read at both ends.

import (
	"fmt"
	"sort"
	"strings"
)

// pairFnSpec is the compiled backend's per-function decision about the pair at the call boundary.
type pairFnSpec struct {
	// params are the parameter indices that arrive as (payload, tag) — two i32 words.
	params map[int]bool
	// returnsPair says every `return` in the body answers a pair, so the callee stores the answer's tag
	// beside the return and a pair-aware caller reads it back.
	returnsPair bool
	// bindsParams is false where the body cannot hold a tagged parameter at all — a float-returning
	// function (whose parameter slots are `double`s), a function known to hand back a string index, a
	// decorated function. The tag words are still passed, because the arity is the `define`'s and every
	// `call`'s agreement; they are simply not bound, and those shapes keep the refusal the argument road
	// already gave them rather than answer a pair into a word that cannot hold one.
	bindsParams bool
}

// pairTagSlot names the one word a pair-returning function stores its answer's kind in: an
// `internal global i32` on the function's own symbol, so two pair-returning functions cannot read each
// other's answer, and `--emit-llvm` shows the door the way it shows every other runtime helper.
func pairTagSlot(fnName string) string { return irSymbol(fnName) + ".anst" }

// pairTagNone is the tag the fall-off-the-end and unwinding returns hand back: CPython's None. A body
// that can reach the end without a `return` answers None on the pair road too, rather than leaving the
// previous call's tag in the word for the next caller to read.
const pairTagNone = "3"

// pairIntTag is the tag an i32 the ordinary numeric road produced means, the same constant
// arithOperandPair answers for an IntLit.
const pairIntTag = "0"

// pairEmptyBuiltins are the zero-argument calls that build an empty container: `xs = list()` is as
// run-time-built as `xs = []`, and the literal-based scan has to read both as "the slots are unwritten".
var pairEmptyBuiltins = map[string]bool{"list": true, "dict": true, "set": true}

// pairMutators are the container methods that change what a name's slots hold, which is what takes a
// literal out of being a description of them — the same rule `indexKindIsRuntimeObject` asks of a read,
// answered here from the spelling because the scan runs before any emitting state exists.
var pairMutators = map[string]bool{
	"append": true, "extend": true, "insert": true, "pop": true, "remove": true, "clear": true,
	"add": true, "discard": true, "update": true,
}

// pairWalker is the one AST walk the three questions of this scan are asked with: which functions are
// called with what, which names are ever bound to what, and which containers the program built while it
// ran. Any nil callback is simply not asked.
type pairWalker struct {
	call   func(fn string, args []Expr)
	assign func(target, value Expr)
	method func(recv Expr, member string)
	empty  func(target Expr) // `x = []`/`{}`/`set()` and their element-assignment cousins
}

func (w pairWalker) walkExprs(es []Expr) {
	for _, e := range es {
		w.walkExpr(e)
	}
}

func (w pairWalker) walkExpr(e Expr) {
	switch n := e.(type) {
	case nil:
	case *Call:
		if nm, ok := n.Fn.(*Name); ok && w.call != nil {
			w.call(nm.Value, n.Args)
		}
		if at, ok := n.Fn.(*Attr); ok && at.Name != nil && w.method != nil {
			w.method(at.Obj, at.Name.Value)
		}
		w.walkExpr(n.Fn)
		for _, a := range n.Args {
			if kw, ok := a.(*KeywordArg); ok {
				w.walkExpr(kw.Value)
				continue
			}
			w.walkExpr(a)
		}
	case *Name, *IntLit, *FloatLit, *BoolLit, *NoneLit, *StrLit:
	case *FString:
		for _, p := range n.Parts {
			w.walkExpr(p.Expr)
		}
	case *Tuple:
		w.walkExprs(n.Elems)
	case *ListLit:
		w.walkExprs(n.Elems)
	case *SetLit:
		w.walkExprs(n.Elems)
	case *DictLit:
		w.walkExprs(n.Keys)
		w.walkExprs(n.Vals)
	case *BinOp:
		w.walkExpr(n.L)
		w.walkExpr(n.R)
	case *UnOp:
		w.walkExpr(n.X)
	case *CondExpr:
		w.walkExpr(n.Cond)
		w.walkExpr(n.If)
		w.walkExpr(n.Else)
	case *AssignExpr:
		if w.assign != nil && n.Name != nil {
			w.assign(n.Name, n.Value)
		}
		w.walkExpr(n.Value)
	case *Index:
		w.walkExpr(n.Obj)
		w.walkExpr(n.Idx)
	case *Slice:
		w.walkExpr(n.Obj)
		w.walkExpr(n.Low)
		w.walkExpr(n.High)
		w.walkExpr(n.Step)
	case *Attr:
		w.walkExpr(n.Obj)
	case *Lambda:
		for _, p := range n.Params {
			w.walkExpr(p.Default)
		}
		w.walkExpr(n.Body)
	case *Comp:
		w.walkExprs(n.Elems)
		w.walkExprs(n.Keys)
		w.walkExprs(n.Vals)
		w.walkExpr(n.Iter)
		w.walkExpr(n.Cond)
	case *Generator:
		w.walkExprs(n.Elems)
		w.walkExpr(n.Iter)
		w.walkExpr(n.Cond)
	case *AwaitExpr:
		w.walkExpr(n.Expr)
	}
}

func (w pairWalker) walkStmts(ss []Stmt) {
	for _, s := range ss {
		switch n := s.(type) {
		case nil:
		case *ExprStmt:
			w.walkExpr(n.Expr)
		case *AssignStmt:
			if w.assign != nil {
				w.assign(n.Target, n.Value)
			}
			if w.empty != nil {
				if _, isIndex := n.Target.(*Index); isIndex {
					w.empty(n.Target) // `xs[0] = v` writes a slot the literal did not describe
				} else if isEmptyContainerLit(n.Value) {
					w.empty(n.Target)
				}
			}
			w.walkExpr(n.Target)
			w.walkExpr(n.Value)
		case *AugAssignStmt:
			if w.assign != nil {
				w.assign(n.Target, nil) // the answer depends on the previous value: unprovable
			}
			w.walkExpr(n.Target)
			w.walkExpr(n.Value)
		case *ReturnStmt:
			w.walkExpr(n.Expr)
		case *RaiseStmt:
			w.walkExpr(n.Expr)
		case *IfStmt:
			w.walkExpr(n.Cond)
			w.walkStmts(n.Then)
			for _, e := range n.Elifs {
				w.walkStmts([]Stmt{e})
			}
			w.walkStmts(n.Else)
		case *WhileStmt:
			w.walkExpr(n.Cond)
			w.walkStmts(n.Body)
			w.walkStmts(n.Else)
		case *ForStmt:
			if w.assign != nil {
				// A loop variable is bound to an element of something whose kind the loop does not name, so
				// it is recorded as unprovable rather than as a read: `for x in xs` over a container the
				// program built is answered by ADR 0185's loop-element tag, not by the pair door, and
				// claiming otherwise would route `mix(s, x)` — a call the ordinary road already answers —
				// through a tagged parameter it never needed (found by the `function_calls` benchmark the
				// day this file landed).
				w.assign(n.Var, nil)
			}
			w.walkExpr(n.Iter)
			w.walkStmts(n.Body)
			w.walkStmts(n.Else)
		case *MatchStmt:
			w.walkExpr(n.Subject)
			for _, c := range n.Cases {
				w.walkExpr(c.Guard)
				w.walkStmts(c.Body)
			}
		case *TryStmt:
			w.walkStmts(n.Body)
			for _, e := range n.Excepts {
				w.walkStmts(e.Body)
			}
			w.walkStmts(n.Finally)
		case *WithStmt:
			w.walkExpr(n.Expr)
			w.walkStmts(n.Body)
		case *FuncDef:
			for _, d := range n.Decorators {
				w.walkExpr(d)
			}
			for _, p := range n.Params {
				w.walkExpr(p.Default)
			}
			w.walkStmts(n.Body)
		case *ClassDef:
			w.walkStmts(n.Body)
		case *YieldStmt:
			w.walkExpr(n.Expr)
		case *YieldFromStmt:
			w.walkExpr(n.Expr)
		}
	}
}

// isEmptyContainerLit is `[]`, `{}`, `set()`/`list()`/`dict()` — a container the program will fill as
// it runs, which is the only way a name's slots stop being described by the literal that made it.
func isEmptyContainerLit(e Expr) bool {
	switch n := e.(type) {
	case *ListLit:
		return len(n.Elems) == 0
	case *SetLit:
		return len(n.Elems) == 0
	case *DictLit:
		return len(n.Keys) == 0
	case *Call:
		nm, ok := n.Fn.(*Name)
		return ok && pairEmptyBuiltins[nm.Value] && len(n.Args) == 0
	}
	return false
}

// pairScan is one program's answers to the three questions, computed once per Compile.
type pairScan struct {
	built  map[string]bool           // names whose containers the program built as it ran
	bound  map[string][]Expr         // every value a name is ever bound to
	calls  map[string]map[int][]Expr // per function, per parameter position, every argument written
	fds    map[string]*FuncDef       // the functions in play
	paired map[string]bool           // names the pair road binds (fixed point, below)
}

// pairCallSpecs is the program-wide decision: which functions take the pair across their call boundary.
func pairCallSpecs(prog *Program) map[string]*pairFnSpec {
	if prog == nil {
		return nil
	}
	fds := map[string]*FuncDef{}
	for _, st := range prog.Stmts {
		fd, ok := st.(*FuncDef)
		if !ok || fd.Name == "" || len(fd.Decorators) > 0 || fd.Async {
			continue
		}
		// A body that defines a closure, yields, or is decorated is emitted under conventions that own
		// its parameter words already (ADR 0199's env, ADR 0208's decorator, ADR 0212's generator list).
		if len(nestedDefs(fd.Body)) > 0 || containsYield(fd.Body) {
			continue
		}
		fds[fd.Name] = fd
	}
	if len(fds) == 0 {
		return nil
	}
	s := &pairScan{
		built: map[string]bool{},
		bound: map[string][]Expr{},
		calls: map[string]map[int][]Expr{},
		fds:   fds,
	}
	mark := func(e Expr) {
		if nm, ok := e.(*Name); ok {
			s.built[nm.Value] = true
		}
	}
	w := pairWalker{
		call: func(fn string, args []Expr) { s.recordCall(fn, args) },
		assign: func(target, value Expr) {
			if nm, ok := target.(*Name); ok {
				s.bound[nm.Value] = append(s.bound[nm.Value], value)
			}
		},
		method: func(recv Expr, member string) {
			if pairMutators[member] {
				mark(recv)
			}
		},
		empty: func(target Expr) {
			if target == nil {
				return
			}
			if ix, ok := target.(*Index); ok {
				mark(ix.Obj) // `xs[0] = v` writes a slot the literal did not describe
				return
			}
			mark(target)
		},
	}
	w.walkStmts(prog.Stmts)

	// Which names the pair road binds is settled with the rest: `n = xs[0][0] * 2` makes `n` a number
	// the compiler cannot see, and handing it to a function is the same missing word one operator
	// further out (Gap R.146's argument position). The rule is its own fixed point, so it is iterated a
	// bounded number of rounds — deeper than any program in the corpus chains a binding — rather than
	// to convergence, which a `n = n + 1` would never reach.
	s.paired = map[string]bool{}
	for round := 0; round < 4; round++ {
		next := map[string]bool{}
		for name, vals := range s.bound {
			if s.anyNeedsWord(vals) && s.argsNumberish(vals) {
				next[name] = true
			}
		}
		s.paired = next
	}

	specs := map[string]*pairFnSpec{}
	for fnName, byIndex := range s.calls {
		fd := fds[fnName]
		if fd == nil {
			continue
		}
		needs := map[int]bool{}
		for _, i := range pairIndexList(byIndex) {
			if i >= len(fd.Params) {
				continue
			}
			args := append([]Expr{}, byIndex[i]...)
			if d := fd.Params[i].Default; d != nil {
				args = append(args, d)
			}
			if s.anyNeedsWord(args) {
				needs[i] = true
			}
		}
		if len(needs) == 0 {
			continue
		}
		// The supply half: every call site of every marked parameter must be readable from its own
		// spelling. One argument this pass cannot name closes the parameter, and with it the function's
		// answer direction — that program keeps the road, and the refusal, it has always had.
		supplies := true
		for i := range needs {
			if !s.argsNumberish(byIndex[i]) {
				supplies = false
				break
			}
		}
		if !supplies {
			continue
		}
		spec := specs[fnName]
		if spec == nil {
			spec = &pairFnSpec{params: map[int]bool{}, bindsParams: true}
			specs[fnName] = spec
		}
		for i := range needs {
			spec.params[i] = true
		}
		// The body half of the gate: a parameter is only worth tagging if the body can read it back.
		// A use the pair doors do not serve keeps the whole function on its ordinary convention, which
		// is the road — and the refusal — the program has always had.
		if !pairUsesServed(fd, spec) {
			for i := range needs {
				delete(spec.params, i)
			}
		}
	}
	for fnName, spec := range specs {
		fd := fds[fnName]
		// A self-call would have the inner frame write the tag word the outer caller has not read yet;
		// such a function keeps its ordinary single-word answer and the pair-needing caller its refusal.
		if fd == nil || containsCallToFn(fnName, fd.Body) || !pairBodyAnswers(fd, spec) {
			continue
		}
		spec.returnsPair = true
	}
	if len(specs) == 0 {
		return nil
	}
	return specs
}

// recordCall files the arguments one call site hands one function, by parameter position: positional
// arguments in order, keyword arguments on the position they name.
func (s *pairScan) recordCall(fn string, args []Expr) {
	fd := s.fds[fn]
	if fd == nil {
		return
	}
	byIndex := s.calls[fn]
	if byIndex == nil {
		byIndex = map[int][]Expr{}
		s.calls[fn] = byIndex
	}
	pos := 0
	for _, a := range args {
		if kw, ok := a.(*KeywordArg); ok {
			for i, p := range fd.Params {
				if p.Name == kw.Name {
					byIndex[i] = append(byIndex[i], kw.Value)
				}
			}
			continue
		}
		if pos < len(fd.Params) {
			byIndex[pos] = append(byIndex[pos], a)
		}
		pos++
	}
}

// pairIndexList orders the recorded parameter indices so the scan answers the same way twice.
func pairIndexList(byIndex map[int][]Expr) []int {
	out := make([]int, 0, len(byIndex))
	for i := range byIndex {
		out = append(out, i)
	}
	sort.Ints(out)
	return out
}

// anyNeedsWord is the scan's copy of `arithWouldRefuse`: would the ordinary numeric road have refused
// one of these expressions? Nothing else is re-routed, which is what lets the door land without moving
// a program that already answered.
func (s *pairScan) anyNeedsWord(es []Expr) bool {
	for _, e := range es {
		if s.exprNeedsWord(e) {
			return true
		}
	}
	return false
}

func (s *pairScan) exprNeedsWord(e Expr) bool {
	switch n := e.(type) {
	case *Name:
		return s.paired[n.Value]
	case *Index:
		nm, depth := chainDepth(e)
		return depth > 0 && nm != nil && s.built[nm.Value]
	case *BinOp:
		switch n.Op {
		case "+", "-", "*":
			return s.exprNeedsWord(n.L) || s.exprNeedsWord(n.R)
		}
	case *UnOp:
		if n.Op == "-" {
			return s.exprNeedsWord(n.X)
		}
	}
	return false
}

// argsNumberish is the supply half: can the pair road produce both words for every one of these from
// the spelling alone? A name counts only when it is bound somewhere and every binding is itself
// numberish — evidence, not a guess — and `seen` keeps a self-referential binding from spinning.
func (s *pairScan) argsNumberish(es []Expr) bool {
	for _, e := range es {
		if !s.exprNumberish(e, map[string]bool{}) {
			return false
		}
	}
	return true
}

func (s *pairScan) exprNumberish(e Expr, seen map[string]bool) bool {
	switch n := e.(type) {
	case *IntLit, *FloatLit, *BoolLit, *NoneLit:
		return true
	case *Name:
		if s.paired[n.Value] {
			return true
		}
		if seen[n.Value] {
			return true // in progress: the fixed point says nothing worse than "not proven"
		}
		vals, ok := s.bound[n.Value]
		if !ok || len(vals) == 0 {
			return false
		}
		seen[n.Value] = true
		defer delete(seen, n.Value)
		for _, v := range vals {
			if !s.exprNumberish(v, seen) {
				return false
			}
		}
		return true
	case *UnOp:
		return n.Op == "-" && s.exprNumberish(n.X, seen)
	case *BinOp:
		switch n.Op {
		case "+", "-", "*", "/", "//", "%", "**":
			return s.exprNumberish(n.L, seen) && s.exprNumberish(n.R, seen)
		}
	case *Index:
		nm, depth := chainDepth(e)
		return depth > 0 && nm != nil && s.built[nm.Value]
	}
	return false
}

// containsCallToFn reports whether the body calls the named function anywhere — the recursion a single
// tag word beside the return cannot describe.
func containsCallToFn(fn string, body []Stmt) bool {
	found := false
	w := pairWalker{call: func(callee string, _ []Expr) {
		if callee == fn {
			found = true
		}
	}}
	w.walkStmts(body)
	return found
}

// pairBodyAnswers reports whether every `return` in the body answers a pair over the pair-carrying
// parameters: the shape where the callee, and not the caller, knows the answer's kind.
func pairBodyAnswers(fd *FuncDef, spec *pairFnSpec) bool {
	pairParams := map[string]bool{}
	for i, p := range fd.Params {
		if spec.params[i] {
			pairParams[p.Name] = true
		}
	}
	if len(pairParams) == 0 {
		return false
	}
	answers, ok := false, true
	var walk func(ss []Stmt)
	check := func(e Expr) {
		mentions, shaped := pairAnswerShape(e, pairParams)
		if !shaped || !mentions {
			ok = false
		} else {
			answers = true
		}
	}
	walk = func(ss []Stmt) {
		for _, s := range ss {
			if !ok {
				return
			}
			switch n := s.(type) {
			case nil:
			case *ReturnStmt:
				if n.Expr == nil {
					ok = false // a bare `return` answers None, which is not a pair this door serves
					return
				}
				check(n.Expr)
			case *IfStmt:
				walk(n.Then)
				for _, e := range n.Elifs {
					walk([]Stmt{e})
				}
				walk(n.Else)
			case *WhileStmt:
				walk(n.Body)
				walk(n.Else)
			case *ForStmt:
				walk(n.Body)
				walk(n.Else)
			case *TryStmt:
				walk(n.Body)
				for _, e := range n.Excepts {
					walk(e.Body)
				}
				walk(n.Finally)
			case *WithStmt:
				walk(n.Body)
			case *MatchStmt:
				for _, c := range n.Cases {
					walk(c.Body)
				}
			case *FuncDef:
				walk(n.Body)
			case *ClassDef:
				walk(n.Body)
			}
		}
	}
	walk(fd.Body)
	return ok && answers
}

// pairAnswerShape asks one expression two questions: does it *mention* a pair-carrying parameter, and
// is everything it mentions answerable by the pair door (a pair parameter, a number literal, or `+`,
// `-`, `*` over those). The other operators are not here because they have their own doors already
// (ADR 0253's `/`, ADR 0264's `//`/`%**`), and a second answer to the same question is how three
// truthiness tables happened.
func pairAnswerShape(e Expr, pairParams map[string]bool) (mentions, shaped bool) {
	switch n := e.(type) {
	case *Name:
		return pairParams[n.Value], pairParams[n.Value]
	case *IntLit, *FloatLit, *BoolLit:
		return false, true
	case *UnOp:
		if n.Op != "-" {
			return false, false
		}
		m, sh := pairAnswerShape(n.X, pairParams)
		return m, sh && m
	case *BinOp:
		switch n.Op {
		case "+", "-", "*":
			lm, ls := pairAnswerShape(n.L, pairParams)
			rm, rs := pairAnswerShape(n.R, pairParams)
			return lm || rm, ls && rs
		}
	}
	return false, false
}

// ---- the emitting side: the two doors that move the pair over the boundary ----------------------

// pairArgWords turns one argument of a pair-carrying parameter into the two words the callee asks
// about. The pair road answers whenever the expression's kind lives in an object; where the ordinary
// numeric road already knows the answer — an index of a container the literal still describes, say — the
// tag is the constant that road's own kind is. An argument whose kind neither road can say is refused in
// words rather than passed as an int wearing another object's bits (ADR 0265's rule, one position
// further out).
func (g *irGen) pairArgWords(b *strings.Builder, fnName string, idx int, a Expr) (payload, tag string, handled bool, err error) {
	spec := g.pairSpecs[fnName]
	if spec == nil || !spec.params[idx] {
		return "", "", false, nil
	}
	// An argument that is itself arithmetic over a slot — `twice(xs[0][0] + 1)` — is the pair the one
	// arithmetic door already answers for the print position (ADR 0265) and for a binding (ADR 0267).
	// Asking that door rather than a second one is what keeps `print(twice(xs[0][0] + 1))` and
	// `print(xs[0][0] + 1)` from ever answering two different kinds of the same expression.
	if p, t, ok, perr := g.taggedArithPair(b, a); perr != nil {
		return "", "", true, perr
	} else if ok {
		return p, t, true, nil
	}
	if p, t, ok, perr := g.arithOperandPair(b, a); perr != nil {
		return "", "", true, perr
	} else if ok {
		return p, t, true, nil
	}
	if g.isFloat(a) {
		return "", "", true, fmt.Errorf("codegen: %s is a float the pair road cannot box, and parameter %d of %s arrives as a (payload, tag) pair whose payload is an i32 (roadmap L11.1, ADR 0273)", exprSurface(a), idx, fnName)
	}
	if isStringExpr(a) || g.isNoneExpr(a) || g.isContainerExpr(a) {
		return "", "", true, fmt.Errorf("codegen: %s is not a number, and parameter %d of %s carries the (payload, tag) pair the arithmetic door answers: this pass will not hand over a payload wearing another kind's bits (roadmap L11.1, ADR 0273)", exprSurface(a), idx, fnName)
	}
	v, err := g.value(b, a)
	if err != nil {
		return "", "", true, err
	}
	return v, pairIntTag, true, nil
}

// pairCallPair is the caller's door: it emits a call to a function whose answer is a pair and hands back
// the payload the call returned with the tag the callee stored beside its return. It answers ok=false
// for anything that is not such a call, which leaves the caller on the road it has always taken,
// refusal included: this is an extra answer, not a substitute for one.
func (g *irGen) pairCallPair(b *strings.Builder, e Expr) (payload, tag string, ok bool, err error) {
	c, isCall := e.(*Call)
	if !isCall {
		return "", "", false, nil
	}
	nm, isName := c.Fn.(*Name)
	if !isName || !g.pairRetDone[nm.Value] {
		return "", "", false, nil
	}
	// The ordinary numeric road refuses a call whose answer is a pair (it would use the payload as
	// though it were the whole value), so this is the road such a call is emitted on. The permission is
	// this call's alone: an argument of it that is itself a pair-returning call is lowered with the
	// permission off and keeps its own refusal.
	g.pairCallAsked[nm.Value] = true
	v, err := g.value(b, c)
	delete(g.pairCallAsked, nm.Value)
	if err != nil {
		return "", "", false, err
	}
	t := g.newTmp()
	fmt.Fprintf(b, "  %s = load i32, i32* @%s\n", t, pairTagSlot(nm.Value))
	return v, t, true, nil
}

// pairCallPrint prints the answer of a pair-returning call through the one printer that takes a value
// *and* its kind — the same door ADR 0265's slot arithmetic and ADR 0269's chosen operands go through,
// so a call's answer renders the way a slot's answer does and `print(twice(xs[0][0]))` is `14` rather
// than the payload alone.
func (g *irGen) pairCallPrint(b *strings.Builder, e Expr) (bool, error) {
	p, t, ok, err := g.pairCallPair(b, e)
	if err != nil || !ok {
		return false, err
	}
	g.heapUsed = true
	g.floatFmtUsed = true
	b.WriteString(fmt.Sprintf("  call void @rt_print_mixed_value(i32 %s, i32 %s, i32 0)\n", p, t))
	return true, nil
}

// bindPairCallResult binds a name to the answer of a pair-returning call: the pair the answer arrived
// in, through the one binding door (ADR 0187), with the arithmetic origin recorded so every position
// that later reads the name asks the door that knows the kind (ADR 0267, ADR 0268).
func (g *irGen) bindPairCallResult(b *strings.Builder, name string, e Expr) (bool, error) {
	p, t, ok, err := g.pairCallPair(b, e)
	if err != nil {
		return false, err
	}
	if !ok {
		return false, nil
	}
	g.bindTaggedVar(b, name, p, t)
	if g.taggedOrigin == nil {
		g.taggedOrigin = map[string]string{}
	}
	g.taggedOrigin[name] = taggedOriginArith
	// The payload may be a float box, which is a heap object: the slot that holds it is a root for as
	// long as the name holds it (ADR 0181's rule for every handle-carrying store; the store itself is
	// the binding door's, so only the registration is emitted here).
	g.gcReg(b, name)
	return true, nil
}

// pairReturnWords is the callee's side of the answer direction: the pair the body's arithmetic answered,
// and the word its tag travels in. A body the scan judged answerable and the door then declines is the
// scan's mistake, not the program's — and the refusal below is what keeps that from being a load of
// whatever the tag word happened to hold.
func (g *irGen) pairReturnWords(b *strings.Builder, e Expr) (payload, tag string, err error) {
	if p, t, ok, perr := g.taggedArithPair(b, e); perr != nil {
		return "", "", perr
	} else if ok {
		return p, t, nil
	}
	if nm, isName := e.(*Name); isName && g.numericPairVar(nm.Value) {
		p, t := g.numericPairRegs(b, nm.Value)
		return p, t, nil
	}
	return "", "", fmt.Errorf("codegen: %q returns %s, which the pair road cannot answer: the parameter's kind arrived from the caller and this body's answer has no tag to travel with (roadmap L11.1, ADR 0273)", g.curFunc, exprSurface(e))
}

// pairSpecFor is the emitting side's question — what does this function's call boundary carry — answered
// of the scan, and narrowed by the conventions the body is emitted under. The arity half of the decision
// stays the scan's (see the file header); only the binding half is narrowed here, so a float-returning
// body never receives a tag word it has nowhere to put.
func (g *irGen) pairSpecFor(fd *FuncDef, floatRet, strRet bool) *pairFnSpec {
	if g.pairSpecs == nil || fd == nil {
		return nil
	}
	spec := g.pairSpecs[fd.Name]
	if spec == nil {
		return nil
	}
	if floatRet || strRet || len(fd.Decorators) > 0 || g.decorated[fd.Name] {
		// A float-returning function's parameters are `double`s and a string-returning one's answer is an
		// @str_tab index: neither can hold the pair, so the body keeps the convention it has. The tag
		// words are still passed, because every `call` reads the scan's arity.
		clone := *spec
		clone.bindsParams = false
		clone.returnsPair = false
		return &clone
	}
	return spec
}

// pairParamReg names the tag word of pair-carrying parameter i, beside its payload %p<i>.
func pairParamReg(i int) string { return fmt.Sprintf("%%q%d", i) }

// pairTagGlobalIR is the module's description of the answer direction: one word per function that hands
// back a pair, so the emitted module carries the door rather than implying it.
func pairTagGlobalIR(fnName string) string {
	return fmt.Sprintf("@%s = internal global i32 0\n", pairTagSlot(fnName))
}

// bindPairParams binds the pair-carrying parameters of a body — the payload word and the tag word the
// caller passed — through the one door every tagged value is bound by, and hands back the undo, because
// the tag records are keyed by name and the next function's parameter of the same name must not read
// this body's binding.
func (g *irGen) bindPairParams(b *strings.Builder, fd *FuncDef, spec *pairFnSpec) func() {
	if spec == nil || !spec.bindsParams {
		return func() {}
	}
	savedVars := map[string]bool{}
	savedOrigin := map[string]string{}
	restore := func() {
		for _, p := range fd.Params {
			if was, ok := savedVars[p.Name]; ok {
				if was {
					g.taggedVars[p.Name] = true
				} else {
					delete(g.taggedVars, p.Name)
				}
			}
			if was, ok := savedOrigin[p.Name]; ok {
				g.taggedOrigin[p.Name] = was
			} else {
				delete(g.taggedOrigin, p.Name)
			}
		}
	}
	for i, p := range fd.Params {
		if !spec.params[i] {
			continue
		}
		savedVars[p.Name] = g.taggedVars[p.Name]
		if g.taggedOrigin != nil {
			savedOrigin[p.Name] = g.taggedOrigin[p.Name]
		}
		g.bindTaggedVar(b, p.Name, fmt.Sprintf("%%p%d", i), pairParamReg(i))
		if g.taggedOrigin == nil {
			g.taggedOrigin = map[string]string{}
		}
		g.taggedOrigin[p.Name] = taggedOriginArith
		// The payload may be a float box, which is a heap object, and the body may collect while it
		// holds the parameter: the slot is a root for the frame's life, exactly as an argument copied
		// into `%_param<i>` is (ADR 0181). An int payload is skipped by the collector's own range test.
		g.gcReg(b, p.Name)
		// The incoming register is the payload only: the body reads the pair through its slots, or every
		// read keeps answering the argument however many times the body assigned (Gap R.3's rule).
		if g.paramSlot == nil {
			g.paramSlot = map[string]bool{}
		}
		g.paramSlot[p.Name] = true
		if g.floatVars != nil {
			delete(g.floatVars, p.Name)
		}
	}
	return restore
}

// storePairTag records the answer's kind beside the answer itself, for a function whose answer travels
// as a pair. For any other function it emits nothing at all — the module carries the word only where a
// call site reads it back (ADR 0273).
func (g *irGen) storePairTag(b *strings.Builder, tag string) {
	if !g.pairRetDone[g.curFunc] {
		return
	}
	fmt.Fprintf(b, "  store i32 %s, i32* @%s\n", tag, pairTagSlot(g.curFunc))
}

// pairArityTag answers the question a call site asks for a parameter it cannot put on the pair road:
// does this call need a tag word at all? A function whose body is emitted under a convention that owns
// its parameter words — a float-returning one, whose parameters are `double`s — still has the arity the
// scan gave it, because the `define` reads the same scan; its body simply never reads the word.
func (g *irGen) pairArityTag(fnName string, idx int) string {
	spec := g.pairSpecs[fnName]
	if spec == nil || !spec.params[idx] {
		return ""
	}
	return pairIntTag
}

// ---- the body half of the gate: can this function *consume* a tagged parameter? ----------------
//
// Marking a parameter pair-carrying binds it tagged, and from that moment every use of the name in the
// body asks the pair doors. The doors serve arithmetic, an ordering, a condition, a print, `str`, and a
// binding of any of those (ADR 0265, ADR 0267, ADR 0268, ADR 0269); they do not serve an index, a
// method call, a container literal, a comprehension, a `%`, a `**`. So the scan asks the body before it
// asks the caller: an unreadable use of the parameter keeps the whole function on the convention it has
// always had. The rule is default-deny — the shape that got the `function_calls` benchmark was a body
// doing `(a * 31 + b * 17) % 100003`, where `%` asks for one i32 and the tag is nowhere in sight.

// pairUsesServed reports whether every mention of a pair-carrying parameter in the body sits in a
// position the pair doors answer.
func pairUsesServed(fd *FuncDef, spec *pairFnSpec) bool {
	pp := map[string]bool{}
	for i, p := range fd.Params {
		if spec.params[i] {
			pp[p.Name] = true
		}
	}
	if len(pp) == 0 {
		return true
	}
	served := true
	check := func(e Expr) {
		if !pairExprServed(e, pp) {
			served = false
		}
	}
	var walk func(ss []Stmt)
	walk = func(ss []Stmt) {
		for _, s := range ss {
			if !served {
				return
			}
			switch n := s.(type) {
			case nil:
			case *ExprStmt:
				check(n.Expr)
			case *AssignStmt:
				check(n.Value)
				check(n.Target)
			case *AugAssignStmt:
				check(n.Value)
				check(n.Target)
			case *ReturnStmt:
				check(n.Expr)
			case *RaiseStmt:
				check(n.Expr)
			case *IfStmt:
				check(n.Cond)
				for _, e := range n.Elifs {
					walk([]Stmt{e})
				}
				walk(n.Then)
				walk(n.Else)
			case *WhileStmt:
				check(n.Cond)
				walk(n.Body)
				walk(n.Else)
			case *ForStmt:
				check(n.Iter)
				check(n.Var)
				walk(n.Body)
				walk(n.Else)
			case *MatchStmt:
				check(n.Subject) // a pattern over a value whose kind only the tag says is not served
				for _, c := range n.Cases {
					check(c.Guard)
					walk(c.Body)
				}
			case *TryStmt:
				walk(n.Body)
				for _, e := range n.Excepts {
					walk(e.Body)
				}
				walk(n.Finally)
			case *WithStmt:
				check(n.Expr) // a context manager is a protocol, and the pair road speaks none of it
				walk(n.Body)
			case *FuncDef:
				served = false // unreachable: an eligible body has no nested `def` (pairCallSpecs)
			case *ClassDef:
				served = false
			case *YieldStmt:
				check(n.Expr)
			case *YieldFromStmt:
				check(n.Expr)
			}
		}
	}
	walk(fd.Body)
	return served
}

// pairExprServed answers one question about an expression that mentions a pair-carrying parameter: is
// it built only out of positions the pair doors answer? An expression that mentions none of them is the
// body's own business and is answered true without looking.
func pairExprServed(e Expr, pp map[string]bool) bool {
	if !pairMentions(e, pp) {
		return true
	}
	switch n := e.(type) {
	case *Name:
		return pp[n.Value]
	case *IntLit, *FloatLit, *BoolLit:
		return true
	case *UnOp:
		return n.Op == "-" && pairExprServed(n.X, pp)
	case *BinOp:
		switch n.Op {
		case "+", "-", "*", "<", "<=", ">", ">=", "and", "or":
			return pairOperandServed(n.L, pp) && pairOperandServed(n.R, pp)
		}
	case *Call:
		// print and str are the two positions a pair is rendered from (ADR 0268's renderer road and the
		// tag-reading printer); anything else is a position that keeps one word for the value.
		nm, ok := n.Fn.(*Name)
		if !ok {
			return false
		}
		switch nm.Value {
		case "print", "printf", "str":
			for _, a := range n.Args {
				if kw, isKw := a.(*KeywordArg); isKw {
					if !pairExprServed(kw.Value, pp) {
						return false
					}
					continue
				}
				if !pairExprServed(a, pp) {
					return false
				}
			}
			return true
		}
	}
	return false
}

// pairOperandServed answers one operand of an expression that mentions a pair-carrying parameter. An
// operand that mentions none is the ordinary road's own business, but only where that road is a number:
// `v * 2` is served, while `"v" + v` is the concatenation road's question and `[v]` the container road's,
// and neither has a word for a tag. Such a body keeps the convention it has always had — which is the
// road, and the refusal, the program had before this door.
func pairOperandServed(e Expr, pp map[string]bool) bool {
	if !pairMentions(e, pp) {
		switch e.(type) {
		case *IntLit, *FloatLit, *BoolLit:
			return true
		}
		return false
	}
	return pairExprServed(e, pp)
}

// pairMentions asks whether an expression reads one of the named parameters anywhere inside it.
func pairMentions(e Expr, pp map[string]bool) bool {
	found := false
	var seen func(x Expr)
	inner := func(es ...Expr) {
		for _, x := range es {
			seen(x)
			if found {
				return
			}
		}
	}
	seen = func(x Expr) {
		if found {
			return
		}
		switch n := x.(type) {
		case nil:
		case *Name:
			found = pp[n.Value]
		case *IntLit, *FloatLit, *BoolLit, *NoneLit, *StrLit:
		case *FString:
			for _, p := range n.Parts {
				seen(p.Expr)
			}
		case *Tuple:
			inner(n.Elems...)
		case *ListLit:
			inner(n.Elems...)
		case *SetLit:
			inner(n.Elems...)
		case *DictLit:
			inner(n.Keys...)
			inner(n.Vals...)
		case *BinOp:
			seen(n.L)
			seen(n.R)
		case *UnOp:
			seen(n.X)
		case *CondExpr:
			seen(n.Cond)
			seen(n.If)
			seen(n.Else)
		case *AssignExpr:
			seen(n.Value)
		case *Index:
			seen(n.Obj)
			seen(n.Idx)
		case *Slice:
			seen(n.Obj)
			seen(n.Low)
			seen(n.High)
			seen(n.Step)
		case *Attr:
			seen(n.Obj)
		case *Lambda:
			seen(n.Body)
		case *Comp:
			inner(n.Elems...)
			inner(n.Keys...)
			inner(n.Vals...)
			seen(n.Iter)
			seen(n.Cond)
		case *Generator:
			inner(n.Elems...)
			seen(n.Iter)
			seen(n.Cond)
		case *AwaitExpr:
			seen(n.Expr)
		case *Call:
			seen(n.Fn)
			for _, a := range n.Args {
				if kw, isKw := a.(*KeywordArg); isKw {
					seen(kw.Value)
					continue
				}
				seen(a)
			}
		}
	}
	seen(e)
	return found
}
