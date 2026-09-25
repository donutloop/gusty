package lang

import (
	"fmt"
	"math"
	"math/rand"
	"os"
	"sort"
	"strconv"
	"strings"
	"unicode"
)

// Evaluator is a small AST interpreter used by --eval and the REPL.
// It evaluates integer-typed expressions deterministically without needing
// an LLVM JIT engine (the AOT backend emits textual IR verified by `llc`).
type Evaluator struct {
	Vars        map[string]int64
	funcs       map[string]*FuncDef
	externs     map[string]*ExternDecl
	inCall      bool // true while evaluating a function body (nested defs become closures)
	heap        map[int64]*obj
	nextID      int64
	nurseryBase int64
	allocCount  int64
	classIDs    map[string]int64
	curRet      *Type  // return annotation of the function currently executing
	fnName      string // name of the function whose body is being evaluated
	cur         Span   // source span of the statement currently being evaluated
	yieldList   int64  // list handle accumulating yields (0 = not in generator)
	curClass    string // class name of the method currently executing (for super())
	curSelf     int64  // receiver of the method currently executing (for super())
}

// obj is a heap value: a class, an instance, or a bound/unbound method.
type obj struct {
	kind  string           // "class" | "instance" | "method" | "list" | "superproxy"
	class string           // class name (instance/method)
	attrs map[string]int64 // instance attrs or class method-handle ids
	env   map[string]int64 // captured enclosing scope (kind=closure)
	fn    *FuncDef         // method body (kind=method)
	mname string           // method name (kind=method)
	recv  int64            // bound receiver id (0 = unbound)
	base  int64            // base class id (kind=class) for inheritance
	elems []int64          // list elements (kind=list)
	dvals []int64          // dict values parallel to elems keys (kind=dict)
	sval  string           // string value (kind=str)
	fval  float64          // float value (kind=float)
	doc   string           // __doc__ string (def/class/closure objects)
}

// tag returns the canonical %obj kind tag for this heap object. Both the
// interpreter heap and the AOT runtime derive tags from the same canonical
// table (value.go), so AOT and interpreter agree on the dynamic type model.
func (o *obj) tag() ValueTag {
	return objKindTag(o.kind)
}

// tagOfVal returns the canonical %obj kind tag for a runtime value. Heap
// values map their obj.kind; plain ints/bools/None are raw i64s in the
// interpreter today, so they report the immediate tag (the %obj payload word
// carries the raw value, exactly as the AOT representation does).
func (e *Evaluator) tagOfVal(v int64) ValueTag {
	if o, ok := e.heap[v]; ok {
		return o.tag()
	}
	return TagInt
}


// setLoopVar binds a for-loop variable (a Name, or a Tuple of Names) to a value.
// For a Tuple, the value must be a list/tuple object whose length matches.
func (e *Evaluator) setLoopVar(v Expr, val int64) error {
	switch t := v.(type) {
	case *Name:
		e.Vars[t.Value] = val
		return nil
	case *Tuple:
		obj := e.heap[val]
		if obj == nil {
			return &EvalError{Msg: "cannot unpack non-iterable loop value"}
		}
		if len(obj.elems) != len(t.Elems) {
			return &EvalError{Msg: "cannot unpack %d values into %d loop variables"}
		}
		for i, n := range t.Elems {
			if nm, ok := n.(*Name); ok {
				e.Vars[nm.Value] = obj.elems[i]
			}
		}
		return nil
	}
	return &EvalError{Msg: "unsupported loop variable"}
}

// resolveClassID returns the heap id of the class bound to name, if any.
// Classes can be referenced by their definition name (classIDs) or as a
// class value stored in a variable (e.g. `Alias = Point`).
func (e *Evaluator) resolveClassID(name string) (int64, bool) {
	if id, ok := e.classIDs[name]; ok {
		return id, true
	}
	if v, ok := e.Vars[name]; ok {
		if o, ok2 := e.heap[v]; ok2 && o.kind == "class" {
			return v, true
		}
	}
	return 0, false
}

func (e *Evaluator) allocObj(kind string) int64 {
	e.nextID++
	e.allocCount++
	id := e.nextID
	e.heap[id] = &obj{kind: kind, attrs: map[string]int64{}}
	return id
}

// allocClosure creates a closure value capturing the current scope.
// allocStr allocates a boxed string value and returns its heap handle.
func (e *Evaluator) allocStr(val string) int64 {
	id := e.allocObj("str")
	e.heap[id].sval = val
	return id
}

// pySliceIndices computes the normalized start/stop/step for a slice
// following CPython's PySlice_GetIndicesEx semantics (used by s[a:b:c]).
func pySliceIndices(low, high, step int64, hasLow, hasHigh bool, n int64) (start, stop, stp int64) {
	if step > 0 {
		if hasLow {
			if low < 0 {
				low = maxInt(n+low, 0)
			} else {
				low = minInt(low, n)
			}
		} else {
			low = 0
		}
		if hasHigh {
			if high < 0 {
				high = maxInt(n+high, 0)
			} else {
				high = minInt(high, n)
			}
		} else {
			high = n
		}
		return low, high, step
	}
	// step < 0
	if hasLow {
		if low < 0 {
			low = maxInt(n+low, -1)
		} else {
			low = minInt(low, n-1)
		}
	} else {
		low = n - 1
	}
	if hasHigh {
		if high < 0 {
			high = maxInt(n+high, -1)
		} else {
			high = minInt(high, n-1)
		}
	} else {
		high = -1
	}
	return low, high, step
}

func maxInt(a, b int64) int64 {
	if a > b {
		return a
	}
	return b
}

func minInt(a, b int64) int64 {
	if a < b {
		return a
	}
	return b
}

// allocFloat allocates a boxed float value and returns its heap handle.
func (e *Evaluator) allocFloat(val float64) int64 {
	id := e.allocObj("float")
	e.heap[id].fval = val
	return id
}

// allocExn allocates an exception object of the given class with a message.
// kind="exn", class=type name, sval=message.
func (e *Evaluator) allocExn(exnType, msg string) int64 {
	id := e.allocObj("exn")
	o := e.heap[id]
	o.class = exnType
	o.sval = msg
	return id
}

// exnInfo returns (type, message) of an exception object (kind="exn"),
// or ("Exception", repr) for a non-exception value.
func (e *Evaluator) exnInfo(id int64) (string, string) {
	if o, ok := e.heap[id]; ok && o.kind == "exn" {
		return o.class, o.sval
	}
	return "Exception", e.Repr(id)
}

// isExnClass reports whether name is a built-in exception constructor.
func isExnClass(name string) bool {
	switch name {
	case "Exception", "ValueError", "TypeError", "KeyError", "IndexError",
		"RuntimeError", "StopIteration", "ZeroDivisionError":
		return true
	}
	return false
}

// floatOf returns the float value of a heap handle (kind=float), with a
// boolean indicating whether the handle is a boxed float.
func (e *Evaluator) floatOf(id int64) (float64, bool) {
	if o, ok := e.heap[id]; ok && o.kind == "float" {
		return o.fval, true
	}
	return 0, false
}

// strOf returns the string value of a heap handle, or "" if the handle is
// not a boxed string.
func (e *Evaluator) strOf(id int64) string {
	if o, ok := e.heap[id]; ok && o.kind == "str" {
		return o.sval
	}
	return ""
}

// Repr renders a heap handle (or plain int) to its printable representation.
func (e *Evaluator) Repr(id int64) string {
	if o, ok := e.heap[id]; ok {
		switch o.kind {
		case "str":
			return o.sval
		case "float":
			return fmt.Sprintf("%g", o.fval)
		case "list":
			parts := make([]string, 0, len(o.elems))
			for _, el := range o.elems {
				parts = append(parts, e.Repr(el))
			}
			return "[" + strings.Join(parts, ", ") + "]"
		case "dict":
			parts := make([]string, 0, len(o.elems))
			for i, k := range o.elems {
				parts = append(parts, fmt.Sprintf("%v: %s", k, e.Repr(o.dvals[i])))
			}
			return "{" + strings.Join(parts, ", ") + "}"
		default:
			return "<" + o.kind + ">"
		}
	}
	return fmt.Sprintf("%d", id)
}

func (e *Evaluator) allocClosure(fn *FuncDef, env map[string]int64) int64 {
	e.nextID++
	id := e.nextID
	e.heap[id] = &obj{kind: "closure", fn: fn, env: env, attrs: map[string]int64{}, doc: fn.Doc}
	return id
}

// typeOfVal maps a runtime value to its static type Kind for gradual typing
// checks. Plain small int64s are ints/bools/none; heap ids are objects whose
// kind string maps to a Type.
// TypeOf reports the dynamic type name of a heap value id for the
// machine-readable --json interface (e.g. "int", "float", "str", "list").
func (e *Evaluator) TypeOf(v int64) string {
	return e.typeOfVal(v).Name()
}

func (e *Evaluator) typeOfVal(val int64) *Type {
	if o, ok := e.heap[val]; ok {
		switch o.kind {
		case "float":
			return TFlt()
		case "str":
			return TStr()
		case "list":
			return TList(TDyn())
		case "dict":
			return TDict(TDyn(), TDyn())
		case "set":
			return TSet(TDyn())
		case "closure", "method", "class":
			return TFunc(nil, TDyn())
		default:
			return TDyn()
		}
	}
	return TInt()
}

// checkAnnot enforces a gradual type annotation on a runtime value. Dynamic
// annotations accept anything; otherwise the value's runtime kind must be
// assignable to the annotation.
func (e *Evaluator) checkAnnot(name string, ty *Type, val int64) error {
	if ty == nil || ty.Kind == KindDynamic {
		return nil
	}
	rt := e.typeOfVal(val)
	if rt.Kind == ty.Kind {
		return nil
	}
	// bools are stored as plain ints 0/1; plain ints are compatible with both
	// int and bool annotations (the interpreter cannot distinguish them).
	if rt.Kind == KindInt && (ty.Kind == KindInt || ty.Kind == KindBool) {
		return nil
	}
	return &EvalError{Msg: "type mismatch: expected " + tyName(ty) + " but got " + tyName(rt) + " for " + name}
}

func tyName(t *Type) string {
	switch t.Kind {
	case KindInt:
		return "int"
	case KindFloat:
		return "float"
	case KindBool:
		return "bool"
	case KindString:
		return "str"
	case KindNone:
		return "none"
	case KindList:
		return "list"
	case KindDict:
		return "dict"
	case KindSet:
		return "set"
	case KindFunc:
		return "func"
	case KindDynamic:
		return "any"
	default:
		return "value"
	}
}

// callFunc evaluates a function body with params bound into a scope seeded
// from env (nil = empty scope). Nested defs inside the body become closures.

func (e *Evaluator) recordCall(err error, callee string, caller string, callSite Span) error {
	ee, ok := err.(*EvalError)
	if !ok {
		return err
	}
	cf := Frame{Name: caller, Line: callSite.Line, Col: callSite.Col}
	if ee.Traceback == nil {
		inner := Frame{Name: callee, Line: e.cur.Line, Col: e.cur.Col}
		ee.Traceback = []Frame{cf, inner}
	} else {
		ee.Traceback = append([]Frame{cf}, ee.Traceback...)
	}
	return err
}

func (e *Evaluator) callFunc(fd *FuncDef, argVals []int64, env map[string]int64) (int64, error) {
	caller := e.fnName
	callSite := e.cur
	savedFn := e.fnName
	e.fnName = fd.Name
	defer func() { e.fnName = savedFn }()

	scope := map[string]int64{}
	for k, v := range env {
		scope[k] = v
	}
	// bind positional args, filling defaults for missing trailing params
	for i, p := range fd.Params {
		var av int64
		if i < len(argVals) {
			av = argVals[i]
		} else if p.Default != nil {
			dv, err := e.eval(p.Default)
			if err != nil {
				return 0, err
			}
			av = dv
		}
		if p.Annot != nil {
			if err := e.checkAnnot(p.Name, p.Annot, av); err != nil {
				return 0, err
			}
		}
		scope[p.Name] = av
	}
	saved := e.Vars
	prevRet := e.curRet
	e.curRet = fd.ReturnAnno
	if containsYield(fd.Body) {
		genH := e.allocObj("list")
		prev := e.yieldList
		e.yieldList = genH
		e.Vars = scope
		e.inCall = true
		_, err := e.evalBody(fd.Body)
		e.inCall = false
		e.yieldList = prev
		e.Vars = saved
		e.curRet = prevRet
		if _, ok := err.(*returnSignal); ok {
			return genH, nil
		}
		return genH, e.recordCall(err, fd.Name, caller, callSite)
	}
	e.Vars = scope
	e.inCall = true
	rv, err := e.evalBody(fd.Body)
	e.inCall = false
	e.Vars = saved
	e.curRet = prevRet
	if rs, ok := err.(*returnSignal); ok {
		return rs.val, nil
	}
	return rv, e.recordCall(err, fd.Name, caller, callSite)
}

// callClosure invokes a closure value with its captured environment.

