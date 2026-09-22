package lang

import "fmt"

// Symbol is a resolved variable/function binding with a type.
type Symbol struct {
	Name string
	Type *Type
}

// Scope is a lexical scope mapping names to symbols.
type Scope struct {
	Parent *Scope
	Vars   map[string]*Type
}

func newScope(parent *Scope) *Scope {
	return &Scope{Parent: parent, Vars: map[string]*Type{}}
}

func (s *Scope) lookup(name string) *Type {
	if t, ok := s.Vars[name]; ok {
		return t
	}
	if s.Parent != nil {
		return s.Parent.lookup(name)
	}
	return nil
}

func (s *Scope) define(name string, t *Type) {
	s.Vars[name] = t
}

// SemanticAnalyzer walks the AST, builds scopes, and infers types.
type SemanticAnalyzer struct {
	scope      *Scope
	Diags      []Diagnostic
	curFn      *FuncDef
	funcs      map[string]*FuncDef
	exceptions map[string]bool
	classes    map[string]bool
	inFunc     bool
	loopDepth  int
	inferring  map[string]bool
}

// Analyze runs semantic analysis and type inference on prog.
func Analyze(prog *Program) []Diagnostic {
	an := &SemanticAnalyzer{scope: newScope(nil), funcs: map[string]*FuncDef{}, classes: map[string]bool{}, exceptions: map[string]bool{"Exception": true}}
	// predeclare builtins
	an.scope.define("print", TFunc(nil, TVoid()))
	an.scope.define("range", TIter(TInt()))
	an.scope.define("min", TFunc([]*Type{TList(TInt())}, TInt()))
	an.scope.define("max", TFunc([]*Type{TList(TInt())}, TInt()))
	an.scope.define("abs", TFunc([]*Type{TInt()}, TInt()))
	for _, st := range prog.Stmts {
		an.analyzeStmt(st)
	}
	return an.Diags
}

func (an *SemanticAnalyzer) errorf(sp Span, msg string, args ...interface{}) {
	if len(args) > 0 {
		msg = fmt.Sprintf(msg, args...)
	}
	an.Diags = append(an.Diags, Diagnostic{Level: LevelError, Span: sp, Msg: msg})
}

func (an *SemanticAnalyzer) warnf(sp Span, msg string, args ...interface{}) {
	if len(args) > 0 {
		msg = fmt.Sprintf(msg, args...)
	}
	an.Diags = append(an.Diags, Diagnostic{Level: LevelWarning, Span: sp, Msg: msg})
}

