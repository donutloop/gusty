// Containers: what a list/dict/set literal holds, what a container method's elements are, and the length and
// index questions a container answers.
package lang

import (
	"fmt"
	"sort"
	"strings"
)

// sortedRuntime lowers sorted(<container>[, reverse=...]) to a runtime copy-and-sort. The fold in
// the call lowering only covers constant int literals; this is the path for a variable and for
// anything that is not plain integers, and print(sorted(xs)) reaches it directly so the handle it
// yields is rendered by the runtime printer rather than printf'd (roadmap L11.7, ADR 0191).
func (g *irGen) sortedRuntime(b *strings.Builder, c *Call) (string, error) {
	// The key question is asked here, at the top and before a single instruction, because three doors
	// lower a `sorted` call through this one (the expression road, print's container path, and the
	// `for` loop's materialisation) and an option read by position at any of them is the same defect
	// wearing a different hat (Gap R.249, ADR 0340; ADR 0273's purity: the question emits nothing).
	if kc := g.keyedSortedCall(b, c, true); kc.done {
		return kc.handle, kc.err
	}
	// A literal argument is freshly allocated by containerOperand, so sorting it in place is
	// correct; a variable must be copied first, because sorted(xs) leaves xs in its own order.
	argIsFresh := true
	if _, isName := c.Args[0].(*Name); isName {
		argIsFresh = false
	}
	sv, serr := g.containerOperand(b, c.Args[0])
	if serr != nil {
		return "", serr
	}
	// Which comparator: numbers by payload, interned strings by their text. A list holding both
	// is Python's TypeError, and ordering strings by payload would sort them by arrival order.
	mode := 0
	switch a := c.Args[0].(type) {
	case *ListLit:
		sawInt, sawStr := false, false
		for _, el := range a.Elems {
			if _, isStr := el.(*StrLit); isStr {
				mode, sawStr = 1, true
			}
			if _, isInt := el.(*IntLit); isInt {
				sawInt = true
			}
		}
		if sawInt && sawStr {
			return "", fmt.Errorf("cannot sort a list whose elements are of more than one kind; Python raises TypeError here too, and the compiled backend reports it (roadmap L11.1, ADR 0191)")
		}
		if sortElementsAreContainers(a) {
			return "", errSortElementsHaveNoOrder()
		}
	case *Name:
		if g.listElemStr[a.Value] {
			mode = 1
		}
		if g.mixedLists[a.Value] {
			return "", fmt.Errorf("cannot sort a list whose elements are of more than one kind; Python raises TypeError here too, and the compiled backend reports it (roadmap L11.1, ADR 0191)")
		}
		if lit, has := g.containerLits[a.Value]; has && sortElementsAreContainersLit(lit) {
			return "", errSortElementsHaveNoOrder()
		}
	}
	// Handle registers in this backend are named %h<N> off heapSeq; a %t<N> name reads as an
	// ordinary temp to the print and call lowerings, which then printf a handle as a value.
	g.heapSeq++
	slot := fmt.Sprintf("%%h%d", g.heapSeq)
	if argIsFresh {
		b.WriteString(fmt.Sprintf("  call void @rt_sort(i32 %s, i32 %d)\n", sv, mode))
		slot = sv
	} else {
		g.emitHeapHelperCall(b, slot, "rt_list_copy", "i32 "+sv, Span{})
		b.WriteString(fmt.Sprintf("  call void @rt_sort(i32 %s, i32 %d)\n", slot, mode))
	}
	if len(c.Args) == 2 {
		revExpr := c.Args[1]
		if kw, ok := c.Args[1].(*KeywordArg); ok {
			revExpr = kw.Value
		}
		rv, rerr := g.constIntVal(revExpr)
		if rerr != nil {
			return "", fmt.Errorf("sorted(reverse=...) needs a constant flag (roadmap L11.7)")
		}
		if rv != 0 {
			b.WriteString(fmt.Sprintf("  call void @rt_reverse(i32 %s)\n", slot))
		}
	}
	return slot, nil
}

// reversedExprs returns a copy of elems with the order reversed.
func reversedExprs(elems []Expr) []Expr {
	out := append([]Expr(nil), elems...)
	for i, j := 0, len(out)-1; i < j; i, j = i+1, j-1 {
		out[i], out[j] = out[j], out[i]
	}
	return out
}

