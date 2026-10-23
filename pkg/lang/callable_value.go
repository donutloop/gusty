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
	"sync"
)

// callableCloneInfix and callableLambdaInfix mark the names this pass mints. The infix is `$`
// because the lexer's identifier rule is `[A-Za-z0-9_]`: a generated name cannot be written in
// source, so the only collision left to check is with a name the pass itself already handed out —
// which is checked, below, against every name the program mentions. The shape follows ADR 0245's
// `env$copy` and ADR 0238's `x$dict$0`.
const (
	callableCloneInfix  = "$callable$"
	callableLambdaInfix = "$lambda$"
	// callableForwardInfix marks the PARAMETER this pass mints for a word a clone forwards to its entry.
	// The word used to be spelled with the captured name, which is the name the higher-order body was
	// allowed to have taken for itself; the minted spelling is the difference between a clone with two
	// slots under one name and a clone whose forwarded word cannot be answered by the body's own read
	// (roadmap L11.7 · Gap R.375, ADR 0397).
	callableForwardInfix = "$cap$"
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
	RecursiveSites int `json:"recursive_sites"`
	// KeyArguments counts the `key=` arguments the pass pointed at the callable they name — the hook
	// positions the door calls and the source never wrote as a call (roadmap L11.7, Gap R.281).
	KeyArguments int `json:"key_arguments"`
	// OmittedArguments counts the parameters the applications LEFT OUT and the pass read off the body's
	// prototype: the entry tuple is the list of functions the BODY asks for, so a call may name fewer
	// arguments than the body has parameters, each of which the pass reads from the parameter instead of
	// from the argument (roadmap L11.7 · Gap R.329, ADR 0371). It is the field that separates
	// `(lambda f, y=10: f(y))(lambda x: x + 1)` from its own twin `(…)(lambda x: x * 2, 5)`, which are
	// otherwise the same clone.
	OmittedArguments int `json:"omitted_arguments"`
	// CaptureParameters counts the capture parameters this pass wrote onto the declarations it minted: a
	// clone of an applied `lambda` that reads the frame around its application gains one parameter per name
	// its body is free in, and the call road fills each from the caller's own reading. Zero means every clone
	// this program needed was self-contained — the shape ADR 0370 cloned — while a non-zero count is the
	// witness that a body that calls its own parameter ALSO carried a capture (roadmap L11.7 · Gap R.334,
	// ADR 0372).
	CaptureParameters int `json:"capture_parameters"`
	// HigherOrderLambdas counts the lambdas that were cloned: a lambda applied where it is written whose
	// body calls one of its own parameters is a higher-order body with no name in the source, and the pass
	// gives it a declaration, clones it once per tuple its called parameters are asked to be, and points
	// the application at the clone (roadmap L11.7, Gap R.265). Zero means the program wrote no such lambda;
	// a program that wrote one and was declined says so in `refused`.
	HigherOrderLambdas int      `json:"higher_order_lambdas"`
	HigherOrderFunc    []string `json:"higher_order_functions"`
	// CarriersDeclined counts, by carrier name, the call sites the reader SAW and the rewrite left exactly
	// where the program wrote them. The key set is closed and small — `except_handler` is the only name
	// today, and `match_arm`/`interpolation_field` are measured answers rather than declines — so a script
	// branches on the key rather than on prose, and `refused` carries the sentence with the line and the
	// roadmap row (roadmap Gap R.383 for the read, Gap R.389 for the restraint).
	CarriersDeclined map[string]int `json:"carriers_declined"`
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
	// callCarriers is the parallel record of the CARRIER each call was written inside — "", or one of the
	// three names the AST carries expressions in without calling them nodes. The reader must see a call
	// wherever it is written (roadmap Gap R.383); the rewrite leaves some of those positions alone
	// (roadmap Gap R.389), and this is the field that says which without re-reading the tree.
	callCarriers []string

	names    []*Name    // every name occurrence, in program preorder
	nameEncs []*FuncDef // the function each name occurrence is written in
	// inLambda marks, beside each call, whether the call is written inside a `lambda`. Such a call is
	// the closure road's program, and this pass does not rewrite through that boundary.
	inLambda []bool

	// calleeNames holds the *Name nodes that sit in the function slot of a call. "Only ever called"
	// is then a test of node identity, not of spelling: `f(1)` counts as a call, `print(f)` does
	// not, and a same-named local rebinds nothing silently.
	calleeNames map[*Name]bool

	// keySlots holds the *Name nodes that stand in the `key=` slot of one of the ordering builtins.
	// That position is a call the SOURCE never wrote and the AST does not show: the door calls the key
	// once per element of the container (ADR 0340). A name written there is therefore being CALLED,
	// not read, which is the difference between `sorted(xs, key=neg)` answering and being handed back
	// as a program that "uses a function as a value" (roadmap L11.7, Gap R.281; the same position
	// ADR 0363 taught the parameter census to read).
	keySlots map[*Name]bool

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
	// onPath holds the nodes the walk is currently inside; depth counts the frames it has opened. A
	// rewritten AST can point back at a node that encloses the very call naming it — a self-call whose
	// callee was pointed at the declaration that owns the body is that shape — and a walk that trusts
	// the tree to be a tree dies in the runtime instead of in a sentence the program could be shown.
	// The guard is a path set plus a depth cap: descending is refused only for a node this path is
	// already inside, so a shared (acyclic) node is still visited on every route to it and the k-th-call
	// ordinal the pass rewrites by is untouched, while a cycle is cut where it closes.
	onPath map[uintptr]bool
	depth  int
	// carrierStack names the AST carriers this path is inside — the three the AST stores expressions in
	// without calling them nodes: an `except` handler, an interpolation's field, a `match` arm. A READER
	// wants all of them (roadmap Gap R.383); a REWRITE does not, because moving a call out of a carrier
	// moves it out of the frame that owns something the carrier was built to do — a handler's trap catch is
	// the measured case (roadmap Gap R.389). The stack is per-path and save/restored, so a `match` arm
	// written inside a handler is still standing in the handler.
	carrierStack []string
}

// The three carriers, named as the pass says them. The names are data because the machine path reports
// them and a test asserts each one is reachable; the AST types they come from are asserted in
// `callableCarrierKindOf`, which is the only place a Go type name is compared.
const (
	callableCarrierExcept = "except_handler"
	callableCarrierInterp = "interpolation_field"
	callableCarrierMatch  = "match_arm"
)

// callableRewriteDeclinedCarriers is the measured half of the carrier rule: a position the reader now
// sees and the rewrite must still leave alone. `except_handler` is there because cloning a `sorted` key
// written in a handler moves the call out of the frame whose trap catch the handler is registered on,
// and the program that used to print `caught` dies at exit 3 with its own `ZeroDivisionError` unhandled
// — a refusal (exit 1) beats that, which is ADR 0166's ordering. `match_arm` and `interpolation_field`
// are deliberately NOT here: both were measured answering CPython's bytes at the CLI once the reader
// could see them.
var callableRewriteDeclinedCarriers = map[string]bool{
	callableCarrierExcept: true,
}

// callableCarrierKindOf names the carrier a non-node struct value is, or "" for one the pass has no
// opinion about. This is the ONLY place a carrier is recognised, so the reader's completeness and the
// rewrite's restraint are decided by one table rather than by two guesses that drift apart.
func callableCarrierKindOf(t reflect.Type) string {
	if t == nil {
		return ""
	}
	switch t.Name() {
	case "ExceptClause":
		return callableCarrierExcept
	case "FStringPart":
		return callableCarrierInterp
	case "MatchCase":
		return callableCarrierMatch
	}
	return ""
}

// inCarrier reports whether this path stands inside a carrier of the given kind.
func (w *callableWalk) inCarrier(kind string) bool {
	for _, k := range w.carrierStack {
		if k == kind {
			return true
		}
	}
	return false
}

// declinedCarrier names the carrier on this path that a REWRITE must not cross, or "" when the path
// stands in none. It walks the whole stack rather than reading the innermost frame, because a `match`
// arm written inside an `except` handler is still standing in the handler, and the position that owns
// the trap catch is the one the rewrite would break.
func (w *callableWalk) declinedCarrier() string {
	for _, k := range w.carrierStack {
		if callableRewriteDeclinedCarriers[k] {
			return k
		}
	}
	return ""
}

// callableWalkDepthCap is the belt beside the path set: no source the parser writes nests expressions
// thousands deep, so a walk that reaches it is walking something a tree is not.
const callableWalkDepthCap = 2000

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
	p := v.Pointer()
	if w.onPath == nil {
		w.onPath = map[uintptr]bool{}
	}
	if w.onPath[p] || w.depth >= callableWalkDepthCap {
		return
	}
	w.onPath[p] = true
	w.depth++
	defer func() { delete(w.onPath, p); w.depth-- }()
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
				// A CARRIER slice: the AST stores the expressions of a `match` arm, an `except` body and
				// an interpolation's field in `[]*MatchCase`, `[]*ExceptClause` and `[]FStringPart`, and none
				// of those three types is a node — none has a `Span()` to answer. Skipping the slice because
				// its element type is not a `Node` is what made a `lambda` written in one of those three
				// places invisible to every question this pass asks, so the same statement compiled beside
				// the arm and was refused inside it (roadmap Gap R.383, ADR 0403). The elements are not
				// VISITED — they are not nodes, and a visitor that switches on node kinds must not be handed
				// a value that answers none — they are DESCENDED, which is all the walk ever wanted.
				for k := 0; k < f.Len(); k++ {
					w.carrier(f.Index(k), enc)
				}
				continue
			}
			for k := 0; k < f.Len(); k++ {
				if e := f.Index(k); !e.IsNil() {
					w.node(e.Interface().(Node), enc)
				}
			}
		case reflect.Interface, reflect.Ptr:
			if f.IsNil() {
				continue
			}
			if !callableIsNodeInterface(f.Type()) {
				w.carrier(f, enc)
				continue
			}
			w.node(f.Interface().(Node), enc)
		}
	}
}

// carrier descends through one value the AST does not call a node — a `*MatchCase`, an `*ExceptClause`,
// a `FStringPart`, or a non-node field of one of those — looking for the nodes it carries. The depth
// cap and the per-path guard of `node` still apply through the fields call, so a malformed tree costs
// a declined rewrite rather than a hung compile, and nothing here ever calls `visit`: a carrier has no
// node identity to report.
func (w *callableWalk) carrier(v reflect.Value, enc *FuncDef) {
	for v.Kind() == reflect.Interface || v.Kind() == reflect.Pointer {
		if v.IsNil() {
			return
		}
		v = v.Elem()
	}
	if v.Kind() != reflect.Struct || !callableCarrierCarriesNodes(v.Type()) {
		return
	}
	if w.depth >= callableWalkDepthCap {
		return
	}
	w.depth++
	defer func() { w.depth-- }()
	if kind := callableCarrierKindOf(v.Type()); kind != "" {
		w.carrierStack = append(w.carrierStack, kind)
		defer func() { w.carrierStack = w.carrierStack[:len(w.carrierStack)-1] }()
	}
	w.fields(v, enc)
}

var callableNodeInterface = reflect.TypeOf((*Node)(nil)).Elem()

// callableCarrierCache memoises the one question worth asking about a non-node field type: can a value
// of this type carry an AST node at all? `Span`, `*Type`, `string` and `Diagnostic` cannot, which is the
// same fact `callableIsNodeInterface` was protecting — the walk must not wander into the annotation
// grammar — now asked of a STRUCT type instead of of a field's interface type, and answered once per
// type rather than once per node.
var callableCarrierCache sync.Map

func callableCarrierCarriesNodes(t reflect.Type) bool {
	if t == nil {
		return false
	}
	if v, ok := callableCarrierCache.Load(t); ok {
		return v.(bool)
	}
	r := callableCarrierCarriesNodesGo(t, map[reflect.Type]bool{})
	callableCarrierCache.Store(t, r)
	return r
}

