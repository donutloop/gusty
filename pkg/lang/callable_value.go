// Roadmap L11.1/L11.7 (functions are values); ADR 0345.
//
// Defunctionalization: the pass that gives a function a value.
//
// A `def` names a function; it is not an expression that evaluates to one. That is the rule the
// compiled back end has always enforced, and it enforced it by refusing: `m(twice, 3)` where `m`
// calls its argument answered
//
//	codegen: "twice" is a function and a function has no value in this backend: "twice" is
//	        declared by "def twice(x)" and used as a value
//
// because the emitter needs a named `define` to `call` and there is none — the value in that
// argument slot is a name with nothing behind it. The reference answers 45.
//
// The construction that removes the question is Church's: if the only thing a program ever *does*
// with a function is call it, then the function can be replaced, at every place it is passed, by
// which function it is, and the calling that was written once can be written once per choice. That
// is defunctionalization, and it is an AST-to-AST pass:
//
//	def m(f, n):            →   def m$callable$3a1f(f, n):   # one clone per distinct function
//	        return f(n) * n #	    return twice(n) * n        #   passed at the called positions
//
//	m(twice, 3)             →   m$callable$3a1f(0, 3)        # the slot survives, filled with 0
//
// Each clone's body calls one known `define` by name, which is the shape the emitter builds an IR
// call against; the argument slot keeps its place so the clone's arity is the source function's
// arity and every call site has as many arguments as the `define` it targets. The placeholder is
// `0` rather than a deletion because the arity table, the pair road and the emitter all count that
// slot, and a program whose call sites and definitions disagree on arity is a different bug that
// this pass must not introduce.
//
// Three things make this a rewrite rather than a pattern match:
//
//   - It is asked of the whole program and answers all-or-nothing. A shape it cannot prove — a
//     function that reaches a container, a parameter that is both called and read, a nested `def`,
//     an arity that disagrees, a name that is bound twice, a call site with keyword arguments —
//     leaves the program exactly as it was, so the refusal the reader would have got is the refusal
//     they get, worded as it was. Nothing half-rewritten can print an answer, because nothing is
//     ever half-rewritten.
//   - It is a pass over the tree, not a matcher over source text: it inherits every argument shape
//     the back end can already lower, and it does not move text around the parser the way the
//     closure-lowering road has to. ADR 0245 and ADR 0248 made that argument for closures; it is
//     the same argument here, and it is why `f = lambda x: …` above a call of `f` is in scope.
//   - The caller's tree is never mutated. The program `--emit-ast` prints and `gusty fmt` renders
//     stays the program that was written, and a clone carries the *source* name in every place a
//     reader meets a function — the traceback frame, the line table, a diagnostic's callable — while
//     its IR symbol is the generated one. That is ADR 0320's rule for a rewritten call, extended to
//     a rewritten definition.
//
// What it does not claim: an interpreter's answer is a value, and this pass turns values into names.
// A function that reaches anywhere the pass cannot see — into a list, a dict, an attribute, a
// closure, a return, a global, a default, a keyword argument, a generator, a nested `def` — keeps the
// refusal. The sentence in docs/language.md remains "a function is declared, not evaluated"; this
// pass widens the set of programs for which that sentence costs nothing, and says nothing about the
// rest.
package lang

import (
	"encoding/json"
	"fmt"
	"hash/fnv"
	"reflect"
	"sort"
	"strings"
)

// callableCloneInfix and callableLambdaInfix mark the names this pass mints. The infix is `$`
// because the lexer's identifier rule is `[A-Za-z0-9_]`: a generated name cannot be written in
// source, so the only collision left to check is with a name the pass itself already handed out —
// which is checked, below, against every name the program mentions. The shape follows ADR 0245's
// `env$copy` and ADR 0238's `x$dict$0`.
const (
	callableCloneInfix  = "$callable$"
	callableLambdaInfix = "$lambda$"
	// callableAnonymousName is the reference's own word for a function written with `lambda`, in every
	// sentence a reader is shown — CPython says `<lambda>() takes 1 positional argument but 2 were
	// given` for a lambda that happens to be bound to a name, so the binding is not the name to print.
	callableAnonymousName = "<lambda>"
)

// callableReport is the pass's own account of what it rewrote. It travels with the compile so that
// a failing case can say which function was cloned, how many times, and which argument the choice
// came from — the thing a bare `compileError` line cannot tell you.
type callableReport struct {
	Tool             string `json:"tool"`
	Pass             string `json:"pass"`
	Hash             string `json:"hash"`
	Rewritten        bool   `json:"rewritten"`
	Clones           int    `json:"clones"`
	HoistedLambdas   int    `json:"hoisted_lambdas"`
	RewrittenCallNum int    `json:"rewritten_call_sites"`
	// RecursiveSites counts the calls of a higher-order function, written inside its own body, that a
	// clone now answers by calling itself — the recursion the pass follows rather than declines
	// (roadmap Gap R.267, ADR 0364).
	RecursiveSites  int      `json:"recursive_sites"`
	HigherOrderFunc []string `json:"higher_order_functions"`
	// Symbols are the IR names the pass minted — clones and hoisted lambdas — so a caller that reads
	// `--emit-llvm` can tell a definition this pass wrote from one the program wrote.
	Symbols []string `json:"symbols"`
	Refused []string `json:"refused"`
}

// ---------------------------------------------------------------------------
// Reading the program, once, in one preorder walk
// ---------------------------------------------------------------------------

// callableRead is everything the pass learns about one program. Every question the pass asks is
// asked of this record, which is why the record is built before any decision is taken: a question
// answered from whatever the pass happened to look at last is a question whose answer depends on
// the order the file was read, which is ADR 0273's bug in a new suit.
type callableRead struct {
	fds   map[string]*FuncDef // the program's declared functions, by name
	calls []*Call             // every call expression, in program preorder
	encs  []*FuncDef          // the function each call is written in (nil at module level)

	names    []*Name    // every name occurrence, in program preorder
	nameEncs []*FuncDef // the function each name occurrence is written in
	// inLambda marks, beside each call, whether the call is written inside a `lambda`. Such a call is
	// the closure road's program, and this pass does not rewrite through that boundary.
	inLambda []bool

	// calleeNames holds the *Name nodes that sit in the function slot of a call. "Only ever called"
	// is then a test of node identity, not of spelling: `f(1)` counts as a call, `print(f)` does
	// not, and a same-named local rebinds nothing silently.
	calleeNames map[*Name]bool

	// bound records every place the program binds a name, with the node that binds it. The pass
	// refuses any name it cannot prove is bound in exactly the one place it is reasoning about.
	bound map[string][]Node

	blocked bool // an unknown construct appeared where one must not be
	why     []string
}

// callableAllowedStmts is the closed set of statement kinds the pass will reason inside. A kind
// that is not on this list fails the pass closed rather than being assumed harmless — the same
// posture check.go takes by listing what it validates, and the reason the list is data.
var callableAllowedStmts = map[string]bool{
	"ReturnStmt": true, "ExprStmt": true, "AssignStmt": true, "AugAssignStmt": true,
	"IfStmt": true, "WhileStmt": true, "ForStmt": true, "FuncDef": true, "BreakStmt": true,
	"PassStmt": true, "ContinueStmt": true, "TryStmt": true, "WithStmt": true, "MatchStmt": true,
	"ClassDef": true, "RaiseStmt": true, "ImportStmt": true, "TypeAliasStmt": true,
	"YieldStmt": true, "YieldFromStmt": true, "ExternDecl": true,
}

// callableWalk is the one traversal every question is asked with. It is reflection over the AST on
// purpose: a hand-written switch stops seeing a construct the day someone adds a node kind, and a
// pass whose soundness is "I looked at everything" must in fact look at everything.
type callableWalk struct {
	visit func(n Node, enc *FuncDef)
	// inLambda counts how many lambdas deep the walk currently is. The pass rewrites the calls of a
	// higher-order function; a call written inside a `lambda` is the closure road's program, where
	// the parameters in scope are the lambda's and the frame that holds them is created by whatever
	// the closure pass builds. Rewriting a call through that boundary is not this pass's contract, so
	// such a call site declines the whole rewrite instead of being rewritten half.
	inLambda int
}

// callableWalker builds a walk from a visitor. The walk is handed out as a pointer so that the
// visitor and the walk read the same lambda-depth counter, which is what lets a visitor ask "am I
// written inside a lambda?" without every caller threading a parameter through the recursion.
func callableWalker(visit func(n Node, enc *FuncDef)) *callableWalk {
	return &callableWalk{visit: visit}
}

func (w *callableWalk) stmts(ss []Stmt, enc *FuncDef) {
	for _, s := range ss {
		w.node(s, enc)
	}
}

func (w *callableWalk) expr(e Expr, enc *FuncDef) {
	if e == nil || reflect.ValueOf(e).IsNil() {
		return
	}
	w.node(e, enc)
}

func (w *callableWalk) node(n Node, enc *FuncDef) {
	if n == nil {
		return
	}
	v := reflect.ValueOf(n)
	if v.Kind() != reflect.Ptr || v.IsNil() {
		return
	}
	if fd, ok := n.(*FuncDef); ok {
		enc = fd
	}
	if _, isLambda := n.(*Lambda); isLambda {
		w.inLambda++
		defer func() { w.inLambda-- }()
	}
	w.visit(n, enc)
	if fd, ok := n.(*FuncDef); ok {
		// A nested declaration is its own scope: the walk goes into it with fd as the enclosing
		// function, which is what makes "every occurrence of this parameter" mean what it says.
		w.stmts(fd.Body, fd)
		return
	}
	w.fields(v.Elem(), enc)
}

func (w *callableWalk) fields(v reflect.Value, enc *FuncDef) {
	if v.Kind() != reflect.Struct {
		return
	}
	t := v.Type()
	for i := 0; i < t.NumField(); i++ {
		f := v.Field(i)
		switch f.Kind() {
		case reflect.Slice, reflect.Array:
			if !callableIsNodeInterface(f.Type().Elem()) {
				continue
			}
			for k := 0; k < f.Len(); k++ {
				if e := f.Index(k); !e.IsNil() {
					w.node(e.Interface().(Node), enc)
				}
			}
		case reflect.Interface, reflect.Ptr:
			if f.IsNil() || !callableIsNodeInterface(f.Type()) {
				continue
			}
			w.node(f.Interface().(Node), enc)
		}
	}
}