// evalDecorator resolves a decorator expression to a callable value.
// A bare Name may refer to a top-level function definition (a first-class
// value not representable as an int64), or to a closure/other value.
func (e *Evaluator) evalDecorator(dec Expr) (any, error) {
	if n, ok := dec.(*Name); ok {
		if fd, ok := e.funcs[n.Value]; ok {
			return fd, nil
		}
		if v, ok := e.Vars[n.Value]; ok {
			return v, nil
		}
	}
	return e.eval(dec)
}

// callDecValue invokes a decorator value (top-level FuncDef or closure) with
// the function value it decorates, returning the (possibly transformed) value.
func (e *Evaluator) callDecValue(decVal any, arg int64) (int64, error) {
	switch v := decVal.(type) {
	case *FuncDef:
		return e.callFunc(v, []int64{arg}, e.Vars)
	case int64:
		o := e.heap[v]
		if o != nil && o.kind == "closure" {
			return e.callFunc(o.fn, []int64{arg}, o.env)
		}
	}
	return 0, &EvalError{Msg: "decorator is not callable"}
}

func (e *Evaluator) callClosure(o *obj, n *Call) (int64, error) {
	argVals := make([]int64, len(n.Args))
	for i, a := range n.Args {
		av, err := e.eval(a)
		if err != nil {
			return 0, err
		}
		argVals[i] = av
	}
	return e.callFunc(o.fn, argVals, o.env)
}

func NewEvaluator() *Evaluator {
	// Reserve a high handle base so boxed heap ids never collide with small
	// integer literal values (which are stored raw in lists, dict keys, vars).
	// Otherwise Repr(id) would format heap[id] as an object and recurse (e.g.
	// a list at handle 1 whose elems contain the raw int 1).
	return &Evaluator{Vars: map[string]int64{}, funcs: map[string]*FuncDef{}, externs: map[string]*ExternDecl{}, heap: map[int64]*obj{}, classIDs: map[string]int64{}, nextID: 1 << 20, fnName: "<module>"}
}

// EvalProgram evaluates prog's top-level statements and returns the value of
// the final expression statement (or last assignment). It returns an error on
// unsupported constructs.

// collect implements a mark-and-sweep memory model pass. Roots are the
// top-level environment bindings (e.Vars). It marks every heap object
// reachable through containers (list/dict/set) and closure environments,
// then sweeps unreachable objects. It runs before each top-level statement:
// values from the prior statement that were bound into Vars are marked and
// kept; values that were only temporaries are freed.
func (e *Evaluator) Collect() {
	if len(e.heap) == 0 {
		return
	}
	marked := map[int64]bool{}
	var mark func(id int64)
	mark = func(id int64) {
		if id <= 0 || marked[id] {
			return
		}
		o, ok := e.heap[id]
		if !ok {
			return
		}
		marked[id] = true
		switch o.kind {
		case "list", "set":
			for _, v := range o.elems {
				mark(v)
			}
		case "dict":
			for _, k := range o.elems {
				mark(k)
			}
			for _, v := range o.dvals {
				mark(v)
			}
		case "closure":
			for _, v := range o.env {
				mark(v)
			}
		}
		// class/object/import/method kinds store ids in attrs (methods, fields).
		for _, v := range o.attrs {
			mark(v)
		}
		for _, v := range o.env {
			mark(v)
		}
	}
	for _, id := range e.Vars {
		mark(id)
	}
	// generational sweep: young GC reclaims unreachable nursery objects and
	// promotes survivors (advance nurseryBase so they become old); a full GC
	// sweeps the whole heap when the old generation grows past a threshold.
	oldCount := 0
	for id := range e.heap {
		if id < e.nurseryBase {
			oldCount++
		}
	}
	young := e.nurseryBase
	if young == 0 || oldCount > 512 {
		// full GC over the whole heap, then reset the nursery to all-new
		for id, o := range e.heap {
			if !marked[id] {
				switch o.kind {
				case "list", "dict", "set", "str", "int", "float":
					if !marked[id] {
						delete(e.heap, id)
					}
				}
			}
		}
		e.nurseryBase = maxHeapID(e.heap)
		e.allocCount = 0
		return
	}
	// young GC: reclaim unreachable nursery objects, promote survivors
	for id, o := range e.heap {
		if id >= young && !marked[id] {
			switch o.kind {
			case "list", "dict", "set", "str", "int", "float":
				if !marked[id] {
					delete(e.heap, id)
				}
			}
		}
	}
	e.nurseryBase = maxHeapID(e.heap)
	e.allocCount = 0
}

func (e *Evaluator) EvalProgram(prog *Program) (int64, error) {
	var last int64
	for _, st := range prog.Stmts {
		e.cur = st.Span()
		switch s := st.(type) {
		case *ClassDef:
			classID := e.allocObj("class")
			e.classIDs[s.Name] = classID
			cls := e.heap[classID]
			cls.doc = s.Doc
			// inheritance: the first base (if any) becomes the base class;
			// methods/attrs missing on the subclass resolve up the base chain.
			if len(s.Bases) > 0 {
				if baseID, ok := e.classIDs[s.Bases[0].Value]; ok {
					cls.base = baseID
				}
			}
			for _, m := range s.Body {
				fd, ok := m.(*FuncDef)
				if !ok {
					continue
				}
				methodID := e.allocObj("method")
				mo := e.heap[methodID]
				mo.class = s.Name
				mo.mname = fd.Name
				mo.fn = fd
				cls.attrs[fd.Name] = methodID
			}
		case *ImportStmt:
			if err := e.importModule(s.Module); err != nil {
				return 0, err
			}
			continue
		case *ExternDecl:
			e.externs[s.Name] = s
		case *FuncDef:
			// Nested defs bind a closure capturing the enclosing scope.
			if len(s.Decorators) > 0 {
				// @dec1 @dec2 def f -> f = dec2(dec1(f)). Build the base fn as a
				// closure so it can be passed to decorators as an int64 id.
				e.Vars[s.Name] = e.allocClosure(s, e.Vars)
				for _, dec := range s.Decorators {
					decVal, err := e.evalDecorator(dec)
					if err != nil {
						return 0, err
					}
					newFn, err := e.callDecValue(decVal, e.Vars[s.Name])
					if err != nil {
						return 0, err
					}
					e.Vars[s.Name] = newFn
				}
				continue
			}
			if e.inCall {
				e.Vars[s.Name] = e.allocClosure(s, e.Vars)
			} else {
				e.funcs[s.Name] = s
			}
			continue
		case *IfStmt:
			cond, err := e.eval(s.Cond)
			if err != nil {
				return 0, err
			}
			taken := false
			if cond != 0 {
				rv, err := e.evalBody(s.Then)
				if err != nil {
					return 0, err
				}
				last = rv
				taken = true
			} else {
				for _, eif := range s.Elifs {
					ec, err := e.eval(eif.Cond)
					if err != nil {
						return 0, err
					}
					if ec != 0 {
						rv, err := e.evalBody(eif.Then)
						if err != nil {
							return 0, err
						}
						last = rv
						taken = true
						break
					}
				}
			}
			if !taken && s.Else != nil {
				rv, err := e.evalBody(s.Else)
				if err != nil {
					return 0, err
				}
				last = rv
			}
			continue
		case *MatchStmt:
			sub, err := e.eval(s.Subject)
			if err != nil {
				return 0, err
			}
			for _, c := range s.Cases {
				matched := false
				for _, p := range append([]Expr{c.Pattern}, c.Or...) {
					ok, err := e.matchPattern(sub, p)
					if err != nil {
						return 0, err
					}
					if ok {
						matched = true
						break
					}
				}
				if matched && c.Guard != nil {
					gv, err := e.eval(c.Guard)
					if err != nil {
						return 0, err
					}
					if gv == 0 {
						matched = false
					}
				}
				if matched {
					rv, err := e.evalBody(c.Body)
					if err != nil {
						return 0, err
					}
					last = rv
					break
				}
			}
			continue
		case *TryStmt:
			_, bodyErr := e.evalBody(s.Body)
			if bodyErr != nil {
				caught := false
				for _, ec := range s.Excepts {
					// bare except, or except Exception, matches any exception;
					// otherwise match the raised exception class name exactly.
					matches := ec.Exn == nil || ec.Exn.Value == "Exception"
					if ee, ok := bodyErr.(*EvalError); ok && ee.ExnType != "" {
						matches = ec.Exn == nil || ec.Exn.Value == "Exception" ||
							ec.Exn.Value == ee.ExnType
					}
					if matches {
						_, err2 := e.evalBody(ec.Body)
						if err2 != nil {
							return 0, err2
						}
						caught = true
						break
					}
				}
				if !caught {
					return 0, bodyErr
				}
			}
			if len(s.Finally) > 0 {
				_, err := e.evalBody(s.Finally)
				if err != nil {
					return 0, err
				}
			}
	case *WithStmt:
		m, err := e.eval(s.Expr)
		if err != nil {
			return 0, err
		}
		ent, err := e.callDunder(m, "__enter__", nil)
		if err != nil {
			return 0, err
		}
		if s.As != nil {
			e.Vars[s.As.Value] = ent
		}
		_, bodyErr := e.evalBody(s.Body)
		if bodyErr != nil {
			if ee, ok := bodyErr.(*EvalError); ok {
				sup, err2 := e.callDunder(m, "__exit__", []int64{int64(len(ee.ExnType)), 0, 0})
				if err2 != nil {
					return 0, err2
				}
				if sup != 0 {
					return 0, nil
				}
				return 0, bodyErr
			}
			_, err2 := e.callDunder(m, "__exit__", []int64{0, 0, 0})
			if err2 != nil {
				return 0, err2
			}
			return 0, bodyErr
		}
		_, err = e.callDunder(m, "__exit__", []int64{0, 0, 0})
		if err != nil {
			return 0, err
		}
		continue
		case *WhileStmt:
			completed := true
			for {
				cond, err := e.eval(s.Cond)
				if err != nil {
					return 0, err
				}
				if cond == 0 {
					break
				}
				rv, err := e.evalBody(s.Body)
				if err != nil {
					if ls, ok := err.(*loopSignal); ok {
						if ls.kind == "break" {
							completed = false
							break
						}
						continue
					}
					return 0, err
				}
				last = rv
			}
			if completed {
				rv, err := e.evalBody(s.Else)
				if err != nil {
					return 0, err
				}
				last = rv
			}
			continue
		case *ForStmt:
			isRange := false
			if c, ok := s.Iter.(*Call); ok {
				if n, ok2 := c.Fn.(*Name); ok2 && n.Value == "range" {
					isRange = true
				}
			}
			completed := true
			if n := s.Var; n != nil {
				if !isRange {
					itV, err := e.eval(s.Iter)
					if err != nil {
						return 0, err
					}
					if o, ok := e.heap[itV]; ok && (o.kind == "list" || o.kind == "set" || o.kind == "dict" || o.kind == "str") {
						if o.kind == "str" {
							for _, r := range o.sval {
								if err := e.setLoopVar(s.Var, e.allocStr(string(r))); err != nil {
									return 0, err
								}
								rv, err := e.evalBody(s.Body)
								if err != nil {
									if ls, ok := err.(*loopSignal); ok {
										if ls.kind == "break" {
											completed = false
											break
										}
										continue
									}
									return 0, err
								}
								last = rv
							}
						} else {
							for _, el := range o.elems {
								if err := e.setLoopVar(s.Var, el); err != nil {
									return 0, err
								}
								rv, err := e.evalBody(s.Body)
								if err != nil {
									if ls, ok := err.(*loopSignal); ok {
										if ls.kind == "break" {
											completed = false
											break
										}
										continue
									}
									return 0, err
								}
								last = rv
							}
						}
					} else {
						start, stop, err := e.rangeBounds(s.Iter)
						if err != nil {
							return 0, err
						}
						step, err := e.rangeStep(s.Iter)
						if err != nil {
							return 0, err
						}
						for i := start; (step > 0 && i < stop) || (step < 0 && i > stop); i += step {
							if err := e.setLoopVar(s.Var, i); err != nil {
								return 0, err
							}
							rv, err := e.evalBody(s.Body)
							if err != nil {
								if ls, ok := err.(*loopSignal); ok {
									if ls.kind == "break" {
										completed = false
										break
									}
									continue
								}
								return 0, err
							}
							last = rv
						}
					}
				} else {
					start, stop, err := e.rangeBounds(s.Iter)
					if err != nil {
						return 0, err
					}
					step, err := e.rangeStep(s.Iter)
					if err != nil {
						return 0, err
					}
					for i := start; (step > 0 && i < stop) || (step < 0 && i > stop); i += step {
						if err := e.setLoopVar(s.Var, i); err != nil {
							return 0, err
						}
						rv, err := e.evalBody(s.Body)
						if err != nil {
							if ls, ok := err.(*loopSignal); ok {
								if ls.kind == "break" {
									completed = false
									break
								}
								continue
							}
							return 0, err
						}
						last = rv
					}
				}
			}
			if completed {
				rv, err := e.evalBody(s.Else)
				if err != nil {
					return 0, err
				}
				last = rv
			}
		case *AssignStmt:
			v, err := e.eval(s.Value)
			if err != nil {
				return 0, err
			}
			if s.Annot != nil {
				name := ""
				if n, ok := s.Target.(*Name); ok {
					name = n.Value
				}
				if err := e.checkAnnot(name, s.Annot, v); err != nil {
					return 0, err
				}
			}
			if n, ok := s.Target.(*Name); ok {
				e.Vars[n.Value] = v
				last = v
			}
			if t, ok := s.Target.(*Tuple); ok {
				obj := e.heap[v]
				if obj == nil {
					return 0, &EvalError{Msg: "cannot unpack non-iterable value"}
				}
				if len(obj.elems) != len(t.Elems) {
					return 0, &EvalError{Msg: "cannot unpack value into tuple"}
				}
				for i, nm := range t.Elems {
					if n2, ok2 := nm.(*Name); ok2 {
						e.Vars[n2.Value] = obj.elems[i]
					}
				}
				last = v
			}

			if a, ok := s.Target.(*Attr); ok {
				objV, err := e.eval(a.Obj)
				if err != nil {
					return 0, err
				}
				o, ok := e.heap[objV]
				if ok && o.kind == "instance" {
					o.attrs[a.Name.Value] = v
					last = v
				}
			}
		case *AugAssignStmt:
			// value = target op rhs, then write the result back to the target.
			b := &BinOp{Op: s.Op, L: s.Target, R: s.Value}
			res, err := e.eval(b)
			if err != nil {
				return 0, err
			}
			switch t := s.Target.(type) {
			case *Name:
				e.Vars[t.Value] = res
				last = res
			case *Attr:
				objV, err := e.eval(t.Obj)
				if err != nil {
					return 0, err
				}
				o, ok := e.heap[objV]
				if ok && o.kind == "instance" {
					o.attrs[t.Name.Value] = res
					last = res
				}
			default:
				return 0, fmt.Errorf("augmented assignment: unsupported target %T", s.Target)
			}
		case *ExprStmt:
			v, err := e.eval(s.Expr)
			if err != nil {
				return 0, err
			}
			last = v
		case *ReturnStmt:
			if s.Expr != nil {
				v, err := e.eval(s.Expr)
				if err != nil {
					return 0, err
				}
				if e.curRet != nil {
					if err := e.checkAnnot("return", e.curRet, v); err != nil {
						return 0, err
					}
				}
				return 0, &returnSignal{val: v}
			}
			return 0, &returnSignal{val: 0}
		case *YieldStmt:
			v, err := e.eval(s.Expr)
			if err != nil {
				return 0, err
			}
			if e.yieldList != 0 {
				if o, ok := e.heap[e.yieldList]; ok {
					o.elems = append(o.elems, v)
				}
				last = v
				continue
			}
			last = v
			continue
	case *YieldFromStmt:
		if e.yieldList != 0 {
			gl, ok := e.heap[e.yieldList]
			if ok {
				elems, err := e.evalIterable(s.Expr)
				if err != nil {
					return 0, err
				}
				gl.elems = append(gl.elems, elems...)
				continue
			}
		}
		return 0, &EvalError{Msg: "yield from outside a generator"}
		case *RaiseStmt:
			// raise Exception("msg") / raise ValueError("msg") etc.
			if s.Expr == nil {
				return 0, exnError("Exception", "raised")
			}
			v, err := e.eval(s.Expr)
			if err != nil {
				return 0, err
			}
			et, em := e.exnInfo(v)
			return 0, exnError(et, em)
		case *BreakStmt:
			return 0, &loopSignal{kind: "break"}
		case *ContinueStmt:
			return 0, &loopSignal{kind: "continue"}
		case *PassStmt:
			// no-op statement
			continue
		default:
			return 0, &EvalError{Msg: "unsupported statement for eval"}
		}
	}
	return last, nil
}