// listElemLoad loads list element i from an inline list literal's global
// struct with a constant GEP index (this llc build accepts only constant
// GEP indices).
func (g *irGen) listElemLoad(b *strings.Builder, ln *ListLit, name string, i int) string {
	v := g.newTmp()
	n := len(ln.Elems)
	b.WriteString(fmt.Sprintf("  %s = load i32, i32* getelementptr({i32, [%d x i32]}, {i32, [%d x i32]}* %s, i32 0, i32 1, i32 %d)\n", v, n, n, name, i))
	return v
}

// dictLiteralKeys returns the integer keys of a dict literal, erroring if any
// key is not a constant integer (the AOT path lowers only int-keyed dicts).
func dictLiteralKeys(dl *DictLit) ([]int64, error) {
	keys := make([]int64, len(dl.Keys))
	for i, k := range dl.Keys {
		il, ok := k.(*IntLit)
		if !ok {
			return nil, fmt.Errorf("codegen: a compiled dict literal holds constant integer keys only; CPython supports string and other keys (a compiled string needs the runtime string table, roadmap Gap I.2)")
		}
		keys[i] = il.Value
	}
	return keys, nil
}

// dictLiteralVals returns the integer values of a dict literal, erroring if
// any value is not a constant integer.
func dictLiteralVals(dl *DictLit) ([]int64, error) {
	vals := make([]int64, len(dl.Vals))
	for i, v := range dl.Vals {
		il, ok := v.(*IntLit)
		if !ok {
			return nil, fmt.Errorf("codegen: a compiled dict literal holds constant integer values only; CPython supports string and other values (a compiled string needs the runtime string table, roadmap Gap I.2)")
		}
		vals[i] = il.Value
	}
	return vals, nil
}

// setLiteralElems returns the integer elements of a set literal, erroring if
// any element is not a constant integer.
func setLiteralElems(sl *SetLit) ([]int64, error) {
	// A SET has one member per distinct VALUE, so a literal with duplicates collapses. The static
	// global was built straight from the source elements, which made `len({1, 2, 2, 3})` answer 4 and
	// `len({1, 1, 1})` answer 3 — a set that printed with braces and behaved like a list (roadmap Gap
	// R.181). the record deduplicates with `eqVal` and keeps the FIRST spelling inserted (ADR
	// 0259's `{True, 1}`); this road only admits integer literals, so distinct words are distinct
	// values here, and insertion order is preserved the same way.
	seen := make(map[int64]bool, len(sl.Elems))
	elems := make([]int64, 0, len(sl.Elems))
	for _, el := range sl.Elems {
		il, ok := el.(*IntLit)
		if !ok {
			return nil, fmt.Errorf("set literal elements must be constant integers")
		}
		if seen[il.Value] {
			continue
		}
		seen[il.Value] = true
		elems = append(elems, il.Value)
	}
	return elems, nil
}

// intMembershipValues returns the integer membership-test values of a literal
// container expression (list/set elements, or dict keys) and whether the
// expression is a literal container whose test values are all integer
// constants. For `x in {k: v, ...}`, membership tests keys -- matching the
// interpreter (dict `in` checks keys).
func intMembershipValues(e Expr) ([]int64, bool) {
	var elems []Expr
	switch c := e.(type) {
	case *ListLit:
		elems = c.Elems
	case *SetLit:
		elems = c.Elems
	case *DictLit:
		elems = c.Keys // membership tests keys
	default:
		return nil, false
	}
	vals := make([]int64, 0, len(elems))
	for _, el := range elems {
		v, ok := constIntMemberVal(el)
		if !ok {
			return nil, false
		}
		vals = append(vals, v)
	}
	return vals, true
}