func (an *SemanticAnalyzer) analyzeStmt(st Stmt) {
	switch s := st.(type) {
	case *AssignStmt:
		an.analyzeAssign(s)
	case *AugAssignStmt:
		// augmented assignment reads the target (must already be in scope)
		// then writes it back. Infer the RHS and validate the target.
		switch t := s.Target.(type) {
		case *Name:
			if an.scope.lookup(t.Value) == nil {
				an.errorf(t.Span(), "undefined variable %q in augmented assignment", t.Value)
			}
			ty := an.inferExpr(s.Value)
			an.scope.define(t.Value, ty)
		case *Attr:
			an.inferExpr(t.Obj)
			an.inferExpr(s.Value)
		default:
			an.errorf(s.Span(), "unsupported augmented-assignment target %T", s.Target)
		}
	case *ExprStmt:
		an.inferExpr(s.Expr)
	case *ReturnStmt:
		if s.Expr != nil {
			an.inferExpr(s.Expr)
		}
	case *RaiseStmt:
		if s.Expr != nil {
			an.inferExpr(s.Expr)
		}
	case *IfStmt:
		an.inferExpr(s.Cond)
		// if/elif/else bodies share the enclosing scope: assignments there
		// flow outward (like Python), so do NOT create a child scope.
		for _, b := range s.Then {
			an.analyzeStmt(b)
		}
		for _, e := range s.Elifs {
			an.inferExpr(e.Cond)
			for _, b := range e.Then {
				an.analyzeStmt(b)
			}
		}
		for _, b := range s.Else {
			an.analyzeStmt(b)
		}
	case *WhileStmt:
		an.inferExpr(s.Cond)
		old := an.scope
		an.scope = newScope(old)
		an.loopDepth++
		for _, b := range s.Body {
			an.analyzeStmt(b)
		}
		for _, b := range s.Else {
			an.analyzeStmt(b)
		}
		an.loopDepth--
		an.scope = old
	case *ForStmt:
		it := an.inferExpr(s.Iter)
		elem := it
		if it != nil && it.Kind == KindIterator {
			elem = it.Elem
		}
		if elem == nil {
			elem = TDyn()
		}
		// loop var and body share the enclosing scope (runtime uses shared vars).
		for _, nm := range loopVarNames(s.Var) {
			an.scope.define(nm, elem)
		}
		an.loopDepth++
		for _, b := range s.Body {
			an.analyzeStmt(b)
		}
		for _, b := range s.Else {
			an.analyzeStmt(b)
		}
		an.loopDepth--

	case *FuncDef:
		an.analyzeFunc(s)
	case *ClassDef:
		an.classes[s.Name] = true
		an.scope = newScope(an.scope)
		for _, b := range s.Body {
			an.analyzeStmt(b)
		}
		an.scope = an.scope.Parent
	case *ImportStmt:
		an.scope.define(s.Module, TDyn())
	case *BreakStmt:
		if an.loopDepth == 0 {
			an.errorf(s.Span(), "break outside loop")
		}
	case *ContinueStmt:
		if an.loopDepth == 0 {
			an.errorf(s.Span(), "continue outside loop")
		}
	case *PassStmt:
		// no-op statement
	case *MatchStmt:
		an.inferExpr(s.Subject)
		for _, c := range s.Cases {
			if c.Guard != nil {
				an.inferExpr(c.Guard)
			}
			an.scope = newScope(an.scope)
			for _, p := range append([]Expr{c.Pattern}, c.Or...) {
				switch t := p.(type) {
				case *ListLit:
					for _, pe := range t.Elems {
						if n, ok := pe.(*Name); ok && n.Value != "_" {
							an.scope.define(n.Value, TDyn())
						}
					}
				case *DictLit:
					for _, ve := range t.Vals {
						if n, ok := ve.(*Name); ok && n.Value != "_" {
							an.scope.define(n.Value, TDyn())
						}
					}
				}
			}
			for _, b := range c.Body {
				an.analyzeStmt(b)
			}
			an.scope = an.scope.Parent
		}
	case *TryStmt:
		an.scope = newScope(an.scope)
		for _, b := range s.Body {
			an.analyzeStmt(b)
		}
		an.scope = an.scope.Parent
		for _, e := range s.Excepts {
			an.scope = newScope(an.scope)
			for _, b := range e.Body {
				an.analyzeStmt(b)
			}
			an.scope = an.scope.Parent
		}
	case *YieldStmt:
		if s.Expr != nil {
			an.inferExpr(s.Expr)
		}
	}
}

func (an *SemanticAnalyzer) analyzeAssign(as *AssignStmt) {
	valTy := an.inferExpr(as.Value)
	// Static gradual typing: report an annotation/inferred-type mismatch.
	if as.Annot != nil && valTy != nil && valTy.Kind != KindDynamic && as.Annot.Kind != KindDynamic && valTy.Kind != as.Annot.Kind {
		an.errorf(as.Span(), "type mismatch: expected %s, got %s", as.Annot.Name(), valTy.Name())
	}
	if as.Annot != nil {
		// gradual typing: annotation overrides inferred type
		valTy = as.Annot
	}
	if n, ok := as.Target.(*Name); ok {
		an.scope.define(n.Value, valTy)
	}
	if t, ok := as.Target.(*Tuple); ok {
		for _, nm := range t.Elems {
			if n, ok2 := nm.(*Name); ok2 {
				an.scope.define(n.Value, TDyn())
			}
		}
	}
	if a, ok := as.Target.(*Attr); ok {
		an.inferExpr(a.Obj)
	}
	if a, ok := as.Target.(*Attr); ok {
		an.inferExpr(a.Obj)
	}
}

func (an *SemanticAnalyzer) analyzeFunc(fd *FuncDef) {
	// Decorators are evaluated in the enclosing scope at def time:
	// @dec def f -> f = dec(f). Infer each decorator expression for validity.
	for _, dec := range fd.Decorators {
		an.inferExpr(dec)
	}
	an.funcs[fd.Name] = fd
	an.inFunc = true
	old := an.scope
	fscope := newScope(old)
	// define the function itself in the outer scope
	ft := TFunc(nil, fd.ReturnAnno)
	old.define(fd.Name, ft)
	an.scope = fscope
	an.curFn = fd
	for _, p := range fd.Params {
		pt := p.Annot
		if pt == nil {
			pt = TDyn()
		}
		fscope.define(p.Name, pt)
	}
	for _, st := range fd.Body {
		an.analyzeStmt(st)
	}
	an.scope = old
	an.curFn = nil
	an.inFunc = false
}