func (e *Evaluator) eval(x Expr) (int64, error) {
	switch n := x.(type) {
	case *IntLit:
		return n.Value, nil
	case *StrLit:
		return e.allocStr(n.Value), nil
	case *FString:
		var b strings.Builder
		for _, part := range n.Parts {
			if part.Lit != "" {
				b.WriteString(part.Lit)
				continue
			}
			v, err := e.eval(part.Expr)
			if err != nil {
				return 0, err
			}
			b.WriteString(e.Repr(v))
		}
		return e.allocStr(b.String()), nil
	case *FloatLit:
		return e.allocFloat(n.Value), nil
	case *BoolLit:
		if n.Value {
			return 1, nil
		}
		return 0, nil
	case *NoneLit:
		return 0, nil
	case *Name:
		if v, ok := e.Vars[n.Value]; ok {
			return v, nil
		}
		if id, ok := e.classIDs[n.Value]; ok {
			return id, nil
		}
		return 0, &EvalError{Msg: "undefined name " + n.Value}
	case *BinOp:
		return e.evalBin(n)
	case *UnOp:
		v, err := e.eval(n.X)
		if err != nil {
			return 0, err
		}
		switch n.Op {
		case "-":
			if fv, ok := e.floatOf(v); ok {
				return e.allocFloat(-fv), nil
			}
			return -v, nil
		case "not":
			if v == 0 {
				return 1, nil
			}
			return 0, nil
		}
		return 0, &EvalError{Msg: "unsupported unary " + n.Op}
	case *Attr:
		// __doc__ introspection on a top-level def/class name: these have no
		// runtime object (top-level functions live in e.funcs as AST nodes).
		if n.Name.Value == "__doc__" {
			if nm, ok := n.Obj.(*Name); ok {
				if fd, ok := e.funcs[nm.Value]; ok {
					return e.allocStr(fd.Doc), nil
				}
				if cid, ok := e.classIDs[nm.Value]; ok {
					if c := e.heap[cid]; c != nil {
						return e.allocStr(c.doc), nil
					}
				}
			}
		}
		objV, err := e.eval(n.Obj)
		if err != nil {
			return 0, err
		}
		if n.Name.Value == "__doc__" {
			// __doc__ on a closure/class value: the object carries doc on the
			// heap object itself (not in attrs).
			if o, ok := e.heap[objV]; ok {
				return e.allocStr(o.doc), nil
			}
		}
		o, ok := e.heap[objV]
		if !ok {
			return 0, &EvalError{Msg: "attribute access on non-object"}
		}
		if o.kind == "module" {
			// mod.attr resolves a top-level name from the imported module.
			if v, ok := o.attrs[n.Name.Value]; ok {
				return v, nil
			}
			return 0, &EvalError{Msg: "no name " + n.Name.Value + " in module"}
		}
		if o.kind == "instance" {
			if v, ok := o.attrs[n.Name.Value]; ok {
				return v, nil
			}
			classID, ok := e.classIDs[o.class]
			if !ok {
				return 0, &EvalError{Msg: "unknown class " + o.class}
			}
			// inheritance: fall back to the class and its bases.
			if mID, ok := e.resolveMethod(classID, n.Name.Value); ok {
				e.heap[mID].recv = objV
				return mID, nil
			}
			return 0, &EvalError{Msg: "no attribute " + n.Name.Value}
		}
		if o.kind == "class" {
			if mID, ok := e.resolveMethod(e.classIDFor(objV), n.Name.Value); ok {
				return mID, nil
			}
			return 0, &EvalError{Msg: "no method " + n.Name.Value}
		}
		if o.kind == "superproxy" {
			// super() proxy: resolve methods on the base class only, bound to
			// the current instance (o.recv), so overridden methods can delegate.
			baseID := o.base
			if mID, ok := e.resolveMethod(baseID, n.Name.Value); ok {
				e.heap[mID].recv = o.recv
				return mID, nil
			}
			return 0, &EvalError{Msg: "no method " + n.Name.Value}
		}
		return 0, &EvalError{Msg: "attribute access on method"}
	case *Call:
		return e.evalCall(n)
	case *ListLit:
		h := e.allocObj("list")
		o := e.heap[h]
		for _, el := range n.Elems {
			ev, err := e.eval(el)
			if err != nil {
				return 0, err
			}
			o.elems = append(o.elems, ev)
		}
		return h, nil
	case *Tuple:
		h := e.allocObj("list")
		o := e.heap[h]
		for _, el := range n.Elems {
			ev, err := e.eval(el)
			if err != nil {
				return 0, err
			}
			o.elems = append(o.elems, ev)
		}
		return h, nil
	case *CondExpr:
		// ternary `then if cond else otherwise`: choose the branch by truthiness.
		cond, err := e.eval(n.Cond)
		if err != nil {
			return 0, err
		}
		if cond != 0 {
			return e.eval(n.If)
		}
		return e.eval(n.Else)

	case *Index:
		objV, err := e.eval(n.Obj)
		if err != nil {
			return 0, err
		}
		idx, err := e.eval(n.Idx)
		if err != nil {
			return 0, err
		}
		o := e.heap[objV]
		if o == nil {
			return 0, &EvalError{Msg: "cannot index null"}
		}
		switch o.kind {
		case "list":
			if idx < 0 || idx >= int64(len(o.elems)) {
				return 0, &EvalError{Msg: "index out of range"}
			}
			return o.elems[idx], nil
		case "dict":
			for i, k := range o.elems {
				if e.dictKeyEq(k, idx) {
					return o.dvals[i], nil
				}
			}
			return 0, &EvalError{Msg: "key not found"}
		case "set":
			for _, el := range o.elems {
				if el == idx {
					return el, nil
				}
			}
			return 0, &EvalError{Msg: "not in set"}
		case "str":
			if idx < 0 || idx >= int64(len(o.sval)) {
				return 0, &EvalError{Msg: "string index out of range"}
			}
			return int64(o.sval[idx]), nil
		default:
			return 0, &EvalError{Msg: "cannot index this value"}
		}

	case *Slice:
		objH, err := e.eval(n.Obj)
		if err != nil {
			return 0, err
		}
		o := e.heap[objH]
		if o == nil {
			return 0, &EvalError{Msg: "slice of null"}
		}
		if o.kind != "list" && o.kind != "str" {
			return 0, &EvalError{Msg: "cannot slice this value"}
		}
		lowRaw := int64(0)
		highRaw := int64(0)
		step := int64(1)
		if n.Low != nil {
			v, err := e.eval(n.Low)
			if err != nil {
				return 0, err
			}
			lowRaw = v
		}
		if n.High != nil {
			v, err := e.eval(n.High)
			if err != nil {
				return 0, err
			}
			highRaw = v
		}
		if n.Step != nil {
			v, err := e.eval(n.Step)
			if err != nil {
				return 0, err
			}
			step = v
			if step == 0 {
				return 0, &EvalError{Msg: "slice step cannot be zero"}
			}
		}
		length := int64(0)
		if o.kind == "str" {
			length = int64(len(o.sval))
		} else {
			length = int64(len(o.elems))
		}
		start, stop, stp := pySliceIndices(lowRaw, highRaw, step, n.Low != nil, n.High != nil, length)
		if o.kind == "str" {
			var sb strings.Builder
			for i := start; (stp > 0 && i < stop) || (stp < 0 && i > stop); i += stp {
				sb.WriteByte(o.sval[i])
			}
			return e.allocStr(sb.String()), nil
		}
		var elems []int64
		for i := start; (stp > 0 && i < stop) || (stp < 0 && i > stop); i += stp {
			elems = append(elems, o.elems[i])
		}
		nh := e.allocObj("list")
		e.heap[nh].elems = elems
		return nh, nil

	case *DictLit:
		h := e.allocObj("dict")
		o := e.heap[h]
		for i, k := range n.Keys {
			kv, err := e.eval(k)
			if err != nil {
				return 0, err
			}
			vv, err := e.eval(n.Vals[i])
			if err != nil {
				return 0, err
			}
			o.elems = append(o.elems, kv)
			o.dvals = append(o.dvals, vv)
		}
		return h, nil
	case *SetLit:
		h := e.allocObj("set")
		o := e.heap[h]
		for _, el := range n.Elems {
			v, err := e.eval(el)
			if err != nil {
				return 0, err
			}
			found := false
			for _, x := range o.elems {
				if x == v {
					found = true
					break
				}
			}
			if !found {
				o.elems = append(o.elems, v)
			}
		}
		return h, nil
	case *Comp:
		return e.evalComp(n)
	case *Generator:
		return e.evalGen(n)
	case *Lambda:
		// lambda params: body => anonymous FuncDef + closure capturing env.
		fd := &FuncDef{
			Name:   "lambda",
			Params: n.Params,
			Body:   []Stmt{&ReturnStmt{Expr: n.Body}},
		}
		h := e.allocClosure(fd, e.Vars)
		return h, nil
	default:
		return 0, &EvalError{Msg: "unsupported expression for eval"}
	}
}