// constIntMemberVal returns the integer constant value of an expr, or ok=false.
func constIntMemberVal(e Expr) (int64, bool) {
	switch c := e.(type) {
	case *IntLit:
		return c.Value, true
	case *NoneLit:
		return 0, true
	case *BoolLit:
		if c.Value {
			return 1, true
		}
		return 0, true
	case *UnOp:
		if c.Op == "-" {
			if negationOperandIsLiterallyNotANumber(c.X) {
				// `[-None]` is not a container of the constant 0: it is a program the reference stops on,
				// and a fold that answers it here would print a number where the raise belongs (Gap R.137).
				return 0, false
			}
			if v, ok := constIntMemberVal(c.X); ok {
				return -v, true
			}
		}
	}
	return 0, false
}

// listCallElems returns the underlying element list of a list-returning
// builtin call over an inline list literal. sorted/reversed preserve the
// element set (only reordering), so consumers like len/sum/min/max/any/all
// fold identically over the original elements.
func listCallElems(a Expr) ([]Expr, bool) {
	c, ok := a.(*Call)
	if !ok {
		return nil, false
	}
	fn, ok := c.Fn.(*Name)
	if !ok {
		return nil, false
	}
	switch fn.Value {
	case "sorted", "reversed":
		if len(c.Args) == 0 {
			return nil, false
		}
		if lit, ok := c.Args[0].(*ListLit); ok {
			if lit.Elems == nil {
				// Empty inline list: return a non-nil empty slice so
				// consumers fold len/sum/any/all over it (e.g. sum(sorted([]))).
				return []Expr{}, true
			}
			return lit.Elems, true
		}
		// Nested sorted/reversed calls preserve the element set; recurse to
		// the underlying inline list literal (e.g. sorted(reversed([...]))).
		if elems, ok := listCallElems(c.Args[0]); ok {
			return elems, true
		}
	}
	return nil, false
}

// listArgLen returns the length of a list-producing expression, which may
// itself be a nested list-producing builtin call (sorted/reversed/enumerate/
// zip/partition/split/rsplit). It recursively unwraps calls to match the
// record's length semantics.
func (g *irGen) listArgLen(a Expr) (int, bool) {
	switch v := a.(type) {
	case *ListLit:
		return len(v.Elems), true
	case *Call:
		fn, ok := v.Fn.(*Name)
		if !ok || len(v.Args) == 0 {
			return 0, false
		}
		switch fn.Value {
		case "sorted", "reversed":
			return g.listArgLen(v.Args[0])
		case "enumerate":
			return g.listArgLen(v.Args[0])
		case "zip":
			if len(v.Args) != 2 {
				return 0, false
			}
			a, ok1 := g.listArgLen(v.Args[0])
			b, ok2 := g.listArgLen(v.Args[1])
			if !ok1 || !ok2 {
				return 0, false
			}
			if b < a {
				return b, true
			}
			return a, true
		case "partition":
			if _, ok := g.stringVal(v.Args[0]); ok {
				return 3, true
			}
			return 0, false
		case "split", "rsplit":
			if len(v.Args) != 2 {
				return 0, false
			}
			s, ok1 := g.stringVal(v.Args[0])
			sep, ok2 := g.stringVal(v.Args[1])
			if !ok1 || !ok2 {
				return 0, false
			}
			return strings.Count(s, sep) + 1, true
		}
	}
	return 0, false
}

// listLen returns the length of a list-producing builtin call over literal
// arguments, matching the record semantics:
//
//	enumerate(x)   -> len(x)         (x must be an inline list literal)
//	zip(a, b)      -> min(len(a), len(b))
//	partition(s)   -> 3              (always three parts)
//	split(s, sep)  -> occurrences(sep in s) + 1
//	rsplit(s, sep) -> occurrences(sep in s) + 1
func (g *irGen) listLen(a Expr) (int, bool) {
	c, ok := a.(*Call)
	if !ok || len(c.Args) == 0 {
		return 0, false
	}
	fn, ok := c.Fn.(*Name)
	if !ok {
		return 0, false
	}
	switch fn.Value {
	case "reversed":
		if len(c.Args) != 1 {
			return 0, false
		}
		// reversed(s) preserves the length of a constant string s.
		if s, ok := g.stringVal(c.Args[0]); ok {
			return len(s), true
		}
		return 0, false
	case "enumerate":
		if len(c.Args) != 1 {
			return 0, false
		}
		return g.listArgLen(c.Args[0])
	case "zip":
		if len(c.Args) != 2 {
			return 0, false
		}
		a, ok1 := g.listArgLen(c.Args[0])
		b, ok2 := g.listArgLen(c.Args[1])
		if !ok1 || !ok2 {
			return 0, false
		}
		if b < a {
			return b, true
		}
		return a, true
	case "partition":
		if len(c.Args) != 1 {
			return 0, false
		}
		if _, ok := g.stringVal(c.Args[0]); ok {
			return 3, true
		}
		return 0, false
	case "split", "rsplit":
		if len(c.Args) != 2 {
			return 0, false
		}
		s, ok1 := g.stringVal(c.Args[0])
		sep, ok2 := g.stringVal(c.Args[1])
		if !ok1 || !ok2 {
			return 0, false
		}
		return strings.Count(s, sep) + 1, true
	}
	return 0, false
}

