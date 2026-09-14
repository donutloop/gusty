package lang

import "os"

// Evaluator is a small AST interpreter used by --eval and the REPL.
// It evaluates integer-typed expressions deterministically without needing
// an LLVM JIT engine (the go-llvm fork is bindings-only, no ExecutionEngine).
type Evaluator struct {
	Vars      map[string]int64
	funcs     map[string]*FuncDef
	inCall    bool // true while evaluating a function body (nested defs become closures)
	heap      map[int64]*obj
	nextID    int64
	classIDs  map[string]int64
	curRet    *Type       // return annotation of the function currently executing
	yieldList int64       // list handle accumulating yields (0 = not in generator)
	curClass  string      // class name of the method currently executing (for super())
	curSelf   int64       // receiver of the method currently executing (for super())
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
}

func (e *Evaluator) allocObj(kind string) int64 {
	e.nextID++
	id := e.nextID
	e.heap[id] = &obj{kind: kind, attrs: map[string]int64{}}
	return id
}

// allocClosure creates a closure value capturing the current scope.
func (e *Evaluator) allocClosure(fn *FuncDef, env map[string]int64) int64 {
	e.nextID++
	id := e.nextID
	e.heap[id] = &obj{kind: "closure", fn: fn, env: env, attrs: map[string]int64{}}
	return id
}

// typeOfVal maps a runtime value to its static type Kind for gradual typing
// checks. Plain small int64s are ints/bools/none; heap ids are objects whose
// kind string maps to a Type.
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
func (e *Evaluator) callFunc(fd *FuncDef, argVals []int64, env map[string]int64) (int64, error) {
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
		return genH, err
	}
	e.Vars = scope
	e.inCall = true
	rv, err := e.evalBody(fd.Body)
	e.inCall = false
	e.Vars = saved
	e.curRet = prevRet
	return rv, err
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
	return &Evaluator{Vars: map[string]int64{}, funcs: map[string]*FuncDef{}, heap: map[int64]*obj{}, classIDs: map[string]int64{}}
}