func (e *Evaluator) evalComp(c *Comp) (int64, error) {
	it, err := e.eval(c.Iter)
	if err != nil {
		return 0, err
	}
	var items []int64
	var o *obj
	if h, ok := e.heap[it]; ok {
		o = h
		items = h.elems
	} else {
		lo, hi, err := e.rangeBounds(c.Iter)
		if err != nil {
			return 0, &EvalError{Msg: "comprehension over non-object"}
		}
		step := int64(1)
		if r, ok := c.Iter.(*Call); ok && len(r.Args) == 3 {
			step, err = e.eval(r.Args[2])
			if err != nil {
				return 0, err
			}
			if step == 0 {
				return 0, &EvalError{Msg: "range step cannot be zero"}
			}
		}
		if step > 0 {
			for v := lo; v < hi; v += step {
				items = append(items, v)
			}
		} else {
			for v := lo; v > hi; v += step {
				items = append(items, v)
			}
		}
	}
	if o != nil && o.kind == "dict" {
		items = o.elems
	}
	var rh int64
	switch c.Kind {
	case CompList:
		rh = e.allocObj("list")
	case CompSet:
		rh = e.allocObj("set")
	case CompDict:
		rh = e.allocObj("dict")
	default:
		return 0, &EvalError{Msg: "unknown comprehension kind"}
	}
	ro := e.heap[rh]
	for _, item := range items {
		e.Vars[c.ForVar.Value] = item
		if c.Cond != nil {
			cv, err := e.eval(c.Cond)
			if err != nil {
				return 0, err
			}
			if cv == 0 {
				continue
			}
		}
		switch c.Kind {
		case CompList:
			v, err := e.eval(c.Elems[0])
			if err != nil {
				return 0, err
			}
			ro.elems = append(ro.elems, v)
		case CompSet:
			v, err := e.eval(c.Elems[0])
			if err != nil {
				return 0, err
			}
			dup := false
			for _, x := range ro.elems {
				if x == v {
					dup = true
					break
				}
			}
			if !dup {
				ro.elems = append(ro.elems, v)
			}
		case CompDict:
			k, err := e.eval(c.Keys[0])
			if err != nil {
				return 0, err
			}
			v, err := e.eval(c.Vals[0])
			if err != nil {
				return 0, err
			}
			ro.elems = append(ro.elems, k)
			ro.dvals = append(ro.dvals, v)
		}
	}
	return rh, nil
}
func (e *Evaluator) evalGen(g *Generator) (int64, error) {
	itv, err := e.eval(g.Iter)
	if err != nil {
		return 0, err
	}
	var items []int64
	if o, ok := e.heap[itv]; ok {
		switch o.kind {
		case "list", "str", "dict", "set":
			items = o.elems
		}
	}
	// generator expression evaluates eagerly to a list of yielded values.
	id := e.allocObj("list")
	lo := e.heap[id]
	for _, it := range items {
		e.Vars[g.ForVar.Value] = it
		if g.Cond != nil {
			cv, err := e.eval(g.Cond)
			if err != nil {
				return 0, err
			}
			if cv == 0 {
				continue
			}
		}
		for _, el := range g.Elems {
			ev, err := e.eval(el)
			if err != nil {
				return 0, err
			}
			lo.elems = append(lo.elems, ev)
		}
	}
	return id, nil
}

// eqVal reports value equality between two handles, mirroring `==` semantics:
// numeric equality via float promotion, string content equality, and handle
// identity for everything else.
func (e *Evaluator) eqVal(l, r int64) bool {
	if lf, ok := e.floatOf(l); ok {
		rf, rfok := e.floatOf(r)
		if !rfok {
			rf = float64(r)
		}
		return lf == rf
	}
	if rf, ok := e.floatOf(r); ok {
		if lf, ok := e.floatOf(l); ok {
			return lf == rf
		}
		return false
	}
	if lo, ok := e.heap[l]; ok && lo.kind == "str" {
		if ro, ok := e.heap[r]; ok && ro.kind == "str" {
			return lo.sval == ro.sval
		}
		return false
	}
	return l == r
}

// contains reports whether container holds v: list/set element membership, dict
// key membership, or (for a string container) substring membership.
func (e *Evaluator) contains(container, v int64) (bool, error) {
	co, ok := e.heap[container]
	if !ok {
		return false, nil
	}
	switch co.kind {
	case "list", "set":
		for _, el := range co.elems {
			if e.eqVal(v, el) {
				return true, nil
			}
		}
		return false, nil
	case "dict":
		for _, k := range co.elems {
			if e.eqVal(v, k) {
				return true, nil
			}
		}
		return false, nil
	case "str":
		if vo, ok := e.heap[v]; ok && vo.kind == "str" {
			return strings.Contains(co.sval, vo.sval), nil
		}
		return false, nil
	}
	return false, nil
}


// dunderForBinOp returns the dunder method name for a binary operator, if any.
func dunderForBinOp(op string) string {
	switch op {
	case "+":
		return "__add__"
	case "-":
		return "__sub__"
	case "*":
		return "__mul__"
	case "/":
		return "__truediv__"
	case "//":
		return "__floordiv__"
	case "%":
		return "__mod__"
	case "**":
		return "__pow__"
	case "==":
		return "__eq__"
	case "!=":
		return "__ne__"
	case "<":
		return "__lt__"
	case "<=":
		return "__le__"
	case ">":
		return "__gt__"
	case ">=":
		return "__ge__"
	}
	return ""
}

// reflectedDunder returns the reflected dunder name for a dunder method name,
// used when the left operand is not overloaded but the right operand is.
func reflectedDunder(name string) string {
	switch name {
	case "__add__":
		return "__radd__"
	case "__sub__":
		return "__rsub__"
	case "__mul__":
		return "__rmul__"
	case "__truediv__":
		return "__rtruediv__"
	case "__floordiv__":
		return "__rfloordiv__"
	case "__mod__":
		return "__rmod__"
	case "__pow__":
		return "__rpow__"
	case "__eq__":
		return "__eq__"
	case "__ne__":
		return "__ne__"
	case "__lt__":
		return "__gt__"
	case "__le__":
		return "__ge__"
	case "__gt__":
		return "__lt__"
	case "__ge__":
		return "__le__"
	}
	return ""
}

// dunderCall resolves and invokes a dunder method `name` on the instance at
// handle `self`, binding self as the receiver and `args` as the arguments.
// It reports whether the instance has such a method, and any call error.
func (e *Evaluator) dunderCall(self int64, name string, args ...int64) (int64, bool, error) {
	o := e.heap[self]
	if o == nil || o.kind != "instance" {
		return 0, false, nil
	}
	mID, ok := e.resolveMethod(e.classIDs[o.class], name)
	if !ok {
		return 0, false, nil
	}
	rv, err := e.callMethod(e.heap[mID], self, args)
	if err != nil {
		return 0, true, err
	}
	return rv, true, nil
}

func (e *Evaluator) evalBin(n *BinOp) (int64, error) {
	l, err := e.eval(n.L)
	if err != nil {
		return 0, err
	}
	r, err := e.eval(n.R)
	if err != nil {
		return 0, err
	}
	// Operator overloading (dunder dispatch): if an operand is a class instance
	// with a dunder method for this operator, call it. If the left operand is
	// not overloaded, fall back to a reflected (__r__) method on the right.
	if name := dunderForBinOp(n.Op); name != "" {
		if o := e.heap[l]; o != nil && o.kind == "instance" {
			rv, found, err := e.dunderCall(l, name, r)
			if found {
				return rv, err
			}
		}
		if o := e.heap[r]; o != nil && o.kind == "instance" {
			if rname := reflectedDunder(name); rname != "" {
				rv, found, err := e.dunderCall(r, rname, l)
				if found {
					return rv, err
				}
			}
		}
	}
	switch n.Op {
	case "+":
		if lo, ok := e.heap[l]; ok && lo.kind == "str" {
			rs := e.strOf(r)
			if rs == "" {
				return 0, &EvalError{Msg: "cannot concatenate string and non-string"}
			}
			return e.allocStr(lo.sval + rs), nil
		}
		if ro, ok := e.heap[r]; ok && ro.kind == "str" {
			return 0, &EvalError{Msg: "cannot concatenate non-string and string"}
		}
		if lf, ok := e.floatOf(l); ok {
			rf, rfok := e.floatOf(r)
			if !rfok {
				rf = float64(r)
			}
			return e.allocFloat(lf + rf), nil
		}
		if rf, ok := e.floatOf(r); ok {
			return e.allocFloat(float64(l) + rf), nil
		}
		return l + r, nil
	case "-":
		if lf, ok := e.floatOf(l); ok {
			rf, rfok := e.floatOf(r)
			if !rfok {
				rf = float64(r)
			}
			return e.allocFloat(lf - rf), nil
		}
		if rf, ok := e.floatOf(r); ok {
			return e.allocFloat(float64(l) - rf), nil
		}
		return l - r, nil
	case "*":
		if lf, ok := e.floatOf(l); ok {
			rf, rfok := e.floatOf(r)
			if !rfok {
				rf = float64(r)
			}
			return e.allocFloat(lf * rf), nil
		}
		if rf, ok := e.floatOf(r); ok {
			return e.allocFloat(float64(l) * rf), nil
		}
		return l * r, nil
	case "/", "//":
		floor := n.Op == "//"
		if lf, ok := e.floatOf(l); ok {
			rf, rfok := e.floatOf(r)
			if !rfok {
				rf = float64(r)
			}
			if rf == 0 {
				return 0, &EvalError{Msg: "division by zero"}
			}
			q := lf / rf
			if floor {
				q = math.Floor(q)
			}
			return e.allocFloat(q), nil
		}
		if rf, ok := e.floatOf(r); ok {
			if rf == 0 {
				return 0, &EvalError{Msg: "division by zero"}
			}
			q := float64(l) / rf
			if floor {
				q = math.Floor(q)
			}
			return e.allocFloat(q), nil
		}
		if r == 0 {
			return 0, &EvalError{Msg: "division by zero"}
		}
		return l / r, nil
	case "%":
		if lf, ok := e.floatOf(l); ok {
			rf, rfok := e.floatOf(r)
			if !rfok {
				rf = float64(r)
			}
			if rf == 0 {
				return 0, &EvalError{Msg: "division by zero"}
			}
			return e.allocFloat(math.Mod(lf, rf)), nil
		}
		if rf, ok := e.floatOf(r); ok {
			if rf == 0 {
				return 0, &EvalError{Msg: "division by zero"}
			}
			return e.allocFloat(math.Mod(float64(l), rf)), nil
		}
		if r == 0 {
			return 0, &EvalError{Msg: "division by zero"}
		}
		return l % r, nil
	case "==":
		if e.eqVal(l, r) {
			return 1, nil
		}
		return 0, nil
	case "is":
		if l == r {
			return 1, nil
		}
		return 0, nil
	case "is not":
		if l != r {
			return 1, nil
		}
		return 0, nil
	case "in", "not in":
		found, err := e.contains(r, l)
		if err != nil {
			return 0, err
		}
		if n.Op == "in" {
			if found {
				return 1, nil
			}
			return 0, nil
		}
		if found {
			return 0, nil
		}
		return 1, nil
	case "!=":
		if l != r {
			return 1, nil
		}
		return 0, nil
	case "<":
		if lf, ok := e.floatOf(l); ok {
			rf, rfok := e.floatOf(r)
			if !rfok {
				rf = float64(r)
			}
			if lf < rf {
				return 1, nil
			}
			return 0, nil
		}
		if rf, ok := e.floatOf(r); ok {
			if float64(l) < rf {
				return 1, nil
			}
			return 0, nil
		}
		if l < r {
			return 1, nil
		}
		return 0, nil
	case "<=":
		if lf, ok := e.floatOf(l); ok {
			rf, rfok := e.floatOf(r)
			if !rfok {
				rf = float64(r)
			}
			if lf <= rf {
				return 1, nil
			}
			return 0, nil
		}
		if rf, ok := e.floatOf(r); ok {
			if float64(l) <= rf {
				return 1, nil
			}
			return 0, nil
		}
		if l <= r {
			return 1, nil
		}
		return 0, nil
	case ">":
		if lf, ok := e.floatOf(l); ok {
			rf, rfok := e.floatOf(r)
			if !rfok {
				rf = float64(r)
			}
			if lf > rf {
				return 1, nil
			}
			return 0, nil
		}
		if rf, ok := e.floatOf(r); ok {
			if float64(l) > rf {
				return 1, nil
			}
			return 0, nil
		}
		if l > r {
			return 1, nil
		}
		return 0, nil
	case ">=":
		if lf, ok := e.floatOf(l); ok {
			rf, rfok := e.floatOf(r)
			if !rfok {
				rf = float64(r)
			}
			if lf >= rf {
				return 1, nil
			}
			return 0, nil
		}
		if rf, ok := e.floatOf(r); ok {
			if float64(l) >= rf {
				return 1, nil
			}
			return 0, nil
		}
		if l >= r {
			return 1, nil
		}
		return 0, nil
	case "and":
		if l != 0 && r != 0 {
			return 1, nil
		}
		return 0, nil
	case "or":
		if l != 0 || r != 0 {
			return 1, nil
		}
		return 0, nil
	case "**":
		if lf, ok := e.floatOf(l); ok {
			rf, rfok := e.floatOf(r)
			if !rfok {
				rf = float64(r)
			}
			return e.allocFloat(math.Pow(lf, rf)), nil
		}
		if rf, ok := e.floatOf(r); ok {
			return e.allocFloat(math.Pow(float64(l), rf)), nil
		}
		// exact integer power (binary exponentiation); negative exponents
		// yield 0 for an integer result, mirroring Python's int ** int.
		if r < 0 {
			return 0, nil
		}
		base, exp := l, r
		res := int64(1)
		for exp > 0 {
			if exp&1 != 0 {
				res *= base
			}
			exp >>= 1
			if exp > 0 {
				base *= base
			}
		}
		return res, nil
	}
	return 0, &EvalError{Msg: "unsupported operator " + n.Op}
}

