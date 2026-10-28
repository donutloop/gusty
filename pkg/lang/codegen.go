// codegen.go is the emitter's front door and the state behind it: GenerateIR,
// GenerateIRWithOptions and GenerateIRReport assemble one module, irGen carries the facts
// about the module being compiled, and the two post-passes that tidy the block layout
// (hoistAllocas and the debug-suffix strip) run over the text the roads produced.
//
// The lowering itself is spread over the files named for the question they answer:
// codegen_call.go dispatches a call to codegen_method.go, codegen_builtin.go and its family,
// codegen_func.go and codegen_instance.go; codegen_value.go dispatches an expression to
// codegen_binop.go and codegen_index.go; codegen_stmt.go dispatches a statement to
// codegen_assign.go, codegen_for.go, codegen_match.go and codegen_func.go. The rest answer
// one construct each — codegen_arith.go (a BinOp's predicates), codegen_comp.go (a
// comprehension or generator), codegen_class.go, codegen_text.go, codegen_container.go,
// codegen_float.go, codegen_minmax.go, codegen_module.go, codegen_gc.go, codegen_error.go (a
// raise, a try, a trap's sentence), codegen_truth.go (an i1 and the and/or connectives),
// codegen_union.go — and the analysis predicates live in codegen_facts.go, which emits no IR.
// The runtime IR blocks the module is assembled from are in codegen_ir_*.go, one file per
// block. One declaration lives in exactly one of them; nothing here re-decides a rule that
// lives there.
package lang

import (
	"fmt"
	"strconv"
	"strings"
)

// GenerateIR runs codegen and returns the module. No debug records are emitted:
// every caller that wants them asks for them (GenerateIRWithOptions /
// GenerateIRReport), so the IR an agent reads with --emit-llvm is byte-identical
// to the IR it was before L8.5 (docs/operations.md § Debug info, ADR 0231).
func GenerateIR(prog *Program) (string, error) {
	ir, _, err := GenerateIRReport(prog, nil)
	return ir, err
}

// GenerateIRWithOptions is GenerateIR with the emitter's choices spelled out.
func GenerateIRWithOptions(prog *Program, opts *IRGenOptions) (string, error) {
	ir, _, err := GenerateIRReport(prog, opts)
	return ir, err
}

