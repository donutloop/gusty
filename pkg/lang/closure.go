package lang

import (
	"fmt"
	"strings"
)

type closureInfo struct {
	name     string
	params   []string
	captured []string
}

var closureBuiltins = map[string]bool{
	"len": true, "range": true, "print": true, "printf": true,
	"input": true, "int": true, "str": true, "float": true,
}

const envStoreName = "@env_store"
const envCountName = "@env_count"
const envSize = 4096

func closureParams(fd *FuncDef) []string {
	out := []string{}
	for _, p := range fd.Params {
		out = append(out, p.Name)
	}
	return out
}

func nestedDefs(body []Stmt) []*FuncDef {
	out := []*FuncDef{}
	for _, s := range body {
		if fd, ok := s.(*FuncDef); ok {
			out = append(out, fd)
		}
	}
	return out
}

func sortStrings(s []string) {
	for i := 1; i < len(s); i++ {
		for j := i; j > 0 && s[j] < s[j-1]; j-- {
			s[j], s[j-1] = s[j-1], s[j]
		}
	}
}

func closureInfoFor(fd *FuncDef, outerParams, outerLocals map[string]bool) *closureInfo {
	ci := &closureInfo{name: fd.Name, params: closureParams(fd)}
	free := map[string]bool{}
	collectStmtNames(fd.Body, free)
	params := map[string]bool{}
	for _, p := range ci.params {
		params[p] = true
	}
	locals := map[string]bool{}
	collectLocals(fd.Body, locals)
	for k := range free {
		if params[k] || locals[k] || closureBuiltins[k] {
			delete(free, k)
		}
	}
	for k := range free {
		if !outerParams[k] && !outerLocals[k] {
			delete(free, k)
		}
	}
	ci.captured = []string{}
	for k := range free {
		ci.captured = append(ci.captured, k)
	}
	sortStrings(ci.captured)
	return ci
}

func (g *irGen) emitEnvGlobals() {
	fmt.Fprintf(&g.globals, "%s = internal global [%d x i32] zeroinitializer\n", envStoreName, envSize)
	fmt.Fprintf(&g.globals, "%s = internal global i32 0\n", envCountName)
}

func (g *irGen) emitEnvLoad(b *strings.Builder, env string, off int) string {
	idx := g.newTmp()
	fmt.Fprintf(b, "  %s = add i32 %s, %d\n", idx, env, off)
	ptr := g.newTmp()
	fmt.Fprintf(b, "  %s = getelementptr [%d x i32], [%d x i32]* %s, i32 0, i32 %s\n", ptr, envSize, envSize, envStoreName, idx)
	val := g.newTmp()
	fmt.Fprintf(b, "  %s = load i32, i32* %s\n", val, ptr)
	return val
}

func (g *irGen) emitEnvStore(b *strings.Builder, env string, off int, val string) {
	idx := g.newTmp()
	fmt.Fprintf(b, "  %s = add i32 %s, %d\n", idx, env, off)
	ptr := g.newTmp()
	fmt.Fprintf(b, "  %s = getelementptr [%d x i32], [%d x i32]* %s, i32 0, i32 %s\n", ptr, envSize, envSize, envStoreName, idx)
	fmt.Fprintf(b, "  store i32 %s, i32* %s\n", val, ptr)
}

// emitNewEnv bumps @env_count and returns the fresh env index temp.
func (g *irGen) emitNewEnv(b *strings.Builder) string {
	cnt := g.newTmp()
	fmt.Fprintf(b, "  %s = load i32, i32* %s\n", cnt, envCountName)
	ncnt := g.newTmp()
	fmt.Fprintf(b, "  %s = add i32 %s, 1\n", ncnt, cnt)
	fmt.Fprintf(b, "  store i32 %s, i32* %s\n", ncnt, envCountName)
	return cnt
}

