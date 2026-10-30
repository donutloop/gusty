// Classes: registration, the method table, an attribute slot's address, and the static questions the roads
// ask about a method's answer (does it return text, does it return a float).
package lang

import (
	"fmt"
	"strings"
)

// registerClass records a class definition (ClassDef) and emits its methods.
type classInfo struct {
	name    string
	bases   []string
	methods map[string]string // method name -> LLVM function name
	// methodDefs is the same table seen as DECLARATIONS rather than symbols: the arity a call site has to
	// fill, the parameter names a `name=` keyword binds against and the default expression a written-out
	// argument left empty all live on the `*FuncDef`, and only the walk that mints the symbol ever holds it
	// (roadmap Gap R.472, ADR 0434). Filled by `registerClass`, read by `resolveMethodDef`.
	methodDefs map[string]*FuncDef
	doc        string
	// attrs is the class's OWN members: the names the class body binds (`class C:` / `first = xs[0]`),
	// which are attributes of the CLASS and are read through the class name (`C.first`) — a different
	// road from the instance attributes below, which live in the object's slot table and are read through
	// an instance (roadmap Gap R.297, ADR 0418). The expression is kept rather than a value because a
	// class body runs once when the class is made, and the read may stand anywhere after it.
	attrs map[string]Expr
	// owedAttrs is the same table for the bindings this road must not answer yet: a float member prints
	// 1.0 where the reference prints 1.5, and a verdict or a void prints 1 and 0 where the reference prints
	// True and None — the answer class this contract ranks worst, so the read refuses instead and names
	// roadmap Gap R.431, which owns the tagged-pair read that would answer them (ADR 0418).
	owedAttrs map[string]Expr
}

func (g *irGen) registerClass(cd *ClassDef) {
	if g.classInfos == nil {
		g.classInfos = map[string]*classInfo{}
	}
	// Idempotent: the class-registration pre-pass runs before main-stmt
	// processing, so a later duplicate call must not re-emit method bodies
	// (that would redefine the same LLVM functions).
	if _, ok := g.classInfos[cd.Name]; ok {
		return
	}
	if _, ok := g.classIDs[cd.Name]; !ok {
		g.classIDs[cd.Name] = len(g.classOrder)
		g.classOrder = append(g.classOrder, cd.Name)
	}
	ci := &classInfo{bases: []string{}, methods: map[string]string{}, methodDefs: map[string]*FuncDef{}, doc: cd.Doc}
	for _, b := range cd.Bases {
		if b != nil {
			ci.bases = append(ci.bases, b.Value)
		}
	}
	g.classInfos[cd.Name] = ci
	// The class body's own bindings are the class's members. The reference builds a class by executing
	// its body, so `first = xs[0]` inside `class C:` is a name the class holds and `C.first` reads; this
	// backend walked the body only for methods and dropped the rest, which is why `print(C.first)` stopped
	// the build with `unsupported attr expression` where CPython prints 1 (roadmap Gap R.297, ADR 0418).
	for _, st := range cd.Body {
		as, ok := st.(*AssignStmt)
		if !ok {
			continue
		}
		nm, ok := as.Target.(*Name)
		if !ok || nm == nil {
			continue
		}
		if ci.attrs == nil {
			ci.attrs = map[string]Expr{}
		}
		if classAttrValueStable(as.Value) {
			ci.attrs[nm.Value] = as.Value
			continue
		}
		if classAttrValueRecorded(as.Value) {
			if ci.owedAttrs == nil {
				ci.owedAttrs = map[string]Expr{}
			}
			ci.owedAttrs[nm.Value] = as.Value
		}
	}
	for _, st := range cd.Body {
		fd, ok := st.(*FuncDef)
		if !ok {
			continue
		}
		mname := fd.Name
		// A method symbol is minted once, stored in the class index, and reused by every
		// call through resolveMethod — so the prefix applied here reaches definition and
		// call sites together.
		funcName := irSymbol(fmt.Sprintf("%s_%s", cd.Name, mname))
		ci.methods[mname] = funcName
		// The declaration travels beside the symbol for the same reason the symbol is minted once: a call
		// site that has to fill the callee's parameter vector asks the walk that minted it, and the arity,
		// the names and the defaults exist only on the `*FuncDef` (roadmap Gap R.472, ADR 0434).
		ci.methodDefs[mname] = fd
		g.markStringAttrs(cd.Name, fd)
		// The generator reading is taken here, at the class's door, and not in the module walk that the
		// `yield` statement consults: a `def` written inside a `ClassDef` body is a method by POSITION, and
		// the whole-program statement walk that decides "is this body a generator" never steps into a class,
		// so the answer it gave was "no" and the body's own `yield` died with `yield outside a generator
		// function` — a sentence that blamed the program for the position its body was written in (roadmap
		// **Gap R.455**, **ADR 0432**). The flag is recorded under the MANGLED symbol, which is what
		// `resolveMethod` hands a call site, for the same reason `strFuncs` is (Gap R.42, ADR 0224).
		if methodIsGenerator(fd) {
			g.genFuncs[funcName] = true
			g.registerGenBuffer(funcName, fd.Body)
			if param := generatorMethodParameterWalk(fd); param != "" {
				if g.genMethodParamWalk == nil {
					g.genMethodParamWalk = map[string]string{}
				}
				g.genMethodParamWalk[funcName] = param
			}
		}
		if !g.answerOwnsMethod(funcName) && (g.methodReturnsStr(fd) || g.strMethodFuncs[fd.Name]) {
			// Registered under the mangled symbol, which is what a call site resolves to
			// (Gap R.42, ADR 0224). The shape question sees a literal or a formatting; the census half sees
			// `return s` where the call sites are what made `s` a text, and `return s.upper()`, which is the
			// same rendering door a function body emits (roadmap Gap R.315, ADR 0366).
			g.strFuncs[funcName] = true
		}
		g.collectAttrs(fd.Body)
		g.emitClassMethod(cd.Name, funcName, fd)
	}
}

// methodIsGenerator is the ONE reading of "does this method body yield", asked with the same question
// `funcDef` asks of a module `def` (`containsYield`, roadmap **Gap R.455**) so a body cannot be a generator
// at its own `yield` and a plain procedure at its caller's door. An `async def` is not included: the checker
// (`effects.go`) stops an async generator before codegen, on both doors.
func methodIsGenerator(fd *FuncDef) bool {
	return fd != nil && !fd.Async && containsYield(fd.Body)
}