// GenerateIRReport compiles a program to textual IR and, when debug records were
// requested, accounts for them (L8.5). The account is read back from the module
// text that was produced, so what `--debug-info` prints is what the module says
// and not what the emitter hoped for.
func GenerateIRReport(prog *Program, opts *IRGenOptions) (string, *DebugInfo, error) {
	// A function used as a value is defunctionalised before anything reads the program: the calls that pass
	// one are rewritten to the specialised clone this pass mints, so every door downstream sees calls that
	// name functions (roadmap L11.7, ADR 0345). It runs FIRST because the tables that decide a call's road
	// — the container-answer scan, the text and float return scans, the pair road's arity — are whole-program
	// reads of this same tree, and a rewrite that arrived after them would be a second opinion about a
	// program they had already answered. A program the pass cannot see through is handed back untouched,
	// with the refusal it always had — and with what the pass said while refusing it, which is the half the
	// emitter cannot work out for itself (roadmap Gap R.337, ADR 0379; the sentence rides to the emitter as
	// a value, beside the program it describes, because ADR 0343 forbids the package global this used to be).
	prog, callableSaid := defunctionalizeCallablesTelling(prog)
	// An applied `lambda` whose body answers a container becomes a declaration of its own, so that the
	// answer is registered, materialised at the `ret` and read by the SAME doors a `def`'s answer uses —
	// there is no second answer road for a `lambda` to fall out of step with. It runs beside the
	// defunctionalisation pass, and for the same reason: the container-answer scan, the pair road's
	// arity and the text-return scan are whole-program reads of this tree, and a declaration that
	// arrived after them would be a second opinion about a program they had already answered (roadmap
	// L11.7 · Gap R.382, ADR 0333, ADR 0344).
	prog, lambdaAnswerNames := hoistAppliedLambdaAnswers(prog)
	imports, err := resolveImports(prog, moduleSearchFor(opts))
	if err != nil {
		return "", nil, err
	}
	// The verdict facts of this program, read once: which parameter every call site fills with a
	// bool, and which name a body's own bindings hold as a bool. Both are AST questions, and both
	// are asked before any IR exists so a define and its call sites cannot disagree about what a
	// name is (roadmap Gap R.111, L11.1's remaining bool item).
	verdict := verdictFacts(prog)
	// Which functions take the (payload, tag) pair across their call boundary: asked of the whole program
	// before any IR exists, because a `define` and the `call`s to it must agree on the arity whichever
	// comes first in the file (roadmap L11.1, Gap R.139, ADR 0273).
	// One scan answers both questions, and answers them *to the caller*: the closed-position set used to be
	// left in a package variable for codegen to pick up, which two compiles in flight cannot share. ADR 0343.
	pairSpecs, pairClosed := pairCallScan(prog)
	pairReturns := map[string]bool{}
	for fnName, spec := range pairSpecs {
		if spec != nil && spec.returnsPair {
			pairReturns[fnName] = true
		}
	}
	// Which calls answer a value whose payload cannot say what it is. Asked BEFORE the tables that name a
	// function's answer kind, because that is the defect this pass closes: `strFuncs` registered a body on
	// the evidence of the first text it saw anywhere in it, and the caller then rendered every answer —
	// including the arm that answered a number — with the text printer (roadmap Gap R.219, ADR 0368). A
	// function the pair road already serves keeps it: its tag is a run-time fact, which beats the constant
	// one this scan can write.
	answer := answerKindPlanOf(prog, pairReturns)
	// Which callables hand back a text: the module's `def`s and, in a table of its own, the methods. One
	// question, asked once, over both — a method's body emits the same doors a function's body emits, and a
	// caller that was never told prints the @str_tab POSITION under the answer (roadmap Gap R.315, ADR 0366).
	strFuncTable, strMethodTable := textReturningCallables(prog, answer)
	g := &irGen{
		classIDs: map[string]int{}, nextSlot: 1,
		listVars:     map[string]bool{},
		runtimeDicts: map[string]bool{}, runtimeSets: map[string]bool{}, noneVars: map[string]bool{},
		boolVars:    map[string]bool{},
		listElemStr: map[string]bool{}, setElemStr: map[string]bool{}, dictKeyStr: map[string]bool{}, dictValStr: map[string]bool{}, listElemInt: map[string]bool{}, setElemInt: map[string]bool{}, dictKeyInt: map[string]bool{}, dictValInt: map[string]bool{}, internedVars: map[string]bool{}, strAttrs: map[string]bool{}, mixedLists: map[string]bool{}, mixedDicts: map[string]bool{}, mixedSets: map[string]bool{}, taggedVars: map[string]bool{}, strParamOf: strArgKinds(prog), boolParamOf: verdict.kinds, calleeBoolVars: verdict.of, strFuncs: strFuncTable, strMethodFuncs: strMethodTable, methodCensus: methodCensus(prog, verdict.kinds), strFillOf: stringFillingParams(prog), imports: imports, sym: map[string]string{}, allocd: map[string]bool{}, funcs: map[string]bool{}, funcBind: map[string]string{}, externs: map[string]*ExternDecl{}, genFuncs: map[string]bool{}, listOperands: map[string]bool{}, floatFuncs: map[string]bool{}, floatTemps: map[string]bool{}, fds: map[string]*FuncDef{}, params: map[string]string{}, fmtIdx: 0, strIdx: 0, tmp: 0, ldN: 0}
	// What the defunctionalisation pass had to say about THIS program, taken from the same call that
	// produced it: "" for a program the pass rewrote, and for one it found it had no work for at all.
	// The door that refuses a call with no `define` behind it is the one that reads it, because that is
	// the call the rewrite would have built (roadmap L11.7 · Gap R.337, ADR 0379).
	g.callablePassWord = callableSaid
	// The emitter mints `lambda_%d` for the `lambda`s this pass left on the road it has always taken.
	// It starts above every spelling the pass took, because two `define`s of one symbol is `llc`'s
	// duplicate-definition complaint at exit 2 (roadmap L11.7 · Gap R.382).
	g.lambdaCounter = lambdaAnswerNames
	// Which functions take the (payload, tag) pair across their call boundary: asked of the whole program
	// before any IR exists, because a `define` and the `call`s to it must agree on the arity whichever
	// comes first in the file (roadmap L11.1, Gap R.139, ADR 0273).
	// One scan answers both questions, and answers them *to the caller*: the closed-position set used to be
	// left in a package variable for codegen to pick up, which two compiles in flight cannot share. ADR 0343.
	g.gcReport = gcReportWanted(opts)
	g.pairSpecs, g.pairClosed = pairSpecs, pairClosed
	g.answer = answer
	g.pairRetDone = map[string]bool{}
	// Seeded from the scan, not from what has been emitted so far: `def f(v): return other(v)` written
	// above `def other(w): return w` asks whether other answers a pair while other's own body is still
	// ungenerated, and answering that question from the emission order made the answer depend on where in
	// the file the two `def`s happened to sit (the same rule as the arity above, roadmap L11.1, ADR 0276's
	// "asked of the program, not of the emission order"). The body's own emission still records the fact at
	// the road that stores the tag, so a returnsPair whose emission declines keeps refusing.
	for fnName, spec := range g.pairSpecs {
		if spec != nil && spec.returnsPair {
			g.pairRetDone[fnName] = true
		}
	}
	// The answer direction does NOT seed that flag, and the difference is load-bearing. pairRetDone is
	// the pair road's permission for the positions it serves, and `value`'s gate reads it to REFUSE any
	// other position that asks a call for one word; seeding it for a carried function would turn every
	// `while f():`, `x = f() + 1` and `f(f(1))` of a mixed-return body into a compile error at exit 0 on
	// a program the reference runs. The answer road is instead an OPT-IN: the positions that know how to
	// read a carried answer — print, str()/repr(), a truth test, an f-string field, a container element
	// — ask the answer door themselves, and every other position keeps the answer it had before this
	// cycle, wrong on one arm though that is (roadmap L11.1, ADR 0273's rule about a refusal that costs
	// a working program, Gap R.219's open remainder in Gap R.229 and Gap R.230, ADR 0368).
	g.pairCallAsked = map[string]bool{}
	g.pairTagDeclared = map[string]bool{}
	// The tree the emitter is lowering, kept for the one question a capture asks about a parameter: what do
	// the calls hand to it. A lambda's body is read by the AST scans as part of the expression it sits in, so
	// a name used ONLY inside a lambda has no call-site record of its own, and the capture road has to ask
	// the calls directly rather than trust a table the scan never filled (roadmap L11.7 · Gap R.265;
	// the same "asked of the program, not of the emission order" rule as the pair arity above).
	g.program = prog
	g.lambdaOrigin = map[*FuncDef]*Lambda{}
	// The forwarded-capture verdicts are per compile, like every other table here: two compiles alive at
	// once must not answer each other's capture question (ADR 0343's rule against toolchain state).
	g.lambdaCaptureVerdicts = map[*FuncDef]map[string]captureWordAnswer{}
	// A clone the defunctionalisation pass minted for an applied `lambda` that reads the frame around its
	// application carries one extra parameter per captured name, and the call road fills those by name — so the
	// list has to be on the table BEFORE the first call is lowered, which is the same "asked of the program,
	// not of the emission order" rule the pair arity above reads by (roadmap L11.7 · Gap R.334, ADR 0372).
	// The closure road fills the same table from the other end, at the moment it emits a `lambda`.
	g.lambdaCaptures = map[string][]string{}
	for _, s := range prog.Stmts {
		if fd, ok := s.(*FuncDef); ok && len(fd.Captures) > 0 {
			g.lambdaCaptures[fd.Name] = append([]string(nil), fd.Captures...)
		}
	}
	// Which functions hand back a container, and which one: asked of the whole program, before any IR
	// exists, so a `def` written below its first call and a call written below that `def` read one answer
	// (roadmap Gap R.67, ADR 0333; the same statelessness ADR 0273 requires of the pair road's arity).
	g.containerRet, g.containerRetOpaque = containerAnswerScan(prog)
	g.containerAsked = map[string]bool{}
	// What a compiled function body may know about its module: literal bindings the module never
	// rebinds are values; the rest stay refused with a message that says so (ADR 0227).
	g.moduleConsts, g.moduleNames = moduleEnvFor(prog)
	g.moduleBinds = map[string][]Expr{}
	scanRebinds(prog.Stmts, g.moduleBinds)
	// A name that some body DECLARES `global` is module state a body writes, so it is not a static fact
	// about this module: the record of "what was last bound to this name" only ever sees the module's own
	// statements, and a body's write arrives after every one of them. `TBD = None` / `def setup(): global
	// TBD; TBD = "ready"` / `setup()` / `print(TBD)` printed the module's None where the reference prints
	// ready — the fold answering a question the run time had already re-asked (roadmap **Gap R.48**,
	// **ADR 0448**). Demoting the name from the per-name kind records sends the read to the slot the write
	// actually touched; the kind-changing shape (an int slot later holding a text, a bool or a container)
	// is the tagged module word **L11.1** owns and is filed from this row rather than answered here.
	g.globalNames = programGlobalDeclNames(prog)
	// The VALUE records are demoted, and the KIND records are not. Which value a declared global holds is
	// the module's last binding as the folder saw it — and a body's write arrives after every one of those
	// statements, so `k = "a"` folded the print to `a` after a setup function had written `b`, and `TBD =
	// None` printed None for a body that had assigned a text. Which KIND it holds is a different question,
	// and the records that answer it are the same ones that format an ordinary variable: demoting those too
	// printed `1` for a declared global the module had bound to `False`, and `0` for one holding a text —
	// the untagged-handle signature this repo ranks worst (roadmap **Gap R.48**, **ADR 0448**;
	// **Gap R.22**). A name whose writes spell two kinds never reaches either record: the checker refuses it.
	for nm := range g.globalNames {
		delete(g.strVals, nm)
		delete(g.moduleBinds, nm)
	}
	g.strArgBindCache = map[string][]Expr{}
	g.moduleSlots, g.moduleFloatSlots, g.moduleContainers = moduleTablesFor(prog, g.moduleConsts, g.moduleNames, g.containerRet)
	g.moduleContainerReads = map[string]bool{}
	// The CONTAINER half of what a body may know about its module (roadmap Gap R.35): a name the module
	// binds once, to a literal container, that nothing in the program writes through, is handed to every
	// body that reads it as a binding of that literal — refused for every name the gate cannot vouch for.
	// Which container variables are still the literal they were bound to, which is what decides
	// whether reading one of their slots can be checked against the tag the builder wrote (ADR 0241).
	g.containerLits = containerLiteralsOf(prog)
	// What each container's slots can hold, spelled out of the literals the program stores into them:
	// the gate on answering `+` and `*` on a slot read at run time (ADR 0265).
	g.numChains = computeNumericSlotChains(prog)
	// Which names a body may read before assigning them is the checker's question, asked once here
	// so codegen does not grow a second, subtly different dataflow rule (ADR 0228).
	g.unwritten = UnwrittenReads(prog)
	// What a class pattern denotes, and every attribute name the program can write, come from the AST
	// and are settled *before* emission: the presence row cleared at each instantiation is only sound
	// if its length already covers every attribute slot the program will ever name (ADR 0235).
	g.classPat = classPatternsOf(prog)
	for _, a := range g.classPat.attrs {
		g.attrSlot(a)
	}
	g.fdAlias = map[*FuncDef]*FuncDef{}
	for nm, sym := range g.moduleSlots {
		g.moduleSlotDecls += fmt.Sprintf("@%s = global i32 0 ; module binding %q, read by a function body (ADR 0227)\n", sym, nm)
	}
	for nm, sym := range g.moduleFloatSlots {
		g.moduleSlotDecls += fmt.Sprintf("@%s = global double 0.0 ; module float binding %q, read by a function body (roadmap Gap R.35)\n", sym, nm)
	}
	// Debug records are opt-in; the marks that make them are one append per statement
	// and per function definition, collected whether or not they will be used, because
	// `--emit-source-map` wants the line table without asking for DWARF. Turning marks
	// into metadata is the gated part (ADR 0231).
	g.dbgOn = opts != nil && opts.Debug != nil
	g.echoOn = opts != nil && opts.EchoResult
	g.echoKinds = map[string]string{}
	g.decoratorNames = map[string]bool{}
	for _, st := range prog.Stmts {
		if fd, ok := st.(*FuncDef); ok {
			for _, d := range fd.Decorators {
				if nm, ok := d.(*Name); ok {
					g.decoratorNames[nm.Value] = true
				}
			}
		}
	}
	// pre-scan top-level for user function names
	// escape analysis: dead list-literal assignments skip rt_alloc
	g.deadLists = deadListAssignments(prog.Stmts)
	for _, st := range prog.Stmts {
		if fd, ok := st.(*FuncDef); ok {
			g.funcs[fd.Name] = true
			g.fds[fd.Name] = fd
			// A module-level function whose return is a string returns a @str_tab index, and
			// saying so here — at the pre-scan, before any call site is lowered — is what lets
			// `print(get()[1])` know that `get()` is a string at all. Methods were registered
			// this way since ADR 0224; the module's own functions were not, which is why
			// `s[1]` worked and `get()[1]` refused (ADR 0229).
			if !g.answerOwns(fd.Name) && g.methodReturnsStr(fd) {
				g.strFuncs[fd.Name] = true
			}
		}
	}
	g.decls = "declare i32 @printf(i8*, ...)\n"
	// AOT heap containers crossing function boundaries: which parameters receive
	// a list/dict/set handle (see heapargs.go).
	g.heapArgs = heapArgKinds(prog)
	g.globals.WriteString("@exn_flag = internal global i32 0\n")
	g.globals.WriteString("@gc_roots_used = internal global i32 0\n")
	cap := strconv.Itoa(gcRootCap)
	g.globals.WriteString(fmt.Sprintf("@gc.roots = internal global [%s x i32*] zeroinitializer\n", cap))
	// The root runtime's two libc dependencies are declared once, here, for the whole
	// module: LLVM rejects the same function declared twice, and they are needed by
	// code (`rt_root_put`'s overflow diagnostic, `rt_gc_report`) that is present even
	// in a module that neither raises nor prints a float (the strlen lesson, ADR 0173).
	// The root stack's data and code travel together with the features that need them.
	// The libc declarations are always emitted (LLVM rejects a function declared in two
	// blocks, so they may not live in the raise/float runtimes alongside their users);
	// the globals go with any module that has a heap or pushes roots, and the
	// primitives additionally with --gc-stats, whose report call lives in main.
	g.globals.WriteString("declare i64 @write(i32, i8*, i64)\n")
	g.globals.WriteString("declare i32 @snprintf(i8*, i32, i8*, ...)\n")
	g.globals.WriteString("@exn_code = internal global i32 0\n")
	var b strings.Builder
	// pre-scan top-level for user function names
	// user function definitions become separate defines before main
	// Register classes before compiling function bodies so that class
	// constructor calls and polymorphic dispatch are recognized inside
	// functions (e.g. `def make(): return Animal()`).
	for _, st := range prog.Stmts {
		if cd, ok := st.(*ClassDef); ok {
			g.registerClass(cd)
		}
	}

	for _, st := range prog.Stmts {
		if fd, ok := st.(*FuncDef); ok {
			if err := g.funcDef(&b, fd); err != nil {
				return "", nil, err
			}
		}
	}
	if err := g.emitModuleFuncs(&b); err != nil {
		return "", nil, err
	}
	// FFI: collect extern declarations and emit their C prototypes.
	for _, st := range prog.Stmts {
		if ed, ok := st.(*ExternDecl); ok {
			g.externs[ed.Name] = ed
			argTypes := []string{}
			for _, p := range ed.Params {
				if p.Annot != nil && p.Annot.Kind == KindString {
					argTypes = append(argTypes, "i8*")
				} else {
					argTypes = append(argTypes, "i32")
				}
			}
			ret := "i32"
			if ed.ReturnAnno != nil && ed.ReturnAnno.Kind == KindString {
				ret = "i8*"
			}
			g.decls += "declare " + ret + " @" + ed.Name + "(" + strings.Join(argTypes, ", ") + ")\n"
		}
	}

	// The module's statements are `main`'s body, and `main` is a function a debugger can be
	// asked about, so it gets a subprogram too. Its position is the first statement's, which is
	// what the prologue (root-stack reset, GC calls) really belongs to (ADR 0231).
	// `main`'s position is the first statement that is really its own code: a `def` or a
	// `class` at the top of a file is emitted as its own function, and the module's
	// prologue belongs to the first line that runs, not to the first line that exists.
	mainSpan := Span{Line: 1, Col: 1}
	for _, st := range prog.Stmts {
		switch st.(type) {
		case *FuncDef, *ClassDef, *ImportStmt, *ExternDecl:
			continue
		}
		if !st.Span().IsZero() {
			mainSpan = st.Span()
			break
		}
	}
	g.dbgDefine(&b, "main", "main", mainSpan)
	b.WriteString("define i32 @main() {\nentry:\n")
	// Module-level code is its own variable-binding scope. funcDef resets these
	// per body; without a reset here, an alloca emitted for a function parameter
	// named `xs` would make `xs = [1, 2]` in main skip its own alloca and store
	// through the (out-of-scope) `%_xs` register inside the function.
	// Module-level code is its own scope too (see beginScope).
	restoreScope := g.beginScope()
	defer restoreScope()
	// Which names hold @str_tab indices, and which functions hand one back — asked of the whole
	// program before any of it is lowered, so a use after a binding knows the kind the binding
	// gave it (ADR 0229). Asked *after* beginScope: that reset clears the per-scope kind maps, so
	// a scan answered before it would be silently thrown away — the same lesson the module-level
	// written-flags learned (ADR 0228), one cycle apart.
	g.scanStringBindings(prog.Stmts)
	// The module's own statements get written-flags too: an unwritten read at top level is a
	// NameError in CPython and must not read the alloca's previous contents (ADR 0228). The nil key
	// is the module's entry in the checker's table, and `inFunc` being false is what makes the trap
	// raise NameError rather than UnboundLocalError. It goes *after* beginScope: that reset clears the
	// slot bookkeeping, and flagging before it emitted a second alloca for the same name -- a module
	// `llc` rejects as "multiple definition of local value", which the exit-code contract calls a
	// compiler bug (ADR 0166).
	doneModuleFlags := g.enterBoundFlags(nil, prog.Stmts)
	defer doneModuleFlags()
	// A `global` declaration is checked against the body that writes it, and the check is PROGRAM-wide
	// rather than emission-wide: the reference refuses `def f(x): global x` and `def f(): print(x); global x`
	// when it COMPILES the function, so a program that never calls the function still refuses. An
	// emission-wide check would compile that program and answer with whichever of the two slots it happened
	// to read (roadmap **Gap R.48**, **ADR 0448**).
	if err := g.checkGlobalDecls(prog); err != nil {
		return "", nil, err
	}
	// The other three: a declaration no statement ever writes, a slot its writes fill with two different
	// kinds, and a declaration written in a class body. Each of these answered at the exit code of success
	// before the checker asked — a NameError the program never raised, a list printed as the 0 that was
	// bound first, a module printing a literal only the class body touched (roadmap **Gap R.48**, **ADR 0448**).
	if err := g.globalDeclChecks(prog); err != nil {
		return "", nil, err
	}
	g.emitBoundAllocas(&b)
	for _, ap := range g.applyCalls {
		b.WriteString(fmt.Sprintf("  call void %s()\n", ap))
	}
	b.WriteString("  store i32 0, i32* @gc_roots_used\n")
	// Root every module-global closure env slot so GC keeps captured envs
	// (and any heap handles they hold) alive across top-level boundaries.
	for _, envName := range g.envSlots {
		g.gcRegGlobal(&b, envName)
	}
	g.funcRaiseExit = "main.raiseexit"
	g.inMain = true
	// The REPL's echo, compiled: the last statement of a snippet is a *question* the reader
	// asked (L11.1's `1 + 1`, L11.2's repr of a text), so the caller that asked it gets the
	// answer back on the tool channel. Only a REPL/`--eval` caller turns this on; a program is
	// never echoed, because a program's stdout is only what the program printed (ADR 0204).
	echoTarget := g.echoTarget(prog)
	for _, st := range prog.Stmts {
		if _, ok := st.(*FuncDef); ok {
			continue
		}
		g.dbgMark(&b, st.Span())
		g.gcCall(&b)
		if st == echoTarget {
			if err := g.echoStmt(&b, st.(*ExprStmt).Expr); err != nil {
				return "", nil, err
			}
			continue
		}
		if err := g.stmt(&b, st); err != nil {
			return "", nil, err
		}
	}
	g.inMain = false
	// A refusal recorded while a method body was being emitted is a compile error and is
	// reported as one; it must never reach `llc` as a half-built function (ADR 0166,
	// roadmap Gap R.41).
	if g.emitErr != nil {
		return "", nil, g.emitErr
	}
	g.gcCall(&b)
	if g.gcReport {
		// The compiled backend's own collector self-report (--gc-stats). The numbers
		// live in the program's globals, so only its runtime can read them out; the
		// line goes to fd 2 like every other tool-level line (ADR 0179, ADR 0181).
		b.WriteString("  call void @rt_gc_report()\n")
	}
	b.WriteString("  ret i32 0\n")
	b.WriteString("main.raiseexit:\n")
	// An uncaught exception used to fall off the end of main and exit 0 printing
	// nothing, so a program that raised looked like a program that succeeded to any
	// script that ran it. Report it on stderr and exit non-zero, like the record --
	// and with the *same code as every other trap*, because ADR 0211 says a failure class
	// has one code whichever path produced it. This is the compiled binary's own exit code,
	// not the CLI's: `gustyc --aot prog.gy` and `./prog` must not disagree about whether the
	// program trapped, and 1 belongs to a compile error, so leaving 1 here made a trap look
	// like a compiler bug to any script running the binary directly.
	if g.gcReport {
		// A run that died of exhaustion is precisely the run whose numbers a reader needs: the report
		// goes out before the death line, so `exhausted=` is non-zero on the traceback run and the trap
		// is diagnosable after the fact instead of only from its sentence (roadmap Gap Q.2, ADR 0347).
		b.WriteString("  call void @rt_gc_report()\n")
	}
	if g.raiseUsed {
		b.WriteString("  %exn.m = load i8*, i8** @exn_msg\n")
		b.WriteString("  call void @rt_die(i8* %exn.m)\n")
		b.WriteString("  ret i32 3\n")
	} else {
		b.WriteString("  ret i32 0\n")
	}
	b.WriteString("}\n")
	// assemble output
	var out strings.Builder
	g.emitEnvGlobals()
	// Which runtime blocks a module needs is derived from what the module *references*,
	// not only from a flag each emitting path remembers. The flags (`heapUsed`,
	// `raiseUsed`, `floatFmtUsed`) are how a path announces what it lowers, and the ones
	// that forgot are exactly the invalid modules LLVM catches and the exit-code contract
	// calls a compiler bug: `def txt(): return "hi"` returns the interned index — correct,
	// ADR 0174 — but never marked the heap runtime used, so the module called
	// `@rt_str_intern2` with no definition of it, the same signature the roadmap blamed on
	// the await path (roadmap Gap R.2, ADR 0209). The flags stay: a container read raises
	// without an explicit `raise`, a printed float needs the formatter. But no block is
	// omitted while the emitted code mentions one of its names.
	bodyText := b.String()
	if g.raiseUsed || g.heapUsed || runtimeBlockReferenced(raiseRuntimeIR, bodyText) {
		// The frame stack travels with the raise block rather than with the pass that instruments
		// calls: rt_die reads the tables, so a module that has the reporter must have the tables
		// whether or not a push ever filled them (ADR 0173).
		g.globals.WriteString(raiseRuntimeIR)
		g.globals.WriteString(frameRuntimeIR)
	}
	// rt_fmt_double is only referenced by Python-style float rendering, so it travels in its own
	// block: a program that never prints a float does not pay for the snprintf/strtod
	// declarations. But the mixed container printer references it from the heap block, so the
	// heap block's own text counts as a reference too — a gate that reads only the body would
	// omit the formatter for `[1.5, "a"]` and produce a module with an undefined internal call,
	// which is the exact failure mode the preamble above warns about.
	needHeap := g.heapUsed || g.percentUsed || runtimeBlockReferenced(heapRuntimeIR, bodyText) ||
		runtimeBlockReferenced(percentRuntimeIR, bodyText)
	if g.floatFmtUsed || runtimeBlockReferenced(floatRuntimeIR, bodyText) || needHeap {
		g.globals.WriteString(floatRuntimeIR)
	}
	if needHeap {
		g.globals.WriteString(heapRuntimeIR)
		// The immutable container's half rides with the heap block because it *is* the heap block's
		// container question: `rt_print_mixed_value` asks `rt_tag_is_container` and the container
		// dispatcher asks `rt_print_list_or_tuple`, and a referenced internal function that was never
		// emitted is the module `llc` rejects (ADR 0173). The reference scan is what makes the gate honest
		// rather than unconditional: a module that carries neither door carries neither definition.
		if g.tupleUsed || runtimeBlockReferenced(tupleRuntimeIR, bodyText) || runtimeBlockReferenced(tupleRuntimeIR, heapRuntimeIR) {
			g.globals.WriteString(tupleRuntimeIR)
		}
		// The instance printer and the class-name table ride with the heap block because the heap block's
		// mixed printer references `@rt_instance_repr_ptr`, and an internal call that no block defines is the
		// undefined-symbol failure ADR 0173 made the whole rule about. The class names cannot live in the
		// static block: they are this module's classes, keyed by the dispatch ids its constructors stored
		// (roadmap Gap R.209, ADR 0325).
		g.globals.WriteString(g.instanceRuntimeIR())
	}
	// The tagged arithmetic door (roadmap L11.1, ADR 0265) is its own block for the same reason the two
	// above are: a program that never does arithmetic on a slot it cannot see should not carry a
	// snprintf-ing raise formatter. It rides with the heap block rather than alone, because
	// `rt_lift_num` asks the heap block for `rt_float_of` and `rt_float_new` — a referenced internal
	// function that was never emitted is the module `llc` rejects (ADR 0173, ADR 0192).
	if g.arithUsed || runtimeBlockReferenced(numArithRuntimeIR, bodyText) {
		if !needHeap {
			g.globals.WriteString(heapRuntimeIR)
		}
		g.globals.WriteString(numArithRuntimeIR)
	}
	// The fold door (roadmap L11.1, Gap R.146; ADR 0316) is its own block for the reason the two above
	// are: `sum`, which orders nothing, and every ordinary program answer without carrying an ordering
	// sentence. It rides with the arithmetic door, whose `rt_lift_num`, `rt_kind_name` and `rt.num.msg`
	// it asks — a referenced internal function that was never emitted is the module llc rejects
	// (ADR 0173, ADR 0192).
	if g.foldUsed || runtimeBlockReferenced(pairFoldRuntimeIR, bodyText) {
		if !needHeap {
			g.globals.WriteString(heapRuntimeIR)
		}
		if !g.arithUsed && !runtimeBlockReferenced(numArithRuntimeIR, bodyText) {
			g.globals.WriteString(numArithRuntimeIR)
		}
		g.globals.WriteString(pairFoldRuntimeIR)
	}
	// The printf-style formatting door (roadmap Gap R.31, ADR 0360) is its own block for the reason the
	// two above are: a program that never formats a text carries no format walker, no argument table and
	// no copy of the reference's eight sentences. It rides with the heap block because `%s` asks
	// `rt_str_of_value` and the mapping form asks `rt_dict_find`, and with the arithmetic door because a
	// wrong-kind conversion names its type through `rt_kind_name` — a referenced internal function that
	// was never emitted is the module `llc` rejects (ADR 0173).
	if g.percentUsed || runtimeBlockReferenced(percentRuntimeIR, bodyText) {
		if !needHeap {
			g.globals.WriteString(heapRuntimeIR)
		}
		if !g.arithUsed && !runtimeBlockReferenced(numArithRuntimeIR, bodyText) {
			g.globals.WriteString(numArithRuntimeIR)
		}
		g.globals.WriteString(percentRuntimeIR)
	}
	// The sequence doors (roadmap Gap R.33, ADR 0365) are gated the way the formatting door and the fold
	// door are: a program that never concatenates two lists, repeats a sequence or repeats a text carries
	// no slot copier. They ride with the heap block because the two container doors call `rt_alloc` and the
	// text door asks `rt_str_len`/`rt_str_ptr`/`rt_str_intern`, and a referenced internal function that was
	// never emitted is the module `llc` rejects (ADR 0173).
	if g.seqUsed || runtimeBlockReferenced(seqRuntimeIR, bodyText) {
		if !needHeap {
			g.globals.WriteString(heapRuntimeIR)
		}
		g.globals.WriteString(seqRuntimeIR)
	}
	// The case door (roadmap L11.5, ADR 0362) is gated the way the formatting door and the frame stack are:
	// the tables are the reference's answer for 1.1 million code points, and a program that never asks a
	// case question has no business carrying them. The block reads the interned-text table
	// (`rt_str_ptr`/`rt_str_nchars`/`rt_str_intern`), so it rides with the heap block — a referenced
	// internal function that was never emitted is the module `llc` rejects (ADR 0173). The table is the
	// artifact's, and a table that will not load is a compiler error rather than a fallback to Go's answer.
	caseIR := ""
	if t, terr := loadCaseTable(); terr == nil {
		caseIR = caseRuntimeBlock(t, g.caseOps)
	} else if g.caseOps != 0 {
		return "", nil, terr
	}
	if g.caseOps != 0 || (caseIR != "" && runtimeBlockReferenced(caseIR, bodyText)) {
		if !needHeap {
			g.globals.WriteString(heapRuntimeIR)
		}
		g.globals.WriteString(caseIR)
	}
	// The REPL echo's writer (roadmap L13.1, ADR 0302) rides with the heap block for the same
	// reason the arithmetic door does: it renders through `rt_str_ptr`/`rt_str_len`, which live
	// there, and a referenced internal function that was never emitted is the module `llc`
	// rejects.
	if g.echoUsed || runtimeBlockReferenced(echoRuntimeIR, bodyText) {
		if !needHeap {
			g.globals.WriteString(heapRuntimeIR)
		}
		// The pair half of the echo announces a value's kind from its tag, and the tag vocabulary lives in
		// the tagged-arithmetic block (`rt_kind_name`) — the one table the operand-type messages read, so
		// the prompt, a TypeError and a container printer call a slot the same thing (ADR 0259). A
		// referenced internal function that was never emitted is the module llc rejects (ADR 0173), so the
		// block rides with the echo the way it rides with the heap.
		if !g.arithUsed && !runtimeBlockReferenced(numArithRuntimeIR, bodyText) {
			g.globals.WriteString(numArithRuntimeIR)
		}
		g.globals.WriteString(echoRuntimeIR)
	}
	// The precise-root stack runtime (ADR 0181) is emitted whether or not the program
	// allocates: every function prologue calls rt_frame_open, and a referenced but
	// undefined internal function fails verification. It is emitted after the other
	// runtime blocks so it can see which libc names they already declared.
	// Both halves of the root runtime are emitted *after* the body has been generated:
	// only then does codegen know whether this module touches the root stack at all.
	// (Emitting the data half in the preamble, where `rooted` was still false while the
	// function half emitted later referenced it, produced modules whose @gc.kinds was
	// missing — the verifier caught it, the tests did not until this round.)
	//
	// The gate reads `needHeap` and not the flag alone: a body that calls a heap helper
	// without the flag being set (a keyed `min` compares its winners with @rt_elem_gt, which
	// lives in that block, and allocates nothing of its own) pulls the whole heap block in
	// textually — including rt_gc, which reads @gc.kinds — and a data half gated on the flag
	// alone then stays home. That asymmetry is the same undefined-symbol class ADR 0173 made
	// the whole rule about (found by ADR 0340).
	if g.rooted || needHeap || g.gcReport {
		g.globals.WriteString(renderedRootGlobals())
	}
	if g.rooted || g.gcReport {
		g.globals.WriteString(renderedRootRuntime())
	}
	out.WriteString(g.moduleSlotDecls)
	// A closure body that could not be lowered and that a decorated call does not run is said here,
	// in the module, where --emit-llvm and the verifier can both see it. It is not a hidden failure:
	// the program runs because the trampoline, not this closure, is what executes (ADR 0227).
	for _, note := range g.closureBodyNotes {
		out.WriteString("; note: " + note + "\n")
	}
	// The debug pass needs to know where in the finished module each builder's writes landed:
	// marks were recorded as offsets into `globals` and into the body builder, and those two
	// address spaces become one here (L8.5, ADR 0231).
	out.WriteString(g.strGlobals.String())
	// The generated `lambda` definitions, in the region the roads that own a `define` never write into.
	out.WriteString(g.lambdaDefines.String())
	globalsPrefix := out.Len()
	out.WriteString(g.globals.String())
	EmitABI(&g.decls)
	bodyPrefix := out.Len() + len(g.decls)
	out.WriteString(g.decls)
	out.WriteString(b.String())
	// PIC Level = 2 module flag: forces llc to emit position-independent code
	// so string constants in .rodata are referenced PIC-safely. Without it llc
	// defaults to the static relocation model, which emits 32-bit absolute
	// relocations (e.g. R_X86_64_32) that the default PIE link (cc) rejects.
	out.WriteString("!llvm.module.flags = !{!0}\n")
	out.WriteString("!0 = !{i32 2, !\"PIC Level\", i32 2}\n")
	// The shadow frame stack goes in over the finished text and before the line table: it is the
	// one pass that may *add instructions*, so it runs ahead of attachDebugInfo resolving its
	// `!dbg` records against the statement marks, and it hands those marks back shifted by what
	// it inserted (Gap K.8, ADR 0354; the offset discipline is ADR 0231's).
	//
	// The gate is `raiseUsed` and nothing else. It is set by `setExn` — the door every raise goes
	// through, the program's own `raise` and the machine-detected index, key and conversion traps
	// alike — so a program that can never report a trap keeps the module it always had, IR line
	// numbers and all, which is what the fold-purity and line-table guards check.
	moduleText := out.String()
	marks := g.dbgMarks
	if g.raiseUsed {
		moduleText, marks, _ = attachFrameStack(moduleText, g.dbgMarks, g.dbgFuncs, globalsPrefix, bodyPrefix)
	}
	// The frame-slot audit, asked of the bytes `llc` would have been handed: every slot a `define` reads must
	// be a slot that SAME `define` allocated. It is not a lint. The bug it catches is roadmap Gap R.342 — a
	// body emitted without entering its own frame read the ENCLOSING frame's `%_w`, `llc` answered
	// `use of undefined value '%_w'`, and three lines of ordinary source left at exit 2 with a temp path and
	// an external tool's stack trace to explain a compiler bug. The compiler saying it first names the
	// function and the slot instead of a filename under /tmp, and it is the reason the rule in
	// `frame_slots.go` cannot quietly rot back: a road that adds a body without entering its frame fails
	// here rather than in the field (roadmap L11.6 · Gap R.342, ADR 0378).
	//
	// Asked only when nothing else refused: a program the compiler already declined is reported as the
	// refusal it got, not as a second opinion about the half-module it left behind.
	if g.emitErr == nil {
		if err := auditFrameSlots(moduleText); err != nil {
			return "", nil, err
		}
	}
	// The line table is added to the module as assembled text, before the slot hoisting that
	// moves allocation instructions to the top of their function: a hoisted `alloca` keeps the
	// `!dbg` it was tagged with, so the slot stays attributed to the assignment that created it
	// rather than to the `def` (ADR 0231, and ADR 0181 for the hoisting itself).
	if opts != nil && opts.Debug != nil {
		text, art := attachDebugInfo(moduleText, marks, g.dbgFuncs, globalsPrefix, bodyPrefix, opts.Debug)
		// Slot hoisting first, *then* the account of the table: hoisting moves allocation
		// lines to the top of their function, and a line table whose rows were counted
		// before that happened would publish IR positions the shipped module does not have.
		shipped := hoistAllocas(text)
		return shipped, readBackDebugInfo(shipped, art), nil
	}
	// Every variable slot has to be allocated once per call for its *address* to
	// identify it — see hoistAllocas (ADR 0181).
	return hoistAllocas(moduleText), nil, nil
}

