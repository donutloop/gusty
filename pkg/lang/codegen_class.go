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
	doc     string
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
	ci := &classInfo{bases: []string{}, methods: map[string]string{}, doc: cd.Doc}
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
		g.markStringAttrs(cd.Name, fd)
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
	g.gcOpenFrame(&g.globals)
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
	if g.curMethodAnswer != "" {
		g.storePairTag(&g.globals, answerWordNone)
	}
	g.gcCloseFrame(&g.globals)
	g.globals.WriteString("  ret i32 0\n")
	// The method's own unwind target: it closes the frame it opened and returns; the caller's
	// call-site check (ADR 0218) is what turns the flag into a propagation.
	fmt.Fprintf(&g.globals, "%s:\n", g.funcRaiseExit)
	if g.curMethodAnswer != "" {
		g.storePairTag(&g.globals, answerWordNone) // the unwinding answer: no value, and the word says so
	}
	g.gcCloseFrame(&g.globals)
	g.globals.WriteString("  ret i32 0\n}\n")
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