// callableCarrierCarriesNodesGo is the recursive half. The `seen` set is per QUESTION, which is what
// makes a self-referential type answer in bounded time: `*Type` reaches `*Type` and stops, rather than
// being the infinite descent the annotation grammar would otherwise hand the walk.
func callableCarrierCarriesNodesGo(t reflect.Type, seen map[reflect.Type]bool) bool {
	if t == nil || seen[t] {
		return false
	}
	seen[t] = true
	switch t.Kind() {
	case reflect.Pointer:
		if t.Implements(callableNodeInterface) {
			return true
		}
		return callableCarrierCarriesNodesGo(t.Elem(), seen)
	case reflect.Interface:
		// An EMPTY interface cannot be answered statically — `any` may hold anything — so the walk
		// declines it here and asks the value dynamically, which is what `carrier` does for the field's
		// own dynamic type. A node interface is the only static yes.
		return t.Implements(callableNodeInterface)
	case reflect.Slice, reflect.Array:
		return callableCarrierCarriesNodesGo(t.Elem(), seen)
	case reflect.Map:
		return callableCarrierCarriesNodesGo(t.Elem(), seen)
	case reflect.Struct:
		for i := 0; i < t.NumField(); i++ {
			if !t.Field(i).IsExported() {
				continue
			}
			if callableCarrierCarriesNodesGo(t.Field(i).Type, seen) {
				return true
			}
		}
	}
	return false
}

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
			// The carrier this call was written inside, asked at the same moment and stored beside it, so a
			// rewrite decision never has to guess the position from a later look at the tree (roadmap
			// Gap R.383 for the read, Gap R.389 for the restraint).
			r.callCarriers = append(r.callCarriers, w.declinedCarrier())
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
	// The hook positions, asked once the program's own functions are known — `def sorted(...)` is the
	// program's function and its `key=` is an ordinary parameter, so the question needs the table.
	r.keySlots = map[*Name]bool{}
	for _, c := range r.calls {
		if _, nm, ok := callableKeySlotOf(r, c); ok {
			r.keySlots[nm] = true
		}
	}
	return r
}

// callableKeySlotOf reads a call the way its door reads it and answers which argument stands in the
// `key=` slot, as a Name. It is the SAME reading the door and the parameter census use —
// `keyDoorOptions` — so the three cannot disagree about which call has a key and which does not
// (ADR 0233's rule against a second answer for one construct). A call whose callee is the program's
// own `sorted`/`min`/`max`, one whose options the door would refuse or raise on, and one with no key
// at all all answer "no slot" (roadmap L11.7, Gap R.281).
func callableKeySlotOf(r *callableRead, c *Call) (int, *Name, bool) {
	fn, isName := c.Fn.(*Name)
	if !isName || !isPredeclaredName(fn.Value) {
		return 0, nil, false
	}
	if _, user := r.fds[fn.Value]; user {
		return 0, nil, false
	}
	_, keyExpr, ok := keyDoorOptions(fn.Value, c.Args)
	if !ok || keyExpr == nil {
		return 0, nil, false
	}
	nm, isKey := keyExpr.(*Name)
	if !isKey {
		return 0, nil, false
	}
	for i, a := range c.Args {
		if kw, isKw := a.(*KeywordArg); isKw && kw.Name == "key" && kw.Value == keyExpr {
			return i, nm, true
		}
	}
	return 0, nil, false
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
		for _, t := range x.compTargets() {
			r.bindName(t.Value, x)
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
			sc.bad = callableHasKeywordArgs(c.Args) || len(c.Args) > len(fn.Params)
			if !sc.bad {
				sc.slots = make([]bool, len(fn.Params))
				for i := range c.Args {
					if i >= len(fn.Params) {
						continue
					}
					an, isArgName := c.Args[i].(*Name)
					sc.slots[i] = isArgName && an.Value == fn.Params[i].Name
				}
			}
			out = append(out, sc)
		}
		rank++
	}
	w.stmts(fn.Body, fn)
	return out
}

// callableSelfCallOmitsRequired reports whether any recursive call leaves out a required parameter.
// That is the reference's missing-argument TypeError, not a hole in the clone's entry tuple. Registering
// the body anyway would let the pass rewrite the call into a placeholder and answer a program CPython
// never runs (roadmap Gap R.335, ADR 0273's rule that a door may decide only what the AST states).
func callableSelfCallOmitsRequired(fn *FuncDef, positions []int) bool {
	if fn == nil {
		return false
	}
	for _, sc := range callableSelfCallsOf(fn) {
		if sc.inLambda || callableHasKeywordArgs(sc.args) || len(sc.args) > len(fn.Params) {
			continue
		}
		for pos := len(sc.args); pos < len(fn.Params); pos++ {
			if fn.Params[pos] == nil || fn.Params[pos].Default == nil {
				return true
			}
		}
	}
	return false
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
			return fmt.Sprintf("%q recurses at line %d with %s; a clone reads the recursion's entry tuple positionally", fn.Name, sc.line, callableSelfCallArityProblem(fn, sc))
		}
	}
	return ""
}

// callableSelfCallArityProblem names the one structural fact a self call may never have: keyword
// arguments and too many positional arguments. A missing argument is no longer one — the clone can hand
// the omitted called slot its own entry (roadmap Gap R.335).
func callableSelfCallArityProblem(fn *FuncDef, sc *callableSelfCall) string {
	if sc == nil {
		return "no call"
	}
	if callableHasKeywordArgs(sc.args) {
		return "a keyword argument"
	}
	if fn != nil && len(sc.args) > len(fn.Params) {
		return "more arguments than the body has parameters"
	}
	return "a shape"
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
	// fromLambda is the lambda whose application this higher-order body came from, nil for a `def` the
	// program wrote. A lambda that is applied where it is written and calls one of its own parameters is
	// a higher-order body with no name in the source; the pass gives it a declaration, clones it once per
	// tuple its called parameters are asked to be, and points the application at the clone (roadmap
	// L11.7 · Gap R.265). The declaration itself is never emitted — only its clones are.
	fromLambda *Lambda
	// self is every call of this function written inside its own body, which each of its clones answers
	// by calling itself (roadmap Gap R.267, ADR 0364).
	self   []*callableSelfSite
	clones []*callableClone
	// srcParams is how many parameters the PROGRAM wrote for this body. A clone of an applied `lambda` that
	// reads the enclosing frame carries one more parameter per captured name (the closure road's rule, moved
	// onto the node the pass minted), and those are not positions an application may name: the audit that
	// decides whether a call fills the body is read against the source's parameters alone (roadmap L11.7 ·
	// Gap R.334, ADR 0372).
	srcParams int
}

// callableSite is a call of a higher-order function from outside every higher-order body.
type callableSite struct {
	rank  int      // index among all calls in the program's preorder
	slots []string // which function each called parameter is asked to be, in calledPos order
	// grow holds the arguments the rewritten call must gain so the clone is entered positionally up to
	// the last parameter it calls: the placeholder word for a called position the application left out
	// (the clone calls the entry by name and reads nothing there), a copy of the default's own
	// expression for a position in between that the body reads (roadmap L11.7 · Gap R.329, ADR 0371).
	grow []Expr
	// omitted is how many parameters the application left out altogether — every one of them read from the
	// parameter's own Default by the audit, whether or not the rewritten call needed a word written there.
	// It is the number the account reports (roadmap Gap R.329, ADR 0371).
	omitted int
	// args is the unused arity-audit placeholder the `def` road never needed.
	args []int
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
	// args records the arguments the source call wrote. An omitted called slot inherits the enclosing
	// clone's entry; a written one may name the parameter or repeat the entry's own expression
	// (roadmap Gap R.335).
	args []Expr
}

type callableClone struct {
	key  []string
	name string
	fn   *callableFn
	// forwarded is the words this clone supplies to its entries: the union of the capture parameters the
	// entries in this tuple were minted beside. The clone is the frame that OWNS them — the call site that
	// picked this clone fills them from the frame the application is written in, and the clone hands each
	// one to the entry it dispatches on (roadmap L11.7 · Gap R.336).
	//
	// Each word is a PAIR, because the two names do the same job in two different frames (roadmap L11.7 ·
	// Gap R.375): `from` is the name the HOST reads — the spelling the program wrote, the only spelling the
	// value tables in the host frame can answer a question about — and `word` is the name of the clone's own
	// PARAMETER, which this pass mints under `callableForwardInfix` so that a higher-order body that has
	// already bound that name for itself cannot have its own read answer the word it is being handed.
	forwarded []callableForward
}

// callableForward is one word a clone forwards to the entries it dispatches on: the name the supplying
// frame reads, and the name of the parameter this pass minted to carry it (roadmap L11.7 · Gap R.375).
type callableForward struct {
	word string
	from string
}

// callableArgHoist is a lambda the pass gave a declaration to in the argument position of a call it does not
// otherwise own — the ordering builtins' `key=`, a builtin's plain argument — recorded by its place among
// the program's calls so the rewrite can point that argument at the declaration (roadmap L11.7, ADR 0340's
// key road, which already calls a named callable and only needed the lambda to have a name).
type callableArgHoist struct {
	rank int
	pos  int
	name string
	// captures is how many words this pass minted beside the lambda's own parameters for the names the
	// body reads out of the frame the call is written in. The account reports them (roadmap L11.7 · Gap
	// R.376), and the count is the hoist's own rather than a re-read of the declaration, so the number a
	// reader sees is the number the road wrote.
	captures int
}