// generatorMethodParameterWalk names the ONE generator body this row REFUSES rather than answers: a `for`
// whose iterable is one of the method's OWN parameters. The loop road classifies a Name iterable out of the
// tables the module walk fills (`listVars`, `mixedLists`, the runtime container sets) and a method parameter
// is in none of them — the keyed-call machinery that hands a module `def` its parameter's kind
// (`call_key.go`) is not reached by a method body. So the walk runs zero iterations and the caller of such a
// body gets the EMPTY buffer at the exit code of success where the reference walks the list: `class C():` /
// `    def m(self, xs):` / `        for v in xs:` / `            yield v` / `xs = [1, 2]` /
// `print(C().m(xs))` answered `[]` for `[1, 2]`. The module twin (`def m(xs):` with the same four lines)
// answers `[1, 2]`, which is what makes the shape a method-door hole rather than a loop-door one, and the
// row that owns it is **Gap R.475**; the identical body with no `yield` in it prints `1` for the reference's
// `1 2 1` on this row's PARENT, byte for byte, which is what makes it a hole this row surfaced rather than
// dug. A row's parent refused the program outright (`yield outside a generator function`), so the choice in
// front of this commit was the refusal in words or a wrong answer, and the language's rule is the refusal.
func generatorMethodParameterWalk(fd *FuncDef) string {
	if fd == nil {
		return ""
	}
	params := map[string]bool{}
	for i, p := range fd.Params {
		// Slot 0 is `self`, which is never a container the body walks.
		if i > 0 && p != nil && p.Name != "" {
			params[p.Name] = true
		}
	}
	if len(params) == 0 {
		return ""
	}
	// A parameter the body itself fills with a container literal is one the loop road CAN classify, because
	// the assignment records the kind — such a body is not the shape this refuses.
	claimed := map[string]bool{}
	var scanClaims func([]Stmt)
	scanClaims = func(ss []Stmt) {
		for _, st := range ss {
			switch s := st.(type) {
			case *AssignStmt:
				if nm, ok := s.Target.(*Name); ok && containerLiteralValue(s.Value) {
					claimed[nm.Value] = true
				}
			case *IfStmt:
				scanClaims(s.Then)
				for _, e := range s.Elifs {
					if e != nil {
						scanClaims(e.Then)
					}
				}
				scanClaims(s.Else)
			case *WhileStmt:
				scanClaims(s.Body)
				scanClaims(s.Else)
			case *ForStmt:
				scanClaims(s.Body)
				scanClaims(s.Else)
			case *TryStmt:
				scanClaims(s.Body)
				for _, e := range s.Excepts {
					if e != nil {
						scanClaims(e.Body)
					}
				}
				scanClaims(s.Finally)
			case *WithStmt:
				scanClaims(s.Body)
			case *MatchStmt:
				for _, c := range s.Cases {
					if c != nil {
						scanClaims(c.Body)
					}
				}
			}
		}
	}
	scanClaims(fd.Body)

	var find func([]Stmt) string
	find = func(ss []Stmt) string {
		for _, st := range ss {
			switch s := st.(type) {
			case *ForStmt:
				if nm, ok := s.Iter.(*Name); ok && params[nm.Value] && !claimed[nm.Value] {
					return nm.Value
				}
				if at := find(s.Body); at != "" {
					return at
				}
				if at := find(s.Else); at != "" {
					return at
				}
			case *IfStmt:
				if at := find(s.Then); at != "" {
					return at
				}
				for _, e := range s.Elifs {
					if e != nil {
						if at := find(e.Then); at != "" {
							return at
						}
					}
				}
				if at := find(s.Else); at != "" {
					return at
				}
			case *WhileStmt:
				if at := find(s.Body); at != "" {
					return at
				}
				if at := find(s.Else); at != "" {
					return at
				}
			case *TryStmt:
				if at := find(s.Body); at != "" {
					return at
				}
				for _, e := range s.Excepts {
					if e != nil {
						if at := find(e.Body); at != "" {
							return at
						}
					}
				}
				if at := find(s.Finally); at != "" {
					return at
				}
			case *WithStmt:
				if at := find(s.Body); at != "" {
					return at
				}
			case *MatchStmt:
				for _, c := range s.Cases {
					if c != nil {
						if at := find(c.Body); at != "" {
							return at
						}
					}
				}
			case *FuncDef:
				// A nested `def` is its own frame with its own parameters: its loops are not this body's.
			}
		}
		return ""
	}
	return find(fd.Body)
}

// containerLiteralValue is a value whose kind the assignment road records: a list, dict or set the program
// wrote where a name would stand.
func containerLiteralValue(e Expr) bool {
	switch e.(type) {
	case *ListLit, *DictLit, *SetLit:
		return true
	}
	return false
}

// callIsGeneratorCall answers, for ANY call the program can write, the question every position that receives
// a call's answer has to ask: does this call hand back a generator's buffer rather than a value? The `def`
// road asked it of a plain name and the answer was registered under the source name; a method call is spelled
// `recv.m(...)`, its symbol is minted by `registerClass` and handed out by `resolveMethod`, so the same
// question needs the method door too — otherwise the position falls back to treating the handle as a number
// and a `for` over a generator method walks a counter (roadmap **Gap R.455**, **ADR 0432**).
func (g *irGen) callIsGeneratorCall(c *Call) bool {
	if c == nil {
		return false
	}
	switch fn := c.Fn.(type) {
	case *Name:
		return g.genFuncs[fn.Value]
	case *Attr:
		if fn.Name == nil {
			return false
		}
		if cls := g.receiverClass(fn.Obj); cls != "" {
			if sym, ok := g.resolveMethod(cls, fn.Name.Value); ok {
				return g.genFuncs[sym]
			}
			return false
		}
		// The receiver's class is a run-time fact, so the arm that runs is not written in the line: the
		// register the dispatch joins is a buffer only where EVERY class that declares the name yields.
		// The split census is refused by the dispatch road itself, not answered here (see
		// `methodCallYieldIsSplit`).
		return g.methodCallCasesAreGenerator(fn.Name.Value)
	}
	return false
}

