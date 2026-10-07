package lang

import "sort"

// String parameters in the AOT backend (roadmap Gap J.5).
//
// A string in this compiler is a compile-time global (@.strN), while a function parameter is
// an i32 slot, so `shout("hi")` used to emit `call i32 @shout(i32 @.str1)` and LLVM rejected
// the module — reporting valid code as a compiler bug. Gap I.2 put strings in a runtime
// interned table (@str_tab / @str_repr_tab, addressed by index), which makes a string
// parameter representable: the argument is interned at the call site and the callee receives
// the index.
//
// The callee has to *know* which of its parameters are string indices, so this file works out
// which parameters receive strings — from a `str` annotation, a string default, or a call
// site that passes a string. The rules mirror heapargs.go's container-parameter inference
// (and reuse its walker): annotations and defaults are authoritative, call sites add
// evidence, and classification runs to a fixed point in a deterministic (sorted) order,
// because `def outer(s): inner(s)` must not classify differently depending on Go's map
// iteration order.

// strArgKinds maps "function name" -> set of parameter indices that receive strings.
func strArgKinds(prog *Program) map[string]map[int]bool {
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

	w := heapASTWalker{}

	// Which plain variables hold a string? A name ever assigned a non-string is dropped from
	// propagation, so a parameter called `s` cannot drag an unrelated numeric `s` along.
	strVars := map[string]bool{}
	notString := map[string]bool{}
	w.assign = func(scope, target string, value Expr, annot *Type) {
		key := scopeKey(scope, target)
		if isStringAnnotation(annot) || isStringExpr(value) {
			strVars[key] = true
			return
		}
		if value != nil {
			notString[key] = true
		}
	}

	// Evidence that a parameter is *not* a string: the same helper called with a number on
	// another path. Without it `f("str")` alone would mark the parameter, and the unrelated
	// `f(7)` would then be printed through the string table — "(null)" for an out-of-range
	// index, which is worse than refusing to compile.
	notStrArg := map[string]map[int]bool{}
	out := map[string]map[int]bool{}
	mark := func(fn string, idx int) {
		if idx < 0 {
			return
		}
		m := out[fn]
		if m == nil {
			m = map[int]bool{}
			out[fn] = m
		}
		m[idx] = true
	}
	w.call = func(scope, fn string, args []Expr) {
		fd, ok := funcs[fn]
		if !ok {
			return
		}
		neg := func(idx int) {
			if idx < 0 {
				return
			}
			m := notStrArg[fn]
			if m == nil {
				m = map[int]bool{}
				notStrArg[fn] = m
			}
			m[idx] = true
		}
		pos := 0
		for _, a := range args {
			if kw, ok := a.(*KeywordArg); ok {
				for i, p := range fd.Params {
					if p.Name != kw.Name {
						continue
					}
					if isStringExpr(kw.Value) {
						mark(fn, i)
					} else if definitelyNotAString(kw.Value) {
						neg(i)
					}
				}
				continue
			}
			if isStringExpr(a) {
				mark(fn, pos)
			} else if nm, ok := a.(*Name); ok && scopeHasString(strVars, notString, scope, nm.Value) {
				// forwarding a variable that is known to carry a string
				mark(fn, pos)
			} else if definitelyNotAString(a) {
				neg(pos)
			}
			pos++
		}
	}

	seed := func() {
		fns := make([]string, 0, len(funcs))
		for fn := range funcs {
			fns = append(fns, fn)
		}
		sort.Strings(fns)
		for _, fn := range fns {
			fd := funcs[fn]
			for i, p := range fd.Params {
				if p == nil {
					continue
				}
				if isStringAnnotation(p.Annot) {
					mark(fn, i)
				} else if isStringExpr(p.Default) {
					mark(fn, i)
				}
			}
		}
		w.stmts(prog.Stmts)
	}

	// Fixed point: a parameter known to receive a string makes its own *variable* a string, so
	// a forwarding call discovered later marks the callee's parameter too.
	for pass := 0; pass < 8; pass++ {
		before := snapshotStrKinds(out)
		fns := make([]string, 0, len(funcs))
		for fn := range funcs {
			fns = append(fns, fn)
		}
		sort.Strings(fns)
		for _, fn := range fns {
			fd := funcs[fn]
			m := out[fn]
			idxs := make([]int, 0, len(m))
			for i := range m {
				idxs = append(idxs, i)
			}
			sort.Ints(idxs)
			for _, i := range idxs {
				if i >= len(fd.Params) {
					continue
				}
				name := fd.Params[i].Name
				if !notString[scopeKey(fn, name)] && !notString[scopeKey("", name)] {
					strVars[scopeKey(fn, name)] = true
				}
			}
		}
		seed()
		if snapshotStrKinds(out) == before {
			break
		}
	}

	// A parameter annotated as a container is never a string, and a parameter that is also
	// demonstrably used as a number stays unsupported rather than silently treating the number
	// as a string-table index.
	for fn, m := range out {
		for i := range m {
			if notStrArg[fn][i] {
				delete(m, i)
			}
		}
	}
	containerParams := heapArgKinds(prog)
	for fn, m := range out {
		fd := funcs[fn]
		for i := range m {
			if i >= len(fd.Params) || fd.Params[i] == nil {
				continue
			}
			name := fd.Params[i].Name
			if containerParams[fn][i] != HeapNone {
				delete(m, i)
				continue
			}
			if notString[scopeKey(fn, name)] && !isStringAnnotation(fd.Params[i].Annot) {
				delete(m, i)
			}
		}
		if len(m) == 0 {
			delete(out, fn)
		}
	}
	return out
}

