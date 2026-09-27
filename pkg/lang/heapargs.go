package lang

import (
	"fmt"
	"sort"
	"strings"
)

// heapargs.go — AOT heap containers across function boundaries.
//
// In the LLVM AOT backend a list, dict or set is an *i32 handle* into the
// runtime heap (`rt_alloc`/`rt_set_elem`/`rt_dict_put`/`rt_set_add`). Getting
// such a value across a function boundary needs two things that a plain
// expression lowering does not give you:
//
//   - at the call site a container *literal* must be materialised into the heap
//     and the handle passed, instead of lowering the literal to its
//     compile-time global (`@.lst1`) — passing that global as `i32` is rejected
//     by LLVM's verifier with "global variable reference must have pointer
//     type"; and
//   - in the callee the parameter must be *known* to hold a handle, so
//     `for x in xs`, `len(xs)` and `xs[i]` use `rt_list_len`/`rt_get_elem`
//     instead of treating the handle as an integer (which silently miscompiles
//     `for x in xs` into a 0..handle range loop).
//
// heapArgKinds infers, per function parameter, which runtime container kind
// flows into it. Sources of knowledge, in priority order:
//
//  1. a container annotation on the parameter (`list[T]`, `set[T]`,
//     `dict[K, V]`, `Sequence[T]`, `Iterator[T]`); and
//  2. every call site in the module — a literal argument, a variable that is
//     assigned a container somewhere, a comprehension, or a generator call all
//     witness the parameter's kind.
//
// The result is a pure, deterministic analysis over the AST: it emits nothing,
// so it can run before any IR is produced.
//
// The kinds mirror the runtime heap tags understood by `rt_alloc`.
const (
	HeapNone = 0 // not a runtime container
	HeapList = 1 // rt_alloc(i32 1)
	HeapDict = 2 // rt_alloc(i32 2)
	HeapSet  = 3 // rt_alloc(i32 3)
	HeapInst = 4 // rt_alloc(i32 4) (class instance; not inferred here)
)

// HeapKindName names a runtime container kind (used in diagnostics and JSON).
func HeapKindName(k int) string {
	switch k {
	case HeapList:
		return "list"
	case HeapDict:
		return "dict"
	case HeapSet:
		return "set"
	case HeapInst:
		return "instance"
	default:
		return "none"
	}
}

// heapKindOfAnnotation maps a parameter annotation to its runtime container
// kind (0 when the annotation is not a container).
func heapKindOfAnnotation(t *Type) int {
	if t == nil {
		return HeapNone
	}
	switch t.Kind {
	case KindList, KindSequence, KindIterator:
		return HeapList
	case KindDict:
		return HeapDict
	case KindSet:
		return HeapSet
	}
	return HeapNone
}

// heapASTWalker walks every statement and expression of a program, reporting
// calls to named functions and container assignments to named variables.
type heapASTWalker struct {
	// scope is the name of the function whose body is being walked ("" for module level). It
	// is threaded through the callbacks so a variable's kind can be resolved per function:
	// `def ins(s, v): s.add(v)` and `def echo(s): return s` both have a parameter called `s`,
	// and a name-keyed table let the first one's set-ness leak onto the second's string (the
	// call `echo(s)` then looked like it passed a set).
	scope  string
	call   func(scope, fn string, args []Expr)
	assign func(scope, target string, value Expr, annot *Type)
}

func (w heapASTWalker) stmts(ss []Stmt) {
	for _, s := range ss {
		w.stmt(s)
	}
}

func (w heapASTWalker) exprs(es []Expr) {
	for _, e := range es {
		w.expr(e)
	}
}