// runtimeBlockReferenced reports whether emitted module code mentions any name that the
// given runtime block defines. The decision is derived from the artifact rather than from a
// flag, so a helper cannot be called without being defined no matter which codegen path
// emits the call — and a data global (@str_tab, @gc.roots) cannot be referenced while its
// definition is missing.
//
// The block is scanned for `define … @name(` and `@name = internal global/constant` lines,
// which is what those files already look like; nothing is maintained by hand, so adding a
// helper to a block cannot reintroduce the bug.
func runtimeBlockReferenced(block, module string) bool {
	for _, ln := range strings.Split(block, "\n") {
		t := strings.TrimSpace(ln)
		var name string
		callForm := false
		switch {
		case strings.HasPrefix(t, "define "):
			i := strings.Index(t, "@")
			if i < 0 {
				continue
			}
			name = t[i+1:]
			if j := strings.IndexAny(name, "( "); j >= 0 {
				name = name[:j]
			}
			callForm = true
		case strings.HasPrefix(t, "@"):
			rest := t[1:]
			sp := strings.IndexAny(rest, " =")
			if sp < 0 {
				continue
			}
			tail := strings.TrimLeft(strings.TrimSpace(rest[sp:]), "= ")
			if !strings.HasPrefix(tail, "internal global") && !strings.HasPrefix(tail, "internal constant") && !strings.HasPrefix(tail, "private") && !strings.HasPrefix(tail, "common") {
				continue
			}
			name = rest[:sp]
		}
		if name == "" {
			continue
		}
		if callForm {
			if strings.Contains(module, "@"+name+"(") {
				return true
			}
			continue
		}
		if strings.Contains(module, "@"+name) {
			return true
		}
	}
	return false
}

