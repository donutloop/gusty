// codegen_call.go is where a Call is decided. g.call names the callee and hands it to the
// road that knows it: a method in codegen_method.go, an instantiation in
// codegen_instance.go, a user function in codegen_func.go, an extern and an imported
// sibling here, and every predeclared name in codegen_builtin.go and its family files.
package lang

import (
	"fmt"
	"strings"
)

// call resolves the callee's name — a Name, an inlined lambda, a name bound to a lambda — and then
// asks, in order, the six roads that can answer a call. Each road is a method in the file that owns
// its shape; the order below is the order the roads have always been tried in.
func (g *irGen) call(b *strings.Builder, c *Call) (string, error) {
	fnName := ""
	if n, ok := c.Fn.(*Name); ok {
		fnName = n.Value
	} else if lam, ok := c.Fn.(*Lambda); ok {
		// inline lambda callee: `(lambda x: expr)(args)`.
		name, err := g.emitLambda(b, lam)
		if err != nil {
			return "", err
		}
		fnName = name
	}
	// `f(3)` where f was bound to a lambda: resolve to its FuncDef name.
	if fnName != "" {
		if b, ok := g.funcBind[fnName]; ok {
			fnName = b
		}
	}
	if fnName != "" {
		if lamName, ok := g.lambdas[fnName]; ok {
			fnName = lamName
		}
	}
	// constant-fold attr methods on constant receivers.
	if attr, ok := c.Fn.(*Attr); ok {
		return g.callMethod(b, c, attr, fnName)
	}

	// Class instantiation: `ClassName(args)`.
	if _, isClass := g.classInfos[fnName]; isClass {
		return g.callInstantiate(b, c, fnName)
	}

	if ed, ok := g.externs[fnName]; ok {
		return g.callExtern(b, c, fnName, ed)
	}
	if g.curModName != "" && g.imports != nil {
		if v, called, err := g.callImportedSibling(b, c, fnName); called {
			return v, err
		}
	}
	if g.funcs[fnName] {
		c, err := g.lambdaCallArgs(c, fnName)
		if err != nil {
			return "", err
		}
		return g.callUserFunc(b, c, fnName)
	}
	// Everything the predeclared names answer is one dispatch away (codegen_builtin.go).
	return g.callBuiltins(b, c, fnName)
}

// lambdaCallArgs fills the parameters an applied lambda gained when its body reads the frame that
// encloses it: the captured name arrives as one more keyword argument whose value is the CALLER'S
// own reading of that name, which is the same read the frame makes anywhere else in its body. A
// positional argument is lowered before a keyword one by callUserFunc, so appending is the only
// shape that cannot collide with how the source spelled its own arguments — except where the source
// already passes a keyword with the captured name on it, which is the one program this road declines
// out loud rather than answer twice (the reference refuses it too, with `got an unexpected keyword
// argument`). The caller's Call is never mutated: this returns a copy, so the tree `--emit-ast`
// prints and `gusty fmt` renders stays the program that was written (ADR 0345's rule, ADR 0320's).
func (g *irGen) lambdaCallArgs(c *Call, fnName string) (*Call, error) {
	captured := g.lambdaCaptures[fnName]
	if len(captured) == 0 {
		return c, nil
	}
	for _, a := range c.Args {
		kw, isKw := a.(*KeywordArg)
		if !isKw {
			continue
		}
		for _, name := range captured {
			if kw.Name == name {
				return nil, fmt.Errorf("codegen: the function called here reads %q from the frame that encloses it, and this call also passes a keyword argument of that name: one name would answer two different values, which the reference refuses as `%s` (roadmap L11.7, Gap R.265)", name, "<lambda>() got an unexpected keyword argument '"+name+"'")
			}
		}
	}
	args := append([]Expr(nil), c.Args...)
	// The sentence names the callable the way a reader knows it. A clone the defunctionalisation pass minted
	// answers under the name the PROGRAM wrote — `<lambda>` for a clone of an applied lambda, `twice` for a
	// clone of a `def` — and the minted symbol is this compiler's private spelling, which ADR 0345 §3 keeps out
	// of a traceback and out of a diagnostic for the same reason.
	subject := fnName
	if fd := g.fds[fnName]; fd != nil && fd.CloneOf != "" {
		subject = callableDisplayName(fd)
	}
	for _, name := range captured {
		if kind, carriesOneWord := g.captureCarriesOneWord(name); !carriesOneWord {
			return nil, captureWordErr(name, kind, subject+"()")
		}
		args = append(args, &KeywordArg{
			Name:  name,
			Value: &Name{Value: name, Src: c.Span()},
			Src:   c.Span(),
		})
	}
	extended := *c
	extended.Args = args
	return &extended, nil
}