func (w heapASTWalker) stmt(s Stmt) {
	switch n := s.(type) {
	case *ExprStmt:
		w.expr(n.Expr)
	case *AssignStmt:
		if nm, ok := n.Target.(*Name); ok && w.assign != nil {
			w.assign(w.scope, nm.Value, n.Value, n.Annot)
		}
		w.expr(n.Value)
	case *AugAssignStmt:
		w.expr(n.Target)
		w.expr(n.Value)
	case *ReturnStmt:
		w.expr(n.Expr)
	case *RaiseStmt:
		w.expr(n.Expr)
	case *IfStmt:
		w.expr(n.Cond)
		w.stmts(n.Then)
		for _, e := range n.Elifs {
			w.stmt(e)
		}
		w.stmts(n.Else)
	case *WhileStmt:
		w.expr(n.Cond)
		w.stmts(n.Body)
		w.stmts(n.Else)
	case *ForStmt:
		w.expr(n.Iter)
		w.stmts(n.Body)
		w.stmts(n.Else)
	case *MatchStmt:
		w.expr(n.Subject)
		for _, c := range n.Cases {
			w.expr(c.Guard)
			w.stmts(c.Body)
		}
	case *TryStmt:
		w.stmts(n.Body)
		for _, e := range n.Excepts {
			w.stmts(e.Body)
		}
		w.stmts(n.Finally)
	case *WithStmt:
		w.expr(n.Expr)
		w.stmts(n.Body)
	case *YieldStmt:
		w.expr(n.Expr)
	case *YieldFromStmt:
		w.expr(n.Expr)
	case *FuncDef:
		for _, d := range n.Decorators {
			w.expr(d)
		}
		for _, p := range n.Params {
			w.expr(p.Default)
		}
		// The body is a fresh variable scope: a copy of the walker carries the function name
		// down, so names inside do not describe the module-level ones (or another function's).
		inner := w
		inner.scope = n.Name
		inner.stmts(n.Body)
	case *ClassDef:
		w.stmts(n.Body)
	}
}

func (w heapASTWalker) expr(e Expr) {
	switch n := e.(type) {
	case nil:
	case *Call:
		if nm, ok := n.Fn.(*Name); ok && w.call != nil {
			w.call(w.scope, nm.Value, n.Args)
		}
		w.expr(n.Fn)
		for _, a := range n.Args {
			if kw, ok := a.(*KeywordArg); ok {
				w.expr(kw.Value)
				continue
			}
			w.expr(a)
		}
	case *Name:
	case *IntLit, *FloatLit, *BoolLit, *NoneLit, *StrLit:
	case *FString:
		for _, p := range n.Parts {
			w.expr(p.Expr)
		}
	case *Tuple:
		w.exprs(n.Elems)
	case *ListLit:
		w.exprs(n.Elems)
	case *SetLit:
		w.exprs(n.Elems)
	case *DictLit:
		w.exprs(n.Keys)
		w.exprs(n.Vals)
	case *BinOp:
		w.expr(n.L)
		w.expr(n.R)
	case *UnOp:
		w.expr(n.X)
	case *CondExpr:
		w.expr(n.Cond)
		w.expr(n.If)
		w.expr(n.Else)
	case *AssignExpr:
		w.expr(n.Value)
	case *Index:
		w.expr(n.Obj)
		w.expr(n.Idx)
	case *Slice:
		w.expr(n.Obj)
		w.expr(n.Low)
		w.expr(n.High)
		w.expr(n.Step)
	case *Attr:
		w.expr(n.Obj)
	case *Lambda:
		for _, p := range n.Params {
			w.expr(p.Default)
		}
		w.expr(n.Body)
	case *Comp:
		w.exprs(n.Elems)
		w.exprs(n.Keys)
		w.exprs(n.Vals)
		w.expr(n.Iter)
		w.expr(n.Cond)
	case *Generator:
		w.exprs(n.Elems)
		w.expr(n.Iter)
		w.expr(n.Cond)
	case *AwaitExpr:
		w.expr(n.Expr)
	}
}