// isAllocaLine reports whether a module line defines a stack slot, e.g.
// `  %_p = alloca i32`.
func isAllocaLine(ln string) bool {
	t := strings.TrimSpace(ln)
	return strings.HasPrefix(t, "%") && strings.Contains(t, " = alloca ")
}

// hoistAllocas moves every stack-slot allocation to the top of its own function.
//
// A precise root stack identifies a variable by the *address* of its slot, so the
// address must be one per variable per call. Codegen emits `%_p = alloca i32` where
// the variable is first assigned, which for a loop body means an alloca instruction
// inside the loop — and at llc's default -O0 nothing hoists it, so each iteration
// bumped a fresh frame slot. Consequences measured, not guessed: the machine stack
// grew by a slot per iteration, and the root entry for that variable never matched the
// one recorded before, so the root stack grew by one entry per iteration (a loop that
// built 2000 instances reached top=2002), every stale slot kept its object live, the
// 1024-slot heap filled up, and the program died. Hoisting the allocation gives each
// variable exactly one slot per call, which is also what a local variable means in C.
func hoistAllocas(module string) string {
	lines := strings.Split(module, "\n")
	out := make([]string, 0, len(lines))
	for i := 0; i < len(lines); {
		ln := lines[i]
		if !strings.HasPrefix(ln, "define ") || !strings.HasSuffix(strings.TrimSpace(ln), "{") {
			out = append(out, ln)
			i++
			continue
		}
		end := i + 1
		for end < len(lines) && lines[end] != "}" {
			end++
		}
		if end >= len(lines) {
			// No closing brace: emit the rest unchanged rather than guess.
			out = append(out, lines[i:]...)
			return strings.Join(out, "\n")
		}
		out = append(out, ln)
		body := lines[i+1 : end]
		var lead []string
		if len(body) > 0 && strings.HasSuffix(strings.TrimSpace(body[0]), ":") {
			lead = append(lead, body[0])
			body = body[1:]
		}
		var allocas, rest []string
		for _, bl := range body {
			if isAllocaLine(bl) {
				allocas = append(allocas, bl)
			} else {
				rest = append(rest, bl)
			}
		}
		out = append(out, lead...)
		// A hoisted variable slot also gets the one thing a road that forgets to initialise its slot
		// cannot supply from where it sits: a defined value. `%_x = alloca i32` with nothing written to
		// it is read by the release-the-old-binding prologue (`load i32, i32* %_x` / `rt_release`), and the
		// frame's previous contents are whatever the last call left on the stack — a garbage handle fed
		// to the release door corrupts the allocator's free list, the next rt_alloc walks off the arena, and the
		// program dies by SIGSEGV with the artifact, not the source, at fault (roadmap Gap R.258, found
		// by ADR 0343's overlapping cases). Zero is what "this variable holds no object yet" already
		// means everywhere else: the container-binding road stores 0 when it creates a slot, and
		// emitFree's null test is the door that turns 0 back into "no object". Asking here is the rule,
		// rather than the forty-site hope.
		//
		// The stores go *after* the whole alloca run, not beside their slots: a root stack identifies a
		// variable by slot address, so every allocation must still precede every instruction of the block
		// (TestNoAllocaBelowTheFirstInstruction), and a slot is only read below this prologue anyway.
		// A slot whose road already cleared it is left alone — that is also what makes the pass
		// idempotent, which gc_roots_ir_test checks by running it twice.
		have := make(map[string]bool, len(body))
		for _, bl := range body {
			have[stripDbgSuffix(strings.TrimSpace(bl))] = true
		}
		var inits []string
		for _, al := range allocas {
			zero, ok := zeroInitForAlloca(al)
			if !ok || have[zero] {
				continue
			}
			// The store inherits its slot's location: the line table's rule is that every instruction of
			// a program function carries a location, and an unlabelled instruction among labelled ones
			// reads as a hole in the source correspondence (ADR 0231, ADR 0181).
			if i := strings.Index(al, ", !dbg !"); i >= 0 {
				zero += strings.TrimSpace(al[i:])
			}
			inits = append(inits, "  "+zero)
		}
		out = append(out, allocas...)
		out = append(out, inits...)
		out = append(out, rest...)
		out = append(out, "}")
		i = end + 1
	}
	return strings.Join(out, "\n")
}

// zeroInitForAlloca decides whether a hoisted allocation line needs the defined-value store with it, and
// spells it. Only a variable slot — `%_x`, or a tag slot `%_x_tag`, both `i32` — gets one: the runtime's own
// temporaries (`%ps`, `%ci`) belong to hand-written code that initialises them, and a `%bnd_x` written-flag
// is an `i8` its own emitter already clears.
func zeroInitForAlloca(line string) (string, bool) {
	trimmed := stripDbgSuffix(strings.TrimSpace(line))
	if !strings.HasSuffix(trimmed, "= alloca i32") {
		return "", false
	}
	name := strings.TrimSpace(strings.TrimSuffix(trimmed, "= alloca i32"))
	name = strings.TrimSpace(strings.TrimSuffix(name, "="))
	if !strings.HasPrefix(name, "%_") {
		return "", false
	}
	return "store i32 0, i32* " + name, true
}

// stripDbgSuffix removes a trailing `!dbg !N` so the same instruction compares equal whether or not the
// module carries the line table — the plain and the debug module must get the very same stores, or the
// debug test's line-by-line comparison against the plain one stops being a comparison.
func stripDbgSuffix(line string) string {
	if i := strings.Index(line, ", !dbg !"); i >= 0 {
		return strings.TrimSpace(line[:i])
	}
	return line
}

