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
	scope  *Scope
	Diags  []Diagnostic
	curFn  *FuncDef
}

// Analyze runs semantic analysis and type inference on prog.
func Analyze(prog *Program) []Diagnostic {
	an := &SemanticAnalyzer{scope: newScope(nil)}
	// predeclare builtins
	an.scope.define("print", TFunc(nil, TVoid()))
	an.scope.define("range", TIter(TInt()))
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
	case *ExprStmt:
		an.inferExpr(s.Expr)
	case *ReturnStmt:
		if s.Expr != nil {
			an.inferExpr(s.Expr)
		}
	case *IfStmt:
		an.inferExpr(s.Cond)
		old := an.scope
		an.scope = newScope(old)
		for _, b := range s.Then {
			an.analyzeStmt(b)
		}
		an.scope = newScope(old)
		for _, b := range s.Else {
			an.analyzeStmt(b)
		}
		an.scope = old
	case *WhileStmt:
		an.inferExpr(s.Cond)
		old := an.scope
		an.scope = newScope(old)
		for _, b := range s.Body {
			an.analyzeStmt(b)
		}
		an.scope = old
	case *ForStmt:
		it := an.inferExpr(s.Iter)
		elem := it
		if it != nil && it.Kind == KindIterator {
			elem = it.Elem
		}
		an.scope = newScope(an.scope)
		an.scope.define(s.Var.Value, elem)
		for _, b := range s.Body {
			an.analyzeStmt(b)
		}
		an.scope = an.scope.Parent
	case *FuncDef:
		an.analyzeFunc(s)
	case *ClassDef:
		an.scope = newScope(an.scope)
		for _, b := range s.Body {
			an.analyzeStmt(b)
		}
		an.scope = an.scope.Parent
	case *ImportStmt:
		an.scope.define(s.Module, TDyn())
	case *MatchStmt:
		an.inferExpr(s.Subject)
		for _, c := range s.Cases {
			an.scope = newScope(an.scope)
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
	if as.Annot != nil {
		// gradual typing: annotation overrides inferred type
		valTy = as.Annot
	}
	if n, ok := as.Target.(*Name); ok {
		an.scope.define(n.Value, valTy)
	}
}

func (an *SemanticAnalyzer) analyzeFunc(fd *FuncDef) {
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
	case *Generator:
		return TIter(TDyn())
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
		an.warnf(n.Span(), "arithmetic on non-numeric operands (%s, %s)", lt.Name(), rt.Name())
		return TDyn()
	case "==", "!=", "<", "<=", ">", ">=":
		return TBool()
	case "and", "or":
		return TBool()
	default:
		return TDyn()
	}
}

func (an *SemanticAnalyzer) inferCall(n *Call) *Type {
	if name, ok := n.Fn.(*Name); ok {
		switch name.Value {
		case "range":
			return TIter(TInt())
		case "print":
			return TVoid()
		}
	}
	ft := an.inferExpr(n.Fn)
	for _, a := range n.Args {
		an.inferExpr(a)
	}
	if ft != nil && ft.Kind == KindFunc {
		return ft.Ret
	}
	return TDyn()
}

func (an *SemanticAnalyzer) inferComp(n *Comp) *Type {
	an.inferExpr(n.Iter)
	if n.Cond != nil {
		an.inferExpr(n.Cond)
	}
	var elem *Type
	if len(n.Elems) > 0 {
		elem = an.inferExpr(n.Elems[0])
	} else {
		elem = TDyn()
	}
	switch n.Kind {
	case CompList:
		return TList(elem)
	case CompSet:
		return TSet(elem)
	case CompDict:
		return TDict(TDyn(), TDyn())
	default:
		return TIter(elem)
	}
}