// refuseGeneratorMethodParameterWalk turns the reading `generatorMethodParameterWalk` took at the class's
// door into the program's refusal, at the call site and before a single instruction of the call is emitted.
// The alternative was the empty buffer at the exit code of success — `print(C().m(xs))` answering `[]` for
// the reference's `[1, 2]` — which is the artifact **ADR 0166** forbids and the reason this row refuses
// (roadmap **Gap R.475**, **ADR 0432**).
func (g *irGen) refuseGeneratorMethodParameterWalk(sym, mname string) error {
	param, ok := g.genMethodParamWalk[sym]
	if !ok {
		return nil
	}
	return fmt.Errorf("codegen: %q is a generator whose body walks its own parameter %q, and a method body has no word for what that parameter carries: the loop would run zero times and the call would answer the empty buffer where the reference walks the list — the same body in a module `def` answers it, because the keyed-call pass records a `def`'s parameter kind and never reaches a method body (roadmap Gap R.475, ADR 0432; ADR 0166 why nothing is answered rather than guessed)", mname, param)
}

// methodCallCensus counts the classes that declare a method name, and how many of them yield.
func (g *irGen) methodCallCensus(mname string) (declared, yielding int) {
	for _, cls := range g.classOrder {
		if sym, ok := g.resolveMethod(cls, mname); ok {
			declared++
			if g.genFuncs[sym] {
				yielding++
			}
		}
	}
	return declared, yielding
}

// methodCallCasesAreGenerator is the dynamic-dispatch form of the same question: the receiver's class is a
// run-time fact, so the answer is a list only when EVERY class that can answer the call yields — one
// non-generator arm and the shared register is a value on that road and a buffer on this one.
func (g *irGen) methodCallCasesAreGenerator(mname string) bool {
	declared, yielding := g.methodCallCensus(mname)
	return declared > 0 && declared == yielding
}

// methodCallYieldIsSplit names the shape the dynamic door cannot answer: some classes that declare this
// method yield and some do not, so the register the class-id `switch` joins carries a buffer on one arm and
// a plain value on another. The roads that receive it (the `for` loop, the print door, a binding) choose
// their helper from a kind the line does not state, and a `for` over such a call walks the counter while
// printing the loop variable — the answer the program never wrote, at the exit code of success. The door
// refuses it in words instead, and the row that owns the run-time tag beside the joined register is named
// in the sentence (roadmap **Gap R.467**, **ADR 0432**).
func (g *irGen) methodCallYieldIsSplit(mname string) bool {
	declared, yielding := g.methodCallCensus(mname)
	return yielding > 0 && yielding < declared
}

// copyInReboundParams gives every parameter the body rebinds a stack slot of its own
// at the top of the entry block and makes that slot authoritative, so a read after the
// assignment sees the store. Without it the assignment allocates a slot nothing reads
// and the read keeps returning the incoming register: `def bump(n): n = n + 1;
// return n` answers the argument, and an accumulator loop never terminates (Gap R.3,
// ADR 0196).
//
// floatRet is the float-returning case, whose prologue already copies every parameter
// into a double slot: those parameters behave like locals today, and giving them a
// second (i32) slot would be both wrong and a duplicate name. A parameter rebound to a
// float outside that case is excluded by reboundParams, which asks the same
// (*irGen).isFloat question the store path will ask, so no i32 slot is ever asked to
// hold a double.
func (g *irGen) copyInReboundParams(b *strings.Builder, fd *FuncDef, floatRet bool) {
	if fd == nil || floatRet || len(fd.Params) == 0 {
		return
	}
	rb := reboundParams(fd, g.isFloat)
	if len(rb) == 0 {
		return
	}
	if g.paramSlot == nil {
		g.paramSlot = map[string]bool{}
	}
	for _, p := range fd.Params {
		if !rb[p.Name] {
			continue
		}
		reg, ok := g.params[p.Name]
		if !ok {
			continue
		}
		if g.allocd[p.Name] || g.paramSlot[p.Name] {
			// A slot already exists for this name (the float prologue, or a container
			// path that allocated it first): it is the authoritative one.
			g.allocd[p.Name] = true
			g.paramSlot[p.Name] = true
			continue
		}
		fmt.Fprintf(b, "  %%%s = alloca i32\n", "_"+p.Name)
		fmt.Fprintf(b, "  store i32 %s, i32* %%%s\n", reg, "_"+p.Name)
		// The slot is rooted like any other local: after `n = make()` it holds a heap
		// handle, and an unrooted handle is one collection away from a segfault
		// (ADR 0181). Rooting a raw int costs the collector one skip.
		g.gcReg(b, p.Name)
		g.allocd[p.Name] = true
		g.paramSlot[p.Name] = true
	}
}