var callableNodeInterface = reflect.TypeOf((*Node)(nil)).Elem()

// callableIsNodeInterface reports whether a field type can hold an AST node. `*Type`, `Span` and
// `string` cannot, which is what keeps the walk from wandering into the annotation grammar.
func callableIsNodeInterface(t reflect.Type) bool {
	return t.Kind() == reflect.Interface && t.Implements(callableNodeInterface)
}

func callableReadProgram(prog *Program) *callableRead {
	r := &callableRead{fds: map[string]*FuncDef{}, calleeNames: map[*Name]bool{}, bound: map[string][]Node{}}
	if prog == nil {
		return r
	}
	var w callableWalk
	w.visit = func(n Node, enc *FuncDef) {
		switch x := n.(type) {
		case *Call:
			r.calls = append(r.calls, x)
			r.encs = append(r.encs, enc)
			r.inLambda = append(r.inLambda, w.inLambda > 0)
			if fn, ok := x.Fn.(*Name); ok {
				r.calleeNames[fn] = true
			}
		case *Name:
			r.names = append(r.names, x)
			r.nameEncs = append(r.nameEncs, enc)
		}
	}
	w.stmts(prog.Stmts, nil)
	for _, s := range prog.Stmts {
		if fn, ok := s.(*FuncDef); ok {
			r.fds[fn.Name] = fn
		}
	}
	// The bindings, asked with the same walk. A name this pass reasons about is a name it must be
	// able to prove is written in one place, so the binder list is complete rather than convenient,
	// and an unknown statement kind blocks the pass instead of being skipped.
	var w2 callableWalk
	w2.visit = func(n Node, enc *FuncDef) { r.bind(n) }
	w2.stmts(prog.Stmts, nil)
	return r
}

func (r *callableRead) bind(n Node) {
	switch x := n.(type) {
	case *FuncDef:
		r.bindName(x.Name, x)
		for _, p := range x.Params {
			r.bindName(p.Name, x)
		}
		for _, d := range x.Decorators {
			_ = d // decorators block a rewrite; callableBodyBlocked is where that is enforced
		}
	case *ClassDef:
		r.bindName(x.Name, x)
	case *TypeAliasStmt:
		r.bindName(x.Name, x)
	case *ExternDecl:
		r.bindName(x.Name, x)
	case *AssignStmt:
		for _, nm := range callablePatternNames(x.Target) {
			r.bindName(nm, x)
		}
	case *AugAssignStmt:
		for _, nm := range callablePatternNames(x.Target) {
			r.bindName(nm, x)
		}
	case *ForStmt:
		for _, nm := range callablePatternNames(x.Var) {
			r.bindName(nm, x)
		}
	case *WithStmt:
		if x.As != nil {
			r.bindName(x.As.Value, x)
		}
	case *Comp:
		if x.ForVar != nil {
			r.bindName(x.ForVar.Value, x)
		}
	case *Generator:
		if x.ForVar != nil {
			r.bindName(x.ForVar.Value, x)
		}
	case *Lambda:
		for _, p := range x.Params {
			r.bindName(p.Name, x)
		}
	case *AssignExpr:
		if x.Name != nil {
			r.bindName(x.Name.Value, x)
		}
	case *MatchStmt:
		for _, c := range x.Cases {
			if c == nil {
				continue
			}
			for _, p := range append(append([]Expr{}, c.Pattern), c.Or...) {
				for _, nm := range callablePatternNames(p) {
					r.bindName(nm, x)
				}
			}
		}
	}
	if callableIsStmtNode(n) {
		kind := reflect.TypeOf(n).Elem().Name()
		if !callableAllowedStmts[kind] {
			r.blocked = true
			r.why = append(r.why, kind+" is not a statement kind this pass knows")
		}
	}
}

func (r *callableRead) bindName(name string, at Node) { r.bound[name] = append(r.bound[name], at) }

func callableIsStmtNode(n Node) bool { _, ok := n.(Stmt); return ok }

// callablePatternNames reads the names a binding target mentions: a bare `x`, or a tuple/list
// unpacking `(a, b)`. Anything else (`d[k]`, `o.a`) is not a name this pass can reason about, and
// it returns nothing for it — the pass refuses those shapes by other routes (a rewrite happens
// only where it can prove every mention, and an unpacking target that is not a name is a shape the
// higher-order body is not allowed to contain anyway).
func callablePatternNames(e Expr) []string {
	switch x := e.(type) {
	case *Name:
		return []string{x.Value}
	case *Tuple:
		var out []string
		for _, el := range x.Elems {
			out = append(out, callablePatternNames(el)...)
		}
		return out
	case *ListLit:
		var out []string
		for _, el := range x.Elems {
			out = append(out, callablePatternNames(el)...)
		}
		return out
	}
	return nil
}

// occurrencesOf returns every occurrence of a name written in the program.
func (r *callableRead) occurrencesOf(name string) []*Name {
	var out []*Name
	for i, n := range r.names {
		if n.Value == name {
			_ = i
			out = append(out, n)
		}
	}
	return out
}

// onlyCalledInBody reports whether every occurrence of a higher-order function's parameter, in that
// function's own body, is the callee of a call — or the one argument of the function's own recursive
// call that hands the parameter back to the position it came from (`passed`, which
// `callableSelfCallsOf` fills). A parameter that is also printed, stored, indexed or returned is not a
// parameter this pass can substitute for — it is being used as a value; the recursion is the single
// position that is not a use as a value, because the clone that body becomes calls itself there
// (roadmap Gap R.267, ADR 0364).
func (r *callableRead) onlyCalledInBody(fn *FuncDef, param string, passed map[*Name]bool) bool {
	seen, called := 0, 0
	for i, n := range r.names {
		if n.Value != param || r.nameEncs[i] != fn {
			continue
		}
		seen++
		if r.calleeNames[n] || passed[n] {
			called++
		}
	}
	return seen > 0 && seen == called
}

// callableSelfCall is one call of a function's own name read out of that function's own body, with the
// answer to "does this position receive the parameter it names?". The three flags are the three ways a
// recursion is not the clone's to answer, and each has its own sentence so a reader is told which one
// the program hit (ADR 0166's rule that a refusal names the missing half).
type callableSelfCall struct {
	rank     int    // index among the calls of the body's own preorder
	line     int    // where the recursion is written
	args     []Expr // the arguments the recursion passes, as the program wrote them
	slots    []bool // slots[i]: the argument at position i is the parameter i, handed back to itself
	bad      bool   // not a plain, full-arity, positional call of the function's own name
	inLambda bool   // written inside a `lambda`, whose hoisted body has no frame for the recursion
}

// callableSelfCallsOf walks a function's body exactly the way `callableBodyCalls` does — the k-th call
// of the body is the k-th call of its clone, which is the coordinate every rewrite in
// `applyDefunctionalization` is written against — and returns the calls of the function's own name.
//
// A call whose callee is a name is the only shape considered: `f = self; f(x)` is a different program,
// and one whose callee some other pass has already decided about is not this pass's to follow.
func callableSelfCallsOf(fn *FuncDef) []*callableSelfCall {
	var out []*callableSelfCall
	if fn == nil || fn.Body == nil {
		return out
	}
	rank := 0
	var w callableWalk
	w.visit = func(n Node, enc *FuncDef) {
		c, isCall := n.(*Call)
		if !isCall {
			return
		}
		if nm, isName := c.Fn.(*Name); isName && enc == fn && nm.Value == fn.Name && !callableHasParamNamed(fn, nm.Value) {
			sc := &callableSelfCall{rank: rank, line: c.Span().Line, args: append([]Expr(nil), c.Args...)}
			sc.inLambda = w.inLambda > 0
			sc.bad = len(c.Args) != len(fn.Params) || callableHasKeywordArgs(c.Args)
			if !sc.bad {
				sc.slots = make([]bool, len(fn.Params))
				for i, p := range fn.Params {
					an, isArgName := c.Args[i].(*Name)
					sc.slots[i] = isArgName && an.Value == p.Name
				}
			}
			out = append(out, sc)
		}
		rank++
	}
	w.stmts(fn.Body, fn)
	return out
}

// callableSelfDeclines is the recursion's own audit, asked before any clone exists: a higher-order body
// that calls itself is in this pass's hands now, and three shapes are not the clone's to answer. The
// sentence is the reason, not a shrug — ADR 0166's rule that a refusal names the half that is missing,
// and the reason a mutually recursive pair reads as a declined recursion rather than as the ordinary
// road's one-word short answer.
func callableSelfDeclines(fn *FuncDef, self []*callableSelfCall, positions []int) string {
	if fn == nil {
		return ""
	}
	for _, sc := range self {
		if sc.inLambda {
			return fmt.Sprintf("%q recurses at line %d from inside a lambda, whose body this pass gives a declaration of its own and therefore no frame to hand the recursion through", fn.Name, sc.line)
		}
		if sc.bad {
			return fmt.Sprintf("%q recurses at line %d without filling every parameter positionally, which a clone cannot answer for the recursion", fn.Name, sc.line)
		}
		for _, pos := range positions {
			if pos < 0 || pos >= len(sc.slots) || !sc.slots[pos] {
				name := ""
				if pos >= 0 && pos < len(fn.Params) && fn.Params[pos] != nil {
					name = fn.Params[pos].Name
				}
				return fmt.Sprintf("%q recurses at line %d and hands %q something other than itself; a clone follows a recursion only while each called parameter is handed back to its own position", fn.Name, sc.line, name)
			}
		}
	}
	return ""
}

// callableHasParamNamed asks whether a name belongs to a function's parameters. A body that writes
// `go(n)` where `go` is both the function's name and one of its parameters is calling the PARAMETER —
// the frame's name wins over the module's, here as in the reference — and that call belongs to the
// inner-call road, which has always answered it by name.
func callableHasParamNamed(fn *FuncDef, name string) bool {
	if fn == nil {
		return false
	}
	for _, p := range fn.Params {
		if p != nil && p.Name == name {
			return true
		}
	}
	return false
}

// callableSelfPassThrough names the argument nodes the recursion hands back to their own positions.
// Only a call that is well shaped for the clone contributes: a recursion the pass would have to
// decline contributes nothing, so a parameter whose only non-callee occurrence is in such a call is
// never credited and the function is never registered as higher-order.
func callableSelfPassThrough(fn *FuncDef, self []*callableSelfCall) map[*Name]bool {
	passed := map[*Name]bool{}
	if fn == nil {
		return passed
	}
	for _, sc := range self {
		if sc.bad || sc.inLambda {
			continue
		}
		for i, ok := range sc.slots {
			if !ok || i >= len(fn.Params) || i >= len(sc.args) {
				continue
			}
			if an, isName := sc.args[i].(*Name); isName && an.Value == fn.Params[i].Name {
				passed[an] = true
			}
		}
	}
	return passed
}