// inferExpr returns the inferred type of an expression.
func (an *SemanticAnalyzer) inferExpr(e Expr) *Type {
	switch n := e.(type) {
	case *IntLit:
		return TInt()
	case *FloatLit:
		return TFlt()
	case *BoolLit:
		return TBool()
	case *StrLit:
		return TStr()
	case *NoneLit:
		return TNone()
	case *Name:
		t := an.scope.lookup(n.Value)
		if t == nil {
			if an.classes[n.Value] {
				return TDyn()
			}
			if an.exceptions[n.Value] {
				return TDyn()
			}
			an.errorf(n.Span(), "undefined name %q", n.Value)
			return TDyn()
		}
		return t
	case *BinOp:
		return an.inferBinOp(n)
	case *UnOp:
		if n.Op == "not" {
			return TBool()
		}
		return an.inferExpr(n.X)
	case *Call:
		return an.inferCall(n)
	case *ListLit:
		var elem *Type
		if len(n.Elems) > 0 {
			elem = an.inferExpr(n.Elems[0])
		} else {
			elem = TDyn()
		}
		return TList(elem)
	case *Tuple:
		for _, el := range n.Elems {
			an.inferExpr(el)
		}
		return TDyn()
	case *DictLit:
		var kt, vt *Type
		if len(n.Keys) > 0 {
			kt = an.inferExpr(n.Keys[0])
			vt = an.inferExpr(n.Vals[0])
		} else {
			kt, vt = TDyn(), TDyn()
		}
		return TDict(kt, vt)
	case *SetLit:
		var elem *Type
		if len(n.Elems) > 0 {
			elem = an.inferExpr(n.Elems[0])
		} else {
			elem = TDyn()
		}
		return TSet(elem)
	case *Comp:
		return an.inferComp(n)
	case *Lambda:
		return TFunc(nil, TDyn())
	case *Attr:
		return TDyn()
	case *Index:
		an.inferExpr(n.Obj)
		an.inferExpr(n.Idx)
		return TDyn()
	case *Slice:
		objTy := an.inferExpr(n.Obj)
		if n.Low != nil {
			an.inferExpr(n.Low)
		}
		if n.High != nil {
			an.inferExpr(n.High)
		}
		if n.Step != nil {
			an.inferExpr(n.Step)
		}
		// slicing preserves the element container type (str -> str, list -> list)
		if objTy.Kind == KindString || objTy.Kind == KindList {
			return objTy
		}
		return TDyn()
	case *Generator:
		return TIter(TDyn())
	case *CondExpr:
		an.inferExpr(n.Cond)
		thenTy := an.inferExpr(n.If)
		elseTy := an.inferExpr(n.Else)
		// the result is the branch type when both agree, else dynamic.
		if thenTy.Same(elseTy) {
			return thenTy
		}
		return TDyn()
	default:
		return TDyn()
	}
}

func (an *SemanticAnalyzer) inferBinOp(n *BinOp) *Type {
	lt := an.inferExpr(n.L)
	rt := an.inferExpr(n.R)
	switch n.Op {
	case "+", "-", "*", "/", "//", "%":
		if lt.IsNum() && rt.IsNum() {
			if lt.Kind == KindFloat || rt.Kind == KindFloat {
				return TFlt()
			}
			return TInt()
		}
		// string concatenation: "a" + "b" -> str (both operands strings)
		if n.Op == "+" && lt.Kind == KindString && rt.Kind == KindString {
			return TStr()
		}
		if !an.inFunc {
			an.warnf(n.Span(), "arithmetic on non-numeric operands (%s, %s)", lt.Name(), rt.Name())
		}
		return TDyn()
	case "==", "!=", "<", "<=", ">", ">=", "in", "not in", "is", "is not":
		return TBool()
	case "and", "or":
		return TBool()
	default:
		return TDyn()
	}
}

func (an *SemanticAnalyzer) inferReturn(fd *FuncDef, argTypes []*Type) *Type {
	if an.inferring[fd.Name] {
		return TDyn()
	}
	if an.inferring == nil {
		an.inferring = map[string]bool{}
	}
	an.inferring[fd.Name] = true
	defer delete(an.inferring, fd.Name)

	old := an.scope
	fscope := newScope(an.scope)
	for i, p := range fd.Params {
		if i < len(argTypes) {
			fscope.define(p.Name, argTypes[i])
		} else {
			fscope.define(p.Name, TDyn())
		}
	}
	an.scope = fscope
	var ret *Type
	for _, st := range fd.Body {
		an.analyzeStmt(st)
		if rs, ok := st.(*ReturnStmt); ok {
			ret = an.inferExpr(rs.Expr)
		}
	}
	an.scope = old
	if ret == nil {
		return TVoid()
	}
	return ret
}

