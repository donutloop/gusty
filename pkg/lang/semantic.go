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
	externs    map[string]*ExternDecl
	exceptions map[string]bool
	classes    map[string]bool
	inFunc     bool
	loopDepth  int
	inferring  map[string]bool
}

// Analyze runs semantic analysis and type inference on prog.
func Analyze(prog *Program) []Diagnostic {
	an := &SemanticAnalyzer{scope: newScope(nil), funcs: map[string]*FuncDef{}, externs: map[string]*ExternDecl{}, classes: map[string]bool{}, exceptions: map[string]bool{"Exception": true}}
	// predeclare builtins
	an.scope.define("print", TFunc(nil, TVoid()))
	an.scope.define("range", TIter(TInt()))
	an.scope.define("min", TFunc([]*Type{TList(TInt())}, TInt()))
	an.scope.define("max", TFunc([]*Type{TList(TInt())}, TInt()))
	an.scope.define("abs", TFunc([]*Type{TInt()}, TInt()))
	for _, st := range prog.Stmts {
		an.analyzeStmt(st)
	}
	// surface lexer-recovered diagnostics (L4.1) ahead of semantic ones
	all := append([]Diagnostic{}, prog.Diags...)
	all = append(all, an.Diags...)
	return all
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
			ty := an.inferExpr(s.Expr)
			if ra := an.returnAnno(); ra != nil && ty != nil && ty.Kind != KindDynamic && ty.Kind != KindVoid && !assignable(ty, ra) {
				an.errorf(s.Span(), "return type mismatch: expected %s, got %s", ra.Name(), ty.Name())
			}
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

	case *ExternDecl:
		an.externs[s.Name] = s
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
		var boundAll map[string]bool
		first := true
		irrefutable := false
		for _, c := range s.Cases {
			an.scope = newScope(an.scope)
			for _, p := range append([]Expr{c.Pattern}, c.Or...) {
				for n := range matchPatternNames(p) {
					an.scope.define(n, TDyn())
				}
			}
			bound := map[string]bool{}
			for _, p := range append([]Expr{c.Pattern}, c.Or...) {
				for n := range matchPatternNames(p) {
					bound[n] = true
				}
			}
			if first {
				boundAll = bound
				first = false
			} else {
				for n := range boundAll {
					if !bound[n] {
						delete(boundAll, n)
					}
				}
			}
			if c.Guard == nil {
				for _, p := range append([]Expr{c.Pattern}, c.Or...) {
					if _, ok := p.(*Name); ok {
						irrefutable = true
					}
				}
			}
			for _, b := range c.Body {
				an.analyzeStmt(b)
			}
			an.scope = an.scope.Parent
		}
		if !irrefutable {
			an.warnf(s.Src, "match is not exhaustive: add a wildcard `_` or always-matching binding case")
		}
		for n := range boundAll {
			an.scope.define(n, TDyn())
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
	case *YieldFromStmt:
		an.inferExpr(s.Expr)
	case *WithStmt:
		an.inferExpr(s.Expr)
		if s.As != nil {
			an.scope.define(s.As.Value, TDyn())
		}
		for _, b := range s.Body {
			an.analyzeStmt(b)
		}
	}
}

