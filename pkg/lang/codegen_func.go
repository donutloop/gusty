// Functions: the symbol a FuncDef is emitted under, its prologue and epilogue, its return
// road, and the questions the call roads ask about what it answers — including
// callUserFunc, the road a call to one of the program's own functions takes, which lives
// here because it is the other half of funcDef.
package lang

import (
	"fmt"
	"reflect"
	"strings"
)

// emitLambda registers a lambda as a generated anonymous FuncDef and emits
// its closure IR, returning the generated function name so Call can invoke it.
//
// The generated `define` lives at module level, because a `define` cannot nest inside the `define`
// the lambda is written in — so a name the lambda reads from the frame that encloses it has nowhere
// to be read from, and `(lambda y: y * 2)(x)` written in a body that binds `x` died with
// `undefined name "x"` where the reference answers `8` (roadmap L11.7, Gap R.265). The name is
// carried the way a nested `def` carries its captures: the question is the one closureInfoFor
// already asks, and the answer arrives as one extra parameter per captured name, filled by the call
// road (lambdaCallArgs) with the caller's own reading of that name. A lambda that reads nothing
// outside itself gains nothing and is emitted exactly as it always was.
// lambdaFuncDef is the one spelling of what a `lambda` becomes: an anonymous declaration whose body is a
// single `return` of the lambda's expression. Both roads that need to ask a question of a lambda's body —
// the one that emits its `define` and the one that asks, from a call site, what that `define` answers with
// — build it here, so the two cannot ask about different programs (roadmap L11.7, Gap R.265, ADR 0370).
func lambdaFuncDef(lam *Lambda) *FuncDef {
	return &FuncDef{
		Params: append([]*Param(nil), lam.Params...),
		Body:   []Stmt{&ReturnStmt{Expr: lam.Body}},
	}
}

func (g *irGen) emitLambda(b *strings.Builder, lam *Lambda) (string, error) {
	name := fmt.Sprintf("lambda_%d", g.lambdaCounter)
	g.lambdaCounter++
	fd := lambdaFuncDef(lam)
	fd.Name = name
	captured := g.lambdaFrameCaptures(lam)
	// The captured names are asked of the frame that owns them BEFORE its `define` is emitted: a name the
	// print door and the arithmetic roads describe as anything but a plain number has no word to cross
	// with, and asking after the body has been lowered lets the body's own refusal — written as if the
	// name were a local of the lambda — be the sentence the program reads.
	for _, c := range captured {
		kind, carries := g.nameCarriesOneWord(c)
		if !carries {
			return "", captureWordErr(c, kind, name+"()")
		}
		// The verdict is the middle frame's own answer about the name it forwards: a body applied inside
		// this one asks what `x` is, `x` is a capture parameter of this frame, and no call the source wrote
		// names that position. What the frame around the application said — asked HERE, while that frame is
		// still on the tables — is the only answer there is (roadmap L11.7 · Gap R.330).
		g.recordCaptureVerdict(fd, c, kind, carries)
	}
	if len(captured) > 0 {
		for _, c := range captured {
			fd.Params = append(fd.Params, &Param{Name: c, Src: lam.Span()})
		}
		if g.lambdaCaptures == nil {
			g.lambdaCaptures = map[string][]string{}
		}
		g.lambdaCaptures[name] = captured
	}
	// The generated function is registered BEFORE its body is emitted, because the body may itself apply a
	// lambda, and the question that application's capture asks — what do this frame's calls hand to that
	// parameter? — is answered by looking the frame up in the table the call roads read. No call the source
	// wrote can name `lambda_N`, so registering early cannot redirect anything.
	if g.funcs == nil {
		g.funcs = map[string]bool{}
	}
	g.funcs[name] = true
	if g.fds == nil {
		g.fds = map[string]*FuncDef{}
	}
	g.fds[name] = fd
	if g.lambdaOrigin == nil {
		g.lambdaOrigin = map[*FuncDef]*Lambda{}
	}
	g.lambdaOrigin[fd] = lam
	// emit the lambda FuncDef at module level (globals), not the current
	// statement builder, so the `define` is not nested inside main.
	//
	// The nested emission does not own the frame it was called from, and it used to take that frame with it:
	// `funcDef` ends by clearing the parameter table, the current function, the environment captures and the
	// root-frame flag, so the statement that contained the lambda went on reading its OWN frame through empty
	// tables — `(lambda y: y * 2)(x)` inside a body that binds `x` died with `undefined name "x"`, the name the
	// reference hands to the call (roadmap L11.7, Gap R.265). The frame goes in and comes back out unchanged,
	// the way `emitClosureDef` already treats the frame it emits a closure from.
	frame := g.enterFrame()
	// A `lambda` that answers a double may only be emitted where the frame around it can answer one. A
	// plain `def` can — `funcDef` reads the body's own answer and emits `define double` — but a closure
	// body and a method body are emitted as `i32` functions whatever their bodies do, so the double the
	// lambda answers would be returned from an `i32` function and `llc` would reject the module: exit 2,
	// the code ADR 0166 reserves for a compiler bug. Saying which word is missing is the alternative
	// (roadmap L11.6, Gap R.265, Gap R.276, ADR 0370).
	if funcReturnsFloat(g, fd) && !lambdaFrameAnswersDoubles(g) {
		kind := "a closure"
		if g.emittingMethod {
			kind = "a method"
		}
		return "", fmt.Errorf("codegen: %s answers a float, and it is applied inside %s, which answers one i32 word whatever its body does: the double has no word to travel back in, and the module that tried would be rejected by llc (ADR 0166). Give the body a plain `def` of its own and call that; the one-word answer of %s is roadmap L11.6, and this row is L11.7 · Gap R.265, Gap R.276", name, kind, kind)
	}
	// The generated `define` goes into a buffer of its own and joins the module only once it is complete.
	// Writing it into `g.globals` as soon as it was ready — which is what the road used to do, so the
	// `define` is not nested inside `main` — was right exactly once per function: the roads that emit a body
	// themselves (the class road's methods, the closure lift's nested `def`s) are ALSO writing into
	// `g.globals`, so a lambda applied inside one appended its `define` into the MIDDLE of the enclosing
	// `define`'s text, and `llc` reported `multiple definition of local value named 'gc.frame.base'` at
	// exit 2 for a program the reference runs (roadmap L11.7, Gap R.265; ADR 0166 reserves exit 2 for a
	// compiler bug). They wait in `g.lambdaDefines` instead, which is written beside the other
	// module-level text.
	var def strings.Builder
	err := g.funcDef(&def, fd)
	frame.leave(g)
	if err != nil {
		return "", err
	}
	g.lambdaDefines.WriteString(def.String())
	return name, nil
}

// lambdaFrameAnswersDoubles reports whether the frame a `lambda` is being emitted in can answer with a
// double at all. Only the `funcDef` road reads a body's answer and chooses the function's word; the
// closure lift and the class road emit an `i32` define unconditionally.
func lambdaFrameAnswersDoubles(g *irGen) bool {
	return !g.envMode && !g.emittingMethod
}

// lambdaFrameState is the emitting frame's own state, saved around the emission of a `define` the
// frame does not own and restored when that emission is over. `funcDef` is written to be the outermost
// road for one function: it clears the parameter table, the current function, the generator handle, the
// environment captures and the root-frame flag on its way out, all of which belong to whoever called it.
// The nested-`def` road has always restored them (`emitClosureDef`'s `savedFrame`); this is the same
// courtesy, kept by the lambda road, which emits a `define` in the middle of a body (roadmap L11.7,
// Gap R.265).
type lambdaFrameState struct {
	params      map[string]string
	paramSlot   map[string]bool
	closures    map[string]*closureInfo
	decorated   map[string]bool
	envMode     bool
	envCaptures map[string]int
	envParam    string
	inFunc      bool
	curFunc     string
	curFD       *FuncDef
	method      bool
	genHandle   string
	frameOpen   bool
}

func (g *irGen) enterFrame() lambdaFrameState {
	return lambdaFrameState{
		params: g.params, paramSlot: g.paramSlot,
		closures: g.closures, decorated: g.decorated,
		envMode: g.envMode, envCaptures: g.envCaptures, envParam: g.envParam,
		inFunc: g.inFunc, curFunc: g.curFunc, curFD: g.curFD, genHandle: g.genHandle, frameOpen: g.frameOpen,
		method: g.emittingMethod,
	}
}

func (f lambdaFrameState) leave(g *irGen) {
	g.params, g.paramSlot = f.params, f.paramSlot
	g.closures, g.decorated = f.closures, f.decorated
	g.envMode, g.envCaptures, g.envParam = f.envMode, f.envCaptures, f.envParam
	g.inFunc, g.curFunc, g.genHandle, g.frameOpen = f.inFunc, f.curFunc, f.genHandle, f.frameOpen
	g.curFD = f.curFD
	g.emittingMethod = f.method
}