// captureCarriesOneWord asks, of the tables the print door and the arithmetic roads already read, whether
// the value a capture carries can travel in the one i32 word a parameter arrives in. It cannot: the
// captured name's facts belong to the frame that owns it, and the generated `define` is a different scope,
// so a captured container reaches the printer as its heap handle (`0` where the reference prints
// `[1, 2]`), a captured bool as `1` for `True`, the void as `0` for `None`, a text as its `@str_tab`
// position, and a double is truncated — or, when the parameter arrives as a `double`, the module asks
// `llc` to return a double from an `i32` function and the run dies at exit 2, the code ADR 0166 reserves
// for a compiler bug.
//
// The neighbouring road has the same hole today — a nested `def`'s `%env` capture answers `0`, `1` and `0`
// for `[1, 2]`, `True` and `None` (filed beside this row, because fixing it is the tagged value word of
// L11.1 and not a lambda's) — and a door that cannot answer says so rather than matching its neighbour
// (ADR 0249). What does travel is what the frame's tables describe as a plain number, or describe as
// nothing at all: the value a body arrives with, or binds from one.
func (g *irGen) captureCarriesOneWord(name string) (string, bool) {
	return g.valueCarriesOneWord(&Name{Value: name})
}

// valueCarriesOneWord is the same question asked of any expression, which is the shape a scan of a frame's
// call sites needs: `def f(v): return (lambda k: k + v)(1)` with `f("abc")` binds a text to the captured
// name at run time, and the only evidence of that is the argument the program wrote.
func (g *irGen) valueCarriesOneWord(e Expr) (string, bool) {
	if nm, isName := e.(*Name); isName {
		return g.nameCarriesOneWord(nm.Value)
	}
	switch {
	case g.isContainerExpr(e):
		return "a container", false
	case g.isTupleExpr(e):
		return "a tuple", false
	case g.isNoneExpr(e):
		return "the void value", false
	case g.isFloat(e):
		return "a float", false
	}
	switch e.(type) {
	case *BoolLit:
		return "a bool", false
	case *ListLit, *DictLit, *SetLit:
		return "a container", false
	case *FloatLit:
		return "a float", false
	case *StrLit:
		return "a text", false
	}
	if _, isText := g.stringVal(e); isText {
		return "a text", false
	}
	return "", true
}

// nameCarriesOneWord asks the question of a name the frame owns, which is where its tables live.
func (g *irGen) nameCarriesOneWord(name string) (string, bool) {
	nm := &Name{Value: name}
	switch {
	case g.isContainerExpr(nm):
		return "a container", false
	case g.isTupleExpr(nm):
		return "a tuple", false
	case g.isNoneExpr(nm) || g.noneVars[name]:
		return "the void value", false
	case g.boolVars[name]:
		return "a bool", false
	case g.taggedVars[name] || g.unionVars[name]:
		return "a value whose kind only its tag carries", false
	case g.internedVars[name]:
		return "a text", false
	default:
		if _, isText := g.stringVal(nm); isText {
			return "a text", false
		}
		if g.isFloat(nm) {
			return "a float", false
		}
		// A parameter of a body the float convention is emitting arrives as a `double` in every position;
		// handing that register to an `i32` parameter is the module `llc` rejects.
		if _, isParam := g.params[name]; isParam && g.floatFuncs[g.curFunc] {
			return "a number the float convention crosses as a double", false
		}
		// A parameter's value is whatever the calls hand it, and the frame's own tables only learn a name
		// is a double when the body itself binds one. `def f(v): return (lambda k: k + v)(1)` with
		// `f(2.5)` is the case this asks: the body reads `v` only inside the lambda, so the float convention
		// never marked the position, the argument arrived truncated to `2`, and the program answered `3` at
		// the exit code of success where the reference answers `3.5` (roadmap L11.6, Gap R.164's one-word
		// argument). Asking every call the program makes of this frame is the same question the (payload,
		// tag) scan asks; a frame whose calls cannot be found is not proved, and an unproved value does not
		// cross as one word.
		if _, isParam := g.params[name]; isParam {
			return g.captureParamWord(name)
		}
	}
	return "", true
}