// emitList emits a dedicated global struct for an inline list literal
// (dedup by AST node) and returns its global name. Elements must be ints.
func (g *irGen) emitList(ln *ListLit) (string, error) {
	if name, ok := g.listNames[ln]; ok {
		return name, nil
	}
	if g.listNames == nil {
		g.listNames = map[*ListLit]string{}
	}
	n := len(ln.Elems)
	g.lstIdx++
	name := fmt.Sprintf("@.lst%d", g.lstIdx)
	// Build the whole definition before touching the globals buffer. This emitter used to write the
	// opening `@.lstN = private global {i32, [N x i32]} { i32 N, [N x i32] [` and only then look at the
	// elements; when one of them was not an int it returned an error and left an unterminated global
	// definition in the module. Whatever caught that error -- the container-equality path, the print
	// path -- shipped the broken line, and the user got exit 2, `expected type`, for a program like
	// `print([1.5, 2])` whose answer CPython prints in one line (roadmap Gap R.40, ADR 0166).
	var def strings.Builder
	parts := make([]string, 0, n)
	for i, el := range ln.Elems {
		il, ok := el.(*IntLit)
		if !ok {
			if _, isFloat := el.(*FloatLit); isFloat {
				return "", fmt.Errorf("a compiled container cannot hold a float yet: the element slot is an i32 word and %s has no representation in one (CPython answers this program; compiled floats in containers are roadmap L11.6)", exprTyName(el))
			}
			return "", fmt.Errorf("list literal elements must be integers, not %s", exprTyName(el))
		}
		_ = i
		parts = append(parts, fmt.Sprintf("i32 %d", il.Value))
	}
	def.WriteString(fmt.Sprintf("%s = private global {i32, [%d x i32]} { i32 %d, [%d x i32] [%s] }\n", name, n, n, n, strings.Join(parts, ", ")))
	if g.staticLists == nil {
		g.staticLists = map[string]*ListLit{}
	}
	g.staticLists[name] = ln
	g.globals.WriteString(def.String())
	g.listNames[ln] = name
	return name, nil
}

// emitDict emits a dedicated global struct for an inline dict literal
// (dedup by AST node) and returns its global name. Layout: {i32 count,
// [n x i32] keys, [n x i32] vals}. Keys and values must be constant ints.
func (g *irGen) emitDict(dl *DictLit) (string, error) {
	if name, ok := g.dictNames[dl]; ok {
		return name, nil
	}
	if g.dictNames == nil {
		g.dictNames = map[*DictLit]string{}
	}
	keys, err := dictLiteralKeys(dl)
	if err != nil {
		return "", err
	}
	vals, err := dictLiteralVals(dl)
	if err != nil {
		return "", err
	}
	n := len(dl.Keys)
	g.dictIdx++
	name := fmt.Sprintf("@.dict%d", g.dictIdx)
	var keysArr, valsArr strings.Builder
	for i := 0; i < n; i++ {
		if i > 0 {
			keysArr.WriteString(", ")
			valsArr.WriteString(", ")
		}
		keysArr.WriteString(fmt.Sprintf("i32 %d", keys[i]))
		valsArr.WriteString(fmt.Sprintf("i32 %d", vals[i]))
	}
	g.globals.WriteString(fmt.Sprintf("%s = private global {i32, [%d x i32], [%d x i32]} { i32 %d, [%d x i32] [%s], [%d x i32] [%s] }\n", name, n, n, n, n, keysArr.String(), n, valsArr.String()))
	g.dictNames[dl] = name
	return name, nil
}