// lambdaFrameCaptures answers which names an applied lambda reads from outside itself — the names its body
// is free in. It is the closure question the nested-`def` road asks (`closureInfoFor`), asked with the
// binding rule rather than the table rule: `closureInfoFor` keeps only the names the *enclosing frame's*
// tables already list, which is the right filter for a `def` whose captures the frame hands it, and the
// wrong one for a lambda, whose enclosing frame is a `define` that has no table for the names it owns.
// `(lambda y: (lambda z: x + y + z)(100))(1)` is the case the difference decides: the inner lambda is free
// in `x`, the frame two levels up binds it, and filtering by the frame's tables deleted the capture and
// left `undefined name "x"` where the reference answers `111` (roadmap L11.7, Gap R.265, Gap R.276).
//
// What the caller's road then does with the name is the frame's own business: it reads it the way the frame
// reads it anywhere, and a name the frame does not bind is the program's error to report, in the same
// words the reference uses.
//
// A name the module binds is NOT a capture: a generated `define` reaches module storage the way any
// other function does, and taking those names as parameters would change what a program that
// compiles today passes across the boundary (Gap R.35 is the row that owns module state).
//
// What the module does NOT bind, and no further frame owns either, is the name the frame hosting the
// `lambda` binds for ITSELF — today a comprehension's target. That name has no module storage to read
// and no `define` but its own to hold it, so at the top level it reaches the body the same way a
// `for` variable reaches a nested `def`: as a capture the application fills from the host's slot
// (roadmap L11.7 · **Gap R.348**; `frame_targets.go` is the table, and the comprehension road is what
// writes it). `x = 5` then `print([(lambda z: z + x)(1) for x in [10, 20]])` is the case the ordering
// decides: the nearer binding is the one the read sees, so the target outranks the module spelling
// rather than being answered by it.
func (g *irGen) lambdaFrameCaptures(lam *Lambda) []string {
	if lam == nil || lam.Body == nil {
		return nil
	}
	free := map[string]bool{}
	lambdaFreeNames(lam, free)
	out := []string{}
	for _, c := range sortedSet(free) {
		if isPredeclaredName(c) {
			continue
		}
		if g.frameOwnsTarget(c) {
			out = append(out, c)
			continue
		}
		// Nothing the frame owns, and nothing a frame below the module owns either: at the top level
		// there is no such name, because the module's own bindings are the only storage a body
		// emitted there could read.
		if !g.inFunc || g.moduleNames[c] {
			continue
		}
		out = append(out, c)
	}
	return out
}

// sortedSet lists a name set in the order the run must see it in: the capture parameters are appended in
// this order, and a map iteration would mint a different `define` on every run (ADR 0273's determinism
// rule, which the pass's own hash test holds).
func sortedSet(set map[string]bool) []string {
	out := make([]string, 0, len(set))
	for k := range set {
		out = append(out, k)
	}
	sortStrings(out)
	return out
}

// lambdaFreeNames collects the names a `lambda` reads without binding them, into `out`. The scan is
// scope-aware in the one way that matters here: a nested binding form — another `lambda`, a comprehension,
// a nested `def` — owns its own parameters and targets, and nothing bound in there is a capture of this
// lambda. A nested `def` is not scanned at all: its body is that `def`'s scope, and the closure lift (ADR
// 0245) is the road that carries names into it.
func lambdaFreeNames(lam *Lambda, out map[string]bool) {
	if lam == nil {
		return
	}
	bound := map[string]bool{}
	for _, p := range lam.Params {
		if p != nil {
			bound[p.Name] = true
		}
	}
	scanFreeIn(lam.Body, bound, out)
}

func scanFreeIn(n Node, bound, out map[string]bool) {
	if n == nil || reflect.ValueOf(n).IsNil() {
		return
	}
	switch x := n.(type) {
	case *Name:
		if !bound[x.Value] {
			out[x.Value] = true
		}
		return
	case *Lambda:
		// A nested `lambda` binds its OWN parameters, so what it reads is not automatically what this body
		// is free in: the names the nested body reaches have to be filtered against the binders this body
		// already has, or `(lambda y: (lambda z: x + y + z)(100))` reports `y` as free — a name the middle
		// frame spells as a parameter — and the capture road appends a SECOND parameter of that spelling to
		// the `define`, which the application then fills twice and `callUserFunc` refuses as `multiple
		// values for argument "y"` (roadmap L11.7 · Gap R.330).
		inner := map[string]bool{}
		lambdaFreeNames(x, inner)
		for nm := range inner {
			if !bound[nm] {
				out[nm] = true
			}
		}
		return
	case *FuncDef, *ClassDef:
		return // another declaration's own scope
	case *Comp:
		scanCompClauseList(x, x.Elems, x.Keys, x.Vals, bound, out)
		return
	case *Generator:
		scanCompScope(x.Iter, x.ForVar, x.Elems, nil, nil, x.Cond, bound, out)
		return
	}
	v := reflect.ValueOf(n)
	if v.Kind() != reflect.Ptr || v.IsNil() {
		return
	}
	e := v.Elem()
	if e.Kind() != reflect.Struct {
		return
	}
	t := e.Type()
	for i := 0; i < t.NumField(); i++ {
		f := e.Field(i)
		switch f.Kind() {
		case reflect.Slice, reflect.Array:
			if !nodeField(f.Type().Elem()) {
				continue
			}
			for k := 0; k < f.Len(); k++ {
				if !f.Index(k).IsNil() {
					scanFreeIn(f.Index(k).Interface().(Node), bound, out)
				}
			}
		case reflect.Interface, reflect.Ptr:
			if f.IsNil() || !nodeField(f.Type()) {
				continue
			}
			scanFreeIn(f.Interface().(Node), bound, out)
		}
	}
}

// scanCompScope walks a comprehension's parts with its own target bound: the iterable the loop ranges over
// is read outside that binding, and everything else is read inside it.
func scanCompScope(iter Expr, forVar *Name, elems, keys, vals []Expr, cond Expr, bound, out map[string]bool) {
	scanFreeIn(iter, bound, out)
	inner := childScope(bound)
	if forVar != nil {
		inner[forVar.Value] = true
	}
	for _, e := range elems {
		scanFreeIn(e, inner, out)
	}
	for _, k := range keys {
		scanFreeIn(k, inner, out)
	}
	for _, v := range vals {
		scanFreeIn(v, inner, out)
	}
	scanFreeIn(cond, inner, out)
}

// scanCompClauseList is the same scan asked of a CLAUSE LIST. Each clause's iterable is scanned in the
// scope the EARLIER clauses' targets already populate — `for y in x` reads `x`, and `x` is bound by the
// clause to its left, so it is not a free name the enclosing frame has to supply — and each target joins
// the scope before that clause's own filter and, at the end, the element are scanned (roadmap Gap R.352:
// a scan that knew only the first target reported `undefined name "y"` for the flattening idiom).
func scanCompClauseList(c *Comp, elems, keys, vals []Expr, bound, out map[string]bool) {
	inner := childScope(bound)
	for _, cl := range c.compClauses() {
		scanFreeIn(cl.Iter, inner, out)
		if cl.Target != nil {
			inner[cl.Target.Value] = true
		}
		scanFreeIn(cl.Cond, inner, out)
	}
	for _, e := range elems {
		scanFreeIn(e, inner, out)
	}
	for _, k := range keys {
		scanFreeIn(k, inner, out)
	}
	for _, v := range vals {
		scanFreeIn(v, inner, out)
	}
}

func childScope(bound map[string]bool) map[string]bool {
	inner := make(map[string]bool, len(bound)+2)
	for k, v := range bound {
		inner[k] = v
	}
	return inner
}

var nodeInterfaceType = reflect.TypeOf((*Node)(nil)).Elem()

// nodeField reports whether a struct field can hold an AST node. `*Type`, `Span` and `string` cannot,
// which is what keeps the scan from wandering into the annotation grammar.
func nodeField(t reflect.Type) bool {
	return t.Kind() == reflect.Interface && t.Implements(nodeInterfaceType)
}

// lookupFuncDef resolves a called name to its definition so the bool predicate can read the
// callee's own returns: what a function returns is determined by what its body does
// (ADR 0254), which is also how the string- and float-returning tables are built.
func (g *irGen) lookupFuncDef(name string) *FuncDef {
	if fd, ok := g.fds[name]; ok {
		return fd
	}
	return nil
}