func (e *Evaluator) matchPattern(sub int64, p Expr) (bool, error) {
	switch t := p.(type) {
	case *ListLit:
		o, ok := e.heap[sub]
		if !ok || o.kind != "list" || len(o.elems) != len(t.Elems) {
			return false, nil
		}
		for i, pe := range t.Elems {
			if n, ok2 := pe.(*Name); ok2 && n.Value != "_" {
				e.Vars[n.Value] = o.elems[i]
				continue
			}
			ev, err := e.eval(pe)
			if err != nil {
				return false, err
			}
			if ev != o.elems[i] {
				return false, nil
			}
		}
		return true, nil
	case *DictLit:
		o, ok := e.heap[sub]
		if !ok || o.kind != "dict" {
			return false, nil
		}
		for i, k := range t.Keys {
			kv, err := e.eval(k)
			if err != nil {
				return false, err
			}
			found := false
			for j, k2 := range o.elems {
				if e.dictKeyEq(kv, k2) {
					if n, ok2 := t.Vals[i].(*Name); ok2 && n.Value != "_" {
						e.Vars[n.Value] = o.dvals[j]
					} else {
						vv, err := e.eval(t.Vals[i])
						if err != nil {
							return false, err
						}
						if vv != o.dvals[j] {
							return false, nil
						}
					}
					found = true
					break
				}
			}
			if !found {
				return false, nil
			}
		}
		return true, nil
	case *Name:
		// Bare-name capture pattern: bind the subject to the name and
		// always match (Python `case x:` semantics).
		if t.Value != "_" {
			e.Vars[t.Value] = sub
		}
		return true, nil
	case *Call:
		// Class pattern: `case Point(x, y):` matches an instance of Point
		// (or a subclass) and binds attributes x and y to the instance's
		// values. If the callee is not a known class, fall back to the
		// default expression-equality behavior.
		fnName, ok := t.Fn.(*Name)
		if ok {
			if pid, ok2 := e.resolveClassID(fnName.Value); ok2 {
				o, ok3 := e.heap[sub]
				if !ok3 || o.kind != "instance" {
					return false, nil
				}
				// The subject must be an instance of pid or a subclass of it.
				instCID, ok4 := e.classIDs[o.class]
				if !ok4 {
					return false, nil
				}
				matched := false
				for c := instCID; c != 0; c = e.heap[c].base {
					if c == pid {
						matched = true
						break
					}
				}
				if !matched {
					return false, nil
				}
				for _, arg := range t.Args {
					nm, ok5 := arg.(*Name)
					if !ok5 {
						return false, nil
					}
					v, ok6 := o.attrs[nm.Value]
					if !ok6 {
						// missing attribute => the pattern fails to match
						return false, nil
					}
					e.Vars[nm.Value] = v
				}
				return true, nil
			}
		}
		// Not a class pattern: treat as expression-equality (the previous
		// default behavior) so `case someCall():` still works.
		pv, err := e.eval(t)
		if err != nil {
			return false, err
		}
		return pv == sub, nil
	default:
		pv, err := e.eval(p)
		if err != nil {
			return false, err
		}
		return pv == sub, nil
	}
}

func (e *Evaluator) evalBody(stmts []Stmt) (int64, error) {
	return e.EvalProgram(&Program{Stmts: stmts})
}

func (e *Evaluator) rangeBounds(iter Expr) (int64, int64, error) {
	if c, ok := iter.(*Call); ok {
		if n, ok2 := c.Fn.(*Name); ok2 && n.Value == "range" && (len(c.Args) == 2 || len(c.Args) == 3) {
			start, err := e.eval(c.Args[0])
			if err != nil {
				return 0, 0, err
			}
			stop, err := e.eval(c.Args[1])
			if err != nil {
				return 0, 0, err
			}
			return start, stop, nil
		}
	}
	stop, err := e.eval(iter)
	if err != nil {
		return 0, 0, err
	}
	return 0, stop, nil
}

// rangeStep returns the iteration step for a `range(start, stop[, step])`
// iterable: 1 when no step is given, or the constant third argument.
func (e *Evaluator) rangeStep(iter Expr) (int64, error) {
	if c, ok := iter.(*Call); ok {
		if n, ok2 := c.Fn.(*Name); ok2 && n.Value == "range" && len(c.Args) == 3 {
			step, err := e.eval(c.Args[2])
			if err != nil {
				return 0, err
			}
			if step == 0 {
				return 0, &EvalError{Msg: "range step cannot be zero"}
			}
			return step, nil
		}
	}
	return 1, nil
}

// callMethod invokes a method body with self bound as a local.
// callStrMethod dispatches builtin string methods: s.upper(), s.lower(),
// s.strip(), s.split(sep?). recv is the boxed string handle.

// capitalize returns s with the first rune uppercased and the rest lowercased.
func capitalize(s string) string {
	if s == "" {
		return ""
	}
	r := []rune(s)
	return string(unicode.ToUpper(r[0])) + strings.ToLower(string(r[1:]))
}

// title returns s with the first rune of each whitespace-separated word uppercased.
func title(s string) string {
	prev := ' '
	return strings.Map(func(r rune) rune {
		if prev == ' ' {
			prev = r
			return unicode.ToUpper(r)
		}
		prev = r
		return unicode.ToLower(r)
	}, s)
}

// swapcase returns s with each rune case swapped.
func swapcase(s string) string {
	return strings.Map(func(r rune) rune {
		if unicode.IsUpper(r) {
			return unicode.ToLower(r)
		}
		return unicode.ToUpper(r)
	}, s)
}

