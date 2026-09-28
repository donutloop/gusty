package lang

import (
	"fmt"
	"sort"
	"strconv"
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
// The kinds are the canonical runtime heap kinds, declared once in value.go as the
// projection of the ValueTag table; they are aliased here because the inference below
// speaks in container kinds.
const (
	HeapNone = HeapKindNone     // not a runtime container
	HeapList = HeapKindList     // rt_alloc(i32 1)
	HeapDict = HeapKindDict     // rt_alloc(i32 2)
	HeapSet  = HeapKindSet      // rt_alloc(i32 3)
	HeapInst = HeapKindInstance // rt_alloc(i32 4) (class instance; not inferred here)
)

// HeapKindName names a runtime container kind (used in diagnostics and JSON). The name
// comes from the canonical tag table, so a kind is not "set" in one report and "dict"
// in another.
func HeapKindName(k int) string { return HeapKindNameOf(int32(k)) }

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
	if g.nestedContainerElem(e) {
		return "", false, nestedContainerErr(e)
	}
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
// heapListFromTagged builds a heap list whose elements carry per-element tags: one
// rt_tag_elem per slot, with the canonical ValueTag number for what the element is. The
// container-wide @estr[h] flag is deliberately left alone -- that flag is the thing a tag
// replaces, and setting it would make rt_print_list render every element as a string.
func (g *irGen) heapListFromTagged(b *strings.Builder, ln *ListLit) (string, error) {
	g.heapUsed = true
	h := g.newTmp()
	fmt.Fprintf(b, "  %s = call i32 @rt_alloc(i32 %d)\n", h, HeapList)
	for i, el := range ln.Elems {
		v, interned, err := g.heapElemKind(b, el)
		if err != nil {
			return "", err
		}
		tag := elemTagFor(el, interned)
		if tag == int32(TagNone) {
			v = "0"
		}
		fmt.Fprintf(b, "  call void @rt_tag_elem(i32 %s, i32 %d, i32 %d)\n", h, i, tag)
		fmt.Fprintf(b, "  call void @rt_set_elem(i32 %s, i32 %d, i32 %s)\n", h, i, v)
	}
	return h, nil
}

func (g *irGen) heapListFrom(b *strings.Builder, ln *ListLit, name string) (string, error) {
	g.heapUsed = true
	h := g.newTmp()
	fmt.Fprintf(b, "  %s = call i32 @rt_alloc(i32 %d)\n", h, HeapList)
	bits := 0
	for i, el := range ln.Elems {
		v, interned, err := g.heapElemKind(b, el)
		if err != nil {
			return "", err
		}
		if interned {
			bits |= 1
			if name != "" {
				g.listElemStr[name] = true
			}
		}
		fmt.Fprintf(b, "  call void @rt_set_elem(i32 %s, i32 %d, i32 %s)\n", h, i, v)
	}
	if bits != 0 {
		fmt.Fprintf(b, "  call void @rt_mark_estr(i32 %s, i32 %d)\n", h, bits)
	}
	return h, nil
}

// heapDictFrom builds a heap dict from a literal, interning any string keys or values and
// recording on the object which positions hold interned strings (rt_mark_estr), so
// {"a": 1} and {1: "v"} build and print like the interpreter renders them (Gap J.6).
func (g *irGen) heapDictFrom(b *strings.Builder, dl *DictLit, name string) (string, error) {
	g.heapUsed = true
	h := g.newTmp()
	fmt.Fprintf(b, "  %s = call i32 @rt_alloc(i32 %d)\n", h, HeapDict)
	bits := 0
	for i := range dl.Keys {
		kk, kIsStr, err := g.heapElemKind(b, dl.Keys[i])
		if err != nil {
			return "", err
		}
		vv, vIsStr, err := g.heapElemKind(b, dl.Vals[i])
		if err != nil {
			return "", err
		}
		if kIsStr {
			bits |= 2
			if name != "" {
				g.dictKeyStr[name] = true
			}
		}
		if vIsStr {
			bits |= 4
			if name != "" {
				g.dictValStr[name] = true
			}
		}
		fmt.Fprintf(b, "  call void @rt_dict_put(i32 %s, i32 %s, i32 %s)\n", h, kk, vv)
	}
	if bits != 0 {
		fmt.Fprintf(b, "  call void @rt_mark_estr(i32 %s, i32 %d)\n", h, bits)
	}
	return h, nil
}

// heapSetFrom builds a heap set from a literal, interning string members.
func (g *irGen) heapSetFrom(b *strings.Builder, sl *SetLit, name string) (string, error) {
	g.heapUsed = true
	h := g.newTmp()
	fmt.Fprintf(b, "  %s = call i32 @rt_alloc(i32 %d)\n", h, HeapSet)
	bits := 0
	for _, el := range sl.Elems {
		v, interned, err := g.heapElemKind(b, el)
		if err != nil {
			return "", err
		}
		if interned {
			bits |= 1
			if name != "" {
				g.setElemStr[name] = true
			}
		}
		fmt.Fprintf(b, "  call void @rt_set_add(i32 %s, i32 %s)\n", h, v)
	}
	if bits != 0 {
		fmt.Fprintf(b, "  call void @rt_mark_estr(i32 %s, i32 %d)\n", h, bits)
	}
	return h, nil
}

// literalNeedsHeap reports whether a container literal cannot be a compile-time global struct
// because some element is a string: the static {i32, [n x i32]} layout has no representation
// for one, so the heap path (which interns) must build it instead (roadmap Gap J.6).
// isContainerLiteral reports the three literal forms that have a runtime printer. Print
// position asks a different question than storage: an all-int list literal has a static global
// layout and needs no heap slot, but that layout is not an i32, and handing @.lstN to
// printf("%d") is a module llc refuses. Anything with a printer gets the printer (Gap J.6).
func isContainerLiteral(e Expr) bool {
	switch e.(type) {
	case *ListLit, *SetLit, *DictLit:
		return true
	}
	return false
}

// emptyContainerLiteral maps a zero-argument `set()` / `list()` / `dict()` to the empty literal
// it means, so print position recognises it. The empty *set* has no literal spelling at all, so
// `set()` is the only way to write one (Gap K.3) — and until now `print(set())` printed the
// freshly allocated handle, `0`, because the print gate asked for a literal.
func emptyContainerLiteral(e Expr) (Expr, bool) {
	c, ok := e.(*Call)
	if !ok || len(c.Args) != 0 {
		return nil, false
	}
	switch calleeName(c) {
	case "set":
		return &SetLit{}, true
	case "list":
		return &ListLit{}, true
	case "dict":
		return &DictLit{}, true
	}
	return nil, false
}

// nestedContainerElem reports an element that is itself a container: a literal inside a literal,
// or a container variable inside another one. The compiled backend cannot store those — the
// collector marks a container by the variable slot that holds it (ADR 0181), and an element that
// is a handle has no slot to be marked from — and the static layout would put `@.lstN` in a value
// position, which is a module llc refuses. Refusing is what keeps `print([[1], [2]])` from
// answering `[1, 2]`: the interned indices of the inner strings, rendered as numbers
// (roadmap L11.1, ADR 0188).
func (g *irGen) nestedContainerElem(e Expr) bool {
	switch n := e.(type) {
	case *ListLit, *SetLit, *DictLit:
		return true
	case *Name:
		return g.listVars[n.Value] || g.mixedLists[n.Value] || g.runtimeDicts[n.Value] || g.runtimeSets[n.Value]
	}
	return false
}

func nestedContainerErr(e Expr) error {
	return fmt.Errorf("codegen: a compiled container cannot hold another container yet; an element that is a handle is not marked by the collector (only containers bound to a variable are), so nested contents need the element-tagging work (roadmap L11.1, ADR 0188). Build the inner container separately and index it, or run it interpreted")
}

func literalNeedsHeap(e Expr) bool {
	switch n := e.(type) {
	case *ListLit:
		for _, el := range n.Elems {
			if isStringExpr(el) {
				return true
			}
		}
	case *SetLit:
		for _, el := range n.Elems {
			if isStringExpr(el) {
				return true
			}
		}
	case *DictLit:
		for _, k := range n.Keys {
			if isStringExpr(k) {
				return true
			}
		}
		for _, v := range n.Vals {
			if isStringExpr(v) {
				return true
			}
		}
	}
	return false
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

// literalMixedKinds reports a container literal that holds both strings and non-strings.
// A compiled container's slots are i32 words and the element kind is recorded once per
// container (or once per dict side), so a heterogeneous list would print its integers through
// the string table — `[1, "a"]` rendered as [(null), 'a']. Misprinting is worse than refusing,
// so this is a compile diagnostic (ADR 0166); Python's heterogeneous lists need per-element
// tagging, which is a representation change rather than a printer fix.
func literalMixedKinds(e Expr) bool {
	anyStr, anyOther := false, false
	classify := func(x Expr) {
		if _, ok := stringConstOf(x); ok {
			anyStr = true
			return
		}
		anyOther = true
	}
	switch n := e.(type) {
	case *ListLit:
		for _, el := range n.Elems {
			classify(el)
		}
	case *SetLit:
		for _, el := range n.Elems {
			classify(el)
		}
	case *DictLit:
		keyStr, keyOther, valStr, valOther := false, false, false, false
		for _, k := range n.Keys {
			if _, ok := stringConstOf(k); ok {
				keyStr = true
			} else {
				keyOther = true
			}
		}
		for _, v := range n.Vals {
			if _, ok := stringConstOf(v); ok {
				valStr = true
			} else {
				valOther = true
			}
		}
		return (keyStr && keyOther) || (valStr && valOther)
	}
	return anyStr && anyOther
}

// stringConstOf resolves a compile-time-known string (literal, interpolated literal, or a
// folded string expression) — the same question heapElemKind asks before interning.
func stringConstOf(e Expr) (string, bool) {
	if sl, ok := e.(*StrLit); ok {
		return sl.Value, true
	}
	if s, ok := stringConst(e); ok {
		return s, true
	}
	return "", false
}

// taggableMixedList reports whether a list literal mixes element kinds but every element is
// one the per-element tag can describe today: an integer, an interned string, or None. That
// set is deliberate. Bools are excluded because bools are not values in either backend yet
// (L11.1), so a [True] would print 1 and disagree with Python; floats are excluded because
// rt_print_mixed_value has no float rendering; and a nested container would have to be
// marked by the collector, which per-element rooting has not reached. Anything else keeps the
// honest refusal below rather than printing something wrong (ADR 0184).
func (g *irGen) taggableMixedList(ln *ListLit) bool {
	if len(ln.Elems) == 0 {
		return false
	}
	sawStr, sawOther := false, false
	for _, el := range ln.Elems {
		tag, ok := g.elemKindTag(el)
		if !ok {
			return false
		}
		if tag == int32(TagStr) {
			sawStr = true
		} else {
			sawOther = true
		}
	}
	return sawStr && sawOther
}

// elemKindTag is the codegen's answer to "what tag does this element's slot carry", and the
// gate on what a mixed list may hold at all. Excluded, each for a reason: bools (not values
// in either backend yet, so [True] would print 1 against Python's True -- L11.1), floats
// (rt_print_mixed_value has no float rendering), containers (the collector does not yet mark
// elements, so a nested heap object could be freed under a list that references it), and any
// expression whose string-ness the codegen cannot prove -- because printing an interned
// string's *index* as a number is exactly the wrong-output bug the refusal exists to avoid.
func (g *irGen) elemKindTag(e Expr) (int32, bool) {
	switch e.(type) {
	case *BoolLit, *FloatLit, *ListLit, *DictLit, *SetLit, *Tuple, *Lambda:
		return 0, false
	case *NoneLit:
		return int32(TagNone), true
	case *StrLit:
		return int32(TagStr), true
	case *IntLit:
		return int32(TagInt), true
	}
	if _, ok := stringConstOf(e); ok {
		return int32(TagStr), true
	}
	if g.printsAsInternedStr(e) {
		return int32(TagStr), true
	}
	if nm, ok := e.(*Name); ok {
		if g.strVals[nm.Value] != "" {
			return int32(TagStr), true
		}
	}
	return int32(TagInt), true
}

// elemTagFor is the canonical ValueTag number an element's slot carries. Numbers are 0
// (TagInt, which is also the array's zero value), None is TagNone, interned strings TagStr.
func elemTagFor(e Expr, interned bool) int32 {
	switch e.(type) {
	case *NoneLit:
		return int32(TagNone)
	}
	if interned {
		return int32(TagStr)
	}
	return int32(TagInt)
}

// mixedReadErr is what reading a single element out of a tagged list reports: the tag says
// what the element is, but the use site was compiled against one static kind.
func mixedReadErr(what string) error {
	return fmt.Errorf("codegen: a compiled %s holds elements of more than one kind, so one element is a (value, tag) pair; print(xs[i]) and v = xs[i] work because the tag travels with them, but this context needs a single static kind (roadmap L11.1, ADR 0187)", what)
}

// mixedTaggedVarErr is what a loop variable from a mixed list reports when the program
// wants a number from it: printing dispatches on the tag, but arithmetic and calls have no
// tag to carry (roadmap L11.1, ADR 0185).
func mixedTaggedVarErr(name string) error {
	return fmt.Errorf("codegen: %s comes from a loop over a mixed list; print(%s) works, but using it as a number needs a tagged value (roadmap L11.1)", name, name)
}

// mixedTaggedElemErr names the element that cannot be tagged, and why refusing is the only
// honest answer: without a tag the slot reads back as whatever the tag array happened to hold,
// which is how a stored string would print as its interned table index (ADR 0184/0187).
func mixedTaggedElemErr(e Expr) error {
	return fmt.Errorf("codegen: an element of a mixed compiled container must carry a tag, and %s has none the runtime can render (bools are not values yet, floats have no mixed-printer rendering, and a nested container is not marked by the collector) (roadmap L11.1, ADR 0187)", exprSnippet(e))
}

// exprSnippet is a short, stable rendering of an expression for a diagnostic.
func exprSnippet(e Expr) string {
	switch n := e.(type) {
	case *BoolLit:
		if n.Value {
			return "True"
		}
		return "False"
	case *FloatLit:
		return "a float literal"
	case *ListLit:
		return "a list literal"
	case *DictLit:
		return "a dict literal"
	case *SetLit:
		return "a set literal"
	case *Tuple:
		return "a tuple literal"
	case *Lambda:
		return "a lambda"
	case *Name:
		return n.Value
	}
	return fmt.Sprintf("%T", e)
}

// mixedElemTag lowers one element expression for a *tagged* container slot: the payload is
// exactly what the untagged path would store (a number, or an index into the interned string
// table), and the tag is the canonical ValueTag that says how to render it. The two halves are
// checked against each other, and a disagreement refuses: "the payload is an interned index but
// the tag says int" is precisely the wrong-answer shape the mixed-container refusal exists to
// prevent, so it is never emitted (ADR 0184, ADR 0187).
func (g *irGen) mixedElemTag(b *strings.Builder, e Expr) (payload, tag string, err error) {
	t, ok := g.elemKindTag(e)
	if !ok {
		return "", "", mixedTaggedElemErr(e)
	}
	v, interned, err := g.heapElemKind(b, e)
	if err != nil {
		return "", "", err
	}
	if interned && t != int32(TagStr) {
		return "", "", mixedTaggedElemErr(e)
	}
	if !interned && t == int32(TagStr) {
		return "", "", mixedTaggedElemErr(e)
	}
	if t == int32(TagNone) {
		// None has no i32 payload of its own; the tag is what renders it.
		v = "0"
	}
	return v, strconv.FormatInt(int64(t), 10), nil
}

// mixedElemPair emits the tagged *read* of one element: the payload through rt_get_elem and its
// tag through rt_tag_of, with the same bounds check every other element read gets. A loop over a
// mixed list already bound its variable this way (ADR 0185); this is the same pair at an
// arbitrary read site, which is what makes print(xs[i]) and `v = xs[i]` honest (ADR 0187).
func (g *irGen) mixedElemPair(b *strings.Builder, listName string, idx Expr, sp Span) (val, tag string, err error) {
	g.heapUsed = true
	g.heapSeq++
	hs := g.heapSeq
	h := fmt.Sprintf("%%h%d", hs)
	fmt.Fprintf(b, "  %s = load i32, i32* %%_%s\n", h, listName)
	idxOp, err := g.value(b, idx)
	if err != nil {
		return "", "", err
	}
	g.checkIndexRead(b, h, idxOp, sp)
	val = g.newTmp()
	fmt.Fprintf(b, "  %s = call i32 @rt_get_elem(i32 %s, i32 %s)\n", val, h, idxOp)
	tag = g.newTmp()
	fmt.Fprintf(b, "  %s = call i32 @rt_tag_of(i32 %s, i32 %s)\n", tag, h, idxOp)
	return val, tag, nil
}

// mixedIndexRead reports whether an `obj[i]` expression reads an element out of a tagged list
// variable, and returns that variable's name.
func (g *irGen) mixedIndexRead(ix *Index) (string, bool) {
	if ix == nil {
		return "", false
	}
	nm, ok := ix.Obj.(*Name)
	if !ok || !g.mixedLists[nm.Value] {
		return "", false
	}
	return nm.Value, true
}

// mixedKindErr is the diagnostic every mixed-container site reports.
func mixedKindErr(what string) error {
	return fmt.Errorf("codegen: a compiled %s holds either strings or numbers, not both; the interpreter allows mixing — a compiled container records one element kind, so heterogeneous contents need per-element tagging (roadmap Gap J.6)", what)
}

// recordElemKind notes that a container variable holds strings (isStr) or numbers in a given
// position, and refuses the case where it has already been told the opposite. The compiled
// container records one element kind per position, so mixing would print an integer through the
// string table — an honest diagnostic beats that (roadmap Gap J.6, ADR 0166).
func (g *irGen) recordElemKind(name, slot string, isStr bool) error {
	strMap, numMap, label := g.kindMapsFor(slot)
	if isStr {
		if numMap[name] {
			return mixedKindErr(label)
		}
		strMap[name] = true
		return nil
	}
	if strMap[name] {
		return mixedKindErr(label)
	}
	numMap[name] = true
	return nil
}

func (g *irGen) kindMapsFor(slot string) (map[string]bool, map[string]bool, string) {
	switch slot {
	case "set":
		return g.setElemStr, g.setElemInt, "set"
	case "dict key":
		return g.dictKeyStr, g.dictKeyInt, "dict key"
	case "dict value":
		return g.dictValStr, g.dictValInt, "dict value"
	default:
		return g.listElemStr, g.listElemInt, "list"
	}
}

// replaceElemKind is recordElemKind for item assignment (`xs[0] = "s"`, `d[k] = v`), which
// *overwrites* a slot rather than growing the container. The old element is gone, so the new
// element's kind is the truth at that position: `xs = [1]` then `xs[0] = "s"` leaves a list
// holding one string, and printing it must show ['s'] rather than be refused as "mixed"
// (roadmap Gap J.6). Growing a container with a different kind (append/add) stays an error.
func (g *irGen) replaceElemKind(name, slot string, isStr bool) {
	strMap, numMap, _ := g.kindMapsFor(slot)
	if isStr {
		numMap[name] = false
		strMap[name] = true
		return
	}
	strMap[name] = false
	numMap[name] = true
}
