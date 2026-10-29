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
	// attrCall, when set, is told about `obj.method(args)` as well as `f(args)`. Only the
	// analyses that need a method's *arguments* set it (roadmap Gap R.111: a verdict handed
	// to `z.m(True)` crosses the same boundary as one handed to `m(True)`, and the
	// parameter-kind inference cannot see a call it is never told about). It is nil for every
	// existing user, which is why adding it changes nothing but this file's readers.
	attrCall func(scope, method string, args []Expr)
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
		if at, ok := n.Fn.(*Attr); ok && w.attrCall != nil && at.Name != nil {
			w.attrCall(w.scope, at.Name.Value, n.Args)
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
		if dl, ok := e.(*DictLit); ok && g.dictWantsContainerKey(dl) {
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
	if g.textDoorIsText(e) {
		// A formatting and a repeat each ARE a text: the door answers the @str_tab index a slot holds, which is
		// the same word an interpolation answers, so `["%d" % 7]`, `["ab" * 2]`, `{"k": "%s" % 1}` and
		// `d["k"] = "hi" * 2` store the text and not the position underneath it (roadmap Gap R.31, ADR 0360;
		// Gap R.33, ADR 0365; ADR 0359's value half).
		g.heapUsed = true
		v, ferr := g.value(b, e)
		return v, true, ferr
	}
	if _, isFs := e.(*FString); isFs {
		// An interpolation IS a text: the value road builds it through the runtime string table and
		// answers the index a slot holds, so `[f"a{n}"]`, `{f"k{n}": 1}` and `d[f"v{n}"] = 1` store the
		// text rather than the word underneath it. This is the same word `xs.append(t)` stores once the
		// program has named the interpolation, which is the only reason naming a value cannot change
		// what it is (roadmap L11.1, ADR 0359; Gap I.2's runtime string table).
		g.heapUsed = true
		v, ferr := g.value(b, e)
		return v, true, ferr
	}
	if g.printsAsInternedStr(e) {
		// A string method on a text receiver (`x.upper()`), a method registered as text-returning, and
		// `min`/`max` of texts all answer an @str_tab index already. Re-interning the index would ask
		// the table for the meaning of a number; treating it as a plain integer instead labels the slot
		// with the wrong tag and the printer renders the index. One predicate — the same one the print
		// road uses — decides both questions (roadmap Gap R.353, ADR 0229, ADR 0232).
		g.heapUsed = true
		v, serr := g.value(b, e)
		return v, true, serr
	}
	if nm, ok := e.(*Name); ok && g.loopElemTag != nil {
		if t, has := g.loopElemTag[nm.Value]; has && slotTagSelfDescribing(t) {
			g.heapUsed = true
			switch t {
			case int32(TagStr):
				v, err := g.value(b, e) // already the @str_tab index the iteration stored
				return v, true, err
			case int32(TagNone):
				return "0", false, nil
			default:
				// A float box, a container handle, an instance, or a verdict already lives in the loop
				// variable's word; the tag that travels with it says which printer reads that word.
				v, err := g.value(b, e)
				return v, false, err
			}
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
		g.emitRtFloatNew(b, bh, inner, Span{})
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
	g.emitRtAllocKind(b, h, HeapList, Span{})
	for i, el := range ln.Elems {
		// A pair-bound element writes its own two words; the tag is a register, not a constant.
		if pv, pt, isPair := g.pairElemPair(b, el); isPair {
			fmt.Fprintf(b, "  call void @rt_tag_elem(i32 %s, i32 %d, i32 %s)\n", h, i, pt)
			fmt.Fprintf(b, "  call void @rt_set_elem(i32 %s, i32 %d, i32 %s)\n", h, i, pv)
			continue
		}
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
	fmt.Fprintf(b, "  call void @rt_mark_estr(i32 %s, i32 %d)\n", h, tupleEstrBits(ln, 8))
	g.noteTuple(ln)
	return h, nil
}

func (g *irGen) heapListFrom(b *strings.Builder, ln *ListLit, name string) (string, error) {
	g.heapUsed = true
	h := g.newTmp()
	g.emitRtAllocKind(b, h, HeapList, Span{})
	bits := 0
	// The census the object is labelled with is taken from the words this loop actually writes, not
	// from a pre-scan that asks a different question. The pre-scan below (`literalMixedKinds`) reads
	// each element's SPELLING through `stringConstOf`, and an interpolation has no spelling to read —
	// `f"a{n}"` is a text to the builder (`heapElemKind` interns it) and a non-text to the pre-scan, so
	// [f"a{n}", 1] was labelled bit 1 ("every member is interned text"), printed the integer through
	// @str_tab and answered `['a3', '3']` where the record and the reference both answer `['a3', 1]`
	// (roadmap L11.1, ADR 0359; ADR 0232's rule that the slots are the truth).
	sawStr, sawOther := false, false
	for i, el := range ln.Elems {
		// A pair-bound element (`[n]` with `n = xs[0]`) is the element the static layout has no
		// representation for and the ordinary path asks for one word: both words come from the
		// name's own allocas, and the tag the objects wrote goes to `rt_tag_elem` as the register
		// it is (roadmap L11.1, Gap R.146, ADR 0187).
		if pv, pt, isPair := g.pairElemPair(b, el); isPair {
			fmt.Fprintf(b, "  call void @rt_set_elem(i32 %s, i32 %d, i32 %s)\n", h, i, pv)
			fmt.Fprintf(b, "  call void @rt_tag_elem(i32 %s, i32 %d, i32 %s)\n", h, i, pt)
			sawOther = true
			continue
		}
		v, interned, err := g.heapElemKind(b, el)
		if err != nil {
			return "", err
		}
		if interned {
			bits |= 1
			sawStr = true
			if name != "" {
				g.listElemStr[name] = true
			}
		} else {
			sawOther = true
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
	if g.literalNeedsTags(ln) || literalMixedKinds(ln) || g.literalHasPairElement(ln) || (sawStr && sawOther) {
		// A slot whose tag came from a register says so on the object too, or the printer reads
		// the list's single kind and answers the payload of a text slot as a number.
		bits |= 8
		if name != "" && sawStr && sawOther {
			// The variable stops claiming one element kind for the same reason the object does: the name
			// that held [f"a{n}", 1] has no single kind for `for x in xs` to read back (ADR 0232).
			g.mixedLists[name] = true
			delete(g.listElemStr, name)
			delete(g.listElemInt, name)
		}
	}
	// The immutable spelling says so on the object, and says it by making its slots the truth: a tuple
	// is built tagged whatever its elements are, because the printer that renders `(1, 'a', None)` asks
	// each slot and no element-kind census can describe that list (roadmap L11.3, ADR 0361).
	bits = tupleEstrBits(ln, bits)
	if bits != 0 {
		fmt.Fprintf(b, "  call void @rt_mark_estr(i32 %s, i32 %d)\n", h, bits)
	}
	g.noteTuple(ln)
	return h, nil
}

// tupleEstrBits adds the flags the immutable spelling owes the object: the tuple bit every door that
// prints, dispatches or mutates asks, and the self-describing bit that makes the per-slot tags the
// truth the tuple printer reads. One function, so no builder can forget the pair (roadmap L11.3, ADR 0361).
func tupleEstrBits(ln *ListLit, bits int) int {
	if ln.IsTuple() {
		bits |= estrBitTuple | 8
	}
	return bits
}

// heapDictFrom builds a heap dict from a literal, interning any string keys or values and
// recording on the object which positions hold interned strings (rt_mark_estr), so
// {"a": 1} and {1: "v"} build and print like the record renders them (Gap J.6).
func (g *irGen) heapDictFrom(b *strings.Builder, dl *DictLit, name string) (string, error) {
	if g.dictWantsContainerKey(dl) {
		// Built anyway, the key would be the inner object's handle printed as a number: {([1, 2]): 3}
		// answered {1: 3}, which is a wrong answer about the key rather than a missing one
		// (roadmap L11.1).
		for _, k := range dl.Keys {
			if isContainerLiteral(k) || g.dictKeyIsContainer(k) {
				return "", nestedContainerKeyErr(k)
			}
		}
	}
	g.heapUsed = true
	h := g.newTmp()
	g.emitRtAllocKind(b, h, HeapDict, Span{})
	bits := 0
	// A dict whose keys or values mix kinds has no kind to record on the object or on the
	// variable: each slot carries its own tag, and bit 8 tells the printer and the lookup to
	// read them (roadmap L11.1 (1b), ADR 0232). A key or a value the pair road bound is a second
	// reason for the same record: its tag is not a constant the compiler can read off a spelling,
	// so the entry has to go through the tagged put with the register the objects wrote (Gap R.146).
	mixed := g.taggableMixedDict(dl) || g.literalTakesPairSlots(dl) || g.dictLiteralHandsBackATag(dl)
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
		if mixed {
			// Each of the two positions answers for itself: a pair-bound one brings both of its words,
			// an ordinary one brings its payload and the tag `elemKindTag` proves. Put and tag stay one
			// call, so an entry never lands with the tag the previous tenant of that slot left behind.
			kk, kt, _, err := g.taggedSlotWords(b, dl.Keys[i])
			if err != nil {
				return "", err
			}
			// The key's own question, whatever road produced its tag (**Gap R.81**, **Gap R.502**).
			g.guardHashableExprTag(b, kt, dl.Keys[i], dl.Src)
			vv, vt, _, err := g.taggedSlotWords(b, dl.Vals[i])
			if err != nil {
				return "", err
			}
			fmt.Fprintf(b, "  call void @rt_dict_put_tagged(i32 %s, i32 %s, i32 %s, i32 %s, i32 %s)\n", h, kk, vv, kt, vt)
			continue
		}
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
	g.emitRtAllocKind(b, h, HeapSet, Span{})
	bits := 0
	idx := 0
	// A member the pair road bound carries its own tag register, and a set dedups by the pair, so a
	// member stored without its tag would collide with an ordinary payload that means something else
	// (roadmap L11.1, Gap R.146; ADR 0232's tagged members, ADR 0187's payload-with-tag).
	mixed := g.taggableMixedSet(sl) || g.literalTakesPairSlots(sl) || g.setLiteralHandsBackATag(sl)
	if mixed {
		bits |= 8
		if name != "" {
			g.mixedSets[name] = true
			g.setElemStr[name] = false
			g.setElemInt[name] = false
		}
	}
	for _, el := range sl.Elems {
		if mixed {
			// Adding and tagging are one call: a member added without its tag dedups against the
			// payload alone and prints through whatever the slot held last time. A pair member hands
			// over the tag it already owns instead of that constant.
			v, t, _, err := g.taggedSlotWords(b, el)
			if err != nil {
				return "", err
			}
			// Whether the tag came from a pair register or from the element's own spelling, it answers the
			// same question before the store: can this member be a key at all? A constant tag makes the check
			// a constant the verifier folds; a register makes it a run-time one, which is where a handed-back
			// container dies, beside the store that would have held it (**Gap R.502**, **Gap R.81**).
			g.guardHashableExprTag(b, t, el, sl.Src)
			fmt.Fprintf(b, "  call void @rt_set_add_tagged(i32 %s, i32 %s, i32 %s)\n", h, v, t)
			idx++
			continue
		}
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

// emptyContainerCall is `emptyContainerLiteral` with the one thing a purely syntactic door cannot know:
// whether the PROGRAM defined `set`, `list` or `dict` itself. A gate that only recognises the shape may use
// the bare door; a road that is about to LABEL the value with a kind — a slot's tag, a dict key's
// hashability, a nested element's taggability — asks this one, because a user's `def set(): return 1` is an
// integer wearing the spelling of a set and the tag would then be a fabricated fact about it (roadmap
// Gap R.360, ADR 0232's rule that a tag is never invented).
func (g *irGen) emptyContainerCall(e Expr) (Expr, bool) {
	lit, ok := emptyContainerLiteral(e)
	if !ok || g == nil {
		return lit, ok
	}
	if g.builtinShadowed(calleeName(e.(*Call))) {
		return nil, false
	}
	return lit, true
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
	case *Comp:
		// A comprehension is the container its brace spells, and the slot stores the handle of the object
		// it builds — materialised out of the compile-time global by `containerOperand`'s `*Comp` arm
		// (**ADR 0188**, **ADR 0192**), which is the road this gate lets the builder take (roadmap
		// **Gap R.488**, **Gap R.487**).
		return g.compIsContainerSlotExpr(n)
	case *Call:
		// The answer of a call the scan proves a container IS the container: the word the callee's `ret`
		// carries is the handle its builders made, and `containerOperand`'s `*Call` arm (**ADR 0333**) is the
		// road this gate lets the builder take. The gate had no such arm, so `heapElemKind` fell past the
		// container road to the ordinary value road, which for a literal hands back the name of a
		// compile-time GLOBAL — and the global emitter is the door that refused, in a sentence about an
		// element kind the program had already proven (roadmap **Gap R.530**, **ADR 0457**).
		return g.containerAnswerIsContainerSlotExpr(n)
	case *Name:
		return g.listVars[n.Value] || g.mixedLists[n.Value] || g.runtimeDicts[n.Value] || g.runtimeSets[n.Value]
	}
	return false
}

// nestedContainerErr is the door that declines a container whose shape no rule describes, and it now
// says WHICH rule it is missing. Until **ADR 0437** one sentence recited both rulings whatever the
// position asked, and closed by offering to "run it interpreted" — a way to run that **ADR 0302**
// retired, on a flag (`--interp`) whose answer is a usage error, so the half of the sentence that was
// true (this shape is not built) arrived with an instruction nobody can follow (roadmap
// **Gap R.288**). There are two rulings, and each road now names its own:
//
//   - an element that nests deeper than any tag the compiler writes: the slot would hold the inner
//     object's handle, which prints by handle and compares by address rather than by content; and
//   - a dict keyed by a container: the entry is filed by the handle the allocator handed out, so
//     nothing but the program that stored it could ever find it again — the hashing rule that would
//     ask two containers with the same elements whether they are the same key is what is missing.
//
// Both keep the family's stem (`a compiled container cannot hold %s`), because a reader who has seen
// one of these refusals recognises the shape of the answer, and **ADR 0232**'s rule that one question
// has one reading: the question here is *which rule is missing at this position*, and the position is
// what the caller knows.
func nestedContainerErr(e Expr) error {
	if key, wantsHash := exprWantingContainerKey(e, 0); wantsHash {
		return nestedContainerKeyErr(key)
	}
	return nestedContainerElemErr(e)
}

// nestedContainerElemErr is the element half of the door above: the object the slot would hold is
// another container, and the tag beside the slot has no number for an element that nests deeper than
// the elements one level in can be tagged as.
func nestedContainerElemErr(e Expr) error {
	return fmt.Errorf("codegen: a compiled container cannot hold %s yet: a slot of a compiled container carries one word and the tag that says what that word is, and this element nests deeper than any tag the compiler writes, so the word would be the inner object's handle — printed by handle and compared by address (roadmap L11.1). Build that inner value separately, one level shallower, so every element is a value a slot can tag", exprSnippet(e))
}

// nestedContainerKeyErr is the key half of the door above: the entry would be filed under the handle
// the allocator handed the key, and the rule that asks two containers with the same elements whether
// they are the same key — the hash — is the half this backend does not have.
func nestedContainerKeyErr(e Expr) error {
	return fmt.Errorf("codegen: a compiled container cannot hold %s as a key yet: a dict finds an entry by hashing its key, and this backend has no hashing rule for a container, so the entry would live under whichever handle the allocator handed out and nothing but the program that stored it could find it again (roadmap L11.1). Key the dict by a value that has a hash — a text, an integer, a float, a verdict or the void — and keep the container in the value", exprSnippet(e))
}

// dictLitWantsContainerKey is the key half of the door read without an irGen in hand — the shape
// question `dictWantsContainerKey` asks when the codegen state exists, minus the arms that need it.
func dictLitWantsContainerKey(dl *DictLit) bool {
	for _, k := range dl.Keys {
		if dictKeyHasNoHash(k) {
			return true
		}
	}
	return false
}

// exprWantingContainerKey is the walk that lets the ELEMENT door report the KEY rule when the element it
// is declining contains a dict that needs it: `print([{{[1]: 2}}])` refuses for want of a hashing rule on
// `[1]`, not for want of a tag on the set, and a sentence that named the tag would send the reader to fix
// the wrong thing. It walks down through the containers the element nests (**Gap R.75**'s depth, bounded by
// the same small number the door's own rulings reach), and it returns the offending KEY expression, not the
// object that holds it, because the key is what the sentence quotes — `{[1]: 2}` as an element says
// “cannot hold a list literal as a key”, which is the object the reader has to change
// (**ADR 0249**'s refusal names the missing half; **ADR 0232**'s one question, here asked of the object).
func exprWantingContainerKey(e Expr, depth int) (Expr, bool) {
	if e == nil || depth > 3 {
		return nil, false
	}
	switch n := e.(type) {
	case *DictLit:
		for _, k := range n.Keys {
			if dictKeyHasNoHash(k) {
				return k, true
			}
		}
		for _, v := range n.Vals {
			if key, ok := exprWantingContainerKey(v, depth+1); ok {
				return key, true
			}
		}
		for _, k := range n.Keys {
			if key, ok := exprWantingContainerKey(k, depth+1); ok {
				return key, true
			}
		}
	case *ListLit:
		for _, el := range n.Elems {
			if key, ok := exprWantingContainerKey(el, depth+1); ok {
				return key, true
			}
		}
	case *SetLit:
		for _, el := range n.Elems {
			if key, ok := exprWantingContainerKey(el, depth+1); ok {
				return key, true
			}
		}
	}
	return nil, false
}

// dictKeyHasNoHash is `dictKeyIsContainer`'s shape question, asked with no codegen state: a list, a dict or
// a set literal has no hash; a tuple inherits one only when every element has one (**ADR 0361**'s
// hashability walk); and a key spelled as a call to the empty-container builtin is the same object its
// literal spells (**Gap R.360**).
func dictKeyHasNoHash(k Expr) bool {
	switch n := k.(type) {
	case *ListLit:
		if n.IsTuple() {
			return !tupleElementsHashable(n)
		}
		return true
	case *DictLit, *SetLit:
		return true
	case *Call:
		_, isEmpty := emptyContainerLiteral(n)
		return isEmpty
	case *Comp:
		// A comprehension builds a list, a dict or a set, and none of the three has a hash: the entry would
		// be filed under whichever handle the allocator handed the object, which is the rule
		// `nestedContainerKeyErr` states for the literal spelling of the same value (roadmap L11.1,
		// **Gap R.487**'s key side; the set-member side of this family is **Gap R.485**).
		switch n.Kind {
		case CompList, CompDict, CompSet:
			return true
		}
	}
	return false
}

func (g *irGen) literalNeedsHeap(e Expr) bool {
	// The static global layout is an int array: an element that is interned text, a float box, or
	// a handle into another container has no representation in it, so the heap builder must make
	// the object instead (roadmap Gap J.6, L11.1).
	// The empty container spelled as a call is a container: an element that is `set()`, `list()` or
	// `dict()` is a heap object, and the static int-array layout has no representation for a handle plus
	// the tag that says what it is. `print([set(), list(), dict()])` printed `[1, 2, 3]` while this gate
	// asked only the literal (roadmap Gap R.360).
	slotsContainer := func(x Expr) bool {
		if isStringExpr(x) || isContainerLiteral(x) || g.compIsContainerSlotExpr(x) {
			return true
		}
		// The answer of a call the scan proves a container is a heap object too: the callee's builders made
		// it, and the static int-array layout has no word for a handle beside the tag that says which of the
		// four it is (roadmap **Gap R.530**, **ADR 0457**; the same arm **ADR 0192** grew for a comprehension).
		if g.containerAnswerIsContainerSlotExpr(x) {
			return true
		}
		if c, isCall := x.(*Call); isCall {
			if _, isEmpty := g.emptyContainerCall(c); isEmpty {
				return true
			}
		}
		return false
	}
	switch n := e.(type) {
	case *ListLit:
		for _, el := range n.Elems {
			if slotsContainer(el) {
				return true
			}
		}
	case *SetLit:
		for _, el := range n.Elems {
			if slotsContainer(el) {
				return true
			}
		}
	case *DictLit:
		for _, k := range n.Keys {
			if slotsContainer(k) {
				// A key that is interned text, another container or the immutable spelling has no
				// representation in the static int array — the key is an object, and an object is a
				// handle plus the tag that says what it is (roadmap L11.3, ADR 0361).
				return true
			}
		}
		for _, v := range n.Vals {
			if slotsContainer(v) {
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
	// The slot's own kind is the question, and the empty container spelled as a call answers it the same
	// way a literal does — `print([set(), list()])` printed the two handles while this list of spellings
	// stopped at `isContainerLiteral` (roadmap Gap R.360, ADR 0232's self-describing slots).
	slotsItself := func(x Expr) bool {
		return g.floatSlotExpr(x) || g.noneSlotExpr(x) || isContainerLiteral(x) || g.boolSlotExpr(x) ||
			g.instanceSlotExpr(x) || g.carriedCallIsPairElem(x) || g.emptyContainerSlotExpr(x) ||
			g.loopItemIsContainer(x) || g.elementNameHoldsContainer(x) || g.compIsContainerSlotExpr(x) ||
			// The answer of a container-answering call is a handle whose kind only the object can say: `[f()]`
			// with `def f(): return [1, 2]` claimed one element kind, the folded global refused the handle, and
			// the membership test below it died reporting an element kind the program had already proven
			// (roadmap **Gap R.530**, **ADR 0457**; **ADR 0333**'s scan is the table this asks).
			g.containerAnswerIsContainerSlotExpr(x) ||
			// The answer of a hand-off call — `def g(v): return v` — wears its argument's kind, and when that
			// kind is a float box, the void, a verdict or another container the payload means nothing on its
			// own: `[g(None), 1]` printed `[0, 1]` because the object claimed one element kind and the number
			// printer rendered the void's payload (roadmap **Gap R.502**, **ADR 0232**).
			g.forwardedCallSlotsItself(x)
	}
	switch n := e.(type) {
	case *ListLit:
		for _, el := range n.Elems {
			if slotsItself(el) {
				return true
			}
		}
	case *SetLit:
		for _, el := range n.Elems {
			if slotsItself(el) {
				return true
			}
		}
	case *DictLit:
		for _, k := range n.Keys {
			if g.floatSlotExpr(k) || g.noneSlotExpr(k) || g.boolSlotExpr(k) || g.carriedCallIsPairElem(k) ||
				g.forwardedCallSlotsItself(k) {
				return true
			}
		}
		for _, v := range n.Vals {
			if slotsItself(v) {
				return true
			}
		}
	}
	return false
}

// emptyContainerSlotExpr is the member/value arm of the same question: a slot that holds `set()`,
// `list()` or `dict()` holds a heap object whose kind the object, not the spelling, decides — so the
// container has to be built self-describing and the tag written beside the handle (roadmap Gap R.360).
func (g *irGen) emptyContainerSlotExpr(e Expr) bool {
	c, isCall := e.(*Call)
	if !isCall {
		return false
	}
	_, isEmpty := g.emptyContainerCall(c)
	return isEmpty
}

// instanceSlotExpr is the fourth question literalNeedsTags asks of each slot: is this payload an
// instance's heap handle? A number's payload is its value, a float box's, a container's and an instance's
// are all only handles, so the container has to stop claiming one kind and let the printer read the tag
// (roadmap Gap R.209).
func (g *irGen) instanceSlotExpr(e Expr) bool {
	if g == nil || e == nil {
		return false
	}
	t, ok := g.elemKindTag(e)
	return ok && t == int32(TagInstance)
}

// literalHasPairElement asks the third question of a container literal, beside "does it need the heap" and
// "can its slots say what they hold": is any element a value the compiler cannot write into a static global?
// A name the pair road bound (`n = xs[0]` over a container the program built) holds a (payload, tag) pair
// whose words only exist at run time — the static `{count, [N x i32]}` layout has no place for it, and
// asking its `value()` is the refusal Gap R.146 kept printing for `[n]`, `{"k": n}` and `min([n, 3])`. The
// heap builder already writes both words per slot (ADR 0187), so the answer is to route the literal there
// rather than to refuse the element (roadmap L11.1).
// pairElemPair is the element-side question: does this literal element name a value the pair road bound
// or *read*, and if so what are its two words? The builders below take the tag as a constant, which is
// right for a literal element and wrong for a pair, whose tag is a register the objects wrote. Passing the
// register to the same call is the whole difference between `[n]` printing `[7]` and refusing (roadmap
// L11.1, Gap R.146; ADR 0187's rule that a payload is never written without its tag).
//
// Gap R.224 widened the door from "a name the pair road bound" to "anything the pair road reads": an
// element that is itself a subscript — `[xs[1], 2]`, `[{"a": "x"}["a"], 2]` — denotes the same (payload,
// tag) pair that `print(xs[1])` asks for, and a builder that stores one word plus a compile-time tag for it
// is storing an `@str_tab` index and calling it a string. The gate that admits it lives beside
// `pairElemPair`'s own file, in `pair_element_read.go`.
func (g *irGen) pairElemPair(b *strings.Builder, e Expr) (payload, tag string, ok bool) {
	if !g.isPairElem(e) {
		return "", "", false
	}
	switch n := e.(type) {
	case *Name:
		payload, tag = g.numericPairRegs(b, n.Value, n.Span())
		return payload, tag, true
	case *Index:
		// The one subscript door, the same call `print(xs[i])`, `v = xs[i]` and ADR 0331's literal read
		// emit. Asking it rather than re-implementing the read is what keeps a slot stored by the
		// builder and the same slot printed by the printer from ever meaning different things.
		return g.taggedSlotPair(b, n)
	case *Call:
		// The answer of a call that carries its own kind: the callee stored the word beside its payload, so
		// the position is handed to the builder as the two registers every other pair element arrives in
		// (roadmap L11.1, Gap R.219, ADR 0368). A door that declines hands the element back to the road that
		// stores one word, and that road refuses such a value by name rather than storing a payload without
		// its tag (ADR 0273). A door that errors declines, the way `taggedOperand` declines for a container
		// it cannot materialise: the element goes back to the one-word road, which refuses such a value by
		// name rather than storing a payload without its tag (roadmap ADR 0166's rule that a broken module
		// is never reported as the program's fault).
		p, t, okPair, perr := g.pairCallPair(b, e)
		if perr != nil || !okPair {
			// The hand-off round of the same question: `def g(v): return v` answers the kind its argument
			// arrived with, and when the argument is a pair the answer is that pair — read at the call site,
			// where the argument is, because the callee returns the caller's own word unchanged
			// (roadmap **Gap R.502**, **ADR 0445**).
			if p2, t2, okFwd, ferr := g.forwardedCallPair(b, e); ferr == nil && okFwd {
				return p2, t2, true
			}
			return "", "", false
		}
		return p, t, true
	}
	return "", "", false
}

// nameIsPairElem is `pairElemPair`'s question without the two loads: is this expression a name the
// pair road bound, whose payload means a different thing per kind and so cannot be stored alone?
// The gate for a whole literal has to be answered before any IR is emitted, and a predicate that
// emitted its own loads would leave two dead registers in the module for every literal it opened.
func (g *irGen) nameIsPairElem(x Expr) bool {
	nm, isName := x.(*Name)
	if !isName || g.taggedVars == nil || !g.taggedVars[nm.Value] || g.taggedOrigin == nil {
		return false
	}
	switch g.taggedOrigin[nm.Value] {
	case taggedOriginArith, taggedOriginFloat, taggedOriginParam, taggedOriginParamArith, taggedOriginSlot, taggedOriginLoop, taggedOriginFold:
		return true
	}
	return false
}

// elemTaggableOrPair is the dict/set gate's element question: can the tagged builder be handed this
// position at all? A pair element can — its tag arrives as a register the objects wrote, which is
// what `rt_dict_put_tagged` and `rt_set_add_tagged` already take — and anything else can only when
// `elemKindTag` can label it. An element the tag cannot label (a tuple, a call that hands back text
// on one path and a number on another) keeps the literal on the road that refuses it: a payload
// stored beside a tag nobody proved is exactly the wrong answer the refusal exists to prevent
// (roadmap L11.1, Gap R.146; ADR 0232's tagged slots, ADR 0187's payload-never-written-without-tag).
//
// Since Gap R.224 the pair half of the question is `isPairElem`, which counts a subscript beside a bound
// name: the element `xs[1]` is read by the same door `print(xs[1])` asks, so the builder is handed a
// register for the tag exactly as it is for a name the pair road bound.
func (g *irGen) elemTaggableOrPair(e Expr) bool {
	if g.isPairElem(e) {
		return true
	}
	_, ok := g.elemKindTag(e)
	return ok
}

// literalTakesPairSlots is the dict's and the set's third gate, beside "does it need the heap"
// and "can its slots say what they hold": does some position name a value the pair road bound,
// **and** can every other position still be labelled by the compile-time tag? When both hold the
// literal goes to the tagged builder, which asks each position for two words instead of one; when
// the second half fails the builder never runs and the element keeps Gap R.146's refusal, which
// names the missing half rather than storing a payload that only means something with a tag.
//
// ADR 0306 opened this door for a list's element. A dict interleaves key and value in one element
// array and a set dedups a member by its (payload, tag) pair, so the same question is simply asked
// more than once per literal — which is why it had to be answered per position and not per literal.
func (g *irGen) literalTakesPairSlots(e Expr) bool {
	if !g.literalHasPairElement(e) {
		return false
	}
	switch n := e.(type) {
	case *SetLit:
		for _, el := range n.Elems {
			if !g.elemTaggableOrPair(el) {
				return false
			}
		}
		return true
	case *DictLit:
		for i := range n.Keys {
			if !g.elemTaggableOrPair(n.Keys[i]) || !g.elemTaggableOrPair(n.Vals[i]) {
				return false
			}
		}
		return true
	}
	return false
}

// taggedSlotWords answers one position of a tagged dict entry or set member: the payload, and the
// tag as the word it travels in — a constant for an element the compiler can read off its spelling,
// a register for one the pair road bound. `rt_dict_put_tagged`/`rt_set_add_tagged` take an i32 for
// the tag, and a register is an i32 (ADR 0306's discovery, applied to the two builders that had not
// asked it): the runtime needed nothing, only the caller that stops guessing (roadmap L11.1,
// Gap R.146).
func (g *irGen) taggedSlotWords(b *strings.Builder, e Expr) (payload, tag string, fromPair bool, err error) {
	if p, t, isPair := g.pairElemPair(b, e); isPair {
		return p, t, true, nil
	}
	v, _, err := g.heapElemKind(b, e)
	if err != nil {
		return "", "", false, err
	}
	tg, ok := g.elemKindTag(e)
	if !ok {
		// Unreachable through the gates above; honest as a refusal if one is ever widened.
		if nm, isName := e.(*Name); isName {
			return "", "", false, g.taggedVarErr(nm.Value)
		}
		return "", "", false, fmt.Errorf("codegen: a dict/set slot cannot be labelled with a tag, so its payload cannot be stored (roadmap L11.1, Gap R.146)")
	}
	return v, strconv.FormatInt(int64(tg), 10), false, nil
}

// guardHashableTag is the one question a dict key and a set member have to be asked before the store,
// which the tag makes a run-time fact once the value arrived as a pair: **can this value be a key at
// all?** CPython refuses a list, a dict and a set member with `TypeError: unhashable type: 'list'` and
// company, and a payload alone answers that question "yes" — the handle of a list is an i32, and two
// lists with different handles are two members, so `{n}` over a container slot became a set that
// happily holds an address (roadmap Gap R.81, which owns the general rule; ADR 0166's "raise what the
// reference raises").
//
// It is emitted only for a tag the objects wrote. A tag the compiler read off the element's spelling
// is a constant this pass already looked at, and the literal path keeps its own gate (`dictWantsContainerKey`)
// and its own open row (Gap R.81) — this guard covers exactly the door ADR 0310 opened, and nothing wider.
func (g *irGen) guardHashableTag(b *strings.Builder, tag string, sp Span) {
	for _, k := range []struct {
		tg   int32
		name string
	}{
		{int32(TagList), "list"},
		{int32(TagDict), "dict"},
		{int32(TagSet), "set"},
	} {
		c := g.newTmp()
		fmt.Fprintf(b, "  %s = icmp eq i32 %s, %d\n", c, tag, k.tg)
		g.markI1(c)
		g.branchRaise(b, c, "TypeError", "unhashable type: '"+k.name+"'", sp, "hash")
	}
}

func (g *irGen) literalHasPairElement(e Expr) bool {
	pairName := func(x Expr) bool {
		if nm, isName := x.(*Name); isName {
			return g.taggedVars[nm.Value] && g.taggedOrigin != nil
		}
		// An element that is itself a subscript denotes the pair the pair door reads, and the builder
		// that never learns it will store one word and a tag the compiler invented (roadmap Gap R.224).
		// The third such element is the answer of a call that carries its own kind: its payload means a
		// different thing per arm, so the slot has to be written with the callee's word beside it
		// (roadmap Gap R.219, ADR 0368).
		return g.subscriptIsPairElem(x) || g.carriedCallIsPairElem(x) || g.popAnswerIsPairElem(x) ||
			g.forwardedCallIsPairElem(x)
	}
	switch n := e.(type) {
	case *ListLit:
		for _, el := range n.Elems {
			if pairName(el) {
				return true
			}
		}
	case *SetLit:
		for _, el := range n.Elems {
			if pairName(el) {
				return true
			}
		}
	case *DictLit:
		for _, k := range n.Keys {
			if pairName(k) {
				return true
			}
		}
		for _, v := range n.Vals {
			if pairName(v) {
				return true
			}
		}
	}
	return false
}

// floatSlotExpr is the question a slot asks to find out whether it holds a double: the literal, or
// any expression the compiler can see is one — `1.5 if c > 0 else 2.5` is written by a program that
// put a float in the slot, and only the ternary's shape hides that from a literal test. Asked as an
// expression question and not a literal question for the same reason ADR 0259 asks `boolSlotExpr`:
// the builder below stores the `@float_box` handle and tags it, and a container that does not say
// "the slots describe themselves" is printed by the container-wide kind, which reads that handle as
// the number it is — `[1.5 if c > 0 else 2.5]` came out `[1]`, the box's index in the heap (roadmap
// L11.6, Gap R.102, ADR 0262; the sibling of ADR 0233's float element).
func (g *irGen) floatSlotExpr(e Expr) bool {
	if isFloatLitExpr(e) {
		return true
	}
	// The item, not the name: `for k in [1.5, 2.5]:` records the kind once per iteration, and a slot
	// holding this item holds a @float_box handle whatever the name's own records say (roadmap Gap R.440).
	if answered, yes := g.loopItemIsThis(e, int32(TagFloat)); answered {
		return yes
	}
	return g != nil && g.isFloat(e)
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
	// The same per-item record: `[True, False]` walked by a `for` hands the body a verdict whose NAME
	// says nothing, exactly as the written `verdict()` does (roadmap Gap R.440, beside ADR 0259).
	if answered, yes := g.loopItemIsThis(e, int32(TagBool)); answered {
		return yes
	}
	return g != nil && g.printsAsBool(e)
}

// noneSlotExpr is the void arm of `literalNeedsTags`, the sibling of `floatSlotExpr` and
// `boolSlotExpr`, and it asks the ELEMENT rather than the spelling: the written `None`, or whatever
// the ADR 0172 predicate behind print, `str`, truthiness and equality answers — a name whose latest
// binding wrote the void, a body that never returns a value, an in-place mutator, a folded dict miss.
// Its two neighbours in the gate have gone past the spelling for years (`print([verdict(), 0])` is
// built tagged because `boolSlotExpr` asks what the CALL answers, ADR 0259); this arm had never been
// widened, so a slot holding the void THROUGH A BINDING was not a void to the gate: the literal stayed
// on the static `{i32, [N x i32]}` array, whose element reader folds a void-bound name to the payload
// the void happens to carry, and the screen printed the number — `k = None` / `print([k])` wrote `[0]`
// at the exit code of success while `print([None])` wrote `[None]` all along (roadmap Gap R.435, ADR 0421;
// the read-door twin is **ADR 0420**'s `elemKindTag` arm, and the two doors ask this ONE predicate).
//
// The two exemptions `elemKindTag` carries are restated here rather than rediscovered downstream
// (**ADR 0249**'s rule that a gate and a door must test the same thing): a name the unroll gave a
// per-item tag is THIS item and not the void some earlier binding left behind, and a name the
// container records claim holds a handle whose kind the object carries. Both are read from the same
// maps the door reads, so a future widening moves the gate and the door together or neither.
func (g *irGen) noneSlotExpr(e Expr) bool {
	if isNoneLitExpr(e) {
		return true
	}
	if g == nil || e == nil {
		return false
	}
	if answered, yes := g.loopItemIsThis(e, int32(TagNone)); answered {
		return yes
	}
	if nm, isName := e.(*Name); isName {
		if g.nameIsContainerRecorded(nm) {
			return false
		}
	}
	return g.isNoneExpr(e)
}

// loopItemTag is the raw reading of the record the unrolled `for` road keeps (codegen_for.go, written
// once per iteration beside the interned-text record **ADR 0233** grew): the kind of the item this body
// is standing in, and whether the record speaks for this name AT ALL. The second answer is the whole
// of the difference between `k = None` / `for k in [1, 2]: print([k])` answering `[1]`, `[2]` and
// answering a mixed-list refusal: the name is an item whose kind is a plain number, and a gate that
// hears "no self-describing kind" and then goes asking the name's stale void record has read the
// binding that the loop replaced (roadmap Gap R.440, and the exemption **ADR 0421** had to write into
// `noneSlotExpr` to keep exactly that program answering).
func (g *irGen) loopItemTag(e Expr) (int32, bool) {
	if g == nil || e == nil || g.loopElemTag == nil {
		return 0, false
	}
	nm, isName := e.(*Name)
	if !isName {
		return 0, false
	}
	t, has := g.loopElemTag[nm.Value]
	return t, has
}

// loopItemIsThis is the arm of that reading a gate asks: the name is an item of an unrolled loop, and
// this arm's kind is (or is not) what the item is. An unanswered question hands the name back to its
// own records; an answered one does not, which is what makes the item and the name agree.
func (g *irGen) loopItemIsThis(e Expr, tag int32) (answered, yes bool) {
	t, has := g.loopItemTag(e)
	if !has {
		return false, false
	}
	return true, t == tag
}

// loopItemKindTag is the door's half of the same reading: the item's kind, when it is one a compiled
// word cannot render on its own. A plain number and an interned text answer "unknown" here, which is
// what keeps **ADR 0239**'s static-array fold — the one `sum`/`min`/`max` read a numeric literal from —
// reachable by a numeric loop, and the row's Definition of Done asserts it in the same commit as the
// widening.
//
// The record is the ONE reading of the unroll's per-item kind: the print door has read it since **Gap
// R.358**, the truth door since **ADR 0388**, the builders' own `elemKindTag`/`elemTagFor` since the
// same row — what never read it were the four GATES beside them, `floatSlotExpr`, `boolSlotExpr`,
// `noneSlotExpr` and the container arm of `literalNeedsTags`, so `for k in [None, 1.5]:` printed `None`
// and `1.5` for `print(k)` and `[0]` and `[1]` for `print([k])`: one value, one pair of parentheses
// apart, answered by two doors of which only one had been told what the item is (**ADR 0249**'s
// gate-and-door rule, one position over from where **ADR 0421** wrote it; roadmap Gap R.440).
func (g *irGen) loopItemKindTag(e Expr) (int32, bool) {
	t, has := g.loopItemTag(e)
	if !has || !slotTagSelfDescribing(t) {
		return 0, false
	}
	return t, true
}

// loopItemExpr is the third arm of the unroll's record: WHICH element this body copy walks, not merely
// what kind it is. A sentence the body raises can name the member only if the pass still knows which
// item stood in front of it - `for k in [1, 2, 9]: s.remove(k)` says `KeyError: 9` in the reference and
// said `key not found` here, because the tag told the pass the item was a number and nothing told it
// which number (roadmap Gap R.189's sentence, on the set side; roadmap Gap R.440's record).
func (g *irGen) loopItemExpr(e Expr) (Expr, bool) {
	if g == nil || e == nil || g.loopElem == nil {
		return nil, false
	}
	nm, isName := e.(*Name)
	if !isName {
		return nil, false
	}
	item, has := g.loopElem[nm.Value]
	return item, has
}

// loopItemIsContainer is the container arm of the same reading: a slot holding an ITEM that is a
// list, a dict, a set or an instance holds a heap handle whose kind the object carries, which is the
// claim `isContainerLiteral` makes for a spelled literal and cannot make for a name (roadmap Gap R.440).
func (g *irGen) loopItemIsContainer(e Expr) bool {
	t, has := g.loopItemTag(e)
	if !has {
		return false
	}
	switch t {
	case int32(TagList), int32(TagDict), int32(TagSet), int32(TagInstance):
		return true
	}
	return false
}

// isFloatLitExpr and isNoneLitExpr are the two elements that make a container literal need the
// tagged path even when every element is alike: their payloads are a @float_box handle and nothing
// at all, neither of which a container-wide kind can describe (roadmap L11.1, ADR 0233).
// `isNoneLitExpr` survives as the spelled-literal half of `noneSlotExpr`; nothing else asks it.
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
	// Same per-iteration fact `elemTagFor` reads: an unrolled loop variable is this item, not the kind
	// some earlier binding gave the name (roadmap Gap R.358). Read through the one predicate the gates
	// beside it ask too, so the two cannot keep two readings of one record (roadmap Gap R.440).
	if t, ok := g.loopItemKindTag(e); ok {
		return t, true
	}
	// A name the ITERATION rebound is its OWN ITEM, not the kind the name's earlier binding gave it.
	// The container tables below are cumulative — `xs = [7]` puts `xs` in `listVars` for the rest of the
	// module — and asking them about a loop variable is how `xs = [7]` / `for xs in [1, 2]:` /
	// `print([xs, 1.5])` printed `[, 1.5]` where the reference prints `[1, 1.5]`: the slot stored the
	// integer 1 and was tagged TagList, so the tag-reading printer went looking for a container at
	// address 1 and printed nothing (roadmap **Gap R.489**; the same per-iteration record ADR 0233 and
	// **Gap R.358** read for the tag table, asked here of the name the unroll replaced). Asked of the
	// ITEM and not of the name, because the item is what the body of this copy walks.
	if nm, isName := e.(*Name); isName && g != nil {
		if item, isItem := g.loopItemExpr(nm); isItem {
			if t, ok := g.elemKindTag(item); ok {
				return t, true
			}
		}
	}
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
	// The empty container spelled as a CALL is the same object the literal spells, and every gate that
	// recognises it already asks `emptyContainerLiteral` — the print gate, the fold gate, `taggableNestedElem`.
	// The tag question was the one that did not, so a slot that stored `set()`, `list()` or `dict()` was
	// labelled `TagInt` beside a heap handle: `print(d[None])` over `{None: set()}` wrote `3`, `bool` of an
	// empty entry answered `True`, and `len` of a slot named `'int'`. The kind is the object's own, so the
	// answer comes from the same door the gates ask, and a program that defines its own `set` keeps its
	// answer too (roadmap Gap R.360, beside ADR 0232's tag-and-store pair and Gap K.3's empty-set spelling).
	if c, isCall := e.(*Call); isCall && g != nil {
		if lit, isEmpty := g.emptyContainerCall(c); isEmpty {
			switch lit.(type) {
			case *SetLit:
				return int32(TagSet), true
			case *DictLit:
				return int32(TagDict), true
			case *ListLit:
				return int32(TagList), true
			}
		}
		// A call whose answer the scan proves a container carries the callee's kind, not the default: the
		// payload is the handle the callee's builders made, and the tag is what routes it to the container
		// printer, the content equality and the object's own membership scan instead of reading the
		// allocator's slot as the integer it is not. `taggableNestedElem` asks the same predicate, so the
		// label and the build cannot disagree about one element (roadmap **Gap R.530**, **ADR 0457**;
		// **ADR 0189**'s nested-container rule, one spelling further out than the literal).
		if t, okTag := g.containerAnswerSlotIsContainer(c); okTag {
			return t, true
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
	case *Comp:
		// A comprehension written as an element IS the container its brace spells: the payload is the
		// handle of the object the road builds and the tag says list, dict or set, so the printer reaches
		// the container printer and the comparison reaches the content compare instead of reading the
		// handle as the integer the allocator handed out. The build door has had this arm since
		// **ADR 0192**; the tag road is the half that was missing, and it is the half that printed
		// `[1, 1]` for `print([[{'a': 1} for x in range(1)], 1])` (roadmap **Gap R.487**).
		if t, ok := g.compSlotIsContainer(e); ok {
			return t, true
		}
		// A comprehension whose contents no slot can describe declines here and is answered by the arms
		// below, exactly as it was before this case existed — the door does not newly refuse a program it
		// used to label.
		break
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
	// The two singletons a BINDING leaves behind, read from the same ADR 0172 records print, truthiness,
	// `str` and equality live by. The tag table has always had a void and a verdict in it (value.go) and
	// both literal arms answer them; what was missing is the NAME whose binding wrote one, so every door
	// that asks this question — the mixed-dict pair, the membership test, the tagged read — fell through to
	// the blanket `TagInt` below and called the void an integer. That is the whole of roadmap Gap R.286:
	// `d = {None: 3.5}` / `k = None` / `print(d[k])` asked the object for the entry keyed (0, TagInt), the
	// void's payload IS 0 and its tag is not, and the program died `KeyError` where the reference prints
	// `3.5`. ADR 0419 gave the integer KEY its tag; this gives the bound VALUE its own.
	//
	// Two names stand aside, for the same reason every other arm here does: a loop variable the unroll gave
	// a per-item tag is THIS item (Gap R.358), and a name the container records claim holds a handle whose
	// kind the object carries.
	if nm, isName := e.(*Name); isName && g != nil {
		if _, perItem := g.loopElemTag[nm.Value]; !perItem && !g.nameIsContainerRecorded(nm) {
			if g.noneVars[nm.Value] {
				return int32(TagNone), true
			}
			// A verdict is the number the reference says it is, and the tag beside it is what lets the
			// printer answer `True` rather than `1` — ADR 0259's rule for the literal, taken to the binding
			// (roadmap Gap R.112's sibling, Gap R.286).
			if g.boolVars[nm.Value] {
				return int32(TagBool), true
			}
		}
	}
	// The void a CALL answers is the same void the spelled singleton and the bound name answer: a body that
	// never returns a value, an in-place mutator (`xs.append(2)`), a folded dict miss, an all-None `min`.
	// The gate (**ADR 0421**'s `noneSlotExpr`) asks this same predicate and sends such a slot to the tagged
	// road; a door that then labelled it `TagInt` beside a void payload declined the very build the gate
	// ordered, which is **ADR 0249**'s gate-and-door disagreement measured as a refusal — `def f(): pass` /
	// `print([f()])` went from `[0]` at exit 0 to `a compiled list holds either strings or numbers, not both`
	// at exit 1 (roadmap Gap R.435; the print road has asked `isNoneExpr` since **ADR 0172**).
	if _, isCall := e.(*Call); isCall && g != nil && g.isNoneExpr(e) {
		return int32(TagNone), true
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
	// A user instance is not an integer wearing a tag. The payload has always been the heap handle;
	// the missing word was the label beside it, which made the printer, `len`, membership and the loop
	// each report the kind the *slot* carried rather than the object the program stored (roadmap
	// Gap R.209). The class name itself is the object's — its slot 0 already has the dispatch id, and
	// the module's class table turns that into `__main__.C` at run time.
	if nm, isName := e.(*Name); isName && g != nil && g.varClasses[nm.Value] != "" {
		return int32(TagInstance), true
	}
	if c, isCall := e.(*Call); isCall && g != nil {
		if nm, isName := c.Fn.(*Name); isName {
			if _, isClass := g.classInfos[nm.Value]; isClass {
				return int32(TagInstance), true
			}
		}
	}
	// A body that is nothing but a hand-off — every `return` naming ONE of its own parameters — answers
	// the kind the caller handed in, and the caller's argument is right here to be asked. The tables above
	// all read what a `return` SPELLS, so `def g(v): return v` answered the table's default for every
	// value but an int: `print([g(None), 1])` wrote the void's payload `0` beside `TagInt` and printed
	// `[0, 1]`, `print([g([1, 2]), 3])` printed the allocator's slot as a number, and a comprehension
	// whose element was such a call tagged every slot `TagInt` and printed `[1, 0, 1, 0]` for
	// `[1, 'a', 2.5, None]` at exit 0 (roadmap **Gap R.502**, **ADR 0445**). Asked LAST among the call
	// readings, so an answer those tables already name keeps the label it has always had, and a question
	// this door cannot answer falls through to the reading it had before — the door answers, it never takes
	// an answer away.
	if t, ok := g.forwardedAnswerKindTag(e); ok {
		return t, true
	}
	return int32(TagInt), true
}

// forwardedAnswerKindTag is the tag half of the hand-off reading: the constant the argument's own spelling
// answers, or nothing where only the objects can tell (that case travels as a pair, not as a constant,
// through `forwardedCallIsPairElem` and `forwardedCallPair`).
func (g *irGen) forwardedAnswerKindTag(e Expr) (int32, bool) {
	tag, fromRegister, ok := g.forwardedAnswerTagUnder(e)
	if !ok || fromRegister {
		return 0, false
	}
	t, err := strconv.Atoi(tag)
	if err != nil {
		return 0, false
	}
	return int32(t), true
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
		if g.isPairElem(dl.Keys[i]) || g.isPairElem(dl.Vals[i]) {
			// One gate for both sides, and it knows every shape whose kind arrives as a register: see
			// elemsTaggable's rule, restated (roadmap L11.1, ADR 0368).
			mustTag = true
			keyOther = true
			valOther = true
			continue
		}
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
//
// The tuple spelling is the exception, because it is the container the language *does* hash: the
// dict's lookup is a scan over (payload, tag) pairs (rt_dict_find → rt_slot_matches →
// rt_payload_eq → rt_container_eq), so two equal tuples find the same entry and a tuple never
// collides with a list of the same slots — rt_container_eq asks the object's tuple flag. A tuple
// carrying a container stays refused: the reference refuses to hash that one too, and the door has
// no raise in it (roadmap L11.3, ADR 0361).
func (g *irGen) dictWantsContainerKey(dl *DictLit) bool {
	for _, k := range dl.Keys {
		if g.dictKeyIsContainer(k) {
			return true
		}
	}
	return false
}

// dictKeyIsContainer is the hashability question one dict key asks, asked of ONE door: a list, a dict
// or a set literal has no hash, an unhashable tuple inherits its elements' lack of one, and — since the
// empty container learned to carry its own kind — neither does `set()`, `list()` or `dict()` spelled as a
// call. The last arm is not a new refusal: it is the same object the literal spells arriving under a
// different spelling, and a key road that recognised only the literal would store the handle beside a tag
// nobody proved and print `{set(): 1}` where the reference raises `unhashable type: 'set'` (roadmap
// Gap R.360, beside Gap R.81 which owns the general hashability rule and ADR 0310's tag-from-the-pair raise).
func (g *irGen) dictKeyIsContainer(k Expr) bool {
	if ln, isList := k.(*ListLit); isList && ln.IsTuple() {
		return !tupleElementsHashable(ln)
	}
	switch k.(type) {
	case *ListLit, *DictLit, *SetLit:
		return true
	case *Comp:
		// The same object the literal spells, arriving under the comprehension's brace: a list, a dict or a
		// set has no hash, so the entry would be filed under whichever handle the allocator handed out — the
		// rule this door already states for `[1]: 1`, read of `print([[x for x in [1]]] and of
		// `{[x for x in [1]]: 1}` (roadmap **Gap R.487**'s key side, beside **Gap R.81** and **Gap R.360**).
		return g.compIsContainerSlotExpr(k)
	}
	if c, isCall := k.(*Call); isCall && g != nil {
		if lit, isEmpty := g.emptyContainerCall(c); isEmpty {
			switch lit.(type) {
			case *ListLit, *DictLit, *SetLit:
				return true
			}
		}
	}
	return false
}

// tupleElementsHashable is the reference's walk: a tuple hashes by hashing its elements left to
// right, so a tuple carrying a list, a dict or a set is not hashable either, and the walk that
// decides it is shared with the gate that raises the reference's sentence (roadmap L11.3, ADR 0361).
func tupleElementsHashable(ln *ListLit) bool {
	for _, el := range ln.Elems {
		switch el := el.(type) {
		case *ListLit:
			if el.IsTuple() {
				if !tupleElementsHashable(el) {
					return false
				}
				continue
			}
			return false
		case *DictLit, *SetLit:
			return false
		case *Comp:
			// The brace builds a list, a dict or a set, and the walk is the reference's: a tuple hashes by
			// hashing its elements, so a tuple carrying the object a comprehension builds is not hashable
			// either (roadmap L11.3, **ADR 0361**, beside **Gap R.487**'s key door). A generator expression is
			// the object the reference happily hashes — an iterator is a value with an identity — so the arm
			// says nothing about it and the tuple keeps the door it has (roadmap Gap R.195).
			switch el.Kind {
			case CompList, CompDict, CompSet:
				return false
			}
		}
	}
	return true
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
	// The identity question is asked of ONE clause's target against THAT clause's iterable; with a clause
	// list the element could copy any of them and the iterable that decides the kind is whichever bound
	// it, so this door stays out and the object is asked at run time (roadmap Gap R.352).
	if !compIsSingleClause(c) {
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
	case int32(TagBool), int32(TagFloat), int32(TagNone), int32(TagList), int32(TagDict), int32(TagSet), int32(TagInstance):
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
		if g.dictWantsContainerKey(n) {
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
	case *Comp:
		// The object a comprehension builds has the slots its element expression writes, so the question
		// is asked of the literal with those contents — one reading for the comprehension and the literal
		// of the same shape (**ADR 0249**'s gate-and-door pair, roadmap **Gap R.487**/**Gap R.488**).
		return g.compContentsTaggable(n)
	case *Call:
		// The object a call hands back has the slots the callee's `return` was built from, so the question is
		// asked of those arms — one reading for the element and the answer it came from (**ADR 0249**'s
		// gate-and-door pair again, roadmap **Gap R.530**, **ADR 0457**). A function the scan could not
		// describe falls through to the empty-container question below, exactly as it did before this arm.
		if g.containerAnswerIsContainerSlotExpr(n) {
			return true
		}
	}
	_, ok := g.emptyContainerCall(e)
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
		if g.isPairElem(el) {
			// The element's kind travels beside it in a register the objects wrote — a name the pair road
			// bound (ADR 0306), a subscript the pair door reads (ADR 0332), or the answer of a call that
			// carries its own kind (ADR 0368). That is the definition of a slot that must be tagged, and a
			// gate that does not know the third shape refuses the program it builds (ADR 0249's rule about
			// a gate and a door that disagree; roadmap L11.1, Gap R.219).
			sawMustTag = true
			sawOther = true
			continue
		}
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
	// The item of an unrolled `for` carries its pair in the two places the unroll writes them: the
	// payload in the name's own i32 slot, the kind in this pass's per-item record. The print door has read
	// that pair since **Gap R.358** and the truth door since **ADR 0388**; this is the same two words asked
	// by the compare door (roadmap **Gap R.416**). The payload is NOT re-derived here: a float item's slot
	// holds the handle of its `@float_box`, a container item's the handle of the object the element built,
	// the void's nothing at all — asking `floatValue` or `containerOperand` of the name would mint a second
	// object out of whichever number the slot happens to hold, which is the ADR 0233 rule read from the
	// other side. Only the kinds a compiled word cannot render on their own come here; an int or interned
	// text item keeps the fold below, which is what keeps `for v in [1, 2]: print(v == 1)` emitting no pair
	// call at all (the row's Definition of Done).
	if t, selfDescribing := g.loopItemKindTag(e); selfDescribing {
		nm := e.(*Name)
		g.checkBound(b, nm.Value, nm.Span())
		v := g.newTmp()
		fmt.Fprintf(b, "\t%s = load i32, i32* %%_%s\n", v, nm.Value)
		if t == int32(TagFloat) {
			// rt_payload_eq unboxes, and the unboxer lives in the float block (ADR 0209's gate rule).
			g.floatFmtUsed = true
		}
		g.heapUsed = true
		return v, strconv.FormatInt(int64(t), 10), true
	}
	if nm, isName := e.(*Name); isName && g.taggedVars[nm.Value] {
		g.checkBound(b, nm.Value, nm.Span()) // the pair may not have been written (Gap R.68, ADR 0228)
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
		g.emitRtFloatNew(b, bh, inner, Span{})
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
	// The key's own expression travels so a missing key raises the reference's sentence — `KeyError: 'z'`,
	// the key's repr — rather than the module's generic prose (roadmap Gap R.189).
	g.checkKeyReadTagged(b, h, kv, kt, key, sp)
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
	// An unrolled loop bound this name to a value whose kind is not spelled by the name. The iteration
	// recorded that kind once per item; reading it here keeps `[[x] for x in [None, 1.5]]` from boxing
	// a handle and calling it an integer (roadmap Gap R.358, beside ADR 0233's unrolled loop-element tag,
	// and the same one reading the container literal's gates now ask — roadmap Gap R.440).
	if t, ok := g.loopItemKindTag(e); ok {
		return t
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
	// The historical sentence was "comes from a loop over a mixed list", which is only true of the loop
	// road that sets the status without an origin. Every road that binds a pair now records where it came
	// from (taggedOrigin), so this is the genuine unknown case: say what is known — the value's kind lives
	// beside it, in the object it was read out of — and name neither a loop nor a container the reader may
	// not have written (roadmap Gap R.38, L11.1).
	return fmt.Errorf("codegen: %s holds a value whose kind lives beside it rather than in it, so this position cannot ask what it is: print(%s) dispatches on the tag, and a position that keeps one word for its operand has nowhere to put the second one — this position needs a tagged value word, the one that carries the kind beside the payload (roadmap L11.1)", name, name)
}

// Where a tagged variable's (payload, tag) pair came from. The refusal a tagged name meets in a
// plain-number context has to name its own origin — a binding this statement made is not a loop
// variable, and a message that says it is has become the false claim Gap R.38 files (roadmap
// L11.1, Gap R.138).
const (
	taggedOriginArith = "arithmetic over a slot the program built at run time"
	// taggedOriginFloat is the pair a variable takes when a *later* binding gives it a double and its
	// slot is the `i32` the first binding chose: the value is boxed through `@rt_float_new` and the tag
	// says so, which is the (payload, tag) pair standing where a `store double` into an `i32` slot would
	// have overflowed into the neighbouring slot (roadmap L11.6, Gap R.155).
	taggedOriginFloat = "a double the variable's own binding boxed"
	// taggedOriginParam is the pair a *parameter* takes when a call site hands it a double: the value
	// arrived from the caller as two words, and nothing in the body built it. The two origins above both
	// blame a slot the program made at run time, which a parameter is not -- there is no container, no
	// arithmetic and no binding in the body to look for -- and a reader following that sentence ends up
	// hunting for an `xs[...]` that was never written (roadmap L11.1, Gap R.38: a refusal that claims
	// something false about the program is its own defect; ADR 0285).
	taggedOriginParam = "a number its caller handed it, which arrived as two words"
	// taggedOriginParamArith is the pair a name holds when the body computed it out of a PARAMETER
	// (`y = x + 1` / `return y`): arithmetic yes, run-time container no.
	taggedOriginParamArith = "arithmetic over a parameter the caller supplied"
	// taggedOriginSlot is the pair a plain binding took from a slot read — `n = xs[0]` over a container
	// the program built — where no arithmetic, parameter or loop is involved. The name exists because the
	// refusal it prevents was real: a program with no loop anywhere was told its value "comes from a loop
	// over a mixed list", which is Gap R.38's defect (a message that describes a program the reader cannot
	// find) reproduced by a fallback sentence rather than by a pasted template.
	taggedOriginSlot = "a slot the program built at run time, read by position"
	// taggedOriginLoop is the pair a `for`/comprehension loop variable takes over a container that mixes
	// kinds. Recorded rather than inferred: "comes from a loop" is true only when a loop bound it, and the
	// door that knows whether a loop ran is the door that should say so.
	taggedOriginLoop = "the element a loop stepped over a container that mixes kinds"
	// taggedOriginFold is the pair a `min`/`max`/`sum` answer leaves behind. A fold hands back one of the
	// values it was given, so the answer's kind is the WINNER's kind and only the run time knows which arm
	// won; the two words the name holds came out of the fold door, not out of a slot, a loop or a call site,
	// and a refusal that blamed any of those three would be Gap R.38's defect again (roadmap L11.1,
	// Gap R.146; ADR 0316).
	taggedOriginFold = "the answer of a fold the built-in chose"
)

// taggedVarErr is mixedTaggedVarErr in the honest voice: the same refusal, worded for the door that
// bound the pair. Either way the program is refused and told that print asks the tag and this context
// does not; what must never be invented is where the tag came from.
func (g *irGen) taggedVarErr(name string) error {
	switch g.taggedOrigin[name] {
	case taggedOriginArith:
		return fmt.Errorf("%s holds the answer of %s, which travels as a (payload, tag) pair: print(%s) asks the tag, and this position keeps one word for its operand, so the tag has nowhere to go (roadmap L11.1, Gap R.146)", name, taggedOriginArith, name)
	case taggedOriginParamArith:
		return fmt.Errorf("%s holds the answer of %s, which travels as a (payload, tag) pair: the kind of the answer is the kind the argument arrived as, print(%s) asks the tag, and this position keeps one word for its operand, so the tag has nowhere to go (roadmap L11.1, Gap R.146, Gap R.38)", name, taggedOriginParamArith, name)
	case taggedOriginParam:
		// The sentence names only what is true of this shape: the value came in through a call site, and
		// no slot was ever involved. An earlier draft added "another arm of this function returns a plain
		// integer", which is a story about a different program — `y = x` / `return y` has no other arm at
		// all — and a refusal that describes a program the reader cannot find is Gap R.38's own defect.
		return fmt.Errorf("%s is a parameter the call site handed a double, so it arrives as a (payload, tag) pair and its kind lives beside it, not in the value: print(%s), a binding and the positions that read one number ask the tag, and this position keeps one word for its operand, so the tag has nowhere to go — the answer's kind is what the argument was, which is a run-time question this position cannot ask (roadmap L11.1, Gap R.146, Gap R.38)", name, name)
	case taggedOriginSlot:
		return fmt.Errorf("%s holds %s, which travels as a (payload, tag) pair: the object the slot was read out of says which kind it is, print(%s) asks the tag, and this position keeps one word for its operand, so the tag has nowhere to go — this position needs a tagged value word, the one that carries the kind beside the payload (roadmap L11.1, Gap R.146)", name, taggedOriginSlot, name)
	case taggedOriginLoop:
		return fmt.Errorf("%s is %s, so it travels as a (payload, tag) pair: print(%s) asks the tag, and this position keeps one word for its operand, so the tag has nowhere to go — using it as a number needs a tagged value word, the one that carries the kind beside the payload (roadmap L11.1, Gap R.146)", name, taggedOriginLoop, name)
	case taggedOriginFold:
		return fmt.Errorf("%s holds %s, which travels as a (payload, tag) pair: a fold hands back one of the values it was given, so the answer's kind is the winner's own kind and only the run time knows which arm won, print(%s) asks the tag, and this position keeps one word for its operand, so the tag has nowhere to go (roadmap L11.1, Gap R.146)", name, taggedOriginFold, name)
	case taggedOriginFloat:
		return fmt.Errorf("%s holds %s: the variable was bound to a number and rebound to a double, so its value travels as a (payload, tag) pair whose payload is a float box, print(%s) asks the tag, and this position keeps one word for its operand (roadmap L11.6, Gap R.155)", name, taggedOriginFloat, name)
	}
	return mixedTaggedVarErr(name)
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
	case *FString:
		// The words the sentence needs are the words the program wrote: an interpolation is a TEXT here,
		// and `%T` would say `(*lang.FString)` — Go where the source belongs (Gap R.232's disease, Gap
		// R.249's rule). It is the same phrase `exprSummary` chose for the same construct, so the two
		// doors cannot describe one thing two ways (ADR 0232).
		return "an interpolated text"
	case *NoneLit:
		return "None"
	case *ListLit:
		// The words the sentence needs are the words the program wrote: the same node answers for both
		// spellings, and only its flag says which one the reader typed (roadmap L11.3, ADR 0361).
		if n.IsTuple() {
			return "a tuple literal"
		}
		return "a list literal"
	case *DictLit:
		return "a dict literal"
	case *SetLit:
		return "a set literal"
	case *Comp:
		// The same rule as the two arms above, learned from the same disease: a refusal that says
		// `*lang.Comp` tells the reader nothing, and the program wrote a brace. The kind is the
		// comprehension's own, so the sentence names which comprehension it is (roadmap **Gap R.232**,
		// **Gap R.249**, beside **Gap R.487**'s key door).
		switch n.Kind {
		case CompDict:
			return "a dict comprehension"
		case CompSet:
			return "a set comprehension"
		case CompGenerator:
			return "a generator expression"
		default:
			return "a list comprehension"
		}
	case *Tuple:
		return "a tuple literal"
	case *Lambda:
		return "a lambda"
	case *Name:
		return n.Value
	case *Call:
		// The words the sentence needs are the words the program wrote. `set()`, `list()` and `dict()`
		// name the same objects their literals spell, and `%T` would answer `*lang.Call` — Go where the
		// source belongs (Gap R.249's rule, the same one the FString arm above learned from; roadmap
		// Gap R.360). A program that defines its own `set` keeps the generic sentence, which is honest
		// about a name this door cannot resolve.
		if lit, isEmpty := emptyContainerLiteral(n); isEmpty {
			switch lit.(type) {
			case *SetLit:
				return "an empty set"
			case *DictLit:
				return "an empty dict"
			case *ListLit:
				return "an empty list"
			}
		}
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
		if p, t, ok := g.taggedLoopVarRead(b, nm.Value, nm.Span()); ok {
			return p, t, false, nil
		}
		// A name the pair road bound — `n = xs[0]` over a container the program built, or an arithmetic
		// answer — already holds both words, so a container slot can be written from them. Asking this
		// name for a payload alone is the mistake ADR 0185 forbids and Gap R.146 refuses: the word beside
		// no tag is a number wearing another object's bits (roadmap L11.1).
		if g.taggedVars[nm.Value] && g.taggedOrigin != nil {
			p, t := g.numericPairRegs(b, nm.Value, nm.Span())
			return p, t, false, nil
		}
	}
	// A call whose whole body hands back one of its own parameters answers the kind the CALLER'S argument
	// wears, and when only the objects can tell that kind the pair arrives as the two registers the pair
	// road wrote — the same arrangement ADR 0306 built for a bound name and ADR 0332 for a read. Asked
	// here, at the one door every container-writing road uses, so a comprehension's element, a list slot,
	// a dict entry and a set member cannot keep four answers to one value (roadmap **Gap R.502**).
	if p, t, isPair, perr := g.forwardedCallPair(b, e); perr == nil && isPair {
		return p, t, false, nil
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
func (g *irGen) taggedLoopVarRead(b *strings.Builder, name string, sp Span) (payload, tag string, ok bool) {
	if !g.taggedVars[name] || !g.allocd[name+"_tag"] {
		return "", "", false
	}
	// Every road that reads a tagged name's pair comes through here, so this is the one place the
	// written-flag can be tested for all of them. It used to be tested only on the ordinary slot read,
	// which meant a name whose only writer was a `match` capture printed its payload and its tag from
	// the two allocas on a path where no arm had bound them — `print(got)` answered `0` at exit 0 where
	// the reference raises NameError, because the print dispatch took the tagged arm and never asked
	// whether the slot had been written (roadmap Gap R.68, ADR 0358; the flag itself is ADR 0228's).
	g.checkBound(b, name, sp)
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
	if !compIsSingleClause(c) {
		return false // the same reason as compElemPrintsAsText: one clause's question, not a clause list's
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
//
// The question is asked through the alias resolver: `ys = xs` shares the handle and writes no claim, so
// the record that says "these slots speak" is `xs`'s, while the handle the read loads is still `ys`'s —
// which is why the lookup returns the name it was handed and consults the other one (roadmap L11.1,
// Gap R.415, ADR 0412; ADR 0232's rule that the object's slots are the record).
func (g *irGen) mixedIndexRead(ix *Index) (string, bool) {
	if ix == nil {
		return "", false
	}
	nm, ok := ix.Obj.(*Name)
	if !ok || !g.mixedListRead(nm.Value) {
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
	case *FString:
		// A refusal that names its receiver answers "which door?"; one that names nothing sends the
		// reader looking for a bug the program does not have. An interpolation is a text the compiler
		// builds at run time, and that is what a sentence about it should say (roadmap ADR 0249's rule
		// for a refusal sentence, ADR 0359's new reach; Gap R.232 measured the same disease)
		return "an interpolated text"
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
	case *Comp:
		// `%T` would answer `*lang.Comp` where the source wrote a brace (**Gap R.249**'s rule; roadmap
		// **Gap R.487**). The kind is the comprehension's own.
		switch n.Kind {
		case CompDict:
			return "a dict comprehension"
		case CompSet:
			return "a set comprehension"
		case CompGenerator:
			return "a generator expression"
		default:
			return "a list comprehension"
		}
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
// came out of the string table as `(null)` where the record prints 7. The element is then not
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
// number printed through the string table is `(null)` where the record prints 7 (ADR 0232).
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
		g.checkBound(b, nm.Value, nm.Span()) // whose pair's tag this is, the flag says (Gap R.68)
		t := g.newTmp()
		fmt.Fprintf(b, "  %s = load i32, i32* %%_%s_tag\n", t, nm.Value)
		return t
	}
	if interned {
		return strconv.FormatInt(int64(TagStr), 10)
	}
	return strconv.FormatInt(int64(TagInt), 10)
}

// mutationSlotWords is the question a MUTATION road asks of the word it is about to store: an operand the
// pair road bound hands the container its payload AND its tag as two registers, and anything else keeps the
// one word and the tag this pass read off its spelling. It is `taggedSlotWords` for the statement roads —
// `xs.append(n)`, `s.add(n)`, `xs[i] = v`, `d[k] = v` — which ask the question of a value they change rather
// than a value they build (roadmap L11.1, Gap R.146; ADR 0310's dict/set entry, ADR 0306's element).
//
// The tag comes back empty for a non-pair on purpose: the roads keep consulting `elemKindTag` for a literal,
// so a later cycle that widens one of these doors cannot silently stop asking the question the container's
// kind record depends on.
func (g *irGen) mutationSlotWords(b *strings.Builder, e Expr) (payload, tag string, fromPair, interned bool, err error) {
	if p, t, ok := g.pairElemPair(b, e); ok {
		return p, t, true, false, nil
	}
	v, isStr, err := g.heapElemKind(b, e)
	if err != nil {
		return "", "", false, false, err
	}
	return v, "", false, isStr, nil
}

// promotePairMixed is promoteMixed's bookkeeping for an element whose kind lives in a register instead of a
// spelling. It cannot call promoteMixed, which answers false unless `elemKindTag` can label the element — the
// question a pair-bound name cannot answer, which is the whole reason the pair road exists — so the container
// is told the same thing by other means: it stops claiming one kind, and it lets its slots speak. The float
// printer is switched on because the tag, and not this pass, now decides per slot whether an arm prints a
// box's double (ADR 0232's promotion, ADR 0244's rule for a list the compiler cannot see into).
func (g *irGen) promotePairMixed(b *strings.Builder, handle, name, kind string) bool {
	if handle == "" || name == "" {
		return false
	}
	switch kind {
	case "list":
		g.markListMixed(name)
	case "set":
		g.markSetMixed(name)
	case "dict key", "dict value":
		g.markDictMixed(name)
	default:
		return false
	}
	g.heapUsed = true
	g.floatFmtUsed = true
	fmt.Fprintf(b, "  call void @rt_mark_estr(i32 %s, i32 8)\n", handle)
	return true
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
		g.markListMixed(name)
	case "set":
		g.markSetMixed(name)
	case "dict key", "dict value":
		g.markDictMixed(name)
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
	// Which keys the door can be asked with is its own question, asked of its own file — the one
	// that keeps the key kinds with tags beside the key kinds without, so the print door, the binding
	// door and the `and`/`or` doors cannot each grow a different list (roadmap Gap R.221, ADR 0353).
	if dictKeyCarriesPair(ix.Idx) {
		return nm.Value, true
	}
	return "", false
}

// mixedKindErr is the diagnostic every mixed-container site reports.
func mixedKindErr(what string) error {
	return fmt.Errorf("codegen: a compiled %s holds either strings or numbers, not both; CPython allows mixing — a compiled container records one element kind, so heterogeneous contents need per-element tagging (roadmap Gap J.6)", what)
}

// recordElemKind notes that a container variable holds strings (isStr) or numbers in a given
// position, and refuses the case where it has already been told the opposite. The compiled
// container records one element kind per position, so mixing would print an integer through the
// string table — an honest diagnostic beats that (roadmap Gap J.6, ADR 0166).
func (g *irGen) recordElemKind(name, slot string, isStr bool, e Expr) error {
	if e != nil && !g.containerKindProvable(e) {
		return fmt.Errorf("codegen: cannot put %s in a compiled %s: the compiler cannot prove one kind for it, because the function it calls hands back text on one path and a number on another. print asks that question when it prints and gets it right; a container slot is labelled once, and labelling it either way misprints the other (CPython answers this program; per-element asking is the tagged value word, roadmap L11.1, ADR 0232)", g.exprSummary(e), containerSlotLabel(slot))
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
	case *Slice:
		// A SLICE of a container is a container: `[1, 2, 3][1:]` lowers to `rt_slice`, which hands
		// back a heap handle, and printf's `%d` printed that handle as `1` where the reference prints
		// `[2, 3]` (Gap R.179). A text slice is answered by the text road beside this, so ask the base.
		if lit, ok := g.stringVal(n.Obj); ok && lit != "" {
			return false
		}
		return g.isContainerExpr(n.Obj)
	case *Index:
		// An element that holds another container is a container: the slot stores its handle, and
		// the tag the builder wrote says so (ADR 0239). Reading it is the question this answers —
		// `for row in xs[0]`, `xs[0] == [1, 2]`, `2 in xs[0]` — and the answer is only yes when the
		// tag is visible, which is the same promise containerHandleOf asks for before it emits a
		// load (roadmap L11.1, ADR 0241).
		_, ok := g.staticContainerKind(n)
		return ok
	case *Call:
		// A call that hands back a container IS a container: its answer is the handle the callee's builders
		// made (Gap R.67), and the kind was proved by the scan of the callee's whole body rather than guessed
		// from this call site. Every road below this question that then asks `containerHandleOf` gets the same
		// answer to both — the comparison, the membership test, the loop, the truthiness test — which is the
		// reason the two are kept as a pair (roadmap Gap R.67, ADR 0333).
		// …and so is the empty container the built-in constructors spell: `list() == []` refused while this
		// arm knew only a callee the scan names, and the tail below — which does know the spelling — was
		// never reached (roadmap Gap R.364, ADR 0333's decide-and-build pair).
		if _, isEmpty := g.emptyContainerCall(n); isEmpty {
			return true
		}
		if nm, isName := n.Fn.(*Name); isName {
			_, ok := g.containerAnswerOf(nm.Value)
			return ok && g.containerAnswerCallIsCarriable(n)
		}
		// The METHOD round (**Gap R.453**, **ADR 0430**): a body in a class answers the same object a `def`
		// answers, and the two doors that ask this question before they build must give the same answer for
		// both spellings (**ADR 0232**'s one question). One kind, proved by the scan of the whole body.
		if _, ok := g.methodContainerAnswerOf(n); ok {
			return true
		}
		return false
	case *CondExpr:
		// A ternary IS a container when both of its arms are: the test chooses between two objects, and
		// whichever one was built is the answer. A test the source wrote is not a run-time question — the arm
		// that runs is the answer, and the arm it rejected is not in the program (roadmap Gap R.340,
		// ADR 0374, read by the container roads for Gap R.385). Asking the pair together rather than each arm
		// separately is what keeps the two doors that ask — this one, which may build, and the print door,
		// which chooses a printer — answering one question (ADR 0232).
		if taken, decided := constantTestArm(n); decided {
			return g.isContainerArm(taken)
		}
		return g.isContainerArm(n.If) && g.isContainerArm(n.Else)
	case *BinOp:
		// `[1] + [2]`, `[1] * 3` and `(1,) * 2` are containers the same way a literal is: the door builds a
		// fresh heap object whose slots are the operands' payloads with their tags. The one question is asked
		// through `seqBuildsContainer` so the printer, the membership test, the comparison and the loop cannot
		// disagree with the builder about which expressions those are (roadmap Gap R.33, ADR 0365). A text
		// repeated by a number answers no — its answer is an @str_tab index, which every text road takes.
		_, applies := g.seqBuildsContainer(n)
		return applies
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
	case *Call:
		// `set()`, `list()` and `dict()` name a container as surely as a literal does, and the door that
		// DECIDES says what the door that BUILDS builds. ADR 0391 gave the slot its tag through this same
		// shape; `len(set())` still refused and `abs(list())` still answered `0` because these two doors ask
		// the spelling and were not told (roadmap Gap R.364).
		if lit, isEmpty := g.emptyContainerCall(n); isEmpty {
			switch lit.(type) {
			case *ListLit:
				return "list", true
			case *DictLit:
				return "dict", true
			case *SetLit:
				return "set", true
			}
		}
		// The answer of a function that answers a container is a container, and the table says which —
		// the same pair of questions the roads beside this one ask of a name or a slot, asked here of a
		// call: decide from the table, build with `containerHandleOf`, which answers the same shape
		// (roadmap Gap R.67, ADR 0333).
		if nm, isName := n.Fn.(*Name); isName {
			if a, ok := g.containerAnswerOf(nm.Value); ok && g.containerAnswerCallIsCarriable(n) {
				return a.kind.String(), true
			}
		}
		return "", false
	case *Slice:
		// A slice of a list IS a list: `rt_slice` builds a fresh object out of the base's slots and their
		// tags, so the kind question about the copy is the kind question about the base. A dict or a set is
		// not sliced — the reference raises for both — so those keep the road they have today (Gap R.217).
		if kind, ok := g.staticContainerKind(n.Obj); ok && kind == "list" {
			return "list", true
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
	case *BinOp:
		// The concatenation of two sequences and the repeat of one are containers too, and the kind is
		// answered by the same door that builds them, so a road that decides here and a road that builds
		// later ask one question (roadmap Gap R.33, ADR 0365).
		if kind, applies := g.seqBuildsContainer(n); applies {
			return kind, true
		}
	case *CondExpr:
		// The kind of a ternary is the kind its arms AGREE on, which is the only answer the print door can
		// choose a printer from. A test the source wrote is not a question: the arm that runs is the answer,
		// and its kind is the ternary's (roadmap Gap R.385, ADR 0374's rule about the rejected arm).
		if taken, decided := constantTestArm(n); decided {
			return g.staticContainerArmKind(taken)
		}
		thenKind, okThen := g.staticContainerArmKind(n.If)
		elseKind, okElse := g.staticContainerArmKind(n.Else)
		if okThen && okElse && thenKind == elseKind {
			return thenKind, true
		}
		return "", false
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
	case *BinOp:
		// A sequence the door builds is a list (in the immutable spelling or not), and the answer to both
		// questions — decide, then build — comes from the one door (roadmap Gap R.33, ADR 0365).
		if _, applies := g.seqBuildsContainer(n); !applies {
			return "", "", false
		}
		hh, _, serr := g.seqHandleOf(b, e)
		if serr != nil {
			return "", "", false
		}
		return hh, "list", true
	case *Slice:
		// A slice of a list IS a list: the object is the fresh heap list `rt_slice` built out of the base's
		// slots and their tags, so `xs[0:1][0]` reads a slot of it exactly as it reads one of a variable's
		// list. `containerOperand` lowers the copy and hands back the handle (roadmap Gap R.230, ADR 0335).
		kindOfBase, okKind := g.staticContainerKind(n.Obj)
		if !okKind || kindOfBase != "list" {
			return "", "", false
		}
		hh, err := g.containerOperand(b, e)
		if err != nil {
			return "", "", false
		}
		return hh, "list", true
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
	case *Call:
		// A call that hands back a container named it as surely as a variable does: the callee's `ret`
		// carries the handle its builders made (Gap R.67), so the doors that read a container — the
		// comparison, the membership test, the loop, the element road — read this answer too. The scan, not
		// the call site, is what proves the answer is a container; a call the scan cannot name declines, and
		// the caller's own refusal stands.
		if lit, isEmpty := g.emptyContainerCall(n); isEmpty {
			// The build half of the answer above: the empty container spelled as a call is the object the
			// literal spells, built by the same road (roadmap Gap R.364, ADR 0333's decide-then-build pair).
			kindOf := ""
			switch lit.(type) {
			case *ListLit:
				kindOf = "list"
			case *DictLit:
				kindOf = "dict"
			case *SetLit:
				kindOf = "set"
			}
			g.heapUsed = true
			h, err := g.containerOperand(b, lit)
			if err != nil {
				return "", "", false
			}
			return h, kindOf, true
		}
		if nm, isName := n.Fn.(*Name); isName {
			if a, okA := g.containerAnswerOf(nm.Value); okA && g.containerAnswerCallIsCarriable(n) {
				g.heapUsed = true
				g.containerAsked[nm.Value] = true
				h, err := g.value(b, e)
				delete(g.containerAsked, nm.Value)
				if err != nil {
					return "", "", false
				}
				return h, a.kind.String(), true
			}
		}
		// The METHOD round (**Gap R.453**, **ADR 0430**): a body in a class that answers one container builds
		// the same object inside its own frame, and the pair the carried method stores beside its `ret` names
		// it. The scan, exactly as for a `def`, is what proves the answer is a container; a method the scan
		// cannot name declines and the caller's own refusal stands.
		if ans, okAns := g.methodContainerAnswerOf(n); okAns && g.containerAnswerCallIsCarriable(n) {
			h, _, okPair, perr := g.pairCallPair(b, e)
			if perr != nil || !okPair {
				return "", "", false
			}
			g.heapUsed = true
			return h, ans.kind.String(), true
		}
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
	if c, isCall := e.(*Call); isCall {
		// The answer of a call that carries its own kind is a run-time fact, and this door has no word
		// for one yet: it would compare the payload — an @str_tab index for a text arm — against the other
		// side's number and answer by coincidence, which is exactly the ADR 0232 collision the refusal
		// below exists to prevent. Declining leaves the answer to `taggedCompareOperandErr`, so the
		// program is refused in words rather than judged by whichever number sits in the slot
		// (roadmap Gap R.83, Gap R.219, ADR 0368's open half in Gap R.324).
		if nm, isName := c.Fn.(*Name); isName && g.answer.carries(nm.Value) {
			return "", "", false
		}
		// The popped slot's pair, from the door the print, the binding and the ordering already ask.
		if p, t, okPop, _ := g.popAnswerPair(b, e); okPop {
			return p, t, true
		}
	}
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
	// The item of an unrolled `for` over a literal list is a third shape of the same thing: its kind is
	// known to this pass and to no notebook the compare road reads, because the unroll records it per
	// iteration (`loopElemTag`, **Gap R.358**/**ADR 0388**) instead of binding a `_tag` slot. Asked here, and
	// the pair comes out of `taggedOperand`'s new arm — the door the printer and the truth road already ask,
	// so one item cannot be the void to `print(v)` and a number to `v == None` (roadmap **Gap R.416**,
	// **ADR 0232**'s one question). An int or interned-text item answers false here, as it does everywhere
	// else, and keeps the source-side fold it has always had.
	if _, selfDescribing := g.loopItemKindTag(e); selfDescribing {
		return true
	}
	if c, isCall := e.(*Call); isCall {
		// A popped slot carries its kind the way a slot read does — the removal read it out of the object —
		// so the equality door is its door too (roadmap L11.1, ADR 0410, ADR 0247's one equality).
		if _, _, serves := g.popAnswerShape(c); serves {
			return true
		}
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
	{
		arm, chk := g.newLabel("len.inst"), g.newLabel("len.chk")
		c := g.newTmp()
		fmt.Fprintf(b, "  %s = icmp eq i32 %s, %d\n", c, tag, int32(TagInstance))
		g.markI1(c)
		fmt.Fprintf(b, "  br i1 %s, label %%%s, label %%%s\n", c, arm, chk)
		fmt.Fprintf(b, "%s:\n", arm)
		g.raiseInstanceTo(b, payload, instanceMsgLen, sp)
		fmt.Fprintf(b, "%s:\n", chk)
	}
	// What is left is text, a container, or a kind this language has not grown yet (tuple).
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
	// A sequence this backend BUILDS — `[1] + [2]`, `[1] * 3` — is neither a name nor a literal, and the
	// generic sentence below sends the reader looking for a literal the program never wrote. The object
	// exists, its slots are walkable, and what is missing is the door's own (payload, tag) pair coming back
	// out of the road that built it (roadmap Gap R.314, ADR 0365).
	if _, builds := g.seqBuildsContainer(ix.Obj); builds {
		return fmt.Errorf("%s cannot reach into the sequence this expression builds: the object is there and its slots are walkable, but the door that built it hands back a handle and the slot read wants the payload with its tag — the arm is owed, not the shape (roadmap Gap R.314, ADR 0365)", ask)
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
	g.settleContainerKind(name, containerKindScalar)
	// The name now holds a pair, not the container it may have been handed by `ys = xs`: its link to that
	// object dies with the rest of its old facts, or a later read of it would ask a record that describes
	// something else (roadmap Gap R.415, ADR 0412).
	g.forgetContainerAlias(name)
	delete(g.strVals, name)
	delete(g.internedVars, name)
	delete(g.noneVars, name)
	if g.floatVars != nil {
		delete(g.floatVars, name)
	}
	// The pair road's payload is an `i32` (a box handle for a float), so the record of a `double`
	// allocation must go with the float status: leaving it would let a later double binding store eight
	// bytes into the slot this door took over (roadmap L11.6, Gap R.155, ADR 0274).
	delete(g.doubleSlot, name)
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

// forgetTaggedBinding retires a name's tag: whatever binds the name next is not a pair, so the
// value slot means what that statement says it means and nothing else. ADR 0172 states the rule for
// the None and bool statuses — the variable's *latest* assignment decides how print, truthiness and
// equality lower, and any other assignment clears the status — and ADR 0185/ADR 0187 carried it to
// the tagged variable. What neither did was clear it at the container, comprehension, lambda, module
// and float bindings, all of which return early with the record still in place: `n = xs[0][0] * 2`
// followed by `n = [1, 2]` printed `2` — the heap handle — because the print dispatch was still
// asking the tag the arithmetic had left behind, and `n = 2.5` printed `0` for the same reason
// (roadmap Gap R.142, measured landing the pair road that makes the shape reachable).
func (g *irGen) forgetTaggedBinding(name string) {
	delete(g.taggedVars, name)
	delete(g.taggedOrigin, name)
}

// retireVarStatuses is ADR 0172's latest-binding rule for the records that are not the tag and not the
// container kind — the interned text and the class an instance came from. The rule has always been stated
// ("the variable's latest assignment decides how print lowers, and any other assignment clears the
// status"); what was missing is one place that applies it to *every* status. `boolVars` and `noneVars` each
// have their own forget helper and their own call, and the interned-text pair has none: `x = "text"` then
// `x = [1, 2]` left `internedVars[x]` standing, and the compiled `print(x)` answered `text` at exit 0 while
// CPython and the record answered `[1, 2]` (roadmap Gap R.145, measured again while landing the signless
// doors of ADR 0271, which read the same records and would have raised on `x = "text"` / `x = 5` / `abs(x)`).
//
// It clears only what the new binding contradicts, and it is called before the binding records its own
// kinds, so a program that rebinds a name to the same family keeps the record it needs.
func (g *irGen) retireVarStatuses(name string, rhs Expr) {
	if name == "" || rhs == nil {
		return
	}
	if !g.bindingIsText(rhs) {
		delete(g.strVals, name)
		delete(g.internedVars, name)
	}
	if _, isCall := rhs.(*Call); !isCall {
		// Only a construction can bind an instance; anything else retires the class the print
		// dispatch and the method call would otherwise keep reading.
		delete(g.varClasses, name)
	}
}

// bindingIsText asks whether this right-hand side leaves the name holding an @str_tab index — the same
// question the print dispatch asks, asked of the expression that is about to be stored (ADR 0229's one
// predicate, read at the binding).
func (g *irGen) bindingIsText(e Expr) bool {
	if _, ok := g.stringVal(e); ok {
		return true
	}
	if g.exprIsString(e) || g.printsAsInternedStr(e) {
		return true
	}
	if c, ok := e.(*Call); ok {
		return g.callReturnsStr(c)
	}
	return false
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
	if dl, isLit := ix.Obj.(*DictLit); isLit {
		// The bottom of the chain is a literal the fold could not read: build the dict the way the binding
		// road builds it and read the entry the key names, tags and all (roadmap Gap R.215, ADR 0327). The
		// fold's own shape stays the fold's answer — the gate above admits every literal dict, and the door
		// declines the ones the compile-time lookup reads, so the two can only disagree in the safe direction.
		if g.dictLiteralFoldAnswers(dl, ix.Idx) {
			return "", "", false
		}
		h, kind, okHandle := g.containerHandleOf(b, ix.Obj)
		if !okHandle {
			return "", "", false
		}
		return g.containerSlotRead(b, h, kind, ix.Idx, ix.Span())
	}
	if ll, isLit := ix.Obj.(*ListLit); isLit {
		// The bottom of the chain is a literal the fold could not read: build the list the way the binding
		// road builds it — each element's kind written beside its payload — and read the position the
		// program named, tags and all. This is ADR 0327's dict arm one container over: a list literal is a
		// list the compiler happens to be looking at, and the element whose kind lives in the tag (a text's
		// @str_tab index, a float's @float_box handle, None, a bool) has always been answered correctly for
		// the same element read off a variable (roadmap Gap R.218, ADR 0331). The fold's own shape stays the
		// fold's answer; the door declines it, so the two can only disagree in the safe direction.
		if g.listLiteralFoldAnswers(ll, ix.Idx) {
			return "", "", false
		}
		h, kind, okHandle := g.containerHandleOf(b, ix.Obj)
		if !okHandle {
			return "", "", false
		}
		return g.containerSlotRead(b, h, kind, ix.Idx, ix.Span())
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
	if _, isCall := ix.Obj.(*Call); isCall {
		// The bottom of the chain is a call that hands back a container. `containerHandleOf` lowers it to
		// the handle the callee's builders made and names the kind the scan recorded, and the read below is
		// the one the variable road uses — so `build()[i]` and `ys = build(); ys[i]` cannot disagree about
		// what slot i holds, which is the parity this row existed to buy (roadmap Gap R.214, ADR 0334).
		h, kind, okHandle := g.containerHandleOf(b, ix.Obj)
		if !okHandle {
			return "", "", false
		}
		return g.containerSlotRead(b, h, kind, ix.Idx, ix.Span())
	}
	if _, isSlice := ix.Obj.(*Slice); isSlice {
		// The bottom is a slice: the object is the fresh list `rt_slice` built, its slots are the base's
		// payloads with their tags, and the read below is the same one a variable's list gets — which is what
		// lets `xs[0:1][0]` and `ys = xs[0:1]; ys[0]` be one program (roadmap Gap R.230, ADR 0335).
		h, kind, okHandle := g.containerHandleOf(b, ix.Obj)
		if !okHandle {
			return "", "", false
		}
		return g.containerSlotRead(b, h, kind, ix.Idx, ix.Span())
	}
	if c, isComp := ix.Obj.(*Comp); isComp {
		// TWO comprehensions, one door. A comprehension the FOLD answered remains the fold's answer; this arm
		// is for the comprehension the RUN TIME BUILT, which has no global and no count table and therefore must
		// be read from the object (**ADR 0442**'s rule, asked here at **Gap R.501**'s subscript).
		if !g.compTakesTheRuntimeRoad(c) {
			return "", "", false
		}
		// The bottom of the chain is a COMPREHENSION the builder filled. `containerHandleOf` hands back the
		// register that builder made (**ADR 0439**), the comprehension's OWN brace names the kind
		// (**ADR 0443**'s `compContainerKind`, because that door answers `""` for a slot read where only the tag
		// knows), and `rt_get_elem`/`rt_tag_of` ask the object. Before this arm neither altitude answered:
		// `print([g(x) for x in [1, 2, 3]][1])` died with `IndexError: list index out of range` at exit **3**
		// for the reference's `4` — the payload of a slot the fold's global never filled, checked against a
		// count that was not there (**roadmap Gap R.501**; **ADR 0442**'s count door and **ADR 0334**'s
		// call-result arm are the neighbours this copies).
		h, kind, okHandle := g.containerHandleOf(b, ix.Obj)
		if !okHandle {
			return "", "", false
		}
		if kind == "" {
			k, isK := compContainerKind(c)
			if !isK {
				return "", "", false
			}
			kind = k
		}
		// A set has no position to read: the reference raises `TypeError: 'set' object is not subscriptable`.
		// The pair road therefore declines the shape and leaves it to the subscript door, which emits that
		// raise; letting `containerSlotRead` answer the documented gusty set-membership extension here would
		// turn a reference raise into an answer the program never asked for (**roadmap Gap R.501**,
		// **ADR 0328**'s separation of the set's own question from a list or dict slot).
		if kind == "set" {
			return "", "", false
		}
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
	case *ListLit:
		// A list literal is a container the compiler is looking at, not one it holds: the elements the fold
		// cannot read — a text, a float, None, a bool — have their kinds written beside their payloads by the
		// same builder a list *variable* uses, so the pair door that serves the variable serves the literal
		// too. Where the fold answers, the door below declines and the fold stays in charge (roadmap
		// Gap R.218, ADR 0331 — ADR 0327's decision, taken for the other container).
		return len(n.Elems) > 0
	case *DictLit:
		// A dict literal is a container the compiler is looking at, not a container the compiler holds:
		// the entries the fold cannot read — a text key, a float value, a nested container — have their
		// kinds written beside their payloads by the same builder a dict *variable* uses, so the pair door
		// that serves the variable serves the literal too. Where the fold answers, the door below declines
		// and the fold stays in charge (roadmap Gap R.215, ADR 0327).
		return len(n.Keys) > 0
	case *Name:
		// A name the escape analysis kept as a compile-time value has no slot to load; emitting the
		// load anyway is the module llc rejects (ADR 0192).
		if !g.allocd[n.Value] {
			return false
		}
		return g.listVars[n.Value] || g.mixedLists[n.Value] || g.runtimeDicts[n.Value] || g.mixedDicts[n.Value]
	case *Index:
		return g.runtimeSlotReadOf(n)
	case *Slice:
		// The haystack may be a run of slots rather than a whole container: `2 in xs[0:2]` asks the sliced
		// object, which is a list the run time built and tagged, exactly like one a variable holds
		// (roadmap Gap R.230).
		return g.isContainerExpr(n.Obj)
	case *Call:
		// A call that hands back a container is a side the run time owns: the callee materialised the
		// object at its `ret`, so the slots carry the tags its builders wrote and the door below can read
		// them. The scan — not the call site — is what proves it, and a call the scan cannot name keeps the
		// refusal it always had (roadmap Gap R.214, ADR 0334; the return half is ADR 0333).
		nm, isName := n.Fn.(*Name)
		if !isName {
			return false
		}
		_, ok := g.containerAnswerOf(nm.Value)
		return ok && g.containerAnswerCallIsCarriable(n)
	case *Comp:
		// The RUN TIME's comprehension owns the object: its elements are slots of the heap object a builder
		// filled (**ADR 0439**), and its length is a word inside that object rather than a count the fold
		// recorded (**ADR 0442**). The fold's comprehension is not this gate's object — it keeps the global
		// answer and the fold's own doors (**ADR 0192**), so widening the pair road here would move the fold
		// onto a runtime read this row does not owe (roadmap **Gap R.501**). The door below asks the same
		// predicate again (**ADR 0249**'s gate/door rule), so the two can only disagree in the safe direction.
		return g.compTakesTheRuntimeRoad(n)
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
		g.raiseTo(b, exnCode("TypeError"), "TypeError", notSubscriptableSentence(t.name), sp)
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
	// The key expression rides along so the raise can name the key (Gap R.189): the payload-and-tag pair
	// says which entry was asked for, and the program's own key expression says what to call it in the
	// message. `ix.Idx`, not `key` — `key` here is the IR value the payload arrived in.
	g.checkKeyReadTagged(b, base, key, keyTag, ix.Idx, sp)
	dv := g.newTmp()
	fmt.Fprintf(b, "  %s = call i32 @rt_dict_get_tagged(i32 %s, i32 %s, i32 %s)\n", dv, base, key, keyTag)
	dt := g.newTmp()
	fmt.Fprintf(b, "  %s = call i32 @rt_dict_value_tag(i32 %s, i32 %s, i32 %s)\n", dt, base, key, keyTag)
	fmt.Fprintf(b, "  br label %%%s\n", tailD)

	// ---- the text arm: a code point, interned like every other one-character string, or the
	// IndexError that says there is no such position.
	fmt.Fprintf(b, "%s:\n", strArm)
	sv := g.rtStrCall(b, "rt_str_char", "i32 "+base, "i32 "+key)
	g.checkStrSentinels(b, sv, "IndexError", stringIndexOutOfRange(), sp, "slotchr")
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

// builtSlotPair reads the slot an expression denotes and hands back its (payload, tag) pair *without*
// claiming anything about what the slot holds. It is the shape `xs[0]` and `d["a"]` take when the
// container is one the program assembled with `append` and assignment rather than wrote as a literal:
// no table says what the slot is, and the tag the writer left beside the payload is the only witness
// (roadmap L11.1, ADR 0251).
//
// It declines for the shape ADR 0241 already serves — a slot the literal still describes — so the two
// roads never compete: where the compiler can read the tag it names the object directly, and where it
// cannot, every caller below asks the object instead of a table.
func (g *irGen) builtSlotPair(b *strings.Builder, ix *Index) (payload, tag string, ok bool) {
	if _, seen := g.staticSlotTag(ix.Obj, ix.Idx); seen {
		return "", "", false
	}
	return g.slotPairFromTheObject(b, ix)
}

// slotPairFromTheObject is `builtSlotPair` with the static door taken out of the way: it reads the slot
// and hands back the (payload, tag) pair the WRITER left beside the payload, whatever the compiler's own
// notebooks say the slot is.
func (g *irGen) slotPairFromTheObject(b *strings.Builder, ix *Index) (payload, tag string, ok bool) {
	base, baseKind, okBase := g.containerHandleOf(b, ix.Obj)
	if okBase {
		return g.containerSlotRead(b, base, baseKind, ix.Idx, ix.Span())
	}
	// The side the slot is read out of is not a container the notebook describes — it is itself a slot.
	// ADR 0251 reads such a chain one level at a time, dispatching each inner read on the tag the outer
	// one reported, and the pair that comes back off the end is exactly what the arms below want: they
	// never asked how deep the chain was, only what the object at its end is (roadmap L11.1, Gap R.210).
	return g.runtimeSlotPairDeep(b, ix)
}

// slotPairWhereStaticLabelCannotWalk is the widening **ADR 0419** had to make before it could open the
// static key door at all (roadmap Gap R.285). Two roads — the membership test and the `for` loop — ask
// an entry's KIND in order to decide what question to ask the entry at all: walk its slots, its keys,
// its members, or test a substring. Where the compiler's notebooks describe the slot as a container,
// the static answer is sound and those roads keep using it. Where the slot holds a text, a number, a
// verdict or the void, the static label is a kind NEITHER road can walk, and reading the payload as a
// heap handle is how `for x in d[None]` came to print nothing at the exit code of success and
// `1 in d[None]` came to answer False where the reference raises `argument of type 'int' is not
// iterable`. Those two roads now ask the object, whose tag says which arm runs and raises the sentence
// the reference raises for a kind with no elements (ADR 0323's rule for a slot, applied one step wider:
// to a slot the COMPILER can describe and the road still cannot walk).
func (g *irGen) slotPairWhereStaticLabelCannotWalk(b *strings.Builder, ix *Index) (payload, tag string, ok bool) {
	tg, seen := g.staticSlotTag(ix.Obj, ix.Idx)
	if !seen || g.containerTag(tg) {
		// Either only the object knows (and `builtSlotPair` already answered), or the static label is a
		// container these roads know how to walk and the static road owns the question.
		return "", "", false
	}
	return g.slotPairFromTheObject(b, ix)
}

// The five sentences an object's own class must name. They are not one template with the word
// substituted at the call site: CPython keeps membership and iteration apart, names the needle rather
// than the haystack for `in <string>`, and quotes a key/member kind but not the operand of
// `requires string`. Each arm chooses one; the runtime formats it from the class id the object carries.
const (
	instanceMsgMembership = iota
	instanceMsgLoop
	instanceMsgLen
	instanceMsgStrNeedle
	instanceMsgUnhashable
)

// raiseInstanceTo is ADR 0228's contract for a failure whose subject is a heap object the compiler can
// name only by asking it. The static raise roads store a compile-time string; an instance's class is in
// the object, so the helper formats the message into the module buffer and the store below replaces the
// placeholder setExn had to write. The exception code, span and landing pad stay where every other
// emitted raise puts them — in the arm that knows the source position.
func (g *irGen) raiseInstanceTo(b *strings.Builder, payload string, kind int, sp Span) {
	g.heapUsed = true
	msg := g.newTmp()
	fmt.Fprintf(b, "  %s = call i8* @rt_instance_msg_ptr(i32 %s, i32 %d)\n", msg, payload, kind)
	g.setExn(b, exnCode("TypeError"), "TypeError", "instance", sp)
	fmt.Fprintf(b, "  store i8* %s, i8** @exn_msg\n", msg)
	if len(g.handlerStack) > 0 {
		b.WriteString("  br label %" + g.handlerStack[len(g.handlerStack)-1] + "\n")
	} else {
		b.WriteString("  br label %" + g.funcRaiseExit + "\n")
	}
}

// notIterableSentence is the sentence the reference writes for the two questions a value with no
// iteration is asked: membership names the type (`argument of type 'int' is not iterable`) and a loop
// names the object (`'int' object is not iterable`). They are not variations of one another, and one
// door writing the other's wording is the mistake ADR 0271 recorded for abs and the unary minus.
func notIterableSentence(kind string) string {
	return "argument of type '" + kind + "' is not iterable"
}

// notIterableLoopSentence is the loop's half of that pair.
func notIterableLoopSentence(kind string) string {
	return "'" + kind + "' object is not iterable"
}

// inStrNeedleSentence is the reference's third membership sentence, and the only one that names the
// needle rather than the haystack: asking a text whether it contains a number is not a question it
// answers with False.
func inStrNeedleSentence(kind string) string {
	return "'in <string>' requires string as left operand, not " + kind
}

// unhashableSentence is what the reference says when the thing being looked up cannot be a key or a
// member. It names the needle's kind, and it is a raise: an answer of "not in" for a program the
// reference stops is the same lost error class ADR 0311 refused at the write site.
func unhashableSentence(kind string) string {
	return "unhashable type: '" + kind + "'"
}

// guardHashableNeedle emits, in the arm that is about to look a needle up in a dict or hash it into a
// set, one test on the needle's tag per container kind that cannot be a key or a member, each leaving
// through the program's own `except TypeError:`. It is the read half of the rule ADR 0311 enforced at
// the write site, and it is keyed by the tag rather than by the compiler's proven kind because the
// needle may be a slot the program filled. `everyContainer` is false for a set haystack, where the
// reference converts a set needle to a frozenset and answers instead of raising (roadmap L11.1,
// Gap R.95's needle half).
func (g *irGen) guardHashableNeedle(b *strings.Builder, needleTag string, sp Span, everyContainer bool) {
	kinds := []struct {
		tg   int32
		name string
	}{{int32(TagList), "list"}, {int32(TagDict), "dict"}}
	if everyContainer {
		kinds = append(kinds, struct {
			tg   int32
			name string
		}{int32(TagSet), "set"})
	}
	for _, t := range kinds {
		bad, chk := g.newLabel("in.unhash"), g.newLabel("in.hashok")
		c := g.newTmp()
		fmt.Fprintf(b, "  %s = icmp eq i32 %s, %d\n", c, needleTag, t.tg)
		g.markI1(c)
		fmt.Fprintf(b, "  br i1 %s, label %%%s, label %%%s\n", c, bad, chk)
		fmt.Fprintf(b, "%s:\n", bad)
		g.raiseTo(b, exnCode("TypeError"), "TypeError", unhashableSentence(t.name), sp)
		fmt.Fprintf(b, "%s:\n", chk)
	}
}

// membershipOfBuiltSlotHaystack answers `7 in xs[0]`, `"k" in d["a"]`, `1 in s[0]` where the haystack
// is a slot the program built one element at a time — the test that today picks its helper by the kind
// the compiler had written down, cannot see one, and refuses (roadmap L11.1, Gap R.95).
//
// ADR 0251 gave the *read* below such a slot exactly this shape: one branch per kind, every arm asking
// the object a question the object can answer, and the arms that can only fail leaving through the
// program's own `except TypeError:`. The haystack is that same read; what it asks — walk slots, walk
// keys, walk members, or test a substring at all — is decided by the tag the writer left beside the
// payload, so a list slot and a dict slot and a text slot answer in one program, and a number slot
// raises the sentence the reference raises instead of being read as a handle and answering False at
// the exit code of success.
//
// The needle keeps ADR 0232's rule: its kind has to be provable, because an untagged compare of an
// @str_tab index with the integer it is indexed by is a coincidence wearing a verdict.
func (g *irGen) membershipOfBuiltSlotHaystack(b *strings.Builder, n *BinOp) (string, bool, error) {
	ix, isRead := n.R.(*Index)
	if !isRead {
		return "", false, nil
	}
	needleKind, kindOk := g.elemKindTag(n.L)
	if !kindOk {
		// The needle's kind is not provable: the untagged compare would answer every question with
		// whichever number happened to sit in the slot, so no arm here can be trusted.
		return "", false, nil
	}
	hay, hayTag, okRead := g.builtSlotPair(b, ix)
	if !okRead || hay == "" || hayTag == "" {
		// The static door may well READ this slot and still label it with a kind a membership test cannot
		// walk — a text, a number, a verdict, the void. The object is what says which arm runs, and what
		// raises for a kind with no elements (roadmap Gap R.285, ADR 0419).
		hay, hayTag, okRead = g.slotPairWhereStaticLabelCannotWalk(b, ix)
	}
	if !okRead || hay == "" || hayTag == "" {
		return "", false, nil
	}
	// The needle is asked the same question as the haystack when it is itself a slot: a value the
	// program put in a container has its kind beside its payload, and nothing else. Going through
	// `taggedOperand` alone read the *notebook*'s kind for such a needle, and the notebook describes
	// the container the needle was read out of, not the needle — the answer came back "not present"
	// for two containers the reference calls equal (roadmap L11.1, Gap R.95's needle half, ADR 0187).
	needle, needleTag, okNeedle := "", "", false
	needleIsIndex := false
	if nix, isIdx := n.L.(*Index); isIdx {
		needleIsIndex = true
		needle, needleTag, okNeedle = g.builtSlotPair(b, nix)
	}
	if !okNeedle {
		needle, needleTag, okNeedle = g.taggedOperand(b, n.L)
	}
	if !okNeedle {
		return "", false, nil
	}
	g.heapUsed = true
	sp := n.Span()

	type verdict struct {
		val, tail string
	}
	var got []verdict
	// The arms that answer, in the order the reference walks them: a list by its elements, a dict by
	// its keys, a set by its members, a text by its substrings.
	walkArms := []struct {
		tg int32
		fn string
	}{
		{int32(TagList), "rt_contains_tagged"},
		{int32(TagDict), "rt_dict_has_tagged"},
		{int32(TagSet), "rt_set_contains_tagged"},
	}
	for _, k := range walkArms {
		arm, chk := g.newLabel("in.arm"), g.newLabel("in.chk")
		c := g.newTmp()
		fmt.Fprintf(b, "  %s = icmp eq i32 %s, %d\n", c, hayTag, k.tg)
		g.markI1(c)
		// A dict is looked up by its key and a set is hashed by its member, and a container cannot be
		// either: the reference stops on that with `unhashable type: 'list'` rather than reporting the
		// absence, and an answer of "not in" is a whole error class lost (ADR 0311 made an unhashable
		// *write* raise from its tag; this is the read half of the same rule). The test is on the needle's
		// tag rather than on the kind the compiler proved, because for a needle that is itself a slot read
		// the tag is the only statement about what the needle is.
		//
		// A set haystack is the one exception the reference itself makes: `{1} in {1, 2}` is answered —
		// `False`, not a raise — because a set needle is looked up as the frozenset it converts to, and
		// this language has no frozenset to be found. A list or a dict needle into that same set still
		// raises, and every unhashable needle into a dict does.
		guard := ""
		switch k.fn {
		case "rt_dict_has_tagged":
			guard = "dict"
		case "rt_set_contains_tagged":
			guard = "set"
		}
		fmt.Fprintf(b, "  br i1 %s, label %%%s, label %%%s\n", c, arm, chk)
		fmt.Fprintf(b, "%s:\n", arm)
		if guard != "" {
			g.guardHashableNeedle(b, needleTag, sp, guard == "dict")
		}
		hit := g.newTmp()
		fmt.Fprintf(b, "  %s = call i32 @%s(i32 %s, i32 %s, i32 %s)\n", hit, k.fn, hay, needle, needleTag)
		tail := g.newLabel("in.tail")
		fmt.Fprintf(b, "  br label %%%s\n", tail)
		got = append(got, verdict{hit, tail})
		fmt.Fprintf(b, "%s:\n", chk)
	}
	// The text arm asks a different question, and asks it only of a text needle: `"b" in s` is a
	// substring test, and `1 in s` is the TypeError that says so. Which of the two runs is decided by
	// the needle's tag, which the compiler proved above and passes as the literal it proved.
	{
		arm, chk := g.newLabel("in.arm"), g.newLabel("in.chk")
		c := g.newTmp()
		fmt.Fprintf(b, "  %s = icmp eq i32 %s, %d\n", c, hayTag, int32(TagStr))
		g.markI1(c)
		fmt.Fprintf(b, "  br i1 %s, label %%%s, label %%%s\n", c, arm, chk)
		fmt.Fprintf(b, "%s:\n", arm)
		if needleKind == int32(TagStr) {
			hit := g.newTmp()
			fmt.Fprintf(b, "  %s = call i32 @rt_str_contains(i32 %s, i32 %s)\n", hit, hay, needle)
			tail := g.newLabel("in.tail")
			fmt.Fprintf(b, "  br label %%%s\n", tail)
			got = append(got, verdict{hit, tail})
		} else if needleKind == int32(TagInstance) {
			// Asking a text whether it contains an instance is not a question answered `False`; the
			// sentence names the needle's class, which the slot's tag alone cannot say (Gap R.209).
			g.raiseInstanceTo(b, needle, instanceMsgStrNeedle, sp)
		} else if needleIsIndex {
			// A slot-read needle's compile-time kind can only describe the container it came out of, not
			// the slot at its end. The tag the pair carries is the fact; the compiler's default word is not.
			instArm, staticArm := g.newLabel("in.instneedle"), g.newLabel("in.staticneedle")
			c := g.newTmp()
			fmt.Fprintf(b, "  %s = icmp eq i32 %s, %d\n", c, needleTag, int32(TagInstance))
			g.markI1(c)
			fmt.Fprintf(b, "  br i1 %s, label %%%s, label %%%s\n", c, instArm, staticArm)
			fmt.Fprintf(b, "%s:\n", instArm)
			g.raiseInstanceTo(b, needle, instanceMsgStrNeedle, sp)
			fmt.Fprintf(b, "%s:\n", staticArm)
			g.raiseTo(b, exnCode("TypeError"), "TypeError", inStrNeedleSentence(g.tagName(needleKind)), sp)
		} else {
			// The haystack is text and the needle provably is not: no verdict is available, so this arm
			// has no edge into the merge — ADR 0205's rule, the one that keeps a phi's predecessors all
			// blocks that stored a value.
			g.raiseTo(b, exnCode("TypeError"), "TypeError", inStrNeedleSentence(g.tagName(needleKind)), sp)
		}
		fmt.Fprintf(b, "%s:\n", chk)
	}
	// An instance has no iterator, but it is not a number either: the reference stops with the class
	// the program actually stored, and slot 0 of the heap object is the only place that can say it.
	{
		arm, chk := g.newLabel("in.bad"), g.newLabel("in.chk")
		c := g.newTmp()
		fmt.Fprintf(b, "  %s = icmp eq i32 %s, %d\n", c, hayTag, int32(TagInstance))
		g.markI1(c)
		fmt.Fprintf(b, "  br i1 %s, label %%%s, label %%%s\n", c, arm, chk)
		fmt.Fprintf(b, "%s:\n", arm)
		g.raiseInstanceTo(b, hay, instanceMsgMembership, sp)
		fmt.Fprintf(b, "%s:\n", chk)
	}
	// A number, a verdict and None have no elements, keys, members or characters. The reference stops
	// on each with a sentence naming the kind, and the program's `except TypeError:` reaches every one
	// of them (ADR 0228).
	// A number, a verdict and None have no elements, keys, members or characters. The reference stops
	// on each with a sentence naming the kind, and the program's `except TypeError:` reaches every one
	// of them (ADR 0228). The list is `notIterableKinds` — the same table the compile-time fold over a
	// spelled literal reads, so the tag question and the fold agree by construction (**Gap R.521**, **ADR 0452**).
	for _, t := range notIterableKinds {
		arm, chk := g.newLabel("in.bad"), g.newLabel("in.chk")
		c := g.newTmp()
		fmt.Fprintf(b, "  %s = icmp eq i32 %s, %d\n", c, hayTag, t.Tag)
		g.markI1(c)
		fmt.Fprintf(b, "  br i1 %s, label %%%s, label %%%s\n", c, arm, chk)
		fmt.Fprintf(b, "%s:\n", arm)
		g.raiseTo(b, exnCode("TypeError"), "TypeError", notIterableSentence(t.Name), sp)
		fmt.Fprintf(b, "%s:\n", chk)
	}
	// What is left is a kind this language has no membership for — a tuple, a class or a function.
	// It is told it does not contain things rather than walked as a container it is not, which is the
	// word the reference itself uses for an `object`.
	g.raiseTo(b, exnCode("TypeError"), "TypeError", notIterableSentence("object"), sp)

	merge := g.newLabel("in.merge")
	for _, v := range got {
		fmt.Fprintf(b, "%s:\n", v.tail)
		fmt.Fprintf(b, "  br label %%%s\n", merge)
	}
	fmt.Fprintf(b, "%s:\n", merge)
	parts := make([]string, 0, len(got))
	for i, v := range got {
		parts = append(parts, fmt.Sprintf("[ %s, %%%s ]", v.val, got[i].tail))
	}
	res := g.newTmp()
	fmt.Fprintf(b, "  %s = phi i32 %s\n", res, strings.Join(parts, ", "))
	asBool := g.newTmp()
	fmt.Fprintf(b, "  %s = icmp ne i32 %s, 0\n", asBool, res)
	g.markI1(asBool)
	if n.Op == "not in" {
		inv := g.newTmp()
		fmt.Fprintf(b, "  %s = xor i1 %s, true\n", inv, asBool)
		g.markI1(inv)
		return g.asBoolI32(b, inv), true, nil
	}
	return g.asBoolI32(b, asBool), true, nil
}

// iterLenUnderTag answers how many steps a loop over a slot's value has, and raises what the reference
// raises when the slot holds something with no steps at all. One icmp and one branch per kind, every
// failing arm leaving through the handler the program opened: the count itself is the object's answer,
// asked by @rt_iter_len, because how many entries a slot's object has is a question only it can answer
// (roadmap L11.1, Gap R.95, ADR 0251's shape).
func (g *irGen) iterLenUnderTag(b *strings.Builder, hay, tag string, sp Span) string {
	g.heapUsed = true
	for _, t := range []struct {
		tg   int32
		name string
	}{
		{int32(TagInt), "int"},
		{int32(TagFloat), "float"},
		{int32(TagBool), "bool"},
		{int32(TagNone), "NoneType"},
	} {
		arm, chk := g.newLabel("iter.bad"), g.newLabel("iter.chk")
		c := g.newTmp()
		fmt.Fprintf(b, "  %s = icmp eq i32 %s, %d\n", c, tag, t.tg)
		g.markI1(c)
		fmt.Fprintf(b, "  br i1 %s, label %%%s, label %%%s\n", c, arm, chk)
		fmt.Fprintf(b, "%s:\n", arm)
		g.raiseTo(b, exnCode("TypeError"), "TypeError", notIterableLoopSentence(t.name), sp)
		fmt.Fprintf(b, "%s:\n", chk)
	}
	{
		arm, chk := g.newLabel("iter.bad"), g.newLabel("iter.chk")
		c := g.newTmp()
		fmt.Fprintf(b, "  %s = icmp eq i32 %s, %d\n", c, tag, int32(TagInstance))
		g.markI1(c)
		fmt.Fprintf(b, "  br i1 %s, label %%%s, label %%%s\n", c, arm, chk)
		fmt.Fprintf(b, "%s:\n", arm)
		g.raiseInstanceTo(b, hay, instanceMsgLoop, sp)
		fmt.Fprintf(b, "%s:\n", chk)
	}
	// The four kinds a loop can step over; anything else is the same sentence again, wearing the word
	// the reference uses for a value that is not one of them.
	walkable := []int32{int32(TagStr), int32(TagList), int32(TagDict), int32(TagSet)}
	acc := ""
	for _, w := range walkable {
		c := g.newTmp()
		fmt.Fprintf(b, "  %s = icmp eq i32 %s, %d\n", c, tag, w)
		g.markI1(c)
		if acc == "" {
			acc = c
			continue
		}
		or := g.newTmp()
		fmt.Fprintf(b, "  %s = or i1 %s, %s\n", or, acc, c)
		g.markI1(or)
		acc = or
	}
	walkL, badL := g.newLabel("iter.walk"), g.newLabel("iter.badg")
	fmt.Fprintf(b, "  br i1 %s, label %%%s, label %%%s\n", acc, walkL, badL)
	fmt.Fprintf(b, "%s:\n", badL)
	g.raiseTo(b, exnCode("TypeError"), "TypeError", notIterableLoopSentence("object"), sp)
	fmt.Fprintf(b, "%s:\n", walkL)
	n := g.newTmp()
	fmt.Fprintf(b, "  %s = call i32 @rt_iter_len(i32 %s, i32 %s)\n", n, hay, tag)
	return n
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

// staticSlotIsText answers the OPERATOR label roads -- the question `is this operand a text?` that the
// concatenation, the repeat and the method doors ask before they choose a word for a value. For a read
// out of a dict literal the compiler can still walk, the answer is the answer about the ENTRY: `d =
// {None: "a"}` / `print(d[None] + "b")` is `ab` and `print(d[None] * 2)` is `aa`, and until the static
// key door could read a void key (ADR 0419) this road answered "not a text" for the `None`-keyed dict
// while answering "a text" for the `1`-keyed one -- the same program, two answers, and neither of them
// the reference's (roadmap Gap R.285).
//
// The permission is ADR 0241's, unchanged: the literal is only consulted where it still describes the
// object's slots. The SCOPE is this row's own defect: a DICT. Measured, widening it one step further --
// to a LIST slot -- moved programs the pair door answers correctly onto a one-word road that refuses
// them: `print(len(["a"][0]))` printed `1` before and refused after, at exit 1, with ADR 0331's own
// sentence, because `exprIsString` is also the question `len`, the ordering road and the print door ask,
// and a list element those roads reach through the pair door is a value whose tag the fold cannot carry.
// A dict is where the answer was missing and nothing else was using the question: a dict whose keys or
// values mix kinds has NO kind recorded on the variable at all (`dictValStr` is cleared with the rest,
// because the object is self-describing), so every operator road answered "not a text" for every entry of
// a mixed dict -- which is why `{None: "a", 1: "b"}` / `print(d[1] + "z")` refused while
// `{1: "a", 2: "b"}` answered, and why the void-keyed dict had no operator road that agreed with the
// object-reading road (roadmap Gap R.285, ADR 0419).
func (g *irGen) staticSlotIsText(ix *Index) bool {
	if ix == nil {
		return false
	}
	if kind, isDict := g.staticContainerKind(ix.Obj); !isDict || kind != "dict" {
		return false
	}
	el, ok := g.staticElemExpr(ix.Obj, ix.Idx)
	if !ok || el == nil {
		// The key names no entry the literal holds. That is not "not a text" -- it is a READ THAT RAISES,
		// and the road that refuses to label it is the difference between the reference's
		// `KeyError: False` and a compiler that declines the program (measured, and the row that owns the
		// sentence's honesty is Gap R.116's). The read is emitted, the lookup raises before any
		// concatenation's semantics can be chosen, and the guarantee that the entry really is absent is
		// ADR 0241's: the moment the program WRITES that key the literal stops describing the object's
		// slots, and this door is silent for that dict again (roadmap Gap R.285, ADR 0419).
		return g.dictLiteralHasNoSuchKey(ix.Obj, ix.Idx)
	}
	if g.exprIsString(el) {
		return true
	}
	_, isStr := g.stringVal(el)
	return isStr
}

// dictLiteralHasNoSuchKey answers the read road's question -- `can this subscript miss?` -- of a dict the
// compiler still holds the literal for. It is true only when every key the literal spells is a key the
// static door can compare (so "none of them matches" is a decision and not a shrug): a key the door cannot
// read at all makes the whole walk unknown, and unknown is a refusal, not a raise.
func (g *irGen) dictLiteralHasNoSuchKey(container, key Expr) bool {
	// BOTH sides have to be readable for "no entry matches" to be a decision rather than a shrug. The
	// literal's keys are half of it; the KEY BEING LOOKED UP is the other half, and leaving that question
	// unasked was a regression this row measured and paid: `d = {1: 2, 3: 4}` / `for k in d:` /
	// `    t = t + d[k]` asks the door about a key the compiler never saw (a loop variable), every literal
	// key fails to match it, the walk answered "that key names no entry — the read will raise", the label
	// road believed it was a text, and `t + d[k]` — two integers — was refused with the concatenation's
	// sentence (`pkg/lang/dict_iteration_test.go`'s twin in `integration/dict_iteration_test.go`, which
	// printed `6` on this row's parent). An unreadable lookup key means the read may land on ANY entry, so
	// the answer is nothing at all and the object-reading road takes the program (roadmap Gap R.285).
	if !g.constKeyIsReadable(key) {
		return false
	}
	c := container
	for {
		switch n := c.(type) {
		case *Name:
			if lit, ok := g.containerLits[n.Value]; ok {
				c = lit
				continue
			}
			if lit, ok := g.staticDicts[n.Value]; ok {
				c = lit
				continue
			}
			return false
		case *DictLit:
			for _, k := range n.Keys {
				if !g.constKeyIsReadable(k) {
					return false // the door cannot compare this key, so nothing is known about the miss
				}
				if g.constKeysMatch(k, key) {
					return false
				}
			}
			return len(n.Keys) > 0
		default:
			return false
		}
	}
}

// constKeyIsReadable is the door's own honesty question: does `constKeysMatch` have an arm for a key
// spelled this way at all. A container key (a list, a dict, a set, a tuple) has none -- and is unhashable
// besides, which is a raise the read road words -- and a call, a name or a comprehension is a value the
// compiler has not seen.
func (g *irGen) constKeyIsReadable(e Expr) bool {
	switch g.constKeyView(e).(type) {
	case *IntLit, *FloatLit, *StrLit, *BoolLit, *NoneLit:
		return true
	}
	if _, ok := g.stringVal(e); ok {
		return true
	}
	return false
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
//
// The two arms this door used to lack are the void key and the verdict key, and **ADR 0419** opened
// them together with the two roads that were the reason they stayed shut (roadmap Gap R.285). The
// measurement that kept the `None` arm out for eight weeks was this: `"a" in d[None]` over an integer
// answered False where the reference raises `argument of type 'int' is not iterable`, and
// `for x in d[None]` printed nothing over a text and `0 1 2` over the integer 5 where the reference
// raises for both. Neither of those is this door's fault — they are two *callers* that read an
// entry's kind off this door and then walked the entry as a container they could not walk. Those two
// callers now ask the object instead (`slotPairWhereStaticLabelCannotWalk`), which is the order the
// row prescribed: widen the label roads first, then the door, so the four rows are controls and not
// regressions.
//
// The void key is answered FIRST, before any number arm can reach it: `foldConstInt` reads a
// `NoneLit` as the integer 0, so an `IntLit` key compared against a `None` index would statically
// find an entry the program never wrote — `{0: "a"}` read by `None`. That crossing is refused here
// rather than being left harmless-by-luck, as it was while every road that reached it happened to ask
// the object (whose tag-aware lookup raises the reference's `KeyError: None`, now pinned both ways).
//
// A verdict key is the number the reference says it is — `{True: "a"}[1]` and `{1: "a"}[True]` are one
// lookup — but it is never a text, which is why the kind is asked before the number there too.
// constKeyView retires a KEY that arrived through a BINDING to the singleton its own binding wrote, so
// the static key door can read an entry the program addresses by name.
//
// `constKeysMatch` compares a key the dict literal spells against the key the SOURCE wrote, and a `*Name`
// had no arm: `d = {None: "a"}` / `k = None` / `print(d[k] + "b")` therefore asked the label road about an
// entry the door could not find, and the program met a sentence about a `TypeError` the reference does not
// raise. The ADR 0172 record answers the question — a variable's LAST binding decides what it is, which is
// the rule print, truthiness, `str` and equality already live by — and the door has no business knowing
// less about the same name at the same point than the printer does (roadmap Gap R.286, beside ADR 0419).
//
// Only the VOID is retired, because only the void is unambiguous: `boolVars` says a name holds a verdict
// and not WHICH one, and a key that cannot be quoted is left as the name the door cannot read — the
// refusal, not a guess. A loop variable the unroll gave a per-item tag is THIS item, not the kind some
// earlier binding handed the name (Gap R.358's rule), and a name the container records claim holds a
// handle: both stand aside, and the object-reading road answers them.
func (g *irGen) constKeyView(e Expr) Expr {
	nm, isName := e.(*Name)
	if !isName || g == nil {
		return e
	}
	if g.loopElemTag != nil {
		if _, perItem := g.loopElemTag[nm.Value]; perItem {
			return e
		}
	}
	if g.nameIsContainerRecorded(nm) {
		return e
	}
	if g.noneVars[nm.Value] {
		return &NoneLit{Src: nm.Span()}
	}
	return e
}

func (g *irGen) constKeysMatch(a, b Expr) bool {
	// Both sides are retired through the binding's record first: a key the program holds in a name whose
	// last binding wrote the void IS the void key (roadmap Gap R.286).
	a, b = g.constKeyView(a), g.constKeyView(b)
	// The void is a key of its own kind, equal to nothing but itself: not 0, not False, not 0.0.
	if _, isNone := a.(*NoneLit); isNone {
		_, bIsNone := b.(*NoneLit)
		return bIsNone
	}
	if _, isNone := b.(*NoneLit); isNone {
		return false // the symmetric half — an integer, float or text key is never the void key
	}
	if ab, isBool := a.(*BoolLit); isBool {
		if isStringExpr(b) {
			return false
		}
		if bb, isBoolB := b.(*BoolLit); isBoolB {
			return ab.Value == bb.Value
		}
		bi, ok2 := g.foldConstInt(b)
		return ok2 && bi == constBoolWord(ab.Value)
	}
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

// constBoolWord is the number a verdict key carries in the reference's identity space.
func constBoolWord(v bool) int64 {
	if v {
		return 1
	}
	return 0
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
		if kind == "list" {
			// The index is asked what it is BEFORE it is lowered, at the door itself and not only on the
			// road that reaches the fold: a text, a float, a None or a bool cannot be a position, and a
			// list or dict used as an index has no word to lower to at all — handing one to the bounds
			// check is what put a `@.lstN` global inside an `icmp slt` and made `llc` reject the module
			// (Gap R.222, ADR 0329). The dict branch beside this one has always asked its own version of
			// the same question inside the door (`checkKeyReadTagged` and Gap R.217's unhashable gate),
			// so both halves of the subscript — the one the fold answers and the one the pair door answers
			// — are guarded by the same gate, which is the only way `print([1,2]["a"])` and
			// `print(xs["a"])` cannot grow apart (roadmap Gap R.218, ADR 0331).
			if g.badIndexGate(b, "", key, sp) {
				// The raise ends the block; the two words the caller reads back are unreachable, and
				// handing back the pair the caller expects is what keeps the module valid.
				return "0", "0", true
			}
		}
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
			g.checkKeyReadTagged(b, base, kv, kt, key, sp)
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
	case *Slice:
		// A slice of a container IS the container the reference builds: a fresh heap list whose slots are
		// copies of the base's payloads with their tags (`rt_slice` pairs them — ADR 0187's rule, applied by
		// ADR 0294), so `2 in xs[0:2]`, `xs[0:2] == [1, 2]` and `xs[0:1][0]` ask the object the same questions
		// a variable's list does. A text slice answers beside this, in the text road, so the base is asked.
		if !g.isContainerExpr(n.Obj) {
			break
		}
		h, serr := g.value(b, e)
		if serr != nil {
			return "", serr
		}
		return h, nil
	case *Call:
		// `return build()` forwards a container a second function built. That function's own `ret` is
		// already the handle its builders made (Gap R.67), so this body hands the word on; the scan proved
		// the callee answers a container, and the permission below is what lets the ordinary call road
		// lower it here instead of refusing to use its own answer (ADR 0273's permission, read again by
		// ADR 0333).
		if g.callAnswersContainer(n) {
			g.heapUsed = true
			calleeName := n.Fn.(*Name).Value
			g.containerAsked[calleeName] = true
			h, err := g.value(b, e)
			delete(g.containerAsked, calleeName)
			return h, err
		}
		// The METHOD round (**Gap R.453**, **ADR 0430**): the object a body in a class builds lives in the
		// callee's frame, so the handle is the payload the carried method stored beside its `ret` — the same
		// word the print door, the loop road and `len` already read, read here (`containerHandleOf`'s twin).
		if _, okAns := g.methodContainerAnswerOf(n); okAns {
			h, _, okPair, perr := g.pairCallPair(b, e)
			if perr != nil {
				return "", perr
			}
			if !okPair {
				// The pair door declined — better its sentence than a word invented for the answer.
				return "", fmt.Errorf("codegen: %s does not name a container the compiled backend can prove; a call answers a container only where the scan can see every arm of the callee answer one (roadmap Gap R.67, ADR 0333)", exprSnippet(e))
			}
			g.heapUsed = true
			return h, nil
		}
		return "", fmt.Errorf("codegen: %s does not name a container the compiled backend can prove; a call answers a container only where the scan can see every arm of the callee answer one (roadmap Gap R.67, ADR 0333)", exprSnippet(e))
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
	case *CondExpr:
		// A ternary whose arms are containers is a container the TEST chooses. Its handle cannot be built
		// beside the test: an arm is a program the test runs, not an operand of the instruction that picks
		// (roadmap Gap R.340, ADR 0374), so each arm's builder runs inside the block the branch enters and
		// the merge phis the two handles. A test the source wrote is not a run-time question, so the arm that
		// runs is the answer and the other arm is not in the program at all.
		//
		// The shape this replaces is the exit-2 class ADR 0166 reserves for a compiler bug: the road that
		// read the arm asked `value`, which renders a container literal as the ADDRESS of its compile-time
		// global, and the printer was handed `i32 @.lst1` in an argument slot —
		// `global variable reference must have pointer type` (roadmap Gap R.385, ADR 0188's rule one
		// construct later, ADR 0192's rule for a ternary).
		if kind, ok := g.staticContainerKind(n); ok {
			if taken, decided := constantTestArm(n); decided {
				_ = kind
				return g.containerOperand(b, taken)
			}
			h, okBranch, berr := g.ternaryBranch(b, n, "i32", func(bb *strings.Builder, e Expr) (string, error) {
				return g.containerOperand(bb, e)
			})
			if berr != nil {
				return "", berr
			}
			if okBranch {
				return h, nil
			}
		}
	case *BinOp:
		// A sequence the door builds — `xs + ys`, `xs * 3`, `(1, 2) * 2` — is a fresh heap object whose slots
		// are the operands' payloads copied WITH their tags, exactly the object `rt_slice` builds for a copy,
		// so the roads that read a container ask it of the object (roadmap Gap R.33, ADR 0365).
		h, okSeq, serr := g.seqHandleOf(b, e)
		if serr != nil {
			return "", serr
		}
		if okSeq {
			return h, nil
		}
	}
	return "", fmt.Errorf("codegen: %s is not a container (roadmap L11.1, ADR 0189)", exprSnippet(e))
}

// ternaryHasContainerArm is the print door's "may this arm not be lowered as a number" question: either
// arm answers a container (and, for a test the source wrote, only the arm that runs counts). It is asked
// after `staticContainerKind` declined, so the pair is a container in ONE arm and has no kind to name —
// the shape that used to reach the assembler as `i32 @.lst1` in an argument slot (roadmap Gap R.385).
func (g *irGen) ternaryHasContainerArm(n *CondExpr) bool {
	if n == nil {
		return false
	}
	if taken, decided := constantTestArm(n); decided {
		return g.isContainerArm(taken)
	}
	return g.isContainerArm(n.If) || g.isContainerArm(n.Else)
}

// isContainerArm is the question an ARM of a ternary answers, and `staticContainerArmKind` the kind it
// answers. They are the pair-wide version of `isContainerExpr`/`staticContainerKind`, widened by the one
// spelling an arm commonly takes that the module-level roads settle by another door: a COMPREHENSION. A bare
// `[v for v in xs]` is built by the comprehension road and printed by the comprehension printer, so the
// predicate never had to call it a container; an arm has to be, or the door that chooses the printer cannot
// see which printer to choose and the number road hands the assembler the arm's folded global in a word slot
// (roadmap Gap R.385). A generator expression `(v for v in xs)` is NOT one: this backend materialises it
// eagerly as a list, which is a divergence the generator row owns, and an arm of that spelling keeps the
// road it has rather than being re-decided here.
//
// Every arm question — this one, the one `staticContainerKind` asks, and the one the print door asks — is
// read through these two functions, so the road that may build and the door that prints cannot disagree
// about which programs are containers (ADR 0232's decide-and-build pair, read at the ternary).
func (g *irGen) isContainerArm(e Expr) bool {
	if g.isContainerExpr(e) {
		return true
	}
	comp, ok := e.(*Comp)
	return ok && compIsContainer(comp)
}

// staticContainerArmKind answers the arm's kind, or false when the arm is not a container or spells a kind
// no printer is chosen from.
func (g *irGen) staticContainerArmKind(e Expr) (string, bool) {
	if kind, ok := g.staticContainerKind(e); ok {
		return kind, true
	}
	if comp, ok := e.(*Comp); ok && compIsContainer(comp) {
		switch comp.Kind {
		case CompList:
			return "list", true
		case CompDict:
			return "dict", true
		case CompSet:
			return "set", true
		}
	}
	return "", false
}

// compIsContainer is the comprehension half of the arm question: the three spelling-bridged kinds, and not
// the generator, whose answer this backend builds eagerly into a list (see above).
func compIsContainer(n *Comp) bool {
	if n == nil {
		return false
	}
	switch n.Kind {
	case CompList, CompDict, CompSet:
		return true
	}
	return false
}

// ternaryContainerArmErr names the arms the program wrote and the kind each answers, because the reader
// has to be able to tell which arm the refusal is about — "not a container" said nothing about a program
// whose arms ARE containers (roadmap Gap R.38, the sentence that misdescribes the value).
func ternaryContainerArmErr(n *CondExpr) error {
	arms := ternaryArmNames(n)
	return fmt.Errorf("codegen: the arms of `%s if %s else %s` are containers whose kinds this backend cannot choose one printer from — each arm's word is a heap handle and the print door picks its printer from a kind the compiler can see, so write the two arms the same kind, or bind each arm and print the name (roadmap Gap R.385, beside Gap R.67 and ADR 0333)", arms[0], arms[1], arms[2])
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
	e  Expr   // the operand itself
	ix *Index // non-nil when the object has to be asked, i.e. the side is a slot read
	// popCall is the answer of `NAME.pop([i])` on a list the program built: the one call answer whose kind
	// sits in the container rather than in a callee's `return`, so the side has to ask the object what it
	// removed. It walks the same arms a slot read walks, and that is the difference between
	// `xs = [1, "a"]` / `print(xs.pop(0) > 1)` raising the reference's TypeError and answering `False` at
	// exit 0 for a text against a number (roadmap L11.1, ADR 0410; Gap R.296's rule for the pair arm).
	popCall *Call
	// pairName is a name the pair road bound (`n = xs[0]`, a loop element over a container that mixes
	// kinds). Its payload means a different thing per kind, so the ordering asks the tag the same way a
	// slot read does — which is the difference between `n > 7` answering and comparing the interned index
	// of "a" as a number (roadmap L11.1, Gap R.148).
	pairName  string
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
// asksTag reports that this side's kind is a run-time fact the door has to test: either a slot read out of
// an object, or a name the pair road bound. Both carry a tag register; neither is settled at compile time.
func (s *orderSide) asksTag() bool { return s.ix != nil || s.pairName != "" || s.popCall != nil }

// readsObject is the side's answer to "does this operand reach into an object the run time owns?" — a
// slot read, or the slot a `pop()` removes. The ordering admits a side whose kinds come from the object
// only against an operand the compiler read itself, so the guard has to count the popped slot beside
// the read it already counted (roadmap L11.1, ADR 0410).
func (s *orderSide) readsObject() bool { return s.ix != nil || s.popCall != nil }

func (s *orderSide) textTest() string {
	if !s.asksTag() {
		if s.tagC == int32(TagStr) {
			return "true"
		}
		return "false"
	}
	return s.tagIsText
}

func (s *orderSide) numberTest() string {
	if !s.asksTag() {
		if s.tagC == int32(TagInt) || s.tagC == int32(TagBool) || s.tagC == int32(TagFloat) {
			return "true"
		}
		return "false"
	}
	return s.tagIsNum
}

func (s *orderSide) definitelyNotText() bool {
	if !s.asksTag() {
		return s.tagC != int32(TagStr)
	}
	return !s.canText
}

func (s *orderSide) definitelyNotNumber() bool {
	if !s.asksTag() {
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
	// A name the pair road bound is an ordering side whose kind the run time owns: the payload is a float
	// box, an interned text or the number, and only the tag says which. It walks the same three arms a slot
	// read out of an object walks, so `n > 7` compares numbers or raises the reference's own sentence for
	// the kind it found rather than keeping one word for the operand (roadmap L11.1, Gap R.148).
	if nm, isName := e.(*Name); isName && g.taggedVars[nm.Value] && g.taggedOrigin != nil {
		switch g.taggedOrigin[nm.Value] {
		case taggedOriginArith, taggedOriginFloat, taggedOriginParam, taggedOriginParamArith, taggedOriginSlot, taggedOriginLoop:
			return orderSide{e: e, pairName: nm.Value, fromTag: true, canNum: true, canText: true, ok: true}
		}
	}
	if c, isCall := e.(*Call); isCall {
		// The slot a `pop()` removes is a slot read by another name: the container owns the kind, so the
		// side may report any of them and only the tag the removal carried out says which. This is the arm
		// that makes `print(xs.pop(0) > 1)` raise `TypeError: '>' not supported between instances of 'str'
		// and 'int'` for the text it removed, instead of beating 1 with an interned index (roadmap L11.1,
		// ADR 0410 — and the same `False`-for-a-TypeError shape Gap R.93 named for the slot read).
		if _, _, serves := g.popAnswerShape(c); serves {
			return orderSide{e: e, popCall: c, fromTag: true, canNum: true, canText: true, ok: true}
		}
	}
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
	if !ls.asksTag() && !rs.asksTag() {
		return false
	}
	// A side whose kind the object reports names itself in the raise sentence, which names *two* types
	// at once; one such side is a chain the arm can emit, two is a cross-product of sentences the
	// compiler would be inventing. So a fromTag side is admitted only against an operand the compiler
	// read itself — which also leaves container-against-container ordering to the door that compares
	// contents, instead of turning Gap R.86's wrong answer into a wrong trap (roadmap L11.1).
	if ls.fromTag && rs.readsObject() || rs.fromTag && ls.readsObject() {
		return false
	}
	needTxt := ls.canText && rs.canText
	needBad := ls.canNum && rs.canText || ls.canText && rs.canNum
	return needTxt || needBad
}

// orderTagOf reads the tag one side reports, together with its payload. A side the compiler already
// read has no tag to ask and leaves the register empty, which the arm tests below read as "settled".
func (g *irGen) orderTagOf(b *strings.Builder, s *orderSide) bool {
	if !s.asksTag() {
		return true
	}
	if s.pairName != "" {
		// A pair-bound name already carries both words; the door reads them from the name's own slots.
		payload, tag := g.numericPairRegs(b, s.pairName, Span{})
		s.payload, s.tag = payload, tag
		return true
	}
	if s.popCall != nil {
		// The popped slot's two words, from the one door that reads them: the removal lifts the tag out of
		// `@heap_tags` beside the payload, so the ordering compares numbers or raises the reference's own
		// sentence for the kind it found (roadmap L11.1, ADR 0410).
		payload, tag, ok, perr := g.popAnswerPair(b, s.popCall)
		if perr != nil || !ok || payload == "" || tag == "" {
			return false
		}
		s.payload, s.tag = payload, tag
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
	if !s.asksTag() || s.tag == "" {
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
	if !s.asksTag() {
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
	// See floatLoweringRefusal: a record that names its own cause is quoted, not re-diagnosed. The
	// slot sentence below is true of a container the object must be asked about and false of a format
	// string, and `print("%.2f" % 3.5)` reached this line wearing it (roadmap L11.2, Gap R.165, ADR 0282).
	if strings.HasPrefix(why, floatLowerCompleteMarker) {
		return fmt.Errorf("codegen: %s", strings.TrimPrefix(why, floatLowerCompleteMarker))
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
	// A record that already names its own cause is passed through. The slot sentence below is the
	// right diagnosis for "a slot whose kind only the object knows", and the wrong one for every
	// other unlift-able operand: `print("%.2f" % 3.5)` reached it saying the *format string* needed
	// the run-time tag, which is not true of a literal and would send a reader looking for a
	// container (roadmap L11.2, Gap R.165, ADR 0282). The marker is the record's own, set where the
	// cause is known.
	if strings.HasPrefix(why, floatLowerCompleteMarker) {
		return fmt.Errorf("codegen: %s", strings.TrimPrefix(why, floatLowerCompleteMarker))
	}
	return fmt.Errorf("codegen: %s of %q has no number the compiled backend can lift: a slot whose kind only the object knows is answered for equality, printing, membership and length, and using it as a number needs the tagged value word still owed here (roadmap L11.1, Gap R.82, ADR 0166)", why, n.Op)
}

// floatLowerCompleteMarker prefixes a `floatUnlowerable` record that is already a full sentence, so
// floatLoweringRefusal quotes it rather than diagnosing it again.
const floatLowerCompleteMarker = "complete: "

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
// it, spelled the way the record spells it — which is not one sentence: text against `+` gets
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
	case "abs":
		// `abs("hi")` is CPython's own sentence, and it is *not* the negation's: the reference names the
		// call, not an operator. One table, one wording per operation, so the interpreter, the emitted
		// raise and the tests cannot disagree (roadmap Gap R.140, ADR 0271).
		return "TypeError", fmt.Sprintf("bad operand type for abs(): '%s'", kind)
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
	return g.taggedDoubleFromPair(b, payload, tag, ix.Span(), n, side, otherName, sentence)
}

// pairDoubleInCtx lifts a pair-bound name into the double an operator wants, asking the per-tag arms the
// slot read already walks — which is the difference between `n / 4` answering 1.75 and a text slot
// answering the interned index of "a" as a number. The other operand's kind comes from where the operator
// spelled it, and the sentence is CPython's for this operator with that kind (ADR 0253, ADR 0252).
func (g *irGen) pairDoubleInCtx(b *strings.Builder, e Expr) (string, bool, error) {
	n := g.numCtx
	if n == nil {
		return "", false, nil
	}
	other := "int"
	for _, side := range []Expr{n.L, n.R} {
		if side != e {
			if k := g.numericUseKind(side); k != "" {
				other = k
			} else if _, isIx := side.(*Index); !isIx {
				if g.arithWouldRefuse(side) {
					// The other side is itself pair-shaped (`abs(n) / 2`): this door would lift THIS side
					// as a double and read the sibling as one word, which is the truncation the door
					// exists to prevent — and lowering it through the double road here made the two doors
					// call each other until the goroutine's stack gave out, a compiler failure ADR 0166
					// counts as exit 2. So the door keeps its hand off and the position refuses.
					return "", false, nil
				}
				if d := g.floatValue(b, side); d == "" {
					return "", false, nil // an operand whose kind would make the message a guess
				}
			}
		}
	}
	slotOnLeft := n.L == e
	sentence := func(kindName string) (string, string) {
		if slotOnLeft {
			return unsupportedNumberOp(n.Op, kindName, other)
		}
		return unsupportedNumberOp(n.Op, other, kindName)
	}
	nm, isName := e.(*Name)
	if !isName {
		return "", false, nil
	}
	payload, tag := g.numericPairRegs(b, nm.Value, nm.Span())
	return g.taggedDoubleFromPair(b, payload, tag, e.Span(), n, e, other, sentence)
}

// taggedDoubleFromPair is the same door asked of a pair it is handed rather than one it reads out of a
// slot: the float arm unboxes, the int/bool arm converts, and every other tag raises the sentence CPython
// writes for this operator and that kind. A name the pair road bound (`n = xs[0]`, a loop element over a
// container that mixes kinds) has no word to widen — the payload is a float box, an interned text, or the
// number, and only the tag says which — so the float domain reaches it here, through the arms the slot read
// already walks, rather than through a second lift that guesses (roadmap L11.1, Gap R.148; ADR 0253's
// per-kind raise, ADR 0252's closed tag set).
func (g *irGen) taggedDoubleFromPair(b *strings.Builder, payload, tag string, sp Span, n *BinOp, side Expr, otherName string, sentence func(string) (string, string)) (string, bool, error) {
	g.heapUsed = true
	g.floatFmtUsed = true
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

// taggedArithPair is the numeric door for the one question a slot keeps to itself until the program
// runs: **what kind of number is in here**. `xs = []` / `xs.append([7, 8])` / `print(xs[0][0] + 1)` is
// CPython's `8` and the evaluator's `8`, and until now the compiled backend refused it — the refusal
// was honest (it named the missing half) but it was exit 1 on a program the reference runs, and the
// tag the object already carries answers the question the compiler could not (roadmap L11.1's
// remaining numeric clause, ADR 0265).
//
// What it does is ask the object, once, and let the target finish the sentence: both operands become
// (payload, tag) pairs — a literal is a pair with a constant tag, a tagged variable is already one, a
// slot read comes out of the door that reads the payload with its tag — and `@rt_num_arith` does the
// operator in the one word that holds both families, deciding the *answer's* kind from the two tags it
// was handed. The raise is emitted here rather than in the helper, because only this code knows
// whether a handler is open: the helper fills a buffer, the emitted code stores it through the same
// raise door every trap in the language uses, so `except TypeError:` reaches it (ADR 0228) and the
// sentence is CPython's own (`unsupported operand type(s) for +: 'int' and 'str'`, and for a negation
// the different sentence the reference really writes, `bad operand type for unary -: 'str'`).
//
// It is deliberately narrow. Both operands must be pairable, at least one of them must actually need
// the pair — otherwise the ordinary road already gets the kind right — and anything the pairing cannot
// name (a call, an attribute) declines, which leaves the existing refusal in place rather than
// replacing a precise diagnostic with a wrong number.
// fieldIsConstantLiteral reports the only f-string fields the compile-time spec engine can read: the literals
// whose digits it writes itself. Everything else — a variable, a call, an arithmetic expression — has to reach
// one of the value doors below, because the engine returns the empty text for a field it cannot see and used to
// answer `f"{x!r}"` with nothing at all, at exit 0, for every variable in the language (roadmap Gap R.186,
// Gap R.192; ADR 0299's spec engine).
// fieldWantsPrinterQuotes reports the f-string fields whose `!r` is a rendering rather than a fold: a text
// the intern table already holds, or any expression the print road renders as interned text. Those go through
// `rt_str_of_value` with the quote flag — the door `repr()` uses — instead of having quote characters spliced
// into the module's format-string global (roadmap Gap R.192, a measured exit 2; ADR 0303's one printer).
func (g *irGen) fieldWantsPrinterQuotes(e Expr) bool {
	if _, isLit := e.(*StrLit); isLit {
		return true
	}
	if _, known := g.stringVal(e); known {
		return true
	}
	return g.printsAsInternedStr(e)
}

func fieldIsConstantLiteral(e Expr) bool {
	switch e.(type) {
	case *IntLit, *FloatLit, *StrLit:
		return true
	}
	return false
}

// callAnswersContainer reports whether a call's result is a container the compiler PROVED: the scan saw
// every arm of the callee answer one, and the shape of this particular call carries it. It is the permission
// `containerHandleOf`'s *Call arm takes, extracted so the comprehension loop asks the identical question and
// cannot answer it differently — the loop used to lower the call through the ordinary one-word road, take the
// `0` back, ask `rt_list_len` of it, and walk zero elements: `[x for x in h(3)]` over `def h(n): return
// range(n)` printed `[]` at exit 0 where CPython prints `[0, 1, 2]` (roadmap Gap R.67, ADR 0333, Gap R.291).
func (g *irGen) callAnswersContainer(n *Call) bool {
	if n == nil {
		return false
	}
	nm, isName := n.Fn.(*Name)
	if !isName {
		return false
	}
	if _, isContainerAnswer := g.containerAnswerOf(nm.Value); !isContainerAnswer {
		return false
	}
	return g.containerAnswerCallIsCarriable(n)
}

// filedPairOf is the f-string field's version of the same question print asks: does this expression travel
// as a (payload, tag) pair, and if so what are its two words? Four sources, each the door the print road
// already asks. A name the pair road bound reads its own two allocas; the answer of a fold the run time
// decided asks `@rt_pair_fold`; a **slot read** asks the one subscript dispatch print asks
// (`subscriptPairOf`, roadmap Gap R.283 — the field used to ask none of its arms and rendered a `@str_tab`
// index where the line above it rendered a word); and an expression built over one (`n - 1`, `-n`, `n // 2`)
// goes through the arithmetic door print uses, so the field, `print()` and `str()` cannot disagree about `6`
// versus `6.0` — and an operator that door will not vouch for (`+` over a slot whose kind is a run-time fact)
// answers not-ok and the field keeps the refusal it has always printed, rather than reading a payload alone
// (roadmap L11.1,
// Gap R.146's rendering positions; ADR 0303's one printer, ADR 0265's per-kind door).
func (g *irGen) filedPairOf(b *strings.Builder, e Expr) (payload, tag string, ok bool, err error) {
	if nm, isName := e.(*Name); isName && (g.numericPairVar(nm.Value) || (g.taggedVars != nil && g.taggedVars[nm.Value])) {
		payload, tag = g.numericPairRegs(b, nm.Value, nm.Span())
		return payload, tag, true, nil
	}
	// The answer of a fold the run time decided: `f"{min(n, 3)}"` prints the WINNER, whose kind is the
	// winner's own, and the field must ask the same door print and str() ask or the three renderings of one
	// value disagree (roadmap L11.1, Gap R.146; ADR 0303's one printer, ADR 0316).
	if p, t, okFold, ferr := g.foldPairOf(b, e); ferr != nil {
		return "", "", false, ferr
	} else if okFold {
		return p, t, true, nil
	}
	// The field of a call whose answer carries its kind is the same print in disguise as `print(f())`:
	// `f"{f(1)}"` and `f"{f(0)}"` of a body that answers a text on one arm and a number on the other render
	// two forms, and the field asks the one door print asks rather than the `%d` leg of the printf below
	// (roadmap Gap R.219, ADR 0356's field, ADR 0368).
	if p, t, okCall, cerr := g.answerCallPair(b, e); cerr != nil {
		return "", "", false, cerr
	} else if okCall {
		return p, t, true, nil
	}
	// The field IS a print in disguise: `f"{d[1]}|{xs[0]}"` renders the same slots that `print(d[1])` renders,
	// so it asks the same dispatch — a dict/list literal's element, an element of a mixed list, an entry of a
	// dict whose values mix kinds, a slot read through a slot — and gets the payload together with the tag the
	// object keeps beside it. Until this arm existed the field asked none of them: `f"{d[1]}"` over
	// `d = {1: 3.5}` printed the `@float_box` handle `5`, `f"{d[2]}"` over a text entry printed its
	// `@str_tab` index, and the line above it printed `3.5` and `x` (roadmap Gap R.283, ADR 0356; ADR 0331's
	// docstring named the field a door position the code never filled in). An element whose kind cannot travel
	// without its tag is REFUSED here, as it is in print, rather than rendered as a number nobody wrote.
	if ix, isIndex := e.(*Index); isIndex {
		if v, t, okPair, xerr := g.subscriptPairOf(b, ix); xerr != nil {
			return "", "", false, xerr
		} else if okPair {
			return v, t, true, nil
		}
	}
	if g.taggedVars == nil {
		return "", "", false, nil
	}
	return g.taggedArithPair(b, e)
}

func (g *irGen) taggedArithPair(b *strings.Builder, e Expr) (val, tag string, ok bool, err error) {
	var op int
	var l, r Expr
	var sp Span
	switch n := e.(type) {
	case *BinOp:
		switch n.Op {
		case "+":
			op = 0
		case "-":
			op = 1
		case "*":
			op = 2
		case "//":
			op = 4
		case "%":
			op = 5
		default:
			return "", "", false, nil
		}
		l, r = n.L, n.R
		sp = n.Src
	case *Call:
		// abs(x) is the signless call: one operand, the magnitude out, CPython's raise when the tag says
		// the value has no sign. The door takes the operand twice because it is binary-shaped — the second
		// word is read only by the operators that ask for a divisor (roadmap L11.1, Gap R.146; ADR 0271's
		// wording rule, which `rt_num_bad` keeps per call rather than per operator).
		nm, isName := n.Fn.(*Name)
		if !isName || nm.Value != "abs" || len(n.Args) != 1 {
			return "", "", false, nil
		}
		op, l, r = 6, n.Args[0], n.Args[0]
		sp = n.Src
	case *UnOp:
		if n.Op != "-" {
			return "", "", false, nil
		}
		op, l, r = 3, n.X, n.X
		sp = n.Src
	default:
		return "", "", false, nil
	}
	if op == 6 {
		if !g.absOperandMayNeedPair(l) && !g.absOperandMayNeedPair(r) {
			return "", "", false, nil
		}
	} else if !g.operandMayNeedPair(l) && !g.operandMayNeedPair(r) {
		return "", "", false, nil
	}
	// `+` and `*` are the two operators the reference can answer with something that is not a number and
	// is not a raise: text or a list repeated, two texts or two lists joined. Answering a raise where the
	// reference answered a value is a wrong program, and this backend cannot answer the repetition at all
	// (Gap R.82), so those two operators take the door only where the compiler can see that no text and no
	// container can reach a slot (ADR 0265). `-` and the unary `-` answer a number or raise, always, so
	// they take it whatever the container holds. `%` joins the guarded list for its own reason: `"%.2f" % v`
	// is CPython's printf formatting, which answers a *text*, and this backend does not implement it — so a
	// slot that might hold a text keeps the ordinary road and the refusal it already gave (roadmap Gap R.165
	// names that missing feature). `//` needs no guard: no operand pair makes it answer a non-number.
	if op == 0 || op == 2 || op == 5 {
		if !slotLiteralMayBeNumber(l) || !slotLiteralMayBeNumber(r) {
			return "", "", false, nil
		}
		if isStringExpr(l) || isStringExpr(r) {
			return "", "", false, nil
		}
		// `+` beside a number the SOURCE spells needs no proof of what the slots hold, because no value the
		// slot can hold turns this operator into anything but a number or a raise: `2.5 + 1`, `True + 1` are
		// numbers and `None + 1`, `"a" + 1`, `[1] + 1` are CPython's TypeError, which `rt_num_arith` raises
		// with the slot's real kind named. The two shapes the guard exists for — `"a" + "b"` and `[1] * 2`,
		// which the reference answers with a joined text and a repeated list this backend cannot build
		// (Gap R.82) — are unreachable from a spelled number on the other side, so the door opens and the
		// answer's kind is derived from the two tags rather than asserted from one. `*` keeps the proof for
		// its own reason: `xs[0] * 2` over a slot holding a list IS answered by the reference, with a list.
		if !(op == 0 && g.plusBesideASpelledNumber(l, r)) {
			if !g.slotArithmeticIsProven(l) || !g.slotArithmeticIsProven(r) {
				return "", "", false, nil
			}
		}
	}
	ap, at, okA, errA := g.arithOperandPair(b, l)
	if errA != nil {
		return "", "", false, errA
	}
	bp, bt, okB, errB := g.arithOperandPair(b, r)
	if errB != nil {
		return "", "", false, errB
	}
	if !okA || !okB {
		return "", "", false, nil
	}
	g.heapUsed = true
	g.floatFmtUsed = true
	g.arithUsed = true
	outp, outt, outm, st := g.newTmp(), g.newTmp(), g.newTmp(), g.newTmp()
	fmt.Fprintf(b, "  %s = alloca i32\n", outp)
	fmt.Fprintf(b, "  %s = alloca i32\n", outt)
	fmt.Fprintf(b, "  %s = alloca i8*\n", outm)
	fmt.Fprintf(b, "  %s = call i32 @rt_num_arith(i32 %d, i32 %s, i32 %s, i32 %s, i32 %s, i32* %s, i32* %s, i8** %s)\n",
		st, op, ap, at, bp, bt, outp, outt, outm)
	// Two statuses, two classes: the operands do not add (CPython's TypeError) or the whole number
	// the answer would be does not fit this backend's int word (an OverflowError naming L12.12, raised
	// rather than letting `fptosi` answer poison — the rule Gap R.133 established the day it was filed).
	// A third says the divisor of `//` or `%` was zero, and the sentence is the runtime's, because which
	// of the reference's three it is depends on the tags, which are the run time's fact (Gap R.162).
	// A fourth says the answer was a double and the arena had no box for it — `MemoryError`, whose
	// sentence the compiler owns because no runtime buffer carries it (roadmap Gap Q.2, ADR 0347).
	isType, isOvf, isZero := g.newTmp(), g.newTmp(), g.newTmp()
	next, badType, badOvf, badZero, done := g.newLabel("numnext"), g.newLabel("numtype"), g.newLabel("numovf"), g.newLabel("numzero"), g.newLabel("numok")
	fmt.Fprintf(b, "  %s = icmp eq i32 %s, 1\n", isType, st)
	g.markI1(isType)
	fmt.Fprintf(b, "  br i1 %s, label %%%s, label %%%s\n", isType, badType, next)
	fmt.Fprintf(b, "%s:\n", badType)
	g.raiseRuntimeMsg(b, "TypeError", outm, sp)
	fmt.Fprintf(b, "%s:\n", next)
	fmt.Fprintf(b, "  %s = icmp eq i32 %s, 2\n", isOvf, st)
	g.markI1(isOvf)
	fmt.Fprintf(b, "  br i1 %s, label %%%s, label %%%s\n", isOvf, badOvf, badZero)
	fmt.Fprintf(b, "%s:\n", badOvf)
	g.raiseRuntimeMsg(b, "OverflowError", outm, sp)
	fmt.Fprintf(b, "%s:\n", badZero)
	fmt.Fprintf(b, "  %s = icmp eq i32 %s, 3\n", isZero, st)
	g.markI1(isZero)
	zeroRaise := g.newLabel("numzeroraise")
	memChk := g.newLabel("nummemchk")
	fmt.Fprintf(b, "  br i1 %s, label %%%s, label %%%s\n", isZero, zeroRaise, memChk)
	fmt.Fprintf(b, "%s:\n", zeroRaise)
	g.raiseRuntimeMsg(b, "ZeroDivisionError", outm, sp)
	// The fourth status is the arena's: the answer was a double and the box for it could not be had
	// (ADR 0347). It is read here rather than beside the call because this door's return value is a
	// status, not a handle — there is nothing for `emitHeapCall` to compare against.
	fmt.Fprintf(b, "%s:\n", memChk)
	isMem := g.newTmp()
	memRaise := g.newLabel("nummemraise")
	fmt.Fprintf(b, "  %s = icmp eq i32 %s, 4\n", isMem, st)
	g.markI1(isMem)
	fmt.Fprintf(b, "  br i1 %s, label %%%s, label %%%s\n", isMem, memRaise, done)
	fmt.Fprintf(b, "%s:\n", memRaise)
	g.raiseHeapExhausted(b, sp)
	fmt.Fprintf(b, "%s:\n", done)
	v, t := g.newTmp(), g.newTmp()
	fmt.Fprintf(b, "  %s = load i32, i32* %s\n", v, outp)
	fmt.Fprintf(b, "  %s = load i32, i32* %s\n", t, outt)
	// A double answer leaves the door as a heap box, and the door itself keeps no root for it: an
	// answer that is only part of a larger expression — the arm of another door — survived long enough
	// to be recycled by the next allocation, which is how `floorit(v) * 2 + modit(v)` printed `3.0` for
	// CPython's `7.5` while the same body printed the truth one door at a time. The registration is the
	// frame's own (ADR 0181), so it costs an entry that the frame close drops; an int payload pushed to
	// die with the frame is a word wasted, not a bug.
	fmt.Fprintf(b, "  call void @rt_root_put(i32* %s)\n", outp)
	g.rooted = true
	return v, t, true, nil
}

// raiseRuntimeMsg is setExn with a message the *target* wrote: the class and the frame come from the
// compiler, the bytes come from a buffer the runtime filled, and the raise leaves through the open
// handler or the function's raise-exit like any other (ADR 0228's "a built-in trap is a typed raise").
func (g *irGen) raiseRuntimeMsg(b *strings.Builder, class string, msgPtr string, sp Span) {
	g.raiseUsed = true
	b.WriteString("  store i32 1, i32* @exn_flag\n")
	fmt.Fprintf(b, "  store i32 %d, i32* @exn_code\n", exnCode(class))
	mv := g.newTmp()
	fmt.Fprintf(b, "  %s = load i8*, i8** %s\n", mv, msgPtr)
	fmt.Fprintf(b, "  store i8* %s, i8** @exn_msg\n", mv)
	if frame := g.raiseFrame(sp); frame != "" {
		fmt.Fprintf(b, "  store i8* %s, i8** @exn_frame\n", g.strConst(frame))
	} else {
		b.WriteString("  store i8* null, i8** @exn_frame\n")
	}
	if len(g.handlerStack) > 0 {
		b.WriteString("  br label %" + g.handlerStack[len(g.handlerStack)-1] + "\n")
	} else {
		b.WriteString("  br label %" + g.funcRaiseExit + "\n")
	}
}

// operandMayNeedPair is the side-effect-free question that decides whether to try the door at all: only
// a slot read whose kind lives in the object can hold a kind the compiler does not know, so a sum of
// literals, a double variable and an int variable stay on the ordinary road they have always used.
func (g *irGen) operandMayNeedPair(e Expr) bool { return g.arithWouldRefuse(e) }

// absOperandMayNeedPair widens that gate for the one signless call whose reference answer is always a
// number or a raise: an ordinary road that currently gives a float slot's payload a static `int` still has
// no tag, and the object already wrote one. The widening is confined to `abs` because `+`, `*` and `%` can
// answer values this backend does not build (Gap R.82), so their existing proof remains theirs (roadmap
// L11.1, Gap R.287).
func (g *irGen) absOperandMayNeedPair(e Expr) bool {
	return g.arithWouldRefuse(e) || g.entryReadNeedsTheTag(e)
}

// arithWouldRefuse is the cheap question the print dispatch asks before trying the tagged arithmetic
// door: would the ordinary numeric road have refused this expression? It is the same test
// slotReadRefusal makes when it writes "cannot reach into xs's slots" — the deepest subscript walks back
// to a container name this pass no longer has a literal story about. Anything else keeps the road it was
// already answered on, so landing this door cannot reroute a working program, and `/` is left out
// because true division has had its own door since ADR 0253.
func (g *irGen) arithWouldRefuse(e Expr) bool {
	switch n := e.(type) {
	case *BinOp:
		if n.Op == "/" {
			return false
		}
		return g.arithWouldRefuse(n.L) || g.arithWouldRefuse(n.R)
	case *UnOp:
		return n.Op == "-" && g.arithWouldRefuse(n.X)
	case *Call:
		// A popped slot is an arm whose kind the container chose: the removal reads the tag, and the
		// ordinary numeric road would use the payload as though the label were not there — which is how
		// `print(xs.pop(0) + 1)` answered 2 for the 2.5 that `n = xs.pop(0)` / `print(n + 1)` already got
		// right (roadmap L11.1, ADR 0410; the door is ADR 0265's).
		if g.popAnswerAsksTheArithmeticDoor(n) {
			return true
		}
		// A call whose answer is a pair is exactly what the ordinary numeric road would read as one word:
		// the callee stored the kind beside its own return (ADR 0273's answer direction), and the payload
		// alone is the truncated number Gap R.161 and Gap R.164 were measured on. So an arm that is such a
		// call may take the door — and an arm that is any other call keeps the road it has always had.
		if nm, ok := n.Fn.(*Name); ok {
			if nm.Value == "abs" && len(n.Args) == 1 {
				// abs of a pair is the signless call over a value whose kind the run time decides: the
				// reference answers a number or raises, so the door takes it whatever the slot holds —
				// the same rule `-x` follows, with its own sentence (roadmap L11.1, Gap R.146). A read whose
				// tag lives in the object is a pair here even when the ordinary numeric road still gives the
				// payload a static kind (roadmap Gap R.287).
				return g.arithWouldRefuse(n.Args[0]) || g.isPairElem(n.Args[0])
			}
			return g.pairRetDone[nm.Value]
		}
		return false
	case *Index:
		return g.indexKindIsRuntimeObject(n)
	case *Name:
		// A name the arithmetic door bound carries its kind in the object, and the ordinary numeric
		// road refuses it for exactly that reason — so the door is the road for it too. A name bound
		// from a SLOT asks the same door: the pair is already bound (ADR 0303), the tag says which of
		// the families the payload means, and `rt_num_arith` raises CPython's own sentence per kind when
		// the slot holds something without a sign. That widening is the one L11.1 owes this position
		// (Gap R.146); `+`, `*` and `%` stay behind the number proof below, because the reference ANSWERS
		// `"a" + "b"` and `[1] * 2` and this backend builds neither from a slot (Gap R.82).
		return g.numericPairVar(n.Value) || g.taggedVars[n.Value]
	}
	return false
}

// numericPairVar reports that a name's (payload, tag) pair came from the arithmetic door, whose target
// stores the int tag or the float tag and nothing else. That is a proof, not a guess: whatever the
// program does next, the payload means a number and the tag says which — so the positions that ask for
// one static number can be given the same door the expression itself was answered by, instead of the
// refusal that blamed the missing pair (roadmap L11.1, Gap R.143).
//
// A tagged name from another door is not this: a slot read bound by ADR 0241 and a loop variable bound
// by ADR 0185 can hold a text or None, and opening the `+`/`*` gate for them would raise where the
// reference answers `"a" + "b"` (Gap R.82).
func (g *irGen) numericPairVar(name string) bool {
	if !g.taggedVars[name] || g.taggedOrigin == nil {
		return false
	}
	switch g.taggedOrigin[name] {
	case taggedOriginArith, taggedOriginFloat, taggedOriginParam, taggedOriginParamArith:
		return true
	}
	return false
}

// numericPairRegs loads the two words a pair-bound name lives in.
func (g *irGen) numericPairRegs(b *strings.Builder, name string, sp Span) (string, string) {
	// The same rule as taggedLoopVarRead: a name that holds a pair is read by its two allocas, and a
	// slot no path wrote must not answer either of them. CheckBound is a no-op for a name the checker
	// proved was written, so this costs nothing on the road every program walks (roadmap Gap R.68).
	g.checkBound(b, name, sp)
	p, t := g.newTmp(), g.newTmp()
	fmt.Fprintf(b, "  %s = load i32, i32* %%%s\n", p, "_"+name)
	fmt.Fprintf(b, "  %s = load i32, i32* %%%s_tag\n", t, "_"+name)
	return p, t
}

// numericPairLift answers the question every plain-number position asks about a name the arithmetic
// door bound: *give me the number*. The name holds a (payload, tag) pair, because the objects decided
// its kind at the moment the arithmetic ran, and `@rt_lift_num` is the one place that turns such a pair
// into the word that holds both families — unboxing a float payload, widening an int or a bool one.
//
// It returns "" when the expression is not such a name, which leaves the caller on the road it has
// always taken, refusal included: this is an extra answer, not a substitute for one (roadmap L11.1,
// Gap R.143).
func (g *irGen) numericPairLift(b *strings.Builder, e Expr) string {
	nm, ok := e.(*Name)
	if !ok || !g.numericPairVar(nm.Value) {
		return ""
	}
	p, t := g.numericPairRegs(b, nm.Value, nm.Span())
	return g.liftPair(b, p, t)
}

// numericExprPair is the same answer for the slightly wider shape a position meets in the wild: the
// name itself, or arithmetic over it. `print(n * 2 > 4)` asks an ordering about a product, and the
// product is what the pair door already knows how to build.
func (g *irGen) numericExprPair(b *strings.Builder, e Expr) (string, string, bool, error) {
	if nm, ok := e.(*Name); ok && g.numericPairVar(nm.Value) {
		p, t := g.numericPairRegs(b, nm.Value, nm.Span())
		return p, t, true, nil
	}
	// A popped slot is NOT answered here. The road it needs is the one `arithWouldRefuse` above already
	// points at — `taggedArithPair`, the arm of ADR 0265's door that lifts the (payload, tag) pair AND
	// refuses the mixed-kind sums the reference refuses (`Gap R.296`: a text against a number must raise,
	// never say `False`). An arm that answered the pair directly here kept the ordering's own refuse arm
	// out of reach, and `xs = [1, "a"]` / `print(xs.pop(0) > 1)` said False at exit 0 where the reference
	// raises TypeError (roadmap L11.1, ADR 0410).
	if g.arithWouldRefuse(e) {
		return g.taggedArithPair(b, e)
	}
	return "", "", false, nil
}

// staticNumberDouble lowers the *other* side of an ordering against a pair-bound name: a number the
// compiler can read is widened into the word the pair lifts into, and anything it cannot read (a text,
// a container, a call whose kind it cannot settle) declines the door and keeps the road — and the
// refusal — it has always had.
func (g *irGen) staticNumberDouble(b *strings.Builder, e Expr) (string, bool) {
	switch n := e.(type) {
	case *IntLit:
		r := g.newTmp()
		fmt.Fprintf(b, "  %s = sitofp i32 %d to double\n", r, n.Value)
		return r, true
	case *BoolLit:
		v := "0"
		if n.Value {
			v = "1"
		}
		r := g.newTmp()
		fmt.Fprintf(b, "  %s = sitofp i32 %s to double\n", r, v)
		return r, true
	case *FloatLit:
		return floatConst(n.Value), true
	}
	if g.isFloat(e) {
		d := g.floatValue(b, e)
		return d, d != ""
	}
	if nm, ok := e.(*Name); ok && !g.taggedVars[nm.Value] && !isStringExpr(e) && !g.isNoneExpr(e) && !g.isContainerExpr(e) {
		v, err := g.value(b, e)
		if err != nil || v == "" {
			return "", false
		}
		r := g.newTmp()
		fmt.Fprintf(b, "  %s = sitofp i32 %s to double\n", r, v)
		return r, true
	}
	return "", false
}

// pairOrder is the ordering a pair-bound name takes part in: `while n > 0:`, `print(n >= 2)`,
// `1 if n > 1 else 0`. The tag decides which family the name's side is, and both sides are lifted to
// the word that holds both — an ordering of two numbers, never of two representations. The tag cannot
// say anything else for this shape (the only door that wrote it raises before storing a non-numeric
// tag), so there is no cross-kind sentence to write here and nothing to refuse a text: a text on the
// other side is not an ordering the reference answers with a number either, and it stays with the door
// that raises CPython's own words (roadmap L11.1, Gap R.143).
func (g *irGen) pairOrder(b *strings.Builder, n *BinOp) (string, bool, error) {
	op, ok := map[string]string{"<": "fcmp olt", "<=": "fcmp ole", ">": "fcmp ogt", ">=": "fcmp oge"}[n.Op]
	if !ok {
		return "", false, nil
	}
	lp, lt, lPair, err := g.numericExprPair(b, n.L)
	if err != nil {
		return "", false, err
	}
	rp, rt, rPair, err := g.numericExprPair(b, n.R)
	if err != nil {
		return "", false, err
	}
	if lPair == rPair { // both or neither: the ordinary road and the tagged-order door own it
		return "", false, nil
	}
	var ld, rd string
	if lPair {
		ld = g.liftPair(b, lp, lt)
		var okR bool
		rd, okR = g.staticNumberDouble(b, n.R)
		if !okR {
			return "", false, nil
		}
	} else {
		rd = g.liftPair(b, rp, rt)
		var okL bool
		ld, okL = g.staticNumberDouble(b, n.L)
		if !okL {
			return "", false, nil
		}
	}
	cmp := g.newTmp()
	fmt.Fprintf(b, "  %s = %s double %s, %s\n", cmp, op, ld, rd)
	return g.markI1(cmp), true, nil
}

// liftPair is the one instruction every consumer of a pair needs and no position should reinvent.
func (g *irGen) liftPair(b *strings.Builder, payload, tag string) string {
	g.heapUsed = true
	g.arithUsed = true
	d := g.newTmp()
	fmt.Fprintf(b, "  %s = call double @rt_lift_num(i32 %s, i32 %s)\n", d, payload, tag)
	return d
}

// recordSlotOrigin says where a name's pair came from when the door that bound it read a slot. The
// origin is what lets a later refusal name the truth (taggedVarErr); a binding that leaves it unset
// falls through to a sentence that has to guess, and the guess it used to make — "comes from a loop
// over a mixed list" — was false for every program with no loop in it (roadmap Gap R.38).
func (g *irGen) recordSlotOrigin(name string) {
	if g.taggedOrigin == nil {
		g.taggedOrigin = map[string]string{}
	}
	if g.taggedOrigin[name] != "" {
		return
	}
	g.taggedOrigin[name] = taggedOriginSlot
}

// arithReadsAParameter reports that an expression reaches its number through a parameter rather than
// through a container: the same walk the pair road asks its operands with, looking for a name the frame
// received. Parameters only — a name the body assigned is that body's own story, and the sentence for it
// already exists above.
func (g *irGen) arithReadsAParameter(e Expr, params map[string]string) bool {
	if len(params) == 0 {
		return false
	}
	// The body's own bindings, so the chain below can leave a name and ask what wrote it. Built the same
	// way ADR 0274's return convention builds it, from the function being emitted.
	bindings := map[string][]Expr{}
	if fd := g.fds[g.curFunc]; fd != nil {
		scanRebinds(fd.Body, bindings)
	}
	// The chain matters: `y = x + 1` / `z = y * 2` reads its number through `y`, which the body bound, and
	// `y` reads it through the parameter. Stopping at the first name reports the second binding as a
	// run-time container — which is the false claim Gap R.38 files, in a program whose only source is an
	// argument. `through` is the set of names already being asked about, so a `y = y + 1` cannot spin.
	var seen func(Expr, map[string]bool) bool
	seen = func(x Expr, through map[string]bool) bool {
		switch n := x.(type) {
		case *Name:
			if _, isParam := params[n.Value]; isParam {
				return true
			}
			if through[n.Value] {
				return false
			}
			through[n.Value] = true
			vals, ok := bindings[n.Value]
			if !ok {
				return false
			}
			for _, v := range vals {
				if seen(v, through) {
					return true
				}
			}
			return false
		case *BinOp:
			return seen(n.L, through) || seen(n.R, through)
		case *UnOp:
			return seen(n.X, through)
		case *CondExpr:
			return seen(n.If, through) || seen(n.Else, through)
		}
		return false
	}
	return seen(e, map[string]bool{})
}

// indexKindIsRuntimeObject walks a chained subscript back to the name it reads through and asks whether
// that name is a container the analysis has lost: allocated, a container by kind, and no longer
// described by the literal that built it. That is precisely the set of names whose numeric read is
// refused today, which is why it is safe to use as the gate.
func (g *irGen) indexKindIsRuntimeObject(ix *Index) bool {
	for {
		switch o := ix.Obj.(type) {
		case *Index:
			ix = o
		case *Name:
			if !g.allocd[o.Value] || !g.containerNamed(o.Value) {
				return false
			}
			_, provable := g.containerLits[o.Value]
			return !provable
		default:
			return false
		}
	}
}

// arithOperandPair turns one operand into the (payload, tag) pair `@rt_num_arith` asks about. A literal
// carries its own kind as a constant; a double variable is boxed on the way in (the same handle a float
// slot holds, so the helper's one lift serves both); a tagged variable already is the pair; a slot read
// comes from the doors that read payload and tag together. Anything else declines.
func (g *irGen) arithOperandPair(b *strings.Builder, e Expr) (pl, tg string, ok bool, err error) {
	switch n := e.(type) {
	case *IntLit:
		return strconv.FormatInt(n.Value, 10), "0", true, nil
	case *BoolLit:
		if n.Value {
			return "1", "2", true, nil
		}
		return "0", "2", true, nil
	case *FloatLit:
		g.heapUsed = true
		h := g.newTmp()
		g.emitRtFloatNew(b, h, floatConst(n.Value), n.Span())
		return h, "1", true, nil
	case *NoneLit:
		return "0", "3", true, nil
	case *StrLit:
		v, verr := g.value(b, e)
		if verr != nil || v == "" {
			return "", "", false, verr
		}
		return v, "4", true, nil
	case *UnOp:
		// The negation of a literal is the literal with its sign taken at compile time — the pair the
		// operand needs is a constant, and asking the target for `-1` costs an instruction for nothing.
		if n.Op != "-" {
			return "", "", false, nil
		}
		switch v := n.X.(type) {
		case *IntLit:
			return strconv.FormatInt(-v.Value, 10), "0", true, nil
		case *FloatLit:
			g.heapUsed = true
			hn := g.newTmp()
			g.emitRtFloatNew(b, hn, floatConst(-v.Value), Span{})
			return hn, "1", true, nil
		}
		return "", "", false, nil
	case *Name:
		if g.taggedVars[n.Value] {
			g.checkBound(b, n.Value, n.Span()) // the pair may not have been written (Gap R.68, ADR 0228)
			v, t := g.newTmp(), g.newTmp()
			fmt.Fprintf(b, "  %s = load i32, i32* %%_%s\n", v, n.Value)
			fmt.Fprintf(b, "  %s = load i32, i32* %%_%s_tag\n", t, n.Value)
			return v, t, true, nil
		}
		if g.isFloat(e) || g.isFloatNumericOperand(e) {
			d := g.floatValue(b, e)
			if d == "" {
				return "", "", false, nil
			}
			g.heapUsed = true
			h := g.newTmp()
			g.emitRtFloatNew(b, h, d, Span{})
			return h, "1", true, nil
		}
		v, verr := g.value(b, e)
		if verr != nil || v == "" {
			return "", "", false, verr
		}
		return v, "0", true, nil
	case *BinOp:
		// An arithmetic operand is either the pair the one door answers, or a plain number the ordinary
		// numeric road already values and which needs no tag beyond 0: `(a + b) + c` over
		// `total(1, 2, 3.5)` is that shape, where the left half is entirely the ordinary road's business
		// and only the outer operation has to ask a tag (roadmap L11.6, Gap P.1).
		if p, t, okPair, pairErr := g.taggedArithPair(b, e); pairErr != nil {
			return "", "", false, pairErr
		} else if okPair {
			return p, t, true, nil
		}
		if g.isFloat(e) || g.isFloatNumericOperand(e) {
			d := g.floatValue(b, e)
			if d == "" {
				return "", "", false, nil
			}
			g.heapUsed = true
			hb := g.newTmp()
			g.emitRtFloatNew(b, hb, d, Span{})
			return hb, "1", true, nil
		}
		if v, verr := g.value(b, e); verr == nil && v != "" && !g.isFloat(e) {
			return v, "0", true, nil
		} else if verr != nil {
			return "", "", false, verr
		}
	case *Call:
		// A *call*'s answer as an arm of a pair expression: the callee stored the kind beside its own
		// return, so the pair is read with the same door a print or a binding reads it with — the shape
		// that left `return (v // 2) + other(v)` refusing while `return other(v)` answered (roadmap
		// Gap R.164's arm half, ADR 0280). The permission is this arm's own, the rule the door already
		// keeps for a pair-returning call in an argument: an arm the scan did not judge pair-returning is
		// not read here and keeps the answer, or the refusal, it always had.
		if p, t, okPair, pairErr := g.pairCallPair(b, e); pairErr != nil {
			return "", "", false, pairErr
		} else if okPair {
			return p, t, true, nil
		}
		if g.arithWouldRefuse(n) {
			// An operand that is itself the door's answer — `abs(n) - 1`, `abs(n) * 2` — asks the same door
			// for its two words. The recursion terminates because the door is asked of the call's ARGUMENT,
			// never of the call (roadmap L11.1, Gap R.146; ADR 0265's door).
			p, t, okPair, arithErr := g.taggedArithPair(b, e)
			if arithErr != nil {
				return "", "", false, arithErr
			}
			if okPair {
				return p, t, true, nil
			}
		}
	case *Index:
		if v, t, okPair := g.runtimeSlotPairDeep(b, e); okPair {
			return v, t, true, nil
		}
		if v, t, okPair := g.taggedSlotPair(b, n); okPair {
			return v, t, true, nil
		}
		if v, t, okPair := g.runtimeSlotPair(b, n); okPair {
			return v, t, true, nil
		}
	}
	return "", "", false, nil
}

// slotLiteralMayBeNumber reports whether an operand written as a literal could ever be anything but a
// number. It is the narrow half of the `+` and `*` gate (ADR 0265): the operand a slot is paired with
// has to be a number too, because "a" + xs[0] and [1] * xs[0] are answered by the reference with a
// joined text and a repeated list, and this backend can build neither from a slot (Gap R.82).
func slotLiteralMayBeNumber(e Expr) bool {
	switch n := e.(type) {
	case *StrLit, *FString:
		return false
	case *ListLit, *DictLit, *SetLit, *Tuple:
		return false
	case *BinOp:
		return slotLiteralMayBeNumber(n.L) && slotLiteralMayBeNumber(n.R)
	case *UnOp:
		return slotLiteralMayBeNumber(n.X)
	case *CondExpr:
		return slotLiteralMayBeNumber(n.If) && slotLiteralMayBeNumber(n.Else)
	default:
		return true
	}
}

// operandIsASpelledNumber reports the narrow fact the `+` gate needs: the operand is a number written in
// the source, so its kind is not a run-time question at all — and an operator with such a number on one
// side can answer a number or raise, whatever the other side holds. Literals only: a NAME the notebook
// calls a number may have been rebound, which is the reason ADR 0243's numeric courtesy keeps to literals
// too.
func (g *irGen) operandIsASpelledNumber(e Expr) bool {
	switch n := e.(type) {
	case *IntLit, *FloatLit, *BoolLit:
		return true
	case *UnOp:
		return n.Op == "-" && g.operandIsASpelledNumber(n.X)
	}
	return false
}

// plusBesideASpelledNumber is the `+` arm of ADR 0265's gate, widened by one operand kind: exactly one
// side is the slot read that needs the pair, and the other is a number the source spells. The widening is
// bounded by what the pair road can ANSWER: the side that is not the number must name a value the tagged
// road knows a story for — a number, a slot the module never pinned, or a nothing/bool/text/list/dict/set,
// each of which the road either adds or raises CPython's own sentence for. A class, an instance or a
// function is not let in: its int half is a pointer, and adding that pointer to a literal and calling the
// sum an answer is worse than the refusal the door used to give (`C() + 1` answering 1 where the reference
// raises). roadmap Gap R.442.
func (g *irGen) plusBesideASpelledNumber(l, r Expr) bool {
	ln, rn := g.operandMayNeedPair(l), g.operandMayNeedPair(r)
	if ln == rn {
		return false
	}
	if !(ln && g.operandIsASpelledNumber(r)) && !(rn && g.operandIsASpelledNumber(l)) {
		return false
	}
	// Whichever side is not the spelled number is the one the pair road has to answer for.
	other := r
	if ln {
		other = l
	}
	return g.pairRoadAnswersThisOperand(other)
}

// pairRoadAnswersThisOperand asks, of the operand that is not the spelled number, whether the tagged
// pair road has an answer for it: an int or float adds, a slot the module never pinned answers from the
// tag at run time, and a text, list, dict, set or the nothing are the road's own run-time raise. A value
// with NO number half — a class, an instance, a function — has neither, and the road would add the
// heap pointer the value keeps in its int half to the literal and call the sum an answer: `class C: pass`
// / `print(C() + 1)` came out 1 for a reference TypeError. Those three stay behind the door that already
// refuses them by name (roadmap Gap R.442).
func (g *irGen) pairRoadAnswersThisOperand(e Expr) bool {
	switch n := e.(type) {
	case *Name:
		if g.declaresTheName(n.Value) {
			return false // `f` names a function, `C` names a class
		}
		if cls := g.varClasses[n.Value]; cls != "" {
			return false // `i` names an instance of C
		}
		return true
	case *Call:
		if fn, ok := n.Fn.(*Name); ok && g.declaresTheName(fn.Value) {
			return false // `C(...)` is an instance being built
		}
		return true
	}
	return true
}

// declaresTheName is the top-level declaration fact: the module writes this name with `def` or `class`,
// so it names a function or a class object rather than a value with a number half.
func (g *irGen) declaresTheName(name string) bool {
	if g.program == nil {
		return false
	}
	for _, st := range g.program.Stmts {
		switch d := st.(type) {
		case *ClassDef:
			if d.Name == name {
				return true
			}
		case *FuncDef:
			if d.Name == name {
				return true
			}
		}
	}
	return false
}

// slotArithmeticIsProven answers the gate's question for one operand of a `+` or a `*`: is this a read
// of a slot the program is *shown* to hold only numbers? A read of a container the notebook has nothing
// to say about is answered no, and so is an operand that is neither a slot read nor something the
// ordinary numeric road can already value.
func (g *irGen) slotArithmeticIsProven(e Expr) bool {
	if !g.operandMayNeedPair(e) {
		// Not a slot read at all: the road this operand always took is the one it takes now.
		return true
	}
	if nm, ok := e.(*Name); ok && g.numericPairVar(nm.Value) {
		// The operand is a name the arithmetic door bound, so its kind was decided by the objects that
		// produced it and there is no container left to prove anything about.
		return true
	}
	if c, isCall := e.(*Call); isCall {
		// A call whose answer is a pair: the callee stored the kind beside its own return, and that word
		// says number — no container is involved, and the same walk that proved a parameter's arm proves
		// this one (roadmap Gap R.164's arm half, ADR 0280). Any other call is the ordinary road's.
		if nm, isName := c.Fn.(*Name); isName && g.pairRetDone[nm.Value] {
			return true
		}
		// A popped slot IS a slot of that container, one level deep, so it is proven by the same
		// program-wide walk that proves `xs[0]`'s number-ness (ADR 0265's proof, re-keyed by ADR 0322):
		// the sum takes the tagged door exactly when no text and no container can reach the list's slots,
		// and keeps the ordinary road when one can. What it must not do is answer 2 where the element is
		// 1.5 — the payload's being an i32 is not a proof that the value is an int.
		if listName, _, serves := g.popAnswerShape(c); serves {
			return g.numChains != nil && g.numChains.numeric(listName, 1)
		}
	}
	switch n := e.(type) {
	case *BinOp:
		// An arm of a pair expression is proven when every leaf under it is. The pair the door combines is
		// built from those leaves, so "can a slot this pass cannot see reach this operand?" has to be
		// walked rather than answered of the top node — declining the whole expression because its left arm
		// is itself a sum is what left `(v - 1) * 2`, `v + 1 + 1` and ADR 0216's flooring identity on the
		// ordinary road, which has one word for a parameter (roadmap Gap R.166, ADR 0278's owed half).
		return g.slotArithmeticIsProven(n.L) && g.slotArithmeticIsProven(n.R)
	case *UnOp:
		return n.Op == "-" && g.slotArithmeticIsProven(n.X)
	case *CondExpr:
		return g.slotArithmeticIsProven(n.If) && g.slotArithmeticIsProven(n.Else)
	}
	nm, depth := chainDepth(e)
	if nm == nil || depth < 1 || g.numChains == nil {
		return false
	}
	// `numeric` carries the blocking half of the gate with it: a text (or a value with no spelling)
	// written into this container — or into any name that can denote it, per the alias closure the pass
	// builds — closes the door for the container and for nothing else (ADR 0265, re-keyed by ADR 0322
	// over roadmap Gap R.191).
	return g.numChains.numeric(nm.Value, depth)
}

// nameHoldsContainer is the element that is a container the builder cannot SEE but the compiler
// knows: a NAME whose last binding was a container, which the slot stores as the object's handle and
// whose tag `elemKindTag` already reads off the runtime tables. The census asked the element's
// SPELLING, and a name has none, so `sa = {1: "a"}` / `print([sa, 1])` built an object whose slots
// carried (handle, TagDict) and whose header claimed one element kind — and rt_print_list printed the
// handle as a number: `[1, 1]` at exit 0 where the reference prints `[{1: 'a'}, 1]` (roadmap
// **Gap R.486**, beside **ADR 0359**'s identical fix for the interpolation element and **ADR 0232**'s
// rule that the slots the builder wrote are the truth about the object). The same reading the tag
// takes, asked once, so the tag and the census cannot disagree about one element.
func (g *irGen) elementNameHoldsContainer(x Expr) bool {
	nm, isName := x.(*Name)
	if !isName || g == nil {
		return false
	}
	// The tag road's own answer, asked once. Reading the container TABLES here instead would claim the
	// slot of a name the ITERATION rebound: `xs = [7]` / `for xs in [1, 2]:` / `print([xs])` is a list of
	// two integers, and a census that kept believing the first binding printed `[]` for it — so the
	// census asks `elemKindTag`, which reads the loop item first and the name's container history only
	// after it (roadmap Gap R.358, ADR 0232's one question).
	t, ok := g.elemKindTag(nm)
	if !ok {
		return false
	}
	return t == int32(TagList) || t == int32(TagDict) || t == int32(TagSet)
}

// compSlotIsContainer is the element door's answer for the slot that holds a COMPREHENSION: the word
// the slot carries is the handle of the object the comprehension builds, and the brace the program
// wrote around it says which of the three that object is — a list, a dict or a set.
//
// The BUILD door has known this since **ADR 0192**: `containerOperand` and `containerHandleOf` each
// carry a `*Comp` arm, and the constant-folded spelling is materialised into the heap precisely because
// **ADR 0188** forbade handing `@.lst1` to a runtime helper whose argument is an `i32`. The doors that
// DECIDE never grew the arm. `nestedContainerElem` stopped at the three literal spellings and a name,
// so `heapElemKind` fell past the container road to the ordinary value road — which for a folded
// comprehension answers the name of a compile-time GLOBAL, and `rt_set_elem(i32 %h, i32 0, i32 @.lst1)`
// is the instruction `llc` rejects (exit **2**, the code **ADR 0166** reserves for a compiler bug, paid
// for `print([[x for x in [1]], 1])` — roadmap **Gap R.488**). `elemKindTag` had no `*Comp` case
// either, so the spelling that did reach the runtime builder stored a good handle beside the blanket
// `TagInt` and printed `[1, 1]` at the exit code of success for `print([[{'a': 1} …], 1])` — roadmap
// **Gap R.487**. One construct, two faces, and the same disagreement **ADR 0249** names: a gate that
// asks a different question from the door it gates.
//
// The taggability of the comprehension's CONTENTS is asked of the same door the literals ask
// (`taggableNestedElem`, through `compContentsTaggable`), so a comprehension whose element nests deeper
// than any slot can be tagged for declines here exactly as the literal of the same contents declines —
// and `heapElemKind`'s own door keeps the refusal sentence rather than emitting an operand nobody can
// lower.
func (g *irGen) compSlotIsContainer(e Expr) (int32, bool) {
	c, isComp := e.(*Comp)
	if !isComp || g == nil {
		return 0, false
	}
	if !g.compContentsTaggable(c) {
		return 0, false
	}
	switch c.Kind {
	case CompList:
		return int32(TagList), true
	case CompSet:
		return int32(TagSet), true
	case CompDict:
		return int32(TagDict), true
	}
	// A generator expression is not a container: it is the iterator the reference hands back, whose
	// state no slot of this backend represents (roadmap Gap R.195). It keeps the road it has today.
	return 0, false
}

// compIsContainerSlotExpr is the census's and the store's reading of the question above: does this
// element hold a container's handle. `literalNeedsTags` and `literalNeedsHeap` ask it so the OBJECT
// says its slots are the truth, `nestedContainerElem` asks it so the WORD is a materialised handle,
// and `elemKindTag` asks the same predicate for the LABEL — three doors, one reading, so a later
// widening of one cannot silently disagree with the other two (**ADR 0232**'s one question, the rule
// **ADR 0438** applied to the container held in a name).
func (g *irGen) compIsContainerSlotExpr(e Expr) bool {
	_, ok := g.compSlotIsContainer(e)
	return ok
}

// compContentsTaggable asks whether every slot of the object a comprehension builds can carry a tag,
// by asking the question of the literal with the same contents. The recursion is safe: a comprehension
// written INSIDE the element list is a smaller expression than the one being asked, so each level
// terminates on the literals, and the `inner` arm of `taggableNestedElem` reaches it through
// `elemKindTag`'s `*Comp` case.
func (g *irGen) compContentsTaggable(c *Comp) bool {
	if g == nil || c == nil {
		return false
	}
	switch c.Kind {
	case CompList:
		return g.taggableNestedElem(&ListLit{Elems: c.Elems, Src: c.Src})
	case CompSet:
		return g.taggableNestedElem(&SetLit{Elems: c.Elems, Src: c.Src})
	case CompDict:
		return g.taggableNestedElem(&DictLit{Keys: c.Keys, Vals: c.Vals, Src: c.Src})
	}
	return false
}