// callsOfParam returns the calls in fn's body whose callee is the named parameter, with their rank
// in the body's own preorder — the coordinate the clone is written against.
func (r *callableRead) callsOfParam(fn *FuncDef, param string) ([]*Call, []int, bool) {
	var calls []*Call
	var ranks []int
	decided := true
	rank := 0
	var w callableWalk
	w.visit = func(n Node, enc *FuncDef) {
		c, isCall := n.(*Call)
		if !isCall {
			return
		}
		nm, isName := c.Fn.(*Name)
		if isName && nm.Value == param && enc == fn {
			if w.inLambda > 0 {
				// The parameter is called from inside a lambda written in the body. The clone would
				// have to give that lambda a frame to read the substituted name through, which is the
				// closure road's business and not this pass's (ADR 0245, ADR 0248).
				decided = false
				return
			}
			calls = append(calls, c)
			ranks = append(ranks, rank)
		}
		rank++
	}
	if fn.Body != nil {
		w.stmts(fn.Body, fn)
	}
	return calls, ranks, decided
}

// callableOnlyCallableArgs reports whether an expression mentions no name and no lambda at all —
// the test a non-called argument slot must pass, so that a rewrite never has to *think* about what
// an argument means.
func callableOnlyLiteralData(e Expr) bool {
	if e == nil {
		return false
	}
	ok := true
	var w callableWalk
	w.visit = func(n Node, enc *FuncDef) {
		switch n.(type) {
		case *Name, *Lambda, *Attr, *Index, *Slice, *Generator, *Call, *Comp:
			ok = false
		}
	}
	w.expr(e, nil)
	return ok
}

// ---------------------------------------------------------------------------
// The plan
// ---------------------------------------------------------------------------

// callableFn is one higher-order function: a `def` whose body calls one of its own parameters.
type callableFn struct {
	def       *FuncDef
	paramPos  map[string]int // parameter name -> index in def.Params
	calledPos []int          // parameter indexes the body calls, ascending
	sites     []*callableSite
	inner     []*callableInner
	// self is every call of this function written inside its own body, which each of its clones answers
	// by calling itself (roadmap Gap R.267, ADR 0364).
	self   []*callableSelfSite
	clones []*callableClone
}

// callableSite is a call of a higher-order function from outside every higher-order body.
type callableSite struct {
	rank  int      // index among all calls in the program's preorder
	slots []string // which function each called parameter is asked to be, in calledPos order
	args  []int    // unused placeholder for arity auditing
}

// callableInner is a call *of a parameter* inside a higher-order body.
type callableInner struct {
	rank int // index among the calls of that body's preorder
	pos  int // which parameter of the enclosing function this call evaluates
	// slot is which entry of the clone's tuple that parameter is asked to be: the ordinal of `pos` within
	// calledPos, NOT `pos` itself. The two coincide exactly when the body calls the FIRST parameter, which
	// is the only shape every program in the suite wrote, and a program that wrote `def run(v, fn)` read
	// the one-entry tuple with the parameter's position, failed its own bounds check and handed the
	// program back to the refusal it had before (roadmap Gap R.313, ADR 0364).
	slot int
	args int // how many arguments it passes
}

// callableSelfSite is a call of the higher-order function from inside its own body that hands back,
// at each position the body calls, the very parameter that position names:
//
//	def fact(n, f):
//	    if n <= 1:
//	        return 1
//	    return f(n) * fact(n - 1, f)   # <- this call
//
// The clone answers such a call by calling ITSELF: the entry tuple the recursion carries is the
// enclosing clone's own, so the callable never has to exist as a value — which is the one thing this
// pass does not make (roadmap L11.7 · Gap R.267, ADR 0364).
//
// Before this row the program was handed back whole, because `onlyCalledInBody` counted the `f` in
// `fact(n - 1, f)` as a use as a value — which, on the ordinary road, it is — and the function was
// never registered as higher-order at all. What is credited now is exactly that one occurrence, at
// exactly that one position, and nothing else: pass the callable anywhere else and the program keeps
// the refusal it has today.
type callableSelfSite struct {
	rank int // index among the calls of that body's preorder — the coordinate the clone is written against
	line int // the source line, for the sentence that declines a recursion this pass cannot follow
}

type callableClone struct {
	key  []string
	name string
	fn   *callableFn
}

// callableArgHoist is a lambda the pass gave a declaration to in the argument position of a call it does not
// otherwise own — the ordering builtins' `key=`, a builtin's plain argument — recorded by its place among
// the program's calls so the rewrite can point that argument at the declaration (roadmap L11.7, ADR 0340's
// key road, which already calls a named callable and only needed the lambda to have a name).
type callableArgHoist struct {
	rank int
	pos  int
	name string
}

// callablePlan is the whole rewrite, decided before a single node is copied.
type callablePlan struct {
	fns map[string]*callableFn
	// order is fns in the order the source declares them. Map iteration in Go is deliberately
	// random, and this pass mints names by counter and clones one per distinct argument tuple: a
	// plan built in a random order would give one program two different sets of IR symbols across
	// two runs, which is the determinism the golden record and the lit tests both depend on.
	order   []*callableFn
	aliases map[string]string // `g = f` retargeted to `f`, when nothing else reads `g`
	hoisted []*FuncDef        // lambdas given declarations of their own
	// argHoists records, for the hoisted lambdas that sit in an argument position, which call and which
	// argument slot to point at the name the pass minted.
	argHoists []callableArgHoist
	// hoistOrigin records, beside each hoisted declaration, the lambda node it came from. The body
	// check asks "is this lambda one the plan gave a declaration to?" and answers it by node
	// identity, so a lambda the plan never resolved still blocks the rewrite.
	hoistOrigin []*Lambda
	// arity records, for every name a call site may be asked to substitute, how many parameters the
	// function behind that name takes. A declared function answers from its own declaration; a hoisted
	// lambda answers from the lambda. The clone's body calls the name with the arguments the parameter
	// call passed, so the two numbers have to be checked against each other before the call is written
	// -- otherwise the module asks the linker a question the answer to is a wrong answer.
	arity map[string]int
	// declared is the set of names the program gives to a `def`, recorded while the plan was read.
	// The invariant at the end of the rewrite is stated over this set.
	declared map[string]bool
	taken    map[string]bool
	// seeds numbers each node a name is minted for, in the order the plan first asked (see freshName).
	seeds     map[Node]int
	seedCount int
}

// callableDeclineError is returned in place of a plan; the program is left alone, which is the
// pass's only safe answer and the reason a decline needs no ceremony.
type callableDecline struct{ why string }

func (e *callableDecline) Error() string {
	// The citation rides on the sentence because the account an agent reads (`gustyc --callables`, the
	// schema's definitions.callablePassReport) prints this text as its `refused` field: a decline that
	// names no row leaves the reader with no idea who owns the shape (ADR 0166, ADR 0181's rule that a
	// diagnostic is a pointer to the row that can fix it).
	return "defunctionalization declines: " + e.why + " (roadmap L11.7, ADR 0345)"
}

func callableDeclinef(format string, args ...any) error {
	return &callableDecline{why: fmt.Sprintf(format, args...)}
}