// snapshotStrKinds renders the inference result as a string so the fixed point can compare
// rounds without a bespoke deep-equal.
func snapshotStrKinds(m map[string]map[int]bool) string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	buf := ""
	for _, k := range keys {
		idxs := make([]int, 0, len(m[k]))
		for i := range m[k] {
			idxs = append(idxs, i)
		}
		sort.Ints(idxs)
		buf += k + "("
		for _, i := range idxs {
			buf += itoa(i) + ","
		}
		buf += ")"
	}
	return buf
}

// isStringAnnotation reports a `str` parameter annotation, the declaration that settles the
// question without looking at call sites.
func isStringAnnotation(t *Type) bool {
	return t.IsString()
}

// strReturningFuncs reports the functions whose `return` yields a string — a string literal,
// an interpolated literal, a string parameter, or a call to another such function. The caller
// needs this to print `echo("yo")` as text rather than as the index the function returns, and
// to store `make_key()` in a container as an interned element (roadmap Gap J.5).
func strReturningFuncs(prog *Program) map[string]bool {
	out := map[string]bool{}
	if prog == nil {
		return out
	}
	funcs := map[string]*FuncDef{}
	order := []string{}
	for _, st := range prog.Stmts {
		if fd, ok := st.(*FuncDef); ok {
			if _, seen := funcs[fd.Name]; !seen {
				order = append(order, fd.Name)
			}
			funcs[fd.Name] = fd
		}
	}
	// A string parameter makes `return s` a string return without any literal involved.
	strParams := strArgKinds(prog)
	// The names the program defines for itself, so a program's own `def str(x): return x + 7` is not
	// read as the builtin (Gap R.6's rule, ADR 0199: a fold through a builtin's meaning is only allowed
	// where the program did not take the name).
	defines := map[string]bool{}
	for _, st := range prog.Stmts {
		if fd, ok := st.(*FuncDef); ok {
			defines[fd.Name] = true
		}
	}
	// Declared before its own body so the name-bound case can ask it of a binding value — a closure
	// assigned with `:=` cannot reach its own name in its initializer (roadmap Gap R.170, ADR 0286).
	var isStrExprIn func(fd *FuncDef, e Expr) bool
	isStrExprIn = func(fd *FuncDef, e Expr) bool {
		if e == nil || isStringExpr(e) {
			return e != nil && isStringExpr(e)
		}
		switch v := e.(type) {
		case *Name:
			for i, p := range fd.Params {
				if p.Name == v.Value && strParams[fd.Name][i] {
					return true
				}
			}
			// A name the body bound to a rendering is a text too: `s = str(v)` / `return s` is the same
			// door two statements earlier, and the caller cannot tell the difference. ADR 0281 marked
			// `return str(42)` string-returning, but the body that binds the rendering to a name first
			// still answered a number to its callers — `print(f(3))` passed the interned index to `printf`
			// with `%d` and printed `0` at exit 0, and `"x" + str(v)` / `return s` printed `2` for `x3`
			// (roadmap L11.2, Gap R.170, ADR 0286). The two-hop question is what ADR 0285's number half
			// asks too: the value's kind is written by an assignment, not by the returned expression.
			for _, val := range bodyBindings(fd)[v.Value] {
				if isStrExprIn(fd, val) {
					return true
				}
			}
		case *Call:
			if nm, ok := v.Fn.(*Name); ok {
				// `return str(42)` renders a value to text. The body already asks the statement-level
				// rendering door for it and returns the interned index (ADR 0258's `rt_str_intern2`), so the
				// callee's half was right and only this verdict was missing: the caller did not know the
				// answer is a text, and `print(g())` passed the index to `printf` with `%d` — `0` at exit 0
				// where CPython and the interpreter print `42` (roadmap L11.2, Gap R.163, ADR 0281).
				if !defines[nm.Value] && (nm.Value == "str" || nm.Value == "repr") && len(v.Args) == 1 {
					return true
				}
				return out[nm.Value]
			}
			// A method's `return str(...)` is the same verdict: the callee emits the same door, and the
			// caller's print dispatch asks `callReturnsStr`, which reads this table.
			return false
		case *BinOp:
			// "a" + x  /  x + "a" is a string; the codegen refuses it until the runtime can
			// allocate, but the *intent* is a string, so the caller must not treat it as an int.
			// concat is refused by codegen until the runtime can allocate; keeping the
			// "it is a string" verdict here means the caller never treats it as an int.
			return isConcat(v)
		}
		return false
	}
	// Round until stable: `def a(s): return s` and `def b(s): return a(s)` reference each other.
	for pass := 0; pass < 6; pass++ {
		changed := false
		for _, name := range order {
			fd := funcs[name]
			if out[name] {
				continue
			}
			w := returnCollector{}
			w.stmts(fd.Body)
			for _, r := range w.returns {
				if isStrExprIn(fd, r) {
					out[name] = true
					changed = true
					break
				}
			}
		}
		if !changed {
			break
		}
	}
	return out
}