type irGen struct {
	// gcReport is this module's own copy of the collector self-report switch, taken from the run's options
	// (or the process default, for a caller that has no options). Reading the package switch from three
	// places during emission made the answer depend on what some other compile had set (ADR 0343).
	gcReport bool
	// callablePassWord is the defunctionalisation pass's own sentence about the program being lowered,
	// carried in from the call in GenerateIRReport that produced the program beside it — "" unless the
	// pass had a shape it wanted to build and could not prove. It is per-module state, not a package
	// variable, for ADR 0343's reason: two compiles in flight must not answer each other's refusals.
	// The one door that reads it is `callBuiltins`, refusing a call no `define` stands behind — the call
	// the withdrawn rewrite would have built (roadmap L11.7 · Gap R.337, ADR 0379).
	callablePassWord string
	globals          strings.Builder
	strGlobals       strings.Builder // string constants, emitted at top of IR
	// lambdaDefines holds the `define`s the lambda road minted. They cannot join `g.globals` as they are
	// finished: a method body and a closure body are themselves being written into that stream, so a
	// lambda applied inside one — the shape `(lambda y: y + 1)(x)` written in a method — landed in the
	// MIDDLE of the enclosing `define` and `llc` answered `multiple definition of local value named
	// 'gc.frame.base'` (roadmap L11.7, Gap R.265, ADR 0370). Module-level `define`s may come in any
	// order, so they wait here and are written beside the other module-level text.
	lambdaDefines strings.Builder
	// captureWordMemo and captureWordAsking are the state of the gate that types a capture by asking the
	// frame's own call sites (ADR 0370): one verdict per (frame, parameter), and the set of questions this
	// answer is being derived from, which is what turns a definition that refers to itself into a refusal
	// rather than into a recursion that reaches the runtime's stack limit.
	captureWordMemo   map[string]captureWordAnswer
	captureWordAsking map[string]bool
	// programCallList is `callableCallsOf(g.program)` walked once: the reflective walk is the pass's and
	// is not cheap, and the gate above asks it per capture.
	programCallList []*Call
	decls           string
	sym             map[string]string // variable -> load temp
	allocd          map[string]bool   // alloca emitted?
	// frameTargets counts the names the frame now being emitted binds for ITSELF — today, a comprehension's
	// own target — which is the question the capture roads ask before they mint a parameter for a name
	// (roadmap L11.7 · Gap R.348; `frame_targets.go` is the rule).
	frameTargets map[string]int
	// rtComps records the comprehensions whose elements are computed at runtime, so a
	// consumer (sum/min/max/len folding) cannot read an empty compile-time element set and
	// answer 0 for a list that has elements (roadmap L11.7, ADR 0192).
	rtComps map[*Comp]bool
	// noneVars records variables whose latest assignment is the None singleton, so
	// print/truthiness/equality can be decided statically (ADR 0172).
	noneVars map[string]bool
	// boolVars records variables whose latest assignment produced a bool, so print and str
	// can render True/False instead of the 0/1 the comparison left in the slot. Same standing
	// as noneVars (ADR 0172) and internedVars (ADR 0224): the front end reads the program and
	// the runtime renders (roadmap L11.1 step 2, ADR 0257).
	boolVars map[string]bool
	// listElemStr / setElemStr / dictKeyStr / dictValStr record that a container's elements
	// are interned strings (indices into @str_tab), which decides how they print and how
	// element reads behave (roadmap Gap I.2).
	listElemStr map[string]bool
	setElemStr  map[string]bool
	dictKeyStr  map[string]bool
	dictValStr  map[string]bool
	// The same four facts for numbers. A compiled container records one element kind, so a
	// container that has held both must be reported rather than printed through the wrong
	// table (roadmap Gap J.6).
	listElemInt map[string]bool
	setElemInt  map[string]bool
	dictKeyInt  map[string]bool
	dictValInt  map[string]bool
	// caughtVars records the name an `except <Type> as name:` clause bound: the word in its slot
	// is an @str_tab index, so the PRINT roads take it as a text and show the message the raise
	// carried. It is deliberately NOT the internedVars record: the operand roads (len, a text
	// method, a concatenation, a comparison) ask that one, and the reference refuses each of them
	// for an exception object -- answering "yes" there is the exit-0 wrong answer ADR 0166 refuses
	// to ship (roadmap Gap R.113, ADR 0406; the doors are Gap R.395's).
	caughtVars map[string]bool
	// internedVars records names bound to an interned-string index (element reads and loop
	// variables over string containers), so print renders the text rather than the index.
	internedVars map[string]bool
	// digitLoopVars records a loop variable that THIS loop bound from a number the compiler can
	// see — the counter of a numeric loop, or an element of a container whose recorded element kind
	// is integer. It is the loop's own claim, taken back when the loop ends and never set for a
	// name the program also assigns to, which is what makes it a fact about the value rather than a
	// guess from the spelling (roadmap L11.1, ADR 0339's per-loop question; ADR 0359 uses it where
	// an interpolated field needs its digits and the str door has no arm for a name no binding
	// describes).
	digitLoopVars map[string]bool
	// mixedLists records list variables whose elements carry per-element tags (more than
	// one kind, all taggable: numbers, interned strings, None). Printing them works; every
	// other element-wise use refuses rather than reading a tag through one static kind
	// (roadmap L11.1, ADR 0184).
	mixedLists map[string]bool
	// mixedDicts and mixedSets are the same rule for the other two containers: the slots
	// describe themselves, so the container has no single element kind to record. A
	// container in these maps prints, dedups and looks up by per-slot tag, and the static
	// dictKey*/dictVal*/setElem* maps say nothing about it (roadmap L11.1 (1b), ADR 0232).
	mixedDicts map[string]bool
	mixedSets  map[string]bool
	// taggedVars records variables bound by a loop over a mixed list: their value slot is an
	// i32 whose meaning depends on the companion tag slot, so printing dispatches on the tag
	// and every other use refuses (roadmap L11.1, ADR 0185).
	taggedVars map[string]bool

	// taggedOrigin says which door bound each tagged variable's pair, so a refusal can name the
	// shape that produced the tag instead of blaming a loop for a binding a statement made — the
	// rule Gap R.38 states about what a refusal may claim (roadmap L11.1, Gap R.138).
	taggedOrigin map[string]string

	// loopElemTag is the unrolled-loop companion of taggedVars: `for v in [1.5, "a", None]` emits
	// one body copy per element, and the body needs to know what *this* element's slot holds to
	// print it. The value is the canonical ValueTag; the entry exists only while a body copy is
	// being emitted (roadmap L11.1, ADR 0233).
	loopElemTag map[string]int32
	// loopElem is the same unroll's record of the ELEMENT itself: while a body copy is being emitted,
	// the name the loop binds is that expression for this iteration. The tag above says what the item
	// IS; this says which item it is, which is the half a missing-member sentence needs - `for k in
	// [1, 2, 9]: s.remove(k)` names `9`, not `key not found` (roadmap Gap R.189's sentence, on the set
	// side; roadmap Gap R.440's record, one door over). Read by the KeyError road alone.
	loopElem map[string]Expr
	// strParamOf maps a function name to the parameter indices that receive strings; the
	// callee marks them in internedVars and the caller interns the argument (Gap J.5).
	strParamOf map[string]map[int]bool

	// calleeBoolVars is the same reading seen from the calling side: what a called body's own
	// bindings hold, so `def isit(): b = True; return b` answers True and not 1 (Gap R.111).
	calleeBoolVars func(string) map[string]bool

	// boolParamOf is the verdict twin of strParamOf: which parameters every call site in the
	// program fills with a bool, read from the AST by boolArgKinds. A position contradicted by
	// any call site is absent, and the body then prints the number it always printed
	// (roadmap Gap R.111, ADR 0257's unfinished edge, L11.1's remaining bool item).
	boolParamOf map[string]map[int]bool
	// strFuncs names the functions that return a string, so `print(echo("yo"))` prints text
	// and `xs.append(make_key())` stores an interned element rather than a raw index.
	strFuncs map[string]bool
	// answer is the answer direction: which calls answer a value whose payload cannot say what it is,
	// and what kind word each `return` road of such a body stores beside its payload. Read by the
	// gates that register a function under ONE kind, by the `return` road that stores the word, and by
	// every position that reads such an answer (roadmap L11.1, Gap R.219, Gap R.228, ADR 0368).
	answer *answerKindPlan
	// strMethodFuncs is the same verdict for a method, kept in its own table because a method's IR symbol is
	// the mangled `Class_method` while its source name is what a call site spells: registerClass moves an
	// entry from here into `strFuncs` under the mangled symbol. Keeping the two apart is what lets a `Name`
	// call be answered only by a `def` (roadmap Gap R.315, ADR 0366).
	strMethodFuncs map[string]bool
	// methodCensus is the method-side reading of the program — which method one callable owns, and
	// which class a receiver hands back — asked by the verdict road so a verdict a METHOD hands back
	// is rendered as one (roadmap Gap R.316, ADR 0365's follow-on to L15.4).
	methodCensus methodCensusT
	// curClassSrc is the class whose method is being lowered, so a `self.other()` call inside a body
	// can be asked of that class; "" outside a method.
	curClassSrc string
	// strFillOf reports, per function and container parameter, which positions the body fills
	// with strings; a call site transfers that onto the caller's own variable so printing an
	// element later does not show a raw @str_tab index (Gap J.5).
	strFillOf map[string]map[int]int
	// pairSpecs is the call-boundary decision of the (payload, tag) pair: which parameters of which
	// functions arrive as two words, and which bodies answer with a pair whose kind the callee stores
	// beside its return. It is a pure scan of the AST (paircall.go) because the `define` of a function
	// written after its first call must agree with that call about the arity (roadmap L11.1, ADR 0273).
	pairSpecs map[string]*pairFnSpec
	// pairClosed is the scan's second answer: the (function, parameter) positions it considered for the
	// pair and closed. Handing a float into one is refused rather than truncated (roadmap Gap R.164, ADR 0280).
	pairClosed map[string]map[int]bool
	// pairRetDone is the subset of pairSpecs whose answer direction the emitted body actually carries —
	// a float- or string-returning function keeps its own convention, so no caller may read a tag for it.
	pairRetDone map[string]bool
	// pairTagDeclared remembers which functions' tag words this module has been given a definition for.
	// The definition is written where the function's own `define` is emitted, and the read can happen in a
	// caller emitted before it, so the two cannot share a guard (roadmap L11.1, ADR 0276's rule that the
	// call boundary is asked of the program, not of the emission order).
	pairTagDeclared map[string]bool
	// pairCallAsked is the permission a pair-aware position holds while it emits such a call: the
	// ordinary road refuses these calls, because a payload used as though it were the whole value is the
	// float-box-handle-printed-as-an-int class of wrong answer (roadmap L11.1, ADR 0273).
	pairCallAsked map[string]bool
	// curMethodAnswer is the answer direction's METHOD round in emission form: the symbol of the method
	// body being lowered, when that body answers more than one kind — or answers a container, which a `def`
	// hands to `containerRet` and a method has no table for. It is the word `storePairTag` writes the arm's
	// kind into and the word the caller's pair door reads back; "" for every body that keeps the roads it
	// has always had, including a method that answers one immediate kind (roadmap Gap R.453, ADR 0430).
	curMethodAnswer string
	// popPairMemo is the popped slot's door, remembered per call node: a position that asks the pair and
	// then asks it again must not remove the element twice, so the second ask answers the registers the
	// first emitted (roadmap L11.1, ADR 0410).
	popPairMemo map[*Call][2]string
	// containerAlias is the notebook's record of the binding that copies a handle and nothing else:
	// `ys = xs` gives `ys` the object `xs` names, so the claims a read of `ys` asks — does its slots
	// describe themselves, what kind do they hold — are answered by `xs`' record (roadmap L11.1,
	// Gap R.415, ADR 0412). Cleared at the binding that hands either name a different object.
	containerAlias map[string]string
	// containerRet is the scan's answer to "which functions hand back a container, and which container"
	// (roadmap Gap R.67, ADR 0333). The answer is one word — a heap handle — and its kind is a fact about
	// the function's whole body, so it is written down once here rather than guessed per call site: a
	// caller that renders a number through the container printer is Gap R.219's defect wearing a tag.
	containerRet map[string]containerAnswer
	// containerRetOpaque is the scan's second answer: the functions whose `return` provably hands back a
	// container and whose slots the answer table declined to describe. They are the difference between a
	// program that is not a container and a program whose container this backend will not read as a
	// number — the first prints 1, the second refuses (roadmap Gap R.261, ADR 0344).
	containerRetOpaque map[string]containerAnswer
	// containerAsked is `containerAnswerOperand`'s permission, beside `pairCallAsked`'s: the ordinary
	// single-word road refuses a call whose answer is a container, and the door that has just handed that
	// answer to a road which can read one lowers the call with this set marked (ADR 0273's rule, applied
	// to the container answer by ADR 0333).
	containerAsked map[string]bool
	// doubleSlot records the names whose stack slot was allocated as a `double`, which is the one fact
	// that says whether a later `store double` fits: `floatVars` says what the newest binding *is*, and a
	// tagged rebinding deletes that record while the allocation stays behind. Read by the pair road that
	// boxes a double bound to an `i32` slot (roadmap L11.6, Gap R.155, ADR 0274).
	doubleSlot map[string]bool
	funcs      map[string]bool
	funcBind   map[string]string
	externs    map[string]*ExternDecl // user-defined function names
	fds        map[string]*FuncDef    // function definitions by name (for call arg binding)
	imports    *ImportInfo            // folded module globals for `import mod`
	params     map[string]string      // current function params: name -> register
	// paramSlot names the parameters whose body rebinds them: their slot (`%_name`),
	// not the incoming argument register, is what reads load (Gap R.3, ADR 0196).
	paramSlot map[string]bool
	// forCtrSeq numbers the private induction counters of `for ... in range(...)`
	// loops, which are no longer allowed to share the loop variable's slot (Gap R.3b).
	forCtrSeq int
	fmtIdx    int
	strIdx    int
	tmp       int
	label     int
	ldN       int
	loopStack []loopInfo

	closures    map[string]*closureInfo
	envMode     bool
	envCaptures map[string]int
	envParam    string
	decorated   map[string]bool
	deadLists   map[string]bool
	inFunc      bool
	applyCalls  []string
	listNames   map[*ListLit]string
	lstIdx      int
	dictNames   map[*DictLit]string
	dictIdx     int
	setNames    map[*SetLit]string
	setIdx      int
	compNames   map[*Comp]string
	// compLen records the folded element count of each lowered comprehension,
	// so indexing can bounds-check and emit the correct GEP shape.
	compLen map[*Comp]int
	// compEls records the folded element constant of each lowered comprehension,
	// so aggregate builtins (sum/min/max) can fold over the comprehension.
	compEls map[*Comp][]int64
	// compKeys records the folded key constants of each lowered dict
	// comprehension, so `d[key]` on a dict comprehension can be resolved at
	// codegen time (set/list comprehensions have no separate keys).
	compKeys map[*Comp][]int64
	// constBindings maps a comprehension variable name to its compile-time
	// constant so comprehension bodies can be unrolled at codegen time.
	constBindings map[string]int64
	// strVals maps a variable name to its concrete string constant value,
	// so `len(s)` and string concat can be resolved at codegen time.
	strVals map[string]string
	// dictVals maps a variable name to its concrete dict literal value,
	// so `d[key]` on a dict variable can be resolved at codegen time.
	dictVals map[string]*DictLit
	// lambdaCounter numbers generated anonymous functions (lambda_0, ...).
	lambdaCounter int
	// lambdas maps a variable name bound to a lambda to its generated
	// FuncDef name, so `f = lambda x: ...; f(3)` resolves in Call.
	lambdas map[string]string
	// lambdaCaptures records, for each generated anonymous function, the names its body reads from the
	// frame the lambda was written in — the parameters and locals of the enclosing body, or the names an
	// enclosing closure received through `%env`. Each one is a parameter this file added to the generated
	// `define` and a keyword argument the call road fills with the caller's own reading of that name
	// (roadmap L11.7, Gap R.265; the question is closureInfoFor's, ADR 0245's lift).
	lambdaCaptures map[string][]string
	// program is the tree this module is being lowered from — the program AFTER the defunctionalisation
	// pass, which is the tree every door reads — so a question about a parameter's callers is asked of the
	// calls that will actually be emitted (roadmap L11.7, Gap R.265).
	program *Program
	// lambdaOrigin maps each `define` this file minted for a lambda back to the lambda node it came from,
	// which is how an inline application `(lambda k: …)(2)` is recognised as a call of that frame.
	lambdaOrigin map[*FuncDef]*Lambda
	// lambdaCaptureVerdicts remembers, for each `define` minted for a lambda or a clone, what the frame
	// AROUND the application said about each name the body forwards as a capture parameter. A capture
	// parameter is not a position the source ever wrote an argument for, so the call-site scan
	// `captureParamWordOf` runs has no row to read for it — and a body applied INSIDE a body that is
	// itself forwarding a capture ((lambda x: (lambda y: (lambda z: x + y + z)(100))(10))(1)) asks that
	// scan about the middle frame's forwarded `x` and is refused for want of an argument no program could
	// write. The verdict the supplying frame gave at the moment the middle `define` was gated is the
	// answer, and this table is where it is kept (roadmap L11.7 · Gap R.330).
	lambdaCaptureVerdicts map[*FuncDef]map[string]captureWordAnswer
	// floatVars tracks variables whose last assignment produced a double.
	floatVars map[string]bool
	// unionVars holds names annotated with a union type (e.g. `int | float`);
	// their slots are tagged so the runtime member is tracked for print dispatch.
	unionVars map[string]bool
	unionDecl bool
	// floatTemps tracks temps that hold a double result (e.g. a float call).
	floatTemps map[string]bool
	// floatFuncs tracks user functions that return a double.
	floatFuncs map[string]bool
	// curFunc tracks the user function currently being emitted.
	curFunc string
	// curFD is the declaration of the frame currently being emitted — the `def`, the nested `def` the
	// closure lift lowered, or the method the class road emitted. `curFunc` is a name, and only the
	// `funcDef` road knows how to turn one back into a declaration; the roads that emit a body without
	// passing through `funcDef` carry the declaration here instead, which is what lets a lambda written
	// in a closure or a method body ask what that frame's calls hand to the parameter it captures
	// (roadmap L11.7, Gap R.276).
	curFD *FuncDef
	// emittingMethod says the frame being emitted is a method body. The class road emits every method as
	// an `i32` function whatever its body answers — it passes `floatRet=false` to the very road that
	// decides a body's word — so a `lambda` applied in a method body that answers a double has no word to
	// return in, and the module `llc` rejects is the alternative to saying so (roadmap L11.6, ADR 0370).
	emittingMethod bool
	listVars       map[string]bool
	runtimeDicts   map[string]bool
	runtimeSets    map[string]bool
	heapUsed       bool
	// echoOn records that this module was built for a caller that wants the value of a snippet
	// back (the REPL, `--eval`), and echoUsed that the module actually calls the writer — which
	// is what gates echoRuntimeIR. Roadmap L13.1, ADR 0302.
	echoOn    bool
	echoUsed  bool
	echoKinds map[string]string
	// curFnSrc is the source-level name of the function being lowered, for the
	// traceback frame; empty means module level, which renders as <module>.
	curFnSrc string
	// raiseUsed is set by any raise site (a `raise` statement, or a
	// compiler-generated one such as an out-of-bounds item assignment). It gates
	// rt_die, which reports an uncaught exception on stderr before main exits 1.
	raiseUsed bool
	// floatFmtUsed records that a float is rendered at run time, which pulls in
	// floatRuntimeIR (rt_fmt_double).
	floatFmtUsed bool
	// arithUsed records that this module does arithmetic on a slot whose kind only the run time can
	// describe, which is what pulls in the tagged arithmetic door (roadmap L11.1, ADR 0265).
	arithUsed bool
	// tupleUsed records that this module builds or prints an immutable container, which is what pulls
	// in the tuple printers (`pkg/lang/tuple_runtime.go`, roadmap L11.3, ADR 0361).
	tupleUsed bool
	// tupleVars is the binding-side half of the same fact: the names whose LATEST binding is a tuple
	// literal, so the print road can choose the bracketed printer for `t = (1, 2)` / `print(t)` without
	// guessing from elements. A binding to anything else retires it — the rule ADR 0172 set for a
	// value's tag word, asked of one more attribute of the binding (Gap R.142).
	tupleVars map[string]bool
	// foldUsed records that this module folds a value whose kind the run time chose — a `min`, `max` or
	// `sum` over a pair — which is what pulls in the fold door (roadmap L11.1, Gap R.146; ADR 0316). A
	// module that never asks the door carries no ordering sentence it never reaches.
	foldUsed bool
	// caseOps is the set of case operations this module asks of the runtime door, one bit per
	// caseUpper..caseSwapcase. It is the gate for the case block: a program that never upper-cases a
	// text the compiler cannot read carries neither the reference's tables nor its searches
	// (roadmap L11.5, ADR 0362).
	caseOps int
	// percentUsed records that this module formats a text with `%`, which pulls in the formatting door
	// (roadmap Gap R.31, ADR 0360). A program that never formats carries no format walker, no argument
	// table and no copy of the reference's eight sentences.
	percentUsed bool
	// seqUsed records that this module builds a sequence the run time assembled — a list or tuple
	// concatenated with `+`, a sequence repeated by `*`, or a text repeated by `*` — which is what pulls
	// in the sequence doors (`pkg/lang/codegen_ir_sequence.go`, roadmap Gap R.33, ADR 0365). A program
	// that never concatenates or repeats carries no copier it never reaches.
	seqUsed bool
	// seqRuntimeMultipliers counts the repeats whose multiplier this pass could not read as a constant —
	// a loop counter, a parameter, the answer of a call. The count is the negative control the IR rows
	// read: a repeat whose multiplier the compiler cannot kind is the shape that still cannot be asked
	// whether it is a whole number at run time, and the row that owns it is Gap R.314 (ADR 0365).
	seqRuntimeMultipliers int
	// percentChecks counts the `%`-formatting calls this module emits. It is the negative control the
	// IR rows read: a guard that emitted the door for a program that never formats one would move this
	// count and fail the row that pins it at zero (ADR 0337's discipline for a conditional guard).
	percentChecks int
	// numChains is the gate on the `+` and `*` half of that door: the literal kind tree of what a program
	// can store into each container's slots, level by level. `+` and `*` are answered by the reference
	// with a joined text or a repeated list when a slot holds one, and this backend can build neither
	// (Gap R.82), so those two operators take the door only where the tree proves the slots hold numbers.
	// `-` and the unary `-` answer a number or raise, always, and are not gated (ADR 0265).
	numChains *numericSlotChains
	// numCtx is the BinOp currently being lowered into the float arms, so the slot read inside it can
	// name the operator and span in the TypeError it raises. Set and restored by floatBinOp.
	numCtx *BinOp
	// floatUnlowerable records that a float arm reached an operand it could not lift, which the caller
	// turns into a front-end refusal instead of an instruction with an empty operand (Gap R.88).
	floatUnlowerable string
	// doubleDomain counts the emissions that asked for a double. The numeric door answers a slot read
	// of a container the program built with one, and only a caller that can hold a double may take it:
	// an int-domain sink — a call argument, an augmented assignment, str()'s argument, a container
	// element — that receives it emits `call i32 @gy_f(double %t29)`, which is the module llc rejects,
	// and ADR 0166 counts that as the compiler's bug for an ordinary program. Outside the double domain
	// the door therefore refuses by naming itself (roadmap Gap R.88, measured beside Gap R.96).
	doubleDomain int
	heapSeq      int
	handlerStack []string
	// handledArms counts the `except` arms whose body is currently being lowered. Inside one,
	// the exception in flight has been *accepted by the program*, so a control transfer out of
	// the arm (`return`, `break`, `continue`) has to leave the pending-exception flag cleared
	// like the arm's normal exit does — otherwise the transfer escapes the clear and the next
	// user-function call reports the exception all over again (roadmap Gap R.21, compiled half).
	handledArms int
	// strAttrs records the instance attributes a class assigns a string to (`self.w = "hi"`),
	// keyed "Class.attr", so a read of one is known to be an @str_tab index and prints as text
	// instead of as the number (roadmap Gap R.42, ADR 0224).
	strAttrs map[string]bool
	// emitErr carries the first codegen refusal raised inside a body emitted outside GenerateIR's
	// statement walk -- a class method, whose emitter writes into the globals buffer and has no
	// error return. Dropping it built an incomplete function and let `llc` report the problem as
	// a toolchain rejection, blaming the compiler for a source error (ADR 0166, roadmap Gap R.41).
	emitErr error
	// deferred is the stack of `finally` bodies belonging to the `try` statements currently
	// being lowered, outermost first. A control transfer that leaves a `try` for good -- a
	// `return`, `break`, `continue`, a raise that its arms do not catch -- has to run them,
	// innermost first, before it goes (roadmap Gap R.23, ADR 0222). The straight-line path
	// does not consult this stack: `tryStmt` emits its own deferred body once, directly.
	deferred [][]Stmt
	// emittingDeferred is set while a deferred body is being lowered, so that a raise from
	// inside one still walks the remaining (outer) deferred bodies instead of jumping out
	// and skipping them.
	emittingDeferred bool
	funcRaiseExit    string

	classInfos map[string]*classInfo // class name -> info
	classIDs   map[string]int        // class name -> runtime dispatch id
	classOrder []string              // classes in id order (dispatch switch)
	varClasses map[string]string     // local var -> class name
	// globalDecls is the set a body declared with `global a, b` (roadmap Gap R.48, ADR 0448): the names it
	// may write even though they are not its frame's. A declared name is deleted from `funcLocals` beside
	// being recorded here — the read road in `value()` takes `@gy_mod_<name>` for a name the body does not
	// own, and the write road asks this table before it stores to a frame slot.
	globalDecls map[string]bool
	// globalNames is the program-wide view of the same table (roadmap Gap R.48, ADR 0448): the names ANY
	// body declares. A name in it is module state a body writes, so the per-name records that answer
	// "what was last bound here" — which only ever see the module's own statements — must not claim it:
	// the read goes to the slot the write actually touched instead.
	globalNames map[string]bool
	// classLinCache / classLinVisits back the method-resolution walk an attribute read takes
	// (`classLinearization`, roadmap L12.4): the order is a property of the class table, so it is computed once
	// per class and reused by every read, and the visit set is what stops a cyclic base list from looping.
	classLinCache  map[string][]string
	classLinVisits map[string]bool
	selfClass      string         // enclosing class of current self
	attrSlots      map[string]int // attr name -> instance data slot
	// classPat is the front end's answer to what a class pattern needs to know — which class a
	// pattern name denotes (through `Alias = Point`), and which attribute names the program can
	// ever write. It is computed before emission so the presence-row clear length is final (ADR 0235).
	classPat *classPatternInfo
	nextSlot int

	// genFuncs records generator function names; calling one yields a runtime
	// heap list handle (mirroring the record's eager yield semantics).
	genFuncs map[string]bool
	// genMethodParamWalk records, per generator-METHOD symbol, the name of a parameter whose container the
	// body walks with a `for` and which the loop road cannot classify — the one generator-method shape this
	// backend REFUSES instead of answering with the empty buffer (roadmap Gap R.475, ADR 0432). Recorded by
	// `registerClass` beside the `genFuncs` mark, so the reading is taken once, at the class's door.
	genMethodParamWalk map[string]string
	// staticLists maps a compile-time list global name (@.lstN) to its literal,
	// so a call site can copy it into the runtime heap when the callee expects a
	// container handle.
	staticLists map[string]*ListLit
	// staticSets and staticDicts are the same record for the two container kinds a
	// comprehension can fold to: `sa = {x for x in [3, 1, 2]}` emits @.setN, whose layout is
	// a length plus an array and therefore not a value, so the binding site materialises the
	// literal these maps hold instead of storing the global (roadmap Gap J.2, ADR 0234).
	staticSets  map[string]*SetLit
	staticDicts map[string]*DictLit
	// containerLits records the container literal a name is bound to exactly once at module
	// level and never mutated afterwards. It is what licenses reading a slot as a container: the
	// builder wrote a tag for that slot from that literal, and as long as nothing has changed the
	// object, the tag the compiler remembers is the tag the object holds. A second binding, an item
	// assignment or a mutating method takes the name out of this map, and the read is refused
	// rather than trusted (roadmap L11.1, ADR 0241).
	containerLits map[string]Expr
	// heapArgs records, per function name, which parameter positions receive a
	// runtime heap container handle (see heapargs.go). Inferred once, before any
	// IR is emitted.
	heapArgs map[string]map[int]int
	// freshSlots records variables whose heap slot was created by the statement
	// currently being lowered, so the "release the previous binding" step is
	// skipped for them (see emitFreeOld).
	freshSlots map[string]bool
	// i1Vals records SSA registers whose LLVM type is i1 — comparison results and
	// boolean-logic results. Truthiness tests need it: feeding an i1 to
	// `icmp ne i32 …, 0` (or an i32 to `br i1`) is the classic way a module stops
	// verifying, and the old code assumed every condition operand was an i32.
	i1Vals map[string]bool
	// listOperands records IR operands known to be runtime heap list handles
	// (generator function results and generator expression results), so
	// print/indexing can treat them as lists.
	listOperands map[string]bool
	// genBuffers is the narrower claim beside it: the IR register (the `%t7` a call left the answer
	// in) or the source name (the `ys` a binding parked it under) that holds a GENERATOR's buffer —
	// the heap list a `yield` filled. Every other list `listOperands` records is written by a road
	// that decides the element's kind at compile time; a generator's slots are written by the
	// `yield` itself, one element at a time, so the door that walks or renders the buffer has to ask
	// the SLOT what it holds rather than print the word underneath the value. That question is the
	// tag, and the tag is written by the same door that appends (ADR 0187, ADR 0311; roadmap
	// **Gap R.468**, **ADR 0433**).
	genBuffers map[string]bool
	// genBufferSlots is the earned half of that claim, kept apart on purpose: the operands whose
	// slots must actually be asked, because the callee's body yields something whose kind the
	// untagged road cannot render. A generator that yields nothing but integers is NOT in this table
	// — its walk stays a sum rather than a (payload, tag) pair, and the pair road's standing refusal
	// (Gap R.146) never fires on a program that was adding numbers up.
	genBufferSlots map[string]bool
	// genBodies is where that verdict is taken from: the recorded body of every generator this program
	// declares, keyed by the same symbol `genFuncs` uses. The claim is READ off the body at the call site
	// rather than written at the registration door, because a body's claim can depend on the body it copies
	// with `yield from`, and the registration order — module `def`s in source order, a class's methods in
	// class order — would otherwise make the answer depend on which body the walk reached first (roadmap
	// **Gap R.468**, **ADR 0433**). `bodyWritesKinds` is the question; `genBufferSlots` caches nothing and
	// asserts nothing, it is the operand-side answer.
	genBodies map[string][]Stmt
	// genHandle is the runtime list handle for the generator function whose
	// body is currently being emitted; stmt() appends each `yield` to it.
	genHandle  string
	genIdx     int
	genExprIdx int
	inMain     bool
	gcRootIdx  int
	gcCallIdx  int
	gcRootSeen map[string]bool
	// frameOpen is set while a function body's root frame is open (ADR 0181): the
	// prologue recorded the root-stack base, so every return and unwind path must pop
	// back to it. gcRootSeen is now only per-scope bookkeeping — dedup of root entries
	// moved into the runtime, where it can be per *frame* rather than per body.
	frameOpen bool
	// rooted says the module touches the root stack at all, which gates shipping the
	// root runtime: a program of pure scalars ships no collector scaffolding.
	rooted bool
	// envSlots holds the module-global closure env slot names; each holds an
	// env heap handle and must be rooted so GC keeps captured envs alive.
	envSlots []string

	// curFnOverride, when non-empty, overrides the emitted name of the
	// current FuncDef (used for AOT module-function emission).
	curFnOverride string
	// curModName is the module whose function is currently being emitted; bare
	// Name calls inside it dispatch to sibling module functions.
	curModName string
	// moduleConsts holds the *program's own* module-level names whose value is a literal and which
	// the module never rebinds. A compiled function body may read those as values, because a value
	// that cannot change needs no slot to read it from (ADR 0227, roadmap Gap R.35's compiled half).
	moduleConsts map[string]Expr
	// moduleBinds is every value the program's own top-level statements bound to each name, and
	// strArgBindCache memoises the merged module+current-body answer the run-time digits road asks
	// (roadmap L11.2, Gap R.170/R.171).
	moduleBinds     map[string][]Expr
	strArgBindCache map[string][]Expr
	// strArgInFlight holds the names whose binding is currently being asked about, so a name whose
	// own binding mentions it (`s = s + x`) is refused instead of recursed into. See strArgIsNumberish.
	strArgInFlight map[string]bool
	// unwritten is the checker's answer to "which names may this body read before assigning them",
	// keyed by the FuncDef whose body the read sits in (nil key: the module's own statements). It is
	// the same walk the checker runs and already warns about, asked a second question (ADR 0228) --
	// codegen does not get its own copy of the rule.
	unwritten map[*FuncDef]map[string]bool
	// fdAlias points an emitted clone (a decorator's `_impl`) at the FuncDef the checker saw, so the
	// clone inherits its unwritten-read set.
	fdAlias map[*FuncDef]*FuncDef
	// boundFlags is the entry from `unwritten` for the body being emitted, after eligibility filtering;
	// boundSlots names the i8 flag alloca for each of those names, and boundOrder lists them in a
	// deterministic order so the entry block does not depend on map iteration. A name outside this set pays nothing:
	// the flag exists only where the checker could not prove the write happens.
	boundFlags map[string]bool
	boundSlots map[string]string
	boundOrder []string
	// matchBinds holds the captures the pattern being lowered computed a value for, waiting for the
	// block the branch puts them in. A pattern is evaluated to decide WHICH arm runs, so until the
	// branch has run nothing here is bound (roadmap Gap R.68, ADR 0358).
	matchBinds []pendingMatchBind
	// decoratorNames holds the names used as decorators anywhere in the program (`@add1` -> add1).
	// While emitting such a function, a nested closure body that cannot be lowered is reported in the
	// module comment instead of refusing: the decorated call goes through the trampoline, whose own
	// body *is* compiled with full error propagation, so the closure object here is unreachable and a
	// refusal would break a program that works (ADR 0227). The failure is never hidden -- it is said
	// in the module, where --emit-llvm and the IR verifier both can see it.
	decoratorNames map[string]bool
	// emittingDecorator is set while funcDef emits one of those functions.
	emittingDecorator bool
	// closureBodyNotes collects the deferred body failures named in the module comment.
	closureBodyNotes []string
	// funcLocals is the set of names the function body currently being emitted binds anywhere in
	// itself. It is the language's rule, not a slot-timing question: a binding inside a body makes
	// the name local to that body even when the module binds it too (ADR 0220), so `K = 1; return K`
	// in a function answers 1 while the module's K stays 5.
	funcLocals map[string]bool
	// moduleSlots holds the module names a compiled body reads that the module also *rebinds*: they
	// cannot be folded, so they live in module globals (`@gy_mod_LATE`) that main writes and any body
	// loads. A frame's allocas die with the frame; module state does not, which is exactly what a call
	// that looks a name up "when it runs" requires (ADR 0220's rule, implemented for the compiled
	// backend by ADR 0227). Containers stay refused -- the element ops are the missing machinery.
	moduleSlots map[string]string
	// moduleFloatSlots is the same door for the DOUBLE word: the module names that some body reads
	// and that every module binding of the module's fills with a float, each stored in its own
	// `global double`. Containers are still not here at all — a container's word is a heap handle that
	// has to be rooted, which is Gap R.35's other half (`module_float.go`, roadmap Gap R.35).
	moduleFloatSlots map[string]string
	// moduleContainers is the third module table, for the CONTAINER word: the module names that some
	// body reads without binding them, whose one module binding is a plain assignment of a literal
	// container the program never writes through. Such a name is handed to the reading frame as a
	// binding — the same `stmtAssign` road the module's own `xs = [1, 2]` goes through — so the frame
	// owns the slot, the root and every notebook the container doors read by name
	// (`module_container.go`, roadmap Gap R.35's container half).
	moduleContainers map[string]containerAnswer
	// moduleContainerReads records the names THIS frame was handed a copy of, so a diagnostic, an IR
	// row and a test can ask the emitter what it bound without reaching into the slot tables.
	moduleContainerReads map[string]bool
	// moduleSlotDecls is the IR declaring those globals.
	moduleSlotDecls string
	// moduleNames is every name the module binds, constant or not. A read of one that is not a
	// constant is refused with the reason that names the missing machinery, instead of the typo
	// message that claims the record reports the same error -- it does not (Gap R.38).
	moduleNames map[string]bool
	// curModGlobals holds folded module-global constants for the module
	// function currently being emitted; bare Name refs resolve against it.
	curModGlobals map[string]Expr
	// curModParams holds the param set of the module function being emitted,
	// so a param that shadows a module global is not substituted.
	curModParams map[string]bool
	// dbgOn asks for DWARF line records; dbgMarks and dbgFuncs are where each statement's
	// and each function definition's IR was written, which the attach pass turns into !dbg
	// metadata (L8.5, ADR 0231). Offsets are builder-relative: a mark says whether the write
	// went to `globals` or to the body builder.
	dbgOn    bool
	dbgMarks []dbgMark
	dbgFuncs []dbgFunc
}