// planDefunctionalization reads a program and decides its rewrite, or declines. It mutates nothing.
func planDefunctionalization(prog *Program) (*callablePlan, error) {
	if prog == nil || len(prog.Stmts) == 0 {
		return nil, &callableDecline{why: "empty program"}
	}
	r := callableReadProgram(prog)
	if r.blocked {
		return nil, &callableDecline{why: strings.Join(r.why, "; ")}
	}
	plan := &callablePlan{
		fns: map[string]*callableFn{}, aliases: map[string]string{},
		arity: map[string]int{}, declared: map[string]bool{}, taken: map[string]bool{},
	}
	for name := range r.fds {
		plan.declared[name] = true
	}

	// Which functions call their own parameters. Asked of the program, and answered per definition,
	// because which parameters a body calls is a property of the definition — not of the call site
	// the walker happened to reach first.
	for _, s := range prog.Stmts {
		fn, ok := s.(*FuncDef)
		if !ok {
			continue
		}
		if len(fn.Decorators) > 0 || fn.Async {
			continue
		}
		self := callableSelfCallsOf(fn)
		passed := callableSelfPassThrough(fn, self)
		positions, ok := callableCalledParams(r, fn, passed)
		if !ok || len(positions) == 0 {
			continue
		}
		// The recursion is now the pass's to answer, which means it is the pass's to refuse. Each of the
		// three shapes below is a recursion whose clone would call something the plan has no entry for;
		// each declines the whole program, naming the recursion as the reason, rather than letting the
		// ordinary road answer it one word short (roadmap Gap R.267, ADR 0364).
		if why := callableSelfDeclines(fn, self, positions); why != "" {
			return nil, callableDeclinef("%s", why)
		}
		cf := &callableFn{def: fn, paramPos: map[string]int{}, calledPos: positions}
		for i, p := range fn.Params {
			cf.paramPos[p.Name] = i
		}
		for _, sc := range self {
			cf.self = append(cf.self, &callableSelfSite{rank: sc.rank, line: sc.line})
		}
		plan.fns[fn.Name] = cf
		plan.order = append(plan.order, cf)
	}

	// A higher-order function is rewritable only while its own name is only ever called: `xs =
	// [apply]` asks for a value of the function about to be cloned, and cloning does not answer that.
	for _, cf := range plan.order {
		if !r.nameIsOnlyCalled(cf.def.Name) {
			return nil, callableDeclinef("%q is used as a value as well as called", cf.def.Name)
		}
		if !callableBoundOnlyByDef(r, cf.def.Name) {
			return nil, callableDeclinef("%q is rebound somewhere", cf.def.Name)
		}
		if len(cf.def.Body) == 0 {
			return nil, callableDeclinef("%q has no body to clone", cf.def.Name)
		}
	}

	// Module-level aliases. `g = twice` is a function with no value today; when every call of `g` can
	// be answered by `twice` and nothing else reads `g`, the alias is retargeted and the binding that
	// gave it no value is replaced by one that has a value. A program that compiles today is
	// unaffected: this branch only fires for a name whose whole history is being called.
	for _, s := range prog.Stmts {
		a, ok := s.(*AssignStmt)
		if !ok {
			continue
		}
		tgt, ok := a.Target.(*Name)
		if !ok {
			continue
		}
		rhs, ok := a.Value.(*Name)
		if !ok || tgt.Value == rhs.Value {
			continue
		}
		if _, isDef := r.fds[rhs.Value]; !isDef {
			continue
		}
		if _, higherOrder := plan.fns[rhs.Value]; higherOrder {
			return nil, callableDeclinef("%q aliases the higher-order %q", tgt.Value, rhs.Value)
		}
		// The alias is only a name for the pass's purposes when the alias's whole history is this one
		// binding and these calls. `f = 1; g = f; m(g, 2)` is a program whose `g` names an int by the
		// time it is called and an int is not a function; `g = twice; print(g)` is a program that wants
		// a function value, which is the thing this pass does not do.
		if isPredeclaredName(tgt.Value) || isPredeclaredName(rhs.Value) {
			continue
		}
		// The alias is a name for the pass's purposes only when this one binding is its whole history.
		// `f = 1; g = f; m(g, 2)` is a program whose `g` names an int by the time it is called, and an
		// int is not a function; and an alias whose name is also *read* is caught by the invariant the
		// rewrite is checked against (callableNoFunctionValuesLeft), which is where the promise is
		// actually kept rather than in the condition that decides to try.
		if callableBoundExactly(r, tgt.Value, []Node{a}) {
			plan.aliases[tgt.Value] = rhs.Value
		}
	}

	// A module-level binding of a LAMBDA is the same shape with the value written inline:
	// `h = lambda x: x * 3` / `print(h(4))` is a function used as a name, and the name is only ever called.
	// The lambda is given a declaration of its own — the hoist the argument position already has — and the
	// alias points at it. It is held to the stricter rule here, because there is no frame to read from:
	// a lambda whose body mentions a name the module binds is left alone, and the program keeps the refusal
	// it has today rather than be answered from a value the pass cannot see at the point the call runs.
	for _, s := range prog.Stmts {
		a, ok := s.(*AssignStmt)
		if !ok {
			continue
		}
		tgt, ok := a.Target.(*Name)
		if !ok {
			continue
		}
		if _, taken := plan.aliases[tgt.Value]; taken {
			continue
		}
		l, ok := a.Value.(*Lambda)
		if !ok || !callableLambdaIsSelfContained(l) {
			continue
		}
		if isPredeclaredName(tgt.Value) {
			continue
		}
		if !callableBoundExactly(r, tgt.Value, []Node{a}) {
			continue
		}
		body := copyASTExpr(l.Body)
		if body == nil {
			return nil, callableDeclinef("the lambda bound to %q could not be copied", tgt.Value)
		}
		name, ok := plan.freshName(tgt.Value+callableLambdaInfix, l)
		if !ok {
			return nil, callableDeclinef("no name could be minted for the lambda bound to %q", tgt.Value)
		}
		def := &FuncDef{
			Name:   name,
			Params: append([]*Param(nil), l.Params...),
			Body:   []Stmt{&ReturnStmt{Expr: body, Src: l.Span()}},
			Src:    l.Span(),
		}
		// The reference names an anonymous function `<lambda>` even when a name is bound to it — that is
		// the word in `g = lambda x: x` / `g(1, 2)`'s TypeError — so the hoisted def reports that word and
		// not the binding the pass happened to mint a symbol from (ADR 0345 §3, ADR 0166's naming rule).
		def.CloneOf = callableAnonymousName
		plan.hoisted = append(plan.hoisted, def)
		plan.hoistOrigin = append(plan.hoistOrigin, l)
		plan.arity[name] = len(l.Params)
		plan.aliases[tgt.Value] = name
	}

	// A closed lambda in an argument position is given a declaration here, so the door that reads the
	// argument already knows how to call a named callable meets one it can name: `sorted(xs, key=lambda x:
	// -x)` and `max(xs, key=lambda v: -v)` ask ADR 0340's key road, which calls a function by the ordinary
	// call road and refused only because a lambda has no name to call. The lambda must read nothing but its
	// own parameters — a free name would be a value the pass cannot see where the call runs — and a call of
	// a function the program declared is left to the clone road above, which owns that shape (roadmap
	// L11.7, ADR 0345).
	for rank, c := range r.calls {
		nm, isCalleeName := c.Fn.(*Name)
		if !isCalleeName {
			continue
		}
		callee := nm.Value
		if aliased, isAlias := plan.aliases[callee]; isAlias {
			callee = aliased
		}
		if _, isUser := r.fds[callee]; isUser {
			continue // a call of the program's own function is the clone road's business, not this one
		}
		for pos, a := range c.Args {
			arg := a
			if kw, isKw := a.(*KeywordArg); isKw {
				arg = kw.Value
			}
			l, isLambda := arg.(*Lambda)
			if !isLambda || !callableLambdaIsSelfContained(l) {
				continue
			}
			if plan.hoistsLambda(l) {
				continue // the argument road above already gave this one a declaration
			}
			body := copyASTExpr(l.Body)
			if body == nil {
				return nil, callableDeclinef("the lambda argument at line %d could not be copied", c.Span().Line)
			}
			name, ok := plan.freshName(callableSurfaceName(nm.Value)+callableLambdaInfix, l)
			if !ok {
				return nil, callableDeclinef("no name could be minted for a hoisted lambda argument")
			}
			def := &FuncDef{
				Name:   name,
				Params: append([]*Param(nil), l.Params...),
				Body:   []Stmt{&ReturnStmt{Expr: body, Src: l.Span()}},
				Src:    l.Span(),
			}
			def.CloneOf = callableAnonymousName
			plan.hoisted = append(plan.hoisted, def)
			plan.hoistOrigin = append(plan.hoistOrigin, l)
			plan.argHoists = append(plan.argHoists, callableArgHoist{rank: rank, pos: pos, name: name})
			plan.arity[name] = len(l.Params)
		}
	}

	// The pass has work to do only when something asked for it: a higher-order body to clone, an alias
	// whose calls can be pointed at the function they mean, or a lambda argument the pass can name. An
	// ordinary program is handed back with one comparison spent on it.
	if len(plan.fns) == 0 && len(plan.aliases) == 0 && len(plan.argHoists) == 0 {
		// The recursion gets one more chance to explain itself. A pair of functions that call a parameter
		// and hand it to each other is a recursion no clone can be written for — and it registers no
		// higher-order function at all, because handing the callable to the other body is a use as a value
		// on the ordinary road. Said here rather than at the pair's own row because this is the one place
		// the pass knows nothing else was rewritable: a decline that fires on a program with work to do
		// would take the answer away from a higher-order call that has nothing to do with the pair.
		if why := callableMutualHigherOrderDecline(r); why != "" {
			return nil, callableDeclinef("%s", why)
		}
		return nil, &callableDecline{why: "no function calls its own parameter, no name aliases one, and no lambda argument needs a declaration"}
	}

	// The call sites: each is read against the definition it targets, and each called parameter is
	// resolved to the function the call asks for.
	for rank, c := range r.calls {
		nm, ok := c.Fn.(*Name)
		if !ok {
			continue
		}
		name := nm.Value
		if aliased, isAlias := plan.aliases[name]; isAlias {
			name = aliased
		}
		cf, higherOrder := plan.fns[name]
		if !higherOrder {
			continue
		}
		if enc := r.encs[rank]; enc != nil {
			if _, inside := plan.fns[enc.Name]; inside {
				if enc != cf.def {
					// One higher-order function called from inside another: the callee's own body has been
					// taken out of the program and the caller's clone has no entry tuple to hand the call, so
					// neither clone owns it. Before the self-call was this pass's, the reading never got this
					// far; now it says what the shape is instead of leaving the invariant to withdraw a
					// rewrite it cannot explain (roadmap Gap R.267, ADR 0364).
					return nil, callableDeclinef("%q is called from inside the higher-order body of %q at line %d: a recursion between two higher-order functions is a call neither clone can answer", name, enc.Name, c.Span().Line)
				}
				continue // a call of the function from inside its own body is the self-call's business
			}
		}
		if callableHasKeywordArgs(c.Args) || len(c.Args) != len(cf.def.Params) {
			return nil, callableDeclinef("the call of %q at line %d does not fill the parameters", name, c.Span().Line)
		}
		site := &callableSite{rank: rank}
		for _, pos := range cf.calledPos {
			entry, err := plan.resolve(r, c.Args[pos], r.encs[rank])
			if err != nil {
				return nil, err
			}
			site.slots = append(site.slots, entry)
		}
		cf.sites = append(cf.sites, site)
	}

	// The inner calls: where the parameter is the callee. Each must be a call of a plain name, of the
	// parameter, passing the arity the entries are declared with.
	for _, cf := range plan.order {
		for slot, pos := range cf.calledPos {
			param := cf.def.Params[pos].Name
			calls, ranks, ok := r.callsOfParam(cf.def, param)
			if !ok || len(ranks) != len(calls) || len(calls) == 0 {
				return nil, callableDeclinef("%q does not call %q in a way a clone can answer", cf.def.Name, param)
			}
			arity := len(calls[0].Args)
			for k, c := range calls {
				if len(c.Args) != arity || callableHasKeywordArgs(c.Args) {
					return nil, callableDeclinef("%q calls %q with differing arity", cf.def.Name, param)
				}
				cf.inner = append(cf.inner, &callableInner{rank: ranks[k], pos: pos, slot: slot, args: len(c.Args)})
			}
		}
		if len(cf.inner) == 0 || len(cf.sites) == 0 {
			return nil, callableDeclinef("%q has a call of a parameter that no call site answers", cf.def.Name)
		}
		sort.SliceStable(cf.inner, func(i, j int) bool { return cf.inner[i].rank < cf.inner[j].rank })
		if !callableInnerAgrees(plan, cf) {
			return nil, callableDeclinef("%q's entries are not declared for the calls %q makes", cf.def.Name, cf.def.Name)
		}
	}

	// The bodies, checked last: a lambda in a higher-order body is allowed only where the call sites
	// above gave it a declaration of its own, and that is not known until every call site has been
	// read. Asking the question in the other order is the difference between a pass and a pattern
	// match -- the check has to see the plan, not merely the tree.
	for _, cf := range plan.order {
		if callableBodyBlocked(r, plan, cf.def) {
			return nil, callableDeclinef("%s's body holds something a clone could not answer", cf.def.Name)
		}
	}

	// One clone per distinct tuple of functions the called parameters are asked to be.
	for _, cf := range plan.order {
		seen := map[string]string{}
		for _, site := range cf.sites {
			k := strings.Join(site.slots, "|")
			if _, hit := seen[k]; hit {
				continue
			}
			name, ok := plan.cloneName(cf.def.Name, k)
			if !ok {
				return nil, callableDeclinef("no name could be minted for a clone of %q", cf.def.Name)
			}
			seen[k] = name
			cf.clones = append(cf.clones, &callableClone{key: strings.Split(k, "|"), name: name, fn: cf})
		}
		if len(cf.clones) == 0 {
			return nil, callableDeclinef("%q is never called", cf.def.Name)
		}
	}
	if !callableCloneNamesDiffer(plan) {
		return nil, &callableDecline{why: "two clones would share a name"}
	}
	if !callableHoistNamesDistinct(plan) {
		return nil, &callableDecline{why: "a generated name would be shared"}
	}
	return plan, nil
}

