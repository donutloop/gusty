// Functions: the symbol a FuncDef is emitted under, its prologue and epilogue, its return road, and the
// questions the call roads ask about what it answers.
package lang

import (
	"fmt"
	"reflect"
	"strings"
)

// emitLambda registers a lambda as a generated anonymous FuncDef and emits
// its closure IR, returning the generated function name so Call can invoke it.
func (g *irGen) emitLambda(b *strings.Builder, lam *Lambda) (string, error) {
	name := fmt.Sprintf("lambda_%d", g.lambdaCounter)
	g.lambdaCounter++
	fd := &FuncDef{
		Name:   name,
		Params: lam.Params,
		Body:   []Stmt{&ReturnStmt{Expr: lam.Body}},
	}
	// emit the lambda FuncDef at module level (globals), not the current
	// statement builder, so the `define` is not nested inside main.
	if err := g.funcDef(&g.globals, fd); err != nil {
		return "", err
	}
	if g.funcs == nil {
		g.funcs = map[string]bool{}
	}
	g.funcs[name] = true
	if g.fds == nil {
		g.fds = map[string]*FuncDef{}
	}
	g.fds[name] = fd
	return name, nil
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
	saved := g.floatVars
	merged := make(map[string]bool, len(saved)+len(atStake))
	for k, v := range saved {
		merged[k] = v
	}
	for _, nm := range atStake {
		merged[nm] = true
	}
	g.floatVars = merged
	needs := funcReturnsFloat(g, fd)
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
		if bad := returnedFloatRebindings(fd, g.isFloat); len(bad) > 0 {
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
	if !fd.Async && returnsStringExpr(g, fd) {
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