// emitSet emits a dedicated global struct for an inline set literal
// (dedup by AST node) and returns its global name. Layout matches a list:
// {i32 count, [n x i32] elems}. Elements must be constant ints.
func (g *irGen) emitSet(sl *SetLit) (string, error) {
	if name, ok := g.setNames[sl]; ok {
		return name, nil
	}
	if g.setNames == nil {
		g.setNames = map[*SetLit]string{}
	}
	elems, err := setLiteralElems(sl)
	if err != nil {
		return "", err
	}
	// The COUNT the global carries is the deduplicated one, not the number of elements the source
	// spelled — see setLiteralElems (roadmap Gap R.181).
	n := len(elems)
	g.setIdx++
	name := fmt.Sprintf("@.set%d", g.setIdx)
	var elemsArr strings.Builder
	for i := 0; i < n; i++ {
		if i > 0 {
			elemsArr.WriteString(", ")
		}
		elemsArr.WriteString(fmt.Sprintf("i32 %d", elems[i]))
	}
	g.globals.WriteString(fmt.Sprintf("%s = private global {i32, [%d x i32]} { i32 %d, [%d x i32] [%s] }\n", name, n, n, n, elemsArr.String()))
	g.setNames[sl] = name
	return name, nil
}

// value emits an IR expression returning an i32 value; returns the operand string.
// stringVal resolves an Expr to a concrete string constant, if any.
func (g *irGen) dictMethodElems(e Expr) ([]Expr, bool) {
	c, ok := e.(*Call)
	if !ok {
		return nil, false
	}
	attr, ok := c.Fn.(*Attr)
	if !ok {
		return nil, false
	}
	if attr.Name.Value == "split" {
		str, ok := g.stringVal(attr.Obj)
		if !ok {
			return nil, false
		}
		sep := " "
		if len(c.Args) > 0 {
			if s2, ok := g.stringVal(c.Args[0]); ok {
				sep = s2
			} else {
				return nil, false
			}
		}
		parts := strings.Split(str, sep)
		elems := make([]Expr, len(parts))
		for i, p := range parts {
			elems[i] = &StrLit{Value: p}
		}
		return elems, true
	}
	if attr.Name.Value == "rsplit" {
		s, ok := g.stringVal(attr.Obj)
		if !ok {
			return nil, false
		}
		sep := " "
		if len(c.Args) == 1 {
			sep, ok = g.stringVal(c.Args[0])
			if !ok {
				return nil, false
			}
		}
		parts := strings.Split(s, sep)
		var elems []Expr
		for _, part := range parts {
			elems = append(elems, &StrLit{Value: part})
		}
		return elems, true
	}
	if attr.Name.Value == "partition" {
		// partition(s) always returns [head, sep, tail] (3 parts), so
		// len(...) folds to 3. Return three dummy int elements to count.
		if _, ok := g.stringVal(attr.Obj); !ok {
			return nil, false
		}
		return []Expr{
			&IntLit{Value: 0},
			&IntLit{Value: 0},
			&IntLit{Value: 0},
		}, true
	}
	if ll, ok := attr.Obj.(*ListLit); ok && attr.Name.Value == "append" {
		if len(c.Args) != 1 {
			return nil, false
		}
		elems := append(append([]Expr{}, ll.Elems...), c.Args[0])
		return elems, true
	}
	dl, ok := attr.Obj.(*DictLit)
	if !ok {
		return nil, false
	}
	switch attr.Name.Value {
	case "keys":
		return dl.Keys, true
	case "values":
		return dl.Vals, true
	case "items":
		// len({...}.items()) folds to the number of key/value pairs.
		return dl.Keys, true
	case "partition":
		// partition(s) always returns [head, sep, tail] (3 parts), so
		// len(...) folds to 3. Return three dummy int elements to count.
		return []Expr{
			&IntLit{Value: 0},
			&IntLit{Value: 0},
			&IntLit{Value: 0},
		}, true
	}
	return nil, false
}
func (g *irGen) isPartitionCall(c *Call) bool {
	if attr, ok := c.Fn.(*Attr); ok && attr.Name.Value == "partition" {
		return true
	}
	return false
}
func reverseVals(v []int64) {
	for i, j := 0, len(v)-1; i < j; i, j = i+1, j-1 {
		v[i], v[j] = v[j], v[i]
	}
}
func (g *irGen) indexListElems(c *Call) ([]Expr, bool) {
	// Method calls returning list elements: keys(), values(), split().
	// partition() returns only dummy length elems (see dictMethodElems),
	// so it is excluded here to avoid silently wrong results.
	if elems, ok := g.dictMethodElems(c); ok {
		if g.isPartitionCall(c) {
			return nil, false
		}
		return elems, true
	}
	// Builtin calls returning list elements: sorted(list), reversed(list).
	if fn, ok := c.Fn.(*Name); ok && (fn.Value == "sorted" || fn.Value == "reversed") {
		if lit, ok := c.Args[0].(*ListLit); ok {
			vals := make([]int64, len(lit.Elems))
			for i, el := range lit.Elems {
				il, ok := el.(*IntLit)
				if !ok {
					return nil, false
				}
				vals[i] = il.Value
			}
			if fn.Value == "sorted" {
				sort.Slice(vals, func(i, j int) bool { return vals[i] < vals[j] })
				// honor sorted(list, reverse=True)
				if len(c.Args) > 1 {
					if kw, ok := c.Args[1].(*KeywordArg); ok && kw.Name == "reverse" {
						if b, ok := kw.Value.(*BoolLit); ok && b.Value {
							reverseVals(vals)
						}
					}
				}
			} else {
				rev := make([]int64, len(vals))
				for i, v := range vals {
					rev[len(vals)-1-i] = v
				}
				vals = rev
			}
			elems := make([]Expr, len(vals))
			for i, v := range vals {
				elems[i] = &IntLit{Value: v}
			}
			return elems, true
		}
	}
	return nil, false
}