// EvalProgram evaluates prog's top-level statements and returns the value of
// the final expression statement (or last assignment). It returns an error on
// unsupported constructs.
func (e *Evaluator) EvalProgram(prog *Program) (int64, error) {
	var last int64
	for _, st := range prog.Stmts {
		switch s := st.(type) {
		case *ClassDef:
			classID := e.allocObj("class")
			e.classIDs[s.Name] = classID
			cls := e.heap[classID]
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
			if cond != 0 {
				rv, err := e.evalBody(s.Then)
				if err != nil {
					return 0, err
				}
				last = rv
			} else if s.Else != nil {
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
				matches := true
				if pn, ok := c.Pattern.(*Name); !ok || pn.Value != "_" {
					pv, err := e.eval(c.Pattern)
					if err != nil {
						return 0, err
					}
					matches = pv == sub
				}
				if matches {
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
					if ec.Exn == nil || ec.Exn.Value == "Exception" {
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
					if o, ok := e.heap[itV]; ok && (o.kind == "list" || o.kind == "set" || o.kind == "dict") {
						for _, el := range o.elems {
							e.Vars[n.Value] = el
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
						start, stop, err := e.rangeBounds(s.Iter)
						if err != nil {
							return 0, err
						}
						for i := start; i < stop; i++ {
							e.Vars[n.Value] = i
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
					for i := start; i < stop; i++ {
						e.Vars[n.Value] = i
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
				return v, nil
			}
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
		case *RaiseStmt:
			return 0, &EvalError{Msg: "raised"}
		case *BreakStmt:
			return 0, &loopSignal{kind: "break"}
		case *ContinueStmt:
			return 0, &loopSignal{kind: "continue"}
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
			return -v, nil
		case "not":
			if v == 0 {
				return 1, nil
			}
			return 0, nil
		}
		return 0, &EvalError{Msg: "unsupported unary " + n.Op}
	case *Attr:
		objV, err := e.eval(n.Obj)
		if err != nil {
			return 0, err
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
				if k == idx {
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
		default:
			return 0, &EvalError{Msg: "cannot index this value"}
		}
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
	default:
		return 0, &EvalError{Msg: "unsupported expression for eval"}
	}
}

func (e *Evaluator) evalComp(c *Comp) (int64, error) {
	it, err := e.eval(c.Iter)
	if err != nil {
		return 0, err
	}
	o := e.heap[it]
	if o == nil {
		return 0, &EvalError{Msg: "comprehension over non-object"}
	}
	items := o.elems
	if o.kind == "dict" {
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

func (e *Evaluator) evalBin(n *BinOp) (int64, error) {
	l, err := e.eval(n.L)
	if err != nil {
		return 0, err
	}
	r, err := e.eval(n.R)
	if err != nil {
		return 0, err
	}
	switch n.Op {
	case "+":
		return l + r, nil
	case "-":
		return l - r, nil
	case "*":
		return l * r, nil
	case "/", "//":
		if r == 0 {
			return 0, &EvalError{Msg: "division by zero"}
		}
		return l / r, nil
	case "%":
		if r == 0 {
			return 0, &EvalError{Msg: "division by zero"}
		}
		return l % r, nil
	case "==":
		if l == r {
			return 1, nil
		}
		return 0, nil
	case "!=":
		if l != r {
			return 1, nil
		}
		return 0, nil
	case "<":
		if l < r {
			return 1, nil
		}
		return 0, nil
	case "<=":
		if l <= r {
			return 1, nil
		}
		return 0, nil
	case ">":
		if l > r {
			return 1, nil
		}
		return 0, nil
	case ">=":
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
	}
	return 0, &EvalError{Msg: "unsupported operator " + n.Op}
}

func (e *Evaluator) evalBody(stmts []Stmt) (int64, error) {
	return e.EvalProgram(&Program{Stmts: stmts})
}

func (e *Evaluator) rangeBounds(iter Expr) (int64, int64, error) {
	if c, ok := iter.(*Call); ok {
		if n, ok2 := c.Fn.(*Name); ok2 && n.Value == "range" && len(c.Args) == 2 {
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

// callMethod invokes a method body with self bound as a local.
func (e *Evaluator) callMethod(mo *obj, self int64, args []int64) (int64, error) {
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

	return e.evalBody(mo.fn.Body)
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
	data, err := os.ReadFile(mod + ".gy")
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
	e.funcs = map[string]*FuncDef{}
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

func (e *Evaluator) evalCall(n *Call) (int64, error) {
	// method call: obj.method(args) — Fn is an Attr resolving to a method
	if _, ok := n.Fn.(*Attr); ok {
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
			return rv, err
		}
		switch name.Value {
		case "print":
			for _, a := range n.Args {
				v, err := e.eval(a)
				if err != nil {
					return 0, err
				}
				_ = v
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
			if o, ok := e.heap[v]; ok && (o.kind == "list" || o.kind == "set" || o.kind == "dict") {
				return int64(len(o.elems)), nil
			}
			return 0, &EvalError{Msg: "len expects a list, set, or dict"}

		case "range":
			if len(n.Args) != 1 {
				return 0, &EvalError{Msg: "range expects 1 argument"}
			}
			return e.eval(n.Args[0])
		}
	}
	return 0, &EvalError{Msg: "unsupported call for eval"}
}

// EvalError is a runtime eval error.
type EvalError struct{ Msg string }

func (e *EvalError) Error() string { return "eval error: " + e.Msg }

// loopSignal carries break/continue control out of a loop body.
type loopSignal struct{ kind string }

func (l *loopSignal) Error() string { return "loop signal: " + l.kind }

// EvalExpr compiles src and evaluates it, returning the integer result and diagnostics.
func EvalExpr(src string) (int64, []Diagnostic, error) {
	prog, err := parseProgram(src)
	if err != nil {
		return 0, nil, err
	}
	diags := Analyze(prog)
	if anyErr(diags) {
		return 0, diags, nil
	}
	ev := NewEvaluator()
	v, err := ev.EvalProgram(prog)
	return v, diags, err
}

func containsYield(stmts []Stmt) bool {
	for _, st := range stmts {
		switch s := st.(type) {
		case *YieldStmt:
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
		case *FuncDef:
			if containsYield(s.Body) {
				return true
			}
		}
	}
	return false
}