// callableCalledParams decides which parameters of a function are called as callees. A parameter is
// eligible only when every one of its occurrences inside the body is a callee (or the recursion
// handing it back to itself, `passed`) and the body's calls of it are uniform; a parameter that is also
// read is not eligible, and a function with an ineligible parameter that is *also* called elsewhere is
// left alone entirely.
func callableCalledParams(r *callableRead, fn *FuncDef, passed map[*Name]bool) ([]int, bool) {
	if fn.Body == nil {
		return nil, false
	}
	seen := map[string]bool{}
	for _, p := range fn.Params {
		if seen[p.Name] || isPredeclaredName(p.Name) {
			return nil, false
		}
		seen[p.Name] = true
	}
	var positions []int
	for i, p := range fn.Params {
		if !r.onlyCalledInBody(fn, p.Name, passed) {
			continue
		}
		// A parameter the body calls has to be a plain slot: a default would be filled by a value the
		// clone cannot see, and a parameter declared `function` or `Callable[...]` is ADR 0284's road to
		// walk — that machinery decides the shape of such a call and its trap today, and a clone written
		// over the top of it would answer the same program by two roads.
		if p.Default != nil || callableAnnotIsCallable(p.Annot) {
			return nil, false
		}
		positions = append(positions, i)
	}
	return positions, true
}

// callableAnnotIsCallable asks whether an annotation is the one that declares a parameter to be
// called: `cb: function` (ADR 0284) or `cb: Callable[[int], int]`. Everything else — `x: int`, `s: str`
// — is a plain slot that the substitution does not care about, and refusing it would decline half the
// programs in the record for a reason this pass has no opinion about.
func callableAnnotIsCallable(t *Type) bool {
	return t != nil && (t.Kind == KindFunc || t.Kind == KindCallable)
}

// callableBodyBlocked lists the things a higher-order body must not contain to be cloned. Each is a
// way the clone could end up answering a different program than the one that was written.
func callableBodyBlocked(r *callableRead, plan *callablePlan, fn *FuncDef) bool {
	blocked := false
	var w callableWalk
	w.visit = func(n Node, enc *FuncDef) {
		switch x := n.(type) {
		case *FuncDef, *ClassDef:
			// A nested declaration is a scope the clone would have to carry; the ADR 0245 pass is the
			// one that lifts those, and the two do not compose yet.
			blocked = true
		case *AwaitExpr, *YieldFromStmt, *YieldStmt, *Generator:
			// A call whose body produces a generator is not the same program with a name substituted.
			blocked = true
		case *Comp:
			if x.Kind == CompGenerator {
				blocked = true
			}
		case *AssignExpr, *WithStmt, *MatchStmt, *ImportStmt, *TypeAliasStmt, *ExternDecl:
			blocked = true
		case *TryStmt:
			if len(x.Finally) > 0 {
				blocked = true
			}
		case *Lambda:
			// A lambda is allowed only where the plan gave it a declaration of its own.
			if !plan.hoistsLambda(x) {
				blocked = true
			}
		}
	}
	w.stmts(fn.Body, fn)
	if blocked {
		return true
	}
	// Every statement kind must be one the pass knows, and a nested def inside the body is out.
	for _, s := range fn.Body {
		if !callableAllowedStmts[reflect.TypeOf(s).Elem().Name()] {
			return true
		}
	}
	return false
}

func (p *callablePlan) hoistsLambda(l *Lambda) bool {
	for i, h := range p.hoisted {
		if h != nil && p.hoistOrigin[i] == l {
			return true
		}
	}
	return false
}

// resolve answers "which function is this argument", or declines. A declared function, a module-level
// alias of one, or a closed lambda that can be given a declaration of its own. Anything else — a
// variable, a container, an attribute, a closure that reads its host's locals — is not a function
// this pass can name.
// callableEntryIsLayable is the one question every substituted name answers, asked in one place so the
// alias road and the declared road cannot drift: a function the plan writes into a call body must take
// its arguments positionally and must not be a parameter some other pass has already been told to call.
func callableEntryIsLayable(fd *FuncDef) error {
	if fd == nil || fd.Body == nil {
		return callableDeclinef("a function has no body to substitute")
	}
	for _, prm := range fd.Params {
		if prm.Default != nil {
			return callableDeclinef("%q has a parameter with a default this pass cannot fill", fd.Name)
		}
		if callableAnnotIsCallable(prm.Annot) {
			return callableDeclinef("%q declares a parameter to be called, which is ADR 0284's road to walk", fd.Name)
		}
	}
	return nil
}

func (p *callablePlan) resolve(r *callableRead, e Expr, enc *FuncDef) (string, error) {
	switch x := e.(type) {
	case *Name:
		if aliased, isAlias := p.aliases[x.Value]; isAlias {
			// The alias may name a function the program declared or one this pass gave a declaration to a
			// moment ago (a module-level `h = lambda x: x * 3`): the second is not in the program's own
			// definitions, so it answers its arity from the plan that minted it.
			if n, known := p.arity[aliased]; known {
				p.arity[x.Value] = n
				return aliased, nil
			}
			fd, isDef := r.fds[aliased]
			if !isDef {
				return "", callableDeclinef("%q aliases %q, which is not a declared function", x.Value, aliased)
			}
			if err := callableEntryIsLayable(fd); err != nil {
				return "", err
			}
			p.arity[aliased] = len(fd.Params)
			return aliased, nil
		}
		fd, isDef := r.fds[x.Value]
		if !isDef {
			return "", callableDeclinef("%q is not a declared function, so it has no name to substitute", x.Value)
		}
		if _, higherOrder := p.fns[x.Value]; higherOrder {
			return "", callableDeclinef("%q is itself higher-order, and a clone cannot be an entry", x.Value)
		}
		if !callableBoundOnlyByDef(r, x.Value) || len(fd.Decorators) > 0 || fd.Async {
			return "", callableDeclinef("%q is not a plain declared function", x.Value)
		}
		if err := callableEntryIsLayable(fd); err != nil {
			return "", err
		}
		p.arity[x.Value] = len(fd.Params)
		return x.Value, nil
	case *Lambda:
		if !callableLambdaIsClosed(r, x, enc) {
			return "", callableDeclinef("a lambda passed to a parameter reads that parameter's function")
		}
		body := copyASTExpr(x.Body)
		if body == nil {
			return "", callableDeclinef("a lambda could not be copied")
		}
		name, ok := p.freshName(callableDisplayNameOf(enc)+callableLambdaInfix, x)
		if !ok {
			return "", callableDeclinef("no name could be minted for a hoisted lambda")
		}
		def := &FuncDef{
			Name:   name,
			Params: append([]*Param(nil), x.Params...),
			Body:   []Stmt{&ReturnStmt{Expr: body, Src: x.Span()}},
			Src:    x.Span(),
		}
		p.hoisted = append(p.hoisted, def)
		p.hoistOrigin = append(p.hoistOrigin, x)
		p.arity[name] = len(x.Params)
		return name, nil
	default:
		return "", callableDeclinef("an argument that is not a declared function or a closed lambda")
	}
}

// callableInnerAgrees checks the body's calls of its own parameter against the entries the call
// sites pass: the same arity everywhere, and the arity each entry is declared with.
func callableInnerAgrees(plan *callablePlan, cf *callableFn) bool {
	if len(cf.inner) == 0 {
		return false
	}
	arity := cf.inner[0].args
	for _, in := range cf.inner {
		if in.args != arity {
			return false
		}
	}
	entries := map[string]bool{}
	for _, site := range cf.sites {
		for _, entry := range site.slots {
			entries[entry] = true
		}
	}
	for entry := range entries {
		if plan.arity[entry] != arity {
			return false
		}
	}
	return true
}

func callableHasKeywordArgs(args []Expr) bool {
	for _, a := range args {
		if _, isKw := a.(*KeywordArg); isKw {
			return true
		}
	}
	return false
}

func (p *callablePlan) cloneName(base, key string) (string, bool) {
	h := fnv.New32a()
	fmt.Fprintf(h, "%s|%s", base, key)
	for i := 0; i < 64; i++ {
		cand := fmt.Sprintf("%s%s%x_%d", base, callableCloneInfix, h.Sum32(), i)
		if ok, _ := p.nameIsFree(cand); ok {
			p.taken[cand] = true
			return cand, true
		}
		h.Write([]byte{byte(i)})
	}
	return "", false
}

// nodeSeed numbers each node the plan mints a name for, in the order the plan first asked.
//
// The seed has to be the node's *ordinal in this plan*, not its address: `%p` on a Go pointer is
// deliberately randomised per process (Go 1.21+), and a symbol minted from it gives one program two
// different modules across two runs — which is the determinism the golden record, the lit tests and every
// cached artifact read (ADR 0340's “the same source, the same IR”). This is the shape Gap R.162 was
// written for, and the reason the pass asks ADR 0232's rule once here rather than twice.
func (p *callablePlan) nodeSeed(n Node) int {
	if p.seeds == nil {
		p.seeds = map[Node]int{}
	}
	if v, ok := p.seeds[n]; ok {
		return v
	}
	p.seedCount++
	p.seeds[n] = p.seedCount
	return p.seedCount
}