// heapArgKinds returns, per function name, the runtime container kind of each
// parameter index. It is a whole-module inference: a parameter is a container
// if its annotation says so, or if any call site in the module passes one.
func heapArgKinds(prog *Program) map[string]map[int]int {
	if prog == nil {
		return nil
	}
	funcs := map[string]*FuncDef{}
	for _, st := range prog.Stmts {
		if fd, ok := st.(*FuncDef); ok {
			funcs[fd.Name] = fd
		}
	}
	if len(funcs) == 0 {
		return nil
	}
	// Which functions produce a heap list (generators return a handle)?
	genFuncs := map[string]bool{}
	for name, fd := range funcs {
		if containsYield(fd.Body) {
			genFuncs[name] = true
		}
	}

	w := heapASTWalker{}

	// Pass 1: which variables hold a runtime container? A name that is ever
	// assigned a plain (non-container) value is excluded from propagation, so a
	// list parameter named `xs` cannot drag an unrelated scalar `xs` along.
	varKinds := map[string]int{}
	nonContainer := map[string]bool{}
	w.assign = func(scope, target string, value Expr, annot *Type) {
		key := scopeKey(scope, target)
		if k := heapKindOfAnnotation(annot); k != HeapNone {
			varKinds[key] = k
			return
		}
		if k := heapKindOfExpr(value, varKinds, genFuncs, scope); k != HeapNone {
			varKinds[key] = k
			return
		}
		nonContainer[key] = true
	}
	w.call = nil
	w.stmts(prog.Stmts)

	out := map[string]map[int]int{}
	mark := func(fn string, idx int, kind int) {
		if kind == HeapNone || idx < 0 {
			return
		}
		m := out[fn]
		if m == nil {
			m = map[int]int{}
			out[fn] = m
		}
		m[idx] = kind
	}
	w.call = func(scope, fn string, args []Expr) {
		fd, ok := funcs[fn]
		if !ok {
			return
		}
		pos := 0
		for _, a := range args {
			if kw, ok := a.(*KeywordArg); ok {
				for i, p := range fd.Params {
					if p.Name == kw.Name {
						mark(fn, i, heapKindOfExpr(kw.Value, varKinds, genFuncs, scope))
					}
				}
				continue
			}
			mark(fn, pos, heapKindOfExpr(a, varKinds, genFuncs, scope))
			pos++
		}
	}
	seed := func() {
		// Annotations and container defaults are authoritative.
		for name, fd := range funcs {
			for i, p := range fd.Params {
				if p == nil || i >= len(fd.Params) {
					continue
				}
				if k := heapKindOfAnnotation(p.Annot); k != HeapNone {
					mark(name, i, k)
				} else if k := heapKindOfExpr(p.Default, varKinds, genFuncs, name); k != HeapNone {
					mark(name, i, k)
				}
			}
		}
		w.stmts(prog.Stmts)
	}
	// Fixed point: a call may forward a container it received itself
	// (`def doubled(xs): return total(xs)`), so a parameter that is known to be
	// a container must also make the *variable* `xs` a container. Re-classify
	// until nothing changes; a handful of rounds covers any real call chain.
	// The merge order is fixed (functions, then parameter indices) so two
	// functions whose same-named parameters disagree cannot classify
	// nondeterministically from Go map iteration order.
	for pass := 0; pass < 8; pass++ {
		fns := make([]string, 0, len(out))
		for fn := range out {
			fns = append(fns, fn)
		}
		sort.Strings(fns)
		for _, fn := range fns {
			fd := funcs[fn]
			if fd == nil {
				continue
			}
			m := out[fn]
			idxs := make([]int, 0, len(m))
			for i := range m {
				idxs = append(idxs, i)
			}
			sort.Ints(idxs)
			for _, i := range idxs {
				if i >= len(fd.Params) || nonContainer[scopeKey(fn, fd.Params[i].Name)] {
					continue
				}
				// A container parameter makes the *variable* inside this function a container;
				// recording it under the function's own scope keeps a same-named parameter in
				// another function unaffected.
				varKinds[scopeKey(fn, fd.Params[i].Name)] = m[i]
			}
		}
		before := snapshotKinds(out)
		seed()
		if snapshotKinds(out) == before {
			break
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// snapshotKinds renders the inference result as a stable string so the fixed
// point can detect "nothing changed".
func snapshotKinds(m map[string]map[int]int) string {
	fns := make([]string, 0, len(m))
	for k := range m {
		fns = append(fns, k)
	}
	sort.Strings(fns)
	var sb strings.Builder
	for _, fn := range fns {
		idxs := make([]int, 0, len(m[fn]))
		for i := range m[fn] {
			idxs = append(idxs, i)
		}
		sort.Ints(idxs)
		for _, i := range idxs {
			fmt.Fprintf(&sb, "%s/%d=%d;", fn, i, m[fn][i])
		}
	}
	return sb.String()
}

// heapKindOfExpr classifies an expression as a runtime container value.
func heapKindOfExpr(e Expr, varKinds map[string]int, genFuncs map[string]bool, scope string) int {
	switch n := e.(type) {
	case *ListLit:
		return HeapList
	case *SetLit:
		return HeapSet
	case *DictLit:
		return HeapDict
	case *Comp:
		switch n.Kind {
		case CompList:
			return HeapList
		case CompSet:
			return HeapSet
		case CompDict:
			return HeapDict
		case CompGenerator:
			return HeapList
		}
	case *Generator:
		// Generator expressions are evaluated eagerly into a heap list.
		return HeapList
	case *Name:
		return lookupVarKind(varKinds, scope, n.Value)
	case *Call:
		if nm, ok := n.Fn.(*Name); ok {
			if genFuncs[nm.Value] {
				return HeapList
			}
			// `t = set()` is a call, not a SetLit, and the codegen does track that variable as
			// a runtime set — so the parameter inference has to know it too, or passing `t` to
			// a helper leaves the parameter unregistered and `s.add(1)` inside the callee is
			// mistaken for a string method.
			switch nm.Value {
			case "set":
				return HeapSet
			case "list":
				return HeapList
			case "dict":
				return HeapDict
			}
		}
	case *CondExpr:
		a := heapKindOfExpr(n.If, varKinds, genFuncs, scope)
		if b := heapKindOfExpr(n.Else, varKinds, genFuncs, scope); b != HeapNone && b == a {
			return a
		}
		return a
	case *Tuple:
		// Tuples are compile-time arrays in the AOT backend, not heap objects.
		return HeapNone
	}
	return HeapNone
}

// heapLiteralKind classifies only container *literal* nodes. Comprehensions,
// generator expressions and variables already lower to a heap handle through
// the ordinary expression path, so the call site must not re-materialise them.
func heapLiteralKind(e Expr) int {
	switch n := e.(type) {
	case *ListLit:
		return HeapList
	case *SetLit:
		return HeapSet
	case *DictLit:
		return HeapDict
	case *Name:
		// a variable already holds a handle
		_ = n
	}
	return HeapNone
}

// --- codegen integration ---------------------------------------------------

// paramHeapKind reports the runtime container kind expected for parameter idx
// of function fn. An explicit annotation wins; otherwise the whole-module
// call-site inference decides.
func (g *irGen) paramHeapKind(fn string, idx int, p *Param) int {
	if p != nil {
		if k := heapKindOfAnnotation(p.Annot); k != HeapNone {
			return k
		}
	}
	if m := g.heapArgs[fn]; m != nil {
		return m[idx]
	}
	return HeapNone
}

// declareHeapParams registers fn's container-typed parameters as heap-backed
// variables: the SSA register holding parameter i is stored into its %_name
// slot (rooted for the GC), so `for x in xs`, `len(xs)`, `xs[i]` and `print(xs)`
// all read the runtime handle instead of treating it as an integer.
// reg(i) returns the SSA register holding parameter i.
//
// The registration is *scoped to this body*: the returned undo restores the
// variable-kind maps, so a parameter named `xs` in one function can never make
// an unrelated `xs` elsewhere look like a container. Call it as
// `defer g.declareHeapParams(...)()`.
func (g *irGen) declareHeapParams(b *strings.Builder, fn string, fd *FuncDef, reg func(int) string) func() {
	type binding struct {
		which                              int // 0 list, 1 dict, 2 set
		name                               string
		wasList, wasDict, wasSet, wasAlloc bool
	}
	var bindings []binding
	for i, p := range fd.Params {
		if p == nil {
			continue
		}
		k := g.paramHeapKind(fn, i, p)
		if k == HeapNone {
			continue
		}
		g.heapUsed = true
		bd := binding{which: 0, name: p.Name, wasList: g.listVars[p.Name], wasDict: g.runtimeDicts[p.Name], wasSet: g.runtimeSets[p.Name], wasAlloc: g.allocd[p.Name]}
		switch k {
		case HeapDict:
			bd.which = 1
			g.runtimeDicts[p.Name] = true
		case HeapSet:
			bd.which = 2
			g.runtimeSets[p.Name] = true
		default:
			g.listVars[p.Name] = true
		}
		if !g.allocd[p.Name] {
			fmt.Fprintf(b, "  %%%s = alloca i32\n", "_"+p.Name)
			fmt.Fprintf(b, "  store i32 %s, i32* %%%s\n", reg(i), "_"+p.Name)
			g.allocd[p.Name] = true
			g.gcRegKey(b, fn+"."+p.Name, p.Name)
		}
		bindings = append(bindings, bd)
	}
	return func() {
		for _, bd := range bindings {
			g.listVars[bd.name] = bd.wasList
			g.runtimeDicts[bd.name] = bd.wasDict
			g.runtimeSets[bd.name] = bd.wasSet
			g.allocd[bd.name] = bd.wasAlloc
		}
	}
}

// heapArg materialises a container *literal* argument into the runtime heap and
// returns its handle. ok is false when the argument is not a container literal
// (variables already hold handles and are lowered normally).
func (g *irGen) heapArg(b *strings.Builder, e Expr) (handle string, ok bool, err error) {
	k := heapLiteralKind(e)
	if k == HeapNone {
		return "", false, nil
	}
	g.heapUsed = true
	switch n := e.(type) {
	case *ListLit:
		h, err := g.heapListFrom(b, n, "")
		if err != nil {
			return "", true, err
		}
		return h, true, nil
	}
	h := g.newTmp()
	fmt.Fprintf(b, "  %s = call i32 @rt_alloc(i32 %d)\n", h, k)
	switch n := e.(type) {
	case *SetLit:
		for _, el := range n.Elems {
			v, e := g.heapElem(b, el)
			if e != nil {
				return "", true, e
			}
			fmt.Fprintf(b, "  call void @rt_set_add(i32 %s, i32 %s)\n", h, v)
		}
	case *DictLit:
		for i := range n.Keys {
			kk, e := g.heapElem(b, n.Keys[i])
			if e != nil {
				return "", true, e
			}
			vv, e := g.heapElem(b, n.Vals[i])
			if e != nil {
				return "", true, e
			}
			fmt.Fprintf(b, "  call void @rt_dict_put(i32 %s, i32 %s, i32 %s)\n", h, kk, vv)
		}
	}
	return h, true, nil
}

// heapElem lowers one container element. Strings are `i8*` globals in this
// backend and the heap stores i32 slots, so a string element would produce IR
// the verifier rejects — report it as the unsupported case it is instead.
func (g *irGen) heapElem(b *strings.Builder, e Expr) (string, error) {
	v, _, err := g.heapElemKind(b, e)
	return v, err
}

// heapElemKind lowers a value for a heap container slot and reports whether it became an
// interned string. Strings are compile-time globals in this backend, so a slot stores the
// index into @str_tab instead of the pointer: `rt_set_elem(i32 %h, i32 0, i32 @.str1)` put a
// global in an i32 parameter, which LLVM rejected and the exit-code contract then called a
// compiler bug (roadmap Gap I.2). A string the backend cannot resolve to text still refuses
// with the actionable diagnostic rather than emitting bad IR (ADR 0166).
func (g *irGen) heapElemKind(b *strings.Builder, e Expr) (string, bool, error) {
	if txt, ok := g.stringVal(e); ok {
		g.heapUsed = true
		t := g.newTmp()
		// Intern the text and its Python repr together: containers store the index, and the
		// printer picks the raw or the repr slot depending on context (Gap I.2).
		fmt.Fprintf(b, "  %s = call i32 @rt_str_intern2(i8* %s, i8* %s)\n", t, g.strConst(txt), g.strConst(pyReprString(txt)))
		return t, true, nil
	}
	if nm, ok := e.(*Name); ok && g.internedVars[nm.Value] {
		v, err := g.value(b, e) // already an index into the string table
		return v, true, err
	}
	if c, ok := e.(*Call); ok {
		// make_key() returns an index already: re-interning would look up an i32 as text.
		if nm, ok2 := c.Fn.(*Name); ok2 && g.strFuncs[nm.Value] {
			v, err := g.value(b, e)
			return v, true, err
		}
	}
	if err := g.rejectRuntimeString(e, "container element", "store"); err != nil {
		return "", false, err
	}
	v, err := g.value(b, e)
	return v, false, err
}

// heapListFrom materialises a list literal as a runtime heap list and returns
// its handle. Elements are lowered as i32 values; a string element is reported
// as unsupported rather than emitting IR the verifier rejects.
func (g *irGen) heapListFrom(b *strings.Builder, ln *ListLit, name string) (string, error) {
	g.heapUsed = true
	h := g.newTmp()
	fmt.Fprintf(b, "  %s = call i32 @rt_alloc(i32 %d)\n", h, HeapList)
	for i, el := range ln.Elems {
		v, interned, err := g.heapElemKind(b, el)
		if err != nil {
			return "", err
		}
		if interned && name != "" {
			g.listElemStr[name] = true
		}
		fmt.Fprintf(b, "  call void @rt_set_elem(i32 %s, i32 %d, i32 %s)\n", h, i, v)
	}
	return h, nil
}

// emitModuleContainerList gives a module-level container variable the one thing
// the per-function parameter path cannot give it: a slot in main's entry block
// plus a GC root. Without it, a container defined at module scope and later
// mutated (`xs = []` then `xs.append(i)`) has no `%_xs` for the mutation path to
// store into, and rt_gc never sees the handle, so a collection could recycle a
// live container. `decl` is the alloca name (`%_xs`).
func (g *irGen) emitModuleContainerList(b *strings.Builder, name string, decl string) {
	if name == "" || g.allocd[name] {
		return
	}
	g.heapUsed = true
	g.listVars[name] = true
	// The slot is brand new: its previous "binding" is the zero store below, so the
	// assignment must not try to release it (that would free heap slot 0).
	if g.freshSlots == nil {
		g.freshSlots = map[string]bool{}
	}
	g.freshSlots[name] = true
	b.WriteString(fmt.Sprintf("  %s = alloca i32\n", decl))
	b.WriteString(fmt.Sprintf("  store i32 0, i32* %s\n", decl))
	g.gcRegKey(b, "main."+name, name)
	g.allocd[name] = true
}

// scopeKey qualifies a variable name with the function whose body it belongs to ("" is module
// level), so per-function inference cannot describe an unrelated same-named variable.
func scopeKey(scope, name string) string {
	return scope + "\x1f" + name
}

// lookupVarKind resolves a variable's container kind: the enclosing function's own binding
// wins, then the module-level one.
func lookupVarKind(varKinds map[string]int, scope, name string) int {
	if scope != "" {
		if k, ok := varKinds[scopeKey(scope, name)]; ok {
			return k
		}
	}
	return varKinds[scopeKey("", name)]
}

// applyStrFill transfers a callee's "this container parameter holds strings in these
// positions" fact onto the argument the caller passed (roadmap Gap J.5). The callee knows what
// it stores; only the caller knows what the container prints as later.
func (g *irGen) applyStrFill(fn string, idx int, arg Expr) {
	if idx < 0 {
		return
	}
	bits := g.strFillOf[fn][idx]
	if bits == 0 {
		return
	}
	nm, ok := arg.(*Name)
	if !ok {
		return
	}
	g.heapUsed = true
	if bits&EstrListElem != 0 && g.listVars[nm.Value] {
		g.listElemStr[nm.Value] = true
	}
	if bits&EstrSetMember != 0 && g.runtimeSets[nm.Value] {
		g.setElemStr[nm.Value] = true
	}
	if !g.runtimeDicts[nm.Value] {
		return
	}
	if bits&EstrDictKey != 0 {
		g.dictKeyStr[nm.Value] = true
	}
	if bits&EstrDictValue != 0 {
		g.dictValStr[nm.Value] = true
	}
}