// emitClassMethod emits a class method as an LLVM function with self as param 0.
func (g *irGen) emitClassMethod(className, funcName string, fd *FuncDef) {
	// `self.helper()` inside a body is a call on the class being lowered, and only the generator
	// knows which class that is: the verdict census asks it with this in hand (roadmap Gap R.316).
	prevClass := g.curClassSrc
	g.curClassSrc = className
	defer func() { g.curClassSrc = prevClass }()
	// A method is emitted i32-returning whatever its body computes, so a parameter the body rebinds to
	// a float and returns by bare name answers the argument: the same refusal funcDef issues for a
	// module function, asked here before the header is written so no half-emitted define is left
	// behind (roadmap L11.6, Gap R.3c, ADR 0196).
	// A method is emitted i32-returning whatever its body computes, so a parameter the body rebinds to a
	// float and hands back as a value has no word for the answer: the same refusal funcDef issues for a
	// module function, asked here before the header is written so no half-emitted define is left behind.
	// Asked for every shape the return can read the rebound name in — including the ternary arm, which the
	// module-function gate refuses separately and which a method would otherwise answer as a truncated
	// i32 select (`c.m(1.0)` printing 1 for 2.5) (roadmap L11.6, Gap R.3c, Gap R.102, ADR 0196).
	if bad := returnedFloatRebindings(fd, g.isFloat, g.withFloatNames); len(bad) > 0 {
		g.noteUnlowered(fd, fmt.Errorf("%q is rebound to a float by this body and returned by bare name (or through an expression, a negation, a numeric builtin, or a ternary arm): a method is emitted with an i32 return word, so the double the body computed has no word to travel in and the call answers the argument as it arrived — bind it to a new name, or return an expression the compiler reads as a float (roadmap L11.6, Gap R.3c, ADR 0196)", bad[0]))
		return
	}
	// The same gate, asked of the module's double: a method that hands back `RATE` would load
	// `@gy_mod_RATE` inside a `define i32`, which `llc` rejects — exit 2, the code the exit-code
	// contract reserves for a compiler bug. Refused here, in words, with the row that owns the missing
	// half (roadmap Gap R.35 read from the class side; L11.6 · Gap R.3c is the return word it needs).
	if nm := returnedModuleFloat(fd, g.moduleFloatOwns); nm != "" {
		g.noteUnlowered(fd, fmt.Errorf("%q is the module's float and this method returns it: a method is emitted with an i32 return word, so the module global's double has no word to travel in — print the value from the body instead, or wait for the method's own double return word (roadmap Gap R.35 read from the class side; L11.6, Gap R.3c, ADR 0196 is the return word it needs; ADR 0166 why nothing malformed is emitted)", nm))
		return
	}
	prevParams := g.params
	prevSlot := g.paramSlot
	prevSelf := g.selfClass
	paramRegs := []string{"i32 %self"}
	for i := range fd.Params {
		paramRegs = append(paramRegs, fmt.Sprintf("i32 %%p%d", i+1))
	}
	g.params = map[string]string{"self": "%self"}
	// A method body is a body like any other: what it binds stays local to it (ADR 0227).
	doneMethodBody := g.enterBody(fd.Body)
	defer doneMethodBody()
	for i := 1; i < len(fd.Params); i++ {
		g.params[fd.Params[i].Name] = fmt.Sprintf("%%p%d", i)
		// A method parameter every call site hands a verdict to is a verdict inside the body, by the
		// same reading `funcDef` applies to a `def`: the call site saw `z.m(True)`, and the body's
		// `print(f)` asks the print road a question about a name it never assigned
		// (roadmap Gap R.111, L11.1's remaining bool item).
		if g.boolParamOf[fd.Name][i] {
			if g.boolVars == nil {
				g.boolVars = map[string]bool{}
			}
			g.boolVars[fd.Params[i].Name] = true
		} else {
			delete(g.boolVars, fd.Params[i].Name)
		}
		// A method parameter every call site hands a TEXT to arrives as an index into the runtime string
		// table, and inside the body it has to behave exactly like a name the body assigned a text to: the
		// print, `str`/`repr`, `len`, `in`, `==`, the formatting, the repeat, the f-string field and the
		// char-method roads all ask `internedVars` of the name. `funcDef` has done this for a `def` since
		// Gap J.5; the method road never did, which is why `def fmt(self, s): return "x%s" % s` printed the
		// intern POSITION (`x0`) where the function twin printed `xhi`, and `return s * 2` printed `0`
		// (roadmap Gap R.315, ADR 0366). Authoritative, not additive, for the same reason the verdict arm is.
		if g.strParamOf[fd.Name][i] {
			g.internedVars[fd.Params[i].Name] = true
		} else {
			delete(g.internedVars, fd.Params[i].Name)
		}
		// The float twin, read by the slice-bound door alone (roadmap Gap R.237, floatargs.go): a method
		// parameter every call site hands a float to is a float inside the body, and `def cut(self, k):`
		// / `return self.xs[0:k]` asks the same question a `def`'s body asks.
		if g.floatParamOf[fd.Name][i] {
			g.floatParamNames[fd.Params[i].Name] = true
		} else {
			delete(g.floatParamNames, fd.Params[i].Name)
		}
	}
	g.selfClass = className
	// A method is a call like any other, so it needs what `funcDef` gives a `def`: its own
	// raise-exit block, no handler inherited from whatever was being emitted when the class
	// happened to be registered, and no deferred bodies belonging to an enclosing `try`. The
	// missing raise-exit was the visible half of roadmap Gap R.41 (ADR 0223): a `try` or a
	// `raise` in a method body branched to `br label %` with an empty target and `llc`
	// rejected the module, so a program whose answer was `m fin\n3` exited 2 with a temp path.
	prevRaise := g.funcRaiseExit
	g.funcRaiseExit = funcName + ".raiseexit"
	prevHandlers := g.handlerStack
	g.handlerStack = nil
	prevHandled := g.handledArms
	g.handledArms = 0
	prevDeferred := g.deferred
	g.deferred = nil
	prevFnSrc := g.curFnSrc
	g.curFnSrc = className + "." + fd.Name
	savedInFunc := g.inFunc
	savedFD := g.curFD
	savedMethod := g.emittingMethod
	g.emittingMethod = true
	g.inFunc = true
	// The frame the roads below read a name against, for the same reason `funcDef` carries it: a lambda
	// written in a method body that reads the method's own parameter asks this declaration what its
	// calls hand to it (roadmap L11.7, Gap R.276).
	g.curFD = fd
	// A method's locals are frame slots like any other body's, so they carry the same flags (ADR 0228) —
	// and that table has to be the METHOD's before a single line of its frame is emitted. Asking
	// `emitBoundAllocas` while the ENCLOSING body's flags were still installed allocated the enclosing
	// body's locals inside this `define`, marked them allocated in the method's frame table, and left this
	// body's own possibly-unwritten name with no flag to set: `store i8 1, i8* %bnd_z` reached `llc` as
	// `use of undefined value`, exit 2, for a method with a local on a path that may not write it
	// (roadmap L11.6 · Gap R.342, ADR 0378 — the same missing frame entry the closure lift had, and the
	// same one helper now takes: `enterBody` above, `frame_slots.go` the rule).
	doneMethodFlags := g.enterBoundFlags(fd, fd.Body)
	defer doneMethodFlags()
	// The answer direction's METHOD round (roadmap **Gap R.453**, **ADR 0430**). A method is emitted i32
	// returning whatever its body computes, and until this line the only reading of "what kind is its answer"
	// that a method got was the one the name-keyed tables could state: a body whose arms answer `7` and the
	// void printed `7 0 False` for CPython's `7 None True`, because which arm ran is a run-time fact and no
	// word said so; and a body that answers a container handed `llc` the literal's GLOBAL in the return slot
	// — `ret i32 @.lst1`, refused at **exit 2** (**ADR 0166**'s compiler-bug code) on a program the reference
	// prints `[1, 2]`. A module `def` has the table **ADR 0333** built for the second shape and the plan for
	// the first; a method had neither, so both are answered here, with the ONE word the pair road already
	// uses for an answer's kind. A body that answers one immediate kind — a number, a verdict, a text —
	// keeps the roads it has always had, unchanged, exactly as the module round's rule reads.
	prevMethodAnswer := g.curMethodAnswer
	g.curMethodAnswer = ""
	defer func() { g.curMethodAnswer = prevMethodAnswer }()
	if _, _, _, _, carried := methodAnswerBodyPlan(fd, g.answer); carried {
		// Which arms answer an OBJECT is asked by the caller, not the emitter: the root that keeps a payload
		// the callee allocated alive is taken at the call, where the pair is read (ADR 0280).
		g.curMethodAnswer = funcName
		g.answerDeclareMethod(funcName)
	}
	// A method is program source, so it gets a DISubprogram of its own; the pass only ever puts
	// locations inside a function that registered one, which is what keeps the compiler's own
	// runtime blocks out of the line table (ADR 0231).
	g.dbgDefine(&g.globals, funcName, className+"."+fd.Name, fd.Src)
	g.globals.WriteString(fmt.Sprintf("define i32 @%s(%s) {\n", funcName, strings.Join(paramRegs, ", ")))
	// The written-flags for this body's possibly-unwritten locals, immediately inside the brace
	// (ADR 0228).
	g.emitBoundAllocas(&g.globals)
	// A method is a call like any other: it opens its own root frame and pops it on
	// the way out. Without this the roots its body pushes (self, container args,
	// locals) piled up on the root stack one frame per call, so a loop that made
	// thousands of instances ran the stack out (ADR 0181).
	savedFrame := g.frameOpen
	prevGenHandle := g.genHandle
	g.gcOpenFrame(&g.globals)
	// A generator METHOD gets the buffer its body yields into, exactly as the `def` road gives a module
	// generator: one list allocated in this frame, rooted so the first body statement cannot collect it, and
	// handed back as the answer. The reading is the body's own — `containsYield`, the same question `funcDef`
	// asks — because a `def` in a class body is a method by POSITION and its body never asked to be read any
	// other way; the refusal this replaces blamed the program for the position its body was written in
	// (roadmap **Gap R.455**, **ADR 0432**).
	isGen := methodIsGenerator(fd)
	if isGen {
		if param := generatorMethodParameterWalk(fd); param != "" {
			if g.genMethodParamWalk == nil {
				g.genMethodParamWalk = map[string]string{}
			}
			g.genMethodParamWalk[funcName] = param
		}
	}
	if isGen {
		g.genIdx++
		g.genHandle = fmt.Sprintf("%%gh%d", g.genIdx)
		g.genFuncs[funcName] = true
		g.heapUsed = true
		g.emitRtAllocKind(&g.globals, g.genHandle, HeapKindList, Span{})
		// Rooted like the module road's: the first `yield` emits a GC safe point, and an unrooted handle is
		// one collection away from appending into a freed slot (ADR 0181).
		ghslot := g.newTmp()
		ghslotName := strings.TrimPrefix(ghslot, "%")
		fmt.Fprintf(&g.globals, "  %%_%s = alloca i32\n", ghslotName)
		fmt.Fprintf(&g.globals, "  store i32 %s, i32* %%_%s\n", g.genHandle, ghslotName)
		g.gcReg(&g.globals, ghslotName)
	}
	// A method that assigns to one of its parameters gets a slot for it, like any
	// other rebinding body (Gap R.3).
	g.copyInReboundParams(&g.globals, fd, false)
	// The module's read-only literal containers, bound in this frame before the body reads them — the
	// same prologue the `def` road takes (`module_container.go`, roadmap Gap R.35's container half). A
	// method is emitted by its own road, so the hook is taken by both.
	if err := g.bindModuleContainerReads(&g.globals, fd); err != nil {
		// The same shape the body loop below uses for a refusal: name it, and let the epilogue close what
		// has been written. A failed binding leaves the name unbound, so the body's own read refuses too.
		if g.emitErr == nil {
			g.emitErr = err
		}
	}
	for _, st := range fd.Body {
		// A refusal inside a method body used to be dropped on the floor, which turned a source
		// error the front end should name into an incomplete function definition and a toolchain
		// rejection -- the ADR 0166 class, and the reason a method could not contain a construct
		// a plain function could (roadmap Gap R.41, ADR 0223).
		if err := g.stmt(&g.globals, st); err != nil {
			if g.emitErr == nil {
				g.emitErr = err
			}
			break
		}
	}
	// The road that falls off the end of a carried body answers the void, and says so: the same store the
	// `def` road's epilogue makes, so a caller that reads the kind word after a call that never reached a
	// `return` reads the void rather than whatever the last call left there (roadmap Gap R.228, ADR 0368,
	// carried to a class's door by **Gap R.453** / **ADR 0430**). Guarded on the round's own state rather
	// than left to `storePairTag`'s name-keyed fallback: a method that is NOT carried declared no word, and
	// a store to a global that was never emitted is the module `llc` refuses (**ADR 0166**'s exit 2).
	if isGen {
		// The answer a generator gives is its buffer, whatever the body's own `return` roads said on the way:
		// the same choice the `def` road makes for a module generator (ADR 0140, Gap D).
		g.gcCloseFrame(&g.globals)
		fmt.Fprintf(&g.globals, "  ret i32 %s\n", g.genHandle)
	} else {
		if g.curMethodAnswer != "" {
			g.storePairTag(&g.globals, answerWordNone)
		}
		g.gcCloseFrame(&g.globals)
		g.globals.WriteString("  ret i32 0\n")
	}
	// The method's own unwind target: it closes the frame it opened and returns; the caller's
	// call-site check (ADR 0218) is what turns the flag into a propagation.
	fmt.Fprintf(&g.globals, "%s:\n", g.funcRaiseExit)
	if isGen {
		// An unwinding generator hands back what it collected up to the raise — the `def` road's arm
		// (roadmap **Gap R.455**); the caller's check turns the flag into a propagation either way.
		g.gcCloseFrame(&g.globals)
		fmt.Fprintf(&g.globals, "  ret i32 %s\n", g.genHandle)
	} else {
		if g.curMethodAnswer != "" {
			g.storePairTag(&g.globals, answerWordNone) // the unwinding answer: no value, and the word says so
		}
		g.gcCloseFrame(&g.globals)
		g.globals.WriteString("  ret i32 0\n")
	}
	g.globals.WriteString("}\n")
	// The handle is this body's, not the next body's: a class registered while an enclosing generator was
	// in flight (`class` inside a `def`, handled at `codegen_stmt.go`) must not find its buffer's register
	// name installed, and a method that is not a generator must not inherit one.
	g.genHandle = prevGenHandle
	g.inFunc = savedInFunc
	g.curFD = savedFD
	g.emittingMethod = savedMethod
	g.frameOpen = savedFrame
	g.curFnSrc = prevFnSrc
	g.deferred = prevDeferred
	g.handledArms = prevHandled
	g.handlerStack = prevHandlers
	g.funcRaiseExit = prevRaise
	g.selfClass = prevSelf
	g.params = prevParams
	g.paramSlot = prevSlot
}