// bodyBindings is the body's own assignment table (name -> every value ever bound to it), built once per
// question so a returned name can be traced back to what wrote it. `scanRebinds` is the same table ADR
// 0274's return convention and ADR 0285's number half read; caching it keeps a `y = y + 1` body from
// rebuilding it on every hop.
var bodyBindingCache = map[*FuncDef]map[string][]Expr{}

func bodyBindings(fd *FuncDef) map[string][]Expr {
	if fd == nil {
		return map[string][]Expr{}
	}
	if m, ok := bodyBindingCache[fd]; ok {
		return m
	}
	m := map[string][]Expr{}
	scanRebinds(fd.Body, m)
	bodyBindingCache[fd] = m
	return m
}

// returnCollector gathers every `return` value in a body, including nested blocks, so a string
// returned from inside `if`/`match` is not missed.
type returnCollector struct {
	returns []Expr
}

func (c *returnCollector) stmts(ss []Stmt) {
	for _, s := range ss {
		c.stmt(s)
	}
}

func (c *returnCollector) stmt(s Stmt) {
	switch st := s.(type) {
	case *ReturnStmt:
		c.returns = append(c.returns, st.Expr)
	case *IfStmt:
		c.stmts(st.Then)
		for _, el := range st.Elifs {
			c.stmts(el.Then)
		}
		c.stmts(st.Else)
	case *ForStmt:
		c.stmts(st.Body)
	case *WhileStmt:
		c.stmts(st.Body)
	case *TryStmt:
		c.stmts(st.Body)
		for _, h := range st.Excepts {
			c.stmts(h.Body)
		}
		c.stmts(st.Finally)
	case *WithStmt:
		c.stmts(st.Body)
	}
}

// isConcat reports `a + b` where either side is a string, the shape the record supports
// and codegen refuses until the runtime can allocate a buffer.
func isConcat(n *BinOp) bool {
	if n.Op != "+" {
		return false
	}
	return isStringExpr(n.L) || isStringExpr(n.R)
}

// definitelyNotAString reports the argument shapes that can never be a string, so a second
// call site with a number cancels a string classification rather than silently reusing it.
func definitelyNotAString(e Expr) bool {
	switch v := e.(type) {
	case *IntLit, *FloatLit, *ListLit, *SetLit, *DictLit, *Comp, *Generator, *Lambda:
		return true
	case *Name:
		return v.Value == "None" || v.Value == "True" || v.Value == "False"
	}
	return false
}