func collectNames(node interface{}, out map[string]bool) {
	switch n := node.(type) {
	case *Name:
		out[n.Value] = true
	case *BinOp:
		collectNames(n.L, out)
		collectNames(n.R, out)
	case *UnOp:
		collectNames(n.X, out)
	case *Call:
		collectNames(n.Fn, out)
		for _, a := range n.Args {
			collectNames(a, out)
		}
	case *ListLit:
		for _, e := range n.Elems {
			collectNames(e, out)
		}
	case *Tuple:
		for _, e := range n.Elems {
			collectNames(e, out)
		}
	case *DictLit:
		for _, k := range n.Keys {
			collectNames(k, out)
		}
		for _, v := range n.Vals {
			collectNames(v, out)
		}
	case *Index:
		collectNames(n.Obj, out)
		collectNames(n.Idx, out)
	case *Slice:
		collectNames(n.Obj, out)
		if n.Low != nil {
			collectNames(n.Low, out)
		}
		if n.High != nil {
			collectNames(n.High, out)
		}
		if n.Step != nil {
			collectNames(n.Step, out)
		}
	case *Attr:
		collectNames(n.Obj, out)
	}
}

func collectStmtNames(node interface{}, out map[string]bool) {
	switch n := node.(type) {
	case []Stmt:
		for _, s := range n {
			collectStmtNames(s, out)
		}
	case *AssignStmt:
		collectNames(n.Value, out)
	case *AugAssignStmt:
		// augmented assignment both reads and writes the target.
		collectNames(n.Target, out)
		collectNames(n.Value, out)
	case *ExprStmt:
		collectNames(n.Expr, out)
	case *ReturnStmt:
		collectNames(n.Expr, out)
	case *FuncDef:
		for _, s := range n.Body {
			collectStmtNames(s, out)
		}
	case *IfStmt:
		collectNames(n.Cond, out)
		for _, s := range n.Then {
			collectStmtNames(s, out)
		}
		for _, s := range n.Else {
			collectStmtNames(s, out)
		}
	case *WhileStmt:
		collectNames(n.Cond, out)
		for _, s := range n.Body {
			collectStmtNames(s, out)
		}
	case *ForStmt:
		for _, s := range n.Body {
			collectStmtNames(s, out)
		}
	}
}

func collectLocals(node interface{}, out map[string]bool) {
	switch n := node.(type) {
	case []Stmt:
		for _, s := range n {
			collectLocals(s, out)
		}
	case *AssignStmt:
		if nm, ok := n.Target.(*Name); ok {
			out[nm.Value] = true
		}
	case *FuncDef:
		for _, s := range n.Body {
			collectLocals(s, out)
		}
	case *IfStmt:
		for _, s := range n.Then {
			collectLocals(s, out)
		}
		for _, s := range n.Else {
			collectLocals(s, out)
		}
	case *WhileStmt:
		for _, s := range n.Body {
			collectLocals(s, out)
		}
	case *ForStmt:
		for _, s := range n.Body {
			collectLocals(s, out)
		}
	}
}