// collectAttrs walks a method body and assigns a slot to each instance attr.
func (g *irGen) collectAttrs(stmts []Stmt) {
	for _, st := range stmts {
		g.scanAttrs(st)
	}
}
func (g *irGen) scanAttrs(st Stmt) {
	switch n := st.(type) {
	case *AssignStmt:
		g.slotForAttrExpr(n.Target)
		g.slotForAttrExpr(n.Value)
	case *ExprStmt:
		g.slotForAttrExpr(n.Expr)
	case *IfStmt:
		for _, s := range n.Then {
			g.scanAttrs(s)
		}
		for _, s := range n.Else {
			g.scanAttrs(s)
		}
	case *WhileStmt:
		for _, s := range n.Body {
			g.scanAttrs(s)
		}
	case *ForStmt:
		for _, s := range n.Body {
			g.scanAttrs(s)
		}
	}
}

// slotForAttrExpr assigns a global slot for each `self.x` / `self.x = v` attr.
func (g *irGen) slotForAttrExpr(e Expr) {
	if attr, ok := e.(*Attr); ok {
		g.attrSlot(attr.Name.Value)
		return
	}
	if call, ok := e.(*Call); ok {
		for _, a := range call.Args {
			g.slotForAttrExpr(a)
		}
	}
}

// attrSlot returns the global instance-data slot for an attribute name.
func (g *irGen) attrSlot(name string) int {
	if g.attrSlots == nil {
		g.attrSlots = map[string]int{}
	}
	if s, ok := g.attrSlots[name]; ok {
		return s
	}
	s := g.nextSlot
	g.attrSlots[name] = s
	g.nextSlot++
	return s
}