func (an *SemanticAnalyzer) inferCall(n *Call) *Type {
	if name, ok := n.Fn.(*Name); ok {
		if an.classes[name.Value] {
			return TDyn()
		}
		if fd, ok2 := an.funcs[name.Value]; ok2 {
			return an.inferUserCall(fd, n)
		}
		switch name.Value {
		case "range":
			return TIter(TInt())
		case "print":
			return TVoid()
		case "len":
			return TInt()
		case "super":
			return TDyn()
		}
	}
	ft := an.inferExpr(n.Fn)
	for _, a := range n.Args {
		an.inferArg(a)
	}
	if ft != nil && ft.Kind == KindFunc {
		// A callable value whose return type is unknown (e.g. an unannotated
		// closure) is still callable; treat the result as dynamic so that
		// names assigned from it are not reported as undefined.
		if ft.Ret != nil {
			return ft.Ret
		}
		return TDyn()
	}
	return TDyn()
}

// inferArg infers the type of a call argument, unwrapping keyword arguments.
func (an *SemanticAnalyzer) inferArg(a Expr) *Type {
	if kw, ok := a.(*KeywordArg); ok {
		return an.inferExpr(kw.Value)
	}
	return an.inferExpr(a)
}

// inferUserCall infers the result type of a call to a user-defined function,
// binding positional and keyword arguments to parameters and filling defaults.
func (an *SemanticAnalyzer) inferUserCall(fd *FuncDef, n *Call) *Type {
	provided, err := an.bindParams(fd.Params, n)
	if err != nil {
		an.errorf(n.Span(), "%s", err)
	}
	argTypes := make([]*Type, len(fd.Params))
	for i, p := range fd.Params {
		if t, ok := provided[i]; ok {
			argTypes[i] = t
		} else if p.Default != nil {
			argTypes[i] = an.inferExpr(p.Default)
		} else {
			argTypes[i] = nil
		}
	}
	return an.inferReturn(fd, argTypes)
}

// bindParams maps a call's positional + keyword arguments onto parameter
// indices (by position or name). It reports arity and keyword-name errors.
func (an *SemanticAnalyzer) bindParams(params []*Param, n *Call) (map[int]*Type, error) {
	provided := map[int]*Type{}
	pos := 0
	seenKw := false
	for _, a := range n.Args {
		if kw, ok := a.(*KeywordArg); ok {
			seenKw = true
			found := -1
			for i, p := range params {
				if p.Name == kw.Name {
					found = i
					break
				}
			}
			if found < 0 {
				return provided, fmt.Errorf("unknown keyword argument %q", kw.Name)
			}
			if _, dup := provided[found]; dup {
				return provided, fmt.Errorf("multiple values for argument %q", kw.Name)
			}
			provided[found] = an.inferArg(kw)
			continue
		}
		if seenKw {
			return provided, fmt.Errorf("positional argument after keyword argument")
		}
		if pos >= len(params) {
			return provided, fmt.Errorf("too many arguments")
		}
		if _, dup := provided[pos]; dup {
			return provided, fmt.Errorf("multiple values for argument %q", params[pos].Name)
		}
		provided[pos] = an.inferArg(a)
		pos++
	}
	return provided, nil
}

func (an *SemanticAnalyzer) inferComp(n *Comp) *Type {
	// infer the iterable's element type to bind the comprehension variable
	it := an.inferExpr(n.Iter)
	var elem *Type
	if it != nil && it.Kind == KindIterator {
		elem = it.Elem
	} else {
		elem = TDyn()
	}
	// bind the comprehension variable in a fresh scope so the condition
	// and body can reference it (e.g. `[x * 2 for x in xs]`).
	old := an.scope
	an.scope = newScope(old)
	if n.ForVar != nil {
		an.scope.define(n.ForVar.Value, elem)
	}
	if n.Cond != nil {
		an.inferExpr(n.Cond)
	}
	var e *Type
	if len(n.Elems) > 0 {
		e = an.inferExpr(n.Elems[0])
	} else {
		e = elem
	}
	an.scope = old
	switch n.Kind {
	case CompList:
		return TList(e)
	case CompSet:
		return TSet(e)
	case CompDict:
		return TDict(TDyn(), TDyn())
	default:
		return TIter(e)
	}
}