// emitClosureDef emits a nested closure as a top-level env function.
func (g *irGen) emitClosureDef(b *strings.Builder, ci *closureInfo, fd *FuncDef) {
	name := ci.name + "_env"
	// A closure is program source under another name: register it so its body gets locations
	// under the nested `def` it came from (ADR 0231).
	g.dbgDefine(b, name, ci.name, fd.Src)
	fmt.Fprintf(b, "define internal i32 @%s(i32 %%env", name)
	for i := range ci.params {
		fmt.Fprintf(b, ", i32 %%p%d", i)
	}
	fmt.Fprintf(b, ") {\n")
	// Written-flags for the closure's possibly-unwritten locals, inside the brace (ADR 0228).
	g.emitBoundAllocas(b)
	// The closure body is a call too: it opens its own root frame and pops it on the
	// way out, so its locals retain nothing once it returns (ADR 0181).
	savedFrame := g.frameOpen
	g.gcOpenFrame(b)
	g.params = map[string]string{}
	g.paramSlot = nil
	for i, p := range ci.params {
		g.params[p] = fmt.Sprintf("%%p%d", i)
	}
	g.envMode = true
	g.envCaptures = map[string]int{}
	for i, c := range ci.captured {
		g.envCaptures[c] = i
	}
	g.funcs[ci.name] = true
	g.fds[ci.name] = fd
	g.envParam = "%env"
	g.inFunc = true
	// A closure that rebinds one of its parameters reads a slot, not the incoming
	// register (Gap R.3) — and so does the environment capture above, which is why
	// the capture site consults paramSlot too.
	g.copyInReboundParams(b, fd, false)
	doneBody := g.enterBody(fd.Body)
	defer doneBody()
	doneFlags := g.enterBoundFlags(fd, fd.Body)
	defer doneFlags()
	for _, st := range fd.Body {
		if err := g.stmt(b, st); err != nil {
			// The body of a closure used to be emitted with its errors thrown away. A statement
			// that could not be lowered -- `return K` reading a module name the capture set had
			// dropped, say -- left the function ending in the fallback `ret i32 0`, so calling it
			// answered 0 for a program whose answer is 9, in the one place a compiler can be wrong
			// without leaving a trace (roadmap Gap R.35, ADR 0227; the same shape as ADR 0225's
			// truthyValue and ADR 0223's method emitter). Report it into the generator, where
			// GenerateIR refuses: nothing that is about to be refused may also be executed.
			//
			// The one exception is a closure nested inside a function used as a decorator: a
			// decorated call runs the trampoline, whose own body reports its failures, so this
			// closure object is unreachable and refusing would break programs that work today.
			// The failure is named in the module comment instead of hidden (ADR 0227).
			if g.emittingDecorator {
				g.closureBodyNotes = append(g.closureBodyNotes, fmt.Sprintf("closure %s: body not lowered (%v); a decorated call runs the trampoline instead", g.fnName(fd), err))
			} else {
				g.noteUnlowered(st, err)
			}
		}
	}
	g.inFunc = false
	if !strings.HasSuffix(strings.TrimSpace(b.String()), "ret ") {
		g.gcCloseFrame(b)
		fmt.Fprintf(b, "  ret i32 0\n")
	}
	fmt.Fprintf(b, "}\n")
	g.frameOpen = savedFrame
	g.envMode = false
	g.envCaptures = nil
	g.params = map[string]string{}
}

// emitDecoratedFunc lowers @dec def f via a function-pointer global + apply.
func (g *irGen) isWrappingDecorator(fd *FuncDef) bool {
	if len(fd.Body) != 2 {
		return false
	}
	wrap, ok := fd.Body[0].(*FuncDef)
	if !ok {
		return false
	}
	rs, ok := fd.Body[1].(*ReturnStmt)
	if !ok {
		return false
	}
	n, ok := rs.Expr.(*Name)
	return ok && n.Value == wrap.Name
}

func (g *irGen) wrapFunc(fd *FuncDef) (*FuncDef, string, bool) {
	if len(fd.Decorators) != 1 {
		return nil, "", false
	}
	n, ok := fd.Decorators[0].(*Name)
	if !ok {
		return nil, "", false
	}
	dec := g.fds[n.Value]
	if dec == nil || len(dec.Body) != 2 || len(dec.Params) != 1 {
		return nil, "", false
	}
	wrap, ok := dec.Body[0].(*FuncDef)
	if !ok {
		return nil, "", false
	}
	return wrap, dec.Params[0].Name, true
}