// exprIsString answers whether an expression's value is text -- an index into @str_tab. The
// element-kind tests ask a runtime question (`heapElemKind` reads the emitted shape), and an
// element written with the comprehension's own variable -- `out = [n for n in names if n == "a"]`
// -- has no shape to read, which left `print(out[0])` printing the index as a number. This is the
// static half of the same question (roadmap Gap R.42, ADR 0224).
func (g *irGen) exprIsString(e Expr) bool {
	if g.seqIsTextExpr(e) {
		// `"ab" * 3` answers an @str_tab index — the word rt_str_repeat interns its buffer into — so every
		// text road takes it without knowing what built it (ADR 0229's rule, roadmap Gap R.33, ADR 0365).
		return true
	}
	if g.percentIsTextExpr(e) {
		// `"hi %s" % name` answers an @str_tab index — the same word a text literal answers — so every
		// text road takes it without knowing what built it (roadmap Gap R.31, ADR 0360).
		return true
	}
	switch v := e.(type) {
	case *StrLit, *FString:
		return true
	case *Name:
		return g.internedVars[v.Value]
	case *Call:
		if g.callReturnsStr(v) {
			return true
		}
		// A string method on a string receiver returns a string: `get()[1].upper()` is text, and
		// the print path and the operation path must ask that question the same way or one of them
		// renders the interned index as a number (ADR 0229).
		if at, ok := v.Fn.(*Attr); ok && g.exprIsString(at.Obj) && g.textMethodAnswersText(at.Name.Value, len(v.Args)) {
			return true
		}
		// str(n) is text whatever n is (ADR 0229): the print path and the operation path ask
		// one predicate, or a number's digits come out as an index. repr is text for the same
		// reason and asked of the same predicate — the pair is one question, so the answer to
		// "is it text" cannot be yes for one half and no for the other (roadmap L11.2, ADR 0258).
		if g.builtinCallAs(v, "str") || g.builtinCallAs(v, "repr") {
			return true
		}
		return false
	case *Attr:
		if cls := g.receiverClass(v.Obj); cls != "" {
			return g.strAttrs[cls+"."+v.Name.Value]
		}
		return false
	case *BinOp:
		if v.Op != "+" {
			return false
		}
		_, lf := g.stringVal(v.L)
		_, rf := g.stringVal(v.R)
		return lf || rf || (g.exprIsString(v.L) && g.exprIsString(v.R))
	case *Slice:
		// s[a:b] of a string is a string whatever the bounds are, constant or not (ADR 0229).
		return g.exprIsString(v.Obj)
	case *CondExpr:
		// A ternary's answer IS one of its arms, so "is it text" is the arm's answer — the same
		// rule ADR 0262 uses to choose the answer's WORD (roadmap L11.6, Gap R.102), asked here of
		// the text domain so the operation path and the print path ask one question (ADR 0229).
		// A constant test settles which arm runs; otherwise only arms that agree are text.
		if taken, ok := constantTestArm(v); ok {
			return g.exprIsString(taken)
		}
		return g.exprIsString(v.If) && g.exprIsString(v.Else)
	case *Index:
		// A subscript of a string the compiler can name is text (ADR 0225), and so is an element
		// of a string container: that is what lets `xs = [s[1]]` remember that its elements are
		// strings rather than print the index that means them.
		// A subscript of a string the compiler *cannot* name is still a one-character string
		// (ADR 0229) — being a string does not require being a constant.
		if g.exprIsString(v.Obj) {
			return true
		}
		if _, isStr := g.stringVal(v.Obj); isStr {
			return true
		}
		// The entry itself, asked of the literal that still describes the object's slots — the road that
		// knows a `None`-keyed entry is a text when the name's own kind bookkeeping cannot say it, because
		// a dict whose key is the void has no single value kind to record (roadmap Gap R.285, ADR 0419).
		// It is asked BEFORE the name's bookkeeping, which is the record a dict with a void key has none of.
		if g.staticSlotIsText(v) {
			return true
		}
		if nm, ok := v.Obj.(*Name); ok {
			return g.listElemStr[nm.Value] || g.setElemStr[nm.Value] || g.dictValStr[nm.Value]
		}
		if dl, isLit := v.Obj.(*DictLit); isLit {
			// The entry a literal dict's read answers has a kind, and it is the entry's own: asking the
			// dict would be asking the wrong thing, because a literal has no name for the bookkeeping to
			// be recorded against (roadmap Gap R.215, ADR 0327).
			return g.dictLiteralValueIsString(dl, v.Idx)
		}
		return false
	case *Comp:
		if len(v.Elems) != 1 {
			return false
		}
		// The element names the iteration variable, so its kind is the iterated collection's.
		if it, ok := v.Iter.(*Name); ok && (g.listElemStr[it.Value] || g.setElemStr[it.Value]) {
			if iv, ok2 := v.Elems[0].(*Name); ok2 && iv.Value == v.ForVar.Value {
				return true
			}
		}
		return g.exprIsString(v.Elems[0])
	}
	return false
}