// evalIterable evaluates an expression to an ordered list of element handles,
// used by `yield from`. It supports list literals, generator calls (which return
// list handles), and range(...) calls.
func (e *Evaluator) evalIterable(x Expr) ([]int64, error) {
	if c, ok := x.(*Call); ok {
		if n, ok2 := c.Fn.(*Name); ok2 && n.Value == "range" {
			start, stop, err := e.rangeBounds(x)
			if err != nil {
				return nil, err
			}
			var elems []int64
			for i := start; i < stop; i++ {
				elems = append(elems, i)
			}
			return elems, nil
		}
	}
	h, err := e.eval(x)
	if err != nil {
		return nil, err
	}
	if o, ok := e.heap[h]; ok && o.kind == "list" {
		return o.elems, nil
	}
	return nil, &EvalError{Msg: "yield from target is not iterable"}
}
func (e *Evaluator) callStrMethod(recv int64, name string, args []Expr) (int64, error) {
	o := e.heap[recv]
	s := o.sval
	switch name {
	case "upper":
		if len(args) != 0 {
			return 0, &EvalError{Msg: "upper() takes no arguments"}
		}
		return e.allocStr(strings.ToUpper(s)), nil
	case "capitalize":
		// s.capitalize() -> first rune upper, rest lower.
		return e.allocStr(capitalize(s)), nil
	case "title":
		// s.title() -> capitalize the first rune of each whitespace-separated word.
		return e.allocStr(title(s)), nil
	case "swapcase":
		// s.swapcase() -> swap each rune case.
		return e.allocStr(swapcase(s)), nil
	case "lower":
		if len(args) != 0 {
			return 0, &EvalError{Msg: "lower() takes no arguments"}
		}
		return e.allocStr(strings.ToLower(s)), nil
	case "strip":
		// s.strip([chars]) -> trim whitespace, or the given chars when provided.
		if len(args) > 1 {
			return 0, &EvalError{Msg: "strip() takes at most 1 argument"}
		}
		if len(args) == 1 {
			cv, err := e.eval(args[0])
			if err != nil {
				return 0, err
			}
			co, ok := e.heap[cv]
			if !ok || co.kind != "str" {
				return 0, &EvalError{Msg: "strip() argument must be a string"}
			}
			return e.allocStr(strings.Trim(s, co.sval)), nil
		}
		return e.allocStr(strings.TrimSpace(s)), nil
	case "lstrip":
		// s.lstrip([chars]) -> trim whitespace, or the given chars when provided.
		if len(args) > 1 {
			return 0, &EvalError{Msg: "lstrip() takes at most 1 argument"}
		}
		if len(args) == 1 {
			cv, err := e.eval(args[0])
			if err != nil {
				return 0, err
			}
			co, ok := e.heap[cv]
			if !ok || co.kind != "str" {
				return 0, &EvalError{Msg: "lstrip() argument must be a string"}
			}
			return e.allocStr(strings.TrimLeft(s, co.sval)), nil
		}
		return e.allocStr(strings.TrimLeftFunc(s, unicode.IsSpace)), nil
	case "rstrip":
		// s.rstrip([chars]) -> trim whitespace, or the given chars when provided.
		if len(args) > 1 {
			return 0, &EvalError{Msg: "rstrip() takes at most 1 argument"}
		}
		if len(args) == 1 {
			cv, err := e.eval(args[0])
			if err != nil {
				return 0, err
			}
			co, ok := e.heap[cv]
			if !ok || co.kind != "str" {
				return 0, &EvalError{Msg: "rstrip() argument must be a string"}
			}
			return e.allocStr(strings.TrimRight(s, co.sval)), nil
		}
		return e.allocStr(strings.TrimRightFunc(s, unicode.IsSpace)), nil
	case "split":
		// s.split([sep[, maxsplit]]) -> list split on sep, at most maxsplit separators.
		sep := " "
		maxsplit := -1
		if len(args) >= 1 && len(args) <= 2 {
			sepv, err := e.eval(args[0])
			if err != nil {
				return 0, err
			}
			sepo, ok := e.heap[sepv]
			if !ok || sepo.kind != "str" {
				return 0, &EvalError{Msg: "split() separator must be a string"}
			}
			sep = sepo.sval
			if len(args) == 2 {
				ms, err := e.eval(args[1])
				if err != nil {
					return 0, err
				}
				maxsplit = int(ms)
			}
		}
		var parts []string
		if maxsplit >= 0 {
			parts = strings.SplitN(s, sep, maxsplit+1)
		} else {
			parts = strings.Split(s, sep)
		}
		listID := e.allocObj("list")
		lo := e.heap[listID]
		for _, part := range parts {
			lo.elems = append(lo.elems, e.allocStr(part))
		}
		return listID, nil
	case "rsplit":
		// s.rsplit([sep[, maxsplit]]) -> list split on sep from the right.
		sep := " "
		maxsplit := -1
		if len(args) >= 1 && len(args) <= 2 {
			sepv, err := e.eval(args[0])
			if err != nil {
				return 0, err
			}
			sepo, ok := e.heap[sepv]
			if !ok || sepo.kind != "str" {
				return 0, &EvalError{Msg: "rsplit() separator must be a string"}
			}
			sep = sepo.sval
			if len(args) == 2 {
				ms, err := e.eval(args[1])
				if err != nil {
					return 0, err
				}
				maxsplit = int(ms)
			}
		}
		parts := strings.Split(s, sep)
		var out []string
		if maxsplit >= 0 && maxsplit < len(parts)-1 {
			keep := len(parts) - maxsplit
			out = append(out, strings.Join(parts[:keep], sep))
			out = append(out, parts[keep:]...)
		} else {
			out = parts
		}
		listID := e.allocObj("list")
		lo := e.heap[listID]
		for _, part := range out {
			lo.elems = append(lo.elems, e.allocStr(part))
		}
		return listID, nil
	case "removeprefix":
		// s.removeprefix(prefix) -> s without the prefix if it is present.
		if len(args) != 1 {
			return 0, &EvalError{Msg: "removeprefix() takes exactly 1 argument"}
		}
		pv, err := e.eval(args[0])
		if err != nil {
			return 0, err
		}
		po, ok := e.heap[pv]
		if !ok || po.kind != "str" {
			return 0, &EvalError{Msg: "removeprefix() argument must be a string"}
		}
		return e.allocStr(strings.TrimPrefix(s, po.sval)), nil
	case "removesuffix":
		// s.removesuffix(suffix) -> s without the suffix if it is present.
		if len(args) != 1 {
			return 0, &EvalError{Msg: "removesuffix() takes exactly 1 argument"}
		}
		pv2, err := e.eval(args[0])
		if err != nil {
			return 0, err
		}
		po2, ok := e.heap[pv2]
		if !ok || po2.kind != "str" {
			return 0, &EvalError{Msg: "removesuffix() argument must be a string"}
		}
		return e.allocStr(strings.TrimSuffix(s, po2.sval)), nil
	case "expandtabs":
		// s.expandtabs(tabsize) -> replace each tab with spaces to the next tab stop.
		if len(args) != 1 {
			return 0, &EvalError{Msg: "expandtabs() takes exactly 1 argument"}
		}
		tv, err := e.eval(args[0])
		if err != nil {
			return 0, err
		}
		tabsize := int(tv)
		if tabsize < 1 {
			return 0, &EvalError{Msg: "expandtabs() tabsize must be positive"}
		}
		col := 0
		var b strings.Builder
		for _, r := range s {
			if r == '\t' {
				spaces := tabsize - (col % tabsize)
				b.WriteString(strings.Repeat(" ", spaces))
				col += spaces
			} else {
				b.WriteRune(r)
				col++
			}
		}
		return e.allocStr(b.String()), nil
	case "partition":
		// s.partition(sep) -> list [head, sep, tail] at the first occurrence of sep.
		if len(args) != 1 {
			return 0, &EvalError{Msg: "partition() takes exactly 1 argument"}
		}
		sepv, err := e.eval(args[0])
		if err != nil {
			return 0, err
		}
		sepo, ok := e.heap[sepv]
		if !ok || sepo.kind != "str" {
			return 0, &EvalError{Msg: "partition() argument must be a string"}
		}
		sep := sepo.sval
		listID := e.allocObj("list")
		lo := e.heap[listID]
		idx := strings.Index(s, sep)
		if idx < 0 {
			lo.elems = append(lo.elems, e.allocStr(s), e.allocStr(""), e.allocStr(""))
		} else {
			head := s[:idx]
			tail := s[idx+len(sep):]
			lo.elems = append(lo.elems, e.allocStr(head), e.allocStr(sep), e.allocStr(tail))
		}
		return listID, nil
	case "replace":
		if len(args) != 2 {
			return 0, &EvalError{Msg: "replace() takes exactly 2 arguments"}
		}
		oldv, err := e.eval(args[0])
		if err != nil {
			return 0, err
		}
		newv, err := e.eval(args[1])
		if err != nil {
			return 0, err
		}
		oldo, ok := e.heap[oldv]
		if !ok || oldo.kind != "str" {
			return 0, &EvalError{Msg: "replace() old must be a string"}
		}
		novo, ok := e.heap[newv]
		if !ok || novo.kind != "str" {
			return 0, &EvalError{Msg: "replace() new must be a string"}
		}
		return e.allocStr(strings.ReplaceAll(s, oldo.sval, novo.sval)), nil
	case "find":
		// s.find(sub) -> index of first occurrence of sub, or -1 if absent.
		if len(args) != 1 {
			return 0, &EvalError{Msg: "find() takes exactly 1 argument"}
		}
		subv, err := e.eval(args[0])
		if err != nil {
			return 0, err
		}
		subo, ok := e.heap[subv]
		if !ok || subo.kind != "str" {
			return 0, &EvalError{Msg: "find() argument must be a string"}
		}
		return int64(strings.Index(s, subo.sval)), nil
	case "index":
		// s.index(sub) -> index of first occurrence, raising when absent.
		if len(args) != 1 {
			return 0, &EvalError{Msg: "index() takes exactly 1 argument"}
		}
		subv, err := e.eval(args[0])
		if err != nil {
			return 0, err
		}
		subo, ok := e.heap[subv]
		if !ok || subo.kind != "str" {
			return 0, &EvalError{Msg: "index() argument must be a string"}
		}
		idx := strings.Index(s, subo.sval)
		if idx < 0 {
			return 0, &EvalError{Msg: "substring not found"}
		}
		return int64(idx), nil
	case "rfind":
		// s.rfind(sub) -> index of last occurrence of sub, or -1 if absent.
		if len(args) != 1 {
			return 0, &EvalError{Msg: "rfind() takes exactly 1 argument"}
		}
		subv, err := e.eval(args[0])
		if err != nil {
			return 0, err
		}
		subo, ok := e.heap[subv]
		if !ok || subo.kind != "str" {
			return 0, &EvalError{Msg: "rfind() argument must be a string"}
		}
		return int64(strings.LastIndex(s, subo.sval)), nil
	case "count":
		// s.count(sub[, start[, end]]) -> occurrences of sub within s[start:end].
		if len(args) < 1 || len(args) > 3 {
			return 0, &EvalError{Msg: "count() takes 1 to 3 arguments"}
		}
		subv, err := e.eval(args[0])
		if err != nil {
			return 0, err
		}
		subo, ok := e.heap[subv]
		if !ok || subo.kind != "str" {
			return 0, &EvalError{Msg: "count() argument must be a string"}
		}
		lo, hi := 0, len(s)
		if len(args) >= 2 {
			start, err := e.eval(args[1])
			if err != nil {
				return 0, err
			}
			lo = int(start)
		}
		if len(args) == 3 {
			endv, err := e.eval(args[2])
			if err != nil {
				return 0, err
			}
			hi = int(endv)
		}
		if lo < 0 {
			lo = 0
		}
		if hi > len(s) {
			hi = len(s)
		}
		if lo > hi {
			lo = hi
		}
		return int64(strings.Count(s[lo:hi], subo.sval)), nil
	case "isdigit":
		// s.isdigit() -> 1 if all runes are digits, else 0.
		for _, r := range s {
			if !unicode.IsDigit(r) {
				return 0, nil
			}
		}
		if s == "" {
			return 0, nil
		}
		return 1, nil
	case "isalpha":
		// s.isalpha() -> 1 if all runes are alphabetic, else 0.
		for _, r := range s {
			if !unicode.IsLetter(r) {
				return 0, nil
			}
		}
		if s == "" {
			return 0, nil
		}
		return 1, nil
	case "isalnum":
		// s.isalnum() -> 1 if every rune is alphanumeric and s is non-empty.
		if s == "" {
			return 0, nil
		}
		for _, r := range s {
			if !unicode.IsLetter(r) && !unicode.IsDigit(r) {
				return 0, nil
			}
		}
		return 1, nil
	case "isspace":
		// s.isspace() -> 1 if every rune is whitespace and s is non-empty.
		if s == "" {
			return 0, nil
		}
		for _, r := range s {
			if !unicode.IsSpace(r) {
				return 0, nil
			}
		}
		return 1, nil
	case "zfill":
		// s.zfill(width) -> pad with leading zeros to width.
		if len(args) != 1 {
			return 0, &EvalError{Msg: "zfill() takes exactly 1 argument"}
		}
		wv, err := e.eval(args[0])
		if err != nil {
			return 0, err
		}
		width := int(wv)
		if width < 0 {
			return 0, &EvalError{Msg: "zfill() width must be non-negative"}
		}
		if len(s) >= width {
			return e.allocStr(s), nil
		}
		pad := strings.Repeat("0", width-len(s))
		return e.allocStr(pad + s), nil
	case "ljust":
		// s.ljust(width) -> pad with spaces on the right to width.
		if len(args) != 1 {
			return 0, &EvalError{Msg: "ljust() takes exactly 1 argument"}
		}
		wv, err := e.eval(args[0])
		if err != nil {
			return 0, err
		}
		width := int(wv)
		if width < 0 {
			return 0, &EvalError{Msg: "ljust() width must be non-negative"}
		}
		if len(s) >= width {
			return e.allocStr(s), nil
		}
		pad := strings.Repeat(" ", width-len(s))
		return e.allocStr(s + pad), nil
	case "rjust":
		// s.rjust(width) -> pad with spaces on the left to width.
		if len(args) != 1 {
			return 0, &EvalError{Msg: "rjust() takes exactly 1 argument"}
		}
		wv, err := e.eval(args[0])
		if err != nil {
			return 0, err
		}
		width := int(wv)
		if width < 0 {
			return 0, &EvalError{Msg: "rjust() width must be non-negative"}
		}
		if len(s) >= width {
			return e.allocStr(s), nil
		}
		pad := strings.Repeat(" ", width-len(s))
		return e.allocStr(pad + s), nil
	case "islower":
		// s.islower() -> 1 if there is a cased rune and all cased runes are lowercase.
		hasCased := false
		allLower := true
		for _, r := range s {
			if unicode.IsLower(r) {
				hasCased = true
			} else if unicode.IsUpper(r) {
				hasCased = true
				allLower = false
			}
		}
		if hasCased && allLower {
			return 1, nil
		}
		return 0, nil
	case "isupper":
		// s.isupper() -> 1 if there is a cased rune and all cased runes are uppercase.
		hasCased := false
		allUpper := true
		for _, r := range s {
			if unicode.IsUpper(r) {
				hasCased = true
			} else if unicode.IsLower(r) {
				hasCased = true
				allUpper = false
			}
		}
		if hasCased && allUpper {
			return 1, nil
		}
		return 0, nil
	case "join":
		// s.join(list) -> join list element strings with separator s.
		if len(args) != 1 {
			return 0, &EvalError{Msg: "join() takes exactly 1 argument"}
		}
		lv, err := e.eval(args[0])
		if err != nil {
			return 0, err
		}
		lo, ok := e.heap[lv]
		if !ok || lo.kind != "list" {
			return 0, &EvalError{Msg: "join() argument must be a list"}
		}
		parts := []string{}
		for _, el := range lo.elems {
			so, ok := e.heap[el]
			if !ok || so.kind != "str" {
				return 0, &EvalError{Msg: "join() list elements must be strings"}
			}
			parts = append(parts, so.sval)
		}
		return e.allocStr(strings.Join(parts, s)), nil
	case "startswith", "endswith":
		// s.startswith(sub) / s.endswith(sub) -> 1 or 0.
		if len(args) != 1 {
			return 0, &EvalError{Msg: name + "() takes exactly 1 argument"}
		}
		subv, err := e.eval(args[0])
		if err != nil {
			return 0, err
		}
		subo, ok := e.heap[subv]
		if !ok || subo.kind != "str" {
			return 0, &EvalError{Msg: name + "() argument must be a string"}
		}
		res := false
		if name == "startswith" {
			res = strings.HasPrefix(s, subo.sval)
		} else {
			res = strings.HasSuffix(s, subo.sval)
		}
		if res {
			return 1, nil
		}
		return 0, nil
	}
	return 0, &EvalError{Msg: "no such string method " + name}
}

// callListMethod dispatches builtin list methods: xs.append(x).
// append mutates the list in place and returns the (updated) list handle,
// so the REPL can show the resulting list.
func (e *Evaluator) callListMethod(recv int64, name string, args []Expr) (int64, error) {
	o := e.heap[recv]
	switch name {
	case "append":
		if len(args) != 1 {
			return 0, &EvalError{Msg: "append() takes exactly 1 argument"}
		}
		v, err := e.eval(args[0])
		if err != nil {
			return 0, err
		}
		o.elems = append(o.elems, v)
		return recv, nil
	case "count":
		// l.count(value) -> number of occurrences of value in l.
		if len(args) != 1 {
			return 0, &EvalError{Msg: "count() takes exactly 1 argument"}
		}
		vv, err := e.eval(args[0])
		if err != nil {
			return 0, err
		}
		cnt := int64(0)
		for _, el := range o.elems {
			if e.dictKeyEq(el, vv) {
				cnt++
			}
		}
		return cnt, nil
	}
	return 0, &EvalError{Msg: "no such list method " + name}
}

// callDictMethod dispatches builtin dict methods: d.keys() and d.values()
// return boxed lists of the keys/values in insertion order.
// dictKeyEq reports whether two dict keys compare equal by content.
func (e *Evaluator) dictKeyEq(k, idx int64) bool {
	ko, ok := e.heap[k]
	if ok && ko.kind == "str" {
		io, ok2 := e.heap[idx]
		return ok2 && io.kind == "str" && ko.sval == io.sval
	}
	return k == idx
}

// lessVal reports whether boxed value a is less than b (ints by value, strings by content).
func (e *Evaluator) lessVal(a, b int64) bool {
	ao, ok := e.heap[a]
	if ok && ao.kind == "str" {
		bo, ok2 := e.heap[b]
		return ok2 && bo.kind == "str" && ao.sval < bo.sval
	}
	return a < b
}

func (e *Evaluator) callDictMethod(recv int64, name string, args []Expr) (int64, error) {
	o := e.heap[recv]
	switch name {
	case "items":
		if len(args) != 0 {
			return 0, &EvalError{Msg: "items() takes no arguments"}
		}
		listID := e.allocObj("list")
		lo := e.heap[listID]
		for i, k := range o.elems {
			pairID := e.allocObj("list")
			p := e.heap[pairID]
			p.elems = append(p.elems, k, o.dvals[i])
			lo.elems = append(lo.elems, pairID)
		}
		return listID, nil
	case "keys":
		if len(args) != 0 {
			return 0, &EvalError{Msg: "keys() takes no arguments"}
		}
		listID := e.allocObj("list")
		lo := e.heap[listID]
		lo.elems = append(lo.elems, o.elems...)
		return listID, nil
	case "values":
		if len(args) != 0 {
			return 0, &EvalError{Msg: "values() takes no arguments"}
		}
		listID := e.allocObj("list")
		lo := e.heap[listID]
		lo.elems = append(lo.elems, o.dvals...)
		return listID, nil
	case "get":
		// d.get(key[, default]) -> value for key, or default if absent.
		if len(args) != 1 && len(args) != 2 {
			return 0, &EvalError{Msg: "get() takes 1 or 2 arguments"}
		}
		kv, err := e.eval(args[0])
		if err != nil {
			return 0, err
		}
		for i, k := range o.elems {
			if e.dictKeyEq(k, kv) {
				return o.dvals[i], nil
			}
		}
		if len(args) == 2 {
			dv, err := e.eval(args[1])
			if err != nil {
				return 0, err
			}
			return dv, nil
		}
		return 0, nil
	}
	return 0, &EvalError{Msg: "no such dict method " + name}
}