func (an *SemanticAnalyzer) analyzeAssign(as *AssignStmt) {
	valTy := an.inferExpr(as.Value)
	// Static gradual typing: report an annotation/inferred-type mismatch.
	if as.Annot != nil && valTy != nil && valTy.Kind != KindDynamic && as.Annot.Kind != KindDynamic && !assignable(valTy, as.Annot) {
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
		var elemTypes []*Type
		if valTy != nil && valTy.Kind == KindTuple {
			elemTypes = valTy.Elems
		}
		for i, nm := range t.Elems {
			if n, ok2 := nm.(*Name); ok2 {
				et := TDyn()
				if i < len(elemTypes) {
					et = elemTypes[i]
				}
				an.scope.define(n.Value, et)
			}
		}
		if valTy != nil && valTy.Kind == KindTuple && len(valTy.Elems) != len(t.Elems) {
			an.errorf(as.Span(), "tuple unpack length mismatch: got %d, want %d", len(valTy.Elems), len(t.Elems))
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
func (an *SemanticAnalyzer) returnAnno() *Type {
	if an.curFn == nil || an.curFn.ReturnAnno == nil {
		return nil
	}
	return an.curFn.ReturnAnno
}

func (an *SemanticAnalyzer) inferExpr(e Expr) *Type {
	ty := an.inferExprTy(e)
	if ty != nil {
		switch n := e.(type) {		case *AssignExpr:
			ty := an.inferExpr(n.Value)
			an.scope.define(n.Name.Value, ty)
			n.Ty = ty.Name()

		case *Name:
			n.Ty = ty.Name()
		case *IntLit:
			n.Ty = ty.Name()
		case *FloatLit:
			n.Ty = ty.Name()
		case *BoolLit:
			n.Ty = ty.Name()
		case *NoneLit:
			n.Ty = ty.Name()
		case *StrLit:
			n.Ty = ty.Name()
		case *FString:
			n.Ty = ty.Name()
		case *ListLit:
			n.Ty = ty.Name()
		case *DictLit:
			n.Ty = ty.Name()
		case *SetLit:
			n.Ty = ty.Name()
		case *Tuple:
			n.Ty = ty.Name()
		case *BinOp:
			n.Ty = ty.Name()
		case *UnOp:
			n.Ty = ty.Name()
		case *CondExpr:
			n.Ty = ty.Name()
		case *Call:
			n.Ty = ty.Name()
		case *Index:
			n.Ty = ty.Name()
		case *Slice:
			n.Ty = ty.Name()
		case *Attr:
			n.Ty = ty.Name()
		case *Lambda:
			n.Ty = ty.Name()
		case *Comp:
			n.Ty = ty.Name()
		case *Generator:
			n.Ty = ty.Name()
		}
	}
	return ty
}

func (an *SemanticAnalyzer) inferExprTy(e Expr) *Type {
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
	case *AssignExpr:
		valTy := an.inferExpr(n.Value)
		an.scope.define(n.Name.Value, valTy)
		return valTy

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
		var elems []*Type
		for _, el := range n.Elems {
			elems = append(elems, an.inferExpr(el))
		}
		return TTuple(elems...)
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
		// the result is the branch type when both agree; when the branches
		// carry different concrete types, widen to a normalized union so the
		// type flows through assignments and call boundaries (L6.3).
		if thenTy.Same(elseTy) {
			return thenTy
		}
		return normalizeUnion(thenTy, elseTy)
	default:
		return TDyn()
	}
}

func (an *SemanticAnalyzer) inferBinOp(n *BinOp) *Type {
	lt := an.inferExpr(n.L)
	rt := an.inferExpr(n.R)
	switch n.Op {
	case "+", "-", "*", "/", "//", "%":
		// Union-aware arithmetic (L6.3): when an operand is union-typed we
		// widen over its members. If every member on both sides is numeric
		// the result is numeric; if every member on both sides is a string
		// then `+` concatenates to str.
		if unionAllNumeric(lt) && unionAllNumeric(rt) {
			if anyFloat(unionMembers(lt)) || anyFloat(unionMembers(rt)) {
				return TFlt()
			}
			return TInt()
		}
		// string concatenation: "a" + "b" -> str (both operands strings)
		if n.Op == "+" && unionAllString(lt) && unionAllString(rt) {
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
		if ed, ok2 := an.externs[name.Value]; ok2 {
			// extern (FFI) call: arity + arg type check; return type comes from the
			// extern declaration's return annotation.
			if len(ed.Params) != len(n.Args) {
				an.errorf(n.Src, "extern function %q expects %d arguments, got %d", name.Value, len(ed.Params), len(n.Args))
				return TDyn()
			}
			for i, p := range ed.Params {
				at := an.inferExpr(n.Args[i])
				if !assignable(at, p.Annot) {
					an.errorf(n.Args[i].Span(), "argument %d of extern function %q: expected %s, got %s", i+1, name.Value, typeName(p.Annot), typeName(at))
				}
			}
			if ed.ReturnAnno != nil {
				return ed.ReturnAnno
			}
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
		case "float", "round", "int", "str", "chr", "ord":
			// conversion builtins: infer args, then return the converted type.
			for _, a := range n.Args {
				an.inferArg(a)
			}
			switch name.Value {
			case "float":
				return TFlt()
			case "round", "int", "ord":
				return TInt()
			default:
				return TStr()
			}
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
	// mypy-style: check each statically-typed argument against its annotation.
	for i, p := range fd.Params {
		if p.Annot == nil || p.Annot.Kind == KindDynamic {
			continue
		}
		got := argTypes[i]
		if got == nil || got.IsDyn() {
			continue
		}
		if !assignable(got, p.Annot) {
			an.errorf(n.Span(), "argument %q: expected %s, got %s", p.Name, p.Annot.Name(), got.Name())
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

// assignable reports whether a concrete type `got` is assignable to a declared
// type `want` under gradual typing and structural protocols (generics).
// Dynamic types are tolerated in either position; Sequence[T] and
// Callable[[...], R] act as structural bounds accepting matching concrete
// sequence / callable types.
// unionMembers returns the flattened member list of a union type (or a
// single-element list for a plain type), so callers can reason over every
// possible runtime value a union-typed expression can hold.
func unionMembers(t *Type) []*Type {
	if t == nil || t.Kind != KindUnion {
		return []*Type{t}
	}
	return t.Members
}

// normalizeUnion builds a union type from the given member types: it flattens
// nested unions, drops KindDynamic (unknown) members, dedupes structurally
// identical members, and collapses a single surviving member back to the
// plain type. None is preserved so `int | None` reads as Optional sugar.
func normalizeUnion(members ...*Type) *Type {
	seen := []*Type{}
	var add func(m *Type)
	add = func(m *Type) {
		if m == nil {
			return
		}
		if m.Kind == KindUnion {
			for _, sub := range m.Members {
				add(sub)
			}
			return
		}
		if m.IsDyn() {
			return
		}
		for _, s := range seen {
			if s.Same(m) {
				return
			}
		}
		seen = append(seen, m)
	}
	for _, m := range members {
		add(m)
	}
	switch len(seen) {
	case 0:
		return TDyn()
	case 1:
		return seen[0]
	default:
		return TUnion(seen...)
	}
}

// anyFloat reports whether any member of a union-typed value is a float type.
// A plain float type trivially passes.
func anyFloat(members []*Type) bool {
	for _, m := range members {
		if m != nil && m.Kind == KindFloat {
			return true
		}
	}
	return false
}

// unionAllNumeric reports whether every member of a union-typed value is a
// numeric type (int or float). A plain numeric type trivially passes.
func unionAllNumeric(t *Type) bool {
	for _, m := range unionMembers(t) {
		if !m.IsNum() {
			return false
		}
	}
	return true
}

// unionAllString reports whether every member of a union-typed value is a
// string type.
func unionAllString(t *Type) bool {
	for _, m := range unionMembers(t) {
		if m.Kind != KindString {
			return false
		}
	}
	return true
}

func assignable(got, want *Type) bool {
	// A union-typed `got` is assignable to `want` iff every member is.
	if got != nil && got.Kind == KindUnion {
		for _, m := range got.Members {
			if !assignable(m, want) {
				return false
			}
		}
		return true
	}
	if got == nil || want == nil {
		return true
	}
	if got.IsDyn() || want.IsDyn() {
		return true
	}
	switch want.Kind {
	case KindSequence:
		return seqAssignable(got, want.Elem)
	case KindCallable:
		return callableAssignable(got, want.Params, want.Ret)
	case KindUnion:
		// `got` is assignable to `want` union iff assignable to any member.
		for _, m := range want.Members {
			if assignable(got, m) {
				return true
			}
		}
		return false
	default:
		return got.Kind == want.Kind
	}
}

// seqAssignable reports whether `got` is a sequence of element type compatible
// with the Sequence bound's element type.
func seqAssignable(got *Type, elem *Type) bool {
	if elem == nil || elem.IsDyn() {
		return true
	}
	switch got.Kind {
	case KindList, KindSet, KindIterator:
		if got.Elem == nil || got.Elem.IsDyn() {
			return true
		}
		return got.Elem.Kind == elem.Kind || got.Elem.Same(elem)
	case KindTuple:
		// A heterogeneous tuple is Sequence[T] only when every element is T.
		for _, e := range got.Elems {
			if e == nil || e.IsDyn() {
				continue
			}
			if e.Kind != elem.Kind && !e.Same(elem) {
				return false
			}
		}
		return true
	case KindString:
		// str is Sequence[str].
		return elem.Kind == KindString
	case KindSequence:
		return got.Elem == nil || got.Elem.IsDyn() || got.Elem.Kind == elem.Kind
	default:
		return false
	}
}

// callableAssignable reports whether `got` (a func/callable type) matches a
// Callable bound with the given parameter and return types.
func callableAssignable(got *Type, wantParams []*Type, wantRet *Type) bool {
	if got.Kind != KindFunc && got.Kind != KindCallable {
		return false
	}
	// A function-name reference carries no parameter info (empty Params), so
	// under gradual typing it is assignable to any Callable bound: arity and
	// param kinds cannot be checked. Explicitly-typed funcs with params must
	// match arity and kinds.
	if len(got.Params) == 0 {
		return true
	}
	if len(got.Params) != len(wantParams) {
		return false
	}
	for i, gp := range got.Params {
		wp := wantParams[i]
		if gp == nil || wp == nil || gp.IsDyn() || wp.IsDyn() {
			continue
		}
		if gp.Kind != wp.Kind && !assignable(gp, wp) {
			return false
		}
	}
	// Return is covariant: got's return must be assignable to the bound's.
	if got.Ret == nil || wantRet == nil || got.Ret.IsDyn() || wantRet.IsDyn() {
		return true
	}
	return assignable(got.Ret, wantRet)
}

// matchPatternNames returns the set of pattern-bound names (non-wildcard)
// introduced by a match pattern expression.
func matchPatternNames(p Expr) map[string]bool {
	names := map[string]bool{}
	switch pat := p.(type) {
	case *Name:
		if pat.Value != "_" {
			names[pat.Value] = true
		}
	case *ListLit:
		for _, e := range pat.Elems {
			for n := range matchPatternNames(e) {
				names[n] = true
			}
		}
	case *DictLit:
		for _, v := range pat.Vals {
			for n := range matchPatternNames(v) {
				names[n] = true
			}
		}
	case *Call:
		for _, a := range pat.Args {
			for n := range matchPatternNames(a) {
				names[n] = true
			}
		}
	}
	return names
}