// scopeHasString reports whether a variable is known to carry a string inside `scope`
// (falling back to module level), and was never assigned a non-string there.
func scopeHasString(strVars, notString map[string]bool, scope, name string) bool {
	if notString[scopeKey(scope, name)] {
		return false
	}
	if scope != "" && strVars[scopeKey(scope, name)] {
		return true
	}
	return strVars[scopeKey("", name)] && !notString[scopeKey("", name)]
}

// Element-kind bits for a container parameter: which positions of the container a function
// fills with interned strings. The caller needs this because a helper can fill a container its
// caller created (`def fill(out, v): out.append(v)`) — the caller's own scope has no way to
// know, and printing `xs[1]` would show the raw @str_tab index (roadmap Gap J.5).
const (
	EstrListElem  = 1
	EstrDictKey   = 2
	EstrDictValue = 4
	EstrSetMember = 8
)

// stringFillingParams maps function name -> parameter index -> element-kind bits.
func stringFillingParams(prog *Program) map[string]map[int]int {
	out := map[string]map[int]int{}
	if prog == nil {
		return out
	}
	allKinds := heapArgKinds(prog)
	allStrParams := strArgKinds(prog)
	for _, st := range prog.Stmts {
		fd, ok := st.(*FuncDef)
		if !ok {
			continue
		}
		kinds := allKinds[fd.Name]
		strParams := allStrParams[fd.Name]
		paramIndex := func(e Expr) int {
			nm, ok := e.(*Name)
			if !ok {
				return -1
			}
			for i, p := range fd.Params {
				if p.Name == nm.Value {
					return i
				}
			}
			return -1
		}
		isString := func(e Expr) bool {
			return isStringExpr(e) || (paramIndex(e) >= 0 && strParams[paramIndex(e)])
		}
		mark := func(idx, bit int) {
			if idx < 0 || kinds[idx] == HeapNone {
				return
			}
			m := out[fd.Name]
			if m == nil {
				m = map[int]int{}
				out[fd.Name] = m
			}
			m[idx] |= bit
		}
		coll := allStatements{}
		coll.stmts(fd.Body)
		for _, s := range coll.out {
			switch st := s.(type) {
			case *ExprStmt:
				// out.append(v) and s.add(v) put a string *into* a container parameter.
				c, ok := st.Expr.(*Call)
				if !ok {
					continue
				}
				attr, ok := c.Fn.(*Attr)
				if !ok || len(c.Args) != 1 {
					continue
				}
				idx := paramIndex(attr.Obj)
				switch attr.Name.Value {
				case "append":
					if isString(c.Args[0]) {
						mark(idx, EstrListElem)
					}
				case "add":
					if isString(c.Args[0]) {
						mark(idx, EstrSetMember)
					}
				}
			case *AssignStmt:
				// d[k] = v — item assignment, not a plain rebinding of the name.
				ix, ok := st.Target.(*Index)
				if !ok {
					continue
				}
				idx := paramIndex(ix.Obj)
				if idx < 0 {
					continue
				}
				if isString(st.Value) {
					mark(idx, EstrDictValue)
				}
				if isString(ix.Idx) {
					mark(idx, EstrDictKey)
				}
			}
		}
	}
	return out
}

// allStatements collects every statement in a body, including those nested in if/for/while/
// try/with blocks, so a container filled inside a loop still counts.
type allStatements struct {
	out []Stmt
}

func (c *allStatements) stmts(ss []Stmt) {
	for _, s := range ss {
		c.out = append(c.out, s)
		switch st := s.(type) {
		case *IfStmt:
			c.stmts(st.Then)
			for _, el := range st.Elifs {
				c.out = append(c.out, el.Then...)
				c.stmts(el.Then)
			}
			c.stmts(st.Else)
		case *ForStmt:
			c.stmts(st.Body)
			c.stmts(st.Else)
		case *WhileStmt:
			c.stmts(st.Body)
			c.stmts(st.Else)
		case *TryStmt:
			c.stmts(st.Body)
			for _, h := range st.Excepts {
				c.stmts(h.Body)
			}
			c.stmts(st.Finally)
		case *WithStmt:
			c.stmts(st.Body)
		case *FuncDef:
			c.stmts(st.Body) // nested functions share the enclosing parameter names
		case *ClassDef:
			c.stmts(st.Body)
		}
	}
}