// captureParamWord asks every call the program makes of the frame this capture belongs to what it hands
// the parameter that is being captured. `argAt` answers nil for a call that is not one of this frame's,
// so a lambda frame is read against the application node it came from (`g.lambdaOrigin`) and a `def`, a
// clone or a hoisted declaration against the name the rewritten program calls.
func (g *irGen) captureParamWord(name string) (string, bool) {
	fd := g.curFD
	if fd == nil {
		fd = g.fds[g.curFunc]
	}
	if fd == nil {
		return "a parameter of a body this backend cannot inspect", false
	}
	// The question is per (frame, parameter), and asking it walks the program. Two guards follow from
	// that: a memo, so a frame asked about the same capture at every emission asks once; and a path set,
	// because the question is recursive — `def f(v): return (lambda k: k + v)(1)` types `v` by asking what
	// its call sites hand it, and a call site whose argument is `v` itself asks the same question back.
	// That is not a hard case, it is a definition with no answer in the program: the kind is not settled,
	// and an unsettled value does not cross as one word (ADR 0273).
	key := fmt.Sprintf("%p\x00%s", fd, name)
	if g.captureWordAsking == nil {
		g.captureWordAsking = map[string]bool{}
	}
	if g.captureWordMemo == nil {
		g.captureWordMemo = map[string]captureWordAnswer{}
	}
	if g.captureWordAsking[key] {
		return captureWordUnaskable, false
	}
	if ans, ok := g.captureWordMemo[key]; ok {
		return ans.kind, ans.carries
	}
	g.captureWordAsking[key] = true
	defer func() { delete(g.captureWordAsking, key) }()
	kind, carries := g.captureParamWordOf(fd, name)
	g.captureWordMemo[key] = captureWordAnswer{kind: kind, carries: carries}
	return kind, carries
}

// captureWordUnaskable is the kind for a value the program itself does not settle: no call names an
// argument for the position, or the only argument written for it is the parameter being asked about.
// It is a refusal's subject, not a type — and the call-site loop hands it back UNSUFFIXED, because
// "…handed to that parameter by one of its call sites" would describe a call site that says nothing.
const captureWordUnaskable = "a value no call in the program can be asked about"

// captureWordAnswer is a memoised verdict on one (frame, parameter) question, and captureParamWordOf is
// the question itself — separated so the memo and the recursion guard sit in one place.
type captureWordAnswer struct {
	kind    string
	carries bool
}

func (g *irGen) captureParamWordOf(fd *FuncDef, name string) (string, bool) {
	idx := -1
	for i, p := range fd.Params {
		if p != nil && p.Name == name {
			idx = i
			break
		}
	}
	if idx < 0 {
		return "", true // not a parameter of this frame after all: the frame's own tables said so above
	}
	seen := false
	for _, c := range g.programCalls() {
		if !g.callBelongsToFrame(c, fd) {
			continue
		}
		arg := callArgAt(c, fd, idx)
		if arg == nil {
			continue
		}
		seen = true
		if kind, ok := g.valueCarriesOneWord(arg); !ok {
			if kind == captureWordUnaskable {
				return kind, false
			}
			return kind + " handed to that parameter by one of its call sites", false
		}
	}
	if !seen {
		if d := fd.Params[idx].Default; d != nil {
			if kind, ok := g.valueCarriesOneWord(d); !ok {
				return kind + " the body's own default hands to that parameter", false
			}
			return "", true
		}
		return captureWordUnaskable, false
	}
	return "", true
}

// programCalls is the whole-program call list, walked once. The reflective walk is the pass's own, and it
// is not cheap; a gate that asks it per capture per emission turns a compile into millions of node visits.
func (g *irGen) programCalls() []*Call {
	if g.programCallList == nil {
		g.programCallList = callableCallsOf(g.program)
	}
	return g.programCallList
}

