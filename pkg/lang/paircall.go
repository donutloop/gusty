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
	// wants is the same set as the scan first marked it, kept so the body half can be re-asked while the
	// answer half is still settling (see pairCallSpecs' rounds).
	wants map[int]bool
	// intParams names the parameters the call sites hand nothing but integers — a literal, or a name the
	// scan can see is numberish. The body may read those as plain numbers, which is what lets
	// `def area(w, h): return w * h` keep `h` on the ordinary road while `w` carries the tag: the shared
	// arithmetic door needs a kind for *both* operands, and this is the evidence that says what `h` is
	// (roadmap L11.6, Gap P.1's `area(2.5, 2)`).
	intParams map[string]bool
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
	// enterFn/leaveFn bracket a `def`'s body, which is how a call site learns *whose* parameter it is
	// handing over (Gap R.161). The signature's defaults are evaluated outside that scope, so they are
	// walked before the callback fires.
	enterFn func(name string)
	leaveFn func()
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
			if w.enterFn != nil {
				w.enterFn(n.Name)
			}
			w.walkStmts(n.Body)
			if w.leaveFn != nil {
				w.leaveFn()
			}
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

// pairForward is one call site handing an *enclosing function's own parameter* to a callee position:
// `def outer(x): return twice(x)` forwards outer's `x` onto twice's `v`. The record is the missing half
// of ADR 0273's supply question — a parameter is written by no assignment, so `s.bound` never sees it, and
// the only evidence that it can carry a double is the argument the caller's own caller wrote.
type pairForward struct {
	callee string
	pos    int
	caller string
	idx    int
}

// pairSupport is one mark's reason for existing: the other parameter whose mark it rests on.
type pairSupport struct {
	fn  string
	idx int
}

// pairScan is one program's answers to the three questions, computed once per Compile.
type pairScan struct {
	built   map[string]bool             // names whose containers the program built as it ran
	bound   map[string][]Expr           // every value a name is ever bound to
	calls   map[string]map[int][]Expr   // per function, per parameter position, every argument written
	callers map[string]map[int][]string // the function each recorded argument was written inside (aligned)
	fds     map[string]*FuncDef         // the functions in play
	paired  map[string]bool             // names the pair road binds (fixed point, below)
	fwd     []pairForward               // parameter-to-parameter forwarding edges, in walk order
	// readsCallAnswers is the return side's permission to ask "what does this callee hand back?".
	// ADR 0276's argument gate asks the *same* function a different question — "can the pair road produce
	// both words for this argument from its spelling alone?" — and a call spelled `make()` cannot, which is
	// what closes a parameter for a program like `print(twice(make()))` and keeps the refusal that ADR
	// 0273 pinned (roadmap Gap R.164, ADR 0280). One predicate, two questions; the flag says which is being
	// asked, so widening the answer for the return side cannot silently widen the gate for arguments.
	readsCallAnswers bool
}

// pairCallSpecs is the program-wide decision: which functions take the pair across their call boundary.
// pairClosedOut carries the closed-position set out of the one scan a Compile runs. It is written, never
// read, by every caller but codegen's, which reads it immediately after asking for the specs.
var pairClosedOut map[string]map[int]bool