// dictIndex resolves a constant key against a DictLit at codegen time.
func (g *irGen) dictIndex(dl *DictLit, key int64) (string, error) {
	keys, err := dictLiteralKeys(dl)
	if err != nil {
		return "", err
	}
	vals, err := dictLiteralVals(dl)
	if err != nil {
		return "", err
	}
	// A repeated key in a literal is one key written twice, and the reference lets the later entry win the
	// value: `{1: 2, 1: 3}[1]` is `3`. Scanning backwards is the whole of it (roadmap Gap R.216, ADR 0327).
	for i := len(keys) - 1; i >= 0; i-- {
		if keys[i] == key {
			return fmt.Sprintf("%d", vals[i]), nil
		}
	}
	return "", fmt.Errorf("missing dict key %d", key)
}

// dictViewElems is the print road's question about `d.keys()` and `d.values()`: what element list does
// this expression lower to, if it is a view at all. Asked of the SAME call shape the fold in the call road
// recognises, so the printer can never claim a kind the fold did not produce (Gap R.182). `items()` is
// excluded: its elements are pairs, which this backend has no tuple value for (roadmap L11.3).
func (g *irGen) dictViewElems(e Expr) ([]Expr, string, bool) {
	call, ok := e.(*Call)
	if !ok || len(call.Args) != 0 {
		return nil, "", false
	}
	attr, isAttr := call.Fn.(*Attr)
	if !isAttr || attr.Name == nil {
		return nil, "", false
	}
	dl, isDict := attr.Obj.(*DictLit)
	if !isDict {
		return nil, "", false
	}
	switch attr.Name.Value {
	case "keys":
		return dl.Keys, "keys", true
	case "values":
		return dl.Vals, "values", true
	}
	return nil, "", false
}

// listLiteralOf reads the list literal an expression lowers to: the node itself when the source spelled
// one, and the recorded literal when the program bound one to a name and never rebound it.
func (g *irGen) listLiteralOf(e Expr) *ListLit {
	if lit, ok := e.(*ListLit); ok {
		return lit
	}
	if nm, ok := e.(*Name); ok {
		if g.builtinShadowed(nm.Value) {
			return nil
		}
		return g.staticLists[nm.Value]
	}
	return nil
}