func (g *irGen) newTmp() string           { g.tmp++; return fmt.Sprintf("%%t%d", g.tmp) }
func (g *irGen) newLabel(s string) string { g.label++; return fmt.Sprintf("%s%d", s, g.label) }

// fmtStr emits a global string constant for a printf format; returns name and size.
func (g *irGen) fmtStr(format string) (string, int) {
	g.fmtIdx++
	name := fmt.Sprintf("@.fmt%d", g.fmtIdx)
	// escape backslashes for the IR c"..." literal; % is literal in IR and
	// must stay single so printf sees a real format directive (e.g. %d -> 42).
	f := strings.ReplaceAll(format, "\\", "\\\\")
	f = strings.ReplaceAll(f, "\n", "\\0A")
	// The array size must be the decoded byte count of the emitted constant:
	// each IR \\0A escape decodes to a single newline byte, so the raw
	// (unescaped) format length plus one trailing null is correct.
	g.strGlobals.WriteString(fmt.Sprintf("%s = private unnamed_addr constant [%d x i8] c\"%s\\00\"\n", name, len(format)+1, f))
	return name, len(format) + 1
}

// enterBody records what a function, method or closure body binds anywhere inside itself, so the
// module-read path can honour the scoping rule rather than the order slots happen to be allocated in.
func (g *irGen) enterBody(body []Stmt) func() {
	saved := g.funcLocals
	g.funcLocals = map[string]bool{}
	collectLocals(body, g.funcLocals)
	// A name the body DECLARES with `global` is not its frame's: it is deleted from the table that answers
	// "does this body own this name", which is what sends both roads — the read in `value()` and the store in
	// `storeModuleScalar` — to the module's `@gy_mod_<name>` without a new arm in either (roadmap Gap R.48,
	// ADR 0448). The table is restored with the body's exit, exactly as `funcLocals` is.
	savedGlobals := g.globalDecls
	g.globalDecls = globalDeclsIn(body)
	if g.globalDecls != nil {
		for nm := range g.globalDecls {
			delete(g.funcLocals, nm)
		}
	}
	// A body that begins its locals also begins its FLOAT table. The two roads that call this helper — the
	// closure lift's nested `def` and the class road's method — do not call `beginScope`, and left the float
	// table out of both helpers, so a method's float `self`/parameter and a lifted closure's local leaked
	// into whatever body was lowered next (roadmap L11.6, Gap R.339, **ADR 0375**; `float_frame.go` is the rule).
	savedFloats := g.enterBodyFloatTable()
	// …and the table of names the frame binds for itself, for the same reason: the comprehension a body
	// hosts is the body's own binding, and what the host frame bound reaches it as a parameter and not as
	// a slot (roadmap L11.7 · Gap R.348).
	savedTargets := g.beginFrameTargets()
	// …and it begins the SLOT table too, for the same reason and by the same argument. `allocd` answers
	// 「does this name already have a stack slot in the frame being emitted」, and a frame is exactly as
	// wide as one body: the closure lift asked it while emitting a nested `def`, found the ENCLOSING body's
	// answer, skipped the `alloca` for the closure's own `w`, and the enclosing body — whose slot the answer
	// had been — then wrote through `%_w` in a `define` that had never allocated it. `llc` refused the module
	// and an ordinary three-line program left at exit 2, ADR 0166's code for a compiler bug (roadmap
	// L11.6 · **Gap R.342**, ADR 0378; `frame_slots.go` is the rule, and `auditFrameSlots` is its guard).
	savedSlots := g.enterFrameSlots()
	return func() {
		g.funcLocals = saved
		g.globalDecls = savedGlobals
		savedFloats()
		savedTargets()
		savedSlots()
	}
}
func (g *irGen) noteUnlowered(where any, err error) {
	if g.emitErr != nil {
		return
	}
	// A statement whose emission already refused carries its own sentence: the door that declined named
	// the construct, the missing half and the row that owns it, all in the program's words. Wrapping that
	// in `a *lang.ExprStmt statement cannot be compiled:` told the reader the emitter's type table and
	// buried the half they were given to act on — the reason two tests of the sibling row had to strip a
	// prefix before they could call the sentence clean. The payload is now handed over as written, with
	// the frame and the line appended, which is the only thing this arm knows that the payload does not
	// (roadmap L11.7 · **Gap R.377**, ADR 0398; ADR 0166/0249's "never the compiler's own machinery").
	//
	// A `*FuncDef` is NOT taken here even though it implements `Stmt`: its arm is Gap R.38's, it names the
	// function the program wrote (which the payload does not), and the drift ledger quotes that sentence.
	if _, isFuncDef := where.(*FuncDef); !isFuncDef {
		if st, ok := where.(Stmt); ok && refusalIsSentence(err) {
			// The prefix is added exactly once: a road that wrote its sentence with the `codegen: ` mark (the
			// 142 sites ADR 0346 counts) must not read as doubled when the wrapper step is taken away.
			g.emitErr = fmt.Errorf("codegen: %s%s", strings.TrimPrefix(err.Error(), "codegen: "), g.atLocation(st))
			return
		}
	}
	what := "an expression"
	switch n := where.(type) {
	case Expr:
		if t := exprTyName(n); t != "" {
			what = t
		}
	case *FuncDef:
		// A definition is not a statement a program wrote in a body: naming it by its Go type read as
		// "a *lang.FuncDef statement cannot be compiled", which told the reader nothing about which
		// function to fix (roadmap Gap R.38's rule — a refusal names the program's own thing).
		what = fmt.Sprintf("function %q", n.Name)
	case Stmt:
		// The payload here is a raw Go error rather than a refusal sentence, so the wrapper IS the
		// sentence — which is exactly why it may not contain a Go type name. It names the construct the
		// program wrote and the line it wrote it on (roadmap L11.7 · Gap R.377, ADR 0398).
		what = stmtSurface(n)
	}
	g.emitErr = fmt.Errorf("codegen: %s cannot be compiled: %v", what, err)
}