// fdReturnsValue reports whether a function can produce a value: any `return <expr>`
// anywhere in the body, or any `yield` (a generator evaluates to the list of yielded
// values). It is used to decide statically whether a call yields None, so the search is
// deliberately exhaustive — a hand-enumerated statement walk once mistook a `return`
// nested inside `match` for a procedure and printed None instead of the value (ADR 0172).
func fdReturnsValue(fd *FuncDef) bool {
	if fd == nil {
		return false
	}
	if containsYield(fd.Body) {
		return true
	}
	return hasValueReturn(reflect.ValueOf(fd.Body))
}
func hasValueReturn(v reflect.Value) bool {
	switch v.Kind() {
	case reflect.Invalid:
		return false
	case reflect.Interface, reflect.Ptr:
		if v.IsNil() {
			return false
		}
		if rs, ok := v.Interface().(*ReturnStmt); ok {
			return rs.Expr != nil
		}
		return hasValueReturn(v.Elem())
	case reflect.Slice, reflect.Array:
		for i := 0; i < v.Len(); i++ {
			if hasValueReturn(v.Index(i)) {
				return true
			}
		}
		return false
	case reflect.Struct:
		for i := 0; i < v.NumField(); i++ {
			if hasValueReturn(v.Field(i)) {
				return true
			}
		}
		return false
	default:
		return false
	}
}

// floatReboundReturnAsksForTheDouble asks the body the question its own instructions are about to
// ask: does some `return` expression read as a double once the names the body rebinds to a float are
// read as the doubles the body stored into them?
//
// It is the same `isFloat` the arithmetic, negation and print lowerings consult, run over a copy of
// the float-variable table with the rebound names added — so the answer is the one the emitted body
// will act on, not a second opinion guessed from the return line's syntax. That agreement is what
// keeps a promoted function from ending in `ret i32` under a `define double`: `return -x` of such a
// parameter used to reach no gate at all, because the return word was read off the shape of the
// return expression and a unary minus says nothing, and the module came out rejected (roadmap
// L11.6, Gap R.3c, ADR 0166).
func (g *irGen) floatReboundReturnAsksForTheDouble(fd *FuncDef, atStake []string) bool {
	return g.withFloatNames(atStake, func() bool { return funcReturnsFloat(g, fd) })
}

// withFloatNames runs one question with the named added to the float-variable table the predicate reads, on a
// COPY of that table. It is the whole reason a scan and an emission can be said to ask the same question: the
// answer `funcReturnsFloat` gives a body, the answer `isFloat` gives one of its expressions, and the answer the
// assignment road will act on while emitting are then facts about one table rather than three readings of it
// (roadmap ADR 0273's rule that a scan which decides must not keep a second opinion, L11.6, Gap R.3c, Gap R.338).
func (g *irGen) withFloatNames(atStake []string, ask func() bool) bool {
	saved := g.floatVars
	merged := make(map[string]bool, len(saved)+len(atStake))
	for k, v := range saved {
		merged[k] = v
	}
	for _, nm := range atStake {
		merged[nm] = true
	}
	g.floatVars = merged
	needs := ask()
	g.floatVars = saved
	return needs
}

// doubleReturnCarriable reports whether emitting fd with a `double` return word can carry the answer at
// all. The convention that decides the return word decides every parameter's word with it — a
// float-returning function takes `double` parameters — so a body with a container parameter (whose word
// is a heap handle) or an interned-text parameter (whose word is an index into `@str_tab`) cannot be
// promoted without handing the callee a `sitofp` of a global, which is the module `llc` rejects and
// ADR 0166 calls our bug. Those shapes stay refusals, and the answer is the tagged value word's.
func (g *irGen) doubleReturnCarriable(fd *FuncDef) bool {
	fn := g.fnName(fd)
	for i, p := range fd.Params {
		if g.paramHeapKind(fn, i, p) != HeapNone {
			return false
		}
		if g.strParamOf[fn][i] {
			return false
		}
	}
	return true
}

// funcReturnsFloat reports whether fd has a return expression that produces a
// double. It scans the body for ReturnStmt expressions and asks isFloat, also
// tracking local variables that are assigned float values so that a function
// returning such a variable (e.g. `y = x * 1.5; return y`) is detected.
func funcReturnsFloat(g *irGen, fd *FuncDef) bool {
	var scan func([]Stmt) bool
	scan = func(sts []Stmt) bool {
		for _, st := range sts {
			switch n := st.(type) {
			case *ReturnStmt:
				if g.isFloat(n.Expr) {
					return true
				}
			case *IfStmt:
				if scan(n.Then) {
					return true
				}
				for _, e := range n.Elifs {
					if scan(e.Then) {
						return true
					}
				}
				if scan(n.Else) {
					return true
				}
			case *WhileStmt:
				if scan(n.Body) {
					return true
				}
			case *ForStmt:
				if scan(n.Body) {
					return true
				}
			case *MatchStmt:
				for _, c := range n.Cases {
					if scan(c.Body) {
						return true
					}
				}
			}
		}
		return false
	}
	return scan(fd.Body)
}

// fnName returns the emitted IR name for fd, honoring a module-function
// name override (g.curFnOverride) so AOT module functions get a mangled,
// collision-free label like `mod$fn`.
// irSymbolPrefix is the prefix on the link name of every function the program's own
// source defines; irSymbol applies it.
//
// An emitted function name is not a label, it is a symbol the linker resolves. A program
// that wrote
//
//	def sync():
//	    return 7
//	print(sync())
//
// was emitted as `define i32 @sync()`, and the call bound to libc's `sync()` instead: the
// interpreter printed `7`, the compiled binary printed libc's `0`, and nothing along the
// way complained (roadmap Gap R.4). `main`, `exit`, `printf`, `free`, `strlen`, `write`,
// `time` are the same accident — all names a program may choose deliberately — and the
// generated entry point is `@main` too, so a user `def main()` was a duplicate definition.
//
// Prefixing every program-defined function closes the whole class without a blocklist to
// keep current, and without refusing a name the program is free to choose. Names the
// program does not define keep theirs: the runtime helpers (`rt_*`), the C library
// (`printf`, `write`, `snprintf`), and the `extern fn` declarations an FFI surface must
// export under the C name (see emitExterns / the `declare` path).
const irSymbolPrefix = "gy_"

// irSymbol is the link name a program-defined function is emitted and called under. It is
// idempotent because a symbol is minted once and then travels through registries (a
// method's symbol is stored in its class index), and both ends must agree.
func irSymbol(name string) string {
	if name == "" || strings.HasPrefix(name, irSymbolPrefix) {
		return name
	}
	return irSymbolPrefix + name
}
func (g *irGen) fnName(fd *FuncDef) string {
	if g.curFnOverride != "" {
		return g.curFnOverride
	}
	return fd.Name
}

