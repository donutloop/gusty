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
	// classList records the declared base chain so nominal subtyping (and the
	// covariant/contravariant rules that reach into class-typed arguments) can
	// walk it. It is filled by a pre-pass so a subclass may be referenced before
	// its declaration site (L6.6).
	classList *ClassIndex
	inFunc    bool
	loopDepth int
	inferring map[string]bool
	definite  map[string]bool
	locals    map[string]bool
	branchDef map[string]bool
}

func (an *SemanticAnalyzer) markDefinite(nm string) {
	if an.definite == nil {
		an.definite = map[string]bool{}
	}
	an.definite[nm] = true
}
func (an *SemanticAnalyzer) markLocal(nm string) {
	if an.locals == nil {
		an.locals = map[string]bool{}
	}
	an.locals[nm] = true
}
func copyDefinite(m map[string]bool) map[string]bool {
	out := map[string]bool{}
	for n := range m {
		out[n] = true
	}
	return out
}
func intersectDef(dst, src map[string]bool) {
	for n := range dst {
		if !src[n] {
			delete(dst, n)
		}
	}
}
func (an *SemanticAnalyzer) isDefinite(nm string) bool {
	if an.locals == nil || !an.locals[nm] {
		return true
	}
	return an.definite != nil && an.definite[nm]
}