func (p *callablePlan) freshName(base string, seed Node) (string, bool) {
	h := fnv.New32a()
	fmt.Fprintf(h, "%s|%d", base, p.nodeSeed(seed))
	for i := 0; i < 4096; i++ {
		cand := fmt.Sprintf("%s%x_%d", base, h.Sum32(), i)
		if ok, _ := p.nameIsFree(cand); ok {
			p.taken[cand] = true
			return cand, true
		}
	}
	return "", false
}

// nameIsFree asks whether a name the pass invented is available: not a name the program mentions,
// binds, or declares, and not one another clone already took. The `$` in the name means no source
// identifier can spell it, so the practical risk is a generated name colliding with another
// generated one — which is exactly what the counter and this check remove.
func (p *callablePlan) nameIsFree(name string) (bool, string) {
	if p.taken[name] || isPredeclaredName(name) || !callableLexable(name) {
		return false, name
	}
	if _, hit := p.fns[name]; hit {
		return false, name
	}
	if _, hit := p.aliases[name]; hit {
		return false, name
	}
	return true, name
}

// callableLexable is the lexer's identifier rule applied to a name the pass invented. A generated
// name the tokenizer could not read back is a name that could not appear in a traceback either, so
// it is not a name at all.
func callableLexable(s string) bool {
	if s == "" {
		return false
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c == '$' || c == '_' || (c >= '0' && c <= '9') || (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') {
			continue
		}
		return false
	}
	return (s[0] >= 'a' && s[0] <= 'z') || (s[0] >= 'A' && s[0] <= 'Z')
}

func callableCloneNamesDiffer(plan *callablePlan) bool {
	seen := map[string]bool{}
	for _, cf := range plan.order {
		for _, cl := range cf.clones {
			if seen[cl.name] {
				return false
			}
			seen[cl.name] = true
		}
	}
	return true
}

func callableHoistNamesDistinct(plan *callablePlan) bool {
	seen := map[string]bool{}
	for _, h := range plan.hoisted {
		if h == nil || seen[h.Name] {
			return false
		}
		seen[h.Name] = true
	}
	return true
}

func callableBoundOnlyByDef(r *callableRead, name string) bool {
	for _, at := range r.bound[name] {
		if fd, ok := at.(*FuncDef); !ok || fd.Name != name {
			return false
		}
	}
	return true
}