// callDunder calls a dunder (__enter__/__exit__) method on an instance handle.
func (e *Evaluator) callDunder(self int64, name string, args []int64) (int64, error) {
	o := e.heap[self]
	if o == nil {
		return 0, &EvalError{Msg: "with: context manager is not an object"}
	}
	mID, ok := e.resolveMethod(e.classIDs[o.class], name)
	if !ok {
		return 0, &EvalError{Msg: "with: missing " + name + " method"}
	}
	return e.callMethod(e.heap[mID], self, args)
}
func (e *Evaluator) callMethod(mo *obj, self int64, args []int64) (int64, error) {
	caller := e.fnName
	callSite := e.cur
	savedFn := e.fnName
	e.fnName = mo.mname
	defer func() { e.fnName = savedFn }()
	scope := map[string]int64{}
	scope["self"] = self
	// params[0] is the receiver `self`; bind the remaining params from args
	params := mo.fn.Params
	if len(params) > 0 {
		params = params[1:]
	}
	for i, p := range params {
		if i < len(args) {
			scope[p.Name] = args[i]
		}
	}
	old := e.Vars
	e.Vars = scope
	defer func() { e.Vars = old }()
	// set the current method context so super() can resolve the base class
	// and bind the current instance.
	prevClass, prevSelf := e.curClass, e.curSelf
	e.curClass = mo.class
	e.curSelf = self
	defer func() { e.curClass, e.curSelf = prevClass, prevSelf }()

	rv, err := e.evalBody(mo.fn.Body)
	if rs, ok := err.(*returnSignal); ok {
		return rs.val, nil
	}
	return rv, e.recordCall(err, mo.mname, caller, callSite)
}

// resolveMethod finds a method named `name` on the class with id `classID`,
// walking up the base-class chain (inheritance). It returns the method obj id.
func (e *Evaluator) resolveMethod(classID int64, name string) (int64, bool) {
	for c := e.heap[classID]; c != nil && c.kind == "class"; c = e.heap[c.base] {
		if mID, ok := c.attrs[name]; ok {
			return mID, true
		}
		if c.base == 0 {
			break
		}
	}
	return 0, false
}

// classIDFor returns the class heap-id whose obj value equals objV.
func (e *Evaluator) classIDFor(objV int64) int64 {
	for _, v := range e.classIDs {
		if v == objV {
			return v
		}
	}
	return 0
}

// importModule loads <mod>.gy, evaluates it in a fresh top-level scope, and
// binds `mod` to a module obj whose attrs are the module's top-level names.
func (e *Evaluator) importModule(mod string) error {
	path := ResolveImportPath(mod)
	if path == "" {
		return &EvalError{Msg: "cannot import module " + mod}
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return &EvalError{Msg: "cannot import module " + mod}
	}
	prog, err := parseProgram(string(data))
	if err != nil {
		return err
	}
	if diags := Analyze(prog); anyErr(diags) {
		return &EvalError{Msg: "module " + mod + " has analysis errors"}
	}
	// Evaluate in a fresh scope so the module's top-level names don't leak;
	// obj ids come from e's shared heap, so the module obj stays valid.
	savedVars := e.Vars
	savedFuncs := e.funcs
	e.Vars = map[string]int64{}
	e.funcs = map[string]*FuncDef{}; e.externs = map[string]*ExternDecl{}
	_, err = e.EvalProgram(prog)
	if err != nil {
		e.Vars, e.funcs = savedVars, savedFuncs
		return err
	}
	modID := e.allocObj("module")
	m := e.heap[modID]
	m.attrs = map[string]int64{}
	for name, v := range e.Vars {
		m.attrs[name] = v
	}
	// module functions live in e.funcs (not Vars); wrap each in a closure obj
	// so `mod.fn(args)` resolves to a callable.
	for name, fd := range e.funcs {
		cID := e.allocObj("closure")
		c := e.heap[cID]
		c.fn = fd
		c.env = map[string]int64{}
		m.attrs[name] = cID
	}
	e.Vars, e.funcs = savedVars, savedFuncs
	e.Vars[mod] = modID
	return nil
}

// maxHeapID returns the highest object id currently allocated, used to
// advance the nursery boundary after a young GC (promoting survivors to old).
func maxHeapID(heap map[int64]*obj) int64 {
	maxID := int64(0)
	for id := range heap {
		if id > maxID {
			maxID = id
		}
	}
	return maxID
}

func (e *Evaluator) evalCall(n *Call) (int64, error) {
	// method call: obj.method(args) — Fn is an Attr resolving to a method
	// inline lambda callee: `(lambda ...)(args)` evaluates to a closure.
	if _, ok := n.Fn.(*Lambda); ok {
		h, err := e.eval(n.Fn)
		if err != nil {
			return 0, err
		}
		mo, ok := e.heap[h]
		if !ok || mo.kind != "closure" {
			return 0, &EvalError{Msg: "lambda callee is not a closure"}
		}
		return e.callClosure(mo, n)
	}
	if attr, ok := n.Fn.(*Attr); ok {
		// string methods: s.upper() / lower() / strip() / split(sep?)
		recv, err := e.eval(attr.Obj)
		if err != nil {
			return 0, err
		}
		if o, ok := e.heap[recv]; ok && o.kind == "str" {
			return e.callStrMethod(recv, attr.Name.Value, n.Args)
		}
		if o, ok := e.heap[recv]; ok && o.kind == "list" {
			return e.callListMethod(recv, attr.Name.Value, n.Args)
		}
		if o, ok := e.heap[recv]; ok && o.kind == "dict" {
			return e.callDictMethod(recv, attr.Name.Value, n.Args)
		}
		// resolve the attribute/method reference via eval
		mID, err := e.eval(n.Fn)
		if err != nil {
			return 0, err
		}
		mo, ok := e.heap[mID]
		if ok && mo.kind == "closure" {
			// imported/module function call: mod.fn(args)
			return e.callClosure(mo, n)
		}
		if ok && mo.kind == "method" {
			argVals := []int64{}
			for _, a := range n.Args {
				av, err := e.eval(a)
				if err != nil {
					return 0, err
				}
				argVals = append(argVals, av)
			}
			self := mo.recv
			if mo.recv == 0 && len(argVals) > 0 {
				self = argVals[0]
				argVals = argVals[1:]
			}
			return e.callMethod(mo, self, argVals)
		}
		return 0, &EvalError{Msg: "not a callable attribute"}
	}
	if name, ok := n.Fn.(*Name); ok {
		// super() returns a proxy bound to the current instance whose methods
		// resolve on the base class of the currently-executing class.
		if name.Value == "super" {
			if len(n.Args) != 0 {
				return 0, &EvalError{Msg: "super() takes no arguments"}
			}
			if e.curClass == "" {
				return 0, &EvalError{Msg: "super() only valid inside a method"}
			}
			classID, ok := e.classIDs[e.curClass]
			if !ok {
				return 0, &EvalError{Msg: "unknown class " + e.curClass}
			}
			cls := e.heap[classID]
			baseID := cls.base
			if baseID == 0 {
				return 0, &EvalError{Msg: "super() outside a subclass"}
			}
			spID := e.allocObj("superproxy")
			sp := e.heap[spID]
			sp.base = baseID
			sp.recv = e.curSelf
			return spID, nil
		}
		// class instantiation: Point(0,0)
		if classID, ok := e.classIDs[name.Value]; ok {
			instID := e.allocObj("instance")
			inst := e.heap[instID]
			inst.class = name.Value
			argVals := []int64{}
			for _, a := range n.Args {
				av, err := e.eval(a)
				if err != nil {
					return 0, err
				}
				argVals = append(argVals, av)
			}
			if initID, ok := e.resolveMethod(classID, "__init__"); ok {
				mo := e.heap[initID]
				mo.recv = instID
				_, err := e.callMethod(mo, instID, argVals)
				if err != nil {
					return 0, err
				}
			}
			return instID, nil
		}
	}
	if name, ok := n.Fn.(*Name); ok {
		// closure value in the current scope?
		if cid, ok2 := e.Vars[name.Value]; ok2 {
			if o := e.heap[cid]; o != nil && o.kind == "closure" {
				return e.callClosure(o, n)
			}
		}
		if ed, ok2 := e.externs[name.Value]; ok2 {
		return e.callExtern(ed, n.Args)
	}
	if fd, ok2 := e.funcs[name.Value]; ok2 {
			argVals := make([]int64, len(fd.Params))
			argSet := make([]bool, len(fd.Params))
			pos := 0
			seenKw := false
			for _, a := range n.Args {
				if kw, ok := a.(*KeywordArg); ok {
					seenKw = true
					idx := -1
					for i, p := range fd.Params {
						if p.Name == kw.Name {
							idx = i
							break
						}
					}
					if idx < 0 {
						return 0, &EvalError{Msg: "unknown keyword argument " + kw.Name}
					}
					if argSet[idx] {
						return 0, &EvalError{Msg: "multiple values for argument " + kw.Name}
					}
					v, err := e.eval(kw.Value)
					if err != nil {
						return 0, err
					}
					argVals[idx] = v
					argSet[idx] = true
					continue
				}
				if seenKw {
					return 0, &EvalError{Msg: "positional argument after keyword argument"}
				}
				if pos >= len(fd.Params) {
					return 0, &EvalError{Msg: "too many arguments"}
				}
				if argSet[pos] {
					return 0, &EvalError{Msg: "multiple values for argument " + fd.Params[pos].Name}
				}
				v, err := e.eval(a)
				if err != nil {
					return 0, err
				}
				argVals[pos] = v
				argSet[pos] = true
				pos++
			}
			for i, p := range fd.Params {
				if argSet[i] {
					continue
				}
				if p.Default == nil {
					return 0, &EvalError{Msg: "missing argument " + p.Name}
				}
				dv, err := e.eval(p.Default)
				if err != nil {
					return 0, err
				}
				argVals[i] = dv
				argSet[i] = true
			}
			saved := e.Vars
			e.Vars = map[string]int64{}
			for i, p := range fd.Params {
				if p.Annot != nil {
					if err := e.checkAnnot(p.Name, p.Annot, argVals[i]); err != nil {
						return 0, err
					}
				}
				e.Vars[p.Name] = argVals[i]
			}
			prevRet := e.curRet
			e.curRet = fd.ReturnAnno
				caller := e.fnName
				callSite := e.cur
				savedFn := e.fnName
				e.fnName = fd.Name
				defer func() { e.fnName = savedFn }()
			if containsYield(fd.Body) {
				genH := e.allocObj("list")
				prev := e.yieldList
				e.yieldList = genH
				e.inCall = true
				_, err := e.evalBody(fd.Body)
				e.inCall = false
				e.yieldList = prev
				e.Vars = saved
				e.curRet = prevRet
				if err != nil {
					return 0, err
				}
				return genH, nil
			}
			e.inCall = true
			rv, err := e.evalBody(fd.Body)
			e.inCall = false
			e.Vars = saved
			e.curRet = prevRet
			if rs, ok := err.(*returnSignal); ok {
				return rs.val, nil
			}
			return rv, e.recordCall(err, fd.Name, caller, callSite)
		}
		// built-in exception constructor: ValueError("msg") etc.
		if isExnClass(name.Value) {
			msg := ""
			for _, arg := range n.Args {
				av, err := e.eval(arg)
				if err != nil {
					return 0, err
				}
				if s, ok := e.heap[av]; ok && s.kind == "str" && msg == "" {
					msg = s.sval
				}
			}
			return e.allocExn(name.Value, msg), nil
		}
		switch name.Value {
		case "print":
			for _, a := range n.Args {
				v, err := e.eval(a)
				if err != nil {
					return 0, err
				}
				fmt.Println(e.Repr(v))
			}
			return 0, nil
		case "len":
			if len(n.Args) != 1 {
				return 0, &EvalError{Msg: "len expects 1 argument"}
			}
			v, err := e.eval(n.Args[0])
			if err != nil {
				return 0, err
			}
			if o, ok := e.heap[v]; ok {
				switch o.kind {
				case "list", "set", "dict":
					return int64(len(o.elems)), nil
				case "str":
					return int64(len(o.sval)), nil
				}
			}
			return 0, &EvalError{Msg: "len expects a list, set, dict, or string"}

		case "min", "max":
			if len(n.Args) != 1 {
				return 0, &EvalError{Msg: "min/max expects 1 argument"}
			}
			lo, err := e.eval(n.Args[0])
			if err != nil {
				return 0, err
			}
			var vals []int64
			if o, ok := e.heap[lo]; ok && o.kind == "dict" {
				// The interpreter rejects dict literals for min/max (matching
				// codegen); scalars are treated as single-element collections.
				return 0, &EvalError{Msg: "min/max expects a list or set"}
			}
			if o, ok := e.heap[lo]; ok && (o.kind == "list" || o.kind == "set") {
				vals = o.elems
			} else {
				vals = []int64{lo}
			}
			if len(vals) == 0 {
				return 0, &EvalError{Msg: "min/max of empty collection"}
			}
			best := vals[0]
			for _, v := range vals[1:] {
				if name.Value == "min" && v < best {
					best = v
				}
				if name.Value == "max" && v > best {
					best = v
				}
			}
			return best, nil
		case "sorted":
			if len(n.Args) < 1 || len(n.Args) > 2 {
				return 0, &EvalError{Msg: "sorted() takes exactly 1 argument"}
			}
			lv, err := e.eval(n.Args[0])
			if err != nil {
				return 0, err
			}
			lo, ok := e.heap[lv]
			if !ok || lo.kind != "list" {
				return 0, &EvalError{Msg: "sorted() argument must be a list"}
			}
			elems := append([]int64(nil), lo.elems...)
			sort.Slice(elems, func(i, j int) bool {
				return e.lessVal(elems[i], elems[j])
			})
			// sorted(iter, reverse=True) returns descending order.
			if len(n.Args) > 1 {
				// Accept reverse=True (KeywordArg) or a positional truthy second arg.
				var rev int64
				if kw, ok := n.Args[1].(*KeywordArg); ok {
					rv, err := e.eval(kw.Value)
					if err != nil {
						return 0, err
					}
					rev = rv
				} else {
					rv, err := e.eval(n.Args[1])
					if err != nil {
						return 0, err
					}
					rev = rv
				}
				if rev != 0 {
					for i, j := 0, len(elems)-1; i < j; i, j = i+1, j-1 {
						elems[i], elems[j] = elems[j], elems[i]
					}
				}
			}
			listID := e.allocObj("list")
			nl := e.heap[listID]
			nl.elems = append(nl.elems, elems...)
			return listID, nil
		case "reversed":
			av, err := e.eval(n.Args[0])
			if err != nil {
				return 0, err
			}
			if o, ok := e.heap[av]; ok {
				switch o.kind {
				case "list", "set":
					id := e.allocObj("list")
					lo := e.heap[id]
					lo.elems = append(lo.elems, o.elems...)
					for i, j := 0, len(lo.elems)-1; i < j; i, j = i+1, j-1 {
						lo.elems[i], lo.elems[j] = lo.elems[j], lo.elems[i]
					}
					return id, nil
				case "str":
					return e.allocStr(reverseStr(o.sval)), nil
				}
			}
			return 0, &EvalError{Msg: "reversed expects a list or string"}
		case "enumerate":
			av, err := e.eval(n.Args[0])
			if err != nil {
				return 0, err
			}
			if o, ok := e.heap[av]; ok && o.kind == "list" {
				id := e.allocObj("list")
				lo := e.heap[id]
				for i, el := range o.elems {
					pair := e.allocObj("list")
					po := e.heap[pair]
					po.elems = append(po.elems, int64(i), el)
					lo.elems = append(lo.elems, pair)
				}
				return id, nil
			}
			return 0, &EvalError{Msg: "enumerate expects a list"}
		case "zip":
			if len(n.Args) != 2 {
				return 0, &EvalError{Msg: "zip expects two lists"}
			}
			l1, err := e.eval(n.Args[0])
			if err != nil {
				return 0, err
			}
			l2, err := e.eval(n.Args[1])
			if err != nil {
				return 0, err
			}
			o1, ok1 := e.heap[l1]
			o2, ok2 := e.heap[l2]
			if !ok1 || o1.kind != "list" || !ok2 || o2.kind != "list" {
				return 0, &EvalError{Msg: "zip expects two lists"}
			}
			n := len(o1.elems)
			if len(o2.elems) < n {
				n = len(o2.elems)
			}
			id := e.allocObj("list")
			lo := e.heap[id]
			for i := 0; i < n; i++ {
				pair := e.allocObj("list")
				po := e.heap[pair]
				po.elems = append(po.elems, o1.elems[i], o2.elems[i])
				lo.elems = append(lo.elems, pair)
			}
			return id, nil
		case "sum":
			if len(n.Args) != 1 {
				return 0, &EvalError{Msg: "sum expects 1 argument"}
			}
			sv, err := e.eval(n.Args[0])
			if err != nil {
				return 0, err
			}
			so, ok := e.heap[sv]
			if !ok || (so.kind != "list" && so.kind != "set") {
				return 0, &EvalError{Msg: "sum expects a list or set"}
			}
			total := int64(0)
			for _, el := range so.elems {
				total += el
			}
			return total, nil
		case "abs":
			if len(n.Args) != 1 {
				return 0, &EvalError{Msg: "abs expects 1 argument"}
			}
			av, err := e.eval(n.Args[0])
			if err != nil {
				return 0, err
			}
			if o, ok := e.heap[av]; ok && o.kind == "float" {
				if o.fval < 0 {
					return e.allocFloat(-o.fval), nil
				}
				return av, nil
			}
			if av < 0 {
				return -av, nil
			}
			return av, nil
		case "int":
			av, err := e.eval(n.Args[0])
			if err != nil {
				return 0, err
			}
			if o, ok := e.heap[av]; ok && o.kind == "float" {
				return int64(o.fval), nil
			}
			if o, ok := e.heap[av]; ok && o.kind == "str" {
				f, err := strconv.ParseFloat(o.sval, 64)
				if err != nil {
					return 0, &EvalError{Msg: "int: cannot parse string"}
				}
				return int64(f), nil
			}
			return av, nil
		case "float":
			av, err := e.eval(n.Args[0])
			if err != nil {
				return 0, err
			}
			if o, ok := e.heap[av]; ok && o.kind == "str" {
				f, err := strconv.ParseFloat(o.sval, 64)
				if err != nil {
					return 0, &EvalError{Msg: "float: cannot parse string"}
				}
				return e.allocFloat(f), nil
			}
			if o, ok := e.heap[av]; ok && o.kind == "float" {
				if o.fval < 0 {
					return e.allocFloat(-o.fval), nil
				}
				return av, nil
			}
			return e.allocFloat(float64(av)), nil
		case "range":
			if len(n.Args) < 1 || len(n.Args) > 3 {
				return 0, &EvalError{Msg: "range expects 1 to 3 arguments"}
			}
			return e.eval(n.Args[0])
		case "any", "all":
			// any(iter) is 1 if any element is truthy; all(iter) is 1 if all are.
			arg, err := e.eval(n.Args[0])
			if err != nil {
				return 0, err
			}
			if arg <= 0 {
				return 0, &EvalError{Msg: "any/all need a list"}
			}
			o, ok := e.heap[arg]
			if !ok || o.kind != "list" {
				return 0, &EvalError{Msg: "any/all need a list"}
			}
			anyMode := name.Value == "any"
			if anyMode {
				for _, v := range o.elems {
					if v != 0 {
						return 1, nil
					}
				}
				return 0, nil
			}
			for _, v := range o.elems {
				if v == 0 {
					return 0, nil
				}
			}
			return 1, nil
		case "chr":
			// chr(n) returns the single-character string for codepoint n.
			cn, err := e.eval(n.Args[0])
			if err != nil {
				return 0, err
			}
			return e.allocStr(string(rune(cn))), nil
		case "ord":
			// ord(s) returns the codepoint of the first character of s.
			arg, err := e.eval(n.Args[0])
			if err != nil {
				return 0, err
			}
			o, ok := e.heap[arg]
			if !ok {
				return 0, &EvalError{Msg: "ord needs a string"}
			}
			if len(o.sval) == 0 {
				return 0, &EvalError{Msg: "ord of empty string"}
			}
			return int64(o.sval[0]), nil
		case "round":
			// round(x) is the identity for ints; truncates floats.
			x, err := e.eval(n.Args[0])
			if err != nil {
				return 0, err
			}
			if o, ok := e.heap[x]; ok && o.kind == "float" {
				return int64(math.Round(o.fval)), nil
			}
			return x, nil

		case "str":
			if len(n.Args) != 1 {
				return 0, &EvalError{Msg: "str expects 1 argument"}
			}
			av, err := e.eval(n.Args[0])
			if err != nil {
				return 0, err
			}
			return e.allocStr(e.Repr(av)), nil
		}
	}
	return 0, &EvalError{Msg: "unsupported call for eval"}
}