// callableKeyRet is one `key=` argument the pass retargets, by the place its call holds among the
// program's calls and the argument slot the option was written in.
type callableKeyRet struct {
	rank int
	pos  int
	from string
	to   string
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
	// entryCanonical is the program's own spelling for each entry symbol. It lets a recursive call that
	// repeats the entry (`run(n - 1, lambda x: x)`) be recognized as the same function as the clone's
	// entry, even though the two lambdas are different nodes (roadmap Gap R.335).
	entryCanonical map[string]string
	// entryCaptures records, for an ENTRY lambda that reads the frame its APPLICATION is written in, the
	// names it reads and the parameter words this pass minted for them beside its own. An entry has always
	// been a bare symbol, and a symbol carries no word: ADR 0370 minted a capture parameter per name a body
	// is free in, on the APPLICATION side, and this is the same minting asked on the entry side — the clone
	// that dispatches on such an entry gains the parameters too and forwards them by name into the call the
	// body makes (roadmap L11.7 · Gap R.336, beside ADR 0372's union for the body's own captures).
	entryCaptures map[string][]string
	// prog is the program the plan was read from. The entry-side capture road asks
	// `callableLambdaCaptures`, which needs the module's own name set to tell a frame's word from a
	// global one, and the plan is a function over the AST with no generator to reach (ADR 0343).
	prog *Program
	// carriersDeclined counts, per carrier name, the call sites the reader SAW and the rewrite then left
	// alone, and carriersDeclineWhy holds the sentence for each kind that fired. The reader being complete
	// is what makes these positions visible at all (roadmap Gap R.383); the account has to say which of
	// them this pass declined and why, because a pass that quietly leaves a call out is indistinguishable
	// from one that never saw it — the exact confusion this row closes (roadmap Gap R.389).
	carriersDeclined   map[string]int
	carriersDeclineWhy []string
	// carriersDeclinedSites are the exact calls the roads left alone, kept so the invariant at the end of
	// the rewrite can ask whether any of them still names a function this pass took away. A half-rewrite is
	// never emitted — that is the rule `callableNoFunctionValuesLeft` exists to enforce for the positions a
	// function name can reach, and a declined site is the same hazard wearing a carrier's clothes.
	carriersDeclinedSites []*Call
	// acceptedSelfArgs are entry arguments a recursive call wrote that the clone discards because it
	// names the clone's own entry. They are still AST nodes in the body, but they no longer name a
	// callable once the self call is pointed at the clone (roadmap Gap R.335).
	acceptedSelfArgs map[Expr]bool
	hoisted          []*FuncDef // lambdas given declarations of their own
	// hoistedHO holds the declarations this pass minted for lambdas that are APPLIED where they are
	// written and call one of their own parameters. Unlike `hoisted`, these are never appended to the
	// program: every application was pointed at one of their clones, and emitting the body the clones
	// were copied from would put a `define` in the module that calls a parameter nothing filled (roadmap
	// L11.7 · Gap R.265).
	hoistedHO []*FuncDef
	// lambdaHO maps each such lambda node to the higher-order record minted for it, so the reading finds
	// its single application site by node identity rather than by a name the source never wrote.
	lambdaHO map[*Lambda]*callableFn
	// argHoists records, for the hoisted lambdas that sit in an argument position, which call and which
	// argument slot to point at the name the pass minted.
	argHoists []callableArgHoist
	// hoistOrigin records, beside each hoisted declaration, the lambda node it came from. The body
	// check asks "is this lambda one the plan gave a declaration to?" and answers it by node
	// identity, so a lambda the plan never resolved still blocks the rewrite.
	hoistOrigin []*Lambda
	// keyRets are the `key=` arguments this pass points at a callable by its real symbol. A name
	// written in that slot is called by the door, but the emitter cannot call a name that has no
	// declaration behind it: `neg = lambda v: 0 - v` hands the callable to the name `neg`, and the
	// declaration lives under the symbol this pass minted. The retarget is the same rewrite the
	// alias road performs on a call's callee, performed on the argument the door calls (roadmap
	// L11.7, Gap R.281; ADR 0345's alias road, ADR 0340's key door).
	keyRets []*callableKeyRet
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
type callableDecline struct {
	why string
	// noWork marks the one answer that is not about the program in front of the reader: the pass had
	// nothing to do here, as it has nothing to do for every first-order program in the corpus. It is a
	// fact about the pass, not about the call the emitter is refusing, so it is the one decline that
	// must not be pasted onto a refusal sentence (roadmap Gap R.337, ADR 0379).
	noWork bool
}

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

// callableRewriteWithdrawn is the account of a plan the pass made and then could not carry out — the
// second way the program comes back untouched, beside a refusal to plan one at all. It is a sentence
// in the decline's own shape because the two readers of it have to see one sentence: `gustyc
// --callables` prints it as the document's `refused`, and the emitter's uncallable-callable refusal
// appends it byte for byte (roadmap L11.7 · Gap R.337, ADR 0379).
func callableRewriteWithdrawn() error {
	return callableDeclinef("the rewrite was withdrawn: the program it produced still used a function as a value")
}

// callableDeclineIsNoWork answers the one question a reader of a refusal needs before repeating a
// decline: did the pass have work to do and fail at it, or did it simply find nothing to rewrite?
// "no function calls its own parameter, …" is true of every first-order program in the corpus —
// including the ones whose only problem is a call to a name that was never a function — and pasting
// it onto the emitter's refusal would say nothing about the program in front of the reader while
// breaking the byte-for-byte sentence that refusal has had since ADR 0166. Only a decline that names
// a shape the pass wanted to build and could not is worth appending (roadmap Gap R.337, ADR 0379).
func callableDeclineIsNoWork(err error) bool {
	d, ok := err.(*callableDecline)
	return ok && d != nil && d.noWork
}

// callableNoWorkDecline is the pass's "nothing for me to do here" answer, named so the question
// above can be asked without comparing prose at the call sites.
func callableNoWorkDecline() error {
	return &callableDecline{noWork: true, why: "no function calls its own parameter, no name aliases one, no lambda argument needs a declaration, and no hook's key names a callable under another symbol"}
}

// callableNoWorkDeclineFor is the same sentence with the carrier gate appended beside it. The two are
// one sentence rather than two because the reader's completeness is what makes the second half true:
// the call at line 4 is a call the pass now READS, and saying only "nothing to do" would describe a
// program the pass never looked at (roadmap Gap R.383 for the read, Gap R.389 for the restraint).
func callableNoWorkDeclineFor(plan *callablePlan) error {
	err := callableNoWorkDecline()
	if plan == nil || len(plan.carriersDeclineWhy) == 0 {
		return err
	}
	why := "no function calls its own parameter, no name aliases one, no lambda argument needs a declaration, and no hook's key names a callable under another symbol"
	for _, s := range plan.carriersDeclineWhy {
		why += "; " + s
	}
	return &callableDecline{noWork: true, why: why}
}

// callableDeclinedCarrierSites reads a program and answers, per carrier name, the lines of the calls the
// reader SAW inside a carrier a rewrite must not cross. It is the same question `noteDeclinedCarrier`
// asks while the roads run, asked again of the read alone, so the account of a DECLINED program — where
// there is no plan to remember it — can still say which positions were visible and refused (roadmap
// Gap R.389). The lines are in the order the walk met them, which is the order the source wrote them.
func callableDeclinedCarrierSites(prog *Program) map[string][]int {
	out := map[string][]int{}
	if prog == nil {
		return out
	}
	var w callableWalk
	w.visit = func(n Node, enc *FuncDef) {
		c, isCall := n.(*Call)
		if !isCall || c == nil {
			return
		}
		kind := w.declinedCarrier()
		if kind == "" {
			return
		}
		out[kind] = append(out[kind], c.Span().Line)
	}
	w.stmts(prog.Stmts, nil)
	return out
}

// noteDeclinedCarrier records that the reader SAW this call and the rewrite left it exactly where the
// program wrote it, because the call stands inside a carrier the clone must not cross — today the one
// carrier the measurement condemns, an `except` handler. It answers true so each rewrite road says so
// once, at the top of the loop, beside the two skips the roads already kept for a call inside a lambda
// and a call the road does not own.
//
// The reason is not soundness of the clone, which is the same anywhere in the tree. It is the frame the
// carrier belongs to: `try:` / `x = 1 / 0` / `except ZeroDivisionError:` / `print(sorted([3,1,2], key=lambda v: v))`
// caught its error and printed the list on the reference, and printed a `Traceback` at exit 3 once the
// call was pointed at a module-level clone — the handler's entry in the frame's trap table is keyed to
// the call the program wrote, and a call that moved stopped being the one the table named. A refusal at
// exit 1 beats that (ADR 0166), so the position stays refused until the table crossing lands (roadmap
// Gap R.389, which the reader half of this rule, roadmap Gap R.383, uncovered by making the call visible).
func (p *callablePlan) noteDeclinedCarrier(r *callableRead, rank int, c *Call) bool {
	if p == nil || r == nil || rank < 0 || rank >= len(r.callCarriers) {
		return false
	}
	kind := r.callCarriers[rank]
	if kind == "" {
		return false
	}
	if p.carriersDeclined == nil {
		p.carriersDeclined = map[string]int{}
	}
	line := 0
	if c != nil {
		line = c.Span().Line
	}
	if p.carriersDeclined[kind] == 0 {
		p.carriersDeclineWhy = append(p.carriersDeclineWhy, callableCarrierDeclineSentence(kind, line))
	}
	if p.carriersDeclinedSites == nil {
		p.carriersDeclinedSites = []*Call{}
	}
	p.carriersDeclinedSites = append(p.carriersDeclinedSites, c)
	p.carriersDeclined[kind]++
	return true
}

// carrierCrossesAClone asks the one soundness question the carrier gate leaves open: did a call the
// roads declined still end up naming a function this pass cloned away? The call keeps the name the
// program wrote, because the road that would have pointed it at a clone was the road that declined;
// and the definition that name names is about to be taken out of the module, because every site the
// pass DID take was pointed at a clone. Either way the answer is to hand the whole program back
// untouched — the refusal it had before this pass ran, which is what an un-taken rewrite always is
// (ADR 0273, ADR 0345's rule that a half-rewrite is never emitted).
func (p *callablePlan) carrierCrossesAClone() bool {
	if p == nil || len(p.carriersDeclinedSites) == 0 {
		return false
	}
	for _, c := range p.carriersDeclinedSites {
		if c == nil {
			continue
		}
		if nm, ok := c.Fn.(*Name); ok {
			target := nm.Value
			if aliased, isAlias := p.aliases[target]; isAlias {
				target = aliased
			}
			for _, cf := range p.order {
				if cf != nil && cf.def != nil && cf.def.Name == target {
					return true
				}
			}
		}
		for _, cf := range p.order {
			if cf != nil && cf.fromLambda != nil && cf.fromLambda == c.Fn {
				return true
			}
		}
	}
	return false
}

// callableCarrierDeclineSentence is what the machine path says about a position the pass declined: the
// carrier by the name the program would recognise, the line the call was written on, and the row that
// owns the missing road — and no Go type, minted symbol or pointer value in it (ADR 0166, ADR 0345 §3).
func callableCarrierDeclineSentence(kind string, line int) string {
	switch kind {
	case callableCarrierExcept:
		return fmt.Sprintf("a call of a callable written inside an `except` handler at line %d keeps the ordinary road's refusal: pointing that call at a declaration this pass minted moves it out of the frame whose trap catch the handler is registered on, and the program that catches its own error today would die unhandled at exit 3 (roadmap Gap R.389)", line)
	}
	return ""
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
		prog: prog, entryCaptures: map[string][]string{}, carriersDeclined: map[string]int{},
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
		if callableSelfCallOmitsRequired(fn, positions) {
			// The reference rejects the self call before any entry question can be asked. Leave the body
			// to the ordinary road, whose arity checker says which argument is missing; a clone does not
			// own the reference's TypeError (roadmap Gap R.335).
			continue
		}
		if why := callableSelfDeclines(fn, self, positions); why != "" {
			return nil, callableDeclinef("%s", why)
		}
		cf := &callableFn{def: fn, paramPos: map[string]int{}, calledPos: positions, srcParams: len(fn.Params)}
		for i, p := range fn.Params {
			cf.paramPos[p.Name] = i
		}
		for _, sc := range self {
			cf.self = append(cf.self, &callableSelfSite{rank: sc.rank, line: sc.line, args: sc.args})
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

	// An applied lambda whose body calls one of its own parameters is the same higher-order body as a
	// `def` that does, written at the place it is called:
	//
	//	print((lambda f: f(3))(lambda x: x * 2))
	//
	// The `def` road clones one body per tuple of functions its called parameters are asked to be, and
	// nothing about that construction needs a name in the source — only a body and the call that fills it.
	// The lambda is therefore given a declaration of its own (the hoist the argument and binding roads
	// already have), registered as the higher-order function that declaration describes, and cloned by
	// the same code; the one difference is that the declaration itself is never emitted, because every
	// call the program wrote has been pointed at a clone. A lambda the pass cannot prove — one that
	// reads a name the frame owns, one whose body reaches a nested lambda or a comprehension, one whose
	// application does not fill its parameters — is left exactly where it was, with the refusal the
	// ordinary road gives it (roadmap L11.7 · Gap R.265; ADR 0345's construction, ADR 0249's rule that a
	// shape the pass cannot state is refused rather than answered by coincidence).
	for rank, c := range r.calls {
		if plan.noteDeclinedCarrier(r, rank, c) {
			continue // the reader sees this call (Gap R.383) and the rewrite does not cross into it (Gap R.389)
		}
		lam, isLambda := c.Fn.(*Lambda)
		if !isLambda || lam == nil || lam.Body == nil {
			continue
		}
		if r.inLambda[rank] {
			continue // a call written inside a lambda is the closure road's frame, not this pass's
		}
		if callableLambdaBodyBlocked(lam) {
			continue
		}
		positions, ok := r.lambdaCalledParams(lam)
		if !ok || len(positions) == 0 {
			continue
		}
		// A `lambda` that reads a name its own parameters do not bind crosses that name into the generated
		// function as a parameter the call site fills — the closure road's rule (ADR 0370), asked here because
		// it is this pass that mints the declaration a CLONE is copied from. Before this, an applied lambda
		// was registered only while it read nothing outside itself, so a body that BOTH calls one of its own
		// parameters AND reads the frame was declined by the entry audit before the capture road was ever
		// consulted; the two roads had been built in one cycle and never met (roadmap L11.7 · Gap R.334).
		captures, capturable := callableLambdaCaptures(r.encs[rank], prog, lam)
		if !capturable {
			continue // a name past the frame the application is written in: the closure lift's, not this pass's
		}
		body := copyASTExpr(lam.Body)
		if body == nil {
			return nil, callableDeclinef("the applied lambda at line %d could not be copied", c.Span().Line)
		}
		name, ok := plan.freshName(callableDisplayNameOf(r.encs[rank])+callableLambdaInfix, lam)
		if !ok {
			return nil, callableDeclinef("no name could be minted for the applied lambda at line %d", c.Span().Line)
		}
		def := &FuncDef{
			Name:   name,
			Params: append([]*Param(nil), lam.Params...),
			Body:   []Stmt{&ReturnStmt{Expr: body, Src: lam.Span()}},
			Src:    lam.Span(),
		}
		for _, c := range captures {
			def.Params = append(def.Params, &Param{Name: c, Src: lam.Span()})
		}
		def.Captures = captures
		def.CloneOf = callableAnonymousName
		cf := &callableFn{def: def, paramPos: map[string]int{}, calledPos: positions, fromLambda: lam, srcParams: len(lam.Params)}
		for i, p := range def.Params {
			cf.paramPos[p.Name] = i
		}
		plan.fns[name] = cf
		plan.order = append(plan.order, cf)
		plan.arity[name] = len(lam.Params)
		plan.hoistedHO = append(plan.hoistedHO, def)
		if plan.lambdaHO == nil {
			plan.lambdaHO = map[*Lambda]*callableFn{}
		}
		plan.lambdaHO[lam] = cf
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

	// An ALIAS OF A NAME is the same callable spelled twice: `neg = lambda v: 0 - v` / `two = neg` /
	// `print(two(3))` is CPython's `-3`, and the pass could only reach it when the right-hand name came
	// from a `def`. The rule is the alias road's own — the name's whole history must be this one binding
	// and the calls it stands for — chased one more link, so a callable the pass itself gave a
	// declaration to can be handed on under a second name (roadmap L11.7, Gap R.281).
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
		rhs, ok := a.Value.(*Name)
		if !ok || tgt.Value == rhs.Value {
			continue
		}
		if isPredeclaredName(tgt.Value) || isPredeclaredName(rhs.Value) {
			continue
		}
		target, ok := plan.chaseCallableName(r, rhs.Value)
		if !ok {
			continue
		}
		if !callableBoundExactly(r, tgt.Value, []Node{a}) {
			continue
		}
		if n, known := plan.arity[target]; known {
			plan.arity[tgt.Value] = n
		}
		plan.aliases[tgt.Value] = target
	}

	// A closed lambda in an argument position is given a declaration here, so the door that reads the
	// argument already knows how to call a named callable meets one it can name: `sorted(xs, key=lambda x:
	// -x)` and `max(xs, key=lambda v: -v)` ask ADR 0340's key road, which calls a function by the ordinary
	// call road and refused only because a lambda has no name to call. A call of a function the program
	// declared is left to the clone road above, which owns that shape (roadmap L11.7, ADR 0345).
	//
	// A hook lambda of the ordering door may ALSO read its own frame. That is the shape ADR 0395 paid one
	// cycle ago for an ENTRY — the callable the CALLEE calls — and left standing for this road, which hoists
	// a hook by a second route that never asked the door (roadmap L11.7 · Gap R.376):
	//
	//	def use():
	//	    base = 1
	//	    return sorted([3, 1, 2], key=lambda x: 0 - x + base)   # the reference's [3, 2, 1]
	//
	// The two roads were built a cycle apart and never met. The hook's declaration is minted HERE, and the
	// one question "what does this body read out of the frame it was written in" is asked of the same door the
	// entry and application roads ask — `callableLambdaCaptures` (ADR 0370) — so a third spelling test is not
	// added to the two this pass already keeps (ADR 0232). The word is filled by the road that already fills
	// it: `lambdaCallArgs` reaches a capture by name through the CALLER'S own reading, and the caller of a
	// hook is the frame the `sorted` call is written in, which is the frame that owns `base`. No second fill
	// road, and no `fnptr`: the hook stays a named `define` the door calls by symbol (ADR 0345).
	for rank, c := range r.calls {
		if plan.noteDeclinedCarrier(r, rank, c) {
			continue // the reader sees this call (Gap R.383) and the rewrite does not cross into it (Gap R.389)
		}
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
		// The one argument position a builtin CALLS is the ordering door's `key=` (ADR 0340). A lambda in any
		// other argument slot is an operand nobody calls, and minting capture words for it would mint words no
		// call ever fills.
		_, keyExpr, isKeyDoor := keyDoorOptions(callee, c.Args)
		for pos, a := range c.Args {
			arg := a
			if kw, isKw := a.(*KeywordArg); isKw {
				arg = kw.Value
			}
			l, isLambda := arg.(*Lambda)
			if !isLambda {
				continue
			}
			captures := []string(nil)
			if !callableLambdaIsSelfContained(l) {
				if !isKeyDoor || keyExpr != arg || r.inLambda[rank] {
					continue // not the position the door calls, or a frame the closure road owns
				}
				caps, capturable := callableLambdaCaptures(r.encs[rank], prog, l)
				if !capturable {
					continue // a name past this frame is ADR 0245's lift (Gap R.331, Gap R.332), and a function read
					// as a VALUE is ADR 0345's (Gap R.378) — neither is this road's to answer with a word
				}
				captures = caps
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
			// One word per name the hook reads out of the frame the application is written in, appended beside
			// the parameters the source spelled — the minting `resolveEntry` does for an entry (ADR 0395) and the
			// application road does for an applied body (ADR 0372). `Captures` IS set here, where the entry road
			// deliberately leaves it empty: the caller of a hook is a frame of the PROGRAM, so "reach for that
			// name by name" is the honest instruction, and `captureCarriesOneWord` is asked where the value lives
			// (Gap R.332).
			for _, cap := range captures {
				def.Params = append(def.Params, &Param{Name: cap, Src: l.Span()})
			}
			def.Captures = captures
			def.CloneOf = callableAnonymousName
			plan.hoisted = append(plan.hoisted, def)
			plan.hoistOrigin = append(plan.hoistOrigin, l)
			plan.argHoists = append(plan.argHoists, callableArgHoist{rank: rank, pos: pos, name: name, captures: len(captures)})
			plan.arity[name] = len(l.Params)
		}
	}

	// The hook's key: the one argument the compiler, not the source, calls. A name written in that
	// slot is the callable, and the door reaches it the way a call reaches a name — by the symbol the
	// callable lives under. For `def neg(v)` that symbol is `neg` and the door already answers; for
	// `neg = lambda v: 0 - v` the declaration this pass minted lives under a generated name and the
	// written name has nothing behind it, which is what made the door call a word instead of a function
	// and the program die with the reference's own `TypeError: 'int' object is not callable` at exit 3
	// (roadmap L11.7, Gap R.281; ADR 0340's door, ADR 0345's alias road).
	for rank, c := range r.calls {
		if plan.noteDeclinedCarrier(r, rank, c) {
			continue // the reader sees this call (Gap R.383) and the rewrite does not cross into it (Gap R.389)
		}
		pos, nm, ok := callableKeySlotOf(r, c)
		if !ok {
			continue
		}
		target, isAlias := plan.aliases[nm.Value]
		if !isAlias || target == nm.Value {
			continue // a `def`'d name is already the symbol the door calls
		}
		if _, isDef := r.fds[target]; !isDef {
			if _, minted := plan.arity[target]; !minted {
				continue // nothing named by that word to call: the door refuses, as it always did
			}
		}
		plan.keyRets = append(plan.keyRets, &callableKeyRet{rank: rank, pos: pos, from: nm.Value, to: target})
	}

	// The pass has work to do only when something asked for it: a higher-order body to clone, an alias
	// whose calls can be pointed at the function they mean, a lambda argument the pass can name, or a
	// hook's key that names a callable under another symbol. An ordinary program is handed back with one
	// comparison spent on it.
	if len(plan.fns) == 0 && len(plan.aliases) == 0 && len(plan.argHoists) == 0 && len(plan.keyRets) == 0 {
		// The recursion gets one more chance to explain itself. A pair of functions that call a parameter
		// and hand it to each other is a recursion no clone can be written for — and it registers no
		// higher-order function at all, because handing the callable to the other body is a use as a value
		// on the ordinary road. Said here rather than at the pair's own row because this is the one place
		// the pass knows nothing else was rewritable: a decline that fires on a program with work to do
		// would take the answer away from a higher-order call that has nothing to do with the pair.
		if why := callableMutualHigherOrderDecline(r); why != "" {
			return nil, callableDeclinef("%s", why)
		}
		return nil, callableNoWorkDeclineFor(plan)
	}

	// The call sites: each is read against the definition it targets, and each called parameter is
	// resolved to the function the call asks for.
	for rank, c := range r.calls {
		if plan.noteDeclinedCarrier(r, rank, c) {
			continue // the reader sees this call (Gap R.383) and the rewrite does not cross into it (Gap R.389)
		}
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
		if why := callableSiteOmission(c, cf); why != "" {
			return nil, callableDeclinef("the call of %q at line %d %s", name, c.Span().Line, why)
		}
		site := &callableSite{rank: rank}
		for _, pos := range cf.calledPos {
			entry, err := plan.resolve(r, callableSiteOperand(c, cf, pos), r.encs[rank])
			if err != nil {
				return nil, err
			}
			site.slots = append(site.slots, entry)
		}
		site.grow = callableSiteFill(c, cf)
		site.omitted = cf.srcParams - len(c.Args)
		cf.sites = append(cf.sites, site)
	}

	// The applied lambdas' own call sites. A `def` may be called from anywhere in the program, so its
	// sites are found by walking every call; a lambda written as a callee has exactly one — the
	// application the source wrote beside it — and that site is read by the same door: every called
	// parameter is resolved to the function the argument asks for, an argument that is a closed lambda
	// is given a declaration of its own, and anything the reading cannot name declines the whole program
	// (roadmap L11.7 · Gap R.265).
	for _, cf := range plan.order {
		if cf.fromLambda == nil {
			continue
		}
		for rank, c := range r.calls {
			if c.Fn != cf.fromLambda {
				continue
			}
			if why := callableSiteOmission(c, cf); why != "" {
				return nil, callableDeclinef("the application of the lambda at line %d %s", cf.fromLambda.Span().Line, why)
			}
			site := &callableSite{rank: rank}
			for _, pos := range cf.calledPos {
				entry, err := plan.resolve(r, callableSiteOperand(c, cf, pos), r.encs[rank])
				if err != nil {
					return nil, err
				}
				site.slots = append(site.slots, entry)
			}
			site.grow = callableSiteFill(c, cf)
			site.omitted = cf.srcParams - len(c.Args)
			cf.sites = append(cf.sites, site)
		}
		if len(cf.sites) == 0 {
			return nil, callableDeclinef("the lambda whose body calls its own parameter is never applied")
		}
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
			cl := &callableClone{key: strings.Split(k, "|"), name: name, fn: cf}
			// The entries of THIS tuple that were minted beside capture words: the clone that dispatches on
			// them is the frame that supplies those words, so it gains one parameter per name — the union, in
			// the order the tuple reads them, the same union rule ADR 0372 wrote for a body's own captures
			// (roadmap L11.7 · Gap R.336).
			//
			// The union is keyed by the name the HOST reads, because that is the name two entries of one tuple
			// share when they read the same word; the name of the clone's own PARAMETER is minted once per such
			// group, under a spelling the source cannot write, so a body that owns the captured name for itself
			// cannot answer the word it is being handed (roadmap L11.7 · Gap R.375, ADR 0397 — the shape ADR 0395
			// declined out loud while the parameter carried the program's own spelling).
			for _, sym := range cl.key {
				for _, c := range plan.entryCaptures[sym] {
					known := false
					for _, have := range cl.forwarded {
						if have.from == c {
							known = true
							break
						}
					}
					if known {
						continue
					}
					word, ok := plan.freshForwardWord(cf.def, cl, c)
					if !ok {
						return nil, callableDeclinef("no name could be minted for the word %q would forward to the entry of the call at line %d: the spelling %q is already taken (roadmap L11.7 · Gap R.375)", cf.def.Name, site.rank, c)
					}
					cl.forwarded = append(cl.forwarded, callableForward{word: word, from: c})
				}
			}
			// The audit the minting is checked against, stated as the invariant it is meant to hold: the word
			// the clone forwards is a PARAMETER of the clone, so no name the higher-order body already owns —
			// a spelled parameter, or anything its own body binds — may be the spelling of that parameter.
			// ADR 0395 could only DECLINE here (the parameter was the captured name, and the body was allowed to
			// own it); minting under `callableForwardInfix` makes the same question an audit that cannot fire,
			// and a pass that stops minting unwriteable names fails this row instead of shipping two slots under
			// one spelling (roadmap L11.7 · Gap R.375).
			for _, f := range cl.forwarded {
				if callableCloneOwnsName(cf.def, f.word) || f.word == f.from || !callableLexable(f.word) || !strings.Contains(f.word, callableForwardInfix) {
					return nil, &callableDecline{why: "the word a clone would forward is a name the body could read for itself"}
				}
				for _, other := range cl.forwarded {
					if other.word == f.word && other.from != f.from {
						return nil, &callableDecline{why: "two forwarded words would share one parameter"}
					}
				}
			}
			cf.clones = append(cf.clones, cl)
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
	for _, cf := range plan.order {
		for _, sf := range cf.self {
			for _, cl := range cf.clones {
				if !callableSelfSiteAgrees(plan, cf, cl, sf) {
					name := ""
					if len(cf.calledPos) > 0 && cf.def != nil {
						for _, pos := range cf.calledPos {
							if pos >= 0 && pos < len(sf.args) {
								if an, ok := sf.args[pos].(*Name); ok && pos < len(cf.def.Params) && cf.def.Params[pos] != nil && an.Value == cf.def.Params[pos].Name {
									continue
								}
								if pos < len(cf.def.Params) && cf.def.Params[pos] != nil {
									name = cf.def.Params[pos].Name
								}
								break
							}
						}
					}
					return nil, callableDeclinef("%q recurses at line %d and hands %q something other than itself — an entry the clone cannot inherit; a self call may omit a called slot, name its own parameter, or repeat the very function this clone was minted for (roadmap Gap R.335)", cf.def.Name, sf.line, name)
				}
			}
		}
	}
	// The bodies are checked last: a lambda is allowed only where the call sites gave it a declaration,
	// or the recursion accepted it as the clone's own entry and will replace it with a placeholder.
	for _, cf := range plan.order {
		if callableBodyBlocked(r, plan, cf.def) {
			return nil, callableDeclinef("%s's body holds something a clone could not answer", cf.def.Name)
		}
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
		// A parameter the body calls has to arrive as a plain slot or as a default the pass can name: a
		// default that is a number, a container or a call would be filled by a value the clone cannot see,
		// and a parameter declared `function` or `Callable[...]` is ADR 0284's road to walk — that machinery
		// decides the shape of such a call and its trap today, and a clone written over the top of it would
		// answer the same program by two roads. A `lambda` or a name as the default is the one shape that
		// does have a symbol to substitute, which is what an entry is (roadmap Gap R.329, ADR 0371).
		if callableAnnotIsCallable(p.Annot) {
			return nil, false
		}
		if p.Default != nil && !callableDefaultNamesAFunction(p.Default) {
			return nil, false
		}
		positions = append(positions, i)
	}
	return positions, true
}

// lambdaCalledParams answers which of an applied lambda's own parameters its body calls —
// callableCalledParams' question, asked of a body the source never gave a name. The rules are the
// `def` road's, unchanged: the parameters are distinct and none is a predeclared name, a called
// parameter is a plain slot (no default, no `function`/`Callable` annotation, which is ADR 0284's road
// to walk), and every one of its occurrences in the body is the callee of a call. The entry-arity audit
// `callableInnerAgrees` is asked of these positions unchanged (roadmap L11.7 · Gap R.265).
func (r *callableRead) lambdaCalledParams(l *Lambda) ([]int, bool) {
	if l == nil || l.Body == nil {
		return nil, false
	}
	seen := map[string]bool{}
	for _, p := range l.Params {
		if p == nil || seen[p.Name] || isPredeclaredName(p.Name) {
			return nil, false
		}
		seen[p.Name] = true
	}
	var positions []int
	for i, p := range l.Params {
		if !r.onlyCalledInLambda(l, p.Name) {
			continue
		}
		// The `def` road's rule, unchanged: a called parameter is a plain slot, or carries a default the
		// pass can name as a function — the case an application that OMITS the parameter is read through
		// (roadmap L11.7 · Gap R.329, ADR 0371).
		if callableAnnotIsCallable(p.Annot) {
			return nil, false
		}
		if p.Default != nil && !callableDefaultNamesAFunction(p.Default) {
			return nil, false
		}
		positions = append(positions, i)
	}
	return positions, true
}

// onlyCalledInLambda is onlyCalledInBody for a lambda body. The record's name table is keyed by the
// enclosing `def`, which a lambda written inside one shares — so the question is asked by walking the
// body and matching each occurrence against the callee set the program-wide read built, which is a
// statement about the node, not about its spelling (the same node-identity rule `callsOfParam` walks
// with, and the reason a parameter the body also reads, stores or returns is never substituted).
func (r *callableRead) onlyCalledInLambda(l *Lambda, param string) bool {
	seen, called := 0, 0
	var w callableWalk
	w.visit = func(n Node, enc *FuncDef) {
		if nm, isName := n.(*Name); isName && nm.Value == param {
			seen++
			if r.calleeNames[nm] {
				called++
			}
		}
	}
	w.expr(l.Body, nil)
	return seen > 0 && seen == called
}

// callableLambdaBodyBlocked is callableBodyBlocked for a lambda the program never named. Each entry is
// a scope or a value this pass does not own: a nested lambda or `def` brings a frame the clone has no
// entry tuple for, a comprehension or generator builds its own bindings, a `with`/`match`/import brings
// state the reading never numbered, and an attribute, subscript or slice reads a value the callable
// would have to BE — which is the one thing this pass does not make. A blocked shape keeps the refusal
// the ordinary road gives it, and keeps its own roadmap row.
func callableLambdaBodyBlocked(l *Lambda) bool {
	if l == nil || l.Body == nil {
		return true
	}
	blocked := false
	var w callableWalk
	w.visit = func(n Node, enc *FuncDef) {
		switch n.(type) {
		case *Lambda, *FuncDef, *ClassDef, *Comp, *Generator, *YieldStmt, *YieldFromStmt, *AwaitExpr,
			*AssignExpr, *WithStmt, *MatchStmt, *ImportStmt, *TypeAliasStmt, *ExternDecl,
			*Attr, *Index, *Slice:
			blocked = true
		}
	}
	w.expr(l.Body, nil)
	return blocked
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
			// A lambda is allowed only where the plan gave it a declaration of its own, or where the
			// recursion road already accepted it as the entry the clone repeats and replaces it with a
			// placeholder (roadmap Gap R.335).
			if !plan.hoistsLambda(x) && !plan.acceptedSelfArg(x) {
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

func (p *callablePlan) acceptedSelfArg(l *Lambda) bool {
	if p == nil || p.acceptedSelfArgs == nil || l == nil {
		return false
	}
	return p.acceptedSelfArgs[l]
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

// chaseCallableName follows a name to the symbol its callable lives under. A chain of module-level
// bindings — `neg = lambda v: 0 - v` then `two = neg` — is one callable wearing the two names the
// program wrote, and every question the pass asks about a callable is asked of the symbol it was
// given. The chain ends at a `def`'s own name or at a declaration this pass minted; a name that ends
// anywhere else names no callable at all, and the caller leaves the program to the refusal the door
// already has (roadmap L11.7, Gap R.281).
func (p *callablePlan) chaseCallableName(r *callableRead, name string) (string, bool) {
	if name == "" {
		return "", false
	}
	seen := map[string]bool{}
	cur := name
	for {
		if seen[cur] {
			return "", false // a binding cycle names no callable either
		}
		seen[cur] = true
		target, isAlias := p.aliases[cur]
		if !isAlias {
			break
		}
		cur = target
	}
	if _, isDef := r.fds[cur]; isDef {
		return cur, true
	}
	if _, minted := p.arity[cur]; minted {
		return cur, true
	}
	return "", false
}

// resolve resolves one entry expression and records the spelling the program used for the symbol it
// produced. The recursion road needs that record on both sides of a self call: `run(n, lambda x: x)` and
// `run(n - 1, lambda x: x)` are the same entry even when they are two different AST nodes, and a clone
// may follow itself only while every called slot still names the function that clone was minted for
// (roadmap L11.7 · Gap R.335, ADR 0345).
func (p *callablePlan) resolve(r *callableRead, e Expr, enc *FuncDef) (string, error) {
	sym, err := p.resolveEntry(r, e, enc)
	if err == nil && sym != "" {
		if p.entryCanonical == nil {
			p.entryCanonical = map[string]string{}
		}
		if _, seen := p.entryCanonical[sym]; !seen {
			p.entryCanonical[sym] = callableCanonicalExprText(e)
		}
	}
	return sym, err
}

func (p *callablePlan) resolveEntry(r *callableRead, e Expr, enc *FuncDef) (string, error) {
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
		// An entry used to be ONLY a symbol: the hoisted `define` below is what the callee's body ends up
		// calling by name, and a symbol carries no word beside itself. A lambda that reads the frame its
		// APPLICATION is written in therefore could not be lifted out of that frame and stay right, and the
		// refusal named the waiting name (Gap R.336). It is minted now, one word per name, by the same door
		// the APPLICATION road asks (ADR 0370): the clone that dispatches on this entry gains the parameters
		// and forwards them into the call its body makes. What the door still cannot supply — a name the
		// enclosing frame does not own, which is the nested-`def` lift's and not this pass's — keeps the
		// refusal, with the same name in it (roadmap L11.7 · Gap R.336, beside Gap R.331 and Gap R.332).
		captures, capturable := callableLambdaCaptures(enc, p.prog, x)
		if !capturable {
			nm, _ := callableLambdaFrameRead(r, x, enc)
			if nm == "" {
				if free := callableFreeNames(x); len(free) > 0 {
					nm = free[0]
				}
			}
			return "", callableDeclinef("an entry lambda reads %q from the frame its application is written in, and an entry is a bare symbol: the word that name holds would have to arrive beside it (the capture parameter of the application road, asked on the entry side — Gap R.336)", nm)
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
		// The words are parameters and nothing else. `Captures` is deliberately NOT set: that field is the
		// instruction to the CALL road to fill a capture by name from the caller's own reading, and the
		// caller here is a clone of the higher-order body, whose reading of that name is the parameter this
		// pass is about to give it — a value handed down a frame, not a value the frame reached for. The
		// clone carries the `Captures` field, and the words arrive positionally at the inner call.
		for _, c := range captures {
			def.Params = append(def.Params, &Param{Name: c, Src: x.Span()})
		}
		if len(captures) > 0 {
			p.entryCaptures[name] = append([]string(nil), captures...)
		}
		p.hoisted = append(p.hoisted, def)
		p.hoistOrigin = append(p.hoistOrigin, x)
		p.arity[name] = len(def.Params)
		return name, nil
	default:
		return "", callableDeclinef("an argument that is not a declared function or a closed lambda")
	}
}

// callableCanonicalExprText renders the shape a callable entry expression has in the source, without
// its positions. Two separately parsed `lambda x: x` nodes are the same entry to the recursion road;
// an `f` in one clone is not interchangeable with `inc` merely because both happen to have a body.
func callableCanonicalExprText(e Expr) string {
	if e == nil {
		return ""
	}
	var sb strings.Builder
	writeExpr(&sb, e, 0)
	return sb.String()
}

// callableSelfSiteAgrees asks whether one self call can follow the clone it sits in. A missing called
// argument inherits that clone's entry; an argument that repeats the entry's spelling is the same entry;
// anything else would ask the clone to become a different function at run time, which is the one thing
// this pass does not make (roadmap Gap R.335, ADR 0345).
func callableSelfSiteAgrees(p *callablePlan, cf *callableFn, cl *callableClone, sf *callableSelfSite) bool {
	if p == nil || cf == nil || cl == nil || sf == nil || cf.def == nil {
		return false
	}
	if len(sf.args) > cf.srcParams || callableHasKeywordArgs(sf.args) {
		return false
	}
	for _, pos := range cf.calledPos {
		if pos < 0 || pos >= len(cf.def.Params) {
			return false
		}
		slot := callableCalledSlot(cf, pos)
		if slot < 0 || slot >= len(cl.key) {
			return false
		}
		if pos >= len(sf.args) {
			if cf.def.Params[pos].Default == nil {
				return false // the reference's TypeError is missing a required argument, not an inherited entry
			}
			continue // inherited from this clone's own entry tuple
		}
		arg := sf.args[pos]
		if an, ok := arg.(*Name); ok && cf.def.Params[pos] != nil && an.Value == cf.def.Params[pos].Name {
			continue // `run(n - 1, f)` hands the parameter back to itself, as before
		}
		want, known := p.entryCanonical[cl.key[slot]]
		if !known || want == "" || callableCanonicalExprText(arg) != want {
			return false
		}
		if _, isLambda := arg.(*Lambda); isLambda {
			if p.acceptedSelfArgs == nil {
				p.acceptedSelfArgs = map[Expr]bool{}
			}
			p.acceptedSelfArgs[arg] = true
		}
	}
	return true
}

func callableCalledSlot(cf *callableFn, pos int) int {
	if cf == nil {
		return -1
	}
	for i, p := range cf.calledPos {
		if p == pos {
			return i
		}
	}
	return -1
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
		// The entry's own parameter count, minus the words this pass minted beside it for the names it
		// reads from the frame its application is written in: those are not arguments the body's call of
		// the parameter spells, they are the ones the clone forwards at that call (Gap R.336, below).
		want := plan.arity[entry] - len(plan.entryCaptures[entry])
		if want != arity {
			return false
		}
	}
	return true
}

// appendForwardedWords writes the words a clone forwards to its entries onto the declaration the pass is
// building: one PARAMETER per word, spelled with the name this pass minted; one entry of `Captures`, so the
// frame knows the position is a word it was GIVEN rather than one the source spelled (ADR 0372's verdict
// memo keys on it); and one entry of `CaptureReads`, the name the caller reads to fill it — the program's own
// spelling, which is the only spelling the supplying frame's tables can be asked about. The older captures
// on the node, the ones the body of an applied `lambda` reads out of its own frame, are padded with "": the
// two lists are read in parallel, so “same spelling” has to be sayable (roadmap L11.7 · Gap R.375).
func appendForwardedWords(fd *FuncDef, forwarded []callableForward, src Span) {
	if fd == nil || len(forwarded) == 0 {
		return
	}
	for _, f := range forwarded {
		fd.Params = append(fd.Params, &Param{Name: f.word, Src: src})
	}
	for len(fd.CaptureReads) < len(fd.Captures) {
		fd.CaptureReads = append(fd.CaptureReads, "")
	}
	for _, f := range forwarded {
		fd.Captures = append(fd.Captures, f.word)
		fd.CaptureReads = append(fd.CaptureReads, f.from)
	}
}

// callableCaptureRead answers which name the CALLER reads to fill capture parameter `i` of this declaration:
// the entry of `CaptureReads`, or the parameter's own spelling where the pass wrote no other. An unknown
// declaration answers with the parameter's own spelling too: only the defunctionalisation pass writes
// `CaptureReads`, so a frame the EMITTER minted — an inline applied `lambda`, which is not in the program's
// own table of definitions — has no forwarded word to read (roadmap L11.7 · Gap R.375, ADR 0232).
func callableCaptureRead(fd *FuncDef, i int, name string) string {
	if fd == nil || i < 0 || i >= len(fd.Captures) {
		return name
	}
	if i < len(fd.CaptureReads) && fd.CaptureReads[i] != "" {
		return fd.CaptureReads[i]
	}
	return name
}

// callableForwardWordOf answers the name of the clone's own parameter that carries the word read from
// `from`, or "" when the clone forwards no such word — which is the shape the writer withdraws on rather
// than emitting a read of a name nothing fills.
func callableForwardWordOf(cl *callableClone, from string) string {
	if cl == nil {
		return ""
	}
	for _, f := range cl.forwarded {
		if f.from == from {
			return f.word
		}
	}
	return ""
}

// callableCloneOwnsName asks whether the higher-order body already has a slot under this spelling: one of
// the parameters the source wrote, or a name its own body binds. A forwarded capture word is a NEW
// parameter on the clone, and two parameters under one name is a body whose reads answer each other.
func callableCloneOwnsName(def *FuncDef, name string) bool {
	if def == nil || name == "" {
		return false
	}
	for _, p := range def.Params {
		if p != nil && p.Name == name {
			return true
		}
	}
	for nm := range bodyBoundNames(def.Body) {
		if nm == name {
			return true
		}
	}
	return false
}

func callableHasKeywordArgs(args []Expr) bool {
	for _, a := range args {
		if _, isKw := a.(*KeywordArg); isKw {
			return true
		}
	}
	return false
}

// callablePositionCalled asks whether a parameter index is one the body calls, i.e. one whose entry the
// clone's tuple carries.
func callablePositionCalled(cf *callableFn, pos int) bool {
	for _, p := range cf.calledPos {
		if p == pos {
			return true
		}
	}
	return false
}

// callableDefaultNamesAFunction is the one shape of default a CALLED parameter may carry: a `lambda`
// written inline, or a plain name. Those are the two spellings `resolve` can answer with a symbol, which
// is what an entry has to be. `*args`, an annotation, a subscript, a call — all name nothing the pass can
// put in a call position, so a called parameter carrying one of them stays the ordinary road's refusal
// (roadmap L11.7 · Gap R.329, ADR 0283's rule that a refusal is what an unanswered shape gets, ADR 0371).
func callableDefaultNamesAFunction(e Expr) bool {
	switch x := e.(type) {
	case *Lambda:
		return callableLambdaIsSelfContained(x) && !callableLambdaBodyBlocked(x)
	case *Name:
		return !isPredeclaredName(x.Value)
	}
	return false
}

// callableDefaultAnswersScalar is the other half of the same question, asked of a parameter the body READS
// rather than calls: a default the call site has to bind is only safe when it is a literal number, text or
// bool, because those are the defaults whose binding the call road already answers. `None`, a list, a dict,
// a set, a tuple and a call are the shapes whose omitted binding reads the payload word instead of the
// value — roadmap Gap R.277 owns that, and this pass declines rather than route a program through it
// (ADR 0273's rule that a wrong value is worse than a door that shuts).
func callableDefaultAnswersScalar(e Expr) bool {
	switch e.(type) {
	case *IntLit, *FloatLit, *StrLit, *BoolLit:
		return true
	}
	return false
}

// callableSiteOmission is the entry tuple's own arity audit, and the ONLY one a call is read against: the
// tuple is the list of functions the body asks for, so a call is readable when it names — or leaves a
// default to name — every position the body calls. A call that writes more arguments than the body has
// parameters, or that names its arguments by keyword, is a call whose positions this pass cannot read and
// keeps the ordinary road's sentence; a call that omits a parameter with no default omits an argument the
// reference itself would have demanded (roadmap L11.7 · Gap R.329, ADR 0371).
func callableSiteOmission(c *Call, cf *callableFn) string {
	if callableHasKeywordArgs(c.Args) {
		return "by position: a keyword argument names a parameter the entry tuple is read for by position"
	}
	if len(c.Args) > cf.srcParams {
		return "more arguments than the body has parameters"
	}
	for pos := len(c.Args); pos < cf.srcParams; pos++ {
		p := cf.def.Params[pos]
		if p == nil || p.Default == nil {
			return fmt.Sprintf("%q, which has no default to answer for it", callableParamName(p))
		}
		if callablePositionCalled(cf, pos) {
			if !callableDefaultNamesAFunction(p.Default) {
				return fmt.Sprintf("the callable parameter %q, whose default is not a function this backend can build a call to", callableParamName(p))
			}
			continue
		}
		if !callableDefaultAnswersScalar(p.Default) {
			return fmt.Sprintf("%q, whose default is a value the call road binds as a payload word — roadmap Gap R.277 owns that binding", callableParamName(p))
		}
	}
	return ""
}

// callableParamName is the word a refusal prints for a parameter the tree did not name.
func callableParamName(p *Param) string {
	if p == nil {
		return "?"
	}
	return p.Name
}

// callableSuppliedParams is the parameter list as a CALLER sees it: the words the source wrote, without the
// ones this pass minted beside them for a body that reads the frame its callable was written in. A capture
// parameter has no argument in the tree — `lambdaCallArgs` fills it from the caller's own reading of the
// name — so an arity door that counts it would tell the ordering key that `lambda s: 0-len(s)+base` needs two
// arguments and raise `missing 1 required positional argument: 'base'` for a body the reference calls with one
// (roadmap L11.7 · Gap R.376). One question — how many arguments must the source supply — one asking place,
// read by every door that decides whether a callable can be called with what it is given (ADR 0232).
func callableSuppliedParams(fd *FuncDef) []*Param {
	if fd == nil {
		return nil
	}
	if len(fd.Captures) == 0 {
		return fd.Params
	}
	out := make([]*Param, 0, len(fd.Params))
	for _, p := range fd.Params {
		if p == nil {
			continue
		}
		captured := false
		for _, c := range fd.Captures {
			if c == p.Name {
				captured = true
				break
			}
		}
		if captured {
			continue
		}
		out = append(out, p)
	}
	return out
}

// callableSiteOperand answers which expression names the function a call hands the parameter at `pos`: the
// argument the call wrote there, or — when the call left the position out — the parameter's own default,
// which is what the body sees there and therefore what the entry has to be (roadmap Gap R.329, ADR 0371).
func callableSiteOperand(c *Call, cf *callableFn, pos int) Expr {
	// The audit above is what makes the positional reading sound: a call that named any argument by
	// keyword is declined, so position `pos` of the argument list is position `pos` of the parameters.
	if pos < len(c.Args) {
		return c.Args[pos]
	}
	if pos >= 0 && pos < cf.srcParams && cf.def.Params[pos] != nil {
		return cf.def.Params[pos].Default
	}
	return nil
}

// callableSiteFill is the argument list the rewritten call gains so the clone is entered positionally up
// to the last parameter it calls. Every position the application left out below that point gets a word:
// the placeholder for a called one (the clone never reads the slot — it calls the entry by name), a copy of
// the default for a read one (which is what the call road's own default fill would evaluate). Positions at
// or above the last called one are left out: the call road reads their defaults unchanged (ADR 0322).
func callableSiteFill(c *Call, cf *callableFn) []Expr {
	last := -1
	for _, pos := range cf.calledPos {
		if pos > last {
			last = pos
		}
	}
	var out []Expr
	for pos := len(c.Args); pos <= last; pos++ {
		if callablePositionCalled(cf, pos) {
			out = append(out, &IntLit{Value: 0, Text: callablePlaceholderText})
			continue
		}
		if pos >= 0 && pos < len(cf.def.Params) && cf.def.Params[pos] != nil && cf.def.Params[pos].Default != nil {
			out = append(out, cf.def.Params[pos].Default)
		}
	}
	return out
}

// freshForwardWord mints the name of the PARAMETER a clone gains for a word it hands to the entries it
// dispatches on. The spelling is derived from the clone, from the name the host reads, and from a counter,
// and is asked through the same doors every other name this pass hands out goes through — not a name the
// program mentions, not a predeclared name, not one another clone took, and readable by the lexer so a
// traceback could spell it if it ever had to — plus the one question a PARAMETER has to answer that a symbol
// does not: no name the higher-order body owns may be the spelling of it, because a body that owns the name
// would then have its own read answer the word it is being handed. That question is why this word is minted
// at all: ADR 0395 spelled the parameter with the captured name, could only decline the collision, and left
// the reference's answer on the table (roadmap L11.7 · Gap R.375, ADR 0397).
func (p *callablePlan) freshForwardWord(def *FuncDef, cl *callableClone, from string) (string, bool) {
	h := fnv.New32a()
	fmt.Fprintf(h, "%s|%s", cl.name, from)
	for i := 0; i < 64; i++ {
		cand := fmt.Sprintf("gy%s%x_%d", callableForwardInfix, h.Sum32(), i)
		clash := !callableLexable(cand) || !strings.Contains(cand, callableForwardInfix)
		if ok, _ := p.nameIsFree(cand); !ok {
			clash = true
		}
		for _, f := range cl.forwarded {
			if f.word == cand {
				clash = true
			}
		}
		if callableCloneOwnsName(def, cand) {
			clash = true
		}
		if !clash {
			p.taken[cand] = true
			return cand, true
		}
		h.Write([]byte{byte(i)})
	}
	return "", false
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
	for _, h := range plan.hoistedHO {
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

// callableLambdaFrameRead asks which name, if any, keeps a lambda from being lifted out of the frame its
// APPLICATION is written in: a parameter of that frame, or a name the walk reached with it as the enclosing
// function — unless the program also binds the name at module level, which a hoisted `def` can read just as
// well as the lambda did. It answers the first such name, in the order the body reads them, so the refusal
// can point at the one word that has nowhere to go; "" and false mean the lambda lifts as it stands.
//
// The name is why this asks per name rather than yes-or-no. An entry is a symbol (ADR 0345), so the read has
// no word to travel in: the machinery that would fill it is the application road's capture parameter moved to
// the entry side, and that is roadmap Gap R.336 (ADR 0372).
func callableLambdaFrameRead(r *callableRead, l *Lambda, host *FuncDef) (string, bool) {
	if host == nil {
		return "", false
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
	for _, nm := range callableFreeNames(l) {
		if locals[nm] && !callableBoundAtModuleLevel(r, nm) {
			return nm, true
		}
	}
	return "", false
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

// callableModuleDeclarations is the census of names the module DECLARES at its top level — a `def` or a
// `class`, which the emitter emits as a symbol every body in the module can call. A name the module later
// REBINDS is not one (it is in `moduleEnvFor`'s binding set, which the capture door consults beside this), and
// a `def` nested inside another body is not one either: only the module's own statement list declares a global
// symbol (roadmap L11.7 · Gap R.378; ADR 0345 §1, a function is a symbol and not a word).
func callableModuleDeclarations(prog *Program) map[string]Node {
	out := map[string]Node{}
	if prog == nil {
		return out
	}
	for _, st := range prog.Stmts {
		switch x := st.(type) {
		case *FuncDef:
			if x != nil && x.Name != "" {
				out[x.Name] = x
			}
		case *ClassDef:
			if x != nil && x.Name != "" {
				out[x.Name] = x
			}
		}
	}
	return out
}

// callableFrameOwns is the set a frame OWNS: its parameters, everything its body BINDS — an assignment, a
// `for` target, a `with ... as` — and the targets of the comprehensions it hosts. It is the census
// `callableLambdaCaptures` uses to tell a name of THIS frame from a name of one further out (ADR 0245's lift,
// which this pass does not compose with), asked from one place (ADR 0232) because the hook road has to ask the
// same question to know whether a module-declared name is still the symbol the minted `define` can reach
// (roadmap L11.7 · Gap R.378): `def use(): helper = 3; … key=lambda x: helper(x)` reads the frame's number,
// and a door that answered with the module's function would be a plausible handle at the exit code of success.
func callableFrameOwns(enc *FuncDef) map[string]bool {
	own := map[string]bool{}
	if enc == nil {
		return own
	}
	for _, p := range enc.Params {
		if p != nil {
			own[p.Name] = true
		}
	}
	for nm := range bodyBoundNames(enc.Body) {
		own[nm] = true
	}
	collectLocals(enc.Body, own)
	callableCompTargets(enc.Body, own)
	return own
}

// callableLambdaCaptures is the capture list the CLOSURE road would hand this `lambda`, asked at the only
// moment it can be asked — before the clone is copied from the declaration this pass mints. It is the same
// question `lambdaFrameCaptures` asks at emission time (ADR 0370): every name the body reads without binding
// it, minus a name the MODULE binds (a generated `define` reaches module storage the way any other function
// does, so taking those as parameters would change what a program that compiles today passes across the
// boundary — Gap R.35 owns module state) and minus a predeclared name, which is a door and not a value.
//
// The second answer is the row's discipline: a name the ENCLOSING frame does not own is not this pass's to
// carry. `def outer(a): def inner(b): (lambda f: f(1) + a + b)(…)` reads `a` from two frames up, and the only
// machinery that reaches a name that far is ADR 0245's nested-`def` lift — which does not compose with this
// pass (AGENTS.md's L11.7 dependency), because the clone's call is a plain call and the lift's env chain is
// not in it. Declining there leaves the program to the ordinary road's sentence, which names the row that owns
// the missing half; answering it with a word that reads nothing would be ADR 0166's bug. The order is the
// sorted one the emission road uses, because a map iteration would mint a different `define` on every run
// (ADR 0273's determinism rule, which the pass's own hash test holds).
// callableCompTargets adds the targets a comprehension or generator in `node` binds to `out`: the frame that
// hosts a comprehension owns its target, and a `lambda` applied inside one reads that name from the same
// frame (the same scope rule `scanCompScope` and `lambdaFreeNames` already speak).
func callableCompTargets(node interface{}, out map[string]bool) {
	if node == nil {
		return
	}
	switch n := node.(type) {
	case nil:
		return
	case *Comp:
		// Every clause binds a name, and a scan that saw only the first reported the second as a free
		// name of the enclosing frame (roadmap Gap R.352).
		for _, t := range n.compTargets() {
			out[t.Value] = true
		}
	case *Generator:
		if n != nil && n.ForVar != nil {
			out[n.ForVar.Value] = true
		}
	case Node:
		v := reflect.ValueOf(n)
		if v.Kind() == reflect.Ptr && v.IsNil() {
			return
		}
		if v.Kind() == reflect.Slice || v.Kind() == reflect.Array {
			for i := 0; i < v.Len(); i++ {
				callableCompTargets(v.Index(i).Interface(), out)
			}
			return
		}
	}
	v := reflect.ValueOf(node)
	switch v.Kind() {
	case reflect.Ptr, reflect.Interface:
		if v.IsNil() {
			return
		}
		callableCompTargets(v.Elem().Interface(), out)
	case reflect.Slice, reflect.Array:
		for i := 0; i < v.Len(); i++ {
			callableCompTargets(v.Index(i).Interface(), out)
		}
	case reflect.Struct:
		t := v.Type()
		for i := 0; i < t.NumField(); i++ {
			f := v.Field(i)
			if !f.CanInterface() {
				continue
			}
			switch f.Kind() {
			case reflect.Ptr, reflect.Interface, reflect.Slice, reflect.Array, reflect.Struct:
				if f.IsZero() && f.Kind() != reflect.Struct {
					continue
				}
				callableCompTargets(f.Interface(), out)
			}
		}
	}
}

func callableLambdaCaptures(enc *FuncDef, prog *Program, l *Lambda) ([]string, bool) {
	if l == nil || l.Body == nil || prog == nil {
		return nil, true
	}
	_, moduleNames := moduleEnvFor(prog)
	own := callableFrameOwns(enc)
	declared := callableModuleDeclarations(prog)
	// The reads that are NOT calls of the name read: one walk with the callee positions marked out, so the
	// difference between "reads" and "calls" costs one set rather than a second AST. A name whose every
	// occurrence is a call is the shape the minted `define` can answer BY SYMBOL, which is what a call of a
	// declared name lowers to in any other body of this program; a read anywhere else — printed, stored,
	// handed to another call, the base of an attribute — is the body wanting a function VALUE, the half no
	// word can carry (roadmap L11.7 · Gap R.378, Gap R.297).
	valueReads := callableFreeNameSet(l, callableCalleeNameNodes(l))
	seen := map[string]bool{}
	out := []string{}
	for _, nm := range callableFreeNames(l) {
		if isPredeclaredName(nm) || moduleNames[nm] || seen[nm] {
			continue
		}
		// A name the MODULE DECLARES — a top-level `def` or `class` the module never rebinds (a rebound one is
		// already in `moduleNames`, consulted above) — needs no word: the minted `define` reaches it the way any
		// other body in the program does, by SYMBOL. `def helper(v): return 0 - v` above
		// `print(sorted([3, 1, 2], key=lambda x: helper(x)))` is that program — the reference's `[3, 2, 1]`, and
		// here a refusal, because a capture merely NAMED a declared function. That was the right verdict about the
		// wrong thing: a function is a symbol rather than a value (ADR 0345 §1), so the thing no word can carry is
		// a function READ AS A VALUE — printed, stored, passed on, the base of an attribute — which is what the
		// second answer still declines (roadmap L11.7 · **Gap R.378**; Gap R.297 owns the value half).
		//
		// The frame's OWN binding wins the question. `def use(): helper = 3; … key=lambda x: helper(x)` reads the
		// frame's number, so a name the frame owns is never answered with the module's symbol: that would be a
		// plausible handle at the exit code of success. It stays a capture, and the one-word gate
		// (`captureCarriesOneWord`, Gap R.332) decides it, asked of the frame that owns the value — ADR 0395 §4's
		// rule, at a third road.
		if decl, isDeclared := declared[nm]; isDeclared && !own[nm] {
			if valueReads[nm] {
				return nil, false
			}
			// DECLARED BEFORE THE BODY THAT CALLS IT, which is the rule the front end already enforces of an
			// ordinary call (`undefined name "helper"` for `print(helper(1))` above `def helper(v)`, where
			// CPython answers NameError) and which a lambda body is not asked. A hoisted body that called a name
			// declared further down the module would answer where the reference raises, so the shape keeps the
			// door's own refusal (roadmap L11.7 · Gap R.378, measured as the fourth probe of its family).
			if decl.Span().Line > l.Span().Line {
				return nil, false
			}
			continue
		}
		// At module level (no enclosing `def`) the only names a body can be free in are the ones an
		// enclosing comprehension or `for` binds around the application, and the road that lowers those
		// already reads them at the call; inside a `def`, a name the frame does not own is the nested-`def`
		// lift's, and this pass declines it.
		if enc != nil && !own[nm] {
			return nil, false
		}
		seen[nm] = true
		out = append(out, nm)
	}
	sortStrings(out)
	return out, true
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
	out := callableFreeNameSet(l, nil)
	var names []string
	for nm := range out {
		names = append(names, nm)
	}
	sortStrings(names)
	return names
}

// callableFreeNameSet is the walk behind `callableFreeNames`, answering the set directly and letting the
// caller mark occurrences to ignore. `skip` is keyed by the NODE, not by the spelling: the hook road hands it
// the `*Name` nodes that stand in a callee position (roadmap L11.7 · Gap R.378), which is how one walk can
// answer both 「what does this body read?」 and 「what does this body read OTHER than calling?」 — the
// difference between the two is a name read as a VALUE, and a name spelled twice (called here, read there)
// appears in both sets, which is what makes the second question answerable at all.
func callableFreeNameSet(l *Lambda, skip map[*Name]bool) map[string]bool {
	out := map[string]bool{}
	if l == nil || l.Body == nil {
		return out
	}
	bound := map[string]bool{}
	for _, p := range l.Params {
		if p != nil {
			bound[p.Name] = true
		}
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
			if skip[x] {
				return
			}
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
			for _, cl := range x.compClauses() {
				walkNode(cl.Iter, b)
				walkNode(cl.Cond, b)
				if cl.Target != nil {
					delete(out, cl.Target.Value)
				}
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
	return out
}

// callableCalleeNameNodes collects the `*Name` nodes that stand in a CALL position inside a lambda body —
// `helper` in `helper(x)`, not `helper` in `print(helper)`, and not the callee of `helper.m()`, whose base is
// a read of the name itself. The pointers, not the spellings, because a body can spell one name twice in both
// positions and the two occurrences have different answers (roadmap L11.7 · Gap R.378).
func callableCalleeNameNodes(l *Lambda) map[*Name]bool {
	out := map[*Name]bool{}
	if l == nil || l.Body == nil {
		return out
	}
	w := callableWalker(func(n Node, enc *FuncDef) {
		if c, ok := n.(*Call); ok && c != nil {
			if fn, isName := c.Fn.(*Name); isName && fn != nil {
				out[fn] = true
			}
		}
	})
	w.expr(l.Body, nil)
	return out
}

// callableDisplayNameOf is the name a minted declaration is spelled FROM — the enclosing frame's, or
// "module" at the top level — which is a different question from the name a READER meets a function under
// (`callableDisplayName`, the source's own spelling).
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

// defunctionalizeCallablesTelling runs the pass and hands back, beside the program the emitter is to
// lower, the sentence the pass has to say about THAT program: "" when it rewrote the tree — there is
// nothing to explain about a rewrite — and when its only answer was that the program held no work for
// it. Everything else is a shape the pass wanted to build, could not prove, and withdrew from; the
// emitter is about to refuse a call that rewrite would have built, and what the pass knew at that
// moment is more than the emitter can know on its own (roadmap L11.7 · Gap R.337, ADR 0379).
//
// The sentence travels as a VALUE, next to the program it describes, for the same reason ADR 0343
// made the toolchain and the name-readers values: a package global holding it would let one compile
// answer another's refusal, and the suite runs programs beside each other.
func defunctionalizeCallablesTelling(prog *Program) (*Program, string) {
	plan, err := planDefunctionalization(prog)
	if err != nil {
		if callableDeclineIsNoWork(err) {
			return prog, ""
		}
		return prog, err.Error()
	}
	out, rep := writeDefunctionalization(prog, plan)
	if rep == nil {
		// The plan was made and the rewrite pulled back. The pass's own account calls this the
		// withdrawal it is, in the same words `--callables` prints, so one run cannot tell two stories.
		return out, callableRewriteWithdrawn().Error()
	}
	return out, ""
}

// applyDefunctionalization returns the program codegen should see, plus the pass's account of what
// it did. A decline returns the caller's program unchanged, and a nil report: the pass's absence is
// always representable, which is what makes it safe to run unconditionally.
func applyDefunctionalization(prog *Program) (*Program, *callableReport) {
	plan, err := planDefunctionalization(prog)
	if err != nil {
		return prog, nil
	}
	return writeDefunctionalization(prog, plan)
}

// writeDefunctionalization performs the plan the reading produced. Every audit below that fails
// returns the CALLER's program and a nil report rather than a half-rewritten tree, which is why the
// pass can run on a program it has never seen before and cost nothing when it is wrong about it.
func writeDefunctionalization(prog *Program, plan *callablePlan) (*Program, *callableReport) {
	fresh := copyProgramAST(prog)
	if fresh == nil {
		return prog, nil
	}
	report := &callableReport{Tool: callableToolName, Pass: "defunctionalize-callables", CarriersDeclined: map[string]int{}}

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
			// The words this clone supplies to its entries (roadmap L11.7 · Gap R.336). They are parameters of
			// the clone, filled at the application by the CALL road from the frame the application is written
			// in — the same `Captures` mechanism ADR 0370 built for an applied `lambda`, asked of the clone
			// that dispatches on a capturing entry — and the body's call of the entry is where they are handed
			// on. APPENDED, never replaced: a clone of an applied `lambda` already carries the words the
			// LAMBDA reads from the frame of its own application (ADR 0370), and those two lists are the body's
			// supply and the entries' — overwriting one with the other loses an argument the callee still has.
			//
			// The PARAMETER is spelled with a name this pass owns and the VALUE is read from the name the
			// program wrote, which is the pair Gap R.375 asked for: the host's tables can only be asked about
			// the spelling the source used, and the clone's frame can only be given a slot the source cannot
			// have bound for itself.
			appendForwardedWords(clone, cl.forwarded, cf.def.Src)
			// The count is the clone's own list, read AFTER the append: a clone of an applied `lambda` brings
			// the body's captures with it (ADR 0370), a clone of a `def` brings only the entries' (this row),
			// and the account reports the words the pass wrote onto the declarations it minted — both kinds.
			report.CaptureParameters += len(clone.Captures)
			if cf.fromLambda != nil {
				// The reference names an anonymous function `<lambda>` however it reached the call, so a
				// clone of an applied lambda is named that for every reader — the traceback frame, the line
				// table, the callable in a diagnostic — and never the symbol this pass minted (ADR 0345 §3).
				clone.CloneOf = callableAnonymousName
			}
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
				// The entry's own words, handed over positionally: the clone reads each one out of the
				// parameter the application filled it with. Positional rather than by name on purpose — the
				// hoisted entry carries no `Captures` field, because the caller here is a frame the pass itself
				// built and the word it holds was handed down rather than reached for (Gap R.336). The name read
				// is the clone's own spelling of the word, never the program's: in the collision shape the body
				// owns that spelling, and reading it here would hand the entry the body's value (Gap R.375).
				for _, c := range plan.entryCaptures[cl.key[in.slot]] {
					word := callableForwardWordOf(cl, c)
					if word == "" {
						return prog, nil
					}
					callee.Args = append(callee.Args, &Name{Value: word, Src: callee.Fn.Span()})
				}
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
				lastCalled := -1
				for _, pos := range cf.calledPos {
					if pos > lastCalled {
						lastCalled = pos
					}
				}
				// A recursive call may leave positions out below the last slot the clone dispatches on.
				// A called one inherits the enclosing clone's entry, so the argument the clone receives is
				// the placeholder it never reads; an intervening ordinary slot takes the default the call
				// road would have evaluated for an omission (roadmap Gap R.335, beside ADR 0371).
				for pos := len(rec.Args); pos <= lastCalled; pos++ {
					if pos < 0 || pos >= len(cf.def.Params) {
						return prog, nil
					}
					if callablePositionCalled(cf, pos) {
						if cf.def.Params[pos].Default == nil {
							return prog, nil
						}
						rec.Args = append(rec.Args, &IntLit{Value: 0, Text: callablePlaceholderText, Src: cf.def.Params[pos].Src})
						continue
					}
					if cf.def.Params[pos] == nil || cf.def.Params[pos].Default == nil {
						return prog, nil
					}
					copied := copyASTExpr(cf.def.Params[pos].Default)
					if copied == nil {
						return prog, nil
					}
					rec.Args = append(rec.Args, copied)
				}
				for _, pos := range cf.calledPos {
					if pos < 0 || pos >= len(rec.Args) {
						return prog, nil
					}
					rec.Args[pos] = &IntLit{Value: 0, Text: callablePlaceholderText, Src: rec.Args[pos].Span()}
				}
				// The forwarded words travel down the recursion beside the entry tuple. The clone is not the
				// frame that owns them — the host that picked this clone is — so the recursive call hands on the
				// parameter it was given, spelled with the name this pass minted for it; and because the tree now
				// says so, the call road leaves that position alone instead of reaching for a name the clone's
				// own body may own (roadmap L11.7 · Gap R.375, beside ADR 0385's rule for the entry itself).
				for _, f := range cl.forwarded {
					if f.word == "" || f.from == "" {
						return prog, nil
					}
					rec.Args = append(rec.Args, &KeywordArg{
						Name:  f.word,
						Value: &Name{Value: f.word, Src: rec.Args[0].Span()},
						Src:   rec.Args[0].Span(),
					})
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
			if !hit || len(node.Args) > cf.srcParams {
				return prog, nil
			}
			// The application may have left parameters out; the call the clone sees gains a word for each of
			// them up to the last one it calls — the placeholder for a called slot, which the clone never
			// reads, and a copy of the default's own expression for a slot the body reads, which is what the
			// call road would have evaluated for an omitted argument anyway (roadmap Gap R.329, ADR 0322,
			// ADR 0371). The new list is built and audited before anything is written, so a site this pass
			// cannot finish leaves the program exactly as it was rather than half-rewritten.
			grown := make([]Expr, 0, len(node.Args)+len(site.grow))
			grown = append(grown, node.Args...)
			for _, e := range site.grow {
				copied := copyASTExpr(e)
				if copied == nil {
					return prog, nil
				}
				grown = append(grown, copied)
			}
			for _, pos := range cf.calledPos {
				if pos >= len(grown) || grown[pos] == nil {
					return prog, nil
				}
			}
			node.Args = grown
			node.Fn = &Name{Value: cl.name, Src: node.Fn.Span()}
			for _, pos := range cf.calledPos {
				node.Args[pos] = &IntLit{Value: 0, Text: callablePlaceholderText, Src: node.Span()}
			}
			report.RewrittenCallNum++
			report.OmittedArguments += site.omitted
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
		// The words this hook reads out of the frame the call is written in are parameters of the declaration
		// this pass minted, and the account reports every word it wrote onto a minted declaration — a clone's
		// and a hook's (roadmap L11.7 · Gap R.376; the field's own wording is in docs/operations.md).
		report.CaptureParameters += h.captures
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

	// (3c) The hook's key: the option the door calls is pointed at the callable's own symbol, so the
	// road underneath it meets a name with a declaration behind it (roadmap L11.7, Gap R.281). The
	// keyword itself survives — the door reads its options by NAME, and a rewrite that flattened
	// `key=f` into a positional argument would move the program past the reference's own reading of it
	// (ADR 0340).
	for _, kr := range plan.keyRets {
		if kr.rank < 0 || kr.rank >= len(programCalls) {
			return prog, nil
		}
		node := programCalls[kr.rank]
		if kr.pos < 0 || kr.pos >= len(node.Args) {
			return prog, nil
		}
		kw, isKw := node.Args[kr.pos].(*KeywordArg)
		if !isKw || kw.Name != "key" {
			return prog, nil
		}
		nm, isName := kw.Value.(*Name)
		if !isName || nm.Value != kr.from {
			return prog, nil
		}
		redo := *kw
		redo.Value = &Name{Value: kr.to, Src: kw.Value.Span()}
		node.Args[kr.pos] = &redo
		report.KeyArguments++
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
	// The carrier gate's own half of the same statement: a call the roads declined because it stands in an
	// `except` handler keeps the name the program wrote, and a name whose definition this pass removed is a
	// call to a `define` that will not be emitted (roadmap Gap R.389, and the invariant ADR 0345 closes).
	if plan.carrierCrossesAClone() {
		return prog, nil
	}
	for _, cf := range plan.order {
		// A reader meets a clone by the name the source wrote, and a lambda has no name in the source:
		// the reference's own word for it is `<lambda>`, and that is what the account, the traceback and
		// the `--lang` lines print. The minted symbol stays where a machine wants it — `symbols`
		// (roadmap L11.7, ADR 0320's rule for a rewritten definition, ADR 0370).
		if cf.fromLambda != nil {
			report.HigherOrderFunc = append(report.HigherOrderFunc, "<lambda>")
			report.HigherOrderLambdas++
		} else {
			report.HigherOrderFunc = append(report.HigherOrderFunc, callableDisplayName(cf.def))
		}
		for _, cl := range cf.clones {
			report.Symbols = append(report.Symbols, cl.name)
		}
	}
	for _, h := range plan.hoisted {
		if h != nil {
			report.Symbols = append(report.Symbols, h.Name)
		}
	}
	sortStrings(report.Symbols)
	if len(plan.carriersDeclined) > 0 {
		report.CarriersDeclined = map[string]int{}
		for kind, n := range plan.carriersDeclined {
			report.CarriersDeclined[kind] = n
		}
	}
	for _, why := range plan.carriersDeclineWhy {
		// The account says which positions the reader saw and the rewrite refused, in the same list a script
		// already reads for the shapes the pass could not prove (roadmap Gap R.389).
		report.Refused = append(report.Refused, why)
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
		if check.calleeNames[n] {
			continue
		}
		// The one position that is a call without looking like one: a hook's `key=`. The ordering door
		// calls the key once per element, so a name written there is being called and not read — but only
		// where the door will actually make that call, which is `keyDoorCallsOneArgument`, the rule the
		// door and the parameter census already read (a key the door raises for — `f() takes 0 positional
		// arguments but 1 was given` — is not a call), asked of the program the pass produced so a symbol
		// this pass minted is in front of it. A program that names such a key still reads as a function
		// reaching a value position (roadmap L11.7, Gap R.281, Gap R.264).
		if !check.keySlots[n] || !keyDoorCallsOneArgument(check.fds[n.Value]) {
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
	doc := callableReport{Tool: callableToolName, Pass: "defunctionalize-callables", Hash: "none", CarriersDeclined: map[string]int{}}
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
		// A decline loses the plan, and with it the record of what the reader saw. The account is still
		// written from the READ, because the reader's verdict is the half a caller needs when there is no
		// rewrite to describe: "the pass declined, and here is the position it saw and would not cross"
		// (roadmap Gap R.383 for the read, Gap R.389 for the restraint).
		for kind, lines := range callableDeclinedCarrierSites(prog) {
			doc.CarriersDeclined[kind] = len(lines)
			doc.Refused = append(doc.Refused, callableCarrierDeclineSentence(kind, lines[0]))
		}
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
		// One sentence, one owner: the emitter's refusal appends exactly this text beside the call it
		// could not build, so a reader who asks both in one run reads one story (Gap R.337, ADR 0379).
		doc.Refused = append(doc.Refused, callableRewriteWithdrawn().Error())
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
		// `higher_order_functions` carries the READER's names — `<lambda>` for a cloned lambda, which is
		// how a human and an agent alike should see it — so it cannot be the whole of a fingerprint. The
		// minted symbols are what make two different programs hash apart, and they are in the document
		// already (roadmap L11.7, ADR 0345's determinism claim, ADR 0370).
		fmt.Fprintf(h, "%s|%d|%d|%d|%d|%d|%s|%s", rep.Pass, rep.Clones, rep.HoistedLambdas, rep.RewrittenCallNum,
			rep.RecursiveSites, rep.KeyArguments, strings.Join(rep.HigherOrderFunc, ","), strings.Join(rep.Symbols, ","))
	}
	return fmt.Sprintf("%016x", h.Sum64())
}