// heapContainerLenFn returns the runtime helper that reports a container
// variable's length, when the expression names a known runtime container.
func (g *irGen) heapContainerLenFn(e Expr) (string, bool) {
	switch n := e.(type) {
	case *Name:
		// The container maps are keyed by the written name (see listVars/dicts/sets).
		switch {
		case g.listVars[n.Value]:
			return "rt_list_len", true
		case g.runtimeDicts[n.Value]:
			return "rt_dict_len", true
		case g.runtimeSets[n.Value]:
			return "rt_set_len", true
		}
		return "", false
	case *Slice:
		// A slice of a list **is** a list, so a condition on it asks the object `rt_slice` built whether it
		// holds anything: `if xs[0:0]:` is false and `if xs[0:2]:` is true, which is what the reference reads
		// and `not xs[0:0] == False` was the symptom of not asking. A text slice stays with the text road,
		// and a dict or set is not sliced at all (roadmap Gap R.230, ADR 0335).
		if kind, ok := g.staticContainerKind(n.Obj); ok && kind == "list" {
			return "rt_list_len", true
		}
	}
	return "", false
}

// nameIsContainerRecorded is the container side of the same question, asked of the records that already
// exist rather than of a new one.
func (g *irGen) nameIsContainerRecorded(e Expr) bool {
	nm, isName := e.(*Name)
	if !isName {
		if _, isList := e.(*ListLit); isList {
			return true
		}
		if _, isDict := e.(*DictLit); isDict {
			return true
		}
		if _, isSet := e.(*SetLit); isSet {
			return true
		}
		if _, isTup := e.(*Tuple); isTup {
			return true
		}
		return false
	}
	return g.listVars[nm.Value] || g.runtimeDicts[nm.Value] || g.runtimeSets[nm.Value] ||
		g.mixedLists[nm.Value] || g.mixedDicts[nm.Value] || g.mixedSets[nm.Value] || g.unionVars[nm.Value]
}

// dictFoldMisses asks the question the dict-literal `get` fold already answered: is this key absent from
// the literal written at the call? It is a separate function rather than an inline test so the fold and
// the print road ask ONE predicate — the fold may not answer None where the printer still says 0, and the
// pair is exactly the ADR 0229 rule about text ("is it text" cannot be yes for one road and no for the
// other). An argument the fold cannot evaluate is not "absent": it is unknown, and the answer is no.
// dictFoldSlot is the HIT counterpart of dictFoldMisses: the value the `get` fold would return for
// this key, and whether the key is there at all. It is the same lookup the fold performs, so the
// print roads can never claim a kind the answer does not have (roadmap Gap R.180).
func (g *irGen) dictFoldSlot(dl *DictLit, key Expr) (Expr, bool) {
	if kv, err := g.constIntVal(key); err == nil {
		for i, k := range dl.Keys {
			if il, ok := k.(*IntLit); ok && il.Value == kv {
				return dl.Vals[i], true
			}
		}
		return nil, false
	}
	if sv, ok := stringConst(key, g.builtinShadowed); ok {
		for i, k := range dl.Keys {
			if sl, ok := k.(*StrLit); ok && sl.Value == sv {
				return dl.Vals[i], true
			}
		}
	}
	return nil, false
}
func (g *irGen) dictFoldMisses(dl *DictLit, key Expr) bool {
	if kv, err := g.constIntVal(key); err == nil {
		for _, k := range dl.Keys {
			if il, ok := k.(*IntLit); ok && il.Value == kv {
				return false
			}
		}
		return true
	}
	if sv, ok := stringConst(key, nil); ok {
		for _, k := range dl.Keys {
			if sl, ok := k.(*StrLit); ok && sl.Value == sv {
				return false
			}
		}
		return true
	}
	return false
}

// heapLenOf reads a container's length through the runtime, which is the only party that knows the
// object: the count is the same word for a list, a dict and a set, so one call answers all three.
func (g *irGen) heapLenOf(b *strings.Builder, h string) string {
	g.heapUsed = true
	v := g.newTmp()
	fmt.Fprintf(b, "  %s = call i32 @rt_heap_len(i32 %s)\n", v, h)
	return v
}