func (g *irGen) emitDecoratedFunc(b *strings.Builder, fd *FuncDef) error {
	if wrap, gParam, ok := g.wrapFunc(fd); ok {
		implName := fd.Name + "_impl"
		origClone := *fd
		origClone.Decorators = nil
		origClone.Name = fd.Name + "_orig"
		// A clone is a different pointer, and the checker only saw the original: record the alias so
		// the clone's body gets the same bound-flag treatment (ADR 0228).
		if g.fdAlias == nil {
			g.fdAlias = map[*FuncDef]*FuncDef{}
		}
		g.fdAlias[&origClone] = fd
		if err := g.funcDef(b, &origClone); err != nil {
			return err
		}
		g.funcs[fd.Name+"_orig"] = true
		g.fds[fd.Name+"_orig"] = &origClone
		clone := *wrap
		clone.Decorators = nil
		clone.Name = implName
		if g.fdAlias == nil {
			g.fdAlias = map[*FuncDef]*FuncDef{}
		}
		g.fdAlias[&clone] = fd
		g.funcBind[gParam] = fd.Name + "_orig"
		if err := g.funcDef(b, &clone); err != nil {
			return err
		}
		g.decorated[fd.Name] = true
		return nil
	}

	n := len(fd.Params)
	fty := "i32"
	for i := 0; i < n; i++ {
		fty += ", i32"
	}
	// @f_impl define (the decorated body). The name carries irSymbolPrefix, as every
	// program-owned function symbol does (Gap R.4), so that this define, the pointer
	// global below and every call through resolveDecorators agree on one symbol — the
	// wrapping branch above gets the prefix for free by emitting through funcDef.
	g.dbgDefine(b, irSymbol(fd.Name)+"_impl", fd.Name+"_impl", fd.Src)
	fmt.Fprintf(b, "define internal i32 @%s_impl(", irSymbol(fd.Name))
	for i := range fd.Params {
		if i > 0 {
			fmt.Fprintf(b, ", ")
		}
		fmt.Fprintf(b, "i32 %%p%d", i)
	}
	fmt.Fprintf(b, ") {\n")
	savedFrame := g.frameOpen
	g.gcOpenFrame(b)
	g.params = map[string]string{}
	g.paramSlot = nil
	for i, p := range fd.Params {
		g.params[p.Name] = fmt.Sprintf("%%p%d", i)
	}
	g.copyInReboundParams(b, fd, false)
	for _, st := range fd.Body {
		g.stmt(b, st)
	}
	g.gcCloseFrame(b)
	fmt.Fprintf(b, "  ret i32 0\n}\n")
	g.frameOpen = savedFrame
	// @f_ptr global fnptr initialized to @f_impl
	finalLabel, err := g.resolveDecorators(fd)
	if err != nil {
		return err
	}
	fmt.Fprintf(&g.globals, "@%s_ptr = internal global i32(%s)* %s\n", fd.Name, repeatParamTypes(n), finalLabel)
	// @f_apply()
	g.dbgDefine(&g.globals, fd.Name+"_apply", fd.Name+"_apply", fd.Src)
	fmt.Fprintf(&g.globals, "define internal void @%s_apply() {\n", fd.Name)
	fmt.Fprintf(&g.globals, "  store i32(%s)* %s, i32(%s)* @%s_ptr\n", repeatParamTypes(n), finalLabel, repeatParamTypes(n), fd.Name)
	fmt.Fprintf(&g.globals, "  ret void\n}\n")
	g.applyCalls = append(g.applyCalls, "@"+fd.Name+"_apply")
	g.decorated[fd.Name] = true
	return nil
}

// repeatParamTypes returns the i32 param type list for an n-param function
// signature ("i32, " repeated n-1 times); empty for 0/1 params (no negative repeat).
func repeatParamTypes(n int) string {
	if n <= 1 {
		return ""
	}
	return strings.Repeat("i32, ", n-1)
}

// resolveDecorators resolves the decorated function value for a FuncDef with
// decorators, applying decorators in source order (matching the record:
// @dec1 @dec2 def f == f = dec2(dec1(f))). AOT currently supports identity
// decorators (`def dec(g): return g`); wrapping/transform decorators that call
// or transform the decorated function are rejected with a clear codegen error
// instead of being silently ignored.
func (g *irGen) resolveDecorators(fd *FuncDef) (string, error) {
	// The decorated body is emitted through funcDef, which emits program-defined names
	// under irSymbolPrefix (Gap R.4); every reference to it has to carry the prefix too,
	// which is why this label is built through irSymbol rather than by concatenation.
	label := "@" + irSymbol(fd.Name+"_impl")
	for _, dec := range fd.Decorators {
		n, ok := dec.(*Name)
		if !ok {
			return "", fmt.Errorf("codegen: unsupported decorator expression on %q (only @name decorators are supported in AOT)", fd.Name)
		}
		decDef := g.fds[n.Value]
		if decDef == nil {
			return "", fmt.Errorf("codegen: cannot resolve decorator %q for %q", n.Value, fd.Name)
		}
		// identity decorator: `def dec(g): return g`
		if len(decDef.Body) == 1 && len(decDef.Params) == 1 {
			if rs, ok := decDef.Body[0].(*ReturnStmt); ok {
				if n2, ok := rs.Expr.(*Name); ok && n2.Value == decDef.Params[0].Name {
					continue
				}
			}
		}
		return "", fmt.Errorf("codegen: decorator %q for %q is not an identity decorator (wrapping/transform decorators are not yet supported in AOT)", n.Value, fd.Name)
	}
	return label, nil
}