// atLocation says where in the program a node sits, in the two words a reader acts on: the frame's name
// (the display name, so a clone the pass minted is never printed — ADR 0345 §3) and the line. An empty
// answer means there is nothing to add: a statement with no span is no help, and a wrong line is worse
// than none.
func (g *irGen) atLocation(st Stmt) string {
	if st == nil || reflectIsNil(st) {
		return ""
	}
	sp := st.Span()
	if sp.Line <= 0 {
		return ""
	}
	frame := ""
	if g != nil && g.curFD != nil {
		frame = callableDisplayName(g.curFD)
	}
	if frame != "" {
		return fmt.Sprintf(" (in `%s`, line %d)", frame, sp.Line)
	}
	return fmt.Sprintf(" (line %d)", sp.Line)
}

// beginScope starts a fresh variable-binding scope — a function body, or the
// module's top-level code — and returns the closure that restores the previous one.
//
// Which names are containers, which slots have been allocated, and which are GC
// roots are facts about a single scope. Sharing them across scopes makes code in one
// scope act on registers belonging to another: a dict `d` inside a function made
// module-level code free `%_d` before main had allocated it, producing a module LLVM
// rejects.
func (g *irGen) beginScope() func() {
	savedList, savedDict, savedSet := g.listVars, g.runtimeDicts, g.runtimeSets
	savedNone := g.noneVars
	savedBool := g.boolVars
	// The float table joins its siblings. `floatVars` answers 「whose last assignment in THIS body
	// produced a double」, and it is read by `isFloat`'s name arm, by the print door, by the comparison
	// road, by the verdict census and by the heap-argument road — every one of them asking about a name in
	// the body being lowered. It was the one per-frame kind table this helper did not install, which made
	// the answer to a question about THIS body's `u` come from whatever the previously-emitted body last
	// wrote to a name of the same spelling (roadmap L11.6, Gap R.339, **ADR 0375**; `float_frame.go` is the rule, and it
	// is why the undo is one call rather than a second opinion).
	savedFloats := g.enterBodyFloatTable()
	savedLStr, savedSStr, savedKStr, savedVStr, savedInt := g.listElemStr, g.setElemStr, g.dictKeyStr, g.dictValStr, g.internedVars
	savedLNum, savedSNum, savedKNum, savedVNum := g.listElemInt, g.setElemInt, g.dictKeyInt, g.dictValInt
	savedMixDict, savedMixSet := g.mixedDicts, g.mixedSets
	savedDigit := g.digitLoopVars
	// …and so does the table of names the frame binds for ITSELF. A comprehension's target belongs to the
	// frame that hosts the comprehension, and a body emitted inside one owns only the targets written
	// inside it (roadmap L11.7 · Gap R.348; `frame_targets.go` is the rule).
	savedTargets := g.beginFrameTargets()
	// The slot tables come from the one helper every body-emitting road now takes — the `def` road
	// through here, the method and closure-lift roads through `enterBody` — so there is no second copy of
	// 「which slots does this frame own」 to forget (roadmap L11.6 · Gap R.342, ADR 0378).
	savedSlots := g.enterFrameSlots()
	g.listVars = map[string]bool{}
	g.runtimeDicts = map[string]bool{}
	g.runtimeSets = map[string]bool{}
	g.noneVars = map[string]bool{}
	g.boolVars = map[string]bool{}
	g.listElemStr = map[string]bool{}
	g.setElemStr = map[string]bool{}
	g.dictKeyStr = map[string]bool{}
	g.dictValStr = map[string]bool{}
	g.listElemInt = map[string]bool{}
	g.setElemInt = map[string]bool{}
	g.dictKeyInt = map[string]bool{}
	g.dictValInt = map[string]bool{}
	g.mixedDicts = map[string]bool{}
	g.mixedSets = map[string]bool{}
	g.internedVars = map[string]bool{}
	g.digitLoopVars = map[string]bool{}
	return func() {
		g.listVars, g.runtimeDicts, g.runtimeSets = savedList, savedDict, savedSet
		savedSlots()
		g.noneVars = savedNone
		g.boolVars = savedBool
		g.listElemStr, g.setElemStr = savedLStr, savedSStr
		g.dictKeyStr, g.dictValStr, g.internedVars = savedKStr, savedVStr, savedInt
		g.listElemInt, g.setElemInt = savedLNum, savedSNum
		g.dictKeyInt, g.dictValInt = savedKNum, savedVNum
		g.mixedDicts, g.mixedSets = savedMixDict, savedMixSet
		g.digitLoopVars = savedDigit
		savedTargets()
		savedFloats()
	}
}

// dbgTernaryPred is the debug switch for the ternary-text investigation.
const dbgTernaryPred = true
