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
	// Sets and dicts handed to a callee are built by the same helpers every other container
	// build uses, so their slots carry tags: a hand-rolled loop here used to write payloads
	// only, and the container it built compared unequal to an identical one whose slots *were*
	// tagged — rt_slot_eq compares the pair (ADR 0189).
	switch n := e.(type) {
	case *SetLit:
		h, err := g.heapSetFrom(b, n, "")
		if err != nil {
			return "", true, err
		}
		return h, true, nil
	case *DictLit:
		h, err := g.heapDictFrom(b, n, "")
		if err != nil {
			return "", true, err
		}
		return h, true, nil
	}
	return "", false, nil
}

// heapElemKind lowers a value for a heap container slot and reports whether it became an
// interned string. Strings are compile-time globals in this backend, so a slot stores the
// index into @str_tab instead of the pointer: `rt_set_elem(i32 %h, i32 0, i32 @.str1)` put a
// global in an i32 parameter, which LLVM rejected and the exit-code contract then called a
// compiler bug (roadmap Gap I.2). A string the backend cannot resolve to text still refuses
// with the actionable diagnostic rather than emitting bad IR (ADR 0166).
func (g *irGen) heapElemKind(b *strings.Builder, e Expr) (string, bool, error) {
	if g.nestedContainerElem(e) {
		// An element that is itself a container is stored as the inner object's handle, tagged so
		// that the printer and the comparison can find their way back to it at run time. The
		// collector reaches it because it marks every element word of a marked object (ADR 0188);
		// what stays refused is the shape no rule covers — nested deeper than every element can be
		// tagged, or a dict keyed by a container (roadmap L11.1).
		if dl, ok := e.(*DictLit); ok && dictWantsContainerKey(dl) {
			return "", false, nestedContainerErr(e)
		}
		if !g.taggableNestedElem(e) {
			return "", false, nestedContainerErr(e)
		}
		g.heapUsed = true
		h, cerr := g.containerOperand(b, e)
		return h, false, cerr
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
	// The one question every container emitter must ask, asked here so it is asked once: can the
	// compiled word hold this? A slot is an i32 and a double does not fit in one, so a float is
	// stored as the handle of a @float_box object and the tag says so. Before that existed this
	// line refused instead, and the alternatives it existed to prevent were a truncated read
	// (`xs = [1.5]; print(xs[0])` answered 1) or an operand the compiler invented, which llc
	// rejected and the exit-code contract called a compiler bug for an ordinary program
	// (roadmap Gap R.40, ADR 0166; the float element itself is roadmap L11.1, ADR 0233).
	if _, isFloatLit := e.(*FloatLit); isFloatLit || g.isFloat(e) {
		g.floatFmtUsed = true // the mixed printer reaches rt_fmt_double
		inner, ferr := g.floatValue(b, e), error(nil)
		if ferr != nil {
			return "", false, ferr
		}
		if inner == "" {
			// A box needs the number to put in it. `rt_float_new(double )` is the module llc rejects,
			// and ADR 0166 counts that as the compiler's bug; the element names itself instead
			// (roadmap Gap R.88, measured beside Gap R.96).
			return "", false, g.floatOperandRefusal(e)
		}
		bh := g.newTmp()
		fmt.Fprintf(b, "  %s = call i32 @rt_float_new(double %s)\n", bh, inner)
		return bh, false, nil
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
// heapListFromTagged builds a heap list whose elements carry per-element tags: one
// rt_tag_elem per slot, with the canonical ValueTag number for what the element is. The
// container-wide @estr[h] bit 8 is set as well, because it now means "the slots are the truth"
// rather than "this is the string list": a list printed through rt_print_list has to reach the
// tag-aware printer on its own, and not only when the codegen happened to pick the mixed
// printer for it (roadmap L11.1, ADR 0232).
func (g *irGen) heapListFromTagged(b *strings.Builder, ln *ListLit) (string, error) {
	g.heapUsed = true
	h := g.newTmp()
	fmt.Fprintf(b, "  %s = call i32 @rt_alloc(i32 %d)\n", h, HeapList)
	for i, el := range ln.Elems {
		v, interned, err := g.heapElemKind(b, el)
		if err != nil {
			return "", err
		}
		tag := g.elemTagFor(el, interned)
		if tag == int32(TagNone) {
			v = "0"
		}
		fmt.Fprintf(b, "  call void @rt_tag_elem(i32 %s, i32 %d, i32 %d)\n", h, i, tag)
		fmt.Fprintf(b, "  call void @rt_set_elem(i32 %s, i32 %d, i32 %s)\n", h, i, v)
	}
	fmt.Fprintf(b, "  call void @rt_mark_estr(i32 %s, i32 8)\n", h)
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
		// Every builder writes the tag with the payload, not only the mixed-list one: a slot
		// whose tag was never written carries whatever the previous tenant of that heap slot
		// left behind, and equality (rt_slot_eq) reads the pair (ADR 0187's rule, ADR 0189).
		fmt.Fprintf(b, "  call void @rt_tag_elem(i32 %s, i32 %d, i32 %d)\n", h, i, g.elemTagFor(el, interned))
	}
	// A literal whose elements can only be read back through their tags — a float box handle, the
	// nothing of a None, another container's handle — says so on the object, and the printer asks
	// the slots instead of the list. The dict and set builders already did this; the list one asked
	// only on its tagged path, so a nested list reached through rt_print_list printed its inner
	// object's handle (roadmap L11.1, ADR 0232).
	// The same rule extended to a list that mixes kinds, not only to one whose elements need
	// tags: ["a", 1] has no single kind either. print chose its printer from this scope and got
	// it right; str() and repr() ask the object, because the pair is one renderer pointed at a
	// different sink, and a renderer that asks the object can be handed a handle the builder's
	// scope never saw. Answering [0, 1] for ["a", 1] was the object not saying what it holds
	// (roadmap L11.2, ADR 0258 — Gap L.2's shape one level down).
	if g.literalNeedsTags(ln) || literalMixedKinds(ln) {
		bits |= 8
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
	if dictWantsContainerKey(dl) {
		// Built anyway, the key would be the inner object's handle printed as a number: {([1, 2]): 3}
		// answered {1: 3}, which is a wrong answer about the key rather than a missing one
		// (roadmap L11.1).
		for _, k := range dl.Keys {
			if isContainerLiteral(k) {
				return "", nestedContainerErr(k)
			}
		}
	}
	g.heapUsed = true
	h := g.newTmp()
	fmt.Fprintf(b, "  %s = call i32 @rt_alloc(i32 %d)\n", h, HeapDict)
	bits := 0
	// A dict whose keys or values mix kinds has no kind to record on the object or on the
	// variable: each slot carries its own tag, and bit 8 tells the printer and the lookup to
	// read them (roadmap L11.1 (1b), ADR 0232).
	mixed := g.taggableMixedDict(dl)
	if mixed {
		bits |= 8
		if name != "" {
			g.mixedDicts[name] = true
			g.dictKeyStr[name] = false
			g.dictKeyInt[name] = false
			g.dictValStr[name] = false
			g.dictValInt[name] = false
		}
	}
	for i := range dl.Keys {
		kk, kIsStr, err := g.heapElemKind(b, dl.Keys[i])
		if err != nil {
			return "", err
		}
		vv, vIsStr, err := g.heapElemKind(b, dl.Vals[i])
		if err != nil {
			return "", err
		}
		if mixed {
			kt, _ := g.elemKindTag(dl.Keys[i])
			vt, _ := g.elemKindTag(dl.Vals[i])
			fmt.Fprintf(b, "  call void @rt_dict_put_tagged(i32 %s, i32 %s, i32 %s, i32 %d, i32 %d)\n", h, kk, vv, kt, vt)
			continue
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
		// A dict interleaves key and value in the element array, so it owns two tag slots per
		// entry: rt_dict_get reads the key payload, rt_container_eq reads the pairs.
		fmt.Fprintf(b, "  call void @rt_tag_elem(i32 %s, i32 %d, i32 %d)\n", h, i*2, g.elemTagFor(dl.Keys[i], kIsStr))
		fmt.Fprintf(b, "  call void @rt_tag_elem(i32 %s, i32 %d, i32 %d)\n", h, i*2+1, g.elemTagFor(dl.Vals[i], vIsStr))
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
	idx := 0
	mixed := g.taggableMixedSet(sl)
	if mixed {
		bits |= 8
		if name != "" {
			g.mixedSets[name] = true
			g.setElemStr[name] = false
			g.setElemInt[name] = false
		}
	}
	for _, el := range sl.Elems {
		v, interned, err := g.heapElemKind(b, el)
		if err != nil {
			return "", err
		}
		if mixed {
			t, _ := g.elemKindTag(el)
			fmt.Fprintf(b, "  call void @rt_set_add_tagged(i32 %s, i32 %s, i32 %d)\n", h, v, t)
			idx++
			continue
		}
		if interned {
			bits |= 1
			if name != "" {
				g.setElemStr[name] = true
			}
		}
		fmt.Fprintf(b, "  call void @rt_set_add(i32 %s, i32 %s)\n", h, v)
		fmt.Fprintf(b, "  call void @rt_tag_elem(i32 %s, i32 %d, i32 %d)\n", h, idx, g.elemTagFor(el, interned))
		idx++
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
// or a container variable inside another one. Such an element is stored as the inner object's
// handle and tagged as a container, which the collector reaches because it marks every element word
// of a marked object (ADR 0181, ADR 0188). What this gate still guards is the shape no rule
// describes: the compile-time global layout would put `@.lstN` in a value position, which is a
// module llc refuses, and an untagged handle prints as its own index — which is how
// `print([[1], [2]])` once answered `[1, 2]` (roadmap L11.1).
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
	return fmt.Errorf("codegen: a compiled container cannot hold %s yet; a dict keyed by a container has no hashing rule, and an element nested deeper than every inner element can be tagged would be printed by handle and compared by address (roadmap L11.1). Build that inner value separately, or run it interpreted", exprSnippet(e))
}

func literalNeedsHeap(e Expr) bool {
	// The static global layout is an int array: an element that is interned text, a float box, or
	// a handle into another container has no representation in it, so the heap builder must make
	// the object instead (roadmap Gap J.6, L11.1).
	switch n := e.(type) {
	case *ListLit:
		for _, el := range n.Elems {
			if isStringExpr(el) || isContainerLiteral(el) {
				return true
			}
		}
	case *SetLit:
		for _, el := range n.Elems {
			if isStringExpr(el) || isContainerLiteral(el) {
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
			if isStringExpr(v) || isContainerLiteral(v) {
				return true
			}
		}
	}
	return false
}

// literalNeedsTags asks the other question of a container literal: not "does it need the heap"
// (a static float-literal array serves sum/min/max today) but "can its elements be read back
// without a per-slot tag?". A float slot holds a @float_box handle and a None slot holds nothing,
// so neither can: a literal containing one must be built and printed through the tagged path,
// which is what makes print([1.5]) answer [1.5] and print([None]) answer [None] instead of the
// handle and the zero (roadmap L11.1, ADR 0233). It is deliberately a separate question from
// literalNeedsHeap: the two were one predicate, and folding them diverted sum([1.5, 2.5]) from
// the static array it handles to a heap list it does not.
func (g *irGen) literalNeedsTags(e Expr) bool {
	switch n := e.(type) {
	case *ListLit:
		for _, el := range n.Elems {
			if isFloatLitExpr(el) || isNoneLitExpr(el) || isContainerLiteral(el) || g.boolSlotExpr(el) {
				return true
			}
		}
	case *SetLit:
		for _, el := range n.Elems {
			if isFloatLitExpr(el) || isNoneLitExpr(el) || isContainerLiteral(el) || g.boolSlotExpr(el) {
				return true
			}
		}
	case *DictLit:
		for _, k := range n.Keys {
			if isFloatLitExpr(k) || isNoneLitExpr(k) || g.boolSlotExpr(k) {
				return true
			}
		}
		for _, v := range n.Vals {
			if isFloatLitExpr(v) || isNoneLitExpr(v) || isContainerLiteral(v) || g.boolSlotExpr(v) {
				return true
			}
		}
	}
	return false
}

// boolSlotExpr is the one question a slot asks to find out whether it is holding a verdict: the
// literal, a comparison, a name the program last bound to one, or a call whose body returns one.
// It is the same predicate print, str() and repr() answer with (ADR 0257), reached from the element
// rather than the print site — and it has to be asked of an expression, not just a literal, or
// print([verdict(), 0]) is built by the static path and prints [1, 0] (roadmap Gap R.112, ADR 0259).
func (g *irGen) boolSlotExpr(e Expr) bool {
	if _, ok := e.(*BoolLit); ok {
		return true
	}
	return g != nil && g.printsAsBool(e)
}

// isFloatLitExpr and isNoneLitExpr are the two elements that make a container literal need the
// tagged path even when every element is alike: their payloads are a @float_box handle and nothing
// at all, neither of which a container-wide kind can describe (roadmap L11.1, ADR 0233).
func isFloatLitExpr(e Expr) bool {
	_, ok := e.(*FloatLit)
	return ok
}

func isNoneLitExpr(e Expr) bool {
	_, ok := e.(*NoneLit)
	return ok
}

// (The bool arm of literalNeedsTags used to be a sibling of these two helpers, testing for the
// literal alone. It is now irGen.boolSlotExpr, because a slot holding the call
// `verdict()` is as much a verdict as one holding True.)

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
	if s, ok := stringConst(e, nil); ok {
		return s, true
	}
	return "", false
}

// taggableMixedList reports whether a list literal's every element is one the per-element tag can
// describe today, and whether the tags are needed at all — the list spelling of elemsTaggable,
// which the set and the dict ask too. Bool is what proved the point: the element tag grew a bool
// and only one of the three spellings noticed (roadmap Gap R.112, ADR 0259).
func (g *irGen) taggableMixedList(ln *ListLit) bool {
	return g.elemsTaggable(ln.Elems)
}

// elemKindTag is the codegen's answer to "what tag does this element's slot carry", and the
// gate on what a mixed list may hold at all. Excluded, each for a reason: bools (not values
// in either backend yet, so [True] would print 1 against Python's True -- L11.1), and any
// expression whose string-ness the codegen cannot prove -- because printing an interned
// string's *index* as a number is exactly the wrong-output bug the refusal exists to avoid.
// Containers are included once every element inside them is taggable too: the payload is the
// inner object's handle, and the tag is what routes it to the container printer and to
// rt_container_eq instead of a number and an integer compare (roadmap L11.1).
func (g *irGen) elemKindTag(e Expr) (int32, bool) {
	// A variable the compiler built as a container holds a handle, and the slot that copies it
	// needs the container's tag: appending a list variable to another list stored the handle and
	// tagged it TagInt, so [1, [2]] printed as [1, 1] (roadmap L11.1).
	if nm, isName := e.(*Name); isName && g != nil && g.taggableNestedElem(e) {
		switch {
		case g.runtimeDicts[nm.Value]:
			return int32(TagDict), true
		case g.runtimeSets[nm.Value]:
			return int32(TagSet), true
		default:
			return int32(TagList), true
		}
	}
	switch e.(type) {
	case *FloatLit:
		// A float goes into the slot as the handle of a @float_box object, which the mixed printer
		// renders and rt_payload_eq compares by value; before both of those existed this line said
		// "not taggable" and the literal was refused rather than misprinted (roadmap L11.1, ADR 0238).
		return int32(TagFloat), true
	case *ListLit, *DictLit, *SetLit:
		// A slot can hold another container: the payload is the inner object's handle and the tag
		// says which printer and which comparison answer for it. Storing it was never the hard
		// part — the collector marks every element word of a marked object — what made it
		// unanswerable was that nothing could render or compare an inner object without knowing
		// statically what it held. The tag routes the print to the inner container's own printer
		// and the comparison to rt_container_eq, both at run time (roadmap L11.1, ADR 0189).
		if !g.taggableNestedElem(e) {
			return 0, false
		}
		switch e.(type) {
		case *ListLit:
			return int32(TagList), true
		case *DictLit:
			return int32(TagDict), true
		default:
			return int32(TagSet), true
		}
	case *Tuple, *Lambda:
		return 0, false
	case *BoolLit:
		// The tag table has always had a bool in it (value.go); what was missing was a container
		// slot allowed to say so. The payload stays the 0/1 the verdict was made from — a bool
		// behaves as that number everywhere (True + 1 is 2, [True] == [1] is True, 1 in {True} is
		// True, all pinned) — and the tag beside it is what lets the printer answer True rather
		// than 1. ADR 0232 promised this line; ADR 0259 is the day it was kept (roadmap Gap R.112).
		return int32(TagBool), true
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
		// A call that hands back text on one path and a number on another has no honest tag:
		// print can render whichever value arrives, because it asks at print time, but a
		// container slot is labelled once at build time — and a number labelled as text comes
		// out of the string table as `(null)` where the interpreter prints 7 (ADR 0232).
		if call, isCall := e.(*Call); isCall && !g.callReturnsOnlyStr(call) {
			return 0, false
		}
		return int32(TagStr), true
	}
	if nm, ok := e.(*Name); ok {
		if g.strVals[nm.Value] != "" {
			return int32(TagStr), true
		}
	}
	// The same question the storage side asks — `heapElemKind`'s float arm and this line are one
	// question, asked once: an element whose value is a float goes into the slot as the handle of a
	// @float_box object, so the label beside it has to say *float* whatever spelled the float. The
	// literal was covered by the case above and a float-valued variable by the Name arm this
	// generalises; a call that returns one (`ys.append(half(3))`) and, since ADR 0253, the numeric
	// door's answer for a slot the object describes (`ys.append(xs[0] / 2)`) were not, and the box
	// went in beside a TagInt: the printer read the handle as the number and `print(ys[0])` answered
	// `1` where CPython answers `3.0` (roadmap L11.1, ADR 0238's rule taken one spelling further).
	if g.isFloat(e) {
		return int32(TagFloat), true
	}
	// The same question ADR 0257's print rule asks, asked of a slot: `b = 1 == 1` and
	// `xs.append(b)`/`xs.append(1 == 1)` store a verdict the program called bool, and the tag has
	// to say bool for the printer to name it. ADR 0257 made this one question for a print site;
	// a container slot is the second site that has to ask it, and it asks the same predicate
	// rather than keeping its own list, so the two cannot drift (roadmap Gap R.112, ADR 0259).
	if g != nil && g.printsAsBool(e) {
		return int32(TagBool), true
	}
	return int32(TagInt), true
}

// taggableMixedDict reports a dict literal whose key and value slots can each carry a tag the
// codegen can prove, and which therefore does not have one element kind: its keys mix, or its
// values do. Such a dict is built with tagged puts, printed position by position and looked up by
// (payload, tag) — the same rule ADR 0184 established for lists, extended to dicts by ADR 0232.
// Anything the tag cannot describe (a bool, a float, a nested container) keeps the whole literal
// on the refused path rather than storing a word whose meaning nobody can recover.
func (g *irGen) taggableMixedDict(dl *DictLit) bool {
	if len(dl.Keys) == 0 {
		return false
	}
	keyStr, keyOther, valStr, valOther := false, false, false, false
	mustTag := false
	for i := range dl.Keys {
		kt, ok := g.elemKindTag(dl.Keys[i])
		if !ok {
			return false
		}
		if kt == int32(TagStr) {
			keyStr = true
		} else {
			// One table for both sides of the dict and for all three container shapes
			// (elemsTaggable's rule, restated here for a key and a value at once).
			if slotTagSelfDescribing(kt) {
				mustTag = true
			}
			keyOther = true
		}
		if vt, ok := g.elemKindTag(dl.Vals[i]); ok {
			if vt == int32(TagStr) {
				valStr = true
			} else {
				if slotTagSelfDescribing(vt) {
					mustTag = true
				}
				valOther = true
			}
		} else {
			return false
		}
	}
	return mustTag || (keyStr && keyOther) || (valStr && valOther)
}

// dictWantsContainerKey reports a dict literal keyed by a container — the one nested shape still
// refused, because a container key has no hashing rule in this backend and a handle compared by
// integer equality would make every key unique by accident. Values nest freely (roadmap L11.1).
func dictWantsContainerKey(dl *DictLit) bool {
	for _, k := range dl.Keys {
		switch k.(type) {
		case *ListLit, *DictLit, *SetLit:
			return true
		}
	}
	return false
}

// compElemPrintsAsText answers whether the elements of a runtime-built list comprehension are text,
// and it asks the question of the **iterated object** rather than of the element expression. The
// element of `[n for n in names]` is the loop variable, and the loop variable's own facts
// (`internedVars[n]`) are scoped to the loop — by the time the assignment records `out`, they are
// gone, and the list was bound as a list of numbers: `print(out)` reached the container printer, which
// asks the object, and printed ['a'], while `print(out[0])` reached `%d` and printed the interned index
// `0`. The iterated container is still there to be asked (roadmap Gap R.46, ADR 0244).
func (g *irGen) compElemPrintsAsText(c *Comp) bool {
	if c == nil || len(c.Elems) != 1 || c.ForVar == nil {
		return false
	}
	nm, isName := c.Elems[0].(*Name)
	if !isName || nm.Value != c.ForVar.Value {
		// An element that computes something (`[n.upper() for n in names]`) is answered by the
		// expression itself, which knows its own kind; this helper is only the identity case.
		return false
	}
	it, isName := c.Iter.(*Name)
	if !isName {
		return g.exprIsString(c.Iter)
	}
	return g.listElemStr[it.Value] || g.setElemStr[it.Value] || g.dictKeyStr[it.Value]
}

// slotTagSelfDescribing reports whether a slot whose payload only means something through its tag
// — a float box's handle, None's nothing, another container's handle — obliges the container to stop
// claiming one element kind and let the printer and the lookup ask each slot instead. That is
// ADR 0232's move, extended to the tags a compiled word cannot render at all (roadmap L11.1).
func slotTagSelfDescribing(t int32) bool {
	switch t {
	case int32(TagBool), int32(TagFloat), int32(TagNone), int32(TagList), int32(TagDict), int32(TagSet):
		return true
	}
	return false
}

// compElemCopiesABool answers the printing question for a comprehension slot that is nothing but a
// copy of the loop variable. `[x for x in [True, 1, 1]]` has an element expression (`x`) that says
// nothing on its own — the item it copies is what says True — so the tag has to be asked of the item
// or the list is built as three integers (roadmap Gap R.112, ADR 0259).
func (g *irGen) compElemCopiesABool(elem Expr, lv string, item Expr) bool {
	if lv == "" || elem == nil || item == nil {
		return false
	}
	var bound string
	switch id := elem.(type) {
	case *Name:
		bound = id.Value
	default:
		return false
	}
	return bound == lv && g.boolSlotExpr(item)
}

// anyItemIsAVerdict asks the same question of every item a comprehension will walk, which is what
// the compile-time fold has to know before it commits the result to a global.
func (g *irGen) anyItemIsAVerdict(elem Expr, forVar *Name, itemExprs []Expr) bool {
	lv := ""
	if forVar != nil {
		lv = forVar.Value
	}
	for _, it := range itemExprs {
		if g.compElemCopiesABool(elem, lv, it) {
			return true
		}
	}
	return false
}

// taggableNestedElem answers whether an element that is itself a container can be stored, printed
// and compared: every element one level in has to be taggable too, recursively, because one
// untaggable element deeper down would be printed by handle and compared by address while
// everything around it behaved as a value. Tuples and lambdas stay outside the tag table entirely,
// and a dict whose keys are containers is refused by dictWantsContainerKey (roadmap L11.1).
func (g *irGen) taggableNestedElem(e Expr) bool {
	// One element of an inner container: a container recurses, anything else is whatever the tag
	// table already answers for. Asking a plain integer of this function is what used to make
	// [[1, 2]] look untaggable — the recursion ended at an IntLit and answered "not a container",
	// which is true and irrelevant (roadmap L11.1).
	inner := func(el Expr) bool {
		if isContainerLiteral(el) || g.isContainerExpr(el) {
			return g.taggableNestedElem(el)
		}
		_, ok := g.elemKindTag(el)
		return ok
	}
	switch n := e.(type) {
	case *ListLit:
		for _, el := range n.Elems {
			if !inner(el) {
				return false
			}
		}
		return true
	case *SetLit:
		for _, el := range n.Elems {
			if !inner(el) {
				return false
			}
		}
		return true
	case *DictLit:
		if dictWantsContainerKey(n) {
			return false
		}
		for _, v := range n.Vals {
			if !inner(v) {
				return false
			}
		}
		return true
	case *Name:
		return g.listVars[n.Value] || g.mixedLists[n.Value] || g.runtimeDicts[n.Value] || g.runtimeSets[n.Value]
	}
	_, ok := emptyContainerLiteral(e)
	return ok
}

// taggableMixedSet is taggableMixedDict for a set literal: every member taggable, and members of
// more than one kind present.
func (g *irGen) taggableMixedSet(sl *SetLit) bool {
	return g.elemsTaggable(sl.Elems)
}

// elemsTaggable is the one question three container builders used to each spell out: can every
// element of this run carry a tag, and does at least one of them need one. A slot whose tag says
// *itself* (slotTagSelfDescribing) needs the tagged path because its payload is a handle, nothing,
// or a 0/1 that cannot speak its kind; a run that mixes interned text with anything else has no
// element kind either. The list, set and dict builders ask this together so a kind added to the
// tag table — bool is the case that proves the point (Gap R.112) — cannot be true for one
// container shape and refused for the other two.
func (g *irGen) elemsTaggable(elems []Expr) bool {
	if len(elems) == 0 {
		return false
	}
	sawStr, sawOther, sawMustTag := false, false, false
	for _, el := range elems {
		t, ok := g.elemKindTag(el)
		if !ok {
			return false
		}
		if t == int32(TagStr) {
			sawStr = true
		} else {
			if slotTagSelfDescribing(t) {
				sawMustTag = true
			}
			sawOther = true
		}
	}
	return sawMustTag || (sawStr && sawOther)
}

// taggedOperand lowers an expression to the (payload, tag) pair it stands for, which is what a
// mixed container's runtime helpers take. A variable bound by a loop over a mixed container
// carries its tag beside it (ADR 0185), so the pair is read from those two slots; anything else
// is an expression whose kind the codegen can prove, whose tag is a literal. ok is false when
// neither holds — the caller refuses rather than guessing a tag, because a wrong tag prints and
// compares as the wrong kind (ADR 0232).
func (g *irGen) taggedOperand(b *strings.Builder, e Expr) (val, tag string, ok bool) {
	if nm, isName := e.(*Name); isName && g.taggedVars[nm.Value] {
		v := g.newTmp()
		fmt.Fprintf(b, "  %s = load i32, i32* %%_%s\n", v, nm.Value)
		t := g.newTmp()
		fmt.Fprintf(b, "  %s = load i32, i32* %%_%s_tag\n", t, nm.Value)
		return v, t, true
	}
	t, tok := g.elemKindTag(e)
	if !tok {
		return "", "", false
	}
	if t == int32(TagFloat) {
		// The payload a float's slot holds is the handle of a @float_box object, not the number
		// g.value would answer by truncating the double; asking for the pair and getting the
		// truncated word is how `1.0 in [1]` came back false (roadmap L11.1, ADR 0233).
		g.floatFmtUsed = true
		inner := g.floatValue(b, e)
		if inner == "" {
			// No number, no box: see the guard in heapElemWord above (roadmap Gap R.88).
			return "", "", false
		}
		bh := g.newTmp()
		fmt.Fprintf(b, "  %s = call i32 @rt_float_new(double %s)\n", bh, inner)
		return bh, strconv.FormatInt(int64(TagFloat), 10), true
	}
	if t == int32(TagList) || t == int32(TagDict) || t == int32(TagSet) {
		// The payload a container slot holds is the inner object's *handle*. g.value answers a
		// literal with its folded global, whose layout is a length plus an array: handing that to
		// rt_set_contains_tagged(i32, i32, i32) is the global-in-an-i32-parameter shape ADR 0166
		// counts as a compiler bug, so the operand is materialised into the heap first — and then
		// compared by content, because two literals holding the same elements are equal
		// (roadmap L11.1, ADR 0189).
		h, cerr := g.containerOperand(b, e)
		if cerr != nil {
			return "", "", false
		}
		return h, strconv.FormatInt(int64(t), 10), true
	}
	v, err := g.value(b, e)
	if err != nil {
		return "", "", false
	}
	return v, strconv.FormatInt(int64(t), 10), true
}

// mixedDictPair reads one entry of a dict whose values mix kinds: the value and the tag the
// value's slot carries. `print(d[k])` and `v = d[k]` are the two uses, and both need the pair —
// the payload alone is a number, a string index or the None singleton with nothing to say which.
func (g *irGen) mixedDictPair(b *strings.Builder, dictName string, key Expr, sp Span) (val, tag string, err error) {
	g.heapUsed = true
	h := g.newTmp()
	fmt.Fprintf(b, "  %s = load i32, i32* %%_%s\n", h, dictName)
	kv, kt, ok := g.taggedOperand(b, key)
	if !ok {
		return "", "", fmt.Errorf("codegen: reading %q from a dict whose values are of more than one kind needs a key whose kind the compiler can prove; a bool, float or container key needs the tagged value word (roadmap L11.1, ADR 0232)", dictName)
	}
	g.checkKeyReadTagged(b, h, kv, kt, sp)
	val = g.newTmp()
	fmt.Fprintf(b, "  %s = call i32 @rt_dict_get_tagged(i32 %s, i32 %s, i32 %s)\n", val, h, kv, kt)
	tag = g.newTmp()
	fmt.Fprintf(b, "  %s = call i32 @rt_dict_value_tag(i32 %s, i32 %s, i32 %s)\n", tag, h, kv, kt)
	return val, tag, nil
}

// isMixedContainer names the containers whose slots describe themselves. Reads, membership tests
// and printers consult it so a mixed container never goes down the one-static-kind path.
func (g *irGen) isMixedContainer(name string) bool {
	return g.mixedDicts[name] || g.mixedSets[name] || g.mixedLists[name]
}

// elemTagFor is the canonical ValueTag number an element's slot carries. Numbers are 0
// (TagInt, which is also the array's zero value), None is TagNone, interned strings TagStr.
// elemTagFor is the tag a slot carries for this element. It is a method because the answer is
// not in the spelling: a Name the generator knows holds a float is a float element, and tagging
// it TagInt would label a box handle as a number (roadmap L11.1, ADR 0233).
func (g *irGen) elemTagFor(e Expr, interned bool) int32 {
	if interned {
		return int32(TagStr)
	}
	// One question, asked once: what tag does this element's slot carry. The builders used to
	// answer it locally and drifted from elemKindTag — which is how appending a list variable to
	// another list stored the handle and tagged it TagInt, printing [1, 1] for [1, [2]]
	// (roadmap L11.1).
	if g != nil {
		if t, ok := g.elemKindTag(e); ok {
			return t
		}
	}
	switch e.(type) {
	case *NoneLit:
		return int32(TagNone)
	case *FloatLit:
		return int32(TagFloat)
	}
	if g != nil && g.isFloat(e) {
		return int32(TagFloat)
	}
	return int32(TagInt)
}

// mixedReadErr is what reading a single element out of a tagged list reports: the tag says
// what the element is, but the use site was compiled against one static kind.
func mixedReadErr(what string) error {
	return fmt.Errorf("codegen: a compiled %s holds elements of more than one kind, so one element is a (value, tag) pair; print(xs[i]), v = xs[i] and — since ADR 0249 — a numeric use whose result kind the compiler can settle all work because the tag travels with them, but this context needs a single static kind (roadmap L11.1, Gap R.82)", what)
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
	return fmt.Errorf("codegen: an element of a mixed compiled container must carry a tag, and %s has none the runtime can render — the tag table covers numbers, text, None, and containers now, but not a tuple, a lambda, or a value with no canonical tag (roadmap L11.1, ADR 0187, ADR 0238)", exprSnippet(e))
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
	case *StrLit:
		return "a string literal"
	case *NoneLit:
		return "None"
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
// elemPayloadAndTag is the one door that turns an expression into what a container slot holds: the
// payload word, the tag that says what it is, and whether the payload is an interned string index.
// Every writer of a slot goes through it, because the two halves must be written together — a slot
// that gets a payload without a tag reads back as whatever the previous occupant of that recycled
// heap slot was tagged as (ADR 0187, ADR 0189), and a container element whose payload is the
// compiler's *global* (`@.set1`) rather than a heap handle is the module `llc` rejects (roadmap
// Gap R.75, ADR 0244: a comprehension over container literals hit exactly that).
//
// `xs.append(v)` already used this door; a comprehension built its elements with `g.value` alone,
// which is why the same program worked one way and failed the other.
func (g *irGen) elemPayloadAndTag(b *strings.Builder, e Expr) (payload, tag string, interned bool, err error) {
	// A variable the compiler bound from a container whose slots carry tags already holds the pair;
	// asking it for the payload alone is the mistake Gap R.76 refuses to make, so the read goes through
	// the two allocas together and the tag that arrives is the object's own, not a guess.
	if nm, isName := e.(*Name); isName {
		if p, t, ok := g.taggedLoopVarRead(b, nm.Value); ok {
			return p, t, false, nil
		}
	}
	t, ok := g.elemKindTag(e)
	if !ok {
		return "", "", false, mixedTaggedElemErr(e)
	}
	v, intern, herr := g.heapElemKind(b, e)
	if herr != nil {
		return "", "", false, herr
	}
	// A string is taggable only if the word really is an interned index, and a container only if
	// heapElemKind materialised it into the heap; either way the tag and the word have to agree.
	if intern && t != int32(TagStr) {
		return "", "", false, mixedTaggedElemErr(e)
	}
	if !intern && t == int32(TagStr) {
		return "", "", false, mixedTaggedElemErr(e)
	}
	if t == int32(TagNone) {
		// None has no i32 payload of its own; the tag is what renders it.
		v = "0"
	}
	return v, strconv.FormatInt(int64(t), 10), intern, nil
}

func (g *irGen) mixedElemTag(b *strings.Builder, e Expr) (payload, tag string, err error) {
	p, t, _, err := g.elemPayloadAndTag(b, e)
	if err != nil {
		return "", "", err
	}
	return p, t, nil
}

// taggedLoopVarRead emits the (payload, tag) pair of a variable the compiler bound out of a
// container whose slots carry tags — the loop variable of a `for` or a comprehension over a container
// that holds more than one kind. The pair lives in two allocas, `%_x` and `%_x_tag`, and ADR 0185's
// rule is that one without the other means nothing: an i32 that is 1 could be the integer 1 or the
// interned index of some text. So the only way to ask for the payload is to ask for both, and the
// caller gets the tag as the object wrote it rather than as the compiler guessed it.
func (g *irGen) taggedLoopVarRead(b *strings.Builder, name string) (payload, tag string, ok bool) {
	if !g.taggedVars[name] || !g.allocd[name+"_tag"] {
		return "", "", false
	}
	p := g.newTmp()
	fmt.Fprintf(b, "  %s = load i32, i32* %%%s\n", p, "_"+name)
	t := g.newTmp()
	fmt.Fprintf(b, "  %s = load i32, i32* %%%s\n", t, "_"+name+"_tag")
	return p, t, true
}

// compElemIsTaggedLoopVar reports whether a comprehension's element is its own loop variable and the
// container being iterated holds more than one kind — the case where the element's kind is only in the
// object, never in the compiler. The list the comprehension builds therefore has to be registered as
// one whose slots speak for themselves (ADR 0232), because nothing static says what is in it.
func (g *irGen) compElemIsTaggedLoopVar(c *Comp) bool {
	if c == nil || len(c.Elems) != 1 || c.ForVar == nil {
		return false
	}
	nm, isName := c.Elems[0].(*Name)
	if !isName || nm.Value != c.ForVar.Value {
		return false
	}
	it, isName := c.Iter.(*Name)
	if !isName {
		return false
	}
	return g.mixedLists[it.Value] || g.mixedSets[it.Value] || g.mixedDicts[it.Value]
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

// literalMixedIsTaggable is the one question every "this literal mixes kinds" site asks: can each
// slot say what it holds? Lists, sets and dicts each have their own answer, and print, value and
// assignment all consult it, so a program is not refused in one context and compiled in another
// (ADR 0232).
func (g *irGen) literalMixedIsTaggable(e Expr) bool {
	switch lit := e.(type) {
	case *ListLit:
		return g.taggableMixedList(lit)
	case *SetLit:
		return g.taggableMixedSet(lit)
	case *DictLit:
		return g.taggableMixedDict(lit)
	}
	return false
}

// exprSummary names an expression inside a diagnostic. `%s` on an AST node dumps a Go struct with
// pointers in it, which is unreadable for a person and unmatchable for an agent, and the
// diagnostics of this compiler are an interface (docs/operations.md). The rule is to say what the
// program wrote — the function called, the variable read — and fall back to the kind.
func (g *irGen) exprSummary(e Expr) string {
	switch n := e.(type) {
	case *Call:
		if nm, ok := n.Fn.(*Name); ok {
			return nm.Value + "(...)"
		}
		if at, ok := n.Fn.(*Attr); ok {
			return "." + at.Name.Value + "()"
		}
		return "a call"
	case *Name:
		return n.Value
	case *StrLit:
		return "a string"
	case *IntLit:
		return "a number"
	case *FloatLit:
		return "a float"
	case *NoneLit:
		return "None"
	case *BoolLit:
		return "a bool"
	case *ListLit:
		return "a list"
	case *DictLit:
		return "a dict"
	case *SetLit:
		return "a set"
	case *Index:
		return "an element read"
	case *BinOp:
		return "(" + n.Op + ")"
	}
	return exprTyName(e)
}

// exprGist renders an expression the way the program wrote it, as far as the names go. exprSummary
// above answers the *kind* ("an element read"), which is what a message about a missing capability
// wants; a message about a refused *sentence* wants the shape, because `print(xs[0] / 4)` and
// `print(ys[1] / 4)` are different programs to the person reading the diagnostic. Only the leaves a
// refusal actually reaches are spelled out; anything else keeps its kind-name.
func (g *irGen) exprGist(e Expr) string {
	switch n := e.(type) {
	case *Index:
		return g.exprGist(n.Obj) + "[" + g.exprGist(n.Idx) + "]"
	case *BinOp:
		return g.exprGist(n.L) + " " + n.Op + " " + g.exprGist(n.R)
	case *IntLit:
		return strconv.FormatInt(int64(n.Value), 10)
	case *FloatLit:
		return strconv.FormatFloat(n.Value, 'g', -1, 64)
	}
	return g.exprSummary(e)
}

// containerKindProvable asks whether a container slot may be labelled for this expression at all.
// A call that returns text on one path and a number on another is the case that may not: print can
// ask when it prints, but a slot's label is fixed when it is built, and the number labelled as text
// came out of the string table as `(null)` where the interpreter prints 7. The element is then not
// "a number" or "a string", it is unlabelable, and the container says so (ADR 0232).
func (g *irGen) containerKindProvable(e Expr) bool {
	call, ok := e.(*Call)
	if !ok {
		return true
	}
	return !g.printsAsInternedStr(call) || g.callReturnsOnlyStr(call)
}

// containerSlotLabel names the position in the diagnostic ("set", "dict key", …).
func containerSlotLabel(slot string) string {
	if slot == "list" {
		return "list"
	}
	return slot
}

// callReturnsOnlyStr answers the question a container slot needs, which is stricter than the one
// print asks. print asks "can this call hand back text", and answers well either way — the value is
// rendered at the moment it is printed. A slot cannot: it holds one tag chosen at build time, so a
// function that hands back text on one path and a number on the other has no honest tag, and the
// number printed through the string table is `(null)` where the interpreter prints 7 (ADR 0232).
// Such an expression is *unprovable*, and the mixed gate refuses rather than guessing.
func (g *irGen) callReturnsOnlyStr(c *Call) bool {
	nm, ok := c.Fn.(*Name)
	if !ok {
		return g.printsAsInternedStr(c) // a method or a chained call keeps the printer's answer
	}
	fd, ok := g.fds[nm.Value]
	if !ok {
		return g.printsAsInternedStr(c)
	}
	seen, onlyStr := false, true
	var walk func([]Stmt)
	walk = func(body []Stmt) {
		for _, st := range body {
			switch n := st.(type) {
			case *ReturnStmt:
				seen = true
				if n.Expr == nil {
					onlyStr = false
					continue
				}
				if _, isStrLit := n.Expr.(*StrLit); isStrLit {
					continue
				}
				switch n.Expr.(type) {
				case *IntLit, *FloatLit, *NoneLit, *BoolLit, *ListLit, *DictLit, *SetLit:
					onlyStr = false
					continue
				}
				if !g.exprIsString(n.Expr) {
					onlyStr = false
				}
			case *IfStmt:
				walk(n.Then)
				for _, e := range n.Elifs {
					walk(e.Then)
				}
				walk(n.Else)
			case *WhileStmt:
				walk(n.Body)
			case *ForStmt:
				walk(n.Body)
			case *TryStmt:
				walk(n.Body)
				for _, h := range n.Excepts {
					walk(h.Body)
				}
				walk(n.Finally)
			}
		}
	}
	walk(fd.Body)
	if !seen {
		return g.printsAsInternedStr(c)
	}
	return onlyStr
}

// elemTagOperand answers the tag of a value being written into a container slot, as a literal or
// as a register holding it. Three sources, in order of honesty: the static tag when the codegen can
// prove the kind; the companion slot when the value is a tagged variable (a loop variable over a
// mixed container, or one bound from a mixed read); and the kind the writing site itself observed —
// which word it just interned, an @str_tab index or a number. There is no fourth answer, because a
// slot written without its tag is the bug ADR 0189 closed: the payload would be read tomorrow
// through whatever meaning the slot held for its previous tenant.
func (g *irGen) elemTagOperand(b *strings.Builder, e Expr, interned bool) string {
	if t, ok := g.elemKindTag(e); ok {
		return strconv.FormatInt(int64(t), 10)
	}
	if nm, isName := e.(*Name); isName && g.taggedVars[nm.Value] {
		t := g.newTmp()
		fmt.Fprintf(b, "  %s = load i32, i32* %%_%s_tag\n", t, nm.Value)
		return t
	}
	if interned {
		return strconv.FormatInt(int64(TagStr), 10)
	}
	return strconv.FormatInt(int64(TagInt), 10)
}

// promoteMixed is what a container becomes when a value arriving now contradicts the kind the
// compiler had recorded for it: the container stops claiming one kind and lets its slots describe
// themselves. That is not a relaxation — it is available only because every word that ever reached
// a slot arrived with its tag (ADR 0189), so the slots already know what they hold and the mixed
// paths can be trusted with them (ADR 0232). It answers false, and the caller keeps its refusal,
// when the incoming value is one no tag can describe (a float, a nested container): there the old
// answer would be a word whose meaning nobody can recover.
//
// `kind` is the position being written — "list", "set", "dict key" or "dict value". The object is
// marked with the same bit 8 a mixed literal carries, so the runtime printer and the lookups agree
// with what the compiler now believes.
func (g *irGen) promoteMixed(b *strings.Builder, handle, name, kind string, elems ...Expr) bool {
	for _, e := range elems {
		if e == nil {
			continue
		}
		if _, ok := g.elemKindTag(e); !ok {
			return false
		}
	}
	if handle == "" || name == "" {
		return false
	}
	switch kind {
	case "list":
		g.mixedLists[name] = true
		g.listElemStr[name] = false
		g.listElemInt[name] = false
	case "set":
		g.mixedSets[name] = true
		g.setElemStr[name] = false
		g.setElemInt[name] = false
	case "dict key", "dict value":
		g.mixedDicts[name] = true
		g.dictKeyStr[name] = false
		g.dictKeyInt[name] = false
		g.dictValStr[name] = false
		g.dictValInt[name] = false
	default:
		return false
	}
	g.heapUsed = true
	fmt.Fprintf(b, "  call void @rt_mark_estr(i32 %s, i32 8)\n", handle)
	return true
}

// mixedDictIndexRead reports when ix reads `d[k]` out of a dict whose values mix kinds — the same
// question mixedIndexRead asks of lists, one container on for later (roadmap L11.1 (1b)). The
// answer only matters where the value's kind is observable (print, a binding); an arithmetic use
// of such a value is the refusal mixedReadErr names.
func (g *irGen) mixedDictIndexRead(ix *Index) (string, bool) {
	if ix == nil {
		return "", false
	}
	nm, ok := ix.Obj.(*Name)
	if !ok || !g.mixedDicts[nm.Value] {
		return "", false
	}
	switch ix.Idx.(type) {
	case *Name, *IntLit, *StrLit, *FloatLit:
		// A float key belongs here: its payload is a box handle, and the untagged read compares
		// payloads, so it would answer with whichever entry happened to hold the same little
		// integer. The tagged read is the only one that can find a float key (roadmap L11.1, ADR 0233).
		return nm.Value, true
	}
	return "", false
}

// mixedKindErr is the diagnostic every mixed-container site reports.
func mixedKindErr(what string) error {
	return fmt.Errorf("codegen: a compiled %s holds either strings or numbers, not both; the interpreter allows mixing — a compiled container records one element kind, so heterogeneous contents need per-element tagging (roadmap Gap J.6)", what)
}

// recordElemKind notes that a container variable holds strings (isStr) or numbers in a given
// position, and refuses the case where it has already been told the opposite. The compiled
// container records one element kind per position, so mixing would print an integer through the
// string table — an honest diagnostic beats that (roadmap Gap J.6, ADR 0166).
func (g *irGen) recordElemKind(name, slot string, isStr bool, e Expr) error {
	if e != nil && !g.containerKindProvable(e) {
		return fmt.Errorf("codegen: cannot put %s in a compiled %s: the compiler cannot prove one kind for it, because the function it calls hands back text on one path and a number on another. print asks that question when it prints and gets it right; a container slot is labelled once, and labelling it either way misprints the other (the interpreter answers this program; per-element asking is the tagged value word, roadmap L11.1, ADR 0232)", g.exprSummary(e), containerSlotLabel(slot))
	}
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

// isContainerExpr reports the expressions whose value the compiled backend knows to be a heap
// container: a container variable, a container literal, or an empty constructor call. It is a
// static question, answered by the same maps that choose the printer for print (ADR 0188) — and
// for the same reason: an i32 that is a handle cannot be treated as a number.
func (g *irGen) isContainerExpr(e Expr) bool {
	switch n := e.(type) {
	case *ListLit, *SetLit, *DictLit:
		return true
	case *Name:
		return g.listVars[n.Value] || g.mixedLists[n.Value] || g.runtimeDicts[n.Value] || g.runtimeSets[n.Value]
	case *Index:
		// An element that holds another container is a container: the slot stores its handle, and
		// the tag the builder wrote says so (ADR 0239). Reading it is the question this answers —
		// `for row in xs[0]`, `xs[0] == [1, 2]`, `2 in xs[0]` — and the answer is only yes when the
		// tag is visible, which is the same promise containerHandleOf asks for before it emits a
		// load (roadmap L11.1, ADR 0241).
		_, ok := g.staticContainerKind(n)
		return ok
	}
	_, ok := emptyContainerLiteral(e)
	return ok
}

// staticContainerKind is the kind question asked without emitting anything: what would the handle
// this expression denotes be, if the compiler is willing to say. It is a separate answer from
// containerHandleOf because the comparison, the membership test and the loop need to *decide* before
// they are allowed to build, and deciding must not put instructions in a module that then refuses.
func (g *irGen) staticContainerKind(e Expr) (string, bool) {
	switch n := e.(type) {
	case *ListLit:
		return "list", true
	case *DictLit:
		return "dict", true
	case *SetLit:
		return "set", true
	case *Name:
		switch {
		case g.runtimeDicts[n.Value]:
			return "dict", true
		case g.runtimeSets[n.Value]:
			return "set", true
		case g.listVars[n.Value] || g.mixedLists[n.Value]:
			return "list", true
		}
		if lit, ok := g.containerLits[n.Value]; ok {
			return g.staticContainerKind(lit)
		}
		return "", false
	case *Index:
		if _, ok := g.staticContainerKind(n.Obj); !ok {
			return "", false
		}
		tg, seen := g.staticSlotTag(n.Obj, n.Idx)
		if !seen {
			return "", false
		}
		switch tg {
		case int32(TagList):
			return "list", true
		case int32(TagDict):
			return "dict", true
		case int32(TagSet):
			return "set", true
		}
	}
	return "", false
}

// containerHandleOf answers the handle an expression denotes when that expression is a container:
// a literal, a container variable, a comprehension, or an element read whose slot holds another
// container — `xs[0]`, `d["a"]`, `m[0][1]`, `t[0][0][0]`. The nested case is what the tagged value
// word was for: ADR 0239 put the inner object's handle in the slot, so reading a container out of a
// container is a load and not a new representation. The `kind` answer is "list"/"dict"/"set" when
// the compiler can see it and "" when only the object knows, in which case the helper it is handed
// to asks the object's own `@heap` kind — never a guess, because a payload read as a handle while it
// holds a number is a wrong answer wearing another object's bits (ADR 0233, roadmap L11.1, ADR 0241).
func (g *irGen) containerHandleOf(b *strings.Builder, e Expr) (h, kind string, ok bool) {
	switch n := e.(type) {
	case *ListLit:
		hh, err := g.containerOperand(b, e)
		if err != nil {
			return "", "", false
		}
		return hh, "list", true
	case *SetLit:
		hh, err := g.containerOperand(b, e)
		if err != nil {
			return "", "", false
		}
		return hh, "set", true
	case *DictLit:
		hh, err := g.containerOperand(b, e)
		if err != nil {
			return "", "", false
		}
		return hh, "dict", true
	case *Comp:
		hh, err := g.containerOperand(b, e)
		if err != nil {
			return "", "", false
		}
		return hh, "", true
	case *Name:
		switch {
		case g.runtimeDicts[n.Value]:
			kind = "dict"
		case g.runtimeSets[n.Value]:
			kind = "set"
		case g.listVars[n.Value] || g.mixedLists[n.Value]:
			kind = "list"
		default:
			return "", "", false
		}
		hh, err := g.containerOperand(b, e)
		if err != nil {
			return "", "", false
		}
		return hh, kind, true
	case *Index:
		// A read is a container only when the tag says so, and the tag is available statically
		// when the compiler can walk the literal: `xs = [[1, 2]]` says slot 0 is a list, so
		// reading it out names the object the print, the length and the comparison will ask. When
		// it cannot see — `xs = f()`, a comprehension, a loop — this declines, and the caller's
		// refusal stands, because a payload read as a handle while it holds a number prints
		// whatever object happens to share its index (ADR 0233).
		tg, seen := g.staticSlotTag(n.Obj, n.Idx)
		if !seen {
			return "", "", false
		}
		base, baseKind, okBase := g.containerHandleOf(b, n.Obj)
		if !okBase {
			return "", "", false
		}
		val, _, okRead := g.containerSlotRead(b, base, baseKind, n.Idx, n.Span())
		if !okRead {
			return "", "", false
		}
		inner := ""
		switch tg {
		case int32(TagList):
			inner = "list"
		case int32(TagDict):
			inner = "dict"
		case int32(TagSet):
			inner = "set"
		default:
			return "", "", false
		}
		return val, inner, true
	}
	return "", "", false
}

// staticNumericElem answers the element a subscript denotes *when the answer is a number the compiler
// can see*: the container is one its literal still describes, and the element is a literal int, float
// or bool. That is the numeric half of the promise ADR 0241 grants a container read — the slot holds
// this value because nothing changed the object — so a numeric context can compile the element itself
// instead of asking for a tag it does not need. `xs = [1, "a"]; print(xs[0] + 1)` is an addition on 1,
// and `xs = [1.5, "a"]; print(xs[0] * 2)` is the float path the language already has (roadmap L11.1,
// ADR 0243).
//
// Literals only. An element that is a *name* would be read at the point of use, and the name may have
// been rebound since the container was built — `[a, "b"]` built while a=1 and printed after a=5 must
// still answer 1, which means reading the slot, not the variable. A text, None or container element
// answers false too: the tag is precisely what makes those print correctly, and arithmetic on them is
// CPython's TypeError, which this backend declines rather than guesses.
func (g *irGen) staticNumericElem(ix *Index) (Expr, bool) {
	el, ok := g.staticElemExpr(ix.Obj, ix.Idx)
	if !ok || el == nil {
		return nil, false
	}
	if !g.isLiteralNumExpr(el) {
		return nil, false
	}
	return el, true
}

// numericElemUse compiles the numeric slot of a literal-backed container for a value
// position. Reading the slot and compiling the expression that built it are the same
// number, so a number in a mixed or nested container does the arithmetic of the number
// it is instead of being refused as an untagged payload (roadmap L11.1, ADR 0243).
func (g *irGen) numericElemUse(b *strings.Builder, n *Index) (string, bool) {
	el, ok := g.staticNumericElem(n)
	if !ok {
		return "", false
	}
	v, err := g.value(b, el)
	if err != nil {
		return "", false
	}
	return v, true
}

// numericSlotUse is the second door a numeric read of a mixed container's slot can go through.
// ADR 0243's door reads the *element expression*, so it needs a constant index; this one needs
// only the tag list: when the literal that built the container is still the whole story about its
// slots (ADR 0241's promise, the same record ADR 0249 reads) and every slot in it is a plain
// number or a bool, then whatever the program computes as the index, the word the slot holds is
// the number the arithmetic wants. A float among the slots closes the door — there the payload is
// a @float_box handle, and converting it is ADR 0249's job, not this one. Without this door,
// making bool a taggable element kind would have taken `xs = [True, 1]` / `i = 0` /
// `print(xs[i] + 1)` from a correct 2 to a refusal: a landing may not break a green promise
// (roadmap L11.1, Gap R.112, ADR 0259).
func (g *irGen) numericSlotUse(b *strings.Builder, name string, ix *Index) (string, bool) {
	if !g.allocd[name] {
		return "", false // no slot to load: the escape analysis kept this one as a global
	}
	tags, ok := g.orderSlotTags(name)
	if !ok {
		return "", false
	}
	for _, t := range tags {
		if t != int32(TagInt) && t != int32(TagBool) {
			return "", false
		}
	}
	g.heapUsed = true
	idxOp := "0"
	if k, isConst := g.foldConstInt(ix.Idx); isConst {
		idxOp = strconv.FormatInt(k, 10)
	} else {
		v, err := g.value(b, ix.Idx)
		if err != nil || v == "" {
			return "", false
		}
		idxOp = v
	}
	h := g.newTmp()
	fmt.Fprintf(b, "  %s = load i32, i32* %%%s\n", h, "_"+name)
	pos := g.normalizeIndex(b, h, idxOp, ix.Span())
	v := g.newTmp()
	fmt.Fprintf(b, "  %s = call i32 @rt_get_elem(i32 %s, i32 %s)\n", v, h, pos)
	return v, true
}

// isLiteralNumExpr is the question the numeric read may not answer about a name: int, float and bool
// literals (and their parenthesised or negated forms) are values, so the element the literal wrote and
// the value the slot holds cannot disagree.
func (g *irGen) isLiteralNumExpr(e Expr) bool {
	switch n := e.(type) {
	case *IntLit, *FloatLit, *BoolLit:
		return true
	case *UnOp:
		return n.Op == "-" && g.isLiteralNumExpr(n.X)
	}
	return false
}

// runtimeSlotPair reads the (payload, tag) pair of `base[key]` when `base` is a container the program
// holds **at run time** — a list appended to, a dict assigned into — so no literal any longer describes
// its slots. The static door (containerSlotRead, licensed by ADR 0241's compile-time promise) refuses
// these, and this is the other half of that promise: the object's own tag array answers, because the
// builder wrote a tag with every slot it filled (ADR 0187's rule, one write per (payload, tag)).
//
// The bounds check, the negative index, and the KeyError-on-a-missing-key come from containerSlotRead
// itself, so the traps here are the same ones an ordinary read raises (ADR 0210, ADR 0189). Only a
// variable with a real alloca is asked: a name the escape analysis kept as a compile-time list has no
// slot to load, and emitting the load is the module `llc` rejects (ADR 0192).
func (g *irGen) runtimeSlotPair(b *strings.Builder, ix *Index) (val, tag string, ok bool) {
	nm, isName := ix.Obj.(*Name)
	if !isName || !g.allocd[nm.Value] {
		return "", "", false
	}
	n := nm.Value
	kind := ""
	switch {
	case g.listVars[n] || g.mixedLists[n]:
		kind = "list"
	case g.runtimeDicts[n] || g.mixedDicts[n]:
		kind = "dict"
	default:
		return "", "", false
	}
	g.heapUsed = true
	h := g.newTmp()
	fmt.Fprintf(b, "  %s = load i32, i32* %%%s\n", h, "_"+n)
	return g.containerSlotRead(b, h, kind, ix.Idx, ix.Span())
}

// isTaggedSlotRead reports whether `e` is a read of a slot whose kind the **object** carries rather
// than the compiler's notebook: an element of a container whose slots describe themselves, or a
// slot of a run-time container reached through an index/key the program computes. Those are the
// reads whose untagged lowering refuses with "this context needs a single static kind", so the
// only answer available is the (payload, tag) pair the builder wrote beside the payload
// (ADR 0187), and a use that wants the value has to ask for the pair (roadmap L11.1, Gap R.79).
//
// It deliberately excludes the read ADR 0241 already licenses — a constant position of a container
// the literal still describes, which has its own checked read and ADR 0243's numeric fold — so
// this door opens exactly where the ordinary path runs out.
func (g *irGen) isTaggedSlotRead(e Expr) bool {
	ix, ok := e.(*Index)
	if !ok {
		return false
	}
	if _, isRead := ix.Obj.(*Index); isRead {
		// A read of a read — `xs[0][0]`, `d["a"][1]`, `xs[0][1][2]`. The object below describes its own
		// slots, so the pair the run-time door answers with is the only value this side has; where the
		// literal still licenses the static read, that door keeps it (ADR 0251, roadmap L11.1).
		return g.runtimeSlotReadOf(ix) && !g.staticSideIsReadable(ix.Obj)
	}
	nm, isName := ix.Obj.(*Name)
	if !isName || !g.allocd[nm.Value] {
		return false
	}
	if g.isMixedContainer(nm.Value) {
		return true
	}
	if !g.listVars[nm.Value] && !g.runtimeDicts[nm.Value] {
		return false
	}
	if _, isConst := g.foldConstInt(ix.Idx); isConst {
		return false
	}
	if _, isConstText := stringConstOf(ix.Idx); isConstText {
		return false
	}
	return true
}

// comparePair lowers one side of an equality to the (payload, tag) pair it denotes: the slot's own
// pair when the side is a read the object describes — one level deep or ten, which is the shape a
// container the program built rather than spelled out presents (ADR 0251) — and otherwise the pair an
// expression whose kind the compiler can prove carries (taggedOperand, ADR 0232). Declining is the
// caller's signal to leave the ordinary path in charge.
func (g *irGen) comparePair(b *strings.Builder, e Expr) (val, tag string, ok bool) {
	if g.isTaggedSlotRead(e) {
		if _, isRead := e.(*Index).Obj.(*Index); isRead {
			v, t, okPair := g.taggedSlotPair(b, e.(*Index))
			if !okPair {
				return "", "", false
			}
			return v, t, true
		}
		v, t, okPair := g.runtimeSlotPair(b, e.(*Index))
		if !okPair {
			return "", "", false
		}
		return v, t, true
	}
	return g.taggedOperand(b, e)
}

// isTaggedCompareSide is the door's question: is this side something whose kind the compiled
// backend cannot state on its own — a variable bound from a tagged read, or a slot read the object
// has to describe? Neither side answering is the ordinary, statically-typed comparison.
func (g *irGen) isTaggedCompareSide(e Expr) bool {
	if nm, ok := e.(*Name); ok && g.taggedVars[nm.Value] {
		return true
	}
	return g.isTaggedSlotRead(e)
}

// taggedCompareOperandErr is what the equality door says about the one shape it will not answer: a
// slot whose kind only the object knows, compared with an expression whose kind the compiler cannot
// prove — typically a call that hands back text on one path and a number on another. The choices are
// a refusal and a comparison of two words that mean different things, and the second one is how
// `xs[0] == f()` answered true because f returned the text whose interned index is the number in
// the slot. ADR 0232 settled this question for membership tests; this is the same rule at the
// comparison (roadmap L11.1).
func (g *irGen) taggedCompareOperandErr(unknown, tagged Expr) error {
	where := "a value whose tag the object carries"
	switch n := tagged.(type) {
	case *Index:
		if nm, isName := n.Obj.(*Name); isName {
			where = "the slots of " + nm.Value
		}
	case *Name:
		where = "the tag " + n.Value + " carries"
	}
	return fmt.Errorf("codegen: comparing %s with %s needs a value whose kind the compiler can prove: the tag is what decides that question, and an untagged compare of the two words answers it with whichever number happens to sit in the slot (roadmap L11.1, ADR 0232)", g.exprSummary(unknown), where)
}

// lenOfTaggedSlot answers len() of a slot whose tag the compiler carries but cannot read: the tag goes
// with the payload to the check, exactly as ADR 0187 requires, and decides which question the object is
// asked. A text slot is measured in characters, a container slot in its own entries through
// rt_heap_len — which dispatches on the object's record, not on what the builder claimed — and anything
// that is not measurable raises what CPython raises, per kind, rather than answering 0.
func (g *irGen) lenOfTaggedSlot(b *strings.Builder, payload, tag string, sp Span) string {
	g.heapUsed = true
	badBlock := func(t int32, typeName, msg string) {
		cmp := g.newTmp()
		fmt.Fprintf(b, "  %s = icmp eq i32 %s, %d\n", cmp, tag, t)
		g.markI1(cmp)
		bad, next := g.newLabel("len.bad"), g.newLabel("len.chk")
		fmt.Fprintf(b, "  br i1 %s, label %%%s, label %%%s\n", cmp, bad, next)
		fmt.Fprintf(b, "%s:\n", bad)
		g.raiseTo(b, exnCode(typeName), typeName, msg, sp)
		fmt.Fprintf(b, "%s:\n", next)
	}
	badBlock(int32(TagInt), "TypeError", "object of type 'int' has no len()")
	badBlock(int32(TagFloat), "TypeError", "object of type 'float' has no len()")
	badBlock(int32(TagBool), "TypeError", "object of type 'bool' has no len()")
	badBlock(int32(TagNone), "TypeError", "object of type 'NoneType' has no len()")
	// What is left is text, a container, or a kind this language has not grown yet (tuple, instance).
	// The measurable tags ask their own question; everything else is told it has no length rather
	// than being measured as if it were a container, which would read a word that means nothing.
	parts := make([]string, 0, 4)
	for _, t := range []int32{int32(TagStr), int32(TagList), int32(TagDict), int32(TagSet)} {
		c := g.newTmp()
		fmt.Fprintf(b, "  %s = icmp eq i32 %s, %d\n", c, tag, t)
		g.markI1(c)
		parts = append(parts, c)
	}
	acc := parts[0]
	for _, p := range parts[1:] {
		or := g.newTmp()
		fmt.Fprintf(b, "  %s = or i1 %s, %s\n", or, acc, p)
		g.markI1(or)
		acc = or
	}
	okL, noL, strL, contL, doneL := g.newLabel("len.ok"), g.newLabel("len.none"), g.newLabel("len.str"), g.newLabel("len.cont"), g.newLabel("len.done")
	fmt.Fprintf(b, "  br i1 %s, label %%%s, label %%%s\n", acc, okL, noL)
	fmt.Fprintf(b, "%s:\n", noL)
	g.raiseTo(b, exnCode("TypeError"), "TypeError", "object has no len()", sp)
	fmt.Fprintf(b, "%s:\n", okL)
	isStr := g.newTmp()
	fmt.Fprintf(b, "  %s = icmp eq i32 %s, %d\n", isStr, tag, int32(TagStr))
	g.markI1(isStr)
	fmt.Fprintf(b, "  br i1 %s, label %%%s, label %%%s\n", isStr, strL, contL)
	fmt.Fprintf(b, "%s:\n", strL)
	sl := g.newTmp()
	fmt.Fprintf(b, "  %s = call i32 @rt_str_len(i32 %s)\n", sl, payload)
	fmt.Fprintf(b, "  br label %%%s\n", doneL)
	fmt.Fprintf(b, "%s:\n", contL)
	hl := g.newTmp()
	fmt.Fprintf(b, "  %s = call i32 @rt_heap_len(i32 %s)\n", hl, payload)
	fmt.Fprintf(b, "  br label %%%s\n", doneL)
	fmt.Fprintf(b, "%s:\n", doneL)
	res := g.newTmp()
	fmt.Fprintf(b, "  %s = phi i32 [ %s, %%%s ], [ %s, %%%s ]\n", res, sl, strL, hl, contL)
	return res
}

// lenOfSlotArgument answers `len(e)` when the argument is a subscript: the pair comes out of whichever
// door owns the read, and the tag decides which question the object is asked — characters of a text,
// entries of a container, and CPython's `object of type 'int' has no len()` for everything else
// (roadmap L11.1, ADR 0246 and ADR 0251).
func (g *irGen) lenOfSlotArgument(b *strings.Builder, e Expr) (string, bool) {
	ix, isIx := e.(*Index)
	if !isIx {
		return "", false
	}
	if v, t, okPair := g.runtimeSlotPair(b, ix); okPair {
		return g.lenOfTaggedSlot(b, v, t, ix.Span()), true
	}
	if v, t, okPair := g.taggedSlotPair(b, ix); okPair {
		return g.lenOfTaggedSlot(b, v, t, ix.Span()), true
	}
	return "", false
}

// slotReadRefusal is what `len(xs[0])`, `xs[0][1]` and friends say when the slot cannot be reached.
// The generic "needs an inline literal" sent an agent looking at a shape that is fine: the program
// reads a container through a name, and what ran out is the *promise* about that name — it was
// rebound, mutated, or handed to code this pass cannot see, so the literal it was bound to no longer
// describes the object's slots. Naming that is the difference between a dead end and a rewrite an
// agent can act on (roadmap L11.1, ADR 0241).
func (g *irGen) slotReadRefusal(e Expr, ask string) error {
	ix, ok := e.(*Index)
	if !ok {
		return fmt.Errorf("%s requires an inline list/dict/set literal", ask)
	}
	nm, okName := baseName(ix)
	if !okName {
		return fmt.Errorf("%s requires an inline list/dict/set literal", ask)
	}
	// Is the thing being subscripted (or measured) a slot that holds a scalar? Then the program is not
	// asking for a tag it happens to lack — it is subscripting a number, which CPython answers with a
	// TypeError. Say that rather than the tagged-value story, which is about a different shape and would
	// send the reader off after the wrong thing.
	if tg, seen := g.staticSlotTagOf(ix.Obj); seen && !g.containerTag(tg) {
		return fmt.Errorf("%s reaches past a %s in %s: the slot holds a %s, not a container, and there is no slot below it. CPython answers with TypeError: '%s' object is not subscriptable; this backend declines rather than read that payload as a handle (roadmap L11.1, ADR 0241)",
			ask, g.tagName(tg), nm, g.tagName(tg), strings.ToLower(g.tagName(tg)))
	}
	if _, provable := g.containerLits[nm]; provable {
		// The read itself is licensed — the slot says what it holds — but this context asked for a bare
		// i32, and a payload without its tag is a number wearing another object's bits. Say which half
		// of the work this is, rather than blaming the shape of the source.
		return fmt.Errorf("%s cannot use an element of %s as a plain number: the payload only means something with its tag, and this context wants one i32 with no tag attached. print(...), ==, `in`, len and a further subscript are answered; arithmetic on an element needs the tagged value word still owed (roadmap L11.1, ADR 0241)", ask, nm)
	}
	if g.containerNamed(nm) {
		// The sentence has to be true about the other contexts too (Gap R.38). Asking the object is what
		// answers them — its tag array says what each slot holds, whether or not a literal ever did — and
		// `in` is deliberately not in the list, because a membership test needs the haystack's *kind*
		// rather than its tag, which is the shape measured as owed beside this row.
		return fmt.Errorf("%s cannot reach into %s's slots: the name was rebound, mutated, or handed to code this pass cannot see, so the literal it was bound to no longer says what the slots hold; print(...), ==, len and a further subscript are answered by asking the object, which carries the tags its writers left (roadmap L11.1, ADR 0241, ADR 0251)", ask, nm)
	}
	return fmt.Errorf("%s requires an inline list/dict/set literal", ask)
}

// containerNamed asks whether a name holds a container of any shape — the question that separates
// "this program never had a container" from "this program's container escaped the analysis".
func (g *irGen) containerNamed(name string) bool {
	return g.listVars[name] || g.mixedLists[name] || g.mixedDicts[name] || g.runtimeDicts[name] ||
		g.mixedSets[name] || g.runtimeSets[name]
}

// staticSlotTagOf answers the tag of a *read*: the tag of the slot `xs[0]` denotes is the tag of slot 0
// of the object `xs` denotes. It is how a refusal knows whether the expression being subscripted is a
// container at all — subscripting an element that holds a number is a TypeError in CPython, not a
// missing tag (roadmap L11.1, ADR 0241).
func (g *irGen) staticSlotTagOf(e Expr) (int32, bool) {
	if ix, ok := e.(*Index); ok {
		return g.staticSlotTag(ix.Obj, ix.Idx)
	}
	if nm, ok := e.(*Name); ok {
		if lit, known := g.containerLits[nm.Value]; known {
			_, isList := lit.(*ListLit)
			_ = isList
			return int32(TagList), true
		}
	}
	return 0, false
}

// containerTag is the tag family a slot must hold for a subscript, a length or an iteration to mean
// anything: below it there is no slot at all.
func (g *irGen) containerTag(tg int32) bool {
	return tg == int32(TagList) || tg == int32(TagDict) || tg == int32(TagSet)
}

// tagName names a tag in a diagnostic. A refusal that says "tag 0" tells a reader nothing; the word is
// what makes the message actionable, and it is the same word CPython uses in its own TypeError.
func (g *irGen) tagName(tg int32) string {
	switch tg {
	case int32(TagInt):
		return "int"
	case int32(TagFloat):
		return "float"
	case int32(TagBool):
		return "bool"
	case int32(TagNone):
		return "NoneType"
	case int32(TagStr):
		return "str"
	case int32(TagList):
		return "list"
	case int32(TagDict):
		return "dict"
	case int32(TagSet):
		return "set"
	}
	return "value"
}

// bindTaggedVar binds a (payload, tag) pair to a variable: the value slot, the tag slot, the bookkeeping
// that makes dropping the old binding safe, and the record that tells print to dispatch on the tag rather
// than guess a kind. Every binding of a tagged element goes through one door for this reason — three
// shapes (a dict value, a list element, a slot of a container reached through a slot) bind the same pair,
// and the third must not drift from the first two (roadmap L11.1, ADR 0187, ADR 0241).
func (g *irGen) bindTaggedVar(b *strings.Builder, name, val, tag string) {
	if g.listVars[name] || g.runtimeDicts[name] || g.runtimeSets[name] {
		g.emitFreeOld(b, name)
		if g.allocd[name] {
			g.gcClearRoot(b, name)
		}
	}
	g.listVars[name] = false
	g.runtimeDicts[name] = false
	g.runtimeSets[name] = false
	g.mixedLists[name] = false
	delete(g.strVals, name)
	delete(g.internedVars, name)
	delete(g.noneVars, name)
	if g.floatVars != nil {
		delete(g.floatVars, name)
	}
	if !g.allocd[name] {
		b.WriteString(fmt.Sprintf("  %%%s = alloca i32\n", "_"+name))
		g.allocd[name] = true
	}
	if !g.allocd[name+"_tag"] {
		b.WriteString(fmt.Sprintf("  %%%s_tag = alloca i32\n", "_"+name))
		g.allocd[name+"_tag"] = true
	}
	b.WriteString(fmt.Sprintf("  store i32 %s, i32* %%_%s\n", val, name))
	b.WriteString(fmt.Sprintf("  store i32 %s, i32* %%_%s_tag\n", tag, name))
	g.markBound(b, name) // the tagged slot is written too (ADR 0228)
	g.taggedVars[name] = true
}

// taggedContainerRead reads the slot named by `ix` out of the container `ix.Obj` denotes.
// It applies where the ordinary read path cannot: the object being subscripted is itself a read (`xs[0][1]`,
// `d["a"][0]`, `m[0][1]`, `t[0][0][0]`), so the compiler has no literal to look at and no static
// element kind to compile against. The answer is the (payload, tag) pair every printer and
// comparison in this runtime already consumes, which is what makes an element of a container a value
// instead of a refusal (roadmap L11.1, ADR 0187's rule one level down, ADR 0241).
//
// It declines — ok false, no IR emitted, the caller's existing path or refusal stands — when the
// object is not a container the compiled backend can name a handle for, when the key is not a value
// the tag rules can describe, or when the slot holds one of the scalars: `xs[0][0]` where the element
// is text or a number is a question about subscripting a non-container, and Python answers that with
// a TypeError rather than with an index into the interned string table.
func (g *irGen) taggedContainerRead(b *strings.Builder, ix *Index) (val, tag string, ok bool, err error) {
	if _, isRead := ix.Obj.(*Index); !isRead {
		// A container *variable* or a literal already has its own checked read path, with its own
		// IndexError and KeyError; this helper is for the level below, and taking the cases they
		// handle would leave two answers to one question.
		return "", "", false, nil
	}
	base, baseKind, okHandle := g.containerHandleOf(b, ix.Obj)
	if !okHandle {
		// The container side is not one the compile-time promise covers. It may still be an object:
		// `xs = []` then `xs.append([7, 8])` builds the inner container at run time, so no literal
		// describes the slots and the answer has to come from the object rather than from the
		// compiler's notebook (roadmap L11.1, ADR 0251).
		v, t, okDyn := g.taggedSlotPair(b, ix)
		return v, t, okDyn, nil
	}
	v, t, okRead := g.containerSlotRead(b, base, baseKind, ix.Idx, ix.Span())
	if okRead {
		return v, t, true, nil
	}
	v, t, okDyn := g.runtimeSlotPairDeep(b, ix)
	return v, t, okDyn, nil
}

// taggedSlotPair is the one door that answers "which (payload, tag) pair does this subscript
// denote?". Two doors stand behind it, tried in this order:
//
//   - the **static** door: the container is one the compiler still holds the literal for, so the tag
//     beside the slot is the one that literal's builder wrote. containerSlotRead answers, bounds check
//     and KeyError check included (ADR 0241).
//   - the **run-time** door: the container was *built* rather than spelled out — `xs = []` then
//     `xs.append([7, 8])`, `d = {}` then `d["a"] = [1, 2]` — so the literal no longer says what the
//     slots hold and the object does instead: the tag written beside the outer slot names the kind of
//     object the payload is, and the tag written beside the inner slot says what the answer means
//     (roadmap L11.1, ADR 0251).
//
// Declining means neither door can answer, and the caller's own path — or the refusal that names what
// is missing — stays in charge.
func (g *irGen) taggedSlotPair(b *strings.Builder, ix *Index) (val, tag string, ok bool) {
	if h, kind, okH := g.containerHandleOf(b, ix.Obj); okH {
		if v, t, okRead := g.containerSlotRead(b, h, kind, ix.Idx, ix.Span()); okRead {
			return v, t, true
		}
	}
	return g.runtimeSlotPairDeep(b, ix)
}

// runtimeSlotPairDeep reads a slot out of a container the program built. The bottom of the chain is a
// variable the compiled backend owns the storage for; every level above it is a read whose tag says
// which kind of object its payload names, so `xs[0][1][2]` asks the object three times and each answer
// is a (payload, tag) pair on its way to a printer, a comparison, a length or another subscript
// (roadmap L11.1, ADR 0187's one-write-per-pair rule, read back one level at a time).
//
// The gate (runtimeSlotReadOf) is asked first and the door refuses whatever it would not admit, so the
// two can only disagree in the safe direction: a shape the gate lets through may still be declined
// here, and the caller then keeps its own refusal.
func (g *irGen) runtimeSlotPairDeep(b *strings.Builder, e Expr) (val, tag string, ok bool) {
	ix, isIx := e.(*Index)
	if !isIx || !g.runtimeSlotReadOf(ix) {
		return "", "", false
	}
	if nm, isName := ix.Obj.(*Name); isName {
		// The bottom of the chain: the variable's own slot holds the object, and the tracked kind says
		// which container the handle names — the same read ADR 0246 put behind len(...) of a slot.
		kind := ""
		switch {
		case g.listVars[nm.Value] || g.mixedLists[nm.Value]:
			kind = "list"
		case g.runtimeDicts[nm.Value] || g.mixedDicts[nm.Value]:
			kind = "dict"
		default:
			return "", "", false
		}
		g.heapUsed = true
		h := g.newTmp()
		fmt.Fprintf(b, "  %s = load i32, i32* %%%s\n", h, "_"+nm.Value)
		return g.containerSlotRead(b, h, kind, ix.Idx, ix.Span())
	}
	base, baseTag, okBase := g.runtimeSlotPairDeep(b, ix.Obj)
	if !okBase {
		return "", "", false
	}
	return g.slotReadUnderTag(b, base, baseTag, ix)
}

// runtimeSlotReadOf is the shape the run-time door handles: a subscript whose container side is a
// variable the compiled backend holds a heap container for, or another subscript like that. It is the
// gate every caller asks before opening the door, and the door asks it too — ADR 0249's lesson is that
// a gate and a door testing different things ship a refusal nobody measured.
func (g *irGen) runtimeSlotReadOf(e Expr) bool {
	ix, isIx := e.(*Index)
	if !isIx {
		return false
	}
	return g.runtimeContainerSide(ix.Obj)
}

// runtimeContainerSide asks whether the side a slot is read out of is an object the run time owns: a
// list the program appended to, a dict the program assigned into, or a read of one of those. A set
// variable is not a side here because it has its own (untagged) read path; a set in a *slot* is a side
// like any other, and the arm the tag reaches asks it the membership question the documented gusty
// extension promises for a set subscript.
func (g *irGen) runtimeContainerSide(e Expr) bool {
	switch n := e.(type) {
	case *Name:
		// A name the escape analysis kept as a compile-time value has no slot to load; emitting the
		// load anyway is the module llc rejects (ADR 0192).
		if !g.allocd[n.Value] {
			return false
		}
		return g.listVars[n.Value] || g.mixedLists[n.Value] || g.runtimeDicts[n.Value] || g.mixedDicts[n.Value]
	case *Index:
		return g.runtimeSlotReadOf(n)
	}
	return false
}

// staticSideIsReadable mirrors the question the static door asks, without emitting, so the gate over
// the run-time door can stand aside where ADR 0241's promise already answers: two doors, one answer.
func (g *irGen) staticSideIsReadable(side Expr) bool {
	switch n := side.(type) {
	case *ListLit, *DictLit, *SetLit, *Comp:
		return true
	case *Name:
		return g.listVars[n.Value] || g.mixedLists[n.Value] || g.runtimeDicts[n.Value] ||
			g.mixedDicts[n.Value] || g.runtimeSets[n.Value]
	case *Index:
		tg, seen := g.staticSlotTag(n.Obj, n.Idx)
		return seen && g.containerTag(tg)
	}
	return false
}

// slotReadUnderTag reads `base[key]` where `base` is the payload of a slot the compiler could not
// read. The tag the builder wrote beside that payload says which kind of object the payload names,
// and so which question the object is asked: a list slot is read by position, normalised and
// bounds-checked like any other positional subscript (ADR 0210's rule, reached through the tag rather
// than around it); a dict slot by the (payload, tag) key, with the KeyError a missing one raises;
// a text slot answers a one-character string, because `s[i]` is a string and not a byte (ADR 0225);
// and a set slot is asked as a member, which is the documented gusty extension for a set subscript
// (docs/language.md § Dicts & sets) applied one level down rather than reinvented there. Anything the
// tag reports that has nothing to read — a number, None, a class the language has not grown
// `__getitem__` for — raises the sentence CPython raises, naming the kind the tag reports, instead of
// reading the payload as a handle and answering with whatever object happens to share its number
// (roadmap L11.1, ADR 0251).
//
// Every arm that produces a value ends at the same merge through its own tail block, and the raise
// arms end at the handler instead, so the merge's phi has only value-producing predecessors —
// ADR 0205's rule, the same one ADR 0250's ordering arms follow.
func (g *irGen) slotReadUnderTag(b *strings.Builder, base, baseTag string, ix *Index) (val, tag string, ok bool) {
	key, keyTag, okKey := g.taggedOperand(b, ix.Idx)
	if !okKey {
		return "", "", false
	}
	g.heapUsed = true
	g.floatFmtUsed = true
	sp := ix.Span()
	listArm, dictArm, strArm, setArm := g.newLabel("slot.lst"), g.newLabel("slot.dct"), g.newLabel("slot.str"), g.newLabel("slot.set")
	tailL, tailD, tailS, tailSet := g.newLabel("slot.lend"), g.newLabel("slot.dend"), g.newLabel("slot.send"), g.newLabel("slot.setend")
	merge := g.newLabel("slot.merge")

	// The tag chain: one icmp and one branch per kind. A side the compiler cannot read has no arm it
	// can fold away — every one of these can run, which is what makes the phi legal and the answers
	// three.
	kinds := []struct {
		tag int32
		arm string
	}{{int32(TagList), listArm}, {int32(TagDict), dictArm}, {int32(TagStr), strArm}, {int32(TagSet), setArm}}
	for _, k := range kinds {
		c := g.newTmp()
		fmt.Fprintf(b, "  %s = icmp eq i32 %s, %d\n", c, baseTag, k.tag)
		g.markI1(c)
		chk := g.newLabel("slot.chk")
		fmt.Fprintf(b, "  br i1 %s, label %%%s, label %%%s\n", c, k.arm, chk)
		fmt.Fprintf(b, "%s:\n", chk)
	}
	traps := []struct {
		tag  int32
		name string
	}{
		{int32(TagInt), "int"},
		{int32(TagFloat), "float"},
		{int32(TagBool), "bool"},
		{int32(TagNone), "NoneType"},
	}
	trapBlock := make([]string, len(traps))
	for i := range traps {
		trapBlock[i] = g.newLabel("slot.bad")
	}
	for i, t := range traps {
		c := g.newTmp()
		fmt.Fprintf(b, "  %s = icmp eq i32 %s, %d\n", c, baseTag, t.tag)
		g.markI1(c)
		chk := g.newLabel("slot.chk")
		fmt.Fprintf(b, "  br i1 %s, label %%%s, label %%%s\n", c, trapBlock[i], chk)
		fmt.Fprintf(b, "%s:\n", chk)
	}
	genericBad := g.newLabel("slot.badg")
	fmt.Fprintf(b, "  br label %%%s\n", genericBad)
	for i, t := range traps {
		fmt.Fprintf(b, "%s:\n", trapBlock[i])
		g.raiseTo(b, exnCode("TypeError"), "TypeError", "'"+t.name+"' object is not subscriptable", sp)
	}
	fmt.Fprintf(b, "%s:\n", genericBad)
	g.raiseTo(b, exnCode("TypeError"), "TypeError", "object is not subscriptable", sp)

	// ---- the list arm: a position, normalised and bounds-checked by the same helper an explicit read
	// uses, and read back with the tag the builder stored beside it.
	fmt.Fprintf(b, "%s:\n", listArm)
	pos := g.normalizeIndex(b, base, key, sp)
	lv := g.newTmp()
	fmt.Fprintf(b, "  %s = call i32 @rt_get_elem(i32 %s, i32 %s)\n", lv, base, pos)
	lt := g.newTmp()
	fmt.Fprintf(b, "  %s = call i32 @rt_tag_of(i32 %s, i32 %s)\n", lt, base, pos)
	fmt.Fprintf(b, "  br label %%%s\n", tailL)

	// ---- the dict arm: the entry whose key is the (payload, tag) pair, and the value one word past it.
	fmt.Fprintf(b, "%s:\n", dictArm)
	g.checkKeyReadTagged(b, base, key, keyTag, sp)
	dv := g.newTmp()
	fmt.Fprintf(b, "  %s = call i32 @rt_dict_get_tagged(i32 %s, i32 %s, i32 %s)\n", dv, base, key, keyTag)
	dt := g.newTmp()
	fmt.Fprintf(b, "  %s = call i32 @rt_dict_value_tag(i32 %s, i32 %s, i32 %s)\n", dt, base, key, keyTag)
	fmt.Fprintf(b, "  br label %%%s\n", tailD)

	// ---- the text arm: a code point, interned like every other one-character string, or the
	// IndexError that says there is no such position.
	fmt.Fprintf(b, "%s:\n", strArm)
	sv := g.rtStrCall(b, "rt_str_char", "i32 "+base, "i32 "+key)
	g.checkStrSentinels(b, sv, "IndexError", "string index out of range", sp, "slotchr")
	fmt.Fprintf(b, "  br label %%%s\n", tailS)

	// ---- the set arm: the subscript is a member the set is asked about, and the member comes back —
	// the gusty extension for a set subscript, read through the (payload, tag) rule the container's own
	// membership tests already use, so a set slot and a set variable answer alike.
	fmt.Fprintf(b, "%s:\n", setArm)
	at := g.newTmp()
	fmt.Fprintf(b, "  %s = call i32 @rt_set_find(i32 %s, i32 %s, i32 %s)\n", at, base, key, keyTag)
	absent := g.newTmp()
	fmt.Fprintf(b, "  %s = icmp slt i32 %s, 0\n", absent, at)
	g.markI1(absent)
	g.branchRaise(b, absent, "KeyError", "not in set", sp, "slotset")
	pv := g.newTmp()
	fmt.Fprintf(b, "  %s = call i32 @rt_get_elem(i32 %s, i32 %s)\n", pv, base, at)
	pt := g.newTmp()
	fmt.Fprintf(b, "  %s = call i32 @rt_tag_of(i32 %s, i32 %s)\n", pt, base, at)
	fmt.Fprintf(b, "  br label %%%s\n", tailSet)

	for _, t := range []string{tailL, tailD, tailS, tailSet} {
		fmt.Fprintf(b, "%s:\n", t)
		fmt.Fprintf(b, "  br label %%%s\n", merge)
	}
	fmt.Fprintf(b, "%s:\n", merge)
	val = g.newTmp()
	fmt.Fprintf(b, "  %s = phi i32 [ %s, %%%s ], [ %s, %%%s ], [ %s, %%%s ], [ %s, %%%s ]\n",
		val, lv, tailL, dv, tailD, sv, tailS, pv, tailSet)
	tag = g.newTmp()
	fmt.Fprintf(b, "  %s = phi i32 [ %s, %%%s ], [ %s, %%%s ], [ %d, %%%s ], [ %s, %%%s ]\n",
		tag, lt, tailL, dt, tailD, int32(TagStr), tailS, pt, tailSet)
	return val, tag, true
}

// membershipOfReadHaystack answers `needle in xs[0]`, `k in d["a"]`, `x in s[0]` — the membership
// test whose haystack is itself an element read. The haystack's handle comes out of the slot, the
// needle comes in as a (payload, tag) pair, and the question is asked of the object by its kind, so
// a list, a dict (whose keys are what `in` tests, as Python says) and a set all answer without the
// compiler having to know which it is (roadmap L11.1, ADR 0241).
func (g *irGen) membershipOfReadHaystack(b *strings.Builder, n *BinOp) (string, bool, error) {
	h, kind, ok := g.containerHandleOf(b, n.R)
	if !ok {
		return "", false, nil
	}
	fn := ""
	switch kind {
	case "list":
		fn = "rt_contains_tagged"
	case "dict":
		fn = "rt_dict_has_tagged"
	case "set":
		fn = "rt_set_contains_tagged"
	default:
		return "", false, nil
	}
	nv, nt, okNeedle := g.taggedOperand(b, n.L)
	if !okNeedle {
		// The needle's kind is not provable, so the untagged compare would match an interned
		// string against the integer it is indexed by (ADR 0232). Declining leaves the caller's
		// refusal, which names the tagged value word as the missing piece.
		return "", false, nil
	}
	g.heapUsed = true
	hit := g.newTmp()
	fmt.Fprintf(b, "  %s = call i32 @%s(i32 %s, i32 %s, i32 %s)\n", hit, fn, h, nv, nt)
	asBool := g.newTmp()
	fmt.Fprintf(b, "  %s = icmp ne i32 %s, 0\n", asBool, hit)
	g.markI1(asBool)
	if n.Op == "not in" {
		inv := g.newTmp()
		fmt.Fprintf(b, "  %s = xor i1 %s, true\n", inv, asBool)
		g.markI1(inv)
		return g.asBoolI32(b, inv), true, nil
	}
	return g.asBoolI32(b, asBool), true, nil
}

// staticSlotTag is the tag the slot at `key` of a container carries, when the compiler can walk the
// container's own literal to it. It is the permission for treating a read as a container: the
// builders wrote that tag with the payload (ADR 0189), so the answer is what the object itself will
// say, and asking it here means the compiled path never has to read a payload as a handle on
// trust (roadmap L11.1, ADR 0241).
func (g *irGen) staticSlotTag(container Expr, key Expr) (int32, bool) {
	el, ok := g.staticElemExpr(container, key)
	if !ok {
		return 0, false
	}
	return g.elemKindTag(el)
}

// staticElemExpr answers what an element read denotes when the compiler can walk the container's
// own literal all the way down: with `xs = [[1, 2], [3, 4]]`, `xs[0]` is the list literal `[1, 2]`,
// and with `m = {0: [1, 2], 1: 3}`, `m[0][1]` is the integer 2. A container reached through a value
// the compiler cannot see — a call, a comprehension, a loop — answers nothing, and the tag inside the
// object is what the runtime helpers read instead (roadmap L11.1, ADR 0241).
func (g *irGen) staticElemExpr(container Expr, key Expr) (Expr, bool) {
	c := container
	for {
		switch n := c.(type) {
		case *Name:
			if lit, ok := g.containerLits[n.Value]; ok {
				c = lit
				continue
			}
			if lit, ok := g.staticLists[n.Value]; ok {
				c = lit
				continue
			}
			if lit, ok := g.staticDicts[n.Value]; ok {
				c = lit
				continue
			}
			if lit, ok := g.staticSets[n.Value]; ok {
				c = lit
				continue
			}
			return nil, false
		case *Index:
			inner, ok := g.staticElemExpr(n.Obj, n.Idx)
			if !ok {
				return nil, false
			}
			c = inner
		case *ListLit:
			k, ok := g.foldConstInt(key)
			if !ok {
				return nil, false
			}
			if k < 0 {
				k += int64(len(n.Elems))
			}
			if k < 0 || k >= int64(len(n.Elems)) {
				return nil, false
			}
			return n.Elems[k], true
		case *DictLit:
			for i, kk := range n.Keys {
				if g.constKeysMatch(kk, key) {
					return n.Vals[i], true
				}
			}
			return nil, false
		default:
			return nil, false
		}
	}
}

// constKeysMatch is the key question of a literal dict read: the same kind and the same value. An
// interned string and the integer it happens to be indexed by are the same bits and different keys
// (ADR 0232), so the kind is checked before the number.
func (g *irGen) constKeysMatch(a, b Expr) bool {
	if ai, ok := a.(*IntLit); ok {
		if isStringExpr(b) {
			return false
		}
		bi, ok2 := g.foldConstInt(b)
		return ok2 && ai.Value == bi
	}
	as, ok := g.stringVal(a)
	if !ok {
		return false
	}
	bs, ok2 := g.stringVal(b)
	return ok2 && as == bs
}

// containerSlotRead reads one slot out of a container the compiler holds a handle for, and returns
// the payload with the tag the slot carries — the pair every printer and comparison in this runtime
// already knows how to read (ADR 0187). A list position is normalised and bounds-checked through the
// same helper an explicit `raise` uses, a dict key is checked through the one that raises KeyError,
// and a set is refused because Python refuses it too ('set' object is not subscriptable). When the
// A list position is normalised and bounds-checked by the same helper an explicit read uses, so
// `xs[0][-1]` counts from the end and `xs[0][5]` raises IndexError (L11.4, ADR 0210); a dict key is
// looked up by the payload-and-tag rule equality uses, and its value is read from the word the key
// was found in — never by treating the key as an index, which is how `d["a"][0]` used to read the
// key back and call it a value (ADR 0189, roadmap L11.1, ADR 0241).
func (g *irGen) containerSlotRead(b *strings.Builder, base, kind string, key Expr, sp Span) (val, tag string, ok bool) {
	if base == "" {
		return "", "", false
	}
	if kind == "set" {
		// CPython: TypeError: 'set' object is not subscriptable. Refusing here is the honest answer,
		// and the row that owns the runtime TypeError says so out loud.
		return "", "", false
	}
	g.heapUsed = true
	if kind == "list" || kind == "dict" {
		kv, kt, okTag := g.taggedOperand(b, key)
		if !okTag {
			return "", "", false
		}
		if kind == "list" {
			// A position, normalised and bounds-checked by the same helper an explicit read uses,
			// so `xs[0][-1]` counts from the end and `xs[0][5]` raises IndexError (L11.4, ADR 0210).
			pos := g.normalizeIndex(b, base, kv, sp)
			val = g.newTmp()
			fmt.Fprintf(b, "  %s = call i32 @rt_get_elem(i32 %s, i32 %s)\n", val, base, pos)
			tag = g.newTmp()
			fmt.Fprintf(b, "  %s = call i32 @rt_tag_of(i32 %s, i32 %s)\n", tag, base, pos)
		} else {
			// A key: found by the payload-and-tag rule equality uses, and the value sits one word
			// past it — rt_get_elem with the key as an index would read the key back and call it a
			// value, which is what rt_dict_get_tagged and rt_dict_value_tag exist to avoid.
			g.checkKeyReadTagged(b, base, kv, kt, sp)
			val = g.newTmp()
			fmt.Fprintf(b, "  %s = call i32 @rt_dict_get_tagged(i32 %s, i32 %s, i32 %s)\n", val, base, kv, kt)
			tag = g.newTmp()
			fmt.Fprintf(b, "  %s = call i32 @rt_dict_value_tag(i32 %s, i32 %s, i32 %s)\n", tag, base, kv, kt)
		}
		return val, tag, true
	}
	// A set has no positions and a value that is not a container has no slots: the caller refuses
	// rather than reading a word that means nothing (CPython raises TypeError for both).
	return "", "", false
}

// containerOperand lowers one operand of a container comparison to its handle. A literal builds
// the runtime object rather than using the compile-time global: the global's layout is a length
// plus an array, not a handle, and handing it to a runtime helper is the invalid-IR shape that
// print used to have (ADR 0188).
func (g *irGen) containerOperand(b *strings.Builder, e Expr) (string, error) {
	if lit, ok := emptyContainerLiteral(e); ok {
		e = lit
	}
	switch n := e.(type) {
	case *ListLit:
		// A list that mixes kinds, or whose elements are floats, Nones or further containers, has
		// no single element kind: it is built tagged, with bit 8 set, so the printer asks each slot.
		// Built untagged instead, a nested [1, "a"] claimed to be the string list and printed its
		// integer as an interned index — [[1, "a"]] came out as [['b', 'a']] (roadmap L11.1).
		if g.taggableMixedList(n) || g.literalNeedsTags(n) {
			return g.heapListFromTagged(b, n)
		}
		return g.heapListFrom(b, n, "")
	case *SetLit:
		return g.heapSetFrom(b, n, "")
	case *DictLit:
		return g.heapDictFrom(b, n, "")
	case *Name:
		g.heapUsed = true
		v := g.newTmp()
		fmt.Fprintf(b, "  %s = load i32, i32* %%_%s\n", v, n.Value)
		return v, nil
	case *Index:
		// An element that holds a container *is* the container: the slot stores the inner object's
		// handle, so the comparison, the membership test and the loop iterator read the slot and
		// pass what is in it straight to the runtime helper that asks the object what it is
		// (roadmap L11.1, ADR 0241).
		h, _, okH := g.containerHandleOf(b, e)
		if !okH {
			return "", fmt.Errorf("codegen: %s does not name a container the compiled backend can prove; reading an element as a container needs the tag the builder wrote (roadmap L11.1, ADR 0241)", exprSnippet(e))
		}
		return h, nil
	case *Comp:
		// A comprehension is a container the same way a literal is. The constant path hands
		// back a folded global (@.lstN / @.setN / @.dictN), whose layout is a length plus an
		// array and not a handle — handing that to a runtime helper is the invalid-IR shape
		// ADR 0188 removed for literals, so it is materialised into the heap here; the runtime
		// path already produced a handle (roadmap L11.7, ADR 0192; Gap J.2, ADR 0234).
		h, err := g.value(b, e)
		if err != nil {
			return "", err
		}
		if lit, ok := g.staticLists[h]; ok {
			return g.heapListFrom(b, lit, "")
		}
		if lit, ok := g.staticSets[h]; ok {
			return g.heapSetFrom(b, lit, "")
		}
		if lit, ok := g.staticDicts[h]; ok {
			return g.heapDictFrom(b, lit, "")
		}
		return h, nil
	}
	return "", fmt.Errorf("codegen: %s is not a container (roadmap L11.1, ADR 0189)", exprSnippet(e))
}

// compareOperand lowers one side of a container comparison: a container through the heap path,
// anything else through the ordinary value path.
func (g *irGen) compareOperand(b *strings.Builder, e Expr, isContainer bool) (string, error) {
	if isContainer {
		return g.containerOperand(b, e)
	}
	return g.value(b, e)
}

// textOrderOperands is the question the ordering door asks: is this an ordering between two values
// the compiler can see are text? Equality of interned texts is an index comparison and is sound,
// because interning is content-addressed; ordering is not, because an index records the order the
// text *arrived*, not the order of its characters (roadmap Gap R.84).
//
// The two predicates below are the ones the print and operation paths already agree on (ADR 0224,
// ADR 0229): an operand that renders as text is an operand that orders as text, or the two doors
// disagree and one of them is quietly wrong.
func (g *irGen) textOrderOperands(n *BinOp) bool {
	switch n.Op {
	case "<", "<=", ">", ">=":
	default:
		return false
	}
	return g.textOrderSide(n.L) && g.textOrderSide(n.R)
}

// textOrderSide is one operand of such an ordering: a literal text, or a value the printer renders
// as text because its word is an index into @str_tab.
func (g *irGen) textOrderSide(e Expr) bool {
	if _, ok := g.stringVal(e); ok {
		return true
	}
	return g.exprIsString(e) || g.printsAsInternedStr(e)
}

// orderSide is what one side of an ordering could turn out to be, once the object is asked.
//
// Two questions decide everything here: can this side report a number, and can it report a text.
// CPython orders two numbers numerically, two texts by their characters, and refuses a number against
// a text with a TypeError that names both types — so an ordering whose sides could each turn out
// either way is a run-time question with three possible answers, and the only thing that settles it is
// the tag the object carries (roadmap L11.1, Gap R.82).
type orderSide struct {
	e         Expr   // the operand itself
	ix        *Index // non-nil when the object has to be asked, i.e. the side is a slot read
	fromTag   bool   // the side's kinds come from the object: no literal describes the slots (Gap R.93)
	canText   bool   // the side can report text
	canNum    bool   // the side can report a number
	numName   string // CPython's name for the side's numbers, "" when they would be a guess
	tagC      int32  // the tag the compiler already knows, when ix is nil
	payload   string // the i32 the value lives in, once read
	tag       string // the i32 tag register, once read
	tagIsText string // the i1 saying that tag is the text tag, once read
	tagIsNum  string // the i1 saying that tag is one of the three number tags
	ok        bool   // the shape is one this door can answer at all
}

// textTest and numberTest are the three answers a side can give to "are you text?" and "are you a
// number?": a register for a slot the object has to be asked about, and a settled yes or no for
// anything the compiler already read. The chain that decides the arms folds on these, so a side read
// off the literal never costs a test at run time (roadmap L11.1, Gap R.82).
func (s *orderSide) textTest() string {
	if s.ix == nil {
		if s.tagC == int32(TagStr) {
			return "true"
		}
		return "false"
	}
	return s.tagIsText
}

func (s *orderSide) numberTest() string {
	if s.ix == nil {
		if s.tagC == int32(TagInt) || s.tagC == int32(TagBool) || s.tagC == int32(TagFloat) {
			return "true"
		}
		return "false"
	}
	return s.tagIsNum
}

func (s *orderSide) definitelyNotText() bool {
	if s.ix == nil {
		return s.tagC != int32(TagStr)
	}
	return !s.canText
}

func (s *orderSide) definitelyNotNumber() bool {
	if s.ix == nil {
		return s.tagC != int32(TagInt) && s.tagC != int32(TagBool) && s.tagC != int32(TagFloat)
	}
	return !s.canNum
}

// orderPickName is the one place that decides whether a side's numbers all answer to the same CPython
// name: a slot that could hold an int or a float would need a different sentence for each, which is a
// guess the compiler is not allowed to make.
func orderPickName(have, want string) string {
	if have == "" {
		return want
	}
	if have == want {
		return have
	}
	return ""
}

// orderSlotTags lists the tags a slot of a container can report, as the literal that built the
// container describes them. It is the same source of truth ADR 0249 reads for the raise arms — the
// elements the program wrote — and when the container was built by a loop instead of written as a
// literal there is no list to read, so the shape is declined rather than guessed
// (ADR 0166's exit-class rule: a refusal the program can read, never an instruction with no operand).
func (g *irGen) orderSlotTags(n string) ([]int32, bool) {
	lit, ok := g.containerLits[n]
	if !ok {
		return nil, false
	}
	var elems []Expr
	switch c := lit.(type) {
	case *ListLit:
		elems = c.Elems
	case *DictLit:
		elems = c.Vals
	default:
		return nil, false
	}
	if len(elems) == 0 {
		return nil, false
	}
	tags := make([]int32, 0, len(elems))
	for _, el := range elems {
		tg, tok := g.elemKindTag(el)
		if !tok {
			return nil, false
		}
		tags = append(tags, tg)
	}
	return tags, true
}

// orderSlotIsObject reports that a side of an ordering is a slot the *object* has to be asked about,
// because no literal describes the slots it can hold: the container was built by `append`/assignment
// rather than spelled out, or the side is itself a read one level below such an object. It is a gate
// only — it emits nothing — and it stands aside wherever ADR 0250's door already reads the kinds off
// the literal, so one shape never gets two doors (roadmap L11.1, Gap R.93).
func (g *irGen) orderSlotIsObject(ix *Index) bool {
	switch side := ix.Obj.(type) {
	case *Name:
		if !g.runtimeContainerSide(ix.Obj) {
			return false
		}
		_, described := g.orderSlotTags(side.Value)
		return !described
	case *Index:
		return g.runtimeSlotReadOf(ix)
	}
	return false
}

// orderShapeOf reads one operand of an ordering: a slot the object has to be asked about, or a literal
// the compiler already knows the tag of.
func (g *irGen) orderShapeOf(e Expr) orderSide {
	s := orderSide{e: e}
	if ix, isIdx := e.(*Index); isIdx {
		if g.orderSlotIsObject(ix) {
			// No literal describes this slot, so the compiler has no list of kinds to read: the side may
			// report any kind the writers can leave in a slot, all three arms stand, and the raise arm
			// becomes a chain that asks the tag which name to put in the sentence. This is the shape the
			// literal door declines and the lowering underneath used to answer with a verdict for a
			// TypeError — `xs = []` / `xs.append(i)` / `print(1 if xs[0] > "a" else 0)` printing `1` for a
			// program CPython crashes (roadmap Gap R.93), and the same question one level down,
			// `xs.append([3, "a"])` / `xs[0][0] > 1`, which ADR 0251's read answers and the ordering did
			// not (roadmap L11.1).
			return orderSide{e: e, ix: ix, fromTag: true, canNum: true, canText: true, ok: true}
		}
		nm, isName := ix.Obj.(*Name)
		if !isName || !g.allocd[nm.Value] {
			return s
		}
		if !(g.listVars[nm.Value] || g.mixedLists[nm.Value] || g.runtimeDicts[nm.Value] || g.mixedDicts[nm.Value]) {
			return s
		}
		tags, known := g.orderSlotTags(nm.Value)
		if !known {
			return s
		}
		s.ix = ix
		// The literal says what was written; the kind maps say what the program has since put in.
		// Both are read, because a container the program grew with append() can report a kind the
		// literal never mentioned, and an ordering that pretends otherwise would print a verdict for
		// the TypeError CPython raises.
		if strMap, numMap, slot := g.kindMapsFor(orderKindSlot(nm.Value, g.runtimeDicts[nm.Value] || g.mixedDicts[nm.Value])); true {
			if strMap[nm.Value] {
				s.canText = true
			}
			if numMap[nm.Value] {
				s.canNum = true
				if s.numName == "" {
					return orderSide{e: e} // numbers are possible but no name is known for them
				}
			}
			_ = slot
		}
		for _, tg := range tags {
			switch tg {
			case int32(TagInt):
				s.canNum, s.numName = true, orderPickName(s.numName, "int")
			case int32(TagBool):
				s.canNum, s.numName = true, orderPickName(s.numName, "bool")
			case int32(TagFloat):
				s.canNum, s.numName = true, orderPickName(s.numName, "float")
			case int32(TagStr):
				s.canText = true
			default:
				return orderSide{e: e} // None, a container: no ordering to emit for it here
			}
		}
		s.ok = (s.canNum || s.canText) && (!s.canNum || s.numName != "")
		return s
	}
	switch n := e.(type) {
	case *StrLit:
		s.canText, s.tagC, s.ok = true, int32(TagStr), true
	case *IntLit:
		s.canNum, s.tagC, s.numName, s.ok = true, int32(TagInt), "int", true
	case *BoolLit:
		s.canNum, s.tagC, s.numName, s.ok = true, int32(TagBool), "bool", true
	case *FloatLit:
		s.canNum, s.tagC, s.numName, s.ok = true, int32(TagFloat), "float", true
	case *Name:
		// A variable the compiler can name a kind for is as settled as a literal: `xs[0] >= i` after
		// `i = 3` asks the object about one side only, which is the shape the gate allows. The question
		// is the one the arithmetic door already asks (`numericUseKind`) and the one the printer asks —
		// a name whose word is an index into @str_tab is text here exactly as it is text there
		// (ADR 0224, ADR 0229), because two doors disagreeing about what an operand is is how a passing
		// suite starts lying (roadmap L11.1, ADR 0252).
		switch k := g.numericUseKind(n); k {
		case "int":
			s.canNum, s.tagC, s.numName, s.ok = true, int32(TagInt), "int", true
		case "float":
			s.canNum, s.tagC, s.numName, s.ok = true, int32(TagFloat), "float", true
		default:
			if g.exprIsString(n) || g.printsAsInternedStr(n) {
				s.canText, s.tagC, s.ok = true, int32(TagStr), true
			}
		}
	}
	return s
}

// taggedOrderApplies is the gate the comparison sites ask: is this an ordering where the object has to
// be asked which pair it is? A slot read on one side, text among the kinds it could report, and at
// least one arm left for the tags to choose between. An ordering that can only ever be two numbers is
// the plain numeric comparison it always was, one with no slot in it at all is ADR 0248's static text
// door or the fold beside it, and one whose verdict the compiler could already settle from the
// literal is left where it was — this door exists for the questions nobody else can answer
// (roadmap L11.1, Gap R.82).
func (g *irGen) taggedOrderApplies(n *BinOp) bool {
	switch n.Op {
	case "<", "<=", ">", ">=":
	default:
		return false
	}
	ls, rs := g.orderShapeOf(n.L), g.orderShapeOf(n.R)
	if !ls.ok || !rs.ok {
		return false
	}
	if ls.ix == nil && rs.ix == nil {
		return false
	}
	// A side whose kind the object reports names itself in the raise sentence, which names *two* types
	// at once; one such side is a chain the arm can emit, two is a cross-product of sentences the
	// compiler would be inventing. So a fromTag side is admitted only against an operand the compiler
	// read itself — which also leaves container-against-container ordering to the door that compares
	// contents, instead of turning Gap R.86's wrong answer into a wrong trap (roadmap L11.1).
	if ls.fromTag && rs.ix != nil || rs.fromTag && ls.ix != nil {
		return false
	}
	needTxt := ls.canText && rs.canText
	needBad := ls.canNum && rs.canText || ls.canText && rs.canNum
	return needTxt || needBad
}

// orderTagOf reads the tag one side reports, together with its payload. A side the compiler already
// read has no tag to ask and leaves the register empty, which the arm tests below read as "settled".
func (g *irGen) orderTagOf(b *strings.Builder, s *orderSide) bool {
	if s.ix == nil {
		return true
	}
	var payload, tag string
	var ok bool
	if s.fromTag {
		// The unified door: the static read if the literal still describes the container, the object's
		// own tags if the program built it instead (ADR 0251).
		payload, tag, ok = g.taggedSlotPair(b, s.ix)
	} else {
		payload, tag, ok = g.runtimeSlotPair(b, s.ix)
	}
	if !ok || payload == "" || tag == "" {
		return false
	}
	s.payload, s.tag = payload, tag
	return true
}

// orderAskTags reads each slot side's tag once, in the block the comparison starts in, and records the
// two tests the arms and the raise sentence branch on.
func (g *irGen) orderAskTags(b *strings.Builder, s *orderSide) {
	if s.ix == nil || s.tag == "" {
		return
	}
	s.tagIsText = g.newTmp()
	fmt.Fprintf(b, "  %s = icmp eq i32 %s, %d\n", s.tagIsText, s.tag, int32(TagStr))
	g.markI1(s.tagIsText)
	s.tagIsNum = g.orderIsNum(b, s.tag)
}

// orderPayload is the i32 the side's value lives in: the slot's payload for a read, the value the
// ordinary lowering produced for everything else.
func (g *irGen) orderPayload(b *strings.Builder, s *orderSide) (string, error) {
	if s.tag != "" {
		return s.payload, nil
	}
	v, err := g.value(b, s.e)
	if err != nil {
		return "", err
	}
	if v == "" {
		return "", g.floatOperandRefusal(s.e)
	}
	return v, nil
}

// orderIsNum tests a tag register against the three number tags.
func (g *irGen) orderIsNum(b *strings.Builder, tag string) string {
	isI := g.newTmp()
	fmt.Fprintf(b, "  %s = icmp eq i32 %s, %d\n", isI, tag, int32(TagInt))
	g.markI1(isI)
	isB := g.newTmp()
	fmt.Fprintf(b, "  %s = icmp eq i32 %s, %d\n", isB, tag, int32(TagBool))
	g.markI1(isB)
	isEither := g.newTmp()
	fmt.Fprintf(b, "  %s = or i1 %s, %s\n", isEither, isI, isB)
	g.markI1(isEither)
	isF := g.newTmp()
	fmt.Fprintf(b, "  %s = icmp eq i32 %s, %d\n", isF, tag, int32(TagFloat))
	g.markI1(isF)
	all := g.newTmp()
	fmt.Fprintf(b, "  %s = or i1 %s, %s\n", all, isEither, isF)
	g.markI1(all)
	return all
}

// orderDoubleTo lifts one side to a double inside the numeric arm. A float slot unboxes out of its
// @float_box object, an int or bool slot converts, and a tag that reports anything else walks to the
// raise arm — which the gate above makes unreachable, but the arm still has to end somewhere legal
// rather than run off the end of the block (ADR 0166's rule against inventing an operand).
func (g *irGen) orderDoubleTo(b *strings.Builder, s *orderSide, cur string) (string, string, error) {
	if s.ix == nil {
		d := g.floatValue(b, s.e)
		if d == "" {
			return "", "", g.floatOperandRefusal(s.e)
		}
		return d, cur, nil
	}
	done, fblk, iblk := g.newLabel("orddbl"), g.newLabel("ordf"), g.newLabel("ordi")
	isF := g.newTmp()
	fmt.Fprintf(b, "  %s = icmp eq i32 %s, %d\n", isF, s.tag, int32(TagFloat))
	g.markI1(isF)
	fmt.Fprintf(b, "  br i1 %s, label %%%s, label %%%s\n", isF, fblk, iblk)
	fmt.Fprintf(b, "%s:\n", fblk)
	dv := g.newTmp()
	fmt.Fprintf(b, "  %s = call double @rt_float_of(i32 %s)\n", dv, s.payload)
	fmt.Fprintf(b, "  br label %%%s\n", done)
	fmt.Fprintf(b, "%s:\n", iblk)
	// The arm is entered only when the tag already said "number", so anything that is not a float here
	// is an int or a bool, and the conversion is total. A fourth answer would need a fourth block, and
	// a block cannot be written under the one the caller is still filling in.
	di := g.newTmp()
	fmt.Fprintf(b, "  %s = sitofp i32 %s to double\n", di, s.payload)
	fmt.Fprintf(b, "  br label %%%s\n", done)
	fmt.Fprintf(b, "%s:\n", done)
	d := g.newTmp()
	fmt.Fprintf(b, "  %s = phi double [ %s, %%%s ], [ %s, %%%s ]\n", d, dv, fblk, di, iblk)
	return d, done, nil
}

// emitTaggedOrder lowers `<`, `<=`, `>`, `>=` when the object has to say which pair it was given.
//
// Three answers are possible and the module has to carry all of them. Two numbers: compare them as
// doubles, because `1 < 1.5` and `1.5 < 2` are both questions about numbers and an i32 compare of two
// payloads cannot hear the difference. Two texts: ask strcmp, the helper ADR 0248 put behind the
// static ordering, because an interned index records the order the text arrived in, not the order of
// the text. A number and a text: raise the TypeError CPython raises, naming the two types in source
// order. That last case is the one the compiled backend used to answer with whichever number happened
// to sit in the slot — `print(1 if xs[1] > "z" else 0)` over a list that can hold either printed a
// verdict for a program that crashes, which is Gap R.85's measurement — while the shapes that reached
// no door at all were Gap R.82's own half of the same question.
//
// Every value-producing arm ends at the same merge and the raise arm ends at the handler instead, so
// the merge only ever has predecessors that produce a value (ADR 0205's rule for a merge of raises).
func (g *irGen) emitTaggedOrder(b *strings.Builder, n *BinOp) (string, string, bool, error) {
	ls, rs := g.orderShapeOf(n.L), g.orderShapeOf(n.R)
	if !g.taggedOrderApplies(n) {
		return "", "", false, nil
	}
	pred, ok := map[string]string{"<": "icmp slt", "<=": "icmp sle", ">": "icmp sgt", ">=": "icmp sge"}[n.Op]
	if !ok {
		return "", "", false, nil
	}
	fcmp, ok := map[string]string{"<": "fcmp olt", "<=": "fcmp ole", ">": "fcmp ogt", ">=": "fcmp oge"}[n.Op]
	if !ok {
		return "", "", false, nil
	}
	g.heapUsed = true
	// Tags and payloads are read in the block the comparison starts in, so an out-of-range subscript
	// still traps there, exactly as it did before this door existed.
	if !g.orderTagOf(b, &ls) || !g.orderTagOf(b, &rs) {
		return "", "", false, nil
	}
	g.orderAskTags(b, &ls)
	g.orderAskTags(b, &rs)
	lPay, err := g.orderPayload(b, &ls)
	if err != nil {
		return "", "", false, err
	}
	rPay, err := g.orderPayload(b, &rs)
	if err != nil {
		return "", "", false, err
	}

	// The three verdicts an ordering can end in are emitted only when they can actually happen. An arm
	// no branch can reach would still be named in the merge's phi, and a phi listing a predecessor that
	// never branches to it is the module `llc` rejects — the exit class this line of work has to stay
	// out of (ADR 0166). A side the compiler already read answers each test with a yes or a no, never
	// with the placeholder `true` that would claim it could be either.
	type orderArm struct {
		kind  string // "text", "number" or "raise"
		label string
		test  string // "true", "false" or an i1 register; the last kept arm is entered unconditionally
	}
	var arms []orderArm
	if ls.canText && rs.canText {
		arms = append(arms, orderArm{kind: "text", label: g.newLabel("ordtxt"), test: g.orderBothText(b, &ls, &rs)})
	}
	if ls.canNum && rs.canNum {
		arms = append(arms, orderArm{kind: "number", label: g.newLabel("ordnum"), test: g.orderBothNumber(b, &ls, &rs)})
	}
	if ls.canNum && rs.canText || ls.canText && rs.canNum {
		arms = append(arms, orderArm{kind: "raise", label: g.newLabel("ordbad")})
	}
	// A test the compiler settled is not a test: a `false` retires its arm, a `true` ends the chain
	// there because nothing below it can run.
	kept := make([]orderArm, 0, len(arms))
	for _, a := range arms {
		if a.test == "false" {
			continue
		}
		kept = append(kept, a)
		if a.test == "true" || a.test == "" {
			break
		}
	}
	valueArms := 0
	for _, a := range kept {
		if a.kind != "raise" {
			valueArms++
		}
	}
	if valueArms == 0 {
		// Nothing left to decide: the pair is settled by the literal and no arm would produce a value.
		// That is the shape the static doors answer, so this one steps aside.
		return "", "", false, nil
	}

	// The chain. Each arm but the last is guarded by its test; the last kept arm is the else, and is
	// entered unconditionally — which is sound, because the gate left only the pairs that arm decides.
	tail := g.newLabel("ordtail")
	cur := "" // "" means still in the block the comparison started in
	for i, a := range kept {
		last := i == len(kept)-1
		if last || a.test == "true" || a.test == "" {
			g.orderBr(b, cur, a.label)
			if a.test == "true" || a.test == "" {
				kept = kept[:i+1]
			}
			cur = ""
			if last {
				break
			}
			continue
		}
		next := g.newLabel("ordchk")
		g.orderCondBr(b, cur, a.test, a.label, next)
		cur = next
	}

	lName, rName := "str", "str"
	if ls.canNum {
		lName = ls.numName
	}
	if rs.canNum {
		rName = rs.numName
	}
	vals := make([]string, len(kept))
	preds := make([]string, len(kept))
	for i, a := range kept {
		fmt.Fprintf(b, "%s:\n", a.label)
		switch a.kind {
		case "text":
			preds[i] = a.label
			ord := g.newTmp()
			fmt.Fprintf(b, "  %s = call i32 @rt_str_order(i32 %s, i32 %s)\n", ord, lPay, rPay)
			cmp := g.newTmp()
			fmt.Fprintf(b, "  %s = %s i32 %s, 0\n", cmp, pred, ord)
			g.markI1(cmp)
			v := g.newTmp()
			fmt.Fprintf(b, "  %s = zext i1 %s to i32\n", v, cmp)
			fmt.Fprintf(b, "  br label %%%s\n", tail)
			vals[i] = v
		case "number":
			ld, cur2, nerr := g.orderDoubleTo(b, &ls, a.label)
			if nerr != nil {
				return "", "", false, nerr
			}
			rd, cur2, nerr := g.orderDoubleTo(b, &rs, cur2)
			if nerr != nil {
				return "", "", false, nerr
			}
			preds[i] = cur2
			cmp := g.newTmp()
			fmt.Fprintf(b, "  %s = %s double %s, %s\n", cmp, fcmp, ld, rd)
			g.markI1(cmp)
			v := g.newTmp()
			fmt.Fprintf(b, "  %s = zext i1 %s to i32\n", v, cmp)
			fmt.Fprintf(b, "  br label %%%s\n", tail)
			vals[i] = v
			_ = cur2
		case "raise":
			if ls.fromTag || rs.fromTag {
				if rerr := g.orderRaiseUnderTag(b, &ls, &rs, n); rerr != nil {
					return "", "", false, rerr
				}
				break
			}
			say := func(lNm, rNm string) error {
				class, msg := unsupportedNumberOp(n.Op, lNm, rNm)
				if class == "" {
					return fmt.Errorf("codegen: %q has no CPython sentence for %q against %q (roadmap L11.1, Gap R.82)", n.Op, lNm, rNm)
				}
				g.raiseTo(b, exnCode(class), class, msg, n.Span())
				return nil
			}
			// CPython names the left operand's type first, so a side whose role is a tag question picks
			// which of the two sentences runs; a side whose kinds are all one family has a settled role.
			var which string
			roleIsLeft := false
			switch {
			case ls.canNum && ls.canText && ls.tag != "":
				which, roleIsLeft = ls.tagIsText, true
			case rs.canNum && rs.canText && rs.tag != "":
				which = rs.tagIsText
			}
			if which == "" {
				if nerr := say(lName, rName); nerr != nil {
					return "", "", false, nerr
				}
				break
			}
			lblK, rblk := g.newLabel("ordbl"), g.newLabel("ordbr")
			g.orderCondBr(b, "", which, lblK, rblk)
			fmt.Fprintf(b, "%s:\n", lblK)
			if roleIsLeft {
				if nerr := say("str", rName); nerr != nil {
					return "", "", false, nerr
				}
			} else {
				if nerr := say(lName, "str"); nerr != nil {
					return "", "", false, nerr
				}
			}
			fmt.Fprintf(b, "%s:\n", rblk)
			if roleIsLeft {
				if nerr := say(lName, "str"); nerr != nil {
					return "", "", false, nerr
				}
			} else {
				if nerr := say("str", rName); nerr != nil {
					return "", "", false, nerr
				}
			}
		}
	}

	fmt.Fprintf(b, "%s:\n", tail)
	phi := g.newTmp()
	parts := make([]string, 0, len(kept))
	for i, a := range kept {
		if a.kind == "raise" {
			continue
		}
		parts = append(parts, fmt.Sprintf("[ %s, %%%s ]", vals[i], preds[i]))
	}
	fmt.Fprintf(b, "  %s = phi i32 %s\n", phi, strings.Join(parts, ", "))
	cnd := g.newTmp()
	fmt.Fprintf(b, "  %s = icmp ne i32 %s, 0\n", cnd, phi)
	g.markI1(cnd)
	return phi, cnd, true, nil
}

// orderRaiseUnderTag is the raise arm when one side's kind is a question only the object can answer:
// a container the program built rather than spelled out, or a slot one level below such an object.
// CPython's sentence names both operand types, so this arm cannot be one raise — it is one test per
// kind the writers can leave beside a payload, each raising the sentence CPython writes for that kind,
// with the text tag as the unconditional last arm because the set of tags ADR 0187's writers store is
// closed. The other side's name is settled: the gate admits this shape only against an operand the
// compiler read itself, so the two names the sentence carries are always both sides' real kinds
// (roadmap L11.1, Gap R.93, Gap R.82's sentence rule).
func (g *irGen) orderRaiseUnderTag(b *strings.Builder, ls, rs *orderSide, n *BinOp) error {
	side, other, left := ls, "str", true
	if !ls.fromTag {
		side, other, left = rs, "str", false
		if ls.canNum && ls.numName != "" {
			other = ls.numName
		}
	} else if rs.canNum && rs.numName != "" {
		other = rs.numName
	}
	kinds := []struct {
		tg   int32
		name string
	}{
		{int32(TagInt), "int"},
		{int32(TagBool), "bool"},
		{int32(TagFloat), "float"},
		{int32(TagNone), "NoneType"},
		{int32(TagList), "list"},
		{int32(TagDict), "dict"},
		{int32(TagSet), "set"},
	}
	arms := make([]string, len(kinds))
	for i := range arms {
		arms[i] = g.newLabel("ordk")
	}
	for i, k := range kinds {
		c := g.newTmp()
		fmt.Fprintf(b, "  %s = icmp eq i32 %s, %d\n", c, side.tag, k.tg)
		g.markI1(c)
		chk := g.newLabel("ordkc")
		fmt.Fprintf(b, "  br i1 %s, label %%%s, label %%%s\n", c, arms[i], chk)
		fmt.Fprintf(b, "%s:\n", chk)
	}
	strArm := g.newLabel("ordkstr")
	fmt.Fprintf(b, "  br label %%%s\n", strArm)
	raise := func(lNm, rNm string) error {
		class, msg := unsupportedNumberOp(n.Op, lNm, rNm)
		if class == "" {
			return fmt.Errorf("codegen: %q has no CPython sentence for %q against %q (roadmap L11.1, Gap R.82)", n.Op, lNm, rNm)
		}
		g.raiseTo(b, exnCode(class), class, msg, n.Span())
		return nil
	}
	for i, k := range kinds {
		fmt.Fprintf(b, "%s:\n", arms[i])
		if left {
			if err := raise(k.name, other); err != nil {
				return err
			}
			continue
		}
		if err := raise(other, k.name); err != nil {
			return err
		}
	}
	fmt.Fprintf(b, "%s:\n", strArm)
	if left {
		return raise("str", other)
	}
	return raise(other, "str")
}

// orderBr and orderCondBr are the two ways out of an arm or a test block. The empty `from` means the
// caller is still emitting in the block it was handed, where a branch has already to be added; a named
// `from` is a block the chain created for itself, whose terminator has not been written yet.
func (g *irGen) orderBr(b *strings.Builder, from, to string) {
	if from != "" {
		fmt.Fprintf(b, "%s:\n", from)
	}
	fmt.Fprintf(b, "  br label %%%s\n", to)
}

func (g *irGen) orderCondBr(b *strings.Builder, from, cond, yes, no string) {
	if from != "" {
		fmt.Fprintf(b, "%s:\n", from)
	}
	fmt.Fprintf(b, "  br i1 %s, label %%%s, label %%%s\n", cond, yes, no)
}

// orderBothText asks whether both sides report text. A side the compiler already read answers yes or
// no; a slot answers with its tag.
func (g *irGen) orderBothText(b *strings.Builder, ls, rs *orderSide) string {
	l, r := ls.textTest(), rs.textTest()
	return g.orderAndTests(b, l, r)
}

// orderBothNumber asks the same question of the three number tags.
func (g *irGen) orderBothNumber(b *strings.Builder, ls, rs *orderSide) string {
	l, r := ls.numberTest(), rs.numberTest()
	return g.orderAndTests(b, l, r)
}

// orderAndTests is the two-input and over the three answers a side can give: a register, or a settled
// yes or no. Anything settled folds away, so the chain never pays for a test the compiler made.
func (g *irGen) orderAndTests(b *strings.Builder, l, r string) string {
	if l == "false" || r == "false" {
		return "false"
	}
	if l == "true" {
		return r
	}
	if r == "true" {
		return l
	}
	both := g.newTmp()
	fmt.Fprintf(b, "  %s = and i1 %s, %s\n", both, l, r)
	g.markI1(both)
	return both
}

// emitTextOrder lowers `<`, `<=`, `>`, `>=` between two texts by asking strcmp which text comes
// first, and returns the i32 0/1 the comparison is plus the i1 a condition can use directly.
//
// Before this, every one of these comparisons compared two indices into @str_tab: `print(1 if "b" >
// "a" else 0)` printed 0, `a > b` printed whatever the declaration order said, and a sort wrote the
// same sentence in its own helper (rt_sort's mode) while the operators never got it — so a program
// could sort a list correctly and then compare two of its elements wrongly (roadmap Gap R.84).
func (g *irGen) emitTextOrder(b *strings.Builder, n *BinOp) (string, string, error) {
	l, err := g.value(b, n.L)
	if err != nil {
		return "", "", err
	}
	r, err := g.value(b, n.R)
	if err != nil {
		return "", "", err
	}
	return g.emitTextOrderOperands(b, n.Op, l, r)
}

// emitTextOrderOperands is the lowering, given the two registers the operands already live in: a
// path that lowered them for its own purposes must not lower them twice, because an operand can
// carry a print or a call (the rule ADR 0189 applies to a statically-decided comparison).
func (g *irGen) emitTextOrderOperands(b *strings.Builder, op, l, r string) (string, string, error) {
	pred, ok := map[string]string{"<": "icmp slt", "<=": "icmp sle", ">": "icmp sgt", ">=": "icmp sge"}[op]
	if !ok {
		return "", "", fmt.Errorf("codegen: %q is not an ordering (internal: emitTextOrderOperands)", op)
	}
	g.heapUsed = true // @str_tab and strcmp live in the heap block
	ord := g.newTmp()
	fmt.Fprintf(b, "  %s = call i32 @rt_str_order(i32 %s, i32 %s)\n", ord, l, r)
	i1 := g.newTmp()
	fmt.Fprintf(b, "  %s = %s i32 %s, 0\n", i1, pred, ord)
	g.markI1(i1)
	res := g.newTmp()
	fmt.Fprintf(b, "  %s = zext i1 %s to i32\n", res, i1)
	return res, i1, nil
}

// floatOperandRefusal is what a print or format site says when the float lift handed back nothing:
// the operand is named, and the program leaves through the refusal exit class instead of reaching
// `llc` with an instruction that has an empty operand (roadmap Gap R.88, ADR 0166).
func (g *irGen) floatOperandRefusal(e Expr) error {
	why := g.floatUnlowerable
	g.floatUnlowerable = ""
	if why == "" {
		why = g.exprSummary(e)
	}
	return fmt.Errorf("codegen: %s has no number the compiled backend can lift: a slot whose kind only the object knows is answered for equality, printing, membership and length, and a numeric use of it is answered where the compiler can settle the result's kind — this one needs the run-time tag to decide it, which is the tagged value word still owed here (roadmap L11.1, Gap R.82, ADR 0166)", why)
}

// taggedNumberUseApplies is the gate the arithmetic and comparison paths ask: does this BinOp have a
// slot read on one side whose kind the object carries, with the other side a number the compiler can
// name? When it does, the BinOp goes down the float path, because that path can ask the tag — and the
// arm that would have read the payload as a plain number is the one that used to emit an empty operand
// (roadmap Gap R.88, ADR 0166's exit-class rule).
func (g *irGen) taggedNumberUseApplies(n *BinOp) bool {
	_, ok := g.taggedNumberOperands(n)
	if !ok {
		return false
	}
	for _, side := range []Expr{n.L, n.R} {
		if ix, isIdx := side.(*Index); isIdx {
			if _, fOk := g.slotNumberFamily(ix.Obj); fOk {
				return true
			}
			if n.Op == "/" && g.slotReadFromObject(ix) {
				return true
			}
		}
	}
	return false
}

// slotReadFromObject reports that a subscript is answered by the object rather than by the literal —
// the container was built by `append`/assignment, or the side is itself such a read — so the only thing
// the compiler knows about the slot is the tag array. It is the same gate the print, the equality, the
// length, the ordering and the subscript use (ADR 0251, ADR 0252), asked here of the arithmetic door
// (roadmap L11.1, Gap R.96).
func (g *irGen) slotReadFromObject(ix *Index) bool {
	switch ix.Obj.(type) {
	case *Name:
		if !g.runtimeContainerSide(ix.Obj) {
			return false
		}
		_, described := g.slotNumberFamily(ix.Obj)
		return !described
	case *Index:
		r := g.runtimeSlotReadOf(ix)
		return r
	}
	return false
}

// floatLoweringRefusal is what the compiled backend says when a float arm reaches an operand it cannot
// lift: the operand is named, the reason is named, and the program leaves through the refusal exit
// class rather than through llc rejecting the module (roadmap Gap R.88, ADR 0166).
func (g *irGen) floatLoweringRefusal(n *BinOp) error {
	why := g.floatUnlowerable
	g.floatUnlowerable = ""
	return fmt.Errorf("codegen: %s of %q has no number the compiled backend can lift: a slot whose kind only the object knows is answered for equality, printing, membership and length, and using it as a number needs the tagged value word still owed here (roadmap L11.1, Gap R.82, ADR 0166)", why, n.Op)
}

// doubleDomainRefusal is what an int-domain sink says when the numeric use underneath it belongs to
// the double domain: printing, a float comparison, an `if`/`while` head and a float assignment all ask
// for a double and can hold one; a call argument, an augmented assignment onto an int variable,
// str()'s argument and a container slot do not, and receiving the door's double there emits an i32
// instruction, a call or a store with a double operand in it — the module llc rejects, which ADR 0166
// counts as the compiler's bug for an ordinary program. The honest answer is to name the missing word:
// the kind of a slot the object reports travels as a (payload, tag) pair, and a value that leaves the
// expression it was computed in needs the tagged value word this row still owes (roadmap L11.1).
func (g *irGen) doubleDomainRefusal(n *BinOp) error {
	return fmt.Errorf("codegen: %s is answered with a double by the tagged numeric door, and this context stores an i32 word: printing, an assignment to a float variable, a comparison and a condition all take the double, and passing one to a call, a container slot or str() needs the (payload, tag) pair the tagged value word still owed here would carry — bind it to a variable that starts life a float (roadmap L11.1, Gap R.88, ADR 0166)", g.exprGist(n))
}

// slotNumberFamily is the promise the compiler can make about the numbers a slot may hold, for the
// one case that can be made without reading the object at run time: a container the literal still
// describes. "float" means every numeric element of it is a float, "int" that every one is an int or
// bool, "none" that it holds no number at all (so a numeric use raises whatever the kind dictates),
// and "mixed" that an int and a float can both appear — in which case the *result's* kind is a
// run-time question, which is the tagged value word this phase is still owed (roadmap L11.1).
//
// A container no literal describes — built up by `append`, handed in by a caller — answers "",false:
// the compiler would be guessing, and a guess here is an answer that depends on the program's data.
func (g *irGen) slotNumberFamily(obj Expr) (string, bool) {
	nm, isName := obj.(*Name)
	if !isName {
		return "", false
	}
	lit, ok := g.containerLits[nm.Value]
	if !ok {
		return "", false
	}
	elems := []Expr{}
	switch c := lit.(type) {
	case *ListLit:
		elems = c.Elems
	case *DictLit:
		elems = append(elems, c.Vals...)
	default:
		return "", false
	}
	if len(elems) == 0 {
		return "", false
	}
	seenFloat, seenInt, seenOther := false, false, false
	for _, el := range elems {
		tg, tok := g.elemKindTag(el)
		if !tok {
			return "", false
		}
		switch tg {
		case int32(TagFloat):
			seenFloat = true
		case int32(TagInt), int32(TagBool):
			seenInt = true
		default:
			seenOther = true
		}
	}
	_ = seenOther
	switch {
	case seenFloat && seenInt:
		return "mixed", true
	case seenFloat:
		return "float", true
	case seenInt:
		return "int", true
	default:
		return "none", true
	}
}

// numericUseKind names the family an operand belongs to for the purposes of the raise message CPython
// writes: "int", "float", or "" when the compiler cannot say — and an operand whose kind this cannot
// name disqualifies the shape, because the sentence has to be CPython's exactly.
func (g *irGen) numericUseKind(e Expr) string {
	switch n := e.(type) {
	case *IntLit, *BoolLit:
		return "int"
	case *FloatLit:
		return "float"
	case *StrLit, *FString, *NoneLit, *ListLit, *DictLit, *SetLit:
		// Not a number, whatever the tail of this function says about an expression it cannot name.
		// A text literal answering "int" here is how `xs[1] > "a"` reached the numeric door and raised
		// a TypeError CPython never raises — comparing "a" with "a" is an ordering, not a trap.
		return ""
	case *Name:
		if g.floatVars[n.Value] || g.floatTemps[n.Value] {
			return "float"
		}
		if g.internedVars[n.Value] || g.taggedVars[n.Value] {
			return ""
		}
		if g.isContainerExpr(n) {
			return ""
		}
		return "int"
	case *Index:
		fam, ok := g.slotNumberFamily(n.Obj)
		if !ok {
			return ""
		}
		switch fam {
		case "float":
			return "float"
		case "int":
			return "int"
		}
		return ""
	}
	if g.isFloat(e) {
		return "float"
	}
	switch e.(type) {
	case *BinOp, *UnOp, *Call:
		return ""
	}
	return "int"
}

// unsupportedNumberOp is CPython's sentence for an arithmetic use of a value that has no number in
// it, spelled the way the interpreter spells it — which is not one sentence: text against `+` gets
// the concatenation wording, everything else the operand-type one. Getting this wrong is worse than
// refusing, because the test table compares the string, and a program's `except TypeError` reads it.
func unsupportedNumberOp(op, kind, other string) (string, string) {
	switch op {
	case "<", "<=", ">", ">=":
		return "TypeError", fmt.Sprintf("'%s' not supported between instances of '%s' and '%s'", op, kind, other)
	case "neg":
		// `-xs[i]` is its own operator to CPython, with its own sentence. Spelling the negation as
		// `0 - xs[i]` would have raised the binary one, which is a different program's error.
		return "TypeError", fmt.Sprintf("bad operand type for unary -: '%s'", kind)
	default:
		if op == "+" && kind == "str" {
			return "TypeError", fmt.Sprintf("can only concatenate str (not %q) to str", other)
		}
		return "TypeError", fmt.Sprintf("unsupported operand type(s) for %s: '%s' and '%s'", op, kind, other)
	}
}

// taggedNumberOperands is the door's question: is this a numeric use of a slot whose kind the object
// carries, with the other side a number the compiler can name? The result's kind has to be settled
// here, in the compiler, or the answer would be printed in the wrong form — so the shapes allowed are
// the ones where it is: a slot that can only hold floats, an ordering (whose answer is a bool whatever
// arrives), and true division (which Python always answers with a float).
//
// The last of those is also the only arithmetic operator that can be answered for a slot **no literal
// describes**: `xs = []` / `xs.append(3)` / `print(xs[0] / 2)` is `1.5` on every engine, because `/` is
// a float whatever either side turns out to be, so the module can be written before the slot is asked.
// `xs[0] + 1` is still refused: an int slot answers `4` and a float slot `4.5`, and the difference is a
// fact about the program's data (roadmap L11.1, Gap R.96 — which measured this shape answering `0.0`).
func (g *irGen) taggedNumberOperands(n *BinOp) (string, bool) {
	switch n.Op {
	case "+", "-", "*", "/", "//", "%", "**", "<", "<=", ">", ">=":
	default:
		return "", false
	}
	reads, others := 0, []Expr{}
	for _, side := range []Expr{n.L, n.R} {
		if ix, isIdx := side.(*Index); isIdx {
			if _, fOk := g.slotNumberFamily(ix.Obj); fOk {
				reads++
				others = append(others, side)
				continue
			}
			if n.Op == "/" && g.slotReadFromObject(ix) {
				reads++
				others = append(others, side)
				continue
			}
		}
		others = append(others, side)
	}
	if reads == 0 {
		return "", false
	}
	// Every side that is not the slot read must be a number this compiler can name, or the raise
	// sentence and the conversion are both guesswork.
	for _, o := range others {
		if _, isIdx := o.(*Index); isIdx {
			continue // the other side is a slot read too; its family is checked below
		}
		if g.numericUseKind(o) == "" {
			return "", false
		}
	}
	// Which family does each read bring?
	fam := ""
	for _, o := range others {
		ix, isIdx := o.(*Index)
		if !isIdx {
			continue
		}
		if n.Op == "/" && g.slotReadFromObject(ix) {
			continue // the object reports this one; `/` settles the result's kind to float below
		}
		f, fOk := g.slotNumberFamily(ix.Obj)
		if !fOk || f == "mixed" {
			return "", false
		}
		if fam == "" || fam == "none" {
			fam = f
		} else if f != "none" && f != fam {
			return "", false // int on one side, float on the other: the result's kind is a run-time question
		}
	}
	relational := n.Op == "<" || n.Op == "<=" || n.Op == ">" || n.Op == ">="
	if n.Op == "/" {
		relational = true // true division is answered with a float whatever the operands are
	}
	if fam != "float" && !relational {
		return "", false
	}
	// A side the literal never described is only admissible where the object can be asked and the
	// sentence CPython raises is still knowable: `/` names the other operand's type, which the gate
	// above has already required the compiler to be able to name.
	for _, o := range others {
		if ix, isIdx := o.(*Index); isIdx && g.slotReadFromObject(ix) && n.Op != "/" {
			return "", false
		}
	}
	// Two slot reads: the door may only raise when a number is certain on the other side. With a
	// payload-and-tag pair on *both* sides, text against text and container against container are
	// orderings CPython answers (`["b","a",1][0] > ["b","a",1][1]` is True), so a container that can
	// hold anything but a number is declined rather than made to raise a trap the program never asked
	// for.
	if reads == 2 {
		for _, o := range others {
			if ix, isIdx := o.(*Index); isIdx {
				kinds, kOk := g.slotNonNumericKinds(ix.Obj)
				if !kOk || len(kinds) > 0 {
					return "", false
				}
			}
		}
	}
	// Repetition and formatting are real Python behaviours the compiled backend does not have
	// (Gap R.33, Gap R.31): a container that might hold text or a list cannot be ordered to raise,
	// because CPython would not raise — it would repeat or format. Decline those.
	if n.Op == "*" || n.Op == "%" {
		for _, o := range others {
			if ix, isIdx := o.(*Index); isIdx {
				if g.textSlotPossible(ix.Obj) {
					return "", false
				}
			}
		}
	}
	return fam, true
}

// textSlotPossible asks whether a container's slots can hold text or a container, either of which
// turns `*` into repetition and `%` into a formatting call rather than a TypeError.
func (g *irGen) textSlotPossible(obj Expr) bool {
	nm, isName := obj.(*Name)
	if !isName {
		return true // cannot say → assume the worst and decline the shape
	}
	if g.listElemStr[nm.Value] || g.setElemStr[nm.Value] || g.dictValStr[nm.Value] {
		return true
	}
	lit, ok := g.containerLits[nm.Value]
	if !ok {
		return true
	}
	elems := []Expr{}
	switch c := lit.(type) {
	case *ListLit:
		elems = c.Elems
	case *DictLit:
		elems = append(elems, c.Vals...)
	}
	for _, el := range elems {
		tg, tok := g.elemKindTag(el)
		if !tok || tg != int32(TagInt) && tg != int32(TagFloat) && tg != int32(TagBool) {
			return true
		}
	}
	return false
}

// slotNonNumericKinds lists the kinds a container's slots can hold that have no number in them, as the
// literal describes them. The list is what makes the raise arms complete: the door emits one branch per
// kind the object could actually report, and the last of them is the unconditional else, so every path
// out of the read either produces a double or raises the sentence CPython writes for that kind — never
// a phi with a predecessor that never stores anything (roadmap L11.1, Gap R.88).
func (g *irGen) slotNonNumericKinds(obj Expr) ([]struct {
	tg   int32
	name string
}, bool) {
	nm, isName := obj.(*Name)
	if !isName {
		return nil, false
	}
	lit, ok := g.containerLits[nm.Value]
	if !ok {
		return nil, false
	}
	var elems []Expr
	switch c := lit.(type) {
	case *ListLit:
		elems = c.Elems
	case *DictLit:
		elems = append(elems, c.Vals...)
	}
	seen := map[int32]bool{}
	var out []struct {
		tg   int32
		name string
	}
	for _, el := range elems {
		tg, tok := g.elemKindTag(el)
		if !tok {
			return nil, false
		}
		var name string
		switch tg {
		case int32(TagInt), int32(TagBool), int32(TagFloat):
			continue
		case int32(TagStr):
			name = "str"
		case int32(TagNone):
			name = "NoneType"
		case int32(TagList):
			name = "list"
		case int32(TagDict):
			name = "dict"
		case int32(TagSet):
			name = "set"
		case int32(TagTuple):
			name = "tuple"
		default:
			return nil, false // a kind whose sentence this door would have to invent: decline
		}
		if !seen[tg] {
			seen[tg] = true
			out = append(out, struct {
				tg   int32
				name string
			}{tg, name})
		}
	}
	return out, true
}

// taggedFloatOperand lowers one operand of a numeric use to the double the arithmetic wants. The
// slot's payload and tag come out of the object (the same read print and == already make), and the tag
// decides: a float slot unboxes out of its @float_box object, an int or bool slot converts, and a slot
// holding anything else raises the TypeError CPython raises for this operator and that kind — the door
// lenOfTaggedSlot already walks to say `object of type 'int' has no len()`.
func (g *irGen) taggedFloatOperand(b *strings.Builder, e Expr, n *BinOp, fam string) (string, bool, error) {
	ix, isIdx := e.(*Index)
	if !isIdx {
		v := g.floatValue(b, e)
		return v, v != "", nil
	}
	other := "int"
	for _, side := range []Expr{n.L, n.R} {
		if side != e {
			if k := g.numericUseKind(side); k != "" {
				other = k
			} else if _, isIx := side.(*Index); !isIx {
				return "", false, nil // an operand whose kind would make the message a guess
			}
		}
	}
	// CPython names the two operand types in source order, so the sentence has to know which side the
	// slot read was on: `1 > xs[i]` with a text slot is "'int' and 'str'", not the other way round.
	slotOnLeft := n.L == e
	sentence := func(kindName string) (string, string) {
		if slotOnLeft {
			return unsupportedNumberOp(n.Op, kindName, other)
		}
		return unsupportedNumberOp(n.Op, other, kindName)
	}
	if g.slotReadFromObject(ix) {
		// No literal to read the kinds off: the object reports them, and the arms and their sentences
		// are emitted per tag (roadmap L11.1, Gap R.96).
		rv, rok, rerr := g.taggedDoubleFromObject(b, ix, n, e, other, sentence)
		return rv, rok, rerr
	}
	return g.taggedDoubleFromSlot(b, ix, n.Span(), sentence)
}

// taggedDoubleFromObject lifts a slot **no literal describes** to the double an arithmetic arm needs:
// the (payload, tag) pair comes out of the object, the float tag unboxes out of its @float_box, an int
// or bool tag converts, and every other tag raises the sentence CPython writes for this operator and
// that kind — the same closed tag set the ordering's raise chain walks (ADR 0252), because the writers
// are ADR 0187's and nothing outside that list is ever stored beside a payload.
//
// A true division also has to trap a zero divisor, and CPython's wording for that trap depends on
// whether either operand is a float: `3 / 0` is `division by zero` and `1.5 / 0` is
// `float division by zero`. With one operand's kind a run-time question the wording is one too, so the
// guard is emitted *inside* each arm, where the arm knows what it lifted — a guard placed after the
// merge could only guess, and guessing here prints a sentence the program's `except` would not match.
func (g *irGen) taggedDoubleFromObject(b *strings.Builder, ix *Index, n *BinOp, side Expr, otherName string, sentence func(string) (string, string)) (string, bool, error) {
	payload, tag, ok := g.taggedSlotPair(b, ix)
	if !ok || payload == "" || tag == "" {
		return "", false, nil
	}
	g.heapUsed = true
	g.floatFmtUsed = true
	sp := ix.Span()
	if n != nil {
		sp = n.Span()
	}

	// A true division has to trap a zero divisor, and the sentence it raises is a fact about the two
	// operand *kinds*: `3 / 0` is `division by zero` and `1.5 / 0` is `float division by zero`. One of
	// those kinds lives only in the object, so the guard belongs inside the arm that knows it — a guard
	// after the merge could only guess, and a guessed sentence is one the program's `except` would not
	// match. What is guarded is whichever operand is the divisor: the lifted slot when the read sits on
	// the right of `/`, the other operand when the read is the dividend. An operand the lift cannot reach
	// for free (a call) is refused rather than divided by unguarded, because `fdiv` does not trap.
	guardDiv, divisorIsSlot := n != nil && n.Op == "/", false
	var divReg string
	if guardDiv {
		divisorIsSlot = n.R == side
		if !divisorIsSlot {
			// the operand on the other side of `/` from the slot read — the divisor when the read is
			// the dividend, the dividend when the read is the divisor (which is guarded below as the
			// lifted slot instead).
			other := n.R
			if n.R == side {
				other = n.L
			}
			switch o := other.(type) {
			case *IntLit:
				t := g.newTmp()
				fmt.Fprintf(b, "  %s = sitofp i32 %d to double\n", t, o.Value)
				divReg = t
			case *BoolLit:
				v := 0
				if o.Value {
					v = 1
				}
				t := g.newTmp()
				fmt.Fprintf(b, "  %s = sitofp i32 %d to double\n", t, v)
				divReg = t
			case *FloatLit, *Name:
				// side-effect-free: lifting it here and letting the caller lift it again costs an
				// instruction and nothing else. `floatValue` knows the double variable from the int one.
				divReg = g.floatValue(b, other)
			}
			if divReg == "" {
				return "", false, nil
			}
		}
	}
	// wording names the ZeroDivisionError sentence CPython writes for two operand kinds — a float
	// anywhere in the pair picks the `float division by zero` phrasing, `int` and `bool` the plain one.
	wording := func(armKind string) string {
		if armKind == "float" || otherName == "float" || otherName == "double" {
			return "float division by zero"
		}
		return "division by zero"
	}

	done := g.newLabel("objnumdone")
	floatBlk, chkBlk := g.newLabel("objnumf"), g.newLabel("objnumc")
	isFloat := g.newTmp()
	fmt.Fprintf(b, "  %s = icmp eq i32 %s, %d\n", isFloat, tag, int32(TagFloat))
	g.markI1(isFloat)
	fmt.Fprintf(b, "  br i1 %s, label %%%s, label %%%s\n", isFloat, floatBlk, chkBlk)

	// ---- the float arm: the payload is a handle on an @float_box, so it unboxes.
	fmt.Fprintf(b, "%s:\n", floatBlk)
	dv := g.newTmp()
	fmt.Fprintf(b, "  %s = call double @rt_float_of(i32 %s)\n", dv, payload)
	floatFrom := floatBlk
	if guardDiv {
		if divisorIsSlot {
			floatFrom = g.guardNonZeroFloat(b, dv, sp, wording("float"))
		} else {
			floatFrom = g.guardNonZeroFloat(b, divReg, sp, wording("float"))
		}
	}
	fmt.Fprintf(b, "  br label %%%s\n", done)

	// ---- the int/bool arm: the payload *is* the number, so it converts — the same widening L11.2
	// will owe to bools, which gusty already answers as ints wherever a bool reaches a number.
	fmt.Fprintf(b, "%s:\n", chkBlk)
	isInt := g.newTmp()
	fmt.Fprintf(b, "  %s = icmp eq i32 %s, %d\n", isInt, tag, int32(TagInt))
	g.markI1(isInt)
	isBool := g.newTmp()
	fmt.Fprintf(b, "  %s = icmp eq i32 %s, %d\n", isBool, tag, int32(TagBool))
	g.markI1(isBool)
	isNum := g.newTmp()
	fmt.Fprintf(b, "  %s = or i1 %s, %s\n", isNum, isInt, isBool)
	g.markI1(isNum)
	intArm := g.newLabel("objnumint")
	badBlk := g.newLabel("objnumbad")
	fmt.Fprintf(b, "  br i1 %s, label %%%s, label %%%s\n", isNum, intArm, badBlk)
	fmt.Fprintf(b, "%s:\n", intArm)
	di := g.newTmp()
	fmt.Fprintf(b, "  %s = sitofp i32 %s to double\n", di, payload)
	intFrom := intArm
	if guardDiv {
		if divisorIsSlot {
			intFrom = g.guardNonZeroFloat(b, di, sp, wording("int"))
		} else {
			intFrom = g.guardNonZeroFloat(b, divReg, sp, wording("int"))
		}
	}
	fmt.Fprintf(b, "  br label %%%s\n", done)

	// ---- the kinds with no number in them: one raise each, the last one as the else, so the merge's
	// only predecessors stay the two arms that produce a double (ADR 0205, ADR 0166) and no reachables
	// statement has to be written for a block nobody can enter (ADR 0214). The set is closed by the
	// writers ADR 0187 allows: int, bool, float, NoneType, list, dict, set and str, and the two numeric
	// ones are the arms above.
	fmt.Fprintf(b, "%s:\n", badBlk)
	kinds := []struct {
		tg   int32
		name string
	}{
		{int32(TagStr), "str"},
		{int32(TagNone), "NoneType"},
		{int32(TagList), "list"},
		{int32(TagDict), "dict"},
		{int32(TagSet), "set"},
	}
	for i, k := range kinds {
		if i == len(kinds)-1 {
			class, msg := sentence(k.name)
			g.raiseTo(b, exnCode(class), class, msg, sp)
			break
		}
		c := g.newTmp()
		fmt.Fprintf(b, "  %s = icmp eq i32 %s, %d\n", c, tag, k.tg)
		g.markI1(c)
		class, msg := sentence(k.name)
		g.branchRaise(b, c, class, msg, sp, "objnum")
	}

	res := g.newTmp()
	fmt.Fprintf(b, "%s:\n", done)
	fmt.Fprintf(b, "  %s = phi double [ %s, %%%s ], [ %s, %%%s ]\n", res, dv, floatFrom, di, intFrom)
	return res, true, nil
}

// taggedFloatNegate lowers `-xs[i]` where the slot's kind is only in the object. Negation has its own
// sentence in CPython — `bad operand type for unary -: 'str'`, not the binary one — which is why it is
// a separate entry point rather than a BinOp pretendee: a door that spelled `-xs[i]` as `0 - xs[i]`
// would raise `unsupported operand type(s) for -: 'int' and 'str'` for a program that never wrote a
// binary minus (roadmap L11.1, Gap R.88).
func (g *irGen) taggedFloatNegate(b *strings.Builder, u *UnOp) (string, bool, error) {
	if u.Op != "-" {
		return "", false, nil
	}
	if !g.taggedNegationApplies(u) {
		return "", false, nil
	}
	ix, isIdx := u.X.(*Index)
	if !isIdx {
		return "", false, nil
	}
	sentence := func(kindName string) (string, string) {
		return unsupportedNumberOp("neg", kindName, "")
	}
	return g.taggedDoubleFromSlot(b, ix, u.Span(), sentence)
}

// taggedNegationApplies is the gate the unary path asks: a negation of a slot read whose container the
// literal still describes, and whose numbers are all floats. An int-only container is not in this: a
// negated int is an int, and answering it from the float arms would print -1.0 where CPython prints -1
// — the same "the result's kind has to be settled in the compiler" rule that declines a mixed family
// (roadmap L11.1, Gap R.82).
func (g *irGen) taggedNegationApplies(u *UnOp) bool {
	ix, isIdx := u.X.(*Index)
	if !isIdx {
		return false
	}
	fam, ok := g.slotNumberFamily(ix.Obj)
	return ok && fam == "float"
}

// taggedDoubleFromSlot is the shared half of the two doors above: read the (payload, tag) pair the
// writer left beside the slot, and dispatch on the tag — unbox a float, convert an int or bool, or
// raise the sentence `sentence` writes for the kind the slot reports. One branch per kind the object
// could actually report, the last as the unconditional else, so every path out of the read either
// produces a double or raises: never a phi with a predecessor that stores nothing (roadmap L11.1).
func (g *irGen) taggedDoubleFromSlot(b *strings.Builder, ix *Index, span Span, sentence func(string) (string, string)) (string, bool, error) {
	others, kindsOk := g.slotNonNumericKinds(ix.Obj)
	if !kindsOk {
		return "", false, nil
	}
	payload, tag, ok := g.runtimeSlotPair(b, ix)
	if !ok || payload == "" || tag == "" {
		return "", false, nil
	}
	g.heapUsed = true
	g.floatFmtUsed = true
	done := g.newLabel("numdone")
	intBlk, floatBlk, chkBlk := g.newLabel("numint"), g.newLabel("numflt"), g.newLabel("numchk")
	isFloat := g.newTmp()
	fmt.Fprintf(b, "  %s = icmp eq i32 %s, %d\n", isFloat, tag, int32(TagFloat))
	g.markI1(isFloat)
	fmt.Fprintf(b, "  br i1 %s, label %%%s, label %%%s\n", isFloat, floatBlk, chkBlk)
	fmt.Fprintf(b, "%s:\n", floatBlk)
	dv := g.newTmp()
	fmt.Fprintf(b, "  %s = call double @rt_float_of(i32 %s)\n", dv, payload)
	fmt.Fprintf(b, "  br label %%%s\n", done)
	fmt.Fprintf(b, "%s:\n", chkBlk)
	isInt := g.newTmp()
	fmt.Fprintf(b, "  %s = icmp eq i32 %s, %d\n", isInt, tag, int32(TagInt))
	g.markI1(isInt)
	isBool := g.newTmp()
	fmt.Fprintf(b, "  %s = icmp eq i32 %s, %d\n", isBool, tag, int32(TagBool))
	g.markI1(isBool)
	isNum := g.newTmp()
	fmt.Fprintf(b, "  %s = or i1 %s, %s\n", isNum, isInt, isBool)
	g.markI1(isNum)
	if len(others) == 0 {
		// Nothing but numbers can come out of this slot, so the numeric test is the whole story and a
		// tag that says otherwise is the object disagreeing with the literal. Fall through to the int
		// arm rather than inventing a sentence for a case the literal rules out.
		fmt.Fprintf(b, "  br i1 %s, label %%%s, label %%%s\n", isNum, intBlk, intBlk)
	} else {
		badBlk := g.newLabel("numbad")
		fmt.Fprintf(b, "  br i1 %s, label %%%s, label %%%s\n", isNum, intBlk, badBlk)
		// The non-numeric kinds, each with CPython's own sentence, the last as the else so every
		// predecessor of the merge stores something.
		fmt.Fprintf(b, "%s:\n", badBlk)
		for i, k := range others {
			if i == len(others)-1 {
				class, msg := sentence(k.name)
				g.raiseTo(b, exnCode(class), class, msg, span)
				break
			}
			chk := g.newTmp()
			fmt.Fprintf(b, "  %s = icmp eq i32 %s, %d\n", chk, tag, k.tg)
			g.markI1(chk)
			class, msg := sentence(k.name)
			g.branchRaise(b, chk, class, msg, span, "num")
		}
	}
	fmt.Fprintf(b, "%s:\n", intBlk)
	di := g.newTmp()
	fmt.Fprintf(b, "  %s = sitofp i32 %s to double\n", di, payload)
	fmt.Fprintf(b, "  br label %%%s\n", done)
	res := g.newTmp()
	fmt.Fprintf(b, "%s:\n", done)
	fmt.Fprintf(b, "  %s = phi double [ %s, %%%s ], [ %s, %%%s ]\n", res, dv, floatBlk, di, intBlk)
	return res, true, nil
}

// isProvenScalarExpr reports what the compiler can show is *not* a container: a number, a string,
// None. Container-vs-scalar is False the way Python says it is — but only when the other side is
// proven, because an i32 with no static kind could be a handle, and two handles that differ are
// two different containers, not an unequal container and number.
func (g *irGen) isProvenScalarExpr(e Expr) bool {
	switch e.(type) {
	case *IntLit, *BoolLit, *NoneLit:
		return true
	}
	if _, ok := g.stringVal(e); ok {
		return true
	}
	if nm, ok := e.(*Name); ok && (g.internedVars[nm.Value] || g.floatVars[nm.Value]) {
		return true
	}
	return g.isFloat(e)
}

// containerEquality lowers == and != when at least one side is a container (ADR 0189).
func (g *irGen) containerEquality(b *strings.Builder, n *BinOp) (string, error) {
	lc, rc := g.isContainerExpr(n.L), g.isContainerExpr(n.R)
	// Each side lowers by what it is: a container becomes a handle, anything else keeps the
	// ordinary value path (and its side effects — deciding an answer statically must not
	// delete an operand that prints).
	lhs, err := g.compareOperand(b, n.L, lc)
	if err != nil {
		return "", err
	}
	rhs, err := g.compareOperand(b, n.R, rc)
	if err != nil {
		return "", err
	}
	if !lc || !rc {
		// Both operands are still built above: deciding the answer statically must not
		// delete a side effect (the same rule as the None comparison in value()).
		// Whichever side is not the container is the one we must be able to prove is a scalar.
		other := n.R
		if rc && !lc {
			other = n.L
		}
		if !g.isProvenScalarExpr(other) {
			return "", fmt.Errorf("codegen: comparing a container with %s needs a tagged value: the compiler cannot see whether that side is a container, and comparing the two i32s would compare a heap slot with a number (roadmap L11.1/L11.2, ADR 0189)", exprSnippet(other))
		}
		if n.Op == "!=" {
			return "1", nil
		}
		return "0", nil
	}
	raw := g.newTmp()
	fmt.Fprintf(b, "  %s = call i32 @rt_container_eq(i32 %s, i32 %s)\n", raw, lhs, rhs)
	cmp := g.newTmp()
	if n.Op == "==" {
		fmt.Fprintf(b, "  %s = icmp ne i32 %s, 0\n", cmp, raw)
	} else {
		fmt.Fprintf(b, "  %s = icmp eq i32 %s, 0\n", cmp, raw)
	}
	g.markI1(cmp)
	out := g.newTmp()
	fmt.Fprintf(b, "  %s = zext i1 %s to i32\n", out, cmp)
	return out, nil
}

// orderKindSlot names the kind map a container's element slots report through: a dict read reports
// its value, everything else the plain list map.
func orderKindSlot(name string, isDict bool) string {
	if isDict {
		return "dict value"
	}
	return "list"
}