// emitModuleFuncs lowers AOT module functions (from `import mod` where mod
// defines functions) as standalone IR defines with a mangled name `mod$fn`.
// Module-global constants are captured by bare-name resolution during funcDef.
func (g *irGen) emitModuleFuncs(b *strings.Builder) error {
	if g.imports == nil {
		return nil
	}
	for mod, fns := range g.imports.Funcs {
		globals := g.imports.Globals[mod]
		for fnName, fd := range fns {
			mangle := mod + "$" + fnName
			params := map[string]bool{}
			for _, p := range fd.Params {
				params[p.Name] = true
			}
			prevOverride, prevGlobals, prevParams, prevMod := g.curFnOverride, g.curModGlobals, g.curModParams, g.curModName
			g.curFnOverride = mangle
			g.curModName = mod
			g.curModGlobals = globals
			g.curModParams = params
			err := g.funcDef(b, fd)
			g.curFnOverride = prevOverride
			g.curModName = prevMod
			g.curModGlobals = prevGlobals
			g.curModParams = prevParams
			if err != nil {
				return fmt.Errorf("import %q: lowering module function %q: %v", mod, fnName, err)
			}
		}
	}
	return nil
}
func (g *irGen) funcDef(b *strings.Builder, fd *FuncDef) error {
	// A definition the defunctionalisation pass retired: every call that used this function as a value now
	// names the clone the pass minted for it, so its own body compiles nothing, and emitting it would put a
	// function in the module that no call reaches — one whose name could then be taken by the very clone that
	// replaced it (ADR 0345).
	if fd.SkipEmit {
		return nil
	}
	prevRaise := g.funcRaiseExit
	prevHandlers := g.handlerStack
	g.handlerStack = nil
	// A function body is not inside its caller's `except` arm, however it was reached — a `def`
	// written inside an arm must not have its `return` clear the enclosing arm's exception.
	prevHandledArms := g.handledArms
	g.handledArms = 0
	defer func() { g.handledArms = prevHandledArms }()
	// Nor is it inside the caller's `try`: a `return` in a nested function body must not run the
	// enclosing statement's deferred bodies (Gap R.23, ADR 0222).
	prevDeferred := g.deferred
	g.deferred = nil
	defer func() { g.deferred = prevDeferred }()
	// The traceback frame names the function the raise is written in — and for a clone the pass minted, that
	// is the function the human wrote, not the pass's internal spelling (ADR 0320's rule for a rewritten
	// call, applied to a rewritten definition; ADR 0345).
	prevFnSrc := g.curFnSrc
	g.curFnSrc = callableDisplayName(fd)
	defer func() { g.curFnSrc = prevFnSrc }()
	g.funcRaiseExit = g.fnName(fd) + ".raiseexit"
	g.closures = map[string]*closureInfo{}
	// A function body is its own variable-binding scope: slot/allocation state and
	// the container-kind maps are per scope. Without this, a dict named `d` in one
	// function made module-level `d = make(4)` emit its "release the old binding"
	// free against `%_d` — a register main had not allocated yet — and the module
	// failed to verify ("input module is broken").
	restoreScope := g.beginScope()
	defer restoreScope()
	g.envMode = false
	g.envCaptures = nil
	g.envParam = "%env"
	g.decorated = map[string]bool{}
	g.inFunc = true
	// What this body binds anywhere inside itself (so a name the module also binds stays local --
	// ADR 0220's rule applied where it is decidable), and whether this function is used as a
	// decorator (ADR 0227).
	doneBody := g.enterBody(fd.Body)
	defer doneBody()
	doneFlags := g.enterBoundFlags(fd, fd.Body)
	defer doneFlags()
	wasDecorator := g.emittingDecorator
	g.emittingDecorator = g.decoratorNames != nil && g.decoratorNames[fd.Name]
	defer func() { g.emittingDecorator = wasDecorator }()
	op := map[string]bool{}
	for _, p := range fd.Params {
		op[p.Name] = true
	}
	ol := map[string]bool{}
	collectLocals(fd.Body, ol)
	for _, nd := range nestedDefs(fd.Body) {
		ci := closureInfoFor(nd, op, ol)
		g.closures[ci.name] = ci
		fmt.Fprintf(&g.globals, "@%s_slot = internal global i32 0\n", ci.name)
		g.envSlots = append(g.envSlots, ci.name)
		g.emitClosureDef(b, ci, nd)
	}
	if len(fd.Decorators) > 0 {
		if err := g.emitDecoratedFunc(b, fd); err != nil {
			return err
		}
		return nil
	}
	g.params = map[string]string{}
	g.paramSlot = nil
	g.curFunc = g.fnName(fd)
	g.curFD = fd
	// The word a function returns is a fact about its body, not about its last line. `floatRet` below
	// asks `isFloat` of each `return` *expression*, which is enough for `return x + 0.0` and nothing at
	// all for `return x` — and a body that has just stored a double into its parameter's slot then has no
	// double word to return it in: the read of the name keeps answering the incoming register, the call
	// hands back the argument, and `print(addf(1.0))` prints `1` with the exit code of success
	// (roadmap L11.6, Gap R.3c, ADR 0254; `programs/probe_float_param_rebind.gy` is the corpus form).
	floatRet := funcReturnsFloat(g, fd)
	// The rebind that convention cannot answer, asked before a single instruction is written — and asked
	// of the same predicate the instructions below will ask, so the word the `ret` writes and the word the
	// value arrives in cannot disagree. Where every parameter is a number the body is emitted
	// double-returning and the program simply answers; where a parameter carries a container handle or an
	// interned text the same convention would have to give that parameter a `double` too — the return
	// convention decides every parameter together — and the program is refused in words rather than
	// emitted wrong, because the emitted form is `sitofp i32 @.lst1 to double` and `llc` rejects it
	// (roadmap ADR 0166; see `returnedFloatRebindings` and `doubleReturnCarriable`).
	if !floatRet {
		if bad := returnedFloatRebindings(fd, g.isFloat, g.withFloatNames); len(bad) > 0 {
			// The gate asks the same question the instructions below will ask, so the word the
			// `ret` writes and the word the value arrives in cannot disagree — which is what the
			// module verifier checks and what `return -x` used to get wrong in silence.
			if !g.floatReboundReturnAsksForTheDouble(fd, bad) {
				// The arms of the returned ternary do not agree on a word, which is the one
				// shape of this family the compiler cannot take: whichever word it chose, the
				// other arm would print a number-shaped wrong answer. The wording comes from
				// the same helper the ternary lowering refuses with, so the two cannot drift
				// into describing different programs (roadmap Gap R.102, ADR 0262).
				if cond := returnedTernary(fd); cond != nil {
					return fmt.Errorf("%q returns `%s`: %v", g.fnName(fd), exprSurface(cond), ternaryWordErr(cond, ternaryArmDisagrees))
				}
				return fmt.Errorf("%q returns a value read off %q, which its own body binds to a float, and the answer's word is not one the compiled backend can write: the double the body computed has no word to travel in and the call answers a truncated i32 — a number-shaped answer that is the wrong one, which ADR 0166 counts as this compiler's bug rather than the program's (take the branch with `if`/`else` and `return` the number itself, or add it to `0.0` on the arm; roadmap L11.6, Gap R.102)",
					g.fnName(fd), bad[0])
			}
			if !g.doubleReturnCarriable(fd) {
				return fmt.Errorf("%q returns %q, which its own body binds to a float: the compiled backend chooses a function's return word from the shape of its return expression, and `return %s` says nothing, so the double the body computed has no word to travel in and the call answers the argument as it arrived — a number-shaped answer that is the wrong one, which ADR 0166 counts as this compiler's bug rather than the program's (write `return %s + 0.0`, or bind it to a new name, to say the answer is a float; roadmap L11.6, Gap R.3c, ADR 0196)",
					g.fnName(fd), bad[0], bad[0], bad[0])
			}
			floatRet = true
		}
	}
	retTy := "i32"
	retVal := "0"
	paramTy := "i32"
	if floatRet {
		g.floatFuncs[g.fnName(fd)] = true
		retTy = "double"
		retVal = "0.0"
		paramTy = "double"
	}
	// Which of this body's parameters arrive as a (payload, tag) pair, and whether its answer carries a
	// tag too. The arity is the scan's decision, so a call site emitted before this `define` agrees with
	// it; only the binding half is narrowed by the convention this body is emitted under (ADR 0273).
	pairSpec := g.pairSpecFor(fd, floatRet, g.strFuncs[g.fnName(fd)])
	if g.answer.carries(g.fnName(fd)) {
		// The answer direction and the double return cannot both own this body's `ret`: the pair the
		// first stores is an i32 payload beside a kind word, and the second crosses a `double`. The
		// scan excludes every arm that could make the body float-returning (a float literal, a name the
		// body rebinds, anything whose kind is not written in the spelling), so reaching this point is
		// the scan's mistake rather than the program's, and it is said as one (roadmap ADR 0166's rule,
		// ADR 0368).
		if floatRet {
			return fmt.Errorf("codegen: %q answers more than one kind AND a double this backend can only cross as a float return word: the answer direction has no word for the float arm (roadmap L11.6, Gap R.219, ADR 0368)", g.fnName(fd))
		}
		g.answerDeclare(g.fnName(fd))
	}
	if pairSpec != nil && pairSpec.returnsPair {
		// The tag word this body stores beside its answer. It is declared here, where the function's own
		// `define` is emitted, and read by callers that may have been emitted already — so the read cannot
		// be gated on the same one-shot flag that writes it (the flag now only records the fact, and the
		// declaration is idempotent per function).
		g.pairRetDone[g.fnName(fd)] = true
		if !g.pairTagDeclared[g.fnName(fd)] {
			g.pairTagDeclared[g.fnName(fd)] = true
			g.globals.WriteString(pairTagGlobalIR(g.fnName(fd)))
		}
	}
	// The program's own functions carry the irSymbolPrefix; see irSymbol.
	g.dbgDefine(b, irSymbol(g.fnName(fd)), callableDisplayName(fd), fd.Src)
	fmt.Fprintf(b, "define %s @%s(", retTy, irSymbol(g.fnName(fd)))
	for i := range fd.Params {
		if i > 0 {
			fmt.Fprintf(b, ", ")
		}
		fmt.Fprintf(b, "%s %%p%d", paramTy, i)
		if pairSpec != nil && pairSpec.params[i] {
			// The kind word beside the payload: the caller read it off the object, and the body asks it
			// the same arithmetic question the caller would have asked (roadmap L11.1, ADR 0273).
			fmt.Fprintf(b, ", i32 %s", pairParamReg(i))
		}
	}
	fmt.Fprintf(b, ") {\n")
	// The written-flags for this body's possibly-unwritten locals, immediately inside the brace:
	// they must be instructions, not module-level text (ADR 0228).
	g.emitBoundAllocas(b)
	// A call opens its own root frame (ADR 0181). Everything this body pushes as a
	// root is dropped when it returns, so a dead frame cannot retain a list — and a
	// recursive call gets its own entries instead of overwriting the outer frame's.
	g.gcOpenFrame(b)
	for i, p := range fd.Params {
		g.params[p.Name] = fmt.Sprintf("%%p%d", i)
		if floatRet {
			if g.floatVars == nil {
				g.floatVars = map[string]bool{}
			}
			g.floatVars[p.Name] = true
			g.forgetVarBool(p.Name)
			fmt.Fprintf(b, "  %%_%s = alloca double\n", p.Name)
			if g.doubleSlot == nil {
				g.doubleSlot = map[string]bool{}
			}
			g.doubleSlot[p.Name] = true
			fmt.Fprintf(b, "  store double %%p%d, double* %%_%s\n", i, p.Name)
			// The slot exists now, so the body's assignment to this name must reuse
			// it: without the registration the assignment emitted a second alloca of
			// the same name and llc called it "multiple definition of local value"
			// (Gap R.3c, ADR 0196).
			g.allocd[p.Name] = true
		}
	}
	// A parameter whose argument the pair road answered is bound through the tagged door, with the
	// arithmetic origin recorded so the body asks the same door the caller asked; the undo is because
	// the tag records are keyed by name and the next body's parameter of the same name must not read
	// this one's binding (roadmap L11.1, ADR 0273).
	defer g.bindPairParams(b, fd, pairSpec)()
	// A parameter that receives a list/dict/set handle must be treated as a
	// runtime container inside the body (see heapargs.go). The registration is
	// scoped to this body: undo it when the body is done.
	defer g.declareHeapParams(b, g.fnName(fd), fd, func(i int) string { return fmt.Sprintf("%%p%d", i) })()
	// A parameter whose arguments are strings arrives as an index into the runtime string
	// table, so inside the body it behaves exactly like an element read from a string
	// container: print shows the text, len measures it, == compares content (Gap J.5).
	for i, p := range fd.Params {
		if g.strParamOf[g.fnName(fd)][i] {
			g.internedVars[p.Name] = true
		}
		// A parameter every call site hands a verdict to behaves inside the body exactly like a
		// name the body itself assigned a verdict to. The print, str, repr, container and --json
		// roads all ask IsBoolExpr of the expression in front of them, and inside a body the
		// argument *is* the name — which is why `def show(f): print(f)` / `show(True)` answered 1:
		// the table had nothing to say about a name the body never assigned. The call site read
		// the verdict; this is where the body finds out (roadmap Gap R.111, L11.1's bool item).
		if g.boolParamOf[g.fnName(fd)][i] {
			if g.boolVars == nil {
				g.boolVars = map[string]bool{}
			}
			g.boolVars[p.Name] = true
		} else {
			// Authoritative, not additive: the parameter's own call sites are the only evidence
			// about this name inside this body, and a name inherited from an enclosing body (a
			// nested def, a method emitted while a `def` was in flight) must not print a verdict
			// the argument never was.
			delete(g.boolVars, p.Name)
		}
	}
	// The call sites decide a parameter's kind, so this is the first moment the body's own
	// returns can be judged: `def f(s): return s[1]` called with a string hands back a string
	// index, and the caller has to read it as one (ADR 0229). Without this the value was
	// correct and the print was not — the index leaked out as a number.
	if !fd.Async && !g.answerOwns(g.fnName(fd)) && returnsStringExpr(g, fd) {
		g.strFuncs[g.fnName(fd)] = true
	}
	isGen := containsYield(fd.Body)
	if isGen {
		g.genIdx++
		g.genHandle = fmt.Sprintf("%%gh%d", g.genIdx)
		g.genFuncs[g.fnName(fd)] = true
		g.heapUsed = true
		g.emitRtAllocKind(b, g.genHandle, HeapKindList, Span{})
		// Keep the generator's accumulator list alive across the GC emitted
		// before the first body statement; otherwise it is collected and every
		// yield appends into a stale/freed slot.
		ghslot := g.newTmp()
		ghslotName := strings.TrimPrefix(ghslot, "%")
		fmt.Fprintf(b, "  %%_%s = alloca i32\n", ghslotName)
		fmt.Fprintf(b, "  store i32 %s, i32* %%_%s\n", g.genHandle, ghslotName)
		g.gcReg(b, ghslotName)
	}
	for i := range fd.Params {
		if floatRet {
			continue
		}
		fmt.Fprintf(b, "  %%_param%d = alloca i32\n", i)
		fmt.Fprintf(b, "  store i32 %%p%d, i32* %%_param%d\n", i, i)
		g.gcReg(b, fmt.Sprintf("param%d", i))
	}
	// A parameter the body rebinds is a local that starts out bound to an argument:
	// copy it into a named slot now and read from there, or every read keeps answering
	// the argument however many times the body assigned (Gap R.3, ADR 0196).
	g.copyInReboundParams(b, fd, floatRet)
	if g.isWrappingDecorator(fd) {
		g.gcCloseFrame(b)
		b.WriteString("  ret i32 0\n}\n")
		g.frameOpen = false
		return nil
	}
	for _, st := range fd.Body {
		g.gcCall(b)
		if err := g.stmt(b, st); err != nil {
			return err
		}
	}
	if isGen {
		g.gcCloseFrame(b)
		fmt.Fprintf(b, "  ret i32 %s\n", g.genHandle)
	} else {
		// A body that reaches its end without a `return` answers None, and a pair-returning one says so
		// in the tag word too — otherwise the next caller reads whatever the previous answer left there
		// (ADR 0273; the same "no invented default" rule ADR 0269 took out of `logicOperandKind`).
		g.storePairTag(b, pairTagNone)
		g.gcCloseFrame(b)
		fmt.Fprintf(b, "  ret %s %s\n", retTy, retVal)
	}
	fmt.Fprintf(b, "%s:\n", g.funcRaiseExit)
	// An unwinding raise pops the frame too, so a raise that crosses a frame boundary
	// does not leave that frame's handles rooted forever.
	if isGen {
		g.gcCloseFrame(b)
		fmt.Fprintf(b, "  ret i32 %s\n", g.genHandle)
	} else {
		g.storePairTag(b, pairTagNone) // the unwinding answer: no value, and the tag says so
		g.gcCloseFrame(b)
		fmt.Fprintf(b, "  ret %s %s\n", retTy, retVal)
	}
	fmt.Fprintf(b, "}\n")
	g.frameOpen = false
	g.funcRaiseExit = prevRaise
	g.genHandle = ""
	g.handlerStack = prevHandlers
	g.params = map[string]string{}
	g.paramSlot = nil
	g.curFunc = ""
	g.curFD = nil
	g.inFunc = false
	return nil
}