// EvalError is a runtime eval error. When a raised exception is the cause,
// ExnType carries the exception class name (e.g. "ValueError") and ExnMsg
// its message; otherwise both are empty.
// Frame is one call-stack frame in a runtime traceback.
type Frame struct {
	Name string `json:"name"`
	Line int    `json:"line"`
	Col  int    `json:"col"`
}

type EvalError struct {
	Msg     string
	ExnType string
	ExnMsg  string
	Traceback []Frame `json:"traceback,omitempty"`
}

func (e *EvalError) Error() string { return "eval error: " + e.Msg }

// reverseStr returns the reverse of s.
func reverseStr(s string) string {
	r := []rune(s)
	for i, j := 0, len(r)-1; i < j; i, j = i+1, j-1 {
		r[i], r[j] = r[j], r[i]
	}
	return string(r)
}

// exnError builds an EvalError carrying a typed exception (type name + message).
func exnError(exnType, msg string) *EvalError {
	return &EvalError{Msg: msg, ExnType: exnType, ExnMsg: msg}
}

// loopSignal carries break/continue control out of a loop body.
type loopSignal struct{ kind string }

func (l *loopSignal) Error() string { return "loop signal: " + l.kind }

// returnSignal carries a `return` statement's value out of nested blocks to the
// enclosing function/method call site. It implements error so that it flows up
// through the statement-loop block handlers (if/while/for), which propagate
// errors via `return 0, err`.
type returnSignal struct{ val int64 }

func (r *returnSignal) Error() string { return "return signal" }

// EvalExpr compiles src and evaluates it, returning the integer result and diagnostics.
func EvalExpr(src string) (int64, []Diagnostic, error) {
	prog, err := parseProgram(src)
	if err != nil {
		return 0, nil, err
	}
	diags := Analyze(prog)
	if anyErr(diags) {
		return 0, diags, fmt.Errorf("verify: %s", diags[0].Msg)
	}
	ev := NewEvaluator()
	v, err := ev.EvalProgram(prog)
	if err != nil {
		return v, diags, ev.FinalizeTraceback(err)
	}
	return v, diags, err
}

// FinalizeTraceback attaches the <module> frame to a runtime error when a
// module was evaluated directly (the CLI calls EvalProgram directly).
func (e *Evaluator) FinalizeTraceback(err error) error {
	ee, ok := err.(*EvalError)
	if !ok || ee.Traceback != nil {
		return err
	}
	ee.Traceback = []Frame{{Name: "<module>", Line: e.cur.Line, Col: e.cur.Col}}
	return err
}

// RenderTraceback renders a Python-style traceback for a runtime EvalError.
func (e *EvalError) RenderTraceback() string {
	if len(e.Traceback) == 0 {
		return ""
	}
	out := "Traceback (most recent call last):\n"
	for _, f := range e.Traceback {
		out += fmt.Sprintf("  File \"prog\", line %d, in %s\n", f.Line, f.Name)
	}
	out += e.Msg
	return out
}

func containsYield(stmts []Stmt) bool {
	for _, st := range stmts {
		switch s := st.(type) {
		case *YieldStmt:
			return true
		case *YieldFromStmt:
			return true
		case *IfStmt:
			if containsYield(s.Then) {
				return true
			}
			for _, e := range s.Elifs {
				if containsYield(e.Then) {
					return true
				}
			}
			if containsYield(s.Else) {
				return true
			}
		case *WhileStmt:
			if containsYield(s.Body) || containsYield(s.Else) {
				return true
			}
		case *ForStmt:
			if containsYield(s.Body) || containsYield(s.Else) {
				return true
			}
		case *MatchStmt:
			for _, c := range s.Cases {
				if containsYield(c.Body) {
					return true
				}
			}
		case *FuncDef:
			if containsYield(s.Body) {
				return true
			}
		}
	}
	return false
}

// callExtern evaluates an FFI call to a C function. In the AST interpreter we
// dispatch to a small Go registry mirroring the C stdlib functions.
func (e *Evaluator) callExtern(ed *ExternDecl, args []Expr) (int64, error) {
	vals := []int64{}
	for _, a := range args {
		v, err := e.eval(a)
		if err != nil {
			return 0, err
		}
		vals = append(vals, v)
	}
	switch ed.Name {
	case "abs":
		if len(vals) != 1 {
			return 0, fmt.Errorf("abs expects 1 argument")
		}
		x := vals[0]
		if x < 0 {
			x = -x
		}
		return x, nil
	case "getpid":
		return int64(os.Getpid()), nil
	case "rand":
		return int64(rand.Intn(1 << 30)), nil
	case "strlen":
		if len(vals) != 1 {
			return 0, fmt.Errorf("strlen expects 1 argument")
		}
		sid := vals[0]
		sv, ok := e.heap[sid]
		if !ok || sv == nil || sv.sval == "" && sv.kind != "str" {
			return 0, fmt.Errorf("strlen: not a string value")
		}
		return int64(len(sv.sval)), nil
	default:
		return 0, fmt.Errorf("extern function %q is not available in the interpreter", ed.Name)
	}
}