func callableBoundExactly(r *callableRead, name string, want []Node) bool {
	got := r.bound[name]
	if len(got) != len(want) {
		return false
	}
	for _, w := range want {
		found := false
		for _, g := range got {
			if g == w {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}
	return true
}

// callableLambdaIsClosed asks whether a lambda can be lifted out of the function it is written in:
// nothing it reads may be one of that function's parameters or local bindings. A lambda that reads
// its host has a closure as its value, and this pass is about names.
func callableLambdaIsClosed(r *callableRead, l *Lambda, host *FuncDef) bool {
	if host == nil {
		return true
	}
	locals := map[string]bool{}
	for _, p := range host.Params {
		locals[p.Name] = true
	}
	for _, n := range r.names {
		// A name the walk reached with host as its enclosing function is host's, unless the program
		// binds it at module level — which a `def` can read just as well as the lambda did.
		if callableEnclosedBy(r, n) == host && !isPredeclaredName(n.Value) {
			locals[n.Value] = true
		}
	}
	free := callableFreeNames(l)
	if len(free) == 0 {
		return true
	}
	for _, nm := range free {
		if locals[nm] && !callableBoundAtModuleLevel(r, nm) {
			return false
		}
	}
	return true
}

func callableEnclosedBy(r *callableRead, n *Name) *FuncDef {
	for i, nm := range r.names {
		if nm == n {
			return r.nameEncs[i]
		}
	}
	return nil
}

func callableBoundAtModuleLevel(r *callableRead, name string) bool {
	for _, at := range r.bound[name] {
		if fd, isFn := at.(*FuncDef); isFn && fd.Name == name {
			continue
		}
		if !callableBindingInsideAnyFunc(r, at) {
			return true
		}
	}
	return false
}

// callableBindingInsideAnyFunc reports whether a binding node lives inside some function body. It
// is answered by identity: the same node, reached by the same walk.
func callableBindingInsideAnyFunc(r *callableRead, at Node) bool {
	for _, fn := range r.fds {
		found := false
		w := callableWalker(func(n Node, enc *FuncDef) {
			if n == at {
				found = true
			}
		})
		w.stmts(fn.Body, fn)
		if found {
			return true
		}
	}
	return false
}

// callableLambdaIsSelfContained is the stricter question a module-level alias asks: does this lambda read
// nothing but its own parameters and the predeclared names? Hoisted out of the binding that gave it a name,
// the declaration this pass mints has no frame to read from, so a free name would be a value the pass cannot
// see at the point the call runs — and a program that says that is left to the refusal it already gets
// (ADR 0211's rule: a door that cannot answer says so).
func callableLambdaIsSelfContained(l *Lambda) bool {
	if l == nil || l.Body == nil {
		return false
	}
	for _, nm := range callableFreeNames(l) {
		if isPredeclaredName(nm) {
			continue
		}
		return false
	}
	return true
}

// callableFreeNames collects the names an expression reads. A construct's own bindings — a
// comprehension's target, a lambda's parameters — are not names the construct reads, and the set is
// seeded by the lambda's parameters for the same reason.
func callableFreeNames(l *Lambda) []string {
	out := map[string]bool{}
	bound := map[string]bool{}
	for _, p := range l.Params {
		bound[p.Name] = true
	}
	var walkNode func(n Node, b map[string]bool)
	var walkFields func(v reflect.Value, b map[string]bool)
	walkFields = func(v reflect.Value, b map[string]bool) {
		t := v.Type()
		for i := 0; i < t.NumField(); i++ {
			f := v.Field(i)
			switch f.Kind() {
			case reflect.Slice, reflect.Array:
				if !callableIsNodeInterface(f.Type().Elem()) {
					continue
				}
				for k := 0; k < f.Len(); k++ {
					if e := f.Index(k); !e.IsNil() {
						walkNode(e.Interface().(Node), b)
					}
				}
			case reflect.Interface, reflect.Ptr:
				if f.IsNil() || !callableIsNodeInterface(f.Type()) {
					continue
				}
				walkNode(f.Interface().(Node), b)
			}
		}
	}
	walkNode = func(n Node, b map[string]bool) {
		if n == nil || reflect.ValueOf(n).Kind() == reflect.Ptr && reflect.ValueOf(n).IsNil() {
			return
		}
		switch x := n.(type) {
		case *Name:
			if !b[x.Value] && !isPredeclaredName(x.Value) {
				out[x.Value] = true
			}
			return
		case *Lambda:
			inner := map[string]bool{}
			for k := range b {
				inner[k] = true
			}
			for _, p := range x.Params {
				inner[p.Name] = true
			}
			walkNode(x.Body, inner)
			return
		case *Comp:
			for _, e := range x.Elems {
				walkNode(e, b)
			}
			for _, e := range x.Keys {
				walkNode(e, b)
			}
			for _, e := range x.Vals {
				walkNode(e, b)
			}
			walkNode(x.Iter, b)
			walkNode(x.Cond, b)
			if x.ForVar != nil {
				delete(out, x.ForVar.Value)
			}
			return
		case *Generator:
			for _, e := range x.Elems {
				walkNode(e, b)
			}
			walkNode(x.Iter, b)
			walkNode(x.Cond, b)
			if x.ForVar != nil {
				delete(out, x.ForVar.Value)
			}
			return
		case *Attr:
			walkNode(x.Obj, b)
			return
		case *AssignExpr:
			walkNode(x.Value, b)
			if x.Name != nil {
				delete(out, x.Name.Value)
			}
			return
		}
		v := reflect.ValueOf(n)
		if v.Kind() != reflect.Ptr || v.IsNil() || v.Elem().Kind() != reflect.Struct {
			return
		}
		walkFields(v.Elem(), b)
	}
	walkNode(l.Body, bound)
	var names []string
	for nm := range out {
		names = append(names, nm)
	}
	sort.Strings(names)
	return names
}

func callableDisplayNameOf(fd *FuncDef) string {
	if fd == nil {
		return "module"
	}
	return fd.Name
}

// ---------------------------------------------------------------------------
// Writing the rewrite
// ---------------------------------------------------------------------------

// callableDisplayName is the name a *reader* meets a function under: the one the source wrote. A
// clone's IR symbol is generated; its traceback frame, its line table and the callable in a
// diagnostic are not (roadmap L11.7; ADR 0320's rule for a rewritten call, applied to a rewritten
// definition).
func callableDisplayName(fd *FuncDef) string {
	if fd == nil {
		return ""
	}
	if fd.CloneOf != "" {
		return fd.CloneOf
	}
	if base := callableGeneratedBase(fd.Name); base != "" {
		return base
	}
	return fd.Name
}

// callableGeneratedBase strips this pass's infix and everything after it. A hoisted lambda keeps its
// host's name, which is the closest thing in the source to what it is.
func callableGeneratedBase(name string) string {
	if i := strings.Index(name, callableLambdaInfix); i > 0 {
		return name[:i]
	}
	if i := strings.Index(name, callableCloneInfix); i > 0 {
		return name[:i]
	}
	return ""
}

// defunctionalizeCallables is the pass. GenerateIRReport calls it before anything reads the program
// that has to agree about arity, and it never mutates the tree it was handed.
func defunctionalizeCallables(prog *Program) *Program {
	out, _ := applyDefunctionalization(prog)
	return out
}

// applyDefunctionalization returns the program codegen should see, plus the pass's account of what
// it did. A decline returns the caller's program unchanged, and a nil report: the pass's absence is
// always representable, which is what makes it safe to run unconditionally.
func applyDefunctionalization(prog *Program) (*Program, *callableReport) {
	plan, err := planDefunctionalization(prog)
	if err != nil {
		return prog, nil
	}
	fresh := copyProgramAST(prog)
	if fresh == nil {
		return prog, nil
	}
	report := &callableReport{Tool: callableToolName, Pass: "defunctionalize-callables"}

	// (1) The clone bodies, each written from the source function's own body so that the only thing
	// that changes in it is the callee the pass decided on.
	var clones []*FuncDef
	for _, cf := range plan.order {
		bodyCalls, _ := callableBodyCalls(cf.def)
		for _, cl := range cf.clones {
			clone := copyFuncDefAST(cf.def)
			if clone == nil || clone.Body == nil {
				return prog, nil
			}
			clone.Name = cl.name
			clone.CloneOf = cf.def.Name
			// The two lists come from the same walk over structurally identical trees, so the k-th call
			// of the clone is the k-th call of the source body. The lengths are checked rather than
			// assumed: a call pointed at the wrong `define` is worse than no rewrite at all.
			cloneCalls, _ := callableBodyCalls(clone)
			if len(cloneCalls) != len(bodyCalls) {
				return prog, nil
			}
			for _, in := range cf.inner {
				if in.rank < 0 || in.rank >= len(cloneCalls) || in.slot < 0 || in.slot >= len(cl.key) {
					return prog, nil
				}
				callee := cloneCalls[in.rank]
				callee.Fn = &Name{Value: cl.key[in.slot], Src: callee.Fn.Span()}
			}
			// The recursion. Every call of the source function written inside this clone's copy of the body
			// is pointed at the clone itself — the entry tuple the recursion carries is the one this clone
			// was minted for — and the slot the callable came in on is filled with the same plain integer an
			// outside call site gets, because a clone reads nothing there. The rank is re-checked against
			// the callee's spelling: a call pointed at the wrong `define` is worse than no rewrite at all
			// (roadmap Gap R.267, ADR 0364).
			for _, sf := range cf.self {
				if sf.rank < 0 || sf.rank >= len(cloneCalls) {
					return prog, nil
				}
				rec := cloneCalls[sf.rank]
				if nm, isName := rec.Fn.(*Name); !isName || nm.Value != cf.def.Name {
					return prog, nil
				}
				rec.Fn = &Name{Value: cl.name, Src: rec.Fn.Span()}
				for _, pos := range cf.calledPos {
					if pos >= len(rec.Args) {
						return prog, nil
					}
					rec.Args[pos] = &IntLit{Value: 0, Text: callablePlaceholderText, Src: rec.Args[pos].Span()}
				}
				report.RecursiveSites++
			}
			clones = append(clones, clone)
			report.Clones++
		}
	}

	// (2) The call sites: pointed at the clone their arguments ask for, with the slot the clone does
	// not read filled with a plain integer, so the clone's arity is still the source's arity.
	programCalls := callableCallsOf(fresh)
	for _, cf := range plan.order {
		byKey := map[string]*callableClone{}
		for _, cl := range cf.clones {
			byKey[strings.Join(cl.key, "|")] = cl
		}
		for _, site := range cf.sites {
			if site.rank >= len(programCalls) {
				return prog, nil
			}
			node := programCalls[site.rank]
			cl, hit := byKey[strings.Join(site.slots, "|")]
			if !hit || len(node.Args) != len(cf.def.Params) {
				return prog, nil
			}
			node.Fn = &Name{Value: cl.name, Src: node.Fn.Span()}
			for _, pos := range cf.calledPos {
				if pos >= len(node.Args) {
					return prog, nil
				}
				node.Args[pos] = &IntLit{Value: 0, Text: callablePlaceholderText, Src: node.Args[pos].Span()}
			}
			report.RewrittenCallNum++
		}
	}

	// (3a) The lambda arguments the plan gave declarations to: the call's argument becomes the name, and
	// the door underneath it — ADR 0340's key road, a builtin's ordinary operand — sees the callable it
	// already knows how to call (roadmap L11.7, ADR 0345).
	for _, h := range plan.argHoists {
		if h.rank >= len(programCalls) {
			return prog, nil
		}
		node := programCalls[h.rank]
		if h.pos < 0 || h.pos >= len(node.Args) {
			return prog, nil
		}
		at := node.Args[h.pos]
		repl := &Name{Value: h.name, Src: at.Span()}
		if kw, isKw := at.(*KeywordArg); isKw {
			redo := *kw
			redo.Value = repl
			node.Args[h.pos] = &redo
			continue
		}
		node.Args[h.pos] = repl
		report.RewrittenCallNum++
	}

	// (3b) The aliases: every call of an alias's name is pointed at the function it aliases, and the
	// binding that gave it no value is taken out. Deleted rather than rewritten to `g = 0`: a binding
	// left behind would leave the name `g` occurring in the program — as the target of an assignment —
	// and the invariant below reads an occurrence of a function's name that is not a call as the
	// program asking for a function value. Nothing reads the deleted binding either, and the invariant
	// is what says so, after the fact, on the tree that was actually produced.
	var drop []Stmt
	for _, alias := range callableSortedKeys(plan.aliases) {
		target := plan.aliases[alias]
		for _, c := range programCalls {
			if nm, ok := c.Fn.(*Name); ok && nm.Value == alias {
				nm.Value = target
			}
		}
		for _, s := range fresh.Stmts {
			if a, ok := s.(*AssignStmt); ok {
				if tgt, ok := a.Target.(*Name); ok && tgt.Value == alias {
					drop = append(drop, a)
				}
			}
		}
	}
	if len(drop) > 0 {
		kept := make([]Stmt, 0, len(fresh.Stmts))
		for _, s := range fresh.Stmts {
			dropped := false
			for _, d := range drop {
				if s == d {
					dropped = true
					break
				}
			}
			if !dropped {
				kept = append(kept, s)
			}
		}
		fresh.Stmts = kept
	}

	// (4) The declarations the pass generated, and the mark on the ones it replaced: a higher-order
	// body asks the question its clones answer, so emitting it would put an unbuildable call in the
	// module — while its name stays behind for `xs = [apply]` to be refused against.
	for _, s := range fresh.Stmts {
		if fn, ok := s.(*FuncDef); ok {
			if _, rewritten := plan.fns[fn.Name]; rewritten {
				fn.SkipEmit = true
			}
		}
	}
	freshStmts := make([]Stmt, 0, len(fresh.Stmts)+len(clones)+len(plan.hoisted))
	freshStmts = append(freshStmts, fresh.Stmts...)
	for _, h := range plan.hoisted {
		if h != nil {
			freshStmts = append(freshStmts, h)
			report.HoistedLambdas++
		}
	}
	for _, cl := range clones {
		freshStmts = append(freshStmts, cl)
	}
	fresh.Stmts = freshStmts

	// (5) The invariant, checked on the program the pass produced. Everything above is an argument
	// for why the rewrite should be sound; this is the soundness statement itself, read back off the
	// result, in the same words the pass uses to describe itself:
	//
	//	the name of a declared function occurs in the rewritten program nowhere except as the
	//	thing a call evaluates
	//
	// A function name left in any other position is a function being used as a value, which is the
	// thing this pass does not do -- and a program that does it is handed back untouched, with its
	// refusal intact. The check is not redundant with the reading above: the reading decides what to
	// rewrite, this decides whether what was rewritten may be run.
	if !callableNoFunctionValuesLeft(fresh, plan) {
		return prog, nil
	}
	for _, cf := range plan.order {
		report.HigherOrderFunc = append(report.HigherOrderFunc, cf.def.Name)
	}
	report.Rewritten = true
	report.Hash = callablePlanHash(report)
	return fresh, report
}

const callablePlaceholderText = "0"

// callableNoFunctionValuesLeft reads the rewritten program back and asks the one question the pass
// is built on: does the name of a function appear anywhere but as the callee of a call? Every name
// the program binds that names a `def`, an alias the pass retargeted, or a declaration the pass
// removed is looked for by node identity among the occurrences that are not a call's callee; one
// such occurrence is a function reaching the program as a value, and the pass hands the whole
// program back unchanged rather than compile a half-rewrite.
//
// This is also where the pass catches what its own reading could not see: a function name that reaches
// an attribute (`o.f`), a match pattern, a comprehension target, an `as` clause, or any other
// position the walk enumerates names in but that is not a call. Those are not shapes the rewrite
// makes sense of, and the answer to a shape the pass does not make sense of is the refusal the
// program already got.
func callableNoFunctionValuesLeft(prog *Program, plan *callablePlan) bool {
	if prog == nil {
		return false
	}
	functions := map[string]bool{}
	for name := range plan.declared {
		functions[name] = true
	}
	for alias := range plan.aliases {
		functions[alias] = true
	}
	for _, cf := range plan.order {
		functions[cf.def.Name] = true
	}
	// A function the pass rewrote has no callers left: every call site was pointed at a clone, so a
	// single occurrence of its name anywhere in the result means a call the reading did not account
	// for — a higher-order function called from inside a lambda being hoisted, or from inside a body
	// the pass never numbered. Handing that program to codegen would emit a `call` to a `define` the
	// pass marked unemittable, which is a link-time mystery rather than a refusal.
	gone := map[string]bool{}
	for _, cf := range plan.order {
		gone[cf.def.Name] = true
		delete(functions, cf.def.Name)
	}
	// The program the pass produced, minus the definitions the pass took out of it: a body marked
	// SkipEmit is never emitted, so a name inside it cannot reach the module — while every statement that
	// IS emitted, each clone included, is still read. A recursion is exactly the shape this distinction is
	// for: the source body names its own function, and the clone that replaces it calls itself (roadmap
	// Gap R.267, ADR 0364).
	check := callableReadProgram(emittedStmtsOf(prog))
	for _, n := range check.names {
		if gone[n.Value] {
			return false
		}
		if !functions[n.Value] {
			continue
		}
		if !check.calleeNames[n] {
			return false
		}
	}
	return true
}

// callableMutualHigherOrderDecline is the sentence for the recursion between TWO higher-order bodies:
//
//	def a(n, f):            #   a calls f, and hands f to b
//	    return f(n) + b(n - 1, f)
//	def b(n, f):            #   b calls f too, and hands it back to a
//	    return f(n) + a(n - 1, f)
//
// Each body's clone would need the other's entry tuple, which is the same fixed point the clone road
// finds for one function and has no way to find across two. The question is asked of the pair, never
// of one body: `f` reaching a plain function is a use as a value the pass has always declined, and
// saying "recursion" about that would blame a shape the program never wrote (roadmap Gap R.267,
// ADR 0364).
func callableMutualHigherOrderDecline(r *callableRead) string {
	if r == nil {
		return ""
	}
	// The functions are asked in the order the source names them, never in map order: which of a pair is
	// named in the sentence has to be the same answer twice in a row (ADR 0345's determinism rule for
	// anything the pass says about a program).
	names := make([]string, 0, len(r.fds))
	for name := range r.fds {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		fn := r.fds[name]
		if fn == nil || fn.Body == nil || len(fn.Decorators) > 0 || fn.Async {
			continue
		}
		for i, p := range fn.Params {
			if p == nil || isPredeclaredName(p.Name) || !bodyCallsParam(r, fn, i) {
				continue
			}
			sentence := ""
			var w callableWalk
			w.visit = func(n Node, enc *FuncDef) {
				if sentence != "" {
					return
				}
				c, isCall := n.(*Call)
				if !isCall || enc != fn || w.inLambda > 0 {
					return
				}
				callee, isName := c.Fn.(*Name)
				if !isName || callee.Value == fn.Name {
					return
				}
				g, declared := r.fds[callee.Value]
				if !declared || g == fn || i >= len(g.Params) || g.Params[i] == nil || i >= len(c.Args) {
					return
				}
				arg, isArgName := c.Args[i].(*Name)
				if !isArgName || arg.Value != p.Name || !bodyCallsParam(r, g, i) {
					return
				}
				sentence = fmt.Sprintf("%q calls its own parameter %q and hands it to %q at line %d, whose body calls it back: a recursion between two higher-order functions is a call neither clone can answer",
					fn.Name, p.Name, callee.Value, c.Span().Line)
			}
			w.stmts(fn.Body, fn)
			if sentence != "" {
				return sentence
			}
		}
	}
	return ""
}

// bodyCallsParam asks whether a function's body ever calls its i-th parameter — the callee position, not
// merely a mention of it, which is what makes "the two bodies call it together" a claim about a pair
// rather than about two programs that never met.
func bodyCallsParam(r *callableRead, fn *FuncDef, i int) bool {
	if fn == nil || i < 0 || i >= len(fn.Params) || fn.Params[i] == nil {
		return false
	}
	for k, n := range r.names {
		if r.nameEncs[k] == fn && n.Value == fn.Params[i].Name && r.calleeNames[n] {
			return true
		}
	}
	return false
}

// emittedStmtsOf is the program a run actually consists of: every statement, minus the definitions the
// pass took out of it. A `SkipEmit` body is not emitted, so it is not part of the program codegen reads
// — which is why the recursion it still spells (`fact(n - 1, f)` in the source body a clone replaced)
// does not read as a leftover use of a function as a value.
func emittedStmtsOf(prog *Program) *Program {
	if prog == nil {
		return nil
	}
	kept := make([]Stmt, 0, len(prog.Stmts))
	for _, s := range prog.Stmts {
		if fd, ok := s.(*FuncDef); ok && fd.SkipEmit {
			continue
		}
		kept = append(kept, s)
	}
	return &Program{Src: prog.Src, Stmts: kept}
}

func callableSortedKeys(m map[string]string) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// callableBodyCalls returns a function body's calls in its own preorder, with the ranks.
func callableBodyCalls(fn *FuncDef) ([]*Call, []int) {
	var calls []*Call
	var ranks []int
	if fn == nil || fn.Body == nil {
		return calls, ranks
	}
	rank := 0
	var w callableWalk
	w.visit = func(n Node, enc *FuncDef) {
		if c, ok := n.(*Call); ok {
			calls = append(calls, c)
			ranks = append(ranks, rank)
		}
		rank++
	}
	w.stmts(fn.Body, fn)
	return calls, ranks
}

func callableCallsOf(p *Program) []*Call {
	var out []*Call
	var w callableWalk
	w.visit = func(n Node, enc *FuncDef) {
		if c, ok := n.(*Call); ok {
			out = append(out, c)
		}
	}
	if p != nil {
		w.stmts(p.Stmts, nil)
	}
	return out
}

// nameIsOnlyCalled reports whether every occurrence of a program-level name is a callee. A function
// that is also printed, stored, returned or passed keeps its refusal, which is what leaves
// `xs = [apply]`, `d = {"k": apply}` and `print(apply)` saying what they said before this pass
// existed.
func (r *callableRead) nameIsOnlyCalled(name string) bool {
	for _, n := range r.occurrencesOf(name) {
		if !r.calleeNames[n] {
			return false
		}
	}
	return true
}

// ---------------------------------------------------------------------------
// The copy
// ---------------------------------------------------------------------------

// copyProgramAST re-builds a program node-for-node. The pass rewrites the copy; the caller's tree is
// what --emit-ast prints, what gusty fmt renders and what a diagnostic quotes, and rewriting the
// program a person wrote into one they did not write would make all three liars.
func copyProgramAST(prog *Program) *Program {
	if prog == nil {
		return nil
	}
	out := &Program{Src: prog.Src}
	for _, s := range prog.Stmts {
		c := copyASTNode(s)
		if c == nil {
			return nil
		}
		out.Stmts = append(out.Stmts, c.(Stmt))
	}
	return out
}

func copyFuncDefAST(fd *FuncDef) *FuncDef {
	c := copyASTNode(fd)
	if c == nil {
		return nil
	}
	return c.(*FuncDef)
}

func copyASTNode(n Node) Node {
	v := reflect.ValueOf(n)
	if !v.IsValid() || v.Kind() != reflect.Ptr || v.IsNil() {
		return nil
	}
	c := copyASTValue(v)
	if !c.IsValid() {
		return nil
	}
	return c.Interface().(Node)
}

func copyASTExpr(e Expr) Expr {
	v := reflect.ValueOf(e)
	if !v.IsValid() || v.Kind() != reflect.Ptr || v.IsNil() {
		return nil
	}
	c := copyASTValue(v)
	if !c.IsValid() {
		return nil
	}
	return c.Interface().(Expr)
}

func copyASTValue(v reflect.Value) reflect.Value {
	switch v.Kind() {
	case reflect.Ptr:
		if v.IsNil() {
			return reflect.Zero(v.Type())
		}
		out := reflect.New(v.Type().Elem())
		out.Elem().Set(copyASTValue(v.Elem()))
		return out
	case reflect.Interface:
		if v.IsNil() {
			return reflect.Zero(v.Type())
		}
		c := copyASTValue(v.Elem())
		if !c.IsValid() {
			return reflect.Zero(v.Type())
		}
		out := reflect.New(v.Type()).Elem()
		out.Set(c)
		return out
	case reflect.Struct:
		out := reflect.New(v.Type()).Elem()
		for i := 0; i < v.NumField(); i++ {
			f := out.Field(i)
			if !f.CanSet() {
				continue
			}
			f.Set(copyASTValue(v.Field(i)))
		}
		return out
	case reflect.Slice:
		if v.IsNil() {
			return reflect.Zero(v.Type())
		}
		out := reflect.MakeSlice(v.Type(), v.Len(), v.Len())
		for i := 0; i < v.Len(); i++ {
			out.Index(i).Set(copyASTValue(v.Index(i)))
		}
		return out
	case reflect.Array:
		out := reflect.New(v.Type()).Elem()
		for i := 0; i < v.Len(); i++ {
			out.Index(i).Set(copyASTValue(v.Index(i)))
		}
		return out
	case reflect.Map:
		if v.IsNil() {
			return reflect.Zero(v.Type())
		}
		out := reflect.MakeMapWithSize(v.Type(), v.Len())
		iter := v.MapRange()
		for iter.Next() {
			out.SetMapIndex(iter.Key(), copyASTValue(iter.Value()))
		}
		return out
	default:
		return v
	}
}

// ---------------------------------------------------------------------------
// The tool's own proof (Gap R.94's pattern, L11.7's subject)
// ---------------------------------------------------------------------------

const callableToolName = "callable-values"

// CallablesJSON is the pass's machine path: the account it keeps of its own work, handed to a caller that
// wants to know WHY a program compiled rather than guess from the IR (`gustyc --callables <src>`, described
// by the schema's definitions.callablePassReport). It answers the three questions a reader of this row has:
// did the pass rewrite this program at all, what did it turn each higher-order function into, and — when it
// did nothing — which shape it could not prove. There is no "pass off" mode to compare against: the pass is
// part of the one backend (ADR 0302), and the claim the tests make is that the answers come from the rewrite
// — the probe table in callable_value_test.go runs the programs on RunSource and the refusal table runs them
// through Compile, so an answer and a refusal are each witnessed where they are decided.
//
// A refusal here is not a failure of the command: a program the pass leaves alone is reported with
// `rewritten: false` and the decline in `refused`, which is the honest answer and the one an agent can
// act on (ADR 0166's rule for a refusal, applied to a report). The `symbols` list is the IR names the
// pass minted, so a caller reading `--emit-llvm` can tell a clone's `define` from the program's own.
func CallablesJSON(src string) (string, error) {
	doc := callableReport{Tool: callableToolName, Pass: "defunctionalize-callables", Hash: "none"}
	prog, err := parseProgram(src)
	if err != nil {
		// A source that cannot be read is not a program the pass declined: the tool was handed something that is
		// not gusty, which is the exit-code contract's compile-error case (ADR 0006) and not the pass's own
		// verdict. A decline, by contrast, is a successful answer to the question and prints its document.
		return "", err
	}
	plan, perr := planDefunctionalization(prog)
	if perr != nil {
		doc.Refused = append(doc.Refused, perr.Error())
		return marshalCallableReport(doc)
	}
	for _, cf := range plan.order {
		for _, cl := range cf.clones {
			doc.Symbols = append(doc.Symbols, cl.name)
		}
	}
	for _, h := range plan.hoisted {
		if h != nil {
			doc.Symbols = append(doc.Symbols, h.Name)
		}
	}
	sort.Strings(doc.Symbols)
	_, rep := applyDefunctionalization(prog)
	if rep == nil {
		doc.Refused = append(doc.Refused, "the rewrite was withdrawn: the program it produced still used a function as a value")
		return marshalCallableReport(doc)
	}
	rep.Symbols = doc.Symbols
	return marshalCallableReport(*rep)
}

func marshalCallableReport(rep callableReport) (string, error) {
	if rep.HigherOrderFunc == nil {
		rep.HigherOrderFunc = []string{}
	}
	if rep.Symbols == nil {
		rep.Symbols = []string{}
	}
	if rep.Refused == nil {
		rep.Refused = []string{}
	}
	b, err := json.MarshalIndent(rep, "", "  ")
	if err != nil {
		return "", err
	}
	return string(b), nil
}

func callablePlanHash(rep *callableReport) string {
	h := fnv.New64a()
	if rep != nil {
		fmt.Fprintf(h, "%s|%d|%d|%d|%d|%s", rep.Pass, rep.Clones, rep.HoistedLambdas, rep.RewrittenCallNum,
			rep.RecursiveSites, strings.Join(rep.HigherOrderFunc, ","))
	}
	return fmt.Sprintf("%016x", h.Sum64())
}