// stmtYieldFrom is the road a `yield from` takes: the sub-generator's values, one at a
// time, into the same buffer.
func (g *irGen) stmtYieldFrom(b *strings.Builder, st Stmt, n *YieldFromStmt) error {
	if g.genHandle == "" {
		return fmt.Errorf("codegen: yield from outside a generator")
	}
	// A statically-known list literal has no heap backing: append each
	// element directly instead of treating the global as a heap handle.
	if ll, ok := n.Expr.(*ListLit); ok {
		for _, el := range ll.Elems {
			ev, err := g.value(b, el)
			if err != nil {
				return err
			}
			fmt.Fprintf(b, "  call void @rt_append(i32 %s, i32 %s)\n", g.genHandle, ev)
		}
		return nil
	}
	hv, err := g.value(b, n.Expr)
	if err != nil {
		return err
	}
	// Keep the sub-list handle alive across the GC emitted before the
	// append loop; otherwise the generator's freshly-returned list can be
	// collected and the loop reads a stale handle. Store the handle into a
	// rooted alloca slot so GC keeps the object reachable.
	hvslot := g.newTmp()
	hvslotName := strings.TrimPrefix(hvslot, "%")
	fmt.Fprintf(b, "  %%_%s = alloca i32\n", hvslotName)
	fmt.Fprintf(b, "  store i32 %s, i32* %%_%s\n", hv, hvslotName)
	g.gcReg(b, hvslotName)
	ln := g.newTmp()
	fmt.Fprintf(b, "%s = call i32 @rt_list_len(i32 %s)\n", ln, hv)
	ild := g.newLabel("yf.cond")
	ilp := g.newLabel("yf.body")
	inc := g.newLabel("yf.inc")
	end := g.newLabel("yf.end")
	iv := g.newTmp()
	fmt.Fprintf(b, "%s = alloca i32\n", iv)
	fmt.Fprintf(b, "  store i32 0, i32* %s\n", iv)
	fmt.Fprintf(b, "  br label %%%s\n", ild)
	fmt.Fprintf(b, "%s:\n", ild)
	ivc := g.newTmp()
	fmt.Fprintf(b, "%s = load i32, i32* %s\n", ivc, iv)
	cmp := g.newTmp()
	fmt.Fprintf(b, "%s = icmp slt i32 %s, %s\n", cmp, ivc, ln)
	fmt.Fprintf(b, "  br i1 %s, label %%%s, label %%%s\n", cmp, ilp, end)
	fmt.Fprintf(b, "%s:\n", ilp)
	// The generator's answer is already parked in a rooted slot above (it keeps the list alive across
	// the append loop), so the body can collect: each element is appended straight into that container,
	// which roots it transitively from the sweep's own walk of the payload. The loop itself is
	// the compiler's, not the program's: it fetches a word and appends it, minting no object, so it
	// asks for no collection (loopSafePoint's `nil` body).
	g.loopSafePoint(b, nil)
	el := g.newTmp()
	fmt.Fprintf(b, "%s = call i32 @rt_get_elem(i32 %s, i32 %s)\n", el, hv, ivc)
	fmt.Fprintf(b, "  call void @rt_append(i32 %s, i32 %s)\n", g.genHandle, el)
	fmt.Fprintf(b, "  br label %%%s\n", inc)
	fmt.Fprintf(b, "%s:\n", inc)
	nv := g.newTmp()
	fmt.Fprintf(b, "%s = add i32 %s, 1\n", nv, ivc)
	fmt.Fprintf(b, "  store i32 %s, i32* %s\n", nv, iv)
	fmt.Fprintf(b, "  br label %%%s\n", ild)
	fmt.Fprintf(b, "%s:\n", end)
	return nil
}