// callBelongsToFrame reports whether a call evaluates this frame's function: a `def`, clone or hoisted
// declaration by the name the program (or the pass) calls it under, and an inline lambda by the node it
// was written as — which is how the application `(lambda k: …)(2)` is matched to the `define` minted for
// that exact lambda.
func (g *irGen) callBelongsToFrame(c *Call, fd *FuncDef) bool {
	switch fn := c.Fn.(type) {
	case *Name:
		if fn.Value == fd.Name {
			return true
		}
		if bound, ok := g.lambdas[fn.Value]; ok && bound == fd.Name {
			return true
		}
		if bound, ok := g.funcBind[fn.Value]; ok && bound == fd.Name {
			return true
		}
		return false
	case *Lambda:
		return g.lambdaOrigin[fd] == fn
	default:
		return false
	}
}

// callArgAt answers the expression a call hands to parameter `idx`, reading the keyword spellings the
// same way callUserFunc does, and nil when the call does not fill that position.
func callArgAt(c *Call, fd *FuncDef, idx int) Expr {
	if idx < 0 || idx >= len(fd.Params) {
		return nil
	}
	pos := 0
	for _, a := range c.Args {
		if kw, isKw := a.(*KeywordArg); isKw {
			if kw.Name == fd.Params[idx].Name {
				return kw.Value
			}
			continue
		}
		if pos == idx {
			return a
		}
		pos++
	}
	if pos <= idx && fd.Params[idx].Default != nil {
		return fd.Params[idx].Default // the call omits it: the body's own default is what arrives
	}
	return nil
}

// captureWordErr is the refusal, worded as the program's own: which name, what it holds, what the reference
// answers, and the road that owns the missing half.
func captureWordErr(name, kind, call string) error {
	return fmt.Errorf("codegen: %s reads %q from the frame that encloses it, and that name holds %s: a capture crosses into the generated function as one i32 word, which cannot say what the value is — the print door would answer the handle, the position in the string table or a truncated number where the reference answers the value. Name the value to the call instead (`%s(..., %s)` with the parameter reading it), or write the body as a nested `def`; the tagged word that would carry it is roadmap L11.1, and this row is L11.7 · Gap R.265, Gap R.276", call, name, kind, call, name)
}

// callImportedSibling calls a function that lives in the module this one is being emitted
// from, under the name the linker gave it. It answers `called` rather than falling through:
// the caller keeps going to the function table when the name is not a sibling's.
func (g *irGen) callImportedSibling(b *strings.Builder, c *Call, fnName string) (string, bool, error) {
	if sibling := g.imports.Funcs[g.curModName]; sibling != nil {
		if fd, ok := sibling[fnName]; ok && fd != nil {
			mangle := g.curModName + "$" + fnName
			ret := g.newTmp()
			b.WriteString(fmt.Sprintf("  %s = call i32 @%s(", ret, irSymbol(mangle)))
			for i, arg := range c.Args {
				if i > 0 {
					b.WriteString(", ")
				}
				av, err := g.value(b, arg)
				if err != nil {
					return "", true, err
				}
				b.WriteString("i32 " + av)
			}
			b.WriteString(")\n")
			return ret, true, nil
		}
	}
	return "", false, nil
}

// callExtern lowers a call to an `extern` declaration: the argument words the declaration asks for
// and the call to the program's own symbol, unprefixed (roadmap Gap R.4).
func (g *irGen) callExtern(b *strings.Builder, c *Call, fnName string, ed *ExternDecl) (string, error) {
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
			gn := g.strConst(lit.Value)
			n := len(lit.Value)
			argTypes = append(argTypes, "i8*")
			argRegs = append(argRegs, fmt.Sprintf("getelementptr([%d x i8], [%d x i8]* %s, i32 0, i32 0)", n, n, gn))
		} else {
			v, err := g.value(b, c.Args[i])
			if err != nil {
				return "", err
			}
			argTypes = append(argTypes, "i32")
			argRegs = append(argRegs, v)
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
	t := g.newTmp()
	// NOT irSymbol: an `extern fn` is an FFI surface, and its link name is the C name
	// the program asked to bind — `declare i32 @strlen(i8*)` and the call to it must
	// both stay exactly `strlen` (roadmap Gap R.4). Only names the program *defines*
	// are prefixed.
	b.WriteString(fmt.Sprintf("  %s = call %s @%s(%s)\n", t, ret, fnName, strings.Join(callArgs, ", ")))
	return t, nil
}