// callReturnsStr answers whether the value a call produces is a string, in the language's
// representation -- an index into @str_tab. Module functions were answered by `strReturningFuncs`;
// methods had no such registration, so `print(Dog().sound())` printed the index as a number while
// the record and CPython printed the text (roadmap Gap R.42, ADR 0224).
func (g *irGen) callReturnsStr(c *Call) bool {
	if nm, ok := c.Fn.(*Name); ok {
		return g.strFuncs[nm.Value]
	}
	if at, ok := c.Fn.(*Attr); ok {
		if cls := g.receiverClass(at.Obj); cls != "" {
			if sym, ok2 := g.resolveMethod(cls, at.Name.Value); ok2 {
				return g.strFuncs[sym]
			}
		}
	}
	return false
}

// methodReturnsStr is `strReturningFuncs` for a method body: the annotation says it, or some
// `return` in the body hands back a string literal or a string parameter. The emitter cannot ask
// the value path, because at that point the question is how to name the return type at every call
// site.
func (g *irGen) markStringAttrs(className string, fd *FuncDef) {
	for _, st := range fd.Body {
		g.scanStringAttrs(className, st)
	}
}

// scanStringAttrs finds `self.<attr> = <string>` anywhere a method body can reach, including
// inside an if/while/try, because an attribute initialised on one path is still a string-valued
// attribute of the class.
func (g *irGen) scanStringAttrs(className string, st Stmt) {
	switch n := st.(type) {
	case *AssignStmt:
		if at, ok := n.Target.(*Attr); ok {
			recv := g.receiverClass(at.Obj)
			if recv != "" && recv != className {
				recv = className // `self` inside a method of this class
			}
			if recv == "" {
				recv = className
			}
			if _, foldable := g.stringVal(n.Value); foldable {
				g.strAttrs[recv+"."+at.Name.Value] = true
			} else if g.textDoorIsText(n.Value) {
				// `self.w = "%s!" % "hi"` and `self.w = "hi!" * 2` both store an @str_tab index in the slot, so
				// reading it back prints the text and not the position (roadmap Gap R.31, ADR 0360; Gap R.33,
				// ADR 0365; ADR 0224's slot-is-the-truth).
				g.strAttrs[recv+"."+at.Name.Value] = true
			} else if c, ok := n.Value.(*Call); ok && g.callReturnsStr(c) {
				g.strAttrs[recv+"."+at.Name.Value] = true
			}
		}
	case *IfStmt:
		for _, s := range n.Then {
			g.scanStringAttrs(className, s)
		}
		for _, e := range n.Elifs {
			for _, s := range e.Then {
				g.scanStringAttrs(className, s)
			}
		}
		for _, s := range n.Else {
			g.scanStringAttrs(className, s)
		}
	case *WhileStmt:
		for _, s := range n.Body {
			g.scanStringAttrs(className, s)
		}
	case *ForStmt:
		for _, s := range n.Body {
			g.scanStringAttrs(className, s)
		}
	case *WithStmt:
		for _, s := range n.Body {
			g.scanStringAttrs(className, s)
		}
	case *TryStmt:
		for _, s := range n.Body {
			g.scanStringAttrs(className, s)
		}
		for _, ec := range n.Excepts {
			for _, s := range ec.Body {
				g.scanStringAttrs(className, s)
			}
		}
		for _, s := range n.Finally {
			g.scanStringAttrs(className, s)
		}
	case *MatchStmt:
		for _, c := range n.Cases {
			for _, s := range c.Body {
				g.scanStringAttrs(className, s)
			}
		}
	}
}

// returnExprIsText is `methodReturnsStr`'s question for an expression the pre-scan can only read
// structurally: at this point in the pass the per-function records (`strVals`, `internedVars`) do not
// exist yet, so the answer comes from the shape of the arm and from the parameter annotations.
func returnExprIsText(e Expr, strParams map[string]bool) bool {
	if textDoorShapeIsText(e) {
		// A formatting and a repeat both hand back the text the door rendered: one @str_tab index, the same
		// word a literal is (roadmap Gap R.31, ADR 0360; Gap R.33, ADR 0365).
		return true
	}
	switch t := e.(type) {
	case *StrLit, *FString:
		return true
	case *Name:
		return strParams[t.Value]
	case *CondExpr:
		if taken, ok := constantTestArm(t); ok {
			return returnExprIsText(taken, strParams)
		}
		return returnExprIsText(t.If, strParams) && returnExprIsText(t.Else, strParams)
	}
	return false
}
func methodReturnsStr(fd *FuncDef) bool { return methodReturnsStrWith(fd, nil) }

// methodReturnsStr asks the census beside the shape. A body that hands back a parameter the call sites
// fill with texts hands back a text, and a caller that was never told prints the @str_tab POSITION under
// it — the same defect ADR 0290 pinned for a ternary arm and ADR 0360 for a formatting, arriving from the
// method side (roadmap Gap R.315, ADR 0366). The shape question alone could not see it: at the pre-scan
// `s` is a name, and only the call sites say what it holds.
func (g *irGen) methodReturnsStr(fd *FuncDef) bool {
	if fd == nil {
		return false
	}
	var census map[string]bool
	for i, p := range fd.Params {
		if p == nil || !g.strParamOf[fd.Name][i] {
			continue
		}
		if census == nil {
			census = map[string]bool{}
		}
		census[p.Name] = true
	}
	if methodReturnsStrWith(fd, census) {
		return true
	}
	// A body that hands back a class MEMBER — `return self.NAME`, where the class body or any BASE of it
	// bound a text — hands back a text too. The shape predicate below reads literals, annotations and
	// parameters, and a class member is none of those: it belongs to the class object, reached by the same
	// resolution chain an attribute read walks (roadmap row **L12.4**, **Gap R.56**). Without this arm the
	// caller prints the intern table's index — `0` — where the reference prints the text.
	if g.methodReturnsClassText(fd) {
		return true
	}
	// The shape predicate reads literals, annotations and the two built-text doors' SPELLINGS. The
	// generator-side predicate reads what the operations actually answer — a char method on a text parameter,
	// a subscript of one, a repeat of one — and is the same question `scanStringBindings` asks a `def`
	// (roadmap Gap R.315, ADR 0366).
	return returnsStringExpr(g, fd, census)
}