// stmtYield is the road a `yield` takes in a generator the backend unrolls.
func (g *irGen) stmtYield(b *strings.Builder, st Stmt, n *YieldStmt) error {
	// `yield expr` inside a generator function appends to the function's
	// runtime heap list (the interpreter evaluates generators eagerly).
	if g.genHandle == "" {
		return fmt.Errorf("codegen: yield outside a generator function")
	}
	v, err := g.value(b, n.Expr)
	if err != nil {
		return err
	}
	fmt.Fprintf(b, "  call void @rt_append(i32 %s, i32 %s)\n", g.genHandle, v)
	return nil
}

// stmtReturn is the road a `return` takes, including the double it has to carry when the
// function answers one.
func (g *irGen) stmtReturn(b *strings.Builder, st Stmt, n *ReturnStmt) error {
	if g.handledArms > 0 {
		// Returning out of an `except` arm ends the handler's work: the exception this
		// arm accepted must not survive the return, or the caller's next call-site check
		// hands it back as though nothing had handled it (Gap R.21, compiled half).
		g.clearExn(b)
	}
	if n.Expr == nil {
		// bare `return` yields None (ADR 0172). A float function reaching this is
		// a type error the checker reports; 0.0 keeps the module valid.
		if g.floatFuncs[g.curFunc] {
			if err := g.runDeferred(b); err != nil {
				return err
			}
			g.gcCloseFrame(b)
			b.WriteString("  ret double 0.000000\n")
			return nil
		}
		g.heapUsed = true
		t := g.newTmp()
		g.emitHeapHelperCall(b, t, "rt_none", "", Span{})
		// A bare `return` answers the void, and a body whose answer carries its kind says so: without the
		// store the caller reads whatever the previous answer of the same function left in the word
		// (roadmap L11.1, ADR 0273's rule about an invented default, ADR 0368).
		g.storePairTag(b, pairTagNone)
		if err := g.runDeferred(b); err != nil {
			return err
		}
		g.gcCloseFrame(b)
		b.WriteString(fmt.Sprintf("  ret i32 %s\n", t))
		return nil
	}
	// One word crosses a `return`, and a dict literal's entry does not always travel in one word: the
	// answer to `{"a": "x"}["a"]` is a text whose payload is an `@str_tab` index, and a caller with no tag
	// to read prints the table position — the reference's `x` turned into a number at exit 0. The pair road
	// at the call boundary (ADR 0273) is the row that owns carrying it (Gap R.146); until it does, this
	// door refuses by name rather than answers wrongly, and every position that does carry the pair —
	// print, str()/repr(), an f-string field, a binding — is answered (roadmap Gap R.215, ADR 0327).
	if dl, key, isLit := literalDictReadOf(n.Expr); isLit && !g.dictLiteralFoldAnswers(dl, key) && !g.dictLiteralEntryIsPlainInt(dl, key) {
		return literalDictOneWordErr(key)
	}
	if g.answer.carries(g.curFunc) {
		// This body answers more than one kind, so the arm that ran says which: the payload comes from the
		// road that has always produced it and the kind is the constant this spelling carries, stored in
		// the same word the pair road uses (roadmap Gap R.219, Gap R.228, ADR 0368).
		if arm, ok := g.answer.armOf(n); ok {
			return g.answerReturn(b, n, arm)
		}
		// A `return` the scan never saw: the body is carried, so every road owes a word, and inventing one
		// here is how a value walks around wearing another kind's bits (ADR 0187's rule).
		return fmt.Errorf("codegen: %q answers %s, a road the answer direction never saw, so the kind its payload means has no source (roadmap L11.1, Gap R.219, ADR 0368)", g.curFunc, exprSurface(n.Expr))
	}
	if g.floatFuncs[g.curFunc] {
		// A pair-returning callee under a double return: the callee answered (payload, tag) beside its
		// own return, and the one place a tagged value becomes a double is @rt_lift_num — the same door a
		// float slot's arithmetic lifts through. `def g(y): return y * 2` under
		// `def f(x): x = x + 1.5; return g(x)` is this shape: the callee's parameter has to arrive as a
		// pair for the multiply to keep the half, and the answer the caller reads is that pair lifted to
		// the double this function's convention already promised (roadmap L11.1, Gap R.139's return half).
		if p, t, okPair, pairErr := g.pairCallPair(b, n.Expr); pairErr != nil {
			return pairErr
		} else if okPair {
			lift := g.newTmp()
			b.WriteString(fmt.Sprintf("  %s = call double @rt_lift_num(i32 %s, i32 %s)\n", lift, p, t))
			if err := g.runDeferred(b); err != nil {
				return err
			}
			g.gcCloseFrame(b)
			b.WriteString(fmt.Sprintf("  ret double %s\n", lift))
			return nil
		}
		fv := g.floatValue(b, n.Expr)
		if err := g.runDeferred(b); err != nil {
			return err
		}
		g.gcCloseFrame(b)
		b.WriteString(fmt.Sprintf("  ret double %s\n", fv))
		return nil
	}
	if g.strFuncs[g.curFunc] {
		// A function known to yield a string returns its @str_tab index: callers
		// read the result as an index (that is what makes print(f("x")) show text,
		// ADR 0174), so returning the raw @.strN global here put a global in an
		// i32 slot and llc rejected the module — a plain `def g(): return "hi"`
		// looked like a compiler bug (ADR 0166).
		if txt, ok := g.stringVal(n.Expr); ok {
			it := g.newTmp()
			b.WriteString(fmt.Sprintf("  %s = call i32 @rt_str_intern2(i8* %s, i8* %s)\n", it, g.strConst(txt), g.strConst(pyReprString(txt))))
			if err := g.runDeferred(b); err != nil {
				return err
			}
			g.gcCloseFrame(b)
			b.WriteString(fmt.Sprintf("  ret i32 %s\n", it))
			return nil
		}
	}
	// A container handed back by a `return` is an object, and an object has a handle rather than a
	// global. `return [1, 2]` used to lower the literal to its compile-time array and write
	// `ret i32 @.lst1`, which is not IR: `llc` rejected the module and `gustyc` exited **2** — the code
	// the exit-code contract reserves for a compiler bug — on `def build(): return [1, 2]` with
	// `print(build())`, a program the reference runs and prints `[1, 2]` (roadmap Gap R.67, ADR 0333).
	// The builders reached here are the ones a binding already uses, so the word that crosses the `ret`
	// is the same handle `ys = build()` has been handing back all along, and the roads that read a
	// container variable read the answer as soon as the print door asks it (below).
	if _, isContainerAnswer := g.containerAnswerOf(g.curFunc); isContainerAnswer {
		mustBuild := false
		switch n.Expr.(type) {
		case *ListLit, *SetLit, *DictLit, *Comp:
			mustBuild = true // the scan proved the answer; a builder that declines says so, it does not emit a global
		}
		if h, _, okHandle := g.containerHandleOf(b, n.Expr); okHandle {
			g.heapUsed = true
			if err := g.runDeferred(b); err != nil {
				return err
			}
			g.gcCloseFrame(b)
			b.WriteString(fmt.Sprintf("  ret i32 %s\n", h))
			return nil
		}
		if mustBuild {
			_, cErr := g.containerOperand(b, n.Expr)
			if cErr != nil {
				return cErr
			}
			return fmt.Errorf("codegen: %q answers a container this backend cannot build at the return (roadmap Gap R.67, ADR 0333)", g.curFunc)
		}
	}
	if g.pairRetDone[g.curFunc] {
		// The body's answer already is a (payload, tag) pair — the arithmetic door decided its kind from
		// the objects the caller read — so the tag travels beside the return and the payload keeps the
		// word this function always returned (roadmap L11.1, Gap R.139, ADR 0273).
		p, t, rerr := g.pairReturnWords(b, n.Expr)
		if rerr != nil {
			return rerr
		}
		g.storePairTag(b, t)
		// The value is computed before the deferred bodies run, as Python's `return` in a `try` does
		// (Gap R.23, ADR 0222) — and the tag is stored before them for the same reason: a `finally` that
		// calls this function again would otherwise leave its own answer's kind in the word.
		if err := g.runDeferred(b); err != nil {
			return err
		}
		g.gcCloseFrame(b)
		b.WriteString(fmt.Sprintf("  ret i32 %s\n", p))
		return nil
	}
	v, err := g.value(b, n.Expr)
	if err != nil {
		return err
	}
	// deferred bodies run -- which is why `return n` in a `try` hands back the `n` from
	// before the `finally` reassigned it, as it does in Python (Gap R.23, ADR 0222).
	if err := g.runDeferred(b); err != nil {
		return err
	}
	g.gcCloseFrame(b)
	b.WriteString(fmt.Sprintf("  ret i32 %s\n", v))
	return nil
}