func pairCallSpecs(prog *Program) map[string]*pairFnSpec {
	pairClosedOut = nil
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
		built:   map[string]bool{},
		bound:   map[string][]Expr{},
		calls:   map[string]map[int][]Expr{},
		callers: map[string]map[int][]string{},
		fds:     fds,
	}
	// The function whose body the walk is inside, so a call site knows whose parameter it is handing
	// over. A stack, because a `def` nests; a body whose enclosing name is not a top-level function (a
	// method, a closure) forwards nothing that this scan can act on, and recordForward says so.
	stack := []string{}
	curFn := func() string {
		if len(stack) == 0 {
			return ""
		}
		return stack[len(stack)-1]
	}
	mark := func(e Expr) {
		if nm, ok := e.(*Name); ok {
			s.built[nm.Value] = true
		}
	}
	w := pairWalker{
		call: func(fn string, args []Expr) {
			s.recordCall(curFn(), fn, args)
			s.recordForward(curFn(), fn, args)
		},
		enterFn: func(name string) { stack = append(stack, name) },
		leaveFn: func() { stack = stack[:len(stack)-1] },
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

	// Pass one: what each parameter position is handed where the program can see it written — its own call
	// sites, and its default. Direct evidence only; the call graph is pass two's question.
	needsOf := map[string]map[int]bool{}
	ownedByReturnRoad := map[string]bool{}
	directOf := map[string]map[int]bool{}
	// justified records *why* a mark that no call site of its own asked for was made: which other parameter's
	// mark it rests on. A mark with no direct evidence and no live support is a pair nobody will ever read,
	// and a caller whose body the rounds closed would otherwise hand a one-word argument to a two-word
	// parameter — the truncation this file exists to remove, arriving back through the call graph.
	justified := map[string]map[int][]pairSupport{}
	for fnName, byIndex := range s.calls {
		fd := fds[fnName]
		if fd == nil {
			continue
		}
		needs := map[int]bool{}
		// Every parameter position the program writes an argument to — at a call site or in the
		// signature. A default *is* an argument the caller did not have to write: `def greet(name,
		// times=1.5)` called as `greet("a")` hands `times` the double the call site never mentions, and
		// a position only a default describes is the one an arity-only scan would miss (roadmap L11.6,
		// Gap P.1).
		present := map[int]bool{}
		for i := range pairIndexList(byIndex) {
			present[i] = true
		}
		for i := range fd.Params {
			if fd.Params[i].Default != nil {
				present[i] = true
			}
		}
		for _, i := range sortedPairPositions(present) {
			if i >= len(fd.Params) {
				continue
			}
			args := append([]Expr{}, byIndex[i]...)
			if d := fd.Params[i].Default; d != nil {
				args = append(args, d)
			}
			// The ordinary numeric road refuses the first kind outright and answers the second with an
			// i32 — which is the wrong number rather than a refusal — so both are the pair road's
			// business (roadmap L11.6, Gap P.1: `twice(2.5)` printed 4 at exit 0).
			if s.anyNeedsWord(args) || s.anyCarriesDouble(args) {
				needs[i] = true
			}
		}
		// A body emitted under a convention that already owns its return word owns its parameter words
		// with it: ADR 0274's float return hands back a name the body bound to a double, and ADR 0174's
		// string return hands back a text. Neither has a word a pair's tag could live beside, so the
		// whole function keeps the convention — and the answer or refusal — it had before this door
		// (answering a pair into a word that cannot hold one is Gap P.1's wrong number). A function that
		// owns its return word also forwards nothing: its parameter never reaches a pair door.
		if pairReturnRoadOwns(fd, s) {
			ownedByReturnRoad[fnName] = true
			continue
		}
		if len(needs) > 0 {
			needsOf[fnName] = needs
			directOf[fnName] = map[int]bool{}
			for i := range needs {
				directOf[fnName][i] = true
			}
		}
	}
	// Pass two: the evidence a parameter can only get from the call graph (Gap R.161).
	// `def twice(v): return v * 2` / `def outer(x): return twice(x)` / `print(outer(2.5))` marks nothing in
	// pass one: twice's own call site writes `x`, a Name no assignment ever recorded a binding for, because
	// a parameter is written by the *caller* — s.bound is the body's story, and the body never assigns x.
	// outer's x is proven from outer's call sites (pass one, the FloatLit 2.5), and the edge carries that
	// proof across the boundary in whichever direction it is missing:
	//
	//   - caller → callee, because a caller that holds a pair-bound name hands one to this callee position,
	//     and a one-word parameter would read the payload and print 4 for 5.0 (the row itself);
	//   - callee → caller, because a callee whose position the callee's *other* call sites proved (a second
	//     `twice(2.5)` written elsewhere) asks every caller to supply the tag, including this forwarding one.
	//
	// Bounded rounds — the chain is as deep as the program's calls nest — and the marking only grows in this
	// pass, so the fixed point is reachable.
	for round := 0; round < 8; round++ {
		changed := false
		mark := func(fn string, idx int, by pairSupport) {
			if fds[fn] == nil {
				return
			}
			m := needsOf[fn]
			if m == nil {
				m = map[int]bool{}
				needsOf[fn] = m
			}
			if !m[idx] {
				m[idx] = true
				changed = true
			}
			j := justified[fn]
			if j == nil {
				j = map[int][]pairSupport{}
				justified[fn] = j
			}
			j[idx] = append(j[idx], by)
		}
		for _, f := range s.fwd {
			if ownedByReturnRoad[f.callee] || ownedByReturnRoad[f.caller] {
				continue
			}
			if cn := needsOf[f.callee]; cn != nil && cn[f.pos] {
				// The callee reads a pair here, so the name the caller hands must arrive as one.
				mark(f.caller, f.idx, pairSupport{fn: f.callee, idx: f.pos})
			}
			if co := needsOf[f.caller]; co != nil && co[f.idx] {
				// The caller holds a pair here, so the position it is handed to must carry it.
				mark(f.callee, f.pos, pairSupport{fn: f.caller, idx: f.idx})
			}
		}
		if !changed {
			break
		}
	}

	specs := map[string]*pairFnSpec{}
	// closed records the (function, parameter) positions the scan considered for the pair and then
	// declined, because one call site wrote them something the pass cannot name. It is not the same
	// statement as "this function never needed the pair": a caller holding a double and handing it to a
	// closed position has no road left that reads its kind, and the ordinary road truncates the double to
	// its int word — `y = twice(x)` / `z = twice(y)` / `return z` printed `8` for CPython's `10.0` at exit
	// 0 this way (roadmap Gap R.164, ADR 0280). codegen asks this set before it hands a float down.
	closed := map[string]map[int]bool{}
	pairClosedOut = closed
	for fnName, needs := range needsOf {
		fd := fds[fnName]
		if fd == nil || len(needs) == 0 || ownedByReturnRoad[fnName] {
			continue
		}
		byIndex := s.calls[fnName]
		// The supply half: every call site of every marked parameter must be readable from its own
		// spelling. One argument this pass cannot name closes the parameter, and with it the function's
		// answer direction — that program keeps the road, and the refusal, it has always had.
		supplies := true
		for i := range needs {
			args := append([]Expr{}, byIndex[i]...)
			callers := s.callers[fnName][i]
			if d := fd.Params[i].Default; d != nil {
				args = append(args, d)
				callers = append(callers, "")
			}
			for k, a := range args {
				if s.argIsMarkedParam(callers, k, a, needsOf) {
					continue // the caller hands a name it bound as a pair itself: read it back as one
				}
				if !s.exprNumberish(a, map[string]bool{}) {
					if closed == nil {
						closed = map[string]map[int]bool{}
					}
					if closed[fnName] == nil {
						closed[fnName] = map[int]bool{}
					}
					for j := range needs {
						closed[fnName][j] = true
					}
					supplies = false
					break
				}
			}
			if !supplies {
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
		spec.wants = map[int]bool{}
		for i := range needs {
			spec.wants[i] = true
		}
		// The parameters the pair does not carry, and what the body may therefore read them as. Asked
		// before the body half, because that half is the question `def area(w, h): return w * h` called
		// with `area(2.5, 2)` turns on: `w` needs the tag and `h` needs only to be provably the integer
		// its call sites write.
		spec.intParams = s.knownIntParams(fd, spec, s.calls[fnName])
	}
	// The body half of the gate, settled together with the answer half, in rounds. A parameter is only
	// worth tagging if the body can read it back, and whether a body can read `return g(x)` back depends
	// on whether g hands its own answer back as a pair — which in turn depends on g's body being served.
	// Both questions only ever remove things (a parameter, a callee), so the marking shrinks
	// monotonically and a bounded number of rounds reaches the answer; nothing here grows a pair road the
	// call sites did not ask for. A self-call is out — the inner frame would write the tag word the outer
	// caller has not read yet — so it keeps its ordinary single-word answer, and the pair-needing caller
	// keeps its refusal (ADR 0273).
	// A parameter proven only by pass two's edge is kept only while the callee position it forwards into is
	// still marked. The rounds below can close a callee — its body may not be served, its answer may not
	// come back as a pair — and a caller left carrying a tag its callee never reads would hand the ordinary
	// road a pair-bound name, which is the wrong number this whole file exists to avoid. Removal only, so the
	// outer loop terminates; the rounds are re-run after a removal, because a closed callee can close its
	// caller's body in turn.
	var answerable map[string]bool
	for settle := 0; settle < 4; settle++ {
		answerable = settlePairAnswer(fds, specs, closed, s)
		if !pruneUnsuppliedForwards(specs, directOf, justified) {
			break
		}
	}
	for fnName, spec := range specs {
		if len(spec.params) == 0 {
			delete(specs, fnName)
			continue
		}
		spec.returnsPair = answerable[fnName]
	}
	if len(specs) == 0 {
		return nil
	}
	return specs
}

// settlePairAnswer runs the body half and the answer half together, in rounds, and reports which functions
// hand their answer back as a pair. A parameter is only worth tagging if the body can read it back, and
// whether a body can read `return g(x)` back depends on whether g hands its own answer back as a pair —
// which in turn depends on g's body being served. Both questions only ever remove things (a parameter, a
// callee), so the marking shrinks monotonically and a bounded number of rounds reaches the answer; nothing
// here grows a pair road the call sites did not ask for. A self-call is out — the inner frame would write
// the tag word the outer caller has not read yet — so it keeps its ordinary single-word answer, and the
// pair-needing caller keeps its refusal (ADR 0273).
func settlePairAnswer(fds map[string]*FuncDef, specs map[string]*pairFnSpec, closedForServe map[string]map[int]bool, sc *pairScan) map[string]bool {
	answerable := map[string]bool{}
	for round := 0; round < 4; round++ {
		for fnName, spec := range specs {
			fd := fds[fnName]
			if fd == nil {
				continue
			}
			// Re-asked from the marking rather than narrowed from the last round: a body whose
			// `return g(x)` is served only once g is answerable must not be closed down by the round
			// that ran before g got there.
			spec.params = map[int]bool{}
			for i := range spec.wants {
				spec.params[i] = true
			}
			if len(spec.params) > 0 && !pairUsesServed(fd, spec, answerable) {
				// A function marked on paper whose body the rounds then declined to serve leaves its
				// callers with no road at all: the emission falls back to the one-word convention, and a
				// caller holding a double truncates it to the int half. `y = twice(x)` / `z = twice(y)` /
				// `return z` printed `8` for CPython's `10.0` at exit 0 by exactly this route — `outer` was
				// marked, its callee closed under it, and nothing told the call site (roadmap Gap R.164,
				// ADR 0280). Recording the positions here is what turns that digit into a refusal.
				//
				// The recording is limited to the body this was measured on: one that holds a pair at all,
				// by binding the answer of a pair-carrying user call. The second draft recorded every
				// not-served body, which is the ordinary fate of most functions, and it took back answers
				// that work today — `def f(x): x = x + 1.5; return int(x)` and `def cmpf(v): return v > 1.5`
				// both printed their reference answer before and refused after, which the ladder forbids.
				if holdsPairFromPairCall(fd, sc) {
					if closedForServe[fnName] == nil {
						closedForServe[fnName] = map[int]bool{}
					}
					for i := range spec.wants {
						closedForServe[fnName][i] = true
					}
				}
				spec.params = map[int]bool{}
			}
		}
		next := map[string]bool{}
		for fnName, spec := range specs {
			fd := fds[fnName]
			if fd == nil || len(spec.params) == 0 || containsCallToFn(fnName, fd.Body) {
				continue
			}
			if pairBodyAnswers(fd, spec, answerable) {
				next[fnName] = true
			}
		}
		stable := len(next) == len(answerable)
		if stable {
			for fn := range next {
				if !answerable[fn] {
					stable = false
					break
				}
			}
		}
		answerable = next
		if stable {
			break
		}
	}
	return answerable
}

// pruneUnsuppliedForwards closes a parameter whose only evidence was a forwarding edge that is no longer
// live, and reports whether it closed anything: the callee position it was justified by has itself been
// closed (its body was not served, its answer does not come back as a pair, its own caller was closed), and
// a mark left standing on a dead edge is a tag word no reader exists for. A parameter with direct evidence —
// its own call sites or its default — is nobody else's business and stays.
func pruneUnsuppliedForwards(specs map[string]*pairFnSpec, directOf map[string]map[int]bool, justified map[string]map[int][]pairSupport) bool {
	var supported func(fn string, idx int, depth int) bool
	supported = func(fn string, idx int, depth int) bool {
		if depth > 5 {
			return false // a forwarding cycle proves nothing: close it, and let the road it needs refuse
		}
		if directOf[fn][idx] {
			return true
		}
		caller := specs[fn]
		if caller == nil || !caller.wants[idx] {
			return false
		}
		for _, sup := range justified[fn][idx] {
			callee := specs[sup.fn]
			if callee != nil && callee.wants[sup.idx] && supported(sup.fn, sup.idx, depth+1) {
				return true
			}
		}
		return false
	}
	changed := false
	for fnName, spec := range specs {
		for i := range spec.wants {
			if !supported(fnName, i, 0) {
				delete(spec.wants, i)
				changed = true
			}
		}
		if len(spec.wants) == 0 {
			delete(specs, fnName)
			continue
		}
		// Only ever shrink: the body half owns `params`, and re-deriving it from `wants` here would hand
		// back the tag word to a function whose body the rounds just closed.
		for i := range spec.params {
			if !spec.wants[i] {
				delete(spec.params, i)
			}
		}
	}
	return changed
}

// argIsMarkedParam is the supply half's call-graph case: the argument is a name the *enclosing* function
// received as its own parameter, and that parameter is marked — so at this call site the name is bound as a
// pair by bindPairParams and the pair road reads it, even though no assignment in the program ever recorded
// a binding for it (a parameter is written by the caller, not by the body). Without this, `def outer(x):
// return twice(x)` proves twice's parameter and then refuses to hand it the argument that proved it.
func (s *pairScan) argIsMarkedParam(callers []string, k int, e Expr, needsOf map[string]map[int]bool) bool {
	if k >= len(callers) {
		return false
	}
	nm, ok := e.(*Name)
	if !ok || callers[k] == "" {
		return false
	}
	cf := s.fds[callers[k]]
	if cf == nil || needsOf[callers[k]] == nil {
		return false
	}
	rebound := map[string][]Expr{}
	scanRebinds(cf.Body, rebound)
	for i, p := range cf.Params {
		if p.Name == nm.Value {
			return rebound[nm.Value] == nil && needsOf[callers[k]][i]
		}
	}
	return false
}

// recordForward files the call sites that hand an enclosing function's own parameter to a callee. The name
// must still be the parameter at that point: a body that rebinds it (`x = read_line()`) has replaced the
// argument the caller wrote, and the edge would be a story about a value the callee never sees.
func (s *pairScan) recordForward(caller, callee string, args []Expr) {
	if caller == "" || callee == "" || caller == callee {
		return
	}
	cf, cfd := s.fds[caller], s.fds[callee]
	if cf == nil || cfd == nil {
		return
	}
	rebound := map[string][]Expr{}
	scanRebinds(cf.Body, rebound)
	handed := func(name string) bool {
		for _, p := range cf.Params {
			if p.Name == name {
				return rebound[name] == nil
			}
		}
		return false
	}
	paramOf := func(name string) int {
		for i, p := range cf.Params {
			if p.Name == name {
				return i
			}
		}
		return -1
	}
	pos := 0
	for _, a := range args {
		v, target := a, pos
		if kw, ok := a.(*KeywordArg); ok {
			v = kw.Value
			target = -1
			for i, p := range cfd.Params {
				if p.Name == kw.Name {
					target = i
				}
			}
		} else if pos >= len(cfd.Params) {
			pos++
			continue
		}
		pos++
		if target < 0 || target >= len(cfd.Params) {
			continue
		}
		nm, ok := v.(*Name)
		if !ok || !handed(nm.Value) {
			continue
		}
		s.fwd = append(s.fwd, pairForward{callee: callee, pos: target, caller: caller, idx: paramOf(nm.Value)})
	}
}

// recordCall files the arguments one call site hands one function, by parameter position: positional
// arguments in order, keyword arguments on the position they name, each beside the function it was written
// inside (whose parameters are the only ones the pair road can read back at that site).
func (s *pairScan) recordCall(caller, fn string, args []Expr) {
	fd := s.fds[fn]
	if fd == nil {
		return
	}
	byIndex := s.calls[fn]
	if byIndex == nil {
		byIndex = map[int][]Expr{}
		s.calls[fn] = byIndex
	}
	byCaller := s.callers[fn]
	if byCaller == nil {
		byCaller = map[int][]string{}
		s.callers[fn] = byCaller
	}
	pos := 0
	for _, a := range args {
		if kw, ok := a.(*KeywordArg); ok {
			for i, p := range fd.Params {
				if p.Name == kw.Name {
					byIndex[i] = append(byIndex[i], kw.Value)
					byCaller[i] = append(byCaller[i], caller)
				}
			}
			continue
		}
		if pos < len(fd.Params) {
			byIndex[pos] = append(byIndex[pos], a)
			byCaller[pos] = append(byCaller[pos], caller)
		}
		pos++
	}
}

// pairReturnRoadOwns reports whether the body is one the float- or string-return road emits: a `return`
// of a name the body bound to a double (ADR 0274's promotion, which writes a `double` return word), or a
// `return` of a text, including one built by str()/repr(). It is asked conservatively — a body that only
// *might* take one of those roads keeps its old convention — because the pair road is an extra answer,
// never a replacement for a road that already worked.
// pairParamSeen names a function's own parameters to the numberish question, as *proven* numbers rather
// than as unproven names. A parameter is written by the CALLER, so it is bound by no assignment in this
// body and the binding table has no entry for it — the same documented hole Gap R.161 records for a float
// forwarded across two frames. Here it is not a forwarding question but a return-word one: `y = x + 1` /
// `return y` reads `y`'s leaf `x`, the answer comes back "not a number", the ordinary return road claims
// the body, the parameter is emitted one-word, and the double the door computed is truncated at the `ret` —
// `y = x + 1` printed `1` for CPython's `1.1`, `y = x` printed `0` for `0.1`, `y = x * 2` printed `0` for
// `0.2`, `y = x - 1` printed `-1` for `-0.9`, each at exit 0 (roadmap L11.6, Gap R.169, ADR 0285).
//
// The mark is scoped to the one function being asked, so a differently-typed name in another body keeps
// its own answer: `seen` is read as "in progress, nothing worse than proven", which is the weaker of the
// two answers a fixed point can give and cannot invent a number for a name that has none.
// bindsNameToPairArithmeticAnswer reports that a returned name's value is arithmetic reaching a
// parameter of its own function through the body's bindings — the two-bindings-deep case Gap R.169
// measured (`y = x + 1` / `z = y * 2` / `return z`), where the leaf the return convention reads is a plain
// local and the evidence for its kind is two assignments away.
func bindsNameToPairArithmeticAnswer(fd *FuncDef, val Expr) bool {
	if fd == nil || val == nil {
		return false
	}
	params := map[string]bool{}
	for _, p := range fd.Params {
		params[p.Name] = true
	}
	bindings := map[string][]Expr{}
	scanRebinds(fd.Body, bindings)
	var reaches func(Expr, map[string]bool) bool
	reaches = func(e Expr, through map[string]bool) bool {
		switch n := e.(type) {
		case *Name:
			if params[n.Value] {
				return true
			}
			if through[n.Value] {
				return false
			}
			through[n.Value] = true
			for _, v := range bindings[n.Value] {
				if reaches(v, through) {
					return true
				}
			}
			return false
		case *BinOp:
			switch n.Op {
			case "+", "-", "*", "/", "//", "%":
			default:
				return false
			}
			return reaches(n.L, through) || reaches(n.R, through)
		case *UnOp:
			return n.Op == "-" && reaches(n.X, through)
		}
		return false
	}
	return reaches(val, map[string]bool{})
}

func pairParamSeen(fd *FuncDef) map[string]bool {
	seen := map[string]bool{}
	if fd == nil {
		return seen
	}
	for _, p := range fd.Params {
		seen[p.Name] = true
	}
	return seen
}

func pairReturnRoadOwns(fd *FuncDef, s *pairScan) bool {
	// The return side may ask what a callee hands back (see pairScan.readsCallAnswers); the argument gate
	// below in this file may not, and the two asks are the only ones that differ.
	was := s.readsCallAnswers
	s.readsCallAnswers = true
	defer func() { s.readsCallAnswers = was }()
	returned := []Expr{}
	var walk func(ss []Stmt)
	handled := false
	declines := false
	walk = func(ss []Stmt) {
		for _, st := range ss {
			switch n := st.(type) {
			case nil:
			case *ReturnStmt:
				if n.Expr == nil {
					continue
				}
				if pairTextReturnExpr(n.Expr) {
					handled = true
					return
				}
				// A body that returns a name it bound to a *call's* answer is not the ordinary road's
				// either. The float-return promotion (ADR 0274) and the string index (ADR 0174) are the two
				// shapes this predicate exists to catch; a third slipped in beside them: `y = twice(x)` /
				// `z = twice(y)` / `return z`, where the leaf is a plain local, the leaf check below cannot
				// read the call that produced it, the body was left to the one-word road, and `print(outer(2.5))`
				// answered `8` for CPython's `10.0` at exit 0 — the silent truncation this file exists to
				// refuse instead (roadmap Gap R.164, ADR 0280). Declining here hands the body to the pair road,
				// which answers it or refuses it, both of which are honest.
				if nm, isName := n.Expr.(*Name); isName && bindsNameToPairCallAnswer(fd, s, nm.Value) {
					declines = true
					return
				}
				returned = append(returned, n.Expr)
			case *IfStmt:
				walk(n.Then)
				walk(n.Else)
				for _, e := range n.Elifs {
					walk([]Stmt{e})
				}
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
			}
			if handled {
				return
			}
		}
	}
	walk(fd.Body)
	if declines {
		return false
	}
	if handled {
		return true
	}
	bindings := map[string][]Expr{}
	scanRebinds(fd.Body, bindings)
	for _, e := range returned {
		// The same leaves ADR 0274's promotion reads the return through (params.go's
		// namedNumericLeaves): `return -x` of a name the body rebound to a double is the float-return
		// road's own shape, and that road hands back a `double` word a pair's tag has no place beside.
		for _, name := range namedNumericLeaves(e) {
			for _, v := range bindings[name] {
				// Only the kinds the pair road speaks are the pair road's business. `s = str(v)` /
				// `return s` is ADR 0174's string index and a container is the container road's answer:
				// carrying a pair over those printed a handle as a number, which is Gap P.1's wrong
				// number one return further in.
				if v == nil || !s.exprNumberish(v, pairParamSeen(fd)) {
					// A name the body bound from a parameter is a number the spelling cannot show — `z`
					// in `y = x + 1` / `z = y * 2` reads its value through `y`, which reads it through
					// the argument — and the ordinary return road answering it one word truncates the
					// double the door computed: that program printed `3` for CPython's `3.0` at exit 0
					// (roadmap Gap R.169, ADR 0285). Decline, and let the pair road answer or refuse.
					if bindsNameToPairArithmeticAnswer(fd, v) {
						// `false` here is the predicate's own "the ordinary road does not own this
						// return" answer — the pair road takes the body and answers it or refuses it.
						return false
					}
					return true
				}
				if s.exprCarriesDouble(v) {
					return true
				}
			}
		}
	}
	return false
}

// holdsPairFromPairCall reports whether a body holds a pair at all: some name it binds to the answer of a
// user-defined call whose own answer the pair road speaks. That is the evidence that the function was
// marked *because of* what its callee carries, and so that closing it strands a caller holding a double —
// as opposed to the hundred bodies the settle rounds decline for ordinary reasons (roadmap Gap R.164, ADR 0280).
func holdsPairFromPairCall(fd *FuncDef, s *pairScan) bool {
	if fd == nil {
		return false
	}
	names := map[string]bool{}
	var collect func(ss []Stmt)
	collect = func(ss []Stmt) {
		for _, st := range ss {
			switch n := st.(type) {
			case nil:
			case *AssignStmt:
				nm, isName := n.Target.(*Name)
				c, isCall := n.Value.(*Call)
				if !isName || !isCall {
					continue
				}
				cf, isFn := c.Fn.(*Name)
				if !isFn {
					continue
				}
				if _, isUser := s.fds[cf.Value]; isUser {
					names[nm.Value] = true
				}
			case *IfStmt:
				collect(n.Then)
				collect(n.Else)
				for _, e := range n.Elifs {
					collect([]Stmt{e})
				}
			case *WhileStmt:
				collect(n.Body)
				collect(n.Else)
			case *ForStmt:
				collect(n.Body)
				collect(n.Else)
			case *TryStmt:
				collect(n.Body)
				for _, e := range n.Excepts {
					collect(e.Body)
				}
				collect(n.Finally)
			case *WithStmt:
				collect(n.Body)
			case *MatchStmt:
				for _, cs := range n.Cases {
					collect(cs.Body)
				}
			}
		}
	}
	collect(fd.Body)
	if len(names) == 0 {
		return false
	}
	// The returned expression must read one of those names: a body that binds such a name and returns
	// something else never leaned on the pair, so nothing is stranded by closing it.
	leans := false
	var walkRet func(ss []Stmt)
	walkRet = func(ss []Stmt) {
		for _, st := range ss {
			switch n := st.(type) {
			case nil:
			case *ReturnStmt:
				if n.Expr == nil {
					continue
				}
				for _, leaf := range namedNumericLeaves(n.Expr) {
					if names[leaf] {
						leans = true
					}
				}
			case *IfStmt:
				walkRet(n.Then)
				walkRet(n.Else)
				for _, e := range n.Elifs {
					walkRet([]Stmt{e})
				}
			case *WhileStmt:
				walkRet(n.Body)
				walkRet(n.Else)
			case *ForStmt:
				walkRet(n.Body)
				walkRet(n.Else)
			case *TryStmt:
				walkRet(n.Body)
				for _, e := range n.Excepts {
					walkRet(e.Body)
				}
				walkRet(n.Finally)
			case *WithStmt:
				walkRet(n.Body)
			case *MatchStmt:
				for _, cs := range n.Cases {
					walkRet(cs.Body)
				}
			}
		}
	}
	walkRet(fd.Body)
	return leans
}

// bindsNameToPairCallAnswer reports whether a body assigns a name the answer of a call whose OWN answer
// the pair road speaks — `y = twice(x)` where `twice` carries the pair, not merely any call.
//
// The narrowing is the whole point of the second draft. The first asked only "was this name bound to a
// call", and it is true of `x = int(s)` / `return x`, of `x = round(v)` / `return x`, and of a comparison
// the body binds and hands back: three families that answer today, on ADR 0274's promoted-float road and
// ADR 0174's string road. Taking those bodies away from the road that prints them turns a working answer
// into a refusal, which the ladder forbids far more loudly than Gap R.164's missing digit. Asking whether
// the callee itself carries a pair keeps the rule on the shape it was measured on.
func bindsNameToPairCallAnswer(fd *FuncDef, s *pairScan, name string) bool {
	found := false
	var walk func(ss []Stmt)
	walk = func(ss []Stmt) {
		for _, st := range ss {
			if found {
				return
			}
			switch n := st.(type) {
			case nil:
			case *AssignStmt:
				nm, isName := n.Target.(*Name)
				if !isName || nm.Value != name {
					continue
				}
				c, isCall := n.Value.(*Call)
				if !isCall {
					continue
				}
				// The callee must be a user-defined function: a builtin's answer (`int(s)`, `round(v)`,
				// `len(xs)`) is the road that already prints it, and none of those travel as a pair.
				cf, isFn := c.Fn.(*Name)
				if !isFn {
					continue
				}
				if _, isUser := s.fds[cf.Value]; !isUser {
					continue
				}
				found = true
				return
			case *IfStmt:
				walk(n.Then)
				walk(n.Else)
				for _, e := range n.Elifs {
					walk([]Stmt{e})
				}
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
				for _, cs := range n.Cases {
					walk(cs.Body)
				}
			}
		}
	}
	walk(fd.Body)
	return found
}

// pairTextReturnExpr is the string-return road's shape: a text, or the call that builds one.
func pairTextReturnExpr(e Expr) bool {
	switch n := e.(type) {
	case *StrLit:
		return true
	case *Call:
		if nm, isName := n.Fn.(*Name); isName {
			switch nm.Value {
			case "str", "repr", "ascii", "f", "format":
				return true
			}
		}
	}
	return false
}

// sortedPairPositions orders a set of parameter positions so the scan answers the same way twice.
func sortedPairPositions(present map[int]bool) []int {
	out := make([]int, 0, len(present))
	for i := range present {
		out = append(out, i)
	}
	sort.Ints(out)
	return out
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
	case *Call:
		// Only the return side asks this question (pairScan.readsCallAnswers): `y = twice(x)` / `return y`
		// is numberish when the callee's own returns are, because the pair road speaks the kind the callee
		// stored beside its return. Answering "no" here is what handed such a body to ADR 0274's float-return
		// road and ADR 0174's string index, which is how Gap R.164's `y = twice(x); return y` lost its tag one
		// statement before the return. A builtin keeps the answer this question has always given it: unknown,
		// which is a refusal to speculate; and a callee that returns a text, a container or nothing is
		// numberish neither.
		if !s.readsCallAnswers {
			return false
		}
		nm, isName := n.Fn.(*Name)
		if !isName {
			return false
		}
		cfd, known := s.fds[nm.Value]
		calleeKey := "fn:" + nm.Value
		if !known || cfd == nil || seen[calleeKey] {
			return false
		}
		seen[calleeKey] = true
		defer delete(seen, calleeKey)
		// The callee's own parameters are its caller's evidence, not a binding this scan can read:
		// `def twice(v): return v * 2` answers a number whose kind arrives in the tag, and asking
		// `s.bound` about `v` would say "nothing recorded", which is a wrong answer rather than no
		// answer. The `seen` seed is `exprNumberish`'s existing "say nothing worse than unproven"
		// path, applied to a parameter the first time it is met.
		for _, prm := range cfd.Params {
			if seen[prm.Name] {
				continue
			}
			seen[prm.Name] = true
			defer delete(seen, prm.Name)
		}
		returns := 0
		answers := true
		var walkReturns func(ss []Stmt)
		walkReturns = func(ss []Stmt) {
			for _, st := range ss {
				if !answers {
					return
				}
				switch n := st.(type) {
				case nil:
				case *ReturnStmt:
					if n.Expr == nil || pairTextReturnExpr(n.Expr) || !s.exprNumberish(n.Expr, seen) {
						answers = false
						return
					}
					returns++
				case *IfStmt:
					walkReturns(n.Then)
					for _, e := range n.Elifs {
						walkReturns([]Stmt{e})
					}
					walkReturns(n.Else)
				case *WhileStmt:
					walkReturns(n.Body)
					walkReturns(n.Else)
				case *ForStmt:
					walkReturns(n.Body)
					walkReturns(n.Else)
				case *TryStmt:
					walkReturns(n.Body)
					for _, e := range n.Excepts {
						walkReturns(e.Body)
					}
					walkReturns(n.Finally)
				case *FuncDef:
					walkReturns(n.Body) // a nested `def`'s returns belong to that frame, not to this one
				}
			}
		}
		walkReturns(cfd.Body)
		return answers && returns > 0
	case *Index:
		nm, depth := chainDepth(e)
		if nm == nil || depth == 0 {
			return false
		}
		if s.built[nm.Value] {
			return true
		}
		// A slot of a container the literal describes is numberish when the literal's elements are:
		// `ys = [1, 2.5]` / `twice(ys[1])` reaches the door ADR 0265's slot arithmetic answers, and the
		// pair is what travels. One text or container among the elements closes it, because that slot is
		// not a number to box (roadmap L11.6, Gap P.1).
		for _, v := range s.bound[nm.Value] {
			if ll, isList := v.(*ListLit); isList && s.litElementsAreNumberish(ll.Elems, seen) {
				return true
			}
		}
		return false
	}
	return false
}

// anyCarriesDouble is the scan's copy of the question the argument road asks: could evaluating this
// expression end in a value whose kind the pair has to carry because it is a double? The ordinary numeric
// road answers those with an i32, which truncates — Gap P.1's `twice(2.5)` printing 4 at exit 0 — so they
// are exactly the arguments this door must own rather than the one that refuses.
func (s *pairScan) anyCarriesDouble(es []Expr) bool {
	return s.anyCarriesDoubleSeen(es, map[string]bool{})
}

func (s *pairScan) anyCarriesDoubleSeen(es []Expr, seen map[string]bool) bool {
	for _, e := range es {
		if s.exprCarriesDoubleSeen(e, seen) {
			return true
		}
	}
	return false
}

// exprCarriesDouble asks whether evaluating an expression can end on a value the pair has to carry
// because it is a double. `seen` walks a name's own bindings once: `x = x + 1.5` is a binding that reads
// the name it writes, and the answer for it is settled by the round the fixed point is in, not by
// following it forever (the same rule exprNumberish answers with).
func (s *pairScan) exprCarriesDouble(e Expr) bool {
	return s.exprCarriesDoubleSeen(e, map[string]bool{})
}

func (s *pairScan) exprCarriesDoubleSeen(e Expr, seen map[string]bool) bool {
	switch n := e.(type) {
	case *FloatLit:
		return true
	case *BinOp:
		switch n.Op {
		case "/":
			return true // true division answers a double whatever the operands were
		case "+", "-", "*":
			return s.anyCarriesDoubleSeen([]Expr{n.L, n.R}, seen)
		}
	case *UnOp:
		return (n.Op == "-" || n.Op == "+") && s.exprCarriesDoubleSeen(n.X, seen)
	case *Name:
		// The name's own bindings are the evidence: `x = 8` then `x = 2.5` is a variable the pair road
		// binds (ADR 0274), and handing it to a function is the same missing word one position further
		// out. A parameter of an enclosing function is bound by no assignment and so is not here: a
		// float forwarded through two function boundaries stays roadmap Gap R.161.
		if seen[n.Value] {
			return false // in progress: the fixed point says nothing worse than "not proven"
		}
		seen[n.Value] = true
		defer delete(seen, n.Value)
		for _, v := range s.bound[n.Value] {
			if v != nil && s.exprCarriesDoubleSeen(v, seen) {
				return true
			}
		}
		return false
	case *Index:
		nm, depth := chainDepth(e)
		if nm == nil || depth == 0 {
			return false
		}
		for _, v := range s.bound[nm.Value] {
			if v != nil && s.litCarriesDouble(v) {
				return true
			}
		}
	}
	return false
}

// argsAreInts is the supply half of the *other* question: are the arguments this position is ever handed
// provably integers? Then the parameter needs no pair of its own and the body may read it as a plain
// number, which is what an `int`-typed parameter means to the arithmetic door.
func (s *pairScan) argsAreInts(es []Expr) bool {
	for _, e := range es {
		if !s.exprIsIntShaped(e) {
			return false
		}
	}
	return true
}

func (s *pairScan) exprIsIntShaped(e Expr) bool {
	switch n := e.(type) {
	case *IntLit:
		return true
	case *Name:
		vals, ok := s.bound[n.Value]
		if !ok || len(vals) == 0 {
			return false
		}
		return s.argsAreInts(vals)
	case *UnOp:
		return (n.Op == "-" || n.Op == "+") && s.exprIsIntShaped(n.X)
	case *BinOp:
		switch n.Op {
		case "+", "-", "*":
			return s.exprIsIntShaped(n.L) && s.exprIsIntShaped(n.R)
		}
	}
	return false
}

// litCarriesDouble asks a container literal whether one of the values it spells out is a double — the
// only evidence available for a slot read whose base was written as a literal.
func (s *pairScan) litCarriesDouble(e Expr) bool {
	switch n := e.(type) {
	case *ListLit:
		for _, el := range n.Elems {
			if s.exprCarriesDouble(el) {
				return true
			}
		}
	case *Tuple:
		for _, el := range n.Elems {
			if s.exprCarriesDouble(el) {
				return true
			}
		}
	case *DictLit:
		for _, v := range n.Vals {
			if s.exprCarriesDouble(v) {
				return true
			}
		}
	}
	return false
}

func (s *pairScan) litElementsAreNumberish(es []Expr, seen map[string]bool) bool {
	for _, e := range es {
		if !s.exprNumberish(e, seen) {
			return false
		}
	}
	return true
}

// knownIntParams names the parameters that carry no pair and may be read as plain numbers: those whose
// every recorded argument — and whose default, which is an argument the call site did not have to write —
// is provably an integer.
func (s *pairScan) knownIntParams(fd *FuncDef, spec *pairFnSpec, byIndex map[int][]Expr) map[string]bool {
	out := map[string]bool{}
	for i, p := range fd.Params {
		if spec.params[i] {
			continue
		}
		args := append([]Expr{}, byIndex[i]...)
		if d := fd.Params[i].Default; d != nil {
			args = append(args, d)
		}
		// A parameter no call site writes and no default describes is nobody's evidence: the body reads
		// it the way it always has, and the arithmetic door keeps its own answer or refusal.
		if len(args) == 0 || !s.argsAreInts(args) {
			continue
		}
		out[p.Name] = true
	}
	return out
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
func pairBodyAnswers(fd *FuncDef, spec *pairFnSpec, callees map[string]bool) bool {
	pairParams := map[string]bool{}
	for i, p := range fd.Params {
		if spec.params[i] {
			pairParams[p.Name] = true
		}
	}
	if len(pairParams) == 0 {
		return false
	}
	// A body may hold the pair without being handed it: `y = twice(x)` binds the answer the callee
	// doubled, floored or added with both words, and `return y` on the next line hands that same pair
	// back. Without this the returned expression is a plain local name, the answer direction closes the
	// body, the call falls back to the ordinary road, and the payload is truncated one statement before
	// the return — the wrong number Gap R.164 measured (roadmap L11.6, ADR 0280).
	bound := pairBoundCallNames(fd.Body, callees, pairParams)
	answers, ok := false, true
	var walk func(ss []Stmt)
	check := func(e Expr) {
		mentions, shaped := pairAnswerShape(e, pairParams, spec.intParams, callees, bound)
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

// pairBoundCallNames collects the names a body binds to the answer of a pair-returning call, keeping only
// those whose arguments mention a pair-carrying parameter — the binding that carries the caller's kind
// rather than a literal's. It is the answer direction's missing shape: `def outer(x): y = twice(x);
// return y` contains no expression that *looks* like a pair to the old question, yet the pair is exactly
// what `y` holds (roadmap Gap R.164, ADR 0280).
//
// A name the body writes two ways is not trusted: any other write to a name that had earned the pair —
// another value, an augmented assignment — retires it, because one tag word cannot travel with a name the
// body fills in two different ways (the rule `scanRebinds` enforces for forwarding, applied to a local).
// pairValueHoldsThePair reports that a bound value is computed from a pair-carrying parameter by an
// operator the tagged door serves — the same operator set `pairAnswerShape` walks for a returned
// expression, asked here of a bound one. Comparison and boolean operators are out: they answer a verdict
// or hand back an operand, not a number the return word has to carry (roadmap L11.6, Gap R.169, ADR 0285).
func pairValueHoldsThePair(e Expr, pairParams map[string]bool, callees map[string]bool, trusted ...map[string]bool) bool {
	switch n := e.(type) {
	case *BinOp:
		switch n.Op {
		case "+", "-", "*", "/", "//", "%":
		default:
			return false
		}
		return pairExprMentionsPairOperand(n, pairParams, callees)
	case *UnOp:
		return n.Op == "-" && pairValueHoldsThePair(n.X, pairParams, callees)
	case *Name:
		if pairParams[n.Value] {
			return true
		}
		// A chain: `y = x + 1` then `z = y * 2` holds the pair through `y`, which holds it through the
		// parameter. Asked of the set the caller is building, so the second binding is trusted exactly
		// when the first one is (roadmap Gap R.169, ADR 0285).
		for _, t := range trusted {
			if t[n.Value] {
				return true
			}
		}
		return false
	}
	return false
}

// pairExprMentionsPairOperand asks either arm of an arithmetic expression, through the shapes the answer
// direction already recognises, so a nested `(x - 1) * 2` is read the same way in both places.
func pairExprMentionsPairOperand(n *BinOp, pairParams map[string]bool, callees map[string]bool, trusted ...map[string]bool) bool {
	mentions := func(e Expr) bool {
		switch t := e.(type) {
		case *Name:
			return pairParams[t.Value]
		case *BinOp:
			return pairValueHoldsThePair(t, pairParams, callees, trusted...)
		case *UnOp:
			return pairValueHoldsThePair(t, pairParams, callees, trusted...)
		case *Index:
			return false
		}
		return false
	}
	return mentions(n.L) || mentions(n.R)
}

// pairCalleesOf is the identity the helpers above read as an argument, kept separate so the two uses of
// the callee set (`pairBoundCallNames` and the shape questions) stay one map.
func pairCalleesOf(callees map[string]bool) map[string]bool { return callees }

func pairBoundCallNames(body []Stmt, callees map[string]bool, pairParams map[string]bool) map[string]bool {
	bound := map[string]bool{}
	untrusted := map[string]bool{}
	var walk func(ss []Stmt)
	walk = func(ss []Stmt) {
		for _, s := range ss {
			switch n := s.(type) {
			case nil:
			case *AssignStmt:
				nm, isName := n.Target.(*Name)
				if !isName {
					continue
				}
				// A binding counts only when it is a call, by name, to a callee the scan judged
				// pair-returning, and it mentions one of this body's pair-carrying parameters. Every
				// question is asked of the value it belongs to — the first draft reached for `c.Fn`
				// before asking whether the value was a call at all, and a later `y = 3` panicked the
				// compiler, which is exit 2 and the compiler's bug, not the program's (ADR 0166).
				handsBackAPair := false
				if c, isCall := n.Value.(*Call); isCall {
					if cf, isFn := c.Fn.(*Name); isFn && callees[cf.Value] && pairMentions(c, pairParams) {
						handsBackAPair = true
					}
				}
				// A binding can also hold the pair directly: `y = x + 1` where `x` arrived as a pair
				// computes its answer through the tagged door, which writes the payload and the tag, and
				// `return y` on the next line hands both words on. Without this the returned name is
				// neither a pair parameter nor a name bound by a call, the answer direction closes the
				// body, the emission falls back to the one-word return, and the double the door computed
				// is truncated one statement before the `ret` — `y = x + 1` / `return y` printed `1` for
				// CPython's `1.1`, `y = x` printed `0` for `0.1`, `y = x * 2` printed `0` for `0.2`, and
				// `y = x - 1` printed `-1` for `-0.9`, every one at exit 0 (roadmap L11.6, Gap R.169,
				// ADR 0285). Asked of the same predicate the answer direction asks, so the decision about
				// the word and the decision about the pair cannot drift apart; a value that mentions no
				// pair parameter proves nothing and stays untrusted.
				if !handsBackAPair && pairValueHoldsThePair(n.Value, pairParams, pairCalleesOf(callees), bound) {
					handsBackAPair = true
				}
				if handsBackAPair {
					if !untrusted[nm.Value] {
						bound[nm.Value] = true
					}
					continue
				}
				delete(bound, nm.Value)
				untrusted[nm.Value] = true
			case *AugAssignStmt:
				if nm, isName := n.Target.(*Name); isName {
					delete(bound, nm.Value)
					untrusted[nm.Value] = true
				}
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
			}
		}
	}
	walk(body)
	return bound
}

// pairAnswerShape asks one expression two questions: does it *mention* a pair-carrying parameter, and
// is everything it mentions answerable by the pair door (a pair parameter, a number literal, or `+`,
// `-`, `*` over those). The other operators are not here because they have their own doors already
// (ADR 0253's `/`, ADR 0264's `//`/`%**`), and a second answer to the same question is how three
// truthiness tables happened.
func pairAnswerShape(e Expr, pairParams, intParams, pairCallees, bound map[string]bool) (mentions, shaped bool) {
	if c, isCall := e.(*Call); isCall {
		// `return g(x)` where g hands back a pair: the callee stored the tag beside its own return and
		// this body passes both words on beside its own. It is shaped because the pair is what the door
		// reads, and it mentions a pair-carrying parameter only if the arguments do (roadmap L11.1's
		// return direction, Gap R.139's caller half).
		if nm, isName := c.Fn.(*Name); isName && pairCallees[nm.Value] {
			return pairMentions(c, pairParams), true
		}
		return false, false
	}
	switch n := e.(type) {
	case *Name:
		if bound[n.Value] {
			// The name holds the pair a callee handed back; the emission reads it through the binding
			// door's registers, the same way `print(y)` already does (Gap R.164, ADR 0280).
			return true, true
		}
		return pairParams[n.Value], pairParams[n.Value]
	case *IntLit, *FloatLit, *BoolLit:
		return false, true
	case *UnOp:
		if n.Op != "-" {
			return false, false
		}
		m, sh := pairAnswerShape(n.X, pairParams, intParams, pairCallees, bound)
		return m, sh && m
	case *BinOp:
		switch n.Op {
		case "+", "-", "*", "//", "%":
			lm, ls := pairOperandShape(n.L, pairParams, intParams, pairCallees, bound)
			rm, rs := pairOperandShape(n.R, pairParams, intParams, pairCallees, bound)
			return lm || rm, ls && rs
		}
	}
	return false, false
}

// pairOperandShape is one arm of the answer's arithmetic: an arm that mentions no pair-carrying parameter
// is shaped only when it is a number the door can tag itself.
func pairOperandShape(e Expr, pairParams, intParams, pairCallees, bound map[string]bool) (mentions, shaped bool) {
	m, sh := pairAnswerShape(e, pairParams, intParams, pairCallees, bound)
	if m {
		return m, sh
	}
	return false, pairLeavesAreInts(e, intParams)
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
		return "", "", true, fmt.Errorf("codegen: %s is a float the pair road cannot box, and parameter %d of %s arrives as a (payload, tag) pair whose payload is an i32: the ordinary road would truncate the double to that word, which is a wrong number rather than a refusal (roadmap L11.6, Gap P.1, ADR 0273)", exprSurface(a), idx, fnName)
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
	// The payload may be a float box the callee's own frame allocated, and that frame is closed the
	// moment the call returns — without a root the box is recycled by the next allocation and the pair
	// carries a handle to somebody else's bits. A single answer printed fine (the box was still live),
	// which is why the shape that exposed it is two pair-returning calls in one expression:
	// `half(7.5) + twice(7.5)` printed `30.0` for CPython's `18.0`, while the same expression with the
	// arms swapped printed the truth — an order-dependent wrong number, the loudest kind this file
	// takes seriously (roadmap Gap R.164, ADR 0280). The registration is the same one a binding of the
	// answer makes (ADR 0181's rule for every handle-carrying store), taken at the call instead.
	if h, isHandle := g.pairPayloadNeedsRoot(c); isHandle {
		_ = h
		pay := g.newTmp()
		fmt.Fprintf(b, "  %s = alloca i32\n", pay)
		fmt.Fprintf(b, "  store i32 %s, i32* %s\n", v, pay)
		fmt.Fprintf(b, "  call void @rt_root_put(i32* %s)\n", pay)
		g.rooted = true
	}
	t := g.newTmp()
	fmt.Fprintf(b, "  %s = load i32, i32* @%s\n", t, pairTagSlot(nm.Value))
	return v, t, true, nil
}

// pairPayloadNeedsRoot reports that a pair-returning callee's answer may be a heap handle rather than an
// immediate int, which is the case whenever the callee's own arithmetic can answer a double. The question
// is asked of the callee's body, not of the call site: the tag that says which kind arrived is a run-time
// fact, and a root pushed for a payload that turned out to be an int costs one dead frame entry.
func (g *irGen) pairPayloadNeedsRoot(c *Call) (bool, bool) {
	nm, isName := c.Fn.(*Name)
	if !isName {
		return false, false
	}
	spec := g.pairSpecs[nm.Value]
	if spec == nil || !spec.returnsPair {
		return false, false
	}
	return true, true
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
	// `return g(x)`: the callee answered a pair and stored its tag beside its own return, so the pair
	// this body hands back is that one, read with the same door a print or a binding reads it with
	// (roadmap L11.1, Gap R.139's caller half).
	if p, t, okPair, pairErr := g.pairCallPair(b, e); pairErr != nil {
		return "", "", pairErr
	} else if okPair {
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
		// The pair a parameter holds came from the CALL SITE, not from arithmetic over a container the
		// program built: naming the true origin is what makes the refusal navigable (roadmap Gap R.38,
		// ADR 0285).
		g.taggedOrigin[p.Name] = taggedOriginParam
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
func pairUsesServed(fd *FuncDef, spec *pairFnSpec, callees map[string]bool) bool {
	gate := pairGate{pp: map[string]bool{}, ip: spec.intParams, callees: callees}
	for i, p := range fd.Params {
		if spec.params[i] {
			gate.pp[p.Name] = true
		}
	}
	if len(gate.pp) == 0 {
		return true
	}
	served := true
	check := func(e Expr) {
		if !gate.expr(e) {
			served = false
		}
	}
	// A condition is a position the truth door reads — `if v > 10:` asks an operand's truth, not its
	// value, and ADR 0269's chosen operands and ADR 0275's and/or already answer a (payload, tag). A
	// value position is the narrower question above, where a comparison has no door yet.
	checkCond := func(e Expr) {
		if !gate.cond(e) {
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
				checkCond(n.Cond)
				for _, e := range n.Elifs {
					walk([]Stmt{e})
				}
				walk(n.Then)
				walk(n.Else)
			case *WhileStmt:
				checkCond(n.Cond)
				walk(n.Body)
				walk(n.Else)
			case *ForStmt:
				checkCond(n.Iter)
				check(n.Var)
				walk(n.Body)
				walk(n.Else)
			case *MatchStmt:
				checkCond(n.Subject) // a pattern over a value whose kind only the tag says is not served
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
// pairGate is the body half of the scan's decision, asked with one set of names: which parameters the
// pair carries, which of the rest the call sites have shown to be integers, and which callees hand back
// a pair themselves — so that a call to one is a position the pair doors read rather than a position that
// keeps one word for the value.
type pairGate struct {
	pp      map[string]bool
	ip      map[string]bool
	callees map[string]bool
}

// expr answers one question about an expression that mentions a pair-carrying parameter: is it built
// only out of positions the pair doors answer? An expression that mentions none of them is the ordinary
// road's own business and is answered true without looking.
func (g *pairGate) expr(e Expr) bool {
	if !pairMentions(e, g.pp) {
		return true
	}
	switch n := e.(type) {
	case *Name:
		return g.pp[n.Value]
	case *IntLit, *FloatLit, *BoolLit:
		return true
	case *UnOp:
		return n.Op == "-" && g.expr(n.X)
	case *BinOp:
		switch n.Op {
		case "+", "-", "*", "//", "%", "and", "or":
			return g.operand(n.L) && g.operand(n.R)
			// A comparison is absent here on purpose: as a *value* it is read by a road that takes one
			// word for its operand, and has no door for a pair-carrying parameter. Asked as a
			// *condition* — the position `cond` answers for — the truth door does read the tag
			// (ADR 0269, ADR 0275), and `def big(v): if v > 10: return v * 2` is served (roadmap
			// Gap R.161's comparison half names the value position still owed one).
			//
			// `//` and `%` joined the list with ADR 0278: the flooring rules are the reference's, chosen
			// by the operands' kinds, and the tag is what says which kind arrived. Before that the two
			// operators were the pair door's silent hole — served by neither list, so a parameter carrying
			// a double stayed on the ordinary road and `floorit(5.0)` printed `2` for CPython's `2.0`,
			// `modop(7.5, 2)` printed `1` for `1.5`, both at exit 0 (roadmap Gap R.162).
		}
	case *Call:
		// print and str are the two positions a pair is rendered from (ADR 0268's renderer road and the
		// tag-reading printer); a call to a function whose own answer is a pair is the third, because the
		// callee stored the kind beside its return and the caller loads it back. Anything else is a
		// position that keeps one word for the value.
		nm, ok := n.Fn.(*Name)
		if !ok {
			return false
		}
		pairReturning := g.callees[nm.Value]
		switch nm.Value {
		case "print", "printf", "str":
		default:
			if !pairReturning {
				return false
			}
		}
		for _, a := range n.Args {
			if kw, isKw := a.(*KeywordArg); isKw {
				if !g.expr(kw.Value) {
					return false
				}
				continue
			}
			if !g.expr(a) {
				return false
			}
		}
		return true
	}
	return false
}

// cond answers the same question where the expression's value is a verdict the truth door reads rather
// than a number a position has to carry: a comparison over a pair-carrying parameter is served here,
// because `if v > 10:` asks an operand's truth and ADR 0269's operands and ADR 0275's short-circuit
// operators both answer a (payload, tag).
func (g *pairGate) cond(e Expr) bool {
	if !pairMentions(e, g.pp) {
		return true
	}
	if n, isBin := e.(*BinOp); isBin {
		switch n.Op {
		case "<", "<=", ">", ">=", "==", "!=", "and", "or":
			return g.condOperand(n.L) && g.condOperand(n.R)
		}
	}
	return g.expr(e)
}

// condOperand is cond's arm: a comparison's operand is read by the truth door, so a number the pair
// carries and a number the call sites proved are both asked the same way.
func (g *pairGate) condOperand(e Expr) bool {
	if !pairMentions(e, g.pp) {
		return pairLeavesAreInts(e, g.ip)
	}
	return g.cond(e)
}

// operand answers one operand of an expression that mentions a pair-carrying parameter. An operand that
// mentions none still has to be one the arithmetic door can pair with a tag of its own: a number, an
// integer-shaped parameter the call sites have proven, or nothing else. `"a" + v` is the concatenation
// road's question and `[v]` the container road's, and neither has a word for a tag — such a body keeps
// the convention, and the refusal, it had before this door.
func (g *pairGate) operand(e Expr) bool {
	if !pairMentions(e, g.pp) {
		return pairLeavesAreInts(e, g.ip)
	}
	return g.expr(e)
}

// pairLeavesAreInts walks an arithmetic-shaped operand and asks every leaf that names a parameter whether
// the call sites have proven it an integer. Leaves naming no parameter are literals and answer true;
// anything else — a text, a container, a call — is not a number this door tags.
func pairLeavesAreInts(e Expr, ip map[string]bool) bool {
	switch n := e.(type) {
	case nil:
		return true
	case *IntLit, *FloatLit, *BoolLit:
		return true
	case *Name:
		return ip[n.Value]
	case *UnOp:
		return pairLeavesAreInts(n.X, ip)
	case *BinOp:
		return pairLeavesAreInts(n.L, ip) && pairLeavesAreInts(n.R, ip)
	case *CondExpr:
		return pairLeavesAreInts(n.Cond, ip) && pairLeavesAreInts(n.If, ip) && pairLeavesAreInts(n.Else, ip)
	}
	return false
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