// methodReturnsStrWith is the shape question, plus whatever the caller's census already knows about the
// parameters. `extraTextParams` is nil for a pure read of the source.
func methodReturnsStrWith(fd *FuncDef, extraTextParams map[string]bool) bool {
	if fd == nil {
		return false
	}
	if fd.ReturnAnno != nil && fd.ReturnAnno.Kind == KindString {
		return true
	}
	strParams := map[string]bool{}
	for _, p := range fd.Params {
		if p == nil {
			continue
		}
		if p.Annot != nil && p.Annot.Kind == KindString {
			strParams[p.Name] = true
		}
	}
	for n := range extraTextParams {
		strParams[n] = true
	}
	found := false
	var walk func([]Stmt)
	walk = func(sts []Stmt) {
		for _, st := range sts {
			switch n := st.(type) {
			case *ReturnStmt:
				if n.Expr == nil {
					continue
				}
				switch e := n.Expr.(type) {
				case *StrLit, *FString:
					found = true
				case *BinOp:
					// `return "%d items" % n` and `return "ab" * 2` each hand back a TEXT, and a body that says so
					// must register the function in `strFuncs` or its caller prints the @str_tab POSITION — the same
					// defect ADR 0290 pinned for a ternary, in a new spelling (roadmap Gap R.31, ADR 0360; Gap R.33,
					// ADR 0365).
					if textDoorShapeIsText(e) {
						found = true
					}
				case *Name:
					if strParams[e.Value] {
						found = true
					}
				case *CondExpr:
					// `return "even" if x % 2 == 0 else "odd"` returns a TEXT, and a body that says so
					// must register the function in `strFuncs` or its caller prints the @str_tab POSITION:
					// `print(f(4))`/`print(f(5))` answered `0`/`1` where the reference prints
					// even/odd (Gap R.173, ADR 0290). Asked of the arms, which is the same rule ADR 0262
					// uses to choose a ternary's word — a ternary hands back one of its arms, so the body's
					// kind is the arm's kind. A constant test settles which arm runs; a run-time test needs
					// both arms to agree, because a function whose two returns disagree on kind is not a
					// string-returning function (the rule that keeps a `return "a"`/`return 0` body out of
					// the table at all).
					if returnExprIsText(e, strParams) {
						found = true
					}
				}
			case *IfStmt:
				walk(n.Then)
				for _, el := range n.Elifs {
					walk(el.Then)
				}
				walk(n.Else)
			case *WhileStmt:
				walk(n.Body)
			case *ForStmt:
				walk(n.Body)
			case *WithStmt:
				walk(n.Body)
			case *MatchStmt:
				for _, c := range n.Cases {
					walk(c.Body)
				}
			case *TryStmt:
				walk(n.Body)
				for _, ec := range n.Excepts {
					walk(ec.Body)
				}
				walk(n.Finally)
			}
		}
	}
	walk(fd.Body)
	return found
}
func (g *irGen) resolveMethod(className, mname string) (string, bool) {
	ci, ok := g.classInfos[className]
	if !ok {
		return "", false
	}
	if fn, ok := ci.methods[mname]; ok {
		return fn, true
	}
	for _, b := range ci.bases {
		if fn, ok := g.resolveMethod(b, mname); ok {
			return fn, true
		}
	}
	return "", false
}

// hasMethod reports whether any registered class defines a method named mname.
func (g *irGen) hasMethod(mname string) bool {
	for _, cls := range g.classOrder {
		if _, ok := g.resolveMethod(cls, mname); ok {
			return true
		}
	}
	return false
}

// receiverClass returns the class of a receiver expression, if statically known.
func (g *irGen) receiverClass(recv Expr) string {
	if n, ok := recv.(*Name); ok {
		if n.Value == "self" {
			return g.selfClass
		}
		if c, ok := g.varClasses[n.Value]; ok {
			return c
		}
	}
	// `Dog().sound()` — the receiver is a fresh instance of a class the source names. Without
	// this the receiver's class is unknown, so the call fell to the dynamic switch and the
	// question "does this method return a string?" had nobody to ask, printing the @str_tab
	// index where the record and CPython print the text (Gap R.42, ADR 0224).
	if c, ok := recv.(*Call); ok {
		if n, ok2 := c.Fn.(*Name); ok2 {
			if _, isClass := g.classInfos[n.Value]; isClass {
				return n.Value
			}
		}
	}
	return ""
}

// classAttrValueStable answers which class-body bindings this road may keep as an EXPRESSION and lower again
// at the read: the ones whose lowering builds no new object and calls nothing. Re-lowering a container
// literal would mint a fresh one per read, so `C.items.append(1)` and `print(C.items)` would disagree about
// the list, and a call would run again where the reference ran it once — both stay refused (roadmap
// Gap R.431). A name, a literal of the scalar kinds, a position of a literal container and a sum of those
// are the shapes the read can be trusted with.
func classAttrValueStable(e Expr) bool {
	switch x := e.(type) {
	case *IntLit, *StrLit:
		return true
	case *Name:
		return true
	case *Index:
		// A position of a container the front end can see (`first = xs[0]`, the program the row is pinned
		// with) is a read, not a new object: lowering it again asks the same slot.
		return classAttrValueStable(x.Obj) && classAttrValueStable(x.Idx)
	case *BinOp:
		return classAttrValueStable(x.L) && classAttrValueStable(x.R)
	case *UnOp:
		return classAttrValueStable(x.X)
	case *CondExpr:
		return classAttrValueStable(x.Cond) && classAttrValueStable(x.If) && classAttrValueStable(x.Else)
	}
	return false
}

// classAttrValueRecorded is the wider question the READ asks when it has to refuse: the binding is a value
// this road recognises but cannot answer faithfully yet, so the sentence names the kind instead of claiming
// the class holds no such member (roadmap Gap R.431, ADR 0418).
func classAttrValueRecorded(e Expr) bool {
	return e != nil
}

// classAttrKindNames the owed value for the refusal that names it.
func classAttrKindName(e Expr) string {
	switch e.(type) {
	case *FloatLit:
		return "a fractional member"
	case *BoolLit:
		return "a verdict member"
	case *NoneLit:
		return "a void member"
	case *Call:
		return "a member bound by a call"
	case *ListLit, *DictLit, *SetLit, *Tuple, *Comp, *Lambda:
		return "a member bound to a container or a callable"
	}
	return "a member whose value this road cannot lower faithfully"
}