// Analyze runs semantic analysis and type inference on prog.
func Analyze(prog *Program) []Diagnostic {
	an := &SemanticAnalyzer{scope: newScope(nil), funcs: map[string]*FuncDef{}, externs: map[string]*ExternDecl{}, classes: map[string]bool{}, exceptions: map[string]bool{"Exception": true}, classList: NewClassIndex()}
	// Pre-pass: record every class (with its bases) before analyzing, so class
	// annotations and nominal subtyping work regardless of declaration order.
	an.indexClasses(prog.Stmts)
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

// indexClasses walks the statement tree (including nested blocks) recording
// class names and their bases into the analyzer's ClassIndex.
func (an *SemanticAnalyzer) indexClasses(stmts []Stmt) {
	for _, st := range stmts {
		switch s := st.(type) {
		case *ClassDef:
			var bases []string
			for _, b := range s.Bases {
				if b != nil {
					bases = append(bases, b.Value)
					an.classes[b.Value] = true
				}
			}
			an.classes[s.Name] = true
			an.classList.Declare(s.Name, bases)
			an.indexClasses(s.Body)
		case *FuncDef:
			an.indexClasses(s.Body)
		case *IfStmt:
			an.indexClasses(s.Then)
			for _, e := range s.Elifs {
				an.indexClasses(e.Then)
			}
			an.indexClasses(s.Else)
		case *WhileStmt:
			an.indexClasses(s.Body)
			an.indexClasses(s.Else)
		case *ForStmt:
			an.indexClasses(s.Body)
			an.indexClasses(s.Else)
		case *WithStmt:
			an.indexClasses(s.Body)
		case *TryStmt:
			an.indexClasses(s.Body)
			for _, e := range s.Excepts {
				an.indexClasses(e.Body)
			}
			an.indexClasses(s.Finally)
		case *MatchStmt:
			for _, c := range s.Cases {
				an.indexClasses(c.Body)
			}
		}
	}
}

// flowCheckAt is the shared body of the flow diagnostics: `got` (the inferred
// type of the optional expression `e`) must flow into `want`.
//
// A freshly built container literal is checked COVARIANTLY element by element:
// nothing aliases the new object yet, so its element type may widen to the
// destination's — `x: list[int | str] = [1]` is fine, exactly like mypy's
// contextual inference. Any other value is a pre-existing object, so the full
// invariant / covariant / contravariant rules apply (subType).
func (an *SemanticAnalyzer) flowCheckAt(sp Span, e Expr, got, want *Type, ctxPrefix string) bool {
	if want == nil || want.IsDyn() || got == nil || got.IsDyn() {
		return true
	}
	report := func(v *Violation) bool {
		msg := ctxPrefix + ": expected " + want.Name() + ", got " + got.Name()
		if v.Kind != RuleKindMismatch {
			msg += " — " + v.Msg
		}
		an.Diags = append(an.Diags, Diagnostic{Level: LevelError, Span: sp, Msg: msg, Code: v.Code, Suggestion: v.Suggestion})
		return false
	}
	if freshContainer(e, want) {
		if v := freshContainerViolation(an.classList, e, want); v != nil {
			return report(v)
		}
		return true
	}
	if v := subType(an.classList, got, want); v != nil {
		return report(v)
	}
	return true
}

// freshContainer reports whether `e` builds a brand-new container whose type
// arguments are still free (a literal whose kind matches the destination).
func freshContainer(e Expr, want *Type) bool {
	if want == nil {
		return false
	}
	switch e.(type) {
	case *ListLit:
		return want.Kind == KindList
	case *SetLit:
		return want.Kind == KindSet
	case *DictLit:
		return want.Kind == KindDict
	}
	return false
}

// constType returns the type of a constant literal expression, nil otherwise.
func constType(e Expr) *Type {
	switch e.(type) {
	case *IntLit:
		return TInt()
	case *FloatLit:
		return TFlt()
	case *StrLit:
		return TStr()
	case *BoolLit:
		return TBool()
	case *NoneLit:
		return TNone()
	}
	return nil
}

// freshContainerViolation checks a fresh container literal's elements against
// the destination's type arguments — the covariant position of an object that
// has no other aliases yet.
func freshContainerViolation(ci *ClassIndex, e Expr, want *Type) *Violation {
	why := func(pos, got, want string) *Violation {
		return &Violation{Kind: RuleCovariant, Code: CodeVarianceCovariant,
			Msg:        want + " is covariant in " + pos + " for a freshly built literal: " + got + " is not " + want,
			Suggestion: "give the literal " + want + " items, or annotate the destination with the narrower element type"}
	}
	switch n := e.(type) {
	case *ListLit:
		if want.Kind != KindList || want.Elem == nil || want.Elem.IsDyn() {
			return nil
		}
		for _, el := range n.Elems {
			et := constType(el)
			if et == nil {
				continue // unknown element: gradual typing tolerates it
			}
			if subType(ci, et, want.Elem) != nil {
				return why("T", et.Name(), want.Name())
			}
		}
	case *SetLit:
		if want.Kind != KindSet || want.Elem == nil || want.Elem.IsDyn() {
			return nil
		}
		for _, el := range n.Elems {
			if et := constType(el); et != nil && subType(ci, et, want.Elem) != nil {
				return why("T", et.Name(), want.Name())
			}
		}
	case *DictLit:
		if want.Kind != KindDict {
			return nil
		}
		for i, k := range n.Keys {
			if want.Key != nil && !want.Key.IsDyn() {
				if kt := constType(k); kt != nil && subType(ci, kt, want.Key) != nil {
					return why("K", kt.Name(), want.Name())
				}
			}
			if i < len(n.Vals) && want.Val != nil && !want.Val.IsDyn() {
				if vt := constType(n.Vals[i]); vt != nil && subType(ci, vt, want.Val) != nil {
					return why("V", vt.Name(), want.Name())
				}
			}
		}
	}
	return nil
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
			if ty != nil && ty.Kind != KindDynamic && ty.Kind != KindVoid {
				an.flowCheckAt(s.Span(), s.Expr, ty, an.returnAnno(), "return type mismatch")
			}
		}
	case *RaiseStmt:
		if s.Expr != nil {
			an.inferExpr(s.Expr)
		}
	case *IfStmt:
		// if/elif/else bodies share the enclosing scope: assignments there
		// flow outward (like Python), so do NOT create a child scope.
		//
		// Type narrowing/refinement (L6.5): `if isinstance(x, int):` narrows
		// `x` to `int` in the then branch and away from `int` in the else
		// branch; `if not isinstance(x, int):` flips those. Narrowing is
		// applied as a temporary shadow that is restored after the block so
		// assignments inside still flow outward.
		an.inferExpr(s.Cond)
		pos, neg := narrowFromCond(s.Cond)
		an.analyzeNarrowed(s.Then, pos, neg)
		defAfter := copyDefinite(an.branchDef)
		lastPos, lastNeg := pos, neg
		for _, e := range s.Elifs {
			an.inferExpr(e.Cond)
			ePos, eNeg := narrowFromCond(e.Cond)
			an.analyzeNarrowed(e.Then, ePos, eNeg)
			intersectDef(defAfter, an.branchDef)
			lastPos, lastNeg = ePos, eNeg
		}
		// The else branch sees the negation of the (last) condition.
		an.analyzeNarrowed(s.Else, lastNeg, lastPos)
		intersectDef(defAfter, an.branchDef)
		an.definite = defAfter
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
		if it != nil {
			// A `for` binds the loop variable to what iteration *yields*, not to the
			// iterable: a list/set yields elements, a dict yields keys. Leaving the
			// iterable's own type here made `for k in d: s = s + k` infer
			// `int + dict[any, any]` and reject a program that runs correctly.
			switch it.Kind {
			case KindIterator, KindList, KindSet:
				elem = it.Elem
			case KindDict:
				elem = it.Key
			}
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
	case *PassStmt, *TypeAliasStmt:
		// no-op statement (type aliases are compile-time only; L5.7)
	case *MatchStmt:
		subTy := an.inferExpr(s.Subject)
		var boundAll map[string]bool
		first := true
		irrefutable := false
		covered := map[int64]bool{}
		for _, c := range s.Cases {
			an.scope = newScope(an.scope)
			// Narrow the subject inside a constant case: if the subject is a
			// Name and the pattern is an integer constant, shadow it with the
			// Literal[v] type in this case's scope.
			if subj, ok := s.Subject.(*Name); ok {
				if il, ok := c.Pattern.(*IntLit); ok {
					covered[il.Value] = true
					an.scope.define(subj.Value, TLit(il.Value))
				}
			}
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
			if vals := literalValues(subTy); vals != nil {
				allCovered := true
				for _, v := range vals {
					if !covered[v] {
						allCovered = false
						break
					}
				}
				if allCovered {
					irrefutable = true
				}
			}
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
	if as.Annot != nil && valTy != nil && valTy.Kind != KindDynamic && as.Annot.Kind != KindDynamic {
		an.flowCheckAt(as.Span(), as.Value, valTy, as.Annot, "type mismatch")
	}
	if as.Annot != nil {
		// gradual typing: annotation overrides inferred type
		valTy = as.Annot
	}
	if n, ok := as.Target.(*Name); ok {
		an.scope.define(n.Value, valTy)
		an.markDefinite(n.Value)
		if an.inFunc {
			an.markLocal(n.Value)
		}
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
	outerDefinite := copyDefinite(an.definite)
	outerLocals := copyDefinite(an.locals)
	an.definite = map[string]bool{}
	an.locals = map[string]bool{}
	old := an.scope
	fscope := newScope(old)
	// define the function itself in the outer scope. Carry the DECLARED parameter
	// annotations into the function type, so passing a named function to a
	// Callable[[...], R] bound is checked by the variance rules: parameters
	// CONTRAVARIANTLY, the return covariantly (L6.6). An unannotated parameter is
	// dynamic, so untyped code stays gradual (no diagnostics).
	var ptys []*Type
	for _, p := range fd.Params {
		if p.Annot != nil {
			ptys = append(ptys, p.Annot)
		} else {
			ptys = append(ptys, TDyn())
		}
	}
	ft := TFunc(ptys, fd.ReturnAnno)
	old.define(fd.Name, ft)
	an.scope = fscope
	an.curFn = fd
	for _, p := range fd.Params {
		pt := p.Annot
		if pt == nil {
			pt = TDyn()
		}
		fscope.define(p.Name, pt)
		an.markDefinite(p.Name)
		an.markLocal(p.Name)
	}
	for _, st := range fd.Body {
		an.analyzeStmt(st)
	}
	an.scope = old
	an.curFn = nil
	an.inFunc = false
	an.definite = outerDefinite
	an.locals = outerLocals
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
		switch n := e.(type) {
		case *AssignExpr:
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
		if !an.isDefinite(n.Value) {
			an.warnf(n.Span(), "possibly unbound: %q is not definitely assigned on all paths", n.Value)
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
			// A constructor call yields an instance of that class, so the nominal
			// class type flows to annotations (L6.6).
			return TClass(name.Value)
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
				an.flowCheckAt(n.Args[i].Span(), n.Args[i], at, p.Annot, fmt.Sprintf("argument %d of extern function %q", i+1, name.Value))
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
	provided, argExprs, err := an.bindParams(fd.Params, n)
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
	// mypy-style: check each statically-typed argument against its annotation,
	// applying the variance rules — a fresh container literal is covariant, an
	// existing container invariant, a callable contravariant in its parameters.
	for i, p := range fd.Params {
		if p.Annot == nil || p.Annot.Kind == KindDynamic {
			continue
		}
		got := argTypes[i]
		if got == nil || got.IsDyn() {
			continue
		}
		an.flowCheckAt(n.Span(), argExprs[i], got, p.Annot, fmt.Sprintf("argument %q", p.Name))
	}
	return an.inferReturn(fd, argTypes)
}

// bindParams maps a call's positional + keyword arguments onto parameter
// indices (by position or name). It reports arity and keyword-name errors.
func (an *SemanticAnalyzer) bindParams(params []*Param, n *Call) (map[int]*Type, map[int]Expr, error) {
	provided := map[int]*Type{}
	exprs := map[int]Expr{}
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
				return provided, exprs, fmt.Errorf("unknown keyword argument %q", kw.Name)
			}
			if _, dup := provided[found]; dup {
				return provided, exprs, fmt.Errorf("multiple values for argument %q", kw.Name)
			}
			provided[found] = an.inferArg(kw)
			exprs[found] = kw.Value
			continue
		}
		if seenKw {
			return provided, exprs, fmt.Errorf("positional argument after keyword argument")
		}
		if pos >= len(params) {
			return provided, exprs, fmt.Errorf("too many arguments")
		}
		if _, dup := provided[pos]; dup {
			return provided, exprs, fmt.Errorf("multiple values for argument %q", params[pos].Name)
		}
		provided[pos] = an.inferArg(a)
		exprs[pos] = a
		pos++
	}
	return provided, exprs, nil
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

// unionContains reports whether t is a member type of the union u.

// literalValues returns the distinct literal constant values of a Literal type
// or a union of Literal types, or nil if t is not a literal-valued type.
func literalValues(t *Type) []int64 {
	if t == nil {
		return nil
	}
	if t.Kind == KindLiteral {
		return []int64{t.LitVal}
	}
	if t.Kind == KindUnion {
		vals := []int64{}
		for _, m := range t.Members {
			if m.Kind != KindLiteral {
				return nil
			}
			vals = append(vals, m.LitVal)
		}
		return vals
	}
	return nil
}

func unionContains(t, u *Type) bool {
	if u == nil || u.Kind != KindUnion {
		return false
	}
	for _, m := range u.Members {
		if m.Same(t) {
			return true
		}
	}
	return false
}

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

// ---------------------------------------------------------------------------
// Type narrowing / refinement (L6.5)
// ---------------------------------------------------------------------------

// typeNameToType maps a type-name used as the second argument of isinstance
// (e.g. "int", "float", "list") to the corresponding Type for narrowing. It
// returns nil for names that are not known static types (e.g. user classes),
// in which case no narrowing is performed.
func typeNameToType(nm string) *Type {
	switch nm {
	case "int":
		return TInt()
	case "float":
		return TFlt()
	case "bool":
		return TBool()
	case "str", "string":
		return TStr()
	case "list":
		return TList(TDyn())
	case "dict":
		return TDict(TDyn(), TDyn())
	case "set":
		return TSet(TDyn())
	case "tuple":
		return TTuple()
	}
	return nil
}

// narrowFromCond walks a boolean condition conjunctively and extracts
// isinstance narrowing constraints. It returns two maps:
//   - pos: variable -> type it definitely has when the condition is true.
//   - neg: variable -> type it definitely does NOT have when true.
//
// `not isinstance(x, T)` adds x to neg; `isinstance(x, T)` adds x to pos.
// "or" and other boolean shapes are skipped (no safe narrowing).
func narrowFromCond(cond Expr) (pos, neg map[string]*Type) {
	pos = map[string]*Type{}
	neg = map[string]*Type{}
	var walk func(e Expr, flip bool)
	walk = func(e Expr, flip bool) {
		switch n := e.(type) {
		case *BinOp:
			if n.Op == "and" {
				walk(n.L, flip)
				walk(n.R, flip)
			}
			// "or" and comparisons: no safe narrowing.
		case *UnOp:
			if n.Op == "not" {
				walk(n.X, !flip)
			}
		case *Call:
			if fn, ok := n.Fn.(*Name); ok && fn.Value == "isinstance" && len(n.Args) >= 2 {
				subj, ok1 := n.Args[0].(*Name)
				tn, ok2 := n.Args[1].(*Name)
				if ok1 && ok2 {
					if ty := typeNameToType(tn.Value); ty != nil {
						m := pos
						if flip {
							m = neg
						}
						m[subj.Value] = ty
					}
				}
			}
		}
	}
	walk(cond, false)
	return pos, neg
}

// dropType removes `drop` from the union members of `cur`, producing the
// complement type. If `cur` is dynamic or the complement is empty, it returns
// dynamic (unknown), since we cannot represent "not T" precisely.
func dropType(cur, drop *Type) *Type {
	if cur == nil || cur.IsDyn() {
		return TDyn()
	}
	out := []*Type{}
	for _, m := range unionMembers(cur) {
		if m.Same(drop) {
			continue
		}
		out = append(out, m)
	}
	switch len(out) {
	case 0:
		return TDyn()
	case 1:
		return out[0]
	default:
		return normalizeUnion(out...)
	}
}

// analyzeNarrowed analyzes a block of statements with variables temporarily
// narrowed: names in `pos` are narrowed to their type, names in `neg` are
// narrowed away from their type (complement of the current type). Narrowed
// types are restored afterward so assignments inside still flow outward.
func (an *SemanticAnalyzer) analyzeNarrowed(stmts []Stmt, pos, neg map[string]*Type) {
	defBefore := copyDefinite(an.definite)
	type save struct {
		name string
		ty   *Type
	}
	saved := []save{}
	seen := map[string]bool{}
	for name, ty := range pos {
		seen[name] = true
		saved = append(saved, save{name, an.scope.lookup(name)})
		an.scope.define(name, ty)
	}
	for name, ty := range neg {
		if seen[name] {
			continue
		}
		cur := an.scope.lookup(name)
		saved = append(saved, save{name, cur})
		an.scope.define(name, dropType(cur, ty))
	}
	for _, st := range stmts {
		an.analyzeStmt(st)
	}
	an.branchDef = copyDefinite(an.definite)
	an.definite = defBefore
	for _, s := range saved {
		an.scope.define(s.name, s.ty)
	}
}