// callUserFunc is the road a call to a function the program defined takes: the argument
// words, the pair the caller crosses with them, the float return the callee may carry, and
// the tail call or trampoline the shape of the callee decides.
func (g *irGen) callUserFunc(b *strings.Builder, c *Call, fnName string) (string, error) {
	fd := g.fds[fnName]
	if fd == nil {
		return "", fmt.Errorf("codegen: unknown function %q", fnName)
	}
	n := len(fd.Params)
	isFloat := g.floatFuncs[fnName]
	vals := make([]string, n)
	// tagWords is the second word of the arguments the pair crosses with, pre-filled from the same
	// scan the `define` read: a parameter the scan marked always gets its tag word, whether the pair
	// road answered the payload or the ordinary road did, because a `call` and its `define` that
	// disagree about the arity is the module verifier's problem (roadmap L11.1, ADR 0273).
	tagWords := make([]string, n)
	for i := range fd.Params {
		tagWords[i] = g.pairArityTag(fnName, i)
	}
	// pairArg is the pair road in front of the ordinary one; it is filled in below `argVal`, whose
	// refusal it keeps for an argument the pair cannot name (ADR 0273).
	var pairArg func(a Expr, idx int) (string, error)
	// A container literal argument is materialised into the runtime heap and
	// passed by handle; the callee declares the matching parameter as a
	// container (see declareHeapParams / heapargs.go).
	// strArgCheck reports the AOT limitation before emitting IR that LLVM
	// would reject: a string is an `i8*` global in this backend, so passing
	// one where the callee expects an `i32` produces
	// `call i32 @f(i32 @.str1)` — "global variable reference must have
	// pointer type". Naming the parameter keeps the message actionable.
	strArgCheck := func(a Expr, idx int) error {
		if _, isStr := g.stringVal(a); !isStr {
			if _, isFs := a.(*FString); !isFs {
				return nil
			}
		}
		what := "an argument"
		if idx >= 0 && idx < len(fd.Params) {
			what = fmt.Sprintf("parameter %q of %s", fd.Params[idx].Name, fnName)
		}
		return fmt.Errorf("codegen: strings are not supported as function arguments in the AOT backend yet (%s); CPython answers them", what)
	}
	argVal := func(a Expr, idx int) (string, error) {
		if h, ok, err := g.heapArg(b, a); ok || err != nil {
			if err != nil {
				return "", err
			}
			return h, nil
		}
		// A string argument to a string parameter becomes its @str_tab index: the
		// parameter is an i32 slot, and `call i32 @f(i32 @.strN)` is what LLVM rejects
		// (Gap J.5). A forwarded string (already an index) passes straight through.
		if idx >= 0 && g.strParamOf[fnName][idx] {
			if nm, ok := a.(*Name); ok && g.internedVars[nm.Value] {
				return g.value(b, a)
			}
			// An interpolation IS a text: the value road interns its parts and answers the index the
			// parameter's i32 slot holds, so `g(f"x{n}")` reaches the callee as any other text does
			// (roadmap Gap J.5's string parameter, L11.1's interpolation; ADR 0359).
			if _, isFs := a.(*FString); isFs {
				return g.value(b, a)
			}
			if txt, ok := g.stringVal(a); ok {
				g.heapUsed = true
				t := g.newTmp()
				b.WriteString(fmt.Sprintf("  %s = call i32 @rt_str_intern2(i8* %s, i8* %s)\n", t, g.strConst(txt), g.strConst(pyReprString(txt))))
				return t, nil
			}
		}
		if err := strArgCheck(a, idx); err != nil {
			return "", err
		}
		v, err := g.value(b, a)
		if err != nil {
			return "", err
		}
		// A compile-time list global (@.lstN, e.g. a folded comprehension)
		// is not an i32 handle: copy it into the runtime heap so the callee
		// receives a handle it can iterate, measure and index.
		if ln, ok := g.staticLists[v]; ok {
			return g.heapListFrom(b, ln, "")
		}
		return v, nil
	}
	// pairArg is tried before the ordinary road for a marked parameter: the argument's kind may live in
	// an object the caller can read and the callee cannot, and the two words are what carry it over.
	// It is written after `argVal` because the fallback is that road, refusal and all (ADR 0273).
	pairArg = func(a Expr, idx int) (string, error) {
		if p, t, handled, err := g.pairArgWords(b, fnName, idx, a); err != nil {
			return "", err
		} else if handled {
			tagWords[idx] = t
			return p, nil
		}
		// A position the scan considered for the pair and then closed is not a free pass. It was closed
		// because some *other* call site hands that parameter something this pass cannot name —
		// `y = twice(x)` / `z = twice(y)` is exactly that — and a caller holding a double now has no road
		// left that reads its kind: the ordinary road keeps one word per argument and truncates the double
		// to its int half, which is how `print(outer(2.5))` answered `8` for CPython's `10.0` at exit 0
		// (roadmap Gap R.164, ADR 0280). Refusing here is the same judgement pairArgWords already makes for a
		// float handed to a *marked* position: a kind with nowhere to travel.
		if g.pairClosed[fnName] != nil && g.pairClosed[fnName][idx] && g.isFloat(a) {
			return "", fmt.Errorf("codegen: %s is a float handed to parameter %d of %s, a position this pass considered for the (payload, tag) pair and closed because another call site hands it something it cannot name: the ordinary road keeps one word per argument and would truncate the double to its int half, which is a wrong number rather than a refusal (roadmap L11.6, Gap R.164, ADR 0280)", exprSurface(a), idx, fnName)
		}
		// The same judgement, one construct nearer the source: an entry of a dict literal whose kind the
		// object chose is a pair, and the ordinary argument road has one word per argument. The answer's
		// tag would be dropped at the door, which is how `g({"a": "x"}["a"])` prints the `@str_tab`
		// position instead of the word — the Gap R.164 shape with a text where ADR 0280 had a float.
		if dl, key, isLit := literalDictReadOf(a); isLit && !g.dictLiteralFoldAnswers(dl, key) && !g.dictLiteralEntryIsPlainInt(dl, key) {
			return "", literalDictOneWordErr(key)
		}
		return argVal(a, idx)
	}
	// callArgWords is the one place the arity is spelled: every parameter's word, plus the tag word
	// where the scan says the pair crosses.
	callArgWords := func() []string {
		out := make([]string, 0, len(vals)+len(tagWords))
		for i := range vals {
			out = append(out, vals[i])
			if tagWords[i] != "" {
				out = append(out, "i32 "+tagWords[i])
			}
		}
		return out
	}
	// A helper that fills a container it was handed tells the caller what its elements are:
	// `def fill(out, v): out.append(v)` called as `fill(names, "one")` is what makes
	// `print(names[1])` render text instead of the raw string-table index (Gap J.5).
	if fills := g.strFillOf[fnName]; len(fills) > 0 {
		fillPos := 0
		for _, a := range c.Args {
			if kw, ok := a.(*KeywordArg); ok {
				for i, p := range fd.Params {
					if p.Name == kw.Name {
						g.applyStrFill(fnName, i, kw.Value)
					}
				}
				continue
			}
			g.applyStrFill(fnName, fillPos, a)
			fillPos++
		}
	}
	// A call whose answer is a pair is emitted by the pair door alone (pairCallPair): the ordinary
	// numeric road would take the payload as though it were the whole value and print an int box
	// handle as a number, which is the wrong answer this line of work exists to eliminate. The
	// permission is this call's, and it is off while the arguments are lowered, so a nested call of
	// the same shape inside an argument keeps its own honest refusal (roadmap L11.1, ADR 0273).
	//
	// A carried answer is exempt, and the exemption is the difference between a gate and a trap. The
	// answer direction declares the same global the pair road does, so seeding this flag from it would
	// refuse every `while f():`, every `x = f() + 1` and every `f(f(1))` of a mixed-return body at exit
	// 1 — programs the reference runs, on the strength of a position that never asked about kinds at all.
	// ADR 0273 wrote this rule for the pair road; the answer road reads it the same way. The positions
	// that DO read a carried answer ask the pair door themselves, and the rest keep the answer they had
	// before this cycle, wrong on one arm though that is — which is Gap R.229's open remainder rather
	// than a compiler that stops compiling (roadmap Gap R.219, ADR 0368).
	if g.pairRetDone[fnName] && !g.pairCallAsked[fnName] && !g.answer.carries(fnName) {
		return "", fmt.Errorf("codegen: %s hands back the (payload, tag) pair its answer arrived in: print(...), a binding, and the positions that read one number ask the tag, and this position keeps one word for the value, so the tag has nowhere to go (roadmap L11.1, Gap R.146, ADR 0273)", fnName)
	}
	// A call whose answer is a container is lowered by the container door alone: the ordinary road
	// would take the heap handle for the number it is not, which is how `print(f())` printed `0` and
	// `[f(), 9]` printed `[1, 9]` (roadmap Gap R.67, ADR 0333). The permission is this call's alone and
	// is off while the arguments are lowered, so a nested call of the same shape keeps its own refusal
	// — ADR 0273's rule, read again by the door beside the pair road's.
	if _, isContainerAnswer := g.containerAnswerOf(fnName); isContainerAnswer && !g.containerAnswerIsAsked(fnName) && g.containerAnswerCallIsCarriable(c) {
		return "", containerAnswerRefusal(fnName)
	}
	// The same refusal for a container the scan proved and the slot table declined. Without this the
	// fall-through returns the word the allocator handed back, and `print(f())` printed `0` for a
	// function that returns `[5]` — the module built, the program ran, exit 0, and the number on the
	// screen was a heap slot (roadmap Gap R.261, ADR 0333's scan read a second time).
	if g.containerAnswerIsUndescribed(fnName) && !g.containerAnswerIsAsked(fnName) {
		return "", containerAnswerUndescribedRefusal(fnName)
	}
	asked := g.pairCallAsked
	g.pairCallAsked = map[string]bool{}
	provided := make([]bool, n)
	pos := 0
	seenKw := false
	for _, a := range c.Args {
		if kw, ok := a.(*KeywordArg); ok {
			seenKw = true
			idx := -1
			for i, p := range fd.Params {
				if p.Name == kw.Name {
					idx = i
					break
				}
			}
			if idx < 0 {
				return "", fmt.Errorf("codegen: unknown keyword argument %q for %s", kw.Name, fnName)
			}
			if provided[idx] {
				return "", fmt.Errorf("codegen: multiple values for argument %q of %s", kw.Name, fnName)
			}
			if isFloat {
				fv := g.floatValue(b, kw.Value)
				vals[idx] = "double " + fv
			} else {
				av, err := pairArg(kw.Value, idx)
				if err != nil {
					return "", err
				}
				vals[idx] = "i32 " + av
			}
			provided[idx] = true
			continue
		}
		if seenKw {
			return "", fmt.Errorf("codegen: positional argument after keyword argument for %s", fnName)
		}
		if pos >= n {
			return "", fmt.Errorf("codegen: too many arguments for %s", callableSurfaceName(fnName))
		}
		if provided[pos] {
			return "", fmt.Errorf("codegen: multiple values for argument %q of %s", fd.Params[pos].Name, fnName)
		}
		if isFloat {
			fv := g.floatValue(b, a)
			vals[pos] = "double " + fv
		} else {
			av, err := pairArg(a, pos)
			if err != nil {
				return "", err
			}
			vals[pos] = "i32 " + av
		}
		provided[pos] = true
		pos++
	}
	// fill defaults for params not supplied
	for i := range fd.Params {
		if provided[i] {
			continue
		}
		if fd.Params[i].Default == nil {
			return "", fmt.Errorf("codegen: missing argument %q for %s", fd.Params[i].Name, fnName)
		}
		if isFloat {
			fv := g.floatValue(b, fd.Params[i].Default)
			vals[i] = "double " + fv
		} else {
			dv, err := pairArg(fd.Params[i].Default, i)
			if err != nil {
				return "", err
			}
			vals[i] = "i32 " + dv
		}
	}
	g.pairCallAsked = asked
	t := g.newTmp()
	if _, ok := g.closures[fnName]; ok {
		env := g.newTmp()
		b.WriteString(fmt.Sprintf("  %s = load i32, i32* @%s_slot\n", env, fnName))
		callArgs := append([]string{"i32 " + env}, callArgWords()...)
		b.WriteString(fmt.Sprintf("  %s = call i32 @%s_env(%s)\n", t, fnName, strings.Join(callArgs, ", ")))
		g.checkExn(b)
	} else if g.decorated[fnName] {
		// The decorated body is emitted through funcDef, which defines program-owned
		// names under irSymbolPrefix, so the reference must carry it too (Gap R.4).
		b.WriteString(fmt.Sprintf("  %s = call i32 @%s_impl(%s)\n", t, irSymbol(fnName), strings.Join(callArgWords(), ", ")))
		g.checkExn(b)
	} else {
		if isFloat {
			b.WriteString(fmt.Sprintf("  %s = call double @%s(%s)\n", t, irSymbol(fnName), strings.Join(callArgWords(), ", ")))
			if g.floatTemps == nil {
				g.floatTemps = map[string]bool{}
			}
			g.floatTemps[t] = true
		} else {
			// FFI: call to an extern (C) function — marshal values to native types.
			if ed, ok := g.externs[fnName]; ok {
				if len(ed.Params) != len(c.Args) {
					panic(fmt.Sprintf("extern function %q: expected %d args, got %d", fnName, len(ed.Params), len(c.Args)))
				}
				argTypes := []string{}
				argRegs := []string{}
				for i, p := range ed.Params {
					if p.Annot != nil && p.Annot.Kind == KindString {
						lit, ok2 := c.Args[i].(*StrLit)
						if !ok2 {
							panic("extern string args must be string literals: " + fnName)
						}
						gn := g.strConst(lit.Value) // ensure global @.strN exists
						n := len(lit.Value)
						argTypes = append(argTypes, "i8*")
						argRegs = append(argRegs, fmt.Sprintf("i8* getelementptr([%d x i8], [%d x i8]* %s, i32 0, i32 0)", n, n, gn))
					} else {
						v, err := g.value(b, c.Args[i])
						if err != nil {
							return "", err
						}
						argTypes = append(argTypes, "i32")
						argRegs = append(argRegs, "i32 "+v)
					}
				}
				ret := "i32"
				if ed.ReturnAnno != nil && ed.ReturnAnno.Kind == KindString {
					ret = "i8*"
				}
				callArgs := []string{}
				for i := range argTypes {
					callArgs = append(callArgs, argTypes[i]+" "+argRegs[i])
				}
				b.WriteString(fmt.Sprintf("  %s = call %s @%s(%s)\n", t, ret, irSymbol(fnName), strings.Join(callArgs, ", ")))
				return t, nil
			}
			b.WriteString(fmt.Sprintf("  %s = call i32 @%s(%s)\n", t, irSymbol(fnName), strings.Join(callArgWords(), ", ")))
		}
		g.checkExn(b)
		// a generator function returns a runtime heap list handle
		if g.genFuncs[fnName] {
			g.listOperands[t] = true
		}
	}
	return t, nil
}
